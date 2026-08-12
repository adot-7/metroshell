package render

import (
	"fmt"
	"sync"

	"github.com/adot-7/metroshell/internal/tiles"

	"github.com/paulmach/orb/encoding/mvt"
)

// TileCache wraps a tiles.DB and adds a second cache layer for parsed MVT
type TileCache struct {
	db  *tiles.DB
	mu  sync.RWMutex
	mvt map[string]mvt.Layers // key: "z/x/y"
}

// NewTileCache creates a TileCache backed by an open MBTiles database.
func NewTileCache(db *tiles.DB) *TileCache {
	return &TileCache{
		db:  db,
		mvt: make(map[string]mvt.Layers),
	}
}

// ReadLayers returns the parsed MVT layers for a tile at (z, x, y).
// Returns (nil, nil) if the tile doesn't exist in the file.
// Parsed results are cached indefinitely — the mbtiles source is read-only.
func (c *TileCache) ReadLayers(z, x, y int) (mvt.Layers, error) {
	key := fmt.Sprintf("%d/%d/%d", z, x, y)

	c.mu.RLock()
	if layers, ok := c.mvt[key]; ok {
		c.mu.RUnlock()
		return layers, nil
	}
	c.mu.RUnlock()

	// Cache miss — read raw bytes and parse.
	data, err := c.db.ReadTile(z, x, y)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil // tile not in file (ocean, outside bbox)
	}

	layers, err := mvt.Unmarshal(data)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.mvt[key] = layers
	c.mu.Unlock()

	return layers, nil
}

// Close closes the underlying tile database.
func (c *TileCache) Close() error {
	return c.db.Close()
}
