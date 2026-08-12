package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	bm "charm.land/wish/v2/bubbletea"
	lm "charm.land/wish/v2/logging"
	"github.com/charmbracelet/log"
	cssh "github.com/charmbracelet/ssh"

	"github.com/adot-7/metroshell/internal/app"
	"github.com/adot-7/metroshell/internal/render"
	"github.com/adot-7/metroshell/internal/tiles"
)

func main() {
	addr := flag.String("addr", ":2222", "SSH server listen address")
	hostKey := flag.String("host-key", "ssh_host_ed25519_key", "Path to SSH host key")
	tilesPath := flag.String("tiles", "mapdata/delhi-ncr.mbtiles", "Path to .mbtiles file")
	gtfsPath := flag.String("gtfs", "", "Path to a GTFS directory or ZIP archive")
	flag.Parse()

	db, err := tiles.Open(*tilesPath)
	if err != nil {
		log.Fatalf("Failed to open MBTiles %q: %v", *tilesPath, err)
	}
	defer db.Close()
	lon, lat := db.ReadMetadata()
	if lon == 0 || lat == 0 {
		lon = 77.2090
		lat = 28.6139
	}

	cache := render.NewTileCache(db)
	s, err := wish.NewServer(
		wish.WithAddress(*addr),
		wish.WithHostKeyPath(*hostKey),
		wish.WithMiddleware(
			bm.Middleware(makeHandler(cache, lat, lon, app.Config{GTFSPath: *gtfsPath})),
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

// makeHandler creates a map model per SSH session while sharing the tile cache.
func makeHandler(cache *render.TileCache, lat, lon float64, configs ...app.Config) bm.Handler {
	config := app.Config{}
	if len(configs) > 0 {
		config = configs[0]
	}
	return func(cssh.Session) (tea.Model, []tea.ProgramOption) {
		return app.NewWithConfig(cache, lat, lon, config), []tea.ProgramOption{}
	}
}
