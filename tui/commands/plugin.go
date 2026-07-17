package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/plugin"
)

// DefaultPluginRegistry is the package-level PluginRegistry instance.
// It must be set by the application startup code before CLI commands are used.
var DefaultPluginRegistry *plugin.PluginRegistry

// RunPlugin executes the plugin sub-command.
//
// Codex-compatible marketplace workflow:
//
//	maclaw-tui plugin marketplace add owner/repo
//	maclaw-tui plugin marketplace list
//	maclaw-tui plugin marketplace remove <name|owner/repo>
//	maclaw-tui plugin search <query>
//	maclaw-tui plugin add name@marketplace
//	maclaw-tui plugin remove name@marketplace
//	maclaw-tui plugin list
func RunPlugin(args []string) error {
	// Action list must stay in sync with InstallCLICatalog["plugin"] (install_cli_catalog.go).
	if len(args) == 0 {
		return NewUsageError("%s", pluginUsage())
	}
	switch args[0] {
	case "list":
		return pluginList(args[1:])
	case "info":
		return pluginInfo(args[1:])
	case "search":
		return pluginSearch(args[1:])
	case "add", "install":
		return pluginAdd(args[1:])
	case "remove", "uninstall":
		return pluginRemove(args[1:])
	case "enable":
		return pluginEnable(args[1:])
	case "disable":
		return pluginDisable(args[1:])
	case "create":
		return pluginCreate(args[1:])
	case "marketplace", "market":
		return pluginMarketplaceCmd(args[1:])
	case "installed":
		return pluginInstalled(args[1:])
	case "help", "--help", "-h":
		Eprint(pluginUsage())
		return nil
	default:
		return NewUsageError("unknown plugin action: %s\n%s", args[0], pluginUsage())
	}
}

func pluginUsage() string {
	return `usage: maclaw-tui plugin <command>

Codex-style plugin marketplace:
  plugin marketplace add <owner/repo>   Add a marketplace (GitHub repo / URL / local path)
  plugin marketplace list               List configured marketplaces
  plugin marketplace remove <name>      Remove a marketplace
  plugin marketplace refresh            Refresh marketplace plugin indexes
  plugin search <query>                 Search plugins across marketplaces
  plugin add <name@marketplace>         Install plugin (skills + MCP)
  plugin remove <name@marketplace>      Uninstall plugin
  plugin installed                      List installed marketplace plugins
  plugin list|info|enable|disable|create

Examples:
  maclaw-tui plugin marketplace add mrexodia/codex-marketplace
  maclaw-tui plugin add ida-pro-mcp@mrexodia
  maclaw-tui plugin remove ida-pro-mcp@mrexodia
`
}

func pluginMarketplaceCmd(args []string) error {
	// Nested actions must stay in sync with InstallCLICatalog["plugin"].Nested.
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui plugin marketplace <add|list|remove|refresh>")
	}
	switch args[0] {
	case "add":
		return pluginMarketplaceAdd(args[1:])
	case "list", "ls":
		return pluginMarketplaceList(args[1:])
	case "remove", "rm", "delete":
		return pluginMarketplaceRemove(args[1:])
	case "refresh", "update":
		return pluginMarketplaceRefresh(args[1:])
	default:
		return NewUsageError("unknown marketplace action: %s\nusage: maclaw-tui plugin marketplace <add|list|remove|refresh>", args[0])
	}
}

func pluginMarketplaceAdd(args []string) error {
	fs := flag.NewFlagSet("plugin marketplace add", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return NewUsageError("usage: maclaw-tui plugin marketplace add <owner/repo|url|local-path>")
	}
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin marketplace add <owner/repo|url|local-path>\nexample: maclaw-tui plugin marketplace add mrexodia/codex-marketplace")
	}
	source := strings.TrimSpace(fs.Arg(0))

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	entry, err := fetchMarketplaceManifest(ctx, source)
	if err != nil {
		return err
	}

	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	// Replace existing marketplace with same name/source.
	replaced := false
	for i, existing := range store.Marketplaces {
		if strings.EqualFold(existing.Name, entry.Name) || strings.EqualFold(existing.Source, entry.Source) || (entry.Repo != "" && strings.EqualFold(existing.Repo, entry.Repo)) {
			entry.AddedAt = existing.AddedAt
			if entry.AddedAt == "" {
				entry.AddedAt = time.Now().UTC().Format(time.RFC3339)
			}
			store.Marketplaces[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		store.Marketplaces = append(store.Marketplaces, entry)
	}
	if err := savePluginMarketplaceStore(store); err != nil {
		return err
	}

	if *jsonOut {
		return PrintJSON(entry)
	}
	action := "added"
	if replaced {
		action = "updated"
	}
	Printf("Marketplace %q %s (%d plugins)\n", entry.Name, action, len(entry.Plugins))
	if entry.Repo != "" {
		Printf("  source: %s@%s\n", entry.Repo, entry.Branch)
	} else {
		Printf("  source: %s\n", entry.Source)
	}
	if len(entry.Plugins) > 0 {
		Println("  plugins:")
		for _, p := range entry.Plugins {
			Printf("    - %s@%s", p.Name, entry.Name)
			if p.Description != "" {
				Printf("  %s", TruncateDisplay(p.Description, 60))
			}
			Println()
		}
		Printf("\nInstall with: maclaw-tui plugin add <name>@%s\n", entry.Name)
	}
	return nil
}

func pluginMarketplaceList(args []string) error {
	fs := flag.NewFlagSet("plugin marketplace list", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	_ = fs.Parse(args)

	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(store.Marketplaces)
	}
	if len(store.Marketplaces) == 0 {
		Println("No plugin marketplaces configured.")
		Println("Add one: maclaw-tui plugin marketplace add owner/repo")
		Println("Example: maclaw-tui plugin marketplace add mrexodia/codex-marketplace")
		return nil
	}
	w := tabwriter.NewWriter(Stdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPLUGINS\tSOURCE\tBRANCH")
	for _, m := range store.Marketplaces {
		src := m.Repo
		if src == "" {
			src = m.Source
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", m.Name, len(m.Plugins), TruncateDisplay(src, 40), m.Branch)
	}
	return w.Flush()
}

func pluginMarketplaceRemove(args []string) error {
	fs := flag.NewFlagSet("plugin marketplace remove", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	if err := fs.Parse(args); err != nil {
		return NewUsageError("usage: maclaw-tui plugin marketplace remove <name|owner/repo>")
	}
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin marketplace remove <name|owner/repo>")
	}
	key := fs.Arg(0)
	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	m, ok := findMarketplace(store, key)
	if !ok {
		return fmt.Errorf("marketplace %q not found", key)
	}
	remaining := store.Marketplaces[:0]
	for _, existing := range store.Marketplaces {
		if strings.EqualFold(existing.Name, m.Name) {
			continue
		}
		remaining = append(remaining, existing)
	}
	store.Marketplaces = remaining
	if err := savePluginMarketplaceStore(store); err != nil {
		return err
	}
	Printf("Marketplace %q removed.\n", m.Name)
	return nil
}

func pluginMarketplaceRefresh(args []string) error {
	fs := flag.NewFlagSet("plugin marketplace refresh", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	_ = fs.Parse(args)

	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	if len(store.Marketplaces) == 0 {
		Println("No marketplaces to refresh.")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for i, m := range store.Marketplaces {
		source := m.Source
		if source == "" {
			source = m.Repo
		}
		entry, err := fetchMarketplaceManifest(ctx, source)
		if err != nil {
			Printf("refresh %s: %v\n", m.Name, err)
			continue
		}
		entry.AddedAt = m.AddedAt
		store.Marketplaces[i] = entry
		Printf("refreshed %s (%d plugins)\n", entry.Name, len(entry.Plugins))
	}
	return savePluginMarketplaceStore(store)
}

func pluginSearch(args []string) error {
	fs := flag.NewFlagSet("plugin search", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return NewUsageError("usage: maclaw-tui plugin search <query>")
	}
	query := strings.ToLower(strings.TrimSpace(strings.Join(fs.Args(), " ")))
	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	type hit struct {
		Name        string `json:"name"`
		Marketplace string `json:"marketplace"`
		Spec        string `json:"spec"`
		Description string `json:"description,omitempty"`
		Category    string `json:"category,omitempty"`
		Installed   bool   `json:"installed"`
	}
	var hits []hit
	installed := map[string]bool{}
	for _, inst := range store.Installed {
		installed[strings.ToLower(inst.Spec)] = true
		installed[strings.ToLower(inst.Name+"@"+inst.Marketplace)] = true
	}
	for _, m := range store.Marketplaces {
		for _, p := range m.Plugins {
			if query != "" {
				hay := strings.ToLower(p.Name + " " + p.Description + " " + p.Category + " " + m.Name)
				if !strings.Contains(hay, query) {
					continue
				}
			}
			spec := p.Name + "@" + m.Name
			hits = append(hits, hit{
				Name:        p.Name,
				Marketplace: m.Name,
				Spec:        spec,
				Description: p.Description,
				Category:    p.Category,
				Installed:   installed[strings.ToLower(spec)],
			})
		}
	}
	if *jsonOut {
		return PrintJSON(hits)
	}
	if len(hits) == 0 {
		if len(store.Marketplaces) == 0 {
			Println("No marketplaces configured. Add one first:")
			Println("  maclaw-tui plugin marketplace add owner/repo")
			return nil
		}
		Println("No matching plugins.")
		return nil
	}
	w := tabwriter.NewWriter(Stdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SPEC\tINSTALLED\tCATEGORY\tDESCRIPTION")
	for _, h := range hits {
		inst := "no"
		if h.Installed {
			inst = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			h.Spec,
			inst,
			TruncateDisplay(h.Category, 16),
			TruncateDisplay(h.Description, 50))
	}
	_ = w.Flush()
	Println("\nInstall: maclaw-tui plugin add <name>@<marketplace>")
	return nil
}

func pluginAdd(args []string) error {
	fs := flag.NewFlagSet("plugin add", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return NewUsageError("usage: maclaw-tui plugin add <name@marketplace>")
	}
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin add <name@marketplace>\nexample: maclaw-tui plugin add ida-pro-mcp@mrexodia")
	}
	spec := fs.Arg(0)
	name, marketKey, ok := pluginSpecParts(spec)
	if !ok || marketKey == "" {
		return NewUsageError("usage: maclaw-tui plugin add <name@marketplace>\nexample: maclaw-tui plugin add ida-pro-mcp@mrexodia")
	}

	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	m, ok := findMarketplace(store, marketKey)
	if !ok {
		// Try treating marketKey as owner/repo and auto-add marketplace.
		if strings.Contains(marketKey, "/") {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			entry, ferr := fetchMarketplaceManifest(ctx, marketKey)
			cancel()
			if ferr == nil {
				store.Marketplaces = append(store.Marketplaces, entry)
				m = entry
				ok = true
				Printf("Auto-added marketplace %q from %s\n", entry.Name, marketKey)
			}
		}
	}
	if !ok {
		return fmt.Errorf("marketplace %q not found; run: maclaw-tui plugin marketplace add <owner/repo>", marketKey)
	}
	p, ok := findMarketplacePlugin(m, name)
	if !ok {
		return fmt.Errorf("plugin %q not found in marketplace %q; try: maclaw-tui plugin search %s", name, m.Name, name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	installed, err := installPluginFromMarketplace(ctx, &store, m, p)
	if err != nil {
		return err
	}
	if err := savePluginMarketplaceStore(store); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(installed)
	}
	Printf("Plugin %s installed.\n", installed.Spec)
	if installed.InstallDir != "" {
		Printf("  dir: %s\n", installed.InstallDir)
	}
	if len(installed.MCPNames) > 0 {
		Printf("  mcp: %s\n", strings.Join(installed.MCPNames, ", "))
	}
	if len(installed.SkillNames) > 0 {
		Printf("  skills: %s\n", strings.Join(installed.SkillNames, ", "))
	}
	if len(installed.MCPNames) == 0 && len(installed.SkillNames) == 0 {
		Println("  note: plugin contained no MCP servers or skills to register")
	}
	return nil
}

func pluginRemove(args []string) error {
	fs := flag.NewFlagSet("plugin remove", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	if err := fs.Parse(args); err != nil {
		return NewUsageError("usage: maclaw-tui plugin remove <name[@marketplace]>")
	}
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin remove <name[@marketplace]>\nexample: maclaw-tui plugin remove ida-pro-mcp@mrexodia")
	}
	spec := fs.Arg(0)
	name, marketKey, ok := pluginSpecParts(spec)
	if !ok {
		return NewUsageError("usage: maclaw-tui plugin remove <name[@marketplace]>")
	}

	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	var target *installedPluginEntry
	for i := range store.Installed {
		inst := &store.Installed[i]
		if marketKey != "" {
			if strings.EqualFold(inst.Name, name) && (strings.EqualFold(inst.Marketplace, marketKey) || strings.EqualFold(inst.Spec, spec)) {
				target = inst
				break
			}
		} else if strings.EqualFold(inst.Name, name) || strings.EqualFold(inst.Spec, name) {
			target = inst
			break
		}
	}
	if target == nil {
		return fmt.Errorf("installed plugin %q not found; run: maclaw-tui plugin installed", spec)
	}
	if err := uninstallPluginEntry(&store, *target); err != nil {
		return err
	}
	if err := savePluginMarketplaceStore(store); err != nil {
		return err
	}
	Printf("Plugin %s removed.\n", target.Spec)
	return nil
}

func pluginInstalled(args []string) error {
	fs := flag.NewFlagSet("plugin installed", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	_ = fs.Parse(args)

	store, err := loadPluginMarketplaceStore()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(store.Installed)
	}
	if len(store.Installed) == 0 {
		Println("No marketplace plugins installed.")
		Println("Install: maclaw-tui plugin add <name>@<marketplace>")
		return nil
	}
	w := tabwriter.NewWriter(Stdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SPEC\tMCP\tSKILLS\tINSTALLED_AT")
	for _, inst := range store.Installed {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			inst.Spec,
			TruncateDisplay(strings.Join(inst.MCPNames, ","), 24),
			TruncateDisplay(strings.Join(inst.SkillNames, ","), 24),
			inst.InstalledAt)
	}
	return w.Flush()
}

func pluginList(args []string) error {
	fs := flag.NewFlagSet("plugin list", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Prefer marketplace-installed plugins when registry is not wired in CLI mode.
	store, _ := loadPluginMarketplaceStore()
	if DefaultPluginRegistry == nil {
		if *jsonOut {
			return PrintJSON(map[string]any{
				"installed":    store.Installed,
				"marketplaces": store.Marketplaces,
			})
		}
		if len(store.Installed) == 0 && len(store.Marketplaces) == 0 {
			Println("No plugins registered.")
			Println("  maclaw-tui plugin marketplace add owner/repo")
			Println("  maclaw-tui plugin add name@marketplace")
			return nil
		}
		if len(store.Installed) > 0 {
			Println("Installed marketplace plugins:")
			for _, inst := range store.Installed {
				Printf("  %s\n", inst.Spec)
			}
		}
		if len(store.Marketplaces) > 0 {
			Println("Marketplaces:")
			for _, m := range store.Marketplaces {
				Printf("  %s (%d plugins)\n", m.Name, len(m.Plugins))
			}
		}
		return nil
	}

	plugins := DefaultPluginRegistry.List()

	if *jsonOut {
		return PrintJSON(plugins)
	}

	if len(plugins) == 0 {
		Println("No plugins registered.")
		return nil
	}

	w := tabwriter.NewWriter(Stdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tSTATUS\tTOOLS")
	for _, p := range plugins {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", p.Name, p.Type, p.Status, p.ToolCount)
	}
	return w.Flush()
}

func pluginInfo(args []string) error {
	fs := flag.NewFlagSet("plugin info", flag.ContinueOnError)
	fs.SetOutput(Stderr())
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin info <name>")
	}
	name := fs.Arg(0)

	// Marketplace installed plugin info fallback.
	store, _ := loadPluginMarketplaceStore()
	for _, inst := range store.Installed {
		if strings.EqualFold(inst.Name, name) || strings.EqualFold(inst.Spec, name) {
			w := tabwriter.NewWriter(Stdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Name:\t%s\n", inst.Name)
			fmt.Fprintf(w, "Spec:\t%s\n", inst.Spec)
			fmt.Fprintf(w, "Marketplace:\t%s\n", inst.Marketplace)
			fmt.Fprintf(w, "Version:\t%s\n", inst.PluginVersion)
			fmt.Fprintf(w, "Description:\t%s\n", inst.Description)
			fmt.Fprintf(w, "InstallDir:\t%s\n", inst.InstallDir)
			fmt.Fprintf(w, "MCP:\t%s\n", strings.Join(inst.MCPNames, ", "))
			fmt.Fprintf(w, "Skills:\t%s\n", strings.Join(inst.SkillNames, ", "))
			fmt.Fprintf(w, "InstalledAt:\t%s\n", inst.InstalledAt)
			return w.Flush()
		}
	}

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin %q not found", name)
	}

	info, ok := DefaultPluginRegistry.Get(name)
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

	w := tabwriter.NewWriter(Stdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", info.Name)
	fmt.Fprintf(w, "Version:\t%s\n", info.Version)
	fmt.Fprintf(w, "Description:\t%s\n", info.Description)
	fmt.Fprintf(w, "Type:\t%s\n", info.Type)
	fmt.Fprintf(w, "Scope:\t%s\n", info.Scope)
	fmt.Fprintf(w, "Status:\t%s\n", info.Status)
	fmt.Fprintf(w, "Tools:\t%d\n", info.ToolCount)
	fmt.Fprintf(w, "Hooks:\t%d\n", info.HookCount)
	fmt.Fprintf(w, "Health:\t%s\n", info.Health.Status)
	if info.Error != "" {
		fmt.Fprintf(w, "Error:\t%s\n", info.Error)
	}
	return w.Flush()
}

func pluginEnable(args []string) error {
	fs := flag.NewFlagSet("plugin enable", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin enable <name>")
	}
	name := fs.Arg(0)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized (enable requires running daemon/TUI plugin runtime)")
	}

	if err := DefaultPluginRegistry.Enable(context.Background(), name); err != nil {
		return err
	}
	Printf("plugin %q enabled\n", name)
	return nil
}

func pluginDisable(args []string) error {
	fs := flag.NewFlagSet("plugin disable", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin disable <name>")
	}
	name := fs.Arg(0)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized (disable requires running daemon/TUI plugin runtime)")
	}

	if err := DefaultPluginRegistry.Disable(name); err != nil {
		return err
	}
	Printf("plugin %q disabled\n", name)
	return nil
}

func pluginCreate(args []string) error {
	fs := flag.NewFlagSet("plugin create", flag.ExitOnError)
	pluginType := fs.String("type", "script", "Plugin type: script, local_mcp, mcp, nlskill")
	scope := fs.String("scope", "project", "Scope: project or user")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin create [--type script|local_mcp|mcp|nlskill] [--scope project|user] <name>")
	}

	// Validate plugin type.
	validTypes := map[string]bool{"script": true, "local_mcp": true, "mcp": true, "nlskill": true}
	if !validTypes[*pluginType] {
		return NewUsageError("type must be one of: script, local_mcp, mcp, nlskill")
	}

	name := fs.Arg(0)

	// Validate plugin name: must be non-empty, alphanumeric + hyphens/underscores only.
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return NewUsageError("plugin name must contain only alphanumeric characters, hyphens, and underscores")
		}
	}

	// Determine target directory.
	var baseDir string
	switch *scope {
	case "project":
		baseDir = filepath.Join(".maclaw", "plugins", name)
	case "user":
		baseDir = filepath.Join(corelib.MaclawBaseDir(), "plugins", name)
	default:
		return NewUsageError("scope must be 'project' or 'user'")
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}

	yamlPath := filepath.Join(baseDir, "plugin.yaml")

	// Check if plugin.yaml already exists to avoid overwriting.
	if _, err := os.Stat(yamlPath); err == nil {
		return fmt.Errorf("plugin %q already exists at %s", name, yamlPath)
	}

	yamlContent := generatePluginYAML(name, plugin.PluginType(*pluginType))
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		return fmt.Errorf("write plugin.yaml: %w", err)
	}

	// For script type, also generate a sample script.
	if *pluginType == "script" {
		scriptPath := filepath.Join(baseDir, "run.sh")
		scriptContent := `#!/bin/bash
# Sample script plugin for maclaw.
# Input: JSON on stdin (tool arguments)
# Output: plain text on stdout (tool result)
echo "hello from plugin"
`
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
			return fmt.Errorf("write run.sh: %w", err)
		}
	}

	Printf("plugin %q created at %s\n", name, baseDir)
	return nil
}

func generatePluginYAML(name string, ptype plugin.PluginType) string {
	switch ptype {
	case plugin.PluginTypeLocalMCP:
		return fmt.Sprintf(`name: %s
version: 0.1.0
description: Local MCP plugin
type: local_mcp
local_mcp:
  command: npx
  args: ["-y", "example-mcp"]
`, name)
	case plugin.PluginTypeMCP:
		return fmt.Sprintf(`name: %s
version: 0.1.0
description: Remote MCP plugin
type: mcp
mcp:
  url: https://example.com/mcp
`, name)
	case plugin.PluginTypeNLSkill:
		return fmt.Sprintf(`name: %s
version: 0.1.0
description: NL skill plugin
type: nlskill
`, name)
	default:
		return fmt.Sprintf(`name: %s
version: 0.1.0
description: Script plugin
type: script
script:
  command: ./run.sh
`, name)
	}
}
