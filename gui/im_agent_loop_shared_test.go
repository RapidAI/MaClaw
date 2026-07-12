package main

import (
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// shouldUseSharedAgentLoopLive mirrors shouldUseSharedAgentLoop but uses the
// production mode resolver so unit tests are not blocked by the testing.Testing()
// gate (which keeps package RunAgentLoop tests on the legacy path).
func shouldUseSharedAgentLoopLive(h *IMMessageHandler, ctx *LoopContext, userID string, attachments []MessageAttachment) bool {
	mode := resolveSharedAgentLoopModeLive(h)
	eligible, reason := h.sharedAgentLoopEligibility(ctx, attachments)
	switch mode {
	case sharedAgentLoopOff:
		return false
	case sharedAgentLoopShadow:
		if eligible {
			recordSharedLoopSkip("shadow", reason)
		}
		return false
	case sharedAgentLoopOn:
		if !eligible {
			recordSharedLoopSkip("ineligible", reason)
			return false
		}
		if !sharedLoopCanaryAllowsFor(h, userID) {
			recordSharedLoopSkip("canary", "canary")
			return false
		}
		return true
	default:
		return false
	}
}

func TestShouldUseSharedAgentLoop_RequiresFlag(t *testing.T) {
	// Package tests force legacy via resolveSharedAgentLoopMode.
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	if h.shouldUseSharedAgentLoop(ctx, "u1", nil) {
		t.Fatal("package tests must keep shared loop off by default")
	}
	// Production defaults still enable for new installs when env is unset.
	_ = os.Unsetenv("MACLAW_SHARED_AGENT_LOOP")
	_ = os.Unsetenv("MACLAW_SHARED_AGENT_LOOP_SHADOW")
	if corelib.AppConfigDefaults().SharedAgentLoopEnabled {
		if resolveSharedAgentLoopModeLive(h) != sharedAgentLoopOn {
			t.Fatal("expected production default on when no env/app config")
		}
	}
}

func TestShouldUseSharedAgentLoop_EnvEnable(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	if !shouldUseSharedAgentLoopLive(h, ctx, "u1", nil) {
		t.Fatal("expected true with env flag")
	}
}

func TestShouldUseSharedAgentLoop_Phase3AllowsBackground(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	bg := &LoopContext{Kind: LoopKindBackground}
	if !shouldUseSharedAgentLoopLive(h, bg, "u1", nil) {
		t.Fatal("background should use shared loop when enabled")
	}
	ok, reason := h.sharedAgentLoopEligibility(bg, nil)
	if !ok || reason != "background" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldUseSharedAgentLoop_Phase2AllowsLightAttachments(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	chat := &LoopContext{Kind: LoopKindChat}
	if !shouldUseSharedAgentLoopLive(h, chat, "u1", []MessageAttachment{{Type: "image", FileName: "a.png", MimeType: "image/png", Size: 1024}}) {
		t.Fatal("light image attachments should be allowed")
	}
}

func TestShouldUseSharedAgentLoop_RejectsWorkflowByDefault(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "")
	h := &IMMessageHandler{}
	wf := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true}
	if shouldUseSharedAgentLoopLive(h, wf, "u1", nil) {
		t.Fatal("workflow must not use shared loop without pilot env")
	}
	doc := &LoopContext{Kind: LoopKindChat, WorkflowDocPhase: true}
	if shouldUseSharedAgentLoopLive(h, doc, "u1", nil) {
		t.Fatal("workflow doc phase must never use shared loop")
	}
}

func TestShouldUseSharedAgentLoop_WorkflowPilot(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "1")
	h := &IMMessageHandler{}
	wf := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true}
	if !shouldUseSharedAgentLoopLive(h, wf, "u1", nil) {
		t.Fatal("workflow pilot should allow non-doc workflow")
	}
	// Doc phase still blocked.
	doc := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true, WorkflowDocPhase: true}
	if shouldUseSharedAgentLoopLive(h, doc, "u1", nil) {
		t.Fatal("doc phase must stay legacy even with pilot")
	}
}

func TestShouldUseSharedAgentLoop_ShadowNeverDiverts(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "shadow")
	h := &IMMessageHandler{}
	chat := &LoopContext{Kind: LoopKindChat}
	if shouldUseSharedAgentLoopLive(h, chat, "u1", nil) {
		t.Fatal("shadow mode must keep legacy path")
	}
	if resolveSharedAgentLoopModeLive(h) != sharedAgentLoopShadow {
		t.Fatal("mode should be shadow")
	}
	ok, _ := h.sharedAgentLoopEligibility(chat, nil)
	if !ok {
		t.Fatal("chat should be eligible even in shadow")
	}
}

func TestShouldUseSharedAgentLoop_CanaryPercentZero(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "0")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	before := processSharedLoopStats.skipCanary.Load()
	if shouldUseSharedAgentLoopLive(h, ctx, "any-user", nil) {
		t.Fatal("percent=0 must never divert")
	}
	if processSharedLoopStats.skipCanary.Load() <= before {
		t.Fatal("expected canary skip counter")
	}
}

func TestShouldUseSharedAgentLoop_IneligibleRecordsSkip(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "100")
	h := &IMMessageHandler{}
	// Workflow doc phase is never eligible.
	ctx := &LoopContext{Kind: LoopKindChat, WorkflowDocPhase: true}
	before := processSharedLoopStats.skipIneligible.Load()
	if shouldUseSharedAgentLoopLive(h, ctx, "u1", nil) {
		t.Fatal("doc phase must not use shared")
	}
	if processSharedLoopStats.skipIneligible.Load() <= before {
		t.Fatal("expected ineligible skip counter")
	}
	st := (&App{}).GetSharedAgentLoopStatus()
	if !strings.Contains(st.LastSkipReason, "workflow doc") && !strings.Contains(st.LastSkipReason, "ineligible") {
		// last may be ineligible:workflow doc phase
		if st.LastSkipReason == "" {
			t.Fatalf("last skip empty")
		}
	}
}

func TestShouldUseSharedAgentLoop_CanaryPercentSticky(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "50")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	// Stickiness: same user always same decision.
	a1 := shouldUseSharedAgentLoopLive(h, ctx, "sticky-user-xyz", nil)
	a2 := shouldUseSharedAgentLoopLive(h, ctx, "sticky-user-xyz", nil)
	if a1 != a2 {
		t.Fatal("canary must be sticky per user")
	}
	// Across many users some should pass and some fail at 50%.
	pass, fail := 0, 0
	for i := 0; i < 200; i++ {
		uid := "user-" + strings.Repeat("x", i%17) + string(rune('a'+i%26)) + string(rune('0'+i%10))
		if sharedLoopCanaryAllows(uid) {
			pass++
		} else {
			fail++
		}
	}
	if pass == 0 || fail == 0 {
		t.Fatalf("expected mix at 50%% canary, pass=%d fail=%d", pass, fail)
	}
}

func TestSharedLoopPercent_Bounds(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	if sharedLoopPercent() != 100 {
		t.Fatal("default 100")
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "30")
	if sharedLoopPercent() != 30 {
		t.Fatal("30")
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "999")
	if sharedLoopPercent() != 100 {
		t.Fatal("cap 100")
	}
}

func TestSharedAgentLoopEnabled_EnvOff(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "0")
	if resolveSharedAgentLoopModeLive(&IMMessageHandler{}) != sharedAgentLoopOff {
		t.Fatal("env 0 should disable")
	}
	if sharedAgentLoopEnabled(&IMMessageHandler{}) {
		t.Fatal("package shouldUse path must stay off under tests")
	}
}

func TestAppConfigDefaults_SharedAgentLoopEnabled(t *testing.T) {
	if !corelib.AppConfigDefaults().SharedAgentLoopEnabled {
		t.Fatal("new installs should default SharedAgentLoopEnabled=true")
	}
}

func TestSharedAgentLoopCallbacks_RouteTurn(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		llmCfg: corelib.MaclawLLMConfig{Model: "m1", ProviderName: "p1"},
		route:  modelRouteDecision{Task: "fast", Source: "aux", Model: "m1", Provider: "p1", Reason: "short"},
	}
	cfg, d, ok := cb.RouteTurn("hi")
	if !ok || cfg.Model != "m1" || d.Source != "aux" || !strings.Contains(d.Reason, "shared") {
		t.Fatalf("cfg=%+v d=%+v ok=%v", cfg, d, ok)
	}
}
