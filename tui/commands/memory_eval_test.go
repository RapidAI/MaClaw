package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestMemoryEvalLoadsCasesArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cases.json")
	want := []memory.RecallEvalCase{{Name: "case1", Query: "react migration", ExpectedContains: []string{"react"}}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(path, data); err != nil {
		t.Fatal(err)
	}
	got, err := loadRecallEvalCases(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "case1" {
		t.Fatalf("unexpected cases: %+v", got)
	}
}

func TestMemoryEvalLoadsCasesWrapper(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cases.json")
	data, err := json.Marshal(map[string]interface{}{
		"cases": []memory.RecallEvalCase{{Name: "wrapped", Query: "postgresql backup"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(path, data); err != nil {
		t.Fatal(err)
	}
	got, err := loadRecallEvalCases(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "wrapped" {
		t.Fatalf("unexpected cases: %+v", got)
	}
}

func TestMemoryEvalCommandRuns(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStoreWithMode(filepath.Join(dir, "memory"), memory.StoreModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.Entry{
		Content:   "React migration decision kept component compatibility",
		Category:  memory.CategoryProjectKnowledge,
		Tags:      []string{"react", "migration"},
		Embedding: []float32{1, 0},
		Status:    memory.StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.Entry{
		Content:   "Vue migration decision reduced bundle complexity",
		Category:  memory.CategoryProjectKnowledge,
		Tags:      []string{"vue", "migration"},
		Embedding: []float32{0.98, 0.02},
		Status:    memory.StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	store.Stop()

	casesPath := filepath.Join(dir, "cases.json")
	cases := []memory.RecallEvalCase{{
		Name:             "migration",
		Query:            "why compare react and vue migration decisions",
		ExpectedContains: []string{"react migration", "vue migration"},
	}}
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(casesPath, data); err != nil {
		t.Fatal(err)
	}

	if err := memoryEval(dir, []string{"--cases", casesPath, "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := memoryEval(dir, []string{"--cases", casesPath, "--maintenance", "--json"}); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
