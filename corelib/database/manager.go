package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/excel"
	mysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

type Manager struct {
	mu                sync.Mutex
	closed            bool
	profiles          map[string]Profile
	secrets           SecretResolver
	items             map[string]Adapter
	lastUsed          map[string]time.Time
	idleTTL           time.Duration
	audit             AuditSink
	bindings          map[string]connectionBinding
	profileIDs        map[string]string
	profileSlots      map[string]chan struct{}
	workspaceRoot     string
	resultDir         string
	results           map[string]storedResult
	consumedApprovals map[string]time.Time
	// pending, when set, makes approval consumption strict: only tokens minted
	// by IssueApproval (and still present as pending mutations) are accepted.
	// nil keeps the legacy host-issued token flow for hosts that have not
	// migrated to unified issuance yet.
	pending             *PendingStore
	enabled             bool
	issueGate           IssueGate
	strictOperationGate bool
	operationStates     map[string]operationGateState
	metrics             runtimeMetrics
	migrationError      string
	tunnel              TunnelDialer
	jobs                map[string]*asyncJob
	favorites           *FavoriteStore
	catalogNames        map[string][]string
	catalogPath         string
}

type connectionBinding struct{ ownerID, sessionID string }

const maxResultBytes = 1_500_000
const resultTTL = 5 * time.Minute

type storedResult struct {
	Columns       []Column
	Rows          [][]interface{}
	Offset        int
	OwnerID       string
	SessionID     string
	ProfileID     string
	ConnectionID  string
	SchemaVersion int
	Expires       time.Time
}

// SetAuditSink installs a metadata-only audit callback. It is safe to replace
// at runtime (for example when a tenant changes its audit destination).
func (m *Manager) SetAuditSink(sink AuditSink) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.audit = sink
	m.mu.Unlock()
}

// SetPendingStore switches the manager to strict approval issuance: commits
// only accept one-time tokens minted by IssueApproval and persisted as
// pending mutations. Passing nil restores the legacy host-token flow. Hosts
// should wire this together with the same data directory used for the audit
// store (for example database_pending.json next to database_audit.jsonl).
func (m *Manager) SetPendingStore(store *PendingStore) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.pending = store
	m.mu.Unlock()
}

func (m *Manager) emitAudit(ctx context.Context, event AuditEvent) {
	m.mu.Lock()
	sink := m.audit
	if event.ProfileID == "" && event.ConnectionID != "" {
		event.ProfileID = m.profileIDs[event.ConnectionID]
	}
	m.mu.Unlock()
	if sink != nil {
		scope := requestScopeFromContext(ctx)
		if event.OwnerID == "" {
			event.OwnerID = scope.OwnerID
		}
		if event.SessionID == "" {
			event.SessionID = scope.SessionID
		}
		if event.OperationID == "" {
			event.OperationID = scope.OperationID
		}
		if event.Attempt == 0 {
			event.Attempt = scope.Attempt
		}
		if event.ParentActionID == "" {
			event.ParentActionID = scope.ParentActionID
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now().UTC()
		}
		sink(ctx, event)
	}
}

func sqlFingerprint(sqlText string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sqlText)))
	return hex.EncodeToString(sum[:])
}

func NewManager(profiles []Profile, resolver SecretResolver) *Manager {
	m := &Manager{profiles: make(map[string]Profile), secrets: resolver, items: make(map[string]Adapter), lastUsed: make(map[string]time.Time), idleTTL: 10 * time.Minute, bindings: make(map[string]connectionBinding), profileIDs: make(map[string]string), profileSlots: make(map[string]chan struct{}), results: make(map[string]storedResult), consumedApprovals: make(map[string]time.Time), operationStates: make(map[string]operationGateState), jobs: make(map[string]*asyncJob), catalogNames: make(map[string][]string), enabled: true}
	m.UpdateProfiles(profiles)
	return m
}

// UpdateProfiles atomically refreshes non-secret profile configuration. Any
// connection whose profile was removed or changed is closed and its cursors
// invalidated; unchanged profiles keep their live sessions. Hosts should call
// this after a profile CRUD/rotation event so stale credentials and policy
// snapshots cannot remain usable.
func (m *Manager) UpdateProfiles(profiles []Profile) {
	if m == nil {
		return
	}
	next := make(map[string]Profile)
	if err := PlaintextSecretError(profiles); err != nil {
		m.mu.Lock()
		if !m.closed {
			m.migrationError = err.Error()
			m.enabled = false
		}
		m.mu.Unlock()
		return
	}
	for _, p := range profiles {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		if _, exists := next[id]; exists {
			continue
		}
		next[id] = p
	}
	var closeAdapters []Adapter
	changedProfiles := make(map[string]bool)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.migrationError = ""
	for id, old := range m.profiles {
		if updated, exists := next[id]; exists && reflect.DeepEqual(old, updated) {
			continue
		}
		changedProfiles[id] = true
		for connectionID, profileID := range m.profileIDs {
			if profileID != id {
				continue
			}
			if adapter := m.items[connectionID]; adapter != nil {
				closeAdapters = append(closeAdapters, adapter)
			}
			delete(m.items, connectionID)
			delete(m.lastUsed, connectionID)
			delete(m.bindings, connectionID)
			delete(m.profileIDs, connectionID)
			m.invalidateResultsForConnectionLocked(connectionID)
		}
	}
	oldProfiles, oldSlots := m.profiles, m.profileSlots
	m.profiles = next
	m.profileSlots = make(map[string]chan struct{}, len(next))
	for id := range next {
		if oldProfiles != nil && reflect.DeepEqual(oldProfiles[id], next[id]) && oldSlots != nil && oldSlots[id] != nil {
			m.profileSlots[id] = oldSlots[id]
		} else {
			m.profileSlots[id] = make(chan struct{}, 4)
		}
	}
	pending := m.pending
	m.mu.Unlock()
	if pending != nil && len(changedProfiles) > 0 {
		// Approvals issued against a changed/removed profile configuration must
		// never commit; destroy their pending mutations.
		pending.PurgeProfiles(changedProfiles)
	}
	for _, adapter := range closeAdapters {
		_ = adapter.Close()
	}
}

// invalidateResultsForConnectionLocked removes in-memory and persisted cursors
// bound to a connection. Caller must hold m.mu. Disk files are removed so a
// later process cannot resurrect a cursor after an explicit disconnect.
func (m *Manager) invalidateResultsForConnectionLocked(connectionID string) {
	for token, result := range m.results {
		if result.ConnectionID == connectionID {
			delete(m.results, token)
			m.deletePersistedResultLocked(token)
		}
	}
}

// consumeApproval atomically marks a host-issued approval as spent. This
// prevents replay of a one-time confirmation across concurrent tool calls.
// In strict mode (a PendingStore is configured) the caller-computed
// operationFingerprint must match the fingerprint bound at issuance, and a
// pending mutation bound to an owner/session may only be consumed by a
// request carrying the same scope.
func (m *Manager) consumeApproval(ctx context.Context, approval ApprovalContext, operationFingerprint string) error {
	id := strings.TrimSpace(approval.ID)
	if id == "" {
		return fmt.Errorf("permission: approval context is required")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	pending := m.pending
	m.mu.Unlock()
	if pending != nil {
		// The approval must commit exactly the operation the caller is about to
		// run; a mismatch means the approval was re-aimed at a different
		// statement after issuance.
		if fp := strings.TrimSpace(operationFingerprint); fp != "" && !strings.EqualFold(fp, approval.SQLFingerprint) {
			return fmt.Errorf("permission: approval context does not match the operation fingerprint")
		}
		// Strict mode: the approval must be a live pending mutation minted by
		// IssueApproval; consumption destroys it so it can never be replayed,
		// including after a process restart.
		item, err := pending.Consume(id, approvalTokenHash(approval.Token), approval.SQLFingerprint, approval.ParamsFingerprint, approval.ProfileID, approval.SchemaVersion)
		if err != nil {
			return err
		}
		// An owner/session-bound pending mutation may only be consumed by the
		// same request scope. Consume already destroyed the record, so a
		// mismatched attempt also burns the approval (fail closed).
		if item.OwnerID != "" {
			scope := requestScopeFromContext(ctx)
			if item.OwnerID != scope.OwnerID || item.SessionID != scope.SessionID {
				return fmt.Errorf("permission: approval context belongs to another session")
			}
		}
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.consumedApprovals == nil {
		m.consumedApprovals = make(map[string]time.Time)
	}
	for key, expiry := range m.consumedApprovals {
		if !expiry.After(now) {
			delete(m.consumedApprovals, key)
		}
	}
	// The approval ID is the one-time capability. The fingerprint is accepted
	// only to make the audit/debug path explicit; authorization is never split
	// into reusable per-operation sub-capabilities. (Strict mode above does
	// enforce the fingerprint binding; the legacy flow cannot, because no
	// issuance-time record exists.)
	_ = operationFingerprint
	key := id
	if _, exists := m.consumedApprovals[key]; exists {
		return fmt.Errorf("permission: approval context has already been consumed")
	}
	expiry := approval.ExpiresAt
	if expiry.IsZero() {
		expiry = now.Add(resultTTL)
	}
	m.consumedApprovals[key] = expiry
	return nil
}

func (m *Manager) acquireProfile(ctx context.Context, profileID string) (func(), error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrManagerClosed
	}
	slots := m.profileSlots[profileID]
	m.mu.Unlock()
	if slots == nil {
		return func() {}, nil
	}
	// A cancelled caller must be rejected deterministically: select would
	// otherwise randomly pick the ready slot case and let a dead request run.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cancelled: %v", err)
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		// Deliberately distinct from the pre-check above: the caller was live
		// at entry but its deadline expired while waiting for a slot, i.e. the
		// request could not obtain quota in time. Pre-cancelled callers report
		// "cancelled"; slot-wait timeouts report "quota_exceeded".
		return nil, fmt.Errorf("quota_exceeded: profile concurrency limit")
	}
}

func (m *Manager) storeResult(result QueryResult, ownerID, sessionID string) string {
	if len(result.allRows) <= len(result.Rows) {
		return ""
	}
	return m.storeResultAt(result, ownerID, sessionID, 0)
}

func (m *Manager) storeResultForConnection(result QueryResult, ownerID, sessionID, connectionID string, offset int) string {
	return m.storeResultAtWithConnection(result, ownerID, sessionID, connectionID, offset)
}

// storeResultAt stores an in-memory, bounded result and records the first
// unread row. The public helper starts at row zero; query responses use the
// page-aware variant below so the page already returned to the caller is not
// repeated when its next_cursor is consumed.
func (m *Manager) storeResultAt(result QueryResult, ownerID, sessionID string, offset int) string {
	return m.storeResultAtWithConnection(result, ownerID, sessionID, "", offset)
}

func (m *Manager) storeResultAtWithConnection(result QueryResult, ownerID, sessionID, connectionID string, offset int) string {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(result.allRows) {
		return ""
	}
	id := "db-result-" + randomID()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ""
	}
	if m.results == nil {
		m.results = make(map[string]storedResult)
	}
	rows := make([][]interface{}, len(result.allRows))
	copy(rows, result.allRows)
	schemaVersion := 0
	if result.ProfileID != "" {
		schemaVersion = m.profiles[result.ProfileID].SchemaVersion
	}
	stored := storedResult{Columns: append([]Column(nil), result.Columns...), Rows: rows, Offset: offset, OwnerID: ownerID, SessionID: sessionID, ProfileID: result.ProfileID, ConnectionID: connectionID, SchemaVersion: schemaVersion, Expires: time.Now().Add(resultTTL)}
	m.results[id] = stored
	m.mu.Unlock()
	m.persistStoredResult(id, stored)
	return id
}

// authorizeStoredResultLocked checks owner/session and optional connection
// binding. An empty connectionID means "use the handle's stored connection"
// so export and paging can consume a disk snapshot without repeating the
// original connection_id. A mismatched non-empty connectionID is denied.
func (m *Manager) authorizeStoredResultLocked(stored storedResult, ownerID, sessionID, connectionID string, loadedFromDisk bool) error {
	if stored.OwnerID != "" && (stored.OwnerID != ownerID || stored.SessionID != sessionID) {
		return fmt.Errorf("permission: result handle belongs to another session")
	}
	if stored.ConnectionID != "" {
		if connectionID != "" && stored.ConnectionID != connectionID {
			return fmt.Errorf("permission: result handle belongs to another connection")
		}
		binding, connected := m.bindings[stored.ConnectionID]
		if connected {
			if binding.ownerID != "" && (binding.ownerID != ownerID || binding.sessionID != sessionID) {
				return fmt.Errorf("permission: result handle belongs to another session")
			}
		} else if !loadedFromDisk {
			return fmt.Errorf("connection: result handle connection is closed")
		}
	}
	if stored.ProfileID != "" && stored.SchemaVersion != 0 {
		if current := m.profiles[stored.ProfileID].SchemaVersion; current != stored.SchemaVersion {
			return fmt.Errorf("connection: result handle invalidated by profile schema change")
		}
	}
	return nil
}

func (m *Manager) readResultPage(token, ownerID, sessionID string, limit int) (QueryResult, error) {
	return m.readResultPageFor(token, ownerID, sessionID, "", limit)
}

func (m *Manager) readResultPageFor(token, ownerID, sessionID, connectionID string, limit int) (QueryResult, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return QueryResult{}, ErrManagerClosed
	}
	stored, ok := m.results[token]
	loadedFromDisk := false
	if !ok {
		m.mu.Unlock()
		if loaded, loadedOK := m.loadStoredResult(token); loadedOK {
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				return QueryResult{}, ErrManagerClosed
			}
			if m.results == nil {
				m.results = make(map[string]storedResult)
			}
			m.results[token] = loaded
			stored, ok = loaded, true
			loadedFromDisk = true
		} else {
			m.mu.Lock()
		}
	}
	if ok && !stored.Expires.After(time.Now()) {
		delete(m.results, token)
		m.deletePersistedResultLocked(token)
		ok = false
	}
	if ok {
		if err := m.authorizeStoredResultLocked(stored, ownerID, sessionID, connectionID, loadedFromDisk); err != nil {
			if strings.HasPrefix(err.Error(), "connection:") {
				delete(m.results, token)
				m.deletePersistedResultLocked(token)
			}
			m.mu.Unlock()
			return QueryResult{}, err
		}
	}
	if !ok {
		m.mu.Unlock()
		return QueryResult{}, fmt.Errorf("connection: result handle not found or expired")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 5000 {
		limit = 5000
	}
	start, end := stored.Offset, stored.Offset+limit
	if end > len(stored.Rows) {
		end = len(stored.Rows)
	}
	page := QueryResult{ContractVersion: ContractVersion, ProfileID: stored.ProfileID, ConnectionID: stored.ConnectionID, Columns: append([]Column(nil), stored.Columns...), Rows: append([][]interface{}(nil), stored.Rows[start:end]...), RowCount: end - start}
	stored.Offset = end
	persistAfter := stored
	persistToken := token
	keep := end < len(stored.Rows)
	if keep {
		m.results[token] = stored
		page.Truncated = true
		page.NextCursor = token
	} else {
		delete(m.results, token)
		m.deletePersistedResultLocked(token)
	}
	m.mu.Unlock()
	if keep {
		m.persistStoredResult(persistToken, persistAfter)
	}
	return page, nil
}

const maxExportHandleRows = 100_000

// SnapshotResultHandle returns the full materialized result bound to a
// result_handle without advancing or deleting the cursor. Export uses this so
// paging and export can share one encrypted snapshot.
func (m *Manager) SnapshotResultHandle(token, ownerID, sessionID, connectionID string) (QueryResult, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return QueryResult{}, fmt.Errorf("syntax: result_handle is required")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return QueryResult{}, ErrManagerClosed
	}
	stored, ok := m.results[token]
	loadedFromDisk := false
	if !ok {
		m.mu.Unlock()
		if loaded, loadedOK := m.loadStoredResult(token); loadedOK {
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				return QueryResult{}, ErrManagerClosed
			}
			if m.results == nil {
				m.results = make(map[string]storedResult)
			}
			m.results[token] = loaded
			stored, ok = loaded, true
			loadedFromDisk = true
		} else {
			m.mu.Lock()
		}
	}
	if ok && !stored.Expires.After(time.Now()) {
		delete(m.results, token)
		m.deletePersistedResultLocked(token)
		ok = false
	}
	if ok {
		if err := m.authorizeStoredResultLocked(stored, ownerID, sessionID, connectionID, loadedFromDisk); err != nil {
			m.mu.Unlock()
			return QueryResult{}, err
		}
	}
	if !ok {
		m.mu.Unlock()
		return QueryResult{}, fmt.Errorf("connection: result handle not found or expired")
	}
	if len(stored.Rows) > maxExportHandleRows {
		m.mu.Unlock()
		return QueryResult{}, fmt.Errorf("quota_exceeded: result handle has %d rows (limit %d)", len(stored.Rows), maxExportHandleRows)
	}
	out := QueryResult{
		ContractVersion: ContractVersion,
		ProfileID:       stored.ProfileID,
		ConnectionID:    stored.ConnectionID,
		Columns:         append([]Column(nil), stored.Columns...),
		Rows:            append([][]interface{}(nil), stored.Rows...),
		RowCount:        len(stored.Rows),
		ResultHandle:    token,
	}
	m.mu.Unlock()
	return out, nil
}

func (m *Manager) Profiles() []Profile {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	out := make([]Profile, 0, len(m.profiles))
	for _, p := range m.profiles {
		p.SecretRef = ""
		// DSNs may embed passwords or access tokens even when the profile also
		// has a secret_ref. Never project the raw connection string to the model.
		p.DSN = ""
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// HasBoundSecret reports whether a profile exists with a non-empty secret_ref.
// It does not return or log the secret.
func (m *Manager) HasBoundSecret(id string) bool {
	if m == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	p, ok := m.profiles[id]
	return ok && strings.TrimSpace(p.SecretRef) != "" && !p.Disabled
}

func (m *Manager) ProfileSummaries() []ProfileSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	out := make([]ProfileSummary, 0, len(m.profiles))
	for _, p := range m.profiles {
		status := "configured"
		if p.Disabled {
			status = "disabled"
		} else if err := ValidateProfile(p); err != nil {
			status = "invalid"
		}
		out = append(out, ProfileSummary{
			ID: p.ID, Name: p.Name, Type: p.Type, Status: status,
			Host: p.Host, Port: p.Port, Database: p.Database, Username: p.Username,
			ReadOnly: p.ReadOnly, WriteEnabled: p.WriteEnabled,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// SetIdleTTL changes the inactivity window for session-scoped connections.
// A non-positive value disables automatic expiry (useful for short-lived test
// managers); production hosts should keep the default bounded TTL.
func (m *Manager) SetIdleTTL(ttl time.Duration) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.idleTTL = ttl
	m.mu.Unlock()
}

// SetWorkspaceRoot configures the owner workspace boundary used by local
// spreadsheet actions. Empty keeps the conservative relative-path policy.
func (m *Manager) SetWorkspaceRoot(root string) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	root = strings.TrimSpace(root)
	if root == "" {
		m.workspaceRoot = ""
	} else {
		m.workspaceRoot = filepath.Clean(root)
	}
	m.mu.Unlock()
}

func (m *Manager) resolvePath(path string, trustedProfile bool) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path_denied: empty path")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrManagerClosed
	}
	root := m.workspaceRoot
	m.mu.Unlock()
	if root == "" {
		if !trustedProfile {
			if err := validateLocalDataPath(path); err != nil {
				return "", err
			}
		}
		return path, nil
	}
	abs, err := filepath.Abs(path)
	if !filepath.IsAbs(path) {
		abs = filepath.Join(root, filepath.Clean(path))
	}
	if err != nil {
		return "", fmt.Errorf("path_denied: %w", err)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("path_denied: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path_denied: path escapes the owner workspace")
	}
	// The lexical check above is not enough: a symlink inside the workspace can
	// point outside it. Resolve symlinks on both sides (evaluating the deepest
	// existing ancestor when the target does not exist yet) and re-check
	// containment, mirroring toolresult.validateResolvedStorePath.
	resolvedAbs, err := resolvePathSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("path_denied: %w", err)
	}
	resolvedRoot, err := resolvePathSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("path_denied: %w", err)
	}
	rel, err = filepath.Rel(resolvedRoot, resolvedAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path_denied: path escapes the owner workspace")
	}
	return abs, nil
}

// resolvePathSymlinks resolves symlinks (and, on Windows, directory
// junctions — filepath.EvalSymlinks does not follow those) for path. When
// path itself does not exist (for example a new export target), the deepest
// existing ancestor is resolved and the unresolved suffix is reattached so
// the containment check still sees the real location.
func resolvePathSymlinks(path string) (string, error) {
	return resolvePathSymlinksDepth(filepath.Clean(path), 0)
}

func resolvePathSymlinksDepth(path string, depth int) (string, error) {
	if depth > 40 {
		return "", fmt.Errorf("path_denied: symlink depth limit exceeded")
	}
	vol := filepath.VolumeName(path)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path_denied: path is not absolute")
	}
	rest := strings.TrimPrefix(path[len(vol):], string(filepath.Separator))
	resolved := vol + string(filepath.Separator)
	parts := strings.Split(rest, string(filepath.Separator))
	for i, part := range parts {
		if part == "" {
			continue
		}
		candidate := filepath.Join(resolved, part)
		fi, err := os.Lstat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				// The remaining tail does not exist yet (new file); every
				// existing prefix above has been fully resolved.
				return filepath.Join(append([]string{resolved}, parts[i:]...)...), nil
			}
			return "", fmt.Errorf("path_denied: %w", err)
		}
		resolved = candidate
		// A junction/mount point is reported by Go's Lstat as neither a symlink
		// nor a directory, so probe every component with Readlink: success
		// means the component is a reparse point that redirects elsewhere and
		// must be followed.
		target, rerr := os.Readlink(candidate)
		if rerr == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(resolved), target)
			}
			return resolvePathSymlinksDepth(filepath.Join(append([]string{target}, parts[i+1:]...)...), depth+1)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path_denied: unreadable link: %w", rerr)
		}
	}
	return resolved, nil
}

func (m *Manager) Connect(ctx context.Context, profileID string) (string, Capabilities, error) {
	return m.ConnectFor(ctx, profileID, "", "")
}

// ConnectFor binds a connection to an owner/session pair. Empty values are
// accepted only for legacy hosts; new hosts should always pass both IDs.
func (m *Manager) ConnectFor(ctx context.Context, profileID, ownerID, sessionID string) (string, Capabilities, error) {
	m.metrics.connectAttempts.Add(1)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", Capabilities{}, ErrManagerClosed
	}
	if !m.enabled || m.migrationError != "" || !SchemaContractLocked() {
		reason := m.migrationError
		enabled := m.enabled
		m.mu.Unlock()
		m.noteDenial()
		if !SchemaContractLocked() {
			return "", Capabilities{}, fmt.Errorf("permission: database tool disabled: schema hash mismatch")
		}
		if reason != "" {
			return "", Capabilities{}, fmt.Errorf("permission: %s", reason)
		}
		if !enabled {
			return "", Capabilities{}, fmt.Errorf("permission: database tool is disabled")
		}
		return "", Capabilities{}, fmt.Errorf("permission: database tool is disabled")
	}
	p, ok := m.profiles[profileID]
	dial := m.tunnel
	m.mu.Unlock()
	if !ok {
		m.noteDenial()
		return "", Capabilities{}, fmt.Errorf("profile_not_found")
	}
	if p.Disabled {
		m.noteDenial()
		return "", Capabilities{}, fmt.Errorf("permission: profile is disabled")
	}
	if err := ValidateProfile(p); err != nil {
		m.noteDenial()
		return "", Capabilities{}, err
	}
	if err := authorizeProfileEndpoint(p); err != nil {
		m.noteDenial()
		return "", Capabilities{}, err
	}
	adapter, err := openProfileWithTunnel(ctx, p, m.secrets, dial)
	if err != nil {
		return "", Capabilities{}, err
	}
	if err := adapter.Ping(ctx); err != nil {
		adapter.Close()
		return "", Capabilities{}, classify(err)
	}
	if profileUsesReplica(p) {
		replica, replicaErr := openReplicaAdapter(ctx, p, m.secrets, dial)
		routed := &routedAdapter{primary: adapter}
		if replicaErr != nil {
			routed.replicaUnavailable = true
		} else {
			routed.replica = replica
		}
		adapter = routed
	}
	id := randomID()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = adapter.Close()
		return "", Capabilities{}, ErrManagerClosed
	}
	m.items[id] = adapter
	m.lastUsed[id] = time.Now()
	m.bindings[id] = connectionBinding{ownerID: ownerID, sessionID: sessionID}
	m.profileIDs[id] = profileID
	m.mu.Unlock()
	m.metrics.connectSuccess.Add(1)
	return id, adapter.Capabilities(), nil
}

func ValidateProfile(p Profile) error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("syntax: profile id is required")
	}
	switch p.Type {
	case SourceMySQL, SourcePostgres, SourceSQLServer:
		if strings.TrimSpace(p.Host) == "" || strings.TrimSpace(p.Database) == "" {
			return fmt.Errorf("syntax: host and database are required")
		}
	case SourceAccess, SourceExcel:
		if strings.TrimSpace(p.FilePath) == "" {
			return fmt.Errorf("path_denied: file_path is required")
		}
	default:
		return fmt.Errorf("unsupported_source_type: %s", p.Type)
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("syntax: invalid port")
	}
	if strings.TrimSpace(p.SSHSessionID) != "" {
		switch p.Type {
		case SourceMySQL, SourcePostgres, SourceSQLServer:
		default:
			return fmt.Errorf("unsupported_capability: ssh tunnel is only supported for mysql, postgres and sqlserver")
		}
	}
	if profileUsesReplica(p) {
		switch p.Type {
		case SourceMySQL, SourcePostgres, SourceSQLServer:
		default:
			return fmt.Errorf("unsupported_capability: read replicas are only supported for mysql, postgres and sqlserver")
		}
		if p.ReplicaPort < 0 || p.ReplicaPort > 65535 {
			return fmt.Errorf("syntax: invalid replica_port")
		}
	}
	// Credentials must flow through secret_ref resolution, never sit inline in
	// a stored/profiled connection string.
	if dsnEmbedsCredentials(p.DSN) {
		return fmt.Errorf("permission: DSN must not embed credentials; use secret_ref")
	}
	if strings.TrimSpace(p.Password) != "" {
		return fmt.Errorf("authentication: plaintext password must be migrated to secret_ref")
	}
	switch normalizeTLSMode(p.TLS.Mode) {
	case "", "disable", "require", "verify-full":
	default:
		return fmt.Errorf("syntax: tls.mode must be disable, require or verify-full")
	}
	if ca := strings.TrimSpace(p.TLS.CAFile); ca != "" && normalizeTLSMode(p.TLS.Mode) == "disable" {
		return fmt.Errorf("syntax: tls.ca_file requires tls.mode require or verify-full")
	}
	return nil
}

// dsnEmbedsCredentials reports whether a connection string carries a password
// inline, either as a PWD=/PASSWORD= key-value pair (case-insensitive) or as
// URL userinfo in the ://user:pass@ form.
func dsnEmbedsCredentials(dsn string) bool {
	if strings.TrimSpace(dsn) == "" {
		return false
	}
	lower := strings.ToLower(dsn)
	if strings.Contains(lower, "pwd=") || strings.Contains(lower, "password=") {
		return true
	}
	if i := strings.Index(dsn, "://"); i >= 0 {
		rest := dsn[i+3:]
		if at := strings.Index(rest, "@"); at > 0 && strings.Contains(rest[:at], ":") {
			return true
		}
	}
	return false
}

func (m *Manager) AdapterFor(id, ownerID, sessionID string) (Adapter, bool) {
	if m == nil || m.IsClosed() {
		return nil, false
	}
	a, ok := m.Adapter(id)
	if !ok {
		return nil, false
	}
	m.mu.Lock()
	binding := m.bindings[id]
	m.mu.Unlock()
	if binding.ownerID != "" && (binding.ownerID != ownerID || binding.sessionID != sessionID) {
		return nil, false
	}
	return a, true
}

func (m *Manager) ProfileIDForConnection(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ""
	}
	return m.profileIDs[id]
}

func (m *Manager) ProfileForConnection(id string) (Profile, bool) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Profile{}, false
	}
	profileID := m.profileIDs[id]
	p, ok := m.profiles[profileID]
	m.mu.Unlock()
	return p, ok
}

func (m *Manager) ProfileByID(id string) (Profile, bool) {
	id = strings.TrimSpace(id)
	if m == nil || id == "" {
		return Profile{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Profile{}, false
	}
	p, ok := m.profiles[id]
	return p, ok
}

// ProfileSchemaVersionForConnection returns the current profile schema
// version for an authenticated connection. A zero version preserves
// compatibility with legacy profiles that predate schema versioning.
func (m *Manager) ProfileSchemaVersionForConnection(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0
	}
	profileID := m.profileIDs[id]
	if profileID == "" {
		return 0
	}
	return m.profiles[profileID].SchemaVersion
}

func (m *Manager) operationAllowed(profileID, action string) bool {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return false
	}
	p, ok := m.profiles[profileID]
	m.mu.Unlock()
	if !ok {
		return false
	}
	if len(p.AllowedOperations) > 0 {
		operationAllowed := false
		for _, configured := range p.AllowedOperations {
			if strings.EqualFold(strings.TrimSpace(configured), action) {
				operationAllowed = true
				break
			}
		}
		if !operationAllowed {
			return false
		}
	}
	if action == "execute" || action == "batch_execute" || action == "write_table" || action == "export_excel" {
		return p.WriteEnabled && !p.ReadOnly
	}
	return true
}

func (m *Manager) connectionHasActiveAsyncLocked(connectionID string) bool {
	for _, job := range m.jobs {
		if job == nil || job.ConnectionID != connectionID {
			continue
		}
		job.mu.Lock()
		active := job.Status == "queued" || job.Status == "running"
		job.mu.Unlock()
		if active {
			return true
		}
	}
	return false
}

func (m *Manager) Adapter(id string) (Adapter, bool) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, false
	}
	a, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	if last := m.lastUsed[id]; m.idleTTL > 0 && !last.IsZero() && time.Since(last) > m.idleTTL {
		if m.connectionHasActiveAsyncLocked(id) {
			m.lastUsed[id] = time.Now()
		} else {
			delete(m.items, id)
			delete(m.lastUsed, id)
			delete(m.bindings, id)
			delete(m.profileIDs, id)
			m.invalidateResultsForConnectionLocked(id)
			m.mu.Unlock()
			// Close outside the manager lock (DisconnectFor pattern): DB.Close
			// waits for in-flight queries and would otherwise stall every manager
			// operation behind it.
			_ = a.Close()
			return nil, false
		}
	}
	m.lastUsed[id] = time.Now()
	m.mu.Unlock()
	return a, ok
}
func (m *Manager) Disconnect(id string) error {
	return m.DisconnectFor(id, "", "")
}

func (m *Manager) DisconnectFor(id, ownerID, sessionID string) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	a, ok := m.items[id]
	binding := m.bindings[id]
	if ok && binding.ownerID != "" && (binding.ownerID != ownerID || binding.sessionID != sessionID) {
		m.mu.Unlock()
		return fmt.Errorf("permission: connection belongs to another session")
	}
	if ok {
		delete(m.items, id)
		delete(m.lastUsed, id)
		delete(m.bindings, id)
		delete(m.profileIDs, id)
		m.invalidateResultsForConnectionLocked(id)
		m.cancelAsyncJobsForConnectionLocked(id)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("connection_not_found")
	}
	return a.Close()
}
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	items := m.takeLiveAdaptersLocked()
	cancels := m.takeJobCancelsLocked()
	m.profileSlots = make(map[string]chan struct{})
	m.consumedApprovals = make(map[string]time.Time)
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	for _, a := range items {
		_ = a.Close()
	}
}

// IsClosed reports whether the manager has crossed its shutdown boundary.
// It is intentionally a read-only probe for host adapters and diagnostics;
// all mutating operations still enforce the check under the manager lock.
func (m *Manager) IsClosed() bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	return closed
}

func openProfile(ctx context.Context, p Profile, resolve SecretResolver) (Adapter, error) {
	return openProfileWithTunnel(ctx, p, resolve, nil)
}

func openProfileWithTunnel(ctx context.Context, p Profile, resolve SecretResolver, dial TunnelDialer) (Adapter, error) {
	secret := ""
	var err error
	if p.SecretRef != "" {
		if resolve == nil {
			return nil, fmt.Errorf("authentication: secret resolver is not configured")
		}
		secret, err = resolve(ctx, p.SecretRef)
		if err != nil {
			return nil, fmt.Errorf("authentication: %w", err)
		}
		if strings.TrimSpace(secret) == "" {
			return nil, fmt.Errorf("authentication: bound secret is empty")
		}
	}
	if err := requireTunnelDialer(p, dial); err != nil {
		return nil, err
	}
	switch p.Type {
	case SourceMySQL:
		addr, serverName, err := sqlDialAddr(p)
		if err != nil {
			return nil, fmt.Errorf("connection: %w", err)
		}
		cfg, err := mysqlClientConfig(p, secret, addr, serverName)
		if err != nil {
			return nil, err
		}
		dsn := cfg.FormatDSN()
		if profileUsesSSHTunnel(p) {
			return openTunneledSQLAdapter("mysql", dsn, p, secret, dial)
		}
		return newSQLAdapter("mysql", dsn, "mysql", p, secret)
	case SourcePostgres:
		addr, _, err := sqlDialAddr(p)
		if err != nil {
			return nil, fmt.Errorf("connection: %w", err)
		}
		u := &url.URL{Scheme: "postgres", User: url.UserPassword(p.Username, secret), Host: addr, Path: "/" + p.Database}
		applyPostgresTLS(u, p.TLS)
		// pgx/v5/stdlib registers the driver as "pgx"/"pgx/v5"; "postgres" is
		// not a registered driver name and would fail with "unknown driver".
		// The dialect string stays "postgres" (resetSession/Inspect switch on it).
		if profileUsesSSHTunnel(p) {
			return openTunneledSQLAdapter("postgres", u.String(), p, secret, dial)
		}
		return newSQLAdapter("postgres", u.String(), "pgx", p, secret)
	case SourceSQLServer:
		addr, _, err := sqlDialAddr(p)
		if err != nil {
			return nil, fmt.Errorf("connection: %w", err)
		}
		u := &url.URL{Scheme: "sqlserver", User: url.UserPassword(p.Username, secret), Host: addr, Path: "/" + p.Database}
		applySQLServerTLS(u, p.TLS)
		if profileUsesSSHTunnel(p) {
			return openTunneledSQLAdapter("sqlserver", u.String(), p, secret, dial)
		}
		return newSQLAdapter("sqlserver", u.String(), "sqlserver", p, secret)
	case SourceAccess:
		if !ODBCDriverLinked() {
			hint := DetectAccessODBC().Hint()
			if hint == "" {
				hint = "use a cgo-enabled Windows build with the Access Database Engine installed"
			}
			return nil, fmt.Errorf("driver_missing: Access ODBC adapter is not available in this build: %s", hint)
		}
		return newSQLAdapter("access", accessConnectionDSN(p, secret), "odbc", p, secret)
	case SourceExcel:
		return newExcelAdapterWithSecret(p, secret)
	default:
		return nil, fmt.Errorf("unsupported_source_type: %s", p.Type)
	}
}

type excelAdapter struct {
	profile Profile
	secret  string
}

func newExcelAdapter(p Profile) (Adapter, error) {
	return newExcelAdapterWithSecret(p, "")
}

func newExcelAdapterWithSecret(p Profile, secret string) (Adapter, error) {
	if p.FilePath == "" {
		return nil, fmt.Errorf("path_denied: file_path required")
	}
	if _, err := os.Stat(p.FilePath); err != nil {
		return nil, fmt.Errorf("connection: %w", err)
	}
	return &excelAdapter{profile: p, secret: secret}, nil
}
func (a *excelAdapter) Ping(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if _, err := excel.ResolveReadablePath(a.profile.FilePath, a.secret); err != nil {
		return err
	}
	return nil
}
func (a *excelAdapter) Close() error { return nil }
func (a *excelAdapter) Capabilities() Capabilities {
	// Query results are materialized by the manager and can therefore be
	// resumed with a short-lived, owner-bound cursor just like SQL adapters.
	return Capabilities{Read: true, Write: false, Transactions: false, Cursors: true, Explain: true, MaxPageSize: 5000}
}
func (a *excelAdapter) Inspect(ctx context.Context, req InspectRequest) (SchemaInfo, error) {
	r, err := excel.ReadFile(a.profile.FilePath, excel.ReadOptions{SheetName: req.Table, MaxRows: 1, Password: a.secret})
	if err != nil {
		return SchemaInfo{}, err
	}
	info := SchemaInfo{ContractVersion: ContractVersion, Dialect: "excel", Capabilities: a.Capabilities()}
	table := TableInfo{Schema: "", Name: r.SheetName}
	if len(r.Rows) > 0 {
		names := inferredColumnNames(r.Rows[0])
		for i, c := range r.Rows[0] {
			name := names[i]
			table.Columns = append(table.Columns, Column{Name: name, Type: string(c.Type)})
		}
	}
	info.Tables = []TableInfo{table}
	return info, nil
}
func (a *excelAdapter) Query(ctx context.Context, req QueryRequest) (QueryResult, error) {
	if strings.TrimSpace(req.Cursor) != "" {
		return QueryResult{}, fmt.Errorf("unsupported_capability: cursor must be resolved by Manager")
	}
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
	defer cancel()
	return queryExcel(ctx, a.profile, a.secret, req)
}
func (a *excelAdapter) Execute(ctx context.Context, req ExecuteRequest) (MutationResult, error) {
	return MutationResult{}, fmt.Errorf("unsupported_capability: Excel transactions/write are not available through SQL")
}

func queryExcel(ctx context.Context, p Profile, secret string, req QueryRequest) (QueryResult, error) {
	// Validate before paying the import cost: an illegal statement must not
	// trigger a full spreadsheet read and staging load.
	sqlText := req.SQL
	if req.Explain {
		wrapped, _, wrapErr := wrapExplainSQL("sqlite", req.SQL)
		if wrapErr != nil {
			return QueryResult{}, wrapErr
		}
		sqlText = wrapped
	} else if err := validateReadSQLDialect(req.SQL, "sqlite"); err != nil {
		return QueryResult{}, err
	}
	if err := p.validateSQLTables(req.SQL); err != nil {
		return QueryResult{}, err
	}
	q, args, err := bindNamed(sqlText, req.Params)
	if err != nil {
		return QueryResult{}, err
	}
	r, err := excel.ReadFile(p.FilePath, excel.ReadOptions{SheetName: p.DefaultSheet(), MaxRows: 100000, Password: secret})
	if err != nil {
		return QueryResult{}, err
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return QueryResult{}, err
	}
	// The in-memory database is per-connection: pin a single connection so a
	// pool reopen can never produce "no such table: sheet".
	db.SetMaxOpenConns(1)
	defer db.Close()
	if len(r.Rows) == 0 {
		result := QueryResult{ContractVersion: ContractVersion}
		if r.Truncated {
			result.Truncated = true
			result.Warnings = []string{"source_row_limit", "pagination_unavailable"}
		}
		return result, nil
	}
	cols := inferredColumnNames(r.Rows[0])
	types := inferExcelSQLiteTypes(r.Rows[1:], len(cols))
	defs := make([]string, len(cols))
	for i, c := range cols {
		defs[i] = fmt.Sprintf("\"%s\" %s", c, types[i])
	}
	if _, err = db.ExecContext(ctx, "CREATE TABLE sheet ("+strings.Join(defs, ",")+")"); err != nil {
		return QueryResult{}, err
	}
	ph := strings.TrimRight(strings.Repeat("?,", len(cols)), ",")
	ins := "INSERT INTO sheet VALUES (" + ph + ")"
	if err := loadExcelRows(ctx, db, ins, len(cols), r.Rows[1:]); err != nil {
		return QueryResult{}, err
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return QueryResult{}, err
	}
	defer rows.Close()
	names, _ := rows.Columns()
	out := QueryResult{ContractVersion: ContractVersion, Columns: make([]Column, len(names))}
	for i, n := range names {
		if p.columnDenied(n) {
			return QueryResult{}, fmt.Errorf("permission: column %q is denied", n)
		}
		out.Columns[i] = Column{Name: n}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	page, all, truncated, warnings, err := collectRows(rows, names, p, limit)
	if err != nil {
		return out, err
	}
	out.Rows, out.Truncated, out.Warnings, out.allRows = page, truncated, warnings, all
	if out.Truncated && !hasTopLevelKeywordPair(req.SQL, "order", "by") {
		out.Warnings = append(out.Warnings, "unstable_order")
	}
	if r.Truncated {
		out.Truncated = true
		out.Warnings = append(out.Warnings, "source_row_limit", "pagination_unavailable")
	}
	out.RowCount = len(out.Rows)
	return out, classify(rows.Err())
}

// loadExcelRows stages spreadsheet rows into the in-memory sheet table. The
// inserts run inside one transaction over a single prepared statement, which
// is 10-50x faster than per-row Exec (each Exec would otherwise re-parse the
// statement and auto-commit individually).
func loadExcelRows(ctx context.Context, db *sql.DB, insertSQL string, width int, rows [][]excel.CellValue) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, row := range rows {
		vals := make([]interface{}, width)
		for i := range vals {
			if i < len(row) {
				vals[i] = row[i].Value
			}
		}
		if _, err := stmt.ExecContext(ctx, vals...); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return err
		}
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
func (p Profile) DefaultSheet() string {
	if p.Sheet != "" {
		return p.Sheet
	}
	return ""
}
func safeIdentifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "col"
	}
	return b.String()
}

func inferredColumnNames(header []excel.CellValue) []string {
	names := make([]string, len(header))
	seen := make(map[string]int, len(header))
	for i, cell := range header {
		base := fmt.Sprintf("col_%03d", i+1)
		if value, ok := cell.Value.(string); ok && strings.TrimSpace(value) != "" {
			base = safeIdentifier(strings.TrimSpace(value))
		}
		seen[base]++
		if seen[base] > 1 {
			names[i] = fmt.Sprintf("%s_%03d", base, seen[base])
		} else {
			names[i] = base
		}
	}
	return names
}

func inferExcelSQLiteTypes(rows [][]excel.CellValue, width int) []string {
	types := make([]string, width)
	for i := range types {
		types[i] = "TEXT"
	}
	for _, row := range rows {
		for i := 0; i < len(row) && i < width; i++ {
			switch row[i].Type {
			case excel.CellTypeNumber:
				if types[i] == "TEXT" {
					types[i] = "NUMERIC"
				}
			case excel.CellTypeBool:
				if types[i] == "TEXT" {
					types[i] = "INTEGER"
				}
			}
		}
	}
	return types
}

type sqlAdapter struct {
	db      *sql.DB
	dialect string
	profile Profile
	// secret is the resolved profile password embedded in the DSN. Driver
	// errors can echo connection parameters, so adapter-originated errors are
	// scrubbed of this literal before they leave the package.
	secret string
}

func newSQLAdapter(dialect, dsn, driver string, p Profile, secret string) (*sqlAdapter, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, classify(err)
	}
	return &sqlAdapter{db: db, dialect: dialect, profile: p, secret: secret}, nil
}

// sanitize classifies a driver error and strips the profile secret from its
// message. An empty secret skips the scan; the redaction is a literal
// replacement, deliberately not a general-purpose sanitizer.
func (a *sqlAdapter) sanitize(err error) error {
	err = classify(err)
	if err == nil || a.secret == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, a.secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(msg, a.secret, "[redacted]"))
}
func (a *sqlAdapter) Ping(ctx context.Context) error {
	if a.db == nil {
		return fmt.Errorf("database unavailable")
	}
	return a.sanitize(a.db.PingContext(ctx))
}

func (a *sqlAdapter) resetSession(ctx context.Context) error {
	// Do not expose these statements to the model. They run only on checkout
	// and prevent search_path/sql_mode/session options leaking across owners.
	var reset string
	switch a.dialect {
	case "postgres":
		reset = "RESET ALL"
	case "mysql":
		reset = "SET SESSION sql_mode=DEFAULT"
	case "sqlserver":
		// SQL Server's driver resets pooled sessions on checkout; no user SQL
		// is needed here.
		return nil
	default:
		return nil
	}
	if _, err := a.db.ExecContext(ctx, reset); err != nil {
		return a.sanitize(err)
	}
	return nil
}
func (a *sqlAdapter) Close() error {
	if a.db == nil {
		return nil
	}
	return a.db.Close()
}
func (a *sqlAdapter) Capabilities() Capabilities {
	write := a.profile.WriteEnabled && !a.profile.ReadOnly
	if a.dialect == "access" {
		return Capabilities{Read: true, Write: write, Transactions: false, Cursors: true, Explain: false, MaxPageSize: 5000}
	}
	return Capabilities{Read: true, Write: write, Transactions: true, Cursors: true, Explain: true, MaxPageSize: 5000}
}
func (a *sqlAdapter) Inspect(ctx context.Context, req InspectRequest) (SchemaInfo, error) {
	if err := a.resetSession(ctx); err != nil {
		return SchemaInfo{}, err
	}
	if a.dialect == "access" {
		return a.inspectAccess(ctx, req)
	}
	q := "SELECT table_schema, table_name, column_name, data_type FROM information_schema.columns"
	args := []interface{}{}
	if req.Table != "" {
		if !safeTableName(req.Table) {
			return SchemaInfo{}, fmt.Errorf("syntax: invalid table filter")
		}
		if !a.profile.tableAllowed(req.Table) {
			return SchemaInfo{}, fmt.Errorf("permission: table is not allowed")
		}
		q += " WHERE table_name = " + dialectPlaceholder(a.dialect, 1)
		args = append(args, req.Table)
	}
	q += " ORDER BY table_schema, table_name, ordinal_position"
	rows, err := a.db.QueryContext(ctx, q, args...)
	if err != nil {
		return SchemaInfo{}, a.sanitize(err)
	}
	defer rows.Close()
	info := SchemaInfo{ContractVersion: ContractVersion, Dialect: a.dialect, Capabilities: a.Capabilities()}
	index := map[string]int{}
	for rows.Next() {
		var s, n, c, t string
		if err := rows.Scan(&s, &n, &c, &t); err != nil {
			return info, a.sanitize(err)
		}
		if !a.profile.schemaAllowed(s) || !a.profile.tableAllowed(n) {
			continue
		}
		key := s + "." + n
		i, ok := index[key]
		if !ok {
			i = len(info.Tables)
			index[key] = i
			info.Tables = append(info.Tables, TableInfo{Schema: s, Name: n})
		}
		info.Tables[i].Columns = append(info.Tables[i].Columns, Column{Name: c, Type: t})
	}
	return info, a.sanitize(rows.Err())
}

func (a *sqlAdapter) inspectAccess(ctx context.Context, req InspectRequest) (SchemaInfo, error) {
	info := SchemaInfo{ContractVersion: ContractVersion, Dialect: a.dialect, Capabilities: a.Capabilities()}
	rows, err := a.db.QueryContext(ctx, "SELECT Name FROM MSysObjects WHERE Type=1 AND Flags=0")
	if err != nil {
		return info, fmt.Errorf("unsupported_capability: Access table metadata unavailable: %w", a.sanitize(err))
	}
	defer rows.Close()
	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return info, a.sanitize(err)
		}
		if req.Table != "" && !strings.EqualFold(req.Table, name) {
			continue
		}
		if !a.profile.tableAllowed(name) {
			continue
		}
		tableNames = append(tableNames, name)
	}
	if err := rows.Err(); err != nil {
		return info, a.sanitize(err)
	}
	_ = rows.Close()
	for _, name := range tableNames {
		table := TableInfo{Name: name}
		columns, err := a.db.QueryContext(ctx, "SELECT * FROM "+quoteIdentifier(name)+" WHERE 1=0")
		if err == nil {
			columnNames, _ := columns.Columns()
			for _, column := range columnNames {
				table.Columns = append(table.Columns, Column{Name: column})
			}
			_ = columns.Close()
		}
		info.Tables = append(info.Tables, table)
	}
	return info, nil
}
func (a *sqlAdapter) Query(ctx context.Context, req QueryRequest) (QueryResult, error) {
	if strings.TrimSpace(req.Cursor) != "" {
		return QueryResult{}, fmt.Errorf("unsupported_capability: cursor must be resolved by Manager")
	}
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
	defer cancel()
	if err := a.resetSession(ctx); err != nil {
		return QueryResult{}, err
	}
	start := time.Now()
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > a.Capabilities().MaxPageSize {
		limit = a.Capabilities().MaxPageSize
	}
	sqlText := req.SQL
	useShowplan := false
	if req.Explain {
		wrapped, showplan, wrapErr := wrapExplainSQL(a.dialect, req.SQL)
		if wrapErr != nil {
			return QueryResult{}, wrapErr
		}
		sqlText = wrapped
		useShowplan = showplan
	} else if err := validateReadSQLDialect(req.SQL, a.dialect); err != nil {
		return QueryResult{}, err
	}
	if err := a.profile.validateSQLTables(req.SQL); err != nil {
		return QueryResult{}, err
	}
	boundSQL, args, err := bindSQL(a.dialect, sqlText, req.Params, req.PositionalParams, req.ParameterMode)
	if err != nil {
		return QueryResult{}, err
	}
	if useShowplan {
		return a.querySQLServerShowplan(ctx, boundSQL, args, start, limit)
	}
	rows, err := a.db.QueryContext(ctx, boundSQL, args...)
	if err != nil {
		return QueryResult{}, a.sanitize(err)
	}
	defer rows.Close()
	names, _ := rows.Columns()
	out := QueryResult{ContractVersion: ContractVersion, ElapsedMS: 0}
	for _, n := range names {
		if a.profile.columnDenied(n) {
			return QueryResult{}, fmt.Errorf("permission: column %q is denied", n)
		}
		out.Columns = append(out.Columns, Column{Name: n})
	}
	page, all, truncated, warnings, err := collectRows(rows, names, a.profile, limit)
	if err != nil {
		return out, a.sanitize(err)
	}
	out.Rows, out.Truncated, out.Warnings, out.allRows = page, truncated, warnings, all
	if req.Explain {
		out.Warnings = append(out.Warnings, "explain_preview")
	}
	if out.Truncated && !hasTopLevelKeywordPair(req.SQL, "order", "by") {
		out.Warnings = append(out.Warnings, "unstable_order")
	}
	out.RowCount = len(out.Rows)
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, a.sanitize(rows.Err())
}

func (a *sqlAdapter) querySQLServerShowplan(ctx context.Context, sqlText string, args []interface{}, start time.Time, limit int) (QueryResult, error) {
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return QueryResult{}, a.sanitize(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET SHOWPLAN_TEXT ON"); err != nil {
		return QueryResult{}, fmt.Errorf("unsupported_capability: SQL Server SHOWPLAN is not available")
	}
	defer func() { _, _ = conn.ExecContext(ctx, "SET SHOWPLAN_TEXT OFF") }()
	rows, err := conn.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return QueryResult{}, a.sanitize(err)
	}
	defer rows.Close()
	names, _ := rows.Columns()
	out := QueryResult{ContractVersion: ContractVersion, Warnings: []string{"explain_preview"}}
	for _, n := range names {
		out.Columns = append(out.Columns, Column{Name: n})
	}
	page, all, truncated, warnings, err := collectRows(rows, names, a.profile, limit)
	if err != nil {
		return out, a.sanitize(err)
	}
	out.Rows, out.Truncated, out.Warnings, out.allRows = page, truncated, append(out.Warnings, warnings...), all
	out.RowCount = len(out.Rows)
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, a.sanitize(rows.Err())
}

func normalizeDBValue(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case []byte:
		if utf8.Valid(v) {
			return string(v)
		}
		sum := sha256.Sum256(v)
		return map[string]interface{}{"binary_length": len(v), "sha256": hex.EncodeToString(sum[:])}
	default:
		return value
	}
}

func collectRows(rows *sql.Rows, names []string, profile Profile, limit int) (page, all [][]interface{}, truncated bool, warnings []string, err error) {
	if limit <= 0 {
		limit = 100
	}
	bytesUsed := 0
	byteLimited := false
	for rows.Next() {
		values := make([]interface{}, len(names))
		dest := make([]interface{}, len(names))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, all, false, warnings, err
		}
		for i, value := range values {
			values[i] = normalizeDBValue(value)
			if profile.columnMasked(names[i]) {
				values[i] = maskValue(values[i])
			}
		}
		encoded, _ := json.Marshal(values)
		if bytesUsed+len(encoded) > maxResultBytes {
			if len(all) == 0 {
				// Fail closed like boundTableRows: a first row that alone blows
				// the byte budget is an error, not a silently empty page.
				return nil, all, false, warnings, fmt.Errorf("result_too_large: first row exceeds result byte limit")
			}
			truncated = true
			byteLimited = true
			warnings = append(warnings, "result_byte_limit")
			break
		}
		bytesUsed += len(encoded)
		all = append(all, values)
		if len(all) >= 100000 {
			truncated = true
			warnings = append(warnings, "scan_row_limit")
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, all, truncated, warnings, err
	}
	if len(all) > limit {
		truncated = true
	}
	// A byte-boundary stop closes the driver rows before the omitted suffix is
	// available. Do not issue a cursor that would imply lossless continuation.
	if byteLimited {
		warnings = append(warnings, "pagination_unavailable")
	}
	page = all
	if len(page) > limit {
		page = page[:limit]
	}
	return page, all, truncated, warnings, nil
}
func (a *sqlAdapter) Execute(ctx context.Context, req ExecuteRequest) (MutationResult, error) {
	return a.ExecuteBatch(ctx, BatchExecuteRequest{
		Statements:    []BatchStatement{{SQL: req.SQL, Params: req.Params, PositionalParams: req.PositionalParams, ParameterMode: req.ParameterMode, MaxAffectedRows: req.MaxAffectedRows}},
		DryRun:        req.DryRun,
		ApprovalToken: req.ApprovalToken,
		MaxStatements: 1,
	})
}

func (a *sqlAdapter) ExecuteBatch(ctx context.Context, req BatchExecuteRequest) (MutationResult, error) {
	if a == nil || a.db == nil {
		return MutationResult{}, fmt.Errorf("database unavailable")
	}
	if !a.profile.WriteEnabled || a.profile.ReadOnly {
		return MutationResult{}, fmt.Errorf("permission: profile is read-only; enable write_enabled explicitly")
	}
	maxStatements := req.MaxStatements
	if maxStatements <= 0 {
		maxStatements = 100
	}
	if len(req.Statements) == 0 {
		return MutationResult{}, fmt.Errorf("syntax: statements must not be empty")
	}
	if len(req.Statements) > maxStatements {
		return MutationResult{}, fmt.Errorf("quota_exceeded: batch contains %d statements (limit %d)", len(req.Statements), maxStatements)
	}
	if len(req.Statements) > 1 {
		// A multi-statement batch promises an atomic commit/rollback. DDL can
		// implicitly commit (e.g. MySQL) and silently break that contract, so
		// it is rejected here regardless of allow_ddl. Single-statement execute
		// keeps the allow_ddl gate below.
		for _, statement := range req.Statements {
			if guard, err := GuardSQLDialect(statement.SQL, a.dialect); err == nil && guard.Class == StatementDDL {
				return MutationResult{}, fmt.Errorf("permission: DDL is not allowed in a multi-statement batch")
			}
		}
	}
	if !req.DryRun && strings.TrimSpace(req.ApprovalToken) == "" {
		return MutationResult{}, fmt.Errorf("permission: approval token required")
	}
	if !req.DryRun && a.dialect == "access" {
		if err := prepareAccessWrite(a.profile.FilePath); err != nil {
			return MutationResult{}, err
		}
	}
	// Mutations inherit the caller's context; hosts should set a deadline.
	// Guard against an unbounded context in direct/TUI calls.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	if err := a.resetSession(ctx); err != nil {
		return MutationResult{}, err
	}
	if req.DryRun {
		hasDDL := false
		for _, statement := range req.Statements {
			if err := validateMutationSQLDialect(statement.SQL, a.dialect); err != nil {
				return MutationResult{}, err
			}
			guard, _ := GuardSQLDialect(statement.SQL, a.dialect)
			if guard.Class == StatementDDL {
				if !a.profile.AllowDDL {
					return MutationResult{}, fmt.Errorf("permission: DDL is disabled for this profile")
				}
				hasDDL = true
			}
			if _, _, err := bindForDialect(a.dialect, statement.SQL, statement.Params); err != nil {
				return MutationResult{}, err
			}
			if err := a.profile.validateSQLTables(statement.SQL); err != nil {
				return MutationResult{}, err
			}
		}
		if hasDDL {
			// DDL may implicitly commit (e.g. MySQL), so a rollback preview would
			// be a false promise. Return a planner preview instead of executing.
			plan := a.planDDL(ctx, req.Statements[0].SQL)
			return MutationResult{ContractVersion: ContractVersion, DryRun: true, DryRunGuarantee: "policy_only", Plan: plan, Warnings: plan.Warnings}, nil
		}
		if !a.Capabilities().Transactions {
			return MutationResult{ContractVersion: ContractVersion, DryRun: true, DryRunGuarantee: "policy_only"}, nil
		}
		// DML-only batches on transaction-capable adapters get a real preview:
		// execute inside a transaction and roll back. The guarantee string tells
		// callers this is not an absolute no-side-effect promise (triggers and
		// sequences may still advance).
		tx, err := a.beginMutationTx(ctx)
		if err != nil {
			return MutationResult{}, a.sanitize(err)
		}
		affected, execErr := a.execStatementsInTx(ctx, tx, req.Statements)
		_ = tx.Rollback()
		if execErr != nil {
			return MutationResult{}, execErr
		}
		return MutationResult{ContractVersion: ContractVersion, MatchedRows: affected, AffectedRows: affected, DryRun: true, DryRunGuarantee: "rolled_back_transaction"}, nil
	}
	if !a.Capabilities().Transactions {
		if len(req.Statements) > 1 {
			return MutationResult{}, fmt.Errorf("unsupported_capability: data source does not support transactions")
		}
		affected, err := a.execStatementsDirect(ctx, req.Statements)
		if err != nil {
			return MutationResult{}, err
		}
		return MutationResult{ContractVersion: ContractVersion, MatchedRows: affected, AffectedRows: affected, CommitID: randomID()}, nil
	}
	tx, err := a.beginMutationTx(ctx)
	if err != nil {
		return MutationResult{}, a.sanitize(err)
	}
	affected, err := a.execStatementsInTx(ctx, tx, req.Statements)
	if err != nil {
		_ = tx.Rollback()
		return MutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return MutationResult{}, a.sanitize(err)
	}
	return MutationResult{ContractVersion: ContractVersion, MatchedRows: affected, AffectedRows: affected, CommitID: randomID()}, nil
}

// execStatementsInTx validates and runs the mutation batch inside tx and
// returns the total affected rows. The caller owns commit/rollback.
func (a *sqlAdapter) execStatementsInTx(ctx context.Context, tx *sql.Tx, statements []BatchStatement) (int64, error) {
	var affected int64
	for _, statement := range statements {
		if err := validateMutationSQLDialect(statement.SQL, a.dialect); err != nil {
			return affected, err
		}
		if guard, _ := GuardSQLDialect(statement.SQL, a.dialect); guard.Class == StatementDDL && !a.profile.AllowDDL {
			return affected, fmt.Errorf("permission: DDL is disabled for this profile")
		}
		if err := a.profile.validateSQLTables(statement.SQL); err != nil {
			return affected, err
		}
		sqlText, args, err := bindSQL(a.dialect, statement.SQL, statement.Params, statement.PositionalParams, statement.ParameterMode)
		if err != nil {
			return affected, err
		}
		result, err := tx.ExecContext(ctx, sqlText, args...)
		if err != nil {
			return affected, a.sanitize(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return affected, err
		}
		limit := statement.MaxAffectedRows
		if limit <= 0 {
			limit = a.profile.MaxAffectedRows
		}
		if limit > 0 && n > limit {
			return affected, fmt.Errorf("quota_exceeded: affected rows %d", n)
		}
		affected += n
		if a.profile.MaxAffectedRows > 0 && affected > a.profile.MaxAffectedRows {
			return affected, fmt.Errorf("quota_exceeded: batch affected rows %d", affected)
		}
	}
	return affected, nil
}

func (a *sqlAdapter) beginMutationTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err == nil {
		return tx, nil
	}
	return a.db.BeginTx(ctx, nil)
}

func (a *sqlAdapter) execStatementsDirect(ctx context.Context, statements []BatchStatement) (int64, error) {
	var affected int64
	for _, statement := range statements {
		if err := validateMutationSQLDialect(statement.SQL, a.dialect); err != nil {
			return affected, err
		}
		if guard, _ := GuardSQLDialect(statement.SQL, a.dialect); guard.Class == StatementDDL && !a.profile.AllowDDL {
			return affected, fmt.Errorf("permission: DDL is disabled for this profile")
		}
		if err := a.profile.validateSQLTables(statement.SQL); err != nil {
			return affected, err
		}
		sqlText, args, err := bindSQL(a.dialect, statement.SQL, statement.Params, statement.PositionalParams, statement.ParameterMode)
		if err != nil {
			return affected, err
		}
		result, err := a.db.ExecContext(ctx, sqlText, args...)
		if err != nil {
			return affected, a.sanitize(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return affected, err
		}
		limit := statement.MaxAffectedRows
		if limit <= 0 {
			limit = a.profile.MaxAffectedRows
		}
		if limit > 0 && n > limit {
			return affected, fmt.Errorf("quota_exceeded: affected rows %d", n)
		}
		affected += n
	}
	return affected, nil
}

func validateMutationSQL(sqlText string) error {
	return validateMutationSQLDialect(sqlText, "")
}

func validateMutationSQLDialect(sqlText, dialect string) error {
	if hasSQLComment(sqlText) {
		return fmt.Errorf("syntax: comments are not allowed")
	}
	guard, err := GuardSQLDialect(sqlText, dialect)
	if err != nil {
		return err
	}
	if guard.Class == StatementRead {
		return fmt.Errorf("syntax: read-only statement must use query")
	}
	if guard.Class == StatementSession || guard.Class == StatementExternal {
		return fmt.Errorf("permission: external or session statement is blocked")
	}
	if guard.Class == StatementDML {
		// The WHERE scan is dialect-aware, literal-blind and top-level only: a
		// string such as ' where ', a dollar-quoted body, or a WHERE inside a
		// subquery cannot masquerade as the statement's own clause.
		stripped, err := stripSQLLiteralsDialect(guard.SQL, scanDialect(dialect))
		if err != nil {
			return err
		}
		fields := strings.Fields(strings.ToLower(stripped))
		if len(fields) > 0 && (fields[0] == "update" || fields[0] == "delete") {
			if !HasTopLevelKeyword(guard.SQL, dialect, "where") {
				return fmt.Errorf("permission: UPDATE/DELETE requires a WHERE clause")
			}
		}
	}
	return nil
}

func validateReadSQL(sqlText string) error {
	return validateReadSQLDialect(sqlText, "")
}

func validateReadSQLDialect(sqlText, dialect string) error {
	if hasSQLComment(sqlText) {
		return fmt.Errorf("syntax: comments are not allowed")
	}
	guard, err := GuardSQLDialect(sqlText, dialect)
	if err != nil {
		return err
	}
	if guard.Class != StatementRead {
		return fmt.Errorf("permission: query only allows read-only SQL; use action=execute for INSERT/UPDATE/DELETE (dry_run=false will request host approval)")
	}
	return nil
}

func bindForDialect(dialect, sqlText string, params map[string]interface{}) (string, []interface{}, error) {
	return bindNamedDialect(sqlText, params, func(index int) string {
		switch strings.ToLower(dialect) {
		case "postgres":
			return fmt.Sprintf("$%d", index)
		case "sqlserver":
			return fmt.Sprintf("@p%d", index)
		default:
			return "?"
		}
	})
}

func dialectPlaceholder(dialect string, index int) string {
	switch strings.ToLower(dialect) {
	case "postgres":
		return fmt.Sprintf("$%d", index)
	case "sqlserver":
		return fmt.Sprintf("@p%d", index)
	default:
		return "?"
	}
}

func safeTableName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == ' ' {
			continue
		}
		return false
	}
	return true
}

func (p Profile) tableAllowed(name string) bool {
	if len(p.AllowedTables) == 0 {
		return true
	}
	name = strings.ToLower(name)
	for _, allowed := range p.AllowedTables {
		if strings.ToLower(strings.TrimSpace(allowed)) == name {
			return true
		}
	}
	return false
}

func (p Profile) schemaAllowed(name string) bool {
	if len(p.AllowedSchemas) == 0 {
		return true
	}
	name = strings.ToLower(name)
	for _, allowed := range p.AllowedSchemas {
		if strings.ToLower(strings.TrimSpace(allowed)) == name {
			return true
		}
	}
	return false
}

func (p Profile) validateSQLTables(sqlText string) error {
	if len(p.AllowedTables) == 0 && len(p.AllowedSchemas) == 0 {
		return nil
	}
	guard, err := GuardSQL(sqlText)
	if err != nil {
		return err
	}
	fields := splitSQLTokens(guard.SQL)
	cteNames := extractCTENames(fields)
	if err := p.validateCommaSeparatedTables(fields, cteNames); err != nil {
		return err
	}
	for i := 0; i+1 < len(fields); i++ {
		keyword := strings.ToLower(strings.Trim(fields[i], "(),"))
		if keyword != "from" && keyword != "join" && keyword != "update" && keyword != "delete" && keyword != "into" && keyword != "merge" {
			continue
		}
		name := normalizeTableReference(fields[i+1])
		if _, isCTE := cteNames[strings.ToLower(name)]; isCTE {
			continue
		}
		if !safeTableName(name) || !p.resourceAllowed(name) {
			return fmt.Errorf("permission: table %q is not allowed", name)
		}
	}
	return nil
}

func splitSQLTokens(sqlText string) []string {
	var tokens []string
	start := -1
	var quote, quoteEnd byte
	flush := func(end int) {
		if start >= 0 && end > start {
			tokens = append(tokens, sqlText[start:end])
		}
		start = -1
	}
	for i := 0; i < len(sqlText); i++ {
		c := sqlText[i]
		if quote != 0 {
			if c == quoteEnd {
				if i+1 < len(sqlText) && sqlText[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			if start < 0 {
				start = i
			}
			quote, quoteEnd = c, c
			if c == '[' {
				quoteEnd = ']'
			}
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(sqlText))
	return tokens
}

func (p Profile) validateCommaSeparatedTables(fields []string, cteNames map[string]struct{}) error {
	inRelations, awaiting := false, false
	for _, raw := range fields {
		word := strings.ToLower(strings.Trim(raw, "(),;\t\r\n"))
		switch word {
		case "from", "join", "update", "delete", "into", "merge":
			inRelations, awaiting = true, true
			continue
		case "where", "on", "set", "values", "returning", "group", "order", "having", "limit", "offset", "union", "except", "intersect":
			inRelations, awaiting = false, false
			continue
		}
		if !inRelations {
			continue
		}
		comma := strings.HasSuffix(raw, ",")
		token := strings.TrimSuffix(raw, ",")
		if token == "" {
			if comma {
				awaiting = true
			}
			continue
		}
		if !awaiting {
			if comma {
				awaiting = true
			}
			continue
		}
		name := normalizeTableReference(token)
		if name == "" || strings.Contains(token, "(") {
			awaiting = false
			continue
		}
		if _, isCTE := cteNames[strings.ToLower(name)]; !isCTE && (!safeTableName(name) || !p.resourceAllowed(name)) {
			return fmt.Errorf("permission: table %q is not allowed", name)
		}
		awaiting = comma
	}
	return nil
}

func extractCTENames(fields []string) map[string]struct{} {
	out := make(map[string]struct{})
	for i := 0; i+1 < len(fields); i++ {
		if !strings.EqualFold(strings.Trim(fields[i+1], "(),"), "as") {
			continue
		}
		name := normalizeTableReference(fields[i])
		if safeTableName(name) {
			out[strings.ToLower(name)] = struct{}{}
		}
	}
	return out
}

// normalizeTableReference accepts the identifier quoting forms used by the
// supported dialects while rejecting expressions and aliases. It intentionally
// does not attempt to parse arbitrary SQL; an unrecognized reference fails
// closed in validateSQLTables.
func normalizeTableReference(raw string) string {
	raw = strings.Trim(raw, "(),;\t\r\n")
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(raw); {
		switch raw[i] {
		case '[', '`', '"':
			end := raw[i]
			if end == '[' {
				end = ']'
			}
			i++
			start := i
			for i < len(raw) && raw[i] != end {
				i++
			}
			if i >= len(raw) {
				return ""
			}
			part := raw[start:i]
			if strings.ContainsAny(part, "[]`\"") {
				return ""
			}
			b.WriteString(part)
			i++
		case '.':
			b.WriteByte('.')
			i++
		default:
			if (raw[i] >= 'a' && raw[i] <= 'z') || (raw[i] >= 'A' && raw[i] <= 'Z') || (raw[i] >= '0' && raw[i] <= '9') || raw[i] == '_' {
				b.WriteByte(raw[i])
				i++
				continue
			}
			return ""
		}
	}
	return b.String()
}

func (p Profile) resourceAllowed(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) > 1 {
		if !p.schemaAllowed(parts[len(parts)-2]) {
			return false
		}
	} else if len(p.AllowedSchemas) > 0 {
		// An unqualified reference is resolved by the server's default schema;
		// require that schema to be explicit and allowlisted rather than letting
		// search_path/session state bypass the resource boundary.
		if strings.TrimSpace(p.DefaultSchema) == "" || !p.schemaAllowed(p.DefaultSchema) {
			return false
		}
	}
	return p.tableAllowed(parts[len(parts)-1])
}

func (p Profile) columnDenied(name string) bool {
	for _, denied := range p.DeniedColumns {
		if strings.EqualFold(strings.TrimSpace(denied), name) {
			return true
		}
	}
	return false
}

func (p Profile) columnMasked(name string) bool {
	for _, masked := range p.MaskedColumns {
		if strings.EqualFold(strings.TrimSpace(masked), name) {
			return true
		}
	}
	return false
}

func maskValue(value interface{}) interface{} {
	s, ok := value.(string)
	if !ok || s == "" {
		return "***"
	}
	runes := []rune(s)
	if len(runes) <= 4 {
		return "***"
	}
	return "***" + string(runes[len(runes)-4:])
}

func randomID() string { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func mysqlClientConfig(p Profile, secret, addr, serverName string) (*mysql.Config, error) {
	// NewConfig() sets AllowNativePasswords=true. A zero-value mysql.Config
	// serializes allowNativePasswords=false, and the driver then rejects
	// mysql_native_password users with ErrNativePassword even when the
	// password is correct.
	cfg := mysql.NewConfig()
	cfg.User = p.Username
	cfg.Passwd = secret
	cfg.Net = "tcp"
	cfg.Addr = addr
	cfg.DBName = p.Database
	cfg.ParseTime = true
	cfg.AllowNativePasswords = true
	if err := applyMySQLTLS(cfg, p.TLS, serverName, p.ID); err != nil {
		return nil, err
	}
	return cfg, nil
}

func classifiedPrefix(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, ":"); i > 0 {
		switch class := msg[:i]; class {
		case "timeout", "cancelled", "syntax", "driver_missing", "authentication", "permission", "constraint", "connection":
			return class
		}
	}
	return ""
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if classifiedPrefix(err) != "" {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timeout: %w", err)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("cancelled: %w", err)
	}
	if errors.Is(err, mysql.ErrNativePassword) {
		return fmt.Errorf("authentication: %w", err)
	}
	// Keep driver messages for diagnostics, but prepend a stable class when
	// the common authentication/permission/constraint cases are recognizable.
	msg := strings.ToLower(err.Error())
	for _, item := range []struct {
		class   string
		needles []string
	}{
		{"syntax", []string{"syntax error", "incorrect syntax", "parse error", "unrecognized token"}},
		{"driver_missing", []string{"unknown driver", "driver not found"}},
		{"authentication", []string{"access denied", "authentication failed", "login failed", "password", "authentication protocol", "auth plugin", "caching_sha2"}},
		{"permission", []string{"permission denied", "not authorized", "insufficient privilege"}},
		{"constraint", []string{"duplicate", "constraint", "foreign key"}},
		{"connection", []string{"connection refused", "no such host", "broken pipe", "driver: bad connection", "actively refused", "i/o timeout", "forcibly closed", "no connection could be made"}},
	} {
		for _, needle := range item.needles {
			if strings.Contains(msg, needle) {
				return fmt.Errorf("%s: %w", item.class, err)
			}
		}
	}
	return err
}

func connectErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if class := classifiedPrefix(classify(err)); class != "" {
		return class
	}
	return "connection"
}
