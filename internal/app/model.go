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
	// TrainAcceleration controls the internal schedule-shaped train view. It
	// is not a user-facing clock; zero selects the calm real-feed default.
	TrainAcceleration float64
	// Now supplies the computer wall clock. Production uses time.Now; tests can
	// inject an exact Delhi-local instant for deterministic schedule windows.
	Now func() time.Time
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
	cache             *render.TileCache
	lat               float64
	lon               float64
	zoom              float64
	width             int
	height            int
	showHelp          bool
	splash            bool
	picker            bool
	search            string
	pickerPos         int
	pickerTop         int
	frame             string
	renderSeq         uint64
	trainClock        int64
	trainSeed         uint64
	trainFleet        int
	trainAcceleration float64
	focused           bool
	simRunning        bool
	simGeneration     uint64
	routeSeq          uint64
	routeAutoFit      bool
	status            string
	notice            string

	gtfsPath    string
	feedState   FeedState
	feedError   error
	feedSeq     uint64
	feed        gtfs.Feed
	feedIndexes gtfs.Indexes
	simCache    *simulationRouteCache

	focus              endpointFocus
	fromStation        string
	toStation          string
	route              gtfs.RouteResult
	clock              func() time.Time
	showScheduleDetail bool
	selectedLeg        int
	expandedLeg        int
}

type simulationRouteCache struct {
	valid            bool
	feedSeq          uint64
	serviceDate      string
	accelerationBits uint64
	routes           []sim.Route
	hits, misses     uint64
	invalidations    uint64
}

// SimulationCacheStats provides deterministic diagnostics for the immutable
// schedule projection. Counters are intentionally independent of timing.
type SimulationCacheStats struct {
	Hits, Misses, Invalidations uint64
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
	seq     uint64
	feedSeq uint64
	result  gtfs.RouteResult
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
	acceleration := config.TrainAcceleration
	if acceleration <= 0 {
		acceleration = defaultTrainAcceleration
	}
	return Model{
		cache:             cache,
		lat:               lat,
		lon:               lon,
		zoom:              12,
		status:            "Waiting for terminal size...",
		gtfsPath:          gtfsPath,
		feedState:         feedState,
		feedSeq:           boolUint64(feedState == FeedStateLoading),
		route:             gtfs.RouteResult{Status: gtfs.RouteNoEndpoints, Message: "Select FROM and TO stations"},
		routeAutoFit:      true,
		trainSeed:         41,
		trainFleet:        24,
		trainAcceleration: acceleration,
		focused:           true,
		splash:            true,
		clock:             config.Now,
		simCache:          &simulationRouteCache{},
		selectedLeg:       -1,
		expandedLeg:       -1,
	}
}

func boolUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
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
		// Clock is advanced internally for deterministic train view motion. The
		// wall clock shown to passengers remains m.now() and never this value.
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
		if m.splash {
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			if msg.String() != "enter" {
				return m, nil
			}
			m.splash = false
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.syncSimulation())
		}
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
		case "r":
			if m.feedState == FeedStateError || (m.feedState == FeedStateMissing && m.gtfsPath != "") {
				m.feedState = FeedStateLoading
				m.feedError = nil
				m.feedSeq++
				m.status = "Loading GTFS..."
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.loadFeedCmd(), m.syncSimulation())
			}
			return m, nil
		case "e":
			if m.route.Status == gtfs.RouteReady {
				m.toggleSelectedLeg()
			} else {
				m.showScheduleDetail = !m.showScheduleDetail
			}
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
			if m.route.Status == gtfs.RouteReady && m.focus == focusMap && len(m.route.Legs) > 0 {
				m.toggleSelectedLeg()
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.syncSimulation())
			}
			m.picker = true
			m.search = ""
			m.pickerPos = 0
			m.pickerTop = 0
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.syncSimulation())
		case "esc":
			if m.expandedLeg >= 0 {
				m.expandedLeg = -1
				m.showScheduleDetail = false
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.syncSimulation())
			}
			m.clearFocusedEndpoint()
			m.routeSeq++
			m.clearRouteForPendingSelection()
			m.setStatus()
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.routeCmd(), m.syncSimulation())
		case "backspace":
			m.clearFocusedEndpoint()
			m.routeSeq++
			m.clearRouteForPendingSelection()
			m.setStatus()
			m.invalidate()
			return m, tea.Batch(m.renderCmd(), m.routeCmd(), m.syncSimulation())
		case "w":
			m.routeAutoFit = false
			m.lat += geo.PanAmount(m.zoom)
		case "s":
			m.routeAutoFit = false
			m.lat -= geo.PanAmount(m.zoom)
		case "a":
			m.routeAutoFit = false
			m.lon -= geo.PanAmount(m.zoom)
		case "d":
			m.routeAutoFit = false
			m.lon += geo.PanAmount(m.zoom)
		case "up", "k":
			if m.route.Status == gtfs.RouteReady && m.focus == focusMap {
				m.moveLeg(-1)
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.syncSimulation())
			}
			if m.focus != focusMap {
				return m, nil
			}
			m.routeAutoFit = false
			m.lat += geo.PanAmount(m.zoom)
		case "down", "j":
			if m.route.Status == gtfs.RouteReady && m.focus == focusMap {
				m.moveLeg(1)
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.syncSimulation())
			}
			if m.focus != focusMap {
				return m, nil
			}
			m.routeAutoFit = false
			m.lat -= geo.PanAmount(m.zoom)
		case "left", "h":
			m.routeAutoFit = false
			m.lon -= geo.PanAmount(m.zoom)
		case "right", "l":
			m.routeAutoFit = false
			m.lon += geo.PanAmount(m.zoom)
		case "+", "=", ",":
			m.routeAutoFit = false
			if m.zoom >= 15.9 {
				return m, nil
			}
			m.zoom += 0.2
		case "-", "_", ".":
			m.routeAutoFit = false
			if m.zoom <= 5.1 {
				return m, nil
			}
			m.zoom -= 0.2
		default:
			return m, nil
		}
		m.setStatus()
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case tea.MouseMsg:
		// Cell-motion mode is required for terminals to deliver wheel events.
		// Only the wheel buttons have behavior here: clicks, releases, and drag
		// motion are deliberately ignored so mouse input cannot select stations,
		// move a map cursor, or pan the viewport.
		if m.splash || m.showHelp || m.picker {
			return m, nil
		}
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			m.routeAutoFit = false
			if m.zoom >= 15.9 {
				return m, nil
			}
			m.zoom += 0.1
		case tea.MouseWheelDown:
			m.routeAutoFit = false
			if m.zoom <= 5.1 {
				return m, nil
			}
			m.zoom -= 0.1
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
		if msg.seq != 0 && msg.seq != m.feedSeq {
			return m, nil
		}
		m.feed = msg.feed
		m.feedIndexes = msg.indexes
		m.feedSeq++
		m.simCache = &simulationRouteCache{}
		m.feedError = nil
		m.feedState = FeedStateReady
		m.notice = ""
		m.fromStation, m.toStation = "", ""
		m.focus = focusMap
		m.routeSeq++
		m.clearRouteForPendingSelection()
		m.route = noEndpointRoute()
		m.frame = ""
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
		if msg.seq != 0 && msg.seq != m.feedSeq {
			return m, nil
		}
		m.feed = gtfs.Feed{}
		m.feedIndexes = gtfs.Indexes{}
		m.feedSeq++
		m.simCache = &simulationRouteCache{}
		m.feedError = nil
		m.feedState = FeedStateMissing
		m.notice = "Map only · no GTFS feed found"
		m.routeSeq++
		m.clearRouteForPendingSelection()
		message := "No GTFS feed configured"
		if m.gtfsPath != "" {
			message = "No GTFS feed found"
		}
		m.route = gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: message}
		m.frame = ""
		m.invalidate()
		if m.cache == nil {
			return m, nil
		}
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case feedErrorMsg:
		if msg.seq != 0 && msg.seq != m.feedSeq {
			return m, nil
		}
		m.feed = gtfs.Feed{}
		m.feedIndexes = gtfs.Indexes{}
		m.feedSeq++
		m.simCache = &simulationRouteCache{}
		m.feedError = msg.err
		m.feedState = FeedStateError
		m.notice = "GTFS unavailable · press r to retry"
		m.routeSeq++
		m.route = gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: "GTFS feed unavailable · press r to retry"}
		m.frame = ""
		m.invalidate()
		if m.cache == nil {
			return m, nil
		}
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())

	case routeReadyMsg:
		if msg.seq != m.routeSeq || m.feedState != FeedStateReady || (msg.feedSeq != 0 && msg.feedSeq != m.feedSeq) {
			return m, nil
		}
		m.route = msg.result
		m.selectedLeg = 0
		m.expandedLeg = -1
		m.showScheduleDetail = false
		m.notice = ""
		if msg.result.Status != gtfs.RouteReady {
			m.notice = msg.result.Message
		}
		m.routeAutoFit = true
		m.fitSelectedRoute()
		m.invalidate()
		return m, tea.Batch(m.renderCmd(), m.syncSimulation())
	}
	return m, nil
}

const (
	// Panel borders consume two cells each; keep one deliberate cell between
	// them so the map and sidebar read as separate surfaces rather than one
	// frame with an incidental divider.
	panelGap = 1

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
	splashShellWidth  = 84
	splashShellHeight = 13
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

// SimulationCacheStats reports cache behavior for profiling and parity tests.
func (m Model) SimulationCacheStats() SimulationCacheStats {
	if m.simCache == nil {
		return SimulationCacheStats{}
	}
	return SimulationCacheStats{Hits: m.simCache.hits, Misses: m.simCache.misses, Invalidations: m.simCache.invalidations}
}

func (m Model) simulationConfig() sim.Config {
	now := m.now()
	return sim.Config{
		Seed:          m.trainSeed,
		Clock:         m.trainClock,
		Fleet:         m.trainFleet,
		Paused:        m.simulationPaused(),
		ReducedMotion: m.simulationReducedMotion(),
		Acceleration:  m.trainAcceleration,
		Routes:        m.cachedSimulationRoutes(now),
	}
}

func (m Model) cachedSimulationRoutes(now time.Time) []sim.Route {
	if m.simCache == nil {
		return simulationRoutes(m.feedIndexes, now)
	}
	localDate := now.In(gtfs.DelhiLocation).Format("2006-01-02")
	accelerationBits := math.Float64bits(m.trainAcceleration)
	if m.simCache.valid && m.simCache.feedSeq == m.feedSeq && m.simCache.serviceDate == localDate && m.simCache.accelerationBits == accelerationBits {
		m.simCache.hits++
		return m.simCache.routes
	}
	if m.simCache.valid {
		m.simCache.invalidations++
	}
	m.simCache.valid = true
	m.simCache.feedSeq = m.feedSeq
	m.simCache.serviceDate = localDate
	m.simCache.accelerationBits = accelerationBits
	m.simCache.routes = simulationRoutes(m.feedIndexes, now)
	m.simCache.misses++
	return m.simCache.routes
}

func (m *Model) viewport() geo.Viewport {
	return geo.Viewport{
		Lat: m.lat, Lon: m.lon, Zoom: m.zoom,
		PixelW: m.mapWidth() * 2,
		PixelH: max(m.height-2, 0) * 4,
	}
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
}

const routeFitPadding = 12

func (m *Model) moveLeg(delta int) {
	if len(m.route.Legs) == 0 {
		m.selectedLeg = -1
		m.expandedLeg = -1
		return
	}
	if m.selectedLeg < 0 {
		m.selectedLeg = 0
	}
	m.selectedLeg = min(max(m.selectedLeg+delta, 0), len(m.route.Legs)-1)
}

func (m *Model) toggleSelectedLeg() {
	if len(m.route.Legs) == 0 {
		return
	}
	if m.selectedLeg < 0 || m.selectedLeg >= len(m.route.Legs) {
		m.selectedLeg = 0
	}
	if m.expandedLeg == m.selectedLeg {
		m.expandedLeg = -1
		m.showScheduleDetail = false
		return
	}
	m.expandedLeg = m.selectedLeg
	m.showScheduleDetail = true
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
	if m.width <= 0 || m.height <= 0 {
		return tea.NewView("")
	}
	mapW := m.mapWidth()
	mapRows := contentRows(m.frame, mapW, max(m.height-2, 0))
	mapPanel := m.mapPanel(mapRows, mapW)

	viewContent := strings.Join(mapPanel, "\n")
	if sidebarW := m.sidebarWidth(); sidebarW > 0 {
		sidebar := renderPanel(m.sidebarLines(max(m.height-2, 0), sidebarW), sidebarW, m.height, sidebarBorderStyle())
		viewContent = joinPanels(mapPanel, sidebar, panelGap)
	}
	if m.showHelp {
		viewContent = m.helpOverlay(viewContent)
	} else if m.picker {
		shellW, shellH, _, _ := m.pickerGeometry()
		viewContent = m.overlayShellFixed(viewContent, m.pickerLines(), shellW, shellH)
	}
	if m.splash {
		viewContent = m.splashOverlay(viewContent)
	}
	viewContent = boundedView(viewContent, m.width, m.height)
	view := tea.NewView(viewContent)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

// SplashVisible reports whether the bounded launch splash is still shown.
// It is exported so local and SSH integration tests can assert the shared
// lifecycle without depending on renderer internals.
func (m Model) SplashVisible() bool { return m.splash }

func chromeBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
}

func splashBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("201"))
}

func sidebarBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("239"))
}

func contentRows(content string, width, height int) []string {
	rows := []string{}
	if content != "" {
		rows = strings.Split(strings.TrimRight(content, "\n"), "\n")
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	for i := range rows {
		rows[i] = padDisplay(truncateDisplay(rows[i], width), width)
	}
	return rows
}

// renderPanel wraps content in a complete neutral shell. Content is kept as a
// separate inner region so neutral endpoint borders and colored map layers do
// not accidentally become part of the panel chrome.
func renderPanel(content []string, contentWidth, height int, border lipgloss.Style) []string {
	outerWidth := max(contentWidth+2, 2)
	rows := []string{border.Render("╭" + strings.Repeat("─", max(outerWidth-2, 0)) + "╮")}
	for i := 0; i < max(height-2, 0); i++ {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		rows = append(rows, border.Render("│")+padDisplay(truncateDisplay(line, contentWidth), contentWidth)+border.Render("│"))
	}
	rows = append(rows, border.Render("╰"+strings.Repeat("─", max(outerWidth-2, 0))+"╯"))
	return rows
}

func joinPanels(left, right []string, gap int) string {
	rows := make([]string, max(len(left), len(right)))
	for i := range rows {
		if i < len(left) {
			rows[i] = left[i]
		}
		rows[i] += strings.Repeat(" ", gap)
		if i < len(right) {
			rows[i] += right[i]
		}
	}
	return strings.Join(rows, "\n")
}

func (m Model) mapPanel(rows []string, contentWidth int) []string {
	border := chromeBorderStyle()
	if m.sidebarWidth() == 0 && m.width < sidebarThreshold {
		rows = make([]string, max(m.height-2, 0))
		if len(rows) > 0 {
			message := "Enlarge terminal to 52 columns"
			rows[len(rows)/2] = centerDisplay(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(message), contentWidth)
		}
	}
	panel := renderPanel(rows, contentWidth, m.height, border)
	if len(panel) == 0 {
		return nil
	}
	// Keep the HUD in the map panel's bottom chrome. It remains readable at
	// normal sizes, while narrow terminals receive the resize instruction.
	outerWidth := contentWidth + 2
	available := max(outerWidth-6, 0)
	// Put data state first: on a narrow map panel the full diagnostic HUD is
	// intentionally clipped, but map-only/loading/error states remain explicit.
	hudText := truncateDisplay(m.hudText(), available)
	padLen := max(available-lipgloss.Width(stripANSI(hudText)), 0)
	hudStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	if m.status == "Rendering..." {
		hudStyle = hudStyle.Foreground(lipgloss.Color("240"))
	}
	panel[len(panel)-1] = border.Render("╰─ ") + hudStyle.Render(hudText) + border.Render(" "+strings.Repeat("─", padLen)+"─╯")
	return panel
}

// boundedView is the final safety net for map and modal composition during a
// resize. Every returned row fits the terminal and the view never grows past
// its reported height.
func boundedView(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(rows) == 0 {
		rows = []string{""}
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	for i := range rows {
		rows[i] = padDisplay(truncateDisplay(rows[i], width), width)
	}
	return strings.Join(rows, "\n")
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
		"    " + key.Render("Tab / Shift-Tab") + dim.Render(" focus map, FROM, or TO"),
		"    " + key.Render("Enter") + dim.Render(" choose a station; expand a ready leg on map"),
		"    " + key.Render("Esc") + dim.Render(" cancel picker, collapse detail, or clear focus"),
		"    " + key.Render("Backspace") + dim.Render(" clear the focused endpoint"),
		"    " + key.Render("w a s d / ←→ h l") + dim.Render(" pan map"),
		"    " + key.Render("↑↓ / j k") + dim.Render(" select a ready journey leg"),
		"",
		accent.Render("  Zoom"),
		"    " + key.Render("+ = ,") + dim.Render(" zoom in   ") + key.Render("- _ .") + dim.Render(" zoom out"),
		"",
		accent.Render("  Map symbols"),
		"    " + key.Render("M") + dim.Render("  metro station    ") + key.Render("T") + dim.Render("  rail/train station"),
		"    " + key.Render("+") + dim.Render("  hospital         ") + key.Render("🍴, 🍲, 🍜, 🥐") + dim.Render("  food places"),
		"    " + key.Render("g") + dim.Render("  fuel station"),
		"",
		accent.Render("  Other"),
		"    " + key.Render("?") + dim.Render(" toggle help   ") + key.Render("q") + dim.Render(" quit   ") + key.Render("r") + dim.Render(" retry feed"),
		"    " + key.Render("e") + dim.Render(" expand/collapse the selected leg"),
		"    " + dim.Render("Trains pause when unfocused, overlaid, or below 20×8; compact terminals reduce motion."),
		"    " + dim.Render(fmt.Sprintf("Train view ×%g is illustrative and uses static GTFS timing; it is not live telemetry.", defaultTrainAcceleration)),
		"    " + dim.Render("Schedules are offline GTFS; expired weekly calendars are marked estimated."),
		"    " + dim.Render("NEXT SERVICE is a timetable calculation, not live DMRC service status."),
		"",
		dim.Render("  Tip: set terminal background to #000000 for AMOLED look"),
	}
	return strings.Join(helpLines, "\n")
}

func (m Model) splashOverlay(background string) string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("201")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	core := []string{
		accent.Render("METROSHELL"),
		accent.Render("DELHI METRO STARTING IN YOUR TERMINAL"),
		"",
		"Press Enter to continue",
		dim.Render("built by Akash Parashar"),
	}
	boxW := min(splashShellWidth, max(m.width, 1))
	boxH := min(splashShellHeight, max(m.height, 1))
	innerH := max(boxH-2, 0)
	lines := append(make([]string, max((innerH-len(core))/2, 0)), core...)
	innerW := max(boxW-4, 0)
	for i, line := range lines {
		lines[i] = centerDisplay(line, innerW)
	}
	return m.overlayShellFixedWithBorder(background, lines, boxW, boxH, splashBorderStyle())
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
	return m.overlayShellFixedWithBorder(background, lines, boxW, boxH, chromeBorderStyle())
}

func (m Model) overlayShellFixedWithBorder(background string, lines []string, boxW, boxH int, border lipgloss.Style) string {
	width, height := max(m.width, 1), max(m.height, 1)
	boxW = min(max(boxW, 1), width)
	boxH = min(max(boxH, 1), height)
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
				m.notice = "FROM set · choose TO"
				m.search = ""
				m.pickerPos = 0
				m.pickerTop = 0
				m.routeSeq++
				m.clearRouteForPendingSelection()
				m.invalidate()
				return m, tea.Batch(m.renderCmd(), m.routeCmd(), m.syncSimulation())
			} else {
				m.toStation = stations[m.pickerPos].ID
				m.picker = false
				m.notice = "Planning route..."
			}
			m.routeSeq++
			m.clearRouteForPendingSelection()
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
	parts := []string{m.dataStatus(), "FOCUS:" + m.focusName(), zoom, "N↑", coords, scale, endpoints}
	if m.notice != "" {
		parts = append(parts, m.notice)
	}
	parts = append(parts, "? help")
	return strings.Join(parts, " │ ")
}

func (m Model) focusName() string {
	switch m.focus {
	case focusFrom:
		return "FROM"
	case focusTo:
		return "TO"
	default:
		return "MAP"
	}
}

func (m Model) sidebarWidth() int {
	if m.width < sidebarThreshold {
		return 0
	}
	return min(44, max((m.width*2)/5, 30))
}

const sidebarThreshold = 52

func (m Model) sidebarLines(height, width int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	brand := lipgloss.NewStyle().Foreground(lipgloss.Color("201")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	clock := lipgloss.NewStyle().Foreground(lipgloss.Color("179"))
	lines := []string{
		centerDisplay(brand.Render("METROSHELL"), width),
		centerDisplay(clock.Render(m.now().In(gtfs.DelhiLocation).Format("02 Jan 2006 15:04:05")), width),
		"",
	}
	lines = append(lines, m.endpointField("FROM", m.fromStation, m.focus == focusFrom, width)...)
	lines = append(lines, "")
	lines = append(lines, m.endpointField("TO", m.toStation, m.focus == focusTo, width)...)
	lines = append(lines, "")
	switch m.feedState {
	case FeedStateLoading:
		lines = append(lines, dim.Render(" Loading feed…"))
	case FeedStateError:
		lines = append(lines, dim.Render(" Feed unavailable · r retry"))
	case FeedStateMissing:
		if m.gtfsPath == "" {
			lines = append(lines, dim.Render(" No feed configured · map only"))
		} else {
			lines = append(lines, dim.Render(" No feed found · r retry"))
		}
	case FeedStateReady:
		if len(m.feedIndexes.OrderedStations) == 0 {
			lines = append(lines, dim.Render(" No stations available"))
		} else {
			lines = append(lines, centerDisplay(accent.Render("JOURNEY"), width))
		}
	}
	if m.fromStation != "" && m.fromStation == m.toStation {
		lines = append(lines, dim.Render(" Same endpoint selected"))
	} else if m.fromStation != "" && m.toStation != "" {
		switch m.route.Status {
		case gtfs.RouteReady:
			lines = append(lines, m.journeySummaryLines(width)...)
			lines = append(lines, m.scheduleSummaryLines(width)...)
			// Summary facts stay adjacent; the one-row gap belongs between the
			// journey summary block and the detailed leg list.
			lines = append(lines, "")
			for i, leg := range m.route.Legs {
				if i > 0 {
					lines = append(lines, "")
				}
				lines = append(lines, m.legRow(i, leg, width)...)
				if i == m.expandedLeg {
					lines = append(lines, m.expandedLegLines(i, leg, width)...)
					// Keep the next compact leg visually separate from a long
					// expanded timeline without putting whitespace before its
					// primary header.
				}
			}
		case gtfs.RouteLoading:
			lines = append(lines, dim.Render(" Planning route…"))
		case gtfs.RouteUnreachable:
			lines = append(lines, dim.Render(" No route between selected stations"))
		case gtfs.RouteInvalid:
			lines = append(lines, dim.Render(" Invalid station selection"))
		default:
			if m.route.Message != "" {
				lines = append(lines, dim.Render(" "+m.route.Message))
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

func (m Model) journeySummary() string {
	if !m.route.Schedule.Available() {
		return fmt.Sprintf("%d stops · %d transfers · %s · TIMING UNAVAILABLE", m.route.Stops, m.route.Transfers, m.routeLineSummary())
	}
	return fmt.Sprintf("%d stops · %d transfers · %s", m.route.Stops, m.route.Transfers, formatDuration(m.route.Schedule.Duration))
}

func (m Model) journeySummaryLines(width int) []string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("109")).Bold(true)
	return []string{accent.Render(" " + m.journeySummary())}
}

func (m Model) routeLineSummary() string {
	sequence := make([]string, 0, len(m.route.FamilyNames))
	for i, id := range m.route.FamilyIDs {
		name := id
		if i < len(m.route.FamilyNames) && m.route.FamilyNames[i] != "" {
			name = m.route.FamilyNames[i]
		}
		if len(sequence) == 0 || sequence[len(sequence)-1] != name {
			sequence = append(sequence, name)
		}
	}
	if len(sequence) == 0 {
		return "line unavailable"
	}
	return strings.Join(sequence, " → ") + " · final " + m.endpointName(m.route.ToStation)
}

func (m Model) scheduleSummary() string {
	schedule := m.route.Schedule
	if !schedule.Available() {
		return m.scheduleUnavailableLabel(0)
	}
	trust := "OFFLINE TIMETABLE"
	if schedule.Status == gtfs.ScheduleEstimated {
		trust = "ESTIMATED OFFLINE TIMETABLE"
	}
	return fmt.Sprintf("SCHEDULED · %s · NEXT SERVICE %s → %s · %s", trust, schedule.NextDeparture.Format("15:04"), schedule.NextArrival.Format("15:04"), formatDuration(schedule.Duration))
}

func (m Model) scheduleSummaryLines(width int) []string {
	if !m.route.Schedule.Available() {
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
		return []string{centerDisplay(dim.Render(m.scheduleUnavailableLabel(width)), width)}
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("109")).Bold(true)
	trust := "OFFLINE TIMETABLE"
	if m.route.Schedule.Status == gtfs.ScheduleEstimated {
		trust = "ESTIMATED OFFLINE TIMETABLE"
	}
	if width < 38 {
		trust = "OFFLINE"
		if m.route.Schedule.Status == gtfs.ScheduleEstimated {
			trust = "ESTIMATED"
		}
	}
	return []string{
		centerDisplay(dim.Render("SCHEDULED · "+trust), width),
		accent.Render(fmt.Sprintf(" NEXT SERVICE %s → %s", m.route.Schedule.NextDeparture.Format("15:04"), m.route.Schedule.NextArrival.Format("15:04"))),
		accent.Render(" DURATION " + formatDuration(m.route.Schedule.Duration)),
	}
}

func (m Model) scheduleUnavailableLabel(width int) string {
	label := "TIMING UNAVAILABLE"
	message := strings.ToLower(m.route.Schedule.Message)
	if strings.Contains(message, "no compatible scheduled service") || strings.Contains(message, "scheduled service unavailable") {
		label = "NO SERVICE · timing unavailable"
	}
	if width > 0 && lipgloss.Width(label) > width {
		if strings.HasPrefix(label, "NO SERVICE") {
			label = "NO SERVICE"
		} else {
			label = "TIMING UNAVAILABLE"
		}
	}
	return label
}

// legRow is deliberately a small, stable grid rather than a single line. The
// sidebar can be as narrow as thirty cells, where a combined origin and
// destination would otherwise force one of the passenger-facing facts into an
// ellipsis. Every fact gets its own cell-aligned row instead.
func (m Model) legRow(index int, leg gtfs.RouteLeg, width int) []string {
	name := leg.FamilyName
	if name == "" {
		name = leg.FamilyID
	}
	lineStyle := familyStyle(leg.Color, index == m.selectedLeg)
	marker := lineStyle.Render("▌")
	if index != m.selectedLeg {
		marker = lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("│")
	}
	meta := m.legMeta(leg)
	lineLabel := lineStyle.Render(name)
	header := fmt.Sprintf(" %s %d  %s", marker, index+1, lineLabel)
	if width < 30 {
		// The real sidebar never enters this branch (52 columns is the cutoff),
		// but direct model fixtures use smaller content widths. Keep their
		// primary route fact intact instead of manufacturing an ellipsis.
		return []string{header, "   " + m.endpointName(leg.From) + " → " + m.endpointName(leg.To), "   " + meta}
	}
	if width < 38 {
		return []string{
			padDisplay(truncateDisplay(header, width), width),
			padDisplay(truncateDisplay("   "+m.endpointName(leg.From), width), width),
			padDisplay(truncateDisplay("   → "+m.endpointName(leg.To), width), width),
			padDisplay(truncateDisplay("   "+meta, width), width),
		}
	}
	return []string{
		padDisplay(truncateDisplay(header, width), width),
		padDisplay(truncateDisplay("   "+m.endpointName(leg.From)+" → "+m.endpointName(leg.To), width), width),
		padDisplay(truncateDisplay("   "+meta, width), width),
	}
}

func familyStyle(color string, bold bool) lipgloss.Style {
	if color == "" {
		color = "109"
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	if bold {
		style = style.Bold(true)
	}
	return style
}

func (m Model) legMeta(leg gtfs.RouteLeg) string {
	stops := fmt.Sprintf("%d stops", leg.Stops)
	if leg.Stops == 1 {
		stops = "1 stop"
	}
	if m.route.Schedule.Available() {
		if legIndex := routeLegIndex(m.route, leg); legIndex >= 0 && legIndex < len(m.route.Schedule.Legs) {
			schedule := m.route.Schedule.Legs[legIndex]
			return fmt.Sprintf("%s · %s · %s–%s", stops, compactDuration(schedule.Arrival.Sub(schedule.Departure)), schedule.Departure.Format("15:04"), schedule.Arrival.Format("15:04"))
		}
	}
	return stops + " · duration unavailable"
}

func routeLegIndex(route gtfs.RouteResult, leg gtfs.RouteLeg) int {
	for i, candidate := range route.Legs {
		if candidate.From == leg.From && candidate.To == leg.To && candidate.FamilyID == leg.FamilyID {
			return i
		}
	}
	return -1
}

func (m Model) expandedLegLines(index int, leg gtfs.RouteLeg, width int) []string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	from, to := m.endpointName(leg.From), m.endpointName(leg.To)
	line := familyStyle(leg.Color, true)
	marker := line.Render("▌")
	lineName := leg.FamilyName
	if lineName == "" {
		lineName = leg.FamilyID
	}
	lines := []string{dim.Render(" ") + marker + line.Render(" "+lineName)}
	if !m.route.Schedule.Available() || index >= len(m.route.Schedule.Legs) {
		lines = append(lines, dim.Render(" "+marker+" TIMING UNAVAILABLE"))
		start, end := routeStationIndex(m.route, leg.From), routeStationIndex(m.route, leg.To)
		if start >= 0 && end >= start && end < len(m.route.Stations) {
			for _, station := range m.route.Stations[start : end+1] {
				lines = append(lines, m.timelineStationLines(marker, stationName(m, station), "", width, dim)...)
			}
		}
		return lines
	}
	schedule := m.route.Schedule.Legs[index]
	// Keep the value column at the same right edge for FROM/TO whenever the
	// sidebar permits it. On compact widths the station and time intentionally
	// become adjacent rows, preserving both facts without ellipses.
	lines = append(lines, m.segmentFactLines(marker, "FROM", from, "DEPART "+schedule.Departure.Format("15:04"), width, dim)...)
	lines = append(lines, m.segmentFactLines(marker, "TO", to, "ARRIVE "+schedule.Arrival.Format("15:04"), width, dim)...)
	accent := line.Bold(true)
	lines = append(lines, accent.Render(fmt.Sprintf(" %s %s · %d stops", marker, compactDuration(schedule.Arrival.Sub(schedule.Departure)), leg.Stops)))
	start, end := routeStationIndex(m.route, leg.From), routeStationIndex(m.route, leg.To)
	if start >= 0 && end >= start && end < len(m.route.Stations) {
		stations := m.route.Stations[start : end+1]
		for stationIndex, station := range stations {
			offset := ""
			scheduleStopIndex := start + stationIndex
			if scheduleStopIndex >= 0 && scheduleStopIndex < len(m.route.Schedule.Stops) {
				stop := m.route.Schedule.Stops[scheduleStopIndex]
				if !stop.Arrival.IsZero() && !schedule.Departure.IsZero() {
					offset = fmt.Sprintf("+%dm", int(stop.Arrival.Sub(schedule.Departure)/time.Minute))
				}
			}
			lines = append(lines, m.timelineStationLines(marker, stationName(m, station), offset, width, dim)...)
		}
	}
	return lines
}

func (m Model) segmentFactLines(marker, label, station, value string, width int, dim lipgloss.Style) []string {
	left := " " + marker + " " + label + " " + station
	if lipgloss.Width(stripANSI(left))+1+lipgloss.Width(value) <= width {
		return []string{dim.Render(padDisplay(left, width-lipgloss.Width(value)) + value)}
	}
	return []string{dim.Render(left), dim.Render(" " + marker + "        " + value)}
}

func (m Model) timelineStationLines(marker, station, offset string, width int, dim lipgloss.Style) []string {
	left := " " + marker + " " + station
	if offset == "" {
		return []string{dim.Render(left)}
	}
	if lipgloss.Width(stripANSI(left))+1+lipgloss.Width(offset) <= width {
		return []string{dim.Render(padDisplay(left, width-lipgloss.Width(offset)) + offset)}
	}
	return []string{dim.Render(left), dim.Render(" " + marker + " " + offset)}
}

func compactDuration(value time.Duration) string {
	if value < time.Minute {
		return value.Round(time.Second).String()
	}
	return fmt.Sprintf("%dm", int(value/time.Minute))
}

func routeStationIndex(route gtfs.RouteResult, station string) int {
	for index, value := range route.Stations {
		if value == station {
			return index
		}
	}
	return -1
}

func (m Model) stationTimeline(stations []string) string {
	if len(stations) == 0 {
		return "unavailable"
	}
	names := make([]string, len(stations))
	for i, station := range stations {
		names[i] = stationName(m, station)
	}
	return strings.Join(names, " → ")
}

func (m Model) stationTimelineLines(stations []string, width int) []string {
	if len(stations) == 0 {
		return []string{lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(" stops unavailable")}
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	lines := []string{}
	current := "  "
	for i, station := range stations {
		separator := ""
		if i > 0 {
			separator = " → "
		}
		name := separator + stationName(m, station)
		if lipgloss.Width(current)+lipgloss.Width(name) > max(width-2, 1) && strings.TrimSpace(current) != "" {
			lines = append(lines, dim.Render(current))
			current = "  " + stationName(m, station)
		} else {
			current += name
		}
	}
	if strings.TrimSpace(current) != "" {
		lines = append(lines, dim.Render(current))
	}
	return lines
}

func stationName(m Model, stationID string) string { return m.endpointName(stationID) }

func formatDuration(value time.Duration) string {
	if value < time.Minute {
		return value.Round(time.Second).String()
	}
	return fmt.Sprintf("%dh %02dm", int(value/time.Hour), int(value/time.Minute)%60)
}

// clearRouteForPendingSelection prevents a completed route from remaining
// visually active while endpoint changes are being planned. It also makes
// malformed endpoint IDs fail closed instead of drawing stale geometry.
func (m *Model) clearRouteForPendingSelection() {
	m.selectedLeg = -1
	m.expandedLeg = -1
	m.showScheduleDetail = false
	if m.feedState != FeedStateReady {
		m.route = gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: "Route unavailable until GTFS is ready"}
		if m.feedState == FeedStateMissing {
			m.route.Message = "No GTFS feed configured"
		} else if m.feedState == FeedStateError {
			m.route.Message = "GTFS feed unavailable"
		}
		return
	}
	if m.fromStation == "" || m.toStation == "" {
		m.route = noEndpointRoute()
		return
	}
	m.route = gtfs.RouteResult{
		Status:      gtfs.RouteLoading,
		FromStation: m.fromStation,
		ToStation:   m.toStation,
		Message:     "Planning route…",
	}
}

func noEndpointRoute() gtfs.RouteResult {
	return gtfs.RouteResult{Status: gtfs.RouteNoEndpoints, Message: "Select FROM and TO stations"}
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
		})
		return frameReadyMsg{seq: seq, frame: frame}
	}
}

const (
	trainCadence        = 250 * time.Millisecond
	simulationClockStep = sim.ClockCycle / 10
	// The feed's median consecutive-stop interval is approximately 127s. A
	// 15× internal acceleration makes one stop-to-stop view readable in about
	// 8.5s while keeping the demo motion calm and legible.
	defaultTrainAcceleration = 15.0
	defaultStopInterval      = 127 * time.Second
)

func trainTickCmd(generation uint64) tea.Cmd {
	return tea.Tick(trainCadence, func(time.Time) tea.Msg {
		return trainTickMsg{generation: generation}
	})
}

func simulationRoutes(indexes gtfs.Indexes, now time.Time) []sim.Route {
	active := gtfs.ActiveScheduleServices(indexes, now, gtfs.DefaultSchedulePolicy)
	routes := make([]sim.Route, 0, len(indexes.SimulationRoutes))
	for _, prepared := range indexes.SimulationRoutes {
		intervals := make([]time.Duration, 0)
		for _, timing := range prepared.Timings {
			if active[timing.ServiceID] {
				intervals = append(intervals, timing.Intervals...)
			}
		}
		if len(intervals) == 0 {
			continue
		}
		// Timings are pre-sorted per service. Sorting the small active merge is
		// done once per service-date cache miss, never per render tick.
		sortDurations(intervals)
		routes = append(routes, sim.PrepareRoute(sim.Route{FamilyID: prepared.FamilyID, RouteID: prepared.RouteID, ShapeID: prepared.ShapeID, Shape: simShape(prepared.Geometry), TravelTime: intervals[len(intervals)/2]}))
	}
	return routes
}

func sortDurations(values []time.Duration) {
	for i := 1; i < len(values); i++ {
		value := values[i]
		j := i
		for j > 0 && values[j-1] > value {
			values[j] = values[j-1]
			j--
		}
		values[j] = value
	}
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
	feedSeq := m.feedSeq
	from, to := m.fromStation, m.toStation
	graph := m.feedIndexes.Graph
	ready := m.feedState == FeedStateReady
	// The sequence is advanced before the command is built so every endpoint
	// change invalidates work already in flight.
	return func() tea.Msg {
		if !ready {
			return routeReadyMsg{seq: seq, feedSeq: feedSeq, result: gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: "Route unavailable until GTFS is ready"}}
		}
		result := gtfs.PlanRoute(graph, from, to)
		if result.Status == gtfs.RouteReady {
			result.Schedule = gtfs.PlanScheduledJourney(m.feedIndexes, result, m.now(), gtfs.DefaultSchedulePolicy)
		}
		return routeReadyMsg{seq: seq, feedSeq: feedSeq, result: result}
	}
}

func (m Model) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

func (m Model) mapWidth() int {
	innerW := max(m.width-2, 0)
	if panelW := m.sidebarWidth(); panelW > 0 {
		// panelW excludes the sidebar's two neutral border cells. Reserve two
		// map border cells and one deliberate gap in addition to those.
		return max(innerW-panelW-3, 0)
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
