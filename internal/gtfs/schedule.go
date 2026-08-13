package gtfs

import (
	"fmt"
	"sort"
	"time"
)

// DelhiLocation is the timezone used by static timetable calculations. GTFS
// times are service-day wall-clock values, not UTC timestamps.
var DelhiLocation = time.FixedZone("Asia/Kolkata", 5*60*60+30*60)

type ScheduleStatus uint8

const (
	ScheduleUnavailable ScheduleStatus = iota
	ScheduleAvailable
	ScheduleEstimated
)

func (s ScheduleStatus) String() string {
	switch s {
	case ScheduleAvailable:
		return "scheduled"
	case ScheduleEstimated:
		return "estimated scheduled"
	default:
		return "unavailable"
	}
}

// SchedulePolicy makes the expired-feed demo choice explicit. The checked-in
// Delhi feed ends 2025-12-31; carry-forward applies its weekly pattern after
// that date, but never upgrades the result to live or realtime information.
type SchedulePolicy struct {
	Location            *time.Location
	CarryForwardExpired bool
}

var DefaultSchedulePolicy = SchedulePolicy{Location: DelhiLocation, CarryForwardExpired: true}

type ScheduleStopDetail struct {
	StationID          string
	Arrival, Departure time.Time
}
type ScheduleLegDetail struct {
	FamilyID, From, To string
	Stops              int
	Departure, Arrival time.Time
	Status             ScheduleStatus
}

type JourneySchedule struct {
	Status        ScheduleStatus
	Message       string
	Duration      time.Duration
	NextDeparture time.Time
	NextArrival   time.Time
	Stops         []ScheduleStopDetail
	Legs          []ScheduleLegDetail
}

func (j JourneySchedule) Available() bool { return j.Status != ScheduleUnavailable }

// buildSchedules creates a deterministic schedule index without changing the
// graph or BFS route projection.
func buildSchedules(trips []Trip, lines LineIndex, stopTimes []StopTime, stopToStation map[string]string, _ []Calendar, _ []CalendarDate) (map[string]TripSchedule, error) {
	byTrip := make(map[string][]StopTime)
	for _, value := range stopTimes {
		byTrip[value.TripID] = append(byTrip[value.TripID], value)
	}
	result := make(map[string]TripSchedule, len(trips))
	for _, trip := range trips {
		stops := append([]StopTime(nil), byTrip[trip.ID]...)
		sort.Slice(stops, func(i, j int) bool { return stops[i].Sequence < stops[j].Sequence })
		if len(stops) < 2 {
			continue
		}
		if stops[0].ArrivalTime == "" || stops[0].DepartureTime == "" {
			continue
		}
		family := lines[trip.RouteID].FamilyID
		value := TripSchedule{TripID: trip.ID, ServiceID: trip.ServiceID, RouteID: trip.RouteID, FamilyID: family, ShapeID: trip.ShapeID}
		for _, stop := range stops {
			station := stopToStation[stop.StopID]
			if station == "" {
				return nil, fmt.Errorf("gtfs schedule: trip %q references missing station for stop %q", trip.ID, stop.StopID)
			}
			value.Stops = append(value.Stops, ScheduledStop{StopID: stop.StopID, StationID: station, Sequence: stop.Sequence, ArrivalSeconds: stop.ArrivalSeconds, DepartureSeconds: stop.DepartureSeconds})
		}
		result[trip.ID] = value
	}
	return result, nil
}

// PlanSchedule computes the next feasible scheduled itinerary on the already
// selected route. It never performs route search and therefore cannot alter
// BFS fewest-stops behavior.
func PlanSchedule(indexes Indexes, route RouteResult, now time.Time) JourneySchedule {
	return planSchedule(indexes, route, now, DefaultSchedulePolicy)
}

func PlanScheduledJourney(indexes Indexes, route RouteResult, now time.Time, policy SchedulePolicy) JourneySchedule {
	return planSchedule(indexes, route, now, policy)
}

func planSchedule(indexes Indexes, route RouteResult, now time.Time, policy SchedulePolicy) JourneySchedule {
	if route.Status != RouteReady || len(route.Stations) < 2 {
		return JourneySchedule{Status: ScheduleUnavailable, Message: "Scheduled service unavailable for this route"}
	}
	if policy.Location == nil {
		policy.Location = DelhiLocation
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(policy.Location)
	schedules := indexes.Schedules
	if len(schedules) == 0 {
		return JourneySchedule{Status: ScheduleUnavailable, Message: "Scheduled times unavailable in this feed"}
	}

	// Each leg is planned independently so a transfer must have a compatible
	// later departure. Candidate dates include the next week and can cross 24:00.
	legDetails := make([]ScheduleLegDetail, 0, len(route.Legs))
	current := now
	var firstDeparture, finalArrival time.Time
	status := ScheduleAvailable
	allStops := make([]ScheduleStopDetail, 0, len(route.Stations))
	for legIndex, leg := range route.Legs {
		path := route.Stations[stationIndex(route, leg.From) : stationIndex(route, leg.To)+1]
		candidate, ok := nextLeg(schedules, indexes.Calendar, indexes.CalendarDates, path, leg.FamilyID, current, policy)
		if !ok {
			return JourneySchedule{Status: ScheduleUnavailable, Message: "No compatible scheduled service for this route"}
		}
		if legIndex == 0 {
			firstDeparture = candidate.departure
		}
		finalArrival = candidate.arrival
		current = candidate.arrival
		legDetails = append(legDetails, ScheduleLegDetail{FamilyID: leg.FamilyID, From: leg.From, To: leg.To, Stops: leg.Stops, Departure: candidate.departure, Arrival: candidate.arrival, Status: candidate.status})
		if candidate.status == ScheduleEstimated {
			status = ScheduleEstimated
		}
		if legIndex == 0 {
			allStops = append(allStops, candidate.stops...)
		} else if len(candidate.stops) > 0 {
			allStops = append(allStops, candidate.stops[1:]...)
		}
	}
	return JourneySchedule{Status: status, Message: scheduleMessage(status), Duration: finalArrival.Sub(firstDeparture), NextDeparture: firstDeparture, NextArrival: finalArrival, Stops: allStops, Legs: legDetails}
}

type legCandidate struct {
	tripID             string
	departure, arrival time.Time
	stops              []ScheduleStopDetail
	status             ScheduleStatus
}

func nextLeg(schedules map[string]TripSchedule, calendars []Calendar, exceptions []CalendarDate, path []string, family string, after time.Time, policy SchedulePolicy) (legCandidate, bool) {
	best := legCandidate{}
	for _, schedule := range schedules {
		if family != "" && schedule.FamilyID != family {
			continue
		}
		for start := 0; start+len(path) <= len(schedule.Stops); start++ {
			match := true
			for i := range path {
				if schedule.Stops[start+i].StationID != path[i] {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			for day := 0; day <= 8; day++ {
				date := after.In(policy.Location).AddDate(0, 0, day)
				active, estimated := serviceActive(schedule.ServiceID, date, calendars, exceptions, policy)
				if !active {
					continue
				}
				departure := serviceTime(date, schedule.Stops[start].DepartureSeconds, policy.Location)
				arrival := serviceTime(date, schedule.Stops[start+len(path)-1].ArrivalSeconds, policy.Location)
				if arrival.Before(departure) {
					arrival = arrival.AddDate(0, 0, 1)
				}
				if departure.Before(after) {
					continue
				}
				if best.departure.IsZero() || departure.Before(best.departure) || (departure.Equal(best.departure) && schedule.TripID < best.tripID) {
					details := make([]ScheduleStopDetail, 0, len(path))
					for i := range path {
						stop := schedule.Stops[start+i]
						details = append(details, ScheduleStopDetail{StationID: stop.StationID, Arrival: serviceTime(date, stop.ArrivalSeconds, policy.Location), Departure: serviceTime(date, stop.DepartureSeconds, policy.Location)})
					}
					best = legCandidate{tripID: schedule.TripID, departure: departure, arrival: arrival, stops: details, status: mapScheduleStatus(estimated)}
				}
			}
		}
	}
	return best, !best.departure.IsZero()
}

func serviceTime(date time.Time, seconds int, location *time.Location) time.Time {
	y, m, d := date.In(location).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, location).Add(time.Duration(seconds) * time.Second)
}
func mapScheduleStatus(estimated bool) ScheduleStatus {
	if estimated {
		return ScheduleEstimated
	}
	return ScheduleAvailable
}
func scheduleMessage(status ScheduleStatus) string {
	if status == ScheduleEstimated {
		return "Scheduled service · weekly pattern carried forward beyond feed expiry"
	}
	return "Scheduled service · static GTFS timetable"
}

func serviceActive(serviceID string, date time.Time, calendars []Calendar, exceptions []CalendarDate, policy SchedulePolicy) (bool, bool) {
	date = date.In(policy.Location)
	for _, exception := range exceptions {
		if exception.ServiceID == serviceID && sameDate(exception.Date, date) {
			return exception.ExceptionType == 1, false
		}
	}
	for _, calendar := range calendars {
		if calendar.ServiceID != serviceID {
			continue
		}
		inRange := !date.Before(calendar.StartDate.In(policy.Location)) && !date.After(calendar.EndDate.In(policy.Location))
		if inRange {
			return calendarWeekday(calendar, date.Weekday()), false
		}
		if policy.CarryForwardExpired && date.After(calendar.EndDate.In(policy.Location)) {
			return calendarWeekday(calendar, date.Weekday()), true
		}
		return false, false
	}
	// Feeds without calendar tables are accepted but their service day is an
	// explicit estimate: a trip's weekly applicability cannot be proven.
	return true, len(calendars) == 0
}
func calendarWeekday(c Calendar, day time.Weekday) bool {
	switch day {
	case time.Monday:
		return c.Monday
	case time.Tuesday:
		return c.Tuesday
	case time.Wednesday:
		return c.Wednesday
	case time.Thursday:
		return c.Thursday
	case time.Friday:
		return c.Friday
	case time.Saturday:
		return c.Saturday
	default:
		return c.Sunday
	}
}
func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
func stationIndex(route RouteResult, station string) int {
	for i, value := range route.Stations {
		if value == station {
			return i
		}
	}
	return -1
}
