package main

import (
	"fmt"

	"github.com/adot-7/metroshell/internal/braille"
)

func main() {
	buf := braille.New(40, 20)
	// Draw a box
	buf.DrawLine(0, 0, 79, 0, braille.RGBToXterm256(255, 100, 100))   // top
	buf.DrawLine(79, 0, 79, 79, braille.RGBToXterm256(100, 255, 100)) // right
	buf.DrawLine(79, 79, 0, 79, braille.RGBToXterm256(100, 100, 255)) // bottom
	buf.DrawLine(0, 79, 0, 0, braille.RGBToXterm256(255, 255, 100))   // left
	// Draw a diagonal
	buf.DrawLine(0, 0, 79, 79, braille.RGBToXterm256(200, 200, 200))
	// Fill a triangle
	buf.FillPolygon(
		[]int{20, 60, 40},
		[]int{20, 20, 60},
		braille.RGBToXterm256(100, 200, 200),
	)
	fmt.Print(buf.Render())
}
