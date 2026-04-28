package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	_ "modernc.org/sqlite"
)

const (
	ArchiveVersion = 1
	ManifestPath   = "manifest.json"
)

type CreateOptions struct {
	ConfigPath  string
	OutputPath  string
	IncludeLogs bool
	Now         time.Time
}

type RestoreOptions struct {
	ArchivePath string
	TargetRoot  string
	Force       bool
	DryRun      bool
}

type Manifest struct {
	Version      int      `json:"version"`
	App          string   `json:"app"`
	CreatedAt    string   `json:"created_at"`
	ConfigPath   string   `json:"config_path,omitempty"`
	DatabaseDSN  string   `json:"database_dsn"`
	DataDir      string   `json:"data_dir"`
	IncludeLogs  bool     `json:"include_logs"`
	Entries      []Entry  `json:"entries"`
	Instructions []string `json:"instructions,omitempty"`
}

type Entry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
}

type CreateResult struct {
	ArchivePath string   `json:"archive_path"`
	Manifest    Manifest `json:"manifest"`
}

type RestoreResult struct {
	ArchivePath string  `json:"archive_path"`
	TargetRoot  string  `json:"target_root"`
	DryRun      bool    `json:"dry_run"`
	Restored    []Entry `json:"restored"`
	Skipped     []Entry `json:"skipped,omitempty"`
}

func Create(ctx context.Context, cfg *config.Config, opts CreateOptions) (*CreateResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if strings.TrimSpace(opts.OutputPath) == "" {
		opts.OutputPath = defaultArchiveName(opts.Now)
	}
	absOut, err := filepath.Abs(opts.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("resolve output path: %w", err)
	}
	dataDir, err := resolveDataDir(cfg.Database.DSN)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
		return nil, fmt.Errorf("create backup output dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "hubcenter-backup-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dbSnapshot, err := snapshotSQLite(ctx, cfg.Database.DSN, tmpDir)
	if err != nil {
		return nil, err
	}

	manifest := Manifest{
		Version:     ArchiveVersion,
		App:         "MaClaw Hub Center",
		CreatedAt:   opts.Now.UTC().Format(time.RFC3339),
		ConfigPath:  cleanOptionalPath(opts.ConfigPath),
		DatabaseDSN: cleanOptionalPath(cfg.Database.DSN),
		DataDir:     cleanOptionalPath(dataDir),
		IncludeLogs: opts.IncludeLogs,
		Instructions: []string{
			"Stop hubcenter before restore.",
			"Run: hubcenter restore --file <archive.tar.gz> --target-root <hubcenter-dir> --force",
			"Start hubcenter after restore and check /api/health.",
		},
	}

	file, err := os.Create(absOut)
	if err != nil {
		return nil, fmt.Errorf("create backup archive: %w", err)
	}
	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)
	ok := false
	defer func() {
		if !ok {
			_ = tw.Close()
			_ = gw.Close()
			_ = file.Close()
			_ = os.Remove(absOut)
		}
	}()

	add := func(src, dst, kind string) error {
		entry, addErr := addFile(tw, src, dst, kind)
		if addErr != nil {
			return addErr
		}
		manifest.Entries = append(manifest.Entries, entry)
		return nil
	}

	if opts.ConfigPath != "" {
		if err := add(opts.ConfigPath, archiveConfigPath(opts.ConfigPath), "config"); err != nil {
			return nil, err
		}
	}
	dbRel, err := filepath.Rel(dataDir, cfg.Database.DSN)
	if err != nil || strings.HasPrefix(dbRel, "..") || filepath.IsAbs(dbRel) {
		dbRel = filepath.Base(cfg.Database.DSN)
	}
	if err := add(dbSnapshot, filepath.ToSlash(filepath.Join("data", dbRel)), "sqlite_snapshot"); err != nil {
		return nil, err
	}
	if err := addDataDir(tw, dataDir, cfg.Database.DSN, absOut, opts.IncludeLogs, &manifest); err != nil {
		return nil, err
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path })
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := addBytes(tw, ManifestPath, manifestData); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close backup archive: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close backup gzip stream: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close backup file: %w", err)
	}
	ok = true
	return &CreateResult{ArchivePath: absOut, Manifest: manifest}, nil
}

func Inspect(archivePath string) (*Manifest, error) {
	file, gr, tr, err := openTarGzip(archivePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	defer gr.Close()

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read backup archive: %w", err)
		}
		if header.Name != ManifestPath {
			continue
		}
		var manifest Manifest
		if err := json.NewDecoder(tr).Decode(&manifest); err != nil {
			return nil, fmt.Errorf("decode manifest: %w", err)
		}
		if manifest.Version != ArchiveVersion {
			return nil, fmt.Errorf("unsupported backup version %d", manifest.Version)
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("manifest not found in backup archive")
}

func Restore(opts RestoreOptions) (*RestoreResult, error) {
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
	file, gr, tr, err := openTarGzip(opts.ArchivePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	defer gr.Close()
	manifest, err := Inspect(opts.ArchivePath)
	if err != nil {
		return nil, err
	}

	result := &RestoreResult{ArchivePath: opts.ArchivePath, TargetRoot: absRoot, DryRun: opts.DryRun}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read backup archive: %w", err)
		}
		if header.Name == ManifestPath || header.Typeflag == tar.TypeDir {
			continue
		}
		if err := validateArchivePath(header.Name); err != nil {
			return nil, err
		}
		dst := filepath.Join(absRoot, filepath.FromSlash(header.Name))
		if !isWithin(absRoot, dst) {
			return nil, fmt.Errorf("archive entry escapes target root: %s", header.Name)
		}
		entry := Entry{Path: header.Name, Kind: entryKind(manifest, header.Name), Size: header.Size}
		if exists(dst) && !opts.Force {
			result.Skipped = append(result.Skipped, entry)
			continue
		}
		result.Restored = append(result.Restored, entry)
		if opts.DryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("create restore dir: %w", err)
		}
		if err := extractFile(header, tr, dst); err != nil {
			return nil, err
		}
	}
	if len(result.Skipped) > 0 && !opts.Force && !opts.DryRun {
		return result, fmt.Errorf("restore would overwrite %d existing files; rerun with --force after stopping hubcenter", len(result.Skipped))
	}
	return result, nil
}

func defaultArchiveName(now time.Time) string {
	return fmt.Sprintf("maclaw-hubcenter-backup-%s.tar.gz", now.Format("2006-01-02-150405"))
}

func resolveDataDir(dsn string) (string, error) {
	if strings.TrimSpace(dsn) == "" || dsn == ":memory:" {
		return "", fmt.Errorf("sqlite file database dsn is required for backup")
	}
	absDSN, err := filepath.Abs(dsn)
	if err != nil {
		return "", fmt.Errorf("resolve database dsn: %w", err)
	}
	return filepath.Dir(absDSN), nil
}

func snapshotSQLite(ctx context.Context, dsn, tmpDir string) (string, error) {
	absDSN, err := filepath.Abs(dsn)
	if err != nil {
		return "", fmt.Errorf("resolve database dsn: %w", err)
	}
	if _, err := os.Stat(absDSN); err != nil {
		return "", fmt.Errorf("stat sqlite database: %w", err)
	}
	snapshot := filepath.Join(tmpDir, filepath.Base(absDSN))
	db, err := sql.Open("sqlite", absDSN)
	if err != nil {
		return "", fmt.Errorf("open sqlite database for backup: %w", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	quoted := strings.ReplaceAll(snapshot, "'", "''")
	if _, err := db.ExecContext(ctx, "VACUUM INTO '"+quoted+"'"); err != nil {
		return "", fmt.Errorf("create sqlite backup snapshot: %w", err)
	}
	return snapshot, nil
}

func addDataDir(tw *tar.Writer, dataDir, dbDSN, outputPath string, includeLogs bool, manifest *Manifest) error {
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	absDB, err := filepath.Abs(dbDSN)
	if err != nil {
		return fmt.Errorf("resolve database dsn: %w", err)
	}
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve backup output path: %w", err)
	}
	return filepath.WalkDir(absDataDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if sameFilePath(absPath, absOutput) || sameFilePath(absPath, absDB) || sameFilePath(absPath, absDB+"-wal") || sameFilePath(absPath, absDB+"-shm") {
			return nil
		}
		if !includeLogs && isLogFile(absPath) {
			return nil
		}
		rel, err := filepath.Rel(absDataDir, absPath)
		if err != nil {
			return err
		}
		entry, err := addFile(tw, absPath, filepath.ToSlash(filepath.Join("data", rel)), classifyDataEntry(rel))
		if err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, entry)
		return nil
	})
}

func classifyDataEntry(rel string) string {
	base := strings.ToLower(filepath.Base(rel))
	relSlash := strings.ToLower(filepath.ToSlash(rel))
	switch {
	case strings.HasSuffix(base, ".pem") || strings.Contains(base, "cert") || strings.Contains(base, "key"):
		return "certificate"
	case strings.HasPrefix(relSlash, "skills/"):
		return "skill"
	case strings.HasPrefix(relSlash, "sm_pending/") || strings.HasPrefix(relSlash, "sm_sandbox/"):
		return "skillmarket_workspace"
	case strings.Contains(base, "gossip"):
		return "gossip_cache"
	case isLogFile(rel):
		return "log"
	default:
		return "data"
	}
}

func isLogFile(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	return strings.HasSuffix(lower, ".log") || strings.Contains(lower, "/logs/")
}

func addFile(tw *tar.Writer, src, dst, kind string) (Entry, error) {
	info, err := os.Stat(src)
	if err != nil {
		return Entry{}, fmt.Errorf("stat %s: %w", src, err)
	}
	if info.IsDir() {
		return Entry{}, fmt.Errorf("cannot add directory as file: %s", src)
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return Entry{}, fmt.Errorf("create tar header: %w", err)
	}
	header.Name = filepath.ToSlash(dst)
	if err := tw.WriteHeader(header); err != nil {
		return Entry{}, fmt.Errorf("create archive entry %s: %w", dst, err)
	}
	f, err := os.Open(src)
	if err != nil {
		return Entry{}, fmt.Errorf("open %s: %w", src, err)
	}
	defer f.Close()
	if _, err := io.Copy(tw, f); err != nil {
		return Entry{}, fmt.Errorf("write archive entry %s: %w", dst, err)
	}
	return Entry{Path: filepath.ToSlash(dst), Kind: kind, Size: info.Size()}, nil
}

func addBytes(tw *tar.Writer, dst string, data []byte) error {
	header := &tar.Header{Name: filepath.ToSlash(dst), Mode: 0o644, Size: int64(len(data)), ModTime: time.Now()}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("create archive entry %s: %w", dst, err)
	}
	_, err := tw.Write(data)
	return err
}

func validateArchivePath(name string) error {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "..") {
		return fmt.Errorf("unsafe archive entry path: %s", name)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if clean != name {
		return fmt.Errorf("unclean archive entry path: %s", name)
	}
	return nil
}

func extractFile(header *tar.Header, r io.Reader, dst string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode())
	if err != nil {
		return fmt.Errorf("create restored file %s: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, r); err != nil {
		return fmt.Errorf("restore archive entry %s: %w", header.Name, err)
	}
	return nil
}

func openTarGzip(path string) (*os.File, *gzip.Reader, *tar.Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open backup archive: %w", err)
	}
	gr, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, nil, nil, fmt.Errorf("open gzip stream: %w", err)
	}
	return file, gr, tar.NewReader(gr), nil
}

func entryKind(manifest *Manifest, path string) string {
	if manifest == nil {
		return "unknown"
	}
	for _, entry := range manifest.Entries {
		if entry.Path == path {
			return entry.Kind
		}
	}
	return "unknown"
}

func sameFilePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func cleanOptionalPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func archiveConfigPath(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			if rel, relErr := filepath.Rel(cwd, abs); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return filepath.ToSlash(filepath.Clean(rel))
			}
		}
	}
	return filepath.ToSlash(filepath.Join("config", filepath.Base(path)))
}
