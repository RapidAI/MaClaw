package skill

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"gopkg.in/yaml.v3"
)

// skillMDFrontMatter 表示 skill.md 的 YAML front-matter 结构。
type skillMDFrontMatter struct {
	Name        string   `yaml:"name"`
	License     string   `yaml:"license"`
	GitHub      string   `yaml:"github"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Platforms   []string `yaml:"platforms"`
	Permissions []string `yaml:"permissions"`
	Metadata    struct {
		Author  string `yaml:"author"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
}

// RemoteImporter 从远程 URL 抓取 skill 并转换为 maclaw 格式。
type RemoteImporter struct {
	client *http.Client
}

// Align with skill install wire budget so multi-asset remote skills can import.
const remoteImportMaxBytes = coreskill.MaxSkillPackageDownloadBytes

func NewRemoteImporter() *RemoteImporter {
	return &RemoteImporter{
		// Allow multi-asset packages within remoteImportMaxBytes.
		client: &http.Client{Timeout: 180 * time.Second},
	}
}

// ImportResult 表示一次导入的结果。
type ImportResult struct {
	Skills      []HubSkillFull  `json:"skills"`
	Suite       *SkillSuiteFull `json:"suite,omitempty"`
	PackageKind string          `json:"package_kind,omitempty"`
	Errors      []string        `json:"errors,omitempty"`
}

type suiteManifest struct {
	ID          string                `yaml:"id"`
	Name        string                `yaml:"name"`
	Description string                `yaml:"description"`
	Version     string                `yaml:"version"`
	Author      string                `yaml:"author"`
	License     string                `yaml:"license"`
	Tags        []string              `yaml:"tags"`
	Members     []suiteManifestMember `yaml:"members"`
	Skills      []suiteManifestMember `yaml:"skills"`
}

type suiteManifestMember struct {
	ID       string `yaml:"id"`
	Path     string `yaml:"path"`
	Required *bool  `yaml:"required"`
}

func (m suiteManifest) entries() []suiteManifestMember {
	if len(m.Skills) > 0 {
		return m.Skills
	}
	return m.Members
}

// gitHubRepo 解析 GitHub 仓库 URL 为 owner/repo/branch/subpath。
type gitHubRepo struct {
	Owner   string
	Repo    string
	Branch  string // 为空时自动尝试 main/master
	SubPath string // 子路径，如 "skills/foo"
}

var ghRepoRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?(?:/tree/([^/]+)(/.*)?)?/?$`)

func parseGitHubURL(rawURL string) (*gitHubRepo, bool) {
	// Remote repository imports must use GitHub's HTTPS endpoint. Reject plain
	// HTTP to avoid credential/content tampering during server-side fetches.
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "https://github.com/") {
		return nil, false
	}
	m := ghRepoRe.FindStringSubmatch(rawURL)
	if m == nil {
		return nil, false
	}
	subPath := strings.TrimPrefix(m[4], "/")
	cleanSub := path.Clean(strings.ReplaceAll(subPath, "\\", "/"))
	if cleanSub == "." {
		cleanSub = ""
	}
	if cleanSub == ".." || strings.HasPrefix(cleanSub, "../") || strings.Contains(cleanSub, "/../") {
		return nil, false
	}
	return &gitHubRepo{Owner: m[1], Repo: m[2], Branch: m[3], SubPath: cleanSub}, true
}

// ImportFromURL 从 URL 导入 skill(s)。
// 支持：
//   - GitHub 仓库 URL → 递归扫描子目录找 skill.yaml
//   - 直接 raw URL 指向 skill.yaml
func (ri *RemoteImporter) ImportFromURL(rawURL string) (*ImportResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("URL is empty")
	}
	if strings.HasPrefix(strings.ToLower(rawURL), "http://github.com/") {
		return nil, fmt.Errorf("GitHub repository URL must use HTTPS")
	}
	if gh, ok := parseGitHubURL(rawURL); ok {
		return ri.importFromGitHub(gh, rawURL)
	}
	return ri.importFromRawURL(rawURL)
}

// importFromGitHub 使用 GitHub API 递归扫描仓库目录树。
func (ri *RemoteImporter) importFromGitHub(gh *gitHubRepo, sourceURL string) (*ImportResult, error) {
	branches := []string{gh.Branch}
	if gh.Branch == "" {
		branches = []string{"main", "master"}
	}
	for _, branch := range branches {
		result, err := ri.scanGitHubTree(gh.Owner, gh.Repo, branch, gh.SubPath, sourceURL)
		if err != nil {
			log.Printf("[remote_import] branch %s failed: %v", branch, err)
			continue
		}
		return result, nil
	}
	return nil, fmt.Errorf("failed to access GitHub repo %s/%s (tried branches: %v); note: unauthenticated GitHub API is limited to 60 requests/hour", gh.Owner, gh.Repo, branches)
}

// ghTreeEntry 是 GitHub Trees API 返回的条目。
type ghTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
	Mode string `json:"mode,omitempty"`
}

type ghTreeResponse struct {
	SHA       string        `json:"sha"`
	Tree      []ghTreeEntry `json:"tree"`
	Truncated bool          `json:"truncated"`
}

type ghCommitResponse struct {
	SHA string `json:"sha"`
}

func (ri *RemoteImporter) scanGitHubTree(owner, repo, branch, subPath, sourceURL string) (*ImportResult, error) {
	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch)
	body, err := ri.httpGet(treeURL)
	if err != nil {
		return nil, fmt.Errorf("fetch tree: %w", err)
	}
	var tree ghTreeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, fmt.Errorf("parse tree: %w", err)
	}
	if tree.Truncated {
		return nil, fmt.Errorf("GitHub repository tree is truncated; refusing partial Suite import")
	}
	// Git tree mode 120000 denotes a symbolic link. Following links during
	// import would violate the archive path-safety contract, so reject them
	// before fetching any content. GitHub may also expose an explicit symlink
	// type in compatible API responses.
	for _, entry := range tree.Tree {
		if !isWithinImportSubpath(entry.Path, subPath) {
			continue
		}
		if entry.Type == "symlink" || entry.Mode == "120000" {
			return nil, fmt.Errorf("GitHub repository contains unsupported symbolic link: %s", entry.Path)
		}
	}

	// 收集已有 skill.yaml 的目录，避免 skill.md 重复导入
	yamlDirs := make(map[string]bool)

	result := &ImportResult{}
	skillDirs := make([]string, 0)
	var sm *suiteManifest
	// An explicit suite manifest is optional. It lives at the repository root
	// (or requested subpath) and controls suite metadata while skills remain
	// independently parsed below.
	manifestPath := "suite.yaml"
	if subPath != "" {
		manifestPath = strings.TrimSuffix(subPath, "/") + "/suite.yaml"
	}
	legacyManifestPath := "skill-suite.yaml"
	if subPath != "" {
		legacyManifestPath = strings.TrimSuffix(subPath, "/") + "/skill-suite.yaml"
	}
	for _, entry := range tree.Tree {
		if entry.Type == "blob" && (entry.Path == manifestPath || entry.Path == legacyManifestPath) {
			data, fetchErr := ri.httpGet(fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, entry.Path))
			if fetchErr != nil {
				return nil, fmt.Errorf("fetch suite manifest: %w", fetchErr)
			}
			var parsed suiteManifest
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				return nil, fmt.Errorf("parse suite manifest: %w", err)
			}
			valid := true
			for _, member := range parsed.entries() {
				p := strings.ReplaceAll(strings.TrimSpace(member.Path), "\\", "/")
				clean := path.Clean(p)
				if p == "" || strings.HasPrefix(p, "/") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || (len(clean) > 1 && clean[1] == ':') {
					valid = false
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("suite manifest contains invalid member path")
			}
			sm = &parsed
		}
	}

	// 第一轮：扫描 skill.yaml（优先）
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		base := path.Base(entry.Path)
		if base != "skill.yaml" {
			continue
		}
		if subPath != "" && entry.Path != subPath && !strings.HasPrefix(entry.Path, strings.TrimSuffix(subPath, "/")+"/") {
			continue
		}

		skillDir := path.Dir(entry.Path)
		yamlDirs[skillDir] = true

		rawContentURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, entry.Path)
		yamlData, err := ri.httpGet(rawContentURL)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("fetch %s: %v", entry.Path, err))
			continue
		}

		sk, err := ri.parseSkillYAML(yamlData, sourceURL, skillDir)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("parse %s: %v", entry.Path, err))
			continue
		}
		if sm != nil && len(sm.entries()) > 0 && !manifestIncludesSkill(*sm, skillDir, sk) {
			continue
		}
		applyManifestIdentity(sm, skillDir, sk)

		ri.collectFiles(sk, &tree, skillDir, owner, repo, branch)
		// 把 skill.yaml 本身也放入 Files，客户端 scanner 需要它
		sk.Files["skill.yaml"] = base64.StdEncoding.EncodeToString(yamlData)
		result.Skills = append(result.Skills, *sk)
		skillDirs = append(skillDirs, skillDir)
	}

	// 第二轮：扫描 skill.md（仅在同目录无 skill.yaml 时）
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		base := path.Base(entry.Path)
		if base != "skill.md" {
			continue
		}
		if subPath != "" && entry.Path != subPath && !strings.HasPrefix(entry.Path, strings.TrimSuffix(subPath, "/")+"/") {
			continue
		}

		skillDir := path.Dir(entry.Path)
		if yamlDirs[skillDir] {
			continue // 同目录已有 skill.yaml，跳过
		}

		rawContentURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, entry.Path)
		mdData, err := ri.httpGet(rawContentURL)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("fetch %s: %v", entry.Path, err))
			continue
		}

		sk, err := ri.parseSkillMD(mdData, sourceURL, skillDir)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("parse %s: %v", entry.Path, err))
			continue
		}
		if sm != nil && len(sm.entries()) > 0 && !manifestIncludesSkill(*sm, skillDir, sk) {
			continue
		}
		applyManifestIdentity(sm, skillDir, sk)

		ri.collectFiles(sk, &tree, skillDir, owner, repo, branch)
		// skill.md 格式没有 skill.yaml，自动生成一个供客户端 scanner 识别
		generatedYAML := ri.generateSkillYAML(sk)
		sk.Files["skill.yaml"] = base64.StdEncoding.EncodeToString(generatedYAML)
		// 保留 skill.md 正文内容，供本地使用
		if sk.AgentSkillMD != "" {
			sk.Files["skill.md"] = base64.StdEncoding.EncodeToString(mdData)
		}
		result.Skills = append(result.Skills, *sk)
		skillDirs = append(skillDirs, skillDir)
	}

	if len(result.Skills) == 0 && len(result.Errors) == 0 {
		return nil, fmt.Errorf("no skill.yaml or skill.md found in repo %s/%s (branch: %s, subpath: %q)", owner, repo, branch, subPath)
	}
	if len(result.Skills) > 32 {
		return nil, fmt.Errorf("repository contains too many skills for one Suite (maximum 32)")
	}
	if len(result.Skills) > 1 {
		result.PackageKind = "suite"
		suiteID, name, desc, ver, author := "github."+strings.ToLower(owner)+"."+strings.ToLower(repo), repo, "", "", ""
		var tags []string
		license := ""
		if sm != nil {
			if sm.ID != "" {
				suiteID = sm.ID
			}
			if sm.Name != "" {
				name = sm.Name
			}
			desc, ver, author = sm.Description, sm.Version, sm.Author
			tags = append(tags, sm.Tags...)
			license = sm.License
		}
		if ver == "" {
			ver = highestSuiteVersion(result.Skills)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		revision := branch
		// Trees API returns a tree-object SHA, not the immutable commit SHA
		// required by the Suite source_revision contract. Resolve the branch
		// ref to its commit; retain the tree SHA only as a last-resort fallback
		// for compatibility with unusual GitHub responses.
		if commitBody, commitErr := ri.httpGet(fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", owner, repo, branch)); commitErr == nil {
			var commit ghCommitResponse
			if json.Unmarshal(commitBody, &commit) == nil && strings.TrimSpace(commit.SHA) != "" {
				revision = strings.TrimSpace(commit.SHA)
			}
		}
		if revision == branch && tree.SHA != "" {
			revision = tree.SHA
		}
		su := &SkillSuiteFull{SkillSuiteMeta: SkillSuiteMeta{ID: suiteID, Name: name, Description: desc, Version: ver, Author: author, License: license, Tags: tags, SourceURL: sourceURL, SourceRevision: revision, CreatedAt: now, UpdatedAt: now, Visible: true, Status: "published"}, Skills: append([]HubSkillFull(nil), result.Skills...), Manifest: SuiteManifest{Format: "skill-suite.v1", GeneratedAt: now}}
		permissionSet := map[string]bool{}
		for _, sk := range result.Skills {
			for _, permission := range sk.Manifest.Permissions {
				if p := strings.TrimSpace(permission); p != "" {
					permissionSet[p] = true
				}
			}
		}
		for permission := range permissionSet {
			su.Permissions = append(su.Permissions, permission)
		}
		sort.Strings(su.Permissions)
		for i := range result.Skills {
			sk := &result.Skills[i]
			memberPath := ""
			if sk.SourceURL != "" {
				memberPath = ""
			}
			required := true
			if sm != nil {
				for _, m := range sm.entries() {
					manifestPath := path.Clean(strings.ReplaceAll(strings.TrimSpace(m.Path), "\\", "/"))
					memberDir := ""
					if i < len(skillDirs) {
						memberDir = path.Clean(skillDirs[i])
					}
					if (m.ID != "" && strings.EqualFold(m.ID, sk.ID)) || (memberDir != "." && strings.EqualFold(manifestPath, memberDir)) || strings.EqualFold(path.Base(manifestPath), sk.Name) || strings.EqualFold(manifestPath, sk.Name) {
						memberPath = m.Path
						if m.Required != nil {
							required = *m.Required
						}
						break
					}
				}
			}
			su.Members = append(su.Members, SkillSuiteMember{SkillID: sk.ID, SkillRef: sk.SkillID, Name: sk.Name, Path: memberPath, Version: sk.Version, Required: required, Order: i})
		}
		if raw, marshalErr := json.Marshal(su.Skills); marshalErr == nil {
			sum := sha256.Sum256(raw)
			su.PackageSHA256 = hex.EncodeToString(sum[:])
		}
		result.Suite = su
	}
	return result, nil
}

func isWithinImportSubpath(entryPath, subPath string) bool {
	entryPath = strings.Trim(strings.ReplaceAll(entryPath, "\\", "/"), "/")
	subPath = strings.Trim(strings.ReplaceAll(subPath, "\\", "/"), "/")
	if subPath == "" {
		return true
	}
	return entryPath == subPath || strings.HasPrefix(entryPath, subPath+"/")
}

func manifestIncludesSkill(manifest suiteManifest, skillDir string, sk *HubSkillFull) bool {
	dir := path.Clean(strings.ReplaceAll(strings.TrimSpace(skillDir), "\\", "/"))
	for _, member := range manifest.entries() {
		memberPath := path.Clean(strings.ReplaceAll(strings.TrimSpace(member.Path), "\\", "/"))
		if memberPath == dir || strings.EqualFold(path.Base(memberPath), sk.Name) || (member.ID != "" && strings.EqualFold(member.ID, sk.ID)) {
			return true
		}
	}
	return false
}

func applyManifestIdentity(manifest *suiteManifest, skillDir string, sk *HubSkillFull) {
	if manifest == nil || sk == nil {
		return
	}
	dir := path.Clean(strings.ReplaceAll(strings.TrimSpace(skillDir), "\\", "/"))
	for _, member := range manifest.entries() {
		memberPath := path.Clean(strings.ReplaceAll(strings.TrimSpace(member.Path), "\\", "/"))
		if member.ID != "" && (memberPath == dir || strings.EqualFold(path.Base(memberPath), sk.Name)) {
			sk.ID = strings.TrimSpace(member.ID)
			sk.SkillID = strings.TrimSpace(member.ID)
			return
		}
	}
}

func highestSuiteVersion(skills []HubSkillFull) string {
	best := ""
	for _, sk := range skills {
		if strings.TrimSpace(sk.Version) == "" {
			continue
		}
		if best == "" || compareSuiteVersion(sk.Version, best) > 0 {
			best = sk.Version
		}
	}
	return best
}

func compareSuiteVersion(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexAny(v, "+-"); i >= 0 {
			v = v[:i]
		}
		p := strings.Split(v, ".")
		out := make([]int, 3)
		for i := 0; i < len(p) && i < 3; i++ {
			n := 0
			for _, r := range p[i] {
				if r < '0' || r > '9' {
					return nil
				}
				n = n*10 + int(r-'0')
			}
			out[i] = n
		}
		if len(p) != 3 {
			return nil
		}
		return out
	}
	pa, pb := parse(a), parse(b)
	if pa != nil && pb != nil {
		for i := 0; i < 3; i++ {
			if pa[i] > pb[i] {
				return 1
			}
			if pa[i] < pb[i] {
				return -1
			}
		}
	}
	if a > b {
		return 1
	}
	if a < b {
		return -1
	}
	return 0
}

// collectFiles 抓取同目录下的附属文件（scripts 等），递归包含子目录。
func (ri *RemoteImporter) collectFiles(sk *HubSkillFull, tree *ghTreeResponse, skillDir, owner, repo, branch string) {
	sk.Files = make(map[string]string)
	dirPrefix := skillDir + "/"
	if skillDir == "." {
		dirPrefix = ""
	}
	skipFiles := map[string]bool{"skill.yaml": true}
	var totalBytes int64
	for _, f := range tree.Tree {
		if f.Type != "blob" {
			continue
		}
		var relPath string
		if dirPrefix == "" {
			relPath = f.Path
		} else {
			if !strings.HasPrefix(f.Path, dirPrefix) {
				continue
			}
			relPath = strings.TrimPrefix(f.Path, dirPrefix)
		}
		if relPath == "" || skipFiles[relPath] {
			continue
		}
		// 跳过根级别的 skill.md（定义文件本身），但保留子目录中的同名文件
		if relPath == "skill.md" {
			continue
		}
		cleanRel := path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
		if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, "../") || strings.HasPrefix(cleanRel, "/") {
			continue
		}
		fileURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, f.Path)
		fileData, err := ri.httpGet(fileURL)
		if err != nil {
			continue
		}
		if len(fileData) <= 512*1024 && totalBytes+int64(len(fileData)) <= 10*1024*1024 {
			sk.Files[cleanRel] = base64.StdEncoding.EncodeToString(fileData)
			totalBytes += int64(len(fileData))
		}
	}
}

// parseSkillMD 解析 skill.md（带 YAML front-matter 的 Markdown）为标准 HubSkillFull。
func (ri *RemoteImporter) parseSkillMD(data []byte, sourceURL, skillDir string) (*HubSkillFull, error) {
	content := string(data)

	// 提取 YAML front-matter（--- 包裹的部分）
	frontMatter, body, err := extractFrontMatter(content)
	if err != nil {
		return nil, err
	}

	var fm skillMDFrontMatter
	if err := yaml.Unmarshal([]byte(frontMatter), &fm); err != nil {
		return nil, fmt.Errorf("invalid YAML front-matter: %w", err)
	}

	name := fm.Name
	if name == "" {
		if skillDir != "" && skillDir != "." {
			name = path.Base(skillDir)
		} else {
			name = "imported-skill"
		}
	}

	version := fm.Metadata.Version
	if version == "" {
		version = "1"
	}

	author := fm.Metadata.Author
	description := fm.Description

	now := time.Now().Format(time.RFC3339)

	full := &HubSkillFull{
		HubSkillMeta: HubSkillMeta{
			ID:          generateImportID(),
			Name:        name,
			Description: description,
			Tags:        fm.Tags,
			Version:     version,
			Author:      author,
			TrustLevel:  "community",
			CreatedAt:   now,
			UpdatedAt:   now,
			Visible:     true,
			Price:       0,
			Status:      "published",
			Platforms:   fm.Platforms,
			Permissions: fm.Permissions,
			SourceURL:   sourceURL,
		},
		AgentSkillMD: strings.TrimSpace(body),
	}
	return full, nil
}

// extractFrontMatter 从 Markdown 内容中提取 YAML front-matter 和正文。
// 支持标准格式 (---\ncontent\n---) 和连续分隔符格式 (---\n---\ncontent\n---)。
func extractFrontMatter(content string) (frontMatter, body string, err error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return "", "", fmt.Errorf("no YAML front-matter found (missing opening ---)")
	}

	// 跳过第一个 ---
	rest := content[3:]
	if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
		rest = rest[idx+1:]
	} else {
		return "", "", fmt.Errorf("no YAML front-matter found (missing closing ---)")
	}

	// 如果紧接着又是 ---（连续分隔符格式），跳过它作为真正的开头
	trimmed := strings.TrimLeft(rest, " \t")
	if strings.HasPrefix(trimmed, "---") {
		after := trimmed[3:]
		if after == "" || after[0] == '\n' || after[0] == '\r' {
			rest = after
			if len(rest) > 0 && (rest[0] == '\n' || rest[0] == '\r') {
				if rest[0] == '\r' && len(rest) > 1 && rest[1] == '\n' {
					rest = rest[2:]
				} else {
					rest = rest[1:]
				}
			}
		}
	}

	endIdx := strings.Index(rest, "\n---")
	if endIdx < 0 {
		return "", "", fmt.Errorf("no YAML front-matter found (missing closing ---)")
	}
	frontMatter = rest[:endIdx]
	body = rest[endIdx+4:] // skip "\n---"
	return frontMatter, body, nil
}

// importFromRawURL 直接抓取一个 URL 作为 skill.yaml 解析。
func (ri *RemoteImporter) importFromRawURL(rawURL string) (*ImportResult, error) {
	data, err := ri.httpGet(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	sk, err := ri.parseSkillYAML(data, rawURL, "")
	if err != nil {
		return nil, fmt.Errorf("parse skill.yaml: %w", err)
	}
	return &ImportResult{Skills: []HubSkillFull{*sk}}, nil
}

// parseSkillYAML 解析 skill.yaml 内容为 HubSkillFull。
func (ri *RemoteImporter) parseSkillYAML(data []byte, sourceURL, skillDir string) (*HubSkillFull, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("empty YAML document")
	}

	name := strVal(raw, "name")
	if name == "" {
		if skillDir != "" && skillDir != "." {
			name = path.Base(skillDir)
		} else {
			name = "imported-skill"
		}
	}

	version := strVal(raw, "version")
	if version == "" {
		version = "1"
	}

	now := time.Now().Format(time.RFC3339)

	full := &HubSkillFull{
		HubSkillMeta: HubSkillMeta{
			ID:          generateImportID(),
			Name:        name,
			Description: strVal(raw, "description"),
			Tags:        strSlice(raw, "tags"),
			Version:     version,
			Author:      strVal(raw, "author"),
			TrustLevel:  "community",
			CreatedAt:   now,
			UpdatedAt:   now,
			Visible:     true,
			Price:       0,
			Status:      "published",
			Platforms:   strSlice(raw, "platforms"),
			Permissions: strSlice(raw, "permissions"),
			SourceURL:   sourceURL,
		},
		Triggers: strSlice(raw, "triggers"),
		Steps:    parseSteps(raw),
	}
	return full, nil
}

// ── YAML 解析辅助函数 ──────────────────────────────────────────────────

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

func parseSteps(m map[string]interface{}) []HubSkillStep {
	raw, ok := m["steps"]
	if !ok {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var steps []HubSkillStep
	for _, item := range list {
		sm, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		step := HubSkillStep{
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

// ── HTTP 和 ID 生成 ────────────────────────────────────────────────────

// generateSkillYAML 为 skill.md 格式的 skill 生成一个最小的 skill.yaml，
// 使客户端 scanner 能够识别该 skill 目录。
func (ri *RemoteImporter) generateSkillYAML(sk *HubSkillFull) []byte {
	m := map[string]interface{}{
		"name":        sk.Name,
		"description": sk.Description,
	}
	if len(sk.HubSkillMeta.Tags) > 0 {
		m["tags"] = sk.HubSkillMeta.Tags
	}
	if len(sk.Triggers) > 0 {
		m["triggers"] = sk.Triggers
	}
	if sk.Version != "" {
		m["version"] = sk.Version
	}
	if len(sk.HubSkillMeta.Platforms) > 0 {
		m["platforms"] = sk.HubSkillMeta.Platforms
	}
	if len(sk.HubSkillMeta.Permissions) > 0 {
		m["permissions"] = sk.HubSkillMeta.Permissions
	}
	if len(sk.HubSkillMeta.RequiredEnv) > 0 {
		m["required_env"] = sk.HubSkillMeta.RequiredEnv
	}
	if sk.HubSkillMeta.RequiresGUI {
		m["requires_gui"] = true
	}
	data, _ := yaml.Marshal(m)
	return data
}

func (ri *RemoteImporter) httpGet(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw-SkillImporter/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := ri.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return readLimitedRemoteImportBodyWithLength(resp.Body, resp.ContentLength, remoteImportMaxBytes)
}

func readLimitedRemoteImportBody(body io.Reader, limit int64) ([]byte, error) {
	return coreskill.ReadLimitedHTTPBody(body, -1, limit)
}

func readLimitedRemoteImportBodyWithLength(body io.Reader, contentLength, limit int64) ([]byte, error) {
	return coreskill.ReadLimitedHTTPBody(body, contentLength, limit)
}

func generateImportID() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("imp-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(buf[:]))
}

// GenerateImportID is the exported version of generateImportID for use by other packages.
func GenerateImportID() string {
	return generateImportID()
}
