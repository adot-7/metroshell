package gtfs

import "sort"

// RouteStatus describes the outcome of a route request.
type RouteStatus uint8

const (
	RouteNoEndpoints RouteStatus = iota
	RouteLoading
	RouteReady
	RouteSameStation
	RouteUnreachable
	RouteInvalid
	RouteUnavailable
)

func (s RouteStatus) String() string {
	switch s {
	case RouteLoading:
		return "loading"
	case RouteReady:
		return "ready"
	case RouteSameStation:
		return "same station"
	case RouteUnreachable:
		return "unreachable"
	case RouteInvalid:
		return "invalid selection"
	case RouteUnavailable:
		return "unavailable"
	default:
		return "no endpoints"
	}
}

// RouteStep is one passenger-facing stop-to-stop hop. FamilyID is the
// canonical (lexicographically first) family serving that graph edge.
type RouteStep struct {
	FromStationID string
	ToStationID   string
	FamilyID      string
	FamilyName    string
	Color         string
}

// RouteResult is the immutable, renderer- and UI-ready result of planning.
// Stops counts stop-to-stop hops; Stations includes both endpoints.
type RouteResult struct {
	Status             RouteStatus
	FromStation        string
	ToStation          string
	Stations           []string
	StationIDs         []string // alias kept explicit for callers using graph IDs
	StationSequence    []string
	FamilyIDs          []string
	LineFamilyIDs      []string // canonical family sequence alias
	LineFamilySequence []string
	FamilyNames        []string
	Transfers          int
	TransferCount      int
	Stops              int
	StopCount          int
	Steps              []RouteStep
	Message            string
}

// PlanRoute runs deterministic breadth-first search over a prepared route
// graph. It minimizes stop-to-stop hops only, never travel time. Neighbors are
// ordered by station ID and each edge's families by family ID; this is the
// documented tie-break for equal-length routes. When an edge has several
// families, the first family is used for the canonical line sequence.
func PlanRoute(graph RouteGraph, from, to string) RouteResult {
	result := RouteResult{FromStation: from, ToStation: to}
	if from == "" || to == "" {
		result.Status = RouteNoEndpoints
		result.Message = "Select FROM and TO stations"
		return result
	}
	if _, ok := graph.Stations[from]; !ok {
		result.Status = RouteInvalid
		result.Message = "FROM station is not in the feed"
		return result
	}
	if _, ok := graph.Stations[to]; !ok {
		result.Status = RouteInvalid
		result.Message = "TO station is not in the feed"
		return result
	}
	if from == to {
		result.Status = RouteSameStation
		result.Stations = []string{from}
		result.StationIDs = append([]string(nil), result.Stations...)
		result.StationSequence = append([]string(nil), result.Stations...)
		result.Message = "FROM and TO are the same station"
		return result
	}

	type searchNode struct {
		station string
		path    []string
		steps   []RouteStep
	}
	queue := []searchNode{{station: from, path: []string{from}}}
	visited := map[string]bool{from: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		neighbors := append([]RouteEdge(nil), graph.Adjacency[current.station]...)
		sort.SliceStable(neighbors, func(i, j int) bool {
			if neighbors[i].ToStationID != neighbors[j].ToStationID {
				return neighbors[i].ToStationID < neighbors[j].ToStationID
			}
			return firstFamilyID(neighbors[i]) < firstFamilyID(neighbors[j])
		})
		for _, edge := range neighbors {
			next := edge.ToStationID
			if visited[next] {
				continue
			}
			step := canonicalStep(edge)
			path := append(append([]string(nil), current.path...), next)
			steps := append(append([]RouteStep(nil), current.steps...), step)
			if next == to {
				result.Status = RouteReady
				result.Stations = path
				result.StationIDs = append([]string(nil), path...)
				result.StationSequence = append([]string(nil), path...)
				result.Steps = steps
				result.Stops = len(steps)
				result.StopCount = result.Stops
				result.FamilyIDs = familyIDs(steps)
				result.LineFamilyIDs = append([]string(nil), result.FamilyIDs...)
				result.LineFamilySequence = append([]string(nil), result.FamilyIDs...)
				for _, step := range steps {
					result.FamilyNames = append(result.FamilyNames, step.FamilyName)
				}
				result.Transfers = countTransfers(result.FamilyIDs)
				result.TransferCount = result.Transfers
				result.Message = routeMessage(result)
				return result
			}
			visited[next] = true
			queue = append(queue, searchNode{station: next, path: path, steps: steps})
		}
	}
	result.Status = RouteUnreachable
	result.Message = "No route between selected stations"
	return result
}

func firstFamilyID(edge RouteEdge) string {
	if len(edge.FamilyIDs) > 0 {
		return edge.FamilyIDs[0]
	}
	if len(edge.Families) > 0 {
		ids := make([]string, 0, len(edge.Families))
		for _, family := range edge.Families {
			ids = append(ids, family.ID)
		}
		sort.Strings(ids)
		return ids[0]
	}
	return ""
}

func canonicalStep(edge RouteEdge) RouteStep {
	familyID := firstFamilyID(edge)
	step := RouteStep{FromStationID: edge.FromStationID, ToStationID: edge.ToStationID, FamilyID: familyID}
	for _, family := range edge.Families {
		if family.ID == familyID {
			step.FamilyName = family.DisplayName
			step.Color = family.RendererColor
			if step.Color == "" {
				step.Color = family.Color
			}
			break
		}
	}
	return step
}

func familyIDs(steps []RouteStep) []string {
	ids := make([]string, len(steps))
	for i, step := range steps {
		ids[i] = step.FamilyID
	}
	return ids
}

func countTransfers(families []string) int {
	transfers := 0
	for i := 1; i < len(families); i++ {
		if families[i] != families[i-1] {
			transfers++
		}
	}
	return transfers
}

func routeMessage(result RouteResult) string {
	return "Route: " + itoa(result.Stops) + " stops, " + itoa(result.Transfers) + " transfers"
}

// Kept local to avoid making formatting a dependency of the graph contract.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}
