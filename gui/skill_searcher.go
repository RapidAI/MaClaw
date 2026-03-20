package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SkillSearchResult 搜索结果条目。
type SkillSearchResult struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          []string `json:"tags"`
	Score         float64  `json:"score"`
	Price         int      `json:"price"`
	Status        string   `json:"status"`
	AvgRating     float64  `json:"avg_rating"`
	DownloadCount int      `json:"download_count"`
}

// SkillSearcher MaClaw 端智能搜索模块。
type SkillSearcher struct {
	client *SkillMarketClient
}

// NewSkillSearcher 创建搜索模块。
func NewSkillSearcher(client *SkillMarketClient) *SkillSearcher {
	return &SkillSearcher{client: client}
}

// Search 调用 HubCenter 搜索 API。
func (s *SkillSearcher) Search(ctx context.Context, query string, tags []string, topN int) ([]SkillSearchResult, error) {
	base := s.client.baseURL()
	if base == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}
	if topN <= 0 {
		topN = 20
	}

	params := url.Values{}
	params.Set("q", query)
	if len(tags) > 0 {
		params.Set("tags", strings.Join(tags, ","))
	}
	params.Set("top_n", fmt.Sprintf("%d", topN))

	reqURL := base + "/api/v1/skillmarket/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search failed: %s", resp.Status)
	}

	var results []SkillSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	return results, nil
}

// SearchAndInstall 搜索并自动安装最佳匹配的 Skill。
// 搜索无结果时记录日志并返回 nil，不中断任务。
// 根据 SkillPurchaseMode 配置过滤结果：free_only 模式只选择免费 Skill。
func (s *SkillSearcher) SearchAndInstall(ctx context.Context, query string) (*SkillSearchResult, error) {
	results, err := s.Search(ctx, query, nil, 5)
	if err != nil {
		log.Printf("[skill-search] search error: %v", err)
		return nil, nil // 不中断任务
	}
	if len(results) == 0 {
		log.Printf("[skill-search] no results for query: %s", query)
		return nil, nil
	}

	// 根据 Skill获取策略 过滤
	mode := s.client.getSkillPurchaseMode()
	if mode == "free_only" {
		var filtered []SkillSearchResult
		for _, r := range results {
			if r.Price == 0 {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			log.Printf("[skill-search] no free results for query (free_only mode): %s", query)
			return nil, nil
		}
		results = filtered
	}

	// 选择第一个结果（已按质量排序）
	best := &results[0]
	log.Printf("[skill-search] found: %s (score=%.2f, rating=%.1f)", best.Name, best.Score, best.AvgRating)
	return best, nil
}
