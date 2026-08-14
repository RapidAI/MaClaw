package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestRepairCooldownFromHours(t *testing.T) {
	if got := RepairCooldownFromHours(0); got != DefaultRepairCooldown {
		t.Fatalf("0 hours = %v, want default %v", got, DefaultRepairCooldown)
	}
	if got := RepairCooldownFromHours(2); got != 2*time.Hour {
		t.Fatalf("2 hours = %v", got)
	}
}

func TestEvolutionPipeline_Status(t *testing.T) {
	p := NewEvolutionPipeline()
	p.EnableRepair = true
	p.EnableOptimizer = true
	st := p.Status()
	if !st.EnableRepair || !st.EnableOptimizer {
		t.Fatalf("status flags = %+v", st)
	}
	if st.RepairCooldown != DefaultRepairCooldown {
		t.Fatalf("cooldown = %v", st.RepairCooldown)
	}
	if st.PendingSkills != 0 {
		t.Fatalf("pending = %d", st.PendingSkills)
	}
}

func TestEventConstantsStable(t *testing.T) {
	// Guard against accidental renames that would desync GUI/frontend listeners.
	if EventSkillRepaired != "skill:repaired" || EventSkillOptimized != "skill:optimized" {
		t.Fatalf("event constants drifted: repaired=%q optimized=%q", EventSkillRepaired, EventSkillOptimized)
	}
}

func TestNotifySkillExecution_CoalescesSameSkill(t *testing.T) {
	p := NewEvolutionPipeline()
	// Don't Start — we only exercise the coalesce map + wake path.
	entry := &corelib.NLSkillEntry{
		Name:  "demo",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo 1"}}},
	}

	p.NotifySkillExecution("demo", entry, &SkillExecutionResultCompat{Success: true}, map[string]string{"input": "a"})
	p.NotifySkillExecution("demo", entry, &SkillExecutionResultCompat{Success: false}, map[string]string{"input": "b"})
	p.NotifySkillExecution("other", entry, &SkillExecutionResultCompat{Success: true}, nil)

	if got := p.PendingSkillCount(); got != 2 {
		t.Fatalf("PendingSkillCount = %d, want 2 (demo coalesced + other)", got)
	}
	if n := p.CoalescedNotifications.Load(); n != 1 {
		t.Fatalf("CoalescedNotifications = %d, want 1", n)
	}

	// Latest demo request should win.
	p.pendingMu.Lock()
	demo := p.pendingBySkill["demo"]
	p.pendingMu.Unlock()
	if demo.ExecResult == nil || demo.ExecResult.Success {
		t.Fatal("expected latest demo result Success=false")
	}
	if demo.RunArgs["input"] != "b" {
		t.Fatalf("RunArgs.input = %q, want b", demo.RunArgs["input"])
	}
}

func TestEvolutionPipeline_TakeAllPendingClearsMap(t *testing.T) {
	p := NewEvolutionPipeline()
	entry := &corelib.NLSkillEntry{Name: "x"}
	p.NotifySkillExecution("x", entry, nil, nil)
	batch := p.takeAllPending()
	if len(batch) != 1 {
		t.Fatalf("batch len = %d", len(batch))
	}
	if p.PendingSkillCount() != 0 {
		t.Fatal("map should be empty after takeAllPending")
	}
}

func TestEvolutionPipeline_tryRepair_InvokesHook(t *testing.T) {
	p := NewEvolutionPipeline()
	p.EnableOptimizer = false
	p.EnablePromoter = false
	p.PostExecDelay = time.Millisecond

	var hooked atomic.Int32
	p.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
		if entry == nil || entry.Name != "broken" {
			t.Errorf("unexpected entry: %#v", entry)
		}
		if runArgs["input"] != "x" {
			t.Errorf("runArgs = %#v", runArgs)
		}
		hooked.Add(1)
	}

	entry := &corelib.NLSkillEntry{
		Name:         "broken",
		Source:       "hub",
		Status:       "active",
		UsageCount:   1,
		SuccessCount: 0,
		LastError:    "[class: command_not_found] missing foo",
		Steps:        []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}
	// ShouldAttemptRepair: hub source + usage<=2 + repairable class.
	if !ShouldAttemptRepair(entry) {
		t.Fatal("fixture should be repair-eligible")
	}

	p.Start()
	defer p.Stop()
	p.NotifySkillExecution("broken", entry, &SkillExecutionResultCompat{Success: false}, map[string]string{"input": "x"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hooked.Load() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("RepairHook not called, hooked=%d", hooked.Load())
}

func TestEvolutionPipeline_tryRepair_Throttles(t *testing.T) {
	p := NewEvolutionPipeline()
	p.EnableOptimizer = false
	p.EnablePromoter = false
	p.RepairCooldown = time.Hour
	var calls atomic.Int32
	p.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
		calls.Add(1)
	}
	entry := &corelib.NLSkillEntry{
		Name: "broken", Source: "hub", Status: "active",
		UsageCount: 1, LastError: "[class: command_not_found] x",
	}
	// First call records throttle timestamp.
	p.tryRepair(context.Background(), evolutionRequest{
		SkillName:  "broken",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	})
	// Immediate second call should be throttled.
	p.tryRepair(context.Background(), evolutionRequest{
		SkillName:  "broken",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	})
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (second throttled)", calls.Load())
	}
}

func TestEvolutionPipeline_LoopProcessesCoalescedBatch(t *testing.T) {
	p := NewEvolutionPipeline()
	p.PostExecDelay = 20 * time.Millisecond
	p.EnableOptimizer = false
	p.EnablePromoter = false

	var processed atomic.Int32
	// processRequest always increments requestCount; use that + short run.
	p.Start()
	defer p.Stop()

	entry := &corelib.NLSkillEntry{Name: "batch-skill"}
	p.NotifySkillExecution("batch-skill", entry, &SkillExecutionResultCompat{Success: true}, nil)
	p.NotifySkillExecution("batch-skill", entry, &SkillExecutionResultCompat{Success: true}, nil)

	// Wait for delay + process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.PendingSkillCount() == 0 && p.requestCount.Load() >= 1 {
			processed.Store(int32(p.requestCount.Load()))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if processed.Load() < 1 {
		t.Fatalf("expected at least 1 processed request, requestCount=%d pending=%d", p.requestCount.Load(), p.PendingSkillCount())
	}
	// Coalesce means one process for two notifies of same skill (after delay drain).
	if p.requestCount.Load() != 1 {
		// If wake happened twice somehow, still ok if coalesced before first take —
		// but typically 1. Allow 1–2 for race of second notify after first take.
		if p.requestCount.Load() > 2 {
			t.Fatalf("requestCount = %d, want 1 or 2", p.requestCount.Load())
		}
	}
}

func TestTriggerOptimize_Guards(t *testing.T) {
	ctx := context.Background()
	entry := &corelib.NLSkillEntry{Name: "opt-me", UsageCount: 2, SuccessCount: 1}

	// nil pipeline
	res := (*EvolutionPipeline)(nil).TriggerOptimize(ctx, entry, true)
	if res.SkipReason == "" {
		t.Fatal("nil pipeline should set SkipReason")
	}

	// missing optimizer
	p := NewEvolutionPipeline()
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p.UsageTracker = tracker
	res = p.TriggerOptimize(ctx, entry, true)
	if !strings.Contains(res.SkipReason, "optimizer") {
		t.Fatalf("missing optimizer SkipReason=%q", res.SkipReason)
	}

	// missing tracker
	p2 := NewEvolutionPipeline()
	p2.Optimizer = NewSkillOptimizer(nil, nil, nil)
	res = p2.TriggerOptimize(ctx, entry, true)
	if !strings.Contains(res.SkipReason, "usage tracker") {
		t.Fatalf("missing tracker SkipReason=%q", res.SkipReason)
	}

	// file-backed hard block
	p3 := NewEvolutionPipeline()
	p3.UsageTracker = tracker
	p3.Optimizer = NewSkillOptimizer(nil, nil, nil)
	fileEntry := &corelib.NLSkillEntry{
		Name: "file-skill", Source: "file", SkillDir: "/tmp/skill",
		UsageCount: 10, SuccessCount: 7,
	}
	res = p3.TriggerOptimize(ctx, fileEntry, true)
	if !strings.Contains(res.SkipReason, "file-backed") {
		t.Fatalf("file-backed SkipReason=%q", res.SkipReason)
	}

	// Agent-guided Markdown project workflows need interactive orchestration,
	// not an LLM rewrite of their single craft_tool adapter step.
	workflowEntry := &corelib.NLSkillEntry{
		Name: "Book-PDF", Source: "clawhub", Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"instructions": "Phase 1 research with multiple background agents; confirm with the user; use templates/ and scripts/; maintain version.json."},
		}},
	}
	res = p3.TriggerOptimize(ctx, workflowEntry, true)
	if !strings.Contains(res.SkipReason, "agent-guided") {
		t.Fatalf("agent-guided SkipReason=%q", res.SkipReason)
	}
}

func TestTriggerOptimize_SkipsThresholdUnlessForce(t *testing.T) {
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker
	// nil LLM is fine — ShouldOptimize rejects before Optimize when force=false
	p.Optimizer = NewSkillOptimizer(nil, nil, nil)

	entry := &corelib.NLSkillEntry{
		Name: "low-usage", Source: "learned", Status: "active",
		UsageCount: 2, SuccessCount: 1, // below MinUsageCount=8
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}

	res := p.TriggerOptimize(context.Background(), entry, false)
	if !res.Skipped || res.Attempted {
		t.Fatalf("expected skip without force, got %+v", res)
	}
	if !strings.Contains(res.SkipReason, "threshold") {
		t.Fatalf("SkipReason=%q", res.SkipReason)
	}
}

func TestTriggerOptimize_ForceAttemptsLLM(t *testing.T) {
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker
	// configured mock that returns no optimization
	llm := &mockLLMRepairer{
		response: `{"optimized": false, "explanation": "nothing to improve"}`,
	}
	p.Optimizer = NewSkillOptimizer(llm, nil, nil)

	entry := &corelib.NLSkillEntry{
		Name: "force-opt", Source: "learned", Status: "active",
		UsageCount: 2, SuccessCount: 1, // would fail ShouldOptimize
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}

	res := p.TriggerOptimize(context.Background(), entry, true)
	if res.Skipped {
		t.Fatalf("force should not skip thresholds, got %+v", res)
	}
	if !res.Attempted {
		t.Fatalf("expected Attempted, got %+v", res)
	}
	if res.Optimized {
		t.Fatalf("mock returned optimized=false, got %+v", res)
	}
	if !strings.Contains(res.Explanation, "nothing to improve") {
		t.Fatalf("Explanation=%q", res.Explanation)
	}
}

func TestTriggerOptimize_ForceAppliesAndEmits(t *testing.T) {
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker

	llm := &mockLLMRepairer{
		response: `{
  "optimized": true,
  "explanation": "tighter command",
  "new_description": "improved",
  "new_steps": [{"action":"bash","params":{"command":"echo better"},"on_error":"stop"}]
}`,
	}
	p.Optimizer = NewSkillOptimizer(llm, nil, nil)

	var saved []corelib.NLSkillEntry
	p.SkillLoader = func() []corelib.NLSkillEntry {
		return []corelib.NLSkillEntry{{
			Name: "apply-opt", Source: "learned", Status: "active",
			UsageCount: 2, SuccessCount: 1,
			Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
		}}
	}
	p.SkillSaver = func(skills []corelib.NLSkillEntry) error {
		saved = append([]corelib.NLSkillEntry(nil), skills...)
		return nil
	}
	var emitted string
	p.EventEmitter = func(event string, payload map[string]string) {
		if event == EventSkillOptimized {
			emitted = payload["skill"]
		}
	}

	entry := &corelib.NLSkillEntry{
		Name: "apply-opt", Source: "learned", Status: "active",
		UsageCount: 2, SuccessCount: 1,
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	res := p.TriggerOptimize(context.Background(), entry, true)
	if !res.Optimized || !res.Attempted {
		t.Fatalf("expected optimized result, got %+v", res)
	}
	if entry.OptimizationCount != 1 || entry.Description != "improved" {
		t.Fatalf("entry not updated: count=%d desc=%q", entry.OptimizationCount, entry.Description)
	}
	if len(saved) != 1 || saved[0].Name != "apply-opt" {
		t.Fatalf("SkillSaver not called with updated skill: %#v", saved)
	}
	if emitted != "apply-opt" {
		t.Fatalf("EventSkillOptimized not emitted, skill=%q", emitted)
	}
}

func TestTriggerOptimize_ThrottleBypassedByForce(t *testing.T) {
	tracker, err := tool.NewUsageTracker("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewEvolutionPipeline()
	p.UsageTracker = tracker
	llm := &mockLLMRepairer{
		response: `{"optimized": false, "explanation": "noop"}`,
	}
	// Pre-seed throttle as if auto path just ran.
	p.optimizeAttempts = map[string]time.Time{"throttled": time.Now()}
	p.Optimizer = NewSkillOptimizer(llm, nil, nil)

	entry := &corelib.NLSkillEntry{
		Name: "throttled", Source: "learned", Status: "active",
		UsageCount: 10, SuccessCount: 7,
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}

	// Without force: either threshold skip (no retry evidence) or 24h throttle skip.
	resAuto := p.runOptimize(context.Background(), evolutionRequest{SkillName: "throttled", Entry: entry}, false)
	if resAuto.Attempted {
		t.Fatalf("auto path should not attempt while throttled/under threshold, got %+v", resAuto)
	}

	resForce := p.TriggerOptimize(context.Background(), entry, true)
	if resForce.Skipped && !resForce.Attempted {
		t.Fatalf("force should bypass throttle/threshold, got %+v", resForce)
	}
	if !resForce.Attempted {
		t.Fatalf("force should attempt LLM, got %+v", resForce)
	}
}

func TestEvolutionPipeline_tryRepair_CancelledCtxSkipsCooldown(t *testing.T) {
	p := NewEvolutionPipeline()
	p.EnableOptimizer = false
	p.EnablePromoter = false
	var calls atomic.Int32
	p.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
		calls.Add(1)
	}
	entry := &corelib.NLSkillEntry{
		Name: "broken", Source: "hub", Status: "active",
		UsageCount: 1, LastError: "[class: command_not_found] x",
	}
	req := evolutionRequest{
		SkillName:  "broken",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.tryRepair(ctx, req)

	if calls.Load() != 0 {
		t.Fatalf("hook called %d times with cancelled ctx", calls.Load())
	}
	p.throttleMu.Lock()
	_, recorded := p.repairAttempts["broken"]
	p.throttleMu.Unlock()
	if recorded {
		t.Fatal("cancelled attempt must not consume repair cooldown")
	}

	// 冷却未被消耗：立即重试应直接发起修复。
	p.tryRepair(context.Background(), req)
	if calls.Load() != 1 {
		t.Fatalf("retry after cancelled attempt should run hook once, got %d", calls.Load())
	}
}

func TestRunOptimize_NotPersistedSkipsEventAndUpload(t *testing.T) {
	newPipeline := func(t *testing.T, withSaver bool) (*EvolutionPipeline, *atomic.Int32, *atomic.Int32, *atomic.Int32) {
		tracker, err := tool.NewUsageTracker("")
		if err != nil {
			t.Fatal(err)
		}
		p := NewEvolutionPipeline()
		p.UsageTracker = tracker
		p.Optimizer = NewSkillOptimizer(&mockLLMRepairer{
			response: `{"optimized": true, "explanation": "tighter command", "new_steps": [{"action":"bash","params":{"command":"echo better"},"on_error":"stop"}]}`,
		}, nil, nil)
		// SkillLoader 列表里找不到该技能。
		p.SkillLoader = func() []corelib.NLSkillEntry { return nil }
		var saved, events, uploads atomic.Int32
		if withSaver {
			p.SkillSaver = func(skills []corelib.NLSkillEntry) error {
				saved.Add(1)
				return nil
			}
		}
		p.EventEmitter = func(event string, payload map[string]string) { events.Add(1) }
		p.UploadTrigger = func(skillName string, result *SkillExecutionResultCompat) { uploads.Add(1) }
		return p, &saved, &events, &uploads
	}
	entry := func() *corelib.NLSkillEntry {
		return &corelib.NLSkillEntry{
			Name: "ghost", Source: "learned", Status: "active",
			UsageCount: 10, SuccessCount: 5,
			Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
		}
	}

	t.Run("skill not found in storage", func(t *testing.T) {
		p, saved, events, uploads := newPipeline(t, true)
		res := p.TriggerOptimize(context.Background(), entry(), true)
		if !res.Attempted || res.Optimized {
			t.Fatalf("result = %+v, want attempted but not optimized", res)
		}
		if !strings.Contains(res.Explanation, "not persisted") {
			t.Fatalf("Explanation = %q", res.Explanation)
		}
		if saved.Load() != 0 || events.Load() != 0 || uploads.Load() != 0 {
			t.Fatalf("saved=%d events=%d uploads=%d, want all 0", saved.Load(), events.Load(), uploads.Load())
		}
	})

	t.Run("SkillSaver nil", func(t *testing.T) {
		p, _, events, uploads := newPipeline(t, false)
		res := p.TriggerOptimize(context.Background(), entry(), true)
		if !res.Attempted || res.Optimized {
			t.Fatalf("result = %+v, want attempted but not optimized", res)
		}
		if !strings.Contains(res.Explanation, "not persisted") {
			t.Fatalf("Explanation = %q", res.Explanation)
		}
		if events.Load() != 0 || uploads.Load() != 0 {
			t.Fatalf("events=%d uploads=%d, want all 0", events.Load(), uploads.Load())
		}
	})
}

func TestMergeEvolvedEntry_PreservesLiveStats(t *testing.T) {
	dst := &corelib.NLSkillEntry{
		Name: "s", Description: "old", Status: "active",
		UsageCount: 10, SuccessCount: 8, FailureCount: 2,
		LastError: "latest error", LastUsedAt: "2026-01-02T00:00:00Z",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "old"}}},
	}
	src := &corelib.NLSkillEntry{
		Name: "s", Description: "improved", Status: "needs_review",
		UsageCount: 1, SuccessCount: 0, FailureCount: 1,
		LastError: "stale error", LastUsedAt: "2025-01-01T00:00:00Z",
		Steps:              []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "new"}}},
		RepairAttemptCount: 2, LastRepairAt: "2026-01-01T00:00:00Z",
		RepairHistory:     []corelib.SkillRepairRecord{{Explanation: "fix"}},
		OptimizationCount: 3, LastOptimizedAt: "2026-01-03T00:00:00Z",
	}
	mergeEvolvedEntry(dst, src)

	// 进化修改的字段从 src 拷贝。
	if dst.Description != "improved" || dst.Status != "needs_review" {
		t.Fatalf("desc/status not merged: %q %q", dst.Description, dst.Status)
	}
	if len(dst.Steps) != 1 || dst.Steps[0].Params["command"] != "new" {
		t.Fatalf("steps not merged: %#v", dst.Steps)
	}
	if dst.RepairAttemptCount != 2 || dst.LastRepairAt == "" || len(dst.RepairHistory) != 1 {
		t.Fatalf("repair metadata not merged: %#v", dst)
	}
	if dst.OptimizationCount != 3 || dst.LastOptimizedAt == "" {
		t.Fatalf("optimization metadata not merged: %#v", dst)
	}
	// dst 的实时计数字段必须保留。
	if dst.UsageCount != 10 || dst.SuccessCount != 8 || dst.FailureCount != 2 {
		t.Fatalf("usage stats clobbered: %#v", dst)
	}
	if dst.LastError != "latest error" || dst.LastUsedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("last error/used clobbered: %#v", dst)
	}
}

func TestEvolutionPipeline_tryFileBackedRepairDraft(t *testing.T) {
	skillDir := t.TempDir()
	// draft 流要求技能目录里有 skill.yaml/skill.yml（SKILL.md-only 技能在
	// LLM 调用前就被跳过）。
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: file-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name: "file-skill", Source: "file", SkillDir: skillDir, Status: "active",
		UsageCount: 5, SuccessCount: 0, FailureCount: 5,
		LastError: "[class: command_not_found] missing foo",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}
	if ok, reason := ExplainRepairGate(entry); ok || reason != "file_backed" {
		t.Fatalf("fixture gate = (%v, %q), want (false, file_backed)", ok, reason)
	}

	var llmCalls atomic.Int32
	p := NewEvolutionPipeline()
	p.RepairCooldown = time.Millisecond
	p.LLM = &stubRepairLLM{
		respond: `{"repaired":true,"explanation":"use echo","new_steps":[{"action":"bash","params":{"command":"echo fixed"}}],"should_disable":false}`,
		onCall:  func([]map[string]string) { llmCalls.Add(1) },
	}
	var emitted map[string]string
	p.EventEmitter = func(event string, payload map[string]string) {
		if event == EventSkillRepairDraftReady {
			emitted = payload
		}
	}
	req := evolutionRequest{
		SkillName:  "file-skill",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	}

	p.tryRepair(context.Background(), req)

	// entry 不被修改：draft 流不落地任何变更。
	if entry.RepairAttemptCount != 0 || len(entry.RepairHistory) != 0 {
		t.Fatalf("entry repair metadata mutated: %#v", entry)
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Params["command"] != "foo" {
		t.Fatalf("entry steps mutated: %#v", entry.Steps)
	}

	// draft 文件已写盘且内容完整。
	draftsDir := filepath.Join(skillDir, RepairDraftsDirName)
	files, err := os.ReadDir(draftsDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("draft files = %v, err = %v, want exactly 1", files, err)
	}
	data, err := os.ReadFile(filepath.Join(draftsDir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var draft RepairDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Skill != "file-skill" || draft.Explanation != "use echo" || draft.CreatedAt == "" {
		t.Fatalf("draft = %#v", draft)
	}
	if draft.LastError != entry.LastError {
		t.Fatalf("draft.LastError = %q", draft.LastError)
	}
	if len(draft.OldSteps) != 1 || draft.OldSteps[0].Params["command"] != "foo" {
		t.Fatalf("draft.OldSteps = %#v", draft.OldSteps)
	}
	if len(draft.NewSteps) != 1 || draft.NewSteps[0].Params["command"] != "echo fixed" {
		t.Fatalf("draft.NewSteps = %#v", draft.NewSteps)
	}

	// 事件携带 skill + draft 文件名。
	if emitted == nil || emitted["skill"] != "file-skill" || emitted["draft"] != files[0].Name() {
		t.Fatalf("event payload = %#v, draft file = %s", emitted, files[0].Name())
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("llm calls = %d, want 1", llmCalls.Load())
	}

	// 已有未评审 draft 时不再调 LLM、不重复生成（越过冷却后再试）。
	time.Sleep(5 * time.Millisecond)
	p.tryRepair(context.Background(), req)
	if llmCalls.Load() != 1 {
		t.Fatalf("pending draft should skip LLM, calls = %d", llmCalls.Load())
	}
	files, err = os.ReadDir(draftsDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("draft files after retry = %v, err = %v, want exactly 1", files, err)
	}
}

func TestEvolutionPipeline_TriggerFileBackedRepairDraft_ForceCreatesReviewedDraft(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: local-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name: "local-skill", Source: "file", SkillDir: skillDir, Status: "active",
		// Deliberately below the automatic threshold: force must only bypass
		// that statistical gate, never the reviewed-draft requirement.
		UsageCount: 1, FailureCount: 1,
		LastError: "[class: command_not_found] missing foo",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}
	p := NewEvolutionPipeline()
	p.LLM = &stubRepairLLM{respond: `{"repaired":true,"explanation":"use echo","new_steps":[{"action":"bash","params":{"command":"echo fixed"}}]}`}
	result := p.TriggerFileBackedRepairDraft(context.Background(), entry, nil, true)
	if !result.Created || !result.RequiresReview || result.Draft == "" {
		t.Fatalf("result = %#v, want a reviewed draft", result)
	}
	if _, err := os.Stat(filepath.Join(skillDir, RepairDraftsDirName, result.Draft)); err != nil {
		t.Fatalf("draft not written: %v", err)
	}
	if entry.Steps[0].Params["command"] != "foo" {
		t.Fatalf("manual draft must not mutate file-backed entry: %#v", entry.Steps)
	}
}

func TestMergeEvolvedEntry_MergesRepairMarkers(t *testing.T) {
	newPair := func(srcErr string) (*corelib.NLSkillEntry, *corelib.NLSkillEntry) {
		dst := &corelib.NLSkillEntry{Name: "s", LastError: "live error"}
		src := &corelib.NLSkillEntry{Name: "s", LastError: srcErr}
		return dst, src
	}

	// 修复产物标记必须拷贝，否则落盘的仍是修复前旧错误、技能会被重复修复。
	for _, marker := range []string{"auto-repaired: rewrote command", "auto-disabled: unfixable"} {
		dst, src := newPair(marker)
		mergeEvolvedEntry(dst, src)
		if dst.LastError != marker {
			t.Fatalf("marker %q not merged, LastError = %q", marker, dst.LastError)
		}
	}

	// 普通实时错误统计仍保留 dst 的值。
	dst, src := newPair("stale pre-repair error")
	mergeEvolvedEntry(dst, src)
	if dst.LastError != "live error" {
		t.Fatalf("live LastError clobbered: %q", dst.LastError)
	}
}

// errorRepairLLM 总是让 LLM 调用失败，用于验证"LLM 已调用但失败"也消耗冷却。
type errorRepairLLM struct{ configured bool }

func (e *errorRepairLLM) IsConfigured() bool { return e.configured }
func (e *errorRepairLLM) ChatCall([]map[string]string) (string, error) {
	return "", fmt.Errorf("simulated LLM outage")
}

func repairCooldownRecorded(p *EvolutionPipeline, skill string) bool {
	p.throttleMu.Lock()
	defer p.throttleMu.Unlock()
	_, ok := p.repairAttempts[skill]
	return ok
}

func repairEligibleCoreEntry() *corelib.NLSkillEntry {
	return &corelib.NLSkillEntry{
		Name: "broken", Source: "hub", Status: "active",
		UsageCount: 1, SuccessCount: 0,
		LastError: "[class: command_not_found] missing foo",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}
}

// TestEvolutionPipeline_tryRepair_LLMErrorConsumesCooldown covers the core
// (non-hook, non-file-backed) path: an LLM call that was actually made but
// failed must consume the cooldown, otherwise a persistently failing skill
// would burn one LLM call on every failed execution.
func TestEvolutionPipeline_tryRepair_LLMErrorConsumesCooldown(t *testing.T) {
	req := evolutionRequest{
		SkillName:  "broken",
		Entry:      repairEligibleCoreEntry(),
		ExecResult: &SkillExecutionResultCompat{Success: false},
	}

	p := NewEvolutionPipeline()
	p.LLM = &errorRepairLLM{configured: true}
	p.tryRepair(context.Background(), req)
	if !repairCooldownRecorded(p, "broken") {
		t.Fatal("failed LLM call must consume the repair cooldown")
	}

	// LLM 未配置：没有真实调用成本，不写时间戳。
	p2 := NewEvolutionPipeline()
	p2.LLM = &errorRepairLLM{configured: false}
	p2.tryRepair(context.Background(), req)
	if repairCooldownRecorded(p2, "broken") {
		t.Fatal("unconfigured LLM must not consume the repair cooldown")
	}

	p3 := NewEvolutionPipeline() // LLM nil
	p3.tryRepair(context.Background(), req)
	if repairCooldownRecorded(p3, "broken") {
		t.Fatal("nil LLM must not consume the repair cooldown")
	}
}

// TestEvolutionPipeline_tryFileBackedRepairDraft_CooldownSemantics covers the
// draft path: LLM failure consumes the cooldown; a draft that cannot be
// written to disk does not (nothing was produced, immediate retry is OK).
func TestEvolutionPipeline_tryFileBackedRepairDraft_CooldownSemantics(t *testing.T) {
	newFileBackedReq := func(skillDir string) evolutionRequest {
		// draft 流要求技能目录里有 skill.yaml/skill.yml（SKILL.md-only 技能
		// 在 LLM 调用前就被跳过）。
		if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: file-skill\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return evolutionRequest{
			SkillName: "file-skill",
			Entry: &corelib.NLSkillEntry{
				Name: "file-skill", Source: "file", SkillDir: skillDir, Status: "active",
				UsageCount: 5, SuccessCount: 0, FailureCount: 5,
				LastError: "[class: command_not_found] missing foo",
				Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
			},
			ExecResult: &SkillExecutionResultCompat{Success: false},
		}
	}

	t.Run("LLM error consumes cooldown", func(t *testing.T) {
		p := NewEvolutionPipeline()
		p.LLM = &errorRepairLLM{configured: true}
		p.tryRepair(context.Background(), newFileBackedReq(t.TempDir()))
		if !repairCooldownRecorded(p, "file-skill") {
			t.Fatal("failed LLM call must consume the repair cooldown on the draft path")
		}
	})

	t.Run("draft write failure keeps cooldown", func(t *testing.T) {
		skillDir := t.TempDir()
		// 把 .evolution-drafts 预先建成普通文件，让 WriteRepairDraft 的
		// MkdirAll 失败，模拟写盘失败。
		if err := os.WriteFile(filepath.Join(skillDir, RepairDraftsDirName), []byte("block"), 0o644); err != nil {
			t.Fatal(err)
		}
		var emitted atomic.Int32
		p := NewEvolutionPipeline()
		p.LLM = &stubRepairLLM{
			respond: `{"repaired":true,"explanation":"use echo","new_steps":[{"action":"bash","params":{"command":"echo fixed"}}],"should_disable":false}`,
		}
		p.EventEmitter = func(event string, payload map[string]string) { emitted.Add(1) }
		p.tryRepair(context.Background(), newFileBackedReq(skillDir))
		if emitted.Load() != 0 {
			t.Fatal("no event expected when draft write fails")
		}
		if repairCooldownRecorded(p, "file-skill") {
			t.Fatal("failed draft write must not consume the repair cooldown")
		}
	})

	t.Run("unconfigured LLM keeps cooldown", func(t *testing.T) {
		p := NewEvolutionPipeline()
		p.LLM = &errorRepairLLM{configured: false}
		p.tryRepair(context.Background(), newFileBackedReq(t.TempDir()))
		if repairCooldownRecorded(p, "file-skill") {
			t.Fatal("unconfigured LLM must not consume the repair cooldown on the draft path")
		}
	})
}
