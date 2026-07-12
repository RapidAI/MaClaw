package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// DataMigrationProgress is emitted to the frontend during data directory migration.
type DataMigrationProgress struct {
	Phase       string  `json:"phase"`       // "scanning" | "copying" | "done" | "error"
	Percent     float64 `json:"percent"`     // 0-100
	CurrentFile string  `json:"currentFile"` // current file being copied (basename only)
	TotalFiles  int     `json:"totalFiles"`
	CopiedFiles int     `json:"copiedFiles"`
	Error       string  `json:"error,omitempty"`
}

// migrateToCustomDataDir checks if data_dir is configured and different from
// the default ~/.maclaw. If so, migrates all data (except config.json) from
// ~/.maclaw to the new directory with progress reporting.
func (a *App) migrateToCustomDataDir() {
	targetDir := corelib.MaclawBaseDir()
	defaultDir := corelib.MaclawDefaultBaseDir()

	// No custom dir configured, or same as default — nothing to do.
	if targetDir == defaultDir {
		return
	}

	// Normalize paths for comparison.
	targetClean := filepath.Clean(strings.ToLower(targetDir))
	defaultClean := filepath.Clean(strings.ToLower(defaultDir))
	if targetClean == defaultClean {
		return
	}

	// Check if migration is needed: target dir doesn't exist or is empty.
	if !a.needsDataMigration(defaultDir, targetDir) {
		log.Printf("[DataMigration] target dir %s already has data, skipping migration", targetDir)
		return
	}

	log.Printf("[DataMigration] starting migration from %s to %s", defaultDir, targetDir)
	a.performDataMigration(defaultDir, targetDir)
}

// needsDataMigration returns true if data should be migrated from src to dst.
// Migration is needed when:
// - src exists and has data files (beyond config.json)
// - dst doesn't exist OR dst exists but has no "memories.json" (marker file)
func (a *App) needsDataMigration(src, dst string) bool {
	// Check if source has already been migrated to this exact target.
	if markerData, err := os.ReadFile(filepath.Join(src, ".migrated_to")); err == nil {
		markerTarget := filepath.Clean(strings.TrimSpace(string(markerData)))
		if markerTarget == filepath.Clean(dst) {
			return false // already migrated to this target
		}
	}

	// Check if source has anything to migrate.
	srcEntries, err := os.ReadDir(src)
	if err != nil || len(srcEntries) == 0 {
		return false
	}
	hasDataInSrc := false
	for _, e := range srcEntries {
		if e.Name() != "config.json" && e.Name() != ".migrated_to" {
			hasDataInSrc = true
			break
		}
	}
	if !hasDataInSrc {
		return false
	}

	return true
}

// performDataMigration copies all files/dirs from src to dst (except config.json),
// emitting progress events to the frontend.
func (a *App) performDataMigration(src, dst string) {
	start := time.Now()

	// Phase 1: Scan — count total files to copy.
	a.emitMigrationProgress(DataMigrationProgress{Phase: "scanning", Percent: 0})

	var filesToCopy []string
	var totalSize int64
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		rel, _ := filepath.Rel(src, path)
		// Skip config.json and migration marker at root level.
		if rel == "config.json" || rel == ".migrated_to" {
			return nil
		}
		// Skip the root directory itself.
		if rel == "." {
			return nil
		}
		if !info.IsDir() {
			filesToCopy = append(filesToCopy, rel)
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		log.Printf("[DataMigration] scan error: %v", err)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
		return
	}

	totalFiles := len(filesToCopy)
	if totalFiles == 0 {
		log.Printf("[DataMigration] no files to migrate")
		a.emitMigrationProgress(DataMigrationProgress{Phase: "done", Percent: 100})
		return
	}

	log.Printf("[DataMigration] found %d files (%d MB) to migrate", totalFiles, totalSize/(1024*1024))

	// Check target disk is writable (best-effort pre-check).
	if !a.checkTargetWritable(dst) {
		errMsg := fmt.Sprintf("目标目录不可写: %s", dst)
		log.Printf("[DataMigration] %s", errMsg)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: errMsg})
		return
	}

	// Phase 2: Copy files with progress.
	_ = os.MkdirAll(dst, 0o755)
	copiedFiles := 0
	failedFiles := 0
	var lastEmit time.Time

	for _, rel := range filesToCopy {
		srcPath := filepath.Join(src, rel)
		dstPath := filepath.Join(dst, rel)

		// Ensure destination directory exists.
		dstDir := filepath.Dir(dstPath)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			log.Printf("[DataMigration] mkdir failed for %s: %v", dstDir, err)
			failedFiles++
			continue
		}

		// Copy file.
		if err := copyMigrationFile(srcPath, dstPath); err != nil {
			log.Printf("[DataMigration] copy failed %s: %v", rel, err)
			failedFiles++
			// If too many failures (>10% or >20 files), abort — likely a disk issue.
			if failedFiles > 20 || (totalFiles > 10 && failedFiles*10 > totalFiles) {
				errMsg := fmt.Sprintf("迁移失败文件过多 (%d/%d)，已中止。请检查目标磁盘空间和权限。", failedFiles, copiedFiles+failedFiles)
				log.Printf("[DataMigration] aborting: %s", errMsg)
				a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: errMsg})
				return // Do NOT write .migrated_to marker — allow retry next startup.
			}
			continue
		}

		copiedFiles++

		// Emit progress at most every 100ms to avoid flooding the frontend.
		if time.Since(lastEmit) > 100*time.Millisecond {
			percent := float64(copiedFiles+failedFiles) / float64(totalFiles) * 100
			a.emitMigrationProgress(DataMigrationProgress{
				Phase:       "copying",
				Percent:     percent,
				CurrentFile: filepath.Base(rel),
				TotalFiles:  totalFiles,
				CopiedFiles: copiedFiles,
			})
			lastEmit = time.Now()
		}
	}

	elapsed := time.Since(start)
	log.Printf("[DataMigration] completed: %d/%d files copied, %d failed, in %s", copiedFiles, totalFiles, failedFiles, elapsed)

	// Only mark as complete if all files were copied successfully.
	if failedFiles == 0 {
		markerPath := filepath.Join(src, ".migrated_to")
		_ = os.WriteFile(markerPath, []byte(filepath.Clean(dst)), 0o644)
	} else {
		log.Printf("[DataMigration] WARNING: %d files failed to copy, migration marker NOT written (will retry next startup)", failedFiles)
	}

	a.emitMigrationProgress(DataMigrationProgress{
		Phase:       "done",
		Percent:     100,
		TotalFiles:  totalFiles,
		CopiedFiles: copiedFiles,
	})
}

// emitMigrationProgress sends migration progress to the frontend.
func (a *App) emitMigrationProgress(p DataMigrationProgress) {
	if a.ctx != nil {
		a.emitEvent("data-migration-progress", p)
	}
}

// copyMigrationFile copies a single file from src to dst, preserving permissions.
func copyMigrationFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat src: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	// Ensure data is flushed to disk before reporting success.
	return dstFile.Sync()
}

// checkTargetWritable verifies the target directory is writable.
// Returns true if a test file can be created and removed.
func (a *App) checkTargetWritable(targetDir string) bool {
	_ = os.MkdirAll(targetDir, 0o755)
	testFile := filepath.Join(targetDir, ".maclaw_write_check")
	f, err := os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(testFile)
	return true
}

// GetMaclawBaseDir is a Wails binding that returns the current effective
// maclaw base directory (for display in settings UI).
func (a *App) GetMaclawBaseDir() string {
	return a.getMaclawBaseDir()
}

// SelectDataDir opens a native directory picker dialog for the user to choose
// a data directory migration target. Returns the selected path or empty string
// if cancelled.
// Note: CanCreateDirectories only takes effect on macOS. On Windows, the native
// folder picker already supports creating new folders via right-click context menu.
func (a *App) SelectDataDir() string {
	// Start the dialog at the current data directory's parent for convenience.
	defaultDir := a.getMaclawBaseDir()
	if parent := filepath.Dir(defaultDir); parent != "" && parent != "." {
		defaultDir = parent
	}

	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "选择数据目录 / Select Data Directory",
		DefaultDirectory:     defaultDir,
		CanCreateDirectories: true,
	})
	if err != nil {
		return ""
	}
	return selection
}

// SetDataDir is a Wails binding called from the settings UI to update the
// data_dir configuration. Returns an error message if the path is invalid.
// Path resolvers are refreshed immediately; migration still completes on restart.
func (a *App) SetDataDir(newDir string) string {
	newDir = strings.TrimSpace(newDir)

	// Validate the path.
	if newDir != "" {
		// Check if it's an absolute path.
		if !filepath.IsAbs(newDir) {
			return "请输入绝对路径（如 D:\\MaclawData 或 /home/user/maclaw-data）"
		}
		// Check if the path is writable (try to create it).
		if err := os.MkdirAll(newDir, 0o755); err != nil {
			return fmt.Sprintf("无法创建目录: %v", err)
		}
		// Don't allow setting to the same as default.
		defaultDir := corelib.MaclawDefaultBaseDir()
		if filepath.Clean(strings.ToLower(newDir)) == filepath.Clean(strings.ToLower(defaultDir)) {
			newDir = "" // same as default, clear it
		}
	}

	// Save only data_dir so concurrent settings changes are not overwritten.
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) { cfg.DataDir = newDir }); err != nil {
		return fmt.Sprintf("保存配置失败: %v", err)
	}

	// If clearing data_dir (reverting to default), remove the migration marker
	// so data in the default directory is used directly on next startup.
	if newDir == "" {
		markerPath := filepath.Join(corelib.MaclawDefaultBaseDir(), ".migrated_to")
		_ = os.Remove(markerPath)
	}

	return "" // success, no error
}
