package skill

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestWriteRepairDraftAtomicWrite(t *testing.T) {
	skillDir := t.TempDir()
	draft := RepairDraft{
		Skill:       "s",
		NewSteps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
		Explanation: "fix",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	name, err := WriteRepairDraft(skillDir, draft)
	if err != nil {
		t.Fatalf("WriteRepairDraft() error = %v", err)
	}
	draftsDir := filepath.Join(skillDir, RepairDraftsDirName)

	// Draft file exists and parses back.
	data, err := os.ReadFile(filepath.Join(draftsDir, name))
	if err != nil {
		t.Fatalf("read draft error = %v", err)
	}
	var got RepairDraft
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("draft does not parse: %v", err)
	}
	if got.Skill != "s" || len(got.NewSteps) != 1 {
		t.Fatalf("draft round-trip = %#v", got)
	}

	// No *.tmp residue: the atomic write must rename its temp file away.
	entries, err := os.ReadDir(draftsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteRepairDraftStatErrorReturns(t *testing.T) {
	skillDir := t.TempDir()
	statErr := errors.New("simulated stat failure")
	orig := statRepairDraftPath
	statRepairDraftPath = func(string) (os.FileInfo, error) { return nil, statErr }
	defer func() { statRepairDraftPath = orig }()

	// A non-IsNotExist stat error must surface instead of spinning forever in
	// the collision-suffix loop.
	if _, err := WriteRepairDraft(skillDir, RepairDraft{Skill: "s"}); !errors.Is(err, statErr) {
		t.Fatalf("WriteRepairDraft() error = %v, want %v", err, statErr)
	}
}

// TestTryFileBackedRepairDraftDisableSuggestion covers the disable-suggestion
// draft: repaired:false + should_disable:true must produce a Disable draft
// (empty steps, explanation = disable rationale) instead of being swallowed.
func TestTryFileBackedRepairDraftDisableSuggestion(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: file-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name: "file-skill", Source: "file", SkillDir: skillDir, Status: "active",
		UsageCount: 5, SuccessCount: 0, FailureCount: 5,
		LastError: "[class: command_not_found] missing foo",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}

	var llmCalls atomic.Int32
	p := NewEvolutionPipeline()
	p.RepairCooldown = time.Millisecond
	p.LLM = &stubRepairLLM{
		respond: `{"repaired":false,"explanation":"tool permanently removed upstream","new_steps":[],"should_disable":true}`,
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

	if llmCalls.Load() != 1 {
		t.Fatalf("llm calls = %d, want 1", llmCalls.Load())
	}
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
	if !draft.Disable {
		t.Fatalf("draft.Disable = false, want true: %#v", draft)
	}
	if len(draft.NewSteps) != 0 || len(draft.OldSteps) != 0 {
		t.Fatalf("disable draft must carry no steps: %#v", draft)
	}
	if draft.Explanation != "tool permanently removed upstream" {
		t.Fatalf("draft.Explanation = %q", draft.Explanation)
	}
	if draft.LastError != entry.LastError {
		t.Fatalf("draft.LastError = %q", draft.LastError)
	}
	if emitted == nil || emitted["skill"] != "file-skill" || emitted["draft"] != files[0].Name() {
		t.Fatalf("event payload = %#v, draft file = %s", emitted, files[0].Name())
	}
	// draft 落盘成功消耗冷却（与普通 draft 一致）。
	if !repairCooldownRecorded(p, "file-skill") {
		t.Fatal("cooldown not recorded after disable draft written")
	}
	// entry 不被修改。
	if entry.Status != "active" || entry.RepairAttemptCount != 0 {
		t.Fatalf("entry mutated: %#v", entry)
	}
}

// TestTryFileBackedRepairDraftSkipsSkillMDOnly covers the SKILL.md-only skip:
// without skill.yaml/skill.yml no draft can ever be applied, so the pipeline
// must skip before the LLM call — no LLM cost, no cooldown, no draft.
func TestTryFileBackedRepairDraftSkipsSkillMDOnly(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# md-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name: "md-skill", Source: "file", SkillDir: skillDir, Status: "active",
		UsageCount: 5, SuccessCount: 0, FailureCount: 5,
		LastError: "[class: command_not_found] missing foo",
		Steps:     []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
	}

	var llmCalls atomic.Int32
	p := NewEvolutionPipeline()
	p.RepairCooldown = time.Millisecond
	p.LLM = &stubRepairLLM{
		respond: `{"repaired":true,"explanation":"fix","new_steps":[{"action":"bash","params":{"command":"echo ok"}}],"should_disable":false}`,
		onCall:  func([]map[string]string) { llmCalls.Add(1) },
	}
	req := evolutionRequest{
		SkillName:  "md-skill",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	}

	p.tryRepair(context.Background(), req)

	if llmCalls.Load() != 0 {
		t.Fatalf("SKILL.md-only skill must not burn LLM calls, got %d", llmCalls.Load())
	}
	if HasPendingRepairDraft(filepath.Join(skillDir, RepairDraftsDirName)) {
		t.Fatal("no draft may be written for a SKILL.md-only skill")
	}
	if repairCooldownRecorded(p, "md-skill") {
		t.Fatal("cooldown must not be consumed when skipping before the LLM call")
	}
}

// Skills whose steps carry poll/loop configs are skipped before the LLM call:
// WriteBackOptimizedSteps cannot round-trip those configs, so applying a
// draft would silently strip them.
func TestTryFileBackedRepairDraftSkipsPollLoopSkills(t *testing.T) {
	skillDir := t.TempDir()
	yamlContent := "name: poll-skill\ndescription: d\nsteps:\n  - action: bash\n    params:\n      command: foo\n    poll:\n      until: done\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name: "poll-skill", Source: "file", SkillDir: skillDir, Status: "active",
		UsageCount: 5, SuccessCount: 0, FailureCount: 5,
		LastError: "[class: command_not_found] missing foo",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "foo"}, Poll: &corelib.StepPollConfig{}},
		},
	}

	var llmCalls atomic.Int32
	p := NewEvolutionPipeline()
	p.RepairCooldown = time.Millisecond
	p.LLM = &stubRepairLLM{
		respond: `{"repaired":true,"explanation":"fix","new_steps":[{"action":"bash","params":{"command":"echo ok"}}],"should_disable":false}`,
		onCall:  func([]map[string]string) { llmCalls.Add(1) },
	}
	req := evolutionRequest{
		SkillName:  "poll-skill",
		Entry:      entry,
		ExecResult: &SkillExecutionResultCompat{Success: false},
	}

	p.tryRepair(context.Background(), req)

	if llmCalls.Load() != 0 {
		t.Fatalf("poll/loop skill must not burn LLM calls, got %d", llmCalls.Load())
	}
	if HasPendingRepairDraft(filepath.Join(skillDir, RepairDraftsDirName)) {
		t.Fatal("no draft may be written for a poll/loop skill")
	}
	if repairCooldownRecorded(p, "poll-skill") {
		t.Fatal("cooldown must not be consumed when skipping before the LLM call")
	}
}

// LLM-produced repair steps must keep the flat optional fields
// (name/condition/when/label/capture) through the NLSkillStep conversion —
// dropping them would silently strip those configs on apply, the same hazard
// class as the poll/loop gate.
func TestConvertRepairResultStepsPreservesFlatFields(t *testing.T) {
	in := []SkillYAMLStep{
		{
			Action:    "bash",
			Params:    map[string]interface{}{"command": "echo hi"},
			OnError:   "continue",
			Name:      "greet",
			Condition: "os == 'windows'",
			When:      "prev.ok",
			Label:     "main",
			Capture:   map[string]string{"out": "(.*)"},
		},
		{Action: "read_file", Params: map[string]interface{}{"path": "a.txt"}},
	}
	out := convertRepairResultSteps(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	s := out[0]
	if s.Name != "greet" || s.Condition != "os == 'windows'" || s.When != "prev.ok" || s.Label != "main" {
		t.Fatalf("flat fields not preserved: %+v", s)
	}
	if s.Capture["out"] != "(.*)" {
		t.Fatalf("capture not preserved: %+v", s.Capture)
	}
	if s.Poll != nil || s.Loop != nil {
		t.Fatal("poll/loop must not be carried (gated upstream)")
	}
	if out[1].Name != "" || out[1].Capture != nil {
		t.Fatalf("zero-value fields must stay zero: %+v", out[1])
	}
}
