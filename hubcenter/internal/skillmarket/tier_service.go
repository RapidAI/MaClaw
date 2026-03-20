package skillmarket

import (
	"context"
	"time"
)

// TierLimits 是某个等级对应的限制。
type TierLimits struct {
	MaxUploadSizeMB int // 最大上传大小 (MB)
	MaxPerHour      int // 每小时最大提交数
	MaxPerDay       int // 每天最大提交数
}

// 默认等级限制
var defaultTierLimits = map[int]TierLimits{
	1: {MaxUploadSizeMB: 10, MaxPerHour: 5, MaxPerDay: 20},
	2: {MaxUploadSizeMB: 20, MaxPerHour: 10, MaxPerDay: 40},
	3: {MaxUploadSizeMB: 50, MaxPerHour: 20, MaxPerDay: 80},
	4: {MaxUploadSizeMB: 100, MaxPerHour: 50, MaxPerDay: 200},
}

// TierService 管理上传者信誉等级。
type TierService struct {
	store *Store
}

// NewTierService 创建 TierService。
func NewTierService(store *Store) *TierService {
	return &TierService{store: store}
}

// GetTier 获取用户当前等级。
func (s *TierService) GetTier(ctx context.Context, userID string) (*UploaderTier, error) {
	return s.store.GetTier(ctx, userID)
}

// GetLimits 根据等级返回限制。
func (s *TierService) GetLimits(tier int) TierLimits {
	if l, ok := defaultTierLimits[tier]; ok {
		return l
	}
	return defaultTierLimits[1]
}

// RecalculateTier 根据指标重新计算等级（可升可降）。
func (s *TierService) RecalculateTier(ctx context.Context, userID string, publishedCount int, avgRating float64, totalDownloads int) error {
	tier := 1
	if publishedCount >= 3 && avgRating >= 0.5 && totalDownloads >= 50 {
		tier = 2
	}
	if publishedCount >= 10 && avgRating >= 1.0 && totalDownloads >= 200 {
		tier = 3
	}
	if publishedCount >= 25 && avgRating >= 1.5 && totalDownloads >= 1000 {
		tier = 4
	}
	return s.store.UpsertTier(ctx, &UploaderTier{
		UserID:         userID,
		Tier:           tier,
		PublishedCount: publishedCount,
		AvgRating:      avgRating,
		TotalDownloads: totalDownloads,
		UpdatedAt:      time.Now(),
	})
}
