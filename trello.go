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
// key + token (no OAuth dance).
type trelloClient struct {
	apiKey      string
	token       string
	sourceLists []namedList // ordered highest priority first
	doneListID  string
	http        *http.Client
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

// list returns today's open todos: every card currently in the source
// lists, sorted so overdue cards come first, then by list priority
// (Verification, WIP, Researching, Waiting, Input), then by due date
// (cards with no due date sort last within their list).
func (c *trelloClient) list() ([]Todo, error) {
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
					local := due.Local()
					t.Due = fmt.Sprintf("%d月%d日", local.Month(), local.Day())
					t.Overdue = due.Before(now)
				}
			}
			out = append(out, t)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Overdue != b.Overdue {
			return a.Overdue // overdue cards first, regardless of list
		}
		if a.listPriority != b.listPriority {
			return a.listPriority < b.listPriority // Verification, WIP, Researching, Waiting, Input
		}
		aHas, bHas := !a.dueAt.IsZero(), !b.dueAt.IsZero()
		if aHas != bHas {
			return aHas // cards with a due date sort before those without
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
	return err
}

// delete archives (closes) the card rather than permanently deleting it.
func (c *trelloClient) delete(cardID string) error {
	_, err := c.do(http.MethodPut, "/cards/"+cardID, url.Values{"closed": {"true"}})
	return err
}
