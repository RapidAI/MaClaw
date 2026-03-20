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
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
		return NewUsageError("usage: maclaw-tui skillhub <search|install|rate>")
	}
	switch args[0] {
	case "search":
		return skillhubSearch(args[1:])
	case "install":
		return skillhubInstall(args[1:])
	case "rate":
		return skillhubRate(args[1:])
	default:
		return NewUsageError("unknown skillhub action: %s", args[0])
	}
}

// resolveHubURL 从本地配置读取 Hub URL。
func resolveHubURL() (string, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("加载配置失败: %w", err)
	}
	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	if hubURL == "" {
		return "", fmt.Errorf("Hub URL 未配置，请先在 GUI 或 config set --local remote_hub_url <url> 中设置")
	}
	return strings.TrimRight(hubURL, "/"), nil
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
		return PrintJSON(result)
	}

	if len(result.Skills) == 0 {
		fmt.Printf("未找到匹配 \"%s\" 的技能。\n", query)
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
