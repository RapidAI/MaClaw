package main

// SkillExecutionResult 表示 Skill 执行结果。
type SkillExecutionResult struct {
	Success          bool   `json:"success"`
	HasError         bool   `json:"has_error"`
	HasSecAlert      bool   `json:"has_security_alert"`
	OutputQuality    string `json:"output_quality"`               // "none", "basic", "good", "excellent"
	TokensConsumed   int    `json:"tokens_consumed,omitempty"`    // LLM tokens used during execution
	OutputSizeBytes  int    `json:"output_size_bytes,omitempty"`  // total stdout/output bytes across all steps
	DurationMs       int64  `json:"duration_ms,omitempty"`        // execution wall-clock time in milliseconds
	TimeoutMs        int64  `json:"timeout_ms,omitempty"`         // configured timeout (for ratio computation)
	StepTotal        int    `json:"step_total,omitempty"`         // total number of steps
	StepSuccessCount int    `json:"step_success_count,omitempty"` // steps that completed successfully
}

// EvaluateSkillExecution 根据 Skill 执行结果生成评分。
// 安全告警 → -2, 错误 → -1, 无效果 → 0, 成功 → +1, 超预期 → +2
//
// Additional signals (when populated):
// - Empty output (<100 bytes) on success → downgrade to 0
// - Duration > 80% of timeout → downgrade by 1 (dangerously close to timeout)
// - Step success ratio < 50% → downgrade to -1
func EvaluateSkillExecution(result *SkillExecutionResult) int {
	if result == nil {
		return 0
	}
	if result.HasSecAlert {
		return -2
	}
	if result.HasError {
		return -1
	}
	if !result.Success {
		return 0
	}

	// Base score from output quality.
	baseScore := 0
	switch result.OutputQuality {
	case "excellent":
		baseScore = 2
	case "good":
		baseScore = 1
	case "basic":
		baseScore = 1
	default:
		baseScore = 0
	}

	// Downgrade: success but empty output likely means the skill didn't
	// actually produce meaningful results.
	if result.OutputSizeBytes > 0 && result.OutputSizeBytes < 100 && baseScore > 0 {
		baseScore = 0
	}

	// Downgrade: execution took > 80% of timeout — dangerously slow,
	// may fail on slightly different inputs.
	if result.TimeoutMs > 0 && result.DurationMs > 0 {
		ratio := float64(result.DurationMs) / float64(result.TimeoutMs)
		if ratio > 0.8 && baseScore > 0 {
			baseScore--
		}
	}

	// Downgrade: step success ratio < 50% — even if overall status is
	// "success" (on_error=continue mode), half the steps failed.
	if result.StepTotal > 1 {
		successRatio := float64(result.StepSuccessCount) / float64(result.StepTotal)
		if successRatio < 0.5 {
			baseScore = -1
		}
	}

	return baseScore
}
