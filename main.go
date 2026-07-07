// Command eink-todo serves a desktop-friendly live board for the Keio Line
// train-position feed (opentidkeio.jp), rebuilding the mobile SVG page as a
// wide dashboard. The Go backend fetches and normalizes the upstream feed
// server-side so the browser never has to deal with the HTTP-only, CORS-quirky
// origin.
package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

//go:embed web/*
var webFS embed.FS

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	client := NewClient()
	if err := client.LoadConfig(); err != nil {
		log.Fatalf("load config: %v", err)
	}
	log.Printf("config loaded")

	// Refresh line/destination tables occasionally in the background.
	go func() {
		t := time.NewTicker(6 * time.Hour)
		for range t.C {
			client.loadConfigFrom(client.readLive)
		}
	}()

	cache := &stateCache{client: client, ttl: 8 * time.Second}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		st, err := cache.get()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(st)
	})

	mux.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{"branches": branches})
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	srv := &http.Server{
		Addr:         addr,
		Handler:      logRequests(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 20 * time.Second,
	}
	log.Printf("Keio live board listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

// stateCache coalesces upstream fetches so bursts of browser polls (or many
// viewers) share one request within the TTL window.
type stateCache struct {
	client *Client
	ttl    time.Duration

	mu       sync.Mutex
	value    *State
	fetched  time.Time
	inFlight bool
	wait     chan struct{}
}

func (c *stateCache) get() (*State, error) {
	c.mu.Lock()
	if c.value != nil && time.Since(c.fetched) < c.ttl {
		v := c.value
		c.mu.Unlock()
		return v, nil
	}
	if c.inFlight {
		wait := c.wait
		c.mu.Unlock()
		<-wait
		c.mu.Lock()
		v := c.value
		c.mu.Unlock()
		if v != nil {
			return v, nil
		}
		return c.client.Fetch()
	}
	c.inFlight = true
	c.wait = make(chan struct{})
	wait := c.wait
	c.mu.Unlock()

	st, err := c.client.Fetch()

	c.mu.Lock()
	if err == nil {
		c.value = st
		c.fetched = time.Now()
	}
	c.inFlight = false
	close(wait)
	c.mu.Unlock()
	return st, err
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path == "/api/state" {
			log.Printf("%s %s %v", r.Method, r.URL.Path, time.Since(start))
		}
	})
}
