package service

import (
	"path/filepath"
	"testing"
)

func TestLineVersionPersistenceRoundTrip(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "pigflow")

	original := NewRuntime()
	if err := original.ConfigurePersistence(dataDir); err != nil {
		t.Fatalf("configure persistence: %v", err)
	}
	oldRun, err := original.CreateOperation("station-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := original.Cancel(oldRun.ID); err != nil {
		t.Fatal(err)
	}
	draft, err := original.CreateLineDraft("tighten limit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := original.UpdateLineDraft(draft.Number, v2Content()); err != nil {
		t.Fatal(err)
	}
	if _, err := original.PublishLineVersion(draft.Number); err != nil {
		t.Fatalf("publish: %v", err)
	}
	newRun, err := original.CreateOperation("station-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := original.PersistSnapshot(); err != nil {
		t.Fatalf("persist snapshot: %v", err)
	}

	restored := NewRuntime()
	if err := restored.ConfigurePersistence(dataDir); err != nil {
		t.Fatalf("configure restored persistence: %v", err)
	}
	report, err := restored.RecoverPersistent()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !report.Ready || report.CurrentLineVersion != 2 || report.RecoveredLineVersions != 2 {
		t.Fatalf("unexpected recovery report: %+v", report)
	}
	oldVersion, err := restored.OperationLineVersion(oldRun.ID)
	if err != nil {
		t.Fatalf("historical run lookup: %v", err)
	}
	if oldVersion.Number != 1 {
		t.Fatalf("old run version = %d; want 1 after recovery", oldVersion.Number)
	}
	newVersion, err := restored.OperationLineVersion(newRun.ID)
	if err != nil {
		t.Fatalf("new run lookup: %v", err)
	}
	if newVersion.Number != 2 || newVersion.Segments[0].MaximumPressure != 11.5 {
		t.Fatalf("new run version mismatch after recovery: %+v", newVersion)
	}
	if control := restored.ensureControl(); control.LineVersion != 2 {
		t.Fatalf("restored control line version = %d; want 2", control.LineVersion)
	}
}
