package style

import "github.com/adot-7/metroshell/internal/braille"

type LayerStyle struct {
	DrawFill    bool
	DrawLine    bool
	FillColor   int
	LineColor   int
	DrawLabel   bool
	LabelColor  int
	LabelSymbol string // if non-empty, render this char instead of the feature name
	MinZoom     int
	MaxZoom     int
	Priority    int
}

// Palette notes:
//   - All colors are tuned for readability on AMOLED black backgrounds.
//   - General tone shifts toward #84a59d (sage teal, xterm~109) — less yellow,
//     more cool blue-greens and muted warm accents.
var styles = map[string]LayerStyle{

	// ── Water ──────────────────────────────────────────────────────────────
	"water": {
		DrawFill:  true,
		FillColor: braille.RGBToXterm256(35, 110, 195), // deep vivid blue
		MinZoom:   0, MaxZoom: 22, Priority: 10,
	},
	"waterway": {
		DrawLine:  true,
		LineColor: braille.RGBToXterm256(50, 130, 210),
		MinZoom:   8, MaxZoom: 22, Priority: 11,
	},

	// ── Land cover ─────────────────────────────────────────────────────────
	"landcover/wood": {
		DrawFill:  true,
		FillColor: braille.RGBToXterm256(45, 140, 75), // vivid teal-green on black
		MinZoom:   7, MaxZoom: 22, Priority: 5,
	},
	"landcover/grass": {
		DrawFill:  true,
		FillColor: braille.RGBToXterm256(95, 170, 130), // sage green
		MinZoom:   7, MaxZoom: 22, Priority: 5,
	},

	// ── Roads — geometry only ───────────────────────────────────────────────
	"transportation/motorway": {
		DrawLine:  true,
		LineColor: braille.RGBToXterm256(210, 75, 45),
		MinZoom:   5, MaxZoom: 22, Priority: 30,
	},
	"transportation/trunk": {
		DrawLine:  true,
		LineColor: braille.RGBToXterm256(190, 125, 40),
		MinZoom:   6, MaxZoom: 22, Priority: 29,
	},
	"transportation/primary": {
		DrawLine:  true,
		LineColor: braille.RGBToXterm256(160, 145, 65),
		MinZoom:   8, MaxZoom: 22, Priority: 28,
	},
	"transportation/secondary": {
		DrawLine:  true,
		LineColor: braille.RGBToXterm256(132, 165, 157), // #84a59d — sage teal
		MinZoom:   10, MaxZoom: 22, Priority: 27,
	},
	"transportation/residential": {
		DrawLine:  true,
		LineColor: braille.RGBToXterm256(90, 120, 115),
		MinZoom:   12, MaxZoom: 22, Priority: 26,
	},
	"transportation/service": {
		DrawLine:  true,
		LineColor: braille.RGBToXterm256(65, 90, 85),
		MinZoom:   13, MaxZoom: 22, Priority: 25,
	},
	"transportation/rail": {
		DrawLine:  true,
		LineColor: braille.RGBToXterm256(80, 100, 100),
		MinZoom:   8, MaxZoom: 22, Priority: 20,
	},

	// ── Buildings ─────────────────────────────────────────────────────────
	// Dark cool grey — subtle texture on AMOLED black.
	"building": {
		DrawFill: true, DrawLine: true,
		FillColor: braille.RGBToXterm256(50, 55, 65),
		LineColor: braille.RGBToXterm256(70, 78, 90),
		MinZoom:   13, MaxZoom: 22, Priority: 40,
	},

	// ── Road name labels (separate layer in this tile set) ─────────────────
	// "transportation_name/motorway": {
	// 	DrawLabel:  true,
	// 	LabelColor: braille.RGBToXterm256(210, 75, 45),
	// 	MinZoom:    7, MaxZoom: 22, Priority: 30,
	// },
	// "transportation_name/trunk": {
	// 	DrawLabel:  true,
	// 	LabelColor: braille.RGBToXterm256(190, 125, 40),
	// 	MinZoom:    8, MaxZoom: 22, Priority: 29,
	// },
	// "transportation_name/primary": {
	// 	DrawLabel:  true,
	// 	LabelColor: braille.RGBToXterm256(160, 145, 65),
	// 	MinZoom:    10, MaxZoom: 22, Priority: 28,
	// },
	// "transportation_name/secondary": {
	// 	DrawLabel:  true,
	// 	LabelColor: braille.RGBToXterm256(132, 165, 157),
	// 	MinZoom:    12, MaxZoom: 22, Priority: 27,
	// },
	// "transportation_name": {
	// 	DrawLabel:  true,
	// 	LabelColor: braille.RGBToXterm256(110, 140, 135),
	// 	MinZoom:    13, MaxZoom: 22, Priority: 25,
	// },

	// ── Place names ────────────────────────────────────────────────────────
	// Light teal-white — high contrast on black, cool tone.
	"place": {
		DrawLabel:  true,
		LabelColor: braille.RGBToXterm256(185, 220, 210),
		MinZoom:    8, MaxZoom: 22, Priority: 60,
	},

	// ── POI allowlist ───────────────────────────────────────────────────────
	//
	// Transit — metro / subway
	// "poi/railway/subway": {
	// 	DrawLabel: true, LabelSymbol: "M",
	// 	LabelColor: braille.RGBToXterm256(0, 200, 230),
	// 	MinZoom:    12, MaxZoom: 22, Priority: 72,
	// },
	// Transit — mainline train stations (standard OMT class)
	"poi/railway/station": {
		DrawLabel: true, LabelSymbol: "M",
		LabelColor: braille.RGBToXterm256(110, 165, 245),
		MinZoom:    12, MaxZoom: 22, Priority: 71,
	},
	// Transit — any other railway poi (tram, halt, monorail …)
	"poi/railway": {
		DrawLabel: true, LabelSymbol: "T",
		LabelColor: braille.RGBToXterm256(100, 150, 230),
		MinZoom:    13, MaxZoom: 22, Priority: 68,
	},

	// Health — hospitals
	// Covers both naming conventions: OpenMapTiles standard ("poi/hospital")
	// and the class/subclass hierarchy ("poi/health/hospital").
	"poi/hospital": {
		DrawLabel: true, LabelSymbol: "+",
		LabelColor: braille.RGBToXterm256(235, 70, 70),
		MinZoom:    13, MaxZoom: 22, Priority: 67,
	},
	// "poi/health/hospital": {
	// 	DrawLabel: true, LabelSymbol: "+",
	// 	LabelColor: braille.RGBToXterm256(235, 70, 70),
	// 	MinZoom: 12, MaxZoom: 22, Priority: 66,
	// },
	// "poi/health": {
	// 	DrawLabel: true, LabelSymbol: "+",
	// 	LabelColor: braille.RGBToXterm256(220, 120, 120),
	// 	MinZoom: 14, MaxZoom: 22, Priority: 62,
	// },
	// // Pharmacy
	// "poi/pharmacy": {
	// 	DrawLabel: true, LabelSymbol: "+",
	// 	LabelColor: braille.RGBToXterm256(200, 160, 220),
	// 	MinZoom: 14, MaxZoom: 22, Priority: 60,
	// },

	// Food — covers both OMT standard names and alternative naming
	"poi/restaurant": {
		DrawLabel: true, LabelSymbol: "🍴",
		LabelColor: braille.RGBToXterm256(225, 165, 45),
		MinZoom:    15, MaxZoom: 22, Priority: 50,
	},
	"poi/cafe": {
		DrawLabel: true, LabelSymbol: "🥐",
		LabelColor: braille.RGBToXterm256(220, 155, 55),
		MinZoom:    15, MaxZoom: 22, Priority: 50,
	},
	"poi/fast_food": {
		DrawLabel: true, LabelSymbol: "🍜",
		LabelColor: braille.RGBToXterm256(215, 145, 45),
		MinZoom:    15, MaxZoom: 22, Priority: 50,
	},
	"poi/food": {
		DrawLabel: true, LabelSymbol: "🍲",
		LabelColor: braille.RGBToXterm256(225, 165, 45),
		MinZoom:    15, MaxZoom: 22, Priority: 50,
	},

	// Fuel / gas stations
	"poi/fuel": {
		DrawLabel: true, LabelSymbol: "⛽",
		LabelColor: braille.RGBToXterm256(65, 210, 75),
		MinZoom:    14, MaxZoom: 22, Priority: 52,
	},
}

// StyleFor returns the rendering style for a given layer name and class.
// For POI subclass lookups, pass class as "parentClass/subclass".
func StyleFor(layerName, class string, zoom int) (LayerStyle, bool) {
	key := layerName
	if class != "" {
		key = layerName + "/" + class
	}

	if s, ok := styles[key]; ok && zoom >= s.MinZoom && zoom <= s.MaxZoom {
		return s, true
	}
	if class != "" {
		if s, ok := styles[layerName]; ok && zoom >= s.MinZoom && zoom <= s.MaxZoom {
			return s, true
		}
	}
	return LayerStyle{}, false
}
