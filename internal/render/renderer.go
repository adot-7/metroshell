package render

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/adot-7/metroshell/internal/braille"
	"github.com/adot-7/metroshell/internal/geo"
	"github.com/adot-7/metroshell/internal/gtfs"
	"github.com/adot-7/metroshell/internal/sim"
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
	// Route is an optional prepared route. Rendering only draws its station
	// sequence; BFS and all graph work happen before Render is called.
	Route *gtfs.RouteResult
	// Trains is a detached, immutable simulator snapshot. Render does not
	// advance simulation state or retain this slice after the frame completes.
	Trains []sim.Train
	// ASCIIOnly selects the conservative consist glyphs for terminals that do
	// not reliably render Unicode block cars.
	ASCIIOnly bool
}

// Label holds a text label to be written into the braille buffer's text overlay.
type Label struct {
	Text  string
	ColX  int
	RowY  int
	Color int
}

const (
	selectedStationColor = 226
	stationHoverRadius   = 7.0
	// Beads are laid out in terminal-cell space, rather than map-pixel space.
	// A cadence of 2.25 cells and a minimum Chebyshev separation of 2 cells
	// leave one visibly empty cell between neighboring beads on straight runs.
	// The cadence is deliberately below a sparse waypoint treatment: rounded
	// short/curved runs keep their anchors and only lose candidates that would
	// crowd an already accepted bead. The dimmed network remains the sole
	// braille line rasterization.
	selectedMarkerCadenceCells = 2.25
	selectedMarkerMinGapCells  = 2
	selectedMarkerMaxBeads     = 512
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
	readyRoute := req.Route != nil && req.Route.Status == gtfs.RouteReady

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
				drawGeometry(buf, simplified, req2, st, readyRoute)

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
		drawGTFSOverlay(buf, *req.GTFS, vp, selected, req.Route)
		drawGTFSStations(buf, *req.GTFS, vp, selected, req.Route)
		if req.Route != nil && req.Route.Status == gtfs.RouteReady {
			// This is the sole selected-route pass. It emits only markers and
			// transfer rings from prepared, station-clipped associations; the
			// base network above remains the only route-line pass.
			drawSelectedRoute(buf, *req.GTFS, *req.Route, vp)
		}
		drawTrains(buf, *req.GTFS, req.Trains, vp, req.Route, req.ASCIIOnly)
	}

	termW := req.PixelW / 2
	termH := req.PixelH / 4
	occupied := writeLabelsToBuffer(buf, labels, termW, termH)
	if req.Cursor != nil {
		drawCursor(buf, *req.Cursor, vp, occupied)
	}
	return buf.Render()
}

// drawTrains is deliberately between transit geometry and text composition.
// It only accepts trains whose shape is present in the immutable GTFS view;
// malformed or stale feed/simulator associations are safely omitted.
func drawTrains(buf *braille.Buffer, indexes gtfs.Indexes, trains []sim.Train, vp geo.Viewport, route *gtfs.RouteResult, asciiOnly ...bool) {
	if len(trains) == 0 {
		return
	}
	shapeIDs := make(map[string]bool)
	for id := range indexes.Shapes {
		shapeIDs[id] = len(indexes.Shapes[id].Geometry) > 0
	}
	for _, shape := range indexes.OrderedShapes {
		shapeIDs[shape.ID] = len(shape.Geometry) > 0
	}
	// Synthetic renderer snapshots commonly provide only line shapes.
	for _, line := range indexes.OrderedLines {
		for _, shape := range line.Shapes {
			shapeIDs[shape.ShapeID] = shapeIDs[shape.ShapeID] || len(shape.Geometry) > 0
		}
	}
	for _, family := range indexes.OrderedFamilies {
		for _, shape := range family.Shapes {
			shapeIDs[shape.ShapeID] = shapeIDs[shape.ShapeID] || len(shape.Geometry) > 0
		}
	}

	selectedFamilies := make(map[string]bool)
	if route != nil && route.Status == gtfs.RouteReady {
		for _, familyID := range route.FamilyIDs {
			selectedFamilies[familyID] = true
		}
	}
	ordered := append([]sim.Train(nil), trains...)
	sort.SliceStable(ordered, func(i, j int) bool {
		iSelected := selectedFamilies[ordered[i].FamilyID]
		jSelected := selectedFamilies[ordered[j].FamilyID]
		if iSelected != jSelected {
			return !iSelected // selected trains compose last and win overlap.
		}
		return ordered[i].ID > ordered[j].ID
	})
	protected := trainProtectedCells(indexes, vp, buf.Width, buf.Height)
	occupied := make(map[[2]int]bool)
	ascii := len(asciiOnly) > 0 && asciiOnly[0]
	if buf.Width < 24 || buf.Height < 8 {
		ascii = true
	}
	for _, train := range ordered {
		if train.ID == "" || train.ShapeID == "" || !shapeIDs[train.ShapeID] ||
			math.IsNaN(train.Position.Lon) || math.IsNaN(train.Position.Lat) ||
			math.IsInf(train.Position.Lon, 0) || math.IsInf(train.Position.Lat, 0) {
			continue
		}
		color := trainRenderColor(indexes, train)
		x, y := vp.Project(orb.Point{train.Position.Lon, train.Position.Lat})
		px, py := int(math.Round(x)), int(math.Round(y))
		selected := selectedFamilies[train.FamilyID]
		dim := route != nil && route.Status == gtfs.RouteReady && !selected
		glyph := trainConsistGlyph(train, vp, selected, px/2, py/4, buf.Width, buf.Height, ascii)
		if glyph == "" {
			continue
		}
		cells := consistCells(px/2, py/4, glyph, trainConsistVertical(train, vp))
		cells = placeConsist(cells, train, vp, protected, occupied, buf.Width, buf.Height)
		center := [2]int{px / 2, py / 4}
		if len(cells) == 0 && !protected[center] && !occupied[center] {
			// At a viewport edge, anchor the consist at the nearest visible cell.
			cells = clipConsist(cells, px/2, py/4, glyph, trainConsistVertical(train, vp), buf.Width, buf.Height)
		}
		if len(cells) == 0 {
			continue
		}
		// Consists are the live foreground map layer. Labels and the cursor are
		// composed later, while station and transfer cells are reserved here.
		for _, cell := range cells {
			buf.SetTextStyle(cell.X, cell.Y, cell.Rune, color, dim)
			occupied[[2]int{cell.X, cell.Y}] = true
		}
	}
}

func clipConsist(_ []consistCell, col, row int, glyph string, vertical bool, width, height int) []consistCell {
	cells := consistCells(col, row, glyph, vertical)
	result := make([]consistCell, 0, len(cells))
	for _, cell := range cells {
		if cell.X >= 0 && cell.X < width && cell.Y >= 0 && cell.Y < height {
			result = append(result, cell)
		}
	}
	return result
}

func placeConsist(cells []consistCell, train sim.Train, vp geo.Viewport, protected, occupied map[[2]int]bool, width, height int) []consistCell {
	vertical := trainConsistVertical(train, vp)
	direction := 1
	if vertical {
		if !trainDirectionDown(train, vp) {
			direction = -1
		}
	} else if !trainDirectionRight(train, vp) {
		direction = -1
	}
	for _, distance := range []int{0} {
		candidate := make([]consistCell, len(cells))
		clear := true
		for i, cell := range cells {
			if vertical {
				cell.Y += distance * direction
			} else {
				cell.X += distance * direction
			}
			if cell.X < 0 || cell.X >= width || cell.Y < 0 || cell.Y >= height || protected[[2]int{cell.X, cell.Y}] || occupied[[2]int{cell.X, cell.Y}] {
				clear = false
			}
			candidate[i] = cell
		}
		if clear {
			return candidate
		}
	}
	return nil
}

// trainConsistGlyph returns a compact, directional train. A narrow map uses
// ASCII-only cars so terminals without Unicode glyph support still get a
// legible bounded marker. Selected trains are longer, not brighter: their
// native line color remains the ownership cue.
func trainConsistGlyph(train sim.Train, vp geo.Viewport, selected bool, col, row, width, height int, asciiOnly bool) string {
	vertical := trainConsistVertical(train, vp)
	length := 4
	if selected {
		length = 5
	}
	if vertical || asciiOnly {
		if asciiOnly && !vertical {
			if trainDirectionRight(train, vp) {
				return "===>"
			}
			return "<==="
		}
		if length > height {
			return ""
		}
	} else if trainDirectionRight(train, vp) {
		if length > width {
			return ""
		}
	} else {
		if length > width {
			return ""
		}
	}
	if vertical {
		if trainDirectionDown(train, vp) {
			return strings.Repeat("=", length-1) + ">"
		}
		return "<" + strings.Repeat("=", length-1)
	}
	if trainDirectionRight(train, vp) {
		if selected && length >= 5 {
			return "▰▰▰▰▶"
		}
		if length < 4 {
			return strings.Repeat("▰", maxInt(length-1, 0)) + "▶"
		}
		return "▰▰▰▶"
	}
	if selected && length >= 5 {
		return "◀▰▰▰▰"
	}
	if length < 4 {
		return "◀" + strings.Repeat("▰", maxInt(length-1, 0))
	}
	return "◀▰▰▰"
}

type consistCell struct {
	X, Y int
	Rune rune
}

func consistCells(col, row int, glyph string, vertical bool) []consistCell {
	runes := []rune(glyph)
	cells := make([]consistCell, 0, len(runes))
	for i, r := range runes {
		x, y := col+i, row
		if vertical {
			x, y = col, row+i
		}
		if !vertical && strings.HasPrefix(glyph, "◀") {
			x = col - (len(runes) - 1 - i)
		}
		cells = append(cells, consistCell{X: x, Y: y, Rune: r})
	}
	return cells
}

func trainConsistVertical(train sim.Train, vp geo.Viewport) bool {
	tangent := train.Tangent
	if tangent.Lon == 0 && tangent.Lat == 0 {
		return false
	}
	x0, y0 := vp.Project(orb.Point{train.Position.Lon, train.Position.Lat})
	x1, y1 := vp.Project(orb.Point{train.Position.Lon + tangent.Lon, train.Position.Lat + tangent.Lat})
	return math.Abs(y1-y0) > math.Abs(x1-x0)
}

func trainDirectionRight(train sim.Train, vp geo.Viewport) bool {
	tangent := train.Tangent
	if tangent.Lon == 0 && tangent.Lat == 0 {
		return true
	}
	x0, _ := vp.Project(orb.Point{train.Position.Lon, train.Position.Lat})
	x1, _ := vp.Project(orb.Point{train.Position.Lon + tangent.Lon, train.Position.Lat + tangent.Lat})
	return x1 >= x0
}

func trainDirectionDown(train sim.Train, vp geo.Viewport) bool {
	tangent := train.Tangent
	if tangent.Lon == 0 && tangent.Lat == 0 {
		return true
	}
	_, y0 := vp.Project(orb.Point{train.Position.Lon, train.Position.Lat})
	_, y1 := vp.Project(orb.Point{train.Position.Lon + tangent.Lon, train.Position.Lat + tangent.Lat})
	return y1 >= y0
}

func trainProtectedCells(indexes gtfs.Indexes, vp geo.Viewport, width, height int) map[[2]int]bool {
	protected := make(map[[2]int]bool)
	add := func(point orb.Point, radius int) {
		x, y := vp.Project(point)
		col, row := int(math.Round(x))/2, int(math.Round(y))/4
		// Keep the station marker readable; transfer stations reserve the
		// compact ring neighborhood as well.
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if absInt(dx)+absInt(dy) <= radius {
					cell := [2]int{col + dx, row + dy}
					if cell[0] >= 0 && cell[0] < width && cell[1] >= 0 && cell[1] < height {
						protected[cell] = true
					}
				}
			}
		}
	}
	for _, station := range indexes.OrderedStations {
		radius := 0
		if len(station.FamilyIDs) > 1 || len(station.LineIDs) > 1 {
			radius = 2
		}
		add(orb.Point{station.Longitude, station.Latitude}, radius)
	}
	for _, line := range indexes.OrderedLines {
		for _, shape := range line.Shapes {
			for _, placement := range shape.Placements {
				add(placement.Point, 0)
			}
		}
	}
	for _, family := range indexes.OrderedFamilies {
		for _, shape := range family.Shapes {
			for _, placement := range shape.Placements {
				add(placement.Point, 0)
			}
		}
	}
	return protected
}

func trainRenderColor(indexes gtfs.Indexes, train sim.Train) int {
	if family, ok := indexes.FamilyByID[train.FamilyID]; ok {
		if family.RendererColor != "" {
			return routeColor(family.RendererColor)
		}
		return routeColor(family.Color)
	}
	if family, ok := indexes.Families[train.FamilyID]; ok {
		if family.RendererColor != "" {
			return routeColor(family.RendererColor)
		}
		return routeColor(family.Color)
	}
	if line, ok := indexes.LineByID[train.RouteID]; ok {
		return lineRenderColor(line)
	}
	if line, ok := indexes.Lines[train.RouteID]; ok {
		return lineRenderColor(line)
	}
	for _, family := range indexes.OrderedFamilies {
		if family.ID == train.FamilyID {
			if family.RendererColor != "" {
				return routeColor(family.RendererColor)
			}
			return routeColor(family.Color)
		}
	}
	for _, line := range indexes.OrderedLines {
		if line.ID == train.RouteID {
			return lineRenderColor(line)
		}
	}
	return routeColor("")
}

func drawSelectedRoute(buf *braille.Buffer, indexes gtfs.Indexes, route gtfs.RouteResult, vp geo.Viewport) {
	drawRouteMarkers(buf, indexes, route, vp)
	drawTransferRings(buf, indexes, route, vp)
}

func drawRouteMarkers(buf *braille.Buffer, indexes gtfs.Indexes, route gtfs.RouteResult, vp geo.Viewport) {
	occupied := make(map[[2]int]bool)
	for _, segment := range selectedRouteSegments(indexes, route) {
		if len(segment.Geometry) < 2 {
			continue
		}
		for _, cell := range selectedRouteBeadCells(segment.Geometry, vp, buf.Width, buf.Height) {
			// Associations meet at a transfer station. Keep one deterministic
			// endpoint bead there; the transfer ring supplies the dual-family
			// semantics without a duplicate text pass.
			if occupied[cell] {
				continue
			}
			occupied[cell] = true
			buf.SetText(cell[0], cell[1], '●', segment.Color)
		}
	}
}

type selectedRouteSegment struct {
	Geometry orb.LineString
	Color    int
}

func selectedRouteSegments(indexes gtfs.Indexes, route gtfs.RouteResult) []selectedRouteSegment {
	if route.Status != gtfs.RouteReady {
		return nil
	}
	segments := make([]selectedRouteSegment, 0, len(route.Steps))
	for _, step := range route.Steps {
		if len(step.ShapeAssociations) == 0 {
			return nil
		}
		for _, association := range step.ShapeAssociations {
			geometry, ok := routeShapeGeometry(indexes, association)
			if !ok {
				return nil
			}
			clipped := clipShape(geometry, association.FromPlacement, association.ToPlacement)
			if len(clipped) < 2 {
				return nil
			}
			segments = append(segments, selectedRouteSegment{
				Geometry: clipped,
				Color:    routeFamilyColor(indexes, association.FamilyID, association.LineID),
			})
		}
	}
	if len(segments) == 0 {
		return nil
	}
	return segments
}

func drawSelectedRouteMarkers(buf *braille.Buffer, geometry orb.LineString, vp geo.Viewport, color int) {
	for _, cell := range selectedRouteBeadCells(geometry, vp, buf.Width, buf.Height) {
		// A text bead is deliberately composed before labels and the cursor. It
		// gives the exact selected spine a visible native-color texture without
		// corrupting passenger-facing map text or cursor state.
		buf.SetText(cell[0], cell[1], '●', color)
	}
}

// selectedRouteBeadCells returns a deterministic, terminal-cell-aware sample
// of one already clipped GTFS association. It never manufactures geometry:
// every candidate is interpolated on the supplied projected polyline, while
// the first and last cells preserve the association's exact endpoints.
func selectedRouteBeadCells(geometry orb.LineString, vp geo.Viewport, width, height int) [][2]int {
	if len(geometry) < 2 || width <= 0 || height <= 0 {
		return nil
	}
	type projectedPoint struct{ x, y float64 }
	points := make([]projectedPoint, len(geometry))
	lengths := make([]float64, len(geometry))
	for i, point := range geometry {
		x, y := vp.Project(point)
		// Braille pixels are two columns by four rows per terminal cell.
		points[i] = projectedPoint{x: x / 2, y: y / 4}
		if i > 0 {
			lengths[i] = lengths[i-1] + math.Hypot(points[i].x-points[i-1].x, points[i].y-points[i-1].y)
		}
	}
	total := lengths[len(lengths)-1]
	if total == 0 {
		return nil
	}

	type candidate struct {
		cell     [2]int
		distance float64
		endpoint bool
	}
	candidates := make([]candidate, 0, int(total/selectedMarkerCadenceCells)+3)
	pointAt := func(distance float64) projectedPoint {
		if distance <= 0 {
			return points[0]
		}
		if distance >= total {
			return points[len(points)-1]
		}
		segment := sort.Search(len(lengths), func(i int) bool { return lengths[i] >= distance })
		if segment == 0 {
			segment = 1
		}
		start, end := lengths[segment-1], lengths[segment]
		fraction := 0.0
		if end > start {
			fraction = (distance - start) / (end - start)
		}
		return projectedPoint{
			x: points[segment-1].x + (points[segment].x-points[segment-1].x)*fraction,
			y: points[segment-1].y + (points[segment].y-points[segment-1].y)*fraction,
		}
	}
	toCandidate := func(point projectedPoint, distance float64, endpoint bool) candidate {
		return candidate{cell: [2]int{int(math.Round(point.x)), int(math.Round(point.y))}, distance: distance, endpoint: endpoint}
	}
	candidates = append(candidates, toCandidate(points[0], 0, true))
	for distance := selectedMarkerCadenceCells; distance < total; distance += selectedMarkerCadenceCells {
		candidates = append(candidates, toCandidate(pointAt(distance), distance, false))
		if len(candidates) >= selectedMarkerMaxBeads {
			break
		}
	}
	candidates = append(candidates, toCandidate(points[len(points)-1], total, true))

	result := make([][2]int, 0, minInt(len(candidates), selectedMarkerMaxBeads))
	for _, next := range candidates {
		if next.cell[0] < 0 || next.cell[0] >= width || next.cell[1] < 0 || next.cell[1] >= height {
			continue
		}
		if len(result) >= selectedMarkerMaxBeads {
			// Preserve the terminal endpoint anchor even when a very long
			// association reaches the safety bound; it replaces the last
			// interior candidate rather than exceeding the bound.
			if next.endpoint && next.cell != result[len(result)-1] {
				result[len(result)-1] = next.cell
			}
			break
		}
		if len(result) == 0 {
			result = append(result, next.cell)
			continue
		}
		previous := result[len(result)-1]
		if previous == next.cell {
			continue
		}
		gap := maxInt(absInt(next.cell[0]-previous[0]), absInt(next.cell[1]-previous[1]))
		if gap < selectedMarkerMinGapCells {
			// Endpoint anchors win over a crowded interior candidate. If the
			// endpoint is still too close to the preceding anchor, retaining
			// both is preferable on a short clipped association: there is no
			// geometry on which to spend the requested empty cell. For a long
			// association, replace the crowded interior bead when the prior
			// anchor gives the endpoint a valid gap.
			if next.endpoint {
				if len(result) > 1 {
					beforePrevious := result[len(result)-2]
					beforeGap := maxInt(absInt(next.cell[0]-beforePrevious[0]), absInt(next.cell[1]-beforePrevious[1]))
					if beforeGap >= selectedMarkerMinGapCells {
						result[len(result)-1] = next.cell
						continue
					}
				}
				result = append(result, next.cell)
			}
			continue
		}
		result = append(result, next.cell)
		if len(result) >= selectedMarkerMaxBeads {
			break
		}
	}
	return result
}

func drawTransferRings(buf *braille.Buffer, indexes gtfs.Indexes, route gtfs.RouteResult, vp geo.Viewport) {
	for i := 0; i+1 < len(route.Steps); i++ {
		current, next := route.Steps[i], route.Steps[i+1]
		if current.FamilyID == next.FamilyID || current.ToStationID != next.FromStationID {
			continue
		}
		station, ok := indexes.Stations[current.ToStationID]
		if !ok {
			station, ok = indexes.StationByID[current.ToStationID]
		}
		if !ok {
			continue
		}
		x, y := vp.Project(orb.Point{station.Longitude, station.Latitude})
		px, py := int(math.Round(x)), int(math.Round(y))
		firstColor := routeFamilyColor(indexes, current.FamilyID, "")
		secondColor := routeFamilyColor(indexes, next.FamilyID, "")
		// Outer and inner rings use separate pixel radii so both family colors
		// survive braille cell color composition. Fixed offsets keep this output
		// deterministic and never connect the two clipped route segments.
		for _, offset := range [][2]int{{-4, 0}, {4, 0}, {0, -4}, {0, 4}, {-3, -3}, {3, -3}, {-3, 3}, {3, 3}} {
			buf.SetPixel(px+offset[0], py+offset[1], firstColor)
		}
		for _, offset := range [][2]int{{-2, 0}, {2, 0}, {0, -2}, {0, 2}, {-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
			buf.SetPixel(px+offset[0], py+offset[1], secondColor)
		}
	}
}

// RouteGeometry returns the exact selected shape geometry represented by a
// prepared route. It is flattened only for consumers such as bounds fitting;
// rendering uses RouteGeometrySegments so disconnected segments are never
// joined. Invalid or incomplete prepared geometry fails closed with nil.
func RouteGeometry(indexes gtfs.Indexes, route gtfs.RouteResult) []orb.Point {
	segments := RouteGeometrySegments(indexes, route)
	var points []orb.Point
	for _, segment := range segments {
		points = append(points, segment...)
	}
	return points
}

// RouteGeometrySegments returns each exact, station-clipped GTFS shape segment
// independently. A ready route without a complete association for every step
// is invalid for rendering and returns nil rather than falling back to station
// coordinates or unrelated family branches.
func RouteGeometrySegments(indexes gtfs.Indexes, route gtfs.RouteResult) [][]orb.Point {
	if route.Status != gtfs.RouteReady {
		return nil
	}
	segments := make([][]orb.Point, 0, len(route.Steps))
	for _, step := range route.Steps {
		if len(step.ShapeAssociations) == 0 {
			return nil
		}
		for _, association := range step.ShapeAssociations {
			geometry, ok := routeShapeGeometry(indexes, association)
			if !ok {
				return nil
			}
			segment := clipShape(geometry, association.FromPlacement, association.ToPlacement)
			if len(segment) < 2 {
				return nil
			}
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return nil
	}
	return segments
}

func routeShapeGeometry(indexes gtfs.Indexes, association gtfs.RouteShapeAssociation) (orb.LineString, bool) {
	if shape, ok := indexes.Shapes[association.ShapeID]; ok && len(shape.Geometry) > 0 {
		return shape.Geometry, true
	}
	if shape, ok := indexes.ShapeByID[association.ShapeID]; ok && len(shape.Geometry) > 0 {
		return shape.Geometry, true
	}
	for _, line := range indexes.OrderedLines {
		if line.ID != association.LineID {
			continue
		}
		for _, shape := range line.Shapes {
			if shape.ShapeID == association.ShapeID && len(shape.Geometry) > 0 {
				return shape.Geometry, true
			}
		}
	}
	return nil, false
}

func clipShape(shape orb.LineString, from, to gtfs.StationPlacement) []orb.Point {
	if len(shape) == 0 {
		return nil
	}
	start, end := placementOffset(from), placementOffset(to)
	forward := start <= end
	if !forward {
		start, end = end, start
	}
	result := make([]orb.Point, 0, len(shape))
	if forward {
		result = append(result, placementPoint(shape, from))
		for i := int(math.Ceil(start)); i < int(math.Floor(end))+1 && i < len(shape)-1; i++ {
			if float64(i) > start && float64(i) < end {
				result = append(result, shape[i])
			}
		}
		result = append(result, placementPoint(shape, to))
		return result
	}
	result = append(result, placementPoint(shape, to))
	for i := int(math.Floor(end)); i >= int(math.Ceil(start)) && i > 0; i-- {
		if float64(i) > start && float64(i) < end {
			result = append(result, shape[i])
		}
	}
	result = append(result, placementPoint(shape, from))
	return result
}

func placementOffset(placement gtfs.StationPlacement) float64 {
	return float64(placement.SegmentIndex) + placement.SegmentFraction
}

func placementPoint(shape orb.LineString, placement gtfs.StationPlacement) orb.Point {
	if len(shape) == 0 {
		return placement.Point
	}
	i := placement.SegmentIndex
	if i < 0 {
		i = 0
	}
	if i >= len(shape)-1 {
		return shape[len(shape)-1]
	}
	f := placement.SegmentFraction
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	a, b := shape[i], shape[i+1]
	return orb.Point{a.X() + (b.X()-a.X())*f, a.Y() + (b.Y()-a.Y())*f}
}

// drawGTFSOverlay draws the complete deterministic transit layer above the
// base map. Lines are emitted in OrderedLines/Shapes order, followed by their
// shape placements in contract order. Drawing stations last keeps the
// passenger-facing points visible over their route geometry.
func drawGTFSOverlay(buf *braille.Buffer, indexes gtfs.Indexes, vp geo.Viewport, selectedStation string, route ...*gtfs.RouteResult) {
	readyRoute := hasReadyRoute(route)
	if len(indexes.OrderedFamilies) > 0 {
		for _, family := range indexes.OrderedFamilies {
			color := routeColor(family.RendererColor)
			for _, shape := range family.Shapes {
				drawGeoLineStyle(buf, shape.Geometry, vp, color, readyRoute)
			}
		}
		return
	}
	for _, line := range indexes.OrderedLines {
		color := lineRenderColor(line)
		for _, shape := range line.Shapes {
			drawGeoLineStyle(buf, shape.Geometry, vp, color, readyRoute)
		}
	}
}

func drawGTFSStations(buf *braille.Buffer, indexes gtfs.Indexes, vp geo.Viewport, selectedStation string, route ...*gtfs.RouteResult) {
	readyRoute := hasReadyRoute(route)
	if len(indexes.OrderedFamilies) > 0 {
		for _, family := range indexes.OrderedFamilies {
			color := routeColor(family.RendererColor)
			for _, shape := range family.Shapes {
				for _, placement := range shape.Placements {
					drawStationStyle(buf, placement.Point, vp, color, placement.StationID == selectedStation, readyRoute)
				}
			}
		}
		return
	}
	for _, line := range indexes.OrderedLines {
		color := lineRenderColor(line)
		for _, shape := range line.Shapes {
			for _, placement := range shape.Placements {
				drawStationStyle(buf, placement.Point, vp, color, placement.StationID == selectedStation, readyRoute)
			}
		}
	}
}

func hasReadyRoute(routes []*gtfs.RouteResult) bool {
	if len(routes) == 0 || routes[0] == nil || routes[0].Status != gtfs.RouteReady {
		return false
	}
	return true
}

func routeFamilyColor(indexes gtfs.Indexes, familyID, lineID string) int {
	if family, ok := indexes.FamilyByID[familyID]; ok {
		if family.RendererColor != "" {
			return routeColor(family.RendererColor)
		}
		return routeColor(family.Color)
	}
	if family, ok := indexes.Families[familyID]; ok {
		if family.RendererColor != "" {
			return routeColor(family.RendererColor)
		}
		return routeColor(family.Color)
	}
	if line, ok := indexes.LineByID[lineID]; ok {
		return lineRenderColor(line)
	}
	if line, ok := indexes.Lines[lineID]; ok {
		return lineRenderColor(line)
	}
	for _, line := range indexes.OrderedLines {
		if line.ID == lineID || line.FamilyID == familyID {
			return lineRenderColor(line)
		}
	}
	return routeColor("")
}

func lineRenderColor(line gtfs.Line) int {
	value := line.RendererColor
	if value == "" {
		value = line.Color
	}
	return routeColor(value)
}

func drawGeoLine(buf *braille.Buffer, geometry []orb.Point, vp geo.Viewport, color int) {
	drawGeoLineStyle(buf, geometry, vp, color, false)
}

func drawGeoLineStyle(buf *braille.Buffer, geometry []orb.Point, vp geo.Viewport, color int, dim bool) {
	if len(geometry) < 2 {
		return
	}
	xs := make([]int, len(geometry))
	ys := make([]int, len(geometry))
	for i, point := range geometry {
		x, y := vp.Project(point)
		xs[i], ys[i] = int(math.Round(x)), int(math.Round(y))
	}
	for i := 1; i < len(xs); i++ {
		drawLineStyle(buf, xs[i-1], ys[i-1], xs[i], ys[i], color, dim)
	}
}

func drawStation(buf *braille.Buffer, point orb.Point, vp geo.Viewport, color int, selected bool) {
	drawStationStyle(buf, point, vp, color, selected, false)
}

func drawStationStyle(buf *braille.Buffer, point orb.Point, vp geo.Viewport, color int, selected, dim bool) {
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
	buf.SetPixelStyle(px, py, color, dim)
	buf.SetPixelStyle(px-1, py, color, dim)
	buf.SetPixelStyle(px+1, py, color, dim)
	buf.SetPixelStyle(px, py-1, color, dim)
	buf.SetPixelStyle(px, py+1, color, dim)
}

func drawLineStyle(buf *braille.Buffer, x0, y0, x1, y1, color int, dim bool) {
	dx := absInt(x1 - x0)
	dy := absInt(y1 - y0)
	sx := signInt(x1 - x0)
	sy := signInt(y1 - y0)
	err := dx - dy
	for {
		buf.SetPixelStyle(x0, y0, color, dim)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func signInt(value int) int {
	if value > 0 {
		return 1
	}
	if value < 0 {
		return -1
	}
	return 0
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

// NearestStation returns the stable passenger-facing station nearest to point,
// when it is within the map cursor's hover radius. It is shared by the map
// renderer and endpoint selection so both surfaces use the same hit testing.
func NearestStation(indexes gtfs.Indexes, vp geo.Viewport, point orb.Point) string {
	return nearestStation(indexes, vp, point)
}

func drawCursor(buf *braille.Buffer, point orb.Point, vp geo.Viewport, occupied ...map[[2]int]bool) {
	x, y := vp.Project(point)
	col, row := int(math.Floor(x))/2, int(math.Floor(y))/4
	blocked := map[[2]int]bool{}
	if len(occupied) > 0 && occupied[0] != nil {
		blocked = occupied[0]
	}
	// Labels are user-facing text and must never be replaced by the cursor. If
	// the geographic cell is occupied, choose the nearest free cell in a stable
	// Manhattan ring. This keeps the cursor visible without corrupting labels
	// such as "New Delhi" at any zoom or tile position.
	if blocked[[2]int{col, row}] {
		found := false
		for radius := 1; radius <= maxInt(buf.Width, buf.Height) && !found; radius++ {
			for dy := -radius; dy <= radius && !found; dy++ {
				dx := radius - absInt(dy)
				for _, candidateX := range []int{col - dx, col + dx} {
					candidate := [2]int{candidateX, row + dy}
					if candidateX >= 0 && candidateX < buf.Width && candidate[1] >= 0 && candidate[1] < buf.Height && !blocked[candidate] {
						col, row = candidate[0], candidate[1]
						found = true
						break
					}
				}
			}
		}
		if !found {
			return
		}
	}
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

func writeLabelsToBuffer(buf *braille.Buffer, labels []Label, termW, termH int) map[[2]int]bool {
	occupied := make(map[[2]int]bool)
	ordered := append([]Label(nil), labels...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].RowY != ordered[j].RowY {
			return ordered[i].RowY < ordered[j].RowY
		}
		if ordered[i].ColX != ordered[j].ColX {
			return ordered[i].ColX < ordered[j].ColX
		}
		if ordered[i].Text != ordered[j].Text {
			return ordered[i].Text < ordered[j].Text
		}
		return ordered[i].Color < ordered[j].Color
	})
	for _, l := range ordered {
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
		// A symbol such as 🍴 or 🥐 occupies two terminal cells. Keep the
		// entire grapheme inside the map cell budget so the application frame
		// cannot be shifted by a wide label at the edge of the map.
		if len(runes) > 0 && runeDisplayWidth(string(runes[0])) > maxLen {
			continue
		}
		for len(runes) > 0 && runeDisplayWidth(string(runes)) > maxLen {
			runes = runes[:len(runes)-1]
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
	return occupied
}

func runeDisplayWidth(value string) int {
	return lipgloss.Width(value)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
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

func drawGeometry(buf *braille.Buffer, g orb.Geometry, req geo.TileRequest, st style.LayerStyle, dim bool) {
	switch geom := g.(type) {
	case orb.LineString:
		if st.DrawLine {
			drawLineStringStyle(buf, geom, req, st.LineColor, dim)
		}
	case orb.MultiLineString:
		if st.DrawLine {
			for _, ls := range geom {
				drawLineStringStyle(buf, ls, req, st.LineColor, dim)
			}
		}
	case orb.Polygon:
		if st.DrawFill {
			drawPolygonStyle(buf, geom, req, st.FillColor, dim)
		}
		if st.DrawLine {
			drawLineStringStyle(buf, orb.LineString(geom[0]), req, st.LineColor, dim)
		}
	case orb.MultiPolygon:
		for _, poly := range geom {
			if st.DrawFill {
				drawPolygonStyle(buf, poly, req, st.FillColor, dim)
			}
			if st.DrawLine {
				drawLineStringStyle(buf, orb.LineString(poly[0]), req, st.LineColor, dim)
			}
		}
	case orb.Point:
		if st.DrawLine {
			px, py := tileToPixel(geom[0], geom[1], req)
			buf.SetPixelStyle(px, py, st.LineColor, dim)
		}
	}
}

func tileToPixel(tileX, tileY float64, req geo.TileRequest) (px, py int) {
	px = req.PixelOffsetX + int(tileX*req.Scale)
	py = req.PixelOffsetY + int(tileY*req.Scale)
	return
}

func drawLineString(buf *braille.Buffer, ls orb.LineString, req geo.TileRequest, color int) {
	drawLineStringStyle(buf, ls, req, color, false)
}

func drawLineStringStyle(buf *braille.Buffer, ls orb.LineString, req geo.TileRequest, color int, dim bool) {
	if len(ls) < 2 {
		return
	}
	xs := make([]int, len(ls))
	ys := make([]int, len(ls))
	for i, pt := range ls {
		xs[i], ys[i] = tileToPixel(pt[0], pt[1], req)
	}
	buf.DrawPolylineStyle(xs, ys, color, dim)
}

func drawPolygon(buf *braille.Buffer, poly orb.Polygon, req geo.TileRequest, color int) {
	drawPolygonStyle(buf, poly, req, color, false)
}

func drawPolygonStyle(buf *braille.Buffer, poly orb.Polygon, req geo.TileRequest, color int, dim bool) {
	if len(poly) == 0 {
		return
	}
	ring := poly[0]
	xs := make([]int, len(ring))
	ys := make([]int, len(ring))
	for i, pt := range ring {
		xs[i], ys[i] = tileToPixel(pt[0], pt[1], req)
	}
	buf.FillPolygonStyle(xs, ys, color, dim)
	for _, hole := range poly[1:] {
		hxs := make([]int, len(hole))
		hys := make([]int, len(hole))
		for i, pt := range hole {
			hxs[i], hys[i] = tileToPixel(pt[0], pt[1], req)
		}
		buf.FillPolygonStyle(hxs, hys, 0, false)
	}
}
