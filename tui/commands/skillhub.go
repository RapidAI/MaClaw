package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// HubSkillMeta 是 SkillHub 搜索返回的技能元数据。
type HubSkillMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	TrustLevel  string   `json:"trust_level"`
	Downloads   int      `json:"downloads"`
	AvgRating   float64  `json:"avg_rating"`
	RatingCount int      `json:"rating_count"`
}

type hubSearchResult struct {
	Skills []HubSkillMeta `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}

// RunSkillHub 执行 skillhub 子命令（search/install/rate）。
func RunSkillHub(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui skillhub <search|install|install-github|rate|check-updates|update>")
	}
	switch args[0] {
	case "search":
		return skillhubSearch(args[1:])
	case "install":
		return skillhubInstall(args[1:])
	case "install-github":
		return skillhubInstallGitHub(args[1:])
	case "rate":
		return skillhubRate(args[1:])
	case "check-updates":
		return skillhubCheckUpdates(args[1:])
	case "update":
		return skillhubUpdate(args[1:])
	default:
		return NewUsageError("unknown skillhub action: %s", args[0])
	}
}

// resolveHubURL 从本地配置读取 SkillHub API 的 base URL。
// SkillHub API 在 HubCenter 上，通过 AppConfig.SkillHubBaseURL() 获取。
func resolveHubURL() (string, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("加载配置失败: %w", err)
	}
	hubURL := cfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
	if hubURL == "" {
		return "", fmt.Errorf("HubCenter URL 未配置")
	}
	return hubURL, nil
}

// resolveMaclawID 从本地配置读取 MachineID 作为 maclaw_id。
func resolveMaclawID() string {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	if cfg.RemoteMachineID != "" {
		return cfg.RemoteMachineID
	}
	return "unknown"
}

func skillhubSearch(args []string) error {
	fs := flag.NewFlagSet("skillhub search", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	page := fs.Int("page", 1, "页码")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillhub search <query> [--page N] [--json]")
	}
	query := strings.Join(fs.Args(), " ")

	hubURL, err := resolveHubURL()
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/api/v1/skills/search?q=%s&page=%d",
		hubURL, url.QueryEscape(query), *page)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MaClaw-TUI/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("搜索 SkillHub 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SkillHub 返回 HTTP %d", resp.StatusCode)
	}

	var result hubSearchResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("解析搜索结果失败: %w", err)
	}

	if *jsonOut {
		if len(result.Skills) == 0 {
			// Fallback 1: ClawHub mirror
			client := skill.NewHubClient()
			clawResults := client.SearchClawHub(ctx, query)
			if len(clawResults) > 0 {
				return PrintJSON(map[string]interface{}{
					"source":  "clawhub",
					"results": clawResults,
				})
			}
			// Fallback 2: GitHub
			gs := skill.NewGitHubSearcher("")
			candidates, ghErr := gs.SearchGitHub(query)
			if ghErr == nil && len(candidates) > 0 {
				return PrintJSON(map[string]interface{}{
					"source":     "github",
					"candidates": candidates,
				})
			}
		}
		return PrintJSON(result)
	}

	if len(result.Skills) == 0 {
		// Fallback 1: ClawHub mirror
		fmt.Printf("SkillHub 未找到匹配 \"%s\" 的技能，正在搜索 ClawHub...\n", query)
		client := skill.NewHubClient()
		clawResults := client.SearchClawHub(ctx, query)
		if len(clawResults) > 0 {
			fmt.Printf("\n在 ClawHub 上找到 %d 个结果:\n\n", len(clawResults))
			fmt.Printf("%-24s %-8s %-10s %s\n", "ID", "VERSION", "TRUST", "NAME")
			fmt.Println(strings.Repeat("-", 70))
			for _, r := range clawResults {
				fmt.Printf("%-24s %-8s %-10s %s\n",
					TruncateDisplay(r.ID, 24),
					TruncateDisplay(r.Version, 8),
					TruncateDisplay(r.TrustLevel, 10),
					TruncateDisplay(r.Name, 30))
			}
			return nil
		}

		// Fallback 2: GitHub
		fmt.Printf("ClawHub 也未找到，正在搜索 GitHub...\n")
		gs := skill.NewGitHubSearcher("") // unauthenticated
		candidates, ghErr := gs.SearchGitHub(query)
		if ghErr != nil || len(candidates) == 0 {
			fmt.Printf("GitHub 也未找到匹配的技能。\n")
			return nil
		}
		fmt.Printf("\n在 GitHub 上找到 %d 个候选技能:\n\n", len(candidates))
		fmt.Printf("%-30s %-6s %-40s %s\n", "REPO", "★", "FILE", "DESCRIPTION")
		fmt.Println(strings.Repeat("-", 100))
		for _, c := range candidates {
			fmt.Printf("%-30s %-6d %-40s %s\n",
				TruncateDisplay(c.RepoFullName, 30),
				c.Stars,
				TruncateDisplay(c.FilePath, 40),
				TruncateDisplay(c.Description, 30))
		}
		fmt.Println("\n使用 skillhub install-github <repo-url> 导入。")
		return nil
	}

	fmt.Printf("搜索 \"%s\" — 共 %d 个结果 (第 %d 页)\n\n", query, result.Total, result.Page)
	fmt.Printf("%-24s %-8s %-6s %-5s %-8s %s\n", "ID", "VERSION", "TRUST", "★", "DOWNLOADS", "NAME")
	fmt.Println(strings.Repeat("-", 90))
	for _, s := range result.Skills {
		rating := fmt.Sprintf("%.1f", s.AvgRating)
		fmt.Printf("%-24s %-8s %-6s %-5s %-8d %s\n",
			TruncateDisplay(s.ID, 24),
			TruncateDisplay(s.Version, 8),
			TruncateDisplay(s.TrustLevel, 6),
			rating,
			s.Downloads,
			TruncateDisplay(s.Name, 30))
	}
	return nil
}

func skillhubInstall(args []string) error {
	fs := flag.NewFlagSet("skillhub install", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillhub install <skill-id>")
	}
	skillID := fs.Arg(0)

	hubURL, err := resolveHubURL()
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/api/v1/skills/%s/download", hubURL, url.PathEscape(skillID))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MaClaw-TUI/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载技能失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SkillHub 返回 HTTP %d", resp.StatusCode)
	}

	var full struct {
		HubSkillMeta
		Triggers []string `json:"triggers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&full); err != nil {
		return fmt.Errorf("解析技能数据失败: %w", err)
	}

	// 写入本地 NLSkills 配置
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 检查是否已安装
	for _, s := range cfg.NLSkills {
		if s.HubSkillID == skillID {
			if *jsonOut {
				return PrintJSON(map[string]string{"status": "already_installed", "name": s.Name})
			}
			fmt.Printf("技能 '%s' 已安装 (hub_id=%s)\n", s.Name, skillID)
			return nil
		}
	}

	// 添加到 NLSkills
	newSkill := newNLSkillFromHub(full.HubSkillMeta, full.Triggers, hubURL)
	cfg.NLSkills = append(cfg.NLSkills, newSkill)
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{"status": "installed", "skill": newSkill})
	}
	fmt.Printf("✓ 技能 '%s' (v%s) 已安装\n", full.Name, full.Version)
	fmt.Printf("  作者: %s  信任等级: %s\n", full.Author, full.TrustLevel)
	return nil
}

// skillhubInstallGitHub imports skill(s) from a GitHub repository URL
// and registers them as local NL Skills.
func skillhubInstallGitHub(args []string) error {
	fs := flag.NewFlagSet("skillhub install-github", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillhub install-github <github-repo-url>")
	}
	repoURL := fs.Arg(0)

	gs := skill.NewGitHubSearcher("")
	imported, err := gs.ImportFromRepoURL(repoURL)
	if err != nil {
		return fmt.Errorf("从 GitHub 导入失败: %w", err)
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, loadErr := store.LoadConfig()
	if loadErr != nil {
		return fmt.Errorf("加载配置失败: %w", loadErr)
	}

	existingNames := make(map[string]bool)
	for _, s := range cfg.NLSkills {
		existingNames[s.Name] = true
	}
	// Also check file-based skills from all scan roots for dedup.
	for _, s := range skill.ScanAllSkillDirs() {
		existingNames[s.Name] = true
	}

	var installed []corelib.NLSkillEntry
	for _, sk := range imported {
		if existingNames[sk.Name] {
			fmt.Printf("跳过已存在的技能: %s\n", sk.Name)
			continue
		}
		cfg.NLSkills = append(cfg.NLSkills, sk)
		existingNames[sk.Name] = true
		installed = append(installed, sk)
	}

	if len(installed) == 0 {
		fmt.Println("没有新技能需要安装。")
		return nil
	}

	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{"status": "installed", "count": len(installed), "skills": installed})
	}
	for _, sk := range installed {
		fmt.Printf("✓ 技能 '%s' 已从 GitHub 导入 (%s)\n", sk.Name, sk.SourceProject)
	}
	return nil
}

func skillhubRate(args []string) error {
	fs := flag.NewFlagSet("skillhub rate", flag.ExitOnError)
	score := fs.Int("score", 0, "评分 (1-5)")
	fs.Parse(args)

	if fs.NArg() == 0 || *score < 1 || *score > 5 {
		return NewUsageError("usage: skillhub rate <skill-id> --score <1-5>")
	}
	skillID := fs.Arg(0)

	hubURL, err := resolveHubURL()
	if err != nil {
		return err
	}

	maclawID := resolveMaclawID()
	body, _ := json.Marshal(map[string]interface{}{
		"maclaw_id": maclawID,
		"score":     *score,
	})

	endpoint := fmt.Sprintf("%s/api/v1/skills/%s/rate", hubURL, url.PathEscape(skillID))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MaClaw-TUI/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("评分失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SkillHub 返回 HTTP %d", resp.StatusCode)
	}

	fmt.Printf("✓ 已为技能 %s 评分 %d 星\n", skillID, *score)
	return nil
}

// newNLSkillFromHub 从 Hub 元数据创建本地 NLSkillEntry。
func newNLSkillFromHub(meta HubSkillMeta, triggers []string, hubURL string) corelib.NLSkillEntry {
	return corelib.NLSkillEntry{
		Name:          meta.Name,
		Description:   meta.Description,
		Triggers:      triggers,
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: hubURL,
		HubSkillID:    meta.ID,
		HubVersion:    meta.Version,
		TrustLevel:    meta.TrustLevel,
	}
}

// ---------- Check Updates ----------

func skillhubCheckUpdates(args []string) error {
	fs := flag.NewFlagSet("skillhub check-updates", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	hubURL, err := resolveHubURL()
	if err != nil {
		return err
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	type updateInfo struct {
		Name         string `json:"name"`
		HubSkillID   string `json:"hub_skill_id"`
		LocalVersion string `json:"local_version"`
		LatestVersion string `json:"latest_version,omitempty"`
		NeedsUpdate  bool   `json:"needs_update"`
		Error        string `json:"error,omitempty"`
	}

	var results []updateInfo
	client := &http.Client{Timeout: 15 * time.Second}

	for _, s := range cfg.NLSkills {
		if s.HubSkillID == "" {
			continue
		}
		endpoint := fmt.Sprintf("%s/api/v1/skills/%s", hubURL, url.PathEscape(s.HubSkillID))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		req.Header.Set("User-Agent", "MaClaw-TUI/1.0")
		resp, err := client.Do(req)
		cancel()

		info := updateInfo{Name: s.Name, HubSkillID: s.HubSkillID, LocalVersion: s.HubVersion}
		if err != nil {
			info.Error = err.Error()
			results = append(results, info)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			info.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
			results = append(results, info)
			continue
		}
		var meta struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&meta); err != nil {
			resp.Body.Close()
			info.Error = err.Error()
			results = append(results, info)
			continue
		}
		resp.Body.Close()
		info.LatestVersion = meta.Version
		info.NeedsUpdate = meta.Version != s.HubVersion && meta.Version != ""
		results = append(results, info)
	}

	if *jsonOut {
		return PrintJSON(results)
	}

	if len(results) == 0 {
		fmt.Println("没有来自 Hub 的技能需要检查更新。")
		return nil
	}

	hasUpdates := false
	fmt.Printf("%-20s %-12s %-12s %s\n", "NAME", "LOCAL", "LATEST", "STATUS")
	fmt.Println(strings.Repeat("-", 60))
	for _, r := range results {
		status := "up-to-date"
		if r.Error != "" {
			status = "error: " + r.Error
		} else if r.NeedsUpdate {
			status = "update available"
			hasUpdates = true
		}
		fmt.Printf("%-20s %-12s %-12s %s\n",
			TruncateDisplay(r.Name, 20),
			TruncateDisplay(r.LocalVersion, 12),
			TruncateDisplay(r.LatestVersion, 12),
			status)
	}
	if !hasUpdates {
		fmt.Println("\n所有 Hub 技能已是最新版本。")
	}
	return nil
}

// ---------- Update ----------

func skillhubUpdate(args []string) error {
	fs := flag.NewFlagSet("skillhub update", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillhub update <skill-name|--all>")
	}
	target := fs.Arg(0)

	hubURL, err := resolveHubURL()
	if err != nil {
		return err
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	updateAll := target == "--all"
	updated := 0

	client := &http.Client{Timeout: 30 * time.Second}
	for i := range cfg.NLSkills {
		s := &cfg.NLSkills[i]
		if s.HubSkillID == "" {
			continue
		}
		if !updateAll && s.Name != target {
			continue
		}

		endpoint := fmt.Sprintf("%s/api/v1/skills/%s/download", hubURL, url.PathEscape(s.HubSkillID))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		req.Header.Set("User-Agent", "MaClaw-TUI/1.0")
		resp, err := client.Do(req)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "更新 '%s' 失败: %v\n", s.Name, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			fmt.Fprintf(os.Stderr, "更新 '%s' 失败: HTTP %d\n", s.Name, resp.StatusCode)
			continue
		}

		var full struct {
			HubSkillMeta
			Triggers []string `json:"triggers"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&full); err != nil {
			resp.Body.Close()
			fmt.Fprintf(os.Stderr, "解析 '%s' 更新数据失败: %v\n", s.Name, err)
			continue
		}
		resp.Body.Close()

		// Update local entry
		s.Description = full.Description
		s.Triggers = full.Triggers
		s.HubVersion = full.Version
		s.TrustLevel = full.TrustLevel
		updated++
		fmt.Printf("✓ '%s' 已更新到 v%s\n", s.Name, full.Version)
	}

	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{"updated": updated})
	}
	if updated == 0 {
		fmt.Println("没有技能被更新。")
	}
	return nil
}
