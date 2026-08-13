package app

import (
	"os"
	"testing"
	"time"

	"github.com/adot-7/metroshell/internal/sim"
)

func BenchmarkCachedExtractedSnapshot(b *testing.B) {
	path := os.Getenv("METROSHELL_GTFS_FEED")
	if path == "" {
		b.Skip("METROSHELL_GTFS_FEED is not set")
	}
	_, indexes, missing, err := loadFeed(b.Context(), path)
	if missing || err != nil {
		b.Fatalf("load feed: missing=%v err=%v", missing, err)
	}
	now := time.Date(2025, 8, 13, 9, 7, 0, 0, time.FixedZone("Asia/Kolkata", 19800))
	m := NewWithConfig(nil, 28.6, 77.2, Config{TrainAcceleration: 20, Now: func() time.Time { return now }})
	m.feedIndexes, m.feedState, m.feedSeq = indexes, FeedStateReady, 1
	_ = m.SimulationSnapshot()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.trainClock = int64(i)
		_ = m.SimulationSnapshot()
	}
}

func BenchmarkCachedExtractedRoutes(b *testing.B) {
	path := os.Getenv("METROSHELL_GTFS_FEED")
	if path == "" {
		b.Skip("METROSHELL_GTFS_FEED is not set")
	}
	_, indexes, missing, err := loadFeed(b.Context(), path)
	if missing || err != nil {
		b.Fatalf("load feed: missing=%v err=%v", missing, err)
	}
	now := time.Date(2025, 8, 13, 9, 7, 0, 0, time.FixedZone("Asia/Kolkata", 19800))
	m := NewWithConfig(nil, 28.6, 77.2, Config{TrainAcceleration: 20, Now: func() time.Time { return now }})
	m.feedIndexes, m.feedState, m.feedSeq = indexes, FeedStateReady, 1
	_ = m.cachedSimulationRoutes(now)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.cachedSimulationRoutes(now)
	}
}

func BenchmarkUncachedExtractedRoutes(b *testing.B) {
	path := os.Getenv("METROSHELL_GTFS_FEED")
	if path == "" {
		b.Skip("METROSHELL_GTFS_FEED is not set")
	}
	_, indexes, missing, err := loadFeed(b.Context(), path)
	if missing || err != nil {
		b.Fatalf("load feed: missing=%v err=%v", missing, err)
	}
	now := time.Date(2025, 8, 13, 9, 7, 0, 0, time.FixedZone("Asia/Kolkata", 19800))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = simulationRoutes(indexes, now)
	}
}

func BenchmarkUncachedExtractedSnapshot(b *testing.B) {
	path := os.Getenv("METROSHELL_GTFS_FEED")
	if path == "" {
		b.Skip("METROSHELL_GTFS_FEED is not set")
	}
	_, indexes, missing, err := loadFeed(b.Context(), path)
	if missing || err != nil {
		b.Fatalf("load feed: missing=%v err=%v", missing, err)
	}
	now := time.Date(2025, 8, 13, 9, 7, 0, 0, time.FixedZone("Asia/Kolkata", 19800))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sim.Snapshot(sim.Config{Seed: 41, Clock: int64(i), Fleet: 24, Acceleration: 20, Routes: simulationRoutes(indexes, now)})
	}
}

func BenchmarkPreparedExtractedSnapshotOnly(b *testing.B) {
	path := os.Getenv("METROSHELL_GTFS_FEED")
	if path == "" {
		b.Skip("METROSHELL_GTFS_FEED is not set")
	}
	_, indexes, missing, err := loadFeed(b.Context(), path)
	if missing || err != nil {
		b.Fatalf("load feed: missing=%v err=%v", missing, err)
	}
	now := time.Date(2025, 8, 13, 9, 7, 0, 0, time.FixedZone("Asia/Kolkata", 19800))
	routes := simulationRoutes(indexes, now)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sim.Snapshot(sim.Config{Seed: 41, Clock: int64(i), Fleet: 24, Acceleration: 20, Routes: routes})
	}
}
