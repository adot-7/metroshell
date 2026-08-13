# Feature specifications

## Delhi map

The map is a braille-rendered Delhi NCR base layer from local MBTiles. Panning,
zooming, resize, and tile failures must not crash the process. A missing optional
metro feed leaves the base map usable and shows a clear data status.

## Metro layer and simulated trains

GTFS route shapes define the line geometry; station coordinates define station
dots. Each route uses its GTFS line color. Simulated trains are dots moving along
that route geometry, not claims about live vehicle positions. Simulation is
deterministic under a seed and clock input, bounded to valid geometry, and
visually subordinate to a selected route. It is an offline visual simulation,
not live DMRC vehicle or service data.

## FROM/TO selection

The sidebar lists Delhi Metro stations and indicates focus, cursor, FROM, and TO.
Tab changes focus between map and sidebar. Enter selects the current item; Escape
or Backspace backs out of an endpoint choice. A map cursor can select the nearest
station and must provide a visible nearest-station/selection affordance.

The UI distinguishes loading, no feed, feed error, no selection, no route, same
station, unreachable, and successful route states. The help and station-picker
overlays are bounded and input-trapping. It never blocks in `View()` while
loading or rendering.

## Routing

Build a bidirectional graph from consecutive stops in each trip, deduplicating
edges while retaining route identity. BFS returns a path with the minimum number
of stop-to-stop hops. The result includes stations from origin through
destination, line IDs per hop, transfers, and a human-readable summary.

If multiple minimum-stop paths exist, use a stable tie-breaker based on sorted
station/route IDs so output is repeatable. Do not imply that BFS minimizes travel
time.

## Local and SSH

The local binary and SSH server share the same Bubble Tea v2 application model,
commands, styles, data loading, and route behavior. Only terminal/session setup
may differ. SSH tolerates narrower terminals through shared resize/focus/overlay
state handling and must not expose host keys, map archives, or feed credentials
in logs. The server's host-key path and mounted data paths are deployment
configuration, not application data.
