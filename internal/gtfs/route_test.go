package gtfs

import (
	"reflect"
	"testing"
)

func TestAggregateLegs(t *testing.T) {
	steps := []RouteStep{
		{FromStationID: "a", ToStationID: "b", FamilyID: "red", FamilyName: "Red"},
		{FromStationID: "b", ToStationID: "c", FamilyID: "red", FamilyName: "Red"},
		{FromStationID: "c", ToStationID: "d", FamilyID: "blue", FamilyName: "Blue"},
	}
	legs := AggregateLegs([]string{"a", "b", "c", "d"}, steps)
	if len(legs) != 2 || legs[0].From != "a" || legs[0].To != "c" || legs[0].Stops != 2 {
		t.Fatalf("unexpected first leg: %#v", legs)
	}
	if legs[1].From != "c" || legs[1].To != "d" || legs[1].Stops != 1 {
		t.Fatalf("unexpected transfer leg: %#v", legs[1])
	}
}

func TestPlanRouteMinimumStopsAndCanonicalFamilies(t *testing.T) {
	graph := testRouteGraph()
	result := PlanRoute(graph, "a", "d")
	if result.Status != RouteReady {
		t.Fatalf("status = %v, want ready", result.Status)
	}
	if got, want := result.Stations, []string{"a", "b", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stations = %v, want %v", got, want)
	}
	if result.Stops != 2 || result.Transfers != 1 {
		t.Fatalf("stops/transfers = %d/%d, want 2/1", result.Stops, result.Transfers)
	}
	if got, want := result.FamilyIDs, []string{"blue", "red"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("families = %v, want %v", got, want)
	}
}

func TestPlanRouteTieBreaksBySortedStationID(t *testing.T) {
	graph := testRouteGraph()
	result := PlanRoute(graph, "a", "d")
	// a-c-d and a-b-d are equal; b sorts first and is therefore selected.
	if got, want := result.Stations, []string{"a", "b", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tie path = %v, want %v", got, want)
	}
}

func TestPlanRouteStates(t *testing.T) {
	graph := testRouteGraph()
	for _, test := range []struct {
		name   string
		from   string
		to     string
		status RouteStatus
	}{
		{"no endpoints", "", "d", RouteNoEndpoints},
		{"invalid", "missing", "d", RouteInvalid},
		{"same station", "a", "a", RouteSameStation},
		{"unreachable", "a", "isolated", RouteUnreachable},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := PlanRoute(graph, test.from, test.to).Status; got != test.status {
				t.Fatalf("status = %v, want %v", got, test.status)
			}
		})
	}
}

func testRouteGraph() RouteGraph {
	stations := StationIndex{}
	for _, id := range []string{"a", "b", "c", "d", "isolated"} {
		stations[id] = Station{ID: id, Name: id}
	}
	edges := func(from, to, family string) RouteEdge {
		return RouteEdge{FromStationID: from, ToStationID: to, FamilyIDs: []string{family}, Families: []RouteFamily{{ID: family, DisplayName: family}}}
	}
	adjacency := map[string][]RouteEdge{}
	add := func(from, to, family string) {
		edge := edges(from, to, family)
		adjacency[from] = append(adjacency[from], edge)
		edge.FromStationID, edge.ToStationID = to, from
		adjacency[to] = append(adjacency[to], edge)
	}
	add("a", "b", "blue")
	add("b", "d", "red")
	add("a", "c", "green")
	add("c", "d", "green")
	return RouteGraph{Stations: stations, Adjacency: adjacency}
}
