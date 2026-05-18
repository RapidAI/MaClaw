package skillmarket

import (
	"context"
	"fmt"
	"strconv"
)

const (
	// configMaxUploadsPerHour 是管理员可配置的全局每小时上传 Skill 数限制。
	// 当设置为 >0 时，直接覆盖等级默认的 MaxPerHour 限制。
	// 设置为 0 或未设置时，使用等级默认限制。
	configMaxUploadsPerHour = "max_skill_uploads_per_hour"
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

	// 管理员配置的全局每小时限制：设置后直接覆盖等级默认限制。
	// 设置为 0 或未设置时，使用等级默认限制。
	maxPerHour := limits.MaxPerHour
	if globalLimit := rl.getGlobalMaxUploadsPerHour(ctx); globalLimit > 0 {
		maxPerHour = globalLimit
	}

	// 检查每小时限制
	hourly, err := rl.store.CountRecentSubmissions(ctx, email, 1)
	if err != nil {
		return fmt.Errorf("count hourly: %w", err)
	}
	if hourly >= maxPerHour {
		return fmt.Errorf("rate limit exceeded: %d submissions in last hour (max %d), retry later", hourly, maxPerHour)
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

// getGlobalMaxUploadsPerHour 从管理员配置中读取全局每小时上传限制。
// 返回 0 表示未设置或无效。
func (rl *RateLimiter) getGlobalMaxUploadsPerHour(ctx context.Context) int {
	val := rl.store.GetConfigWithDefault(ctx, configMaxUploadsPerHour, "0")
	n, err := strconv.Atoi(val)
	if err != nil || n < 0 {
		return 0
	}
	return n
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
