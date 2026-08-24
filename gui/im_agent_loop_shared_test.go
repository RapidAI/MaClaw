package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

// shouldUseSharedAgentLoopLive mirrors shouldUseSharedAgentLoop but uses the
// production mode resolver so unit tests are not blocked by the testing.Testing()
// gate (which keeps package RunAgentLoop tests on the legacy path).
func shouldUseSharedAgentLoopLive(h *IMMessageHandler, ctx *LoopContext, userID string, attachments []MessageAttachment) bool {
	mode := resolveSharedAgentLoopModeLive(h)
	eligible, reason := h.sharedAgentLoopEligibility(ctx, attachments)
	switch mode {
	case sharedAgentLoopOff:
		return false
	case sharedAgentLoopShadow:
		if eligible {
			recordSharedLoopSkip("shadow", reason)
		}
		return false
	case sharedAgentLoopOn:
		if !eligible {
			recordSharedLoopSkip("ineligible", reason)
			return false
		}
		if !sharedLoopCanaryAllowsFor(h, userID) {
			recordSharedLoopSkip("canary", "canary")
			return false
		}
		return true
	default:
		return false
	}
}

func TestSharedProjectToolResultUsesRuntimePolicyOwner(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })
	ctx := NewLoopContext("chat", 3, nil)
	ctx.Runtime.PolicyOwnerID = "remote:mobile"
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, loopCtx: ctx, userID: "desktop-user"}
	raw := strings.Repeat("raw-result\n", 2000)
	projected := cb.ProjectToolResult("bash", agent.ToolExecutionResult{Result: raw, Outcome: agent.ToolExecutionOutcomeOK})
	if !strings.Contains(projected, "[tool_result_handle]") {
		t.Fatalf("projection missing handle: %q", projected[max(0, len(projected)-300):])
	}
	if strings.Contains(projected, maclawpath.ToolResultsDir()) || strings.Contains(projected, "path:") {
		t.Fatalf("projection exposed local storage path: %q", projected[max(0, len(projected)-500):])
	}
	entries, err := os.ReadDir(filepath.Join(maclawpath.ToolResultsDir(), toolresult.SessionDirectoryName("remote:mobile")))
	if err != nil || len(entries) == 0 {
		t.Fatalf("projection was not stored under runtime policy owner: entries=%d err=%v", len(entries), err)
	}
}

func TestSharedExecuteToolCarriesScreenshotIntoFinalResponse(t *testing.T) {
	imageData := testOnePixelPNGBase64
	h := &IMMessageHandler{
		registry: NewToolRegistry(),
		client:   &http.Client{},
	}
	if err := h.registry.Register(RegisteredTool{
		Name:        "screenshot",
		Description: "test screenshot",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(map[string]interface{}, tool.ProgressCallback) string {
			return "[screenshot_base64]" + imageData
		},
	}); err != nil {
		t.Fatalf("Register screenshot tool: %v", err)
	}

	cb := &sharedAgentLoopCallbacks{handler: h, platform: "lansenger"}
	if got := cb.ExecuteTool("screenshot", "{}"); got != toolPayloadPreparedMessage {
		t.Fatalf("ExecuteTool() = %q, want %q", got, toolPayloadPreparedMessage)
	}
	if cb.screenshotImageKey != imageData {
		t.Fatalf("screenshotImageKey = %q, want screenshot payload", cb.screenshotImageKey)
	}

	resp := &IMAgentResponse{ResponseSource: "shared_agent_loop"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.ImageKey != imageData || resp.ResponseSource != imResponseSourceScreenshot.String() {
		t.Fatalf("final response lost screenshot: %+v", resp)
	}
}

func TestSharedAgentLoopRejectsToolCallOutsideExposedLegacySurface(t *testing.T) {
	registry := NewToolRegistry()
	var executed bool
	if err := registry.Register(RegisteredTool{
		Name: "hidden_legacy_tool", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
		Handler: func(map[string]interface{}) string {
			executed = true
			return "unexpected execution"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	cb := &sharedAgentLoopCallbacks{
		handler:       &IMMessageHandler{registry: registry},
		legacySurface: newLegacyToolSurface([]map[string]interface{}{}),
	}
	result := cb.ExecuteToolCall("hidden_legacy_tool", `{}`, "call-hidden")
	if executed {
		t.Fatal("tool outside the model surface executed")
	}
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "not available on this request's tool surface") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSharedAgentLoopLegacySurfaceAllowsExposedTool(t *testing.T) {
	registry := NewToolRegistry()
	var executed bool
	if err := registry.Register(RegisteredTool{
		Name: "read_file", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
		Handler: func(map[string]interface{}) string {
			executed = true
			return "executed"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		legacySurface: newLegacyToolSurface([]map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "read_file"}},
		}),
	}
	result := cb.ExecuteToolCall("read_file", `{}`, "call-exposed")
	if !executed || result.Outcome != agent.ToolExecutionOutcomeOK || result.Result != "executed" {
		t.Fatalf("expected exposed tool execution, got executed=%v result=%+v", executed, result)
	}
}

func TestSharedAgentLoopRejectsLegacyMCPGatewayBeforeHandlerDispatch(t *testing.T) {
	registry := NewToolRegistry()
	var called bool
	if err := registry.Register(RegisteredTool{
		Name: "call_mcp_tool", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
		Handler: func(map[string]interface{}) string {
			called = true
			return "unexpected MCP dispatch"
		},
	}); err != nil {
		t.Fatalf("register gateway: %v", err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		legacySurface: newLegacyToolSurface([]map[string]interface{}{toolDef("call_mcp_tool", "legacy gateway", map[string]interface{}{
			"server_id": map[string]interface{}{"type": "string"},
			"tool_name": map[string]interface{}{"type": "string"},
			"arguments": map[string]interface{}{"type": "object"},
		}, []string{"server_id", "tool_name"})}),
	}
	result := cb.ExecuteToolCall("call_mcp_tool", `{"server_id":"unbound","tool_name":"execute","arguments":{}}`, "call-mcp")
	if called {
		t.Fatal("shared legacy MCP gateway reached its handler")
	}
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "dynamic_mcp_requires_managed_surface") {
		t.Fatalf("unexpected gateway result: %+v", result)
	}
}

func TestSharedAgentLoopRejectsLegacyManageSkillGatewayBeforeHandlerDispatch(t *testing.T) {
	registry := NewToolRegistry()
	var called bool
	if err := registry.Register(RegisteredTool{
		Name: "manage_skill", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
		Handler: func(map[string]interface{}) string {
			called = true
			return "unexpected skill dispatch"
		},
	}); err != nil {
		t.Fatalf("register gateway: %v", err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		legacySurface: newLegacyToolSurface([]map[string]interface{}{toolDef("manage_skill", "legacy skill gateway", map[string]interface{}{
			"action": map[string]interface{}{"type": "string"},
			"name":   map[string]interface{}{"type": "string"},
			"args":   map[string]interface{}{"type": "object"},
		}, []string{"action"})}),
	}
	result := cb.ExecuteToolCall("manage_skill", `{"action":"run","name":"unbound-skill","args":{}}`, "call-skill")
	if called {
		t.Fatal("shared legacy manage_skill gateway reached its handler")
	}
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "dynamic_skill_requires_managed_surface") {
		t.Fatalf("unexpected gateway result: %+v", result)
	}
}

func TestSharedAgentLoopRejectsStaleLegacySurfaceEpoch(t *testing.T) {
	registry := NewToolRegistry()
	var executed bool
	if err := registry.Register(RegisteredTool{
		Name: "read_file", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
		Handler: func(map[string]interface{}) string {
			executed = true
			return "unexpected execution"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	surface := newLegacyToolSurface([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "read_file"}},
	})
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{registry: registry}, legacySurface: surface}
	epochA := cb.BeginToolSurfaceEpoch(0)
	epochB := cb.BeginToolSurfaceEpoch(1)
	if epochA == "" || epochB == "" || epochA == epochB {
		t.Fatalf("epochs=%q,%q", epochA, epochB)
	}

	stale := cb.ExecuteToolCallWithContext("read_file", `{}`, "call-stale", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if executed || stale.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(stale.Result, "stale_surface") {
		t.Fatalf("stale result=%+v executed=%v", stale, executed)
	}
	current := cb.ExecuteToolCallWithContext("read_file", `{}`, "call-current", agent.ToolCallExecutionContext{SurfaceEpoch: epochB})
	if !executed || current.Outcome != agent.ToolExecutionOutcomeOK || current.Result != "unexpected execution" {
		t.Fatalf("current result=%+v executed=%v", current, executed)
	}
}

func TestLegacyToolSurfaceReplacementRetainsCurrentRequestEpoch(t *testing.T) {
	first := newLegacyToolSurface([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "first"}},
	})
	epoch := first.beginEpoch()
	if epoch == "" {
		t.Fatal("initial request epoch is empty")
	}

	replacement := first.replaceDefinitions([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "second"}},
	}, nil)
	if !replacement.epochIsCurrent(epoch) {
		t.Fatalf("replacement lost current request epoch %q", epoch)
	}
	if replacement.Allows("first") || !replacement.Allows("second") {
		t.Fatalf("replacement definitions were merged or stale: first=%v second=%v", replacement.Allows("first"), replacement.Allows("second"))
	}
	if successor := replacement.beginEpoch(); successor == "" || successor == epoch {
		t.Fatalf("successor epoch=%q, predecessor=%q", successor, epoch)
	}
	if replacement.epochIsCurrent(epoch) {
		t.Fatalf("predecessor epoch %q remained current after successor render", epoch)
	}
}

func TestSharedAgentLoopRequestRendererReplacesLegacySurfaceEachRequest(t *testing.T) {
	registry := NewToolRegistry()
	var firstCalls, secondCalls int
	for _, registration := range []RegisteredTool{
		{
			Name: "read_file", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
			Handler: func(map[string]interface{}) string {
				firstCalls++
				return "first"
			},
		},
		{
			Name: "write_file", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
			Handler: func(map[string]interface{}) string {
				secondCalls++
				return "second"
			},
		},
	} {
		if err := registry.Register(registration); err != nil {
			t.Fatalf("register %s: %v", registration.Name, err)
		}
	}

	firstDefs := []map[string]interface{}{toolDef("read_file", "first", nil, nil)}
	secondDefs := []map[string]interface{}{toolDef("write_file", "second", nil, nil)}
	h := &IMMessageHandler{
		registry:   registry,
		toolDefGen: NewToolDefinitionGenerator(nil, firstDefs),
	}
	cb := &sharedAgentLoopCallbacks{
		handler:       h,
		userID:        "request-renderer-user",
		userText:      "perform the requested action",
		legacySurface: newLegacyToolSurface(firstDefs),
	}

	epochA := cb.BeginToolSurfaceEpoch(0)
	if names := legacySurfaceTestNames(cb.BuildToolsForModelRequest(cb.userText, 0)); len(names) != 1 || names[0] != "read_file" {
		t.Fatalf("first request tools = %v", names)
	}
	if !cb.legacySurface.epochIsCurrent(epochA) || !cb.legacySurface.Allows("read_file") {
		t.Fatalf("first request was not bound to its rendered surface")
	}

	// Simulate a registry/generator update between two actual model requests.
	// The second request must be a replacement, never a union with the first.
	h.toolsMu.Lock()
	h.toolDefGen = NewToolDefinitionGenerator(nil, secondDefs)
	h.cachedTools = nil
	h.cachedToolDefGen = nil
	h.toolsCacheTime = time.Time{}
	h.toolsMu.Unlock()

	epochB := cb.BeginToolSurfaceEpoch(1)
	if names := legacySurfaceTestNames(cb.BuildToolsForModelRequest(cb.userText, 1)); len(names) != 1 || names[0] != "write_file" {
		t.Fatalf("second request tools = %v", names)
	}
	if epochA == epochB || cb.legacySurface.Allows("read_file") || !cb.legacySurface.Allows("write_file") {
		t.Fatalf("second request did not replace first surface: epochA=%q epochB=%q", epochA, epochB)
	}

	stale := cb.ExecuteToolCallWithContext("read_file", `{}`, "stale", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if firstCalls != 0 || stale.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(stale.Result, "stale_surface") {
		t.Fatalf("stale first request executed: calls=%d result=%+v", firstCalls, stale)
	}
	current := cb.ExecuteToolCallWithContext("write_file", `{}`, "current", agent.ToolCallExecutionContext{SurfaceEpoch: epochB})
	if secondCalls != 1 || current.Outcome != agent.ToolExecutionOutcomeOK || current.Result != "second" {
		t.Fatalf("second request was not admitted: calls=%d result=%+v", secondCalls, current)
	}
}

func legacySurfaceTestNames(definitions []map[string]interface{}) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if name := extractToolName(definition); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func TestAttachSharedLoopArtifactsKeepsFileDeliveryResponseSource(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		deliveredPaths:       []string{"C:\\artifacts\\report.pdf"},
		fileMaterializeNanos: 123,
		filesForwarded:       1,
	}
	resp := &IMAgentResponse{ResponseSource: "shared_agent_loop"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.ResponseSource != imResponseSourceFileDelivery.String() {
		t.Fatalf("ResponseSource = %q, want file delivery", resp.ResponseSource)
	}
	if resp.LocalFilePath != "C:\\artifacts\\report.pdf" || len(resp.LocalFilePaths) != 1 || resp.FileMaterializeNanos != 123 {
		t.Fatalf("file artifact response = %+v", resp)
	}
}

func TestAttachSharedLoopArtifactsMaterializesDesktopSemanticFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n%fake"))
	cb := &sharedAgentLoopCallbacks{
		handler:                  &IMMessageHandler{app: app},
		platform:                 "desktop",
		semanticDeliveryFileData: pdf,
		semanticDeliveryFileName: "weather.pdf",
		semanticDeliveryFileMIME: "application/pdf",
	}
	resp := &IMAgentResponse{ResponseSource: "shared_agent_loop"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.ResponseSource != imResponseSourceFileDelivery.String() || resp.FileMimeType != "application/pdf" {
		t.Fatalf("desktop semantic file response = %+v", resp)
	}
	if resp.LocalFilePath == "" || filepath.Base(resp.LocalFilePath) != "weather.pdf" {
		t.Fatalf("desktop chat must get a local path: %+v", resp)
	}
	if resp.FileData != "" {
		t.Fatalf("desktop event must drop inline FileData after materialize: %+v", resp)
	}
	if _, err := os.Stat(resp.LocalFilePath); err != nil {
		t.Fatalf("materialized PDF missing: %v", err)
	}

	im := &IMAgentResponse{}
	attachSharedLoopArtifacts(im, &sharedAgentLoopCallbacks{
		handler:                  &IMMessageHandler{app: app},
		platform:                 "lansenger",
		semanticDeliveryFileData: pdf,
		semanticDeliveryFileName: "weather.pdf",
		semanticDeliveryFileMIME: "application/pdf",
	})
	if im.FileData != pdf || im.LocalFilePath != "" {
		t.Fatalf("IM gateways must keep FileData and not materialize a local path: %+v", im)
	}
}

func TestAttachSharedLoopArtifactsMaterializesDesktopFileAlongsideExistingPath(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n%fake"))
	existing := filepath.Join(t.TempDir(), "other.txt")
	cb := &sharedAgentLoopCallbacks{
		handler:                  &IMMessageHandler{app: app},
		platform:                 "desktop",
		deliveredPaths:           []string{existing},
		filesForwarded:           1,
		semanticDeliveryFileData: pdf,
		semanticDeliveryFileName: "weather.pdf",
		semanticDeliveryFileMIME: "application/pdf",
	}
	resp := &IMAgentResponse{ResponseSource: "shared_agent_loop"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileData != "" {
		t.Fatalf("desktop event must drop FileData after materialize: %+v", resp)
	}
	if resp.LocalFilePath != existing {
		t.Fatalf("existing local path must stay primary: %+v", resp)
	}
	found := false
	for _, path := range resp.LocalFilePaths {
		if filepath.Base(path) == "weather.pdf" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("semantic PDF must be appended beside the existing path: %+v", resp)
	}
}

func TestCurrentChannelFileDeliveryReadyRequiresBoundDocument(t *testing.T) {
	grant := tool.InvocationGrant{SelectionID: "deliver-1"}
	deliver := func(deps []tool.ArtifactDependency) *semanticCallSurface {
		return &semanticCallSurface{plan: tool.ToolPlan{Selections: []tool.PlannedSelection{{
			ID:                   "deliver-1",
			AdapterName:          "semantic_deliver_current_file",
			Provider:             tool.ProviderBinding{Kind: "channel", ProviderID: "desktop"},
			FitProof:             tool.FitProof{MatchedCapability: "artifact.deliver.current_channel"},
			ArtifactDependencies: deps,
		}}}}
	}
	if currentChannelFileDeliveryReady(deliver([]tool.ArtifactDependency{{}}), grant) {
		t.Fatal("unbound dependency must not be ready")
	}
	if currentChannelFileDeliveryReady(deliver([]tool.ArtifactDependency{{
		ProducerSelection: "generate-1",
		Contract:          tool.ArtifactContract{Kind: "document"},
	}}), grant) {
		t.Fatal("producer-only dependency must wait for a published artifact")
	}
	if !currentChannelFileDeliveryReady(deliver([]tool.ArtifactDependency{{
		ArtifactID: "art-1",
		Artifact:   tool.ArtifactBinding{Kind: "document"},
		Contract:   tool.ArtifactContract{Kind: "document"},
	}}), grant) {
		t.Fatal("artifact-bound document must be ready")
	}
	if currentChannelFileDeliveryReady(deliver([]tool.ArtifactDependency{{
		ArtifactID: "art-1",
		Artifact:   tool.ArtifactBinding{Kind: "image"},
	}}), grant) {
		t.Fatal("image dependency must not auto-deliver as a document")
	}
	specified := &semanticCallSurface{plan: tool.ToolPlan{Selections: []tool.PlannedSelection{{
		ID:          "deliver-1",
		AdapterName: semanticSpecifiedTargetDeliveryAdapter,
		Provider:    tool.ProviderBinding{Kind: "channel", ProviderID: "desktop"},
		FitProof:    tool.FitProof{MatchedCapability: semanticSpecifiedTargetDeliveryCapability},
		ArtifactDependencies: []tool.ArtifactDependency{{
			ProducerSelection: "generate-1",
			Contract:          tool.ArtifactContract{Kind: "document"},
		}},
	}}}}
	if currentChannelFileDeliveryReady(specified, grant) {
		t.Fatal("specified-target delivery must not be host-auto flushed")
	}
}

func TestAttachSharedLoopArtifactsPrefersScreenshotResponseSource(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		deliveredPaths:     []string{"C:\\artifacts\\report.pdf"},
		filesForwarded:     1,
		screenshotImageKey: testOnePixelPNGBase64,
	}
	resp := &IMAgentResponse{ResponseSource: "shared_agent_loop"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.ResponseSource != imResponseSourceScreenshot.String() {
		t.Fatalf("ResponseSource = %q, want screenshot", resp.ResponseSource)
	}
	if resp.ImageKey != testOnePixelPNGBase64 || resp.LocalFilePath == "" {
		t.Fatalf("combined artifact response = %+v", resp)
	}
}

func TestSharedScreenshotCompletesACPToolEvent(t *testing.T) {
	const requestID = "acp-screenshot-event"
	var events []ACPToolEvent
	clear := globalACPToolSinks.set(requestID, func(ev ACPToolEvent) {
		events = append(events, ev)
	})
	defer clear()

	h := &IMMessageHandler{
		registry: NewToolRegistry(),
		client:   &http.Client{},
	}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Source: "test",
		HandlerProg: func(map[string]interface{}, tool.ProgressCallback) string {
			return "[screenshot_base64]" + testOnePixelPNGBase64
		},
	}); err != nil {
		t.Fatalf("Register screenshot tool: %v", err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, userID: "acp-user", platform: "lansenger", loopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: requestID, PolicyOwnerID: "acp-user"}}}
	if got := cb.ExecuteTool("screenshot", "{}"); got != toolPayloadPreparedMessage {
		t.Fatalf("ExecuteTool() = %q, want %q", got, toolPayloadPreparedMessage)
	}
	if len(events) != 2 || events[0].Phase != "start" || events[1].Phase != "end" || !events[1].OK {
		t.Fatalf("ACP events = %#v, want successful start/end pair", events)
	}
	if events[1].Result != toolPayloadPreparedMessage {
		t.Fatalf("ACP end result = %q, want prepared result", events[1].Result)
	}
}

func TestGUIAndCoreToolResultProjectionStayEquivalent(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })
	raw := strings.Repeat("build output\n", 2000)
	guiProjection := truncateToolResultForToolWithSession("bash", "owner-a", raw)
	coreProjection := agent.TruncateToolResultForToolWithSession("bash", "owner-a", raw)
	previewOnly := func(s string) string {
		if i := strings.Index(s, "\n\n[tool_result_handle]\n"); i >= 0 {
			return s[:i]
		}
		return s
	}
	if previewOnly(guiProjection) != previewOnly(coreProjection) {
		t.Fatal("GUI and core projections diverged before handle metadata")
	}
	for label, projection := range map[string]string{"gui": guiProjection, "core": coreProjection} {
		if !strings.Contains(projection, "[tool_result_handle]") || !strings.Contains(projection, "read_tool_result") {
			t.Fatalf("%s projection lost read-back metadata: %q", label, projection[max(0, len(projection)-300):])
		}
		if len(projection) > agent.MaxToolResultLen {
			t.Fatalf("%s projection exceeded budget: %d", label, len(projection))
		}
	}
}

func TestShouldUseSharedAgentLoop_RequiresFlag(t *testing.T) {
	// Package tests force legacy via resolveSharedAgentLoopMode.
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	if h.shouldUseSharedAgentLoop(ctx, "u1", nil) {
		t.Fatal("package tests must keep shared loop off by default")
	}
	// Production defaults still enable for new installs when env is unset.
	_ = os.Unsetenv("MACLAW_SHARED_AGENT_LOOP")
	_ = os.Unsetenv("MACLAW_SHARED_AGENT_LOOP_SHADOW")
	if corelib.AppConfigDefaults().SharedAgentLoopEnabled {
		if resolveSharedAgentLoopModeLive(h) != sharedAgentLoopOn {
			t.Fatal("expected production default on when no env/app config")
		}
	}
}

func TestShouldUseSharedAgentLoop_EnvEnable(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	if !shouldUseSharedAgentLoopLive(h, ctx, "u1", nil) {
		t.Fatal("expected true with env flag")
	}
}

func TestShouldUseSharedAgentLoop_Phase3AllowsBackground(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	bg := &LoopContext{Kind: LoopKindBackground}
	if !shouldUseSharedAgentLoopLive(h, bg, "u1", nil) {
		t.Fatal("background should use shared loop when enabled")
	}
	ok, reason := h.sharedAgentLoopEligibility(bg, nil)
	if !ok || reason != "background" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldUseSharedAgentLoop_Phase2AllowsLightAttachments(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	chat := &LoopContext{Kind: LoopKindChat}
	if !shouldUseSharedAgentLoopLive(h, chat, "u1", []MessageAttachment{{Type: "image", FileName: "a.png", MimeType: "image/png", Size: 1024}}) {
		t.Fatal("light image attachments should be allowed")
	}
}

func TestShouldUseSharedAgentLoop_RejectsWorkflowByDefault(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "")
	h := &IMMessageHandler{}
	wf := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true}
	if shouldUseSharedAgentLoopLive(h, wf, "u1", nil) {
		t.Fatal("workflow must not use shared loop without pilot env")
	}
	doc := &LoopContext{Kind: LoopKindChat, WorkflowDocPhase: true}
	if shouldUseSharedAgentLoopLive(h, doc, "u1", nil) {
		t.Fatal("workflow doc phase must never use shared loop")
	}
}

func TestShouldUseSharedAgentLoop_WorkflowPilot(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "1")
	h := &IMMessageHandler{}
	wf := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true}
	if !shouldUseSharedAgentLoopLive(h, wf, "u1", nil) {
		t.Fatal("workflow pilot should allow non-doc workflow")
	}
	// Doc phase still blocked.
	doc := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true, WorkflowDocPhase: true}
	if shouldUseSharedAgentLoopLive(h, doc, "u1", nil) {
		t.Fatal("doc phase must stay legacy even with pilot")
	}
}

func TestShouldUseSharedAgentLoop_ShadowNeverDiverts(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "shadow")
	h := &IMMessageHandler{}
	chat := &LoopContext{Kind: LoopKindChat}
	if shouldUseSharedAgentLoopLive(h, chat, "u1", nil) {
		t.Fatal("shadow mode must keep legacy path")
	}
	if resolveSharedAgentLoopModeLive(h) != sharedAgentLoopShadow {
		t.Fatal("mode should be shadow")
	}
	ok, _ := h.sharedAgentLoopEligibility(chat, nil)
	if !ok {
		t.Fatal("chat should be eligible even in shadow")
	}
}

func TestShouldUseSharedAgentLoop_CanaryPercentZero(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "0")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	before := processSharedLoopStats.skipCanary.Load()
	if shouldUseSharedAgentLoopLive(h, ctx, "any-user", nil) {
		t.Fatal("percent=0 must never divert")
	}
	if processSharedLoopStats.skipCanary.Load() <= before {
		t.Fatal("expected canary skip counter")
	}
}

func TestShouldUseSharedAgentLoop_IneligibleRecordsSkip(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "100")
	h := &IMMessageHandler{}
	// Workflow doc phase is never eligible.
	ctx := &LoopContext{Kind: LoopKindChat, WorkflowDocPhase: true}
	before := processSharedLoopStats.skipIneligible.Load()
	if shouldUseSharedAgentLoopLive(h, ctx, "u1", nil) {
		t.Fatal("doc phase must not use shared")
	}
	if processSharedLoopStats.skipIneligible.Load() <= before {
		t.Fatal("expected ineligible skip counter")
	}
	st := (&App{}).GetSharedAgentLoopStatus()
	if !strings.Contains(st.LastSkipReason, "workflow doc") && !strings.Contains(st.LastSkipReason, "ineligible") {
		// last may be ineligible:workflow doc phase
		if st.LastSkipReason == "" {
			t.Fatalf("last skip empty")
		}
	}
}

func TestShouldUseSharedAgentLoop_CanaryPercentSticky(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "50")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	// Stickiness: same user always same decision.
	a1 := shouldUseSharedAgentLoopLive(h, ctx, "sticky-user-xyz", nil)
	a2 := shouldUseSharedAgentLoopLive(h, ctx, "sticky-user-xyz", nil)
	if a1 != a2 {
		t.Fatal("canary must be sticky per user")
	}
	// Across many users some should pass and some fail at 50%.
	pass, fail := 0, 0
	for i := 0; i < 200; i++ {
		uid := "user-" + strings.Repeat("x", i%17) + string(rune('a'+i%26)) + string(rune('0'+i%10))
		if sharedLoopCanaryAllows(uid) {
			pass++
		} else {
			fail++
		}
	}
	if pass == 0 || fail == 0 {
		t.Fatalf("expected mix at 50%% canary, pass=%d fail=%d", pass, fail)
	}
}

func TestSharedLoopPercent_Bounds(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	if sharedLoopPercent() != 100 {
		t.Fatal("default 100")
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "30")
	if sharedLoopPercent() != 30 {
		t.Fatal("30")
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "999")
	if sharedLoopPercent() != 100 {
		t.Fatal("cap 100")
	}
}

func TestSharedAgentLoopEnabled_EnvOff(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "0")
	if resolveSharedAgentLoopModeLive(&IMMessageHandler{}) != sharedAgentLoopOff {
		t.Fatal("env 0 should disable")
	}
	if sharedAgentLoopEnabled(&IMMessageHandler{}) {
		t.Fatal("package shouldUse path must stay off under tests")
	}
}

func TestAppConfigDefaults_SharedAgentLoopEnabled(t *testing.T) {
	if !corelib.AppConfigDefaults().SharedAgentLoopEnabled {
		t.Fatal("new installs should default SharedAgentLoopEnabled=true")
	}
}

func TestSharedAgentLoopCallbacks_RouteTurn(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		llmCfg: corelib.MaclawLLMConfig{Model: "m1", ProviderName: "p1"},
		route:  modelRouteDecision{Task: "fast", Source: "aux", Model: "m1", Provider: "p1", Reason: "short"},
	}
	cfg, d, ok := cb.RouteTurn("hi")
	if !ok || cfg.Model != "m1" || d.Source != "aux" || !strings.Contains(d.Reason, "shared") {
		t.Fatalf("cfg=%+v d=%+v ok=%v", cfg, d, ok)
	}
}

func TestSharedAgentLoopEscalatesForContextOnlyReasoningRoute(t *testing.T) {
	base := corelib.MaclawLLMConfig{
		URL:           "https://models.example/v1",
		Model:         "same-model",
		ContextLength: 32_000,
	}
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {Model: base.Model, ContextLength: 400_000},
	})
	h := &IMMessageHandler{
		app:              app,
		standaloneConfig: &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig { return base }},
	}
	cb := &sharedAgentLoopCallbacks{
		handler:   h,
		llmCfg:    base,
		route:     modelRouteDecision{Task: string(llm.TaskFast), Source: "route", Model: base.Model},
		toolCalls: 1,
	}

	cb.maybeEscalateAfterTools()
	if !cb.escalated || cb.route.Task != string(llm.TaskReasoning) || cb.llmCfg.ContextLength != 400_000 {
		t.Fatalf("context-only reasoning route was not applied: escalated=%v route=%+v cfg=%+v", cb.escalated, cb.route, cb.llmCfg)
	}
	if got, want := cb.llmCfg.EffectiveContextTokens(), 320_000; got != want {
		t.Fatalf("effective context = %d, want %d", got, want)
	}
}

func TestSharedAgentLoopDoesNotEscalateSemanticLightLookup(t *testing.T) {
	base := corelib.MaclawLLMConfig{
		URL:           "https://models.example/v1",
		Model:         "same-model",
		ContextLength: 32_000,
	}
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {Model: base.Model, ContextLength: 400_000},
	})
	h := &IMMessageHandler{
		app:              app,
		standaloneConfig: &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig { return base }},
	}
	cb := &sharedAgentLoopCallbacks{
		handler:         h,
		llmCfg:          base,
		route:           modelRouteDecision{Task: string(llm.TaskFast), Source: "route", Model: base.Model},
		toolCalls:       1,
		semanticSurface: &semanticCallSurface{},
		loopCtx:         &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}}},
	}
	cb.maybeEscalateAfterTools()
	if cb.escalated || cb.surfaceRefreshPending || cb.route.Task != string(llm.TaskFast) {
		t.Fatalf("light semantic lookup escalated: escalated=%v pending=%v route=%+v", cb.escalated, cb.surfaceRefreshPending, cb.route)
	}
	if cb.llmCfg.ContextLength != 32_000 {
		t.Fatalf("light semantic lookup changed context: %+v", cb.llmCfg)
	}
}

func TestSemanticLightRefreshKeepsLightPrompt(t *testing.T) {
	const lightPrompt = "light fence: do not write files"
	cb := &sharedAgentLoopCallbacks{
		handler:               &IMMessageHandler{},
		semanticSurface:       &semanticCallSurface{},
		tools:                 []map[string]interface{}{toolDef("invoke_search", "opaque", nil, nil)},
		systemPrompt:          lightPrompt,
		surfaceRefreshPending: true,
		loopCtx:               &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}}},
	}
	if !cb.RefreshAfterToolExecution("invoke_search") {
		t.Fatal("light semantic refresh should still complete")
	}
	if cb.systemPrompt != lightPrompt {
		t.Fatalf("light prompt was replaced with full-agent text: %q", cb.systemPrompt)
	}
	if len(cb.tools) != 1 || extractToolName(cb.tools[0]) != "invoke_search" {
		t.Fatalf("light semantic refresh changed tools: %#v", cb.tools)
	}
}

func TestSemanticLightRefreshPushesConsumedGrantWithoutEscalatePending(t *testing.T) {
	const lightPrompt = "light fence: do not write files"
	cb := &sharedAgentLoopCallbacks{
		handler:         &IMMessageHandler{},
		semanticSurface: &semanticCallSurface{grants: map[string]tool.InvocationGrant{}},
		tools:           nil,
		systemPrompt:    lightPrompt,
		loopCtx:         &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}}},
	}
	if cb.surfaceRefreshPending {
		t.Fatal("consumed lookup must not require an escalate marker")
	}
	if !cb.RefreshAfterToolExecution("invoke_search") {
		t.Fatal("consumed semantic grant must refresh so the loop drops the spent tool")
	}
	if cb.systemPrompt != lightPrompt {
		t.Fatalf("light prompt was replaced with full-agent text: %q", cb.systemPrompt)
	}
	if len(cb.BuildTools("")) != 0 {
		t.Fatalf("consumed lookup still exposed tools: %#v", cb.BuildTools(""))
	}
}

func TestSemanticFullRefreshReappliesGrantPromptFence(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		handler:               &IMMessageHandler{},
		semanticSurface:       &semanticCallSurface{grants: map[string]tool.InvocationGrant{"invoke_pdf": {}}},
		systemPrompt:          "FULL SYSTEM: prefer web_search / web_fetch",
		surfaceRefreshPending: true,
		loopCtx:               &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerFull), PromptProfile: "full"}}},
	}
	if !cb.RefreshAfterToolExecution("invoke_search") {
		t.Fatal("full semantic refresh should complete")
	}
	if !strings.Contains(cb.systemPrompt, "one-time grants") || !strings.Contains(cb.systemPrompt, "web_search") {
		t.Fatalf("full prompt rebuild dropped grant fence: %q", cb.systemPrompt)
	}
}

func TestLegacyAgentLoopEscalatesForContextOnlyReasoningRoute(t *testing.T) {
	base := corelib.MaclawLLMConfig{
		URL:           "https://models.example/v1",
		Model:         "same-model",
		ContextLength: 32_000,
	}
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {Model: base.Model, ContextLength: 400_000},
	})
	h := &IMMessageHandler{
		app:              app,
		standaloneConfig: &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig { return base }},
	}
	run := &agentLoopRunState{
		ActiveConfig: base,
		RouteTask:    string(llm.TaskFast),
		RouteSource:  "route",
		RouteModel:   base.Model,
	}

	h.escalateRunStateToReasoning(run, "tools requested after light turn")
	if !run.RouteEscalated || run.RouteTask != string(llm.TaskReasoning) || run.ActiveConfig.ContextLength != 400_000 {
		t.Fatalf("context-only reasoning route was not applied: run=%+v", run)
	}
	if got, want := run.EffectiveTokenLimit, 320_000; got != want {
		t.Fatalf("effective context = %d, want %d", got, want)
	}
}

func TestCompletionBonusRoundUsesEscalatedRunConfig(t *testing.T) {
	light := corelib.MaclawLLMConfig{Model: "same-model", ContextLength: 32_000}
	escalated := light
	escalated.ContextLength = 400_000
	run := newAgentLoopRunState(light)
	run.ActiveConfig = escalated

	got := completionConfigFromRunState(run, light)
	if got.ContextLength != 400_000 || got.EffectiveContextTokens() != 320_000 {
		t.Fatalf("bonus-round config = %+v, want escalated 400K/320K", got)
	}
}

func TestRoundPrepUsesEscalatedRunConfig(t *testing.T) {
	light := corelib.MaclawLLMConfig{Model: "same-model", ContextLength: 32_000}
	escalated := light
	escalated.ContextLength = 400_000
	run := newAgentLoopRunState(light)
	run.ActiveConfig = escalated

	got := activeAgentLoopConfig(run, light)
	if got.ContextLength != 400_000 || got.EffectiveContextTokens() != 320_000 {
		t.Fatalf("round-prep config = %+v, want escalated 400K/320K", got)
	}
}

func TestSharedAgentLoopCallbacks_UpgradeLightPromptKeepsCurrentAttachmentOutOfComputerUse(t *testing.T) {
	resetComputerUseSessionForTest(t)
	defs := []map[string]interface{}{
		toolDef("read_file", "read local file", nil, nil),
		toolDef("computer_observe", "observe desktop", nil, nil),
		toolDef("computer_click", "click desktop", nil, nil),
		toolDef("gui_click", "legacy click", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := NewLoopContext("shared-attachment-upgrade", 3, nil)
	ctx.Runtime.Execution = ExecutionProfile{
		Layer:         string(executionLayerLight),
		PromptProfile: "light",
	}
	// Simulate a desktop task immediately followed by an uploaded attachment.
	// The raw text has no staged attachment marker yet, so the callback field is
	// the signal that must survive the profile upgrade.
	markComputerUseSessionActive()
	cb := &sharedAgentLoopCallbacks{
		handler:          h,
		loopCtx:          ctx,
		userID:           "desktop-user:attachment-upgrade",
		userText:         "请阅读这个压缩包",
		hasLocalFileWork: true,
	}
	if !cb.UpgradeLightPromptToFull("need local file reader") {
		t.Fatal("light profile should upgrade to full")
	}
	// The upgrade changes only successor policy posture. It must not render a
	// wider callback-local surface while the current model response is still
	// owned by its already-sent request.
	if len(cb.tools) != 0 || cb.legacySurface.HasSnapshot() {
		t.Fatalf("light upgrade rendered an unowned current surface: tools=%#v snapshot=%v", cb.tools, cb.legacySurface.HasSnapshot())
	}
	tools := cb.BuildToolsForModelRequest(cb.userText, 1)
	names := toolNameSetForWorkflowFilterTest(tools)
	if names["computer_observe"] || names["computer_click"] {
		t.Fatalf("attachment upgrade must not reintroduce Computer Use: %#v", names)
	}
	if !names["read_file"] || names["gui_click"] {
		t.Fatalf("attachment upgrade must preserve file tools but remove legacy desktop tools: %#v", names)
	}
	if computerUseSessionActive() {
		t.Fatal("attachment upgrade should clear the stale Computer Use session")
	}
}

func TestSharedAgentLoopRefreshDoesNotRenderLegacySurfaceBeforeSuccessorRequest(t *testing.T) {
	initialDefs := []map[string]interface{}{toolDef("read_file", "read", nil, nil)}
	fullDefs := []map[string]interface{}{toolDef("bash", "bash", nil, nil)}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, fullDefs)}
	ctx := NewLoopContext("shared-refresh-boundary", 3, nil)
	ctx.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	cb := &sharedAgentLoopCallbacks{
		handler:               h,
		loopCtx:               ctx,
		userID:                "shared-refresh-boundary-user",
		userText:              "inspect workspace",
		tools:                 initialDefs,
		legacySurface:         newLegacyToolSurface(initialDefs),
		surfaceRefreshPending: true,
	}
	epoch := cb.BeginToolSurfaceEpoch(0)
	if epoch == "" || !cb.legacySurface.epochIsCurrent(epoch) {
		t.Fatalf("current request epoch was not established: %q", epoch)
	}
	if !cb.RefreshAfterToolExecution("read_file") {
		t.Fatal("refresh should signal a successor policy update")
	}
	if !cb.legacySurface.epochIsCurrent(epoch) || !cb.legacySurface.Allows("read_file") || cb.legacySurface.Allows("bash") {
		t.Fatalf("refresh rewrote current request surface: epoch=%q read=%v bash=%v", epoch, cb.legacySurface.Allows("read_file"), cb.legacySurface.Allows("bash"))
	}
	if len(cb.tools) != 1 || extractToolName(cb.tools[0]) != "read_file" {
		t.Fatalf("refresh rewrote current tool definitions: %#v", cb.tools)
	}

	successor := cb.BeginToolSurfaceEpoch(1)
	rendered := cb.BuildToolsForModelRequest(cb.userText, 1)
	if successor == "" || successor == epoch || !cb.legacySurface.epochIsCurrent(successor) {
		t.Fatalf("successor epoch=%q predecessor=%q", successor, epoch)
	}
	if names := legacySurfaceTestNames(rendered); len(names) != 1 || names[0] != "bash" {
		t.Fatalf("successor replacement = %v", names)
	}
	if cb.legacySurface.Allows("read_file") || !cb.legacySurface.Allows("bash") {
		t.Fatalf("successor did not replace current surface")
	}
}

func TestSharedAgentLoopUpgradeLightPromptToFullKeepsSemanticSurface(t *testing.T) {
	ctx := NewLoopContext("chat", 300, nil)
	ctx.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	surface := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "search", Effects: []tool.EffectClass{tool.EffectReadOnly}},
		}},
		grants: map[string]tool.InvocationGrant{"invoke_capability": {SelectionID: "search"}},
	}
	cb := &sharedAgentLoopCallbacks{
		loopCtx: ctx,
		tools: []map[string]interface{}{
			toolDef("invoke_capability", "opaque", nil, nil),
		},
		semanticSurface: surface,
	}
	if !cb.ManagedSemanticTurn() {
		t.Fatal("semantic surface must report a managed turn")
	}
	if cb.UpgradeLightPromptToFull("need more reasoning") {
		t.Fatal("semantic light profile must not upgrade to a fake full surface")
	}
	if cb.semanticSurface != surface || len(cb.tools) != 1 || extractToolName(cb.tools[0]) != "invoke_capability" {
		t.Fatalf("semantic upgrade restored a different tool surface: surface=%p tools=%#v", cb.semanticSurface, cb.tools)
	}
	if !cb.IsToolAllowed("invoke_capability") {
		t.Fatal("planned semantic grant was rejected after refusing upgrade")
	}
	if cb.IsToolAllowed("write_file") || cb.IsToolAllowed("web_fetch") {
		t.Fatal("ungranted tools were authorized after refusing semantic upgrade")
	}
}

func TestSharedAgentLoopUpgradeLightPromptToFullKeepsLeftoverBound(t *testing.T) {
	ctx := NewLoopContext("chat", 300, nil)
	ctx.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	ctx.Runtime.RoutingMissFallback = true
	cb := &sharedAgentLoopCallbacks{
		loopCtx: ctx,
		tools: []map[string]interface{}{
			toolDef("memory", "memory", nil, nil),
			toolDef("web_fetch", "fetch", nil, nil),
		},
	}
	if cb.UpgradeLightPromptToFull("tool_deny_retry:bash") {
		t.Fatal("leftover light miss must not upgrade into a full leftover kitchen sink")
	}
	if !ctx.Runtime.Execution.IsLight() {
		t.Fatal("leftover upgrade must not mutate the light execution profile")
	}
	if len(cb.tools) != 2 || extractToolName(cb.tools[0]) != "memory" {
		t.Fatalf("leftover tools were rebuilt: %#v", cb.tools)
	}
}

func TestSemanticGrantRejectDoesNotAskReauthorize(t *testing.T) {
	got := semanticGrantRejectMessage("selection_not_authorized")
	if !strings.Contains(got, "selection_not_authorized") || !strings.Contains(got, "Do not ask the user to re-authorize") {
		t.Fatalf("got=%q", got)
	}
	replayed := semanticGrantRejectMessage("invocation_grant_replayed")
	if !strings.Contains(replayed, "invocation_grant_replayed") || !strings.Contains(replayed, "Do not ask the user to re-authorize") {
		t.Fatalf("replayed=%q", replayed)
	}
}

func TestSemanticAdvanceAfterSuccessKeepsPublishedResult(t *testing.T) {
	got := semanticAdvanceAfterSuccess("PDF artifact published; deliver it through the current-channel file adapter.", fmt.Errorf("route_state_corrupt"))
	if !strings.Contains(got, "PDF artifact published") || strings.Contains(got, "semantic_plan_advance_failed") {
		t.Fatalf("got=%q", got)
	}
	if !strings.Contains(got, "do not retry this grant") {
		t.Fatalf("got=%q", got)
	}
}

func TestSemanticStaleGrantNameCannotAliasTheCurrentLookup(t *testing.T) {
	const live = "web_search"
	const stale = "invoke_MY0ko7TXyVVgdwJ4HfCtOjfs"
	surface := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "search", Effects: []tool.EffectClass{tool.EffectReadOnly}, FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		}},
		grants: map[string]tool.InvocationGrant{live: {SelectionID: "search", AdapterName: semanticTrustedWebSearchAdapter}},
	}
	cb := &sharedAgentLoopCallbacks{
		semanticSurface: surface,
		loopCtx:         &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}}},
	}
	if surface.resolveFunctionName(stale) != stale {
		t.Fatalf("stale name resolved to %q, want itself", surface.resolveFunctionName(stale))
	}
	if surface.resolveFunctionName("web_search") != live {
		t.Fatalf("web_search resolved to %q, want the live grant", surface.resolveFunctionName("web_search"))
	}
	if surface.resolveFunctionName(previousTurnSemanticToolName) != previousTurnSemanticToolName {
		t.Fatal("history placeholder must not consume the live lookup grant")
	}
	if cb.IsToolAllowed(stale) || cb.IsToolAllowedForPromptProfile(stale, agent.PromptProfileLight) {
		t.Fatal("leftover invoke_* token must not be authorized as the live lookup")
	}
	if !cb.IsToolAllowed("web_search") {
		t.Fatal("the stable search name must be authorized")
	}
	if cb.IsToolAllowed(previousTurnSemanticToolName) || cb.IsToolAllowed("invented_invoke") || cb.IsToolAllowed("write_file") || cb.IsToolAllowed("web_fetch") {
		t.Fatal("non-grant names were authorized by leftover invoke_* remapping")
	}
	if allowed, _ := cb.IsToolCallAllowed(stale, `{"query":"南京天气"}`); allowed {
		t.Fatal("search-shaped leftover invoke_* must not be admitted")
	}
	if allowed, _ := cb.IsToolCallAllowed("web_search", `{"query":"杭州天气"}`); !allowed {
		t.Fatal("search-shaped web_search must be admitted")
	}
	if allowed, _ := cb.IsToolCallAllowed(stale, `{"path":"x.go"}`); allowed {
		t.Fatal("write-shaped leftover invoke_* must not be admitted")
	}
	if allowed, _ := cb.IsToolCallAllowed(previousTurnSemanticToolName, `{"query":"杭州天气"}`); allowed {
		t.Fatal("history placeholder must not be executable")
	}
	full := &sharedAgentLoopCallbacks{
		semanticSurface: surface,
		loopCtx:         &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerFull), PromptProfile: "full"}}},
	}
	if full.IsToolAllowed(previousTurnSemanticToolName) {
		t.Fatal("full weather+PDF turns must not treat previous_turn_tool as a grant")
	}
	if allowed, _ := full.IsToolCallAllowed("web_search", `{"query":"杭州天气"}`); !allowed {
		t.Fatal("full turns must admit the stable web_search name")
	}
	child := *surface
	child.replan = &semanticReplanInput{Attempts: 1}
	if child.resolveFunctionName(stale) != stale {
		t.Fatalf("child revision aliased parent spelling to %q", child.resolveFunctionName(stale))
	}
	two := &semanticCallSurface{grants: map[string]tool.InvocationGrant{
		live:  {SelectionID: "search"},
		stale: {SelectionID: "fetch"},
	}}
	if two.resolveFunctionName("invoke_0PhPG80sU5cAiJrEk5wbcQeX") != "invoke_0PhPG80sU5cAiJrEk5wbcQeX" {
		t.Fatal("ambiguous live grants must not alias an expired name")
	}
	write := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "write", Effects: []tool.EffectClass{tool.EffectSensitive}, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityFSWriteLocal}},
		}},
		grants: map[string]tool.InvocationGrant{live: {SelectionID: "write"}},
	}
	if write.resolveFunctionName(stale) != stale {
		t.Fatal("a sole write grant must not inherit last turn's lookup token")
	}
	read := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "read", Effects: []tool.EffectClass{tool.EffectReadOnly}, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityFSReadLocal}},
		}},
		grants: map[string]tool.InvocationGrant{live: {SelectionID: "read"}},
	}
	if read.resolveFunctionName(stale) != stale {
		t.Fatal("a sole file-read grant must not inherit last turn's lookup token")
	}
	fetch := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "fetch", Effects: []tool.EffectClass{tool.EffectReadOnly}, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityInformationFetchWeb}},
		}},
		grants: map[string]tool.InvocationGrant{live: {SelectionID: "fetch"}},
	}
	if fetch.resolveFunctionName(stale) != stale {
		t.Fatal("a sole fetch grant must not inherit a search token")
	}
	clock := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "clock", Effects: []tool.EffectClass{tool.EffectReadOnly}, FitProof: tool.FitProof{MatchedCapability: "information.current_time"}},
		}},
		grants: map[string]tool.InvocationGrant{live: {SelectionID: "clock"}},
	}
	if clock.resolveFunctionName(stale) != stale {
		t.Fatal("a sole clock grant must not inherit a search token")
	}
}

func TestRewriteExpiredSemanticGrantNamesLeavesLiveTokens(t *testing.T) {
	const live = "web_search"
	const stale = "invoke_MY0ko7TXyVVgdwJ4HfCtOjfs"
	original := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: stale, Arguments: `{"query":"北京天气"}`}}}},
		{Role: "tool", ToolName: previousTurnSemanticToolName, Content: map[string]interface{}{"name": previousTurnSemanticToolName, "arguments": `{"query":"北京天气"}`}},
		{Role: "assistant", ToolCalls: []map[string]interface{}{{"function": map[string]interface{}{"name": live}}}},
	}
	got := rewriteExpiredSemanticGrantNames(original, map[string]bool{live: true})
	calls, ok := got[0].ToolCalls.([]llm.ToolCall)
	if !ok || len(calls) != 1 || calls[0].Function.Name != stale {
		t.Fatalf("leftover invoke_* must stay in history: %#v", got[0].ToolCalls)
	}
	if original[1].ToolName != previousTurnSemanticToolName {
		t.Fatal("history rewrite mutated the stored tool name")
	}
	if got[1].ToolName != live {
		t.Fatalf("history placeholder = %q, want %q", got[1].ToolName, live)
	}
	content, _ := got[1].Content.(map[string]interface{})
	if content["name"] != live {
		t.Fatalf("history placeholder content name = %#v", got[1].Content)
	}
	maps, ok := got[2].ToolCalls.([]map[string]interface{})
	if !ok || maps[0]["function"].(map[string]interface{})["name"] != live {
		t.Fatalf("live grant was rewritten: %#v", got[2].ToolCalls)
	}
	clean := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{Name: live}}}},
	}
	if rewritten := rewriteExpiredSemanticGrantNames(clean, map[string]bool{live: true}); len(rewritten) != 1 || &rewritten[0] != &clean[0] {
		t.Fatal("history without the leftover placeholder must be left in place")
	}
	boxed := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []interface{}{llm.ToolCall{Function: llm.ToolCallFunction{Name: previousTurnSemanticToolName}}}},
	}
	gotBoxed := rewriteExpiredSemanticGrantNames(boxed, map[string]bool{live: true})
	boxedCalls, ok := gotBoxed[0].ToolCalls.([]interface{})
	if !ok || len(boxedCalls) != 1 {
		t.Fatalf("boxed tool calls=%#v", gotBoxed[0].ToolCalls)
	}
	boxedCall, ok := boxedCalls[0].(llm.ToolCall)
	if !ok || boxedCall.Function.Name != live {
		t.Fatalf("boxed history placeholder=%#v", boxedCalls[0])
	}
	stringFn := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []map[string]interface{}{{"function": map[string]string{"name": previousTurnSemanticToolName}}}},
	}
	gotFn := rewriteExpiredSemanticGrantNames(stringFn, map[string]bool{live: true})
	fnMaps, ok := gotFn[0].ToolCalls.([]map[string]interface{})
	if !ok || fnMaps[0]["function"].(map[string]string)["name"] != live {
		t.Fatalf("string-function history placeholder=%#v", gotFn[0].ToolCalls)
	}
	pdfOnly := []agent.ConversationEntry{
		{Role: "tool", ToolName: previousTurnSemanticToolName, Content: map[string]interface{}{"name": previousTurnSemanticToolName}},
	}
	gotPDF := rewriteExpiredSemanticGrantNames(pdfOnly, map[string]bool{"generate_pdf": true})
	if gotPDF[0].ToolName != previousTurnSemanticToolName {
		t.Fatalf("leftover search history must not become generate_pdf: %q", gotPDF[0].ToolName)
	}
	gotBoth := rewriteExpiredSemanticGrantNames(pdfOnly, map[string]bool{"web_search": true, "generate_pdf": true})
	if gotBoth[0].ToolName != live {
		t.Fatalf("live web_search must still claim leftover search history: %q", gotBoth[0].ToolName)
	}
}

func TestSemanticLightLookupDenialTellsModelToAnswerFromEvidence(t *testing.T) {
	light := &sharedAgentLoopCallbacks{
		semanticSurface: &semanticCallSurface{grants: map[string]tool.InvocationGrant{"invoke_search": {}}},
		loopCtx:         &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}}},
	}
	msg := light.ToolDenialMessage("write_file")
	if !strings.Contains(msg, "lookup already returned evidence") || !strings.Contains(msg, "write_file") {
		t.Fatalf("light lookup deny=%q", msg)
	}
	full := &sharedAgentLoopCallbacks{
		semanticSurface: &semanticCallSurface{grants: map[string]tool.InvocationGrant{"invoke_pdf": {}}},
		loopCtx:         &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerFull), PromptProfile: "full"}}},
	}
	if got := full.ToolDenialMessage("bash"); got != "" {
		t.Fatalf("full semantic deny should use generic policy text, got %q", got)
	}
	child := &sharedAgentLoopCallbacks{
		semanticSurface: &semanticCallSurface{
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
				{ID: "search", Effects: []tool.EffectClass{tool.EffectReadOnly}, FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
			}},
			grants: map[string]tool.InvocationGrant{"invoke_7hgQ9D9U1q0Bxui-AsGXOics": {SelectionID: "search"}},
			replan: &semanticReplanInput{Attempts: 1},
		},
		loopCtx: &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}}},
	}
	msg = child.ToolDenialMessage("invoke_MY0ko7TXyVVgdwJ4HfCtOjfs")
	if !strings.Contains(msg, "invoke_7hgQ9D9U1q0Bxui-AsGXOics") || !strings.Contains(msg, "not the current lookup grant") {
		t.Fatalf("child-revision deny=%q", msg)
	}
	if got := child.ToolDenialMessage(previousTurnSemanticToolName); strings.Contains(got, "invoke_7hgQ9D9U1q0Bxui-AsGXOics") || !strings.Contains(got, "not available") {
		t.Fatalf("history placeholder deny=%q", got)
	}
	writeOnly := &sharedAgentLoopCallbacks{
		semanticSurface: &semanticCallSurface{
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
				{ID: "write", Effects: []tool.EffectClass{tool.EffectSensitive}, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityFSWriteLocal}},
			}},
			grants: map[string]tool.InvocationGrant{"invoke_7hgQ9D9U1q0Bxui-AsGXOics": {SelectionID: "write"}},
		},
		loopCtx: &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}}},
	}
	if got := writeOnly.ToolDenialMessage("invoke_MY0ko7TXyVVgdwJ4HfCtOjfs"); strings.Contains(got, "invoke_7hgQ9D9U1q0Bxui-AsGXOics") {
		t.Fatalf("denial advertised a write grant as the current lookup: %q", got)
	}
}

func TestSharedSemanticLightPromptPolicyUsesPlannedEffects(t *testing.T) {
	ctx := NewLoopContext("semantic-light-policy", 3, nil)
	ctx.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	surface := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "read", AdapterName: "misleading_mutation_name", Effects: []tool.EffectClass{tool.EffectReadOnly}},
			{ID: "external", AdapterName: "innocent_name", Effects: []tool.EffectClass{tool.EffectExternalEffect}},
		}},
		grants: map[string]tool.InvocationGrant{
			"invoke_read":     {SelectionID: "read"},
			"invoke_external": {SelectionID: "external"},
		},
	}
	cb := &sharedAgentLoopCallbacks{loopCtx: ctx, semanticSurface: surface}
	if !cb.IsToolAllowedForPromptProfile("invoke_read", agent.PromptProfileLight) {
		t.Fatal("read-only semantic selection was rejected by opaque grant name")
	}
	if cb.IsToolAllowedForPromptProfile("invoke_external", agent.PromptProfileLight) {
		t.Fatal("external-effect semantic selection bypassed light policy")
	}
	if cb.IsToolAllowedForPromptProfile("invented_invoke", agent.PromptProfileLight) {
		t.Fatal("invented semantic function was admitted without a grant")
	}
	if !cb.IsToolAllowed("invoke_read") {
		t.Fatal("light authorizer rejected a granted read-only selection")
	}
	if cb.IsToolAllowed("invoke_external") {
		t.Fatal("light authorizer admitted a granted mutating selection")
	}
	if cb.IsToolAllowed("web_fetch") || cb.IsToolAllowed("write_file") {
		t.Fatal("ungranted names were authorized on a light semantic surface")
	}
	filtered := agent.FilterToolDefinitionsByAuthorizer(cb, []map[string]interface{}{
		toolDef("invoke_read", "read", nil, nil),
		toolDef("invoke_external", "mutate", nil, nil),
		toolDef("web_fetch", "fetch", nil, nil),
	})
	if len(filtered) != 1 || extractToolName(filtered[0]) != "invoke_read" {
		t.Fatalf("light authorizer filter=%#v, want only invoke_read", filtered)
	}
}

func TestLocalFileWorkFenceSurvivesToolRecoveryAndAugmentation(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("read_file", "read local file", nil, nil),
		toolDef("computer_observe", "observe desktop", nil, nil),
		toolDef("computer_click", "click desktop", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := NewLoopContext("local-file-recovery", 3, nil)
	ctx.ComputerUseBlockedForLocalFileWork = true

	restored, _, _ := h.restoreToolsAfterSkillRecover("desktop-user:local-file-recovery", ctx, h.getTools(), agentLoopPhase{})
	restoredNames := toolNameSetForWorkflowFilterTest(restored)
	if restoredNames["computer_observe"] || restoredNames["computer_click"] || !restoredNames["read_file"] {
		t.Fatalf("recovery bypassed local-file Computer Use fence: %#v", restoredNames)
	}

	augmented, _ := h.finalizeInjectionAugmentedTools(ctx, "desktop-user:local-file-recovery", h.getTools())
	augmentedNames := toolNameSetForWorkflowFilterTest(augmented)
	if augmentedNames["computer_observe"] || augmentedNames["computer_click"] || !augmentedNames["read_file"] {
		t.Fatalf("augmentation bypassed local-file Computer Use fence: %#v", augmentedNames)
	}
}

func TestSharedAgentLoopCallbacks_RejectsStaleComputerUseCallForLocalFileWork(t *testing.T) {
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_click",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop clicked"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx := NewLoopContext("shared-local-file-execution", 1, nil)
	ctx.ComputerUseBlockedForLocalFileWork = true
	cb := &sharedAgentLoopCallbacks{
		handler:  &IMMessageHandler{registry: registry},
		loopCtx:  ctx,
		userText: "请阅读附件",
	}
	if got := cb.ExecuteTool("computer_click", `{}`); !strings.Contains(got, "local attachment") {
		t.Fatalf("shared stale Computer Use call = %q, want local attachment rejection", got)
	}
	if called {
		t.Fatal("shared callback must not invoke Computer Use handler for local-file work")
	}
}

func TestSharedAgentLoopCallbacks_TransformConversationInjectsLiveSteer(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:shared-steer"
	h.accumulateInjection(userID, buildGuideLaunchInjection("switch to SQLite"))
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID}
	conversation := []interface{}{map[string]string{"role": "user", "content": "build a database"}}

	next := cb.TransformConversation(conversation)
	if len(next) != 2 {
		t.Fatalf("conversation len = %d, want 2", len(next))
	}
	msg, ok := next[1].(map[string]string)
	if !ok || msg["role"] != "user" || !strings.Contains(msg["content"], "switch to SQLite") {
		t.Fatalf("unexpected shared steer injection: %#v", next[1])
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("shared loop transform should consume pending steer once")
	}
}

func TestSharedLoopDisplayReasoningUsesAllAssistantSummaries(t *testing.T) {
	delta := []agent.ConversationEntry{
		{Role: "user", Content: "weather"},
		{Role: "assistant", Content: "checking", ReasoningContent: "First summary."},
		{Role: "tool", Content: "weather result"},
		{Role: "assistant", Content: "answer", ReasoningContent: "Final display-safe summary."},
	}
	result := agent.LoopResult{HistoryDelta: delta, Reasoning: "First summary.\n\nFinal display-safe summary."}
	if got, want := sharedLoopDisplayReasoning(result), "First summary.\n\nFinal display-safe summary."; got != want {
		t.Fatalf("sharedLoopDisplayReasoning() = %q, want %q", got, want)
	}
}

func TestSharedLoopDisplayReasoningSkipsEmptyAssistantTurns(t *testing.T) {
	delta := []agent.ConversationEntry{
		{Role: "assistant", Content: "first", ReasoningContent: "usable summary"},
		{Role: "assistant", Content: "final", ReasoningContent: "  "},
	}
	if got, want := sharedLoopDisplayReasoning(agent.LoopResult{HistoryDelta: delta}), "usable summary"; got != want {
		t.Fatalf("sharedLoopDisplayReasoning() = %q, want %q", got, want)
	}
}

func TestSharedAgentLoopCallbacks_DetectsReplanRevision(t *testing.T) {
	ctx := NewLoopContext("shared-replan", 3, nil)
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, userID: "desktop-user:shared-replan", loopCtx: ctx}
	cb.TransformConversation(nil)
	_, finish, err := cb.LLMRequestContext(0)
	if err != nil {
		t.Fatalf("LLMRequestContext: %v", err)
	}
	defer finish(nil)
	if cb.LLMReplanRequested() {
		t.Fatal("unexpected replan before steering")
	}
	ctx.RequestReplan()
	if !cb.LLMReplanRequested() {
		t.Fatal("shared callback did not observe live-steer replan")
	}
}

func TestSharedAgentLoopCallbacks_DetectsReplanBetweenTransformAndRequest(t *testing.T) {
	ctx := NewLoopContext("shared-replan-race", 3, nil)
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, userID: "desktop-user:shared-replan-race", loopCtx: ctx}

	cb.TransformConversation(nil)
	ctx.RequestReplan() // steering lands after transform, before HTTP setup
	_, finish, err := cb.LLMRequestContext(0)
	if err != nil {
		t.Fatalf("LLMRequestContext: %v", err)
	}
	defer finish(nil)
	if !cb.LLMReplanRequested() {
		t.Fatal("replan in transform/request race window was lost")
	}
}

func TestSharedAgentLoopCallbacks_TransformConsumesExistingReplanRevision(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:shared-existing-replan"
	ctx := NewLoopContext("shared-existing-replan", 3, nil)
	h.accumulateInjection(userID, buildGuideLaunchInjection("prefer the existing API"))
	ctx.RequestReplan()
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID, loopCtx: ctx}

	next := cb.TransformConversation([]interface{}{map[string]string{"role": "user", "content": "refactor it"}})
	if len(next) != 2 {
		t.Fatalf("conversation len = %d, want injected steer", len(next))
	}
	if cb.LLMReplanRequested() {
		t.Fatal("revision already consumed by transform should not cancel its own replacement request")
	}
}

func TestSharedAgentLoopCallbacks_ForwardsNewLLMRound(t *testing.T) {
	var calls int
	cb := &sharedAgentLoopCallbacks{onNewRound: func() { calls++ }}
	cb.OnLLMNewRound()
	if calls != 1 {
		t.Fatalf("new-round callback calls = %d, want 1", calls)
	}
}

func TestSharedAgentLoopCallbacks_FinalizationRejectsLateAcceptedReplan(t *testing.T) {
	ctx := NewLoopContext("shared-finalize-race", 3, nil)
	cb := &sharedAgentLoopCallbacks{loopCtx: ctx}
	cb.llmReplanRevision.Store(ctx.ReplanRevision())
	ctx.RequestReplan()
	if cb.TryFinalizeLLMResponse() {
		t.Fatal("final response committed despite a newer accepted steer")
	}
	cb.llmReplanRevision.Store(ctx.ReplanRevision())
	if !cb.TryFinalizeLLMResponse() {
		t.Fatal("final response should commit after the steer revision is consumed")
	}
	if ctx.AcceptingReplans() {
		t.Fatal("committed final response must close steer acceptance")
	}
}

func TestSharedAgentLoopCallbacks_LiveSteerCancelsContextAwareTool(t *testing.T) {
	registry := NewToolRegistry()
	started := make(chan struct{})
	if err := registry.Register(RegisteredTool{
		Name: "blocking_shared_tool",
		HandlerCtx: func(ctx context.Context, _ map[string]interface{}, _ coretool.ProgressCallback) string {
			close(started)
			<-ctx.Done()
			return "handler observed cancellation"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	ctx := NewLoopContext("shared-tool-replan", 3, nil)
	h := &IMMessageHandler{registry: registry}
	cb := &sharedAgentLoopCallbacks{handler: h, userID: "desktop-user:shared-tool-replan", loopCtx: ctx}
	cb.TransformConversation(nil)
	resultC := make(chan agent.ToolExecutionResult, 1)
	go func() { resultC <- cb.ExecuteToolStructured("blocking_shared_tool", `{}`) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("context-aware tool did not start")
	}

	ctx.RequestReplan()
	select {
	case result := <-resultC:
		if result.Outcome != agent.ToolExecutionOutcomeError {
			t.Fatalf("outcome = %q, want error; result=%q", result.Outcome, result.Result)
		}
		if !strings.Contains(result.Result, "tool execution interrupted") {
			t.Fatalf("missing interruption result: %q", result.Result)
		}
	case <-time.After(time.Second):
		t.Fatal("live steer did not cancel context-aware tool")
	}
	if !cb.LLMReplanRequested() {
		t.Fatal("cancelled tool did not leave shared loop ready to replan")
	}
}

func TestSharedAgentLoopCallbacks_DoesNotStartToolWhenSteerWinsStartRace(t *testing.T) {
	registry := NewToolRegistry()
	var calls int
	if err := registry.Register(RegisteredTool{
		Name: "must_not_start_after_steer",
		Handler: func(_ map[string]interface{}) string {
			calls++
			return "unexpected execution"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	ctx := NewLoopContext("shared-tool-start-race", 3, nil)
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		userID:  "desktop-user:shared-tool-start-race",
		loopCtx: ctx,
	}
	cb.TransformConversation(nil)
	ctx.RequestReplan()
	result := cb.ExecuteToolStructured("must_not_start_after_steer", `{}`)
	if calls != 0 {
		t.Fatalf("stale tool executed %d times", calls)
	}
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "tool execution interrupted") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSharedAgentLoopCallbacks_SteerSuppressesPostToolFileMaterialization(t *testing.T) {
	registry := NewToolRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	if err := registry.Register(RegisteredTool{
		Name: "stale_file_payload",
		Handler: func(_ map[string]interface{}) string {
			close(started)
			<-release
			return `[file_base64|c3RhbGU=|stale.txt|im]`
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	ctx := NewLoopContext("shared-tool-postprocess-race", 3, nil)
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		userID:  "desktop-user:shared-tool-postprocess-race",
		loopCtx: ctx,
	}
	cb.TransformConversation(nil)
	resultC := make(chan agent.ToolExecutionResult, 1)
	go func() { resultC <- cb.ExecuteToolStructured("stale_file_payload", `{}`) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}
	ctx.RequestReplan()
	close(release)
	select {
	case result := <-resultC:
		if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "tool execution interrupted") {
			t.Fatalf("unexpected result: %+v", result)
		}
		if len(cb.deliveredPaths) != 0 || cb.filesForwarded != 0 {
			t.Fatalf("stale file payload was materialized: paths=%v forwarded=%d", cb.deliveredPaths, cb.filesForwarded)
		}
	case <-time.After(time.Second):
		t.Fatal("tool did not return after release")
	}
}

func TestSharedAgentLoopPreToolCheckpointKeepsPersistedHistoryProviderValid(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	callback := &sharedAgentLoopCallbacks{
		handler:  &IMMessageHandler{memory: memory},
		userID:   "desktop-user:pre-tool-checkpoint",
		userText: "inspect the project",
		checkpointHistory: []agent.ConversationEntry{
			{Role: "user", Content: "inspect the project"},
		},
		checkpointRunID: "run-1",
	}
	delta := []agent.ConversationEntry{{
		Role: "assistant", Content: "", ToolCalls: []map[string]string{{"id": "call-1", "name": "write_file"}},
	}}
	if err := callback.OnToolBatchStarting(delta, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "write_file", SideEffectState: "external_uncertain"}); err != nil {
		t.Fatalf("OnToolBatchStarting() error = %v", err)
	}
	history := memory.Load(callback.userID)
	if len(history) != 1 || history[0].Role != "user" {
		t.Fatalf("pre-tool checkpoint persisted unpaired tool declaration: %#v", history)
	}
	if task, _ := memory.ConsumeInFlightTask(callback.userID); task == "" {
		t.Fatal("pre-tool checkpoint did not persist recovery marker")
	}
}

func TestSharedPreToolCheckpointTracksPendingBatchUntilCommit(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	callback := &sharedAgentLoopCallbacks{
		handler:  &IMMessageHandler{memory: memory},
		userID:   "desktop-user:pending-checkpoint",
		userText: "update the project",
		checkpointHistory: []agent.ConversationEntry{
			{Role: "user", Content: "update the project"},
		},
		checkpointRunID: "run-1",
	}
	preTool := []agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-1", "name": "write_file"}},
	}}
	if err := callback.OnToolBatchStarting(preTool, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "write_file", SideEffectState: "external_uncertain"}); err != nil {
		t.Fatalf("OnToolBatchStarting() error = %v", err)
	}
	if !callback.hasPendingToolBatch {
		t.Fatal("pre-tool checkpoint must mark the in-memory delta unsafe to save")
	}
	committed := append(append([]agent.ConversationEntry(nil), preTool...), agent.ConversationEntry{
		Role: "tool", Content: "written", ToolCallID: "call-1", ToolName: "write_file",
	})
	if err := callback.OnToolBatchCommitted(committed, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "write_file", SideEffectState: "local_committed"}); err != nil {
		t.Fatalf("OnToolBatchCommitted() error = %v", err)
	}
	if callback.hasPendingToolBatch {
		t.Fatal("complete durable batch must allow normal terminal history saving")
	}
}

func TestSharedFailedBatchCommitKeepsSemanticDependantHeld(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	const userID = "desktop-user:failed-semantic-batch-commit"
	memory.SetInFlightTaskForRun(userID, "newer active task", "/project", "run-new")
	callback := &sharedAgentLoopCallbacks{
		handler:                    &IMMessageHandler{memory: memory},
		userID:                     userID,
		userText:                   "generate a report",
		checkpointHistory:          []agent.ConversationEntry{{Role: "user", Content: "generate a report"}},
		checkpointRunID:            "run-stale",
		semanticSurface:            &semanticCallSurface{},
		semanticHoldDependantIssue: true,
		semanticNeedDependantIssue: true,
	}
	batch := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []map[string]string{{"id": "call-search", "name": "web_search"}}},
		{Role: "tool", Content: "weather found", ToolCallID: "call-search", ToolName: "web_search"},
	}
	if err := callback.OnToolBatchCommitted(batch, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "web_search", SideEffectState: "external_uncertain"}); err == nil {
		t.Fatal("expected run-scoped checkpoint conflict")
	}
	if !callback.semanticHoldDependantIssue || !callback.semanticNeedDependantIssue {
		t.Fatalf("failed durable commit released dependant issue: hold=%v need=%v", callback.semanticHoldDependantIssue, callback.semanticNeedDependantIssue)
	}
	if !callback.semanticDurabilityBlocked {
		t.Fatal("failed durable commit must close semantic successor publication")
	}
}

func TestSharedFailedPreToolCheckpointBlocksSemanticSuccessor(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	const userID = "desktop-user:failed-semantic-pre-tool-checkpoint"
	memory.SetInFlightTaskForRun(userID, "newer active task", "/project", "run-new")
	callback := &sharedAgentLoopCallbacks{
		handler:                    &IMMessageHandler{memory: memory},
		userID:                     userID,
		userText:                   "generate a report",
		checkpointHistory:          []agent.ConversationEntry{{Role: "user", Content: "generate a report"}},
		checkpointRunID:            "run-stale",
		semanticSurface:            &semanticCallSurface{},
		semanticHoldDependantIssue: true,
		semanticNeedDependantIssue: true,
	}
	preTool := []agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-search", "name": "web_search"}},
	}}
	if err := callback.OnToolBatchStarting(preTool, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "web_search", SideEffectState: "external_uncertain"}); err == nil {
		t.Fatal("expected run-scoped pre-tool checkpoint conflict")
	}
	if callback.hasPendingToolBatch {
		t.Fatal("failed pre-tool checkpoint must not claim a durable pending batch")
	}
	if !callback.semanticDurabilityBlocked {
		t.Fatal("failed pre-tool checkpoint must close semantic successor publication")
	}
	callback.releaseSemanticDependantIssue()
	if !callback.semanticHoldDependantIssue || !callback.semanticNeedDependantIssue {
		t.Fatalf("failed pre-tool checkpoint released dependant: hold=%v need=%v", callback.semanticHoldDependantIssue, callback.semanticNeedDependantIssue)
	}
}

func TestSharedAbandonedBatchKeepsSemanticDependantHeld(t *testing.T) {
	callback := &sharedAgentLoopCallbacks{
		semanticSurface:            &semanticCallSurface{},
		semanticHoldDependantIssue: true,
		semanticNeedDependantIssue: true,
	}
	callback.OnToolBatchAbandoned(agent.ToolBatchMetadata{Sequence: 1, LastToolName: "web_search", SideEffectState: "external_uncertain"})
	if !callback.semanticHoldDependantIssue || !callback.semanticNeedDependantIssue {
		t.Fatalf("abandoned batch released dependant issue: hold=%v need=%v", callback.semanticHoldDependantIssue, callback.semanticNeedDependantIssue)
	}
}

func TestAttachSharedLoopArtifactsKeepsPendingSemanticDependantHeld(t *testing.T) {
	callback := &sharedAgentLoopCallbacks{
		semanticSurface:            &semanticCallSurface{},
		semanticHoldDependantIssue: true,
		semanticNeedDependantIssue: true,
		hasPendingToolBatch:        true,
	}

	attachSharedLoopArtifacts(&IMAgentResponse{Text: "search completed"}, callback)

	if !callback.semanticHoldDependantIssue || !callback.semanticNeedDependantIssue {
		t.Fatalf("terminal artifact projection released uncommitted dependant: hold=%v need=%v", callback.semanticHoldDependantIssue, callback.semanticNeedDependantIssue)
	}
}

func TestSharedDurabilityFailureKeepsSemanticDependantHeld(t *testing.T) {
	callback := &sharedAgentLoopCallbacks{
		semanticSurface:            &semanticCallSurface{},
		semanticHoldDependantIssue: true,
		semanticNeedDependantIssue: true,
		semanticDurabilityBlocked:  true,
	}

	callback.releaseSemanticDependantIssue()

	if !callback.semanticHoldDependantIssue || !callback.semanticNeedDependantIssue {
		t.Fatalf("failed durability boundary released dependant: hold=%v need=%v", callback.semanticHoldDependantIssue, callback.semanticNeedDependantIssue)
	}
}

func TestSharedInteractivePauseCommitReleasesSemanticDependant(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	callback := &sharedAgentLoopCallbacks{
		handler:                    &IMMessageHandler{memory: memory},
		userID:                     "desktop-user:interactive-semantic-batch",
		userText:                   "generate a report",
		checkpointHistory:          []agent.ConversationEntry{{Role: "user", Content: "generate a report"}},
		checkpointRunID:            "run-1",
		semanticSurface:            &semanticCallSurface{},
		semanticHoldDependantIssue: true,
	}
	if err := callback.OnToolBatchStarting([]agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "ask-1", "name": "ask_user"}},
	}}, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "ask_user", SideEffectState: "external_uncertain"}); err != nil {
		t.Fatalf("OnToolBatchStarting() error = %v", err)
	}
	if err := callback.handler.persistSharedInteractivePause(callback.userID, callback.checkpointRunID, []agent.ConversationEntry{
		{Role: "user", Content: "generate a report"},
		{Role: "assistant", ToolCalls: []map[string]string{{"id": "ask-1", "name": "ask_user"}}},
		{Role: "tool", Content: "Asked user", ToolCallID: "ask-1", ToolName: "ask_user", ToolOutcome: "paused"},
	}); err != nil {
		t.Fatalf("persistSharedInteractivePause() error = %v", err)
	}
	callback.checkpointCommitted = false
	callback.hasPendingToolBatch = false
	callback.releaseSemanticDependantIssue()
	if callback.semanticHoldDependantIssue {
		t.Fatal("durably paired interactive pause did not release dependant hold")
	}
}

func TestInterruptedSharedLoopExitResponseDoesNotWritePartialHistory(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	h := &IMMessageHandler{memory: memory}
	const userID, runID = "desktop-user:partial-shared-exit", "run-1"
	durable := []agent.ConversationEntry{{Role: "user", Content: "update the project"}}
	if err := memory.PersistInFlightCheckpoint(userID, durable, "update the project", "/project", runID, agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "write_file", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatalf("PersistInFlightCheckpoint() error = %v", err)
	}
	partial := append(append([]agent.ConversationEntry(nil), durable...), agent.ConversationEntry{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-1", "name": "write_file"}},
	})
	resp := h.interruptedSharedLoopExitResponse("update the project")
	if resp == nil || !strings.Contains(resp.Text, "interrupted") {
		t.Fatalf("response = %#v", resp)
	}
	if got := memory.Load(userID); len(got) != 1 || got[0].Role != "user" || got[0].Content != "update the project" {
		t.Fatalf("partial history overwrote durable provider-valid prefix: %#v", got)
	}
	if len(partial) != 2 { // Keep the test's partial shape explicit and intentional.
		t.Fatalf("partial history setup changed: %#v", partial)
	}
	if task, _ := memory.ConsumeInFlightTask(userID); task == "" {
		t.Fatal("interrupted exit must preserve the recovery marker")
	}
}

func TestShouldSaveSharedLoopTerminalHistoryRejectsPendingToolBatch(t *testing.T) {
	result := agent.LoopResult{HistoryDelta: []agent.ConversationEntry{
		{Role: "user", Content: "update"},
		{Role: "assistant", ToolCalls: []map[string]string{{"id": "call-1", "name": "write_file"}}},
	}}
	if shouldSaveSharedLoopTerminalHistory(result, &sharedAgentLoopCallbacks{hasPendingToolBatch: true}) {
		t.Fatal("partial tool batch must not be written by the generic terminal save")
	}
	if !shouldSaveSharedLoopTerminalHistory(result, &sharedAgentLoopCallbacks{}) {
		t.Fatal("without a pending batch the complete terminal history remains saveable")
	}
	result.Error = "recovery_checkpoint_failed"
	if shouldSaveSharedLoopTerminalHistory(result, &sharedAgentLoopCallbacks{}) {
		t.Fatal("checkpoint failure must not fall back to an asynchronous history save")
	}
}

func TestInterruptedSharedLoopResultResponseKeepsRecoverySemantics(t *testing.T) {
	h := &IMMessageHandler{}
	callback := &sharedAgentLoopCallbacks{userID: "desktop-user:result-interrupt"}
	resp := h.interruptedSharedLoopResultResponse(
		"update the project", agent.LoopResult{Usage: agent.TurnUsage{InputTokens: 4, OutputTokens: 6}},
		"request-1", nil, nil, callback,
	)
	if resp == nil || !resp.HardExit || resp.Error != "" || resp.RequestID != "request-1" || resp.SessionKey != callback.userID {
		t.Fatalf("response = %#v", resp)
	}
	if !strings.Contains(resp.Text, "interrupted") || resp.InputTokens != 4 || resp.OutputTokens != 6 {
		t.Fatalf("response did not preserve recovery messaging/usage: %#v", resp)
	}
}

func TestSameConversationElementsDetectsSameLengthReplacement(t *testing.T) {
	first := map[string]interface{}{"role": "system", "content": "sys"}
	oldUser := map[string]string{"role": "user", "content": "old"}
	conversation := []interface{}{first, oldUser}
	alias := conversation[:]
	replacement := []interface{}{first, map[string]string{"role": "user", "content": "new"}}

	if !sameConversationElements(alias, conversation) {
		t.Fatal("slice aliases should compare as the same conversation")
	}
	if sameConversationElements(replacement, conversation) {
		t.Fatal("same-length replacement must be detected")
	}
}
