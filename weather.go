package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// Weather is the normalized shape served at /api/weather. Source: Open-Meteo
// (no API key required).
type Weather struct {
	TempC       float64 `json:"tempC"`
	Condition   string  `json:"condition"`
	HighC       float64 `json:"highC"`
	LowC        float64 `json:"lowC"`
	PrecipPct   int     `json:"precipPct"`
	HumidityPct int     `json:"humidityPct"`
	UpdatedAt   string  `json:"updatedAt"`
}

// wmoCondition maps WMO weather codes (used by Open-Meteo) to a short label.
// https://open-meteo.com/en/docs#weathervariables
func wmoCondition(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code <= 2:
		return "Mostly Sunny"
	case code == 3:
		return "Cloudy"
	case code == 45 || code == 48:
		return "Fog"
	case code >= 51 && code <= 57:
		return "Drizzle"
	case code >= 61 && code <= 67:
		return "Rain"
	case code >= 71 && code <= 77:
		return "Snow"
	case code >= 80 && code <= 82:
		return "Showers"
	case code >= 85 && code <= 86:
		return "Snow Showers"
	case code >= 95:
		return "Thunderstorm"
	default:
		return "Unknown"
	}
}

type weatherCache struct {
	mu       sync.Mutex
	lat, lon string
	value    *Weather
	fetched  time.Time
	ttl      time.Duration
}

func newWeatherCache() *weatherCache {
	lat := os.Getenv("WEATHER_LAT")
	if lat == "" {
		lat = "35.6762" // Tokyo
	}
	lon := os.Getenv("WEATHER_LON")
	if lon == "" {
		lon = "139.6503"
	}
	return &weatherCache{lat: lat, lon: lon, ttl: 10 * time.Minute}
}

func (c *weatherCache) get() (*Weather, error) {
	c.mu.Lock()
	if c.value != nil && time.Since(c.fetched) < c.ttl {
		v := c.value
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()

	w, err := c.fetch()
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
	c.value = w
	c.fetched = time.Now()
	c.mu.Unlock()
	return w, nil
}

func (c *weatherCache) fetch() (*Weather, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s"+
			"&current=temperature_2m,relative_humidity_2m,weather_code"+
			"&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max"+
			"&timezone=auto",
		c.lat, c.lon)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo: status %d", resp.StatusCode)
	}

	var raw struct {
		Current struct {
			Temperature2m       float64 `json:"temperature_2m"`
			RelativeHumidity2m  float64 `json:"relative_humidity_2m"`
			WeatherCode         int     `json:"weather_code"`
		} `json:"current"`
		Daily struct {
			Temperature2mMax           []float64 `json:"temperature_2m_max"`
			Temperature2mMin           []float64 `json:"temperature_2m_min"`
			PrecipitationProbabilityMax []float64 `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	w := &Weather{
		TempC:       round1(raw.Current.Temperature2m),
		Condition:   wmoCondition(raw.Current.WeatherCode),
		HumidityPct: int(raw.Current.RelativeHumidity2m),
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}
	if len(raw.Daily.Temperature2mMax) > 0 {
		w.HighC = round1(raw.Daily.Temperature2mMax[0])
	}
	if len(raw.Daily.Temperature2mMin) > 0 {
		w.LowC = round1(raw.Daily.Temperature2mMin[0])
	}
	if len(raw.Daily.PrecipitationProbabilityMax) > 0 {
		w.PrecipPct = int(raw.Daily.PrecipitationProbabilityMax[0])
	}
	return w, nil
}

func round1(f float64) float64 {
	v, _ := strconv.ParseFloat(fmt.Sprintf("%.1f", f), 64)
	return v
}
