package skillmarket

import (
	"context"
	"fmt"
)

// RatingService 管理 Skill 评分。
type RatingService struct {
	store *Store
}

// NewRatingService 创建 RatingService。
func NewRatingService(store *Store) *RatingService {
	return &RatingService{store: store}
}

// SubmitRating 提交评分。
// score 范围 -2 ~ +2，email == uploaderEmail 时拒绝（不能给自己评分）。
// -2 分触发紧急下架（返回 emergencyTakedown=true）。
func (s *RatingService) SubmitRating(ctx context.Context, skillID, email string, score int, uploaderEmail string) (emergencyTakedown bool, err error) {
	if score < -2 || score > 2 {
		return false, fmt.Errorf("score must be between -2 and +2")
	}
	if email == uploaderEmail {
		return false, fmt.Errorf("cannot rate your own skill")
	}
	r := &Rating{
		SkillID: skillID,
		Email:   email,
		Score:   score,
	}
	if err := s.store.UpsertRating(ctx, r); err != nil {
		return false, err
	}
	return score == -2, nil
}

// GetStats 返回 Skill 的评分统计。
func (s *RatingService) GetStats(ctx context.Context, skillID string) (*RatingStats, error) {
	return s.store.GetRatingStats(ctx, skillID)
}

// GetRatings 返回 Skill 的所有评分列表。
func (s *RatingService) GetRatings(ctx context.Context, skillID string) ([]Rating, error) {
	return s.store.ListRatingsBySkill(ctx, skillID)
}
