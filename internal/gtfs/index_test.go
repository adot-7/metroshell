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
	if got := indexes.Stations["rajiv_chowk"]; !reflect.DeepEqual(got, Station{ID: "rajiv_chowk", Name: "Rajiv Chowk", Latitude: 28.6328, Longitude: 77.2197, StopIDs: []string{"rajiv_chowk"}, LineIDs: []string{"blue", "yellow"}, FamilyIDs: []string{"blue", "yellow"}}) {
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

func TestBuildIndexesGroupsOnlyExplicitParentStations(t *testing.T) {
	feed, err := Load(context.Background(), os.DirFS("testdata/platform-grouped"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}

	if got, want := indexes.StationIDs, []string{"central", "north", "unparented_central"}; !reflect.DeepEqual(got, want) {
		t.Errorf("StationIDs = %v, want %v", got, want)
	}
	if got, want := indexes.StopToStation, map[string]string{
		"central":            "central",
		"central_blue":       "central",
		"central_yellow":     "central",
		"north":              "north",
		"unparented_central": "unparented_central",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("StopToStation = %v, want %v", got, want)
	}
	if got, want := indexes.Stations["central"], (Station{
		ID:        "central",
		Name:      "Central",
		Latitude:  28.6000,
		Longitude: 77.2000,
		StopIDs:   []string{"central", "central_blue", "central_yellow"},
		LineIDs:   []string{"blue", "yellow"},
		FamilyIDs: []string{"blue", "yellow"},
	}); !reflect.DeepEqual(got, want) {
		t.Errorf("central station = %#v, want %#v", got, want)
	}
	if got := indexes.Stations["north"]; !reflect.DeepEqual(got.StopIDs, []string{"north"}) {
		t.Errorf("north StopIDs = %v, want standalone stop only", got.StopIDs)
	}
	if got := indexes.Stations["unparented_central"]; !reflect.DeepEqual(got.StopIDs, []string{"unparented_central"}) {
		t.Errorf("unparented same-name station StopIDs = %v, want standalone stop only", got.StopIDs)
	}
}

func TestBuildIndexesPublishesDeterministicRendererAssociations(t *testing.T) {
	indexes, err := BuildIndexes(mustLoadMiniFeed(t))
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}

	blueShape := indexes.Lines["blue"].Shapes[0]
	if got, want := blueShape.ShapeID, "blue_east"; got != want {
		t.Errorf("blue shape ID = %q, want %q", got, want)
	}
	if got, want := blueShape.StationIDs, []string{"dwarka_21", "rajiv_chowk", "yamuna_bank"}; !reflect.DeepEqual(got, want) {
		t.Errorf("blue station order = %v, want %v", got, want)
	}
	if got, want := blueShape.Placements[1].SegmentIndex, 0; got != want {
		t.Errorf("Rajiv Chowk segment = %d, want %d", got, want)
	}
	if got, want := blueShape.Placements[1].SegmentFraction, 1.0; got != want {
		t.Errorf("Rajiv Chowk segment fraction = %v, want %v", got, want)
	}
	if got := blueShape.Placements[1].Point; got.X() != 77.2197 || got.Y() != 28.6328 {
		t.Errorf("Rajiv Chowk placement = %v, want source station coordinate", got)
	}

	stationPlacements := indexes.StationPlacements["rajiv_chowk"]
	if got, want := len(stationPlacements), 2; got != want {
		t.Fatalf("Rajiv Chowk placements = %d, want %d", got, want)
	}
	if got, want := []string{stationPlacements[0].LineID, stationPlacements[1].LineID}, []string{"blue", "yellow"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Rajiv Chowk line placement order = %v, want %v", got, want)
	}
	if got, want := indexes.Trips["blue_east"].StationIDs, []string{"dwarka_21", "rajiv_chowk", "yamuna_bank"}; !reflect.DeepEqual(got, want) {
		t.Errorf("blue trip station order = %v, want %v", got, want)
	}
}

func TestBuildIndexesMergesTripsSharingLineShapeAssociation(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Trips = append(feed.Trips, Trip{ID: "blue_late", RouteID: "blue", ShapeID: "blue_east"})
	feed.StopTimes = append(feed.StopTimes, StopTime{TripID: "blue_late", StopID: "rajiv_chowk", Sequence: 1})

	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
	shape := indexes.Lines["blue"].Shapes[0]
	if got, want := shape.TripIDs, []string{"blue_east", "blue_late"}; !reflect.DeepEqual(got, want) {
		t.Errorf("blue shape trips = %v, want %v", got, want)
	}
	placement := indexes.StationPlacements["rajiv_chowk"][0]
	if got, want := placement.TripIDs, []string{"blue_east", "blue_late"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Rajiv Chowk blue trips = %v, want %v", got, want)
	}
	if got, want := placement.StopIDs, []string{"rajiv_chowk"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Rajiv Chowk blue source stops = %v, want %v", got, want)
	}
}

func TestBuildIndexesRendererAssociationsStayDeterministicForUnorderedFeed(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Stops = reverseStops(feed.Stops)
	feed.Routes = reverseRoutes(feed.Routes)
	feed.Trips = reverseTrips(feed.Trips)
	feed.StopTimes = reverseStopTimes(feed.StopTimes)
	feed.Shapes = reverseShapePoints(feed.Shapes)

	first, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
	second, err := BuildIndexes(mustLoadMiniFeed(t))
	if err != nil {
		t.Fatalf("second BuildIndexes() error = %v", err)
	}
	if !reflect.DeepEqual(first.StationPlacements, second.StationPlacements) || !reflect.DeepEqual(first.Lines, second.Lines) || !reflect.DeepEqual(first.Trips, second.Trips) {
		t.Errorf("renderer associations changed with source ordering:\nfirst=%#v\nsecond=%#v", first, second)
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
	if err == nil || !strings.Contains(err.Error(), `stop "dwarka_21" has coordinates outside Delhi-NCR bounds`) {
		t.Errorf("BuildIndexes() error = %v, want Delhi coordinate error", err)
	}
}

func TestBuildIndexesRejectsOutOfRegionShapeCoordinates(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Shapes[0].Latitude = 51.5

	_, err := BuildIndexes(feed)
	if err == nil || !strings.Contains(err.Error(), `shape "blue_east" point sequence 1 has coordinates outside Delhi-NCR bounds`) {
		t.Errorf("BuildIndexes() error = %v, want NCR shape coordinate error", err)
	}
}

func TestBuildIndexesAcceptsNCRBoundaries(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Stops[0].Latitude = DelhiMinLatitude
	feed.Stops[0].Longitude = DelhiMinLongitude
	feed.Stops[1].Latitude = DelhiMaxLatitude
	feed.Stops[1].Longitude = DelhiMaxLongitude
	feed.Shapes[0].Latitude = DelhiMinLatitude
	feed.Shapes[0].Longitude = DelhiMinLongitude
	feed.Shapes[1].Latitude = DelhiMaxLatitude
	feed.Shapes[1].Longitude = DelhiMaxLongitude

	if _, err := BuildIndexes(feed); err != nil {
		t.Fatalf("BuildIndexes() error = %v, want NCR boundary coordinates accepted", err)
	}
}

func TestBuildIndexesRejectsNegativeSequences(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Feed)
		want string
	}{
		{
			name: "shape",
			edit: func(feed *Feed) { feed.Shapes[0].Sequence = -1 },
			want: `shape "blue_east" has invalid point sequence -1`,
		},
		{
			name: "stop time",
			edit: func(feed *Feed) { feed.StopTimes[0].Sequence = -1 },
			want: `stop time for trip "blue_east" has invalid sequence -1`,
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
		if line.Color == "#808080" || line.RendererColor == "#808080" {
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
		if line.DisplayName == "" || line.RendererColor == defaultRouteColor {
			t.Errorf("line %q = %#v, want named line with fallback color", routeID, line)
		}
	}
}

func TestBuildIndexesGroupsDirectionalRoutesWithoutDroppingRawAssociations(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Routes = []Route{
		{ID: "red_dn", DisplayName: "RED_DN"},
		{ID: "red_up", DisplayName: "RED_R"},
		{ID: "blue_branch", DisplayName: "BLUE_DV", Color: "123456"},
	}
	feed.Trips[0].RouteID = "red_dn"
	feed.Trips[0].ID = "red-trip"
	feed.Trips[1].RouteID = "red_up"
	feed.Trips[1].ID = "red-return"
	feed.StopTimes[0].TripID = "red-trip"
	feed.StopTimes[1].TripID = "red-trip"
	feed.StopTimes[2].TripID = "red-trip"
	feed.StopTimes[3].TripID = "red-return"
	feed.StopTimes[4].TripID = "red-return"

	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
	if got, want := len(indexes.Lines), 3; got != want {
		t.Fatalf("raw lines = %d, want %d", got, want)
	}
	if got, want := len(indexes.Families), 2; got != want {
		t.Fatalf("line families = %d, want %d", got, want)
	}
	red := indexes.Families["red"]
	if got, want := red.RouteIDs, []string{"red_dn", "red_up"}; !reflect.DeepEqual(got, want) {
		t.Errorf("red raw route IDs = %v, want %v", got, want)
	}
	if got, want := red.DisplayName, "Red Line"; got != want {
		t.Errorf("red display name = %q, want %q", got, want)
	}
	if got, want := indexes.Trips["red-trip"].LineID, "red_dn"; got != want {
		t.Errorf("trip raw route ID = %q, want %q", got, want)
	}
	if got, want := indexes.Trips["red-trip"].FamilyID, "red"; got != want {
		t.Errorf("trip family ID = %q, want %q", got, want)
	}
	if got, want := indexes.Lines["red_dn"].Shapes[0].TripIDs, []string{"red-trip"}; !reflect.DeepEqual(got, want) {
		t.Errorf("shape raw trip IDs = %v, want %v", got, want)
	}
	if len(indexes.Lines["red_dn"].Shapes[0].Placements) == 0 || len(red.Shapes[0].Placements) == 0 {
		t.Fatal("raw and family shape associations lost station placements")
	}
}

func TestBuildIndexesUsesKnownAndGenericAccessibleFamilyColors(t *testing.T) {
	feed := mustLoadMiniFeed(t)
	feed.Routes = []Route{
		{ID: "red_variant", DisplayName: "RED_HQ"},
		{ID: "mystery_variant", DisplayName: "MYSTERY_R"},
	}
	feed.Trips[0].RouteID = "red_variant"
	feed.Trips[1].RouteID = "mystery_variant"
	first, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
	second, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("second BuildIndexes() error = %v", err)
	}
	if got, want := first.Lines["red_variant"].RendererColor, "#E31E24"; got != want {
		t.Errorf("known family fallback = %q, want %q", got, want)
	}
	unknown := first.Lines["mystery_variant"].RendererColor
	if unknown == defaultRouteColor || unknown != second.Lines["mystery_variant"].RendererColor {
		t.Errorf("unknown family fallback = %q / %q, want stable non-gray colors", unknown, second.Lines["mystery_variant"].RendererColor)
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
