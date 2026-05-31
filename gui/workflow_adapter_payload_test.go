package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"pgregory.net/rapid"
)

// --- Task 4.2 / Property 7: Round-trip stability of the emitted payload ---

// TestProperty7_PhaseMetadataPayloadRoundTrip asserts that marshaling
// workflow.PhaseMetadata(t) to JSON and parsing it back into []workflow.PhaseMeta
// preserves the phase IDs, their ascending index order, and the three boolean
// flags (expects_document / can_skip / needs_confirm) for every registered
// template. Nothing is dropped, duplicated, or reordered across the wire — this
// is the serialization contract the dashboard relies on (collectWorkflowPhases
// on the frontend consumes exactly this JSON shape).
//
// **Validates: Requirements 1.4**
func TestProperty7_PhaseMetadataPayloadRoundTrip(t *testing.T) {
	templates := workflow.NewWorkflowRegistry().All()
	if len(templates) == 0 {
		t.Fatal("registry returned no templates")
	}

	rapid.Check(t, func(rt *rapid.T) {
		tmpl := rapid.SampledFrom(templates).Draw(rt, "template")
		want := workflow.PhaseMetadata(tmpl)

		raw, err := json.Marshal(want)
		if err != nil {
			rt.Fatalf("%s: marshal PhaseMetadata failed: %v", tmpl.Type, err)
		}

		var got []workflow.PhaseMeta
		if err := json.Unmarshal(raw, &got); err != nil {
			rt.Fatalf("%s: unmarshal PhaseMetadata failed: %v", tmpl.Type, err)
		}

		// Nothing dropped or duplicated: same number of phases.
		if len(got) != len(want) {
			rt.Fatalf("%s: round-trip changed phase count: got %d, want %d", tmpl.Type, len(got), len(want))
		}

		for i := range want {
			// IDs preserved positionally (no reordering, no rename).
			if got[i].ID != want[i].ID {
				rt.Fatalf("%s: phase[%d] ID = %q, want %q", tmpl.Type, i, got[i].ID, want[i].ID)
			}
			// Ascending, contiguous index order preserved across the wire.
			if got[i].Index != i {
				rt.Fatalf("%s: phase[%d] Index = %d, want %d (ascending order must be preserved)", tmpl.Type, i, got[i].Index, i)
			}
			if got[i].Index != want[i].Index {
				rt.Fatalf("%s: phase[%d] Index = %d, want %d", tmpl.Type, i, got[i].Index, want[i].Index)
			}
			// Display label preserved.
			if got[i].Name != want[i].Name {
				rt.Fatalf("%s: phase[%d] Name = %q, want %q", tmpl.Type, i, got[i].Name, want[i].Name)
			}
			// All three boolean flags preserved.
			if got[i].ExpectsDocument != want[i].ExpectsDocument {
				rt.Fatalf("%s: phase[%d] (%s) ExpectsDocument = %v, want %v", tmpl.Type, i, got[i].ID, got[i].ExpectsDocument, want[i].ExpectsDocument)
			}
			if got[i].CanSkip != want[i].CanSkip {
				rt.Fatalf("%s: phase[%d] (%s) CanSkip = %v, want %v", tmpl.Type, i, got[i].ID, got[i].CanSkip, want[i].CanSkip)
			}
			if got[i].NeedsConfirm != want[i].NeedsConfirm {
				rt.Fatalf("%s: phase[%d] (%s) NeedsConfirm = %v, want %v", tmpl.Type, i, got[i].ID, got[i].NeedsConfirm, want[i].NeedsConfirm)
			}
		}
	})
}

// --- Task 4.3: Unit tests for the adapter's emitted JSON shape ---

// emittedPhase mirrors the JSON shape of workflow.PhaseMeta as it appears on the
// wire, so the tests assert against the serialized field names (the contract the
// frontend parses) rather than the Go struct.
type emittedPhase struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Index           int    `json:"index"`
	ExpectsDocument bool   `json:"expects_document"`
	CanSkip         bool   `json:"can_skip"`
	NeedsConfirm    bool   `json:"needs_confirm"`
}

// emittedState captures the fields of the emitted workflow:phase_update payload
// that the dashboard depends on, parsed from the marshaled frontend state.
type emittedState struct {
	CurrentPhase string                                 `json:"current_phase"`
	PhaseOutputs map[string]string                      `json:"phase_outputs"`
	GateResults  map[string]*workflow.QualityGateResult `json:"gate_results"`
	Phases       []emittedPhase                         `json:"phases"`
}

// TestAdapterEmittedJSONShapeWithRegistry asserts that, with a registry present,
// the emitted payload carries a populated `phases` array whose every element
// exposes the id/name/index/expects_document/can_skip/needs_confirm fields with
// the values derived from the backend template (single source of truth).
func TestAdapterEmittedJSONShapeWithRegistry(t *testing.T) {
	registry := workflow.NewWorkflowRegistry()
	state := &workflow.WorkflowState{
		Type:         workflow.WorkflowCoding,
		CurrentPhase: "tech_design",
	}

	raw, err := json.Marshal(normalizeWorkflowStateForFrontendWithRegistry(state, registry))
	if err != nil {
		t.Fatalf("marshal emitted state failed: %v", err)
	}

	var got emittedState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal emitted state failed: %v", err)
	}

	type want struct {
		id              string
		index           int
		expectsDocument bool
		canSkip         bool
		needsConfirm    bool
	}
	expected := []want{
		{"requirements", 0, true, false, true},
		{"design", 1, true, false, true},
		{"tasks", 2, true, true, true},
		{"implementation", 3, false, false, false},
		{"review", 4, true, true, true},
	}
	if len(got.Phases) != len(expected) {
		t.Fatalf("emitted phases count = %d, want %d: %#v", len(got.Phases), len(expected), got.Phases)
	}
	for i, w := range expected {
		p := got.Phases[i]
		if p.ID != w.id {
			t.Errorf("phase[%d] id = %q, want %q", i, p.ID, w.id)
		}
		if p.Index != w.index {
			t.Errorf("phase[%d] (%s) index = %d, want %d", i, p.ID, p.Index, w.index)
		}
		if strings.TrimSpace(p.Name) == "" {
			t.Errorf("phase[%d] (%s) has empty name in emitted JSON", i, p.ID)
		}
		if p.ExpectsDocument != w.expectsDocument {
			t.Errorf("phase[%d] (%s) expects_document = %v, want %v", i, p.ID, p.ExpectsDocument, w.expectsDocument)
		}
		if p.CanSkip != w.canSkip {
			t.Errorf("phase[%d] (%s) can_skip = %v, want %v", i, p.ID, p.CanSkip, w.canSkip)
		}
		if p.NeedsConfirm != w.needsConfirm {
			t.Errorf("phase[%d] (%s) needs_confirm = %v, want %v", i, p.ID, p.NeedsConfirm, w.needsConfirm)
		}
	}
}

// TestAdapterEmittedJSONOmitsPhasesWithNilRegistry asserts that, with a nil
// registry, the `phases` field is omitted from the emitted JSON entirely (via
// omitempty) so the dashboard degrades to its hardcoded fallback maps.
func TestAdapterEmittedJSONOmitsPhasesWithNilRegistry(t *testing.T) {
	state := &workflow.WorkflowState{
		Type:         workflow.WorkflowCoding,
		CurrentPhase: "requirements",
	}

	raw, err := json.Marshal(normalizeWorkflowStateForFrontendWithRegistry(state, nil))
	if err != nil {
		t.Fatalf("marshal emitted state failed: %v", err)
	}

	// The omitempty tag must drop the field entirely when no metadata is derived.
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("unmarshal emitted state into map failed: %v", err)
	}
	if _, present := asMap["phases"]; present {
		t.Fatalf("phases key must be omitted from JSON when registry is nil, got: %s", string(raw))
	}
	if strings.Contains(string(raw), "\"phases\"") {
		t.Fatalf("phases must not appear in emitted JSON with nil registry: %s", string(raw))
	}
}

// TestAdapterEmittedJSONCanonicalizesPhaseFields asserts that the emitted JSON
// canonicalizes CurrentPhase (tech_design -> design) and collapses alias-keyed
// PhaseOutputs / GateResults to their canonical keys, so the dashboard never
// sees a raw alias key on the wire.
func TestAdapterEmittedJSONCanonicalizesPhaseFields(t *testing.T) {
	state := &workflow.WorkflowState{
		Type:         workflow.WorkflowCoding,
		CurrentPhase: "tech_design",
		PhaseOutputs: map[string]string{
			"requirements":   "requirements doc",
			"tech_design":    "design doc",
			"task_breakdown": "tasks doc",
		},
		GateResults: map[string]*workflow.QualityGateResult{
			"tech_design": {PhaseID: "tech_design", Passed: true},
		},
	}

	raw, err := json.Marshal(normalizeWorkflowStateForFrontendWithRegistry(state, workflow.NewWorkflowRegistry()))
	if err != nil {
		t.Fatalf("marshal emitted state failed: %v", err)
	}

	var got emittedState
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal emitted state failed: %v", err)
	}

	if got.CurrentPhase != "design" {
		t.Fatalf("current_phase = %q, want design", got.CurrentPhase)
	}
	if got.PhaseOutputs["design"] != "design doc" {
		t.Fatalf("phase_outputs[design] = %q, want %q (alias key tech_design must collapse)", got.PhaseOutputs["design"], "design doc")
	}
	if got.PhaseOutputs["tasks"] != "tasks doc" {
		t.Fatalf("phase_outputs[tasks] = %q, want %q (alias key task_breakdown must collapse)", got.PhaseOutputs["tasks"], "tasks doc")
	}
	if _, leaked := got.PhaseOutputs["tech_design"]; leaked {
		t.Fatalf("raw alias key tech_design leaked into emitted phase_outputs: %#v", got.PhaseOutputs)
	}
	if _, leaked := got.PhaseOutputs["task_breakdown"]; leaked {
		t.Fatalf("raw alias key task_breakdown leaked into emitted phase_outputs: %#v", got.PhaseOutputs)
	}
	gate := got.GateResults["design"]
	if gate == nil || gate.PhaseID != "design" {
		t.Fatalf("gate_results[design] not canonicalized: %#v", got.GateResults)
	}
	if _, leaked := got.GateResults["tech_design"]; leaked {
		t.Fatalf("raw alias key tech_design leaked into emitted gate_results: %#v", got.GateResults)
	}
}
