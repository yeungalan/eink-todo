package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// CalendarEvent is the normalized shape served at /api/events.
type CalendarEvent struct {
	Time    string `json:"time"` // "HH:MM", empty for all-day
	Title   string `json:"title"`
	AllDay  bool   `json:"allDay"`
	StartAt string `json:"startAt"` // RFC3339, for sorting/debug
}

// calendarClient talks to the Google Calendar API using a long-lived refresh
// token (obtained once via a manual OAuth consent flow — see README). It
// keeps a short-lived access token cached in memory and refreshes it on
// demand; no interactive auth happens at runtime.
type calendarClient struct {
	clientID     string
	clientSecret string
	refreshToken string
	calendarID   string

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

func newCalendarClient() *calendarClient {
	id := os.Getenv("GOOGLE_CLIENT_ID")
	secret := os.Getenv("GOOGLE_CLIENT_SECRET")
	refresh := os.Getenv("GOOGLE_REFRESH_TOKEN")
	if id == "" || secret == "" || refresh == "" {
		return nil // calendar wiring disabled
	}
	calID := os.Getenv("GOOGLE_CALENDAR_ID")
	if calID == "" {
		calID = "primary"
	}
	return &calendarClient{clientID: id, clientSecret: secret, refreshToken: refresh, calendarID: calID}
}

func (c *calendarClient) token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.expiresAt) {
		return c.accessToken, nil
	}

	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {c.refreshToken},
		"grant_type":    {"refresh_token"},
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token refresh: status %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	c.accessToken = out.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(out.ExpiresIn-30) * time.Second)
	return c.accessToken, nil
}

// today fetches events on the calendar between now and the end of the
// local day, sorted by start time.
func (c *calendarClient) today() ([]CalendarEvent, error) {
	tok, err := c.token()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	dayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())

	q := url.Values{
		"timeMin":      {now.Format(time.RFC3339)},
		"timeMax":      {dayEnd.Format(time.RFC3339)},
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
		"maxResults":   {"10"},
	}
	reqURL := "https://www.googleapis.com/calendar/v3/calendars/" +
		url.PathEscape(c.calendarID) + "/events?" + q.Encode()

	req, _ := http.NewRequest(http.MethodGet, reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("calendar list: status %d", resp.StatusCode)
	}

	var raw struct {
		Items []struct {
			Summary string `json:"summary"`
			Start   struct {
				DateTime string `json:"dateTime"`
				Date     string `json:"date"`
			} `json:"start"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	events := make([]CalendarEvent, 0, len(raw.Items))
	for _, it := range raw.Items {
		title := it.Summary
		if title == "" {
			title = "(no title)"
		}
		if it.Start.DateTime != "" {
			t, err := time.Parse(time.RFC3339, it.Start.DateTime)
			startAt := it.Start.DateTime
			hm := ""
			if err == nil {
				hm = t.Format("15:04")
			}
			events = append(events, CalendarEvent{Time: hm, Title: title, StartAt: startAt})
		} else {
			events = append(events, CalendarEvent{Title: title, AllDay: true, StartAt: it.Start.Date})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].AllDay != events[j].AllDay {
			return events[i].AllDay // all-day events first
		}
		return strings.Compare(events[i].StartAt, events[j].StartAt) < 0
	})
	return events, nil
}
