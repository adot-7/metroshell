package tiles

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	_ "modernc.org/sqlite"
)

// DB wraps an MBTiles SQLite file.
type DB struct {
	db    *sql.DB
	stmt  *sql.Stmt // prepared query for tile fetch
	mu    sync.Mutex
	cache map[string][]byte // raw decoded tile bytes
}

// Open opens an MBTiles file for reading.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		log.Debug("couldnt open db")
		return nil, fmt.Errorf("open mbtiles: %w", err)
	}
	// Pre-verify the schema
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM tiles").Scan(&count); err != nil {
		log.Debug("couldnt select from tiles db")

		return nil, fmt.Errorf("mbtiles schema check failed: %w", err)
	}

	// Prepare the query once
	// NOTE: In MBTiles, tile_row is in TMS order (y=0 at south).
	// We flip it: tms_y = (1 << zoom_level) - 1 - xyz_y
	stmt, err := db.Prepare(`
        SELECT tile_data FROM tiles
        WHERE zoom_level = ? AND tile_column = ? AND tile_row = ?
    `)
	if err != nil {
		log.Debug("couldnt select tile data db")

		return nil, fmt.Errorf("prepare tile query: %w", err)
	}

	return &DB{
		db:    db,
		stmt:  stmt,
		cache: make(map[string][]byte),
	}, nil
}

// ReadTile reads the raw (gunzipped) MVT bytes for a tile at (z, x, y) in XYZ convention.
// Returns nil if the tile doesn't exist in the file.
func (d *DB) ReadTile(z, x, y int) ([]byte, error) {

	key := fmt.Sprintf("%d/%d/%d", z, x, y)
	log.Debug("reading tile for ", z, x, y)

	d.mu.Lock()
	if cached, ok := d.cache[key]; ok {
		d.mu.Unlock()
		return cached, nil
	}
	d.mu.Unlock()

	// Convert XYZ y to TMS y (flip Y axis)
	tmsY := (1 << z) - 1 - y

	var raw []byte
	err := d.stmt.QueryRow(z, x, tmsY).Scan(&raw)
	if err == sql.ErrNoRows {
		log.Debug("tile doesn't exist")

		return nil, nil // tile doesn't exist (ocean, or outside bbox)
	}
	if err != nil {
		log.Debug("query tile", key, err)

		return nil, fmt.Errorf("query tile %s: %w", key, err)
	}

	// Tile data in MBTiles is gzip-compressed.
	// Detect: gzip magic bytes are 0x1f, 0x8b
	decoded, err := gunzip(raw)
	if err != nil {
		// Might not be gzipped (some older files store raw protobuf)
		decoded = raw
	}

	d.mu.Lock()
	d.cache[key] = decoded
	d.mu.Unlock()

	return decoded, nil
}

func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	return buf.Bytes(), err
}
func (d *DB) ReadMetadata() (float64, float64) {
	log.Debug("READMETA DATA FUNCTION CALLED: ")

	var res string
	err := d.db.QueryRow("SELECT value from metadata where name=\"center\"").Scan(&res)
	if err != nil {
		log.Debug("error metadata: ", err)
	}
	log.Debug("metadata: ", res)
	result := strings.Split(res, ",")
	lon, err := strconv.ParseFloat(result[0], 64)
	if err != nil {
		return 0, 0
	}
	lat, err := strconv.ParseFloat(result[1], 64)
	if err != nil {
		return 0, 0
	}

	return lon, lat
}

// Close releases the database.
func (d *DB) Close() error {
	d.stmt.Close()
	return d.db.Close()
}
