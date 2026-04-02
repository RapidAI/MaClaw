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

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
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

	var wrapper struct {
		Results []SkillSearchResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, err
	}
	return wrapper.Results, nil
}

// ClawHubMirrorURL is the China mirror for ClawHub skill search.
const ClawHubMirrorURL = "https://cn.clawhub-mirror.com"

// SearchAndInstall 搜索并自动安装最佳匹配的 Skill。
// 搜索顺序: SkillMarket → ClawHub 中国镜像 → GitHub。
// 搜索无结果时记录日志并返回 nil，不中断任务。
// 根据 SkillPurchaseMode 配置过滤结果：free_only 模式只选择免费 Skill。
func (s *SkillSearcher) SearchAndInstall(ctx context.Context, query string) (*SkillSearchResult, error) {
	// Step 1: SkillMarket (HubCenter)
	results, err := s.Search(ctx, query, nil, 5)
	if err != nil {
		log.Printf("[skill-search] skillmarket search error: %v", err)
	}
	if len(results) == 0 {
		// Step 2: ClawHub 中国镜像
		log.Printf("[skill-search] no skillmarket results for: %s, trying ClawHub mirror...", query)
		results = s.searchClawHubMirror(ctx, query)
	}
	if len(results) == 0 {
		// Step 3: GitHub fallback
		log.Printf("[skill-search] no ClawHub results for: %s, trying GitHub fallback...", query)
		return s.searchGitHubFallback(ctx, query)
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

// searchClawHubMirror queries the ClawHub China mirror for skills.
// API: GET /api/v1/search?q=<query> → {"results": [...]}
// Returns nil on any error (non-fatal fallback).
func (s *SkillSearcher) searchClawHubMirror(ctx context.Context, query string) []SkillSearchResult {
	endpoint := ClawHubMirrorURL + "/api/v1/search?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		log.Printf("[skill-search] clawhub mirror request error: %v", err)
		return nil
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[skill-search] clawhub mirror error: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[skill-search] clawhub mirror HTTP %d", resp.StatusCode)
		return nil
	}

	var raw struct {
		Results []struct {
			Slug        string  `json:"slug"`
			DisplayName string  `json:"displayName"`
			Summary     string  `json:"summary"`
			Version     string  `json:"version"`
			Score       float64 `json:"score"`
			UpdatedAt   int64   `json:"updatedAt"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("[skill-search] clawhub mirror decode error: %v", err)
		return nil
	}

	var results []SkillSearchResult
	for _, r := range raw.Results {
		name := r.DisplayName
		if name == "" {
			name = r.Slug
		}
		results = append(results, SkillSearchResult{
			ID:          r.Slug,
			Name:        name,
			Description: r.Summary,
			Score:       r.Score,
			Status:      "clawhub",
		})
	}
	if len(results) > 0 {
		log.Printf("[skill-search] clawhub mirror found %d results for: %s", len(results), query)
	}
	return results
}

// searchGitHubFallback searches GitHub for skill.yaml files when SkillMarket
// returns no results. Returns the first matching candidate as a SkillSearchResult.
func (s *SkillSearcher) searchGitHubFallback(ctx context.Context, query string) (*SkillSearchResult, error) {
	gs := cskill.NewGitHubSearcher("")
	candidates, err := gs.SearchGitHub(query)
	if err != nil {
		log.Printf("[skill-search] GitHub fallback error: %v", err)
		return nil, nil
	}
	if len(candidates) == 0 {
		log.Printf("[skill-search] GitHub fallback: no results for query: %s", query)
		return nil, nil
	}
	best := candidates[0]
	log.Printf("[skill-search] GitHub fallback found: %s (★%d)", best.RepoFullName, best.Stars)
	return &SkillSearchResult{
		ID:          best.RepoFullName,
		Name:        best.RepoFullName,
		Description: best.Description,
		Status:      "github",
	}, nil
}
