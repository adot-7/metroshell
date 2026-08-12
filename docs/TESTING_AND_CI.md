# Testing and CI

## Test layers

The project currently has no committed automated test files, so the baseline is
the existing compile/build path plus manual rendering with local map data. The
target test pyramid is:

- **Unit:** tile math, TMS Y-flip, braille mapping, GTFS parsing, graph creation,
  BFS minimum-stop behavior, route colors, and deterministic train positions.
- **Component:** renderer composition with fixture tiles; sidebar selection,
  resize, loading, and empty states using a Bubble Tea test model.
- **Integration:** local and SSH entry points start, converge on the same model,
  and accept the documented key flows.
- **Manual:** inspect a Delhi map at small and large terminal sizes, verify line
  contrast, cursor selection, route highlight, and animation readability.

Fixtures must be tiny and synthetic where possible. Do not require a live GTFS
download or a 200 MB map archive in CI.

## Checks for every PR

Run the repository's pinned Go toolchain checks as they become available:

```text
go test ./...
go vet ./...
go build ./...
```

For renderer or terminal changes, also run the documented local demo with a
fixture or local MBTiles file and inspect resize, quit, and loading behavior.
Once formatting is part of CI, include `gofmt` verification for Go changes.

## CI expectations

CI should run on pull requests and pushes to the default branch, cache Go
modules, and fail on test, vet, or build errors. Release CI is currently tag
driven through GoReleaser; it builds CGO-free local and SSH binaries. The
existing deploy workflow runs on pushes to `main` and performs a remote Docker
deployment, so deployment changes require extra review and should not be hidden
inside feature PRs.

Docs-only PRs should still validate Markdown links/structure when a Markdown
checker is available and should not trigger large-data or runtime requirements.

