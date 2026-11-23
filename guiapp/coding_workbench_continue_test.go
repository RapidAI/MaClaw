package guiapp

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestSoftenExploreOnlyPlanDeps(t *testing.T) {
	tasks := finalizeCodingWorkbenchTasks([]*v2.TaskItem{
		{Title: "explore auth", Description: "定位登录入口"},
		{Title: "explore billing", Description: "探查计费模块"},
		{Title: "implement JWT", Description: "实现签发"},
	}, "build auth and billing")
	tasks = softenExploreOnlyPlanDeps(tasks)
	if len(tasks) != 3 {
		t.Fatalf("len=%d", len(tasks))
	}
	// T2 explore should no longer depend solely on T1 explore.
	if len(tasks[1].DependsOn) != 0 {
		t.Fatalf("explore T2 should have empty deps after soften, got %v", tasks[1].DependsOn)
	}
	// Implement still depends on previous.
	if len(tasks[2].DependsOn) == 0 {
		t.Fatal("implement step should keep a dependency")
	}
}

func TestTaskRunnerParallelWave(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32
	sub := func(ctx context.Context, task *v2.TaskItem, config v2.TaskRunnerConfig, onToken func(string), onProgress func(string)) *v2.TaskRunResult {
		n := atomic.AddInt32(&concurrent, 1)
		for {
			cur := atomic.LoadInt32(&maxConcurrent)
			if n <= cur || atomic.CompareAndSwapInt32(&maxConcurrent, cur, n) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
		return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskPassed, Summary: "ok"}
	}
	// Two independent tasks (no deps) + one dependent.
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "A"},
		{Index: 2, Title: "B"},
		{Index: 3, Title: "C", DependsOn: []int{1, 2}},
	}
	runner := v2.NewTaskRunner(v2.TaskRunnerConfig{ProjectPath: t.TempDir(), MaxRetries: 0, MaxParallel: 2}, sub)
	results := runner.RunAll(context.Background(), tasks, nil, nil)
	if len(results) != 3 {
		t.Fatalf("results=%d", len(results))
	}
	for _, r := range results {
		if r.Status != v2.TaskPassed {
			t.Fatalf("result=%+v", r)
		}
	}
	if atomic.LoadInt32(&maxConcurrent) < 2 {
		t.Fatalf("expected parallel wave concurrency>=2, got %d", maxConcurrent)
	}
}

func TestCodingWorkbenchParallelWriterAdmission(t *testing.T) {
	project := t.TempDir()
	base := func(filesA, filesB []string) []*v2.TaskItem {
		return []*v2.TaskItem{
			{Index: 1, Title: "implement auth", Description: "write auth", Files: filesA},
			{Index: 2, Title: "implement billing", Description: "write billing", Files: filesB},
		}
	}
	if !codingWorkbenchCanRunParallelWriterWave(codingRequestImplementation, true, codingWorktreeModeAuto, project, base([]string{"internal/auth/service.go"}, []string{"internal/billing/service.go"})) {
		t.Fatal("disjoint declared writer wave should be admitted")
	}
	for _, wave := range [][]*v2.TaskItem{
		base(nil, []string{"internal/billing/service.go"}),
		base([]string{"internal/auth/"}, []string{"internal/auth/service.go"}),
		base([]string{"../escape.go"}, []string{"internal/billing/service.go"}),
		base([]string{"internal/auth/*.go"}, []string{"internal/billing/service.go"}),
	} {
		if codingWorkbenchCanRunParallelWriterWave(codingRequestImplementation, true, codingWorktreeModeAuto, project, wave) {
			t.Fatalf("unsafe writer wave admitted: %+v", wave)
		}
	}
	if codingWorkbenchCanRunParallelWriterWave(codingRequestImplementation, true, codingWorktreeModeOff, project, base([]string{"a.go"}, []string{"b.go"})) {
		t.Fatal("worktree-off writer wave must remain serial")
	}
}

func TestParseRemoteVerifyExitCode(t *testing.T) {
	if parseRemoteVerifyExitCode("ok\n__MACLAW_VERIFY_EXIT:0__\n") != 0 {
		t.Fatal("want 0")
	}
	if parseRemoteVerifyExitCode("fail\n__MACLAW_VERIFY_EXIT:1__") != 1 {
		t.Fatal("want 1")
	}
}

func TestCodingLoopUsageFields(t *testing.T) {
	in, out, cost := codingLoopUsageFields(agent.TurnUsage{InputTokens: 10, OutputTokens: 5})
	if in != 10 || out != 5 {
		t.Fatalf("in=%d out=%d", in, out)
	}
	if cost <= 0 {
		t.Fatalf("expected est cost > 0, got %v", cost)
	}
	_, _, _, lastPrompt, lastCompletion := codingLoopWindowUsage(agent.TurnUsage{InputTokens: 10, OutputTokens: 5})
	if lastPrompt != 10 || lastCompletion != 5 {
		t.Fatalf("single request last prompt=%d completion=%d", lastPrompt, lastCompletion)
	}
	_, _, _, lastPrompt, lastCompletion = codingLoopWindowUsage(agent.TurnUsage{
		InputTokens: 30213603, OutputTokens: 199260, Requests: 1250,
		LastPromptTokens: 24000, LastCompletionTokens: 180,
	})
	if lastPrompt != 24000 || lastCompletion != 180 {
		t.Fatalf("multi-request last prompt=%d completion=%d", lastPrompt, lastCompletion)
	}
	_, _, _, _, lastCompletion = codingLoopWindowUsage(agent.TurnUsage{
		InputTokens: 100, OutputTokens: 50, Requests: 4,
	})
	if lastCompletion != 0 {
		t.Fatalf("multi-request output sum became 本轮 out: %d", lastCompletion)
	}
}

func TestAccumulateStickyCodingUsage(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:cost-test"
	h.accumulateStickyCodingUsage(userID, 100, 20, 0.01)
	h.accumulateStickyCodingUsage(userID, 50, 10, 0.005)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionInputTokens != 150 || mem.SessionOutputTokens != 30 {
		t.Fatalf("tokens in=%d out=%d", mem.SessionInputTokens, mem.SessionOutputTokens)
	}
	if mem.LastTurnInputTokens != 50 {
		t.Fatalf("last turn=%d", mem.LastTurnInputTokens)
	}
	line := formatCodingSessionCostLine(mem)
	if !strings.Contains(line, "in=150") {
		t.Fatalf("line=%q", line)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestCodingWorkbenchStatusContextTokens(t *testing.T) {
	// Configured coding model → max comes from the resolved config, and the
	// used fill is the last request's prompt size.
	h := &IMMessageHandler{standaloneConfig: &StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{Model: "coding-model", URL: "https://llm.example/v1", Key: "k", ContextLength: 256000}
		},
	}}
	path := t.TempDir()
	userID := projectSessionOwnerID(path)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.accumulateStickyCodingUsage(userID, 126000, 2000, 0)
	st := (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.ContextUsedTokens != 126000 {
		t.Fatalf("ContextUsedTokens=%d", st.ContextUsedTokens)
	}
	// EffectiveContextTokens reserves 20% headroom: 256000 * 0.8 = 204800.
	if st.ContextMaxTokens != 204800 {
		t.Fatalf("ContextMaxTokens=%d", st.ContextMaxTokens)
	}
}

func TestCodingWorkbenchStatusContextTokensUsesLastPromptNotSessionSum(t *testing.T) {
	h := &IMMessageHandler{standaloneConfig: &StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{Model: "coding-model", URL: "https://llm.example/v1", Key: "k", ContextLength: 262144}
		},
	}}
	path := t.TempDir()
	userID := projectSessionOwnerID(path)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.noteCodingContextFill(userID, 257658, 0)
	st := (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.ContextUsedTokens != 257658 {
		t.Fatalf("live fill=%d", st.ContextUsedTokens)
	}
	h.noteCodingContextFill(userID, 122, 0)
	st = (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.ContextUsedTokens != 257658 {
		t.Fatalf("degenerate prompt replaced the fill: %d", st.ContextUsedTokens)
	}
	// Task end adds the lifetime sums and the last request. The bar shows the
	// prompt; 本轮 out shows the last completion; the session chip keeps both
	// sums. 262144*0.8 = 209715.
	h.recordStickyCodingUsage(userID, 30213603, 24000, 199260, 180, 0)
	st = (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.ContextUsedTokens != 24000 {
		t.Fatalf("ContextUsedTokens=%d, want last prompt", st.ContextUsedTokens)
	}
	if st.SessionInputTokens != 30213603 {
		t.Fatalf("session=%d", st.SessionInputTokens)
	}
	if st.LastTurnOutputTokens != 180 || st.SessionOutputTokens != 199260 {
		t.Fatalf("本轮 out=%d session out=%d", st.LastTurnOutputTokens, st.SessionOutputTokens)
	}
	if st.ContextMaxTokens != 209715 {
		t.Fatalf("ContextMaxTokens=%d", st.ContextMaxTokens)
	}
	// A task-end report with no completion must not wipe the live 本轮 out.
	h.recordStickyCodingUsage(userID, 10, 3000, 20, 0, 0)
	st = (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.LastTurnOutputTokens != 180 || st.SessionOutputTokens != 199280 {
		t.Fatalf("zero completion clobbered 本轮 out=%d session out=%d", st.LastTurnOutputTokens, st.SessionOutputTokens)
	}
}

func TestRecordStickyCodingUsagePublishesPromptNotSessionSum(t *testing.T) {
	var raw string
	app := &App{ctx: context.Background(), testEmit: func(name string, data ...interface{}) {
		if name == "ai-assistant-context-usage" && len(data) > 0 {
			raw, _ = data[0].(string)
		}
	}}
	h := &IMMessageHandler{app: app, standaloneConfig: &StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{Model: "coding-model", URL: "https://llm.example/v1", Key: "k", ContextLength: 256000}
		},
	}}
	userID := "desktop-user:usage-boundary"
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })

	h.noteCodingContextFill(userID, 257658, 40)
	if !strings.Contains(raw, `"context_used_tokens":257658`) {
		t.Fatalf("live event=%s", raw)
	}
	raw = ""
	// Task end: the session sum and the last prompt commit together, but the
	// window event carries only the prompt.
	h.recordStickyCodingUsage(userID, 30213603, 24000, 199260, 180, 0)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionInputTokens != 30213603 || mem.LastTurnInputTokens != 24000 {
		t.Fatalf("session=%d last=%d", mem.SessionInputTokens, mem.LastTurnInputTokens)
	}
	if !strings.Contains(raw, `"context_used_tokens":24000`) || strings.Contains(raw, "30213603") {
		t.Fatalf("task-end event=%s", raw)
	}
	raw = ""
	h.recordStickyCodingUsage(userID, 10, 0, 0, 0, 0)
	if raw != "" {
		t.Fatalf("session-only delta published %s", raw)
	}
	mem = h.getStickyCodingWorkbenchMemory(userID)
	if mem.LastTurnInputTokens != 24000 || mem.SessionInputTokens != 30213613 {
		t.Fatalf("session-only delta moved the fill: session=%d last=%d", mem.SessionInputTokens, mem.LastTurnInputTokens)
	}
}

// Repro BR-20261122-4FFF37BF12F7: the provider reported a 257658 prompt, then
// the task ended with a 1250-request sum of 30213603. Task end must commit
// that sum as the session total and the last prompt as the window fill. A
// status poll after the live note is gone must not paint the sum as used.
func TestCodingTaskEndReproDoesNotPaintSessionSumAsWindow(t *testing.T) {
	var raw string
	app := &App{ctx: context.Background(), testEmit: func(name string, data ...interface{}) {
		if name == "ai-assistant-context-usage" && len(data) > 0 {
			raw, _ = data[0].(string)
		}
	}}
	h := &IMMessageHandler{app: app, standaloneConfig: &StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{Model: "coding-model", URL: "https://llm.example/v1", Key: "k", ContextLength: 262144}
		},
	}}
	path := t.TempDir()
	userID := projectSessionOwnerID(path)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })

	live := agent.TurnUsage{InputTokens: 257658, OutputTokens: 40, Requests: 1}
	h.noteCodingContextFill(userID, live.InputTokens, live.OutputTokens)
	if !strings.Contains(raw, `"context_used_tokens":257658`) || strings.Contains(raw, "30213603") {
		t.Fatalf("live event=%s", raw)
	}

	// Same shape the subagent hands the workflow: lifetime sums plus the last
	// request. The window fields come from that split, not from the sum.
	finished := agent.TurnUsage{
		InputTokens: 30213603, OutputTokens: 199260, Requests: 1250,
		LastPromptTokens: 24000, LastCompletionTokens: 180,
	}
	sessionIn, sessionOut, _, lastPrompt, lastCompletion := codingLoopWindowUsage(finished)
	if sessionIn != 30213603 || lastPrompt != 24000 || lastCompletion != 180 {
		t.Fatalf("handoff session=%d prompt=%d completion=%d", sessionIn, lastPrompt, lastCompletion)
	}
	raw = ""
	h.recordStickyCodingUsage(userID, sessionIn, lastPrompt, sessionOut, lastCompletion, 0)

	st := (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.ContextUsedTokens != 24000 || st.ContextMaxTokens != 209715 {
		t.Fatalf("status used=%d max=%d, session sum must not fill the meter", st.ContextUsedTokens, st.ContextMaxTokens)
	}
	if st.SessionInputTokens != 30213603 || st.SessionOutputTokens != 199260 {
		t.Fatalf("session in=%d out=%d", st.SessionInputTokens, st.SessionOutputTokens)
	}
	if st.LastTurnOutputTokens != 180 {
		t.Fatalf("本轮 out=%d", st.LastTurnOutputTokens)
	}
	if !strings.Contains(raw, `"context_used_tokens":24000`) || strings.Contains(raw, "30213603") {
		t.Fatalf("task-end event=%s", raw)
	}

	// The log path that missed the live note: task end is the only commit.
	// The session sum still must not become the fill.
	h.clearStickyCodingWorkbenchMemory(userID)
	raw = ""
	h.recordStickyCodingUsage(userID, sessionIn, lastPrompt, sessionOut, lastCompletion, 0)
	st = (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.ContextUsedTokens != 24000 || st.SessionInputTokens != 30213603 {
		t.Fatalf("task-end-only used=%d session=%d", st.ContextUsedTokens, st.SessionInputTokens)
	}
	if !strings.Contains(raw, `"context_used_tokens":24000`) || strings.Contains(raw, "30213603") {
		t.Fatalf("task-end-only event=%s", raw)
	}
}

func TestCodingWorkbenchStatusContextTokensDegrade(t *testing.T) {
	// No turn yet and no resolvable model → the cap is 0 rather than an
	// invented number. The session sum must not stand in for the window fill:
	// it is a lifetime total and paints "used" far above the window
	// (BR-20261122-4FFF37BF12F7).
	h := &IMMessageHandler{}
	path := t.TempDir()
	userID := projectSessionOwnerID(path)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.SessionInputTokens = 42000
	})
	st := (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.ContextUsedTokens != 0 {
		t.Fatalf("ContextUsedTokens=%d, session sum must not fill the meter", st.ContextUsedTokens)
	}
	if st.ContextMaxTokens != 0 {
		t.Fatalf("ContextMaxTokens=%d, want 0 when no model resolves", st.ContextMaxTokens)
	}
}

func TestCodingWorkbenchStatusContextTokensIgnoresSessionSumWhenWindowKnown(t *testing.T) {
	h := &IMMessageHandler{standaloneConfig: &StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{Model: "coding-model", URL: "https://llm.example/v1", Key: "k", ContextLength: 256000}
		},
	}}
	path := t.TempDir()
	userID := projectSessionOwnerID(path)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.SessionInputTokens = 30_213_603
	})
	st := (&App{}).codingWorkbenchStatusFromHandler(path, h)
	if st.ContextUsedTokens != 0 {
		t.Fatalf("ContextUsedTokens=%d, want 0 until a prompt is recorded", st.ContextUsedTokens)
	}
	if st.ContextMaxTokens != 204800 {
		t.Fatalf("ContextMaxTokens=%d", st.ContextMaxTokens)
	}
	if st.SessionInputTokens != 30_213_603 {
		t.Fatalf("session=%d", st.SessionInputTokens)
	}
}

func TestIsCodingWorkbenchSlashBgCost(t *testing.T) {
	if !isCodingWorkbenchSlash("/bg test") || !isCodingWorkbenchSlash("/cost") {
		t.Fatal("bg/cost should classify")
	}
	if classifyImmediateIMCommand("/bg test") != imCommandCodingWorkbench {
		t.Fatal("classify bg")
	}
}
