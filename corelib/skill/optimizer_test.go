package skill

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestShouldOptimize_BelowMinUsage(t *testing.T) {
	opt := NewSkillOptimizer(nil, nil, nil)
	skill := &corelib.NLSkillEntry{UsageCount: 5, SuccessCount: 3}
	records := []SkillUsageRecord{{FollowUp: "retry"}, {FollowUp: "retry"}}
	if opt.ShouldOptimize(skill, records) {
		t.Error("should not optimize: usage < 8")
	}
}

func TestShouldOptimize_TooHighSuccessRate(t *testing.T) {
	opt := NewSkillOptimizer(nil, nil, nil)
	skill := &corelib.NLSkillEntry{UsageCount: 10, SuccessCount: 9}
	records := []SkillUsageRecord{{FollowUp: "retry"}, {FollowUp: "retry"}}
	if opt.ShouldOptimize(skill, records) {
		t.Error("should not optimize: success rate > 85%")
	}
}

func TestShouldOptimize_TooLowSuccessRate(t *testing.T) {
	opt := NewSkillOptimizer(nil, nil, nil)
	skill := &corelib.NLSkillEntry{UsageCount: 10, SuccessCount: 4} // 40%
	records := []SkillUsageRecord{{FollowUp: "retry"}, {FollowUp: "retry"}}
	if opt.ShouldOptimize(skill, records) {
		t.Error("should not optimize: success rate < 50% (should go to repair instead)")
	}
}

func TestShouldOptimize_InRange_WithRetryEvidence(t *testing.T) {
	opt := NewSkillOptimizer(nil, nil, nil)
	skill := &corelib.NLSkillEntry{UsageCount: 10, SuccessCount: 7} // 70%
	records := []SkillUsageRecord{
		{FollowUp: "continue", Success: true},
		{FollowUp: "retry", Success: false},
		{FollowUp: "abandon", Success: false},
	}
	if !opt.ShouldOptimize(skill, records) {
		t.Error("should optimize: success rate 70% with 2 retry/abandon records")
	}
}

func TestShouldOptimize_InRange_InsufficientRetryEvidence(t *testing.T) {
	opt := NewSkillOptimizer(nil, nil, nil)
	skill := &corelib.NLSkillEntry{UsageCount: 10, SuccessCount: 7}
	records := []SkillUsageRecord{
		{FollowUp: "continue", Success: true},
		{FollowUp: "retry", Success: false}, // only 1 retry
	}
	if opt.ShouldOptimize(skill, records) {
		t.Error("should not optimize: only 1 retry (need >= 2)")
	}
}

func TestShouldOptimize_Cooldown(t *testing.T) {
	opt := NewSkillOptimizer(nil, nil, nil)
	skill := &corelib.NLSkillEntry{
		UsageCount:      10,
		SuccessCount:    7,
		LastOptimizedAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339), // 1 hour ago
	}
	records := []SkillUsageRecord{{FollowUp: "retry"}, {FollowUp: "abandon"}}
	if opt.ShouldOptimize(skill, records) {
		t.Error("should not optimize: within 24h cooldown")
	}
}

func TestShouldOptimize_FileBacked(t *testing.T) {
	opt := NewSkillOptimizer(nil, nil, nil)
	skill := &corelib.NLSkillEntry{
		UsageCount:   10,
		SuccessCount: 7,
		Source:       "file",
		SkillDir:     "/some/dir",
	}
	records := []SkillUsageRecord{{FollowUp: "retry"}, {FollowUp: "abandon"}}
	if opt.ShouldOptimize(skill, records) {
		t.Error("should not optimize: file-backed skill")
	}
}

func TestApplyOptimization_UpdatesMetadata(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name:        "test-skill",
		Description: "old description",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo old"}},
		},
	}

	result := &OptimizationResult{
		Optimized: true,
		NewSteps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo new"}},
		},
		NewDesc:     "improved description",
		Explanation: "adjusted command param",
	}

	modified := ApplyOptimization(skill, result, nil)
	if !modified {
		t.Fatal("expected modification")
	}
	if skill.OptimizationCount != 1 {
		t.Errorf("OptimizationCount = %d, want 1", skill.OptimizationCount)
	}
	if skill.LastOptimizedAt == "" {
		t.Error("LastOptimizedAt should be set")
	}
	if skill.Description != "improved description" {
		t.Errorf("Description = %q, want %q", skill.Description, "improved description")
	}
	cmd, _ := skill.Steps[0].Params["command"].(string)
	if cmd != "echo new" {
		t.Errorf("step command = %q, want %q", cmd, "echo new")
	}
}

func TestApplyOptimization_NotOptimized_NoChange(t *testing.T) {
	skill := &corelib.NLSkillEntry{Name: "test"}
	result := &OptimizationResult{Optimized: false}
	if ApplyOptimization(skill, result, nil) {
		t.Error("should not modify when Optimized=false")
	}
}

// TestWriteBackOptimizedStepsPreservesNameAndCondition covers the field-loss
// fix: WriteBackOptimizedSteps must round-trip name/condition (alongside
// label/when/capture) instead of silently dropping them from skill.yaml.
func TestWriteBackOptimizedStepsPreservesNameAndCondition(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: cond-skill
description: keeps me
triggers:
  - run it
steps:
  - action: bash
    params:
      command: echo old
`
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:        "cond-skill",
		Description: "keeps me",
		SkillDir:    dir,
		Steps: []corelib.NLSkillStep{
			{
				Action:    "bash",
				Params:    map[string]interface{}{"command": "echo new"},
				OnError:   "continue",
				Name:      "first step",
				Condition: "on_failure",
				Label:     "step-one",
				When:      "{{op}} == run",
				Capture:   map[string]string{"out": "(.*)"},
			},
		},
	}
	if err := WriteBackOptimizedSteps(entry); err != nil {
		t.Fatalf("WriteBackOptimizedSteps() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("re-parse written skill.yaml: %v", err)
	}
	if len(sf.Steps) != 1 {
		t.Fatalf("steps = %+v, want 1", sf.Steps)
	}
	got := sf.Steps[0]
	if got.Name != "first step" {
		t.Errorf("name lost on write-back: %+v", got)
	}
	if got.Condition != "on_failure" {
		t.Errorf("condition lost on write-back: %+v", got)
	}
	if got.Label != "step-one" || got.When != "{{op}} == run" || got.Capture["out"] != "(.*)" {
		t.Errorf("label/when/capture regressed: %+v", got)
	}
	// Non-steps sections must survive the write-back.
	if sf.Description != "keeps me" || len(sf.Triggers) != 1 || sf.Triggers[0] != "run it" {
		t.Errorf("non-steps fields lost: desc=%q triggers=%v", sf.Description, sf.Triggers)
	}
}

// TestLoadSkillStepsFromDirReadsFreshDiskState covers the single-dir parse
// entry used by the repair-draft TOCTOU check: it must bypass caches and see
// hand edits immediately.
func TestLoadSkillStepsFromDirReadsFreshDiskState(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	write := func(cmd string) {
		yaml := "name: s\ndescription: d\nsteps:\n  - action: bash\n    params:\n      command: " + cmd + "\n"
		if err := os.WriteFile(yamlPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("echo one")
	steps, err := LoadSkillStepsFromDir(dir)
	if err != nil {
		t.Fatalf("LoadSkillStepsFromDir() error = %v", err)
	}
	if len(steps) != 1 || steps[0].Params["command"] != "echo one" {
		t.Fatalf("steps = %+v", steps)
	}
	// A hand edit is visible on the very next call (no cache).
	write("echo two")
	steps, err = LoadSkillStepsFromDir(dir)
	if err != nil {
		t.Fatalf("LoadSkillStepsFromDir() after edit error = %v", err)
	}
	if len(steps) != 1 || steps[0].Params["command"] != "echo two" {
		t.Fatalf("stale steps after edit: %+v", steps)
	}
	if _, err := LoadSkillStepsFromDir(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected error for dir without skill.yaml")
	}
}
