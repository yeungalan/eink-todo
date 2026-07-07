# 京王線 走行位置 — Desktop Live Board

A desktop-friendly rewrite of the Keio Line real-time train-position page
(`http://a.opentidkeio.jp/html/zaisen_keio.html`). The original is a single
tall SVG strip map designed for phones; on a wide screen it wastes most of the
window and forces vertical scrolling. This project reads the **same live feed**
and presents it as a wide dashboard: horizontal strip maps per line plus a
sortable/filterable train board.

It performs exactly what the original page does — reading the live operating
status and every train's type, destination, delay and position — and refreshes
every 30 seconds, matching the upstream client's auto-update interval.

## How it works

The upstream origin is HTTP-only and its JSON/CSV endpoints are awkward to call
straight from a browser (mixed-content + quirky CORS). A small **Go backend**
sits in front of it:

- fetches and normalizes the feed **server-side**, so the browser only ever
  talks to this app (same origin, no mixed content);
- joins the raw feed against the config tables (train types, destinations,
  station/section names) and resolves each train onto a line + fractional
  position for the strip map;
- coalesces upstream calls behind an 8-second cache and keeps serving the last
  good snapshot (flagged as `キャッシュ`) if the origin blips.

### Upstream endpoints consumed

| Endpoint | Purpose |
|---|---|
| `/config/system.json` | points at the live data file + config version |
| `/config/syasyu.json` | train types (種別): name, icon, colour |
| `/config/ikisaki.json` | destination code → station name |
| `/config/position.json` | element id → station / between-station name |
| `/config/line.json` | line codes/names |
| `/data/traffic_info.json` | **realtime train positions** (`TS` = at a station, `TB` = between stations) |
| `/unkouinf/unkou_pub.csv` | operating-status headline |
| `/unkouinf/unkou_pub2.csv` | per-line status (平常 / 遅延 / 見合わせ) |

A snapshot of the config tables is embedded under `data/` as a fallback so the
server still boots if the origin is unreachable; it refreshes them live on
startup and every 6 hours.

## Feature parity + desktop extras

- **Live operating status** for 京王線 / 井の頭線, colour-coded (normal / delay / stop).
- **Every train** with 種別 badge (authentic Keio colours), 列車番号, 行先,
  現在地 (station or A〜B section), 遅れ, direction (上り/下り) and the
  per-train guidance text (`inf`).
- **Strip-map view** — one horizontal rail per line/branch (本線・相模原線・
  高尾線・競馬場線・動物園線・京王新線・井の頭線), up trains above the rail,
  down trains below, positioned by section.
- **Board view** — a sortable table of all trains; click any column to sort.
- **Filters**: by line, direction, and 種別, plus free-text search on train
  number / destination.
- **Auto-refresh** every 30 s with a countdown ring, manual refresh, and pause.

## Run

```bash
go run .
# then open http://localhost:8080
# override the port with:  ADDR=:9000 go run .
```

Requires Go 1.25+. The only dependency is `golang.org/x/text` (Unicode NFKC
folding, so feed station names using CJK compatibility ideographs — e.g.
塚 U+FA10 — match the built-in station list).

## Layout

```
main.go        HTTP server, embeds web/, /api/state + /api/meta, request coalescing
feed.go        upstream client, config loading, feed → normalized State
network.go     line/branch topology + section→position resolver
types.go       raw feed structs + normalized output structs
helpers.go     embedded seed config + CSV parsing
web/           index.html · style.css · app.js  (no external assets)
data/          embedded config snapshots (fallback)
```

## Notes

Unofficial viewer. Train data © Keio Corporation, sourced from
opentidkeio.jp. A handful of trains that briefly enter Toei Shinjuku Line
territory have no display anchor in the Keio config (the original page drops
them silently); this board still lists them in the board view rather than
losing them.
