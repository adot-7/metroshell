package sim

import (
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
