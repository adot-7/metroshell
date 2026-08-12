package gtfs

import (
	"context"
	"io/fs"
)

// Feed is the normalized subset of a static GTFS feed used by Metroshell.
// IDs retain their source values so downstream rendering and routing can make
// stable references without depending on source-file layout.
type Feed struct {
	Stops     []Stop
	Routes    []Route
	Trips     []Trip
	StopTimes []StopTime
	Shapes    []ShapePoint
}

// Stop is a station or platform represented by a stable ID and coordinates.
type Stop struct {
	ID        string
	Name      string
	Latitude  float64
	Longitude float64
}

// Route identifies a metro line and its official display color.
type Route struct {
	ID          string
	DisplayName string
	Color       string
}

// Trip connects a route with the shape it follows.
type Trip struct {
	ID      string
	RouteID string
	ShapeID string
}

// StopTime places a stop in a trip's ordered sequence.
type StopTime struct {
	TripID   string
	StopID   string
	Sequence int
}

// ShapePoint is one ordered geographic point on a trip shape.
type ShapePoint struct {
	ShapeID   string
	Latitude  float64
	Longitude float64
	Sequence  int
}

// Loader parses the required GTFS tables from source into a normalized Feed.
// Implementations must honor ctx and validate source data before returning it.
// Required files are stops.txt, routes.txt, trips.txt, stop_times.txt, and
// shapes.txt.
//
// The parser implementation is intentionally deferred to the next Phase 1
// work item. This interface is the contract it will satisfy.
type Loader interface {
	Load(ctx context.Context, source fs.FS) (Feed, error)
}
