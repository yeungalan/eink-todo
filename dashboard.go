package main

// Dashboard data sources that sit behind the Go backend as proxies/scrapers:
//
//   - weather : Open-Meteo current+daily for Shibuya, Tokyo, mapped onto the
//     Erik Flowers weather-icons set (github.com/erikflowers/weather-icons).
//   - 運行情報: the Yahoo! transit operation-info board for area 4 (Kanto),
//     https://transit.yahoo.co.jp/diainfo/area/4, scraped and reduced to the
//     handful of lines that matter for a 幡ヶ谷 resident.
//   - next train: 幡ヶ谷 → 新線新宿 upbound trains lifted from the live Keio feed.
//
// The browser only ever talks to this app, so it never has to deal with the
// upstreams' CORS / mixed-content / HTML-shape quirks.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Shibuya, Tokyo (渋谷). Open-Meteo snaps to the nearest grid cell.
const (
	shibuyaLat = 35.6595
	shibuyaLon = 139.7005
	weatherLoc = "Shibuya, Tokyo"
)

// jst is the reference clock for everything the dashboard shows — the weather
// station, the trains and the reload schedule are all Tokyo-local.
var jst = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return time.FixedZone("JST", 9*3600)
	}
	return loc
}()

func nowJST() time.Time { return time.Now().In(jst) }

// ---------------- Weather ----------------

type openMeteo struct {
	Current struct {
		Time     string  `json:"time"`
		Temp     float64 `json:"temperature_2m"`
		Humidity int     `json:"relative_humidity_2m"`
		Feels    float64 `json:"apparent_temperature"`
		IsDay    int     `json:"is_day"`
		Code     int     `json:"weather_code"`
		Wind     float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Daily struct {
		Max        []float64 `json:"temperature_2m_max"`
		Min        []float64 `json:"temperature_2m_min"`
		PrecipProb []int     `json:"precipitation_probability_max"`
	} `json:"daily"`
}

// wmo maps a WMO weather-interpretation code to labels + a base weather-icons
// name. The base is decorated with a day/night variant by iconFor.
type wmoInfo struct {
	en, ja, day, night string
}

var wmoTable = map[int]wmoInfo{
	0:  {"Clear", "快晴", "wi-day-sunny", "wi-night-clear"},
	1:  {"Mainly Clear", "晴れ", "wi-day-sunny-overcast", "wi-night-alt-cloudy"},
	2:  {"Partly Cloudy", "晴れ時々曇り", "wi-day-cloudy", "wi-night-alt-cloudy"},
	3:  {"Overcast", "曇り", "wi-cloudy", "wi-cloudy"},
	45: {"Fog", "霧", "wi-day-fog", "wi-fog"},
	48: {"Rime Fog", "霧氷", "wi-day-fog", "wi-fog"},
	51: {"Light Drizzle", "弱い霧雨", "wi-sprinkle", "wi-sprinkle"},
	53: {"Drizzle", "霧雨", "wi-sprinkle", "wi-sprinkle"},
	55: {"Heavy Drizzle", "強い霧雨", "wi-sprinkle", "wi-sprinkle"},
	56: {"Freezing Drizzle", "着氷性の霧雨", "wi-sleet", "wi-sleet"},
	57: {"Freezing Drizzle", "着氷性の霧雨", "wi-sleet", "wi-sleet"},
	61: {"Light Rain", "小雨", "wi-rain", "wi-rain"},
	63: {"Rain", "雨", "wi-rain", "wi-rain"},
	65: {"Heavy Rain", "大雨", "wi-rain", "wi-rain"},
	66: {"Freezing Rain", "着氷性の雨", "wi-rain-mix", "wi-rain-mix"},
	67: {"Freezing Rain", "着氷性の雨", "wi-rain-mix", "wi-rain-mix"},
	71: {"Light Snow", "小雪", "wi-snow", "wi-snow"},
	73: {"Snow", "雪", "wi-snow", "wi-snow"},
	75: {"Heavy Snow", "大雪", "wi-snow", "wi-snow"},
	77: {"Snow Grains", "細氷", "wi-snow", "wi-snow"},
	80: {"Light Showers", "にわか雨", "wi-day-showers", "wi-showers"},
	81: {"Showers", "にわか雨", "wi-day-showers", "wi-showers"},
	82: {"Heavy Showers", "激しいにわか雨", "wi-showers", "wi-showers"},
	85: {"Snow Showers", "にわか雪", "wi-snow", "wi-snow"},
	86: {"Snow Showers", "にわか雪", "wi-snow", "wi-snow"},
	95: {"Thunderstorm", "雷雨", "wi-day-thunderstorm", "wi-thunderstorm"},
	96: {"Thunderstorm w/ Hail", "雹を伴う雷雨", "wi-thunderstorm", "wi-thunderstorm"},
	99: {"Thunderstorm w/ Hail", "雹を伴う雷雨", "wi-thunderstorm", "wi-thunderstorm"},
}

func iconFor(code int, isDay bool) (icon, en, ja string) {
	info, ok := wmoTable[code]
	if !ok {
		return "wi-na", "Unknown", "不明"
	}
	if isDay {
		return info.day, info.en, info.ja
	}
	return info.night, info.en, info.ja
}

func (c *Client) fetchWeather() (*Weather, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&current=temperature_2m,relative_humidity_2m,apparent_temperature,is_day,weather_code,wind_speed_10m"+
			"&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max"+
			"&timezone=Asia%%2FTokyo&forecast_days=1",
		shibuyaLat, shibuyaLon)

	body, err := c.getPlain(url)
	if err != nil {
		return nil, err
	}
	var om openMeteo
	if err := json.Unmarshal(body, &om); err != nil {
		return nil, fmt.Errorf("decode weather: %w", err)
	}

	isDay := om.Current.IsDay == 1
	icon, en, ja := iconFor(om.Current.Code, isDay)

	w := &Weather{
		Location:    weatherLoc,
		TempC:       om.Current.Temp,
		FeelsC:      om.Current.Feels,
		Code:        om.Current.Code,
		IsDay:       isDay,
		Condition:   en,
		ConditionJa: ja,
		Icon:        icon,
		Humidity:    om.Current.Humidity,
		WindKmh:     om.Current.Wind,
		ObservedAt:  shortTime(om.Current.Time),
		FetchedAt:   nowJST().Format("15:04"),
	}
	if len(om.Daily.Max) > 0 {
		w.HighC = om.Daily.Max[0]
	}
	if len(om.Daily.Min) > 0 {
		w.LowC = om.Daily.Min[0]
	}
	if len(om.Daily.PrecipProb) > 0 {
		w.PrecipProb = om.Daily.PrecipProb[0]
	}
	return w, nil
}

// shortTime turns an Open-Meteo ISO local time ("2026-07-10T10:15") into "10:15".
func shortTime(iso string) string {
	if i := strings.IndexByte(iso, 'T'); i >= 0 && len(iso) >= i+6 {
		return iso[i+1 : i+6]
	}
	return iso
}

// ---------------- 運行情報 (Yahoo transit) ----------------

const yahooDiainfoURL = "https://transit.yahoo.co.jp/diainfo/area/4"

// Lines we surface, in display order. Scoped to the ones the user rides:
// 京王新線 ⟷ 都営新宿線, plus the JR 山手線.
var wantedLines = []string{
	"/diainfo/103/0", // 京王新線
	"/diainfo/130/0", // 都営新宿線
	"/diainfo/21/0",  // JR 山手線
}

var (
	rowRe = regexp.MustCompile(`<tr><td><a href="(/diainfo/\d+/0)">([^<]+)</a></td><td>(.*?)</td><td>(.*?)</td></tr>`)
	tagRe = regexp.MustCompile(`<[^>]+>`)
)

func (c *Client) fetchDiainfo() (*Diainfo, error) {
	body, err := c.getPlain(yahooDiainfoURL)
	if err != nil {
		return nil, err
	}
	byHref := map[string]LineInfo{}
	for _, m := range rowRe.FindAllStringSubmatch(string(body), -1) {
		href, name := m[1], stripTags(m[2])
		status := stripTags(m[3])
		detail := stripTags(m[4])
		if _, seen := byHref[href]; seen {
			continue
		}
		level, icon := classifyStatus(status)
		byHref[href] = LineInfo{
			Name: name, Href: href, Status: status,
			Detail: detail, Level: level, Icon: icon,
		}
	}

	out := &Diainfo{
		UpdatedAt: nowJST().Format("15:04"),
		Source:    "live",
	}
	for _, href := range wantedLines {
		if li, ok := byHref[href]; ok {
			out.Lines = append(out.Lines, li)
		}
	}
	if len(out.Lines) == 0 {
		return nil, fmt.Errorf("no matching lines parsed from Yahoo transit")
	}
	return out, nil
}

// classifyStatus maps a Japanese status phrase onto a severity level and the
// base name of the icon under web/img/ (normal / info / adjust).
func classifyStatus(status string) (level, icon string) {
	switch {
	case strings.Contains(status, "見合"):
		return "suspended", "adjust"
	case strings.Contains(status, "遅延"), strings.Contains(status, "遅れ"):
		return "delay", "info"
	case strings.Contains(status, "平常"):
		return "normal", "normal"
	case status == "":
		return "normal", "normal"
	default:
		return "info", "info"
	}
}

func stripTags(s string) string {
	return strings.TrimSpace(tagRe.ReplaceAllString(s, ""))
}

// ---------------- Next train: 幡ヶ谷 → 新線新宿 ----------------

const (
	fromStation = "幡ヶ谷"
	toStation   = "新線新宿"
	// Rough dwell+run time between adjacent stations on this closely-spaced
	// corridor. There is no public Keio timetable feed, so ETAs are estimated
	// from live position — surfaced as 約N分 in the UI.
	perStopMin  = 2.0
	maxStopsFar = 8.0 // don't list trains further back than this
)

// nextTrains reads the already-normalized live state and pulls the upbound
// (新宿方面) trains heading for 幡ヶ谷, estimating each one's ETA from its live
// position so the panel can answer "how long until the next few trains".
//
// Only 京王新線 local service actually calls at 幡ヶ谷: a train reaches it either
// already on the 新線 (笹塚→幡ヶ谷) or from the main line bound for the 都営新宿線
// corridor (…→代田橋→笹塚→[新線]→幡ヶ谷). Main-line trains to 京王線新宿 skip the
// 新線 entirely and are excluded by destination.
func nextTrains(st *State) *NextTrainInfo {
	info := &NextTrainInfo{
		From:      fromStation,
		To:        toStation,
		FetchedAt: nowJST().Format("15:04:05"),
	}
	if st == nil {
		info.Note = "運行情報を取得できませんでした"
		return info
	}

	type scored struct {
		nt   NextTrain
		dist float64
	}
	var cand []scored
	for _, t := range st.Trains {
		if t.Direction != "up" {
			continue
		}
		dist, ok := distToHatagaya(t)
		if !ok {
			continue
		}
		eta := int(dist*perStopMin+0.5) + t.DelayMin
		cand = append(cand, scored{
			dist: dist,
			nt: NextTrain{
				TrainNo:     t.TrainNo,
				TypeName:    t.TypeName,
				TypeIcon:    t.TypeIcon,
				Color:       t.Color,
				TextOnColor: t.TextOnColor,
				Destination: t.Destination,
				LocName:     t.LocName,
				AtStation:   t.AtStation,
				DelayMin:    t.DelayMin,
				StopsAway:   int(dist + 0.5),
				EtaMin:      eta,
			},
		})
	}

	// Nearest first, then keep the next 3.
	for i := 1; i < len(cand); i++ {
		for j := i; j > 0 && cand[j].dist < cand[j-1].dist; j-- {
			cand[j], cand[j-1] = cand[j-1], cand[j]
		}
	}
	if len(cand) > 3 {
		cand = cand[:3]
	}
	for _, s := range cand {
		info.Trains = append(info.Trains, s.nt)
	}
	if len(info.Trains) == 0 {
		info.Note = "接近中の上り列車はありません"
	}
	return info
}

// distToHatagaya returns the distance (in station-stops) an upbound train is
// from serving 幡ヶ谷, and whether it belongs on this panel. Fractional values
// come from between-station positions.
func distToHatagaya(t Train) (float64, bool) {
	switch t.Branch {
	case "shinsen":
		// shinsen indices: …新線新宿(3) 初台(4) 幡ヶ谷(5) 笹塚(6); up = toward 0.
		// A train in the 笹塚〜幡ヶ谷 gap sits at pos 5..6; 幡ヶ谷 itself is 5.
		d := t.Pos - 5
		if d < -0.05 || d > 1.05 {
			return 0, false // already past 幡ヶ谷 (初台 side) or not adjacent
		}
		if d < 0 {
			d = 0
		}
		return d, true
	case "main":
		// main indices: 新宿(0) 笹塚(1) 代田橋(2) …; up = toward 0. Distance to
		// 幡ヶ谷 ≈ pos (pos−1 stops to reach 笹塚, +1 to branch onto 幡ヶ谷).
		if t.Pos < 0.95 {
			return 0, false // past 笹塚 toward 京王線新宿 — off our corridor
		}
		if t.Pos > maxStopsFar {
			return 0, false
		}
		if !corridorBound(t.Destination) {
			return 0, false // bound for 京王線新宿, does not serve 幡ヶ谷
		}
		return t.Pos, true
	default:
		return 0, false
	}
}

// corridorBound reports whether a destination puts the train onto the
// 京王新線 → 都営新宿線 corridor (which serves 幡ヶ谷/新線新宿) rather than the
// main line to 京王線新宿.
func corridorBound(dest string) bool {
	for _, kw := range []string{"都営", "新線新宿", "本八幡", "大島", "瑞江", "篠崎", "岩本町", "馬喰横山", "九段下", "市ヶ谷"} {
		if strings.Contains(dest, kw) {
			return true
		}
	}
	return false
}

// ---------------- shared HTTP helper ----------------

// getPlain fetches a URL with a browser-ish UA and no Keio-specific referer,
// used for the public weather/transit endpoints.
func (c *Client) getPlain(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EinkDashboard/1.0; +https://github.com/yeungalan/eink-todo)")
	req.Header.Set("Accept-Language", "ja,en;q=0.8")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}
