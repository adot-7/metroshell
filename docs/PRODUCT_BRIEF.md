# Metroshell product brief

## Product

Metroshell is an elegant, Delhi-only terminal live metro visualizer and route
planner. It presents a quiet, high-contrast map of Delhi NCR with Delhi Metro
lines, stations, and simulated GTFS trains rendered as small moving dots in each
line's color. A FROM/TO sidebar and map-cursor selection make route planning
usable without leaving the terminal.

The product is deliberately focused: Delhi Metro first, offline-friendly local
use, and the same Bubble Tea v2 experience in a local terminal and over SSH.
Work should land through small, reviewable PRs.

## User promise

From a terminal, a user can:

1. See a readable Delhi map with line-colored metro infrastructure.
2. Choose a FROM and TO station from the sidebar or by moving a map cursor.
3. Get the route with the fewest stops, including transfers and line changes.
4. Watch simulated trains move along their lines as a visual live layer.
5. Use the same controls locally or through the SSH server.

## Product boundaries

- Delhi only; do not generalize the data model for other cities yet.
- Simulated trains, not real-time DMRC vehicle positions or service alerts.
- Fewest stops is the routing objective; time, fares, accessibility, and crowding
  are future concerns.
- Local data is the default. Network access is not required at runtime.
- Terminal UI and map legibility take priority over feature breadth.

## Success criteria

The first useful release should start with a local MBTiles map, load optional
Delhi Metro GTFS data, allow station selection, return a fewest-stops route,
and animate deterministic simulated trains without blocking the UI. The local
and SSH entry points should share the same model and rendering behavior.

