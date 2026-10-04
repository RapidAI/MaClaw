package guiapp

import (
	"bytes"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

const (
	cloudWorkspaceSearchDefaultLimit         = 8
	cloudWorkspaceSearchMaxLimit             = 20
	cloudWorkspaceSearchMaxWorkspaces        = 64
	cloudWorkspaceSearchMaxEntries           = 8000
	cloudWorkspaceSearchMaxFileBytes         = 256 * 1024
	cloudWorkspaceSearchExtensionlessMaxByte = 64 * 1024
	cloudWorkspaceSearchMaxContentFiles      = 200
	cloudWorkspaceSearchMaxContentBytes      = 4 * 1024 * 1024
	cloudWorkspaceSearchSnippetBefore        = 36
	cloudWorkspaceSearchSnippetAfter         = 48
)

// CloudWorkspaceContentHit is one file inside a cloud workspace cache whose
// name or already-downloaded text matches the header search.
type CloudWorkspaceContentHit struct {
	ID            string   `json:"id"`
	WorkspaceID   string   `json:"workspace_id"`
	WorkspaceName string   `json:"workspace_name"`
	ProjectPath   string   `json:"project_path"`
	RelativePath  string   `json:"relative_path"`
	Title         string   `json:"title"`
	Preview       string   `json:"preview"`
	Match         string   `json:"match"`
	Tags          []string `json:"tags,omitempty"`
}

type cloudWorkspaceSearchRoot struct {
	ID          string
	Name        string
	ProjectPath string
	CacheRoot   string
	Tags        []string
}

type cloudWorkspaceScoredHit struct {
	hit   CloudWorkspaceContentHit
	score int
}

// SearchCloudWorkspaceContent searches file names in local cloud-workspace
// listings, and the text of files already materialized in those caches.
// It does not call Hub, download missing objects, or read a sealed cache.
func (a *App) SearchCloudWorkspaceContent(query string, limit int) []CloudWorkspaceContentHit {
	if a == nil {
		return nil
	}
	seq := a.headerCloudSearchSeq.Add(1)
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	return searchCloudWorkspaceContent(a.cloudWorkspaceContentSearchRoots(), query, limit, func() bool {
		return a.headerCloudSearchSeq.Load() == seq
	})
}

func headerSearchStillCurrent(still func() bool) bool {
	return still == nil || still()
}

func searchCloudWorkspaceContent(roots []cloudWorkspaceSearchRoot, query string, limit int, still func() bool) []CloudWorkspaceContentHit {
	query = strings.TrimSpace(query)
	if query == "" || len(roots) == 0 {
		return nil
	}
	limit = cloudWorkspaceContentSearchLimit(limit)
	queryLower := strings.ToLower(query)
	type listedFile struct {
		root cloudWorkspaceSearchRoot
		rel  string
	}
	files := make([]listedFile, 0, 64)
	entriesLeft := cloudWorkspaceSearchMaxEntries
	for _, root := range roots {
		if entriesLeft <= 0 {
			break
		}
		listing, err := readCloudWorkspaceListing(root.CacheRoot)
		if err != nil || listing == nil {
			continue
		}
		for _, entry := range listing.Entries {
			if entriesLeft <= 0 {
				break
			}
			rel, ok := cloudWorkspaceSearchRel(entry.Path)
			if !ok {
				continue
			}
			entriesLeft--
			files = append(files, listedFile{root: root, rel: rel})
		}
	}
	hits := make([]cloudWorkspaceScoredHit, 0, limit)
	matched := make(map[string]struct{}, len(files))
	for _, file := range files {
		score := cloudWorkspaceNameScore(file.rel, queryLower)
		if score == 0 {
			continue
		}
		key := file.root.ID + "\x00" + file.rel
		matched[key] = struct{}{}
		hits = append(hits, cloudWorkspaceScoredHit{
			hit:   cloudWorkspaceSearchHit(file.root, file.rel, "name", file.rel),
			score: score,
		})
	}
	// Every name score is above the content score, so a full page of name hits
	// cannot change if the bodies are read.
	if len(hits) < limit {
		contentFiles := 0
		contentBytes := 0
		sealed := map[string]bool{}
		for _, file := range files {
			if contentFiles >= cloudWorkspaceSearchMaxContentFiles || contentBytes >= cloudWorkspaceSearchMaxContentBytes {
				break
			}
			key := file.root.ID + "\x00" + file.rel
			if _, ok := matched[key]; ok {
				continue
			}
			if !cloudWorkspaceSearchTextName(path.Base(file.rel)) {
				continue
			}
			if _, seen := sealed[file.root.CacheRoot]; !seen {
				sealed[file.root.CacheRoot] = cloudWorkspaceCacheSealMarkerExists(file.root.CacheRoot)
			}
			if sealed[file.root.CacheRoot] {
				continue
			}
			if !headerSearchStillCurrent(still) {
				return nil
			}
			text, n, ok := cloudWorkspaceSearchFileText(file.root.CacheRoot, file.rel)
			if n > 0 {
				contentFiles++
				contentBytes += n
			}
			if !ok {
				continue
			}
			snippet := cloudWorkspaceSearchSnippet(text, query)
			if snippet == "" {
				continue
			}
			hits = append(hits, cloudWorkspaceScoredHit{
				hit:   cloudWorkspaceSearchHit(file.root, file.rel, "content", snippet),
				score: 40,
			})
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if hits[i].hit.WorkspaceName != hits[j].hit.WorkspaceName {
			return hits[i].hit.WorkspaceName < hits[j].hit.WorkspaceName
		}
		return hits[i].hit.RelativePath < hits[j].hit.RelativePath
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]CloudWorkspaceContentHit, 0, len(hits))
	for _, item := range hits {
		out = append(out, item.hit)
	}
	return out
}

func cloudWorkspaceContentSearchLimit(limit int) int {
	if limit <= 0 {
		return cloudWorkspaceSearchDefaultLimit
	}
	if limit > cloudWorkspaceSearchMaxLimit {
		return cloudWorkspaceSearchMaxLimit
	}
	return limit
}

func cloudWorkspaceNameScore(rel, queryLower string) int {
	baseLower := strings.ToLower(path.Base(rel))
	if baseLower == queryLower {
		return 140
	}
	if strings.Contains(baseLower, queryLower) {
		return 100
	}
	if strings.Contains(strings.ToLower(rel), queryLower) {
		return 60
	}
	return 0
}

func cloudWorkspaceSearchRel(raw string) (string, bool) {
	rel, ok := cloudWorkspaceSafeRelPath(raw)
	if !ok {
		return "", false
	}
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") {
			return "", false
		}
	}
	return rel, true
}

func cloudWorkspaceSearchHit(root cloudWorkspaceSearchRoot, rel, matchKind, detail string) CloudWorkspaceContentHit {
	name := strings.TrimSpace(root.Name)
	if name == "" {
		name = root.ID
	}
	tags := append([]string(nil), root.Tags...)
	return CloudWorkspaceContentHit{
		ID:            root.ID + "/" + rel,
		WorkspaceID:   root.ID,
		WorkspaceName: name,
		ProjectPath:   root.ProjectPath,
		RelativePath:  rel,
		Title:         path.Base(rel),
		Preview:       cloudWorkspaceSearchPreview(name, detail),
		Match:         matchKind,
		Tags:          tags,
	}
}

func cloudWorkspaceSearchPreview(name, detail string) string {
	name = strings.TrimSpace(name)
	detail = strings.TrimSpace(detail)
	if name == "" {
		return detail
	}
	if detail == "" || detail == name {
		return name
	}
	return name + " · " + detail
}

func cloudWorkspaceSearchSnippet(text, query string) string {
	query = strings.TrimSpace(query)
	if query == "" || text == "" {
		return ""
	}
	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(query)
	lowerAt := strings.Index(lowerText, lowerQuery)
	if lowerAt < 0 {
		return ""
	}
	matchStart := cloudWorkspaceSearchByteIndex(text, lowerText, lowerAt)
	matchEnd := cloudWorkspaceSearchMatchEnd(text, matchStart, lowerQuery)
	start := cloudWorkspaceSearchSnippetEdge(text, matchStart, cloudWorkspaceSearchSnippetBefore, false)
	end := cloudWorkspaceSearchSnippetEdge(text, matchEnd, cloudWorkspaceSearchSnippetAfter, true)
	if start > end {
		start = end
	}
	frag := strings.Join(strings.Fields(text[start:end]), " ")
	if start > 0 {
		frag = "…" + frag
	}
	if end < len(text) {
		frag += "…"
	}
	return frag
}

// cloudWorkspaceSearchByteIndex maps a byte offset in strings.ToLower(text)
// back onto text. A fold can shrink one rune and grow another, so an equal
// byte length does not mean the offset still points at the same character.
func cloudWorkspaceSearchByteIndex(text, lowerText string, lowerAt int) int {
	if lowerAt <= 0 {
		return 0
	}
	if lowerAt > len(lowerText) {
		lowerAt = len(lowerText)
	}
	if lowerAt > len(text) {
		lowerAt = len(text)
	}
	// Equal length is not enough: one rune can shrink and another grow. ASCII
	// and text that did not change keep the same byte offsets.
	if cloudWorkspaceSearchASCII(text) || lowerText == text {
		for lowerAt > 0 && !utf8.RuneStart(text[lowerAt]) {
			lowerAt--
		}
		return lowerAt
	}
	orig := 0
	lowerLen := 0
	for orig < len(text) {
		r, size := utf8.DecodeRuneInString(text[orig:])
		if size <= 0 {
			break
		}
		next := lowerLen + len(strings.ToLower(string(r)))
		if next > lowerAt {
			return orig
		}
		orig += size
		lowerLen = next
		if lowerLen == lowerAt {
			return orig
		}
	}
	return orig
}

func cloudWorkspaceSearchASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func cloudWorkspaceSearchMatchEnd(text string, matchStart int, lowerQuery string) int {
	end := matchStart
	got := 0
	need := len(lowerQuery)
	for end < len(text) && got < need {
		r, size := utf8.DecodeRuneInString(text[end:])
		if size <= 0 {
			break
		}
		got += len(strings.ToLower(string(r)))
		end += size
	}
	return end
}

func cloudWorkspaceSearchSnippetEdge(text string, from, runes int, forward bool) int {
	if runes <= 0 {
		return from
	}
	pos := from
	if forward {
		for i := 0; i < runes && pos < len(text); i++ {
			_, size := utf8.DecodeRuneInString(text[pos:])
			if size <= 0 {
				break
			}
			pos += size
		}
		return pos
	}
	for i := 0; i < runes && pos > 0; i++ {
		_, size := utf8.DecodeLastRuneInString(text[:pos])
		if size <= 0 {
			break
		}
		pos -= size
	}
	return pos
}

// cloudWorkspaceSearchFileText reads at most the search cap. A longer text
// file still matches when the query is inside that prefix. Extensionless files
// above the small cap are left unread so a binary blob cannot fill the budget.
// n is the number of bytes actually read. Callers must not charge the content
// budget when n is 0.
func cloudWorkspaceSearchFileText(root, rel string) (string, int, bool) {
	absPath, err := codingWorkbenchBrowserLocalPath(root, rel)
	if err != nil {
		return "", 0, false
	}
	info, err := os.Lstat(absPath)
	if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", 0, false
	}
	if !cloudWorkspaceSearchTextName(info.Name()) {
		return "", 0, false
	}
	if path.Ext(info.Name()) == "" && info.Size() > cloudWorkspaceSearchExtensionlessMaxByte {
		return "", 0, false
	}
	maxBytes := int64(cloudWorkspaceSearchMaxFileBytes)
	file, err := os.Open(absPath)
	if err != nil {
		return "", 0, false
	}
	defer file.Close()
	toRead := maxBytes
	if info.Size() > 0 && info.Size() < toRead {
		toRead = info.Size()
	}
	buf, err := io.ReadAll(io.LimitReader(file, toRead))
	n := len(buf)
	if err != nil || n == 0 {
		return "", n, false
	}
	if !utf8.Valid(buf) && info.Size() > int64(n) {
		for trim := 1; trim < utf8.UTFMax && trim < n; trim++ {
			if utf8.Valid(buf[:n-trim]) {
				buf = buf[:n-trim]
				break
			}
		}
	}
	if !utf8.Valid(buf) || bytes.IndexByte(buf, 0) >= 0 {
		return "", n, false
	}
	return string(buf), n, true
}

func cloudWorkspaceSearchTextName(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if ext == "" {
		return true
	}
	_, ok := cloudWorkspaceSearchTextExt[ext]
	return ok
}

var cloudWorkspaceSearchTextExt = map[string]struct{}{
	"txt": {}, "md": {}, "markdown": {}, "json": {}, "csv": {}, "tsv": {},
	"yaml": {}, "yml": {}, "xml": {}, "html": {}, "htm": {}, "css": {},
	"js": {}, "jsx": {}, "ts": {}, "tsx": {}, "mjs": {}, "cjs": {},
	"go": {}, "py": {}, "rs": {}, "java": {}, "c": {}, "h": {}, "cpp": {}, "hpp": {},
	"cs": {}, "rb": {}, "php": {}, "sql": {}, "sh": {}, "ps1": {}, "bash": {},
	"tex": {}, "latex": {}, "ltx": {}, "log": {}, "ini": {}, "toml": {}, "cfg": {},
	"vue": {}, "rst": {}, "org": {}, "bib": {}, "mod": {}, "sum": {}, "svg": {},
	"env": {}, "gitignore": {}, "editorconfig": {},
}

func (a *App) cloudWorkspaceContentSearchRoots() []cloudWorkspaceSearchRoot {
	if a == nil {
		return nil
	}
	a.ensureMemoryStore()
	hidden := map[string]bool{}
	roots := make([]cloudWorkspaceSearchRoot, 0, 8)
	seen := map[string]struct{}{}
	if a.memoryStore != nil && a.memoryStore.ProjectIndex() != nil {
		pi := a.memoryStore.ProjectIndex()
		records := pi.ListAllMatching(func(rec memory.ProjectRecord) bool {
			return isTaskManagementRecord(rec) && primaryCloudWorkspaceID(rec) != ""
		})
		for i := range records {
			if custom := strings.TrimSpace(pi.CustomName(records[i].ProjectPath)); custom != "" {
				records[i].Name = custom
			}
		}
		visible := make([]memory.ProjectRecord, 0, len(records))
		visibleID := map[string]struct{}{}
		for _, rec := range records {
			id := primaryCloudWorkspaceID(rec)
			if id == "" {
				continue
			}
			if pi.IsHidden(rec.ProjectPath) || pi.IsArchived(rec.ProjectPath) || omittedFromTaskSidebar(rec) {
				hidden[id] = true
				continue
			}
			visibleID[id] = struct{}{}
			visible = append(visible, rec)
		}
		for id := range visibleID {
			delete(hidden, id)
		}
		visible = collapseDuplicateCloudWorkspaceRecords(visible)
		for _, rec := range visible {
			if len(roots) >= cloudWorkspaceSearchMaxWorkspaces {
				break
			}
			id := primaryCloudWorkspaceID(rec)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			cache := a.cloudWorkspaceSearchCacheRoot(rec, id)
			if cache == "" {
				continue
			}
			seen[id] = struct{}{}
			name := strings.TrimSpace(rec.Name)
			roots = append(roots, cloudWorkspaceSearchRoot{
				ID:          id,
				Name:        name,
				ProjectPath: rec.ProjectPath,
				CacheRoot:   cache,
				Tags:        append([]string(nil), rec.Tags...),
			})
		}
	}
	tenantID := a.cloudWorkspaceTenantID()
	for _, found := range cloudWorkspaceSearchRootsFromChildDirs(filepath.Join(a.GetDataDir(), "cloud-workspaces", safeCloudWorkspaceCacheComponent(tenantID, cloudWorkspaceDefaultTenantID, "tenant"))) {
		if len(roots) >= cloudWorkspaceSearchMaxWorkspaces {
			break
		}
		if hidden[found.ID] {
			continue
		}
		if _, ok := seen[found.ID]; ok {
			continue
		}
		seen[found.ID] = struct{}{}
		roots = append(roots, found)
	}
	for _, found := range a.cloudWorkspaceSearchReadonlyCaches(tenantID) {
		if len(roots) >= cloudWorkspaceSearchMaxWorkspaces {
			break
		}
		if hidden[found.ID] {
			continue
		}
		if _, ok := seen[found.ID]; ok {
			continue
		}
		seen[found.ID] = struct{}{}
		roots = append(roots, found)
	}
	return roots
}

func (a *App) cloudWorkspaceSearchCacheRoot(rec memory.ProjectRecord, id string) string {
	candidates := make([]string, 0, 3)
	if wd := recentTaskWorkingDirFromTags(rec.Tags); wd != "" {
		if root := cloudWorkspaceCacheRootFromPath(wd); root != "" {
			candidates = append(candidates, root)
		}
		if root := cloudWorkspaceReadOnlyInstanceDir(wd); root != "" {
			candidates = append(candidates, root)
		}
	}
	if a != nil && id != "" {
		candidates = append(candidates, a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), id))
	}
	for _, candidate := range candidates {
		if cloudWorkspaceListingExists(candidate) {
			return normalizeProjectSessionPath(candidate)
		}
	}
	if a != nil && id != "" {
		if root, ok := a.newestCloudWorkspaceReadonlyRoot(a.cloudWorkspaceTenantID(), id); ok {
			return root
		}
	}
	return ""
}

func cloudWorkspaceListingExists(root string) bool {
	root = normalizeProjectSessionPath(root)
	if root == "" {
		return false
	}
	info, err := os.Lstat(filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceListingFile))
	return err == nil && info.Mode().IsRegular()
}

func (a *App) cloudWorkspaceSearchReadonlyCaches(tenantID string) []cloudWorkspaceSearchRoot {
	parent := filepath.Join(a.GetDataDir(), "cloud-workspaces-readonly", safeCloudWorkspaceCacheComponent(tenantID, cloudWorkspaceDefaultTenantID, "tenant"))
	names := cloudWorkspaceSearchChildDirNames(parent)
	out := make([]cloudWorkspaceSearchRoot, 0, len(names))
	for _, name := range names {
		root, ok := a.newestCloudWorkspaceReadonlyRoot(tenantID, name)
		if !ok {
			continue
		}
		out = append(out, cloudWorkspaceSearchRoot{
			ID:          name,
			Name:        name,
			ProjectPath: root,
			CacheRoot:   root,
		})
	}
	return out
}

func (a *App) newestCloudWorkspaceReadonlyRoot(tenantID, workspaceID string) (string, bool) {
	if !validCloudWorkspaceCacheID(workspaceID) {
		return "", false
	}
	parent := filepath.Join(a.GetDataDir(), "cloud-workspaces-readonly", safeCloudWorkspaceCacheComponent(tenantID, cloudWorkspaceDefaultTenantID, "tenant"), workspaceID)
	var best string
	var bestNano int64
	for _, instance := range cloudWorkspaceSearchChildDirNames(parent) {
		root := filepath.Join(parent, instance)
		info, err := os.Lstat(filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceListingFile))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		nano := info.ModTime().UnixNano()
		if best == "" || nano > bestNano {
			best = root
			bestNano = nano
		}
	}
	if best == "" {
		return "", false
	}
	return normalizeProjectSessionPath(best), true
}

func cloudWorkspaceSearchRootsFromChildDirs(parent string) []cloudWorkspaceSearchRoot {
	names := cloudWorkspaceSearchChildDirNames(parent)
	out := make([]cloudWorkspaceSearchRoot, 0, len(names))
	for _, name := range names {
		root := normalizeProjectSessionPath(filepath.Join(parent, name))
		if !cloudWorkspaceListingExists(root) {
			continue
		}
		out = append(out, cloudWorkspaceSearchRoot{
			ID:          name,
			Name:        name,
			ProjectPath: root,
			CacheRoot:   root,
		})
	}
	return out
}

func cloudWorkspaceSearchChildDirNames(parent string) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !validCloudWorkspaceCacheID(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
