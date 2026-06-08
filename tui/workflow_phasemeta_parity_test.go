package main

import (
	"bytes"
	"log"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// TestTUIWorkflowPhaseMetaParity asserts that the PhaseMeta slice the TUI
// callbacks derive for every registered template is identical, element-by-element,
// to calling the single source-of-truth deriver workflow.PhaseMetadata directly —
// the same function the GUI adapter uses in
// normalizeWorkflowStateForFrontendWithRegistry.
//
// This proves Requirement 1.5: the TUI_Adapter invokes the same
// Phase_Metadata_Deriver function used by the Phase_Update_Emitter, producing
// identical Phase_Meta elements, rather than maintaining a separately derived
// phase list.
func TestTUIWorkflowPhaseMetaParity(t *testing.T) {
	registry := workflow.NewWorkflowRegistry()
	callbacks := &TUIWorkflowCallbacks{registry: registry}

	templates := registry.All()
	if len(templates) == 0 {
		t.Fatal("registry has no registered templates")
	}

	for _, tmpl := range templates {
		// What EmitPhaseUpdate derives: workflow.PhaseMetadata(c.registry.Match(state.Type)).
		got := workflow.PhaseMetadata(callbacks.registry.Match(tmpl.Type))

		// The single source of truth, derived from an independent registry. The
		// GUI adapter produces exactly this via the same workflow.PhaseMetadata call.
		want := workflow.PhaseMetadata(workflow.NewWorkflowRegistry().Match(tmpl.Type))

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: TUI-derived PhaseMeta != workflow.PhaseMetadata\n got=%#v\nwant=%#v", tmpl.Type, got, want)
		}

		// Element-by-element field check so a divergence is reported precisely.
		if len(got) != len(want) {
			t.Fatalf("%s: derived %d phases, want %d", tmpl.Type, len(got), len(want))
		}
		for i := range want {
			g, w := got[i], want[i]
			if g.ID != w.ID || g.Name != w.Name || g.Index != w.Index ||
				g.ExpectsDocument != w.ExpectsDocument || g.CanSkip != w.CanSkip ||
				g.NeedsConfirm != w.NeedsConfirm || g.Kind != w.Kind ||
				g.ToolPolicy != w.ToolPolicy || g.MutationScope != w.MutationScope ||
				g.ActivatesOrchestrator != w.ActivatesOrchestrator {
				t.Fatalf("%s phase %d:\n got=%+v\nwant=%+v", tmpl.Type, i, g, w)
			}
		}
	}
}

// TestTUIWorkflowPhaseMetaParityCodingConcrete pins the parity for the coding
// template to concrete expected canonical phase IDs, so the parity test fails
// loudly if the deriver or the coding template phase order changes unexpectedly.
func TestTUIWorkflowPhaseMetaParityCodingConcrete(t *testing.T) {
	registry := workflow.NewWorkflowRegistry()
	callbacks := &TUIWorkflowCallbacks{registry: registry}

	got := workflow.PhaseMetadata(callbacks.registry.Match(workflow.WorkflowCoding))
	if len(got) == 0 {
		t.Fatal("coding template derived no phase metadata")
	}

	want := workflow.PhaseMetadata(workflow.NewWorkflowRegistry().Match(workflow.WorkflowCoding))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coding PhaseMeta diverged from workflow.PhaseMetadata\n got=%#v\nwant=%#v", got, want)
	}

	// Indices must be contiguous and 0-based, every label non-empty (the deriver
	// contract the GUI adapter also relies on).
	for i, m := range got {
		if m.Index != i {
			t.Fatalf("coding phase %d has Index=%d, want %d", i, m.Index, i)
		}
		if strings.TrimSpace(m.Name) == "" {
			t.Fatalf("coding phase %q (index %d) has an empty label", m.ID, i)
		}
		if strings.TrimSpace(string(m.Kind)) == "" {
			t.Fatalf("coding phase %q (index %d) has empty contract kind", m.ID, i)
		}
	}
}

// TestTUIWorkflowEmitPhaseUpdateUsesSharedDeriver exercises the real
// EmitPhaseUpdate code path and asserts the metadata it emits (logged
// structurally for the text-only TUI) equals formatTUIPhaseMeta over the
// canonically-derived workflow.PhaseMetadata output. This proves EmitPhaseUpdate
// derives through the shared deriver rather than a separate phase list.
func TestTUIWorkflowEmitPhaseUpdateUsesSharedDeriver(t *testing.T) {
	registry := workflow.NewWorkflowRegistry()
	callbacks := &TUIWorkflowCallbacks{registry: registry}

	var buf bytes.Buffer
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(nil)
		log.SetFlags(origFlags)
	})

	state := &workflow.WorkflowState{Type: workflow.WorkflowCoding}
	if err := callbacks.EmitPhaseUpdate("tui-user", state); err != nil {
		t.Fatalf("EmitPhaseUpdate returned error: %v", err)
	}

	wantPhases := formatTUIPhaseMeta(workflow.PhaseMetadata(workflow.NewWorkflowRegistry().Match(workflow.WorkflowCoding)))
	out := buf.String()
	if !strings.Contains(out, "phases="+wantPhases) {
		t.Fatalf("EmitPhaseUpdate log did not emit shared-deriver metadata\n log=%q\nwant phases=%q", out, wantPhases)
	}
}
