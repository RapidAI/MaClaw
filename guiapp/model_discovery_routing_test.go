package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestModelDiscoveryPlanDoesNotClassify(t *testing.T) {
	calls := 0
	h := &IMMessageHandler{
		registry: NewToolRegistry(),
		unifiedClassifier: intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
			calls++
			return `{"top":[{"skill":"knowledge_write","score":0.99}]}`, nil
		}}),
	}
	registerBuiltinTools(h.registry, h)
	for _, text := range []string{"记到知识库：笔记", "给张三发信", "使用 book-pdf skill 写一份教程"} {
		calls = 0
		profile, semantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{Text: text, UserID: "user-1"}, false, false)
		if calls != 0 {
			t.Fatalf("%q classifier calls=%d", text, calls)
		}
		if semantic == nil || !intent.ClassificationIsModelDiscovery(*semantic) {
			t.Fatalf("%q semantic=%+v", text, semantic)
		}
		if semantic.Primary == intent.LabelShellCommand || semantic.Primary == intent.LabelKnowledgeWrite || semantic.Primary == intent.LabelMailboxSend {
			t.Fatalf("%q primary=%s", text, semantic.Primary)
		}
		if profile.IsLight() {
			t.Fatalf("%q profile=%+v", text, profile)
		}
		_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
			"user-1", text, "desktop", "root-discovery", "turn-"+text, semantic,
		)
		if err != nil || !handled || surface == nil || surface.replan == nil {
			t.Fatalf("%q surface handled=%v err=%v", text, handled, err)
		}
		if !intent.ClassificationIsModelDiscovery(surface.replan.Classification) {
			t.Fatalf("%q plan primary=%s", text, surface.replan.Classification.Primary)
		}
		if surface.replan.Classification.Primary == intent.LabelShellCommand || surface.replan.Classification.Primary == intent.LabelKnowledgeWrite {
			t.Fatalf("%q primary rewritten to %s", text, surface.replan.Classification.Primary)
		}
	}
	calls = 0
	prepared, handled, err := h.semanticPlanForTurn("user-1", "记到知识库：笔记", "desktop", "root-nil", "turn-nil")
	if calls != 0 {
		t.Fatalf("nil plan classifier calls=%d", calls)
	}
	if !handled || err != nil || prepared == nil {
		t.Fatalf("nil plan handled=%v err=%v prepared=%v", handled, err, prepared != nil)
	}
}

func TestModelDiscoveryFirstWaveIsTheThreeTools(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	defs, surface, cb := turnDirectoryPlannedSurface(t, "给张三发一封邮件", discovery)
	if !intent.ClassificationIsModelDiscovery(surface.replan.Classification) {
		t.Fatalf("primary=%s", surface.replan.Classification.Primary)
	}
	visible := modelDiscoveryVisibleTools(cb.handler, defs)
	have := turnDirectoryHave(visible)
	if !have["tools_search"] {
		t.Fatalf("visible=%v", turnDirectoryNames(visible))
	}
	for _, closed := range []string{"bash", "mailbox_send", "ssh", "computer_use", "current_datetime", "write_file"} {
		if have[closed] {
			t.Fatalf("first wave listed %s: %v", closed, turnDirectoryNames(visible))
		}
	}
	for _, name := range []string{"mailbox_send", "ssh", "computer_use"} {
		if !cb.semanticTurnAdmits(name) {
			t.Fatalf("directory did not admit %s", name)
		}
	}
	if cb.semanticTurnAdmits("current_datetime") {
		t.Fatal("clock is not a petition")
	}
	granted, message := cb.PetitionToolCall("mailbox_send")
	if strings.Contains(message, "不在本回合能力目录") {
		t.Fatalf("mailbox petition denied as missing: granted=%v message=%q", granted, message)
	}
	if !granted {
		t.Fatalf("mailbox_send petition granted=%v message=%q", granted, message)
	}
}

func TestModelDiscoveryDatabasePetitionDoesNotMintMIS(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, surface, cb := turnDirectoryPlannedSurface(t, "请继续使用 database 工具，不要改用 ssh。", discovery)
	granted, message := cb.PetitionToolCall("database")
	if !granted {
		t.Fatalf("database petition granted=%v message=%q", granted, message)
	}
	surface = cb.semanticSurface
	if planHasCapabilities(surface.plan, tool.CapabilityBusinessDataMIS) {
		t.Fatalf("database petition minted an MIS obligation: %#v", surface.plan.Selections)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityBusinessDataDatabase) {
		t.Fatalf("database petition lost its own capability: %#v", surface.plan.Selections)
	}
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityBusinessDataRead {
			surface.completed[selection.ID] = true
		}
	}
	if snapshot := cb.hopRunSnapshot(); snapshot.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("successful SQL read left unrelated obligations open: %#v", snapshot)
	}
}

func TestDiscoveryEffectfulPetitionAfterLookupKeepsParentAuthority(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, discovery.Primary)}
	h.semanticTrustedWebSearch = func(_, query string) (string, error) { return "found: " + query, nil }
	h.semanticTrustedWebFetch = func(_, url string) (string, error) { return "page: " + url, nil }
	h.semanticTrustedBrowser = func(_, _, _ string) (string, error) { return "opened", nil }
	registerBuiltinTools(h.registry, h)
	stored := discovery
	loop := &LoopContext{
		Platform:              "desktop",
		OfferChatTaskDispatch: true,
		Runtime: RuntimeContext{
			SemanticIntent: &stored,
			Execution:      fullExecutionProfile("model discovery"),
		},
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(loop, "user-1", "核对 Natural Language Processing Journal 是否列入 CCF", "desktop")
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("surface handled=%v err=%v defs=%d", handled, err, len(defs))
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop", userID: "user-1",
		userText: "核对 Natural Language Processing Journal 是否列入 CCF", loopCtx: loop,
	}
	for _, name := range []string{"web_search", "web_fetch"} {
		granted, message := cb.PetitionToolCall(name)
		if !granted {
			t.Fatalf("%s granted=%v message=%q", name, granted, message)
		}
	}
	// Search and fetch publish five siblings. The fifth success appends a
	// continuation the rule template does not mint. The journal turn had
	// already done that before it asked for the PDF.
	before := len(cb.semanticSurface.plan.Selections)
	for i := 0; i < 5; i++ {
		if got := cb.ExecuteTool("web_search", fmt.Sprintf(`{"query":"nlp %d"}`, i)); !strings.Contains(got, "found:") {
			t.Fatalf("web_search %d: %s", i, got)
		}
		if got := cb.ExecuteTool("web_fetch", fmt.Sprintf(`{"url":"https://example.com/%d"}`, i)); !strings.Contains(got, "page:") {
			t.Fatalf("web_fetch %d: %s", i, got)
		}
	}
	if len(cb.semanticSurface.plan.Selections) <= before {
		t.Fatalf("spent wave did not append a continuation: before=%d after=%d", before, len(cb.semanticSurface.plan.Selections))
	}
	for _, name := range []string{"download_file", "bash", "read_file", "write_file", "browser"} {
		granted, message := cb.PetitionToolCall(name)
		if !granted {
			t.Fatalf("%s granted=%v message=%q", name, granted, message)
		}
		next := cb.BuildToolsForModelRequest("核对", 2)
		have := turnDirectoryHave(next)
		if !have[name] || !have["dispatch_background_task"] || have["current_datetime"] {
			t.Fatalf("%s next request = %v", name, turnDirectoryNames(next))
		}
	}
}

func TestModelDiscoveryRepeatedReadOnlyPetitions(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "查一下", discovery)
	for _, name := range []string{"web_search", "web_fetch", "read_file"} {
		granted, message := cb.PetitionToolCall(name)
		if !granted {
			t.Fatalf("%s granted=%v message=%q", name, granted, message)
		}
	}
}

func TestModelDiscoveryGroupPolicyDeniesEffectfulPetition(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "跑一下", discovery)
	cb.loopCtx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{}
	first, firstMessage := cb.PetitionToolCall("bash")
	second, secondMessage := cb.PetitionToolCall("write_file")
	if first || second {
		t.Fatalf("group policy granted effectful petitions first=%v %q second=%v %q", first, firstMessage, second, secondMessage)
	}
}

func TestModelDiscoverySteerDoesNotClassify(t *testing.T) {
	calls := 0
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "南京天气", discovery)
	cb.handler.unifiedClassifier = intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
		calls++
		return `{"top":[{"skill":"document_generate","score":0.99}]}`, nil
	}})
	cb.userText = "南京天气"
	cb.admitLiveSteerIntoPlan("生成pdf")
	if calls != 0 {
		t.Fatalf("steer classifier calls=%d", calls)
	}
	if !intent.ClassificationIsModelDiscovery(cb.semanticSurface.replan.Classification) {
		t.Fatalf("steer replaced discovery with %s", cb.semanticSurface.replan.Classification.Primary)
	}
}

func TestModelDiscoveryDelegateAdmissionDoesNotClassify(t *testing.T) {
	calls := 0
	h := &IMMessageHandler{unifiedClassifier: intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
		calls++
		return `{"top":[{"skill":"bug_fix","score":0.99}]}`, nil
	}})}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := "fix the bug in " + dir
	ok, reason := h.allowCodingWorkflowDelegate(text, agentLoopToolExecutionOptions{UserText: text}, dir)
	if calls != 0 {
		t.Fatalf("delegate classifier calls=%d", calls)
	}
	if !ok {
		t.Fatalf("admission rejected a coded project: %s", reason)
	}
}

func TestModelDiscoveryPetitionDeliversBodyAndNextFunction(t *testing.T) {
	const (
		alphaLine = "GUIDED-ALPHA-BODY-LINE"
		betaLine  = "GUIDED-BETA-BODY-LINE"
		mcpDesc   = "distinctive-mcp-quill-description"
	)
	alphaDir := writeGuidedSkillDir(t, alphaLine)
	betaDir := writeGuidedSkillDir(t, betaLine)
	chinese := strings.Repeat("测", 40) + "ENDMARKER"
	alpha := corelib.NLSkillEntry{Name: "alpha-guide", Status: "active", Source: "file", Description: "alpha guide", SkillDir: alphaDir}
	beta := corelib.NLSkillEntry{Name: "beta-guide", Status: "active", Source: "file", Description: "beta guide", SkillDir: betaDir}
	runnable := corelib.NLSkillEntry{
		Name: "echo-run", Status: "active", Source: "file", Description: chinese,
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	tools := []MCPToolView{{Name: "post_work_log_add", Description: mcpDesc, InputSchema: discoveredMCPObjectSchema()}}
	h, loop := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	installed := []corelib.NLSkillEntry{alpha, beta, runnable}
	h.app.nlSkillsSnap.Store(&installed)
	h.app.cachedSkillScanner = &CachedSkillScanner{}
	h.app.cachedSkillScanner.cache.Store(&skillCacheEntry{skills: []corelib.NLSkillEntry{}, createdAt: time.Now()})
	registerBuiltinTools(h.registry, h)
	h.syncSkillHubTools()
	calls := 0
	h.unifiedClassifier = intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
		calls++
		return `{"top":[{"skill":"shell_command","score":0.99}]}`, nil
	}})

	discovery := intent.ModelDiscoveryClassification()
	stored := discovery
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", "看一下", "desktop", "root-discovery-gaps", "turn-gaps", &stored,
	)
	if err != nil || !handled || surface == nil || surface.replan == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	loop.Runtime.SemanticIntent = &stored
	loop.OfferChatTaskDispatch = true
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop", userID: "desktop-user",
		userText: "看一下", loopCtx: loop, systemPrompt: "base-prompt",
	}
	first := cb.BuildToolsForModelRequest("看一下", 1)
	assertDiscoveryFirstWave(t, first)
	if !utf8.ValidString(cb.systemPrompt) || strings.Contains(cb.systemPrompt, "ENDMARKER") || !strings.Contains(cb.systemPrompt, "测") {
		t.Fatalf("skill summary was not cut on a rune boundary:\n%s", cb.systemPrompt)
	}
	mcpName := tool.InstalledMCPModelFunctionName("tengyun", "post_work_log_add")
	if !strings.Contains(cb.systemPrompt, mcpDesc) || discoveryDirectoryLine(cb.systemPrompt, mcpName) == "已安装的 MCP 工具。" {
		t.Fatalf("MCP row dropped its description:\n%s", cb.systemPrompt)
	}

	alphaAlias := discoverySkillAlias(t, h.getSkillExecutor(), alpha.Name)
	betaAlias := discoverySkillAlias(t, h.getSkillExecutor(), beta.Name)
	runAlias := discoverySkillAlias(t, h.getSkillExecutor(), runnable.Name)
	granted, alphaResult := cb.PetitionToolCall(alphaAlias)
	if !granted || !strings.Contains(alphaResult, alphaLine) || strings.Contains(alphaResult, "不要改用 bash") {
		t.Fatalf("alpha petition granted=%v result=%q", granted, alphaResult)
	}
	granted, betaResult := cb.PetitionToolCall(betaAlias)
	if !granted || !strings.Contains(betaResult, betaLine) || strings.Contains(betaResult, alphaLine) || strings.Contains(betaResult, "不要改用 bash") {
		t.Fatalf("beta petition granted=%v result=%q", granted, betaResult)
	}
	if !strings.Contains(alphaResult, alphaLine) {
		t.Fatal("the first guided result was replaced by the second petition")
	}

	var injected strings.Builder
	h.appendAdmittedAgentGuidedSkillDocs(&injected, "使用 alpha-guide skill 写一份教程", loop)
	if !strings.Contains(injected.String(), alphaLine) {
		t.Fatalf("named skill was not injected:\n%s", injected.String())
	}
	profile, semantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{Text: "使用 alpha-guide skill 写一份教程", UserID: "desktop-user"}, false, false)
	if semantic == nil || !intent.ClassificationIsModelDiscovery(*semantic) || semantic.Primary == intent.LabelShellCommand || profile.IsLight() {
		t.Fatalf("named skill profile=%+v semantic=%+v", profile, semantic)
	}

	granted, message := cb.PetitionToolCall(mcpName)
	if !granted {
		t.Fatalf("mcp petition granted=%v message=%q", granted, message)
	}
	granted, message = cb.PetitionToolCall(runAlias)
	if !granted {
		t.Fatalf("runnable skill petition granted=%v message=%q", granted, message)
	}
	granted, message = cb.PetitionToolCall("write_file")
	if !granted {
		t.Fatalf("write_file petition granted=%v message=%q", granted, message)
	}
	next := cb.BuildToolsForModelRequest("看一下", 2)
	for _, name := range []string{mcpName, runAlias, "write_file"} {
		if !turnDirectoryHave(next)[name] {
			t.Fatalf("next request missing %s: %v", name, turnDirectoryNames(next))
		}
		if !toolDefinitionHasSchema(next, name) {
			t.Fatalf("next request listed %s without a schema", name)
		}
		if !cb.IsToolAllowed(name) {
			t.Fatalf("next request did not admit %s", name)
		}
	}
	if strings.Count(cb.systemPrompt, semanticTurnDirectoryClose) != 1 {
		t.Fatalf("directory was prepended instead of replaced:\n%s", cb.systemPrompt)
	}
	for _, name := range []string{mcpName, runAlias} {
		rows := discoveryDirectoryRows(cb.systemPrompt, name)
		if len(rows) != 1 || !strings.Contains(rows[0], "已在当前函数列表里") || strings.Contains(rows[0], "可请愿，不在当前函数列表里") {
			t.Fatalf("%s directory rows=%q", name, rows)
		}
		if promptStillPetitions(cb.systemPrompt, name) {
			t.Fatalf("%s still has a petition row:\n%s", name, cb.systemPrompt)
		}
	}
	if !strings.Contains(discoveryDirectoryLine(cb.systemPrompt, mcpName), mcpDesc) {
		t.Fatalf("listed MCP row lost its description:\n%s", cb.systemPrompt)
	}
	if calls != 0 {
		t.Fatalf("classifier calls=%d", calls)
	}

	var managed strings.Builder
	appendManagedSemanticSurfaceRules(&managed)
	if !strings.Contains(managed.String(), "请愿每轮每类限一次") {
		t.Fatal("a classified turn lost the once-per-class petition limit")
	}
	var discoveryRules strings.Builder
	h.appendGUIPostSSHRules(&discoveryRules, false, "", corelib.AppConfig{}, "desktop-user", true, true)
	instructions := ensureTurnGrantPromptFence(discoveryRules.String(), discovery)
	for _, banned := range []string{"一次回复只调用一个工具", "请愿每轮每类限一次", "不要凭记忆调用 manage_skill、call_mcp_tool、discover_tool、craft_tool"} {
		if strings.Contains(instructions, banned) {
			t.Fatalf("discovery instructions still say %q", banned)
		}
	}
	for _, kept := range []string{"需要 bash、write_file、office 或 generate_pdf 时，把那个名字放进 needs", "不要调用 office、generate_pdf、write_file 或 bash 来顶替", "标了等待用户确认的名字不要调用", "encrypted", "craft_tool"} {
		if !strings.Contains(instructions, kept) {
			t.Fatalf("discovery instructions missing %q", kept)
		}
	}
}

func writeGuidedSkillDir(t *testing.T, line string) string {
	t.Helper()
	dir := t.TempDir()
	body := line + "\n```bash\necho one\n```\n```bash\necho two\n```\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func discoverySkillAlias(t *testing.T, exec *SkillExecutor, name string) string {
	t.Helper()
	for _, row := range discoveryInstalledSkills(exec) {
		if row.Entry.Name == name {
			return row.Alias
		}
	}
	t.Fatalf("installed skill %q was not in the discovery catalog", name)
	return ""
}

func assertDiscoveryFirstWave(t *testing.T, defs []map[string]interface{}) {
	t.Helper()
	allowed := map[string]bool{
		"tools_search": true, "search_and_install_skill": true, "craft_tool": true,
		"dispatch_background_task": true, "steer_background_task": true, "stop_all_work": true,
	}
	have := turnDirectoryHave(defs)
	for name := range allowed {
		if !have[name] {
			t.Fatalf("first wave missing %s: %v", name, turnDirectoryNames(defs))
		}
	}
	for name := range have {
		if !allowed[name] {
			t.Fatalf("first wave listed %s: %v", name, turnDirectoryNames(defs))
		}
	}
}

func TestDiscoveryListsSkillSearchWithoutLegacyCache(t *testing.T) {
	h := &IMMessageHandler{app: &App{}, registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	if _, ok := h.registry.Get("search_and_install_skill"); ok {
		t.Fatal("builtin registration must not imply skill search")
	}
	visible := modelDiscoveryVisibleTools(h, nil)
	have := turnDirectoryHave(visible)
	if !have["search_and_install_skill"] || !have["craft_tool"] || !have["tools_search"] {
		t.Fatalf("visible=%v", turnDirectoryNames(visible))
	}
}

func TestDiscoveryExtensionCallIsAllowedWhenListed(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, surface, cb := turnDirectoryPlannedSurface(t, "生成一张图片", discovery)
	if !semanticSurfaceIsModelDiscovery(surface) {
		t.Fatal("picture turn left discovery")
	}
	for _, name := range []string{"craft_tool", "search_and_install_skill"} {
		allowed, reason := cb.IsToolCallAllowed(name, `{"task":"draw a picture","query":"draw a picture"}`)
		if !allowed || reason != "" {
			t.Fatalf("%s allowed=%v reason=%q", name, allowed, reason)
		}
	}
	if allowed, reason := cb.IsToolCallAllowed("bash", `{"command":"echo hi"}`); allowed {
		t.Fatalf("bash ran on the first wave: %s", reason)
	}
}

func TestDiscoveryExtensionExecutesWithoutGrant(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, surface, cb := turnDirectoryPlannedSurface(t, "生成一张图片", discovery)
	if err := cb.handler.registry.Register(RegisteredTool{
		Name: "craft_tool", Status: RegToolAvailable,
		Handler: func(map[string]interface{}) string { return "ran:craft_tool" },
	}); err != nil {
		t.Fatal(err)
	}
	// craft_tool is the registry handler. search_and_install_skill is the host
	// skill-search function, which this fixture cannot finish without an App.
	// Either result is the host path. selection_not_authorized is the grant map.
	want := map[string]string{
		"craft_tool":               "ran:craft_tool",
		"search_and_install_skill": "Skill search failed: app is not initialized.",
	}
	for _, name := range []string{"craft_tool", "search_and_install_skill"} {
		if got := cb.ExecuteTool(name, `{"task":"draw a picture","query":"draw a picture"}`); got != want[name] || strings.Contains(got, "selection_not_authorized") {
			t.Fatalf("%s ExecuteTool = %s", name, got)
		}
		epoch := surface.beginEpoch()
		got := cb.ExecuteToolCallWithContext(name, `{"task":"draw a picture","query":"draw a picture"}`, "call-"+name, agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
		if got.Result != want[name] || strings.Contains(got.Result, "selection_not_authorized") {
			t.Fatalf("%s current epoch = %s", name, got.Result)
		}
		stale := epoch
		surface.invalidateEpoch()
		late := cb.ExecuteToolCallWithContext(name, `{"task":"draw a picture","query":"draw a picture"}`, "late-"+name, agent.ToolCallExecutionContext{SurfaceEpoch: stale})
		if !strings.Contains(late.Result, "stale_surface") {
			t.Fatalf("%s stale epoch = %s", name, late.Result)
		}
	}
}

// TestDiscoveryExtensionSameResponseSurvivesSiblingSearch is the picture
// turn's first batch: tools_search and search_and_install_skill in one
// response. The search publishes a child surface and retires the epoch.
// The skill search holds no grant, so same-family continuation cannot carry
// it, and it used to return stale_surface. The epoch BeginToolSurfaceEpoch
// recorded still belongs to this response. The next request does not.
func TestDiscoveryExtensionSameResponseSurvivesSiblingSearch(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成老鼠戏猫图", discovery)
	if err := cb.handler.registry.Register(RegisteredTool{
		Name: "craft_tool", Status: RegToolAvailable,
		Handler: func(map[string]interface{}) string { return "ran:craft_tool" },
	}); err != nil {
		t.Fatal(err)
	}
	epoch := cb.BeginToolSurfaceEpoch(0)
	if epoch == "" || epoch != cb.requestSurfaceEpoch {
		t.Fatalf("request epoch = %q recorded %q", epoch, cb.requestSurfaceEpoch)
	}
	searched := cb.ExecuteToolCallWithContext(semanticToolsSearchName, `{"query":"核对期刊","needs":["web_search"]}`, "call-search", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if strings.Contains(searched.Result, "stale_surface") || !strings.Contains(searched.Result, "web_search") {
		t.Fatalf("sibling search = %s", searched.Result)
	}
	if cb.semanticSurface.epochIsCurrent(epoch) {
		t.Fatal("sibling search left the request epoch current")
	}
	want := map[string]string{
		"craft_tool":               "ran:craft_tool",
		"search_and_install_skill": "Skill search failed: app is not initialized.",
	}
	for _, name := range []string{"craft_tool", "search_and_install_skill"} {
		got := cb.ExecuteToolCallWithContext(name, `{"task":"生成老鼠戏猫图","query":"生成老鼠戏猫图"}`, "call-"+name, agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
		if got.Result != want[name] || strings.Contains(got.Result, "stale_surface") || strings.Contains(got.Result, "selection_not_authorized") {
			t.Fatalf("%s same response = %s", name, got.Result)
		}
	}
	next := cb.BeginToolSurfaceEpoch(1)
	if next == "" || next == epoch {
		t.Fatalf("successor epoch = %q previous %q", next, epoch)
	}
	late := cb.ExecuteToolCallWithContext("craft_tool", `{"task":"生成老鼠戏猫图"}`, "late-craft", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(late.Result, "stale_surface") {
		t.Fatalf("successor craft = %s", late.Result)
	}
	skill := cb.ExecuteToolCallWithContext("search_and_install_skill", `{"query":"生成老鼠戏猫图"}`, "late-skill", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(skill.Result, "stale_surface") {
		t.Fatalf("successor skill = %s", skill.Result)
	}
}

func TestDiscoveryPetitionedSkillDoesNotClosePlannedTools(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	installed := []corelib.NLSkillEntry{paperTranslatorTestSkill()}
	app.nlSkillsSnap.Store(&installed)
	// LoadConfig during the petition publishes the on-disk skill table and
	// drops this in-memory snap. The scanner cache is what loadSkills still
	// returns after that, so the catalog row and the grant name the same skill.
	app.cachedSkillScanner = &CachedSkillScanner{}
	app.cachedSkillScanner.cache.Store(&skillCacheEntry{skills: append([]corelib.NLSkillEntry(nil), installed...), createdAt: time.Now()})
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	alias := discoverySkillAlias(t, app.skillExecutor, "paper-translator")
	discovery := intent.ModelDiscoveryClassification()
	_, surface, cb := turnDirectoryPlannedSurface(t, "生成庆祝北京建城1500年纪念ppt", discovery)
	if !semanticSurfaceIsModelDiscovery(surface) {
		t.Fatal("ppt turn left discovery")
	}
	cb.handler.app = app
	cb.discoveryCatalogLoaded = true
	cb.discoveryCatalog = discoverySkillDirectoryRows(app.skillExecutor)
	raw, err := json.Marshal(map[string]interface{}{
		"query": "生成庆祝北京建城1500年纪念ppt",
		"needs": []string{alias, "web_search", "office"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := cb.runSemanticToolsSearch(string(raw))
	if !strings.Contains(got, semanticToolsSearchListedDirectivePrefix) {
		t.Fatalf("search did not publish the loaded names:\n%s", got)
	}
	for _, name := range []string{alias, "web_search", "office"} {
		if !strings.Contains(got, name) || !strings.Contains(got, name+" —") {
			t.Fatalf("missing %s:\n%s", name, got)
		}
	}
	if _, ok := cb.admittedSkillBindings[alias]; !ok {
		t.Fatalf("petition did not bind %s:\n%s", alias, got)
	}
	if desc := toolDefinitionDescription(cb.tools, alias); strings.Contains(desc, "宿主已选定") || strings.Contains(desc, "不要再搜索或安装") {
		t.Fatalf("petitioned skill described as a host order: %s", desc)
	}
	next := cb.BuildToolsForModelRequest("生成庆祝北京建城1500年纪念ppt", 2)
	if desc := toolDefinitionDescription(next, alias); strings.Contains(desc, "宿主已选定") || strings.Contains(desc, "不要再搜索或安装") || desc == "" {
		t.Fatalf("next request described %s as a host order: %s", alias, desc)
	}
	host := skillAdmissionToolDefinition(admittedSkillBinding{Alias: alias, Name: "paper-translator"})
	if desc := toolDefinitionDescription([]map[string]interface{}{host}, alias); !strings.Contains(desc, "宿主已选定") {
		t.Fatalf("host admission lost its selection: %s", desc)
	}
	for _, name := range []string{"web_search", "office", "craft_tool", "bash", "generate_pdf"} {
		if blocked, reason := cb.skillSubstituteBlocked(name); blocked {
			t.Fatalf("%s closed by the petitioned skill: %s", name, reason)
		}
	}
	if prompt := cb.SkillObligationPrompt(); prompt != "" {
		t.Fatalf("discovery prompt demanded the petitioned skill: %q", prompt)
	}
	for _, name := range []string{"craft_tool", alias, "web_search", "office"} {
		allowed, reason := cb.IsToolCallAllowed(name, `{"query":"北京建城","task":"deck"}`)
		if !allowed || strings.Contains(reason, "本机已有匹配的完整 Skill") {
			t.Fatalf("%s allowed=%v reason=%q", name, allowed, reason)
		}
	}

	plain := &sharedAgentLoopCallbacks{
		semanticSurface:       &semanticCallSurface{},
		admittedSkillBindings: map[string]admittedSkillBinding{alias: {Alias: alias, Name: "paper-translator"}},
	}
	if blocked, reason := plain.skillSubstituteBlocked("web_search"); !blocked || !strings.Contains(reason, alias) {
		t.Fatalf("host admission left web_search open: blocked=%v reason=%q", blocked, reason)
	}
	if blocked, reason := plain.skillSubstituteBlocked("office"); !blocked || !strings.Contains(reason, alias) {
		t.Fatalf("host admission left office open: blocked=%v reason=%q", blocked, reason)
	}
	if blocked, reason := plain.skillSubstituteBlocked("craft_tool"); !blocked || !strings.Contains(reason, alias) {
		t.Fatalf("host admission left craft_tool open: blocked=%v reason=%q", blocked, reason)
	}
	if !strings.Contains(plain.SkillObligationPrompt(), alias) {
		t.Fatalf("host prompt=%q", plain.SkillObligationPrompt())
	}
}

func TestDiscoveryToolsSearchMissContinuesToSkillSearch(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成一张图片", discovery)
	got := semanticToolsSearchRun(cb, `{"query":"生成图片","needs":["image.generate"]}`)
	for _, want := range []string{"needs filter matched nothing", "search_and_install_skill", "craft_tool"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{"做不到", "不要改用已经列出的其它工具", "每轮每类限一次", "直接调用那个名字", "[可请愿：直接调用一次]"} {
		if strings.Contains(got, banned) {
			t.Fatalf("discovery miss still says %q:\n%s", banned, got)
		}
	}
	defs := cb.BuildToolsForModelRequest("生成一张图片", 2)
	have := turnDirectoryHave(defs)
	if have["web_search"] || have["web_fetch"] || have["current_datetime"] {
		t.Fatalf("a miss loaded tools: %v", turnDirectoryNames(defs))
	}
}

// TestDiscoveryCatalogMissRejectsSubstitution is the picture turn: the
// directory has no image tool, so search, download, a document renderer, and
// a background task must not become the result. The extension path has to
// run before any other name opens, and a failed skill search does not count.
func TestDiscoveryCatalogMissRejectsSubstitution(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成一张图片", discovery)
	got := cb.runSemanticToolsSearch(`{"query":"生成一张图片","needs":["image.generate"]}`)
	if !cb.discoveryUnmatchedObligation || !cb.loopCtx.discoveryCatalogMiss() {
		t.Fatal("catalog miss did not close substitution")
	}
	for _, want := range []string{"needs filter matched nothing", "search_and_install_skill", "craft_tool"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{"做不到", "不要改用已经列出的其它工具", "每轮每类限一次", "直接调用那个名字", "[可请愿：直接调用一次]", "selection_not_authorized"} {
		if strings.Contains(got, banned) {
			t.Fatalf("discovery miss still says %q:\n%s", banned, got)
		}
	}
	defs := cb.BuildToolsForModelRequest("生成一张图片", 2)
	have := turnDirectoryHave(defs)
	if have["web_search"] || have["web_fetch"] || have["current_datetime"] || have["download_file"] {
		t.Fatalf("a miss loaded tools: %v", turnDirectoryNames(defs))
	}

	cb.loopCtx.OfferChatTaskDispatch = true
	publishDesktopChatLoop(cb.handler, cb.loopCtx, "生成一张图片")
	allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"find a public picture and download it"}`)
	if allowed || !strings.Contains(reason, "search_and_install_skill") || !strings.Contains(reason, "craft_tool") || strings.Contains(reason, "selection_not_authorized") {
		t.Fatalf("dispatch allowed=%v reason=%q", allowed, reason)
	}
	if reason != discoveryUnmatchedExtensionMessage {
		t.Fatalf("dispatch reason = %q", reason)
	}
	for _, name := range []string{"web_search", "bash", "download_file"} {
		granted, message := cb.PetitionToolCall(name)
		if granted || message != discoveryUnmatchedExtensionMessage {
			t.Fatalf("%s granted=%v message=%q", name, granted, message)
		}
	}

	skill := cb.ExecuteTool("search_and_install_skill", `{"query":"生成一张图片"}`)
	if strings.Contains(skill, "selection_not_authorized") || strings.HasPrefix(strings.TrimSpace(skill), "[system rejected]") {
		t.Fatalf("skill search did not run: %s", skill)
	}
	if skill != "Skill search failed: app is not initialized." && !strings.HasPrefix(skill, "Skill search skipped:") {
		t.Fatalf("skill search = %s", skill)
	}
	if !cb.discoverySkillSearchRan || cb.discoverySkillInstalled || cb.discoveryExtensionSettled() {
		t.Fatalf("failed skill search settled the miss: ran=%v installed=%v settled=%v", cb.discoverySkillSearchRan, cb.discoverySkillInstalled, cb.discoveryExtensionSettled())
	}
	again := cb.runSemanticToolsSearch(`{"query":"生成一张图片","needs":["web_search","download_file"]}`)
	if strings.Contains(again, "selection_not_authorized") {
		t.Fatalf("substitute search = %s", again)
	}
	next := cb.BuildToolsForModelRequest("生成一张图片", 3)
	loaded := turnDirectoryHave(next)
	if loaded["web_search"] || loaded["web_fetch"] || loaded["download_file"] || loaded["current_datetime"] {
		t.Fatalf("substitute needs loaded tools: %v text=%s", turnDirectoryNames(next), again)
	}

	if err := cb.handler.registry.Register(RegisteredTool{
		Name: "craft_tool", Status: RegToolAvailable,
		Handler: func(map[string]interface{}) string { return "ran:craft_tool" },
	}); err != nil {
		t.Fatal(err)
	}
	if crafted := cb.ExecuteTool("craft_tool", `{"task":"生成一张图片","query":"生成一张图片"}`); crafted != "ran:craft_tool" {
		t.Fatalf("craft = %s", crafted)
	}
	if !cb.discoveryCraftRan || !cb.discoveryExtensionSettled() {
		t.Fatal("craft_tool did not settle the miss")
	}
	if granted, message := cb.PetitionToolCall("web_search"); granted || message != discoveryUnmatchedSubstituteMessage {
		t.Fatalf("web_search after craft granted=%v message=%q", granted, message)
	}
	allowed, reason = cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"find a public picture and download it"}`)
	if allowed || reason != discoveryUnmatchedSubstituteMessage || strings.Contains(reason, "selection_not_authorized") {
		t.Fatalf("dispatch after craft allowed=%v reason=%q", allowed, reason)
	}
	dispatched := cb.handler.dispatchBackgroundTaskTool(desktopUserID, map[string]interface{}{"request": "find a public picture and download it"})
	if strings.Contains(dispatched.Text, "Started:") || strings.Contains(dispatched.Text, "selection_not_authorized") || !strings.Contains(dispatched.Text, discoveryUnmatchedSubstituteMessage) {
		t.Fatalf("dispatch tool = %+v", dispatched)
	}
	// The petition is checked before the handoff flag, so the flag stays off
	// until the gate result is visible.
	if granted, message := cb.PetitionToolCall("bash"); strings.Contains(message, discoveryUnmatchedExtensionMessage) || strings.Contains(message, discoveryUnmatchedSubstituteMessage) || strings.Contains(message, "selection_not_authorized") {
		t.Fatalf("bash stayed on the miss gate: granted=%v message=%q", granted, message)
	}
	cb.loopCtx.ForegroundHandoffNeeded = true
	note, ok := cb.handler.continueSpentForegroundChat(cb.loopCtx, desktopUserID, "生成一张图片", agent.LoopResult{
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "craft_tool", ToolOutcome: "succeeded", Content: "ran:craft_tool"},
		},
	})
	if ok || note != "" {
		t.Fatalf("catalog miss handed the turn off: note=%q ok=%v", note, ok)
	}
	if tasks := cb.handler.ensureChatTasks().List(desktopUserID); len(tasks) != 0 {
		t.Fatalf("catalog miss started %d tasks", len(tasks))
	}
}

// TestDiscoveryFailedPetitionIsACatalogMiss is the picture turn's real
// search. browser, bash, and download_file are directory names, so the
// filter matches. The petitions do not grant. That used to leave dispatch
// open, and the worker downloaded the book. A needs list that still loads
// one tool, such as web_search beside a failed download, stays a match.
func TestDiscoveryFailedPetitionIsACatalogMiss(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成一张图片", discovery)
	cb.loopCtx.OfferChatTaskDispatch = true
	cb.semanticPetitionDenied = map[string]struct{}{
		"bash":          {},
		"download_file": {},
	}
	publishDesktopChatLoop(cb.handler, cb.loopCtx, "生成一张图片")
	got := cb.runSemanticToolsSearch(`{"query":"生成一张图片","needs":["bash","download_file"]}`)
	if !cb.discoveryUnmatchedObligation || !cb.loopCtx.discoveryCatalogMiss() {
		t.Fatal("a needs list that loaded nothing did not close substitution")
	}
	for _, want := range []string{"没有加载", "search_and_install_skill", "craft_tool"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{"做不到", "不要改用已经列出的其它工具", "每轮每类限一次", "直接调用那个名字", "selection_not_authorized", "needs filter matched nothing"} {
		if strings.Contains(got, banned) {
			t.Fatalf("unloaded petition still says %q:\n%s", banned, got)
		}
	}
	allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"find a public picture and download it"}`)
	if allowed || reason != discoveryUnmatchedExtensionMessage {
		t.Fatalf("dispatch allowed=%v reason=%q", allowed, reason)
	}
	dispatched := cb.handler.dispatchBackgroundTaskTool(desktopUserID, map[string]interface{}{"request": "find a public picture and download it"})
	if strings.Contains(dispatched.Text, "Started:") || !strings.Contains(dispatched.Text, discoveryUnmatchedExtensionMessage) {
		t.Fatalf("dispatch tool = %+v", dispatched)
	}
	if cb.loopCtx.chatTaskHandoffSent() {
		t.Fatal("failed petition started the handoff")
	}
	if tasks := cb.handler.ensureChatTasks().List(desktopUserID); len(tasks) != 0 {
		t.Fatalf("failed petition started %d tasks", len(tasks))
	}

	_, _, matched := turnDirectoryPlannedSurface(t, "核对期刊", discovery)
	matched.semanticPetitionDenied = map[string]struct{}{"download_file": {}}
	loaded := matched.runSemanticToolsSearch(`{"query":"核对期刊","needs":["web_search","download_file"]}`)
	if matched.discoveryUnmatchedObligation || matched.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("a list that loaded web_search became a miss:\n%s", loaded)
	}
	have := turnDirectoryHave(matched.BuildToolsForModelRequest("核对期刊", 2))
	if !have["web_search"] || have["download_file"] || have["current_datetime"] {
		t.Fatalf("next request = %v\n%s", turnDirectoryNames(matched.BuildToolsForModelRequest("核对期刊", 2)), loaded)
	}
	matched.loopCtx.OfferChatTaskDispatch = true
	if allowed, reason := matched.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"keep reading"}`); !allowed || reason != "" {
		t.Fatalf("matched dispatch allowed=%v reason=%q", allowed, reason)
	}
}

// TestDiscoveryUnrunnableSkillDoesNotSettleTheMiss is an installed package
// the runner must not enter. Success stays false, so the catalog miss stays
// open and web_search is still the substitute the page refused.
func TestDiscoveryUnrunnableSkillDoesNotSettleTheMiss(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成老鼠戏猫图", discovery)
	cb.userText = "生成老鼠戏猫图"
	got := cb.runSemanticToolsSearch(`{"query":"生成老鼠戏猫图","needs":["web_search"]}`)
	if !cb.discoveryUnmatchedObligation || !cb.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("picture search did not miss:\n%s", got)
	}
	note := skillInitialExecutionUnrunnableNote("火山引擎图片生成", "package has no runnable program")
	cb.handler.skillSearchInstallHandler = func(map[string]interface{}, tool.ProgressCallback) searchAndInstallSkillResult {
		return searchAndInstallSkillResult{Text: note, Success: false}
	}
	if skill := cb.ExecuteTool("search_and_install_skill", `{"query":"生成老鼠戏猫图"}`); skill != note {
		t.Fatalf("skill = %s", skill)
	}
	if !cb.discoverySkillSearchRan || cb.discoverySkillInstalled || cb.discoveryExtensionSettled() {
		t.Fatalf("unrunnable install settled the miss: ran=%v installed=%v settled=%v", cb.discoverySkillSearchRan, cb.discoverySkillInstalled, cb.discoveryExtensionSettled())
	}
	if granted, message := cb.PetitionToolCall("web_search"); granted || message != discoveryUnmatchedExtensionMessage {
		t.Fatalf("web_search after unrunnable skill granted=%v message=%q", granted, message)
	}
}

// TestDiscoveryAdoptedInstructionSkillSettlesAndClosesCraftTool is a ClawHub
// package whose program is SKILL.md. The install succeeds, the miss settles,
// and craft_tool stays closed so the model follows the document.
func TestDiscoveryAdoptedInstructionSkillSettlesAndClosesCraftTool(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成老鼠戏猫图，需要生动", discovery)
	cb.userText = "生成老鼠戏猫图，需要生动"
	cb.systemPrompt = "base"
	// This harness has no App, so syncSkillHubTools does not register the
	// discovery preset. A desktop turn does. Register it here so the next
	// request still lists it after the instruction package is adopted.
	if err := cb.handler.registry.Register(RegisteredTool{Name: "search_and_install_skill", Status: RegToolAvailable}); err != nil {
		t.Fatal(err)
	}
	got := cb.runSemanticToolsSearch(`{"query":"生成老鼠戏猫图，需要生动","needs":["web_search"]}`)
	if !cb.discoveryUnmatchedObligation || !cb.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("picture search did not miss:\n%s", got)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Drawing\n\nMOUSE-CAT-PROCEDURE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.handler.skillSearchInstallHandler = func(map[string]interface{}, tool.ProgressCallback) searchAndInstallSkillResult {
		return searchAndInstallSkillResult{
			Text:          skillInitialExecutionInstructionNote("Drawing", dir),
			Success:       true,
			ProcedureDir:  dir,
			ProcedureName: "Drawing",
		}
	}
	skill := cb.ExecuteTool("search_and_install_skill", `{"query":"生成老鼠戏猫图，需要生动"}`)
	if !cb.discoverySkillSearchRan || !cb.discoverySkillInstalled || !cb.discoveryExtensionSettled() {
		t.Fatalf("instruction install did not settle: ran=%v installed=%v settled=%v text=%s", cb.discoverySkillSearchRan, cb.discoverySkillInstalled, cb.discoveryExtensionSettled(), skill)
	}
	if strings.Contains(skill, "craft_tool") || strings.HasPrefix(skill, "failed:") || !strings.Contains(skill, filepath.Clean(dir)) && !strings.Contains(skill, dir) {
		t.Fatalf("tool result = %s", skill)
	}
	if cb.adoptedProcedureDir != filepath.Clean(dir) || cb.adoptedProcedureName != "Drawing" {
		t.Fatalf("adopted dir=%q name=%q", cb.adoptedProcedureDir, cb.adoptedProcedureName)
	}
	if blocked, reason := cb.skillSubstituteBlocked("craft_tool"); !blocked {
		t.Fatalf("craft_tool stayed open: %s", reason)
	}
	if blocked, reason := cb.skillSubstituteBlocked("bash"); blocked {
		t.Fatalf("bash closed: %s", reason)
	}
	if blocked, reason := cb.skillSubstituteBlocked("generate_pdf"); !blocked {
		t.Fatalf("generate_pdf stayed open: %s", reason)
	}
	if !strings.Contains(cb.systemPrompt, "MOUSE-CAT-PROCEDURE") || !strings.Contains(cb.systemPrompt, filepath.Clean(dir)) {
		t.Fatalf("prompt missing the procedure:\n%s", cb.systemPrompt)
	}
	next := cb.BuildToolsForModelRequest(cb.userText, 2)
	have := turnDirectoryHave(next)
	for _, name := range []string{"read_file", "bash", "write_file", "search_and_install_skill", "craft_tool"} {
		if !have[name] {
			t.Fatalf("next request missing %s: %v", name, turnDirectoryNames(next))
		}
	}
	if prompt := cb.BuildSystemPrompt(cb.userText, false); !strings.Contains(prompt, "MOUSE-CAT-PROCEDURE") {
		t.Fatalf("rebuilt prompt dropped the procedure:\n%s", prompt)
	}
	if allowed, reason := cb.IsToolCallAllowed("craft_tool", `{"task":"draw"}`); allowed {
		t.Fatalf("craft_tool allowed after adoption: %s", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed("search_and_install_skill", `{"query":"生成老鼠戏猫图，需要生动"}`); allowed {
		t.Fatalf("search_and_install_skill allowed after adoption: %s", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed("bash", `{"command":"echo hi"}`); !allowed {
		t.Fatalf("bash refused: %s", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed("read_file", `{"path":"SKILL.md"}`); !allowed {
		t.Fatalf("read_file refused: %s", reason)
	}
	for _, name := range []string{"manage_skill", "discover_tool"} {
		if blocked, reason := cb.skillSubstituteBlocked(name); !blocked {
			t.Fatalf("%s stayed open after adoption: %s", name, reason)
		}
	}
}

func TestEnsureAdoptedProcedureDocsRetriesWhenTheDocumentWasEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{systemPrompt: "base", adoptedProcedureDir: dir, adoptedProcedureName: "Drawing"}
	cb.ensureAdoptedProcedureDocs()
	if strings.Contains(cb.systemPrompt, adoptedProcedurePromptMarkPrefix) {
		t.Fatalf("empty document pinned the prompt:\n%s", cb.systemPrompt)
	}
	if err := os.WriteFile(path, []byte("# Drawing\n\nMOUSE-CAT-PROCEDURE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.ensureAdoptedProcedureDocs()
	if !strings.Contains(cb.systemPrompt, "MOUSE-CAT-PROCEDURE") || strings.Count(cb.systemPrompt, adoptedProcedurePromptMarkPrefix) != 1 {
		t.Fatalf("retry did not adopt the document:\n%s", cb.systemPrompt)
	}
	cb.ensureAdoptedProcedureDocs()
	if strings.Count(cb.systemPrompt, adoptedProcedurePromptMarkPrefix) != 1 {
		t.Fatalf("procedure was appended twice:\n%s", cb.systemPrompt)
	}
}

// TestDiscoveryPictureSubstituteNeedsAreACatalogMiss is the 2026-11-06
// picture turn. needs named web_search, a real directory row, so the host
// loaded it and never opened the catalog miss. A picture request has no
// image tool. That substitute stays unloaded. A journal lookup still loads
// web_search. A PDF, a download, a screenshot, a search for an existing
// picture, and a diagram do not take the picture path.
func TestDiscoveryPictureSubstituteNeedsAreACatalogMiss(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	for _, text := range []string{"生成老鼠戏猫图", "生成一张图片", "画图", "文生图"} {
		_, _, cb := turnDirectoryPlannedSurface(t, text, discovery)
		cb.userText = text
		got := cb.runSemanticToolsSearch(`{"query":"` + text + `","needs":["web_search"]}`)
		if !cb.discoveryUnmatchedObligation || !cb.loopCtx.discoveryCatalogMiss() {
			t.Fatalf("%q did not close substitution:\n%s", text, got)
		}
		for _, want := range []string{"没有加载", "search_and_install_skill", "craft_tool"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%q missing %q:\n%s", text, want, got)
			}
		}
		for _, banned := range []string{"做不到", "不要改用已经列出的其它工具", "needs filter matched nothing", "selection_not_authorized", "立即调用已在当前工具面的名字："} {
			if strings.Contains(got, banned) {
				t.Fatalf("%q still says %q:\n%s", text, banned, got)
			}
		}
		have := turnDirectoryHave(cb.BuildToolsForModelRequest(text, 2))
		if have["web_search"] || have["web_fetch"] || have["download_file"] || have["generate_pdf"] || have["current_datetime"] {
			t.Fatalf("%q loaded substitutes: %v", text, turnDirectoryNames(cb.BuildToolsForModelRequest(text, 2)))
		}
	}

	_, _, journal := turnDirectoryPlannedSurface(t, "核对期刊", discovery)
	journal.userText = "核对期刊"
	loaded := journal.runSemanticToolsSearch(`{"query":"核对期刊","needs":["web_search"]}`)
	if journal.discoveryUnmatchedObligation || journal.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("journal search became a miss:\n%s", loaded)
	}
	if !turnDirectoryHave(journal.BuildToolsForModelRequest("核对期刊", 2))["web_search"] {
		t.Fatalf("journal did not load web_search:\n%s", loaded)
	}

	for _, text := range []string{"北京天气，生成pdf", "下载一张猫的图片", "截一张图", "找一张猫的图片", "生成流程图", "生成一张清单", "生成一张表", "画一个方案", "画一个方案图", "如图所示，请生成一段说明", "根据图片生成说明", "生成一段如图所示的说明", "把方案确定后，参考图片生成说明", "不要生成一张图", "不是生成一张图", "生成图片的说明"} {
		_, _, other := turnDirectoryPlannedSurface(t, text, discovery)
		other.userText = text
		page := other.runSemanticToolsSearch(`{"query":"` + text + `","needs":["web_search"]}`)
		if other.discoveryUnmatchedObligation || other.loopCtx.discoveryCatalogMiss() {
			t.Fatalf("%q became a picture miss:\n%s", text, page)
		}
		if !turnDirectoryHave(other.BuildToolsForModelRequest(text, 2))["web_search"] {
			t.Fatalf("%q did not load web_search:\n%s", text, page)
		}
	}
}

// TestDiscoveryDispatchWaitsForTheSameBatchProbe is a catalog probe and a
// dispatch in one assistant message. Dispatch ordered first must not start
// the worker: the handoff fence would drop the later tools_search, and the
// miss would never be recorded. A search that matches, and a batch that only
// dispatches, still hand the work off.
func TestDiscoveryDispatchWaitsForTheSameBatchProbe(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成一张图片", discovery)
	cb.loopCtx.OfferChatTaskDispatch = true
	publishDesktopChatLoop(cb.handler, cb.loopCtx, "生成一张图片")
	if err := cb.OnToolBatchStarting(discoveryAssistantBatch(chatTaskToolDispatch, semanticToolsSearchName), agent.ToolBatchMetadata{Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if cb.loopCtx.discoveryBatchPendingSearchCount() != 1 {
		t.Fatalf("pending = %d", cb.loopCtx.discoveryBatchPendingSearchCount())
	}
	allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"find a public picture and download it"}`)
	if allowed || reason != discoveryDispatchBeforeProbeMessage || strings.Contains(reason, "selection_not_authorized") {
		t.Fatalf("dispatch before the probe allowed=%v reason=%q", allowed, reason)
	}
	for _, banned := range []string{"做不到", "不要改用已经列出的其它工具", "每轮每类限一次", "直接调用那个名字"} {
		if strings.Contains(reason, banned) {
			t.Fatalf("probe hold says %q", banned)
		}
	}
	dispatched := cb.handler.dispatchBackgroundTaskTool(desktopUserID, map[string]interface{}{"request": "find a public picture and download it"})
	if strings.Contains(dispatched.Text, "Started:") || !strings.Contains(dispatched.Text, discoveryDispatchBeforeProbeMessage) {
		t.Fatalf("dispatch tool = %+v", dispatched)
	}
	if cb.loopCtx.chatTaskHandoffSent() || cb.foregroundHandoffOnly() {
		t.Fatal("probe hold closed the turn before tools_search")
	}
	if tasks := cb.handler.ensureChatTasks().List(desktopUserID); len(tasks) != 0 {
		t.Fatalf("probe hold started %d tasks", len(tasks))
	}
	got := cb.runSemanticToolsSearch(`{"query":"生成一张图片","needs":["image.generate"]}`)
	if !strings.Contains(got, "needs filter matched nothing") || !cb.discoveryUnmatchedObligation || cb.loopCtx.discoveryBatchPendingSearchCount() != 0 {
		t.Fatalf("probe pending=%d obligation=%v\n%s", cb.loopCtx.discoveryBatchPendingSearchCount(), cb.discoveryUnmatchedObligation, got)
	}
	allowed, reason = cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"find a public picture and download it"}`)
	if allowed || reason != discoveryUnmatchedExtensionMessage {
		t.Fatalf("dispatch after the miss allowed=%v reason=%q", allowed, reason)
	}
	dispatched = cb.handler.dispatchBackgroundTaskTool(desktopUserID, map[string]interface{}{"request": "find a public picture and download it"})
	if strings.Contains(dispatched.Text, "Started:") || !strings.Contains(dispatched.Text, discoveryUnmatchedExtensionMessage) {
		t.Fatalf("dispatch after the miss = %+v", dispatched)
	}
	if tasks := cb.handler.ensureChatTasks().List(desktopUserID); len(tasks) != 0 {
		t.Fatalf("miss started %d tasks", len(tasks))
	}

	_, _, matched := turnDirectoryPlannedSurface(t, "爱思唯尔 NLP 期刊 CCF", discovery)
	matched.loopCtx.OfferChatTaskDispatch = true
	if err := matched.OnToolBatchStarting(discoveryAssistantBatch(semanticToolsSearchName, chatTaskToolDispatch), agent.ToolBatchMetadata{Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	loaded := matched.runSemanticToolsSearch(`{"query":"Elsevier Natural Language Processing CCF","needs":["web_search","web_fetch"]}`)
	if matched.discoveryUnmatchedObligation || !strings.Contains(loaded, "web_search") {
		t.Fatalf("matched probe:\n%s", loaded)
	}
	allowed, reason = matched.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"keep looking"}`)
	if !allowed || reason != "" {
		t.Fatalf("dispatch after a match allowed=%v reason=%q", allowed, reason)
	}
	if err := matched.OnToolBatchStarting(discoveryAssistantBatch(chatTaskToolDispatch), agent.ToolBatchMetadata{Sequence: 2}); err != nil {
		t.Fatal(err)
	}
	if matched.loopCtx.discoveryBatchPendingSearchCount() != 0 {
		t.Fatalf("dispatch-only pending = %d", matched.loopCtx.discoveryBatchPendingSearchCount())
	}
	allowed, reason = matched.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"keep looking"}`)
	if !allowed || reason != "" {
		t.Fatalf("dispatch-only allowed=%v reason=%q", allowed, reason)
	}
	matched.loopCtx.setDiscoveryBatchPendingSearch(1)
	if err := matched.OnToolBatchCommitted(nil, agent.ToolBatchMetadata{}); err != nil {
		t.Fatal(err)
	}
	if matched.loopCtx.discoveryBatchPendingSearchCount() != 0 {
		t.Fatal("commit kept the probe hold")
	}

	cb.loopCtx.setDiscoveryBatchPendingSearch(2)
	resetSemanticTurnLocalState(cb.loopCtx)
	if cb.loopCtx.discoveryBatchPendingSearchCount() != 0 || cb.loopCtx.discoveryCatalogMiss() {
		t.Fatal("turn reset kept the probe hold")
	}
}

func discoveryAssistantBatch(names ...string) []agent.ConversationEntry {
	calls := make([]llm.ToolCall, len(names))
	for i, name := range names {
		calls[i] = llm.ToolCall{
			ID:   fmt.Sprintf("call-%d", i+1),
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      name,
				Arguments: `{}`,
			},
		}
	}
	return []agent.ConversationEntry{{
		Role:      "assistant",
		ToolCalls: calls,
	}}
}

func TestFailedSkillInstallDoesNotSettleTheMiss(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成一张图片", discovery)
	cb.noteDiscoveryExtensionHost("search_and_install_skill", "installed draw", true)
	if cb.loopCtx.catalogMissExtensionSettled() {
		t.Fatal("an install settled a miss that has not happened")
	}
	cb.noteDiscoveryCatalogMiss()
	if !cb.discoveryUnmatchedObligation || !cb.loopCtx.catalogMissExtensionSettled() {
		t.Fatal("an install that already ran did not settle the later miss")
	}
	cb.discoverySkillInstalled = false
	cb.discoverySkillSearchRan = false
	cb.loopCtx.mu.Lock()
	cb.loopCtx.discoveryMissExtensionSettled = false
	cb.loopCtx.mu.Unlock()
	cb.noteDiscoveryExtensionHost("search_and_install_skill", "GitHub skill import failed: no candidate", false)
	if cb.discoverySkillInstalled || cb.discoveryExtensionSettled() || cb.loopCtx.catalogMissExtensionSettled() {
		t.Fatal("import failure settled the miss")
	}
	if !cb.discoverySkillSearchRan {
		t.Fatal("the failed search was not recorded")
	}
}

func TestDiscoveryToolsSearchLoadsTheNamedTools(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "爱思唯尔 NLP 期刊 CCF", discovery)
	got := cb.runSemanticToolsSearch(`{"query":"Elsevier Natural Language Processing CCF","needs":["web_search","web_fetch"]}`)
	if strings.Contains(got, "current_datetime") || strings.Contains(got, "没有未列出的计划步骤") || strings.Contains(got, "cannot change") || strings.Contains(got, "没有对得上的名字") {
		t.Fatalf("clock, frozen surface, or miss step:\n%s", got)
	}
	if !strings.Contains(got, semanticToolsSearchListedDirectivePrefix) || !strings.Contains(got, "web_search") || !strings.Contains(got, "web_fetch") {
		t.Fatalf("named tools were not the next call:\n%s", got)
	}
	cb.loopCtx.OfferChatTaskDispatch = true
	defs := cb.BuildToolsForModelRequest("爱思唯尔 NLP 期刊 CCF", 2)
	have := turnDirectoryHave(defs)
	if !have["web_search"] || !have["web_fetch"] || have["current_datetime"] || !have["dispatch_background_task"] {
		t.Fatalf("next request = %v", turnDirectoryNames(defs))
	}
	same := `{"query":"Elsevier Natural Language Processing CCF","needs":["web_search","web_fetch"]}`
	again := cb.runSemanticToolsSearch(same)
	if again != semanticToolsSearchDirectedExhausted || strings.Contains(again, "cannot change") {
		t.Fatalf("repeat = %s", again)
	}
	var budget string
	for i := 0; i < 3; i++ {
		budget = cb.runSemanticToolsSearch(same)
	}
	if budget != semanticToolsSearchDirectedExhausted || strings.Contains(budget, "cannot change") {
		t.Fatalf("budget = %s", budget)
	}
}

func TestDiscoveryToolsSearchLoadsACapabilityID(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "爱思唯尔 NLP 期刊 CCF", discovery)
	got := cb.runSemanticToolsSearch(`{"query":"Elsevier Natural Language Processing CCF","needs":["information.search.web","information.fetch.web"]}`)
	if strings.Contains(got, "current_datetime") || strings.Contains(got, "没有未列出的计划步骤") || strings.Contains(got, "needs filter matched nothing") || strings.Contains(got, "没有对得上的名字") {
		t.Fatalf("capability id was not the tool:\n%s", got)
	}
	if !strings.Contains(got, semanticToolsSearchListedDirectivePrefix) || !strings.Contains(got, "web_search") || !strings.Contains(got, "web_fetch") {
		t.Fatalf("capability id did not name the tool:\n%s", got)
	}
	defs := cb.BuildToolsForModelRequest("爱思唯尔 NLP 期刊 CCF", 2)
	have := turnDirectoryHave(defs)
	if !have["web_search"] || !have["web_fetch"] || have["current_datetime"] {
		t.Fatalf("next request = %v", turnDirectoryNames(defs))
	}
}

func TestDiscoveryToolsSearchLoadsANamedSkillBody(t *testing.T) {
	alphaDir := writeGuidedSkillDir(t, "SKILL-ALPHA-BODY")
	alpha := corelib.NLSkillEntry{Name: "Foo Bar", Status: "active", Source: "file", Description: "alpha", SkillDir: alphaDir}
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	installed := []corelib.NLSkillEntry{alpha}
	app.nlSkillsSnap.Store(&installed)
	app.cachedSkillScanner = &CachedSkillScanner{}
	app.cachedSkillScanner.cache.Store(&skillCacheEntry{skills: []corelib.NLSkillEntry{}, createdAt: time.Now()})
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app}
	alias := discoverySkillAlias(t, h.getSkillExecutor(), "Foo Bar")
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "用这个技能", discovery)
	cb.handler = h
	cb.discoveryCatalogLoaded = true
	cb.discoveryCatalog = discoverySkillDirectoryRows(h.getSkillExecutor())
	raw, err := json.Marshal(map[string]interface{}{"query": "用这个技能", "needs": []string{alias}})
	if err != nil {
		t.Fatal(err)
	}
	got := cb.runSemanticToolsSearch(string(raw))
	if !strings.Contains(got, "SKILL-ALPHA-BODY") || strings.Contains(got, "needs filter matched nothing") || strings.Contains(got, "没有对得上的名字") || strings.Contains(got, "每轮每类限一次") {
		t.Fatalf("skill body missing:\n%s", got)
	}
	if !strings.Contains(got, "[已装载]") || strings.Contains(got, semanticToolsSearchListedDirectivePrefix) {
		t.Fatalf("guided skill was treated as a function:\n%s", got)
	}
	bodyAt := strings.Index(got, "SKILL-ALPHA-BODY")
	rowAt := strings.Index(got, "[已装载]")
	if bodyAt < 0 || rowAt < 0 || bodyAt > rowAt {
		t.Fatalf("skill body was not ahead of the row:\n%s", got)
	}
}

func TestDiscoveryToolsSearchInvalidPageTokenDoesNotLoad(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "查资料", discovery)
	got := cb.runSemanticToolsSearch(`{"query":"查资料","needs":["web_search"],"page_token":"v1:0:not-a-real-digest"}`)
	if got != "[system rejected] tools_search_page_token_invalid" {
		t.Fatalf("token = %s", got)
	}
	defs := cb.BuildToolsForModelRequest("查资料", 2)
	have := turnDirectoryHave(defs)
	if have["web_search"] || have["current_datetime"] {
		t.Fatalf("rejected token loaded tools: %v", turnDirectoryNames(defs))
	}
	loaded := cb.runSemanticToolsSearch(`{"query":"查资料","needs":["web_search"]}`)
	if !strings.Contains(loaded, semanticToolsSearchListedDirectivePrefix) || !strings.Contains(loaded, "web_search") {
		t.Fatalf("a later valid search did not load web_search:\n%s", loaded)
	}
}

func TestDiscoveryToolsSearchMissSurvivesTheUnfilteredDirectory(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成一张图片", discovery)
	if first := cb.runSemanticToolsSearch(`{"query":"生成一张图片"}`); strings.Contains(first, semanticToolsSearchDiscoveryNextStep) {
		t.Fatalf("unfiltered directory already had the miss step:\n%s", first)
	}
	if cb.discoveryUnmatchedObligation || cb.loopCtx.discoveryCatalogMiss() {
		t.Fatal("an unfiltered directory set the catalog-miss obligation")
	}
	got := cb.runSemanticToolsSearch(`{"query":"生成一张图片","needs":["image.generate"]}`)
	if !strings.Contains(got, semanticToolsSearchDiscoveryNextStep) || !strings.Contains(got, "needs filter matched nothing") {
		t.Fatalf("miss after the directory was dropped:\n%s", got)
	}
	again := cb.runSemanticToolsSearch(`{"query":"生成一张图片","needs":["image.generate"]}`)
	if again != semanticToolsSearchDiscoveryRepeatExhausted {
		t.Fatalf("second miss = %s", again)
	}
}

func TestDiscoveryToolsSearchKeepsTheCallAfterASkillBody(t *testing.T) {
	alphaDir := writeGuidedSkillDir(t, "SKILL-ALPHA-BODY")
	alpha := corelib.NLSkillEntry{Name: "Foo Bar", Status: "active", Source: "file", Description: "alpha", SkillDir: alphaDir}
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	installed := []corelib.NLSkillEntry{alpha}
	app.nlSkillsSnap.Store(&installed)
	app.cachedSkillScanner = &CachedSkillScanner{}
	app.cachedSkillScanner.cache.Store(&skillCacheEntry{skills: []corelib.NLSkillEntry{}, createdAt: time.Now()})
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "用这个技能查资料", discovery)
	cb.handler.app = app
	alias := discoverySkillAlias(t, cb.handler.getSkillExecutor(), "Foo Bar")
	cb.discoveryCatalogLoaded = true
	cb.discoveryCatalog = discoverySkillDirectoryRows(cb.handler.getSkillExecutor())
	raw, err := json.Marshal(map[string]interface{}{"query": "用这个技能查资料", "needs": []string{alias, "web_search"}})
	if err != nil {
		t.Fatal(err)
	}
	got := cb.runSemanticToolsSearch(string(raw))
	bodyAt := strings.Index(got, "SKILL-ALPHA-BODY")
	callAt := strings.Index(got, semanticToolsSearchListedDirectivePrefix)
	if bodyAt < 0 || callAt < 0 || bodyAt > callAt || !strings.Contains(got, "[已装载]") || !strings.Contains(got, "web_search") {
		t.Fatalf("skill body displaced the call:\n%s", got)
	}
	if last := semanticToolsSearchLastLine(got); !strings.HasPrefix(last, semanticToolsSearchListedDirectivePrefix) || !strings.Contains(last, "web_search") {
		t.Fatalf("call was not the tail: %q\n%s", last, got)
	}
	defs := cb.BuildToolsForModelRequest("用这个技能查资料", 2)
	have := turnDirectoryHave(defs)
	if !have["web_search"] || have["current_datetime"] {
		t.Fatalf("next request = %v", turnDirectoryNames(defs))
	}
}

func TestDiscoveryToolsSearchOmitsClassifiedPetitionFooter(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "看看目录", discovery)
	got := cb.runSemanticToolsSearch(`{"query":"看看目录"}`)
	if strings.Contains(got, "每轮每类限一次") || strings.Contains(got, "不在普通回合里") {
		t.Fatalf("classified petition rule on a discovery directory:\n%s", got)
	}
	if !strings.Contains(got, "- bash — ") || !strings.Contains(got, "[可请愿]") || strings.Contains(got, "直接调用一次") {
		t.Fatalf("bash must stay petitionable without an emit:\n%s", got)
	}
	if !strings.Contains(got, semanticToolsSearchDiscoveryLoadLine) {
		t.Fatalf("discovery directory did not say to load the name through needs:\n%s", got)
	}
	again := cb.runSemanticToolsSearch(`{"query":"看看目录"}`)
	if again != semanticToolsSearchDiscoveryRepeatExhausted || strings.Contains(again, "Call a listed tool") || strings.Contains(again, "cannot change") {
		t.Fatalf("discovery repeat = %s", again)
	}
	defs := cb.BuildToolsForModelRequest("看看目录", 2)
	have := turnDirectoryHave(defs)
	if have["web_search"] || have["bash"] || have["current_datetime"] {
		t.Fatalf("an unfiltered directory loaded tools: %v", turnDirectoryNames(defs))
	}
}

func TestDiscoveryChatTaskStaysOnDiscoverySurface(t *testing.T) {
	calls := 0
	h := &IMMessageHandler{
		registry: NewToolRegistry(),
		unifiedClassifier: intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
			calls++
			return `{"top":[{"skill":"shell_command","score":0.99}]}`, nil
		}}),
	}
	registerBuiltinTools(h.registry, h)
	discovery := intent.ModelDiscoveryClassification()
	worker := NewLoopContext("chat-task", 30, nil)
	worker.ChatTaskID = "ct-1"
	worker.UserID = "user-1"
	worker.Runtime.SemanticIntent = &discovery
	worker.Runtime.Execution = fullExecutionProfile("background chat task")
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(worker, "user-1", "生成一张图片", "desktop")
	if err != nil || !handled || surface == nil || !semanticSurfaceIsModelDiscovery(surface) {
		t.Fatalf("discovery worker handled=%v err=%v surface=%v", handled, err, surface != nil)
	}
	if calls != 0 {
		t.Fatalf("discovery worker classified: %d", calls)
	}
	if !loopContextBlocksLegacyToolRouter(worker) {
		t.Fatal("discovery worker re-entered the name router")
	}
	helper := NewLoopContext("chat-task-computer", 30, nil)
	helper.ChatTaskID = "ct-helper"
	helper.ComputerHelper = true
	helper.Runtime.SemanticIntent = &discovery
	_, helperSurface, helperHandled, helperErr := h.semanticCallSurfaceForSharedTurnWithContext(helper, "user-1", "生成一张图片", "desktop")
	if helperErr != nil || helperHandled || helperSurface != nil {
		t.Fatalf("computer helper handled=%v err=%v", helperHandled, helperErr)
	}
	if loopContextBlocksLegacyToolRouter(helper) {
		t.Fatal("computer helper skipped the name router")
	}
}

func TestDiscoveryCatalogProjectionSharesOneObservation(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{
		{ID: "launch-unstarted", Name: "launch-unstarted", Command: "unused", AutoStart: true},
	}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	t.Cleanup(func() { app.localMCPManager.cancel() })
	cb := &sharedAgentLoopCallbacks{
		handler:      &IMMessageHandler{app: app},
		systemPrompt: "Governed tools:\n",
		semanticSurface: &semanticCallSurface{
			replan: &semanticReplanInput{Classification: intent.ModelDiscoveryClassification()},
		},
	}
	cb.projectSemanticDirectoryPrompt()
	if cb.discoveryCatalogLoads != 1 {
		t.Fatalf("one projection observed the catalog %d times", cb.discoveryCatalogLoads)
	}
	if cb.discoveryCatalogLoaded {
		t.Fatal("not-ready observation was kept for the turn")
	}
	cb.SyncDirectoryToRenderedTools(nil)
	if cb.discoveryCatalogLoads != 1 {
		t.Fatalf("filtered rewrite observed the catalog again: loads=%d", cb.discoveryCatalogLoads)
	}
	if strings.Count(cb.systemPrompt, semanticTurnDirectoryClose) != 1 {
		t.Fatalf("directory was not projected once:\n%s", cb.systemPrompt)
	}
	app.localMCPManager.syncFinished.Store(true)
	cb.projectSemanticDirectoryPrompt()
	if cb.discoveryCatalogLoads != 2 {
		t.Fatalf("the next projection did not take a new observation: loads=%d", cb.discoveryCatalogLoads)
	}
	if !cb.discoveryCatalogLoaded {
		t.Fatal("finished MCP family was not cached")
	}
	if strings.Count(cb.systemPrompt, semanticTurnDirectoryClose) != 1 {
		t.Fatalf("the refreshed directory was prepended:\n%s", cb.systemPrompt)
	}
}

func TestDiscoveryCatalogGrantBindsTheObservedMCPRow(t *testing.T) {
	base := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.effectiveBaseDir.Store(base)
	cb := &sharedAgentLoopCallbacks{
		handler:                &IMMessageHandler{app: app},
		loopCtx:                &LoopContext{},
		discoveryCatalogLoaded: true,
		discoveryCatalog: []semanticTurnDirectoryEntry{{
			Name:        "rewrittenmcp_abcdef12",
			CatalogKind: discoveryCatalogKindMCP,
			ServerID:    "server-1",
			ToolName:    "post_work_log_add",
		}},
	}
	ok, msg := cb.grantDiscoveryCatalogName("rewrittenmcp_abcdef12")
	if ok || msg != "" {
		t.Fatalf("publish succeeded without an invocation identity: %v %q", ok, msg)
	}
	if binds := cb.loopCtx.discoveredMCPBindList(); len(binds) != 0 {
		t.Fatalf("failed publish left the bind on the turn: %+v", binds)
	}
	if cb.discoveryCatalogLoads != 0 {
		t.Fatalf("grant observed the catalog again: %d", cb.discoveryCatalogLoads)
	}
}

func TestUnreadyDiscoveryPetitionKeepsTheCurrentTools(t *testing.T) {
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
	if !loop.rememberDiscoveredMCPBind("tengyun", "post_work_log_add") {
		t.Fatal("bind was not stored")
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: loop, userID: "desktop-user", platform: "desktop",
		discoveryCatalogLoaded: true,
	}
	if err := cb.attachDiscoveredMCPBindings(); err != nil {
		t.Fatal(err)
	}
	surface := cb.discoveredMCPSurface
	if surface == nil {
		t.Fatal("ready tool was not published")
	}
	goodName := tool.InstalledMCPModelFunctionName("tengyun", "post_work_log_add")
	badName := tool.InstalledMCPModelFunctionName("tengyun", "bad_enum")
	cb.discoveryCatalog = []semanticTurnDirectoryEntry{
		{Name: goodName, CatalogKind: discoveryCatalogKindMCP, ServerID: "tengyun", ToolName: "post_work_log_add"},
		{Name: badName, CatalogKind: discoveryCatalogKindMCP, ServerID: "tengyun", ToolName: "bad_enum"},
	}
	ok, msg := cb.grantDiscoveryCatalogName(badName)
	if ok || msg != "" {
		t.Fatalf("unprojectable petition succeeded: %v %q", ok, msg)
	}
	if cb.discoveredMCPSurface != surface {
		t.Fatal("unprojectable petition replaced the current MCP tools")
	}
	if len(cb.discoveredMCPBinds) != 1 || cb.discoveredMCPBinds[0].ServerID != "tengyun" || cb.discoveredMCPBinds[0].ToolName != "post_work_log_add" {
		t.Fatalf("published binds changed: %+v", cb.discoveredMCPBinds)
	}
	for _, bind := range loop.discoveredMCPBindList() {
		if bind.ToolName == "bad_enum" {
			t.Fatalf("unprojectable petition left the bind on the turn: %+v", loop.discoveredMCPBindList())
		}
	}
	ok, msg = cb.grantDiscoveryCatalogName(goodName)
	if !ok || !strings.Contains(msg, "已装载 "+goodName) {
		t.Fatalf("ready petition failed: %v %q", ok, msg)
	}
	if cb.discoveredMCPSurface != surface {
		t.Fatal("ready petition replaced an already published tool")
	}
}

func TestDiscoverySkillPetitionKeepsTheObservedSkill(t *testing.T) {
	alphaDir := writeGuidedSkillDir(t, "SKILL-ALPHA-BODY")
	betaDir := writeGuidedSkillDir(t, "SKILL-BETA-BODY")
	alpha := corelib.NLSkillEntry{Name: "Foo Bar", Status: "active", Source: "file", Description: "alpha", SkillDir: alphaDir}
	beta := corelib.NLSkillEntry{Name: "foo_bar", Status: "active", Source: "file", Description: "beta", SkillDir: betaDir}
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	installed := []corelib.NLSkillEntry{alpha, beta}
	app.nlSkillsSnap.Store(&installed)
	app.cachedSkillScanner = &CachedSkillScanner{}
	app.cachedSkillScanner.cache.Store(&skillCacheEntry{skills: []corelib.NLSkillEntry{}, createdAt: time.Now()})
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app}
	listed := discoveryInstalledSkills(h.getSkillExecutor())
	var alphaAlias, betaAlias string
	for _, row := range listed {
		switch row.Entry.SkillDir {
		case alphaDir:
			alphaAlias = row.Alias
		case betaDir:
			betaAlias = row.Alias
		}
	}
	if alphaAlias == "" || betaAlias == "" || alphaAlias == betaAlias {
		t.Fatalf("aliases alpha=%q beta=%q", alphaAlias, betaAlias)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, loopCtx: &LoopContext{},
		discoveryCatalogLoaded: true,
		discoveryCatalog:       discoverySkillDirectoryRows(h.getSkillExecutor()),
	}
	reversed := []corelib.NLSkillEntry{beta, alpha}
	app.nlSkillsSnap.Store(&reversed)
	ok, msg := cb.grantDiscoveryCatalogName(alphaAlias)
	if !ok || !strings.Contains(msg, "SKILL-ALPHA-BODY") || strings.Contains(msg, "SKILL-BETA-BODY") {
		t.Fatalf("petition followed the swapped alias: ok=%v msg=%q", ok, msg)
	}
}

func TestFailedMCPRepublishKeepsTheCurrentTools(t *testing.T) {
	surface := &semanticCallSurface{}
	cb := &sharedAgentLoopCallbacks{
		handler:              &IMMessageHandler{},
		loopCtx:              &LoopContext{},
		discoveredMCPSurface: surface,
		discoveredMCPBinds:   []discoveredMCPBind{{ServerID: "old", ToolName: "keep"}},
	}
	cb.loopCtx.rememberDiscoveredMCPBind("old", "keep")
	cb.loopCtx.rememberDiscoveredMCPBind("new", "tool")
	if err := cb.attachDiscoveredMCPBindings(); err == nil {
		t.Fatal("publish succeeded without an invocation identity")
	}
	if cb.discoveredMCPSurface != surface {
		t.Fatal("failed publish dropped the current MCP tools")
	}
	if len(cb.discoveredMCPBinds) != 1 || cb.discoveredMCPBinds[0].ServerID != "old" || cb.discoveredMCPBinds[0].ToolName != "keep" {
		t.Fatalf("published binds changed: %+v", cb.discoveredMCPBinds)
	}
}

func TestDiscoveryCatalogGrantFollowsTheRowKind(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{discoveryCatalogLoaded: true}
	cb.discoveryCatalog = []semanticTurnDirectoryEntry{{
		Name: "rewrittenmcp_abcdef12", CatalogKind: discoveryCatalogKindMCP,
	}, {
		Name: "server__kept_skill", CatalogKind: discoveryCatalogKindSkill,
	}}
	if got := cb.discoveryCatalogKind("rewrittenmcp_abcdef12"); got != discoveryCatalogKindMCP {
		t.Fatalf("rewritten MCP kind=%q", got)
	}
	if got := cb.discoveryCatalogKind("server__kept_skill"); got != discoveryCatalogKindSkill {
		t.Fatalf("skill whose name contains __ kind=%q", got)
	}
	if cb.discoveryCatalogHas("missing") {
		t.Fatal("missing name was admitted")
	}
}

func TestDiscoveryCatalogDoesNotCacheUnreadyMCPFamily(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{
		{ID: "launch-unstarted", Name: "launch-unstarted", Command: "unused", AutoStart: true},
	}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	t.Cleanup(func() { app.localMCPManager.cancel() })
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{app: app}}
	if rows := cb.discoveryCatalogRows(); cb.discoveryCatalogLoaded {
		t.Fatalf("not-ready MCP family was cached: %v", rows)
	}
	cb.pinDiscoveryCatalog()
	start := cb.discoveryCatalogLoads
	_ = cb.discoveryCatalogRows()
	_ = cb.discoveryCatalogHas("later-server__tool")
	if cb.discoveryCatalogLoads != start+1 {
		t.Fatalf("one petition re-read an unready catalog: loads=%d", cb.discoveryCatalogLoads-start)
	}
	cb.unpinDiscoveryCatalog()
	if cb.discoveryCatalogLoaded {
		t.Fatal("petition hold cached an unready family")
	}
	app.localMCPManager.syncFinished.Store(true)
	if rows := cb.discoveryCatalogRows(); !cb.discoveryCatalogLoaded {
		t.Fatalf("finished MCP family was not cached: %v", rows)
	}
}

func TestDiscoveryCatalogDoesNotCacheFailedMCPInventory(t *testing.T) {
	base := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.effectiveBaseDir.Store(base)
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{app: app}}
	if rows := cb.discoveryCatalogRows(); len(rows) != 0 || cb.discoveryCatalogLoaded {
		t.Fatalf("failed inventory cached rows=%v loaded=%v", rows, cb.discoveryCatalogLoaded)
	}
	if rows := cb.discoveryCatalogRows(); len(rows) != 0 || cb.discoveryCatalogLoaded {
		t.Fatalf("retry cached the failure rows=%v loaded=%v", rows, cb.discoveryCatalogLoaded)
	}
	cb.pinDiscoveryCatalog()
	beforeHold := cb.discoveryCatalogLoads
	_ = cb.discoveryCatalogRows()
	_ = cb.discoveryCatalogHas("tengyun__post_work_log_add")
	if cb.discoveryCatalogLoads != beforeHold+1 {
		t.Fatalf("one petition re-read a failed inventory: loads=%d", cb.discoveryCatalogLoads-beforeHold)
	}
	cb.unpinDiscoveryCatalog()
	if cb.discoveryCatalogLoaded {
		t.Fatal("petition hold cached a failed inventory")
	}
	cb.systemPrompt = "Governed tools:\n"
	cb.semanticSurface = &semanticCallSurface{
		replan: &semanticReplanInput{Classification: intent.ModelDiscoveryClassification()},
	}
	before := cb.discoveryCatalogLoads
	cb.projectSemanticDirectoryPrompt()
	cb.SyncDirectoryToRenderedTools(nil)
	if got := cb.discoveryCatalogLoads - before; got != 2 {
		t.Fatalf("filtered rewrite reused a failed inventory: loads=%d", got)
	}
	if cb.discoveryCatalogLoaded {
		t.Fatal("failed inventory was cached after the rewrite")
	}
}

// A discovery tools_search that names web_search used to copy LabelSearch's
// required bit onto the hop. Office and send_file could finish, and the
// pending search still hard-exited required_effect_unsettled. The petition
// loads the tool. It is not a classifier obligation. Office stays one.
func TestDiscoveryPetitionedWebSearchDoesNotBlockFinishedOffice(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成庆祝北京建城1500年纪念ppt", discovery)
	for _, name := range []string{"web_search", "web_fetch", "office"} {
		granted, message := cb.PetitionToolCall(name)
		if !granted {
			t.Fatalf("%s petition granted=%v message=%q", name, granted, message)
		}
	}
	surface := cb.semanticSurface
	if surface == nil || !surface.plan.ObligationsRecorded {
		t.Fatal("petition did not record obligations")
	}
	search := plannedBaseSelection(surface.plan, agentservice.CapabilityInformationSearchWeb)
	fetch := plannedBaseSelection(surface.plan, tool.CapabilityInformationFetchWeb)
	office := plannedBaseSelection(surface.plan, tool.CapabilityDocumentWriteOffice)
	if search == nil || !search.Optional || search.FitProof.QualifierBindings[agentservice.QualifierSearchFreshness] != agentservice.SearchFreshnessReference {
		t.Fatalf("discovery search = %#v", search)
	}
	if fetch == nil || !fetch.Optional {
		t.Fatalf("discovery fetch = %#v", fetch)
	}
	if office == nil || office.Optional {
		t.Fatalf("discovery office = %#v", office)
	}
	if !cb.RequiredEffectUnsettled() {
		t.Fatal("unstarted office was treated as finished")
	}
	if surface.completed == nil {
		surface.completed = map[string]bool{}
	}
	// Only the office base is closed. Any other required step, including a
	// lookup this petition should have relaxed, still fails the run.
	surface.completed[office.ID] = true
	snapshot := cb.hopRunSnapshot()
	if cb.RequiredEffectUnsettled() || snapshot.Remaining != 0 {
		t.Fatalf("finished office still open: remaining=%d unsettled=%v steps=%+v", snapshot.Remaining, cb.RequiredEffectUnsettled(), snapshot.Steps)
	}
	assertPendingOptionalStep(t, snapshot, string(agentservice.CapabilityInformationSearchWeb))
	assertPendingOptionalStep(t, snapshot, string(tool.CapabilityInformationFetchWeb))
}

func TestTreeConfirmedSearchStaysUnsettled(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.95, Reason: "tree confirmed search"}
	_, surface, cb := turnDirectoryPlannedSurface(t, "搜索北京建城纪年", search)
	selection := plannedBaseSelection(surface.plan, agentservice.CapabilityInformationSearchWeb)
	if selection == nil || selection.Optional || !surface.plan.ObligationsRecorded {
		t.Fatalf("tree search = %#v recorded=%v", selection, surface.plan.ObligationsRecorded)
	}
	snapshot := cb.hopRunSnapshot()
	if !cb.RequiredEffectUnsettled() || snapshot.Remaining == 0 {
		t.Fatalf("unstarted tree search settled: remaining=%d unsettled=%v", snapshot.Remaining, cb.RequiredEffectUnsettled())
	}
	assertPendingRequiredStep(t, snapshot, string(agentservice.CapabilityInformationSearchWeb))
}

func TestDiscoveryPetitionedLookupNeedIsNotAnObligation(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	discovery := intent.ModelDiscoveryClassification()
	ctx := withSemanticPetitionedLabels(context.Background(), []intent.IntentLabel{
		intent.LabelSearch, intent.LabelWebFetch, intent.LabelOffice, intent.LabelFileRead,
	})
	needs, managed, err := semanticNeedsFromClassificationContext(ctx, registry, discovery)
	if err != nil || !managed {
		t.Fatalf("managed=%v err=%v needs=%#v", managed, err, needs)
	}
	assertBaseNeed(t, needs, agentservice.CapabilityInformationSearchWeb, map[string]string{
		agentservice.QualifierSearchFreshness: agentservice.SearchFreshnessReference,
	}, false)
	assertBaseNeed(t, needs, tool.CapabilityInformationFetchWeb, nil, false)
	assertBaseNeed(t, needs, tool.CapabilityDocumentWriteOffice, nil, true)
	assertBaseNeed(t, needs, tool.CapabilityFSReadLocal, nil, true)
	assertBaseNeed(t, needs, agentservice.CapabilityCurrentTime, nil, false)

	visualNeeds, _, err := semanticNeedsFromClassificationContext(
		withSemanticPetitionedLabels(context.Background(), []intent.IntentLabel{intent.LabelLiveDataVisual}),
		registry, discovery,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNeed(t, visualNeeds, agentservice.CapabilityInformationSearchWeb, map[string]string{
		agentservice.QualifierSearchFreshness: agentservice.SearchFreshnessCurrent,
	}, true)
	assertBaseNeed(t, visualNeeds, agentservice.CapabilityLiveDataVisual, nil, true)

	// The chart records the current-web search first. A later live_data
	// petition names that same id and must not clear the bit the chart owns.
	chartThenLookup, _, err := semanticNeedsFromClassificationContext(
		withSemanticPetitionedLabels(context.Background(), []intent.IntentLabel{intent.LabelLiveDataVisual, intent.LabelLiveData}),
		registry, discovery,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNeed(t, chartThenLookup, agentservice.CapabilityInformationSearchWeb, map[string]string{
		agentservice.QualifierSearchFreshness: agentservice.SearchFreshnessCurrent,
	}, true)
	assertBaseNeed(t, chartThenLookup, agentservice.CapabilityLiveDataVisual, nil, true)

	tree := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.95, Reason: "tree confirmed search"}
	treeNeeds, _, err := semanticNeedsFromClassificationContext(
		withSemanticPetitionedLabels(context.Background(), []intent.IntentLabel{intent.LabelOffice}),
		registry, tree,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertBaseNeed(t, treeNeeds, agentservice.CapabilityInformationSearchWeb, map[string]string{
		agentservice.QualifierSearchFreshness: agentservice.SearchFreshnessReference,
	}, true)
	assertBaseNeed(t, treeNeeds, tool.CapabilityDocumentWriteOffice, nil, true)
}

func assertBaseNeed(t *testing.T, needs []tool.CapabilityNeed, capability tool.CapabilityID, qualifiers map[string]string, required bool) {
	t.Helper()
	found := 0
	for _, need := range needs {
		if need.Capability != capability || tool.IsRepeatCeilingID(need.ID) {
			continue
		}
		if !tool.QualifiersEqual(need.Qualifiers, qualifiers) {
			continue
		}
		found++
		if need.Required != required {
			t.Fatalf("%s qualifiers=%v required=%v, want %v", capability, need.Qualifiers, need.Required, required)
		}
	}
	if found == 0 {
		t.Fatalf("missing %s qualifiers=%v among %d needs", capability, qualifiers, len(needs))
	}
}

func assertPendingOptionalStep(t *testing.T, snapshot *HopRunSnapshot, capability string) {
	t.Helper()
	assertHopBaseStep(t, snapshot, capability, false, HopStepOptional)
}

func assertPendingRequiredStep(t *testing.T, snapshot *HopRunSnapshot, capability string) {
	t.Helper()
	assertHopBaseStep(t, snapshot, capability, true, HopStepPending)
}

func assertHopBaseStep(t *testing.T, snapshot *HopRunSnapshot, capability string, required bool, status HopStepStatus) {
	t.Helper()
	if snapshot == nil {
		t.Fatal("missing hop snapshot")
	}
	for _, step := range snapshot.Steps {
		if step.Capability != capability || tool.IsRepeatCeilingID(step.NeedID) {
			continue
		}
		if step.Required != required || step.Status != status || step.Evidence.State != "" {
			t.Fatalf("%s step = %+v", capability, step)
		}
		return
	}
	t.Fatalf("missing %s step in %+v", capability, snapshot.Steps)
}

func plannedBaseSelection(plan tool.ToolPlan, capability tool.CapabilityID) *tool.PlannedSelection {
	for i := range plan.Selections {
		selection := &plan.Selections[i]
		if selection.FitProof.MatchedCapability != capability || tool.IsRepeatCeilingID(selection.NeedID) {
			continue
		}
		return selection
	}
	return nil
}

func discoveryDirectoryRows(prompt, name string) []string {
	prefix := "- " + name + " — "
	var rows []string
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, prefix) {
			rows = append(rows, line)
		}
	}
	return rows
}

func discoveryDirectoryLine(prompt, name string) string {
	rows := discoveryDirectoryRows(prompt, name)
	if len(rows) == 0 {
		return ""
	}
	return strings.TrimPrefix(rows[0], "- "+name+" — ")
}

func promptStillPetitions(prompt, name string) bool {
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, name) && strings.Contains(line, "可请愿，不在当前函数列表里") {
			return true
		}
	}
	return false
}

func toolDefinitionDescription(defs []map[string]interface{}, name string) string {
	for _, def := range defs {
		if extractToolName(def) != name {
			continue
		}
		fn, _ := def["function"].(map[string]interface{})
		desc, _ := fn["description"].(string)
		return desc
	}
	return ""
}

func TestDiscoveryDirectoryDoesNotCarryIntoShortFollowUp(t *testing.T) {
	calls := 0
	h := &IMMessageHandler{
		registry: NewToolRegistry(),
		unifiedClassifier: intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
			calls++
			return `{"top":[{"skill":"shell_command","score":0.99}]}`, nil
		}}),
	}
	registerBuiltinTools(h.registry, h)
	h.SetToolRouter(NewToolRouter(nil))
	tools := modelDiscoveryVisibleTools(h, nil)
	tools = append(tools,
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": chatTaskToolDispatch}},
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": chatTaskToolSteer}},
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": chatTaskToolStop}},
	)
	h.recordSemanticExecutionSurface("user-1", fullExecutionProfile("model discovery"), tools)
	if h.parentExecutionIsFull("user-1") {
		t.Fatalf("directory presets became a parent surface: %v", h.parentExecutionTools("user-1"))
	}
	follow, semantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{Text: "继续", UserID: "user-1"}, false, false)
	if calls != 0 {
		t.Fatalf("classifier calls=%d", calls)
	}
	if semantic == nil || !intent.ClassificationIsModelDiscovery(*semantic) {
		t.Fatalf("short follow-up left discovery: profile=%+v semantic=%+v", follow, semantic)
	}
	if routes := h.toolRouter.LegacyRouteCalls(); routes != 0 {
		t.Fatalf("directory follow-up entered the name router: %d", routes)
	}
}

func TestShortContinuationPublishesCarriedToolsWithoutRanking(t *testing.T) {
	calls := 0
	h := &IMMessageHandler{
		registry: NewToolRegistry(),
		unifiedClassifier: intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
			calls++
			return `{"top":[{"skill":"web_search","score":0.99}]}`, nil
		}}),
	}
	registerBuiltinTools(h.registry, h)
	h.SetToolRouter(NewToolRouter(nil))
	h.noteParentExecution("desktop-user", true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "ssh"}},
	})
	ctx := NewLoopContext("chat", 3, nil)
	ctx.Runtime.Execution = fullExecutionProfile(shortContinuationReason)
	set := h.prepareAgentLoopTools("desktop-user", "继续", ctx, agentLoopPhase{})
	if calls != 0 {
		t.Fatalf("classifier calls=%d", calls)
	}
	if routes := h.toolRouter.LegacyRouteCalls(); routes != 0 {
		t.Fatalf("short continuation entered the name router: %d", routes)
	}
	have := turnDirectoryHave(set.Tools)
	if !have["bash"] || !have["ssh"] {
		t.Fatalf("carried tools = %v", turnDirectoryNames(set.Tools))
	}
	for _, extra := range []string{"craft_tool", "manage_skill", "web_search", "tools_search", "read_file", "write_file"} {
		if have[extra] {
			t.Fatalf("short continuation published %s: %v", extra, turnDirectoryNames(set.Tools))
		}
	}
	if !have["read_tool_result"] {
		t.Fatalf("short continuation dropped the spilled-result reader: %v", turnDirectoryNames(set.Tools))
	}
}

func TestShortNewInstructionReentersModelDiscovery(t *testing.T) {
	const userID = "desktop-user:api2"
	const text = "更新api2服务器上的omnirouter 到最新版本。"
	if n := utf8.RuneCountInString(text); n == 0 || n > 40 {
		t.Fatalf("fixture must sit inside the old 40-rune gate, got %d", n)
	}
	if parentContinuationDefersDiscovery(text) {
		t.Fatal("a server update names new work")
	}
	for _, cue := range []string{
		"继续", "请继续", "请帮我继续", "接着说", "继续说一下", "继续往下", "往下说", "下一步",
		"重试", "重试一下", "再说一下", "用这个", "用这个吧", "用这个继续", "请用这个",
		"按这个做一下", "好的，用这个", "好的，继续", "继续吗", "继续吗？", "用这个吗",
		"好的，继续吗", "继续了，说", "继续，请", "continue please",
		"继续\n\n" + agent.FilePathPromptPrefix + "\nC:\\slides\\pic.png\n" + agent.AttachedImageReadHint,
	} {
		if !parentContinuationDefersDiscovery(cue) {
			t.Fatalf("%q must keep the open list", cue)
		}
	}
	for _, fresh := range []string{
		"帮我", "请", "麻烦你", "please", "pls", "好的", "可以", "行",
		"继续做配图版", "好的，继续做配图版", "用这个方案", "用这个方案吗", "下一步把服务升到最新",
		"继续了解", "继续更新", "升级吗", "了继续",
		"继续，请更新服务器", "continue please update the server",
		text + "\n\n" + agent.FilePathPromptPrefix + "\nC:\\slides\\pic.png",
		"帮我更新api2服务器上的omnirouter 到最新版本。",
		"好的，更新api2服务器上的omnirouter 到最新版本。",
	} {
		if parentContinuationDefersDiscovery(fresh) {
			t.Fatalf("%q names a new request or is only courtesy", fresh)
		}
	}
	h := &IMMessageHandler{
		registry: NewToolRegistry(),
		unifiedClassifier: intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
			t.Fatal("classifier ran")
			return "", nil
		}}),
	}
	registerBuiltinTools(h.registry, h)
	h.noteParentExecution(userID, true, []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_save_text"}},
	})
	if !h.parentExecutionIsFull(userID) {
		t.Fatal("a knowledge write is still a parent carry for a real continuation")
	}
	for _, ack := range []string{"可以", "行", "好的吧", "可以吗", "好的吗"} {
		ackProfile, ackSemantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{Text: ack, UserID: userID}, false, false)
		if ackSemantic != nil {
			t.Fatalf("%q classified: semantic=%+v", ack, ackSemantic)
		}
		ackKept := h.continuationKeepsParentExecution(ackProfile, userID, ack, ackSemantic)
		if ackKept.Reason != shortContinuationReason || !executionKeepsParentToolSurface(ackKept) {
			t.Fatalf("%q profile=%+v", ack, ackKept)
		}
	}
	fileCue := "继续\n\n" + agent.FilePathPromptPrefix + "\nC:\\slides\\pic.png\n" + agent.AttachedImageReadHint
	if utf8.RuneCountInString(fileCue) <= 40 {
		t.Fatalf("file cue must exceed the raw 40-rune gate, got %d", utf8.RuneCountInString(fileCue))
	}
	if hasStructuralFullExecutionSignal(fileCue) {
		t.Fatal("a path inside the file-picker block is host text, not a structural signal")
	}
	typedPath := "看看 C:\\slides\\pic.png"
	if !hasStructuralFullExecutionSignal(typedPath) {
		t.Fatal("a path the user typed is still a structural signal")
	}
	freshWithFile := text + "\n\n" + agent.FilePathPromptPrefix + "\nC:\\slides\\pic.png"
	for _, fresh := range []string{
		text,
		"好的，" + text,
		"帮我" + text,
		"继续吗",
		fileCue,
		freshWithFile,
		typedPath,
	} {
		freshProfile, freshSemantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{Text: fresh, UserID: userID}, false, false)
		kept := h.continuationKeepsParentExecution(freshProfile, userID, fresh, freshSemantic)
		if fresh == "继续吗" || fresh == fileCue {
			if freshSemantic != nil || kept.Reason != shortContinuationReason || !executionKeepsParentToolSurface(kept) {
				t.Fatalf("%q profile=%+v semantic=%+v", fresh, kept, freshSemantic)
			}
			continue
		}
		if freshSemantic == nil || !intent.ClassificationIsModelDiscovery(*freshSemantic) {
			t.Fatalf("%q semantic=%+v profile=%+v", fresh, freshSemantic, freshProfile)
		}
		if kept.Reason != freshProfile.Reason || executionKeepsParentToolSurface(kept) || freshProfile.IsLight() {
			t.Fatalf("%q inherited the knowledge-write surface: %+v", fresh, kept)
		}
	}
	visible := modelDiscoveryVisibleTools(h, nil)
	if !turnDirectoryHave(visible)["tools_search"] || turnDirectoryHave(visible)["knowledge_save_text"] {
		t.Fatalf("discovery directory = %v", turnDirectoryNames(visible))
	}
	follow, followSemantic := h.classifyIMExecutionProfileAndSemantic(IMUserMessage{Text: "继续", UserID: userID}, false, false)
	if followSemantic != nil {
		t.Fatalf("继续 classified: semantic=%+v profile=%+v", followSemantic, follow)
	}
	upgraded := h.continuationKeepsParentExecution(follow, userID, "继续", followSemantic)
	if upgraded.Reason != shortContinuationReason || !executionKeepsParentToolSurface(upgraded) {
		t.Fatalf("继续 profile=%+v", upgraded)
	}
}

func TestDiscoveryWorkToolCarrySurvivesShortFollowUp(t *testing.T) {
	calls := 0
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "跑一段命令", discovery)
	cb.userID = "user-1"
	cb.handler.unifiedClassifier = intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
		calls++
		return `{"top":[{"skill":"web_search","score":0.99}]}`, nil
	}})
	cb.handler.SetToolRouter(NewToolRouter(nil))
	if granted, message := cb.PetitionToolCall("bash"); !granted {
		t.Fatalf("bash petition granted=%v message=%q", granted, message)
	}
	if next := cb.BuildToolsForModelRequest("跑一段命令", 2); !turnDirectoryHave(next)["bash"] {
		t.Fatalf("next request = %v", turnDirectoryNames(next))
	}
	if got := cb.handler.parentExecutionTools("user-1"); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("discovery carry = %v, want [bash]", got)
	}
	follow, semantic := cb.handler.classifyIMExecutionProfileAndSemantic(IMUserMessage{Text: "继续", UserID: "user-1"}, false, false)
	if calls != 0 || semantic != nil {
		t.Fatalf("short follow-up classified: calls=%d semantic=%+v profile=%+v", calls, semantic, follow)
	}
	upgraded := cb.handler.continuationKeepsParentExecution(follow, "user-1", "继续", semantic)
	if upgraded.Reason != shortContinuationReason {
		t.Fatalf("follow-up profile = %+v", upgraded)
	}
	ctx := NewLoopContext("chat", 3, nil)
	ctx.Runtime.Execution = upgraded
	set := cb.handler.prepareAgentLoopTools("user-1", "继续", ctx, agentLoopPhase{})
	if calls != 0 || cb.handler.toolRouter.LegacyRouteCalls() != 0 {
		t.Fatalf("ranking ran: calls=%d routes=%d tools=%v", calls, cb.handler.toolRouter.LegacyRouteCalls(), turnDirectoryNames(set.Tools))
	}
	have := turnDirectoryHave(set.Tools)
	if !have["bash"] || have["web_search"] || have["tools_search"] || have["craft_tool"] {
		t.Fatalf("published = %v", turnDirectoryNames(set.Tools))
	}
}

func TestDiscoveryExclusiveNeedsLoadOnlyAlone(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "查资料并处理文件", discovery)
	cb.userID = "user-exclusive"
	cb.admitDiscoverySearchNeeds([]string{"web_search", "bash", "mailbox_send", "ssh", "browser", "delete_file", "record_audio", "computer_use"})
	next := cb.BuildToolsForModelRequest("查资料", 2)
	have := turnDirectoryHave(next)
	for _, name := range []string{"web_search", "bash", "mailbox_send"} {
		if !have[name] {
			t.Fatalf("workspace name %s missing: %v", name, turnDirectoryNames(next))
		}
	}
	for _, name := range []string{"ssh", "browser", "delete_file", "record_audio", "computer_use"} {
		if have[name] {
			t.Fatalf("exclusive name %s loaded from a mixed list: %v", name, turnDirectoryNames(next))
		}
	}

	_, _, sole := turnDirectoryPlannedSurface(t, "连上那台机器", discovery)
	sole.admitDiscoverySearchNeeds([]string{"ssh"})
	sshNext := sole.BuildToolsForModelRequest("连上", 2)
	if !turnDirectoryHave(sshNext)["ssh"] {
		t.Fatalf("sole ssh did not load: %v", turnDirectoryNames(sshNext))
	}

	_, _, desktop := turnDirectoryPlannedSurface(t, "操作桌面", discovery)
	desktop.admitDiscoverySearchNeeds([]string{"computer_use"})
	cuNext := desktop.BuildToolsForModelRequest("操作", 2)
	if turnDirectoryHave(cuNext)["computer_use"] {
		t.Fatalf("main loop loaded computer_use: %v", turnDirectoryNames(cuNext))
	}
}

func TestDiscoveryMainLoopDeniesComputerUsePetition(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "打开记事本", discovery)
	if !discoveryComputerUsePetitionBlocked(cb) {
		t.Fatal("an unsettled main loop must not petition computer_use")
	}
	if granted, message := cb.PetitionToolCall("computer_use"); granted || message != "" {
		t.Fatalf("main loop granted computer_use: granted=%v message=%q", granted, message)
	}
	helper := NewLoopContext("cu-helper", 1, nil)
	helper.ComputerHelper = true
	helper.ComputerUseGateSettled = true
	helper.ComputerUseActive = true
	cb.loopCtx = helper
	if discoveryComputerUsePetitionBlocked(cb) {
		t.Fatal("an open computer helper must be able to petition computer_use")
	}
	executor := NewLoopContext("cu-executor", 1, nil)
	executor.HorizonRole = longhorizon.RoleGUIExecutor
	executor.ComputerUseGateSettled = true
	executor.ComputerUseActive = true
	cb.loopCtx = executor
	if discoveryComputerUsePetitionBlocked(cb) {
		t.Fatal("an open gui executor must be able to petition computer_use")
	}
	closed := NewLoopContext("cu-helper-closed", 1, nil)
	closed.ComputerHelper = true
	closed.ComputerUseGateSettled = true
	cb.loopCtx = closed
	if !discoveryComputerUsePetitionBlocked(cb) {
		t.Fatal("a closed helper gate must deny computer_use")
	}
	if granted, message := cb.PetitionToolCall("computer_use"); granted || message != "" {
		t.Fatalf("closed helper gate granted computer_use: granted=%v message=%q", granted, message)
	}
}

func TestDiscoverySendFileWithoutTrustedDocumentIsNotACatalogMiss(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "生成老鼠戏猫图", discovery)
	cb.userText = "生成老鼠戏猫图"
	got := semanticToolsSearchRun(cb, `{"query":"生成老鼠戏猫图","needs":["send_file"]}`)
	if cb.discoveryUnmatchedObligation || (cb.loopCtx != nil && cb.loopCtx.discoveryCatalogMiss()) {
		t.Fatalf("trusted-input bind opened a catalog miss:\n%s", got)
	}
	if strings.Contains(got, semanticToolsSearchDiscoveryNextStep) || strings.Contains(got, semanticToolsSearchDiscoveryUnloaded) || strings.Contains(got, discoveryUnmatchedExtensionMessage) {
		t.Fatalf("page sent the model to install a skill:\n%s", got)
	}
	// A picture turn refuses generate_pdf and office. The last line used to
	// say to add a document producer anyway, and the next needs list opened
	// the catalog miss. The action names a producer only when this admission
	// layer would load it.
	pictureTail := "send_file 在目录里。当前计划没有可绑定的受信任文档，这一步不会加载。不要调用 search_and_install_skill 或 craft_tool。"
	producerTail := "再调用 tools_search，needs 里只写 generate_pdf 或 office。加载成功后，再把 send_file 放进 needs。不要调用 search_and_install_skill 或 craft_tool。"
	if !strings.HasSuffix(strings.TrimSpace(got), pictureTail) || strings.Contains(pictureTail, "generate_pdf") || strings.Contains(pictureTail, "文档生产者") {
		t.Fatalf("page=\n%s", got)
	}
	if status := semanticToolsSearchStatus(cb, "send_file"); status != "[已在目录：当前计划没有可绑定的受信任文档]" {
		t.Fatalf("status=%s", status)
	}
	if turnDirectoryHave(cb.BuildToolsForModelRequest("生成老鼠戏猫图", 2))["send_file"] {
		t.Fatal("unbound send_file was published")
	}
	again := semanticToolsSearchRun(cb, `{"query":"生成老鼠戏猫图","needs":["send_file"]}`)
	if cb.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(again), pictureTail) {
		t.Fatalf("repeat opened a miss:\n%s", again)
	}
	viaRun := cb.runSemanticToolsSearch(`{"query":"生成老鼠戏猫图","needs":["send_file"]}`)
	if cb.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(viaRun), pictureTail) {
		t.Fatalf("production search:\n%s", viaRun)
	}
	closed := cb.runSemanticToolsSearch(`{"query":"生成老鼠戏猫图","needs":["send_file"]}`)
	if cb.discoveryUnmatchedObligation || closed != semanticToolsSearchDiscoveryRepeatExhausted || strings.Contains(closed, semanticToolsSearchDiscoveryNextStep) {
		t.Fatalf("repeat closer=\n%s", closed)
	}

	_, _, workspace := turnDirectoryPlannedSurface(t, "把结果发出去", discovery)
	workspace.userText = "把结果发出去"
	unbound := semanticToolsSearchRun(workspace, `{"query":"把结果发出去","needs":["send_file"]}`)
	if workspace.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(unbound), producerTail) || !strings.Contains(unbound, "send_file 在目录里。当前计划没有可绑定的受信任文档") {
		t.Fatalf("unbound page=\n%s", unbound)
	}
	loaded := semanticToolsSearchRun(workspace, `{"query":"把结果发出去","needs":["web_search"]}`)
	if workspace.discoveryUnmatchedObligation || !turnDirectoryHave(workspace.BuildToolsForModelRequest("把结果发出去", 3))["web_search"] {
		t.Fatalf("web_search after unbound send_file:\n%s", loaded)
	}
	_, _, mixed := turnDirectoryPlannedSurface(t, "把结果发出去", discovery)
	mixed.userText = "把结果发出去"
	mixedPage := semanticToolsSearchRun(mixed, `{"query":"把结果发出去","needs":["send_file","web_search"]}`)
	if mixed.discoveryUnmatchedObligation || !turnDirectoryHave(mixed.BuildToolsForModelRequest("把结果发出去", 2))["web_search"] || turnDirectoryHave(mixed.BuildToolsForModelRequest("把结果发出去", 3))["send_file"] {
		t.Fatalf("mixed needs:\n%s", mixedPage)
	}
	if strings.HasSuffix(strings.TrimSpace(mixedPage), producerTail) || !strings.Contains(mixedPage, "没有可绑定的受信任文档") {
		t.Fatalf("mixed page hid the workspace action or the bind result:\n%s", mixedPage)
	}
	_, _, produced := turnDirectoryPlannedSurface(t, "写一份说明", discovery)
	produced.userText = "写一份说明"
	before := semanticToolsSearchRun(produced, `{"query":"写一份说明","needs":["send_file"]}`)
	if produced.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(before), producerTail) || !strings.Contains(before, "send_file 在目录里。当前计划没有可绑定的受信任文档") {
		t.Fatalf("send_file before a producer:\n%s", before)
	}
	withPDF := semanticToolsSearchRun(produced, `{"query":"写一份说明","needs":["generate_pdf"]}`)
	if produced.discoveryUnmatchedObligation || !turnDirectoryHave(produced.BuildToolsForModelRequest("写一份说明", 4))["generate_pdf"] || !strings.HasSuffix(strings.TrimSpace(withPDF), semanticToolsSearchListedDirectivePrefix+"generate_pdf。不要再查询这些名字。") {
		t.Fatalf("generate_pdf after an unbound send_file:\n%s", withPDF)
	}
	after := semanticToolsSearchRun(produced, `{"query":"写一份说明","needs":["send_file"]}`)
	// The producer petition already plans send_file. That search must not
	// issue it: the file does not exist until generate_pdf runs, and the
	// last line is that call.
	if produced.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(after), "现在调用 generate_pdf。") || turnDirectoryHave(produced.BuildToolsForModelRequest("写一份说明", 5))["send_file"] {
		t.Fatalf("send_file stayed unbound after generate_pdf:\n%s", after)
	}
	if !strings.Contains(semanticToolsSearchStatus(produced, "send_file"), "已列入本轮计划") || !strings.Contains(semanticToolsSearchStatus(produced, "send_file"), "先调用 generate_pdf") {
		t.Fatalf("send_file status after generate_pdf=%s\n%s", semanticToolsSearchStatus(produced, "send_file"), after)
	}
	miss := semanticToolsSearchRun(cb, `{"query":"生成老鼠戏猫图","needs":["not_a_catalog_tool"]}`)
	if !cb.discoveryUnmatchedObligation || !strings.Contains(miss, semanticToolsSearchDiscoveryNextStep) {
		t.Fatalf("unknown name must still miss:\n%s", miss)
	}
	_, _, followed := turnDirectoryPlannedSurface(t, "生成老鼠戏猫图", discovery)
	followed.userText = "生成老鼠戏猫图"
	if page := semanticToolsSearchRun(followed, `{"query":"生成老鼠戏猫图","needs":["send_file"]}`); followed.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(page), pictureTail) {
		t.Fatalf("bind before a producer petition:\n%s", page)
	}
	producer := semanticToolsSearchRun(followed, `{"query":"生成老鼠戏猫图","needs":["generate_pdf"]}`)
	if !followed.discoveryUnmatchedObligation || !strings.Contains(producer, semanticToolsSearchDiscoveryNextStep) || turnDirectoryHave(followed.BuildToolsForModelRequest("生成老鼠戏猫图", 2))["generate_pdf"] {
		t.Fatalf("picture producer petition must still miss:\n%s", producer)
	}
	_, _, reader := turnDirectoryPlannedSurface(t, "读一下附件", discovery)
	reader.userText = "读一下附件"
	readPage := semanticToolsSearchRun(reader, `{"query":"读一下附件","needs":["read_attachment"]}`)
	readTail := "read_attachment 在目录里。当前计划没有可绑定的受信任文档，这一步不会加载。不要调用 search_and_install_skill 或 craft_tool。"
	if reader.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(readPage), readTail) || strings.Contains(readTail, "generate_pdf") || turnDirectoryHave(reader.BuildToolsForModelRequest("读一下附件", 2))["read_attachment"] {
		t.Fatalf("read without a document:\n%s", readPage)
	}
	for _, needs := range []string{`["read_attachment","send_file"]`, `["send_file","read_attachment"]`} {
		_, _, both := turnDirectoryPlannedSurface(t, "写一份说明", discovery)
		both.userText = "写一份说明"
		page := semanticToolsSearchRun(both, `{"query":"写一份说明","needs":`+needs+`}`)
		have := turnDirectoryHave(both.BuildToolsForModelRequest("写一份说明", 2))
		if both.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(page), producerTail) || !strings.Contains(page, "read_attachment 在目录里") || have["send_file"] || have["read_attachment"] {
			t.Fatalf("needs %s:\n%s", needs, page)
		}
	}
	_, _, officeTurn := turnDirectoryPlannedSurface(t, "写一份说明", discovery)
	officeTurn.userText = "写一份说明"
	officePage := semanticToolsSearchRun(officeTurn, `{"query":"写一份说明","needs":["office"]}`)
	if officeTurn.discoveryUnmatchedObligation || !turnDirectoryHave(officeTurn.BuildToolsForModelRequest("写一份说明", 2))["office"] || !strings.HasSuffix(strings.TrimSpace(officePage), semanticToolsSearchListedDirectivePrefix+"office。不要再查询这些名字。") {
		t.Fatalf("office petition:\n%s", officePage)
	}
	officeSend := semanticToolsSearchRun(officeTurn, `{"query":"写一份说明","needs":["send_file"]}`)
	if officeTurn.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(officeSend), "现在调用 office。") || turnDirectoryHave(officeTurn.BuildToolsForModelRequest("写一份说明", 3))["send_file"] {
		t.Fatalf("send_file stayed unbound after office:\n%s", officeSend)
	}
	if !strings.Contains(semanticToolsSearchStatus(officeTurn, "send_file"), "已列入本轮计划") || !strings.Contains(semanticToolsSearchStatus(officeTurn, "send_file"), "先调用 office") {
		t.Fatalf("send_file status after office=%s\n%s", semanticToolsSearchStatus(officeTurn, "send_file"), officeSend)
	}

	// The follow-up's last line orders generate_pdf or office after send_file
	// failed to bind. A lookup already in the parent plan must not make that
	// ordered petition alter parent authority and open a catalog miss.
	for _, producer := range []string{"generate_pdf", "office"} {
		_, _, afterLookup := turnDirectoryPlannedSurface(t, "写一份说明", discovery)
		afterLookup.userText = "写一份说明"
		searched := semanticToolsSearchRun(afterLookup, `{"query":"写一份说明","needs":["web_search"]}`)
		if afterLookup.discoveryUnmatchedObligation || !turnDirectoryHave(afterLookup.BuildToolsForModelRequest("写一份说明", 2))["web_search"] {
			t.Fatalf("web_search before %s:\n%s", producer, searched)
		}
		// A spent lookup wave appends a continuation the producer template
		// does not mint. The follow-up still orders that producer.
		beforeWave := len(afterLookup.semanticSurface.plan.Selections)
		for i := 0; i < 5; i++ {
			if got := afterLookup.ExecuteTool("web_search", fmt.Sprintf(`{"query":"说明 %d"}`, i)); !strings.Contains(got, "found:") {
				t.Fatalf("web_search %d before %s: %s", i, producer, got)
			}
		}
		if len(afterLookup.semanticSurface.plan.Selections) <= beforeWave {
			t.Fatalf("spent wave did not append a continuation before %s: before=%d after=%d", producer, beforeWave, len(afterLookup.semanticSurface.plan.Selections))
		}
		stillUnbound := semanticToolsSearchRun(afterLookup, `{"query":"写一份说明","needs":["send_file"]}`)
		if afterLookup.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(stillUnbound), producerTail) {
			t.Fatalf("send_file after web_search:\n%s", stillUnbound)
		}
		loadedProducer := semanticToolsSearchRun(afterLookup, `{"query":"写一份说明","needs":["`+producer+`"]}`)
		loadedHave := turnDirectoryHave(afterLookup.BuildToolsForModelRequest("写一份说明", 4))
		// The continuation lookup stays granted. The last line names the
		// producer, so the model does not spend the next call on that lookup.
		if afterLookup.discoveryUnmatchedObligation || strings.Contains(loadedProducer, semanticToolsSearchDiscoveryNextStep) || !loadedHave[producer] || !loadedHave["web_search"] || !strings.HasSuffix(strings.TrimSpace(loadedProducer), semanticToolsSearchListedDirectivePrefix+producer+"。不要再查询这些名字。") {
			t.Fatalf("%s after web_search tools=%v\n%s", producer, loadedHave, loadedProducer)
		}
		delivered := semanticToolsSearchRun(afterLookup, `{"query":"写一份说明","needs":["send_file"]}`)
		if afterLookup.discoveryUnmatchedObligation || strings.Contains(delivered, semanticToolsSearchDiscoveryNextStep) || !strings.HasSuffix(strings.TrimSpace(delivered), "现在调用 "+producer+"。") || turnDirectoryHave(afterLookup.BuildToolsForModelRequest("写一份说明", 5))["send_file"] {
			t.Fatalf("send_file after web_search and %s:\n%s", producer, delivered)
		}
		if !strings.Contains(semanticToolsSearchStatus(afterLookup, "send_file"), "已列入本轮计划") || !strings.Contains(semanticToolsSearchStatus(afterLookup, "send_file"), "先调用 "+producer) {
			t.Fatalf("send_file status after web_search and %s=%s\n%s", producer, semanticToolsSearchStatus(afterLookup, "send_file"), delivered)
		}
	}
}

// The follow-up's send_file page says the tool appears after the producer
// succeeds. Classified office and PDF turns already publish the artifact and
// attach it. This is the discovery petition the page orders: the write has
// to release send_file. Calling that name, or stopping once it is listed,
// has to put the producer's own file on the desktop card and accept the
// delivery. Listing the name is not the attachment.
func TestDiscoveryProducerWriteReleasesSendFile(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := app.SetTabWorkingDir("", workspace); err != nil {
		t.Fatalf("SetTabWorkingDir: %v", err)
	}
	discovery := intent.ModelDiscoveryClassification()
	producers := []struct {
		name   string
		args   string
		marker string
		mime   string
		file   string
	}{
		{name: "office", args: `{"path":"说明.pptx","title":"说明","slides":[{"title":"封面","bullets":["正文"]}]}`, marker: "Office document artifact published", mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation", file: "说明.pptx"},
		{name: "generate_pdf", args: `{"content":"# 说明\n这是一份说明的正文。","title":"说明"}`, marker: "PDF artifact published", mime: "application/pdf", file: "说明.pdf"},
	}
	for _, producer := range producers {
		for _, modelStops := range []bool{false, true} {
			suffix := "call"
			if modelStops {
				suffix = "stop"
			}
			h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, discovery.Primary), app: app}
			registerBuiltinTools(h.registry, h)
			stored := discovery
			_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
				desktopUserID, "写一份说明", "desktop", "root-discovery-"+producer.name+"-"+suffix, "turn-discovery-"+producer.name+"-"+suffix, &stored,
			)
			if err != nil || !handled || surface == nil {
				t.Fatalf("%s %s surface handled=%v err=%v", producer.name, suffix, handled, err)
			}
			target := hostOwnedCurrentChannelDeliveryTarget("desktop", desktopUserID)
			if target == nil || target.ChannelScope != "desktop" || target.DestinationID != "user:"+desktopUserID {
				t.Fatalf("desktop delivery target = %+v", target)
			}
			cb := &sharedAgentLoopCallbacks{
				handler: h, semanticSurface: surface, platform: "desktop", userID: desktopUserID, userText: "写一份说明",
				loopCtx: &LoopContext{Runtime: RuntimeContext{SemanticIntent: &stored}, DeliveryTarget: target},
			}
			unbound := semanticToolsSearchRun(cb, `{"query":"写一份说明","needs":["send_file"]}`)
			producerTail := "再调用 tools_search，needs 里只写 generate_pdf 或 office。加载成功后，再把 send_file 放进 needs。不要调用 search_and_install_skill 或 craft_tool。"
			if cb.discoveryUnmatchedObligation || !strings.HasSuffix(strings.TrimSpace(unbound), producerTail) {
				t.Fatalf("%s %s unbound page=\n%s", producer.name, suffix, unbound)
			}
			page := semanticToolsSearchRun(cb, `{"query":"写一份说明","needs":["`+producer.name+`"]}`)
			if cb.discoveryUnmatchedObligation || !turnDirectoryHave(cb.BuildToolsForModelRequest("写一份说明", 2))[producer.name] {
				t.Fatalf("%s %s petition:\n%s", producer.name, suffix, page)
			}
			if turnDirectoryHave(cb.BuildToolsForModelRequest("写一份说明", 3))["send_file"] {
				t.Fatalf("send_file was issued before %s wrote the document", producer.name)
			}
			got := cb.ExecuteToolCall(producer.name, producer.args, "call-"+producer.name+"-"+suffix).Result
			if !strings.Contains(got, producer.marker) {
				t.Fatalf("%s %s result=%q", producer.name, suffix, got)
			}
			if !turnDirectoryHave(cb.BuildToolsForModelRequest("写一份说明", 4))["send_file"] {
				t.Fatalf("send_file missing after %s %s: %v\n%s", producer.name, suffix, turnDirectoryNames(cb.BuildToolsForModelRequest("写一份说明", 5)), got)
			}
			deliveryName, deliveryGrant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "semantic_deliver_current_file")
			if deliveryName != "send_file" || !currentChannelFileDeliveryReady(cb.semanticSurface, deliveryGrant) {
				t.Fatalf("%s %s delivery is not ready for the desktop card: name=%q", producer.name, suffix, deliveryName)
			}
			resp := &IMAgentResponse{}
			if modelStops {
				attachSharedLoopArtifacts(resp, cb)
			} else {
				delivered := cb.ExecuteToolCall("send_file", `{}`, "call-send-"+producer.name).Result
				if !agent.CurrentChannelAttachCommitted(delivered) {
					t.Fatalf("%s deliver=%q", producer.name, delivered)
				}
				if cb.semanticDeliveryFileData == "" || cb.semanticDeliveryFileMIME != producer.mime {
					t.Fatalf("%s attached mime=%q result=%q", producer.name, cb.semanticDeliveryFileMIME, delivered)
				}
				attachSharedLoopArtifacts(resp, cb)
			}
			rel, relErr := filepath.Rel(workspace, resp.LocalFilePath)
			info, statErr := os.Stat(resp.LocalFilePath)
			if resp.FileData != "" || resp.FileMimeType != producer.mime || filepath.Base(resp.LocalFilePath) != producer.file || relErr != nil || strings.HasPrefix(rel, "..") || statErr != nil || info.Size() == 0 {
				t.Fatalf("%s %s desktop card path=%q mime=%q data=%t rel=%q stat=%v", producer.name, suffix, resp.LocalFilePath, resp.FileMimeType, resp.FileData != "", rel, statErr)
			}
			if cb.semanticDelivery == nil || surface.artifacts == nil || surface.artifacts.store == nil {
				t.Fatalf("%s %s delivery projection missing", producer.name, suffix)
			}
			record, err := surface.artifacts.store.Delivery(cb.semanticDelivery.Scope, cb.semanticDelivery.SelectionID)
			if err != nil || record.State != tool.DeliveryAccepted {
				t.Fatalf("%s %s receipt=%+v err=%v", producer.name, suffix, record.State, err)
			}
			if name, _ := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "semantic_deliver_current_file"); name != "" {
				t.Fatalf("%s %s send_file stayed live after the card", producer.name, suffix)
			}
		}
	}
}

func TestDiscoveryComputerUseNeedsDoesNotCatalogMiss(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "打开记事本", discovery)
	// A planned PDF already wants web_search called now. That imperative
	// belongs to the plan, not to a search that only asked for desktop control.
	if cb.semanticSurface.completed == nil {
		cb.semanticSurface.completed = map[string]bool{}
	}
	if cb.semanticSurface.grants == nil {
		cb.semanticSurface.grants = map[string]tool.InvocationGrant{}
	}
	selectionsBefore := append([]tool.PlannedSelection(nil), cb.semanticSurface.plan.Selections...)
	searchGrant, hadSearchGrant := cb.semanticSurface.grants["web_search"]
	renderedSearch := false
	if cb.semanticSurface.rendered != nil {
		renderedSearch = cb.semanticSurface.rendered["web_search"]
	}
	cb.semanticSurface.grants["web_search"] = tool.InvocationGrant{SelectionID: "search", Token: "live"}
	if cb.semanticSurface.rendered == nil {
		cb.semanticSurface.rendered = map[string]bool{}
	}
	cb.semanticSurface.rendered["web_search"] = true
	cb.semanticSurface.plan.Selections = append(cb.semanticSurface.plan.Selections,
		tool.PlannedSelection{ID: "search", AdapterName: "web_search", FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		tool.PlannedSelection{ID: "pdf", AdapterName: "generate_pdf", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}, Requires: []string{"search"}},
	)
	if status := semanticToolsSearchStatus(cb, "generate_pdf"); !strings.Contains(status, "已列入本轮计划") || !strings.Contains(status, "先调用 web_search") {
		t.Fatalf("fixture did not name web_search as the call: %s", status)
	}
	got := semanticToolsSearchRun(cb, `{"query":"打开记事本","needs":["computer_use"]}`)
	if cb.discoveryUnmatchedObligation || (cb.loopCtx != nil && cb.loopCtx.discoveryCatalogMiss()) {
		t.Fatalf("desktop-control needs opened a catalog miss:\n%s", got)
	}
	if strings.Contains(got, "needs filter matched nothing") || !strings.HasSuffix(strings.TrimSpace(got), discoveryMainLoopComputerUseNote) {
		t.Fatalf("page=\n%s", got)
	}
	if strings.Contains(got, semanticToolsSearchDiscoveryLoadLine) || strings.Contains(got, "现在调用") || strings.Contains(got, "先调用") {
		t.Fatalf("desktop door still invites another petition:\n%s", got)
	}
	// The stand-in grant only lets the plan name web_search. It does not
	// match the issued plan, and leaving it makes the renderer reject the
	// whole surface. Put the original plan back before checking what loads.
	cb.semanticSurface.plan.Selections = selectionsBefore
	if hadSearchGrant {
		cb.semanticSurface.grants["web_search"] = searchGrant
	} else {
		delete(cb.semanticSurface.grants, "web_search")
	}
	if renderedSearch {
		cb.semanticSurface.rendered["web_search"] = true
	} else {
		delete(cb.semanticSurface.rendered, "web_search")
	}
	if turnDirectoryHave(cb.BuildToolsForModelRequest("打开记事本", 2))["computer_use"] {
		t.Fatal("main loop loaded computer_use")
	}
	loaded := semanticToolsSearchRun(cb, `{"query":"查资料","needs":["web_search"]}`)
	if cb.discoveryUnmatchedObligation || !turnDirectoryHave(cb.BuildToolsForModelRequest("查资料", 3))["web_search"] {
		t.Fatalf("web_search after computer_use:\n%s", loaded)
	}

	_, _, mixed := turnDirectoryPlannedSurface(t, "查资料", discovery)
	mixedPage := semanticToolsSearchRun(mixed, `{"query":"查资料","needs":["web_search","computer_use"]}`)
	if !turnDirectoryHave(mixed.BuildToolsForModelRequest("查资料", 2))["web_search"] || turnDirectoryHave(mixed.BuildToolsForModelRequest("查资料", 3))["computer_use"] {
		t.Fatalf("mixed needs:\n%s", mixedPage)
	}
	if strings.HasSuffix(strings.TrimSpace(mixedPage), discoveryMainLoopComputerUseNote) {
		t.Fatalf("mixed page hid the workspace action:\n%s", mixedPage)
	}
}

// discoveryListedWebSearchPlan installs a listed web_search grant that a
// planned PDF is waiting on. A search that does not load its own names must
// not keep 「现在调用 web_search」 as the page action.
func discoveryListedWebSearchPlan(t *testing.T, text string) *sharedAgentLoopCallbacks {
	t.Helper()
	_, _, cb := turnDirectoryPlannedSurface(t, text, intent.ModelDiscoveryClassification())
	if cb.semanticSurface.completed == nil {
		cb.semanticSurface.completed = map[string]bool{}
	}
	if cb.semanticSurface.grants == nil {
		cb.semanticSurface.grants = map[string]tool.InvocationGrant{}
	}
	cb.semanticSurface.grants["web_search"] = tool.InvocationGrant{SelectionID: "search", Token: "live"}
	if cb.semanticSurface.rendered == nil {
		cb.semanticSurface.rendered = map[string]bool{}
	}
	cb.semanticSurface.rendered["web_search"] = true
	cb.semanticSurface.plan.Selections = append(cb.semanticSurface.plan.Selections,
		tool.PlannedSelection{ID: "search", AdapterName: "web_search", FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		tool.PlannedSelection{ID: "pdf", AdapterName: "generate_pdf", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}, Requires: []string{"search"}},
	)
	if status := semanticToolsSearchStatus(cb, "generate_pdf"); !strings.Contains(status, "已列入本轮计划") || !strings.Contains(status, "先调用 web_search") {
		t.Fatalf("fixture did not name web_search as the call: %s", status)
	}
	return cb
}

func discoveryPlanWantsWebSearchNow(t *testing.T, text string) *sharedAgentLoopCallbacks {
	t.Helper()
	cb := discoveryListedWebSearchPlan(t, text)
	if cb.semanticSurface.retiredGrants == nil {
		cb.semanticSurface.retiredGrants = map[string]tool.InvocationGrant{}
	}
	// Spent means this petition cannot load. The directory still knows the name.
	cb.semanticSurface.retiredGrants["download_file"] = tool.InvocationGrant{AdapterName: "download_file", SelectionID: "spent-download"}
	return cb
}

func TestDiscoveryUnloadedNeedsDoesNotKeepPlanCall(t *testing.T) {
	spent := discoveryPlanWantsWebSearchNow(t, "下载这张图")
	got := semanticToolsSearchRun(spent, `{"query":"下载这张图","needs":["download_file"]}`)
	if !spent.discoveryUnmatchedObligation || spent.loopCtx == nil || !spent.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("a needs list that loaded nothing left the turn open:\n%s", got)
	}
	if strings.Contains(got, "现在调用") || strings.Contains(got, "先调用 web_search") || !strings.HasSuffix(strings.TrimSpace(got), semanticToolsSearchDiscoveryNextStep) {
		t.Fatalf("failed load kept the plan call:\n%s", got)
	}

	mixed := discoveryPlanWantsWebSearchNow(t, "打开记事本并下载")
	mixedPage := semanticToolsSearchRun(mixed, `{"query":"打开记事本并下载","needs":["download_file","computer_use"]}`)
	if !mixed.discoveryUnmatchedObligation || mixed.loopCtx == nil || !mixed.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("failed sibling did not close the turn:\n%s", mixedPage)
	}
	trimmed := strings.TrimSpace(mixedPage)
	if strings.Contains(mixedPage, "现在调用") || strings.Contains(mixedPage, "先调用 web_search") || !strings.Contains(mixedPage, discoveryMainLoopComputerUseNote) || strings.HasSuffix(trimmed, discoveryMainLoopComputerUseNote) || !strings.HasSuffix(trimmed, semanticToolsSearchDiscoveryNextStep) {
		t.Fatalf("desktop refusal hid the failed load:\n%s", mixedPage)
	}
}

// TestDiscoveryPlannedGenerateIsNotACatalogMiss is a document step the plan
// already holds. PetitionToolCall returns granted=false while generate_pdf
// waits on web_search, and again while a finished search still holds the
// PDF for the next request. That wait is not a failed load. Treating it as
// one closed generate_pdf and dispatch for the rest of the turn.
func TestDiscoveryPlannedGenerateIsNotACatalogMiss(t *testing.T) {
	for _, hold := range []bool{false, true} {
		cb := discoveryScheduledGenerate(t, "写一份说明", hold)
		got := cb.runSemanticToolsSearch(`{"query":"写一份说明","needs":["generate_pdf"]}`)
		if cb.discoveryUnmatchedObligation || cb.loopCtx.discoveryCatalogMiss() {
			t.Fatalf("hold=%v planned generate became a miss:\n%s", hold, got)
		}
		for _, banned := range []string{"没有加载", "不在本回合目录里", discoveryUnmatchedExtensionMessage, discoveryUnmatchedSubstituteMessage} {
			if strings.Contains(got, banned) {
				t.Fatalf("hold=%v page says %q:\n%s", hold, banned, got)
			}
		}
		if allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"write the pdf"}`); !allowed || reason != "" {
			t.Fatalf("hold=%v dispatch allowed=%v reason=%q", hold, allowed, reason)
		}
		if !hold && !strings.Contains(got, "现在调用 web_search") {
			t.Fatalf("waiting generate dropped the predecessor:\n%s", got)
		}
		if hold && !strings.Contains(got, "下一请求出现：generate_pdf") {
			t.Fatalf("held generate dropped the next request:\n%s", got)
		}
	}
}

// TestDiscoveryPlannedGenerateSurvivesAFailedSibling is a document step the
// plan already holds, named beside a download the plan never receives.
// The download is a failed load. The PDF is not. Closing the turn there
// refused generate_pdf and told the model to install a skill.
func TestDiscoveryPlannedGenerateSurvivesAFailedSibling(t *testing.T) {
	cb := discoveryScheduledGenerate(t, "写一份说明", false)
	cb.semanticPetitionDenied = map[string]struct{}{"download_file": {}}
	got := cb.runSemanticToolsSearch(`{"query":"写一份说明","needs":["generate_pdf","download_file"]}`)
	if cb.discoveryUnmatchedObligation || cb.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("planned generate beside a failed download became a miss:\n%s", got)
	}
	for _, banned := range []string{"没有加载", discoveryUnmatchedExtensionMessage, discoveryUnmatchedSubstituteMessage} {
		if strings.Contains(got, banned) {
			t.Fatalf("page closed the planned generate with %q:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, "现在调用 web_search") {
		t.Fatalf("predecessor call dropped:\n%s", got)
	}
	if _, ok := cb.semanticSurface.grants["download_file"]; ok {
		t.Fatal("failed download was granted")
	}
	if allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"write the pdf"}`); !allowed || reason != "" {
		t.Fatalf("dispatch allowed=%v reason=%q", allowed, reason)
	}
}

// TestDiscoveryLiveHostGenerateIsNotACatalogMiss is the same wait when the
// plan selected the host adapter. generate_pdf is that row's model spelling.
// The selection is still held, so the predecessor call stays and dispatch
// stays available.
func TestDiscoveryLiveHostGenerateIsNotACatalogMiss(t *testing.T) {
	for _, hold := range []bool{false, true} {
		cb := discoveryScheduledGenerateAdapter(t, "写一份说明", hold, "host_document_generate_file")
		got := cb.runSemanticToolsSearch(`{"query":"写一份说明","needs":["generate_pdf"]}`)
		if cb.discoveryUnmatchedObligation || cb.loopCtx.discoveryCatalogMiss() {
			t.Fatalf("hold=%v live host generate became a miss:\n%s", hold, got)
		}
		for _, banned := range []string{"没有加载", "不在本回合目录里", discoveryUnmatchedExtensionMessage, discoveryUnmatchedSubstituteMessage} {
			if strings.Contains(got, banned) {
				t.Fatalf("hold=%v page says %q:\n%s", hold, banned, got)
			}
		}
		if allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"write the pdf"}`); !allowed || reason != "" {
			t.Fatalf("hold=%v dispatch allowed=%v reason=%q", hold, allowed, reason)
		}
		if !hold && !strings.Contains(got, "现在调用 web_search") {
			t.Fatalf("waiting host generate dropped the predecessor:\n%s", got)
		}
		if hold && !strings.Contains(got, "下一请求出现：generate_pdf") {
			t.Fatalf("held host generate dropped the next request:\n%s", got)
		}
	}
}

// TestDiscoverySpentHostGenerateIsACatalogMiss is a generate_pdf grant the
// host already retired under host_document_generate_file. The selection is
// still in the plan, and the model name is the stable spelling of that
// adapter. That spent name is not the wait that keeps web_search as the call.
func TestDiscoverySpentHostGenerateIsACatalogMiss(t *testing.T) {
	cb := discoveryScheduledGenerateAdapter(t, "写一份说明", false, "host_document_generate_file")
	if cb.semanticSurface.retiredGrants == nil {
		cb.semanticSurface.retiredGrants = map[string]tool.InvocationGrant{}
	}
	cb.semanticSurface.retiredGrants["host_document_generate_file"] = tool.InvocationGrant{
		AdapterName: "host_document_generate_file",
		SelectionID: "pdf",
	}
	if discoveryPetitionWaitingInPlan(cb, "generate_pdf") {
		t.Fatal("spent host adapter still counts as a plan hold")
	}
	granted, message := cb.PetitionToolCall("generate_pdf")
	if granted || message != "" {
		t.Fatalf("spent host generate petition granted=%v message=%q", granted, message)
	}
	if live := semanticLiveGrantNameForCapability(cb.semanticSurface, semanticPetitionableCapabilities["generate_pdf"]); live != "" {
		t.Fatalf("spent host generate was issued again as %s", live)
	}
	got := cb.runSemanticToolsSearch(`{"query":"写一份说明","needs":["generate_pdf"]}`)
	if !cb.discoveryUnmatchedObligation || cb.loopCtx == nil || !cb.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("spent host generate left the turn open:\n%s", got)
	}
	if !strings.Contains(got, "没有加载") || !strings.Contains(got, "本轮授权已用尽") || strings.Contains(got, "现在调用") || strings.Contains(got, "已列入本轮计划") || strings.Contains(got, "先调用 web_search") || strings.Contains(got, "下一请求出现：generate_pdf") || !strings.HasSuffix(strings.TrimSpace(got), semanticToolsSearchDiscoveryNextStep) {
		t.Fatalf("spent host generate kept the predecessor:\n%s", got)
	}
	allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"write the pdf"}`)
	if allowed || reason != discoveryUnmatchedExtensionMessage {
		t.Fatalf("dispatch allowed=%v reason=%q", allowed, reason)
	}
}

// TestDiscoverySpentHostGenerateDirectoryDoesNotScheduleIt is the directory
// page after that grant is retired. The selection is still in the plan, but
// the row is spent. The page must not send the model back to web_search to
// produce generate_pdf, and showing the directory is not itself a miss.
func TestDiscoverySpentHostGenerateDirectoryDoesNotScheduleIt(t *testing.T) {
	cb := discoveryScheduledGenerateAdapter(t, "写一份说明", false, "host_document_generate_file")
	if cb.semanticSurface.retiredGrants == nil {
		cb.semanticSurface.retiredGrants = map[string]tool.InvocationGrant{}
	}
	cb.semanticSurface.retiredGrants["host_document_generate_file"] = tool.InvocationGrant{
		AdapterName: "host_document_generate_file",
		SelectionID: "pdf",
	}
	got := cb.runSemanticToolsSearch(`{"query":"写一份说明"}`)
	if cb.discoveryUnmatchedObligation || cb.loopCtx.discoveryCatalogMiss() {
		t.Fatalf("an unfiltered directory opened a miss:\n%s", got)
	}
	rowSpent := false
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- generate_pdf — ") {
			rowSpent = strings.Contains(line, "本轮授权已用尽")
		}
	}
	if !rowSpent || strings.Contains(got, "下一请求出现：generate_pdf") || strings.Contains(got, "generate_pdf（先调用") || strings.Contains(got, "现在调用 web_search") {
		t.Fatalf("spent host generate stayed scheduled:\n%s", got)
	}
	if allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"write the pdf"}`); !allowed || reason != "" {
		t.Fatalf("dispatch allowed=%v reason=%q", allowed, reason)
	}
}

// TestDiscoveryMailboxConfirmationDoesNotCatalogMiss is a retired send that
// still waits for the user. The host keys it by semantic_send_trusted_mailbox
// while the model says mailbox_send. That wait is not a failed load. A spent
// send with no confirmation record still is. The confirmation footer is the
// last line only when the search named that draft. A directory, and the miss
// after it, keep their own action as the tail.
func TestDiscoveryMailboxConfirmationDoesNotCatalogMiss(t *testing.T) {
	for _, key := range []string{"semantic_send_trusted_mailbox", "mailbox_send"} {
		cb := discoveryMailboxConfirmationSurface(t, key)
		got := cb.runSemanticToolsSearch(`{"query":"发一封邮件","needs":["mailbox_send"]}`)
		if cb.discoveryUnmatchedObligation || cb.loopCtx.discoveryCatalogMiss() {
			t.Fatalf("key=%s confirmation opened a miss:\n%s", key, got)
		}
		footer := "等待用户确认：mailbox_send。不要再次调用，也不要用其它工具完成该动作。"
		if strings.Count(got, footer) != 1 || !strings.HasSuffix(strings.TrimSpace(got), footer) || strings.Contains(got, semanticToolsSearchDiscoveryLoadLine) || strings.Contains(got, "没有加载") || strings.Contains(got, discoveryUnmatchedExtensionMessage) {
			t.Fatalf("key=%s confirmation page:\n%s", key, got)
		}
		denial := cb.ToolDenialMessage("mailbox_send")
		if !strings.Contains(denial, "waiting for the user to confirm") || !strings.Contains(denial, "do not use another tool to send it") {
			t.Fatalf("key=%s denial=%q", key, denial)
		}
		if allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"send the mail"}`); !allowed || reason != "" {
			t.Fatalf("key=%s dispatch allowed=%v reason=%q", key, allowed, reason)
		}
	}

	directory := discoveryMailboxConfirmationSurface(t, "semantic_send_trusted_mailbox")
	directoryPage := directory.runSemanticToolsSearch(`{"query":"发一封邮件"}`)
	footer := "等待用户确认：mailbox_send。不要再次调用，也不要用其它工具完成该动作。"
	if directory.discoveryUnmatchedObligation || directory.loopCtx.discoveryCatalogMiss() || strings.Count(directoryPage, footer) != 1 || !strings.HasSuffix(strings.TrimSpace(directoryPage), semanticToolsSearchDiscoveryLoadLine) || strings.Index(directoryPage, footer) > strings.LastIndex(directoryPage, semanticToolsSearchDiscoveryLoadLine) {
		t.Fatalf("unfiltered directory dropped the load line or the draft:\n%s", directoryPage)
	}
	miss := directory.runSemanticToolsSearch(`{"query":"发一封邮件","needs":["not_a_catalog_tool"]}`)
	if strings.Contains(miss, semanticToolsSearchDiscoveryRepeatExhausted) || !directory.discoveryUnmatchedObligation || !directory.loopCtx.discoveryCatalogMiss() || strings.Count(miss, footer) != 1 || !strings.HasSuffix(strings.TrimSpace(miss), semanticToolsSearchDiscoveryNextStep) {
		t.Fatalf("confirmation footer collapsed the miss:\n%s", miss)
	}

	for _, key := range []string{"semantic_send_trusted_mailbox", "mailbox_send"} {
		spent := discoveryMailboxSpentSurface(t, key)
		got := spent.runSemanticToolsSearch(`{"query":"发一封邮件","needs":["mailbox_send"]}`)
		if !spent.discoveryUnmatchedObligation || !spent.loopCtx.discoveryCatalogMiss() {
			t.Fatalf("key=%s spent mailbox left the turn open:\n%s", key, got)
		}
		if !strings.Contains(got, "没有加载") || strings.Contains(got, "等待用户确认") {
			t.Fatalf("key=%s spent mailbox page:\n%s", key, got)
		}
		allowed, reason := spent.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"send the mail"}`)
		if allowed || reason != discoveryUnmatchedExtensionMessage {
			t.Fatalf("key=%s dispatch allowed=%v reason=%q", key, allowed, reason)
		}
	}
}

func discoveryMailboxConfirmationSurface(t *testing.T, key string) *sharedAgentLoopCallbacks {
	t.Helper()
	cb := discoveryMailboxSpentSurface(t, key)
	hop, store := newHopRunTestSurface(t)
	cb.semanticSurface.executor = hop.executor
	cb.semanticSurface.scope = hop.scope
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: cb.semanticSurface.scope, SelectionID: "send"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(cb.semanticSurface.scope, "send", tool.PlanExecutionAwaitingReceipt, "", "mailbox_send_awaiting_confirmation", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if !discoveryRetiredAwaitingConfirmation(cb, "mailbox_send") {
		t.Fatalf("key=%s did not await confirmation", key)
	}
	return cb
}

func discoveryMailboxSpentSurface(t *testing.T, key string) *sharedAgentLoopCallbacks {
	t.Helper()
	_, _, cb := turnDirectoryPlannedSurface(t, "发一封邮件", intent.ModelDiscoveryClassification())
	if cb.semanticSurface.retiredGrants == nil {
		cb.semanticSurface.retiredGrants = map[string]tool.InvocationGrant{}
	}
	cb.semanticSurface.retiredGrants[key] = tool.InvocationGrant{
		AdapterName: "semantic_send_trusted_mailbox",
		SelectionID: "send",
	}
	cb.loopCtx.OfferChatTaskDispatch = true
	return cb
}

// discoveryScheduledGenerate puts generate_pdf on a discovery plan and
// checks the petition really returns the successor denial. hold=false waits
// on an unfinished web_search. hold=true has finished that search and keeps
// the PDF off this request.
func discoveryScheduledGenerate(t *testing.T, text string, hold bool) *sharedAgentLoopCallbacks {
	t.Helper()
	return discoveryScheduledGenerateAdapter(t, text, hold, "generate_pdf")
}

// discoveryScheduledGenerateAdapter is discoveryScheduledGenerate with the
// PDF selection's adapter. host_document_generate_file is the host id; the
// model still says generate_pdf.
func discoveryScheduledGenerateAdapter(t *testing.T, text string, hold bool, adapter string) *sharedAgentLoopCallbacks {
	t.Helper()
	_, _, cb := turnDirectoryPlannedSurface(t, text, intent.ModelDiscoveryClassification())
	if cb.semanticSurface.completed == nil {
		cb.semanticSurface.completed = map[string]bool{}
	}
	if cb.semanticSurface.grants == nil {
		cb.semanticSurface.grants = map[string]tool.InvocationGrant{}
	}
	cb.semanticSurface.grants["web_search"] = tool.InvocationGrant{SelectionID: "search", Token: "live"}
	if cb.semanticSurface.rendered == nil {
		cb.semanticSurface.rendered = map[string]bool{}
	}
	cb.semanticSurface.rendered["web_search"] = true
	cb.semanticSurface.plan.Selections = append(cb.semanticSurface.plan.Selections,
		tool.PlannedSelection{ID: "search", AdapterName: "web_search", FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		tool.PlannedSelection{ID: "pdf", AdapterName: adapter, FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}, Requires: []string{"search"}},
	)
	if hold {
		cb.semanticSurface.completed["search"] = true
		cb.semanticHoldDependantIssue = true
	}
	if !cb.discoverySearchNeedLoadable("generate_pdf") {
		t.Fatal("generate_pdf must be a loadable discovery need")
	}
	granted, message := cb.PetitionToolCall("generate_pdf")
	if hold {
		if granted || !successorHeldForNextRequest(message, "generate_pdf") {
			t.Fatalf("held generate granted=%v message=%q", granted, message)
		}
	} else if granted || !successorNotYetListedDenial(message, "generate_pdf") {
		t.Fatalf("waiting generate granted=%v message=%q", granted, message)
	}
	if cb.discoverySearchNeedCallable("generate_pdf") || !discoveryPetitionWaitingInPlan(cb, "generate_pdf") {
		t.Fatal("generate_pdf left the waiting-in-plan state")
	}
	cb.loopCtx.OfferChatTaskDispatch = true
	return cb
}

func TestDiscoveryListedSiblingKeepsWorkspaceAction(t *testing.T) {
	cb := discoveryListedWebSearchPlan(t, "查资料并打开记事本")
	got := semanticToolsSearchRun(cb, `{"query":"查资料并打开记事本","needs":["web_search","computer_use"]}`)
	if cb.discoveryUnmatchedObligation || (cb.loopCtx != nil && cb.loopCtx.discoveryCatalogMiss()) {
		t.Fatalf("an already listed workspace name opened a catalog miss:\n%s", got)
	}
	trimmed := strings.TrimSpace(got)
	wantTail := semanticToolsSearchListedDirectivePrefix + "web_search。不要再查询这些名字。"
	if strings.Contains(got, "现在调用") || strings.Contains(got, "先调用") || !strings.Contains(got, discoveryMainLoopComputerUseNote) || strings.HasSuffix(trimmed, discoveryMainLoopComputerUseNote) || !strings.HasSuffix(trimmed, wantTail) {
		t.Fatalf("desktop refusal hid the listed workspace tool:\n%s", got)
	}
}

func TestDiscoveryHeldToolKeepsPredecessorCall(t *testing.T) {
	cb := discoveryListedWebSearchPlan(t, "生成pdf并打开记事本")
	got := semanticToolsSearchRun(cb, `{"query":"生成pdf并打开记事本","needs":["generate_pdf","computer_use"]}`)
	if cb.discoveryUnmatchedObligation || (cb.loopCtx != nil && cb.loopCtx.discoveryCatalogMiss()) {
		t.Fatalf("a held pdf opened a catalog miss:\n%s", got)
	}
	trimmed := strings.TrimSpace(got)
	if !strings.Contains(got, discoveryMainLoopComputerUseNote) || !strings.Contains(got, "先调用 web_search") || strings.HasSuffix(trimmed, discoveryMainLoopComputerUseNote) || !strings.HasSuffix(trimmed, "现在调用 web_search。") {
		t.Fatalf("desktop refusal hid the predecessor:\n%s", got)
	}

	// A spent download beside that hold used to open the catalog miss and
	// replace the predecessor with the skill step.
	spent := discoveryPlanWantsWebSearchNow(t, "生成pdf并下载")
	spent.loopCtx.OfferChatTaskDispatch = true
	spentPage := semanticToolsSearchRun(spent, `{"query":"生成pdf并下载","needs":["generate_pdf","download_file","computer_use"]}`)
	spentTail := strings.TrimSpace(spentPage)
	if spent.discoveryUnmatchedObligation || (spent.loopCtx != nil && spent.loopCtx.discoveryCatalogMiss()) {
		t.Fatalf("a spent sibling closed the held pdf:\n%s", spentPage)
	}
	if !strings.Contains(spentPage, discoveryMainLoopComputerUseNote) || strings.HasSuffix(spentTail, discoveryMainLoopComputerUseNote) || strings.HasSuffix(spentTail, semanticToolsSearchDiscoveryNextStep) || !strings.HasSuffix(spentTail, "现在调用 web_search。") {
		t.Fatalf("failed sibling hid the predecessor:\n%s", spentPage)
	}
	if allowed, reason := spent.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"write the pdf"}`); !allowed || reason != "" {
		t.Fatalf("dispatch allowed=%v reason=%q\n%s", allowed, reason, spentPage)
	}
}

func TestDiscoveryExclusiveSkipDoesNotKeepPlanCall(t *testing.T) {
	planned := discoveryListedWebSearchPlan(t, "连上并浏览")
	got := semanticToolsSearchRun(planned, `{"query":"连上并浏览","needs":["ssh","browser"]}`)
	if planned.discoveryUnmatchedObligation || (planned.loopCtx != nil && planned.loopCtx.discoveryCatalogMiss()) {
		t.Fatalf("exclusive skip opened a catalog miss:\n%s", got)
	}
	want := discoveryExclusiveRetryNote([]string{"browser", "ssh"})
	if strings.Contains(got, "现在调用") || strings.Contains(got, "先调用") || !strings.HasSuffix(strings.TrimSpace(got), want) {
		t.Fatalf("exclusive skip kept the plan call:\n%s", got)
	}

	withDesktop := discoveryListedWebSearchPlan(t, "连上并打开记事本")
	desktopPage := semanticToolsSearchRun(withDesktop, `{"query":"连上并打开记事本","needs":["ssh","computer_use"]}`)
	desktopTail := strings.TrimSpace(desktopPage)
	sshAlone := discoveryExclusiveRetryNote([]string{"ssh"})
	if withDesktop.discoveryUnmatchedObligation || strings.Contains(desktopPage, "现在调用") || !strings.Contains(desktopPage, discoveryMainLoopComputerUseNote) || strings.HasSuffix(desktopTail, discoveryMainLoopComputerUseNote) || !strings.HasSuffix(desktopTail, sshAlone) {
		t.Fatalf("desktop refusal hid the exclusive retry:\n%s", desktopPage)
	}

	discovery := intent.ModelDiscoveryClassification()
	_, _, fresh := turnDirectoryPlannedSurface(t, "连上并浏览", discovery)
	skipped := semanticToolsSearchRun(fresh, `{"query":"连上并浏览","needs":["ssh","browser"]}`)
	if fresh.discoveryUnmatchedObligation {
		t.Fatalf("exclusive skip closed the turn:\n%s", skipped)
	}
	sole := semanticToolsSearchRun(fresh, `{"query":"连上那台机器","needs":["ssh"]}`)
	if fresh.discoveryUnmatchedObligation || !turnDirectoryHave(fresh.BuildToolsForModelRequest("连上", 3))["ssh"] {
		t.Fatalf("sole ssh after an exclusive skip:\n%s", sole)
	}

	_, _, workspace := turnDirectoryPlannedSurface(t, "查资料并连上", discovery)
	mixed := semanticToolsSearchRun(workspace, `{"query":"查资料并连上","needs":["web_search","ssh"]}`)
	mixedTail := strings.TrimSpace(mixed)
	if workspace.discoveryUnmatchedObligation || !turnDirectoryHave(workspace.BuildToolsForModelRequest("查资料", 2))["web_search"] || turnDirectoryHave(workspace.BuildToolsForModelRequest("查资料", 3))["ssh"] || strings.HasSuffix(mixedTail, discoveryExclusiveRetryNote([]string{"ssh"})) || !strings.Contains(mixed, "ssh 只有单独写进 needs") {
		t.Fatalf("workspace load lost its action:\n%s", mixed)
	}
}

func toolDefinitionHasSchema(defs []map[string]interface{}, name string) bool {
	for _, def := range defs {
		if extractToolName(def) != name {
			continue
		}
		fn, _ := def["function"].(map[string]interface{})
		params, ok := fn["parameters"].(map[string]interface{})
		return ok && len(params) > 0
	}
	return false
}

func TestDiscoveryShellPetitionRouteCorrectionReplacesUnstartedLocal(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "清理", discovery)
	cb.userText = "清理"
	if granted, message := cb.PetitionToolCall("bash"); !granted {
		t.Fatalf("bash petition granted=%v message=%q", granted, message)
	}
	if !planHasCapability(cb.semanticSurface.plan, tool.CapabilityShellExecuteLocal) {
		t.Fatal("fixture must first admit the local shell route")
	}
	if granted, message := cb.PetitionToolCall("ssh"); !granted {
		t.Fatalf("ssh route correction granted=%v message=%q", granted, message)
	}
	child := cb.semanticSurface
	if planHasCapability(child.plan, tool.CapabilityShellExecuteLocal) {
		t.Fatalf("unstarted local route survived the correction: %#v", child.plan.Selections)
	}
	if !planHasCapability(child.plan, tool.CapabilityShellExecuteRemoteHost) {
		t.Fatalf("remote route missing after correction: %#v", child.plan.Selections)
	}
	published := turnDirectoryHave(cb.BuildToolsForModelRequest("清理", 2))
	if published["bash"] || !published["ssh"] {
		t.Fatalf("corrected model surface=%v, want ssh without bash", turnDirectoryNames(cb.BuildToolsForModelRequest("清理", 2)))
	}
	for _, label := range child.replan.Petitioned {
		if label == intent.LabelShellCommand {
			t.Fatalf("a later revision would resurrect local shell: %#v", child.replan.Petitioned)
		}
	}
	for _, selection := range child.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityShellExecuteRemoteHost && !selection.Optional {
			child.completed[selection.ID] = true
		}
	}
	snapshot := cb.hopRunSnapshot()
	if snapshot.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("completed remote route stayed unsettled: %#v", snapshot)
	}
	if final := finalizeHopRun(snapshot, agent.LoopResult{}); final.Status != "completed" {
		t.Fatalf("final route status=%q remaining=%d", final.Status, final.Remaining)
	}
}

func TestDiscoveryShellPetitionRouteCorrectionKeepsStartedLocal(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "清理", discovery)
	cb.userText = "清理"
	if granted, message := cb.PetitionToolCall("bash"); !granted {
		t.Fatalf("bash petition granted=%v message=%q", granted, message)
	}
	parent := cb.semanticSurface
	localID := ""
	for _, selection := range parent.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityShellExecuteLocal && !selection.Optional {
			localID = selection.ID
		}
	}
	if localID == "" {
		t.Fatal("local route fixture has no required selection")
	}
	parent.hopEvidence = map[string]HopStepEvidence{
		hopEvidenceKey(parent, localID): {State: tool.PlanExecutionRunning},
	}
	if granted, message := cb.PetitionToolCall("ssh"); !granted {
		t.Fatalf("ssh expansion granted=%v message=%q", granted, message)
	}
	if !planHasCapability(cb.semanticSurface.plan, tool.CapabilityShellExecuteLocal) {
		t.Fatal("a running local execution was removed by a later petition")
	}
}

func TestSSHPetitionDoesNotReplaceClassifierOwnedLocalShell(t *testing.T) {
	classification := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98}
	_, parent, cb := turnDirectoryPlannedSurface(t, "在本机清理", classification)
	cb.userText = "在本机清理"
	child, _, err := cb.handler.petitionExpandSemanticCallSurface(context.Background(), parent, intent.LabelSSH)
	if err != nil || child == nil {
		t.Fatalf("classifier-owned parent expansion child=%v err=%v", child, err)
	}
	if !planHasCapability(child.plan, tool.CapabilityShellExecuteLocal) {
		t.Fatal("SSH petition removed a local obligation the classifier owned")
	}
}

func TestDiscoveryShellPetitionRouteCorrectionReplacesUnstartedRemote(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	_, _, cb := turnDirectoryPlannedSurface(t, "清理", discovery)
	cb.userText = "清理"
	if granted, message := cb.PetitionToolCall("ssh"); !granted {
		t.Fatalf("ssh petition granted=%v message=%q", granted, message)
	}
	if granted, message := cb.PetitionToolCall("bash"); !granted {
		t.Fatalf("bash route correction granted=%v message=%q", granted, message)
	}
	child := cb.semanticSurface
	if planHasCapability(child.plan, tool.CapabilityShellExecuteRemoteHost) {
		t.Fatalf("unstarted remote route survived the correction: %#v", child.plan.Selections)
	}
	if !planHasCapability(child.plan, tool.CapabilityShellExecuteLocal) {
		t.Fatalf("local route missing after correction: %#v", child.plan.Selections)
	}
	for _, selection := range child.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityShellExecuteLocal && !selection.Optional {
			child.completed[selection.ID] = true
		}
	}
	if snapshot := cb.hopRunSnapshot(); snapshot.Remaining != 0 || cb.RequiredEffectUnsettled() {
		t.Fatalf("completed local route stayed unsettled: %#v", snapshot)
	}
}

func TestPetitionRouteCorrectionKeepsDependentShell(t *testing.T) {
	local := tool.PlannedSelection{
		ID: "selection:local", NeedID: "need:shell.execute.local:test",
		FitProof: tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteLocal},
	}
	surface := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			local,
			{
				ID: "selection:report", NeedID: "need:report", Requires: []string{local.ID},
				FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice},
			},
		}},
		replan:    &semanticReplanInput{Petitioned: []intent.IntentLabel{intent.LabelShellCommand}},
		completed: map[string]bool{},
	}
	if _, ok := petitionRouteCorrectionFor(surface, intent.LabelSSH); ok {
		t.Fatal("route correction removed a shell family another step requires")
	}
}
