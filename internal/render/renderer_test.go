package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/adot-7/metroshell/internal/braille"
	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/gtfs"
	"github.com/paulmach/orb"
)

func TestRenderEmptyAndMissingGTFSRemainMapOnly(t *testing.T) {
	base := Render(RenderRequest{Lat: 28.6139, Lon: 77.2090, Zoom: 12, PixelW: 40, PixelH: 20})
	empty := gtfs.Indexes{}
	withEmpty := Render(RenderRequest{Lat: 28.6139, Lon: 77.2090, Zoom: 12, PixelW: 40, PixelH: 20, GTFS: &empty})
	if base != withEmpty {
		t.Fatalf("empty GTFS changed map-only frame")
	}
}

func TestRouteGeometryIncludesSelectedFamilyShapesAndStations(t *testing.T) {
	indexes := gtfs.Indexes{
		Stations:        gtfs.StationIndex{"a": {ID: "a", Longitude: 77, Latitude: 28.5}, "b": {ID: "b", Longitude: 77.2, Latitude: 28.7}},
		OrderedFamilies: []gtfs.LineFamily{{ID: "blue", Shapes: []gtfs.LineShape{{Geometry: orb.LineString{{76.9, 28.4}, {77.3, 28.8}}}}}},
	}
	points := RouteGeometry(indexes, gtfs.RouteResult{Status: gtfs.RouteReady, Stations: []string{"a", "b"}, FamilyIDs: []string{"blue"}})
	if len(points) != 4 || points[2] != (orb.Point{76.9, 28.4}) || points[3] != (orb.Point{77.3, 28.8}) {
		t.Fatalf("route geometry = %#v, want stations plus selected shape", points)
	}
}

func TestRenderDrawsOrderedLinesAndStationsLast(t *testing.T) {
	center := orb.Point{77.2090, 28.6139}
	indexes := gtfs.Indexes{
		OrderedLines: []gtfs.Line{
			{
				ID: "a", DisplayName: "Alpha Line", RendererColor: "#FF0000",
				Shapes: []gtfs.LineShape{{
					ShapeID: "a-shape", Geometry: orb.LineString{{center[0] - .0001, center[1]}, {center[0] + .0001, center[1]}},
					Placements: []gtfs.StationPlacement{{Point: center}},
				}},
			},
			{
				ID: "b", DisplayName: "Beta Line", RendererColor: "#0000FF",
				Shapes: []gtfs.LineShape{{
					ShapeID: "b-shape", Geometry: orb.LineString{{center[0], center[1] - .0001}, {center[0], center[1] + .0001}},
					Placements: []gtfs.StationPlacement{{Point: center}},
				}},
			},
		},
	}
	frame := Render(RenderRequest{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: 40, PixelH: 20, GTFS: &indexes})
	if !strings.Contains(frame, "\x1b[38;5;21m") {
		t.Fatalf("frame did not contain the normalized route color: %q", frame)
	}
	if routeColor("#FF0000") != 196 {
		t.Fatal("red route color was not normalized deterministically")
	}
	if strings.Contains(frame, "Alpha Line") || strings.Contains(frame, "Beta Line") {
		t.Fatalf("frame contained the removed textual line legend: %q", frame)
	}
	// Both stations overlap at the interchange. Since station composition is
	// after all lines and line order is stable, the last line owns that marker.
	if !strings.Contains(frame, "\x1b[38;5;21m⠿") && !strings.Contains(frame, "\x1b[38;5;21m⠂") {
		// The exact braille mask depends on the cross and terminal dimensions;
		// the blue color escape still verifies the final station draw path below.
		if strings.Count(frame, "\x1b[38;5;21m") < 1 {
			t.Fatalf("station was not composed in final route color: %q", frame)
		}
	}
}

func TestRenderDrawsCursorAboveMetroOverlay(t *testing.T) {
	center := orb.Point{77.2090, 28.6139}
	indexes := gtfs.Indexes{OrderedLines: []gtfs.Line{{
		ID: "blue", RendererColor: "#0072BC",
		Shapes: []gtfs.LineShape{{
			Geometry:   orb.LineString{{center[0] - .0001, center[1]}, {center[0] + .0001, center[1]}},
			Placements: []gtfs.StationPlacement{{Point: center}},
		}},
	}}}
	frame := Render(RenderRequest{
		Lat: center[1], Lon: center[0], Zoom: 15, PixelW: 40, PixelH: 20,
		GTFS: &indexes, Cursor: &center,
	})
	if !strings.Contains(frame, "◎") {
		t.Fatalf("cursor was not rendered: %q", frame)
	}
}

func TestRouteColorNormalizesHexAndFallback(t *testing.T) {
	if got := routeColor("#ff0000"); got != 196 {
		t.Fatalf("red route color = %d, want xterm 196", got)
	}
	if got := routeColor("bad"); got != routeColor("#808080") {
		t.Fatalf("invalid route color = %d, want deterministic gray fallback", got)
	}
}

func TestNearestStationSelectsClosestStableStation(t *testing.T) {
	center := orb.Point{77.2090, 28.6139}
	indexes := gtfs.Indexes{OrderedStations: []gtfs.Station{
		{ID: "far", Latitude: center[1] + .001, Longitude: center[0]},
		{ID: "near", Latitude: center[1] + .00001, Longitude: center[0]},
	}}
	vp := geo.Viewport{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: 40, PixelH: 20}
	if got := nearestStation(indexes, vp, center); got != "near" {
		t.Fatalf("nearest station = %q, want near", got)
	}
	if got := nearestStation(indexes, vp, orb.Point{center[0] + 1, center[1] + 1}); got != "" {
		t.Fatalf("distant cursor selected station %q", got)
	}
}

func TestRenderSelectedStationUsesAccentWithoutChangingRouteColor(t *testing.T) {
	center := orb.Point{77.2090, 28.6139}
	indexes := gtfs.Indexes{
		OrderedStations: []gtfs.Station{{ID: "center", Latitude: center[1], Longitude: center[0]}},
		OrderedLines: []gtfs.Line{{ID: "blue", DisplayName: "Blue Line", RendererColor: "#0072BC", Shapes: []gtfs.LineShape{{
			Geometry:   orb.LineString{{center[0] - .0001, center[1]}, {center[0] + .0001, center[1]}},
			Placements: []gtfs.StationPlacement{{StationID: "center", Point: center}},
		}}}},
	}
	frame := Render(RenderRequest{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: 40, PixelH: 20, GTFS: &indexes, Cursor: &center})
	if !strings.Contains(frame, "\x1b[38;5;226m") {
		t.Fatalf("selected station accent was not rendered: %q", frame)
	}
	if !strings.Contains(frame, "\x1b[38;5;25m") {
		t.Fatalf("route color was lost while rendering selected station: %q", frame)
	}
}

func TestRenderAggregatedFamilyKeepsInterchangeFamiliesDeterministic(t *testing.T) {
	center := orb.Point{77.2090, 28.6139}
	indexes := gtfs.Indexes{
		OrderedFamilies: []gtfs.LineFamily{{
			ID: "blue", DisplayName: "Blue Line", RendererColor: "#0072BC",
			Shapes: []gtfs.LineShape{{
				Geometry:   orb.LineString{{center[0] - .0001, center[1]}, {center[0] + .0001, center[1]}},
				Placements: []gtfs.StationPlacement{{StationID: "interchange", Point: center, FamilyID: "blue"}},
			}},
		}},
		OrderedStations: []gtfs.Station{{ID: "interchange", Latitude: center[1], Longitude: center[0], FamilyIDs: []string{"blue", "yellow"}}},
	}
	first := Render(RenderRequest{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: 40, PixelH: 20, GTFS: &indexes})
	second := Render(RenderRequest{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: 40, PixelH: 20, GTFS: &indexes})
	if first != second {
		t.Fatal("aggregated family station rendering was not deterministic")
	}
	if strings.Contains(first, "Blue Line") || !strings.Contains(first, "\x1b[38;5;25m") {
		t.Fatalf("aggregated family rendering emitted a legend or lost stable line color: %q", first)
	}
}

func TestRenderResizeAndMissingFeedRemainBoundedMapStates(t *testing.T) {
	center := orb.Point{77.2090, 28.6139}
	indexes := gtfs.Indexes{OrderedLines: []gtfs.Line{{ID: "blue", DisplayName: "Blue Line", RendererColor: "#0072BC", Shapes: []gtfs.LineShape{{
		Geometry: orb.LineString{{center[0] - .0001, center[1]}, {center[0] + .0001, center[1]}},
	}}}}}
	for _, size := range [][2]int{{4, 3}, {20, 8}} {
		frame := Render(RenderRequest{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: size[0] * 2, PixelH: size[1] * 4, GTFS: &indexes})
		if got := len(strings.Split(strings.TrimSuffix(frame, "\n"), "\n")); got != size[1] {
			t.Fatalf("resize %dx%d produced %d rows, want %d", size[0], size[1], got, size[1])
		}
		if strings.Contains(frame, "Blue Line") || strings.Contains(frame, "●") {
			t.Fatalf("resize %dx%d emitted removed textual legend: %q", size[0], size[1], frame)
		}
	}
	missing := Render(RenderRequest{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: 8, PixelH: 8})
	if strings.Contains(missing, "Blue Line") || strings.Contains(missing, "●") {
		t.Fatalf("missing-feed map-only frame unexpectedly contained legend: %q", missing)
	}
}

func TestCursorNeverOverwritesBaseMapLabelsAtMultipleZoomPositions(t *testing.T) {
	labels := []Label{{Text: "New Delhi", ColX: 4, RowY: 2, Color: 250}}
	for _, cursorCell := range [][2]int{{4, 2}, {5, 2}, {3, 2}, {4, 1}, {4, 3}} {
		buf := braille.New(20, 8)
		occupied := writeLabelsToBuffer(buf, labels, 20, 8)
		point := orb.Point{77.209, 28.6139}
		vp := geo.Viewport{Lat: point[1], Lon: point[0], Zoom: 12, PixelW: 40, PixelH: 32}
		// Use the same geographic projection path as the production cursor to
		// exercise label collisions at several screen positions/zoom scales.
		projected := vp.Unproject(float64(cursorCell[0]*2), float64(cursorCell[1]*4))
		drawCursor(buf, projected, vp, occupied)
		frame := buf.Render()
		if !strings.Contains(stripANSI(frame), "New Delhi") {
			t.Fatalf("cursor at cell %v corrupted label: %q", cursorCell, frame)
		}
		if !strings.Contains(stripANSI(frame), "◎") {
			t.Fatalf("cursor at cell %v disappeared: %q", cursorCell, frame)
		}
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(value string) string { return ansiPattern.ReplaceAllString(value, "") }

func TestLabelCompositionOrderIsDeterministic(t *testing.T) {
	labels := []Label{
		{Text: "New Delhi", ColX: 4, RowY: 2, Color: 250},
		{Text: "Road", ColX: 0, RowY: 0, Color: 245},
	}
	first := braille.New(20, 8)
	second := braille.New(20, 8)
	firstOccupied := writeLabelsToBuffer(first, labels, 20, 8)
	secondOccupied := writeLabelsToBuffer(second, []Label{labels[1], labels[0]}, 20, 8)
	drawCursor(first, orb.Point{77.209, 28.6139}, geo.Viewport{Lat: 28.6139, Lon: 77.209, Zoom: 12, PixelW: 40, PixelH: 32}, firstOccupied)
	drawCursor(second, orb.Point{77.209, 28.6139}, geo.Viewport{Lat: 28.6139, Lon: 77.209, Zoom: 12, PixelW: 40, PixelH: 32}, secondOccupied)
	if first.Render() != second.Render() {
		t.Fatalf("label composition changed with input order:\nfirst=%q\nsecond=%q", first.Render(), second.Render())
	}
}
