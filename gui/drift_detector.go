package main

import (
	"fmt"
	"time"
)

const (
	defaultWindowSize       = 8
	defaultSimilarityThresh = 0.8
	loopPatternMinRepeat    = 3
	// slowPollMinRepeat is the higher threshold for drift detection when
	// consecutive identical calls are spread over a long time span. This
	// indicates the LLM is interleaving other work (e.g., screenshot calls
	// between list_directory polls while waiting for an async process),
	// not stuck in a tight loop.
	slowPollMinRepeat = 5
	// slowPollTimeSpan is the minimum time span between the first and last
	// consecutive identical calls to be considered "slow polling" rather
	// than a tight loop. If the span exceeds this, use slowPollMinRepeat.
	slowPollTimeSpan = 15 * time.Second
)

// ToolCallRecord 记录单次 tool_call 的关键信息。
type ToolCallRecord struct {
	ToolName   string
	ArgsHash   string // 参数的规范化哈希
	Timestamp  time.Time
	ResultHint string // 工具返回结果的摘要（截断到 200 字符），用于漂移恢复时提供上下文
	ResultHash string // 工具返回结果的完整哈希，用于漂移检测中比较结果是否变化
}

// DriftResult 描述漂移检测结果。
type DriftResult struct {
	Drifted       bool
	Pattern       string // "loop" | "diverge" | ""
	ReplanPrompt  string // 注入 LLM 的重规划提示
	NeedHumanHelp bool   // 二次触发时为 true
	DriftedTool   string // 触发漂移的工具名
}

// DriftDetector 分析 tool_call 序列检测循环模式。
//
// 漂移的定义：同样的输入产生同样的输出——死循环，外部状态没有推进。
// 非漂移（合法轮询）：同样的输入但输出在变化——外部状态在推进（如后台任务
// 从 running → completed，编程会话产出新输出）。
//
// 检测机制：连续 N 次调用同名工具且参数哈希相同时，进一步检查结果是否有变化。
// 结果有变化 → 不触发漂移（外部状态在推进）。
// 结果无变化 → 触发漂移（死循环）。
type DriftDetector struct {
	windowSize       int     // 检测窗口大小 (默认 K=8)
	similarityThresh float64 // 参数相似度阈值 (默认 0.8)
	replanCount      int     // 当前 loop 中重规划次数（跨轮次累积）
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

// NewDriftDetectorWithHistory 创建漂移检测器并继承之前的重规划次数。
// 用于跨 agent loop 保持漂移记忆，避免用户确认后重新走一遍完整的
// "第一次漂移→recover→第二次漂移→人工介入"流程。
func NewDriftDetectorWithHistory(windowSize int, threshold float64, priorReplanCount int) *DriftDetector {
	d := NewDriftDetector(windowSize, threshold)
	d.replanCount = priorReplanCount
	return d
}

// ReplanCount 返回当前累积的重规划次数。
func (d *DriftDetector) ReplanCount() int {
	if d == nil {
		return 0
	}
	return d.replanCount
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
//
// 循环模式检测：连续 3 次或以上调用相同工具且参数哈希相同。
// 但仅当结果也没有变化时才判定为漂移。如果参数相同但结果在变化，
// 说明外部状态在推进（如后台任务从 running → completed），不是死循环。
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

	if consecutiveCount < loopPatternMinRepeat {
		return DriftResult{}
	}

	// --- 时间跨度感知：区分紧密循环和慢速轮询 ---
	// 如果连续相同调用的时间跨度超过 slowPollTimeSpan（15s），说明 LLM
	// 在调用之间穿插了其他工作（如 screenshot），不是紧密循环。
	// 此时提高触发阈值到 slowPollMinRepeat（5 次），给 LLM 更多轮询机会。
	startIdx := len(window) - consecutiveCount
	timeSpan := window[lastIdx].Timestamp.Sub(window[startIdx].Timestamp)
	if timeSpan > slowPollTimeSpan && consecutiveCount < slowPollMinRepeat {
		return DriftResult{}
	}

	// --- 机制性修复：检查结果是否有变化 ---
	// 真正的漂移 = 同样的输入 + 同样的输出（死循环）
	// 合法轮询 = 同样的输入 + 不同的输出（外部状态在推进）
	//
	// 检查连续相同参数的调用中，ResultHint 是否有变化。
	// 只要有任意两次结果不同，就说明外部状态在推进，不是死循环。
	if resultsAreChanging(window, consecutiveCount) {
		return DriftResult{}
	}

	d.replanCount++
	needHuman := d.replanCount >= 2

	lastResultHint := lastRec.ResultHint

	var prompt string
	if needHuman {
		prompt = fmt.Sprintf(
			"[⚠️ 漂移检测 — 严重]\n连续 %d 次调用 %s 且参数相同，已是第 %d 次漂移。\n"+
				"该工具在当前场景下无法完成任务。\n"+
				"禁止再次调用 %s。\n"+
				"请直接向用户说明当前遇到的具体问题和限制，不要再尝试。\n",
			consecutiveCount, lastRec.ToolName, d.replanCount, lastRec.ToolName,
		)
		if lastResultHint != "" {
			prompt += fmt.Sprintf("最后一次工具返回: %s\n", lastResultHint)
		}
		prompt += "[/漂移检测]"
	} else {
		prompt = fmt.Sprintf(
			"[⚠️ 漂移检测]\n检测到循环模式: 连续 %d 次调用 %s 且参数相似。\n"+
				"请暂停当前操作，重新审视原始目标，制定新的执行计划。\n"+
				"不要重复之前失败的方法，尝试不同的解决路径。\n",
			consecutiveCount, lastRec.ToolName,
		)
		if lastResultHint != "" {
			prompt += fmt.Sprintf("最后一次工具返回: %s\n请根据以上工具反馈调整策略，而不是用相同参数重试。\n", lastResultHint)
		}
		prompt += "如果没有其他可行路径，直接告诉用户当前的限制。\n[/漂移检测]"
	}

	return DriftResult{
		Drifted:       true,
		Pattern:       "loop",
		ReplanPrompt:  prompt,
		NeedHumanHelp: needHuman,
		DriftedTool:   lastRec.ToolName,
	}
}

// resultsAreChanging checks whether the tool results are changing across
// the consecutive same-args calls in the detection window.
//
// This is the core mechanism that distinguishes real drift (dead loop) from
// legitimate polling (external state progressing):
//   - SSH check_task: "running 18s" → "running 23s" → "completed" = changing
//   - SSH exec same failing command: "connection refused" → "connection refused" = not changing
//   - get_session_output: "" → "compiling..." → "tests passed" = changing
//   - write_file same content: "ok" → "ok" → "ok" = not changing
//
// Prefers ResultHash (hash of the full tool result) for comparison, falling
// back to ResultHint (truncated to 200 runes) when hash is not available.
// Using the full hash avoids false negatives when the changing part (e.g.
// "已运行: 23s") falls beyond the truncation boundary of ResultHint.
//
// Returns true if any two consecutive results differ (at least one state transition).
// Returns false (not changing) when:
//   - All results are identical, OR
//   - All comparison values are empty (no result data — conservative, treat as drift)
func resultsAreChanging(window []ToolCallRecord, consecutiveCount int) bool {
	startIdx := len(window) - consecutiveCount

	// Pick the comparison key: prefer ResultHash, fall back to ResultHint.
	getKey := func(r *ToolCallRecord) string {
		if r.ResultHash != "" {
			return r.ResultHash
		}
		return r.ResultHint
	}

	allEmpty := true
	for i := startIdx; i < len(window); i++ {
		if getKey(&window[i]) != "" {
			allEmpty = false
			break
		}
	}
	// If all keys are empty, we have no result data to compare.
	// Conservative: treat as potential drift (don't suppress detection).
	if allEmpty {
		return false
	}

	// Check if any two consecutive results differ.
	for i := startIdx + 1; i < len(window); i++ {
		if getKey(&window[i]) != getKey(&window[i-1]) {
			return true // At least one state transition — not a dead loop.
		}
	}
	return false
}

// PreviewDrift analyzes the recent window without mutating detector state.
// It reports whether the next DetectDrift call would classify the current
// sequence as a loop and whether that next classification would require
// human help.
func (d *DriftDetector) PreviewDrift() DriftResult {
	if d == nil || len(d.records) < loopPatternMinRepeat {
		return DriftResult{}
	}

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
	if consecutiveCount < loopPatternMinRepeat {
		return DriftResult{}
	}
	// Apply the same time-span check as DetectDrift.
	startIdx := len(window) - consecutiveCount
	timeSpan := window[lastIdx].Timestamp.Sub(window[startIdx].Timestamp)
	if timeSpan > slowPollTimeSpan && consecutiveCount < slowPollMinRepeat {
		return DriftResult{}
	}
	// Apply the same result-change check as DetectDrift.
	if resultsAreChanging(window, consecutiveCount) {
		return DriftResult{}
	}
	return DriftResult{
		Drifted:       true,
		Pattern:       "loop",
		NeedHumanHelp: d.replanCount+1 >= 2,
		DriftedTool:   lastRec.ToolName,
	}
}

// ResetWindow 重规划后重置检测窗口。
func (d *DriftDetector) ResetWindow() {
	d.records = nil
}
