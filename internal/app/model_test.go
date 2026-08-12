package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

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
