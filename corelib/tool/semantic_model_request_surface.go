package tool

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ModelRequestSurfaceState is the lifecycle of one concrete LLM request's
// model-visible aliases. It is deliberately separate from InvocationGrant:
// a retry receives a new request surface but must reuse the same unconsumed
// grant rather than minting fresh authority.
type ModelRequestSurfaceState string

const (
	// modelRequestSurfacePrepared records aliases assembled for an outbound
	// request. It is intentionally not resolvable: definitions must exist
	// before transport starts, but no model response has yet proven that this
	// request was actually sent.
	modelRequestSurfacePrepared   ModelRequestSurfaceState = "prepared"
	modelRequestSurfaceActive     ModelRequestSurfaceState = "active"
	modelRequestSurfaceFinished   ModelRequestSurfaceState = "finished"
	modelRequestSurfaceSuperseded ModelRequestSurfaceState = "superseded"
	modelRequestSurfaceCancelled  ModelRequestSurfaceState = "cancelled"
)

// ModelRequestSurface is a durable proof of the exact aliases sent in one
// model request. Protocol and ConnectionID are host/provider metadata, never
// model arguments. ResponseID is recorded only after the provider returns a
// trusted correlation value.
type ModelRequestSurface struct {
	ID           string
	Scope        InvocationScope
	Protocol     string
	ConnectionID string
	ResponseID   string
	Epoch        string
	State        ModelRequestSurfaceState
	Aliases      map[string]InvocationGrant
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ModelRequestSurfacePublish binds aliases to already-issued, exposed grants.
// It never issues a grant and therefore cannot be used as an authorization
// side door. Aliases must be opaque and unique only within this epoch.
type ModelRequestSurfacePublish struct {
	Scope        InvocationScope
	Protocol     string
	ConnectionID string
	Epoch        string
	Aliases      map[string]InvocationGrant
	Now          time.Time
}

// ModelRequestSurfaceReplace atomically retires an active request surface and
// publishes its retry/fallback successor. The successor must refer to the
// same current route and already-issued grants; this operation never creates
// new authorization.
type ModelRequestSurfaceReplace struct {
	PreviousEpoch string
	Successor     ModelRequestSurfacePublish
}

// ModelRequestSurfaceRecovery identifies a request surface from trusted
// transport metadata after a host process restarts. TenantID is supplied by
// the authenticated host principal; it is never inferred from a model call,
// a route ID, or a provider configuration value.
//
// Recovery intentionally admits only a response-bound, current surface. A
// prepared surface represents an outbound delivery whose result is unknown
// after a restart, while terminal/superseded surfaces must remain dead.
type ModelRequestSurfaceRecovery struct {
	TenantID     string
	Protocol     string
	ConnectionID string
	Epoch        string
}

// initModelRequestSurfaces owns the request presentation tables in the same
// SQLite database as routes, grants and host-call journal. These tables are
// intentionally coordinator-private; a GUI map may cache output but may not
// be the source of execution authority.
func (c *SQLiteSemanticExecutionCoordinator) initModelRequestSurfaces() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	_, err := c.db.Exec(`CREATE TABLE IF NOT EXISTS semantic_model_request_surfaces (
		surface_id TEXT PRIMARY KEY,
		route_key TEXT NOT NULL,
		root_task_id TEXT NOT NULL,
		plan_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		turn_id TEXT NOT NULL,
		principal_id TEXT NOT NULL,
		protocol TEXT NOT NULL,
		connection_id TEXT NOT NULL,
		response_id TEXT NOT NULL DEFAULT '',
		epoch TEXT NOT NULL UNIQUE,
		state TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_semantic_model_request_surfaces_route_state
		ON semantic_model_request_surfaces(route_key, state);
	CREATE TABLE IF NOT EXISTS semantic_model_request_aliases (
		surface_id TEXT NOT NULL,
		alias TEXT NOT NULL,
		grant_nonce TEXT NOT NULL,
		grant_fingerprint TEXT NOT NULL,
		grant_json BLOB NOT NULL,
		PRIMARY KEY(surface_id, alias),
		FOREIGN KEY(surface_id) REFERENCES semantic_model_request_surfaces(surface_id)
	);
	CREATE INDEX IF NOT EXISTS idx_semantic_model_request_aliases_grant
		ON semantic_model_request_aliases(grant_nonce, grant_fingerprint);`)
	return err
}

// PublishModelRequestSurface writes a prepared presentation record before the
// model request is sent. Prepared aliases are deliberately unresolvable; only
// BindModelRequestResponse promotes them to active after a provider response
// proves the request reached a response domain. A caller must retire the
// prepared surface if transport never starts.
func (c *SQLiteSemanticExecutionCoordinator) PublishModelRequestSurface(request ModelRequestSurfacePublish) (ModelRequestSurface, error) {
	if c == nil || c.db == nil {
		return ModelRequestSurface{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	request.Protocol = strings.TrimSpace(request.Protocol)
	request.ConnectionID = strings.TrimSpace(request.ConnectionID)
	request.Epoch = strings.TrimSpace(request.Epoch)
	if err := validateModelRequestSurfaceInput(request); err != nil {
		return ModelRequestSurface{}, err
	}
	now := request.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return ModelRequestSurface{}, err
	}
	defer func() { _ = tx.Rollback() }()
	surface, err := c.publishModelRequestSurfaceTx(tx, request, now)
	if err != nil {
		return ModelRequestSurface{}, err
	}
	if err := tx.Commit(); err != nil {
		return ModelRequestSurface{}, err
	}
	return surface, nil
}

// ReplaceModelRequestSurface is the retry/fallback replacement boundary. It
// prevents a late predecessor response from resolving an alias between a
// host retiring the old request and publishing the successor. Unlike route
// cancellation it deliberately preserves an issued grant, so a retry may use
// the same one-shot authority exactly once.
func (c *SQLiteSemanticExecutionCoordinator) ReplaceModelRequestSurface(request ModelRequestSurfaceReplace) (ModelRequestSurface, error) {
	if c == nil || c.db == nil {
		return ModelRequestSurface{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	request.PreviousEpoch = strings.TrimSpace(request.PreviousEpoch)
	request.Successor.Protocol = strings.TrimSpace(request.Successor.Protocol)
	request.Successor.ConnectionID = strings.TrimSpace(request.Successor.ConnectionID)
	request.Successor.Epoch = strings.TrimSpace(request.Successor.Epoch)
	if request.PreviousEpoch == "" || request.PreviousEpoch == request.Successor.Epoch {
		return ModelRequestSurface{}, fmt.Errorf("model_request_surface_replace_invalid")
	}
	if err := validateModelRequestSurfaceInput(request.Successor); err != nil {
		return ModelRequestSurface{}, err
	}
	now := request.Successor.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return ModelRequestSurface{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var priorRouteKey string
	err = tx.QueryRow(`SELECT route_key FROM semantic_model_request_surfaces WHERE epoch=? AND state IN ('prepared','active')`, request.PreviousEpoch).Scan(&priorRouteKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return ModelRequestSurface{}, fmt.Errorf("model_request_surface_not_active")
		}
		return ModelRequestSurface{}, err
	}
	if priorRouteKey != routeStateKey(request.Successor.Scope) {
		return ModelRequestSurface{}, fmt.Errorf("model_request_surface_scope_mismatch")
	}
	if err := routeRevisionIsCurrent(tx, request.Successor.Scope); err != nil {
		return ModelRequestSurface{}, err
	}
	if _, err := tx.Exec(`UPDATE semantic_model_request_surfaces SET state='superseded', updated_at=? WHERE epoch=? AND state IN ('prepared','active')`, routeStateTime(now), request.PreviousEpoch); err != nil {
		return ModelRequestSurface{}, err
	}
	surface, err := c.publishModelRequestSurfaceTx(tx, request.Successor, now)
	if err != nil {
		return ModelRequestSurface{}, err
	}
	if err := tx.Commit(); err != nil {
		return ModelRequestSurface{}, err
	}
	return surface, nil
}

func (c *SQLiteSemanticExecutionCoordinator) publishModelRequestSurfaceTx(tx *sql.Tx, request ModelRequestSurfacePublish, now time.Time) (ModelRequestSurface, error) {
	if tx == nil {
		return ModelRequestSurface{}, fmt.Errorf("semantic execution transaction is unavailable")
	}
	if err := routeRevisionIsCurrent(tx, request.Scope); err != nil {
		return ModelRequestSurface{}, err
	}
	if err := ensureModelRequestAliasesMaterializedTx(tx, request.Scope, request.Aliases, now); err != nil {
		return ModelRequestSurface{}, err
	}
	surfaceID, err := newModelRequestSurfaceID()
	if err != nil {
		return ModelRequestSurface{}, err
	}
	if _, err := tx.Exec(`INSERT INTO semantic_model_request_surfaces(surface_id, route_key, root_task_id, plan_id, session_id, turn_id, principal_id, protocol, connection_id, epoch, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'prepared', ?, ?)`, surfaceID, routeStateKey(request.Scope), request.Scope.RootTaskID, request.Scope.PlanID, request.Scope.SessionID, request.Scope.TurnID, request.Scope.PrincipalID, request.Protocol, request.ConnectionID, request.Epoch, routeStateTime(now), routeStateTime(now)); err != nil {
		return ModelRequestSurface{}, err
	}
	aliases := sortedModelRequestAliases(request.Aliases)
	for _, alias := range aliases {
		grant := request.Aliases[alias]
		encoded, err := json.Marshal(grant)
		if err != nil {
			return ModelRequestSurface{}, err
		}
		if _, err := tx.Exec(`INSERT INTO semantic_model_request_aliases(surface_id, alias, grant_nonce, grant_fingerprint, grant_json) VALUES (?, ?, ?, ?, ?)`, surfaceID, alias, grant.Nonce, InvocationGrantFingerprint(grant), encoded); err != nil {
			return ModelRequestSurface{}, err
		}
	}
	return ModelRequestSurface{ID: surfaceID, Scope: request.Scope, Protocol: request.Protocol, ConnectionID: request.ConnectionID, Epoch: request.Epoch, State: modelRequestSurfacePrepared, Aliases: cloneModelRequestAliases(request.Aliases), CreatedAt: now, UpdatedAt: now}, nil
}

// BindModelRequestResponse records the provider-supplied response correlation.
// It is intentionally mandatory before alias resolution; a provider that
// cannot supply it cannot safely execute Coding dynamic aliases.
func (c *SQLiteSemanticExecutionCoordinator) BindModelRequestResponse(epoch, protocol, connectionID, responseID string, now time.Time) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	epoch, protocol, connectionID, responseID = strings.TrimSpace(epoch), strings.TrimSpace(protocol), strings.TrimSpace(connectionID), strings.TrimSpace(responseID)
	if epoch == "" || protocol == "" || connectionID == "" || responseID == "" {
		return fmt.Errorf("model_response_correlation_required")
	}
	if now = now.UTC(); now.IsZero() {
		now = time.Now().UTC()
	}
	// A provider/parser may deliver the same response metadata twice while a
	// stream reconnects. Accept only the identical durable binding on an active
	// record; a different response ID is never allowed to replace it.
	result, err := c.db.Exec(`UPDATE semantic_model_request_surfaces SET response_id=?, state='active', updated_at=? WHERE epoch=? AND protocol=? AND connection_id=? AND ((state='prepared' AND response_id='') OR (state='active' AND response_id=?))`, responseID, routeStateTime(now), epoch, protocol, connectionID, responseID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("model_request_surface_not_active")
	}
	return nil
}

// ResolveModelRequestAlias is the only durable alias lookup. It validates the
// request/response correlation and current route before returning a fixed
// grant. The caller must still Validate and Admit the grant in its execution
// transaction; this lookup never consumes it.
func (c *SQLiteSemanticExecutionCoordinator) ResolveModelRequestAlias(epoch, protocol, connectionID, responseID, alias string) (InvocationGrant, InvocationScope, error) {
	if c == nil || c.db == nil {
		return InvocationGrant{}, InvocationScope{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	epoch, protocol, connectionID, responseID, alias = strings.TrimSpace(epoch), strings.TrimSpace(protocol), strings.TrimSpace(connectionID), strings.TrimSpace(responseID), strings.TrimSpace(alias)
	if epoch == "" || protocol == "" || connectionID == "" || responseID == "" || alias == "" {
		return InvocationGrant{}, InvocationScope{}, fmt.Errorf("stale_surface")
	}
	tx, err := c.db.Begin()
	if err != nil {
		return InvocationGrant{}, InvocationScope{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var surfaceID, state, storedResponse string
	var scope InvocationScope
	err = tx.QueryRow(`SELECT surface_id, root_task_id, plan_id, session_id, turn_id, principal_id, state, response_id FROM semantic_model_request_surfaces WHERE epoch=? AND protocol=? AND connection_id=?`, epoch, protocol, connectionID).Scan(&surfaceID, &scope.RootTaskID, &scope.PlanID, &scope.SessionID, &scope.TurnID, &scope.PrincipalID, &state, &storedResponse)
	if err != nil {
		if err == sql.ErrNoRows {
			return InvocationGrant{}, InvocationScope{}, fmt.Errorf("stale_surface")
		}
		return InvocationGrant{}, InvocationScope{}, err
	}
	if state != string(modelRequestSurfaceActive) || storedResponse != responseID {
		return InvocationGrant{}, InvocationScope{}, fmt.Errorf("stale_surface")
	}
	if err := routeRevisionIsCurrent(tx, scope); err != nil {
		return InvocationGrant{}, InvocationScope{}, fmt.Errorf("stale_surface")
	}
	var encoded []byte
	if err := tx.QueryRow(`SELECT grant_json FROM semantic_model_request_aliases WHERE surface_id=? AND alias=?`, surfaceID, alias).Scan(&encoded); err != nil {
		if err == sql.ErrNoRows {
			return InvocationGrant{}, InvocationScope{}, fmt.Errorf("stale_surface")
		}
		return InvocationGrant{}, InvocationScope{}, err
	}
	grant, err := unmarshalModelRequestGrant(encoded)
	if err != nil || grant.Scope != scope {
		return InvocationGrant{}, InvocationScope{}, fmt.Errorf("stale_surface")
	}
	if err := tx.Commit(); err != nil {
		return InvocationGrant{}, InvocationScope{}, err
	}
	return grant, scope, nil
}

// RecoverBoundModelRequestSurface reconstructs only the durable correlation
// state needed to accept a late, already-bound provider tool call after host
// restart. It deliberately does not rebuild model definitions or dispatch
// bindings: those are presentation/runtime caches and must never become a
// recovery source of provider authority.
//
// Alias execution still goes through ResolveModelRequestAlias, Validate and
// Admit. Returning aliases here supports audit/tests only; callers must not
// use this result as an in-memory name dispatcher.
func (c *SQLiteSemanticExecutionCoordinator) RecoverBoundModelRequestSurface(request ModelRequestSurfaceRecovery) (ModelRequestSurface, error) {
	if c == nil || c.db == nil {
		return ModelRequestSurface{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	request.TenantID = strings.TrimSpace(request.TenantID)
	request.Protocol = strings.TrimSpace(request.Protocol)
	request.ConnectionID = strings.TrimSpace(request.ConnectionID)
	request.Epoch = strings.TrimSpace(request.Epoch)
	if request.TenantID == "" || request.Protocol == "" || request.ConnectionID == "" || request.Epoch == "" {
		return ModelRequestSurface{}, fmt.Errorf("stale_surface")
	}
	tx, err := c.db.Begin()
	if err != nil {
		return ModelRequestSurface{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var surface ModelRequestSurface
	var state, createdAt, updatedAt, tenantID string
	err = tx.QueryRow(`SELECT s.surface_id, s.root_task_id, s.plan_id, s.session_id, s.turn_id, s.principal_id,
		s.protocol, s.connection_id, s.response_id, s.epoch, s.state, s.created_at, s.updated_at, rs.tenant_id
		FROM semantic_model_request_surfaces s
		JOIN semantic_route_states rs ON rs.route_key=s.route_key
		WHERE s.epoch=? AND s.protocol=? AND s.connection_id=?`, request.Epoch, request.Protocol, request.ConnectionID).Scan(
		&surface.ID, &surface.Scope.RootTaskID, &surface.Scope.PlanID, &surface.Scope.SessionID, &surface.Scope.TurnID, &surface.Scope.PrincipalID,
		&surface.Protocol, &surface.ConnectionID, &surface.ResponseID, &surface.Epoch, &state, &createdAt, &updatedAt, &tenantID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return ModelRequestSurface{}, fmt.Errorf("stale_surface")
		}
		return ModelRequestSurface{}, err
	}
	if tenantID != request.TenantID || state != string(modelRequestSurfaceActive) || strings.TrimSpace(surface.ResponseID) == "" {
		return ModelRequestSurface{}, fmt.Errorf("stale_surface")
	}
	if surface.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return ModelRequestSurface{}, fmt.Errorf("stale_surface")
	}
	if surface.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return ModelRequestSurface{}, fmt.Errorf("stale_surface")
	}
	surface.State = modelRequestSurfaceActive
	if err := routeRevisionIsCurrent(tx, surface.Scope); err != nil {
		return ModelRequestSurface{}, fmt.Errorf("stale_surface")
	}
	rows, err := tx.Query(`SELECT alias, grant_json FROM semantic_model_request_aliases WHERE surface_id=? ORDER BY alias`, surface.ID)
	if err != nil {
		return ModelRequestSurface{}, err
	}
	defer rows.Close()
	surface.Aliases = make(map[string]InvocationGrant)
	for rows.Next() {
		var alias string
		var encoded []byte
		if err := rows.Scan(&alias, &encoded); err != nil {
			return ModelRequestSurface{}, err
		}
		grant, err := unmarshalModelRequestGrant(encoded)
		if err != nil || strings.TrimSpace(alias) == "" || grant.Scope != surface.Scope {
			return ModelRequestSurface{}, fmt.Errorf("stale_surface")
		}
		surface.Aliases[alias] = grant
	}
	if err := rows.Err(); err != nil {
		return ModelRequestSurface{}, err
	}
	if len(surface.Aliases) == 0 {
		return ModelRequestSurface{}, fmt.Errorf("stale_surface")
	}
	if err := tx.Commit(); err != nil {
		return ModelRequestSurface{}, err
	}
	return surface, nil
}

// RetireModelRequestSurface atomically makes all aliases in one request
// unresolvable for a non-success terminal outcome. It does not revoke grants:
// retry/fallback on the same current revision must be able to reuse a
// still-issued grant through its successor request surface. A successful
// response must use FinishModelRequestSurface instead; allowing arbitrary
// callers to write `finished` would let a prepared or superseded presentation
// impersonate a durably settled response.
func (c *SQLiteSemanticExecutionCoordinator) RetireModelRequestSurface(epoch string, state ModelRequestSurfaceState, now time.Time) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if state != modelRequestSurfaceSuperseded && state != modelRequestSurfaceCancelled {
		return fmt.Errorf("model_request_surface_retire_state_invalid")
	}
	epoch = strings.TrimSpace(epoch)
	if epoch == "" {
		return fmt.Errorf("model_request_surface_epoch_required")
	}
	if now = now.UTC(); now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := c.db.Exec(`UPDATE semantic_model_request_surfaces SET state=?, updated_at=? WHERE epoch=? AND state IN ('prepared','active')`, string(state), routeStateTime(now), epoch)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed > 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("model_request_surface_conflict")
	}
	return nil
}

// FinishModelRequestSurface closes one concrete request presentation after its
// response has been durably accepted by the host. Unlike CancelRouteSurface,
// it does not cancel the route revision or revoke still-issued grants: a
// completed tool batch may legitimately materialize its successor surface on
// that same current revision. The finished request's aliases are nevertheless
// permanently unresolvable, so no later provider callback can execute against
// the predecessor response.
func (c *SQLiteSemanticExecutionCoordinator) FinishModelRequestSurface(epoch string, now time.Time) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	epoch = strings.TrimSpace(epoch)
	if epoch == "" {
		return fmt.Errorf("model_request_surface_epoch_required")
	}
	if now = now.UTC(); now.IsZero() {
		now = time.Now().UTC()
	}
	// A settled disposition is stronger than a generic best-effort retirement:
	// the response must have been bound and still be active.  Treating a
	// concurrently cancelled/superseded presentation as a successful finish
	// would make the terminal audit fact disagree with durable authority.
	result, err := c.db.Exec(`UPDATE semantic_model_request_surfaces SET state='finished', updated_at=? WHERE epoch=? AND state='active'`, routeStateTime(now), epoch)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	if changed != 0 {
		return fmt.Errorf("model_request_surface_conflict")
	}
	var state string
	if err := c.db.QueryRow(`SELECT state FROM semantic_model_request_surfaces WHERE epoch=?`, epoch).Scan(&state); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("model_request_surface_not_active")
		}
		return err
	}
	if state == string(modelRequestSurfaceFinished) {
		// The loop guarantees one disposition. Retaining idempotence here makes
		// a duplicate delivery harmless without allowing any different terminal
		// state to masquerade as a settled response.
		return nil
	}
	return fmt.Errorf("model_request_surface_not_active")
}

// CancelRouteSurface is the terminal cancellation boundary for a current
// route revision. It deliberately owns request-surface retirement,
// materialization retirement, and revocation of still-issued grants in one
// coordinator transaction. Calling the three stores independently would leave
// a window in which a late model response could resolve an alias or spend a
// grant after the host has cancelled the task.
//
// Cancellation is terminal for this revision: a later attempt must publish a
// child revision through PublishSurface. It must not rematerialize grants on
// the cancelled route.
func (c *SQLiteSemanticExecutionCoordinator) CancelRouteSurface(scope InvocationScope, now time.Time) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	if strings.TrimSpace(scope.RootTaskID) == "" || strings.TrimSpace(scope.PlanID) == "" || strings.TrimSpace(scope.SessionID) == "" || strings.TrimSpace(scope.TurnID) == "" || strings.TrimSpace(scope.PrincipalID) == "" {
		return fmt.Errorf("model_request_surface_scope_required")
	}
	if now = now.UTC(); now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := routeRevisionIsCurrent(tx, scope); err != nil {
		return err
	}
	if err := cancelRouteSurfaceTx(tx, routeStateKey(scope), now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func validateModelRequestSurfaceInput(request ModelRequestSurfacePublish) error {
	if strings.TrimSpace(request.Scope.RootTaskID) == "" || strings.TrimSpace(request.Scope.PlanID) == "" || strings.TrimSpace(request.Scope.SessionID) == "" || strings.TrimSpace(request.Scope.TurnID) == "" || strings.TrimSpace(request.Scope.PrincipalID) == "" {
		return fmt.Errorf("model_request_surface_scope_required")
	}
	if request.Protocol == "" || request.ConnectionID == "" || request.Epoch == "" {
		return fmt.Errorf("model_request_surface_correlation_required")
	}
	if len(request.Aliases) == 0 {
		return fmt.Errorf("model_request_surface_aliases_required")
	}
	for alias, grant := range request.Aliases {
		if strings.TrimSpace(alias) == "" || grant.Scope != request.Scope || strings.TrimSpace(grant.Nonce) == "" {
			return fmt.Errorf("model_request_surface_alias_invalid")
		}
	}
	return nil
}

func ensureModelRequestAliasesMaterializedTx(tx *sql.Tx, scope InvocationScope, aliases map[string]InvocationGrant, now time.Time) error {
	routeKey := routeStateKey(scope)
	for _, alias := range sortedModelRequestAliases(aliases) {
		grant := aliases[alias]
		fingerprint := InvocationGrantFingerprint(grant)
		var materialized, state string
		err := tx.QueryRow(`SELECT m.grant_json, m.state FROM semantic_route_materializations m WHERE m.route_key=? AND m.state='exposed' AND m.grant_json IS NOT NULL AND json_extract(m.grant_json, '$.Nonce')=?`, routeKey, grant.Nonce).Scan(&materialized, &state)
		if err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("model_request_surface_grant_not_exposed")
			}
			return err
		}
		stored, err := unmarshalModelRequestGrant([]byte(materialized))
		if err != nil || InvocationGrantFingerprint(stored) != fingerprint || stored.Scope != scope {
			return fmt.Errorf("model_request_surface_grant_mismatch")
		}
		var grantState string
		if err := tx.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=? AND fingerprint=? AND expires_at>?`, grant.Nonce, fingerprint, routeStateTime(now)).Scan(&grantState); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("model_request_surface_grant_unavailable")
			}
			return err
		}
		if grantState != "issued" {
			return fmt.Errorf("model_request_surface_grant_unavailable")
		}
	}
	return nil
}

func retireModelRequestSurfacesForRouteTx(tx *sql.Tx, routeKey string, state ModelRequestSurfaceState, now time.Time) error {
	if state != modelRequestSurfaceSuperseded && state != modelRequestSurfaceCancelled {
		return fmt.Errorf("model_request_surface_retire_state_invalid")
	}
	_, err := tx.Exec(`UPDATE semantic_model_request_surfaces SET state=?, updated_at=? WHERE route_key=? AND state IN ('prepared','active')`, string(state), routeStateTime(now), routeKey)
	return err
}

// cancelRouteSurfaceTx is shared by explicit cancellation and supersession.
// Its ordering is intentional: first make request aliases unresolvable, then
// retire their route materializations, then revoke every still-issued grant
// referenced by those materializations. All three writes commit together.
func cancelRouteSurfaceTx(tx *sql.Tx, routeKey string, now time.Time) error {
	return retireRouteSurfaceAuthorityTx(tx, routeKey, modelRequestSurfaceCancelled, now)
}

// supersedeRouteSurfaceTx is the child-revision counterpart of cancellation.
// It keeps a distinct terminal request-surface state for auditability while
// applying the same atomic authority retirement.
func supersedeRouteSurfaceTx(tx *sql.Tx, routeKey string, now time.Time) error {
	return retireRouteSurfaceAuthorityTx(tx, routeKey, modelRequestSurfaceSuperseded, now)
}

func retireRouteSurfaceAuthorityTx(tx *sql.Tx, routeKey string, state ModelRequestSurfaceState, now time.Time) error {
	if tx == nil || strings.TrimSpace(routeKey) == "" {
		return fmt.Errorf("model_request_surface_route_required")
	}
	if state != modelRequestSurfaceCancelled && state != modelRequestSurfaceSuperseded {
		return fmt.Errorf("model_request_surface_retire_state_invalid")
	}
	if err := retireModelRequestSurfacesForRouteTx(tx, routeKey, state, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE semantic_route_materializations SET state='retired', updated_at=? WHERE route_key=? AND state='exposed'`, routeStateTime(now), routeKey); err != nil {
		return err
	}
	if err := revokeExposedRouteGrantsTx(tx, routeKey); err != nil {
		return err
	}
	if state == modelRequestSurfaceCancelled {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_route_cancellations(route_key, cancelled_at) VALUES (?, ?)`, routeKey, routeStateTime(now)); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`UPDATE semantic_route_states SET updated_at=? WHERE route_key=?`, routeStateTime(now), routeKey)
	return err
}

func revokeExposedRouteGrantsTx(tx *sql.Tx, routeKey string) error {
	rows, err := tx.Query(`SELECT grant_json FROM semantic_route_materializations WHERE route_key=? AND state='retired'`, routeKey)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			return err
		}
		grant, err := unmarshalModelRequestGrant(encoded)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE invocation_grants SET state='revoked' WHERE nonce=? AND fingerprint=? AND state='issued'`, grant.Nonce, InvocationGrantFingerprint(grant)); err != nil {
			return err
		}
	}
	return rows.Err()
}

func sortedModelRequestAliases(aliases map[string]InvocationGrant) []string {
	keys := make([]string, 0, len(aliases))
	for alias := range aliases {
		keys = append(keys, alias)
	}
	sort.Strings(keys)
	return keys
}

func cloneModelRequestAliases(in map[string]InvocationGrant) map[string]InvocationGrant {
	out := make(map[string]InvocationGrant, len(in))
	for alias, grant := range in {
		out[alias] = grant
	}
	return out
}

func newModelRequestSurfaceID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "model-request:" + hex.EncodeToString(value[:]), nil
}

func unmarshalModelRequestGrant(encoded []byte) (InvocationGrant, error) {
	var grant InvocationGrant
	if err := json.Unmarshal(encoded, &grant); err != nil {
		return InvocationGrant{}, err
	}
	return grant, nil
}
