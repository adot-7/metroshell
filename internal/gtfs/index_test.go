package gtfs

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestBuildIndexesFromDelhiMiniFixture(t *testing.T) {
	feed, err := Load(context.Background(), os.DirFS("testdata/delhi-mini"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}

	if got, want := indexes.StationIDs, []string{"dwarka_21", "new_delhi", "rajiv_chowk", "yamuna_bank"}; !reflect.DeepEqual(got, want) {
		t.Errorf("StationIDs = %v, want %v", got, want)
	}
	if got := indexes.Stations["rajiv_chowk"]; got != (Station{ID: "rajiv_chowk", Name: "Rajiv Chowk", Latitude: 28.6328, Longitude: 77.2197}) {
		t.Errorf("Rajiv Chowk = %#v, want fixture station", got)
	}
	if got, want := indexes.LineIDs, []string{"blue", "yellow"}; !reflect.DeepEqual(got, want) {
		t.Errorf("LineIDs = %v, want %v", got, want)
	}
	blue := indexes.Lines["blue"]
	if blue.Color != "#0072BC" || blue.RendererColor != "#0072BC" || blue.GTFSColor != "0072BC" || blue.OriginalColor != "0072BC" {
		t.Errorf("blue line colors = %#v, want normalized and preserved values", blue)
	}
	if got, want := blue.ShapeIDs, []string{"blue_east"}; !reflect.DeepEqual(got, want) {
		t.Errorf("blue ShapeIDs = %v, want %v", got, want)
	}

	shape := indexes.Shapes["blue_east"]
	if got, want := shape.Points[0].Sequence, 1; got != want {
		t.Errorf("first blue shape sequence = %d, want %d", got, want)
	}
	if got, want := shape.Points[len(shape.Points)-1].Sequence, 3; got != want {
		t.Errorf("last blue shape sequence = %d, want %d", got, want)
	}
	if got, want := shape.Geometry[0].X(), 77.0582; got != want {
		t.Errorf("first blue shape longitude = %v, want %v", got, want)
	}
	if got, want := shape.Geometry[0].Y(), 28.5525; got != want {
		t.Errorf("first blue shape latitude = %v, want %v", got, want)
	}
}

func TestBuildIndexesSortsUnorderedFeedDeterministically(t *testing.T) {
	feed, err := Load(context.Background(), os.DirFS("testdata/delhi-mini"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	feed.Stops = reverseStops(feed.Stops)
	feed.Routes = reverseRoutes(feed.Routes)
	feed.Trips = reverseTrips(feed.Trips)
	feed.StopTimes = reverseStopTimes(feed.StopTimes)
	feed.Shapes = reverseShapePoints(feed.Shapes)

	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
	if got, want := indexes.StationIDs, []string{"dwarka_21", "new_delhi", "rajiv_chowk", "yamuna_bank"}; !reflect.DeepEqual(got, want) {
		t.Errorf("StationIDs = %v, want %v", got, want)
	}
	if got, want := indexes.ShapeIDs, []string{"blue_east", "yellow_north"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ShapeIDs = %v, want %v", got, want)
	}
	if got := indexes.Shapes["yellow_north"].Points[0].Sequence; got != 1 {
		t.Errorf("yellow first sequence = %d, want 1", got)
	}
}

func TestBuildIndexesRejectsDuplicateIDs(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Stops = append(feed.Stops, feed.Stops[0])
	_, err := BuildIndexes(feed)
	if err == nil || !strings.Contains(err.Error(), `duplicate stop ID "dwarka_21"`) {
		t.Errorf("BuildIndexes() error = %v, want duplicate stop ID", err)
	}
}

func TestBuildIndexesRejectsBadDelhiCoordinates(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Stops[0].Latitude = 51.5
	_, err := BuildIndexes(feed)
	if err == nil || !strings.Contains(err.Error(), `stop "dwarka_21" has coordinates outside Delhi bounds`) {
		t.Errorf("BuildIndexes() error = %v, want Delhi coordinate error", err)
	}
}

func TestBuildIndexesRejectsMissingReferences(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Feed)
		want string
	}{
		{
			name: "route",
			edit: func(feed *Feed) { feed.Trips[0].RouteID = "missing-route" },
			want: `trip "blue_east" references missing route "missing-route"`,
		},
		{
			name: "shape",
			edit: func(feed *Feed) { feed.Trips[0].ShapeID = "missing-shape" },
			want: `trip "blue_east" references missing shape "missing-shape"`,
		},
		{
			name: "stop",
			edit: func(feed *Feed) { feed.StopTimes[0].StopID = "missing-stop" },
			want: `stop time for trip "blue_east" references missing stop "missing-stop"`,
		},
		{
			name: "trip",
			edit: func(feed *Feed) { feed.StopTimes[0].TripID = "missing-trip" },
			want: `stop time references missing trip "missing-trip"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			feed := mustLoadMiniFeed(t)
			test.edit(&feed)
			_, err := BuildIndexes(feed)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("BuildIndexes() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBuildIndexesNormalizesColors(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Routes[0].Color = "#aBc123"
	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
	line := indexes.Lines[feed.Routes[0].ID]
	if line.Color != "#ABC123" || line.GTFSColor != "#aBc123" {
		t.Errorf("line color = %#v, want normalized renderer and original GTFS values", line)
	}
}

func TestBuildIndexesAssignsDeterministicFallbackForUncoloredRoutes(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Routes[0].Color = ""
	feed.Routes[1].Color = ""

	first, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
	second, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("second BuildIndexes() error = %v", err)
	}

	for _, routeID := range []string{"blue", "yellow"} {
		line := first.Lines[routeID]
		if line.Color != "#808080" || line.RendererColor != "#808080" {
			t.Errorf("%s line colors = %#v, want renderer-safe fallback", routeID, line)
		}
		if line.GTFSColor != "" || line.OriginalColor != "" {
			t.Errorf("%s source colors = %#v, want preserved blank values", routeID, line)
		}
		if line.Color != second.Lines[routeID].Color {
			t.Errorf("%s fallback color changed between index builds: %q and %q", routeID, line.Color, second.Lines[routeID].Color)
		}
	}
}

func TestBuildIndexesRealFeedLikeUncoloredRouteSet(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Routes = []Route{
		{ID: "1", DisplayName: "Red Line"},
		{ID: "3", DisplayName: "Blue Line"},
	}
	feed.Trips[0].RouteID = "3"
	feed.Trips[1].RouteID = "1"

	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
	if got, want := indexes.LineIDs, []string{"1", "3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("LineIDs = %v, want %v", got, want)
	}
	for _, routeID := range []string{"1", "3"} {
		line := indexes.Lines[routeID]
		if line.DisplayName == "" || line.RendererColor != defaultRouteColor {
			t.Errorf("line %q = %#v, want named line with fallback color", routeID, line)
		}
	}
}

func TestBuildIndexesRejectsNonEmptyMalformedColor(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Routes[0].Color = "not-a-color"

	_, err := BuildIndexes(feed)
	if err == nil || !strings.Contains(err.Error(), `route "blue": color "not-a-color" must be a six-digit hexadecimal RGB value`) {
		t.Errorf("BuildIndexes() error = %v, want malformed non-empty color error", err)
	}
}

func mustLoadMiniFeed(t *testing.T) Feed {
	t.Helper()
	feed, err := Load(context.Background(), os.DirFS("testdata/delhi-mini"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return feed
}

func reverseStops(values []Stop) []Stop {
	result := append([]Stop(nil), values...)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func reverseRoutes(values []Route) []Route {
	result := append([]Route(nil), values...)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func reverseTrips(values []Trip) []Trip {
	result := append([]Trip(nil), values...)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func reverseStopTimes(values []StopTime) []StopTime {
	result := append([]StopTime(nil), values...)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func reverseShapePoints(values []ShapePoint) []ShapePoint {
	result := append([]ShapePoint(nil), values...)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}
