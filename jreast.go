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

// jrKantoCache fetches JR East's Kanto-area line-status overview
// (traininfo.jreast.co.jp/train_info/kanto.aspx) once per ttl and serves
// every tracked line's status from that single shared page, keyed by the
// "lineid" slug JR's own markup uses (e.g. "yamanoteline"). This page gives
// JR's real 5-way classification (normal/delay/adjust/info/stopdirect) —
// the same one their own status icons encode — rather than the coarser
// normal/trouble split on a per-line diainfo page.
type jrKantoCache struct {
	mu      sync.Mutex
	value   map[string]LineStatus
	fetched time.Time
	ttl     time.Duration
	http    *http.Client
}

// JR East line slugs (the "lineid=" query param in kanto.aspx's per-line
// links) for the lines this dashboard tracks.
const (
	jrLineYamanote       = "yamanoteline"
	jrLineUenoTokyo      = "ueno-tokyoline"
	jrLineShonanShinjuku = "shonan-shinjukuline"
	jrLineTokaido        = "tokaidoline"
)

func newJRKantoCache() *jrKantoCache {
	return &jrKantoCache{ttl: 2 * time.Minute, http: &http.Client{Timeout: 8 * time.Second}}
}

// get returns the given line's status, or a synthetic "no info" status if
// that slug wasn't found on the page (rather than erroring the whole
// dashboard over one line JR happens to not be listing right now).
func (c *jrKantoCache) get(slug string) (*LineStatus, error) {
	m, err := c.snapshot()
	if err != nil {
		return nil, err
	}
	if s, ok := m[slug]; ok {
		return &s, nil
	}
	return &LineStatus{Text: "情報なし", Level: "error"}, nil
}

func (c *jrKantoCache) snapshot() (map[string]LineStatus, error) {
	c.mu.Lock()
	if c.value != nil && time.Since(c.fetched) < c.ttl {
		v := c.value
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()

	m, err := c.fetch()
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
	c.value = m
	c.fetched = time.Now()
	c.mu.Unlock()
	return m, nil
}

// jrRouteRe pulls (line slug, status level, raw label HTML) triples out of
// kanto.aspx's route listing, e.g.:
//
//	<a href="/train_info/line.aspx?gid=1&lineid=ueno-tokyoline" class="traininfo-routes__info">
//	    <p class="traininfo-routes__status stopdirect">
//	        <span>直通運転中止</span>
//	    </p>
//
// Anchored on "lineid=" rather than the line-color badge span, since major
// lines (Yamanote, Ueno-Tokyo, ...) render a two-letter icon badge instead
// of a plain color one, but every entry links to line.aspx with its slug.
//
// The label group captures everything up to </p> rather than requiring a
// flat <span>text</span>: a line with an estimated resumption time nests a
// second <span> around the clock time (e.g. <span><span>11時20分頃</span>
// 運転再開見込</span>), which a stricter [^<]+ group fails to match — and
// since this whole pattern is unanchored, FindAll doesn't just miss that
// line, it keeps scanning past it and silently attaches the *next* line's
// unrelated status. See htmlTagRe below for turning this raw HTML into text.
var jrRouteRe = regexp.MustCompile(
	`(?s)lineid=([a-z0-9-]+)".*?traininfo-routes__status ([a-z]+)">(.*?)</p>`)

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var whitespaceRunRe = regexp.MustCompile(`\s+`)

// stripTags turns a fragment of inner HTML (possibly with nested tags, e.g.
// a resumption-time <span> inside the status <span>) into flat text,
// preserving a word boundary where a tag used to be rather than mashing
// adjacent text together.
func stripTags(html string) string {
	return strings.TrimSpace(whitespaceRunRe.ReplaceAllString(htmlTagRe.ReplaceAllString(html, " "), " "))
}

func (c *jrKantoCache) fetch() (map[string]LineStatus, error) {
	const url = "https://traininfo.jreast.co.jp/train_info/kanto.aspx"
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

	out := map[string]LineStatus{}
	for _, m := range jrRouteRe.FindAllSubmatch(body, -1) {
		slug, level, text := string(m[1]), string(m[2]), stripTags(string(m[3]))
		if _, exists := out[slug]; exists {
			continue // first occurrence wins (a line can be listed more than once, e.g. by section)
		}
		out[slug] = LineStatus{Text: text, Level: level}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("jreast kanto.aspx: no routes parsed (page layout may have changed)")
	}
	return out, nil
}
