package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
