package structureddata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const backupMetaExt = ".json"

func (s *SQLiteStore) CreateBackup(ctx context.Context, in CreateBackupInput, actor string, now time.Time) (*BackupInfo, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: sqlite store is not open", ErrInvalidInput)
	}
	if err := os.MkdirAll(s.backupDir, 0o700); err != nil {
		return nil, err
	}
	id := backupID(now)
	backupPath := filepath.Join(s.backupDir, id+".db")
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM main INTO `+quoteSQLiteString(backupPath)); err != nil {
		return nil, err
	}
	stat, err := os.Stat(backupPath)
	if err != nil {
		return nil, err
	}
	checksum, err := fileSHA256(backupPath)
	if err != nil {
		return nil, err
	}
	info := BackupInfo{ID: id, Name: strings.TrimSpace(in.Name), Note: strings.TrimSpace(in.Note), Engine: "sqlite", Path: backupPath, SizeBytes: stat.Size(), SHA256: checksum, DownloadURL: backupDownloadPath(id), CreatedBy: strings.TrimSpace(actor), CreatedAt: now}
	if err := writeBackupMeta(s.backupMetaPath(id), info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (s *SQLiteStore) ListBackups(ctx context.Context, in QueryBackupsInput) ([]BackupInfo, error) {
	_ = ctx
	entries, err := os.ReadDir(s.backupDir)
	if os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []BackupInfo{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), backupMetaExt) {
			continue
		}
		info, err := readBackupMeta(filepath.Join(s.backupDir, entry.Name()))
		if err != nil {
			continue
		}
		if stat, err := os.Stat(info.Path); err == nil {
			info.SizeBytes = stat.Size()
		}
		if info.SHA256 == "" {
			info.SHA256, _ = fileSHA256(info.Path)
		}
		info.DownloadURL = backupDownloadPath(info.ID)
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		filtered := out[:0]
		for _, info := range out {
			createdAt := info.CreatedAt.Format(time.RFC3339Nano)
			if createdAt < before || (beforeID != "" && createdAt == before && info.ID < beforeID) {
				filtered = append(filtered, info)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *SQLiteStore) GetBackup(ctx context.Context, backupID string) (*BackupInfo, error) {
	_ = ctx
	info, err := s.readBackupInfo(backupID)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (s *SQLiteStore) ReadBackup(ctx context.Context, backupID string) ([]byte, *BackupInfo, error) {
	_ = ctx
	info, err := s.readBackupInfo(backupID)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(info.Path)
	if err != nil {
		return nil, nil, err
	}
	return data, info, nil
}

func (s *SQLiteStore) RestoreBackup(ctx context.Context, backupID string, in RestoreBackupInput, actor string, now time.Time) (*RestoreResult, error) {
	if !in.Confirm {
		return nil, fmt.Errorf("%w: restore requires confirm=true", ErrInvalidInput)
	}
	backupID = strings.TrimSpace(backupID)
	if backupID == "" || strings.ContainsAny(backupID, `/\\`) {
		return nil, fmt.Errorf("%w: invalid backup id", ErrInvalidInput)
	}
	info, err := readBackupMeta(s.backupMetaPath(backupID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBackupNotFound
		}
		return nil, err
	}
	if info.Engine != "sqlite" {
		return nil, fmt.Errorf("%w: backup engine is not sqlite", ErrInvalidInput)
	}
	if _, err := os.Stat(info.Path); err != nil {
		return nil, err
	}
	if s.db != nil {
		if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return nil, err
		}
		if err := s.db.Close(); err != nil {
			return nil, err
		}
		s.db = nil
	}
	if err := removeSQLiteSidecars(s.path); err != nil {
		return nil, err
	}
	rollbackPath := s.path + ".before-restore-" + now.Format("20060102T150405Z")
	if _, err := os.Stat(s.path); err == nil {
		_ = os.Remove(rollbackPath)
		if err := os.Rename(s.path, rollbackPath); err != nil {
			return nil, err
		}
	}
	tmpPath := s.path + ".restore.tmp"
	_ = os.Remove(tmpPath)
	if err := copyFile(info.Path, tmpPath); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s.db = db
	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		s.db = nil
		return nil, err
	}
	return &RestoreResult{Status: "restored", Backup: info, RestoredBy: strings.TrimSpace(actor), RestoredAt: now}, nil
}

func (s *SQLiteStore) readBackupInfo(backupID string) (*BackupInfo, error) {
	backupID = strings.TrimSpace(backupID)
	if backupID == "" || strings.ContainsAny(backupID, `/\\`) {
		return nil, fmt.Errorf("%w: invalid backup id", ErrInvalidInput)
	}
	info, err := readBackupMeta(s.backupMetaPath(backupID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBackupNotFound
		}
		return nil, err
	}
	if info.Engine != "sqlite" {
		return nil, fmt.Errorf("%w: backup engine is not sqlite", ErrInvalidInput)
	}
	stat, err := os.Stat(info.Path)
	if err != nil {
		return nil, err
	}
	info.SizeBytes = stat.Size()
	checksum, err := fileSHA256(info.Path)
	if err != nil {
		return nil, err
	}
	info.SHA256 = checksum
	info.DownloadURL = backupDownloadPath(info.ID)
	return &info, nil
}

func (s *SQLiteStore) backupMetaPath(id string) string {
	return filepath.Join(s.backupDir, id+backupMetaExt)
}

func backupID(now time.Time) string {
	return "backup_" + now.UTC().Format("20060102T150405.000000000Z")
}

func quoteSQLiteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func writeBackupMeta(path string, info BackupInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func readBackupMeta(path string) (BackupInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BackupInfo{}, err
	}
	var info BackupInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return BackupInfo{}, err
	}
	return info, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func backupDownloadPath(backupID string) string {
	if strings.TrimSpace(backupID) == "" {
		return ""
	}
	return "/api/v1/data/backups/" + strings.TrimSpace(backupID) + "/download"
}

func removeSQLiteSidecars(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
