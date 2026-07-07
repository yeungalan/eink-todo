package main

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// nfkc folds CJK compatibility ideographs (e.g. 塚 U+FA10 -> U+585A) and
// full/half-width variants so feed station names match our hardcoded list.
func nfkc(s string) string { return norm.NFKC.String(strings.TrimSpace(s)) }

// Branch defines an ordered sequence of stations. Order is "down" direction:
// index 0 is the up-terminal (toward Shinjuku/origin), last index is the
// outbound terminus. This lets us place a train at a fractional position and
// know which way it points.
type Branch struct {
	Key      string
	Name     string
	Color    string
	Stations []string
}

// Keio network as drawn by keio.svg (everything except the Inokashira line is
// part of the same page). Names match position.json / ikisaki.json exactly.
var branches = []Branch{
	{
		Key: "main", Name: "京王線 本線", Color: "#da007a",
		Stations: []string{
			"新宿", "笹塚", "代田橋", "明大前", "下高井戸", "桜上水", "上北沢",
			"八幡山", "芦花公園", "千歳烏山", "仙川", "つつじヶ丘", "柴崎", "国領",
			"布田", "調布", "西調布", "飛田給", "武蔵野台", "多磨霊園", "東府中",
			"府中", "分倍河原", "中河原", "聖蹟桜ヶ丘", "百草園", "高幡不動", "南平",
			"平山城址公園", "長沼", "北野", "京王八王子",
		},
	},
	{
		Key: "shinsen", Name: "京王新線・都営新宿線", Color: "#c0177a",
		Stations: []string{
			"市ヶ谷", "曙橋", "新宿三丁目", "新線新宿", "初台", "幡ヶ谷", "笹塚",
		},
	},
	{
		Key: "takao", Name: "高尾線", Color: "#e0447f",
		Stations: []string{
			"北野", "京王片倉", "山田", "めじろ台", "狭間", "高尾", "高尾山口",
		},
	},
	{
		Key: "sagamihara", Name: "相模原線", Color: "#8f2fbf",
		Stations: []string{
			"調布", "京王多摩川", "京王稲田堤", "京王よみうりランド", "稲城",
			"若葉台", "京王永山", "京王多摩センター", "京王堀之内", "南大沢",
			"多摩境", "橋本",
		},
	},
	{
		Key: "keibajo", Name: "競馬場線", Color: "#0f9e57",
		Stations: []string{"東府中", "府中競馬正門前"},
	},
	{
		Key: "dobutsuen", Name: "動物園線", Color: "#e08a1e",
		Stations: []string{"高幡不動", "多摩動物公園"},
	},
	{
		Key: "inokashira", Name: "井の頭線", Color: "#0d3870",
		Stations: []string{
			"渋谷", "神泉", "駒場東大前", "池ノ上", "下北沢", "新代田", "東松原",
			"明大前", "永福町", "西永福", "浜田山", "高井戸", "富士見ヶ丘", "久我山",
			"三鷹台", "井の頭公園", "吉祥寺",
		},
	},
}

// stationIndex[branchKey][stationName] = position index
var stationIndex = func() map[string]map[string]int {
	m := map[string]map[string]int{}
	for _, b := range branches {
		idx := map[string]int{}
		for i, s := range b.Stations {
			idx[nfkc(s)] = i
		}
		m[b.Key] = idx
	}
	return m
}()

func branchByKey(key string) *Branch {
	for i := range branches {
		if branches[i].Key == key {
			return &branches[i]
		}
	}
	return nil
}

// placement resolves a location name into a branch and a fractional position.
//   - station name (e.g. "調布")  -> exact index
//   - section "A～B"               -> midpoint between A and B on a branch
//
// A station may belong to several branches (e.g. 調布 is on main & sagamihara);
// we prefer the branch where a "between" section's two endpoints are adjacent,
// otherwise the first branch that contains the station. Returns ok=false if
// unresolvable.
type placed struct {
	branch  string
	fromIdx int
	toIdx   int
	pos     float64
	ends    [2]string
}

func resolvePlacement(locName string, atStation bool) (placed, bool) {
	if atStation {
		// exact station
		key := nfkc(locName)
		best, ok := preferredBranchForStation(key)
		if !ok {
			return placed{}, false
		}
		i := stationIndex[best][key]
		return placed{branch: best, fromIdx: i, toIdx: i, pos: float64(i)}, true
	}

	// section "A～B" (upstream uses the wave dash 〜 or ～)
	aRaw, bRaw, ok := splitSection(locName)
	if !ok {
		return placed{}, false
	}
	a, b := nfkc(aRaw), nfkc(bRaw)
	// find a branch where both endpoints exist and are adjacent
	for _, br := range branches {
		idx := stationIndex[br.Key]
		ia, oka := idx[a]
		ib, okb := idx[b]
		if oka && okb {
			lo, hi := ia, ib
			if lo > hi {
				lo, hi = hi, lo
			}
			return placed{
				branch:  br.Key,
				fromIdx: lo,
				toIdx:   hi,
				pos:     float64(lo+hi) / 2,
				ends:    [2]string{aRaw, bRaw},
			}, true
		}
	}
	// fall back: endpoint that resolves to any branch
	if p, ok := preferredBranchForStation(a); ok {
		i := stationIndex[p][a]
		return placed{branch: p, fromIdx: i, toIdx: i, pos: float64(i), ends: [2]string{aRaw, bRaw}}, true
	}
	if p, ok := preferredBranchForStation(b); ok {
		i := stationIndex[p][b]
		return placed{branch: p, fromIdx: i, toIdx: i, pos: float64(i), ends: [2]string{aRaw, bRaw}}, true
	}
	return placed{}, false
}

// preferredBranchForStation picks the branch a lone station should belong to.
// Junction stations are claimed by their trunk line first for a stable board.
func preferredBranchForStation(name string) (string, bool) {
	// Order of branches already puts trunk lines first; interchange stations
	// like 明大前 exist on both main and inokashira — for a station-only train
	// we cannot know the line, so trunk (main) wins by iteration order.
	for _, b := range branches {
		if _, ok := stationIndex[b.Key][name]; ok {
			return b.Key, true
		}
	}
	return "", false
}

func splitSection(s string) (string, string, bool) {
	for _, sep := range []string{"〜", "～", "~"} {
		if i := strings.Index(s, sep); i >= 0 {
			a := strings.TrimSpace(s[:i])
			b := strings.TrimSpace(s[i+len(sep):])
			if a != "" && b != "" {
				return a, b, true
			}
		}
	}
	return "", "", false
}
