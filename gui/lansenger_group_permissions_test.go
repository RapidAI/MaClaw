package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestLansengerGroupShortcutCommandsAreBlocked(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &policy
	h := &IMMessageHandler{}

	for _, command := range []string{"/reset", "/goal test", "/skill list", "/btw summarize"} {
		resp, handled := h.handleImmediateIMCommandWithLoop(IMUserMessage{UserID: "group-user", Platform: "lansenger_local", Text: command}, command, ctx, nil, nil)
		if !handled || resp == nil || resp.Text != localizedLansengerGroupCommandRestrictedMessage("") {
			t.Fatalf("command %q = %#v handled=%v, want group rejection", command, resp, handled)
		}
	}

	if resp, handled := h.handleImmediateIMCommandWithLoop(IMUserMessage{UserID: "private-user", Platform: "lansenger_local", Text: "/help"}, "/help", nil, nil, nil); !handled || resp == nil {
		t.Fatal("private shortcut command should retain existing behavior")
	}
}

func TestLansengerGroupMoACommandIsBlockedBeforeSessionMutation(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &policy
	h := &IMMessageHandler{}

	resp := h.handleIMMessageWithLoop(IMUserMessage{
		UserID:   "group-user",
		Platform: "lansenger_local",
		Text:     "/moa summarize this",
	}, ctx, nil, nil, nil, nil)
	if resp == nil || resp.Text != localizedLansengerGroupCommandRestrictedMessage("") {
		t.Fatalf("response = %#v, want group shortcut rejection", resp)
	}
	if h.moaSessions != nil {
		t.Fatal("group /moa must not allocate or mutate the MoA session store")
	}
}

func TestLansengerGroupPermissionPolicyRestrictsKnowledgeSources(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}

	args := map[string]interface{}{"query": "status"}
	if err := policy.restrictKnowledgeArgs(args); err != nil {
		t.Fatalf("restrict unspecified sources: %v", err)
	}
	if got := knowledgeIDsForLansengerPermissionTest(args["source_ids"]); len(got) != 1 || got[0] != "approved" {
		t.Fatalf("source_ids = %#v, want [approved]", got)
	}

	if err := policy.restrictKnowledgeArgs(map[string]interface{}{"source_ids": []interface{}{"other"}}); err == nil {
		t.Fatal("unapproved source should be rejected")
	}
	if err := policy.restrictKnowledgeArgs(map[string]interface{}{"source_ids": []interface{}{}}); err == nil {
		t.Fatal("explicit empty source list should be rejected")
	}
	if err := (lansengerGroupPermissionPolicy{}).restrictKnowledgeArgs(map[string]interface{}{"query": "status"}); err == nil {
		t.Fatal("empty allowlist should deny knowledge access")
	}
}

func TestLansengerGroupPermissionPolicyRestrictsFiles(t *testing.T) {
	allowed := t.TempDir()
	inside := filepath.Join(allowed, "visible.txt")
	if err := os.WriteFile(inside, []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	policy := lansengerGroupPermissionPolicy{AllowedDirectories: []string{allowed}}
	if err := policy.validateFileToolArgs("read_file", map[string]interface{}{"path": inside}); err != nil {
		t.Fatalf("allowed file rejected: %v", err)
	}
	if err := policy.validateFileToolArgs("read_file", map[string]interface{}{"path": outside}); err == nil {
		t.Fatal("outside file should be rejected")
	}
	if err := policy.validateFileToolArgs("list_directory", map[string]interface{}{"path": filepath.Join(allowed, "..", filepath.Base(allowed))}); err != nil {
		t.Fatalf("cleaned allowed directory rejected: %v", err)
	}
	if err := policy.validateFileToolArgs("search_files", map[string]interface{}{"project_path": outsideDir}); err == nil {
		t.Fatal("search_files outside project_path should be rejected")
	}

	policy.AllowAllDirectories = true
	if err := policy.validateFileToolArgs("read_file", map[string]interface{}{"path": outside}); err != nil {
		t.Fatalf("allow-all should permit file access: %v", err)
	}
}

func TestLansengerGroupPermissionPolicyValidatesResolvedFileToolPath(t *testing.T) {
	allowed := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	args := map[string]interface{}{"path": "relative.txt"}
	policy := lansengerGroupPermissionPolicy{AllowedDirectories: []string{allowed}}
	err := policy.resolveAndValidateFileToolArgs("read_file", args, func(string) (string, error) {
		return outside, nil
	})
	if err == nil {
		t.Fatal("path must be checked after resolving against the tool's execution base")
	}

	inside := filepath.Join(allowed, "visible.txt")
	if err := os.WriteFile(inside, []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	args = map[string]interface{}{"project_path": "relative-project"}
	if err := policy.resolveAndValidateFileToolArgs("search_files", args, func(string) (string, error) {
		return allowed, nil
	}); err != nil {
		t.Fatalf("resolved directory rejected: %v", err)
	}
	if got, _ := args["project_path"].(string); got != allowed {
		t.Fatalf("project_path = %q, want resolved path %q", got, allowed)
	}
}

func TestLansengerGroupPermissionPolicyFiltersToolExposure(t *testing.T) {
	tools := []map[string]interface{}{
		agent.ToolDef("memory", "all memory actions", map[string]interface{}{
			"action":       map[string]interface{}{"type": "string", "description": "save, recall, list"},
			"content":      map[string]interface{}{"type": "string"},
			"query":        map[string]interface{}{"type": "string"},
			"project_path": map[string]interface{}{"type": "string"},
		}, []string{"action"}),
		agent.ToolDef("knowledge_search", "", nil, nil),
		agent.ToolDef("knowledge_delete_source", "", nil, nil),
		agent.ToolDef("read_file", "", nil, nil),
		agent.ToolDef("bash", "", nil, nil),
		agent.ToolDef("git_status", "", nil, nil),
		agent.ToolDef("web_fetch", "", nil, nil),
		agent.ToolDef("current_datetime", "", nil, nil),
	}

	filtered := filterToolsForLansengerGroupPermissions(tools, lansengerGroupPermissionPolicy{})
	if containsLansengerPermissionTestTool(filtered, "knowledge_search") || containsLansengerPermissionTestTool(filtered, "read_file") || containsLansengerPermissionTestTool(filtered, "bash") || containsLansengerPermissionTestTool(filtered, "git_status") {
		t.Fatalf("ungranted tools remain exposed: %#v", filtered)
	}
	if !containsLansengerPermissionTestTool(filtered, "current_datetime") || !containsLansengerPermissionTestTool(filtered, "web_fetch") {
		t.Fatal("explicitly safe tools should remain exposed")
	}
	assertLansengerGroupMemoryRecallSchema(t, filtered)

	filtered = filterToolsForLansengerGroupPermissions(tools, lansengerGroupPermissionPolicy{
		KnowledgeSourceIDs:  []string{"approved"},
		AllowedDirectories:  []string{t.TempDir()},
		AllowAllDirectories: false,
	})
	if !containsLansengerPermissionTestTool(filtered, "knowledge_search") || !containsLansengerPermissionTestTool(filtered, "read_file") {
		t.Fatalf("granted retrieval tools missing: %#v", filtered)
	}
	if containsLansengerPermissionTestTool(filtered, "knowledge_delete_source") || containsLansengerPermissionTestTool(filtered, "bash") || containsLansengerPermissionTestTool(filtered, "git_status") {
		t.Fatalf("unsafe tools remain exposed: %#v", filtered)
	}
}

func assertLansengerGroupMemoryRecallSchema(t *testing.T, defs []map[string]interface{}) {
	t.Helper()
	for _, def := range defs {
		function, _ := def["function"].(map[string]interface{})
		if function["name"] != "memory" {
			continue
		}
		parameters, _ := function["parameters"].(map[string]interface{})
		properties, _ := parameters["properties"].(map[string]interface{})
		if _, ok := properties["content"]; ok {
			t.Fatalf("group memory schema must not expose write-only content: %#v", properties)
		}
		if _, ok := properties["cursor"]; ok {
			t.Fatalf("group memory schema must not expose cursor pagination: %#v", properties)
		}
		if _, ok := properties["session"]; ok {
			t.Fatalf("group memory schema must not expose scroll sessions: %#v", properties)
		}
		if _, ok := properties["mode"]; ok {
			t.Fatalf("group memory schema must not expose alternate recall modes: %#v", properties)
		}
		action, _ := properties["action"].(map[string]interface{})
		enum, _ := action["enum"].([]string)
		if !reflect.DeepEqual(enum, []string{"recall"}) {
			t.Fatalf("group memory action enum = %#v, want [recall]", action["enum"])
		}
		return
	}
	t.Fatal("filtered tool list does not include memory")
}

func TestLansengerGroupPermissionPolicyAllowsMemoryRecallOnly(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{}
	if !policy.allowsTool("memory") {
		t.Fatal("group policy should expose memory so it can be limited to recall at execution time")
	}

	ownerID := "lansenger-group:6:memory:4:user"
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("create memory store: %v", err)
	}
	if err := store.SaveForUser(corememory.Entry{Content: "group deployment runbook"}, ownerID); err != nil {
		t.Fatalf("save group memory: %v", err)
	}
	if err := store.SaveForUser(corememory.Entry{Content: "private deployment secret"}, "private-user"); err != nil {
		t.Fatalf("save private memory: %v", err)
	}
	if err := store.Save(corememory.Entry{Content: "shared desktop deployment secret"}); err != nil {
		t.Fatalf("save shared memory: %v", err)
	}
	h := &IMMessageHandler{memoryStore: store, registry: NewToolRegistry()}
	if err := h.registry.Register(RegisteredTool{Name: "memory", Handler: h.toolMemory}); err != nil {
		t.Fatalf("register memory tool: %v", err)
	}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &policy
	h.setSessionLoopCtx(ownerID, ctx)

	result := h.executeToolDetailedWithRuntimeContext(context.Background(), ownerID, true, "lansenger", "memory", `{"action":"recall","query":"deployment"}`, "", nil)
	if result.FailureKind == toolFailurePolicyRejected || !strings.Contains(result.Text, "group deployment runbook") || strings.Contains(result.Text, "private deployment secret") || strings.Contains(result.Text, "shared desktop deployment secret") {
		t.Fatalf("memory recall must be allowed and owner-scoped: %+v", result)
	}
	for _, action := range []string{"themes", "scenes", "trace", "candidates", "derived", "summary", "save", "delete", "list", ""} {
		result := h.executeToolDetailedWithRuntimeContext(context.Background(), ownerID, false, "", "memory", `{"action":"`+action+`"}`, "", nil)
		if result.FailureKind != toolFailurePolicyRejected {
			t.Fatalf("memory action %q = %+v, want policy rejection", action, result)
		}
	}
	for _, args := range []string{
		`{"action":"recall","query":"deployment","cursor":"opaque"}`,
		`{"action":"recall","query":"deployment","session":true}`,
		`{"action":"recall","query":"deployment","mode":"exhaustive"}`,
	} {
		result := h.executeToolDetailedWithRuntimeContext(context.Background(), ownerID, false, "", "memory", args, "", nil)
		if result.FailureKind != toolFailurePolicyRejected || !strings.Contains(result.Text, "不支持分页、滚动会话或 exhaustive") {
			t.Fatalf("group recall transport %s = %+v, want policy rejection", args, result)
		}
	}
}

func TestLansengerGroupMemoryPromptExcludesSharedDesktopEntries(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("create memory store: %v", err)
	}
	defer store.Stop()

	ownerID := "lansenger-group:9:group-one:6:member"
	if err := store.Save(corememory.Entry{Category: corememory.CategoryUserFact, Content: "shared desktop preference secret"}); err != nil {
		t.Fatalf("save shared user fact: %v", err)
	}
	if err := store.SaveForUser(corememory.Entry{Category: corememory.CategoryUserFact, Content: "group member preference"}, ownerID); err != nil {
		t.Fatalf("save group user fact: %v", err)
	}
	if err := store.Save(corememory.Entry{Category: corememory.CategoryProjectKnowledge, Content: "shared desktop deployment secret"}); err != nil {
		t.Fatalf("save shared recall entry: %v", err)
	}
	if err := store.SaveForUser(corememory.Entry{Category: corememory.CategoryProjectKnowledge, Content: "group deployment runbook"}, ownerID); err != nil {
		t.Fatalf("save group recall entry: %v", err)
	}

	h := &IMMessageHandler{memoryStore: store}
	var prompt strings.Builder
	h.appendMemorySection(&prompt, true, ownerID, lifecycle.EventContext{}, "deployment")
	out := prompt.String()
	if strings.Contains(out, "shared desktop preference secret") || strings.Contains(out, "shared desktop deployment secret") {
		t.Fatalf("group prompt leaked shared desktop memory: %s", out)
	}
	if !strings.Contains(out, "group member preference") || !strings.Contains(out, "group deployment runbook") {
		t.Fatalf("group prompt omitted its owner-scoped memory: %s", out)
	}
}

func TestLansengerGroupPermissionPolicyFailsClosedForUnlistedTools(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{
		KnowledgeSourceIDs: []string{"approved"},
		AllowedDirectories: []string{t.TempDir()},
	}
	for _, name := range []string{"git_status", "check_health", "manage_skill", "call_mcp_tool", "future_local_tool"} {
		if policy.allowsTool(name) {
			t.Fatalf("%q must be denied unless it has an explicit group-permission contract", name)
		}
	}
	for _, name := range []string{"memory", "knowledge_search", "read_file", "web_search", "web_fetch", "current_datetime"} {
		if !policy.allowsTool(name) {
			t.Fatalf("%q should be allowed by its explicit contract", name)
		}
	}
}

func TestLansengerGroupPermissionPolicyBlocksInjectedAndDiscoveredTools(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &policy

	defs := []map[string]interface{}{
		agent.ToolDef("current_datetime", "", nil, nil),
		agent.ToolDef("git_status", "", nil, nil),
		agent.ToolDef("call_mcp_tool", "", nil, nil),
	}
	h := &IMMessageHandler{}
	filtered, _ := h.finalizeInjectionAugmentedTools(ctx, "group-user", defs)
	if containsLansengerPermissionTestTool(filtered, "git_status") || containsLansengerPermissionTestTool(filtered, "call_mcp_tool") {
		t.Fatalf("injected tools bypassed group policy: %#v", filtered)
	}
	if !containsLansengerPermissionTestTool(filtered, "current_datetime") {
		t.Fatalf("safe injected tool was removed: %#v", filtered)
	}

	ranked := []discoveredToolScore{{name: "current_datetime"}, {name: "git_status"}, {name: "remote_search"}}
	discovered := filterDiscoveredToolsForLansengerGroup(ranked, map[string]discoverableMCPTool{"remote_search": {}}, policy)
	if len(discovered) != 1 || discovered[0].name != "current_datetime" {
		t.Fatalf("group discovery = %#v, want only current_datetime", discovered)
	}
}

func TestLansengerGroupKnowledgePriorityBlocksWebBeforeKnowledgeLookup(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("web search must be blocked until authorised knowledge is searched")
	}
	policy.recordKnowledgeSearchResult(toolExecutionResult{
		Text:    `{"ok":true,"count":0,"results":[]}`,
		Outcome: toolOutcomeSucceeded,
	})
	if reason := policy.webFallbackBlockReason(); reason != "" {
		t.Fatalf("web fallback after an empty knowledge search = %q, want allowed", reason)
	}
}

func TestLansengerGroupKnowledgePriorityBlocksWebWhenKnowledgeExists(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	policy.recordKnowledgeSearchResult(toolExecutionResult{
		Text:    `{"ok":true,"count":1,"results":[{}]}`,
		Outcome: toolOutcomeSucceeded,
	})
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("web search must remain blocked when authorised knowledge was found")
	}

	policy = lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	policy.markKnowledgeAutoRecallEvidence()
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("web search must remain blocked when authorised auto-recall supplied evidence")
	}
}

func TestLansengerGroupKnowledgePriorityDoesNotConstrainGroupsWithoutKnowledge(t *testing.T) {
	if reason := (&lansengerGroupPermissionPolicy{}).webFallbackBlockReason(); reason != "" {
		t.Fatalf("group without knowledge permission unexpectedly blocked web fallback: %q", reason)
	}
}

func TestLansengerGroupKnowledgePermissionDropsBlankSourceIDs(t *testing.T) {
	policy := lansengerGroupPermissionsFromConfig(&corelib.AppConfig{
		LansengerGroupKnowledgeSourceIDs: []string{" ", " approved ", "approved", ""},
	})
	if got, want := policy.KnowledgeSourceIDs, []string{"approved"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalised source IDs = %#v, want %#v", got, want)
	}
	if !policy.allowsKnowledge() {
		t.Fatal("non-blank authorised source must enable knowledge access")
	}

	blankOnly := lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"", "  "}}
	if blankOnly.allowsKnowledge() {
		t.Fatal("blank source IDs must not enable knowledge access")
	}
	args := map[string]interface{}{}
	if err := blankOnly.restrictKnowledgeArgs(args); err == nil {
		t.Fatal("blank source IDs must not produce an unscoped knowledge query")
	}
}

func TestLansengerGroupKnowledgeSearchScopesAndUnlocksWebFallback(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "knowledge_search",
		HandlerCtx: func(_ context.Context, args map[string]interface{}, _ coretool.ProgressCallback) string {
			got := knowledgeIDsForLansengerPermissionTest(args["source_ids"])
			if len(got) != 1 || got[0] != "approved" {
				t.Fatalf("knowledge source_ids = %#v, want [approved]", got)
			}
			return `{"ok":true,"count":0,"results":[]}`
		},
	}); err != nil {
		t.Fatalf("register knowledge_search: %v", err)
	}
	if err := registry.Register(RegisteredTool{
		Name:    "web_search",
		Handler: func(map[string]interface{}) string { return "web fallback" },
	}); err != nil {
		t.Fatalf("register web_search: %v", err)
	}

	h := &IMMessageHandler{registry: registry}
	ownerID := "lansenger:group:priority"
	ctx := NewLoopContext("lansenger-group", 2, nil)
	ctx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	h.setSessionLoopCtx(ownerID, ctx)

	blocked := h.executeToolDetailedWithRuntimeContext(context.Background(), ownerID, false, "", "web_search", `{"query":"jump failed"}`, "", nil)
	if blocked.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("web before knowledge = %+v, want policy rejection", blocked)
	}

	searched := h.executeToolDetailedWithRuntimeContext(context.Background(), ownerID, false, "", "knowledge_search", `{"query":"jump failed"}`, "", nil)
	if searched.Outcome != toolOutcomeSucceeded {
		t.Fatalf("knowledge search = %+v, want success", searched)
	}

	fallback := h.executeToolDetailedWithRuntimeContext(context.Background(), ownerID, false, "", "web_search", `{"query":"jump failed"}`, "", nil)
	if fallback.Text != "web fallback" || fallback.Outcome != toolOutcomeSucceeded {
		t.Fatalf("web after empty knowledge search = %+v, want fallback success", fallback)
	}
}

func TestLansengerGroupKnowledgePriorityTreatsMalformedKnowledgeResultAsNoFallback(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	policy.recordKnowledgeSearchResult(toolExecutionResult{Text: "not JSON", Outcome: toolOutcomeSucceeded})
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("malformed knowledge result must not unlock web fallback")
	}

	policy = lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	policy.recordKnowledgeSearchResult(toolExecutionResult{Text: `{"ok":true}`, Outcome: toolOutcomeSucceeded})
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("knowledge result without an explicit count must not unlock web fallback")
	}

	policy = lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	policy.recordKnowledgeSearchResult(toolExecutionResult{Text: `{"ok":false,"count":0}`, Outcome: toolOutcomeSucceeded})
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("knowledge tool error result must not unlock web fallback")
	}

	policy = lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	policy.recordKnowledgeSearchResult(toolExecutionResult{Text: `{"ok":true,"count":0}`, Outcome: toolOutcomeSucceeded})
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("knowledge result without results must not unlock web fallback")
	}

	policy = lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	policy.recordKnowledgeSearchResult(toolExecutionResult{Text: `{"ok":true,"count":0,"results":[{}]}`, Outcome: toolOutcomeSucceeded})
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("non-empty results must not be accepted as an empty knowledge search")
	}

	policy = lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	policy.recordKnowledgeSearchResult(toolExecutionResult{Text: `{"ok":true,"count":1,"results":[]}`, Outcome: toolOutcomeSucceeded})
	if reason := policy.webFallbackBlockReason(); reason == "" {
		t.Fatal("count/results mismatch must not be accepted as knowledge evidence")
	}
}

func TestLansengerGroupKnowledgePermissionDisablesDirectExecution(t *testing.T) {
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	ctx.Runtime.Execution = ExecutionProfile{Layer: "direct"}
	if resp, handled := (&IMMessageHandler{}).tryDirectExecutionProfile(IMUserMessage{}, ctx, nil); resp != nil || handled {
		t.Fatal("group knowledge policy must not take the direct-execution path")
	}
}

func TestLansengerGroupKnowledgePermissionKeepsKnowledgeSearchInToolList(t *testing.T) {
	registry := NewToolRegistry()
	for _, name := range []string{"knowledge_search", "web_fetch"} {
		if err := registry.Register(RegisteredTool{Name: name, Category: ToolCategoryBuiltin, Status: RegToolAvailable, Handler: func(map[string]interface{}) string { return "ok" }}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	h := &IMMessageHandler{registry: registry}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}

	toolSet := h.prepareAgentLoopTools("lansenger:group:tools", "网页跳转失败", ctx, agentLoopPhase{})
	if !containsLansengerPermissionTestTool(toolSet.Tools, "knowledge_search") {
		t.Fatalf("authorised group tool list must include knowledge_search: %#v", toolSet.Tools)
	}
}

func TestNewIMMessageHandlerKeepsAuthorizedKnowledgeSearchForLansengerGroup(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := NewIMMessageHandler(app, nil)
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}

	toolSet := h.prepareAgentLoopTools("lansenger:group:tools", "查询已授权知识库中的故障处理方案", ctx, agentLoopPhase{})
	if !containsLansengerPermissionTestTool(toolSet.Tools, "memory") {
		t.Fatalf("local Lansenger group tool list must retain the read-only memory tool: %#v", toolSet.Tools)
	}
	if !containsLansengerPermissionTestTool(toolSet.Tools, "knowledge_search") {
		t.Fatalf("authorised local Lansenger group tool list must include knowledge_search: %#v", toolSet.Tools)
	}
	if containsLansengerPermissionTestTool(toolSet.Tools, "knowledge_save_text") {
		t.Fatalf("group permission must not expose knowledge write tools: %#v", toolSet.Tools)
	}
}

func TestLansengerGroupRestoresMemoryRecallAfterRoutingDropsIt(t *testing.T) {
	registry := NewToolRegistry()
	for _, name := range []string{"memory", "web_fetch"} {
		if err := registry.Register(RegisteredTool{Name: name, Category: ToolCategoryBuiltin, Status: RegToolAvailable, Handler: func(map[string]interface{}) string { return "ok" }}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	h := &IMMessageHandler{registry: registry}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{}

	// Simulate a general-support route that selected only a network tool.
	tools := h.ensureLansengerGroupMemoryRecallTool("lansenger:group:tools", []map[string]interface{}{registeredToolToDef(RegisteredTool{Name: "web_fetch"})})
	tools = filterToolsForLansengerGroupPermissions(tools, *ctx.LansengerGroupPermissions)
	if !containsLansengerPermissionTestTool(tools, "memory") {
		t.Fatalf("group memory recall must be restored after routing: %#v", tools)
	}
	assertLansengerGroupMemoryRecallSchema(t, tools)
}

func TestLansengerGroupRecoveryReappliesPermissions(t *testing.T) {
	registry := NewToolRegistry()
	for _, name := range []string{"memory", "knowledge_search", "knowledge_save_text", "bash", "web_fetch"} {
		if err := registry.Register(RegisteredTool{Name: name, Category: ToolCategoryBuiltin, Status: RegToolAvailable, Handler: func(map[string]interface{}) string { return "ok" }}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	h := &IMMessageHandler{registry: registry}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	baseTools := []map[string]interface{}{
		registeredToolToDef(RegisteredTool{Name: "knowledge_save_text"}),
		registeredToolToDef(RegisteredTool{Name: "bash"}),
		registeredToolToDef(RegisteredTool{Name: "web_fetch"}),
	}

	restored, _, _ := h.restoreToolsAfterSkillRecover("lansenger:group:recover", ctx, baseTools, agentLoopPhase{})
	if !containsLansengerPermissionTestTool(restored, "memory") || !containsLansengerPermissionTestTool(restored, "knowledge_search") || !containsLansengerPermissionTestTool(restored, "web_fetch") {
		t.Fatalf("group recovery missing safe retrieval tools: %#v", restored)
	}
	if containsLansengerPermissionTestTool(restored, "knowledge_save_text") || containsLansengerPermissionTestTool(restored, "bash") {
		t.Fatalf("group recovery restored unsafe tools: %#v", restored)
	}
	assertLansengerGroupMemoryRecallSchema(t, restored)
}

func TestLansengerGroupFallbackCatalogReappliesPermissions(t *testing.T) {
	registry := NewToolRegistry()
	for _, name := range []string{"memory", "knowledge_search", "bash", "craft_tool", "web_fetch"} {
		if err := registry.Register(RegisteredTool{Name: name, Category: ToolCategoryBuiltin, Status: RegToolAvailable, Handler: func(map[string]interface{}) string { return "ok" }}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	h := &IMMessageHandler{registry: registry}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	baseTools := []map[string]interface{}{
		registeredToolToDef(RegisteredTool{Name: "bash"}),
		registeredToolToDef(RegisteredTool{Name: "craft_tool"}),
		registeredToolToDef(RegisteredTool{Name: "web_fetch"}),
	}

	catalog := h.truncationFallbackToolCatalog(ctx, "lansenger:group:fallback", nil, baseTools)
	if containsLansengerPermissionTestTool(catalog, "bash") || containsLansengerPermissionTestTool(catalog, "craft_tool") {
		t.Fatalf("group fallback catalog retained unsafe tools: %#v", catalog)
	}
	if !containsLansengerPermissionTestTool(catalog, "memory") || !containsLansengerPermissionTestTool(catalog, "knowledge_search") || !containsLansengerPermissionTestTool(catalog, "web_fetch") {
		t.Fatalf("group fallback catalog missing safe retrieval tools: %#v", catalog)
	}
	assertLansengerGroupMemoryRecallSchema(t, catalog)
}

func TestLansengerGroupKnowledgePriorityPolicySurvivesValueCopies(t *testing.T) {
	policy := lansengerGroupPermissionsFromConfig(&corelib.AppConfig{LansengerGroupKnowledgeSourceIDs: []string{"approved"}})
	policyCopy := policy
	policyCopy.recordKnowledgeSearchResult(toolExecutionResult{Text: `{"ok":true,"count":0,"results":[]}`, Outcome: toolOutcomeSucceeded})
	if reason := policy.webFallbackBlockReason(); reason != "" {
		t.Fatalf("copied policy state did not unlock the original fallback: %q", reason)
	}
}

func TestLansengerGroupKnowledgePriorityRequiresLookupOnlyUntilResolved(t *testing.T) {
	policy := lansengerGroupPermissionsFromConfig(&corelib.AppConfig{LansengerGroupKnowledgeSourceIDs: []string{"approved"}})
	if !policy.requiresKnowledgeLookup() {
		t.Fatal("fresh authorised group should require knowledge lookup")
	}
	policy.recordKnowledgeSearchResult(toolExecutionResult{Text: `{"ok":true,"count":0,"results":[]}`, Outcome: toolOutcomeSucceeded})
	if policy.requiresKnowledgeLookup() {
		t.Fatal("empty knowledge result should resolve the lookup requirement")
	}
}

func TestLansengerGroupKnowledgeSearchSurvivesInjectionToolRefresh(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "knowledge_search", Category: ToolCategoryBuiltin, Status: RegToolAvailable}); err != nil {
		t.Fatalf("register knowledge_search: %v", err)
	}
	h := &IMMessageHandler{registry: registry}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}

	tools, _ := h.finalizeInjectionAugmentedTools(ctx, "lansenger:group:refresh", nil)
	if !containsLansengerPermissionTestTool(tools, "knowledge_search") {
		t.Fatalf("injection refresh removed knowledge_search: %#v", tools)
	}
}

func knowledgeIDsForLansengerPermissionTest(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func containsLansengerPermissionTestTool(defs []map[string]interface{}, name string) bool {
	for _, def := range defs {
		if fn, ok := def["function"].(map[string]interface{}); ok && fn["name"] == name {
			return true
		}
	}
	return false
}
