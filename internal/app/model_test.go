package app

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/gtfs"
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

func TestPickerAndHelpUseNeutralColoredBordersAtFrameSize(t *testing.T) {
	m := readyTestModel(t)
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	view := m.View().Content
	if !strings.Contains(view, "\x1b[38;5;238m╭") || !strings.Contains(view, "\x1b[38;5;238m│") {
		t.Fatalf("picker borders are not explicitly neutral-colored: %q", view)
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
	if !strings.Contains(view, "\x1b[38;5;238m╭") {
		t.Fatalf("help border is not explicitly neutral-colored: %q", view)
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
	if strings.Contains(m.View().Content, "\x1b[48;5;0m") {
		t.Fatal("overlay painted an opaque black background")
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "esc"}))
	m = modelValue(updated)
	if !strings.Contains(stripANSI(strings.Join(m.sidebarLines(20, 40), "\n")), "METROSHELL") {
		t.Fatal("sidebar lost centered wordmark")
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
