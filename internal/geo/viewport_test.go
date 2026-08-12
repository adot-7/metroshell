package geo

import (
	"math"
	"testing"

	"github.com/paulmach/orb"
)

func TestViewportProjectKeepsCenterAtScreenCenter(t *testing.T) {
	vp := Viewport{Lat: 28.6139, Lon: 77.2090, Zoom: 12.4, PixelW: 200, PixelH: 120}
	x, y := vp.Project(orb.Point{vp.Lon, vp.Lat})
	if math.Abs(x-100) > 1e-9 || math.Abs(y-60) > 1e-9 {
		t.Fatalf("center projected to (%.6f, %.6f), want (100, 60)", x, y)
	}
}

func TestViewportProjectMovesWithPanAndZoomMath(t *testing.T) {
	vp := Viewport{Lat: 28.6139, Lon: 77.2090, Zoom: 12, PixelW: 256, PixelH: 128}
	x1, y1 := vp.Project(orb.Point{77.2091, 28.6139})
	x2, y2 := vp.Project(orb.Point{77.2091, 28.6140})
	if !(x1 > 128 && math.Abs(y1-64) < 1e-6) {
		t.Fatalf("east point projected to (%.3f, %.3f), want right of center", x1, y1)
	}
	if !(y2 < 64 && math.Abs(x2-x1) < 1e-6) {
		t.Fatalf("north point projected to (%.3f, %.3f), want above center", x2, y2)
	}

	zoomed := vp
	zoomed.Zoom++
	zoomedX, _ := zoomed.Project(orb.Point{77.2091, 28.6139})
	if math.Abs(zoomedX-128) <= math.Abs(x1-128) {
		t.Fatalf("zoomed projection moved less: %.3f versus %.3f", zoomedX, x1)
	}
}

func TestViewportProjectWrapsLongitude(t *testing.T) {
	vp := Viewport{Lat: 0, Lon: 179.9, Zoom: 4, PixelW: 256, PixelH: 128}
	x, _ := vp.Project(orb.Point{-179.9, 0})
	if x < 0 || x > float64(vp.PixelW) {
		t.Fatalf("wrapped longitude projected off screen at %.3f", x)
	}
}
