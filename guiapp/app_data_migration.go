package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	systemruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"

	"github.com/RapidAI/CodeClaw/guiapp/wruntime"
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

// codingAuthorizationMigrationFiles are the two SQLite files that together
// form the durable audit history of the desktop coding authorization chain.
// Live authority is deliberately not migrated: data-dir teardown revokes
// active handles and interrupts queued/running attempts before these files
// are copied, so the destination receives history only, never a live token.
var codingAuthorizationMigrationFiles = []string{
	"coding_runtime.db",
	"coding_task_relations.db",
}

const codingAuthorizationMigrationMarker = ".coding_authorization_migration.json"

const (
	dataDirMigrationJournalName    = ".maclaw_data_migration.json"
	dataDirMigrationJournalVersion = 1
	dataDirMigrationTempSuffix     = ".maclaw-migration-tmp"
)

type dataDirMigrationFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type dataDirMigrationJournal struct {
	Version   int                    `json:"version"`
	SourceDir string                 `json:"source_dir"`
	Files     []dataDirMigrationFile `json:"files"`
	StartedAt int64                  `json:"started_at"`
}

type dataMigrationTargetNotEmptyError struct {
	path string
}

func (e dataMigrationTargetNotEmptyError) Error() string {
	return fmt.Sprintf("target data directory already contains independent data at %s", e.path)
}

type codingAuthorizationMigrationState struct {
	SourceDir string   `json:"source_dir,omitempty"`
	Files     []string `json:"files"`
}

type codingAuthorizationMigrationRecovery struct {
	sourceDir string
	files     []string
	complete  bool
}

// codingAuthorizationMigrationMu serializes recovery/resume with reset-time
// migration. Runtime and relation opens have separate lifecycle locks; both
// may be the first caller to notice an interrupted target, so the file-level
// migration itself needs one process-local serialization point.
var codingAuthorizationMigrationMu sync.Mutex

// sameCodingDataDirPath compares local filesystem paths with the platform's
// case semantics. filepath.Clean alone treats a Windows case-only edit as a
// different directory, which would hard-cancel work and "migrate" a ledger
// onto itself even though the physical data-dir epoch never changed.
func sameCodingDataDirPath(left, right string) bool {
	left = filepath.Clean(strings.TrimSpace(left))
	right = filepath.Clean(strings.TrimSpace(right))
	if left == "" || left == "." || right == "" || right == "." {
		return false
	}
	return pathsEqual(left, right)
}

func canonicalCodingDataDirKey(dir string) string {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if systemruntime.GOOS == "windows" {
		return strings.ToUpper(dir)
	}
	return dir
}

func migrateCodingAuthorizationStoresSerialized(sourceDir, targetDir string) (bool, error) {
	codingAuthorizationMigrationMu.Lock()
	defer codingAuthorizationMigrationMu.Unlock()
	return migrateCodingAuthorizationStoresForDataDirChange(sourceDir, targetDir)
}

// resumeCodingAuthorizationMigrationIfPending retries an interrupted migration
// using the source recorded in its marker. It is safe after the reset commit:
// the old epoch has already been revoked/interrupted, and after a process crash
// there are no live writers left by construction.
func resumeCodingAuthorizationMigrationIfPending(dataDir string) error {
	if !codingAuthorizationMigrationPending(dataDir) {
		return nil
	}
	if _, err := migrateCodingAuthorizationStoresSerialized("", dataDir); err != nil {
		return err
	}
	if codingAuthorizationMigrationPending(dataDir) {
		return fmt.Errorf("coding authorization migration is still pending in %q", dataDir)
	}
	return nil
}

func codingAuthorizationMigrationTempName(name string) string {
	return ".migrating-" + name
}

func sqliteMigrationSidecarNames(base string) []string {
	return []string{base + "-wal", base + "-shm", base + "-journal"}
}

func codingAuthorizationMigrationTempArtifactNames(name string) []string {
	tmp := codingAuthorizationMigrationTempName(name)
	out := []string{tmp}
	out = append(out, sqliteMigrationSidecarNames(tmp)...)
	return out
}

func codingAuthorizationMigrationFinalArtifactNames(name string) []string {
	out := []string{name}
	out = append(out, sqliteMigrationSidecarNames(name)...)
	return out
}

func sqliteMigrationStringLiteral(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "''") + "'"
}

func vacuumSQLiteDatabaseInto(srcPath, dstPath string) error {
	db, err := sql.Open("sqlite", srcPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	_, err = db.Exec(`VACUUM INTO ` + sqliteMigrationStringLiteral(dstPath))
	return err
}

func writeMigrationFileAtomic(path string, payload []byte) error {
	// Migration markers are recovery journals. Write through a temporary file
	// and rename so a crash cannot leave a torn JSON object that recovery would
	// have to treat as unrecoverable evidence.
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	var writeErr error
	if _, err = f.Write(payload); err != nil {
		writeErr = err
	} else if err = f.Sync(); err != nil {
		writeErr = err
	}
	if err = f.Close(); err != nil && writeErr == nil {
		writeErr = err
	}
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return writeErr
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func writeCodingAuthorizationMigrationMarker(path string, state codingAuthorizationMigrationState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeMigrationFileAtomic(path, payload)
}

// recoverCodingAuthorizationMigration completes or discards an interrupted
// migration using only evidence written by this migration protocol. Migration
// never starts when either managed destination DB already exists, so an
// expected final file created while the marker exists is known to be migration
// output and may be discarded safely. Recovery therefore returns the destination to
// exactly one of two states: all expected files installed, or none installed.
// For a partial migration it also returns the original source facts so a later
// process can resume from the real source instead of guessing from config.
func recoverCodingAuthorizationMigration(targetDir string) (codingAuthorizationMigrationRecovery, error) {
	var recovery codingAuthorizationMigrationRecovery
	markerPath := filepath.Join(targetDir, codingAuthorizationMigrationMarker)
	payload, err := os.ReadFile(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		// Temporary names are private to this protocol. If a crash happened
		// before the marker rename committed, copying had not started yet, so the
		// unrenamed journal temp is safe to discard.
		if err := os.Remove(markerPath + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return recovery, err
		}
		for _, name := range codingAuthorizationMigrationFiles {
			for _, artifact := range codingAuthorizationMigrationTempArtifactNames(name) {
				if err := os.Remove(filepath.Join(targetDir, artifact)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return recovery, err
				}
			}
		}
		return recovery, nil
	}
	if err != nil {
		return recovery, err
	}
	var state codingAuthorizationMigrationState
	if err = json.Unmarshal(payload, &state); err != nil {
		return recovery, fmt.Errorf("invalid coding authorization migration marker: %w", err)
	}
	if len(state.Files) == 0 {
		return recovery, fmt.Errorf("invalid coding authorization migration marker: empty file list")
	}
	expected := make(map[string]struct{}, len(state.Files))
	for _, name := range state.Files {
		valid := false
		for _, managed := range codingAuthorizationMigrationFiles {
			if name == managed {
				valid = true
				break
			}
		}
		if !valid {
			return recovery, fmt.Errorf("invalid coding authorization migration marker: unexpected file %q", name)
		}
		if _, duplicate := expected[name]; duplicate {
			return recovery, fmt.Errorf("invalid coding authorization migration marker: duplicate file %q", name)
		}
		expected[name] = struct{}{}
	}
	// A validated marker is authoritative. A leftover unrenamed marker temp is
	// redundant private output from the atomic journal write, not independent
	// destination evidence.
	if err := os.Remove(markerPath + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return recovery, err
	}
	// The marker is authoritative for the migration file set. A managed final
	// or temporary file outside that set cannot be migration output from this
	// journal; deleting it could destroy independent destination history, and
	// ignoring it could mix two ledgers. Fail closed and preserve the evidence.
	for _, name := range codingAuthorizationMigrationFiles {
		if _, ok := expected[name]; ok {
			continue
		}
		artifacts := append(codingAuthorizationMigrationFinalArtifactNames(name), codingAuthorizationMigrationTempArtifactNames(name)...)
		for _, artifact := range artifacts {
			path := filepath.Join(targetDir, artifact)
			if _, err := os.Stat(path); err == nil {
				return recovery, fmt.Errorf("interrupted coding migration has unexpected artifact %s", artifact)
			} else if !errors.Is(err, os.ErrNotExist) {
				return recovery, err
			}
		}
	}
	recovery.sourceDir = filepath.Clean(strings.TrimSpace(state.SourceDir))
	recovery.files = append([]string(nil), state.Files...)
	complete := true
	for _, name := range state.Files {
		// A final WAL/SHM/journal artifact is never output of the install phase:
		// VACUUM INTO finishes its temporary database before the rename. Treat one
		// as foreign evidence rather than deleting or merging it.
		for _, sidecar := range sqliteMigrationSidecarNames(name) {
			if _, err := os.Stat(filepath.Join(targetDir, sidecar)); err == nil {
				return recovery, fmt.Errorf("interrupted coding migration has unexpected artifact %s", sidecar)
			} else if !errors.Is(err, os.ErrNotExist) {
				return recovery, err
			}
		}
		if _, err := os.Stat(filepath.Join(targetDir, name)); err != nil {
			complete = false
			break
		}
		tempMissing := true
		for _, artifact := range codingAuthorizationMigrationTempArtifactNames(name) {
			if _, err := os.Stat(filepath.Join(targetDir, artifact)); err == nil {
				tempMissing = false
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return recovery, err
			}
		}
		if !tempMissing {
			complete = false
			break
		}
	}
	if complete {
		recovery.complete = true
		return recovery, os.Remove(markerPath)
	}
	// Discard partial migration output. Keep the marker in place: the caller
	// immediately rewrites it before retrying, and leaving it avoids a crash
	// window in which neither complete output nor recovery metadata exists.
	for _, name := range state.Files {
		artifacts := append([]string{name}, codingAuthorizationMigrationTempArtifactNames(name)...)
		for _, artifact := range artifacts {
			if err := os.Remove(filepath.Join(targetDir, artifact)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return recovery, err
			}
		}
	}
	return recovery, nil
}

func codingAuthorizationMigrationPending(dataDir string) bool {
	if strings.TrimSpace(dataDir) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(filepath.Clean(dataDir), codingAuthorizationMigrationMarker))
	// Existence is not the only unsafe answer: if the marker cannot be stat'ed,
	// the recovery state is unknown and opening the ledger normally would be
	// optimistic rather than fail-closed.
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func quiesceCodingAuthorizationMigrationSource(sourceDir string, files []string) error {
	for _, name := range files {
		path := filepath.Join(sourceDir, name)
		switch name {
		case "coding_runtime.db":
			store, err := codingruntime.NewSQLiteStore(path)
			if err != nil {
				return err
			}
			_, interruptErr := store.InterruptRunsForReplacement(time.Now().UTC())
			closeErr := store.Close()
			if interruptErr != nil {
				return interruptErr
			}
			if closeErr != nil {
				return closeErr
			}
		case "coding_task_relations.db":
			service, err := newCodingTaskRelationService(path)
			if err != nil {
				return err
			}
			if err := service.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateCodingAuthorizationStoresForDataDirChange copies the quiesced coding
// runtime/relation databases into the replacement data directory. It uses
// SQLite's VACUUM INTO rather than byte-copying live files, then installs both
// outputs under a migration marker. If any destination database already
// exists, that destination history is preserved and source history is not
// merged; merging two independently valid ledgers would be ambiguous.
func migrateCodingAuthorizationStoresForDataDirChange(sourceDir, targetDir string) (bool, error) {
	return migrateCodingAuthorizationStoresForDataDirChangeSeen(sourceDir, targetDir, nil)
}

func migrateCodingAuthorizationStoresForDataDirChangeSeen(sourceDir, targetDir string, activeTargets map[string]bool) (bool, error) {
	sourceDir = filepath.Clean(strings.TrimSpace(sourceDir))
	targetDir = filepath.Clean(strings.TrimSpace(targetDir))
	if targetDir == "" || targetDir == "." {
		return false, nil
	}
	if activeTargets == nil {
		activeTargets = make(map[string]bool)
	}
	targetKey := canonicalCodingDataDirKey(targetDir)
	if activeTargets[targetKey] {
		return false, fmt.Errorf("cyclic coding authorization migration chain at %q", targetDir)
	}
	activeTargets[targetKey] = true
	defer delete(activeTargets, targetKey)
	// Recovery must run even when the caller does not know a fallback source:
	// the migration marker is the authoritative record of an interrupted copy.
	recovery, err := recoverCodingAuthorizationMigration(targetDir)
	if err != nil {
		return false, fmt.Errorf("recover interrupted coding migration: %w", err)
	}
	if recovery.complete {
		return false, nil
	}
	if recovery.sourceDir != "" && recovery.sourceDir != "." {
		// A crashed migration is the authoritative evidence of its real source.
		// Config only knows the newest target and can otherwise guess wrong after
		// a restart (especially for a custom-dir to custom-dir switch).
		sourceDir = recovery.sourceDir
	}
	if len(recovery.files) != 0 && (sourceDir == "" || sourceDir == ".") {
		return false, fmt.Errorf("interrupted coding migration does not record its source directory")
	}
	if sourceDir == "" || sourceDir == "." || sameCodingDataDirPath(sourceDir, targetDir) {
		return false, nil
	}
	// Decide destination preservation before repairing or scanning the source.
	// A skipped migration must not mutate either ledger. This is also what makes
	// recursively repairing a pending source safe: it only happens after we know
	// this target can actually receive migrated history.
	for _, name := range codingAuthorizationMigrationFiles {
		if _, err := os.Stat(filepath.Join(targetDir, name)); err == nil {
			return false, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	for _, name := range codingAuthorizationMigrationFiles {
		for _, sidecar := range sqliteMigrationSidecarNames(name) {
			if _, err := os.Stat(filepath.Join(targetDir, sidecar)); err == nil {
				return false, fmt.Errorf("destination has orphaned SQLite artifact %s without its database", sidecar)
			} else if !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
	}
	// A rapid switch can move on before an intermediate target ever opened its
	// coding stores. If that intermediate directory still carries an interrupted
	// migration marker, it is not yet a valid source: its main databases may be
	// absent or partial migration output. Finish the intermediate migration first,
	// then copy the repaired quiescent ledger chain forward.
	if codingAuthorizationMigrationPending(sourceDir) {
		if _, err := migrateCodingAuthorizationStoresForDataDirChangeSeen("", sourceDir, activeTargets); err != nil {
			return false, fmt.Errorf("repair pending migration in source %q: %w", sourceDir, err)
		}
		if codingAuthorizationMigrationPending(sourceDir) {
			return false, fmt.Errorf("source %q migration is still pending", sourceDir)
		}
	}
	files := make([]string, 0, len(codingAuthorizationMigrationFiles))
	if len(recovery.files) != 0 {
		files = append(files, recovery.files...)
		expected := make(map[string]struct{}, len(files))
		for _, name := range files {
			expected[name] = struct{}{}
			if _, err := os.Stat(filepath.Join(sourceDir, name)); err != nil {
				return false, fmt.Errorf("resume source missing %s: %w", name, err)
			}
		}
		for _, name := range codingAuthorizationMigrationFiles {
			if _, extra := expected[name]; extra {
				continue
			}
			if _, err := os.Stat(filepath.Join(sourceDir, name)); err == nil {
				return false, fmt.Errorf("resume source changed while migration was interrupted: unexpected %s", name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
	} else {
		for _, name := range codingAuthorizationMigrationFiles {
			if _, err := os.Stat(filepath.Join(sourceDir, name)); err == nil {
				files = append(files, name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
	}
	if len(files) == 0 {
		return false, nil
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return false, err
	}
	markerPath := filepath.Join(targetDir, codingAuthorizationMigrationMarker)
	if len(recovery.files) == 0 {
		if err := writeCodingAuthorizationMigrationMarker(markerPath, codingAuthorizationMigrationState{SourceDir: sourceDir, Files: files}); err != nil {
			return false, err
		}
	}
	// An interrupted migration already has a validated marker with exactly this
	// file set and source. Do not replace it while retrying: keeping the original
	// journal avoids a non-atomic remove/rename window on filesystems that cannot
	// atomically replace an existing file.
	// Quiescence is part of migration, not merely a caller precondition. It
	// runs only after destination preservation has been decided; otherwise a
	// skipped migration would still mutate source history. The marker is
	// written first, so even a crash while quiescing leaves enough evidence to
	// resume from the correct source; quiescence itself is idempotent.
	if err := quiesceCodingAuthorizationMigrationSource(sourceDir, files); err != nil {
		return false, fmt.Errorf("quiesce coding migration source: %w", err)
	}
	removeTemps := func() {
		for _, name := range codingAuthorizationMigrationFiles {
			for _, artifact := range codingAuthorizationMigrationTempArtifactNames(name) {
				_ = os.Remove(filepath.Join(targetDir, artifact))
			}
		}
	}
	for _, name := range files {
		tmpPath := filepath.Join(targetDir, codingAuthorizationMigrationTempName(name))
		if err := vacuumSQLiteDatabaseInto(filepath.Join(sourceDir, name), tmpPath); err != nil {
			removeTemps()
			return false, fmt.Errorf("copy %s: %w", name, err)
		}
	}
	installed := make([]string, 0, len(files))
	for _, name := range files {
		tmpPath := filepath.Join(targetDir, codingAuthorizationMigrationTempName(name))
		finalPath := filepath.Join(targetDir, name)
		if err := os.Rename(tmpPath, finalPath); err != nil {
			// Only finals already installed by this migration may be removed. A
			// concurrent external file that caused this rename failure is not ours.
			for _, installedName := range installed {
				_ = os.Remove(filepath.Join(targetDir, installedName))
			}
			removeTemps()
			return false, fmt.Errorf("install %s: %w", name, err)
		}
		installed = append(installed, name)
	}
	// VACUUM INTO normally removes its own rollback journal before returning.
	// If a driver or filesystem leaves an empty temporary sidecar behind, it is
	// still migration-private output and must not survive into the replacement
	// directory. Final databases are already installed; these removals never
	// touch their live SQLite sidecars.
	removeTemps()
	if err := os.Remove(markerPath); err != nil {
		return false, fmt.Errorf("clear coding migration marker: %w", err)
	}
	return true, nil
}

// migrateStartupCodingAuthorizationStores carries the two coding authorization
// ledgers across the startup-only default→custom data migration. The generic
// byte-copy path deliberately excludes these files, so without this step a
// manually restored/imported config that already points at a custom directory
// would silently start with empty coding history.
func migrateStartupCodingAuthorizationStores(defaultBaseDir, targetBaseDir string) (bool, error) {
	return migrateCodingAuthorizationStoresSerialized(
		filepath.Join(filepath.Clean(defaultBaseDir), "data"),
		filepath.Join(filepath.Clean(targetBaseDir), "data"),
	)
}

// migrateToCustomDataDir checks if data_dir is configured and different from
// the default ~/.maclaw. If so, migrates all data (except app config storage)
// from ~/.maclaw to the new directory with progress reporting.
func (a *App) migrateToCustomDataDir() {
	a.migrateDataDirBetweenBases(corelib.MaclawDefaultBaseDir(), corelib.MaclawBaseDir())
}

// migrateDataDirBetweenBases is the testable startup migration core. Keeping
// the base-dir inputs explicit avoids binding the migration transaction to
// process-global path state in tests.
func (a *App) migrateDataDirBetweenBases(defaultDir, targetDir string) {
	defaultDir = filepath.Clean(strings.TrimSpace(defaultDir))
	targetDir = filepath.Clean(strings.TrimSpace(targetDir))
	if targetDir == "" || targetDir == "." || sameCodingDataDirPath(targetDir, defaultDir) {
		return
	}
	if dataMigrationAlreadyComplete(defaultDir, targetDir) {
		if err := cleanupDataMigrationJournalArtifacts(defaultDir, targetDir); err != nil {
			log.Printf("[DataMigration] completed migration journal cleanup failed: %v", err)
		}
		return
	}
	if !a.needsDataMigration(defaultDir, targetDir) {
		return
	}
	if dataMigrationPathsOverlap(defaultDir, targetDir) {
		errMsg := fmt.Sprintf("源数据目录和目标数据目录不能相互包含: %s <-> %s", defaultDir, targetDir)
		log.Printf("[DataMigration] %s", errMsg)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: errMsg})
		return
	}

	a.emitMigrationProgress(DataMigrationProgress{Phase: "scanning", Percent: 0})
	journal, err := a.ensureDataMigrationJournal(defaultDir, targetDir)
	if err != nil {
		var targetNotEmpty dataMigrationTargetNotEmptyError
		if errors.As(err, &targetNotEmpty) {
			errMsg := fmt.Sprintf("目标目录已有独立数据（%s），为避免覆盖未执行迁移", targetNotEmpty.path)
			log.Printf("[DataMigration] target dir %s already has independent data (%s), skipping migration", targetDir, targetNotEmpty.path)
			a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: errMsg})
			return
		}
		log.Printf("[DataMigration] migration preflight failed: %v", err)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
		return
	}

	log.Printf("[DataMigration] starting migration from %s to %s", defaultDir, targetDir)
	if migrated, err := migrateStartupCodingAuthorizationStores(defaultDir, targetDir); err != nil {
		// Coding history is part of this migration transaction. Do not byte-copy
		// the remaining data and then write the generic completion marker while
		// the SQLite migration may not even have reached its own journal phase;
		// leaving the generic migration incomplete makes the next startup retry.
		log.Printf("[DataMigration] coding authorization migration incomplete: %v", err)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: fmt.Sprintf("编程历史迁移失败: %v", err)})
		return
	} else if migrated {
		log.Printf("[DataMigration] coding authorization history migrated")
	}
	if !a.performDataMigrationCopy(defaultDir, targetDir, journal) {
		return
	}
	if err := finishDataMigration(defaultDir, targetDir, journal); err != nil {
		log.Printf("[DataMigration] completion failed: %v", err)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
		return
	}
	a.emitMigrationProgress(DataMigrationProgress{Phase: "done", Percent: 100, TotalFiles: len(journal.Files), CopiedFiles: len(journal.Files)})
}

func isCodingAuthorizationManagedDataFile(rel string) bool {
	rel = filepath.Clean(rel)
	if rel == filepath.Join("data", codingAuthorizationMigrationMarker) || rel == filepath.Join("data", codingAuthorizationMigrationMarker+".tmp") {
		return true
	}
	for _, name := range codingAuthorizationMigrationFiles {
		for _, base := range []string{name, codingAuthorizationMigrationTempName(name)} {
			// The hot-switch protocol installs one SQLite-consistent database file.
			// Byte-copying WAL/SHM/journal sidecars beside it can make SQLite replay
			// frames from the old directory into the replacement ledger.
			for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
				if rel == filepath.Join("data", base+suffix) {
					return true
				}
			}
		}
	}
	return false
}

func isAppConfigRootFile(path string) bool {
	switch canonicalDataMigrationRelKey(path) {
	case "CONFIG.JSON", "CONFIG.JSON.MIGRATED.BAK", "CONFIG.DB", "CONFIG.DB-JOURNAL", "CONFIG.DB-WAL", "CONFIG.DB-SHM":
		return systemruntime.GOOS == "windows"
	}
	switch path {
	case "config.json", "config.json.migrated.bak", "config.db", "config.db-journal", "config.db-wal", "config.db-shm":
		return true
	}
	return false
}

func dataMigrationJournalPath(dst string) string {
	return filepath.Join(dst, dataDirMigrationJournalName)
}

func sameDataMigrationRelPath(left, right string) bool {
	return canonicalDataMigrationRelKey(left) == canonicalDataMigrationRelKey(right)
}

func isDataMigrationProtocolArtifact(rel string) bool {
	return isDataMigrationSourceArtifact(rel) || isDataMigrationTargetArtifact(rel)
}

// Source artifacts are not payload and must never be copied. The completion
// marker belongs to the source, while app config intentionally stays outside
// the byte-copy migration.
func isDataMigrationSourceArtifact(rel string) bool {
	rel = filepath.Clean(rel)
	return isAppConfigRootFile(rel) || sameDataMigrationRelPath(rel, ".migrated_to") || sameDataMigrationRelPath(rel, ".migrated_to.tmp") || isDataMigrationTargetArtifact(rel)
}

// Target artifacts are private to migration into this destination. A target
// config file or .migrated_to marker is NOT such an artifact: migration never
// writes either into the destination, so they are independent evidence that
// the directory is already owned by other data.
func isDataMigrationTargetArtifact(rel string) bool {
	rel = filepath.Clean(rel)
	return sameDataMigrationRelPath(rel, dataDirMigrationJournalName) || sameDataMigrationRelPath(rel, dataDirMigrationJournalName+".tmp")
}

func canonicalDataMigrationRelKey(rel string) string {
	rel = filepath.Clean(rel)
	if systemruntime.GOOS == "windows" {
		return strings.ToUpper(rel)
	}
	return rel
}

func validDataMigrationRelPath(rel string) bool {
	if strings.TrimSpace(rel) == "" || filepath.IsAbs(rel) {
		return false
	}
	clean := filepath.Clean(rel)
	if clean != rel || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	if isDataMigrationProtocolArtifact(clean) || isCodingAuthorizationManagedDataFile(clean) || strings.HasSuffix(canonicalDataMigrationRelKey(clean), canonicalDataMigrationRelKey(dataDirMigrationTempSuffix)) {
		return false
	}
	return true
}

func validateDataMigrationJournalFiles(files []dataDirMigrationFile) error {
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if !validDataMigrationRelPath(file.Path) {
			return fmt.Errorf("invalid migration journal file path %q", file.Path)
		}
		if file.Size < 0 {
			return fmt.Errorf("invalid migration journal file size for %q", file.Path)
		}
		key := canonicalDataMigrationRelKey(file.Path)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate migration journal file path %q", file.Path)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func writeDataDirMigrationJournal(dst string, state dataDirMigrationJournal) error {
	if state.Version != dataDirMigrationJournalVersion {
		return fmt.Errorf("invalid data migration journal version %d", state.Version)
	}
	if err := validateDataMigrationJournalFiles(state.Files); err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeMigrationFileAtomic(dataMigrationJournalPath(dst), payload)
}

func readDataDirMigrationJournal(src, dst string) (*dataDirMigrationJournal, error) {
	path := dataMigrationJournalPath(dst)
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// A crash before the journal rename cannot have reached the copy phase.
		if err := os.Remove(path + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state dataDirMigrationJournal
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, fmt.Errorf("invalid data migration journal: %w", err)
	}
	if state.Version != dataDirMigrationJournalVersion {
		return nil, fmt.Errorf("unsupported data migration journal version %d", state.Version)
	}
	if !sameCodingDataDirPath(state.SourceDir, src) {
		return nil, fmt.Errorf("data migration journal source %q does not match %q", state.SourceDir, src)
	}
	if err := validateDataMigrationJournalFiles(state.Files); err != nil {
		return nil, err
	}
	// A validated journal is authoritative; an unrenamed journal temp is stale
	// private output and must not be mistaken for independent destination data.
	if err := os.Remove(path + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return &state, nil
}

func cleanupDataMigrationJournalFiles(dst string) error {
	// The writability probe never writes payload bytes. A zero-byte uniquely
	// named probe left by a crash is therefore known probe output and can be
	// removed; a non-empty lookalike remains independent target evidence.
	if entries, err := os.ReadDir(dst); err == nil {
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".maclaw_write_check-") {
				continue
			}
			match := filepath.Join(dst, entry.Name())
			if info, statErr := entry.Info(); statErr == nil && info.Mode().IsRegular() && info.Size() == 0 {
				if err := os.Remove(match); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(dataMigrationJournalPath(dst)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(dataMigrationJournalPath(dst) + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func cleanupDataMigrationJournalArtifacts(src, dst string) error {
	// Completion is already proven by the source marker when callers use this
	// helper. Use the journal manifest to remove only temp files that are known
	// migration outputs. If the journal is corrupt or absent, still remove the
	// private journal files themselves, but never guess that some other
	// similarly suffixed payload file belonged to the migration.
	if journal, err := readDataDirMigrationJournal(src, dst); err == nil && journal != nil {
		cleanupDataMigrationTempFiles(dst, journal)
	}
	return cleanupDataMigrationJournalFiles(dst)
}

func findIndependentDataMigrationPayload(dst string) (string, error) {
	// Before a journal commit there is no proof that a *.maclaw-migration-tmp
	// file belongs to this protocol. Only the journal's own root artifacts may
	// be discarded here; data temp files must remain independent evidence and
	// block migration rather than being deleted as if they were ours.
	if err := cleanupDataMigrationJournalFiles(dst); err != nil {
		return "", err
	}
	if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	found := ""
	err := filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || found != "" {
			return nil
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		if isDataMigrationTargetArtifact(rel) {
			return nil
		}
		found = rel
		return filepath.SkipAll
	})
	if err != nil {
		return "", err
	}
	return found, nil
}

func validateDataMigrationTargetState(dst string, state *dataDirMigrationJournal) error {
	expected := make(map[string]int64, len(state.Files))
	for _, file := range state.Files {
		expected[canonicalDataMigrationRelKey(file.Path)] = file.Size
	}
	if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		if isDataMigrationTargetArtifact(rel) || isCodingAuthorizationManagedDataFile(rel) {
			return nil
		}
		if strings.HasSuffix(canonicalDataMigrationRelKey(rel), canonicalDataMigrationRelKey(dataDirMigrationTempSuffix)) {
			base := rel[:len(rel)-len(dataDirMigrationTempSuffix)]
			if _, ok := expected[canonicalDataMigrationRelKey(base)]; ok {
				return nil
			}
			return fmt.Errorf("interrupted data migration has unexpected temporary artifact %s", rel)
		}
		size, ok := expected[canonicalDataMigrationRelKey(rel)]
		if !ok {
			return fmt.Errorf("interrupted data migration has unexpected destination file %s", rel)
		}
		if info.Size() != size {
			return fmt.Errorf("interrupted data migration destination file %s changed (size %d, want %d)", rel, info.Size(), size)
		}
		return nil
	})
}

func validateDataMigrationTargetComplete(dst string, state *dataDirMigrationJournal) error {
	if err := validateDataMigrationTargetState(dst, state); err != nil {
		return err
	}
	for _, file := range state.Files {
		info, err := os.Stat(filepath.Join(dst, file.Path))
		if err != nil {
			return fmt.Errorf("migration output %s is missing before completion: %w", file.Path, err)
		}
		if info.IsDir() || !info.Mode().IsRegular() || info.Size() != file.Size {
			return fmt.Errorf("migration output %s is invalid before completion", file.Path)
		}
	}
	return nil
}

func cleanupDataMigrationTempFiles(dst string, state *dataDirMigrationJournal) {
	for _, file := range state.Files {
		_ = os.Remove(filepath.Join(dst, file.Path) + dataDirMigrationTempSuffix)
	}
}

func dataMigrationPathWithin(child, parent string) bool {
	child = canonicalCodingDataDirKey(child)
	parent = canonicalCodingDataDirKey(parent)
	if child == parent {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func dataMigrationPathsOverlap(src, dst string) bool {
	return dataMigrationPathWithin(src, dst) || dataMigrationPathWithin(dst, src)
}

func dataMigrationAlreadyComplete(src, dst string) bool {
	markerData, err := os.ReadFile(filepath.Join(src, ".migrated_to"))
	if err != nil {
		return false
	}
	return sameCodingDataDirPath(filepath.Clean(strings.TrimSpace(string(markerData))), dst)
}

// needsDataMigration reports whether the source root has any non-config data
// that could require migration. Target ownership and crash recovery are handled
// by the migration journal, not by overwriting whatever happens to be in dst.
func (a *App) needsDataMigration(src, dst string) bool {
	if dataMigrationAlreadyComplete(src, dst) {
		return false
	}
	srcEntries, err := os.ReadDir(src)
	if err != nil || len(srcEntries) == 0 {
		return false
	}
	for _, e := range srcEntries {
		if !isAppConfigRootFile(e.Name()) && e.Name() != ".migrated_to" && e.Name() != ".migrated_to.tmp" {
			return true
		}
	}
	return false
}

func scanDataMigrationFiles(src string) ([]dataDirMigrationFile, int64, error) {
	var files []dataDirMigrationFile
	var totalSize int64
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		if rel == "." || isDataMigrationSourceArtifact(rel) || isCodingAuthorizationManagedDataFile(rel) {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported non-regular file in data migration: %s", rel)
		}
		files = append(files, dataDirMigrationFile{Path: rel, Size: info.Size()})
		totalSize += info.Size()
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return files, totalSize, nil
}

func (a *App) ensureDataMigrationJournal(src, dst string) (*dataDirMigrationJournal, error) {
	if dataMigrationPathsOverlap(src, dst) {
		return nil, fmt.Errorf("source and target data directories overlap: %s <-> %s", src, dst)
	}
	if journal, err := readDataDirMigrationJournal(src, dst); err != nil || journal != nil {
		return journal, err
	}
	files, totalSize, err := scanDataMigrationFiles(src)
	if err != nil {
		return nil, fmt.Errorf("scan source data: %w", err)
	}
	log.Printf("[DataMigration] found %d files (%d MB) to migrate", len(files), totalSize/(1024*1024))
	if independent, err := findIndependentDataMigrationPayload(dst); err != nil {
		return nil, fmt.Errorf("scan target data: %w", err)
	} else if independent != "" {
		return nil, dataMigrationTargetNotEmptyError{path: independent}
	}
	if !a.checkTargetWritable(dst) {
		return nil, fmt.Errorf("target data directory is not writable: %s", dst)
	}
	journal := &dataDirMigrationJournal{
		Version:   dataDirMigrationJournalVersion,
		SourceDir: filepath.Clean(src),
		Files:     files,
		StartedAt: time.Now().Unix(),
	}
	if err := writeDataDirMigrationJournal(dst, *journal); err != nil {
		return nil, fmt.Errorf("write data migration journal: %w", err)
	}
	return journal, nil
}

func writeDataMigrationCompleteMarker(src, dst string) error {
	return writeMigrationFileAtomic(filepath.Join(src, ".migrated_to"), []byte(filepath.Clean(dst)))
}

func finishDataMigration(src, dst string, journal *dataDirMigrationJournal) error {
	// Revalidate at the commit point. Startup services or an external writer
	// must not be able to smuggle an unrelated file into the destination after
	// the initial preflight and have completion hide that fact.
	if err := validateDataMigrationTargetComplete(dst, journal); err != nil {
		return fmt.Errorf("validate destination before completion: %w", err)
	}
	cleanupDataMigrationTempFiles(dst, journal)
	if err := writeDataMigrationCompleteMarker(src, dst); err != nil {
		return err
	}
	return cleanupDataMigrationJournalArtifacts(src, dst)
}

func (a *App) performDataMigrationCopy(src, dst string, journal *dataDirMigrationJournal) bool {
	start := time.Now()
	if err := validateDataMigrationTargetState(dst, journal); err != nil {
		log.Printf("[DataMigration] target validation failed: %v", err)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
		return false
	}
	cleanupDataMigrationTempFiles(dst, journal)

	totalFiles := len(journal.Files)
	if totalFiles == 0 {
		log.Printf("[DataMigration] no generic files to migrate")
		return true
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		log.Printf("[DataMigration] create target failed: %v", err)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
		return false
	}

	processed := 0
	var lastEmit time.Time
	for _, file := range journal.Files {
		srcPath := filepath.Join(src, file.Path)
		dstPath := filepath.Join(dst, file.Path)
		if info, err := os.Stat(dstPath); err == nil {
			if info.IsDir() || !info.Mode().IsRegular() || info.Size() != file.Size {
				errMsg := fmt.Sprintf("目标文件已被修改或不完整: %s", file.Path)
				log.Printf("[DataMigration] %s", errMsg)
				a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: errMsg})
				return false
			}
		} else if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				log.Printf("[DataMigration] mkdir failed for %s: %v", filepath.Dir(dstPath), err)
				a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
				return false
			}
			written, err := copyMigrationFileAtomic(srcPath, dstPath)
			if err != nil {
				log.Printf("[DataMigration] copy failed %s: %v", file.Path, err)
				a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
				return false
			}
			if written != file.Size {
				// This final file is known output of the copy that just returned:
				// the destination did not exist before it, and the atomic rename has
				// already committed. Remove that invalid output so the next retry can
				// copy a stable source instead of bricking recovery on a size mismatch.
				_ = os.Remove(dstPath)
				errMsg := fmt.Sprintf("源文件在迁移过程中发生变化: %s", file.Path)
				log.Printf("[DataMigration] %s", errMsg)
				a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: errMsg})
				return false
			}
		} else {
			log.Printf("[DataMigration] stat target failed %s: %v", file.Path, err)
			a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
			return false
		}
		processed++
		if time.Since(lastEmit) > 100*time.Millisecond {
			percent := float64(processed) / float64(totalFiles) * 100
			a.emitMigrationProgress(DataMigrationProgress{
				Phase:       "copying",
				Percent:     percent,
				CurrentFile: filepath.Base(file.Path),
				TotalFiles:  totalFiles,
				CopiedFiles: processed,
			})
			lastEmit = time.Now()
		}
	}
	log.Printf("[DataMigration] copied %d files in %s", processed, time.Since(start))
	return true
}

// performDataMigration copies all files/dirs from src to dst (except app config
// storage), emitting progress events to the frontend.
func (a *App) performDataMigration(src, dst string) {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	a.emitMigrationProgress(DataMigrationProgress{Phase: "scanning", Percent: 0})
	if dataMigrationAlreadyComplete(src, dst) {
		_ = cleanupDataMigrationJournalArtifacts(src, dst)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "done", Percent: 100})
		return
	}
	journal, err := a.ensureDataMigrationJournal(src, dst)
	if err != nil {
		log.Printf("[DataMigration] migration preflight failed: %v", err)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
		return
	}
	if !a.performDataMigrationCopy(src, dst, journal) {
		return
	}
	if err := finishDataMigration(src, dst, journal); err != nil {
		log.Printf("[DataMigration] completion failed: %v", err)
		a.emitMigrationProgress(DataMigrationProgress{Phase: "error", Error: err.Error()})
		return
	}
	a.emitMigrationProgress(DataMigrationProgress{Phase: "done", Percent: 100, TotalFiles: len(journal.Files), CopiedFiles: len(journal.Files)})
}

// emitMigrationProgress sends migration progress to the frontend.
func (a *App) emitMigrationProgress(p DataMigrationProgress) {
	if a.ctx != nil {
		a.emitEvent("data-migration-progress", p)
	}
}

// copyMigrationFileAtomic copies one file through a migration-private temp
// file and renames it into place. A crash can therefore leave either no final
// file or a complete file, never a torn destination file.
func copyMigrationFileAtomic(src, dst string) (int64, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("open src: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat src: %w", err)
	}
	if !srcInfo.Mode().IsRegular() {
		return 0, fmt.Errorf("source is not a regular file: %s", src)
	}

	tmpPath := dst + dataDirMigrationTempSuffix
	if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("remove stale migration temp: %w", err)
	}
	dstFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, srcInfo.Mode())
	if err != nil {
		return 0, fmt.Errorf("create migration temp: %w", err)
	}
	written, copyErr := io.Copy(dstFile, srcFile)
	if copyErr == nil {
		copyErr = dstFile.Sync()
	}
	if closeErr := dstFile.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return written, fmt.Errorf("copy: %w", copyErr)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return written, fmt.Errorf("install copied file: %w", err)
	}
	return written, nil
}

// checkTargetWritable verifies the target directory is writable.
// Returns true if a test file can be created and removed.
func (a *App) checkTargetWritable(targetDir string) bool {
	_ = os.MkdirAll(targetDir, 0o755)
	f, err := os.CreateTemp(targetDir, ".maclaw_write_check-*")
	if err != nil {
		return false
	}
	testFile := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(testFile)
		return false
	}
	return os.Remove(testFile) == nil
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
		if sameCodingDataDirPath(newDir, defaultDir) {
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
