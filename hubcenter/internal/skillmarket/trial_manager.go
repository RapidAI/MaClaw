package skillmarket

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

const (
	configTrialDuration       = "trial_duration_days"
	configAutoPublishThreshold = "auto_publish_threshold"
	defaultTrialDays          = 7
	defaultAutoPublishCount   = 5
)

// TrialManager 管理 Skill 试用期生命周期。
type TrialManager struct {
	store      *Store
	skillStore *skill.SkillStore
	ratingSvc  *RatingService
}

// NewTrialManager 创建 TrialManager。
func NewTrialManager(store *Store, skillStore *skill.SkillStore, ratingSvc *RatingService) *TrialManager {
	return &TrialManager{store: store, skillStore: skillStore, ratingSvc: ratingSvc}
}

// OnSkillValidated 语法验证通过后调用，将 Skill 设为 trial 状态。
// 目前 SkillStore 没有 Status 字段，先通过 Publish 发布并设 Visible=true。
// TODO: 后续 Task 13.3 扩展 HubSkillMeta 后改为设置 Status="trial"。
func (m *TrialManager) OnSkillValidated(ctx context.Context, skillID string) error {
	// 当前实现：Skill 已通过 Publish 发布，这里只记录 trial 到期时间到 config
	days := m.getTrialDays(ctx)
	expireAt := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	return m.store.SetConfig(ctx, "trial_expire:"+skillID, expireAt.Format(timeFmt))
}

// CheckAutoPublish 检查 Skill 是否满足自动上架条件。
// 条件：评价人数 >= threshold 且平均分 >= 0。
func (m *TrialManager) CheckAutoPublish(ctx context.Context, skillID string) (bool, error) {
	threshold := m.getAutoPublishThreshold(ctx)
	stats, err := m.ratingSvc.GetStats(ctx, skillID)
	if err != nil {
		return false, err
	}
	return stats.UniqueRaters >= threshold && stats.AverageScore >= 0, nil
}

// ProcessExpiredTrials 扫描到期的 trial Skill，转为 pending_review。
// 目前简化实现：扫描 config 中 trial_expire: 前缀的条目。
func (m *TrialManager) ProcessExpiredTrials(ctx context.Context) {
	// 简化实现：由于 SkillStore 目前没有 Status 字段，
	// 这里只记录日志。后续 HubSkillMeta 扩展后完善。
	log.Printf("[skillmarket] ProcessExpiredTrials: checking expired trials")
}

// AdminApprove 管理员批准 pending_review 的 Skill。
func (m *TrialManager) AdminApprove(ctx context.Context, skillID string) error {
	// 设为 visible（当前等价于 published）
	return m.skillStore.SetVisibility(skillID, true)
}

// AdminReject 管理员拒绝 pending_review 的 Skill。
func (m *TrialManager) AdminReject(ctx context.Context, skillID string) error {
	// 设为不可见（当前等价于 rejected）
	return m.skillStore.SetVisibility(skillID, false)
}

// EmergencyTakedown 紧急下架（-2 评分触发）。
func (m *TrialManager) EmergencyTakedown(ctx context.Context, skillID string) error {
	log.Printf("[skillmarket] emergency takedown: skill %s", skillID)
	return m.skillStore.SetVisibility(skillID, false)
}

func (m *TrialManager) getTrialDays(ctx context.Context) int {
	val := m.store.GetConfigWithDefault(ctx, configTrialDuration, strconv.Itoa(defaultTrialDays))
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return defaultTrialDays
	}
	return n
}

func (m *TrialManager) getAutoPublishThreshold(ctx context.Context) int {
	val := m.store.GetConfigWithDefault(ctx, configAutoPublishThreshold, strconv.Itoa(defaultAutoPublishCount))
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return defaultAutoPublishCount
	}
	return n
}
