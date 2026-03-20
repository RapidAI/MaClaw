package main

import "testing"

// ── Task 17.3: Evaluator 单元测试 ───────────────────────────────────────

func TestEvaluateSkillExecution_SecurityAlert(t *testing.T) {
	result := &SkillExecutionResult{HasSecAlert: true}
	if score := EvaluateSkillExecution(result); score != -2 {
		t.Errorf("security alert: got %d, want -2", score)
	}
}

func TestEvaluateSkillExecution_Error(t *testing.T) {
	result := &SkillExecutionResult{HasError: true}
	if score := EvaluateSkillExecution(result); score != -1 {
		t.Errorf("error: got %d, want -1", score)
	}
}

func TestEvaluateSkillExecution_NoEffect(t *testing.T) {
	result := &SkillExecutionResult{Success: false}
	if score := EvaluateSkillExecution(result); score != 0 {
		t.Errorf("no effect: got %d, want 0", score)
	}
}

func TestEvaluateSkillExecution_SuccessBasic(t *testing.T) {
	result := &SkillExecutionResult{Success: true, OutputQuality: "basic"}
	if score := EvaluateSkillExecution(result); score != 1 {
		t.Errorf("basic success: got %d, want 1", score)
	}
}

func TestEvaluateSkillExecution_SuccessGood(t *testing.T) {
	result := &SkillExecutionResult{Success: true, OutputQuality: "good"}
	if score := EvaluateSkillExecution(result); score != 1 {
		t.Errorf("good success: got %d, want 1", score)
	}
}

func TestEvaluateSkillExecution_Excellent(t *testing.T) {
	result := &SkillExecutionResult{Success: true, OutputQuality: "excellent"}
	if score := EvaluateSkillExecution(result); score != 2 {
		t.Errorf("excellent: got %d, want 2", score)
	}
}

func TestEvaluateSkillExecution_Nil(t *testing.T) {
	if score := EvaluateSkillExecution(nil); score != 0 {
		t.Errorf("nil: got %d, want 0", score)
	}
}

func TestEvaluateSkillExecution_SuccessNoQuality(t *testing.T) {
	result := &SkillExecutionResult{Success: true, OutputQuality: "none"}
	if score := EvaluateSkillExecution(result); score != 0 {
		t.Errorf("success/none: got %d, want 0", score)
	}
}

func TestEvaluateSkillExecution_SecurityOverridesError(t *testing.T) {
	// 安全告警优先级高于错误
	result := &SkillExecutionResult{HasSecAlert: true, HasError: true}
	if score := EvaluateSkillExecution(result); score != -2 {
		t.Errorf("sec+error: got %d, want -2", score)
	}
}
