# Testing and CI

## Test layers

The repository has focused GTFS unit and app component coverage in addition to
the compile/build path. The target test pyramid is:

- **Unit:** tile math, TMS Y-flip, braille mapping, GTFS parsing and validation,
  deterministic station/line/shape indexes, graph creation, BFS minimum-stop
  behavior, route colors, and deterministic train positions.
- **Component:** renderer composition with fixture tiles; sidebar selection,
  resize, GTFS loading/error/missing states, and empty states using a Bubble Tea
  test model.
- **Integration:** local and SSH entry points start, converge on the same model,
  and accept the documented key flows.
- **Manual:** inspect a Delhi map at small and large terminal sizes, verify line
  contrast, cursor selection, route highlight, and animation readability.

Fixtures must be tiny and synthetic where possible. Do not require a live GTFS
download or a 200 MB map archive in CI.

### Current GTFS checks

`internal/gtfs` tests load the committed `testdata/delhi-mini` fixture through
the filesystem boundary and verify that all five required tables and expected
headers are present. They also check Delhi-bounded coordinates, connected route,
trip, stop-time, and shape references, parsed direction metadata, ordered
records, ZIP loading, context cancellation, and useful failures for malformed
CSV, missing files/columns, bad coordinates, duplicate IDs, and duplicate
sequences.

Index tests verify deterministic ordering independent of source row order,
stable ID-keyed station/line/shape indexes, Delhi coordinate enforcement,
missing-reference rejection, line-to-shape association, and preservation plus
normalization of route colors. These tests exercise data preparation only; they
do not claim that visual station placement or route planning is implemented.

`internal/app/model_test.go` exercises the command flow around the boundary: a
configured directory fixture reaches `GTFS: ready`, a missing path falls back
to `GTFS: missing`, malformed input reaches visible `GTFS: error` without
blocking the map model, and `View` does not perform GTFS I/O. The fixture is
also used to assert the ready stop/line summary.

## Checks for every PR

The pull-request workflow currently runs these checks with the Go version from
`go.mod`:

```text
go test ./...
go vet ./...
go build ./...
```

The workflow also verifies that every tracked Go file is gofmt-clean. The
commands above are the authoritative CI checks; no network feed or large local
data file is required for them.

For renderer or terminal changes, also run the documented local demo with a
fixture or local MBTiles file and inspect resize, quit, and loading behavior.
Markdown-only changes should receive a link/structure review and do not need
runtime data. Go changes must pass the workflow's formatting check.

## CI expectations

CI should run on pull requests and pushes to the default branch, cache Go
modules, and fail on test, vet, or build errors. Release CI is currently tag
driven through GoReleaser; it builds CGO-free local and SSH binaries. The
existing deploy workflow runs on pushes to `main` and performs a remote Docker
deployment, so deployment changes require extra review and should not be hidden
inside feature PRs.

Docs-only PRs should still validate Markdown links/structure when a Markdown
checker is available and should not trigger large-data or runtime requirements.
