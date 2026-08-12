package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	bm "charm.land/wish/v2/bubbletea"
	lm "charm.land/wish/v2/logging"

	cssh "github.com/charmbracelet/ssh"

	"charm.land/wish/v2"

	"github.com/charmbracelet/log"

	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/render"
	"github.com/adot-7/metroshell/internal/tiles"
)

var initlon, initlat float64

// 77.223500,28.521500
func main() {
	addr := flag.String("addr", ":2222", "SSH server listen address")
	hostKey := flag.String("host-key", "ssh_host_ed25519_key", "Path to SSH host key")
	tilesPath := flag.String("tiles", "mapdata/delhi-ncr.mbtiles", "Path to .mbtiles file")
	flag.Parse()

	db, err := tiles.Open(*tilesPath)
	if err != nil {
		log.Fatalf("Failed to open MBTiles %q: %v", *tilesPath, err)
	}
	defer db.Close()
	initlon, initlat = db.ReadMetadata()
	if initlon == 0 || initlat == 0 {
		initlon = 77.2090
		initlat = 28.6139
	}

	// Shared TileCache — one MVT parse per tile, reused across all SSH sessions.
	cache := render.NewTileCache(db)

	s, err := wish.NewServer(
		wish.WithAddress(*addr),
		wish.WithHostKeyPath(*hostKey),
		wish.WithMiddleware(
			bm.Middleware(makeHandler(cache)),
			lm.Middleware(),
		),
	)
	if err != nil {
		log.Fatalf("Failed to create wish server: %v", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Infof("Starting SSH server on %s", *addr)
	log.Infof("Connect with: ssh <host> -p %s", portOf(*addr))

	go func() {
		if err := s.ListenAndServe(); err != nil && !errors.Is(err, cssh.ErrServerClosed) {
			log.Errorf("Server error: %v", err)
			done <- syscall.SIGTERM
		}
	}()

	<-done
	log.Info("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, cssh.ErrServerClosed) {
		log.Errorf("Shutdown error: %v", err)
	}
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return port
}

// makeHandler returns a BubbleTea handler that creates a fresh sshModel per
// connection but shares the TileCache across all sessions.
func makeHandler(cache *render.TileCache) bm.Handler {
	return func(s cssh.Session) (tea.Model, []tea.ProgramOption) {
		return newSSHModel(cache), []tea.ProgramOption{}
	}
}

// ── Model ──────────────────────────────────────────────────────────────────

type sshModel struct {
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

type sshFrameReadyMsg string

func newSSHModel(cache *render.TileCache) sshModel {
	return sshModel{
		cache:  cache,
		lat:    initlat,
		lon:    initlon,
		zoom:   12,
		status: "Waiting for terminal size...",
	}
}

func (m sshModel) Init() tea.Cmd { return nil }

func (m sshModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			if m.zoom < 15.9 {
				m.zoom += 0.1
				m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
				return m, m.renderCmd()
			}
			return m, nil
		case tea.MouseWheelDown:
			if m.zoom > 5.1 {
				m.zoom -= 0.1
				m.status = fmt.Sprintf("lat=%.4f lon=%.4f z=%.1f", m.lat, m.lon, m.zoom)
				return m, m.renderCmd()
			}
			return m, nil
		}
	case sshFrameReadyMsg:
		m.frame = string(msg)
		return m, nil
	}
	return m, nil
}

func (m sshModel) View() tea.View {
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
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	var hudStyled string
	if m.status == "Rendering..." {
		hudStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(hudText)
	} else {
		hudStyled = dim.Render(hudText)
	}
	bottom := bdr.Render("╰─ ") + hudStyled + bdr.Render(" "+strings.Repeat("─", padLen)+"─╯")
	result := top + "\n" + framed.String() + bottom
	view := tea.NewView(result)
	view.AltScreen = true
	return view
}

func (m sshModel) hudText() string {
	zoom := fmt.Sprintf("z:%.1f", m.zoom)
	coords := fmt.Sprintf("%.4f°N  %.4f°E", m.lat, m.lon)
	scale := zoomToScale(int(math.Floor(m.zoom)))
	parts := []string{zoom, "N↑", coords, scale, "? help"}
	return strings.Join(parts, " │ ")
}

func (m sshModel) helpContent() string {
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

func (m sshModel) renderCmd() tea.Cmd {
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
		return sshFrameReadyMsg(frame)
	}
}

func zoomToScale(zoom int) string {
	scales := map[int]string{
		5: "~500km", 6: "~250km", 7: "~125km",
		8: "~60km", 9: "~30km", 10: "~15km",
		11: "~7km", 12: "~3.5km", 13: "~1.8km",
		14: "~900m", 15: "~450m",
	}
	if s, ok := scales[zoom]; ok {
		return s
	}
	return ""
}
