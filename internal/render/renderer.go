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
	selectedMarkerStep   = 7.0
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
		drawGTFSOverlay(buf, *req.GTFS, vp, selected, req.Route)
		if req.Route != nil && req.Route.Status == gtfs.RouteReady {
			drawRouteMarkers(buf, *req.GTFS, *req.Route, vp)
		}
		drawGTFSStations(buf, *req.GTFS, vp, selected)
		if req.Route != nil && req.Route.Status == gtfs.RouteReady {
			drawTransferRings(buf, *req.GTFS, *req.Route, vp)
		}
		drawTrains(buf, *req.GTFS, req.Trains, vp, req.Route)
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
func drawTrains(buf *braille.Buffer, indexes gtfs.Indexes, trains []sim.Train, vp geo.Viewport, route *gtfs.RouteResult) {
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

	ordered := append([]sim.Train(nil), trains...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	selectedFamilies := make(map[string]bool)
	if route != nil && route.Status == gtfs.RouteReady {
		for _, familyID := range route.FamilyIDs {
			selectedFamilies[familyID] = true
		}
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
		if selected {
			// A small yellow halo keeps selected-route contrast while the center
			// remains family/route colored for stable line ownership.
			for _, offset := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				buf.SetPixel(px+offset[0], py+offset[1], selectedStationColor)
			}
		}
		// A text dot remains legible even when the route line already occupies
		// the same braille cell. Labels and the cursor are composed later and
		// therefore retain priority over this live layer.
		buf.SetText(px/2, py/4, '●', color)
	}
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

func drawRouteHighlight(buf *braille.Buffer, indexes gtfs.Indexes, route gtfs.RouteResult, vp geo.Viewport) {
	drawRouteMarkers(buf, indexes, route, vp)
	drawTransferRings(buf, indexes, route, vp)
}

func drawRouteMarkers(buf *braille.Buffer, indexes gtfs.Indexes, route gtfs.RouteResult, vp geo.Viewport) {
	for _, segment := range selectedRouteSegments(indexes, route) {
		if len(segment.Geometry) < 2 {
			continue
		}
		drawSelectedRouteMarkers(buf, segment.Geometry, vp, segment.Color)
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
	if len(geometry) < 2 {
		return
	}
	lastCell := [2]int{-1, -1}
	distanceSinceMarker := selectedMarkerStep
	for i := 1; i < len(geometry); i++ {
		x0, y0 := vp.Project(geometry[i-1])
		x1, y1 := vp.Project(geometry[i])
		dx, dy := x1-x0, y1-y0
		length := math.Hypot(dx, dy)
		if length == 0 {
			continue
		}
		steps := int(math.Floor((distanceSinceMarker + length) / selectedMarkerStep))
		for marker := 0; marker < steps; marker++ {
			along := selectedMarkerStep*float64(marker+1) - distanceSinceMarker
			if along > length {
				break
			}
			fraction := along / length
			px := int(math.Round(x0 + dx*fraction))
			py := int(math.Round(y0 + dy*fraction))
			cell := [2]int{px / 2, py / 4}
			if cell != lastCell {
				buf.SetPixel(px, py, color)
				buf.SetPixel(px+1, py, color)
				lastCell = cell
			}
		}
		distanceSinceMarker += length
		if steps > 0 {
			distanceSinceMarker -= float64(steps) * selectedMarkerStep
		}
	}
	// Short clipped legs still receive a marker at both exact endpoints. The
	// markers are cell-sized, while the base line remains the source geometry.
	for _, point := range []orb.Point{geometry[0], geometry[len(geometry)-1]} {
		x, y := vp.Project(point)
		cell := [2]int{int(math.Round(x)) / 2, int(math.Round(y)) / 4}
		if cell != lastCell {
			buf.SetPixel(int(math.Round(x)), int(math.Round(y)), color)
			buf.SetPixel(int(math.Round(x))+1, int(math.Round(y)), color)
			lastCell = cell
		}
	}
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
	selectedFamilies := selectedRouteFamilies(route)
	if len(indexes.OrderedFamilies) > 0 {
		for _, family := range indexes.OrderedFamilies {
			color := routeColor(family.RendererColor)
			if len(selectedFamilies) > 0 && !selectedFamilies[family.ID] {
				color = dimRouteColor(color)
			}
			for _, shape := range family.Shapes {
				drawGeoLine(buf, shape.Geometry, vp, color)
			}
		}
		return
	}
	for _, line := range indexes.OrderedLines {
		color := lineRenderColor(line)
		if len(selectedFamilies) > 0 && !selectedFamilies[line.FamilyID] && !selectedFamilies[line.ID] {
			color = dimRouteColor(color)
		}
		for _, shape := range line.Shapes {
			drawGeoLine(buf, shape.Geometry, vp, color)
		}
	}
}

func drawGTFSStations(buf *braille.Buffer, indexes gtfs.Indexes, vp geo.Viewport, selectedStation string) {
	if len(indexes.OrderedFamilies) > 0 {
		for _, family := range indexes.OrderedFamilies {
			color := routeColor(family.RendererColor)
			for _, shape := range family.Shapes {
				for _, placement := range shape.Placements {
					drawStation(buf, placement.Point, vp, color, placement.StationID == selectedStation)
				}
			}
		}
		return
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

func selectedRouteFamilies(routes []*gtfs.RouteResult) map[string]bool {
	if len(routes) == 0 || routes[0] == nil || routes[0].Status != gtfs.RouteReady {
		return nil
	}
	selected := make(map[string]bool)
	for _, familyID := range routes[0].FamilyIDs {
		selected[familyID] = true
	}
	return selected
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

func dimRouteColor(color int) int {
	r, g, b := xtermRGB(color)
	return braille.RGBToXterm256(uint8(r*3/5), uint8(g*3/5), uint8(b*3/5))
}

func xtermRGB(color int) (int, int, int) {
	if color >= 232 && color <= 255 {
		value := 8 + (color-232)*10
		return value, value, value
	}
	if color >= 16 && color <= 231 {
		value := color - 16
		return []int{0, 95, 135, 175, 215, 255}[value/36], []int{0, 95, 135, 175, 215, 255}[(value%36)/6], []int{0, 95, 135, 175, 215, 255}[value%6]
	}
	return 128, 128, 128
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
