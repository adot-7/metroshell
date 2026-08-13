# Metroshell product brief

## Product

Metroshell is an elegant, Delhi-only terminal metro visualizer and route planner.
It presents a quiet, high-contrast map of Delhi NCR with Delhi Metro lines,
stations, and simulated GTFS trains rendered along each line's shape. A FROM/TO
sidebar and keyboard station picker make route planning usable without leaving
the terminal.

The product is deliberately focused: Delhi Metro first, offline-friendly local
use, and the same Bubble Tea v2 behavior in a local terminal and over SSH. The
current implementation supplies this through a local MBTiles path and an
optional local static GTFS snapshot. Work should land through small, reviewable
PRs.

## User promise

From a terminal, a user can:

1. See a readable Delhi map with line-colored metro infrastructure.
2. Choose a FROM and TO station from the sidebar's keyboard picker.
3. Get the route with the fewest stops, including transfers and line changes.
4. Watch deterministic, schedule-shaped trains move along their lines as an
   offline visual layer.
5. Use the same controls locally or through the SSH server.

## Product boundaries

- Delhi only; do not generalize the data model for other cities yet.
- Simulated trains, not real-time DMRC vehicle positions or service alerts.
- Fewest stops is the routing objective; time, fares, accessibility, and crowding
  are future concerns.
- Local data is the default. Network access is not required at runtime.
- Terminal UI and map legibility take priority over feature breadth.

## Current release boundary

The current useful release starts with a local MBTiles map, loads optional Delhi
Metro GTFS data, allows keyboard-only endpoint selection, returns a fewest-stops
route, computes a static schedule-shaped journey, and animates deterministic
simulated trains without blocking the UI. The local and SSH entry points share
the same model and rendering behavior. The launch splash, neutral map/sidebar
shells, pink identity accents, seconds clock, and centered journey headings are
part of the shipped presentation.

`NEXT SERVICE` is derived from local static GTFS stop times and calendar rules;
it is not a live departure board or realtime DMRC service. The simulator is not
a live DMRC feed, and routing does not optimize travel time.
