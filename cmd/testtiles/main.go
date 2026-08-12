package main

import (
	"fmt"

	"github.com/adot-7/metroshell/internal/tiles"

	"github.com/charmbracelet/log"
)

func main() {
	db, err := tiles.Open("mapdata/delhi-ncr.mbtiles")
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	// Delhi center at zoom 12: approximately tile (2817, 1681) NOPE
	data, err := db.ReadTile(12, 2922, 2389)
	if err != nil {
		log.Fatal(err)
	}
	if data == nil {
		log.Debug("reading tile for ", data)

		fmt.Println("Tile not found — check your z/x/y values")
		return
	}
	fmt.Printf("Tile bytes: %d (first 4 bytes: %x %x %x %x)\n",
		len(data), data[0], data[1], data[2], data[3])
	// Expected: raw protobuf, first byte should be 0x0a (protobuf field 1, wire type 2)
}
