package guiapp

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	systemCleanupCatToolPackages   = "tool_packages"
	systemCleanupCatAppTemp        = "app_temp"
	systemCleanupCatSkillTemp      = "skill_temp"
	systemCleanupCatSkillStaging   = "skill_staging"
	systemCleanupCatDocPreview     = "doc_preview"
	systemCleanupCatClipboardPaste = "clipboard_paste"
	systemCleanupCatPreviewBrowser = "preview_browser"
	systemCleanupCatSpilledScripts = "spilled_scripts"
	systemCleanupCatCodingVerify   = "coding_verify"
	systemCleanupCatIndex          = "rebuildable_index"
	systemCleanupCatIntentShadow   = "intent_shadow"
	systemCleanupCatDemoShots      = "demo_shots"
	systemCleanupCatLLMPartials    = "llm_partials"
	systemCleanupCatDesktopStaging = "desktop_staging"
	systemCleanupCatWebview        = "webview_cache"
	systemCleanupCatAudit          = "audit_temp"
	systemCleanupCatVRepoAuth      = "vrepo_auth"

	systemCleanupAutoGrace            = 15 * time.Minute
	systemCleanupSizeCooldownHours    = 1
	systemCleanupStatusCategoryBudget = 40 * time.Millisecond
	systemCleanupAutoCategoryBudget   = 150 * time.Millisecond
	systemCleanupPassTimeout          = 2 * time.Minute
	systemCleanupSchedulerDelay       = 45 * time.Second
	systemCleanupSchedulerInterval    = time.Hour
	systemCleanupProgressEvent        = "system-cleanup-progress"
	appTempPendingUploadName          = "pending-bug-report-upload.json"
	appTempInputsName                 = "app-inputs"
	appTempMigrationName              = "user-data-migration"
	skillTmpGitignoreName             = ".gitignore"
	llmRespTmpPrefix                  = ".llm_resp_"
	llmRespTmpSuffix                  = ".tmp"
	systemCleanupMaxReportedErrors    = 5
)

// systemCleanupCategoryIDs is the stable settings order. An empty disabled
// list means every id is selected.
func systemCleanupCategoryIDs() []string {
	return []string{
		systemCleanupCatToolPackages,
		systemCleanupCatAppTemp,
		systemCleanupCatSkillTemp,
		systemCleanupCatSkillStaging,
		systemCleanupCatDocPreview,
		systemCleanupCatClipboardPaste,
		systemCleanupCatPreviewBrowser,
		systemCleanupCatSpilledScripts,
		systemCleanupCatCodingVerify,
		systemCleanupCatIndex,
		systemCleanupCatIntentShadow,
		systemCleanupCatDemoShots,
		systemCleanupCatLLMPartials,
		systemCleanupCatDesktopStaging,
		systemCleanupCatWebview,
		systemCleanupCatAudit,
		systemCleanupCatVRepoAuth,
	}
}

func systemCleanupCategorySet() map[string]struct{} {
	ids := systemCleanupCategoryIDs()
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func filterSystemCleanupCategories(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	known := systemCleanupCategorySet()
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if _, ok := known[id]; !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type SystemCleanupCategoryStatus struct {
	ID              string `json:"id"`
	SizeBytes       int64  `json:"size_bytes"`
	SizeApproximate bool   `json:"size_approximate,omitempty"`
	Selected        bool   `json:"selected"`
}

type SystemCleanupStatus struct {
	AutoEnabled      bool                          `json:"auto_enabled"`
	IntervalDays     int                           `json:"interval_days"`
	MaxBytes         int64                         `json:"max_bytes"`
	LastCleanupAt    string                        `json:"last_cleanup_at,omitempty"`
	TotalBytes       int64                         `json:"total_bytes"`
	TotalApproximate bool                          `json:"total_approximate,omitempty"`
	Categories       []SystemCleanupCategoryStatus `json:"categories"`
}

type SystemCleanupResult struct {
	FreedBytes    int64    `json:"freed_bytes"`
	RemovedCount  int      `json:"removed_count"`
	SkippedRecent int      `json:"skipped_recent,omitempty"`
	SkippedBusy   int      `json:"skipped_busy,omitempty"`
	Skipped       bool     `json:"skipped"`
	Reason        string   `json:"reason,omitempty"`
	LastCleanupAt string   `json:"last_cleanup_at,omitempty"`
	Incomplete    bool     `json:"incomplete,omitempty"`
	Errors        []string `json:"errors,omitempty"`
}

type SystemCleanupProgress struct {
	Phase      string `json:"phase"`
	Percent    int    `json:"percent"`
	CategoryID string `json:"category_id,omitempty"`
	FreedBytes int64  `json:"freed_bytes"`
	Index      int    `json:"index"`
	Total      int    `json:"total"`
}

type systemCleanupRoots struct {
	base            string
	appTemp         string
	toolCache       string
	dataDir         string
	skillTmp        string
	osTemp          string
	promptCache     string
	webviewUserData string
	searchIndexes   []string
	toolEmbeddings  string
	intentAnchors   []string
}

type cleanupTarget struct {
	category string
	path     string
}

type osTempRule struct {
	category string
	exact    string
	prefix   string
}

var (
	// maclaw-vscode is absent on purpose. It stores local-only snapshots of
	// remote workbench files, and edits there are not uploaded. The workbench
	// owner already drops snapshots past its own retention and refuses to
	// remove one that VS Code still has open.
	osTempExact = map[string]string{
		"maclaw-latex-preview": systemCleanupCatDocPreview,
		"maclaw-latex-verify":  systemCleanupCatDocPreview,
		"maclaw-paste":         systemCleanupCatClipboardPaste,
		"maclaw-verify":        systemCleanupCatCodingVerify,
		"maclaw-intent-shadow": systemCleanupCatIntentShadow,
		"maclaw-demo":          systemCleanupCatDemoShots,
		"maclaw-audit":         systemCleanupCatAudit,
		"maclaw-skill-runs":    systemCleanupCatSkillStaging,
	}
	osTempPrefixes = []osTempRule{
		{category: systemCleanupCatSkillStaging, prefix: "maclaw-skill-scan-"},
		{category: systemCleanupCatSkillStaging, prefix: "maclaw-skill-install-"},
		{category: systemCleanupCatSkillStaging, prefix: "skill-import-"},
		{category: systemCleanupCatSkillStaging, prefix: "skill-package-"},
		{category: systemCleanupCatSkillStaging, prefix: "maclaw-crafted-skill-scan-"},
		{category: systemCleanupCatSkillStaging, prefix: "maclaw-crafted-skill-prescan-"},
		{category: systemCleanupCatSkillStaging, prefix: "maclawsrv-skill-import-"},
		{category: systemCleanupCatSkillStaging, prefix: "maclaw-skillhub-dl-"},
		{category: systemCleanupCatSkillStaging, prefix: "maclaw-skillmarket-dl-"},
		{category: systemCleanupCatSkillStaging, prefix: "maclaw_skill_sandbox_"},
		{category: systemCleanupCatDocPreview, prefix: "maclaw-attach-pptx-"},
		{category: systemCleanupCatDocPreview, prefix: "maclaw-pptx-preview-"},
		{category: systemCleanupCatDocPreview, prefix: "maclaw-chat-pdf-"},
		{category: systemCleanupCatPreviewBrowser, prefix: "maclaw-preview-chrome-"},
		{category: systemCleanupCatSpilledScripts, prefix: "maclaw_script_"},
		{category: systemCleanupCatSpilledScripts, prefix: "maclaw_shell_"},
		{category: systemCleanupCatDesktopStaging, prefix: "maclaw-desktop-supervisor-"},
		{category: systemCleanupCatDesktopStaging, prefix: "maclaw-desktop-copy-"},
		{category: systemCleanupCatVRepoAuth, prefix: "maclaw-vrepo-auth-"},
	}
)

func matchOSTempCategory(name string) string {
	if category := osTempExact[name]; category != "" {
		return category
	}
	for _, rule := range osTempPrefixes {
		if strings.HasPrefix(name, rule.prefix) {
			return rule.category
		}
	}
	return ""
}

var systemCleanupMu sync.Mutex

func policyPromptCache(cfg corelib.AppConfig) string {
	return cfg.LLMPromptCache.WithDefaults().CacheDir
}

func (a *App) systemCleanupRoots() systemCleanupRoots {
	prompt := ""
	if a != nil && a.testHomeDir == "" {
		if cfg, err := a.LoadConfig(); err == nil {
			prompt = policyPromptCache(cfg)
		}
	}
	return a.systemCleanupRootsWithPrompt(prompt)
}

func (a *App) systemCleanupRootsWithPrompt(prompt string) systemCleanupRoots {
	base := a.getMaclawBaseDir()
	roots := systemCleanupRoots{
		base:           base,
		appTemp:        filepath.Join(base, "temp"),
		toolCache:      filepath.Join(base, "data", "cache"),
		dataDir:        filepath.Join(base, "data"),
		toolEmbeddings: filepath.Join(base, "cache", "tool_embeddings.gob"),
	}
	// Tests must not resolve the real temp dir, workspace, WebView profile,
	// home search index, or prompt cache. The prompt argument is ignored here.
	if a != nil && a.testHomeDir != "" {
		roots.osTemp = filepath.Join(a.testHomeDir, "os-temp")
		roots.skillTmp = filepath.Join(a.testHomeDir, "work", ".maclaw-tmp")
		roots.promptCache = filepath.Join(base, "llm_prompt_cache")
		roots.webviewUserData = filepath.Join(a.testHomeDir, "webview")
		roots.searchIndexes = []string{filepath.Join(base, "cache", "search-index")}
		roots.intentAnchors = []string{filepath.Join(base, "cache", "intent-anchors")}
		return roots
	}
	roots.osTemp = os.TempDir()
	roots.skillTmp = filepath.Join(corelib.EffectiveWorkspaceDir(), ".maclaw-tmp")
	roots.webviewUserData = defaultWebviewUserDataPath()
	roots.searchIndexes = uniqueCleanupPaths([]string{
		filepath.Join(base, "cache", "search-index"),
		defaultSearchIndexPath(),
	})
	roots.intentAnchors = uniqueCleanupPaths([]string{
		filepath.Join(base, "cache", "intent-anchors"),
		defaultIntentAnchorPath(),
	})
	roots.promptCache = prompt
	return roots
}

func defaultSearchIndexPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".maclaw", "cache", "search-index")
}

func defaultIntentAnchorPath() string {
	cache, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cache) == "" {
		return ""
	}
	return filepath.Join(cache, "maclaw", "intent-anchors")
}

func (a *App) GetSystemCleanupStatus() (SystemCleanupStatus, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return SystemCleanupStatus{}, err
	}
	policy := cfg.SystemCleanup.WithDefaults()
	disabled := disabledCategorySet(policy.DisabledCategories)
	roots := a.systemCleanupRootsWithPrompt(policyPromptCache(cfg))
	grouped := listSystemCleanupTargets(roots)
	status := SystemCleanupStatus{
		AutoEnabled:   policy.Enabled,
		IntervalDays:  policy.IntervalDays,
		MaxBytes:      policy.MaxBytes,
		LastCleanupAt: policy.LastCleanupAt,
		Categories:    make([]SystemCleanupCategoryStatus, 0, len(systemCleanupCategoryIDs())),
	}
	for _, id := range systemCleanupCategoryIDs() {
		size, known := measureCleanupTargets(grouped[id], time.Now().Add(systemCleanupStatusCategoryBudget), 0)
		_, off := disabled[id]
		item := SystemCleanupCategoryStatus{
			ID:              id,
			SizeBytes:       size,
			SizeApproximate: !known,
			Selected:        !off,
		}
		status.Categories = append(status.Categories, item)
		if item.Selected {
			status.TotalBytes += size
			if !known {
				status.TotalApproximate = true
			}
		}
	}
	return status, nil
}

func (a *App) CleanSystemCleanupNow() (SystemCleanupResult, error) {
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	ctx, cancel := context.WithTimeout(parent, systemCleanupPassTimeout)
	defer cancel()
	return a.cleanSystem(ctx, true)
}

func (a *App) cleanSystem(ctx context.Context, manual bool) (SystemCleanupResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := SystemCleanupResult{}
	if !systemCleanupMu.TryLock() {
		result.Skipped = true
		result.Reason = "busy"
		return result, nil
	}
	defer systemCleanupMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil {
		return result, err
	}
	policy := cfg.SystemCleanup.WithDefaults()
	if !manual && !policy.Enabled {
		result.Skipped = true
		result.Reason = "disabled"
		return result, nil
	}
	roots := a.systemCleanupRootsWithPrompt(policyPromptCache(cfg))
	grouped := listSystemCleanupTargets(roots)
	disabled := disabledCategorySet(policy.DisabledCategories)
	selected := selectedTargets(grouped, disabled)
	grace := time.Duration(0)
	if !manual {
		should, reason := systemCleanupAutoDue(policy, grouped, disabled)
		if !should {
			result.Skipped = true
			result.Reason = reason
			if reason == "baseline" {
				if stamp, err := a.stampSystemCleanup(); err != nil {
					log.Printf("[system-cleanup] baseline timestamp save failed: %v", err)
				} else {
					result.LastCleanupAt = stamp
				}
			}
			return result, nil
		}
		result.Reason = reason
		grace = systemCleanupAutoGrace
	}

	a.emitSystemCleanupProgress(SystemCleanupProgress{Phase: "clean", Percent: 1, Total: len(selected)})
	now := time.Now()
	var removed int
	var skippedRecent int
	var skippedBusy int
	var unfinished bool
	var errorsOut []string
	lastPercent := 0
	for i, target := range selected {
		if err := ctx.Err(); err != nil {
			errorsOut = appendCleanupError(errorsOut, err)
			unfinished = true
			break
		}
		info, statErr := os.Lstat(target.path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			skippedBusy++
			errorsOut = appendCleanupError(errorsOut, statErr)
			continue
		}
		freed, outcome, skipped, stepErr := a.deleteCleanupTarget(ctx, info, target, i, len(selected), result.FreedBytes, now, grace)
		result.FreedBytes += freed
		skippedRecent += skipped
		if stepErr != nil {
			errorsOut = appendCleanupError(errorsOut, stepErr)
		}
		switch outcome {
		case cleanupDeleteBusy:
			skippedBusy++
		case cleanupDeleteUnfinished:
			unfinished = true
		case cleanupDeleteRemoved:
			removed++
			percent := cleanupPercent(i+1, len(selected), 0)
			if percent != lastPercent {
				lastPercent = percent
				a.emitSystemCleanupProgress(SystemCleanupProgress{
					Phase:      "clean",
					Percent:    percent,
					CategoryID: target.category,
					FreedBytes: result.FreedBytes,
					Index:      i + 1,
					Total:      len(selected),
				})
			}
		}
		if unfinished && ctx.Err() != nil {
			break
		}
	}
	result.RemovedCount = removed
	result.SkippedRecent = skippedRecent
	result.SkippedBusy = skippedBusy
	result.Incomplete = unfinished
	result.Errors = errorsOut
	// A cancelled or unfinished pass is partial: leave the timestamp alone so
	// the next tick retries. An automatic pass that only skipped fresh files
	// also leaves the timestamp alone.
	if ctx.Err() == nil && !unfinished && (manual || removed > 0 || skippedRecent == 0) {
		if stamp, err := a.stampSystemCleanup(); err != nil {
			log.Printf("[system-cleanup] timestamp save failed: %v", err)
		} else {
			result.LastCleanupAt = stamp
		}
	}
	a.emitSystemCleanupProgress(SystemCleanupProgress{Phase: "done", Percent: 100, FreedBytes: result.FreedBytes, Total: len(selected)})
	return result, nil
}

func cleanupModRecent(mod, now time.Time, grace time.Duration) bool {
	if grace <= 0 || mod.IsZero() {
		return false
	}
	age := now.Sub(mod)
	return age < grace
}

type cleanupDeleteOutcome int

const (
	cleanupDeleteRemoved cleanupDeleteOutcome = iota
	cleanupDeleteRecent
	cleanupDeleteBusy
	cleanupDeleteUnfinished
)

// deleteCleanupTarget removes one selected entry. Files and symlinks are
// removed directly. A directory is deleted as it is walked, so a pass that
// runs out of time still keeps the bytes already freed. Automatic cleanup
// skips entries written inside the grace window and still deletes the older
// files beside them. A directory stays only while it still holds something
// that was kept. Links that resolve outside the directory are unlinked
// themselves and are not followed.
func (a *App) deleteCleanupTarget(ctx context.Context, info os.FileInfo, target cleanupTarget, index, total int, freed int64, now time.Time, grace time.Duration) (int64, cleanupDeleteOutcome, int, error) {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		if grace > 0 && cleanupModRecent(info.ModTime(), now, grace) {
			return 0, cleanupDeleteRecent, 1, nil
		}
		if err := removeCleanupTarget(target); err != nil {
			return 0, cleanupDeleteBusy, 0, err
		}
		return info.Size(), cleanupDeleteRemoved, 0, nil
	}
	if err := cleanupDirAllowed(target.path); err != nil {
		return 0, cleanupDeleteBusy, 0, err
	}
	walk := &cleanupDeleteWalk{
		app:      a,
		root:     target.path,
		category: target.category,
		index:    index,
		total:    total,
		base:     freed,
		now:      now,
		grace:    grace,
		seen:     map[string]struct{}{},
	}
	cleanupMarkSeen(walk.seen, target.path)
	kept, err := walk.delete(ctx, target.path)
	if ctx.Err() != nil {
		return walk.freed, cleanupDeleteUnfinished, walk.skippedRecent, ctx.Err()
	}
	if err != nil {
		return walk.freed, cleanupDeleteBusy, walk.skippedRecent, err
	}
	if kept {
		return walk.freed, cleanupDeleteRecent, walk.skippedRecent, nil
	}
	if err := removeCleanupPath(target.path); err != nil {
		return walk.freed, cleanupDeleteBusy, walk.skippedRecent, err
	}
	return walk.freed, cleanupDeleteRemoved, walk.skippedRecent, nil
}

type cleanupDeleteWalk struct {
	app           *App
	root          string
	category      string
	index         int
	total         int
	base          int64
	freed         int64
	files         int
	lastEmit      time.Time
	lastPercent   int
	now           time.Time
	grace         time.Duration
	skippedRecent int
	seen          map[string]struct{}
}

// delete removes dir's children. kept is true when something inside was left
// on purpose or could not be removed, so the caller must not remove dir.
func (w *cleanupDeleteWalk) delete(ctx context.Context, dir string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return true, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		// An unreadable directory may still hold a file written moments ago.
		if w.grace > 0 {
			w.skippedRecent++
			return true, nil
		}
		return true, err
	}
	var firstErr error
	remember := func(err error) {
		if err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	kept := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if w.grace > 0 {
				w.skippedRecent++
				kept = true
				continue
			}
			remember(err)
			kept = true
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			if w.grace > 0 && cleanupModRecent(info.ModTime(), w.now, w.grace) {
				w.skippedRecent++
				kept = true
				continue
			}
			if err := removeCleanupPath(path); err != nil {
				remember(err)
				kept = true
				continue
			}
			w.freed += info.Size()
			w.pulse()
			continue
		}
		resolved, inside := cleanupResolveInside(w.root, path)
		if !inside || cleanupAlreadySeen(w.seen, path, resolved) {
			// The real directory was already walked through another name.
			// Remove it when that walk left it empty. A file kept inside was
			// already counted; this remove failing is not a new busy file.
			if inside && sameCleanupPath(path, resolved) {
				if err := removeCleanupPath(path); err != nil {
					kept = true
				}
				continue
			}
			// An outside directory, or an extra link back to one already
			// being deleted. Remove this entry only.
			if w.grace > 0 && cleanupModRecent(info.ModTime(), w.now, w.grace) {
				w.skippedRecent++
				kept = true
				continue
			}
			if err := removeCleanupPath(path); err != nil {
				remember(err)
				kept = true
			}
			continue
		}
		cleanupMarkSeen(w.seen, path)
		childKept, err := w.delete(ctx, path)
		if ctx.Err() != nil {
			return true, err
		}
		if err != nil {
			remember(err)
			kept = true
		}
		if childKept {
			kept = true
			continue
		}
		if err := removeCleanupPath(path); err != nil {
			remember(err)
			kept = true
		}
	}
	return kept, firstErr
}

func (w *cleanupDeleteWalk) pulse() {
	w.files++
	if time.Since(w.lastEmit) < 150*time.Millisecond && w.lastEmit != (time.Time{}) {
		return
	}
	frac := 1 - 1/(1+float64(w.files)/32)
	percent := cleanupPercent(w.index, w.total, frac)
	if percent == w.lastPercent && w.lastEmit != (time.Time{}) {
		return
	}
	w.lastEmit = time.Now()
	w.lastPercent = percent
	w.app.emitSystemCleanupProgress(SystemCleanupProgress{
		Phase:      "clean",
		Percent:    percent,
		CategoryID: w.category,
		FreedBytes: w.base + w.freed,
		Index:      w.index + 1,
		Total:      w.total,
	})
}

func cleanupDirAllowed(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if !sameCleanupPath(path, resolved) && !pathWithinDir(path, resolved) {
		return os.ErrInvalid
	}
	return nil
}

func cleanupResolveInside(root, path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	if sameCleanupPath(path, resolved) || pathWithinDir(root, resolved) {
		return resolved, true
	}
	return "", false
}

func cleanupSeenKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return strings.ToLower(filepath.Clean(abs))
}

func cleanupMarkSeen(seen map[string]struct{}, path string) {
	if seen == nil {
		return
	}
	seen[cleanupSeenKey(path)] = struct{}{}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		seen[cleanupSeenKey(resolved)] = struct{}{}
	}
}

func cleanupAlreadySeen(seen map[string]struct{}, path, resolved string) bool {
	if len(seen) == 0 {
		return false
	}
	if _, ok := seen[cleanupSeenKey(path)]; ok {
		return true
	}
	if resolved == "" {
		return false
	}
	_, ok := seen[cleanupSeenKey(resolved)]
	return ok
}

// cleanupPercent maps finished targets plus an in-target fraction onto 1..99.
// The done event is the only 100.
func cleanupPercent(done, total int, frac float64) int {
	if total <= 0 {
		return 1
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	raw := 1 + int((float64(done)+frac)*99/float64(total))
	if raw < 1 {
		return 1
	}
	if raw > 99 {
		return 99
	}
	return raw
}

func (a *App) stampSystemCleanup() (string, error) {
	stamp := time.Now().Format(time.RFC3339)
	err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SystemCleanup = cfg.SystemCleanup.WithDefaults()
		cfg.SystemCleanup.LastCleanupAt = stamp
	})
	if err != nil {
		return "", err
	}
	return stamp, nil
}

// systemCleanupAutoDue decides whether a scheduled pass should delete files.
// A missing timestamp only starts the weekly clock. The size trigger can still
// run on that first look, so an already-large temp set is not left for a week.
// Each category has its own measure budget. One shared budget let a slow
// directory consume the whole window, and a later directory already over the
// threshold was never seen.
func systemCleanupAutoDue(policy corelib.SystemCleanupConfig, grouped map[string][]cleanupTarget, disabled map[string]struct{}) (bool, string) {
	policy = policy.WithDefaults()
	hasHistory := strings.TrimSpace(policy.LastCleanupAt) != ""
	if hasHistory && cleanupIntervalElapsed(policy.LastCleanupAt, policy.IntervalDays*24) {
		return true, "interval"
	}
	if hasHistory && !cleanupIntervalElapsed(policy.LastCleanupAt, systemCleanupSizeCooldownHours) {
		return false, "not_due"
	}
	var size int64
	for _, id := range systemCleanupCategoryIDs() {
		if _, off := disabled[id]; off {
			continue
		}
		remaining := policy.MaxBytes - size
		part, _ := measureCleanupTargets(grouped[id], time.Now().Add(systemCleanupAutoCategoryBudget), remaining)
		size += part
		// measureCleanupTargets reports a size at or above the stop point only
		// after it has actually reached that point, including when the budget
		// ends on the same file.
		if size >= policy.MaxBytes {
			return true, "size"
		}
	}
	if !hasHistory {
		return false, "baseline"
	}
	return false, "not_due"
}

func (a *App) maybeStartSystemCleanup() {
	if a == nil || a.ctx == nil || a.isToolCacheMaintenanceSuppressedForTest() {
		return
	}
	if !a.systemCleanupLoopStarted.CompareAndSwap(false, true) {
		return
	}
	go a.systemCleanupLoop()
}

func (a *App) systemCleanupLoop() {
	timer := time.NewTimer(systemCleanupSchedulerDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-a.ctx.Done():
		return
	}
	a.runSystemCleanupAuto("startup")
	ticker := time.NewTicker(systemCleanupSchedulerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.runSystemCleanupAuto("timer")
		}
	}
}

func (a *App) runSystemCleanupAuto(reason string) {
	ctx, cancel := context.WithTimeout(a.ctx, systemCleanupPassTimeout)
	defer cancel()
	result, err := a.cleanSystem(ctx, false)
	if err != nil {
		log.Printf("[system-cleanup] %s cleanup failed: %v", reason, err)
		return
	}
	if result.Skipped {
		log.Printf("[system-cleanup] %s cleanup skipped: %s", reason, result.Reason)
		return
	}
	log.Printf("[system-cleanup] %s cleanup removed=%d freed=%d recent=%d busy=%d reason=%s",
		reason, result.RemovedCount, result.FreedBytes, result.SkippedRecent, result.SkippedBusy, result.Reason)
}

func (a *App) emitSystemCleanupProgress(progress SystemCleanupProgress) {
	if a == nil {
		return
	}
	a.emitEvent(systemCleanupProgressEvent, progress)
}

func disabledCategorySet(ids []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range filterSystemCleanupCategories(ids) {
		out[id] = struct{}{}
	}
	return out
}

func selectedTargets(grouped map[string][]cleanupTarget, disabled map[string]struct{}) []cleanupTarget {
	var out []cleanupTarget
	for _, id := range systemCleanupCategoryIDs() {
		if _, off := disabled[id]; off {
			continue
		}
		out = append(out, grouped[id]...)
	}
	return out
}

func listSystemCleanupTargets(roots systemCleanupRoots) map[string][]cleanupTarget {
	grouped := map[string][]cleanupTarget{}
	add := func(targets []cleanupTarget) {
		for _, target := range targets {
			if target.path == "" || target.category == "" {
				continue
			}
			grouped[target.category] = append(grouped[target.category], target)
		}
	}
	add(listChildTargets(roots.appTempRoot(), systemCleanupCatAppTemp, appTempSkipNames(roots.appTempRoot())))
	add(listChildTargets(roots.toolCacheRoot(), systemCleanupCatToolPackages, nil))
	add(listChildTargets(roots.skillTmpRoot(), systemCleanupCatSkillTemp, map[string]struct{}{skillTmpGitignoreName: {}}))
	add(listOSTempTargets(roots.osTempRoot()))
	add(listLLMPartialTargets(roots.promptCacheRoot()))
	add(listWebviewTargets(roots.webviewUserData))
	add(listIndexTargets(roots))
	return grouped
}

func listChildTargets(root, category string, keep map[string]struct{}) []cleanupTarget {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []cleanupTarget
	for _, entry := range entries {
		name := entry.Name()
		if cleanupNameKept(keep, name) {
			continue
		}
		path := filepath.Join(root, name)
		if !cleanupChildPath(root, path) {
			continue
		}
		out = append(out, cleanupTarget{category: category, path: path})
	}
	return out
}

func listOSTempTargets(root string) []cleanupTarget {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []cleanupTarget
	for _, entry := range entries {
		category := matchOSTempCategory(entry.Name())
		if category == "" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !cleanupChildPath(root, path) {
			continue
		}
		out = append(out, cleanupTarget{category: category, path: path})
	}
	return out
}

func listLLMPartialTargets(root string) []cleanupTarget {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []cleanupTarget
	for _, entry := range entries {
		if entry.IsDir() || !isLLMRespTmp(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !cleanupChildPath(root, path) {
			continue
		}
		out = append(out, cleanupTarget{category: systemCleanupCatLLMPartials, path: path})
	}
	return out
}

func listWebviewTargets(userData string) []cleanupTarget {
	userData = filepath.Clean(strings.TrimSpace(userData))
	if userData == "" || unsafeCleanupRoot(userData) {
		return nil
	}
	var out []cleanupTarget
	for _, rel := range webviewAssetCacheDirs() {
		path := filepath.Join(userData, rel)
		if !pathWithinDir(userData, path) || sameCleanupPath(path, userData) {
			continue
		}
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		out = append(out, cleanupTarget{category: systemCleanupCatWebview, path: path})
	}
	return out
}

func listIndexTargets(roots systemCleanupRoots) []cleanupTarget {
	var out []cleanupTarget
	for _, path := range roots.searchIndexes {
		if !allowedSearchIndex(path, roots) {
			continue
		}
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		out = append(out, cleanupTarget{category: systemCleanupCatIndex, path: path})
	}
	if allowedToolEmbeddings(roots.toolEmbeddings, roots.base) {
		if _, err := os.Lstat(roots.toolEmbeddings); err == nil {
			out = append(out, cleanupTarget{category: systemCleanupCatIndex, path: roots.toolEmbeddings})
		}
	}
	for _, path := range roots.intentAnchors {
		if !allowedIntentAnchors(path, roots) {
			continue
		}
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		out = append(out, cleanupTarget{category: systemCleanupCatIndex, path: path})
	}
	return out
}

func (r systemCleanupRoots) appTempRoot() string {
	path := filepath.Clean(r.appTemp)
	if r.base == "" || unsafeCleanupRoot(r.base) || unsafeCleanupRoot(path) || filepath.Base(path) != "temp" {
		return ""
	}
	if !sameCleanupPath(filepath.Dir(path), r.base) {
		return ""
	}
	return path
}

func (r systemCleanupRoots) toolCacheRoot() string {
	if r.toolCache == "" || r.dataDir == "" || unsafeCleanupRoot(r.dataDir) || unsafeCleanupRoot(r.toolCache) {
		return ""
	}
	if err := ensureToolCachePath(r.toolCache, r.dataDir); err != nil {
		return ""
	}
	return filepath.Clean(r.toolCache)
}

func (r systemCleanupRoots) skillTmpRoot() string {
	path := filepath.Clean(r.skillTmp)
	if filepath.Base(path) != ".maclaw-tmp" || unsafeCleanupRoot(path) {
		return ""
	}
	return path
}

func (r systemCleanupRoots) osTempRoot() string {
	path := filepath.Clean(r.osTemp)
	if path == "" || unsafeCleanupRoot(path) {
		return ""
	}
	return path
}

func (r systemCleanupRoots) promptCacheRoot() string {
	path := filepath.Clean(strings.TrimSpace(r.promptCache))
	if path == "" || unsafeCleanupRoot(path) {
		return ""
	}
	if filepath.Base(path) == "llm_prompt_cache" {
		return path
	}
	if r.base != "" && pathWithinDir(r.base, path) && !sameCleanupPath(r.base, path) {
		return path
	}
	return ""
}

func allowedSearchIndex(path string, roots systemCleanupRoots) bool {
	path = filepath.Clean(path)
	if filepath.Base(path) != "search-index" || filepath.Base(filepath.Dir(path)) != "cache" || unsafeCleanupRoot(path) {
		return false
	}
	parent := filepath.Dir(filepath.Dir(path))
	if roots.base != "" && sameCleanupPath(parent, roots.base) {
		return true
	}
	home := defaultSearchIndexPath()
	return home != "" && sameCleanupPath(path, home)
}

func allowedToolEmbeddings(path, base string) bool {
	path = filepath.Clean(path)
	if filepath.Base(path) != "tool_embeddings.gob" || filepath.Base(filepath.Dir(path)) != "cache" {
		return false
	}
	return base != "" && sameCleanupPath(path, filepath.Join(base, "cache", "tool_embeddings.gob"))
}

func allowedIntentAnchors(path string, roots systemCleanupRoots) bool {
	path = filepath.Clean(path)
	if filepath.Base(path) != "intent-anchors" || unsafeCleanupRoot(path) {
		return false
	}
	if roots.base != "" && sameCleanupPath(path, filepath.Join(roots.base, "cache", "intent-anchors")) {
		return true
	}
	fallback := defaultIntentAnchorPath()
	return fallback != "" && sameCleanupPath(path, fallback)
}

func cleanupChildPath(root, path string) bool {
	if root == "" || unsafeCleanupRoot(root) || unsafeCleanupRoot(path) {
		return false
	}
	if sameCleanupPath(root, path) || !pathWithinDir(root, path) {
		return false
	}
	parent := filepath.Dir(filepath.Clean(path))
	return sameCleanupPath(parent, root)
}

// appTempSkipNames are children of the app temp directory that system cleanup
// does not own. Staged app uploads stay until the app's own 24-hour cleanup.
// A migration working directory holds a copy of user data until that job
// finishes. The pending bug-report record and the diagnostics archive it names
// stay so a failed upload can be retried.
func appTempSkipNames(root string) map[string]struct{} {
	skip := map[string]struct{}{
		appTempPendingUploadName: {},
		appTempInputsName:        {},
		appTempMigrationName:     {},
	}
	if name := pendingBugReportArchiveName(root); name != "" {
		skip[name] = struct{}{}
	}
	return skip
}

func pendingBugReportArchiveName(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, appTempPendingUploadName))
	if err != nil {
		return ""
	}
	var saved pendingBugReportUploadMetadata
	if json.Unmarshal(data, &saved) != nil {
		return ""
	}
	clean := filepath.Clean(strings.TrimSpace(saved.ZipPath))
	name := filepath.Base(clean)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return ""
	}
	if !sameCleanupPath(filepath.Dir(clean), root) {
		return ""
	}
	if !strings.EqualFold(filepath.Ext(name), ".zip") || !strings.HasPrefix(strings.ToLower(name), "maclaw-diagnostics-") {
		return ""
	}
	return name
}

func cleanupNameKept(keep map[string]struct{}, name string) bool {
	if len(keep) == 0 || name == "" {
		return false
	}
	if _, ok := keep[name]; ok {
		return true
	}
	for kept := range keep {
		if strings.EqualFold(kept, name) {
			return true
		}
	}
	return false
}

func isLLMRespTmp(name string) bool {
	return strings.HasPrefix(name, llmRespTmpPrefix) && strings.HasSuffix(name, llmRespTmpSuffix)
}

func unsafeCleanupRoot(path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || path == string(filepath.Separator) {
		return true
	}
	volume := filepath.VolumeName(path)
	if volume == "" {
		return false
	}
	rest := strings.TrimPrefix(path, volume)
	rest = strings.Trim(rest, string(filepath.Separator))
	return rest == ""
}

func sameCleanupPath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func uniqueCleanupPaths(paths []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(path))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

// measureCleanupTargets sums file bytes. A positive stopAt returns as soon as
// the total reaches it and reports the size as known. A deadline in the future
// stops the walk early; the size is then known only when it already reached
// stopAt.
func measureCleanupTargets(targets []cleanupTarget, deadline time.Time, stopAt int64) (int64, bool) {
	var total int64
	known := true
	for _, target := range targets {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return total, stopAt > 0 && total >= stopAt
		}
		size, ok := measureCleanupPath(target.path, deadline, stopAt-total)
		total += size
		if !ok {
			known = stopAt > 0 && total >= stopAt
			return total, known
		}
		if stopAt > 0 && total >= stopAt {
			return total, true
		}
	}
	return total, known
}

func measureCleanupPath(path string, deadline time.Time, stopAt int64) (int64, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, true
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return info.Size(), true
	}
	// A directory junction is not reported as a symlink. Counting through one
	// would treat files outside this directory as temporary data. A junction
	// that points back at a directory already counted would never finish.
	seen := map[string]struct{}{}
	cleanupMarkSeen(seen, path)
	return measureCleanupDir(path, path, deadline, stopAt, seen)
}

func measureCleanupDir(root, dir string, deadline time.Time, stopAt int64, seen map[string]struct{}) (int64, bool) {
	if cleanupMeasureStopped(deadline) {
		return 0, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, true
	}
	var total int64
	for _, entry := range entries {
		if cleanupMeasureStopped(deadline) {
			return total, stopAt > 0 && total >= stopAt
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !info.IsDir() {
			total += info.Size()
			if stopAt > 0 && total >= stopAt {
				return total, true
			}
			continue
		}
		resolved, inside := cleanupResolveInside(root, path)
		if !inside || cleanupAlreadySeen(seen, path, resolved) {
			continue
		}
		cleanupMarkSeen(seen, path)
		remain := int64(0)
		if stopAt > 0 {
			remain = stopAt - total
			if remain <= 0 {
				return total, true
			}
		}
		sub, ok := measureCleanupDir(root, path, deadline, remain, seen)
		total += sub
		if stopAt > 0 && total >= stopAt {
			return total, true
		}
		if !ok {
			return total, false
		}
	}
	return total, true
}

func cleanupMeasureStopped(deadline time.Time) bool {
	return !deadline.IsZero() && time.Now().After(deadline)
}

func removeCleanupTarget(target cleanupTarget) error {
	info, err := os.Lstat(target.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Directories are walked. RemoveAll on a directory junction would delete
	// the directory it points at.
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return os.ErrInvalid
	}
	return removeCleanupPath(target.path)
}

// removeCleanupPath removes one file, link, or empty directory. On Windows,
// os.Remove clears the read-only attribute for a file and then retries, but a
// read-only directory is left in place. Clearing that attribute here is limited
// to a real directory, so a junction is not followed into another folder.
func removeCleanupPath(path string) error {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	if !cleanupCanClearReadOnlyDir(path) {
		return err
	}
	if chmodErr := os.Chmod(path, 0o700); chmodErr != nil {
		return err
	}
	if retry := os.Remove(path); retry == nil || os.IsNotExist(retry) {
		return nil
	} else {
		return retry
	}
}

func cleanupCanClearReadOnlyDir(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !sameCleanupPath(path, resolved) {
		return false
	}
	return true
}

func appendCleanupError(errorsOut []string, err error) []string {
	if err == nil || len(errorsOut) >= systemCleanupMaxReportedErrors {
		return errorsOut
	}
	return append(errorsOut, err.Error())
}
