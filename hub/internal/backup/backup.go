package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	_ "modernc.org/sqlite"
)

const (
	ArchiveVersion = 2
	ManifestPath   = "manifest.json"
)

type CreateOptions struct {
	ConfigPath  string
	OutputPath  string
	IncludeLogs bool
	Now         time.Time
	// KeyProvider optionally supplies an external KMS/secret-backed provider.
	// When nil, Cloud Workspace selects its configured environment/file provider.
	KeyProvider cloudworkspace.KeyProvider
}

type RestoreOptions struct {
	ArchivePath string
	TargetRoot  string
	Force       bool
	DryRun      bool
	// KeyProvider optionally supplies the provider needed to decrypt an
	// environment/KMS-backed archive. It is never serialized into results.
	KeyProvider cloudworkspace.KeyProvider
}

type Manifest struct {
	Version        int                    `json:"version"`
	App            string                 `json:"app"`
	GenerationID   string                 `json:"generation_id,omitempty"`
	CreatedAt      string                 `json:"created_at"`
	CompletedAt    string                 `json:"completed_at,omitempty"`
	DurationMS     int64                  `json:"duration_ms,omitempty"`
	WritePauseMS   int64                  `json:"write_pause_ms,omitempty"`
	Consistency    string                 `json:"consistency,omitempty"`
	ConfigPath     string                 `json:"config_path,omitempty"`
	DatabaseDSN    string                 `json:"database_dsn"`
	DatabasePath   string                 `json:"database_path,omitempty"`
	DataDir        string                 `json:"data_dir"`
	IncludeLogs    bool                   `json:"include_logs"`
	CloudWorkspace *CloudWorkspaceSummary `json:"cloud_workspace,omitempty"`
	Entries        []Entry                `json:"entries"`
	Instructions   []string               `json:"instructions,omitempty"`
}

type Entry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type CloudWorkspaceSummary struct {
	Root            string                              `json:"root"`
	Workspaces      int                                 `json:"workspaces"`
	Objects         int                                 `json:"objects"`
	VerifiedObjects int                                 `json:"verified_objects"`
	Snapshots       int                                 `json:"snapshots"`
	Sidecars        int                                 `json:"sidecars"`
	Usage           cloudworkspace.RetainedUsage        `json:"usage"`
	MasterKey       *cloudworkspace.MasterKeyDescriptor `json:"master_key,omitempty"`
}

type CreateResult struct {
	ArchivePath string   `json:"archive_path"`
	Manifest    Manifest `json:"manifest"`
}

type RestoreResult struct {
	ArchivePath  string                 `json:"archive_path"`
	TargetRoot   string                 `json:"target_root"`
	GenerationID string                 `json:"generation_id,omitempty"`
	DryRun       bool                   `json:"dry_run"`
	CompletedAt  string                 `json:"completed_at,omitempty"`
	DurationMS   int64                  `json:"duration_ms,omitempty"`
	Verification *CloudWorkspaceSummary `json:"cloud_workspace_verification,omitempty"`
	Restored     []Entry                `json:"restored"`
	Skipped      []Entry                `json:"skipped,omitempty"`
}

func Create(ctx context.Context, cfg *config.Config, opts CreateOptions) (*CreateResult, error) {
	startedAt := time.Now()
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
	// Putting generated archives directly beside the live database would make
	// it impossible to skip the output directory without also skipping the
	// rest of the Hub data tree. Reject that layout instead of recursively
	// archiving prior generations on every run.
	if sameFilePath(filepath.Dir(absOut), dataDir) {
		return nil, fmt.Errorf("backup output directory must not be Hub data directory")
	}
	if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
		return nil, fmt.Errorf("create backup output dir: %w", err)
	}
	if _, statErr := os.Stat(absOut); statErr == nil {
		return nil, fmt.Errorf("backup output already exists: %s", absOut)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect backup output: %w", statErr)
	}

	tmpDir, err := os.MkdirTemp("", "hub-backup-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	generation, err := stageConsistentGenerationWithProvider(ctx, cfg.Database.DSN, dataDir, tmpDir, opts.KeyProvider)
	if err != nil {
		return nil, err
	}
	dbSnapshot := generation.DatabasePath
	dbArchivePath := archiveDatabasePath(dataDir, cfg.Database.DSN)

	manifest := Manifest{
		Version:        ArchiveVersion,
		App:            "MaClaw Hub",
		GenerationID:   generation.ID,
		CreatedAt:      generation.CutAt.UTC().Format(time.RFC3339Nano),
		WritePauseMS:   generation.WritePauseMS,
		Consistency:    generation.Consistency,
		ConfigPath:     cleanOptionalPath(opts.ConfigPath),
		DatabaseDSN:    cleanOptionalPath(cfg.Database.DSN),
		DatabasePath:   dbArchivePath,
		DataDir:        cleanOptionalPath(dataDir),
		IncludeLogs:    opts.IncludeLogs,
		CloudWorkspace: cloudWorkspaceSummary(generation.Report),
		Instructions: []string{
			"Stop hub before restore.",
			"Run a validated dry-run before replacing the target data generation.",
			"Run: hub restore --file <archive.tar.gz> --target-root <hub-dir> --dry-run",
			"Then run: hub restore --file <archive.tar.gz> --target-root <hub-dir> --force",
			"Start hub after restore and check /api/health.",
		},
	}

	file, err := os.CreateTemp(filepath.Dir(absOut), "."+filepath.Base(absOut)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary backup archive: %w", err)
	}
	tempArchivePath := file.Name()
	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)
	ok := false
	defer func() {
		if !ok {
			_ = tw.Close()
			_ = gw.Close()
			_ = file.Close()
			_ = os.Remove(tempArchivePath)
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
	if err := add(dbSnapshot, dbArchivePath, "sqlite_snapshot"); err != nil {
		return nil, err
	}
	if generation.CloudWorkspacePath != "" {
		archiveLocalKeyFiles := opts.KeyProvider == nil
		if opts.KeyProvider != nil {
			archiveLocalKeyFiles = isLocalCloudWorkspaceKeyProvider(opts.KeyProvider.ProviderName())
		}
		// Even an empty workspace may contain a stale key file from an older
		// file-provider deployment. The presence of an environment secret is
		// enough to classify the current provider; do not wait for a referenced
		// object before deciding whether local key files are safe to archive.
		if strings.TrimSpace(os.Getenv("MACLAW_CWS_KEYRING")) != "" || strings.TrimSpace(os.Getenv("MACLAW_CWS_MASTER_KEY")) != "" {
			archiveLocalKeyFiles = false
		}
		if generation.Report != nil && generation.Report.MasterKey != nil {
			providerName := strings.ToLower(strings.TrimSpace(generation.Report.MasterKey.Provider))
			// Non-file providers (environment, KMS, or a custom implementation)
			// must never leak a stale local key copy that happens to be left under
			// cloud-workspaces. Their recovery secret is supplied out-of-band.
			archiveLocalKeyFiles = isLocalCloudWorkspaceKeyProvider(providerName)
		}
		if err := addDirectoryFiltered(add, generation.CloudWorkspacePath, "data/cloud-workspaces", classifyCloudWorkspaceEntry, func(rel string) bool {
			if archiveLocalKeyFiles {
				return true
			}
			base := strings.ToLower(filepath.Base(filepath.ToSlash(rel)))
			return base != "master.key" && base != "master-keyring.json"
		}); err != nil {
			return nil, err
		}
	}
	if err := addExtraFile(add, dataDir, cfg.TLS.CertFile, "tls_certificate"); err != nil {
		return nil, err
	}
	if err := addExtraFile(add, dataDir, cfg.TLS.KeyFile, "tls_private_key"); err != nil {
		return nil, err
	}
	if err := addDataDir(tw, dataDir, cfg.Database.DSN, tempArchivePath, filepath.Join(dataDir, "cloud-workspaces"), opts.IncludeLogs, &manifest); err != nil {
		return nil, err
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path })
	manifest.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	manifest.DurationMS = time.Since(startedAt).Milliseconds()
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
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync backup file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close backup file: %w", err)
	}
	// Publish with an atomic no-overwrite primitive. Two concurrent backup
	// invocations may race on the same timestamp; a plain rename would replace
	// the first completed archive on POSIX.
	if err := os.Link(tempArchivePath, absOut); err != nil {
		return nil, fmt.Errorf("publish backup archive: %w", err)
	}
	_ = os.Remove(tempArchivePath)
	ok = true
	return &CreateResult{ArchivePath: absOut, Manifest: manifest}, nil
}

func isLocalCloudWorkspaceKeyProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "file-keyring" || provider == "file-legacy"
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
		if manifest.Version < 1 || manifest.Version > ArchiveVersion {
			return nil, fmt.Errorf("unsupported backup version %d", manifest.Version)
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("manifest not found in backup archive")
}

func Restore(opts RestoreOptions) (*RestoreResult, error) {
	return restoreValidated(opts)
}

func defaultArchiveName(now time.Time) string {
	return fmt.Sprintf("maclaw-hub-backup-%s.tar.gz", now.Format("2006-01-02-150405"))
}

func resolveDataDir(dsn string) (string, error) {
	dbPath := sqliteFilePath(dsn)
	if strings.TrimSpace(dbPath) == "" || dbPath == ":memory:" || strings.HasPrefix(dbPath, ":memory:") {
		return "", fmt.Errorf("sqlite file database dsn is required for backup")
	}
	absDSN, err := filepath.Abs(dbPath)
	if err != nil {
		return "", fmt.Errorf("resolve database dsn: %w", err)
	}
	return filepath.Dir(absDSN), nil
}

func snapshotSQLite(ctx context.Context, dsn, tmpDir string) (string, error) {
	dbPath := sqliteFilePath(dsn)
	absDSN, err := filepath.Abs(dbPath)
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

func addExtraFile(add func(src, dst, kind string) error, dataDir, path, kind string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", kind, err)
	}
	info, err := os.Stat(absPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", kind, err)
	}
	if info.IsDir() {
		return nil
	}
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	if isWithin(absDataDir, absPath) {
		return nil
	}
	return add(absPath, filepath.ToSlash(filepath.Join("external", kind, filepath.Base(absPath))), kind)
}
func addDataDir(tw *tar.Writer, dataDir, dbDSN, outputPath, cloudWorkspaceRoot string, includeLogs bool, manifest *Manifest) error {
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	absDB, err := filepath.Abs(sqliteFilePath(dbDSN))
	if err != nil {
		return fmt.Errorf("resolve database dsn: %w", err)
	}
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve backup output path: %w", err)
	}
	absOutputDir := filepath.Dir(absOutput)
	outputDirInsideDataDir := !sameFilePath(absOutputDir, absDataDir) && isWithin(absDataDir, absOutputDir)
	absCloudWorkspaceRoot, _ := filepath.Abs(cloudWorkspaceRoot)
	return filepath.WalkDir(absDataDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			absPath, absErr := filepath.Abs(path)
			if absErr == nil && sameFilePath(absPath, absCloudWorkspaceRoot) {
				return filepath.SkipDir
			}
			// Never archive the directory that contains the output archive.
			// Prior generations may be kept beside the current archive; including
			// them would recursively inflate every new backup.
			if absErr == nil && outputDirInsideDataDir && sameFilePath(absPath, absOutputDir) {
				return filepath.SkipDir
			}
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if sameFilePath(absPath, absOutput) || (outputDirInsideDataDir && isWithin(absOutputDir, absPath)) || sameFilePath(absPath, absDB) || sameFilePath(absPath, absDB+"-wal") || sameFilePath(absPath, absDB+"-shm") || sameFilePath(absPath, absDB+"-journal") {
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
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tw, hasher), f); err != nil {
		return Entry{}, fmt.Errorf("write archive entry %s: %w", dst, err)
	}
	return Entry{Path: filepath.ToSlash(dst), Kind: kind, Size: info.Size(), SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
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
