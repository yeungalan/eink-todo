package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Todo is the shape served at /api/todos, backed by Trello cards.
type Todo struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
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
// in board order, list by list.
func (c *trelloClient) list() ([]Todo, error) {
	out := []Todo{}
	for _, listID := range c.sourceListIDs {
		body, err := c.do(http.MethodGet, "/lists/"+listID+"/cards", url.Values{"fields": {"name"}})
		if err != nil {
			return nil, err
		}
		var cards []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &cards); err != nil {
			return nil, err
		}
		for _, c := range cards {
			out = append(out, Todo{ID: c.ID, Text: c.Name, Done: false})
		}
	}
	return out, nil
}

// create adds a new card to the first source list (the "inbox").
func (c *trelloClient) create(text string) (Todo, error) {
	body, err := c.do(http.MethodPost, "/cards", url.Values{
		"idList": {c.sourceListIDs[0]},
		"name":   {text},
	})
	if err != nil {
		return Todo{}, err
	}
	var card struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &card); err != nil {
		return Todo{}, err
	}
	return Todo{ID: card.ID, Text: card.Name, Done: false}, nil
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
