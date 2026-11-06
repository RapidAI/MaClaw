package guiapp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// TestHopLiveLLMRunsBeijingWeather compiles 北京天气 with the assistant model
// configured on this machine, then runs that spec. The web_search handler is
// the same search stack the desktop tool uses. Set MACLAW_HOP_LIVE=1 to run
// it; the default suite skips it because it spends a model call and a search.
func TestHopLiveLLMRunsBeijingWeather(t *testing.T) {
	if os.Getenv("MACLAW_HOP_LIVE") != "1" {
		t.Skip("set MACLAW_HOP_LIVE=1 to call the local assistant model")
	}
	app := &App{}
	cfg := app.GetMaclawLLMConfig()
	if strings.TrimSpace(cfg.Model) == "" || strings.TrimSpace(cfg.URL) == "" {
		t.Fatal("the local assistant model is not configured")
	}
	t.Logf("model=%s provider=%s", cfg.Model, cfg.ProviderName)

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	var llmCalls atomic.Int32
	caller := &guiHopJITCaller{
		app:     app,
		handler: liveHopWebSearchHandler(ctx),
		identity: hopJITExecutionIdentity{
			PrincipalID: "hop-live", PolicyOwnerID: "hop-live", SessionID: "hop-live", Platform: "desktop",
		},
		invoke: func(ctx context.Context, messages []interface{}, jsonResponse bool) (string, error) {
			n := llmCalls.Add(1)
			opts := simpleLLMRequestOptions{AwaitResponse: true}
			if jsonResponse {
				opts.ResponseFormat = map[string]interface{}{"type": "json_object"}
			}
			resp, err := doSimpleLLMRequestWithOptions(ctx, app.GetMaclawLLMConfig(), messages, nil, 180*time.Second, opts)
			if err != nil {
				return "", err
			}
			text := resp.Content
			if strings.TrimSpace(text) == "" {
				text = resp.RawContent
			}
			if jsonResponse {
				t.Logf("llm[%d] user=%s resp=%s", n, hopLiveUserExcerpt(messages), hopLiveShort(text))
			}
			return text, nil
		},
	}
	caller.task = "北京天气"
	spec, err := compileHopSpec(ctx, "北京天气", caller)
	compileCalls := llmCalls.Load()
	if err != nil {
		t.Fatalf("compile calls=%d err=%v", compileCalls, err)
	}
	parsed, errs := hopspec.Parse(spec)
	if len(errs) != 0 {
		t.Fatalf("parse errs=%v spec=%s", errs, spec)
	}
	if err := liveLookupToolsRequired(spec, "北京天气"); err != nil {
		t.Fatalf("compiled spec is not a retrieval: %v\n%s", err, spec)
	}
	if !hopLiveSpecListsSearch(parsed) {
		t.Fatalf("compiled spec has no web_search act:\n%s", spec)
	}
	t.Logf("compile_calls=%d steps=%d", compileCalls, len(parsed.Steps))

	store := hopjit.NewMemoryPersistence()
	result, err := runHopSpecWithPersistence(ctx, store, spec, nil, caller)
	if err != nil {
		t.Fatal(err)
	}
	for resumes := 0; result.Status == hopjit.NextPaused && resumes < 3; resumes++ {
		answers, ok := hopLiveBoolAnswers(result.Pause)
		if !ok {
			t.Fatalf("paused for a text answer: %+v", result.Pause)
		}
		result, err = resumeHopSpecWithPersistence(ctx, store, result.RunID, answers, caller)
		if err != nil {
			t.Fatal(err)
		}
	}
	if result.Status != hopjit.NextCompleted {
		reason := ""
		if result.Failure != nil {
			reason = result.Failure.Reason
		}
		for name, value := range result.Snapshot.Variables {
			t.Logf("var %s assigned=%v value=%s", name, value.Assigned, hopLiveShort(fmt.Sprint(value.Value)))
		}
		for id, state := range result.Snapshot.StepStates {
			t.Logf("step %s=%s", id, state)
		}
		t.Fatalf("status=%s reason=%s spec=%s", result.Status, reason, spec)
	}
	for _, decl := range parsed.Outputs {
		if decl.Name == "" || decl.Type == "bool" {
			continue
		}
		text, ok := result.Outputs[decl.Name].(string)
		if !ok || !hopLiveLooksLikeWeather(text) {
			t.Fatalf("output %s=%v spec=%s", decl.Name, result.Outputs[decl.Name], spec)
		}
		t.Logf("exec_calls=%d %s=%s", llmCalls.Load()-compileCalls, decl.Name, hopLiveShort(text))
	}
}

func liveHopWebSearchHandler(ctx context.Context) *IMMessageHandler {
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	strategy.BrowserFallbackEnabled = false
	registry := NewToolRegistry()
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query":       map[string]interface{}{"type": "string"},
			"max_results": map[string]interface{}{"type": "integer"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
	if err := registry.Register(RegisteredTool{
		Name: "web_search", Status: RegToolAvailable, InputSchema: schema,
		Description:          "Search the internet.",
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: tool.CapabilityFSReadLocal, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler: func(args map[string]interface{}) string {
			return agent.ToolWebSearchWithStrategyCtx(ctx, strategy, args)
		},
	}); err != nil {
		panic(err)
	}
	return &IMMessageHandler{registry: registry}
}

func hopLiveSpecListsSearch(spec hopspec.SpecAST) bool {
	for _, step := range spec.AllSteps() {
		if step.Type == hopspec.StepAct && hopStepListsTool(step, "web_search") {
			return true
		}
	}
	return false
}

func hopLiveBoolAnswers(pause *hopjit.PauseState) (map[string]any, bool) {
	if pause == nil || len(pause.OutputSchema) == 0 {
		return nil, false
	}
	answers := map[string]any{}
	for _, decl := range pause.OutputSchema {
		if decl.Type != "bool" || decl.Name == "" {
			return nil, false
		}
		answers[decl.Name] = true
	}
	return answers, true
}

func hopLiveLooksLikeWeather(text string) bool {
	text = strings.TrimSpace(text)
	if len([]rune(text)) < 12 || text == "晴" || strings.Contains(text, "搜索失败") || strings.Contains(text, "未找到相关结果") || strings.Contains(text, "Web search is not configured") {
		return false
	}
	return strings.Contains(text, "北京") || strings.Contains(text, "Beijing") || strings.Contains(text, "天气") || strings.Contains(text, "http")
}

func hopLiveUserExcerpt(messages []interface{}) string {
	var parts []string
	for _, message := range messages {
		row, ok := message.(map[string]interface{})
		if !ok || row["role"] != "user" {
			continue
		}
		text := fmt.Sprint(row["content"])
		parts = append(parts, fmt.Sprintf("len=%d beijing=%t weather_text=%t", len([]rune(text)), strings.Contains(text, "北京"), strings.Contains(text, "weather_text")))
		parts = append(parts, hopLiveShort(text))
	}
	return strings.Join(parts, " | ")
}

func hopLiveShort(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 180 {
		return string(runes[:180]) + "…"
	}
	return text
}
