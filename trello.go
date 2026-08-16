package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Todo is the shape served at /api/todos, backed by Trello cards.
type Todo struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Done    bool   `json:"done"`
	List    string `json:"list"`          // source list name, e.g. "WIP"
	Due     string `json:"due,omitempty"` // formatted, e.g. "Aug 25"; absent if the card has no due date
	Overdue bool   `json:"overdue,omitempty"`

	dueAt        time.Time // zero value = no due date; used for sorting only
	listPriority int       // lower = higher priority; used for sorting only
}

type namedList struct {
	ID       string
	Name     string
	Priority int // lower = higher priority (shown first)
}

// trelloClient mirrors a subset of a Trello board's todo workflow: cards in
// sourceLists (Verification, WIP, Researching, Waiting, Input, in priority
// order) are today's open todos; checking one off moves the card to
// doneListID. It talks straight to the Trello REST API using a personal API
// key + token (no OAuth dance). Fetching a list means one Trello API call
// per source list, so the result is cached briefly (see list()) — without
// it, every dashboard poll (and every tap) would fan out to 5 upstream
// calls each.
type trelloClient struct {
	apiKey      string
	token       string
	sourceLists []namedList // ordered highest priority first
	doneListID  string
	http        *http.Client
	cacheTTL    time.Duration

	cacheMu      sync.Mutex
	cacheValue   []Todo
	cacheFetched time.Time
}

func newTrelloClient() *trelloClient {
	key := os.Getenv("TRELLO_API_KEY")
	token := os.Getenv("TRELLO_TOKEN")
	sources := os.Getenv("TRELLO_SOURCE_LISTS") // "id:Name,id:Name,..." in priority order, highest first
	done := os.Getenv("TRELLO_DONE_LIST_ID")
	if key == "" || token == "" || sources == "" || done == "" {
		return nil // Trello wiring disabled
	}
	var lists []namedList
	for i, pair := range strings.Split(sources, ",") {
		idName := strings.SplitN(pair, ":", 2)
		if len(idName) != 2 {
			continue
		}
		lists = append(lists, namedList{ID: idName[0], Name: idName[1], Priority: i})
	}
	return &trelloClient{
		apiKey:      key,
		token:       token,
		sourceLists: lists,
		doneListID:  done,
		http:        &http.Client{Timeout: 8 * time.Second},
		cacheTTL:    90 * time.Second,
	}
}

func (c *trelloClient) auth(q url.Values) url.Values {
	if q == nil {
		q = url.Values{}
	}
	q.Set("key", c.apiKey)
	q.Set("token", c.token)
	return q
}

func (c *trelloClient) do(method, path string, q url.Values) ([]byte, error) {
	reqURL := "https://api.trello.com/1" + path + "?" + c.auth(q).Encode()
	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("trello %s %s: status %d: %s", method, path, resp.StatusCode, string(body))
	}
	return body, nil
}

// list returns cached todos, refetching from Trello only once per cacheTTL
// so repeated client polls (and rapid taps) don't each fan out to one
// Trello API call per source list.
func (c *trelloClient) list() ([]Todo, error) {
	c.cacheMu.Lock()
	if c.cacheValue != nil && time.Since(c.cacheFetched) < c.cacheTTL {
		v := c.cacheValue
		c.cacheMu.Unlock()
		return v, nil
	}
	c.cacheMu.Unlock()

	todos, err := c.fetchList()
	if err != nil {
		c.cacheMu.Lock()
		if c.cacheValue != nil {
			v := c.cacheValue
			c.cacheMu.Unlock()
			return v, nil // serve stale on upstream error
		}
		c.cacheMu.Unlock()
		return nil, err
	}

	c.cacheMu.Lock()
	c.cacheValue = todos
	c.cacheFetched = time.Now()
	c.cacheMu.Unlock()
	return todos, nil
}

// invalidateCache forces the next list() call to hit Trello again — used
// after a mutation (setDone/delete) so the change is visible immediately
// rather than waiting out cacheTTL.
func (c *trelloClient) invalidateCache() {
	c.cacheMu.Lock()
	c.cacheValue = nil
	c.cacheMu.Unlock()
}

// fetchList hits Trello for every card currently in the source lists,
// sorted so overdue cards come first, then cards with a due date (real or,
// for Verification/WIP, an assumed one-week default) ahead of undated
// cards, then by list priority (Verification, WIP, Researching, Waiting,
// Input), then by due date ascending.
func (c *trelloClient) fetchList() ([]Todo, error) {
	out := []Todo{}
	now := time.Now()
	for _, l := range c.sourceLists {
		body, err := c.do(http.MethodGet, "/lists/"+l.ID+"/cards", url.Values{"fields": {"name,due"}})
		if err != nil {
			return nil, err
		}
		var cards []struct {
			ID   string  `json:"id"`
			Name string  `json:"name"`
			Due  *string `json:"due"`
		}
		if err := json.Unmarshal(body, &cards); err != nil {
			return nil, err
		}
		for _, c := range cards {
			t := Todo{ID: c.ID, Text: c.Name, List: l.Name, listPriority: l.Priority}
			if c.Due != nil {
				if due, err := time.Parse(time.RFC3339, *c.Due); err == nil {
					t.dueAt = due
					t.Due = relativeDate(due.Local(), now)
					t.Overdue = due.Before(now)
				}
			} else if l.Priority < 2 {
				// Verification/WIP (the two highest-priority lists) are
				// assumed due in a week when no explicit deadline is set,
				// so they still surface ahead of undated backlog items —
				// this is sort-only, not a real due date, so it's not
				// shown on the card and never counts as overdue.
				t.dueAt = now.AddDate(0, 0, 7)
			}
			out = append(out, t)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Overdue != b.Overdue {
			return a.Overdue // overdue cards first, regardless of list
		}
		aHas, bHas := !a.dueAt.IsZero(), !b.dueAt.IsZero()
		if aHas != bHas {
			return aHas // cards with a due date (real or assumed) sort before those without
		}
		if a.listPriority != b.listPriority {
			return a.listPriority < b.listPriority // Verification, WIP, Researching, Waiting, Input
		}
		if !aHas {
			return false // preserve original order among no-due-date cards
		}
		return a.dueAt.Before(b.dueAt)
	})
	return out, nil
}

// setDone moves a card to the Done list when checked, or back to the lowest-
// priority source list (Input) when unchecked.
func (c *trelloClient) setDone(cardID string, done bool) error {
	target := c.doneListID
	if !done && len(c.sourceLists) > 0 {
		target = c.sourceLists[len(c.sourceLists)-1].ID
	}
	_, err := c.do(http.MethodPut, "/cards/"+cardID, url.Values{"idList": {target}})
	if err == nil {
		c.invalidateCache()
	}
	return err
}

// delete archives (closes) the card rather than permanently deleting it.
func (c *trelloClient) delete(cardID string) error {
	_, err := c.do(http.MethodPut, "/cards/"+cardID, url.Values{"closed": {"true"}})
	if err == nil {
		c.invalidateCache()
	}
	return err
}

// relativeDate renders a due date the way day.js's relative-time plugin
// would (moment's modern, lighter alternative): 今日/明日/昨日 for the
// adjacent days, "N日後"/"N日前" within a week, and an absolute date
// beyond that where a relative label stops being useful at a glance.
func relativeDate(due, now time.Time) string {
	dueDay := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, due.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	days := int(dueDay.Sub(today).Hours() / 24)

	switch {
	case days == 0:
		return "今日"
	case days == 1:
		return "明日"
	case days == -1:
		return "昨日"
	case days > 1 && days <= 7:
		return fmt.Sprintf("%d日後", days)
	case days < -1 && days >= -7:
		return fmt.Sprintf("%d日前", -days)
	default:
		return fmt.Sprintf("%d月%d日", due.Month(), due.Day())
	}
}
