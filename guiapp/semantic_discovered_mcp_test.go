package guiapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func listedInstalledMCP(definitions []map[string]interface{}, serverID, toolName string) (name, desc string) {
	want := tool.InstalledMCPModelFunctionName(serverID, toolName)
	for _, def := range definitions {
		if extractToolName(def) != want {
			continue
		}
		fn, _ := def["function"].(map[string]interface{})
		desc, _ = fn["description"].(string)
		return want, desc
	}
	return "", ""
}

func discoveredMCPObjectSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"content": map[string]interface{}{"type": "string"}},
		"additionalProperties": false,
	}
}

func newDiscoveredMCPHandler(t *testing.T, serverID string, tools []MCPToolView, publish bool) (*IMMessageHandler, *LoopContext) {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	seedObservedRemoteMCP(t, app, serverID, tools)
	if publish {
		if err := app.publishReviewedMCPServerTools(serverID, tools); err != nil {
			t.Fatal(err)
		}
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	loop := &LoopContext{UserID: "desktop-user", Platform: "desktop"}
	h.setSessionLoopCtx("desktop-user", loop)
	return h, loop
}

func TestDiscoveredCompoundMCPBindsOntoLegacySurface(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	out := h.toolDiscoverToolForOwner("desktop-user", map[string]interface{}{"need": "post_work_log_add"})
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || !strings.Contains(out, "tengyun/post_work_log_add") || !strings.Contains(out, discoveredMCPBoundSentence) {
		t.Fatalf("discover did not bind the named tool: %s", out)
	}
	binds := loop.discoveredMCPBindList()
	if len(binds) != 1 || binds[0].ServerID != "tengyun" || binds[0].ToolName != "post_work_log_add" {
		t.Fatalf("binds=%+v", binds)
	}
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		tools: []map[string]interface{}{readFile},
	}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if cb.semanticSurface != nil || cb.discoveredMCPSurface == nil {
		t.Fatalf("semantic=%v sidecar=%v", cb.semanticSurface != nil, cb.discoveredMCPSurface != nil)
	}
	for _, selection := range cb.discoveredMCPSurface.plan.Selections {
		if selection.FitProof.MatchedCapability == agentservice.CapabilityRecordUpdateWorklog {
			t.Fatalf("unreviewed name provisioned record.update.worklog: %+v", selection)
		}
		want := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "post_work_log_add")
		if selection.FitProof.MatchedCapability != want {
			t.Fatalf("selection capability=%s want %s", selection.FitProof.MatchedCapability, want)
		}
	}
	grantName, grantDesc := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	sawRead := false
	for _, def := range cb.tools {
		if extractToolName(def) == "read_file" {
			sawRead = true
		}
	}
	if !sawRead || grantName != "tengyun__post_work_log_add" || !strings.Contains(grantDesc, "post_work_log_add") || !strings.Contains(grantDesc, "tengyun") {
		t.Fatalf("legacy list missing read_file or the stable grant: name=%q desc=%q tools=%#v", grantName, grantDesc, cb.tools)
	}
	sidecar := cb.discoveredMCPSurface
	cb.tools = []map[string]interface{}{readFile}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if cb.discoveredMCPSurface != sidecar {
		t.Fatal("unchanged bind set published a second surface")
	}
	if cb.semanticSurface != nil {
		t.Fatal("request epoch promoted the sidecar")
	}
	var selection tool.PlannedSelection
	for _, item := range cb.discoveredMCPSurface.plan.Selections {
		if item.FitProof.MatchedCapability == agentservice.AutoMCPCapabilityIDForBinding("tengyun", "post_work_log_add") {
			selection = item
			break
		}
	}
	if _, asked := cb.mcpServerWriteGateForSelection(selection, nil); asked {
		t.Fatal("installed MCP asked for a chat confirmation")
	}
	if _, ok := cb.discoveredMCPSurface.grants[grantName]; !ok {
		t.Fatal("listing the installed tool consumed its grant")
	}
	legacy := cb.ExecuteTool("read_file", `{}`)
	if strings.Contains(legacy, "__ASK_USER__") || strings.Contains(legacy, "post_work_log_add") {
		t.Fatalf("legacy tool entered the MCP grant path: %s", legacy)
	}
}

func TestInstalledMCPListedWithoutDiscovery(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	managed := &semanticCallSurface{rendered: map[string]bool{}}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		semanticSurface: managed,
		tools:           []map[string]interface{}{readFile},
	}
	cb.publishReadyInstalledMCPTools()
	if cb.semanticSurface != managed {
		t.Fatal("installed MCP replaced the managed surface")
	}
	name, desc := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	if name != "tengyun__post_work_log_add" || !strings.Contains(desc, "post_work_log_add") || !strings.Contains(desc, "tengyun") {
		t.Fatalf("installed tool was not listed: name=%q desc=%q tools=%#v", name, desc, cb.tools)
	}
	if !managed.rendered[name] {
		t.Fatal("managed request did not record the installed MCP name")
	}
	if len(cb.discoveredMCPSurface.plan.Selections) == 0 {
		t.Fatal("installed MCP plan had no selection")
	}
	if _, asked := cb.mcpServerWriteGateForSelection(cb.discoveredMCPSurface.plan.Selections[0], nil); asked {
		t.Fatal("installed MCP asked for a chat confirmation")
	}
}

// The managed plan can list an installed MCP tool it did not grant.
// FilterToolDefinitionsByAuthorizer must keep that live sidecar grant.
// A retired-only name and a turn that has closed stay unauthorized.
func TestInstalledMCPSidecarStaysAuthorizedBesideManagedSurface(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	managed := &semanticCallSurface{
		rendered: map[string]bool{},
		grants: map[string]tool.InvocationGrant{
			"read_file": {SelectionID: "selection:read"},
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		semanticSurface: managed,
		tools:           []map[string]interface{}{readFile},
	}
	cb.publishReadyInstalledMCPTools()
	name, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	if name != "tengyun__post_work_log_add" {
		t.Fatalf("installed tool was not listed: %q", name)
	}
	if !cb.IsToolAllowed(name) || !cb.IsToolAllowedForPromptProfile(name, agent.PromptProfileFull) || !cb.IsToolAllowedForPromptProfile(name, agent.PromptProfileLight) {
		t.Fatal("managed surface rejected the installed MCP tool the request listed")
	}
	if cb.IsToolAllowed("web_fetch") {
		t.Fatal("managed surface admitted a tool neither plan granted")
	}
	filtered := agent.FilterToolDefinitionsByAuthorizer(cb, cb.tools)
	if got, _ := listedInstalledMCP(filtered, "tengyun", "post_work_log_add"); got != name {
		t.Fatalf("authorizer removed the installed tool before the model request: %#v", filtered)
	}
	sawRead := false
	for _, def := range filtered {
		if extractToolName(def) == "read_file" {
			sawRead = true
		}
	}
	if !sawRead {
		t.Fatal("authorizer removed the managed host tool")
	}
	grant, ok := cb.discoveredMCPSurface.grants[name]
	if !ok {
		t.Fatal("listing the installed tool left no live sidecar grant")
	}
	delete(cb.discoveredMCPSurface.grants, name)
	if cb.discoveredMCPSurface.retiredGrants == nil {
		cb.discoveredMCPSurface.retiredGrants = map[string]tool.InvocationGrant{}
	}
	cb.discoveredMCPSurface.retiredGrants[name] = grant
	if cb.IsToolAllowed(name) || cb.IsToolAllowedForPromptProfile(name, agent.PromptProfileFull) {
		t.Fatal("a retired installed MCP name stayed authorized")
	}
	cb.discoveredMCPSurface.grants[name] = grant
	loop.semanticTurnAnswerOnly = true
	if cb.IsToolAllowed(name) || cb.IsToolAllowedForPromptProfile(name, agent.PromptProfileLight) {
		t.Fatal("a closed turn authorized the installed MCP tool")
	}
}

// A managed turn and the installed-MCP sidecar share one request epoch.
// A response from the previous request must not run the stable server__tool
// name after the next request has begun. The same response may still call
// that name when its own sidecar epoch is current, or when an earlier call
// in the batch retired the epoch and issued the same family's next grant.
func TestInstalledMCPSidecarUsesTheRequestEpoch(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	managed := &semanticCallSurface{rendered: map[string]bool{}}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		semanticSurface: managed,
		tools:           []map[string]interface{}{readFile},
	}
	epochBeforePublish := cb.BeginToolSurfaceEpoch(0)
	cb.publishReadyInstalledMCPTools()
	name, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	if name != "tengyun__post_work_log_add" || cb.discoveredMCPSurface == nil || cb.discoveredMCPSurface == managed {
		t.Fatalf("sidecar was not published beside the managed surface: name=%q", name)
	}
	if !cb.discoveredMCPSurface.epochIsCurrent(epochBeforePublish) {
		t.Fatal("a sidecar created while preparing the request did not adopt that request's epoch")
	}
	if !cb.installedMCPSidecarEpochAdmits(epochBeforePublish, name) {
		t.Fatal("current request epoch did not admit the installed tool")
	}

	epoch := cb.BeginToolSurfaceEpoch(1)
	if epoch == "" || epoch == epochBeforePublish || !cb.discoveredMCPSurface.epochIsCurrent(epoch) {
		t.Fatalf("successor epoch was not adopted: first=%q next=%q", epochBeforePublish, epoch)
	}
	if cb.installedMCPSidecarEpochAdmits(epochBeforePublish, name) {
		t.Fatal("previous request epoch still admitted the installed tool after the next request began")
	}
	managed.invalidateEpoch()
	if !cb.installedMCPSidecarEpochAdmits(epoch, name) {
		t.Fatal("retiring the managed epoch dropped the installed tool that this request still lists")
	}
	cb.discoveredMCPSurface.invalidateEpoch()
	if !cb.installedMCPSidecarEpochAdmits(epoch, name) {
		t.Fatal("same-family batch continuation was rejected")
	}

	selectionID := ""
	for _, selection := range cb.discoveredMCPSurface.plan.Selections {
		if selection.FitProof.MatchedCapability == agentservice.AutoMCPCapabilityIDForBinding("tengyun", "post_work_log_add") {
			selectionID = selection.ID
			break
		}
	}
	if selectionID == "" {
		t.Fatal("installed selection was not on the sidecar plan")
	}
	savedSurface := cb.semanticSurface
	savedTools := cb.tools
	cb.semanticSurface = cb.discoveredMCPSurface
	if _, err := cb.advanceSemanticToolSurface(selectionID); err != nil {
		cb.semanticSurface = savedSurface
		cb.tools = savedTools
		t.Fatal(err)
	}
	cb.semanticSurface = savedSurface
	cb.tools = savedTools
	if !cb.installedMCPSidecarEpochAdmits(epoch, name) {
		t.Fatal("the next sibling under the same name was not a continuation of this request")
	}

	successor := cb.BeginToolSurfaceEpoch(2)
	if successor == "" || successor == epoch {
		t.Fatalf("third epoch did not begin: %q", successor)
	}
	late := cb.ExecuteToolCallWithContext(name, `{}`, "call-late", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(late.Result, "stale_surface") {
		t.Fatalf("superseded request executed the installed tool: %#v", late)
	}
	other := cb.ExecuteToolCallWithContext("web_fetch", `{"url":"https://example.invalid"}`, "call-other", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(other.Result, "stale_surface") {
		t.Fatalf("sidecar epoch admitted a tool it does not own: %#v", other)
	}
}

// The managed plan and the sidecar can both hold one stable server__tool name.
// After the managed grant is retired and no sibling is reissued, a second call
// in that same request must not run the sidecar's unconsumed grant. The next
// request may. Executing the current sidecar epoch would dial the test server.
func TestManagedMCPRetirementDoesNotHopToTheSidecar(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	managed := &semanticCallSurface{rendered: map[string]bool{}, grants: map[string]tool.InvocationGrant{}, retiredGrants: map[string]tool.InvocationGrant{}}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		semanticSurface: managed,
		tools:           []map[string]interface{}{},
	}
	epoch := cb.BeginToolSurfaceEpoch(0)
	cb.publishReadyInstalledMCPTools()
	name, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	if name != "tengyun__post_work_log_add" || cb.discoveredMCPSurface == nil || cb.discoveredMCPSurface == managed {
		t.Fatalf("sidecar was not published: name=%q", name)
	}
	saved := cb.semanticSurface
	cb.semanticSurface = cb.discoveredMCPSurface
	cb.noteManagedInstalledMCPSpent(name)
	cb.semanticSurface = saved
	if cb.managedInstalledMCPSpentThisRequest(name) {
		t.Fatal("a sidecar retirement was recorded against the managed surface")
	}

	const selectionID = "selection:managed-mcp"
	managed.grants[name] = tool.InvocationGrant{Token: "managed-once", SelectionID: selectionID}
	if cb.installedMCPSidecarCall(name) {
		t.Fatal("live managed grant did not win over the sidecar")
	}
	spent := managed.grants[name]
	delete(managed.grants, name)
	managed.retiredGrants[name] = spent
	managed.completed = map[string]bool{selectionID: true}
	managed.invalidateEpoch()
	cb.noteManagedInstalledMCPSpent(name)
	if cb.installedMCPSidecarCall(name) || cb.installedMCPSidecarEpochAdmits(epoch, name) {
		t.Fatal("retired managed grant still admitted the sidecar in this request")
	}
	again := cb.ExecuteToolCallWithContext(name, `{}`, "call-again", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if again.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(again.Result, "already ran") || strings.Contains(again.Result, "stale_surface") {
		t.Fatalf("same-request hop was not closed: %#v", again)
	}

	managed.grants[name] = tool.InvocationGrant{Token: "managed-next", SelectionID: "selection:managed-next"}
	cb.noteManagedInstalledMCPSpent(name)
	if cb.managedInstalledMCPSpentThisRequest(name) || cb.installedMCPSidecarCall(name) {
		t.Fatal("reissued managed grant did not take the name back")
	}
	delete(managed.grants, name)
	cb.noteManagedInstalledMCPSpent(name)
	if !cb.managedInstalledMCPSpentThisRequest(name) {
		t.Fatal("dropping the reissued grant did not restore the same-request fence")
	}

	next := cb.BeginToolSurfaceEpoch(1)
	if next == "" || next == epoch || cb.managedInstalledMCPSpentThisRequest(name) || !cb.installedMCPSidecarEpochAdmits(next, name) {
		t.Fatalf("next request did not restore the sidecar grant: epoch=%q next=%q", epoch, next)
	}
}

// A managed write that is still awaiting its receipt must not be called again
// through the sidecar on the next model request. Executing that epoch would
// dial the test server. After the receipt settles, the same name is listed.
func TestAwaitingManagedMCPRetirementBlocksTheNextRequest(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	const selectionID = "selection:awaiting-mcp"
	scope := tool.InvocationScope{RootTaskID: "root", PlanID: "plan-await", SessionID: "session", TurnID: "turn", PrincipalID: "user"}
	issuer, err := tool.NewInvocationIssuer([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	store := tool.NewMemoryPlanExecutionStore()
	executor, err := tool.NewPlanExecutor(issuer, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: scope, SelectionID: selectionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(scope, selectionID, tool.PlanExecutionAwaitingReceipt, "digest", "awaiting", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	managed := &semanticCallSurface{
		rendered: map[string]bool{}, grants: map[string]tool.InvocationGrant{}, retiredGrants: map[string]tool.InvocationGrant{},
		scope: scope, executor: executor,
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		semanticSurface: managed,
	}
	cb.BeginToolSurfaceEpoch(0)
	cb.publishReadyInstalledMCPTools()
	name, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	if name != "tengyun__post_work_log_add" {
		t.Fatalf("installed tool was not listed: %q", name)
	}
	managed.retiredGrants[name] = tool.InvocationGrant{Token: "managed-awaiting", SelectionID: selectionID}
	cb.noteManagedInstalledMCPSpent(name)
	blocked := cb.BeginToolSurfaceEpoch(1)
	if !cb.managedInstalledMCPSpentThisRequest(name) || cb.installedMCPSidecarEpochAdmits(blocked, name) {
		t.Fatal("awaiting receipt cleared the sidecar fence")
	}
	cb.setVisibleToolDefinitions(nil)
	cb.publishReadyInstalledMCPTools()
	if listed, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add"); listed != "" {
		t.Fatalf("awaiting tool stayed listed: %q", listed)
	}
	if status := semanticToolsSearchStatus(cb, name); status != "[结果未定：不要再次调用]" {
		t.Fatalf("search status=%q", status)
	}
	call := cb.ExecuteToolCallWithContext(name, `{}`, "call-awaiting", agent.ToolCallExecutionContext{SurfaceEpoch: blocked})
	if call.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(call.Result, "waiting for its prepared action") {
		t.Fatalf("awaiting hop was not closed: %#v", call)
	}
	if _, err := store.SettleAwaitingReceipt(scope, selectionID, tool.PlanExecutionSucceeded, "digest", "settled", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	released := cb.BeginToolSurfaceEpoch(2)
	if cb.managedInstalledMCPSpentThisRequest(name) || !cb.installedMCPSidecarEpochAdmits(released, name) {
		t.Fatal("settled receipt did not restore the sidecar grant")
	}
	cb.setVisibleToolDefinitions(nil)
	cb.publishReadyInstalledMCPTools()
	if listed, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add"); listed != name {
		t.Fatalf("settled tool was not listed: %q", listed)
	}
}

func TestShortCompoundMCPBindReachesTheNextRequest(t *testing.T) {
	if tool.ProviderToolNamed("a_b", "a_b") {
		t.Fatal("prose pin accepted a three-character compound")
	}
	tools := []MCPToolView{{Name: "a_b", Description: "short tool", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	out := h.toolDiscoverToolForOwner("desktop-user", map[string]interface{}{"need": "a_b"})
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || !strings.Contains(out, "tengyun/a_b") || !strings.Contains(out, discoveredMCPBoundSentence) {
		t.Fatalf("short compound was not bound: %s", out)
	}
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		tools: []map[string]interface{}{readFile},
	}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if cb.semanticSurface != nil {
		t.Fatal("short compound promoted the managed surface")
	}
	grantName, grantDesc := listedInstalledMCP(cb.tools, "tengyun", "a_b")
	sawRead := false
	for _, def := range cb.tools {
		if extractToolName(def) == "read_file" {
			sawRead = true
		}
	}
	if !sawRead || grantName != "tengyun__a_b" || !strings.Contains(grantDesc, "a_b") {
		t.Fatalf("legacy list missing read_file or the short grant: name=%q desc=%q tools=%#v", grantName, grantDesc, cb.tools)
	}
}

func TestSharedMCPAddressBindsTheRoundTripSpelling(t *testing.T) {
	separate := discoveredMCPDistinctBinds([]discoveredMCPBind{
		{ServerID: "tengyun", ToolName: "post_work_log_add"},
		{ServerID: "other", ToolName: "post_work_log_add"},
	})
	if len(separate) != 2 {
		t.Fatalf("different servers collapsed: %+v", separate)
	}
	folded := discoveredMCPDistinctBinds([]discoveredMCPBind{
		{ServerID: "tengyun", ToolName: "Foo Bar"},
		{ServerID: "tengyun", ToolName: "foo_bar"},
	})
	if len(folded) != 1 || folded[0].ToolName != "foo_bar" {
		t.Fatalf("folded names=%+v", folded)
	}

	tools := []MCPToolView{
		{Name: "Foo Bar", Description: "folded spelling", InputSchema: discoveredMCPObjectSchema()},
		{Name: "foo_bar", Description: "round trip spelling", InputSchema: discoveredMCPObjectSchema()},
	}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	h.registry = nil
	out := h.toolDiscoverToolForOwner("desktop-user", map[string]interface{}{"need": "Foo Bar foo_bar"})
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || !strings.Contains(out, "call tengyun__foo_bar** (tengyun/foo_bar)") || strings.Contains(out, "call tengyun__Foo_Bar** (tengyun/Foo Bar)") || !strings.Contains(out, "tengyun/Foo Bar uses the same installed MCP address") {
		t.Fatalf("shared address was claimed twice: %s", out)
	}
	binds := loop.discoveredMCPBindList()
	if len(binds) != 1 || binds[0].ServerID != "tengyun" || binds[0].ToolName != "foo_bar" {
		t.Fatalf("binds=%+v", binds)
	}
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		tools: []map[string]interface{}{readFile},
	}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	grantName, grantDesc := listedInstalledMCP(cb.tools, "tengyun", "foo_bar")
	sawRead := false
	for _, def := range cb.tools {
		if extractToolName(def) == "read_file" {
			sawRead = true
		}
	}
	if !sawRead || grantName != "tengyun__foo_bar" || !strings.Contains(grantDesc, "foo_bar") || !strings.Contains(grantDesc, "tengyun") || cb.semanticSurface != nil {
		t.Fatalf("read=%v name=%q desc=%q semantic=%v tools=%#v", sawRead, grantName, grantDesc, cb.semanticSurface != nil, cb.tools)
	}
	loop.rememberDiscoveredMCPBind("tengyun", "Foo Bar")
	cb.tools = []map[string]interface{}{readFile}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if again, _ := listedInstalledMCP(cb.tools, "tengyun", "foo_bar"); again != "tengyun__foo_bar" || cb.semanticSurface != nil {
		t.Fatalf("stored collision published %q", again)
	}
}

func TestFoldedAliasDoesNotCancelThePublishedSurface(t *testing.T) {
	tools := []MCPToolView{
		{Name: "Foo Bar", Description: "folded spelling", InputSchema: discoveredMCPObjectSchema()},
		{Name: "foo_bar", Description: "round trip spelling", InputSchema: discoveredMCPObjectSchema()},
	}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	if !loop.rememberDiscoveredMCPBind("tengyun", "Foo Bar") || !loop.rememberDiscoveredMCPBind("tengyun", "foo_bar") {
		t.Fatal("binds were not stored")
	}
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		tools: []map[string]interface{}{readFile},
	}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if binds := loop.discoveredMCPBindList(); len(binds) != 1 || binds[0].ServerID != "tengyun" || binds[0].ToolName != "foo_bar" {
		t.Fatalf("folded alias stayed on the loop: %+v", binds)
	}
	if cb.semanticSurface != nil || cb.discoveredMCPSurface == nil {
		t.Fatalf("semantic=%v sidecar=%v", cb.semanticSurface != nil, cb.discoveredMCPSurface != nil)
	}
	first, firstDesc := listedInstalledMCP(cb.tools, "tengyun", "foo_bar")
	sawRead := false
	for _, def := range cb.tools {
		if extractToolName(def) == "read_file" {
			sawRead = true
		}
	}
	if first != "" && (!strings.Contains(firstDesc, "foo_bar") || !strings.Contains(firstDesc, "tengyun")) {
		t.Fatalf("grant description=%q", firstDesc)
	}
	selectionID := ""
	for _, selection := range cb.discoveredMCPSurface.plan.Selections {
		if selection.FitProof.MatchedCapability == agentservice.AutoMCPCapabilityIDForBinding("tengyun", "foo_bar") {
			selectionID = selection.ID
			break
		}
	}
	if !sawRead || first != "tengyun__foo_bar" || selectionID == "" {
		t.Fatalf("grant=%q selection=%q read=%v", first, selectionID, sawRead)
	}
	savedSurface := cb.semanticSurface
	savedTools := cb.tools
	cb.semanticSurface = cb.discoveredMCPSurface
	note, err := cb.advanceSemanticToolSurface(selectionID)
	successor := cb.semanticSurface
	cb.semanticSurface = savedSurface
	cb.tools = savedTools
	if successor != nil && successor != cb.discoveredMCPSurface {
		cb.discoveredMCPSurface = successor
	}
	if err != nil {
		t.Fatal(err)
	}
	kept := cb.discoveredMCPSurface
	if !loop.rememberDiscoveredMCPBind("tengyun", "Foo Bar") {
		t.Fatal("alias was not stored again")
	}
	cb.tools = []map[string]interface{}{readFile}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if cb.discoveredMCPSurface != kept || cb.semanticSurface != nil {
		t.Fatal("folded alias cancelled the published surface")
	}
	if binds := loop.discoveredMCPBindList(); len(binds) != 1 || binds[0].ToolName != "foo_bar" {
		t.Fatalf("alias returned: %+v", binds)
	}
	second, _ := listedInstalledMCP(cb.tools, "tengyun", "foo_bar")
	sawRead = false
	for _, def := range cb.tools {
		if extractToolName(def) == "read_file" {
			sawRead = true
		}
	}
	live, liveOK := cb.discoveredMCPSurface.grants[first]
	retired, retiredOK := cb.discoveredMCPSurface.retiredGrants[first]
	if !sawRead || second != first || !liveOK || !retiredOK || live.Token == "" || live.Token == retired.Token {
		t.Fatalf("stable name did not keep a fresh grant: second=%q first=%q live=%v retired=%v note=%q", second, first, live.Token, retired.Token, note)
	}
}

// A settled installed-MCP call spends its grant token. The next request lists
// the same server__tool name with a new token. Two work-log entries are two
// calls of that name.
func TestDiscoveredMCPSuccessListsTheNextGrant(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	loop.rememberDiscoveredMCPBind("tengyun", "post_work_log_add")
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		tools: []map[string]interface{}{readFile},
	}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	first := discoveredMCPListedGrant(cb.tools)
	selectionID := ""
	for _, selection := range cb.discoveredMCPSurface.plan.Selections {
		if selection.FitProof.MatchedCapability == agentservice.AutoMCPCapabilityIDForBinding("tengyun", "post_work_log_add") {
			selectionID = selection.ID
			break
		}
	}
	if first == "" || selectionID == "" {
		t.Fatalf("grant=%q selection=%q", first, selectionID)
	}
	savedSurface := cb.semanticSurface
	savedTools := cb.tools
	cb.semanticSurface = cb.discoveredMCPSurface
	note, err := cb.advanceSemanticToolSurface(selectionID)
	successor := cb.semanticSurface
	cb.semanticSurface = savedSurface
	cb.tools = savedTools
	if successor != nil && successor != cb.discoveredMCPSurface {
		cb.discoveredMCPSurface = successor
	}
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(note, "complete") {
		t.Fatalf("iterative MCP call was reported as the last invocation: %q", note)
	}
	cb.tools = []map[string]interface{}{readFile}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if cb.semanticSurface != nil {
		t.Fatal("next request promoted the sidecar")
	}
	second := discoveredMCPListedGrant(cb.tools)
	sawRead := false
	for _, def := range cb.tools {
		if extractToolName(def) == "read_file" {
			sawRead = true
		}
	}
	live, liveOK := cb.discoveredMCPSurface.grants[first]
	retired, retiredOK := cb.discoveredMCPSurface.retiredGrants[first]
	if !sawRead || second != first || first != "tengyun__post_work_log_add" || !liveOK || !retiredOK || live.Token == "" || live.Token == retired.Token {
		t.Fatalf("stable name lost its next grant: first=%s second=%s live=%v retired=%v read=%v note=%q", first, second, live.Token, retired.Token, sawRead, note)
	}
	if !cb.discoveredMCPSurface.completed[selectionID] {
		t.Fatal("settled call was not recorded complete")
	}
	family := 0
	for _, selection := range cb.discoveredMCPSurface.plan.Selections {
		if selection.FitProof.MatchedCapability == agentservice.AutoMCPCapabilityIDForBinding("tengyun", "post_work_log_add") {
			family++
		}
	}
	if family < 2 {
		t.Fatalf("settled call did not leave a sibling selection: %d", family)
	}
	for _, selection := range cb.discoveredMCPSurface.plan.Selections {
		if selection.FitProof.MatchedCapability == agentservice.CapabilityRecordUpdateWorklog {
			t.Fatalf("sibling provisioned record.update.worklog: %+v", selection)
		}
	}
}

func discoveredMCPListedGrant(definitions []map[string]interface{}) string {
	name, _ := listedInstalledMCP(definitions, "tengyun", "post_work_log_add")
	return name
}

func TestUniqueCompoundMCPMatchBinds(t *testing.T) {
	tools := []MCPToolView{{Name: "create_issue", Description: "open a tracker ticket", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tracker", tools, true)
	h.registry = nil
	out := h.toolDiscoverToolForOwner("desktop-user", map[string]interface{}{"need": "open a tracker ticket"})
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || !strings.Contains(out, discoveredMCPBoundSentence) {
		t.Fatalf("unique compound match did not bind: %s", out)
	}
	binds := loop.discoveredMCPBindList()
	if len(binds) != 1 || binds[0].ToolName != "create_issue" || binds[0].ServerID != "tracker" {
		t.Fatalf("binds=%+v out=%s", binds, out)
	}
}

func TestMultipleUnnamedMCPMatchesDoNotBind(t *testing.T) {
	tools := []MCPToolView{
		{Name: "create_issue", Description: "lookup records alpha", InputSchema: discoveredMCPObjectSchema()},
		{Name: "post_work_log_add", Description: "lookup records beta", InputSchema: discoveredMCPObjectSchema()},
	}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	h.registry = nil
	out := h.toolDiscoverToolForOwner("desktop-user", map[string]interface{}{"need": "lookup records"})
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || strings.Contains(out, "bound for the next request") || !strings.Contains(out, discoveredMCPUnboundSentence) {
		t.Fatalf("unnamed MCP matches were bound or still ask for a replan: %s", out)
	}
	if binds := loop.discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("binds=%+v", binds)
	}
}

func TestBareMCPWordStaysUnboundOnALoop(t *testing.T) {
	tools := []MCPToolView{{Name: "search", Description: "search enterprise content", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "remote", tools, true)
	h.registry = nil
	out := h.toolDiscoverToolForOwner("desktop-user", map[string]interface{}{"need": "search enterprise content"})
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || strings.Contains(out, "bound for the next request") || !strings.Contains(out, "remote/search") || !strings.Contains(out, discoveredMCPUnboundSentence) {
		t.Fatalf("bare tool name was bound: %s", out)
	}
	if binds := loop.discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("binds=%+v", binds)
	}
}

func TestUnprojectableMCPDiscoveryDoesNotClaimABind(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"enum": []interface{}{"list"}},
		},
		"additionalProperties": false,
	}
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: schema}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	out := h.toolDiscoverToolForOwner("desktop-user", map[string]interface{}{"need": "post_work_log_add"})
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || strings.Contains(out, "bound for the next request") || !strings.Contains(out, "not a published semantic provider") {
		t.Fatalf("unprojectable tool was described as bindable: %s", out)
	}
	if binds := loop.discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("binds=%+v", binds)
	}
}

func TestUnprojectableSiblingDoesNotHideABindableTool(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"enum": []interface{}{"list"}},
		},
		"additionalProperties": false,
	}
	tools := []MCPToolView{
		{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()},
		{Name: "bad_enum", Description: "unprojectable sibling", InputSchema: schema},
	}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	if !loop.rememberDiscoveredMCPBind("tengyun", "bad_enum") || !loop.rememberDiscoveredMCPBind("tengyun", "post_work_log_add") {
		t.Fatal("binds were not stored")
	}
	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		tools: []map[string]interface{}{readFile},
	}
	assertOneBindableGrant := func() {
		t.Helper()
		if err := cb.attachDiscoveredMCPBindings(); err != nil {
			t.Fatal(err)
		}
		if cb.semanticSurface != nil {
			t.Fatal("sidecar replaced the legacy surface")
		}
		if len(cb.discoveredMCPBinds) != 1 || cb.discoveredMCPBinds[0].ServerID != "tengyun" || cb.discoveredMCPBinds[0].ToolName != "post_work_log_add" {
			t.Fatalf("published binds=%+v", cb.discoveredMCPBinds)
		}
		grantName, grantDesc := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
		sawRead := false
		for _, def := range cb.tools {
			if extractToolName(def) == "read_file" {
				sawRead = true
			}
		}
		if !sawRead || grantName != "tengyun__post_work_log_add" || !strings.Contains(grantDesc, "post_work_log_add") || strings.Contains(grantDesc, "bad_enum") {
			t.Fatalf("read=%v name=%q desc=%q tools=%#v", sawRead, grantName, grantDesc, cb.tools)
		}
	}
	assertOneBindableGrant()
	if binds := loop.discoveredMCPBindList(); len(binds) != 2 {
		t.Fatalf("skipped sibling left the loop: %+v", binds)
	}
	cb.tools = []map[string]interface{}{readFile}
	assertOneBindableGrant()
}

func TestRejectedMCPContractDoesNotHideASiblingTool(t *testing.T) {
	tools := []MCPToolView{
		{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()},
		{Name: "post_work_log_delete", Description: "delete a work log", InputSchema: discoveredMCPObjectSchema()},
	}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	contracts, err := h.app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	principal := semanticDesktopContractPrincipal()
	contract, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "tengyun", "post_work_log_delete")
	if !ok {
		t.Fatal("delete contract was not published")
	}
	// Sensitive is outside the installed MCP effect ceiling. Publication of
	// this one contract must not drop the sibling whose contract still fits.
	contract.Effects = []tool.EffectClass{tool.EffectSensitive}
	if err := contracts.PublishMCPContract(principal, "tengyun", "post_work_log_delete", contract); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
	}
	cb.publishReadyInstalledMCPTools()
	name, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	if name != "tengyun__post_work_log_add" {
		t.Fatalf("rejected sibling hid the installed write: %q tools=%#v", name, cb.tools)
	}
	if hidden, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_delete"); hidden != "" {
		t.Fatalf("rejected contract was listed: %q", hidden)
	}
}

func TestPoisonedMCPContractDoesNotRejectTheHostTurn(t *testing.T) {
	tools := []MCPToolView{
		{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()},
		{Name: "post_work_log_delete", Description: "delete a work log", InputSchema: discoveredMCPObjectSchema()},
	}
	h, _ := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	contracts, err := h.app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	principal := semanticDesktopContractPrincipal()
	contract, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "tengyun", "post_work_log_delete")
	if !ok {
		t.Fatal("delete contract was not published")
	}
	// Sensitive is outside the installed MCP effect ceiling. The managed
	// plan used to reject the whole turn here, before the model loop, so
	// the clock and the sibling that still fits never reached the request.
	contract.Effects = []tool.EffectClass{tool.EffectSensitive}
	if err := contracts.PublishMCPContract(principal, "tengyun", "post_work_log_delete", contract); err != nil {
		t.Fatal(err)
	}
	h.unifiedClassifier = semanticClassifierForLabel(t, intent.LabelCurrentTime)
	if err := h.registry.Register(RegisteredTool{
		Name: "current_datetime", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.current_time", Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "clock" },
	}); err != nil {
		t.Fatal(err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "what time is it now?", "lansenger")
	if err != nil || !handled || surface == nil {
		t.Fatalf("poisoned MCP contract rejected the host turn: handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	foundClock := false
	for _, selection := range surface.plan.Selections {
		if selection.AdapterName == semanticTrustedClockAdapter {
			foundClock = true
		}
		if selection.Provider.ImplementationID == "post_work_log_delete" {
			t.Fatalf("rejected contract was selected: %+v", selection)
		}
	}
	if !foundClock {
		t.Fatalf("clock was not selected: %+v", surface.plan.Selections)
	}
	for _, def := range defs {
		if extractToolName(def) == "current_datetime" {
			return
		}
	}
	t.Fatalf("clock was not rendered: %#v", defs)
}

// Naming the tool whose contract was dropped must not host-reject a turn
// that already selected something else. The pin is still unmet. The clock
// stays on the request, so the model loop can list the tools that published.
func TestNamedDroppedMCPDoesNotRejectTheHostTurn(t *testing.T) {
	tools := []MCPToolView{
		{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()},
		{Name: "post_work_log_delete", Description: "delete a work log", InputSchema: discoveredMCPObjectSchema()},
	}
	h, _ := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	contracts, err := h.app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	principal := semanticDesktopContractPrincipal()
	contract, ok := contracts.ResolveMCPDynamicContract(context.Background(), principal, "tengyun", "post_work_log_delete")
	if !ok {
		t.Fatal("delete contract was not published")
	}
	contract.Effects = []tool.EffectClass{tool.EffectSensitive}
	if err := contracts.PublishMCPContract(principal, "tengyun", "post_work_log_delete", contract); err != nil {
		t.Fatal(err)
	}
	h.unifiedClassifier = semanticClassifierForLabel(t, intent.LabelCurrentTime)
	if err := h.registry.Register(RegisteredTool{
		Name: "current_datetime", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.current_time", Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "clock" },
	}); err != nil {
		t.Fatal(err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "what time is it now? post_work_log_delete", "lansenger")
	if err != nil || !handled || surface == nil {
		t.Fatalf("named dropped MCP rejected the host turn: handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	foundClock := false
	foundMCPUnmet := false
	for _, selection := range surface.plan.Selections {
		if selection.AdapterName == semanticTrustedClockAdapter {
			foundClock = true
		}
		if selection.Provider.ImplementationID == "post_work_log_delete" {
			t.Fatalf("rejected contract was selected: %+v", selection)
		}
	}
	for _, item := range surface.plan.Unmet {
		if strings.Contains(item.NeedID, "x:mcp:") && strings.Contains(item.NeedID, "post_work_log_delete") {
			foundMCPUnmet = true
		}
		if strings.Contains(item.NeedID, "record.update.worklog") || strings.Contains(item.NeedID, "information.current_time") {
			t.Fatalf("host need was treated as an installed MCP miss: %+v", surface.plan.Unmet)
		}
	}
	if !foundClock || !foundMCPUnmet {
		t.Fatalf("clock=%v mcpUnmet=%v selections=%+v unmet=%+v", foundClock, foundMCPUnmet, surface.plan.Selections, surface.plan.Unmet)
	}
	for _, def := range defs {
		if extractToolName(def) == "current_datetime" {
			return
		}
	}
	t.Fatalf("clock was not rendered: %#v", defs)
}

func TestInstalledWriteStaysListedWhileSiblingLaunchIsOpen(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	cfg, err := h.app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.LocalMCPServers = append(cfg.LocalMCPServers, corelib.LocalMCPServerEntry{
		ID: "launch-unstarted", Name: "launch-unstarted", Command: "unused", AutoStart: true,
	})
	if err := h.app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	h.app.localMCPManager = NewLocalMCPManager(h.app.mcpRegistry)
	t.Cleanup(func() { h.app.localMCPManager.cancel() })
	inventory, err := h.semanticInstalledMCPInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	coverage := inventory.coverage.ForProviderKind("mcp")
	if coverage.State != tool.CatalogCoverageIncomplete || coverage.ReasonCode != tool.CatalogCoverageReasonNotReady {
		t.Fatalf("coverage=%+v", coverage)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
	}
	cb.publishReadyInstalledMCPTools()
	name, _ := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	if name != "tengyun__post_work_log_add" {
		t.Fatalf("sibling launch hid the installed write: %q tools=%#v", name, cb.tools)
	}
}

func TestManagedRenderFailureStillListsInstalledMCP(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		semanticSurface: &semanticCallSurface{rendered: map[string]bool{}},
	}
	listed := cb.BuildToolsForModelRequest("append today's work log", 0)
	name, _ := listedInstalledMCP(listed, "tengyun", "post_work_log_add")
	if name != "tengyun__post_work_log_add" {
		t.Fatalf("render failure hid the installed tool: %q tools=%#v", name, listed)
	}
}

func TestOpenLookupWithoutExactMCPAddressDoesNotClaimABind(t *testing.T) {
	contract := agentservice.ReviewedMCPOpenLookupContract()
	contract.Effects = []tool.EffectClass{tool.EffectExternalEffect}
	entry := func(withAddress bool) agentservice.MCPToolEntry {
		copied := contract
		copied.Provisions = append([]tool.CapabilityProvision(nil), contract.Provisions...)
		if withAddress {
			copied.Provisions = append(copied.Provisions, tool.CapabilityProvision{
				Capability: agentservice.AutoMCPCapabilityIDForBinding("tengyun", "post_work_log_add"),
				Quality:    1,
			})
		}
		return agentservice.MCPToolEntry{
			ServerID: "tengyun", ToolName: "post_work_log_add",
			InputSchema: discoveredMCPObjectSchema(), Contract: copied,
		}
	}
	assess := func(withAddress bool, state tool.CatalogCoverageState) string {
		return assessDiscoveredMCPBind(semanticDynamicInventory{
			mcpEntries: []agentservice.MCPToolEntry{entry(withAddress)},
			coverage:   tool.CatalogCoverage{State: state},
		}, "tengyun", "post_work_log_add")
	}
	missing := assess(false, tool.CatalogCoverageComplete)
	if !strings.Contains(missing, "not a published semantic provider") || strings.Contains(missing, "cannot be bound until") {
		t.Fatalf("open lookup without the exact address was bindable: %q", missing)
	}
	if got := assess(true, tool.CatalogCoverageComplete); got != "" {
		t.Fatalf("exact address was refused: %q", got)
	}
	incomplete := assess(true, tool.CatalogCoverageIncomplete)
	if incomplete != "" {
		t.Fatalf("observed tool waited on the family watermark: %q", incomplete)
	}
	blocked := entry(true)
	blocked.RuntimeBlocked = true
	if got := assessDiscoveredMCPBind(semanticDynamicInventory{
		mcpEntries: []agentservice.MCPToolEntry{blocked},
		coverage:   tool.CatalogCoverage{State: tool.CatalogCoverageIncomplete},
	}, "tengyun", "post_work_log_add"); !strings.Contains(got, "not ready") {
		t.Fatalf("blocked observation was bindable: %q", got)
	}
}

func TestUnpublishedMCPDiscoveryDoesNotClaimABind(t *testing.T) {
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, false)
	out := h.toolDiscoverToolForOwner("desktop-user", map[string]interface{}{"need": "post_work_log_add"})
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || strings.Contains(out, "bound for the next request") || !strings.Contains(out, "not a published semantic provider") {
		t.Fatalf("unpublished tool was described as bindable: %s", out)
	}
	if binds := loop.discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("binds=%+v", binds)
	}
}

func TestApprovedMCPWriteRepublishesOnTheContinuationLoop(t *testing.T) {
	isolateMCPServerWriteGrants(t)
	tools := []MCPToolView{{Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema()}}
	h, _ := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	write := &pendingMCPServerWrite{ServerID: "tengyun", ServerName: "腾云", ToolName: "post_work_log_add"}
	ask := func(text string, background bool, askUser bool) *LoopContext {
		return h.prepareIMLoopContext(nil, IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: text, IsBackground: background}, nil, background, askUser)
	}

	if _, handled := h.finishMCPServerWriteAskUser("desktop-user", "取消", write); !handled {
		t.Fatal("cancel was not handled")
	}
	if binds := ask("取消", false, true).discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("cancel restored %+v", binds)
	}
	if _, handled := h.finishMCPServerWriteAskUser("desktop-user", "maybe", write); handled {
		t.Fatal("ambiguous answer was treated as a decision")
	}
	if binds := ask("maybe", false, true).discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("ambiguous answer restored %+v", binds)
	}

	hint, handled := h.finishMCPServerWriteAskUser("desktop-user", "确认", write)
	if !handled || !strings.Contains(hint, "post_work_log_add") || strings.Contains(hint, "managed replan") || strings.Contains(hint, "call call_mcp_tool") {
		t.Fatalf("approve hint=%q handled=%v", hint, handled)
	}
	if binds := ask("确认", true, false).discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("background prepare consumed the continuation: %+v", binds)
	}
	loop := ask("确认", false, true)
	binds := loop.discoveredMCPBindList()
	if len(binds) != 1 || binds[0].ServerID != "tengyun" || binds[0].ToolName != "post_work_log_add" {
		t.Fatalf("continuation binds=%+v", binds)
	}
	if extra := ask("确认", false, true).discoveredMCPBindList(); len(extra) != 0 {
		t.Fatalf("second continuation invented %+v", extra)
	}

	readFile := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "read_file", "description": "Read a file",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		tools: []map[string]interface{}{readFile},
	}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if cb.semanticSurface != nil || cb.discoveredMCPSurface == nil {
		t.Fatalf("semantic=%v sidecar=%v", cb.semanticSurface != nil, cb.discoveredMCPSurface != nil)
	}
	grantName, grantDesc := listedInstalledMCP(cb.tools, "tengyun", "post_work_log_add")
	sawRead := false
	for _, def := range cb.tools {
		if extractToolName(def) == "read_file" {
			sawRead = true
		}
	}
	if !sawRead || grantName != "tengyun__post_work_log_add" || !strings.Contains(grantDesc, "post_work_log_add") {
		t.Fatalf("continuation list missing read_file or the republished grant: name=%q desc=%q tools=%#v", grantName, grantDesc, cb.tools)
	}

	if _, handled := h.finishMCPServerWriteAskUser("desktop-user", "确认", write); !handled {
		t.Fatal("second approve was not handled")
	}
	if _, handled := h.finishMCPServerWriteAskUser("desktop-user", "取消", write); !handled {
		t.Fatal("decline after an untaken approval was not handled")
	}
	if binds := ask("取消", false, true).discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("decline kept the untaken approval: %+v", binds)
	}

	if _, handled := h.finishMCPServerWriteAskUser("desktop-user", "确认", write); !handled {
		t.Fatal("third approve was not handled")
	}
	if binds := ask("hello", false, false).discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("ordinary turn kept the continuation: %+v", binds)
	}
	if binds := ask("确认", false, true).discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("dropped continuation leaked onto a later ask: %+v", binds)
	}
}

func TestNewTurnDropsStaleDiscoveredMCPBinds(t *testing.T) {
	isolateMCPServerWriteGrants(t)
	h := &IMMessageHandler{}
	loop := NewLoopContext("chat", 4, nil)
	loop.rememberDiscoveredMCPBind("tengyun", "create_issue")
	ordinary := h.prepareIMLoopContext(loop, IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "hello"}, nil, false, false)
	if ordinary != loop {
		t.Fatal("prepare replaced the supplied loop")
	}
	if binds := loop.discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("new turn kept %+v", binds)
	}

	write := &pendingMCPServerWrite{ServerID: "tengyun", ServerName: "腾云", ToolName: "post_work_log_add"}
	if _, handled := h.finishMCPServerWriteAskUser("desktop-user", "确认", write); !handled {
		t.Fatal("approve was not handled")
	}
	loop.rememberDiscoveredMCPBind("other", "create_issue")
	continued := h.prepareIMLoopContext(loop, IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "确认", RequestID: "ask-1"}, nil, false, true)
	binds := continued.discoveredMCPBindList()
	if len(binds) != 1 || binds[0].ServerID != "tengyun" || binds[0].ToolName != "post_work_log_add" {
		t.Fatalf("continuation kept a stale bind: %+v", binds)
	}
}

func TestDiscoveredMCPContinuationKeepsTheLegacySurface(t *testing.T) {
	ctx := &LoopContext{IsAskUserResponse: true, UserID: "desktop-user", Platform: "desktop"}
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.73, Layer: 2, Degraded: true}
	if !loopContextIsSemanticManaged(ctx) || !loopContextBlocksLegacyToolRouter(ctx) {
		t.Fatal("fixture did not close the legacy router")
	}
	ctx.rememberDiscoveredMCPBind("tengyun", "post_work_log_add")
	if loopContextBlocksLegacyToolRouter(ctx) {
		t.Fatal("approved MCP continuation closed the legacy router")
	}
	defs, surface, handled, err := (&IMMessageHandler{}).semanticCallSurfaceForSharedTurnWithContext(ctx, "desktop-user", "确认", "desktop")
	if handled || err != nil || surface != nil || len(defs) != 0 {
		t.Fatalf("planner owned the continuation handled=%v err=%v defs=%d surface=%v", handled, err, len(defs), surface != nil)
	}
}
