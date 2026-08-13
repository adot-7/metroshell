package render

import (
	"testing"

	"github.com/adot-7/metroshell/internal/sim"
)

func BenchmarkRenderSnapshotNormal(b *testing.B) {
	benchmarkRenderSnapshot(b, 100, 30)
}

func BenchmarkRenderSnapshotWide(b *testing.B) {
	benchmarkRenderSnapshot(b, 180, 48)
}

func benchmarkRenderSnapshot(b *testing.B, width, height int) {
	route := sim.PrepareRoute(sim.Route{FamilyID: "blue", RouteID: "blue", ShapeID: "shape", Shape: []sim.Point{{Lon: 77.1, Lat: 28.5}, {Lon: 77.2, Lat: 28.6}, {Lon: 77.3, Lat: 28.7}}})
	trains := sim.Snapshot(sim.Config{Seed: 41, Fleet: 24, Routes: []sim.Route{route}})
	req := RenderRequest{PixelW: (width - 2) * 2, PixelH: (height - 2) * 4, Trains: trains}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Trains = sim.Snapshot(sim.Config{Seed: 41, Clock: int64(i), Fleet: 24, Routes: []sim.Route{route}})
		_ = Render(req)
	}
}
