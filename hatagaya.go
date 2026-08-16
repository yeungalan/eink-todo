package main

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
	hatagayaBranch = "shinsen"
	hatagayaName   = "幡ヶ谷"
)

// hatagayaNextTrains scans the live train list for the next up and down
// train at/approaching Hatagaya station on the Keio New Line branch.
// Per network.go's branch convention, index 0 is the "up" terminal, so an
// up train's position decreases toward Hatagaya and a down train's
// position increases toward it.
func hatagayaNextTrains(trains []Train) map[string]*NextTrain {
	idx, ok := stationIndex[hatagayaBranch][nfkc(hatagayaName)]
	if !ok {
		return nil
	}
	target := float64(idx)

	var bestUp, bestDown *Train
	for i := range trains {
		t := &trains[i]
		if t.Branch != hatagayaBranch {
			continue
		}
		switch t.Direction {
		case "down":
			if t.Pos <= target && (bestDown == nil || t.Pos > bestDown.Pos) {
				bestDown = t
			}
		case "up":
			if t.Pos >= target && (bestUp == nil || t.Pos < bestUp.Pos) {
				bestUp = t
			}
		}
	}

	toNextTrain := func(t *Train) *NextTrain {
		if t == nil {
			return nil
		}
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

	return map[string]*NextTrain{
		"up":   toNextTrain(bestUp),
		"down": toNextTrain(bestDown),
	}
}
