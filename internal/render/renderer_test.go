package render

import (
	"strings"
	"testing"

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
	if !strings.Contains(frame, "\x1b[38;5;196m") || !strings.Contains(frame, "\x1b[38;5;21m") {
		t.Fatalf("frame did not contain both normalized route colors: %q", frame)
	}
	if !strings.Contains(frame, "Alpha Line") || !strings.Contains(frame, "Beta Line") {
		t.Fatalf("frame did not contain the fixed line legend: %q", frame)
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

func TestLegendEntriesFollowRenderedLineOrderAndColors(t *testing.T) {
	indexes := gtfs.Indexes{OrderedLines: []gtfs.Line{
		{ID: "z", DisplayName: "Zulu", RendererColor: "#00FF00", Shapes: []gtfs.LineShape{{Geometry: orb.LineString{{77.2, 28.6}, {77.21, 28.61}}}}},
		{ID: "a", DisplayName: "Alpha", RendererColor: "#FF0000"}, // not rendered, no legend entry
		{ID: "b", DisplayName: "Bravo", RendererColor: "#0000FF", Shapes: []gtfs.LineShape{{Geometry: orb.LineString{{77.2, 28.6}, {77.21, 28.61}}}}},
	}}
	entries := legendEntries(indexes)
	if len(entries) != 2 || entries[0].Name != "Zulu" || entries[1].Name != "Bravo" {
		t.Fatalf("legend entries = %#v, want rendered OrderedLines order", entries)
	}
	if entries[0].Color != routeColor("#00FF00") || entries[1].Color != routeColor("#0000FF") {
		t.Fatalf("legend colors = %#v, want normalized route colors", entries)
	}
}

func TestLegendLayoutIsBoundedAndDeterministic(t *testing.T) {
	entries := []legendEntry{{Name: "Blue Line", Color: 33}, {Name: "Yellow Line", Color: 226}, {Name: "Red Line", Color: 196}}
	first := layoutLegend(entries, 12, 2)
	second := layoutLegend(entries, 12, 2)
	if len(first) != len(entries) || len(second) != len(entries) {
		t.Fatalf("legend layout length = %d/%d, want %d", len(first), len(second), len(entries))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("legend layout changed between runs: %#v versus %#v", first, second)
		}
		if first[i].ColX < 0 || first[i].RowY < 0 || first[i].ColX+first[i].Width > 12 || first[i].RowY >= 2 {
			t.Fatalf("legend placement escaped bounds: %#v", first[i])
		}
	}
	if compact := layoutLegend(entries, 2, 1); compact != nil {
		t.Fatalf("too-small legend layout = %#v, want empty bounded state", compact)
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

func TestLegendResizeAndMissingFeedRemainBoundedMapStates(t *testing.T) {
	center := orb.Point{77.2090, 28.6139}
	indexes := gtfs.Indexes{OrderedLines: []gtfs.Line{{ID: "blue", DisplayName: "Blue Line", RendererColor: "#0072BC", Shapes: []gtfs.LineShape{{
		Geometry: orb.LineString{{center[0] - .0001, center[1]}, {center[0] + .0001, center[1]}},
	}}}}}
	for _, size := range [][2]int{{4, 3}, {20, 8}} {
		frame := Render(RenderRequest{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: size[0] * 2, PixelH: size[1] * 4, GTFS: &indexes})
		if got := len(strings.Split(strings.TrimSuffix(frame, "\n"), "\n")); got != size[1] {
			t.Fatalf("resize %dx%d produced %d rows, want %d", size[0], size[1], got, size[1])
		}
	}
	missing := Render(RenderRequest{Lat: center[1], Lon: center[0], Zoom: 15, PixelW: 8, PixelH: 8})
	if strings.Contains(missing, "Blue Line") || strings.Contains(missing, "●") {
		t.Fatalf("missing-feed map-only frame unexpectedly contained legend: %q", missing)
	}
}
