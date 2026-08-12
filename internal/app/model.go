// Package app contains the shared Metroshell Bubble Tea application model.
package app

import (
	"fmt"
	"math"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/render"
)

// Model holds the state for one interactive map session.
type Model struct {
	cache    *render.TileCache
	lat      float64
	lon      float64
	zoom     float64
	width    int
	height   int
	showHelp bool
	frame    string
	status   string
}

type frameReadyMsg string

// New creates a map model centered at lat and lon. The cache can be shared by
// multiple models, such as concurrent SSH sessions.
func New(cache *render.TileCache, lat, lon float64) Model {
	return Model{
		cache:  cache,
		lat:    lat,
		lon:    lon,
		zoom:   12,
		status: "Waiting for terminal size...",
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.status = "Rendering..."
		return m, m.renderCmd()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "up", "k", "w":
			m.lat += geo.PanAmount(m.zoom)
		case "down", "j", "s":
			m.lat -= geo.PanAmount(m.zoom)
		case "left", "h", "a":
			m.lon -= geo.PanAmount(m.zoom)
		case "right", "l", "d":
			m.lon += geo.PanAmount(m.zoom)
		case "+", "=":
			if m.zoom >= 15.9 {
				return m, nil
			}
			m.zoom += 0.2
		case "-", "_":
			if m.zoom <= 5.1 {
				return m, nil
			}
			m.zoom -= 0.2
		default:
			return m, nil
		}
		m.setStatus()
		return m, m.renderCmd()

	case tea.MouseMsg:
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			if m.zoom >= 15.9 {
				return m, nil
			}
			m.zoom += 0.1
		case tea.MouseWheelDown:
			if m.zoom <= 5.1 {
				return m, nil
			}
			m.zoom -= 0.1
		default:
			return m, nil
		}
		m.setStatus()
		return m, m.renderCmd()

	case frameReadyMsg:
		m.frame = string(msg)
		return m, nil
	}
	return m, nil
}

func (m Model) View() tea.View {
	bdr := lipgloss.NewStyle().Foreground(lipgloss.Color("201"))
	innerW := max(m.width-2, 0)
	top := bdr.Render("╭" + strings.Repeat("─", innerW) + "╮")

	rawContent := m.frame
	if m.showHelp {
		rawContent = m.helpContent()
	} else if rawContent == "" {
		rawContent = strings.Repeat("\n", max(m.height-2, 1))
	}

	lines := strings.Split(strings.TrimRight(rawContent, "\n"), "\n")
	var framed strings.Builder
	for _, line := range lines {
		framed.WriteString(bdr.Render("│"))
		framed.WriteString(line)
		framed.WriteString(bdr.Render("│"))
		framed.WriteString("\n")
	}

	hudText := m.hudText()
	available := max(m.width-6, 0)
	hudRunes := []rune(hudText)
	if len(hudRunes) > available {
		hudText = string(hudRunes[:available])
	}
	padLen := max(available-len([]rune(hudText)), 0)

	hudStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	if m.status == "Rendering..." {
		hudStyle = hudStyle.Foreground(lipgloss.Color("240"))
	}
	bottom := bdr.Render("╰─ ") + hudStyle.Render(hudText) + bdr.Render(" "+strings.Repeat("─", padLen)+"─╯")

	view := tea.NewView(top + "\n" + framed.String() + bottom)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m *Model) setStatus() {
	m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
}

func (m Model) helpContent() string {
	w := max(m.width-2, 0)
	h := max(m.height-2, 0)
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("109"))
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("222"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpLines := []string{
		"",
		accent.Render("  Metroshell") + dim.Render("  ─  keybindings"),
		"",
		accent.Render("  Navigation"),
		"    " + key.Render("↑ k w") + dim.Render("  pan north    ") + key.Render("↓ j s") + dim.Render("  pan south"),
		"    " + key.Render("← h a") + dim.Render("  pan west     ") + key.Render("→ l d") + dim.Render("  pan east"),
		"",
		accent.Render("  Zoom"),
		"    " + key.Render("+ =") + dim.Render("         zoom in     ") + key.Render("- _") + dim.Render("       zoom out"),
		"    " + key.Render("scroll ↑") + dim.Render("     zoom in     ") + key.Render("scroll ↓") + dim.Render("   zoom out"),
		"",
		accent.Render("  Map symbols"),
		"    " + key.Render("M") + dim.Render("  metro station    ") + key.Render("T") + dim.Render("  rail/train station"),
		"    " + key.Render("+") + dim.Render("  hospital         ") + key.Render("🍴, 🍲, 🍜, 🥐") + dim.Render("  food places"),
		"    " + key.Render("g") + dim.Render("  fuel station"),
		"",
		accent.Render("  Other"),
		"    " + key.Render("?") + dim.Render("   toggle this help screen"),
		"    " + key.Render("q") + dim.Render("   quit"),
		"",
		dim.Render("  Tip: set terminal background to #000000 for AMOLED look"),
	}
	var sb strings.Builder
	for i := 0; i < h; i++ {
		line := ""
		if i < len(helpLines) {
			line = helpLines[i]
		}
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", max(w-lipgloss.Width(line), 0)))
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) hudText() string {
	zoom := fmt.Sprintf("z:%.1f", m.zoom)
	coords := fmt.Sprintf("%.4f°N  %.4f°E", m.lat, m.lon)
	scale := zoomToScale(int(math.Floor(m.zoom)))
	return strings.Join([]string{zoom, "N↑", coords, scale, "? help"}, " │ ")
}

func (m Model) renderCmd() tea.Cmd {
	cache := m.cache
	lat, lon, zoom := m.lat, m.lon, m.zoom
	pixelW := (m.width - 2) * 2
	pixelH := (m.height - 2) * 4
	return func() tea.Msg {
		frame := render.Render(render.RenderRequest{
			DB:     cache,
			Lat:    lat,
			Lon:    lon,
			Zoom:   zoom,
			PixelW: pixelW,
			PixelH: pixelH,
		})
		return frameReadyMsg(frame)
	}
}

func zoomToScale(zoom int) string {
	scales := map[int]string{
		5: "~500km", 6: "~250km", 7: "~125km",
		8: "~60km", 9: "~30km", 10: "~15km",
		11: "~7km", 12: "~3.5km", 13: "~1.8km",
		14: "~900m", 15: "~450m", 16: "~225m",
		17: "~110m",
	}
	return scales[zoom]
}
