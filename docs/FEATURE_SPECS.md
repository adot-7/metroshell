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

The sidebar lists Delhi Metro stations and indicates focus, FROM, and TO. Tab
changes focus between map and sidebar. Enter opens the endpoint picker, keyboard
navigation selects the current item, and Escape or Backspace backs out of an
endpoint choice. Mouse clicks, releases, and motion do not select stations or
move a map cursor. Mouse-wheel input remains a map-zoom control.

The UI distinguishes loading, no feed, feed error, no selection, no route, same
station, unreachable, and successful route states. Successful routes show
centered `JOURNEY` and `SCHEDULED` sections; `NEXT SERVICE` is labeled as an
offline GTFS timetable, with expired-calendar carry-forward visibly marked
estimated. A configured missing or invalid feed exposes `r` to retry while
preserving map-only behavior. The HUD exposes the active map/FROM/TO focus, and
selecting FROM visibly hands off to TO. The help and station-picker overlays
are bounded and input-trapping. The compact route view has no persistent shortcut hint, visible
`EXPANDED` label, or pre-leg `STATIONS` catalogue; Enter expands the focused leg
inline when detail is useful. A route with no compatible scheduled trip says
`NO SERVICE`; a feed without usable timing says `TIMING UNAVAILABLE`. It never
blocks in `View()` while loading or rendering.

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

## Presentation and data boundaries

The responsive launch splash uses a large code-native pixel wordmark, metro
emblem, small network motif, and the current GTFS startup state at normal
terminal sizes. Medium terminals retain the wordmark, while compact terminals
reduce to the exact visible copy `METROSHELL`, `DELHI METRO STARTING IN YOUR
TERMINAL`, and `built by Akash Parashar`. Every size remains bounded and
dismissible with Enter. Map and sidebar shells are neutral; pink accents
identify the MetroShell brand and launch identity. The sidebar clock includes
seconds and has no `DELHI` prefix. The application may show schedule-derived
train motion at the default 15× internal demo pace, but it never claims live
DMRC vehicle positions, realtime service status, or network-backed departures.
