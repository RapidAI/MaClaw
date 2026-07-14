package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// maxCodeFileSize is the maximum file size (1 MB) for code event emission.
// Files larger than this are skipped to avoid performance issues.
const maxCodeFileSize = 1 << 20 // 1 MB

// emitCodeFileEvents processes ImportantEvents from a session and emits
// code:file_update events for file.change and file.read events.
// It reads file content from disk and, for file.change events, attempts
// to retrieve the original content via git show.
func (m *RemoteSessionManager) emitCodeFileEvents(s *RemoteSession, events []ImportantEvent) {
	if m.app == nil || m.app.codeEventEmitter == nil {
		return
	}
	emitter := m.app.codeEventEmitter

	for _, evt := range events {
		codeEvent, ok := buildRemoteCodeFileEvent(s, evt)
		if !ok {
			continue
		}
		emitter.EmitCodeFileEvent(codeEvent)
	}
}

func buildRemoteCodeFileEvent(s *RemoteSession, evt ImportantEvent) (CodeFileEvent, bool) {
	if s == nil {
		return CodeFileEvent{}, false
	}
	eventType := normalizeSummaryEventType(evt.Type)
	if !eventType.IsFileEvent() {
		return CodeFileEvent{}, false
	}

	filePath := evt.RelatedFile
	if filePath == "" {
		// Try to extract file path from Summary for SDK events.
		filePath = extractFilePathFromSummary(evt.Summary)
	}
	if filePath == "" {
		return CodeFileEvent{}, false
	}

	absPath := filePath
	if !filepath.IsAbs(filePath) && s.ProjectPath != "" {
		absPath = filepath.Join(s.ProjectPath, filePath)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		log.Printf("[code-event] skip %s: stat error: %v", filePath, err)
		return CodeFileEvent{}, false
	}
	if info.Size() > maxCodeFileSize {
		log.Printf("[code-event] skip %s: file too large (%d bytes)", filePath, info.Size())
		return CodeFileEvent{}, false
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		log.Printf("[code-event] skip %s: read error: %v", filePath, err)
		return CodeFileEvent{}, false
	}
	if !isCodePreviewTextContent(content) {
		log.Printf("[code-event] skip %s: binary or invalid UTF-8 content", filePath)
		return CodeFileEvent{}, false
	}

	fileName := filepath.Base(filePath)
	opType := "read"
	var original string
	if eventType.IsFileChange() {
		opType = "modify"
		// Attempt to read original content via git show.
		original = gitShowOriginal(s.ProjectPath, absPath)
	}

	return CodeFileEvent{
		SessionID:   s.ID,
		FilePath:    filePath,
		FileName:    fileName,
		AbsPath:     absPath,
		Content:     string(content),
		Original:    original,
		OpType:      opType,
		Language:    detectLanguageFromExt(fileName),
		ProjectPath: s.ProjectPath,
	}, true
}

// codePreviewRouteProjectPath returns the frontend tab project path used to
// route code:file_update / code:session_* events.
//
// Pure-coding tasks use a managed task directory as the tab projectPath
// (e.g. …/data/tasks/hello-…), while tools execute under working_dir
// (e.g. D:\testprj6). Events must carry the tab path so useCodePreviewState's
// shouldAcceptCodeEventForProject accepts them; disk reads still use execDir.
func codePreviewRouteProjectPath(ownerUserID, diskProjectPath string) string {
	if tab := projectPathFromSessionOwnerID(ownerUserID); tab != "" {
		return tab
	}
	return strings.TrimSpace(diskProjectPath)
}

// firstNonEmptyRoutePath picks an explicit route override, else falls back to disk.
func firstNonEmptyRoutePath(diskProjectPath string, routeProjectPath ...string) string {
	if len(routeProjectPath) > 0 {
		if s := strings.TrimSpace(routeProjectPath[0]); s != "" {
			return s
		}
	}
	return strings.TrimSpace(diskProjectPath)
}

// shouldStickyMergePreviewScan is true when end-of-turn preview paths came from a
// project scan (no turn audit, no prior sticky). Those paths are not already in
// sticky via recordStickyLocalCodingTurn and must be persisted for re-arm restore.
func shouldStickyMergePreviewScan(turnModified, turnCreated, stickyFiles, emitted []string) bool {
	if !hasNonEmptyPreviewPath(emitted) {
		return false
	}
	// Any turn audit path → recordStickyLocalCodingTurn already owns sticky.
	if hasNonEmptyPreviewPath(turnModified) || hasNonEmptyPreviewPath(turnCreated) {
		return false
	}
	// Sticky already had history — emit reused sticky fill, not a bare scan.
	if hasNonEmptyPreviewPath(stickyFiles) {
		return false
	}
	return true
}

func hasNonEmptyPreviewPath(paths []string) bool {
	for _, p := range paths {
		if strings.TrimSpace(p) != "" {
			return true
		}
	}
	return false
}

// applyCodeEventRouteProjectPath rewrites ProjectPath on built events for tab routing.
// Empty route leaves events unchanged.
func applyCodeEventRouteProjectPath(events []CodeFileEvent, routeProjectPath string) []CodeFileEvent {
	routeProjectPath = strings.TrimSpace(routeProjectPath)
	if routeProjectPath == "" || len(events) == 0 {
		return events
	}
	for i := range events {
		if events[i].ProjectPath == routeProjectPath {
			continue
		}
		events[i].ProjectPath = routeProjectPath
	}
	return events
}

func emitCodingSubAgentCodeFileEvents(app *App, sessionID, projectPath string, filesModified, filesCreated []string, routeProjectPath ...string) {
	if app == nil || app.codeEventEmitter == nil {
		return
	}
	events := buildCodingSubAgentCodeFileEvents(sessionID, projectPath, filesModified, filesCreated)
	route := firstNonEmptyRoutePath(projectPath, routeProjectPath...)
	for _, evt := range applyCodeEventRouteProjectPath(events, route) {
		app.codeEventEmitter.EmitCodeFileEvent(evt)
	}
}

// codingWorkbenchPreviewRestoreSessionID is stable across tab re-arms so the
// frontend merges restore events instead of wiping the panel each open.
func codingWorkbenchPreviewRestoreSessionID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "coding-workbench-restore"
	}
	return "coding-workbench-restore:" + userID
}

const codingWorkbenchPreviewEmitMaxFiles = 40

// emitCodingWorkbenchSourcePreview pushes changed source files into the right-hand
// code preview for pure-coding workbench turns.
//
// Multi-turn note: pure-coding session_start (auto_open) keeps open tabs.
// Sticky history is merged at end-of-turn (and on arm restore). Only when both
// the current turn and sticky are empty — and allowScan is true — do we fall
// back to a shallow project source scan (arm/restore should pass allowScan=false).
//
// forceOpen=false on arm/restore so we never hijack an active session
// (frontend mismatch+forceOpen wipes to a single file).
// forceOpen=true at end-of-turn so the panel opens and shows the full batch.
//
// Sticky / scan hits are emitted as *modified* (not create) so restore does not
// paint every tab as a dirty new file.
//
// diskProjectPath is where files are read (exec / working_dir).
// routeProjectPath is optional and sets CodeFileEvent.ProjectPath for tab routing
// (managed task dir). When empty, diskProjectPath is used for both.
// Returns the relative paths that were successfully emitted (for sticky recovery).
func emitCodingWorkbenchSourcePreview(app *App, sessionID, diskProjectPath string, filesModified, filesCreated, stickyFiles []string, allowScan, forceOpen bool, routeProjectPath ...string) []string {
	if app == nil || app.codeEventEmitter == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "coding-workbench"
	}
	projectPath := diskProjectPath
	// Drop missing paths early (turn audit can lag deletes / failed writes).
	turnModified := filterExistingProjectRelPaths(projectPath, uniqueSortedSubAgentStrings(filesModified))
	turnCreated := filterExistingProjectRelPaths(projectPath, uniqueSortedSubAgentStrings(filesCreated))
	sticky := filterExistingProjectRelPaths(projectPath, uniqueSortedSubAgentStrings(stickyFiles))

	// Prefer current-turn paths, then fill from sticky (session_start wipe recovery).
	modified := mergePreviewPathsPreferFirst(turnModified, sticky, codingWorkbenchPreviewEmitMaxFiles)

	// Created stays turn-only; drop paths already covered as modified; cap total emits.
	created := make([]string, 0, len(turnCreated))
	modSet := make(map[string]bool, len(modified))
	for _, p := range modified {
		modSet[p] = true
	}
	for _, p := range turnCreated {
		if modSet[p] {
			continue
		}
		created = append(created, p)
		if len(modified)+len(created) >= codingWorkbenchPreviewEmitMaxFiles {
			break
		}
	}

	if len(modified) == 0 && len(created) == 0 {
		if !allowScan {
			return nil
		}
		// Last resort: shallow project scan (end-of-turn / recovery only).
		modified = listCodingWorkbenchPreviewSources(projectPath, 24)
	}
	if len(modified) == 0 && len(created) == 0 {
		return nil
	}

	inputs := make([]subAgentCodeEventInput, 0, len(modified)+len(created))
	for _, p := range modified {
		inputs = append(inputs, subAgentCodeEventInput{path: p, created: false, forceOpen: forceOpen})
	}
	for _, p := range created {
		inputs = append(inputs, subAgentCodeEventInput{path: p, created: true, forceOpen: forceOpen})
	}
	events := buildCodingSubAgentCodeFileEventsForPaths(sessionID, projectPath, inputs)
	route := firstNonEmptyRoutePath(projectPath, routeProjectPath...)
	events = applyCodeEventRouteProjectPath(events, route)
	if len(events) == 0 {
		return nil
	}
	// One summary line instead of N per-file logs when batching restore/end-of-turn.
	if len(events) > 1 {
		log.Printf("[code-event] emit file_update batch session=%q disk=%q route=%q count=%d force_open=%v",
			sessionID, projectPath, route, len(events), forceOpen)
	}
	emitted := make([]string, 0, len(events))
	for _, evt := range events {
		// Suppress per-file chatter for multi-file batches; single-file keeps detail log.
		if len(events) == 1 {
			app.codeEventEmitter.EmitCodeFileEvent(evt)
		} else {
			app.codeEventEmitter.emitCodeFileEventQuiet(evt)
		}
		if p := strings.TrimSpace(evt.FilePath); p != "" {
			emitted = append(emitted, p)
		}
	}
	return emitted
}

// mergePreviewPathsPreferFirst keeps all primary paths, then appends secondary
// until maxFiles. Order of primary is preserved; secondary fills the remainder.
func mergePreviewPathsPreferFirst(primary, secondary []string, maxFiles int) []string {
	if maxFiles <= 0 {
		return nil
	}
	seen := make(map[string]bool, len(primary)+len(secondary))
	out := make([]string, 0, maxFiles)
	for _, p := range primary {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxFiles {
			return out
		}
	}
	for _, p := range secondary {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxFiles {
			return out
		}
	}
	return out
}

// filterExistingProjectRelPaths drops sticky paths that no longer exist on disk.
// Absolute paths under projectPath are rewritten to project-relative form so the
// preview panel does not open duplicate tabs for the same file.
func filterExistingProjectRelPaths(projectPath string, relPaths []string) []string {
	projectPath = strings.TrimSpace(projectPath)
	if len(relPaths) == 0 {
		return nil
	}
	out := make([]string, 0, len(relPaths))
	seen := make(map[string]bool, len(relPaths))
	for _, rel := range relPaths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		full := rel
		if projectPath != "" && !filepath.IsAbs(rel) {
			full = filepath.Join(projectPath, filepath.FromSlash(rel))
		} else if projectPath != "" && filepath.IsAbs(rel) {
			full = filepath.Clean(rel)
		}
		fi, err := os.Stat(full)
		if err != nil || fi.IsDir() || fi.Size() > maxCodeFileSize {
			continue
		}
		display := filepath.ToSlash(rel)
		if projectPath != "" && filepath.IsAbs(full) {
			if r, err := filepath.Rel(projectPath, full); err == nil && r != "." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && r != ".." {
				display = filepath.ToSlash(r)
			}
		}
		if seen[display] {
			continue
		}
		seen[display] = true
		out = append(out, display)
	}
	return out
}

// listCodingWorkbenchPreviewSources returns shallow text source files under
// projectPath for preview bootstrap (skips binaries, build artifacts, VCS).
// Code-like extensions are preferred over config/docs so a 24-file cap is useful.
func listCodingWorkbenchPreviewSources(projectPath string, limit int) []string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" || limit <= 0 {
		return nil
	}
	info, err := os.Stat(projectPath)
	if err != nil || !info.IsDir() {
		return nil
	}
	skipDir := map[string]bool{
		".git": true, ".svn": true, ".hg": true,
		"node_modules": true, "vendor": true, "dist": true, "build": true,
		"target": true, "out": true, "bin": true, "obj": true,
		".maclaw": true, ".idea": true, ".vscode": true, "__pycache__": true,
		".next": true, ".turbo": true, "coverage": true, "tmp": true, "temp": true,
	}
	type hit struct {
		rel   string
		rank  int
		mtime int64
	}
	var hits []hit
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		// Collect more than limit so we can rank; hard cap walk work.
		if len(hits) >= limit*3 || depth > 3 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, ent := range entries {
			if len(hits) >= limit*3 {
				return
			}
			name := ent.Name()
			if name == "" || (strings.HasPrefix(name, ".") && name != ".gitignore" && name != ".env.example") {
				continue
			}
			full := filepath.Join(dir, name)
			if ent.IsDir() {
				if skipDir[strings.ToLower(name)] {
					continue
				}
				walk(full, depth+1)
				continue
			}
			rank := codingWorkbenchPreviewSourceRank(name)
			if rank <= 0 {
				continue
			}
			fi, err := ent.Info()
			if err != nil || fi.IsDir() || fi.Size() <= 0 || fi.Size() > maxCodeFileSize {
				continue
			}
			rel := name
			if r, err := filepath.Rel(projectPath, full); err == nil {
				rel = r
			}
			hits = append(hits, hit{
				rel:   filepath.ToSlash(rel),
				rank:  rank,
				mtime: fi.ModTime().UnixNano(),
			})
		}
	}
	walk(projectPath, 0)
	// Higher rank first, then newer mtime.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank > hits[j].rank
		}
		return hits[i].mtime > hits[j].mtime
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.rel)
	}
	return out
}

// codingWorkbenchPreviewSourceRank ranks files for bootstrap preview.
// 0 = skip; higher = more likely implementation source.
func codingWorkbenchPreviewSourceRank(name string) int {
	base := strings.ToLower(filepath.Base(name))
	switch base {
	case "makefile", "gnumakefile", "cmakelists.txt", "dockerfile", "containerfile",
		"go.mod", "cargo.toml", "package.json":
		return 2
	case "readme", "readme.md", "license", "go.sum":
		return 1
	}
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hh",
		".go", ".rs", ".java", ".kt", ".kts", ".cs",
		".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".py", ".swift", ".m", ".mm":
		return 5
	case ".vue", ".svelte", ".astro", ".rb", ".php", ".lua":
		return 4
	case ".sql", ".proto", ".graphql", ".cmake":
		return 3
	case ".sh", ".bash", ".ps1", ".bat", ".cmd",
		".html", ".css", ".scss", ".json", ".jsonc",
		".yaml", ".yml", ".toml", ".xml", ".md", ".mdx", ".txt":
		return 2
	default:
		return 0
	}
}

func emitCodingSubAgentCodeSessionStart(app *App, sessionID string, projectPath ...string) {
	if app == nil || app.codeEventEmitter == nil {
		return
	}
	// Keep the right-hand source panel open while clearing files for the new turn.
	// Without auto_open, pure-coding tabs flicker closed on every session_start.
	app.codeEventEmitter.EmitSessionStartAutoOpen(sessionID, projectPath...)
}

func emitCodingSubAgentCodeSessionEnd(app *App, sessionID string, projectPath ...string) {
	if app == nil || app.codeEventEmitter == nil {
		return
	}
	app.codeEventEmitter.EmitSessionEnd(sessionID, projectPath...)
}

// emitCodeFilePreviewForPath reads a file under diskProjectPath (or an absolute
// path) and emits code:file_update. routeProjectPath controls frontend tab
// routing (see codePreviewRouteProjectPath); empty uses diskProjectPath.
// originalOverride, when present as a single string, supplies the pre-edit content.
func emitCodeFilePreviewForPath(app *App, sessionID, diskProjectPath, routeProjectPath, filePath string, created, forceOpen bool, originalOverride ...string) {
	if app == nil || app.codeEventEmitter == nil {
		return
	}
	input := subAgentCodeEventInput{
		path:      filePath,
		created:   created,
		forceOpen: forceOpen,
	}
	if len(originalOverride) > 0 {
		input.original = &originalOverride[0]
	}
	events := buildCodingSubAgentCodeFileEventsForPaths(sessionID, diskProjectPath, []subAgentCodeEventInput{input})
	route := firstNonEmptyRoutePath(diskProjectPath, routeProjectPath)
	for _, evt := range applyCodeEventRouteProjectPath(events, route) {
		app.codeEventEmitter.EmitCodeFileEvent(evt)
	}
}

func codingSubAgentCodeSessionID(scope, userID string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "subagent"
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return scope
	}
	return scope + ":" + userID
}

var codingSubAgentCodeSessionSeq atomic.Uint64

func newCodingSubAgentCodeSessionID(scope, userID string) string {
	seq := codingSubAgentCodeSessionSeq.Add(1)
	return fmt.Sprintf("%s:%d:%d", codingSubAgentCodeSessionID(scope, userID), time.Now().UnixNano(), seq)
}

func buildCodingSubAgentCodeFileEvents(sessionID, projectPath string, filesModified, filesCreated []string) []CodeFileEvent {
	inputs := make([]subAgentCodeEventInput, 0, len(filesModified)+len(filesCreated))
	created := make(map[string]bool, len(filesCreated))
	for _, filePath := range filesCreated {
		if normalized := normalizeSubAgentCodeEventPath(filePath, projectPath); normalized.displayPath != "" {
			created[normalized.displayPath] = true
		}
	}
	for _, filePath := range filesModified {
		normalized := normalizeSubAgentCodeEventPath(filePath, projectPath)
		inputs = append(inputs, subAgentCodeEventInput{
			path:      filePath,
			created:   created[normalized.displayPath],
			forceOpen: true,
		})
	}
	for _, filePath := range filesCreated {
		inputs = append(inputs, subAgentCodeEventInput{
			path:      filePath,
			created:   true,
			forceOpen: true,
		})
	}
	return buildCodingSubAgentCodeFileEventsForPaths(sessionID, projectPath, inputs)
}

type subAgentCodeEventInput struct {
	path      string
	created   bool
	forceOpen bool
	original  *string
}

func buildCodingSubAgentCodeFileEventsForPaths(sessionID, projectPath string, inputFiles []subAgentCodeEventInput) []CodeFileEvent {
	seen := make(map[string]bool, len(inputFiles))
	events := make([]CodeFileEvent, 0, len(inputFiles))
	for _, input := range inputFiles {
		filePath := input.path
		normalized := normalizeSubAgentCodeEventPath(filePath, projectPath)
		if normalized.displayPath == "" || normalized.absPath == "" || seen[normalized.displayPath] {
			continue
		}
		seen[normalized.displayPath] = true
		if !isSubAgentCodeEventPathInProject(normalized.absPath, projectPath) {
			log.Printf("[code-event] skip subagent file %s: outside project path", normalized.displayPath)
			continue
		}

		info, err := os.Stat(normalized.absPath)
		if err != nil || info.IsDir() {
			if err != nil {
				log.Printf("[code-event] skip subagent file %s: stat error: %v", normalized.displayPath, err)
			}
			continue
		}
		if info.Size() > maxCodeFileSize {
			log.Printf("[code-event] skip subagent file %s: file too large (%d bytes)", normalized.displayPath, info.Size())
			continue
		}

		content, err := os.ReadFile(normalized.absPath)
		if err != nil {
			log.Printf("[code-event] skip subagent file %s: read error: %v", normalized.displayPath, err)
			continue
		}
		if !isCodePreviewTextContent(content) {
			log.Printf("[code-event] skip subagent file %s: binary or invalid UTF-8 content", normalized.displayPath)
			continue
		}

		opType := "modify"
		var original string
		if input.created {
			opType = "create"
		} else if input.original != nil {
			original = *input.original
		} else {
			original = gitShowOriginal(projectPath, normalized.absPath)
		}

		fileName := filepath.Base(normalized.displayPath)
		events = append(events, CodeFileEvent{
			SessionID:   sessionID,
			FilePath:    normalized.displayPath,
			FileName:    fileName,
			AbsPath:     normalized.absPath,
			Content:     string(content),
			Original:    original,
			OpType:      opType,
			Language:    detectLanguageFromExt(fileName),
			ForceOpen:   input.forceOpen,
			ProjectPath: projectPath,
		})
	}
	return events
}

type subAgentCodeEventPath struct {
	displayPath string
	absPath     string
}

func normalizeSubAgentCodeEventPath(filePath, projectPath string) subAgentCodeEventPath {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return subAgentCodeEventPath{}
	}

	absPath := filePath
	if !filepath.IsAbs(absPath) && strings.TrimSpace(projectPath) != "" {
		absPath = filepath.Join(projectPath, filePath)
	}
	absPath = filepath.Clean(absPath)

	displayPath := filepath.Clean(filePath)
	if strings.TrimSpace(projectPath) != "" && filepath.IsAbs(absPath) {
		if rel, err := filepath.Rel(projectPath, absPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			displayPath = rel
		}
	}
	return subAgentCodeEventPath{
		displayPath: filepath.ToSlash(displayPath),
		absPath:     absPath,
	}
}

func isSubAgentCodeEventPathInProject(absPath, projectPath string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return true
	}
	ok, err := isPathWithinDir(absPath, projectPath)
	return err == nil && ok
}

func isCodePreviewTextContent(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	if !utf8.Valid(content) {
		return false
	}
	for _, b := range content {
		if b == 0 {
			return false
		}
	}
	return true
}

// gitShowTimeout is the maximum time allowed for a git show command.
// Prevents blocking the event processing goroutine on slow repos.
const gitShowTimeout = 5 * time.Second

// gitShowOriginal attempts to read the original file content from git HEAD.
// Returns empty string if git is unavailable, the file is new, or the
// command times out.
func gitShowOriginal(projectPath, absPath string) string {
	if projectPath == "" {
		return ""
	}

	relPath, err := filepath.Rel(projectPath, absPath)
	if err != nil {
		return ""
	}
	// Normalize to forward slashes for git
	relPath = filepath.ToSlash(relPath)

	ctx, cancel := context.WithTimeout(context.Background(), gitShowTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "show", fmt.Sprintf("HEAD:%s", relPath))
	cmd.Dir = projectPath
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		// git show fails for new files, non-git repos, or timeout — expected
		return ""
	}
	return string(out)
}

// extractFilePathFromSummary extracts a file path from an event summary string.
// SDK events store the full path in Summary like "Inspected /path/to/file" or
// "Modified /path/to/file".
func extractFilePathFromSummary(summary string) string {
	prefixes := []string{"Inspected ", "Modified ", "Read ", "Edited ", "Created ", "Wrote "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(summary, prefix) {
			path := strings.TrimSpace(strings.TrimPrefix(summary, prefix))
			if path != "" {
				return path
			}
		}
	}
	return ""
}
