package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
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
	InstallRef    string   `json:"install_ref,omitempty"`
	AvgRating     float64  `json:"avg_rating"`
	DownloadCount int      `json:"download_count"`
	Version       string   `json:"version,omitempty"`
	Author        string   `json:"author,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
}

// MixedSkillSearchResult is the GUI-facing unified search result model.
type MixedSkillSearchResult struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          []string `json:"tags"`
	Source        string   `json:"source"`
	SourceLabel   string   `json:"source_label"`
	InstallRef    string   `json:"install_ref,omitempty"`
	FilePath      string   `json:"file_path,omitempty"`
	Version       string   `json:"version,omitempty"`
	Author        string   `json:"author,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
	TrustLevel    string   `json:"trust_level,omitempty"`
	AvgRating     float64  `json:"avg_rating"`
	RatingCount   int      `json:"rating_count"`
	Downloads     int      `json:"downloads"`
	Score         float64  `json:"score"`
	Price         int      `json:"price"`
	RepoURL       string   `json:"repo_url,omitempty"`
	Installed     bool     `json:"installed"`
	InstalledName string   `json:"installed_name,omitempty"`
	CanUpdate     bool     `json:"can_update"`
	HasUpdate     bool     `json:"has_update"`
}

// SkillSearcher MaClaw 端智能搜索模块。
type SkillSearcher struct {
	client *SkillMarketClient
	app    *App
}

// NewSkillSearcher 创建搜索模块。
func NewSkillSearcher(client *SkillMarketClient) *SkillSearcher {
	var app *App
	if client != nil {
		app = client.app
	}
	return &SkillSearcher{client: client, app: app}
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

// ClawHubMirrorURL re-exports the shared constant for backward compatibility.
// New code should use cskill.ClawHubMirrorURL directly.
const ClawHubMirrorURL = cskill.ClawHubMirrorURL

// SearchAll aggregates SkillMarket, ClawHub mirror, and GitHub results for GUI search.
func (s *SkillSearcher) SearchAll(ctx context.Context, query string) ([]MixedSkillSearchResult, error) {
	var results []MixedSkillSearchResult
	var errs []string

	marketResults, err := s.Search(ctx, query, nil, 10)
	if err != nil {
		errs = append(errs, fmt.Sprintf("skillmarket: %v", err))
	} else {
		for _, r := range marketResults {
			results = append(results, s.toMixedSkillSearchResult(r))
		}
	}

	// ClawHub + GitHub via shared HubClient (single implementation).
	hubClient := cskill.NewHubClient()
	for _, r := range hubClient.SearchClawHub(ctx, query) {
		results = append(results, hubSearchResultToMixed(r))
	}
	if ctx.Err() == nil {
		for _, r := range hubClient.SearchGitHub(query) {
			results = append(results, hubSearchResultToMixed(r))
		}
	}

	s.enrichInstalledState(results)
	if len(results) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	sort.SliceStable(results, func(i, j int) bool {
		li := sourcePriority(results[i].Source)
		lj := sourcePriority(results[j].Source)
		if li != lj {
			return li < lj
		}
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})
	return results, nil
}

func sourcePriority(source string) int {
	switch source {
	case "skillmarket":
		return 0
	case "clawhub":
		return 1
	case "github":
		return 2
	default:
		return 3
	}
}

func mixedSourceLabel(source string) string {
	switch source {
	case "skillmarket":
		return "SkillMarket"
	case "clawhub":
		return "ClawHub"
	case "github":
		return "GitHub"
	default:
		return source
	}
}

func (s *SkillSearcher) toMixedSkillSearchResult(r SkillSearchResult) MixedSkillSearchResult {
	source := "skillmarket"
	if r.Status == "clawhub" {
		source = "clawhub"
	}
	return MixedSkillSearchResult{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Tags:        r.Tags,
		Source:      source,
		SourceLabel: mixedSourceLabel(source),
		TrustLevel:  mixedTrustLevel(source),
		AvgRating:   r.AvgRating,
		Downloads:   r.DownloadCount,
		Score:       r.Score,
		Price:       r.Price,
		Version:     r.Version,
		Author:      r.Author,
		CreatedAt:   r.CreatedAt,
	}
}

func mixedTrustLevel(source string) string {
	switch source {
	case "clawhub", "github":
		return "community"
	default:
		return ""
	}
}

// hubSearchResultToMixed converts a shared HubSearchResult (from corelib/skill.HubClient)
// to the GUI-specific MixedSkillSearchResult display type.
func hubSearchResultToMixed(r cskill.HubSearchResult) MixedSkillSearchResult {
	return MixedSkillSearchResult{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Source:      r.Source,
		SourceLabel: mixedSourceLabel(r.Source),
		TrustLevel:  mixedTrustLevel(r.Source),
		Version:     r.Version,
		Author:      r.Author,
		AvgRating:   r.AvgRating,
		Downloads:   r.Downloads,
		Score:       r.Score,
		InstallRef:  r.InstallRef,
		FilePath:    r.FilePath,
		RepoURL:     r.RepoURL,
	}
}

func (s *SkillSearcher) enrichInstalledState(results []MixedSkillSearchResult) {
	if s.app == nil || s.app.skillExecutor == nil {
		return
	}
	skills := s.app.skillExecutor.loadSkills()
	updatesByHubID := map[string]bool{}
	for _, update := range s.app.checkHubSkillUpdatesSafe() {
		for _, skill := range skills {
			if skill.Name == update.SkillName && skill.HubSkillID != "" {
				updatesByHubID[skill.HubSkillID] = true
			}
		}
	}
	for i := range results {
		for _, skill := range skills {
			if mixedResultMatchesSkill(results[i], skill) {
				results[i].Installed = true
				results[i].InstalledName = skill.Name
				if skill.Source == "hub" && skill.HubSkillID != "" {
					results[i].CanUpdate = true
					results[i].HasUpdate = updatesByHubID[skill.HubSkillID]
				}
				break
			}
		}
	}
}

func mixedResultMatchesSkill(result MixedSkillSearchResult, skill corelib.NLSkillEntry) bool {
	switch result.Source {
	case "skillmarket":
		return skill.Source == "hub" && skill.HubSkillID == result.ID
	case "clawhub":
		return skill.Source == "clawhub" && strings.EqualFold(skill.Name, result.Name)
	case "github":
		return skill.Source == "github" && (strings.EqualFold(skill.SourceProject, result.RepoURL) || strings.EqualFold(skill.Name, result.Name))
	default:
		return false
	}
}

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
// Delegates to the shared HubClient and converts results to the legacy
// SkillSearchResult format (used by SearchAndInstall).
func (s *SkillSearcher) searchClawHubMirror(ctx context.Context, query string) []SkillSearchResult {
	hubClient := cskill.NewHubClient()
	hubResults := hubClient.SearchClawHub(ctx, query)
	if len(hubResults) == 0 {
		return nil
	}
	results := make([]SkillSearchResult, 0, len(hubResults))
	for _, r := range hubResults {
		results = append(results, SkillSearchResult{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			Score:       r.Score,
			Status:      "clawhub",
		})
	}
	log.Printf("[skill-search] clawhub mirror found %d results for: %s", len(results), query)
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
	installRefBytes, _ := json.Marshal(best)
	log.Printf("[skill-search] GitHub fallback found: %s (★%d)", best.RepoFullName, best.Stars)
	return &SkillSearchResult{
		ID:          best.RepoFullName,
		Name:        best.RepoFullName,
		Description: best.Description,
		Status:      "github",
		InstallRef:  string(installRefBytes),
	}, nil
}
