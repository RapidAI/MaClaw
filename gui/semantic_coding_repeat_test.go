package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingRepeatSurface starts a coding turn and returns the live surface plus a
// counter of how many times the file-read adapter actually ran.
func codingRepeatSurface(t *testing.T, turn string) (*IMMessageHandler, *semanticCallSurface, []map[string]interface{}, *int) {
	t.Helper()
	reads := 0
	h := semanticCodingHandler(t, intent.LabelCoding)
	h.semanticTrustedFileRead = func(_, path, _, _ string) (string, error) {
		reads++
		return "contents of " + path, nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "改一下这个函数", "desktop", "root-"+turn, "turn-"+turn,
		&intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("coding turn did not plan: handled=%v err=%v", handled, err)
	}
	return h, surface, defs, &reads
}

// The gap this budget exists to close: before it, reading a second file was
// refused as a replayed grant, which no real code change survives.
func TestSemanticCodingTurnReadsMoreThanOneFile(t *testing.T) {
	h, surface, defs, reads := codingRepeatSurface(t, "repeat-read")
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, userID: "user-1"}
	for round := 1; round <= 5; round++ {
		name := semanticCodingInvocationName(t, surface, cb.tools, semanticTrustedFileReadAdapter)
		path := fmt.Sprintf("file%d.go", round)
		got := cb.ExecuteTool(name, fmt.Sprintf(`{"path":%q}`, path))
		if !strings.Contains(got, "contents of "+path) {
			t.Fatalf("round %d read = %q", round, got)
		}
	}
	if *reads != 5 {
		t.Fatalf("adapter ran %d times, want 5", *reads)
	}
}

func TestSemanticCodingRepeatStillIssuesUnderGenerateHold(t *testing.T) {
	h, surface, defs, reads := codingRepeatSurface(t, "repeat-hold")
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, userID: "user-1"}
	cb.semanticHoldDependantIssue = true
	name := semanticCodingInvocationName(t, surface, cb.tools, semanticTrustedFileReadAdapter)
	first := cb.ExecuteToolCall(name, `{"path":"a.go"}`, "call-read-1")
	if !strings.Contains(first.Result, "contents of a.go") || *reads != 1 {
		t.Fatalf("first read=%+v reads=%d", first, *reads)
	}
	second := cb.ExecuteToolCall(name, `{"path":"b.go"}`, "call-read-2")
	if !strings.Contains(second.Result, "contents of b.go") || *reads != 2 {
		t.Fatalf("generate hold must not block same-family repeats: second=%+v reads=%d", second, *reads)
	}
}

// After the first read is consumed, the next sibling reuses read_file. A
// provider retry of the original call ID must replay, not conflict against
// the new grant or run the adapter again.
func TestSemanticCodingRepeatReplaysFirstCallAfterSiblingIsLive(t *testing.T) {
	h, surface, defs, reads := codingRepeatSurface(t, "repeat-replay")
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, userID: "user-1", checkpointRunID: "loop-repeat-replay"}
	name := semanticCodingInvocationName(t, surface, cb.tools, semanticTrustedFileReadAdapter)
	first := cb.ExecuteToolCall(name, `{"path":"a.go"}`, "call-read-1")
	if !strings.Contains(first.Result, "contents of a.go") || *reads != 1 {
		t.Fatalf("first read=%+v reads=%d", first, *reads)
	}
	if semanticCodingInvocationName(t, surface, cb.tools, semanticTrustedFileReadAdapter) != "read_file" {
		t.Fatal("sibling was not re-exposed as read_file")
	}
	replay := cb.ExecuteToolCall(name, `{"path":"a.go"}`, "call-read-1")
	if replay.Result != first.Result || *reads != 1 {
		t.Fatalf("first call did not replay after sibling issued: replay=%+v reads=%d", replay, *reads)
	}
	if strings.Contains(replay.Result, "host_call_conflict") {
		t.Fatal("stable-name sibling shadowed the original host call")
	}
	second := cb.ExecuteToolCall(name, `{"path":"b.go"}`, "call-read-2")
	if !strings.Contains(second.Result, "contents of b.go") || *reads != 2 {
		t.Fatalf("second sibling=%+v reads=%d", second, *reads)
	}
}

// Each invocation must be its own grant. A budget that re-armed one token
// would be a replayable grant wearing a different name.
func TestSemanticCodingRepeatIssuesAFreshGrantEachTime(t *testing.T) {
	h, surface, defs, _ := codingRepeatSurface(t, "repeat-grant")
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, userID: "user-1"}
	seenTokens := map[string]bool{}
	seenSelections := map[string]bool{}
	for round := 1; round <= 4; round++ {
		name := semanticCodingInvocationName(t, surface, cb.tools, semanticTrustedFileReadAdapter)
		if name != "read_file" {
			t.Fatalf("round %d name=%q, want read_file", round, name)
		}
		grant := surface.grants[name]
		if seenTokens[grant.Token] {
			t.Fatalf("round %d reused grant token", round)
		}
		seenTokens[grant.Token] = true
		if seenSelections[grant.SelectionID] {
			t.Fatalf("round %d reused selection %q", round, grant.SelectionID)
		}
		seenSelections[grant.SelectionID] = true
		if got := cb.ExecuteTool(name, `{"path":"a.go"}`); !strings.Contains(got, "contents of") {
			t.Fatalf("round %d read = %q", round, got)
		}
	}
	if len(seenSelections) != 4 {
		t.Fatalf("distinct selections=%d, want 4", len(seenSelections))
	}
}

// The budget is a ceiling, not a suggestion: once the planned siblings are
// spent the capability leaves the surface for the rest of the turn.
func TestSemanticCodingRepeatStopsAtTheDeclaredBudget(t *testing.T) {
	budget := 0
	for _, template := range semanticCodingCapabilityRule {
		if template.Capability == tool.CapabilityRepoInspectVCS {
			budget = template.MaxInvocations
		}
	}
	if budget < 2 {
		t.Fatalf("repo inspect budget=%d, expected a repeatable need", budget)
	}
	h, surface, defs, _ := codingRepeatSurface(t, "repeat-budget")
	inspects := 0
	h.semanticTrustedRepoInspect = func(string) (string, error) {
		inspects++
		return "diff --git a/x b/x", nil
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, userID: "user-1"}
	for round := 1; round <= budget; round++ {
		name := semanticCodingInvocationName(t, surface, cb.tools, semanticTrustedRepoInspectAdapter)
		if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "diff --git") {
			t.Fatalf("round %d inspect = %q", round, got)
		}
	}
	if inspects != budget {
		t.Fatalf("adapter ran %d times, want the %d it was budgeted", inspects, budget)
	}
	for _, def := range cb.tools {
		grant, ok := surface.grants[extractToolName(def)]
		if ok && grant.AdapterName == semanticTrustedRepoInspectAdapter {
			t.Fatalf("exhausted capability stayed on the surface: %#v", grant)
		}
	}
	// The other capabilities must not be dragged down with it.
	if name := semanticCodingInvocationName(t, surface, cb.tools, semanticTrustedFileReadAdapter); name == "" {
		t.Fatal("exhausting one budget removed an unrelated capability")
	}
}

// Only one sibling may be live at a time. Handing the model its whole budget
// at once would let a single round spend it, and would show one outcome as
// several different actions.
func TestSemanticCodingRepeatExposesOneCallPerCapability(t *testing.T) {
	_, surface, defs, _ := codingRepeatSurface(t, "repeat-exposure")
	if len(defs) != 6 {
		t.Fatalf("coding surface rendered %d tools, want one per capability including ambient retrieval", len(defs))
	}
	perAdapter := map[string]int{}
	for _, def := range defs {
		perAdapter[surface.grants[extractToolName(def)].AdapterName]++
	}
	for adapter, count := range perAdapter {
		if count != 1 {
			t.Fatalf("%s was exposed %d times at once", adapter, count)
		}
	}
	// The budget lives in the plan, so the siblings must really be there.
	if len(surface.plan.Selections) <= 4 {
		t.Fatalf("plan holds %d selections; budgets are not materialized as nodes", len(surface.plan.Selections))
	}
	if live := len(surface.grants); live != 6 {
		t.Fatalf("%d grants are live at once, want 6", live)
	}
}

// Budgets introduced a way for a tool to disappear while the work is still
// unfinished. Silence there reads to the model as "nothing left to do", so the
// call that spends the last invocation has to say so.
func TestSemanticCodingRepeatAnnouncesTheSpentBudget(t *testing.T) {
	budget := 0
	for _, template := range semanticCodingCapabilityRule {
		if template.Capability == tool.CapabilityRepoInspectVCS {
			budget = template.MaxInvocations
		}
	}
	h, surface, defs, _ := codingRepeatSurface(t, "repeat-notice")
	h.semanticTrustedRepoInspect = func(string) (string, error) { return "diff --git a/x b/x", nil }
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, userID: "user-1"}
	for round := 1; round <= budget; round++ {
		name := semanticCodingInvocationName(t, surface, cb.tools, semanticTrustedRepoInspectAdapter)
		got := cb.ExecuteTool(name, `{}`)
		if !strings.Contains(got, "diff --git") {
			t.Fatalf("round %d inspect = %q", round, got)
		}
		spent := strings.Contains(got, "reached its limit")
		if spent != (round == budget) {
			t.Fatalf("round %d of %d: budget notice present=%v, result=%q", round, budget, spent, got)
		}
	}
}

// The notice belongs to budgets alone. Every family migrated before budgets
// existed ends by producing its one outcome, and announcing a "limit" there
// would describe success as a restriction.
func TestSemanticSingleInvocationFamilySaysNothingAboutLimits(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	h.semanticTrustedWebSearch = func(string, string) (string, error) {
		return "Public web results for \"go\" (1):\n\n1. Go\n   https://example.com", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "搜索 Go 并发文档", "lansenger", "root-search-notice", "turn-search-notice", webSearchClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, userID: "user-1"}
	got := cb.ExecuteTool(semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter), `{"query":"go"}`)
	if !strings.Contains(got, "Public web results") {
		t.Fatalf("search = %q", got)
	}
	if strings.Contains(got, "reached its limit") {
		t.Fatalf("single-invocation family reported a budget: %q", got)
	}
}
