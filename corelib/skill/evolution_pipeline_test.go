package skill

import (
	"context"
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
		if p.PendingSkillCount() == 0 && p.requestCount >= 1 {
			processed.Store(int32(p.requestCount))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if processed.Load() < 1 {
		t.Fatalf("expected at least 1 processed request, requestCount=%d pending=%d", p.requestCount, p.PendingSkillCount())
	}
	// Coalesce means one process for two notifies of same skill (after delay drain).
	if p.requestCount != 1 {
		// If wake happened twice somehow, still ok if coalesced before first take —
		// but typically 1. Allow 1–2 for race of second notify after first take.
		if p.requestCount > 2 {
			t.Fatalf("requestCount = %d, want 1 or 2", p.requestCount)
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
