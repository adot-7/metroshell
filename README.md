# Metroshell

Metroshell is a Delhi-only terminal metro visualizer and route planner. It
combines a local Delhi NCR braille map with an optional local static GTFS feed,
Delhi Metro lines and stations, a FROM/TO route-planning sidebar, and
deterministic simulated trains shown as line-colored moving dots. The local
terminal and SSH server construct the same Bubble Tea v2 application model.

The current app loads the optional feed asynchronously, builds its validated
indexes and route graph, draws the metro overlay and selected route, and supports
sidebar, keyboard-cursor, and map-click station selection. It reports loading,
missing, ready, and error states without making the base map unusable. Simulated
trains are an offline visual layer, not live DMRC positions or service status.

## Current map

The executable currently expects a local MBTiles path:

```text
go run . mapdata/delhi-ncr.mbtiles
```

To load a local GTFS directory or ZIP as well:

```text
go run . mapdata/delhi-ncr.mbtiles mapdata/delhi-metro/
```

Large map and GTFS archives are ignored under `mapdata/`. The base map and feed
are local and offline at runtime; see [docs/DATA.md](docs/DATA.md) for the
maintainer refresh, provenance, validation, and data-handling workflow.

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

Release builds are tag-driven through GoReleaser: the release workflow responds
to pushed `v*` tags and builds the local and Linux SSH binaries with
`CGO_ENABLED=0`. Deployment currently runs from pushes to `main`; changes to the
SSH server, Dockerfile, deployment workflow, or mounted runtime paths are
deployment-sensitive and need explicit review. See [docs/TESTING_AND_CI.md](docs/TESTING_AND_CI.md)
for local/SSH build, release, deploy, and smoke-check details.
