# Architecture

## Current repository state

The repository currently contains a single-panel Bubble Tea v1 application and
the renderer packages that came from the `ncr-on-terminal` fork:

- `main.go` owns the model, keyboard/mouse handling, viewport state, and frame
  display.
- `internal/geo` calculates map tile and viewport geometry.
- `internal/tiles` reads gzip-compressed MVT data from SQLite MBTiles.
- `internal/render` decodes and rasterizes vector tiles.
- `internal/braille` converts pixels to terminal braille cells.
- `internal/style` maps OpenMapTiles layers to xterm colors.
- `cmd/sshserver` and `Dockerfile.sshserver` provide the SSH direction, but the
  product convergence is not complete.

There is no `internal/gtfs` package, station sidebar, route graph, train
simulation, or committed test suite yet. The current app is map-only and
requires an MBTiles path as its command-line argument.

## Target shape

```text
Bubble Tea v2 program
  ├─ shared model/update/view
  ├─ map viewport + async render commands
  ├─ sidebar selection and route summary
  ├─ GTFS feed, graph, and deterministic simulator
  └─ renderer: tiles + metro lines + stations + trains + cursor
        ├─ local terminal entry point
        └─ Wish SSH entry point
```

The local and SSH programs should construct the same application model and use
the same Bubble Tea v2 view. Transport-specific setup belongs at the entry
point; product behavior must not be forked between local and SSH modes.

## Rendering rules

Rendering stays outside `View()`. Bubble Tea commands perform tile reads, MVT
decoding, geometry work, and frame composition; `View()` returns the last
accepted frame plus lightweight sidebar text. Preserve stale-frame protection
with a monotonically increasing render ID. Tile-space geometry is simplified
before conversion to screen pixels, and MBTiles TMS Y coordinates are flipped
before lookup.

The target draw order is map base, metro lines, stations, route highlight,
simulated trains, cursor, and labels. Line colors come from GTFS route metadata,
with accessible contrast and a dimmed base layer so the selected route remains
clear.

## Bubble Tea v2 convergence

The vision document is stale on this point: it describes Bubble Tea v1 APIs,
while `go.mod` already lists `charm.land/bubbletea/v2` and `lipgloss/v2`.
Migration should be an explicit early task. Confirm v2 APIs against the pinned
module before changing behavior, then remove obsolete v1 dependencies once all
entry points compile and tests pass.

