package geo

import (
	"math"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/maptile"
)

// TileRequest describes a tile that needs to be loaded and where it sits on screen.
type TileRequest struct {
	Z, X, Y int

	// Where does this tile's (0,0) pixel land on the braille pixel grid
	PixelOffsetX int
	PixelOffsetY int

	// How many braille pixels does one tile-space unit correspond to
	Scale float64
}

// Viewport holds the current view state.
type Viewport struct {
	Lat, Lon float64
	Zoom     float64
	// Braille pixel dimensions of the display
	PixelW, PixelH int
}

// ComputeTiles returns all tiles needed to fill this viewport,
// along with their pixel offsets and scale.
func (v Viewport) ComputeTiles() []TileRequest {
	// Step 1: Find the fractional tile position of the viewport center.
	center := orb.Point{v.Lon, v.Lat} // orb uses lon,lat order

	// frac.X = e.g., 2928.713 (tile column + fraction within tile)
	// frac.Y = e.g., 1703.204 (tile row + fraction within tile)

	// Step 2: One tile in tile-space is 4096 units wide.
	// We want to know: how many screen pixels does one tile cover?
	// This is our "scale factor": screen_pixels_per_tile.
	// We'll choose it such that at minimum zoom we see a reasonable amount of the world.
	// For simplicity, start with: one tile covers 256 braille pixels.
	// You can make this zoom-dependent later.
	// const tilePixels = 256.0     // how many braille pixels wide one tile is
	// scale := tilePixels / 4096.0 // braille pixels per tile-space unit
	// Fractional zoom: tilePixels grows continuously, tiles load at integer boundary
	intZoom := int(math.Floor(v.Zoom))
	tilePixels := 256.0 * math.Pow(2.0, v.Zoom-math.Floor(v.Zoom))
	scale := tilePixels / 4096.0
	frac := maptile.Fraction(center, maptile.Zoom(intZoom))

	// Step 3: The center of the viewport is at (PixelW/2, PixelH/2) in pixel-space.
	// The center tile is at fractional position (frac.X, frac.Y).
	// The center tile's (0,0) point is at:
	//   offsetX = centerPixelX - fracX_within_tile * tilePixels
	// where fracX_within_tile is the fractional part of frac.X.

	// Integer tile indices for the center tile
	centerTileX := int(math.Floor(frac[0]))
	centerTileY := int(math.Floor(frac[1]))
	// Fractional position within the center tile (how far into the tile is our center?)
	fracWithinX := frac[0] - math.Floor(frac[0]) // 0.0 to 1.0
	fracWithinY := frac[1] - math.Floor(frac[1]) // 0.0 to 1.0

	// In braille pixel space, the center of the screen is at:
	screenCenterX := v.PixelW / 2
	screenCenterY := v.PixelH / 2

	// The (0,0) corner of the center tile is at:
	centerTileOriginX := screenCenterX - int(fracWithinX*tilePixels)
	centerTileOriginY := screenCenterY - int(fracWithinY*tilePixels)

	// Step 4: Determine which tile range is needed.
	// We need tiles from -(screenW / tilePixels / 2) to +(screenW / tilePixels / 2)
	// around the center tile.
	tilesX := int(math.Ceil(float64(v.PixelW)/tilePixels)) + 1
	tilesY := int(math.Ceil(float64(v.PixelH)/tilePixels)) + 1

	maxTile := (1 << intZoom) // 2^zoom = number of tiles per row/col

	var requests []TileRequest
	for dy := -tilesY; dy <= tilesY; dy++ {
		for dx := -tilesX; dx <= tilesX; dx++ {
			tileX := centerTileX + dx
			tileY := centerTileY + dy

			// Wrap X (the world is cylindrical in X)
			tileX = ((tileX % maxTile) + maxTile) % maxTile
			// Clamp Y (the world is not cylindrical in Y — there's no pole wrapping)
			if tileY < 0 || tileY >= maxTile {
				continue
			}

			// Where does this tile's (0,0) pixel land on screen?
			offsetX := centerTileOriginX + dx*int(tilePixels)
			offsetY := centerTileOriginY + dy*int(tilePixels)

			// Quick visibility check: is any part of this tile on screen?
			if offsetX+int(tilePixels) < 0 || offsetX >= v.PixelW {
				continue
			}
			if offsetY+int(tilePixels) < 0 || offsetY >= v.PixelH {
				continue
			}

			requests = append(requests, TileRequest{
				Z: intZoom, X: tileX, Y: tileY,
				PixelOffsetX: offsetX,
				PixelOffsetY: offsetY,
				Scale:        scale,
			})
		}
	}
	return requests
}

// PanAmount returns how much to move lat/lon for one keypress at the given zoom level.
// Larger zoom = smaller pan (you're more zoomed in).
func PanAmount(zoom float64) float64 {
	return 0.05 * math.Pow(0.5, float64(zoom-10))
}
