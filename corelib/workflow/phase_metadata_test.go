package workflow

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// --- Test helpers (shared by the property and unit tests) ---

// idsOf projects a PhaseMeta slice to its ordered list of canonical IDs.
func idsOf(metas []PhaseMeta) []string {
	if len(metas) == 0 {
		return nil
	}
	ids := make([]string, len(metas))
	for i, m := range metas {
		ids[i] = m.ID
	}
	return ids
}

// dedupCanonical is the reference implementation of "distinct canonical phase
// IDs in first-occurrence order". It mirrors the rule PhaseMetadata applies:
// canonicalize each phase ID, drop empty canonical IDs, and keep the first
// occurrence of each canonical ID. The property tests assert that PhaseMetadata
// agrees with this independent computation.
func dedupCanonical(phases []PhaseTemplate) []string {
	seen := make(map[string]bool, len(phases))
	var ids []string
	for _, p := range phases {
		id := CanonicalPhaseID(p.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// syntheticPhaseIDPool mixes alias-prone IDs (to exercise canonical collapse),
// plain IDs, and the empty string (to exercise the drop rule).
var syntheticPhaseIDPool = []string{
	"requirements", "requirement", "req",
	"design", "tech_design", "technical_design",
	"tasks", "task", "task_plan", "task_breakdown",
	"implementation", "review", "phase_a", "phase_b", "",
}

// syntheticToolPolicies covers the defined policies plus an unknown/empty value
// so the document-expectation rule is exercised beyond the enum.
var syntheticToolPolicies = []ToolFilterPolicy{
	ToolFilterNone, ToolFilterDocOnly, ToolFilterPlanning, ToolFilterFull, ToolFilterOpsControlled,
	ToolFilterPolicy("unknown_policy"), ToolFilterPolicy(""),
}

// drawSyntheticTemplate builds a template with a randomized phase list,
// randomized tool policies, randomized alias-prone IDs, and randomized flags.
func drawSyntheticTemplate(t *rapid.T) *WorkflowTemplate {
	n := rapid.IntRange(0, 8).Draw(t, "numPhases")
	phases := make([]PhaseTemplate, n)
	for i := 0; i < n; i++ {
		phases[i] = PhaseTemplate{
			ID:           rapid.SampledFrom(syntheticPhaseIDPool).Draw(t, fmt.Sprintf("id%d", i)),
			Name:         rapid.String().Draw(t, fmt.Sprintf("name%d", i)),
			ToolPolicy:   rapid.SampledFrom(syntheticToolPolicies).Draw(t, fmt.Sprintf("policy%d", i)),
			CanSkip:      rapid.Bool().Draw(t, fmt.Sprintf("skip%d", i)),
			NeedsConfirm: rapid.Bool().Draw(t, fmt.Sprintf("confirm%d", i)),
		}
	}
	return &WorkflowTemplate{Type: WorkflowType("synthetic"), Name: "Synthetic", Phases: phases}
}

// assertPhaseOrder checks Property 1 for a single template: the derived phase-ID
// order equals dedup(canonical(phases)) and Index is contiguous 0..n-1.
func assertPhaseOrder(t *rapid.T, tmpl *WorkflowTemplate) {
	t.Helper()
	metas := PhaseMetadata(tmpl)
	want := dedupCanonical(tmpl.Phases)
	got := idsOf(metas)
	if !stringSlicesEqual(got, want) {
		t.Fatalf("phase order mismatch for %s: got %v, want %v", tmpl.Type, got, want)
	}
	for i, m := range metas {
		if m.Index != i {
			t.Fatalf("%s: meta[%d].Index = %d, want %d (must be contiguous 0..n-1)", tmpl.Type, i, m.Index, i)
		}
	}
}

// --- Task 1.2 / Property 1: Dashboard-derived phase order equals template order ---

// TestProperty1_PhaseOrderMatchesTemplate asserts that, for every registered
// template and for arbitrary synthetic templates, the ordered list of phase IDs
// PhaseMetadata derives equals the template's phase order after canonicalization
// and de-duplication, and that Index is contiguous and strictly increasing.
//
// **Validates: Requirements 1.1**
func TestProperty1_PhaseOrderMatchesTemplate(t *testing.T) {
	templates := NewWorkflowRegistry().All()
	if len(templates) == 0 {
		t.Fatal("registry returned no templates")
	}

	// Over all registered built-in templates.
	rapid.Check(t, func(t *rapid.T) {
		tmpl := rapid.SampledFrom(templates).Draw(t, "template")
		assertPhaseOrder(t, tmpl)
	})

	// Over synthetic templates with randomized phase lists/policies/aliases.
	rapid.Check(t, func(t *rapid.T) {
		assertPhaseOrder(t, drawSyntheticTemplate(t))
	})
}

// --- Task 1.3 / Property 6: Document-expectation follows review gate before ToolPolicy ---

// TestProperty6_DocumentExpectationFromToolPolicy asserts that PhaseExpectsDocument
// is true for reviewable phases and otherwise false exactly when the phase uses
// ToolFilterFull or ToolFilterOpsControlled, across every real template phase
// and arbitrary synthetic ToolPolicy values.
//
// **Validates: Requirements 1.3**
func TestProperty6_DocumentExpectationFromToolPolicy(t *testing.T) {
	var allPhases []PhaseTemplate
	for _, tmpl := range NewWorkflowRegistry().All() {
		allPhases = append(allPhases, tmpl.Phases...)
	}
	if len(allPhases) == 0 {
		t.Fatal("registry templates have no phases")
	}

	// Over all real template phases.
	rapid.Check(t, func(t *rapid.T) {
		p := rapid.SampledFrom(allPhases).Draw(t, "phase")
		want := p.NeedsConfirm || (p.ToolPolicy != ToolFilterFull && p.ToolPolicy != ToolFilterOpsControlled)
		if got := PhaseExpectsDocument(p); got != want {
			t.Fatalf("PhaseExpectsDocument(policy=%q needs_confirm=%v) = %v, want %v", p.ToolPolicy, p.NeedsConfirm, got, want)
		}
	})

	// Over synthetic phases with randomized ToolPolicy values.
	rapid.Check(t, func(t *rapid.T) {
		p := PhaseTemplate{
			ID:         rapid.String().Draw(t, "id"),
			Name:       rapid.String().Draw(t, "name"),
			ToolPolicy: rapid.SampledFrom(syntheticToolPolicies).Draw(t, "policy"),
		}
		p.NeedsConfirm = rapid.Bool().Draw(t, "needs_confirm")
		want := p.NeedsConfirm || (p.ToolPolicy != ToolFilterFull && p.ToolPolicy != ToolFilterOpsControlled)
		if got := PhaseExpectsDocument(p); got != want {
			t.Fatalf("PhaseExpectsDocument(policy=%q needs_confirm=%v) = %v, want %v", p.ToolPolicy, p.NeedsConfirm, got, want)
		}
	})
}

// --- Task 1.4 / Property 2: Every emitted phaseID has a non-empty label ---

// TestProperty2_EveryPhaseHasNonEmptyLabel asserts that every PhaseMeta derived
// from a registered template has a Name with at least one non-whitespace
// character, so the dashboard never renders a bare phase ID for a built-in
// workflow.
//
// **Validates: Requirements 1.2**
func TestProperty2_EveryPhaseHasNonEmptyLabel(t *testing.T) {
	templates := NewWorkflowRegistry().All()
	if len(templates) == 0 {
		t.Fatal("registry returned no templates")
	}

	rapid.Check(t, func(t *rapid.T) {
		tmpl := rapid.SampledFrom(templates).Draw(t, "template")
		for _, m := range PhaseMetadata(tmpl) {
			if strings.TrimSpace(m.Name) == "" {
				t.Fatalf("%s: phase %q (index %d) has empty/whitespace label", tmpl.Type, m.ID, m.Index)
			}
		}
	})
}

// --- Task 1.5: Unit tests for the deriver ---

func TestCanonicalPhaseID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"requirements canonical", "requirements", "requirements"},
		{"requirement alias", "requirement", "requirements"},
		{"req alias", "req", "requirements"},
		{"design canonical", "design", "design"},
		{"tech_design alias collapses to design", "tech_design", "design"},
		{"technical_design alias collapses to design", "technical_design", "design"},
		{"tasks canonical", "tasks", "tasks"},
		{"task alias", "task", "tasks"},
		{"task_plan alias", "task_plan", "tasks"},
		{"task_breakdown alias collapses to tasks", "task_breakdown", "tasks"},
		{"uppercase alias normalized", "Tech_Design", "design"},
		{"surrounding whitespace trimmed for matching", "  requirements  ", "requirements"},
		{"unknown id passes through unchanged", "implementation", "implementation"},
		{"another unknown id passes through", "review", "review"},
		{"empty id stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanonicalPhaseID(tc.in); got != tc.want {
				t.Errorf("CanonicalPhaseID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPhaseExpectsDocument(t *testing.T) {
	cases := []struct {
		name   string
		policy ToolFilterPolicy
		want   bool
	}{
		{"ToolFilterFull is an execution phase", ToolFilterFull, false},
		{"ToolFilterOpsControlled is an execution phase", ToolFilterOpsControlled, false},
		{"ToolFilterDocOnly produces a document", ToolFilterDocOnly, true},
		{"ToolFilterNone produces a document", ToolFilterNone, true},
		{"unknown policy defaults to document", ToolFilterPolicy("unknown"), true},
		{"empty policy defaults to document", ToolFilterPolicy(""), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := PhaseTemplate{ID: "p", Name: "Phase", ToolPolicy: tc.policy}
			if got := PhaseExpectsDocument(p); got != tc.want {
				t.Errorf("PhaseExpectsDocument(policy=%q) = %v, want %v", tc.policy, got, tc.want)
			}
		})
	}
}

func TestPhaseMetadata_NilAndEmpty(t *testing.T) {
	if got := PhaseMetadata(nil); got != nil {
		t.Errorf("PhaseMetadata(nil) = %v, want nil", got)
	}
	empty := &WorkflowTemplate{Type: WorkflowType("empty"), Name: "Empty", Phases: nil}
	if got := PhaseMetadata(empty); got != nil {
		t.Errorf("PhaseMetadata(empty phases) = %v, want nil", got)
	}
}

func TestPhaseMetadata_CodingTemplate(t *testing.T) {
	tmpl := NewWorkflowRegistry().Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not registered")
	}
	metas := PhaseMetadata(tmpl)

	// tech_design -> design, task_breakdown -> tasks; everything else passes through.
	wantIDs := []string{"requirements", "design", "tasks", "implementation", "review"}
	if got := idsOf(metas); !stringSlicesEqual(got, wantIDs) {
		t.Fatalf("coding phase IDs = %v, want %v", got, wantIDs)
	}

	type expect struct {
		index           int
		expectsDocument bool
		canSkip         bool
		needsConfirm    bool
	}
	want := map[string]expect{
		"requirements":   {0, true, false, true},
		"design":         {1, true, false, true},
		"tasks":          {2, true, true, true},    // task_breakdown has CanSkip=true
		"implementation": {3, false, false, false}, // ToolFilterFull -> no document
		"review":         {4, true, true, true},
	}
	for _, m := range metas {
		w, ok := want[m.ID]
		if !ok {
			t.Errorf("unexpected phase %q", m.ID)
			continue
		}
		if m.Index != w.index {
			t.Errorf("%s: Index = %d, want %d", m.ID, m.Index, w.index)
		}
		if m.ExpectsDocument != w.expectsDocument {
			t.Errorf("%s: ExpectsDocument = %v, want %v", m.ID, m.ExpectsDocument, w.expectsDocument)
		}
		if m.CanSkip != w.canSkip {
			t.Errorf("%s: CanSkip = %v, want %v", m.ID, m.CanSkip, w.canSkip)
		}
		if m.NeedsConfirm != w.needsConfirm {
			t.Errorf("%s: NeedsConfirm = %v, want %v", m.ID, m.NeedsConfirm, w.needsConfirm)
		}
		if strings.TrimSpace(m.Name) == "" {
			t.Errorf("%s: empty label", m.ID)
		}
	}
}

func TestPhaseExpectsDocumentTreatsReviewablePlanningPhaseAsDocument(t *testing.T) {
	phase := PhaseTemplate{
		ID:           PhaseCodingTaskBreakdown,
		ToolPolicy:   ToolFilterPlanning,
		NeedsConfirm: true,
	}
	if !PhaseExpectsDocument(phase) {
		t.Fatal("reviewable planning phase should still produce a document")
	}
}

func TestPhaseMetadata_PPTGenerationIsExecutionPhase(t *testing.T) {
	tmpl := NewWorkflowRegistry().Match(WorkflowPresentationDesign)
	if tmpl == nil {
		t.Fatal("presentation_design template not registered")
	}
	metas := PhaseMetadata(tmpl)

	var found bool
	for _, m := range metas {
		if m.ID == "ppt_generation" {
			found = true
			if m.ExpectsDocument {
				t.Errorf("ppt_generation uses ToolFilterFull and must not expect a document")
			}
		}
	}
	if !found {
		t.Fatal("ppt_generation phase not found in presentation_design metadata")
	}
}

func TestPhaseMetadata_OpsControlledExecutionPhase(t *testing.T) {
	tmpl := NewWorkflowRegistry().Match(WorkflowOpsMaintenance)
	if tmpl == nil {
		t.Fatal("ops_maintenance template not registered")
	}
	metas := PhaseMetadata(tmpl)

	var found bool
	for _, m := range metas {
		if m.ID == "controlled_execution" {
			found = true
			if m.ExpectsDocument {
				t.Errorf("controlled_execution uses ToolFilterOpsControlled and must not expect a document")
			}
			if !m.CanSkip {
				t.Errorf("controlled_execution has CanSkip=true in the template; CanSkip must propagate")
			}
		}
	}
	if !found {
		t.Fatal("controlled_execution phase not found in ops_maintenance metadata")
	}
}

func TestPhaseMetadata_DuplicateCanonicalIDCollapse(t *testing.T) {
	// tech_design and design both canonicalize to "design"; first occurrence wins.
	tmpl := &WorkflowTemplate{
		Type: WorkflowType("dup_test"),
		Name: "Dup",
		Phases: []PhaseTemplate{
			{ID: "tech_design", Name: "First Design", ToolPolicy: ToolFilterDocOnly},
			{ID: "design", Name: "Second Design", ToolPolicy: ToolFilterDocOnly},
			{ID: "task_breakdown", Name: "Tasks", ToolPolicy: ToolFilterDocOnly},
		},
	}
	metas := PhaseMetadata(tmpl)

	wantIDs := []string{"design", "tasks"}
	if got := idsOf(metas); !stringSlicesEqual(got, wantIDs) {
		t.Fatalf("collapsed IDs = %v, want %v", got, wantIDs)
	}
	if metas[0].Name != "First Design" {
		t.Errorf("first occurrence should win: Name = %q, want %q", metas[0].Name, "First Design")
	}
	if metas[0].Index != 0 || metas[1].Index != 1 {
		t.Errorf("indices must be contiguous after collapse: got %d,%d", metas[0].Index, metas[1].Index)
	}
}

func TestPhaseMetadata_EmptyCanonicalIDDropped(t *testing.T) {
	tmpl := &WorkflowTemplate{
		Type: WorkflowType("empty_id_test"),
		Name: "EmptyID",
		Phases: []PhaseTemplate{
			{ID: "", Name: "Dropped", ToolPolicy: ToolFilterDocOnly},
			{ID: "requirements", Name: "Kept", ToolPolicy: ToolFilterDocOnly},
		},
	}
	metas := PhaseMetadata(tmpl)
	wantIDs := []string{"requirements"}
	if got := idsOf(metas); !stringSlicesEqual(got, wantIDs) {
		t.Fatalf("IDs = %v, want %v (empty canonical ID must be dropped)", got, wantIDs)
	}
	if metas[0].Index != 0 {
		t.Errorf("re-index after drop: Index = %d, want 0", metas[0].Index)
	}
}
