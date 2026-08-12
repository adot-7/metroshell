package gtfs

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildRouteGraphBuildsBranchesInterchangesAndGroupedStations(t *testing.T) {
	indexes := mustBuildGraphFixture(t, false)
	graph := indexes.Graph

	if got, want := graph.StationIDs, []string{"branch", "central", "east", "north", "unreachable"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("station IDs = %v, want %v", got, want)
	}
	if got, want := graph.Stations["central"].StopIDs, []string{"central", "central_blue"}; !reflect.DeepEqual(got, want) {
		t.Errorf("grouped central stop IDs = %v, want %v", got, want)
	}
	if got, want := graph.Stations["central"].FamilyIDs, []string{"blue", "red"}; !reflect.DeepEqual(got, want) {
		t.Errorf("central family membership = %v, want %v", got, want)
	}

	if got, want := graph.Neighbors["central"], []string{"branch", "east", "north"}; !reflect.DeepEqual(got, want) {
		t.Errorf("central neighbors = %v, want %v", got, want)
	}
	if got, want := graph.Neighbors["unreachable"], []string{}; !reflect.DeepEqual(got, want) {
		t.Errorf("unreachable neighbors = %v, want empty", got)
	}
	if got, want := len(graph.Edges), 3; got != want {
		t.Fatalf("edge count = %d, want %d", got, want)
	}

	edge := graphEdge(t, graph, "branch", "central")
	if got, want := edge.FamilyIDs, []string{"blue"}; !reflect.DeepEqual(got, want) {
		t.Errorf("branch edge families = %v, want %v", got, want)
	}
	if got, want := edge.RouteIDs, []string{"blue_dn"}; !reflect.DeepEqual(got, want) {
		t.Errorf("branch edge routes = %v, want %v", got, want)
	}
	if got, want := edge.TripIDs, []string{"blue-branch"}; !reflect.DeepEqual(got, want) {
		t.Errorf("branch edge trips = %v, want %v", got, want)
	}

	interchange := graphEdge(t, graph, "central", "east")
	if got, want := interchange.FamilyIDs, []string{"blue", "red"}; !reflect.DeepEqual(got, want) {
		t.Errorf("interchange edge families = %v, want %v", got, want)
	}
	if got, want := interchange.RouteIDs, []string{"blue_dn", "blue_up", "red"}; !reflect.DeepEqual(got, want) {
		t.Errorf("interchange edge routes = %v, want %v", got, want)
	}
	if got, want := interchange.TripIDs, []string{"blue-east", "blue-east-reverse", "red-east"}; !reflect.DeepEqual(got, want) {
		t.Errorf("interchange edge trips = %v, want %v", got, want)
	}
	if got, want := interchange.Families[0].DisplayName, "Blue Line"; got != want {
		t.Errorf("blue family display name = %q, want %q", got, want)
	}
	if got, want := interchange.Families[1].DisplayName, "Red Line"; got != want {
		t.Errorf("red family display name = %q, want %q", got, want)
	}

	for stationID, edges := range graph.Adjacency {
		for _, edge := range edges {
			if edge.FromStationID != stationID {
				t.Errorf("adjacency[%q] contains edge oriented %q -> %q", stationID, edge.FromStationID, edge.ToStationID)
			}
		}
	}
}

func TestBuildRouteGraphDeduplicatesTripsAndSelfEdges(t *testing.T) {
	indexes := mustBuildGraphFixture(t, false)
	graph := indexes.Graph

	if got, want := graph.Neighbors["north"], []string{"central"}; !reflect.DeepEqual(got, want) {
		t.Errorf("north neighbors = %v, want %v", got, want)
	}
	edge := graphEdge(t, graph, "central", "north")
	if got, want := edge.TripIDs, []string{"blue-north", "blue-north-duplicate", "blue-self"}; !reflect.DeepEqual(got, want) {
		t.Errorf("north edge trips = %v, want all unique trips", got)
	}
	if got, want := len(graph.Edges), 3; got != want {
		t.Errorf("self/duplicate edge handling produced %d edges, want %d", got, want)
	}
}

func TestBuildRouteGraphIsDeterministicForShuffledInput(t *testing.T) {
	first := mustBuildGraphFixture(t, false).Graph
	second := mustBuildGraphFixture(t, true).Graph
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("shuffled feed changed graph:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestBuildRouteGraphKeepsEmptyFeedValid(t *testing.T) {
	graph, err := BuildRouteGraph(Indexes{})
	if err != nil {
		t.Fatalf("BuildRouteGraph(empty) error = %v", err)
	}
	if len(graph.StationIDs) != 0 || len(graph.Edges) != 0 || len(graph.Adjacency) != 0 || len(graph.Neighbors) != 0 {
		t.Fatalf("empty graph = %#v, want no stations or edges", graph)
	}
}

func TestBuildRouteGraphReportsMissingReferences(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Indexes)
		want string
	}{
		{
			name: "route",
			edit: func(indexes *Indexes) { indexes.Lines = LineIndex{} },
			want: `trip "blue-branch" references missing route "blue_dn"`,
		},
		{
			name: "family",
			edit: func(indexes *Indexes) { indexes.Families = LineFamilyIndex{} },
			want: `route "blue_dn" references missing line family "blue"`,
		},
		{
			name: "station",
			edit: func(indexes *Indexes) {
				indexes.Trips["blue-branch"] = TripView{ID: "blue-branch", LineID: "blue_dn", FamilyID: "blue", StationIDs: []string{"central", "missing"}}
			},
			want: `trip "blue-branch" references missing station "missing"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			indexes := mustBuildGraphFixture(t, false)
			test.edit(&indexes)
			_, err := BuildRouteGraph(indexes)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildRouteGraph() error = %v, want %q", err, test.want)
			}
		})
	}
}

func mustBuildGraphFixture(t *testing.T, shuffled bool) Indexes {
	t.Helper()
	feed := Feed{
		Stops: []Stop{
			{ID: "central", Name: "Central", Latitude: 28.60, Longitude: 77.20},
			{ID: "central_blue", Name: "Central Blue Platform", Latitude: 28.60, Longitude: 77.20, ParentStationID: "central"},
			{ID: "east", Name: "East", Latitude: 28.61, Longitude: 77.21},
			{ID: "north", Name: "North", Latitude: 28.62, Longitude: 77.20},
			{ID: "branch", Name: "Branch", Latitude: 28.59, Longitude: 77.19},
			{ID: "unreachable", Name: "Unreachable", Latitude: 28.70, Longitude: 77.30},
		},
		Routes: []Route{
			{ID: "blue_dn", DisplayName: "BLUE_DN"},
			{ID: "blue_up", DisplayName: "BLUE_R"},
			{ID: "red", DisplayName: "RED"},
		},
		Trips: []Trip{
			{ID: "blue-branch", RouteID: "blue_dn", ShapeID: "shape-branch"},
			{ID: "blue-east", RouteID: "blue_dn", ShapeID: "shape-east"},
			{ID: "blue-east-reverse", RouteID: "blue_up", ShapeID: "shape-east"},
			{ID: "blue-north", RouteID: "blue_dn", ShapeID: "shape-north"},
			{ID: "blue-north-duplicate", RouteID: "blue_dn", ShapeID: "shape-north"},
			{ID: "blue-self", RouteID: "blue_dn", ShapeID: "shape-self"},
			{ID: "red-east", RouteID: "red", ShapeID: "shape-red"},
		},
		StopTimes: []StopTime{
			{TripID: "blue-branch", StopID: "central", Sequence: 1}, {TripID: "blue-branch", StopID: "branch", Sequence: 2},
			{TripID: "blue-east", StopID: "central", Sequence: 1}, {TripID: "blue-east", StopID: "east", Sequence: 2},
			{TripID: "blue-east-reverse", StopID: "east", Sequence: 1}, {TripID: "blue-east-reverse", StopID: "central", Sequence: 2},
			{TripID: "blue-north", StopID: "central", Sequence: 1}, {TripID: "blue-north", StopID: "north", Sequence: 2},
			{TripID: "blue-north-duplicate", StopID: "central_blue", Sequence: 1}, {TripID: "blue-north-duplicate", StopID: "north", Sequence: 2},
			{TripID: "blue-self", StopID: "central", Sequence: 1}, {TripID: "blue-self", StopID: "central_blue", Sequence: 2}, {TripID: "blue-self", StopID: "north", Sequence: 3},
			{TripID: "red-east", StopID: "central", Sequence: 1}, {TripID: "red-east", StopID: "east", Sequence: 2},
		},
		Shapes: []ShapePoint{
			{ShapeID: "shape-branch", Latitude: 28.60, Longitude: 77.20, Sequence: 1}, {ShapeID: "shape-east", Latitude: 28.60, Longitude: 77.20, Sequence: 1},
			{ShapeID: "shape-north", Latitude: 28.60, Longitude: 77.20, Sequence: 1}, {ShapeID: "shape-self", Latitude: 28.60, Longitude: 77.20, Sequence: 1}, {ShapeID: "shape-red", Latitude: 28.60, Longitude: 77.20, Sequence: 1},
		},
	}
	if shuffled {
		feed.Stops = reverseStops(feed.Stops)
		feed.Routes = reverseRoutes(feed.Routes)
		feed.Trips = reverseTrips(feed.Trips)
		feed.StopTimes = reverseStopTimes(feed.StopTimes)
		feed.Shapes = reverseShapePoints(feed.Shapes)
	}
	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatalf("BuildIndexes(graph fixture) error = %v", err)
	}
	return indexes
}

func graphEdge(t *testing.T, graph RouteGraph, from, to string) RouteEdge {
	t.Helper()
	for _, edge := range graph.Edges {
		if edge.FromStationID == from && edge.ToStationID == to || edge.FromStationID == to && edge.ToStationID == from {
			return edge
		}
	}
	t.Fatalf("edge %q-%q not found in %#v", from, to, graph.Edges)
	return RouteEdge{}
}
