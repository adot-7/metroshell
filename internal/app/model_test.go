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

func TestMissingConfiguredFeedCanBeRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: path})
	m = sizedModel(t, m)
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	if m.FeedState() != FeedStateMissing || !strings.Contains(stripANSI(m.View().Content), "r retry") {
		t.Fatalf("missing feed view = %q, want retry affordance", stripANSI(m.View().Content))
	}
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "r", Code: 'r'}))
	m = updated.(Model)
	if m.FeedState() != FeedStateLoading || cmd == nil {
		t.Fatalf("retry state=%v cmd=%v, want loading with command", m.FeedState(), cmd != nil)
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, child := range batch {
			message := child()
			if message == nil {
				continue
			}
			updated, _ = m.Update(message)
			m = modelValue(updated)
		}
	} else {
		updated, _ = m.Update(cmd())
		m = modelValue(updated)
	}
	if m.FeedState() != FeedStateMissing || !strings.Contains(m.notice, "Map only") {
		t.Fatalf("retry result state=%v notice=%q, want missing/map-only", m.FeedState(), m.notice)
	}
}

func TestPickerHandoffAndFineZoomAliasesAreVisible(t *testing.T) {
	m := readyTestModel(t)
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "tab", Code: tea.KeyTab}))
	m = modelValue(updated)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	m = modelValue(updated)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	m = modelValue(updated)
	if m.focus != focusTo || !strings.Contains(m.notice, "FROM set") {
		t.Fatalf("FROM handoff focus=%v notice=%q, want TO and explicit handoff", m.focus, m.notice)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "esc", Code: tea.KeyEscape}))
	m = modelValue(updated)
	zoom := m.zoom
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: ",", Code: ','}))
	m = modelValue(updated)
	if m.zoom <= zoom {
		t.Fatalf("comma zoom=%v, want greater than %v", m.zoom, zoom)
	}
	if !strings.Contains(m.hudText(), "FOCUS:TO") {
		t.Fatalf("HUD omitted active endpoint focus: %q", m.hudText())
	}
}

func TestSplashLifecycleIsBoundedSkippableAndFeedLoadsBehindIt(t *testing.T) {
	path := filepath.Join("..", "gtfs", "testdata", "delhi-mini")
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: path})
	if !m.SplashVisible() || m.FeedState() != FeedStateLoading {
		t.Fatalf("initial splash/feed state = %v/%v, want visible/loading", m.SplashVisible(), m.FeedState())
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = modelValue(updated)
	plain := stripANSI(m.View().Content)
	for _, want := range []string{"METROSHELL", "DELHI METRO STARTING IN YOUR TERMINAL", "built by Akash Parashar"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("splash omitted %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, " AO") || strings.Contains(plain, "manager of agents") || strings.Contains(plain, "built by Akash Parashar ·") {
		t.Fatalf("splash credit was not Akash-only: %q", plain)
	}
	for _, want := range []string{"METROSHELL", "DELHI METRO STARTING IN YOUR TERMINAL", "built by Akash Parashar"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("splash omitted exact copy %q: %q", want, plain)
		}
	}
	updated, _ = m.Update(m.Init()())
	m = modelValue(updated)
	if !m.SplashVisible() || m.FeedState() != FeedStateReady {
		t.Fatalf("feed did not progress behind splash: splash=%v feed=%v", m.SplashVisible(), m.FeedState())
	}
	for _, size := range [][2]int{{100, 30}, {50, 12}} {
		m = NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: path})
		updated, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = modelValue(updated)
		if !m.SplashVisible() {
			t.Fatal("resize dismissed splash")
		}
		assertBoundedView(t, m, size[0], size[1])
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
		m = modelValue(updated)
		if m.SplashVisible() {
			t.Fatalf("Enter did not dismiss splash at %dx%d", size[0], size[1])
		}
	}
}

func TestSplashQuitKeysRemainAvailable(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		m := New(nil, 28.6, 77.2)
		updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: key}))
		if cmd == nil || updated.(Model).SplashVisible() == false {
			t.Fatalf("%s did not quit while splash was visible", key)
		}
	}
}

func TestSplashCopyIsCenteredLargerAndCompactBounded(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {207, 50}, {50, 12}, {20, 8}, {4, 3}} {
		m := New(nil, 28.6, 77.2)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = modelValue(updated)
		plain := stripANSI(m.View().Content)
		if (size[0] >= lipgloss.Width("built by Akash Parashar") && !strings.Contains(plain, "built by Akash Parashar")) || strings.Contains(plain, " AO") || strings.Contains(plain, "manager of agents") {
			t.Fatalf("size %dx%d rendered invalid splash credit: %s", size[0], size[1], plain)
		}
		assertBoundedView(t, m, size[0], size[1])
		if size[0] < splashShellWidth || size[1] < splashShellHeight {
			continue
		}
		rows := strings.Split(plain, "\n")
		shellLeft := (size[0] - splashShellWidth) / 2
		shellTop := (size[1] - splashShellHeight) / 2
		for _, text := range []string{"METROSHELL", "DELHI METRO STARTING IN YOUR TERMINAL", "Press Enter to continue", "built by Akash Parashar"} {
			row := -1
			left := -1
			for i := shellTop; i < shellTop+splashShellHeight; i++ {
				if i >= len(rows) {
					break
				}
				if candidate := strings.Index(rows[i], text); candidate >= 0 {
					row, left = i, candidate
					break
				}
			}
			if row < 0 {
				t.Fatalf("size %dx%d omitted splash copy %q", size[0], size[1], text)
			}
			if left < shellLeft+2 || left+lipgloss.Width(text) > shellLeft+splashShellWidth-2 {
				t.Fatalf("size %dx%d splash copy %q row=%d left=%d escaped shell bounds", size[0], size[1], text, row, left)
			}
		}
		t.Logf("native splash evidence at %dx%d: centered %dx%d shell with exact Akash-only copy", size[0], size[1], splashShellWidth, splashShellHeight)
	}
}

func TestSplashNativeEvidenceHasPinkIdentityAndBalancedCopy(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {207, 50}, {50, 12}} {
		m := New(nil, 28.6, 77.2)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = modelValue(updated)
		view := m.View().Content
		plainRows := strings.Split(stripANSI(view), "\n")
		rows := strings.Split(view, "\n")
		shellW := min(splashShellWidth, size[0])
		shellH := min(splashShellHeight, size[1])
		shellLeft := (size[0] - shellW) / 2
		shellTop := (size[1] - shellH) / 2
		copyLines := []string{"METROSHELL", "DELHI METRO STARTING IN YOUR TERMINAL", "built by Akash Parashar"}
		positions := make([]int, len(copyLines))
		for i, text := range copyLines {
			positions[i] = -1
			for row := shellTop; row < min(shellTop+shellH, len(plainRows)); row++ {
				if strings.Contains(plainRows[row], text) {
					positions[i] = row
					break
				}
			}
		}
		if size[0] >= 50 {
			for i, row := range positions {
				if row < 0 {
					t.Fatalf("size %dx%d omitted splash copy line %q", size[0], size[1], copyLines[i])
				}
			}
			if positions[1] != positions[0]+1 || positions[2] != positions[0]+4 {
				t.Fatalf("size %dx%d splash copy rows=%v, want title/subtitle adjacent and credit balanced", size[0], size[1], positions)
			}
			leading := positions[0] - (shellTop + 1)
			trailing := shellTop + shellH - 2 - positions[2]
			delta := leading - trailing
			if delta < 0 {
				delta = -delta
			}
			if delta > 1 {
				t.Fatalf("size %dx%d splash vertical balance leading=%d trailing=%d rows=%v", size[0], size[1], leading, trailing, positions)
			}
			if !strings.Contains(rows[positions[0]], "38;5;201m") || !strings.Contains(rows[positions[1]], "38;5;201m") {
				t.Fatalf("size %dx%d splash branding lost pink styling: %q / %q", size[0], size[1], rows[positions[0]], rows[positions[1]])
			}
			if strings.Count(stripANSI(strings.Join(plainRows[shellTop:shellTop+shellH], "\n")), "built by Akash Parashar") != 1 {
				t.Fatalf("size %dx%d splash credit was not exactly one Akash line", size[0], size[1])
			}
		}
		if !strings.Contains(view, "\x1b[38;5;201m╭") || !strings.Contains(view, "\x1b[38;5;201m╰") {
			t.Fatalf("size %dx%d splash outer chrome was not pink", size[0], size[1])
		}
		assertBoundedView(t, m, size[0], size[1])
		t.Logf("native splash evidence %dx%d: pink %dx%d shell centered at (%d,%d), copy rows=%v, balanced credit", size[0], size[1], shellW, shellH, shellLeft, shellTop, positions)
	}
}

func TestSplashShowsFeedErrorsWithoutBlockingDismissal(t *testing.T) {
	m := NewWithConfig(nil, 28.6, 77.2, Config{GTFSPath: filepath.Join(t.TempDir(), "missing-feed")})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = modelValue(updated)
	updated, _ = m.Update(m.Init()())
	m = modelValue(updated)
	if m.FeedState() != FeedStateMissing || !m.SplashVisible() {
		t.Fatalf("missing feed behind splash = state:%v splash:%v", m.FeedState(), m.SplashVisible())
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	if !strings.Contains(stripANSI(modelValue(updated).View().Content), "GTFS: missing") {
		t.Fatal("feed error state was not visible after splash dismissal")
	}
}

func TestPersistentDiscoverabilityIsConcise(t *testing.T) {
	m := sizedModel(t, New(nil, 28.6, 77.2))
	plain := stripANSI(strings.Join(m.sidebarLines(28, m.sidebarWidth()), "\n"))
	if strings.Contains(plain, "Tab · Enter/search") || strings.Contains(plain, "j/k leg · Enter expand · ? help") {
		t.Fatalf("persistent shortcut hint was not removed: %q", plain)
	}
	if strings.Contains(plain, "STATIONS") || strings.Contains(plain, "timetable") || strings.Contains(plain, "EXPANDED") {
		t.Fatalf("persistent discoverability reintroduced clutter: %q", plain)
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

func TestDefaultTrainAccelerationUsesCalmDemoPace(t *testing.T) {
	m := New(nil, 28.6, 77.2)
	if m.trainAcceleration != 15 {
		t.Fatalf("default train acceleration=%g, want 15x calm demo pace", m.trainAcceleration)
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
	before := m.lat
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "?"}))
	m = updated.(Model)
	if !m.showHelp || m.fromStation != "dwarka_21" || m.toStation != "new_delhi" || m.route.Status != gtfs.RouteReady {
		t.Fatal("opening help changed route/endpoint state")
	}
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
	resized := updated.(Model)
	updated, _ = resized.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	return updated.(Model)
}

func TestModelPreservesMapControlsAndViewOptions(t *testing.T) {
	m := New(nil, 28.6139, 77.2090)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "w", Code: 'w'}))
	if cmd == nil {
		t.Fatal("pan key did not request a render")
	}
	m = updated.(Model)
	if m.lat <= 28.6139 {
		t.Fatalf("pan key did not move north: %v", m.lat)
	}

	updated, cmd = m.Update(tea.KeyPressMsg(tea.Key{Text: "+", Code: '+'}))
	if cmd == nil {
		t.Fatal("zoom key did not request a render")
	}
	m = updated.(Model)
	if m.zoom != 12.2 {
		t.Fatalf("zoom key zoom = %v, want 12.2", m.zoom)
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "?", Code: '?'}))
	m = updated.(Model)
	if !m.showHelp || !strings.Contains(m.View().Content, "keybindings") {
		t.Fatal("help key did not show the help screen")
	}

	view := m.View()
	if !view.AltScreen || view.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("view did not enable alternate screen with wheel-only cell-motion input")
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
		if strings.Contains(plain, "highlighted on map") {
			t.Fatalf("route state %v retained removed map label: %q", want.status, plain)
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
	if strings.Contains(view, "highlighted on map") || !strings.Contains(view, "transfers") {
		t.Fatalf("connected route summary missing: %q", view)
	}
}

func TestMouseSelectionIsDisabledAndKeyboardPickerRemainsAvailable(t *testing.T) {
	m := readyTestModel(t)
	before := m
	for _, pointer := range []tea.Msg{
		tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft},
		tea.MouseReleaseMsg{X: 3, Y: 3, Button: tea.MouseLeft},
		tea.MouseMotionMsg{X: 4, Y: 4, Button: tea.MouseLeft},
	} {
		updated, cmd := m.Update(pointer)
		m = modelValue(updated)
		if cmd != nil {
			t.Fatalf("pointer event %T unexpectedly requested a command", pointer)
		}
	}
	if m.fromStation != before.fromStation || m.toStation != before.toStation || m.lat != before.lat || m.lon != before.lon || m.zoom != before.zoom || m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("pointer input changed application state: from=%q to=%q lat/lon=%f/%f zoom=%f mode=%v", m.fromStation, m.toStation, m.lat, m.lon, m.zoom, m.View().MouseMode)
	}
	for _, forbidden := range []string{"mouse", "cursor", "click map", "◎"} {
		if strings.Contains(strings.ToLower(stripANSI(m.helpContent())), forbidden) || strings.Contains(stripANSI(m.View().Content), forbidden) {
			t.Fatalf("removed map-pointer affordance %q remained visible", forbidden)
		}
	}
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "tab"}))
	m = modelValue(updated)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	if !m.picker {
		t.Fatal("keyboard Enter did not open endpoint picker")
	}
}

func TestMouseWheelOnlyZoomsWithoutPanningOrPointerState(t *testing.T) {
	m := readyTestModel(t)
	lat, lon, zoom := m.lat, m.lon, m.zoom
	updated, cmd := m.Update(tea.MouseWheelMsg(tea.Mouse{X: 12, Y: 8, Button: tea.MouseWheelUp}))
	m = modelValue(updated)
	if cmd == nil || m.zoom != zoom+0.1 || m.lat != lat || m.lon != lon {
		t.Fatalf("wheel up changed unexpected state: zoom=%v want=%v lat/lon=%v/%v want=%v/%v cmd=%v", m.zoom, zoom+0.1, m.lat, m.lon, lat, lon, cmd != nil)
	}
	updated, cmd = m.Update(tea.MouseWheelMsg(tea.Mouse{X: 12, Y: 8, Button: tea.MouseWheelDown}))
	m = modelValue(updated)
	if cmd == nil || m.zoom != zoom || m.lat != lat || m.lon != lon {
		t.Fatalf("wheel down changed unexpected state: zoom=%v want=%v lat/lon=%v/%v want=%v/%v cmd=%v", m.zoom, zoom, m.lat, m.lon, lat, lon, cmd != nil)
	}
	plain := stripANSI(m.View().Content) + "\n" + stripANSI(m.helpContent())
	for _, forbidden := range []string{"◎", "cursor", "mouse", "click map"} {
		if strings.Contains(strings.ToLower(plain), strings.ToLower(forbidden)) {
			t.Fatalf("wheel-only input restored forbidden affordance %q", forbidden)
		}
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
		t.Fatal("picker changed map before keyboard dismissal")
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
	fromTop, fromBody, fromBottom := lines[3], lines[4], lines[5]
	toTop, toBody, toBottom := lines[7], lines[8], lines[9]
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

func TestAMOLEDPanelsHaveIndependentPinkShellsAndDeliberateGap(t *testing.T) {
	m := sizedModel(t, New(nil, 28.6, 77.2))
	plain := stripANSI(m.View().Content)
	rows := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
	if len(rows) != 30 {
		t.Fatalf("normal fixture rows=%d, want 30", len(rows))
	}
	for i, row := range rows {
		if lipgloss.Width(row) != 100 {
			t.Fatalf("normal fixture row %d width=%d, want 100", i, lipgloss.Width(row))
		}
	}
	mapWidth, sidebarWidth := m.mapWidth(), m.sidebarWidth()
	if mapWidth != 55 || sidebarWidth != 40 {
		t.Fatalf("100x30 panel widths map=%d sidebar=%d, want 55/40", mapWidth, sidebarWidth)
	}
	for row, line := range rows {
		cells := []rune(line)
		if cells[mapWidth+2] != ' ' {
			t.Fatalf("row %d has no one-cell panel gap at column %d: %q", row, mapWidth+2, line)
		}
		if row > 0 && row < len(rows)-1 && cells[mapWidth+3] != '│' {
			t.Fatalf("row %d sidebar shell column moved: %q", row, line)
		}
		if row > 0 && row < len(rows)-1 && cells[len(cells)-1] != '│' {
			t.Fatalf("row %d outer shell column moved: %q", row, line)
		}
	}
	view := m.View().Content
	if strings.Count(view, "\x1b[38;5;245m╭") < 1 || strings.Count(view, "\x1b[38;5;245m╰") < 1 || strings.Count(view, "\x1b[38;5;239m╭") < 1 || strings.Count(view, "\x1b[38;5;239m╰") < 1 {
		t.Fatalf("map/sidebar did not each receive neutral outer borders: %q", view)
	}
	if !strings.Contains(view, "\x1b[38;5;238m╭") || !strings.Contains(view, "\x1b[38;5;238m╰") {
		t.Fatalf("endpoint fields lost neutral gray inner borders: %q", view)
	}
	t.Logf("native terminal fixture 100x30: map panel=%d cols, sidebar panel=%d cols, gap=%d col; independent neutral shells and gray endpoint fields verified", mapWidth+2, sidebarWidth+2, panelGap)
}

func TestAMOLEDPanelBoundaryAndCompactInstruction(t *testing.T) {
	for _, width := range []int{50, 51} {
		m := New(nil, 28.6, 77.2)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = modelValue(updated)
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
		m = modelValue(updated)
		if m.sidebarWidth() != 0 {
			t.Fatalf("demo boundary %d unexpectedly stacked/sidebar width=%d", width, m.sidebarWidth())
		}
		if !strings.Contains(stripANSI(m.View().Content), "Enlarge terminal to 52 columns") {
			t.Fatalf("width %d omitted concise enlarge-terminal instruction", width)
		}
		assertBoundedView(t, m, width, 30)
		t.Logf("native terminal fixture %dx30: side-by-side disabled at demo boundary; resize instruction bounded", width)
	}

	m := New(nil, 28.6, 77.2)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = modelValue(updated)
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	m = modelValue(updated)
	if strings.Contains(stripANSI(m.View().Content), "METROSHELL") {
		t.Fatal("compact unsupported width introduced a stacked sidebar")
	}
	if !strings.Contains(stripANSI(m.View().Content), "Enlarge terminal") {
		t.Fatal("compact unsupported width omitted resize guidance")
	}
	assertBoundedView(t, m, 40, 12)
	t.Log("native terminal fixture 40x12: compact map-only panel remains bounded with resize guidance")
}

func assertBoundedView(t *testing.T, m Model, width, height int) {
	t.Helper()
	rows := strings.Split(strings.TrimSuffix(m.View().Content, "\n"), "\n")
	if len(rows) != height {
		t.Fatalf("view rows=%d, want %d", len(rows), height)
	}
	for i, row := range rows {
		if got := lipgloss.Width(stripANSI(row)); got != width {
			t.Fatalf("view row %d width=%d, want %d: %q", i, got, width, row)
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
	if !strings.Contains(view, "\x1b[38;5;245m╭") || !strings.Contains(view, "\x1b[38;5;238m╭") {
		t.Fatalf("picker did not split neutral outer and inner borders: %q", view)
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
	if !strings.Contains(view, "\x1b[38;5;245m╭") {
		t.Fatalf("help shell did not retain neutral outer border: %q", view)
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
	if !strings.Contains(view, "\x1b[38;5;245m╭") {
		t.Fatal("application frame did not retain neutral theme")
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
	if !strings.Contains(stripANSI(sidebar), "METROSHELL") || !strings.Contains(stripANSI(sidebar), "13 Aug 2026") || strings.Contains(stripANSI(sidebar), "DELHI ") || strings.Contains(sidebar, "—") || strings.Contains(stripANSI(sidebar), "ENDPOINTS") {
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
	wall := time.Date(2026, 8, 13, 9, 7, 0, 0, gtfs.DelhiLocation)
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{GTFSPath: filepath.Join("..", "gtfs", "testdata", "delhi-mini"), Now: func() time.Time { return wall }})
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

func TestSingleDelhiWallClockIsInjectedAndNeverPlaybackClock(t *testing.T) {
	wall := time.Date(2026, 8, 13, 9, 7, 0, 0, gtfs.DelhiLocation)
	m := NewWithConfig(nil, 28.6139, 77.2090, Config{Now: func() time.Time { return wall }})
	m = sizedModel(t, m)
	plain := stripANSI(m.View().Content)
	if strings.Count(plain, "13 Aug 2026 09:07:00") != 1 {
		t.Fatalf("wall clock occurrences=%d view=%q", strings.Count(plain, "13 Aug 2026 09:07:00"), plain)
	}
	if strings.Contains(plain, "DELHI ") {
		t.Fatalf("wall clock retained DELHI prefix: %q", plain)
	}
	if strings.Contains(plain, "SIM:") || strings.Contains(plain, "PLAYBACK") {
		t.Fatalf("competing simulator readout in view=%q", plain)
	}
	lines := strings.Split(strings.Join(m.sidebarLines(m.height-2, m.sidebarWidth()), "\n"), "\n")
	indexOf := func(value string) int {
		for i, line := range lines {
			if strings.Contains(stripANSI(line), value) {
				return i
			}
		}
		return -1
	}
	title, clock := indexOf("METROSHELL"), indexOf("13 Aug 2026 09:07:00")
	if title < 0 || clock != title+1 {
		t.Fatalf("sidebar clock position title=%d clock=%d, want clock directly below title: %q", title, clock, strings.Join(lines, "\n"))
	}
	if strings.Contains(m.hudText(), "13 Aug 2026 09:07:00") {
		t.Fatal("bottom-left HUD retained the Delhi wall clock")
	}
}

func TestScheduleMotionUsesCalendarAndConfigurableAcceleration(t *testing.T) {
	wall := time.Date(2025, 1, 6, 7, 0, 0, 0, gtfs.DelhiLocation)
	path := filepath.Join("..", "gtfs", "testdata", "scheduled-mini")
	makeModel := func(acceleration float64) Model {
		m := NewWithConfig(nil, 28.6, 77.2, Config{GTFSPath: path, Now: func() time.Time { return wall }, TrainAcceleration: acceleration})
		m = sizedModel(t, m)
		updated, _ := m.Update(m.Init()())
		m = updated.(Model)
		m.fromStation, m.toStation = "a", "c"
		updated, _ = m.Update(m.routeCmd()())
		return updated.(Model)
	}
	fast, calm := makeModel(40), makeModel(20)
	fast.trainClock, calm.trainClock = sim.ClockCycle/2, sim.ClockCycle/2
	if fast.route.Schedule.Status == gtfs.ScheduleUnavailable || calm.route.Schedule.Status == gtfs.ScheduleUnavailable {
		t.Fatalf("expected scheduled route: fast=%#v calm=%#v", fast.route.Schedule, calm.route.Schedule)
	}
	if !reflect.DeepEqual(fast.SimulationSnapshot(), fast.SimulationSnapshot()) || !reflect.DeepEqual(calm.SimulationSnapshot(), calm.SimulationSnapshot()) {
		t.Fatal("simulation snapshot was not deterministic")
	}
	if reflect.DeepEqual(fast.SimulationSnapshot(), calm.SimulationSnapshot()) {
		t.Fatal("configurable acceleration did not affect train view")
	}
	noService := makeModel(20)
	noService.clock = func() time.Time { return time.Date(2026, 1, 11, 7, 0, 0, 0, gtfs.DelhiLocation) }
	if routes := simulationRoutes(noService.feedIndexes, noService.now()); len(routes) != 0 {
		t.Fatalf("no-service calendar still produced %d active train routes", len(routes))
	}
}

func TestSimulationRouteCacheHitsAndInvalidatesDeterministically(t *testing.T) {
	wall := time.Date(2025, 1, 6, 7, 0, 0, 0, gtfs.DelhiLocation)
	m := NewWithConfig(nil, 28.6, 77.2, Config{GTFSPath: filepath.Join("..", "gtfs", "testdata", "scheduled-mini"), Now: func() time.Time { return wall }, TrainAcceleration: 20})
	m = sizedModel(t, m)
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	_ = m.SimulationConfig()
	if got := m.SimulationCacheStats(); got.Misses != 1 || got.Hits != 0 {
		t.Fatalf("first simulation config stats=%+v, want one miss", got)
	}
	_ = m.SimulationConfig()
	if got := m.SimulationCacheStats(); got.Hits != 1 || got.Misses != 1 {
		t.Fatalf("repeated simulation config stats=%+v, want one hit", got)
	}
	wall = wall.AddDate(0, 0, 1)
	_ = m.SimulationConfig()
	if got := m.SimulationCacheStats(); got.Invalidations != 1 || got.Misses != 2 {
		t.Fatalf("date transition stats=%+v, want deterministic invalidation", got)
	}
	wall = time.Date(2025, 1, 12, 7, 0, 0, 0, gtfs.DelhiLocation)
	if routes := m.cachedSimulationRoutes(wall); len(routes) != 0 {
		t.Fatalf("Sunday no-service transition retained %d routes", len(routes))
	}
	wall = time.Date(2025, 1, 13, 7, 0, 0, 0, gtfs.DelhiLocation)
	if routes := m.cachedSimulationRoutes(wall); len(routes) == 0 {
		t.Fatal("Monday service transition did not restore routes")
	}
	m.trainAcceleration = 40
	_ = m.SimulationConfig()
	if got := m.SimulationCacheStats(); got.Invalidations != 4 || got.Misses != 5 {
		t.Fatalf("acceleration change stats=%+v, want config invalidation", got)
	}
	updated, _ = m.Update(feedReadyMsg{seq: m.feedSeq, feed: m.feed, indexes: m.feedIndexes})
	m = updated.(Model)
	if got := m.SimulationCacheStats(); got != (SimulationCacheStats{}) {
		t.Fatalf("feed reload retained cache stats=%+v", got)
	}
	_ = m.SimulationConfig()
	if got := m.SimulationCacheStats(); got.Misses != 1 {
		t.Fatalf("reloaded feed stats=%+v, want fresh miss", got)
	}
}

func TestCachedSimulationRoutesAndSnapshotsMatchUncachedAcrossBoundaries(t *testing.T) {
	path := filepath.Join("..", "gtfs", "testdata", "scheduled-mini")
	feed, indexes, missing, err := loadFeed(t.Context(), path)
	if missing || err != nil {
		t.Fatalf("load representative schedule fixture: missing=%v err=%v", missing, err)
	}
	assertCachedSimulationEquivalence(t, feed, indexes)

	if path := os.Getenv("METROSHELL_GTFS_FEED"); path != "" {
		feed, indexes, missing, err = loadFeed(t.Context(), path)
		if missing || err != nil {
			t.Fatalf("load extracted feed: missing=%v err=%v", missing, err)
		}
		assertCachedSimulationEquivalence(t, feed, indexes)
	}
}

func assertCachedSimulationEquivalence(t *testing.T, feed gtfs.Feed, indexes gtfs.Indexes) {
	t.Helper()
	wall := time.Date(2025, 1, 6, 7, 0, 0, 0, gtfs.DelhiLocation)
	m := NewWithConfig(nil, 28.6, 77.2, Config{Now: func() time.Time { return wall }, TrainAcceleration: 20})
	m.feed, m.feedIndexes, m.feedState, m.feedSeq = feed, indexes, FeedStateReady, 1
	m.width, m.height = 100, 30

	for _, test := range []struct {
		name         string
		date         time.Time
		acceleration float64
		clock        int64
		feedBump     bool
	}{
		{name: "active weekday", date: time.Date(2025, 1, 6, 7, 0, 0, 0, gtfs.DelhiLocation), acceleration: 20, clock: 17},
		{name: "no service Sunday", date: time.Date(2025, 1, 12, 7, 0, 0, 0, gtfs.DelhiLocation), acceleration: 20, clock: 31},
		{name: "next service date", date: time.Date(2025, 1, 13, 7, 0, 0, 0, gtfs.DelhiLocation), acceleration: 20, clock: 43},
		{name: "acceleration change", date: time.Date(2025, 1, 13, 7, 0, 0, 0, gtfs.DelhiLocation), acceleration: 40, clock: 59},
		{name: "feed generation change", date: time.Date(2025, 1, 13, 7, 0, 0, 0, gtfs.DelhiLocation), acceleration: 40, clock: 71, feedBump: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			wall = test.date
			m.trainAcceleration = test.acceleration
			m.trainClock = test.clock
			if test.feedBump {
				m.feedSeq++
			}
			uncachedRoutes := simulationRoutes(indexes, test.date)
			cachedRoutes := m.cachedSimulationRoutes(test.date)
			if !reflect.DeepEqual(cachedRoutes, uncachedRoutes) {
				t.Fatalf("cached routes differ from uncached routes: cached=%d uncached=%d", len(cachedRoutes), len(uncachedRoutes))
			}
			uncachedConfig := sim.Config{Seed: m.trainSeed, Clock: m.trainClock, Fleet: m.trainFleet, Paused: m.simulationPaused(), ReducedMotion: m.simulationReducedMotion(), Acceleration: m.trainAcceleration, Routes: uncachedRoutes}
			if got, want := m.SimulationSnapshot(), sim.Snapshot(uncachedConfig); !reflect.DeepEqual(got, want) {
				t.Fatal("cached snapshot differs from uncached snapshot")
			}
		})
	}
}

func TestTimingUnavailableKeepsRouteStopsAndLegDetails(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.routeSeq++
	updated, _ := m.Update(m.routeCmd()())
	m = updated.(Model)
	m.route.Schedule = gtfs.JourneySchedule{Status: gtfs.ScheduleUnavailable, Message: "Scheduled times unavailable"}
	m.expandedLeg = 0
	plain := stripANSI(strings.Join(m.journeySummaryLines(40), "\n") + "\n" + strings.Join(m.expandedLegLines(0, m.route.Legs[0], 40), "\n"))
	for _, want := range []string{"TIMING UNAVAILABLE", "Dwarka Sector 21", "Rajiv Chowk", "Blue Line", "2 stops"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("timing fallback omitted %q: %q", want, plain)
		}
	}
}

func TestTimingUnavailableSidebarHasNoPreLegStationsButExpandedStops(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	m.route.Schedule = gtfs.JourneySchedule{Status: gtfs.ScheduleUnavailable, Message: "Scheduled times unavailable"}
	m.selectedLeg = 0
	m.expandedLeg = 0

	plain := stripANSI(strings.Join(m.sidebarLines(50, 40), "\n"))
	if strings.Contains(plain, "STATIONS") {
		t.Fatalf("timing-unavailable sidebar reintroduced pre-leg STATIONS block: %q", plain)
	}
	if strings.Contains(plain, "Dwarka Sector 21 → Rajiv Chowk → New Delhi") {
		t.Fatalf("timing-unavailable sidebar reintroduced pre-leg station sequence: %q", plain)
	}
	for _, want := range []string{
		"2 stops · 1 transfers",
		"Blue Line",
		"Dwarka Sector 21 → Rajiv Chowk",
		"1 stop · duration unavailable",
		"Rajiv Chowk → New Delhi",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("timing-unavailable sidebar omitted %q: %q", want, plain)
		}
	}
	expanded := stripANSI(strings.Join(m.expandedLegLines(0, m.route.Legs[0], 40), "\n"))
	for _, want := range []string{"TIMING UNAVAILABLE", "Dwarka Sector 21", "Rajiv Chowk"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded timing fallback omitted %q: %q", want, expanded)
		}
	}
	if strings.Index(expanded, "Dwarka Sector 21") > strings.Index(expanded, "Rajiv Chowk") {
		t.Fatalf("expanded stops are not ordered: %q", expanded)
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

func TestJourneyTimelineUsesCompactLegFocusAndSingleExpansion(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	if m.route.Status != gtfs.RouteReady || len(m.route.Legs) != 2 {
		t.Fatalf("fixture route=%#v, want two legs", m.route)
	}
	m.route.Schedule = gtfs.JourneySchedule{
		Status:        gtfs.ScheduleAvailable,
		Duration:      25 * time.Minute,
		NextDeparture: time.Date(2026, 8, 13, 8, 0, 0, 0, gtfs.DelhiLocation),
		NextArrival:   time.Date(2026, 8, 13, 8, 25, 0, 0, gtfs.DelhiLocation),
		Legs: []gtfs.ScheduleLegDetail{
			{From: "dwarka_21", To: "rajiv_chowk", Stops: 1, Departure: time.Date(2026, 8, 13, 8, 0, 0, 0, gtfs.DelhiLocation), Arrival: time.Date(2026, 8, 13, 8, 20, 0, 0, gtfs.DelhiLocation)},
			{From: "rajiv_chowk", To: "new_delhi", Stops: 1, Departure: time.Date(2026, 8, 13, 8, 20, 0, 0, gtfs.DelhiLocation), Arrival: time.Date(2026, 8, 13, 8, 25, 0, 0, gtfs.DelhiLocation)},
		},
	}
	m.route.Schedule.Stops = []gtfs.ScheduleStopDetail{
		{StationID: "dwarka_21"}, {StationID: "rajiv_chowk"}, {StationID: "new_delhi"},
	}
	m.selectedLeg = 0
	plain := stripANSI(strings.Join(m.sidebarLines(28, 40), "\n"))
	for _, want := range []string{"JOURNEY", "SCHEDULED", "0h 25m", "2 stops", "1 transfers", "NEXT SERVICE", "Dwarka Sector 21 → Rajiv Chowk", "New Delhi"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("compact journey omitted %q: %q", want, plain)
		}
	}
	for _, forbidden := range []string{"08:00:00", "08:20:00", "08:25:00", "SCHEDULED STOP DETAIL"} {
		if strings.Contains(plain, forbidden) {
			t.Fatalf("compact journey exposed raw detail %q: %q", forbidden, plain)
		}
	}

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	if m.expandedLeg != 0 || !m.showScheduleDetail {
		t.Fatalf("Enter expansion selected=%d expanded=%d detail=%v", m.selectedLeg, m.expandedLeg, m.showScheduleDetail)
	}
	plain = stripANSI(strings.Join(m.sidebarLines(28, 40), "\n"))
	for _, want := range []string{"DEPART 08:00", "FROM Dwarka Sector 21", "ARRIVE 08:20", "TO Rajiv Chowk", "1 stops", "Dwarka Sector 21", "Rajiv Chowk"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expanded journey omitted %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "08:00:00") || strings.Contains(plain, "08:20:00") {
		t.Fatalf("expanded journey exposed raw seconds: %q", plain)
	}

	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "j"}))
	m = modelValue(updated)
	if m.selectedLeg != 1 || m.expandedLeg != 0 {
		t.Fatalf("j moved focus selected=%d expanded=%d, want selected 1 and expanded 0", m.selectedLeg, m.expandedLeg)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter"}))
	m = modelValue(updated)
	if m.selectedLeg != 1 || m.expandedLeg != 1 {
		t.Fatalf("Enter did not move expansion selected=%d expanded=%d", m.selectedLeg, m.expandedLeg)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "esc"}))
	m = modelValue(updated)
	if m.expandedLeg != -1 || m.showScheduleDetail {
		t.Fatalf("Esc did not collapse expanded leg=%d detail=%v", m.expandedLeg, m.showScheduleDetail)
	}
	updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "k"}))
	m = modelValue(updated)
	if m.selectedLeg != 0 {
		t.Fatalf("k selected leg=%d, want 0", m.selectedLeg)
	}
}

func TestJourneyTimelineHasNoPreLegStationsAndUsesStableFactColumns(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	m.route.Schedule = gtfs.JourneySchedule{
		Status:        gtfs.ScheduleAvailable,
		Duration:      25 * time.Minute,
		NextDeparture: time.Date(2026, 8, 13, 8, 0, 0, 0, gtfs.DelhiLocation),
		NextArrival:   time.Date(2026, 8, 13, 8, 25, 0, 0, gtfs.DelhiLocation),
		Stops: []gtfs.ScheduleStopDetail{
			{StationID: "dwarka_21", Arrival: time.Date(2026, 8, 13, 8, 0, 0, 0, gtfs.DelhiLocation)},
			{StationID: "rajiv_chowk", Arrival: time.Date(2026, 8, 13, 8, 20, 0, 0, gtfs.DelhiLocation)},
			{StationID: "new_delhi", Arrival: time.Date(2026, 8, 13, 8, 25, 0, 0, gtfs.DelhiLocation)},
		},
		Legs: []gtfs.ScheduleLegDetail{
			{FamilyID: "blue", From: "dwarka_21", To: "rajiv_chowk", Stops: 1, Departure: time.Date(2026, 8, 13, 8, 0, 0, 0, gtfs.DelhiLocation), Arrival: time.Date(2026, 8, 13, 8, 20, 0, 0, gtfs.DelhiLocation)},
			{FamilyID: "yellow", From: "rajiv_chowk", To: "new_delhi", Stops: 1, Departure: time.Date(2026, 8, 13, 8, 20, 0, 0, gtfs.DelhiLocation), Arrival: time.Date(2026, 8, 13, 8, 25, 0, 0, gtfs.DelhiLocation)},
		},
	}
	m.selectedLeg = 0
	plain := stripANSI(strings.Join(m.sidebarLines(40, 40), "\n"))
	if strings.Contains(plain, "STATIONS") || strings.Contains(plain, "Dwarka Sector 21 → Rajiv Chowk → New Delhi") {
		t.Fatalf("pre-leg station sequence survived: %q", plain)
	}
	for _, want := range []string{"Blue Line", "Dwarka Sector 21 → Rajiv Chowk", "1 stop · 20m · 08:00–08:20", "Yellow Line", "Rajiv Chowk → New Delhi", "1 stop · 5m · 08:20–08:25"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("compact leg omitted %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "…") {
		t.Fatalf("primary compact facts were ellipsized: %q", plain)
	}

	m.expandedLeg = 0
	expanded := strings.Split(strings.Join(m.expandedLegLines(0, m.route.Legs[0], 40), "\n"), "\n")
	expandedPlain := stripANSI(strings.Join(expanded, "\n"))
	for _, want := range []string{"FROM Dwarka Sector 21", "DEPART 08:00", "TO Rajiv Chowk", "ARRIVE 08:20", "+0m", "+20m"} {
		if !strings.Contains(expandedPlain, want) {
			t.Fatalf("expanded timeline omitted %q: %q", want, expandedPlain)
		}
	}
	if strings.Contains(expandedPlain, "08:00:00") || !strings.Contains(expandedPlain, "▌") {
		t.Fatalf("expanded timeline lost rail or concise times: %q", expandedPlain)
	}
	for _, row := range expanded {
		if strings.Contains(stripANSI(row), "+20m") && lipgloss.Width(stripANSI(row)) != 40 {
			t.Fatalf("elapsed offset is not right aligned to width 40: %q", row)
		}
	}
}

func TestJourneyTimelineIsBoundedAtWideBoundaryAndCompactSizes(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	m.route.Schedule = gtfs.JourneySchedule{Status: gtfs.ScheduleAvailable, Duration: 25 * time.Minute, NextDeparture: time.Date(2026, 8, 13, 8, 0, 0, 0, gtfs.DelhiLocation), NextArrival: time.Date(2026, 8, 13, 8, 25, 0, 0, gtfs.DelhiLocation)}
	for _, size := range [][2]int{{207, 50}, {100, 30}, {52, 30}, {40, 12}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		resized := modelValue(updated)
		assertBoundedView(t, resized, size[0], size[1])
		if size[0] >= 52 && resized.sidebarWidth() > 0 {
			plain := stripANSI(strings.Join(resized.sidebarLines(size[1]-2, resized.sidebarWidth()), "\n"))
			if !strings.Contains(plain, "JOURNEY") || !strings.Contains(plain, "SCHEDULED") {
				t.Fatalf("size %dx%d omitted journey labels: %q", size[0], size[1], plain)
			}
		}
	}
}

func TestJourneySidebarUsesSingleRowBreathingRhythmWithoutMovingFacts(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	m.route.Schedule = gtfs.JourneySchedule{Status: gtfs.ScheduleAvailable, Duration: 25 * time.Minute}
	lines := m.sidebarLines(50, 40)
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = strings.TrimSpace(stripANSI(line))
	}
	indexOf := func(value string) int {
		for i, line := range plain {
			if strings.Contains(line, value) {
				return i
			}
		}
		return -1
	}
	header, from, to, journey, summary, leg := indexOf("METROSHELL"), indexOf("FROM:"), indexOf("TO:"), indexOf("JOURNEY"), indexOf("2 stops"), indexOf("Blue Line")
	for label, index := range map[string]int{"header": header, "from": from, "to": to, "journey": journey, "summary": summary, "leg": leg} {
		if index < 0 {
			t.Fatalf("sidebar omitted %s: %q", label, strings.Join(plain, "\n"))
		}
	}
	if from <= header || to <= from || journey <= to || summary <= journey || leg <= summary {
		t.Fatalf("sidebar section order changed: header=%d from=%d to=%d journey=%d summary=%d leg=%d", header, from, to, journey, summary, leg)
	}
	for _, pair := range [][2]int{{header, from}, {from, to}, {to, journey}, {summary, leg}} {
		if pair[1]-pair[0] < 2 {
			t.Fatalf("sidebar lost one-row breathing gap between lines %d and %d: %q", pair[0], pair[1], strings.Join(plain, "\n"))
		}
	}
	if strings.Contains(strings.Join(plain, "\n"), "STATIONS") {
		t.Fatal("sidebar reintroduced removed STATIONS block")
	}
}

func TestNativeSidebarEvidenceKeepsNeutralShellsPinkBrandAndBreathingRoom(t *testing.T) {
	m := readyTestModel(t)
	m.fromStation, m.toStation = "dwarka_21", "new_delhi"
	m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
	for _, size := range [][2]int{{100, 30}, {207, 50}} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		resized := modelValue(updated)
		view := resized.View().Content
		plain := stripANSI(view)
		if !strings.Contains(view, "\x1b[38;5;245m╭") || !strings.Contains(view, "\x1b[38;5;239m╭") {
			t.Fatalf("size %dx%d lost neutral map/sidebar outer shells", size[0], size[1])
		}
		if !strings.Contains(view, "38;5;201m") || strings.Count(plain, "METROSHELL") != 1 {
			t.Fatalf("size %dx%d sidebar branding is not one prominent pink title: %q", size[0], size[1], view)
		}
		if strings.Count(plain, "13 Aug 2026") != 1 || strings.Contains(plain, "DELHI ") || strings.Contains(plain, "click map") || strings.Contains(plain, "Cursor:") || strings.Contains(plain, "Route ready") || strings.Contains(plain, "SIM:") || strings.Contains(plain, "PLAYBACK") || strings.Contains(plain, "STATIONS") {
			t.Fatalf("size %dx%d sidebar retained removed copy: %q", size[0], size[1], plain)
		}
		rows := strings.Split(strings.Join(resized.sidebarLines(size[1]-2, resized.sidebarWidth()), "\n"), "\n")
		indexes := map[string]int{}
		for _, label := range []string{"METROSHELL", "13 Aug ", "FROM:", "TO:", "JOURNEY", "2 stops", "Blue Line"} {
			indexes[label] = -1
			for row, line := range rows {
				if strings.Contains(stripANSI(line), label) {
					indexes[label] = row
					break
				}
			}
		}
		for label, row := range indexes {
			if row < 0 && label != "2 stops" && label != "Blue Line" {
				t.Fatalf("size %dx%d sidebar omitted %q: %q", size[0], size[1], label, strings.Join(rows, "\n"))
			}
		}
		if indexes["13 Aug "] != indexes["METROSHELL"]+1 || indexes["TO:"]-indexes["FROM:"] < 4 || indexes["JOURNEY"]-indexes["TO:"] < 2 {
			t.Fatalf("size %dx%d sidebar rhythm collapsed: indexes=%v", size[0], size[1], indexes)
		}
		t.Logf("native sidebar evidence %dx%d: neutral shells, pink title, single clock, journey facts, and one-row breathing gaps verified", size[0], size[1])
	}
}

func TestSidebarPolishNativeEvidenceCentersHeadingsAndKeepsCompactRows(t *testing.T) {
	wall := time.Date(2026, 8, 13, 9, 7, 12, 0, gtfs.DelhiLocation)
	for _, size := range [][2]int{{100, 30}, {207, 50}, {52, 16}} {
		m := NewWithConfig(nil, 28.6, 77.2, Config{GTFSPath: filepath.Join("..", "gtfs", "testdata", "delhi-mini"), Now: func() time.Time { return wall }})
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		m = modelValue(updated)
		updated, _ = m.Update(m.Init()())
		m = modelValue(updated)
		updated, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
		m = modelValue(updated)
		m.fromStation, m.toStation = "dwarka_21", "new_delhi"
		m.route = gtfs.PlanRoute(m.feedIndexes.Graph, m.fromStation, m.toStation)
		updated, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = modelValue(updated)
		plainRows := strings.Split(stripANSI(strings.Join(m.sidebarLines(size[1]-2, m.sidebarWidth()), "\n")), "\n")
		plain := strings.Join(plainRows, "\n")
		if strings.Contains(plain, "Tab · Enter/search") || strings.Contains(plain, "j/k leg · Enter expand · ? help") || strings.Contains(plain, "DELHI ") || strings.Contains(plain, "EXPANDED") || strings.Contains(plain, "STATIONS") || strings.Contains(plain, "SIM:") || strings.Contains(plain, "PLAYBACK") {
			t.Fatalf("size %dx%d retained removed sidebar copy: %q", size[0], size[1], plain)
		}
		if strings.Count(plain, "13 Aug 2026 09:07:12") != 1 {
			t.Fatalf("size %dx%d clock count=%d, want one seconds clock: %q", size[0], size[1], strings.Count(plain, "13 Aug 2026 09:07:12"), plain)
		}
		find := func(text string) (string, int) {
			for row, line := range plainRows {
				if strings.Contains(line, text) {
					return line, row
				}
			}
			return "", -1
		}
		journey, journeyRow := find("JOURNEY")
		scheduled, scheduledRow := find("TIMING UNAVAILABLE")
		if journeyRow < 0 || scheduledRow < 0 {
			t.Fatalf("size %dx%d omitted centered headings: journey=%d scheduled=%d plain=%q", size[0], size[1], journeyRow, scheduledRow, plain)
		}
		if strings.Index(journey, "JOURNEY") != (m.sidebarWidth()-lipgloss.Width("JOURNEY"))/2 {
			t.Fatalf("size %dx%d JOURNEY not centered: %q", size[0], size[1], journey)
		}
		scheduledText := "TIMING UNAVAILABLE"
		if strings.Index(scheduled, scheduledText) != (m.sidebarWidth()-lipgloss.Width(scheduledText))/2 {
			t.Fatalf("size %dx%d SCHEDULED not centered: %q", size[0], size[1], scheduled)
		}
		assertBoundedView(t, m, size[0], size[1])
		t.Logf("native sidebar polish evidence %dx%d: one HH:MM:SS clock, no hint/expanded labels, centered JOURNEY/SCHEDULED, compact rows bounded", size[0], size[1])
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
