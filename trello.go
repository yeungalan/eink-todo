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
	"time"
)

// Todo is the shape served at /api/todos, backed by Trello cards.
type Todo struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Done    bool   `json:"done"`
	Due     string `json:"due,omitempty"` // formatted, e.g. "Aug 25"; absent if the card has no due date
	Overdue bool   `json:"overdue,omitempty"`

	dueAt time.Time // zero value = no due date; used for sorting only
}

// trelloClient mirrors a subset of a Trello board's todo workflow: cards in
// sourceListIDs (e.g. "Input", "Working in progress") are today's open
// todos; checking one off moves the card to doneListID. It talks straight to
// the Trello REST API using a personal API key + token (no OAuth dance).
type trelloClient struct {
	apiKey        string
	token         string
	sourceListIDs []string // first entry is where new cards + unchecked cards land
	doneListID    string
	http          *http.Client
}

func newTrelloClient() *trelloClient {
	key := os.Getenv("TRELLO_API_KEY")
	token := os.Getenv("TRELLO_TOKEN")
	sources := os.Getenv("TRELLO_SOURCE_LIST_IDS") // comma-separated
	done := os.Getenv("TRELLO_DONE_LIST_ID")
	if key == "" || token == "" || sources == "" || done == "" {
		return nil // Trello wiring disabled
	}
	return &trelloClient{
		apiKey:        key,
		token:         token,
		sourceListIDs: strings.Split(sources, ","),
		doneListID:    done,
		http:          &http.Client{Timeout: 8 * time.Second},
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

// list returns today's open todos: every card currently in the source lists,
// sorted by due date (cards with no due date sort last).
func (c *trelloClient) list() ([]Todo, error) {
	out := []Todo{}
	now := time.Now()
	for _, listID := range c.sourceListIDs {
		body, err := c.do(http.MethodGet, "/lists/"+listID+"/cards", url.Values{"fields": {"name,due"}})
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
			t := Todo{ID: c.ID, Text: c.Name}
			if c.Due != nil {
				if due, err := time.Parse(time.RFC3339, *c.Due); err == nil {
					t.dueAt = due
					t.Due = due.Local().Format("Jan 2")
					t.Overdue = due.Before(now)
				}
			}
			out = append(out, t)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		iHas, jHas := !out[i].dueAt.IsZero(), !out[j].dueAt.IsZero()
		if iHas != jHas {
			return iHas // cards with a due date sort before those without
		}
		if !iHas {
			return false // preserve original order among no-due-date cards
		}
		return out[i].dueAt.Before(out[j].dueAt)
	})
	return out, nil
}

// setDone moves a card to the Done list when checked, or back to the inbox
// list when unchecked.
func (c *trelloClient) setDone(cardID string, done bool) error {
	target := c.sourceListIDs[0]
	if done {
		target = c.doneListID
	}
	_, err := c.do(http.MethodPut, "/cards/"+cardID, url.Values{"idList": {target}})
	return err
}

// delete archives (closes) the card rather than permanently deleting it.
func (c *trelloClient) delete(cardID string) error {
	_, err := c.do(http.MethodPut, "/cards/"+cardID, url.Values{"closed": {"true"}})
	return err
}
