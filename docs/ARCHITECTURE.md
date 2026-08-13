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
- `cmd/sshserver` and `Dockerfile.sshserver` provide the SSH entry point. Each
  Wish session constructs the same `internal/app` model used by the local
  executable; transport and PTY setup are the entry-point differences.

`internal/gtfs` defines the normalized feed model, filesystem-based
loader contract, parser, validation, deterministic indexes, and small synthetic
fixture. It has no dependency on the app or renderer. The app requires an
MBTiles path and remains map-only when no usable GTFS path is supplied. With a
ready feed, the shared model renders line shapes and stations, plans routes
through the prepared graph, and supplies deterministic train snapshots to the
renderer. Station grouping in the indexes uses only an explicit GTFS
`parent_station`; names and proximity are never used to infer passenger-facing
stations.

## Current composition

```text
Bubble Tea v2 program
  ├─ shared model/update/view
  ├─ map viewport + async render commands
  ├─ sidebar, picker, cursor/click selection, and route summary
  ├─ GTFS feed, graph, and deterministic simulator
  └─ renderer: tiles + metro lines + stations + route + trains + cursor
        ├─ local terminal entry point
        └─ Wish SSH entry point
```

The local and SSH programs construct the same application model and use the same
Bubble Tea v2 view. Transport-specific setup belongs at the entry point; product
behavior is not forked between local and SSH modes. The parity test replays feed
loading, route selection, resize, focus, and bounded overlay behavior through
both paths.

## Rendering rules

Rendering stays outside `View()`. Bubble Tea commands perform tile reads, MVT
decoding, geometry work, and frame composition; `View()` returns the last
accepted frame plus lightweight sidebar text. Preserve stale-frame protection
with a monotonically increasing render ID. Tile-space geometry is simplified
before conversion to screen pixels, and MBTiles TMS Y coordinates are flipped
before lookup.

The draw order is map base, metro lines, stations, route highlight, simulated
trains, cursor, and labels. Line colors come from prepared GTFS metadata, with a
deterministic gray fallback for uncolored routes. The selected route and station
receive an accent without changing the underlying line ownership.

## Interaction and Phase 5 state behavior

`Tab` cycles map, FROM, and TO focus. Enter opens the station picker for an
endpoint, and keyboard navigation selects a station. A left click in the map
area moves the cursor and selects the nearest station within the renderer's
hit radius; the first click fills FROM and the next fills TO, independent of
sidebar focus. Mouse-wheel events zoom the map.

The help screen and station picker are bounded overlays. They trap their input,
and the underlying map/sidebar remains the background rather than being replaced
by a blank full-screen state. Loading, missing-feed, feed-error, no-endpoint,
same-station, unreachable, and ready route states are represented in the HUD or
sidebar. Feed I/O, parsing, index construction, route planning, and frame
composition remain outside `View()`.

Simulation starts only after a feed is ready. It uses seed `41`, a fleet size of
`24`, and a 250 ms tick; it is paused when unfocused, while help/picker is open,
or below 20×8, and uses reduced motion below 52×16. State changes invalidate
queued render/tick work and restart only an eligible simulation generation.

## Current app/data integration

When a GTFS path is configured, `Model.Init` starts a command that reads either
`os.DirFS(path)` or a ZIP archive, calls `gtfs.Load`, and then calls
`gtfs.BuildIndexes`. The command returns a ready, missing, or error message;
`Update` commits that message and `View` only renders the current state. A feed
failure therefore remains visible in the HUD without preventing the map model
from running. On success, the complete immutable indexes include transit overlay
associations and the route graph consumed by the renderer and app model; the
loader still does not perform UI or rendering work.
