package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// writeDisableDraftSkill creates a file-backed skill (skill.yaml on disk) plus
// one pending "disable suggestion" draft and returns the config entry.
func writeDisableDraftSkill(t *testing.T, root, name, explanation string) (corelib.NLSkillEntry, string) {
	t.Helper()
	skillDir := filepath.Join(root, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	yaml := "name: " + name + "\ndescription: test skill\nsteps:\n  - action: bash\n    params:\n      command: echo old\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write skill.yaml error = %v", err)
	}
	draft := cskill.RepairDraft{
		Skill:       name,
		Explanation: explanation,
		LastError:   "[class: command_not_found] gone",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Disable:     true,
	}
	draftName, err := cskill.WriteRepairDraft(skillDir, draft)
	if err != nil {
		t.Fatalf("WriteRepairDraft() error = %v", err)
	}
	entry := corelib.NLSkillEntry{
		Name:     name,
		Status:   "active",
		Source:   "file",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo old"}},
		},
	}
	return entry, draftName
}

func findOverlay(t *testing.T, app *App, name string) *corelib.NLSkillEntry {
	t.Helper()
	for i, s := range app.PeekNLSkills() {
		if s.Name == name {
			return &app.PeekNLSkills()[i]
		}
	}
	return nil
}

// TestApplySkillRepairDraftDisable covers the disable-suggestion apply: no
// yaml write-back, status flips to disabled, counter + history recorded with
// via="reviewed_draft_disable", LastError becomes an auto-disabled marker and
// the draft is removed.
func TestApplySkillRepairDraftDisable(t *testing.T) {
	entry, draftName := writeDisableDraftSkill(t, t.TempDir(), "disable-skill", "tool permanently removed upstream")
	yamlBefore, err := os.ReadFile(filepath.Join(entry.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	parsed := applyDraftResult(t, app, "disable-skill", draftName)
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("disable apply failed: %v", parsed["error"])
	}
	if disabled, _ := parsed["disabled"].(bool); !disabled {
		t.Fatalf("response missing disabled=true: %v", parsed)
	}

	// skill.yaml untouched (no step write-back on the disable path).
	yamlAfter, err := os.ReadFile(filepath.Join(entry.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(yamlBefore) != string(yamlAfter) {
		t.Fatalf("skill.yaml must not change on disable apply:\n%s", yamlAfter)
	}

	overlay := findOverlay(t, app, "disable-skill")
	if overlay == nil {
		t.Fatal("overlay missing after disable apply")
	}
	if overlay.Status != "disabled" {
		t.Fatalf("overlay.Status = %q, want disabled", overlay.Status)
	}
	if overlay.RepairAttemptCount != 1 || overlay.LastRepairAt == "" {
		t.Fatalf("repair counter not recorded: count=%d at=%q", overlay.RepairAttemptCount, overlay.LastRepairAt)
	}
	if len(overlay.RepairHistory) != 1 {
		t.Fatalf("repair history = %#v, want 1 record", overlay.RepairHistory)
	}
	rec := overlay.RepairHistory[0]
	if rec.Via != "reviewed_draft_disable" || !rec.Success || rec.Explanation != "tool permanently removed upstream" {
		t.Fatalf("repair history record = %+v", rec)
	}
	if !strings.HasPrefix(overlay.LastError, "auto-disabled: ") ||
		!strings.Contains(overlay.LastError, "tool permanently removed upstream") {
		t.Fatalf("LastError marker = %q", overlay.LastError)
	}

	// Draft removed.
	if cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft still pending after disable apply")
	}
}

// TestApplySkillRepairDraftTOCTOUConflict covers the optimistic concurrency
// check: when the skill's steps changed on disk after the draft was
// generated, apply must refuse (keeping the draft) instead of silently
// overwriting the user's edits.
func TestApplySkillRepairDraftTOCTOUConflict(t *testing.T) {
	root := t.TempDir()
	entry, draftName := writeFileBackedSkillWithDraft(t, root)
	// User hand-edits the skill after the draft was generated.
	yaml := "name: draft-skill\ndescription: test skill\nsteps:\n  - action: bash\n    params:\n      command: echo hand-edited\n"
	if err := os.WriteFile(filepath.Join(entry.SkillDir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	// Register the temp root as an external skill dir so loadSkills hydrates
	// steps from the edited on-disk skill.yaml (the production layout).
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, func(cfg *corelib.AppConfig) {
		cfg.ExternalSkillDirs = []string{root}
	})

	parsed := applyDraftResult(t, app, "draft-skill", draftName)
	if ok, _ := parsed["ok"].(bool); ok {
		t.Fatalf("apply unexpectedly succeeded: %v", parsed)
	}
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "modified after the draft was generated") {
		t.Fatalf("error should mention the concurrent modification, got %q", errMsg)
	}
	if !cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft must be kept on a TOCTOU conflict")
	}
	// The user's edit must survive, and no repair counter may be recorded.
	yamlData, err := os.ReadFile(filepath.Join(entry.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(yamlData), "echo hand-edited") {
		t.Fatalf("user edit was overwritten:\n%s", yamlData)
	}
	for _, s := range app.PeekNLSkills() {
		if s.Name == "draft-skill" && s.RepairAttemptCount != 0 {
			t.Fatalf("repair counter advanced on refused apply: %+v", s)
		}
	}
}

// TestRejectSkillRepairDraftCountsAndAudits covers reject accounting: the
// attempt counter and a via="reviewed_draft_rejected" history row are
// recorded (so max_attempts gates the regenerate → reject loop), plus a
// repair_draft audit row with status="rejected".
func TestRejectSkillRepairDraftCountsAndAudits(t *testing.T) {
	entry, draftName := writeDisableDraftSkill(t, t.TempDir(), "reject-skill", "not worth fixing")
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	res := app.RejectSkillRepairDraft("reject-skill", draftName)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(res), &parsed); err != nil {
		t.Fatalf("invalid JSON response %q", res)
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("reject failed: %v", parsed["error"])
	}
	if cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft still pending after reject")
	}

	overlay := findOverlay(t, app, "reject-skill")
	if overlay == nil {
		t.Fatal("overlay missing after reject (counter should make it persist)")
	}
	if overlay.RepairAttemptCount != 1 || overlay.LastRepairAt == "" {
		t.Fatalf("reject must count as an attempt: count=%d at=%q", overlay.RepairAttemptCount, overlay.LastRepairAt)
	}
	if len(overlay.RepairHistory) != 1 {
		t.Fatalf("repair history = %#v, want 1 record", overlay.RepairHistory)
	}
	rec := overlay.RepairHistory[0]
	if rec.Via != "reviewed_draft_rejected" || rec.Success || rec.Explanation != "not worth fixing" {
		t.Fatalf("repair history record = %+v", rec)
	}
	// A rejection is not a repair artifact: no LastError marker.
	if strings.HasPrefix(overlay.LastError, "auto-repaired:") || strings.HasPrefix(overlay.LastError, "auto-disabled:") {
		t.Fatalf("rejected draft must not set a repair marker, LastError = %q", overlay.LastError)
	}

	// Audit row: kind repair_draft for this skill.
	events, err := cskill.ListEvolutionAudit(cskill.DefaultEvolutionAuditPath(), cskill.EvolutionAuditMaxKeep)
	if err != nil {
		t.Fatalf("ListEvolutionAudit() error = %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "repair_draft" && ev.Skill == "reject-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no repair_draft audit row recorded for the rejection")
	}
}

// TestRejectSkillRepairDraftUnreadableCountsOnly covers rejecting a corrupt
// draft: it must still go through (counted + deleted + audited), only the
// history row is skipped because the draft content is unavailable.
func TestRejectSkillRepairDraftUnreadableCountsOnly(t *testing.T) {
	entry, _ := writeDisableDraftSkill(t, t.TempDir(), "reject-corrupt-skill", "x")
	draftsDir := filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)
	if err := os.WriteFile(filepath.Join(draftsDir, "20990103T000000.000000000Z.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	res := app.RejectSkillRepairDraft("reject-corrupt-skill", "20990103T000000.000000000Z.json")
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(res), &parsed); err != nil {
		t.Fatalf("invalid JSON response %q", res)
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("reject unreadable draft failed: %v", parsed["error"])
	}
	if _, err := os.Stat(filepath.Join(draftsDir, "20990103T000000.000000000Z.json")); !os.IsNotExist(err) {
		t.Fatal("corrupt draft file must be deleted")
	}

	overlay := findOverlay(t, app, "reject-corrupt-skill")
	if overlay == nil {
		t.Fatal("overlay missing after unreadable reject")
	}
	if overlay.RepairAttemptCount != 1 {
		t.Fatalf("unreadable reject must still count: count=%d", overlay.RepairAttemptCount)
	}
	if len(overlay.RepairHistory) != 0 {
		t.Fatalf("unreadable reject must skip history, got %#v", overlay.RepairHistory)
	}
}

// TestApplySkillRepairDraftTOCTOUConflictThroughStaleCache covers the round-3
// TOCTOU fix: the concurrency check must re-parse skill.yaml from disk, not
// trust loadSkills (two-layer 10-minute cache). Here the config entry (what
// the cache would serve) still holds the OLD steps while the user hand-edited
// the yaml on disk — apply must refuse and keep both the draft and the edit.
func TestApplySkillRepairDraftTOCTOUConflictThroughStaleCache(t *testing.T) {
	entry, draftName := writeFileBackedSkillWithDraft(t, t.TempDir())
	// User hand-edits the skill after the draft was generated. No
	// ExternalSkillDirs registration: loadSkills never re-hydrates from this
	// temp dir, so entry.Steps (the cache view) stays at "echo old" — exactly
	// the stale-cache scenario the fix targets.
	yaml := "name: draft-skill\ndescription: test skill\nsteps:\n  - action: bash\n    params:\n      command: echo hand-edited\n"
	if err := os.WriteFile(filepath.Join(entry.SkillDir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	parsed := applyDraftResult(t, app, "draft-skill", draftName)
	if ok, _ := parsed["ok"].(bool); ok {
		t.Fatalf("apply unexpectedly succeeded over a stale cache: %v", parsed)
	}
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "modified after the draft was generated") {
		t.Fatalf("error should mention the concurrent modification, got %q", errMsg)
	}
	if !cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft must be kept on a TOCTOU conflict")
	}
	yamlData, err := os.ReadFile(filepath.Join(entry.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(yamlData), "echo hand-edited") {
		t.Fatalf("user edit was overwritten:\n%s", yamlData)
	}
}

// TestApplySkillRepairDraftRollsBackYAMLOnConfigSaveFailure covers the round-3
// partial-failure fix: when the yaml write-back succeeded but persisting the
// config overlay fails, apply must roll skill.yaml back to the pre-apply
// steps — otherwise the kept draft could never be retried (the next apply's
// TOCTOU check would see new_steps on disk vs old_steps in the draft).
func TestApplySkillRepairDraftRollsBackYAMLOnConfigSaveFailure(t *testing.T) {
	entry, draftName := writeFileBackedSkillWithDraft(t, t.TempDir())
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	orig := saveRepairDraftSkills
	saveRepairDraftSkills = func(exec *SkillExecutor, skills []corelib.NLSkillEntry) error {
		return errors.New("injected save failure")
	}
	t.Cleanup(func() { saveRepairDraftSkills = orig })

	parsed := applyDraftResult(t, app, "draft-skill", draftName)
	if ok, _ := parsed["ok"].(bool); ok {
		t.Fatalf("apply unexpectedly succeeded: %v", parsed)
	}
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "save config failed") || !strings.Contains(errMsg, "rolled back") {
		t.Fatalf("error should report the save failure and the rollback, got %q", errMsg)
	}
	if !cskill.HasPendingRepairDraft(filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft must be kept when the config save fails")
	}
	// skill.yaml rolled back to the pre-apply steps (retry stays possible).
	yamlData, err := os.ReadFile(filepath.Join(entry.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(yamlData), "echo old") || strings.Contains(string(yamlData), "echo new") {
		t.Fatalf("skill.yaml not rolled back:\n%s", yamlData)
	}
	// No repair counter may be recorded on a failed apply.
	for _, s := range app.PeekNLSkills() {
		if s.Name == "draft-skill" && s.RepairAttemptCount != 0 {
			t.Fatalf("repair counter advanced on failed apply: %+v", s)
		}
	}

	// Retry with the save hook restored: the draft applies cleanly, proving
	// the rollback left the draft applicable again.
	saveRepairDraftSkills = orig
	parsed = applyDraftResult(t, app, "draft-skill", draftName)
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("retry after rollback failed: %v", parsed["error"])
	}
	yamlData, err = os.ReadFile(filepath.Join(entry.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(yamlData), "echo new") {
		t.Fatalf("skill.yaml not written back on retry:\n%s", yamlData)
	}
}

// TestRepairDraftStepViewsIncludeName covers the round-3 frontend contract
// addition: the step view must carry `name` alongside label/when/capture/
// condition so reviewers see every field apply will write back.
func TestRepairDraftStepViewsIncludeName(t *testing.T) {
	views := repairDraftStepViews([]corelib.NLSkillStep{{
		Action:    "bash",
		Params:    map[string]interface{}{"command": "echo hi"},
		OnError:   "continue",
		Name:      "first step",
		Condition: "on_failure",
		Label:     "step-one",
	}})
	if len(views) != 1 {
		t.Fatalf("views = %+v", views)
	}
	data, err := json.Marshal(views[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["name"] != "first step" {
		t.Fatalf("name missing from step view: %s", data)
	}
	if decoded["label"] != "step-one" || decoded["condition"] != "on_failure" {
		t.Fatalf("label/condition regressed: %s", data)
	}
}

// TestApplySkillRepairDraftRejectsPollLoop covers the poll/loop hard gate:
// WriteBackOptimizedSteps cannot round-trip poll/loop configs, so apply must
// refuse (keeping draft and yaml untouched) instead of silently stripping
// them. The pipeline already skips such skills at generation; this gate is
// the backstop for hand-written or older drafts.
func TestApplySkillRepairDraftRejectsPollLoop(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "poll-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "name: poll-skill\ndescription: test skill\nsteps:\n  - action: http\n    params:\n      url: http://example.com\n    poll:\n      until: done\n      interval_sec: 5\n"
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if err := os.WriteFile(yamlPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	// No old_steps: skips the TOCTOU check so the poll/loop gate is what fires.
	draft := cskill.RepairDraft{
		Skill:       "poll-skill",
		NewSteps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo new"}}},
		Explanation: "fix",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	draftName, err := cskill.WriteRepairDraft(skillDir, draft)
	if err != nil {
		t.Fatal(err)
	}
	entry := corelib.NLSkillEntry{
		Name:     "poll-skill",
		Status:   "active",
		Source:   "file",
		SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{
			{Action: "http", Params: map[string]interface{}{"url": "http://example.com"}, Poll: &corelib.StepPollConfig{}},
		},
	}
	app := setupEvolutionTestApp(t, []corelib.NLSkillEntry{entry}, nil)

	parsed := applyDraftResult(t, app, "poll-skill", draftName)
	if ok, _ := parsed["ok"].(bool); ok {
		t.Fatalf("poll/loop apply must be rejected: %v", parsed)
	}
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "poll/loop") {
		t.Fatalf("error should mention poll/loop, got %q", errMsg)
	}
	if !cskill.HasPendingRepairDraft(filepath.Join(skillDir, cskill.RepairDraftsDirName)) {
		t.Fatal("draft must be kept when apply is rejected")
	}
	yamlAfter, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(yamlAfter) != yaml {
		t.Fatalf("skill.yaml must not change on rejected apply:\n%s", yamlAfter)
	}
	if overlay := findOverlay(t, app, "poll-skill"); overlay != nil && overlay.RepairAttemptCount != 0 {
		t.Fatalf("repair counter must not move on rejected apply, got %d", overlay.RepairAttemptCount)
	}
}
