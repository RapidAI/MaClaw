package commands

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// Known marketplace manifest paths (Codex / Claude / generic).
var marketplaceManifestCandidates = []string{
	".agents/plugins/marketplace.json",
	".claude-plugin/marketplace.json",
	".codex-plugin/marketplace.json",
	"marketplace.json",
}

// Known plugin manifest paths inside a plugin source tree.
var pluginManifestCandidates = []string{
	".codex-plugin/plugin.json",
	".claude-plugin/plugin.json",
	"plugin.json",
	"plugin.yaml",
	".maclaw/plugin.yaml",
}

type pluginMarketplaceStore struct {
	Version      int                      `json:"version"`
	Marketplaces []pluginMarketplaceEntry `json:"marketplaces"`
	Installed    []installedPluginEntry   `json:"installed"`
}

type pluginMarketplaceEntry struct {
	Name        string                `json:"name"`
	Source      string                `json:"source"` // owner/repo, git URL, or local path
	Repo        string                `json:"repo,omitempty"`
	Branch      string                `json:"branch,omitempty"`
	ManifestURL string                `json:"manifest_url,omitempty"`
	DisplayName string                `json:"display_name,omitempty"`
	Description string                `json:"description,omitempty"`
	AddedAt     string                `json:"added_at"`
	UpdatedAt   string                `json:"updated_at,omitempty"`
	Plugins     []marketplacePlugin   `json:"plugins,omitempty"`
}

type marketplacePlugin struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	SourceType  string `json:"source_type,omitempty"` // path | url | github
	SourcePath  string `json:"source_path,omitempty"`
	SourceURL   string `json:"source_url,omitempty"`
	SourceRepo  string `json:"source_repo,omitempty"`
}

type installedPluginEntry struct {
	Name          string   `json:"name"`
	Marketplace   string   `json:"marketplace"`
	Spec          string   `json:"spec"` // name@marketplace
	SourceURL     string   `json:"source_url,omitempty"`
	InstallDir    string   `json:"install_dir,omitempty"`
	MCPNames      []string `json:"mcp_names,omitempty"`
	SkillNames    []string `json:"skill_names,omitempty"`
	InstalledAt   string   `json:"installed_at"`
	PluginVersion string   `json:"plugin_version,omitempty"`
	Description   string   `json:"description,omitempty"`
}

type remoteMarketplaceManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Interface   *struct {
		DisplayName string `json:"displayName"`
	} `json:"interface"`
	Owner *struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"owner"`
	Plugins []remoteMarketplacePlugin `json:"plugins"`
}

type remoteMarketplacePlugin struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Source      json.RawMessage `json:"source"`
	Policy      json.RawMessage `json:"policy"`
}

type remotePluginManifest struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	MCPServers  json.RawMessage `json:"mcpServers"`
	Skills      string          `json:"skills"`
	Author      *struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"author"`
}

type mcpServerSpec struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Cwd     string            `json:"cwd"`
	URL     string            `json:"url"`
	Type    string            `json:"type"`
}

func pluginMarketplacesPath() string {
	return filepath.Join(ResolveDataDir(), "plugin_marketplaces.json")
}

func loadPluginMarketplaceStore() (pluginMarketplaceStore, error) {
	path := pluginMarketplacesPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pluginMarketplaceStore{Version: 1}, nil
		}
		return pluginMarketplaceStore{}, fmt.Errorf("read plugin marketplaces: %w", err)
	}
	if len(data) == 0 {
		return pluginMarketplaceStore{Version: 1}, nil
	}
	var store pluginMarketplaceStore
	if err := json.Unmarshal(data, &store); err != nil {
		return pluginMarketplaceStore{}, fmt.Errorf("parse plugin marketplaces: %w", err)
	}
	if store.Version == 0 {
		store.Version = 1
	}
	return store, nil
}

func savePluginMarketplaceStore(store pluginMarketplaceStore) error {
	if store.Version == 0 {
		store.Version = 1
	}
	dir := filepath.Dir(pluginMarketplacesPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir plugin marketplace dir: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.AtomicWriteFile(pluginMarketplacesPath(), data, 0o644)
}

var (
	pluginHTTPClientOnce sync.Once
	pluginHTTPClient     *http.Client
)

func pluginMarketplaceHTTPClient() *http.Client {
	pluginHTTPClientOnce.Do(func() {
		pluginHTTPClient = &http.Client{Timeout: 30 * time.Second}
	})
	return pluginHTTPClient
}

// userPluginsDir is where marketplace plugin packages are materialized.
// Prefer ResolveDataDir so MACLAW_DATA_DIR isolates installs in tests/profiles;
// fall back to MaclawBaseDir/plugins which runtime discovery also scans.
func userPluginsDir() string {
	return filepath.Join(ResolveDataDir(), "plugins")
}

func httpGetBytes(ctx context.Context, rawURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "MaClaw-TUI/1.0")
	req.Header.Set("Accept", "application/json, application/octet-stream, */*")
	resp, err := pluginMarketplaceHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// anyMapString returns a trimmed string from a JSON-decoded map value.
// Unlike fmt.Sprint, missing/nil values become "" instead of "<nil>".
func anyMapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		s := strings.TrimSpace(fmt.Sprint(t))
		if s == "<nil>" {
			return ""
		}
		return s
	}
}

func parseOwnerRepo(source string) (owner, repo string, ok bool) {
	s := strings.TrimSpace(source)
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	if strings.HasPrefix(s, "https://github.com/") {
		s = strings.TrimPrefix(s, "https://github.com/")
	} else if strings.HasPrefix(s, "http://github.com/") {
		s = strings.TrimPrefix(s, "http://github.com/")
	} else if strings.HasPrefix(s, "git@github.com:") {
		s = strings.TrimPrefix(s, "git@github.com:")
	}
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if strings.Contains(parts[0], ":") || strings.Contains(parts[1], ":") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func githubDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	body, status, err := httpGetBytes(ctx, fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo))
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("GitHub API HTTP %d for %s/%s", status, owner, repo)
	}
	var meta struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", err
	}
	if meta.DefaultBranch == "" {
		return "main", nil
	}
	return meta.DefaultBranch, nil
}

func fetchMarketplaceManifest(ctx context.Context, source string) (pluginMarketplaceEntry, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return pluginMarketplaceEntry{}, fmt.Errorf("marketplace source is required")
	}

	// Local directory marketplace.
	if st, err := os.Stat(source); err == nil && st.IsDir() {
		for _, rel := range marketplaceManifestCandidates {
			path := filepath.Join(source, filepath.FromSlash(rel))
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			entry, err := parseMarketplaceManifestBytes(data, source, "", "", path)
			if err != nil {
				return pluginMarketplaceEntry{}, err
			}
			return entry, nil
		}
		return pluginMarketplaceEntry{}, fmt.Errorf("no marketplace.json found under %s", source)
	}

	owner, repo, ok := parseOwnerRepo(source)
	if !ok {
		return pluginMarketplaceEntry{}, fmt.Errorf("invalid marketplace source %q (use owner/repo, GitHub URL, or local path)", source)
	}
	branch, err := githubDefaultBranch(ctx, owner, repo)
	if err != nil {
		// Fall back to common branch names when API is rate-limited.
		branch = ""
	}
	branches := []string{}
	if branch != "" {
		branches = append(branches, branch)
	}
	for _, b := range []string{"main", "master"} {
		if b != branch {
			branches = append(branches, b)
		}
	}
	var lastErr error
	for _, b := range branches {
		for _, rel := range marketplaceManifestCandidates {
			rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, b, rel)
			data, status, err := httpGetBytes(ctx, rawURL)
			if err != nil {
				lastErr = err
				continue
			}
			if status != http.StatusOK {
				lastErr = fmt.Errorf("HTTP %d for %s", status, rawURL)
				continue
			}
			entry, err := parseMarketplaceManifestBytes(data, source, owner+"/"+repo, b, rawURL)
			if err != nil {
				return pluginMarketplaceEntry{}, err
			}
			return entry, nil
		}
	}
	if lastErr != nil {
		return pluginMarketplaceEntry{}, fmt.Errorf("fetch marketplace from %s/%s: %w", owner, repo, lastErr)
	}
	return pluginMarketplaceEntry{}, fmt.Errorf("no marketplace.json found in %s/%s", owner, repo)
}

func parseMarketplaceManifestBytes(data []byte, source, repo, branch, manifestURL string) (pluginMarketplaceEntry, error) {
	var raw remoteMarketplaceManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return pluginMarketplaceEntry{}, fmt.Errorf("parse marketplace manifest: %w", err)
	}
	name := strings.TrimSpace(raw.Name)
	if name == "" && repo != "" {
		// Prefer owner segment as marketplace name (Codex style: name@mrexodia).
		if owner, _, ok := parseOwnerRepo(repo); ok {
			name = owner
		} else {
			name = repo
		}
	}
	if name == "" {
		name = filepath.Base(strings.TrimSuffix(source, string(filepath.Separator)))
	}
	display := name
	if raw.Interface != nil && raw.Interface.DisplayName != "" {
		display = raw.Interface.DisplayName
	}
	now := time.Now().UTC().Format(time.RFC3339)
	entry := pluginMarketplaceEntry{
		Name:        name,
		Source:      source,
		Repo:        repo,
		Branch:      branch,
		ManifestURL: manifestURL,
		DisplayName: display,
		Description: raw.Description,
		AddedAt:     now,
		UpdatedAt:   now,
	}
	for _, p := range raw.Plugins {
		mp, err := normalizeMarketplacePlugin(p)
		if err != nil {
			continue
		}
		entry.Plugins = append(entry.Plugins, mp)
	}
	return entry, nil
}

func normalizeMarketplacePlugin(p remoteMarketplacePlugin) (marketplacePlugin, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return marketplacePlugin{}, fmt.Errorf("plugin name required")
	}
	out := marketplacePlugin{
		Name:        name,
		Description: strings.TrimSpace(p.Description),
		Category:    strings.TrimSpace(p.Category),
	}
	if len(p.Source) == 0 {
		return out, nil
	}
	// source may be a string path or an object.
	var asString string
	if err := json.Unmarshal(p.Source, &asString); err == nil {
		asString = strings.TrimSpace(asString)
		if asString != "" {
			out.SourceType = "path"
			out.SourcePath = asString
		}
		return out, nil
	}
	var asObj map[string]any
	if err := json.Unmarshal(p.Source, &asObj); err != nil {
		return out, nil
	}
	srcType := strings.ToLower(anyMapString(asObj, "source"))
	switch srcType {
	case "url":
		out.SourceType = "url"
		out.SourceURL = anyMapString(asObj, "url")
	case "github":
		out.SourceType = "github"
		out.SourceRepo = firstNonEmpty(anyMapString(asObj, "repo"), anyMapString(asObj, "url"))
	case "path":
		out.SourceType = "path"
		out.SourcePath = anyMapString(asObj, "path")
	default:
		// Missing/unknown type: infer from concrete fields (url > github repo > path).
		if u := anyMapString(asObj, "url"); u != "" {
			// GitHub-ish URLs without explicit type still count as url sources.
			out.SourceType = "url"
			out.SourceURL = u
		} else if repo := anyMapString(asObj, "repo"); repo != "" {
			out.SourceType = "github"
			out.SourceRepo = repo
		} else if path := anyMapString(asObj, "path"); path != "" {
			out.SourceType = "path"
			out.SourcePath = path
		} else if srcType != "" && srcType != "<nil>" {
			// Bare relative path sometimes appears as the source string field value
			// already handled above; keep unknown typed objects empty rather than
			// inventing a path.
		}
	}
	return out, nil
}

func pluginSpecParts(spec string) (name, marketplace string, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", false
	}
	if i := strings.LastIndex(spec, "@"); i > 0 && i < len(spec)-1 {
		return strings.TrimSpace(spec[:i]), strings.TrimSpace(spec[i+1:]), true
	}
	return spec, "", true
}

func findMarketplace(store pluginMarketplaceStore, key string) (pluginMarketplaceEntry, bool) {
	key = strings.TrimSpace(strings.ToLower(key))
	for _, m := range store.Marketplaces {
		if strings.EqualFold(m.Name, key) || strings.EqualFold(m.DisplayName, key) || strings.EqualFold(m.Repo, key) || strings.EqualFold(m.Source, key) {
			return m, true
		}
		if owner, _, ok := parseOwnerRepo(m.Repo); ok && strings.EqualFold(owner, key) {
			return m, true
		}
		if owner, _, ok := parseOwnerRepo(m.Source); ok && strings.EqualFold(owner, key) {
			return m, true
		}
	}
	return pluginMarketplaceEntry{}, false
}

func findMarketplacePlugin(m pluginMarketplaceEntry, name string) (marketplacePlugin, bool) {
	for _, p := range m.Plugins {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return marketplacePlugin{}, false
}

func resolvePluginSourceURL(m pluginMarketplaceEntry, p marketplacePlugin) (string, error) {
	switch strings.ToLower(p.SourceType) {
	case "url":
		if p.SourceURL == "" {
			return "", fmt.Errorf("plugin %s has empty url source", p.Name)
		}
		return p.SourceURL, nil
	case "github":
		if p.SourceRepo == "" {
			return "", fmt.Errorf("plugin %s has empty github source", p.Name)
		}
		if owner, repo, ok := parseOwnerRepo(p.SourceRepo); ok {
			return "https://github.com/" + owner + "/" + repo + ".git", nil
		}
		return p.SourceRepo, nil
	case "path", "":
		if p.SourcePath == "" || p.SourcePath == "." {
			if m.Repo != "" {
				return "https://github.com/" + m.Repo + ".git", nil
			}
			if owner, repo, ok := parseOwnerRepo(m.Source); ok {
				return "https://github.com/" + owner + "/" + repo + ".git", nil
			}
			if st, err := os.Stat(m.Source); err == nil && st.IsDir() {
				return m.Source, nil
			}
			return "", fmt.Errorf("cannot resolve path source for plugin %s", p.Name)
		}
		// Relative path inside marketplace repo.
		if m.Repo != "" {
			return "https://github.com/" + m.Repo + ".git#" + strings.TrimPrefix(p.SourcePath, "./"), nil
		}
		if st, err := os.Stat(m.Source); err == nil && st.IsDir() {
			return filepath.Join(m.Source, filepath.FromSlash(strings.TrimPrefix(p.SourcePath, "./"))), nil
		}
		return "", fmt.Errorf("cannot resolve relative source %s for plugin %s", p.SourcePath, p.Name)
	default:
		return "", fmt.Errorf("unsupported plugin source type %q", p.SourceType)
	}
}

func downloadGitHubArchive(ctx context.Context, owner, repo, preferredBranch, destDir string) (root string, err error) {
	branches := make([]string, 0, 3)
	addBranch := func(b string) {
		b = strings.TrimSpace(b)
		if b == "" {
			return
		}
		for _, existing := range branches {
			if existing == b {
				return
			}
		}
		branches = append(branches, b)
	}
	if preferredBranch != "" {
		addBranch(preferredBranch)
	} else if b, berr := githubDefaultBranch(ctx, owner, repo); berr == nil {
		addBranch(b)
	}
	addBranch("main")
	addBranch("master")

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}

	var lastErr error
	for _, branch := range branches {
		// Clean previous attempt residue so fallback does not mix trees.
		if entries, rerr := os.ReadDir(destDir); rerr == nil {
			for _, e := range entries {
				_ = os.RemoveAll(filepath.Join(destDir, e.Name()))
			}
		}
		archiveURL := fmt.Sprintf("https://codeload.github.com/%s/%s/zip/refs/heads/%s", owner, repo, branch)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
		if reqErr != nil {
			lastErr = reqErr
			continue
		}
		req.Header.Set("User-Agent", "MaClaw-TUI/1.0")
		resp, doErr := pluginMarketplaceHTTPClient().Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("download archive HTTP %d for %s@%s", resp.StatusCode, owner+"/"+repo, branch)
			resp.Body.Close()
			continue
		}
		tmpZip := filepath.Join(destDir, "source.zip")
		f, createErr := os.Create(tmpZip)
		if createErr != nil {
			resp.Body.Close()
			return "", createErr
		}
		_, copyErr := io.Copy(f, io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		closeErr := f.Close()
		if copyErr != nil {
			lastErr = copyErr
			_ = os.Remove(tmpZip)
			continue
		}
		if closeErr != nil {
			lastErr = closeErr
			_ = os.Remove(tmpZip)
			continue
		}
		if unzipErr := unzipTo(tmpZip, destDir); unzipErr != nil {
			lastErr = unzipErr
			_ = os.Remove(tmpZip)
			continue
		}
		_ = os.Remove(tmpZip)
		// GitHub archives extract to owner-repo-branch/
		entries, readErr := os.ReadDir(destDir)
		if readErr != nil {
			return "", readErr
		}
		for _, e := range entries {
			if e.IsDir() {
				return filepath.Join(destDir, e.Name()), nil
			}
		}
		return destDir, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("download archive failed for %s/%s", owner, repo)
}

func unzipTo(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	destAbs, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	destAbs = filepath.Clean(destAbs)

	for _, f := range r.File {
		// Reject absolute paths and traversal.
		name := filepath.ToSlash(f.Name)
		name = strings.TrimPrefix(name, "/")
		if name == "" || strings.Contains(name, "..") {
			continue
		}
		target := filepath.Join(destAbs, filepath.FromSlash(name))
		target = filepath.Clean(target)
		rel, relErr := filepath.Rel(destAbs, target)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, 32<<20))
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func materializePluginSource(ctx context.Context, sourceURL, installDir string) (string, error) {
	// Local path.
	if st, err := os.Stat(sourceURL); err == nil {
		if st.IsDir() {
			return sourceURL, nil
		}
	}
	// GitHub URL, optionally with #subdir
	subPath := ""
	cleanURL := sourceURL
	if i := strings.Index(sourceURL, "#"); i >= 0 {
		cleanURL = sourceURL[:i]
		subPath = sourceURL[i+1:]
	}
	owner, repo, ok := parseOwnerRepo(cleanURL)
	if !ok {
		return "", fmt.Errorf("unsupported plugin source %q (need GitHub URL or local path)", sourceURL)
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", err
	}
	// Clear previous install contents.
	entries, _ := os.ReadDir(installDir)
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(installDir, e.Name()))
	}
	root, err := downloadGitHubArchive(ctx, owner, repo, "", installDir)
	if err != nil {
		return "", err
	}
	if subPath != "" {
		candidate := filepath.Join(root, filepath.FromSlash(subPath))
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate, nil
		}
	}
	return root, nil
}

func loadRemotePluginManifest(pluginDir string) (remotePluginManifest, string, error) {
	for _, rel := range pluginManifestCandidates {
		path := filepath.Join(pluginDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.HasSuffix(strings.ToLower(rel), ".yaml") || strings.HasSuffix(strings.ToLower(rel), ".yml") {
			// Maclaw-native plugin.yaml is handled elsewhere; treat as minimal.
			return remotePluginManifest{Name: filepath.Base(pluginDir)}, path, nil
		}
		var m remotePluginManifest
		if err := json.Unmarshal(data, &m); err != nil {
			return remotePluginManifest{}, "", fmt.Errorf("parse %s: %w", rel, err)
		}
		if m.Name == "" {
			m.Name = filepath.Base(pluginDir)
		}
		return m, path, nil
	}
	return remotePluginManifest{Name: filepath.Base(pluginDir)}, "", nil
}

func parseMCPServersField(raw json.RawMessage, pluginDir string) (map[string]mcpServerSpec, error) {
	out := make(map[string]mcpServerSpec)
	if len(raw) == 0 {
		// Fallback files.
		for _, rel := range []string{".codex-plugin/mcp.json", ".mcp.json", "mcp.json"} {
			path := filepath.Join(pluginDir, filepath.FromSlash(rel))
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			return parseMCPServersJSON(data)
		}
		return out, nil
	}
	// String path to mcp config file.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		path := asString
		if !filepath.IsAbs(path) {
			path = filepath.Join(pluginDir, filepath.FromSlash(strings.TrimPrefix(asString, "./")))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read mcpServers file %s: %w", asString, err)
		}
		return parseMCPServersJSON(data)
	}
	// Inline map of servers.
	return parseMCPServersJSON(raw)
}

func parseMCPServersJSON(data []byte) (map[string]mcpServerSpec, error) {
	// Accept either { "mcpServers": { ... } } or bare { "name": {...} }
	var wrapped struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.MCPServers) > 0 {
		return wrapped.MCPServers, nil
	}
	var bare map[string]mcpServerSpec
	if err := json.Unmarshal(data, &bare); err != nil {
		return nil, err
	}
	// Filter non-object noise keys.
	out := make(map[string]mcpServerSpec, len(bare))
	for k, v := range bare {
		if k == "mcpServers" {
			continue
		}
		out[k] = v
	}
	return out, nil
}

func expandPluginPlaceholders(s, pluginRoot string) string {
	replacer := strings.NewReplacer(
		"${CLAUDE_PLUGIN_ROOT}", pluginRoot,
		"${CODEX_PLUGIN_ROOT}", pluginRoot,
		"${MACLAW_PLUGIN_ROOT}", pluginRoot,
		"${pluginRoot}", pluginRoot,
		"${PLUGIN_ROOT}", pluginRoot,
	)
	return replacer.Replace(s)
}

func expandPluginArgs(args []string, pluginRoot string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, expandPluginPlaceholders(a, pluginRoot))
	}
	return out
}

func expandPluginEnv(env map[string]string, pluginRoot string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = expandPluginPlaceholders(v, pluginRoot)
	}
	return out
}

func installMCPSpecs(cfg *corelib.AppConfig, specs map[string]mcpServerSpec, pluginRoot, pluginName string) []string {
	_ = pluginName // reserved for future namespacing of server keys
	now := time.Now().UTC().Format(time.RFC3339)
	// Stable order for deterministic config/tests.
	keys := make([]string, 0, len(specs))
	for name := range specs {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	var names []string
	for _, name := range keys {
		spec := specs[name]
		// Keep server key as MCP name for tool discovery.
		displayName := name
		// Remote MCP via URL.
		if endpoint := strings.TrimSpace(spec.URL); endpoint != "" {
			entry := corelib.MCPServerEntry{
				ID:          fmt.Sprintf("plugin-%s-%d", sanitizePluginDirName(displayName), time.Now().UnixMilli()),
				Name:        displayName,
				EndpointURL: expandPluginPlaceholders(endpoint, pluginRoot),
				AuthType:    "none",
				CreatedAt:   now,
				Source:      corelib.MCPSourceMarket,
			}
			upsertRemoteMCPConfig(cfg, entry)
			names = append(names, displayName)
			continue
		}
		cmd := strings.TrimSpace(expandPluginPlaceholders(spec.Command, pluginRoot))
		if cmd == "" {
			continue
		}
		cwd := strings.TrimSpace(expandPluginPlaceholders(spec.Cwd, pluginRoot))
		args := expandPluginArgs(spec.Args, pluginRoot)
		// If cwd is relative ".", bind to plugin root.
		if cwd == "" || cwd == "." {
			cwd = pluginRoot
		}
		env := expandPluginEnv(spec.Env, pluginRoot)
		if env == nil {
			env = map[string]string{}
		}
		if pluginRoot != "" {
			env["MACLAW_PLUGIN_ROOT"] = pluginRoot
		}
		args = ensureUVDirectoryArg(cmd, args, cwd)
		entry := corelib.LocalMCPServerEntry{
			ID:        fmt.Sprintf("plugin-%s-%d", sanitizePluginDirName(displayName), time.Now().UnixMilli()),
			Name:      displayName,
			Command:   cmd,
			Args:      args,
			Env:       env,
			AutoStart: true,
			CreatedAt: now,
			Source:    corelib.MCPSourceMarket,
		}
		upsertLocalMCPConfig(cfg, entry)
		names = append(names, displayName)
	}
	return names
}

// ensureUVDirectoryArg injects `uv --directory <cwd>` when the plugin relies on
// a project-local pyproject (Codex/Claude plugin packages).
func ensureUVDirectoryArg(cmd string, args []string, cwd string) []string {
	if cwd == "" {
		return args
	}
	base := strings.ToLower(filepath.Base(cmd))
	if base != "uv" && base != "uv.exe" {
		return args
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project", "--directory", "-C":
			return args
		}
	}
	for i, a := range args {
		if a == "run" {
			next := make([]string, 0, len(args)+2)
			next = append(next, args[:i+1]...)
			next = append(next, "--directory", cwd)
			next = append(next, args[i+1:]...)
			return next
		}
	}
	return append([]string{"--directory", cwd}, args...)
}

func upsertRemoteMCPConfig(cfg *corelib.AppConfig, entry corelib.MCPServerEntry) {
	for i, existing := range cfg.MCPServers {
		if strings.EqualFold(existing.Name, entry.Name) {
			entry.ID = existing.ID
			if entry.CreatedAt == "" {
				entry.CreatedAt = existing.CreatedAt
			}
			cfg.MCPServers[i] = entry
			return
		}
	}
	cfg.MCPServers = append(cfg.MCPServers, entry)
}

func upsertLocalMCPConfig(cfg *corelib.AppConfig, entry corelib.LocalMCPServerEntry) {
	for i, existing := range cfg.LocalMCPServers {
		if strings.EqualFold(existing.Name, entry.Name) {
			entry.ID = existing.ID
			if entry.CreatedAt == "" {
				entry.CreatedAt = existing.CreatedAt
			}
			cfg.LocalMCPServers[i] = entry
			return
		}
	}
	cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
}

func installSkillsFromPluginDir(pluginDir, pluginName string) ([]string, error) {
	var skillRoots []string
	// Common layouts: skills/, skill/, ./SKILL.md
	for _, rel := range []string{"skills", "skill", ".agents/skills"} {
		p := filepath.Join(pluginDir, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			skillRoots = append(skillRoots, p)
		}
	}
	// Direct SKILL.md at plugin root.
	for _, doc := range []string{"SKILL.md", "skill.md", "skill.yaml", "skill.yml"} {
		if _, err := os.Stat(filepath.Join(pluginDir, doc)); err == nil {
			skillRoots = append(skillRoots, pluginDir)
			break
		}
	}
	if len(skillRoots) == 0 {
		return nil, nil
	}

	destRoot, err := skill.PrimarySkillsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return nil, err
	}

	var installed []string
	seen := map[string]bool{}
	for _, root := range skillRoots {
		// If root itself is a single skill.
		if isSkillDir(root) {
			name := filepath.Base(root)
			if root == pluginDir {
				name = pluginName
			}
			if name == "" {
				name = filepath.Base(pluginDir)
			}
			if err := copySkillDir(root, filepath.Join(destRoot, name)); err != nil {
				return installed, err
			}
			if !seen[name] {
				installed = append(installed, name)
				seen[name] = true
			}
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			src := filepath.Join(root, e.Name())
			if !isSkillDir(src) {
				continue
			}
			name := e.Name()
			if err := copySkillDir(src, filepath.Join(destRoot, name)); err != nil {
				return installed, err
			}
			if !seen[name] {
				installed = append(installed, name)
				seen[name] = true
			}
		}
	}
	return installed, nil
}

func isSkillDir(dir string) bool {
	for _, name := range []string{"SKILL.md", "skill.md", "skill.yaml", "skill.yml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func copySkillDir(src, dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	return filepath.Walk(srcAbs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("invalid skill path: %s", path)
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(destAbs, rel)
		if !pathUnderRoot(target, destAbs) {
			return fmt.Errorf("skill copy escapes destination: %s", rel)
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		// Skip very large skill assets (>8 MiB) to avoid bloating the skills tree.
		if info.Size() > 8<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		return fileutil.AtomicWriteFile(target, data, mode)
	})
}

func installPluginFromMarketplace(ctx context.Context, store *pluginMarketplaceStore, m pluginMarketplaceEntry, p marketplacePlugin) (installedPluginEntry, error) {
	sourceURL, err := resolvePluginSourceURL(m, p)
	if err != nil {
		return installedPluginEntry{}, err
	}
	installRoot := filepath.Join(userPluginsDir(), sanitizePluginDirName(p.Name))
	pluginDir, err := materializePluginSource(ctx, sourceURL, installRoot)
	if err != nil {
		return installedPluginEntry{}, fmt.Errorf("download plugin source: %w", err)
	}

	manifest, _, err := loadRemotePluginManifest(pluginDir)
	if err != nil {
		return installedPluginEntry{}, err
	}
	// Prefer marketplace listing name for the install spec; fall back to manifest.
	name := firstNonEmpty(p.Name, manifest.Name, filepath.Base(pluginDir))

	// Security checks for local command / remote URL installs.
	cfgStore := NewFileConfigStore(ResolveDataDir())
	cfg, err := cfgStore.LoadConfig()
	if err != nil {
		return installedPluginEntry{}, err
	}
	// Reinstall: drop previous MCP entries owned by this plugin so renames don't orphan servers.
	marketName := firstNonEmpty(m.Name, "local")
	for _, existing := range store.Installed {
		if strings.EqualFold(existing.Name, name) && strings.EqualFold(existing.Marketplace, marketName) {
			removeMCPNamesFromConfig(&cfg, existing.MCPNames)
			break
		}
	}
	mcpSpecs, err := parseMCPServersField(manifest.MCPServers, pluginDir)
	if err != nil {
		return installedPluginEntry{}, err
	}
	if len(mcpSpecs) == 0 {
		// Manifest may omit mcpServers and only ship .codex-plugin/mcp.json / .mcp.json.
		if fallback, ferr := parseMCPServersField(nil, pluginDir); ferr == nil && len(fallback) > 0 {
			mcpSpecs = fallback
		}
	}
	for _, spec := range mcpSpecs {
		if endpoint := strings.TrimSpace(spec.URL); endpoint != "" {
			if ok, reason := clientsecurity.EnforceConfig(cfg, "web_fetch", map[string]interface{}{"url": endpoint}); !ok {
				return installedPluginEntry{}, fmt.Errorf("%s", reason)
			}
		}
		if cmd := strings.TrimSpace(spec.Command); cmd != "" {
			cmdText := strings.Join(append([]string{cmd}, spec.Args...), " ")
			if ok, reason := clientsecurity.EnforceConfig(cfg, "bash", map[string]interface{}{"command": cmdText}); !ok {
				return installedPluginEntry{}, fmt.Errorf("%s", reason)
			}
		}
	}

	mcpNames := installMCPSpecs(&cfg, mcpSpecs, pluginDir, name)
	skillNames, err := installSkillsFromPluginDir(pluginDir, name)
	if err != nil {
		return installedPluginEntry{}, fmt.Errorf("install skills: %w", err)
	}
	if len(mcpNames) == 0 && len(skillNames) == 0 {
		return installedPluginEntry{}, fmt.Errorf("plugin %q has no MCP servers or skills to install", name)
	}
	if err := cfgStore.SaveConfig(cfg); err != nil {
		return installedPluginEntry{}, err
	}

	spec := name + "@" + marketName
	entry := installedPluginEntry{
		Name:          name,
		Marketplace:   marketName,
		Spec:          spec,
		SourceURL:     sourceURL,
		InstallDir:    pluginDir,
		MCPNames:      mcpNames,
		SkillNames:    skillNames,
		InstalledAt:   time.Now().UTC().Format(time.RFC3339),
		PluginVersion: manifest.Version,
		Description:   firstNonEmpty(manifest.Description, p.Description),
	}

	// Upsert installed list (match by spec, then name+marketplace).
	replaced := false
	for i, existing := range store.Installed {
		if strings.EqualFold(existing.Spec, entry.Spec) ||
			(strings.EqualFold(existing.Name, entry.Name) && strings.EqualFold(existing.Marketplace, entry.Marketplace)) {
			// On reinstall, drop previous MCP/skill names that are no longer present.
			entry.InstalledAt = existing.InstalledAt
			if entry.InstalledAt == "" {
				entry.InstalledAt = time.Now().UTC().Format(time.RFC3339)
			}
			store.Installed[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		store.Installed = append(store.Installed, entry)
	}
	return entry, nil
}

func pathUnderRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func removeMCPNamesFromConfig(cfg *corelib.AppConfig, names []string) {
	if cfg == nil || len(names) == 0 {
		return
	}
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[strings.ToLower(strings.TrimSpace(n))] = true
	}
	if len(nameSet) == 0 {
		return
	}
	remote := make([]corelib.MCPServerEntry, 0, len(cfg.MCPServers))
	for _, s := range cfg.MCPServers {
		if !nameSet[strings.ToLower(s.Name)] {
			remote = append(remote, s)
		}
	}
	if len(remote) == 0 {
		cfg.MCPServers = nil
	} else {
		cfg.MCPServers = remote
	}
	local := make([]corelib.LocalMCPServerEntry, 0, len(cfg.LocalMCPServers))
	for _, s := range cfg.LocalMCPServers {
		if !nameSet[strings.ToLower(s.Name)] {
			local = append(local, s)
		}
	}
	if len(local) == 0 {
		cfg.LocalMCPServers = nil
	} else {
		cfg.LocalMCPServers = local
	}
}

func uninstallPluginEntry(store *pluginMarketplaceStore, entry installedPluginEntry) error {
	cfgStore := NewFileConfigStore(ResolveDataDir())
	cfg, err := cfgStore.LoadConfig()
	if err != nil {
		return err
	}
	removeMCPNamesFromConfig(&cfg, entry.MCPNames)
	if err := cfgStore.SaveConfig(cfg); err != nil {
		return err
	}

	// Remove skill directories if they still exist under primary skills dir.
	if destRoot, err := skill.PrimarySkillsDir(); err == nil {
		for _, name := range entry.SkillNames {
			skillPath := filepath.Join(destRoot, name)
			if pathUnderRoot(skillPath, destRoot) {
				_ = os.RemoveAll(skillPath)
			}
		}
	}
	// Remove plugin install package under user plugins dir.
	pluginsRoot := userPluginsDir()
	pkgDir := filepath.Join(pluginsRoot, sanitizePluginDirName(entry.Name))
	if pathUnderRoot(pkgDir, pluginsRoot) {
		_ = os.RemoveAll(pkgDir)
	}
	// Also remove legacy installs under MaclawBaseDir/plugins.
	legacyRoot := filepath.Join(corelib.MaclawBaseDir(), "plugins")
	if legacyRoot != pluginsRoot {
		legacyPkg := filepath.Join(legacyRoot, sanitizePluginDirName(entry.Name))
		if pathUnderRoot(legacyPkg, legacyRoot) {
			_ = os.RemoveAll(legacyPkg)
		}
	}

	remaining := store.Installed[:0]
	for _, existing := range store.Installed {
		if strings.EqualFold(existing.Spec, entry.Spec) || (strings.EqualFold(existing.Name, entry.Name) && strings.EqualFold(existing.Marketplace, entry.Marketplace)) {
			continue
		}
		remaining = append(remaining, existing)
	}
	store.Installed = remaining
	return nil
}

func sanitizePluginDirName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "plugin"
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// searchCapabilityMarketMCP queries HubCenter capability market for MCP entries.
func searchCapabilityMarketMCP(ctx context.Context, query string) ([]map[string]any, error) {
	base := resolveHubCenterURL()
	if base == "" {
		return nil, nil
	}
	endpoint := strings.TrimRight(base, "/") + "/api/capability-market/mcp"
	if strings.TrimSpace(query) != "" {
		endpoint += "?q=" + url.QueryEscape(query)
	}
	data, status, err := httpGetBytes(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("capability market HTTP %d", status)
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}
