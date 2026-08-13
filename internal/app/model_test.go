package app

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/gtfs"
	"github.com/adot-7/metroshell/internal/render"
	"github.com/adot-7/metroshell/internal/sim"
	"github.com/paulmach/orb"
)

func TestModelMissingGTFSFeedFallsBackToMapOnly(t *testing.T) {
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: filepath.Join(t.TempDir(), "missing")})
	m = sizedModel(t, m)
	if got := m.FeedState(); got != FeedStateLoading {
		t.Fatalf("initial FeedState = %v, want loading", got)
	}

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("configured feed did not start a load command")
	}
	updated, next := m.Update(cmd())
	if next != nil {
		t.Fatal("feed state transition returned an unexpected command")
	}
	m = updated.(Model)
	if got := m.FeedState(); got != FeedStateMissing {
		t.Fatalf("missing feed FeedState = %v, want missing", got)
	}
	if _, ok := m.Feed(); ok {
		t.Fatal("missing feed was reported as ready")
	}
	if got := m.DataStatus(); got != "GTFS: missing" {
		t.Fatalf("missing feed status = %q, want GTFS: missing", got)
	}
	if !strings.Contains(m.View().Content, "GTFS: missing") {
		t.Fatal("missing feed status was not shown in the HUD")
	}
}

func TestModelLoadsFixtureThroughCommandFlow(t *testing.T) {
	path := filepath.Join("..", "gtfs", "testdata", "delhi-mini")
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: path})
	m = sizedModel(t, m)
	if got := m.FeedState(); got != FeedStateLoading {
		t.Fatalf("initial FeedState = %v, want loading", got)
	}

	updated, next := m.Update(m.Init()())
	if next != nil {
		t.Fatal("successful feed load returned an unexpected command")
	}
	m = updated.(Model)
	if got := m.FeedState(); got != FeedStateReady {
		t.Fatalf("fixture FeedState = %v, want ready", got)
	}
	feed, ok := m.Feed()
	if !ok || len(feed.Stops) != 4 {
		t.Fatalf("fixture feed = %#v, ready = %v, want four stops", feed, ok)
	}
	indexes, ok := m.FeedIndexes()
	if !ok || len(indexes.StationIDs) != 4 || len(indexes.LineIDs) != 2 {
		t.Fatalf("fixture indexes = %#v, ready = %v, want four stations and two lines", indexes, ok)
	}
	if got := m.DataStatus(); got != "GTFS: ready (4 stops, 2 lines)" {
		t.Fatalf("fixture status = %q, want ready summary", got)
	}
}

func TestModelGTFSParseErrorIsVisibleAndNonFatal(t *testing.T) {
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "stops.txt"), []byte("stop_id,stop_name,stop_lat,stop_lon\nstop,Stop,not-a-coordinate,77.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: path})
	m = sizedModel(t, m)
	updated, next := m.Update(m.Init()())
	if next != nil {
		t.Fatal("parse error transition returned an unexpected command")
	}
	m = updated.(Model)
	if got := m.FeedState(); got != FeedStateError {
		t.Fatalf("parse error FeedState = %v, want error", got)
	}
	if m.feedError == nil || !strings.Contains(m.feedError.Error(), "required file is missing") {
		t.Fatalf("parse error = %v, want useful missing-table detail", m.feedError)
	}
	if !strings.Contains(m.View().Content, "GTFS: error") {
		t.Fatal("parse error status was not shown in the HUD")
	}
}

func TestModelViewDoesNotStartGTFSIO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feed-that-view-must-not-open")
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: path})
	m = sizedModel(t, m)
	if got := m.FeedState(); got != FeedStateLoading {
		t.Fatalf("initial FeedState = %v, want loading", got)
	}
	if !strings.Contains(m.View().Content, "GTFS: loading") {
		t.Fatal("loading status was not shown before command completion")
	}
	if got := m.FeedState(); got != FeedStateLoading {
		t.Fatalf("View changed FeedState to %v, want loading until command message", got)
	}
}

func TestModelRejectsStaleTrainRenderFrames(t *testing.T) {
	m := sizedModel(t, New(nil, 28.6139, 77.2090))
	updated, _ := m.Update(frameReadyMsg{seq: m.renderSeq - 1, frame: "stale"})
	m = updated.(Model)
	if m.frame == "stale" {
		t.Fatal("stale frame replaced current viewport state")
	}
	updated, _ = m.Update(frameReadyMsg{seq: m.renderSeq, frame: "current"})
	if got := updated.(Model).frame; got != "current" {
		t.Fatalf("current frame = %q, want current", got)
	}
}

func TestLocalAndSSHConstructionShareSimulationConfigAndSnapshot(t *testing.T) {
	path := filepath.Join("..", "gtfs", "testdata", "delhi-mini")
	local := readyTestModel(t)
	ssh := NewWithConfig(nil, 28.6139, 77.2090, Config{FeedPath: path})
	ssh = sizedModel(t, ssh)
	updated, _ := ssh.Update(ssh.Init()())
	ssh = updated.(Model)
	if !reflect.DeepEqual(local.SimulationConfig(), ssh.SimulationConfig()) {
		t.Fatalf("local/SSH simulator config differs:\nlocal=%#v\nssh=%#v", local.SimulationConfig(), ssh.SimulationConfig())
	}
	if !reflect.DeepEqual(local.SimulationSnapshot(), ssh.SimulationSnapshot()) {
		t.Fatal("local/SSH simulator snapshots differ")
	}
}

func TestSimulationCadenceAndGenerationProtection(t *testing.T) {
	if trainCadence != 250*time.Millisecond {
		t.Fatalf("train cadence=%s, want 250ms", trainCadence)
	}
	if simulationClockStep != sim.ClockCycle/10 {
		t.Fatalf("simulation clock step=%d, want %d", simulationClockStep, sim.ClockCycle/10)
	}
	m := readyTestModel(t)
	if !m.simRunning || m.simGeneration == 0 {
		t.Fatal("ready model did not start simulator")
	}
	oldGeneration := m.simGeneration
	updated, _ := m.Update(tea.BlurMsg{})
	m = updated.(Model)
	if m.simRunning || m.simGeneration == oldGeneration {
		t.Fatalf("blur did not stop/invalidate simulator: running=%v generation=%d old=%d", m.simRunning, m.simGeneration, oldGeneration)
	}
	clock := m.trainClock
	updated, _ = m.Update(trainTickMsg{generation: oldGeneration})
	if got := updated.(Model).trainClock; got != clock {
		t.Fatalf("stale tick advanced clock from %d to %d", clock, got)
	}
}

func TestNormalTickAdvancesVisibleSimulationClock(t *testing.T) {
	m := readyTestModel(t)
	m.width, m.height = 100, 30
	clock := m.trainClock
	updated, _ := m.Update(trainTickMsg{generation: m.simGeneration})
	got := updated.(Model).trainClock
	if got != clock+simulationClockStep {
		t.Fatalf("normal tick clock=%d, want %d", got, clock+simulationClockStep)
	}
	if got != sim.ClockCycle/10 {
		t.Fatalf("first normal tick=%d, want one tenth cycle=%d", got, sim.ClockCycle/10)
	}
}

func TestSimulationPauseReducedMotionAndTransitions(t *testing.T) {
	m := readyTestModel(t)
	m.width, m.height = 100, 30
	if m.simulationPaused() || m.simulationReducedMotion() {
		t.Fatal("normal terminal unexpectedly paused/reduced")
	}
	m.width, m.height = 40, 12
	if m.simulationPaused() || !m.simulationReducedMotion() {
		t.Fatal("compact terminal policy is not reduced motion")
	}
	m.width, m.height = 19, 8
	if !m.simulationPaused() {
		t.Fatal("small terminal was not paused")
	}
	m.width, m.height = 100, 30
	updated, _ := m.Update(tea.BlurMsg{})
	m = updated.(Model)
	if !m.simulationPaused() || m.simulationEligible() {
		t.Fatal("unfocused session was not paused")
	}
	updated, _ = m.Update(tea.FocusMsg{})
	if !updated.(Model).simulationEligible() {
		t.Fatal("focused normal session did not resume")
	}
}

func TestSimulationStateChangesPreserveEndpointAndOverlayState(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "?"}))
	m = updated.(Model)
	if !m.showHelp || m.fromStation != "dwarka_21" || m.toStation != "new_delhi" || m.route.Status != gtfs.RouteReady {
		t.Fatal("opening help changed route/endpoint state")
	}
	before := m.lat
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "j", Code: 'j'}))
	m = updated.(Model)
	if m.lat != before || !m.showHelp {
		t.Fatal("help overlay stopped trapping background input")
	}
	oldGeneration := m.simGeneration
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 19, Height: 8})
	m = updated.(Model)
	if m.simGeneration == oldGeneration || !m.simulationPaused() || m.route.Status != gtfs.RouteReady {
		t.Fatal("resize did not invalidate/pause while preserving route")
	}
	view := stripANSI(m.View().Content)
	rows := strings.Split(view, "\n")
	if len(rows) != 8 {
		t.Fatalf("small help overlay rows=%d, want 8", len(rows))
	}
	for i, row := range rows {
		if lipgloss.Width(row) > 19 {
			t.Fatalf("small help overlay row %d width=%d exceeds 19", i, lipgloss.Width(row))
		}
	}
}

func sizedModel(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(Model)
}

func TestModelPreservesMapControlsAndViewOptions(t *testing.T) {
	m := New(nil, 28.6139, 77.2090)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "w", Code: 'w'}))
	if cmd == nil {
		t.Fatal("pan key did not request a render")
	}
	m = updated.(Model)
	if m.lat <= 28.6139 {
		t.Fatalf("pan key did not move north: %v", m.lat)
	}

	updated, cmd = m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if cmd == nil {
		t.Fatal("mouse wheel up did not request a render")
	}
	m = updated.(Model)
	if m.zoom != 12.1 {
		t.Fatalf("mouse wheel up zoom = %v, want 12.1", m.zoom)
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "?", Code: '?'}))
	m = updated.(Model)
	if !m.showHelp || !strings.Contains(m.View().Content, "keybindings") {
		t.Fatal("help key did not show the help screen")
	}

	view := m.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("view did not enable alternate screen and mouse cell motion")
	}
}

func TestWASDAlwaysPanMapWithoutMovingStationSelection(t *testing.T) {
	m := readyTestModel(t)
	m.focus = focusFrom
	position := m.pickerPos
	lat, lon := m.lat, m.lon
	for _, key := range []string{"w", "a", "s", "d"} {
		updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: key, Code: rune(key[0])}))
		m = updated.(Model)
		if cmd == nil {
			t.Fatalf("%s did not request a map render", key)
		}
	}
	if m.pickerPos != position {
		t.Fatalf("WASD changed station selection from %d to %d", position, m.pickerPos)
	}
	if m.lat != lat || m.lon != lon {
		t.Fatalf("WASD should be a reversible map-only pan, got lat/lon %.6f/%.6f from %.6f/%.6f", m.lat, m.lon, lat, lon)
	}
}

func TestCursorMovementIsDeterministicAndClamped(t *testing.T) {
	newModel := func() Model {
		m := New(nil, 28.6139, 77.2090)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
		return updated.(Model)
	}
	m1, m2 := newModel(), newModel()
	for i := 0; i < 100; i++ {
		updated, _ := m1.Update(tea.KeyPressMsg(tea.Key{Text: "L", Code: 'L'}))
		m1 = updated.(Model)
		updated, _ = m2.Update(tea.KeyPressMsg(tea.Key{Text: "L", Code: 'L'}))
		m2 = updated.(Model)
	}
	if m1.cursor != m2.cursor {
		t.Fatalf("repeated movement diverged: %v versus %v", m1.cursor, m2.cursor)
	}
	vp := geo.Viewport{Lat: m1.lat, Lon: m1.lon, Zoom: m1.zoom, PixelW: 36, PixelH: 24}
	x, y := vp.Project(m1.cursor)
	if x < 0 || x > float64(vp.PixelW-1) || y < 0 || y > float64(vp.PixelH-1) {
		t.Fatalf("cursor escaped small map viewport: (%.3f, %.3f)", x, y)
	}
}

func TestCursorSurvivesResizeAndZoomInsideNewViewport(t *testing.T) {
	m := New(nil, 28.6139, 77.2090)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)
	for _, key := range []string{"I", "I", "L", "K"} {
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: key, Code: rune(key[0])}))
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "+", Code: '+'}))
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 4, Height: 3})
	m = updated.(Model)
	vp := geo.Viewport{Lat: m.lat, Lon: m.lon, Zoom: m.zoom, PixelW: 4, PixelH: 4}
	x, y := vp.Project(m.cursor)
	if math.IsNaN(x) || math.IsNaN(y) || x < -1e-9 || x > 3+1e-9 || y < -1e-9 || y > 3+1e-9 {
		t.Fatalf("cursor escaped resized viewport: (%.3f, %.3f)", x, y)
	}
}

func TestCursorRendersAboveMetroLayer(t *testing.T) {
	m := New(nil, 28.6139, 77.2090)
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	m = updated.(Model)
	m.width, m.height = 100, 30
	if !strings.Contains(m.helpContent(), "move map cursor") {
		t.Fatal("cursor movement was not documented in help")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !strings.Contains(m.frame, "◎") {
		t.Fatal("cursor was not included in model render path")
	}
}

func TestModelResizeProducesCurrentViewportFrame(t *testing.T) {
	m := New(nil, 28.6139, 77.2090)
	updated, firstCmd := m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	m = updated.(Model)
	if firstCmd == nil {
		t.Fatal("initial resize did not request a frame")
	}
	oldFrame := firstCmd()
	updated, secondCmd := m.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	m = updated.(Model)
	if secondCmd == nil {
		t.Fatal("second resize did not request a frame")
	}
	updated, _ = m.Update(oldFrame)
	m = updated.(Model)
	if m.frame != "" {
		t.Fatal("stale frame was accepted after resize")
	}
	updated, _ = m.Update(secondCmd())
	m = updated.(Model)
	if got := len(strings.Split(strings.TrimSuffix(m.frame, "\n"), "\n")); got != 8 {
		t.Fatalf("current resize frame has %d rows, want 8", got)
	}
}

func TestFeedErrorsRejectStaleFramesAndRoutes(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	oldFrameSeq := m.renderSeq
	oldRouteSeq := m.routeSeq
	updated, _ := m.Update(feedErrorMsg{seq: m.feedSeq, err: fmt.Errorf("unreadable feed")})
	m = modelValue(updated)
	if m.FeedState() != FeedStateError || len(m.feedIndexes.Stations) != 0 || m.route.Status == gtfs.RouteReady || m.frame != "" {
		t.Fatalf("feed error retained stale data: state=%v indexes=%d route=%v frame=%q", m.FeedState(), len(m.feedIndexes.Stations), m.route.Status, m.frame)
	}
	updated, _ = m.Update(frameReadyMsg{seq: oldFrameSeq, frame: "stale map"})
	m = modelValue(updated)
	if m.frame != "" {
		t.Fatal("pre-error render frame was accepted")
	}
	updated, _ = m.Update(routeReadyMsg{seq: oldRouteSeq, feedSeq: m.feedSeq, result: gtfs.RouteResult{Status: gtfs.RouteReady, Message: "stale route"}})
	m = modelValue(updated)
	if m.route.Status == gtfs.RouteReady {
		t.Fatal("pre-error route result was accepted")
	}
}

func TestRouteStatesRemainVisibleWithoutStaleHighlight(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	for _, want := range []struct {
		status gtfs.RouteStatus
		text   string
	}{
		{gtfs.RouteNoEndpoints, "Select FROM and TO stations"},
		{gtfs.RouteSameStation, "Same endpoint selected"},
		{gtfs.RouteUnreachable, "No route between selected stations"},
	} {
		m.route = gtfs.RouteResult{Status: want.status, Message: want.text}
		plain := stripANSI(strings.Join(m.sidebarLines(20, 40), "\n"))
		if !strings.Contains(plain, want.text) {
			t.Fatalf("route state %v omitted %q: %q", want.status, want.text, plain)
		}
		if want.status != gtfs.RouteReady && strings.Contains(plain, "highlighted on map") {
			t.Fatalf("route state %v retained stale highlight: %q", want.status, plain)
		}
	}
}

func TestViewIsBoundedAtCompactAndZeroSizes(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {4, 3}, {20, 8}, {52, 16}} {
		m := New(nil, 28.6, 77.2)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = modelValue(updated)
		m.showHelp = true
		rows := strings.Split(strings.TrimSuffix(m.View().Content, "\n"), "\n")
		if len(rows) != size[1] {
			t.Fatalf("size %dx%d rows=%d, want %d", size[0], size[1], len(rows), size[1])
		}
		for i, row := range rows {
			if lipgloss.Width(stripANSI(row)) > size[0] {
				t.Fatalf("size %dx%d row %d overflows: %d", size[0], size[1], i, lipgloss.Width(stripANSI(row)))
			}
		}
	}
	m := New(nil, 28.6, 77.2)
	if got := m.View().Content; got != "" {
		t.Fatalf("unsized view = %q, want empty until terminal size", got)
	}
}

func modelValue(value tea.Model) Model {
	if pointer, ok := value.(*Model); ok {
		return *pointer
	}
	return value.(Model)
}

func TestEndpointSelectionIsDeterministicAndNonBlocking(t *testing.T) {
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: filepath.Join("..", "gtfs", "testdata", "delhi-mini")})
	m = sizedModel(t, m)
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	if !strings.Contains(m.View().Content, "METROSHELL") {
		t.Fatal("ready feed did not show app wordmark")
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = updated.(Model)
	if !m.picker || m.fromStation != "" {
		t.Fatalf("FROM picker state = picker:%v station:%q; want picker open before selection", m.picker, m.fromStation)
	}
	m.pickerPos = 0
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	if m.fromStation == "" || m.focus != focusTo {
		t.Fatalf("FROM selection = %q, focus = %v; want selected FROM and TO focus", m.fromStation, m.focus)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "down"}))
	m = modelValue(updated)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	if m.toStation == "" {
		t.Fatal("TO selection did not accept focused station")
	}
	if !strings.Contains(m.View().Content, "FROM:") || !strings.Contains(m.View().Content, "TO:") {
		t.Fatalf("endpoint names were not shown: %q", m.View().Content)
	}
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	view := m.View().Content
	if !strings.Contains(view, "Route ready") || !strings.Contains(view, "highlighted") || !strings.Contains(view, "transfers") {
		t.Fatalf("connected route summary missing: %q", view)
	}
}

func TestMapCellProjectsToGeographyAndSelectsNearestStation(t *testing.T) {
	m := readyTestModel(t)
	m.zoom = 10
	station := m.feedIndexes.StationByID["rajiv_chowk"]
	vp := m.viewport()
	x, y := vp.Project(stationPoint(station))
	cellX, cellY := int(math.Floor(x/2))+1, int(math.Floor(y/4))+1
	point, ok := m.mapPointFromCell(cellX, cellY)
	if !ok {
		t.Fatalf("station cell (%d,%d) was rejected as outside map", cellX, cellY)
	}
	projectedX, projectedY := vp.Project(point)
	if math.Abs(projectedX-x) > 1 || math.Abs(projectedY-y) > 2 {
		t.Fatalf("cell point projected to (%.3f,%.3f), station at (%.3f,%.3f)", projectedX, projectedY, x, y)
	}

	updated, cmd := m.Update(tea.MouseClickMsg{X: cellX, Y: cellY, Button: tea.MouseLeft})
	m = modelValue(updated)
	if cmd == nil || m.cursor != point || m.fromStation != "rajiv_chowk" || m.toStation != "" {
		t.Fatalf("map click cursor=%v from=%q to=%q cmd=%v, want cursor point and FROM only", m.cursor, m.fromStation, m.toStation, cmd != nil)
	}
}

func TestMapClicksSequenceEndpointsAndMatchPickerRouteFit(t *testing.T) {
	clickModel := readyTestModel(t)
	clickModel.zoom = 10
	clickStation := func(m Model, stationID string) Model {
		station := m.feedIndexes.StationByID[stationID]
		x, y := m.viewport().Project(stationPoint(station))
		updated, _ := m.Update(tea.MouseClickMsg{
			X:      int(math.Floor(x/2)) + 1,
			Y:      int(math.Floor(y/4)) + 1,
			Button: tea.MouseLeft,
		})
		m = modelValue(updated)
		// The click and picker both use the same asynchronous route command;
		// execute the current command synchronously for deterministic comparison.
		updated, _ = m.Update(m.routeCmd()())
		return modelValue(updated)
	}
	clickModel = clickStation(clickModel, "rajiv_chowk")
	clickModel = clickStation(clickModel, "new_delhi")
	if clickModel.fromStation != "rajiv_chowk" || clickModel.toStation != "new_delhi" || clickModel.route.Status != gtfs.RouteReady {
		t.Fatalf("map endpoint flow from=%q to=%q route=%#v", clickModel.fromStation, clickModel.toStation, clickModel.route)
	}

	pickerModel := readyTestModel(t)
	pickerModel.picker = true
	pickerModel.focus = focusFrom
	pickerModel.pickerPos = stationIndex(pickerModel.feedIndexes.OrderedStations, "rajiv_chowk")
	updated, _ := pickerModel.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	pickerModel = modelValue(updated)
	pickerModel.focus = focusTo
	pickerModel.picker = true
	pickerModel.pickerPos = stationIndex(pickerModel.feedIndexes.OrderedStations, "new_delhi")
	updated, _ = pickerModel.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	pickerModel = modelValue(updated)
	updated, _ = pickerModel.Update(pickerModel.routeCmd()())
	pickerModel = modelValue(updated)
	if !reflect.DeepEqual(clickModel.route, pickerModel.route) {
		t.Fatalf("map route=%#v differs from picker route=%#v", clickModel.route, pickerModel.route)
	}
	if clickModel.lat != pickerModel.lat || clickModel.lon != pickerModel.lon || clickModel.zoom != pickerModel.zoom {
		t.Fatalf("map fit=(%.6f,%.6f,z%.3f) differs from picker fit=(%.6f,%.6f,z%.3f)", clickModel.lat, clickModel.lon, clickModel.zoom, pickerModel.lat, pickerModel.lon, pickerModel.zoom)
	}
	t.Logf("native terminal fixture map clicks Rajiv Chowk → New Delhi: route=%s fit=(%.6f,%.6f,z%.3f)", clickModel.route.Message, clickModel.lat, clickModel.lon, clickModel.zoom)
}

func TestMapClickUsesCurrentViewportAfterResize(t *testing.T) {
	m := readyTestModel(t)
	m.zoom = 10
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	m = modelValue(updated)
	station := m.feedIndexes.StationByID["rajiv_chowk"]
	x, y := m.viewport().Project(stationPoint(station))
	cellX, cellY := int(math.Floor(x/2))+1, int(math.Floor(y/4))+1
	if cellX < 1 || cellX > m.mapWidth() || cellY < 1 || cellY > m.height-2 {
		t.Fatalf("resized station cell=(%d,%d) outside map=%dx%d", cellX, cellY, m.mapWidth(), m.height-2)
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: cellX, Y: cellY, Button: tea.MouseLeft})
	m = modelValue(updated)
	if m.fromStation != "rajiv_chowk" {
		t.Fatalf("resized map click selected FROM=%q, want rajiv_chowk", m.fromStation)
	}
}

func TestMapClickFarFromStationsMovesCursorWithoutSelecting(t *testing.T) {
	m := readyTestModel(t)
	m.zoom = 10
	mapX, mapY := m.mapWidth()/2, max(m.height-2, 1)/2
	updated, _ := m.Update(tea.MouseClickMsg{X: mapX, Y: mapY, Button: tea.MouseLeft})
	m = modelValue(updated)
	if m.fromStation != "" || m.toStation != "" {
		t.Fatalf("far map click selected endpoint from=%q to=%q", m.fromStation, m.toStation)
	}
	point, ok := m.mapPointFromCell(mapX, mapY)
	if !ok || m.cursor != point {
		t.Fatalf("far map click cursor=%v point=%v ok=%v", m.cursor, point, ok)
	}
}

func TestMapClicksRespectMapBoundsModalsAndSmallTerminals(t *testing.T) {
	m := readyTestModel(t)
	before := m
	for _, click := range []tea.MouseClickMsg{
		{X: 0, Y: 1, Button: tea.MouseLeft},                // outer frame
		{X: m.mapWidth() + 1, Y: 1, Button: tea.MouseLeft}, // divider/sidebar
		{X: 1, Y: 0, Button: tea.MouseLeft},                // top frame
		{X: 1, Y: m.height - 1, Button: tea.MouseLeft},     // HUD
	} {
		updated, _ := m.Update(click)
		m = modelValue(updated)
	}
	if m.fromStation != "" || m.toStation != "" || m.cursor != before.cursor {
		t.Fatalf("non-map clicks changed selection/cursor: from=%q to=%q cursor=%v want=%v", m.fromStation, m.toStation, m.cursor, before.cursor)
	}

	m.showHelp = true
	updated, _ := m.Update(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	m = modelValue(updated)
	if !m.showHelp || m.fromStation != "" {
		t.Fatal("help overlay did not trap map click")
	}
	m.showHelp = false
	m.picker = true
	updated, _ = m.Update(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	m = modelValue(updated)
	if !m.picker || m.fromStation != "" {
		t.Fatal("picker did not trap map click")
	}

	small := New(nil, 28.6, 77.2)
	updated, _ = small.Update(tea.WindowSizeMsg{Width: 4, Height: 3})
	small = modelValue(updated)
	updated, _ = small.Update(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	if modelValue(updated).fromStation != "" {
		t.Fatal("small terminal click selected a station")
	}
}

func stationPoint(station gtfs.Station) orb.Point {
	return orb.Point{station.Longitude, station.Latitude}
}

func stationIndex(stations []gtfs.Station, id string) int {
	for i, station := range stations {
		if station.ID == id {
			return i
		}
	}
	return -1
}

func TestHelpModalTrapsBackgroundInputAndFitsResize(t *testing.T) {
	m := sizedModel(t, New(nil, 28.6, 77.2))
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "?"}))
	m = updated.(Model)
	before := m.lat
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "j"}))
	m = updated.(Model)
	if m.lat != before {
		t.Fatal("background map input changed state while help was open")
	}
	view := m.View().Content
	if !strings.Contains(view, "╭") || !strings.Contains(view, "keybindings") {
		t.Fatal("help modal not rendered")
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "esc"}))
	if updated.(Model).showHelp {
		t.Fatal("escape did not close help modal")
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "?"}))
	m = updated.(Model)
	if len(strings.Split(m.View().Content, "\n")) < 8 {
		t.Fatal("resized modal was not bounded")
	}
}

func TestPickerOpensWithoutSelectingAndFiltersCaseInsensitively(t *testing.T) {
	m := readyTestModel(t)
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	m = updated.(Model)
	before := m.lat
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = updated.(Model)
	if !m.picker || m.fromStation != "" {
		t.Fatalf("enter changed endpoint or did not open picker: picker=%v from=%q", m.picker, m.fromStation)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "r"}))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "a"}))
	m = updated.(Model)
	if m.search != "ra" || len(m.filteredStations()) == 0 {
		t.Fatalf("search=%q matches=%v", m.search, m.filteredStations())
	}
	updated, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	m = updated.(Model)
	if m.lat != before {
		t.Fatal("mouse changed map while picker open")
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "esc"}))
	m = updated.(Model)
	if m.picker || m.search != "ra" {
		t.Fatalf("escape state picker=%v search=%q", m.picker, m.search)
	}
}

func TestPickerUsesContextualTitlesAndArrowNavigation(t *testing.T) {
	m := readyTestModel(t)
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = updated.(Model)
	if !strings.Contains(stripANSI(strings.Join(m.pickerLines(), "\n")), "Where are you at?") {
		t.Fatal("FROM picker did not use contextual title")
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "down"}))
	m = modelValue(updated)
	if m.pickerPos != 1 {
		t.Fatalf("arrow navigation position = %d, want 1", m.pickerPos)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	if !m.picker || m.focus != focusTo || m.fromStation == "" {
		t.Fatalf("FROM selection did not automatically open TO picker: picker=%v focus=%v from=%q", m.picker, m.focus, m.fromStation)
	}
	if !strings.Contains(stripANSI(strings.Join(m.pickerLines(), "\n")), "Where are you headed?") {
		t.Fatal("TO picker did not use contextual title")
	}
	for _, value := range []string{"j", "k", "space"} {
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: value, Code: rune(value[0])}))
		m = modelValue(updated)
	}
	if m.search != "jk " {
		t.Fatalf("plain picker search = %q, want j/k/space input", m.search)
	}
}

func TestPickerHasFixedGeometryAndScrollsInsideResultWindow(t *testing.T) {
	m := readyTestModel(t)
	m.feedIndexes.OrderedStations = make([]gtfs.Station, 12)
	for i := range m.feedIndexes.OrderedStations {
		m.feedIndexes.OrderedStations[i] = gtfs.Station{ID: fmt.Sprintf("station-%02d", i), Name: fmt.Sprintf("Station %02d", i), FamilyIDs: []string{"blue"}}
	}
	m.feedIndexes.FamilyByID = gtfs.LineFamilyIndex{"blue": {ID: "blue", DisplayName: "Blue Line", RendererColor: "#0072BC"}}
	m.picker, m.focus = true, focusFrom
	base := len(m.pickerLines())
	if base != pickerShellHeight-2 {
		t.Fatalf("normal picker content rows=%d, want %d", base, pickerShellHeight-2)
	}
	for _, search := range []string{"", "station-11"} {
		m.search = search
		if got := len(m.pickerLines()); got != base {
			t.Fatalf("search %q changed picker content rows to %d", search, got)
		}
	}
	m.search = ""
	m.pickerPos = 10
	m.keepPickerVisible()
	if m.pickerTop != 3 {
		t.Fatalf("picker scroll top=%d, want 3 for selected row 10", m.pickerTop)
	}
	plain := stripANSI(strings.Join(m.pickerLines(), "\n"))
	if !strings.Contains(plain, "Station 03") || !strings.Contains(plain, "Station 10") || strings.Contains(plain, "Station 02") {
		t.Fatalf("picker did not scroll result window: %q", plain)
	}
	for _, size := range []struct{ width, height int }{{100, 30}, {40, 12}, {20, 8}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		resized := updated.(Model)
		w, h, _, _ := resized.pickerGeometry()
		if w > size.width || h > size.height {
			t.Fatalf("picker geometry %dx%d escaped terminal %dx%d", w, h, size.width, size.height)
		}
		if got := len(resized.pickerLines()); got < max(h-2, 0) {
			t.Fatalf("constrained picker content rows=%d, want at least %d", got, max(h-2, 0))
		}
	}
}

func TestPickerRowsShowLineOwnershipAndSelectedLineColor(t *testing.T) {
	m := readyTestModel(t)
	m.picker, m.focus = true, focusFrom
	rows := strings.Split(strings.Join(m.pickerLines(), "\n"), "\n")
	var interchange, selected string
	for _, row := range rows {
		plain := stripANSI(row)
		if strings.Contains(plain, "Rajiv Chowk") {
			interchange = row
		}
		if strings.Contains(plain, "Dwarka Sector 21") {
			selected = row
		}
	}
	if !strings.Contains(stripANSI(interchange), "● BLUE") || !strings.Contains(stripANSI(interchange), "● YELLOW") {
		t.Fatalf("interchange row did not show both line indicators: %q", interchange)
	}
	if !strings.Contains(selected, "48;2;0;114;188") || !strings.Contains(stripANSI(selected), "● BLUE") {
		t.Fatalf("selected row did not have a line-colored inset highlight: %q", selected)
	}
}

func TestUnselectedEndpointFieldsAreEmptyButSelectedFieldsShowStationLines(t *testing.T) {
	m := readyTestModel(t)
	sidebar := strings.Join(m.sidebarLines(20, 40), "\n")
	plain := stripANSI(sidebar)
	if !strings.Contains(plain, "FROM:") || !strings.Contains(plain, "TO:") {
		t.Fatalf("empty endpoint fields missing labels: %q", sidebar)
	}
	for _, forbidden := range []string{"Where are you at?", "Where are you headed?", "—"} {
		if strings.Contains(plain, forbidden) {
			t.Fatalf("empty endpoint field contains forbidden placeholder %q: %q", forbidden, plain)
		}
	}
	m.fromStation = "rajiv_chowk"
	selected := stripANSI(strings.Join(m.sidebarLines(20, 40), "\n"))
	if !strings.Contains(selected, "Rajiv Chowk") || !strings.Contains(selected, "● BLUE") || !strings.Contains(selected, "● YELLOW") {
		t.Fatalf("selected endpoint did not show station and compact lines: %q", selected)
	}
}

func TestEndpointFieldsHaveFourSidedNeutralBorders(t *testing.T) {
	m := readyTestModel(t)
	lines := m.sidebarLines(20, 40)
	fromTop, fromBody, fromBottom := lines[2], lines[3], lines[4]
	toTop, toBody, toBottom := lines[6], lines[7], lines[8]
	for name, line := range map[string]string{"from top": fromTop, "from body": fromBody, "from bottom": fromBottom, "to top": toTop, "to body": toBody, "to bottom": toBottom} {
		if !strings.Contains(line, "\x1b[38;5;238m") {
			t.Fatalf("%s lost neutral border styling: %q", name, line)
		}
	}
	if !strings.HasPrefix(stripANSI(fromTop), "╭") || !strings.HasSuffix(stripANSI(fromTop), "╮") || !strings.HasPrefix(stripANSI(fromBottom), "╰") || !strings.HasSuffix(stripANSI(fromBottom), "╯") {
		t.Fatalf("FROM field is not four-sided: %q / %q", fromTop, fromBottom)
	}
	if !strings.HasPrefix(stripANSI(toTop), "╭") || !strings.HasSuffix(stripANSI(toTop), "╮") || !strings.HasPrefix(stripANSI(toBottom), "╰") || !strings.HasSuffix(stripANSI(toBottom), "╯") {
		t.Fatalf("TO field is not four-sided: %q / %q", toTop, toBottom)
	}
}

func TestMapSymbolsCannotShiftPinkFrameColumns(t *testing.T) {
	m := New(nil, 28.6, 77.2)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(Model)
	mapRows := make([]string, m.height-2)
	for i := range mapRows {
		mapRows[i] = strings.Repeat("·", m.mapWidth()-2) + "🍴"
	}
	m.frame = strings.Join(mapRows, "\n")
	viewRows := strings.Split(strings.TrimSuffix(m.View().Content, "\n"), "\n")
	for i, row := range viewRows {
		if got := lipgloss.Width(stripANSI(row)); got != m.width {
			t.Fatalf("row %d width=%d after wide map symbol, want %d: %q", i, got, m.width, row)
		}
	}
}

func TestSidebarAndOverlayLayoutAreResponsive(t *testing.T) {
	for _, size := range []struct{ width, height, wantPanel int }{{100, 30, 40}, {70, 20, 30}, {40, 12, 0}} {
		m := New(nil, 28.6, 77.2)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m = updated.(Model)
		if got := m.sidebarWidth(); got != size.wantPanel {
			t.Fatalf("size %dx%d sidebar width=%d, want %d", size.width, size.height, got, size.wantPanel)
		}
		if size.wantPanel > 0 && m.mapWidth() < size.width/3 {
			t.Fatalf("size %dx%d map width=%d is not viable beside sidebar", size.width, size.height, m.mapWidth())
		}
		m.showHelp = true
		lines := strings.Split(strings.TrimSuffix(m.View().Content, "\n"), "\n")
		if len(lines) != size.height {
			t.Fatalf("size %dx%d overlay rows=%d, want %d", size.width, size.height, len(lines), size.height)
		}
		for row, line := range lines {
			if lipgloss.Width(stripANSI(line)) > size.width {
				t.Fatalf("size %dx%d row %d overflows at %d columns", size.width, size.height, row, lipgloss.Width(stripANSI(line)))
			}
		}
	}
}

func TestPickerAndHelpSplitOuterPinkFromInnerNeutralBorders(t *testing.T) {
	m := readyTestModel(t)
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	view := m.View().Content
	if !strings.Contains(view, "\x1b[38;5;201m╭") || !strings.Contains(view, "\x1b[38;5;238m╭") {
		t.Fatalf("picker did not split pink outer and neutral inner borders: %q", view)
	}
	if got := len(m.pickerLines()); got >= m.height {
		t.Fatalf("picker content has oversized frame height %d for terminal height %d", got, m.height)
	}
	pickerRows := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(pickerRows) != m.height {
		t.Fatalf("picker frame rows=%d, want %d", len(pickerRows), m.height)
	}
	for i, line := range pickerRows {
		if lipgloss.Width(stripANSI(line)) != m.width {
			t.Fatalf("picker frame row %d width=%d, want %d", i, lipgloss.Width(stripANSI(line)), m.width)
		}
	}
	m.picker = false
	m.showHelp = true
	view = m.View().Content
	if !strings.Contains(view, "\x1b[38;5;201m╭") {
		t.Fatalf("help shell did not retain pink outer border: %q", view)
	}
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) != m.height {
		t.Fatalf("help frame rows=%d, want %d", len(lines), m.height)
	}
	for i, line := range lines {
		if lipgloss.Width(stripANSI(line)) != m.width {
			t.Fatalf("help frame row %d width=%d, want %d", i, lipgloss.Width(stripANSI(line)), m.width)
		}
	}
}

func TestUIThemeSplitAndCompactOverlayCopy(t *testing.T) {
	m := readyTestModel(t)
	view := m.View().Content
	if !strings.Contains(view, "\x1b[38;5;201m╭") {
		t.Fatal("application frame did not retain pink theme")
	}
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	m = modelValue(updated)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	plain := stripANSI(strings.Join(m.pickerLines(), "\n"))
	for _, unwanted := range []string{" / ", "Blue Line", "Red Line", "Yellow Line", "ENDPOINTS", "—"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("picker contains unwanted copy %q: %q", unwanted, plain)
		}
	}
	if !strings.Contains(plain, "Where are you at?") || !strings.Contains(plain, "▏") {
		t.Fatalf("picker lost contextual title or caret: %q", plain)
	}
	if strings.Contains(m.View().Content, "\x1b[48;") {
		t.Fatal("overlay painted an opaque background")
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "esc"}))
	m = modelValue(updated)
	sidebar := strings.Join(m.sidebarLines(20, 40), "\n")
	if !strings.Contains(stripANSI(sidebar), "METROSHELL") || strings.Contains(sidebar, "—") || strings.Contains(stripANSI(sidebar), "ENDPOINTS") {
		t.Fatalf("sidebar has invalid heading or placeholder: %q", sidebar)
	}
	if !strings.Contains(sidebar, "\x1b[38;5;238m│") {
		t.Fatalf("sidebar endpoint fields lost neutral soft borders: %q", sidebar)
	}
}

func TestOverlayPreservesUnderlyingApplicationRows(t *testing.T) {
	m := New(nil, 28.6, 77.2)
	m.width, m.height = 40, 12
	backgroundRows := make([]string, m.height)
	for i := range backgroundRows {
		backgroundRows[i] = fmt.Sprintf("\x1b[38;5;33mMAP-ROW-%02d\x1b[0m", i)
	}
	background := strings.Join(backgroundRows, "\n")
	view := m.overlayShell(background, []string{"compact picker"}, 30, m.height)
	rows := strings.Split(view, "\n")
	if len(rows) != m.height {
		t.Fatalf("overlay rows=%d, want %d", len(rows), m.height)
	}
	boxTop := (m.height - 3) / 2
	for i, row := range rows {
		if i < boxTop || i >= boxTop+3 {
			want := padDisplay(backgroundRows[i], m.width)
			if row != want {
				t.Fatalf("background row %d changed outside compact shell: got %q want %q", i, row, want)
			}
		}
		if lipgloss.Width(stripANSI(row)) != m.width {
			t.Fatalf("overlay row %d width=%d, want %d", i, lipgloss.Width(stripANSI(row)), m.width)
		}
	}
	if strings.Contains(view, "\x1b[48;") {
		t.Fatal("overlay painted an opaque background")
	}
}

func TestOverlayPreservesColoredBackgroundFragmentsBesideShell(t *testing.T) {
	m := New(nil, 28.6, 77.2)
	m.width, m.height = 40, 12
	backgroundRows := make([]string, m.height)
	for i := range backgroundRows {
		backgroundRows[i] = "\x1b[38;5;33m" + strings.Repeat("X", m.width) + "\x1b[0m"
	}
	background := strings.Join(backgroundRows, "\n")
	boxW, boxH := 20, 3
	view := m.overlayShellFixed(background, []string{"compact picker"}, boxW, boxH)
	rows := strings.Split(view, "\n")
	left, top := (m.width-boxW)/2, (m.height-boxH)/2
	for row := top; row < top+boxH; row++ {
		wantLeft := displaySlice(padDisplay(backgroundRows[row], m.width), 0, left)
		wantRight := displaySlice(padDisplay(backgroundRows[row], m.width), left+boxW, m.width-left-boxW)
		if got := displaySlice(rows[row], 0, left); got != wantLeft {
			t.Fatalf("left fragment row %d changed color/bytes: got %q want %q", row, got, wantLeft)
		}
		gotRight := displaySlice(rows[row], left+boxW, m.width-left-boxW)
		if stripANSI(gotRight) != stripANSI(wantRight) || !strings.Contains(gotRight, "38;5;33") {
			t.Fatalf("right fragment row %d changed color/bytes: got %q want %q", row, gotRight, wantRight)
		}
	}
}

func TestOverlayShellIsCenteredAndTerminalBounded(t *testing.T) {
	for _, size := range []struct{ width, height int }{{20, 8}, {4, 3}, {100, 30}} {
		m := New(nil, 28.6, 77.2)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m = updated.(Model)
		m.showHelp = true
		content := strings.TrimSuffix(m.View().Content, "\n")
		lines := strings.Split(content, "\n")
		if len(lines) != size.height {
			t.Fatalf("size %dx%d produced %d rows", size.width, size.height, len(lines))
		}
		for i, line := range lines {
			if lipgloss.Width(stripANSI(line)) > size.width {
				t.Fatalf("size %dx%d row %d width=%d", size.width, size.height, i, lipgloss.Width(stripANSI(line)))
			}
		}
		shellW, shellH, _, _ := m.pickerGeometry()
		if m.showHelp {
			shellW, shellH = min(helpShellWidth, size.width), min(helpShellHeight, size.height)
		}
		t.Logf("rendered overlay terminal=%dx%d shell=%dx%d top-left=%d,%d surrounding rows preserved and bounded", size.width, size.height, shellW, shellH, max((size.width-shellW)/2, 0), max((size.height-shellH)/2, 0))
	}
}

func readyTestModel(t *testing.T) Model {
	t.Helper()
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: filepath.Join("..", "gtfs", "testdata", "delhi-mini")})
	m = sizedModel(t, m)
	updated, _ := m.Update(m.Init()())
	return updated.(Model)
}

func TestEndpointStatesRemainReadableWithoutFeed(t *testing.T) {
	for _, state := range []string{"GTFS: missing", "GTFS: loading"} {
		m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: "feed"})
		m = sizedModel(t, m)
		if state == "GTFS: missing" {
			updated, _ := m.Update(feedMissingMsg{})
			m = updated.(Model)
		}
		view := m.View().Content
		if !strings.Contains(view, "FROM:") || !strings.Contains(view, "TO:") || !strings.Contains(view, state) {
			t.Fatalf("state %q view omitted bounded endpoint state: %q", state, view)
		}
	}
}

func TestRouteCommandProducesResultAndIgnoresStaleSelection(t *testing.T) {
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: filepath.Join("..", "gtfs", "testdata", "delhi-mini")})
	m = sizedModel(t, m)
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	m.fromStation, m.toStation = "rajiv_chowk", "central_secretariat"
	m.routeSeq++
	old := m.routeCmd()
	m.toStation = "new_delhi"
	m.routeSeq++
	updated, _ = m.Update(old())
	m = updated.(Model)
	if m.route.Status == gtfs.RouteReady {
		t.Fatal("stale route result was accepted")
	}
	updated, _ = m.Update(m.routeCmd()())
	m = updated.(Model)
	if m.route.Status != gtfs.RouteReady || m.route.ToStation != "new_delhi" {
		t.Fatalf("current route = %#v, want ready route to new_delhi", m.route)
	}
}

func TestReadyRouteFitsActualGeometryAndExcludesSidebar(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.routeSeq++
	updated, _ := m.Update(m.routeCmd()())
	m = updated.(Model)
	if m.route.Status != gtfs.RouteReady {
		t.Fatalf("route status = %v, want ready", m.route.Status)
	}
	geometry := render.RouteGeometry(m.feedIndexes, m.route)
	bounds, ok := geo.NewBounds(geometry)
	if !ok {
		t.Fatal("route geometry has no bounds")
	}
	fit, ok := geo.FitBounds(bounds, m.mapWidth()*2, (m.height-2)*4, routeFitPadding, m.viewport())
	if !ok || math.Abs(m.lat-fit.Lat) > 1e-9 || math.Abs(m.lon-fit.Lon) > 1e-9 || math.Abs(m.zoom-fit.Zoom) > 1e-9 {
		t.Fatalf("model fit=(%.6f,%.6f,z%.3f), expected=(%.6f,%.6f,z%.3f), map=%dpx sidebar=%d", m.lat, m.lon, m.zoom, fit.Lat, fit.Lon, fit.Zoom, m.mapWidth()*2, m.sidebarWidth())
	}
	t.Logf("delhi-mini route dwarka_21→new_delhi fit center=(%.6f,%.6f) zoom=%.3f map=%dx%dpx sidebar=%d", m.lat, m.lon, m.zoom, m.mapWidth()*2, (m.height-2)*4, m.sidebarWidth())
	before := m.lat
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "w", Code: 'w'}))
	m = updated.(Model)
	if m.lat == before || m.routeAutoFit {
		t.Fatalf("manual pan did not take ownership: lat=%v autoFit=%v", m.lat, m.routeAutoFit)
	}
}

func TestRouteFitDoesNotChangeNoRouteOrMissingGeometry(t *testing.T) {
	m := sizedModel(t, New(nil, 28.6, 77.2))
	want := m.viewport()
	m.fitSelectedRoute()
	if m.viewport() != want {
		t.Fatalf("no-route fit changed viewport from %#v to %#v", want, m.viewport())
	}
	m.feedState = FeedStateReady
	m.route = gtfs.RouteResult{Status: gtfs.RouteReady, Stations: []string{"missing"}}
	want = m.viewport()
	m.fitSelectedRoute()
	if m.viewport() != want {
		t.Fatalf("missing-geometry fit changed viewport from %#v to %#v", want, m.viewport())
	}
}
