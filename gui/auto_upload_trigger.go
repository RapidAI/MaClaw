package main

import (
	"context"
	"log"
	"sync"
)

// SkillUsageRecord 记录单个 Skill 的使用情况。
type SkillUsageRecord struct {
	ExecCount    int
	RecentScores []int
	LocalHash    string // 本地文件内容 hash，用于检测变更
	LastUploaded string // 上次上传时的 hash
}

// AutoUploadTrigger MaClaw 侧自动上传触发器。
type AutoUploadTrigger struct {
	mu      sync.Mutex
	tracker map[string]*SkillUsageRecord // key: skill name
	client  *SkillMarketClient
	email   string
}

// NewAutoUploadTrigger 创建自动上传触发器。
func NewAutoUploadTrigger(client *SkillMarketClient, email string) *AutoUploadTrigger {
	return &AutoUploadTrigger{
		tracker: make(map[string]*SkillUsageRecord),
		client:  client,
		email:   email,
	}
}

// RecordExecution 记录一次 Skill 执行及其评分。
func (t *AutoUploadTrigger) RecordExecution(skillName string, score int, localHash string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rec, ok := t.tracker[skillName]
	if !ok {
		rec = &SkillUsageRecord{}
		t.tracker[skillName] = rec
	}
	rec.ExecCount++
	rec.RecentScores = append(rec.RecentScores, score)
	// 只保留最近 10 次评分
	if len(rec.RecentScores) > 10 {
		rec.RecentScores = rec.RecentScores[len(rec.RecentScores)-10:]
	}
	rec.LocalHash = localHash
}

// ShouldUpload 判断 Skill 是否满足自动上传条件。
// 条件：执行次数 ≥ 3 且最近评分平均 ≥ +1 且本地版本有变更。
func (t *AutoUploadTrigger) ShouldUpload(skillName string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	rec, ok := t.tracker[skillName]
	if !ok {
		return false
	}
	if rec.ExecCount < 3 {
		return false
	}
	if len(rec.RecentScores) == 0 {
		return false
	}
	// 计算最近评分平均值
	sum := 0
	for _, s := range rec.RecentScores {
		sum += s
	}
	avg := float64(sum) / float64(len(rec.RecentScores))
	if avg < 1.0 {
		return false
	}
	// 检查本地是否有变更
	if rec.LocalHash == "" || rec.LocalHash == rec.LastUploaded {
		return false
	}
	return true
}

// CheckAndTrigger 在 Skill 执行完成后调用，判断是否触发上传。
func (t *AutoUploadTrigger) CheckAndTrigger(ctx context.Context, skillName, zipPath, localHash string, execResult *SkillExecutionResult) error {
	t.RecordExecution(skillName, EvaluateSkillExecution(execResult), localHash)

	if !t.ShouldUpload(skillName) {
		return nil
	}

	log.Printf("[auto-upload] triggering upload for skill %s", skillName)
	submissionID, err := t.client.SubmitSkill(ctx, zipPath, t.email)
	if err != nil {
		return err
	}
	log.Printf("[auto-upload] submitted skill %s, submission_id=%s", skillName, submissionID)

	// 标记已上传
	t.mu.Lock()
	if rec, ok := t.tracker[skillName]; ok {
		rec.LastUploaded = localHash
	}
	t.mu.Unlock()

	return nil
}
