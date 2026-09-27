package route

import (
	"container/heap"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/wyw14/solo-036/internal/model"
)

// Segment 是 model.Segment 的别名，保留 route 包既有调用习惯。
type Segment = model.Segment

type Network struct {
	stations map[string]model.Station
	segments map[string]Segment
	outgoing map[string][]string
}

type Path struct {
	Stations        []string `json:"stations"`
	Segments        []string `json:"segments"`
	Length          float64  `json:"length"`
	MaximumPressure float64  `json:"maximum_pressure"`
}

type pathCandidate struct {
	station  string
	distance float64
	pressure float64
	stations []string
	segments []string
	index    int
}

type candidateHeap []*pathCandidate

func (h candidateHeap) Len() int           { return len(h) }
func (h candidateHeap) Less(i, j int) bool { return h[i].distance < h[j].distance }
func (h candidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *candidateHeap) Push(value any) {
	item := value.(*pathCandidate)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *candidateHeap) Pop() any {
	old := *h
	count := len(old)
	item := old[count-1]
	old[count-1] = nil
	item.index = -1
	*h = old[:count-1]
	return item
}

// NewNetwork 依据一份线路版本内容构造拓扑；构造前复用领域校验，拒绝非法配置。
func NewNetwork(stations []model.Station, segments []Segment) (*Network, error) {
	if err := model.ValidateLineContent(stations, segments); err != nil {
		return nil, err
	}
	network := &Network{stations: make(map[string]model.Station), segments: make(map[string]Segment), outgoing: make(map[string][]string)}
	for _, station := range stations {
		network.stations[station.ID] = station
	}
	for _, segment := range segments {
		network.segments[segment.ID] = segment
		network.outgoing[segment.From] = append(network.outgoing[segment.From], segment.ID)
		sort.Strings(network.outgoing[segment.From])
	}
	return network, nil
}

func (n *Network) Stations() []model.Station {
	out := make([]model.Station, 0, len(n.stations))
	for _, station := range n.stations {
		out = append(out, station)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kilometer == out[j].Kilometer {
			return out[i].ID < out[j].ID
		}
		return out[i].Kilometer < out[j].Kilometer
	})
	return out
}

func (n *Network) Segments() []Segment {
	out := make([]Segment, 0, len(n.segments))
	for _, segment := range n.segments {
		out = append(out, segment)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (n *Network) Path(from, to string, requestedPressure float64) (Path, error) {
	if _, ok := n.stations[from]; !ok {
		return Path{}, fmt.Errorf("unknown source station %s", from)
	}
	if _, ok := n.stations[to]; !ok {
		return Path{}, fmt.Errorf("unknown destination station %s", to)
	}
	if requestedPressure <= 0 {
		return Path{}, errors.New("requested pressure must be positive")
	}
	queue := candidateHeap{}
	heap.Init(&queue)
	heap.Push(&queue, &pathCandidate{station: from, distance: 0, pressure: requestedPressure, stations: []string{from}, segments: []string{}})
	best := make(map[string]float64)
	for queue.Len() > 0 {
		candidate := heap.Pop(&queue).(*pathCandidate)
		if distance, ok := best[candidate.station]; ok && distance <= candidate.distance {
			continue
		}
		best[candidate.station] = candidate.distance
		if candidate.station == to {
			return Path{Stations: candidate.stations, Segments: candidate.segments, Length: candidate.distance, MaximumPressure: candidate.pressure}, nil
		}
		for _, segmentID := range n.outgoing[candidate.station] {
			segment := n.segments[segmentID]
			if !segment.Enabled || requestedPressure > segment.MaximumPressure {
				continue
			}
			pressure := candidate.pressure
			if segment.MaximumPressure < pressure {
				pressure = segment.MaximumPressure
			}
			heap.Push(&queue, &pathCandidate{station: segment.To, distance: candidate.distance + segment.Length, pressure: pressure, stations: appendCopy(candidate.stations, segment.To), segments: appendCopy(candidate.segments, segment.ID)})
		}
	}
	return Path{}, fmt.Errorf("no enabled route from %s to %s at pressure %.2f", from, to, requestedPressure)
}

func appendCopy(values []string, value string) []string {
	out := make([]string, len(values), len(values)+1)
	copy(out, values)
	return append(out, value)
}

func (n *Network) RouteStates() []model.RouteState {
	launchers := make([]model.Station, 0)
	receivers := make([]model.Station, 0)
	for _, station := range n.Stations() {
		if station.Role == model.StationLauncher {
			launchers = append(launchers, station)
		}
		if station.Role == model.StationReceiver {
			receivers = append(receivers, station)
		}
	}
	states := make([]model.RouteState, 0)
	for _, launcher := range launchers {
		for _, receiver := range receivers {
			path, err := n.Path(launcher.ID, receiver.ID, minimumPressure(launcher.PressureLimit, receiver.PressureLimit))
			if err != nil || len(path.Segments) == 0 {
				continue
			}
			states = append(states, model.RouteState{ID: "main-" + launcher.ID + "-" + receiver.ID, FromStation: launcher.ID, ToStation: receiver.ID, UpdatedAt: time.Now().UTC()})
		}
	}
	return states
}

func minimumPressure(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
