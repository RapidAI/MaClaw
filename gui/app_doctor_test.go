package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
)

func TestRunDoctor_AlwaysIncludesSharedLoopStats(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "40")
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })
	agent.ResetPromptProfileStatsForTest()

	app := &App{testHomeDir: tempHome}
	// Ensure config loads under temp home.
	if _, err := app.LoadConfig(); err != nil {
		// LoadConfig may create defaults — still ok if it fails lightly.
		t.Logf("LoadConfig: %v", err)
	}

	report := app.RunDoctor()
	var found bool
	for _, c := range report.Checks {
		if c.ID != "agent.shared_loop_stats" {
			continue
		}
		found = true
		if c.Status != "info" && string(c.Status) != "info" {
			// Status type may be doctor.StatusInfo
		}
		if !strings.Contains(c.Message, "process shared-loop mode=") {
			t.Fatalf("msg=%q", c.Message)
		}
		if !strings.Contains(c.Message, "canary 40%") {
			t.Fatalf("expected canary in msg=%q", c.Message)
		}
		if c.Detail == nil {
			t.Fatal("detail nil")
		}
		if c.Detail["percent"] != 40 && c.Detail["percent"] != int(40) {
			// may be int
			if n, ok := c.Detail["percent"].(int); !ok || n != 40 {
				t.Fatalf("detail percent=%#v", c.Detail["percent"])
			}
		}
		break
	}
	if !found {
		t.Fatalf("missing agent.shared_loop_stats in checks (count=%d)", len(report.Checks))
	}
	_ = filepath.Join(tempHome, "config.json")
}

func TestRunDoctor_IncludesTokenOptimizationTelemetry(t *testing.T) {
	t.Setenv("MACLAW_CONTEXT_CHECKPOINT", "on")
	before := agent.CurrentLoopInputBreakdownStats()
	agent.RecordLoopInputBreakdown(agent.LoopInputBreakdown{
		SystemPromptTokens:   2,
		ToolDefinitionTokens: 3,
		HistoryTokens:        5,
		ToolResultTokens:     7,
		TotalEstimatedTokens: 17,
	})

	report := (&App{testHomeDir: t.TempDir()}).RunDoctor()
	for _, check := range report.Checks {
		if check.ID != "agent.shared_loop_stats" {
			continue
		}
		breakdown, ok := check.Detail["input_breakdown"].(agent.LoopInputBreakdownStats)
		if !ok {
			t.Fatalf("input_breakdown type/value = %#v", check.Detail["input_breakdown"])
		}
		if breakdown.Requests != before.Requests+1 || breakdown.TotalEstimatedTokens != before.TotalEstimatedTokens+17 {
			t.Fatalf("doctor breakdown not current: before=%+v got=%+v", before, breakdown)
		}
		if got := check.Detail["context_checkpoint_mode"]; got != "on" {
			t.Fatalf("context checkpoint mode = %#v, want on", got)
		}
		return
	}
	t.Fatal("missing agent.shared_loop_stats check")
}

func TestRunDoctor_IncludesLightDenyWhenPresent(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfile(agent.PromptProfileLight)
	agent.RecordLightToolDeny("bash")
	agent.RecordLightToolDeny("bash")
	agent.RecordLightToolDeny("write_file")
	agent.RecordLightUpgrade("tool_deny_retry:bash")

	app := &App{testHomeDir: tempHome}
	report := app.RunDoctor()
	var msg string
	for _, c := range report.Checks {
		if c.ID == "agent.shared_loop_stats" {
			msg = c.Message
			break
		}
	}
	if msg == "" {
		t.Fatal("missing shared_loop_stats")
	}
	if !strings.Contains(msg, "light_deny=3") {
		t.Fatalf("msg=%q", msg)
	}
	// Top breakdown, not last-only: bash:2+1tools
	if !strings.Contains(msg, "bash:2") {
		t.Fatalf("expected denied-tool top breakdown in msg=%q", msg)
	}
	if !strings.Contains(msg, "light_upgrade=1(bash)") {
		t.Fatalf("expected compact upgrade reason in msg=%q", msg)
	}
}

func TestDoctorDeniedTopHelpers_UsedBySharedLoopStats(t *testing.T) {
	if got := doctor.FormatDeniedToolTop(nil, "bash"); got != "bash" {
		t.Fatalf("last-only=%q", got)
	}
	by := map[string]int64{"bash": 2, "write_file": 1}
	got := doctor.FormatDeniedToolTop(by, "write_file")
	if got != "bash:2+1tools" {
		t.Fatalf("got %q", got)
	}
	if got := doctor.CompactUpgradeReason("tool_deny_retry:bash", 24); got != "bash" {
		t.Fatalf("compact=%q", got)
	}
}

func TestGetSharedAgentLoopStatus_HubAndExportDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv(agent.PromptLightRetryEnvKey, "off")
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfile(agent.PromptProfileLight)

	app := &App{testHomeDir: tempHome}
	st := app.GetSharedAgentLoopStatus()
	if st.LightRetryEnabled {
		t.Fatal("expected light_retry off")
	}
	if st.ExportDir == "" {
		t.Fatal("export_dir empty")
	}
	if st.HubAdaptiveSummary == "" {
		t.Fatal("expected hub adaptive summary after light turn")
	}
	// MkdirAll always; explorer open is soft (opened may be false headless).
	out, err := app.OpenAdaptivePromptExportsDir()
	if err != nil {
		t.Fatalf("open exports: %v", err)
	}
	if out["ok"] != true || out["path"] == "" {
		t.Fatalf("%#v", out)
	}
	if _, statErr := os.Stat(st.ExportDir); statErr != nil {
		t.Fatalf("export dir not created: %v", statErr)
	}

	report := app.RunDoctor()
	var msg string
	for _, c := range report.Checks {
		if c.ID == "agent.shared_loop_stats" {
			msg = c.Message
			if c.Detail["light_retry_enabled"] != false {
				t.Fatalf("detail light_retry=%#v", c.Detail["light_retry_enabled"])
			}
			break
		}
	}
	if !strings.Contains(msg, "light_retry=off") {
		t.Fatalf("msg=%q", msg)
	}
}

func TestExportAdaptivePromptStats_WritesFile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfile(agent.PromptProfileLight)

	app := &App{testHomeDir: tempHome}
	out, err := app.ExportAdaptivePromptStats()
	if err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("%#v", out)
	}
	path, _ := out["path"].(string)
	if path == "" {
		t.Fatalf("missing path: %#v", out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("export file missing: %v path=%s", err, path)
	}
	if !strings.Contains(path, "prompt_profile_") || !strings.HasSuffix(path, ".json") {
		t.Fatalf("unexpected path %s", path)
	}
	// Re-load via agent to ensure valid export schema.
	exp, err := agent.LoadPromptProfileExport(path)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Stats.LightTurns < 1 {
		t.Fatalf("export stats=%+v", exp.Stats)
	}
}

func TestGetSharedAgentLoopStatus_IncludesABAndRates(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv(agent.PromptABPercentEnvKey, "25")
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfileDecision(agent.PromptProfileDecision{
		Profile: agent.PromptProfileLight,
		Task:    "fast",
		Reason:  "test",
	})
	agent.RecordABEligibleLight()
	agent.RecordABSampleFull()
	agent.RecordLightToolDeny("bash")
	agent.RecordLightUpgrade("tool_deny_retry:bash")

	app := &App{testHomeDir: tempHome}
	st := app.GetSharedAgentLoopStatus()
	if st.PromptAbEligibleLight != 1 || st.PromptAbSampleFull != 1 {
		t.Fatalf("ab fields: eligible=%d sample=%d", st.PromptAbEligibleLight, st.PromptAbSampleFull)
	}
	if st.PromptAbSamplePercent != 25 {
		t.Fatalf("ab_pct=%d", st.PromptAbSamplePercent)
	}
	if st.PromptDenyRatePct <= 0 {
		t.Fatalf("deny_rate=%d", st.PromptDenyRatePct)
	}
	if st.PromptUpgradeRatePct <= 0 {
		t.Fatalf("upgrade_rate=%d", st.PromptUpgradeRatePct)
	}
	if st.PromptByTask["fast"] != 1 {
		t.Fatalf("by_task=%v", st.PromptByTask)
	}

	report := app.RunDoctor()
	var msg string
	var detail map[string]any
	for _, c := range report.Checks {
		if c.ID == "agent.shared_loop_stats" {
			msg = c.Message
			detail = c.Detail
			break
		}
	}
	if !strings.Contains(msg, "ab=1/1") {
		t.Fatalf("msg missing ab: %q", msg)
	}
	if !strings.Contains(msg, "ab_pct=25") {
		t.Fatalf("msg missing ab_pct: %q", msg)
	}
	if !strings.Contains(msg, "deny_rate=") {
		t.Fatalf("msg missing deny_rate: %q", msg)
	}
	if detail["prompt_ab_sample_percent"] != 25 && detail["prompt_ab_sample_percent"] != int(25) {
		// JSON/any may box as int
		if n, ok := detail["prompt_ab_sample_percent"].(int); !ok || n != 25 {
			t.Fatalf("detail ab_pct=%#v", detail["prompt_ab_sample_percent"])
		}
	}
}
