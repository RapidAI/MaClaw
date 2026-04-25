package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
)

// SkillUsageRecord 鐠佹澘缍嶉崡鏇氶嚋 Skill 閻ㄥ嫪濞囬悽銊﹀剰閸愮偣鈧?
type SkillUsageRecord struct {
	ExecCount    int
	RecentScores []int
	LocalHash    string // 閺堫剙婀撮弬鍥︽閸愬懎顔?hash閿涘瞼鏁ゆ禍搴㈩梾濞村褰夐弴?
	LastUploaded string // 娑撳﹥顐兼稉濠佺炊閺冨墎娈?hash
}

// AutoUploadTrigger MaClaw 娓氀嗗殰閸斻劋绗傛导鐘盒曢崣鎴濇珤閵?
type AutoUploadTrigger struct {
	mu      sync.Mutex
	tracker map[string]*SkillUsageRecord // key: skill name
	client  *SkillMarketClient
	emailFn func() string // dynamic email getter to avoid stale config
}

// NewAutoUploadTrigger 閸掓稑缂撻懛顏勫З娑撳﹣绱剁憴锕€褰傞崳銊ｂ偓?
func NewAutoUploadTrigger(client *SkillMarketClient, emailFn func() string) *AutoUploadTrigger {
	return &AutoUploadTrigger{
		tracker: make(map[string]*SkillUsageRecord),
		client:  client,
		emailFn: emailFn,
	}
}

// RecordExecution 鐠佹澘缍嶆稉鈧▎?Skill 閹笛嗩攽閸欏﹤鍙剧拠鍕瀻閵?
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
	// 閸欘亙绻氶悾娆愭付鏉?10 濞喡ょ槑閸?
	if len(rec.RecentScores) > 10 {
		rec.RecentScores = rec.RecentScores[len(rec.RecentScores)-10:]
	}
	if strings.TrimSpace(localHash) != "" {
		rec.LocalHash = localHash
	}
}

// ShouldUpload 閸掋倖鏌?Skill 閺勵垰鎯佸陇鍐婚懛顏勫З娑撳﹣绱堕弶鈥叉閵?
// 閺夆€叉閿涙碍澧界悰灞绢偧閺?閳?3 娑撴梹娓舵潻鎴ｇ槑閸掑棗閽╅崸?閳?+1 娑撴梹婀伴崷鎵閺堫剚婀侀崣妯绘纯閵?
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
	// 鐠侊紕鐣婚張鈧潻鎴ｇ槑閸掑棗閽╅崸鍥р偓?
	sum := 0
	for _, s := range rec.RecentScores {
		sum += s
	}
	avg := float64(sum) / float64(len(rec.RecentScores))
	if avg < 1.0 {
		return false
	}
	// 濡偓閺屻儲婀伴崷鐗堟Ц閸氾附婀侀崣妯绘纯
	if rec.LocalHash == "" || rec.LocalHash == rec.LastUploaded {
		return false
	}
	return true
}

// CheckAndTrigger 閸?Skill 閹笛嗩攽鐎瑰本鍨氶崥搴ょ殶閻㈩煉绱濋崚銈嗘焽閺勵垰鎯佺憴锕€褰傛稉濠佺炊閵?
// 濞夈劍鍓伴敍姘劃閺傝纭舵导姘帥閹垫挸瀵橀崘宥呭灲閺傤叏绱濋柅鍌氭値婢舵牠鍎撮惄瀛樺复鐠嬪啰鏁ら妴?
// SkillRunner 閸愬懘鍎存担璺ㄦ暏 RecordExecution + ShouldUpload + SubmitAndMark 閸掑棙顒炵拫鍐暏娴犮儵浼╅崗宥勭瑝韫囧懓顩﹂惃鍕ⅵ閸栧懌鈧?
func (t *AutoUploadTrigger) CheckAndTrigger(ctx context.Context, skillName, zipPath, localHash string, execResult *SkillExecutionResult) error {
	t.RecordExecution(skillName, EvaluateSkillExecution(execResult), localHash)

	if !t.ShouldUpload(skillName) {
		return nil
	}

	return t.SubmitAndMark(ctx, skillName, zipPath, localHash)
}

// SubmitAndMark 娑撳﹣绱?zip 楠炶埖鐖ｇ拋鏉垮嚒娑撳﹣绱?hash閵?
func (t *AutoUploadTrigger) SubmitAndMark(ctx context.Context, skillName, zipPath, localHash string) error {
	email := t.emailFn()
	if email == "" {
		return fmt.Errorf("remote_email is not configured, skipping auto upload")
	}

	log.Printf("[auto-upload] triggering upload for skill %s", skillName)
	submissionID, err := t.client.SubmitSkill(ctx, zipPath, email)
	if err != nil {
		return err
	}
	log.Printf("[auto-upload] submitted skill %s, submission_id=%s", skillName, submissionID)

	t.mu.Lock()
	if rec, ok := t.tracker[skillName]; ok {
		rec.LastUploaded = localHash
	}
	t.mu.Unlock()

	return nil
}
