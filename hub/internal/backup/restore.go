package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	_ "modernc.org/sqlite"
)

func restoreValidated(opts RestoreOptions) (*RestoreResult, error) {
	startedAt := time.Now()
	if strings.TrimSpace(opts.ArchivePath) == "" {
		return nil, fmt.Errorf("restore requires archive path")
	}
	targetRoot := strings.TrimSpace(opts.TargetRoot)
	if targetRoot == "" {
		targetRoot = "."
	}
	absRoot, err := filepath.Abs(targetRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve target root: %w", err)
	}
	if info, statErr := os.Lstat(absRoot); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("restore target root must not be a symlink: %s", absRoot)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect restore target root: %w", statErr)
	}
	manifest, err := Inspect(opts.ArchivePath)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(absRoot)
	stageParent := parent
	if opts.DryRun {
		if _, statErr := os.Stat(parent); errors.Is(statErr, os.ErrNotExist) {
			stageParent = os.TempDir()
		} else if statErr != nil {
			return nil, fmt.Errorf("inspect restore parent: %w", statErr)
		}
	} else if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create restore parent: %w", err)
	}
	stageRoot, err := os.MkdirTemp(stageParent, ".maclaw-restore-stage-*")
	if err != nil {
		return nil, fmt.Errorf("create restore staging directory: %w", err)
	}
	defer os.RemoveAll(stageRoot)

	extracted, err := extractAndVerifyArchive(opts.ArchivePath, stageRoot, manifest)
	if err != nil {
		return nil, err
	}
	verification, err := verifyStagedGenerationWithProvider(context.Background(), stageRoot, manifest, opts.KeyProvider)
	if err != nil {
		return nil, err
	}
	result := &RestoreResult{
		ArchivePath:  opts.ArchivePath,
		TargetRoot:   absRoot,
		GenerationID: manifest.GenerationID,
		DryRun:       opts.DryRun,
		Verification: verification,
	}
	for _, entry := range extracted {
		destination := filepath.Join(absRoot, filepath.FromSlash(entry.Path))
		if exists(destination) && !opts.Force {
			result.Skipped = append(result.Skipped, entry)
			continue
		}
		result.Restored = append(result.Restored, entry)
	}
	if opts.DryRun {
		finishRestoreResult(result, startedAt)
		return result, nil
	}
	if len(result.Skipped) > 0 && !opts.Force {
		return result, fmt.Errorf("restore would overwrite %d existing files; rerun with --force after stopping hub", len(result.Skipped))
	}
	if err := installStagedGeneration(stageRoot, absRoot, manifest); err != nil {
		return result, err
	}
	finishRestoreResult(result, startedAt)
	return result, nil
}

func finishRestoreResult(result *RestoreResult, startedAt time.Time) {
	if result == nil {
		return
	}
	result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	result.DurationMS = time.Since(startedAt).Milliseconds()
}

func extractAndVerifyArchive(archivePath, stageRoot string, manifest *Manifest) ([]Entry, error) {
	expected := make(map[string]Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if err := validateArchivePath(entry.Path); err != nil {
			return nil, fmt.Errorf("invalid manifest entry: %w", err)
		}
		if _, duplicate := expected[entry.Path]; duplicate {
			return nil, fmt.Errorf("duplicate manifest entry: %s", entry.Path)
		}
		if manifest.Version >= 2 && !validDigest(entry.SHA256) {
			return nil, fmt.Errorf("manifest entry %s has invalid sha256", entry.Path)
		}
		expected[entry.Path] = entry
	}
	file, gr, tr, err := openTarGzip(archivePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	defer gr.Close()
	seen := make(map[string]bool, len(expected))
	var extracted []Entry
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("read backup archive: %w", nextErr)
		}
		if header.Name == ManifestPath {
			continue
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("unsupported archive entry type for %s", header.Name)
		}
		if err := validateArchivePath(header.Name); err != nil {
			return nil, err
		}
		entry, listed := expected[header.Name]
		if manifest.Version >= 2 && !listed {
			return nil, fmt.Errorf("archive entry not declared in manifest: %s", header.Name)
		}
		if seen[header.Name] {
			return nil, fmt.Errorf("duplicate archive entry: %s", header.Name)
		}
		seen[header.Name] = true
		if !listed {
			entry = Entry{Path: header.Name, Kind: "unknown", Size: header.Size}
		}
		if listed && entry.Size != header.Size {
			return nil, fmt.Errorf("archive entry %s size=%d want=%d", header.Name, header.Size, entry.Size)
		}
		destination := filepath.Join(stageRoot, filepath.FromSlash(header.Name))
		if !isWithin(stageRoot, destination) {
			return nil, fmt.Errorf("archive entry escapes restore staging root: %s", header.Name)
		}
		digest, size, err := extractHashedFile(header, tr, destination)
		if err != nil {
			return nil, err
		}
		if size != header.Size {
			return nil, fmt.Errorf("archive entry %s truncated: size=%d want=%d", header.Name, size, header.Size)
		}
		if manifest.Version >= 2 && !strings.EqualFold(digest, entry.SHA256) {
			return nil, fmt.Errorf("archive entry %s sha256 mismatch", header.Name)
		}
		entry.SHA256 = digest
		extracted = append(extracted, entry)
	}
	for path := range expected {
		if !seen[path] {
			return nil, fmt.Errorf("manifest entry missing from archive: %s", path)
		}
	}
	sort.Slice(extracted, func(i, j int) bool { return extracted[i].Path < extracted[j].Path })
	return extracted, nil
}

func extractHashedFile(header *tar.Header, reader io.Reader, destination string) (string, int64, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", 0, fmt.Errorf("create restore staging dir: %w", err)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, header.FileInfo().Mode().Perm())
	if err != nil {
		return "", 0, fmt.Errorf("create staged restore file %s: %w", destination, err)
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(output, hasher), reader)
	closeErr := output.Close()
	if copyErr != nil {
		return "", size, fmt.Errorf("extract archive entry %s: %w", header.Name, copyErr)
	}
	if closeErr != nil {
		return "", size, fmt.Errorf("close staged archive entry %s: %w", header.Name, closeErr)
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func verifyStagedGeneration(ctx context.Context, stageRoot string, manifest *Manifest) (*CloudWorkspaceSummary, error) {
	return verifyStagedGenerationWithProvider(ctx, stageRoot, manifest, nil)
}

func verifyStagedGenerationWithProvider(ctx context.Context, stageRoot string, manifest *Manifest, keyProvider cloudworkspace.KeyProvider) (*CloudWorkspaceSummary, error) {
	databasePath := strings.TrimSpace(manifest.DatabasePath)
	if databasePath == "" {
		for _, entry := range manifest.Entries {
			if entry.Kind == "sqlite_snapshot" {
				databasePath = entry.Path
				break
			}
		}
	}
	if databasePath == "" {
		return nil, fmt.Errorf("backup manifest has no sqlite snapshot")
	}
	if err := validateArchivePath(databasePath); err != nil {
		return nil, fmt.Errorf("invalid sqlite snapshot path: %w", err)
	}
	dbPath := filepath.Join(stageRoot, filepath.FromSlash(databasePath))
	if !isWithin(stageRoot, dbPath) {
		return nil, fmt.Errorf("sqlite snapshot escapes restore staging root")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open restored sqlite snapshot: %w", err)
	}
	defer db.Close()
	if err := sqliteQuickCheck(ctx, db); err != nil {
		return nil, err
	}
	cloudRootPath := "data/cloud-workspaces"
	if manifest.CloudWorkspace != nil && strings.TrimSpace(manifest.CloudWorkspace.Root) != "" {
		cloudRootPath = manifest.CloudWorkspace.Root
	}
	if err := validateArchivePath(cloudRootPath); err != nil {
		return nil, fmt.Errorf("invalid cloud workspace backup root: %w", err)
	}
	cloudRoot := filepath.Join(stageRoot, filepath.FromSlash(cloudRootPath))
	var report *cloudworkspace.RecoveryReport
	if keyProvider != nil {
		report, err = cloudworkspace.VerifyRecoveryWithProvider(ctx, db, cloudRoot, keyProvider)
	} else {
		report, err = cloudworkspace.VerifyRecovery(ctx, db, cloudRoot, cloudRoot)
	}
	if err != nil {
		return nil, fmt.Errorf("verify restored cloud workspace generation: %w", err)
	}
	actual := cloudWorkspaceSummary(report)
	actual.Root = cloudRootPath
	if manifest.CloudWorkspace != nil {
		if err := compareCloudWorkspaceSummary(manifest.CloudWorkspace, actual); err != nil {
			return nil, err
		}
	}
	return actual, nil
}

func compareCloudWorkspaceSummary(expected, actual *CloudWorkspaceSummary) error {
	if expected.Workspaces != actual.Workspaces || expected.Objects != actual.Objects ||
		expected.VerifiedObjects != actual.VerifiedObjects || expected.Snapshots != actual.Snapshots ||
		expected.Sidecars != actual.Sidecars || expected.Usage != actual.Usage {
		return fmt.Errorf("cloud workspace recovery summary mismatch: expected=%+v actual=%+v", expected, actual)
	}
	if (expected.MasterKey == nil) != (actual.MasterKey == nil) {
		return fmt.Errorf("cloud workspace recovery key descriptor mismatch")
	}
	if expected.MasterKey != nil && *expected.MasterKey != *actual.MasterKey {
		return fmt.Errorf("cloud workspace recovery key mismatch: provider/key id does not match backup generation")
	}
	return nil
}

// installStagedGeneration swaps each top-level archive root by rename. Existing
// files that were intentionally not in the archive (for example logs) are
// copied into the candidate first, so --force replaces the backed generation
// without discarding unrelated local files. Rollback renames are retained until
// every top-level switch succeeds.
func installStagedGeneration(stageRoot, targetRoot string, manifest *Manifest) error {
	if _, err := os.Stat(targetRoot); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(stageRoot, targetRoot); err != nil {
			return fmt.Errorf("install restored generation: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect restore target: %w", err)
	}
	entries, err := os.ReadDir(stageRoot)
	if err != nil {
		return err
	}
	rollbackRoot, err := os.MkdirTemp(filepath.Dir(targetRoot), ".maclaw-restore-rollback-*")
	if err != nil {
		return fmt.Errorf("create restore rollback directory: %w", err)
	}
	committed := make([]restoreSwap, 0, len(entries))
	rollback := func() {
		for i := len(committed) - 1; i >= 0; i-- {
			swap := committed[i]
			_ = os.RemoveAll(swap.Destination)
			if swap.HadOriginal {
				_ = os.Rename(swap.Rollback, swap.Destination)
			}
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		source := filepath.Join(stageRoot, name)
		destination := filepath.Join(targetRoot, name)
		oldPath := filepath.Join(rollbackRoot, name)
		hadOriginal := false
		if oldInfo, statErr := os.Lstat(destination); statErr == nil {
			if oldInfo.Mode()&os.ModeSymlink != 0 {
				rollback()
				return fmt.Errorf("restore destination contains unsupported symlink: %s", destination)
			}
			sourceInfo, sourceErr := os.Stat(source)
			if sourceErr != nil {
				rollback()
				return sourceErr
			}
			if oldInfo.IsDir() && sourceInfo.IsDir() {
				if err := copyMissingTree(destination, source); err != nil {
					rollback()
					return fmt.Errorf("preserve existing restore extras: %w", err)
				}
				if err := removeStaleSQLiteSidecars(stageRoot, manifest); err != nil {
					rollback()
					return err
				}
			}
			if err := os.Rename(destination, oldPath); err != nil {
				rollback()
				return fmt.Errorf("stage existing restore root %s: %w", destination, err)
			}
			hadOriginal = true
		} else if !errors.Is(statErr, os.ErrNotExist) {
			rollback()
			return fmt.Errorf("inspect restore destination %s: %w", destination, statErr)
		}
		if err := os.Rename(source, destination); err != nil {
			if hadOriginal {
				_ = os.Rename(oldPath, destination)
			}
			rollback()
			return fmt.Errorf("activate restored root %s: %w", destination, err)
		}
		committed = append(committed, restoreSwap{Destination: destination, Rollback: oldPath, HadOriginal: hadOriginal})
	}
	if err := os.RemoveAll(rollbackRoot); err != nil {
		return fmt.Errorf("restored generation active but rollback cleanup failed: %w", err)
	}
	return nil
}

func removeStaleSQLiteSidecars(stageRoot string, manifest *Manifest) error {
	databasePath := strings.TrimSpace(manifest.DatabasePath)
	if databasePath == "" {
		for _, entry := range manifest.Entries {
			if entry.Kind == "sqlite_snapshot" {
				databasePath = entry.Path
				break
			}
		}
	}
	if databasePath == "" {
		return nil
	}
	database := filepath.Join(stageRoot, filepath.FromSlash(databasePath))
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		path := database + suffix
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale restored sqlite sidecar %s: %w", path, err)
		}
	}
	return nil
}

type restoreSwap struct {
	Destination string
	Rollback    string
	HadOriginal bool
}

func copyMissingTree(existingRoot, candidateRoot string) error {
	return filepath.WalkDir(existingRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("existing restore tree contains unsupported symlink: %s", path)
		}
		rel, err := filepath.Rel(existingRoot, path)
		if err != nil || rel == "." {
			return err
		}
		destination := filepath.Join(candidateRoot, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if _, statErr := os.Stat(destination); statErr == nil {
			return nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			_ = input.Close()
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeOutErr := output.Close()
		closeInErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		return closeInErr
	})
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
