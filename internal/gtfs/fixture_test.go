package gtfs

import (
	"context"
	"encoding/csv"
	"io/fs"
	"os"
	"reflect"
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
		file, err := fixture.Open(name)
		if err != nil {
			t.Fatalf("open fixture %q: %v", name, err)
		}
		header, err := csv.NewReader(file).Read()
		closeErr := file.Close()
		if err != nil {
			t.Fatalf("read header from %q: %v", name, err)
		}
		if closeErr != nil {
			t.Fatalf("close fixture %q: %v", name, closeErr)
		}
		if !reflect.DeepEqual(header, want) {
			t.Errorf("header in %q = %v, want %v", name, header, want)
		}
	}
}

func TestLoaderBoundaryUsesFilesystemSource(t *testing.T) {
	var _ fs.FS = os.DirFS("testdata/delhi-mini")
	var _ Loader = fixtureLoader{}
}

type fixtureLoader struct{}

func (fixtureLoader) Load(_ context.Context, _ fs.FS) (Feed, error) {
	return Feed{}, nil
}
