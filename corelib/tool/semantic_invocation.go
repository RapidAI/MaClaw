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

// InvocationScope is supplied by the trusted host, never by model arguments.
// It prevents a materialized call surface from leaking across conversations or
// being replayed after a plan revision changes.
type InvocationScope struct {
	RootTaskID  string
	PlanID      string
	SessionID   string
	TurnID      string
	PrincipalID string
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
	CatalogGeneration      uint64
	Scope                  InvocationScope
	IssuedAt               time.Time
	ExpiresAt              time.Time
	Nonce                  string
	Signature              string
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
	if i == nil {
		return PlannedSelection{}, fmt.Errorf("nil invocation issuer")
	}
	if !hmac.Equal([]byte(grant.Signature), []byte(i.sign(grant))) {
		return PlannedSelection{}, fmt.Errorf("invocation_grant_invalid")
	}
	if !i.now().UTC().Before(grant.ExpiresAt.UTC()) || grant.ExpiresAt.Before(grant.IssuedAt) {
		return PlannedSelection{}, fmt.Errorf("invocation_grant_expired")
	}
	if grant.Scope != scope || scope.RootTaskID != plan.RootTaskID || scope.PlanID != plan.ID {
		return PlannedSelection{}, fmt.Errorf("invocation_grant_scope_mismatch")
	}
	if grant.CatalogGeneration != plan.CatalogGeneration {
		return PlannedSelection{}, fmt.Errorf("invocation_grant_stale")
	}
	completed := map[string]bool(nil)
	if len(satisfied) > 0 {
		completed = satisfied[0]
	}
	for _, selection := range plan.Selections {
		if selection.ID != grant.SelectionID {
			continue
		}
		if selection.AdapterName != grant.AdapterName || selection.Provider.StableID() != grant.ProviderBinding || selection.FitProof.Digest != grant.FitProofDigest || !parameterAuthorizationsEqual(selection.ParameterAuthorization, grant.ParameterAuthorization) {
			return PlannedSelection{}, fmt.Errorf("invocation_grant_binding_mismatch")
		}
		if !invocationSelectionReady(selection, completed) {
			return PlannedSelection{}, fmt.Errorf("selection_not_ready")
		}
		return selection, nil
	}
	return PlannedSelection{}, fmt.Errorf("invocation_grant_selection_not_found")
}

// ValidateAndConsume validates a grant against a trusted runtime scope and a
// current immutable plan, then consumes it. An optional satisfied map supplies
// executor-verified dependencies for a later plan phase. With no map, only
// initially-ready selections can execute. Hosts needing an atomic boundary
// across host-call admission and execution must instead use Validate followed
// by SemanticExecutionCoordinator.Admit.
func (i *InvocationIssuer) ValidateAndConsume(grant InvocationGrant, scope InvocationScope, plan ToolPlan, satisfied ...map[string]bool) (PlannedSelection, error) {
	selection, err := i.Validate(grant, scope, plan, satisfied...)
	if err != nil {
		return PlannedSelection{}, err
	}
	state, err := i.store.Consume(grant.Nonce, invocationGrantFingerprint(grant), i.now().UTC())
	if err != nil {
		return PlannedSelection{}, fmt.Errorf("invocation_grant_store_unavailable")
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
	if err := i.store.Revoke(grant.Nonce, invocationGrantFingerprint(grant)); err != nil {
		return fmt.Errorf("revoke invocation grant: %w", err)
	}
	return nil
}

func (i *InvocationIssuer) sign(grant InvocationGrant) string {
	mac := hmac.New(sha256.New, i.key)
	_, _ = mac.Write([]byte(invocationSignaturePayload(grant)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func invocationToken(grant InvocationGrant) string {
	sum := sha256.Sum256([]byte(invocationSignaturePayload(grant)))
	return "invoke_" + base64.RawURLEncoding.EncodeToString(sum[:18])
}

func invocationGrantFingerprint(grant InvocationGrant) string {
	sum := sha256.Sum256([]byte(invocationSignaturePayload(grant) + "\x00" + grant.Signature))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func invocationSignaturePayload(grant InvocationGrant) string {
	payload := struct {
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
