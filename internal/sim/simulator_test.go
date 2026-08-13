package sim

import (
	"math"
	"reflect"
	"testing"
)

func TestSnapshotDeterministicAndImmutable(t *testing.T) {
	r := Route{FamilyID: "red", RouteID: "r", ShapeID: "s", Shape: []Point{{0, 0}, {10, 0}, {10, 10}}}
	c := Config{Seed: 4, Clock: 20, Fleet: 3, Routes: []Route{r}}
	a := Snapshot(c)
	b := Snapshot(c)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("snapshot is not deterministic")
	}
	c.Routes[0].Shape[0].Lon = 99
	if a[0].Position.Lon == 99 {
		t.Fatal("snapshot aliases input")
	}
}
func TestSnapshotBoundariesAndInvalidShapes(t *testing.T) {
	for _, shape := range [][]Point{nil, {{1, 2}}, {{1, 2}, {1, 2}}} {
		got := Snapshot(Config{Seed: 1, Fleet: 2, Routes: []Route{{RouteID: "x", Shape: shape}}})
		if len(got) != 2 {
			t.Fatal(len(got))
		}
		for _, tr := range got {
			if tr.Progress < 0 || tr.Progress > 1 {
				t.Fatal(tr)
			}
		}
	}
	if got := Snapshot(Config{Fleet: 99, Routes: []Route{{RouteID: "x", Shape: []Point{{0, 0}, {1, 1}}}}}); len(got) != 8 {
		t.Fatalf("fleet=%d", len(got))
	}
}
func TestPauseAndReducedMotionFreeze(t *testing.T) {
	r := Route{RouteID: "x", Shape: []Point{{0, 0}, {1, 0}}}
	for _, c := range []Config{{Seed: 1, Fleet: 1, Clock: 1, Paused: true, Routes: []Route{r}}, {Seed: 1, Fleet: 1, Clock: 1, ReducedMotion: true, Routes: []Route{r}}} {
		a := Snapshot(c)
		c.Clock = 999
		if !reflect.DeepEqual(a, Snapshot(c)) {
			t.Fatal("motion was not frozen")
		}
	}
}

func TestNormalClockFramesMoveOnTheirExactShape(t *testing.T) {
	route := Route{FamilyID: "blue", RouteID: "curve", ShapeID: "shape", Shape: []Point{{0, 0}, {1, 1}, {2, 0}}}
	a := Snapshot(Config{Seed: 9, Clock: 0, Fleet: 1, Routes: []Route{route}})
	b := Snapshot(Config{Seed: 9, Clock: 500000, Fleet: 1, Routes: []Route{route}})
	if len(a) != 1 || len(b) != 1 || a[0].Position == b[0].Position {
		t.Fatalf("normal clock frames did not move: %#v -> %#v", a, b)
	}
	for _, train := range append(a, b...) {
		if train.Position.Lat < 0 || train.Position.Lat > 1 || train.Position.Lon < 0 || train.Position.Lon > 2 {
			t.Fatalf("train left shape bounds: %#v", train)
		}
		// Every generated point is on one of the two shape segments.
		if !onSegment(train.Position, route.Shape[0], route.Shape[1]) && !onSegment(train.Position, route.Shape[1], route.Shape[2]) {
			t.Fatalf("train is not on shape geometry: %#v", train)
		}
	}
}

func TestTangentFollowsCurvedAndReversedOrderedShape(t *testing.T) {
	forward := Route{RouteID: "curve", Shape: []Point{{0, 0}, {1, 1}, {2, 0}}}
	reverse := Route{RouteID: "reverse", Shape: []Point{{2, 0}, {1, 1}, {0, 0}}}
	_, _, _, a := locate(forward.Shape, .25)
	_, _, _, b := locate(reverse.Shape, .25)
	if a != (Point{1, 1}) || b != (Point{-1, 1}) {
		t.Fatalf("ordered shape tangents = %#v/%#v", a, b)
	}
}

func TestClockCycleAndVisibleCadence(t *testing.T) {
	route := Route{RouteID: "line", ShapeID: "shape", Shape: []Point{{0, 0}, {1, 0}}}
	start := Snapshot(Config{Seed: 9, Clock: 0, Fleet: 1, Routes: []Route{route}})
	step := Snapshot(Config{Seed: 9, Clock: ClockCycle / 10, Fleet: 1, Routes: []Route{route}})
	cycle := Snapshot(Config{Seed: 9, Clock: ClockCycle, Fleet: 1, Routes: []Route{route}})
	if reflect.DeepEqual(start, step) {
		t.Fatal("one tenth of a cycle did not move the train")
	}
	if !reflect.DeepEqual(start, cycle) {
		t.Fatal("one full clock cycle did not repeat the deterministic frame")
	}
}

func onSegment(point, a, b Point) bool {
	cross := (point.Lat-a.Lat)*(b.Lon-a.Lon) - (point.Lon-a.Lon)*(b.Lat-a.Lat)
	if math.Abs(cross) > 1e-9 {
		return false
	}
	return point.Lon >= math.Min(a.Lon, b.Lon)-1e-9 && point.Lon <= math.Max(a.Lon, b.Lon)+1e-9 && point.Lat >= math.Min(a.Lat, b.Lat)-1e-9 && point.Lat <= math.Max(a.Lat, b.Lat)+1e-9
}

func TestNegativeClockKeepsProgressBounded(t *testing.T) {
	got := Snapshot(Config{Seed: 1, Clock: -999999999, Fleet: 10, Routes: []Route{{RouteID: "x", Shape: []Point{{0, 0}, {1, 0}}}}})
	for _, train := range got {
		if train.Progress < 0 || train.Progress > 1 {
			t.Fatalf("progress=%v", train.Progress)
		}
	}
}

func TestLargeSeedIDsAndDuplicateRoutesAreUnique(t *testing.T) {
	r := Route{FamilyID: "f", RouteID: "r", ShapeID: "s", Shape: []Point{{0, 0}, {1, 0}}}
	got := Snapshot(Config{Seed: ^uint64(0), Fleet: 3, Routes: []Route{r, r}})
	seen := map[string]bool{}
	for _, train := range got {
		if seen[train.ID] {
			t.Fatalf("duplicate train ID %q", train.ID)
		}
		seen[train.ID] = true
	}
	if len(got) != 3 {
		t.Fatalf("got %d trains, want 3", len(got))
	}
}
