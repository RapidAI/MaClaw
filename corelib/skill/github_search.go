package skill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
)

const (
	githubDefinitionYAML    = "yaml"
	githubDefinitionSkillMD = "skill_md"
)

// GitHubSkillCandidate represents a skill found via GitHub search.
type GitHubSkillCandidate struct {
	RepoFullName   string `json:"repo_full_name"` // "owner/repo"
	RepoURL        string `json:"repo_url"`
	Description    string `json:"description"`
	Stars          int    `json:"stars"`
	FilePath       string `json:"file_path"` // path to skill.md / skill.yaml in repo
	RawURL         string `json:"raw_url"`   // direct download URL
	Branch         string `json:"branch"`
	DefinitionType string `json:"definition_type,omitempty"`
}

// GitHubSearcher searches GitHub for supported skill definition files and imports them.
type GitHubSearcher struct {
	client *http.Client
	token  string // optional GitHub token for higher rate limits
}

// NewGitHubSearcher creates a new searcher.
// When token is empty, automatically resolves the built-in default token
// via ResolveGitHubToken(). Callers do NOT need to know about token resolution.
func NewGitHubSearcher(token string) *GitHubSearcher {
	if token == "" {
		token = ResolveGitHubToken()
	}
	return &GitHubSearcher{
		client: &http.Client{Timeout: 30 * time.Second},
		token:  token,
	}
}

// ghCodeSearchResponse is the GitHub Code Search API response.
type ghCodeSearchResponse struct {
	TotalCount int                `json:"total_count"`
	Items      []ghCodeSearchItem `json:"items"`
}

type ghCodeSearchItem struct {
	Name       string       `json:"name"`
	Path       string       `json:"path"`
	HTMLURL    string       `json:"html_url"`
	Repository ghSearchRepo `json:"repository"`
}

type ghSearchRepo struct {
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	Description   string `json:"description"`
	Stars         int    `json:"stargazers_count"`
	DefaultBranch string `json:"default_branch"`
}

type githubSearchTarget struct {
	filename       string
	definitionType string
}

// SearchGitHub searches GitHub for skill repositories matching the query.
//
// Strategy:
//  1. Repository Search API (/search/repositories) with topic:skill filter.
//     This API does NOT require authentication and finds repos tagged as skills.
//  2. Code Search API (/search/code) as fallback — only when a GitHub token
//     is configured, because this API requires authentication since 2023.
//
// Returns up to 10 candidates, deduplicated by repo.
func (gs *GitHubSearcher) SearchGitHub(query string) ([]GitHubSkillCandidate, error) {
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}

	sanitized := sanitizeGitHubQuery(query)
	if sanitized == "" {
		return nil, fmt.Errorf("query contains only special characters")
	}

	var candidates []GitHubSkillCandidate
	seen := make(map[string]bool)
	var errs []string

	// Primary: Repository Search (no auth required, topic-filtered).
	repoCandidates, err := gs.searchGitHubByRepo(sanitized)
	if err != nil {
		errs = append(errs, fmt.Sprintf("repo-search: %v", err))
	}
	for _, c := range repoCandidates {
		if !seen[c.RepoFullName] {
			seen[c.RepoFullName] = true
			candidates = append(candidates, c)
		}
	}

	// Fallback: Code Search (requires auth token).
	if gs.token != "" {
		targets := []githubSearchTarget{
			{filename: "skill.md", definitionType: githubDefinitionSkillMD},
			{filename: "SKILL.md", definitionType: githubDefinitionSkillMD},
			{filename: "skill.yaml", definitionType: githubDefinitionYAML},
			{filename: "skill.yml", definitionType: githubDefinitionYAML},
		}
		for _, target := range targets {
			resp, err := gs.searchGitHubByFilename(sanitized, target)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", target.filename, err))
				continue
			}
			for _, item := range resp.Items {
				key := item.Repository.FullName
				if !seen[key] {
					seen[key] = true
					candidates = append(candidates, newGitHubCandidate(item, target.definitionType))
				}
			}
		}
	}

	if len(candidates) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("GitHub search failed: %s", strings.Join(errs, "; "))
	}
	return candidates, nil
}

// ghRepoSearchResponse is the GitHub Repository Search API response.
type ghRepoSearchResponse struct {
	TotalCount int            `json:"total_count"`
	Items      []ghSearchRepo `json:"items"`
}

// searchGitHubByRepo uses the Repository Search API (/search/repositories)
// which does NOT require authentication. Filters by topic:skill to find
// skill repositories, then infers the skill definition file path.
func (gs *GitHubSearcher) searchGitHubByRepo(query string) ([]GitHubSkillCandidate, error) {
	// Search for repos with the "skill" topic matching the user query.
	// Also include "claude-code" topic repos as they often contain skills.
	searchQuery := fmt.Sprintf("%s topic:skill", query)
	endpoint := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&per_page=10&sort=stars&order=desc",
		url.QueryEscape(searchQuery))

	body, err := gs.httpGet(endpoint)
	if err != nil {
		return nil, err
	}

	var resp ghRepoSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse GitHub repo response: %w", err)
	}

	var candidates []GitHubSkillCandidate
	for _, repo := range resp.Items {
		branch := repo.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		candidates = append(candidates, GitHubSkillCandidate{
			RepoFullName:   repo.FullName,
			RepoURL:        repo.HTMLURL,
			Description:    repo.Description,
			Stars:          repo.Stars,
			FilePath:       "SKILL.md", // default; ImportFromCandidate will try multiple paths
			RawURL:         fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/SKILL.md", repo.FullName, branch),
			Branch:         branch,
			DefinitionType: githubDefinitionSkillMD,
		})
	}
	return candidates, nil
}

func (gs *GitHubSearcher) searchGitHubByFilename(query string, target githubSearchTarget) (*ghCodeSearchResponse, error) {
	searchQuery := fmt.Sprintf("filename:%s %s", target.filename, query)
	endpoint := fmt.Sprintf("https://api.github.com/search/code?q=%s&per_page=10",
		url.QueryEscape(searchQuery))
	body, err := gs.httpGet(endpoint)
	if err != nil {
		return nil, err
	}
	var resp ghCodeSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse GitHub response: %w", err)
	}
	return &resp, nil
}

func newGitHubCandidate(item ghCodeSearchItem, fallbackType string) GitHubSkillCandidate {
	branch := item.Repository.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	return GitHubSkillCandidate{
		RepoFullName:   item.Repository.FullName,
		RepoURL:        item.Repository.HTMLURL,
		Description:    item.Repository.Description,
		Stars:          item.Repository.Stars,
		FilePath:       item.Path,
		RawURL:         fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", item.Repository.FullName, branch, item.Path),
		Branch:         branch,
		DefinitionType: definitionTypeForPath(item.Path, fallbackType),
	}
}

// ImportFromCandidate downloads a supported GitHub skill definition file and
// converts it to an NLSkillEntry ready for local registration.
// When the initial RawURL fails (e.g. repo-search results with a guessed path),
// it tries common skill definition file paths as fallback.
func (gs *GitHubSearcher) ImportFromCandidate(c GitHubSkillCandidate) (*corelib.NLSkillEntry, error) {
	data, err := gs.httpGet(c.RawURL)
	if err == nil {
		return gs.parseCandidateData(data, c)
	}

	// Fallback: try common skill definition file paths.
	baseURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/", c.RepoFullName, c.Branch)
	fallbackPaths := []struct {
		path    string
		defType string
	}{
		{"SKILL.md", githubDefinitionSkillMD},
		{"skill.md", githubDefinitionSkillMD},
		{"skill.yaml", githubDefinitionYAML},
		{"skill.yml", githubDefinitionYAML},
	}
	for _, fb := range fallbackPaths {
		fbURL := baseURL + fb.path
		if fbURL == c.RawURL {
			continue // already tried
		}
		data, fbErr := gs.httpGet(fbURL)
		if fbErr != nil {
			continue
		}
		fc := c
		fc.RawURL = fbURL
		fc.FilePath = fb.path
		fc.DefinitionType = fb.defType
		return gs.parseCandidateData(data, fc)
	}
	return nil, fmt.Errorf("download GitHub skill file: %w", err)
}

// ImportFromRepoURL imports all skills from a GitHub repository URL.
// Supports: https://github.com/owner/repo[/tree/branch[/subpath]]
func (gs *GitHubSearcher) ImportFromRepoURL(rawURL string) ([]corelib.NLSkillEntry, error) {
	gh, ok := parseGitHubURL(rawURL)
	if !ok {
		return nil, fmt.Errorf("not a valid GitHub URL: %s", rawURL)
	}

	branches := []string{gh.branch}
	if gh.branch == "" {
		branches = []string{"main", "master"}
	}

	for _, branch := range branches {
		skills, err := gs.scanRepoTree(gh.owner, gh.repo, branch, gh.subPath, rawURL)
		if err != nil {
			continue
		}
		return skills, nil
	}
	return nil, fmt.Errorf("failed to access %s/%s", gh.owner, gh.repo)
}

// ── GitHub repo tree scanning ──────────────────────────────────────────

type ghRepo struct {
	owner, repo, branch, subPath string
}

var ghRepoRe = regexp.MustCompile(
	`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?(?:/tree/([^/]+)(/.*)?)?/?$`)

func parseGitHubURL(rawURL string) (*ghRepo, bool) {
	m := ghRepoRe.FindStringSubmatch(rawURL)
	if m == nil {
		return nil, false
	}
	return &ghRepo{
		owner:   m[1],
		repo:    m[2],
		branch:  m[3],
		subPath: strings.TrimPrefix(m[4], "/"),
	}, true
}

type ghTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type ghTreeResponse struct {
	Tree      []ghTreeEntry `json:"tree"`
	Truncated bool          `json:"truncated"`
}

func (gs *GitHubSearcher) scanRepoTree(owner, repo, branch, subPath, sourceURL string) ([]corelib.NLSkillEntry, error) {
	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		owner, repo, branch)
	body, err := gs.httpGet(treeURL)
	if err != nil {
		return nil, err
	}
	var tree ghTreeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, err
	}

	var results []corelib.NLSkillEntry
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		definitionType := definitionTypeForPath(entry.Path, "")
		if definitionType == "" {
			continue
		}
		if subPath != "" && !strings.HasPrefix(entry.Path, subPath) {
			continue
		}

		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
			owner, repo, branch, entry.Path)
		data, err := gs.httpGet(rawURL)
		if err != nil {
			log.Printf("[github-search] skip %s: download failed: %v", entry.Path, err)
			continue
		}

		candidate := GitHubSkillCandidate{
			RepoFullName:   owner + "/" + repo,
			RepoURL:        sourceURL,
			FilePath:       entry.Path,
			RawURL:         rawURL,
			Branch:         branch,
			DefinitionType: definitionType,
		}
		sk, err := gs.parseCandidateData(data, candidate)
		if err != nil {
			log.Printf("[github-search] skip %s: parse failed: %v", entry.Path, err)
			continue
		}
		results = append(results, *sk)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no supported skill definition found in %s/%s", owner, repo)
	}
	return results, nil
}

func definitionTypeForPath(filePath, fallback string) string {
	switch path.Base(filePath) {
	case "skill.yaml", "skill.yml":
		return githubDefinitionYAML
	case "skill.md", "SKILL.md":
		return githubDefinitionSkillMD
	default:
		return fallback
	}
}

func (gs *GitHubSearcher) parseCandidateData(data []byte, c GitHubSkillCandidate) (*corelib.NLSkillEntry, error) {
	switch definitionTypeForPath(c.FilePath, c.DefinitionType) {
	case githubDefinitionSkillMD:
		return gs.parseSkillMarkdown(data, c)
	case githubDefinitionYAML:
		return gs.parseSkillYAML(data, c)
	default:
		return nil, fmt.Errorf("unsupported GitHub skill file %q", c.FilePath)
	}
}

// ── YAML / Markdown parsing ────────────────────────────────────────────

// parseSkillYAML uses the local definition parser so GitHub imports get the
// same compatibility behavior as file-based skills.
func (gs *GitHubSearcher) parseSkillYAML(data []byte, c GitHubSkillCandidate) (*corelib.NLSkillEntry, error) {
	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		return nil, err
	}
	return gs.skillEntryFromDefinition(sf, c)
}

func (gs *GitHubSearcher) skillEntryFromDefinition(sf *SkillYAMLFile, c GitHubSkillCandidate) (*corelib.NLSkillEntry, error) {

	name := strings.TrimSpace(sf.Name)
	if name == "" {
		name = inferSkillName(c)
	}

	status := sf.Status
	if status == "" {
		status = "active"
	}

	producesArtifact := true
	if sf.ProducesArtifact != nil && !*sf.ProducesArtifact {
		producesArtifact = false
	}
	now := time.Now().Format(time.RFC3339)
	entry := &corelib.NLSkillEntry{
		Name:                    name,
		Description:             sf.Description,
		Triggers:                sf.Triggers,
		Steps:                   convertSkillYAMLSteps(sf.Steps),
		Status:                  status,
		Source:                  "github",
		SourceProject:           c.RepoURL,
		Platforms:               sf.Platforms,
		RequiresGUI:             sf.RequiresGUI,
		Mode:                    sf.Mode,
		ExecMode:                sf.ExecMode,
		GlobalTimeout:           sf.GlobalTimeout,
		ProducesArtifact:        producesArtifact,
		Operations:              convertSkillYAMLOperations(sf.Operations),
		Params:                  convertSkillYAMLParams(sf.Params),
		RequiredArgs:            sf.RequiredArgs,
		RequiredEnv:             sf.RequiredEnv,
		PreferredShell:          sf.PreferredShell,
		RequiresTools:           sf.RequiresTools,
		FallbackForTools:        sf.FallbackForTools,
		RequiresToolsets:        sf.RequiresToolsets,
		FallbackForToolsets:     sf.FallbackForToolsets,
		RequiredCredentialFiles: sf.RequiredCredentialFiles,
		RequiresPython:          requiresPythonFromYAML(sf.Requires),
		RequiresNode:            requiresNodeFromYAML(sf.Requires),
		Stateful:                sf.Stateful,
		Pipeline:                convertPipelineSteps(sf.Pipeline),
		TrustLevel:              "community",
		CreatedAt:               now,
	}
	NormalizeSkillForRunner(entry)
	return entry, nil
}

func (gs *GitHubSearcher) parseSkillMarkdown(data []byte, c GitHubSkillCandidate) (*corelib.NLSkillEntry, error) {
	// Try Claude SKILL.md format first (YAML frontmatter with allowed-tools/tools).
	// This enables importing skills from awesome-claude-skills and similar repos.
	if IsClaudeSKILLMD(data) {
		// For GitHub imports we don't have a local skillDir, so scripts/ won't
		// resolve. Use the markdown body as craft_tool instructions instead,
		// but preserve the structured name/description from frontmatter.
		entry, err := parseClaudeSKILLMDForGitHub(data, c)
		if err == nil {
			return entry, nil
		}
		// Fall through to standard markdown parsing on error.
	}

	parsed, err := parseSkillMarkdownDocument(string(data), inferSkillName(c), strings.TrimSpace(c.Description))
	if err != nil {
		return nil, err
	}
	triggers := inferSkillTriggers(parsed.name, c)
	if len(parsed.triggers) > 0 {
		triggers = parsed.triggers
	}
	producesArtifact := false
	if parsed.producesArtifact != nil {
		producesArtifact = *parsed.producesArtifact
	}
	requiresGUI := false
	if parsed.requiresGUI != nil {
		requiresGUI = *parsed.requiresGUI
	}
	entry := &corelib.NLSkillEntry{
		Name:        parsed.name,
		Description: parsed.description,
		Triggers:    triggers,
		Steps: []corelib.NLSkillStep{
			{
				Action: "craft_tool",
				Params: map[string]interface{}{
					"instructions":      parsed.markdown,
					"verification_mode": markdownVerificationMode(parsed.frontmatter["verification_mode"], producesArtifact),
					"register_policy":   "manual",
				},
			},
		},
		Status:                  "active",
		CreatedAt:               time.Now().Format(time.RFC3339),
		Source:                  "github",
		SourceProject:           c.RepoURL,
		TrustLevel:              "community",
		Platforms:               parsed.platforms,
		RequiresGUI:             requiresGUI,
		Mode:                    parsed.mode,
		ExecMode:                parsed.execMode,
		GlobalTimeout:           parsed.timeout,
		ProducesArtifact:        producesArtifact,
		RequiredArgs:            parsed.requiredArgs,
		RequiredEnv:             parsed.requiredEnv,
		PreferredShell:          parsed.preferredShell,
		RequiresPython:          parsed.requiresPython,
		RequiresNode:            parsed.requiresNode,
		Operations:              parsed.operations,
		Params:                  parsed.params,
		Pipeline:                parsed.pipeline,
		RequiresTools:           parsed.requiresTools,
		FallbackForTools:        parsed.fallbackForTools,
		RequiresToolsets:        parsed.requiresToolsets,
		FallbackForToolsets:     parsed.fallbackForToolsets,
		RequiredCredentialFiles: parsed.requiredCredentialFiles,
		Stateful:                parsed.stateful,
	}
	NormalizeSkillForRunner(entry)
	return entry, nil
}

func inferSkillName(c GitHubSkillCandidate) string {
	dir := path.Dir(c.FilePath)
	if dir != "" && dir != "." {
		name := path.Base(dir)
		if strings.TrimSpace(name) != "" && name != "." {
			return name
		}
	}
	repoName := path.Base(c.RepoFullName)
	if strings.TrimSpace(repoName) != "" && repoName != "." {
		return repoName
	}
	return "github-imported-skill"
}

func inferSkillTriggers(name string, c GitHubSkillCandidate) []string {
	var triggers []string
	for _, candidate := range []string{name, inferSkillName(c), path.Base(c.RepoFullName)} {
		trigger := normalizeTrigger(candidate)
		if trigger == "" {
			continue
		}
		duplicate := false
		for _, existing := range triggers {
			if existing == trigger {
				duplicate = true
				break
			}
		}
		if !duplicate {
			triggers = append(triggers, trigger)
		}
	}
	return triggers
}

// GitHub search helpers.

// ghQualifierRe matches GitHub search qualifiers like "repo:", "path:", etc.
var ghQualifierRe = regexp.MustCompile(`\b\w+:`)

// sanitizeGitHubQuery removes GitHub search syntax special characters
// (qualifiers, boolean operators, quotes) that could break the Code Search API.
func sanitizeGitHubQuery(q string) string {
	// Remove common GitHub search qualifiers like "repo:", "path:", etc.
	q = ghQualifierRe.ReplaceAllString(q, "")
	// Remove special chars used in GitHub search syntax.
	q = strings.NewReplacer(
		`"`, "", `'`, "", "`", "",
		"(", "", ")", "",
		"[", "", "]", "",
		"NOT ", "", "AND ", "", "OR ", "",
	).Replace(q)
	return strings.TrimSpace(q)
}

func (gs *GitHubSearcher) httpGet(reqURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw-SkillSearcher/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if gs.token != "" {
		req.Header.Set("Authorization", "Bearer "+gs.token)
	}

	resp, err := gs.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		// GitHub rate limit: check X-RateLimit-Remaining header.
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("GitHub API rate limit exceeded (set GITHUB_TOKEN for higher limits)")
		}
		return nil, fmt.Errorf("HTTP 403 from %s (may be rate-limited)", reqURL)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, reqURL)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 5<<20))
}

// parseClaudeSKILLMDForGitHub parses a Claude-format SKILL.md from a GitHub
// import. Since we don't have local scripts/ directory, the skill body is
// used as craft_tool instructions while preserving the structured metadata
// from the YAML frontmatter.
func parseClaudeSKILLMDForGitHub(data []byte, c GitHubSkillCandidate) (*corelib.NLSkillEntry, error) {
	marker := []byte("---")
	parts := bytes.SplitN(data, marker, 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("no YAML frontmatter")
	}

	var meta claudeSkillMeta
	if err := yaml.Unmarshal(parts[1], &meta); err != nil {
		return nil, err
	}

	body := strings.TrimSpace(string(parts[2]))

	name := meta.Name
	if name == "" {
		name = inferSkillName(c)
	}
	desc := meta.Description
	if desc == "" {
		desc = firstMarkdownParagraph(body)
	}

	// Replace Claude-specific paths so the skill body works in our environment.
	body = replaceClaudePaths(body)

	return &corelib.NLSkillEntry{
		Name:        name,
		Description: desc,
		Triggers:    inferSkillTriggers(name, c),
		Steps: []corelib.NLSkillStep{
			{
				Action: "craft_tool",
				Params: map[string]interface{}{
					"instructions":      body,
					"verification_mode": "artifact_optional",
					"register_policy":   "manual",
				},
			},
		},
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "github",
		SourceProject: c.RepoURL,
		TrustLevel:    "community",
	}, nil
}

// replaceClaudePaths replaces Claude-specific path references in skill content
// with paths appropriate for this project. This is inspired by goskills'
// download command which replaces ~/.claude/skills with its own path.
func replaceClaudePaths(content string) string {
	replacer := strings.NewReplacer(
		"~/.claude/skills", "~/.maclaw/data/skills",
		"~/.claude/", "~/.maclaw/data/",
	)
	return replacer.Replace(content)
}
