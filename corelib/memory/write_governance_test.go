package memory

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAssessMemoryCandidateRejectsSmallTalk(t *testing.T) {
	decision := AssessMemoryCandidate(Entry{Content: "thanks", Category: CategoryProjectKnowledge}, "")
	if decision.Action != MemoryGovernanceReject {
		t.Fatalf("expected reject, got %+v", decision)
	}
}

func TestSaveGovernedWithContextQuarantinesWeakCandidate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	decision, err := store.SaveGovernedWithContext(Entry{Content: "I just ran the current task and it is probably okay", Category: CategoryProjectKnowledge}, "")
	if err != nil {
		t.Fatalf("expected quarantine save, got err=%v decision=%+v", err, decision)
	}
	if decision.Action != MemoryGovernanceQuarantine {
		t.Fatalf("expected quarantine, got %+v", decision)
	}
	entries := store.List("", "memory_candidate")
	if len(entries) != 1 || entries[0].Status != StatusDormant {
		t.Fatalf("expected one dormant candidate, got %+v", entries)
	}
	if got := store.RecallDynamic("current task probably okay", "", ""); len(got) != 0 {
		t.Fatalf("quarantined candidate should not be recalled, got %+v", got)
	}
}

func TestSaveGovernedWithContextAcceptsProjectFact(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	decision, err := store.SaveGovernedWithContext(Entry{Content: "Project API endpoint is https://api.example.com and test command is pnpm test", Category: CategoryProjectKnowledge}, "")
	if err != nil {
		t.Fatalf("save governed: %v", err)
	}
	if decision.Action != MemoryGovernanceAccept {
		t.Fatalf("expected accept, got %+v", decision)
	}
	if got := store.RecallDynamic("api endpoint pnpm test", CategoryProjectKnowledge, ""); len(got) == 0 {
		t.Fatalf("accepted project fact should be recallable")
	}
}

func TestSaveGovernedWithContextRejectsEmptyCandidate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	_, err = store.SaveGovernedWithContext(Entry{Content: "", Category: CategoryProjectKnowledge}, "")
	if !errors.Is(err, ErrMemoryCandidateRejected) {
		t.Fatalf("expected rejected error, got %v", err)
	}
}
