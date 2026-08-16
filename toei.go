package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// toeiCache fetches Toei Shinjuku Line status from ODPT's public,
// no-API-key mirror (api-public.odpt.org) — unlike the main ODPT API, this
// endpoint needs no consumer key, but only covers Toei's own lines.
type toeiCache struct {
	mu      sync.Mutex
	value   *LineStatus
	fetched time.Time
	ttl     time.Duration
	http    *http.Client
}

func newToeiCache() *toeiCache {
	return &toeiCache{ttl: 2 * time.Minute, http: &http.Client{Timeout: 8 * time.Second}}
}

func (c *toeiCache) get() (*LineStatus, error) {
	c.mu.Lock()
	if c.value != nil && time.Since(c.fetched) < c.ttl {
		v := c.value
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()

	s, err := c.fetch()
	if err != nil {
		c.mu.Lock()
		if c.value != nil {
			v := c.value
			c.mu.Unlock()
			return v, nil // serve stale on upstream error
		}
		c.mu.Unlock()
		return nil, err
	}

	c.mu.Lock()
	c.value = s
	c.fetched = time.Now()
	c.mu.Unlock()
	return s, nil
}

func (c *toeiCache) fetch() (*LineStatus, error) {
	const url = "https://api-public.odpt.org/api/v4/odpt:TrainInformation?odpt:operator=odpt.Operator:Toei"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var items []struct {
		Railway string `json:"odpt:railway"`
		Text    struct {
			Ja string `json:"ja"`
		} `json:"odpt:trainInformationText"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	for _, it := range items {
		if it.Railway != "odpt.Railway:Toei.Shinjuku" {
			continue
		}
		text := it.Text.Ja
		level := "normal"
		switch {
		case strings.Contains(text, "見合わせ"):
			level = "stop"
		case !strings.Contains(text, "遅延はありません"):
			level = "delay"
		}
		return &LineStatus{Text: text, Level: level}, nil
	}
	return &LineStatus{Text: "情報なし", Level: "error"}, nil
}
