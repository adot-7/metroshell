# Roadmap

Each phase should be split into small PRs with one observable behavior and its
tests. Merge order matters: keep the renderer stable while adding product state.

## Phase 0 — baseline and convergence

- Record the current map-only behavior and command invocation.
- Add test/lint commands and a minimal CI check.
- Migrate local and SSH entry points to Bubble Tea v2.
- Keep the existing map usable during the migration.

Acceptance: both entry points compile with the pinned v2 modules; a local map
renders from sample data; no Go source change is mixed with unrelated docs or
data work.

## Phase 1 — Delhi Metro data

- Define a small GTFS feed model for stops, routes, trips, stop times, and shapes.
- Load `data`/`mapdata` feeds asynchronously and report missing data clearly.
- Parse line colors and shapes; validate required fields and duplicate IDs.

Acceptance: a fixture feed loads without network access, malformed input returns
an actionable error, and every retained station has valid Delhi coordinates.

## Phase 2 — visual metro layer

- Draw line shapes and station dots over the map.
- Add a fixed, line-colored legend and selected-station styling.
- Add a map cursor with predictable movement and viewport clamping.

Acceptance: all fixture lines are distinguishable, station coordinates align with
the map, and the cursor remains visible at terminal sizes supported by the app.

## Phase 3 — route planning

- Add the FROM/TO sidebar and keyboard focus model.
- Support selecting either endpoint from the sidebar or map cursor.
- Build a deduplicated bidirectional graph from consecutive GTFS stop times.
- Run BFS and show stop count, transfers, and line sequence.

Acceptance: a known fixture route returns the minimum number of stops; unreachable
stations produce a useful empty state; changing either endpoint updates the map
highlight and summary without blocking `View()`.

## Phase 4 — simulated live layer

- Create deterministic train instances from route shapes and a simulation clock.
- Animate line-colored dots at a bounded update cadence.
- Pause or reduce motion when the terminal is too small or the app is unfocused.

Acceptance: the same seed and elapsed time produce the same train positions;
trains stay on their line geometry; animation does not change route results or
cause stale frames to overwrite newer viewport state.

## Phase 5 — local/SSH convergence and polish

- Exercise the same model through local and SSH sessions.
- Add mouse map-cursor selection where terminal support permits it.
- Improve empty, loading, resize, and missing-data states.
- Document data refresh and release procedures.

Acceptance: a scripted local session and an SSH session expose the same core
controls and route output; release builds remain CGO-free and reproducible.

