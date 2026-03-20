package main

// SkillExecutionResult 表示 Skill 执行结果。
type SkillExecutionResult struct {
	Success       bool   `json:"success"`
	HasError      bool   `json:"has_error"`
	HasSecAlert   bool   `json:"has_security_alert"`
	OutputQuality string `json:"output_quality"` // "none", "basic", "good", "excellent"
}

// EvaluateSkillExecution 根据 Skill 执行结果生成评分。
// 安全告警 → -2, 错误 → -1, 无效果 → 0, 成功 → +1, 超预期 → +2
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
	switch result.OutputQuality {
	case "excellent":
		return 2
	case "good":
		return 1
	case "basic":
		return 1
	default:
		return 0
	}
}
