package main

import "testing"

// ── Task 21.7: Auto Upload Trigger 单元测试 ─────────────────────────────

func TestShouldUpload_MeetsAllConditions(t *testing.T) {
	trigger := NewAutoUploadTrigger(nil, "dev@test.com")

	// 记录 3 次执行，评分都是 +2，有变更
	trigger.RecordExecution("my-skill", 2, "hash-v2")
	trigger.RecordExecution("my-skill", 2, "hash-v2")
	trigger.RecordExecution("my-skill", 1, "hash-v2")

	if !trigger.ShouldUpload("my-skill") {
		t.Error("should upload: all conditions met")
	}
}

func TestShouldUpload_InsufficientExecCount(t *testing.T) {
	trigger := NewAutoUploadTrigger(nil, "dev@test.com")

	// 只执行 2 次（不足 3 次）
	trigger.RecordExecution("my-skill", 2, "hash-v2")
	trigger.RecordExecution("my-skill", 2, "hash-v2")

	if trigger.ShouldUpload("my-skill") {
		t.Error("should not upload: exec count < 3")
	}
}

func TestShouldUpload_LowRating(t *testing.T) {
	trigger := NewAutoUploadTrigger(nil, "dev@test.com")

	// 3 次执行但评分低
	trigger.RecordExecution("my-skill", 0, "hash-v2")
	trigger.RecordExecution("my-skill", 0, "hash-v2")
	trigger.RecordExecution("my-skill", 1, "hash-v2")

	// avg = (0+0+1)/3 ≈ 0.33 < 1.0
	if trigger.ShouldUpload("my-skill") {
		t.Error("should not upload: avg rating < 1.0")
	}
}

func TestShouldUpload_NoChange(t *testing.T) {
	trigger := NewAutoUploadTrigger(nil, "dev@test.com")

	// 3 次执行，评分好，但已上传过且无变更
	trigger.RecordExecution("my-skill", 2, "hash-v1")
	trigger.RecordExecution("my-skill", 2, "hash-v1")
	trigger.RecordExecution("my-skill", 2, "hash-v1")

	// 模拟已上传
	trigger.mu.Lock()
	trigger.tracker["my-skill"].LastUploaded = "hash-v1"
	trigger.mu.Unlock()

	if trigger.ShouldUpload("my-skill") {
		t.Error("should not upload: no local change")
	}
}

func TestShouldUpload_UnknownSkill(t *testing.T) {
	trigger := NewAutoUploadTrigger(nil, "dev@test.com")
	if trigger.ShouldUpload("nonexistent") {
		t.Error("should not upload: unknown skill")
	}
}

func TestShouldUpload_EmptyHash(t *testing.T) {
	trigger := NewAutoUploadTrigger(nil, "dev@test.com")

	trigger.RecordExecution("my-skill", 2, "")
	trigger.RecordExecution("my-skill", 2, "")
	trigger.RecordExecution("my-skill", 2, "")

	if trigger.ShouldUpload("my-skill") {
		t.Error("should not upload: empty hash")
	}
}

func TestRecordExecution_RecentScoresCapped(t *testing.T) {
	trigger := NewAutoUploadTrigger(nil, "dev@test.com")

	// 记录 15 次，应只保留最近 10 次
	for i := 0; i < 15; i++ {
		trigger.RecordExecution("my-skill", i%3, "hash")
	}

	trigger.mu.Lock()
	rec := trigger.tracker["my-skill"]
	trigger.mu.Unlock()

	if len(rec.RecentScores) != 10 {
		t.Errorf("recent scores len=%d, want 10", len(rec.RecentScores))
	}
	if rec.ExecCount != 15 {
		t.Errorf("exec count=%d, want 15", rec.ExecCount)
	}
}
