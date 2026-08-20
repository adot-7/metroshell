# metroshell — Full Project Handoff

> Complete context document for continuing development. Covers architecture,
> vision, renderer internals, key bugs already fixed, and Phase 4 plan.
> Generated from the full development history of this project.

---

## Table of Contents

1. [What This Project Is](#1-what-this-project-is)
2. [Repository Context](#2-repository-context)
3. [Tech Stack](#3-tech-stack)
4. [Directory Structure](#4-directory-structure)
5. [How the Renderer Works — Full Pipeline](#5-how-the-renderer-works--full-pipeline)
6. [Bubble Tea Architecture — Key Patterns](#6-bubble-tea-architecture--key-patterns)
7. [paulmach/orb — How It's Used](#7-paulmachorb--how-its-used)
8. [MBTiles and Offline Tile Data](#8-mbtiles-and-offline-tile-data)
9. [Current State — What Works](#9-current-state--what-works)
10. [Phase 4 — GTFS + Metro Routing](#10-phase-4--gtfs--metro-routing)
11. [GTFS Data Model](#11-gtfs-data-model)
12. [Two-Panel Layout Plan](#12-two-panel-layout-plan)
13. [Critical Bugs Already Found and Fixed](#13-critical-bugs-already-found-and-fixed)
14. [Conventions and Patterns](#14-conventions-and-patterns)
15. [What NOT to Do](#15-what-not-to-do)

---

## 1. What This Project Is

**metroshell** is a terminal-based Delhi Metro route planner that renders a real
OpenStreetMap braille map of Delhi NCR and overlays DMRC metro lines, stations, and
computed routes on top of it.

The user sees a split-panel TUI:
- **Left (large):** a live braille-rendered map of Delhi NCR, pannable with hjkl,
  zoomable with +/-, draggable with mouse
- **Right (sidebar):** a list of all Delhi Metro stations. User selects FROM and TO,
  the shortest route is computed via BFS, and the route is drawn on the map in the
  metro line's official color

Everything runs offline. No API keys. No network. Map data comes from a local
`delhi-ncr.mbtiles` SQLite file. Metro data comes from a local GTFS ZIP.

**Parent project:** `github.com/adot-7/ncr-on-terminal` (v1.0.0 — pure map renderer,
no GTFS). metroshell was forked from that at v1.0.0 and adds Phase 4 (GTFS routing).

**Vision for metroshell:** DMRC-specific for now. The GTFS loading is optional (if
the zip isn't present, the map still works, sidebar shows coordinates instead of
stations). This keeps the door open for other cities without over-engineering it.

**Vision for ncr-on-terminal (separate repo, not this one):** Long-term becomes a
generic city map viewer where users can search for any city and the program downloads
and renders map data. That's a much bigger scope and entirely separate.

---

## 2. Repository Context

```
github.com/adot-7/metroshell   ← this repo
github.com/adot-7/ncr-on-terminal  ← parent, pure map viewer at v1.0.0
```

metroshell was created by:
```bash
cp -r ncr-on-terminal metroshell
cd metroshell
rm -rf .git && git init
# updated go.mod module to github.com/adot-7/metroshell
# updated all import paths via sed
```

The renderer code in `internal/` is identical to ncr-on-terminal at fork time.
Fixes to the renderer should be applied to both repos manually — they are not linked
by a shared module (intentional decision to avoid premature abstraction at this stage).

---

## 3. Tech Stack

| Concern | Package | Notes |
|---|---|---|
| TUI framework | `github.com/charmbracelet/bubbletea` | v1, NOT v2 |
| Terminal styling | `github.com/charmbracelet/lipgloss` | Borders, colors, text styles |
| Geometry types | `github.com/paulmach/orb` | Point, LineString, Polygon etc |
| MVT tile decode | `github.com/paulmach/orb/encoding/mvt` | Decodes protobuf vector tiles |
| Tile math | `github.com/paulmach/orb/maptile` | Z/X/Y coords, lat/lon conversion |
| Simplification | `github.com/paulmach/orb/simplify` | Douglas-Peucker on geometry |
| SQLite (MBTiles) | `modernc.org/sqlite` | Pure Go, zero CGO, static builds |
| SSH server | `github.com/charmbracelet/wish` | Serve TUI over SSH (Phase 4) |
| Releases | `goreleaser` | Cross-platform binary builds |

**Why `modernc.org/sqlite` and not `github.com/mattn/go-sqlite3`:**
go-sqlite3 requires CGO. CGO breaks cross-compilation and complicates goreleaser.
modernc.org/sqlite is a pure Go port, slightly slower but imperceptible for local
MBTiles reads. The API is identical — just change the driver name from `"sqlite3"`
to `"sqlite"` in `sql.Open`.

**Why Bubble Tea v1 and not v2:**
The project was built on v1. v2 (`charm.land/bubbletea/v2`) has cleaner mouse APIs
but requires changing `View() string` → `View() tea.View`, `tea.KeyMsg` →
`tea.KeyPressMsg`, and moving `WithAltScreen()` into View(). This migration can be
done in Phase 4 but is not necessary. v1 handles everything needed.

---

## 4. Directory Structure

```
metroshell/
├── main.go                 ← Bubble Tea entry point, model, Update, View
├── go.mod
├── go.sum
├── .goreleaser.yml
├── data/
│   ├── .gitkeep
│   ├── delhi-ncr.mbtiles   ← NOT in git (.gitignore), ~200MB
│   └── delhi-metro-gtfs.zip ← NOT in git, from otd.delhi.gov.in
├── internal/
│   ├── braille/
│   │   └── buffer.go       ← BrailleBuffer: pixel ops + Unicode render
│   ├── geo/
│   │   └── viewport.go     ← Tile math, viewport → tile requests, LatLonToPixel
│   ├── tiles/
│   │   └── mbtiles.go      ← SQLite reader, Y-flip, gzip, LRU cache
│   ├── render/
│   │   └── renderer.go     ← Main render loop: tiles → geometry → pixels
│   ├── style/
│   │   └── style.go        ← Hardcoded layer→color style table
│   └── gtfs/               ← Phase 4, may not exist yet
│       ├── feed.go         ← GTFS ZIP parser: stops, routes, trips, shapes
│       └── graph.go        ← StopGraph adjacency list + BFS router
└── cmd/
    └── server/
        └── main.go         ← wish SSH server (Phase 4)
```

**Data file acquisition:**

MBTiles:
```bash
# Download north India OSM extract
curl -O https://download.geofabrik.de/asia/india/north-india-latest.osm.pbf

# Clip to Delhi NCR bounding box
osmium extract --bbox 76.8,28.4,77.4,28.9 \
  north-india-latest.osm.pbf --output delhi-ncr.osm.pbf

# Generate MBTiles with tilemaker (OpenMapTiles schema)
tilemaker \
  --input delhi-ncr.osm.pbf \
  --output delhi-ncr.mbtiles \
  --config resources/config-openmaptiles.json \
  --process resources/process-openmaptiles.lua
```

GTFS: https://otd.delhi.gov.in/data/staticDMRC/ (requires free registration)

---

## 5. How the Renderer Works — Full Pipeline

This is the core of the project. Understand this completely before touching anything.

### The one-sentence summary

`(lat, lon, zoom, terminalSize) → string`

That string is braille Unicode characters with ANSI color codes. Every part of the
codebase is in service of that function.

### Step 1: Slippy map tile math

The world is divided into a grid of tiles at each zoom level Z. At zoom Z there are
`2^Z × 2^Z` tiles. Each tile is addressed by (Z, X, Y) where X goes left→right
(west→east) and Y goes top→bottom (north→south).

To find which tile contains a lat/lon:
```go
n := math.Pow(2, float64(zoom))
x = int(math.Floor((lon + 180.0) / 360.0 * n))
latRad := lat * math.Pi / 180.0
y = int(math.Floor((1 - math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi) / 2 * n))
```

This is the Web Mercator projection. The `ln(tan+sec)` part is the Gudermannian
function — the mathematical heart of Mercator.

`maptile.Fraction(point, zoom)` from paulmach/orb returns the *fractional* tile
position (e.g. `{2928.713, 1703.204}`). The integer part is which tile. The fractional
part tells you how far into that tile your center point is, which determines pixel
offsets for rendering.

### Step 2: Viewport → tile requests

`geo.Viewport.ComputeTiles()` returns a slice of `TileRequest`. Each `TileRequest` has:
- `Z, X, Y` — which tile to fetch
- `PixelOffsetX, PixelOffsetY` — where this tile's (0,0) corner lands in braille pixel space
- `Scale` — braille pixels per tile-space unit (tile space is 0–4096)

The center of the screen is at `(PixelW/2, PixelH/2)` in braille pixels. The center
tile's origin is at `screenCenter - fracWithinTile * tilePixels`.

### Step 3: Read tiles from MBTiles

MBTiles is a SQLite database:
```sql
SELECT tile_data FROM tiles
WHERE zoom_level=? AND tile_column=? AND tile_row=?
```

**Critical: MBTiles uses TMS Y ordering (y=0 at south pole), not XYZ (y=0 at north).**
Always flip: `tmsY = (1 << zoom) - 1 - xyzY` before querying.

`tile_data` is gzip-compressed MVT protobuf. The MBTiles reader gunzips it.

### Step 4: Decode MVT with orb

```go
layers, err := mvt.Unmarshal(data)  // data is already gunzipped
```

`mvt.Layers` is `[]*mvt.Layer` — a **slice**, not a map. Access by iterating:
```go
func findLayer(layers mvt.Layers, name string) *mvt.Layer {
    for _, l := range layers {
        if l != nil && l.Name == name { return l }
    }
    return nil
}
```

**Never do `layers["transportation"]` — it's a slice, not a map. This was a major bug.**

Each `mvt.Layer` has `Features []*geojson.Feature`. Each feature has:
- `feature.Geometry` — `orb.Geometry` interface (Polygon, LineString, Point, etc.)
- `feature.Properties` — `map[string]interface{}` (the OSM tags for this feature)

Geometry coordinates are in **tile space** (float64 values 0–4096), NOT lat/lon,
NOT screen pixels. They look like integers (2048.0, 1024.0) because MVT uses integers
internally, but orb stores them as float64.

### Step 5: Style lookup

For each feature, look up its rendering style:
```go
class, _ := feature.Properties["class"].(string)
st, ok := style.StyleFor(layerName, class, zoom)
if !ok { continue }
```

`StyleFor` returns a `LayerStyle{DrawFill, DrawLine, FillColor, LineColor, MinZoom}`
where colors are xterm-256 indices (not RGB). The style table uses fixed xterm-256
indices (not computed via RGBToXterm256) so colors are predictable.

Layer draw order (bottom to top): `landcover → landuse → water → waterway →
boundary → transportation (minor first, then major) → building`

Transportation is drawn in two passes over the same layer — minor roads first,
then major roads on top, so hierarchy is respected.

### Step 6: Transform tile-space → screen pixels

```go
func tileToPixel(tileX, tileY float64, tr geo.TileRequest) (px, py int) {
    px = tr.PixelOffsetX + int(tileX * tr.Scale)
    py = tr.PixelOffsetY + int(tileY * tr.Scale)
    return
}
```

That's the entire coordinate transform. `PixelOffsetX` is where the tile's (0,0)
corner lands on screen (can be negative). `Scale` is pixels/tile-unit (typically
256.0/4096.0 = 0.0625 at the base zoom).

### Step 7: Rasterize into BrailleBuffer

**The braille trick:** each terminal character cell contains a 2×4 dot grid.
A terminal of W×H characters gives W×2 × H×4 effective "pixels". Unicode's Braille
Patterns block (U+2800–U+28FF) has 256 characters — one per combination of the 8 dots.

```
Dot bit mapping (col, row → bit value):
  col:  0     1
row 0: 0x01  0x08
row 1: 0x02  0x10
row 2: 0x04  0x20
row 3: 0x40  0x80

Character = U+2800 + (OR of all raised dot bits)
```

`SetPixel(px, py, color)`:
```go
charCol := px / 2;  dotCol := px % 2
charRow := py / 4;  dotRow := py % 4
buffer[charRow*width + charCol] |= dotBit[dotCol][dotRow]
```

Lines use Bresenham's algorithm (all-octant, integer only).
Polygons use scanline fill (for each y, find intersections, fill between pairs).

### Step 8: Render buffer to string

```go
for each cell:
    rune = 0x2800 + uint32(mask)
    if color != 0:
        "\x1b[38;5;{color}m{rune}\x1b[0m"
    else:
        "{rune}"
```

This string is what `View()` returns.

### Step 9: Label overlay (if implemented)

Labels are written on top of the braille frame using ANSI cursor positioning:
```
\x1b[{row};{col}H\x1b[38;5;{color}m{text}\x1b[0m
```

Labels come from the `place` and `transportation_name` MVT layers. Collision detection
uses a `map[[2]int]bool` of occupied character cells.

---

## 6. Bubble Tea Architecture — Key Patterns

### The Elm loop

```
Init() → [optional first Cmd]
         ↓
Update(Msg) → (newModel, Cmd)   ← called on every event
         ↓
View() → string                 ← called after every Update, must be instant
```

Bubble Tea spawns each `Cmd` in a goroutine. The goroutine returns a `Msg` through
a channel. The loop calls `Update(msg)` with it.

### The renderID pattern — CRITICAL

This is the most important architectural pattern in this codebase. Understand it
completely because it's been a source of bugs.

**The problem:** user presses a key → `renderCmd()` spawns goroutine → user presses
another key → another goroutine → first goroutine finishes with stale data. Without
protection, stale frames overwrite the latest frame.

**The solution:**
```go
type model struct {
    renderID int    // increments on every render kick-off
    frame    string // last accepted frame
}

// withRenderCmd MUST return the mutated model so renderID propagates.
// Cannot just call m.renderID++ inside renderCmd() — that modifies a copy.
func (m model) withRenderCmd() (tea.Model, tea.Cmd) {
    m.renderID++          // mutates the local copy
    id := m.renderID
    return m, m.renderCmd(id)  // returns mutated copy as new model
}

func (m model) renderCmd(id int) tea.Cmd {
    // capture all state needed for render — goroutine may run later
    lat, lon, zoom := m.lat, m.lon, m.zoom
    pixelW, pixelH := m.pixelW, m.pixelH
    db := m.db

    return func() tea.Msg {
        frame := render.Render(render.RenderRequest{...})
        return frameReadyMsg{id: id, frame: frame}
    }
}

// In Update:
case frameReadyMsg:
    if msg.id == m.renderID {  // only accept if not stale
        m.frame = msg.frame
    }
    return m, nil
```

**Why `withRenderCmd()` must return `(tea.Model, tea.Cmd)` and not just `tea.Cmd`:**
Go structs are value types. `m.renderID++` in a method that returns only `tea.Cmd`
modifies a copy that gets thrown away. The real model's `renderID` never updates.
The fix is returning the modified model so Bubble Tea replaces its tracked state.

**Usage in Update:**
```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height
    return m.withRenderCmd()  // returns (tea.Model, tea.Cmd)
```

### Custom message types

Every async result needs its own type. Pattern used throughout:

```go
type frameReadyMsg struct { id int; frame string }
type gtfsLoadedMsg  *gtfs.Feed
type routeFoundMsg  *gtfs.RouteResult
type statusMsg      string
```

### View() is always instant

`View()` is called after every single Update. It must return in microseconds.
**Never** read from SQLite, decode protobuf, or compute geometry in `View()`.

`View()` returns `m.frame` — the last string computed by a goroutine. Even if the
frame is stale (from a previous viewport position), returning it is correct. The user
sees the old frame while the new one renders.

### Pixel dimensions passed to renderer

The map panel is not full-terminal. It's `(totalWidth - sidebarWidth)` columns wide.
The renderer needs braille pixel dimensions, not character dimensions:

```go
pixelW = innerMapWidth  * 2  // 2 braille pixels per character column
pixelH = innerMapHeight * 4  // 4 braille pixels per character row
```

These must exactly match the panel dimensions computed in `View()`. If they don't
match, the rendered frame won't fit the panel — either clipped or padded wrong.

---

## 7. paulmach/orb — How It's Used

### Type conventions

```go
orb.Point{lon, lat}   // LON first, then lat. Not a typo. GeoJSON order.
```

This is the single most common gotcha. orb uses GeoJSON convention (longitude, latitude).

### Geometry type switch — always use this pattern

```go
switch g := feature.Geometry.(type) {
case orb.Point:          // single point
case orb.MultiPoint:     // collection of points
case orb.LineString:     // sequence of points (road, river)
case orb.MultiLineString: // collection of lines
case orb.Polygon:        // outer ring + optional hole rings
    // g[0] = outer ring ([]orb.Point)
    // g[1:] = hole rings
case orb.MultiPolygon:   // collection of polygons
    for _, poly := range g { ... }
case orb.Collection:     // heterogeneous collection
}
```

Never use bare type assertions on geometry. MultiPolygon and MultiLineString are
common in real OSM data.

### After MVT decode, coordinates are tile-space

After `mvt.Unmarshal()`, all geometry coordinates are in tile-space (0–4096 range),
stored as float64. They are NOT lat/lon. Do not call `layer.ProjectToWGS84()` —
convert tile-space directly to screen pixels for efficiency.

### Simplification

```go
simplifier := simplify.DouglasPeucker(4.0)
simplified := simplifier.Geometry(feature.Geometry)
```

The tolerance (4.0) is in tile-space units. At scale 256px/tile, one pixel ≈ 16
tile-units, so tolerance=4.0 keeps points that contribute at least ~0.25 pixels.
Scale the tolerance with zoom if needed.

### maptile.Fraction for viewport math

```go
frac := maptile.Fraction(orb.Point{lon, lat}, maptile.Zoom(zoom))
// frac[0] = e.g. 2928.713 (tile X + fraction within tile)
// frac[1] = e.g. 1703.204 (tile Y + fraction within tile)
centerTileX := int(math.Floor(frac[0]))
fracWithinX := frac[0] - math.Floor(frac[0]) // 0.0–1.0
```

### LatLonToPixel (for GTFS overlay)

This converts geographic coordinates directly to screen pixels, bypassing tile-space:

```go
func LatLonToPixel(lat, lon float64, vp Viewport) (px, py int) {
    const tilePixels = 256.0
    centerFrac := maptile.Fraction(orb.Point{vp.Lon, vp.Lat}, maptile.Zoom(vp.Zoom))
    pointFrac  := maptile.Fraction(orb.Point{lon, lat},        maptile.Zoom(vp.Zoom))
    dtx := pointFrac[0] - centerFrac[0]
    dty := pointFrac[1] - centerFrac[1]
    px = vp.PixelW/2 + int(dtx*tilePixels)
    py = vp.PixelH/2 + int(dty*tilePixels)
    return
}
```

Used for plotting GTFS station dots and metro lines without going through the MVT
tile coordinate system.

---

## 8. MBTiles and Offline Tile Data

### Schema

```sql
CREATE TABLE tiles (
    zoom_level  INTEGER,
    tile_column INTEGER,
    tile_row    INTEGER,    ← TMS order (y=0 at south pole)
    tile_data   BLOB        ← gzip-compressed MVT protobuf
);
CREATE TABLE metadata (name TEXT, value TEXT);
```

### Y-flip — the single most common bug

MBTiles tile_row is TMS order. Web maps use XYZ order (y=0 at north pole).
**Always flip before querying:**
```go
tmsY := (1 << zoom) - 1 - xyzY
```

If the map is blank or upside-down, this is why.

### Tile data format

`tile_data` is gzip-compressed MVT. The MBTiles reader gunzips it. After gunzip, pass
raw bytes to `mvt.Unmarshal(data)` — NOT `mvt.UnmarshalGzipped()` (that would try to
gunzip again and fail).

### OpenMapTiles layer schema

Layer names produced by tilemaker with OpenMapTiles config:
```
water           - oceans, lakes (polygons)
waterway        - rivers, canals (lines)
landcover       - forest, grass, farmland (polygons)
landuse         - parks, industrial, residential (polygons)
boundary        - country/state borders (lines)
transportation  - roads, rail (lines); class property = motorway/primary/etc
building        - buildings (polygons); shows at zoom 13+
place           - city/town/suburb names (points)
transportation_name - road names (points/lines)
water_name      - names for water bodies (points)
poi             - points of interest (points)
```

The `class` property on transportation features: `motorway`, `trunk`, `primary`,
`secondary`, `tertiary`, `residential`, `service`, `track`, `path`, `rail`, `subway`.

---

## 9. Current State — What Works

At the point metroshell was forked from ncr-on-terminal v1.0.0:

✅ Full-terminal braille map rendering of Delhi NCR  
✅ MBTiles SQLite reader with gzip decode and LRU cache  
✅ MVT protobuf decode via paulmach/orb  
✅ Hardcoded style table (layer+class → xterm-256 color)  
✅ Geometry rendering: lines (Bresenham), polygons (scanline fill)  
✅ Geometry simplification (Douglas-Peucker)  
✅ Viewport math: which tiles to load, pixel offsets, scale  
✅ hjkl panning, +/- zoom  
✅ renderID stale frame protection  
✅ withRenderCmd() pattern (renderID propagates correctly)  
✅ Bubble Tea v1 with WithAltScreen()  

Not yet in metroshell (Phase 4):  
❌ Two-panel layout (map + station sidebar)  
❌ Mouse pan/drag and scroll-to-zoom  
❌ GTFS parsing and station list  
❌ BFS route finding  
❌ Metro line overlay on map  
❌ Labels (place names on map)  
❌ SSH server  

---

## 10. Phase 4 — GTFS + Metro Routing

### Overview of what to build

```
┌─────────────────────────────────────┬────────────────────┐
│         Map Panel (braille)          │    Station Sidebar  │
│                                     │                     │
│  [Delhi NCR map rendered here]      │  Delhi Metro        │
│  [Metro lines overlaid]             │  Choose FROM        │
│  [Selected route highlighted]       │                     │
│  [Station dots]                     │ >[ ] Rajiv Chowk    │
│                                     │  [F] Kashmere Gate  │
│                                     │  [T] Huda City Cen  │
│                                     │                     │
│                                     │  Route: 12 stops    │
│                                     │  1 transfer         │
│                                     │  Yellow → Blue      │
└─────────────────────────────────────┴────────────────────┘
  hjkl=pan  +/-=zoom  Tab=switch  q=quit      ↑↓=scroll  Enter=select
```

### Phase 4 build order

1. Two-panel layout (map panel + sidebar), Tab to switch focus
2. GTFS parsing (stops, routes, trips, shapes)
3. Render all metro lines and station dots on map
4. Sidebar station list with scroll and selection
5. BFS route finder
6. Route highlight on map + route summary in sidebar
7. Mouse support (scroll=zoom, drag=pan)
8. Labels (place names at appropriate zoom levels)
9. SSH server

Build and test each step before moving to the next.

---

## 11. GTFS Data Model

### What GTFS is

A ZIP file containing CSV-like `.txt` files. A relational database stored as CSV.
Downloaded from: https://otd.delhi.gov.in/data/staticDMRC/

### File relationships

```
routes.txt   ←──route_id──┐
                           │
trips.txt    ←──trip_id───┤
    │                      │
    │ shape_id             │
    ▼                      │
shapes.txt              stop_times.txt ←──stop_id──→ stops.txt
(track geometry)        (sequence of stops per trip)  (lat/lon of stations)
```

### Key files and fields

**stops.txt** — 262 rows for DMRC
```
stop_id, stop_name, stop_lat, stop_lon
```

**routes.txt** — one per metro line
```
route_id, route_short_name (YL/BL/RL...), route_long_name, route_color (RRGGBB, no #)
```

**trips.txt** — many per route (one per scheduled run)
```
trip_id, route_id, direction_id (0/1), shape_id
```

**stop_times.txt** — ~411k rows (large file)
```
trip_id, stop_id, stop_sequence, arrival_time, departure_time
```

**shapes.txt** — actual track geometry (use this for rendering, not straight lines)
```
shape_id, shape_pt_lat, shape_pt_lon, shape_pt_sequence
```

### Graph construction from stop_times

Build the graph while parsing stop_times (do not store all 411k rows):

```go
// For each consecutive pair of stops on the same trip:
// AddEdge(stopA, stopB, routeID) — bidirectional, deduplicated
```

After parsing: ~260 nodes, ~500-600 edges. BFS on this takes ~0.1ms.

### BFS routing

```go
type RouteResult struct {
    Stops     []string // stop IDs from→to inclusive
    RouteIDs  []string // route used at each hop (len = len(Stops)-1)
    Transfers []int    // indices in Stops where line changes
}
```

BFS finds minimum stops. All edges weight 1 (metro stops are ~2-3min apart, uniform).
This is intentional — no need for RAPTOR or CSA at this stage.

### GTFS loading as a Cmd

Loading takes 1-3 seconds (411k rows). Always load as a Bubble Tea Cmd:

```go
func (m model) loadGTFSCmd() tea.Cmd {
    return func() tea.Msg {
        feed, err := gtfs.LoadZip("data/delhi-metro-gtfs.zip")
        if err != nil { return statusMsg("GTFS unavailable: " + err.Error()) }
        return gtfsLoadedMsg(feed)
    }
}
```

App starts immediately and shows map. Sidebar shows "Loading..." until feed arrives.
If the GTFS ZIP doesn't exist, `statusMsg` is returned and the map still works.

### Route color rendering

```go
func parseRouteColor(hex string) int {
    // hex is "FFFF00" (no #)
    rv, _ := strconv.ParseUint(hex[0:2], 16, 8)
    gv, _ := strconv.ParseUint(hex[2:4], 16, 8)
    bv, _ := strconv.ParseUint(hex[4:6], 16, 8)
    return braille.RGBToXterm256(uint8(rv), uint8(gv), uint8(bv))
}
```

All metro lines draw at dim brightness (0.4×). The selected route draws at full
brightness. The FROM station is a green cross. The TO station is a red cross.

---

## 12. Two-Panel Layout Plan

### Model additions for Phase 4

```go
type focusTarget int
const (focusMap focusTarget = iota; focusSidebar)

type selectionStep int
const (selectFrom selectionStep = iota; selectDestination)

type model struct {
    // existing: db, lat, lon, zoom, width, height, frame, renderID
    
    // Phase 4 additions:
    gtfsData    *gtfs.Feed
    sideStops   []gtfs.Stop       // sorted station list
    focus       focusTarget       // which panel has keyboard focus
    sideStep    selectionStep     // FROM or TO selection
    cursor      int               // current sidebar position
    sideScroll  int               // sidebar scroll offset
    fromStop    int               // selected from index (-1=none)
    toStop      int               // selected to index (-1=none)
    routeResult *gtfs.RouteResult // nil=no route yet
}
```

### Key routing: Tab switches focus, hjkl vs arrows

```go
case "tab":
    if m.focus == focusMap { m.focus = focusSidebar } else { m.focus = focusMap }
    return m, nil
default:
    if m.focus == focusSidebar { return m.updateSidebar(msg) }
    return m.updateMap(msg)
```

Map: `h/l` = pan left/right, `k/j` = pan up/down, `+/-` = zoom
Sidebar: `↑/↓` = scroll, `Enter` = select, `Esc/Backspace` = back to FROM

### Panel sizing

```go
sidebarWidth := clamp(totalWidth/4, 24, 36)
mapWidth := totalWidth - sidebarWidth

// Braille pixels for renderer:
pixelW = (mapWidth - panelBorderSize) * 2
pixelH = (height - panelBorderSize - 1) * 4  // -1 for status line
```

These pixel dimensions must match exactly what's passed to renderCmd.

### Focus indicator: border color changes

```go
activePanel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("212"))
inactivePanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
```

Use `activePanel` for the focused panel, `inactivePanel` for the other.

### Sidebar must not block View()

The sidebar content (station list, route summary) is computed directly in `View()`.
This is fine because it's just iterating a slice of strings — O(n) string ops, no I/O.

The map frame is computed in a goroutine and stored in `m.frame`. `View()` just returns
`m.frame`. Never compute the braille render in `View()`.

---

## 13. Critical Bugs Already Found and Fixed

### Bug 1: mvt.Layers is a slice, not a map

**Symptom:** Map renders almost completely blank — a few white lines at most.  
**Root cause:** `layers["transportation"]` was written assuming a map. `mvt.Layers`
is `[]*mvt.Layer`. String indexing silently returns zero value (nil), so all layer
lookups return nil.

**Fix:** Use `findLayer()` helper:
```go
func findLayer(layers mvt.Layers, name string) *mvt.Layer {
    for _, l := range layers {
        if l != nil && l.Name == name { return l }
    }
    return nil
}
```

**How to detect:** Add a one-time log: `for _, l := range layers { log.Printf("%q", l.Name) }`
Run it. If you see layer names, the decode is working. If you see zero output, the
tile data isn't being decoded at all (different problem: wrong Y-flip, empty tiles, etc.)

### Bug 2: renderID never updates — "Loading map..." forever

**Symptom:** App starts, "Loading map..." stays forever. No frame ever appears.  
**Root cause:**
```go
// WRONG — modifies a copy of m, not the real model
func (m model) renderCmd() tea.Cmd {
    m.renderID++   // this goes nowhere
    id := m.renderID  // always 1
    ...
}
// model.renderID in Bubble Tea's state is still 0
// frameReadyMsg{id:1} arrives but 1 != 0, so it's discarded
```

**Fix:** `withRenderCmd()` returns the mutated model:
```go
func (m model) withRenderCmd() (tea.Model, tea.Cmd) {
    m.renderID++
    id := m.renderID
    return m, m.renderCmd(id)  // returns mutated copy as new model
}

// Usage in Update — captures the new model:
case tea.WindowSizeMsg:
    m.width, m.height = msg.Width, msg.Height
    return m.withRenderCmd()  // (tea.Model, tea.Cmd) propagates new renderID
```

### Bug 3: TMS Y-flip forgotten

**Symptom:** All tile reads return `sql.ErrNoRows`. Map is blank.  
**Fix:** `tmsY := (1 << zoom) - 1 - xyzY` in the SQLite query.

### Bug 4: Polygon geometry panics on type assertion

**Symptom:** Panic on `feature.Geometry.(orb.Polygon)` for MultiPolygon features.  
**Fix:** Always use type switch, never bare type assertion on geometry.

### Bug 5: Stale Cmd results pile up on rapid keypresses

**Symptom:** After fast panning, map flickers to previous positions.  
**Fix:** The renderID pattern above. Stale `frameReadyMsg` with wrong ID are discarded.

---

## 14. Conventions and Patterns

### Color values

Use fixed xterm-256 indices, not computed RGB:
```go
// Good: predictable, no quantization surprise
const ColorPrimary = 214  // you know exactly what this looks like

// Avoid where possible:
braille.RGBToXterm256(255, 175, 0)  // might quantize to something unexpected
```

xterm-256 reference: https://www.ditig.com/256-colors-cheat-sheet

### Error handling in render path

The render path (tile read → MVT decode → geometry) uses `continue` on errors, not
`return`. A bad tile shouldn't crash the whole render. Log errors in debug mode but
don't surface them to the user during normal operation.

### Geometry before transform

Always simplify geometry in tile-space (before `tileToPixel`), not in screen-space.
Tile-space simplification is cheaper and the tolerance is scale-invariant.

### MBTiles cache

The cache in `tiles.DB` uses `map[string][]byte` keyed by `"z/x/y"`. This is an
unbounded cache. For a Delhi NCR dataset at zoom 8–14, this is fine — total unique
tiles in memory will be a few hundred MB at most during a session. For a worldwide
dataset this would be a problem. Do not add eviction logic prematurely.

### Import order in Go files

```go
import (
    // stdlib
    "fmt"
    "math"

    // external
    tea "github.com/charmbracelet/bubbletea"
    lg  "github.com/charmbracelet/lipgloss"
    "github.com/paulmach/orb"

    // internal
    "github.com/adot-7/metroshell/internal/braille"
    "github.com/adot-7/metroshell/internal/geo"
)
```

---

## 15. What NOT to Do

**Don't use `layers[layerName]`** — it's a slice. Use `findLayer()`.

**Don't compute the braille frame in `View()`** — it takes 50-500ms. `View()` must
return instantly. Compute frames in Cmds (goroutines), store in `m.frame`.

**Don't use goroutines directly** — use Bubble Tea Cmds. Raw goroutines writing to
model fields create data races. Cmds feed results through the single-threaded Update.

**Don't return only `tea.Cmd` from a zoom/pan handler** — you must return `(tea.Model,
tea.Cmd)` with the updated model. `withRenderCmd()` does this correctly.

**Don't commit `.mbtiles` or `.zip` files** — they're 100-400MB each. They're in
`.gitignore`. Document where to get them in README.

**Don't call `mvt.UnmarshalGzipped()` if your tile data is already gunzipped** —
the MBTiles reader gunzips in `ReadTile()`. Call `mvt.Unmarshal()` (not Gzipped).

**Don't add Mapbox style spec JSON parsing to Phase 4** — the hardcoded style table
works correctly, is fast, and is easy to modify. The style spec parser adds complexity
for no visible gain at this stage. It broke things in the previous attempt.

**Don't skip the renderID check to "simplify"** — without it, rapid keypresses cause
stale renders to arrive and overwrite the correct frame. The flicker is obvious and
users will notice.
