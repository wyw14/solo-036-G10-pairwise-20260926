package lineversion

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/wyw14/solo-036/internal/model"
)

// 哨兵错误便于上层区分"未找到 / 不可变 / 内容非法"，映射为不同的 HTTP 状态。
var (
	ErrNotFound  = errors.New("line version not found")
	ErrImmutable = errors.New("line version is already published and immutable")
	ErrInvalid   = errors.New("line version content is invalid")
)

// Registry 保存全部线路版本（含草拟与已发布），并跟踪新作业应采用的当前版本。
// 已发布版本不可变：作业按版本号回看当时的线路配置。
type Registry struct {
	mu       sync.RWMutex
	versions map[int]model.LineVersion
	current  int
}

func NewRegistry() *Registry {
	return &Registry{versions: make(map[int]model.LineVersion)}
}

// SeedBaseline 以已发布状态写入第一版基线（仅在注册表为空时生效）。
func (r *Registry) SeedBaseline(content model.LineContent, now time.Time) (model.LineVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.versions) > 0 || r.current > 0 {
		return model.LineVersion{}, fmt.Errorf("line version registry is already initialized")
	}
	return r.publishLocked(1, content, now)
}

// Restore 用持久化的版本记录重建注册表，并返回当前版本号。
func (r *Registry) Restore(versions []model.LineVersion) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	restored := make(map[int]model.LineVersion, len(versions))
	current := 0
	for _, version := range versions {
		if err := version.Validate(); err != nil {
			return 0, err
		}
		if _, exists := restored[version.Number]; exists {
			return 0, fmt.Errorf("duplicate line version number %d", version.Number)
		}
		restored[version.Number] = version
		if version.Status == model.LineVersionPublished {
			if version.Number > current {
				current = version.Number
			}
		}
	}
	if len(restored) > 0 && current == 0 {
		return 0, fmt.Errorf("no published line version found")
	}
	r.versions = restored
	r.current = current
	return current, nil
}

// CreateDraft 基于上一版本内容（若有）创建一份新草拟，便于操作员增量录入。
func (r *Registry) CreateDraft(note string, now time.Time) (model.LineVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.nextNumberLocked()
	version := model.LineVersion{
		Number:    next,
		Status:    model.LineVersionDraft,
		Note:      note,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if r.current > 0 {
		base := r.versions[r.current]
		version.Stations = append([]model.Station(nil), base.Stations...)
		version.Segments = append([]model.Segment(nil), base.Segments...)
	}
	r.versions[next] = version
	return version, nil
}

// UpdateDraft 用操作员录入的完整站点/区段/压力限制覆盖草拟内容。
func (r *Registry) UpdateDraft(number int, content model.LineContent, now time.Time) (model.LineVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	version, ok := r.versions[number]
	if !ok {
		return model.LineVersion{}, fmt.Errorf("%w: %d", ErrNotFound, number)
	}
	if version.Status != model.LineVersionDraft {
		return model.LineVersion{}, fmt.Errorf("%w: %d", ErrImmutable, number)
	}
	version.Stations = append([]model.Station(nil), content.Stations...)
	version.Segments = append([]model.Segment(nil), content.Segments...)
	if note := content.Note; note != "" {
		version.Note = note
	}
	version.UpdatedAt = now
	r.versions[number] = version
	return version, nil
}

// ValidateDraft 校验草拟内容但不改变其状态，返回校验结果供操作员核对。
func (r *Registry) ValidateDraft(number int) (model.LineVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	version, ok := r.versions[number]
	if !ok {
		return model.LineVersion{}, fmt.Errorf("%w: %d", ErrNotFound, number)
	}
	if version.Status != model.LineVersionDraft {
		return model.LineVersion{}, fmt.Errorf("%w: %d", ErrImmutable, number)
	}
	if err := model.ValidateLineContent(version.Stations, version.Segments); err != nil {
		return model.LineVersion{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	checksum, err := model.ComputeLineChecksum(version.Stations, version.Segments)
	if err != nil {
		return model.LineVersion{}, err
	}
	version.ContentChecksum = checksum
	return version, nil
}

// Publish 冻结草拟：校验通过后写入摘要与发布时间，并将其置为当前版本。
func (r *Registry) Publish(number int, now time.Time) (model.LineVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	version, ok := r.versions[number]
	if !ok {
		return model.LineVersion{}, fmt.Errorf("%w: %d", ErrNotFound, number)
	}
	if version.Status != model.LineVersionDraft {
		return model.LineVersion{}, fmt.Errorf("%w: %d", ErrImmutable, number)
	}
	if err := model.ValidateLineContent(version.Stations, version.Segments); err != nil {
		return model.LineVersion{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	checksum, err := model.ComputeLineChecksum(version.Stations, version.Segments)
	if err != nil {
		return model.LineVersion{}, err
	}
	version.Status = model.LineVersionPublished
	version.ContentChecksum = checksum
	version.PublishedAt = now
	version.UpdatedAt = now
	r.versions[number] = version
	r.current = number
	return version, nil
}

func (r *Registry) publishLocked(number int, content model.LineContent, now time.Time) (model.LineVersion, error) {
	if err := model.ValidateLineContent(content.Stations, content.Segments); err != nil {
		return model.LineVersion{}, err
	}
	checksum, err := model.ComputeLineChecksum(content.Stations, content.Segments)
	if err != nil {
		return model.LineVersion{}, err
	}
	version := model.LineVersion{
		Number:          number,
		Status:          model.LineVersionPublished,
		Note:            content.Note,
		Stations:        append([]model.Station(nil), content.Stations...),
		Segments:        append([]model.Segment(nil), content.Segments...),
		ContentChecksum: checksum,
		CreatedAt:       now,
		UpdatedAt:       now,
		PublishedAt:     now,
	}
	r.versions[number] = version
	r.current = number
	return version, nil
}

// Get 返回指定版本（旧作业据此回看当时线路）。
func (r *Registry) Get(number int) (model.LineVersion, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	version, ok := r.versions[number]
	return version, ok
}

// Current 返回新作业应采用的已发布版本号。
func (r *Registry) Current() (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current, r.current > 0
}

// List 返回按版本号升序的全部版本（含草拟）。
func (r *Registry) List() []model.LineVersion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.LineVersion, 0, len(r.versions))
	for _, version := range r.versions {
		out = append(out, version)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// Draft 返回当前存在的唯一草拟版本（版本号最大的 draft），没有则返回 false。
func (r *Registry) Draft() (model.LineVersion, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	number := 0
	for _, version := range r.versions {
		if version.Status == model.LineVersionDraft && version.Number > number {
			number = version.Number
		}
	}
	if number == 0 {
		return model.LineVersion{}, false
	}
	return r.versions[number], true
}

func (r *Registry) nextNumberLocked() int {
	max := 0
	for number := range r.versions {
		if number > max {
			max = number
		}
	}
	return max + 1
}
