package main

import (
	"fmt"
	"log"

	"github.com/adot-7/metroshell/internal/tiles"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/mvt"
)

func main() {
	db, err := tiles.Open("mapdata/delhi-ncr.mbtiles")
	if err != nil {
		log.Fatal(err)
	}

	data, err := db.ReadTile(12, 2922, 2389) // Delhi center at z12
	if err != nil {
		log.Fatal(err)
	}
	if data == nil {
		log.Fatal("tile not found")
	}

	layers, err := mvt.Unmarshal(data) // data is already gunzipped from our ReadTile
	if err != nil {
		log.Fatal(err)
	}

	for _, layer := range layers {
		fmt.Printf("Layer: %-25s features: %d  extent: %d\n",
			layer.Name, len(layer.Features), layer.Extent)

		// Show first feature's geometry type and first few coords
		if len(layer.Features) > 0 {
			f := layer.Features[0]
			fmt.Printf("  First feature type: %T\n", f.Geometry)
			fmt.Printf("  Properties: %v\n", f.Properties)

			// Show coordinates
			switch g := f.Geometry.(type) {
			case orb.LineString:
				fmt.Printf("  Points: %d, first: %v\n", len(g), g[0])
			case orb.Polygon:
				fmt.Printf("  Outer ring points: %d\n", len(g[0]))
			case orb.Point:
				fmt.Printf("  Point: %v\n", g)
			}
		}
	}
}
