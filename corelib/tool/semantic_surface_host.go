package tool

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// NormalizeGrantTTL returns ttl when positive, otherwise the shared production default.
func NormalizeGrantTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultInvocationGrantTTL
	}
	return ttl
}

// NewSurfacePublishRequest fills GrantTTL and Now so GUI and srv do not copy
// a second issuance envelope. Hosts still supply tenant, issuer, and revision.
func NewSurfacePublishRequest(revision RouteRevisionPublishRequest, tenantID string, issuer *InvocationIssuer, ttl time.Duration, now time.Time) SurfacePublishRequest {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return SurfacePublishRequest{
		Revision: revision,
		TenantID: tenantID,
		Issuer:   issuer,
		GrantTTL: NormalizeGrantTTL(ttl),
		Now:      now.UTC(),
	}
}

// PublishCurrentSurface publishes a model-visible surface. Production hosts
// pass the coordinator so revision, grants, and materializations commit
// together. Memory/test hosts fall back to PublishRevision without grants.
func PublishCurrentSurface(coordinator *SQLiteSemanticExecutionCoordinator, routeState RouteStateStore, request SurfacePublishRequest) (RouteState, []InvocationGrant, error) {
	if coordinator != nil {
		return coordinator.PublishSurface(request)
	}
	if routeState == nil {
		return RouteState{}, nil, fmt.Errorf("semantic route state is unavailable")
	}
	now := request.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state, err := routeState.PublishRevision(request.Revision, now)
	return state, nil, err
}

// IssueReadySurface materializes the next ready grant closure. The coordinator
// path is atomic. The memory/test path issues grants then records exposed
// materializations on routeState, matching the historical host fallback.
func IssueReadySurface(coordinator *SQLiteSemanticExecutionCoordinator, routeState RouteStateStore, issuer *InvocationIssuer, plan ToolPlan, scope InvocationScope, ttl time.Duration, completed map[string]bool, selectionIDs map[string]bool, now time.Time) ([]InvocationGrant, error) {
	ttl = NormalizeGrantTTL(ttl)
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if coordinator != nil {
		_, grants, err := coordinator.MaterializeReadySurface(scope, issuer, ttl, completed, selectionIDs, now)
		return grants, err
	}
	if issuer == nil {
		return nil, fmt.Errorf("invocation issuer is required")
	}
	grants, err := issuer.IssueReady(PlanWithSelections(plan, selectionIDs), scope, ttl, completed)
	if err != nil {
		return nil, err
	}
	if routeState == nil {
		return grants, nil
	}
	for _, grant := range grants {
		if _, err := routeState.RecordMaterialization(scope, plan.ID, RouteMaterialization{
			FunctionName: grant.Token, Grant: grant, State: RouteMaterializationExposed,
		}, now); err != nil {
			return nil, err
		}
	}
	return grants, nil
}

// MarkIssuedSelectionIDs records every grant's selection as already issued so
// the next exposure closure will not mint a replacement identity.
func MarkIssuedSelectionIDs(grants []InvocationGrant, issued map[string]bool) {
	if issued == nil {
		return
	}
	for _, grant := range grants {
		if id := strings.TrimSpace(grant.SelectionID); id != "" {
			issued[id] = true
		}
	}
}

// IssueAndBindReadySurface issues the next ready grant closure, binds it into
// the host grant table, and records issued selection IDs. GUI refresh and
// headless Definitions both used this three-step sequence.
func IssueAndBindReadySurface(coordinator *SQLiteSemanticExecutionCoordinator, routeState RouteStateStore, issuer *InvocationIssuer, plan ToolPlan, scope InvocationScope, ttl time.Duration, completed map[string]bool, selectionIDs map[string]bool, table map[string]InvocationGrant, issued map[string]bool, now time.Time) ([]InvocationGrant, error) {
	if len(selectionIDs) == 0 {
		return nil, nil
	}
	grants, err := IssueReadySurface(coordinator, routeState, issuer, plan, scope, ttl, completed, selectionIDs, now)
	if err != nil {
		return nil, err
	}
	if err := BindIssuedGrants(grants, table); err != nil {
		return nil, err
	}
	MarkIssuedSelectionIDs(grants, issued)
	return grants, nil
}

// FilterSelectionIDs drops IDs whose planned selection matches skip. A nil
// skip leaves the set unchanged.
func FilterSelectionIDs(ids map[string]bool, plan ToolPlan, skip func(PlannedSelection) bool) map[string]bool {
	if len(ids) == 0 || skip == nil {
		return ids
	}
	out := make(map[string]bool, len(ids))
	for id, ok := range ids {
		if !ok {
			continue
		}
		selection, found := PlanSelectionByID(plan, id)
		if found && skip(selection) {
			continue
		}
		out[id] = true
	}
	return out
}

// BindIssuedGrant inserts one grant into the host's model-visible name table.
// An empty rendered name, a nil table, or a different token on the same name
// is rejected. Hosts still mark issued/materialized themselves.
func BindIssuedGrant(grant InvocationGrant, table map[string]InvocationGrant) (string, error) {
	if table == nil {
		return "", fmt.Errorf("grant table is unavailable")
	}
	name := RenderedSemanticFunctionName(grant.AdapterName, grant.Token)
	if name == "" {
		return "", fmt.Errorf("semantic grant %q has no model function name", grant.SelectionID)
	}
	if existing, exists := table[name]; exists && existing.Token != grant.Token {
		return "", fmt.Errorf("function-name collision for grant %q", grant.SelectionID)
	}
	table[name] = grant
	return name, nil
}

// BindIssuedGrants inserts a batch of grants. The first collision or unnamed
// grant fails the whole batch; earlier inserts in this call remain.
func BindIssuedGrants(grants []InvocationGrant, table map[string]InvocationGrant) error {
	for _, grant := range grants {
		if _, err := BindIssuedGrant(grant, table); err != nil {
			return err
		}
	}
	return nil
}

// MergeCompletedSelections unions projected completion onto the executor's
// completed set. GUI recovery and headless Definitions both recover this way.
func MergeCompletedSelections(completed, projected map[string]bool) map[string]bool {
	if completed == nil {
		completed = make(map[string]bool, len(projected))
	}
	for selectionID, ok := range projected {
		if ok {
			completed[selectionID] = true
		}
	}
	return completed
}

// LoadCompletedSelections unions the executor's succeeded set with the
// route-state projection. GUI recovery and headless surface open both do this.
func LoadCompletedSelections(executor *PlanExecutor, routeState RouteStateStore, scope InvocationScope) (map[string]bool, error) {
	if executor == nil {
		return nil, fmt.Errorf("plan executor is unavailable")
	}
	if routeState == nil {
		return nil, fmt.Errorf("semantic route state is unavailable")
	}
	completed, err := executor.Completed(scope)
	if err != nil {
		return nil, err
	}
	projected, err := routeState.CompletedSelections(scope)
	if err != nil {
		return nil, err
	}
	return MergeCompletedSelections(completed, projected), nil
}

// PrepareReadySurfaceCompletion loads durable completion and unions trusted
// facts into the satisfied set MaterializeReadySurface uses for ReadySelections.
func PrepareReadySurfaceCompletion(executor *PlanExecutor, routeState RouteStateStore, scope InvocationScope, trustedFacts map[string]bool) (completed, satisfied map[string]bool, err error) {
	completed, err = LoadCompletedSelections(executor, routeState, scope)
	if err != nil {
		return nil, nil, err
	}
	return completed, UnionSatisfiedIDs(completed, trustedFacts), nil
}

// PlaceMaterializedGrant puts a recovered materialization into the live grant
// table or the retired table. Hosts still mark issued/materialized themselves.
func PlaceMaterializedGrant(materialization RouteMaterialization, live, retired map[string]InvocationGrant) error {
	if retired == nil {
		return fmt.Errorf("retired grant table is unavailable")
	}
	if materialization.State != RouteMaterializationExposed {
		name := RenderedSemanticFunctionName(materialization.Grant.AdapterName, materialization.Grant.Token)
		retired[name] = materialization.Grant
		return nil
	}
	_, err := BindIssuedGrant(materialization.Grant, live)
	return err
}

// RetireLiveGrant moves one live grant into the retired table after the host
// has durably retired its materialization. The retire callback runs first so a
// failed durable write cannot drop the model-visible name.
func RetireLiveGrant(live, retired map[string]InvocationGrant, functionName string, grant InvocationGrant, retire func() error) error {
	if live == nil || retired == nil {
		return fmt.Errorf("grant tables are unavailable")
	}
	if retire != nil {
		if err := retire(); err != nil {
			return err
		}
	}
	retired[functionName] = grant
	delete(live, functionName)
	return nil
}

// RetireConsumedGrant moves a consumed grant out of the live table even when
// the durable retire callback fails. Re-exposing it would invite a stale model
// retry; recovery reads durable materialization rather than dispatching again.
func RetireConsumedGrant(live, retired map[string]InvocationGrant, functionName string, grant InvocationGrant, retire func() error) error {
	var err error
	if retire != nil {
		err = retire()
	}
	if moveErr := RetireLiveGrant(live, retired, functionName, grant, nil); moveErr != nil && err == nil {
		err = moveErr
	}
	return err
}

// RetireLiveGrantsForSelection retires every live grant bound to one selection.
// Consumed complete/retire paths use RetireConsumedGrant so a durable write
// failure cannot leave the function model-visible.
func RetireLiveGrantsForSelection(live, retired map[string]InvocationGrant, selectionID string, retire func(functionName string, grant InvocationGrant) error) error {
	selectionID = strings.TrimSpace(selectionID)
	if selectionID == "" {
		return nil
	}
	for functionName, grant := range live {
		if grant.SelectionID != selectionID {
			continue
		}
		name := functionName
		current := grant
		if err := RetireConsumedGrant(live, retired, name, current, func() error {
			if retire == nil {
				return nil
			}
			return retire(name, current)
		}); err != nil {
			return err
		}
	}
	return nil
}

// RetireConsumedLiveGrants hides every live grant whose durable execution
// already consumed the model function. GUI surface open and headless
// constructor both recover this way. Execution-not-found keeps the grant;
// any other store error fails closed so a broken execution log cannot
// re-expose a spent invocation.
func RetireConsumedLiveGrants(live, retired map[string]InvocationGrant, executor *PlanExecutor, scope InvocationScope, retire func(InvocationGrant) error) error {
	if executor == nil || live == nil {
		return nil
	}
	for functionName, grant := range live {
		record, err := executor.Execution(scope, grant.SelectionID)
		if err != nil {
			if errors.Is(err, ErrPlanExecutionNotFound) {
				continue
			}
			return fmt.Errorf("recover plan execution: %w", err)
		}
		if !ExecutionConsumesModelGrant(record.State) {
			continue
		}
		name, current := functionName, grant
		if err := RetireConsumedGrant(live, retired, name, current, func() error {
			if retire == nil {
				return nil
			}
			return retire(current)
		}); err != nil {
			return err
		}
	}
	return nil
}

// UnionSatisfiedIDs copies every true identity from the given sets into a new
// map. Trusted facts and completed selections merge this way; callers must
// not mutate the inputs.
func UnionSatisfiedIDs(sets ...map[string]bool) map[string]bool {
	size := 0
	for _, set := range sets {
		size += len(set)
	}
	out := make(map[string]bool, size)
	for _, set := range sets {
		for id, ok := range set {
			if ok {
				out[id] = true
			}
		}
	}
	return out
}

// VisibleReadyGrants returns the currently ready, uncompleted selections that
// already hold a grant, plus those grants. Hosts compute `ready` with their
// own completion/fact set; `completed` is only the skip set so trusted facts
// can make a node ready without hiding it.
func VisibleReadyGrants(ready []PlannedSelection, completed map[string]bool, grants map[string]InvocationGrant) (map[string]bool, []InvocationGrant) {
	readyIDs := make(map[string]bool, len(ready))
	for _, selection := range ready {
		if completed[selection.ID] {
			continue
		}
		readyIDs[selection.ID] = true
	}
	if len(readyIDs) == 0 {
		return nil, nil
	}
	visible := make(map[string]bool, len(readyIDs))
	out := make([]InvocationGrant, 0, len(readyIDs))
	for _, grant := range grants {
		if !readyIDs[grant.SelectionID] {
			continue
		}
		visible[grant.SelectionID] = true
		out = append(out, grant)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return visible, out
}

// UnrenderedReadyGrants is the incremental-refresh subset of VisibleReadyGrants:
// already-rendered function names stay off the next model surface.
func UnrenderedReadyGrants(ready []PlannedSelection, completed map[string]bool, grants map[string]InvocationGrant, renderedNames map[string]bool) (map[string]bool, []InvocationGrant) {
	visible, out := VisibleReadyGrants(ready, completed, grants)
	if len(out) == 0 || len(renderedNames) == 0 {
		return visible, out
	}
	filteredIDs := make(map[string]bool, len(visible))
	filtered := make([]InvocationGrant, 0, len(out))
	for _, grant := range out {
		name := RenderedSemanticFunctionName(grant.AdapterName, grant.Token)
		if renderedNames[name] {
			continue
		}
		filteredIDs[grant.SelectionID] = true
		filtered = append(filtered, grant)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	return filteredIDs, filtered
}

// RenderVisibleReadyDefinitions renders the current exposure closure. It never
// issues grants; hosts that need a full replacement list (GUI after execution,
// headless Definitions after bind) share this path.
func RenderVisibleReadyDefinitions(registry *CapabilityRegistry, plan ToolPlan, ready []PlannedSelection, completed map[string]bool, grants map[string]InvocationGrant, schemas map[string]map[string]interface{}, satisfied map[string]bool) ([]RenderedTool, error) {
	visible, grantList := VisibleReadyGrants(ready, completed, grants)
	if len(grantList) == 0 {
		return nil, nil
	}
	return NewCatalogRenderer(registry).RenderReady(PlanWithSelections(plan, visible), grantList, schemas, satisfied)
}

// ProjectRenderedDefinitions copies renderer output into the host-visible
// OpenAI function list. Optional markNames / byName maps record incremental
// GUI rendered-set and headless definition cache.
func ProjectRenderedDefinitions(rendered []RenderedTool, markNames map[string]bool, byName map[string]map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(rendered))
	for _, item := range rendered {
		if item.FunctionName != "" {
			if markNames != nil {
				markNames[item.FunctionName] = true
			}
			if byName != nil {
				byName[item.FunctionName] = item.Definition
			}
		}
		out = append(out, item.Definition)
	}
	return out
}

// ReadySurfaceRequest is the host-filled envelope for one materialize pass.
// GUI refresh and headless Definitions share completion prepare, issue/bind,
// and render. Hosts still supply epoch invalidation, skip, and whether the
// pass is incremental (RenderedNames) or a full visible replacement.
type ReadySurfaceRequest struct {
	Coordinator   *SQLiteSemanticExecutionCoordinator
	RouteState    RouteStateStore
	Issuer        *InvocationIssuer
	Executor      *PlanExecutor
	Registry      *CapabilityRegistry
	Plan          ToolPlan
	Scope         InvocationScope
	TTL           time.Duration
	TrustedFacts  map[string]bool
	Completed     map[string]bool
	Satisfied     map[string]bool
	Needed        map[string]bool
	Unsettled     func(string) bool
	Skip          func(PlannedSelection) bool
	Grants        map[string]InvocationGrant
	Issued        map[string]bool
	Schemas       map[string]map[string]interface{}
	RenderedNames map[string]bool
	MarkRendered  map[string]bool
	IndexByName   map[string]map[string]interface{}
	Now           time.Time
}

// ApplyReadySurfaceCompletion fills Completed and Satisfied for one
// materialize pass. When Executor is set it loads durable completion and
// unions TrustedFacts; otherwise Satisfied defaults to Completed ∪ facts.
// A non-nil Completed map is updated in place so host tables stay aligned.
func ApplyReadySurfaceCompletion(req *ReadySurfaceRequest) error {
	if req == nil {
		return fmt.Errorf("ready surface request is unavailable")
	}
	if req.Executor != nil {
		loaded, _, err := PrepareReadySurfaceCompletion(req.Executor, req.RouteState, req.Scope, req.TrustedFacts)
		if err != nil {
			return err
		}
		req.Completed = MergeCompletedSelections(req.Completed, loaded)
		req.Satisfied = UnionSatisfiedIDs(req.Completed, req.TrustedFacts)
		return nil
	}
	if req.Satisfied == nil {
		req.Satisfied = UnionSatisfiedIDs(req.Completed, req.TrustedFacts)
	}
	return nil
}

// MaterializeReadySurface issues the next ready grant closure and renders the
// model-visible function list. Incremental hosts pass RenderedNames so already
// shown functions stay off the next surface; full hosts omit it.
func MaterializeReadySurface(req ReadySurfaceRequest) ([]map[string]interface{}, error) {
	if err := ApplyReadySurfaceCompletion(&req); err != nil {
		return nil, err
	}
	if req.Registry == nil {
		return nil, fmt.Errorf("capability registry is unavailable")
	}
	satisfied := req.Satisfied
	if satisfied == nil {
		satisfied = req.Completed
	}
	ready := req.Plan.ReadySelections(satisfied)
	needed := req.Needed
	if needed == nil {
		needed = NextExposedSelections(ready, req.Completed, req.Issued, req.Grants, req.Unsettled)
	}
	needed = FilterSelectionIDs(needed, req.Plan, req.Skip)
	if _, err := IssueAndBindReadySurface(req.Coordinator, req.RouteState, req.Issuer, req.Plan, req.Scope, req.TTL, satisfied, needed, req.Grants, req.Issued, req.Now); err != nil {
		return nil, err
	}
	if req.RenderedNames != nil {
		renderIDs, renderGrants := UnrenderedReadyGrants(ready, req.Completed, req.Grants, req.RenderedNames)
		if len(renderIDs) == 0 {
			return nil, nil
		}
		rendered, err := NewCatalogRenderer(req.Registry).RenderReady(PlanWithSelections(req.Plan, renderIDs), renderGrants, req.Schemas, satisfied)
		if err != nil {
			return nil, err
		}
		return ProjectRenderedDefinitions(rendered, req.MarkRendered, req.IndexByName), nil
	}
	rendered, err := RenderVisibleReadyDefinitions(req.Registry, req.Plan, ready, req.Completed, req.Grants, req.Schemas, satisfied)
	if err != nil {
		return nil, err
	}
	if len(rendered) == 0 {
		return nil, nil
	}
	return ProjectRenderedDefinitions(rendered, req.MarkRendered, req.IndexByName), nil
}
