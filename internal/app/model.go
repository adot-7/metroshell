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
	cache     *render.TileCache
	lat       float64
	lon       float64
	cursor    orb.Point
	zoom      float64
	width     int
	height    int
	showHelp  bool
	picker    bool
	search    string
	pickerPos int
	pickerTop int
	frame     string
	renderSeq uint64
	routeSeq  uint64
	status    string

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
		cache:     cache,
		lat:       lat,
		lon:       lon,
		cursor:    orb.Point{lon, lat},
		zoom:      12,
		status:    "Waiting for terminal size...",
		gtfsPath:  gtfsPath,
		feedState: feedState,
		route:     gtfs.RouteResult{Status: gtfs.RouteNoEndpoints, Message: "Select FROM and TO stations"},
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
		m.status = "Rendering..."
		m.renderSeq++
		return m, m.renderCmd()

	case tea.KeyMsg:
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" || msg.String() == "ctrl+c" {
				if msg.String() == "ctrl+c" || msg.String() == "q" {
					return m, tea.Quit
				}
				m.showHelp = false
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
			return m, nil
		case "tab":
			m.focus = (m.focus + 1) % 3
			m.setStatus()
			m.renderSeq++
			return m, m.renderCmd()
		case "shift+tab":
			if m.focus == focusMap {
				m.focus = focusTo
			} else {
				m.focus--
			}
			m.setStatus()
			m.renderSeq++
			return m, m.renderCmd()
		case "enter":
			if m.focus != focusMap {
				m.picker = true
				m.search = ""
				m.pickerPos = 0
				m.pickerTop = 0
				return m, nil
			}
			m.selectFocusedStation()
			m.routeSeq++
			m.setStatus()
			m.renderSeq++
			return m, tea.Batch(m.renderCmd(), m.routeCmd())
		case "esc", "backspace":
			m.clearFocusedEndpoint()
			m.routeSeq++
			m.setStatus()
			m.renderSeq++
			return m, tea.Batch(m.renderCmd(), m.routeCmd())
		case "I", "ctrl+up":
			m.moveCursor(0, -cursorStepY)
		case "K", "ctrl+down":
			m.moveCursor(0, cursorStepY)
		case "J", "ctrl+left":
			m.moveCursor(-cursorStepX, 0)
		case "L", "ctrl+right":
			m.moveCursor(cursorStepX, 0)
		case "up", "k", "w":
			if m.focus != focusMap {
				m.moveStation(-1)
				m.setStatus()
				m.renderSeq++
				return m, m.renderCmd()
			}
			m.lat += geo.PanAmount(m.zoom)
			m.clampCursor()
		case "down", "j", "s":
			if m.focus != focusMap {
				m.moveStation(1)
				m.setStatus()
				m.renderSeq++
				return m, m.renderCmd()
			}
			m.lat -= geo.PanAmount(m.zoom)
			m.clampCursor()
		case "left", "h", "a":
			m.lon -= geo.PanAmount(m.zoom)
			m.clampCursor()
		case "right", "l", "d":
			m.lon += geo.PanAmount(m.zoom)
			m.clampCursor()
		case "+", "=":
			if m.zoom >= 15.9 {
				return m, nil
			}
			m.zoom += 0.2
			m.clampCursor()
		case "-", "_":
			if m.zoom <= 5.1 {
				return m, nil
			}
			m.zoom -= 0.2
			m.clampCursor()
		default:
			return m, nil
		}
		m.setStatus()
		m.renderSeq++
		return m, m.renderCmd()

	case tea.MouseMsg:
		if m.showHelp || m.picker {
			return m, nil
		}
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			if m.zoom >= 15.9 {
				return m, nil
			}
			m.zoom += 0.1
			m.clampCursor()
		case tea.MouseWheelDown:
			if m.zoom <= 5.1 {
				return m, nil
			}
			m.zoom -= 0.1
			m.clampCursor()
		default:
			return m, nil
		}
		m.setStatus()
		m.renderSeq++
		return m, m.renderCmd()

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
		m.renderSeq++
		if m.cache == nil {
			return m, nil
		}
		return m, tea.Batch(m.renderCmd(), m.routeCmd())

	case feedMissingMsg:
		m.feed = gtfs.Feed{}
		m.feedIndexes = gtfs.Indexes{}
		m.feedError = nil
		m.feedState = FeedStateMissing
		m.routeSeq++
		m.route = gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: "No GTFS feed configured"}
		m.renderSeq++
		if m.cache == nil {
			return m, nil
		}
		return m, m.renderCmd()

	case feedErrorMsg:
		m.feed = gtfs.Feed{}
		m.feedIndexes = gtfs.Indexes{}
		m.feedError = msg.err
		m.feedState = FeedStateError
		m.routeSeq++
		m.route = gtfs.RouteResult{Status: gtfs.RouteUnavailable, Message: "GTFS feed unavailable"}
		m.renderSeq++
		if m.cache == nil {
			return m, nil
		}
		return m, m.renderCmd()

	case routeReadyMsg:
		if msg.seq != m.routeSeq {
			return m, nil
		}
		m.route = msg.result
		m.renderSeq++
		return m, m.renderCmd()
	}
	return m, nil
}

const (
	cursorStepX = 2.0
	cursorStepY = 4.0
)

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
		framed.WriteString(truncateDisplay(line, mapW))
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
		viewContent = m.overlayShell(viewContent, m.pickerLines(), 68, 26)
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
		"    " + key.Render("I J K L") + dim.Render("  move map cursor (or Ctrl+Arrow)"),
		"    " + key.Render("Tab") + dim.Render("         focus FROM/TO; Enter selects station"),
		"    " + key.Render("Esc/Backspace") + dim.Render(" clear focused endpoint"),
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

func (m Model) helpOverlay(background string) string {
	return m.overlayShell(background, strings.Split(strings.TrimRight(m.helpContent(), "\n"), "\n"), 72, 24)
}

func (m Model) overlayShell(background string, lines []string, maxW, maxH int) string {
	width, height := max(m.width, 1), max(m.height, 1)
	boxW := min(maxW, max(width-2, 1))
	boxH := min(maxH, max(height-2, 1))
	if boxW < 4 {
		boxW = width
	}
	if boxH < 4 {
		boxH = height
	}
	innerW := max(boxW-4, 0)
	var box []string
	box = append(box, "╭"+strings.Repeat("─", boxW-2)+"╮")
	for i := 0; i < boxH-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if boxW >= 4 {
			line = truncateDisplay(line, innerW)
			box = append(box, "│ "+padDisplay(line, innerW)+" │")
		} else {
			box = append(box, padDisplay(truncateDisplay(line, boxW), boxW))
		}
	}
	box = append(box, "╰"+strings.Repeat("─", boxW-2)+"╯")
	bg := strings.Split(strings.TrimRight(background, "\n"), "\n")
	for i := range bg {
		bg[i] = dimLine(bg[i])
	}
	left := max((width-boxW)/2, 0)
	top := max((height-boxH)/2, 0)
	for i := 0; i < height; i++ {
		if i >= len(bg) {
			bg = append(bg, "")
		}
		bg[i] = padDisplay(truncateDisplay(bg[i], width), width)
		if i >= top && i < top+boxH {
			line := box[i-top]
			bg[i] = strings.Repeat(" ", left) + line + strings.Repeat(" ", max(width-left-lipgloss.Width(line), 0))
		}
	}
	return strings.Join(bg[:height], "\n")
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
	label := "FROM"
	if m.focus == focusTo {
		label = "TO"
	}
	lines := []string{"", "  Select " + label, "", "  Search: " + m.search, ""}
	stations := m.filteredStations()
	if len(stations) == 0 {
		return append(lines, "  No matching stations", "", "  Esc cancel")
	}
	visible := max(min(m.height-8, 12), 1)
	top := min(max(m.pickerTop, 0), len(stations)-1)
	if m.pickerPos < top {
		top = m.pickerPos
	}
	end := min(top+visible, len(stations))
	if m.pickerPos >= end {
		top = m.pickerPos
		end = min(top+visible, len(stations))
	}
	for i, station := range stations[top:end] {
		i += top
		marker := "  "
		if i == m.pickerPos {
			marker = "› "
		}
		color := lipgloss.Color("245")
		if len(station.FamilyIDs) > 0 {
			if family, ok := m.feedIndexes.FamilyByID[station.FamilyIDs[0]]; ok {
				color = lipgloss.Color(family.RendererColor)
			}
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(color).Render(marker+station.Name))
		if len(station.FamilyIDs) > 1 {
			families := make([]string, 0, len(station.FamilyIDs))
			for _, familyID := range station.FamilyIDs {
				name := familyID
				if family, ok := m.feedIndexes.FamilyByID[familyID]; ok && family.DisplayName != "" {
					name = family.DisplayName
				}
				families = append(families, name)
			}
			lines = append(lines, lipgloss.NewStyle().Foreground(color).Render("    ↳ "+strings.Join(families, " · ")))
		}
	}
	return append(lines, "", fmt.Sprintf("  %d–%d of %d · ↑↓ navigate · Enter select · Esc cancel", top+1, end, len(stations)))
}

func (m Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "up", "k":
		m.pickerPos = max(m.pickerPos-1, 0)
	case "down", "j":
		m.pickerPos = min(m.pickerPos+1, max(len(m.filteredStations())-1, 0))
	case "tab":
		m.picker = false
	case "enter":
		stations := m.filteredStations()
		if m.pickerPos < len(stations) {
			if m.focus == focusFrom {
				m.fromStation = stations[m.pickerPos].ID
				m.focus = focusTo
			} else {
				m.toStation = stations[m.pickerPos].ID
			}
			m.picker = false
			m.routeSeq++
			m.renderSeq++
			return m, tea.Batch(m.renderCmd(), m.routeCmd())
		}
	default:
		if len([]rune(value)) == 1 && value >= " " && value != "?" {
			m.search += value
			m.pickerPos = 0
			m.pickerTop = 0
		}
	}
	return m, nil
}

func dimLine(value string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(stripANSI(value))
}

func (m Model) hudText() string {
	zoom := fmt.Sprintf("z:%.1f", m.zoom)
	coords := fmt.Sprintf("%.4f°N  %.4f°E", m.lat, m.lon)
	scale := zoomToScale(int(math.Floor(m.zoom)))
	endpoints := fmt.Sprintf("FROM:%s TO:%s", m.endpointName(m.fromStation), m.endpointName(m.toStation))
	return strings.Join([]string{zoom, "N↑", coords, scale, m.dataStatus(), endpoints, "? help"}, " │ ")
}

func (m Model) sidebarWidth() int {
	if m.width < 48 {
		return 0
	}
	return min(28, max(m.width-24, 0))
}

func (m Model) sidebarLines(height, width int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("109"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	lines := []string{accent.Render(" ENDPOINTS"), "", m.endpointLine("FROM", m.fromStation, m.focus == focusFrom), m.endpointLine("TO", m.toStation, m.focus == focusTo), ""}
	if m.route.Message != "" {
		lines = append(lines, dim.Render(" "+m.routeSummary()), "")
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
			lines = append(lines, accent.Render(" Journey"))
			if m.focus == focusMap {
				nearest := render.NearestStation(m.feedIndexes, m.viewport(), m.cursor)
				if nearest != "" {
					lines = append(lines, dim.Render(" Cursor: "+m.endpointName(nearest)))
				}
			}
			lines = append(lines, dim.Render(" Choose endpoints with Enter"))
		}
	}
	if m.fromStation != "" && m.fromStation == m.toStation {
		lines = append(lines, dim.Render(" Same endpoint selected"))
	} else if m.fromStation != "" && m.toStation != "" {
		lines = append(lines, dim.Render(" Route ready · highlighted on map"))
		if m.route.Status == gtfs.RouteReady {
			for i, step := range m.route.Steps {
				name := step.FamilyName
				if name == "" {
					name = step.FamilyID
				}
				lines = append(lines, dim.Render(fmt.Sprintf(" %d. %s → %s", i+1, name, m.endpointName(step.ToStationID))))
			}
		}
	}
	lines = append(lines, "", dim.Render("Tab focus · Enter picker"))
	for i := range lines {
		lines[i] = padDisplay(truncateDisplay(lines[i], width), width)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func (m Model) endpointLine(label, stationID string, focused bool) string {
	marker := "  "
	if focused {
		marker = "> "
	}
	return marker + label + ": " + m.endpointName(stationID)
}

func (m Model) endpointName(stationID string) string {
	if stationID == "" {
		return "—"
	}
	if station, ok := m.feedIndexes.StationByID[stationID]; ok {
		if strings.TrimSpace(station.Name) != "" {
			return station.Name
		}
	}
	return stationID
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
	plain := stripANSI(value)
	runes := []rune(plain)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
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
	if m.feedState == FeedStateReady {
		snapshot := m.feedIndexes
		indexes = &snapshot
	}
	return func() tea.Msg {
		frame := render.Render(render.RenderRequest{
			DB:     cache,
			Lat:    lat,
			Lon:    lon,
			Zoom:   zoom,
			PixelW: pixelW,
			PixelH: pixelH,
			GTFS:   indexes,
			Route:  routePtr(m.route),
			Cursor: &m.cursor,
		})
		return frameReadyMsg{seq: seq, frame: frame}
	}
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
