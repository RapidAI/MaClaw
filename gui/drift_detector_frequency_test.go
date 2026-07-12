package main

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Tests for DriftDetector Layer 2: tool-frequency anomaly detection.
//
// Layer 2 detects semantic loops where the LLM calls the same tool
// repeatedly with DIFFERENT arguments. This is the exact pattern from
// the bug: memory(save) called 3 times with different content, each
// call succeeds, but the LLM is stuck in a "save → summarize → save"
// loop.
// ---------------------------------------------------------------------------

func TestFrequencyAnomaly_MemorySaveLoop_Detected(t *testing.T) {
	// Reproduces the original bug: LLM calls memory(save) 4 times in a
	// semantic loop. In real loops, the LLM often retries with similar
	// content — some ArgsHash values repeat because the LLM rephrases
	// the same knowledge slightly. Layer 2 fires because:
	// 1. memory dominates window (4/5 = 80% > 75%)
	// 2. ArgsHash are NOT all unique (has a repeated hash)
	// 3. Results are identical ("已保存")
	d := NewDriftDetector(8, 0.8)

	// One non-memory call at the start
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "ssh_hash_1", ResultHint: "OmniRoute 降级成功"})

	// Four memory(save) calls — note mem_hash_2 is repeated (LLM rephrased same content)
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "mem_hash_1", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "mem_hash_2", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "mem_hash_2", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "mem_hash_3", ResultHint: "已保存"})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("expected frequency anomaly to be detected for repeated memory(save)")
	}
	if result.Pattern != "frequency" {
		t.Errorf("expected pattern='frequency', got %q", result.Pattern)
	}
	if result.DriftedTool != "memory" {
		t.Errorf("expected drifted tool='memory', got %q", result.DriftedTool)
	}
	if !strings.Contains(result.ReplanPrompt, "语义循环") {
		t.Errorf("prompt should mention semantic loop, got: %s", result.ReplanPrompt)
	}
	if !strings.Contains(result.ReplanPrompt, "汇报已完成的工作") {
		t.Errorf("prompt should guide LLM to report results, got: %s", result.ReplanPrompt)
	}
}

func TestFrequencyAnomaly_BashDifferentCommands_NotDetected(t *testing.T) {
	// bash called 3 times with different commands in a window of 6 (50%) —
	// this is normal coding behavior (mkdir, compile, run). Should NOT trigger
	// because 50% < 60% threshold.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "hash_mkdir"})
	d.Record(ToolCallRecord{ToolName: "read_file", ArgsHash: "hash_read1"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "hash_compile"})
	d.Record(ToolCallRecord{ToolName: "read_file", ArgsHash: "hash_read2"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "hash_test"})
	d.Record(ToolCallRecord{ToolName: "write_file", ArgsHash: "hash_write"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("bash with different commands at 50% should NOT trigger frequency anomaly")
	}
}

func TestFrequencyAnomaly_PollingExcluded(t *testing.T) {
	// check_task called 4 times with the SAME ArgsHash (polling same task).
	// This is polling behavior — all calls have the same args.
	// isPollingPattern should return true, excluding it from Layer 2.
	d := NewDriftDetector(8, 0.8)

	sameHash := "task_id_123"
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: sameHash, ResultHint: "running 10s"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: sameHash, ResultHint: "running 15s"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: sameHash, ResultHint: "running 20s"})

	result := d.DetectDrift()
	// Layer 1 won't fire because results are changing.
	// Layer 2 won't fire because it's a polling pattern (same ArgsHash).
	if result.Drifted {
		t.Fatal("polling pattern (same args, changing results) should NOT trigger any drift")
	}
}

func TestFrequencyAnomaly_BelowThreshold_NotDetected(t *testing.T) {
	// memory called 3 times in a window of 5 — 60%, below 75% threshold.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "h1"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1"})
	d.Record(ToolCallRecord{ToolName: "read_file", ArgsHash: "r1"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("memory at 60% of window should NOT trigger frequency anomaly (threshold is 75%)")
	}
}

func TestFrequencyAnomaly_ExactThreshold_Detected(t *testing.T) {
	// memory called 6 times in a window of 8 — 75%, at the threshold.
	// Results are identical AND args have a repeat → DRIFT.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "h1", ResultHint: "ok"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "h2", ResultHint: "ok"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHint: "已保存"}) // repeated
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m4", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m5", ResultHint: "已保存"})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("memory at 75% of window with repeated args should trigger frequency anomaly")
	}
	if result.Pattern != "frequency" {
		t.Errorf("expected pattern='frequency', got %q", result.Pattern)
	}
}

func TestFrequencyAnomaly_Layer1TakesPrecedence(t *testing.T) {
	// When both Layer 1 and Layer 2 would fire, Layer 1 fires first
	// (it's checked first in DetectDrift). Verify the pattern is "loop".
	d := NewDriftDetector(8, 0.8)

	sameHash := "same_hash"
	sameResult := "已保存记忆: 同样的内容"
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: sameHash, ResultHint: sameResult})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: sameHash, ResultHint: sameResult})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: sameHash, ResultHint: sameResult})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("expected drift to be detected")
	}
	if result.Pattern != "loop" {
		t.Errorf("Layer 1 should take precedence, expected pattern='loop', got %q", result.Pattern)
	}
}

func TestFrequencyAnomaly_PreviewDrift_DetectsFrequency(t *testing.T) {
	// PreviewDrift should also detect Layer 2 anomalies when results
	// are not progressing and args have repeats.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "ssh_1", ResultHint: "done"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHint: "已保存"}) // repeated
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3", ResultHint: "已保存"})

	result := d.PreviewDrift()
	if !result.Drifted {
		t.Fatal("PreviewDrift should detect frequency anomaly")
	}
	if result.Pattern != "frequency" {
		t.Errorf("expected pattern='frequency', got %q", result.Pattern)
	}
}

func TestFrequencyAnomaly_WriteFileCodingSession_NotDetected(t *testing.T) {
	// write_file called 4 times in a window of 8 (50%) — normal coding
	// behavior (scaffolding multiple files). Should NOT trigger because
	// 50% < 75% threshold. The higher threshold avoids false positives
	// on legitimate scaffolding patterns.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "read_file", ArgsHash: "r1"})
	d.Record(ToolCallRecord{ToolName: "write_file", ArgsHash: "w1"})
	d.Record(ToolCallRecord{ToolName: "write_file", ArgsHash: "w2"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b1"})
	d.Record(ToolCallRecord{ToolName: "write_file", ArgsHash: "w3"})
	d.Record(ToolCallRecord{ToolName: "write_file", ArgsHash: "w4"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b2"})
	d.Record(ToolCallRecord{ToolName: "read_file", ArgsHash: "r2"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("write_file at 50% should NOT trigger frequency anomaly")
	}
}

func TestFrequencyAnomaly_NormalCodingMix_NotDetected(t *testing.T) {
	// Normal coding pattern: interleaved read/write/bash/edit.
	// No single tool dominates.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "read_file", ArgsHash: "r1"})
	d.Record(ToolCallRecord{ToolName: "write_file", ArgsHash: "w1"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b1"})
	d.Record(ToolCallRecord{ToolName: "read_file", ArgsHash: "r2"})
	d.Record(ToolCallRecord{ToolName: "edit_file", ArgsHash: "e1"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b2"})
	d.Record(ToolCallRecord{ToolName: "write_file", ArgsHash: "w2"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b3"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("normal coding mix should NOT trigger any drift")
	}
}

func TestFrequencyAnomaly_SecondDrift_NeedHumanHelp(t *testing.T) {
	// After the first frequency anomaly drift, if the LLM continues
	// the same pattern, the second drift should set NeedHumanHelp=true.
	d := NewDriftDetector(8, 0.8)

	// First drift — results identical, args have repeats
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "s1", ResultHint: "done"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHint: "已保存"}) // repeated
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3", ResultHint: "已保存"})

	result1 := d.DetectDrift()
	if !result1.Drifted {
		t.Fatal("first drift should be detected")
	}
	if result1.NeedHumanHelp {
		t.Fatal("first drift should NOT need human help")
	}

	d.ResetWindow()

	// Second drift (LLM ignored the warning) — still identical results with repeats
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m4", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m5", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m4", ResultHint: "已保存"}) // repeated
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m6", ResultHint: "已保存"})

	result2 := d.DetectDrift()
	if !result2.Drifted {
		t.Fatal("second drift should be detected")
	}
	if !result2.NeedHumanHelp {
		t.Fatal("second drift should need human help")
	}
}

func TestIsPollingPattern_SameArgs_True(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "ssh", ArgsHash: "same"},
		{ToolName: "bash", ArgsHash: "other"},
		{ToolName: "ssh", ArgsHash: "same"},
		{ToolName: "ssh", ArgsHash: "same"},
	}
	if !isPollingPattern(window, "ssh") {
		t.Fatal("all ssh calls have same ArgsHash — should be polling")
	}
}

func TestIsPollingPattern_DifferentArgs_False(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "memory", ArgsHash: "hash1"},
		{ToolName: "bash", ArgsHash: "other"},
		{ToolName: "memory", ArgsHash: "hash2"},
		{ToolName: "memory", ArgsHash: "hash3"},
	}
	if isPollingPattern(window, "memory") {
		t.Fatal("memory calls have different ArgsHash — should NOT be polling")
	}
}

func TestIsPollingPattern_SingleCall_True(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "memory", ArgsHash: "hash1"},
		{ToolName: "bash", ArgsHash: "other"},
	}
	// Only one memory call — trivially "all same args"
	if !isPollingPattern(window, "memory") {
		t.Fatal("single call should be considered polling (trivially same args)")
	}
}

func TestFrequencyAnomaly_SlowPollWithDifferentArgs_Detected(t *testing.T) {
	// Even with time gaps between calls, if args have repeats AND results
	// are not progressing, it's a semantic loop.
	d := NewDriftDetector(8, 0.8)

	now := time.Now()
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", Timestamp: now, ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2", Timestamp: now.Add(10 * time.Second), ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", Timestamp: now.Add(20 * time.Second), ResultHint: "已保存"}) // repeated
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3", Timestamp: now.Add(30 * time.Second), ResultHint: "已保存"})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("semantic loop should be detected regardless of time span")
	}
	if result.Pattern != "frequency" {
		t.Errorf("expected pattern='frequency', got %q", result.Pattern)
	}
}

// ---------------------------------------------------------------------------
// Tests for result-progression check in Layer 2 (freqResultsAreProgressing).
//
// The mechanism-level invariant: if tool results are changing across calls,
// external state is progressing, and the LLM is not stuck in a semantic loop.
// This is the same invariant as Layer 1's resultsAreChanging (#48), now
// applied to Layer 2.
// ---------------------------------------------------------------------------

func TestFrequencyAnomaly_SSHMultiStepWorkflow_ResultsProgressing_NotDetected(t *testing.T) {
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h1", ResultHash: "rh1", ResultHint: "SSH 连接成功 会话ID: ssh_root@api:22_1"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h2", ResultHash: "rh2", ResultHint: "后台任务已提交 任务ID: bg_1"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h3", ResultHash: "rh3", ResultHint: "docker-compose: command not found EXIT: 127"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h4", ResultHash: "rh4", ResultHint: "后台任务已提交 任务ID: bg_2"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatalf("SSH multi-step workflow with progressing results should NOT trigger drift, got pattern=%q tool=%q",
			result.Pattern, result.DriftedTool)
	}
}

func TestFrequencyAnomaly_SSHFullBugScenario_8Calls_NotDetected(t *testing.T) {
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h1", ResultHash: "rh1", ResultHint: "SSH 连接成功"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h2", ResultHash: "rh2", ResultHint: "后台任务已提交 bg_1"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h3", ResultHash: "rh3", ResultHint: "command not found EXIT: 127"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h4", ResultHash: "rh4", ResultHint: "后台任务已提交 bg_2"})

	result1 := d.DetectDrift()
	if result1.Drifted {
		t.Fatalf("Window 1: should NOT trigger, got pattern=%q", result1.Pattern)
	}

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h5", ResultHash: "rh5", ResultHint: "omniroute:base Up 15h EXIT: 0"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h6", ResultHash: "rh6", ResultHint: "后台任务已提交 bg_3"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h7", ResultHash: "rh7", ResultHint: "HEAD detached at v3.7.0 EXIT: 0"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h8", ResultHash: "rh8", ResultHint: "git fetch origin v3.7.2"})

	result2 := d.DetectDrift()
	if result2.Drifted {
		t.Fatalf("Window 2: should NOT trigger, got pattern=%q", result2.Pattern)
	}
}

func TestFrequencyAnomaly_MemorySaveLoop_DifferentHash_NotDetected(t *testing.T) {
	// memory(save) called 4 times with different args AND different ResultHash.
	// Each call genuinely returns different content → results are progressing
	// → NOT drift. This is the accepted trade-off: Layer 2 cannot distinguish
	// "SSH multi-step workflow" from "memory save loop" when both have
	// different ResultHash values. The memory loop will eventually stop via
	// max iterations or user intervention (P2), while SSH being killed is P0.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "s1", ResultHash: "rh0", ResultHint: "OmniRoute 降级成功"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHash: "rh1", ResultHint: "已保存记忆: 标准操作流程..."})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2", ResultHash: "rh2", ResultHint: "已保存记忆: 版本切换完整流程..."})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3", ResultHash: "rh3", ResultHint: "已保存记忆: 踩坑记录..."})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m4", ResultHash: "rh4", ResultHint: "已保存记忆: API 服务器信息..."})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("memory(save) with all-different ResultHash should NOT trigger — results are progressing")
	}
}

func TestFrequencyAnomaly_MemorySaveLoop_IdenticalResults_Detected(t *testing.T) {
	// memory(save) called 4 times, results identical "已保存", AND has
	// repeated ArgsHash (LLM retrying similar content).
	// Results NOT progressing + args NOT all unique → DRIFT.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "s1", ResultHint: "OmniRoute 降级成功"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2", ResultHint: "已保存"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", ResultHint: "已保存"}) // repeated
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3", ResultHint: "已保存"})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("memory(save) with repeated ArgsHash and identical results should trigger frequency anomaly")
	}
	if result.Pattern != "frequency" {
		t.Errorf("expected pattern='frequency', got %q", result.Pattern)
	}
}

func TestFrequencyAnomaly_BashSameErrorRepeated_Detected(t *testing.T) {
	// bash called 4 times with different commands but all returning the
	// same error. Results are NOT progressing → stuck → DRIFT.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b1", ResultHint: "permission denied"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b2", ResultHint: "permission denied"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b3", ResultHint: "permission denied"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b4", ResultHint: "permission denied"})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("bash with identical error results should trigger frequency anomaly")
	}
}

func TestFrequencyAnomaly_BashDifferentResults_NotDetected(t *testing.T) {
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b1", ResultHash: "rh1", ResultHint: "directory created"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b2", ResultHash: "rh2", ResultHint: "compiled successfully"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b3", ResultHash: "rh3", ResultHint: "3 tests passed"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b4", ResultHash: "rh4", ResultHint: "deployed to staging"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("bash with all-different results should NOT trigger frequency anomaly")
	}
}

func TestFrequencyAnomaly_PreviewDrift_ResultsProgressing_NotDetected(t *testing.T) {
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h1", ResultHash: "rh1", ResultHint: "connected"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h2", ResultHash: "rh2", ResultHint: "task submitted"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h3", ResultHash: "rh3", ResultHint: "task completed"})
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "h4", ResultHash: "rh4", ResultHint: "git fetched v3.7.2"})

	result := d.PreviewDrift()
	if result.Drifted {
		t.Fatalf("PreviewDrift should NOT detect progressing SSH as drift, got pattern=%q", result.Pattern)
	}
}

func TestFreqResultsAreProgressing_AllDifferent_True(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "ssh", ResultHash: "rh1"},
		{ToolName: "ssh", ResultHash: "rh2"},
		{ToolName: "ssh", ResultHash: "rh3"},
		{ToolName: "ssh", ResultHash: "rh4"},
	}
	if !freqResultsAreProgressing(window, "ssh") {
		t.Fatal("all different results should be progressing")
	}
}

func TestFreqResultsAreProgressing_AllSame_False(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "memory", ResultHash: "same"},
		{ToolName: "memory", ResultHash: "same"},
		{ToolName: "memory", ResultHash: "same"},
		{ToolName: "memory", ResultHash: "same"},
	}
	if freqResultsAreProgressing(window, "memory") {
		t.Fatal("all same results should NOT be progressing")
	}
}

func TestFreqResultsAreProgressing_MajorityDifferent_True(t *testing.T) {
	// 3 out of 4 pairs differ → >50% → progressing
	window := []ToolCallRecord{
		{ToolName: "ssh", ResultHash: "A"},
		{ToolName: "ssh", ResultHash: "B"},
		{ToolName: "ssh", ResultHash: "B"}, // same as previous
		{ToolName: "ssh", ResultHash: "C"},
		{ToolName: "ssh", ResultHash: "D"},
	}
	if !freqResultsAreProgressing(window, "ssh") {
		t.Fatal("3/4 pairs different should be progressing")
	}
}

func TestFreqResultsAreProgressing_MajoritySame_False(t *testing.T) {
	// 1 out of 3 pairs differ → <50% → NOT progressing
	window := []ToolCallRecord{
		{ToolName: "memory", ResultHash: "same"},
		{ToolName: "memory", ResultHash: "same"},
		{ToolName: "memory", ResultHash: "diff"},
		{ToolName: "memory", ResultHash: "diff"},
	}
	if freqResultsAreProgressing(window, "memory") {
		t.Fatal("1/3 pairs different should NOT be progressing")
	}
}

func TestFreqResultsAreProgressing_AllEmpty_False(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "ssh", ResultHint: ""},
		{ToolName: "ssh", ResultHint: ""},
		{ToolName: "ssh", ResultHint: ""},
	}
	if freqResultsAreProgressing(window, "ssh") {
		t.Fatal("all empty results should NOT be progressing (conservative)")
	}
}

func TestFreqResultsAreProgressing_UsesResultHash(t *testing.T) {
	// freqResultsAreProgressing uses ResultHash for comparison.
	// Different hashes → progressing.
	window := []ToolCallRecord{
		{ToolName: "ssh", ResultHash: "hash1"},
		{ToolName: "ssh", ResultHash: "hash2"},
		{ToolName: "ssh", ResultHash: "hash3"},
	}
	if !freqResultsAreProgressing(window, "ssh") {
		t.Fatal("different ResultHash should indicate progressing")
	}
}

func TestFreqResultsAreProgressing_SameHash_NotProgressing(t *testing.T) {
	// Same ResultHash → not progressing, even if ResultHint differs.
	window := []ToolCallRecord{
		{ToolName: "ssh", ResultHash: "same", ResultHint: "hint A"},
		{ToolName: "ssh", ResultHash: "same", ResultHint: "hint B"},
		{ToolName: "ssh", ResultHash: "same", ResultHint: "hint C"},
	}
	if freqResultsAreProgressing(window, "ssh") {
		t.Fatal("same ResultHash should NOT indicate progressing")
	}
}

func TestFreqResultsAreProgressing_IgnoresOtherTools(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "ssh", ResultHash: "same"},
		{ToolName: "bash", ResultHash: "different"},
		{ToolName: "ssh", ResultHash: "same"},
		{ToolName: "bash", ResultHash: "also different"},
		{ToolName: "ssh", ResultHash: "same"},
	}
	if freqResultsAreProgressing(window, "ssh") {
		t.Fatal("ssh results are all 'same' — should NOT be progressing")
	}
}



func TestFrequencyAnomaly_BashScaffolding_DifferentHash_NotDetected(t *testing.T) {
	// bash called 4 times with different ResultHash.
	// Results are progressing → NOT drift.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b1", ResultHash: "rh1", ResultHint: "mkdir: created directory 'src'"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b2", ResultHash: "rh2", ResultHint: "go build: compiled successfully"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b3", ResultHash: "rh3", ResultHint: "PASS: 15 tests passed in 2.3s"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "b4", ResultHash: "rh4", ResultHint: "deployed to staging environment"})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("bash with different ResultHash should NOT trigger drift")
	}
}


// ---------------------------------------------------------------------------
// Tests for argument-diversity signal (freqArgsAllUnique + isGeneralPurposeExecutor).
//
// The mechanism: when a general-purpose executor (bash/ssh) has ALL unique
// ArgsHash values, the LLM is executing different commands on different targets.
// This is a batch operation, not a semantic loop — even if results are identical
// (e.g., successful delete returns empty output).
// ---------------------------------------------------------------------------

func TestFrequencyAnomaly_BashBatchCleanup_AllUniqueArgs_NotDetected(t *testing.T) {
	// Reproduces the screensaver cleanup bug: LLM calls bash 6 times with
	// completely different commands (different registry keys, different files),
	// all succeeding with empty output "(命令执行完成，无输出)".
	// Layer 2 should NOT fire because all ArgsHash are unique and majority
	// of calls succeeded.
	d := NewDriftDetector(8, 0.8)

	emptyResult := "(命令执行完成，无输出)"
	emptyHash := "empty_result_hash"

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "del_screensaver_exe", ResultHash: emptyHash, ResultHint: emptyResult, Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "del_reg_key_1", ResultHash: emptyHash, ResultHint: emptyResult, Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "del_reg_key_2", ResultHash: emptyHash, ResultHint: emptyResult, Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "stop_service", ResultHash: emptyHash, ResultHint: emptyResult, Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "del_startup_entry", ResultHash: emptyHash, ResultHint: emptyResult, Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "del_scheduled_task", ResultHash: emptyHash, ResultHint: emptyResult, Succeeded: true})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatalf("bash batch cleanup with all-unique args should NOT trigger drift, got pattern=%q", result.Pattern)
	}
}

func TestFrequencyAnomaly_BashBatchCleanup_WithFailures_NotDetected(t *testing.T) {
	// Same as above but some commands fail (e.g., path not found).
	// Still should not trigger — different commands on different targets,
	// majority succeeded (4/6 = 67% > 50%).
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "scan_registry", ResultHash: "rh1", ResultHint: "found 3 suspicious keys", Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "del_key_hkcu_1", ResultHash: "rh_empty", ResultHint: "(命令执行完成，无输出)", Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "del_key_hklm_1", ResultHash: "rh_fail", ResultHint: "[错误] 退出码: exit status 1", Succeeded: false})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "del_file_c_prog", ResultHash: "rh_empty", ResultHint: "(命令执行完成，无输出)", Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "disable_service", ResultHash: "rh_fail", ResultHint: "[错误] 退出码: exit status 1", Succeeded: false})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "restore_default", ResultHash: "rh_empty", ResultHint: "(命令执行完成，无输出)", Succeeded: true})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatalf("bash batch with unique args and mixed success/failure should NOT trigger, got pattern=%q", result.Pattern)
	}
}

func TestFrequencyAnomaly_MemorySave_AllUniqueArgs_NotDetected(t *testing.T) {
	// memory(save) with all-unique ArgsHash and same results.
	// Under the argument-diversity mechanism, ALL-unique args = batch operation
	// signal, regardless of tool name. This is the accepted trade-off:
	// memory(save) 4x with genuinely different content is indistinguishable
	// from a batch operation at the frequency detector level. The memory loop
	// scenario is handled by max iterations (P2), while bash batch cleanup
	// being killed is P0.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "s1", ResultHash: "rs1", ResultHint: "done", Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "unique_m1", ResultHash: "same_hash", ResultHint: "已保存", Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "unique_m2", ResultHash: "same_hash", ResultHint: "已保存", Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "unique_m3", ResultHash: "same_hash", ResultHint: "已保存", Succeeded: true})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "unique_m4", ResultHash: "same_hash", ResultHint: "已保存", Succeeded: true})

	result := d.DetectDrift()
	if result.Drifted {
		t.Fatal("memory(save) with ALL-unique args should NOT trigger — argument diversity signals batch operation (P2 handled by max iterations)")
	}
}

func TestFrequencyAnomaly_BashSameCommand_Retrying_StillDetected(t *testing.T) {
	// bash called 4 times but with REPEATED ArgsHash (same command retried).
	// freqArgsAllUnique returns false → no bypass → drift detected.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "install_cmd", ResultHash: "fail_hash", ResultHint: "[错误] 退出码: exit status 1", Succeeded: false})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "install_cmd", ResultHash: "fail_hash", ResultHint: "[错误] 退出码: exit status 1", Succeeded: false})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "install_cmd_v2", ResultHash: "fail_hash", ResultHint: "[错误] 退出码: exit status 1", Succeeded: false})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "install_cmd", ResultHash: "fail_hash", ResultHint: "[错误] 退出码: exit status 1", Succeeded: false})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("bash retrying same command (repeated ArgsHash) should still trigger drift")
	}
}

func TestFreqArgsAllUnique_AllUnique_True(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "bash", ArgsHash: "h1", Succeeded: true},
		{ToolName: "other", ArgsHash: "x", Succeeded: true},
		{ToolName: "bash", ArgsHash: "h2", Succeeded: true},
		{ToolName: "bash", ArgsHash: "h3", Succeeded: true},
		{ToolName: "bash", ArgsHash: "h4", Succeeded: true},
	}
	if !freqArgsAllUnique(window, "bash") {
		t.Fatal("all unique ArgsHash should return true")
	}
}

func TestFreqArgsAllUnique_HasDuplicate_False(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "bash", ArgsHash: "h1", Succeeded: true},
		{ToolName: "bash", ArgsHash: "h2", Succeeded: true},
		{ToolName: "bash", ArgsHash: "h1", Succeeded: true}, // duplicate
		{ToolName: "bash", ArgsHash: "h3", Succeeded: true},
	}
	if freqArgsAllUnique(window, "bash") {
		t.Fatal("duplicate ArgsHash should return false")
	}
}

func TestFreqArgsAllUnique_SingleCall_False(t *testing.T) {
	window := []ToolCallRecord{
		{ToolName: "bash", ArgsHash: "h1", Succeeded: true},
		{ToolName: "other", ArgsHash: "x", Succeeded: true},
	}
	if freqArgsAllUnique(window, "bash") {
		t.Fatal("single call should return false (need at least 2)")
	}
}

func TestFreqArgsAllUnique_FailureMajority_False(t *testing.T) {
	// When majority of calls fail, argument diversity does NOT indicate
	// progress — the LLM is trying different approaches that all fail.
	window := []ToolCallRecord{
		{ToolName: "bash", ArgsHash: "h1", Succeeded: false},
		{ToolName: "bash", ArgsHash: "h2", Succeeded: false},
		{ToolName: "bash", ArgsHash: "h3", Succeeded: false},
		{ToolName: "bash", ArgsHash: "h4", Succeeded: true},
	}
	if freqArgsAllUnique(window, "bash") {
		t.Fatal("majority failed calls should return false (not a successful batch)")
	}
}
