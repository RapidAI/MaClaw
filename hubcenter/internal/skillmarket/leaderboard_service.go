package skillmarket

import (
	"sort"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

// LeaderboardEntry 排行榜条目。
type LeaderboardEntry struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          []string `json:"tags"`
	AvgRating     float64  `json:"avg_rating"`
	DownloadCount int      `json:"download_count"`
	CreatedAt     string   `json:"created_at"`
}

// LeaderboardService 提供排行榜查询。
type LeaderboardService struct {
	skillStore *skill.SkillStore
}

// NewLeaderboardService 创建 LeaderboardService。
func NewLeaderboardService(skillStore *skill.SkillStore) *LeaderboardService {
	return &LeaderboardService{skillStore: skillStore}
}

// GetTop 返回排行榜。sortBy: "rating" | "downloads" | "newest"，limit 1~50。
func (s *LeaderboardService) GetTop(sortBy string, limit int) []LeaderboardEntry {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// 获取所有 visible Skill
	result := s.skillStore.Search("", nil, 1)
	total := result.Total
	if total == 0 {
		return nil
	}

	var all []skill.HubSkillMeta
	page := 1
	for {
		result = s.skillStore.Search("", nil, page)
		for _, m := range result.Skills {
			if m.Visible {
				all = append(all, m)
			}
		}
		if page*40 >= total {
			break
		}
		page++
	}

	switch sortBy {
	case "downloads":
		sort.Slice(all, func(i, j int) bool { return all[i].Downloads > all[j].Downloads })
	case "newest":
		sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt > all[j].CreatedAt })
	default: // "rating"
		sort.Slice(all, func(i, j int) bool { return all[i].AvgRating > all[j].AvgRating })
	}

	if limit > len(all) {
		limit = len(all)
	}
	entries := make([]LeaderboardEntry, limit)
	for i := 0; i < limit; i++ {
		m := all[i]
		entries[i] = LeaderboardEntry{
			ID:            m.ID,
			Name:          m.Name,
			Description:   m.Description,
			Tags:          m.Tags,
			AvgRating:     m.AvgRating,
			DownloadCount: m.Downloads,
			CreatedAt:     m.CreatedAt,
		}
	}
	return entries
}
