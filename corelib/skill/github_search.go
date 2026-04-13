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

// NewGitHubSearcher creates a new searcher. token can be empty.
func NewGitHubSearcher(token string) *GitHubSearcher {
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

// SearchGitHub searches GitHub for repositories containing supported skill
// definition files matching the given query. Returns up to 10 candidates per
// definition type and merges them by repo/path.
func (gs *GitHubSearcher) SearchGitHub(query string) ([]GitHubSkillCandidate, error) {
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}

	// Sanitize query: remove GitHub search syntax special chars to avoid
	// breaking the Code Search API query.
	sanitized := sanitizeGitHubQuery(query)
	if sanitized == "" {
		return nil, fmt.Errorf("query contains only special characters")
	}

	targets := []githubSearchTarget{
		{filename: "skill.md", definitionType: githubDefinitionSkillMD},
		{filename: "SKILL.md", definitionType: githubDefinitionSkillMD},
		{filename: "skill.yaml", definitionType: githubDefinitionYAML},
	}
	var candidates []GitHubSkillCandidate
	seen := make(map[string]bool)
	var errs []string
	for _, target := range targets {
		resp, err := gs.searchGitHubByFilename(sanitized, target)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", target.filename, err))
			continue
		}
		for _, item := range resp.Items {
			key := item.Repository.FullName + ":" + item.Path
			if seen[key] {
				continue
			}
			seen[key] = true
			candidates = append(candidates, newGitHubCandidate(item, target.definitionType))
		}
	}
	if len(candidates) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("GitHub search failed: %s", strings.Join(errs, "; "))
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
func (gs *GitHubSearcher) ImportFromCandidate(c GitHubSkillCandidate) (*corelib.NLSkillEntry, error) {
	data, err := gs.httpGet(c.RawURL)
	if err != nil {
		return nil, fmt.Errorf("download GitHub skill file: %w", err)
	}
	return gs.parseCandidateData(data, c)
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
	case "skill.yaml":
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

// parseSkillYAML uses raw map parsing (like hubcenter's RemoteImporter)
// to avoid losing fields such as author, tags, version, permissions that
// are not present in the SkillYAMLFile struct.
func (gs *GitHubSearcher) parseSkillYAML(data []byte, c GitHubSkillCandidate) (*corelib.NLSkillEntry, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("empty YAML document")
	}

	name := strings.TrimSpace(strVal(raw, "name"))
	if name == "" {
		name = inferSkillName(c)
	}

	status := strVal(raw, "status")
	if status == "" {
		status = "active"
	}

	steps := parseRawSteps(raw)
	requiresGUI, _ := raw["requires_gui"].(bool)

	now := time.Now().Format(time.RFC3339)
	return &corelib.NLSkillEntry{
		Name:          name,
		Description:   strVal(raw, "description"),
		Triggers:      strSlice(raw, "triggers"),
		Steps:         steps,
		Status:        status,
		Source:        "github",
		SourceProject: c.RepoURL,
		Platforms:     strSlice(raw, "platforms"),
		RequiresGUI:   requiresGUI,
		TrustLevel:    "community",
		CreatedAt:     now,
	}, nil
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
	return &corelib.NLSkillEntry{
		Name:        parsed.name,
		Description: parsed.description,
		Triggers:    inferSkillTriggers(parsed.name, c),
		Steps: []corelib.NLSkillStep{
			{
				Action: "craft_tool",
				Params: map[string]interface{}{
					"instructions":      parsed.markdown,
					"verification_mode": "artifact_required",
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

// ── raw YAML helpers (mirrors hubcenter/internal/skill/remote_import.go) ──

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

func strVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func strSlice(m map[string]interface{}, key string) []string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var result []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func parseRawSteps(m map[string]interface{}) []corelib.NLSkillStep {
	raw, ok := m["steps"]
	if !ok {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var steps []corelib.NLSkillStep
	for _, item := range list {
		sm, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		step := corelib.NLSkillStep{
			Action:  strVal(sm, "action"),
			OnError: strVal(sm, "on_error"),
		}
		if params, ok := sm["params"].(map[string]interface{}); ok {
			step.Params = params
		}
		steps = append(steps, step)
	}
	return steps
}

// ── HTTP helper ────────────────────────────────────────────────────────

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
					"verification_mode": "artifact_required",
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
