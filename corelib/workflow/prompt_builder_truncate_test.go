package workflow

import (
	"strings"
	"testing"
)

const truncationMarker = "...(truncated)"

func TestTruncateRunesSmart_ShortString(t *testing.T) {
	s := "hello world"
	got := truncateRunesSmart(s, 100)
	if got != s {
		t.Errorf("expected unchanged string, got %q", got)
	}
}

func TestTruncateRunesSmart_ExactLimit(t *testing.T) {
	s := "abcde"
	got := truncateRunesSmart(s, 5)
	if got != s {
		t.Errorf("expected unchanged string, got %q", got)
	}
}

func TestTruncateRunesSmart_ParagraphBreak(t *testing.T) {
	before := strings.Repeat("a", 70)
	after := strings.Repeat("b", 30)
	s := before + "\n\n" + after

	got := truncateRunesSmart(s, 80)
	if !strings.HasSuffix(got, truncationMarker) {
		t.Errorf("expected truncation marker, got %q", got)
	}
	if !strings.HasPrefix(got, before) {
		t.Errorf("expected content before paragraph break to be preserved")
	}
	if strings.Contains(got, "b") {
		t.Errorf("expected content after paragraph break to be removed")
	}
}

func TestTruncateRunesSmart_LineBreak(t *testing.T) {
	before := strings.Repeat("a", 70)
	after := strings.Repeat("b", 30)
	s := before + "\n" + after

	got := truncateRunesSmart(s, 80)
	if !strings.HasSuffix(got, truncationMarker) {
		t.Errorf("expected truncation marker, got %q", got)
	}
	if !strings.HasPrefix(got, before) {
		t.Errorf("expected content before line break to be preserved")
	}
	if strings.Contains(got, "b") {
		t.Errorf("expected content after line break to be removed")
	}
}

func TestTruncateRunesSmart_HardCut(t *testing.T) {
	s := strings.Repeat("x", 200)
	got := truncateRunesSmart(s, 100)
	if !strings.HasSuffix(got, truncationMarker) {
		t.Errorf("expected truncation marker, got %q", got)
	}
	content := strings.TrimSuffix(got, truncationMarker)
	if len([]rune(content)) != 100 {
		t.Errorf("expected 100 runes of content, got %d", len([]rune(content)))
	}
}

func TestTruncateRunesSmart_UnicodeText(t *testing.T) {
	s := strings.Repeat("need", 50) + "\n\n" + strings.Repeat("tail", 50)
	got := truncateRunesSmart(s, 60)
	if !strings.HasSuffix(got, truncationMarker) {
		t.Errorf("expected truncation marker, got %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("need", 15)) {
		t.Errorf("expected content before break to be preserved")
	}
	if strings.Contains(got, "tail") {
		t.Errorf("expected content after break to be removed")
	}
}

func TestTruncateRunesSmart_BreakOutsideSearchZone(t *testing.T) {
	before := strings.Repeat("a", 30)
	middle := strings.Repeat("b", 70)
	s := before + "\n\n" + middle

	got := truncateRunesSmart(s, 100)
	if !strings.HasSuffix(got, truncationMarker) {
		t.Errorf("expected truncation marker, got %q", got)
	}
	content := strings.TrimSuffix(got, truncationMarker)
	if len([]rune(content)) != 100 {
		t.Errorf("expected 100 runes of content, got %d", len([]rune(content)))
	}
}

func TestTruncateRunesSmart_PrefersParagraphOverLine(t *testing.T) {
	part1 := strings.Repeat("a", 68)
	part2 := strings.Repeat("b", 5)
	part3 := strings.Repeat("c", 30)
	s := part1 + "\n\n" + part2 + "\n" + part3

	got := truncateRunesSmart(s, 80)
	if !strings.HasPrefix(got, part1) {
		t.Errorf("expected to cut at paragraph break, preserving part1")
	}
	if strings.Contains(got, "b") {
		t.Errorf("expected content after paragraph break to be removed")
	}
}

func TestBuildPhaseSystemPrompt_PrevOutputsTruncated(t *testing.T) {
	registry := NewWorkflowRegistry()
	tmpl := registry.Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not found")
	}

	longRequirements := strings.Repeat("requirements paragraph\n\n", 200)
	longDesign := strings.Repeat("design paragraph\n\n", 200)
	state := &WorkflowState{
		ID:           "wf-test",
		UserID:       "u1",
		Type:         WorkflowCoding,
		Intent:       StructuredIntent{Category: WorkflowCoding, Summary: "test"},
		CurrentPhase: "task_breakdown",
		PhaseIndex:   2,
		PhaseOutputs: map[string]string{
			"requirements": longRequirements,
			"tech_design":  longDesign,
		},
	}

	phase := &tmpl.Phases[2]
	prompt := BuildPhaseSystemPrompt(state, phase, registry)
	if !strings.Contains(prompt, truncationMarker) {
		t.Error("expected truncation markers in prompt for large previous outputs")
	}
	if !strings.Contains(prompt, "(summary)") {
		t.Error("expected summary labels in prompt")
	}
	if promptRunes := len([]rune(prompt)); promptRunes > 5000 {
		t.Errorf("prompt too long (%d runes), previous outputs may not be truncated", promptRunes)
	}
	if !strings.Contains(prompt, phase.Name) {
		t.Error("expected current phase name in prompt")
	}
}

func TestBuildPhaseSystemPrompt_RendersWorkflowInputAndStructuredForm(t *testing.T) {
	phase := &PhaseTemplate{
		ID:     "collect",
		Name:   "Collect",
		Prompt: "produce deliverable",
		InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{
			{Name: "topic", Label: "Topic", Type: "text"},
			{Name: "count", Label: "Count", Type: "number"},
		}},
	}
	state := &WorkflowState{
		ID:     "wf-test",
		UserID: "u1",
		Type:   WorkflowCoding,
		Intent: StructuredIntent{Category: WorkflowCoding, Summary: "summary text"},
		InputPayload: &WorkflowInputPayload{
			Text:        "source material",
			Attachments: []WorkflowInputAttachment{{FileName: "brief.pdf", Type: "document", MimeType: "application/pdf", Size: 123}},
		},
		PhaseFormData:      map[string]interface{}{"topic": "Alpha", "count": 3},
		PhaseFormSubmitted: true,
	}

	prompt := BuildPhaseSystemPrompt(state, phase, nil)
	for _, want := range []string{"## Current Phase: Collect", "source material", "brief.pdf", "User-Provided Structured Context", "**Topic**: Alpha", "**Count**: 3", "summary text"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, bad := range []string{"?s", "锛?s", "鈥", "绫诲"} {
		if strings.Contains(prompt, bad) {
			t.Fatalf("prompt contains mojibake token %q:\n%s", bad, prompt)
		}
	}
}

func TestBuildPhaseSystemPrompt_RendersEmptySubmittedOptionalForm(t *testing.T) {
	phase := &PhaseTemplate{
		ID:          "collect",
		Name:        "Collect",
		Prompt:      "produce deliverable",
		InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{Name: "note", Label: "Note", Type: "text"}}},
	}
	state := &WorkflowState{
		ID:                 "wf-test",
		UserID:             "u1",
		Type:               WorkflowCoding,
		Intent:             StructuredIntent{Category: WorkflowCoding, Summary: "summary text"},
		PhaseFormSubmitted: true,
	}
	prompt := BuildPhaseSystemPrompt(state, phase, nil)
	if !strings.Contains(prompt, "submitted the structured form without optional details") {
		t.Fatalf("prompt should explicitly represent empty optional form submission:\n%s", prompt)
	}
}

func TestBuildPhaseSystemPrompt_PlanningBoundarySeparatesDocsFromProjectWrites(t *testing.T) {
	registry := NewWorkflowRegistry()
	tmpl := registry.Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not found")
	}
	phase := tmpl.Phases[2]
	if phase.ID != PhaseCodingTaskBreakdown || phase.ToolPolicy != ToolFilterPlanning {
		t.Fatalf("expected coding task_breakdown planning phase, got id=%s policy=%s", phase.ID, phase.ToolPolicy)
	}
	state := &WorkflowState{
		Type:         WorkflowCoding,
		PhaseIndex:   2,
		CurrentPhase: PhaseCodingTaskBreakdown,
		Intent:       StructuredIntent{Category: WorkflowCoding, Summary: "build CMake app"},
		PhaseOutputs: map[string]string{
			PhaseCodingRequirements: "Need a C++ app.",
			PhaseCodingTechDesign:   "Use CMake and src/main.cpp.",
		},
	}

	prompt := BuildPhaseSystemPrompt(state, &phase, registry)
	for _, want := range []string{
		"## Planning Tool Boundary",
		"workflow system saves this phase deliverable",
		"Do not create, edit, move, or delete project files",
		"CMake files",
		"implementation phase",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("planning prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPhaseSystemPrompt_CodingImplementationRequiresSubAgentHandoff(t *testing.T) {
	registry := NewWorkflowRegistry()
	tmpl := registry.Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not found")
	}
	phase := mustPhase(t, tmpl, PhaseCodingImplementation)
	state := &WorkflowState{
		Type:         WorkflowCoding,
		PhaseIndex:   3,
		CurrentPhase: PhaseCodingImplementation,
		Intent:       StructuredIntent{Category: WorkflowCoding, Summary: "implement approved tasks"},
		PhaseOutputs: map[string]string{
			PhaseCodingRequirements:  "Need a CLI app.",
			PhaseCodingTechDesign:    "Use Go packages.",
			PhaseCodingTaskBreakdown: "### T1: create CLI\n### T2: add tests",
		},
	}

	prompt := BuildPhaseSystemPrompt(state, &phase, registry)
	for _, want := range []string{
		"## Coding Implementation Handoff Contract",
		"delegate_task(agent=\"coding_workflow\"",
		"CodingSubAgent",
		"approved task IDs",
		"Do not call local project mutation tools",
		"`bash`",
		"`write_file`",
		"If `delegate_task` is unavailable",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("implementation prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPhaseSystemPrompt_CodingHandoffOnlyForImplementation(t *testing.T) {
	registry := NewWorkflowRegistry()
	tmpl := registry.Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not found")
	}
	phase := mustPhase(t, tmpl, PhaseCodingTaskBreakdown)
	state := &WorkflowState{
		Type:         WorkflowCoding,
		PhaseIndex:   2,
		CurrentPhase: PhaseCodingTaskBreakdown,
		Intent:       StructuredIntent{Category: WorkflowCoding, Summary: "plan tasks"},
		PhaseOutputs: map[string]string{
			PhaseCodingRequirements: "Need a CLI app.",
			PhaseCodingTechDesign:   "Use Go packages.",
		},
	}

	prompt := BuildPhaseSystemPrompt(state, &phase, registry)
	if strings.Contains(prompt, "Coding Implementation Handoff Contract") {
		t.Fatalf("task breakdown prompt must not receive implementation handoff contract:\n%s", prompt)
	}
	if strings.Contains(prompt, "delegate_task(agent=\"coding_workflow\"") {
		t.Fatalf("task breakdown prompt must not instruct subagent delegation:\n%s", prompt)
	}
}

func TestBuildPhaseSystemPrompt_CodingHandoffRequiresCurrentImplementationState(t *testing.T) {
	registry := NewWorkflowRegistry()
	tmpl := registry.Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not found")
	}
	phase := mustPhase(t, tmpl, PhaseCodingImplementation)
	state := &WorkflowState{
		Type:         WorkflowCoding,
		PhaseIndex:   2,
		CurrentPhase: PhaseCodingTaskBreakdown,
		Intent:       StructuredIntent{Category: WorkflowCoding, Summary: "still planning"},
	}

	prompt := BuildPhaseSystemPrompt(state, &phase, registry)
	if strings.Contains(prompt, "Coding Implementation Handoff Contract") {
		t.Fatalf("mismatched workflow state must not receive implementation handoff contract:\n%s", prompt)
	}
}
