package service

import (
	"fmt"
	"sort"
	"time"

	"github.com/wyw14/solo-036/internal/journal"
	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/run"
)

func (r *Runtime) CreateOperation(station string) (model.Run, error) {
	active, err := r.Versions.MustActiveVersion()
	if err != nil {
		return model.Run{}, err
	}
	created := r.Runs.Create(station, active.ID)
	event := model.Event{ID: created.ID + "/loaded", RunID: created.ID, Kind: "state", Epoch: created.Epoch, Payload: string(created.State), At: time.Now().UTC()}
	if err := r.appendEvent(event); err != nil {
		return model.Run{}, fmt.Errorf("journal: %w", err)
	}
	if err := r.appendEvent(model.Event{
		RunID:   created.ID,
		Kind:    "line_version_bound",
		Epoch:   created.Epoch,
		Payload: active.ID,
		At:      created.CreatedAt,
	}); err != nil {
		return model.Run{}, fmt.Errorf("journal line version: %w", err)
	}
	return created, nil
}
func (r *Runtime) Operations() []model.Run { return run.Active(r.Runs) }

// OperationsHistory returns every run, including completed and cancelled
// ones, so operators can locate historical operations and review the line
// versions they ran against.
func (r *Runtime) OperationsHistory() []model.Run {
	records := r.Runs.Snapshot()
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records
}
func (r *Runtime) JournalHealthy() bool { return journal.Healthy(r.Journal) }
