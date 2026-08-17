package main

import (
	"sort"
	"strings"
)

// NextTrain is one direction's next-departure info for a single station,
// derived from the already-fetched Keio feed (no extra upstream call).
type NextTrain struct {
	DirLabel    string `json:"dirLabel"` // 上り / 下り
	TypeName    string `json:"typeName"`
	Destination string `json:"destination"`
	DelayMin    int    `json:"delayMin"`
	Status      string `json:"status"` // "到着" | "接近中" | "情報なし"
	EtaMin      int    `json:"etaMin"` // rough estimate, see hatagayaMinPerSegment
}

const (
	hatagayaBranch    = "shinsen"
	hatagayaName      = "幡ヶ谷"
	hatagayaTrainsMax = 3 // how many upcoming trains to show per direction

	// The feed gives no speed/ETA data, only a station-granularity position,
	// so estimated arrival time is (station segments away) * this constant.
	// Tuned to the Keio New Line/Toei Shinjuku Line's typical inter-station
	// run time — treat it as a rough "about N min", not a real prediction.
	hatagayaMinPerSegment = 2.0
)

// isToeiShinjukuBound reports whether a destination string marks a train as
// through-running onto the Toei Shinjuku Line (destination codes 300/301 in
// ikisaki.json: "京王線新宿・都営新宿線方面" / "都営新宿線方面"). Such trains
// necessarily route via the Keio New Line, i.e. through Hatagaya.
func isToeiShinjukuBound(dest string) bool {
	return strings.Contains(dest, "都営新宿線")
}

// hatagayaNextTrains scans the live train list for the next few up and down
// trains at/approaching Hatagaya station on the Keio New Line branch, each
// direction sorted soonest-first.
// Per network.go's branch convention, index 0 is the "up" terminal, so an
// up train's position decreases toward Hatagaya and a down train's
// position increases toward it.
func hatagayaNextTrains(trains []Train) map[string][]*NextTrain {
	idx, ok := stationIndex[hatagayaBranch][nfkc(hatagayaName)]
	if !ok {
		return nil
	}
	target := float64(idx)

	// 笹塚 sits at the boundary between the main line and this branch, and
	// station-only placement always claims it for the trunk ("main") branch
	// (see preferredBranchForStation) — even for an up train sitting there
	// that is about to divert onto the New Line toward the Toei Shinjuku
	// Line. Treat those as already at the branch's 笹塚 end so they show up
	// as upcoming Hatagaya arrivals instead of being missed entirely.
	sasazukaIdx, hasSasazuka := stationIndex[hatagayaBranch][nfkc("笹塚")]

	var up, down []*Train
	for i := range trains {
		t := &trains[i]
		if t.Branch == hatagayaBranch {
			switch t.Direction {
			case "down":
				if t.Pos <= target {
					down = append(down, t)
				}
			case "up":
				if t.Pos >= target {
					up = append(up, t)
				}
			}
			continue
		}
		if hasSasazuka && t.Branch == "main" && t.Direction == "up" && t.AtStation &&
			nfkc(t.LocName) == nfkc("笹塚") && isToeiShinjukuBound(t.Destination) {
			cp := *t
			cp.Pos = float64(sasazukaIdx)
			up = append(up, &cp)
		}
	}

	// Down trains approach with increasing Pos, up trains with decreasing
	// Pos, so in both cases "soonest" sorts toward target first.
	sort.Slice(down, func(i, j int) bool { return down[i].Pos > down[j].Pos })
	sort.Slice(up, func(i, j int) bool { return up[i].Pos < up[j].Pos })

	toNextTrain := func(t *Train) *NextTrain {
		dist := t.Pos - target
		if dist < 0 {
			dist = -dist
		}
		status := "接近中"
		etaMin := int(dist*hatagayaMinPerSegment + 0.5)
		if t.AtStation && t.Pos == target {
			status = "到着"
			etaMin = 0
		}
		return &NextTrain{
			DirLabel:    t.DirLabel,
			TypeName:    t.TypeName,
			Destination: t.Destination,
			DelayMin:    t.DelayMin,
			Status:      status,
			EtaMin:      etaMin,
		}
	}

	take := func(ts []*Train) []*NextTrain {
		if len(ts) > hatagayaTrainsMax {
			ts = ts[:hatagayaTrainsMax]
		}
		out := make([]*NextTrain, len(ts))
		for i, t := range ts {
			out[i] = toNextTrain(t)
		}
		return out
	}

	return map[string][]*NextTrain{
		"up":   take(up),
		"down": take(down),
	}
}
