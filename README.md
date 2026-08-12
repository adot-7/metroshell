# Metroshell

Metroshell is a Delhi-only terminal live metro visualizer and route planner. It
combines a local Delhi NCR braille map with Delhi Metro lines, stations, a
FROM/TO route-planning sidebar, and simulated GTFS trains shown as line-colored
moving dots. Local terminal and SSH sessions are intended to converge on one
Bubble Tea v2 application.

The repository is currently an early map-renderer foundation: the checked-in app
is a single-panel Bubble Tea v2 renderer with optional asynchronous loading of a
local GTFS snapshot. GTFS routing, the sidebar, train simulation, and complete
local/SSH convergence remain planned work.

## Current map

The executable currently expects a local MBTiles path:

```text
go run . mapdata/delhi-ncr.mbtiles
```

To load a local GTFS directory or ZIP as well:

```text
go run . mapdata/delhi-ncr.mbtiles mapdata/delhi-metro/
```

Large map and GTFS archives are ignored under `mapdata/`. The base map is local
and offline at runtime; see [docs/DATA.md](docs/DATA.md) for format, provenance,
and data-handling rules.

## Direction

The product direction and acceptance criteria are in:

- [Product brief](docs/PRODUCT_BRIEF.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Roadmap](docs/ROADMAP.md)
- [Feature specs](docs/FEATURE_SPECS.md)
- [Data notes](docs/DATA.md)
- [Testing and CI](docs/TESTING_AND_CI.md)
- [Agent orchestration](docs/AGENT_ORCHESTRATION.md)

The historical handoff at `vision.md` is useful renderer context, but parts of
it are stale; the current repository state and discrepancies are called out in
the architecture and roadmap documents.

## Development workflow

Work through small PRs. Keep each change focused, include acceptance criteria
and tests/checks in the PR description, and do not commit MBTiles, GTFS ZIPs,
SSH keys, or other generated/secret data. The baseline checks are:

```text
go test ./...
go vet ./...
go build ./...
```

Release builds are tag-driven through GoReleaser. Deployment currently runs from
pushes to `main`; deployment-affecting changes need explicit review.
