package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillStateImportsLegacyJSONOnce(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(SkillState{
		CurrentPhase: "review",
		NextPrompt:   "continue",
		Vars:         map[string]string{"done": "1"},
		History:      []StateHistoryEntry{{Summary: "first", Success: true}},
		UpdatedAt:    "2026-05-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(stateDir, "state.json")
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentPhase != "review" || loaded.GetVar("done") != "1" || len(loaded.History) != 1 {
		t.Fatalf("imported = %+v", loaded)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("legacy state.json should be removed, stat err=%v", err)
	}
	if err := os.WriteFile(jsonPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := LoadState(dir)
	if err != nil || again.CurrentPhase != "review" {
		t.Fatalf("later corrupt file changed state: %+v %v", again, err)
	}
}

func TestSkillStateSaveLeavesUnreadLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	stateDirPath := filepath.Join(dir, ".state")
	if err := os.MkdirAll(stateDirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(SkillState{
		CurrentPhase: "review",
		Vars:         map[string]string{"done": "1"},
		UpdatedAt:    "2026-05-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(stateDirPath, "state.json")
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(dir, &SkillState{CurrentPhase: "replaced"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("save removed unread legacy json: %v", err)
	}
	restored, err := LoadState(dir)
	if err != nil || restored.CurrentPhase != "review" || restored.GetVar("done") != "1" {
		t.Fatalf("import after unread save = %+v %v", restored, err)
	}
}
