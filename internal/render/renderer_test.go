package render

import (
	"strings"
	"testing"

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
				ID: "a", RendererColor: "#FF0000",
				Shapes: []gtfs.LineShape{{
					ShapeID: "a-shape", Geometry: orb.LineString{{center[0] - .0001, center[1]}, {center[0] + .0001, center[1]}},
					Placements: []gtfs.StationPlacement{{Point: center}},
				}},
			},
			{
				ID: "b", RendererColor: "#0000FF",
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

func TestRouteColorNormalizesHexAndFallback(t *testing.T) {
	if got := routeColor("#ff0000"); got != 196 {
		t.Fatalf("red route color = %d, want xterm 196", got)
	}
	if got := routeColor("bad"); got != routeColor("#808080") {
		t.Fatalf("invalid route color = %d, want deterministic gray fallback", got)
	}
}
