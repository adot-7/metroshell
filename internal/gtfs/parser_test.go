package gtfs

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadDelhiMiniFixture(t *testing.T) {
	feed, err := Load(context.Background(), os.DirFS("testdata/delhi-mini"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(feed.Stops) != 4 || feed.Stops[1] != (Stop{ID: "rajiv_chowk", Name: "Rajiv Chowk", Latitude: 28.6328, Longitude: 77.2197}) {
		t.Errorf("Stops = %#v, want parsed Delhi fixture stops", feed.Stops)
	}
	if len(feed.Routes) != 2 || feed.Routes[0] != (Route{ID: "blue", DisplayName: "Blue Line", Color: "0072BC"}) {
		t.Errorf("Routes = %#v, want blue route with preserved color", feed.Routes)
	}
	if len(feed.Trips) != 2 || feed.Trips[0].DirectionID == nil || *feed.Trips[0].DirectionID != 0 || feed.Trips[0].ServiceID != "weekday" {
		t.Errorf("Trips = %#v, want preserved direction metadata", feed.Trips)
	}
	if got := feed.StopTimes; len(got) != 5 || got[0].ArrivalSeconds != 28800 || got[0].DepartureSeconds != 28800 || got[0].ArrivalTime != "08:00:00" || got[2].Sequence != 3 {
		t.Errorf("StopTimes = %#v, want ordered stop times", got)
	}
	if got := feed.Shapes; len(got) != 5 || got[0] != (ShapePoint{ShapeID: "blue_east", Latitude: 28.5525, Longitude: 77.0582, Sequence: 1}) || got[2].Sequence != 3 {
		t.Errorf("Shapes = %#v, want ordered shape points", got)
	}
}

func TestLoadOrdersStopTimesAndShapePoints(t *testing.T) {
	fixture := miniFixture(t)
	fixture["stop_times.txt"] = &fstest.MapFile{Data: []byte("trip_id,arrival_time,departure_time,stop_id,stop_sequence\nblue_east,08:35:00,08:35:00,yamuna_bank,3\nblue_east,08:00:00,08:00:00,dwarka_21,1\nblue_east,08:20:00,08:20:00,rajiv_chowk,2\nyellow_north,08:10:00,08:10:00,rajiv_chowk,1\nyellow_north,08:15:00,08:15:00,new_delhi,2\n")}
	fixture["shapes.txt"] = &fstest.MapFile{Data: []byte("shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence,shape_dist_traveled\nblue_east,28.6230,77.2677,3,23.7\nblue_east,28.5525,77.0582,1,0\nblue_east,28.6328,77.2197,2,18.2\nyellow_north,28.6415,77.2198,2,1.0\nyellow_north,28.6328,77.2197,1,0\n")}

	feed, err := Load(context.Background(), fixture)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := feed.StopTimes[:3]; got[0].Sequence != 1 || got[1].Sequence != 2 || got[2].Sequence != 3 {
		t.Errorf("blue stop order = %#v, want sequences 1, 2, 3", got)
	}
	if got := feed.Shapes[:3]; got[0].Sequence != 1 || got[1].Sequence != 2 || got[2].Sequence != 3 {
		t.Errorf("blue shape order = %#v, want sequences 1, 2, 3", got)
	}
}

func TestLoadAcceptsZeroBasedSequencesAndRealFeedOptionalFields(t *testing.T) {
	fixture := miniFixture(t)
	fixture["stop_times.txt"] = &fstest.MapFile{Data: []byte("trip_id,arrival_time,departure_time,stop_id,stop_sequence,stop_headsign,pickup_type,drop_off_type,shape_dist_traveled,timepoint,continuous_pickup,continuous_drop_off\nblue_east,08:00:00,08:00:00,dwarka_21,0,,0,0,0.0,1,,\nblue_east,08:20:00,08:20:00,rajiv_chowk,1,,0,0,18.2,1,,\n")}
	fixture["shapes.txt"] = &fstest.MapFile{Data: []byte("shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence,shape_dist_traveled\nblue_east,28.5525,77.0582,0,0.0\nblue_east,28.6328,77.2197,1,18.2\nyellow_north,28.6328,77.2197,0,0.0\n")}

	feed, err := Load(context.Background(), fixture)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := feed.StopTimes[0].Sequence; got != 0 {
		t.Errorf("first stop sequence = %d, want zero-based sequence", got)
	}
	if got := feed.Shapes[0].Sequence; got != 0 {
		t.Errorf("first shape sequence = %d, want zero-based sequence", got)
	}
	if _, err := BuildIndexes(feed); err != nil {
		t.Fatalf("BuildIndexes() error = %v", err)
	}
}

func TestLoadRouteColorIsOptionalAndPreservesBlankValues(t *testing.T) {
	tests := []struct {
		name   string
		routes string
		want   []string
	}{
		{
			name:   "missing column",
			routes: "route_id,route_short_name,route_long_name,route_type\nblue,Blue,Blue Line,1\nyellow,Yellow,Yellow Line,1\n",
			want:   []string{"", ""},
		},
		{
			name:   "blank values",
			routes: "route_id,route_short_name,route_long_name,route_type,route_color\nblue,Blue,Blue Line,1,\nyellow,Yellow,Yellow Line,1,   \n",
			want:   []string{"", ""},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := miniFixture(t)
			fixture["routes.txt"] = &fstest.MapFile{Data: []byte(test.routes)}
			feed, err := Load(context.Background(), fixture)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got := []string{feed.Routes[0].Color, feed.Routes[1].Color}; !reflect.DeepEqual(got, test.want) {
				t.Errorf("route colors = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLoadRejectsNonEmptyMalformedRouteColor(t *testing.T) {
	fixture := miniFixture(t)
	fixture["routes.txt"] = &fstest.MapFile{Data: []byte("route_id,route_short_name,route_long_name,route_type,route_color\nblue,Blue,Blue Line,1,not-a-color\n")}

	_, err := Load(context.Background(), fixture)
	if err == nil || !strings.Contains(err.Error(), "routes.txt line 2: route_color must be a six-digit hexadecimal RGB color") {
		t.Errorf("Load() error = %v, want malformed non-empty route_color error", err)
	}
}

func TestLoadZIP(t *testing.T) {
	fixture := miniFixture(t)
	fixture["routes.txt"] = &fstest.MapFile{Data: []byte("route_id,route_short_name,route_long_name,route_type,route_color\nblue,Blue,Blue Line,1,\nyellow,Yellow,Yellow Line,1,\n")}
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, name := range requiredFiles {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create ZIP file %q: %v", name, err)
		}
		if _, err := file.Write(fixture[name].Data); err != nil {
			t.Fatalf("write ZIP file %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP writer: %v", err)
	}

	feed, err := LoadZIP(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatalf("LoadZIP() error = %v", err)
	}
	if len(feed.Stops) != 4 || len(feed.Shapes) != 5 {
		t.Errorf("LoadZIP() feed = %#v, want complete fixture feed", feed)
	}
}

func TestLoadRejectsMalformedFixtures(t *testing.T) {
	tests := []struct {
		name string
		edit func(fstest.MapFS)
		want string
	}{
		{
			name: "missing required file",
			edit: func(fixture fstest.MapFS) { delete(fixture, "shapes.txt") },
			want: "shapes.txt: required file is missing",
		},
		{
			name: "missing required column",
			edit: func(fixture fstest.MapFS) {
				fixture["stops.txt"] = &fstest.MapFile{Data: []byte("stop_id,stop_name,stop_lat\nstation,Station,28.6\n")}
			},
			want: "stops.txt: required column \"stop_lon\" is missing",
		},
		{
			name: "bad coordinate",
			edit: func(fixture fstest.MapFS) {
				fixture["stops.txt"] = &fstest.MapFile{Data: []byte("stop_id,stop_name,stop_lat,stop_lon\nstation,Station,not-a-coordinate,77.2\n")}
			},
			want: "stops.txt line 2: stop_lat",
		},
		{
			name: "duplicate stop ID",
			edit: func(fixture fstest.MapFS) {
				fixture["stops.txt"] = &fstest.MapFile{Data: []byte("stop_id,stop_name,stop_lat,stop_lon\nstation,Station,28.6,77.2\nstation,Other,28.7,77.3\n")}
			},
			want: "stops.txt line 3: stop_id duplicate ID \"station\"",
		},
		{
			name: "duplicate route ID",
			edit: func(fixture fstest.MapFS) {
				fixture["routes.txt"] = &fstest.MapFile{Data: []byte("route_id,route_short_name,route_long_name,route_type,route_color,route_text_color\nblue,Blue,Blue Line,1,0072BC,FFFFFF\nblue,Blue2,Blue Line 2,1,0072BC,FFFFFF\n")}
			},
			want: "routes.txt line 3: route_id duplicate ID \"blue\"",
		},
		{
			name: "duplicate trip ID",
			edit: func(fixture fstest.MapFS) {
				fixture["trips.txt"] = &fstest.MapFile{Data: []byte("route_id,service_id,trip_id,trip_headsign,direction_id,shape_id\nblue,weekday,trip,East,0,blue_east\nyellow,weekday,trip,North,1,yellow_north\n")}
			},
			want: "trips.txt line 3: trip_id duplicate ID \"trip\"",
		},
		{
			name: "duplicate stop sequence",
			edit: func(fixture fstest.MapFS) {
				fixture["stop_times.txt"] = &fstest.MapFile{Data: []byte("trip_id,arrival_time,departure_time,stop_id,stop_sequence\nblue_east,08:00:00,08:00:00,dwarka_21,1\nblue_east,08:20:00,08:20:00,rajiv_chowk,1\n")}
			},
			want: "stop_times.txt line 3: stop_sequence duplicate sequence 1",
		},
		{
			name: "negative stop sequence",
			edit: func(fixture fstest.MapFS) {
				fixture["stop_times.txt"] = &fstest.MapFile{Data: []byte("trip_id,arrival_time,departure_time,stop_id,stop_sequence\nblue_east,08:00:00,08:00:00,dwarka_21,-1\n")}
			},
			want: "stop_times.txt line 2: stop_sequence must be a non-negative integer",
		},
		{
			name: "duplicate shape sequence",
			edit: func(fixture fstest.MapFS) {
				fixture["shapes.txt"] = &fstest.MapFile{Data: []byte("shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence,shape_dist_traveled\nblue_east,28.6,77.2,1,0\nblue_east,28.7,77.3,1,1\nyellow_north,28.6,77.2,1,0\n")}
			},
			want: "shapes.txt line 3: shape_pt_sequence duplicate sequence 1",
		},
		{
			name: "negative shape sequence",
			edit: func(fixture fstest.MapFS) {
				fixture["shapes.txt"] = &fstest.MapFile{Data: []byte("shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence\nblue_east,28.6,77.2,-1\n")}
			},
			want: "shapes.txt line 2: shape_pt_sequence must be a non-negative integer",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := miniFixture(t)
			test.edit(fixture)
			_, err := Load(context.Background(), fixture)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("Load() error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestLoadRejectsMalformedScheduleFieldsAndCalendarReferences(t *testing.T) {
	tests := []struct{ name, file, data, want string }{
		{"malformed stop time", "stop_times.txt", "trip_id,arrival_time,departure_time,stop_id,stop_sequence\nblue_east,25:61:00,25:61:00,dwarka_21,1\n", "stop_times.txt line 2: arrival_time must use valid HH:MM:SS values"},
		{"trip missing calendar service", "trips.txt", "route_id,service_id,trip_id,trip_headsign,direction_id,shape_id\nblue,missing,blue_east,East,0,blue_east\nyellow,weekday,yellow_north,North,1,yellow_north\n", "gtfs trips: trip \"blue_east\" references missing calendar service \"missing\""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := miniFixture(t)
			if test.file == "trips.txt" {
				fixture["calendar.txt"] = &fstest.MapFile{Data: []byte("service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\nweekday,1,1,1,1,1,0,0,20240101,20241231\n")}
			}
			fixture[test.file] = &fstest.MapFile{Data: []byte(test.data)}
			if _, err := Load(context.Background(), fixture); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error=%v want %q", err, test.want)
			}
		})
	}
}

func TestLoadAcceptsOrphanCalendarServiceRules(t *testing.T) {
	fixture := miniFixture(t)
	fixture["calendar.txt"] = &fstest.MapFile{Data: []byte("service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\nweekday,1,1,1,1,1,0,0,20240101,20241231\nsunday,0,0,0,0,0,0,1,20240101,20241231\n")}

	feed, err := Load(context.Background(), fixture)
	if err != nil {
		t.Fatalf("Load() error = %v, want orphan calendar service accepted", err)
	}
	if len(feed.Calendar) != 2 || feed.Calendar[1].ServiceID != "sunday" {
		t.Fatalf("Calendar = %#v, want retained orphan sunday rule", feed.Calendar)
	}
	if _, err := BuildIndexes(feed); err != nil {
		t.Fatalf("BuildIndexes() error = %v, want accepted orphan calendar service", err)
	}
}

func TestLoadHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Load(ctx, miniFixture(t))
	if err != context.Canceled {
		t.Errorf("Load() error = %v, want %v", err, context.Canceled)
	}
}

func miniFixture(t *testing.T) fstest.MapFS {
	t.Helper()
	source := os.DirFS("testdata/delhi-mini")
	fixture := make(fstest.MapFS, len(requiredFiles))
	for _, name := range requiredFiles {
		data, err := fs.ReadFile(source, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		fixture[name] = &fstest.MapFile{Data: data}
	}
	return fixture
}
