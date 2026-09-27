package service

import (
	"strings"
	"testing"

	"github.com/wyw14/solo-036/internal/lineconfig"
	"github.com/wyw14/solo-036/internal/model"
)

func raiseLimitsConfig() model.LineConfig {
	return model.LineConfig{
		Stations: []model.Station{
			{ID: "station-a", Name: "North Launcher", Role: model.StationLauncher, Kilometer: 0, PressureLimit: 12.5, Enabled: true},
			{ID: "station-b", Name: "Central Block Valve", Role: model.StationIntermediate, Kilometer: 86.4, PressureLimit: 12.2, Enabled: true},
			{ID: "station-c", Name: "South Receiver", Role: model.StationReceiver, Kilometer: 173.2, PressureLimit: 12.2, Enabled: true},
		},
		Segments: []model.LineSegment{
			{ID: "segment-a-b", From: "station-a", To: "station-b", Length: 86.4, MaximumPressure: 12.2, Enabled: true},
			{ID: "segment-b-c", From: "station-b", To: "station-c", Length: 86.8, MaximumPressure: 12.2, Enabled: true},
		},
	}
}

func publishDraft(t *testing.T, r *Runtime, config model.LineConfig, operator string) model.LineVersion {
	t.Helper()
	draft, err := r.CreateDraft(DraftInput{Name: "next", Operator: operator, Config: config}, "")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	report, err := r.ValidateDraft(draft.ID)
	if err != nil {
		t.Fatalf("validate draft: %v", err)
	}
	if !report.Valid {
		t.Fatalf("draft invalid: %+v", report.Issues)
	}
	published, err := r.PublishLineVersion(draft.ID, operator, "")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return published
}

func TestRunBindsActiveVersionAtCreation(t *testing.T) {
	r := NewRuntime()
	run, err := r.CreateOperation("station-a")
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if run.LineVersionID != "line-v1" {
		t.Fatalf("new run bound to %s, want line-v1", run.LineVersionID)
	}
	view, err := r.OperationLineVersion(run.ID)
	if err != nil {
		t.Fatalf("operation line version: %v", err)
	}
	if view.Version.Number != 1 || view.BoundRun != run.ID {
		t.Fatalf("lookback = %+v, want v1 bound to run", view)
	}
}

func TestPublishBlockedWhileOperationInFlight(t *testing.T) {
	r := NewRuntime()
	run, err := r.CreateOperation("station-a")
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	draft, err := r.CreateDraft(DraftInput{Operator: "op", Config: raiseLimitsConfig()}, "")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if _, err := r.PublishLineVersion(draft.ID, "op", ""); err == nil {
		t.Fatal("publish succeeded with a non-terminal run present")
	} else if !strings.Contains(err.Error(), lineconfig.ErrInFlight.Error()) {
		t.Fatalf("publish error = %v, want in-flight rejection", err)
	}
	if err := r.Cancel(run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := r.PublishLineVersion(draft.ID, "op", ""); err != nil {
		t.Fatalf("publish after quiesce: %v", err)
	}
}

func TestNewRunUsesNewVersionOldRunKeepsHistory(t *testing.T) {
	r := NewRuntime()
	oldRun, err := r.CreateOperation("station-a")
	if err != nil {
		t.Fatalf("create old run: %v", err)
	}
	if err := r.Cancel(oldRun.ID); err != nil {
		t.Fatalf("cancel old run: %v", err)
	}
	published := publishDraft(t, r, raiseLimitsConfig(), "op")

	newRun, err := r.CreateOperation("station-a")
	if err != nil {
		t.Fatalf("create new run: %v", err)
	}
	if newRun.LineVersionID != published.ID {
		t.Fatalf("new run bound to %s, want %s", newRun.LineVersionID, published.ID)
	}
	oldView, err := r.OperationLineVersion(oldRun.ID)
	if err != nil {
		t.Fatalf("historical lookback: %v", err)
	}
	if oldView.Version.ID != "line-v1" {
		t.Fatalf("old run resolves %s, want line-v1", oldView.Version.ID)
	}
	newView, err := r.OperationLineVersion(newRun.ID)
	if err != nil {
		t.Fatalf("new run lookback: %v", err)
	}
	if newView.Version.ID != published.ID {
		t.Fatalf("new run resolves %s, want %s", newView.Version.ID, published.ID)
	}
	// The active control network must reflect the published revision.
	control := r.ensureControl()
	if control.LineVersionID != published.ID {
		t.Fatalf("control network on version %s, want %s", control.LineVersionID, published.ID)
	}
}

func TestPublishAuditsToJournal(t *testing.T) {
	r := NewRuntime()
	published := publishDraft(t, r, raiseLimitsConfig(), "auditor")
	found := false
	for _, event := range r.Journal.Events() {
		if event.Kind == "line_version_published" && event.Payload == published.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("journal has no line_version_published audit event")
	}
}

func TestSnapshotCarriesVersionHistory(t *testing.T) {
	r := NewRuntime()
	op, err := r.CreateOperation("station-a")
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if err := r.Cancel(op.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	published := publishDraft(t, r, raiseLimitsConfig(), "op")
	snapshot := r.Snapshot()
	if snapshot.ActiveLineVersionID != published.ID {
		t.Fatalf("snapshot active = %s, want %s", snapshot.ActiveLineVersionID, published.ID)
	}
	if len(snapshot.LineVersions) < 2 {
		t.Fatalf("snapshot has %d line versions, want >= 2", len(snapshot.LineVersions))
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("snapshot validation: %v", err)
	}
}
