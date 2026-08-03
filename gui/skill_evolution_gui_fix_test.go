package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// setupEvolutionTestApp builds an App backed by a temp home with the given
// skills registered in the config. mutate (optional) adjusts the config
// before the single SaveConfig — NOTE: LoadConfig returns a config with
// NLSkills detached (skills live in a separate atomic snap), so a second
// LoadConfig+SaveConfig round-trip would wipe the skill table.
func setupEvolutionTestApp(t *testing.T, skills []corelib.NLSkillEntry, mutate func(*corelib.AppConfig)) *App {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "")

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	// Keep the desktop pet off: SaveConfig otherwise spawns real floating
	// windows from async goroutines (and races on the global window handle).
	cfg.PetEnabled = false
	cfg.NLSkills = skills
	if mutate != nil {
		mutate(&cfg)
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	return app
}

// TestExecutePipelineAsyncFailureNotifiesEvolutionPipeline covers P0-1: a
// failed pipeline-type skill run must reach the evolution pipeline (before
// the fix only executeAsync reported, so pipeline skills never evolved).
func TestExecutePipelineAsyncFailureNotifiesEvolutionPipeline(t *testing.T) {
	pipeEntry := corelib.NLSkillEntry{
		Name:   "pipe-skill",
		Status: "active",
		Pipeline: []corelib.SkillPipelineStep{
			{Skill: "definitely-missing-sub-skill"},
		},
	}
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{pipeEntry}, nil)
	runner := NewSkillRunner(app.skillExecutor)
	pipeline := cskill.NewEvolutionPipeline()
	runner.evolutionPipeline = pipeline

	run := &skillRun{
		status: SkillRunStatus{
			RunID:      "run-pipe-fail",
			Skill:      pipeEntry.Name,
			OwnerID:    "owner-1",
			Status:     skillRunStatusRunning,
			StartedAt:  time.Now().Format(time.RFC3339),
			Steps:      make([]StepResult, len(pipeEntry.Pipeline)),
			TotalSteps: len(pipeEntry.Pipeline),
		},
		templateVars: map[string]string{},
		runArgs:      map[string]interface{}{},
		extraEnv:     map[string]string{},
		liveOutput:   newSkillRunLiveOutput(),
	}

	entry := pipeEntry
	runner.executePipelineAsync(context.Background(), run, &entry)

	if run.status.Status != skillRunStatusFailed {
		t.Fatalf("run status = %v, want failed", run.status.Status)
	}
	if got := pipeline.PendingSkillCount(); got != 1 {
		t.Fatalf("pipeline pending count = %d, want 1 (failure must notify evolution pipeline)", got)
	}
}

// setupRepairEligibleRunner builds a runner whose skill is eligible for
// self-repair (hub source, first failure, repairable error class) and whose
// LLM repairer is "configured" (dummy endpoint, never called on these paths).
func setupRepairEligibleRunner(t *testing.T) (*App, *SkillRunner) {
	t.Helper()
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{{
		Name:   "hub-skill",
		Status: "active",
		Source: "hub",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}},
		},
	}}, func(cfg *corelib.AppConfig) {
		// "Configured" LLM (dummy endpoint, never called on these paths) so
		// buildSkillRepairer returns non-nil and canStartRepairSkill can pass.
		cfg.MaclawLLMUrl = "http://127.0.0.1:9"
		cfg.MaclawLLMModel = "test-model"
		cfg.MaclawLLMProtocol = "openai"
		cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
			Name:     "Custom1",
			URL:      "http://127.0.0.1:9",
			Model:    "test-model",
			Protocol: "openai",
			IsCustom: true,
			AuthType: "none",
		}}
		cfg.MaclawLLMCurrentProvider = "Custom1"
	})

	runner := NewSkillRunner(app.skillExecutor)
	// Route repair through a (hook-only) pipeline so no fallback repair
	// goroutine clears the pending marker during the assertion.
	runner.evolutionPipeline = &cskill.EvolutionPipeline{
		EnableRepair: true,
		RepairHook:   func(entry *corelib.NLSkillEntry, runArgs map[string]string) {},
	}
	return app, runner
}

// TestUpdateUsageStatsRespectsEvolutionSwitch covers P0-2: when evolution is
// disabled (env kill switch or config), updateUsageStats must not set the
// self-repair pending marker.
func TestUpdateUsageStatsRespectsEvolutionSwitch(t *testing.T) {
	execErr := os.ErrNotExist

	t.Run("enabled sets pending marker", func(t *testing.T) {
		_, runner := setupRepairEligibleRunner(t)
		skill := &corelib.NLSkillEntry{Name: "hub-skill", Source: "hub"}
		if updated := runner.updateUsageStats(skill, execErr); updated == nil {
			t.Fatal("updateUsageStats returned nil entry")
		}
		if _, ok := runner.repairingSkills.Load("hub-skill"); !ok {
			t.Fatal("expected repairingSkills marker for eligible failed skill when evolution is enabled")
		}
	})

	t.Run("env kill switch skips marker", func(t *testing.T) {
		_, runner := setupRepairEligibleRunner(t)
		t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "1")
		skill := &corelib.NLSkillEntry{Name: "hub-skill", Source: "hub"}
		if updated := runner.updateUsageStats(skill, execErr); updated == nil {
			t.Fatal("updateUsageStats returned nil entry")
		}
		if _, ok := runner.repairingSkills.Load("hub-skill"); ok {
			t.Fatal("repairingSkills marker must not be set when env kill switch disables evolution")
		}
	})

	t.Run("config disabled skips marker", func(t *testing.T) {
		app, runner := setupRepairEligibleRunner(t)
		if _, err := app.PatchConfigFields(map[string]interface{}{"skill_evolution_enabled": false}); err != nil {
			t.Fatalf("PatchConfigFields() error = %v", err)
		}
		skill := &corelib.NLSkillEntry{Name: "hub-skill", Source: "hub"}
		if updated := runner.updateUsageStats(skill, execErr); updated == nil {
			t.Fatal("updateUsageStats returned nil entry")
		}
		if _, ok := runner.repairingSkills.Load("hub-skill"); ok {
			t.Fatal("repairingSkills marker must not be set when skill_evolution_enabled=false")
		}
	})
}

// writeFileBackedSkillWithDraft creates a file-backed skill on disk (skill.yaml
// + one pending repair draft) and returns the config entry.
func writeFileBackedSkillWithDraft(t *testing.T, root string) (corelib.NLSkillEntry, string) {
	t.Helper()
	skillDir := filepath.Join(root, "draft-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	yaml := "name: draft-skill\ndescription: test skill\nsteps:\n  - action: bash\n    params:\n      command: echo old\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write skill.yaml error = %v", err)
	}
	draft := cskill.RepairDraft{
		Skill: "draft-skill",
		OldSteps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo old"}},
		},
		NewSteps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo new"}},
		},
		Explanation: "fix the command",
		LastError:   "exit status 1",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	draftName, err := cskill.WriteRepairDraft(skillDir, draft)
	if err != nil {
		t.Fatalf("WriteRepairDraft() error = %v", err)
	}
	entry := corelib.NLSkillEntry{
		Name:     "draft-skill",
		Status:   "active",
		Source:   "file",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo old"}},
		},
	}
	return entry, draftName
}

// TestApplySkillRepairDraftRejectsPathTraversal covers the P0-4 validation:
// draft names must stay inside <skill_dir>/.evolution-drafts/.
func TestApplySkillRepairDraftRejectsPathTraversal(t *testing.T) {
	entry, draftName := writeFileBackedSkillWithDraft(t, t.TempDir())
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	for _, evil := range []string{"../evil.json", "..\\evil.json", "sub/dir.json", "/abs/evil.json", draftName + "/../x.json"} {
		res := app.ApplySkillRepairDraft("draft-skill", evil)
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(res), &parsed); err != nil {
			t.Fatalf("apply(%q): invalid JSON response %q", evil, res)
		}
		if ok, _ := parsed["ok"].(bool); ok {
			t.Fatalf("apply(%q) unexpectedly succeeded", evil)
		}
		res = app.RejectSkillRepairDraft("draft-skill", evil)
		if err := json.Unmarshal([]byte(res), &parsed); err != nil {
			t.Fatalf("reject(%q): invalid JSON response %q", evil, res)
		}
		if ok, _ := parsed["ok"].(bool); ok {
			t.Fatalf("reject(%q) unexpectedly succeeded", evil)
		}
	}
	// The legitimate draft must still be there (nothing was deleted/applied).
	if !cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("legitimate draft was removed by traversal attempts")
	}
}

// TestApplySkillRepairDraftRoundTrip verifies the happy path: config steps
// updated, skill.yaml written back, draft removed.
func TestApplySkillRepairDraftRoundTrip(t *testing.T) {
	entry, draftName := writeFileBackedSkillWithDraft(t, t.TempDir())
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	res := app.ApplySkillRepairDraft("draft-skill", draftName)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(res), &parsed); err != nil {
		t.Fatalf("invalid JSON response %q", res)
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("apply failed: %v", parsed["error"])
	}

	// Config entry: file-backed skills persist as a thin disk overlay (steps
	// stripped); skill.yaml is the source of truth for their definition.
	// NLSkills is detached from LoadConfig, so read the published snap.
	//
	// Positive assertion: the overlay must exist (repair counter makes it
	// persist) and must carry no steps.
	var overlay *corelib.NLSkillEntry
	for i, s := range app.PeekNLSkills() {
		if s.Name == "draft-skill" {
			overlay = &app.PeekNLSkills()[i]
			break
		}
	}
	if overlay == nil {
		t.Fatal("thin overlay missing after apply (counter should make it persist)")
	}
	if len(overlay.Steps) != 0 {
		t.Fatalf("file-backed overlay must strip steps, got %+v", overlay.Steps)
	}

	// skill.yaml written back.
	yamlData, err := os.ReadFile(filepath.Join(entry.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatalf("read skill.yaml error = %v", err)
	}
	if !strings.Contains(string(yamlData), "echo new") {
		t.Fatalf("skill.yaml not written back:\n%s", yamlData)
	}

	// Draft removed.
	if cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft still pending after apply")
	}

	// Fix 7b: apply counts as a repair attempt so max_attempts also gates the
	// draft flow — the thin overlay must carry the counter and a
	// via="reviewed_draft" history row.
	if overlay.RepairAttemptCount != 1 || overlay.LastRepairAt == "" {
		t.Fatalf("repair counter not recorded: count=%d at=%q", overlay.RepairAttemptCount, overlay.LastRepairAt)
	}
	if len(overlay.RepairHistory) != 1 {
		t.Fatalf("repair history = %#v, want 1 record", overlay.RepairHistory)
	}
	rec := overlay.RepairHistory[0]
	if rec.Via != "reviewed_draft" || !rec.Success || rec.Explanation != "fix the command" {
		t.Fatalf("repair history record = %+v", rec)
	}
	// Round 2 fix 6: LastError becomes a repair artifact marker (aligned with
	// the automatic ApplyRepair path) so queued failure notifications don't
	// regenerate drafts from the stale pre-repair error.
	if overlay.LastError != "auto-repaired: fix the command" {
		t.Fatalf("LastError marker = %q", overlay.LastError)
	}
}

// writeSKILLmdSkillWithDraft creates a SKILL.md-form file-backed skill (no
// skill.yaml) plus one pending repair draft.
func writeSKILLmdSkillWithDraft(t *testing.T, root string) (corelib.NLSkillEntry, string) {
	t.Helper()
	skillDir := filepath.Join(root, "md-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# md-skill\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md error = %v", err)
	}
	draft := cskill.RepairDraft{
		Skill: "md-skill",
		NewSteps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo new"}},
		},
		Explanation: "fix",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	draftName, err := cskill.WriteRepairDraft(skillDir, draft)
	if err != nil {
		t.Fatalf("WriteRepairDraft() error = %v", err)
	}
	entry := corelib.NLSkillEntry{
		Name:     "md-skill",
		Status:   "active",
		Source:   "file",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo old"}},
		},
	}
	return entry, draftName
}

func applyDraftResult(t *testing.T, app *App, skill, draft string) map[string]interface{} {
	t.Helper()
	res := app.ApplySkillRepairDraft(skill, draft)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(res), &parsed); err != nil {
		t.Fatalf("invalid JSON response %q", res)
	}
	return parsed
}

// TestApplySkillRepairDraftRequiresSkillYAML covers fix 6: SKILL.md-form
// skills have no machine-applicable steps file — apply must fail and keep
// the draft (the config overlay alone would silently drop the repair).
func TestApplySkillRepairDraftRequiresSkillYAML(t *testing.T) {
	entry, draftName := writeSKILLmdSkillWithDraft(t, t.TempDir())
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	parsed := applyDraftResult(t, app, "md-skill", draftName)
	if ok, _ := parsed["ok"].(bool); ok {
		t.Fatalf("apply unexpectedly succeeded: %v", parsed)
	}
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "skill.yaml") {
		t.Fatalf("error should mention missing skill.yaml, got %q", errMsg)
	}
	if !cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft must be kept when apply is refused")
	}
}

// TestApplySkillRepairDraftYAMLWriteFailure covers fix 6: when skill.yaml is
// unusable, apply must fail, keep the draft and emit no repaired event. A
// corrupt yaml is now caught even earlier than the write-back — at the
// fresh-disk re-read for the TOCTOU check — which is the same fail-safe path.
func TestApplySkillRepairDraftYAMLWriteFailure(t *testing.T) {
	entry, draftName := writeFileBackedSkillWithDraft(t, t.TempDir())
	// Corrupt skill.yaml so the fresh re-read / write-back fails at parse time.
	if err := os.WriteFile(filepath.Join(entry.SkillDir, "skill.yaml"), []byte(":\tnot yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	parsed := applyDraftResult(t, app, "draft-skill", draftName)
	if ok, _ := parsed["ok"].(bool); ok {
		t.Fatalf("apply unexpectedly succeeded: %v", parsed)
	}
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "skill.yaml") {
		t.Fatalf("error should mention the skill.yaml failure, got %q", errMsg)
	}
	if !cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft must be kept when yaml write-back fails")
	}
	// No repair counter may be recorded on a failed apply.
	for _, s := range app.PeekNLSkills() {
		if s.Name == "draft-skill" && s.RepairAttemptCount != 0 {
			t.Fatalf("repair counter advanced on failed apply: %+v", s)
		}
	}
}

// TestApplySkillRepairDraftRejectsInvalidSteps covers fix 7a: a draft whose
// new_steps violate the GUI runner action whitelist (e.g. hand-edited on
// disk) must be refused and kept for rejection.
func TestApplySkillRepairDraftRejectsInvalidSteps(t *testing.T) {
	entry, draftName := writeFileBackedSkillWithDraft(t, t.TempDir())
	bad := cskill.RepairDraft{
		Skill: "draft-skill",
		NewSteps: []corelib.NLSkillStep{
			{Action: "shell_tool", Params: map[string]interface{}{"command": "echo pwn"}},
		},
		Explanation: "hand-edited",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := cskill.WriteRepairDraft(entry.SkillDir, bad); err != nil {
		t.Fatal(err)
	}
	// Apply the bad draft specifically (list order is not guaranteed).
	badName := ""
	files, err := os.ReadDir(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		d, err := readRepairDraftFile(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName, f.Name()))
		if err == nil && d.Explanation == "hand-edited" {
			badName = f.Name()
		}
	}
	if badName == "" {
		t.Fatal("bad draft not found on disk")
	}
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	parsed := applyDraftResult(t, app, "draft-skill", badName)
	if ok, _ := parsed["ok"].(bool); ok {
		t.Fatalf("apply unexpectedly succeeded: %v", parsed)
	}
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "new_steps invalid") {
		t.Fatalf("error should mention step validation, got %q", errMsg)
	}
	if !cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("invalid draft must be kept so the user can reject it")
	}
	// The good draft must still apply cleanly afterwards.
	parsed = applyDraftResult(t, app, "draft-skill", draftName)
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("good draft apply failed after invalid draft refusal: %v", parsed["error"])
	}
}

// TestListSkillRepairDraftsContract covers fix 8: the list payload is a
// pinned frontend contract — entry-owned skill names, full old/new step
// arrays with raw params, summary fields, and unreadable drafts listed with
// unreadable=true (no steps) so they can still be rejected.
func TestListSkillRepairDraftsContract(t *testing.T) {
	entry, draftName := writeFileBackedSkillWithDraft(t, t.TempDir())
	draftsDir := filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)

	// A draft whose self-declared skill field disagrees with the directory —
	// the directory (entry name) must win.
	spoofed := `{"skill":"spoofed-other","old_steps":[],"new_steps":[{"action":"bash","params":{"command":"echo x"},"on_error":"stop"}],"explanation":"spoof","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(draftsDir, "20990101T000000.000000000Z.json"), []byte(spoofed), 0o644); err != nil {
		t.Fatal(err)
	}
	// An unparseable draft file — must still be listed as unreadable.
	if err := os.WriteFile(filepath.Join(draftsDir, "20990102T000000.000000000Z.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)
	res := app.ListSkillRepairDrafts()
	var payload struct {
		OK     bool `json:"ok"`
		Count  int  `json:"count"`
		Drafts []struct {
			Skill      string `json:"skill"`
			Draft      string `json:"draft"`
			Unreadable bool   `json:"unreadable"`
			OldSteps   []struct {
				Action  string                 `json:"action"`
				Params  map[string]interface{} `json:"params"`
				OnError string                 `json:"on_error"`
			} `json:"old_steps"`
			NewSteps []struct {
				Action  string                 `json:"action"`
				Params  map[string]interface{} `json:"params"`
				OnError string                 `json:"on_error"`
			} `json:"new_steps"`
			OldStepsSummary struct {
				Count   int      `json:"count"`
				Actions []string `json:"actions"`
			} `json:"old_steps_summary"`
			NewStepsSummary struct {
				Count   int      `json:"count"`
				Actions []string `json:"actions"`
			} `json:"new_steps_summary"`
		} `json:"drafts"`
	}
	if err := json.Unmarshal([]byte(res), &payload); err != nil {
		t.Fatalf("invalid JSON response %q", res)
	}
	if !payload.OK || payload.Count != 3 || len(payload.Drafts) != 3 {
		t.Fatalf("payload ok=%v count=%d drafts=%d, want 3 drafts", payload.OK, payload.Count, len(payload.Drafts))
	}

	byDraft := map[string]int{}
	for i, d := range payload.Drafts {
		byDraft[d.Draft] = i
		// skill 一律取 entry 名（目录归属）。
		if d.Skill != "draft-skill" {
			t.Fatalf("draft %s skill = %q, want entry name draft-skill", d.Draft, d.Skill)
		}
	}

	good := payload.Drafts[byDraft[draftName]]
	if good.Unreadable {
		t.Fatal("good draft flagged unreadable")
	}
	if len(good.OldSteps) != 1 || good.OldSteps[0].Action != "bash" ||
		good.OldSteps[0].Params["command"] != "echo old" {
		t.Fatalf("old_steps = %+v", good.OldSteps)
	}
	if len(good.NewSteps) != 1 || good.NewSteps[0].Params["command"] != "echo new" {
		t.Fatalf("new_steps = %+v", good.NewSteps)
	}
	if good.OldStepsSummary.Count != 1 || good.NewStepsSummary.Count != 1 ||
		len(good.NewStepsSummary.Actions) != 1 || good.NewStepsSummary.Actions[0] != "bash" {
		t.Fatalf("summaries = %+v / %+v", good.OldStepsSummary, good.NewStepsSummary)
	}

	spoof := payload.Drafts[byDraft["20990101T000000.000000000Z.json"]]
	if len(spoof.NewSteps) != 1 || spoof.NewSteps[0].OnError != "stop" {
		t.Fatalf("spoofed draft steps = %+v", spoof.NewSteps)
	}

	bad := payload.Drafts[byDraft["20990102T000000.000000000Z.json"]]
	if !bad.Unreadable {
		t.Fatal("corrupt draft must be listed with unreadable=true")
	}
	if len(bad.OldSteps) != 0 || len(bad.NewSteps) != 0 {
		t.Fatalf("unreadable draft must not carry steps: %+v", bad)
	}
	// 不可读 draft 也必须可 reject（用户清理入口）。
	parsed := map[string]interface{}{}
	if err := json.Unmarshal([]byte(app.RejectSkillRepairDraft("draft-skill", "20990102T000000.000000000Z.json")), &parsed); err != nil {
		t.Fatal(err)
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("reject unreadable draft failed: %v", parsed["error"])
	}
}
