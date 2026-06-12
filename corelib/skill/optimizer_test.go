package skill

import (
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
