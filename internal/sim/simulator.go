// Package sim provides a pure train-position simulator for GTFS shapes.
package sim

import (
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"time"
)

// Point is a shape coordinate in longitude, latitude order.
type Point struct{ Lon, Lat float64 }

// Route describes the metadata shared by generated trains.
type Route struct {
	FamilyID, RouteID, ShapeID string
	Shape                      []Point
	// TravelTime is the schedule-shaped duration of the complete geometry.
	// A zero value retains the original deterministic cycle behavior for
	// geometry-only callers.
	TravelTime time.Duration
	// CumulativeLengths and TotalLength are optional immutable geometry
	// preparation. Snapshot uses them when supplied to avoid rescanning a
	// long shape for every train on every frame.
	CumulativeLengths []float64
	TotalLength       float64
}

// ClockCycle is one complete deterministic animation cycle. Config.Clock is
// expressed in these arbitrary clock units, not nanoseconds or wall time.
// Keeping the unit explicit lets the app choose a visible cadence while tests
// and SSH sessions remain fully replayable.
const ClockCycle int64 = 1_000_000

// Config fixes every input affecting output. Seed and Clock are intentionally
// explicit so callers can replay a frame without wall-clock or network state.
// Clock is measured in ClockCycle units; one cycle wraps back to the same
// phase.
type Config struct {
	Seed          uint64
	Clock         int64
	Fleet         int
	Paused        bool
	ReducedMotion bool
	// Acceleration is an internal demo multiplier. It is deliberately not a
	// second clock: Clock remains the only animation input exposed to callers.
	Acceleration float64
	Routes       []Route
}

// Train is an immutable snapshot. Progress is in [0,1], and Position is on
// the selected shape (including exact endpoints).
type Train struct {
	ID, FamilyID, RouteID, ShapeID string
	Position                       Point
	// Tangent is the ordered-shape direction at Position. It is derived from
	// the current shape segment, so reversing a GTFS shape reverses the train
	// orientation without introducing a second direction model.
	Tangent         Point
	Progress        float64
	Segment         int
	SegmentFraction float64
}

// Snapshot deterministically generates at most Fleet trains, sorted by ID.
// Empty, one-point, and degenerate shapes produce valid endpoint snapshots.
// Paused and ReducedMotion both freeze motion at the seed-derived phase.
func Snapshot(c Config) []Train {
	if c.Fleet < 0 {
		c.Fleet = 0
	}
	routes := append([]Route(nil), c.Routes...)
	sort.SliceStable(routes, func(i, j int) bool { return routeKey(routes[i]) < routeKey(routes[j]) })
	for i := range routes {
		if len(routes[i].CumulativeLengths) != len(routes[i].Shape)-1 || routes[i].TotalLength <= 0 {
			routes[i] = PrepareRoute(routes[i])
		}
	}
	if len(routes) == 0 || c.Fleet == 0 {
		return []Train{}
	}
	n := c.Fleet
	if n > len(routes)*8 {
		n = len(routes) * 8
	} // bounded policy: eight per shape
	out := make([]Train, 0, n)
	for i := 0; i < n; i++ {
		r := routes[i%len(routes)]
		phase := unit(c.Seed, routeKey(r), i)
		if !c.Paused && !c.ReducedMotion {
			phase += motionPhase(c, r)
			phase = math.Mod(phase, 1)
			if phase < 0 {
				phase++
			}
		}
		p, seg, frac, tangent := locate(r.Shape, phase)
		out = append(out, Train{ID: trainID(c.Seed, r, i), FamilyID: r.FamilyID, RouteID: r.RouteID, ShapeID: r.ShapeID, Position: p, Tangent: tangent, Progress: phase, Segment: seg, SegmentFraction: frac})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PrepareRoute returns a detached route with cumulative geometry lengths. It
// is pure and deterministic; callers may retain the result across snapshots.
func PrepareRoute(r Route) Route {
	r.Shape = append([]Point(nil), r.Shape...)
	r.CumulativeLengths = make([]float64, maxInt(len(r.Shape)-1, 0))
	total := 0.0
	for i := range r.CumulativeLengths {
		total += math.Hypot(r.Shape[i+1].Lon-r.Shape[i].Lon, r.Shape[i+1].Lat-r.Shape[i].Lat)
		r.CumulativeLengths[i] = total
	}
	r.TotalLength = total
	return r
}

func motionPhase(c Config, route Route) float64 {
	if route.TravelTime <= 0 {
		return math.Mod(float64(c.Clock), float64(ClockCycle)) / float64(ClockCycle)
	}
	acceleration := c.Acceleration
	if acceleration <= 0 {
		acceleration = 1
	}
	// ClockCycle is one animation second. This keeps the existing deterministic
	// clock contract while letting schedule durations control train cadence.
	return float64(c.Clock) / float64(ClockCycle) * acceleration / route.TravelTime.Seconds()
}

func routeKey(r Route) string { return r.FamilyID + "\x00" + r.RouteID + "\x00" + r.ShapeID }
func trainID(seed uint64, r Route, i int) string {
	return routeKey(r) + "\x00" + strconv.Itoa(i) + "\x00" + strconv.FormatUint(seed, 10)
}
func itoa(v int) string {
	return strconv.Itoa(v)
}
func unit(seed uint64, key string, i int) float64 {
	h := fnv.New64a()
	var b [8]byte
	for j := 0; j < 8; j++ {
		b[j] = byte(seed >> (8 * j))
	}
	_, _ = h.Write(b[:])
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte(itoa(i)))
	return float64(h.Sum64()) / float64(^uint64(0))
}
func locate(points []Point, progress float64) (Point, int, float64, Point) {
	return locatePrepared(Route{Shape: points}, progress)
}

func locatePrepared(route Route, progress float64) (Point, int, float64, Point) {
	points := route.Shape
	if len(points) == 0 {
		return Point{}, 0, 0, Point{}
	}
	if len(points) == 1 {
		return points[0], 0, 0, Point{}
	}
	cumulative := route.CumulativeLengths
	total := route.TotalLength
	if len(cumulative) != len(points)-1 || total <= 0 {
		prepared := PrepareRoute(route)
		cumulative, total = prepared.CumulativeLengths, prepared.TotalLength
	}
	if total == 0 {
		return points[0], 0, 0, Point{}
	}
	d := progress * total
	previous := 0.0
	for i, end := range cumulative {
		l := end - previous
		if d <= l || i == len(cumulative)-1 {
			f := 0.0
			if l > 0 {
				f = d / l
			}
			return Point{points[i].Lon + (points[i+1].Lon-points[i].Lon)*f, points[i].Lat + (points[i+1].Lat-points[i].Lat)*f}, i, f, segmentTangent(points, i)
		}
		d -= l
		previous = end
	}
	return points[len(points)-1], len(points) - 2, 1, segmentTangent(points, len(points)-2)
}

func segmentTangent(points []Point, segment int) Point {
	if segment < 0 {
		segment = 0
	}
	if segment >= len(points)-1 {
		segment = len(points) - 2
	}
	if segment < 0 {
		return Point{}
	}
	// A repeated GTFS point has no direction. Search in ordered geometry for
	// the nearest non-degenerate segment, preferring the forward segment.
	for distance := 0; distance < len(points); distance++ {
		for _, candidate := range []int{segment + distance, segment - distance} {
			if candidate < 0 || candidate >= len(points)-1 {
				continue
			}
			tangent := Point{Lon: points[candidate+1].Lon - points[candidate].Lon, Lat: points[candidate+1].Lat - points[candidate].Lat}
			if tangent.Lon != 0 || tangent.Lat != 0 {
				return tangent
			}
		}
	}
	return Point{}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
