package gtfs

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/paulmach/orb"
)

// Delhi's bounding box is deliberately conservative enough to reject
// accidentally supplied world coordinates while covering the NCR feed data.
const (
	DelhiMinLatitude  = 28.4
	DelhiMaxLatitude  = 28.9
	DelhiMinLongitude = 76.8
	DelhiMaxLongitude = 77.5
)

// Station is the renderer and planner representation of a GTFS stop.
type Station struct {
	ID        string
	Name      string
	Latitude  float64
	Longitude float64
}

// Line is a route with a renderer-ready color. GTFSColor and OriginalColor
// retain the source value; both are provided so callers can use either
// descriptive name without losing the original metadata.
type Line struct {
	ID            string
	DisplayName   string
	Color         string
	RendererColor string
	GTFSColor     string
	OriginalColor string
	ShapeIDs      []string
}

// Shape is one ordered shape geometry. Points retain sequence metadata while
// Geometry is convenient for renderers that consume orb line strings.
type Shape struct {
	ID       string
	Points   []ShapePoint
	Geometry orb.LineString
}

// StationIndex, LineIndex, and ShapeIndex are keyed by stable GTFS IDs.
type StationIndex map[string]Station
type LineIndex map[string]Line
type ShapeIndex map[string]Shape

// Indexes contains the derived data-preparation indexes. Maps are keyed by
// source IDs; the ordered slices are the deterministic iteration form.
type Indexes struct {
	Stations StationIndex
	Lines    LineIndex
	Shapes   ShapeIndex

	StationIDs []string
	LineIDs    []string
	ShapeIDs   []string

	// ByID names make the keyed nature explicit for callers that prefer it.
	StationByID StationIndex
	LineByID    LineIndex
	ShapeByID   ShapeIndex

	// Ordered names make the stable iteration contract explicit.
	OrderedStations []Station
	OrderedLines    []Line
	OrderedShapes   []Shape
}

// Index is retained as a concise alias for callers that prefer the singular
// name when handling one feed snapshot.
type Index = Indexes

// BuildIndexes validates a typed feed and builds deterministic station, line,
// and shape indexes. It does not perform routing or rendering.
func BuildIndexes(feed Feed) (Indexes, error) {
	stations, stationIDs, err := buildStations(feed.Stops)
	if err != nil {
		return Indexes{}, err
	}

	shapes, shapeIDs, err := buildShapes(feed.Shapes)
	if err != nil {
		return Indexes{}, err
	}

	lines, lineIDs, err := buildLines(feed.Routes)
	if err != nil {
		return Indexes{}, err
	}

	trips, tripIDs, err := validateTrips(feed.Trips, lines, shapes)
	if err != nil {
		return Indexes{}, err
	}
	if err := validateStopTimes(feed.StopTimes, stations, tripIDs); err != nil {
		return Indexes{}, err
	}
	attachTripShapes(lines, trips)

	orderedStations := stationsInOrder(stations, stationIDs)
	orderedLines := linesInOrder(lines, lineIDs)
	orderedShapes := shapesInOrder(shapes, shapeIDs)
	return Indexes{
		Stations:        stations,
		Lines:           lines,
		Shapes:          shapes,
		StationIDs:      stationIDs,
		LineIDs:         lineIDs,
		ShapeIDs:        shapeIDs,
		StationByID:     stations,
		LineByID:        lines,
		ShapeByID:       shapes,
		OrderedStations: orderedStations,
		OrderedLines:    orderedLines,
		OrderedShapes:   orderedShapes,
	}, nil
}

// BuildIndex is a singular-name compatibility wrapper around BuildIndexes.
func BuildIndex(feed Feed) (Indexes, error) {
	return BuildIndexes(feed)
}

func buildStations(stops []Stop) (StationIndex, []string, error) {
	ordered := append([]Stop(nil), stops...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	index := make(StationIndex, len(ordered))
	ids := make([]string, 0, len(ordered))
	for _, stop := range ordered {
		if err := validateID("stop", stop.ID); err != nil {
			return nil, nil, err
		}
		if !isDelhiCoordinate(stop.Latitude, stop.Longitude) {
			return nil, nil, fmt.Errorf("gtfs index: stop %q has coordinates outside Delhi bounds: (%v, %v)", stop.ID, stop.Latitude, stop.Longitude)
		}
		if _, exists := index[stop.ID]; exists {
			return nil, nil, fmt.Errorf("gtfs index: duplicate stop ID %q", stop.ID)
		}
		index[stop.ID] = Station{
			ID:        stop.ID,
			Name:      stop.Name,
			Latitude:  stop.Latitude,
			Longitude: stop.Longitude,
		}
		ids = append(ids, stop.ID)
	}
	return index, ids, nil
}

func buildLines(routes []Route) (LineIndex, []string, error) {
	ordered := append([]Route(nil), routes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	index := make(LineIndex, len(ordered))
	ids := make([]string, 0, len(ordered))
	for _, route := range ordered {
		if err := validateID("route", route.ID); err != nil {
			return nil, nil, err
		}
		if _, exists := index[route.ID]; exists {
			return nil, nil, fmt.Errorf("gtfs index: duplicate route ID %q", route.ID)
		}
		rendererColor, err := normalizeColor(route.Color)
		if err != nil {
			return nil, nil, fmt.Errorf("gtfs index: route %q: %w", route.ID, err)
		}
		index[route.ID] = Line{
			ID:            route.ID,
			DisplayName:   route.DisplayName,
			Color:         rendererColor,
			RendererColor: rendererColor,
			GTFSColor:     route.Color,
			OriginalColor: route.Color,
			ShapeIDs:      []string{},
		}
		ids = append(ids, route.ID)
	}
	return index, ids, nil
}

func buildShapes(points []ShapePoint) (ShapeIndex, []string, error) {
	ordered := append([]ShapePoint(nil), points...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ShapeID != ordered[j].ShapeID {
			return ordered[i].ShapeID < ordered[j].ShapeID
		}
		if ordered[i].Sequence != ordered[j].Sequence {
			return ordered[i].Sequence < ordered[j].Sequence
		}
		if ordered[i].Latitude != ordered[j].Latitude {
			return ordered[i].Latitude < ordered[j].Latitude
		}
		return ordered[i].Longitude < ordered[j].Longitude
	})

	index := make(ShapeIndex)
	ids := make([]string, 0)
	for _, point := range ordered {
		if err := validateID("shape", point.ShapeID); err != nil {
			return nil, nil, err
		}
		if !isDelhiCoordinate(point.Latitude, point.Longitude) {
			return nil, nil, fmt.Errorf("gtfs index: shape %q point sequence %d has coordinates outside Delhi bounds: (%v, %v)", point.ShapeID, point.Sequence, point.Latitude, point.Longitude)
		}
		if point.Sequence <= 0 {
			return nil, nil, fmt.Errorf("gtfs index: shape %q has invalid point sequence %d", point.ShapeID, point.Sequence)
		}
		shape, exists := index[point.ShapeID]
		if !exists {
			shape = Shape{ID: point.ShapeID, Points: make([]ShapePoint, 0), Geometry: make(orb.LineString, 0)}
			ids = append(ids, point.ShapeID)
		}
		if len(shape.Points) > 0 && shape.Points[len(shape.Points)-1].Sequence == point.Sequence {
			return nil, nil, fmt.Errorf("gtfs index: shape %q has duplicate point sequence %d", point.ShapeID, point.Sequence)
		}
		shape.Points = append(shape.Points, point)
		shape.Geometry = append(shape.Geometry, orb.Point{point.Longitude, point.Latitude})
		index[point.ShapeID] = shape
	}
	return index, ids, nil
}

func validateTrips(trips []Trip, lines LineIndex, shapes ShapeIndex) ([]Trip, map[string]struct{}, error) {
	ordered := append([]Trip(nil), trips...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ID != ordered[j].ID {
			return ordered[i].ID < ordered[j].ID
		}
		if ordered[i].RouteID != ordered[j].RouteID {
			return ordered[i].RouteID < ordered[j].RouteID
		}
		return ordered[i].ShapeID < ordered[j].ShapeID
	})

	ids := make(map[string]struct{}, len(ordered))
	for _, trip := range ordered {
		if err := validateID("trip", trip.ID); err != nil {
			return nil, nil, err
		}
		if _, exists := ids[trip.ID]; exists {
			return nil, nil, fmt.Errorf("gtfs index: duplicate trip ID %q", trip.ID)
		}
		if _, exists := lines[trip.RouteID]; !exists {
			return nil, nil, fmt.Errorf("gtfs index: trip %q references missing route %q", trip.ID, trip.RouteID)
		}
		if _, exists := shapes[trip.ShapeID]; !exists {
			return nil, nil, fmt.Errorf("gtfs index: trip %q references missing shape %q", trip.ID, trip.ShapeID)
		}
		ids[trip.ID] = struct{}{}
	}
	return ordered, ids, nil
}

func validateStopTimes(stopTimes []StopTime, stations StationIndex, trips map[string]struct{}) error {
	ordered := append([]StopTime(nil), stopTimes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].TripID != ordered[j].TripID {
			return ordered[i].TripID < ordered[j].TripID
		}
		if ordered[i].Sequence != ordered[j].Sequence {
			return ordered[i].Sequence < ordered[j].Sequence
		}
		return ordered[i].StopID < ordered[j].StopID
	})

	seen := make(map[string]map[int]struct{})
	for _, stopTime := range ordered {
		if _, exists := trips[stopTime.TripID]; !exists {
			return fmt.Errorf("gtfs index: stop time references missing trip %q", stopTime.TripID)
		}
		if _, exists := stations[stopTime.StopID]; !exists {
			return fmt.Errorf("gtfs index: stop time for trip %q references missing stop %q", stopTime.TripID, stopTime.StopID)
		}
		if stopTime.Sequence <= 0 {
			return fmt.Errorf("gtfs index: stop time for trip %q has invalid sequence %d", stopTime.TripID, stopTime.Sequence)
		}
		if seen[stopTime.TripID] == nil {
			seen[stopTime.TripID] = make(map[int]struct{})
		}
		if _, exists := seen[stopTime.TripID][stopTime.Sequence]; exists {
			return fmt.Errorf("gtfs index: trip %q has duplicate stop sequence %d", stopTime.TripID, stopTime.Sequence)
		}
		seen[stopTime.TripID][stopTime.Sequence] = struct{}{}
	}
	return nil
}

func attachTripShapes(lines LineIndex, trips []Trip) {
	for _, trip := range trips {
		line := lines[trip.RouteID]
		line.ShapeIDs = append(line.ShapeIDs, trip.ShapeID)
		lines[trip.RouteID] = line
	}
	for routeID, line := range lines {
		sort.Strings(line.ShapeIDs)
		line.ShapeIDs = uniqueStrings(line.ShapeIDs)
		lines[routeID] = line
	}
}

func normalizeColor(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "#") {
		trimmed = trimmed[1:]
	}
	if len(trimmed) != 6 {
		return "", fmt.Errorf("color %q must be a six-digit hexadecimal RGB value", value)
	}
	for _, character := range trimmed {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') && !(character >= 'A' && character <= 'F') {
			return "", fmt.Errorf("color %q must be a six-digit hexadecimal RGB value", value)
		}
	}
	return "#" + strings.ToUpper(trimmed), nil
}

func validateID(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("gtfs index: %s ID is required", kind)
	}
	return nil
}

func isDelhiCoordinate(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsInf(latitude, 0) &&
		!math.IsNaN(longitude) && !math.IsInf(longitude, 0) &&
		latitude >= DelhiMinLatitude && latitude <= DelhiMaxLatitude &&
		longitude >= DelhiMinLongitude && longitude <= DelhiMaxLongitude
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func stationsInOrder(index StationIndex, ids []string) []Station {
	result := make([]Station, 0, len(ids))
	for _, id := range ids {
		result = append(result, index[id])
	}
	return result
}

func linesInOrder(index LineIndex, ids []string) []Line {
	result := make([]Line, 0, len(ids))
	for _, id := range ids {
		result = append(result, index[id])
	}
	return result
}

func shapesInOrder(index ShapeIndex, ids []string) []Shape {
	result := make([]Shape, 0, len(ids))
	for _, id := range ids {
		result = append(result, index[id])
	}
	return result
}
