package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingDurableDynamicSurface is the G3 request-presentation handoff for a
// Coding dynamic plan. The coordinator, rather than a CodingSubAgent map, is
// authoritative for every alias. Its aliases remain prepared (and therefore
// unresolvable) until the trusted provider adapter binds a response ID.
//
// It deliberately contains no dispatcher. G4 must resolve the durable alias,
// validate/admit the returned grant and invoke the fixed binding bridge; doing
// provider I/O from this type would recreate the old renderer-to-handler
// bypass under a different name.
type codingDurableDynamicSurface struct {
	coordinator *tool.SQLiteSemanticExecutionCoordinator
	plan        tool.ToolPlan
	scope       tool.InvocationScope
	tenantID    string
	protocol    string
	connection  string
	epoch       string
	aliases     map[string]tool.InvocationGrant
	definitions []map[string]interface{}
}

// publishCodingDurableDynamicSurface atomically publishes the route and its
// initial grants, then records one prepared request surface over those grants.
// Protocol and connectionID are supplied by the host's transport adapter; no
// LoopContext, project path, model value, or provider descriptor is accepted
// as a correlation substitute.
func (a *App) publishCodingDurableDynamicSurface(identity *trustedCodingInvocationIdentity, prepared codingDynamicPlanPreparation, dynamic codingDynamicCatalogSnapshot, protocol, connectionID string, now time.Time) (*codingDurableDynamicSurface, error) {
	return a.publishCodingDurableDynamicSurfaceForEpoch(identity, prepared, dynamic, protocol, connectionID, "", now)
}

// publishCodingDurableDynamicSurfaceForEpoch is the correlation-bound form of
// the G3 publisher. A non-empty epoch comes exclusively from RunLoop after it
// reserved the concrete request channel. Keeping this separate from the
// compatibility helper above prevents a future adapter from publishing aliases
// under a locally invented epoch and then sending them on another transport.
func (a *App) publishCodingDurableDynamicSurfaceForEpoch(identity *trustedCodingInvocationIdentity, prepared codingDynamicPlanPreparation, dynamic codingDynamicCatalogSnapshot, protocol, connectionID, epoch string, now time.Time) (*codingDurableDynamicSurface, error) {
	if a == nil || identity == nil || !identity.complete() {
		return nil, fmt.Errorf("coding dynamic identity is incomplete")
	}
	if !dynamic.complete() {
		return nil, fmt.Errorf("coding dynamic catalog is incomplete")
	}
	if strings.TrimSpace(prepared.Plan.ID) == "" || prepared.Plan.RootTaskID != identity.RootTaskID {
		return nil, fmt.Errorf("coding dynamic plan is invalid")
	}
	// A partial plan must never be presented as a usable dynamic tool surface.
	// Required needs left unmet are a catalog/replanning outcome, not a reason to
	// let the model attempt whichever ready selection happened to fit first.
	if len(prepared.Plan.Unmet) != 0 || len(prepared.Plan.Selections) == 0 {
		return nil, fmt.Errorf("coding dynamic plan is incomplete")
	}
	protocol, connectionID = strings.TrimSpace(protocol), strings.TrimSpace(connectionID)
	if protocol == "" || connectionID == "" {
		return nil, fmt.Errorf("coding dynamic request correlation is required")
	}
	if now = now.UTC(); now.IsZero() {
		now = time.Now().UTC()
	}
	issuer, err := a.semanticInvocationIssuer()
	if err != nil {
		return nil, fmt.Errorf("load coding dynamic issuer: %w", err)
	}
	coordinator, err := a.semanticExecutionCoordinatorForApp()
	if err != nil {
		return nil, fmt.Errorf("load coding dynamic coordinator: %w", err)
	}
	scope := tool.InvocationScope{
		RootTaskID: identity.RootTaskID, PlanID: prepared.Plan.ID, SessionID: identity.SessionID,
		TurnID: identity.TurnID, PrincipalID: identity.PrincipalID,
	}
	_, grants, err := coordinator.PublishSurface(tool.SurfacePublishRequest{
		Revision: tool.RouteRevisionPublishRequest{Scope: scope, Plan: prepared.Plan, SnapshotDigest: prepared.Plan.SnapshotDigest},
		TenantID: identity.TenantID, Issuer: issuer, GrantTTL: 5 * time.Minute, Now: now,
	})
	if err != nil {
		return nil, fmt.Errorf("publish coding dynamic route: %w", err)
	}
	// PublishSurface commits the plan and its initial grants before this method
	// can bind their presentation to the reserved model request. From this point
	// on, every local render/alias/request-surface failure must retire that
	// durable route in the coordinator. Returning an error alone would leave
	// exposed grants that have no complete request-owned presentation closure.
	// Cancellation is deliberately coordinator-owned so request surfaces,
	// materializations and still-issued grants retire atomically.
	failPublishedRoute := func(cause error) (*codingDurableDynamicSurface, error) {
		if cause == nil {
			cause = fmt.Errorf("coding dynamic route publication failed")
		}
		if cancelErr := coordinator.CancelRouteSurface(scope, time.Now().UTC()); cancelErr != nil {
			return nil, fmt.Errorf("%w; retire incomplete coding dynamic route: %v", cause, cancelErr)
		}
		return nil, cause
	}
	rendered, err := tool.NewCatalogRenderer(newIMSemanticCapabilityRegistry()).Render(prepared.Plan, grants, dynamic.Catalog.Definitions)
	if err != nil {
		return failPublishedRoute(fmt.Errorf("render coding dynamic route: %w", err))
	}
	if len(rendered) == 0 {
		return failPublishedRoute(fmt.Errorf("coding dynamic route has no ready materialization"))
	}
	aliases := make(map[string]tool.InvocationGrant, len(rendered))
	definitions := make([]map[string]interface{}, 0, len(rendered))
	for _, item := range rendered {
		nonce, err := codingDurableDynamicSurfaceNonce()
		if err != nil {
			return failPublishedRoute(err)
		}
		alias := "coding_dynamic_" + nonce[:24]
		if _, exists := aliases[alias]; exists {
			return failPublishedRoute(fmt.Errorf("coding dynamic alias collision"))
		}
		definition, err := codingDynamicDefinitionWithAlias(item.Definition, alias)
		if err != nil {
			return failPublishedRoute(err)
		}
		aliases[alias] = item.Grant
		definitions = append(definitions, definition)
	}
	epoch = strings.TrimSpace(epoch)
	if epoch == "" {
		nonce, err := codingDurableDynamicSurfaceNonce()
		if err != nil {
			return failPublishedRoute(err)
		}
		epoch = "coding-surface:" + nonce
	}
	if _, err = coordinator.PublishModelRequestSurface(tool.ModelRequestSurfacePublish{
		Scope: scope, Protocol: protocol, ConnectionID: connectionID, Epoch: epoch, Aliases: aliases, Now: now,
	}); err != nil {
		return failPublishedRoute(fmt.Errorf("publish coding dynamic request surface: %w", err))
	}
	return &codingDurableDynamicSurface{
		coordinator: coordinator, plan: prepared.Plan, scope: scope, tenantID: identity.TenantID, protocol: protocol, connection: connectionID,
		epoch: epoch, aliases: aliases, definitions: definitions,
	}, nil
}

// BindResponse is invoked only by the host's trusted provider response
// adapter. Models never receive or choose response IDs.
func (s *codingDurableDynamicSurface) BindResponse(responseID string, now time.Time) error {
	if s == nil || s.coordinator == nil {
		return fmt.Errorf("coding dynamic request surface is unavailable")
	}
	return s.coordinator.BindModelRequestResponse(s.epoch, s.protocol, s.connection, strings.TrimSpace(responseID), now)
}

// ResolveAlias is a G3/G4 boundary. It proves the alias was actually rendered
// in this response domain and returns only the immutable coordinator grant and
// scope; it deliberately does not identify or invoke an MCP/Skill provider.
func (s *codingDurableDynamicSurface) ResolveAlias(responseID, alias string) (tool.InvocationGrant, tool.InvocationScope, error) {
	if s == nil || s.coordinator == nil {
		return tool.InvocationGrant{}, tool.InvocationScope{}, fmt.Errorf("stale_surface")
	}
	return s.coordinator.ResolveModelRequestAlias(s.epoch, s.protocol, s.connection, strings.TrimSpace(responseID), strings.TrimSpace(alias))
}

// recoverCodingDurableDynamicSurface recreates the narrow G3/G4 holder after
// a GUI process restart. The durable coordinator, authenticated identity and
// transport-owned request tuple are its only inputs. It deliberately restores
// neither rendered definitions nor a DynamicSemanticCatalog: those in-memory
// values are not authority and must be freshly observed by the fixed bridge
// immediately before provider I/O.
//
// This is a recovery helper only. CodingSubAgent remains closed to dynamic
// aliases until its real request lifecycle adapter supplies the same trusted
// protocol/connection/response/tool-call correlation.
func (a *App) recoverCodingDurableDynamicSurface(identity *trustedCodingInvocationIdentity, protocol, connectionID, epoch string) (*codingDurableDynamicSurface, error) {
	if a == nil || identity == nil || !identity.complete() {
		return nil, fmt.Errorf("stale_surface")
	}
	coordinator, err := a.semanticExecutionCoordinatorForApp()
	if err != nil {
		return nil, fmt.Errorf("load coding dynamic coordinator: %w", err)
	}
	surface, err := coordinator.RecoverBoundModelRequestSurface(tool.ModelRequestSurfaceRecovery{
		TenantID: identity.TenantID, Protocol: protocol, ConnectionID: connectionID, Epoch: epoch,
	})
	if err != nil {
		return nil, err
	}
	if surface.Scope.RootTaskID != identity.RootTaskID || surface.Scope.SessionID != identity.SessionID || surface.Scope.TurnID != identity.TurnID || surface.Scope.PrincipalID != identity.PrincipalID {
		return nil, fmt.Errorf("stale_surface")
	}
	plan, err := coordinator.Routes.PublishedPlan(surface.Scope)
	if err != nil {
		return nil, fmt.Errorf("stale_surface")
	}
	return &codingDurableDynamicSurface{
		coordinator: coordinator,
		plan:        plan,
		scope:       surface.Scope,
		tenantID:    identity.TenantID,
		protocol:    surface.Protocol,
		connection:  surface.ConnectionID,
		epoch:       surface.Epoch,
		aliases:     nil, // must never rehydrate a process-local name dispatcher
		definitions: nil, // presentation is only valid in the original sent request
	}, nil
}

// ReplaceRequest atomically retires this prepared/active request epoch and
// publishes a retry/fallback successor over exactly the same still-issued
// grants. It never signs a replacement grant, so a late predecessor response
// cannot spend authority intended for the successor.
func (s *codingDurableDynamicSurface) ReplaceRequest(protocol, connectionID string, now time.Time) (*codingDurableDynamicSurface, error) {
	if s == nil || s.coordinator == nil || len(s.aliases) == 0 {
		return nil, fmt.Errorf("coding dynamic request surface is unavailable")
	}
	protocol, connectionID = strings.TrimSpace(protocol), strings.TrimSpace(connectionID)
	if protocol == "" || connectionID == "" {
		return nil, fmt.Errorf("coding dynamic request correlation is required")
	}
	if now = now.UTC(); now.IsZero() {
		now = time.Now().UTC()
	}
	nonce, err := codingDurableDynamicSurfaceNonce()
	if err != nil {
		return nil, err
	}
	epoch := "coding-surface:" + nonce
	// Copy the model-visible definitions before replacing the durable surface.
	// A clone failure must leave the predecessor active; publishing first would
	// create a successor that this caller cannot safely present or retire.
	definitions, err := cloneCodingDynamicDefinitions(s.definitions)
	if err != nil {
		return nil, err
	}
	if _, err := s.coordinator.ReplaceModelRequestSurface(tool.ModelRequestSurfaceReplace{
		PreviousEpoch: s.epoch,
		Successor: tool.ModelRequestSurfacePublish{
			Scope: s.scope, Protocol: protocol, ConnectionID: connectionID, Epoch: epoch, Aliases: s.aliases, Now: now,
		},
	}); err != nil {
		return nil, fmt.Errorf("replace coding dynamic request surface: %w", err)
	}
	return &codingDurableDynamicSurface{
		coordinator: s.coordinator, plan: s.plan, scope: s.scope, tenantID: s.tenantID, protocol: protocol, connection: connectionID,
		epoch: epoch, aliases: cloneCodingDynamicAliases(s.aliases), definitions: definitions,
	}, nil
}

// codingDurableDynamicSurfaceNonce refuses the legacy fail-closed sentinel.
// The renderer must never slice or publish that sentinel as if it were a
// cryptographically random request identity.
func codingDurableDynamicSurfaceNonce() (string, error) {
	nonce := codingDynamicSurfaceNonce()
	if nonce == "random_unavailable" || len(nonce) < 24 {
		return "", fmt.Errorf("coding dynamic surface nonce unavailable")
	}
	return nonce, nil
}

// Cancel retires every active request surface and every still-issued grant for
// this current route in one coordinator transaction. It is the only valid G5
// cancellation hook for an eventually-wired Coding request; callers must not
// independently clear maps, revoke grants, or retire aliases.
func (s *codingDurableDynamicSurface) Cancel(now time.Time) error {
	if s == nil || s.coordinator == nil {
		return fmt.Errorf("coding dynamic request surface is unavailable")
	}
	return s.coordinator.CancelRouteSurface(s.scope, now)
}

// Finish retires only this concrete request/response presentation after the
// loop has durably settled it. It intentionally does not cancel the route:
// a completed batch can unlock a successor presentation on the same current
// revision, while this response's aliases must never remain executable.
func (s *codingDurableDynamicSurface) Finish(now time.Time) error {
	if s == nil || s.coordinator == nil {
		return fmt.Errorf("coding dynamic request surface is unavailable")
	}
	return s.coordinator.FinishModelRequestSurface(s.epoch, now)
}

// ExecuteBoundSelection is the G4 fixed bridge. It accepts only correlation
// metadata produced by a trusted provider adapter plus the opaque alias and
// model business arguments. Provider selectors never cross this boundary:
// ResolveAlias fixes the grant, Validate fixes the selection, and the catalog
// executes only that immutable binding.
//
// It intentionally remains a host helper until CodingSubAgent and the remote
// agent receive a verified task ingress and a real request lifecycle adapter.
// Connecting it to the legacy byName callback before then would re-create the
// name-addressed execution path that this design removes.
func (s *codingDurableDynamicSurface) ExecuteBoundSelection(ctx context.Context, identity *trustedCodingInvocationIdentity, dynamic codingDynamicCatalogSnapshot, handler *IMMessageHandler, protocol, connectionID, responseID, toolCallID, alias, argsJSON string, now time.Time) tool.SelectionExecutionResult {
	if s == nil || s.coordinator == nil || handler == nil || identity == nil || !identity.complete() || !dynamic.complete() {
		return rejectedCodingDynamicSelection("catalog_incomplete")
	}
	if identity.TenantID != s.tenantID || identity.RootTaskID != s.scope.RootTaskID || identity.SessionID != s.scope.SessionID || identity.TurnID != s.scope.TurnID || identity.PrincipalID != s.scope.PrincipalID {
		return rejectedCodingDynamicSelection("stale_surface")
	}
	protocol, connectionID = strings.TrimSpace(protocol), strings.TrimSpace(connectionID)
	responseID, toolCallID, alias = strings.TrimSpace(responseID), strings.TrimSpace(toolCallID), strings.TrimSpace(alias)
	if protocol == "" || connectionID == "" || responseID == "" || toolCallID == "" || alias == "" || protocol != s.protocol || connectionID != s.connection {
		return rejectedCodingDynamicSelection("stale_surface")
	}
	if now = now.UTC(); now.IsZero() {
		now = time.Now().UTC()
	}
	grant, scope, err := s.ResolveAlias(responseID, alias)
	if err != nil || scope != s.scope {
		return rejectedCodingDynamicSelection("stale_surface")
	}
	// The durable route is the authority after recovery. Do not validate
	// against the process-local plan copy: a restarted executor must reach the
	// same conclusion from coordinator state, and a replaced route must fail
	// before any binding catalog is observed.
	publishedPlan, err := s.coordinator.Routes.PublishedPlan(scope)
	if err != nil {
		return rejectedCodingDynamicSelection("stale_surface")
	}
	issuer, err := handler.semanticInvocationIssuer()
	if err != nil {
		return rejectedCodingDynamicSelection("semantic_execution_coordinator_unavailable")
	}
	selection, err := issuer.Validate(grant, scope, publishedPlan)
	if err != nil {
		return rejectedCodingDynamicSelection(err.Error())
	}
	canonical, canonicalErr := dynamic.Catalog.CanonicalizeSelectionArguments(selection, argsJSON)
	requestDigest := "invalid:" + tool.SchemaDigest([]byte(argsJSON))
	if canonicalErr == nil {
		requestDigest = canonical.Digest
	}
	admission := tool.SemanticExecutionAdmission{
		Identity: tool.HostCallIdentity{Protocol: protocol, ConnectionID: connectionID, CallID: toolCallID, SurfaceEpoch: s.epoch},
		Grant:    grant, RequestDigest: requestDigest, Scope: scope, Selection: selection, Now: now,
	}
	if canonicalErr != nil {
		return s.rejectBoundSelection(admission, semanticModelParameterRejection(semanticCanonicalRejectionText(canonicalErr)), "parameter_schema_invalid")
	}
	record, action, err := s.coordinator.Admit(admission)
	if err != nil {
		return rejectedCodingDynamicSelection(err.Error())
	}
	if replayed, handled := codingDynamicHostCallResult(s.coordinator, scope, selection.ID, record, action, admission.RequestDigest); handled {
		return replayed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// The reservation owner cancels this context before retiring the route. A
	// call that crossed durable admission immediately before cancellation must
	// not begin catalog observation or provider I/O merely because its alias was
	// valid a moment earlier. Complete the admitted journal entry as failed so a
	// retransmission cannot turn cancellation into a fresh execution attempt.
	if err := ctx.Err(); err != nil {
		_, _ = s.coordinator.Complete(admission, tool.PlanExecutionFailed, "[system rejected] stale_surface", "stale_surface", now)
		return rejectedCodingDynamicSelection("stale_surface")
	}
	principal := agentservice.Principal{TenantID: identity.TenantID, UserID: identity.PrincipalID}
	var effects agentservice.DynamicExternalEffectCoordinator
	if semanticDynamicSelectionRequiresReceipt(selection) {
		effects, err = handler.semanticDynamicEffectCoordinator()
		if err != nil {
			_, _ = s.coordinator.Complete(admission, tool.PlanExecutionFailed, "[system rejected] dynamic_effect_coordinator_unavailable", "dynamic_effect_coordinator_unavailable", now)
			return rejectedCodingDynamicSelection("dynamic_effect_coordinator_unavailable")
		}
	}
	// Rebuild from the lifecycle-owned inventory after admission. The original
	// catalog is sufficient to canonicalize the exact request that the model
	// saw; this second observation is the binding-drift fence immediately
	// before provider I/O and cannot be substituted with an alias/name lookup.
	fresh, refreshErr := handler.codingDynamicCatalogForIdentity(ctx, identity)
	var result tool.SelectionExecutionResult
	if ctx.Err() != nil {
		result = rejectedCodingDynamicSelection("stale_surface")
	} else if refreshErr != nil || !fresh.complete() {
		result = rejectedCodingDynamicSelection("catalog_incomplete")
	} else {
		result = fresh.Catalog.ExecuteSelectionWithEffects(agentservice.WithDynamicSemanticAdmission(ctx, admission), scope, principal, guiSemanticMCPBridge{handler: handler}, guiSemanticSkillBridge{handler: handler}, effects, selection, string(canonical.CanonicalJSON))
		// A catalog-level cancellation check is the final pre-provider fence for
		// generic bindings. Preserve the request lifecycle's stronger local
		// meaning here: this response surface is terminal, so no late call may be
		// presented as a normal provider failure or a reusable execution outcome.
		if ctx.Err() != nil && result.ReasonCode == "dynamic_execution_cancelled" {
			result = rejectedCodingDynamicSelection("stale_surface")
		}
	}
	if unified, ok := effects.(agentservice.UnifiedSemanticEffectCoordinator); ok && unified.UsesSemanticExecutionCoordinator() && semanticDynamicSelectionRequiresReceipt(selection) && result.ReasonCode != "parameter_schema_invalid" {
		return result
	}
	state := tool.PlanExecutionSucceeded
	if result.Unknown {
		state, result.Succeeded = tool.PlanExecutionUnknown, false
	} else if result.AwaitingReceipt {
		state, result.Succeeded = tool.PlanExecutionAwaitingReceipt, false
	} else if !result.Succeeded {
		state = tool.PlanExecutionFailed
	}
	if _, err := s.coordinator.Complete(admission, state, result.Result, result.ReasonCode, now); err != nil {
		return rejectedCodingDynamicSelection(err.Error())
	}
	return result
}

func (s *codingDurableDynamicSurface) rejectBoundSelection(admission tool.SemanticExecutionAdmission, result, reason string) tool.SelectionExecutionResult {
	record, action, err := s.coordinator.Reject(admission, result, reason)
	if err != nil {
		return rejectedCodingDynamicSelection(err.Error())
	}
	if replayed, handled := codingDynamicHostCallResult(s.coordinator, admission.Scope, admission.Selection.ID, record, action, admission.RequestDigest); handled {
		return replayed
	}
	return tool.SelectionExecutionResult{Result: result, ReasonCode: reason}
}

func codingDynamicHostCallResult(coordinator *tool.SQLiteSemanticExecutionCoordinator, scope tool.InvocationScope, selectionID string, record tool.HostCallRecord, action tool.HostCallAcquireAction, requestDigest string) (tool.SelectionExecutionResult, bool) {
	action = tool.ResolveHostCallAcquireAction(action, record, requestDigest)
	switch action {
	case tool.HostCallAcquireAdmit:
		return tool.SelectionExecutionResult{}, false
	case tool.HostCallAcquireConflict:
		return rejectedCodingDynamicSelection("host_call_conflict"), true
	case tool.HostCallAcquireInProgress:
		return rejectedCodingDynamicSelection("host_call_in_progress"), true
	case tool.HostCallAcquireUnknown:
		return tool.SelectionExecutionResult{Result: "[system rejected] host_call_unknown", Unknown: true, ReasonCode: "host_call_unknown"}, true
	case tool.HostCallAcquireReplay:
		if coordinator != nil && coordinator.Executions != nil {
			if execution, err := coordinator.Executions.Execution(scope, selectionID); err == nil {
				switch execution.State {
				case tool.PlanExecutionSucceeded:
					return tool.SelectionExecutionResult{Result: record.Result, Succeeded: true, ReasonCode: execution.ReasonCode}, true
				case tool.PlanExecutionFailed:
					return tool.SelectionExecutionResult{Result: record.Result, ReasonCode: execution.ReasonCode}, true
				case tool.PlanExecutionUnknown:
					return tool.SelectionExecutionResult{Result: record.Result, Unknown: true, ReasonCode: execution.ReasonCode}, true
				case tool.PlanExecutionAwaitingReceipt:
					return tool.SelectionExecutionResult{Result: record.Result, AwaitingReceipt: true, ReasonCode: execution.ReasonCode}, true
				}
			}
		}
		return codingDynamicRecordedResult(record.Result), true
	default:
		return rejectedCodingDynamicSelection("host_call_in_progress"), true
	}
}

func codingDynamicRecordedResult(result string) tool.SelectionExecutionResult {
	trimmed := strings.TrimSpace(result)
	switch {
	case strings.HasPrefix(trimmed, "[system unknown]"):
		return tool.SelectionExecutionResult{Result: result, Unknown: true}
	case strings.HasPrefix(trimmed, "[system rejected]"), strings.HasPrefix(strings.ToLower(trimmed), "error:"):
		return tool.SelectionExecutionResult{Result: result}
	default:
		return tool.SelectionExecutionResult{Result: result, Succeeded: true}
	}
}

func rejectedCodingDynamicSelection(reason string) tool.SelectionExecutionResult {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "catalog_incomplete"
	}
	return tool.SelectionExecutionResult{Result: "[system rejected] " + reason, ReasonCode: reason}
}

func cloneCodingDynamicAliases(in map[string]tool.InvocationGrant) map[string]tool.InvocationGrant {
	out := make(map[string]tool.InvocationGrant, len(in))
	for alias, grant := range in {
		out[alias] = grant
	}
	return out
}

func cloneCodingDynamicDefinitions(in []map[string]interface{}) ([]map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0, len(in))
	for index, definition := range in {
		clone, err := codingDynamicDefinitionWithAlias(definition, extractToolName(definition))
		if err != nil {
			return nil, fmt.Errorf("clone coding dynamic definition %d: %w", index, err)
		}
		out = append(out, clone)
	}
	return out, nil
}

func codingDynamicDefinitionWithAlias(definition map[string]interface{}, alias string) (map[string]interface{}, error) {
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("clone coding dynamic definition: %w", err)
	}
	var clone map[string]interface{}
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil, fmt.Errorf("decode coding dynamic definition: %w", err)
	}
	function, ok := clone["function"].(map[string]interface{})
	if !ok || strings.TrimSpace(alias) == "" {
		return nil, fmt.Errorf("coding dynamic definition is invalid")
	}
	function["name"] = alias
	return clone, nil
}

// prepareAndPublishCodingDurableDynamicSurface is intentionally not connected
// to CodingSubAgent yet. The caller must be the real verified task ingress and
// an LLM transport adapter that can later provide protocol/connection/response
// correlation. Until both are wired, the agent's legacy dynamic surface stays
// closed instead of publishing aliases without an executable G4 path.
func (h *IMMessageHandler) prepareAndPublishCodingDurableDynamicSurface(ctx context.Context, identity *trustedCodingInvocationIdentity, needs []tool.CapabilityNeed, facts []tool.RoutingFact, constraints []tool.RoutingConstraint, budget tool.PlanningBudget, protocol, connectionID string) (*codingDurableDynamicSurface, error) {
	if h == nil || h.app == nil {
		return nil, fmt.Errorf("coding dynamic host is unavailable")
	}
	dynamic, err := h.codingDynamicCatalogForIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, needs, facts, constraints, budget, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return h.app.publishCodingDurableDynamicSurface(identity, prepared, dynamic, protocol, connectionID, time.Now().UTC())
}

// prepareAndPublishCodingDurableDynamicSurfaceForVerifiedCodingTask is the
// policy-bound counterpart of the low-level G3 helper. It does not let an
// integration obtain an alias surface by passing provider-derived needs: the
// only demand it can publish is the host-reviewed Coding capability policy.
// It remains deliberately unused by CodingSubAgent until a transport adapter
// can supply response/tool-call correlation and the G4 bridge is wired.
func (h *IMMessageHandler) prepareAndPublishCodingDurableDynamicSurfaceForVerifiedCodingTask(ctx context.Context, identity *trustedCodingInvocationIdentity, facts []tool.RoutingFact, constraints []tool.RoutingConstraint, budget tool.PlanningBudget, protocol, connectionID string) (*codingDurableDynamicSurface, error) {
	return h.prepareAndPublishCodingDurableDynamicSurface(ctx, identity, codingDynamicCapabilityNeeds(), facts, constraints, budget, protocol, connectionID)
}
