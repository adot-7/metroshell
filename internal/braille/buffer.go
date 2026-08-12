package braille

import (
	"fmt"
	"strings"
)

// The braille dot bit-values.
// Layout within a character cell (col, row → bit):
//
// col: 0, 1
// row 0: 0x01 0x08
// row 1: 0x02 0x10
// row 2: 0x04 0x20
// row 3: 0x40 0x80
//
// Unicode codepoint = 0x2800 + (OR of all raised dot bits)
var dotBit = [2][4]uint8{
	{0x01, 0x02, 0x04, 0x40}, // column 0: rows 0,1,2,3
	{0x08, 0x10, 0x20, 0x80}, // column 1: rows 0,1,2,3
}

// Buffer is a 2D grid of braille character cells.
// Width and Height are in *terminal character* units.
// In pixel-space, this covers Width*2 × Height*4 pixels.
type Buffer struct {
	Width, Height int
	mask          []uint8 // bitmask per cell: len = Width * Height
	color         []int   // xterm-256 color per cell: 0 means no color

	// Text overlay: when txtChar[i] != 0, Render emits that rune instead of the
	// braille character at that cell. Set via SetText; cleared by Clear.
	txtChar  []rune // text character overlay per cell (0 = no overlay)
	txtColor []int  // xterm-256 color for the text overlay (0 = terminal default)
}

// New creates a braille buffer for a terminal of w columns × h rows.
func New(w, h int) *Buffer {
	size := w * h
	return &Buffer{
		Width:    w,
		Height:   h,
		mask:     make([]uint8, size),
		color:    make([]int, size),
		txtChar:  make([]rune, size),
		txtColor: make([]int, size),
	}
}

// Clear resets the buffer to empty (all spaces, no color, no text overlay).
func (b *Buffer) Clear() {
	for i := range b.mask {
		b.mask[i] = 0
		b.color[i] = 0
		b.txtChar[i] = 0
		b.txtColor[i] = 0
	}
}

// PixelWidth returns how many "pixels" wide the buffer is.
// Each character cell holds 2 dot columns.
func (b *Buffer) PixelWidth() int { return b.Width * 2 }

// PixelHeight returns how many "pixels" tall the buffer is.
// Each character cell holds 4 dot rows.
func (b *Buffer) PixelHeight() int { return b.Height * 4 }

// SetPixel turns on a single pixel at braille-pixel coordinates (px, py).
// (0,0) is top-left. px can be 0 to PixelWidth()-1, py 0 to PixelHeight()-1.
// colorCode is an xterm-256 color index (0 means default terminal color).
func (b *Buffer) SetPixel(px, py, colorCode int) {
	if px < 0 || px >= b.PixelWidth() || py < 0 || py >= b.PixelHeight() {
		return // silently clip to bounds
	}
	// Which character cell does this pixel fall in?
	charCol := px / 2
	charRow := py / 4
	// Which dot within that cell?
	dotCol := px % 2 // 0 or 1
	dotRow := py % 4 // 0, 1, 2, or 3
	cellIndex := charRow*b.Width + charCol
	b.mask[cellIndex] |= dotBit[dotCol][dotRow]
	if colorCode != 0 {
		b.color[cellIndex] = colorCode
	}
}

// SetText places a single text character at terminal cell (col, row).
// During Render, text overlay takes priority over whatever braille character
// occupies the same cell — the geometry is still drawn underneath, but the
// character emitted is r instead of the braille codepoint.
// (0,0) is the top-left terminal cell. col is 0..Width-1, row is 0..Height-1.
func (b *Buffer) SetText(col, row int, r rune, colorCode int) {
	if col < 0 || col >= b.Width || row < 0 || row >= b.Height {
		return
	}
	idx := row*b.Width + col
	b.txtChar[idx] = r
	b.txtColor[idx] = colorCode
}

// DrawLine rasterizes a line from (x0,y0) to (x1,y1) using Bresenham's algorithm.
// Coordinates are in braille-pixel space.
func (b *Buffer) DrawLine(x0, y0, x1, y1, colorCode int) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx := sign(x1 - x0)
	sy := sign(y1 - y0)
	err := dx - dy
	for {
		b.SetPixel(x0, y0, colorCode)
		if x0 == x1 && y0 == y1 {
			break
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

// DrawPolyline draws a connected sequence of line segments.
func (b *Buffer) DrawPolyline(xs, ys []int, colorCode int) {
	for i := 1; i < len(xs); i++ {
		b.DrawLine(xs[i-1], ys[i-1], xs[i], ys[i], colorCode)
	}
}

// FillPolygon fills a polygon using the scanline algorithm.
// xs and ys are parallel slices of the polygon's vertices.
// The polygon is automatically closed (last point connects to first).
func (b *Buffer) FillPolygon(xs, ys []int, colorCode int) {
	n := len(xs)
	if n < 3 {
		return
	}
	// Find the bounding box in y
	minY, maxY := ys[0], ys[0]
	for _, y := range ys {
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	// Clip to buffer
	if minY < 0 {
		minY = 0
	}
	if maxY >= b.PixelHeight() {
		maxY = b.PixelHeight() - 1
	}
	// For each scanline
	intersections := make([]int, 0, 8)
	for y := minY; y <= maxY; y++ {
		intersections = intersections[:0]
		j := n - 1
		for i := 0; i < n; i++ {
			yi, yj := ys[i], ys[j]
			// Does edge (j→i) cross the scanline at y?
			if (yi <= y && yj > y) || (yj <= y && yi > y) {
				// x at the intersection (integer division)
				x := xs[i] + (y-yi)*(xs[j]-xs[i])/(yj-yi)
				intersections = append(intersections, x)
			}
			j = i
		}
		// Sort intersections (usually just 2, but could be more for concave polygons)
		sortInts(intersections)
		// Fill between pairs
		for k := 0; k+1 < len(intersections); k += 2 {
			for x := intersections[k]; x <= intersections[k+1]; x++ {
				b.SetPixel(x, y, colorCode)
			}
		}
	}
}

// Render converts the buffer to a string of braille Unicode characters with ANSI colors.
// Cells that have a text overlay (set via SetText) emit their text character instead of
// the braille codepoint — this is how map labels are drawn inline with the geometry.
// This string is what you return from the Bubble Tea View() function.
func (b *Buffer) Render() string {
	var sb strings.Builder
	sb.Grow(b.Width * b.Height * 6) // pre-allocate rough estimate
	for row := 0; row < b.Height; row++ {
		for col := 0; col < b.Width; col++ {
			cellIndex := row*b.Width + col

			// Text overlay takes priority: emit the label character, not the braille dot.
			if r := b.txtChar[cellIndex]; r != 0 {
				tc := b.txtColor[cellIndex]
				if tc != 0 {
					//sb.WriteString(fmt.Sprintf("\x1b[38;5;%dm%c\x1b[0m", tc, r))
					fmt.Fprintf(&sb, "\x1b[38;5;%dm%c\x1b[0m", tc, r)
				} else {
					sb.WriteRune(r)
				}
				continue
			}

			// Regular braille rendering.
			mask := b.mask[cellIndex]
			clr := b.color[cellIndex]
			ch := rune(0x2800 + uint32(mask)) // braille codepoint
			if clr != 0 {
				// xterm-256 foreground color escape: \x1b[38;5;{n}m
				sb.WriteString(fmt.Sprintf("\x1b[38;5;%dm%c\x1b[0m", clr, ch))
			} else {
				sb.WriteRune(ch)
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// --- Helper types for color conversion ---
// RGBToXterm256 converts an RGB color to the nearest xterm-256 color index.
// The 6×6×6 color cube occupies indices 16-231.
// The 24-step grayscale ramp occupies indices 232-255.
func RGBToXterm256(r, g, b uint8) int {
	// Check if it's closer to a grayscale
	ri, gi, bi := int(r), int(g), int(b)
	if abs(ri-gi) < 20 && abs(gi-bi) < 20 && abs(ri-bi) < 20 {
		// Use grayscale ramp (indices 232-255)
		// Each step is (255-8)/23 ≈ 10.8 gray units
		avg := (ri + gi + bi) / 3
		if avg < 8 {
			return 16
		}
		// black
		if avg > 238 {
			return 231
		} // white
		return 232 + (avg-8)/10
	}
	// Use the 6×6×6 color cube
	// Cube component values: 0→0, 1→95, 2→135, 3→175, 4→215, 5→255
	cr := cubeIndex(ri)
	cg := cubeIndex(gi)
	cb := cubeIndex(bi)
	return 16 + 36*cr + 6*cg + cb
}

// cubeIndex finds the nearest value in the xterm color cube ramp [0,95,135,175,215,255]
func cubeIndex(v int) int {
	// thresholds between ramp values
	thresholds := []int{48, 115, 155, 195, 235}
	for i, t := range thresholds {
		if v < t {
			return i
		}
	}
	return 5
}

// --- stdlib helpers ---
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func sign(x int) int {
	if x > 0 {
		return 1
	}
	if x < 0 {
		return -1
	}
	return 0
}

// sortInts is a simple insertion sort (fast for the small slices we have)
func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
