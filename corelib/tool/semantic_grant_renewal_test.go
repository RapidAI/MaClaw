package tool

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func renewalFixture(t *testing.T) (ToolPlan, InvocationScope) {
	t.Helper()
	registry := semanticRegistry(t)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "renew-root", SessionID: "renew-session", TurnID: "renew-turn",
		Snapshot: semanticSnapshot(t, registry, []ProviderSpec{
			semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
		}),
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	scope := InvocationScope{
		RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "renew-session", TurnID: "renew-turn",
		PrincipalID: "renew-principal", ToolSnapshotID: "toolsnap:renew",
	}
	return plan, scope
}

func renewalIssuer(t *testing.T, store InvocationGrantStore, now func() time.Time) *InvocationIssuer {
	t.Helper()
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("r", 32)), store)
	if err != nil {
		t.Fatal(err)
	}
	issuer.now = now
	return issuer
}

func TestRenewExpiredReplacesUnconsumedGrant(t *testing.T) {
	for _, name := range []string{"memory", "sqlite"} {
		t.Run(name, func(t *testing.T) {
			var store InvocationGrantStore
			if name == "sqlite" {
				sqlite, err := NewSQLiteInvocationGrantStore(filepath.Join(t.TempDir(), "grants.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer sqlite.Close()
				store = sqlite
			} else {
				store = NewMemoryInvocationGrantStore()
			}
			plan, scope := renewalFixture(t)
			issuedAt := time.Date(2026, 10, 4, 8, 22, 33, 0, time.UTC)
			clock := issuedAt
			issuer := renewalIssuer(t, store, func() time.Time { return clock })
			grants, err := issuer.Issue(plan, scope, DefaultInvocationGrantTTL)
			if err != nil || len(grants) != 1 {
				t.Fatalf("issue grants=%+v err=%v", grants, err)
			}
			grant := grants[0]
			if _, err := issuer.RenewExpired(grant, scope, plan, DefaultInvocationGrantTTL, issuedAt.Add(DefaultInvocationGrantTTL-time.Second), nil); err == nil || err.Error() != "invocation_grant_not_expired" {
				t.Fatalf("still-valid renewal err=%v", err)
			}
			other := scope
			other.TurnID = "other-turn"
			if _, err := issuer.RenewExpired(grant, other, plan, DefaultInvocationGrantTTL, issuedAt.Add(DefaultInvocationGrantTTL), nil); err == nil || err.Error() != "invocation_grant_scope_mismatch" {
				t.Fatalf("other-turn renewal err=%v", err)
			}
			expiredAt := issuedAt.Add(DefaultInvocationGrantTTL)
			clock = expiredAt
			successor, err := issuer.RenewExpired(grant, scope, plan, DefaultInvocationGrantTTL, expiredAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			if successor.Nonce == grant.Nonce || successor.Token == grant.Token || !successor.ExpiresAt.Equal(expiredAt.Add(DefaultInvocationGrantTTL)) {
				t.Fatalf("successor=%+v", successor)
			}
			if _, err := issuer.Validate(successor, scope, plan); err != nil {
				t.Fatalf("successor validate: %v", err)
			}
			tampered := successor
			tampered.ExpiresAt = tampered.ExpiresAt.Add(time.Second)
			if _, err := issuer.Validate(tampered, scope, plan); err == nil || err.Error() != "invocation_grant_invalid" {
				t.Fatalf("tampered expiry err=%v", err)
			}
			if _, err := issuer.RenewExpired(grant, scope, plan, DefaultInvocationGrantTTL, expiredAt, nil); err == nil || err.Error() != "invocation_grant_revoked" {
				t.Fatalf("second renewal of spent clock err=%v", err)
			}
			if _, err := issuer.ValidateAndConsume(successor, scope, plan); err != nil {
				t.Fatalf("consume successor: %v", err)
			}
			name := RenderedSemanticFunctionName(grant.AdapterName, grant.Token)
			live := map[string]InvocationGrant{name: grant}
			if err := InstallRenewedGrant(live, name, grant, successor); err != nil {
				t.Fatal(err)
			}
			rendered := RenderedSemanticFunctionName(successor.AdapterName, successor.Token)
			if live[rendered].Nonce != successor.Nonce {
				t.Fatalf("installed=%+v", live)
			}
			if name != rendered {
				if _, still := live[name]; still {
					t.Fatal("previous token name remained live")
				}
			}
			occupied := map[string]InvocationGrant{name: grant, rendered: {Nonce: "other", Token: successor.Token}}
			if err := InstallRenewedGrant(occupied, name, grant, successor); err == nil {
				t.Fatal("collision was installed")
			}
			if occupied[name].Nonce != grant.Nonce {
				t.Fatal("collision replaced the live grant")
			}
			if err := InstallRenewedGrant(map[string]InvocationGrant{}, name, grant, successor); err == nil || err.Error() != "invocation_grant_not_live" {
				t.Fatalf("retired grant install err=%v", err)
			}
		})
	}
}

func TestRenewExpiredDoesNotRearmConsumedOrRevokedGrant(t *testing.T) {
	plan, scope := renewalFixture(t)
	issuedAt := time.Date(2026, 10, 4, 8, 22, 33, 0, time.UTC)
	clock := issuedAt
	issuer := renewalIssuer(t, NewMemoryInvocationGrantStore(), func() time.Time { return clock })
	grants, err := issuer.Issue(plan, scope, DefaultInvocationGrantTTL)
	if err != nil || len(grants) != 1 {
		t.Fatal(err)
	}
	if _, err := issuer.ValidateAndConsume(grants[0], scope, plan); err != nil {
		t.Fatal(err)
	}
	clock = issuedAt.Add(DefaultInvocationGrantTTL)
	if _, err := issuer.RenewExpired(grants[0], scope, plan, DefaultInvocationGrantTTL, clock, nil); err == nil || err.Error() != "invocation_grant_replayed" {
		t.Fatalf("consumed renewal err=%v", err)
	}

	clock = issuedAt
	revokedIssuer := renewalIssuer(t, NewMemoryInvocationGrantStore(), func() time.Time { return clock })
	revoked, err := revokedIssuer.Issue(plan, scope, DefaultInvocationGrantTTL)
	if err != nil || len(revoked) != 1 {
		t.Fatal(err)
	}
	if err := revokedIssuer.RevokeWithError(revoked[0]); err != nil {
		t.Fatal(err)
	}
	clock = issuedAt.Add(DefaultInvocationGrantTTL)
	if _, err := revokedIssuer.RenewExpired(revoked[0], scope, plan, DefaultInvocationGrantTTL, clock, nil); err == nil || err.Error() != "invocation_grant_revoked" {
		t.Fatalf("revoked renewal err=%v", err)
	}
}

func TestCoordinatorRenewsExpiredUnconsumedGrantInOneTransaction(t *testing.T) {
	plan, scope := renewalFixture(t)
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "renew.db"), WithCoordinatorContinuityTenant("tenant-renew"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	issuedAt := time.Date(2026, 10, 4, 8, 22, 33, 0, time.UTC)
	clock := issuedAt
	issuer := renewalIssuer(t, coordinator.Grants, func() time.Time { return clock })
	_, grants, err := coordinator.PublishSurface(SurfacePublishRequest{
		Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest},
		TenantID: "tenant-renew", Issuer: issuer, GrantTTL: DefaultInvocationGrantTTL, Now: issuedAt,
	})
	if err != nil || len(grants) != 1 {
		t.Fatalf("publish grants=%+v err=%v", grants, err)
	}
	grant := grants[0]
	if _, err := coordinator.PublishModelRequestSurface(ModelRequestSurfacePublish{
		Scope: scope, Protocol: "agent-loop/v1", ConnectionID: "connection-renew", Epoch: "epoch-renew",
		Aliases: map[string]InvocationGrant{"bash": grant}, Now: issuedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.CommitExpiredRenewal(issuer, scope, grant, DefaultInvocationGrantTTL, issuedAt.Add(time.Minute), nil); err == nil || err.Error() != "invocation_grant_not_expired" {
		t.Fatalf("still-valid renewal err=%v", err)
	}
	other := scope
	other.TurnID = "other-turn"
	if _, err := coordinator.CommitExpiredRenewal(issuer, other, grant, DefaultInvocationGrantTTL, issuedAt.Add(DefaultInvocationGrantTTL), nil); err == nil {
		t.Fatal("other turn renewed a grant")
	}
	var state string
	if err := coordinator.db.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=?`, grant.Nonce).Scan(&state); err != nil || state != "issued" {
		t.Fatalf("state=%q err=%v", state, err)
	}
	expiredAt := issuedAt.Add(DefaultInvocationGrantTTL)
	clock = expiredAt
	successor, err := coordinator.CommitExpiredRenewal(issuer, scope, grant, DefaultInvocationGrantTTL, expiredAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if successor.Nonce == grant.Nonce || !successor.ExpiresAt.Equal(expiredAt.Add(DefaultInvocationGrantTTL)) {
		t.Fatalf("successor=%+v", successor)
	}
	if _, err := issuer.ValidateWithCanonicalScope(successor, scope, plan); err != nil {
		t.Fatalf("successor validate: %v", err)
	}
	tampered := successor
	tampered.ExpiresAt = tampered.ExpiresAt.Add(time.Nanosecond)
	if _, err := issuer.ValidateWithCanonicalScope(tampered, scope, plan); err == nil || err.Error() != "invocation_grant_invalid" {
		t.Fatalf("tampered expiry err=%v", err)
	}
	rows, err := coordinator.db.Query(`SELECT function_name, state FROM semantic_route_materializations WHERE route_key=?`, routeStateKey(scope))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for rows.Next() {
		var functionName, matState string
		if err := rows.Scan(&functionName, &matState); err != nil {
			t.Fatal(err)
		}
		seen[functionName] = matState
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if seen[grant.Token] != string(RouteMaterializationRetired) || seen[successor.Token] != string(RouteMaterializationExposed) {
		t.Fatalf("materializations=%v", seen)
	}
	var aliasNonce, aliasFingerprint string
	if err := coordinator.db.QueryRow(`SELECT grant_nonce, grant_fingerprint FROM semantic_model_request_aliases WHERE alias=?`, "bash").Scan(&aliasNonce, &aliasFingerprint); err != nil {
		t.Fatal(err)
	}
	if aliasNonce != successor.Nonce || aliasFingerprint != invocationGrantFingerprint(successor) {
		t.Fatalf("alias nonce=%s fingerprint=%s", aliasNonce, aliasFingerprint)
	}
	if err := coordinator.db.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=?`, grant.Nonce).Scan(&state); err != nil || state != "revoked" {
		t.Fatalf("old state=%q err=%v", state, err)
	}
	if _, err := coordinator.CommitExpiredRenewal(issuer, scope, grant, DefaultInvocationGrantTTL, expiredAt, nil); err == nil || err.Error() != "invocation_grant_revoked" {
		t.Fatalf("second renewal err=%v", err)
	}
	admission := SemanticExecutionAdmission{
		Identity:      HostCallIdentity{Protocol: "agent-loop/v1", ConnectionID: "connection-renew", CallID: "call-renew"},
		Grant:         successor,
		RequestDigest: "request:renew",
		Scope:         scope,
		Selection:     plan.Selections[0],
		Now:           expiredAt,
	}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	clock = successor.ExpiresAt
	if _, err := coordinator.CommitExpiredRenewal(issuer, scope, successor, DefaultInvocationGrantTTL, clock, nil); err == nil || err.Error() != "invocation_grant_replayed" {
		t.Fatalf("consumed successor renewal err=%v", err)
	}
}

func TestReplaceExpiredGrantContinuesPastLayoutMissAndStopsOnCollision(t *testing.T) {
	sqlite, err := NewSQLiteInvocationGrantStore(filepath.Join(t.TempDir(), "grants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()
	plan, scope := renewalFixture(t)
	scope.ToolSnapshotID = ""
	issuedAt := time.Date(2026, 10, 4, 8, 22, 33, 0, time.UTC)
	issuer := renewalIssuer(t, sqlite, func() time.Time { return issuedAt })
	grants, err := issuer.Issue(plan, scope, DefaultInvocationGrantTTL)
	if err != nil || len(grants) != 1 {
		t.Fatalf("issue grants=%d err=%v", len(grants), err)
	}
	grant := grants[0]
	candidates := issuer.FingerprintCandidates(grant)
	if len(candidates) < 2 || candidates[0] == candidates[1] {
		t.Fatalf("fingerprint candidates=%v", candidates)
	}
	if _, err := sqlite.db.Exec(`UPDATE invocation_grants SET fingerprint=? WHERE nonce=?`, candidates[1], grant.Nonce); err != nil {
		t.Fatal(err)
	}
	expiredAt := issuedAt.Add(DefaultInvocationGrantTTL)
	successor, err := issuer.PrepareExpiredRenewal(grant, scope, plan, DefaultInvocationGrantTTL, expiredAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := sqlite.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceExpiredGrantTx(tx, issuer, grant, successor); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := tx.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=?`, grant.Nonce).Scan(&state); err != nil || state != "revoked" {
		t.Fatalf("replaced old state=%q err=%v", state, err)
	}
	if err := tx.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=?`, successor.Nonce).Scan(&state); err != nil || state != "issued" {
		t.Fatalf("successor state=%q err=%v", state, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, err := sqlite.db.Exec(`INSERT INTO invocation_grants(nonce, fingerprint, expires_at, state, created_at) VALUES (?, ?, ?, 'issued', ?)`, successor.Nonce, "occupied", successor.ExpiresAt.UTC().Format(time.RFC3339Nano), successor.IssuedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	tx, err = sqlite.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := replaceIssuedGrantTx(tx, grant.Nonce, "not-a-fingerprint", successor); err == nil || err.Error() != "invocation_grant_invalid" {
		t.Fatalf("layout miss err=%v", err)
	}
	err = replaceExpiredGrantTx(tx, issuer, grant, successor)
	if err == nil || err.Error() != "invocation grant nonce collision" {
		t.Fatalf("collision err=%v", err)
	}
	if err := tx.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=?`, grant.Nonce).Scan(&state); err != nil || state != "issued" {
		t.Fatalf("colliding renewal state=%q err=%v", state, err)
	}
}
