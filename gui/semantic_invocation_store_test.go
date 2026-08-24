package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestAppSemanticInvocationIssuerPersistsSigningKeyAndGrantState(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	issuer, err := app.semanticInvocationIssuer()
	if err != nil {
		t.Fatalf("semanticInvocationIssuer: %v", err)
	}
	if len(app.semanticInvocationKey) != semanticInvocationKeySize {
		t.Fatalf("signing key length=%d", len(app.semanticInvocationKey))
	}
	if app.semanticInvocationStore == nil {
		t.Fatal("App did not install durable invocation store")
	}
	keyPath := filepath.Join(base, ".maclaw", "semantic-routing", "invocation-signing-key")
	if _, err := readSemanticInvocationKey(keyPath); err != nil {
		t.Fatalf("read durable signing key: %v", err)
	}
	_ = issuer
	keyBeforeClose := append([]byte(nil), app.semanticInvocationKey...)
	app.closeSemanticInvocationStore()
	restarted := &App{testHomeDir: base}
	if _, err := restarted.semanticInvocationIssuer(); err != nil {
		t.Fatalf("restart semanticInvocationIssuer: %v", err)
	}
	defer restarted.closeSemanticInvocationStore()
	if string(restarted.semanticInvocationKey) != string(keyBeforeClose) {
		t.Fatal("restart did not recover semantic invocation signing key")
	}
}

func TestSemanticSurfaceRecoversExistingOpaqueMaterializationWithoutReissuing(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces:     []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:              func(map[string]interface{}) string { return "captured" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	ctx := &LoopContext{ID: "stable-loop", Runtime: RuntimeContext{RequestID: "request-1"}}
	firstDefs, firstSurface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(ctx, "user", "capture primary screen", "lansenger")
	if err != nil || !handled || firstSurface == nil || len(firstDefs) != 1 {
		t.Fatalf("first defs=%#v surface=%#v handled=%v err=%v", firstDefs, firstSurface, handled, err)
	}
	firstName := extractToolName(firstDefs[0])
	firstGrant, ok := firstSurface.grants[firstName]
	if !ok {
		t.Fatalf("first grant missing for %q", firstName)
	}
	app.closeSemanticInvocationStore()

	restartedApp := &App{testHomeDir: base}
	restarted := &IMMessageHandler{app: restartedApp, registry: h.registry, unifiedClassifier: semanticTestClassifier(t)}
	defer restartedApp.closeSemanticInvocationStore()
	secondDefs, secondSurface, handled, err := restarted.semanticCallSurfaceForSharedTurnWithContext(ctx, "user", "capture primary screen", "lansenger")
	if err != nil || !handled || secondSurface == nil || len(secondDefs) != 1 {
		t.Fatalf("second defs=%#v surface=%#v handled=%v err=%v", secondDefs, secondSurface, handled, err)
	}
	secondName := extractToolName(secondDefs[0])
	secondGrant, ok := secondSurface.grants[secondName]
	if !ok || secondName != firstName || !reflect.DeepEqual(secondGrant, firstGrant) {
		t.Fatalf("recovery rematerialized adapter first=(%q,%+v) second=(%q,%+v)", firstName, firstGrant, secondName, secondGrant)
	}
}

func TestManagedSemanticLoopCancellationRevokesOutstandingSurface(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces:     []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:              func(map[string]interface{}) string { return "captured" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	ctx := &LoopContext{ID: "cancel-loop", Runtime: RuntimeContext{RequestID: "request-cancel"}}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(ctx, "user", "capture primary screen", "desktop")
	if err != nil || !handled || surface == nil || len(defs) != 1 {
		t.Fatalf("defs=%#v surface=%#v handled=%v err=%v", defs, surface, handled, err)
	}
	name := extractToolName(defs[0])
	grant, ok := surface.grants[name]
	if !ok {
		t.Fatalf("grant missing for %q", name)
	}
	remove := ctx.RegisterCancelHook((&sharedAgentLoopCallbacks{semanticSurface: surface}).cancelManagedSemanticSurface)
	defer remove()
	ctx.Cancel()
	if _, err := surface.issuer.ValidateAndConsume(grant, surface.scope, surface.plan); err == nil || !strings.Contains(err.Error(), "invocation_grant_revoked") {
		t.Fatalf("cancelled surface grant remained usable: %v", err)
	}
}

func TestIMLoopContextReplacementRevokesPriorManagedSurface(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces:     []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:              func(map[string]interface{}) string { return "captured" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	ctx := NewLoopContext("reused-loop", 1, nil)
	first := h.prepareIMLoopContext(ctx, IMUserMessage{
		UserID: "user", Platform: "desktop", RequestID: "request-a", Text: "capture primary screen",
	}, nil, false, false)
	defs, oldSurface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(first, "user", "capture primary screen", "desktop")
	if err != nil || !handled || oldSurface == nil || len(defs) != 1 {
		t.Fatalf("old defs=%#v surface=%#v handled=%v err=%v", defs, oldSurface, handled, err)
	}
	oldGrant, ok := oldSurface.grants[extractToolName(defs[0])]
	if !ok {
		t.Fatal("old surface has no visible grant")
	}

	second := h.prepareIMLoopContext(ctx, IMUserMessage{
		UserID: "user", Platform: "desktop", RequestID: "request-b", Text: "capture primary screen",
	}, nil, false, false)
	if _, err := oldSurface.issuer.ValidateAndConsume(oldGrant, oldSurface.scope, oldSurface.plan); err == nil || !strings.Contains(err.Error(), "invocation_grant_revoked") {
		t.Fatalf("replacement left prior grant usable: %v", err)
	}
	newDefs, newSurface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(second, "user", "capture primary screen", "desktop")
	if err != nil || !handled || newSurface == nil || len(newDefs) != 1 {
		t.Fatalf("new defs=%#v surface=%#v handled=%v err=%v", newDefs, newSurface, handled, err)
	}
	newGrant, ok := newSurface.grants[extractToolName(newDefs[0])]
	if !ok {
		t.Fatal("replacement surface has no visible grant")
	}
	if _, err := newSurface.issuer.ValidateAndConsume(newGrant, newSurface.scope, newSurface.plan); err != nil {
		t.Fatalf("replacement surface grant unusable: %v", err)
	}
}

func TestSemanticLoopInvocationSnapshotCannotMixReplacementIdentityAndGeneration(t *testing.T) {
	ctx := NewLoopContext("snapshot-loop", 1, nil)
	ctx.semanticInvocation = semanticLoopInvocationIdentity{
		RootTaskID: "semantic-root:before", TurnID: "semantic-turn:before", SessionID: "semantic-session:before",
	}
	ctx.semanticTurnGeneration = 7
	identity, generation := semanticLoopInvocationSnapshotFor(ctx)
	if identity.RootTaskID != "semantic-root:before" || identity.TurnID != "semantic-turn:before" || identity.SessionID != "semantic-session:before" || generation != 7 {
		t.Fatalf("snapshot = (%+v, %d)", identity, generation)
	}
	ctx.ReplaceSemanticTurn()
	identity, generation = semanticLoopInvocationSnapshotFor(ctx)
	if generation != 8 {
		t.Fatalf("replacement generation=%d, want 8", generation)
	}
	if identity.RootTaskID == "semantic-root:before" || identity.TurnID == "semantic-turn:before" || identity.SessionID == "semantic-session:before" {
		t.Fatalf("replacement retained prior identity: %+v", identity)
	}
}

func TestAppSemanticRouteStateStorePersistsAcrossRestart(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	store, err := app.semanticRouteStateStoreForApp()
	if err != nil || store == nil || app.semanticRouteStateStore == nil {
		t.Fatalf("route state store=%#v field=%#v err=%v", store, app.semanticRouteStateStore, err)
	}
	app.closeSemanticInvocationStore()
	restarted := &App{testHomeDir: base}
	store, err = restarted.semanticRouteStateStoreForApp()
	defer restarted.closeSemanticInvocationStore()
	if err != nil || store == nil || restarted.semanticRouteStateStore == nil {
		t.Fatalf("restarted route state store=%#v field=%#v err=%v", store, restarted.semanticRouteStateStore, err)
	}
}

func TestAppSemanticCoordinatorEncryptsArtifactsAndSweepsOnOpen(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	store, err := app.semanticArtifactStoreForApp()
	if err != nil || store == nil {
		t.Fatalf("artifact store=%#v err=%v", store, err)
	}
	scope := tool.InvocationScope{RootTaskID: "root", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "user"}
	payload, err := tool.NewArtifactPayload(scope, "selection:producer", "document", "text/plain", "c2VjcmV0LXBheWxvYWQ=", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(payload); err != nil {
		t.Fatalf("publish: %v", err)
	}
	app.closeSemanticInvocationStore()
	dbPath := filepath.Join(base, ".maclaw", "semantic-routing", "semantic-execution.db")
	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read coordinator db: %v", err)
	}
	if !strings.Contains(string(raw), "enc:v1:") {
		t.Fatal("GUI coordinator must encrypt artifact payloads at rest")
	}
	if strings.Contains(string(raw), "secret-payload") || strings.Contains(string(raw), "c2VjcmV0LXBheWxvYWQ=") {
		t.Fatal("plaintext artifact payload leaked into the coordinator database")
	}
	restarted := &App{testHomeDir: base}
	defer restarted.closeSemanticInvocationStore()
	reopened, err := restarted.semanticArtifactStoreForApp()
	if err != nil {
		t.Fatalf("restart after encrypted publish: %v", err)
	}
	got, err := reopened.PublishedArtifacts(scope, "selection:producer")
	if err != nil || len(got) != 1 || got[0].IntegrityDigest != payload.Ref.IntegrityDigest {
		t.Fatalf("restarted artifacts=%#v err=%v", got, err)
	}
}

func TestAppSemanticArtifactStorePersistsAcrossRestart(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	store, err := app.semanticArtifactStoreForApp()
	if err != nil || store == nil || app.semanticArtifactStore == nil {
		t.Fatalf("artifact store=%#v field=%#v err=%v", store, app.semanticArtifactStore, err)
	}
	app.closeSemanticInvocationStore()
	restarted := &App{testHomeDir: base}
	store, err = restarted.semanticArtifactStoreForApp()
	defer restarted.closeSemanticInvocationStore()
	if err != nil || store == nil || restarted.semanticArtifactStore == nil {
		t.Fatalf("restarted artifact store=%#v field=%#v err=%v", store, restarted.semanticArtifactStore, err)
	}
}

func TestAppSemanticDynamicCapabilityContractsPersistAcrossRestart(t *testing.T) {
	base := t.TempDir()
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: "user-1"}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
		ObservedBindingDigest: agentservice.DynamicMCPObservedBindingDigest("server-1", "search", map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "additionalProperties": false,
		}),
	}
	app := &App{testHomeDir: base}
	registry, err := app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatalf("dynamic registry: %v", err)
	}
	if err := registry.PublishMCPContract(principal, "server-1", "search", contract); err != nil {
		t.Fatalf("publish contract: %v", err)
	}
	app.closeSemanticInvocationStore()

	restarted := &App{testHomeDir: base}
	defer restarted.closeSemanticInvocationStore()
	recovered, err := restarted.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatalf("restarted dynamic registry: %v", err)
	}
	got, ok := recovered.ResolveMCPDynamicContract(context.Background(), principal, "server-1", "search")
	if !ok || got.Digest() != contract.Digest() {
		t.Fatalf("recovered contract=%+v ok=%v, want durable contract", got, ok)
	}
}

func TestAppSemanticDynamicEffectCoordinatorPersistsReceiptBoundOperationAcrossRestart(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	coordinator, err := app.semanticDynamicEffectCoordinatorForApp()
	if err != nil {
		t.Fatalf("dynamic effect coordinator: %v", err)
	}
	registry := tool.NewCapabilityRegistry("test-v1")
	if err := registry.Register(tool.CapabilityDescriptor{
		ID: "artifact.deliver.current_channel", Version: "v1",
		Effects: []tool.EffectClass{tool.EffectExternalEffect},
	}); err != nil {
		t.Fatalf("register delivery capability: %v", err)
	}
	if err := registry.Seal(); err != nil {
		t.Fatalf("seal registry: %v", err)
	}
	authorization, err := tool.NewParameterAuthorization(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"message": map[string]interface{}{"type": "string"}}, "required": []interface{}{"message"}, "additionalProperties": false,
	})
	if err != nil {
		t.Fatalf("authorize schema: %v", err)
	}
	snapshot, err := tool.NewToolCatalog(registry).Publish([]tool.ProviderSpec{{
		AdapterName: "dynamic_mcp", Binding: tool.ProviderBinding{Kind: "mcp", ProviderID: "server-effect", ImplementationID: "tool-effect", SchemaDigest: "schema-effect"},
		ParameterAuthorization: authorization,
		Provides:               []tool.CapabilityProvision{{Capability: "artifact.deliver.current_channel", Quality: 1}},
		Effects:                []tool.EffectClass{tool.EffectExternalEffect},
		Ready:                  true,
	}}, time.Now().UTC())
	if err != nil {
		t.Fatalf("publish snapshot: %v", err)
	}
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{RootTaskID: "root-effect", SessionID: "session-effect", TurnID: "turn-effect", Snapshot: snapshot, Needs: []tool.CapabilityNeed{{ID: "need-effect", Capability: "artifact.deliver.current_channel", Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	scope := tool.InvocationScope{RootTaskID: "root-effect", PlanID: plan.ID, SessionID: "session-effect", TurnID: "turn-effect", PrincipalID: "user-1"}
	routes, err := app.semanticRouteStateStoreForApp()
	if err != nil {
		t.Fatalf("route state store: %v", err)
	}
	if _, err := routes.PublishRevision(tool.RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatalf("publish route revision: %v", err)
	}
	issuer, err := app.semanticInvocationIssuer()
	if err != nil {
		t.Fatalf("invocation issuer: %v", err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("issue grants=%+v err=%v", grants, err)
	}
	invocation := agentservice.DynamicExternalEffectInvocation{
		Scope:     scope,
		Principal: agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: "user-1"},
		Selection: plan.Selections[0],
		Arguments: map[string]interface{}{"message": "bound"},
	}
	admission := tool.SemanticExecutionAdmission{Identity: tool.HostCallIdentity{Protocol: "test", ConnectionID: "connection-effect", CallID: "call-effect"}, Grant: grants[0], RequestDigest: "request-effect", Scope: scope, Selection: plan.Selections[0], Now: time.Now().UTC()}
	if _, action, err := coordinator.SemanticCoordinator.Admit(admission); err != nil || action != tool.HostCallAcquireAdmit {
		t.Fatalf("admit dynamic effect action=%q err=%v", action, err)
	}
	calls := 0
	prepared, err := coordinator.CoordinateDynamicExternalEffect(agentservice.WithDynamicSemanticAdmission(context.Background(), admission), invocation, func() (string, error) {
		calls++
		return "provider accepted locally", nil
	})
	if err != nil || prepared.State != agentservice.DynamicEffectReceiptAwaiting || prepared.OperationID == "" || calls != 1 {
		t.Fatalf("prepared receipt=%+v calls=%d err=%v", prepared, calls, err)
	}
	app.closeSemanticInvocationStore()

	restarted := &App{testHomeDir: base}
	defer restarted.closeSemanticInvocationStore()
	recovered, err := restarted.semanticDynamicEffectCoordinatorForApp()
	if err != nil {
		t.Fatalf("restart dynamic effect coordinator: %v", err)
	}
	operation, err := recovered.SemanticCoordinator.ExternalEffectOperation(prepared.OperationID)
	if err != nil || operation.State != tool.SemanticExternalEffectAwaitingReceipt || operation.OperationKey != prepared.OperationID || calls != 1 {
		t.Fatalf("recovered operation=%+v prepared=%+v calls=%d err=%v", operation, prepared, calls, err)
	}
	settled, err := recovered.SettleDynamicExternalEffect(agentservice.DynamicExternalEffectSettlement{
		Scope: invocation.Scope, Principal: invocation.Principal, Selection: invocation.Selection,
		OperationID: prepared.OperationID, State: agentservice.DynamicEffectReceiptAccepted,
		ReasonCode: "trusted_gateway_accepted", Receipt: "receipt-effect",
	})
	if err != nil || settled.State != agentservice.DynamicEffectReceiptAccepted || !settled.Reconciled {
		t.Fatalf("settled receipt=%+v err=%v", settled, err)
	}
	restarted.closeSemanticInvocationStore()
	afterReceipt := &App{testHomeDir: base}
	defer afterReceipt.closeSemanticInvocationStore()
	afterReceiptCoordinator, err := afterReceipt.semanticDynamicEffectCoordinatorForApp()
	if err != nil {
		t.Fatalf("receipt restart coordinator: %v", err)
	}
	finalOperation, err := afterReceiptCoordinator.SemanticCoordinator.ExternalEffectOperation(prepared.OperationID)
	if err != nil || finalOperation.State != tool.SemanticExternalEffectSucceeded || finalOperation.ReceiptDigest != tool.SchemaDigest([]byte("receipt-effect")) || calls != 1 {
		t.Fatalf("reconciled operation=%+v calls=%d err=%v", finalOperation, calls, err)
	}
}

func TestSemanticSurfaceRefreshNeverRestoresLegacyTools(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces:     []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:              func(map[string]interface{}) string { return "captured" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user", "capture primary screen", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("surface=%#v defs=%#v handled=%v err=%v", surface, defs, handled, err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, userID: "user", userText: "capture primary screen", platform: "lansenger",
		semanticSurface: surface, tools: defs, surfaceRefreshPending: true,
	}
	if !cb.RefreshAfterToolExecution(extractToolName(defs[0])) {
		t.Fatal("semantic refresh was not applied")
	}
	if len(cb.tools) != 1 || extractToolName(cb.tools[0]) != extractToolName(defs[0]) {
		t.Fatalf("semantic refresh restored non-semantic tools: %#v", cb.tools)
	}
}
