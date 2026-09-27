package service

import (
	"strings"
	"testing"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/route"
)

// 调整 a-b 区段压力限制（仍低于相邻站点限制），用于生成合法的 v2 内容。
func v2Content() model.LineContent {
	content := route.BuiltinLineContent()
	content.Note = "tighten segment-a-b pressure"
	content.Segments[0].MaximumPressure = 11.5
	return content
}

func TestRunBindsCurrentLineVersion(t *testing.T) {
	runtime := NewRuntime()
	current, err := runtime.CurrentLineVersion()
	if err != nil {
		t.Fatal(err)
	}
	if current.Number != 1 {
		t.Fatalf("baseline version = %d; want 1", current.Number)
	}
	created, err := runtime.CreateOperation("station-a")
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if created.LineVersion != 1 {
		t.Fatalf("run line version = %d; want 1", created.LineVersion)
	}
	bound, err := runtime.OperationLineVersion(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Number != 1 || bound.ContentChecksum == "" {
		t.Fatalf("operation bound to unexpected version: %+v", bound)
	}
}

func TestPublishBlockedWhileOperationActive(t *testing.T) {
	runtime := NewRuntime()
	created, err := runtime.CreateOperation("station-a")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := runtime.CreateLineDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.UpdateLineDraft(draft.Number, v2Content()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.PublishLineVersion(draft.Number); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("publish during active run = %v; want active-operations error", err)
	}
	if err := runtime.Cancel(created.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
}

func TestPublishSwitchesNetworkAndHistoricalRunsKeepOldVersion(t *testing.T) {
	runtime := NewRuntime()
	oldRun, err := runtime.CreateOperation("station-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Cancel(oldRun.ID); err != nil {
		t.Fatal(err)
	}

	draft, err := runtime.CreateLineDraft("tighten limit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.UpdateLineDraft(draft.Number, v2Content()); err != nil {
		t.Fatal(err)
	}
	validated, err := runtime.ValidateLineDraft(draft.Number)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validated.ContentChecksum == "" {
		t.Fatal("validated draft should expose content checksum")
	}
	published, err := runtime.PublishLineVersion(draft.Number)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.Number != 2 || published.Status != model.LineVersionPublished {
		t.Fatalf("unexpected published version: %+v", published)
	}

	// 发布后实时拓扑切换为新版压力限制。
	control := runtime.ensureControl()
	if control.LineVersion != 2 {
		t.Fatalf("control line version = %d; want 2", control.LineVersion)
	}
	segmentAB := control.Network.Segments()[0]
	if segmentAB.MaximumPressure != 11.5 {
		t.Fatalf("segment pressure = %.2f; want 11.5", segmentAB.MaximumPressure)
	}

	// 新作业绑定 v2，旧作业仍可回看 v1。
	newRun, err := runtime.CreateOperation("station-a")
	if err != nil {
		t.Fatal(err)
	}
	if newRun.LineVersion != 2 {
		t.Fatalf("new run line version = %d; want 2", newRun.LineVersion)
	}
	oldVersion, err := runtime.OperationLineVersion(oldRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldVersion.Number != 1 || oldVersion.Segments[0].MaximumPressure != 11.8 {
		t.Fatalf("historical run should still see v1, got %+v", oldVersion)
	}
	newVersion, err := runtime.OperationLineVersion(newRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newVersion.Number != 2 || newVersion.Segments[0].MaximumPressure != 11.5 {
		t.Fatalf("new run should see v2, got %+v", newVersion)
	}

	// 已发布版本不可变；快照包含两个版本。
	if _, err := runtime.UpdateLineDraft(2, v2Content()); err == nil {
		t.Fatal("updating a published version must fail")
	}
	versions := runtime.ListLineVersions()
	if len(versions) != 2 || versions[0].Number != 1 || versions[1].Number != 2 {
		t.Fatalf("version history = %+v; want v1 and v2", versions)
	}
}
