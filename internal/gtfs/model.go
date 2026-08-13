package gtfs

import (
	"context"
	"io/fs"
	"time"

	"github.com/paulmach/orb"
)

// Feed is the normalized subset of a static GTFS feed used by Metroshell.
// IDs retain their source values so downstream rendering and routing can make
// stable references without depending on source-file layout.
type Feed struct {
	Stops         []Stop
	Routes        []Route
	Trips         []Trip
	StopTimes     []StopTime
	Shapes        []ShapePoint
	Calendar      []Calendar
	CalendarDates []CalendarDate
}

// Stop is a station or platform represented by a stable ID and coordinates.
// ParentStationID is populated only when the source feed explicitly supplies
// parent_station. An empty value means this stop is not grouped with another
// stop; names and proximity are never used to infer a parent.
type Stop struct {
	ID              string
	Name            string
	Latitude        float64
	Longitude       float64
	ParentStationID string
}

// Route identifies a metro line and its optional official display color. Color
// is empty when route_color is absent or blank in the source feed.
type Route struct {
	ID          string
	DisplayName string
	Color       string
}

// Trip connects a route with the shape it follows. DirectionID is nil when the
// source feed does not supply direction metadata.
type Trip struct {
	ID          string
	RouteID     string
	ServiceID   string
	ShapeID     string
	DirectionID *int
}

// TripView is the renderer-facing view of a trip. StopIDs and StationIDs are
// aligned and retain the source stop occurrence order from stop_times.txt.
// The view is deliberately separate from Trip so the normalized feed remains
// a faithful representation of the input tables.
type TripView struct {
	ID          string
	LineID      string
	FamilyID    string
	ShapeID     string
	DirectionID *int
	ServiceID   string
	StopIDs     []string
	StationIDs  []string
	Stops       []ScheduledStop
}

// StationPlacement is one passenger-facing station's placement on one line
// shape. It contains all source stop and trip references which contributed to
// the placement, rather than making a renderer choose between them.
// SegmentIndex and SegmentFraction locate Point on the ordered shape geometry.
type StationPlacement struct {
	StationID       string
	LineID          string
	FamilyID        string
	ShapeID         string
	Point           orb.Point
	SegmentIndex    int
	SegmentFraction float64
	StopIDs         []string
	TripIDs         []string
}

// LineShape is the complete renderer input for one shape used by a line.
// Placements are sorted along Geometry, so rendering does not need to join
// routes, trips, stop_times, and shapes itself.
type LineShape struct {
	ShapeID    string
	LineIDs    []string
	Geometry   orb.LineString
	TripIDs    []string
	StationIDs []string
	Placements []StationPlacement
}

// StopTime places a stop in a trip's ordered sequence.
type StopTime struct {
	TripID           string
	StopID           string
	Sequence         int
	ArrivalTime      string
	DepartureTime    string
	ArrivalSeconds   int
	DepartureSeconds int
}

// Calendar is one GTFS weekly service rule. Dates are date-only values stored
// at UTC midnight; schedule calculations always interpret them in Delhi time.
type Calendar struct {
	ServiceID                                                      string
	StartDate                                                      time.Time
	EndDate                                                        time.Time
	Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday bool
}

// CalendarDate is a GTFS exception: 1 adds service and 2 removes it.
type CalendarDate struct {
	ServiceID     string
	Date          time.Time
	ExceptionType int
}

// ScheduledStop retains the timing association for one passenger-facing stop.
type ScheduledStop struct {
	StopID, StationID                string
	Sequence                         int
	ArrivalSeconds, DepartureSeconds int
}

// TripSchedule is the validated, indexed timing projection of one trip.
type TripSchedule struct {
	TripID, ServiceID, RouteID, FamilyID, ShapeID string
	Stops                                         []ScheduledStop
}

// SimulationServiceTiming is the immutable schedule timing projection for one
// service on one shape. Keeping intervals grouped by service lets callers
// select today's active services without rescanning every trip on each frame.
type SimulationServiceTiming struct {
	ServiceID string
	Intervals []time.Duration
}

// SimulationRoute is the immutable geometry/timing projection used by the
// pure simulator. It is built once with the feed indexes; active service
// selection remains a deterministic wall-clock concern of the app.
type SimulationRoute struct {
	FamilyID, RouteID, ShapeID string
	Geometry                   orb.LineString
	Timings                    []SimulationServiceTiming
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
type Loader interface {
	Load(ctx context.Context, source fs.FS) (Feed, error)
}
