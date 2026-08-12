package render

import (
	"math"
	"strconv"
	"strings"

	"github.com/adot-7/metroshell/internal/braille"
	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/gtfs"
	"github.com/adot-7/metroshell/internal/style"

	"github.com/charmbracelet/log"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/paulmach/orb/simplify"
)

// RenderRequest bundles everything needed for one frame.
type RenderRequest struct {
	DB       *TileCache // uses the MVT-layer cache — not tiles.DB directly
	Lat, Lon float64
	Zoom     float64
	PixelW   int // braille pixel width  (= (termCols-2) * 2)
	PixelH   int // braille pixel height (= (termRows-2) * 4)
	// GTFS is an immutable, renderer-facing snapshot. Render never joins raw
	// feed tables; indexes are prepared by the asynchronous loader.
	GTFS *gtfs.Indexes
	// Cursor is an optional geographic map cursor drawn above the metro layer.
	Cursor *orb.Point
}

// Label holds a text label to be written into the braille buffer's text overlay.
type Label struct {
	Text  string
	ColX  int
	RowY  int
	Color int
}

// legendEntry is the small, renderer-ready portion of a GTFS line that is
// needed by the fixed map legend. Names are kept separate from the swatch so
// the name remains readable as one unbroken terminal string.
type legendEntry struct {
	Name  string
	Color int
}

type legendPlacement struct {
	Entry legendEntry
	ColX  int
	RowY  int
	Width int
}

const (
	selectedStationColor = 226
	stationHoverRadius   = 7.0
)

func findLayer(layers mvt.Layers, name string) *mvt.Layer {
	for _, layer := range layers {
		if layer != nil && layer.Name == name {
			return layer
		}
	}
	return nil
}

// Render builds a full frame string from the given request.
func Render(req RenderRequest) string {
	if req.PixelW < 0 {
		req.PixelW = 0
	}
	if req.PixelH < 0 {
		req.PixelH = 0
	}
	buf := braille.New(req.PixelW/2, req.PixelH/4)
	buf.Clear()

	vp := geo.Viewport{
		Lat: req.Lat, Lon: req.Lon, Zoom: req.Zoom,
		PixelW: req.PixelW, PixelH: req.PixelH,
	}
	tileRequests := vp.ComputeTiles()

	layerOrder := []string{
		"landcover", "landuse", "water", "waterway",
		"boundary", "transportation", "transportation_name",
		"building", "poi", "place",
	}

	var labels []Label
	seenRoadLabels := make(map[string]bool)

	isFirstTile := true
	for _, req2 := range tileRequests {
		if req.DB == nil {
			break
		}
		// ReadLayers returns cached parsed MVT — mvt.Unmarshal only runs once
		// per tile position for the lifetime of this TileCache session.
		layers, err := req.DB.ReadLayers(req2.Z, req2.X, req2.Y)
		if err != nil || layers == nil {
			continue
		}
		if isFirstTile {
			for _, l := range layers {
				if l != nil {
					log.Debugf("Layer:%s (%d features)", l.Name, len(l.Features))
				}
			}
			isFirstTile = false
		}

		for _, layerName := range layerOrder {
			layer := findLayer(layers, layerName)
			if layer == nil {
				continue
			}

			tolerance := 4096.0 / float64(256) * 0.5
			simplifier := simplify.DouglasPeucker(tolerance)

			for _, feature := range layer.Features {
				class, _ := feature.Properties["class"].(string)

				var st style.LayerStyle
				var ok bool
				if layerName == "poi" {
					subclass, _ := feature.Properties["subclass"].(string)
					if subclass != "" {
						st, ok = style.StyleFor(layerName, class+"/"+subclass, int(math.Floor(req.Zoom)))
					}
					if !ok {
						st, ok = style.StyleFor(layerName, class, int(math.Floor(req.Zoom)))
					}
				} else {
					st, ok = style.StyleFor(layerName, class, int(math.Floor(req.Zoom)))
				}
				if !ok {
					continue
				}

				simplified := simplifier.Simplify(feature.Geometry)
				drawGeometry(buf, simplified, req2, st)

				if st.DrawLabel {
					var text string
					if st.LabelSymbol != "" {
						text = st.LabelSymbol
					} else {
						text = featureName(feature.Properties)
					}
					if text != "" {
						isRoadLayer := layerName == "transportation" ||
							layerName == "transportation_name"
						if isRoadLayer && seenRoadLabels[text] {
							continue
						}
						if tx, ty, ok2 := featurePoint(simplified); ok2 {
							px, py := tileToPixel(tx, ty, req2)
							col, row := px/2, py/4
							labels = append(labels, Label{
								Text:  text,
								ColX:  col,
								RowY:  row,
								Color: st.LabelColor,
							})
							if isRoadLayer {
								seenRoadLabels[text] = true
							}
						}
					}
				}
			}
		}
	}

	if req.GTFS != nil {
		selected := ""
		if req.Cursor != nil {
			selected = nearestStation(*req.GTFS, vp, *req.Cursor)
		}
		drawGTFSOverlay(buf, *req.GTFS, vp, selected)
	}

	termW := req.PixelW / 2
	termH := req.PixelH / 4
	writeLabelsToBuffer(buf, labels, termW, termH)
	if req.GTFS != nil {
		writeLegendToBuffer(buf, legendEntries(*req.GTFS), termW, termH)
	}
	if req.Cursor != nil {
		drawCursor(buf, *req.Cursor, vp)
	}
	return buf.Render()
}

// drawGTFSOverlay draws the complete deterministic transit layer above the
// base map. Lines are emitted in OrderedLines/Shapes order, followed by their
// shape placements in contract order. Drawing stations last keeps the
// passenger-facing points visible over their route geometry.
func drawGTFSOverlay(buf *braille.Buffer, indexes gtfs.Indexes, vp geo.Viewport, selectedStation string) {
	for _, line := range indexes.OrderedLines {
		color := lineRenderColor(line)
		for _, shape := range line.Shapes {
			drawGeoLine(buf, shape.Geometry, vp, color)
		}
	}
	for _, line := range indexes.OrderedLines {
		color := lineRenderColor(line)
		for _, shape := range line.Shapes {
			for _, placement := range shape.Placements {
				drawStation(buf, placement.Point, vp, color, placement.StationID == selectedStation)
			}
		}
	}
}

func legendEntries(indexes gtfs.Indexes) []legendEntry {
	entries := make([]legendEntry, 0, len(indexes.OrderedLines))
	for _, line := range indexes.OrderedLines {
		if !lineHasRenderableShape(line) {
			continue
		}
		name := strings.TrimSpace(line.DisplayName)
		if name == "" {
			name = strings.TrimSpace(line.ID)
		}
		if name == "" {
			name = "Unnamed line"
		}
		entries = append(entries, legendEntry{Name: name, Color: lineRenderColor(line)})
	}
	return entries
}

func lineHasRenderableShape(line gtfs.Line) bool {
	for _, shape := range line.Shapes {
		if len(shape.Geometry) >= 2 || len(shape.Placements) > 0 {
			return true
		}
	}
	return false
}

// layoutLegend lays out every entry in a bounded column-major grid. The
// number of columns grows only when the terminal cannot fit one entry per row;
// this keeps the ordinary layout easy to scan while still making small but
// usable terminals deterministic. A terminal too small for even a compact
// swatch returns an empty layout instead of overflowing the map frame.
func layoutLegend(entries []legendEntry, width, height int) []legendPlacement {
	if len(entries) == 0 || width < 3 || height < 1 {
		return nil
	}
	columns := (len(entries) + height - 1) / height
	maxColumns := width / 3
	if columns > maxColumns {
		if len(entries) > maxColumns*height {
			return nil
		}
		columns = maxColumns
	}
	columnWidth := width / columns
	rows := (len(entries) + columns - 1) / columns
	if columnWidth < 3 || rows > height {
		return nil
	}

	placements := make([]legendPlacement, 0, len(entries))
	for i, entry := range entries {
		column := i / height
		row := i % height
		if column >= columns {
			return nil
		}
		placements = append(placements, legendPlacement{
			Entry: entry,
			ColX:  column * columnWidth,
			RowY:  row,
			Width: columnWidth,
		})
	}
	return placements
}

func writeLegendToBuffer(buf *braille.Buffer, entries []legendEntry, termW, termH int) {
	for _, placement := range layoutLegend(entries, termW, termH) {
		// The swatch is the only colored character. Keeping names uncolored
		// avoids ANSI escapes between every rune and makes the fixed legend
		// readable on both dark and light terminal themes.
		buf.SetText(placement.ColX, placement.RowY, '●', placement.Entry.Color)
		nameWidth := placement.Width - 2
		if nameWidth <= 0 {
			continue
		}
		name := truncateRunes(placement.Entry.Name, nameWidth)
		for i, r := range name {
			buf.SetText(placement.ColX+2+i, placement.RowY, r, 0)
		}
	}
}

func truncateRunes(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func lineRenderColor(line gtfs.Line) int {
	value := line.RendererColor
	if value == "" {
		value = line.Color
	}
	return routeColor(value)
}

func drawGeoLine(buf *braille.Buffer, geometry []orb.Point, vp geo.Viewport, color int) {
	if len(geometry) < 2 {
		return
	}
	xs := make([]int, len(geometry))
	ys := make([]int, len(geometry))
	for i, point := range geometry {
		x, y := vp.Project(point)
		xs[i], ys[i] = int(math.Round(x)), int(math.Round(y))
	}
	buf.DrawPolyline(xs, ys, color)
}

func drawStation(buf *braille.Buffer, point orb.Point, vp geo.Viewport, color int, selected bool) {
	x, y := vp.Project(point)
	px, py := int(math.Round(x)), int(math.Round(y))
	if selected {
		// Draw the accent first. The route-colored marker is composed last so
		// selecting a station cannot replace the route color in its cell.
		for _, offset := range [][2]int{{-2, 0}, {2, 0}, {0, -2}, {0, 2}} {
			buf.SetPixel(px+offset[0], py+offset[1], selectedStationColor)
		}
	}
	// A small cross is more legible than a single braille dot at low zoom and
	// remains an accessible, route-colored station marker.
	buf.SetPixel(px, py, color)
	buf.SetPixel(px-1, py, color)
	buf.SetPixel(px+1, py, color)
	buf.SetPixel(px, py-1, color)
	buf.SetPixel(px, py+1, color)
}

func nearestStation(indexes gtfs.Indexes, vp geo.Viewport, cursor orb.Point) string {
	type candidate struct {
		id    string
		point orb.Point
	}
	candidates := make([]candidate, 0, len(indexes.OrderedStations))
	seen := make(map[string]bool)
	for _, station := range indexes.OrderedStations {
		if station.ID == "" || seen[station.ID] {
			continue
		}
		seen[station.ID] = true
		candidates = append(candidates, candidate{id: station.ID, point: orb.Point{station.Longitude, station.Latitude}})
	}
	// Synthetic renderer snapshots may contain only line placements. Retain
	// that useful renderer contract without requiring View-time index joins.
	for _, line := range indexes.OrderedLines {
		for _, shape := range line.Shapes {
			for _, placement := range shape.Placements {
				if placement.StationID == "" || seen[placement.StationID] {
					continue
				}
				seen[placement.StationID] = true
				candidates = append(candidates, candidate{id: placement.StationID, point: placement.Point})
			}
		}
	}
	cursorX, cursorY := vp.Project(cursor)
	bestDistance := stationHoverRadius * stationHoverRadius
	selected := ""
	for _, station := range candidates {
		x, y := vp.Project(station.point)
		dx, dy := x-cursorX, y-cursorY
		distance := dx*dx + dy*dy
		if distance > bestDistance || (selected != "" && distance == bestDistance && station.id >= selected) {
			continue
		}
		bestDistance = distance
		selected = station.id
	}
	return selected
}

func drawCursor(buf *braille.Buffer, point orb.Point, vp geo.Viewport) {
	x, y := vp.Project(point)
	col, row := int(math.Floor(x))/2, int(math.Floor(y))/4
	// Text is deliberately composed last: the cursor remains visible when it
	// shares a cell with a metro station, route, or base-map label.
	buf.SetText(col, row, '◎', 226)
}

func routeColor(value string) int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return braille.RGBToXterm256(128, 128, 128)
	}
	raw, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return braille.RGBToXterm256(128, 128, 128)
	}
	return braille.RGBToXterm256(uint8(raw>>16), uint8(raw>>8), uint8(raw))
}

func featureName(props map[string]interface{}) string {
	for _, key := range []string{"name", "name:hi", "name:latin", "name:en", "name_en", "ref"} {
		if v, ok := props[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func writeLabelsToBuffer(buf *braille.Buffer, labels []Label, termW, termH int) {
	occupied := make(map[[2]int]bool)
	for _, l := range labels {
		if l.ColX < 0 || l.RowY < 0 || l.RowY >= termH {
			continue
		}
		maxLen := termW - l.ColX
		if maxLen <= 0 {
			continue
		}
		runes := []rune(l.Text)
		if len(runes) > maxLen {
			runes = runes[:maxLen]
		}
		collision := false
		for i := range runes {
			if occupied[[2]int{l.ColX + i, l.RowY}] {
				collision = true
				break
			}
		}
		if collision {
			continue
		}
		for i, r := range runes {
			col := l.ColX + i
			occupied[[2]int{col, l.RowY}] = true
			buf.SetText(col, l.RowY, r, l.Color)
		}
	}
}

func featurePoint(g orb.Geometry) (x, y float64, ok bool) {
	switch geom := g.(type) {
	case orb.Point:
		return geom[0], geom[1], true
	case orb.LineString:
		if len(geom) == 0 {
			return
		}
		mid := geom[len(geom)/2]
		return mid[0], mid[1], true
	case orb.MultiLineString:
		if len(geom) == 0 || len(geom[0]) == 0 {
			return
		}
		mid := geom[0][len(geom[0])/2]
		return mid[0], mid[1], true
	case orb.Polygon:
		if len(geom) == 0 || len(geom[0]) == 0 {
			return
		}
		ring := geom[0]
		var sx, sy float64
		for _, pt := range ring {
			sx += pt[0]
			sy += pt[1]
		}
		n := float64(len(ring))
		return sx / n, sy / n, true
	case orb.MultiPolygon:
		if len(geom) == 0 || len(geom[0]) == 0 || len(geom[0][0]) == 0 {
			return
		}
		ring := geom[0][0]
		var sx, sy float64
		for _, pt := range ring {
			sx += pt[0]
			sy += pt[1]
		}
		n := float64(len(ring))
		return sx / n, sy / n, true
	}
	return
}

func drawGeometry(buf *braille.Buffer, g orb.Geometry, req geo.TileRequest, st style.LayerStyle) {
	switch geom := g.(type) {
	case orb.LineString:
		if st.DrawLine {
			drawLineString(buf, geom, req, st.LineColor)
		}
	case orb.MultiLineString:
		if st.DrawLine {
			for _, ls := range geom {
				drawLineString(buf, ls, req, st.LineColor)
			}
		}
	case orb.Polygon:
		if st.DrawFill {
			drawPolygon(buf, geom, req, st.FillColor)
		}
		if st.DrawLine {
			drawLineString(buf, orb.LineString(geom[0]), req, st.LineColor)
		}
	case orb.MultiPolygon:
		for _, poly := range geom {
			if st.DrawFill {
				drawPolygon(buf, poly, req, st.FillColor)
			}
			if st.DrawLine {
				drawLineString(buf, orb.LineString(poly[0]), req, st.LineColor)
			}
		}
	case orb.Point:
		if st.DrawLine {
			px, py := tileToPixel(geom[0], geom[1], req)
			buf.SetPixel(px, py, st.LineColor)
		}
	}
}

func tileToPixel(tileX, tileY float64, req geo.TileRequest) (px, py int) {
	px = req.PixelOffsetX + int(tileX*req.Scale)
	py = req.PixelOffsetY + int(tileY*req.Scale)
	return
}

func drawLineString(buf *braille.Buffer, ls orb.LineString, req geo.TileRequest, color int) {
	if len(ls) < 2 {
		return
	}
	xs := make([]int, len(ls))
	ys := make([]int, len(ls))
	for i, pt := range ls {
		xs[i], ys[i] = tileToPixel(pt[0], pt[1], req)
	}
	buf.DrawPolyline(xs, ys, color)
}

func drawPolygon(buf *braille.Buffer, poly orb.Polygon, req geo.TileRequest, color int) {
	if len(poly) == 0 {
		return
	}
	ring := poly[0]
	xs := make([]int, len(ring))
	ys := make([]int, len(ring))
	for i, pt := range ring {
		xs[i], ys[i] = tileToPixel(pt[0], pt[1], req)
	}
	buf.FillPolygon(xs, ys, color)
	for _, hole := range poly[1:] {
		hxs := make([]int, len(hole))
		hys := make([]int, len(hole))
		for i, pt := range hole {
			hxs[i], hys[i] = tileToPixel(pt[0], pt[1], req)
		}
		buf.FillPolygon(hxs, hys, 0)
	}
}
