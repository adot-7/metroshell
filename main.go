package main

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/render"
	"github.com/adot-7/metroshell/internal/tiles"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/log"

	tea "github.com/charmbracelet/bubbletea"
)

// model holds all application state.
// Rule: only Update() modifies the model.
type model struct {
	db       *tiles.DB
	cache    *render.TileCache // MVT layer cache — shared with renderCmd goroutines
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
type statusMsg string

func initialModel(db *tiles.DB) model {
	var lon, lat float64
	lon, lat = db.ReadMetadata()
	if lon == 0 || lat == 0 {
		lon = 77.2090
		lat = 28.6139
	}
	return model{
		db:     db,
		cache:  render.NewTileCache(db),
		lat:    lat,
		lon:    lon,
		zoom:   12,
		status: "Waiting for terminal size...",
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
			return m, m.renderCmd()
		case "down", "j", "s":
			m.lat -= geo.PanAmount(m.zoom)
			m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
			return m, m.renderCmd()
		case "left", "h", "a":
			m.lon -= geo.PanAmount(m.zoom)
			m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
			return m, m.renderCmd()
		case "right", "l", "d":
			m.lon += geo.PanAmount(m.zoom)
			m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
			return m, m.renderCmd()
		case "+", "=":
			if m.zoom < 15.9 {
				m.zoom += 0.2
				m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
				return m, m.renderCmd()
			}
		case "-", "_":
			if m.zoom > 5.1 {
				m.zoom -= 0.2
				m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
				return m, m.renderCmd()
			}
		}

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.zoom < 15.9 {
				m.zoom += 0.1 // finer increments on scroll
				m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
				return m, m.renderCmd()
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			if m.zoom > 5.1 {
				m.zoom -= 0.1
				m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
				return m, m.renderCmd()
			}
			return m, nil
		}

	case frameReadyMsg:
		m.frame = string(msg)
		return m, nil

	case statusMsg:
		m.status = string(msg)
		return m, nil
	}

	return m, nil
}

func (m model) View() string {
	bdr := lipgloss.NewStyle().Foreground(lipgloss.Color("201"))
	innerW := max(m.width-2, 0)
	top := bdr.Render("╭" + strings.Repeat("─", innerW) + "╮")

	var rawContent string
	if m.showHelp {
		rawContent = m.helpContent()
	} else {
		rawContent = m.frame
		if rawContent == "" {
			rawContent = strings.Repeat("\n", max(m.height-2, 1))
		}
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
	available := m.width - 6
	if available < 0 {
		available = 0
	}
	hudRunes := []rune(hudText)
	if len(hudRunes) > available {
		hudRunes = hudRunes[:available]
		hudText = string(hudRunes)
	}
	padLen := available - len([]rune(hudText))
	if padLen < 0 {
		padLen = 0
	}

	var hudStyled string
	if m.status == "Rendering..." {
		hudStyled = mutedStyle.Foreground(lipgloss.Color("240")).Render(hudText)
	} else {
		hudStyled = mutedStyle.Render(hudText)
	}

	bottom := bdr.Render("╰─ ") + hudStyled + bdr.Render(" "+strings.Repeat("─", padLen)+"─╯")
	return top + "\n" + framed.String() + bottom
}

func (m model) helpContent() string {
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
		var line string
		if i < len(helpLines) {
			line = helpLines[i]
		}
		pad := w - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", pad))
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m model) hudText() string {
	zoom := fmt.Sprintf("z:%.1f", m.zoom)
	coords := fmt.Sprintf("%.4f°N  %.4f°E", m.lat, m.lon)
	scale := zoomToScale(int(math.Floor(m.zoom)))
	parts := []string{zoom, "N↑", coords, scale, "? help"}
	return strings.Join(parts, " │ ")
}

func (m model) renderCmd() tea.Cmd {
	cache := m.cache
	lat, lon := m.lat, m.lon
	zoom := m.zoom
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

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: metroshell <path-to.mbtiles>")
	}
	db, err := tiles.Open(os.Args[1])
	// db.ReadMetadata()
	if err != nil {
		log.Fatalf("Failed to open MBTiles: %v", err)
	}
	defer db.Close()

	f, err := os.OpenFile("trip.log", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	log.SetOutput(f)
	log.SetLevel(log.DebugLevel) // suppress noisy debug logs
	log.SetReportCaller(true)

	p := tea.NewProgram(
		initialModel(db),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

var mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

func zoomToScale(zoom int) string {
	scales := map[int]string{
		5: "~500km", 6: "~250km", 7: "~125km",
		8: "~60km", 9: "~30km", 10: "~15km",
		11: "~7km", 12: "~3.5km", 13: "~1.8km",
		14: "~900m", 15: "~450m", 16: "~225m",
		17: "~110m",
	}
	if s, ok := scales[zoom]; ok {
		return s
	}
	return ""
}
