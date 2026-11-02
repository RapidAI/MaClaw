package guiapp

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestCodingIterBudgetHardLimitUsesLoopMaxIterations(t *testing.T) {
	ctx := NewLoopContext("chat", 300, nil)
	if got := codingIterBudgetHardLimit(ctx); got != 300 {
		t.Fatalf("codingIterBudgetHardLimit() = %d, want 300", got)
	}
}

func TestCodingIterBudgetHardLimitRespectsLoopOverride(t *testing.T) {
	ctx := NewLoopContext("chat", 200, nil)
	if got := codingIterBudgetHardLimit(ctx); got != 200 {
		t.Fatalf("codingIterBudgetHardLimit() = %d, want 200", got)
	}
}

func TestCodingIterBudgetHardLimitDoesNotExceedSystemCap(t *testing.T) {
	ctx := NewLoopContext("chat", config.MaxAgentIterationsCap*2, nil)
	if got := codingIterBudgetHardLimit(ctx); got != config.MaxAgentIterationsCap {
		t.Fatalf("codingIterBudgetHardLimit() = %d, want %d", got, config.MaxAgentIterationsCap)
	}
}

func TestCodingIterBudgetHardLimitUsesEffectiveMinimum(t *testing.T) {
	ctx := NewLoopContext("chat", config.MinAgentIterations-1, nil)
	if got := codingIterBudgetHardLimit(ctx); got != config.MinAgentIterations {
		t.Fatalf("codingIterBudgetHardLimit() = %d, want %d", got, config.MinAgentIterations)
	}
}

func TestReusedLoopDropsPriorIterationExtension(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("chat", 30, nil)
	ctx.iterationExtensions = sessionRoundContinuationMaxWaves
	ctx.SetMaxIterations(900)
	ctx.codeSessionID = "old-coding"
	got := h.prepareIMLoopContext(ctx, IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "继续"}, nil, false, false)
	if got != ctx {
		t.Fatal("prepare replaced the reused loop")
	}
	if ctx.iterationExtensions != 0 || ctx.codeSessionID != "" || ctx.MaxIterations() != 30 {
		t.Fatalf("extensions=%d session=%q max=%d", ctx.iterationExtensions, ctx.codeSessionID, ctx.MaxIterations())
	}

	live := IMUserMessage{RequestID: "req-keep", UserID: "desktop-user", Platform: "desktop", Text: "做一个游戏"}
	kept := h.prepareIMLoopContext(ctx, live, nil, false, false)
	kept.iterationExtensions = 4
	kept.SetMaxIterations(900)
	kept.codeSessionID = "live-coding"
	again := h.prepareIMLoopContext(kept, live, nil, false, false)
	if again.iterationExtensions != 4 || again.codeSessionID != "live-coding" || again.MaxIterations() != 900 {
		t.Fatalf("retry cleared the live turn extensions=%d session=%q max=%d", again.iterationExtensions, again.codeSessionID, again.MaxIterations())
	}

	chat := NewLoopContext("chat-extend", 30, nil)
	chat.Kind = LoopKindChat
	if got := h.refreshAgentLoopEffectiveMax(chat, 28, 30, 0, nil, nil); got != 60 || chat.MaxIterations() != 60 {
		t.Fatalf("auto-extend = %d stored %d, want 60", got, chat.MaxIterations())
	}
	next := h.prepareIMLoopContext(chat, IMUserMessage{RequestID: "after-extend", UserID: "desktop-user", Platform: "desktop", Text: "下一句"}, nil, false, false)
	if next.MaxIterations() != 30 || next.iterationCeilingRaised {
		t.Fatalf("auto-extend leaked max=%d raised=%v", next.MaxIterations(), next.iterationCeilingRaised)
	}
}

func TestCodingBackgroundDoesNotPauseBeforeRefill(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewBackgroundLoopContext("bg-coding", SlotKindCoding, "coding", config.MaxAgentIterationsCap, nil, nil)
	ctx.codeSessionID = "coding-session"
	started := time.Now()
	result := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      ctx,
		UserID:       "bg-coding",
		Iteration:    config.MaxAgentIterationsCap - 2,
		EffectiveMax: config.MaxAgentIterationsCap,
		ConfigMax:    config.MaxAgentIterationsCap,
		ToolCalls:    4,
	})
	if time.Since(started) > 2*time.Second {
		t.Fatal("unfinished coding waited for a person to grant more rounds")
	}
	if result.Stop || result.Response != nil {
		t.Fatalf("stop=%v response=%#v", result.Stop, result.Response)
	}
	if ctx.State() == string(LoopStatePaused) || ctx.State() == string(LoopStateTimeout) {
		t.Fatalf("coding background state = %s", ctx.State())
	}
}

func TestSameLoopRefillsIterationCeiling(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("legacy-refill", config.MaxAgentIterationsCap, nil)
	ctx.Kind = LoopKindBackground
	result := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      ctx,
		UserID:       "legacy-refill",
		Iteration:    config.MaxAgentIterationsCap,
		EffectiveMax: config.MaxAgentIterationsCap,
		ConfigMax:    config.MaxAgentIterationsCap,
		ToolCalls:    4,
	})
	if result.Stop || result.Response != nil {
		t.Fatalf("stop=%v response=%#v, want the same loop to continue", result.Stop, result.Response)
	}
	if result.EffectiveMax != config.MaxAgentIterationsCap*2 {
		t.Fatalf("effective max = %d, want another %d iterations", result.EffectiveMax, config.MaxAgentIterationsCap)
	}

	ctx.iterationExtensions = sessionRoundContinuationMaxWaves
	stopped := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      ctx,
		UserID:       "legacy-refill",
		Iteration:    result.EffectiveMax,
		EffectiveMax: result.EffectiveMax,
		ConfigMax:    config.MaxAgentIterationsCap,
		ToolCalls:    4,
	})
	if !stopped.Stop {
		t.Fatal("the refill cap was ignored")
	}

	coding := NewLoopContext("coding-open", config.MaxAgentIterationsCap, nil)
	coding.Kind = LoopKindBackground
	coding.codeSessionID = "coding-session"
	coding.iterationExtensions = sessionRoundContinuationMaxWaves
	codingOpen := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      coding,
		UserID:       "coding-open",
		Iteration:    config.MaxAgentIterationsCap,
		EffectiveMax: config.MaxAgentIterationsCap,
		ConfigMax:    config.MaxAgentIterationsCap,
		ToolCalls:    4,
	})
	if codingOpen.Stop || codingOpen.EffectiveMax != config.MaxAgentIterationsCap*2 {
		t.Fatalf("unfinished coding task stop=%v effective=%d", codingOpen.Stop, codingOpen.EffectiveMax)
	}

	shared := &sharedAgentLoopCallbacks{loopCtx: NewLoopContext("chat", 300, nil), maxIter: 300}
	if got := shared.ExtendIterationBudget(300, 0); got != 0 {
		t.Fatalf("a turn with no tool calls was refilled by %d", got)
	}
	if got := shared.ExtendIterationBudget(300, 6); got != 300 {
		t.Fatalf("shared refill = %d, want 300", got)
	}
	shared.loopCtx.iterationExtensions = sessionRoundContinuationMaxWaves
	if got := shared.ExtendIterationBudget(300, 6); got != 0 {
		t.Fatalf("chat refill continued past the wave cap: %d", got)
	}
	shared.loopCtx.codeSessionID = "coding-session"
	if got := shared.ExtendIterationBudget(300, 6); got != 300 {
		t.Fatalf("unfinished coding refill = %d, want 300", got)
	}

	raised := &sharedAgentLoopCallbacks{loopCtx: NewLoopContext("raise", 80, nil), maxIter: 80, loopMaxAtStart: 80}
	raised.loopCtx.SetMaxIterations(200)
	if got := raised.GetMaxIterations(); got != 200 {
		t.Fatalf("shared budget ignored the raised limit: %d", got)
	}
	raised.loopCtx.SetMaxIterations(30)
	if got := raised.ExtendIterationBudget(200, 4); got != 200 {
		t.Fatalf("shared refill followed the restored limit: %d", got)
	}
	foregroundShared := &sharedAgentLoopCallbacks{loopCtx: NewLoopContext("fg", 300, nil), maxIter: 8, loopMaxAtStart: 300}
	if got := foregroundShared.GetMaxIterations(); got != config.EffectiveMaxIterations(8) {
		t.Fatalf("foreground wave = %d, want the clamped start %d", got, config.EffectiveMaxIterations(8))
	}

	lowered := NewLoopContext("lowered", config.MaxAgentIterationsCap, nil)
	lowered.Kind = LoopKindBackground
	lowered.raiseIterationCeiling(40)
	passed := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      lowered,
		UserID:       "lowered",
		Iteration:    100,
		EffectiveMax: config.MaxAgentIterationsCap,
		ConfigMax:    config.MaxAgentIterationsCap,
		ToolCalls:    4,
	})
	if !passed.Stop || lowered.iterationExtensions != 0 {
		t.Fatalf("work past the explicit ceiling stop=%v extensions=%d", passed.Stop, lowered.iterationExtensions)
	}
	codingPast := NewLoopContext("coding-past", config.MaxAgentIterationsCap, nil)
	codingPast.Kind = LoopKindBackground
	codingPast.codeSessionID = "coding-session"
	codingPast.raiseIterationCeiling(40)
	keptCoding := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      codingPast,
		UserID:       "coding-past",
		Iteration:    100,
		EffectiveMax: config.MaxAgentIterationsCap,
		ConfigMax:    config.MaxAgentIterationsCap,
		ToolCalls:    4,
	})
	if keptCoding.Stop || keptCoding.EffectiveMax != 100+config.MaxAgentIterationsCap || codingPast.iterationExtensions != 1 {
		t.Fatalf("coding past the ceiling stop=%v effective=%d extensions=%d", keptCoding.Stop, keptCoding.EffectiveMax, codingPast.iterationExtensions)
	}
	exact := NewLoopContext("exact-ceiling", 40, nil)
	exact.Kind = LoopKindBackground
	exact.raiseIterationCeiling(40)
	landed := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      exact,
		UserID:       "exact-ceiling",
		Iteration:    40,
		EffectiveMax: 40,
		ConfigMax:    config.MaxAgentIterationsCap,
		ToolCalls:    4,
	})
	if landed.Stop || landed.EffectiveMax != 40+config.MaxAgentIterationsCap {
		t.Fatalf("landing on the ceiling stop=%v effective=%d", landed.Stop, landed.EffectiveMax)
	}
	sharedCeiling := &sharedAgentLoopCallbacks{loopCtx: NewLoopContext("shared-ceiling", 300, nil), maxIter: 300, loopMaxAtStart: 300}
	sharedCeiling.loopCtx.raiseIterationCeiling(40)
	if n, ok := sharedCeiling.TurnIterationCeiling(100); !ok || n != 40 {
		t.Fatalf("shared ceiling = %d ok=%v", n, ok)
	}
	if _, ok := sharedCeiling.TurnIterationCeiling(100); ok {
		t.Fatal("the same explicit ceiling was applied twice")
	}
	codingShared := &sharedAgentLoopCallbacks{loopCtx: NewLoopContext("coding-shared", 300, nil), maxIter: 300, loopMaxAtStart: 300}
	codingShared.loopCtx.codeSessionID = "coding-session"
	codingShared.loopCtx.raiseIterationCeiling(40)
	if _, ok := codingShared.TurnIterationCeiling(100); ok {
		t.Fatal("coding work already past the ceiling was stopped")
	}

	foreground := NewLoopContext("foreground-refill", 8, nil)
	foreground.Kind = LoopKindChat
	foreground.ForegroundChatBudget = 8
	foreground.iterationExtensions = 1
	foreground.SetMaxIterations(308)
	if got := h.refreshAgentLoopEffectiveMax(foreground, 8, 8, 0, nil, nil); got != 308 {
		t.Fatalf("foreground refresh = %d, want the raised ceiling kept", got)
	}
	if got := h.refreshAgentLoopEffectiveMax(foreground, 8, 400, 0, nil, nil); got < 308 || foreground.MaxIterations() != 308 {
		t.Fatalf("higher running max = %d stored = %d, want the raised ceiling kept", got, foreground.MaxIterations())
	}

	backgroundGrace := NewLoopContext("legacy-grace", config.MaxAgentIterationsCap, nil)
	backgroundGrace.Kind = LoopKindBackground
	grace := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           backgroundGrace,
		UserID:            "legacy-grace",
		Iteration:         config.MaxAgentIterationsCap,
		EffectiveMax:      config.MaxAgentIterationsCap,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         4,
	})
	if grace.Stop || grace.Response != nil || grace.EffectiveMax != config.MaxAgentIterationsCap*2 {
		t.Fatalf("grace refill stop=%v response=%#v effective=%d", grace.Stop, grace.Response, grace.EffectiveMax)
	}

	chatGrace := NewLoopContext("chat-grace", 8, nil)
	chatGrace.Kind = LoopKindChat
	chatGrace.ForegroundChatBudget = 8
	chatRound := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           chatGrace,
		UserID:            "chat-grace",
		Iteration:         8,
		EffectiveMax:      8,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         4,
	})
	if chatRound.Stop || chatRound.EffectiveMax != 8+config.MaxAgentIterationsCap {
		t.Fatalf("chat refill stop=%v effective=%d", chatRound.Stop, chatRound.EffectiveMax)
	}
	for _, msg := range chatRound.Conversation {
		if strings.Contains(fmt.Sprint(msg), "Finalization requirement") {
			t.Fatal("the wrap-up prompt ran before the same loop refilled")
		}
	}
	chatGrace.iterationExtensions = sessionRoundContinuationMaxWaves
	held := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           chatGrace,
		UserID:            "chat-grace",
		Iteration:         chatRound.EffectiveMax,
		EffectiveMax:      chatRound.EffectiveMax,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         4,
		SendProgress:      func(string) {},
	})
	if held.Stop {
		t.Fatal("the last wave skipped its grace rounds")
	}
	exhausted := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           chatGrace,
		UserID:            "chat-grace",
		Iteration:         chatRound.EffectiveMax + 2,
		EffectiveMax:      chatRound.EffectiveMax,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         4,
	})
	if !exhausted.Stop {
		t.Fatal("grace rounds continued after the refill cap")
	}
}

func TestCodingIterBudgetHardLimitFallsBackToConfigCap(t *testing.T) {
	if got := codingIterBudgetHardLimit(nil); got != config.MaxAgentIterationsCap {
		t.Fatalf("codingIterBudgetHardLimit(nil) = %d, want %d", got, config.MaxAgentIterationsCap)
	}
}

func TestEnforceAgentLoopCodingBudgetDoesNotStopAtLegacySixtyFive(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("chat", 300, nil)
	toolCalls := []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "write_file"}}}

	result := h.enforceAgentLoopCodingBudget(
		ctx,
		"desktop-user",
		65,
		64,
		toolCalls,
		nil,
		nil,
		nil,
		"",
		"",
		"",
		func(int, []interface{}) {},
		func(*IMAgentResponse) {},
		func(*IMAgentResponse) {},
	)

	if result.Count != 65 {
		t.Fatalf("Count = %d, want 65", result.Count)
	}
	if result.Response != nil {
		t.Fatalf("Response = %#v, want nil before configured max", result.Response)
	}
}

func TestEnforceAgentLoopCodingBudgetStopsAtConfiguredMax(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	ctx := NewLoopContext("chat", config.MaxAgentIterationsCap, nil)
	toolCalls := []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "write_file"}}}
	telemetryAttached := false
	artifactsAttached := false

	result := h.enforceAgentLoopCodingBudget(
		ctx,
		"desktop-user",
		config.MaxAgentIterationsCap,
		config.MaxAgentIterationsCap-1,
		toolCalls,
		nil,
		nil,
		nil,
		"",
		"",
		"",
		func(int, []interface{}) {},
		func(*IMAgentResponse) { telemetryAttached = true },
		func(*IMAgentResponse) { artifactsAttached = true },
	)

	if result.Count != config.MaxAgentIterationsCap {
		t.Fatalf("Count = %d, want the coding count to stay in this loop", result.Count)
	}
	if result.Response != nil {
		t.Fatalf("Response = %#v, want the loop to keep going", result.Response)
	}
	if telemetryAttached || artifactsAttached {
		t.Fatalf("callbacks telemetry=%v artifacts=%v, want the turn to stay open", telemetryAttached, artifactsAttached)
	}
}
