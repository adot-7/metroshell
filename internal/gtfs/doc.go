// Package gtfs defines the normalized static transit-feed boundary used by
// Metroshell's Delhi Metro features.
//
// It deliberately has no dependency on the Bubble Tea app, map renderer, or
// routing implementation. Its parser reads the five required GTFS tables from
// an fs.FS; callers can use os.DirFS for a directory, an archive/zip.Reader,
// or an in-memory filesystem in tests. Loading may happen in an application
// command so UI updates remain asynchronous.
package gtfs
