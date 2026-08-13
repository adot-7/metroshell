// Package app contains the shared Metroshell Bubble Tea application model.
package app

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/gtfs"
	"github.com/adot-7/metroshell/internal/render"
	"github.com/adot-7/metroshell/internal/sim"
	"github.com/paulmach/orb"
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
	cache         *render.TileCache
	lat           float64
	lon           float64
	cursor        orb.Point
	zoom          float64
	width         int
	height        int
	showHelp      bool
	picker        bool
	search        string
	pickerPos     int
	pickerTop     int
	frame         string
	renderSeq     uint64
	trainClock    int64
	trainSeed     uint64
	trainFleet    int
	focused       bool
	simRunning    bool
	simGeneration uint64
	routeSeq      uint64
	routeAutoFit  bool
	status        string

	gtfsPath    string
	feedState   FeedState
	feedError   error
	feed        gtfs.Feed
	feedIndexes gtfs.Indexes

	focus       endpointFocus
	stationPos  int
	fromStation string
	toStation   string
	route       gtfs.RouteResult
}

type endpointFocus uint8

const (
	focusMap endpointFocus = iota
	focusFrom
	focusTo
)

type frameReadyMsg struct {
	seq   uint64
	frame string
}

type trainTickMsg struct {
	generation uint64
}

type routeReadyMsg struct {
	seq    uint64
	result gtfs.RouteResult
}

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
		cache:        cache,
		lat:          lat,
		lon:          lon,
		cursor:       orb.Point{lon, lat},
		zoom:         12,
		status:       "Waiting for terminal size...",
		gtfsPath:     gtfsPath,
		feedState:    feedState,
		route:        gtfs.RouteResult{Status: gtfs.RouteNoEndpoints, Message: "Select FROM and TO stations"},
		routeAutoFit: true,
		trainSeed:    41,
		trainFleet:   24,
		focused:      true,
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
		m.clampCursor()
		if m.routeAutoFit {
			m.fitSelectedRoute()
		}
		m.status = "Rendering..."
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case trainTickMsg:
		if !m.simRunning || msg.generation != m.simGeneration || !m.simulationEligible() {
			return m, nil
		}
		// The simulator clock is in sim.ClockCycle units. Ten steps per cycle
		// make motion visible in about 2.5 seconds at the 250ms cadence while
		// preserving the existing pause, reduced-motion, focus, and resize gates.
		m.trainClock += simulationClockStep
		m.renderSeq++
		return m, tea.Batch(m.renderCmd(), trainTickCmd(msg.generation))

	case tea.FocusMsg:
		m.focused = true
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case tea.BlurMsg:
		m.focused = false
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case tea.KeyMsg:
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" || msg.String() == "ctrl+c" {
				if msg.String() == "ctrl+c" || msg.String() == "q" {
					return m, tea.Quit
				}
				m.showHelp = false
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.syncSimulation())
			}
			return m, nil
		}
		if m.picker {
			return m.updatePicker(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.syncSimulation())
		case "tab":
			m.focus = (m.focus + 1) % 3
			m.setStatus()
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.syncSimulation())
		case "shift+tab":
			if m.focus == focusMap {
				m.focus = focusTo
			} else {
				m.focus--
			}
			m.setStatus()
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.syncSimulation())
		case "enter":
			if m.focus != focusMap {
				m.picker = true
				m.search = ""
				m.pickerPos = 0
				m.pickerTop = 0
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.syncSimulation())
			}
			m.selectFocusedStation()
			m.routeSeq++
			m.setStatus()
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.routeCmd(), m.syncSimulation())
		case "esc", "backspace":
			m.clearFocusedEndpoint()
			m.routeSeq++
			m.setStatus()
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.routeCmd(), m.syncSimulation())
		case "I", "ctrl+up":
			m.moveCursor(0, -cursorStepY)
		case "K", "ctrl+down":
			m.moveCursor(0, cursorStepY)
		case "J", "ctrl+left":
			m.moveCursor(-cursorStepX, 0)
		case "L", "ctrl+right":
			m.moveCursor(cursorStepX, 0)
		case "w":
			m.routeAutoFit = false
			m.lat += geo.PanAmount(m.zoom)
			m.clampCursor()
		case "s":
			m.routeAutoFit = false
			m.lat -= geo.PanAmount(m.zoom)
			m.clampCursor()
		case "a":
			m.routeAutoFit = false
			m.lon -= geo.PanAmount(m.zoom)
			m.clampCursor()
		case "d":
			m.routeAutoFit = false
			m.lon += geo.PanAmount(m.zoom)
			m.clampCursor()
		case "up", "k":
			if m.focus != focusMap {
				m.moveStation(-1)
				m.setStatus()
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.syncSimulation())
			}
			m.routeAutoFit = false
			m.lat += geo.PanAmount(m.zoom)
			m.clampCursor()
		case "down", "j":
			if m.focus != focusMap {
				m.moveStation(1)
				m.setStatus()
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.syncSimulation())
			}
			m.routeAutoFit = false
			m.lat -= geo.PanAmount(m.zoom)
			m.clampCursor()
		case "left", "h":
			m.routeAutoFit = false
			m.lon -= geo.PanAmount(m.zoom)
			m.clampCursor()
		case "right", "l":
			m.routeAutoFit = false
			m.lon += geo.PanAmount(m.zoom)
			m.clampCursor()
		case "+", "=":
			m.routeAutoFit = false
			if m.zoom >= 15.9 {
				return m, nil
			}
			m.zoom += 0.2
			m.clampCursor()
		case "-", "_":
			m.routeAutoFit = false
			if m.zoom <= 5.1 {
				return m, nil
			}
			m.zoom -= 0.2
			m.clampCursor()
		default:
			return m, nil
		}
		m.setStatus()
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case tea.MouseMsg:
		if m.showHelp || m.picker {
			return m, nil
		}
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			m.routeAutoFit = false
			if m.zoom >= 15.9 {
				return m, nil
			}
			m.zoom += 0.1
			m.clampCursor()
		case tea.MouseWheelDown:
			m.routeAutoFit = false
			if m.zoom <= 5.1 {
				return m, nil
			}
			m.zoom -= 0.1
			m.clampCursor()
		default:
			return m, nil
		}
		m.setStatus()
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case frameReadyMsg:
		if msg.seq != m.renderSeq {
			return m, nil
		}
		m.frame = msg.frame
		return m, nil

	case feedReadyMsg:
		m.feed = msg.feed
		m.feedIndexes = msg.indexes
		m.feedError = nil
		m.feedState = FeedStateReady
		m.routeSeq++
		m.status = "Rendering..."
		m.invalidate()
		if m.cache == nil {
			if m.simulationEligible() {
				m.simGeneration++
				m.simRunning = true
			}
			return m, nil
		}
		return m, tea.Batch(m.renderCmd(), m.routeCmd(), m.syncSimulation())

	case feedMissingMsg:
		m.feed = gtfs.Feed{}
		m.feedIndexes = gtfs.Indexes{}
		m.feedError = nil
		m.feedState = FeedStateMissing
		m.routeSeq++
		m.route = gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: "No GTFS feed configured"}
		m.invalidate()
		if m.cache == nil {
			return m, nil
		}
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case feedErrorMsg:
		m.feed = gtfs.Feed{}
		m.feedIndexes = gtfs.Indexes{}
		m.feedError = msg.err
		m.feedState = FeedStateError
		m.routeSeq++
		m.route = gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: "GTFS feed unavailable"}
		m.invalidate()
		if m.cache == nil {
			return m, nil
		}
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case routeReadyMsg:
		if msg.seq != m.routeSeq {
			return m, nil
		}
		m.route = msg.result
		m.routeAutoFit = true
		m.fitSelectedRoute()
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())
	}
	return m, nil
}

const (
	cursorStepX = 2.0
	cursorStepY = 4.0

	// Simulation is paused below this size because a train HUD cannot be read
	// reliably and the map has no useful drawing area. Above it, cramped
	// terminals use a deterministic frozen snapshot instead of rapid motion.
	simPauseWidth   = 20
	simPauseHeight  = 8
	simReduceWidth  = 52
	simReduceHeight = 16

	// Picker geometry is content-independent at normal terminal sizes.
	pickerShellWidth  = 68
	pickerShellHeight = 18
	pickerResultRows  = 8
	helpShellWidth    = 72
	helpShellHeight   = 20
)

func (m *Model) invalidate() {
	m.renderSeq++
	// A state-changing event invalidates every tick command already queued by
	// Bubble Tea. The next eligible state starts a fresh generation.
	m.simGeneration++
	m.simRunning = false
}

func (m Model) simulationPaused() bool {
	return !m.focused || m.showHelp || m.picker || m.width < simPauseWidth || m.height < simPauseHeight
}

func (m Model) simulationEligible() bool {
	return m.feedState == FeedStateReady && m.trainFleet > 0 && !m.simulationPaused()
}

func (m Model) simulationReducedMotion() bool {
	return m.width < simReduceWidth || m.height < simReduceHeight
}

func (m *Model) syncSimulation() tea.Cmd {
	if !m.simulationEligible() {
		m.simRunning = false
		return nil
	}
	if m.simRunning {
		return nil
	}
	m.simGeneration++
	m.simRunning = true
	return trainTickCmd(m.simGeneration)
}

// SimulationConfig is the exact deterministic input used by local and SSH
// sessions. It is intentionally exported for diagnostics and parity tests.
func (m Model) SimulationConfig() sim.Config { return m.simulationConfig() }

// SimulationSnapshot returns the immutable train snapshot for the model's
// current state without advancing its clock or touching Bubble Tea state.
func (m Model) SimulationSnapshot() []sim.Train { return sim.Snapshot(m.simulationConfig()) }

func (m Model) simulationConfig() sim.Config {
	return sim.Config{
		Seed:          m.trainSeed,
		Clock:         m.trainClock,
		Fleet:         m.trainFleet,
		Paused:        m.simulationPaused(),
		ReducedMotion: m.simulationReducedMotion(),
		Routes:        simulationRoutes(m.feedIndexes),
	}
}

func (m *Model) viewport() geo.Viewport {
	return geo.Viewport{
		Lat: m.lat, Lon: m.lon, Zoom: m.zoom,
		PixelW: m.mapWidth() * 2,
		PixelH: max(m.height-2, 0) * 4,
	}
}

func (m *Model) clampCursor() {
	vp := m.viewport()
	if vp.PixelW == 0 || vp.PixelH == 0 {
		return
	}
	m.cursor = vp.ClampPoint(m.cursor)
}

func (m *Model) fitSelectedRoute() {
	if m.route.Status != gtfs.RouteReady || m.feedState != FeedStateReady {
		return
	}
	points := render.RouteGeometry(m.feedIndexes, m.route)
	bounds, ok := geo.NewBounds(points)
	if !ok {
		return
	}
	fallback := m.viewport()
	fit, ok := geo.FitBounds(bounds, fallback.PixelW, fallback.PixelH, routeFitPadding, fallback)
	if !ok {
		return
	}
	m.lat, m.lon, m.zoom = fit.Lat, fit.Lon, fit.Zoom
	m.clampCursor()
}

const routeFitPadding = 12

func (m *Model) moveCursor(dx, dy float64) {
	vp := m.viewport()
	if vp.PixelW == 0 || vp.PixelH == 0 {
		return
	}
	x, y := vp.Project(m.cursor)
	x += dx
	y += dy
	x = min(max(x, 0), float64(vp.PixelW-1))
	y = min(max(y, 0), float64(vp.PixelH-1))
	m.cursor = vp.Unproject(x, y)
}

func (m *Model) moveStation(delta int) {
	count := len(m.feedIndexes.OrderedStations)
	if count == 0 {
		m.stationPos = 0
		return
	}
	m.stationPos = min(max(m.stationPos+delta, 0), count-1)
}

func (m *Model) selectFocusedStation() {
	if m.feedState != FeedStateReady || len(m.feedIndexes.OrderedStations) == 0 {
		return
	}
	stationID := ""
	if m.focus == focusMap {
		stationID = render.NearestStation(m.feedIndexes, m.viewport(), m.cursor)
	} else if m.stationPos < len(m.feedIndexes.OrderedStations) {
		stationID = m.feedIndexes.OrderedStations[m.stationPos].ID
	}
	if stationID == "" {
		return
	}
	if m.focus == focusFrom || (m.focus == focusMap && m.fromStation == "") {
		m.fromStation = stationID
		if m.focus == focusFrom {
			m.focus = focusTo
		}
		return
	}
	m.toStation = stationID
}

func (m *Model) clearFocusedEndpoint() {
	switch m.focus {
	case focusFrom:
		m.fromStation = ""
	case focusTo:
		m.toStation = ""
	}
}

func (m Model) View() tea.View {
	bdr := lipgloss.NewStyle().Foreground(lipgloss.Color("201"))
	innerW := max(m.width-2, 0)
	top := bdr.Render("╭" + strings.Repeat("─", innerW) + "╮")

	rawContent := m.frame
	if rawContent == "" {
		rawContent = strings.Repeat("\n", max(m.height-2, 1))
	}

	lines := strings.Split(strings.TrimRight(rawContent, "\n"), "\n")
	panelW := m.sidebarWidth()
	mapW := innerW
	if panelW > 0 {
		mapW = max(innerW-panelW-1, 0)
	}
	mapH := max(m.height-2, 0)
	for len(lines) < mapH {
		lines = append(lines, "")
	}
	if len(lines) > mapH && mapH > 0 {
		lines = lines[:mapH]
	}
	panel := m.sidebarLines(max(m.height-2, 0), panelW)
	var framed strings.Builder
	for i, line := range lines {
		framed.WriteString(bdr.Render("│"))
		framed.WriteString(padDisplay(truncateDisplay(line, mapW), mapW))
		if panelW > 0 {
			framed.WriteString(bdr.Render("│"))
			right := ""
			if i < len(panel) {
				right = panel[i]
			}
			framed.WriteString(padDisplay(truncateDisplay(right, panelW), panelW))
		}
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

	viewContent := top + "\n" + framed.String() + bottom
	if m.showHelp {
		viewContent = m.helpOverlay(viewContent)
	} else if m.picker {
		shellW, shellH, _, _ := m.pickerGeometry()
		viewContent = m.overlayShellFixed(viewContent, m.pickerLines(), shellW, shellH)
	}
	view := tea.NewView(viewContent)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m *Model) setStatus() {
	m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
}

func (m Model) helpContent() string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("109"))
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("222"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpLines := []string{
		"",
		accent.Render("  Metroshell") + dim.Render("  ─  keybindings"),
		"",
		accent.Render("  Navigation"),
		"    " + key.Render("w a s d") + dim.Render(" pan map   ") + key.Render("←→ h l") + dim.Render(" west/east"),
		"    " + key.Render("↑↓ k j") + dim.Render(" stations in FROM/TO   ") + key.Render("I J K L") + dim.Render(" move map cursor"),
		"    " + key.Render("Tab") + dim.Render(" endpoint   ") + key.Render("Enter") + dim.Render(" picker   ") + key.Render("Esc") + dim.Render(" clear/cancel"),
		"",
		accent.Render("  Zoom"),
		"    " + key.Render("+ = / scroll ↑") + dim.Render(" zoom in   ") + key.Render("- _ / scroll ↓") + dim.Render(" zoom out"),
		"",
		accent.Render("  Map symbols"),
		"    " + key.Render("M") + dim.Render("  metro station    ") + key.Render("T") + dim.Render("  rail/train station"),
		"    " + key.Render("+") + dim.Render("  hospital         ") + key.Render("🍴, 🍲, 🍜, 🥐") + dim.Render("  food places"),
		"    " + key.Render("g") + dim.Render("  fuel station"),
		"",
		accent.Render("  Other"),
		"    " + key.Render("?") + dim.Render(" toggle help   ") + key.Render("q") + dim.Render(" quit"),
		"    " + dim.Render("Trains pause when unfocused, overlaid, or below 20×8; compact terminals reduce motion."),
		"",
		dim.Render("  Tip: set terminal background to #000000 for AMOLED look"),
	}
	return strings.Join(helpLines, "\n")
}

func (m Model) helpOverlay(background string) string {
	return m.overlayShell(background, strings.Split(strings.TrimRight(m.helpContent(), "\n"), "\n"), helpShellWidth, helpShellHeight)
}

func (m Model) overlayShell(background string, lines []string, maxW, maxH int) string {
	width, height := max(m.width, 1), max(m.height, 1)
	contentW := 0
	for _, line := range lines {
		contentW = max(contentW, lipgloss.Width(stripANSI(line)))
	}
	boxW := min(maxW, max(1, min(contentW+4, width)))
	boxH := min(maxH, max(1, min(len(lines)+2, height)))
	if width < 6 {
		boxW = width
	}
	if height < 6 {
		boxH = height
	}
	return m.overlayShellFixed(background, lines, boxW, boxH)
}

// overlayShellFixed composites a bounded shell over the existing application
// rows without painting a full-screen backdrop.
func (m Model) overlayShellFixed(background string, lines []string, boxW, boxH int) string {
	width, height := max(m.width, 1), max(m.height, 1)
	boxW = min(max(boxW, 1), width)
	boxH = min(max(boxH, 1), height)
	border := lipgloss.NewStyle().Foreground(lipgloss.Color("201"))
	innerW := max(boxW-4, 0)
	var box []string
	if boxW >= 2 {
		box = append(box, border.Render("╭"+strings.Repeat("─", boxW-2)+"╮"))
	} else {
		box = append(box, border.Render("│"))
	}
	for i := 0; i < boxH-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if boxW >= 4 {
			line = truncateDisplay(line, innerW)
			box = append(box, border.Render("│")+" "+padDisplay(line, innerW)+" "+border.Render("│"))
		} else if boxW >= 2 {
			box = append(box, border.Render("│")+padDisplay(truncateDisplay(line, boxW-2), boxW-2)+border.Render("│"))
		} else {
			box = append(box, border.Render("│"))
		}
	}
	if boxW >= 2 {
		box = append(box, border.Render("╰"+strings.Repeat("─", boxW-2)+"╯"))
	} else {
		box = append(box, border.Render("│"))
	}
	bg := strings.Split(strings.TrimRight(background, "\n"), "\n")
	left := max((width-boxW)/2, 0)
	top := max((height-boxH)/2, 0)
	for i := 0; i < height; i++ {
		if i >= len(bg) {
			bg = append(bg, "")
		}
		bg[i] = padDisplay(truncateDisplay(bg[i], width), width)
		if i >= top && i < top+boxH {
			line := box[i-top]
			right := width - left - boxW
			bg[i] = displaySlice(bg[i], 0, left) + line + displaySlice(bg[i], left+boxW, right)
		}
	}
	return strings.Join(bg[:height], "\n")
}

// displaySlice keeps ANSI styling while selecting a terminal-cell range. It
// is used only for the two background fragments beside a modal shell, so the
// underlying map/sidebar/HUD bytes and colors remain visible outside bounds.
func displaySlice(value string, start, width int) string {
	if width <= 0 {
		return ""
	}
	end := start + width
	column := 0
	active := make([]string, 0, 2)
	var out strings.Builder
	emittedStyle := false
	for i := 0; i < len(value); {
		if value[i] == '\x1b' && i+1 < len(value) && value[i+1] == '[' {
			j := i + 2
			for j < len(value) && !((value[j] >= 'a' && value[j] <= 'z') || (value[j] >= 'A' && value[j] <= 'Z')) {
				j++
			}
			if j < len(value) {
				raw := value[i : j+1]
				wasStyled := len(active) > 0
				reset := raw == "\x1b[0m" || raw == "\x1b[m"
				if strings.HasSuffix(raw, "m") {
					if reset {
						active = active[:0]
					} else {
						active = append(active, raw)
					}
				}
				if column >= start && column < end && (!reset || wasStyled) {
					out.WriteString(raw)
					emittedStyle = true
				}
				i = j + 1
				continue
			}
		}
		r, size := rune(value[i]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(value[i:])
		}
		cellWidth := lipgloss.Width(string(r))
		if cellWidth < 1 {
			cellWidth = 1
		}
		if column >= start && column < end {
			if column == start && !emittedStyle {
				for _, style := range active {
					out.WriteString(style)
				}
				emittedStyle = true
			}
			out.WriteRune(r)
		}
		column += cellWidth
		i += size
	}
	return out.String()
}

// pickerGeometry returns fixed picker dimensions, clamped independently to the
// terminal. The result window is the only region that changes at small sizes.
func (m Model) pickerGeometry() (shellW, shellH, controlW, visibleRows int) {
	width, height := max(m.width, 1), max(m.height, 1)
	shellW = min(pickerShellWidth, width)
	shellH = min(pickerShellHeight, height)
	controlW = max(shellW-6, 1)
	visibleRows = min(pickerResultRows, max(shellH-10, 1))
	return
}

func (m Model) filteredStations() []gtfs.Station {
	needle := strings.ToLower(strings.TrimSpace(m.search))
	result := make([]gtfs.Station, 0)
	for _, station := range m.feedIndexes.OrderedStations {
		if needle == "" || strings.Contains(strings.ToLower(station.Name), needle) || strings.Contains(strings.ToLower(station.ID), needle) {
			result = append(result, station)
		}
	}
	return result
}

func (m Model) pickerLines() []string {
	title := "Where are you at?"
	if m.focus == focusTo {
		title = "Where are you headed?"
	}
	stations := m.filteredStations()
	_, _, rowWidth, visible := m.pickerGeometry()
	lines := []string{"  " + title, "", pickerTopBorder(rowWidth), pickerInputLine(m.search, rowWidth), pickerBorder(rowWidth), pickerTopBorder(rowWidth)}
	top := 0
	if len(stations) > 0 {
		top = min(max(m.pickerTop, 0), len(stations)-1)
	}
	if m.pickerPos < top {
		top = m.pickerPos
	}
	end := min(top+visible, len(stations))
	if len(stations) > 0 && m.pickerPos >= end {
		top = m.pickerPos
		end = min(top+visible, len(stations))
	}
	for i := 0; i < visible; i++ {
		index := top + i
		if index >= end {
			lines = append(lines, pickerRow("", rowWidth))
			continue
		}
		lines = append(lines, m.pickerStationRow(stations[index], index == m.pickerPos, rowWidth))
	}
	count := "No matching stations"
	if len(stations) > 0 {
		count = fmt.Sprintf("%d–%d of %d", top+1, end, len(stations))
	}
	lines = append(lines, pickerBorder(rowWidth), fmt.Sprintf("  %s · ↑↓ navigate · Enter select · Esc cancel", count))
	return lines
}

func (m Model) pickerStationRow(station gtfs.Station, selected bool, width int) string {
	innerWidth := max(width-2, 1)
	marker := "  "
	if selected {
		marker = "▌ "
	}
	indicator := stationIndicatorText(m, station)
	indicatorWidth := lipgloss.Width(indicator)
	nameWidth := max(innerWidth-lipgloss.Width(marker)-indicatorWidth-1, 1)
	name := truncateDisplay(station.Name, nameWidth)
	body := marker + name + strings.Repeat(" ", max(innerWidth-lipgloss.Width(marker)-lipgloss.Width(name)-indicatorWidth, 1)) + indicator
	body = padDisplay(truncateDisplay(body, innerWidth), innerWidth)
	if selected {
		_, color := m.familyPresentation(primaryFamily(station))
		style := lipgloss.NewStyle().Background(lipgloss.Color(color)).Foreground(lipgloss.Color(selectionTextColor(color))).Bold(true)
		return neutralBorder("│") + " " + style.Render(body) + " " + neutralBorder("│")
	}
	return neutralBorder("│") + " " + body + " " + neutralBorder("│")
}

func stationIndicatorText(m Model, station gtfs.Station) string {
	ids := append([]string(nil), station.FamilyIDs...)
	if len(ids) == 0 {
		ids = append(ids, station.LineIDs...)
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		name, color := m.familyPresentation(id)
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("● "+lineCode(id, name)))
	}
	return strings.Join(parts, " ")
}

func lineCode(id, name string) string {
	value := strings.ToUpper(strings.TrimSpace(id))
	if value == "" {
		value = strings.ToUpper(strings.TrimSpace(name))
	}
	if runes := []rune(value); len(runes) > 7 {
		value = string(runes[:7])
	}
	return value
}

func selectionTextColor(hexColor string) string {
	value := strings.TrimPrefix(strings.TrimSpace(hexColor), "#")
	if len(value) != 6 {
		return "255"
	}
	var red, green, blue uint64
	if _, err := fmt.Sscanf(value, "%02x%02x%02x", &red, &green, &blue); err != nil {
		return "255"
	}
	if 0.299*float64(red)+0.587*float64(green)+0.114*float64(blue) > 145 {
		return "0"
	}
	return "255"
}

func pickerInputLine(search string, width int) string {
	value := search + "▏"
	return pickerRow(value, width)
}

func pickerBorder(width int) string {
	return neutralBorder("╰" + strings.Repeat("─", width) + "╯")
}

func pickerTopBorder(width int) string {
	return neutralBorder("╭" + strings.Repeat("─", width) + "╮")
}

func pickerRow(value string, width int) string {
	return neutralBorder("│") + " " + padDisplay(truncateDisplay(value, width-2), width-2) + " " + neutralBorder("│")
}

func neutralBorder(value string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(value)
}

func firstFamily(station gtfs.Station) string {
	if len(station.FamilyIDs) == 0 {
		if len(station.LineIDs) == 0 {
			return ""
		}
		return station.LineIDs[0]
	}
	return station.FamilyIDs[0]
}

func primaryFamily(station gtfs.Station) string { return firstFamily(station) }

func (m Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	wasPicker := m.picker
	switch value := msg.String(); value {
	case "esc":
		m.picker = false
	case "backspace":
		if m.search != "" {
			runes := []rune(m.search)
			m.search = string(runes[:len(runes)-1])
			m.pickerPos = 0
		} else {
			m.picker = false
		}
	case "up", "ctrl+k", "ctrl+p":
		m.pickerPos = max(m.pickerPos-1, 0)
		m.keepPickerVisible()
	case "down", "ctrl+j", "ctrl+n":
		m.pickerPos = min(m.pickerPos+1, max(len(m.filteredStations())-1, 0))
		m.keepPickerVisible()
	case "tab":
		// Keep the picker modal; tab must not leak focus to the background.
		return m, nil
	case "enter":
		stations := m.filteredStations()
		if m.pickerPos < len(stations) {
			if m.focus == focusFrom {
				m.fromStation = stations[m.pickerPos].ID
				m.focus = focusTo
				m.search = ""
				m.pickerPos = 0
				m.pickerTop = 0
				m.routeSeq++
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.routeCmd(), m.syncSimulation())
			} else {
				m.toStation = stations[m.pickerPos].ID
				m.picker = false
			}
			m.routeSeq++
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.routeCmd(), m.syncSimulation())
		}
	default:
		if value == "space" {
			m.search += " "
			m.pickerPos = 0
			m.pickerTop = 0
		} else if len([]rune(value)) == 1 && value >= " " {
			m.search += value
			m.pickerPos = 0
			m.pickerTop = 0
		}
	}
	if wasPicker && !m.picker {
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())
	}
	return m, nil
}

func (m *Model) keepPickerVisible() {
	_, _, _, visible := m.pickerGeometry()
	if m.pickerPos < m.pickerTop {
		m.pickerTop = m.pickerPos
	}
	if m.pickerPos >= m.pickerTop+visible {
		m.pickerTop = m.pickerPos - visible + 1
	}
}

func (m Model) familyPresentation(id string) (string, string) {
	if family, ok := m.feedIndexes.FamilyByID[id]; ok {
		name := family.DisplayName
		if strings.TrimSpace(name) == "" {
			name = id
		}
		color := family.RendererColor
		if color == "" {
			color = "#808080"
		}
		return name, color
	}
	return id, "#808080"
}

func (m Model) hudText() string {
	zoom := fmt.Sprintf("z:%.1f", m.zoom)
	coords := fmt.Sprintf("%.4f°N  %.4f°E", m.lat, m.lon)
	scale := zoomToScale(int(math.Floor(m.zoom)))
	endpoints := fmt.Sprintf("FROM:%s TO:%s", m.endpointName(m.fromStation), m.endpointName(m.toStation))
	simStatus := "SIM:paused"
	if m.simulationEligible() {
		simStatus = "SIM:running"
		if m.simulationReducedMotion() {
			simStatus = "SIM:reduced"
		}
	} else if m.feedState == FeedStateReady && m.simulationReducedMotion() {
		simStatus = "SIM:reduced"
	}
	return strings.Join([]string{zoom, "N↑", coords, scale, m.dataStatus(), simStatus, endpoints, "? help"}, " │ ")
}

func (m Model) sidebarWidth() int {
	if m.width < 52 {
		return 0
	}
	return min(44, max((m.width*2)/5, 30))
}

func (m Model) sidebarLines(height, width int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("109"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	lines := []string{centerDisplay(accent.Render("METROSHELL"), width), ""}
	lines = append(lines, m.endpointField("FROM", m.fromStation, m.focus == focusFrom, width)...)
	lines = append(lines, "")
	lines = append(lines, m.endpointField("TO", m.toStation, m.focus == focusTo, width)...)
	lines = append(lines, "")
	if m.route.Status == gtfs.RouteReady {
		lines = append(lines, accent.Render(" "+m.routeSummary()), "")
	}
	switch m.feedState {
	case FeedStateLoading:
		lines = append(lines, dim.Render(" Loading feed…"))
	case FeedStateError:
		lines = append(lines, dim.Render(" Feed unavailable"))
	case FeedStateMissing:
		lines = append(lines, dim.Render(" No feed configured"))
	case FeedStateReady:
		if len(m.feedIndexes.OrderedStations) == 0 {
			lines = append(lines, dim.Render(" No stations available"))
		} else {
			lines = append(lines, accent.Render(" JOURNEY"))
			if m.focus == focusMap {
				nearest := render.NearestStation(m.feedIndexes, m.viewport(), m.cursor)
				if nearest != "" {
					lines = append(lines, dim.Render(" Cursor: "+m.endpointName(nearest)))
				}
			}
		}
	}
	if m.fromStation != "" && m.fromStation == m.toStation {
		lines = append(lines, dim.Render(" Same endpoint selected"))
	} else if m.fromStation != "" && m.toStation != "" {
		lines = append(lines, dim.Render(" Route ready · highlighted on map"))
		if m.route.Status == gtfs.RouteReady {
			for i, leg := range m.route.Legs {
				name := leg.FamilyName
				if name == "" {
					name = leg.FamilyID
				}
				lines = append(lines, accent.Render(fmt.Sprintf(" %d  %s", i+1, name)))
				lines = append(lines, dim.Render("    FROM "+m.endpointName(leg.From)))
				lines = append(lines, dim.Render("    TO   "+m.endpointName(leg.To)+fmt.Sprintf("  · %d stops", leg.Stops)))
				if i+1 < len(m.route.Legs) {
					lines = append(lines, dim.Render("    TRANSFER at "+m.endpointName(leg.To)))
				}
			}
		}
	}
	for i := range lines {
		lines[i] = padDisplay(truncateDisplay(lines[i], width), width)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func (m Model) endpointField(label, stationID string, focused bool, width int) []string {
	marker := "  "
	if focused {
		marker = "> "
	}
	value := m.endpointName(stationID)
	if stationID != "" {
		if station, ok := m.feedIndexes.StationByID[stationID]; ok {
			if indicator := stationIndicatorText(m, station); indicator != "" {
				value += "  " + indicator
			}
		}
	}
	innerWidth := max(width-4, 0)
	content := truncateDisplay(marker+label+": "+value, innerWidth)
	return []string{
		neutralBorder("╭" + strings.Repeat("─", max(width-2, 0)) + "╮"),
		endpointBorderLine(content, innerWidth),
		neutralBorder("╰" + strings.Repeat("─", max(width-2, 0)) + "╯"),
	}
}

func endpointBorderLine(content string, innerWidth int) string {
	return neutralBorder("│") + " " + padDisplay(content, innerWidth) + " " + neutralBorder("│")
}

func (m Model) endpointName(stationID string) string {
	if stationID == "" {
		return ""
	}
	if station, ok := m.feedIndexes.StationByID[stationID]; ok {
		if strings.TrimSpace(station.Name) != "" {
			return station.Name
		}
	}
	return stationID
}

func centerDisplay(value string, width int) string {
	valueWidth := lipgloss.Width(stripANSI(value))
	if valueWidth >= width {
		return truncateDisplay(value, width)
	}
	left := (width - valueWidth) / 2
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", width-valueWidth-left)
}

func (m Model) routeSummary() string {
	if m.route.Status != gtfs.RouteReady {
		return m.route.Message
	}
	sequence := make([]string, 0, len(m.route.FamilyNames))
	for i, familyID := range m.route.FamilyIDs {
		name := familyID
		if i < len(m.route.FamilyNames) && strings.TrimSpace(m.route.FamilyNames[i]) != "" {
			name = m.route.FamilyNames[i]
		}
		if len(sequence) == 0 || sequence[len(sequence)-1] != name {
			sequence = append(sequence, name)
		}
	}
	return fmt.Sprintf("%d stops · %d transfers · %s", m.route.Stops, m.route.Transfers, strings.Join(sequence, " → "))
}

func truncateDisplay(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(value, width, "…")
}

func padDisplay(value string, width int) string {
	return value + strings.Repeat(" ", max(width-lipgloss.Width(stripANSI(value)), 0))
}

func stripANSI(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); {
		if value[i] == '\x1b' && i+1 < len(value) && value[i+1] == '[' {
			i += 2
			for i < len(value) {
				if (value[i] >= 'a' && value[i] <= 'z') || (value[i] >= 'A' && value[i] <= 'Z') {
					i++
					break
				}
				i++
			}
			continue
		}
		b.WriteByte(value[i])
		i++
	}
	return b.String()
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
		lineCount := len(m.feedIndexes.Lines)
		if len(m.feedIndexes.Families) > 0 {
			lineCount = len(m.feedIndexes.Families)
		}
		return fmt.Sprintf("GTFS: ready (%d stops, %d lines)", len(m.feedIndexes.Stations), lineCount)
	default:
		return "GTFS: missing"
	}
}

func (m Model) renderCmd() tea.Cmd {
	cache := m.cache
	lat, lon, zoom := m.lat, m.lon, m.zoom
	pixelW := m.mapWidth() * 2
	pixelH := (m.height - 2) * 4
	seq := m.renderSeq
	var indexes *gtfs.Indexes
	var simConfig sim.Config
	if m.feedState == FeedStateReady {
		snapshot := m.feedIndexes
		indexes = &snapshot
		simConfig = m.simulationConfig()
	}
	return func() tea.Msg {
		trains := sim.Snapshot(simConfig)
		frame := render.Render(render.RenderRequest{
			DB:     cache,
			Lat:    lat,
			Lon:    lon,
			Zoom:   zoom,
			PixelW: pixelW,
			PixelH: pixelH,
			GTFS:   indexes,
			Trains: trains,
			Route:  routePtr(m.route),
			Cursor: &m.cursor,
		})
		return frameReadyMsg{seq: seq, frame: frame}
	}
}

const (
	trainCadence        = 250 * time.Millisecond
	simulationClockStep = sim.ClockCycle / 10
)

func trainTickCmd(generation uint64) tea.Cmd {
	return tea.Tick(trainCadence, func(time.Time) tea.Msg {
		return trainTickMsg{generation: generation}
	})
}

func simulationRoutes(indexes gtfs.Indexes) []sim.Route {
	routes := make([]sim.Route, 0)
	if len(indexes.OrderedLines) > 0 {
		for _, line := range indexes.OrderedLines {
			for _, shape := range line.Shapes {
				routes = append(routes, sim.Route{FamilyID: line.FamilyID, RouteID: line.ID, ShapeID: shape.ShapeID, Shape: simShape(shape.Geometry)})
			}
		}
		return routes
	}
	for _, family := range indexes.OrderedFamilies {
		for _, shape := range family.Shapes {
			routes = append(routes, sim.Route{FamilyID: family.ID, RouteID: family.ID, ShapeID: shape.ShapeID, Shape: simShape(shape.Geometry)})
		}
	}
	return routes
}

func simShape(points []orb.Point) []sim.Point {
	shape := make([]sim.Point, len(points))
	for i, point := range points {
		shape[i] = sim.Point{Lon: point.X(), Lat: point.Y()}
	}
	return shape
}

func routePtr(route gtfs.RouteResult) *gtfs.RouteResult {
	if route.Status != gtfs.RouteReady {
		return nil
	}
	return &route
}

func (m Model) routeCmd() tea.Cmd {
	seq := m.routeSeq
	from, to := m.fromStation, m.toStation
	graph := m.feedIndexes.Graph
	ready := m.feedState == FeedStateReady
	// The sequence is advanced before the command is built so every endpoint
	// change invalidates work already in flight.
	return func() tea.Msg {
		if !ready {
			return routeReadyMsg{seq: seq, result: gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: "Route unavailable until GTFS is ready"}}
		}
		return routeReadyMsg{seq: seq, result: gtfs.PlanRoute(graph, from, to)}
	}
}

func (m Model) mapWidth() int {
	innerW := max(m.width-2, 0)
	if panelW := m.sidebarWidth(); panelW > 0 {
		return max(innerW-panelW-1, 0)
	}
	return innerW
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
