package main

import (
	"fmt"
	"time"
)

const (
	defaultWindowSize       = 8
	defaultSimilarityThresh = 0.8
	loopPatternMinRepeat    = 3
)

// ToolCallRecord 记录单次 tool_call 的关键信息。
type ToolCallRecord struct {
	ToolName  string
	ArgsHash  string // 参数的规范化哈希
	Timestamp time.Time
}

// DriftResult 描述漂移检测结果。
type DriftResult struct {
	Drifted       bool
	Pattern       string // "loop" | "diverge" | ""
	ReplanPrompt  string // 注入 LLM 的重规划提示
	NeedHumanHelp bool   // 二次触发时为 true
}

// DriftDetector 分析 tool_call 序列检测循环模式。
type DriftDetector struct {
	windowSize       int     // 检测窗口大小 (默认 K=8)
	similarityThresh float64 // 参数相似度阈值 (默认 0.8)
	replanCount      int     // 当前 loop 中重规划次数
	records          []ToolCallRecord
}

// NewDriftDetector 创建漂移检测器。
func NewDriftDetector(windowSize int, threshold float64) *DriftDetector {
	if windowSize <= 0 {
		windowSize = defaultWindowSize
	}
	if threshold <= 0 || threshold > 1 {
		threshold = defaultSimilarityThresh
	}
	return &DriftDetector{
		windowSize:       windowSize,
		similarityThresh: threshold,
	}
}

// Record 记录一次 tool_call。
func (d *DriftDetector) Record(rec ToolCallRecord) {
	d.records = append(d.records, rec)
	// 只保留最近 windowSize 条记录
	if len(d.records) > d.windowSize {
		d.records = d.records[len(d.records)-d.windowSize:]
	}
}

// DetectDrift 分析最近 K 步，返回漂移类型。
// 循环模式：连续 3 次或以上调用相同工具且参数哈希相同。
func (d *DriftDetector) DetectDrift() DriftResult {
	if len(d.records) < loopPatternMinRepeat {
		return DriftResult{}
	}

	// 从窗口末尾向前检查连续相同 tool+argsHash
	window := d.records
	lastIdx := len(window) - 1
	lastRec := window[lastIdx]
	consecutiveCount := 1

	for i := lastIdx - 1; i >= 0; i-- {
		if window[i].ToolName == lastRec.ToolName && window[i].ArgsHash == lastRec.ArgsHash {
			consecutiveCount++
		} else {
			break
		}
	}

	if consecutiveCount >= loopPatternMinRepeat {
		d.replanCount++
		needHuman := d.replanCount >= 2

		prompt := fmt.Sprintf(
			"[⚠️ 漂移检测]\n检测到循环模式: 连续 %d 次调用 %s 且参数相似。\n请暂停当前操作，重新审视原始目标，制定新的执行计划。\n不要重复之前失败的方法，尝试不同的解决路径。\n[/漂移检测]",
			consecutiveCount, lastRec.ToolName,
		)

		return DriftResult{
			Drifted:       true,
			Pattern:       "loop",
			ReplanPrompt:  prompt,
			NeedHumanHelp: needHuman,
		}
	}

	return DriftResult{}
}

// ResetWindow 重规划后重置检测窗口。
func (d *DriftDetector) ResetWindow() {
	d.records = nil
}
