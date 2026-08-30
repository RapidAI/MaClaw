package skill

// Durable compensation records make an incomplete rollback recoverable after
// process exit.  The record is deliberately independent of the audit log: an
// audit row describes what happened, while this queue contains the bytes and
// config snapshot required to repair it.

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const evolutionCompensationSchemaVersion = "1"

// EvolutionCompensationRecord is one pending rollback operation. Config is
// serialized as a full snapshot so recovery never depends on a stale in-memory
// entry. YAML bytes are base64 encoded to keep the queue valid JSONL.
type EvolutionCompensationRecord struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id,omitempty"`
	Skill         string `json:"skill"`
	// RecoveryScope identifies the service-owned persistence root that may
	// recover this record (for example an AgentService DataRoot). It prevents
	// one service instance from replaying another tenant/service's directory
	// transaction when all instances share the process-global queue file.
	// Empty is retained only for backwards-compatible legacy records.
	RecoveryScope string `json:"recovery_scope,omitempty"`
	// AffectedSkills lists every identity covered by one batch transaction.
	// Skill remains the primary key for backwards compatibility, while this
	// field prevents a pending multi-package rollback from leaving sibling
	// packages executable or uploadable.
	AffectedSkills []string               `json:"affected_skills,omitempty"`
	Action         string                 `json:"action,omitempty"`
	YAMLPath       string                 `json:"yaml_path,omitempty"`
	YAMLBackup     string                 `json:"yaml_backup_b64,omitempty"`
	YAMLExists     bool                   `json:"yaml_exists"`
	ConfigBackup   []corelib.NLSkillEntry `json:"config_backup,omitempty"`
	DraftPath      string                 `json:"draft_path,omitempty"`
	DraftBackup    string                 `json:"draft_backup_b64,omitempty"`
	DraftExists    bool                   `json:"draft_exists,omitempty"`
	// Directory movement is used by recoverable rename/delete operations. The
	// queue stores paths only (the directory contents remain on disk), so a
	// restart can move the staged directory back without embedding arbitrary
	// files in JSONL.
	DirPath       string `json:"dir_path,omitempty"`
	DirBackupPath string `json:"dir_backup_path,omitempty"`
	DirMoved      bool   `json:"dir_moved,omitempty"`
	// DirHadPrevious distinguishes an update (where the published directory
	// must never be deleted without first restoring its retained .prev) from a
	// new install (where CreatedDirs can be safely removed). It is persisted in
	// the initial pre-move snapshot to close the crash window before DirMoved is
	// updated.
	DirHadPrevious bool `json:"dir_had_previous,omitempty"`
	// DirPublished is set once the replacement directory has been published.
	// It is informational for recovery and allows future migrations to
	// distinguish an intent-only record from a post-move snapshot.
	DirPublished bool `json:"dir_published,omitempty"`
	// CreatedDirs records newly installed directories that must be removed if
	// an import transaction fails after the filesystem move. Unlike DirMoved,
	// these paths have no pre-existing source to restore.
	CreatedDirs []string `json:"created_dirs,omitempty"`
	// PostCommitCleanupPaths are safe to remove only after final audit and
	// committed-state persistence. They are never used for rollback.
	PostCommitCleanupPaths []string `json:"post_commit_cleanup_paths,omitempty"`
	// RollbackCleanupPaths are artifacts created by the transaction (for
	// example a new Versioner backup). They must be removed only after the
	// pre-image has been restored. Keeping them in the durable record closes
	// the crash window between artifact creation and rollback completion.
	RollbackCleanupPaths []string `json:"rollback_cleanup_paths,omitempty"`
	// ExternalSnapshots contains opaque, caller-owned pre-images for external
	// authorization/state systems. The core package never interprets these
	// values; an owning service may restore them during cross-process recovery.
	ExternalSnapshots map[string]string `json:"external_snapshots,omitempty"`
	// ExternalApplied marks transitions whose side effect is either complete or
	// may have crossed its mutation boundary. Callers set the marker before
	// performing the side effect and durably replace the record, so a crash
	// cannot lose the intent to restore the external pre-image.
	ExternalApplied map[string]bool `json:"external_applied,omitempty"`
	// DirectoryMoves records every directory crossed by a multi-package install
	// transaction.  The legacy singular fields remain for backwards
	// compatibility with older lifecycle adapters.
	DirectoryMoves []EvolutionDirectoryMove `json:"directory_moves,omitempty"`
	// FileSnapshots stores small non-Skill registry pre-images (for example the
	// legacy GUI metadata.json).  Values are base64 encoded and are restored
	// before config/index recovery; callers must use it only for bounded JSON
	// metadata, never for arbitrary package contents.
	FileSnapshots []EvolutionFileSnapshot `json:"file_snapshots,omitempty"`
	FailureReason string                  `json:"failure_reason,omitempty"`
	// FinalAuditKind identifies the strict audit event that crosses this
	// transaction's business boundary. It is persisted with the prepared
	// record so crash recovery can distinguish "audit written, committed marker
	// not yet persisted" from a transaction that never reached final audit.
	// The audit row remains the source of truth; this field is only a lookup
	// hint and never authorizes a commit by itself.
	FinalAuditKind string `json:"final_audit_kind,omitempty"`
	Attempts       int    `json:"attempts"`
	NextRetryAt    string `json:"next_retry_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	LastError      string `json:"last_error,omitempty"`
	Status         string `json:"status,omitempty"` // pending | needs_review
	// TransactionState separates the business commit result from queue cleanup.
	// A committed transaction must never be rolled back merely because its
	// compensation record could not yet be removed.
	TransactionState string `json:"transaction_state,omitempty"` // prepared | committed | rolled_back | audit_pending
	// CleanupStatus is independent from the business transaction result. A
	// committed definition may remain blocked while backup/queue cleanup is
	// pending; callers must not infer clear from Status alone.
	CleanupStatus string `json:"cleanup_status,omitempty"` // clear | pending | needs_review
}

// EvolutionDirectoryMove is a durable description of one directory publish.
// Paths are local recovery targets and are never exposed through summaries.
type EvolutionDirectoryMove struct {
	OriginalPath string `json:"original_path,omitempty"`
	BackupPath   string `json:"backup_path,omitempty"`
	HadPrevious  bool   `json:"had_previous,omitempty"`
	Moved        bool   `json:"moved,omitempty"`
	Published    bool   `json:"published,omitempty"`
}

type EvolutionFileSnapshot struct {
	Path      string `json:"path,omitempty"`
	BackupB64 string `json:"backup_b64,omitempty"`
	Exists    bool   `json:"exists,omitempty"`
}

// EvolutionCompensationSummary is the operator-safe view of a pending
// compensation. It deliberately omits YAML/config/draft bytes and absolute
// paths, which may contain user data or secrets.
type EvolutionCompensationSummary struct {
	RequestID        string `json:"request_id,omitempty"`
	Skill            string `json:"skill"`
	Action           string `json:"action,omitempty"`
	Attempts         int    `json:"attempts"`
	Status           string `json:"status,omitempty"`
	FailureReason    string `json:"failure_reason,omitempty"`
	LastError        string `json:"last_error,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	NextRetryAt      string `json:"next_retry_at,omitempty"`
	CleanupStatus    string `json:"cleanup_status,omitempty"`
	TransactionState string `json:"transaction_state,omitempty"`
}

const evolutionCompensationMaxAttempts = 3

var evolutionCompensationMu sync.Mutex

func DefaultEvolutionCompensationPath() string {
	return filepath.Join(corelib.MaclawBaseDir(), "skill_evolution", "audit_pending.jsonl")
}

func newEvolutionCompensationRecord(requestID, skillName, action string, yamlPath string, yamlBackup []byte, yamlExists bool, config []corelib.NLSkillEntry, reason string) EvolutionCompensationRecord {
	record := EvolutionCompensationRecord{
		SchemaVersion: evolutionCompensationSchemaVersion,
		RequestID:     strings.TrimSpace(requestID), Skill: strings.TrimSpace(skillName), Action: strings.TrimSpace(action),
		YAMLPath: yamlPath, YAMLExists: yamlExists, ConfigBackup: cloneSkillEntries(config),
		FailureReason: strings.TrimSpace(reason), Attempts: 0, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		CleanupStatus: "pending",
	}
	if yamlExists {
		record.YAMLBackup = base64.StdEncoding.EncodeToString(yamlBackup)
	}
	return record
}

// SetDraftBackup adds an optional reviewed-draft snapshot to a compensation
// record. It is kept separate from the constructor so existing YAML/config
// callers remain source-compatible.
func (r *EvolutionCompensationRecord) SetDraftBackup(path string, data []byte, exists bool) {
	if r == nil {
		return
	}
	r.DraftPath = strings.TrimSpace(path)
	r.DraftExists = exists
	if exists {
		r.DraftBackup = base64.StdEncoding.EncodeToString(data)
	}
}

// SetDirectoryBackup records a directory move that must be reversed during
// compensation. It intentionally does not copy directory contents; the
// moved directory itself is the durable snapshot.
func (r *EvolutionCompensationRecord) SetDirectoryBackup(originalPath, movedPath string, moved bool) {
	if r == nil {
		return
	}
	r.DirPath = strings.TrimSpace(originalPath)
	r.DirBackupPath = strings.TrimSpace(movedPath)
	r.DirMoved = moved
	r.DirHadPrevious = moved
}

// SetDirectoryBackupIntent records an existing installation that is about to
// be moved to a backup path. It deliberately does not mark the move complete;
// recovery can then distinguish a pre-move crash (backup absent) from a
// post-move crash (backup present) without deleting the original directory.
func (r *EvolutionCompensationRecord) SetDirectoryBackupIntent(originalPath, movedPath string) {
	if r == nil {
		return
	}
	r.DirPath = strings.TrimSpace(originalPath)
	r.DirBackupPath = strings.TrimSpace(movedPath)
	r.DirMoved = false
	r.DirHadPrevious = true
}

// SetDirectoryPublished records that the replacement directory crossed the
// filesystem publication boundary.
func (r *EvolutionCompensationRecord) SetDirectoryPublished(published bool) {
	if r == nil {
		return
	}
	r.DirPublished = published
}

// SetCreatedDirectories records paths created by an import/install operation.
// The paths are intentionally stored, rather than directory contents, because
// the durable rollback action is to remove the newly published directories.
func (r *EvolutionCompensationRecord) SetCreatedDirectories(paths []string) {
	if r == nil {
		return
	}
	r.CreatedDirs = append([]string(nil), paths...)
}

// SetPostCommitCleanupPaths records idempotent cleanup targets for a committed
// transaction. Recovery removes these paths without restoring old config/YAML.
func (r *EvolutionCompensationRecord) SetPostCommitCleanupPaths(paths []string) {
	if r == nil {
		return
	}
	r.PostCommitCleanupPaths = append([]string(nil), paths...)
}

// SetRollbackCleanupPaths records artifacts that are valid only for a
// transaction which is ultimately rolled back. Successful commits retain
// these artifacts as user-visible history; rollback/recovery removes them.
func (r *EvolutionCompensationRecord) SetRollbackCleanupPaths(paths []string) {
	if r == nil {
		return
	}
	r.RollbackCleanupPaths = append([]string(nil), paths...)
}

// SetExternalSnapshot records an opaque caller-owned pre-image and initializes
// its transition marker to false. The value is intentionally absent from
// operator summaries; callers should serialize only the minimum safe state.
func (r *EvolutionCompensationRecord) SetExternalSnapshot(key, value string) {
	if r == nil || strings.TrimSpace(key) == "" {
		return
	}
	key = strings.TrimSpace(key)
	if r.ExternalSnapshots == nil {
		r.ExternalSnapshots = make(map[string]string)
	}
	if r.ExternalApplied == nil {
		r.ExternalApplied = make(map[string]bool)
	}
	r.ExternalSnapshots[key] = value
	r.ExternalApplied[key] = false
}

// MarkExternalApplied records that an external transition may have crossed
// its mutation boundary. It is safe to call before the side effect; recovery
// restores the snapshot idempotently even if the side effect later fails.
func (r *EvolutionCompensationRecord) MarkExternalApplied(key string, applied bool) {
	if r == nil || strings.TrimSpace(key) == "" {
		return
	}
	if r.ExternalApplied == nil {
		r.ExternalApplied = make(map[string]bool)
	}
	r.ExternalApplied[strings.TrimSpace(key)] = applied
}

// SetDirectoryMoves replaces the durable directory movement plan for a
// multi-package install. Callers must invoke it before publication and update
// the Moved/Published flags after each filesystem boundary.
func (r *EvolutionCompensationRecord) SetDirectoryMoves(moves []EvolutionDirectoryMove) {
	if r == nil {
		return
	}
	r.DirectoryMoves = append([]EvolutionDirectoryMove(nil), moves...)
}

// SetFileSnapshots replaces bounded file pre-images used by legacy adapters.
func (r *EvolutionCompensationRecord) SetFileSnapshots(snapshots []EvolutionFileSnapshot) {
	if r == nil {
		return
	}
	r.FileSnapshots = append([]EvolutionFileSnapshot(nil), snapshots...)
}

// SetAffectedSkills records all Skill identities covered by a batch commit.
// Empty values and duplicates are removed while preserving input order.
func (r *EvolutionCompensationRecord) SetAffectedSkills(skills []string) {
	if r == nil {
		return
	}
	seen := make(map[string]struct{}, len(skills))
	r.AffectedSkills = r.AffectedSkills[:0]
	for _, name := range skills {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		r.AffectedSkills = append(r.AffectedSkills, name)
	}
}

// SetRecoveryScope binds a compensation record to its owning service scope.
// The value is metadata only; recovery still validates the durable paths and
// action prefix before applying any filesystem operation.
func (r *EvolutionCompensationRecord) SetRecoveryScope(scope string) {
	if r == nil {
		return
	}
	r.RecoveryScope = normalizeRecoveryScope(scope)
}

func normalizeRecoveryScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	if absolute, err := filepath.Abs(scope); err == nil {
		scope = absolute
	}
	scope = filepath.Clean(scope)
	if runtime.GOOS == "windows" {
		scope = strings.ToLower(scope)
	}
	if scope == "." {
		return ""
	}
	return scope
}

// compensationRecordMatchesScope proves ownership of a legacy record that
// predates RecoveryScope. Every durable path present in the record must be
// under the candidate scope; records without any path cannot be safely
// attributed and therefore remain pending (fail-closed).
func compensationRecordMatchesScope(record EvolutionCompensationRecord, scope string) bool {
	scope = normalizeRecoveryScope(scope)
	if scope == "" {
		return false
	}
	paths := make([]string, 0, 8+len(record.DirectoryMoves)+len(record.CreatedDirs)+len(record.PostCommitCleanupPaths)+len(record.RollbackCleanupPaths)+len(record.FileSnapshots))
	paths = append(paths, record.YAMLPath, record.DraftPath, record.DirPath, record.DirBackupPath)
	for _, move := range record.DirectoryMoves {
		paths = append(paths, move.OriginalPath, move.BackupPath)
	}
	paths = append(paths, record.CreatedDirs...)
	paths = append(paths, record.PostCommitCleanupPaths...)
	paths = append(paths, record.RollbackCleanupPaths...)
	for _, snapshot := range record.FileSnapshots {
		paths = append(paths, snapshot.Path)
	}
	found := false
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !filepath.IsAbs(raw) {
			return false
		}
		found = true
		candidate := normalizeRecoveryScope(raw)
		rel, err := filepath.Rel(scope, candidate)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
	}
	return found
}

func compensationRecordContainsPathInScope(record EvolutionCompensationRecord, scope string) bool {
	scope = normalizeRecoveryScope(scope)
	if scope == "" {
		return false
	}
	paths := make([]string, 0, 8+len(record.DirectoryMoves)+len(record.CreatedDirs)+len(record.PostCommitCleanupPaths)+len(record.RollbackCleanupPaths)+len(record.FileSnapshots))
	paths = append(paths, record.YAMLPath, record.DraftPath, record.DirPath, record.DirBackupPath)
	for _, move := range record.DirectoryMoves {
		paths = append(paths, move.OriginalPath, move.BackupPath)
	}
	paths = append(paths, record.CreatedDirs...)
	paths = append(paths, record.PostCommitCleanupPaths...)
	paths = append(paths, record.RollbackCleanupPaths...)
	for _, snapshot := range record.FileSnapshots {
		paths = append(paths, snapshot.Path)
	}
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" || !filepath.IsAbs(raw) {
			continue
		}
		candidate := normalizeRecoveryScope(raw)
		rel, err := filepath.Rel(scope, candidate)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func cloneSkillEntries(src []corelib.NLSkillEntry) []corelib.NLSkillEntry {
	if len(src) == 0 {
		return nil
	}
	dst := make([]corelib.NLSkillEntry, len(src))
	for i := range src {
		if cp := CloneNLSkillEntry(&src[i]); cp != nil {
			dst[i] = *cp
		}
	}
	return dst
}

func appendEvolutionCompensation(record EvolutionCompensationRecord) error {
	path := DefaultEvolutionCompensationPath()
	record.SchemaVersion = evolutionCompensationSchemaVersion
	if strings.TrimSpace(record.CreatedAt) == "" {
		record.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	evolutionCompensationMu.Lock()
	defer evolutionCompensationMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// PersistEvolutionCompensation exposes the durable queue boundary to GUI/TUI
// adapters. The record is never treated as a success signal; it only records
// work that must be recovered before the affected Skill may run again.
func PersistEvolutionCompensation(record EvolutionCompensationRecord) error {
	return appendEvolutionCompensation(record)
}

// ReplaceEvolutionCompensation updates the authoritative snapshot for one
// request. Adapters that predate SkillCommitter can use this when an import or
// lifecycle operation changes from prepared to rolled_back/committed without
// leaving duplicate live records in the queue.
func ReplaceEvolutionCompensation(record EvolutionCompensationRecord) error {
	return replaceEvolutionCompensation(record)
}

// replaceEvolutionCompensation replaces the latest durable snapshot for one
// request. The append-only writer is useful for the initial prepare record,
// but state transitions (rollback, audit_pending, committed/cleanup pending)
// must not leave multiple live snapshots for the same request: recovery and
// operator views need one authoritative record.
func replaceEvolutionCompensation(record EvolutionCompensationRecord) error {
	requestID := strings.TrimSpace(record.RequestID)
	if requestID == "" {
		return fmt.Errorf("compensation request_id is required")
	}
	records, err := readEvolutionCompensations()
	if err != nil {
		return err
	}
	remaining := make([]EvolutionCompensationRecord, 0, len(records)+1)
	for _, existing := range records {
		if strings.TrimSpace(existing.RequestID) == requestID {
			continue
		}
		remaining = append(remaining, existing)
	}
	remaining = append(remaining, record)
	return writeEvolutionCompensations(remaining)
}

// ClearEvolutionCompensation removes the durable snapshot for one completed
// transaction. Callers must only use it after the transaction's final audit
// and post-commit cleanup have succeeded. If cleanup fails, the caller should
// roll the transaction back and leave the record pending.
func ClearEvolutionCompensation(requestID, skillName, action string) error {
	requestID = strings.TrimSpace(requestID)
	skillName = strings.TrimSpace(skillName)
	action = strings.TrimSpace(action)
	records, err := readEvolutionCompensations()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	remaining := make([]EvolutionCompensationRecord, 0, len(records))
	for _, record := range records {
		matchesRequest := requestID != "" && strings.TrimSpace(record.RequestID) == requestID
		matchesIdentity := requestID == "" && strings.EqualFold(strings.TrimSpace(record.Skill), skillName) && strings.EqualFold(strings.TrimSpace(record.Action), action)
		if matchesRequest || matchesIdentity {
			continue
		}
		remaining = append(remaining, record)
	}
	return writeEvolutionCompensations(remaining)
}

// ListEvolutionCompensationSummaries returns an operator-safe snapshot of the
// durable queue. The queue remains fail-closed when it cannot be read.
func ListEvolutionCompensationSummaries() ([]EvolutionCompensationSummary, error) {
	records, err := readEvolutionCompensations()
	if err != nil {
		return nil, err
	}
	out := make([]EvolutionCompensationSummary, 0, len(records))
	for _, record := range records {
		out = append(out, EvolutionCompensationSummary{
			RequestID: record.RequestID, Skill: record.Skill, Action: record.Action,
			Attempts: record.Attempts, Status: record.Status, FailureReason: record.FailureReason,
			LastError: safeCompensationText(record.LastError), CreatedAt: record.CreatedAt, NextRetryAt: record.NextRetryAt, CleanupStatus: record.CleanupStatus, TransactionState: record.TransactionState,
		})
	}
	return out, nil
}

// CheckEvolutionCompensationQueue validates the durable queue without
// exposing rollback snapshots. Callers that need a system-wide health gate
// should use this instead of probing HasPendingCompensation with a sentinel
// Skill name.
func CheckEvolutionCompensationQueue() error {
	_, err := readEvolutionCompensations()
	return err
}

// safeCompensationText keeps operator diagnostics bounded and avoids exposing
// absolute local paths from filesystem errors through GUI/TUI responses.
func safeCompensationText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	for i, part := range parts {
		if strings.Contains(part, `\`) || strings.HasPrefix(part, "/") || (len(part) >= 3 && part[1] == ':' && (part[2] == '\\' || part[2] == '/')) {
			parts[i] = "<path>"
		}
	}
	value = strings.Join(parts, " ")
	if len([]rune(value)) > 256 {
		value = string([]rune(value)[:256])
	}
	return value
}

// NewEvolutionCompensationRecord creates a durable rollback snapshot for a
// platform-specific transaction that cannot use persistDefinitionChange.
func NewEvolutionCompensationRecord(requestID, skillName, action, yamlPath string, yamlBackup []byte, yamlExists bool, config []corelib.NLSkillEntry, reason string) EvolutionCompensationRecord {
	return newEvolutionCompensationRecord(requestID, skillName, action, yamlPath, yamlBackup, yamlExists, config, reason)
}

// RestoreEvolutionCompensation applies a durable rollback snapshot. It is
// exported for GUI adapters that need to compensate a failed install/update
// without duplicating the queue's restore semantics.
func RestoreEvolutionCompensation(record EvolutionCompensationRecord, skillSaver func([]corelib.NLSkillEntry) error, indexRefresher func() error) error {
	return restoreEvolutionCompensation(record, skillSaver, indexRefresher)
}

func readEvolutionCompensations() ([]EvolutionCompensationRecord, error) {
	path := DefaultEvolutionCompensationPath()
	evolutionCompensationMu.Lock()
	defer evolutionCompensationMu.Unlock()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var records []EvolutionCompensationRecord
	scanner := bufio.NewScanner(f)
	// ConfigBackup may contain many installed Skills. The Scanner default
	// limit (64 KiB) would turn a valid durable snapshot into an unreadable
	// queue and unnecessarily block every execution/upload path.
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var record EvolutionCompensationRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode compensation record: %w", err)
		}
		// Records written before schema_version was introduced are accepted as
		// the current shape for forward-compatible reads. An explicitly unknown
		// version must fail closed: silently skipping it could make a Skill look
		// executable while an unrecognized rollback is still pending.
		if strings.TrimSpace(record.SchemaVersion) == "" {
			record.SchemaVersion = evolutionCompensationSchemaVersion
		}
		if record.SchemaVersion != evolutionCompensationSchemaVersion {
			return nil, fmt.Errorf("unsupported compensation schema_version %q", record.SchemaVersion)
		}
		if strings.TrimSpace(record.Skill) == "" {
			return nil, fmt.Errorf("compensation record missing skill")
		}
		if record.Attempts < 0 {
			return nil, fmt.Errorf("compensation record has negative attempts for skill %q", record.Skill)
		}
		if status := strings.TrimSpace(record.Status); status != "" && status != "pending" && status != "needs_review" {
			return nil, fmt.Errorf("unsupported compensation status %q for skill %q", status, record.Skill)
		}
		if cleanup := strings.TrimSpace(record.CleanupStatus); cleanup != "" && cleanup != "clear" && cleanup != "pending" && cleanup != "needs_review" {
			return nil, fmt.Errorf("unsupported compensation cleanup_status %q for skill %q", cleanup, record.Skill)
		}
		if strings.TrimSpace(record.CleanupStatus) == "" {
			record.CleanupStatus = "pending"
		}
		if state := strings.TrimSpace(record.TransactionState); state != "" && state != "prepared" && state != "committed" && state != "rolled_back" && state != "audit_pending" {
			return nil, fmt.Errorf("unsupported compensation transaction_state %q for skill %q", state, record.Skill)
		}
		if strings.TrimSpace(record.TransactionState) == "" {
			// Legacy records were only written for incomplete rollback paths. Keep
			// the conservative interpretation during schema-compatible reads.
			record.TransactionState = "audit_pending"
		}
		if len(record.ExternalSnapshots) > 0 {
			for key, value := range record.ExternalSnapshots {
				if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
					return nil, fmt.Errorf("compensation record has invalid external snapshot for skill %q", record.Skill)
				}
			}
			for key := range record.ExternalApplied {
				if _, exists := record.ExternalSnapshots[key]; !exists {
					return nil, fmt.Errorf("compensation record external transition has no snapshot for skill %q", record.Skill)
				}
			}
		}
		if scope := strings.TrimSpace(record.RecoveryScope); scope != "" && !filepath.IsAbs(scope) {
			return nil, fmt.Errorf("compensation record recovery_scope must be absolute for skill %q", record.Skill)
		}
		for _, snapshot := range record.FileSnapshots {
			if strings.TrimSpace(snapshot.Path) == "" || !filepath.IsAbs(snapshot.Path) {
				return nil, fmt.Errorf("compensation record file snapshot path must be absolute for skill %q", record.Skill)
			}
			if snapshot.Exists {
				data, decodeErr := base64.StdEncoding.DecodeString(snapshot.BackupB64)
				if decodeErr != nil || len(data) > 2*1024*1024 {
					return nil, fmt.Errorf("compensation record file snapshot is invalid for skill %q", record.Skill)
				}
			}
		}
		record.ConfigBackup = cloneSkillEntries(record.ConfigBackup)
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// A crash between append and queue compaction can leave duplicate snapshots.
	// Keep the newest record for each request/action pair so recovery remains
	// idempotent and does not replay the same side effect more than once.
	latest := make(map[string]EvolutionCompensationRecord, len(records))
	order := make([]string, 0, len(records))
	for _, record := range records {
		key := strings.TrimSpace(record.RequestID)
		if key == "" {
			key = strings.Join([]string{record.Skill, record.Action, record.YAMLPath, record.DraftPath}, "|")
		} else {
			key += "|" + record.Action
		}
		if _, exists := latest[key]; !exists {
			order = append(order, key)
		}
		latest[key] = record
	}
	compacted := make([]EvolutionCompensationRecord, 0, len(order))
	for _, key := range order {
		compacted = append(compacted, latest[key])
	}
	return compacted, nil
}

func writeEvolutionCompensations(records []EvolutionCompensationRecord) error {
	path := DefaultEvolutionCompensationPath()
	evolutionCompensationMu.Lock()
	defer evolutionCompensationMu.Unlock()
	if len(records) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".audit_pending-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	enc := json.NewEncoder(tmp)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err == nil {
		return nil
	} else if _, statErr := os.Stat(path); statErr != nil {
		// The destination does not exist, so the original error is more useful
		// than attempting a replacement sequence that could hide permissions or
		// filesystem failures.
		return err
	}
	// Windows cannot rename over an existing file. Move the old queue aside,
	// publish the fully-synced temporary file, and restore the old queue if the
	// second rename fails. POSIX takes the fast path above; this fallback keeps
	// the replacement recoverable on Windows as well.
	backupPath := path + ".replace-backup"
	_ = os.Remove(backupPath)
	if err := os.Rename(path, backupPath); err != nil {
		return err
	}
	// Keep the old queue until the new file is durably published. If publishing
	// fails and restoration also fails, deliberately retain the backup as
	// operator-recoverable evidence instead of deleting the only known-good
	// snapshot.
	cleanupBackup := false
	defer func() {
		if cleanupBackup {
			_ = os.Remove(backupPath)
		}
	}()
	if err := os.Rename(tmpPath, path); err != nil {
		if restoreErr := os.Rename(backupPath, path); restoreErr == nil {
			cleanupBackup = true
		}
		return err
	}
	cleanupBackup = true
	return nil
}

// restoreEvolutionCompensation applies one durable snapshot. It deliberately
// restores config before rebuilding the derived index; a failed config write
// therefore leaves the record pending rather than claiming recovery.
func restoreEvolutionCompensation(record EvolutionCompensationRecord, skillSaver func([]corelib.NLSkillEntry) error, indexRefresher func() error) error {
	if len(record.DirectoryMoves) > 0 {
		// Multi-package directory transactions are restored in reverse order.
		// An intent-only move is deliberately left untouched when both paths
		// still exist; recovery must not guess which copy is authoritative.
		for i := len(record.DirectoryMoves) - 1; i >= 0; i-- {
			move := record.DirectoryMoves[i]
			original := filepath.Clean(strings.TrimSpace(move.OriginalPath))
			backup := filepath.Clean(strings.TrimSpace(move.BackupPath))
			if original == "." || backup == "." || original == "" || backup == "" {
				continue
			}
			_, backupErr := os.Stat(backup)
			_, originalErr := os.Stat(original)
			backupExists := backupErr == nil
			originalExists := originalErr == nil
			if backupExists {
				if originalExists && !move.Moved && !move.Published {
					return fmt.Errorf("restore directory: ambiguous original and backup paths; manual review required")
				}
				if originalExists {
					if err := removeCompensationPath(original); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("remove published directory: %w", err)
					}
				}
				if err := os.MkdirAll(filepath.Dir(original), 0o755); err != nil {
					return err
				}
				if err := renameCompensationPath(backup, original); err != nil {
					return fmt.Errorf("restore directory: %w", err)
				}
			} else if move.Published && originalExists && !move.HadPrevious {
				if err := removeCompensationPath(original); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove created directory: %w", err)
				}
			}
		}
	} else {
		// A pre-move compensation intent may point at an existing installation but
		// must not remove that path. For multi-package records derive removals from
		// each movement flag instead of the legacy singular fields.
		if len(record.DirectoryMoves) == 0 {
			removeCreated := !record.DirHadPrevious || record.DirMoved || record.DirPublished
			for _, path := range record.CreatedDirs {
				path = strings.TrimSpace(path)
				if path == "" || !removeCreated {
					continue
				}
				if err := removeCompensationPath(path); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove created directory: %w", err)
				}
			}
		} else {
			for _, move := range record.DirectoryMoves {
				if move.HadPrevious && !move.Moved && !move.Published {
					continue
				}
				if !move.HadPrevious && !move.Published {
					continue
				}
				if move.HadPrevious {
					continue // restored below from BackupPath
				}
				if err := removeCompensationPath(move.OriginalPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove created directory: %w", err)
				}
			}
		}
	}
	if (record.DirMoved || record.DirHadPrevious) && strings.TrimSpace(record.DirPath) != "" && strings.TrimSpace(record.DirBackupPath) != "" {
		original := filepath.Clean(record.DirPath)
		moved := filepath.Clean(record.DirBackupPath)
		if _, err := os.Stat(moved); err == nil {
			originalExists := false
			if _, originalErr := os.Stat(original); originalErr == nil {
				originalExists = true
			}
			// An intent-only snapshot with both paths present is ambiguous: the
			// replacement may have been published before the enriched queue record
			// was appended, or an operator may have left an unrelated backup. Do
			// not guess by deleting either copy; retain the queue for manual review.
			if originalExists && !record.DirMoved && !record.DirPublished {
				return fmt.Errorf("restore directory: ambiguous original and backup paths; manual review required")
			}
			// If the original is absent, the move likely completed before the
			// queue update and the backup should be restored. A marked post-move
			// record may also have both paths, in which case replacing the new
			// directory with the retained previous version is authoritative.
			if !originalExists || record.DirMoved || record.DirPublished {
				if originalExists {
					if err := removeCompensationPath(original); err != nil {
						return fmt.Errorf("remove published directory: %w", err)
					}
				}
				if err := os.MkdirAll(filepath.Dir(original), 0o755); err != nil {
					return err
				}
				if err := renameCompensationPath(moved, original); err != nil {
					return fmt.Errorf("restore directory: %w", err)
				}
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect moved directory: %w", err)
		}
	}
	if record.YAMLExists {
		data, err := base64.StdEncoding.DecodeString(record.YAMLBackup)
		if err != nil {
			return fmt.Errorf("decode YAML backup: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(record.YAMLPath), 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(record.YAMLPath), ".skill-restore-*.tmp")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := renameCompensationPath(tmpPath, record.YAMLPath); err != nil {
			// Windows does not replace an existing destination with Rename.
			// Remove only the exact rollback target, then retry the same atomic
			// move; the durable queue still retains the pre-image if this fails.
			if removeErr := removeCompensationPath(record.YAMLPath); removeErr != nil && !os.IsNotExist(removeErr) {
				return fmt.Errorf("replace YAML rollback target: %w (initial rename: %v)", removeErr, err)
			}
			if retryErr := renameCompensationPath(tmpPath, record.YAMLPath); retryErr != nil {
				return fmt.Errorf("replace YAML rollback target after remove: %w (initial rename: %v)", retryErr, err)
			}
		}
	} else if strings.TrimSpace(record.YAMLPath) != "" {
		if err := os.Remove(record.YAMLPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if record.DraftExists {
		data, err := base64.StdEncoding.DecodeString(record.DraftBackup)
		if err != nil {
			return fmt.Errorf("decode draft backup: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(record.DraftPath), 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(record.DraftPath), ".draft-restore-*.tmp")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := renameCompensationPath(tmpPath, record.DraftPath); err != nil {
			if removeErr := removeCompensationPath(record.DraftPath); removeErr != nil && !os.IsNotExist(removeErr) {
				return fmt.Errorf("replace draft rollback target: %w (initial rename: %v)", removeErr, err)
			}
			if retryErr := renameCompensationPath(tmpPath, record.DraftPath); retryErr != nil {
				return fmt.Errorf("replace draft rollback target after remove: %w (initial rename: %v)", retryErr, err)
			}
		}
	} else if strings.TrimSpace(record.DraftPath) != "" {
		// The snapshot explicitly records that no draft existed before the
		// transaction. Remove a newly-created/partially-deleted draft during
		// recovery so a rejected operation cannot reappear after restart.
		if err := os.Remove(record.DraftPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := restoreEvolutionFileSnapshots(record.FileSnapshots); err != nil {
		return err
	}
	// Legacy GUI metadata transactions may carry only FileSnapshots and no
	// NLSkillEntry config pre-image. Do not reject those records merely because
	// the generic SkillSaver is unavailable; records that do not carry either a
	// config or file snapshot still require the authoritative saver because they
	// are ordinary pipeline compensation records.
	hasRestorePayload := len(record.ConfigBackup) > 0 || len(record.FileSnapshots) > 0 ||
		len(record.DirectoryMoves) > 0 || len(record.CreatedDirs) > 0 ||
		strings.TrimSpace(record.YAMLPath) != "" || strings.TrimSpace(record.DraftPath) != "" ||
		strings.TrimSpace(record.DirPath) != "" || strings.TrimSpace(record.DirBackupPath) != ""
	if len(record.ConfigBackup) > 0 || !hasRestorePayload {
		if skillSaver == nil {
			return fmt.Errorf("skill saver unavailable")
		}
		if err := skillSaver(cloneSkillEntries(record.ConfigBackup)); err != nil {
			return fmt.Errorf("restore config: %w", err)
		}
	}
	if indexRefresher != nil {
		if err := indexRefresher(); err != nil {
			return fmt.Errorf("refresh index: %w", err)
		}
	}
	if err := cleanupRollbackCompensation(record); err != nil {
		return err
	}
	return nil
}

// restoreEvolutionFileSnapshots restores bounded, explicitly captured files
// such as a legacy metadata registry. It is intentionally independent from
// SkillSaver because those files are not part of NLSkillEntry config. Missing
// pre-images remove only the exact target; no parent or sibling path is ever
// touched.
func restoreEvolutionFileSnapshots(snapshots []EvolutionFileSnapshot) error {
	for _, snapshot := range snapshots {
		path := strings.TrimSpace(snapshot.Path)
		if path == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("restore file snapshot: absolute path required")
		}
		if !snapshot.Exists {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove restored file snapshot: %w", err)
			}
			continue
		}
		data, err := base64.StdEncoding.DecodeString(snapshot.BackupB64)
		if err != nil {
			return fmt.Errorf("decode file snapshot: %w", err)
		}
		if len(data) > 2*1024*1024 {
			return fmt.Errorf("file snapshot exceeds size limit")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".skill-file-restore-*.tmp")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		ok := false
		defer func() {
			_ = tmp.Close()
			if !ok {
				_ = os.Remove(tmpPath)
			}
		}()
		if _, err := tmp.Write(data); err != nil {
			return err
		}
		if err := tmp.Sync(); err != nil {
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := renameCompensationPath(tmpPath, path); err != nil {
			if removeErr := removeCompensationPath(path); removeErr != nil && !os.IsNotExist(removeErr) {
				return fmt.Errorf("replace file snapshot: %w", removeErr)
			}
			if retryErr := renameCompensationPath(tmpPath, path); retryErr != nil {
				return fmt.Errorf("replace file snapshot: %w", retryErr)
			}
		}
		ok = true
	}
	return nil
}

// restoreExternalCompensation is deliberately a separate hook from the
// filesystem restore logic. External state must be restored before its
// corresponding Skill becomes routable again; callers own the concrete
// serialization and idempotent restore implementation.
func restoreExternalCompensation(record EvolutionCompensationRecord, restore func(EvolutionCompensationRecord) error) error {
	if len(record.ExternalSnapshots) == 0 {
		return nil
	}
	if restore == nil {
		return fmt.Errorf("external compensation restore unavailable")
	}
	// A snapshot is not proof that its side effect happened. New records mark
	// the transition before invoking the external mutation and persist that
	// marker; recovery restores only transitions that may have crossed the
	// boundary. Legacy records without markers remain conservative and restore
	// all snapshots because their exact boundary is unknowable.
	if len(record.ExternalApplied) == 0 {
		return restore(record)
	}
	filtered := record
	filtered.ExternalSnapshots = make(map[string]string)
	for key, value := range record.ExternalSnapshots {
		if record.ExternalApplied[key] {
			filtered.ExternalSnapshots[key] = value
		}
	}
	if len(filtered.ExternalSnapshots) == 0 {
		return nil
	}
	return restore(filtered)
}

// cleanupRollbackCompensation removes only artifacts explicitly created by a
// transaction that is being rolled back. It is intentionally separate from
// committed cleanup: a successful commit must preserve user-visible backups.
func cleanupRollbackCompensation(record EvolutionCompensationRecord) error {
	for _, path := range record.RollbackCleanupPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if err := removeCompensationPath(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove rollback cleanup path: %w", err)
		}
	}
	return nil
}

// Windows scanners can briefly keep skill.yaml open while a transaction is
// compensating a just-published directory.  Treat those short sharing
// violations as transient and retry the exact path operation before declaring
// durable rollback failure.  We never broaden the target or fall back to
// deleting a previous-version directory.
func removeCompensationPath(path string) error {
	return retryCompensationFSOp(func() error { return os.RemoveAll(path) })
}

func renameCompensationPath(oldPath, newPath string) error {
	return retryCompensationFSOp(func() error { return os.Rename(oldPath, newPath) })
}

// RetryDirectoryRename is the public filesystem compensation primitive used
// by GUI lifecycle adapters. Windows scanners may hold a short-lived handle;
// bounded retries preserve synchronous rollback semantics without deleting or
// broadening either target path.
func RetryDirectoryRename(oldPath, newPath string) error {
	return renameCompensationPath(oldPath, newPath)
}

func retryCompensationFSOp(op func() error) error {
	var lastErr error
	for attempt := 0; attempt < 40; attempt++ {
		if err := op(); err == nil || os.IsNotExist(err) {
			return err
		} else {
			lastErr = err
		}
		delay := time.Duration(attempt+1) * 10 * time.Millisecond
		if delay > 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		time.Sleep(delay)
	}
	return lastErr
}

// RecoverPendingCompensations retries durable rollback records. It is safe to
// call on every startup: successful records are removed atomically and failed
// records are retained with an incremented attempt count. After the bounded
// retry budget, records become needs_review and must be handled manually.
func (p *EvolutionPipeline) RecoverPendingCompensations() (recovered int, pending int, err error) {
	if p == nil {
		return 0, 0, nil
	}
	return p.recoverPendingCompensations("", "", "", p.ExternalRecovery)
}

func compensationRecordMatchesSkill(record EvolutionCompensationRecord, skillName string) bool {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(record.Skill), skillName) {
		return true
	}
	for _, affected := range record.AffectedSkills {
		if strings.EqualFold(strings.TrimSpace(affected), skillName) {
			return true
		}
	}
	return false
}

// compensationFinalAuditExists closes the narrow crash window between a
// successful strict final-audit append and persistence of transaction_state=
// committed. The queue record stores the expected audit kind; the durable
// audit row still has to match request ID, Skill, action and a terminal
// decision. A missing or unreadable audit is treated as not committed so the
// normal rollback path remains fail-closed.
func compensationFinalAuditExists(record EvolutionCompensationRecord) bool {
	expectedKind := strings.TrimSpace(record.FinalAuditKind)
	requestID := strings.TrimSpace(record.RequestID)
	if expectedKind == "" || requestID == "" {
		return false
	}
	events, err := ListEvolutionAudit(DefaultEvolutionAuditPath(), EvolutionAuditMaxKeep)
	if err != nil {
		return false
	}
	for _, event := range events {
		if !strings.EqualFold(strings.TrimSpace(event.RequestID), requestID) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(event.Kind), expectedKind) {
			continue
		}
		if skillName := strings.TrimSpace(record.Skill); skillName != "" && !strings.EqualFold(strings.TrimSpace(event.Skill), skillName) {
			continue
		}
		if action := strings.TrimSpace(record.Action); action != "" && !strings.EqualFold(strings.TrimSpace(event.Action), action) {
			continue
		}
		decision := strings.ToLower(strings.TrimSpace(event.Decision))
		// Only the two terminal success decisions currently emitted by
		// committed write paths are acceptable. Treat every other value as
		// non-committing (including future/unknown values) until its semantics
		// are explicitly reviewed; this keeps an audit schema extension from
		// silently authorizing destructive cleanup.
		if decision != "applied" && decision != "committed" {
			continue
		}
		return true
	}
	return false
}

// RecoverPendingEvolutionCompensationsForActionPrefix replays only records
// owned by a non-GUI host. This lets headless services recover their own
// directory transactions without touching another host's Skill registry.
func RecoverPendingEvolutionCompensationsForActionPrefix(prefix string, skillSaver func([]corelib.NLSkillEntry) error, indexRefresher func() error) (recovered int, pending int, err error) {
	p := &EvolutionPipeline{SkillSaver: skillSaver, IndexRefresher: indexRefresher}
	return p.recoverPendingCompensations(strings.TrimSpace(prefix), "", "", nil)
}

// RecoverPendingEvolutionCompensationsForActionPrefixAndSkill is the narrow
// legacy-adapter recovery entry point. A process-global queue may contain
// records for several Skills (and several GUI tools); an operation for one
// Skill must not claim or mutate another Skill's recovery record merely because
// the action prefix matches. The returned pending count includes only records
// matching the requested Skill.
func RecoverPendingEvolutionCompensationsForActionPrefixAndSkill(prefix, skillName string, skillSaver func([]corelib.NLSkillEntry) error, indexRefresher func() error) (recovered int, pending int, err error) {
	p := &EvolutionPipeline{SkillSaver: skillSaver, IndexRefresher: indexRefresher}
	return p.recoverPendingCompensations(strings.TrimSpace(prefix), "", strings.TrimSpace(skillName), nil)
}

// RecoverPendingEvolutionCompensationsForActionPrefixAndScope replays only
// records owned by the requested action prefix and persistence scope. This is
// used by multi-tenant/headless services sharing the process-global queue so
// startup recovery cannot mutate another service's directories.
func RecoverPendingEvolutionCompensationsForActionPrefixAndScope(prefix, scope string, skillSaver func([]corelib.NLSkillEntry) error, indexRefresher func() error) (recovered int, pending int, err error) {
	return RecoverPendingEvolutionCompensationsForActionPrefixAndScopeWithExternalRecovery(prefix, scope, skillSaver, indexRefresher, nil)
}

// RecoverPendingEvolutionCompensationsForActionPrefixAndScopeWithExternalRecovery
// is the scoped recovery entry point for services that mutate state outside
// the Skill directory/config (for example dynamic capability contracts).
// externalRecovery must be idempotent and restore only the record's opaque
// pre-image before filesystem/config rollback is attempted.
func RecoverPendingEvolutionCompensationsForActionPrefixAndScopeWithExternalRecovery(prefix, scope string, skillSaver func([]corelib.NLSkillEntry) error, indexRefresher func() error, externalRecovery func(EvolutionCompensationRecord) error) (recovered int, pending int, err error) {
	p := &EvolutionPipeline{SkillSaver: skillSaver, IndexRefresher: indexRefresher}
	return p.recoverPendingCompensations(strings.TrimSpace(prefix), normalizeRecoveryScope(scope), "", externalRecovery)
}

func (p *EvolutionPipeline) recoverPendingCompensations(actionPrefix string, recoveryScope string, skillFilter string, externalRecovery func(EvolutionCompensationRecord) error) (recovered int, pending int, err error) {
	if p == nil {
		return 0, 0, nil
	}
	p.compensationRecoveryMu.Lock()
	defer p.compensationRecoveryMu.Unlock()
	records, err := readEvolutionCompensations()
	if err != nil {
		return 0, 0, err
	}
	if len(records) == 0 {
		return 0, 0, nil
	}
	remaining := make([]EvolutionCompensationRecord, 0, len(records))
	matchedRemaining := 0
	for _, record := range records {
		if actionPrefix != "" && !strings.HasPrefix(strings.TrimSpace(record.Action), actionPrefix) {
			remaining = append(remaining, record)
			continue
		}
		if skillFilter != "" && !compensationRecordMatchesSkill(record, skillFilter) {
			remaining = append(remaining, record)
			continue
		}
		if recoveryScope != "" {
			recordScope := normalizeRecoveryScope(record.RecoveryScope)
			if recordScope == "" {
				// Legacy records can be safely claimed only when every durable
				// path proves ownership by this service root. Otherwise retain
				// them as pending rather than guessing across tenants/services.
				if !compensationRecordMatchesScope(record, recoveryScope) {
					remaining = append(remaining, record)
					matchedRemaining++
					continue
				}
				record.SetRecoveryScope(recoveryScope)
			} else if recordScope != recoveryScope {
				remaining = append(remaining, record)
				if compensationRecordContainsPathInScope(record, recoveryScope) {
					matchedRemaining++
				}
				continue
			}
			// The scope field is an ownership hint, not authority. Always
			// revalidate every durable path before touching the filesystem so a
			// forged or stale record cannot escape the service root.
			if !compensationRecordMatchesScope(record, recoveryScope) {
				remaining = append(remaining, record)
				matchedRemaining++
				continue
			}
		}
		// A process may crash after the strict final audit was appended but
		// before the queue row could be rewritten as committed. Use the matched
		// audit row as a narrowly-scoped commit marker; never infer commitment
		// from FinalAuditKind alone.
		if !strings.EqualFold(strings.TrimSpace(record.TransactionState), "committed") && compensationFinalAuditExists(record) {
			record.TransactionState = "committed"
			record.CleanupStatus = "pending"
			record.FailureReason = "post_commit_state_persist_recovered"
		}
		// Once final audit has crossed the business commit boundary, recovery
		// must never restore the pre-commit YAML/config snapshot. A crash can
		// leave only post-commit cleanup pending; retry that cleanup idempotently
		// and keep the Skill blocked until the queue entry is removed.
		if strings.EqualFold(strings.TrimSpace(record.TransactionState), "committed") {
			if err := cleanupCommittedCompensation(record); err != nil {
				record.Attempts++
				record.LastError = err.Error()
				record.CleanupStatus = "pending"
				if record.Attempts >= evolutionCompensationMaxAttempts {
					record.Status = "needs_review"
					record.CleanupStatus = "needs_review"
				}
				remaining = append(remaining, record)
				matchedRemaining++
				continue
			}
			if clearErr := ClearEvolutionCompensation(record.RequestID, record.Skill, record.Action); clearErr != nil {
				record.Attempts++
				record.LastError = clearErr.Error()
				record.CleanupStatus = "pending"
				if record.Attempts >= evolutionCompensationMaxAttempts {
					record.Status = "needs_review"
					record.CleanupStatus = "needs_review"
				}
				remaining = append(remaining, record)
				matchedRemaining++
				continue
			}
			recovered++
			RecordEvolutionEvent(EventSkillCompensationRecovered, map[string]string{
				"skill": record.Skill, "action": record.Action, "request_id": record.RequestID,
				"attempt": fmt.Sprintf("%d", record.Attempts+1), "decision": "cleanup_recovered",
				"reason": "post_commit_cleanup_applied", "schema_version": "2",
			}, "desktop")
			continue
		}
		if strings.TrimSpace(record.Status) == "needs_review" {
			// A previous attempt may have persisted the queue state but failed
			// while demoting the Skill (for example, config storage was briefly
			// unavailable). Retry the safe demotion on every startup; the queue
			// remains the source of truth and execution is fail-closed meanwhile.
			if markErr := p.markCompensationNeedsReview(record); markErr != nil {
				record.LastError = strings.TrimSpace(record.LastError + "; mark needs_review: " + markErr.Error())
			}
			remaining = append(remaining, record)
			matchedRemaining++
			continue
		}
		if err := restoreExternalCompensation(record, externalRecovery); err != nil {
			record.Attempts++
			record.LastError = err.Error()
			if record.Attempts >= evolutionCompensationMaxAttempts {
				record.Status = "needs_review"
			}
			remaining = append(remaining, record)
			matchedRemaining++
			continue
		}
		if err := restoreEvolutionCompensation(record, p.SkillSaver, p.IndexRefresher); err != nil {
			record.Attempts++
			record.LastError = err.Error()
			if record.Attempts >= evolutionCompensationMaxAttempts {
				record.Status = "needs_review"
			}
			// A pending/incomplete rollback must never leave an active Skill
			// executable while recovery is unavailable. Best-effort demotion is
			// safe to repeat; the durable record remains the source of truth when
			// this save itself is unavailable.
			if record.Status == "needs_review" {
				if markErr := p.markCompensationNeedsReview(record); markErr != nil {
					record.LastError += "; mark needs_review: " + markErr.Error()
				}
				RecordEvolutionEvent(EventSkillCompensationNeedsReview, map[string]string{
					"skill": record.Skill, "action": record.Action, "request_id": record.RequestID,
					"attempt": fmt.Sprintf("%d", record.Attempts), "decision": "needs_review",
					"reason": "compensation_retry_exhausted", "failure_reason": record.LastError,
					"schema_version": "2",
				}, "desktop")
			}
			remaining = append(remaining, record)
			matchedRemaining++
			continue
		}
		recovered++
		RecordEvolutionEvent(EventSkillCompensationRecovered, map[string]string{
			"skill": record.Skill, "action": record.Action, "request_id": record.RequestID,
			"attempt": fmt.Sprintf("%d", record.Attempts+1), "decision": "recovered",
			"reason": "durable_compensation_applied", "schema_version": "2",
		}, "desktop")
	}
	if err := writeEvolutionCompensations(remaining); err != nil {
		return recovered, len(remaining), err
	}
	if recoveryScope != "" || skillFilter != "" {
		return recovered, matchedRemaining, nil
	}
	return recovered, len(remaining), nil
}

// cleanupCommittedCompensation only removes artifacts explicitly marked as
// post-commit cleanup. It intentionally does not call SkillSaver,
// IndexRefresher, or restore YAML/config: those operations would violate the
// committed transaction boundary after a restart.
func cleanupCommittedCompensation(record EvolutionCompensationRecord) error {
	for _, move := range record.DirectoryMoves {
		if move.Moved && strings.TrimSpace(move.BackupPath) != "" {
			if err := removeCompensationPath(move.BackupPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove committed directory backup: %w", err)
			}
		}
	}
	if record.DirMoved && strings.TrimSpace(record.DirBackupPath) != "" {
		if err := removeCompensationPath(record.DirBackupPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove committed backup: %w", err)
		}
	}
	for _, path := range record.PostCommitCleanupPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if err := removeCompensationPath(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove committed cleanup path: %w", err)
		}
	}
	return nil
}

// HasPendingCompensation is a fail-closed admission check used by execution
// and upload callers. An unreadable queue is treated as pending because the
// system cannot prove that the Skill is consistent.
func (p *EvolutionPipeline) HasPendingCompensation(skillName string) bool {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return false
	}
	records, err := readEvolutionCompensations()
	if err != nil {
		return true
	}
	for _, record := range records {
		if strings.EqualFold(strings.TrimSpace(record.Skill), skillName) {
			return true
		}
		for _, affected := range record.AffectedSkills {
			if strings.EqualFold(strings.TrimSpace(affected), skillName) {
				return true
			}
		}
	}
	return false
}

// HasPendingCompensationForScope is the scoped variant used by headless
// services. A record without a scope is treated as pending for safety because
// ownership cannot be proven after a service split or migration.
func (p *EvolutionPipeline) HasPendingCompensationForScope(skillName, scope string) bool {
	skillName = strings.TrimSpace(skillName)
	scope = normalizeRecoveryScope(scope)
	if skillName == "" || scope == "" {
		return true
	}
	records, err := readEvolutionCompensations()
	if err != nil {
		return true
	}
	for _, record := range records {
		matches := strings.EqualFold(strings.TrimSpace(record.Skill), skillName)
		if !matches {
			for _, affected := range record.AffectedSkills {
				if strings.EqualFold(strings.TrimSpace(affected), skillName) {
					matches = true
					break
				}
			}
		}
		if !matches {
			continue
		}
		recordScope := normalizeRecoveryScope(record.RecoveryScope)
		if recordScope == "" {
			// Legacy records are unsafe to attribute; a path under this root is
			// enough to block, while a path outside it is ignored for this scope.
			if compensationRecordContainsPathInScope(record, scope) {
				return true
			}
			continue
		}
		if recordScope == scope || compensationRecordContainsPathInScope(record, scope) {
			// Scope metadata is not authoritative. A matching scope with an
			// out-of-root durable path is itself suspicious and must block.
			if !compensationRecordMatchesScope(record, scope) {
				return true
			}
			return true
		}
	}
	return false
}

func (p *EvolutionPipeline) markCompensationNeedsReview(record EvolutionCompensationRecord) error {
	if p == nil || p.SkillLoader == nil || p.SkillSaver == nil || strings.TrimSpace(record.Skill) == "" {
		return nil
	}
	skills := p.SkillLoader()
	changed := false
	for i := range skills {
		matches := strings.EqualFold(strings.TrimSpace(skills[i].Name), strings.TrimSpace(record.Skill))
		if !matches {
			for _, affected := range record.AffectedSkills {
				if strings.EqualFold(strings.TrimSpace(skills[i].Name), strings.TrimSpace(affected)) {
					matches = true
					break
				}
			}
		}
		if !matches {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(skills[i].Status), "needs_review") {
			return nil
		}
		skills[i].Status = "needs_review"
		message := "audit_pending compensation requires review"
		if strings.TrimSpace(record.LastError) != "" {
			message += ": " + record.LastError
		}
		if len(message) > 512 {
			message = message[:512]
		}
		skills[i].LastError = message
		changed = true
		break
	}
	if !changed {
		return nil
	}
	if err := p.SkillSaver(skills); err != nil {
		return err
	}
	if p.IndexRefresher != nil {
		return p.IndexRefresher()
	}
	return nil
}
