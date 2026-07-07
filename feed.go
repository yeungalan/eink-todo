package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const upstreamBase = "http://a.opentidkeio.jp"

// syasyu style -> badge background / foreground colours (from zaisen.css).
var styleColors = map[string][2]string{
	"STYLE_STOP_0":      {"#da007a", "#ffffff"}, // 特急
	"STYLE_STOP_1":      {"#f3980f", "#ffffff"},
	"STYLE_STOP_2":      {"#00a785", "#ffffff"}, // 急行
	"STYLE_STOP_3":      {"#d8ca00", "#1a1a1a"}, // 区間急行
	"STYLE_STOP_4":      {"#436488", "#ffffff"}, // 快速
	"STYLE_STOP_5":      {"#898989", "#ffffff"}, // 各駅停車
	"STYLE_STOP_6":      {"#231f20", "#ffffff"}, // 京王ライナー
	"STYLE_STOP_7":      {"#ffffff", "#1a1a1a"}, // 臨時
	"STYLE_STOP_8":      {"#618e2b", "#ffffff"}, // Mt.TAKAO
	"STYLE_MULTI_TRAIN": {"#202020", "#ffffff"},
}

type typeInfo struct {
	name, nameEn, icon, color, text string
}

// Client fetches and normalizes the Keio realtime feed.
type Client struct {
	http *http.Client

	mu      sync.RWMutex
	types   map[string]typeInfo // syasyu code -> info
	dest    map[string]string   // ikisaki code -> name
	loc     map[string]string   // position ID -> name
	locKind map[string]string   // position ID -> kind (駅 / 駅間)
	last    *State              // last successfully built state
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 12 * time.Second},
	}
}

// LoadConfig seeds the lookup tables from the embedded snapshots, then tries a
// live refresh (non-fatal on failure — seeds keep us running).
func (c *Client) LoadConfig() error {
	if err := c.loadConfigFrom(readSeed); err != nil {
		return fmt.Errorf("seed config: %w", err)
	}
	// Best-effort live refresh so destination/station tables stay current.
	c.loadConfigFrom(c.readLive)
	return nil
}

type reader func(name string) ([]byte, error)

func (c *Client) loadConfigFrom(rd reader) error {
	types := map[string]typeInfo{}
	if b, err := rd("syasyu.json"); err == nil {
		var f syasyuFile
		if json.Unmarshal(b, &f) == nil {
			for _, s := range f.Syasyu {
				col := styleColors[s.Style]
				bg, fg := "#555555", "#ffffff"
				if col[0] != "" {
					bg, fg = col[0], col[1]
				}
				types[s.Code] = typeInfo{
					name: s.Name, nameEn: s.NameE, icon: s.IconName,
					color: bg, text: fg,
				}
			}
		}
	}
	dest := map[string]string{}
	if b, err := rd("ikisaki.json"); err == nil {
		var f ikisakiFile
		if json.Unmarshal(b, &f) == nil {
			for _, s := range f.Ikisaki {
				dest[s.Code] = s.Name
			}
		}
	}
	loc := map[string]string{}
	kind := map[string]string{}
	if b, err := rd("position.json"); err == nil {
		var f positionFile
		if json.Unmarshal(b, &f) == nil {
			for _, p := range f.Pos {
				loc[p.ID] = p.Name
				kind[p.ID] = p.Kind
			}
		}
	}
	if len(types) == 0 || len(dest) == 0 || len(loc) == 0 {
		return fmt.Errorf("incomplete config (types=%d dest=%d loc=%d)", len(types), len(dest), len(loc))
	}
	c.mu.Lock()
	c.types, c.dest, c.loc, c.locKind = types, dest, loc, kind
	c.mu.Unlock()
	return nil
}

func (c *Client) readLive(name string) ([]byte, error) {
	return c.get(fmt.Sprintf("%s/config/%s?ver=%d", upstreamBase, name, time.Now().Unix()))
}

func (c *Client) get(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; KeioLiveBoard/1.0)")
	req.Header.Set("Referer", upstreamBase+"/html/zaisen_keio.html")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Fetch pulls the realtime feed + service CSVs and returns a normalized State.
// On upstream failure it returns the last good State marked stale (if any).
func (c *Client) Fetch() (*State, error) {
	feedURL := fmt.Sprintf("%s/data/traffic_info.json?ts=%d", upstreamBase, time.Now().UnixMilli())
	body, err := c.get(feedURL)
	if err != nil {
		if s := c.cached(); s != nil {
			return s, nil
		}
		return nil, err
	}
	var raw rawFeed
	if err := json.Unmarshal(body, &raw); err != nil {
		if s := c.cached(); s != nil {
			return s, nil
		}
		return nil, fmt.Errorf("decode feed: %w", err)
	}

	st := c.normalize(&raw)
	st.Service = c.fetchService()
	st.Source = "live"

	c.mu.Lock()
	c.last = st
	c.mu.Unlock()
	return st, nil
}

func (c *Client) cached() *State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.last == nil {
		return nil
	}
	cp := *c.last
	cp.Stale = true
	cp.Source = "cache"
	return &cp
}

func (c *Client) normalize(raw *rawFeed) *State {
	c.mu.RLock()
	types, dest, loc := c.types, c.dest, c.loc
	c.mu.RUnlock()

	st := &State{
		FetchedAt: time.Now().Format("15:04:05"),
		SystemOK:  true,
	}
	if len(raw.Up) > 0 {
		u := raw.Up[0]
		if u.St != "0" && u.St != "" {
			st.SystemOK = false
		}
		if len(u.Dt) > 0 {
			d := u.Dt[0]
			st.UpdatedAt = fmt.Sprintf("%s:%s:%s", d.Hh, d.Mm, d.Ss)
			st.UpdatedFull = fmt.Sprintf("%s/%s/%s %s:%s:%s", d.Yy, d.Mt, d.Dy, d.Hh, d.Mm, d.Ss)
		}
	}

	add := func(units []rawUnit, atStation bool) {
		for _, u := range units {
			locName := loc[u.ID]
			if locName == "" {
				locName = u.ID
			}
			for _, p := range u.Ps {
				ti := types[p.SyTr]
				delay, _ := strconv.Atoi(strings.TrimSpace(p.Dl))
				dirUp := p.Ki == "0"
				dirLabel := "下り"
				direction := "down"
				if dirUp {
					dirLabel = "上り"
					direction = "up"
				}
				destName := dest[strings.TrimSpace(p.Ik)]
				if destName == "" {
					destName = dest[strings.TrimSpace(p.IkTr)]
				}
				tr := Train{
					TrainNo:     strings.TrimSpace(p.Tr),
					TypeCode:    p.SyTr,
					TypeName:    orDash(ti.name),
					TypeNameEn:  ti.nameEn,
					TypeIcon:    ti.icon,
					Color:       orDefault(ti.color, "#555555"),
					TextOnColor: orDefault(ti.text, "#ffffff"),
					Direction:   direction,
					DirLabel:    dirLabel,
					Destination: orDash(destName),
					DelayMin:    delay,
					AtStation:   atStation,
					LocName:     locName,
					Info:        strings.TrimSpace(p.Inf),
				}
				if pl, ok := resolvePlacement(locName, atStation); ok {
					tr.Branch = pl.branch
					if b := branchByKey(pl.branch); b != nil {
						tr.BranchName = b.Name
					}
					tr.FromIdx = pl.fromIdx
					tr.ToIdx = pl.toIdx
					tr.Pos = pl.pos
					tr.Endpoints = pl.ends
				} else {
					tr.Branch = "other"
					tr.BranchName = "その他"
				}
				st.Trains = append(st.Trains, tr)
			}
		}
	}
	add(raw.TS, true)
	add(raw.TB, false)

	// counts
	st.Counts.Total = len(st.Trains)
	for _, t := range st.Trains {
		if t.Direction == "up" {
			st.Counts.Up++
		} else {
			st.Counts.Down++
		}
		if t.DelayMin > 0 {
			st.Counts.Delayed++
			if t.DelayMin > st.Counts.MaxLate {
				st.Counts.MaxLate = t.DelayMin
			}
		}
	}
	return st
}

// fetchService reads the two operation-info CSVs and maps them to line status.
func (c *Client) fetchService() ServiceStatus {
	statusText := map[string]string{
		"jyokyo_1": "概ね平常運行",
		"jyokyo_2": "運転見合わせ",
		"jyokyo_3": "一部運転見合わせ",
		"jyokyo_4": "遅れています",
	}
	statusLevel := map[string]string{
		"jyokyo_1": "normal", "jyokyo_2": "stop", "jyokyo_3": "stop", "jyokyo_4": "delay",
	}
	out := ServiceStatus{
		Keio:       LineStatus{Text: "情報が取得できません", Level: "error"},
		Inokashira: LineStatus{Text: "情報が取得できません", Level: "error"},
	}

	if b, err := c.get(fmt.Sprintf("%s/unkouinf/unkou_pub.csv?ts=%d", upstreamBase, time.Now().Unix())); err == nil {
		line := firstLine(b)
		fields := parseCSVLine(line)
		if len(fields) >= 2 && fields[1] != "" {
			out.Notice = fields[1]
		}
	}
	if b, err := c.get(fmt.Sprintf("%s/unkouinf/unkou_pub2.csv?ts=%d", upstreamBase, time.Now().Unix())); err == nil {
		rows := splitLines(b)
		var codes []string
		for _, r := range rows {
			f := parseCSVLine(r)
			if len(f) >= 2 {
				codes = append(codes, f[1])
			}
		}
		if len(codes) >= 1 {
			out.Keio = LineStatus{Text: orDefault(statusText[codes[0]], "情報が取得できません"), Level: orDefault(statusLevel[codes[0]], "error")}
		}
		if len(codes) >= 2 {
			out.Inokashira = LineStatus{Text: orDefault(statusText[codes[1]], "情報が取得できません"), Level: orDefault(statusLevel[codes[1]], "error")}
		}
	}
	return out
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "―"
	}
	return s
}
func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
