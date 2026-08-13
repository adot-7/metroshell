package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/adot-7/metroshell/internal/app"
	"github.com/adot-7/metroshell/internal/gtfs"
	"github.com/adot-7/metroshell/internal/render"
)

// TestLocalAndSSHHandlerReplayDelhiFlow is a transport-boundary integration
// test. It deliberately calls makeHandler directly instead of starting a
// listener: Wish owns only the PTY/session wiring, while both transports must
// replay the same app model and logical terminal events.
func TestLocalAndSSHHandlerReplayDelhiFlow(t *testing.T) {
	const (
		lat = 28.6139
		lon = 77.2090
	)
	feedPath := "../../internal/gtfs/testdata/delhi-mini"
	config := app.Config{GTFSPath: feedPath}

	var local tea.Model = app.NewWithConfig(nil, lat, lon, config)
	sshModel, _ := makeHandler(nil, lat, lon, config)(nil)
	if _, ok := sshModel.(app.Model); !ok {
		t.Fatalf("SSH handler returned %T, want app.Model", sshModel)
	}

	local = replayInit(t, local)
	sshModel = replayInit(t, sshModel)
	assertParity(t, "feed load", local, sshModel)
	local = replayEvent(t, local, tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	sshModel = replayEvent(t, sshModel, tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}))
	assertParity(t, "splash dismissal", local, sshModel)

	events := []tea.Msg{
		tea.WindowSizeMsg{Width: 100, Height: 30},
		tea.KeyPressMsg(tea.Key{Text: "tab", Code: tea.KeyTab}),
		tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}),
		tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}),
		tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown}),
		tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}),
	}
	for i, event := range events {
		local = replayEvent(t, local, event)
		sshModel = replayEvent(t, sshModel, event)
		assertParity(t, "event "+string(rune('0'+i)), local, sshModel)
	}

	localModel := local.(app.Model)
	sshAppModel := sshModel.(app.Model)
	indexes, localReady := localModel.FeedIndexes()
	sshIndexes, sshReady := sshAppModel.FeedIndexes()
	if !localReady || !sshReady {
		t.Fatalf("fixture readiness local=%v SSH=%v, want both ready", localReady, sshReady)
	}
	from := indexes.OrderedStations[0].ID
	to := indexes.OrderedStations[1].ID
	localRoute := gtfs.PlanRoute(indexes.Graph, from, to)
	sshRoute := gtfs.PlanRoute(sshIndexes.Graph, from, to)
	if !reflect.DeepEqual(localRoute, sshRoute) {
		t.Fatalf("route result differs:\nlocal=%#v\nSSH=%#v", localRoute, sshRoute)
	}
	if localRoute.Status != gtfs.RouteReady {
		t.Fatalf("fixture route %s -> %s status = %v, want ready", from, to, localRoute.Status)
	}
	localGeometry := render.RouteGeometry(indexes, localRoute)
	sshGeometry := render.RouteGeometry(sshIndexes, sshRoute)
	if !reflect.DeepEqual(localGeometry, sshGeometry) {
		t.Fatalf("route geometry differs:\nlocal=%#v\nSSH=%#v", localGeometry, sshGeometry)
	}
	if len(localGeometry) < 2 {
		t.Fatalf("fixture route geometry has %d points, want at least two", len(localGeometry))
	}
	view := localModel.View().Content
	if localModel.View().MouseMode != tea.MouseModeCellMotion || sshModel.(app.Model).View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("local/SSH parity did not retain wheel-capable cell-motion mode")
	}
	routeSummary := fmt.Sprintf("%d stops · %d transfers", localRoute.Stops, localRoute.Transfers)
	if strings.Contains(view, "Route ready") || !strings.Contains(view, routeSummary) {
		t.Fatalf("replayed route output omitted route evidence: %q", view)
	}
	if strings.Contains(view, "◎") || strings.Contains(view, "Cursor:") || strings.Contains(view, "click map") || strings.Contains(view, "SIM:") || strings.Contains(view, "PLAYBACK") || strings.Contains(view, "STATIONS") {
		t.Fatalf("replayed route output retained removed cursor/mouse/demo copy: %q", view)
	}
	t.Logf("Delhi-mini parity route %s -> %s: %d stops, %d transfers, %d geometry points; local and Wish handler views/config/snapshots match", from, to, localRoute.Stops, localRoute.Transfers, len(localGeometry))

	// Exercise the shared focus/resize policy after the route is selected.
	for _, size := range []tea.WindowSizeMsg{{Width: 40, Height: 12}, {Width: 19, Height: 8}, {Width: 100, Height: 30}} {
		local = replayEvent(t, local, size)
		sshModel = replayEvent(t, sshModel, size)
		assertParity(t, "resize", local, sshModel)
		assertBounded(t, local.(app.Model), size.Width, size.Height)
		assertBounded(t, sshModel.(app.Model), size.Width, size.Height)
	}
	local = replayEvent(t, local, tea.BlurMsg{})
	sshModel = replayEvent(t, sshModel, tea.BlurMsg{})
	assertParity(t, "blur", local, sshModel)
	local = replayEvent(t, local, tea.FocusMsg{})
	sshModel = replayEvent(t, sshModel, tea.FocusMsg{})
	assertParity(t, "focus", local, sshModel)

	for _, overlayKey := range []string{"?", "j", "esc"} {
		event := tea.KeyPressMsg(tea.Key{Text: overlayKey, Code: rune(overlayKey[0])})
		local = replayEvent(t, local, event)
		sshModel = replayEvent(t, sshModel, event)
		assertParity(t, "help key "+overlayKey, local, sshModel)
	}
	assertBounded(t, local.(app.Model), 100, 30)

	// The same small terminal must keep the help and picker shells in bounds,
	// while their input remains trapped before returning to the map.
	for _, event := range []tea.Msg{
		tea.WindowSizeMsg{Width: 19, Height: 8},
		tea.KeyPressMsg(tea.Key{Text: "?", Code: '?'}),
		tea.KeyPressMsg(tea.Key{Text: "j", Code: 'j'}),
	} {
		local = replayEvent(t, local, event)
		sshModel = replayEvent(t, sshModel, event)
		assertParity(t, "narrow help", local, sshModel)
		assertBounded(t, local.(app.Model), 19, 8)
	}
	for _, event := range []tea.Msg{
		tea.KeyPressMsg(tea.Key{Text: "esc", Code: tea.KeyEscape}),
		tea.KeyPressMsg(tea.Key{Text: "tab", Code: tea.KeyTab}),
		tea.KeyPressMsg(tea.Key{Text: "tab", Code: tea.KeyTab}),
		tea.KeyPressMsg(tea.Key{Text: "enter", Code: tea.KeyEnter}),
		tea.KeyPressMsg(tea.Key{Text: "w", Code: 'w'}),
	} {
		local = replayEvent(t, local, event)
		sshModel = replayEvent(t, sshModel, event)
		assertParity(t, "narrow picker", local, sshModel)
		assertBounded(t, local.(app.Model), 19, 8)
	}
}

func replayInit(t *testing.T, model tea.Model) tea.Model {
	t.Helper()
	return replayCommand(t, model, model.Init())
}

func replayEvent(t *testing.T, model tea.Model, event tea.Msg) tea.Model {
	t.Helper()
	updated, command := model.Update(event)
	return replayCommand(t, updated, command)
}

// replayCommand drains synchronous app commands while deliberately ignoring
// the recurring simulation timer. The timer is transport-independent and
// must not make this deterministic test wait on wall-clock time.
func replayCommand(t *testing.T, model tea.Model, command tea.Cmd) tea.Model {
	t.Helper()
	if command == nil {
		return model
	}
	message := commandWithTimeout(command)
	if message == nil {
		return model
	}
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, child := range batch {
			model = replayCommand(t, model, child)
		}
		return model
	}
	updated, next := model.Update(message)
	return replayCommand(t, updated, next)
}

func commandWithTimeout(command tea.Cmd) tea.Msg {
	result := make(chan tea.Msg, 1)
	go func() { result <- command() }()
	select {
	case message := <-result:
		return message
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func assertParity(t *testing.T, step string, local, ssh tea.Model) {
	t.Helper()
	lm, lok := local.(app.Model)
	sm, sok := ssh.(app.Model)
	if !lok || !sok {
		t.Fatalf("%s model types local=%T SSH=%T", step, local, ssh)
	}
	if lm.FeedState() != sm.FeedState() || lm.DataStatus() != sm.DataStatus() {
		t.Fatalf("%s feed state/status differ: local=%v/%q SSH=%v/%q", step, lm.FeedState(), lm.DataStatus(), sm.FeedState(), sm.DataStatus())
	}
	if !reflect.DeepEqual(lm.SimulationConfig(), sm.SimulationConfig()) {
		t.Fatalf("%s simulation config differs:\nlocal=%#v\nSSH=%#v", step, lm.SimulationConfig(), sm.SimulationConfig())
	}
	if !reflect.DeepEqual(lm.SimulationSnapshot(), sm.SimulationSnapshot()) {
		t.Fatalf("%s simulation snapshot differs", step)
	}
	if lm.View().Content != sm.View().Content {
		t.Fatalf("%s rendered app state differs", step)
	}
}

func assertBounded(t *testing.T, model app.Model, width, height int) {
	t.Helper()
	rows := strings.Split(strings.TrimSuffix(model.View().Content, "\n"), "\n")
	if len(rows) != height {
		t.Fatalf("bounded view rows=%d, want %d", len(rows), height)
	}
	for i, row := range rows {
		if got := lipgloss.Width(row); got > width {
			t.Fatalf("bounded view row %d width=%d, want <=%d", i, got, width)
		}
	}
}
