package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticCodingSurfaceAdapters is the exact managed surface a coding turn is
// entitled to. Writing it out as a set rather than a count is what makes the
// withholding assertions below mean something: a test that only counted four
// tools would pass if shell quietly replaced repo inspection.
func semanticCodingSurfaceAdapters() []string {
	return []string{
		semanticTrustedBuildVerifyAdapter,
		semanticTrustedFileReadAdapter,
		semanticTrustedFileWriteAdapter,
		semanticTrustedRepoInspectAdapter,
		semanticTrustedKnowledgeReadAdapter,
		semanticTrustedMemoryRecallAdapter,
	}
}

func semanticCodingHandler(t *testing.T, label intent.IntentLabel) *IMMessageHandler {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, label)}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	return h
}

// semanticCodingSurfaceAdapterNames resolves the surface back to adapters. The
// names rendered to the model are opaque per-grant tokens, so reading the
// definitions alone would compare noise; the grant carries the adapter the
// token was issued for.
func semanticCodingSurfaceAdapterNames(t *testing.T, surface *semanticCallSurface, defs []map[string]interface{}) []string {
	t.Helper()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		rendered := extractToolName(def)
		grant, ok := surface.grants[rendered]
		if !ok {
			t.Fatalf("rendered tool %q has no grant on the surface", rendered)
		}
		names = append(names, grant.AdapterName)
	}
	sort.Strings(names)
	return names
}

func semanticCodingInvocationName(t *testing.T, surface *semanticCallSurface, defs []map[string]interface{}, adapter string) string {
	t.Helper()
	for _, def := range defs {
		rendered := extractToolName(def)
		if grant, ok := surface.grants[rendered]; ok && grant.AdapterName == adapter {
			return rendered
		}
	}
	t.Fatalf("%s is not on the coding surface", adapter)
	return ""
}

// TestSemanticCodingTurnPlansTheManagedSurfaceInsteadOfRejecting is the point of
// the slice. A coding label had no capability rule, and an unmapped capability
// label HostRejects before the managed gate, so "改一下这个函数" on the shared
// loop was refused outright rather than served. This asserts the turn now plans.
//
// The coding subagent is a different surface reached through different entry
// points; nothing here changes it.
func TestSemanticCodingTurnPlansTheManagedSurfaceInsteadOfRejecting(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "改一下这个函数", "desktop", "root-coding", "turn-coding",
		&intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("coding turn must plan, handled=%v err=%v", handled, err)
	}
	if len(surface.plan.Unmet) != 0 {
		t.Fatalf("coding plan has unmet needs: %#v", surface.plan.Unmet)
	}
	got := semanticCodingSurfaceAdapterNames(t, surface, defs)
	want := semanticCodingSurfaceAdapters()
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("coding surface = %v, want %v", got, want)
	}
}

// TestSemanticSubFloorCodingPrimaryPlansInsteadOfRejecting is the 2026-08-20
// live refusal: UIC said coding 0.72 after the tree. That is tree-confirmed,
// not a coding-family exception to 0.78.
func TestSemanticSubFloorCodingPrimaryPlansInsteadOfRejecting(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "改为豪华版hello world", "desktop", "root-subfloor-coding", "turn-subfloor-coding",
		&intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.72, Layer: 3},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("sub-floor coding primary must plan, handled=%v err=%v", handled, err)
	}
	if len(surface.plan.Unmet) != 0 {
		t.Fatalf("sub-floor coding plan has unmet needs: %#v", surface.plan.Unmet)
	}
	got := semanticCodingSurfaceAdapterNames(t, surface, defs)
	want := semanticCodingSurfaceAdapters()
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sub-floor coding surface = %v, want %v", got, want)
	}
}

// TestSemanticCodingSurfaceWithholdsShellAndRepoMutate pins the two capabilities
// the rule deliberately leaves out, because both are ways for a coding grant to
// become something larger than a code change.
//
// shell.execute.local would carry arbitrary local execution, and with it the
// file and repository mutation that the decomposition exists to separate.
// repo.mutate.vcs would let a request to change code also commit and push it.
//
// The assertion above is an exact set rather than a blocklist, which already
// excludes both. This test states the two by name anyway, at the capability
// level, because these are the ones a future widening would most plausibly
// reintroduce and a diff should have to argue with a test that says so.
func TestSemanticCodingSurfaceWithholdsShellAndRepoMutate(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "改一下这个函数", "desktop", "root-coding-withhold", "turn-coding-withhold",
		&intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("coding turn must plan, handled=%v err=%v", handled, err)
	}
	for _, adapter := range semanticCodingSurfaceAdapterNames(t, surface, defs) {
		if adapter == semanticTrustedShellAdapter || adapter == semanticTrustedRepoMutateAdapter {
			t.Fatalf("coding surface granted %q", adapter)
		}
	}
	for _, selection := range surface.plan.Selections {
		switch selection.FitProof.MatchedCapability {
		case tool.CapabilityShellExecuteLocal, tool.CapabilityRepoMutateVCS:
			t.Fatalf("coding plan selected %s", selection.FitProof.MatchedCapability)
		}
	}
}

// TestSemanticCodingFamilyLabelsShareOneSurface guards the reason the three
// labels read one shared rule variable. They describe the same outcome, so a
// capability added for coding but missed for bug_fix would be a behavior split
// that no single-label test would ever show.
func TestSemanticCodingFamilyLabelsShareOneSurface(t *testing.T) {
	want := semanticCodingSurfaceAdapters()
	sort.Strings(want)
	for _, label := range []intent.IntentLabel{intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance} {
		t.Run(string(label), func(t *testing.T) {
			h := semanticCodingHandler(t, label)
			defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
				"user-1", "处理一下这个问题", "desktop", "root-"+string(label), "turn-"+string(label),
				&intent.ClassificationResult{Primary: label, Confidence: .98},
			)
			if err != nil || !handled || surface == nil {
				t.Fatalf("%s must plan, handled=%v err=%v", label, handled, err)
			}
			got := semanticCodingSurfaceAdapterNames(t, surface, defs)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("%s surface = %v, want %v", label, got, want)
			}
		})
	}
}

// TestSemanticCodingTurnExecutesThroughTheGrant is the execution half. Planning
// a surface proves the route exists; it does not prove the selections can run.
// Each of these four adapters has its own unit tests, so what this covers is the
// coding route specifically: a turn classified as coding reaches them.
func TestSemanticCodingTurnExecutesThroughTheGrant(t *testing.T) {
	cases := []struct {
		adapter   string
		arguments string
		want      string
		install   func(h *IMMessageHandler, reached *string)
	}{
		{
			adapter: semanticTrustedFileReadAdapter, arguments: `{"path":"main.go"}`, want: "package main",
			install: func(h *IMMessageHandler, reached *string) {
				h.semanticTrustedFileRead = func(_, path, _, _ string) (string, error) {
					*reached = path
					return "package main", nil
				}
			},
		},
		{
			adapter: semanticTrustedFileWriteAdapter, arguments: `{"path":"main.go","content":"package main"}`, want: "written",
			install: func(h *IMMessageHandler, reached *string) {
				h.semanticTrustedFileWrite = func(_, path, _, _ string) (string, error) {
					*reached = path
					return "written", nil
				}
			},
		},
		{
			adapter: semanticTrustedRepoInspectAdapter, arguments: `{}`, want: "clean tree",
			install: func(h *IMMessageHandler, reached *string) {
				h.semanticTrustedRepoInspect = func(string) (string, error) {
					*reached = "inspect"
					return "clean tree", nil
				}
			},
		},
		{
			adapter: semanticTrustedBuildVerifyAdapter, arguments: `{"task":"test"}`, want: "ok",
			install: func(h *IMMessageHandler, reached *string) {
				h.semanticTrustedBuildVerify = func(_, task, _ string) (string, error) {
					*reached = task
					return "ok", nil
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.adapter, func(t *testing.T) {
			reached := ""
			h := semanticCodingHandler(t, intent.LabelCoding)
			tc.install(h, &reached)
			defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
				"user-1", "改一下这个函数", "desktop", "root-exec-"+tc.adapter, "turn-exec-"+tc.adapter,
				&intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: .98},
			)
			if err != nil || !handled || surface == nil {
				t.Fatalf("coding turn must plan, handled=%v err=%v", handled, err)
			}
			invocation := semanticCodingInvocationName(t, surface, defs, tc.adapter)
			cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}
			if got := cb.ExecuteTool(invocation, tc.arguments); !strings.Contains(got, tc.want) {
				t.Fatalf("%s result=%q, want it to contain %q", tc.adapter, got, tc.want)
			}
			if reached == "" {
				t.Fatalf("%s selection never reached its host adapter", tc.adapter)
			}
		})
	}
}
