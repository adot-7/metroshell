# Data notes

## Map data

The base map is a Delhi NCR MBTiles SQLite file containing gzip-compressed Mapbox
Vector Tiles. Runtime reads are local and offline. Large `.mbtiles`, `.pmtiles`,
and feed archives belong in the ignored `mapdata/` area and must never be
committed. The repository currently contains only `mapdata/.gitkeep`.

The renderer must preserve the MBTiles TMS row convention: convert an XYZ tile
row with `tmsY = (1 << zoom) - 1 - xyzY` before querying SQLite. After the reader
gunzips tile bytes, decode them once as MVT. Geometry is in tile space until it
is transformed to screen pixels.

## GTFS

The application accepts a local static GTFS snapshot as the optional second
argument:

```text
go run . mapdata/delhi-ncr.mbtiles mapdata/delhi-metro.zip
go run . mapdata/delhi-ncr.mbtiles mapdata/delhi-metro/
```

The path may name either a ZIP archive or an unpacked directory. There is no
network download, polling, or live-service lookup. A feed is an input snapshot,
not live service truth. Large archives belong in the ignored `mapdata/` area and
must not be committed.

The loader boundary is `internal/gtfs`: it reads an `fs.FS`, so directory-backed
feeds, ZIP readers, and in-memory test files use the same parser. The package
owns normalization and validation only; it does not depend on the Bubble Tea
application, map renderer, or route planner. `gtfs.BuildIndexes` then turns the
accepted feed into deterministic station, line, and shape indexes for callers.

### Required local feed

The local feed must contain these five files at its root:
`stops.txt`, `routes.txt`, `trips.txt`, `stop_times.txt`, and `shapes.txt`.
The parser accepts additional GTFS columns, but requires these headers and
fields:

Required normalized fields:

| File | Use |
| --- | --- |
| `stops.txt` | stable station ID, name, latitude, longitude |
| `routes.txt` | line ID, display name, optional official color |
| `trips.txt` | route and shape relationships |
| `stop_times.txt` | ordered station adjacency per trip |
| `shapes.txt` | geographic line geometry for rendering |

The current parser expects the following required columns: `stop_id`,
`stop_name`, `stop_lat`, `stop_lon`; `route_id`, `route_long_name`, and the
optional `route_color` (plus the standard route short-name column); `route_id`,
`trip_id`, and `shape_id`; `trip_id`, `stop_id`, and `stop_sequence`; and `shape_id`,
`shape_pt_lat`, `shape_pt_lon`, and `shape_pt_sequence`, respectively. IDs must
be unique within their table. The index builder additionally requires every
station and shape point to fall within the Delhi-NCR bounds (28.3–29.0 latitude,
76.8–77.6 longitude), checks trip and stop-time references, checks non-negative
and non-duplicate sequences, and normalizes populated route colors to `#RRGGBB` while
retaining the source value. Uncolored routes receive the deterministic,
renderer-safe fallback color `#808080` in the derived indexes.

The parser reports malformed CSV, missing files or headers, empty required
values, invalid coordinates or sequences, duplicate IDs, invalid colors, and
unknown references with the source filename and (where applicable) line and
field. Context cancellation is honored while reading. Validation is all-or-
left to the upstream GTFS producer; the parser does not currently perform a
separate encoding conversion or normalization step. An invalid snapshot is not
exposed as a partial feed.

### Fixture policy

`internal/gtfs/testdata/delhi-mini` is the committed canonical fixture. It is
small, synthetic, deterministic, and deliberately not a DMRC timetable or claim
about real line geometry. It contains only the five required tables with
plausible Delhi-area coordinates and connected references. Tests should prefer
this fixture or tiny in-memory variants; they must not fetch a live feed or
depend on a large archive. Maintainers may keep a real local feed for manual
inspection, but must record its source URL, retrieval date, geographic extent,
and version outside the committed fixture.

### Application loading and failures

When a GTFS path is configured, `internal/app` starts loading from `Init` via a
Bubble Tea command. File I/O, parsing, and index construction stay outside
`Update` and `View`. The HUD reports `GTFS: loading` during the command,
`GTFS: ready (N stops, M lines)` after a successful snapshot, and a compact
`GTFS: error (...)` for an existing invalid/unreadable feed. A blank path or a
missing path produces `GTFS: missing` and keeps the map-only mode usable. Feed
errors clear feed state and do not prevent the base map from starting; they are
not silently converted into a ready or partial index.

## Simulation data

Simulation state is derived from route shapes and an explicit seed/clock. It is
ephemeral and must not be persisted as if it were operational DMRC data. Keep
the seed controllable in tests, and document any default seed or time origin.

## Provenance and refresh

Document the source URL, retrieval date, geographic extent, and feed version next
to any locally supplied dataset. Refreshing data is a maintainer operation; CI
should use fixtures, not an unpinned network download. Do not include personal
registration details, API keys, or credentials in the repository.

## Risks before visual metro rendering

- `Indexes` associates routes with ordered shape IDs through trips, but it does
  not yet provide a station-to-shape projection or station-to-line membership
  suitable for drawing. Phase 2 must define that renderer-facing contract
  instead of inferring it in `View` (tracked in issue #13).
- The current normalized subset treats each GTFS stop as a station and does not
  include `parent_station`. Production feeds may contain platform-level stops or
  interchange variants, so Phase 2 must decide whether to group them before
  labels and dots are rendered (tracked in issue #14).
- Delhi-NCR bounds are intentionally conservative validation bounds, not a complete
  geographic authority. A future feed refresh that legitimately extends the
  supported NCR extent will require an explicit contract and fixture update.
