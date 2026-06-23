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

	// --- Tool-frequency anomaly detection (Layer 2) ---
	//
	// Detects semantic loops where the LLM calls the same tool repeatedly
	// with DIFFERENT arguments that all succeed. Example: memory(save) called
	// 4+ times with different content — each call succeeds, but the LLM is
	// stuck in a "save knowledge → summarize → save more knowledge" loop.
	//
	// The existing Layer 1 (ArgsHash-based) misses this because each call
	// has a different ArgsHash. Layer 2 looks at tool-name frequency within
	// the detection window, regardless of arguments.
	//
	// freqAnomalyMinCalls: minimum number of calls to the same tool within
	// the window to trigger frequency anomaly detection.
	freqAnomalyMinCalls = 4
	// freqAnomalyDominanceRatio: the fraction of the window that must be
	// occupied by the same tool to be considered anomalous. 0.75 means the
	// tool must account for 75%+ of the window. This is deliberately high
	// to avoid false positives on legitimate coding patterns (e.g.,
	// scaffolding 5-6 files via write_file in sequence).
	freqAnomalyDominanceRatio = 0.75
)

// ToolCallRecord 记录单次 tool_call 的关键信息。
type ToolCallRecord struct {
	ToolName   string
	ArgsHash   string // 参数的规范化哈希
	Timestamp  time.Time
	ResultHint string // 工具返回结果的摘要（截断到 200 字符），用于漂移恢复时提供上下文
	ResultHash string // 工具返回结果的完整哈希，用于漂移检测中比较结果是否变化
	Succeeded  bool   // 工具调用是否成功（来自 outcome 结构化判定，非文本猜测）
}

// DriftResult 描述漂移检测结果。
type DriftResult struct {
	Drifted       bool
	Pattern       string // "loop" (Layer 1: same args) | "frequency" (Layer 2: same tool dominates window) | ""
	ReplanPrompt  string // 注入 LLM 的重规划提示
	NeedHumanHelp bool   // 二次触发时为 true
	DriftedTool   string // 触发漂移的工具名
}

// DriftDetector 分析 tool_call 序列检测循环模式。
//
// Two detection layers:
//
// Layer 1 (ArgsHash-based): 连续 N 次调用同名工具且参数哈希相同，
// 且结果也没有变化 → 死循环。结果有变化 → 合法轮询，不触发。
//
// Layer 2 (frequency-based): 窗口内同一工具占比超过阈值（75%），
// 即使参数不同。检测语义循环——LLM 反复执行同类型操作但参数每次不同。
// 排除轮询模式（所有调用参数相同 = 在查询同一个状态）。
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
// Two detection layers:
//
// Layer 1 (ArgsHash-based): 连续 N 次调用相同工具且参数哈希相同。
// 仅当结果也没有变化时才判定为漂移。如果参数相同但结果在变化，
// 说明外部状态在推进（如后台任务从 running → completed），不是死循环。
//
// Layer 2 (frequency-based): 窗口内同一工具被调用超过阈值次数，
// 即使参数不同。检测语义循环——LLM 反复执行同类型操作（如反复保存知识）
// 但参数每次不同，Layer 1 无法捕获。
// 排除轮询模式（同一工具所有调用参数相同 = 在反复查询同一个状态）。
func (d *DriftDetector) DetectDrift() DriftResult {
	if len(d.records) < loopPatternMinRepeat {
		return DriftResult{}
	}

	// --- Layer 1: ArgsHash-based exact repetition ---
	if result := d.detectArgsHashDrift(); result.Drifted {
		return result
	}

	// --- Layer 2: Tool-frequency anomaly ---
	if result := d.detectFrequencyAnomaly(); result.Drifted {
		return result
	}

	return DriftResult{}
}

// detectArgsHashDrift is the original Layer 1 detection: consecutive calls
// with the same tool + same ArgsHash + same results.
func (d *DriftDetector) detectArgsHashDrift() DriftResult {
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

	startIdx := len(window) - consecutiveCount
	timeSpan := window[lastIdx].Timestamp.Sub(window[startIdx].Timestamp)
	if timeSpan > slowPollTimeSpan && consecutiveCount < slowPollMinRepeat {
		return DriftResult{}
	}

	if resultsAreChanging(window, consecutiveCount) {
		return DriftResult{}
	}

	return d.buildDriftResult(consecutiveCount, lastRec.ToolName, lastRec.ResultHint, "loop")
}

// buildDriftResult constructs a DriftResult with the appropriate prompt.
// Shared by both Layer 1 and Layer 2 to avoid duplicating prompt logic.
func (d *DriftDetector) buildDriftResult(count int, toolName, lastResultHint, pattern string) DriftResult {
	d.replanCount++
	needHuman := d.replanCount >= 2

	var prompt string
	if needHuman {
		prompt = fmt.Sprintf(
			"[⚠️ 漂移检测 — 严重]\n连续 %d 次调用 %s，已是第 %d 次漂移。\n"+
				"该工具在当前场景下无法完成任务。\n"+
				"禁止再次调用 %s。\n"+
				"请直接向用户说明当前遇到的具体问题和限制，不要再尝试。\n",
			count, toolName, d.replanCount, toolName,
		)
		if lastResultHint != "" {
			prompt += fmt.Sprintf("最后一次工具返回: %s\n", lastResultHint)
		}
		prompt += "[/漂移检测]"
	} else {
		if pattern == "frequency" {
			// Layer 2: frequency anomaly — the LLM is repeating the same
			// type of action with different arguments. Guide it to stop
			// and deliver the result to the user.
			prompt = fmt.Sprintf(
				"[⚠️ 漂移检测]\n检测到语义循环: 在最近 %d 次工具调用中，%s 被调用了 %d 次（参数不同但行为重复）。\n"+
					"你正在反复执行同一类操作。用户的任务可能已经完成。\n"+
					"请停止调用 %s，直接向用户汇报已完成的工作和结果。\n",
				len(d.records), toolName, count, toolName,
			)
		} else {
			prompt = fmt.Sprintf(
				"[⚠️ 漂移检测]\n检测到循环模式: 连续 %d 次调用 %s 且参数相似。\n"+
					"请暂停当前操作，重新审视原始目标，制定新的执行计划。\n"+
					"不要重复之前失败的方法，尝试不同的解决路径。\n",
				count, toolName,
			)
		}
		if lastResultHint != "" {
			if pattern == "frequency" {
				prompt += fmt.Sprintf("最后一次工具返回: %s\n", lastResultHint)
			} else {
				prompt += fmt.Sprintf("最后一次工具返回: %s\n请根据以上工具反馈调整策略，而不是用相同参数重试。\n", lastResultHint)
			}
		}
		prompt += "如果没有其他可行路径，直接告诉用户当前的限制。\n[/漂移检测]"
	}

	return DriftResult{
		Drifted:       true,
		Pattern:       pattern,
		ReplanPrompt:  prompt,
		NeedHumanHelp: needHuman,
		DriftedTool:   toolName,
	}
}

// detectFrequencyAnomaly is Layer 2: detects when the same tool dominates
// the detection window, even with different arguments.
//
// This catches semantic loops where the LLM repeats the same TYPE of action
// (e.g., saving knowledge 4 times with different wording) but each call has
// a different ArgsHash, so Layer 1 doesn't fire.
//
// The detection is based on three conditions:
//  1. The tool appears >= freqAnomalyMinCalls times in the window.
//  2. The tool's share of the window exceeds freqAnomalyDominanceRatio.
//  3. The results are NOT changing across the dominant tool's calls.
//
// Condition 3 is the mechanism-level invariant shared with Layer 1:
// if tool results are changing, external state is progressing, and the
// LLM is not stuck. This applies regardless of whether arguments are
// the same (Layer 1) or different (Layer 2).
//
// Polling is excluded via isPollingPattern: when ALL calls to the dominant
// tool share the same ArgsHash, the tool is repeatedly querying the same
// thing (status polling), which is legitimate and already handled by Layer 1.
func (d *DriftDetector) detectFrequencyAnomaly() DriftResult {
	window := d.records
	if len(window) < freqAnomalyMinCalls {
		return DriftResult{}
	}

	// Count occurrences of each tool in the window.
	toolCounts := make(map[string]int)
	for i := range window {
		toolCounts[window[i].ToolName]++
	}

	// Find the most frequent tool.
	var dominantTool string
	var dominantCount int
	for name, count := range toolCounts {
		if count > dominantCount {
			dominantTool = name
			dominantCount = count
		}
	}

	if dominantCount < freqAnomalyMinCalls {
		return DriftResult{}
	}

	ratio := float64(dominantCount) / float64(len(window))
	if ratio < freqAnomalyDominanceRatio {
		return DriftResult{}
	}

	// Exclude polling patterns: a tool is considered "polling" if ALL of
	// its calls in the window have the same ArgsHash (same query) — this
	// is the defining characteristic of polling (repeatedly checking the
	// same thing). Tools with different ArgsHash each time are NOT polling
	// — they're performing different operations (like saving different
	// knowledge).
	if isPollingPattern(window, dominantTool) {
		return DriftResult{}
	}

	// --- Core mechanism: check if the LLM is making forward progress ---
	//
	// Two independent signals indicate forward progress (either one suffices):
	//
	// Signal 1 (result diversity): tool results are changing across calls →
	// external state is progressing. Same invariant as Layer 1 (#48).
	//
	// Signal 2 (argument diversity + non-failure): ALL calls have unique
	// ArgsHash values AND majority are not failures → each call operates on
	// a different target, completing a different sub-goal. This is the
	// defining characteristic of a batch operation (deleting different files,
	// querying different endpoints, writing different config entries).
	//
	// Why argument diversity alone is a valid progression signal:
	// The semantic loop Layer 2 detects has a specific shape — the LLM
	// repeats the same INTENT (e.g., "save knowledge") with superficially
	// different arguments but achieves no user-facing progress. In that case,
	// the results are also identical ("已保存") because the operation type is
	// the same. When results ARE identical but arguments are ALL unique AND
	// successful, the parsimonious explanation is a batch operation, not a
	// semantic loop — because a stuck LLM would eventually repeat arguments
	// (it has limited ways to rephrase the same intent).
	//
	// The failure check prevents bypassing drift detection when the LLM is
	// trying different approaches to solve the same problem and all fail.
	if freqResultsAreProgressing(window, dominantTool) {
		return DriftResult{}
	}
	if freqArgsAllUnique(window, dominantTool) {
		return DriftResult{}
	}

	// Find the last result hint for the dominant tool.
	var lastHint string
	for i := len(window) - 1; i >= 0; i-- {
		if window[i].ToolName == dominantTool {
			lastHint = window[i].ResultHint
			break
		}
	}

	return d.buildDriftResult(dominantCount, dominantTool, lastHint, "frequency")
}

// resultKey returns the comparison key for a ToolCallRecord's result.
// Prefers ResultHash (full hash) over ResultHint (truncated).
func resultKey(r *ToolCallRecord) string {
	if r.ResultHash != "" {
		return r.ResultHash
	}
	return r.ResultHint
}

// freqResultsAreProgressing checks whether the dominant tool's calls
// in the window represent a multi-step workflow (progressing) or a
// semantic loop (stuck).
//
// The mechanism is the same invariant as Layer 1 (#48): if tool results
// are changing across calls, external state is progressing. Uses ResultHash
// (full result hash) for comparison — this is the most reliable signal
// because it captures the complete tool output, not a truncated hint.
//
// Returns true if the majority of consecutive result pairs have different
// ResultHash values, indicating the LLM is making forward progress.
// Returns false if results are mostly identical (semantic loop).
//
// The threshold is >50% of consecutive pairs must differ.
func freqResultsAreProgressing(window []ToolCallRecord, toolName string) bool {
	// Collect ResultHash values for the dominant tool in order.
	var hashes []string
	for i := range window {
		if window[i].ToolName == toolName {
			hashes = append(hashes, window[i].ResultHash)
		}
	}

	if len(hashes) < 2 {
		return false // Not enough data to judge
	}

	// If all hashes are empty, no result data — conservative, treat as drift.
	allEmpty := true
	for _, h := range hashes {
		if h != "" {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		return false
	}

	// Count how many consecutive pairs have different hashes.
	diffCount := 0
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[i-1] {
			diffCount++
		}
	}

	// >50% of pairs differ → progressing (not a semantic loop).
	totalPairs := len(hashes) - 1
	return diffCount*2 > totalPairs
}

// freqArgsAllUnique checks whether ALL calls to the dominant tool in the
// window have unique ArgsHash values. When every call has different arguments,
// the LLM is executing different commands — this is a batch operation pattern,
// not a semantic loop.
//
// Returns true only when ALL ArgsHash values are distinct (no duplicates)
// AND the majority of calls succeeded. Even a single repeated ArgsHash means
// the LLM might be retrying the same operation, which is a weaker signal of
// progress. The failure check prevents bypassing drift detection when the LLM
// is trying different approaches to solve the same problem and all fail.
func freqArgsAllUnique(window []ToolCallRecord, toolName string) bool {
	seen := make(map[string]struct{})
	failCount := 0
	totalCount := 0
	for i := range window {
		if window[i].ToolName != toolName {
			continue
		}
		if _, dup := seen[window[i].ArgsHash]; dup {
			return false
		}
		seen[window[i].ArgsHash] = struct{}{}
		totalCount++
		if !window[i].Succeeded {
			failCount++
		}
	}
	if len(seen) < 2 {
		return false // Need at least 2 calls to be meaningful
	}
	// If majority of calls failed, this is not a successful batch operation —
	// the LLM is trying different approaches that all fail (should be caught).
	if totalCount > 0 && failCount*2 > totalCount {
		return false
	}
	return true
}

// isPollingPattern checks whether the given tool's calls in the window
// look like polling behavior (same args every time = checking the same
// thing repeatedly). This is the mechanism-level distinction between
// polling (legitimate) and semantic loops (anomalous):
//
//   - Polling: same tool + same args + results may change → checking status
//   - Semantic loop: same tool + different args + all succeed → repeating action
//
// Returns true if all calls to the tool have the same ArgsHash.
func isPollingPattern(window []ToolCallRecord, toolName string) bool {
	var firstHash string
	seen := false
	for i := range window {
		if window[i].ToolName != toolName {
			continue
		}
		if !seen {
			firstHash = window[i].ArgsHash
			seen = true
			continue
		}
		if window[i].ArgsHash != firstHash {
			return false // Different args → not polling
		}
	}
	return true // All same args → polling pattern
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

	allEmpty := true
	for i := startIdx; i < len(window); i++ {
		if resultKey(&window[i]) != "" {
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
		if resultKey(&window[i]) != resultKey(&window[i-1]) {
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

	// Layer 1: ArgsHash-based
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
		startIdx := len(window) - consecutiveCount
		timeSpan := window[lastIdx].Timestamp.Sub(window[startIdx].Timestamp)
		if !(timeSpan > slowPollTimeSpan && consecutiveCount < slowPollMinRepeat) {
			if !resultsAreChanging(window, consecutiveCount) {
				return DriftResult{
					Drifted:       true,
					Pattern:       "loop",
					NeedHumanHelp: d.replanCount+1 >= 2,
					DriftedTool:   lastRec.ToolName,
				}
			}
		}
	}

	// Layer 2: frequency anomaly (preview only — don't mutate replanCount)
	if len(window) >= freqAnomalyMinCalls {
		toolCounts := make(map[string]int)
		for i := range window {
			toolCounts[window[i].ToolName]++
		}
		for name, count := range toolCounts {
			if count >= freqAnomalyMinCalls {
				ratio := float64(count) / float64(len(window))
				if ratio >= freqAnomalyDominanceRatio && !isPollingPattern(window, name) && !freqResultsAreProgressing(window, name) && !freqArgsAllUnique(window, name) {
					return DriftResult{
						Drifted:       true,
						Pattern:       "frequency",
						NeedHumanHelp: d.replanCount+1 >= 2,
						DriftedTool:   name,
					}
				}
			}
		}
	}

	return DriftResult{}
}

// ResetWindow 重规划后重置检测窗口。
func (d *DriftDetector) ResetWindow() {
	d.records = nil
}

// UndoLastReplan decrements the replan counter by one. Used when a drift
// detection result is deferred (e.g., mid-parallel-group) and the detection
// will be re-evaluated later. Without this, the deferred detection's
// replanCount increment would cause the next detection to immediately
// escalate to NeedHumanHelp=true.
func (d *DriftDetector) UndoLastReplan() {
	if d.replanCount > 0 {
		d.replanCount--
	}
}
