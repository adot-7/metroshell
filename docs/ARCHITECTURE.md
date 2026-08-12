# Architecture

## Current repository state

The repository currently contains a single-panel Bubble Tea v2 application and
the renderer packages that came from the `ncr-on-terminal` fork:

- `main.go` opens the required MBTiles path, accepts an optional local GTFS
  directory or ZIP path, and constructs the shared application model.
- `internal/app` owns the model, keyboard/mouse handling, viewport state, frame
  display, and asynchronous GTFS feed lifecycle.
- `internal/geo` calculates map tile and viewport geometry.
- `internal/tiles` reads gzip-compressed MVT data from SQLite MBTiles.
- `internal/render` decodes and rasterizes vector tiles.
- `internal/braille` converts pixels to terminal braille cells.
- `internal/style` maps OpenMapTiles layers to xterm colors.
- `cmd/sshserver` and `Dockerfile.sshserver` provide the SSH direction, but the
  product convergence is not complete.

`internal/gtfs` defines the normalized Phase 1 feed model, filesystem-based
loader contract, parser, validation, deterministic indexes, and small synthetic
fixture. It has no dependency on the app or renderer. There is no station
sidebar, route graph, or train simulation yet. The app requires an MBTiles path
and remains map-only when no usable GTFS path is supplied.

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

## Current app/data integration

When a GTFS path is configured, `Model.Init` starts a command that reads either
`os.DirFS(path)` or a ZIP archive, calls `gtfs.Load`, and then calls
`gtfs.BuildIndexes`. The command returns a ready, missing, or error message;
`Update` commits that message and `View` only renders the current state. A feed
failure therefore remains visible in the HUD without preventing the map model
from running. Metro shapes, stations, route planning, and simulation are still
future consumers of the indexes, not responsibilities of the loader.
