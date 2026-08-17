package main

import "sort"

// NextTrain is one direction's next-departure info for a single station,
// derived from the already-fetched Keio feed (no extra upstream call).
type NextTrain struct {
	DirLabel    string `json:"dirLabel"` // 上り / 下り
	TypeName    string `json:"typeName"`
	Destination string `json:"destination"`
	DelayMin    int    `json:"delayMin"`
	Status      string `json:"status"` // "到着" | "接近中" | "情報なし"
}

const (
	hatagayaBranch    = "shinsen"
	hatagayaName      = "幡ヶ谷"
	hatagayaTrainsMax = 2 // how many upcoming trains to show per direction
)

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

	var up, down []*Train
	for i := range trains {
		t := &trains[i]
		if t.Branch != hatagayaBranch {
			continue
		}
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
	}

	// Down trains approach with increasing Pos, up trains with decreasing
	// Pos, so in both cases "soonest" sorts toward target first.
	sort.Slice(down, func(i, j int) bool { return down[i].Pos > down[j].Pos })
	sort.Slice(up, func(i, j int) bool { return up[i].Pos < up[j].Pos })

	toNextTrain := func(t *Train) *NextTrain {
		status := "接近中"
		if t.AtStation && t.Pos == target {
			status = "到着"
		}
		return &NextTrain{
			DirLabel:    t.DirLabel,
			TypeName:    t.TypeName,
			Destination: t.Destination,
			DelayMin:    t.DelayMin,
			Status:      status,
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
