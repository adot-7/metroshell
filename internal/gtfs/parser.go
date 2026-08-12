package gtfs

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"sort"
	"strconv"
	"strings"
)

var requiredFiles = []string{
	"stops.txt",
	"routes.txt",
	"trips.txt",
	"stop_times.txt",
	"shapes.txt",
}

// Parser loads the Metroshell subset of a GTFS static feed.
type Parser struct{}

// Load parses a GTFS feed from source. source can be a directory-backed fs.FS,
// a zip.Reader, or another filesystem implementation.
func Load(ctx context.Context, source fs.FS) (Feed, error) {
	return Parser{}.Load(ctx, source)
}

// LoadZIP parses a GTFS ZIP archive from a random-access reader.
func LoadZIP(ctx context.Context, source io.ReaderAt, size int64) (Feed, error) {
	archive, err := zip.NewReader(source, size)
	if err != nil {
		return Feed{}, fmt.Errorf("gtfs zip: %w", err)
	}
	return Load(ctx, archive)
}

// Load parses and validates all required GTFS tables.
func (Parser) Load(ctx context.Context, source fs.FS) (Feed, error) {
	if err := ctx.Err(); err != nil {
		return Feed{}, err
	}
	if source == nil {
		return Feed{}, fmt.Errorf("gtfs source: nil filesystem")
	}

	tables := make(map[string][]record, len(requiredFiles))
	for _, name := range requiredFiles {
		if err := ctx.Err(); err != nil {
			return Feed{}, err
		}
		records, err := readTable(source, name)
		if err != nil {
			return Feed{}, err
		}
		tables[name] = records
	}

	feed, err := parseStops(tables["stops.txt"])
	if err != nil {
		return Feed{}, err
	}
	if feed.Routes, err = parseRoutes(tables["routes.txt"]); err != nil {
		return Feed{}, err
	}
	if feed.Shapes, err = parseShapes(tables["shapes.txt"]); err != nil {
		return Feed{}, err
	}
	if feed.Trips, err = parseTrips(tables["trips.txt"], feed.Routes, feed.Shapes); err != nil {
		return Feed{}, err
	}
	if feed.StopTimes, err = parseStopTimes(tables["stop_times.txt"], feed.Stops, feed.Trips); err != nil {
		return Feed{}, err
	}
	return feed, nil
}

type record struct {
	file string
	line int
	data map[string]string
}

func readTable(source fs.FS, name string) ([]record, error) {
	file, err := source.Open(name)
	if err != nil {
		if errorsIsNotExist(err) {
			return nil, fmt.Errorf("gtfs %s: required file is missing", name)
		}
		return nil, fmt.Errorf("gtfs %s: open: %w", name, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("gtfs %s: invalid CSV: %w", name, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("gtfs %s: missing header row", name)
	}

	header := make(map[string]int, len(rows[0]))
	for i, field := range rows[0] {
		field = strings.TrimPrefix(strings.TrimSpace(field), "\ufeff")
		if field == "" {
			return nil, fmt.Errorf("gtfs %s: header column %d is empty", name, i+1)
		}
		if _, exists := header[field]; exists {
			return nil, fmt.Errorf("gtfs %s: duplicate column %q", name, field)
		}
		header[field] = i
	}
	for _, field := range requiredColumns(name) {
		if _, exists := header[field]; !exists {
			return nil, fmt.Errorf("gtfs %s: required column %q is missing", name, field)
		}
	}

	records := make([]record, 0, len(rows)-1)
	for rowIndex, row := range rows[1:] {
		line := rowIndex + 2
		if len(row) != len(rows[0]) {
			return nil, fmt.Errorf("gtfs %s line %d: has %d columns, want %d", name, line, len(row), len(rows[0]))
		}
		data := make(map[string]string, len(header))
		for field, index := range header {
			data[field] = strings.TrimSpace(row[index])
		}
		records = append(records, record{file: name, line: line, data: data})
	}
	return records, nil
}

func requiredColumns(file string) []string {
	switch file {
	case "stops.txt":
		return []string{"stop_id", "stop_name", "stop_lat", "stop_lon"}
	case "routes.txt":
		return []string{"route_id", "route_short_name", "route_long_name", "route_color"}
	case "trips.txt":
		return []string{"route_id", "trip_id", "shape_id"}
	case "stop_times.txt":
		return []string{"trip_id", "stop_id", "stop_sequence"}
	case "shapes.txt":
		return []string{"shape_id", "shape_pt_lat", "shape_pt_lon", "shape_pt_sequence"}
	default:
		return nil
	}
}

func parseStops(records []record) (Feed, error) {
	feed := Feed{Stops: make([]Stop, 0, len(records))}
	ids := make(map[string]struct{}, len(records))
	for _, row := range records {
		id, err := requiredValue(row, "stop_id")
		if err != nil {
			return Feed{}, err
		}
		if _, exists := ids[id]; exists {
			return Feed{}, duplicateID(row, "stop_id", id)
		}
		name, err := requiredValue(row, "stop_name")
		if err != nil {
			return Feed{}, err
		}
		latitude, err := coordinate(row, "stop_lat", -90, 90)
		if err != nil {
			return Feed{}, err
		}
		longitude, err := coordinate(row, "stop_lon", -180, 180)
		if err != nil {
			return Feed{}, err
		}
		ids[id] = struct{}{}
		feed.Stops = append(feed.Stops, Stop{ID: id, Name: name, Latitude: latitude, Longitude: longitude})
	}
	return feed, nil
}

func parseRoutes(records []record) ([]Route, error) {
	routes := make([]Route, 0, len(records))
	ids := make(map[string]struct{}, len(records))
	for _, row := range records {
		id, err := requiredValue(row, "route_id")
		if err != nil {
			return nil, err
		}
		if _, exists := ids[id]; exists {
			return nil, duplicateID(row, "route_id", id)
		}
		name, err := requiredValue(row, "route_long_name")
		if err != nil {
			return nil, err
		}
		color, err := requiredValue(row, "route_color")
		if err != nil {
			return nil, err
		}
		if !isHexColor(color) {
			return nil, fieldError(row, "route_color", "must be a six-digit hexadecimal RGB color")
		}
		ids[id] = struct{}{}
		routes = append(routes, Route{ID: id, DisplayName: name, Color: color})
	}
	return routes, nil
}

func parseTrips(records []record, routes []Route, shapes []ShapePoint) ([]Trip, error) {
	routeIDs := makeIDSet(routes, func(route Route) string { return route.ID })
	shapeIDs := makeIDSet(shapes, func(shape ShapePoint) string { return shape.ShapeID })
	trips := make([]Trip, 0, len(records))
	ids := make(map[string]struct{}, len(records))
	for _, row := range records {
		id, err := requiredValue(row, "trip_id")
		if err != nil {
			return nil, err
		}
		if _, exists := ids[id]; exists {
			return nil, duplicateID(row, "trip_id", id)
		}
		routeID, err := requiredValue(row, "route_id")
		if err != nil {
			return nil, err
		}
		if _, exists := routeIDs[routeID]; !exists {
			return nil, fieldError(row, "route_id", fmt.Sprintf("references unknown route %q", routeID))
		}
		shapeID, err := requiredValue(row, "shape_id")
		if err != nil {
			return nil, err
		}
		if _, exists := shapeIDs[shapeID]; !exists {
			return nil, fieldError(row, "shape_id", fmt.Sprintf("references unknown shape %q", shapeID))
		}
		trip := Trip{ID: id, RouteID: routeID, ShapeID: shapeID}
		if value, exists := row.data["direction_id"]; exists && value != "" {
			direction, err := parseDirection(row, value)
			if err != nil {
				return nil, err
			}
			trip.DirectionID = &direction
		}
		ids[id] = struct{}{}
		trips = append(trips, trip)
	}
	return trips, nil
}

func parseStopTimes(records []record, stops []Stop, trips []Trip) ([]StopTime, error) {
	stopIDs := makeIDSet(stops, func(stop Stop) string { return stop.ID })
	tripIDs := makeIDSet(trips, func(trip Trip) string { return trip.ID })
	sequences := make(map[string]map[int]struct{})
	stopTimes := make([]StopTime, 0, len(records))
	for _, row := range records {
		tripID, err := requiredValue(row, "trip_id")
		if err != nil {
			return nil, err
		}
		if _, exists := tripIDs[tripID]; !exists {
			return nil, fieldError(row, "trip_id", fmt.Sprintf("references unknown trip %q", tripID))
		}
		stopID, err := requiredValue(row, "stop_id")
		if err != nil {
			return nil, err
		}
		if _, exists := stopIDs[stopID]; !exists {
			return nil, fieldError(row, "stop_id", fmt.Sprintf("references unknown stop %q", stopID))
		}
		sequence, err := positiveInt(row, "stop_sequence")
		if err != nil {
			return nil, err
		}
		if sequences[tripID] == nil {
			sequences[tripID] = make(map[int]struct{})
		}
		if _, exists := sequences[tripID][sequence]; exists {
			return nil, fieldError(row, "stop_sequence", fmt.Sprintf("duplicate sequence %d for trip %q", sequence, tripID))
		}
		sequences[tripID][sequence] = struct{}{}
		stopTimes = append(stopTimes, StopTime{TripID: tripID, StopID: stopID, Sequence: sequence})
	}
	sort.SliceStable(stopTimes, func(i, j int) bool {
		if stopTimes[i].TripID == stopTimes[j].TripID {
			return stopTimes[i].Sequence < stopTimes[j].Sequence
		}
		return stopTimes[i].TripID < stopTimes[j].TripID
	})
	return stopTimes, nil
}

func parseShapes(records []record) ([]ShapePoint, error) {
	sequences := make(map[string]map[int]struct{})
	shapes := make([]ShapePoint, 0, len(records))
	for _, row := range records {
		shapeID, err := requiredValue(row, "shape_id")
		if err != nil {
			return nil, err
		}
		latitude, err := coordinate(row, "shape_pt_lat", -90, 90)
		if err != nil {
			return nil, err
		}
		longitude, err := coordinate(row, "shape_pt_lon", -180, 180)
		if err != nil {
			return nil, err
		}
		sequence, err := positiveInt(row, "shape_pt_sequence")
		if err != nil {
			return nil, err
		}
		if sequences[shapeID] == nil {
			sequences[shapeID] = make(map[int]struct{})
		}
		if _, exists := sequences[shapeID][sequence]; exists {
			return nil, fieldError(row, "shape_pt_sequence", fmt.Sprintf("duplicate sequence %d for shape %q", sequence, shapeID))
		}
		sequences[shapeID][sequence] = struct{}{}
		shapes = append(shapes, ShapePoint{ShapeID: shapeID, Latitude: latitude, Longitude: longitude, Sequence: sequence})
	}
	sort.SliceStable(shapes, func(i, j int) bool {
		if shapes[i].ShapeID == shapes[j].ShapeID {
			return shapes[i].Sequence < shapes[j].Sequence
		}
		return shapes[i].ShapeID < shapes[j].ShapeID
	})
	return shapes, nil
}

func requiredValue(row record, field string) (string, error) {
	value := row.data[field]
	if value == "" {
		return "", fieldError(row, field, "is required")
	}
	return value, nil
}

func coordinate(row record, field string, min, max float64) (float64, error) {
	value, err := requiredValue(row, field)
	if err != nil {
		return 0, err
	}
	coordinate, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || coordinate < min || coordinate > max {
		return 0, fieldError(row, field, fmt.Sprintf("must be a finite number between %g and %g", min, max))
	}
	return coordinate, nil
}

func positiveInt(row record, field string) (int, error) {
	value, err := requiredValue(row, field)
	if err != nil {
		return 0, err
	}
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return 0, fieldError(row, field, "must be a positive integer")
	}
	return number, nil
}

func parseDirection(row record, value string) (int, error) {
	direction, err := strconv.Atoi(value)
	if err != nil || (direction != 0 && direction != 1) {
		return 0, fieldError(row, "direction_id", "must be 0 or 1")
	}
	return direction, nil
}

func isHexColor(color string) bool {
	if len(color) != 6 {
		return false
	}
	for _, character := range color {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') && !(character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

func duplicateID(row record, field, value string) error {
	return fieldError(row, field, fmt.Sprintf("duplicate ID %q", value))
}

func fieldError(row record, field, message string) error {
	return fmt.Errorf("gtfs %s line %d: %s %s", row.file, row.line, field, message)
}

func makeIDSet[T any](values []T, id func(T) string) map[string]struct{} {
	ids := make(map[string]struct{}, len(values))
	for _, value := range values {
		ids[id(value)] = struct{}{}
	}
	return ids
}

func errorsIsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
