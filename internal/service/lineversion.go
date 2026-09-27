package service

import (
	"fmt"
	"time"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/run"
)

// LineVersions 列出全部线路版本（含草拟），按版本号升序。
func (r *Runtime) ListLineVersions() []model.LineVersion {
	return r.LineVersions.List()
}

// CurrentLineVersion 返回新作业当前采用的已发布版本。
func (r *Runtime) CurrentLineVersion() (model.LineVersion, error) {
	number, ok := r.LineVersions.Current()
	if !ok {
		return model.LineVersion{}, fmt.Errorf("no published line version is available")
	}
	version, _ := r.LineVersions.Get(number)
	return version, nil
}

// GetLineVersion 按版本号回看线路配置；旧作业通过其记录的版本号查询。
func (r *Runtime) GetLineVersion(number int) (model.LineVersion, error) {
	version, ok := r.LineVersions.Get(number)
	if !ok {
		return model.LineVersion{}, fmt.Errorf("line version %d not found", number)
	}
	return version, nil
}

// OperationLineVersion 返回指定作业当时采用的线路版本。
func (r *Runtime) OperationLineVersion(runID string) (model.LineVersion, error) {
	record, ok := r.Runs.Get(runID)
	if !ok {
		return model.LineVersion{}, fmt.Errorf("run %s not found", runID)
	}
	return r.GetLineVersion(record.LineVersion)
}

// CreateLineDraft 新建草拟；默认复制当前已发布版本内容，便于增量修改。
func (r *Runtime) CreateLineDraft(note string) (model.LineVersion, error) {
	return r.LineVersions.CreateDraft(note, time.Now().UTC())
}

// UpdateLineDraft 用操作员录入的站点、区段与压力限制整体覆盖草拟。
func (r *Runtime) UpdateLineDraft(number int, content model.LineContent) (model.LineVersion, error) {
	return r.LineVersions.UpdateDraft(number, content, time.Now().UTC())
}

// ValidateLineDraft 校验草拟但不发布，返回带内容摘要的版本供操作员核对。
func (r *Runtime) ValidateLineDraft(number int) (model.LineVersion, error) {
	return r.LineVersions.ValidateDraft(number)
}

// PublishLineVersion 校验并冻结草拟。存在在途作业时拒绝切换，
// 避免在线作业与新版拓扑混杂；发布后新作业一律采用新版本。
func (r *Runtime) PublishLineVersion(number int) (model.LineVersion, error) {
	if active := run.Active(r.Runs); len(active) > 0 {
		return model.LineVersion{}, fmt.Errorf("cannot publish line version while %d operation(s) are still active", len(active))
	}
	version, err := r.LineVersions.Publish(number, time.Now().UTC())
	if err != nil {
		return model.LineVersion{}, err
	}
	r.switchLineVersion(version)
	if err := r.appendEvent(model.Event{
		Kind:    "line_version_published",
		Epoch:   int64(version.Number),
		Payload: fmt.Sprintf("v%d", version.Number),
		At:      version.PublishedAt,
	}); err != nil {
		return model.LineVersion{}, fmt.Errorf("journal: %w", err)
	}
	return version, nil
}

// switchLineVersion 用已发布版本重建实时拓扑；保留持久化句柄与历史事故记录。
func (r *Runtime) switchLineVersion(version model.LineVersion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rebuilt := r.buildControl(version)
	rebuilt.Persistent = r.control.Persistent
	rebuilt.Incidents = append([]model.Incident(nil), r.control.Incidents...)
	r.control = rebuilt
}
