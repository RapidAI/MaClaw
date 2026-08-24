package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The trusted SSH, browser, computer-use and delegate adapters all report an
// indeterminate outcome as "[system unknown] ...". The loop boundary used to
// classify only "[system rejected]" and "error:", so every one of those texts
// arrived as a successful tool execution. That is the same unknown-as-success
// collapse that was fixed inside the selection executor, one layer further out.
func TestIndeterminateToolTextIsNotReportedToTheLoopAsSuccess(t *testing.T) {
	for _, text := range []string{
		"[system unknown] host_ssh_timeout",
		"[system unknown] host_call_unknown",
		"[system unknown] trusted_delegate_child_receipt_missing",
	} {
		t.Run(text, func(t *testing.T) {
			result := semanticAgentToolExecutionResult(text)
			if result.Outcome == agent.ToolExecutionOutcomeOK {
				t.Fatalf("indeterminate outcome reported as ok: %#v", result)
			}
			// The uncertainty must survive into the text the model reads;
			// collapsing it to a bare failure string would hide that the
			// effect may already hold.
			if !strings.Contains(result.Result, "[system unknown]") {
				t.Fatalf("unknown marker was dropped from %q", result.Result)
			}
		})
	}
}

// A host call journalled as unknown is the strongest "this may already have
// happened" evidence the host holds. Replaying it to the model as a definite
// failure is what invites a second commit, push or remote command.
func TestRecordedUnknownHostCallIsSurfacedAsUnknown(t *testing.T) {
	reached := false
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGitMutate)}
	h.semanticTrustedRepoMutate = func(string, string, string) (string, error) {
		reached = true
		return "commit 0123456789abcdef0123456789abcdef01234567", nil
	}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})

	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这些改动提交一下", "desktop", "root-unknown-replay", "turn-unknown-replay",
		&intent.ClassificationResult{Primary: intent.LabelGitMutate, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}
	if surface.coordinator != nil {
		t.Skip("this fence covers the uncoordinated journal path")
	}

	const argsJSON = `{"action":"commit","message":"save note"}`
	grant, ok := surface.grants[name]
	if !ok {
		t.Fatalf("no grant for %q", name)
	}
	selection, ok := semanticSelectionByID(surface.plan, grant.SelectionID)
	if !ok {
		t.Fatal("planned selection is missing")
	}
	canonical, err := cb.semanticCanonicalArguments(selection, argsJSON)
	if err != nil {
		t.Fatal(err)
	}
	identity := tool.HostCallIdentity{Protocol: "agent-loop/v1", ConnectionID: cb.semanticHostConnectionID(), CallID: "call-unknown"}
	fingerprint := tool.InvocationGrantFingerprint(grant)
	if _, _, err := surface.hostCalls.Acquire(identity, fingerprint, canonical.Digest, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := surface.hostCalls.MarkUnknown(identity, fingerprint, canonical.Digest, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	result := cb.ExecuteToolCall(name, argsJSON, "call-unknown")
	if !strings.Contains(result.Result, "host_call_unknown") {
		t.Fatalf("result=%#v", result)
	}
	if !semanticSelectionOutcomeUnknown(result.Result) {
		t.Fatalf("a journalled unknown was surfaced as a definite verdict: %q", result.Result)
	}
	if result.Outcome == agent.ToolExecutionOutcomeOK {
		t.Fatalf("a journalled unknown was reported to the loop as ok: %#v", result)
	}
	if reached {
		t.Fatal("an unknown record must never reach the adapter a second time")
	}
}

func TestSemanticModelCallRejectsSupersededSurfaceEpoch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-epoch", "北京天气", "desktop", "root-epoch", "turn-epoch",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	grant := surface.grants[name]
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-epoch"}
	epochA := cb.BeginToolSurfaceEpoch(0)
	epochB := cb.BeginToolSurfaceEpoch(1)
	if epochA == "" || epochA == epochB {
		t.Fatalf("epochs=%q,%q", epochA, epochB)
	}
	stale := cb.ExecuteToolCallWithContext(name, `{}`, "epoch-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if !strings.Contains(stale.Result, "stale_surface") {
		t.Fatalf("stale result=%#v", stale)
	}
	if _, err := surface.issuer.Validate(grant, surface.scope, surface.plan, surface.completed); err != nil {
		t.Fatalf("stale call consumed current grant: %v", err)
	}
	current := cb.ExecuteToolCallWithContext(name, `{}`, "epoch-call", agent.ToolCallExecutionContext{SurfaceEpoch: epochB})
	if strings.Contains(current.Result, "stale_surface") {
		t.Fatalf("current epoch was rejected: %#v", current)
	}
}
