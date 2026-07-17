package commands

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestParseOwnerRepo(t *testing.T) {
	cases := []struct {
		in          string
		owner, repo string
		ok          bool
	}{
		{"mrexodia/codex-marketplace", "mrexodia", "codex-marketplace", true},
		{"https://github.com/mrexodia/ida-pro-mcp", "mrexodia", "ida-pro-mcp", true},
		{"https://github.com/mrexodia/ida-pro-mcp.git", "mrexodia", "ida-pro-mcp", true},
		{"git@github.com:mrexodia/ida-pro-mcp.git", "mrexodia", "ida-pro-mcp", true},
		{"not-a-repo", "", "", false},
		{"", "", "", false},
		{"https://gitlab.com/a/b", "", "", false},
	}
	for _, tc := range cases {
		owner, repo, ok := parseOwnerRepo(tc.in)
		if ok != tc.ok || owner != tc.owner || repo != tc.repo {
			t.Fatalf("parseOwnerRepo(%q) = %q %q %v, want %q %q %v", tc.in, owner, repo, ok, tc.owner, tc.repo, tc.ok)
		}
	}
}

func TestPluginSpecParts(t *testing.T) {
	name, market, ok := pluginSpecParts("ida-pro-mcp@mrexodia")
	if !ok || name != "ida-pro-mcp" || market != "mrexodia" {
		t.Fatalf("got %q %q %v", name, market, ok)
	}
	name, market, ok = pluginSpecParts("solo")
	if !ok || name != "solo" || market != "" {
		t.Fatalf("got %q %q %v", name, market, ok)
	}
	_, _, ok = pluginSpecParts("")
	if ok {
		t.Fatal("empty spec should fail")
	}
}

func TestParseMarketplaceManifestBytes(t *testing.T) {
	raw := `{
  "name": "mrexodia",
  "plugins": [
    {
      "name": "ida-pro-mcp",
      "description": "IDA MCP",
      "source": {"source": "url", "url": "https://github.com/mrexodia/ida-pro-mcp.git"}
    },
    {
      "name": "local-tool",
      "source": "./plugins/local-tool"
    },
    {
      "name": "github-tool",
      "source": {"source": "github", "repo": "acme/tool"}
    }
  ]
}`
	entry, err := parseMarketplaceManifestBytes([]byte(raw), "mrexodia/codex-marketplace", "mrexodia/codex-marketplace", "master", "https://example/marketplace.json")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "mrexodia" {
		t.Fatalf("name = %q", entry.Name)
	}
	if len(entry.Plugins) != 3 {
		t.Fatalf("plugins = %d", len(entry.Plugins))
	}
	if entry.Plugins[0].SourceType != "url" || entry.Plugins[0].SourceURL == "" {
		t.Fatalf("plugin0 = %+v", entry.Plugins[0])
	}
	if entry.Plugins[1].SourceType != "path" || entry.Plugins[1].SourcePath != "./plugins/local-tool" {
		t.Fatalf("plugin1 = %+v", entry.Plugins[1])
	}
	if entry.Plugins[2].SourceType != "github" || entry.Plugins[2].SourceRepo != "acme/tool" {
		t.Fatalf("plugin2 = %+v", entry.Plugins[2])
	}
}

func TestNormalizeMarketplacePluginMissingKeysNoNilString(t *testing.T) {
	// Object with only url (no source type) must not produce SourceType "<nil>".
	p := remoteMarketplacePlugin{
		Name:   "x",
		Source: []byte(`{"url":"https://github.com/acme/x.git"}`),
	}
	got, err := normalizeMarketplacePlugin(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceType != "url" || got.SourceURL != "https://github.com/acme/x.git" {
		t.Fatalf("got %+v", got)
	}
	if strings.Contains(got.SourceType, "nil") || strings.Contains(got.SourceURL, "nil") {
		t.Fatalf("unexpected nil literal: %+v", got)
	}
}

func TestParseMCPServersJSONInlineAndWrapped(t *testing.T) {
	wrapped := []byte(`{"mcpServers":{"idalib":{"command":"uv","args":["run","idalib-mcp"]}}}`)
	servers, err := parseMCPServersJSON(wrapped)
	if err != nil || servers["idalib"].Command != "uv" {
		t.Fatalf("wrapped: %+v err=%v", servers, err)
	}
	bare := []byte(`{"idalib":{"command":"uv","args":["run","x"]}}`)
	servers, err = parseMCPServersJSON(bare)
	if err != nil || len(servers["idalib"].Args) != 2 {
		t.Fatalf("bare: %+v err=%v", servers, err)
	}
}

func TestInstallMCPSpecsLocalAndRemote(t *testing.T) {
	cfg := corelib.AppConfig{}
	specs := map[string]mcpServerSpec{
		"local-one":  {Command: "uv", Args: []string{"run", "tool"}, Cwd: "."},
		"remote-one": {URL: "https://mcp.example/mcp"},
		"zzz-last":   {Command: "npx", Args: []string{"-y", "pkg"}},
	}
	names := installMCPSpecs(&cfg, specs, `D:\plugins\demo`, "demo")
	if len(names) != 3 {
		t.Fatalf("names = %v", names)
	}
	// Stable alphabetical order
	if names[0] != "local-one" || names[1] != "remote-one" || names[2] != "zzz-last" {
		t.Fatalf("order = %v", names)
	}
	if len(cfg.LocalMCPServers) != 2 {
		t.Fatalf("local count = %d", len(cfg.LocalMCPServers))
	}
	var uvEntry *corelib.LocalMCPServerEntry
	for i := range cfg.LocalMCPServers {
		if cfg.LocalMCPServers[i].Command == "uv" {
			uvEntry = &cfg.LocalMCPServers[i]
		}
	}
	if uvEntry == nil {
		t.Fatal("missing uv entry")
	}
	joined := strings.Join(uvEntry.Args, " ")
	if !strings.Contains(joined, "--directory") {
		t.Fatalf("expected --directory in args: %v", uvEntry.Args)
	}
	if uvEntry.Env["MACLAW_PLUGIN_ROOT"] == "" {
		t.Fatalf("expected MACLAW_PLUGIN_ROOT env, got %#v", uvEntry.Env)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].EndpointURL != "https://mcp.example/mcp" {
		t.Fatalf("remote = %+v", cfg.MCPServers)
	}
}

func TestEnsureUVDirectoryArg(t *testing.T) {
	got := ensureUVDirectoryArg("uv", []string{"run", "tool"}, `/tmp/p`)
	if strings.Join(got, " ") != "run --directory /tmp/p tool" {
		t.Fatalf("got %v", got)
	}
	// Already has --project
	got = ensureUVDirectoryArg("uv", []string{"run", "--project", "/x", "tool"}, `/tmp/p`)
	if strings.Join(got, " ") != "run --project /x tool" {
		t.Fatalf("got %v", got)
	}
	// Non-uv unchanged
	got = ensureUVDirectoryArg("npx", []string{"-y", "pkg"}, `/tmp/p`)
	if strings.Join(got, " ") != "-y pkg" {
		t.Fatalf("got %v", got)
	}
}

func TestMarketplaceStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dir)

	store := pluginMarketplaceStore{
		Version: 1,
		Marketplaces: []pluginMarketplaceEntry{{
			Name:   "mrexodia",
			Source: "mrexodia/codex-marketplace",
			Repo:   "mrexodia/codex-marketplace",
			Plugins: []marketplacePlugin{{
				Name: "ida-pro-mcp",
			}},
		}},
	}
	if err := savePluginMarketplaceStore(store); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPluginMarketplaceStore()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Marketplaces) != 1 || loaded.Marketplaces[0].Name != "mrexodia" {
		t.Fatalf("loaded = %+v", loaded)
	}
	m, ok := findMarketplace(loaded, "mrexodia")
	if !ok || m.Repo != "mrexodia/codex-marketplace" {
		t.Fatalf("findMarketplace = %+v %v", m, ok)
	}
}

func TestFetchMarketplaceManifestLocal(t *testing.T) {
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, ".agents", "plugins")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "name": "local-market",
  "plugins": [{"name": "demo", "source": "./plugins/demo", "description": "Demo plugin"}]
}`
	if err := os.WriteFile(filepath.Join(manifestDir, "marketplace.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entry, err := fetchMarketplaceManifest(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "local-market" || len(entry.Plugins) != 1 || entry.Plugins[0].Name != "demo" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestInstallSkillsFromPluginDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	// PrimarySkillsDir uses MaclawSkillsDir which may not follow MACLAW_DATA_DIR.
	// Still verify copy works into the returned dest when skill dirs exist.
	pluginDir := t.TempDir()
	skillDir := filepath.Join(pluginDir, "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: hello\n---\n# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := installSkillsFromPluginDir(pluginDir, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "hello" {
		t.Fatalf("names = %v", names)
	}
}

func TestExpandPluginPlaceholders(t *testing.T) {
	got := expandPluginPlaceholders("run --project ${CLAUDE_PLUGIN_ROOT}", `/tmp/p`)
	if got != "run --project /tmp/p" {
		t.Fatalf("got %q", got)
	}
}

func TestUnzipToRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("pwned"))
	w2, err := zw.Create("safe/ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w2.Write([]byte("ok"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("zip slip wrote outside dest")
	}
	if _, err := os.Stat(filepath.Join(dest, "safe", "ok.txt")); err != nil {
		t.Fatalf("safe file missing: %v", err)
	}
}

func TestPathUnderRoot(t *testing.T) {
	root := filepath.Join("a", "b")
	if !pathUnderRoot(filepath.Join("a", "b", "c"), root) {
		t.Fatal("child should be under root")
	}
	if pathUnderRoot(filepath.Join("a", "other"), root) {
		t.Fatal("sibling should not be under root")
	}
	if !pathUnderRoot(root, root) {
		t.Fatal("root is under itself")
	}
}

func TestUserPluginsDirUsesDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dir)
	got := userPluginsDir()
	if got != filepath.Join(dir, "plugins") {
		t.Fatalf("got %q", got)
	}
}

func TestInstallPluginFromLocalSource(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)

	// Local plugin package with Codex-style layout.
	pluginSrc := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pluginSrc, ".codex-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginSrc, ".codex-plugin", "plugin.json"), []byte(`{
  "name": "demo-plugin",
  "version": "1.0.0",
  "description": "demo",
  "mcpServers": "./.codex-plugin/mcp.json"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginSrc, ".codex-plugin", "mcp.json"), []byte(`{
  "mcpServers": {
    "demo-mcp": {
      "command": "npx",
      "args": ["-y", "demo"]
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(pluginSrc, "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := pluginMarketplaceStore{Version: 1}
	m := pluginMarketplaceEntry{Name: "local"}
	p := marketplacePlugin{Name: "demo-plugin", SourceType: "url", SourceURL: pluginSrc}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	installed, err := installPluginFromMarketplace(ctx, &store, m, p)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Spec != "demo-plugin@local" {
		t.Fatalf("spec = %q", installed.Spec)
	}
	if len(installed.MCPNames) != 1 || installed.MCPNames[0] != "demo-mcp" {
		t.Fatalf("mcp = %v", installed.MCPNames)
	}
	if len(installed.SkillNames) != 1 || installed.SkillNames[0] != "demo-skill" {
		t.Fatalf("skills = %v", installed.SkillNames)
	}

	// Config should contain the MCP entry under MACLAW_DATA_DIR.
	cfg, err := NewFileConfigStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LocalMCPServers) != 1 || cfg.LocalMCPServers[0].Name != "demo-mcp" {
		t.Fatalf("cfg local mcp = %+v", cfg.LocalMCPServers)
	}

	// Uninstall cleans config.
	if err := uninstallPluginEntry(&store, installed); err != nil {
		t.Fatal(err)
	}
	cfg, err = NewFileConfigStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LocalMCPServers) != 0 {
		t.Fatalf("expected MCP removed, got %+v", cfg.LocalMCPServers)
	}
	if len(store.Installed) != 0 {
		t.Fatalf("expected installed cleared, got %+v", store.Installed)
	}
}

func TestAnyMapString(t *testing.T) {
	m := map[string]any{"a": "x", "b": nil}
	if anyMapString(m, "a") != "x" {
		t.Fatal("a")
	}
	if anyMapString(m, "b") != "" {
		t.Fatal("b")
	}
	if anyMapString(m, "missing") != "" {
		t.Fatal("missing")
	}
	if anyMapString(nil, "a") != "" {
		t.Fatal("nil map")
	}
}
