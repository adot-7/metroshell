package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/adot-7/metroshell/internal/gtfs"
)

// Feed-loading messages are deliberately private: callers start loading with
// Init and observe the resulting state through the model, just like any other
// Bubble Tea command/message transition.
type feedReadyMsg struct {
	seq     uint64
	feed    gtfs.Feed
	indexes gtfs.Indexes
}

type feedMissingMsg struct{ seq uint64 }

type feedErrorMsg struct {
	seq uint64
	err error
}

func (m Model) loadFeedCmd() tea.Cmd {
	path := m.gtfsPath
	seq := m.feedSeq
	return func() tea.Msg {
		feed, indexes, missing, err := loadFeed(context.Background(), path)
		if missing {
			return feedMissingMsg{seq: seq}
		}
		if err != nil {
			return feedErrorMsg{seq: seq, err: err}
		}
		return feedReadyMsg{seq: seq, feed: feed, indexes: indexes}
	}
}

func loadFeed(ctx context.Context, path string) (gtfs.Feed, gtfs.Indexes, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return gtfs.Feed{}, gtfs.Indexes{}, true, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return gtfs.Feed{}, gtfs.Indexes{}, true, nil
		}
		return gtfs.Feed{}, gtfs.Indexes{}, false, fmt.Errorf("gtfs feed %q: %w", path, err)
	}

	var feed gtfs.Feed
	if info.IsDir() {
		feed, err = gtfs.Load(ctx, os.DirFS(path))
	} else {
		feed, err = loadZIP(ctx, path, info.Size())
	}
	if err != nil {
		return gtfs.Feed{}, gtfs.Indexes{}, false, err
	}

	indexes, err := gtfs.BuildIndexes(feed)
	if err != nil {
		return gtfs.Feed{}, gtfs.Indexes{}, false, err
	}
	return feed, indexes, false, nil
}

func loadZIP(ctx context.Context, path string, size int64) (gtfs.Feed, error) {
	file, err := os.Open(path)
	if err != nil {
		return gtfs.Feed{}, fmt.Errorf("gtfs feed %q: open: %w", path, err)
	}
	defer file.Close()

	return gtfs.LoadZIP(ctx, file, size)
}

func compactError(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if message == "" {
		return "unknown error"
	}
	const maxLength = 48
	if len([]rune(message)) <= maxLength {
		return message
	}
	return string([]rune(message)[:maxLength-1]) + "…"
}
