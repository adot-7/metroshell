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
owns normalization, validation, and deterministic graph preparation; it does
not depend on the Bubble Tea application or map renderer. `gtfs.BuildIndexes`
then turns the accepted feed into deterministic station, line, shape, and
routing indexes for callers.

### Required local feed

The local feed must contain these five files at its root:
`stops.txt`, `routes.txt`, `trips.txt`, `stop_times.txt`, and `shapes.txt`.
The parser accepts additional GTFS columns, but requires these headers and
fields:

Required normalized fields:

| File | Use |
| --- | --- |
| `stops.txt` | stable stop ID, name, latitude, longitude, optional explicit `parent_station` |
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

### Station display policy

The normalized/rendering contract is **explicit-parent grouping**:

- A source stop with an empty `parent_station` remains its own passenger-facing
  station, keyed by its source `stop_id`.
- A source stop with `parent_station` is displayed under that explicit parent;
  the parent ID is the stable station ID. The parent must be a source stop and
  may not itself have a parent. The station keeps sorted `StopIDs` for the
  parent and all child platforms, so source IDs remain available to consumers.
- Names, coordinates, prefixes, and proximity are never used to infer groups.
  A feed that omits `parent_station` therefore renders every stop separately,
  even when names look alike or coordinates are close.
- `StopToStation` maps every source stop ID, including platform IDs, to its
  display station. A station's sorted `LineIDs` is the union of routes serving
  any represented stop, so an interchange retains all line membership. The
  display coordinate and name are taken from the explicit parent row;
  standalone stations use their own row.

This policy is covered by the committed `testdata/platform-grouped` synthetic
fixture. It deliberately uses distinct platform names and coordinates to prove
that grouping depends on `parent_station`, not raw-name heuristics.

### Renderer association projection

`BuildIndexes` also publishes the complete renderer-facing association in
`Indexes.Lines[*].Shapes`, `Indexes.Trips`, and `Indexes.StationPlacements`:

- `TripView` retains the source trip ID, line/route ID, shape ID, direction,
  and ordered source `StopIDs` plus their passenger-facing `StationIDs`.
- Each `LineShape` contains ordered shape geometry, sorted contributing
  `TripIDs`, and `StationPlacements` sorted along that geometry. A line can
  expose multiple shapes when its trips use multiple shape IDs.
- A placement is emitted once per `(station, line, shape)` pair. Multiple trips
  sharing that pair are merged while retaining sorted source `TripIDs` and
  `StopIDs`. `Indexes.StationPlacements[stationID]` provides the inverse view,
  sorted by line ID, shape ID, and shape position. Thus an interchange has one
  placement for each served line/shape pair rather than an arbitrary single
  placement.
- Placement is the nearest point on the ordered shape polyline to the
  passenger-facing station coordinate (longitude/latitude). The projection is
  clamped to each segment. Exact ties retain the earliest shape segment, and a
  zero-length shape segment is handled deterministically. `SegmentIndex` and
  `SegmentFraction` locate the result; `Point` is the projected coordinate.

These derived associations are deterministic and are the only contract a
renderer needs for line geometry and station placement. View/render composition
must not reconstruct relationships by joining raw GTFS tables. Source IDs are
never replaced by display names or coordinates.

### Route graph projection

`Indexes.Graph` is the routing projection built from grouped `TripView` station
sequences. Each consecutive pair contributes one undirected edge; grouped
platform IDs therefore produce one passenger-facing adjacency. Self-edges are
ignored and duplicate trips/edges are merged. `RouteGraph.Edges` is sorted by
`FromStationID`, then `ToStationID`; `Neighbors` and `Adjacency` contain both
directions with sorted neighbor order. Every grouped station, including an
unreachable station, is retained with an empty adjacency list when applicable.

Each edge retains sorted canonical line-family metadata plus raw route and trip
IDs, both on the edge and within each family. Missing station, route, trip, or
family references fail graph construction with an explicit error; an empty
`Indexes` value produces a valid empty graph. Construction happens with index
preparation, outside `View`, so an async feed-ready update can publish the graph
as one complete snapshot.

### Fixture policy

`internal/gtfs/testdata/delhi-mini` and `internal/gtfs/testdata/platform-grouped`
are committed synthetic fixtures. They are small, synthetic, deterministic, and
deliberately not a DMRC timetable or claim about real line geometry. Each
contains only the five required tables with
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
to any locally supplied dataset. Use this maintainer-only record outside Git (for
example in a private operations note):

```text
Dataset: Delhi GTFS / Delhi NCR MBTiles
Source URL: <exact URL used for retrieval>
Retrieved: <YYYY-MM-DD>
Geographic extent: <Delhi NCR bounds or provider extent>
Feed/version metadata: <publisher, feed version, effective date, checksum if useful>
Local path: <mapdata/...; ignored>
Notes: <license/attribution and any refresh caveats>
```

Do not invent a source URL: record the provider URL actually used by the
maintainer. Refreshing data is a maintainer operation; CI should use the
committed synthetic fixtures, not an unpinned network download. Do not include
personal registration details, API keys, or credentials in the repository.

### Local refresh and validation

1. Record the provenance template above before downloading. Retrieve the Delhi
   GTFS snapshot and MBTiles file from their approved provider URLs into a
   temporary directory; inspect the archive contents before extracting.
2. Put the ignored archive or unpacked feed under `mapdata/`, for example
   `mapdata/delhi-metro.zip` or `mapdata/delhi-metro/`, and put the MBTiles file
   at `mapdata/delhi-ncr.mbtiles`. Keep both paths local; do not add them to the
   index. An unpacked feed must have these five files at its root:
   `stops.txt`, `routes.txt`, `trips.txt`, `stop_times.txt`, and `shapes.txt`.
3. Run the parser/index validation against the committed fixture and run the
   full Go checks. The application performs the same load and index build for a
   local directory or ZIP when started with the commands in the README:

   ```text
   go test ./internal/gtfs ./internal/app
   go test ./...
   go vet ./...
   go build ./...
   ```

   A configured local feed must reach `GTFS: ready`; missing files, malformed
   CSV, invalid references, duplicate IDs/sequences, or coordinates outside the
   supported Delhi-NCR bounds must remain a visible `GTFS: error` and must not
   produce a partial feed.
4. With a valid MBTiles file, manually inspect decoded tile layers and sample
   geometry using the repository diagnostic command:

   ```text
   go run ./cmd/inspect
   ```

   Then start the application with the local MBTiles and feed paths, exercise
   resize/quit, station selection, route output, and the loading/ready HUD:

   ```text
   go run . mapdata/delhi-ncr.mbtiles mapdata/delhi-metro/
   # or use mapdata/delhi-metro.zip as the second argument
   ```

5. On refresh, replace the ignored local snapshot only after recording the new
   retrieval date, extent, feed/version metadata, and any parser/index result.
   Never commit archives, generated data, unpacked local feeds, MBTiles,
   binaries, logs, or diagnostic output. The local data is for maintainer
   validation and manual inspection, not a repository fixture.

## Data contract risks and boundaries

- Renderers consume the deterministic line/shape/trip/station projection above;
  UI work must not infer these relationships from raw GTFS tables.
- The normalized contract groups only explicit GTFS `parent_station` children;
  production feeds with missing or inconsistent parent metadata remain
  ungrouped or fail index validation rather than being guessed from names.
- Delhi-NCR bounds are intentionally conservative validation bounds, not a complete
  geographic authority. A future feed refresh that legitimately extends the
  supported NCR extent will require an explicit contract and fixture update.
