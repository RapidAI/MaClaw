package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// applyMCPAuth sets authentication and custom headers on an MCP HTTP request.
// Custom headers are applied first (lower precedence), then AuthType/AuthSecret
// (higher precedence). Protocol-level headers (Content-Type, Accept) are protected.
func applyMCPAuth(req *http.Request, entry corelib.MCPServerEntry) {
	for k, v := range entry.Headers {
		if k == "" || v == "" {
			continue
		}
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "accept" {
			continue
		}
		req.Header.Set(k, v)
	}
	if entry.AuthSecret == "" {
		return
	}
	switch entry.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+entry.AuthSecret)
	case "api_key":
		req.Header.Set("X-API-Key", entry.AuthSecret)
	}
}

// RunMCP 执行 mcp 子命令。
//
// Marketplace-style search/install:
//
//	maclaw-tui mcp search <query>
//	maclaw-tui mcp install <id|owner/repo|name@marketplace>
//	maclaw-tui mcp remove <name>
func RunMCP(args []string) error {
	// Action list must stay in sync with InstallCLICatalog["mcp"] (install_cli_catalog.go).
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui mcp <list|search|install|add|remove|health-check|tools|call-tool>")
	}
	switch args[0] {
	case "list":
		return mcpList(args[1:])
	case "search":
		return mcpSearch(args[1:])
	case "install":
		return mcpInstall(args[1:])
	case "add":
		return mcpAdd(args[1:])
	case "remove", "uninstall", "rm":
		return mcpRemove(args[1:])
	case "health-check":
		return mcpHealthCheck(args[1:])
	case "tools":
		return mcpTools(args[1:])
	case "call-tool":
		return mcpCallTool(args[1:])
	default:
		return NewUsageError("unknown mcp action: %s\nusage: maclaw-tui mcp <list|search|install|add|remove|health-check|tools|call-tool>", args[0])
	}
}

func mcpSearch(args []string) error {
	fs := flag.NewFlagSet("mcp search", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return NewUsageError("usage: maclaw-tui mcp search <query>")
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return NewUsageError("usage: maclaw-tui mcp search <query>")
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	type mcpHit struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Source      string `json:"source"`
		InstallRef  string `json:"install_ref,omitempty"`
		Kind        string `json:"kind"` // market | clawhub | github | plugin
	}
	var hits []mcpHit

	seen := map[string]bool{}
	addHit := func(h mcpHit) {
		key := strings.ToLower(h.Kind + "|" + h.ID)
		if h.ID == "" || seen[key] {
			return
		}
		seen[key] = true
		hits = append(hits, h)
	}

	// 1) HubCenter capability market
	if items, err := searchCapabilityMarketMCP(ctx, query); err == nil {
		for _, raw := range items {
			id := firstMCPString(raw["id"], raw["capability_id"], raw["name"], raw["display_name"])
			if id == "" {
				continue
			}
			addHit(mcpHit{
				ID:          id,
				Name:        firstMCPString(raw["display_name"], raw["name"], id),
				Description: firstMCPString(raw["description"]),
				Source:      "capability_market",
				InstallRef:  id,
				Kind:        "market",
			})
		}
	}

	// 2) ClawHub + GitHub topic:mcp-server
	allowed := cfg.SkillSourcesAllowed
	client := skill.DefaultHubClient()
	for _, r := range client.SearchMCPFiltered(ctx, query, allowed) {
		addHit(mcpHit{
			ID:          r.ID,
			Name:        r.Name,
			Description: r.Description,
			Source:      r.Source,
			InstallRef:  firstNonEmpty(r.InstallRef, r.RepoURL, r.ID),
			Kind:        r.Source,
		})
	}

	// 3) Registered plugin marketplaces (MCP-capable plugins)
	if pstore, err := loadPluginMarketplaceStore(); err == nil {
		q := strings.ToLower(query)
		for _, m := range pstore.Marketplaces {
			for _, p := range m.Plugins {
				hay := strings.ToLower(p.Name + " " + p.Description + " " + p.Category)
				if !strings.Contains(hay, q) {
					continue
				}
				addHit(mcpHit{
					ID:          p.Name + "@" + m.Name,
					Name:        p.Name,
					Description: p.Description,
					Source:      "plugin:" + m.Name,
					InstallRef:  p.Name + "@" + m.Name,
					Kind:        "plugin",
				})
			}
		}
	}

	if *jsonOut {
		return PrintJSON(hits)
	}
	if len(hits) == 0 {
		Printf("No MCP results for %q.\n", query)
		Println("Tips:")
		Println("  maclaw-tui plugin marketplace add owner/repo")
		Println("  maclaw-tui mcp add --name <n> --command <cmd>")
		Println("  maclaw-tui mcp add --name <n> --url <endpoint>")
		return nil
	}
	Printf("MCP search %q — %d results\n\n", query, len(hits))
	Printf("%-28s %-12s %-20s %s\n", "ID/SPEC", "SOURCE", "NAME", "DESCRIPTION")
	Println(strings.Repeat("-", 100))
	for _, h := range hits {
		Printf("%-28s %-12s %-20s %s\n",
			TruncateDisplay(h.ID, 28),
			TruncateDisplay(h.Source, 12),
			TruncateDisplay(h.Name, 20),
			TruncateDisplay(h.Description, 40))
	}
	Println("\nInstall:")
	Println("  maclaw-tui mcp install <id>")
	Println("  maclaw-tui plugin add <name>@<marketplace>")
	return nil
}

func firstMCPString(values ...any) string {
	for _, v := range values {
		switch t := v.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				return s
			}
		}
	}
	return ""
}

func mcpInstall(args []string) error {
	fs := flag.NewFlagSet("mcp install", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	nameFlag := fs.String("name", "", "覆盖安装后的 MCP 名称")
	if err := fs.Parse(args); err != nil {
		return NewUsageError("usage: maclaw-tui mcp install <id|owner/repo|name@marketplace>")
	}
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui mcp install <id|owner/repo|name@marketplace>\nexamples:\n  maclaw-tui mcp install ida-pro-mcp@mrexodia\n  maclaw-tui mcp install mrexodia/ida-pro-mcp\n  maclaw-tui mcp install jira-mcp")
	}
	ref := strings.TrimSpace(fs.Arg(0))

	// 1) plugin-style name@marketplace
	if _, market, ok := pluginSpecParts(ref); ok && market != "" {
		return pluginAdd([]string{ref})
	}

	// 2) GitHub owner/repo → install as plugin-like MCP source via archive
	if owner, repo, ok := parseOwnerRepo(ref); ok {
		return mcpInstallFromGitHub(owner, repo, *nameFlag, *jsonOut)
	}

	// 3) Capability market id
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	items, err := searchCapabilityMarketMCP(ctx, ref)
	if err == nil {
		var match map[string]any
		for _, raw := range items {
			id := firstMCPString(raw["id"], raw["capability_id"], raw["name"], raw["display_name"])
			if strings.EqualFold(id, ref) || strings.EqualFold(firstMCPString(raw["display_name"], raw["name"]), ref) {
				match = raw
				break
			}
		}
		if match == nil && len(items) == 1 {
			match = items[0]
		}
		if match != nil {
			return mcpInstallFromMarketItem(match, *nameFlag, *jsonOut)
		}
	}

	// 4) ClawHub / GitHub search hit by id
	client := skill.DefaultHubClient()
	results := client.SearchMCPFiltered(ctx, ref, nil)
	for _, r := range results {
		if strings.EqualFold(r.ID, ref) || strings.EqualFold(r.Name, ref) || strings.EqualFold(r.InstallRef, ref) {
			if r.Source == "github" || strings.Contains(r.InstallRef, "/") {
				owner, repo, ok := parseOwnerRepo(firstNonEmpty(r.InstallRef, r.RepoURL, r.ID))
				if ok {
					return mcpInstallFromGitHub(owner, repo, firstNonEmpty(*nameFlag, r.Name), *jsonOut)
				}
			}
			// ClawHub MCP without full package metadata: guide user.
			return fmt.Errorf("found MCP %q from %s but no install package metadata; use: maclaw-tui mcp add --name %s --command <cmd>  or plugin marketplace install", r.Name, r.Source, r.Name)
		}
	}

	return fmt.Errorf("MCP %q not found; try: maclaw-tui mcp search %s", ref, ref)
}

func mcpInstallFromMarketItem(raw map[string]any, nameOverride string, jsonOut bool) error {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	// Flatten nested mcp metadata if present.
	meta := map[string]any{}
	for k, v := range raw {
		meta[k] = v
	}
	if nested, ok := raw["mcp"].(map[string]any); ok {
		for k, v := range nested {
			if _, exists := meta[k]; !exists {
				meta[k] = v
			}
		}
	}
	name := firstNonEmpty(nameOverride, firstMCPString(meta["name"], meta["display_name"], meta["id"], meta["capability_id"]))
	if name == "" {
		return fmt.Errorf("MCP market item missing name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if command := firstMCPString(meta["command"]); command != "" {
		if err := enforceMCPClientSecurity(cfg, "bash", map[string]interface{}{"command": command}); err != nil {
			return err
		}
		entry := corelib.LocalMCPServerEntry{
			ID:        fmt.Sprintf("market-%s-%d", name, time.Now().UnixMilli()),
			Name:      name,
			Command:   command,
			Args:      anyStringSlice(meta["args"]),
			Env:       anyStringMap(meta["env"]),
			AutoStart: true,
			CreatedAt: now,
			Source:    corelib.MCPSourceMarket,
		}
		upsertLocalMCPConfig(&cfg, entry)
		if err := store.SaveConfig(cfg); err != nil {
			return err
		}
		if jsonOut {
			return PrintJSON(map[string]any{"status": "installed", "type": "local", "entry": entry})
		}
		Printf("本地 MCP '%s' 已从能力市场安装 (command: %s)\n", name, command)
		return nil
	}
	endpoint := firstMCPString(meta["endpoint_url"], meta["url"])
	if endpoint == "" {
		return fmt.Errorf("MCP market item %q has neither command nor endpoint_url", name)
	}
	if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": endpoint}); err != nil {
		return err
	}
	authType := firstNonEmpty(firstMCPString(meta["auth_type"]), "none")
	entry := corelib.MCPServerEntry{
		ID:          fmt.Sprintf("market-%s-%d", name, time.Now().UnixMilli()),
		Name:        name,
		EndpointURL: endpoint,
		AuthType:    authType,
		Headers:     anyStringMap(meta["headers"]),
		CreatedAt:   now,
		Source:      corelib.MCPSourceMarket,
	}
	upsertRemoteMCPConfig(&cfg, entry)
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	if jsonOut {
		return PrintJSON(map[string]any{"status": "installed", "type": "remote", "entry": entry})
	}
	Printf("远程 MCP '%s' 已从能力市场安装 (url: %s)\n", name, endpoint)
	return nil
}

func mcpInstallFromGitHub(owner, repo, nameOverride string, jsonOut bool) error {
	// Reuse plugin install pipeline for GitHub MCP packages (Codex/Claude plugin layouts).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	name := firstNonEmpty(nameOverride, repo)
	m := pluginMarketplaceEntry{
		Name:   owner,
		Source: owner + "/" + repo,
		Repo:   owner + "/" + repo,
	}
	p := marketplacePlugin{
		Name:       name,
		SourceType: "url",
		SourceURL:  "https://github.com/" + owner + "/" + repo + ".git",
	}
	installed, err := installPluginFromMarketplace(ctx, &store, m, p)
	if err != nil {
		return err
	}
	// Record under synthetic marketplace so remove works.
	if installed.Marketplace == "" {
		installed.Marketplace = owner
		installed.Spec = installed.Name + "@" + owner
	}
	if err := savePluginMarketplaceStore(store); err != nil {
		return err
	}
	if jsonOut {
		return PrintJSON(installed)
	}
	Printf("MCP package %s installed from GitHub %s/%s\n", installed.Name, owner, repo)
	if len(installed.MCPNames) > 0 {
		Printf("  mcp: %s\n", strings.Join(installed.MCPNames, ", "))
	}
	if len(installed.SkillNames) > 0 {
		Printf("  skills: %s\n", strings.Join(installed.SkillNames, ", "))
	}
	return nil
}

func anyStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		return splitMCPArgs(t)
	default:
		return nil
	}
}

func anyStringMap(v any) map[string]string {
	switch t := v.(type) {
	case map[string]string:
		return t
	case map[string]any:
		out := make(map[string]string, len(t))
		for k, val := range t {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

func mcpList(args []string) error {
	fs := flag.NewFlagSet("mcp list", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	lang := i18n.NormalizeLang(cfg.Language)

	type mcpView struct {
		Remote         []corelib.MCPServerEntry      `json:"remote"`
		Local          []corelib.LocalMCPServerEntry `json:"local"`
		NextAction     string                        `json:"next_action,omitempty"`
		NextTUICommand string                        `json:"next_tui_command,omitempty"`
	}
	view := mcpView{
		Remote:         cfg.MCPServers,
		Local:          cfg.LocalMCPServers,
		NextAction:     mcpNextAction(cfg, lang),
		NextTUICommand: mcpNextTUICommand(cfg),
	}

	if *jsonOut {
		return PrintJSON(view)
	}

	if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
		if lang == "en" {
			Println("No MCP servers configured.")
			Printf("Next: %s\n", mcpNextAction(cfg, lang))
			Printf("TUI add: %s\n", mcpNextTUICommand(cfg))
			return nil
		}
		Println("未配置 MCP 服务器。")
		Printf("下一步: %s\n", mcpNextAction(cfg, lang))
		Printf("TUI 添加: %s\n", mcpNextTUICommand(cfg))
		return nil
	}

	if len(cfg.MCPServers) > 0 {
		if lang == "en" {
			Println("Remote MCP servers:")
		} else {
			Println("远程 MCP 服务器:")
		}
		Printf("  %-20s %-10s %-8s %s\n", "NAME", "AUTH", "SOURCE", "URL")
		Println("  " + strings.Repeat("-", 70))
		for _, s := range cfg.MCPServers {
			Printf("  %-20s %-10s %-8s %s\n",
				TruncateDisplay(s.Name, 20),
				s.AuthType,
				string(s.Source),
				TruncateDisplay(s.EndpointURL, 40))
		}
	}

	if len(cfg.LocalMCPServers) > 0 {
		if len(cfg.MCPServers) > 0 {
			Println()
		}
		if lang == "en" {
			Println("Local MCP servers:")
		} else {
			Println("本地 MCP 服务器:")
		}
		Printf("  %-20s %-8s %s\n", "NAME", "DISABLED", "COMMAND")
		Println("  " + strings.Repeat("-", 60))
		for _, s := range cfg.LocalMCPServers {
			disabled := "no"
			if s.Disabled {
				disabled = "yes"
			}
			cmd := s.Command
			if len(s.Args) > 0 {
				cmd += " " + strings.Join(s.Args, " ")
			}
			Printf("  %-20s %-8s %s\n",
				TruncateDisplay(s.Name, 20),
				disabled,
				TruncateDisplay(cmd, 50))
		}
	}
	if lang == "en" {
		Printf("\nNext: %s\n", mcpNextAction(cfg, lang))
	} else {
		Printf("\n下一步: %s\n", mcpNextAction(cfg, lang))
	}
	return nil
}

func mcpNextAction(cfg corelib.AppConfig, lang string) string {
	cliName := mcpTUIName()
	if lang == "en" {
		if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
			return fmt.Sprintf("Run %s mcp to add MCP from TUI templates; use %s mcp remote for remote endpoints.", cliName, cliName)
		}
		return fmt.Sprintf("Run %s mcp to view/add MCP in the TUI; scripted checks can use %s mcp health-check.", cliName, cliName)
	}
	if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
		return fmt.Sprintf("运行 %s mcp 从模板添加 MCP；远程端点可用 %s mcp remote。", cliName, cliName)
	}
	return fmt.Sprintf("运行 %s mcp 在 TUI 中查看和添加 MCP；脚本检查可用 %s mcp health-check。", cliName, cliName)
}

func mcpNextTUICommand(cfg corelib.AppConfig) string {
	cliName := mcpTUIName()
	if len(cfg.MCPServers) == 0 && len(cfg.LocalMCPServers) == 0 {
		return cliName + " mcp"
	}
	return cliName + " mcp"
}

func mcpTUIName() string {
	return strings.ToLower(brand.Current().DisplayName) + "-tui"
}

func mcpAdd(args []string) error {
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	name := fs.String("name", "", "服务器名称（必填）")
	endpoint := fs.String("url", "", "远程端点 URL")
	command := fs.String("command", "", "本地启动命令")
	authType := fs.String("auth", "none", "认证类型 (none/api_key/bearer)")
	authSecret := fs.String("secret", "", "认证密钥")
	mcpArgs := fs.String("args", "", "命令参数（逗号分隔）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *name == "" {
		return NewUsageError("usage: mcp add --name <name> (--url <endpoint> | --command <cmd>)\n推荐: 运行 maclaw-tui mcp，在 TUI 中从模板选择。")
	}
	if *endpoint == "" && *command == "" {
		return NewUsageError("必须指定 --url（远程）或 --command（本地）。推荐运行 maclaw-tui mcp，在 TUI 中从模板选择。")
	}
	if *endpoint != "" && *command != "" {
		return NewUsageError("--url 和 --command 只能二选一。推荐运行 maclaw-tui mcp local 或 maclaw-tui mcp remote，在 TUI 中选择模板。")
	}
	auth := strings.TrimSpace(*authType)
	if auth == "" {
		auth = "none"
	}
	secret := strings.TrimSpace(*authSecret)
	if *endpoint != "" {
		switch auth {
		case "none":
			secret = ""
		case "api_key", "bearer":
			if secret == "" {
				return NewUsageError("认证类型 %s 需要 --secret。推荐运行 maclaw-tui mcp remote，在 TUI 中选择认证方式并填写密钥。", auth)
			}
		default:
			return NewUsageError("不支持的 MCP 认证类型 %q（可用: none/api_key/bearer）。推荐运行 maclaw-tui mcp remote。", auth)
		}
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if *command != "" {
		entryArgs := splitMCPArgs(*mcpArgs)
		cmdText := strings.Join(append([]string{*command}, entryArgs...), " ")
		if err := enforceMCPClientSecurity(cfg, "bash", map[string]interface{}{"command": cmdText}); err != nil {
			return err
		}
		// 本地 MCP
		entry := corelib.LocalMCPServerEntry{
			ID:        fmt.Sprintf("local-%s-%d", *name, time.Now().UnixMilli()),
			Name:      *name,
			Command:   *command,
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		entry.Args = entryArgs
		cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		Printf("本地 MCP 服务器 '%s' 已添加 (command: %s)\n", *name, *command)
	} else {
		if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": *endpoint}); err != nil {
			return err
		}
		// 远程 MCP
		entry := corelib.MCPServerEntry{
			ID:          fmt.Sprintf("remote-%s-%d", *name, time.Now().UnixMilli()),
			Name:        *name,
			EndpointURL: *endpoint,
			AuthType:    auth,
			AuthSecret:  secret,
			CreatedAt:   time.Now().Format(time.RFC3339),
			Source:      corelib.MCPSourceManual,
		}
		cfg.MCPServers = append(cfg.MCPServers, entry)
		Printf("远程 MCP 服务器 '%s' 已添加 (url: %s)\n", *name, *endpoint)
	}

	return store.SaveConfig(cfg)
}

func splitMCPArgs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func enforceMCPClientSecurity(cfg corelib.AppConfig, name string, args map[string]interface{}) error {
	if ok, reason := clientsecurity.EnforceConfig(cfg, name, args); !ok {
		if reason == "" {
			reason = "blocked by Hub security policy"
		}
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func mcpRemove(args []string) error {
	fs := flag.NewFlagSet("mcp remove", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return NewUsageError("usage: mcp remove <name>")
	}
	name := fs.Arg(0)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	found := false
	// 从远程列表移除
	for i, s := range cfg.MCPServers {
		if s.Name == name {
			cfg.MCPServers = append(cfg.MCPServers[:i], cfg.MCPServers[i+1:]...)
			found = true
			break
		}
	}
	// 从本地列表移除
	if !found {
		for i, s := range cfg.LocalMCPServers {
			if s.Name == name {
				cfg.LocalMCPServers = append(cfg.LocalMCPServers[:i], cfg.LocalMCPServers[i+1:]...)
				found = true
				break
			}
		}
	}

	if !found {
		return fmt.Errorf("MCP 服务器 '%s' 不存在", name)
	}

	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	Printf("MCP 服务器 '%s' 已移除。\n", name)
	return nil
}

// ---------- Health Check ----------

func mcpHealthCheck(args []string) error {
	fs := flag.NewFlagSet("mcp health-check", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	type healthResult struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Endpoint string `json:"endpoint,omitempty"`
		Command  string `json:"command,omitempty"`
		Status   string `json:"status"`
		Error    string `json:"error,omitempty"`
		Latency  string `json:"latency,omitempty"`
	}

	var results []healthResult

	client := &http.Client{Timeout: 5 * time.Second}
	for _, s := range cfg.MCPServers {
		if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": s.EndpointURL}); err != nil {
			results = append(results, healthResult{Name: s.Name, Type: "remote", Endpoint: s.EndpointURL, Status: "blocked", Error: err.Error()})
			continue
		}
		start := time.Now()
		// Send a JSON-RPC tools/list request (POST) per MCP Streamable HTTP spec.
		reqBody, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/list",
			"params":  map[string]interface{}{},
		})
		req, _ := http.NewRequest(http.MethodPost, s.EndpointURL, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		applyMCPAuth(req, s)
		resp, err := client.Do(req)
		elapsed := time.Since(start)
		r := healthResult{Name: s.Name, Type: "remote", Endpoint: s.EndpointURL}
		if err != nil {
			r.Status = "unreachable"
			r.Error = err.Error()
		} else {
			// Check HTTP status first, then validate response is parseable.
			if resp.StatusCode != http.StatusOK {
				errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
				resp.Body.Close()
				r.Status = fmt.Sprintf("HTTP %d", resp.StatusCode)
				if len(errBody) > 0 {
					detail := string(errBody)
					if len(detail) > 200 {
						detail = detail[:200] + "..."
					}
					r.Error = detail
				}
			} else {
				// Validate response is parseable (handles SSE / Streamable HTTP).
				ct := resp.Header.Get("Content-Type")
				_, parseErr := corelib.ParseMCPResponse(resp.Body, ct, 64*1024)
				resp.Body.Close()
				if parseErr != nil {
					r.Status = "parse_error"
					r.Error = parseErr.Error()
				} else {
					r.Status = "healthy"
				}
			}
			r.Latency = elapsed.Round(time.Millisecond).String()
		}
		results = append(results, r)
	}

	for _, s := range cfg.LocalMCPServers {
		r := healthResult{Name: s.Name, Type: "local", Command: s.Command}
		if s.Disabled {
			r.Status = "disabled"
		} else {
			r.Status = "configured"
		}
		results = append(results, r)
	}

	if *jsonOut {
		return PrintJSON(results)
	}

	if len(results) == 0 {
		Println("未配置 MCP 服务器。")
		return nil
	}

	Printf("%-20s %-8s %-15s %-10s %s\n", "NAME", "TYPE", "STATUS", "LATENCY", "ENDPOINT")
	Println(strings.Repeat("-", 80))
	for _, r := range results {
		ep := r.Endpoint
		if ep == "" {
			ep = r.Command
		}
		latency := r.Latency
		if latency == "" {
			latency = "-"
		}
		Printf("%-20s %-8s %-15s %-10s %s\n",
			TruncateDisplay(r.Name, 20), r.Type, r.Status, latency, TruncateDisplay(ep, 40))
	}
	return nil
}

// ---------- Tools ----------

func mcpTools(args []string) error {
	fs := flag.NewFlagSet("mcp tools", flag.ExitOnError)
	server := fs.String("server", "", "按服务器名称过滤")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	type toolInfo struct {
		Server string `json:"server"`
		Name   string `json:"name"`
		Desc   string `json:"description,omitempty"`
		Params string `json:"params,omitempty"`
	}

	var tools []toolInfo

	// For remote MCP servers, fetch tool list via JSON-RPC tools/list.
	client := &http.Client{Timeout: 10 * time.Second}
	for _, s := range cfg.MCPServers {
		if *server != "" && s.Name != *server {
			continue
		}
		if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": s.EndpointURL}); err != nil {
			tools = append(tools, toolInfo{Server: s.Name, Name: "(blocked)", Desc: err.Error()})
			continue
		}
		reqBody, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/list",
			"params":  map[string]interface{}{},
		})
		req, _ := http.NewRequest(http.MethodPost, s.EndpointURL, bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		applyMCPAuth(req, s)
		resp, err := client.Do(req)
		if err != nil {
			tools = append(tools, toolInfo{Server: s.Name, Name: "(unreachable)", Desc: err.Error()})
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			tools = append(tools, toolInfo{Server: s.Name, Name: "(error)", Desc: fmt.Sprintf("HTTP %d", resp.StatusCode)})
			continue
		}
		// Parse response — handles both plain JSON and SSE (Streamable HTTP).
		ct := resp.Header.Get("Content-Type")
		parsed, parseErr := corelib.ParseMCPResponse(resp.Body, ct, 256*1024)
		resp.Body.Close()
		if parseErr != nil {
			tools = append(tools, toolInfo{Server: s.Name, Name: "(parse error)", Desc: parseErr.Error()})
			continue
		}
		var rpcResp struct {
			Result struct {
				Tools []struct {
					Name        string                 `json:"name"`
					Description string                 `json:"description"`
					InputSchema map[string]interface{} `json:"inputSchema"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(parsed, &rpcResp); err != nil {
			tools = append(tools, toolInfo{Server: s.Name, Name: "(parse error)", Desc: err.Error()})
			continue
		}
		for _, t := range rpcResp.Result.Tools {
			params := formatMCPToolParams(t.InputSchema)
			tools = append(tools, toolInfo{Server: s.Name, Name: t.Name, Desc: t.Description, Params: params})
		}
	}

	// For local MCP servers, start the process briefly to discover tools.
	for _, s := range cfg.LocalMCPServers {
		if s.Disabled {
			continue
		}
		if *server != "" && s.Name != *server {
			continue
		}
		if err := enforceMCPClientSecurity(cfg, "bash", map[string]interface{}{"command": strings.Join(append([]string{s.Command}, s.Args...), " ")}); err != nil {
			tools = append(tools, toolInfo{Server: s.Name + " (local)", Name: "(blocked)", Desc: err.Error()})
			continue
		}
		discovered, err := discoverLocalMCPTools(s)
		if err != nil {
			tools = append(tools, toolInfo{Server: s.Name + " (local)", Name: "(error)", Desc: err.Error()})
			continue
		}
		for _, t := range discovered {
			params := formatMCPToolParams(t.InputSchema)
			tools = append(tools, toolInfo{Server: s.Name + " (local)", Name: t.Name, Desc: t.Description, Params: params})
		}
	}

	if *jsonOut {
		return PrintJSON(tools)
	}

	if len(tools) == 0 {
		Println("未发现 MCP 工具。")
		return nil
	}

	Printf("%-20s %-30s %-40s %s\n", "SERVER", "TOOL", "DESCRIPTION", "PARAMS")
	Println(strings.Repeat("-", 100))
	for _, t := range tools {
		params := t.Params
		if params == "" {
			params = "(no parameters)"
		}
		Printf("%-20s %-30s %-40s %s\n",
			TruncateDisplay(t.Server, 20), TruncateDisplay(t.Name, 30), TruncateDisplay(t.Desc, 40), params)
	}
	return nil
}

// formatMCPToolParams extracts parameter names from an MCP inputSchema and
// returns a compact summary string like "search_query*, content_size".
// Required parameters are marked with "*".
func formatMCPToolParams(schema map[string]interface{}) string {
	if schema == nil {
		return ""
	}
	props, _ := schema["properties"].(map[string]interface{})
	if len(props) == 0 {
		return ""
	}

	// Collect required parameter names into a set for quick lookup.
	requiredSet := map[string]bool{}
	if reqList, ok := schema["required"].([]interface{}); ok {
		for _, r := range reqList {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}

	// Sort parameter names for stable output.
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		if requiredSet[name] {
			parts = append(parts, name+"*")
		} else {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}

// localMCPToolInfo holds a tool discovered from a local MCP server.
type localMCPToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// discoverLocalMCPTools starts a local MCP server process, performs the
// initialize handshake, calls tools/list, and shuts down. The entire
// operation is bounded by a 15-second timeout.
func discoverLocalMCPTools(entry corelib.LocalMCPServerEntry) ([]localMCPToolInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, entry.Command, entry.Args...)
	cmd.Env = os.Environ()
	for k, v := range entry.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", entry.Command, err)
	}
	defer func() {
		stdinPipe.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait() //nolint:errcheck
	}()

	reader := bufio.NewReaderSize(stdoutPipe, 64*1024)
	var nextID atomic.Int64

	// Single goroutine reads lines from stdout; shared across all sendRPC calls.
	type rpcLine struct {
		line string
		err  error
	}
	lineCh := make(chan rpcLine, 4)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			lineCh <- rpcLine{strings.TrimSpace(line), err}
			if err != nil {
				return
			}
		}
	}()

	sendRPC := func(method string, params interface{}) (json.RawMessage, error) {
		id := nextID.Add(1)
		req := struct {
			JSONRPC string      `json:"jsonrpc"`
			ID      int64       `json:"id"`
			Method  string      `json:"method"`
			Params  interface{} `json:"params,omitempty"`
		}{"2.0", id, method, params}

		data, _ := json.Marshal(req)
		data = append(data, '\n')
		if _, err := stdinPipe.Write(data); err != nil {
			return nil, fmt.Errorf("write: %w", err)
		}

		deadline := time.After(10 * time.Second)
		for {
			select {
			case <-deadline:
				return nil, fmt.Errorf("timeout waiting for %s response", method)
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-lineCh:
				if r.err != nil {
					return nil, fmt.Errorf("read: %w", r.err)
				}
				if r.line == "" {
					continue
				}
				var resp struct {
					ID     *int64          `json:"id"`
					Result json.RawMessage `json:"result,omitempty"`
					Error  *struct {
						Code    int    `json:"code"`
						Message string `json:"message"`
					} `json:"error,omitempty"`
				}
				if err := json.Unmarshal([]byte(r.line), &resp); err != nil {
					continue // skip non-JSON lines (server logs)
				}
				if resp.ID == nil || *resp.ID != id {
					continue // skip notifications or mismatched IDs
				}
				if resp.Error != nil {
					return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
				}
				return resp.Result, nil
			}
		}
	}

	// 1. Initialize handshake.
	_, err = sendRPC("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "maclaw-cli", "version": "1.0.0"},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// Send initialized notification (fire-and-forget).
	notif, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	notif = append(notif, '\n')
	stdinPipe.Write(notif) //nolint:errcheck

	// 2. Discover tools.
	result, err := sendRPC("tools/list", map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var listResult struct {
		Tools []localMCPToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}

	return listResult.Tools, nil
}

// ---------- Call Tool ----------

func mcpCallTool(args []string) error {
	fs := flag.NewFlagSet("mcp call-tool", flag.ExitOnError)
	server := fs.String("server", "", "MCP 服务器名称（必填）")
	tool := fs.String("tool", "", "工具名称（必填）")
	toolArgs := fs.String("args", "{}", "工具参数（JSON 格式）")
	fs.Parse(args)

	if *server == "" || *tool == "" {
		return NewUsageError("usage: mcp call-tool --server <name> --tool <name> [--args '{...}']")
	}
	if coretool.IsDisabledExternalCodingSessionTool(*tool) {
		return fmt.Errorf("external coding-session tool %q is disabled", *tool)
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Find the server
	var serverEntry corelib.MCPServerEntry
	found := false
	for _, s := range cfg.MCPServers {
		if s.Name == *server {
			serverEntry = s
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("MCP 服务器 '%s' 不存在或不是远程服务器", *server)
	}

	if err := enforceMCPClientSecurity(cfg, "web_fetch", map[string]interface{}{"url": serverEntry.EndpointURL}); err != nil {
		return err
	}

	// Parse args
	var parsedArgs map[string]interface{}
	if err := json.Unmarshal([]byte(*toolArgs), &parsedArgs); err != nil {
		return fmt.Errorf("解析工具参数失败: %w", err)
	}

	if err := enforceMCPClientSecurity(cfg, "call_mcp_tool", map[string]interface{}{"tool_name": *tool, "arguments": parsedArgs}); err != nil {
		return err
	}

	// Call the tool via JSON-RPC
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      *tool,
			"arguments": parsedArgs,
		},
	}
	body, _ := json.Marshal(reqBody)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, serverEntry.EndpointURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	applyMCPAuth(req, serverEntry)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("调用工具失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	// Parse response — handles both plain JSON and SSE (Streamable HTTP).
	ct := resp.Header.Get("Content-Type")
	parsed, parseErr := corelib.ParseMCPResponse(resp.Body, ct, 256*1024)
	if parseErr != nil {
		return fmt.Errorf("解析响应失败: %w", parseErr)
	}

	var result interface{}
	if err := json.Unmarshal(parsed, &result); err != nil {
		return fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return PrintJSON(result)
}
