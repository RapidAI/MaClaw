package backup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	modernsqlite "modernc.org/sqlite"
)

const cloudWorkspaceConsistency = "sqlite-online-backup+cws-write-barrier-v1"

// consistentGenerationBarrierHook is set only by package tests to hold the
// barrier at a deterministic point while another SQLite writer is attempted.
var consistentGenerationBarrierHook func()

type stagedGeneration struct {
	ID                 string
	Consistency        string
	DatabasePath       string
	CloudWorkspacePath string
	Report             *cloudworkspace.RecoveryReport
	CutAt              time.Time
	WritePauseMS       int64
}

// stageConsistentGeneration holds SQLite's single-writer barrier while it
// takes an online snapshot and copies the Cloud Workspace encrypted tree. Hub
// reads remain available; mutations wait behind BEGIN IMMEDIATE. Referenced
// object and sidecar files are immutable once their database root commits, so
// the staged pair represents one recoverable generation.
func stageConsistentGeneration(ctx context.Context, dsn, dataDir, tmpDir string) (*stagedGeneration, error) {
	return stageConsistentGenerationWithProvider(ctx, dsn, dataDir, tmpDir, nil)
}

func stageConsistentGenerationWithProvider(ctx context.Context, dsn, dataDir, tmpDir string, keyProvider cloudworkspace.KeyProvider) (*stagedGeneration, error) {
	sourceDBPath := sqliteFilePath(dsn)
	absSourceDBPath, err := filepath.Abs(sourceDBPath)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite backup source: %w", err)
	}
	if info, statErr := os.Stat(absSourceDBPath); statErr != nil {
		return nil, fmt.Errorf("stat sqlite backup source: %w", statErr)
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sqlite backup source is not a regular file: %s", absSourceDBPath)
	}
	dbSnapshot := filepath.Join(tmpDir, filepath.Base(sourceDBPath))
	if filepath.Base(dbSnapshot) == "." || filepath.Base(dbSnapshot) == string(filepath.Separator) || filepath.Base(dbSnapshot) == "" {
		dbSnapshot = filepath.Join(tmpDir, "hub.db")
	}
	liveCloudRoot := filepath.Join(dataDir, "cloud-workspaces")
	stagedCloudRoot := filepath.Join(tmpDir, "cloud-workspaces")

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database for consistent backup: %w", err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve sqlite backup connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 60000`); err != nil {
		return nil, fmt.Errorf("configure sqlite backup timeout: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, fmt.Errorf("acquire cloud workspace backup write barrier: %w", err)
	}
	barrierStarted := time.Now()
	cutAt := barrierStarted.UTC()
	if consistentGenerationBarrierHook != nil {
		consistentGenerationBarrierHook()
	}
	barrierHeld := true
	defer func() {
		if barrierHeld {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	// SQLite's online-backup API cannot run from the same connection that owns
	// the write transaction. A second read connection observes the generation
	// frozen by the RESERVED writer lock above.
	snapshotConn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open sqlite snapshot connection: %w", err)
	}
	if err := onlineSQLiteBackup(snapshotConn, dbSnapshot); err != nil {
		_ = snapshotConn.Close()
		return nil, fmt.Errorf("create sqlite online backup: %w", err)
	}
	if err := snapshotConn.Close(); err != nil {
		return nil, fmt.Errorf("close sqlite snapshot connection: %w", err)
	}
	cloudRootExists := false
	if info, statErr := os.Stat(liveCloudRoot); statErr == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("cloud workspace root is not a directory: %s", liveCloudRoot)
		}
		cloudRootExists = true
		if err := copyTree(liveCloudRoot, stagedCloudRoot); err != nil {
			return nil, fmt.Errorf("stage cloud workspace files: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("stat cloud workspace root: %w", statErr)
	}
	if _, err := conn.ExecContext(ctx, `ROLLBACK`); err != nil {
		return nil, fmt.Errorf("release cloud workspace backup write barrier: %w", err)
	}
	barrierHeld = false
	writePauseMS := time.Since(barrierStarted).Milliseconds()

	snapshotDB, err := sql.Open("sqlite", dbSnapshot)
	if err != nil {
		return nil, fmt.Errorf("open staged sqlite backup: %w", err)
	}
	defer snapshotDB.Close()
	if err := sqliteQuickCheck(ctx, snapshotDB); err != nil {
		return nil, err
	}
	var report *cloudworkspace.RecoveryReport
	if keyProvider != nil {
		report, err = cloudworkspace.VerifyRecoveryWithProvider(ctx, snapshotDB, stagedCloudRoot, keyProvider)
	} else {
		report, err = cloudworkspace.VerifyRecovery(ctx, snapshotDB, stagedCloudRoot, stagedCloudRoot)
	}
	if err != nil {
		return nil, fmt.Errorf("verify staged cloud workspace generation: %w", err)
	}

	root := ""
	if cloudRootExists {
		root = stagedCloudRoot
	}
	generationID, err := newGenerationID()
	if err != nil {
		return nil, err
	}
	return &stagedGeneration{
		ID:                 generationID,
		Consistency:        cloudWorkspaceConsistency,
		DatabasePath:       dbSnapshot,
		CloudWorkspacePath: root,
		Report:             report,
		CutAt:              cutAt,
		WritePauseMS:       writePauseMS,
	}, nil
}

func onlineSQLiteBackup(conn *sql.Conn, destination string) error {
	type backuper interface {
		NewBackup(string) (*modernsqlite.Backup, error)
	}
	_ = os.Remove(destination)
	return conn.Raw(func(driverConn any) error {
		source, ok := driverConn.(backuper)
		if !ok {
			return fmt.Errorf("sqlite driver does not support online backup")
		}
		backup, err := source.NewBackup(destination)
		if err != nil {
			return err
		}
		finished := false
		defer func() {
			if !finished {
				_ = backup.Finish()
			}
		}()
		for more := true; more; {
			more, err = backup.Step(-1)
			if err != nil {
				return err
			}
		}
		if err := backup.Finish(); err != nil {
			return err
		}
		finished = true
		return nil
	})
}

func sqliteQuickCheck(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA quick_check`)
	if err != nil {
		return fmt.Errorf("run sqlite quick_check: %w", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(results) != 1 || !strings.EqualFold(strings.TrimSpace(results[0]), "ok") {
		return fmt.Errorf("sqlite quick_check failed: %s", strings.Join(results, "; "))
	}
	return nil
}

func newGenerationID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate backup id: %w", err)
	}
	return "hubdr_" + hex.EncodeToString(raw), nil
}

func cloudWorkspaceSummary(report *cloudworkspace.RecoveryReport) *CloudWorkspaceSummary {
	if report == nil {
		return nil
	}
	return &CloudWorkspaceSummary{
		Root:            "data/cloud-workspaces",
		Workspaces:      report.Workspaces,
		Objects:         report.Objects,
		VerifiedObjects: report.VerifiedObjects,
		Snapshots:       report.Snapshots,
		Sidecars:        report.Sidecars,
		Usage:           report.Usage,
		MasterKey:       report.MasterKey,
	}
}

func archiveDatabasePath(dataDir, dsn string) string {
	dbPath := sqliteFilePath(dsn)
	dbRel, err := filepath.Rel(dataDir, dbPath)
	if err != nil || dbRel == ".." || strings.HasPrefix(dbRel, ".."+string(os.PathSeparator)) || filepath.IsAbs(dbRel) {
		dbRel = filepath.Base(dbPath)
	}
	return filepath.ToSlash(filepath.Join("data", dbRel))
}

func sqliteFilePath(dsn string) string {
	path := strings.TrimSpace(dsn)
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	path = strings.TrimPrefix(path, "file:")
	return path
}

func addDirectory(add func(src, dst, kind string) error, sourceRoot, archiveRoot string, classify func(string) string) error {
	return addDirectoryFiltered(add, sourceRoot, archiveRoot, classify, func(string) bool { return true })
}

func addDirectoryFiltered(add func(src, dst, kind string) error, sourceRoot, archiveRoot string, classify func(string) string, include func(string) bool) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup source contains unsupported symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if include != nil && !include(rel) {
			return nil
		}
		return add(path, filepath.ToSlash(filepath.Join(archiveRoot, rel)), classify(rel))
	})
}

func classifyCloudWorkspaceEntry(rel string) string {
	rel = strings.ToLower(filepath.ToSlash(rel))
	switch {
	case filepath.Base(rel) == "master.key" || filepath.Base(rel) == "master-keyring.json":
		return "cloud_workspace_master_key"
	case strings.Contains(rel, "/sidecars/"):
		return "cloud_workspace_sidecar"
	case strings.Contains(rel, ".part/") || strings.Contains(rel, "/staging/"):
		return "cloud_workspace_staging"
	case strings.HasSuffix(rel, ".enc"):
		return "cloud_workspace_object"
	default:
		return "cloud_workspace_data"
	}
}

func copyTree(sourceRoot, destinationRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cloud workspace tree contains unsupported symlink: %s", path)
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(destinationRoot, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cloud workspace tree contains unsupported file: %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
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
