package main

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/log"

	"github.com/adot-7/metroshell/internal/app"
	"github.com/adot-7/metroshell/internal/render"
	"github.com/adot-7/metroshell/internal/tiles"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: metroshell <path-to.mbtiles> [path-to-gtfs-directory-or-zip]")
	}
	gtfsPath := ""
	if len(os.Args) > 2 {
		gtfsPath = os.Args[2]
	}
	db, err := tiles.Open(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to open MBTiles: %v", err)
	}
	defer db.Close()

	lon, lat := db.ReadMetadata()
	// if lon == 0 || lat == 0 {
	// 	lon = 77.2199
	// 	lat = 28.6329
	// }
	lon = 77.2199
	lat = 28.6329

	f, err := os.OpenFile("trip.log", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	log.SetOutput(f)
	log.SetLevel(log.DebugLevel)
	log.SetReportCaller(true)

	p := tea.NewProgram(app.NewWithConfig(render.NewTileCache(db), lat, lon, app.Config{GTFSPath: gtfsPath}))
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
