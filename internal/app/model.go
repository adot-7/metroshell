// Package app contains the shared Metroshell Bubble Tea application model.
package app

import (
	"fmt"
	"math"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/gtfs"
	"github.com/adot-7/metroshell/internal/render"
)

// Config controls optional data sources for a map session. An empty GTFSPath
// keeps the app in map-only mode without starting a feed-loading command.
type Config struct {
	GTFSPath string
	// FeedPath is accepted as a descriptive alias for GTFSPath.
	FeedPath string
}

// FeedState describes the lifecycle of the configured GTFS snapshot.
type FeedState uint8

const (
	FeedStateMissing FeedState = iota
	FeedStateLoading
	FeedStateError
	FeedStateReady
)

// Aliases keep the state names convenient for callers that prefer the GTFS
// prefix while retaining the explicit FeedState names above.
const (
	GTFSStateMissing = FeedStateMissing
	GTFSStateLoading = FeedStateLoading
	GTFSStateError   = FeedStateError
	GTFSStateReady   = FeedStateReady
)

func (s FeedState) String() string {
	switch s {
	case FeedStateLoading:
		return "loading"
	case FeedStateError:
		return "error"
	case FeedStateReady:
		return "ready"
	default:
		return "missing"
	}
}

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

	gtfsPath    string
	feedState   FeedState
	feedError   error
	feed        gtfs.Feed
	feedIndexes gtfs.Indexes
}

type frameReadyMsg string

// New creates a map model centered at lat and lon. The cache can be shared by
// multiple models, such as concurrent SSH sessions. An optional Config keeps
// the original map-only call form source-compatible.
func New(cache *render.TileCache, lat, lon float64, configs ...Config) Model {
	config := Config{}
	if len(configs) > 0 {
		config = configs[0]
	}
	return NewWithConfig(cache, lat, lon, config)
}

// NewWithConfig creates a map model with optional GTFS feed loading. The feed
// is not touched here; a Bubble Tea command started by Init performs all file
// IO and parsing away from Update and View.
func NewWithConfig(cache *render.TileCache, lat, lon float64, config Config) Model {
	gtfsPath := strings.TrimSpace(config.GTFSPath)
	if gtfsPath == "" {
		gtfsPath = strings.TrimSpace(config.FeedPath)
	}
	feedState := FeedStateMissing
	if gtfsPath != "" {
		feedState = FeedStateLoading
	}
	return Model{
		cache:     cache,
		lat:       lat,
		lon:       lon,
		zoom:      12,
		status:    "Waiting for terminal size...",
		gtfsPath:  gtfsPath,
		feedState: feedState,
	}
}

func (m Model) Init() tea.Cmd {
	if m.gtfsPath == "" {
		return nil
	}
	return m.loadFeedCmd()
}

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

	case feedReadyMsg:
		m.feed = msg.feed
		m.feedIndexes = msg.indexes
		m.feedError = nil
		m.feedState = FeedStateReady
		return m, nil

	case feedMissingMsg:
		m.feed = gtfs.Feed{}
		m.feedIndexes = gtfs.Indexes{}
		m.feedError = nil
		m.feedState = FeedStateMissing
		return m, nil

	case feedErrorMsg:
		m.feed = gtfs.Feed{}
		m.feedIndexes = gtfs.Indexes{}
		m.feedError = msg.err
		m.feedState = FeedStateError
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
	return strings.Join([]string{zoom, "N↑", coords, scale, m.dataStatus(), "? help"}, " │ ")
}

// FeedState reports the current GTFS loading state without exposing mutable
// model internals.
func (m Model) FeedState() FeedState { return m.feedState }

// GTFSPath reports the configured feed path, if any.
func (m Model) GTFSPath() string { return m.gtfsPath }

// Feed returns the accepted normalized feed after a successful load.
func (m Model) Feed() (gtfs.Feed, bool) {
	return m.feed, m.feedState == FeedStateReady
}

// FeedIndexes returns the deterministic indexes built for the accepted feed.
func (m Model) FeedIndexes() (gtfs.Indexes, bool) {
	return m.feedIndexes, m.feedState == FeedStateReady
}

// FeedError returns the parse or filesystem error for FeedStateError.
func (m Model) FeedError() error { return m.feedError }

// DataStatus is the concise status shown in the existing HUD.
func (m Model) DataStatus() string { return m.dataStatus() }

func (m Model) dataStatus() string {
	switch m.feedState {
	case FeedStateLoading:
		return "GTFS: loading"
	case FeedStateError:
		if m.feedError == nil {
			return "GTFS: error"
		}
		return "GTFS: error (" + compactError(m.feedError.Error()) + ")"
	case FeedStateReady:
		return fmt.Sprintf("GTFS: ready (%d stops, %d lines)", len(m.feedIndexes.Stations), len(m.feedIndexes.Lines))
	default:
		return "GTFS: missing"
	}
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
