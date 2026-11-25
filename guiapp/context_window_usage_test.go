package guiapp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSharedUsageDoesNotOverwriteEstimateWithMissingPromptTokens(t *testing.T) {
	var events []AIAssistantStreamEvent
	app := &App{ctx: context.Background()}
	app.testEmit = func(name string, data ...interface{}) {
		if name != "ai-assistant-context-usage" || len(data) == 0 {
			return
		}
		payload, _ := data[0].(string)
		var event AIAssistantStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Errorf("decode context event: %v", err)
			return
		}
		events = append(events, event)
	}
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "desktop-user:E:/workprj/MaClawRust",
		llmCfg:  corelib.MaclawLLMConfig{ContextLength: 400000},
	}
	cb.ReportContextWindowUsage(12345, 320000, true)
	cb.OnLLMUsage("test-model", 0, 80)
	if len(events) != 1 {
		t.Fatalf("output-only usage replaced the prompt estimate: %#v", events)
	}
	if events[0].ContextUsedTokens != 12345 || !events[0].ContextUsageEstimated {
		t.Fatalf("estimate changed after output-only usage: %#v", events[0])
	}

	cb.OnLLMUsage("test-model", 15000, 90)
	if len(events) != 2 || events[1].ContextUsedTokens != 15000 || events[1].ContextUsageEstimated {
		t.Fatalf("provider prompt usage was not published: %#v", events)
	}
}

// BR-20261125: Devin/Windsurf can report only the incremental input leg after
// a much larger outbound prompt. That partial count must not collapse the
// meter, and the task-end last-prompt contract must carry the reconciled fill.
func TestOutboundContextMeterPrefersNewerEstimateAfterCompaction(t *testing.T) {
	var meter outboundContextMeter
	meter.observeEstimate(42450, 102400)
	if fill, max, estimated := meter.providerFill(192); fill != 42450 || max != 102400 || !estimated {
		t.Fatalf("partial provider fill=%d max=%d estimated=%v", fill, max, estimated)
	}
	// The final response omitted usage after compaction. The loop result still
	// carries the previous 42450 provider prompt; the newer estimate must win.
	meter.observeEstimate(8000, 102400)
	if got := meter.preferPrompt(42450); got != 8000 {
		t.Fatalf("stale provider prompt won over the compacted estimate: %d", got)
	}
}

func TestOutboundContextMeterKeepsRequestFloorAcrossFanOut(t *testing.T) {
	var meter outboundContextMeter
	meter.observeEstimate(42450, 102400)
	if fill, _, _ := meter.providerFill(2000); fill != 42450 {
		t.Fatalf("first reference fill=%d", fill)
	}
	// A second reference call belongs to the same outbound turn and must not
	// drop below the estimate for the main request.
	if fill, _, _ := meter.providerFill(1800); fill != 42450 {
		t.Fatalf("second reference fill=%d", fill)
	}
}

func TestOutboundContextMeterDoesNotInheritStaleMax(t *testing.T) {
	var meter outboundContextMeter
	meter.observeEstimate(1000, 8000)
	meter.observeEstimate(2000, 0)
	fill, max, estimated := meter.providerFill(100)
	if fill != 2000 || max != 0 || !estimated {
		t.Fatalf("fill=%d max=%d estimated=%v", fill, max, estimated)
	}
}

func TestOutboundContextMeterHonorsZeroEstimateReset(t *testing.T) {
	var meter outboundContextMeter
	meter.observeEstimate(42450, 102400)
	meter.observeEstimate(0, 102400)
	if got := meter.preferPrompt(42450); got != 0 {
		t.Fatalf("zero-token estimate did not reset the result prompt: %d", got)
	}
	if fill, _, _ := meter.providerFill(192); fill != 192 {
		t.Fatalf("zero-token estimate still applied a stale floor: %d", fill)
	}
}

func TestOutboundContextMeterKeepsPlausibleProviderDifference(t *testing.T) {
	var meter outboundContextMeter
	meter.observeEstimate(10000, 102400)
	fill, max, estimated := meter.providerFill(9000)
	if fill != 9000 || max != 102400 || estimated {
		t.Fatalf("fill=%d max=%d estimated=%v", fill, max, estimated)
	}
}

func TestCodingSubAgentMeterKeepsOutboundEstimate(t *testing.T) {
	var events []AIAssistantStreamEvent
	app := &App{ctx: context.Background()}
	app.testEmit = func(name string, data ...interface{}) {
		if name != "ai-assistant-context-usage" || len(data) == 0 {
			return
		}
		payload, _ := data[0].(string)
		var event AIAssistantStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Errorf("decode context event: %v", err)
			return
		}
		events = append(events, event)
	}
	h := &IMMessageHandler{app: app, standaloneConfig: &StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{Model: "swe-2-high", URL: "https://llm.example/v1", Key: "k", ContextLength: 128000}
		},
	}}
	userID := "desktop-user:coding-partial-input"
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	cb := &codingSubAgentCallbacks{
		subagent:  &CodingSubAgent{handler: h, loopCtx: &LoopContext{UserID: userID}},
		compactor: NewSubAgentCompactor(102400, nil, nil, nil, nil),
	}

	cb.ReportContextWindowUsage(42450, 102400, true)
	cb.OnLLMUsage("swe-2-high", 192, 80)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.LastTurnInputTokens != 42450 {
		t.Fatalf("coding meter fill=%d, want reconciled 42450", mem.LastTurnInputTokens)
	}
	if len(events) == 0 {
		t.Fatal("context event was not published")
	}
	got := events[len(events)-1]
	if got.ContextUsedTokens != 42450 || got.ContextMaxTokens != 102400 || !got.ContextUsageEstimated {
		t.Fatalf("coding context event=%#v", got)
	}
	if got := cb.contextMeter.preferPrompt(192); got != 42450 {
		t.Fatalf("task-end prompt=%d, want reconciled 42450", got)
	}
}

func TestSharedUsageKeepsOutboundEstimateWhenProviderReportsPartialInput(t *testing.T) {
	var events []AIAssistantStreamEvent
	app := &App{ctx: context.Background()}
	app.testEmit = func(name string, data ...interface{}) {
		if name != "ai-assistant-context-usage" || len(data) == 0 {
			return
		}
		payload, _ := data[0].(string)
		var event AIAssistantStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Errorf("decode context event: %v", err)
			return
		}
		events = append(events, event)
	}
	h := &IMMessageHandler{app: app}
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "desktop-user:partial-input",
		llmCfg:  corelib.MaclawLLMConfig{Model: "swe-2-high", ContextLength: 128000},
	}

	cb.ReportContextWindowUsage(42450, 102400, true)
	cb.OnLLMUsage("swe-2-high", 192, 80)
	if len(events) != 2 {
		t.Fatalf("events=%#v", events)
	}
	got := events[1]
	if got.ContextUsedTokens != 42450 || got.ContextMaxTokens != 102400 || !got.ContextUsageEstimated {
		t.Fatalf("partial provider input collapsed the meter: %#v", got)
	}
	if cb.contextMeter.lastFillTokens() != 42450 {
		t.Fatalf("last fill=%d", cb.contextMeter.lastFillTokens())
	}
	if got := cb.contextMeter.preferPrompt(192); got != 42450 {
		t.Fatalf("task-end prompt=%d, want reconciled 42450", got)
	}

	// A higher provider count still wins (different tokenizer / exact usage).
	cb.ReportContextWindowUsage(42450, 102400, true)
	cb.OnLLMUsage("swe-2-high", 46000, 90)
	if len(events) != 4 {
		t.Fatalf("events=%#v", events)
	}
	if events[3].ContextUsedTokens != 46000 || events[3].ContextUsageEstimated {
		t.Fatalf("provider count higher than estimate was not published: %#v", events[3])
	}
}
