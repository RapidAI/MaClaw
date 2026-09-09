package tool

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DefaultInvocationGrantTTL is the production grant lifetime for IssueReady
// and PublishSurface. Hosts pass this instead of copying a private duration.
const DefaultInvocationGrantTTL = 10 * time.Minute

// InvocationScope is supplied by the trusted host, never by model arguments.
// It prevents a materialized call surface from leaking across conversations or
// being replayed after a plan revision changes.
type InvocationScope struct {
	RootTaskID  string
	PlanID      string
	SessionID   string
	TurnID      string
	PrincipalID string
	// ToolSnapshotID binds the invocation to the exact host-admitted tool
	// surface used to create its plan. It is a route/surface identity, separate
	// from InvocationGrant.CatalogDigest (the provider inventory identity), and
	// prevents a later catalog refresh from silently changing the meaning of an
	// already-issued grant.
	ToolSnapshotID string
}

// invocationScopesCompatible compares the security-bearing invocation scope
// fields.  ToolSnapshotID was added after grants and several route/materialized
// rows were already durable, so an old row can legitimately carry an empty
// value.  Treat that one value as an unknown legacy identity while requiring
// every other field to match exactly and rejecting two concrete, different
// snapshots.  Callers should still canonicalize an empty value from the
// durable route whenever possible; this helper is the narrow read/validation
// compatibility needed while that migration is in progress.
func invocationScopesCompatible(left, right InvocationScope) bool {
	if left.RootTaskID != right.RootTaskID || left.PlanID != right.PlanID ||
		left.SessionID != right.SessionID || left.TurnID != right.TurnID ||
		left.PrincipalID != right.PrincipalID {
		return false
	}
	leftSnapshot, rightSnapshot := strings.TrimSpace(left.ToolSnapshotID), strings.TrimSpace(right.ToolSnapshotID)
	return leftSnapshot == "" || rightSnapshot == "" || leftSnapshot == rightSnapshot
}

// InvocationScopesCompatible reports whether two trusted invocation scopes
// identify the same execution boundary. All identity fields are compared
// exactly. ToolSnapshotID is the sole migration exception: a durable row
// written before snapshot binding was introduced may leave it empty, which is
// treated as an unknown legacy value and is compatible with one concrete
// snapshot. Two different concrete snapshots never compare equal.
//
// This helper is intended for trusted durable readers (for example, a route
// or operation row being reconciled after a restart). It does not replace the
// strict signature checks performed by InvocationIssuer.Validate.
func InvocationScopesCompatible(left, right InvocationScope) bool {
	return invocationScopesCompatible(left, right)
}

// canonicalInvocationScope hydrates the migration-only ToolSnapshotID field
// from a trusted reference while preserving strict equality for every other
// identity component. It is for durable projections whose schema predates the
// snapshot column (artifact/delivery rows, for example); it must never be used
// to turn an untrusted model argument into an authorization scope.
func canonicalInvocationScope(scope, reference InvocationScope) (InvocationScope, error) {
	if !invocationScopesCompatible(scope, reference) {
		return InvocationScope{}, fmt.Errorf("invocation_scope_mismatch")
	}
	if strings.TrimSpace(scope.ToolSnapshotID) == "" {
		scope.ToolSnapshotID = strings.TrimSpace(reference.ToolSnapshotID)
	}
	return scope, nil
}

// CanonicalInvocationScope is the public form used by trusted host adapters
// when they read a legacy durable row and already possess the canonical route
// scope. It only fills an empty ToolSnapshotID; concrete conflicting snapshots
// and all other identity drift are rejected.
func CanonicalInvocationScope(scope, reference InvocationScope) (InvocationScope, error) {
	return canonicalInvocationScope(scope, reference)
}

// ValidateInvocationScopeSnapshot rejects restoration against a different
// host-admitted tool-surface snapshot. CatalogDigest is intentionally checked
// separately by grant/plan validation; it must not be used as a substitute
// for this scope identity. Empty expected values preserve legacy callers that
// have not yet adopted semantic tool snapshots.
func ValidateInvocationScopeSnapshot(scope InvocationScope, expectedSnapshotID string) error {
	expectedSnapshotID = strings.TrimSpace(expectedSnapshotID)
	if expectedSnapshotID == "" {
		return nil
	}
	if strings.TrimSpace(scope.ToolSnapshotID) == "" || scope.ToolSnapshotID != expectedSnapshotID {
		return fmt.Errorf("invocation_grant_stale_tool_snapshot")
	}
	return nil
}

// InvocationGrant binds one concrete selection to a one-shot consume/replay
// identity. Token is the durable route-state key; the model-visible function
// name is SemanticModelFunctionName(AdapterName), never this Token.
type InvocationGrant struct {
	Token                  string
	AdapterName            string
	SelectionID            string
	ProviderBinding        string
	FitProofDigest         string
	ParameterAuthorization ParameterAuthorization
	// CatalogDigest binds the grant to the exact provider inventory. The
	// generation remains a fast monotonic check; the digest catches restores
	// and publishers that reuse a generation after a process restart.
	CatalogDigest     string
	CatalogGeneration uint64
	Scope             InvocationScope
	IssuedAt          time.Time
	ExpiresAt         time.Time
	Nonce             string
	Signature         string
}

// invocationGrantPayloadVersion identifies the wire payload used for a
// grant's signature and replay fingerprint.  Invocation grants are durable
// records: adding a field to InvocationScope or InvocationGrant must not make
// grants issued by the previous binary unverifiable after a restart.  Keep the
// old layouts explicit instead of relying on json.Unmarshal defaults, because
// the signature covers the exact JSON bytes (including field presence and
// order).
type invocationGrantPayloadVersion uint8

const (
	invocationGrantPayloadCurrent invocationGrantPayloadVersion = iota
	// v1 was emitted after CatalogDigest was introduced, but before
	// ToolSnapshotID was added to InvocationScope.
	invocationGrantPayloadWithoutSnapshot
	// v2 was emitted after ToolSnapshotID was introduced, but before
	// CatalogDigest was introduced.  It is retained for mixed-version rolling
	// upgrades even though the two fields landed close together.
	invocationGrantPayloadWithoutCatalog
	// The original payload had neither CatalogDigest nor ToolSnapshotID.
	invocationGrantPayloadLegacy
)

// legacyInvocationScope is the exact pre-ToolSnapshotID JSON shape.  Do not
// alias InvocationScope here: encoding/json would then include the new field
// and old signatures would still fail verification.
type legacyInvocationScope struct {
	RootTaskID  string
	PlanID      string
	SessionID   string
	TurnID      string
	PrincipalID string
}

func legacyInvocationScopeFor(scope InvocationScope) legacyInvocationScope {
	return legacyInvocationScope{
		RootTaskID: scope.RootTaskID, PlanID: scope.PlanID, SessionID: scope.SessionID,
		TurnID: scope.TurnID, PrincipalID: scope.PrincipalID,
	}
}

// InvocationIssuer signs, validates and atomically consumes grants. A grant is
// intentionally short lived. Its mutable state belongs to InvocationGrantStore
// so durable hosts never fall back to a process-local "unused" view after a
// reconnect or restart.
type InvocationIssuer struct {
	key   []byte
	now   func() time.Time
	store InvocationGrantStore
}

func NewInvocationIssuer(key []byte) (*InvocationIssuer, error) {
	return NewInvocationIssuerWithStore(key, NewMemoryInvocationGrantStore())
}

// NewInvocationIssuerWithStore creates an issuer whose mutable grant state is
// owned by store. Production hosts that may restart or have more than one
// executor must provide a durable, shared store; the memory store is reserved
// for tests and explicit single-process development.
func NewInvocationIssuerWithStore(key []byte, store InvocationGrantStore) (*InvocationIssuer, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("invocation issuer key must be at least 32 bytes")
	}
	if store == nil {
		return nil, fmt.Errorf("invocation grant store is required")
	}
	return &InvocationIssuer{
		key:   append([]byte(nil), key...),
		now:   func() time.Time { return time.Now().UTC() },
		store: store,
	}, nil
}

func NewRandomInvocationIssuer() (*InvocationIssuer, error) {
	return NewRandomInvocationIssuerWithStore(NewMemoryInvocationGrantStore())
}

func NewRandomInvocationIssuerWithStore(store InvocationGrantStore) (*InvocationIssuer, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate invocation issuer key: %w", err)
	}
	return NewInvocationIssuerWithStore(key, store)
}

// Issue binds only the initially-ready selections of the plan to one trusted
// scope. A renderer may use Token as a function suffix but must never expose
// the signature payload itself as a model-controlled argument. Future DAG
// nodes must be materialized with IssueReady after the executor has recorded
// their trusted dependencies; issuing a grant is itself an authorization
// boundary, not merely a presentation concern.
func (i *InvocationIssuer) Issue(plan ToolPlan, scope InvocationScope, ttl time.Duration) ([]InvocationGrant, error) {
	return i.IssueReady(plan, scope, ttl, nil)
}

// IssueReady binds precisely the current exposure closure. satisfied must
// contain only executor-verified selection/artifact/confirmation facts; model
// call order or prose must never be used to make a future selection ready.
func (i *InvocationIssuer) IssueReady(plan ToolPlan, scope InvocationScope, ttl time.Duration, satisfied map[string]bool) ([]InvocationGrant, error) {
	if i == nil {
		return nil, fmt.Errorf("nil invocation issuer")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("invocation grant ttl must be positive")
	}
	if strings.TrimSpace(plan.ID) == "" || strings.TrimSpace(plan.RootTaskID) == "" {
		return nil, fmt.Errorf("plan identity is required")
	}
	if scope.RootTaskID != plan.RootTaskID || scope.PlanID != plan.ID {
		return nil, fmt.Errorf("invocation scope does not match plan")
	}
	now := i.now().UTC()
	ready := plan.ReadySelections(satisfied)
	grants := make([]InvocationGrant, 0, len(ready))
	for _, selection := range ready {
		if satisfied[selection.ID] {
			// A completed node remains part of the immutable plan and may be
			// needed as a dependency fact, but must never be re-materialized.
			continue
		}
		if strings.TrimSpace(selection.ID) == "" || strings.TrimSpace(selection.AdapterName) == "" || strings.TrimSpace(selection.FitProof.Digest) == "" {
			return nil, fmt.Errorf("selection is not materializable")
		}
		nonce, err := randomInvocationNonce()
		if err != nil {
			return nil, err
		}
		grant := InvocationGrant{
			AdapterName:            selection.AdapterName,
			SelectionID:            selection.ID,
			ProviderBinding:        selection.Provider.StableID(),
			FitProofDigest:         selection.FitProof.Digest,
			ParameterAuthorization: selection.ParameterAuthorization,
			CatalogDigest:          plan.CatalogDigest,
			CatalogGeneration:      plan.CatalogGeneration,
			Scope:                  scope,
			IssuedAt:               now,
			ExpiresAt:              now.Add(ttl),
			Nonce:                  nonce,
		}
		grant.Token = invocationToken(grant)
		grant.Signature = i.sign(grant)
		grants = append(grants, grant)
	}
	if err := i.store.RecordIssued(grants); err != nil {
		return nil, fmt.Errorf("record invocation grants: %w", err)
	}
	return grants, nil
}

// Validate validates the immutable part of a grant without changing its replay
// state. It exists for the unified execution coordinator, which must consume a
// grant together with the host-call and execution admission in one durable
// transaction. Callers that do not own that transaction must use
// ValidateAndConsume instead.
func (i *InvocationIssuer) Validate(grant InvocationGrant, scope InvocationScope, plan ToolPlan, satisfied ...map[string]bool) (PlannedSelection, error) {
	selection, _, err := i.validateWithPayloadVersions(grant, scope, plan, false, satisfied...)
	return selection, err
}

// ValidateWithCanonicalScope is the explicit migration entry point for a
// trusted route/model-surface reader. Grants issued before ToolSnapshotID was
// added cannot authenticate that field, so the reader may supply the exact
// snapshot it recovered from durable state and validate the historical
// signature against the original empty field. Direct Validate remains strict:
// callers that do not own a trusted route snapshot must not guess one from
// model input or a function name.
func (i *InvocationIssuer) ValidateWithCanonicalScope(grant InvocationGrant, scope InvocationScope, plan ToolPlan, satisfied ...map[string]bool) (PlannedSelection, error) {
	selection, _, err := i.validateWithPayloadVersions(grant, scope, plan, true, satisfied...)
	return selection, err
}

// validateWithPayloadVersions is the implementation shared by Validate and
// ValidateAndConsume. The returned versions are the payloads whose HMAC
// matched the grant. A caller must use those versions when looking up the
// durable replay fingerprint; recomputing only the current payload would make
// a valid pre-migration grant appear to have disappeared from the store.
func (i *InvocationIssuer) validateWithPayloadVersions(grant InvocationGrant, scope InvocationScope, plan ToolPlan, allowCanonicalSnapshot bool, satisfied ...map[string]bool) (PlannedSelection, []invocationGrantPayloadVersion, error) {
	if i == nil {
		return PlannedSelection{}, nil, fmt.Errorf("nil invocation issuer")
	}
	versions := invocationGrantMatchingPayloadVersions(grant, i.key)
	if len(versions) == 0 {
		return PlannedSelection{}, nil, fmt.Errorf("invocation_grant_invalid")
	}
	// Keep the signed grant untouched so historical HMAC/fingerprint lookup
	// remains possible. For the explicit trusted-route entry point only, use a
	// separate effective copy whose snapshot is the route's canonical value.
	effectiveGrant := grant
	if allowCanonicalSnapshot && strings.TrimSpace(grant.Scope.ToolSnapshotID) == "" && strings.TrimSpace(scope.ToolSnapshotID) != "" {
		if grant.Scope.RootTaskID != scope.RootTaskID || grant.Scope.PlanID != scope.PlanID || grant.Scope.SessionID != scope.SessionID || grant.Scope.TurnID != scope.TurnID || grant.Scope.PrincipalID != scope.PrincipalID {
			return PlannedSelection{}, nil, fmt.Errorf("invocation_grant_scope_mismatch")
		}
		effectiveGrant.Scope.ToolSnapshotID = strings.TrimSpace(scope.ToolSnapshotID)
	}
	if !i.now().UTC().Before(grant.ExpiresAt.UTC()) || grant.ExpiresAt.Before(grant.IssuedAt) {
		return PlannedSelection{}, nil, fmt.Errorf("invocation_grant_expired")
	}
	if effectiveGrant.Scope != scope || scope.RootTaskID != plan.RootTaskID || scope.PlanID != plan.ID {
		return PlannedSelection{}, nil, fmt.Errorf("invocation_grant_scope_mismatch")
	}
	if effectiveGrant.CatalogGeneration != plan.CatalogGeneration {
		return PlannedSelection{}, nil, fmt.Errorf("invocation_grant_stale")
	}
	// Legacy grants predate CatalogDigest. Treat an empty side as an unknown
	// legacy identity (the generation and signed selection bindings still
	// apply), while rejecting two concrete, different digests fail-closed.
	if strings.TrimSpace(plan.CatalogDigest) != "" && strings.TrimSpace(effectiveGrant.CatalogDigest) != "" && effectiveGrant.CatalogDigest != plan.CatalogDigest {
		return PlannedSelection{}, nil, fmt.Errorf("invocation_grant_stale_catalog")
	}
	completed := map[string]bool(nil)
	if len(satisfied) > 0 {
		completed = satisfied[0]
	}
	for _, selection := range plan.Selections {
		if selection.ID != effectiveGrant.SelectionID {
			continue
		}
		if selection.AdapterName != effectiveGrant.AdapterName || selection.Provider.StableID() != effectiveGrant.ProviderBinding || selection.FitProof.Digest != effectiveGrant.FitProofDigest || !parameterAuthorizationsEqual(selection.ParameterAuthorization, effectiveGrant.ParameterAuthorization) {
			return PlannedSelection{}, nil, fmt.Errorf("invocation_grant_binding_mismatch")
		}
		if !invocationSelectionReady(selection, completed) {
			return PlannedSelection{}, nil, fmt.Errorf("selection_not_ready")
		}
		return selection, versions, nil
	}
	return PlannedSelection{}, nil, fmt.Errorf("invocation_grant_selection_not_found")
}

// ValidateAndConsume validates a grant against a trusted runtime scope and a
// current immutable plan, then consumes it. An optional satisfied map supplies
// executor-verified dependencies for a later plan phase. With no map, only
// initially-ready selections can execute. Hosts needing an atomic boundary
// across host-call admission and execution must instead use Validate followed
// by SemanticExecutionCoordinator.Admit.
func (i *InvocationIssuer) ValidateAndConsume(grant InvocationGrant, scope InvocationScope, plan ToolPlan, satisfied ...map[string]bool) (PlannedSelection, error) {
	selection, matchedVersions, err := i.validateWithPayloadVersions(grant, scope, plan, false, satisfied...)
	if err != nil {
		return PlannedSelection{}, err
	}
	return i.consumeValidatedGrant(grant, selection, matchedVersions)
}

// ValidateAndConsumeWithCanonicalScope is the explicit one-shot migration
// entry point for a trusted route/model-surface recovery path. It accepts an
// old grant whose signed scope omitted ToolSnapshotID only when the caller
// supplies a concrete canonical scope from durable route state. Callers that
// do not own that state must use the strict ValidateAndConsume method.
func (i *InvocationIssuer) ValidateAndConsumeWithCanonicalScope(grant InvocationGrant, scope InvocationScope, plan ToolPlan, satisfied ...map[string]bool) (PlannedSelection, error) {
	selection, matchedVersions, err := i.validateWithPayloadVersions(grant, scope, plan, true, satisfied...)
	if err != nil {
		return PlannedSelection{}, err
	}
	return i.consumeValidatedGrant(grant, selection, matchedVersions)
}

func (i *InvocationIssuer) consumeValidatedGrant(grant InvocationGrant, selection PlannedSelection, matchedVersions []invocationGrantPayloadVersion) (PlannedSelection, error) {
	// Normally the matching signature version and the persisted fingerprint
	// version are identical. During a rolling upgrade an operator may have
	// rebuilt the mutable store from grant JSON, so try the other layouts only
	// after a fingerprint miss. A terminal state is returned immediately;
	// never consume a second candidate after the nonce has become consumed or
	// revoked.
	versions := invocationGrantPayloadVersionsForStore(grant, matchedVersions)
	var state InvocationGrantConsumeResult
	var err error
	for _, version := range versions {
		state, err = i.store.Consume(grant.Nonce, invocationGrantFingerprintForVersion(grant, version), i.now().UTC())
		if err != nil {
			return PlannedSelection{}, fmt.Errorf("invocation_grant_store_unavailable")
		}
		if state != InvocationGrantConsumeInvalid {
			break
		}
	}
	switch state {
	case InvocationGrantConsumeAccepted:
		return selection, nil
	case InvocationGrantConsumeRevoked:
		return PlannedSelection{}, fmt.Errorf("invocation_grant_revoked")
	case InvocationGrantConsumeConsumed:
		return PlannedSelection{}, fmt.Errorf("invocation_grant_replayed")
	case InvocationGrantConsumeExpired:
		return PlannedSelection{}, fmt.Errorf("invocation_grant_expired")
	default:
		return PlannedSelection{}, fmt.Errorf("invocation_grant_invalid")
	}
}

func invocationSelectionReady(selection PlannedSelection, satisfied map[string]bool) bool {
	for _, requirement := range selection.Requires {
		if !satisfied[requirement] {
			return false
		}
	}
	return true
}

func (i *InvocationIssuer) Revoke(grant InvocationGrant) {
	_ = i.RevokeWithError(grant)
}

// RevokeWithError revokes an issued grant before execution. It is safe to call
// repeatedly; a consumed grant remains consumed, so a later revoke cannot
// revive or change the terminal admission result.
func (i *InvocationIssuer) RevokeWithError(grant InvocationGrant) error {
	if i == nil {
		return fmt.Errorf("nil invocation issuer")
	}
	if strings.TrimSpace(grant.Nonce) == "" {
		return fmt.Errorf("invocation grant nonce is required")
	}
	// A valid old grant carries enough information to identify its historical
	// payload layout. Prefer that layout so revocation also works while the
	// durable store still contains the pre-migration fingerprint. For an
	// unsigned/forensic grant retain the current-layout behavior.
	versions := invocationGrantMatchingPayloadVersions(grant, i.key)
	if len(versions) == 0 {
		versions = []invocationGrantPayloadVersion{invocationGrantPayloadCurrent}
	}
	var lastErr error
	for _, version := range versions {
		if err := i.store.Revoke(grant.Nonce, invocationGrantFingerprintForVersion(grant, version)); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("revoke invocation grant: %w", lastErr)
	}
	return nil
}

func (i *InvocationIssuer) sign(grant InvocationGrant) string {
	return invocationGrantSignatureForVersion(i.key, grant, invocationGrantPayloadCurrent)
}

// Fingerprint returns the durable replay fingerprint for a grant using the
// payload layout that its signature authenticates. New grants use the current
// layout; a persisted legacy grant keeps its historical fingerprint so host
// journals and non-coordinated adapters can resume it after an upgrade.
// Invalid signatures fall back to the current layout and are still rejected by
// Validate before execution.
func (i *InvocationIssuer) Fingerprint(grant InvocationGrant) string {
	if i != nil {
		if matched := invocationGrantMatchingPayloadVersions(grant, i.key); len(matched) > 0 {
			return invocationGrantFingerprintForVersion(grant, matched[0])
		}
	}
	return invocationGrantFingerprint(grant)
}

// FingerprintCandidates exposes the compatible durable identities for callers
// that need to query a persisted row without changing its stored fingerprint.
// The current layout is first so newly-written records remain deterministic.
func (i *InvocationIssuer) FingerprintCandidates(grant InvocationGrant) []string {
	return invocationGrantFingerprintCandidates(grant)
}

func invocationToken(grant InvocationGrant) string {
	return invocationTokenForVersion(grant, invocationGrantPayloadCurrent)
}

func invocationTokenForVersion(grant InvocationGrant, version invocationGrantPayloadVersion) string {
	sum := sha256.Sum256([]byte(invocationSignaturePayloadVersion(grant, version)))
	return "invoke_" + base64.RawURLEncoding.EncodeToString(sum[:18])
}

func invocationGrantFingerprint(grant InvocationGrant) string {
	return invocationGrantFingerprintForVersion(grant, invocationGrantPayloadCurrent)
}

func invocationGrantFingerprintForVersion(grant InvocationGrant, version invocationGrantPayloadVersion) string {
	sum := sha256.Sum256([]byte(invocationSignaturePayloadVersion(grant, version) + "\x00" + grant.Signature))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// invocationGrantPayloadVersionsForStore returns deterministic candidates,
// with HMAC-matching versions first. The extra allowed layouts make a grant
// readable if a prior migration wrote a current fingerprint for a legacy
// signature (or vice versa); a nonce still has only one store row, so this
// cannot authorize two consumptions.
func invocationGrantPayloadVersionsForStore(grant InvocationGrant, matched []invocationGrantPayloadVersion) []invocationGrantPayloadVersion {
	allowed := invocationGrantPayloadVersions(grant)
	ordered := make([]invocationGrantPayloadVersion, 0, len(allowed))
	seen := make(map[invocationGrantPayloadVersion]bool, len(allowed))
	for _, version := range matched {
		if !seen[version] {
			ordered = append(ordered, version)
			seen[version] = true
		}
	}
	for _, version := range allowed {
		if !seen[version] {
			ordered = append(ordered, version)
			seen[version] = true
		}
	}
	return ordered
}

// invocationGrantMatchingPayloadVersions verifies the signature against the
// current layout and every historical layout that is safe for the fields
// present in the grant. A historical layout that omitted a newly-added field
// is considered only when that field is empty in the decoded grant. This is
// the fail-closed rule that prevents an attacker from signing an old payload
// and then injecting a new non-empty ToolSnapshotID/CatalogDigest.
func invocationGrantMatchingPayloadVersions(grant InvocationGrant, key []byte) []invocationGrantPayloadVersion {
	matched := make([]invocationGrantPayloadVersion, 0, 2)
	for _, version := range invocationGrantPayloadVersions(grant) {
		if hmac.Equal([]byte(grant.Signature), []byte(invocationGrantSignatureForVersion(key, grant, version))) {
			matched = append(matched, version)
		}
	}
	return matched
}

// invocationGrantFingerprintCandidates returns all layouts permitted by the
// decoded fields. It is used by durable projections that do not own the issuer
// key (for example, alias/revocation repair) and therefore cannot first
// identify the HMAC-matching layout.
func invocationGrantFingerprintCandidates(grant InvocationGrant) []string {
	versions := invocationGrantPayloadVersions(grant)
	result := make([]string, 0, len(versions))
	seen := make(map[string]bool, len(versions))
	for _, version := range versions {
		fingerprint := invocationGrantFingerprintForVersion(grant, version)
		if !seen[fingerprint] {
			result = append(result, fingerprint)
			seen[fingerprint] = true
		}
	}
	return result
}

func invocationGrantFingerprintEquivalent(left, right InvocationGrant) bool {
	rightFingerprints := make(map[string]bool)
	for _, fingerprint := range invocationGrantFingerprintCandidates(right) {
		rightFingerprints[fingerprint] = true
	}
	for _, fingerprint := range invocationGrantFingerprintCandidates(left) {
		if rightFingerprints[fingerprint] {
			return true
		}
	}
	return false
}

func invocationGrantFingerprintIsCandidate(grant InvocationGrant, fingerprint string) bool {
	for _, candidate := range invocationGrantFingerprintCandidates(grant) {
		if candidate == fingerprint {
			return true
		}
	}
	return false
}

func containsInvocationGrantFingerprint(fingerprints []string, wanted string) bool {
	for _, fingerprint := range fingerprints {
		if fingerprint == wanted {
			return true
		}
	}
	return false
}

// invocationGrantFingerprintSQLArgs returns a placeholder list and its
// fingerprint arguments for a dynamic `IN (...)` predicate. SQLite stores one
// fingerprint per nonce, but historical grants may use any one of the
// compatible payload layouts.
func invocationGrantFingerprintSQLArgs(grant InvocationGrant) (string, []interface{}) {
	fingerprints := invocationGrantFingerprintCandidates(grant)
	placeholders := make([]string, len(fingerprints))
	args := make([]interface{}, len(fingerprints))
	for index, fingerprint := range fingerprints {
		placeholders[index] = "?"
		args[index] = fingerprint
	}
	return strings.Join(placeholders, ","), args
}

func invocationGrantPayloadVersions(grant InvocationGrant) []invocationGrantPayloadVersion {
	versions := []invocationGrantPayloadVersion{invocationGrantPayloadCurrent}
	// A payload can omit ToolSnapshotID only when the decoded grant has no
	// snapshot identity. Likewise CatalogDigest omission is safe only for an
	// empty decoded digest. Keep both mixed-version combinations for rolling
	// upgrades where the fields were introduced independently.
	if grant.Scope.ToolSnapshotID == "" {
		versions = append(versions, invocationGrantPayloadWithoutSnapshot)
	}
	if grant.CatalogDigest == "" {
		versions = append(versions, invocationGrantPayloadWithoutCatalog)
	}
	if grant.Scope.ToolSnapshotID == "" && grant.CatalogDigest == "" {
		versions = append(versions, invocationGrantPayloadLegacy)
	}
	return versions
}

func invocationGrantSignatureForVersion(key []byte, grant InvocationGrant, version invocationGrantPayloadVersion) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(invocationSignaturePayloadVersion(grant, version)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func invocationSignaturePayload(grant InvocationGrant) string {
	return invocationSignaturePayloadVersion(grant, invocationGrantPayloadCurrent)
}

func invocationSignaturePayloadVersion(grant InvocationGrant, version invocationGrantPayloadVersion) string {
	var payload interface{}
	switch version {
	case invocationGrantPayloadWithoutSnapshot:
		payload = struct {
			Token                  string
			AdapterName            string
			SelectionID            string
			ProviderBinding        string
			FitProofDigest         string
			ParameterAuthorization ParameterAuthorization
			CatalogDigest          string
			CatalogGeneration      uint64
			Scope                  legacyInvocationScope
			IssuedAt               int64
			ExpiresAt              int64
			Nonce                  string
		}{
			Token: grant.Token, AdapterName: grant.AdapterName, SelectionID: grant.SelectionID,
			ProviderBinding: grant.ProviderBinding, FitProofDigest: grant.FitProofDigest, ParameterAuthorization: grant.ParameterAuthorization,
			CatalogDigest: grant.CatalogDigest, CatalogGeneration: grant.CatalogGeneration,
			Scope: legacyInvocationScopeFor(grant.Scope), IssuedAt: grant.IssuedAt.UnixNano(), ExpiresAt: grant.ExpiresAt.UnixNano(), Nonce: grant.Nonce,
		}
	case invocationGrantPayloadWithoutCatalog:
		payload = struct {
			Token                  string
			AdapterName            string
			SelectionID            string
			ProviderBinding        string
			FitProofDigest         string
			ParameterAuthorization ParameterAuthorization
			CatalogGeneration      uint64
			Scope                  InvocationScope
			IssuedAt               int64
			ExpiresAt              int64
			Nonce                  string
		}{
			Token: grant.Token, AdapterName: grant.AdapterName, SelectionID: grant.SelectionID,
			ProviderBinding: grant.ProviderBinding, FitProofDigest: grant.FitProofDigest, ParameterAuthorization: grant.ParameterAuthorization,
			CatalogGeneration: grant.CatalogGeneration, Scope: grant.Scope,
			IssuedAt: grant.IssuedAt.UnixNano(), ExpiresAt: grant.ExpiresAt.UnixNano(), Nonce: grant.Nonce,
		}
	case invocationGrantPayloadLegacy:
		payload = struct {
			Token                  string
			AdapterName            string
			SelectionID            string
			ProviderBinding        string
			FitProofDigest         string
			ParameterAuthorization ParameterAuthorization
			CatalogGeneration      uint64
			Scope                  legacyInvocationScope
			IssuedAt               int64
			ExpiresAt              int64
			Nonce                  string
		}{
			Token: grant.Token, AdapterName: grant.AdapterName, SelectionID: grant.SelectionID,
			ProviderBinding: grant.ProviderBinding, FitProofDigest: grant.FitProofDigest, ParameterAuthorization: grant.ParameterAuthorization,
			CatalogGeneration: grant.CatalogGeneration, Scope: legacyInvocationScopeFor(grant.Scope),
			IssuedAt: grant.IssuedAt.UnixNano(), ExpiresAt: grant.ExpiresAt.UnixNano(), Nonce: grant.Nonce,
		}
	default:
		payload = struct {
			Token                  string
			AdapterName            string
			SelectionID            string
			ProviderBinding        string
			FitProofDigest         string
			ParameterAuthorization ParameterAuthorization
			CatalogDigest          string
			CatalogGeneration      uint64
			Scope                  InvocationScope
			IssuedAt               int64
			ExpiresAt              int64
			Nonce                  string
		}{
			Token: grant.Token, AdapterName: grant.AdapterName, SelectionID: grant.SelectionID,
			ProviderBinding: grant.ProviderBinding, FitProofDigest: grant.FitProofDigest, ParameterAuthorization: grant.ParameterAuthorization,
			CatalogDigest: grant.CatalogDigest, CatalogGeneration: grant.CatalogGeneration, Scope: grant.Scope,
			IssuedAt: grant.IssuedAt.UnixNano(), ExpiresAt: grant.ExpiresAt.UnixNano(), Nonce: grant.Nonce,
		}
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func randomInvocationNonce() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate invocation nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
