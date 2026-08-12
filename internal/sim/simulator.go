// Package sim provides a pure train-position simulator for GTFS shapes.
package sim

import (
	"hash/fnv"
	"math"
	"sort"

	"github.com/paulmach/orb"
)

// Point is a shape coordinate in longitude, latitude order.
type Point struct{ Lon, Lat float64 }

// Shape is an ordered route shape. Shape points are copied when a simulation
// is created; callers may safely reuse or mutate their input after that call.
type Shape struct {
	ID     string
	Points []Point
}

// Route describes the metadata shared by generated trains.
type Route struct {
	FamilyID, RouteID, ShapeID string
	Shape                      []Point
}

// Config fixes every input affecting output. Seed and Clock are intentionally
// explicit so callers can replay a frame without wall-clock or network state.
type Config struct {
	Seed          uint64
	Clock         int64
	Fleet         int
	Paused        bool
	ReducedMotion bool
	Routes        []Route
}

// Train is an immutable snapshot. Progress is in [0,1], and Position is on
// the selected shape (including exact endpoints).
type Train struct {
	ID, FamilyID, RouteID, ShapeID string
	Position                       Point
	Progress                       float64
	Segment                        int
	SegmentFraction                float64
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
			phase = math.Mod(phase+float64(c.Clock%100000)/1000000, 1)
		}
		p, seg, frac := locate(r.Shape, phase)
		out = append(out, Train{ID: trainID(c.Seed, r, i), FamilyID: r.FamilyID, RouteID: r.RouteID, ShapeID: r.ShapeID, Position: p, Progress: phase, Segment: seg, SegmentFraction: frac})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func routeKey(r Route) string { return r.FamilyID + "\x00" + r.RouteID + "\x00" + r.ShapeID }
func trainID(seed uint64, r Route, i int) string {
	return routeKey(r) + "\x00" + itoa(i) + "\x00" + itoa(int(seed))
}
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	s := ""
	for v > 0 {
		s = string(rune('0'+v%10)) + s
		v /= 10
	}
	return s
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
func locate(points []Point, progress float64) (Point, int, float64) {
	if len(points) == 0 {
		return Point{}, 0, 0
	}
	if len(points) == 1 {
		return points[0], 0, 0
	}
	lengths := make([]float64, len(points)-1)
	total := 0.0
	for i := range lengths {
		a, b := points[i], points[i+1]
		lengths[i] = math.Hypot(b.Lon-a.Lon, b.Lat-a.Lat)
		total += lengths[i]
	}
	if total == 0 {
		return points[0], 0, 0
	}
	d := progress * total
	for i, l := range lengths {
		if d <= l || i == len(lengths)-1 {
			f := 0.0
			if l > 0 {
				f = d / l
			}
			return Point{points[i].Lon + (points[i+1].Lon-points[i].Lon)*f, points[i].Lat + (points[i+1].Lat-points[i].Lat)*f}, i, f
		}
		d -= l
	}
	return points[len(points)-1], len(points) - 2, 1
}

var _ = orb.Point{}
