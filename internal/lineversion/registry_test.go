package lineversion

import (
	"errors"
	"testing"
	"time"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/route"
)

func testNow() time.Time { return time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC) }

func validContent() model.LineContent { return route.BuiltinLineContent() }

func TestSeedBaselineAndCurrent(t *testing.T) {
	registry := NewRegistry()
	baseline, err := registry.SeedBaseline(validContent(), testNow())
	if err != nil {
		t.Fatalf("seed baseline: %v", err)
	}
	if baseline.Number != 1 || baseline.Status != model.LineVersionPublished {
		t.Fatalf("unexpected baseline: %+v", baseline)
	}
	if current, ok := registry.Current(); !ok || current != 1 {
		t.Fatalf("current = %d, %v; want 1", current, ok)
	}
	if _, err := registry.SeedBaseline(validContent(), testNow()); err == nil {
		t.Fatal("expected second seed to fail")
	}
}

func TestDraftCopiesCurrentContent(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.SeedBaseline(validContent(), testNow()); err != nil {
		t.Fatal(err)
	}
	draft, err := registry.CreateDraft("add station d", testNow())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Number != 2 || draft.Status != model.LineVersionDraft {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	if len(draft.Stations) != 3 || len(draft.Segments) != 2 {
		t.Fatalf("draft should copy current content, got %d stations %d segments", len(draft.Stations), len(draft.Segments))
	}
}

func TestValidateDraftRejectsInvalidContent(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.SeedBaseline(validContent(), testNow()); err != nil {
		t.Fatal(err)
	}
	draft, _ := registry.CreateDraft("", testNow())
	invalid := validContent()
	invalid.Segments[0].MaximumPressure = 99 // 超过相邻站点压力限制
	if _, err := registry.UpdateDraft(draft.Number, invalid, testNow()); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ValidateDraft(draft.Number); !errors.Is(err, ErrInvalid) {
		t.Fatalf("validate = %v; want ErrInvalid", err)
	}
	if _, err := registry.Publish(draft.Number, testNow()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("publish = %v; want ErrInvalid", err)
	}
	// 发布失败后当前版本仍是 v1。
	if current, _ := registry.Current(); current != 1 {
		t.Fatalf("current = %d; want 1 after failed publish", current)
	}
}

func TestPublishFreezesVersionAndAdvancesCurrent(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.SeedBaseline(validContent(), testNow()); err != nil {
		t.Fatal(err)
	}
	draft, _ := registry.CreateDraft("extend line", testNow())
	content := validContent()
	content.Stations = append(content.Stations, model.Station{
		ID: "station-d", Name: "East Receiver", Role: model.StationReceiver,
		Kilometer: 260.0, PressureLimit: 10.5, Enabled: true,
	})
	content.Segments = append(content.Segments, model.Segment{
		ID: "segment-c-d", From: "station-c", To: "station-d", Length: 86.8, MaximumPressure: 10.5, Enabled: true,
	})
	if _, err := registry.UpdateDraft(draft.Number, content, testNow()); err != nil {
		t.Fatal(err)
	}
	validated, err := registry.ValidateDraft(draft.Number)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validated.ContentChecksum == "" {
		t.Fatal("validated draft should carry a content checksum")
	}
	published, err := registry.Publish(draft.Number, testNow())
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.ContentChecksum == "" || published.PublishedAt.IsZero() {
		t.Fatal("published version must carry checksum and published_at")
	}
	if current, _ := registry.Current(); current != 2 {
		t.Fatalf("current = %d; want 2", current)
	}
	// 已发布版本不可变：再次更新或发布都应被拒绝。
	if _, err := registry.UpdateDraft(2, content, testNow()); !errors.Is(err, ErrImmutable) {
		t.Fatalf("update published = %v; want ErrImmutable", err)
	}
	if _, err := registry.Publish(2, testNow()); !errors.Is(err, ErrImmutable) {
		t.Fatalf("republish = %v; want ErrImmutable", err)
	}
}

func TestOldVersionRemainsReadable(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.SeedBaseline(validContent(), testNow()); err != nil {
		t.Fatal(err)
	}
	draft, _ := registry.CreateDraft("", testNow())
	if _, err := registry.Publish(draft.Number, testNow()); err != nil {
		t.Fatal(err)
	}
	old, ok := registry.Get(1)
	if !ok {
		t.Fatal("baseline v1 must remain available for historical lookups")
	}
	if old.Status != model.LineVersionPublished || len(old.Stations) != 3 {
		t.Fatalf("unexpected historical version: %+v", old)
	}
	if _, ok := registry.Get(99); ok {
		t.Fatal("Get(99) should report not found")
	}
	listed := registry.List()
	if len(listed) != 2 || listed[0].Number != 1 || listed[1].Number != 2 {
		t.Fatalf("list = %+v; want v1, v2", listed)
	}
}

func TestRestoreRejectsCorruptAndRecomputesCurrent(t *testing.T) {
	registry := NewRegistry()
	baseline, err := registry.SeedBaseline(validContent(), testNow())
	if err != nil {
		t.Fatal(err)
	}
	tampered := baseline
	tampered.ContentChecksum = "deadbeef"
	if _, err := registry.Restore([]model.LineVersion{tampered}); err == nil {
		t.Fatal("restore must reject checksum mismatch")
	}
	fresh := NewRegistry()
	current, err := fresh.Restore([]model.LineVersion{baseline})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if current != 1 {
		t.Fatalf("restored current = %d; want 1", current)
	}
}
