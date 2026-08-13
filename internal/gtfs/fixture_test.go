package gtfs

import (
	"context"
	"encoding/csv"
	"io/fs"
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestDelhiMiniFixtureContainsRequiredTables(t *testing.T) {
	fixture := os.DirFS("testdata/delhi-mini")
	wantHeaders := map[string][]string{
		"stops.txt":      {"stop_id", "stop_name", "stop_lat", "stop_lon"},
		"routes.txt":     {"route_id", "route_short_name", "route_long_name", "route_type", "route_color", "route_text_color"},
		"trips.txt":      {"route_id", "service_id", "trip_id", "trip_headsign", "direction_id", "shape_id"},
		"stop_times.txt": {"trip_id", "arrival_time", "departure_time", "stop_id", "stop_sequence"},
		"shapes.txt":     {"shape_id", "shape_pt_lat", "shape_pt_lon", "shape_pt_sequence", "shape_dist_traveled"},
	}

	for name, want := range wantHeaders {
		header, _ := readFixtureRecords(t, fixture, name)
		if !reflect.DeepEqual(header, want) {
			t.Errorf("header in %q = %v, want %v", name, header, want)
		}
	}
}

func TestDelhiMiniFixtureRecordsAreDelhiBoundedAndConnected(t *testing.T) {
	fixture := os.DirFS("testdata/delhi-mini")
	_, stops := readFixtureRecords(t, fixture, "stops.txt")
	_, routes := readFixtureRecords(t, fixture, "routes.txt")
	_, trips := readFixtureRecords(t, fixture, "trips.txt")
	_, stopTimes := readFixtureRecords(t, fixture, "stop_times.txt")
	_, shapes := readFixtureRecords(t, fixture, "shapes.txt")

	stopIDs := make(map[string]struct{}, len(stops))
	for _, stop := range stops {
		assertDelhiCoordinates(t, "stop "+stop["stop_id"], stop["stop_lat"], stop["stop_lon"])
		stopIDs[stop["stop_id"]] = struct{}{}
	}
	if got := stops[1]; got["stop_id"] != "rajiv_chowk" || got["stop_name"] != "Rajiv Chowk" {
		t.Errorf("representative stop = %v, want Rajiv Chowk", got)
	}

	routeIDs := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		routeIDs[route["route_id"]] = struct{}{}
	}
	if got := routes[0]; got["route_id"] != "blue" || got["route_color"] != "0072BC" {
		t.Errorf("representative route = %v, want blue with color 0072BC", got)
	}

	tripIDs := make(map[string]struct{}, len(trips))
	shapeIDs := make(map[string]struct{}, len(shapes))
	for _, shape := range shapes {
		assertDelhiCoordinates(t, "shape "+shape["shape_id"], shape["shape_pt_lat"], shape["shape_pt_lon"])
		shapeIDs[shape["shape_id"]] = struct{}{}
	}
	for _, trip := range trips {
		if _, ok := routeIDs[trip["route_id"]]; !ok {
			t.Errorf("trip %q references missing route %q", trip["trip_id"], trip["route_id"])
		}
		if _, ok := shapeIDs[trip["shape_id"]]; !ok {
			t.Errorf("trip %q references missing shape %q", trip["trip_id"], trip["shape_id"])
		}
		tripIDs[trip["trip_id"]] = struct{}{}
	}
	for _, stopTime := range stopTimes {
		if _, ok := tripIDs[stopTime["trip_id"]]; !ok {
			t.Errorf("stop time references missing trip %q", stopTime["trip_id"])
		}
		if _, ok := stopIDs[stopTime["stop_id"]]; !ok {
			t.Errorf("stop time references missing stop %q", stopTime["stop_id"])
		}
	}
}

func readFixtureRecords(t *testing.T, fixture fs.FS, name string) ([]string, []map[string]string) {
	t.Helper()
	file, err := fixture.Open(name)
	if err != nil {
		t.Fatalf("open fixture %q: %v", name, err)
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	if len(records) < 2 {
		t.Fatalf("fixture %q has no data records", name)
	}

	header := records[0]
	rows := make([]map[string]string, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(record) != len(header) {
			t.Fatalf("record in %q has %d columns, want %d", name, len(record), len(header))
		}
		row := make(map[string]string, len(header))
		for i, field := range header {
			row[field] = record[i]
		}
		rows = append(rows, row)
	}
	return header, rows
}

func assertDelhiCoordinates(t *testing.T, label, latitude, longitude string) {
	t.Helper()
	lat, err := strconv.ParseFloat(latitude, 64)
	if err != nil {
		t.Fatalf("parse latitude for %s: %v", label, err)
	}
	lon, err := strconv.ParseFloat(longitude, 64)
	if err != nil {
		t.Fatalf("parse longitude for %s: %v", label, err)
	}
	if lat < DelhiMinLatitude || lat > DelhiMaxLatitude || lon < DelhiMinLongitude || lon > DelhiMaxLongitude {
		t.Errorf("%s coordinates = (%v, %v), want Delhi bounds", label, lat, lon)
	}
}

func TestLoaderBoundaryUsesFilesystemSource(t *testing.T) {
	var _ fs.FS = os.DirFS("testdata/delhi-mini")
	var _ Loader = fixtureLoader{}
}

func TestDelhiMiniFixtureScheduleFieldsAndExplicitSyntheticPolicy(t *testing.T) {
	feed, err := Load(context.Background(), os.DirFS("testdata/delhi-mini"))
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Calendar) != 0 || len(feed.CalendarDates) != 0 {
		t.Fatal("synthetic fixture unexpectedly gained calendar rules")
	}
	if feed.Trips[0].ServiceID != "weekday" || feed.StopTimes[0].ArrivalTime == "" {
		t.Fatal("fixture did not retain schedule fields")
	}
}

type fixtureLoader struct{}

func (fixtureLoader) Load(_ context.Context, _ fs.FS) (Feed, error) {
	return Feed{}, nil
}
