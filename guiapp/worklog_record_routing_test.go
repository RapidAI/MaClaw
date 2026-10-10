package guiapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const worklogProductionUtterance = "上午添加： windows构建服务维护与修复，100%"

func TestWorklogRecordKindStaysOffTodosAndMeetings(t *testing.T) {
	if got := classifyWorklogRecord(worklogProductionUtterance); got != worklogRecordUpdate {
		t.Fatalf("production utterance kind=%v", got)
	}
	if got := classifyWorklogRecord("查一下今天上午的工作日志"); got != worklogRecordRead {
		t.Fatalf("read kind=%v", got)
	}
	if got := classifyWorklogRecord("修改上午工作日志，内容追加一段"); got != worklogRecordUpdate {
		t.Fatalf("named update kind=%v", got)
	}
	if got := classifyWorklogRecord("上午添加一个会议"); got != worklogRecordNone {
		t.Fatalf("meeting without a percent kind=%v", got)
	}
	if got := classifyWorklogRecord("上午添加一个 100% 覆盖率的测试"); got != worklogRecordNone {
		t.Fatalf("attributive percent kind=%v", got)
	}
	if got := classifyWorklogRecord("上午添加覆盖率 100% 的测试"); got != worklogRecordNone {
		t.Fatalf("percent before 的 kind=%v", got)
	}
	if got := classifyWorklogRecord("上午添加：A，100%；B，80%"); got != worklogRecordUpdate {
		t.Fatalf("terminal multi-entry stamp kind=%v", got)
	}
	if got := classifyWorklogRecord("下午添加：接口联调，80%"); got != worklogRecordUpdate {
		t.Fatalf("clause stamp kind=%v", got)
	}
	if got := classifyWorklogRecord("今天下午添加一个按钮，宽度 100%"); got != worklogRecordNone {
		t.Fatalf("measurement percent kind=%v", got)
	}
	if got := classifyWorklogRecord("上午添加：修复100%"); got != worklogRecordNone {
		t.Fatalf("percent glued to the item kind=%v", got)
	}
	if got := classifyWorklogRecord("把上午的工时补上这项，完成度 100%"); got != worklogRecordUpdate {
		t.Fatalf("named record kind=%v", got)
	}
	if got := classifyWorklogRecord("把修复登录问题加到任务列表里"); got != worklogRecordNone {
		t.Fatalf("todo kind=%v", got)
	}
	if got := classifyWorklogRecord("查一下今天的工时记录"); got != worklogRecordRead {
		t.Fatalf("noun 记录 must not become an update: %v", got)
	}
}

func TestSemanticManagedPlanningTextKeepsMarkdownAndWeatherContinue(t *testing.T) {
	markdown := []agent.ConversationEntry{{Role: "user", Content: "生成markdown"}}
	if got := semanticManagedPlanningText("继续", markdown); got != "生成markdown" {
		t.Fatalf("markdown continue=%q", got)
	}
	weather := []agent.ConversationEntry{{Role: "user", Content: "今天天气怎么样"}}
	if got := semanticManagedPlanningText("继续", weather); got != "继续" {
		t.Fatalf("weather continue=%q", got)
	}
	worklog := []agent.ConversationEntry{{Role: "user", Content: worklogProductionUtterance}}
	if got := semanticManagedPlanningText("继续", worklog); got != worklogProductionUtterance {
		t.Fatalf("worklog continue=%q", got)
	}
	if got := semanticManagedPlanningText("继续", []agent.ConversationEntry{
		{Role: "user", Content: worklogProductionUtterance},
		{Role: "user", Content: "生成markdown"},
	}); got != "生成markdown" {
		t.Fatalf("later markdown continue must win: %q", got)
	}
}

func TestWorklogUpdateUtteranceFailsClosedWithoutReviewedWriter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98, Secondary: []intent.IntentLabel{intent.LabelKnowledgeRead}}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", worklogProductionUtterance, "desktop", "root-worklog-update", "turn-worklog-update", stored,
	)
	if !handled || err == nil || surface != nil || !strings.Contains(err.Error(), "record.update.worklog") {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	if strings.Contains(err.Error(), "task.track") || strings.Contains(err.Error(), "tools_search") {
		t.Fatalf("update fell through to another surface: %v", err)
	}
}

func TestAttributivePercentDoesNotBecomeWorklogUpdate(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	for _, utterance := range []string{
		"上午添加一个 100% 覆盖率的测试",
		"今天下午添加一个按钮，宽度 100%",
	} {
		_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
			"user-1", utterance, "desktop", "root-coverage", "turn-coverage", stored,
		)
		if err != nil || !handled || surface == nil {
			t.Fatalf("%q handled=%v err=%v", utterance, handled, err)
		}
		if _, selected := semanticSelectionForCapability(surface.plan, tool.CapabilityRecordUpdateWorklog); selected {
			t.Fatalf("%q selected a work-record update", utterance)
		}
		if _, selected := semanticSelectionForCapability(surface.plan, tool.CapabilityTaskTrackLocal); !selected {
			t.Fatalf("%q dropped the stored task track", utterance)
		}
	}
}

func TestTodoUtteranceStillPlansLocalTask(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把修复登录问题加到任务列表里", "desktop", "root-todo", "turn-todo", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityTaskTrackLocal)
	if !ok || selection.AdapterName != semanticTrustedTaskAdapter {
		t.Fatalf("todo selection=%+v ok=%v", selection, ok)
	}
}

func TestWorklogContinueProjectsUpdateAndWeatherContinueDoesNot(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	continuation := &intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: .92}
	ctx := &LoopContext{
		History: []agent.ConversationEntry{{Role: "user", Content: worklogProductionUtterance}},
		Runtime: RuntimeContext{SemanticIntent: continuation},
	}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, "user-1", "继续", "desktop", nil)
	if !handled || err == nil || surface != nil || !strings.Contains(err.Error(), "record.update.worklog") {
		t.Fatalf("worklog continue handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	weather := &LoopContext{
		History: []agent.ConversationEntry{{Role: "user", Content: "今天天气怎么样"}},
		Runtime: RuntimeContext{SemanticIntent: continuation},
	}
	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(weather, "user-1", "继续", "desktop", nil)
	if err != nil || handled || surface != nil {
		t.Fatalf("weather continue handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
}

func TestReviewedMCPPublicationQuarantinesUnlistedTools(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{"type": "object"}
	tools := []MCPToolView{
		{Name: "work_log_update", Description: "update the morning work log", InputSchema: schema},
		{Name: "work_log_query", Description: "query work log", InputSchema: schema},
	}
	seedObservedRemoteMCP(t, app, "tengyun", tools)
	if err := app.publishReviewedMCPServerTools("tengyun", tools); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	project, err := h.semanticDynamicInventory(context.Background(), "desktop-user:D:/proj")
	if err != nil {
		t.Fatal(err)
	}
	var query, mutation agentservice.MCPToolEntry
	for _, entry := range project.mcpEntries {
		switch entry.ToolName {
		case "work_log_query":
			query = entry
		case "work_log_update":
			mutation = entry
		}
	}
	want := agentservice.DynamicMCPObservedBindingDigest("tengyun", "work_log_query", schema)
	if query.Contract.ObservedBindingDigest != want || query.Contract.Provisions[0].Capability != tool.CapabilityRecordReadWorklog || query.Contract.Provisions[0].Quality != 3 {
		t.Fatalf("project query contract=%#v", query.Contract)
	}
	if len(mutation.Contract.Provisions) != 0 || mutation.Contract.ObservedBindingDigest != "" {
		t.Fatalf("description-only mutation was contracted: %#v", mutation.Contract)
	}
	other, err := h.semanticDynamicInventory(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range other.mcpEntries {
		if len(entry.Contract.Provisions) != 0 || entry.Contract.ObservedBindingDigest != "" {
			t.Fatalf("desktop contract leaked to %s: %#v", entry.ToolName, entry.Contract)
		}
	}
	if semanticContractPrincipal(agentservice.Principal{TenantID: "desktop", UserID: "principal"}).UserID != "principal" {
		t.Fatal("coding principal was collapsed")
	}
	if err := app.publishReviewedMCPServerTools("tengyun", []MCPToolView{tools[0]}); err != nil {
		t.Fatal(err)
	}
	revoked, err := h.semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range revoked.mcpEntries {
		if entry.ToolName == "work_log_query" && (len(entry.Contract.Provisions) != 0 || entry.Contract.ObservedBindingDigest != "") {
			t.Fatalf("disappeared query stayed contracted: %#v", entry.Contract)
		}
	}
}

func TestReviewedMCPCapabilityIdentityPublishesInstalledSearchTool(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"search_query": map[string]interface{}{"type": "string"},
			"content_size": map[string]interface{}{"type": "string"},
		},
		"required": []string{"search_query"},
	}
	tools := []MCPToolView{
		{Name: agentservice.ReviewedMCPWebSearchPrimeTool, InputSchema: schema},
		{Name: "unreviewed_tool", Description: "search the web", InputSchema: schema},
	}
	seedObservedRemoteMCP(t, app, "web-search-prime", tools)
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	var server *corelib.MCPServerEntry
	for i := range cfg.MCPServers {
		if cfg.MCPServers[i].ID == "web-search-prime" {
			server = &cfg.MCPServers[i]
		}
	}
	if server == nil {
		t.Fatal("seeded server missing")
	}
	server.Capability = &corelib.MCPServerCapabilityRef{
		CapabilityID: "cap_43f40e502b689144",
		VersionKey:   agentservice.ReviewedMCPWebSearchPrimeGlobalKey + "@1.0.0",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := app.publishReviewedMCPServerTools("web-search-prime", tools); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	blocked, err := h.semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range blocked.mcpEntries {
		if len(entry.Contract.Provisions) != 0 {
			t.Fatalf("instance id contracted %s: %#v", entry.ToolName, entry.Contract)
		}
	}
	server.Capability.GlobalKey = agentservice.ReviewedMCPWebSearchPrimeGlobalKey
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := app.publishReviewedMCPServerTools("web-search-prime", tools); err != nil {
		t.Fatal(err)
	}
	project, err := h.semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	var search, extra agentservice.MCPToolEntry
	for _, entry := range project.mcpEntries {
		switch entry.ToolName {
		case agentservice.ReviewedMCPWebSearchPrimeTool:
			search = entry
		case "unreviewed_tool":
			extra = entry
		}
	}
	if search.CapabilityGlobalKey != agentservice.ReviewedMCPWebSearchPrimeGlobalKey || search.InstalledCapabilityID != "cap_43f40e502b689144" {
		t.Fatalf("inventory identity=%q/%q", search.CapabilityGlobalKey, search.InstalledCapabilityID)
	}
	if len(search.Contract.Provisions) != 2 || search.Contract.Provisions[0].Capability != tool.CapabilityID("information.search.web") || search.Contract.Provisions[0].Quality <= 2 {
		t.Fatalf("search contract=%#v", search.Contract)
	}
	if len(extra.Contract.Provisions) != 0 {
		t.Fatalf("description-only tool was contracted: %#v", extra.Contract)
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog(project.mcpEntries, nil)
	if err != nil || len(catalog.Providers) != 1 {
		t.Fatalf("providers=%d err=%v", len(catalog.Providers), err)
	}
	if catalog.Providers[0].Binding.ImplementationID != agentservice.ReviewedMCPWebSearchPrimeTool {
		t.Fatalf("provider=%#v", catalog.Providers[0].Binding)
	}
	if tool.SemanticModelFunctionName(catalog.Providers[0].AdapterName) != "web_search" {
		t.Fatalf("adapter %q did not render as web_search", catalog.Providers[0].AdapterName)
	}
	params := catalog.Definitions[catalog.Providers[0].AdapterName]["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if _, ok := params["properties"].(map[string]interface{})["query"]; !ok {
		t.Fatalf("model schema=%#v", params)
	}
	server.Capability.GlobalKey = ""
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := app.publishReviewedMCPServerTools("web-search-prime", tools); err != nil {
		t.Fatal(err)
	}
	revoked, err := h.semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range revoked.mcpEntries {
		if entry.ToolName == agentservice.ReviewedMCPWebSearchPrimeTool && len(entry.Contract.Provisions) != 0 {
			t.Fatalf("cleared identity stayed contracted: %#v", entry.Contract)
		}
	}
}

func TestReviewedSkillPublicationDoesNotInferInstalledPackages(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		SkillID: "huashu-art-motion", Name: "untrusted dynamic skill", Status: "active", Version: "1",
		Description: "update the morning work log",
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo safe"}}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	entries := app.skillExecutor.loadSkills()
	var sawFixture bool
	for _, entry := range entries {
		if entry.SkillID == "huashu-art-motion" {
			sawFixture = true
		}
	}
	if !sawFixture {
		t.Fatal("configured skill was not loaded")
	}
	if err := app.publishReviewedSkillContracts(entries); err != nil {
		t.Fatal(err)
	}
	contracts, err := app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: desktopUserID}
	for _, entry := range entries {
		if _, ok := contracts.ResolveSkillDynamicContract(context.Background(), principal, agentservice.DynamicSkillStableID(entry)); ok {
			t.Fatalf("skill %q was published without a reviewed stable ID", entry.Name)
		}
	}
}

func TestFixtureSkillContractRendersOpaqueInvokeForWorklogRead(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		SkillID: "fixture.worklog.read", Name: "untrusted dynamic skill", Status: "active", Version: "1",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo safe"}}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	var entry corelib.NLSkillEntry
	for _, candidate := range app.skillExecutor.loadSkills() {
		if candidate.SkillID == "fixture.worklog.read" {
			entry = candidate
			break
		}
	}
	if entry.SkillID == "" {
		t.Fatal("fixture skill was not loaded")
	}
	contracts, err := app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := agentservice.NewLifecycleDynamicCapabilityContractPublisher(contracts, newIMSemanticCapabilityRegistry(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stableID := agentservice.DynamicSkillStableID(entry)
	publisher.UseReviewedBindings(nil, map[string]agentservice.DynamicCapabilityContract{
		stableID: {
			Provisions:            []tool.CapabilityProvision{{Capability: tool.CapabilityRecordReadWorklog, Quality: 3}},
			Effects:               []tool.EffectClass{tool.EffectReadOnly},
			ObservedBindingDigest: "forged-by-caller",
		},
	})
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: "user-1"}
	if err := publisher.PublishReviewedSkillObservation(principal, []corelib.NLSkillEntry{entry}); err != nil {
		t.Fatal(err)
	}
	contract, ok := contracts.ResolveSkillDynamicContract(context.Background(), principal, stableID)
	want := agentservice.DynamicSkillObservedBindingDigest(stableID, entry.Version, agentservice.DynamicSkillContentDigest(entry))
	if !ok || contract.ObservedBindingDigest != want {
		t.Fatalf("published digest=%q want=%q ok=%v", contract.ObservedBindingDigest, want, ok)
	}
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "查一下今天上午的工作日志", "desktop", "root-worklog-read", "turn-worklog-read", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityRecordReadWorklog)
	if !ok || selection.Provider.Kind != "skill" {
		t.Fatalf("read selection=%+v ok=%v", selection, ok)
	}
	if _, taskSelected := semanticSelectionForCapability(surface.plan, tool.CapabilityTaskTrackLocal); taskSelected {
		t.Fatal("worklog read selected the local todo")
	}
	var skillDef map[string]interface{}
	for _, def := range defs {
		if strings.HasPrefix(extractToolName(def), "invoke_") {
			skillDef = def
		}
	}
	if skillDef == nil {
		t.Fatalf("invoke missing: %#v", defs)
	}
	if name := extractToolName(skillDef); strings.Contains(name, "fixture") || strings.Contains(skillDef["function"].(map[string]interface{})["description"].(string), "untrusted") {
		t.Fatalf("skill identity leaked: %#v", skillDef)
	}
	properties, _ := skillDef["function"].(map[string]interface{})["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, leaked := properties["skill_name"]; leaked {
		t.Fatalf("skill selector leaked: %#v", skillDef)
	}
}

func TestFixtureWorklogUpdateContractRendersInvoke(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	const toolName = "fixture_worklog_append"
	seedObservedRemoteMCP(t, app, "fixture-mcp", []MCPToolView{{Name: toolName, Description: "append a work record", InputSchema: schema}})
	contracts, err := app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: "user-1"}
	if err := contracts.PublishMCPContract(principal, "fixture-mcp", toolName, agentservice.DynamicCapabilityContract{
		Provisions:            []tool.CapabilityProvision{{Capability: tool.CapabilityRecordUpdateWorklog, Quality: 3}},
		Effects:               []tool.EffectClass{tool.EffectExternalEffect},
		ObservedBindingDigest: agentservice.DynamicMCPObservedBindingDigest("fixture-mcp", toolName, schema),
	}); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", worklogProductionUtterance, "desktop", "root-worklog-writer", "turn-worklog-writer", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityRecordUpdateWorklog)
	if !ok || selection.Provider.Kind != "mcp" {
		t.Fatalf("update selection=%+v ok=%v", selection, ok)
	}
	if _, taskSelected := semanticSelectionForCapability(surface.plan, tool.CapabilityTaskTrackLocal); taskSelected {
		t.Fatal("worklog update selected the local todo")
	}
	sawInvoke := false
	for _, def := range defs {
		name := extractToolName(def)
		if strings.HasPrefix(name, "invoke_") {
			sawInvoke = true
			if strings.Contains(name, toolName) || strings.Contains(name, "fixture") {
				t.Fatalf("update identity leaked: %s", name)
			}
		}
	}
	if !sawInvoke {
		t.Fatalf("invoke missing: %#v", defs)
	}
}

func TestReviewedMCPWebSearchRecordsLookupEvidence(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	searchSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"search_query":          map[string]interface{}{"type": "string"},
			"content_size":          map[string]interface{}{"type": "string"},
			"location":              map[string]interface{}{"type": "string"},
			"search_domain_filter":  map[string]interface{}{"type": "string"},
			"search_recency_filter": map[string]interface{}{"type": "string"},
		},
		"required": []string{"search_query"},
	}
	worklogSchema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	tools := []MCPToolView{
		{Name: agentservice.ReviewedMCPWebSearchPrimeTool, InputSchema: searchSchema},
		{Name: "work_log_query", InputSchema: worklogSchema},
	}
	seedObservedRemoteMCP(t, app, "web-search-prime", tools)
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.MCPServers {
		if cfg.MCPServers[i].ID != "web-search-prime" {
			continue
		}
		cfg.MCPServers[i].Capability = &corelib.MCPServerCapabilityRef{
			CapabilityID: "cap_43f40e502b689144",
			VersionKey:   agentservice.ReviewedMCPWebSearchPrimeGlobalKey + "@1.0.0",
			GlobalKey:    agentservice.ReviewedMCPWebSearchPrimeGlobalKey,
		}
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := app.publishReviewedMCPServerTools("web-search-prime", tools); err != nil {
		t.Fatal(err)
	}
	transport := &mcpEvidenceRoundTrip{mode: "search"}
	app.mcpRegistry.client = &http.Client{Transport: transport}
	h := &IMMessageHandler{app: app}
	inventory, err := h.semanticDynamicInventory(context.Background(), desktopUserID)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog(inventory.mcpEntries, nil)
	if err != nil {
		t.Fatal(err)
	}
	searchProvider, ok := providerByImplementation(catalog.Providers, agentservice.ReviewedMCPWebSearchPrimeTool)
	if !ok {
		t.Fatal("reviewed search provider was not projected")
	}
	worklogProvider, ok := providerByImplementation(catalog.Providers, "work_log_query")
	if !ok {
		t.Fatal("work log provider was not projected")
	}
	callback := &sharedAgentLoopCallbacks{
		handler: h,
		semanticSurface: &semanticCallSurface{
			scope: tool.InvocationScope{PrincipalID: desktopUserID, RootTaskID: "root-evidence", PlanID: "plan-evidence"},
		},
	}
	search := plannedSelectionForProvider(searchProvider, "information.search.web")
	result := callback.executeBoundSemanticSelectionCanonical(search, tool.CanonicalRequest{CanonicalJSON: []byte(`{"query":"张学友"}`)})
	if !result.Succeeded || result.Result != "张学友资料" {
		t.Fatalf("search result=%+v", result)
	}
	if strings.Contains(result.Result, "jsonrpc") || strings.Contains(result.Result, "web-search-prime") {
		t.Fatalf("protocol identity leaked into the tool result: %q", result.Result)
	}
	if callback.semanticLookupEvidence != result.Result {
		t.Fatalf("evidence=%q result=%q", callback.semanticLookupEvidence, result.Result)
	}
	if body := hostOwnedPDFReportContent("", callback.semanticLookupEvidence, "张学友"); !strings.Contains(body, "张学友资料") || strings.Contains(body, "jsonrpc") {
		t.Fatalf("pdf body lost the search text: %q", body)
	}
	call := transport.toolCall(0)
	arguments, _ := call["params"].(map[string]interface{})["arguments"].(map[string]interface{})
	if arguments["search_query"] != "张学友" {
		t.Fatalf("mapped arguments=%#v", arguments)
	}
	if _, present := arguments["query"]; present {
		t.Fatalf("model field reached the provider: %#v", arguments)
	}
	kept := callback.semanticLookupEvidence
	transport.setMode("untrusted")
	untrusted := callback.executeBoundSemanticSelectionCanonical(search, tool.CanonicalRequest{CanonicalJSON: []byte(`{"query":"张学友"}`)})
	if !untrusted.Succeeded || callback.semanticLookupEvidence != kept {
		t.Fatalf("untrusted payload overwrote evidence: result=%+v evidence=%q", untrusted, callback.semanticLookupEvidence)
	}
	transport.setMode("worklog")
	worklog := callback.executeBoundSemanticSelectionCanonical(plannedSelectionForProvider(worklogProvider, tool.CapabilityRecordReadWorklog), tool.CanonicalRequest{CanonicalJSON: []byte(`{}`)})
	if !worklog.Succeeded || worklog.Result != "上午工作记录" || callback.semanticLookupEvidence != kept {
		t.Fatalf("non-search result=%+v evidence=%q", worklog, callback.semanticLookupEvidence)
	}
	transport.setMode("tool_error")
	toolErr := callback.executeBoundSemanticSelectionCanonical(search, tool.CanonicalRequest{CanonicalJSON: []byte(`{"query":"张学友"}`)})
	if toolErr.Succeeded || toolErr.Unknown || !strings.Contains(toolErr.Result, "quota exceeded") || callback.semanticLookupEvidence != kept {
		t.Fatalf("tool error result=%+v evidence=%q", toolErr, callback.semanticLookupEvidence)
	}
	transport.setMode("fail")
	failed := callback.executeBoundSemanticSelectionCanonical(search, tool.CanonicalRequest{CanonicalJSON: []byte(`{"query":"张学友"}`)})
	if failed.Succeeded || !failed.Unknown || callback.semanticLookupEvidence != kept {
		t.Fatalf("failed search result=%+v evidence=%q", failed, callback.semanticLookupEvidence)
	}
}

func providerByImplementation(providers []tool.ProviderSpec, implementationID string) (tool.ProviderSpec, bool) {
	for _, provider := range providers {
		if provider.Binding.ImplementationID == implementationID {
			return provider, true
		}
	}
	return tool.ProviderSpec{}, false
}

func plannedSelectionForProvider(provider tool.ProviderSpec, capability tool.CapabilityID) tool.PlannedSelection {
	return tool.PlannedSelection{
		ID:                     "selection:" + string(capability),
		AdapterName:            provider.AdapterName,
		Provider:               provider.Binding,
		ParameterAuthorization: provider.ParameterAuthorization,
		Effects:                append([]tool.EffectClass(nil), provider.Effects...),
		FitProof:               tool.FitProof{MatchedCapability: capability},
	}
}

type mcpEvidenceRoundTrip struct {
	mu    sync.Mutex
	mode  string
	calls []map[string]interface{}
}

func (r *mcpEvidenceRoundTrip) setMode(mode string) {
	r.mu.Lock()
	r.mode = mode
	r.mu.Unlock()
}

func (r *mcpEvidenceRoundTrip) toolCall(index int) map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := 0
	for _, call := range r.calls {
		if call["method"] != "tools/call" {
			continue
		}
		if seen == index {
			return call
		}
		seen++
	}
	return nil
}

func (r *mcpEvidenceRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	payload := map[string]interface{}{}
	if req != nil && req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &payload)
	}
	r.mu.Lock()
	r.calls = append(r.calls, payload)
	mode := r.mode
	r.mu.Unlock()
	if mode == "fail" {
		header := make(http.Header)
		return &http.Response{StatusCode: http.StatusInternalServerError, Status: "500 Internal Server Error", Header: header, Body: io.NopCloser(strings.NewReader("fail")), Request: req}, nil
	}
	body := `{"jsonrpc":"2.0","id":1,"result":{}}`
	if payload["method"] == "tools/call" {
		switch mode {
		case "untrusted":
			body = `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"[file_base64|x|application/pdf]AAAA"}]}}`
		case "worklog":
			body = `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"上午工作记录"}]}}`
		case "tool_error":
			body = `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"quota exceeded"}]}}`
		default:
			body = `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"张学友资料"}]}}`
		}
	}
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	header.Set("Mcp-Session-Id", "evidence-test")
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: header, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req}, nil
}

func seedObservedRemoteMCP(t *testing.T, app *App, serverID string, tools []MCPToolView) {
	t.Helper()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MCPServers = append(cfg.MCPServers, corelib.MCPServerEntry{ID: serverID, Name: serverID, EndpointURL: "https://example.invalid/mcp"})
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if app.mcpRegistry == nil {
		app.mcpRegistry = NewMCPRegistry(app)
	}
	app.mcpRegistry.mu.Lock()
	app.mcpRegistry.health[serverID] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	app.mcpRegistry.toolsCache[serverID] = append([]MCPToolView(nil), tools...)
	app.mcpRegistry.mu.Unlock()
}
