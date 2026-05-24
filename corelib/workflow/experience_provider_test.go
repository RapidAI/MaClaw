package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

func TestExperienceProviderSearchesPhaseOutput(t *testing.T) {
	ws := &WorkflowState{
		ID:           "wf-1",
		UserID:       "u1",
		Type:         WorkflowCoding,
		Status:       WorkflowActive,
		ProjectPath:  `D:\work\alpha`,
		PhaseOutputs: map[string]string{PhaseCodingTechDesign: "Use sqlite WAL isolation before running go test."},
	}
	provider := NewExperienceProvider(ws)

	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{
		Text:     "sqlite wal go test",
		Types:    []lifecycle.EntryType{lifecycle.EntryTypeEpisodic},
		Boundary: lifecycle.Boundary{OwnerID: "u1", ProjectPath: `D:\work\alpha`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected phase output candidate, got %+v", candidates)
	}
	if candidates[0].Entry.EntryType != lifecycle.EntryTypeEpisodic || candidates[0].BoundaryScore <= 0 {
		t.Fatalf("unexpected workflow candidate: %+v", candidates[0])
	}
}

func TestExperienceProviderSearchesGateFailure(t *testing.T) {
	ws := &WorkflowState{
		ID:     "wf-2",
		UserID: "u1",
		Type:   WorkflowOpsMaintenance,
		Status: WorkflowActive,
		GateResults: map[string]*QualityGateResult{
			"risk_policy": {
				PhaseID:   "risk_policy",
				Passed:    false,
				CheckedAt: time.Now(),
				Items:     []GateCheckItem{{Description: "missing rollback plan", Passed: false, Note: "rollback command absent"}},
			},
		},
	}
	provider := NewExperienceProvider(ws)

	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{
		Text:  "rollback risk policy",
		Types: []lifecycle.EntryType{lifecycle.EntryTypeFailureSkill},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected gate failure candidate, got %+v", candidates)
	}
	entry := candidates[0].Entry
	if entry.Governance != lifecycle.GovernanceDraft || !strings.Contains(entry.Content, "rollback command absent") {
		t.Fatalf("expected draft failure evidence, got %+v", entry)
	}
}

func TestExperienceProviderSearchesReviewRevision(t *testing.T) {
	ws := &WorkflowState{
		ID:                             "wf-3",
		UserID:                         "u1",
		Type:                           WorkflowProductDesign,
		Status:                         WorkflowActive,
		PendingReviewPhaseID:           "brief",
		PendingReviewRevisionRequested: true,
	}
	provider := NewExperienceProvider(ws)

	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{
		Text:  "review feedback revise product design brief",
		Types: []lifecycle.EntryType{lifecycle.EntryTypeComparativeSkill},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Entry.EntryType != lifecycle.EntryTypeComparativeSkill {
		t.Fatalf("expected review revision comparative skill, got %+v", candidates)
	}
}
