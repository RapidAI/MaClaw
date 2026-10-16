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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// HubSkillMeta 是 SkillHub 搜索返回的技能元数据。
type HubSkillMeta struct {
	ID          string   `json:"id"`
	SkillID     string   `json:"skill_id,omitempty"`
	SemVer      string   `json:"semver,omitempty"`
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

// skillHubMutationMu serializes CLI registry read-modify-write operations in
// one process. Cross-process recovery is provided by SkillCommitter's durable
// compensation record; callers still must treat an unreadable queue as a hard
// admission failure.
var skillHubMutationMu sync.Mutex

func commitSkillHubConfigEntry(store *FileConfigStore, entry *corelib.NLSkillEntry, action, event string, allowCreate bool, mergers ...func(dst, src *corelib.NLSkillEntry)) error {
	if store == nil || entry == nil {
		return fmt.Errorf("SkillHub 提交参数无效")
	}
	if err := skill.CheckEvolutionCompensationQueue(); err != nil {
		return fmt.Errorf("SkillHub 写入被阻止：补偿队列不可读: %w", err)
	}
	requestID := fmt.Sprintf("evo_cli_%s_%d", action, time.Now().UnixNano())
	committer := &skill.SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry {
			cfg, err := store.LoadConfig()
			if err != nil {
				return nil
			}
			if cfg.NLSkills == nil {
				return []corelib.NLSkillEntry{}
			}
			return cfg.NLSkills
		},
		SkillSaver: func(entries []corelib.NLSkillEntry) error {
			cfg, err := store.LoadConfig()
			if err != nil {
				return err
			}
			cfg.NLSkills = entries
			return store.SaveConfig(cfg)
		},
		RollbackSkillSaver: func(entries []corelib.NLSkillEntry) error {
			cfg, err := store.LoadConfig()
			if err != nil {
				return err
			}
			cfg.NLSkills = entries
			return store.SaveConfig(cfg)
		},
		DefinitionWriter: func(*corelib.NLSkillEntry) error { return nil },
		FinalAuditor: func(kind string, data map[string]string) error {
			return skill.RecordEvolutionEventStrict(kind, data, "tui-cli")
		},
		ConfigRevision:       "tui-cli",
		AllowCreate:          allowCreate,
		SkipIfUnchanged:      true,
		SkipDefinitionBackup: true,
		CompensationMutator: func(record *skill.EvolutionCompensationRecord) {
			// CLI config-only writes share the TUI data root. Bind recovery to
			// that scope and explicitly skip routing-index refresh, which is not
			// available in headless CLI mode.
			record.SetRecoveryScope(ResolveDataDir())
			record.SetSkipIndexRefresh(true)
		},
	}
	if len(mergers) > 0 && mergers[0] != nil {
		committer.EntryMerger = mergers[0]
	}
	recordAction := "tui_cli_" + strings.TrimSpace(action)
	commitCtx := skill.WithEvolutionRequestMetadata(context.Background(), requestID, 1)
	result := committer.Commit(commitCtx, entry.Name, entry, event, map[string]string{
		"skill": entry.Name, "action": recordAction, "via": "tui-cli", "request_id": requestID,
		"attempt": "1", "config_revision": "tui-cli", "schema_version": "2", "evidence_mode": "none",
	})
	if result.State != "committed" && result.State != "skipped" {
		return fmt.Errorf("SkillHub %s 未提交: %s (%s)", action, result.State, result.FailureReason)
	}
	if result.CleanupStatus != "clear" {
		return fmt.Errorf("SkillHub %s 已提交但清理待处理: %s", action, result.FailureReason)
	}
	return nil
}

// commitSkillHubConfigBatch publishes a set of config-only imports as one
// durable transaction.  A single compensation snapshot prevents a mid-batch
// failure from leaving only a prefix of the imported registry entries.
func commitSkillHubConfigBatch(store *FileConfigStore, entries []corelib.NLSkillEntry, action, event string) error {
	if store == nil || len(entries) == 0 {
		return nil
	}
	if err := skill.CheckEvolutionCompensationQueue(); err != nil {
		return fmt.Errorf("SkillHub 批量写入被阻止：补偿队列不可读: %w", err)
	}
	requestID := fmt.Sprintf("evo_cli_%s_%d", action, time.Now().UnixNano())
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" {
			return fmt.Errorf("SkillHub 批量提交包含空名称")
		}
		names = append(names, entry.Name)
	}
	committer := &skill.SkillCommitter{
		SkillLoader: func() []corelib.NLSkillEntry {
			cfg, err := store.LoadConfig()
			if err != nil {
				return nil
			}
			if cfg.NLSkills == nil {
				return []corelib.NLSkillEntry{}
			}
			return cfg.NLSkills
		},
		SkillSaver: func(updated []corelib.NLSkillEntry) error {
			cfg, err := store.LoadConfig()
			if err != nil {
				return err
			}
			cfg.NLSkills = updated
			return store.SaveConfig(cfg)
		},
		EntriesMutator: func(original []corelib.NLSkillEntry) ([]corelib.NLSkillEntry, error) {
			seen := make(map[string]struct{}, len(original)+len(entries))
			for _, existing := range original {
				seen[strings.ToLower(strings.TrimSpace(existing.Name))] = struct{}{}
			}
			updated := append([]corelib.NLSkillEntry(nil), original...)
			for _, entry := range entries {
				key := strings.ToLower(strings.TrimSpace(entry.Name))
				if _, exists := seen[key]; exists {
					return nil, fmt.Errorf("Skill '%s' 在提交期间已存在", entry.Name)
				}
				seen[key] = struct{}{}
				updated = append(updated, *skill.CloneNLSkillEntry(&entry))
			}
			return updated, nil
		},
		FinalAuditor: func(kind string, data map[string]string) error {
			return skill.RecordEvolutionEventStrict(kind, data, "tui-cli")
		},
		CompensationMutator: func(record *skill.EvolutionCompensationRecord) {
			record.SetRecoveryScope(ResolveDataDir())
			record.SetSkipIndexRefresh(true)
			record.SetAffectedSkills(names)
		},
		ConfigRevision:       "tui-cli",
		AllowCreate:          false,
		SkipIfUnchanged:      true,
		SkipDefinitionBackup: true,
	}
	commitCtx := skill.WithEvolutionRequestMetadata(context.Background(), requestID, 1)
	result := committer.Commit(commitCtx, entries[0].Name, &entries[0], event, map[string]string{
		"skill": entries[0].Name, "action": "tui_cli_" + strings.TrimSpace(action), "via": "tui-cli",
		"request_id": requestID, "attempt": "1", "config_revision": "tui-cli",
		"schema_version": "2", "evidence_mode": "none", "package_count": fmt.Sprintf("%d", len(entries)),
	})
	if result.State != "committed" || result.CleanupStatus != "clear" {
		return fmt.Errorf("SkillHub 批量 %s 未提交: %s (%s)", action, result.State, result.FailureReason)
	}
	return nil
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

// resolveHubURL 从本地配置读取 SkillHub API 的 base URL，并执行 failover。
// SkillHub API 在 HubCenter 上，通过 AppConfig.SkillHubBaseURL() 获取。
// 如果配置的 URL 不可达，会尝试 failover 到其他已知节点，并持久化成功的选择。
//
// This delegates to the shared ResolveHubCenterWithFailover() for failover logic.
func resolveHubURL() (string, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("加载配置失败: %w", err)
	}
	hubURL := cfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)

	// Use the shared failover logic from skill_search_api.go.
	// This ensures all CLI commands use the same singleton cache and persister.
	resolved := ResolveHubCenterWithFailover(cfg, hubURL, nil, nil)
	if resolved == "" {
		return "", fmt.Errorf("HubCenter URL 未配置或不可达")
	}
	return resolved, nil
}

// resolveMaclawID 从本地配置读取 MachineID 作为 maclaw_id。
func loadSkillHubCommandConfig() (corelib.AppConfig, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return corelib.AppConfig{}, fmt.Errorf("鍔犺浇閰嶇疆澶辫触: %w", err)
	}
	return cfg, nil
}

func enforceSkillHubClientSecurity(cfg corelib.AppConfig, tool string, args map[string]interface{}) error {
	if ok, reason := clientsecurity.EnforceConfig(cfg, tool, args); !ok {
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func enforceSkillHubSourceAction(cfg corelib.AppConfig, source, action string, extra map[string]interface{}) error {
	args := map[string]interface{}{"action": action, "source": source}
	for k, v := range extra {
		args[k] = v
	}
	return enforceSkillHubClientSecurity(cfg, "manage_skill", args)
}

func enforceSkillHubFetchSecurity(cfg corelib.AppConfig, endpoint string) error {
	if clientsecurity.IsDeveloperMode(cfg) {
		return nil
	}
	return enforceSkillHubClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": endpoint})
}

func sourceAllowedBySkillHubPolicy(cfg corelib.AppConfig, source string) bool {
	if clientsecurity.IsDeveloperMode(cfg) {
		return true
	}
	return skill.IsSourceAllowed(source, cfg.SkillSourcesAllowed)
}

func recordDeveloperSkillInstallAudit(cfg corelib.AppConfig, source, action string, args map[string]interface{}) {
	if !clientsecurity.IsDeveloperMode(cfg) {
		return
	}
	auditAction := security.AuditActionHubSkillInstall
	if strings.EqualFold(action, "update") {
		auditAction = security.AuditActionHubSkillUpdate
	}
	entryArgs := map[string]interface{}{"source": source, "action": action}
	for k, v := range args {
		entryArgs[k] = v
	}
	al, err := security.NewAuditLog(filepath.Join(ResolveDataDir(), "audit_logs"))
	if err != nil {
		return
	}
	defer al.Close()
	_ = al.Log(security.AuditEntry{
		Timestamp:    time.Now(),
		Action:       auditAction,
		ToolName:     "skillhub_" + action,
		Arguments:    entryArgs,
		RiskLevel:    security.RiskHigh,
		PolicyAction: security.PolicyAudit,
		Source:       source,
		Result:       "developer mode recorded skill install risk and allowed operation",
	})
}

func resolveMaclawID() string {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	if cfg.RemoteMachineID != "" {
		return cfg.RemoteMachineID
	}
	return "unknown"
}

func skillhubSearch(args []string) error {
	fs := flag.NewFlagSet("skillhub search", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	page := fs.Int("page", 1, "页码")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillhub search <query> [--page N] [--json]")
	}
	query := strings.Join(fs.Args(), " ")
	cfg, err := loadSkillHubCommandConfig()
	if err != nil {
		return err
	}
	searchSkillHub := sourceAllowedBySkillHubPolicy(cfg, "skillhub")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var result hubSearchResult
	if searchSkillHub {
		if err := enforceSkillHubSourceAction(cfg, "skillhub", "search", map[string]interface{}{"query": query, "url": cfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)}); err != nil {
			return err
		}

		hubURL, err := resolveHubURL()
		if err != nil {
			return err
		}

		endpoint := fmt.Sprintf("%s/api/v1/skills/search?q=%s&page=%d",
			hubURL, url.QueryEscape(query), *page)
		if err := enforceSkillHubFetchSecurity(cfg, endpoint); err != nil {
			return err
		}

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

		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
			return fmt.Errorf("解析搜索结果失败: %w", err)
		}

	}

	if *jsonOut {
		if len(result.Skills) == 0 {
			// Fallback 1: ClawHub mirror
			if sourceAllowedBySkillHubPolicy(cfg, "clawhub") {
				if err := enforceSkillHubSourceAction(cfg, "clawhub", "search", map[string]interface{}{"query": query, "url": skill.ClawHubMirrorURL}); err != nil {
					return err
				}
				client := skill.DefaultHubClient()
				clawResults := client.SearchClawHub(ctx, query)
				if len(clawResults) > 0 {
					return PrintJSON(map[string]interface{}{
						"source":  "clawhub",
						"results": clawResults,
					})
				}
			}
			// Fallback 2: GitHub
			if sourceAllowedBySkillHubPolicy(cfg, "github") {
				if err := enforceSkillHubSourceAction(cfg, "github", "search", map[string]interface{}{"query": query, "url": "https://github.com"}); err != nil {
					return err
				}
				gs := skill.NewGitHubSearcher("")
				candidates, ghErr := gs.SearchGitHub(query)
				if ghErr == nil && len(candidates) > 0 {
					return PrintJSON(map[string]interface{}{
						"source":     "github",
						"candidates": candidates,
					})
				}
			}
		}
		return PrintJSON(result)
	}

	if len(result.Skills) == 0 {
		// Fallback 1: ClawHub mirror
		if sourceAllowedBySkillHubPolicy(cfg, "clawhub") {
			if err := enforceSkillHubSourceAction(cfg, "clawhub", "search", map[string]interface{}{"query": query, "url": skill.ClawHubMirrorURL}); err != nil {
				return err
			}
			Printf("SkillHub 未找到匹配 \"%s\" 的技能，正在搜索 ClawHub...\n", query)
			client := skill.DefaultHubClient()
			clawResults := client.SearchClawHub(ctx, query)
			if len(clawResults) > 0 {
				Printf("\n在 ClawHub 上找到 %d 个结果:\n\n", len(clawResults))
				Printf("%-24s %-8s %-10s %s\n", "ID", "VERSION", "TRUST", "NAME")
				Println(strings.Repeat("-", 70))
				for _, r := range clawResults {
					Printf("%-24s %-8s %-10s %s\n",
						TruncateDisplay(r.ID, 24),
						TruncateDisplay(r.Version, 8),
						TruncateDisplay(r.TrustLevel, 10),
						TruncateDisplay(r.Name, 30))
				}
				return nil
			}

		}

		// Fallback 2: GitHub
		if !sourceAllowedBySkillHubPolicy(cfg, "github") {
			Println("No matching skills found in allowed sources.")
			return nil
		}
		if err := enforceSkillHubSourceAction(cfg, "github", "search", map[string]interface{}{"query": query, "url": "https://github.com"}); err != nil {
			return err
		}
		Printf("ClawHub 也未找到，正在搜索 GitHub...\n")
		gs := skill.NewGitHubSearcher("") // unauthenticated
		candidates, ghErr := gs.SearchGitHub(query)
		if ghErr != nil || len(candidates) == 0 {
			Printf("GitHub 也未找到匹配的技能。\n")
			return nil
		}
		Printf("\n在 GitHub 上找到 %d 个候选技能:\n\n", len(candidates))
		Printf("%-30s %-6s %-40s %s\n", "REPO", "STARS", "FILE", "DESCRIPTION")
		Println(strings.Repeat("-", 100))
		for _, c := range candidates {
			Printf("%-30s %-6d %-40s %s\n",
				TruncateDisplay(c.RepoFullName, 30),
				c.Stars,
				TruncateDisplay(c.FilePath, 40),
				TruncateDisplay(c.Description, 30))
		}
		Println("\n使用 skillhub install-github <repo-url> 导入。")
		return nil
	}

	Printf("搜索 \"%s\" — 共 %d 个结果 (第 %d 页)\n\n", query, result.Total, result.Page)
	Printf("%-24s %-8s %-6s %-5s %-8s %s\n", "ID", "VERSION", "TRUST", "RATE", "DOWNLOADS", "NAME")
	Println(strings.Repeat("-", 90))
	for _, s := range result.Skills {
		rating := fmt.Sprintf("%.1f", s.AvgRating)
		Printf("%-24s %-8s %-6s %-5s %-8d %s\n",
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
	fs := flag.NewFlagSet("skillhub install", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillhub install <skill-id>")
	}
	skillID := fs.Arg(0)
	secCfg, err := loadSkillHubCommandConfig()
	if err != nil {
		return err
	}
	if err := enforceSkillHubSourceAction(secCfg, "skillhub", "install", map[string]interface{}{"skill_id": skillID, "url": secCfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)}); err != nil {
		return err
	}
	recordDeveloperSkillInstallAudit(secCfg, "skillhub", "install", map[string]interface{}{"skill_id": skillID})

	hubURL, err := resolveHubURL()
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/api/v1/skills/%s/download", hubURL, url.PathEscape(skillID))
	if err := enforceSkillHubFetchSecurity(secCfg, endpoint); err != nil {
		return err
	}

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

	// Registry publication is serialized and coordinated by the same durable
	// committer used by manage_skill. Load a fresh snapshot after the network
	// request so another CLI process cannot be silently overwritten.
	store := NewFileConfigStore(ResolveDataDir())
	skillHubMutationMu.Lock()
	defer skillHubMutationMu.Unlock()
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
			Printf("技能 '%s' 已安装 (hub_id=%s)\n", s.Name, skillID)
			return nil
		}
	}

	// Add through SkillCommitter so config writes have a durable pre-image and a
	// strict final audit. The in-memory cfg is refreshed after the commit for
	// callers that continue using this command process.
	newSkill := newNLSkillFromHub(full.HubSkillMeta, full.Triggers, hubURL)
	if err := commitSkillHubConfigEntry(store, &newSkill, "install", "skill:cli_skillhub_installed", true); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{"status": "installed", "skill": newSkill})
	}
	Printf("技能 '%s' (v%s) 已安装\n", full.Name, full.Version)
	Printf("  作者: %s  信任等级: %s\n", full.Author, full.TrustLevel)
	return nil
}

// skillhubInstallGitHub imports skill(s) from a GitHub repository URL
// and registers them as local NL Skills.
func skillhubInstallGitHub(args []string) error {
	fs := flag.NewFlagSet("skillhub install-github", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillhub install-github <github-repo-url>")
	}
	repoURL := fs.Arg(0)
	cfg, err := loadSkillHubCommandConfig()
	if err != nil {
		return err
	}
	if err := enforceSkillHubSourceAction(cfg, "github", "install", map[string]interface{}{"install_ref": repoURL, "url": repoURL}); err != nil {
		return err
	}
	recordDeveloperSkillInstallAudit(cfg, "github", "install", map[string]interface{}{"install_ref": repoURL})

	gs := skill.NewGitHubSearcher("")
	imported, err := gs.ImportFromRepoURL(repoURL)
	if err != nil {
		return fmt.Errorf("从 GitHub 导入失败: %w", err)
	}

	store := NewFileConfigStore(ResolveDataDir())
	skillHubMutationMu.Lock()
	defer skillHubMutationMu.Unlock()
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
			Printf("跳过已存在的技能: %s\n", sk.Name)
			continue
		}
		cfg.NLSkills = append(cfg.NLSkills, sk)
		existingNames[sk.Name] = true
		installed = append(installed, sk)
	}

	if len(installed) == 0 {
		Println("没有新技能需要安装。")
		return nil
	}

	if err := commitSkillHubConfigBatch(store, installed, "github_install", "skill:cli_skillhub_github_installed"); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{"status": "installed", "count": len(installed), "skills": installed})
	}
	for _, sk := range installed {
		Printf("技能 '%s' 已从 GitHub 导入 (%s)\n", sk.Name, sk.SourceProject)
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
	cfg, err := loadSkillHubCommandConfig()
	if err != nil {
		return err
	}
	if err := enforceSkillHubSourceAction(cfg, "skillhub", "search", map[string]interface{}{"skill_id": skillID, "url": cfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)}); err != nil {
		return err
	}

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
	if err := enforceSkillHubFetchSecurity(cfg, endpoint); err != nil {
		return err
	}

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

	Printf("已为技能 %s 评分 %d 星\n", skillID, *score)
	return nil
}

// newNLSkillFromHub 从 Hub 元数据创建本地 NLSkillEntry。
func newNLSkillFromHub(meta HubSkillMeta, triggers []string, hubURL string) corelib.NLSkillEntry {
	return corelib.NLSkillEntry{
		SkillID:       meta.SkillID,
		Name:          meta.Name,
		Description:   meta.Description,
		Triggers:      triggers,
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: hubURL,
		HubSkillID:    meta.ID,
		HubVersion:    meta.Version,
		Version:       meta.SemVer,
		TrustLevel:    meta.TrustLevel,
	}
}

// ---------- Check Updates ----------

func skillhubCheckUpdates(args []string) error {
	fs := flag.NewFlagSet("skillhub check-updates", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)
	secCfg, err := loadSkillHubCommandConfig()
	if err != nil {
		return err
	}
	if err := enforceSkillHubSourceAction(secCfg, "skillhub", "search", map[string]interface{}{"url": secCfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)}); err != nil {
		return err
	}

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
		Name          string `json:"name"`
		HubSkillID    string `json:"hub_skill_id"`
		LocalVersion  string `json:"local_version"`
		LatestVersion string `json:"latest_version,omitempty"`
		NeedsUpdate   bool   `json:"needs_update"`
		Error         string `json:"error,omitempty"`
	}

	var results []updateInfo
	client := &http.Client{Timeout: 15 * time.Second}

	for _, s := range cfg.NLSkills {
		if s.HubSkillID == "" {
			continue
		}
		endpoint := fmt.Sprintf("%s/api/v1/skills/%s", hubURL, url.PathEscape(s.HubSkillID))
		if err := enforceSkillHubFetchSecurity(cfg, endpoint); err != nil {
			return err
		}
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
		Println("没有来自 Hub 的技能需要检查更新。")
		return nil
	}

	hasUpdates := false
	Printf("%-20s %-12s %-12s %s\n", "NAME", "LOCAL", "LATEST", "STATUS")
	Println(strings.Repeat("-", 60))
	for _, r := range results {
		status := "up-to-date"
		if r.Error != "" {
			status = "error: " + r.Error
		} else if r.NeedsUpdate {
			status = "update available"
			hasUpdates = true
		}
		Printf("%-20s %-12s %-12s %s\n",
			TruncateDisplay(r.Name, 20),
			TruncateDisplay(r.LocalVersion, 12),
			TruncateDisplay(r.LatestVersion, 12),
			status)
	}
	if !hasUpdates {
		Println("\n所有 Hub 技能已是最新版本。")
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
	secCfg, err := loadSkillHubCommandConfig()
	if err != nil {
		return err
	}
	if err := enforceSkillHubSourceAction(secCfg, "skillhub", "update", map[string]interface{}{"url": secCfg.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)}); err != nil {
		return err
	}
	recordDeveloperSkillInstallAudit(secCfg, "skillhub", "update", map[string]interface{}{"target": target})

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
	pendingUpdates := make([]corelib.NLSkillEntry, 0)

	client := &http.Client{Timeout: 30 * time.Second}
	for i := range cfg.NLSkills {
		s := cfg.NLSkills[i]
		if s.HubSkillID == "" {
			continue
		}
		if !updateAll && s.Name != target {
			continue
		}

		endpoint := fmt.Sprintf("%s/api/v1/skills/%s/download", hubURL, url.PathEscape(s.HubSkillID))
		if err := enforceSkillHubFetchSecurity(cfg, endpoint); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		req.Header.Set("User-Agent", "MaClaw-TUI/1.0")
		resp, err := client.Do(req)
		cancel()
		if err != nil {
			Eprintf("更新 '%s' 失败: %v\n", s.Name, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			Eprintf("更新 '%s' 失败: HTTP %d\n", s.Name, resp.StatusCode)
			continue
		}

		var full struct {
			HubSkillMeta
			Triggers []string `json:"triggers"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&full); err != nil {
			resp.Body.Close()
			Eprintf("解析 '%s' 更新数据失败: %v\n", s.Name, err)
			continue
		}
		resp.Body.Close()

		// Keep network work outside the mutation lock. The complete candidate is
		// committed below against a fresh config snapshot so a concurrent install
		// or status update cannot be overwritten by this stale list.
		candidate := s
		candidate.Description = full.Description
		candidate.Triggers = full.Triggers
		candidate.HubVersion = full.Version
		candidate.TrustLevel = full.TrustLevel
		candidate.Version = full.SemVer
		pendingUpdates = append(pendingUpdates, candidate)
	}

	skillHubMutationMu.Lock()
	defer skillHubMutationMu.Unlock()
	for i := range pendingUpdates {
		candidate := pendingUpdates[i]
		if err := commitSkillHubConfigEntry(store, &candidate, "update", "skill:cli_skillhub_updated", false,
			func(dst, src *corelib.NLSkillEntry) {
				if dst == nil || src == nil {
					return
				}
				// Hub metadata is authoritative for the package fields, while the
				// candidate already carries local lifecycle/runtime overlays.
				*dst = *src
			}); err != nil {
			return fmt.Errorf("保存配置失败: %w", err)
		}
		updated++
		Printf("'%s' 已更新到 v%s\n", candidate.Name, candidate.HubVersion)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{"updated": updated})
	}
	if updated == 0 {
		Println("没有技能被更新。")
	}
	return nil
}
