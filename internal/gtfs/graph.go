package gtfs

import (
	"fmt"
	"sort"
)

// RouteFamily is the line-family metadata carried by a route edge. ID and
// DisplayName are the canonical passenger-facing values; RouteIDs and TripIDs
// retain the raw source associations that contributed this edge.
type RouteFamily struct {
	ID            string
	DisplayName   string
	Color         string
	RendererColor string
	RouteIDs      []string
	TripIDs       []string
}

// RouteEdge connects two passenger-facing stations. Edges in Edges are
// undirected and use lexicographic station order. Adjacency contains a
// directionally-oriented copy for each direction so route planners can walk
// it without reconstructing the reverse edge.
type RouteEdge struct {
	FromStationID string
	ToStationID   string
	Families      []RouteFamily
	FamilyIDs     []string
	RouteIDs      []string
	TripIDs       []string
}

// RouteGraph is the deterministic routing projection of a grouped GTFS feed.
// Stations is copied from the grouped station index, including source StopIDs
// and line/family membership. Every station is present, including stations
// which are not reachable from any other station.
type RouteGraph struct {
	Stations   StationIndex
	StationIDs []string
	Edges      []RouteEdge
	Adjacency  map[string][]RouteEdge
	Neighbors  map[string][]string
}

// Graph is a concise compatibility alias for callers which refer to the
// routing projection simply as a graph.
type Graph = RouteGraph

// BuildRouteGraph builds the routing projection from the already-normalized
// grouped stations and TripViews in indexes. It does not read raw GTFS tables,
// depend on the application, or mutate indexes.
//
// A pair of consecutive passenger-facing station IDs contributes one
// bidirectional edge. Repeated station IDs are ignored as self-edges, and
// duplicate trips or edges are merged while retaining all serving families,
// raw route IDs, and raw trip IDs in sorted order.
func BuildRouteGraph(indexes Indexes) (RouteGraph, error) {
	stations, stationIDs, err := graphStations(indexes)
	if err != nil {
		return RouteGraph{}, err
	}

	tripIDs, err := graphTripIDs(indexes)
	if err != nil {
		return RouteGraph{}, err
	}
	trips := indexes.Trips
	if trips == nil {
		trips = indexes.TripByID
	}

	accumulated := make(map[graphEdgeKey]*edgeAccumulator)
	for _, tripID := range tripIDs {
		trip := trips[tripID]
		lines := indexes.Lines
		if lines == nil {
			lines = indexes.LineByID
		}
		line, ok := lines[trip.LineID]
		if !ok {
			return RouteGraph{}, fmt.Errorf("gtfs graph: trip %q references missing route %q", trip.ID, trip.LineID)
		}
		if line.FamilyID == "" {
			return RouteGraph{}, fmt.Errorf("gtfs graph: route %q has no line family", line.ID)
		}
		families := indexes.Families
		if families == nil {
			families = indexes.FamilyByID
		}
		family, ok := families[line.FamilyID]
		if !ok {
			return RouteGraph{}, fmt.Errorf("gtfs graph: route %q references missing line family %q", line.ID, line.FamilyID)
		}
		if trip.FamilyID != "" && trip.FamilyID != line.FamilyID {
			return RouteGraph{}, fmt.Errorf("gtfs graph: trip %q references line family %q, want %q from route %q", trip.ID, trip.FamilyID, line.FamilyID, line.ID)
		}
		for _, stationID := range trip.StationIDs {
			if _, ok := stations[stationID]; !ok {
				return RouteGraph{}, fmt.Errorf("gtfs graph: trip %q references missing station %q", trip.ID, stationID)
			}
		}

		for i := 1; i < len(trip.StationIDs); i++ {
			from, to := trip.StationIDs[i-1], trip.StationIDs[i]
			if from == to {
				continue
			}
			key := newGraphEdgeKey(from, to)
			value := accumulated[key]
			if value == nil {
				value = &edgeAccumulator{families: make(map[string]*familyAccumulator)}
				accumulated[key] = value
			}
			familyValue := value.families[family.ID]
			if familyValue == nil {
				familyValue = &familyAccumulator{family: family, routeIDs: make(map[string]struct{}), tripIDs: make(map[string]struct{})}
				value.families[family.ID] = familyValue
			}
			familyValue.routeIDs[line.ID] = struct{}{}
			familyValue.tripIDs[trip.ID] = struct{}{}
		}
	}

	keys := make([]graphEdgeKey, 0, len(accumulated))
	for key := range accumulated {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].from != keys[j].from {
			return keys[i].from < keys[j].from
		}
		return keys[i].to < keys[j].to
	})

	edges := make([]RouteEdge, 0, len(keys))
	adjacency := make(map[string][]RouteEdge, len(stationIDs))
	neighbors := make(map[string][]string, len(stationIDs))
	for _, stationID := range stationIDs {
		adjacency[stationID] = []RouteEdge{}
		neighbors[stationID] = []string{}
	}
	for _, key := range keys {
		edge := accumulated[key].edge(key)
		edges = append(edges, edge)
		forward := edge
		backward := edge
		backward.FromStationID, backward.ToStationID = edge.ToStationID, edge.FromStationID
		adjacency[forward.FromStationID] = append(adjacency[forward.FromStationID], forward)
		adjacency[backward.FromStationID] = append(adjacency[backward.FromStationID], backward)
		neighbors[forward.FromStationID] = append(neighbors[forward.FromStationID], forward.ToStationID)
		neighbors[backward.FromStationID] = append(neighbors[backward.FromStationID], backward.ToStationID)
	}

	return RouteGraph{
		Stations:   stations,
		StationIDs: stationIDs,
		Edges:      edges,
		Adjacency:  adjacency,
		Neighbors:  neighbors,
	}, nil
}

// BuildGraph is a compatibility wrapper around BuildRouteGraph.
func BuildGraph(indexes Indexes) (RouteGraph, error) {
	return BuildRouteGraph(indexes)
}

type graphEdgeKey struct{ from, to string }

func newGraphEdgeKey(a, b string) graphEdgeKey {
	if a < b {
		return graphEdgeKey{from: a, to: b}
	}
	return graphEdgeKey{from: b, to: a}
}

type edgeAccumulator struct {
	families map[string]*familyAccumulator
}

type familyAccumulator struct {
	family   LineFamily
	routeIDs map[string]struct{}
	tripIDs  map[string]struct{}
}

func (a *edgeAccumulator) edge(key graphEdgeKey) RouteEdge {
	familyIDs := make([]string, 0, len(a.families))
	for familyID := range a.families {
		familyIDs = append(familyIDs, familyID)
	}
	sort.Strings(familyIDs)

	edge := RouteEdge{
		FromStationID: key.from,
		ToStationID:   key.to,
		Families:      make([]RouteFamily, 0, len(familyIDs)),
		FamilyIDs:     append([]string(nil), familyIDs...),
		RouteIDs:      []string{},
		TripIDs:       []string{},
	}
	for _, familyID := range familyIDs {
		value := a.families[familyID]
		routeIDs := sortedSet(value.routeIDs)
		tripIDs := sortedSet(value.tripIDs)
		family := RouteFamily{
			ID:            value.family.ID,
			DisplayName:   value.family.DisplayName,
			Color:         value.family.Color,
			RendererColor: value.family.RendererColor,
			RouteIDs:      routeIDs,
			TripIDs:       tripIDs,
		}
		edge.Families = append(edge.Families, family)
		edge.RouteIDs = append(edge.RouteIDs, routeIDs...)
		edge.TripIDs = append(edge.TripIDs, tripIDs...)
	}
	sort.Strings(edge.RouteIDs)
	sort.Strings(edge.TripIDs)
	return edge
}

func graphStations(indexes Indexes) (StationIndex, []string, error) {
	stations := indexes.Stations
	if stations == nil {
		stations = indexes.StationByID
	}
	if stations == nil {
		stations = make(StationIndex)
	}
	stationIDs := make([]string, 0, len(stations))
	for stationID, station := range stations {
		if stationID == "" || station.ID == "" {
			return nil, nil, fmt.Errorf("gtfs graph: station ID is required")
		}
		if stationID != station.ID {
			return nil, nil, fmt.Errorf("gtfs graph: station map key %q does not match station ID %q", stationID, station.ID)
		}
		stationIDs = append(stationIDs, stationID)
	}
	sort.Strings(stationIDs)
	return stations, stationIDs, nil
}

func graphTripIDs(indexes Indexes) ([]string, error) {
	trips := indexes.Trips
	if trips == nil {
		trips = indexes.TripByID
	}
	if trips == nil {
		trips = make(map[string]TripView)
	}
	tripIDs := make([]string, 0, len(trips))
	for tripID, trip := range trips {
		if tripID == "" || trip.ID == "" {
			return nil, fmt.Errorf("gtfs graph: trip ID is required")
		}
		if tripID != trip.ID {
			return nil, fmt.Errorf("gtfs graph: trip map key %q does not match trip ID %q", tripID, trip.ID)
		}
		tripIDs = append(tripIDs, tripID)
	}
	sort.Strings(tripIDs)
	return tripIDs, nil
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
