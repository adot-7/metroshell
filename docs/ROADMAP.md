# Roadmap

Each phase should be split into small PRs with one observable behavior and its
tests. Merge order matters: keep the renderer stable while adding product state.

Phases 0–6 below describe the behavior now present on `main`; they are retained
as a history of product boundaries rather than a list of unimplemented work.

## Phase 0 — baseline and convergence (complete)

- Record the current map-only behavior and command invocation.
- Add test/lint commands and a minimal CI check.
- Migrate local and SSH entry points to Bubble Tea v2.
- Keep the existing map usable during the migration.

Acceptance: both entry points compile with the pinned v2 modules; a local map
renders from sample data; no Go source change is mixed with unrelated docs or
data work.

## Phase 1 — Delhi Metro data (complete)

- Define a small GTFS feed model for stops, routes, trips, stop times, and shapes.
- Load `data`/`mapdata` feeds asynchronously and report missing data clearly.
- Parse line colors and shapes; validate required fields and duplicate IDs.

Acceptance: a fixture feed loads without network access, malformed input returns
an actionable error, and every retained station has valid Delhi coordinates.

## Phase 2 — visual metro layer (complete)

- Draw line shapes and station dots over the map.
- Add line-colored station styling and selected-station treatment.
- Keep station and line placement legible over the base map.

Acceptance: all fixture lines are distinguishable and station coordinates align
with the map at supported terminal sizes.

## Phase 3 — route planning (complete)

- Add the FROM/TO sidebar and keyboard focus model.
- Support selecting either endpoint from the sidebar's keyboard picker.
- Build a deduplicated bidirectional graph from consecutive GTFS stop times.
- Run BFS and show stop count, transfers, and line sequence.

Acceptance: a known fixture route returns the minimum number of stops; unreachable
stations produce a useful empty state; changing either endpoint updates the map
highlight and summary without blocking `View()`.

## Phase 4 — simulated offline layer (complete)

- Create deterministic train instances from route shapes and static schedule
  durations.
- Animate line-colored trains at a bounded update cadence and the default 15×
  internal demo pace.
- Pause or reduce motion when the terminal is too small or the app is unfocused.

Acceptance: the same seed and elapsed time produce the same train positions;
trains stay on their line geometry; animation does not change route results or
cause stale frames to overwrite newer viewport state.

## Phase 5 — local/SSH convergence and bounded state handling (complete)

- Exercise the same model through local and SSH sessions.
- Keep mouse input limited to wheel zoom; endpoint selection remains keyboard-only.
- Improve empty, loading, resize, and missing-data states.
- Document data refresh and release procedures.

Acceptance: a scripted local session and an SSH session expose the same core
controls and route output; state transitions keep views bounded; release builds
remain CGO-free and reproducible.

## Phase 6 — release presentation and static schedule semantics (complete)

- Ship the bounded launch splash with the exact Akash-only credit.
- Use neutral map/sidebar shells with pink identity accents, a seconds clock, and
  centered `JOURNEY` / `SCHEDULED` headings.
- Present static GTFS `NEXT SERVICE` output and schedule-shaped train motion
  without suggesting live DMRC data.
- Keep README and maintainer docs aligned with local/SSH parity and offline data
  prerequisites.

Acceptance: release documentation describes the shipped controls and data
boundaries without claiming realtime service.

## Deferred product boundaries

- Simulated offline trains must not be presented as live DMRC positions, vehicle
  telemetry, or service alerts.
- Routing remains deterministic BFS for fewest stop-to-stop hops. It is not
  travel-time, fare, accessibility, or crowding optimization.
- Realtime DMRC service, vehicle telemetry, alerts, and automatic network data
  refresh remain outside the product boundary. Do not present static GTFS
  schedule output as live service.
