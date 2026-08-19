package main

import "time"

// hatagayaScheduleMinutes gives, for each direction ("up"/"down") and day
// type (false = weekday/平日, true = Saturday-or-holiday/土曜・休日), the
// published departure minute within each service hour at Hatagaya station.
// Hours 0 and 1 are the tail of the *previous* service day, still running
// past midnight — hour 1 is always empty (last train is in the 0 o'clock
// hour) but kept for completeness.
//
// Code generated from transfer-train.navitime.biz/pdf/keio/timetable/20260816/7501.pdf
// (幡ヶ谷駅 発車標準時刻表, retrieved 2026-08-17). Regenerate if Keio revises the
// timetable. This does not know about Japanese public holidays — it only
// distinguishes Saturday/Sunday from weekdays, so a weekday national holiday
// will use the wrong (平日) column.
var hatagayaScheduleMinutes = map[string]map[bool]map[int][]int{
	"up": {
		false: { // weekday (平日)
			0:  {5, 18, 28},
			1:  {},
			4:  {46},
			5:  {10, 31, 46},
			6:  {0, 11, 22, 33, 43, 50, 57},
			7:  {3, 8, 13, 18, 23, 28, 33, 38, 42, 48, 53, 57},
			8:  {0, 5, 8, 11, 16, 20, 23, 28, 31, 34, 38, 41, 45, 47, 51, 55, 59},
			9:  {4, 8, 13, 17, 20, 25, 30, 34, 39, 44, 49, 54},
			10: {0, 5, 10, 15, 21, 26, 32, 39, 43, 50, 56},
			11: {1, 8, 13, 21, 26, 32, 38, 43, 49, 56},
			12: {1, 8, 13, 21, 26, 32, 38, 43, 49, 56},
			13: {1, 8, 13, 21, 26, 32, 38, 43, 49, 56},
			14: {1, 8, 13, 21, 26, 32, 38, 43, 49, 56},
			15: {2, 6, 12, 17, 22, 28, 34, 40, 44, 50, 55},
			16: {0, 5, 10, 17, 22, 27, 32, 38, 45, 50, 58},
			17: {5, 11, 16, 21, 25, 28, 33, 42, 47, 50, 55, 59},
			18: {2, 6, 10, 15, 21, 26, 31, 35, 40, 43, 49, 53, 58},
			19: {4, 9, 14, 20, 23, 28, 33, 39, 44, 49, 55},
			20: {1, 7, 14, 21, 27, 33, 39, 46, 54},
			21: {1, 8, 14, 20, 27, 34, 40, 47, 53},
			22: {1, 7, 15, 23, 33, 43, 54},
			23: {5, 15, 23, 32, 40, 53},
		},
		true: { // Saturday / holiday (土曜・休日)
			0:  {5, 18, 28},
			1:  {},
			4:  {46},
			5:  {10, 31, 48},
			6:  {0, 10, 22, 33, 42, 51},
			7:  {2, 12, 21, 27, 32, 43, 50, 56},
			8:  {2, 9, 14, 19, 28, 34, 40, 45, 50, 55},
			9:  {2, 10, 16, 21, 27, 36, 42, 48, 55},
			10: {1, 7, 12, 17, 21, 26, 33, 39, 45, 50, 56},
			11: {1, 6, 11, 16, 21, 27, 33, 40, 49, 56},
			12: {1, 6, 12, 20, 27, 33, 40, 49, 56},
			13: {1, 7, 12, 20, 27, 33, 40, 49, 56},
			14: {1, 6, 11, 20, 27, 33, 40, 49, 56},
			15: {1, 10, 17, 25, 33, 40, 48, 58},
			16: {3, 10, 16, 21, 26, 31, 40, 46, 51, 58},
			17: {7, 12, 17, 22, 29, 34, 40, 47, 53, 59},
			18: {4, 8, 14, 20, 26, 32, 36, 41, 51},
			19: {0, 7, 13, 20, 27, 33, 40, 47, 54},
			20: {0, 7, 14, 20, 27, 34, 41, 47, 55},
			21: {2, 9, 18, 25, 32, 41, 48, 58},
			22: {4, 15, 27, 40, 53},
			23: {3, 13, 23, 32, 40, 53},
		},
	},
	"down": {
		false: { // weekday (平日)
			0:  {7, 20, 31, 45},
			1:  {},
			4:  {59},
			5:  {23, 32, 45, 57},
			6:  {10, 22, 34, 46, 58},
			7:  {6, 14, 21, 27, 34, 41, 44, 49, 52, 56, 59},
			8:  {3, 7, 10, 14, 16, 20, 23, 26, 30, 33, 37, 41, 44, 48, 52, 55},
			9:  {0, 3, 8, 12, 17, 21, 26, 31, 34, 39, 44, 48, 51, 56, 58},
			10: {3, 7, 11, 18, 22, 26, 32, 37, 42, 48, 55},
			11: {2, 8, 12, 20, 26, 31, 37, 42, 48, 55},
			12: {2, 8, 12, 20, 26, 31, 37, 42, 48, 55},
			13: {2, 8, 12, 20, 26, 31, 37, 42, 48, 55},
			14: {2, 8, 12, 20, 26, 31, 37, 42, 48, 55},
			15: {2, 6, 12, 20, 26, 31, 37, 42, 49, 55},
			16: {2, 6, 12, 17, 23, 29, 34, 41, 47, 53, 58},
			17: {3, 7, 12, 15, 19, 25, 29, 35, 38, 41, 44, 48, 51, 56},
			18: {0, 5, 10, 15, 20, 24, 29, 35, 38, 44, 47, 55, 58},
			19: {4, 9, 15, 17, 24, 30, 35, 38, 44, 50, 56},
			20: {4, 9, 15, 24, 29, 35, 44, 50, 57},
			21: {4, 11, 16, 23, 30, 38, 44, 51, 58},
			22: {5, 12, 22, 31, 43, 54},
			23: {6, 17, 28, 41, 54},
		},
		true: { // Saturday / holiday (土曜・休日)
			0:  {7, 20, 31, 45},
			1:  {},
			4:  {59},
			5:  {23, 32, 46, 57},
			6:  {10, 22, 34, 47, 58},
			7:  {10, 21, 34, 39, 43, 49, 54, 59},
			8:  {5, 11, 17, 25, 30, 35, 41, 46, 52, 57},
			9:  {6, 12, 17, 26, 36, 40, 47, 50, 54},
			10: {3, 8, 14, 20, 26, 31, 37, 42, 45, 52, 56},
			11: {1, 8, 12, 17, 23, 30, 39, 45, 51, 56},
			12: {2, 12, 17, 22, 30, 39, 45, 51, 56},
			13: {2, 12, 17, 22, 31, 40, 45, 51, 56},
			14: {2, 12, 18, 22, 30, 40, 45, 52},
			15: {1, 6, 14, 23, 31, 40, 45, 52},
			16: {1, 6, 12, 18, 26, 34, 40, 47, 56},
			17: {1, 6, 11, 17, 23, 29, 37, 39, 44, 49, 54},
			18: {0, 6, 11, 16, 21, 27, 35, 44, 49, 55},
			19: {2, 7, 13, 20, 27, 33, 40, 46, 53},
			20: {0, 6, 14, 21, 26, 34, 41, 47, 56},
			21: {5, 11, 19, 27, 35, 45, 55},
			22: {4, 14, 24, 35, 44, 55},
			23: {6, 17, 28, 41, 54},
		},
	},
}

// hatagayaScheduleHourOrder is the chronological order of hour-of-day keys
// within one service day: the day starts at 4am and runs past midnight
// through the 0/1 o'clock tail.
var hatagayaScheduleHourOrder = []int{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 0, 1}

// hatagayaDefaultDestination is used for schedule-derived (non-live)
// estimates, which don't carry per-train destination/type detail — "up"
// trains are shown bound for 本八幡 and "down" trains for 京王八王子, the
// most common destination on each side per the timetable's legend.
var hatagayaDefaultDestination = map[string]string{"up": "本八幡", "down": "京王八王子"}

// serviceDay returns the calendar date (midnight, same location as t) that
// t's train-schedule "service day" belongs to. Service runs from ~4am
// through ~1am the next calendar day, so times before 4am still belong to
// the previous day's schedule.
func serviceDay(t time.Time) time.Time {
	if t.Hour() < 4 {
		t = t.AddDate(0, 0, -1)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// isScheduleWeekend reports whether day (as returned by serviceDay) uses the
// 土曜・休日 timetable column. Public holidays aren't accounted for — see
// hatagayaScheduleMinutes' doc comment.
func isScheduleWeekend(day time.Time) bool {
	wd := day.Weekday()
	return wd == time.Saturday || wd == time.Sunday
}

// hatagayaScheduledDepartures returns the next n scheduled departure times
// for direction dir at or after searchFrom, walking forward across
// service-day boundaries (and day-type changes, e.g. Friday night into
// Saturday) as needed.
func hatagayaScheduledDepartures(searchFrom time.Time, dir string, n int) []time.Time {
	var out []time.Time
	day := serviceDay(searchFrom)
	for daysTried := 0; len(out) < n && daysTried < 7; daysTried++ { // safety bound: a week out is plenty
		weekend := isScheduleWeekend(day)
		hours := hatagayaScheduleMinutes[dir][weekend]
		for _, h := range hatagayaScheduleHourOrder {
			// Hours 0/1 belong to the tail of this service day, i.e. the
			// *next* calendar date.
			date := day
			if h < 4 {
				date = day.AddDate(0, 0, 1)
			}
			for _, m := range hours[h] {
				dep := time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, searchFrom.Location())
				if dep.Before(searchFrom) {
					continue
				}
				out = append(out, dep)
				if len(out) >= n {
					return out
				}
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return out
}

// hatagayaScheduleMatchWindow bounds how far a live-tracked train's rough
// position-based ETA guess (see hatagayaMinPerSegment) may sit from a
// published departure and still be considered "the same train" by
// hatagayaMatchSchedule. Set below half the timetable's tightest peak
// headway (~5 min) so it can't misfire onto the wrong neighboring
// departure.
const hatagayaScheduleMatchWindow = 4 * time.Minute

// hatagayaMatchSchedule looks for the published departure closest to a
// live-tracked train's rough ETA guess (now + rawEtaMin) and, if one falls
// within hatagayaScheduleMatchWindow, returns the ETA implied by that
// departure instead — the timetable minute is exact where the position-based
// guess is only ever "about N min" (see hatagayaMinPerSegment's doc
// comment). delayMin (Keio's own reported delay for this train) is added on
// top, so a train running late still gets an accurate ETA rather than being
// silently snapped back to its original slot. Returns ok=false if nothing in
// the timetable is close enough — e.g. an extra/off-timetable working —
// in which case the raw estimate should be kept as-is.
func hatagayaMatchSchedule(now time.Time, dir string, rawEtaMin, delayMin int) (etaMin int, ok bool) {
	guess := now.Add(time.Duration(rawEtaMin) * time.Minute)
	from := guess.Add(-hatagayaScheduleMatchWindow)
	if from.Before(now) {
		from = now
	}

	var best time.Time
	bestDiff := time.Duration(-1)
	for _, dep := range hatagayaScheduledDepartures(from, dir, 8) {
		diff := dep.Sub(guess)
		if diff < 0 {
			diff = -diff
		}
		if diff > hatagayaScheduleMatchWindow {
			if dep.After(guess) {
				break // sorted ascending — no closer candidate remains
			}
			continue
		}
		if bestDiff < 0 || diff < bestDiff {
			best, bestDiff = dep, diff
		}
	}
	if bestDiff < 0 {
		return 0, false
	}

	adjusted := best.Add(time.Duration(delayMin) * time.Minute)
	etaMin = int(adjusted.Sub(now).Minutes() + 0.5)
	if etaMin < 0 {
		etaMin = 0
	}
	return etaMin, true
}

// hatagayaScheduleEstimates fills in up to n schedule-derived NextTrain
// entries for direction dir, considering only departures at or after
// searchFrom — pass a time just past the last live-tracked train (rather
// than now) so an estimate never duplicates a train the live feed already
// reported. ETA is still shown relative to the real now. These entries are
// marked Status "予定" (scheduled) rather than a live "接近中"/"到着" status,
// since they come from the static timetable, not train position tracking.
func hatagayaScheduleEstimates(now, searchFrom time.Time, dir string, n int) []*NextTrain {
	deps := hatagayaScheduledDepartures(searchFrom, dir, n)
	out := make([]*NextTrain, len(deps))
	for i, dep := range deps {
		out[i] = &NextTrain{
			DirLabel:    map[string]string{"up": "上り", "down": "下り"}[dir],
			Destination: hatagayaDefaultDestination[dir],
			Status:      "予定",
			EtaMin:      int(dep.Sub(now).Minutes() + 0.5),
		}
	}
	return out
}
