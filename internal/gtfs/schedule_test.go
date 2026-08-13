package gtfs

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDelhiScheduleCarriesExpiredWeeklyCalendarAsEstimatedScheduledService(t *testing.T) {
	feed, err := Load(context.Background(), os.DirFS("testdata/scheduled-mini"))
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatal(err)
	}
	route := PlanRoute(indexes.Graph, "a", "c")
	if route.Status != RouteReady || route.Stops != 2 {
		t.Fatalf("route = %#v", route)
	}
	now := time.Date(2026, 1, 5, 7, 0, 0, 0, DelhiLocation)
	journey := PlanScheduledJourney(indexes, route, now, DefaultSchedulePolicy)
	if journey.Status != ScheduleEstimated {
		t.Fatalf("status=%v want estimated carry-forward", journey.Status)
	}
	if journey.NextDeparture.Format("15:04:05") != "08:00:00" || journey.NextArrival.Format("15:04:05") != "08:20:00" || journey.Duration != 20*time.Minute {
		t.Fatalf("journey=%#v", journey)
	}
	if journey.Message == "" || journey.Message == "live" {
		t.Fatalf("message=%q", journey.Message)
	}
}

func TestScheduleTransferRequiresCompatibleLaterTrip(t *testing.T) {
	feed, err := Load(context.Background(), os.DirFS("testdata/scheduled-mini"))
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatal(err)
	}
	route := PlanRoute(indexes.Graph, "a", "d")
	journey := PlanScheduledJourney(indexes, route, time.Date(2025, 1, 6, 8, 5, 0, 0, DelhiLocation), SchedulePolicy{Location: DelhiLocation})
	if journey.Status != ScheduleUnavailable {
		t.Fatalf("status=%v want unavailable", journey.Status)
	}
}

func TestScheduleWithoutCalendarIsExplicitEstimate(t *testing.T) {
	feed, err := Load(context.Background(), os.DirFS("testdata/delhi-mini"))
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := BuildIndexes(feed)
	if err != nil {
		t.Fatal(err)
	}
	route := PlanRoute(indexes.Graph, "dwarka_21", "yamuna_bank")
	journey := PlanScheduledJourney(indexes, route, time.Date(2025, 1, 6, 7, 0, 0, 0, DelhiLocation), SchedulePolicy{Location: DelhiLocation})
	if journey.Status != ScheduleEstimated {
		t.Fatalf("status=%v want estimated", journey.Status)
	}
}
