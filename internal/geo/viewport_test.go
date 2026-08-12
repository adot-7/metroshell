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

func TestViewportUnprojectInvertsProject(t *testing.T) {
	vp := Viewport{Lat: 28.6139, Lon: 77.2090, Zoom: 12.7, PixelW: 200, PixelH: 120}
	want := orb.Point{77.25, 28.58}
	x, y := vp.Project(want)
	got := vp.Unproject(x, y)
	if math.Abs(got[0]-want[0]) > 1e-9 || math.Abs(got[1]-want[1]) > 1e-9 {
		t.Fatalf("unproject(project(%v)) = %v, want %v", want, got, want)
	}
}

func TestViewportClampPointUsesVisiblePixelEdges(t *testing.T) {
	vp := Viewport{Lat: 28.6139, Lon: 77.2090, Zoom: 12, PixelW: 20, PixelH: 12}
	got := vp.ClampPoint(orb.Point{77.3, 28.7})
	x, y := vp.Project(got)
	if x < 0 || x > float64(vp.PixelW-1) || y < 0 || y > float64(vp.PixelH-1) {
		t.Fatalf("clamped point projects outside viewport: (%.3f, %.3f)", x, y)
	}
}

func TestFitBoundsUsesPaddingAndClampsSupportedZoom(t *testing.T) {
	bounds, ok := NewBounds([]orb.Point{{77.0, 28.5}, {77.3, 28.8}})
	if !ok {
		t.Fatal("route bounds were not created")
	}
	fallback := Viewport{Lat: 28.6, Lon: 77.1, Zoom: 12, PixelW: 400, PixelH: 200}
	fit, ok := FitBounds(bounds, fallback.PixelW, fallback.PixelH, 20, fallback)
	if !ok {
		t.Fatal("valid bounds did not fit")
	}
	if fit.Lat < bounds.MinLat || fit.Lat > bounds.MaxLat || fit.Lon < bounds.MinLon || fit.Lon > bounds.MaxLon {
		t.Fatalf("fit center = (%v,%v), outside bounds", fit.Lat, fit.Lon)
	}
	if fit.Zoom < minSupportedZoom || fit.Zoom > maxSupportedZoom {
		t.Fatalf("fit zoom = %v, outside supported range", fit.Zoom)
	}
	for _, point := range []orb.Point{{bounds.MinLon, bounds.MinLat}, {bounds.MaxLon, bounds.MaxLat}} {
		x, y := fit.Project(point)
		if x < 20-1e-9 || x > float64(fit.PixelW-20)+1e-9 || y < 20-1e-9 || y > float64(fit.PixelH-20)+1e-9 {
			t.Fatalf("point %v projected outside padded viewport: (%v,%v)", point, x, y)
		}
	}
}

func TestFitBoundsRejectsMissingGeometryAndTinyMap(t *testing.T) {
	fallback := Viewport{Lat: 28.6, Lon: 77.1, Zoom: 12, PixelW: 4, PixelH: 4}
	if _, ok := FitBounds(Bounds{}, 4, 4, 2, fallback); ok {
		t.Fatal("empty bounds unexpectedly fit")
	}
	bounds := Bounds{MinLon: 77, MinLat: 28, MaxLon: 77.1, MaxLat: 28.1}
	if got, ok := FitBounds(bounds, 4, 4, 2, fallback); ok || got != fallback {
		t.Fatalf("tiny map fit = %#v, %v; want unchanged fallback", got, ok)
	}
}
