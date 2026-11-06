package skill

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
)

// clawHubDownloadDirPrefix marks a temp directory DownloadClawHub created
// because the caller did not supply TargetDir. persistImportedEntries copies
// it and then deletes it. A caller-owned staging directory does not use it.
const clawHubDownloadDirPrefix = "maclaw-clawhub-dl-"

// ClawHubDownloadOptions controls how a ClawHub skill folder is materialised.
// TargetDir is the caller's empty staging directory. Empty means a temp
// directory the caller copies from entry.SkillDir.
type ClawHubDownloadOptions struct {
	// Source is NLSkillEntry.Source. Empty means "clawhub".
	Source string
	// TargetDir is the extraction directory. It must be empty when the
	// download is a zip. The caller owns cleanup.
	TargetDir string
}

// DownloadClawHub downloads a ClawHub skill folder and returns an entry whose
// SkillDir contains that folder. Bash fences and bundled scripts become runner
// steps. A markdown-only package still keeps its SKILL.md and sibling files
// on disk; the craft_tool fallback is only the runner's representation of an
// instruction document, not a substitute package.
func (c *HubClient) DownloadClawHub(ctx context.Context, slug string) (*corelib.NLSkillEntry, error) {
	return c.DownloadClawHubWithOptions(ctx, slug, ClawHubDownloadOptions{})
}

// DownloadClawHubWithOptions is the staging-aware ClawHub install. Callers
// that own a transaction pass TargetDir so a failed commit never publishes a
// partial folder, and so the scanned directory is the folder that was downloaded.
func (c *HubClient) DownloadClawHubWithOptions(ctx context.Context, slug string, opts ClawHubDownloadOptions) (*corelib.NLSkillEntry, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("clawhub slug is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	detail, err := c.fetchClawHubDetail(ctx, slug)
	if err != nil {
		return nil, err
	}
	dir, ephemeral, err := clawHubMaterializeDir(opts.TargetDir)
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		if ephemeral {
			_ = os.RemoveAll(dir)
		}
	}
	if err := c.materializeClawHubPackage(ctx, slug, detail, dir); err != nil {
		cleanup()
		return nil, err
	}
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = "clawhub"
	}
	name := clawHubInstallName(detail.displayName, firstNonEmptyString(detail.slug, slug))
	entry, err := ImportMarkdownSkillDir(dir, MarkdownSkillOptions{
		NameFallback:        name,
		DescriptionFallback: detail.summary,
		Source:              source,
		SourceProject:       ClawHubMirrorURL,
		TrustLevel:          "community",
		SkillDir:            dir,
		Triggers:            []string{firstNonEmptyString(detail.slug, slug)},
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("import clawhub skill %s: %w", slug, err)
	}
	entry.Name = name
	entry.Description = firstNonEmptyString(entry.Description, detail.summary)
	entry.Triggers = []string{firstNonEmptyString(detail.slug, slug)}
	entry.Source = source
	entry.SourceProject = ClawHubMirrorURL
	entry.TrustLevel = "community"
	entry.Status = "active"
	entry.SkillDir = dir
	if strings.TrimSpace(entry.CreatedAt) == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	return entry, nil
}

type clawHubDetail struct {
	slug        string
	displayName string
	summary     string
	skillMD     string
	files       []string
}

func (c *HubClient) fetchClawHubDetail(ctx context.Context, slug string) (clawHubDetail, error) {
	endpoint := ClawHubMirrorURL + "/api/v1/skills/" + url.PathEscape(slug)
	body, err := c.getBytes(ctx, endpoint, hubClientJSONMaxBytes)
	if err != nil {
		return clawHubDetail{}, fmt.Errorf("下载 ClawHub Skill 失败: %w", err)
	}
	detail, err := parseClawHubDetail(body)
	if err != nil {
		return clawHubDetail{}, fmt.Errorf("下载 ClawHub Skill 失败: %w", err)
	}
	if detail.slug == "" {
		detail.slug = slug
	}
	return detail, nil
}

func (c *HubClient) materializeClawHubPackage(ctx context.Context, slug string, detail clawHubDetail, dir string) error {
	downloadURL := ClawHubMirrorURL + "/api/v1/download?slug=" + url.QueryEscape(slug)
	body, downloadErr := c.getBytes(ctx, downloadURL, MaxSkillPackageDownloadBytes)
	if downloadErr == nil && isZipPayload(body) {
		result := archiveutil.ExtractZIPBytesToDirectory(body, dir, archiveutil.Limits{
			MaxInputBytes: MaxSkillPackageDownloadBytes,
			MaxFiles:      MaxSkillMarketZipEntries,
			MaxFileBytes:  MaxSkillMarketZipSingleFileBytes,
			MaxTotalBytes: MaxSkillMarketZipTotalBytes,
		}, archiveutil.ExtractionPolicy{})
		if !result.OK {
			return fmt.Errorf("extract clawhub skill %s: %s", slug, result.Message)
		}
		if err := hoistClawHubPackageRoot(dir); err != nil {
			return err
		}
	}
	if !clawHubDirHasSkillDoc(dir) {
		if err := c.writeClawHubListedFiles(ctx, slug, detail, dir); err != nil {
			if downloadErr != nil {
				return fmt.Errorf("下载 ClawHub Skill 失败: %w", downloadErr)
			}
			return err
		}
	}
	if !clawHubDirHasSkillDoc(dir) && strings.TrimSpace(detail.skillMD) != "" {
		if err := writeClawHubPackageFile(dir, "SKILL.md", []byte(detail.skillMD)); err != nil {
			return err
		}
	}
	if !clawHubDirHasSkillDoc(dir) {
		if downloadErr != nil {
			return fmt.Errorf("下载 ClawHub Skill 失败: %w", downloadErr)
		}
		return fmt.Errorf("skill %s has no SKILL.md content", slug)
	}
	return nil
}

func (c *HubClient) writeClawHubListedFiles(ctx context.Context, slug string, detail clawHubDetail, dir string) error {
	var total int64
	for _, rel := range clawHubMaterialFilePaths(detail.files) {
		endpoint := ClawHubMirrorURL + "/api/v1/skills/" + url.PathEscape(slug) + "/file?path=" + url.QueryEscape(rel)
		body, err := c.getBytes(ctx, endpoint, MaxSkillPackageDownloadBytes)
		if err != nil {
			return fmt.Errorf("download clawhub file %s: %w", rel, err)
		}
		total += int64(len(body))
		if total > MaxSkillPackageDownloadBytes {
			return fmt.Errorf("clawhub skill %s exceeds %d bytes", slug, MaxSkillPackageDownloadBytes)
		}
		if err := writeClawHubPackageFile(dir, rel, body); err != nil {
			return err
		}
	}
	return nil
}

func clawHubMaterializeDir(target string) (string, bool, error) {
	target = strings.TrimSpace(target)
	if target != "" {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return "", false, err
		}
		return target, false, nil
	}
	dir, err := os.MkdirTemp("", clawHubDownloadDirPrefix)
	if err != nil {
		return "", false, err
	}
	return dir, true, nil
}

func isZipPayload(body []byte) bool {
	return len(body) >= 4 && body[0] == 'P' && body[1] == 'K'
}

func clawHubDirHasSkillDoc(dir string) bool {
	_, err := findSkillMarkdownDocPath(dir)
	return err == nil
}

func hoistClawHubPackageRoot(dir string) error {
	if clawHubDirHasSkillDoc(dir) {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var dirs []string
	for _, entry := range entries {
		name := entry.Name()
		if name == "__MACOSX" || strings.HasPrefix(name, ".") {
			continue
		}
		if !entry.IsDir() {
			return nil
		}
		dirs = append(dirs, name)
	}
	if len(dirs) != 1 {
		return nil
	}
	nested := filepath.Join(dir, dirs[0])
	if !clawHubDirHasSkillDoc(nested) {
		return nil
	}
	kids, err := os.ReadDir(nested)
	if err != nil {
		return err
	}
	for _, kid := range kids {
		from := filepath.Join(nested, kid.Name())
		to := filepath.Join(dir, kid.Name())
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("hoist clawhub package root: %w", err)
		}
	}
	return os.Remove(nested)
}

func writeClawHubPackageFile(root, rel string, data []byte) error {
	normalized := strings.ReplaceAll(rel, "\\", "/")
	clean := filepath.Clean(filepath.FromSlash(normalized))
	if isUnsafeHubBundledPath(normalized, clean) {
		return fmt.Errorf("unsafe clawhub file path %q", rel)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	dest := filepath.Join(rootAbs, clean)
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	relToRoot, err := filepath.Rel(rootAbs, destAbs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return fmt.Errorf("unsafe clawhub file path %q", rel)
	}
	info, err := os.Lstat(rootAbs)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("unsafe skill directory %q", root)
	}
	if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(destAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			return fmt.Errorf("unsafe clawhub file target %q", rel)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(destAbs, data, 0o644)
}

func parseClawHubDetail(data []byte) (clawHubDetail, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return clawHubDetail{}, err
	}
	var skillObj struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Summary     string `json:"summary"`
	}
	if raw, ok := root["skill"]; ok {
		if err := json.Unmarshal(raw, &skillObj); err != nil {
			return clawHubDetail{}, err
		}
	}
	meta := root["metaContent"]
	if meta == nil {
		meta = root["meta"]
	}
	var skillMD string
	var files []string
	if len(meta) > 0 && string(meta) != "null" {
		var metaMap map[string]json.RawMessage
		if err := json.Unmarshal(meta, &metaMap); err != nil {
			return clawHubDetail{}, err
		}
		for _, key := range []string{"skillMd", "skillMD", "skill_md"} {
			raw, ok := metaMap[key]
			if !ok {
				continue
			}
			if err := json.Unmarshal(raw, &skillMD); err != nil {
				return clawHubDetail{}, err
			}
			if strings.TrimSpace(skillMD) != "" {
				break
			}
		}
		for _, key := range []string{"Files", "files"} {
			raw, ok := metaMap[key]
			if !ok {
				continue
			}
			listed, err := clawHubListedFiles(raw)
			if err != nil {
				return clawHubDetail{}, err
			}
			if len(listed) > 0 {
				files = listed
				break
			}
		}
	}
	return clawHubDetail{
		slug:        strings.TrimSpace(skillObj.Slug),
		displayName: strings.TrimSpace(skillObj.DisplayName),
		summary:     strings.TrimSpace(skillObj.Summary),
		skillMD:     skillMD,
		files:       files,
	}, nil
}

// clawHubMaterialFilePaths drops blanks, directory markers, and any path
// that is only a parent of another listed file. Writing the parent as a
// file would make scripts/draw.py fail on a file named scripts.
func clawHubMaterialFilePaths(files []string) []string {
	cleaned := make([]string, 0, len(files))
	for _, rel := range files {
		rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" || strings.HasSuffix(rel, "/") {
			continue
		}
		cleaned = append(cleaned, rel)
	}
	out := make([]string, 0, len(cleaned))
	for _, rel := range cleaned {
		if clawHubPathIsListedParent(rel, cleaned) {
			continue
		}
		out = append(out, rel)
	}
	return out
}

// clawHubPathIsListedParent is true when another listed path is a child.
// The comparison ignores case: on Windows, scripts and Scripts/draw.py are
// the same directory, and writing scripts as a file blocks the child.
func clawHubPathIsListedParent(rel string, files []string) bool {
	prefix := rel + "/"
	for _, other := range files {
		if len(other) <= len(prefix) {
			continue
		}
		if strings.EqualFold(other[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

func clawHubListedFiles(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		return asStrings, nil
	}
	var asObjects []map[string]interface{}
	if err := json.Unmarshal(raw, &asObjects); err == nil {
		out := make([]string, 0, len(asObjects))
		for _, obj := range asObjects {
			if clawHubFileEntryIsDir(obj) {
				continue
			}
			for _, key := range []string{"path", "file", "name", "filename"} {
				text, _ := obj[key].(string)
				text = strings.TrimSpace(text)
				if text != "" {
					out = append(out, text)
					break
				}
			}
		}
		return out, nil
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asMap); err == nil {
		out := make([]string, 0, len(asMap))
		for path := range asMap {
			path = strings.TrimSpace(path)
			if path != "" {
				out = append(out, path)
			}
		}
		sort.Strings(out)
		return out, nil
	}
	return nil, fmt.Errorf("unrecognized clawhub file list")
}

func (c *HubClient) getBytes(ctx context.Context, endpoint string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: 180 * time.Second}
	}
	if maxBytes > MaxSkillHubSearchJSONBytes {
		client = &http.Client{Timeout: 180 * time.Second, Transport: client.Transport}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return ReadLimitedHTTPBody(resp.Body, resp.ContentLength, maxBytes)
}

// PackageFilesBase64 reads a materialised skill folder as relative path →
// base64, the same map SkillHub packages publish. Dot names and the scan
// database are not part of the package.
func PackageFilesBase64(root string) (map[string]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(rootAbs)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("unsafe skill directory %q", root)
	}
	out := map[string]string{}
	var total int64
	err = filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil {
			return walkErr
		}
		name := d.Name()
		if name != "." && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		total += int64(len(data))
		if total > MaxSkillPackageDownloadBytes {
			return fmt.Errorf("skill package exceeds %d bytes", MaxSkillPackageDownloadBytes)
		}
		out[rel] = base64.StdEncoding.EncodeToString(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// RetargetInstalledSkillPaths rewrites step parameters that still name the
// download directory. The published folder is a different path: a staging
// rename, or the user skill root after an ephemeral download is copied.
func RetargetInstalledSkillPaths(entry *corelib.NLSkillEntry, oldDir, newDir string) {
	oldDir = strings.TrimSpace(oldDir)
	newDir = strings.TrimSpace(newDir)
	if entry == nil || oldDir == "" || newDir == "" {
		return
	}
	oldClean := filepath.Clean(oldDir)
	newClean := newDir
	if !strings.Contains(newDir, "{") {
		newClean = filepath.Clean(newDir)
	}
	if oldClean == "." || oldClean == newClean {
		return
	}
	olds := []string{oldClean}
	news := []string{newClean}
	if slash := filepath.ToSlash(oldClean); slash != oldClean {
		olds = append(olds, slash)
		if strings.Contains(newDir, "{") {
			news = append(news, newClean)
		} else {
			news = append(news, filepath.ToSlash(newClean))
		}
	}
	for i := range entry.Steps {
		if entry.Steps[i].Params == nil {
			continue
		}
		for key, value := range entry.Steps[i].Params {
			text, ok := value.(string)
			if !ok || text == "" {
				continue
			}
			entry.Steps[i].Params[key] = replaceSkillDirMentions(text, olds, news)
		}
	}
}

func replaceSkillDirMentions(text string, olds, news []string) string {
	for i, old := range olds {
		if old == "" || !strings.Contains(text, old) {
			continue
		}
		neu := news[0]
		if i < len(news) {
			neu = news[i]
		}
		text = replaceBoundedSkillDir(text, old, neu)
	}
	return text
}

// replaceBoundedSkillDir replaces old only when it is a whole path. A
// directory named stage must not rewrite stage-extra or notstage.
func replaceBoundedSkillDir(text, old, neu string) string {
	if old == "" || old == neu {
		return text
	}
	var b strings.Builder
	for {
		i := strings.Index(text, old)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		end := i + len(old)
		if skillPathBoundary(text, i, end) {
			b.WriteString(text[:i])
			b.WriteString(neu)
		} else {
			b.WriteString(text[:end])
		}
		text = text[end:]
	}
}

func skillPathBoundary(text string, start, end int) bool {
	if start > 0 {
		switch text[start-1] {
		case '/', '\\', '"', '\'', ' ', '\t', '\n', '\r', '=':
		default:
			return false
		}
	}
	if end >= len(text) {
		return true
	}
	switch text[end] {
	case '/', '\\', '"', '\'', ' ', '\t', '\n', '\r', ')':
		return true
	default:
		return false
	}
}

func clawHubFileEntryIsDir(obj map[string]interface{}) bool {
	for _, key := range []string{"type", "kind", "fileType"} {
		text, _ := obj[key].(string)
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "dir", "directory", "tree", "folder":
			return true
		}
	}
	return false
}

// EphemeralSkillDownloadDir reports a temp folder created only to hold a
// download until the caller copies it into the skill root.
func EphemeralSkillDownloadDir(dir string) bool {
	base := filepath.Base(strings.TrimSpace(dir))
	return strings.HasPrefix(base, "maclaw-skillhub-dl-") ||
		strings.HasPrefix(base, "maclaw-skillmarket-dl-") ||
		strings.HasPrefix(base, clawHubDownloadDirPrefix)
}
