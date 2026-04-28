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
	// Reproduces the exact bug: LLM calls memory(save) 4 times with
	// different content (different ArgsHash) in a window of 5 calls.
	// Layer 1 doesn't fire (different ArgsHash). Layer 2 should fire
	// because memory dominates the window (4/5 = 80% > 75%).
	d := NewDriftDetector(8, 0.8)

	// One non-memory call at the start (e.g., ssh exec that completed the task)
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "ssh_hash_1", ResultHint: "OmniRoute 降级成功"})

	// Four memory(save) calls with different content
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "mem_hash_1", ResultHint: "已保存记忆: 标准操作流程..."})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "mem_hash_2", ResultHint: "已保存记忆: 版本切换完整流程..."})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "mem_hash_3", ResultHint: "已保存记忆: 踩坑记录..."})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "mem_hash_4", ResultHint: "已保存记忆: API 服务器信息..."})

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
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "h1"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1"})
	d.Record(ToolCallRecord{ToolName: "bash", ArgsHash: "h2"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m4"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m5"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m6"})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("memory at 75% of window should trigger frequency anomaly")
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
	// PreviewDrift should also detect Layer 2 anomalies.
	d := NewDriftDetector(8, 0.8)

	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "ssh_1"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m4"})

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

	// First drift
	d.Record(ToolCallRecord{ToolName: "ssh", ArgsHash: "s1"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m4"})

	result1 := d.DetectDrift()
	if !result1.Drifted {
		t.Fatal("first drift should be detected")
	}
	if result1.NeedHumanHelp {
		t.Fatal("first drift should NOT need human help")
	}

	d.ResetWindow()

	// Second drift (LLM ignored the warning)
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m5"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m6"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m7"})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m8"})

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
	// Even with time gaps between calls, if the args are different,
	// it's not polling — it's a semantic loop. Time span doesn't matter
	// for Layer 2 (it only matters for Layer 1's slow poll exemption).
	d := NewDriftDetector(8, 0.8)

	now := time.Now()
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m1", Timestamp: now})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m2", Timestamp: now.Add(10 * time.Second)})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m3", Timestamp: now.Add(20 * time.Second)})
	d.Record(ToolCallRecord{ToolName: "memory", ArgsHash: "m4", Timestamp: now.Add(30 * time.Second)})

	result := d.DetectDrift()
	if !result.Drifted {
		t.Fatal("semantic loop should be detected regardless of time span")
	}
	if result.Pattern != "frequency" {
		t.Errorf("expected pattern='frequency', got %q", result.Pattern)
	}
}
