package service

import (
	"fmt"
	"time"

	"github.com/wyw14/solo-036/internal/journal"
	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/run"
)

func (r *Runtime) CreateOperation(station string) (model.Run, error) {
	current, ok := r.LineVersions.Current()
	if !ok {
		return model.Run{}, fmt.Errorf("no published line version is available")
	}
	created := r.Runs.Create(station, current)
	event := model.Event{ID: created.ID + "/loaded", RunID: created.ID, Kind: "state", Epoch: created.Epoch, Payload: string(created.State), At: time.Now().UTC()}
	if err := r.appendEvent(event); err != nil {
		return model.Run{}, fmt.Errorf("journal: %w", err)
	}
	if err := r.appendEvent(model.Event{
		RunID:   created.ID,
		Kind:    "line_version",
		Epoch:   created.Epoch,
		Payload: fmt.Sprintf("v%d", created.LineVersion),
		At:      time.Now().UTC(),
	}); err != nil {
		return model.Run{}, fmt.Errorf("journal: %w", err)
	}
	return created, nil
}
func (r *Runtime) Operations() []model.Run { return run.Active(r.Runs) }
func (r *Runtime) JournalHealthy() bool    { return journal.Healthy(r.Journal) }
