package guiapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// TestWorklogUpdateUtterancePlansReviewedMCPWriter is the production turn
// from the 2026-10-10 report: an observed server presenting the reviewed
// mutation name must plan that writer instead of answering
// "当前能力目录未覆盖这项请求". The optional read leg is not required to be
// present, so a server that only exposes the mutation still plans.
func TestWorklogUpdateUtterancePlansReviewedMCPWriter(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	tools := []MCPToolView{{Name: "work_log_update", Description: "update the morning work log", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "tengyun", tools)
	if err := app.publishReviewedMCPServerTools("tengyun", tools); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", worklogProductionUtterance, "desktop", "root-worklog-writer", "turn-worklog-writer", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityRecordUpdateWorklog)
	if !ok || selection.Provider.Kind != "mcp" || selection.Provider.ProviderID != "tengyun" {
		t.Fatalf("update selection=%+v ok=%v", selection, ok)
	}
	for _, effect := range selection.Effects {
		if effect != tool.EffectExternalEffect {
			t.Fatalf("update effect=%v", effect)
		}
	}
	if len(surface.plan.Unmet) != 0 {
		t.Fatalf("unmet=%+v", surface.plan.Unmet)
	}
	// The write goes to an external system of record, so the turn settles on
	// the host's external-effect receipt boundary rather than the provider's
	// own result. That is asserted here so a later effect downgrade cannot
	// quietly turn a recorded mutation into a self-certified success.
	if !tool.SelectionRequiresExternalReceipt(selection) {
		t.Fatalf("update selection is not receipt-bound: %+v", selection)
	}
}

// TestWorklogUpdateUtterancePlansInstalledAddTool is the installed server the
// user actually has: post_work_log_add, not the reviewed name work_log_update.
// Installation has to make that write reachable. The turn must not die as an
// uncovered record.update.worklog before the model is called.
func TestWorklogUpdateUtterancePlansInstalledAddTool(t *testing.T) {
	tools := []MCPToolView{{
		Name: "post_work_log_add", Description: "append a work log", InputSchema: discoveredMCPObjectSchema(),
	}}
	h, _ := newDiscoveredMCPHandler(t, "tengyun", tools, true)
	stored := &intent.ClassificationResult{Primary: intent.LabelTaskTrack, Confidence: .98}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", worklogProductionUtterance, "desktop", "root-worklog-add", "turn-worklog-add", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	found := false
	for _, selection := range surface.plan.Selections {
		if selection.Provider.ImplementationID == "post_work_log_add" && selection.Provider.ProviderID == "tengyun" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("installed write was not selected: selections=%+v unmet=%+v", surface.plan.Selections, surface.plan.Unmet)
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
		{Name: "work_log_delete", Description: "delete a work log entry", InputSchema: schema},
	}
	seedObservedRemoteMCP(t, app, "tengyun", tools)
	if err := app.publishReviewedMCPServerTools("tengyun", tools); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	project, err := h.semanticDynamicInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var query, mutation, unlisted agentservice.MCPToolEntry
	for _, entry := range project.mcpEntries {
		switch entry.ToolName {
		case "work_log_query":
			query = entry
		case "work_log_update":
			mutation = entry
		case "work_log_delete":
			unlisted = entry
		}
	}
	want := agentservice.DynamicMCPObservedBindingDigest("tengyun", "work_log_query", schema)
	if query.Contract.ObservedBindingDigest != want || query.Contract.Provisions[0].Capability != tool.CapabilityRecordReadWorklog || query.Contract.Provisions[0].Quality != 3 {
		t.Fatalf("project query contract=%#v", query.Contract)
	}
	// The mutation is reviewed by name, so any observed server that presents
	// it carries the capability. Its effect is the external one: the record
	// lives in a system this process cannot read back.
	wantUpdate := agentservice.DynamicMCPObservedBindingDigest("tengyun", "work_log_update", schema)
	if mutation.Contract.ObservedBindingDigest != wantUpdate ||
		mutation.Contract.Provisions[0].Capability != tool.CapabilityRecordUpdateWorklog ||
		mutation.Contract.Provisions[0].Quality != 2 ||
		mutation.Contract.Effects[0] != tool.EffectExternalEffect {
		t.Fatalf("project mutation contract=%#v", mutation.Contract)
	}
	// A neighbouring name for another mutation of the same record is not the
	// reviewed name, so reviewing work_log_update must not widen into it.
	// The open family does make it callable, and because the host cannot trust
	// an unreviewed vendor tool to be read-only, it is classified fail-safe as
	// an external effect gated by the receipt boundary — that is the relaxed
	// "arbitrary installed MCP may write" posture. The invariant that still
	// holds is that the unreviewed name never gains the REVIEWED mutation
	// capability: no record.update.worklog may come from it.
	if len(unlisted.Contract.Provisions) == 0 {
		t.Fatalf("unlisted tool was not contracted at all: %#v", unlisted.Contract)
	}
	for _, provision := range unlisted.Contract.Provisions {
		if provision.Capability == tool.CapabilityRecordUpdateWorklog {
			t.Fatalf("unlisted name gained the reviewed mutation: %#v", unlisted.Contract)
		}
	}
	if unlisted.Contract.Provisions[0].Capability != agentservice.CapabilityInformationLookup {
		t.Fatalf("unlisted name not on the open family: %#v", unlisted.Contract)
	}
	for _, effect := range unlisted.Contract.Effects {
		if effect != tool.EffectExternalEffect && effect != tool.EffectReadOnly {
			t.Fatalf("unlisted name contracted an unexpected effect: %#v", unlisted.Contract)
		}
	}
	// The GUI semantic surface resolves under the single host contract scope,
	// so every session reaching it observes the same published contracts.
	other, err := h.semanticDynamicInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range other.mcpEntries {
		if len(entry.Contract.Provisions) == 0 || entry.Contract.ObservedBindingDigest == "" {
			t.Fatalf("host contract missing for %s: %#v", entry.ToolName, entry.Contract)
		}
	}
	// Per-principal scopes stay isolated through the execution-time entry: a
	// Coding principal resolves its own scope and must not see the desktop
	// contracts.
	coding, err := h.semanticDynamicInventoryForPrincipal(context.Background(), agentservice.Principal{TenantID: "desktop", UserID: "principal"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range coding.mcpEntries {
		if len(entry.Contract.Provisions) != 0 || entry.Contract.ObservedBindingDigest != "" {
			t.Fatalf("desktop contract leaked to %s: %#v", entry.ToolName, entry.Contract)
		}
	}
	if err := app.publishReviewedMCPServerTools("tengyun", []MCPToolView{tools[0]}); err != nil {
		t.Fatal(err)
	}
	revoked, err := h.semanticDynamicInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range revoked.mcpEntries {
		if entry.ToolName == "work_log_query" && (len(entry.Contract.Provisions) != 0 || entry.Contract.ObservedBindingDigest != "") {
			t.Fatalf("disappeared query stayed contracted: %#v", entry.Contract)
		}
	}
}

// TestArbitraryMCPToolIsCallableThroughOpenLookup is the root-cause test for
// "a newly installed MCP is unusable". A tools/list name nobody reviewed must
// still be callable: it is published under the open read-only family, so the
// model can invoke an arbitrary MCP tool, while the effect stays read-only and
// the tool never enters the external-effect receipt path.
func TestArbitraryMCPToolIsCallableThroughOpenLookup(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
		"additionalProperties": false,
	}
	tools := []MCPToolView{{Name: "totally_new_vendor_tool", Description: "anything", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "brand-new-mcp", tools)
	if err := app.publishReviewedMCPServerTools("brand-new-mcp", tools); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	inventory, err := h.semanticDynamicInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var entry agentservice.MCPToolEntry
	for _, candidate := range inventory.mcpEntries {
		if candidate.ToolName == "totally_new_vendor_tool" {
			entry = candidate
		}
	}
	if entry.ToolName == "" {
		t.Fatalf("arbitrary tool was not contracted: %#v", inventory.mcpEntries)
	}
	if len(entry.Contract.Provisions) != 2 ||
		entry.Contract.Provisions[0].Capability != agentservice.CapabilityInformationLookup ||
		entry.Contract.Provisions[1].Capability != agentservice.AutoMCPCapabilityIDForBinding("brand-new-mcp", "totally_new_vendor_tool") ||
		entry.Contract.Effects[0] != tool.EffectExternalEffect {
		t.Fatalf("arbitrary contract=%#v", entry.Contract)
	}
	// The binding must be digest-bound, so a schema change on the same name
	// cannot keep the old authority.
	if entry.Contract.ObservedBindingDigest != agentservice.DynamicMCPObservedBindingDigest("brand-new-mcp", "totally_new_vendor_tool", schema) {
		t.Fatalf("digest=%q", entry.Contract.ObservedBindingDigest)
	}
	// An arbitrary, unannotated MCP tool is callable but fail-safe treated as
	// an external effect: it is not silently assumed read-only, so its
	// selection is receipt-bound and settles through the external-effect
	// boundary rather than running with no confirmation.
	selection := tool.PlannedSelection{Effects: entry.Contract.Effects}
	if !tool.SelectionRequiresExternalReceipt(selection) {
		t.Fatalf("open lookup selection is not receipt-bound: %+v", selection)
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog(inventory.mcpEntries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Providers) != 1 || catalog.Providers[0].Binding.Kind != "mcp" {
		t.Fatalf("catalog providers=%#v", catalog.Providers)
	}
	// The provider must carry the open capability, or it is in the catalog but
	// nothing can ever select it for a need.
	provides := catalog.Providers[0].Provides
	if len(provides) != 2 || provides[0].Capability != agentservice.CapabilityInformationLookup {
		t.Fatalf("catalog provision=%#v", provides)
	}
}

// TestArbitraryMCPReachesTheModelSurface closes the loop the publication test
// opens: the arbitrary tool must be demanded, planned, and rendered on a live
// managed turn — not merely present in the inventory. The open family's
// optional ambient need is the only demand route, because a label rule would
// be an owner statement about user outcomes and a vendor tool is not one.
func TestArbitraryMCPReachesTheModelSurface(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
		"additionalProperties": false,
	}
	tools := []MCPToolView{{Name: "totally_new_vendor_tool", Description: "anything", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "brand-new-mcp", tools)
	if err := app.publishReviewedMCPServerTools("brand-new-mcp", tools); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelKnowledgeRead, Confidence: .98}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", "查一下上周的项目进展", "desktop", "root-open-lookup", "turn-open-lookup", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, agentservice.CapabilityInformationLookup)
	if !ok {
		t.Fatalf("open family never planned: selections=%+v unmet=%+v", surface.plan.Selections, surface.plan.Unmet)
	}
	if selection.Provider.Kind != "mcp" || selection.Provider.ProviderID != "brand-new-mcp" {
		t.Fatalf("open selection=%+v", selection)
	}
	if len(surface.plan.Unmet) != 0 {
		t.Fatalf("optional open need produced unmet rows: %+v", surface.plan.Unmet)
	}
	if selection.Effects[0] != tool.EffectExternalEffect {
		t.Fatalf("open selection effects=%v", selection.Effects)
	}
	// The grant must be on the rendered surface: publish+plan without a model
	// visible invoke token is still unusable.
	rendered := false
	for name := range surface.grants {
		if strings.HasPrefix(name, "invoke_") {
			rendered = true
		}
	}
	if !rendered {
		t.Fatalf("open family planned but never rendered: grants=%v", surface.grants)
	}
}

// TestOpenLookupNeedRequiresAProvider keeps the demand side inert on hosts
// with no dynamic provider: no installed MCP means no extra need on any
// managed turn, so unplugged hosts keep their exact historical plan shape.
func TestOpenLookupNeedRequiresAProvider(t *testing.T) {
	needs := []tool.CapabilityNeed{{ID: "need:x", Capability: tool.CapabilityKnowledgeReadLocal, Polarity: tool.NeedRequire}}
	got := semanticNeedsWithDynamicLookupForProviders(needs, nil)
	if len(got) != 1 {
		t.Fatalf("no provider must not add a need: %+v", got)
	}
	providers := []tool.ProviderSpec{{
		Ready:       true,
		AdapterName: "dynamic_mcp_probe",
		Binding:     tool.ProviderBinding{Kind: "mcp", ProviderID: "brand-new-mcp", ImplementationID: "totally_new_vendor_tool"},
		Provides:    []tool.CapabilityProvision{{Capability: agentservice.CapabilityInformationLookup, Quality: 1}},
		Effects:     []tool.EffectClass{tool.EffectReadOnly},
	}}
	got = semanticNeedsWithDynamicLookupForProviders(needs, providers)
	if len(got) != 2 || got[1].Capability != agentservice.CapabilityInformationLookup || got[1].Required {
		t.Fatalf("open lookup need=%+v", got[1])
	}
	got = semanticNeedsWithDynamicLookupForProviders(got, providers)
	if len(got) != 2 {
		t.Fatalf("second pass must not duplicate the need: %+v", got)
	}
}

// TestTightBudgetOmitsTheOpenNeed pins the safety property under a selection
// budget that cannot fit the open family: the optional need lands in
// plan.Omitted with optional_budget_omitted, never in plan.Unmet — a tight
// budget must not turn an installed MCP into a failed turn or a host reject.
func TestTightBudgetOmitsTheOpenNeed(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
		"additionalProperties": false,
	}
	tools := []MCPToolView{{Name: "totally_new_vendor_tool", Description: "x", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "brand-new-mcp", tools)
	if err := app.publishReviewedMCPServerTools("brand-new-mcp", tools); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelKnowledgeRead, Confidence: .98}
	ctx := withSemanticPlanningBudget(context.Background(), 1)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, "desktop-user", "查一下上周的项目进展", "desktop", "root-budget", "turn-budget", stored, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	for _, sel := range surface.plan.Selections {
		if sel.FitProof.MatchedCapability == agentservice.CapabilityInformationLookup {
			t.Fatalf("open family planned under budget=1: %+v", sel)
		}
	}
	if len(surface.plan.Unmet) != 0 {
		t.Fatalf("open need produced unmet rows under a tight budget: %+v", surface.plan.Unmet)
	}
	omitted := false
	for _, item := range surface.plan.Omitted {
		if item.NeedID == "need:~ambient:"+string(agentservice.CapabilityInformationLookup) &&
			item.ReasonCode == "optional_budget_omitted" {
			omitted = true
		}
	}
	if !omitted {
		t.Fatalf("open need missing from Omitted: %+v", surface.plan.Omitted)
	}
}

// TestDegradedMCPDoesNotPublishOpenLookup keeps the readiness gate honest: a
// server whose observation did not succeed must not hand an arbitrary tool a
// capability, or an unreachable server becomes callable on the strength of a
// stale tools/list alone.
func TestDegradedMCPDoesNotPublishOpenLookup(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{"type": "object"}
	tools := []MCPToolView{{Name: "totally_new_vendor_tool", InputSchema: schema}}
	seedObservedRemoteMCP(t, app, "brand-new-mcp", tools)
	app.mcpRegistry.health["brand-new-mcp"].Status = mcpHealthStatusDegraded
	if err := app.publishReviewedMCPServerTools("brand-new-mcp", tools); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	inventory, err := h.semanticDynamicInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range inventory.mcpEntries {
		if entry.ToolName != "totally_new_vendor_tool" {
			continue
		}
		if len(entry.Contract.Provisions) != 0 || entry.Contract.ObservedBindingDigest != "" {
			t.Fatalf("degraded server published an arbitrary tool: %#v", entry.Contract)
		}
	}
}

// TestLocalStdioMCPJoinsTheOpenFamily keeps the local admission honest: the
// real sync path publishes a local stdio server's tools while its client is
// running, and that running client IS the finished observation — the gate
// must admit it, or every unreviewed tool on npx-style servers is invisible.
func TestLocalStdioMCPJoinsTheOpenFamily(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
		"additionalProperties": false,
	}
	tools := []MCPToolView{{Name: "local_vendor_tool", Description: "x", InputSchema: schema}}
	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	if err := app.mcpRegistry.RegisterLocal(corelib.LocalMCPServerEntry{ID: "local-stdio", Name: "local-stdio", Command: "echo", AutoStart: true}); err != nil {
		t.Fatalf("register local: %v", err)
	}
	app.localMCPManager.mu.Lock()
	app.localMCPManager.clients["local-stdio"] = &LocalMCPClient{entry: corelib.LocalMCPServerEntry{ID: "local-stdio", Name: "local-stdio"}, running: true, tools: append([]MCPToolView(nil), tools...)}
	app.localMCPManager.mu.Unlock()

	// The sync path: recordRuntimeSyncReady (a no-op for non-market sources)
	// then publish while the client is running.
	if err := app.publishReviewedMCPServerTools("local-stdio", tools); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	inventory, err := h.semanticDynamicInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range inventory.mcpEntries {
		if entry.ToolName != "local_vendor_tool" {
			continue
		}
		found = true
		if len(entry.Contract.Provisions) != 2 || entry.Contract.Provisions[0].Capability != agentservice.CapabilityInformationLookup || entry.Contract.Provisions[1].Capability != agentservice.AutoMCPCapabilityIDForBinding("local-stdio", "local_vendor_tool") {
			t.Fatalf("local stdio contract=%#v", entry.Contract)
		}
		if entry.Contract.ObservedBindingDigest != agentservice.DynamicMCPObservedBindingDigest("local-stdio", "local_vendor_tool", schema) {
			t.Fatalf("local stdio digest=%q", entry.Contract.ObservedBindingDigest)
		}
	}
	if !found {
		t.Fatalf("local stdio tool missing from inventory: %#v", inventory.mcpEntries)
	}

	// A stopped client is not an observation: publishing must fail closed.
	app.localMCPManager.mu.Lock()
	app.localMCPManager.clients["local-stdio"].running = false
	app.localMCPManager.mu.Unlock()
	if err := app.publishReviewedMCPServerTools("local-stdio", tools); err != nil {
		t.Fatal(err)
	}
	inventory, err = h.semanticDynamicInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range inventory.mcpEntries {
		if entry.ToolName == "local_vendor_tool" && len(entry.Contract.Provisions) != 0 {
			t.Fatalf("stopped client still published: %#v", entry.Contract)
		}
	}
}

// TestFirstDiscoveryPublishesOpenFamilyAndExecutes drives the real first-contact
// lifecycle over a live MCP JSON-RPC server: the tools/list observation itself
// must admit its own unreviewed tools (health recorded before publication, or
// the gate reads the pre-probe unknown state and drops every arbitrary tool on
// first discovery), and the planned selection must dispatch tools/call with the
// model's arguments and return the vendor content.
func TestFirstDiscoveryPublishesOpenFamilyAndExecutes(t *testing.T) {
	var calledTool string
	var calledArgs map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{"protocolVersion": "2025-03-26", "capabilities": map[string]interface{}{}},
			})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{"tools": []map[string]interface{}{{
					"name":        "totally_new_vendor_tool",
					"description": "anything",
					"inputSchema": map[string]interface{}{
						"type":                 "object",
						"properties":           map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
						"additionalProperties": false,
					},
					"annotations": map[string]interface{}{"readOnlyHint": true},
				}}},
			})
		case "tools/call":
			var params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			calledTool = params.Name
			calledArgs = params.Arguments
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{"content": []map[string]interface{}{
					{"type": "text", "text": "vendor says: hello"},
				}},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID, "result": map[string]interface{}{},
			})
		}
	}))
	defer srv.Close()

	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MCPServers = append(cfg.MCPServers, corelib.MCPServerEntry{ID: "brand-new-mcp", Name: "brand-new-mcp", EndpointURL: srv.URL})
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	// First contact: no seeded cache, no seeded health. The probe itself must
	// publish the open-family contract.
	if err := app.mcpRegistry.HealthCheck("brand-new-mcp"); err != nil {
		t.Fatalf("health check: %v", err)
	}
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	stored := &intent.ClassificationResult{Primary: intent.LabelKnowledgeRead, Confidence: .98}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"desktop-user", "查一下上周的项目进展", "desktop", "root-exec", "turn-exec", stored,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, agentservice.CapabilityInformationLookup)
	if !ok {
		t.Fatalf("open family not planned on first discovery: unmet=%+v omitted=%+v", surface.plan.Unmet, surface.plan.Omitted)
	}
	if selection.Provider.ProviderID != "brand-new-mcp" {
		t.Fatalf("selection provider=%+v", selection.Provider)
	}
	if tool.SelectionRequiresExternalReceipt(selection) {
		t.Fatalf("open selection is receipt-bound: %+v", selection)
	}

	// Execute through the same bridge the shared loop uses.
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
	result, _ := cb.executeSemanticDynamicProviderWithContext(context.Background(), selection, `{"q":"weekly progress"}`)
	if !result.Succeeded || !strings.Contains(result.Result, "vendor says: hello") {
		t.Fatalf("execution result=%#v", result)
	}
	if calledTool != "totally_new_vendor_tool" || calledArgs["q"] != "weekly progress" {
		t.Fatalf("server saw tool=%q args=%#v", calledTool, calledArgs)
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
	blocked, err := h.semanticDynamicInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range blocked.mcpEntries {
		// Without the reviewed product identity, no entry may carry the
		// product-scoped search capability. The open family may still make
		// a tool callable for read-only retrieval; what must never happen is
		// the identity-gated product contract appearing on identity alone.
		for _, provision := range entry.Contract.Provisions {
			if provision.Capability == agentservice.CapabilityInformationSearchWeb {
				t.Fatalf("instance id contracted %s: %#v", entry.ToolName, entry.Contract)
			}
		}
	}
	server.Capability.GlobalKey = agentservice.ReviewedMCPWebSearchPrimeGlobalKey
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := app.publishReviewedMCPServerTools("web-search-prime", tools); err != nil {
		t.Fatal(err)
	}
	project, err := h.semanticDynamicInventory(context.Background())
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
	if len(search.Contract.Provisions) != 3 || search.Contract.Provisions[0].Capability != tool.CapabilityID("information.search.web") || search.Contract.Provisions[0].Quality <= 2 {
		t.Fatalf("search contract=%#v", search.Contract)
	}
	// The reviewed contract also carries the host-issued exact capability so
	// naming web_search_prime resolves this binding.
	if search.Contract.Provisions[2].Capability != agentservice.AutoMCPCapabilityIDForBinding("web-search-prime", agentservice.ReviewedMCPWebSearchPrimeTool) {
		t.Fatalf("search contract missing auto capability=%#v", search.Contract.Provisions)
	}
	// An unreviewed name is callable through the open read-only family, but it
	// must not render as the reviewed product's model function: the identity
	// join is what maps web_search onto search_query, and no description or
	// neighbouring name may reach that spelling.
	for _, provision := range extra.Contract.Provisions {
		if provision.Capability == agentservice.CapabilityInformationSearchWeb {
			t.Fatalf("description-only tool gained the product search: %#v", extra.Contract)
		}
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog(project.mcpEntries, nil)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	var product *tool.ProviderSpec
	for i := range catalog.Providers {
		if catalog.Providers[i].Binding.ImplementationID == agentservice.ReviewedMCPWebSearchPrimeTool {
			product = &catalog.Providers[i]
		}
		if tool.SemanticModelFunctionName(catalog.Providers[i].AdapterName) == "web_search" &&
			catalog.Providers[i].Binding.ImplementationID != agentservice.ReviewedMCPWebSearchPrimeTool {
			t.Fatalf("%q rendered as web_search", catalog.Providers[i].Binding.ImplementationID)
		}
	}
	if product == nil || product.Binding.ImplementationID != agentservice.ReviewedMCPWebSearchPrimeTool {
		t.Fatalf("product provider missing: %#v", catalog.Providers)
	}
	if tool.SemanticModelFunctionName(product.AdapterName) != "web_search" {
		t.Fatalf("adapter %q did not render as web_search", product.AdapterName)
	}
	params := catalog.Definitions[product.AdapterName]["function"].(map[string]interface{})["parameters"].(map[string]interface{})
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
	revoked, err := h.semanticDynamicInventory(context.Background())
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
	principal := semanticDesktopContractPrincipal()
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
	principal := semanticDesktopContractPrincipal()
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
	inventory, err := h.semanticDynamicInventory(context.Background())
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
