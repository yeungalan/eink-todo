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

	weather := newWeatherCache()
	calClient := newCalendarClient() // nil if GOOGLE_* env vars are unset
	trello := newTrelloClient()      // nil if TRELLO_* env vars are unset
	toei := newToeiCache() // Toei Shinjuku Line, via ODPT's keyless public mirror

	// JR East lines, scraped from Yahoo!路線情報 (see yahoo.go).
	jrLines := []struct {
		Name  string
		Cache *yahooLineCache
	}{
		{"JR山手線", newYahooLineCache(yahooLineYamanote)},
		{"JR上野東京ライン", newYahooLineCache(yahooLineUenoTokyo)},
		{"JR湘南新宿ライン", newYahooLineCache(yahooLineShonanShinjuku)},
		{"JR東海道線", newYahooLineCache(yahooLineTokaido)},
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

	// /api/lines is a flat, named list for the e-ink dashboard's 運行情報
	// column — combines the Keio feed (already fetched for /api/state) with
	// other operators (Toei Shinjuku, and JR East's Yamanote/Ueno-Tokyo/
	// Shonan-Shinjuku/Tokaido lines).
	mux.HandleFunc("/api/lines", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		st, err := cache.get()
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		lines := []struct {
			Name string `json:"name"`
			LineStatus
		}{
			{Name: "京王線", LineStatus: st.Service.Keio},
			{Name: "井の頭線", LineStatus: st.Service.Inokashira},
		}
		if toeiStatus, err := toei.get(); err == nil {
			lines = append(lines, struct {
				Name string `json:"name"`
				LineStatus
			}{Name: "都営新宿線", LineStatus: *toeiStatus})
		}
		for _, jr := range jrLines {
			if status, err := jr.Cache.get(); err == nil {
				lines = append(lines, struct {
					Name string `json:"name"`
					LineStatus
				}{Name: jr.Name, LineStatus: *status})
			}
		}
		json.NewEncoder(w).Encode(lines)
	})

	// /api/hatagaya: next up/down train at Hatagaya, derived from the
	// Keio feed already fetched above — no extra upstream call.
	mux.HandleFunc("/api/hatagaya", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		st, err := cache.get()
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(hatagayaNextTrains(st.Trains))
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if trello == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "trello not configured"})
			return
		}
		list, err := trello.list()
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(list)
	})

	mux.HandleFunc("PATCH /api/todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if trello == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "trello not configured"})
			return
		}
		id := r.PathValue("id")
		var body struct {
			Done bool `json:"done"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
			return
		}
		if err := trello.setDone(id, body.Done); err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /api/todos/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if trello == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "trello not configured"})
			return
		}
		if err := trello.delete(r.PathValue("id")); err != nil {
			w.WriteHeader(http.StatusBadGateway)
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
