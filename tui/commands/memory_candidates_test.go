package commands

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestMemoryCandidatesCommandListsQuarantinedCandidates(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStoreWithMode(filepath.Join(dir, "memory"), memory.StoreModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.Entry{
		Content:  "I just ran the current task and it is probably okay",
		Category: memory.CategoryProjectKnowledge,
		Tags:     []string{"memory_candidate"},
		Status:   memory.StatusDormant,
	}); err != nil {
		t.Fatal(err)
	}
	store.Stop()

	out, err := captureMemoryStdout(t, func() error {
		return memoryCandidates(dir, []string{"--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Candidates []memory.MemoryCandidateSnapshot `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal candidates output: %v\n%s", err, out)
	}
	if len(payload.Candidates) != 1 {
		t.Fatalf("expected one candidate, got %+v", payload.Candidates)
	}
	if payload.Candidates[0].Entry.Status != memory.StatusDormant || payload.Candidates[0].Decision.Action != memory.MemoryGovernanceQuarantine {
		t.Fatalf("unexpected candidate payload: %+v", payload.Candidates[0])
	}
}

func TestMemoryCandidatesCommandApplyPromotesCandidate(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStoreWithMode(filepath.Join(dir, "memory"), memory.StoreModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.Entry{
		Content:    "Project API endpoint is https://api.example.com and build command is pnpm test",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"memory_candidate", "api"},
		Status:     memory.StatusDormant,
		SourceType: "memory_candidate",
	}); err != nil {
		t.Fatal(err)
	}
	store.Stop()

	out, err := captureMemoryStdout(t, func() error {
		return memoryCandidates(dir, []string{"--apply", "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Consolidation memory.CandidateConsolidationResult `json:"consolidation"`
		Candidates    []memory.MemoryCandidateSnapshot    `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal apply output: %v\n%s", err, out)
	}
	if payload.Consolidation.Promoted != 1 || len(payload.Candidates) != 0 {
		t.Fatalf("expected promoted candidate and empty quarantine list, got %+v", payload)
	}

	store, err = memory.NewStoreWithMode(filepath.Join(dir, "memory"), memory.StoreModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	entries := store.List(memory.CategoryProjectKnowledge, "api.example.com")
	if len(entries) != 1 || entries[0].Status != memory.StatusActive {
		t.Fatalf("expected promoted active entry, got %+v", entries)
	}
}

func captureMemoryStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(out), runErr
}
