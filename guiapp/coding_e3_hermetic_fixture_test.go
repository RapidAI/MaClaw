package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingE3HermeticEnv is the hermetic E3 assembly environment: a loopback
// Responses-WS provider, a loopback MCP JSON-RPC provider whose lifecycle
// observation satisfies the production capability needs, and the test-only
// qualification override that rehearses the real assembly. Nothing in this
// file reimplements composition logic; every reservation goes through the
// production relay, factory, validation, durable coordinator, and fixed
// bridge.
type codingE3HermeticEnv struct {
	app         *App
	handler     *IMMessageHandler
	mcpServerID string
	mcpCalls    int32
	mcpResult   func(toolName string, args map[string]interface{}) string
	wsURL       string
	wsConnSeq   int32
	// wsScript runs per accepted socket after its response.create frame was
	// read. seq is the 1-based dial order for this environment.
	wsScript func(seq int, conn *websocket.Conn, frame []byte)
	// wsOnFrame, when set, runs after a socket's response.create frame was
	// read and before its script.
	wsOnFrame        func(seq int)
	cachedTools      []MCPToolView
	capabilityByTool map[string]tool.CapabilityID
	// wsFrames retains each socket's response.create payload, keyed by dial
	// order, so scenario assertions can inspect exactly what a reservation sent.
	wsFrames sync.Map
	// httpClient talks plain HTTP to the loopback provider (the S0.5
	// compatibility path uses HTTP Responses streaming, not the WS channel).
	httpClient *http.Client
	// httpSSE, when set, is the SSE body served for plain HTTP POSTs.
	httpSSE string
}

var codingE3MCPTools = []struct {
	name       string
	capability tool.CapabilityID
}{
	{name: "read_workspace", capability: tool.CapabilityFSReadLocal},
	{name: "write_workspace", capability: tool.CapabilityFSWriteLocal},
	{name: "inspect_repo", capability: tool.CapabilityRepoInspectVCS},
	{name: "verify_build", capability: tool.CapabilityBuildVerifyLocal},
}

// codingE3ContractEffects declares the honest ontology effects per seeded
// tool. The planner rejects a provider whose contract effects fall outside the
// capability descriptor's declared effects (fs.write.local and
// build.verify.local are EffectSensitive, not read-only), so a read-only or
// external declaration would fail closed at plan time.
func codingE3ContractEffects(toolName string) []tool.EffectClass {
	switch toolName {
	case "write_workspace", "verify_build":
		return []tool.EffectClass{tool.EffectSensitive}
	default:
		return []tool.EffectClass{tool.EffectReadOnly}
	}
}

func codingE3MCPInputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
		"required":             []interface{}{"query"},
		"additionalProperties": false,
	}
}

func newCodingE3HermeticEnv(t *testing.T) *codingE3HermeticEnv {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	t.Cleanup(func() {
		// The real prompt/tool surface opens the enterprise knowledge store;
		// release it before TempDir removal, just as other app-level tests do.
		if app.enterpriseClient != nil {
			_ = app.enterpriseClient.Close()
			app.enterpriseClient = nil
		}
		// Static-surface audit observations are written through the app-owned
		// audit logger.  Close it before TempDir cleanup; on Windows an async
		// writer otherwise keeps audit-YYYY-MM-DD.jsonl open and makes the E4
		// kill-switch drill flaky.
		if app.auditLog != nil {
			_ = app.auditLog.Close()
			app.auditLog = nil
		}
	})
	env := &codingE3HermeticEnv{app: app, handler: &IMMessageHandler{app: app}, mcpServerID: "e3-mcp"}

	mcpSrv := httptest.NewServer(http.HandlerFunc(env.serveMCP))
	t.Cleanup(mcpSrv.Close)
	registry := NewMCPRegistry(app)
	app.mcpRegistry = registry
	if err := registry.Register(corelib.MCPServerEntry{ID: env.mcpServerID, Name: "e3-mcp", EndpointURL: mcpSrv.URL}); err != nil {
		t.Fatalf("register loopback MCP provider: %v", err)
	}
	if err := registry.HealthCheck(env.mcpServerID); err != nil {
		t.Fatalf("loopback MCP health check: %v", err)
	}
	cached, observed := registry.CachedServerTools(env.mcpServerID)
	if !observed || len(cached) != len(codingE3MCPTools) {
		t.Fatalf("lifecycle tools observation=%v tools=%d", observed, len(cached))
	}
	capabilityByTool := make(map[string]tool.CapabilityID, len(codingE3MCPTools))
	for _, toolDef := range codingE3MCPTools {
		capabilityByTool[toolDef.name] = toolDef.capability
	}
	env.cachedTools = cached
	env.capabilityByTool = capabilityByTool
	env.seedContracts(t, "desktop", "principal")

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			// The S0.5 compatibility path for a responses-ws configuration is
			// the HTTP Responses stream: serve the scripted SSE answer.
			if env.httpSSE != "" {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(env.httpSSE))
			}
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		seq := int(atomic.AddInt32(&env.wsConnSeq, 1))
		_, frame, err := conn.ReadMessage()
		if err != nil {
			return
		}
		env.wsFrames.Store(seq, append([]byte(nil), frame...))
		if env.wsOnFrame != nil {
			env.wsOnFrame(seq)
		}
		script := env.wsScript
		if script == nil {
			// Reservations that never dispatch (dual-executor isolation, child
			// handoff) hold an idle socket; drain until the client closes.
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}
		script(seq, conn, frame)
	}))
	t.Cleanup(wsSrv.Close)
	env.wsURL = wsSrv.URL
	env.httpClient = wsSrv.Client()
	env.installQualification(t)
	return env
}

// seedContracts publishes the dynamic capability contracts for one principal
// scope against the lifecycle-observed tool bindings. Execution-time inventory
// resolves contracts only for this exact principal, so verified-ingress
// scenarios seed their own subject.
func (env *codingE3HermeticEnv) seedContracts(t *testing.T, tenantID, principalID string) {
	t.Helper()
	contracts, err := env.app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatalf("load contract registry: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenantID, UserID: principalID}
	for _, discovered := range env.cachedTools {
		capability, ok := env.capabilityByTool[discovered.Name]
		if !ok {
			t.Fatalf("observed tool %q has no seeded capability", discovered.Name)
		}
		contract := agentservice.DynamicCapabilityContract{
			Provisions:            []tool.CapabilityProvision{{Capability: capability, Quality: 2}},
			Effects:               codingE3ContractEffects(discovered.Name),
			ObservedBindingDigest: agentservice.DynamicMCPObservedBindingDigest(env.mcpServerID, discovered.Name, discovered.InputSchema),
		}
		if err := contracts.PublishMCPContract(principal, env.mcpServerID, discovered.Name, contract); err != nil {
			t.Fatalf("publish contract for %q: %v", discovered.Name, err)
		}
	}
}

func (env *codingE3HermeticEnv) serveMCP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     interface{} `json:"id"`
		Method string      `json:"method"`
		Params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		} `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	w.Header().Set("Content-Type", "application/json")
	result := func(payload interface{}) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": payload})
	}
	switch req.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "e3-mcp-session")
		result(map[string]interface{}{"protocolVersion": "2025-03-26", "capabilities": map[string]interface{}{}, "serverInfo": map[string]interface{}{"name": "e3-fake", "version": "1.0.0"}})
	case "tools/list":
		tools := make([]map[string]interface{}, 0, len(codingE3MCPTools))
		for _, toolDef := range codingE3MCPTools {
			tools = append(tools, map[string]interface{}{"name": toolDef.name, "description": "e3 loopback " + toolDef.name, "inputSchema": codingE3MCPInputSchema()})
		}
		result(map[string]interface{}{"tools": tools})
	case "tools/call":
		atomic.AddInt32(&env.mcpCalls, 1)
		resultFn := env.mcpResult
		if resultFn == nil {
			resultFn = func(toolName string, _ map[string]interface{}) string { return "e3-mcp-result:" + toolName }
		}
		result(map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": resultFn(req.Params.Name, req.Params.Arguments)}}})
	default:
		result(map[string]interface{}{})
	}
}

// installQualification installs the E3 rehearsal literal: the production
// default (which carries only the E1/E2/E3 machine evidence) completed with
// the remaining gate fields and Wired/Enabled, reachable only through this
// test-only override. The production default itself stays disabled.
func (env *codingE3HermeticEnv) installQualification(t *testing.T) {
	t.Helper()
	full := codingDynamicProductionAdapterForConfig(env.cfg())
	full.CatalogReceiptPolicyCovered = true
	full.Wired = true
	full.Enabled = true
	if !full.eligible() {
		t.Fatalf("E3 rehearsal qualification is incomplete: %#v", full)
	}
	previous := codingDynamicProductionAdapterQualificationOverride
	codingDynamicProductionAdapterQualificationOverride = &full
	t.Cleanup(func() { codingDynamicProductionAdapterQualificationOverride = previous })
}

func (env *codingE3HermeticEnv) cfg() corelib.MaclawLLMConfig {
	return corelib.MaclawLLMConfig{URL: env.wsURL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses-ws"}
}

func codingE3Identity() *trustedCodingInvocationIdentity {
	return &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root-e3", TurnID: "turn"}
}

// newLocalCallbacks constructs the real local callback composition. The relay
// must attach through the production tryAttach path inside the constructor;
// anything else means the assembly regressed.
func (env *codingE3HermeticEnv) newLocalCallbacks(t *testing.T, identity *trustedCodingInvocationIdentity, loopCtx *LoopContext) *codingSubAgentCallbacks {
	t.Helper()
	subagent := &CodingSubAgent{handler: env.handler, loopCtx: loopCtx, cfg: env.cfg(), dynamicInvocationIdentity: identity, projectPath: t.TempDir()}
	cb := newCodingSubAgentCallbacks(subagent, &TaskItem{Title: "e3 hermetic"}, "", "", nil)
	if cb.dynamicLifecycleRelay == nil {
		t.Fatal("production callback constructor did not attach the qualified relay")
	}
	return cb
}

// newRemoteCallbacks mirrors the production remote run path: struct
// construction followed by the single tryAttach boundary.
func (env *codingE3HermeticEnv) newRemoteCallbacks(t *testing.T, identity *trustedCodingInvocationIdentity, loopCtx *LoopContext) *remoteCodingCallbacks {
	t.Helper()
	remote := &RemoteCodingSubAgent{handler: env.handler, loopCtx: loopCtx, cfg: env.cfg(), dynamicInvocationIdentity: identity}
	cb := &remoteCodingCallbacks{agent: remote, task: "e3 hermetic"}
	cb.tryAttachQualifiedDynamicLifecycleRelay()
	if cb.dynamicLifecycleRelay == nil {
		t.Fatal("production remote tryAttach did not attach the qualified relay")
	}
	return cb
}

// newCallbacks builds the real callback composition for one matrix family.
func (env *codingE3HermeticEnv) newCallbacks(t *testing.T, family string, identity *trustedCodingInvocationIdentity, loopCtx *LoopContext) agent.LoopCallbacks {
	t.Helper()
	if family == "local" {
		return env.newLocalCallbacks(t, identity, loopCtx)
	}
	return env.newRemoteCallbacks(t, identity, loopCtx)
}

// codingE3LoopHost wraps the real callbacks with ledger recording and scenario
// controls only. Every composition method delegates; nothing about the epoch
// source, prompt, iteration budget, or authorization is reimplemented here.
type codingE3LoopHostBase struct {
	agent.LoopCallbacks
	ledger           *testCodingReservationLedger
	control          *testCodingDynamicRunLoopControl
	receiptExecution agent.ToolCallExecutionContext
	// executions records the exact reservation tuples RunLoop rendered, in
	// order: index 0 is the first request's reservation.
	executions []agent.ToolCallExecutionContext
}

func (h *codingE3LoopHostBase) BuildToolsForBoundModelRequest(userText string, iteration int, execution agent.ToolCallExecutionContext) []map[string]interface{} {
	if h.ledger != nil {
		h.ledger.record("reserved", execution)
	}
	definitions := h.LoopCallbacks.(agent.BoundModelRequestToolSurfaceRenderer).BuildToolsForBoundModelRequest(userText, iteration, execution)
	if h.ledger != nil && len(definitions) != 0 {
		h.ledger.record("prepared", execution)
	}
	h.receiptExecution = execution
	return definitions
}

func (h *codingE3LoopHostBase) RenderPublishedBoundToolSurface(userText string, iteration int, execution agent.ToolCallExecutionContext) agent.BoundToolSurfaceRender {
	if h.ledger != nil {
		h.ledger.record("reserved", execution)
	}
	rendered := h.LoopCallbacks.(agent.PublishedBoundModelRequestToolSurfaceRenderer).RenderPublishedBoundToolSurface(userText, iteration, execution)
	if h.ledger != nil && rendered.Published {
		h.ledger.record("prepared", execution)
	}
	h.receiptExecution = execution
	h.executions = append(h.executions, execution)
	return rendered
}

func (h *codingE3LoopHostBase) OnToolSurfaceReceipt(receipt agent.ToolSurfaceReceipt) {
	if h.ledger == nil {
		return
	}
	event := "receipt:"
	if receipt.Verified {
		event += receipt.PayloadDigest + ":" + receipt.WirePayloadHash + ":" + receipt.AuditDigest
	} else {
		event += "failure:" + receipt.Failure
	}
	h.ledger.record(event, h.receiptExecution)
}

func (h *codingE3LoopHostBase) BindToolSurfaceResponse(execution agent.ToolCallExecutionContext) error {
	err := h.LoopCallbacks.(agent.ToolSurfaceResponseBinder).BindToolSurfaceResponse(execution)
	if err == nil && h.ledger != nil {
		h.ledger.record("bound:"+execution.ResponseID, execution)
	}
	if err == nil && h.control != nil && h.control.afterBind != nil {
		h.control.afterBind()
	}
	return err
}

func (h *codingE3LoopHostBase) OnToolSurfaceDisposition(execution agent.ToolCallExecutionContext, disposition agent.ToolSurfaceDisposition) {
	if h.ledger != nil {
		h.ledger.record("terminal:"+string(disposition), execution)
	}
	h.LoopCallbacks.(agent.ToolSurfaceDispositionObserver).OnToolSurfaceDisposition(execution, disposition)
}

func (h *codingE3LoopHostBase) ExecuteToolCallWithContext(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) agent.ToolExecutionResult {
	if h.control != nil {
		h.control.executions++
	}
	return h.LoopCallbacks.(agent.ToolCallContextExecutor).ExecuteToolCallWithContext(name, argsJSON, callID, execution)
}

func (h *codingE3LoopHostBase) ShouldStop() bool {
	return h.control != nil && (h.control.stop || h.control.stopFlag.Load())
}

// The delegates below forward the optional RunLoop extension interfaces the
// real callbacks implement. Embedding agent.LoopCallbacks would otherwise hide
// them and silently drop the loop onto the static compatibility path, which is
// exactly the assembly shortcut this suite must not take.

func (h *codingE3LoopHostBase) ReserveToolSurfaceRequestChannel(ctx context.Context, cfg corelib.MaclawLLMConfig) (agent.ToolSurfaceRequestChannel, error) {
	return h.LoopCallbacks.(agent.ToolSurfaceRequestChannelProvider).ReserveToolSurfaceRequestChannel(ctx, cfg)
}

func (h *codingE3LoopHostBase) BeginToolSurfaceEpoch(iteration int) string {
	return h.LoopCallbacks.(agent.ToolSurfaceEpochProvider).BeginToolSurfaceEpoch(iteration)
}

func (h *codingE3LoopHostBase) ToolSurfaceAuditEvidence(execution agent.ToolCallExecutionContext) agent.ToolSurfacePlanEvidence {
	return h.LoopCallbacks.(agent.ToolSurfaceAuditEvidenceProvider).ToolSurfaceAuditEvidence(execution)
}

func (h *codingE3LoopHostBase) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	return h.LoopCallbacks.(agent.LLMRequestContextProvider).LLMRequestContext(iteration)
}

func (h *codingE3LoopHostBase) LLMReplanRequested() bool {
	return h.LoopCallbacks.(agent.LLMReplanAware).LLMReplanRequested()
}

func (h *codingE3LoopHostBase) TryFinalizeLLMResponse() bool {
	return h.LoopCallbacks.(agent.LLMFinalizationGuard).TryFinalizeLLMResponse()
}

func (h *codingE3LoopHostBase) RefreshLLMAuth(ctx context.Context) (corelib.MaclawLLMConfig, bool) {
	return h.LoopCallbacks.(agent.LLMAuthRefresh).RefreshLLMAuth(ctx)
}

func (h *codingE3LoopHostBase) RouteTurn(userText string) (corelib.MaclawLLMConfig, agent.RouteDecision, bool) {
	return h.LoopCallbacks.(agent.TurnRouter).RouteTurn(userText)
}

func (h *codingE3LoopHostBase) ExecuteToolStructured(name, argsJSON string) agent.ToolExecutionResult {
	return h.LoopCallbacks.(agent.StructuredToolExecutor).ExecuteToolStructured(name, argsJSON)
}

func (h *codingE3LoopHostBase) ContainToolSurfaceAmbiguousDelivery() bool {
	return h.LoopCallbacks.(agent.ToolSurfaceAmbiguousDeliveryContainment).ContainToolSurfaceAmbiguousDelivery()
}

func (h *codingE3LoopHostBase) OnToolSurfaceAttemptStarted(execution agent.ToolCallExecutionContext) {
	h.LoopCallbacks.(agent.ToolSurfaceAttemptObserver).OnToolSurfaceAttemptStarted(execution)
}

func (h *codingE3LoopHostBase) OnToolSurfaceAttemptFinished(execution agent.ToolCallExecutionContext, delivery agent.ToolSurfaceDeliveryState) {
	h.LoopCallbacks.(agent.ToolSurfaceAttemptObserver).OnToolSurfaceAttemptFinished(execution, delivery)
}

func (h *codingE3LoopHostBase) BuildToolsForModelRequest(userText string, iteration int) []map[string]interface{} {
	return h.LoopCallbacks.(agent.ModelRequestToolSurfaceRenderer).BuildToolsForModelRequest(userText, iteration)
}

// The hermetic host is split per family because the real compositions differ:
// the local callback also implements agent.ToolResultProjector. Each host
// exposes exactly the optional interfaces of its own composition — no more,
// no less.
type codingE3LocalLoopHost struct {
	*codingE3LoopHostBase
}

func (h *codingE3LocalLoopHost) ProjectToolResult(name string, result agent.ToolExecutionResult) string {
	return h.LoopCallbacks.(agent.ToolResultProjector).ProjectToolResult(name, result)
}

type codingE3RemoteLoopHost struct {
	*codingE3LoopHostBase
}

// codingE3AssertHostCompositionParity fails unless the wrapper still exposes
// every optional interface the real composition implements. It guards against
// a future callback extension silently disappearing from this suite.
func codingE3AssertHostCompositionParity(t *testing.T, family string, host agent.LoopCallbacks) {
	t.Helper()
	want := map[string]bool{
		"ToolResultProjector": family == "local",
	}
	for name, ok := range map[string]bool{
		"ToolSurfaceRequestChannelProvider":             implementsInterface[agent.ToolSurfaceRequestChannelProvider](host),
		"BoundModelRequestToolSurfaceRenderer":          implementsInterface[agent.BoundModelRequestToolSurfaceRenderer](host),
		"PublishedBoundModelRequestToolSurfaceRenderer": implementsInterface[agent.PublishedBoundModelRequestToolSurfaceRenderer](host),
		"ToolSurfaceAuditEvidenceProvider":              implementsInterface[agent.ToolSurfaceAuditEvidenceProvider](host),
		"ToolSurfaceResponseBinder":                     implementsInterface[agent.ToolSurfaceResponseBinder](host),
		"ToolCallContextExecutor":                       implementsInterface[agent.ToolCallContextExecutor](host),
		"ToolSurfaceDispositionObserver":                implementsInterface[agent.ToolSurfaceDispositionObserver](host),
		"LLMRequestContextProvider":                     implementsInterface[agent.LLMRequestContextProvider](host),
		"LLMReplanAware":                                implementsInterface[agent.LLMReplanAware](host),
		"LLMFinalizationGuard":                          implementsInterface[agent.LLMFinalizationGuard](host),
		"ToolSurfaceEpochProvider":                      implementsInterface[agent.ToolSurfaceEpochProvider](host),
		"ToolSurfaceAttemptObserver":                    implementsInterface[agent.ToolSurfaceAttemptObserver](host),
		"ToolSurfaceAmbiguousDeliveryContainment":       implementsInterface[agent.ToolSurfaceAmbiguousDeliveryContainment](host),
		"LLMAuthRefresh":                                implementsInterface[agent.LLMAuthRefresh](host),
		"TurnRouter":                                    implementsInterface[agent.TurnRouter](host),
		"StructuredToolExecutor":                        implementsInterface[agent.StructuredToolExecutor](host),
		"ModelRequestToolSurfaceRenderer":               implementsInterface[agent.ModelRequestToolSurfaceRenderer](host),
	} {
		if !ok {
			t.Fatalf("hermetic host lost composition interface %s", name)
		}
	}
	for name, required := range want {
		if got := implementsInterface[agent.ToolResultProjector](host); got != required {
			t.Fatalf("hermetic host composition interface %s=%v, want %v", name, got, required)
		}
	}
}

func implementsInterface[T any](host agent.LoopCallbacks) bool {
	_, ok := host.(T)
	return ok
}

// --- loopback WS provider scripting helpers ---

func codingE3WSWrite(t *testing.T, conn *websocket.Conn, frames ...string) {
	t.Helper()
	for _, frame := range frames {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			t.Errorf("write frame: %v", err)
			return
		}
	}
}

// codingE3WSFirstToolName extracts the first rendered tool name from the
// response.create frame. The fake provider answers with the exact alias the
// real assembly published rather than any hard-coded name.
func codingE3WSFirstToolName(t *testing.T, frame []byte) string {
	t.Helper()
	var payload struct {
		Tools []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(frame, &payload); err != nil || len(payload.Tools) == 0 {
		t.Fatalf("response.create frame has no rendered tools: %v", err)
	}
	name, _ := payload.Tools[0]["name"].(string)
	if name == "" {
		t.Fatalf("first rendered tool has no name: %#v", payload.Tools[0])
	}
	return name
}

// codingE3WSReadOnlyToolName picks the rendered alias bound to a read-only
// loopback tool. Execution scenarios stay on read-only selections so the
// hermetic bridge needs no external-receipt coordinator; the sensitive
// providers still satisfy the production plan's needs at publish time. The
// descriptions are the capability ontology summaries carried verbatim into
// the rendered definitions.
func codingE3WSReadOnlyToolName(t *testing.T, frame []byte) string {
	t.Helper()
	var payload struct {
		Tools []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(frame, &payload); err != nil || len(payload.Tools) == 0 {
		t.Fatalf("response.create frame has no rendered tools: %v", err)
	}
	for _, definition := range payload.Tools {
		description, _ := definition["description"].(string)
		if strings.HasPrefix(description, "Read or search local filesystem content") || strings.HasPrefix(description, "Inspect version-control status and diffs") {
			if name, _ := definition["name"].(string); name != "" {
				return name
			}
		}
	}
	t.Fatalf("no read-only alias in rendered tools: %#v", payload.Tools)
	return ""
}

func codingE3WSToolCallFrames(responseID, alias, args string) []string {
	argsJSON, _ := json.Marshal(args)
	return []string{
		fmt.Sprintf(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call-%s","name":"%s"}}`, responseID, alias),
		fmt.Sprintf(`{"type":"response.function_call_arguments.done","output_index":0,"arguments":%s}`, string(argsJSON)),
		fmt.Sprintf(`{"type":"response.completed","response":{"id":"%s","status":"completed"}}`, responseID),
	}
}

func codingE3WSFinalTextFrames(responseID, text string) []string {
	textJSON, _ := json.Marshal(text)
	return []string{
		fmt.Sprintf(`{"type":"response.output_text.delta","delta":%s}`, string(textJSON)),
		fmt.Sprintf(`{"type":"response.completed","response":{"id":"%s","status":"completed"}}`, responseID),
	}
}

// codingE3ScriptToolCall serves one tool-call response for the read-only
// rendered alias.
func codingE3ScriptToolCall(t *testing.T, responseID string) func(int, *websocket.Conn, []byte) {
	return func(_ int, conn *websocket.Conn, frame []byte) {
		codingE3WSWrite(t, conn, codingE3WSToolCallFrames(responseID, codingE3WSReadOnlyToolName(t, frame), `{"query":"e3"}`)...)
	}
}

func codingE3ScriptFinalText(t *testing.T, responseID, text string) func(int, *websocket.Conn, []byte) {
	return func(_ int, conn *websocket.Conn, frame []byte) {
		codingE3WSWrite(t, conn, codingE3WSFinalTextFrames(responseID, text)...)
	}
}

func codingE3WSConnCount(env *codingE3HermeticEnv) int {
	return int(atomic.LoadInt32(&env.wsConnSeq))
}

func codingE3MCPCallCount(env *codingE3HermeticEnv) int {
	return int(atomic.LoadInt32(&env.mcpCalls))
}

// codingE3FrameContains reports whether the response.create payload of the
// given dial order contains the text.
func codingE3FrameContains(env *codingE3HermeticEnv, seq int, text string) bool {
	frame, ok := env.wsFrames.Load(seq)
	if !ok {
		return false
	}
	return strings.Contains(string(frame.([]byte)), text)
}
