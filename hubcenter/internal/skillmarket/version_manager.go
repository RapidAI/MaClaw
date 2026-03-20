package skillmarket

import (
	"context"
	"fmt"
)

// VersionManager 管理 Skill 版本（去重与升级）。
type VersionManager struct {
	store *Store
}

// NewVersionManager 创建 VersionManager。
func NewVersionManager(store *Store) *VersionManager {
	return &VersionManager{store: store}
}

// VersionResolution 是版本解析结果。
type VersionResolution struct {
	IsUpgrade   bool
	NextVersion int
	PrevSkillID string // 上一个版本的 Skill ID（如果是升级）
}

// ResolveSubmission 根据 fingerprint 判断是新建还是版本升级。
// fingerprint = uploader_email + ":" + skill_name
func (m *VersionManager) ResolveSubmission(ctx context.Context, fingerprint string) (*VersionResolution, error) {
	// 查询同 fingerprint 的最新成功提交
	sub, err := m.store.GetLatestSuccessSubmissionByFingerprint(ctx, fingerprint)
	if err != nil {
		if err == ErrNotFound {
			return &VersionResolution{IsUpgrade: false, NextVersion: 1}, nil
		}
		return nil, fmt.Errorf("query fingerprint: %w", err)
	}
	// 查询该 Skill 的当前版本号（从 submission 记录推断）
	count, err := m.store.CountSuccessSubmissionsByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, err
	}
	return &VersionResolution{
		IsUpgrade:   true,
		NextVersion: count + 1,
		PrevSkillID: sub.SkillID,
	}, nil
}
