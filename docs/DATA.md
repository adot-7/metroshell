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

The intended Delhi Metro feed is the static DMRC feed referenced by the vision
notes. A feed supplies `stops.txt`, `routes.txt`, `trips.txt`, `stop_times.txt`,
and `shapes.txt`. The implementation should treat the feed as an input snapshot,
not as live service truth.

Required normalized fields:

| File | Use |
| --- | --- |
| `stops.txt` | stable station ID, name, latitude, longitude |
| `routes.txt` | line ID, display name, official color |
| `trips.txt` | route and shape relationships |
| `stop_times.txt` | ordered station adjacency per trip |
| `shapes.txt` | geographic line geometry for rendering |

Load asynchronously. Validate UTF-8/CSV headers, required fields, coordinates,
referenced IDs, and route colors. A missing or invalid feed should be visible to
the user and should not prevent the base map from starting.

## Simulation data

Simulation state is derived from route shapes and an explicit seed/clock. It is
ephemeral and must not be persisted as if it were operational DMRC data. Keep
the seed controllable in tests, and document any default seed or time origin.

## Provenance and refresh

Document the source URL, retrieval date, geographic extent, and feed version next
to any locally supplied dataset. Refreshing data is a maintainer operation; CI
should use fixtures, not an unpinned network download. Do not include personal
registration details, API keys, or credentials in the repository.

