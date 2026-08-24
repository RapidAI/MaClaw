package tool

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ContinuityScope is the task-fact namespace. It deliberately mirrors the
// durable invocation identity instead of inventing a second task lineage.
// TenantID is supplied only by a trusted host when consuming projections; it
// is not part of InvocationScope because invocation authorization is already
// partitioned by the coordinator database owner.
type ContinuityScope struct {
	TenantID       string
	PrincipalID    string
	ConversationID string
	RootTaskID     string
}

// ContinuityEvidence is a small, durable statement about one completed need.
// It contains no provider name, invocation grant, adapter contract, or model
// writable parameter. Artifact and receipt access remain RouteState-owned.
type ContinuityEvidence struct {
	NeedID      string
	Capability  CapabilityID
	Provenance  string
	CompletedAt time.Time
}

// ContinuityState is a rebuildable task-fact projection. RouteState remains
// the execution authorization authority: callers must never use this value to
// mint a grant, recover an adapter, or alter a published route revision.
type ContinuityState struct {
	Scope                  ContinuityScope
	OpenNeeds              []CapabilityNeed
	CompletedEvidence      []ContinuityEvidence
	RouteRevision          RouteRevisionRef
	FencingToken           uint64
	Version                uint64
	LastProjectionSequence uint64
	UpdatedAt              time.Time
}

// ContinuityProjectionEvent is an immutable coordinator outbox item. Its
// payload is a snapshot of task facts only; it explicitly excludes grants,
// bindings, host-call identities, provider names, and invocation parameters.
type ContinuityProjectionEvent struct {
	Sequence     uint64
	EventKey     string
	EventKind    string
	TenantID     string
	Scope        InvocationScope
	Revision     RouteRevisionRef
	FencingToken uint64
	Needs        []continuityNeedFact
	CompletedIDs []string
	CreatedAt    time.Time
}

type continuityNeedFact struct {
	SelectionID string
	Need        CapabilityNeed
}

type continuityProjectionPayload struct {
	TenantID     string               `json:"tenant_id"`
	Scope        InvocationScope      `json:"scope"`
	Revision     RouteRevisionRef     `json:"revision"`
	FencingToken uint64               `json:"fencing_token"`
	Needs        []continuityNeedFact `json:"needs"`
	CompletedIDs []string             `json:"completed_ids"`
}

const (
	continuityProjectionRoutePublished  = "route_published"
	continuityProjectionExecutionUpdate = "execution_updated"
)

func validateContinuityScope(scope ContinuityScope) error {
	if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.PrincipalID) == "" || strings.TrimSpace(scope.ConversationID) == "" || strings.TrimSpace(scope.RootTaskID) == "" {
		return fmt.Errorf("continuity_scope_required")
	}
	return nil
}

func continuityScopeForInvocation(tenantID string, scope InvocationScope) (ContinuityScope, error) {
	if err := ValidateArtifactScope(scope); err != nil {
		return ContinuityScope{}, err
	}
	result := ContinuityScope{TenantID: strings.TrimSpace(tenantID), PrincipalID: scope.PrincipalID, ConversationID: scope.SessionID, RootTaskID: scope.RootTaskID}
	if err := validateContinuityScope(result); err != nil {
		return ContinuityScope{}, err
	}
	return result, nil
}

func continuityStateKey(scope ContinuityScope) string {
	return SchemaDigest([]byte(strings.Join([]string{scope.TenantID, scope.PrincipalID, scope.ConversationID, scope.RootTaskID}, "\x00")))
}

func cloneContinuityNeed(value CapabilityNeed) CapabilityNeed {
	result := value
	result.Qualifiers = make(map[string]string, len(value.Qualifiers))
	for key, entry := range value.Qualifiers {
		result.Qualifiers[key] = entry
	}
	result.EvidenceIDs = append([]string(nil), value.EvidenceIDs...)
	return result
}

func continuityNeedFacts(plan ToolPlan) []continuityNeedFact {
	values := make([]continuityNeedFact, 0, len(plan.Selections))
	for _, selection := range plan.Selections {
		need := CapabilityNeed{ID: strings.TrimSpace(selection.NeedID), Capability: selection.FitProof.MatchedCapability, Qualifiers: map[string]string{}, Polarity: NeedRequire, Required: true}
		if need.ID == "" {
			need.ID = strings.TrimSpace(selection.ID)
		}
		for key, value := range selection.FitProof.QualifierBindings {
			need.Qualifiers[key] = value
		}
		values = append(values, continuityNeedFact{SelectionID: selection.ID, Need: need})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].SelectionID < values[j].SelectionID })
	return values
}

func continuityProjectionKey(tenantID, routeKey string, fencingToken uint64, eventKind string, completedIDs []string) string {
	ids := append([]string(nil), completedIDs...)
	sort.Strings(ids)
	return SchemaDigest([]byte(strings.Join([]string{strings.TrimSpace(tenantID), routeKey, fmt.Sprintf("%d", fencingToken), eventKind, strings.Join(ids, ",")}, "\x00")))
}

func (c *SQLiteSemanticExecutionCoordinator) initContinuityProjectionOutbox() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	_, err := c.db.Exec(`CREATE TABLE IF NOT EXISTS semantic_continuity_projection_outbox (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		event_key TEXT NOT NULL UNIQUE,
		tenant_id TEXT NOT NULL DEFAULT '',
		route_key TEXT NOT NULL,
		lineage_key TEXT NOT NULL,
		fencing_token INTEGER NOT NULL,
		event_kind TEXT NOT NULL CHECK(event_kind IN ('route_published','execution_updated')),
		payload_json BLOB NOT NULL,
		state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','applied','obsolete')),
		created_at TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	// Existing rows predate immutable tenant binding. They cannot safely be
	// projected in a multi-tenant process, so retire them rather than letting a
	// future consumer supply a tenant after the fact.
	if _, err := c.db.Exec(`ALTER TABLE semantic_continuity_projection_outbox ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if _, err := c.db.Exec(`CREATE INDEX IF NOT EXISTS idx_semantic_continuity_projection_outbox_tenant_pending ON semantic_continuity_projection_outbox(tenant_id, state, sequence)`); err != nil {
		return err
	}
	_, err = c.db.Exec(`UPDATE semantic_continuity_projection_outbox SET state='obsolete', applied_at=created_at WHERE tenant_id='' AND state='pending'`)
	return err
}

func continuityRouteRevisionTx(tx *sql.Tx, scope InvocationScope) (RouteRevisionRef, uint64, error) {
	var revision uint64
	var planID, digest string
	var fencing uint64
	err := tx.QueryRow(`SELECT rr.revision, rs.plan_id, rs.plan_digest, rr.fencing_token FROM semantic_route_revisions rr JOIN semantic_route_states rs ON rs.route_key=rr.route_key WHERE rr.route_key=?`, routeStateKey(scope)).Scan(&revision, &planID, &digest, &fencing)
	if err != nil {
		return RouteRevisionRef{}, 0, err
	}
	return RouteRevisionRef{RootTaskID: scope.RootTaskID, SessionID: scope.SessionID, PrincipalID: scope.PrincipalID, Revision: revision, PlanID: planID, PlanDigest: digest}, fencing, nil
}

func continuityCompletedSelectionIDsTx(tx *sql.Tx, scope InvocationScope) ([]string, error) {
	rows, err := tx.Query(`SELECT selection_id FROM semantic_route_completed_selections WHERE route_key=? ORDER BY selection_id`, routeStateKey(scope))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func continuityTenantForRouteTx(c *SQLiteSemanticExecutionCoordinator, tx *sql.Tx, scope InvocationScope) (string, error) {
	var tenantID string
	if err := tx.QueryRow(`SELECT tenant_id FROM semantic_route_states WHERE route_key=?`, routeStateKey(scope)).Scan(&tenantID); err != nil {
		return "", err
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" && c != nil {
		// A route created through the pre-coordinator RouteStateStore has no
		// tenant column value. It is safe to migrate only to this coordinator's
		// fixed single-tenant owner; a request/consumer must never choose it.
		tenantID = strings.TrimSpace(c.continuityTenantID)
		if tenantID != "" {
			if _, err := tx.Exec(`UPDATE semantic_route_states SET tenant_id=? WHERE route_key=? AND tenant_id=''`, tenantID, routeStateKey(scope)); err != nil {
				return "", err
			}
		}
	}
	if tenantID == "" {
		return "", fmt.Errorf("continuity_scope_required")
	}
	return tenantID, nil
}

func recordContinuityProjectionTx(c *SQLiteSemanticExecutionCoordinator, tx *sql.Tx, eventKind string, scope InvocationScope, revision RouteRevisionRef, fencingToken uint64, plan ToolPlan, completedIDs []string, now time.Time) error {
	if eventKind != continuityProjectionRoutePublished && eventKind != continuityProjectionExecutionUpdate {
		return fmt.Errorf("continuity_projection_event_invalid")
	}
	tenantID, err := continuityTenantForRouteTx(c, tx, scope)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(continuityProjectionPayload{TenantID: tenantID, Scope: scope, Revision: revision, FencingToken: fencingToken, Needs: continuityNeedFacts(plan), CompletedIDs: append([]string(nil), completedIDs...)})
	if err != nil {
		return err
	}
	key := continuityProjectionKey(tenantID, routeStateKey(scope), fencingToken, eventKind, completedIDs)
	_, err = tx.Exec(`INSERT OR IGNORE INTO semantic_continuity_projection_outbox(event_key, tenant_id, route_key, lineage_key, fencing_token, event_kind, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, key, tenantID, routeStateKey(scope), routeLineageKey(scope), fencingToken, eventKind, payload, routeStateTime(now))
	return err
}

// ContinuityProjectionEvent returns one immutable outbox event. It is safe for
// a consumer to retry the same event because ApplyContinuityProjection records
// its sequence in the projected state and atomically marks the event terminal.
func (c *SQLiteSemanticExecutionCoordinator) ContinuityProjectionEvent(sequence uint64) (ContinuityProjectionEvent, error) {
	if c == nil || c.db == nil || sequence == 0 {
		return ContinuityProjectionEvent{}, fmt.Errorf("continuity_projection_event_required")
	}
	return c.loadContinuityProjectionEvent(c.db, sequence)
}

// DrainContinuityProjections is the best-effort projection consumer used by a
// trusted host after a route transaction commits. Projection failure is
// deliberately reported to the caller but never fed back into RouteState: the
// pending row remains durable and can be retried after restart. limit <= 0
// selects a small bounded batch to prevent one tenant's backlog from turning a
// request path into an unbounded recovery job.
func (c *SQLiteSemanticExecutionCoordinator) DrainContinuityProjections(tenantID string, limit int, now time.Time) (int, error) {
	if c == nil || c.db == nil || strings.TrimSpace(tenantID) == "" {
		return 0, fmt.Errorf("continuity_scope_required")
	}
	if limit <= 0 {
		limit = 32
	}
	rows, err := c.db.Query(`SELECT sequence FROM semantic_continuity_projection_outbox WHERE tenant_id=? AND state='pending' ORDER BY sequence LIMIT ?`, strings.TrimSpace(tenantID), limit)
	if err != nil {
		return 0, err
	}
	sequences := make([]uint64, 0, limit)
	for rows.Next() {
		var sequence uint64
		if err := rows.Scan(&sequence); err != nil {
			_ = rows.Close()
			return 0, err
		}
		sequences = append(sequences, sequence)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	applied := 0
	for _, sequence := range sequences {
		event, err := c.ContinuityProjectionEvent(sequence)
		if err != nil {
			return applied, err
		}
		if strings.TrimSpace(event.TenantID) != strings.TrimSpace(tenantID) {
			return applied, fmt.Errorf("continuity_projection_tenant_mismatch")
		}
		scope, err := continuityScopeForInvocation(event.TenantID, event.Scope)
		if err != nil {
			return applied, err
		}
		expected := uint64(0)
		state, stateErr := c.ContinuityState(scope)
		if stateErr == nil {
			expected = state.Version
		} else if stateErr != sql.ErrNoRows {
			return applied, stateErr
		}
		if _, err := c.ApplyContinuityProjection(sequence, expected, tenantID, now); err != nil {
			if err.Error() == "continuity_projection_superseded" {
				applied++
				continue
			}
			return applied, err
		}
		applied++
	}
	return applied, nil
}

type continuityProjectionQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func (c *SQLiteSemanticExecutionCoordinator) loadContinuityProjectionEvent(q continuityProjectionQuerier, sequence uint64) (ContinuityProjectionEvent, error) {
	var event ContinuityProjectionEvent
	var payloadJSON []byte
	var created string
	if err := q.QueryRow(`SELECT sequence, event_key, tenant_id, event_kind, fencing_token, payload_json, created_at FROM semantic_continuity_projection_outbox WHERE sequence=?`, sequence).Scan(&event.Sequence, &event.EventKey, &event.TenantID, &event.EventKind, &event.FencingToken, &payloadJSON, &created); err != nil {
		return ContinuityProjectionEvent{}, err
	}
	var payload continuityProjectionPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil || payload.FencingToken != event.FencingToken || strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(payload.TenantID) != strings.TrimSpace(event.TenantID) {
		return ContinuityProjectionEvent{}, fmt.Errorf("continuity_projection_corrupt")
	}
	event.Scope, event.Revision, event.Needs, event.CompletedIDs = payload.Scope, payload.Revision, payload.Needs, payload.CompletedIDs
	event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return event, nil
}

// ContinuityState returns a rebuildable fact projection for exactly one trusted
// tenant/principal/conversation/root-task scope. A mismatch with route scope is
// rejected by ApplyContinuityProjection before any write can happen.
func (c *SQLiteSemanticExecutionCoordinator) ContinuityState(scope ContinuityScope) (ContinuityState, error) {
	if c == nil || c.db == nil {
		return ContinuityState{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := validateContinuityScope(scope); err != nil {
		return ContinuityState{}, err
	}
	return c.loadContinuityState(c.db, scope)
}

// FindOpenContinuityStates returns only fact projections whose referenced
// revision is still current for the same trusted principal/session/root-task.
// It is intentionally a relation candidate lookup, not an authorization
// search: callers must still require an explicit continue/refine decision and
// reject multiple candidates instead of merging their needs.
func (c *SQLiteSemanticExecutionCoordinator) FindOpenContinuityStates(tenantID, principalID, conversationID string) ([]ContinuityState, error) {
	if c == nil || c.db == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(principalID) == "" || strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("continuity_scope_required")
	}
	rows, err := c.db.Query(`SELECT cs.root_task_id, cs.version, cs.last_projection_sequence, cs.fencing_token, cs.open_needs_json, cs.completed_evidence_json, cs.route_revision_json, cs.updated_at
		FROM semantic_continuity_states cs
		JOIN semantic_route_lineages rl ON rl.root_task_id=cs.root_task_id AND rl.session_id=cs.conversation_id AND rl.principal_id=cs.principal_id AND rl.fencing_token=cs.fencing_token
		WHERE cs.tenant_id=? AND cs.principal_id=? AND cs.conversation_id=?
		ORDER BY cs.updated_at DESC`, strings.TrimSpace(tenantID), strings.TrimSpace(principalID), strings.TrimSpace(conversationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := make([]ContinuityState, 0)
	for rows.Next() {
		var state ContinuityState
		var openJSON, completedJSON, revisionJSON []byte
		var updated string
		if err := rows.Scan(&state.Scope.RootTaskID, &state.Version, &state.LastProjectionSequence, &state.FencingToken, &openJSON, &completedJSON, &revisionJSON, &updated); err != nil {
			return nil, err
		}
		if json.Unmarshal(openJSON, &state.OpenNeeds) != nil || json.Unmarshal(completedJSON, &state.CompletedEvidence) != nil || json.Unmarshal(revisionJSON, &state.RouteRevision) != nil {
			return nil, fmt.Errorf("continuity_state_corrupt")
		}
		if len(state.OpenNeeds) == 0 {
			continue
		}
		state.Scope.TenantID, state.Scope.PrincipalID, state.Scope.ConversationID = strings.TrimSpace(tenantID), strings.TrimSpace(principalID), strings.TrimSpace(conversationID)
		state.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		states = append(states, state)
	}
	return states, rows.Err()
}

func (c *SQLiteSemanticExecutionCoordinator) loadContinuityState(q continuityProjectionQuerier, scope ContinuityScope) (ContinuityState, error) {
	var state ContinuityState
	var openJSON, completedJSON, revisionJSON []byte
	var updated string
	if err := q.QueryRow(`SELECT version, last_projection_sequence, fencing_token, open_needs_json, completed_evidence_json, route_revision_json, updated_at FROM semantic_continuity_states WHERE state_key=?`, continuityStateKey(scope)).Scan(&state.Version, &state.LastProjectionSequence, &state.FencingToken, &openJSON, &completedJSON, &revisionJSON, &updated); err != nil {
		return ContinuityState{}, err
	}
	if json.Unmarshal(openJSON, &state.OpenNeeds) != nil || json.Unmarshal(completedJSON, &state.CompletedEvidence) != nil || json.Unmarshal(revisionJSON, &state.RouteRevision) != nil {
		return ContinuityState{}, fmt.Errorf("continuity_state_corrupt")
	}
	state.Scope = scope
	state.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return state, nil
}

func (c *SQLiteSemanticExecutionCoordinator) initContinuityStateStore() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	_, err := c.db.Exec(`CREATE TABLE IF NOT EXISTS semantic_continuity_states (
		state_key TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		principal_id TEXT NOT NULL,
		conversation_id TEXT NOT NULL,
		root_task_id TEXT NOT NULL,
		version INTEGER NOT NULL,
		last_projection_sequence INTEGER NOT NULL,
		fencing_token INTEGER NOT NULL,
		open_needs_json BLOB NOT NULL,
		completed_evidence_json BLOB NOT NULL,
		route_revision_json BLOB NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	return err
}

// ApplyContinuityProjection applies an outbox event with an optional expected
// state version. The route lineage is rechecked in the same transaction, so an
// event from a superseded revision becomes obsolete instead of overwriting a
// newer task. expectedVersion is zero only for a first projection.
func (c *SQLiteSemanticExecutionCoordinator) ApplyContinuityProjection(sequence, expectedVersion uint64, tenantID string, now time.Time) (ContinuityState, error) {
	if c == nil || c.db == nil || sequence == 0 {
		return ContinuityState{}, fmt.Errorf("continuity_projection_event_required")
	}
	if strings.TrimSpace(tenantID) == "" {
		return ContinuityState{}, fmt.Errorf("continuity_scope_required")
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return ContinuityState{}, err
	}
	defer func() { _ = tx.Rollback() }()
	event, err := c.loadContinuityProjectionEvent(tx, sequence)
	if err != nil {
		return ContinuityState{}, err
	}
	if strings.TrimSpace(event.TenantID) != strings.TrimSpace(tenantID) {
		return ContinuityState{}, fmt.Errorf("continuity_projection_tenant_mismatch")
	}
	scope, err := continuityScopeForInvocation(event.TenantID, event.Scope)
	if err != nil {
		return ContinuityState{}, err
	}
	var currentRouteKey string
	var currentFencing uint64
	err = tx.QueryRow(`SELECT current_route_key, fencing_token FROM semantic_route_lineages WHERE lineage_key=?`, routeLineageKey(event.Scope)).Scan(&currentRouteKey, &currentFencing)
	if err != nil {
		return ContinuityState{}, err
	}
	if currentRouteKey != routeStateKey(event.Scope) || currentFencing != event.FencingToken {
		if _, err := tx.Exec(`UPDATE semantic_continuity_projection_outbox SET state='obsolete', applied_at=? WHERE sequence=? AND state='pending'`, routeStateTime(now), sequence); err != nil {
			return ContinuityState{}, err
		}
		if err := tx.Commit(); err != nil {
			return ContinuityState{}, err
		}
		return ContinuityState{}, fmt.Errorf("continuity_projection_superseded")
	}
	state, loadErr := c.loadContinuityState(tx, scope)
	if loadErr != nil && loadErr != sql.ErrNoRows {
		return ContinuityState{}, loadErr
	}
	if loadErr == nil && state.LastProjectionSequence >= sequence {
		if _, err := tx.Exec(`UPDATE semantic_continuity_projection_outbox SET state='applied', applied_at=? WHERE sequence=? AND state='pending'`, routeStateTime(now), sequence); err != nil {
			return ContinuityState{}, err
		}
		if err := tx.Commit(); err != nil {
			return ContinuityState{}, err
		}
		return state, nil
	}
	actualVersion := uint64(0)
	if loadErr == nil {
		actualVersion = state.Version
	}
	if actualVersion != expectedVersion {
		return ContinuityState{}, fmt.Errorf("continuity_state_version_conflict")
	}
	state = continuityStateFromEvent(scope, event, actualVersion+1, now)
	openJSON, _ := json.Marshal(state.OpenNeeds)
	completedJSON, _ := json.Marshal(state.CompletedEvidence)
	revisionJSON, _ := json.Marshal(state.RouteRevision)
	if _, err := tx.Exec(`INSERT INTO semantic_continuity_states(state_key, tenant_id, principal_id, conversation_id, root_task_id, version, last_projection_sequence, fencing_token, open_needs_json, completed_evidence_json, route_revision_json, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(state_key) DO UPDATE SET version=excluded.version, last_projection_sequence=excluded.last_projection_sequence, fencing_token=excluded.fencing_token, open_needs_json=excluded.open_needs_json, completed_evidence_json=excluded.completed_evidence_json, route_revision_json=excluded.route_revision_json, updated_at=excluded.updated_at`, continuityStateKey(scope), scope.TenantID, scope.PrincipalID, scope.ConversationID, scope.RootTaskID, state.Version, state.LastProjectionSequence, state.FencingToken, openJSON, completedJSON, revisionJSON, routeStateTime(now)); err != nil {
		return ContinuityState{}, err
	}
	if _, err := tx.Exec(`UPDATE semantic_continuity_projection_outbox SET state='applied', applied_at=? WHERE sequence=? AND state='pending'`, routeStateTime(now), sequence); err != nil {
		return ContinuityState{}, err
	}
	if err := tx.Commit(); err != nil {
		return ContinuityState{}, err
	}
	return state, nil
}

func continuityStateFromEvent(scope ContinuityScope, event ContinuityProjectionEvent, version uint64, now time.Time) ContinuityState {
	completed := make(map[string]bool, len(event.CompletedIDs))
	for _, id := range event.CompletedIDs {
		completed[id] = true
	}
	state := ContinuityState{Scope: scope, RouteRevision: event.Revision, FencingToken: event.FencingToken, Version: version, LastProjectionSequence: event.Sequence, UpdatedAt: now}
	for _, fact := range event.Needs {
		if strings.TrimSpace(fact.SelectionID) == "" || strings.TrimSpace(fact.Need.ID) == "" || strings.TrimSpace(string(fact.Need.Capability)) == "" {
			continue
		}
		if completed[fact.SelectionID] {
			state.CompletedEvidence = append(state.CompletedEvidence, ContinuityEvidence{NeedID: fact.Need.ID, Capability: fact.Need.Capability, Provenance: "route_completion", CompletedAt: now})
			continue
		}
		state.OpenNeeds = append(state.OpenNeeds, cloneContinuityNeed(fact.Need))
	}
	sort.Slice(state.OpenNeeds, func(i, j int) bool { return state.OpenNeeds[i].ID < state.OpenNeeds[j].ID })
	sort.Slice(state.CompletedEvidence, func(i, j int) bool { return state.CompletedEvidence[i].NeedID < state.CompletedEvidence[j].NeedID })
	return state
}
