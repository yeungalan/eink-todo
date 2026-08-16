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
	"strconv"
	"strings"
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

	weather := newWeatherCache()
	calClient := newCalendarClient() // nil if GOOGLE_* env vars are unset

	todoPath := os.Getenv("TODO_DB_PATH")
	if todoPath == "" {
		todoPath = "todos.db"
	}
	todos, err := newTodoStore(todoPath)
	if err != nil {
		log.Fatalf("open todo store: %v", err)
	}

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

	mux.HandleFunc("/api/weather", func(w http.ResponseWriter, r *http.Request) {
		wt, err := weather.get()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(wt)
	})

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if calClient == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "calendar not configured"})
			return
		}
		events, err := calClient.today()
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(events)
	})

	mux.HandleFunc("GET /api/todos", func(w http.ResponseWriter, r *http.Request) {
		list, err := todos.list()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(list)
	})

	mux.HandleFunc("POST /api/todos", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "text is required"})
			return
		}
		t, err := todos.create(strings.TrimSpace(body.Text))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(t)
	})

	mux.HandleFunc("PATCH /api/todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid id"})
			return
		}
		var body struct {
			Done bool `json:"done"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
			return
		}
		if err := todos.setDone(id, body.Done); err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /api/todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid id"})
			return
		}
		if err := todos.delete(id); err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
