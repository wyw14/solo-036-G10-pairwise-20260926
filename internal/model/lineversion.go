package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// LineVersionStatus 是线路配置版本的生命周期状态。
type LineVersionStatus string

const (
	// LineVersionDraft 草拟中：可反复修改、可校验，尚未生效。
	LineVersionDraft LineVersionStatus = "draft"
	// LineVersionPublished 已发布：内容不可变，并成为新作业采用的版本。
	LineVersionPublished LineVersionStatus = "published"
)

// Segment 描述两个站点之间的管段及其压力限制。
type Segment struct {
	ID              string  `json:"id"`
	From            string  `json:"from"`
	To              string  `json:"to"`
	Length          float64 `json:"length"`
	MaximumPressure float64 `json:"maximum_pressure"`
	Enabled         bool    `json:"enabled"`
}

// LineVersion 是一份可审核、可回看的线路配置快照。
type LineVersion struct {
	// Number 是单调递增的版本号，作业通过它引用当时采用的线路。
	Number int `json:"number"`
	// Status 为 draft 或 published；发布后内容冻结。
	Status LineVersionStatus `json:"status"`
	// Note 记录操作员录入的变更说明，便于审核。
	Note string `json:"note,omitempty"`
	// Stations/Segments 是该版本的完整线路拓扑，发布后不再变化。
	Stations []Station `json:"stations"`
	Segments []Segment `json:"segments"`
	// ContentChecksum 是版本内容（站点+区段）的规范化摘要，用于审核与防篡改。
	ContentChecksum string    `json:"content_checksum"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	PublishedAt     time.Time `json:"published_at,omitempty"`
}

// LineContent 是操作员录入的线路配置内容（不带版本元数据）。
type LineContent struct {
	Note     string    `json:"note,omitempty"`
	Stations []Station `json:"stations"`
	Segments []Segment `json:"segments"`
}

// sortedLineContent 用于生成与录入顺序无关的确定性摘要。
type sortedLineContent struct {
	Stations []Station `json:"stations"`
	Segments []Segment `json:"segments"`
}

// ComputeLineChecksum 对线路内容计算规范化 SHA-256 摘要。
func ComputeLineChecksum(stations []Station, segments []Segment) (string, error) {
	sortedStations := append([]Station(nil), stations...)
	sort.Slice(sortedStations, func(i, j int) bool { return sortedStations[i].ID < sortedStations[j].ID })
	sortedSegments := append([]Segment(nil), segments...)
	sort.Slice(sortedSegments, func(i, j int) bool { return sortedSegments[i].ID < sortedSegments[j].ID })
	payload, err := json.Marshal(sortedLineContent{Stations: sortedStations, Segments: sortedSegments})
	if err != nil {
		return "", fmt.Errorf("marshal line content: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// ValidateLineContent 校验一份完整线路拓扑是否可发布。
func ValidateLineContent(stations []Station, segments []Segment) error {
	index := make(map[string]Station, len(stations))
	launchers := 0
	receivers := 0
	for _, station := range stations {
		if err := station.Validate(); err != nil {
			return fmt.Errorf("station %s: %w", station.ID, err)
		}
		if _, exists := index[station.ID]; exists {
			return fmt.Errorf("duplicate station %s", station.ID)
		}
		index[station.ID] = station
		if station.Role == StationLauncher {
			launchers++
		}
		if station.Role == StationReceiver {
			receivers++
		}
	}
	if len(index) < 2 {
		return errors.New("line needs at least two stations")
	}
	if launchers == 0 || receivers == 0 {
		return errors.New("line needs a launcher and a receiver")
	}
	segmentIndex := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment.ID) == "" || strings.TrimSpace(segment.From) == "" || strings.TrimSpace(segment.To) == "" {
			return errors.New("segment identity is incomplete")
		}
		if _, exists := segmentIndex[segment.ID]; exists {
			return fmt.Errorf("duplicate segment %s", segment.ID)
		}
		segmentIndex[segment.ID] = struct{}{}
		from, fromOK := index[segment.From]
		to, toOK := index[segment.To]
		if !fromOK {
			return fmt.Errorf("segment %s has unknown source station", segment.ID)
		}
		if !toOK {
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
	if len(segments) == 0 {
		return errors.New("line needs at least one segment")
	}
	return nil
}

// Validate 校验版本记录自身；已发布版本必须持有合法且与内容一致的摘要。
func (v LineVersion) Validate() error {
	if v.Number <= 0 {
		return errors.New("line version number must be positive")
	}
	switch v.Status {
	case LineVersionDraft, LineVersionPublished:
	default:
		return fmt.Errorf("invalid line version status %q", v.Status)
	}
	if v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() {
		return errors.New("line version timestamps are required")
	}
	if v.Status == LineVersionPublished {
		if err := ValidateLineContent(v.Stations, v.Segments); err != nil {
			return fmt.Errorf("line version %d: %w", v.Number, err)
		}
		checksum, err := ComputeLineChecksum(v.Stations, v.Segments)
		if err != nil {
			return err
		}
		if v.ContentChecksum != checksum {
			return fmt.Errorf("line version %d content checksum mismatch", v.Number)
		}
		if v.PublishedAt.IsZero() {
			return fmt.Errorf("line version %d is missing published_at", v.Number)
		}
	}
	return nil
}
