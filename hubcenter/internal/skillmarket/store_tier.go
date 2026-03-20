package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ── TierRepository implementation ───────────────────────────────────────

// GetTier 获取上传者信誉等级。
func (s *Store) GetTier(ctx context.Context, userID string) (*UploaderTier, error) {
	var t UploaderTier
	var updatedAt string
	err := s.readDB.QueryRowContext(ctx, `
		SELECT user_id, tier, published_count, avg_rating, total_downloads, updated_at
		FROM sm_uploader_tiers WHERE user_id = ?`, userID,
	).Scan(&t.UserID, &t.Tier, &t.PublishedCount, &t.AvgRating, &t.TotalDownloads, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &UploaderTier{UserID: userID, Tier: 1, UpdatedAt: time.Now()}, nil
		}
		return nil, err
	}
	t.UpdatedAt = parseTime(updatedAt)
	return &t, nil
}

// UpsertTier 插入或更新上传者信誉等级。
func (s *Store) UpsertTier(ctx context.Context, t *UploaderTier) error {
	now := time.Now().Format(timeFmt)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_uploader_tiers (user_id, tier, published_count, avg_rating, total_downloads, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			tier = excluded.tier,
			published_count = excluded.published_count,
			avg_rating = excluded.avg_rating,
			total_downloads = excluded.total_downloads,
			updated_at = excluded.updated_at`,
		t.UserID, t.Tier, t.PublishedCount, t.AvgRating, t.TotalDownloads, now)
	return err
}
