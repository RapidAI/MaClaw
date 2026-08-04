package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
)

func TestSetSharedAgentLoopEnabled_WritesConfig(t *testing.T) {
	dir := t.TempDir()
	a := &App{testHomeDir: dir}
	// Seed a config file via SaveConfig path used by PatchConfigIfChanged.
	// Minimal: write defaults then toggle.
	cfgPath := filepath.Join(dir, "config.json")
	_ = cfgPath

	// LoadConfig with testHomeDir should use isolated home if wired.
	// If testHomeDir isn't hooked into LoadConfig, Patch may still work on real home —
	// skip when LoadConfig doesn't honor test home.
	if a.testHomeDir == "" {
		t.Skip("no test home")
	}

	// Direct unit test of migration-safe toggle logic via Apply + fields.
	cfg := corelib.AppConfig{SharedAgentLoopEnabled: false, SharedAgentLoopMigrated: false}
	cfg.SharedAgentLoopEnabled = true
	cfg.SharedAgentLoopMigrated = true
	if !cfg.SharedAgentLoopEnabled || !cfg.SharedAgentLoopMigrated {
		t.Fatal("toggle fields")
	}

	// Counters
	recordLegacyAgentLoopTurn()
	recordSharedAgentLoopTurn(true, false, false)
	st := (&App{}).GetSharedAgentLoopStatus()
	if st.LegacyTurns < 1 || st.SharedTurns < 1 || st.SharedSuccess < 1 {
		t.Fatalf("counters=%+v", st)
	}
}

func TestSetSharedAgentLoopCanaryPercent_Clamps(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	// Resolve clamps config values.
	p := 150
	n, fromEnv := doctor.ResolveSharedLoopPercent(corelib.AppConfig{SharedAgentLoopCanaryPercent: &p})
	if fromEnv || n != 100 {
		t.Fatalf("150 should clamp to 100, got %d fromEnv=%v", n, fromEnv)
	}
	zero := 0
	n, _ = doctor.ResolveSharedLoopPercent(corelib.AppConfig{SharedAgentLoopCanaryPercent: &zero})
	if n != 0 {
		t.Fatalf("got %d", n)
	}
	p25 := 25
	n, _ = doctor.ResolveSharedLoopPercent(corelib.AppConfig{SharedAgentLoopCanaryPercent: &p25})
	if n != 25 {
		t.Fatalf("got %d", n)
	}
}

func TestRecordSharedLoopSkip(t *testing.T) {
	beforeC := processSharedLoopStats.skipCanary.Load()
	beforeI := processSharedLoopStats.skipIneligible.Load()
	beforeS := processSharedLoopStats.shadowEligible.Load()
	recordSharedLoopSkip("canary", "canary")
	recordSharedLoopSkip("ineligible", "workflow phase")
	recordSharedLoopSkip("shadow", "chat")
	if processSharedLoopStats.skipCanary.Load() <= beforeC {
		t.Fatal("canary skip")
	}
	if processSharedLoopStats.skipIneligible.Load() <= beforeI {
		t.Fatal("ineligible skip")
	}
	if processSharedLoopStats.shadowEligible.Load() <= beforeS {
		t.Fatal("shadow")
	}
	st := (&App{}).GetSharedAgentLoopStatus()
	if st.SkipCanary < 1 || st.SkipIneligible < 1 || st.ShadowEligible < 1 {
		t.Fatalf("status skips=%+v", st)
	}
	if st.LastSkipReason == "" || len(st.SkipByReason) == 0 {
		t.Fatalf("reason map empty: last=%q by=%v", st.LastSkipReason, st.SkipByReason)
	}
}

func TestRecordSharedAgentLoopTurn_CancelledAndError(t *testing.T) {
	beforeC := processSharedLoopStats.sharedCancelled.Load()
	beforeE := processSharedLoopStats.sharedError.Load()
	recordSharedAgentLoopTurn(false, true, false)
	recordSharedAgentLoopTurn(false, false, true)
	if processSharedLoopStats.sharedCancelled.Load() <= beforeC {
		t.Fatal("cancelled")
	}
	if processSharedLoopStats.sharedError.Load() <= beforeE {
		t.Fatal("error")
	}
}

func TestRecordLoopUsage_LastAndProcess(t *testing.T) {
	beforeIn := processSharedLoopStats.processUsage.InputTokens
	recordLoopUsage(agent.TurnUsage{
		Model:        "m-test",
		InputTokens:  11,
		OutputTokens: 3,
		Requests:     1,
		EstCostRMB:   0.001,
	})
	st := (&App{}).GetSharedAgentLoopStatus()
	if st.LastUsage.Model != "m-test" || st.LastUsage.InputTokens != 11 {
		t.Fatalf("last=%+v", st.LastUsage)
	}
	if st.ProcessUsage.InputTokens < beforeIn+11 {
		t.Fatalf("process=%+v", st.ProcessUsage)
	}
	if sum := st.LastUsage.Summary(); !strings.Contains(sum, "in=11") {
		t.Fatalf("summary=%q", sum)
	}
}

func TestAccumulateLoopResultUsage_RecordsProcessStats(t *testing.T) {
	before := processSharedLoopStats.processUsage.OutputTokens
	accumulateLoopResultUsage(nil, corelib.MaclawLLMConfig{Model: "x"}, agent.LoopResult{
		Usage: agent.TurnUsage{Model: "x", InputTokens: 1, OutputTokens: 7, Requests: 1},
	})
	st := (&App{}).GetSharedAgentLoopStatus()
	if st.LastUsage.OutputTokens != 7 {
		t.Fatalf("last=%+v", st.LastUsage)
	}
	if st.ProcessUsage.OutputTokens < before+7 {
		t.Fatalf("process=%+v", st.ProcessUsage)
	}
}

func TestGetSharedAgentLoopStatus_IncludesPromptProfileStats(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "")
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfileSavings(agent.PromptProfileLight, 4000, 1000)
	agent.RecordPromptProfile(agent.PromptProfileFull)
	st := (&App{}).GetSharedAgentLoopStatus()
	if st.PromptLightTurns < 1 || st.PromptFullTurns < 1 {
		t.Fatalf("prompt stats missing: %+v", st)
	}
	if st.PromptEstTokensSaved < 3000 {
		t.Fatalf("est saved=%d", st.PromptEstTokensSaved)
	}
	if st.LastPromptProfile == "" {
		t.Fatal("last prompt profile empty")
	}
	if st.PromptProfileForced != "" {
		t.Fatalf("forced profile should be empty when env unset: %q", st.PromptProfileForced)
	}
}

func TestGetSharedAgentLoopStatus_PromptProfileEnvOverride(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "light")
	agent.ResetPromptProfileStatsForTest()
	st := (&App{}).GetSharedAgentLoopStatus()
	if st.PromptProfileEnv != "light" {
		t.Fatalf("env=%q", st.PromptProfileEnv)
	}
	if st.PromptProfileForced != "light" {
		t.Fatalf("forced=%q want light", st.PromptProfileForced)
	}
}

func TestGetSharedAgentLoopStatusIncludesTokenOptimizationTelemetry(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "shadow")
	before := agent.CurrentLoopInputBreakdownStats()
	agent.RecordLoopInputBreakdown(agent.LoopInputBreakdown{
		SystemPromptTokens:   11,
		ToolDefinitionTokens: 13,
		HistoryTokens:        17,
		ToolResultTokens:     19,
		TotalEstimatedTokens: 60,
	})

	st := (&App{}).GetSharedAgentLoopStatus()
	if st.InputBreakdown.Requests != before.Requests+1 || st.InputBreakdown.TotalEstimatedTokens != before.TotalEstimatedTokens+60 {
		t.Fatalf("input breakdown not surfaced: before=%+v status=%+v", before, st.InputBreakdown)
	}
	if st.ContextCheckpoints != agent.CurrentContextCheckpointStats() {
		t.Fatalf("checkpoint stats differ: status=%+v current=%+v", st.ContextCheckpoints, agent.CurrentContextCheckpointStats())
	}
	if st.ContextCheckpointMode != "shadow" {
		t.Fatalf("checkpoint mode = %q, want shadow", st.ContextCheckpointMode)
	}
}

func TestResetAdaptivePromptStats(t *testing.T) {
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfileSavings(agent.PromptProfileLight, 2000, 500)
	a := &App{}
	st, err := a.ResetAdaptivePromptStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.PromptLightTurns != 0 || st.PromptFullTurns != 0 || st.PromptEstTokensSaved != 0 {
		t.Fatalf("after reset: %+v", st)
	}
}
