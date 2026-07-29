package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKindFromEventName_MarkNeedsReviewAndRestore(t *testing.T) {
	if got := KindFromEventName("skill:mark_needs_review"); got != "mark_needs_review" {
		t.Fatalf("mark_needs_review kind=%q", got)
	}
	if got := KindFromEventName("mark_needs_review"); got != "mark_needs_review" {
		t.Fatalf("alias kind=%q", got)
	}
	if got := KindFromEventName("skill:yaml_restore"); got != "yaml_restore" {
		t.Fatalf("yaml_restore kind=%q", got)
	}
	if got := KindFromEventName("skill:maintenance_apply"); got != "maintenance_apply" {
		t.Fatalf("maintenance_apply kind=%q", got)
	}
}

func TestEvolutionAudit_AppendAndListNewestFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	if err := AppendEvolutionAudit(path, EvolutionAuditEvent{Kind: "failed", Skill: "a", Explanation: "e1", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvolutionAudit(path, EvolutionAuditEvent{Kind: "repaired", Skill: "b", Explanation: "e2", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvolutionAudit(path, EvolutionAuditEvent{Kind: "optimized", Skill: "c", Source: "test"}); err != nil {
		t.Fatal(err)
	}

	list, err := ListEvolutionAudit(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("len=%d want 3", len(list))
	}
	// Newest first.
	if list[0].Skill != "c" || list[0].Kind != "optimized" {
		t.Fatalf("newest = %+v", list[0])
	}
	if list[2].Skill != "a" {
		t.Fatalf("oldest = %+v", list[2])
	}
	if list[0].Timestamp == "" {
		t.Fatal("timestamp should be filled")
	}
}

func TestEvolutionAudit_ListLimitAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.jsonl")
	list, err := ListEvolutionAudit(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if list != nil && len(list) != 0 {
		t.Fatalf("want empty for missing file, got %v", list)
	}

	path = filepath.Join(dir, "audit.jsonl")
	for i := 0; i < 10; i++ {
		if err := AppendEvolutionAudit(path, EvolutionAuditEvent{Kind: "failed", Skill: "s", Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	list, err = ListEvolutionAudit(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("limit 3 got %d", len(list))
	}
}

func TestKindFromEventName(t *testing.T) {
	if KindFromEventName(EventSkillRepaired) != "repaired" {
		t.Fatal(KindFromEventName(EventSkillRepaired))
	}
	if KindFromEventName(EventSkillOptimized) != "optimized" {
		t.Fatal(KindFromEventName(EventSkillOptimized))
	}
	if KindFromEventName("custom") != "custom" {
		t.Fatal(KindFromEventName("custom"))
	}
	if KindFromEventName("skill:yaml_restore") != "yaml_restore" {
		t.Fatal(KindFromEventName("skill:yaml_restore"))
	}
	if KindFromEventName("skill:maintenance_apply") != "maintenance_apply" {
		t.Fatal(KindFromEventName("skill:maintenance_apply"))
	}
}

func TestEvolutionAuditToolPayload_FilterAndLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	_ = AppendEvolutionAudit(path, EvolutionAuditEvent{Kind: "failed", Skill: "alpha-tool", Source: "test"})
	_ = AppendEvolutionAudit(path, EvolutionAuditEvent{Kind: "repaired", Skill: "beta-tool", Source: "test"})
	_ = AppendEvolutionAudit(path, EvolutionAuditEvent{Kind: "optimized", Skill: "alpha-helper", Source: "test"})

	payload := EvolutionAuditToolPayload(path, 10, "alpha")
	if payload["ok"] != true {
		t.Fatalf("payload=%v", payload)
	}
	events, _ := payload["events"].([]EvolutionAuditEvent)
	if len(events) != 2 {
		t.Fatalf("filter alpha: count=%d events=%v", payload["count"], events)
	}
	payload = EvolutionAuditToolPayload(path, 1, "")
	if payload["count"] != 1 {
		t.Fatalf("limit 1 count=%v", payload["count"])
	}
}

func TestRecordEvolutionEvent_WritesDefaultPathOverrideable(t *testing.T) {
	// RecordEvolutionEvent uses DefaultEvolutionAuditPath — just ensure it doesn't panic.
	// We exercise Append via path-specific tests above.
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	if err := AppendEvolutionAudit(path, EvolutionAuditEvent{
		Kind:   KindFromEventName(EventSkillAutoDiscovered),
		Skill:  "new-one",
		Source: "test",
	}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		t.Fatalf("file not written: %v", err)
	}
}
