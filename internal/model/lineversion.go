package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// LineSegment is a single directional pipeline section between two stations.
// It is the versioned counterpart of the routing graph edges.
type LineSegment struct {
	ID              string  `json:"id"`
	From            string  `json:"from"`
	To              string  `json:"to"`
	Length          float64 `json:"length"`
	MaximumPressure float64 `json:"maximum_pressure"`
	Enabled         bool    `json:"enabled"`
}

// LineConfig is the operator-entered content of a line version:
// stations, segments and the pressure limits carried on both.
type LineConfig struct {
	Stations []Station     `json:"stations"`
	Segments []LineSegment `json:"segments"`
}

// LineVersionStatus tracks the lifecycle of a line version.
type LineVersionStatus string

const (
	// LineVersionDraft is editable and not yet usable by operations.
	LineVersionDraft LineVersionStatus = "draft"
	// LineVersionPublished is immutable and may be the active line.
	LineVersionPublished LineVersionStatus = "published"
)

// LineVersionIssue describes one validation finding against a draft.
type LineVersionIssue struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// LineVersion is an immutable-once-published configuration revision.
// New operations bind to the active published version; existing runs keep
// pointing at the version that was active when they were created.
type LineVersion struct {
	ID          string             `json:"id"`
	Number      int                `json:"number"`
	Status      LineVersionStatus  `json:"status"`
	Name        string             `json:"name"`
	Notes       string             `json:"notes,omitempty"`
	Config      LineConfig         `json:"config"`
	Active      bool               `json:"active"`
	CreatedBy   string             `json:"created_by,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
	PublishedAt time.Time          `json:"published_at,omitempty"`
	PublishedBy string             `json:"published_by,omitempty"`
	LastValid   bool               `json:"last_valid"`
	LastChecked time.Time          `json:"last_checked,omitempty"`
	Issues      []LineVersionIssue `json:"issues,omitempty"`
}

// ValidationReport is returned when a draft is checked.
type ValidationReport struct {
	VersionID string             `json:"version_id"`
	Valid     bool               `json:"valid"`
	Issues    []LineVersionIssue `json:"issues"`
	CheckedAt time.Time          `json:"checked_at"`
}

func (s LineVersionStatus) Validate() error {
	switch s {
	case LineVersionDraft, LineVersionPublished:
		return nil
	default:
		return fmt.Errorf("invalid line version status %q", s)
	}
}

func (c LineConfig) Normalize() LineConfig {
	out := LineConfig{
		Stations: append([]Station(nil), c.Stations...),
		Segments: append([]LineSegment(nil), c.Segments...),
	}
	for i := range out.Stations {
		out.Stations[i].ID = strings.TrimSpace(out.Stations[i].ID)
		out.Stations[i].Name = strings.TrimSpace(out.Stations[i].Name)
	}
	for i := range out.Segments {
		out.Segments[i].ID = strings.TrimSpace(out.Segments[i].ID)
		out.Segments[i].From = strings.TrimSpace(out.Segments[i].From)
		out.Segments[i].To = strings.TrimSpace(out.Segments[i].To)
	}
	return out
}

// Validate performs content-only checks shared by drafts and the routing
// graph. Reachability between launchers and receivers is checked separately
// because it requires path finding over the segment graph.
func (v LineVersion) ValidateContent() []LineVersionIssue {
	issues := make([]LineVersionIssue, 0)
	add := func(path, message string) {
		issues = append(issues, LineVersionIssue{Path: path, Message: message})
	}
	config := v.Config.Normalize()
	if len(config.Stations) == 0 {
		add("stations", "at least one station is required")
	}
	if len(config.Segments) == 0 {
		add("segments", "at least one segment is required")
	}
	stationIDs := make(map[string]struct{}, len(config.Stations))
	launchers, receivers := 0, 0
	for i, station := range config.Stations {
		path := fmt.Sprintf("stations[%d]", i)
		if station.ID == "" {
			add(path+".id", "station id is required")
			continue
		}
		if _, exists := stationIDs[station.ID]; exists {
			add(path+".id", fmt.Sprintf("duplicate station %s", station.ID))
		}
		stationIDs[station.ID] = struct{}{}
		if err := station.Validate(); err != nil {
			add(path, err.Error())
		}
		switch station.Role {
		case StationLauncher:
			launchers++
		case StationReceiver:
			receivers++
		}
	}
	if len(config.Stations) >= 2 && launchers == 0 {
		add("stations", "network needs a launcher station")
	}
	if len(config.Stations) >= 2 && receivers == 0 {
		add("stations", "network needs a receiver station")
	}
	segmentIDs := make(map[string]struct{}, len(config.Segments))
	for i, segment := range config.Segments {
		path := fmt.Sprintf("segments[%d]", i)
		if segment.ID == "" || segment.From == "" || segment.To == "" {
			add(path, "segment identity is incomplete")
			continue
		}
		if _, exists := segmentIDs[segment.ID]; exists {
			add(path+".id", fmt.Sprintf("duplicate segment %s", segment.ID))
		}
		segmentIDs[segment.ID] = struct{}{}
		if _, ok := stationIDs[segment.From]; !ok {
			add(path+".from", fmt.Sprintf("segment %s has unknown source station", segment.ID))
		}
		if _, ok := stationIDs[segment.To]; !ok {
			add(path+".to", fmt.Sprintf("segment %s has unknown destination station", segment.ID))
		}
		if segment.From == segment.To {
			add(path, fmt.Sprintf("segment %s loops to its source", segment.ID))
		}
		if segment.Length <= 0 {
			add(path+".length", fmt.Sprintf("segment %s length must be positive", segment.ID))
		}
		if segment.MaximumPressure <= 0 {
			add(path+".maximum_pressure", fmt.Sprintf("segment %s pressure limit must be positive", segment.ID))
		}
	}
	if len(issues) > 0 {
		return issues
	}
	stationByID := make(map[string]Station, len(config.Stations))
	for _, station := range config.Stations {
		stationByID[station.ID] = station
	}
	for _, segment := range config.Segments {
		from := stationByID[segment.From]
		to := stationByID[segment.To]
		if from.Kilometer >= to.Kilometer {
			add("", fmt.Sprintf("segment %s does not advance downstream", segment.ID))
			continue
		}
		limit := from.PressureLimit
		if to.PressureLimit < limit {
			limit = to.PressureLimit
		}
		if segment.MaximumPressure > limit {
			add("", fmt.Sprintf("segment %s limit exceeds adjacent station limit", segment.ID))
		}
	}
	return issues
}

// ErrLineVersionNotFound is returned when a draft or revision is missing.
var ErrLineVersionNotFound = errors.New("line version not found")
