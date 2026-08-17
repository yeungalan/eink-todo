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
	FeelsLikeC  float64 `json:"feelsLikeC"`
	Condition   string  `json:"condition"`
	HighC       float64 `json:"highC"`
	LowC        float64 `json:"lowC"`
	PrecipPct   int     `json:"precipPct"`
	HumidityPct int     `json:"humidityPct"`
	WindKph     float64 `json:"windKph"`
	WindDir     string  `json:"windDir"`
	UVIndex     float64 `json:"uvIndex"`
	Sunrise     string  `json:"sunrise"`
	Sunset      string  `json:"sunset"`
	Icon        string  `json:"icon"` // clear | partly-cloudy | cloudy | fog | rain | snow | thunder
	UpdatedAt   string  `json:"updatedAt"`
}

// compassDir converts a wind direction in degrees to a 16-point compass label.
func compassDir(deg float64) string {
	dirs := []string{"北", "北北東", "北東", "東北東", "東", "東南東", "南東", "南南東",
		"南", "南南西", "南西", "西南西", "西", "西北西", "北西", "北北西"}
	idx := int((deg/22.5)+0.5) % 16
	if idx < 0 {
		idx += 16
	}
	return dirs[idx]
}

// wmoCondition maps WMO weather codes (used by Open-Meteo) to a short label.
// https://open-meteo.com/en/docs#weathervariables
func wmoCondition(code int) string {
	switch {
	case code == 0:
		return "快晴"
	case code <= 2:
		return "ほぼ晴れ"
	case code == 3:
		return "曇り"
	case code == 45 || code == 48:
		return "霧"
	case code >= 51 && code <= 57:
		return "霧雨"
	case code >= 61 && code <= 67:
		return "雨"
	case code >= 71 && code <= 77:
		return "雪"
	case code >= 80 && code <= 82:
		return "にわか雨"
	case code >= 85 && code <= 86:
		return "にわか雪"
	case code >= 95:
		return "雷雨"
	default:
		return "不明"
	}
}

// wmoIcon maps a WMO weather code to one of the icon keys the dashboard's
// weather panel knows how to draw (see weatherIconMarkup in dashboard.html).
func wmoIcon(code int) string {
	switch {
	case code == 0:
		return "clear"
	case code <= 2:
		return "partly-cloudy"
	case code == 3:
		return "cloudy"
	case code == 45 || code == 48:
		return "fog"
	case code >= 51 && code <= 57:
		return "rain"
	case code >= 61 && code <= 67:
		return "rain"
	case code >= 71 && code <= 77:
		return "snow"
	case code >= 80 && code <= 82:
		return "rain"
	case code >= 85 && code <= 86:
		return "snow"
	case code >= 95:
		return "thunder"
	default:
		return "cloudy"
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
			"&current=temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m,wind_direction_10m"+
			"&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max,uv_index_max,sunrise,sunset"+
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
			ApparentTemperature float64 `json:"apparent_temperature"`
			RelativeHumidity2m  float64 `json:"relative_humidity_2m"`
			WeatherCode         int     `json:"weather_code"`
			WindSpeed10m        float64 `json:"wind_speed_10m"`
			WindDirection10m    float64 `json:"wind_direction_10m"`
		} `json:"current"`
		Daily struct {
			Temperature2mMax            []float64 `json:"temperature_2m_max"`
			Temperature2mMin            []float64 `json:"temperature_2m_min"`
			PrecipitationProbabilityMax []float64 `json:"precipitation_probability_max"`
			UVIndexMax                  []float64 `json:"uv_index_max"`
			Sunrise                     []string  `json:"sunrise"`
			Sunset                      []string  `json:"sunset"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	w := &Weather{
		TempC:       round1(raw.Current.Temperature2m),
		FeelsLikeC:  round1(raw.Current.ApparentTemperature),
		Condition:   wmoCondition(raw.Current.WeatherCode),
		Icon:        wmoIcon(raw.Current.WeatherCode),
		HumidityPct: int(raw.Current.RelativeHumidity2m),
		WindKph:     round1(raw.Current.WindSpeed10m),
		WindDir:     compassDir(raw.Current.WindDirection10m),
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
	if len(raw.Daily.UVIndexMax) > 0 {
		w.UVIndex = raw.Daily.UVIndexMax[0]
	}
	if len(raw.Daily.Sunrise) > 0 {
		w.Sunrise = formatClock(raw.Daily.Sunrise[0])
	}
	if len(raw.Daily.Sunset) > 0 {
		w.Sunset = formatClock(raw.Daily.Sunset[0])
	}
	return w, nil
}

// formatClock turns Open-Meteo's "2026-08-17T05:03" into 24h "5:03".
func formatClock(iso string) string {
	t, err := time.Parse("2006-01-02T15:04", iso)
	if err != nil {
		return ""
	}
	return t.Format("15:04")
}

func round1(f float64) float64 {
	v, _ := strconv.ParseFloat(fmt.Sprintf("%.1f", f), 64)
	return v
}
