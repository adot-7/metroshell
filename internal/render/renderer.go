package render

import (
	"math"

	"github.com/adot-7/metroshell/internal/braille"
	"github.com/adot-7/metroshell/internal/geo"
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
}

// Label holds a text label to be written into the braille buffer's text overlay.
type Label struct {
	Text  string
	ColX  int
	RowY  int
	Color int
}

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

	termW := req.PixelW / 2
	termH := req.PixelH / 4
	writeLabelsToBuffer(buf, labels, termW, termH)
	return buf.Render()
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
