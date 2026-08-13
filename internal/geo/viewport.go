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

// Bounds is a geographic rectangle in longitude/latitude order. It is kept
// independent of a viewport so callers can build deterministic fits from
// prepared route geometry without involving the renderer.
type Bounds struct {
	MinLon, MinLat float64
	MaxLon, MaxLat float64
}

// NewBounds returns the bounds of finite geographic points. The bool is false
// when no usable point was supplied.
func NewBounds(points []orb.Point) (Bounds, bool) {
	var bounds Bounds
	found := false
	for _, point := range points {
		if math.IsNaN(point[0]) || math.IsInf(point[0], 0) || math.IsNaN(point[1]) || math.IsInf(point[1], 0) {
			continue
		}
		if !found {
			bounds = Bounds{MinLon: point[0], MinLat: point[1], MaxLon: point[0], MaxLat: point[1]}
			found = true
			continue
		}
		bounds.MinLon = min(bounds.MinLon, point[0])
		bounds.MinLat = min(bounds.MinLat, point[1])
		bounds.MaxLon = max(bounds.MaxLon, point[0])
		bounds.MaxLat = max(bounds.MaxLat, point[1])
	}
	return bounds, found
}

const (
	minSupportedZoom = 5.1
	maxSupportedZoom = 15.9
	maxMercatorLat   = 85.0511287798066
)

// FitBounds deterministically fits geographic bounds inside a map viewport.
// Padding is measured in braille pixels and is applied independently on both
// axes. Invalid bounds or a terminal with no usable map area return false and
// leave the fallback viewport untouched.
func FitBounds(bounds Bounds, pixelW, pixelH, padding int, fallback Viewport) (Viewport, bool) {
	if pixelW <= 0 || pixelH <= 0 || math.IsNaN(bounds.MinLon) || math.IsNaN(bounds.MinLat) ||
		math.IsNaN(bounds.MaxLon) || math.IsNaN(bounds.MaxLat) || bounds.MinLon > bounds.MaxLon || bounds.MinLat > bounds.MaxLat {
		return fallback, false
	}
	usableW := pixelW - 2*max(padding, 0)
	usableH := pixelH - 2*max(padding, 0)
	if usableW < 2 || usableH < 2 {
		return fallback, false
	}
	minLat := clamp(bounds.MinLat, -maxMercatorLat, maxMercatorLat)
	maxLat := clamp(bounds.MaxLat, -maxMercatorLat, maxMercatorLat)
	minX, minY := normalizedMercator(bounds.MinLon, maxLat)
	maxX, maxY := normalizedMercator(bounds.MaxLon, minLat)
	dx := max(maxX-minX, 1e-12)
	dy := max(maxY-minY, 1e-12)
	zoomX := math.Log2(float64(usableW) / (256 * dx))
	zoomY := math.Log2(float64(usableH) / (256 * dy))
	zoom := clamp(min(zoomX, zoomY), minSupportedZoom, maxSupportedZoom)
	centerX := (minX + maxX) / 2
	centerY := (minY + maxY) / 2
	centerLon := centerX*360 - 180
	centerLat := inverseMercator(centerY)
	return Viewport{
		Lat:  clamp(centerLat, -maxMercatorLat, maxMercatorLat),
		Lon:  clamp(centerLon, -180, 180),
		Zoom: zoom, PixelW: pixelW, PixelH: pixelH,
	}, true
}

func normalizedMercator(lon, lat float64) (float64, float64) {
	lat = clamp(lat, -maxMercatorLat, maxMercatorLat)
	x := (lon + 180) / 360
	sinLat := math.Sin(lat * math.Pi / 180)
	y := 0.5 - math.Log((1+sinLat)/(1-sinLat))/(4*math.Pi)
	return x, y
}

func inverseMercator(y float64) float64 {
	return 180 / math.Pi * math.Atan(math.Sinh(math.Pi*(1-2*y)))
}

func clamp(value, low, high float64) float64 { return min(max(value, low), high) }

// Project converts a longitude/latitude point to the viewport's braille pixel
// space. It uses the same fractional-zoom Web Mercator math as ComputeTiles,
// with the viewport center fixed at PixelW/2, PixelH/2.
func (v Viewport) Project(point orb.Point) (x, y float64) {
	intZoom := int(math.Floor(v.Zoom))
	tilePixels := 256.0 * math.Pow(2.0, v.Zoom-math.Floor(v.Zoom))
	center := maptile.Fraction(orb.Point{v.Lon, v.Lat}, maptile.Zoom(intZoom))
	projected := maptile.Fraction(point, maptile.Zoom(intZoom))

	// Longitude wraps around the world. Pick the shortest displacement so an
	// edge crossing does not draw the line across the whole screen.
	worldTiles := float64(uint64(1) << uint(intZoom))
	dx := projected[0] - center[0]
	if dx > worldTiles/2 {
		dx -= worldTiles
	} else if dx < -worldTiles/2 {
		dx += worldTiles
	}
	return float64(v.PixelW)/2 + dx*tilePixels,
		float64(v.PixelH)/2 + (projected[1]-center[1])*tilePixels
}

// Unproject converts a point in the viewport's braille pixel space back to a
// longitude/latitude point. It is the inverse of Project (within floating
// point precision), and deliberately uses the same fractional-zoom scale.
func (v Viewport) Unproject(x, y float64) orb.Point {
	intZoom := int(math.Floor(v.Zoom))
	tilePixels := 256.0 * math.Pow(2.0, v.Zoom-math.Floor(v.Zoom))
	worldTiles := float64(uint64(1) << uint(intZoom))
	center := maptile.Fraction(orb.Point{v.Lon, v.Lat}, maptile.Zoom(intZoom))

	worldX := center[0] + (x-float64(v.PixelW)/2)/tilePixels
	worldY := center[1] + (y-float64(v.PixelH)/2)/tilePixels
	worldX = math.Mod(worldX, worldTiles)
	if worldX < 0 {
		worldX += worldTiles
	}
	longitude := worldX/worldTiles*360 - 180

	// Invert Web Mercator's normalized Y coordinate. Clamp the normalized
	// value so a tiny terminal or a point on the edge cannot produce NaN.
	worldY = min(max(worldY/worldTiles, 0), 1)
	n := math.Pi * (1 - 2*worldY)
	latitude := math.Atan(math.Sinh(n)) * 180 / math.Pi
	return orb.Point{longitude, latitude}
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
