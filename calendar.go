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
// demand; no interactive auth happens at runtime. The event list itself is
// also cached briefly (see today()) so repeated client polls don't each
// trigger a fresh Calendar API call.
type calendarClient struct {
	clientID     string
	clientSecret string
	refreshToken string
	calendarID   string
	cacheTTL     time.Duration

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time

	cacheMu      sync.Mutex
	cacheValue   []CalendarEvent
	cacheFetched time.Time
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
	return &calendarClient{
		clientID: id, clientSecret: secret, refreshToken: refresh, calendarID: calID,
		cacheTTL: 4 * time.Minute,
	}
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

// collectionReminderKeywords: event titles containing any of these are
// treated as trash/recycling collection reminders — see today().
var collectionReminderKeywords = []string{"ごみ収集", "資源収集"}

func isCollectionReminder(title string) bool {
	for _, kw := range collectionReminderKeywords {
		if strings.Contains(title, kw) {
			return true
		}
	}
	return false
}

// today returns cached events, refetching from Google Calendar only once
// per cacheTTL so repeated client polls don't each hit the API.
func (c *calendarClient) today() ([]CalendarEvent, error) {
	c.cacheMu.Lock()
	if c.cacheValue != nil && time.Since(c.cacheFetched) < c.cacheTTL {
		v := c.cacheValue
		c.cacheMu.Unlock()
		return v, nil
	}
	c.cacheMu.Unlock()

	events, err := c.fetchToday()
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
	c.cacheValue = events
	c.cacheFetched = time.Now()
	c.cacheMu.Unlock()
	return events, nil
}

// fetchToday hits the Calendar API for events between now and the end of
// tomorrow (the lookahead lets collection-day reminders below get pulled
// forward), then returns only today's events plus, for any ごみ収集/資源収集
// event that falls tomorrow, a same-day reminder at 23:59.
func (c *calendarClient) fetchToday() ([]CalendarEvent, error) {
	tok, err := c.token()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	todayDate := now.Format("2006-01-02")
	tomorrowDate := now.AddDate(0, 0, 1).Format("2006-01-02")
	dayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	windowEnd := dayEnd.AddDate(0, 0, 1)

	q := url.Values{
		"timeMin":      {now.Format(time.RFC3339)},
		"timeMax":      {windowEnd.Format(time.RFC3339)},
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
		"maxResults":   {"20"},
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
			title = "（タイトルなし）"
		}

		allDay := it.Start.DateTime == ""
		var itemDate, hm, startAt string
		if allDay {
			itemDate = it.Start.Date
			startAt = it.Start.Date
		} else {
			startAt = it.Start.DateTime
			if t, err := time.Parse(time.RFC3339, it.Start.DateTime); err == nil {
				itemDate = t.Local().Format("2006-01-02")
				hm = t.Format("15:04")
			}
		}

		if itemDate == tomorrowDate && isCollectionReminder(title) {
			reminderAt := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, now.Location())
			events = append(events, CalendarEvent{
				Time: "23:59", Title: "明日の" + title, StartAt: reminderAt.Format(time.RFC3339),
			})
			continue
		}
		if itemDate != todayDate {
			continue // only today's events belong here, plus the reminders shifted in above
		}
		if allDay {
			events = append(events, CalendarEvent{Title: title, AllDay: true, StartAt: startAt})
		} else {
			events = append(events, CalendarEvent{Time: hm, Title: title, StartAt: startAt})
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
