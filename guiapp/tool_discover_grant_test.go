package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func leftoverSSHCatalogHandler(t *testing.T) *IMMessageHandler {
	t.Helper()
	defs := []map[string]interface{}{
		toolDef("bash", "run local shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("discover_tool", "discover tools", nil, nil),
		toolDef("ssh", "SSH remote server access", nil, nil),
	}
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "ssh", Description: "SSH remote server access", Category: ToolCategoryBuiltin, Status: RegToolAvailable}); err != nil {
		t.Fatalf("Register ssh: %v", err)
	}
	if err := registry.Register(RegisteredTool{Name: "bash", Description: "run local shell", Category: ToolCategoryBuiltin, Status: RegToolAvailable}); err != nil {
		t.Fatalf("Register bash: %v", err)
	}
	if err := registry.Register(RegisteredTool{
		Name:        "download_file",
		Description: "download a remote PDF over HTTP/HTTPS into the workspace. Use for PDF files, PDF reports, PDF books.",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
	}); err != nil {
		t.Fatalf("Register download_file: %v", err)
	}
	if err := registry.Register(RegisteredTool{Name: "discover_tool", Description: "discover tools", Category: ToolCategoryBuiltin, Status: RegToolAvailable}); err != nil {
		t.Fatalf("Register discover_tool: %v", err)
	}
	gen := NewToolDefinitionGenerator(nil, defs)
	return &IMMessageHandler{
		registry:   registry,
		toolDefGen: gen,
		toolRouter: NewToolRouter(gen),
	}
}

func leftoverRoutingContext() *LoopContext {
	return &LoopContext{
		Runtime: RuntimeContext{
			RoutingMissFallback: true,
			SemanticIntent: &intent.ClassificationResult{
				Primary:    intent.LabelUnknown,
				Confidence: 0.30,
				Degraded:   true,
				Reason:     "routing miss fallback",
			},
		},
	}
}

func TestLeftoverClassifiedSSHExposesBuiltinSSH(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	ctx := leftoverRoutingContext()
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{
		Primary:    intent.LabelSSH,
		Confidence: 0.95,
		ToolNames:  []string{"ssh"},
	}
	set := h.prepareAgentLoopTools("desktop-user", "upgrade the remote service and keep the running image", ctx, agentLoopPhase{})
	if !toolListContainsName(set.Tools, "ssh") {
		t.Fatalf("leftover LabelSSH classification must expose builtin ssh, got %v", agentLoopToolNamesForLog(set.Tools))
	}
}

func TestUnboundSSHRoutingMissKeepsClassificationAndExposesBuiltinSSH(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{
				Primary:    intent.LabelSSH,
				Confidence: 0.95,
				ToolNames:  []string{"ssh"},
			},
		},
	}
	applySemanticChatProjection(ctx)
	applySemanticRoutingMissFallback(ctx)
	if ctx.Runtime.SemanticIntent == nil || ctx.Runtime.SemanticIntent.Primary != intent.LabelSSH {
		t.Fatalf("unbound LabelSSH miss must keep classification, got %#v", ctx.Runtime.SemanticIntent)
	}
	if loopContextHasChatProjection(ctx) {
		t.Fatal("unbound LabelSSH leftover must not chat-project")
	}
	if !ctx.Runtime.RoutingMissFallback {
		t.Fatal("unbound LabelSSH miss must mark leftover")
	}
	if loopContextBlocksLegacyToolRouter(ctx) {
		t.Fatal("leftover LabelSSH must not skip the name-router")
	}
	set := h.prepareAgentLoopTools("desktop-user", "upgrade the remote service and keep the running image", ctx, agentLoopPhase{})
	if !toolListContainsName(set.Tools, "ssh") {
		t.Fatalf("unbound LabelSSH leftover must expose builtin ssh, got %v", agentLoopToolNamesForLog(set.Tools))
	}
}

func TestUnboundMixedSSHRoutingMissExposesBuiltinSSH(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{
				Primary:    intent.LabelSearch,
				Secondary:  []intent.IntentLabel{intent.LabelSSH},
				Confidence: 0.95,
			},
		},
	}
	applySemanticChatProjection(ctx)
	applySemanticRoutingMissFallback(ctx)
	if ctx.Runtime.SemanticIntent == nil || !classificationHasLabel(*ctx.Runtime.SemanticIntent, intent.LabelSSH) {
		t.Fatalf("mixed unbound SSH miss must keep LabelSSH, got %#v", ctx.Runtime.SemanticIntent)
	}
	if loopContextHasChatProjection(ctx) {
		t.Fatal("mixed unbound SSH leftover must not chat-project")
	}
	if loopContextBlocksLegacyToolRouter(ctx) {
		t.Fatal("mixed leftover SSH must not skip the name-router")
	}
	set := h.prepareAgentLoopTools("desktop-user", "search and restart the server", ctx, agentLoopPhase{})
	if !toolListContainsName(set.Tools, "ssh") {
		t.Fatalf("mixed unbound SSH leftover must expose builtin ssh, got %v", agentLoopToolNamesForLog(set.Tools))
	}
}

func TestUnboundSSHRoutingMissWithoutToolNamesUsesAffinity(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{
				Primary:    intent.LabelSSH,
				Confidence: 0.95,
			},
		},
	}
	applySemanticChatProjection(ctx)
	applySemanticRoutingMissFallback(ctx)
	set := h.prepareAgentLoopTools("desktop-user", "upgrade the remote service and keep the running image", ctx, agentLoopPhase{})
	if !toolListContainsName(set.Tools, "ssh") {
		t.Fatalf("leftover LabelSSH without ToolNames must still expose builtin ssh via affinity, got %v", agentLoopToolNamesForLog(set.Tools))
	}
}

func TestLeftoverGenericChatDoesNotExposeSSH(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	ctx := leftoverRoutingContext()
	set := h.prepareAgentLoopTools("desktop-user", "今天天气怎么样", ctx, agentLoopPhase{})
	if toolListContainsName(set.Tools, "ssh") {
		t.Fatalf("generic leftover chat must not expose ssh, got %v", agentLoopToolNamesForLog(set.Tools))
	}
}

func TestSkillPreferenceLeftoverDoesNotApplyDiscoveryGrant(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	owner := "desktop-user"
	loop := leftoverRoutingContext()
	h.setSessionLoopCtx(owner, loop)
	out := h.toolDiscoverToolForOwner(owner, map[string]interface{}{"need": "ssh remote server"})
	if !strings.Contains(out, "(granted for the next request)") {
		t.Fatalf("discover should grant ssh on this loop, got %q", out)
	}
	set := h.prepareAgentLoopTools(owner, "continue", loop, agentLoopPhase{ForceSkillPreference: true})
	if toolListContainsName(set.Tools, "ssh") {
		t.Fatalf("skill-preference leftover must not apply discovery grants, got %v", agentLoopToolNamesForLog(set.Tools))
	}
}

func TestDiscoverToolGrantsFailClosedSSHOnCurrentLoop(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	owner := "desktop-user:D:/tasks/one"
	loop := leftoverRoutingContext()
	h.setSessionLoopCtx(owner, loop)

	out := h.toolDiscoverToolForOwner(owner, map[string]interface{}{"need": "ssh remote server"})
	if !strings.Contains(out, "ssh") {
		t.Fatalf("discover output should mention ssh, got %q", out)
	}
	if !strings.Contains(out, "(granted for the next request)") {
		t.Fatalf("discover output should grant ssh for this loop, got %q", out)
	}
	if h.toolRouter.IsSessionPinnedForSession(owner, "ssh") {
		t.Fatal("discover_tool must not pin ssh on the shared router")
	}
	if got := loop.discoveredConditionalToolNames(); len(got) != 1 || got[0] != "ssh" {
		t.Fatalf("loop grant names = %v, want [ssh]", got)
	}

	set := h.prepareAgentLoopTools(owner, "continue the remote upgrade", loop, agentLoopPhase{})
	if !toolListContainsName(set.Tools, "ssh") {
		t.Fatalf("next leftover iteration must include discovered ssh, got %v", agentLoopToolNamesForLog(set.Tools))
	}

	other := leftoverRoutingContext()
	h.setSessionLoopCtx("desktop-user:D:/tasks/two", other)
	otherSet := h.prepareAgentLoopTools("desktop-user:D:/tasks/two", "continue", other, agentLoopPhase{})
	if toolListContainsName(otherSet.Tools, "ssh") {
		t.Fatal("ssh discovery grant must not leak into another session")
	}
}

func TestDiscoverToolWithoutLoopStillDoesNotPinSSH(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	out := h.toolDiscoverTool(map[string]interface{}{"need": "ssh remote server"})
	if !strings.Contains(out, "ssh") {
		t.Fatalf("discover output should mention ssh, got %q", out)
	}
	if strings.Contains(out, "(granted for the next request)") || strings.Contains(out, "(activated)") {
		t.Fatalf("without a live loop, ssh must stay matched not granted, got %q", out)
	}
	if h.toolRouter.IsSessionPinned("ssh") {
		t.Fatal("discover_tool must not turn ssh into a sticky tool authorization")
	}
}

func TestDiscoveryNeedMentionsTool(t *testing.T) {
	if !discoveryNeedMentionsTool("ssh remote server", "ssh") {
		t.Fatal("need must mention ssh")
	}
	if !discoveryNeedMentionsTool("需要ssh工具", "ssh") {
		t.Fatal("cjk-wrapped ssh must count as a mention")
	}
	if discoveryNeedMentionsTool("observe desktop gui", "ssh") {
		t.Fatal("unrelated need must not mention ssh")
	}
	if discoveryNeedMentionsTool("sshd config on the host", "ssh") {
		t.Fatal("sshd must not count as mentioning ssh")
	}
	if discoveryNeedMentionsTool("bash Copy-Item PDF", "copy") {
		t.Fatal("Copy-Item must not count as mentioning a tool named copy")
	}
	if discoveryNeedMentionsTool("bash Copy-Item PDF", "item") {
		t.Fatal("Copy-Item must not count as mentioning a tool named item")
	}
}

func TestDiscoverToolDoesNotGrantUnmentionedSSH(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	owner := "desktop-user:D:/tasks/one"
	loop := leftoverRoutingContext()
	h.setSessionLoopCtx(owner, loop)

	out := h.toolDiscoverToolForOwner(owner, map[string]interface{}{"need": "observe desktop gui"})
	if strings.Contains(out, "**ssh**") && (strings.Contains(out, "(granted for the next request)") || strings.Contains(out, "(activated)")) {
		t.Fatalf("observe-desktop discovery must not grant ssh, got %q", out)
	}
	for _, name := range loop.discoveredConditionalToolNames() {
		if name == "ssh" {
			t.Fatal("unmentioned ssh must not become a loop grant")
		}
	}
}

func TestDiscoverToolPinsMentionedHostToolAgainstPDFRetrieval(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	owner := "desktop-user:D:/tasks/copy"
	loop := leftoverRoutingContext()
	h.setSessionLoopCtx(owner, loop)
	loop.setExposedToolNames([]string{"read_file", "discover_tool"})

	out := h.toolDiscoverToolForOwner(owner, map[string]interface{}{
		"need": "bash Copy-Item PDF 到当前工作区",
	})
	if !strings.Contains(out, "**bash**") {
		t.Fatalf("named catalog tool must appear even when PDF tokens rank download_file, got %q", out)
	}
	if !strings.Contains(out, "(granted for the next request)") {
		t.Fatalf("named bash missing from the current surface must be granted, got %q", out)
	}
	granted := loop.discoveredConditionalToolNames()
	found := false
	for _, name := range granted {
		if name == "bash" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("bash grant names = %v, want bash", granted)
	}

	set := h.prepareAgentLoopTools(owner, "continue copying the file", loop, agentLoopPhase{})
	if !toolListContainsName(set.Tools, "bash") {
		t.Fatalf("next request must include granted bash, got %v", agentLoopToolNamesForLog(set.Tools))
	}
}

func TestDiscoverToolReportsCurrentSurfaceInsteadOfGlobalCore(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	owner := "desktop-user:D:/tasks/copy"
	loop := leftoverRoutingContext()
	h.setSessionLoopCtx(owner, loop)
	loop.setExposedToolNames([]string{"bash", "read_file", "discover_tool"})

	out := h.toolDiscoverToolForOwner(owner, map[string]interface{}{
		"need": "bash Copy-Item",
	})
	if !strings.Contains(out, "already in this turn's tools") {
		t.Fatalf("bash already on the surface must be reported as callable now, got %q", out)
	}
}

func TestFinalizeDiscoveryRankingKeepsMentionedNamesBeyondCap(t *testing.T) {
	ranked := []discoveredToolScore{
		{name: "download_file", score: 9},
		{name: "web_fetch", score: 8},
		{name: "generate_pdf", score: 7},
		{name: "ocr_recognize", score: 6},
		{name: "knowledge_search", score: 5},
		{name: "bash", score: 1e9},
		{name: "send_file", score: 1e9},
	}
	got := finalizeDiscoveryRanking(map[string]bool{"bash": true, "send_file": true}, ranked)
	names := make(map[string]bool, len(got))
	for _, item := range got {
		names[item.name] = true
	}
	if !names["bash"] || !names["send_file"] {
		t.Fatalf("mentioned catalog names must survive the retrieval cap, got %v", got)
	}
}

func TestDiscoverToolBudgetStopsNoProgressSpiral(t *testing.T) {
	h := leftoverSSHCatalogHandler(t)
	owner := "desktop-user:D:/tasks/copy"
	loop := leftoverRoutingContext()
	h.setSessionLoopCtx(owner, loop)
	loop.setExposedToolNames([]string{"bash", "read_file", "discover_tool"})

	var last string
	for i := 0; i < legacyDiscoverToolMaxPerTurn+2; i++ {
		last = h.toolDiscoverToolForOwner(owner, map[string]interface{}{"need": "bash Copy-Item"})
	}
	if !strings.Contains(last, "reached its limit") {
		t.Fatalf("repeated discovery of an already-listed name must exhaust, got %q", last)
	}
}
