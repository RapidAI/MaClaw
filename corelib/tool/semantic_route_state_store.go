package tool

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// RouteStateVersion is included in the immutable plan payload so a host never
// silently interprets a persisted materialization with a newer plan format.
const RouteStateVersion = "semantic-route-state-v1"

type RouteMaterializationState string

const (
	RouteMaterializationExposed RouteMaterializationState = "exposed"
	RouteMaterializationRetired RouteMaterializationState = "retired"
)

// RouteMaterialization is the durable, server-owned lookup between an opaque
// host function and a signed grant. FunctionName is not authorization; it is
// only the key used to recover the already-authorized grant.
type RouteMaterialization struct {
	FunctionName string
	Grant        InvocationGrant
	State        RouteMaterializationState
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// RouteCompletedSelection is an execution fact, not an authorization. Its
// purpose digest lets a child revision reuse a completed prerequisite only
// when the same capability purpose, qualifiers, artifact contracts and effect
// boundary still exist. Adapter/grant/host-call state is deliberately absent.
type RouteCompletedSelection struct {
	SelectionID   string
	PurposeDigest string
	CompletedAt   time.Time
}

// RouteConfirmation is a trusted, scope-bound approval fact. Requirement and
// PurposeDigest identify what was approved; unlike an invocation grant it
// contains neither an adapter binding nor model-writable parameters.
type RouteConfirmation struct {
	Requirement   string
	PurposeDigest string
	Authority     FactAuthority
	ValidUntil    time.Time
	GrantedAt     time.Time
}

// RouteArtifactRef is a trusted metadata projection for an immutable artifact
// payload. It deliberately omits the payload and every access grant. A child
// revision may retain this reference only when the producer purpose and a
// current consumer artifact contract remain compatible; the ArtifactStore
// must still mint a new, child-scoped one-time grant before content is read.
type RouteArtifactRef struct {
	ArtifactID            string
	Kind                  string
	MIMEType              string
	IntegrityDigest       string
	ProducerSelection     string
	ProducerPurposeDigest string
	SourceScope           InvocationScope
	CreatedAt             time.Time
}

// RouteRevisionRef identifies one immutable published decision within a
// logical task. It intentionally excludes TurnID: a replan normally happens
// in a later turn, while principal and session remain part of the authority
// boundary. PlanID is retained as an immutable decision reference, never as a
// provider lookup key.
type RouteRevisionRef struct {
	RootTaskID  string
	SessionID   string
	PrincipalID string
	Revision    uint64
	PlanID      string
	PlanDigest  string
}

// RouteAmendmentRef is immutable, host-verified evidence that a child route
// changes the logical task rather than merely replacing a failed binding. It
// contains neither the amendment text nor any provider/grant authority. The
// coordinator binds CommandID to the parent revision and consumes it in the
// same transaction that publishes the child surface.
type RouteAmendmentRef struct {
	CommandID          string
	Digest             string
	ParentRevision     uint64
	ParentFencingToken uint64
}

// RouteRevisionPublishRequest is the compare-and-publish input for a new
// immutable route revision. SnapshotDigest is supplied by the trusted route
// snapshot owner; it prevents a caller from treating the same plan bytes as a
// publication from an unspecified or different snapshot.
type RouteRevisionPublishRequest struct {
	Scope          InvocationScope
	Plan           ToolPlan
	ExpectedParent *RouteRevisionRef
	SnapshotDigest string
	Amendment      *RouteAmendmentRef
}

// RouteState persists an immutable plan revision and every materialization of
// that revision. It deliberately does not persist mutable artifact payloads,
// provider output, credentials, or untrusted model arguments.
type RouteState struct {
	Version        string
	Scope          InvocationScope
	Plan           ToolPlan
	PlanDigest     string
	Revision       *RouteRevisionRef
	ParentRevision *RouteRevisionRef
	SnapshotDigest string
	Amendment      *RouteAmendmentRef
	// FencingToken is the monotonic token allocated inside the publish
	// transaction. It orders this revision against every outbox
	// prepare/claim in the same database; outbox records stamped with an
	// older token are fenced off once a newer revision publishes. Zero marks
	// a legacy Open-only state that predates revision fencing.
	FencingToken     uint64
	Materializations []RouteMaterialization
	Completed        []RouteCompletedSelection
	Confirmations    []RouteConfirmation
	Artifacts        []RouteArtifactRef
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// RouteStateStore owns plan-revision materialization state. Open is a
// compare-and-create operation: a pre-existing scope may only be recovered if
// its complete immutable plan payload has the same digest.
type RouteStateStore interface {
	Open(scope InvocationScope, plan ToolPlan, now time.Time) (RouteState, error)
	// PublishRevision is the only operation that advances a logical task. It
	// conditionally replaces the current revision, retires the parent's
	// materializations, and never copies adapter/grant state to the child.
	// Each successful publish allocates a monotonic fencing token inside the
	// same transaction (returned as RouteState.FencingToken); outbox
	// prepare/claim records stamped with an older token are fenced off from
	// dispatch and settlement once a newer revision exists.
	PublishRevision(request RouteRevisionPublishRequest, now time.Time) (RouteState, error)
	// IsCurrent rejects an invocation from a published revision that a newer
	// revision has superseded. Legacy Open-only states deliberately return nil
	// so existing migration families remain compatible until they opt in.
	IsCurrent(scope InvocationScope) error
	// CurrentRevision returns the current immutable revision for the trusted
	// root/session/principal lineage. Hosts use it as the expected parent of a
	// new candidate; it never returns adapters, grants, parameters, or facts.
	CurrentRevision(scope InvocationScope) (RouteRevisionRef, error)
	// PublishedPlan returns the immutable plan for this exact scope. It is a
	// trusted reconciliation read: it exposes neither function materializations
	// nor grants, and exists so a receipt worker can bind a settlement to the
	// originally selected effect rather than accepting a caller-supplied one.
	PublishedPlan(scope InvocationScope) (ToolPlan, error)
	// RecordSelectionCompletion persists a provider-success fact only after the
	// execution store has durably recorded the same selection as succeeded.
	// The returned state is still immutable with respect to plan/binding data.
	RecordSelectionCompletion(scope InvocationScope, planID, selectionID string, now time.Time) (RouteState, error)
	// CompletedSelections returns the current revision's trusted completion
	// facts. A host may use them only as DAG dependencies; they never revive an
	// adapter, grant, host call, or parameter authorization.
	CompletedSelections(scope InvocationScope) (map[string]bool, error)
	// RecordConfirmation accepts only an already verified trusted confirmation
	// fact for a requirement in the current immutable plan. It does not accept
	// model text, provider output, a grant, or an arbitrary selection ID.
	RecordConfirmation(scope InvocationScope, planID string, fact RoutingFact, now time.Time) (RouteState, error)
	// ConfirmedRequirements returns non-expired, current-plan approvals solely
	// as DAG dependencies. It never restores a prior invocation authority.
	ConfirmedRequirements(scope InvocationScope, now time.Time) (map[string]bool, error)
	// RecordArtifact stores a trusted payload reference after the execution
	// plane has published it. The payload itself and all access grants remain
	// outside RouteState.
	RecordArtifact(scope InvocationScope, planID string, ref ArtifactRef, now time.Time) (RouteState, error)
	// ArtifactRefs returns immutable metadata references compatible with this
	// revision. Callers must pass one back to ArtifactStore for a fresh grant;
	// this method never grants content access.
	ArtifactRefs(scope InvocationScope) ([]RouteArtifactRef, error)
	// CurrentArtifactRefs resolves the current revision for a root/session/
	// principal lineage. It is read-only planning input: callers still need a
	// specific returned reference and a child-scoped ArtifactStore grant to
	// consume content.
	CurrentArtifactRefs(scope InvocationScope) ([]RouteArtifactRef, error)
	// ReconcileArtifacts repairs the recoverable publication gap after a
	// producer selection has durably completed but before its already-published
	// ArtifactRef reached RouteState. It only enumerates the exact completed
	// producer scope, never a model-selected or root-wide artifact set.
	ReconcileArtifacts(scope InvocationScope, artifacts ArtifactStore, now time.Time) (RouteState, error)
	// ReconcileCurrentArtifacts applies the same repair to the current
	// root/session/principal revision before a host attempts to publish a child.
	ReconcileCurrentArtifacts(scope InvocationScope, artifacts ArtifactStore, now time.Time) (RouteState, error)
	RecordMaterialization(scope InvocationScope, planID string, materialization RouteMaterialization, now time.Time) (RouteState, error)
	RetireMaterialization(scope InvocationScope, planID, functionName string, now time.Time) (RouteState, error)
}

func validateRouteStateScope(scope InvocationScope, plan ToolPlan) error {
	if strings.TrimSpace(scope.RootTaskID) == "" || strings.TrimSpace(scope.PlanID) == "" || strings.TrimSpace(scope.SessionID) == "" || strings.TrimSpace(scope.TurnID) == "" || strings.TrimSpace(scope.PrincipalID) == "" {
		return fmt.Errorf("route_state_scope_required")
	}
	if scope.RootTaskID != plan.RootTaskID || scope.PlanID != plan.ID {
		return fmt.Errorf("route_state_scope_mismatch")
	}
	return nil
}

func canonicalRoutePlan(plan ToolPlan) ([]byte, string, error) {
	if strings.TrimSpace(plan.RootTaskID) == "" || strings.TrimSpace(plan.ID) == "" {
		return nil, "", fmt.Errorf("route_state_plan_required")
	}
	if err := validateToolPlanArtifactDependencies(plan); err != nil {
		return nil, "", err
	}
	cloned := cloneRouteStatePlan(plan)
	encoded, err := json.Marshal(cloned)
	if err != nil {
		return nil, "", fmt.Errorf("canonicalize route plan: %w", err)
	}
	return encoded, SchemaDigest(encoded), nil
}

func cloneRouteStatePlan(plan ToolPlan) ToolPlan {
	clone := plan
	clone.Selections = make([]PlannedSelection, len(plan.Selections))
	for i, selection := range plan.Selections {
		clone.Selections[i] = clonePlannedSelection(selection)
	}
	clone.Unmet = append([]UnmetNeed(nil), plan.Unmet...)
	clone.Decisions = append([]ToolDecision(nil), plan.Decisions...)
	clone.Trace.Events = append([]TraceEvent(nil), plan.Trace.Events...)
	sort.Slice(clone.Selections, func(i, j int) bool { return clone.Selections[i].ID < clone.Selections[j].ID })
	sort.Slice(clone.Unmet, func(i, j int) bool {
		if clone.Unmet[i].NeedID == clone.Unmet[j].NeedID {
			return clone.Unmet[i].ReasonCode < clone.Unmet[j].ReasonCode
		}
		return clone.Unmet[i].NeedID < clone.Unmet[j].NeedID
	})
	return clone
}

func routeStateKey(scope InvocationScope) string {
	return SchemaDigest([]byte(strings.Join([]string{scope.RootTaskID, scope.PlanID, scope.SessionID, scope.TurnID, scope.PrincipalID}, "\x00")))
}

func routeLineageKey(scope InvocationScope) string {
	return SchemaDigest([]byte(strings.Join([]string{scope.RootTaskID, scope.SessionID, scope.PrincipalID}, "\x00")))
}

func routeRevisionRef(scope InvocationScope, planID, planDigest string, revision uint64) RouteRevisionRef {
	return RouteRevisionRef{
		RootTaskID: scope.RootTaskID, SessionID: scope.SessionID, PrincipalID: scope.PrincipalID,
		Revision: revision, PlanID: planID, PlanDigest: planDigest,
	}
}

func cloneRouteRevisionRef(value *RouteRevisionRef) *RouteRevisionRef {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneRouteAmendmentRef(value *RouteAmendmentRef) *RouteAmendmentRef {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func sameOptionalRouteAmendmentRef(a, b *RouteAmendmentRef) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.CommandID == b.CommandID && a.Digest == b.Digest && a.ParentRevision == b.ParentRevision && a.ParentFencingToken == b.ParentFencingToken
}

func validateRouteRevisionRef(ref RouteRevisionRef) error {
	if strings.TrimSpace(ref.RootTaskID) == "" || strings.TrimSpace(ref.SessionID) == "" || strings.TrimSpace(ref.PrincipalID) == "" || ref.Revision == 0 || strings.TrimSpace(ref.PlanID) == "" || strings.TrimSpace(ref.PlanDigest) == "" {
		return fmt.Errorf("route_revision_ref_required")
	}
	return nil
}

func sameRouteRevisionRef(a, b RouteRevisionRef) bool {
	return a == b
}

func sameOptionalRouteRevisionRef(a, b *RouteRevisionRef) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameRouteRevisionRef(*a, *b)
}

func validateRouteRevisionPublish(request RouteRevisionPublishRequest) ([]byte, string, error) {
	if err := validateRouteStateScope(request.Scope, request.Plan); err != nil {
		return nil, "", err
	}
	request.SnapshotDigest = strings.TrimSpace(request.SnapshotDigest)
	if request.SnapshotDigest == "" {
		return nil, "", fmt.Errorf("route_snapshot_digest_required")
	}
	if strings.TrimSpace(request.Plan.SnapshotDigest) != "" && request.SnapshotDigest != request.Plan.SnapshotDigest {
		return nil, "", fmt.Errorf("route_snapshot_digest_mismatch")
	}
	if request.ExpectedParent != nil && validateRouteRevisionRef(*request.ExpectedParent) != nil {
		return nil, "", fmt.Errorf("route_revision_parent_invalid")
	}
	if request.Amendment != nil {
		amendment := request.Amendment
		if strings.TrimSpace(amendment.CommandID) == "" || strings.TrimSpace(amendment.Digest) == "" || amendment.ParentRevision == 0 || amendment.ParentFencingToken == 0 {
			return nil, "", fmt.Errorf("route_amendment_invalid")
		}
		if request.ExpectedParent == nil || amendment.ParentRevision != request.ExpectedParent.Revision {
			return nil, "", fmt.Errorf("route_amendment_parent_mismatch")
		}
	}
	return canonicalRoutePlan(request.Plan)
}

func validateRouteMaterialization(materialization RouteMaterialization) error {
	if strings.TrimSpace(materialization.FunctionName) == "" || !validRenderedFunctionName(materialization.FunctionName) {
		return fmt.Errorf("route_state_function_invalid")
	}
	if materialization.State != RouteMaterializationExposed && materialization.State != RouteMaterializationRetired {
		return fmt.Errorf("route_state_materialization_state_invalid")
	}
	if strings.TrimSpace(materialization.Grant.Token) != materialization.FunctionName || strings.TrimSpace(materialization.Grant.Nonce) == "" || strings.TrimSpace(materialization.Grant.Signature) == "" {
		return fmt.Errorf("route_state_grant_invalid")
	}
	return nil
}

func routeMaterializationMatchesPlan(plan ToolPlan, scope InvocationScope, materialization RouteMaterialization) bool {
	if materialization.Grant.Scope != scope || materialization.Grant.Scope.PlanID != plan.ID || materialization.Grant.CatalogGeneration != plan.CatalogGeneration {
		return false
	}
	for _, selection := range plan.Selections {
		if selection.ID != materialization.Grant.SelectionID {
			continue
		}
		return selection.AdapterName == materialization.Grant.AdapterName &&
			selection.Provider.StableID() == materialization.Grant.ProviderBinding &&
			selection.FitProof.Digest == materialization.Grant.FitProofDigest &&
			parameterAuthorizationsEqual(selection.ParameterAuthorization, materialization.Grant.ParameterAuthorization)
	}
	return false
}

func cloneRouteState(state RouteState) RouteState {
	state.Plan = cloneRouteStatePlan(state.Plan)
	state.Revision = cloneRouteRevisionRef(state.Revision)
	state.ParentRevision = cloneRouteRevisionRef(state.ParentRevision)
	state.Amendment = cloneRouteAmendmentRef(state.Amendment)
	state.Materializations = append([]RouteMaterialization(nil), state.Materializations...)
	state.Completed = append([]RouteCompletedSelection(nil), state.Completed...)
	state.Confirmations = append([]RouteConfirmation(nil), state.Confirmations...)
	state.Artifacts = append([]RouteArtifactRef(nil), state.Artifacts...)
	for i := range state.Materializations {
		state.Materializations[i].Grant = cloneInvocationGrant(state.Materializations[i].Grant)
	}
	sort.Slice(state.Materializations, func(i, j int) bool {
		return state.Materializations[i].FunctionName < state.Materializations[j].FunctionName
	})
	sort.Slice(state.Completed, func(i, j int) bool { return state.Completed[i].SelectionID < state.Completed[j].SelectionID })
	sort.Slice(state.Confirmations, func(i, j int) bool { return state.Confirmations[i].Requirement < state.Confirmations[j].Requirement })
	sort.Slice(state.Artifacts, func(i, j int) bool {
		if state.Artifacts[i].CreatedAt.Equal(state.Artifacts[j].CreatedAt) {
			return state.Artifacts[i].ArtifactID < state.Artifacts[j].ArtifactID
		}
		return state.Artifacts[i].CreatedAt.Before(state.Artifacts[j].CreatedAt)
	})
	return state
}

func cloneInvocationGrant(grant InvocationGrant) InvocationGrant {
	grant.ParameterAuthorization.AllowedFields = append([]string(nil), grant.ParameterAuthorization.AllowedFields...)
	grant.ParameterAuthorization.AllowedTargets = append([]string(nil), grant.ParameterAuthorization.AllowedTargets...)
	grant.ParameterAuthorization.AllowedArtifactIDs = append([]string(nil), grant.ParameterAuthorization.AllowedArtifactIDs...)
	return grant
}

func sameInvocationGrant(left, right InvocationGrant) bool {
	return left.Token == right.Token &&
		left.AdapterName == right.AdapterName &&
		left.SelectionID == right.SelectionID &&
		left.ProviderBinding == right.ProviderBinding &&
		left.FitProofDigest == right.FitProofDigest &&
		parameterAuthorizationsEqual(left.ParameterAuthorization, right.ParameterAuthorization) &&
		left.CatalogGeneration == right.CatalogGeneration &&
		left.Scope == right.Scope &&
		left.IssuedAt.Equal(right.IssuedAt) &&
		left.ExpiresAt.Equal(right.ExpiresAt) &&
		left.Nonce == right.Nonce &&
		left.Signature == right.Signature
}

func selectionPurposeDigest(selection PlannedSelection) string {
	return SchemaDigest([]byte(strings.Join([]string{
		selection.NeedID, string(selection.FitProof.MatchedCapability), canonicalStringMap(selection.FitProof.QualifierBindings),
		selection.ParameterAuthorization.Digest, selection.ParameterAuthorization.CanonicalizerVer, strings.Join(selection.ParameterAuthorization.AllowedFields, "\x1f"),
		canonicalEffects(selection.Effects), canonicalArtifacts(selection.Consumes), canonicalArtifacts(selection.Produces), canonicalArtifactDependencies(selection.ArtifactDependencies),
	}, "\x00")))
}

func confirmationPurposeDigest(selection PlannedSelection) string {
	return SchemaDigest([]byte(strings.Join([]string{
		selection.NeedID, selection.ConfirmationID, selectionPurposeDigest(selection),
	}, "\x00")))
}

func confirmationMatchesPlan(confirmation RouteConfirmation, plan ToolPlan, now time.Time) bool {
	if confirmation.Requirement == "" || confirmation.PurposeDigest == "" || (confirmation.Authority != AuthorityRuntime && confirmation.Authority != AuthorityPolicy && confirmation.Authority != AuthorityChannel) {
		return false
	}
	if !confirmation.ValidUntil.IsZero() && now.After(confirmation.ValidUntil) {
		return false
	}
	for _, selection := range plan.Selections {
		if selection.ConfirmationID == confirmation.Requirement && confirmationPurposeDigest(selection) == confirmation.PurposeDigest {
			return true
		}
	}
	return false
}

func validatedRouteConfirmation(plan ToolPlan, scope InvocationScope, fact RoutingFact, now time.Time) (RouteConfirmation, error) {
	if fact.Authority != AuthorityRuntime && fact.Authority != AuthorityPolicy && fact.Authority != AuthorityChannel {
		return RouteConfirmation{}, fmt.Errorf("route_confirmation_authority_invalid")
	}
	if !strings.EqualFold(strings.TrimSpace(fact.Kind), "confirmation_granted") || strings.TrimSpace(fact.Attributes["root_task_id"]) != scope.RootTaskID {
		return RouteConfirmation{}, fmt.Errorf("route_confirmation_invalid")
	}
	if !fact.ValidUntil.IsZero() && now.After(fact.ValidUntil) {
		return RouteConfirmation{}, fmt.Errorf("route_confirmation_expired")
	}
	requirement := strings.TrimSpace(fact.Attributes["confirmation_requirement"])
	for _, selection := range plan.Selections {
		if selection.ConfirmationID == requirement {
			return RouteConfirmation{Requirement: requirement, PurposeDigest: confirmationPurposeDigest(selection), Authority: fact.Authority, ValidUntil: fact.ValidUntil.UTC(), GrantedAt: now.UTC()}, nil
		}
	}
	return RouteConfirmation{}, fmt.Errorf("route_confirmation_requirement_not_found")
}

func mergeRouteConfirmations(parent, child ToolPlan, values []RouteConfirmation, now time.Time) []RouteConfirmation {
	projected := make([]RouteConfirmation, 0, len(values))
	for _, value := range values {
		if confirmationMatchesPlan(value, parent, now) && confirmationMatchesPlan(value, child, now) {
			projected = append(projected, value)
		}
	}
	sort.Slice(projected, func(i, j int) bool { return projected[i].Requirement < projected[j].Requirement })
	return projected
}

func completedSelectionMatchesPlan(completed RouteCompletedSelection, plan ToolPlan) bool {
	for _, selection := range plan.Selections {
		if selection.ID == completed.SelectionID && selectionPurposeDigest(selection) == completed.PurposeDigest {
			return true
		}
	}
	return false
}

func mergeRouteCompletedSelections(parent, child ToolPlan, completed []RouteCompletedSelection, now time.Time) []RouteCompletedSelection {
	projected := make([]RouteCompletedSelection, 0, len(completed))
	for _, value := range completed {
		if completedSelectionMatchesPlan(value, parent) && completedSelectionMatchesPlan(value, child) {
			projected = append(projected, value)
		}
	}
	sort.Slice(projected, func(i, j int) bool { return projected[i].SelectionID < projected[j].SelectionID })
	return projected
}

func routeArtifactRefFromArtifact(ref ArtifactRef, producerPurpose string) RouteArtifactRef {
	return RouteArtifactRef{ArtifactID: ref.ID, Kind: ref.Kind, MIMEType: ref.MIMEType, IntegrityDigest: ref.IntegrityDigest, ProducerSelection: ref.ProducerSelection, ProducerPurposeDigest: producerPurpose, SourceScope: ref.Scope, CreatedAt: ref.CreatedAt.UTC()}
}

func (ref RouteArtifactRef) artifactRef() ArtifactRef {
	return ArtifactRef{ID: ref.ArtifactID, Kind: ref.Kind, MIMEType: ref.MIMEType, IntegrityDigest: ref.IntegrityDigest, ProducerSelection: ref.ProducerSelection, Scope: ref.SourceScope, CreatedAt: ref.CreatedAt}
}

func validateRouteArtifactRef(ref RouteArtifactRef) error {
	if err := ValidateArtifactScope(ref.SourceScope); err != nil {
		return err
	}
	if strings.TrimSpace(ref.ArtifactID) == "" || strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.MIMEType) == "" || strings.TrimSpace(ref.IntegrityDigest) == "" || strings.TrimSpace(ref.ProducerSelection) == "" || strings.TrimSpace(ref.ProducerPurposeDigest) == "" || ref.CreatedAt.IsZero() {
		return fmt.Errorf("route_artifact_ref_incomplete")
	}
	return nil
}

func routeArtifactMatchesPlan(ref RouteArtifactRef, plan ToolPlan) bool {
	if validateRouteArtifactRef(ref) != nil {
		return false
	}
	for _, selection := range plan.Selections {
		if selection.ID == ref.ProducerSelection && selectionPurposeDigest(selection) == ref.ProducerPurposeDigest {
			return true
		}
	}
	return false
}

func routeArtifactHasCurrentConsumer(ref RouteArtifactRef, plan ToolPlan) bool {
	produced := ArtifactContract{Kind: ref.Kind, MIMEType: ref.MIMEType, Required: true}
	for _, selection := range plan.Selections {
		for _, dependency := range selection.ArtifactDependencies {
			// A consumer that names only the kind (file deliver: document)
			// must still accept a more specific produced MIME (PDF). Exact
			// sameArtifactContract matching treats that as no consumer and
			// then get() reports route_state_corrupt after a successful
			// generate. Use the same wildcard rule as producesArtifact.
			if !producesArtifact([]ArtifactContract{produced}, dependency.Contract) {
				continue
			}
			if strings.TrimSpace(dependency.ProducerSelection) == ref.ProducerSelection || strings.TrimSpace(dependency.ArtifactID) == ref.ArtifactID {
				return true
			}
		}
	}
	return false
}

func routeArtifactProducerCompatible(ref RouteArtifactRef, plan ToolPlan) bool {
	for _, selection := range plan.Selections {
		if selection.ID == ref.ProducerSelection {
			return selectionPurposeDigest(selection) == ref.ProducerPurposeDigest
		}
	}
	// A child can consume a previously produced artifact without repeating its
	// producer selection. The source selection remains bound to the immutable
	// parent scope and purpose digest; a same-ID child producer, if present,
	// must still be purpose-compatible.
	return true
}

func routeArtifactUsableInPlan(ref RouteArtifactRef, plan ToolPlan) bool {
	return validateRouteArtifactRef(ref) == nil && validateToolPlanArtifactDependencies(plan) == nil && routeArtifactProducerCompatible(ref, plan) && routeArtifactHasCurrentConsumer(ref, plan)
}

func mergeRouteArtifactRefs(parent, child ToolPlan, completed []RouteCompletedSelection, refs []RouteArtifactRef) []RouteArtifactRef {
	completedPurposes := make(map[string]string, len(completed))
	for _, completed := range completed {
		if completedSelectionMatchesPlan(completed, parent) {
			completedPurposes[completed.SelectionID] = completed.PurposeDigest
		}
	}
	projected := make([]RouteArtifactRef, 0, len(refs))
	for _, ref := range refs {
		if completedPurposes[ref.ProducerSelection] != ref.ProducerPurposeDigest || !routeArtifactMatchesPlan(ref, parent) || !routeArtifactUsableInPlan(ref, child) {
			continue
		}
		projected = append(projected, ref)
	}
	sort.Slice(projected, func(i, j int) bool { return projected[i].ArtifactID < projected[j].ArtifactID })
	return projected
}

type memoryRouteStateStore struct {
	mu       sync.Mutex
	states   map[string]RouteState
	lineages map[string]RouteRevisionRef
	// fencing is the in-process monotonic token source mirroring the SQLite
	// fencing counter, so tests observe the same publish ordering contract.
	fencing uint64
}

// NewMemoryRouteStateStore is restricted to tests and explicit single-process
// development. Restartable hosts must use SQLiteRouteStateStore.
func NewMemoryRouteStateStore() RouteStateStore {
	return &memoryRouteStateStore{states: make(map[string]RouteState), lineages: make(map[string]RouteRevisionRef)}
}

func (s *memoryRouteStateStore) Open(scope InvocationScope, plan ToolPlan, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if err := validateRouteStateScope(scope, plan); err != nil {
		return RouteState{}, err
	}
	_, digest, err := canonicalRoutePlan(plan)
	if err != nil {
		return RouteState{}, err
	}
	key := routeStateKey(scope)
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.states[key]; ok {
		if current.Version != RouteStateVersion || current.PlanDigest != digest {
			return RouteState{}, fmt.Errorf("route_state_conflict")
		}
		return cloneRouteState(current), nil
	}
	state := RouteState{Version: RouteStateVersion, Scope: scope, Plan: cloneRouteStatePlan(plan), PlanDigest: digest, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	s.states[key] = state
	return cloneRouteState(state), nil
}

func (s *memoryRouteStateStore) PublishRevision(request RouteRevisionPublishRequest, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if request.Amendment != nil {
		return RouteState{}, fmt.Errorf("route_amendment_requires_coordinator")
	}
	_, digest, err := validateRouteRevisionPublish(request)
	if err != nil {
		return RouteState{}, err
	}
	key, lineageKey := routeStateKey(request.Scope), routeLineageKey(request.Scope)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.states[key]; ok {
		if existing.PlanDigest != digest || existing.SnapshotDigest != request.SnapshotDigest || existing.Revision == nil || !sameOptionalRouteAmendmentRef(existing.Amendment, request.Amendment) || (!sameOptionalRouteRevisionRef(existing.ParentRevision, request.ExpectedParent) && !sameOptionalRouteRevisionRef(existing.Revision, request.ExpectedParent)) {
			return RouteState{}, fmt.Errorf("route_state_conflict")
		}
		return cloneRouteState(existing), nil
	}
	parent, hasParent := s.lineages[lineageKey]
	if request.ExpectedParent == nil {
		if hasParent {
			return RouteState{}, fmt.Errorf("route_revision_parent_required")
		}
	} else if !hasParent || !sameRouteRevisionRef(parent, *request.ExpectedParent) {
		return RouteState{}, fmt.Errorf("route_revision_conflict")
	}
	revision := uint64(1)
	var projectedCompleted []RouteCompletedSelection
	var projectedConfirmations []RouteConfirmation
	var projectedArtifacts []RouteArtifactRef
	if hasParent {
		revision = parent.Revision + 1
		parentKey, parentState, ok := findMemoryRouteRevision(s.states, parent)
		if !ok || parentState.Revision == nil || !sameRouteRevisionRef(*parentState.Revision, parent) {
			return RouteState{}, fmt.Errorf("route_revision_corrupt")
		}
		for index := range parentState.Materializations {
			if parentState.Materializations[index].State == RouteMaterializationExposed {
				parentState.Materializations[index].State = RouteMaterializationRetired
				parentState.Materializations[index].UpdatedAt = now.UTC()
			}
		}
		parentState.UpdatedAt = now.UTC()
		s.states[parentKey] = parentState
		projectedCompleted = mergeRouteCompletedSelections(parentState.Plan, request.Plan, parentState.Completed, now)
		projectedConfirmations = mergeRouteConfirmations(parentState.Plan, request.Plan, parentState.Confirmations, now)
		projectedArtifacts = mergeRouteArtifactRefs(parentState.Plan, request.Plan, parentState.Completed, parentState.Artifacts)
	}
	ref := routeRevisionRef(request.Scope, request.Plan.ID, digest, revision)
	s.fencing++
	state := RouteState{Version: RouteStateVersion, Scope: request.Scope, Plan: cloneRouteStatePlan(request.Plan), PlanDigest: digest, Revision: &ref, ParentRevision: cloneRouteRevisionRef(request.ExpectedParent), SnapshotDigest: request.SnapshotDigest, Amendment: cloneRouteAmendmentRef(request.Amendment), FencingToken: s.fencing, Completed: projectedCompleted, Confirmations: projectedConfirmations, Artifacts: projectedArtifacts, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	s.states[key], s.lineages[lineageKey] = state, ref
	return cloneRouteState(state), nil
}

// findMemoryRouteRevision resolves the exact parent row without allowing the
// caller to substitute a turn. A small scan is sufficient for the in-memory
// test implementation; the SQLite implementation uses its lineage table.
func findMemoryRouteRevision(states map[string]RouteState, ref RouteRevisionRef) (string, RouteState, bool) {
	for key, state := range states {
		if state.Revision != nil && sameRouteRevisionRef(*state.Revision, ref) {
			return key, state, true
		}
	}
	return "", RouteState{}, false
}

func (s *memoryRouteStateStore) IsCurrent(scope InvocationScope) error {
	if s == nil {
		return fmt.Errorf("route state store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[routeStateKey(scope)]
	if !ok {
		return fmt.Errorf("route_state_not_found")
	}
	if state.Revision == nil { // Legacy Open-only state.
		if _, published := s.lineages[routeLineageKey(scope)]; published {
			return fmt.Errorf("route_revision_superseded")
		}
		return nil
	}
	current, ok := s.lineages[routeLineageKey(scope)]
	if !ok || !sameRouteRevisionRef(*state.Revision, current) {
		return fmt.Errorf("route_revision_superseded")
	}
	return nil
}

func (s *memoryRouteStateStore) CurrentRevision(scope InvocationScope) (RouteRevisionRef, error) {
	if s == nil {
		return RouteRevisionRef{}, fmt.Errorf("route state store is unavailable")
	}
	if strings.TrimSpace(scope.RootTaskID) == "" || strings.TrimSpace(scope.SessionID) == "" || strings.TrimSpace(scope.PrincipalID) == "" {
		return RouteRevisionRef{}, fmt.Errorf("route_state_scope_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.lineages[routeLineageKey(scope)]
	if !ok {
		return RouteRevisionRef{}, fmt.Errorf("route_revision_not_found")
	}
	return ref, nil
}

func (s *memoryRouteStateStore) PublishedPlan(scope InvocationScope) (ToolPlan, error) {
	if s == nil {
		return ToolPlan{}, fmt.Errorf("route state store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[routeStateKey(scope)]
	if !ok {
		return ToolPlan{}, fmt.Errorf("route_state_not_found")
	}
	if state.Revision != nil {
		current, ok := s.lineages[routeLineageKey(scope)]
		if !ok || !sameRouteRevisionRef(*state.Revision, current) {
			return ToolPlan{}, fmt.Errorf("route_revision_superseded")
		}
	}
	return cloneRouteStatePlan(state.Plan), nil
}

func (s *memoryRouteStateStore) RecordSelectionCompletion(scope InvocationScope, planID, selectionID string, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	selectionID = strings.TrimSpace(selectionID)
	if selectionID == "" {
		return RouteState{}, fmt.Errorf("route_state_selection_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := routeStateKey(scope)
	state, ok := s.states[key]
	if !ok || state.Plan.ID != planID {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	if state.Revision != nil {
		current, ok := s.lineages[routeLineageKey(scope)]
		if !ok || !sameRouteRevisionRef(*state.Revision, current) {
			return RouteState{}, fmt.Errorf("route_revision_superseded")
		}
	}
	for _, value := range state.Completed {
		if value.SelectionID == selectionID {
			return cloneRouteState(state), nil
		}
	}
	for _, selection := range state.Plan.Selections {
		if selection.ID == selectionID {
			state.Completed = append(state.Completed, RouteCompletedSelection{SelectionID: selectionID, PurposeDigest: selectionPurposeDigest(selection), CompletedAt: now.UTC()})
			state.UpdatedAt = now.UTC()
			s.states[key] = state
			return cloneRouteState(state), nil
		}
	}
	return RouteState{}, fmt.Errorf("route_state_selection_not_found")
}

func (s *memoryRouteStateStore) CompletedSelections(scope InvocationScope) (map[string]bool, error) {
	if s == nil {
		return nil, fmt.Errorf("route state store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[routeStateKey(scope)]
	if !ok {
		return nil, fmt.Errorf("route_state_not_found")
	}
	completed := make(map[string]bool, len(state.Completed))
	for _, value := range state.Completed {
		if completedSelectionMatchesPlan(value, state.Plan) {
			completed[value.SelectionID] = true
		}
	}
	return completed, nil
}

func (s *memoryRouteStateStore) RecordConfirmation(scope InvocationScope, planID string, fact RoutingFact, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := routeStateKey(scope)
	state, ok := s.states[key]
	if !ok || state.Plan.ID != planID {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	if state.Revision != nil {
		current, ok := s.lineages[routeLineageKey(scope)]
		if !ok || !sameRouteRevisionRef(*state.Revision, current) {
			return RouteState{}, fmt.Errorf("route_revision_superseded")
		}
	}
	confirmation, err := validatedRouteConfirmation(state.Plan, scope, fact, now)
	if err != nil {
		return RouteState{}, err
	}
	for _, existing := range state.Confirmations {
		if existing.Requirement == confirmation.Requirement {
			if existing.PurposeDigest == confirmation.PurposeDigest && existing.Authority == confirmation.Authority && existing.ValidUntil.Equal(confirmation.ValidUntil) {
				return cloneRouteState(state), nil
			}
			return RouteState{}, fmt.Errorf("route_confirmation_conflict")
		}
	}
	state.Confirmations = append(state.Confirmations, confirmation)
	state.UpdatedAt = now.UTC()
	s.states[key] = state
	return cloneRouteState(state), nil
}

func (s *memoryRouteStateStore) ConfirmedRequirements(scope InvocationScope, now time.Time) (map[string]bool, error) {
	if s == nil {
		return nil, fmt.Errorf("route state store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[routeStateKey(scope)]
	if !ok {
		return nil, fmt.Errorf("route_state_not_found")
	}
	confirmed := make(map[string]bool, len(state.Confirmations))
	for _, confirmation := range state.Confirmations {
		if confirmationMatchesPlan(confirmation, state.Plan, now) {
			confirmed[confirmation.Requirement] = true
		}
	}
	return confirmed, nil
}

func (s *memoryRouteStateStore) RecordArtifact(scope InvocationScope, planID string, ref ArtifactRef, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if ref.Scope != scope || scope.PlanID != planID {
		return RouteState{}, fmt.Errorf("route_artifact_scope_mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := routeStateKey(scope)
	state, ok := s.states[key]
	if !ok || state.Plan.ID != planID {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	if state.Revision != nil {
		current, ok := s.lineages[routeLineageKey(scope)]
		if !ok || !sameRouteRevisionRef(*state.Revision, current) {
			return RouteState{}, fmt.Errorf("route_revision_superseded")
		}
	}
	var purpose string
	for _, selection := range state.Plan.Selections {
		if selection.ID == ref.ProducerSelection {
			if !producesArtifact(selection.Produces, ArtifactContract{Kind: ref.Kind, MIMEType: ref.MIMEType, Required: true}) {
				return RouteState{}, fmt.Errorf("route_artifact_producer_contract_mismatch")
			}
			purpose = selectionPurposeDigest(selection)
			break
		}
	}
	if purpose == "" {
		return RouteState{}, fmt.Errorf("route_artifact_producer_not_found")
	}
	value := routeArtifactRefFromArtifact(ref, purpose)
	if err := validateRouteArtifactRef(value); err != nil {
		return RouteState{}, err
	}
	for _, existing := range state.Artifacts {
		if existing.ArtifactID == value.ArtifactID {
			if existing != value {
				return RouteState{}, fmt.Errorf("route_artifact_conflict")
			}
			return cloneRouteState(state), nil
		}
	}
	state.Artifacts = append(state.Artifacts, value)
	state.UpdatedAt = now.UTC()
	s.states[key] = state
	return cloneRouteState(state), nil
}

func (s *memoryRouteStateStore) ArtifactRefs(scope InvocationScope) ([]RouteArtifactRef, error) {
	if s == nil {
		return nil, fmt.Errorf("route state store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[routeStateKey(scope)]
	if !ok {
		return nil, fmt.Errorf("route_state_not_found")
	}
	refs := make([]RouteArtifactRef, 0, len(state.Artifacts))
	for _, ref := range state.Artifacts {
		if routeArtifactUsableInPlan(ref, state.Plan) {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func (s *memoryRouteStateStore) CurrentArtifactRefs(scope InvocationScope) ([]RouteArtifactRef, error) {
	if s == nil {
		return nil, fmt.Errorf("route state store is unavailable")
	}
	if strings.TrimSpace(scope.RootTaskID) == "" || strings.TrimSpace(scope.SessionID) == "" || strings.TrimSpace(scope.PrincipalID) == "" {
		return nil, fmt.Errorf("route_state_scope_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.lineages[routeLineageKey(scope)]
	if !ok {
		return nil, fmt.Errorf("route_revision_not_found")
	}
	_, state, ok := findMemoryRouteRevision(s.states, ref)
	if !ok {
		return nil, fmt.Errorf("route_revision_corrupt")
	}
	refs := make([]RouteArtifactRef, 0, len(state.Artifacts))
	for _, artifact := range state.Artifacts {
		if routeArtifactUsableInPlan(artifact, state.Plan) {
			refs = append(refs, artifact)
		}
	}
	return refs, nil
}

func reconcileRouteArtifacts(state RouteState, artifacts ArtifactStore, now time.Time) ([]ArtifactRef, error) {
	if artifacts == nil {
		return nil, fmt.Errorf("artifact store is unavailable")
	}
	completed := make(map[string]bool, len(state.Completed))
	for _, value := range state.Completed {
		if completedSelectionMatchesPlan(value, state.Plan) {
			completed[value.SelectionID] = true
		}
	}
	registered := make(map[string]bool, len(state.Artifacts))
	for _, value := range state.Artifacts {
		registered[value.ArtifactID] = true
	}
	refs := make([]ArtifactRef, 0)
	for _, selection := range state.Plan.Selections {
		if !completed[selection.ID] || len(selection.Produces) == 0 {
			continue
		}
		published, err := artifacts.PublishedArtifacts(state.Scope, selection.ID)
		if err != nil {
			return nil, err
		}
		for _, ref := range published {
			if ref.Scope != state.Scope || registered[ref.ID] || !producesArtifact(selection.Produces, ArtifactContract{Kind: ref.Kind, MIMEType: ref.MIMEType, Required: true}) {
				continue
			}
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func (s *memoryRouteStateStore) ReconcileArtifacts(scope InvocationScope, artifacts ArtifactStore, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	s.mu.Lock()
	state, ok := s.states[routeStateKey(scope)]
	s.mu.Unlock()
	if !ok {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	refs, err := reconcileRouteArtifacts(cloneRouteState(state), artifacts, now)
	if err != nil {
		return RouteState{}, err
	}
	for _, ref := range refs {
		if _, err := s.RecordArtifact(scope, state.Plan.ID, ref, now); err != nil {
			return RouteState{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRouteState(s.states[routeStateKey(scope)]), nil
}

func (s *memoryRouteStateStore) ReconcileCurrentArtifacts(scope InvocationScope, artifacts ArtifactStore, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	s.mu.Lock()
	ref, ok := s.lineages[routeLineageKey(scope)]
	key, state, found := findMemoryRouteRevision(s.states, ref)
	s.mu.Unlock()
	if !ok {
		return RouteState{}, fmt.Errorf("route_revision_not_found")
	}
	if !found {
		return RouteState{}, fmt.Errorf("route_revision_corrupt")
	}
	_ = key
	return s.ReconcileArtifacts(state.Scope, artifacts, now)
}

func (s *memoryRouteStateStore) RecordMaterialization(scope InvocationScope, planID string, materialization RouteMaterialization, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if err := validateRouteMaterialization(materialization); err != nil {
		return RouteState{}, err
	}
	key := routeStateKey(scope)
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[key]
	if !ok || state.Plan.ID != planID {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	if materialization.Grant.Scope != scope || materialization.Grant.Scope.PlanID != planID {
		return RouteState{}, fmt.Errorf("route_state_grant_scope_mismatch")
	}
	if !routeMaterializationMatchesPlan(state.Plan, scope, materialization) {
		return RouteState{}, fmt.Errorf("route_state_grant_binding_mismatch")
	}
	for _, existing := range state.Materializations {
		if existing.FunctionName == materialization.FunctionName {
			if sameInvocationGrant(existing.Grant, materialization.Grant) && existing.State == materialization.State {
				return cloneRouteState(state), nil
			}
			return RouteState{}, fmt.Errorf("route_state_materialization_conflict")
		}
	}
	materialization.CreatedAt, materialization.UpdatedAt = now.UTC(), now.UTC()
	state.Materializations = append(state.Materializations, materialization)
	state.UpdatedAt = now.UTC()
	s.states[key] = state
	return cloneRouteState(state), nil
}

func (s *memoryRouteStateStore) RetireMaterialization(scope InvocationScope, planID, functionName string, now time.Time) (RouteState, error) {
	if s == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	key := routeStateKey(scope)
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[key]
	if !ok || state.Plan.ID != planID {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	for i := range state.Materializations {
		if state.Materializations[i].FunctionName != functionName {
			continue
		}
		if state.Materializations[i].State == RouteMaterializationRetired {
			return cloneRouteState(state), nil
		}
		state.Materializations[i].State, state.Materializations[i].UpdatedAt = RouteMaterializationRetired, now.UTC()
		state.UpdatedAt = now.UTC()
		s.states[key] = state
		return cloneRouteState(state), nil
	}
	return RouteState{}, fmt.Errorf("route_state_materialization_not_found")
}

// SQLiteRouteStateStore is the durable recovery owner for immutable plans and
// opaque adapter mappings. It intentionally uses one connection so a local
// process sees compare-and-create and transition order consistently.
type SQLiteRouteStateStore struct{ db *sql.DB }

func NewSQLiteRouteStateStore(dbPath string) (*SQLiteRouteStateStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("route state store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create route state store directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteRouteStateStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteRouteStateStore) init() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("route state store is unavailable")
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS semantic_route_states (
			route_key TEXT PRIMARY KEY, version TEXT NOT NULL, tenant_id TEXT NOT NULL DEFAULT '', root_task_id TEXT NOT NULL, plan_id TEXT NOT NULL,
			session_id TEXT NOT NULL, turn_id TEXT NOT NULL, principal_id TEXT NOT NULL,
			plan_json BLOB NOT NULL, plan_digest TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS semantic_route_materializations (
			route_key TEXT NOT NULL, function_name TEXT NOT NULL, grant_json BLOB NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('exposed','retired')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
			PRIMARY KEY(route_key, function_name),
			FOREIGN KEY(route_key) REFERENCES semantic_route_states(route_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_route_states_scope ON semantic_route_states(root_task_id, plan_id, session_id, turn_id, principal_id)`,
		`CREATE TABLE IF NOT EXISTS semantic_route_lineages (
			lineage_key TEXT PRIMARY KEY, root_task_id TEXT NOT NULL, session_id TEXT NOT NULL, principal_id TEXT NOT NULL,
			current_route_key TEXT NOT NULL, current_revision INTEGER NOT NULL, current_plan_id TEXT NOT NULL,
			current_plan_digest TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS semantic_route_revisions (
			route_key TEXT PRIMARY KEY, lineage_key TEXT NOT NULL, revision INTEGER NOT NULL,
			parent_route_key TEXT NOT NULL DEFAULT '', snapshot_digest TEXT NOT NULL,
			UNIQUE(lineage_key, revision), FOREIGN KEY(route_key) REFERENCES semantic_route_states(route_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_route_revisions_lineage ON semantic_route_revisions(lineage_key, revision)`,
		`CREATE TABLE IF NOT EXISTS semantic_route_cancellations (
			route_key TEXT PRIMARY KEY, cancelled_at TEXT NOT NULL,
			FOREIGN KEY(route_key) REFERENCES semantic_route_states(route_key)
		)`,
		`CREATE TABLE IF NOT EXISTS semantic_route_completed_selections (
			route_key TEXT NOT NULL, selection_id TEXT NOT NULL, purpose_digest TEXT NOT NULL, completed_at TEXT NOT NULL,
			PRIMARY KEY(route_key, selection_id), FOREIGN KEY(route_key) REFERENCES semantic_route_states(route_key)
		)`,
		`CREATE TABLE IF NOT EXISTS semantic_route_confirmations (
			route_key TEXT NOT NULL, requirement TEXT NOT NULL, purpose_digest TEXT NOT NULL,
			authority TEXT NOT NULL, valid_until TEXT NOT NULL DEFAULT '', granted_at TEXT NOT NULL,
			PRIMARY KEY(route_key, requirement), FOREIGN KEY(route_key) REFERENCES semantic_route_states(route_key)
		)`,
		`CREATE TABLE IF NOT EXISTS semantic_route_artifacts (
			route_key TEXT NOT NULL, artifact_id TEXT NOT NULL, kind TEXT NOT NULL, mime_type TEXT NOT NULL,
			integrity_digest TEXT NOT NULL, producer_selection TEXT NOT NULL, producer_purpose_digest TEXT NOT NULL,
			source_root_task_id TEXT NOT NULL, source_plan_id TEXT NOT NULL, source_session_id TEXT NOT NULL,
			source_turn_id TEXT NOT NULL, source_principal_id TEXT NOT NULL, created_at TEXT NOT NULL,
			PRIMARY KEY(route_key, artifact_id), FOREIGN KEY(route_key) REFERENCES semantic_route_states(route_key)
		)`,
		`CREATE TABLE IF NOT EXISTS semantic_route_amendments (
			route_key TEXT PRIMARY KEY, command_id TEXT NOT NULL, digest TEXT NOT NULL,
			parent_revision INTEGER NOT NULL, parent_fencing_token INTEGER NOT NULL,
			FOREIGN KEY(route_key) REFERENCES semantic_route_states(route_key)
		)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	// Fencing token columns are additive migrations; existing databases keep
	// token 0 for pre-fencing rows, which the outbox predicates treat as
	// legacy/unfenced rather than failing.
	for _, statement := range []string{
		`ALTER TABLE semantic_route_states ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE semantic_route_revisions ADD COLUMN fencing_token INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE semantic_route_lineages ADD COLUMN fencing_token INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := s.db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_semantic_route_states_tenant_scope ON semantic_route_states(tenant_id, root_task_id, session_id, principal_id)`); err != nil {
		return err
	}
	return initOutboxFencing(s.db)
}

func (s *SQLiteRouteStateStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteRouteStateStore) Open(scope InvocationScope, plan ToolPlan, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if err := validateRouteStateScope(scope, plan); err != nil {
		return RouteState{}, err
	}
	encoded, digest, err := canonicalRoutePlan(plan)
	if err != nil {
		return RouteState{}, err
	}
	key := routeStateKey(scope)
	result, err := s.db.Exec(`INSERT OR IGNORE INTO semantic_route_states(route_key, version, root_task_id, plan_id, session_id, turn_id, principal_id, plan_json, plan_digest, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, key, RouteStateVersion, scope.RootTaskID, scope.PlanID, scope.SessionID, scope.TurnID, scope.PrincipalID, encoded, digest, routeStateTime(now), routeStateTime(now))
	if err != nil {
		return RouteState{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return RouteState{}, err
	}
	state, err := s.get(key)
	if err != nil {
		return RouteState{}, err
	}
	if changed == 0 && (state.Version != RouteStateVersion || state.PlanDigest != digest) {
		return RouteState{}, fmt.Errorf("route_state_conflict")
	}
	return state, nil
}

func (s *SQLiteRouteStateStore) PublishRevision(request RouteRevisionPublishRequest, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if request.Amendment != nil {
		return RouteState{}, fmt.Errorf("route_amendment_requires_coordinator")
	}
	encoded, digest, err := validateRouteRevisionPublish(request)
	if err != nil {
		return RouteState{}, err
	}
	routeKey, lineageKey := routeStateKey(request.Scope), routeLineageKey(request.Scope)
	tx, err := s.db.Begin()
	if err != nil {
		return RouteState{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingDigest, existingSnapshot string
	err = tx.QueryRow(`SELECT rs.plan_digest, rr.snapshot_digest FROM semantic_route_states rs JOIN semantic_route_revisions rr ON rr.route_key = rs.route_key WHERE rs.route_key = ?`, routeKey).Scan(&existingDigest, &existingSnapshot)
	if err == nil {
		if existingDigest != digest || existingSnapshot != request.SnapshotDigest {
			return RouteState{}, fmt.Errorf("route_state_conflict")
		}
		if err := tx.Commit(); err != nil {
			return RouteState{}, err
		}
		state, err := s.get(routeKey)
		if err != nil {
			return RouteState{}, err
		}
		if !sameOptionalRouteRevisionRef(state.ParentRevision, request.ExpectedParent) && !sameOptionalRouteRevisionRef(state.Revision, request.ExpectedParent) {
			return RouteState{}, fmt.Errorf("route_revision_conflict")
		}
		return state, nil
	}
	if err != sql.ErrNoRows {
		return RouteState{}, err
	}

	var parentRouteKey, parentPlanID, parentDigest string
	var parentRevision uint64
	lineageErr := tx.QueryRow(`SELECT current_route_key, current_revision, current_plan_id, current_plan_digest FROM semantic_route_lineages WHERE lineage_key = ?`, lineageKey).Scan(&parentRouteKey, &parentRevision, &parentPlanID, &parentDigest)
	if request.ExpectedParent == nil {
		if lineageErr == nil {
			return RouteState{}, fmt.Errorf("route_revision_parent_required")
		}
		if lineageErr != sql.ErrNoRows {
			return RouteState{}, lineageErr
		}
		parentRevision = 0
	} else {
		if lineageErr != nil {
			if lineageErr == sql.ErrNoRows {
				return RouteState{}, fmt.Errorf("route_revision_conflict")
			}
			return RouteState{}, lineageErr
		}
		current := RouteRevisionRef{RootTaskID: request.Scope.RootTaskID, SessionID: request.Scope.SessionID, PrincipalID: request.Scope.PrincipalID, Revision: parentRevision, PlanID: parentPlanID, PlanDigest: parentDigest}
		if !sameRouteRevisionRef(current, *request.ExpectedParent) {
			return RouteState{}, fmt.Errorf("route_revision_conflict")
		}
		result, err := tx.Exec(`UPDATE semantic_route_materializations SET state = 'retired', updated_at = ? WHERE route_key = ? AND state = 'exposed'`, routeStateTime(now), parentRouteKey)
		if err != nil {
			return RouteState{}, err
		}
		if _, err := result.RowsAffected(); err != nil {
			return RouteState{}, err
		}
		if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at = ? WHERE route_key = ?`, routeStateTime(now), parentRouteKey); err != nil {
			return RouteState{}, err
		}
	}

	if _, err := tx.Exec(`INSERT INTO semantic_route_states(route_key, version, root_task_id, plan_id, session_id, turn_id, principal_id, plan_json, plan_digest, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, routeKey, RouteStateVersion, request.Scope.RootTaskID, request.Scope.PlanID, request.Scope.SessionID, request.Scope.TurnID, request.Scope.PrincipalID, encoded, digest, routeStateTime(now), routeStateTime(now)); err != nil {
		return RouteState{}, err
	}
	// Allocate the fencing token inside the publish transaction so the token
	// order is linearizable with the lineage advance it authorizes.
	fencingToken, err := nextOutboxFencingToken(tx)
	if err != nil {
		return RouteState{}, err
	}
	revision := parentRevision + 1
	if _, err := tx.Exec(`INSERT INTO semantic_route_revisions(route_key, lineage_key, revision, parent_route_key, snapshot_digest, fencing_token) VALUES (?, ?, ?, ?, ?, ?)`, routeKey, lineageKey, revision, parentRouteKey, request.SnapshotDigest, fencingToken); err != nil {
		return RouteState{}, err
	}
	if parentRouteKey != "" {
		rows, err := tx.Query(`SELECT selection_id, purpose_digest, completed_at FROM semantic_route_completed_selections WHERE route_key = ?`, parentRouteKey)
		if err != nil {
			return RouteState{}, err
		}
		childPurposes := make(map[string]string, len(request.Plan.Selections))
		for _, selection := range request.Plan.Selections {
			childPurposes[selection.ID] = selectionPurposeDigest(selection)
		}
		for rows.Next() {
			var selectionID, purposeDigest, completedAt string
			if err := rows.Scan(&selectionID, &purposeDigest, &completedAt); err != nil {
				_ = rows.Close()
				return RouteState{}, err
			}
			if childPurposes[selectionID] != purposeDigest {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO semantic_route_completed_selections(route_key, selection_id, purpose_digest, completed_at) VALUES (?, ?, ?, ?)`, routeKey, selectionID, purposeDigest, completedAt); err != nil {
				_ = rows.Close()
				return RouteState{}, err
			}
		}
		if err := rows.Close(); err != nil {
			return RouteState{}, err
		}
		confirmationRows, err := tx.Query(`SELECT requirement, purpose_digest, authority, valid_until, granted_at FROM semantic_route_confirmations WHERE route_key = ?`, parentRouteKey)
		if err != nil {
			return RouteState{}, err
		}
		childConfirmations := make(map[string]string, len(request.Plan.Selections))
		for _, selection := range request.Plan.Selections {
			if selection.ConfirmationID != "" {
				childConfirmations[selection.ConfirmationID] = confirmationPurposeDigest(selection)
			}
		}
		for confirmationRows.Next() {
			var requirement, purposeDigest, authority, validUntil, grantedAt string
			if err := confirmationRows.Scan(&requirement, &purposeDigest, &authority, &validUntil, &grantedAt); err != nil {
				_ = confirmationRows.Close()
				return RouteState{}, err
			}
			if childConfirmations[requirement] != purposeDigest {
				continue
			}
			if validUntil != "" {
				expires, parseErr := time.Parse(time.RFC3339Nano, validUntil)
				if parseErr != nil || now.After(expires) {
					continue
				}
			}
			if _, err := tx.Exec(`INSERT INTO semantic_route_confirmations(route_key, requirement, purpose_digest, authority, valid_until, granted_at) VALUES (?, ?, ?, ?, ?, ?)`, routeKey, requirement, purposeDigest, authority, validUntil, grantedAt); err != nil {
				_ = confirmationRows.Close()
				return RouteState{}, err
			}
		}
		if err := confirmationRows.Close(); err != nil {
			return RouteState{}, err
		}
		artifactRows, err := tx.Query(`SELECT artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at FROM semantic_route_artifacts WHERE route_key = ?`, parentRouteKey)
		if err != nil {
			return RouteState{}, err
		}
		parentArtifacts := make([]RouteArtifactRef, 0)
		for artifactRows.Next() {
			var value RouteArtifactRef
			var createdAt string
			if err := artifactRows.Scan(&value.ArtifactID, &value.Kind, &value.MIMEType, &value.IntegrityDigest, &value.ProducerSelection, &value.ProducerPurposeDigest, &value.SourceScope.RootTaskID, &value.SourceScope.PlanID, &value.SourceScope.SessionID, &value.SourceScope.TurnID, &value.SourceScope.PrincipalID, &createdAt); err != nil {
				_ = artifactRows.Close()
				return RouteState{}, err
			}
			value.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
			if err := validateRouteArtifactRef(value); err != nil {
				_ = artifactRows.Close()
				return RouteState{}, fmt.Errorf("route_state_corrupt")
			}
			parentArtifacts = append(parentArtifacts, value)
		}
		if err := artifactRows.Close(); err != nil {
			return RouteState{}, err
		}
		parentPlanJSON := []byte(nil)
		if err := tx.QueryRow(`SELECT plan_json FROM semantic_route_states WHERE route_key = ?`, parentRouteKey).Scan(&parentPlanJSON); err != nil {
			return RouteState{}, err
		}
		var parentPlan ToolPlan
		if json.Unmarshal(parentPlanJSON, &parentPlan) != nil {
			return RouteState{}, fmt.Errorf("route_state_corrupt")
		}
		parentCompletedRows, err := tx.Query(`SELECT selection_id, purpose_digest, completed_at FROM semantic_route_completed_selections WHERE route_key = ?`, parentRouteKey)
		if err != nil {
			return RouteState{}, err
		}
		parentCompleted := make([]RouteCompletedSelection, 0)
		for parentCompletedRows.Next() {
			var completed RouteCompletedSelection
			var completedAt string
			if err := parentCompletedRows.Scan(&completed.SelectionID, &completed.PurposeDigest, &completedAt); err != nil {
				_ = parentCompletedRows.Close()
				return RouteState{}, err
			}
			completed.CompletedAt, _ = time.Parse(time.RFC3339Nano, completedAt)
			parentCompleted = append(parentCompleted, completed)
		}
		if err := parentCompletedRows.Close(); err != nil {
			return RouteState{}, err
		}
		for _, value := range mergeRouteArtifactRefs(parentPlan, request.Plan, parentCompleted, parentArtifacts) {
			if _, err := tx.Exec(`INSERT INTO semantic_route_artifacts(route_key, artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, routeKey, value.ArtifactID, value.Kind, value.MIMEType, value.IntegrityDigest, value.ProducerSelection, value.ProducerPurposeDigest, value.SourceScope.RootTaskID, value.SourceScope.PlanID, value.SourceScope.SessionID, value.SourceScope.TurnID, value.SourceScope.PrincipalID, routeStateTime(value.CreatedAt)); err != nil {
				return RouteState{}, err
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO semantic_route_lineages(lineage_key, root_task_id, session_id, principal_id, current_route_key, current_revision, current_plan_id, current_plan_digest, fencing_token, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(lineage_key) DO UPDATE SET current_route_key = excluded.current_route_key, current_revision = excluded.current_revision, current_plan_id = excluded.current_plan_id, current_plan_digest = excluded.current_plan_digest, fencing_token = excluded.fencing_token, updated_at = excluded.updated_at`, lineageKey, request.Scope.RootTaskID, request.Scope.SessionID, request.Scope.PrincipalID, routeKey, revision, request.Plan.ID, digest, fencingToken, routeStateTime(now)); err != nil {
		return RouteState{}, err
	}
	if err := tx.Commit(); err != nil {
		return RouteState{}, err
	}
	return s.get(routeKey)
}

func (s *SQLiteRouteStateStore) IsCurrent(scope InvocationScope) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("route state store is unavailable")
	}
	return routeRevisionIsCurrent(s.db, scope)
}

// routeRevisionIsCurrent runs the supersession check against either the store's
// database or an open transaction. A caller that already holds a transaction
// must use the transaction: the store limits itself to one connection, so a
// database-level read while a transaction is open would wait on itself.
func routeRevisionIsCurrent(q routeStateRowQuerier, scope InvocationScope) error {
	key := routeStateKey(scope)
	var cancelled int
	if err := q.QueryRow(`SELECT 1 FROM semantic_route_cancellations WHERE route_key = ?`, key).Scan(&cancelled); err == nil {
		return fmt.Errorf("route_revision_cancelled")
	} else if err != sql.ErrNoRows {
		return err
	}
	var revisionRouteKey, currentRouteKey string
	err := q.QueryRow(`SELECT rr.route_key, rl.current_route_key FROM semantic_route_revisions rr JOIN semantic_route_lineages rl ON rl.lineage_key = rr.lineage_key WHERE rr.route_key = ?`, key).Scan(&revisionRouteKey, &currentRouteKey)
	if err == sql.ErrNoRows {
		var exists int
		if err := q.QueryRow(`SELECT 1 FROM semantic_route_states WHERE route_key = ?`, key).Scan(&exists); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("route_state_not_found")
			}
			return err
		}
		var currentRouteKey string
		err = q.QueryRow(`SELECT current_route_key FROM semantic_route_lineages WHERE lineage_key = ?`, routeLineageKey(scope)).Scan(&currentRouteKey)
		if err == sql.ErrNoRows {
			return nil // Legacy Open-only state without a published lineage.
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("route_revision_superseded")
	}
	if err != nil {
		return err
	}
	if revisionRouteKey != currentRouteKey {
		return fmt.Errorf("route_revision_superseded")
	}
	return nil
}

func (s *SQLiteRouteStateStore) CurrentRevision(scope InvocationScope) (RouteRevisionRef, error) {
	if s == nil || s.db == nil {
		return RouteRevisionRef{}, fmt.Errorf("route state store is unavailable")
	}
	if strings.TrimSpace(scope.RootTaskID) == "" || strings.TrimSpace(scope.SessionID) == "" || strings.TrimSpace(scope.PrincipalID) == "" {
		return RouteRevisionRef{}, fmt.Errorf("route_state_scope_required")
	}
	ref := RouteRevisionRef{RootTaskID: scope.RootTaskID, SessionID: scope.SessionID, PrincipalID: scope.PrincipalID}
	err := s.db.QueryRow(`SELECT current_revision, current_plan_id, current_plan_digest FROM semantic_route_lineages WHERE lineage_key = ?`, routeLineageKey(scope)).Scan(&ref.Revision, &ref.PlanID, &ref.PlanDigest)
	if err == sql.ErrNoRows {
		return RouteRevisionRef{}, fmt.Errorf("route_revision_not_found")
	}
	if err != nil {
		return RouteRevisionRef{}, err
	}
	if err := validateRouteRevisionRef(ref); err != nil {
		return RouteRevisionRef{}, fmt.Errorf("route_revision_corrupt")
	}
	return ref, nil
}

func (s *SQLiteRouteStateStore) PublishedPlan(scope InvocationScope) (ToolPlan, error) {
	if s == nil || s.db == nil {
		return ToolPlan{}, fmt.Errorf("route state store is unavailable")
	}
	if err := s.IsCurrent(scope); err != nil {
		return ToolPlan{}, err
	}
	state, err := s.get(routeStateKey(scope))
	if err == sql.ErrNoRows {
		return ToolPlan{}, fmt.Errorf("route_state_not_found")
	}
	if err != nil {
		return ToolPlan{}, err
	}
	return cloneRouteStatePlan(state.Plan), nil
}

func (s *SQLiteRouteStateStore) RecordSelectionCompletion(scope InvocationScope, planID, selectionID string, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	selectionID = strings.TrimSpace(selectionID)
	if selectionID == "" {
		return RouteState{}, fmt.Errorf("route_state_selection_required")
	}
	key := routeStateKey(scope)
	state, err := s.get(key)
	if err != nil {
		if err == sql.ErrNoRows {
			return RouteState{}, fmt.Errorf("route_state_not_found")
		}
		return RouteState{}, err
	}
	if state.Plan.ID != planID {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	if err := s.IsCurrent(scope); err != nil {
		return RouteState{}, err
	}
	var purpose string
	for _, selection := range state.Plan.Selections {
		if selection.ID == selectionID {
			purpose = selectionPurposeDigest(selection)
			break
		}
	}
	if purpose == "" {
		return RouteState{}, fmt.Errorf("route_state_selection_not_found")
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO semantic_route_completed_selections(route_key, selection_id, purpose_digest, completed_at) VALUES (?, ?, ?, ?)`, key, selectionID, purpose, routeStateTime(now))
	if err != nil {
		return RouteState{}, err
	}
	if _, err := s.db.Exec(`UPDATE semantic_route_states SET updated_at = ? WHERE route_key = ?`, routeStateTime(now), key); err != nil {
		return RouteState{}, err
	}
	return s.get(key)
}

// routeStateRowQuerier is satisfied by both *sql.DB and *sql.Tx so the checks
// guarding a route completion can run inside a caller's transaction.
type routeStateRowQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// recordSelectionCompletionTx applies the same admission checks and writes as
// RecordSelectionCompletion inside an already-open transaction, so a caller can
// commit the completion together with the execution state it projects. It
// returns no RouteState: reloading one before the commit would report a state
// that is not yet durable.
func (s *SQLiteRouteStateStore) recordSelectionCompletionTx(tx *sql.Tx, scope InvocationScope, planID, selectionID string, now time.Time) error {
	if s == nil || tx == nil {
		return fmt.Errorf("route state store is unavailable")
	}
	selectionID = strings.TrimSpace(selectionID)
	if selectionID == "" {
		return fmt.Errorf("route_state_selection_required")
	}
	key := routeStateKey(scope)
	var storedPlanID string
	var planJSON []byte
	err := tx.QueryRow(`SELECT plan_id, plan_json FROM semantic_route_states WHERE route_key = ?`, key).Scan(&storedPlanID, &planJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("route_state_not_found")
	}
	if err != nil {
		return err
	}
	if storedPlanID != planID {
		return fmt.Errorf("route_state_not_found")
	}
	if err := routeRevisionIsCurrent(tx, scope); err != nil {
		return err
	}
	// The purpose digest comes from the published plan rather than from the
	// caller's copy, so a completion can only ever cite the purpose the durable
	// revision actually recorded for that selection.
	var plan ToolPlan
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		return fmt.Errorf("route_state_corrupt")
	}
	purpose := ""
	for _, selection := range plan.Selections {
		if selection.ID == selectionID {
			purpose = selectionPurposeDigest(selection)
			break
		}
	}
	if purpose == "" {
		return fmt.Errorf("route_state_selection_not_found")
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO semantic_route_completed_selections(route_key, selection_id, purpose_digest, completed_at) VALUES (?, ?, ?, ?)`, key, selectionID, purpose, routeStateTime(now)); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at = ? WHERE route_key = ?`, routeStateTime(now), key); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteRouteStateStore) CompletedSelections(scope InvocationScope) (map[string]bool, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("route state store is unavailable")
	}
	state, err := s.get(routeStateKey(scope))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("route_state_not_found")
		}
		return nil, err
	}
	completed := make(map[string]bool, len(state.Completed))
	for _, value := range state.Completed {
		if completedSelectionMatchesPlan(value, state.Plan) {
			completed[value.SelectionID] = true
		}
	}
	return completed, nil
}

func (s *SQLiteRouteStateStore) RecordConfirmation(scope InvocationScope, planID string, fact RoutingFact, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	key := routeStateKey(scope)
	state, err := s.get(key)
	if err != nil {
		if err == sql.ErrNoRows {
			return RouteState{}, fmt.Errorf("route_state_not_found")
		}
		return RouteState{}, err
	}
	if state.Plan.ID != planID {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	if err := s.IsCurrent(scope); err != nil {
		return RouteState{}, err
	}
	confirmation, err := validatedRouteConfirmation(state.Plan, scope, fact, now)
	if err != nil {
		return RouteState{}, err
	}
	validUntil := ""
	if !confirmation.ValidUntil.IsZero() {
		validUntil = routeStateTime(confirmation.ValidUntil)
	}
	result, err := s.db.Exec(`INSERT OR IGNORE INTO semantic_route_confirmations(route_key, requirement, purpose_digest, authority, valid_until, granted_at) VALUES (?, ?, ?, ?, ?, ?)`, key, confirmation.Requirement, confirmation.PurposeDigest, confirmation.Authority, validUntil, routeStateTime(confirmation.GrantedAt))
	if err != nil {
		return RouteState{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return RouteState{}, err
	}
	if changed == 0 {
		var storedPurpose, storedAuthority, storedUntil string
		if err := s.db.QueryRow(`SELECT purpose_digest, authority, valid_until FROM semantic_route_confirmations WHERE route_key = ? AND requirement = ?`, key, confirmation.Requirement).Scan(&storedPurpose, &storedAuthority, &storedUntil); err != nil {
			return RouteState{}, err
		}
		if storedPurpose != confirmation.PurposeDigest || storedAuthority != string(confirmation.Authority) || storedUntil != validUntil {
			return RouteState{}, fmt.Errorf("route_confirmation_conflict")
		}
	}
	if _, err := s.db.Exec(`UPDATE semantic_route_states SET updated_at = ? WHERE route_key = ?`, routeStateTime(now), key); err != nil {
		return RouteState{}, err
	}
	return s.get(key)
}

func (s *SQLiteRouteStateStore) ConfirmedRequirements(scope InvocationScope, now time.Time) (map[string]bool, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("route state store is unavailable")
	}
	state, err := s.get(routeStateKey(scope))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("route_state_not_found")
		}
		return nil, err
	}
	confirmed := make(map[string]bool, len(state.Confirmations))
	for _, confirmation := range state.Confirmations {
		if confirmationMatchesPlan(confirmation, state.Plan, now) {
			confirmed[confirmation.Requirement] = true
		}
	}
	return confirmed, nil
}

func (s *SQLiteRouteStateStore) RecordArtifact(scope InvocationScope, planID string, ref ArtifactRef, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if ref.Scope != scope || scope.PlanID != planID {
		return RouteState{}, fmt.Errorf("route_artifact_scope_mismatch")
	}
	key := routeStateKey(scope)
	state, err := s.get(key)
	if err != nil {
		if err == sql.ErrNoRows {
			return RouteState{}, fmt.Errorf("route_state_not_found")
		}
		return RouteState{}, err
	}
	if state.Plan.ID != planID {
		return RouteState{}, fmt.Errorf("route_state_not_found")
	}
	if err := s.IsCurrent(scope); err != nil {
		return RouteState{}, err
	}
	var purpose string
	for _, selection := range state.Plan.Selections {
		if selection.ID == ref.ProducerSelection {
			if !producesArtifact(selection.Produces, ArtifactContract{Kind: ref.Kind, MIMEType: ref.MIMEType, Required: true}) {
				return RouteState{}, fmt.Errorf("route_artifact_producer_contract_mismatch")
			}
			purpose = selectionPurposeDigest(selection)
			break
		}
	}
	if purpose == "" {
		return RouteState{}, fmt.Errorf("route_artifact_producer_not_found")
	}
	value := routeArtifactRefFromArtifact(ref, purpose)
	if err := validateRouteArtifactRef(value); err != nil {
		return RouteState{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return RouteState{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`INSERT OR IGNORE INTO semantic_route_artifacts(route_key, artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, key, value.ArtifactID, value.Kind, value.MIMEType, value.IntegrityDigest, value.ProducerSelection, value.ProducerPurposeDigest, value.SourceScope.RootTaskID, value.SourceScope.PlanID, value.SourceScope.SessionID, value.SourceScope.TurnID, value.SourceScope.PrincipalID, routeStateTime(value.CreatedAt))
	if err != nil {
		return RouteState{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return RouteState{}, err
	}
	if changed == 0 {
		var existing RouteArtifactRef
		var created string
		if err := tx.QueryRow(`SELECT artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at FROM semantic_route_artifacts WHERE route_key = ? AND artifact_id = ?`, key, value.ArtifactID).Scan(&existing.ArtifactID, &existing.Kind, &existing.MIMEType, &existing.IntegrityDigest, &existing.ProducerSelection, &existing.ProducerPurposeDigest, &existing.SourceScope.RootTaskID, &existing.SourceScope.PlanID, &existing.SourceScope.SessionID, &existing.SourceScope.TurnID, &existing.SourceScope.PrincipalID, &created); err != nil {
			return RouteState{}, err
		}
		existing.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if existing != value {
			return RouteState{}, fmt.Errorf("route_artifact_conflict")
		}
	}
	if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at = ? WHERE route_key = ?`, routeStateTime(now), key); err != nil {
		return RouteState{}, err
	}
	if err := tx.Commit(); err != nil {
		return RouteState{}, err
	}
	return s.get(key)
}

func (s *SQLiteRouteStateStore) ArtifactRefs(scope InvocationScope) ([]RouteArtifactRef, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("route state store is unavailable")
	}
	state, err := s.get(routeStateKey(scope))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("route_state_not_found")
		}
		return nil, err
	}
	refs := make([]RouteArtifactRef, 0, len(state.Artifacts))
	for _, ref := range state.Artifacts {
		if routeArtifactUsableInPlan(ref, state.Plan) {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func (s *SQLiteRouteStateStore) CurrentArtifactRefs(scope InvocationScope) ([]RouteArtifactRef, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("route state store is unavailable")
	}
	if strings.TrimSpace(scope.RootTaskID) == "" || strings.TrimSpace(scope.SessionID) == "" || strings.TrimSpace(scope.PrincipalID) == "" {
		return nil, fmt.Errorf("route_state_scope_required")
	}
	var routeKey string
	err := s.db.QueryRow(`SELECT current_route_key FROM semantic_route_lineages WHERE lineage_key = ?`, routeLineageKey(scope)).Scan(&routeKey)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("route_revision_not_found")
	}
	if err != nil {
		return nil, err
	}
	state, err := s.get(routeKey)
	if err != nil {
		return nil, fmt.Errorf("route_revision_corrupt")
	}
	refs := make([]RouteArtifactRef, 0, len(state.Artifacts))
	for _, artifact := range state.Artifacts {
		if routeArtifactUsableInPlan(artifact, state.Plan) {
			refs = append(refs, artifact)
		}
	}
	return refs, nil
}

func (s *SQLiteRouteStateStore) ReconcileArtifacts(scope InvocationScope, artifacts ArtifactStore, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	state, err := s.get(routeStateKey(scope))
	if err != nil {
		if err == sql.ErrNoRows {
			return RouteState{}, fmt.Errorf("route_state_not_found")
		}
		return RouteState{}, err
	}
	refs, err := reconcileRouteArtifacts(state, artifacts, now)
	if err != nil {
		return RouteState{}, err
	}
	for _, ref := range refs {
		if _, err := s.RecordArtifact(state.Scope, state.Plan.ID, ref, now); err != nil {
			return RouteState{}, err
		}
	}
	return s.get(routeStateKey(state.Scope))
}

func (s *SQLiteRouteStateStore) ReconcileCurrentArtifacts(scope InvocationScope, artifacts ArtifactStore, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if strings.TrimSpace(scope.RootTaskID) == "" || strings.TrimSpace(scope.SessionID) == "" || strings.TrimSpace(scope.PrincipalID) == "" {
		return RouteState{}, fmt.Errorf("route_state_scope_required")
	}
	var routeKey string
	err := s.db.QueryRow(`SELECT current_route_key FROM semantic_route_lineages WHERE lineage_key = ?`, routeLineageKey(scope)).Scan(&routeKey)
	if err == sql.ErrNoRows {
		return RouteState{}, fmt.Errorf("route_revision_not_found")
	}
	if err != nil {
		return RouteState{}, err
	}
	state, err := s.get(routeKey)
	if err != nil {
		return RouteState{}, fmt.Errorf("route_revision_corrupt")
	}
	return s.ReconcileArtifacts(state.Scope, artifacts, now)
}

func (s *SQLiteRouteStateStore) RecordMaterialization(scope InvocationScope, planID string, materialization RouteMaterialization, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	if err := validateRouteMaterialization(materialization); err != nil {
		return RouteState{}, err
	}
	if materialization.Grant.Scope != scope || materialization.Grant.Scope.PlanID != planID {
		return RouteState{}, fmt.Errorf("route_state_grant_scope_mismatch")
	}
	key := routeStateKey(scope)
	grantJSON, err := json.Marshal(materialization.Grant)
	if err != nil {
		return RouteState{}, err
	}
	// The plan row is immutable after Open. Read it before beginning the write
	// transaction: SQLite hosts use one connection, and calling get while this
	// transaction owns that connection would deadlock rather than improve the
	// binding check.
	state, err := s.get(key)
	if err != nil {
		return RouteState{}, err
	}
	if !routeMaterializationMatchesPlan(state.Plan, scope, materialization) {
		return RouteState{}, fmt.Errorf("route_state_grant_binding_mismatch")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return RouteState{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := routeStateExists(tx, key, planID); err != nil {
		return RouteState{}, err
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO semantic_route_materializations(route_key, function_name, grant_json, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, key, materialization.FunctionName, grantJSON, materialization.State, routeStateTime(now), routeStateTime(now))
	if err != nil {
		return RouteState{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return RouteState{}, err
	}
	if changed == 0 {
		var existingGrant []byte
		var existingState RouteMaterializationState
		if err := tx.QueryRow(`SELECT grant_json, state FROM semantic_route_materializations WHERE route_key = ? AND function_name = ?`, key, materialization.FunctionName).Scan(&existingGrant, &existingState); err != nil {
			return RouteState{}, err
		}
		if string(existingGrant) != string(grantJSON) || existingState != materialization.State {
			return RouteState{}, fmt.Errorf("route_state_materialization_conflict")
		}
	}
	if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at = ? WHERE route_key = ?`, routeStateTime(now), key); err != nil {
		return RouteState{}, err
	}
	if err := tx.Commit(); err != nil {
		return RouteState{}, err
	}
	return s.get(key)
}

func (s *SQLiteRouteStateStore) RetireMaterialization(scope InvocationScope, planID, functionName string, now time.Time) (RouteState, error) {
	if s == nil || s.db == nil {
		return RouteState{}, fmt.Errorf("route state store is unavailable")
	}
	key := routeStateKey(scope)
	tx, err := s.db.Begin()
	if err != nil {
		return RouteState{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := routeStateExists(tx, key, planID); err != nil {
		return RouteState{}, err
	}
	result, err := tx.Exec(`UPDATE semantic_route_materializations SET state = 'retired', updated_at = ? WHERE route_key = ? AND function_name = ? AND state = 'exposed'`, routeStateTime(now), key, functionName)
	if err != nil {
		return RouteState{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return RouteState{}, err
	}
	if changed == 0 {
		var state RouteMaterializationState
		if err := tx.QueryRow(`SELECT state FROM semantic_route_materializations WHERE route_key = ? AND function_name = ?`, key, functionName).Scan(&state); err != nil {
			if err == sql.ErrNoRows {
				return RouteState{}, fmt.Errorf("route_state_materialization_not_found")
			}
			return RouteState{}, err
		}
		if state != RouteMaterializationRetired {
			return RouteState{}, fmt.Errorf("route_state_materialization_conflict")
		}
	}
	if _, err := tx.Exec(`UPDATE semantic_route_states SET updated_at = ? WHERE route_key = ?`, routeStateTime(now), key); err != nil {
		return RouteState{}, err
	}
	if err := tx.Commit(); err != nil {
		return RouteState{}, err
	}
	return s.get(key)
}

func routeStateExists(tx *sql.Tx, key, planID string) error {
	var storedPlan string
	if err := tx.QueryRow(`SELECT plan_id FROM semantic_route_states WHERE route_key = ?`, key).Scan(&storedPlan); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("route_state_not_found")
		}
		return err
	}
	if storedPlan != planID {
		return fmt.Errorf("route_state_conflict")
	}
	return nil
}

func (s *SQLiteRouteStateStore) get(key string) (RouteState, error) {
	var state RouteState
	var planJSON []byte
	var created, updated string
	err := s.db.QueryRow(`SELECT version, root_task_id, plan_id, session_id, turn_id, principal_id, plan_json, plan_digest, created_at, updated_at FROM semantic_route_states WHERE route_key = ?`, key).Scan(&state.Version, &state.Scope.RootTaskID, &state.Scope.PlanID, &state.Scope.SessionID, &state.Scope.TurnID, &state.Scope.PrincipalID, &planJSON, &state.PlanDigest, &created, &updated)
	if err != nil {
		return RouteState{}, err
	}
	if state.Version != RouteStateVersion || json.Unmarshal(planJSON, &state.Plan) != nil || state.Plan.ID != state.Scope.PlanID || state.Plan.RootTaskID != state.Scope.RootTaskID {
		return RouteState{}, fmt.Errorf("route_state_corrupt")
	}
	_, digest, err := canonicalRoutePlan(state.Plan)
	if err != nil || digest != state.PlanDigest {
		return RouteState{}, fmt.Errorf("route_state_corrupt")
	}
	state.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	state.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	var revision uint64
	var parentRouteKey, snapshotDigest string
	err = s.db.QueryRow(`SELECT revision, parent_route_key, snapshot_digest, fencing_token FROM semantic_route_revisions WHERE route_key = ?`, key).Scan(&revision, &parentRouteKey, &snapshotDigest, &state.FencingToken)
	if err != nil && err != sql.ErrNoRows {
		return RouteState{}, err
	}
	if err == nil {
		ref := routeRevisionRef(state.Scope, state.Plan.ID, state.PlanDigest, revision)
		state.Revision, state.SnapshotDigest = &ref, snapshotDigest
		if parentRouteKey != "" {
			var parentRootTaskID, parentSessionID, parentTurnID, parentPrincipalID string
			var parentPlanID, parentDigest string
			var parentRevision uint64
			parentErr := s.db.QueryRow(`SELECT rs.root_task_id, rs.plan_id, rs.session_id, rs.turn_id, rs.principal_id, rs.plan_digest, rr.revision FROM semantic_route_states rs JOIN semantic_route_revisions rr ON rr.route_key = rs.route_key WHERE rs.route_key = ?`, parentRouteKey).Scan(&parentRootTaskID, &parentPlanID, &parentSessionID, &parentTurnID, &parentPrincipalID, &parentDigest, &parentRevision)
			if parentErr != nil {
				return RouteState{}, fmt.Errorf("route_state_corrupt")
			}
			parent := routeRevisionRef(InvocationScope{RootTaskID: parentRootTaskID, SessionID: parentSessionID, TurnID: parentTurnID, PrincipalID: parentPrincipalID}, parentPlanID, parentDigest, parentRevision)
			state.ParentRevision = &parent
		}
	}
	var amendment RouteAmendmentRef
	amendmentErr := s.db.QueryRow(`SELECT command_id, digest, parent_revision, parent_fencing_token FROM semantic_route_amendments WHERE route_key = ?`, key).Scan(&amendment.CommandID, &amendment.Digest, &amendment.ParentRevision, &amendment.ParentFencingToken)
	if amendmentErr == nil {
		state.Amendment = &amendment
	} else if amendmentErr != sql.ErrNoRows {
		return RouteState{}, amendmentErr
	}
	completedRows, err := s.db.Query(`SELECT selection_id, purpose_digest, completed_at FROM semantic_route_completed_selections WHERE route_key = ? ORDER BY selection_id`, key)
	if err != nil {
		return RouteState{}, err
	}
	for completedRows.Next() {
		var completed RouteCompletedSelection
		var completedAt string
		if err := completedRows.Scan(&completed.SelectionID, &completed.PurposeDigest, &completedAt); err != nil {
			_ = completedRows.Close()
			return RouteState{}, err
		}
		completed.CompletedAt, _ = time.Parse(time.RFC3339Nano, completedAt)
		if !completedSelectionMatchesPlan(completed, state.Plan) {
			_ = completedRows.Close()
			return RouteState{}, fmt.Errorf("route_state_corrupt")
		}
		state.Completed = append(state.Completed, completed)
	}
	if err := completedRows.Close(); err != nil {
		return RouteState{}, err
	}
	confirmationRows, err := s.db.Query(`SELECT requirement, purpose_digest, authority, valid_until, granted_at FROM semantic_route_confirmations WHERE route_key = ? ORDER BY requirement`, key)
	if err != nil {
		return RouteState{}, err
	}
	for confirmationRows.Next() {
		var confirmation RouteConfirmation
		var authority, validUntil, grantedAt string
		if err := confirmationRows.Scan(&confirmation.Requirement, &confirmation.PurposeDigest, &authority, &validUntil, &grantedAt); err != nil {
			_ = confirmationRows.Close()
			return RouteState{}, err
		}
		confirmation.Authority = FactAuthority(authority)
		if validUntil != "" {
			confirmation.ValidUntil, _ = time.Parse(time.RFC3339Nano, validUntil)
		}
		confirmation.GrantedAt, _ = time.Parse(time.RFC3339Nano, grantedAt)
		if !confirmationMatchesPlan(confirmation, state.Plan, time.Now().UTC()) && (confirmation.ValidUntil.IsZero() || time.Now().UTC().Before(confirmation.ValidUntil)) {
			_ = confirmationRows.Close()
			return RouteState{}, fmt.Errorf("route_state_corrupt")
		}
		state.Confirmations = append(state.Confirmations, confirmation)
	}
	if err := confirmationRows.Close(); err != nil {
		return RouteState{}, err
	}
	artifactRows, err := s.db.Query(`SELECT artifact_id, kind, mime_type, integrity_digest, producer_selection, producer_purpose_digest, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, created_at FROM semantic_route_artifacts WHERE route_key = ? ORDER BY artifact_id`, key)
	if err != nil {
		return RouteState{}, err
	}
	for artifactRows.Next() {
		var value RouteArtifactRef
		var createdAt string
		if err := artifactRows.Scan(&value.ArtifactID, &value.Kind, &value.MIMEType, &value.IntegrityDigest, &value.ProducerSelection, &value.ProducerPurposeDigest, &value.SourceScope.RootTaskID, &value.SourceScope.PlanID, &value.SourceScope.SessionID, &value.SourceScope.TurnID, &value.SourceScope.PrincipalID, &createdAt); err != nil {
			_ = artifactRows.Close()
			return RouteState{}, err
		}
		value.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if !routeArtifactUsableInPlan(value, state.Plan) {
			_ = artifactRows.Close()
			return RouteState{}, fmt.Errorf("route_state_corrupt")
		}
		state.Artifacts = append(state.Artifacts, value)
	}
	if err := artifactRows.Close(); err != nil {
		return RouteState{}, err
	}
	rows, err := s.db.Query(`SELECT function_name, grant_json, state, created_at, updated_at FROM semantic_route_materializations WHERE route_key = ? ORDER BY function_name`, key)
	if err != nil {
		return RouteState{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var materialization RouteMaterialization
		var grantJSON []byte
		var materializedAt, updatedAt string
		if err := rows.Scan(&materialization.FunctionName, &grantJSON, &materialization.State, &materializedAt, &updatedAt); err != nil {
			return RouteState{}, err
		}
		if err := json.Unmarshal(grantJSON, &materialization.Grant); err != nil || validateRouteMaterialization(materialization) != nil || !routeMaterializationMatchesPlan(state.Plan, state.Scope, materialization) {
			return RouteState{}, fmt.Errorf("route_state_corrupt")
		}
		materialization.CreatedAt, _ = time.Parse(time.RFC3339Nano, materializedAt)
		materialization.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		state.Materializations = append(state.Materializations, materialization)
	}
	if err := rows.Err(); err != nil {
		return RouteState{}, err
	}
	return cloneRouteState(state), nil
}

func routeStateTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
