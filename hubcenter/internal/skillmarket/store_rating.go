package skillmarket

import (
	"context"
	"time"
)

// ── RatingRepository implementation ─────────────────────────────────────

// UpsertRating 插入或更新评分（email 去重，覆盖旧评分）。
func (s *Store) UpsertRating(ctx context.Context, r *Rating) error {
	now := time.Now().Format(timeFmt)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_ratings (skill_id, email, score, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(skill_id, email) DO UPDATE SET score = excluded.score, updated_at = excluded.updated_at`,
		r.SkillID, r.Email, r.Score, now, now)
	return err
}

// GetRatingStats 返回 Skill 的评分统计（去重后）。
func (s *Store) GetRatingStats(ctx context.Context, skillID string) (*RatingStats, error) {
	var stats RatingStats
	stats.SkillID = skillID
	err := s.readDB.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(score), 0), COALESCE(AVG(score), 0)
		FROM sm_ratings WHERE skill_id = ?`, skillID,
	).Scan(&stats.UniqueRaters, &stats.TotalScore, &stats.AverageScore)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// ListRatingsBySkill 返回 Skill 的所有评分。
func (s *Store) ListRatingsBySkill(ctx context.Context, skillID string) ([]Rating, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT skill_id, email, score, created_at, updated_at
		FROM sm_ratings WHERE skill_id = ? ORDER BY updated_at DESC`, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rating
	for rows.Next() {
		var r Rating
		var createdAt, updatedAt string
		if err := rows.Scan(&r.SkillID, &r.Email, &r.Score, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(createdAt)
		r.UpdatedAt = parseTime(updatedAt)
		out = append(out, r)
	}
	return out, rows.Err()
}
