package lineconfig

import (
	"errors"
	"testing"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/route"
)

func validConfig() model.LineConfig {
	return model.LineConfig{
		Stations: []model.Station{
			{ID: "a", Name: "Launcher", Role: model.StationLauncher, Kilometer: 0, PressureLimit: 12, Enabled: true},
			{ID: "b", Name: "Mid", Role: model.StationIntermediate, Kilometer: 50, PressureLimit: 12, Enabled: true},
			{ID: "c", Name: "Receiver", Role: model.StationReceiver, Kilometer: 100, PressureLimit: 12, Enabled: true},
		},
		Segments: []model.LineSegment{
			{ID: "ab", From: "a", To: "b", Length: 50, MaximumPressure: 12, Enabled: true},
			{ID: "bc", From: "b", To: "c", Length: 50, MaximumPressure: 12, Enabled: true},
		},
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	if _, err := route.NetworkFromConfig(validConfig()); err != nil {
		t.Fatalf("seed config must build a network: %v", err)
	}
	return NewStore(validConfig())
}

func TestSeedsActiveV1(t *testing.T) {
	store := newTestStore(t)
	active, ok := store.ActiveVersion()
	if !ok {
		t.Fatal("expected an active version after seeding")
	}
	if active.Number != 1 || active.Status != model.LineVersionPublished || !active.Active {
		t.Fatalf("seed = number %d status %s active %v, want published active v1", active.Number, active.Status, active.Active)
	}
	if _, _, err := store.ActiveNetwork(); err != nil {
		t.Fatalf("active network: %v", err)
	}
}

func TestDraftLifecycle(t *testing.T) {
	store := newTestStore(t)
	draft, err := store.CreateDraft(DraftRequest{Name: "d", Operator: "op", Config: validConfig()}, "line-v1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if draft.Status != model.LineVersionDraft {
		t.Fatalf("new version status = %s, want draft", draft.Status)
	}
	report, err := store.Validate(draft.ID)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !report.Valid {
		t.Fatalf("valid draft reported issues: %+v", report.Issues)
	}
	published, err := store.Publish(draft.ID, PublishRequest{Operator: "op"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.Number != 2 || !published.Active || published.Status != model.LineVersionPublished {
		t.Fatalf("published = %+v, want number 2 active published", published)
	}
	// v1 must be deactivated.
	v1, _ := store.Get("line-v1")
	if v1.Active {
		t.Fatal("previous active version still marked active after publish")
	}
	// Published revisions are immutable.
	if _, err := store.UpdateDraft(draft.ID, DraftRequest{Config: validConfig()}); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("update published = %v, want ErrNotDraft", err)
	}
	if _, err := store.Publish(draft.ID, PublishRequest{}); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("republish = %v, want ErrNotDraft", err)
	}
}

func TestPublishAssignsMonotonicNumbers(t *testing.T) {
	store := newTestStore(t)
	for want := 2; want <= 4; want++ {
		draft, err := store.CreateDraft(DraftRequest{Operator: "op", Config: validConfig()}, "")
		if err != nil {
			t.Fatalf("create draft: %v", err)
		}
		published, err := store.Publish(draft.ID, PublishRequest{Operator: "op"})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		if published.Number != want {
			t.Fatalf("revision number = %d, want %d", published.Number, want)
		}
	}
}

func TestDraftDelete(t *testing.T) {
	store := newTestStore(t)
	draft, _ := store.CreateDraft(DraftRequest{Operator: "op", Config: validConfig()}, "")
	if err := store.DeleteDraft(draft.ID); err != nil {
		t.Fatalf("delete draft: %v", err)
	}
	if _, ok := store.Get(draft.ID); ok {
		t.Fatal("draft still present after delete")
	}
	if err := store.DeleteDraft("line-v1"); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("delete published = %v, want ErrNotDraft", err)
	}
}

func TestValidateRejectsBrokenConfig(t *testing.T) {
	store := newTestStore(t)
	broken := validConfig()
	broken.Segments[0].MaximumPressure = 99 // exceeds adjacent station limits
	draft, _ := store.CreateDraft(DraftRequest{Operator: "op", Config: broken}, "")
	report, err := store.Validate(draft.ID)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if report.Valid || len(report.Issues) == 0 {
		t.Fatalf("expected issues for over-pressure segment, got %+v", report)
	}
	if _, err := store.Publish(draft.ID, PublishRequest{Operator: "op"}); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("publish invalid = %v, want ErrInvalidDraft", err)
	}
}

func TestValidateRejectsUnreachableReceiver(t *testing.T) {
	store := newTestStore(t)
	// Disable the segment leading into the receiver: no operable path.
	cut := validConfig()
	cut.Segments[1].Enabled = false
	draft, _ := store.CreateDraft(DraftRequest{Operator: "op", Config: cut}, "")
	report, err := store.Validate(draft.ID)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if report.Valid {
		t.Fatal("expected reachability failure when receiver is cut off")
	}
}

func TestUnknownVersionErrors(t *testing.T) {
	store := newTestStore(t)
	missing := "does-not-exist"
	if _, err := store.CreateDraft(DraftRequest{}, missing); !errors.Is(err, model.ErrLineVersionNotFound) {
		t.Fatalf("create from missing base = %v, want ErrLineVersionNotFound", err)
	}
	if _, err := store.Validate(missing); !errors.Is(err, model.ErrLineVersionNotFound) {
		t.Fatalf("validate missing = %v, want ErrLineVersionNotFound", err)
	}
	if _, err := store.Publish(missing, PublishRequest{}); !errors.Is(err, model.ErrLineVersionNotFound) {
		t.Fatalf("publish missing = %v, want ErrLineVersionNotFound", err)
	}
}

func TestNetworkForHistoricalVersion(t *testing.T) {
	store := newTestStore(t)
	draft, _ := store.CreateDraft(DraftRequest{Operator: "op", Config: validConfig()}, "line-v1")
	published, _ := store.Publish(draft.ID, PublishRequest{Operator: "op"})

	oldNetwork, oldVersion, err := store.NetworkFor("line-v1")
	if err != nil {
		t.Fatalf("network for historical v1: %v", err)
	}
	if oldVersion.ID != "line-v1" {
		t.Fatalf("historical version = %s, want line-v1", oldVersion.ID)
	}
	if stations := len(oldNetwork.Stations()); stations != 3 {
		t.Fatalf("historical network stations = %d, want 3", stations)
	}
	newNetwork, active, err := store.ActiveNetwork()
	if err != nil {
		t.Fatalf("active network: %v", err)
	}
	if active.ID != published.ID {
		t.Fatalf("active version = %s, want %s", active.ID, published.ID)
	}
	if len(newNetwork.Segments()) != 2 {
		t.Fatal("active network missing segments")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	store := newTestStore(t)
	draft, _ := store.CreateDraft(DraftRequest{Operator: "op", Config: validConfig()}, "")
	published, _ := store.Publish(draft.ID, PublishRequest{Operator: "op"})
	abandoned, _ := store.CreateDraft(DraftRequest{Operator: "op", Config: validConfig()}, "")

	versions, activeID := store.Snapshot()
	restored := Restore(versions, activeID)
	active, ok := restored.ActiveVersion()
	if !ok || active.ID != published.ID {
		t.Fatalf("restored active = %s ok=%v, want %s", active.ID, ok, published.ID)
	}
	if _, ok := restored.Get(abandoned.ID); !ok {
		t.Fatal("restored store lost the unpublished draft")
	}
	if got := len(restored.List()); got != len(versions) {
		t.Fatalf("restored list len = %d, want %d", got, len(versions))
	}
	if v, _ := restored.GetPublished(1); v.ID != "line-v1" {
		t.Fatalf("GetPublished(1) = %s, want line-v1", v.ID)
	}
}
