package workflow

import (
	"strings"
	"testing"
	"time"
)

// Task 11.3 — Regression tests for all-template coverage and phase-index alignment.
//
// These two regressions lock in the mechanism-level guarantees behind
// Requirements 6.2 and 6.4:
//
//   - 6.2: every one of the registered workflow templates renders through the
//     single shared PhaseMetadata deriver, with no template-specific dashboard
//     path. The test iterates registry.All() and proves each template produces
//     well-formed metadata (>=1 phase, contiguous 0-based indices, non-empty
//     canonical IDs and labels).
//   - 6.4: the workflow engine keeps the current phase index equal to the
//     position of the current phase ID within the canonical (alias-applied,
//     de-duplicated) phase order — the exact order the dashboard board uses to
//     highlight the active node and compute progress.

// canonicalPhaseIndex returns the 0-based position of canonicalID within the
// canonical de-duplicated phase order (the output of PhaseMetadata), or -1 when
// the ID is absent. Because PhaseMetadata assigns Index == slice position, the
// returned Index is exactly that position.
func canonicalPhaseIndex(canonicalID string, metas []PhaseMeta) int {
	for _, m := range metas {
		if m.ID == canonicalID {
			return m.Index
		}
	}
	return -1
}

// --- Task 11.3 / Requirement 6.2: all templates render through the one deriver ---

// TestRegression_AllTemplatesRenderThroughSharedDeriver asserts that EVERY
// registered template is projected to well-formed dashboard metadata through
// the single shared workflow.PhaseMetadata deriver — there is no
// template-specific dashboard path. For each template it verifies the deriver
// emits at least one phase, contiguous 0-based indices, canonical (alias-applied,
// idempotent) and unique phase IDs, and a non-empty display label per phase.
//
// _Requirements: 6.2_
func TestRegression_AllTemplatesRenderThroughSharedDeriver(t *testing.T) {
	templates := NewWorkflowRegistry().All()
	if len(templates) == 0 {
		t.Fatal("registry returned no templates")
	}
	// The feature targets the 19+ built-in templates; guard against a registry
	// that silently lost templates.
	if len(templates) < 19 {
		t.Fatalf("expected at least 19 registered templates, got %d", len(templates))
	}

	for _, tmpl := range templates {
		if tmpl == nil {
			t.Fatal("registry returned a nil template")
		}

		// The ONE mechanism: every template goes through PhaseMetadata. No
		// template-specific branch exists.
		metas := PhaseMetadata(tmpl)
		if len(metas) == 0 {
			t.Fatalf("%s: PhaseMetadata produced no phases; every registered template must render >=1 phase through the shared deriver", tmpl.Type)
		}

		seen := make(map[string]bool, len(metas))
		for i, m := range metas {
			// Contiguous, strictly increasing 0-based index.
			if m.Index != i {
				t.Errorf("%s: phase %q has Index=%d, want %d (indices must be contiguous 0..n-1)", tmpl.Type, m.ID, m.Index, i)
			}
			// Non-empty canonical ID.
			if strings.TrimSpace(m.ID) == "" {
				t.Errorf("%s: phase at index %d has an empty/whitespace ID", tmpl.Type, i)
			}
			// The emitted ID is already canonical: canonicalizing it again is a
			// no-op (idempotent). This proves the deriver applied aliasing.
			if got := CanonicalPhaseID(m.ID); got != m.ID {
				t.Errorf("%s: emitted phase ID %q is not canonical (CanonicalPhaseID -> %q)", tmpl.Type, m.ID, got)
			}
			// Canonical IDs are de-duplicated within a template.
			if seen[m.ID] {
				t.Errorf("%s: duplicate canonical phase ID %q in derived metadata", tmpl.Type, m.ID)
			}
			seen[m.ID] = true
			// Non-empty display label, so the board never renders a bare ID.
			if strings.TrimSpace(m.Name) == "" {
				t.Errorf("%s: phase %q (index %d) has an empty/whitespace label", tmpl.Type, m.ID, m.Index)
			}
		}
	}
}

// --- Task 11.3 / Requirement 6.4: phase index aligns with canonical order ---

// TestRegression_PhaseIndexAlignsWithCanonicalPhaseOrder drives every registered
// template through all of its phases using the engine's own advancement
// mechanism and asserts, at each phase, that WorkflowState.PhaseIndex equals the
// position of the canonical current phase ID within the canonical (alias-applied,
// de-duplicated) phase order produced by PhaseMetadata. This is the exact
// invariant the dashboard relies on to highlight the active node and compute
// progress for any of the 19 templates.
//
// _Requirements: 6.4_
func TestRegression_PhaseIndexAlignsWithCanonicalPhaseOrder(t *testing.T) {
	for _, tmpl := range NewWorkflowRegistry().All() {
		if tmpl == nil || len(tmpl.Phases) == 0 {
			t.Fatalf("template %v has no phases", tmpl)
		}
		metas := PhaseMetadata(tmpl)
		if len(metas) == 0 {
			t.Fatalf("%s: PhaseMetadata produced no phases", tmpl.Type)
		}

		// Fresh engine per template so each workflow starts at phase 0.
		engine, _ := newTestEngine()
		userID := "align_" + string(tmpl.Type)
		if _, err := engine.StartWorkflow(userID, StructuredIntent{Category: tmpl.Type, Summary: "phase-index alignment test"}); err != nil {
			t.Fatalf("%s: StartWorkflow failed: %v", tmpl.Type, err)
		}

		for i := 0; i < len(tmpl.Phases); i++ {
			// Snapshot the engine-owned state under the lock; advance with the
			// engine's own mechanism (the single writer of PhaseIndex +
			// CurrentPhase) for every phase except the last.
			engine.mu.Lock()
			ws := engine.workflows[userID]
			present := ws != nil
			var gotIndex int
			var gotPhase string
			if present {
				gotIndex = ws.PhaseIndex
				gotPhase = ws.CurrentPhase
			}
			var advErr error
			if present && i < len(tmpl.Phases)-1 {
				_, advErr = engine.advancePhase(userID, ws, tmpl)
			}
			engine.mu.Unlock()

			if !present {
				t.Fatalf("%s: no active workflow at phase step %d", tmpl.Type, i)
			}
			// Engine invariant: the raw index points at the current phase ID.
			if gotIndex != i || tmpl.Phases[i].ID != gotPhase {
				t.Fatalf("%s: engine phase pointer broken at step %d: PhaseIndex=%d CurrentPhase=%q template[%d].ID=%q",
					tmpl.Type, i, gotIndex, gotPhase, i, tmpl.Phases[i].ID)
			}
			// Requirement 6.4: PhaseIndex equals the position of the canonical
			// current phase ID within the canonical de-duplicated phase order.
			canonicalID := CanonicalPhaseID(gotPhase)
			wantPos := canonicalPhaseIndex(canonicalID, metas)
			if wantPos < 0 {
				t.Fatalf("%s: current phase %q (canonical %q) not found in canonical phase order %v",
					tmpl.Type, gotPhase, canonicalID, idsOf(metas))
			}
			if gotIndex != wantPos {
				t.Fatalf("%s: PhaseIndex=%d but canonical position of %q (canonical %q) is %d",
					tmpl.Type, gotIndex, gotPhase, canonicalID, wantPos)
			}
			if advErr != nil {
				t.Fatalf("%s: advancePhase at step %d failed: %v", tmpl.Type, i, advErr)
			}
		}
	}
}

// TestRegression_RepairAlignsPhaseIndexWithCanonicalOrder asserts that the
// engine's consistency-repair logic (Unchanged Behavior 3.4) restores the
// PhaseIndex so that it equals the canonical position of the current phase ID.
// A persisted workflow with a corrupt PhaseIndex but a valid CurrentPhase is
// restored and repaired; the repaired index must agree with the canonical
// (alias-applied, de-duplicated) phase order produced by PhaseMetadata.
//
// _Requirements: 6.4_
func TestRegression_RepairAlignsPhaseIndexWithCanonicalOrder(t *testing.T) {
	engine, _ := newTestEngine()
	tmpl := engine.GetRegistry().Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not registered")
	}
	metas := PhaseMetadata(tmpl)

	// Corrupt: CurrentPhase is the (raw) tech_design phase but PhaseIndex is
	// invalid. The first phase has output, so repair must not rewind.
	state := &WorkflowState{
		ID:           "wf-align-repair",
		UserID:       "u_align_repair",
		Type:         WorkflowCoding,
		CurrentPhase: PhaseCodingTechDesign,
		PhaseIndex:   -1,
		PhaseOutputs: map[string]string{PhaseCodingRequirements: reviewStateValidContent},
		GateResults:  map[string]*QualityGateResult{},
		Status:       WorkflowActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	store := &recordingRestoreStore{states: []*WorkflowState{state}}
	engine.store = store

	if err := engine.RestoreFromStore(); err != nil {
		t.Fatalf("RestoreFromStore failed: %v", err)
	}

	active := engine.GetActiveWorkflow("u_align_repair")
	if active == nil {
		t.Fatal("repaired workflow should be active")
	}
	// Engine invariant after repair.
	if active.PhaseIndex < 0 || active.PhaseIndex >= len(tmpl.Phases) || tmpl.Phases[active.PhaseIndex].ID != active.CurrentPhase {
		t.Fatalf("repair left phase pointer inconsistent: PhaseIndex=%d CurrentPhase=%q", active.PhaseIndex, active.CurrentPhase)
	}
	// Requirement 6.4: repaired index equals canonical position of current phase.
	wantPos := canonicalPhaseIndex(CanonicalPhaseID(active.CurrentPhase), metas)
	if wantPos < 0 {
		t.Fatalf("current phase %q has no canonical position in %v", active.CurrentPhase, idsOf(metas))
	}
	if active.PhaseIndex != wantPos {
		t.Fatalf("repaired PhaseIndex=%d but canonical position of %q is %d", active.PhaseIndex, active.CurrentPhase, wantPos)
	}
}
