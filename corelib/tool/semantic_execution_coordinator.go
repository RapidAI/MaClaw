package tool

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteSemanticExecutionCoordinator is the single durable ownership boundary
// for a semantic invocation. Its component stores deliberately share one
// SQLite connection/database so that host-call admission, one-time grant
// consumption, execution creation, completion projection, and the delivery
// outbox all have one recoverable persistence domain.
//
// Provider/channel I/O is never performed by this type. Admit commits the
// intent first; Complete commits the observed result afterwards. A process
// loss between them leaves an admitted/running record which recovery changes
// to unknown, never a candidate for blind redispatch.
type SQLiteSemanticExecutionCoordinator struct {
	db                 *sql.DB
	continuityTenantID string
	Grants             *SQLiteInvocationGrantStore
	Executions         *SQLitePlanExecutionStore
	Routes             *SQLiteRouteStateStore
	HostCalls          *SQLiteHostCallJournal
	Artifacts          *SQLiteArtifactStore
}

// OutboxClaim is the trusted, immutable payload handle yielded to a channel
// worker after it atomically claims a prepared delivery. The worker receives
// no model arguments, provider credentials, or mutable selection metadata.
//
// FencingToken is the monotonic token allocated to this claim inside the
// claim transaction; HolderID reserves multi-replica gateway ownership
// identity (empty on single-node hosts). Settlement re-checks the token
// against the current route lineage, so a claim from a superseded revision
// can only converge to unknown, never to a second dispatch.
type OutboxClaim struct {
	Delivery     DeliveryRecord
	Payload      ArtifactPayload
	FencingToken uint64
	HolderID     string
}

const (
	// DefaultArtifactQuotaBytes is the reviewed per-principal payload cap
	// for host-owned semantic coordinators.
	DefaultArtifactQuotaBytes int64 = 256 << 20
	// DefaultArtifactRetention is the reviewed payload retention used by
	// SweepExpiredArtifacts. In-flight deliveries keep referenced payloads.
	DefaultArtifactRetention = 30 * 24 * time.Hour
)

// SemanticCoordinatorOption configures the coordinator before its schema is
// initialized. These are the hooks later host slices (agentservice, GUI) use
// to wire quota/retention/encryption without changing this package.
type SemanticCoordinatorOption func(*SQLiteSemanticExecutionCoordinator)

// WithCoordinatorContinuityTenant configures the trusted default tenant for
// a single-tenant host. Multi-tenant hosts must set SurfacePublishRequest's
// TenantID from their authenticated principal on every publish instead.
func WithCoordinatorContinuityTenant(tenantID string) SemanticCoordinatorOption {
	return func(c *SQLiteSemanticExecutionCoordinator) {
		if c != nil {
			c.continuityTenantID = strings.TrimSpace(tenantID)
		}
	}
}

// WithCoordinatorArtifactQuotaBytes sets the per-principal payload quota of
// the coordinator-owned artifact store.
func WithCoordinatorArtifactQuotaBytes(bytes int64) SemanticCoordinatorOption {
	return func(c *SQLiteSemanticExecutionCoordinator) { WithArtifactQuotaBytes(bytes)(c.Artifacts) }
}

// WithCoordinatorArtifactRetention sets the artifact payload retention used
// by SweepExpiredArtifacts on the coordinator-owned artifact store.
func WithCoordinatorArtifactRetention(retention time.Duration) SemanticCoordinatorOption {
	return func(c *SQLiteSemanticExecutionCoordinator) { WithArtifactRetention(retention)(c.Artifacts) }
}

// WithCoordinatorArtifactEncryptionKey injects the host-owned 32-byte key
// from which the artifact payload AEAD key is derived (for example the
// agentservice data-root key). Without it payloads stay plaintext at rest,
// preserving pre-encryption databases and behavior.
func WithCoordinatorArtifactEncryptionKey(key []byte) SemanticCoordinatorOption {
	return func(c *SQLiteSemanticExecutionCoordinator) { WithArtifactEncryptionKey(key)(c.Artifacts) }
}

// NewSQLiteSemanticExecutionCoordinator opens the unified semantic execution
// database. Existing split databases are intentionally not merged implicitly:
// they may contain ambiguous in-flight effects and must be reconciled by the
// old recovery path before a host migrates to this owner.
func NewSQLiteSemanticExecutionCoordinator(path string, opts ...SemanticCoordinatorOption) (*SQLiteSemanticExecutionCoordinator, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("semantic execution coordinator path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create semantic execution directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// A coordinator opened without an explicit multi-tenant host is a
	// single-tenant local store. The fixed default preserves that boundary for
	// desktop/test hosts; shared hosts must override it or set request TenantID.
	c := &SQLiteSemanticExecutionCoordinator{db: db, continuityTenantID: "tenant"}
	c.Grants = &SQLiteInvocationGrantStore{db: db}
	c.Executions = &SQLitePlanExecutionStore{db: db}
	c.Routes = &SQLiteRouteStateStore{db: db}
	c.HostCalls = &SQLiteHostCallJournal{db: db}
	c.Artifacts = &SQLiteArtifactStore{db: db}
	for _, opt := range opts {
		opt(c)
	}
	for _, init := range []func() error{c.Grants.init, c.Executions.init, c.Routes.init, c.HostCalls.init, c.Artifacts.init, c.initExternalEffects, c.initSurfacePublishOutbox, c.initModelRequestSurfaces, c.initContinuityProjectionOutbox, c.initContinuityStateStore, c.initTaskContinuationHandles, c.initTaskAmendmentCommands} {
		if err := init(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize semantic execution coordinator: %w", err)
		}
	}
	return c, nil
}

func (c *SQLiteSemanticExecutionCoordinator) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	db := c.db
	c.db, c.Grants, c.Executions, c.Routes, c.HostCalls, c.Artifacts = nil, nil, nil, nil, nil, nil
	return db.Close()
}

// SurfacePublishRequest defines the complete durable boundary that must exist
// before a model can receive semantic tool definitions.  The plan revision,
// its ready grants, their exposed materializations, and the publish audit
// event are committed together; callers receive no definitions on failure.
type SurfacePublishRequest struct {
	Revision RouteRevisionPublishRequest
	// TenantID is host-authenticated partition data. It is written into the
	// route and continuity outbox transaction; it must never be supplied by a
	// model or reconstructed by an asynchronous projection consumer.
	TenantID string
	Issuer   *InvocationIssuer
	GrantTTL time.Duration
	Now      time.Time
}

// PublishSurface atomically publishes a route revision and its initial closed
// materialization. It is intentionally owned by the coordinator rather than
// composing RouteStateStore.PublishRevision, InvocationIssuer.IssueReady, and
// RecordMaterialization in the GUI: separate commits leave an executable
// orphan grant or a published revision with no model-visible closure.
func (c *SQLiteSemanticExecutionCoordinator) PublishSurface(request SurfacePublishRequest) (RouteState, []InvocationGrant, error) {
	if c == nil || c.db == nil || request.Issuer == nil {
		return RouteState{}, nil, fmt.Errorf("semantic surface publisher is unavailable")
	}
	if request.GrantTTL <= 0 {
		return RouteState{}, nil, fmt.Errorf("semantic surface grant ttl must be positive")
	}
	request.TenantID = strings.TrimSpace(request.TenantID)
	if request.TenantID == "" {
		request.TenantID = strings.TrimSpace(c.continuityTenantID)
	}
	if request.TenantID == "" {
		return RouteState{}, nil, fmt.Errorf("semantic surface tenant is required")
	}
	now := request.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	encoded, digest, err := validateRouteRevisionPublish(request.Revision)
	if err != nil {
		return RouteState{}, nil, err
	}
	if request.Issuer.store != c.Grants {
		return RouteState{}, nil, fmt.Errorf("semantic surface issuer must use coordinator grants")
	}
	routeKey, lineageKey := routeStateKey(request.Revision.Scope), routeLineageKey(request.Revision.Scope)
	tx, err := c.db.Begin()
	if err != nil {
		return RouteState{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingDigest, existingSnapshot, existingTenantID string
	err = tx.QueryRow(`SELECT rs.plan_digest, rr.snapshot_digest, rs.tenant_id FROM semantic_route_states rs JOIN semantic_route_revisions rr ON rr.route_key = rs.route_key WHERE rs.route_key = ?`, routeKey).Scan(&existingDigest, &existingSnapshot, &existingTenantID)
	if err == nil {
		if existingDigest != digest || existingSnapshot != request.Revision.SnapshotDigest || existingTenantID != request.TenantID {
			return RouteState{}, nil, fmt.Errorf("route_state_conflict")
		}
		if err := tx.Commit(); err != nil {
			return RouteState{}, nil, err
		}
		state, err := c.Routes.get(routeKey)
		if err != nil {
			return RouteState{}, nil, err
		}
		if !sameOptionalRouteRevisionRef(state.ParentRevision, request.Revision.ExpectedParent) && !sameOptionalRouteRevisionRef(state.Revision, request.Revision.ExpectedParent) {
			return RouteState{}, nil, fmt.Errorf("route_revision_conflict")
		}
		if !sameOptionalRouteAmendmentRef(state.Amendment, request.Revision.Amendment) {
			return RouteState{}, nil, fmt.Errorf("route_state_conflict")
		}
		return state, exposedMaterializationGrants(state), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RouteState{}, nil, err
	}

	parentRouteKey, parentPlanID, parentDigest := "", "", ""
	var parentRevision uint64
	lineageErr := tx.QueryRow(`SELECT current_route_key, current_revision, current_plan_id, current_plan_digest FROM semantic_route_lineages WHERE lineage_key = ?`, lineageKey).Scan(&parentRouteKey, &parentRevision, &parentPlanID, &parentDigest)
	if request.Revision.ExpectedParent == nil {
		if lineageErr == nil {
			return RouteState{}, nil, fmt.Errorf("route_revision_parent_required")
		}
		if !errors.Is(lineageErr, sql.ErrNoRows) {
			return RouteState{}, nil, lineageErr
		}
		parentRevision = 0
	} else {
		if lineageErr != nil {
			if errors.Is(lineageErr, sql.ErrNoRows) {
				return RouteState{}, nil, fmt.Errorf("route_revision_conflict")
			}
			return RouteState{}, nil, lineageErr
		}
		current := RouteRevisionRef{RootTaskID: request.Revision.Scope.RootTaskID, SessionID: request.Revision.Scope.SessionID, PrincipalID: request.Revision.Scope.PrincipalID, Revision: parentRevision, PlanID: parentPlanID, PlanDigest: parentDigest}
		if !sameRouteRevisionRef(current, *request.Revision.ExpectedParent) {
			return RouteState{}, nil, fmt.Errorf("route_revision_conflict")
		}
		if request.Revision.Amendment != nil && request.Revision.Amendment.ParentFencingToken != 0 {
			var currentFencing uint64
			if err := tx.QueryRow(`SELECT fencing_token FROM semantic_route_lineages WHERE lineage_key = ?`, lineageKey).Scan(&currentFencing); err != nil {
				return RouteState{}, nil, err
			}
			if currentFencing != request.Revision.Amendment.ParentFencingToken {
				return RouteState{}, nil, fmt.Errorf("route_amendment_parent_mismatch")
			}
		}
		// Supersession is an authorization boundary, not merely a renderer
		// update. Retire every request surface that could still resolve a model
		// alias and revoke the corresponding unconsumed grants in this same
		// transaction. A late response cannot otherwise spend parent authority
		// after the child route has won the lineage fence.
		if err := supersedeRouteSurfaceTx(tx, parentRouteKey, now); err != nil {
			return RouteState{}, nil, err
		}
	}

	if _, err := tx.Exec(`INSERT INTO semantic_route_states(route_key, version, tenant_id, root_task_id, plan_id, session_id, turn_id, principal_id, plan_json, plan_digest, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, routeKey, RouteStateVersion, request.TenantID, request.Revision.Scope.RootTaskID, request.Revision.Scope.PlanID, request.Revision.Scope.SessionID, request.Revision.Scope.TurnID, request.Revision.Scope.PrincipalID, encoded, digest, routeStateTime(now), routeStateTime(now)); err != nil {
		return RouteState{}, nil, err
	}
	fencingToken, err := nextOutboxFencingToken(tx)
	if err != nil {
		return RouteState{}, nil, err
	}
	revision := parentRevision + 1
	if _, err := tx.Exec(`INSERT INTO semantic_route_revisions(route_key, lineage_key, revision, parent_route_key, snapshot_digest, fencing_token) VALUES (?, ?, ?, ?, ?, ?)`, routeKey, lineageKey, revision, parentRouteKey, request.Revision.SnapshotDigest, fencingToken); err != nil {
		return RouteState{}, nil, err
	}
	if amendment := request.Revision.Amendment; amendment != nil {
		if err := consumeTaskAmendmentCommandTx(tx, amendment, request.TenantID, request.Revision.Scope, now); err != nil {
			return RouteState{}, nil, err
		}
		if _, err := tx.Exec(`INSERT INTO semantic_route_amendments(route_key, command_id, digest, parent_revision, parent_fencing_token) VALUES (?, ?, ?, ?, ?)`, routeKey, amendment.CommandID, amendment.Digest, amendment.ParentRevision, amendment.ParentFencingToken); err != nil {
			return RouteState{}, nil, err
		}
	}
	completed, err := copySurfacePublishParentFacts(tx, parentRouteKey, request.Revision.Plan, routeKey, now)
	if err != nil {
		return RouteState{}, nil, err
	}
	if _, err := tx.Exec(`INSERT INTO semantic_route_lineages(lineage_key, root_task_id, session_id, principal_id, current_route_key, current_revision, current_plan_id, current_plan_digest, fencing_token, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(lineage_key) DO UPDATE SET current_route_key=excluded.current_route_key, current_revision=excluded.current_revision, current_plan_id=excluded.current_plan_id, current_plan_digest=excluded.current_plan_digest, fencing_token=excluded.fencing_token, updated_at=excluded.updated_at`, lineageKey, request.Revision.Scope.RootTaskID, request.Revision.Scope.SessionID, request.Revision.Scope.PrincipalID, routeKey, revision, request.Revision.Plan.ID, digest, fencingToken, routeStateTime(now)); err != nil {
		return RouteState{}, nil, err
	}
	grants, err := issueSurfaceReadyGrantsTx(tx, request.Issuer, request.Revision.Plan, request.Revision.Scope, request.GrantTTL, completed)
	if err != nil {
		return RouteState{}, nil, err
	}
	for _, grant := range grants {
		materialization := RouteMaterialization{FunctionName: grant.Token, Grant: grant, State: RouteMaterializationExposed}
		if !routeMaterializationMatchesPlan(request.Revision.Plan, request.Revision.Scope, materialization) {
			return RouteState{}, nil, fmt.Errorf("route_state_grant_binding_mismatch")
		}
		grantJSON, err := json.Marshal(grant)
		if err != nil {
			return RouteState{}, nil, err
		}
		if _, err := tx.Exec(`INSERT INTO semantic_route_materializations(route_key, function_name, grant_json, state, created_at, updated_at) VALUES (?, ?, ?, 'exposed', ?, ?)`, routeKey, grant.Token, grantJSON, routeStateTime(now), routeStateTime(now)); err != nil {
			return RouteState{}, nil, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO semantic_surface_publish_outbox(route_key, lineage_key, fencing_token, event_kind, created_at) VALUES (?, ?, ?, 'surface_published', ?)`, routeKey, lineageKey, fencingToken, routeStateTime(now)); err != nil {
		return RouteState{}, nil, err
	}
	ref := routeRevisionRef(request.Revision.Scope, request.Revision.Plan.ID, digest, revision)
	completedIDs := make([]string, 0, len(completed))
	for selectionID := range completed {
		completedIDs = append(completedIDs, selectionID)
	}
	if err := recordContinuityProjectionTx(c, tx, continuityProjectionRoutePublished, request.Revision.Scope, ref, fencingToken, request.Revision.Plan, completedIDs, now); err != nil {
		return RouteState{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return RouteState{}, nil, err
	}
	state, err := c.Routes.get(routeKey)
	if err != nil {
		return RouteState{}, nil, err
	}
	return state, grants, nil
}

// MaterializeReadySurface atomically issues and records the next ready closure
// of an already-published current revision. It is used after a trusted DAG
// completion; a grant is never committed without its corresponding exposed
// materialization.
func (c *SQLiteSemanticExecutionCoordinator) MaterializeReadySurface(scope InvocationScope, issuer *InvocationIssuer, ttl time.Duration, completed map[string]bool, selectionIDs map[string]bool, now time.Time) (RouteState, []InvocationGrant, error) {
	if c == nil || c.db == nil || issuer == nil || issuer.store != c.Grants {
		return RouteState{}, nil, fmt.Errorf("semantic surface materializer is unavailable")
	}
	if ttl <= 0 {
		return RouteState{}, nil, fmt.Errorf("semantic surface grant ttl must be positive")
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return RouteState{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	plan, err := coordinatedPublishedPlan(tx, scope)
	if err != nil {
		return RouteState{}, nil, err
	}
	if err := routeRevisionIsCurrent(tx, scope); err != nil {
		return RouteState{}, nil, err
	}
	if len(selectionIDs) == 0 {
		return RouteState{}, nil, fmt.Errorf("semantic surface selections required")
	}
	readyIDs := make(map[string]bool, len(plan.Selections))
	for _, selection := range plan.ReadySelections(completed) {
		if !completed[selection.ID] {
			readyIDs[selection.ID] = true
		}
	}
	for selectionID := range selectionIDs {
		if !readyIDs[selectionID] {
			return RouteState{}, nil, fmt.Errorf("semantic surface selection not ready")
		}
	}
	partialPlan := semanticPlanWithSelectionIDs(plan, selectionIDs)
	if len(partialPlan.Selections) != len(selectionIDs) {
		return RouteState{}, nil, fmt.Errorf("semantic surface selection not found")
	}
	if err := ensureSurfaceSelectionsUnmaterialized(tx, routeStateKey(scope), selectionIDs); err != nil {
		return RouteState{}, nil, err
	}
	grants, err := issueSurfaceReadyGrantsTx(tx, issuer, partialPlan, scope, ttl, completed)
	if err != nil {
		return RouteState{}, nil, err
	}
	for _, grant := range grants {
		materialization := RouteMaterialization{FunctionName: grant.Token, Grant: grant, State: RouteMaterializationExposed}
		if !routeMaterializationMatchesPlan(plan, scope, materialization) {
			return RouteState{}, nil, fmt.Errorf("route_state_grant_binding_mismatch")
		}
		grantJSON, err := json.Marshal(grant)
		if err != nil {
			return RouteState{}, nil, err
		}
		if _, err := tx.Exec(`INSERT INTO semantic_route_materializations(route_key, function_name, grant_json, state, created_at, updated_at) VALUES (?, ?, ?, 'exposed', ?, ?)`, routeStateKey(scope), grant.Token, grantJSON, routeStateTime(now), routeStateTime(now)); err != nil {
			return RouteState{}, nil, err
		}
	}
	if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at=? WHERE route_key=?`, routeStateTime(now), routeStateKey(scope)); err != nil {
		return RouteState{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return RouteState{}, nil, err
	}
	state, err := c.Routes.get(routeStateKey(scope))
	if err != nil {
		return RouteState{}, nil, err
	}
	return state, grants, nil
}

func semanticPlanWithSelectionIDs(plan ToolPlan, selectionIDs map[string]bool) ToolPlan {
	partial := plan
	partial.Selections = make([]PlannedSelection, 0, len(selectionIDs))
	for _, selection := range plan.Selections {
		if selectionIDs[selection.ID] {
			partial.Selections = append(partial.Selections, selection)
		}
	}
	return partial
}

// ensureSurfaceSelectionsUnmaterialized is the durable half of the
// materialization idempotency check. Host-local maps are only caches; without
// this check two processes recovering the same revision could both mint a
// valid one-shot grant for one selection before either response is rendered.
func ensureSurfaceSelectionsUnmaterialized(tx *sql.Tx, routeKey string, selectionIDs map[string]bool) error {
	rows, err := tx.Query(`SELECT grant_json FROM semantic_route_materializations WHERE route_key=?`, routeKey)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			return err
		}
		var grant InvocationGrant
		if err := json.Unmarshal(encoded, &grant); err != nil || strings.TrimSpace(grant.SelectionID) == "" {
			return fmt.Errorf("route_state_corrupt")
		}
		if selectionIDs[grant.SelectionID] {
			return fmt.Errorf("semantic surface selection already materialized")
		}
	}
	return rows.Err()
}

func (c *SQLiteSemanticExecutionCoordinator) initSurfacePublishOutbox() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	_, err := c.db.Exec(`CREATE TABLE IF NOT EXISTS semantic_surface_publish_outbox (
		route_key TEXT PRIMARY KEY, lineage_key TEXT NOT NULL, fencing_token INTEGER NOT NULL,
		event_kind TEXT NOT NULL CHECK(event_kind='surface_published'), created_at TEXT NOT NULL
	)`)
	return err
}

func exposedMaterializationGrants(state RouteState) []InvocationGrant {
	grants := make([]InvocationGrant, 0, len(state.Materializations))
	for _, materialization := range state.Materializations {
		if materialization.State == RouteMaterializationExposed {
			grants = append(grants, materialization.Grant)
		}
	}
	return grants
}

func issueSurfaceReadyGrantsTx(tx *sql.Tx, issuer *InvocationIssuer, plan ToolPlan, scope InvocationScope, ttl time.Duration, completed map[string]bool) ([]InvocationGrant, error) {
	if issuer == nil || ttl <= 0 || scope.RootTaskID != plan.RootTaskID || scope.PlanID != plan.ID {
		return nil, fmt.Errorf("semantic surface grant input invalid")
	}
	now := issuer.now().UTC()
	grants := make([]InvocationGrant, 0)
	for _, selection := range plan.ReadySelections(completed) {
		if completed[selection.ID] {
			continue
		}
		if strings.TrimSpace(selection.ID) == "" || strings.TrimSpace(selection.AdapterName) == "" || strings.TrimSpace(selection.FitProof.Digest) == "" {
			return nil, fmt.Errorf("selection is not materializable")
		}
		nonce, err := randomInvocationNonce()
		if err != nil {
			return nil, err
		}
		grant := InvocationGrant{AdapterName: selection.AdapterName, SelectionID: selection.ID, ProviderBinding: selection.Provider.StableID(), FitProofDigest: selection.FitProof.Digest, ParameterAuthorization: selection.ParameterAuthorization, CatalogGeneration: plan.CatalogGeneration, Scope: scope, IssuedAt: now, ExpiresAt: now.Add(ttl), Nonce: nonce}
		grant.Token = invocationToken(grant)
		grant.Signature = issuer.sign(grant)
		if _, err := tx.Exec(`INSERT INTO invocation_grants(nonce, fingerprint, expires_at, state, created_at) VALUES (?, ?, ?, 'issued', ?)`, grant.Nonce, invocationGrantFingerprint(grant), grant.ExpiresAt.UTC().Format(time.RFC3339Nano), grant.IssuedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, nil
}

// copySurfacePublishParentFacts projects only compatible immutable facts from
// the parent revision. Grants and materializations are explicitly excluded.
func copySurfacePublishParentFacts(tx *sql.Tx, parentRouteKey string, child ToolPlan, routeKey string, now time.Time) (map[string]bool, error) {
	completed := make(map[string]bool)
	if parentRouteKey == "" {
		return completed, nil
	}
	var parentJSON []byte
	if err := tx.QueryRow(`SELECT plan_json FROM semantic_route_states WHERE route_key=?`, parentRouteKey).Scan(&parentJSON); err != nil {
		return nil, err
	}
	var parent ToolPlan
	if json.Unmarshal(parentJSON, &parent) != nil {
		return nil, fmt.Errorf("route_state_corrupt")
	}
	completedRows, err := tx.Query(`SELECT selection_id, purpose_digest, completed_at FROM semantic_route_completed_selections WHERE route_key=?`, parentRouteKey)
	if err != nil {
		return nil, err
	}
	parentCompleted := make([]RouteCompletedSelection, 0)
	for completedRows.Next() {
		var value RouteCompletedSelection
		var at string
		if err := completedRows.Scan(&value.SelectionID, &value.PurposeDigest, &at); err != nil {
			_ = completedRows.Close()
			return nil, err
		}
		value.CompletedAt, _ = time.Parse(time.RFC3339Nano, at)
		parentCompleted = append(parentCompleted, value)
	}
	if err := completedRows.Close(); err != nil {
		return nil, err
	}
	for _, value := range mergeRouteCompletedSelections(parent, child, parentCompleted, now) {
		if _, err := tx.Exec(`INSERT INTO semantic_route_completed_selections(route_key, selection_id, purpose_digest, completed_at) VALUES (?, ?, ?, ?)`, routeKey, value.SelectionID, value.PurposeDigest, routeStateTime(value.CompletedAt)); err != nil {
			return nil, err
		}
		completed[value.SelectionID] = true
	}
	confirmationRows, err := tx.Query(`SELECT requirement, purpose_digest, authority, valid_until, granted_at FROM semantic_route_confirmations WHERE route_key=?`, parentRouteKey)
	if err != nil {
		return nil, err
	}
	parentConfirmations := make([]RouteConfirmation, 0)
	for confirmationRows.Next() {
		var value RouteConfirmation
		var validUntil, grantedAt string
		if err := confirmationRows.Scan(&value.Requirement, &value.PurposeDigest, &value.Authority, &validUntil, &grantedAt); err != nil {
			_ = confirmationRows.Close()
			return nil, err
		}
		if validUntil != "" {
			value.ValidUntil, err = time.Parse(time.RFC3339Nano, validUntil)
			if err != nil {
				_ = confirmationRows.Close()
				return nil, fmt.Errorf("route_state_corrupt")
			}
		}
		value.GrantedAt, err = time.Parse(time.RFC3339Nano, grantedAt)
		if err != nil {
			_ = confirmationRows.Close()
			return nil, fmt.Errorf("route_state_corrupt")
		}
		parentConfirmations = append(parentConfirmations, value)
	}
	if err := confirmationRows.Close(); err != nil {
		return nil, err
	}
	for _, value := range mergeRouteConfirmations(parent, child, parentConfirmations, now) {
		validUntil := ""
		if !value.ValidUntil.IsZero() {
			validUntil = routeStateTime(value.ValidUntil)
		}
		if _, err := tx.Exec(`INSERT INTO semantic_route_confirmations(route_key, requirement, purpose_digest, authority, valid_until, granted_at) VALUES (?, ?, ?, ?, ?, ?)`, routeKey, value.Requirement, value.PurposeDigest, value.Authority, validUntil, routeStateTime(value.GrantedAt)); err != nil {
			return nil, err
		}
	}
	artifactRows, err := tx.Query(`SELECT artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at FROM semantic_route_artifacts WHERE route_key=?`, parentRouteKey)
	if err != nil {
		return nil, err
	}
	parentArtifacts := make([]RouteArtifactRef, 0)
	for artifactRows.Next() {
		var value RouteArtifactRef
		var createdAt string
		if err := artifactRows.Scan(&value.ArtifactID, &value.Kind, &value.MIMEType, &value.IntegrityDigest, &value.ProducerSelection, &value.ProducerPurposeDigest, &value.SourceScope.RootTaskID, &value.SourceScope.PlanID, &value.SourceScope.SessionID, &value.SourceScope.TurnID, &value.SourceScope.PrincipalID, &createdAt); err != nil {
			_ = artifactRows.Close()
			return nil, err
		}
		value.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil || validateRouteArtifactRef(value) != nil {
			_ = artifactRows.Close()
			return nil, fmt.Errorf("route_state_corrupt")
		}
		parentArtifacts = append(parentArtifacts, value)
	}
	if err := artifactRows.Close(); err != nil {
		return nil, err
	}
	for _, value := range mergeRouteArtifactRefs(parent, child, parentCompleted, parentArtifacts) {
		if _, err := tx.Exec(`INSERT INTO semantic_route_artifacts(route_key, artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, routeKey, value.ArtifactID, value.Kind, value.MIMEType, value.IntegrityDigest, value.ProducerSelection, value.ProducerPurposeDigest, value.SourceScope.RootTaskID, value.SourceScope.PlanID, value.SourceScope.SessionID, value.SourceScope.TurnID, value.SourceScope.PrincipalID, routeStateTime(value.CreatedAt)); err != nil {
			return nil, err
		}
	}
	return completed, nil
}

// SemanticExecutionAdmission binds one host call to the immutable invocation
// authority selected by the planner. RequestDigest is either the canonical
// request digest or a deterministic raw-request digest for a rejected parse.
// In both cases the coordinator consumes a valid one-shot grant before it
// records a terminal result. This prevents a model from probing schemas with
// malformed requests and then reusing the same grant for a different call.
type SemanticExecutionAdmission struct {
	Identity      HostCallIdentity
	Grant         InvocationGrant
	RequestDigest string
	Scope         InvocationScope
	Selection     PlannedSelection
	Now           time.Time
}

func (a SemanticExecutionAdmission) validate() error {
	if err := validateHostCallInputs(a.Identity, InvocationGrantFingerprint(a.Grant), a.RequestDigest); err != nil {
		return err
	}
	if a.Scope != a.Grant.Scope || a.Scope != a.SelectionScope() {
		return fmt.Errorf("semantic_execution_scope_mismatch")
	}
	if strings.TrimSpace(a.Selection.ID) == "" || strings.TrimSpace(a.Grant.Nonce) == "" {
		return fmt.Errorf("semantic_execution_selection_required")
	}
	return nil
}

func (a SemanticExecutionAdmission) SelectionScope() InvocationScope { return a.Grant.Scope }

// Admit is the pre-I/O transaction. Replay/conflict/in-progress results come
// from the host-call journal without modifying grant or execution state.
func (c *SQLiteSemanticExecutionCoordinator) Admit(a SemanticExecutionAdmission) (HostCallRecord, HostCallAcquireAction, error) {
	if c == nil || c.db == nil {
		return HostCallRecord{}, "", fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := a.validate(); err != nil {
		return HostCallRecord{}, "", err
	}
	now := a.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	fingerprint, key := InvocationGrantFingerprint(a.Grant), hostCallKey(a.Identity)
	tx, err := c.db.Begin()
	if err != nil {
		return HostCallRecord{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	if existing, found, err := coordinatedHostCall(tx, key); err != nil {
		return HostCallRecord{}, "", err
	} else if found {
		if !sameHostCallBinding(existing, fingerprint, a.RequestDigest) {
			return existing, HostCallAcquireConflict, nil
		}
		return existing, hostCallAcquireAction(existing.State), nil
	}
	// The route revision check and grant consumption must share this transaction.
	// A read through Routes.IsCurrent before Begin would leave a window where a
	// child revision could supersede this surface and the old grant would still
	// be consumed. Existing host-call records are deliberately handled above so
	// a retry can retain its durable idempotency result; this check protects the
	// creation of new execution authority only.
	if err := routeRevisionIsCurrent(tx, a.Scope); err != nil {
		return HostCallRecord{}, "", err
	}
	result, err := tx.Exec(`UPDATE invocation_grants SET state='consumed' WHERE nonce=? AND fingerprint=? AND state='issued' AND expires_at > ?`, a.Grant.Nonce, fingerprint, now.Format(time.RFC3339Nano))
	if err != nil {
		return HostCallRecord{}, "", err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return HostCallRecord{}, "", err
	}
	if changed != 1 {
		return HostCallRecord{}, "", coordinatedGrantState(tx, a.Grant.Nonce, fingerprint, now)
	}
	started := now.Format(time.RFC3339Nano)
	executionKey := planExecutionKey(a.Scope, a.Selection.ID)
	inserted, err := tx.Exec(`INSERT OR IGNORE INTO semantic_plan_executions(execution_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, state, started_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'running', ?, ?)`, executionKey, a.Scope.RootTaskID, a.Scope.PlanID, a.Scope.SessionID, a.Scope.TurnID, a.Scope.PrincipalID, a.Selection.ID, started, started)
	if err != nil {
		return HostCallRecord{}, "", err
	}
	if n, _ := inserted.RowsAffected(); n != 1 {
		return HostCallRecord{}, "", fmt.Errorf("selection_execution_exists")
	}
	if _, err := tx.Exec(`INSERT INTO semantic_host_calls(call_key, protocol, connection_id, call_id, surface_epoch, grant_fingerprint, request_digest, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'admitted', ?, ?)`, key, a.Identity.Protocol, a.Identity.ConnectionID, a.Identity.CallID, a.Identity.SurfaceEpoch, fingerprint, a.RequestDigest, started, started); err != nil {
		return HostCallRecord{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return HostCallRecord{}, "", err
	}
	return HostCallRecord{Identity: a.Identity, GrantFingerprint: fingerprint, RequestDigest: a.RequestDigest, State: HostCallAdmitted, CreatedAt: now, UpdatedAt: now}, HostCallAcquireAdmit, nil
}

// Reject atomically consumes a valid grant and records a deterministic
// pre-I/O result such as JSON/schema/canonicalization rejection. It follows
// the same host-call and grant boundary as Admit, except it creates a failed
// execution and a completed journal result rather than a running execution.
// A replay of the same host call returns the stored rejection; a different
// host call cannot reuse the consumed grant.
func (c *SQLiteSemanticExecutionCoordinator) Reject(a SemanticExecutionAdmission, result, reasonCode string) (HostCallRecord, HostCallAcquireAction, error) {
	if c == nil || c.db == nil {
		return HostCallRecord{}, "", fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := a.validate(); err != nil {
		return HostCallRecord{}, "", err
	}
	if err := validateHostCallResult(result); err != nil {
		return HostCallRecord{}, "", err
	}
	now := a.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	fingerprint, key := InvocationGrantFingerprint(a.Grant), hostCallKey(a.Identity)
	tx, err := c.db.Begin()
	if err != nil {
		return HostCallRecord{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	if existing, found, err := coordinatedHostCall(tx, key); err != nil {
		return HostCallRecord{}, "", err
	} else if found {
		if !sameHostCallBinding(existing, fingerprint, a.RequestDigest) {
			return existing, HostCallAcquireConflict, nil
		}
		return existing, hostCallAcquireAction(existing.State), nil
	}
	// Reject consumes the same one-shot grant as Admit. It therefore needs the
	// identical in-transaction revision fence: a stale model response must not
	// be able to burn an old grant or create a terminal execution record.
	if err := routeRevisionIsCurrent(tx, a.Scope); err != nil {
		return HostCallRecord{}, "", err
	}
	consumed, err := tx.Exec(`UPDATE invocation_grants SET state='consumed' WHERE nonce=? AND fingerprint=? AND state='issued' AND expires_at > ?`, a.Grant.Nonce, fingerprint, now.Format(time.RFC3339Nano))
	if err != nil {
		return HostCallRecord{}, "", err
	}
	if n, err := consumed.RowsAffected(); err != nil {
		return HostCallRecord{}, "", err
	} else if n != 1 {
		return HostCallRecord{}, "", coordinatedGrantState(tx, a.Grant.Nonce, fingerprint, now)
	}
	started := now.Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT INTO semantic_plan_executions(execution_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, state, result_digest, reason_code, started_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'failed', ?, ?, ?, ?)`, planExecutionKey(a.Scope, a.Selection.ID), a.Scope.RootTaskID, a.Scope.PlanID, a.Scope.SessionID, a.Scope.TurnID, a.Scope.PrincipalID, a.Selection.ID, SchemaDigest([]byte(result)), strings.TrimSpace(reasonCode), started, started); err != nil {
		return HostCallRecord{}, "", err
	}
	if _, err := tx.Exec(`INSERT INTO semantic_host_calls(call_key, protocol, connection_id, call_id, surface_epoch, grant_fingerprint, request_digest, state, result, result_digest, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'completed', ?, ?, ?, ?)`, key, a.Identity.Protocol, a.Identity.ConnectionID, a.Identity.CallID, a.Identity.SurfaceEpoch, fingerprint, a.RequestDigest, result, SchemaDigest([]byte(result)), started, started); err != nil {
		return HostCallRecord{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return HostCallRecord{}, "", err
	}
	return HostCallRecord{Identity: a.Identity, GrantFingerprint: fingerprint, RequestDigest: a.RequestDigest, State: HostCallCompleted, Result: result, ResultDigest: SchemaDigest([]byte(result)), CreatedAt: now, UpdatedAt: now}, HostCallAcquireAdmit, nil
}

// Complete is the post-I/O transaction. The route completion projection is
// inserted in the same commit as the execution terminal state and host result,
// so a successful provider response can never unlock a DAG node in only one
// of those recovery views.
func (c *SQLiteSemanticExecutionCoordinator) Complete(a SemanticExecutionAdmission, state PlanExecutionState, result, reasonCode string, now time.Time) (HostCallRecord, error) {
	return c.CompleteWithArtifacts(a, state, result, reasonCode, nil, now)
}

// CompleteWithArtifacts is the post-I/O commit for a selection that produced
// artifacts. The payload must already be persisted by the trusted capture
// adapter in this coordinator's artifact table; this method atomically makes
// its immutable RouteState projection visible together with execution success
// and the host-call result. Thus no later phase can observe success without
// the precise artifact fact that success is meant to produce.
func (c *SQLiteSemanticExecutionCoordinator) CompleteWithArtifacts(a SemanticExecutionAdmission, state PlanExecutionState, result, reasonCode string, artifacts []ArtifactRef, now time.Time) (HostCallRecord, error) {
	return c.complete(a, state, result, reasonCode, artifacts, nil, now)
}

// CompleteWithArtifactPayloads persists newly produced artifact bytes, their
// RouteState projections, the terminal execution state and host-call result in
// one transaction. Capture adapters use this rather than publishing bytes
// before completion, eliminating an orphan-payload crash window.
func (c *SQLiteSemanticExecutionCoordinator) CompleteWithArtifactPayloads(a SemanticExecutionAdmission, state PlanExecutionState, result, reasonCode string, payloads []ArtifactPayload, now time.Time) (HostCallRecord, error) {
	refs := make([]ArtifactRef, 0, len(payloads))
	for _, payload := range payloads {
		if err := validateArtifactPayload(payload); err != nil {
			return HostCallRecord{}, err
		}
		refs = append(refs, payload.Ref)
	}
	return c.complete(a, state, result, reasonCode, refs, payloads, now)
}

func (c *SQLiteSemanticExecutionCoordinator) complete(a SemanticExecutionAdmission, state PlanExecutionState, result, reasonCode string, artifacts []ArtifactRef, payloads []ArtifactPayload, now time.Time) (HostCallRecord, error) {
	if c == nil || c.db == nil {
		return HostCallRecord{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := a.validate(); err != nil {
		return HostCallRecord{}, err
	}
	if state != PlanExecutionSucceeded && state != PlanExecutionFailed && state != PlanExecutionUnknown && state != PlanExecutionAwaitingReceipt {
		return HostCallRecord{}, fmt.Errorf("semantic execution terminal state is invalid")
	}
	if err := validateHostCallResult(result); err != nil {
		return HostCallRecord{}, err
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	fingerprint, key := InvocationGrantFingerprint(a.Grant), hostCallKey(a.Identity)
	tx, err := c.db.Begin()
	if err != nil {
		return HostCallRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// The provider I/O happened before this transaction, so this check cannot
	// undo that external observation. It does ensure that a superseded route
	// cannot project its result into execution, completion, artifacts, or the
	// host-call journal after a newer revision owns the lineage.
	if err := routeRevisionIsCurrent(tx, a.Scope); err != nil {
		return HostCallRecord{}, err
	}
	for _, payload := range payloads {
		ref := payload.Ref
		if ref.Scope != a.Scope || ref.ProducerSelection != a.Selection.ID {
			return HostCallRecord{}, fmt.Errorf("route_artifact_producer_contract_mismatch")
		}
		key := artifactStoreKey(ref.Scope, ref.ID)
		var storedBase64 string
		scanErr := tx.QueryRow(`SELECT payload_base64 FROM semantic_artifacts WHERE artifact_key=?`, key).Scan(&storedBase64)
		if errors.Is(scanErr, sql.ErrNoRows) {
			// New payload: enforce the per-principal quota inside this atomic
			// completion transaction and encrypt before persisting. The
			// integrity digest remains computed over plaintext bytes.
			decoded, err := base64.StdEncoding.DecodeString(payload.Base64)
			if err != nil {
				return HostCallRecord{}, fmt.Errorf("artifact content is not valid base64")
			}
			if err := c.Artifacts.enforcePublishQuota(tx, ref.Scope.PrincipalID, int64(len(decoded))); err != nil {
				return HostCallRecord{}, err
			}
			stored, err := c.Artifacts.encodeStoredPayload(key, payload.Base64)
			if err != nil {
				return HostCallRecord{}, err
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_artifacts(artifact_key, root_task_id, plan_id, session_id, turn_id, principal_id, artifact_id, kind, mime_type, integrity_digest, producer_selection, payload_base64, payload_bytes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, key, ref.Scope.RootTaskID, ref.Scope.PlanID, ref.Scope.SessionID, ref.Scope.TurnID, ref.Scope.PrincipalID, ref.ID, ref.Kind, ref.MIMEType, ref.IntegrityDigest, ref.ProducerSelection, stored, len(decoded), artifactStoreTime(ref.CreatedAt)); err != nil {
				return HostCallRecord{}, err
			}
		} else if scanErr != nil {
			return HostCallRecord{}, scanErr
		} else {
			// Idempotent replay compares decoded plaintext: the stored form may
			// be AEAD ciphertext while the caller always supplies plaintext.
			decoded, err := c.Artifacts.decodeStoredPayload(key, storedBase64)
			if err != nil {
				return HostCallRecord{}, err
			}
			if decoded != payload.Base64 {
				return HostCallRecord{}, fmt.Errorf("artifact_conflict")
			}
		}
	}
	allowedExecutionState := "state='running'"
	// PrepareDelivery owns the atomic transition to awaiting_receipt together
	// with the outbox intent. Completing the host-call projection afterwards is
	// therefore an idempotent finalization of that same known state, not a
	// second transition or a second delivery.
	if state == PlanExecutionAwaitingReceipt {
		allowedExecutionState = "state IN ('running','awaiting_receipt')"
	}
	updated, err := tx.Exec(`UPDATE semantic_plan_executions SET state=?, result_digest=?, reason_code=?, updated_at=? WHERE execution_key=? AND `+allowedExecutionState, state, SchemaDigest([]byte(result)), strings.TrimSpace(reasonCode), now.Format(time.RFC3339Nano), planExecutionKey(a.Scope, a.Selection.ID))
	if err != nil {
		return HostCallRecord{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return HostCallRecord{}, fmt.Errorf("selection_execution_not_running")
	}
	if state == PlanExecutionSucceeded {
		purpose := selectionPurposeDigest(a.Selection)
		if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_route_completed_selections(route_key, selection_id, purpose_digest, completed_at) VALUES (?, ?, ?, ?)`, routeStateKey(a.Scope), a.Selection.ID, purpose, now.Format(time.RFC3339Nano)); err != nil {
			return HostCallRecord{}, err
		}
		if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at=? WHERE route_key=?`, now.Format(time.RFC3339Nano), routeStateKey(a.Scope)); err != nil {
			return HostCallRecord{}, err
		}
		for _, ref := range artifacts {
			if ref.Scope != a.Scope || ref.ProducerSelection != a.Selection.ID || !producesArtifact(a.Selection.Produces, ArtifactContract{Kind: ref.Kind, MIMEType: ref.MIMEType, Required: true}) {
				return HostCallRecord{}, fmt.Errorf("route_artifact_producer_contract_mismatch")
			}
			// The route projection may only cite bytes that the same durable
			// execution owner actually holds. A callback cannot fabricate an
			// ArtifactRef merely because it matches the declared contract.
			var stored ArtifactRef
			var created string
			err := tx.QueryRow(`SELECT artifact_id, kind, mime_type, integrity_digest, producer_selection, created_at FROM semantic_artifacts WHERE artifact_key=?`, artifactStoreKey(ref.Scope, ref.ID)).Scan(&stored.ID, &stored.Kind, &stored.MIMEType, &stored.IntegrityDigest, &stored.ProducerSelection, &created)
			if errors.Is(err, sql.ErrNoRows) {
				return HostCallRecord{}, fmt.Errorf("route_artifact_not_published")
			}
			if err != nil {
				return HostCallRecord{}, err
			}
			stored.Scope = ref.Scope
			stored.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			if !sameArtifactIdentity(stored, ref) {
				return HostCallRecord{}, fmt.Errorf("route_artifact_conflict")
			}
			// Preserve the serialized timestamp exactly as the artifact table
			// stores it. RFC3339Nano parsing accepts a shorter fractional form,
			// but formatting a parsed value can add trailing zeroes and make the
			// RouteState equality check reject its own row on reload.
			value := routeArtifactRefFromArtifact(ref, purpose)
			value.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			if err := validateRouteArtifactRef(value); err != nil {
				return HostCallRecord{}, err
			}
			inserted, err := tx.Exec(`INSERT OR IGNORE INTO semantic_route_artifacts(route_key, artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, routeStateKey(a.Scope), value.ArtifactID, value.Kind, value.MIMEType, value.IntegrityDigest, value.ProducerSelection, value.ProducerPurposeDigest, value.SourceScope.RootTaskID, value.SourceScope.PlanID, value.SourceScope.SessionID, value.SourceScope.TurnID, value.SourceScope.PrincipalID, routeStateTime(value.CreatedAt))
			if err != nil {
				return HostCallRecord{}, err
			}
			if n, _ := inserted.RowsAffected(); n == 0 {
				var existing RouteArtifactRef
				var created string
				if err := tx.QueryRow(`SELECT artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at FROM semantic_route_artifacts WHERE route_key=? AND artifact_id=?`, routeStateKey(a.Scope), value.ArtifactID).Scan(&existing.ArtifactID, &existing.Kind, &existing.MIMEType, &existing.IntegrityDigest, &existing.ProducerSelection, &existing.ProducerPurposeDigest, &existing.SourceScope.RootTaskID, &existing.SourceScope.PlanID, &existing.SourceScope.SessionID, &existing.SourceScope.TurnID, &existing.SourceScope.PrincipalID, &created); err != nil {
					return HostCallRecord{}, err
				}
				existing.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
				if existing != value {
					return HostCallRecord{}, fmt.Errorf("route_artifact_conflict")
				}
			}
		}
	}
	// Completion advances only RouteState authority, but it must also emit a
	// rebuildable task-fact snapshot in this same transaction. The projection
	// remains asynchronous and cannot affect this terminal execution result.
	plan, err := coordinatedPublishedPlan(tx, a.Scope)
	if err != nil {
		return HostCallRecord{}, err
	}
	revision, fencingToken, err := continuityRouteRevisionTx(tx, a.Scope)
	if err != nil {
		return HostCallRecord{}, err
	}
	completedIDs, err := continuityCompletedSelectionIDsTx(tx, a.Scope)
	if err != nil {
		return HostCallRecord{}, err
	}
	if err := recordContinuityProjectionTx(c, tx, continuityProjectionExecutionUpdate, a.Scope, revision, fencingToken, plan, completedIDs, now); err != nil {
		return HostCallRecord{}, err
	}
	// An unknown outcome is not a completed host call. Recording it as
	// completed would make the next acquire of this call ID a replay, and a
	// replay reconstructs its verdict from the stored text — so an effect whose
	// result was never observed would come back as a definite one. The
	// uncontrolled journal path already refuses this by writing MarkUnknown
	// instead of Complete; this is the same invariant on the coordinated path.
	// The result text is still stored, because it is the only forensic record
	// of what the provider said before observation was lost.
	hostState := HostCallCompleted
	if state == PlanExecutionUnknown {
		hostState = HostCallUnknown
	}
	updated, err = tx.Exec(`UPDATE semantic_host_calls SET state=?, result=?, result_digest=?, updated_at=? WHERE call_key=? AND grant_fingerprint=? AND request_digest=? AND state='admitted'`, string(hostState), result, SchemaDigest([]byte(result)), now.Format(time.RFC3339Nano), key, fingerprint, a.RequestDigest)
	if err != nil {
		return HostCallRecord{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return HostCallRecord{}, fmt.Errorf("host_call_not_transitionable")
	}
	if err := tx.Commit(); err != nil {
		return HostCallRecord{}, err
	}
	return HostCallRecord{Identity: a.Identity, GrantFingerprint: fingerprint, RequestDigest: a.RequestDigest, State: hostState, Result: result, ResultDigest: SchemaDigest([]byte(result)), UpdatedAt: now}, nil
}

// PrepareStandaloneDelivery creates a fencing-stamped outbox intent for a
// host worker that is not a model-admitted selection (due-time schedule fire).
// It does not touch plan executions. ClaimDelivery / SettleStandaloneDelivery
// remain the only dispatch and settle path.
func (c *SQLiteSemanticExecutionCoordinator) PrepareStandaloneDelivery(record DeliveryRecord, now time.Time) (DeliveryRecord, error) {
	if c == nil || c.db == nil {
		return DeliveryRecord{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if record.ArtifactSourceScope == (InvocationScope{}) {
		record.ArtifactSourceScope = record.Scope
	}
	if err := validateDeliveryRecord(record); err != nil {
		return DeliveryRecord{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	artifact, err := c.Artifacts.byID(record.ArtifactSourceScope, record.ArtifactID)
	if err != nil {
		return DeliveryRecord{}, err
	}
	record.OperationKey = deliveryOperationKey(record, artifact.Ref)
	record.State, record.CreatedAt, record.UpdatedAt = DeliveryPrepared, now.UTC(), now.UTC()
	tx, err := c.db.Begin()
	if err != nil {
		return DeliveryRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	key := deliveryStoreKey(record.Scope, record.SelectionID)
	preparedToken, err := currentLineageFencingToken(tx, routeLineageKey(record.Scope))
	if err != nil {
		return DeliveryRecord{}, err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_delivery_preparations(delivery_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, artifact_id, artifact_source_root_task_id, artifact_source_plan_id, artifact_source_session_id, artifact_source_turn_id, artifact_source_principal_id, channel_scope, destination_id, operation_key, state, prepared_fencing_token, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'prepared', ?, ?, ?)`, key, record.Scope.RootTaskID, record.Scope.PlanID, record.Scope.SessionID, record.Scope.TurnID, record.Scope.PrincipalID, record.SelectionID, record.ArtifactID, record.ArtifactSourceScope.RootTaskID, record.ArtifactSourceScope.PlanID, record.ArtifactSourceScope.SessionID, record.ArtifactSourceScope.TurnID, record.ArtifactSourceScope.PrincipalID, record.ChannelScope, record.DestinationID, record.OperationKey, preparedToken, artifactStoreTime(now), artifactStoreTime(now)); err != nil {
		return DeliveryRecord{}, err
	}
	var storedOperation, state string
	if err := tx.QueryRow(`SELECT operation_key, state FROM semantic_delivery_preparations WHERE delivery_key=?`, key).Scan(&storedOperation, &state); err != nil {
		return DeliveryRecord{}, err
	}
	if storedOperation != record.OperationKey {
		return DeliveryRecord{}, fmt.Errorf("delivery_conflict")
	}
	if err := tx.Commit(); err != nil {
		return DeliveryRecord{}, err
	}
	return c.Artifacts.Delivery(record.Scope, record.SelectionID)
}

// SettleStandaloneDelivery CAS-settles a claimed outbox row without requiring
// a plan execution in awaiting_receipt. Used by host fire workers. A stale
// fencing token returns delivery_fencing_stale and never accepts.
func (c *SQLiteSemanticExecutionCoordinator) SettleStandaloneDelivery(scope InvocationScope, selectionID string, outcome DeliveryState, receiptDigest, reasonCode string, now time.Time) (DeliveryRecord, error) {
	if c == nil || c.db == nil {
		return DeliveryRecord{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := ValidateArtifactScope(scope); err != nil {
		return DeliveryRecord{}, err
	}
	if !validDeliveryOutcome(outcome) {
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_invalid")
	}
	if outcome == DeliveryAccepted && strings.TrimSpace(receiptDigest) == "" {
		return DeliveryRecord{}, fmt.Errorf("delivery_acceptance_receipt_required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_ = reasonCode
	tx, err := c.db.Begin()
	if err != nil {
		return DeliveryRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	key := deliveryStoreKey(scope, selectionID)
	receiptDigest = strings.TrimSpace(receiptDigest)
	var existingState, unknownOrigin string
	var claimToken uint64
	scanErr := tx.QueryRow(`SELECT state, claim_fencing_token, unknown_origin FROM semantic_delivery_preparations WHERE delivery_key=?`, key).Scan(&existingState, &claimToken, &unknownOrigin)
	if errors.Is(scanErr, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return DeliveryRecord{}, err
		}
		return c.Artifacts.Delivery(scope, selectionID)
	}
	if scanErr != nil {
		return DeliveryRecord{}, scanErr
	}
	if existingState == string(outcome) {
		if err := tx.Commit(); err != nil {
			return DeliveryRecord{}, err
		}
		return c.Artifacts.Delivery(scope, selectionID)
	}
	if !deliverySettlementAllowedFrom(existingState, unknownOrigin) {
		if err := tx.Commit(); err != nil {
			return DeliveryRecord{}, err
		}
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_conflict")
	}
	if err := settleDeliveryFencingCheck(tx, routeLineageKey(scope), claimToken); err != nil {
		return DeliveryRecord{}, err
	}
	updated, err := tx.Exec(`UPDATE semantic_delivery_preparations SET state=?, receipt_digest=?, updated_at=? WHERE delivery_key=? AND (state='dispatching' OR (state='unknown' AND unknown_origin=?))`, outcome, receiptDigest, artifactStoreTime(now), key, deliveryUnknownFromLapsedLease)
	if err != nil {
		return DeliveryRecord{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_conflict")
	}
	if err := tx.Commit(); err != nil {
		return DeliveryRecord{}, err
	}
	return c.Artifacts.Delivery(scope, selectionID)
}

// deliveryUnknownFromLapsedLease marks an unknown that was reached because the
// dispatch lease ran out with nobody watching, as opposed to one a fire worker
// wrote as its considered final answer.
const deliveryUnknownFromLapsedLease = "dispatch_lease_expired"

// deliverySettlementAllowedFrom reports whether a delivery in this state can
// still receive an outcome.
//
// A claimed dispatch obviously can. An unknown one depends on how it got
// there, and that is why the origin is stored rather than inferred. When the
// lease lapsed, nobody ever looked, so a receipt arriving afterwards is new
// information and must be recorded. When a fire worker settled unknown itself,
// that was its answer about a channel that issues no receipts; a later
// "acceptance" on that row could not have come from the channel, so accepting
// one would be inventing evidence.
func deliverySettlementAllowedFrom(state, unknownOrigin string) bool {
	if state == string(DeliveryDispatching) {
		return true
	}
	return state == string(DeliveryUnknown) && unknownOrigin == deliveryUnknownFromLapsedLease
}

// ReconcileStaleDeliveryDispatches marks expired dispatching leases unknown.
// It never re-sends.
//
// The selection execution moves with the delivery, in the same transaction.
// Converging only the delivery used to strand the execution at
// awaiting_receipt: the lease had already fired, so nothing would look at that
// dispatch again, and no other reconciler owns the row. The pair has to travel
// together or the selection is left claiming to await an answer that no longer
// has anywhere to come from.
func (c *SQLiteSemanticExecutionCoordinator) ReconcileStaleDeliveryDispatches(now time.Time, maxAge time.Duration) (int, error) {
	if c == nil || c.db == nil || c.Artifacts == nil {
		return 0, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if maxAge <= 0 {
		return 0, fmt.Errorf("delivery dispatch maximum age must be positive")
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	cutoff := artifactStoreTime(now.Add(-maxAge))
	rows, err := tx.Query(`SELECT root_task_id, plan_id, session_id, turn_id, principal_id, selection_id
		FROM semantic_delivery_preparations WHERE state=? AND updated_at<=?`, DeliveryDispatching, cutoff)
	if err != nil {
		return 0, err
	}
	executionKeys := make([]string, 0)
	for rows.Next() {
		var scope InvocationScope
		var selectionID string
		if err := rows.Scan(&scope.RootTaskID, &scope.PlanID, &scope.SessionID, &scope.TurnID, &scope.PrincipalID, &selectionID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		executionKeys = append(executionKeys, planExecutionKey(scope, selectionID))
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	// One execution at a time, by key. External effects park their executions
	// in awaiting_receipt as well, under a different lease; a blanket sweep
	// here would converge operations this reconciler knows nothing about.
	for _, executionKey := range executionKeys {
		if _, err := tx.Exec(`UPDATE semantic_plan_executions SET state='unknown', reason_code='delivery_dispatch_lease_expired', updated_at=?
			WHERE execution_key=? AND state='awaiting_receipt'`, planExecutionTime(now), executionKey); err != nil {
			return 0, err
		}
	}
	updated, err := tx.Exec(`UPDATE semantic_delivery_preparations SET state=?, unknown_origin=?, updated_at=? WHERE state=? AND updated_at<=?`,
		DeliveryUnknown, deliveryUnknownFromLapsedLease, artifactStoreTime(now), DeliveryDispatching, cutoff)
	if err != nil {
		return 0, err
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(changed), nil
}

// PrepareDelivery atomically creates the delivery outbox intent and advances
// the already-running selection to awaiting_receipt. A channel worker must
// subsequently ClaimDelivery; merely preparing an artifact never authorizes a
// gateway response projection to send it.
func (c *SQLiteSemanticExecutionCoordinator) PrepareDelivery(record DeliveryRecord, now time.Time) (DeliveryRecord, error) {
	if c == nil || c.db == nil {
		return DeliveryRecord{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if record.ArtifactSourceScope == (InvocationScope{}) {
		record.ArtifactSourceScope = record.Scope
	}
	if err := validateDeliveryRecord(record); err != nil {
		return DeliveryRecord{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	artifact, err := c.Artifacts.byID(record.ArtifactSourceScope, record.ArtifactID)
	if err != nil {
		return DeliveryRecord{}, err
	}
	record.OperationKey = deliveryOperationKey(record, artifact.Ref)
	record.State, record.CreatedAt, record.UpdatedAt = DeliveryPrepared, now.UTC(), now.UTC()
	tx, err := c.db.Begin()
	if err != nil {
		return DeliveryRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	key := deliveryStoreKey(record.Scope, record.SelectionID)
	// Stamp the prepare-time lineage fencing token. A later route revision
	// fences this intent off before it can ever be claimed for dispatch.
	preparedToken, err := currentLineageFencingToken(tx, routeLineageKey(record.Scope))
	if err != nil {
		return DeliveryRecord{}, err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_delivery_preparations(delivery_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, artifact_id, artifact_source_root_task_id, artifact_source_plan_id, artifact_source_session_id, artifact_source_turn_id, artifact_source_principal_id, channel_scope, destination_id, operation_key, state, prepared_fencing_token, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'prepared', ?, ?, ?)`, key, record.Scope.RootTaskID, record.Scope.PlanID, record.Scope.SessionID, record.Scope.TurnID, record.Scope.PrincipalID, record.SelectionID, record.ArtifactID, record.ArtifactSourceScope.RootTaskID, record.ArtifactSourceScope.PlanID, record.ArtifactSourceScope.SessionID, record.ArtifactSourceScope.TurnID, record.ArtifactSourceScope.PrincipalID, record.ChannelScope, record.DestinationID, record.OperationKey, preparedToken, artifactStoreTime(now), artifactStoreTime(now)); err != nil {
		return DeliveryRecord{}, err
	}
	var storedOperation, state string
	if err := tx.QueryRow(`SELECT operation_key, state FROM semantic_delivery_preparations WHERE delivery_key=?`, key).Scan(&storedOperation, &state); err != nil {
		return DeliveryRecord{}, err
	}
	if storedOperation != record.OperationKey {
		return DeliveryRecord{}, fmt.Errorf("delivery_conflict")
	}
	if state == string(DeliveryPrepared) {
		updated, err := tx.Exec(`UPDATE semantic_plan_executions SET state='awaiting_receipt', result_digest=?, reason_code='channel_delivery_prepared', updated_at=? WHERE execution_key=? AND state='running'`, SchemaDigest([]byte(record.OperationKey)), planExecutionTime(now), planExecutionKey(record.Scope, record.SelectionID))
		if err != nil {
			return DeliveryRecord{}, err
		}
		if n, _ := updated.RowsAffected(); n != 1 {
			return DeliveryRecord{}, fmt.Errorf("selection_execution_not_running")
		}
	}
	if err := tx.Commit(); err != nil {
		return DeliveryRecord{}, err
	}
	return c.Artifacts.Delivery(record.Scope, record.SelectionID)
}

// PrepareDeliveryAndComplete atomically makes a trusted current-channel
// delivery eligible for dispatch and completes its originating host call. It
// is the only path an App-hosted semantic adapter uses for a receipt-bound
// delivery: a crash can therefore leave either no delivery intent at all, or
// one prepared outbox record whose matching host result and execution state
// are already durable. Channel I/O remains strictly after this commit.
func (c *SQLiteSemanticExecutionCoordinator) PrepareDeliveryAndComplete(a SemanticExecutionAdmission, record DeliveryRecord, result, reasonCode string, now time.Time) (DeliveryRecord, HostCallRecord, error) {
	if c == nil || c.db == nil {
		return DeliveryRecord{}, HostCallRecord{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := a.validate(); err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	if err := validateHostCallResult(result); err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	if record.ArtifactSourceScope == (InvocationScope{}) {
		record.ArtifactSourceScope = record.Scope
	}
	if record.Scope != a.Scope || strings.TrimSpace(record.SelectionID) != strings.TrimSpace(a.Selection.ID) {
		return DeliveryRecord{}, HostCallRecord{}, fmt.Errorf("delivery_execution_scope_mismatch")
	}
	if err := validateDeliveryRecord(record); err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	// This is a durable database read, not channel/provider I/O. It verifies
	// that the outbox binds an exact existing payload before we start the
	// transaction that exposes the send intent.
	artifact, err := c.Artifacts.byID(record.ArtifactSourceScope, record.ArtifactID)
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	record.OperationKey = deliveryOperationKey(record, artifact.Ref)
	record.State, record.CreatedAt, record.UpdatedAt = DeliveryPrepared, now, now
	fingerprint, hostKey := InvocationGrantFingerprint(a.Grant), hostCallKey(a.Identity)
	tx, err := c.db.Begin()
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	deliveryKey := deliveryStoreKey(record.Scope, record.SelectionID)
	// Same prepare-time fencing stamp as PrepareDelivery: the outbox intent
	// dies with the revision that authorized it.
	preparedToken, err := currentLineageFencingToken(tx, routeLineageKey(record.Scope))
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_delivery_preparations(delivery_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, artifact_id, artifact_source_root_task_id, artifact_source_plan_id, artifact_source_session_id, artifact_source_turn_id, artifact_source_principal_id, channel_scope, destination_id, operation_key, state, prepared_fencing_token, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'prepared', ?, ?, ?)`, deliveryKey, record.Scope.RootTaskID, record.Scope.PlanID, record.Scope.SessionID, record.Scope.TurnID, record.Scope.PrincipalID, record.SelectionID, record.ArtifactID, record.ArtifactSourceScope.RootTaskID, record.ArtifactSourceScope.PlanID, record.ArtifactSourceScope.SessionID, record.ArtifactSourceScope.TurnID, record.ArtifactSourceScope.PrincipalID, record.ChannelScope, record.DestinationID, record.OperationKey, preparedToken, artifactStoreTime(now), artifactStoreTime(now)); err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	var operationKey string
	if err := tx.QueryRow(`SELECT operation_key FROM semantic_delivery_preparations WHERE delivery_key=?`, deliveryKey).Scan(&operationKey); err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	if operationKey != record.OperationKey {
		return DeliveryRecord{}, HostCallRecord{}, fmt.Errorf("delivery_conflict")
	}
	updated, err := tx.Exec(`UPDATE semantic_plan_executions SET state='awaiting_receipt', result_digest=?, reason_code=?, updated_at=? WHERE execution_key=? AND state='running'`, SchemaDigest([]byte(result)), strings.TrimSpace(reasonCode), planExecutionTime(now), planExecutionKey(a.Scope, a.Selection.ID))
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return DeliveryRecord{}, HostCallRecord{}, fmt.Errorf("selection_execution_not_running")
	}
	updated, err = tx.Exec(`UPDATE semantic_host_calls SET state='completed', result=?, result_digest=?, updated_at=? WHERE call_key=? AND grant_fingerprint=? AND request_digest=? AND state='admitted'`, result, SchemaDigest([]byte(result)), now.Format(time.RFC3339Nano), hostKey, fingerprint, a.RequestDigest)
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return DeliveryRecord{}, HostCallRecord{}, fmt.Errorf("host_call_not_transitionable")
	}
	plan, err := coordinatedPublishedPlan(tx, a.Scope)
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	revision, continuityFencingToken, err := continuityRouteRevisionTx(tx, a.Scope)
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	completedIDs, err := continuityCompletedSelectionIDsTx(tx, a.Scope)
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	if err := recordContinuityProjectionTx(c, tx, continuityProjectionExecutionUpdate, a.Scope, revision, continuityFencingToken, plan, completedIDs, now); err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	stored, err := c.Artifacts.Delivery(record.Scope, record.SelectionID)
	if err != nil {
		return DeliveryRecord{}, HostCallRecord{}, err
	}
	return stored, HostCallRecord{Identity: a.Identity, GrantFingerprint: fingerprint, RequestDigest: a.RequestDigest, State: HostCallCompleted, Result: result, ResultDigest: SchemaDigest([]byte(result)), UpdatedAt: now}, nil
}

// ClaimDelivery is the transactional outbox claim. It intentionally has no
// retry loop: recovery changes a stale dispatch lease to unknown and requires
// receipt reconciliation/manual resolution rather than another send.
//
// The claim is stamped with LocalDispatchHolder. Nothing reads it to decide
// anything -- exclusion is the compare-and-set on state, and staleness is the
// fencing token -- but when a lease lapses and the operation converges to
// unknown, this is the only record of where the send was actually attempted.
// The person who has to go and establish what happened needs somewhere to look.
func (c *SQLiteSemanticExecutionCoordinator) ClaimDelivery(scope InvocationScope, selectionID string, now time.Time) (OutboxClaim, bool, error) {
	return c.ClaimDeliveryWithHolder(scope, selectionID, LocalDispatchHolder(), now)
}

// localDispatchHolder is resolved once: it names a process, and a process does
// not move.
var localDispatchHolder = sync.OnceValue(resolveLocalDispatchHolder)

// LocalDispatchHolder names the process attempting a dispatch, for the benefit
// of whoever later has to work out what became of one.
//
// A deployment that already has a meaningful replica identity should say so
// through MACLAW_DISPATCH_HOLDER; container platforms usually expose one, and
// it will outlive the guess made here. Otherwise this is host and pid, which
// is enough to find the right machine and to tell one process lifetime from
// the next after a restart.
func LocalDispatchHolder() string { return localDispatchHolder() }

func resolveLocalDispatchHolder() string {
	if declared := strings.TrimSpace(os.Getenv("MACLAW_DISPATCH_HOLDER")); declared != "" {
		return declared
	}
	host, _ := os.Hostname()
	if host = strings.TrimSpace(host); host == "" {
		host = "unknown-host"
	}
	return host + ":" + strconv.Itoa(os.Getpid())
}

// ClaimDeliveryWithHolder is ClaimDelivery with an explicit claim-holder
// identity. HolderID is reserved for multi-replica gateway deployments so a
// later reconciler can tell which replica owned the dispatch lease; it is
// never model or adapter supplied on this trusted path. The claim is a
// compare-and-set guarded by the route-lineage fencing token: an intent
// prepared under a superseded revision converges to unknown here and is never
// dispatched.
func (c *SQLiteSemanticExecutionCoordinator) ClaimDeliveryWithHolder(scope InvocationScope, selectionID, holderID string, now time.Time) (OutboxClaim, bool, error) {
	if c == nil || c.db == nil {
		return OutboxClaim{}, false, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := ValidateArtifactScope(scope); err != nil {
		return OutboxClaim{}, false, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return OutboxClaim{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	key := deliveryStoreKey(scope, selectionID)
	var state string
	var preparedToken uint64
	err = tx.QueryRow(`SELECT state, prepared_fencing_token FROM semantic_delivery_preparations WHERE delivery_key=?`, key).Scan(&state, &preparedToken)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return OutboxClaim{}, false, err
		}
		record, err := c.Artifacts.Delivery(scope, selectionID)
		return OutboxClaim{Delivery: record}, false, err
	}
	if err != nil {
		return OutboxClaim{}, false, err
	}
	claimed := false
	var claimToken uint64
	if state == string(DeliveryPrepared) {
		if claimed, claimToken, err = claimDeliveryOutbox(tx, key, routeLineageKey(scope), holderID, preparedToken, artifactStoreTime(now)); err != nil {
			return OutboxClaim{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return OutboxClaim{}, false, err
	}
	record, err := c.Artifacts.Delivery(scope, selectionID)
	if err != nil || !claimed {
		return OutboxClaim{Delivery: record}, false, err
	}
	payload, err := c.Artifacts.byID(record.ArtifactSourceScope, record.ArtifactID)
	if err != nil {
		return OutboxClaim{}, false, err
	}
	return OutboxClaim{Delivery: record, Payload: payload, FencingToken: claimToken, HolderID: strings.TrimSpace(holderID)}, true, nil
}

// SettleDelivery atomically records the channel outcome, settles the plan
// selection, and projects successful acceptance into the route state. It must
// be called only by the channel worker that owns a dispatch claim or a trusted
// receipt reconciler.
func (c *SQLiteSemanticExecutionCoordinator) SettleDelivery(scope InvocationScope, selectionID string, outcome DeliveryState, receiptDigest, reasonCode string, now time.Time) (DeliveryRecord, error) {
	if c == nil || c.db == nil {
		return DeliveryRecord{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if err := ValidateArtifactScope(scope); err != nil {
		return DeliveryRecord{}, err
	}
	if !validDeliveryOutcome(outcome) {
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_invalid")
	}
	if outcome == DeliveryAccepted && strings.TrimSpace(receiptDigest) == "" {
		return DeliveryRecord{}, fmt.Errorf("delivery_acceptance_receipt_required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return DeliveryRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	key := deliveryStoreKey(scope, selectionID)
	receiptDigest = strings.TrimSpace(receiptDigest)
	// Load the claim first so an idempotent replay (already at the requested
	// terminal outcome) returns the stored record without touching fencing,
	// while a real transition is fencing-checked before it consumes the claim.
	var existingState, unknownOrigin string
	var claimToken uint64
	scanErr := tx.QueryRow(`SELECT state, claim_fencing_token, unknown_origin FROM semantic_delivery_preparations WHERE delivery_key=?`, key).Scan(&existingState, &claimToken, &unknownOrigin)
	if errors.Is(scanErr, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return DeliveryRecord{}, err
		}
		return c.Artifacts.Delivery(scope, selectionID)
	}
	if scanErr != nil {
		return DeliveryRecord{}, scanErr
	}
	if existingState == string(outcome) {
		if err := tx.Commit(); err != nil {
			return DeliveryRecord{}, err
		}
		return c.Artifacts.Delivery(scope, selectionID)
	}
	if !deliverySettlementAllowedFrom(existingState, unknownOrigin) {
		if err := tx.Commit(); err != nil {
			return DeliveryRecord{}, err
		}
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_conflict")
	}
	if err := settleDeliveryFencingCheck(tx, routeLineageKey(scope), claimToken); err != nil {
		return DeliveryRecord{}, err
	}
	updated, err := tx.Exec(`UPDATE semantic_delivery_preparations SET state=?, receipt_digest=?, updated_at=? WHERE delivery_key=? AND (state='dispatching' OR (state='unknown' AND unknown_origin=?))`, outcome, receiptDigest, artifactStoreTime(now), key, deliveryUnknownFromLapsedLease)
	if err != nil {
		return DeliveryRecord{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_conflict")
	}
	state := PlanExecutionUnknown
	if outcome == DeliveryAccepted {
		state = PlanExecutionSucceeded
	} else if outcome == DeliveryFailed {
		state = PlanExecutionFailed
	}
	updated, err = tx.Exec(`UPDATE semantic_plan_executions SET state=?, result_digest=?, reason_code=?, updated_at=? WHERE execution_key=? AND state IN ('awaiting_receipt','unknown')`, state, strings.TrimSpace(receiptDigest), strings.TrimSpace(reasonCode), planExecutionTime(now), planExecutionKey(scope, selectionID))
	if err != nil {
		return DeliveryRecord{}, err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return DeliveryRecord{}, fmt.Errorf("selection_execution_not_awaiting_receipt")
	}
	if outcome == DeliveryAccepted {
		plan, err := coordinatedPublishedPlan(tx, scope)
		if err != nil {
			return DeliveryRecord{}, err
		}
		var purpose string
		for _, selection := range plan.Selections {
			if selection.ID == selectionID {
				purpose = selectionPurposeDigest(selection)
				break
			}
		}
		if purpose == "" {
			return DeliveryRecord{}, fmt.Errorf("route_state_selection_not_found")
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_route_completed_selections(route_key, selection_id, purpose_digest, completed_at) VALUES (?, ?, ?, ?)`, routeStateKey(scope), selectionID, purpose, routeStateTime(now)); err != nil {
			return DeliveryRecord{}, err
		}
		if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at=? WHERE route_key=?`, routeStateTime(now), routeStateKey(scope)); err != nil {
			return DeliveryRecord{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return DeliveryRecord{}, err
	}
	return c.Artifacts.Delivery(scope, selectionID)
}

func coordinatedHostCall(tx *sql.Tx, key string) (HostCallRecord, bool, error) {
	var record HostCallRecord
	var created, updated string
	err := tx.QueryRow(`SELECT protocol, connection_id, call_id, surface_epoch, grant_fingerprint, request_digest, state, result, result_digest, created_at, updated_at FROM semantic_host_calls WHERE call_key=?`, key).Scan(&record.Identity.Protocol, &record.Identity.ConnectionID, &record.Identity.CallID, &record.Identity.SurfaceEpoch, &record.GrantFingerprint, &record.RequestDigest, &record.State, &record.Result, &record.ResultDigest, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return HostCallRecord{}, false, nil
	}
	if err != nil {
		return HostCallRecord{}, false, err
	}
	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	record.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return record, true, nil
}

// coordinatedPublishedPlan reads the published route through the transaction
// that is settling a delivery. Calling Routes.PublishedPlan here would acquire
// the coordinator's sole SQLite connection while this transaction already owns
// it, causing successful receipt settlement to deadlock.
func coordinatedPublishedPlan(tx *sql.Tx, scope InvocationScope) (ToolPlan, error) {
	key := routeStateKey(scope)
	var revisionRouteKey, currentRouteKey string
	err := tx.QueryRow(`SELECT rr.route_key, rl.current_route_key FROM semantic_route_revisions rr JOIN semantic_route_lineages rl ON rl.lineage_key = rr.lineage_key WHERE rr.route_key = ?`, key).Scan(&revisionRouteKey, &currentRouteKey)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM semantic_route_states WHERE route_key = ?`, key).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ToolPlan{}, fmt.Errorf("route_state_not_found")
			}
			return ToolPlan{}, err
		}
		if err := tx.QueryRow(`SELECT current_route_key FROM semantic_route_lineages WHERE lineage_key = ?`, routeLineageKey(scope)).Scan(&currentRouteKey); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ToolPlan{}, err
		} else if err == nil {
			return ToolPlan{}, fmt.Errorf("route_revision_superseded")
		}
	} else if err != nil {
		return ToolPlan{}, err
	} else if revisionRouteKey != currentRouteKey {
		return ToolPlan{}, fmt.Errorf("route_revision_superseded")
	}
	var planJSON []byte
	var digest string
	if err := tx.QueryRow(`SELECT plan_json, plan_digest FROM semantic_route_states WHERE route_key = ?`, key).Scan(&planJSON, &digest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ToolPlan{}, fmt.Errorf("route_state_not_found")
		}
		return ToolPlan{}, err
	}
	var plan ToolPlan
	if err := json.Unmarshal(planJSON, &plan); err != nil || plan.ID != scope.PlanID || plan.RootTaskID != scope.RootTaskID {
		return ToolPlan{}, fmt.Errorf("route_state_corrupt")
	}
	if _, actual, err := canonicalRoutePlan(plan); err != nil || actual != digest {
		return ToolPlan{}, fmt.Errorf("route_state_corrupt")
	}
	return cloneRouteStatePlan(plan), nil
}

func coordinatedGrantState(tx *sql.Tx, nonce, fingerprint string, now time.Time) error {
	var stored, expiry, state string
	if err := tx.QueryRow(`SELECT fingerprint, expires_at, state FROM invocation_grants WHERE nonce=?`, nonce).Scan(&stored, &expiry, &state); errors.Is(err, sql.ErrNoRows) || stored != fingerprint {
		return fmt.Errorf("invocation_grant_invalid")
	} else if err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !now.Before(parsed.UTC()) {
		return fmt.Errorf("invocation_grant_expired")
	}
	switch state {
	case "consumed":
		return fmt.Errorf("invocation_grant_replayed")
	case "revoked":
		return fmt.Errorf("invocation_grant_revoked")
	default:
		return fmt.Errorf("invocation_grant_invalid")
	}
}
