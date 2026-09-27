package route

import (
	"container/heap"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/wyw14/solo-036/internal/model"
)

// Segment is the routing-graph view of a versioned line segment.
type Segment = model.LineSegment

type Network struct {
	stations map[string]model.Station
	segments map[string]Segment
	outgoing map[string][]string
	config   model.LineConfig
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

func NewNetwork(stations []model.Station, segments []Segment) (*Network, error) {
	network := &Network{
		stations: make(map[string]model.Station),
		segments: make(map[string]Segment),
		outgoing: make(map[string][]string),
		config:   model.LineConfig{Stations: stations, Segments: segments},
	}
	for _, station := range stations {
		if err := station.Validate(); err != nil {
			return nil, err
		}
		if _, exists := network.stations[station.ID]; exists {
			return nil, fmt.Errorf("duplicate station %s", station.ID)
		}
		network.stations[station.ID] = station
	}
	for _, segment := range segments {
		if err := network.addSegment(segment); err != nil {
			return nil, err
		}
	}
	if err := network.Validate(); err != nil {
		return nil, err
	}
	return network, nil
}

// NetworkFromConfig builds a validated network from a versioned line config.
func NetworkFromConfig(config model.LineConfig) (*Network, error) {
	return NewNetwork(config.Stations, config.Segments)
}

// Config returns the station and segment content the network was built from.
func (n *Network) Config() model.LineConfig {
	return model.LineConfig{
		Stations: append([]model.Station(nil), n.config.Stations...),
		Segments: append([]Segment(nil), n.config.Segments...),
	}
}

// CheckReachability verifies that every enabled launcher can reach every
// enabled receiver at the minimum of their pressure limits using only
// enabled segments. It returns nil when the network is operationally usable.
func (n *Network) CheckReachability() error {
	launchers := make([]model.Station, 0)
	receivers := make([]model.Station, 0)
	for _, station := range n.Stations() {
		if !station.Enabled {
			continue
		}
		switch station.Role {
		case model.StationLauncher:
			launchers = append(launchers, station)
		case model.StationReceiver:
			receivers = append(receivers, station)
		}
	}
	if len(launchers) == 0 || len(receivers) == 0 {
		return errors.New("network needs an enabled launcher and receiver")
	}
	for _, launcher := range launchers {
		for _, receiver := range receivers {
			path, err := n.Path(launcher.ID, receiver.ID, minimumPressure(launcher.PressureLimit, receiver.PressureLimit))
			if err != nil {
				return fmt.Errorf("no enabled route from %s to %s: %w", launcher.ID, receiver.ID, err)
			}
			if len(path.Segments) == 0 {
				return fmt.Errorf("no enabled route from %s to %s", launcher.ID, receiver.ID)
			}
		}
	}
	return nil
}

func (n *Network) addSegment(segment Segment) error {
	if segment.ID == "" || segment.From == "" || segment.To == "" {
		return errors.New("segment identity is incomplete")
	}
	if _, exists := n.segments[segment.ID]; exists {
		return fmt.Errorf("duplicate segment %s", segment.ID)
	}
	if _, ok := n.stations[segment.From]; !ok {
		return fmt.Errorf("segment %s has unknown source station", segment.ID)
	}
	if _, ok := n.stations[segment.To]; !ok {
		return fmt.Errorf("segment %s has unknown destination station", segment.ID)
	}
	if segment.From == segment.To {
		return fmt.Errorf("segment %s loops to its source", segment.ID)
	}
	if segment.Length <= 0 {
		return fmt.Errorf("segment %s length must be positive", segment.ID)
	}
	if segment.MaximumPressure <= 0 {
		return fmt.Errorf("segment %s pressure limit must be positive", segment.ID)
	}
	n.segments[segment.ID] = segment
	n.outgoing[segment.From] = append(n.outgoing[segment.From], segment.ID)
	sort.Strings(n.outgoing[segment.From])
	return nil
}

func (n *Network) Validate() error {
	if len(n.stations) < 2 {
		return errors.New("network needs at least two stations")
	}
	if len(n.segments) == 0 {
		return errors.New("network needs at least one segment")
	}
	launchers := 0
	receivers := 0
	for _, station := range n.stations {
		if station.Role == model.StationLauncher {
			launchers++
		}
		if station.Role == model.StationReceiver {
			receivers++
		}
	}
	if launchers == 0 || receivers == 0 {
		return errors.New("network needs a launcher and receiver")
	}
	for _, segment := range n.segments {
		from := n.stations[segment.From]
		to := n.stations[segment.To]
		if from.Kilometer >= to.Kilometer {
			return fmt.Errorf("segment %s does not advance downstream", segment.ID)
		}
		limit := from.PressureLimit
		if to.PressureLimit < limit {
			limit = to.PressureLimit
		}
		if segment.MaximumPressure > limit {
			return fmt.Errorf("segment %s limit exceeds adjacent station limit", segment.ID)
		}
	}
	return nil
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

// DefaultLineConfig is the line content the service boots with before any
// operator-published version exists.
func DefaultLineConfig() model.LineConfig {
	return model.LineConfig{
		Stations: []model.Station{
			{ID: "station-a", Name: "North Launcher", Role: model.StationLauncher, Kilometer: 0, PressureLimit: 12.5, Enabled: true},
			{ID: "station-b", Name: "Central Block Valve", Role: model.StationIntermediate, Kilometer: 86.4, PressureLimit: 11.8, Enabled: true},
			{ID: "station-c", Name: "South Receiver", Role: model.StationReceiver, Kilometer: 173.2, PressureLimit: 11.2, Enabled: true},
		},
		Segments: []Segment{
			{ID: "segment-a-b", From: "station-a", To: "station-b", Length: 86.4, MaximumPressure: 11.8, Enabled: true},
			{ID: "segment-b-c", From: "station-b", To: "station-c", Length: 86.8, MaximumPressure: 11.2, Enabled: true},
		},
	}
}

func DefaultNetwork() *Network {
	network, err := NewNetwork(DefaultLineConfig().Stations, DefaultLineConfig().Segments)
	if err != nil {
		panic(err)
	}
	return network
}
