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

	// Weather changes slowly and the transit board only refreshes every couple
	// of minutes upstream, so cache both generously — this shields Open-Meteo /
	// Yahoo from the browser's own reload cadence and from multiple viewers.
	weatherCache := newTimedCache(5*time.Minute, func() (*Weather, error) { return client.fetchWeather() })
	diainfoCache := newTimedCache(90*time.Second, func() (*Diainfo, error) { return client.fetchDiainfo() })

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

	mux.HandleFunc("/api/weather", func(w http.ResponseWriter, r *http.Request) {
		wx, stale, err := weatherCache.get()
		writeJSON(w, wx, stale, err)
	})

	mux.HandleFunc("/api/diainfo", func(w http.ResponseWriter, r *http.Request) {
		di, stale, err := diainfoCache.get()
		if di != nil {
			di.Stale = stale
			if stale {
				di.Source = "cache"
			}
		}
		writeJSON(w, di, stale, err)
	})

	mux.HandleFunc("/api/nexttrain", func(w http.ResponseWriter, r *http.Request) {
		st, err := cache.get()
		if err != nil {
			writeJSON(w, nextTrains(nil), false, nil)
			return
		}
		writeJSON(w, nextTrains(st), st.Stale, nil)
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

// writeJSON emits v as JSON. On a hard error with no cached value it returns
// 502 so the browser can show its "retrying" banner; a served-stale copy goes
// out with 200 (the payload carries its own stale flag where relevant).
func writeJSON(w http.ResponseWriter, v any, stale bool, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil && v == nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(v)
}

// timedCache memoizes one fetch behind a TTL, serializing concurrent callers
// and serving the last good value if a refresh fails (so a single upstream
// blip never blanks the dashboard).
type timedCache[T any] struct {
	ttl   time.Duration
	fetch func() (T, error)

	mu  sync.Mutex
	val T
	ok  bool
	at  time.Time
}

func newTimedCache[T any](ttl time.Duration, fetch func() (T, error)) *timedCache[T] {
	return &timedCache[T]{ttl: ttl, fetch: fetch}
}

// get returns the cached value plus a stale flag (true when a refresh failed
// and a previous value is being reused).
func (c *timedCache[T]) get() (T, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok && time.Since(c.at) < c.ttl {
		return c.val, false, nil
	}
	v, err := c.fetch()
	if err != nil {
		if c.ok {
			return c.val, true, nil
		}
		var zero T
		return zero, false, err
	}
	c.val, c.ok, c.at = v, true, time.Now()
	return c.val, false, nil
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
