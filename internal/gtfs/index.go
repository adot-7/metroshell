package gtfs

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/paulmach/orb"
)

// Delhi-NCR's bounding box covers the metropolitan area represented by the
// supported feeds while still rejecting accidentally supplied world or distant
// regional coordinates. The limits are intentionally a little wider than any
// one feed so stations on the NCR fringe are not rejected due to rounding.
const (
	DelhiMinLatitude  = 28.3
	DelhiMaxLatitude  = 29.0
	DelhiMinLongitude = 76.8
	DelhiMaxLongitude = 77.6
	defaultRouteColor = "#808080"
)

var knownFamilyColors = map[string]string{
	"airport": "#0072BC",
	"aqua":    "#00AEEF",
	"blue":    "#0072BC",
	"green":   "#00A651",
	"grey":    "#6B7280",
	"magenta": "#C2185B",
	"orange":  "#F58220",
	"pink":    "#E83E8C",
	"purple":  "#7F3F98",
	"red":     "#E31E24",
	"violet":  "#7F3F98",
	"yellow":  "#D9A400",
}

var genericFamilyColors = []string{"#005F73", "#0A9396", "#2A6F97", "#6A4C93", "#9B2226", "#386641", "#8A5A44"}

// Station is the renderer and planner representation of a passenger-facing
// station. A station is one source stop when parent_station is empty, or the
// explicit GTFS parent plus its child platform stops when parent_station is
// supplied. StopIDs preserve the source IDs represented by this station and
// LineIDs identify every route serving any represented stop.
type Station struct {
	ID        string
	Name      string
	Latitude  float64
	Longitude float64
	StopIDs   []string
	LineIDs   []string
	FamilyIDs []string
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
	FamilyID      string
	FamilyName    string
	ShapeIDs      []string
	Shapes        []LineShape
}

// LineFamily is the passenger-facing display projection of one or more raw
// routes. RouteIDs and the aggregated shapes retain the source membership so
// this compact view never replaces the raw route/trip planning contract.
type LineFamily struct {
	ID            string
	DisplayName   string
	Color         string
	RendererColor string
	GTFSColor     string
	RouteIDs      []string
	ShapeIDs      []string
	Shapes        []LineShape
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
type LineFamilyIndex map[string]LineFamily
type ShapeIndex map[string]Shape

// Indexes contains the derived data-preparation indexes. Maps are keyed by
// source IDs; the ordered slices are the deterministic iteration form.
type Indexes struct {
	Stations StationIndex
	Lines    LineIndex
	Families LineFamilyIndex
	Shapes   ShapeIndex
	Trips    map[string]TripView

	StationIDs []string
	LineIDs    []string
	ShapeIDs   []string
	TripIDs    []string

	// ByID names make the keyed nature explicit for callers that prefer it.
	StationByID StationIndex
	LineByID    LineIndex
	FamilyByID  LineFamilyIndex
	ShapeByID   ShapeIndex
	TripByID    map[string]TripView

	// StopToStation maps every source stop ID, including platform IDs, to the
	// stable passenger-facing station ID used by Stations.
	StopToStation map[string]string

	// StationPlacements contains the deterministic placements for each
	// passenger-facing station. Each entry is keyed by line and shape; an
	// interchange therefore has one placement per served line/shape pair.
	StationPlacements map[string][]StationPlacement

	// Ordered names make the stable iteration contract explicit.
	OrderedStations []Station
	OrderedLines    []Line
	OrderedFamilies []LineFamily
	OrderedShapes   []Shape
	OrderedTrips    []TripView
}

// Index is retained as a concise alias for callers that prefer the singular
// name when handling one feed snapshot.
type Index = Indexes

// BuildIndexes validates a typed feed and builds deterministic station, line,
// and shape indexes. It does not perform routing or rendering.
func BuildIndexes(feed Feed) (Indexes, error) {
	stations, stationIDs, stopToStation, err := buildStations(feed.Stops)
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
	if err := validateStopTimes(feed.StopTimes, stopToStation, tripIDs); err != nil {
		return Indexes{}, err
	}
	tripViews, tripIDsOrdered := buildTripViews(trips, lines, feed.StopTimes, stopToStation)
	attachTripShapes(lines, trips)
	attachStationLines(stations, stopToStation, feed.StopTimes, trips)
	attachStationFamilies(stations, lines)
	stationPlacements, err := attachRenderAssociations(stations, lines, shapes, tripViews, feed.StopTimes, stopToStation)
	if err != nil {
		return Indexes{}, err
	}
	families, familyIDs := buildFamilies(lines)

	orderedStations := stationsInOrder(stations, stationIDs)
	orderedLines := linesInOrder(lines, lineIDs)
	orderedFamilies := familiesInOrder(families, familyIDs)
	orderedShapes := shapesInOrder(shapes, shapeIDs)
	orderedTrips := tripsInOrder(tripViews, tripIDsOrdered)
	return Indexes{
		Stations:          stations,
		Lines:             lines,
		Families:          families,
		Shapes:            shapes,
		Trips:             tripViews,
		StationIDs:        stationIDs,
		LineIDs:           lineIDs,
		ShapeIDs:          shapeIDs,
		TripIDs:           tripIDsOrdered,
		StationByID:       stations,
		LineByID:          lines,
		FamilyByID:        families,
		ShapeByID:         shapes,
		TripByID:          tripViews,
		StopToStation:     stopToStation,
		StationPlacements: stationPlacements,
		OrderedStations:   orderedStations,
		OrderedLines:      orderedLines,
		OrderedFamilies:   orderedFamilies,
		OrderedShapes:     orderedShapes,
		OrderedTrips:      orderedTrips,
	}, nil
}

// BuildIndex is a singular-name compatibility wrapper around BuildIndexes.
func BuildIndex(feed Feed) (Indexes, error) {
	return BuildIndexes(feed)
}

func buildStations(stops []Stop) (StationIndex, []string, map[string]string, error) {
	ordered := append([]Stop(nil), stops...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	byID := make(map[string]Stop, len(ordered))
	for _, stop := range ordered {
		if err := validateID("stop", stop.ID); err != nil {
			return nil, nil, nil, err
		}
		if !isDelhiCoordinate(stop.Latitude, stop.Longitude) {
			return nil, nil, nil, fmt.Errorf("gtfs index: stop %q has coordinates outside Delhi-NCR bounds: (%v, %v)", stop.ID, stop.Latitude, stop.Longitude)
		}
		if _, exists := byID[stop.ID]; exists {
			return nil, nil, nil, fmt.Errorf("gtfs index: duplicate stop ID %q", stop.ID)
		}
		byID[stop.ID] = stop
	}

	for _, stop := range ordered {
		if stop.ParentStationID == "" {
			continue
		}
		parent, exists := byID[stop.ParentStationID]
		if !exists {
			return nil, nil, nil, fmt.Errorf("gtfs index: stop %q references missing parent station %q", stop.ID, stop.ParentStationID)
		}
		if stop.ParentStationID == stop.ID {
			return nil, nil, nil, fmt.Errorf("gtfs index: stop %q cannot be its own parent station", stop.ID)
		}
		if parent.ParentStationID != "" {
			return nil, nil, nil, fmt.Errorf("gtfs index: stop %q references nested parent station %q", stop.ID, stop.ParentStationID)
		}
	}

	groups := make(map[string][]string, len(ordered))
	stopToStation := make(map[string]string, len(ordered))
	for _, stop := range ordered {
		stationID := stop.ID
		if stop.ParentStationID != "" {
			stationID = stop.ParentStationID
		}
		groups[stationID] = append(groups[stationID], stop.ID)
		stopToStation[stop.ID] = stationID
	}

	ids := make([]string, 0, len(groups))
	for stationID := range groups {
		ids = append(ids, stationID)
	}
	sort.Strings(ids)
	index := make(StationIndex, len(ids))
	for _, stationID := range ids {
		representative := byID[stationID]
		members := groups[stationID]
		sort.Strings(members)
		index[stationID] = Station{
			ID:        stationID,
			Name:      representative.Name,
			Latitude:  representative.Latitude,
			Longitude: representative.Longitude,
			StopIDs:   members,
			LineIDs:   []string{},
		}
	}
	return index, ids, stopToStation, nil
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
		familyID, familyName := lineFamilyIdentity(route.DisplayName, route.ID)
		rendererColor, err := rendererColorFor(route.Color, familyID)
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
			FamilyID:      familyID,
			FamilyName:    familyName,
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
			return nil, nil, fmt.Errorf("gtfs index: shape %q point sequence %d has coordinates outside Delhi-NCR bounds: (%v, %v)", point.ShapeID, point.Sequence, point.Latitude, point.Longitude)
		}
		if point.Sequence < 0 {
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

func validateStopTimes(stopTimes []StopTime, stopToStation map[string]string, trips map[string]struct{}) error {
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
		if _, exists := stopToStation[stopTime.StopID]; !exists {
			return fmt.Errorf("gtfs index: stop time for trip %q references missing stop %q", stopTime.TripID, stopTime.StopID)
		}
		if stopTime.Sequence < 0 {
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

func attachStationLines(stations StationIndex, stopToStation map[string]string, stopTimes []StopTime, trips []Trip) {
	tripLines := make(map[string]string, len(trips))
	for _, trip := range trips {
		tripLines[trip.ID] = trip.RouteID
	}
	lineIDs := make(map[string]map[string]struct{}, len(stations))
	for _, stopTime := range stopTimes {
		stationID := stopToStation[stopTime.StopID]
		if lineIDs[stationID] == nil {
			lineIDs[stationID] = make(map[string]struct{})
		}
		lineIDs[stationID][tripLines[stopTime.TripID]] = struct{}{}
	}
	for stationID, station := range stations {
		station.LineIDs = make([]string, 0, len(lineIDs[stationID]))
		for lineID := range lineIDs[stationID] {
			station.LineIDs = append(station.LineIDs, lineID)
		}
		sort.Strings(station.LineIDs)
		stations[stationID] = station
	}
}

func attachStationFamilies(stations StationIndex, lines LineIndex) {
	for stationID, station := range stations {
		families := make(map[string]struct{})
		for _, lineID := range station.LineIDs {
			if line, ok := lines[lineID]; ok {
				families[line.FamilyID] = struct{}{}
			}
		}
		station.FamilyIDs = make([]string, 0, len(families))
		for familyID := range families {
			station.FamilyIDs = append(station.FamilyIDs, familyID)
		}
		sort.Strings(station.FamilyIDs)
		stations[stationID] = station
	}
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

// buildTripViews turns stop_times into the ordered trip associations exposed to
// renderers. The input has already been validated, so each trip's stop
// sequence is unique and every reference is known.
func buildTripViews(trips []Trip, lines LineIndex, stopTimes []StopTime, stopToStation map[string]string) (map[string]TripView, []string) {
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

	views := make(map[string]TripView, len(trips))
	ids := make([]string, 0, len(trips))
	for _, trip := range trips {
		views[trip.ID] = TripView{ID: trip.ID, LineID: trip.RouteID, FamilyID: lines[trip.RouteID].FamilyID, ShapeID: trip.ShapeID, DirectionID: trip.DirectionID, StopIDs: []string{}, StationIDs: []string{}}
		ids = append(ids, trip.ID)
	}
	sort.Strings(ids)
	for _, stopTime := range ordered {
		view := views[stopTime.TripID]
		view.StopIDs = append(view.StopIDs, stopTime.StopID)
		view.StationIDs = append(view.StationIDs, stopToStation[stopTime.StopID])
		views[stopTime.TripID] = view
	}
	return views, ids
}

type lineShapeStationKey struct {
	lineID    string
	shapeID   string
	stationID string
}

// attachRenderAssociations is the complete renderer-facing projection. A
// station is projected onto each line/shape pair serving it. If several trips
// serve that same pair, they share one placement and their source stop/trip IDs
// are retained on it. Projection uses the nearest point on the ordered shape
// polyline in lon/lat coordinates; ties retain the lowest segment and fraction.
func attachRenderAssociations(stations StationIndex, lines LineIndex, shapes ShapeIndex, trips map[string]TripView, stopTimes []StopTime, stopToStation map[string]string) (map[string][]StationPlacement, error) {
	placements := make(map[lineShapeStationKey]StationPlacement)
	tripIDsByLineShape := make(map[string]map[string]struct{})
	for _, trip := range trips {
		_, lineExists := lines[trip.LineID]
		_, shapeExists := shapes[trip.ShapeID]
		if !lineExists || !shapeExists {
			return nil, fmt.Errorf("gtfs index: trip %q has an unresolved line/shape association", trip.ID)
		}
		lineShapeID := trip.LineID + "\x00" + trip.ShapeID
		if tripIDsByLineShape[lineShapeID] == nil {
			tripIDsByLineShape[lineShapeID] = make(map[string]struct{})
		}
		tripIDsByLineShape[lineShapeID][trip.ID] = struct{}{}
	}

	stopTimesByTrip := make(map[string][]StopTime, len(trips))
	for _, stopTime := range stopTimes {
		stopTimesByTrip[stopTime.TripID] = append(stopTimesByTrip[stopTime.TripID], stopTime)
	}
	for tripID := range stopTimesByTrip {
		sort.Slice(stopTimesByTrip[tripID], func(i, j int) bool {
			return stopTimesByTrip[tripID][i].Sequence < stopTimesByTrip[tripID][j].Sequence
		})
	}

	for _, trip := range trips {
		shape := shapes[trip.ShapeID]
		for _, stopTime := range stopTimesByTrip[trip.ID] {
			stationID := stopToStation[stopTime.StopID]
			if stationID == "" {
				return nil, fmt.Errorf("gtfs index: stop time for trip %q has no station association for stop %q", trip.ID, stopTime.StopID)
			}
			station := stations[stationID]
			point, segmentIndex, fraction := projectPoint(shape.Geometry, orb.Point{station.Longitude, station.Latitude})
			key := lineShapeStationKey{lineID: trip.LineID, shapeID: trip.ShapeID, stationID: stationID}
			placement, exists := placements[key]
			if !exists {
				placement = StationPlacement{StationID: stationID, LineID: trip.LineID, FamilyID: lines[trip.LineID].FamilyID, ShapeID: trip.ShapeID, Point: point, SegmentIndex: segmentIndex, SegmentFraction: fraction, StopIDs: []string{}, TripIDs: []string{}}
			}
			placement.StopIDs = appendUniqueSorted(placement.StopIDs, stopTime.StopID)
			placement.TripIDs = appendUniqueSorted(placement.TripIDs, trip.ID)
			placements[key] = placement
		}
	}

	lineShapes := make(map[string][]LineShape)
	for lineID, line := range lines {
		for _, shapeID := range line.ShapeIDs {
			shape := shapes[shapeID]
			lineShape := LineShape{ShapeID: shapeID, LineIDs: []string{lineID}, Geometry: shape.Geometry, TripIDs: []string{}, StationIDs: []string{}, Placements: []StationPlacement{}}
			tripKey := lineID + "\x00" + shapeID
			for tripID := range tripIDsByLineShape[tripKey] {
				lineShape.TripIDs = append(lineShape.TripIDs, tripID)
			}
			sort.Strings(lineShape.TripIDs)
			for key, placement := range placements {
				if key.lineID != lineID || key.shapeID != shapeID {
					continue
				}
				lineShape.StationIDs = append(lineShape.StationIDs, placement.StationID)
				lineShape.Placements = append(lineShape.Placements, placement)
			}
			sort.Slice(lineShape.Placements, func(i, j int) bool {
				return placementLess(lineShape.Placements[i], lineShape.Placements[j])
			})
			lineShape.StationIDs = lineShape.StationIDs[:0]
			for _, placement := range lineShape.Placements {
				if len(lineShape.StationIDs) == 0 || lineShape.StationIDs[len(lineShape.StationIDs)-1] != placement.StationID {
					lineShape.StationIDs = append(lineShape.StationIDs, placement.StationID)
				}
			}
			lineShapes[lineID] = append(lineShapes[lineID], lineShape)
		}
	}
	for lineID, line := range lines {
		sort.Slice(lineShapes[lineID], func(i, j int) bool { return lineShapes[lineID][i].ShapeID < lineShapes[lineID][j].ShapeID })
		line.Shapes = append([]LineShape{}, lineShapes[lineID]...)
		lines[lineID] = line
	}

	stationPlacements := make(map[string][]StationPlacement, len(stations))
	for stationID := range stations {
		stationPlacements[stationID] = []StationPlacement{}
		for key, placement := range placements {
			if key.stationID == stationID {
				stationPlacements[stationID] = append(stationPlacements[stationID], placement)
			}
		}
		sort.Slice(stationPlacements[stationID], func(i, j int) bool {
			a, b := stationPlacements[stationID][i], stationPlacements[stationID][j]
			if a.LineID != b.LineID {
				return a.LineID < b.LineID
			}
			if a.ShapeID != b.ShapeID {
				return a.ShapeID < b.ShapeID
			}
			return placementLess(a, b)
		})
	}
	return stationPlacements, nil
}

func buildFamilies(lines LineIndex) (LineFamilyIndex, []string) {
	families := make(LineFamilyIndex)
	lineIDs := make([]string, 0, len(lines))
	for id := range lines {
		lineIDs = append(lineIDs, id)
	}
	sort.Strings(lineIDs)
	for _, lineID := range lineIDs {
		line := lines[lineID]
		family := families[line.FamilyID]
		if family.ID == "" {
			family = LineFamily{ID: line.FamilyID, DisplayName: line.FamilyName, Color: line.RendererColor, RendererColor: line.RendererColor, RouteIDs: []string{}, ShapeIDs: []string{}, Shapes: []LineShape{}}
		}
		family.RouteIDs = appendUniqueSorted(family.RouteIDs, line.ID)
		// A source color wins over a fallback. Sorted route IDs make the
		// choice deterministic when variants provide different source colors.
		if line.GTFSColor != "" && family.GTFSColor == "" {
			family.Color, family.RendererColor = line.RendererColor, line.RendererColor
			family.GTFSColor = line.GTFSColor
		}
		for _, shape := range line.Shapes {
			family.ShapeIDs = appendUniqueSorted(family.ShapeIDs, shape.ShapeID)
			family.Shapes = append(family.Shapes, shape)
		}
		families[line.FamilyID] = family
	}
	for familyID, family := range families {
		sort.SliceStable(family.Shapes, func(i, j int) bool {
			if family.Shapes[i].ShapeID != family.Shapes[j].ShapeID {
				return family.Shapes[i].ShapeID < family.Shapes[j].ShapeID
			}
			return family.Shapes[i].LineIDs[0] < family.Shapes[j].LineIDs[0]
		})
		family.Shapes = mergeFamilyShapes(family.Shapes)
		families[familyID] = family
	}
	ids := make([]string, 0, len(families))
	for id := range families {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return families, ids
}

func mergeFamilyShapes(shapes []LineShape) []LineShape {
	merged := make(map[string]LineShape)
	for _, shape := range shapes {
		value := merged[shape.ShapeID]
		if value.ShapeID == "" {
			value = LineShape{ShapeID: shape.ShapeID, Geometry: shape.Geometry, TripIDs: []string{}, StationIDs: []string{}, Placements: []StationPlacement{}}
		}
		for _, id := range shape.LineIDs {
			value.LineIDs = appendUniqueSorted(value.LineIDs, id)
		}
		for _, id := range shape.TripIDs {
			value.TripIDs = appendUniqueSorted(value.TripIDs, id)
		}
		for _, placement := range shape.Placements {
			found := false
			for i := range value.Placements {
				if value.Placements[i].LineID == placement.LineID && value.Placements[i].StationID == placement.StationID {
					found = true
					break
				}
			}
			if !found {
				value.Placements = append(value.Placements, placement)
			}
		}
		merged[shape.ShapeID] = value
	}
	result := make([]LineShape, 0, len(merged))
	for _, shape := range merged {
		sort.Slice(shape.Placements, func(i, j int) bool { return placementLess(shape.Placements[i], shape.Placements[j]) })
		for _, p := range shape.Placements {
			shape.StationIDs = appendUniqueSorted(shape.StationIDs, p.StationID)
		}
		result = append(result, shape)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ShapeID < result[j].ShapeID })
	return result
}

func lineFamilyIdentity(displayName, routeID string) (string, string) {
	value := strings.TrimSpace(displayName)
	if value == "" {
		value = strings.TrimSpace(routeID)
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '_' || r == '-' || r == '/' || r == ' '
	})
	key := ""
	if len(parts) > 0 {
		key = strings.ToLower(strings.TrimSpace(parts[0]))
	}
	if key == "" {
		key = "unknown"
	}
	knownNames := map[string]string{"red": "Red Line", "blue": "Blue Line", "yellow": "Yellow Line", "green": "Green Line", "violet": "Violet Line", "magenta": "Magenta Line", "pink": "Pink Line", "orange": "Airport Express", "aqua": "Aqua Line", "grey": "Grey Line", "purple": "Purple Line"}
	name := knownNames[key]
	if name == "" {
		name = strings.TrimSpace(strings.Split(value, "_")[0])
		if name == "" {
			name = "Other Line"
		}
		name += " Line"
	}
	return key, name
}

func familyColorForFamily(familyID string) string {
	if color := knownFamilyColors[familyID]; color != "" {
		return color
	}
	return genericFamilyColors[deterministicFamilyColor(familyID)]
}

func rendererColorFor(source, familyID string) (string, error) {
	if strings.TrimSpace(source) != "" {
		return normalizeColor(source)
	}
	return familyColorForFamily(familyID), nil
}

func deterministicFamilyColor(familyID string) int {
	var hash uint32 = 2166136261
	for _, r := range familyID {
		hash = (hash ^ uint32(r)) * 16777619
	}
	return int(hash % uint32(len(genericFamilyColors)))
}

func projectPoint(line orb.LineString, point orb.Point) (orb.Point, int, float64) {
	if len(line) == 0 {
		return point, 0, 0
	}
	best := line[0]
	bestSegment, bestFraction, bestDistance := 0, 0.0, math.Inf(1)
	for i := 0; i < len(line)-1; i++ {
		start, end := line[i], line[i+1]
		dx, dy := end.X()-start.X(), end.Y()-start.Y()
		fraction := 0.0
		if length := dx*dx + dy*dy; length > 0 {
			fraction = ((point.X()-start.X())*dx + (point.Y()-start.Y())*dy) / length
			fraction = math.Max(0, math.Min(1, fraction))
		}
		candidate := orb.Point{start.X() + fraction*dx, start.Y() + fraction*dy}
		distance := (candidate.X()-point.X())*(candidate.X()-point.X()) + (candidate.Y()-point.Y())*(candidate.Y()-point.Y())
		if distance < bestDistance {
			best, bestSegment, bestFraction, bestDistance = candidate, i, fraction, distance
		}
	}
	if len(line) == 1 {
		best = line[0]
	} else if bestDistance == math.Inf(1) {
		best = line[len(line)-1]
		bestSegment = len(line) - 2
		bestFraction = 1
	}
	return best, bestSegment, bestFraction
}

func placementLess(a, b StationPlacement) bool {
	if a.SegmentIndex != b.SegmentIndex {
		return a.SegmentIndex < b.SegmentIndex
	}
	if a.SegmentFraction != b.SegmentFraction {
		return a.SegmentFraction < b.SegmentFraction
	}
	return a.StationID < b.StationID
}

func appendUniqueSorted(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

func normalizeColor(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return defaultRouteColor, nil
	}
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

func familiesInOrder(index LineFamilyIndex, ids []string) []LineFamily {
	result := make([]LineFamily, 0, len(ids))
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

func tripsInOrder(index map[string]TripView, ids []string) []TripView {
	result := make([]TripView, 0, len(ids))
	for _, id := range ids {
		result = append(result, index[id])
	}
	return result
}
