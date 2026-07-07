package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestEnsureSkillIDBeforeUpload_AlreadyHasValidID(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		SkillID: "lovstudio.any2pdf",
		Name:    "Any2PDF",
	}
	id, err := EnsureSkillIDBeforeUpload(entry, "user@test.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "lovstudio.any2pdf" {
		t.Errorf("got %q, want lovstudio.any2pdf", id)
	}
}

func TestEnsureSkillIDBeforeUpload_InvalidExistingID(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		SkillID: "INVALID",
		Name:    "Test",
	}
	_, err := EnsureSkillIDBeforeUpload(entry, "user@test.com")
	if err == nil {
		t.Fatal("expected error for invalid existing id")
	}
}

func TestEnsureSkillIDBeforeUpload_AutoGenerates(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	os.WriteFile(yamlPath, []byte("name: My Tool\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n"), 0644)

	entry := &corelib.NLSkillEntry{
		Name:     "My Tool",
		SkillDir: dir,
	}
	id, err := EnsureSkillIDBeforeUpload(entry, "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}
	if !IsValidSkillID(id) {
		t.Errorf("generated id %q is not valid", id)
	}
	// Should start with "alice-" prefix
	if !strings.HasPrefix(id, "alice-") {
		t.Errorf("id %q should start with alice-", id)
	}
	// Entry should be updated
	if entry.SkillID != id {
		t.Errorf("entry.SkillID = %q, want %q", entry.SkillID, id)
	}
	if entry.Publisher == "" {
		t.Error("entry.Publisher should be set")
	}

	// Should be persisted to disk
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read skill.yaml: %v", err)
	}
	if !strings.Contains(string(data), "id: "+id) {
		t.Errorf("skill.yaml should contain id: %s, got:\n%s", id, string(data))
	}
}

func TestEnsureSkillIDBeforeUpload_NoEmail(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "Test"}
	_, err := EnsureSkillIDBeforeUpload(entry, "")
	if err == nil {
		t.Fatal("expected error for empty email")
	}
}

func TestEnsureSkillIDBeforeUpload_NilEntry(t *testing.T) {
	_, err := EnsureSkillIDBeforeUpload(nil, "user@test.com")
	if err == nil {
		t.Fatal("expected error for nil entry")
	}
}

func TestEnsureSkillIDBeforeUpload_PreservesYAMLDocumentMarker(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	content := "---\nname: My Tool\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n"
	os.WriteFile(yamlPath, []byte(content), 0644)

	entry := &corelib.NLSkillEntry{Name: "My Tool", SkillDir: dir}
	id, err := EnsureSkillIDBeforeUpload(entry, "bob@test.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(yamlPath)
	lines := strings.Split(string(data), "\n")
	if lines[0] != "---" {
		t.Errorf("first line should still be ---, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "id: ") {
		t.Errorf("second line should be id:, got %q", lines[1])
	}
	_ = id
}
