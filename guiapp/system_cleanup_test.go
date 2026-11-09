package guiapp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSystemCleanupManualRemovesOnlyRegenerableFiles(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	roots := app.systemCleanupRoots()
	keepData := filepath.Join(roots.dataDir, "keep.db")
	cacheBlob := filepath.Join(roots.toolCache, "_cacache", "blob")
	pending := filepath.Join(roots.appTemp, appTempPendingUploadName)
	diag := filepath.Join(roots.appTemp, "maclaw-diagnostics-1.zip")
	gitignore := filepath.Join(roots.skillTmp, skillTmpGitignoreName)
	skillScratch := filepath.Join(roots.skillTmp, "scratch.txt")
	worktree := filepath.Join(roots.osTemp, "maclaw-coding-worktrees", "repo.txt")
	vscodeSnapshot := filepath.Join(roots.osTemp, "maclaw-vscode", "digest", "snapshots", "snapshot-1", "main.go")
	verifyOut := filepath.Join(roots.osTemp, "maclaw-verify", "out.txt")
	unrelated := filepath.Join(roots.osTemp, "notes.txt")
	preview := filepath.Join(roots.osTemp, "maclaw-preview-chrome-1", "cache")
	script := filepath.Join(roots.osTemp, "maclaw_script_1.py")
	preferences := filepath.Join(roots.webviewUserData, "EBWebView", "Default", "Preferences")
	webviewCache := filepath.Join(roots.webviewUserData, "EBWebView", "Default", "Cache", "data")
	promptJSON := filepath.Join(roots.promptCache, "answer.json")
	promptTmp := filepath.Join(roots.promptCache, ".llm_resp_123.tmp")
	searchBlob := filepath.Join(roots.searchIndexes[0], "index.gob")
	anchors := filepath.Join(roots.intentAnchors[0], "anchors.bin")

	for _, path := range []string{
		keepData, cacheBlob, pending, diag, gitignore, skillScratch, worktree, vscodeSnapshot, verifyOut,
		unrelated, preview, script, preferences, webviewCache, promptJSON, promptTmp, searchBlob, anchors,
		roots.toolEmbeddings,
	} {
		writeCleanupFile(t, path, "temp-bytes")
	}
	outside := filepath.Join(app.testHomeDir, "keep-outside", "secret.txt")
	writeCleanupFile(t, outside, "secret")
	link := filepath.Join(roots.osTemp, "maclaw-latex-preview")
	if err := os.Symlink(filepath.Dir(outside), link); err != nil {
		t.Logf("symlink unavailable: %v", err)
	}
	nestedLink := filepath.Join(filepath.Dir(verifyOut), "outside-link")
	if err := os.Symlink(outside, nestedLink); err != nil {
		t.Logf("nested symlink unavailable: %v", err)
	}

	result, err := app.CleanSystemCleanupNow()
	if err != nil {
		t.Fatalf("CleanSystemCleanupNow() error = %v", err)
	}
	if result.Skipped {
		t.Fatalf("manual cleanup skipped: %+v", result)
	}
	if result.RemovedCount == 0 || result.FreedBytes == 0 {
		t.Fatalf("cleanup removed nothing: %+v", result)
	}
	assertCleanupGone(t, cacheBlob, diag, skillScratch, verifyOut, preview, script, webviewCache, promptTmp, searchBlob, anchors, roots.toolEmbeddings)
	assertCleanupKept(t, keepData, pending, gitignore, worktree, vscodeSnapshot, unrelated, preferences, promptJSON, outside)
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("symlink staging dir still present")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("symlink cleanup removed the outside target: %v", err)
	}
}

func TestSystemCleanupHonorsDisabledCategory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	roots := app.systemCleanupRoots()
	kept := filepath.Join(roots.appTemp, "diag.zip")
	removed := filepath.Join(roots.toolCache, "pkg.bin")
	writeCleanupFile(t, kept, "keep")
	writeCleanupFile(t, removed, "remove")
	setSystemCleanup(t, app, corelib.SystemCleanupConfig{
		Enabled:            true,
		IntervalDays:       7,
		MaxBytes:           32,
		DisabledCategories: []string{systemCleanupCatAppTemp, "not-a-category"},
	})

	if _, err := app.CleanSystemCleanupNow(); err != nil {
		t.Fatal(err)
	}
	assertCleanupKept(t, kept)
	assertCleanupGone(t, removed)

	status, err := app.GetSystemCleanupStatus()
	if err != nil {
		t.Fatal(err)
	}
	var sawApp bool
	for _, category := range status.Categories {
		if category.ID == systemCleanupCatAppTemp {
			sawApp = true
			if category.Selected {
				t.Fatal("app temp category still selected")
			}
			if category.SizeBytes == 0 {
				t.Fatal("unchecked category size was not reported")
			}
		}
	}
	if !sawApp {
		t.Fatal("app temp category missing from status")
	}
}

func TestSystemCleanupAutoRespectsIntervalThresholdAndGrace(t *testing.T) {
	t.Run("not due", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		path := filepath.Join(app.systemCleanupRoots().appTemp, "small.txt")
		writeCleanupFile(t, path, "x")
		ageCleanupPath(t, path, 2*time.Hour)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       true,
			IntervalDays:  7,
			MaxBytes:      1 << 20,
			LastCleanupAt: time.Now().Format(time.RFC3339),
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Skipped || result.Reason != "not_due" {
			t.Fatalf("result = %+v, want skipped not_due", result)
		}
		assertCleanupKept(t, path)
	})

	t.Run("size", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		path := filepath.Join(app.systemCleanupRoots().appTemp, "big.txt")
		writeCleanupFile(t, path, "this is more than thirty two bytes of temp")
		ageCleanupPath(t, path, time.Hour)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       true,
			IntervalDays:  7,
			MaxBytes:      32,
			LastCleanupAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Skipped || result.Reason != "size" || result.RemovedCount == 0 {
			t.Fatalf("result = %+v, want size cleanup", result)
		}
		assertCleanupGone(t, path)
	})

	t.Run("interval", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		path := filepath.Join(app.systemCleanupRoots().appTemp, "old.txt")
		writeCleanupFile(t, path, "x")
		ageCleanupPath(t, path, time.Hour)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       true,
			IntervalDays:  7,
			MaxBytes:      1 << 30,
			LastCleanupAt: time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Skipped || result.Reason != "interval" {
			t.Fatalf("result = %+v, want interval cleanup", result)
		}
		assertCleanupGone(t, path)
	})

	t.Run("recent files wait", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		path := filepath.Join(app.systemCleanupRoots().appTemp, "fresh.txt")
		writeCleanupFile(t, path, "still in use")
		previous := time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       true,
			IntervalDays:  7,
			MaxBytes:      1 << 30,
			LastCleanupAt: previous,
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if result.RemovedCount != 0 || result.SkippedRecent == 0 || result.LastCleanupAt != "" {
			t.Fatalf("result = %+v, want recent skip without a new timestamp", result)
		}
		assertCleanupKept(t, path)
		cfg, err := app.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SystemCleanup.LastCleanupAt != previous {
			t.Fatalf("LastCleanupAt = %q, want %q", cfg.SystemCleanup.LastCleanupAt, previous)
		}
	})

	t.Run("baseline starts the clock", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		path := filepath.Join(app.systemCleanupRoots().appTemp, "small.txt")
		writeCleanupFile(t, path, "x")
		ageCleanupPath(t, path, 2*time.Hour)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:      true,
			IntervalDays: 7,
			MaxBytes:     1 << 20,
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Skipped || result.Reason != "baseline" || result.RemovedCount != 0 || result.LastCleanupAt == "" {
			t.Fatalf("result = %+v, want skipped baseline with a timestamp", result)
		}
		assertCleanupKept(t, path)
		cfg, err := app.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SystemCleanup.LastCleanupAt != result.LastCleanupAt {
			t.Fatalf("LastCleanupAt = %q, result %q", cfg.SystemCleanup.LastCleanupAt, result.LastCleanupAt)
		}
	})

	t.Run("first look cleans when already over the threshold", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		path := filepath.Join(app.systemCleanupRoots().appTemp, "big.txt")
		writeCleanupFile(t, path, "this is more than thirty two bytes of temp")
		ageCleanupPath(t, path, time.Hour)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:      true,
			IntervalDays: 7,
			MaxBytes:     32,
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Skipped || result.Reason != "size" || result.RemovedCount == 0 || result.LastCleanupAt == "" {
			t.Fatalf("result = %+v, want first-look size cleanup", result)
		}
		assertCleanupGone(t, path)
	})

	t.Run("recent child of an old directory waits", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		dir := filepath.Join(app.systemCleanupRoots().appTemp, "session")
		child := filepath.Join(dir, "live.txt")
		writeCleanupFile(t, child, "still writing")
		ageCleanupPath(t, child, 0)
		ageCleanupPath(t, dir, time.Hour)
		previous := time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       true,
			IntervalDays:  7,
			MaxBytes:      1 << 30,
			LastCleanupAt: previous,
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if result.RemovedCount != 0 || result.SkippedRecent == 0 || result.LastCleanupAt != "" {
			t.Fatalf("result = %+v, want recent child skip without a new timestamp", result)
		}
		assertCleanupKept(t, dir, child)
		cfg, err := app.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SystemCleanup.LastCleanupAt != previous {
			t.Fatalf("LastCleanupAt = %q, want %q", cfg.SystemCleanup.LastCleanupAt, previous)
		}
	})

	t.Run("recent nested file waits", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		dir := filepath.Join(app.systemCleanupRoots().appTemp, "session")
		nested := filepath.Join(dir, "nested")
		child := filepath.Join(nested, "live.txt")
		writeCleanupFile(t, child, "still writing")
		ageCleanupPath(t, child, 0)
		ageCleanupPath(t, nested, time.Hour)
		ageCleanupPath(t, dir, time.Hour)
		previous := time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       true,
			IntervalDays:  7,
			MaxBytes:      1 << 30,
			LastCleanupAt: previous,
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if result.RemovedCount != 0 || result.SkippedRecent == 0 || result.LastCleanupAt != "" {
			t.Fatalf("result = %+v, want nested recent skip without a new timestamp", result)
		}
		assertCleanupKept(t, dir, child)
	})

	t.Run("old file beside a recent file is removed", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		dir := filepath.Join(app.systemCleanupRoots().appTemp, "session")
		oldFile := filepath.Join(dir, "old.txt")
		live := filepath.Join(dir, "nested", "live.txt")
		writeCleanupFile(t, oldFile, "stale")
		writeCleanupFile(t, live, "still writing")
		ageCleanupPath(t, oldFile, time.Hour)
		ageCleanupPath(t, filepath.Dir(live), time.Hour)
		ageCleanupPath(t, dir, time.Hour)
		ageCleanupPath(t, live, 0)
		previous := time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       true,
			IntervalDays:  7,
			MaxBytes:      1 << 30,
			LastCleanupAt: previous,
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if result.RemovedCount != 0 || result.SkippedRecent == 0 || result.FreedBytes == 0 || result.LastCleanupAt != "" {
			t.Fatalf("result = %+v, want the old file removed and the recent file kept without a new timestamp", result)
		}
		assertCleanupGone(t, oldFile)
		assertCleanupKept(t, dir, live)
		cfg, err := app.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SystemCleanup.LastCleanupAt != previous {
			t.Fatalf("LastCleanupAt = %q, want %q", cfg.SystemCleanup.LastCleanupAt, previous)
		}
	})

	t.Run("keeps live temp the app still owns", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		roots := app.systemCleanupRoots()
		keptZip := filepath.Join(roots.appTemp, "maclaw-diagnostics-kept.zip")
		looseZip := filepath.Join(roots.appTemp, "maclaw-diagnostics-loose.zip")
		pending := filepath.Join(roots.appTemp, appTempPendingUploadName)
		staged := filepath.Join(roots.appTemp, appTempInputsName, "input-1", "notes.txt")
		migration := filepath.Join(roots.appTemp, appTempMigrationName, "migration-export-1", "payload.json")
		junk := filepath.Join(roots.appTemp, "old-scratch.txt")
		for _, path := range []string{keptZip, looseZip, staged, migration, junk} {
			writeCleanupFile(t, path, "this is more than thirty two bytes of temp")
			ageCleanupPath(t, path, 2*time.Hour)
		}
		body, err := json.Marshal(pendingBugReportUploadMetadata{ZipPath: keptZip, IncludedFiles: 1})
		if err != nil {
			t.Fatal(err)
		}
		writeCleanupFile(t, pending, string(body))
		ageCleanupPath(t, pending, 2*time.Hour)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       true,
			IntervalDays:  7,
			MaxBytes:      1 << 30,
			LastCleanupAt: time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Skipped || result.Reason != "interval" {
			t.Fatalf("result = %+v, want interval cleanup", result)
		}
		assertCleanupKept(t, keptZip, pending, staged, migration)
		assertCleanupGone(t, looseZip, junk)
	})

	t.Run("disabled", func(t *testing.T) {
		app := &App{testHomeDir: t.TempDir()}
		path := filepath.Join(app.systemCleanupRoots().appTemp, "old.txt")
		writeCleanupFile(t, path, "x")
		ageCleanupPath(t, path, time.Hour)
		setSystemCleanup(t, app, corelib.SystemCleanupConfig{
			Enabled:       false,
			IntervalDays:  7,
			MaxBytes:      32,
			LastCleanupAt: time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		})
		result, err := app.cleanSystem(t.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Skipped || result.Reason != "disabled" {
			t.Fatalf("result = %+v, want disabled", result)
		}
		assertCleanupKept(t, path)
	})
}

func TestPatchSystemCleanupPreservesOmittedFields(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	setSystemCleanup(t, app, corelib.SystemCleanupConfig{
		Enabled:            false,
		IntervalDays:       14,
		MaxBytes:           2048,
		DisabledCategories: []string{systemCleanupCatDemoShots},
		LastCleanupAt:      "2026-11-01T00:00:00Z",
	})
	cfg, err := app.PatchConfigFields(map[string]interface{}{
		"system_cleanup": map[string]interface{}{
			"max_bytes": float64(4096),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.SystemCleanup
	if got.Enabled || got.IntervalDays != 14 || got.MaxBytes != 4096 || got.LastCleanupAt != "2026-11-01T00:00:00Z" {
		t.Fatalf("partial patch changed unrelated fields: %+v", got)
	}
	if len(got.DisabledCategories) != 1 || got.DisabledCategories[0] != systemCleanupCatDemoShots {
		t.Fatalf("disabled categories = %#v", got.DisabledCategories)
	}
}

func TestSystemCleanupMeasureStaysInsideDirectory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	roots := app.systemCleanupRoots()
	dir := filepath.Join(roots.osTemp, "maclaw-verify")
	inside := filepath.Join(dir, "out.txt")
	writeCleanupFile(t, inside, "inside")
	outsideDir := filepath.Join(app.testHomeDir, "outside")
	outside := filepath.Join(outsideDir, "secret.bin")
	writeCleanupFile(t, outside, strings.Repeat("x", 8192))
	link := filepath.Join(dir, "link")
	if !makeCleanupDirLink(t, link, outsideDir) {
		t.Skip("directory link is unavailable")
	}

	size, known := measureCleanupPath(dir, time.Time{}, 0)
	if !known {
		t.Fatal("measure reported an unknown size")
	}
	info, err := os.Lstat(inside)
	if err != nil {
		t.Fatal(err)
	}
	if size != info.Size() {
		t.Fatalf("measure counted %d bytes, want %d from the file inside the cleanup directory", size, info.Size())
	}

	if _, err := app.CleanSystemCleanupNow(); err != nil {
		t.Fatal(err)
	}
	assertCleanupGone(t, inside, link)
	assertCleanupKept(t, outside)
}

func makeCleanupDirLink(t *testing.T, link, target string) bool {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// A junction is a directory reparse point and is not a symlink.
		// Measuring must not walk through it.
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Logf("junction unavailable: %v (%s)", err, out)
			return false
		}
		return true
	}
	if err := os.Symlink(target, link); err != nil {
		t.Logf("symlink unavailable: %v", err)
		return false
	}
	return true
}

func TestSystemCleanupDirectoryLinkCycleStaysBounded(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	roots := app.systemCleanupRoots()
	dir := filepath.Join(roots.osTemp, "maclaw-verify")
	inside := filepath.Join(dir, "out.txt")
	nested := filepath.Join(dir, "sub", "a.txt")
	writeCleanupFile(t, inside, "inside")
	writeCleanupFile(t, nested, "nested")
	loop := filepath.Join(dir, "loop")
	alias := filepath.Join(dir, "alias")
	if !makeCleanupDirLink(t, loop, dir) || !makeCleanupDirLink(t, alias, filepath.Dir(nested)) {
		t.Skip("directory link is unavailable")
	}

	size, known := measureCleanupPath(dir, time.Time{}, 0)
	if !known {
		t.Fatal("measure reported an unknown size")
	}
	insideInfo, err := os.Lstat(inside)
	if err != nil {
		t.Fatal(err)
	}
	nestedInfo, err := os.Lstat(nested)
	if err != nil {
		t.Fatal(err)
	}
	want := insideInfo.Size() + nestedInfo.Size()
	if size != want {
		t.Fatalf("measure counted %d bytes, want %d", size, want)
	}
	if _, err := app.CleanSystemCleanupNow(); err != nil {
		t.Fatal(err)
	}
	assertCleanupGone(t, inside, nested, loop, alias, dir)
}

func TestSystemCleanupRepeatDirectoryLinkKeepsRecentFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	roots := app.systemCleanupRoots()
	dir := filepath.Join(roots.appTemp, "session")
	real := filepath.Join(dir, "z-real")
	oldFile := filepath.Join(real, "old.txt")
	live := filepath.Join(real, "live.txt")
	writeCleanupFile(t, oldFile, "stale")
	writeCleanupFile(t, live, "still writing")
	link := filepath.Join(dir, "a-link")
	if !makeCleanupDirLink(t, link, real) {
		t.Skip("directory link is unavailable")
	}
	// Recreate the real directory entry so it is enumerated after the link.
	reopenCleanupDirEntry(t, real)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	linkAt, realAt := -1, -1
	for i, entry := range entries {
		switch entry.Name() {
		case "a-link":
			linkAt = i
		case "z-real":
			realAt = i
		}
	}
	if linkAt < 0 || realAt < 0 || linkAt > realAt {
		t.Fatalf("link must be enumerated before the real directory, got %#v", cleanupEntryNames(entries))
	}
	ageCleanupPath(t, oldFile, time.Hour)
	ageCleanupPath(t, real, time.Hour)
	ageCleanupPath(t, link, time.Hour)
	ageCleanupPath(t, dir, time.Hour)
	ageCleanupPath(t, live, 0)
	previous := time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	setSystemCleanup(t, app, corelib.SystemCleanupConfig{
		Enabled:       true,
		IntervalDays:  7,
		MaxBytes:      1 << 30,
		LastCleanupAt: previous,
	})
	result, err := app.cleanSystem(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedBusy != 0 || len(result.Errors) != 0 || result.SkippedRecent == 0 || result.Incomplete {
		t.Fatalf("result = %+v, want the recent file kept without a busy error", result)
	}
	assertCleanupGone(t, oldFile)
	assertCleanupKept(t, live)
}

func reopenCleanupDirEntry(t *testing.T, path string) {
	t.Helper()
	tmp := path + ".reorder"
	if err := os.Rename(path, tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func cleanupEntryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

func TestSystemCleanupRemovesReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a read-only directory attribute blocks RemoveDirectory only on Windows")
	}
	app := &App{testHomeDir: t.TempDir()}
	dir := filepath.Join(app.systemCleanupRoots().appTemp, "packed")
	nested := filepath.Join(dir, "inner")
	file := filepath.Join(nested, "blob.bin")
	writeCleanupFile(t, file, "packed")
	if err := os.Chmod(file, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	if !cleanupPathIsReadOnly(t, nested) || !cleanupPathIsReadOnly(t, dir) {
		t.Fatal("directory was not marked read-only")
	}
	result, err := app.CleanSystemCleanupNow()
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedBusy != 0 || len(result.Errors) != 0 || result.RemovedCount == 0 {
		t.Fatalf("result = %+v, want the read-only directory removed", result)
	}
	assertCleanupGone(t, file, nested, dir)
}

func cleanupPathIsReadOnly(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()&0o200 == 0
}

func TestSystemCleanupRejectsUnsafeRoots(t *testing.T) {
	if !unsafeCleanupRoot(string(filepath.Separator)) {
		t.Fatal("filesystem root was accepted")
	}
	if runtime.GOOS == "windows" && !unsafeCleanupRoot(`C:\`) {
		t.Fatal(`drive root was accepted`)
	}
	roots := systemCleanupRoots{base: string(filepath.Separator), appTemp: filepath.Join(string(filepath.Separator), "temp"), osTemp: string(filepath.Separator)}
	if roots.appTempRoot() != "" || roots.osTempRoot() != "" {
		t.Fatalf("unsafe roots accepted: app=%q os=%q", roots.appTempRoot(), roots.osTempRoot())
	}
	if matchOSTempCategory("maclaw-coding-worktrees") != "" || matchOSTempCategory("maclaw-chrome-debug-profile") != "" {
		t.Fatal("runtime worktrees or browser profiles were classified as temp")
	}
	if matchOSTempCategory("maclaw-vscode") != "" {
		t.Fatal("local VS Code snapshots of remote files were classified as temp")
	}
	if matchOSTempCategory("maclaw-verify") != systemCleanupCatCodingVerify {
		t.Fatal("verify output was not classified")
	}
}

func setSystemCleanup(t *testing.T, app *App, cfg corelib.SystemCleanupConfig) {
	t.Helper()
	if err := app.PatchConfig(func(current *corelib.AppConfig) {
		current.SystemCleanup = cfg
	}); err != nil {
		t.Fatal(err)
	}
}

func writeCleanupFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ageCleanupPath(t *testing.T, path string, age time.Duration) {
	t.Helper()
	ts := time.Now().Add(-age)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func assertCleanupGone(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
}

func assertCleanupKept(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("%s was removed: %v", path, err)
		}
	}
}
