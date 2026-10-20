package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

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
	grantName := ""
	sawRead := false
	for _, def := range cb.tools {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		if name == "read_file" {
			sawRead = true
		}
		if strings.HasPrefix(name, "invoke_") && strings.Contains(desc, "post_work_log_add") && strings.Contains(desc, "tengyun") {
			grantName = name
		}
	}
	if !sawRead || grantName == "" {
		t.Fatalf("legacy list missing read_file or the bound grant: %#v", cb.tools)
	}
	sidecar := cb.discoveredMCPSurface
	cb.tools = []map[string]interface{}{readFile}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	if cb.discoveredMCPSurface != sidecar {
		t.Fatal("unchanged bind set published a second surface")
	}
	cb.legacySurface = cb.legacySurface.replaceDefinitions(cb.tools, nil)
	epoch := cb.BeginToolSurfaceEpoch(1)
	if cb.semanticSurface != nil {
		t.Fatal("request epoch promoted the sidecar")
	}
	result := cb.ExecuteToolCallWithContext(grantName, `{"content":"morning"}`, "call-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if cb.semanticSurface != nil {
		t.Fatal("execution left the sidecar assigned")
	}
	if !strings.Contains(result.Result, "__ASK_USER__") || !strings.Contains(result.Result, "post_work_log_add") {
		t.Fatalf("unconfirmed external effect did not pause: %s", result.Result)
	}
	if _, ok := cb.discoveredMCPSurface.grants[grantName]; !ok {
		t.Fatal("confirmation pause consumed the invocation grant")
	}
	legacy := cb.ExecuteTool("read_file", `{}`)
	if strings.Contains(legacy, "__ASK_USER__") || strings.Contains(legacy, "post_work_log_add") {
		t.Fatalf("legacy tool entered the MCP grant path: %s", legacy)
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
	sawRead := false
	grantName := ""
	for _, def := range cb.tools {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		if name == "read_file" {
			sawRead = true
		}
		if strings.HasPrefix(name, "invoke_") && strings.Contains(desc, "a_b") && strings.Contains(desc, "tengyun") {
			grantName = name
		}
	}
	if !sawRead || grantName == "" {
		t.Fatalf("legacy list missing read_file or the short grant: %#v", cb.tools)
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
	if strings.Contains(out, "managed replan") || strings.Contains(out, "call call_mcp_tool") || !strings.Contains(out, "bound for the next request** (tengyun/foo_bar)") || strings.Contains(out, "bound for the next request** (tengyun/Foo Bar)") || !strings.Contains(out, "tengyun/Foo Bar uses the same installed MCP address") {
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
	grants := 0
	sawRead := false
	for _, def := range cb.tools {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		if name == "read_file" {
			sawRead = true
		}
		if strings.HasPrefix(name, "invoke_") {
			grants++
			if !strings.Contains(desc, "foo_bar") || !strings.Contains(desc, "tengyun") {
				t.Fatalf("grant description=%q", desc)
			}
		}
	}
	if !sawRead || grants != 1 || cb.semanticSurface != nil {
		t.Fatalf("read=%v grants=%d semantic=%v tools=%#v", sawRead, grants, cb.semanticSurface != nil, cb.tools)
	}
	loop.rememberDiscoveredMCPBind("tengyun", "Foo Bar")
	cb.tools = []map[string]interface{}{readFile}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	grants = 0
	for _, def := range cb.tools {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if strings.HasPrefix(name, "invoke_") {
			grants++
		}
	}
	if grants != 1 || cb.semanticSurface != nil {
		t.Fatalf("stored collision published %d grants", grants)
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
	first := ""
	sawRead := false
	grants := 0
	for _, def := range cb.tools {
		name := extractToolName(def)
		fn, _ := def["function"].(map[string]interface{})
		desc, _ := fn["description"].(string)
		if name == "read_file" {
			sawRead = true
		}
		if strings.HasPrefix(name, "invoke_") {
			grants++
			if !strings.Contains(desc, "foo_bar") || !strings.Contains(desc, "tengyun") {
				t.Fatalf("grant description=%q", desc)
			}
			first = name
		}
	}
	selectionID := ""
	for _, selection := range cb.discoveredMCPSurface.plan.Selections {
		if selection.FitProof.MatchedCapability == agentservice.AutoMCPCapabilityIDForBinding("tengyun", "foo_bar") {
			selectionID = selection.ID
			break
		}
	}
	if !sawRead || grants != 1 || first == "" || selectionID == "" {
		t.Fatalf("grant=%q selection=%q grants=%d read=%v", first, selectionID, grants, sawRead)
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
	second := ""
	sawRead = false
	grants = 0
	for _, def := range cb.tools {
		name := extractToolName(def)
		fn, _ := def["function"].(map[string]interface{})
		desc, _ := fn["description"].(string)
		if name == "read_file" {
			sawRead = true
		}
		if strings.HasPrefix(name, "invoke_") && strings.Contains(desc, "foo_bar") && strings.Contains(desc, "tengyun") {
			grants++
			second = name
		}
	}
	if !sawRead || grants != 1 || second == "" || second == first {
		t.Fatalf("next grant=%q first=%q read=%v grants=%d note=%q", second, first, sawRead, grants, note)
	}
	if _, ok := cb.discoveredMCPSurface.retiredGrants[first]; !ok {
		t.Fatal("spent grant was not retired")
	}
}

// A settled installed-MCP call spends its grant. The next model request in the
// same turn has to list a new token for that same tool, beside the legacy
// tools. Two work-log entries are two calls; the second one cannot reuse the
// token the first call consumed.
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
	if !sawRead || second == "" || second == first {
		t.Fatalf("next request did not list a new grant: first=%s second=%s read=%v note=%q", first, second, sawRead, note)
	}
	if _, ok := cb.discoveredMCPSurface.grants[first]; ok {
		t.Fatal("spent grant stayed live")
	}
	if _, ok := cb.discoveredMCPSurface.retiredGrants[first]; !ok {
		t.Fatal("spent grant was not retired")
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
	for _, def := range definitions {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		if strings.HasPrefix(name, "invoke_") && strings.Contains(desc, "post_work_log_add") && strings.Contains(desc, "tengyun") {
			return name
		}
	}
	return ""
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
		grants := 0
		sawRead := false
		for _, def := range cb.tools {
			fn, _ := def["function"].(map[string]interface{})
			name, _ := fn["name"].(string)
			desc, _ := fn["description"].(string)
			if name == "read_file" {
				sawRead = true
			}
			if strings.HasPrefix(name, "invoke_") {
				grants++
				if !strings.Contains(desc, "post_work_log_add") || !strings.Contains(desc, "tengyun") || strings.Contains(desc, "bad_enum") {
					t.Fatalf("grant description=%q", desc)
				}
			}
		}
		if !sawRead || grants != 1 {
			t.Fatalf("read=%v grants=%d tools=%#v", sawRead, grants, cb.tools)
		}
	}
	assertOneBindableGrant()
	if binds := loop.discoveredMCPBindList(); len(binds) != 2 {
		t.Fatalf("skipped sibling left the loop: %+v", binds)
	}
	cb.tools = []map[string]interface{}{readFile}
	assertOneBindableGrant()
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
	if !strings.Contains(incomplete, "cannot be bound until the MCP catalog observation is complete.") {
		t.Fatalf("incomplete observation was not refused: %q", incomplete)
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
	grantName := ""
	sawRead := false
	for _, def := range cb.tools {
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		if name == "read_file" {
			sawRead = true
		}
		if strings.HasPrefix(name, "invoke_") && strings.Contains(desc, "post_work_log_add") && strings.Contains(desc, "tengyun") {
			grantName = name
		}
	}
	if !sawRead || grantName == "" {
		t.Fatalf("continuation list missing read_file or the republished grant: %#v", cb.tools)
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
