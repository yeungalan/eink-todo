package main

// ---- Raw upstream feed structures (traffic_info.json) ----

type rawFeed struct {
	Up []struct {
		Dt []struct {
			Yy string `json:"yy"`
			Mt string `json:"mt"`
			Dy string `json:"dy"`
			Hh string `json:"hh"`
			Mm string `json:"mm"`
			Ss string `json:"ss"`
		} `json:"dt"`
		St string `json:"st"`
	} `json:"up"`
	TS []rawUnit `json:"TS"` // trains stopped at a station
	TB []rawUnit `json:"TB"` // trains between stations
}

type rawUnit struct {
	ID string   `json:"id"`
	Sn string   `json:"sn"`
	Ps []rawPos `json:"ps"`
}

type rawPos struct {
	Tr   string `json:"tr"`    // train number
	Sy   string `json:"sy"`    // type code (degenerate mode)
	SyTr string `json:"sy_tr"` // type code (normal mode)
	Ki   string `json:"ki"`    // direction 0=up 1=down
	Bs   string `json:"bs"`    // block sub-position
	Dl   string `json:"dl"`    // delay minutes
	Ik   string `json:"ik"`    // destination code (normal)
	IkTr string `json:"ik_tr"` // destination code (alt)
	Sr   string `json:"sr"`    // rolling stock / car type
	Sk   string `json:"sk"`    // car model code
	Inf  string `json:"inf"`   // free text notice
}

// ---- Config structures ----

type syasyuFile struct {
	Syasyu []struct {
		Code     string `json:"code"`
		Style    string `json:"style"`
		IconName string `json:"iconname"`
		Name     string `json:"name"`
		NameE    string `json:"name_e"`
	} `json:"syasyu"`
}

type ikisakiFile struct {
	Ikisaki []struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"ikisaki"`
}

type positionFile struct {
	Pos []struct {
		ID   string `json:"ID"`
		Name string `json:"name"`
		Kind string `json:"kind"`
	} `json:"pos"`
}

// ---- Normalized output (served to the browser) ----

type State struct {
	UpdatedAt   string        `json:"updatedAt"`   // "HH:MM:SS"
	UpdatedFull string        `json:"updatedFull"` // "YYYY/MM/DD HH:MM:SS"
	FetchedAt   string        `json:"fetchedAt"`   // server fetch time
	SystemOK    bool          `json:"systemOK"`    // feed status flag (up[0].st == 0)
	Service     ServiceStatus `json:"service"`
	Trains      []Train       `json:"trains"`
	Counts      Counts        `json:"counts"`
	Stale       bool          `json:"stale"`  // true if serving a cached copy after a failed refresh
	Source      string        `json:"source"` // "live" or "cache"
}

type Counts struct {
	Total   int `json:"total"`
	Up      int `json:"up"`
	Down    int `json:"down"`
	Delayed int `json:"delayed"`
	MaxLate int `json:"maxLate"`
}

type ServiceStatus struct {
	Keio       LineStatus `json:"keio"`
	Inokashira LineStatus `json:"inokashira"`
	Notice     string     `json:"notice"` // headline from unkou_pub.csv
}

type LineStatus struct {
	Text  string `json:"text"`  // e.g. "概ね平常運行"
	Level string `json:"level"` // normal | delay | stop | error
}

type Train struct {
	TrainNo     string    `json:"trainNo"`     // trimmed train number
	TypeCode    string    `json:"typeCode"`    // syasyu code
	TypeName    string    `json:"typeName"`    // 種別 名称
	TypeNameEn  string    `json:"typeNameEn"`  // English
	TypeIcon    string    `json:"typeIcon"`    // single-char badge (特/急/…)
	Color       string    `json:"color"`       // badge background hex
	TextOnColor string    `json:"textOnColor"` // #fff or #000 for contrast
	Direction   string    `json:"direction"`   // "up" | "down"
	DirLabel    string    `json:"dirLabel"`    // 上り / 下り
	Destination string    `json:"destination"` // resolved station name
	DelayMin    int       `json:"delayMin"`
	AtStation   bool      `json:"atStation"`  // true=stopped at station, false=between
	LocName     string    `json:"locName"`    // "新宿" or "笹塚～新宿"
	Info        string    `json:"info"`       // free-text notice
	Branch      string    `json:"branch"`     // branch key (main/sagamihara/…)
	BranchName  string    `json:"branchName"` // Japanese branch label
	FromIdx     int       `json:"fromIdx"`    // index of "lower" station on the branch
	ToIdx       int       `json:"toIdx"`      // index of "upper" station (== FromIdx if at a station)
	Pos         float64   `json:"pos"`        // fractional position along the branch (0..len-1)
	Endpoints   [2]string `json:"endpoints"`  // resolved section endpoint names (between only)
}
