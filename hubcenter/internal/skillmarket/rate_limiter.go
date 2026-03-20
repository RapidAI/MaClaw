package skillmarket

import (
	"context"
	"fmt"
)

// RateLimiter 检查上传频率限制。
type RateLimiter struct {
	store   *Store
	tierSvc *TierService
}

// NewRateLimiter 创建 RateLimiter。
func NewRateLimiter(store *Store, tierSvc *TierService) *RateLimiter {
	return &RateLimiter{store: store, tierSvc: tierSvc}
}

// TierSvc 返回关联的 TierService。
func (rl *RateLimiter) TierSvc() *TierService { return rl.tierSvc }

// CheckRateLimit 检查 email 是否超过频率限制。
// 返回 nil 表示允许，否则返回包含 retry 信息的错误。
func (rl *RateLimiter) CheckRateLimit(ctx context.Context, email, userID string) error {
	tier, err := rl.tierSvc.GetTier(ctx, userID)
	if err != nil {
		return fmt.Errorf("get tier: %w", err)
	}
	limits := rl.tierSvc.GetLimits(tier.Tier)

	// 检查每小时限制
	hourly, err := rl.store.CountRecentSubmissions(ctx, email, 1)
	if err != nil {
		return fmt.Errorf("count hourly: %w", err)
	}
	if hourly >= limits.MaxPerHour {
		return fmt.Errorf("rate limit exceeded: %d submissions in last hour (max %d), retry later", hourly, limits.MaxPerHour)
	}

	// 检查每天限制
	daily, err := rl.store.CountRecentDailySubmissions(ctx, email)
	if err != nil {
		return fmt.Errorf("count daily: %w", err)
	}
	if daily >= limits.MaxPerDay {
		return fmt.Errorf("rate limit exceeded: %d submissions today (max %d), retry tomorrow", daily, limits.MaxPerDay)
	}

	return nil
}

// CheckSizeLimit 检查 zip 大小是否超过等级限制。
func (rl *RateLimiter) CheckSizeLimit(ctx context.Context, userID string, sizeBytes int64) error {
	tier, err := rl.tierSvc.GetTier(ctx, userID)
	if err != nil {
		return fmt.Errorf("get tier: %w", err)
	}
	limits := rl.tierSvc.GetLimits(tier.Tier)
	maxBytes := int64(limits.MaxUploadSizeMB) << 20
	if sizeBytes > maxBytes {
		return fmt.Errorf("file too large: %d bytes (max %dMB for tier %d)", sizeBytes, limits.MaxUploadSizeMB, tier.Tier)
	}
	return nil
}
