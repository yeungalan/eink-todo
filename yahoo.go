package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// yahooLineCache fetches a JR East line's status from Yahoo!路線情報
// (transit.yahoo.co.jp/diainfo/<lineID>/0) — no key or auth needed, but it's
// an HTML scrape rather than a stable API, so treat it as best-effort.
type yahooLineCache struct {
	lineID string

	mu      sync.Mutex
	value   *LineStatus
	fetched time.Time
	ttl     time.Duration
	http    *http.Client
}

// Yahoo!路線情報 diainfo IDs for the JR East lines this dashboard tracks.
const (
	yahooLineYamanote       = "21"  // 山手線
	yahooLineUenoTokyo      = "627" // 上野東京ライン
	yahooLineShonanShinjuku = "25"  // 湘南新宿ライン
	yahooLineTokaido        = "27"  // 東海道本線（東京～熱海）
)

func newYahooLineCache(lineID string) *yahooLineCache {
	return &yahooLineCache{lineID: lineID, ttl: 2 * time.Minute, http: &http.Client{Timeout: 8 * time.Second}}
}

func (c *yahooLineCache) get() (*LineStatus, error) {
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

// mdServiceStatusRe pulls the status title, css class ("normal" when clear),
// and detail text out of Yahoo's per-line page, e.g.:
//
//	<div id="mdServiceStatus"><dl><dt><span class="icnNormalLarge"></span>平常運転</dt>
//	<dd class="normal"><p>現在､事故･遅延に関する情報はありません。</p></dd></dl></div>
//
// A disrupted line's <p> often has a trailing "(8月17日 10時10分掲載)"
// timestamp wrapped in its own <span> before the closing </p> — the detail
// capture group deliberately doesn't require an immediate </p> so it still
// matches up to that inner tag instead of failing the whole line.
var mdServiceStatusRe = regexp.MustCompile(
	`id="mdServiceStatus"><dl><dt>(?:<span[^>]*></span>)?([^<]+)</dt><dd class="([^"]+)"><p>([^<]+)`)

func (c *yahooLineCache) fetch() (*LineStatus, error) {
	url := fmt.Sprintf("https://transit.yahoo.co.jp/diainfo/%s/0", c.lineID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; eink-todo/1.0)")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	m := mdServiceStatusRe.FindSubmatch(body)
	if m == nil {
		return &LineStatus{Text: "情報取得エラー", Level: "error"}, nil
	}
	title, cssClass, detail := string(m[1]), string(m[2]), string(m[3])

	level := "delay"
	switch {
	case cssClass == "normal":
		level = "normal"
	case strings.Contains(title, "見合わせ"):
		level = "stop"
	}
	return &LineStatus{Text: title + "：" + detail, Level: level}, nil
}
