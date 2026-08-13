package style

import (
	"testing"

	"github.com/adot-7/metroshell/internal/braille"
)

func TestIdlePaletteRetainsAMOLEDDepthColors(t *testing.T) {
	want := map[string]int{
		"water":                    braille.RGBToXterm256(35, 110, 195),
		"waterway":                 braille.RGBToXterm256(50, 130, 210),
		"landcover/wood":           braille.RGBToXterm256(45, 140, 75),
		"landcover/grass":          braille.RGBToXterm256(95, 170, 130),
		"transportation/motorway":  braille.RGBToXterm256(210, 75, 45),
		"transportation/trunk":     braille.RGBToXterm256(190, 125, 40),
		"transportation/primary":   braille.RGBToXterm256(160, 145, 65),
		"transportation/secondary": braille.RGBToXterm256(132, 165, 157),
	}
	for layer, expected := range want {
		colorField := "fill"
		if layer != "water" && layer != "landcover/wood" && layer != "landcover/grass" {
			colorField = "line"
		}
		got, ok := StyleFor(layer, "", 15)
		if !ok {
			t.Fatalf("StyleFor(%q) did not return an idle style", layer)
		}
		color := got.FillColor
		if colorField == "line" {
			color = got.LineColor
		}
		if color != expected {
			t.Errorf("%s color = %d, want %d", layer, color, expected)
		}
	}
}
