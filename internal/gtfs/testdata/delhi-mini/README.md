# Delhi mini GTFS fixture

This is a tiny, fully synthetic static GTFS fixture for parser, validation,
rendering, and routing tests. Its stations and shapes use plausible Delhi-area
coordinates, but it is not derived from or a claim about a DMRC timetable,
service pattern, or line geometry.

The fixture intentionally contains only the five tables Metroshell requires:
`stops.txt`, `routes.txt`, `trips.txt`, `stop_times.txt`, and `shapes.txt`.
It is committed so tests never need `mapdata/DMRC_GTFS.zip` or a network fetch.
