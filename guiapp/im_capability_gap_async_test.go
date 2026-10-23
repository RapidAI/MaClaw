package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestShouldStartAsyncCapabilityGapSearch_CompletedApi2Lookup(t *testing.T) {
	phase := &agentLoopPhase{Stage: agentStageOrient}
	if shouldStartAsyncCapabilityGapSearch(0, "api2 is the ominiroute server.", 0, 0, phase) {
		t.Fatal("completed lookup should not start async skill installation")
	}
}

func TestShouldStartAsyncCapabilityGapSearch_CompletedRememberedApi2Lookup(t *testing.T) {
	phase := &agentLoopPhase{Stage: agentStageFinalize}
	if shouldStartAsyncCapabilityGapSearch(0, "api2 \u662f ominiroute \u670d\u52a1\u5668\u3002", 0, 0, phase) {
		t.Fatal("completed remembered api2 lookup should not start async skill installation")
	}
}

func TestShouldStartAsyncCapabilityGapSearch_RecoverNoToolStall(t *testing.T) {
	phase := &agentLoopPhase{Stage: agentStageRecover, RecoverReason: agentRecoverNoToolStall}
	if !shouldStartAsyncCapabilityGapSearch(0, "I cannot handle this report format.", 0, 0, phase) {
		t.Fatal("recover no-tool stall should be eligible for capability gap search")
	}
}

func TestShouldStartAsyncCapabilityGapSearch_FinalizeWithToolCalls(t *testing.T) {
	phase := &agentLoopPhase{Stage: agentStageFinalize}
	if shouldStartAsyncCapabilityGapSearch(1, "server status is healthy.", 0, 2, phase) {
		t.Fatal("successful finalize after tool calls should not start async skill installation")
	}
}

func TestFilterSkillSearchResultsForIntent_RemovesIncompatibleCandidates(t *testing.T) {
	results := []SkillSearchResult{
		{Name: "Api Design Reviewer", Description: "Generates and reviews API design documents."},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterSkillSearchResultsForIntent("check api2 server status", results)
	if len(filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0].Name != "Server Status Search" {
		t.Fatalf("kept %q, want Server Status Search", filtered[0].Name)
	}
}

func TestFilterSkillSearchResultsForIntent_UsesOriginalTaskNotSearchQuery(t *testing.T) {
	searchQuery := "api design review"
	taskText := "check api2 server status"
	results := []SkillSearchResult{
		{Name: "Api Design Reviewer", Description: "Generates and reviews API design documents."},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}

	if got := filterSkillSearchResultsForIntent(searchQuery, results); len(got) != 2 {
		t.Fatalf("search query should be ambiguous enough to keep both candidates, got %d", len(got))
	}
	filtered := filterSkillSearchResultsForIntent(taskText, results)
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("task intent filter kept %#v, want only Server Status Search", filtered)
	}
}

func TestSkipUnrunnableInstalledKeepsALaterRunnableHit(t *testing.T) {
	shop := corelib.NLSkillEntry{
		Name:     "域名测试",
		Status:   "needs_setup",
		Triggers: []string{"kwaishop-intelligent-diagnosis-skill_test"},
	}
	progress := corelib.NLSkillEntry{
		Name:     "强制进度检查点。当龙虾助手在执行任务时超过60秒没有完成，必须立即向用户发送进度反馈",
		Status:   "needs_setup",
		Triggers: []string{"long-task-checkin"},
	}
	results := []SkillSearchResult{
		{ID: "kwaishop-intelligent-diagnosis-skill_test", Name: "域名测试", Description: "域名测试"},
		{ID: "long-task-checkin", Name: "long-task-checkin", Description: "progress"},
		{ID: "server-status", Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	got := skipUnrunnableInstalled([]corelib.NLSkillEntry{shop, progress}, results)
	if len(got) != 1 || got[0].Name != "Server Status Search" {
		t.Fatalf("kept %#v", got)
	}
	shop.Status = "active"
	kept := skipUnrunnableInstalled([]corelib.NLSkillEntry{shop}, results[:1])
	if len(kept) != 1 || kept[0].Name != "域名测试" {
		t.Fatalf("active install was skipped: %#v", kept)
	}
}

func TestFilterSkillSearchResultsForIntent_DropsUndescribedSkillForServerQuery(t *testing.T) {
	shop := SkillSearchResult{Name: "kwaishop-intelligent-diagnosis-skill_test", Description: "域名测试"}
	server := SkillSearchResult{Name: "Server Status Search", Description: "Searches server status and lists health checks."}
	filtered := filterSkillSearchResultsForIntent("查询 api2服务器信息", []SkillSearchResult{shop, server})
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("server query kept %#v, want only Server Status Search", filtered)
	}
	// The filter compacts its input in place. Pass a fresh slice so the
	// only-shop case is not the survivor of the call above.
	onlyShop := filterSkillSearchResultsForIntent("查询 api2服务器信息", []SkillSearchResult{shop})
	if len(onlyShop) != 0 {
		t.Fatalf("shop diagnosis kept %#v, want none", onlyShop)
	}
}

func TestFilterSkillSearchResultsForIntent_ChineseQueryIntent(t *testing.T) {
	results := []SkillSearchResult{
		{Name: "Api Design Reviewer", Description: "Generates and reviews API design documents."},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterSkillSearchResultsForIntent("\u67e5\u8be2 api2 \u670d\u52a1\u5668\u60c5\u51b5", results)
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("Chinese query intent filter kept %#v, want only Server Status Search", filtered)
	}
}

func TestFilterSkillSearchResultsForIntent_RememberedServerQueryIntent(t *testing.T) {
	results := []SkillSearchResult{
		{Name: "Api Design Reviewer", Description: "Generates and reviews API design documents."},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterSkillSearchResultsForIntent("\u8fd8\u8bb0\u5f97 api2 \u670d\u52a1\u5668\u5417\uff1f", results)
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("remembered server query filter kept %#v, want only Server Status Search", filtered)
	}
}

func TestFilterSkillSearchResultsForIntent_EnglishRememberedServerQueryIntent(t *testing.T) {
	results := []SkillSearchResult{
		{Name: "Api Design Reviewer", Description: "Generates and reviews API design documents."},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterSkillSearchResultsForIntent("remember api2 server?", results)
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("English remembered server query filter kept %#v, want only Server Status Search", filtered)
	}
}

func TestFilterSkillSearchResultsForIntent_RemovesAPIDesignDomainForServerQuery(t *testing.T) {
	results := []SkillSearchResult{
		{Name: "Api Design Reviewer", Description: "API Design Reviewer"},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterSkillSearchResultsForIntent("remember api2 server?", results)
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("server query domain filter kept %#v, want only Server Status Search", filtered)
	}
}

func TestFilterSkillSearchResultsForIntent_ChineseInspectServerQueryIntent(t *testing.T) {
	for _, query := range []string{
		"\u770b\u770b api2 \u670d\u52a1\u5668",
		"\u68c0\u67e5 api2 \u670d\u52a1\u5668\u72b6\u6001",
	} {
		results := []SkillSearchResult{
			{Name: "Api Design Reviewer", Description: "API Design Reviewer"},
			{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
		}
		filtered := filterSkillSearchResultsForIntent(query, results)
		if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
			t.Fatalf("inspect server query %q kept %#v, want only Server Status Search", query, filtered)
		}
	}
}

func TestFilterHubSkillMetaForIntent_RemovesIncompatibleCandidates(t *testing.T) {
	candidates := []HubSkillMeta{
		{Name: "Api Design Reviewer", Description: "Generates and reviews API design documents."},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterHubSkillMetaForIntent("\u67e5\u8be2 api2 \u670d\u52a1\u5668\u60c5\u51b5", candidates)
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("hub intent filter kept %#v, want only Server Status Search", filtered)
	}
}

func TestFilterHubSkillMetaForIntent_RemovesAPIDesignDomainForServerQuery(t *testing.T) {
	candidates := []HubSkillMeta{
		{Name: "Api Design Reviewer", Description: "API Design Reviewer"},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterHubSkillMetaForIntent("\u8fd8\u8bb0\u5f97 api2 \u670d\u52a1\u5668\u5417\uff1f", candidates)
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("hub server query domain filter kept %#v, want only Server Status Search", filtered)
	}
}

func TestFilterGitHubSkillCandidatesForIntent_RemovesIncompatibleCandidates(t *testing.T) {
	candidates := []cskill.GitHubSkillCandidate{
		{RepoFullName: "example/api-design-reviewer", Description: "Generates and reviews API design documents."},
		{RepoFullName: "example/server-status-search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterGitHubSkillCandidatesForIntent("\u67e5\u8be2 api2 \u670d\u52a1\u5668\u60c5\u51b5", candidates)
	if len(filtered) != 1 || filtered[0].RepoFullName != "example/server-status-search" {
		t.Fatalf("github intent filter kept %#v, want only server-status-search", filtered)
	}
}

func TestFilterGitHubSkillCandidatesForIntent_RemovesAPIDesignDomainForServerQuery(t *testing.T) {
	candidates := []cskill.GitHubSkillCandidate{
		{RepoFullName: "example/api-design-reviewer", Description: "API Design Reviewer"},
		{RepoFullName: "example/server-status-search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterGitHubSkillCandidatesForIntent("remember api2 server?", candidates)
	if len(filtered) != 1 || filtered[0].RepoFullName != "example/server-status-search" {
		t.Fatalf("github server query domain filter kept %#v, want only server-status-search", filtered)
	}
}

func TestIntentCandidateFilters_EmptyInput(t *testing.T) {
	if got := filterHubSkillMetaForIntent("check api2 server status", nil); got != nil {
		t.Fatalf("filterHubSkillMetaForIntent(nil) = %#v, want nil", got)
	}
	if got := filterGitHubSkillCandidatesForIntent("check api2 server status", nil); got != nil {
		t.Fatalf("filterGitHubSkillCandidatesForIntent(nil) = %#v, want nil", got)
	}
}

func TestSearchAndInstallForTask_EmptyQueryDoesNotSearch(t *testing.T) {
	searcher := NewSkillSearcher(nil)
	best, err := searcher.SearchAndInstallForTask(context.Background(), "   ", "check api2 server status")
	if err != nil {
		t.Fatalf("SearchAndInstallForTask() error = %v", err)
	}
	if best != nil {
		t.Fatalf("best = %#v, want nil", best)
	}
}

func TestToolSearchAndInstallSkillRejectsHostAdmittedLookupAndContinuation(t *testing.T) {
	handler := &IMMessageHandler{}
	lookup := handler.toolSearchAndInstallSkillResultAdmitted(map[string]interface{}{"query": "查询 api2服务器信息"}, nil, true)
	if lookup.Success || !strings.Contains(lookup.Text, "information lookup") {
		t.Fatalf("host-admitted lookup = %+v", lookup)
	}
	continuation := handler.toolSearchAndInstallSkillResultAdmitted(map[string]interface{}{"query": "继续"}, nil, true)
	if continuation.Success || !strings.Contains(continuation.Text, "continues the current task") {
		t.Fatalf("host-admitted continuation = %+v", continuation)
	}
	modify := skillSearchQueryAllowed("修改这份表格的目录结构", true)
	if !modify {
		t.Fatal("host-admitted modify task was dropped")
	}
}

func TestToolSearchAndInstallSkillRejectsCapabilityNounLookup(t *testing.T) {
	handler := &IMMessageHandler{}
	lookup := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": "查询 api2 的处理能力"}, nil)
	if lookup.Success || !strings.Contains(lookup.Text, "information lookup") {
		t.Fatalf("capability noun lookup = %+v", lookup)
	}
	asked := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": "需要能力检查 api2 服务器状态"}, nil)
	if strings.Contains(asked.Text, "information lookup") || !strings.Contains(asked.Text, "app is not initialized") {
		t.Fatalf("explicit capability phrase = %+v", asked)
	}
	clean := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": "看下有没有可以清理的"}, nil)
	if clean.Success || !strings.Contains(clean.Text, "information lookup") {
		t.Fatalf("cleanup look = %+v", clean)
	}
	for _, query := range []string{"查询已安装的技能", "查找已安装的技能", "有哪些技能", "搜索已安装的技能"} {
		lookup := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": query}, nil)
		if lookup.Success || !strings.Contains(lookup.Text, "information lookup") {
			t.Fatalf("installed-skill inventory %q = %+v", query, lookup)
		}
		if skillSearchQueryAllowed(query, true) {
			t.Fatalf("host-admitted inventory %q was searched", query)
		}
	}
	for _, query := range []string{
		"找一个能改 tex 的技能",
		"寻找 pdf 技能",
		"安装 huashu-art-motion 技能",
		"需要一个能处理 pdf 的技能",
		"使用这个技能查询服务器",
	} {
		asked := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": query}, nil)
		if strings.Contains(asked.Text, "information lookup") || strings.Contains(asked.Text, "not a missing capability") || !strings.Contains(asked.Text, "app is not initialized") {
			t.Fatalf("skill request %q = %+v", query, asked)
		}
		if !skillSearchQueryAllowed(query, true) {
			t.Fatalf("host-admitted skill request %q was skipped", query)
		}
	}
}

func TestToolSearchAndInstallSkillRejectsLookupIntent(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": "还记得 api2 服务器吗？"}, nil)
	if result.Success {
		t.Fatalf("toolSearchAndInstallSkillResult() success = true, want false")
	}
	if !strings.Contains(result.Text, "information lookup") {
		t.Fatalf("toolSearchAndInstallSkillResult() text = %q, want information lookup rejection", result.Text)
	}
}

func TestToolSearchAndInstallSkillRejectsBareInfrastructureLookup(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": "api2 服务器情况"}, nil)
	if result.Success {
		t.Fatalf("toolSearchAndInstallSkillResult() success = true, want false")
	}
	if !strings.Contains(result.Text, "information lookup") {
		t.Fatalf("toolSearchAndInstallSkillResult() text = %q, want information lookup rejection", result.Text)
	}
}

func TestToolSearchAndInstallSkillRejectsToolStatusLookup(t *testing.T) {
	handler := &IMMessageHandler{}
	for _, query := range []string{"api2 server tool status", "api2 服务器工具状态"} {
		result := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": query}, nil)
		if result.Success {
			t.Fatalf("toolSearchAndInstallSkillResult(%q) success = true, want false", query)
		}
		if !strings.Contains(result.Text, "information lookup") {
			t.Fatalf("toolSearchAndInstallSkillResult(%q) text = %q, want information lookup rejection", query, result.Text)
		}
	}
}

func TestToolSearchAndInstallSkillAllowsExplicitCapabilityRequest(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": "need a skill for api2 server status checks"}, nil)
	if result.Success {
		t.Fatalf("toolSearchAndInstallSkillResult() success = true without app/search backend, want false")
	}
	if strings.Contains(result.Text, "information lookup") {
		t.Fatalf("toolSearchAndInstallSkillResult() text = %q, explicit capability request should pass lookup gate", result.Text)
	}
	if !strings.Contains(result.Text, "app is not initialized") {
		t.Fatalf("toolSearchAndInstallSkillResult() text = %q, want app initialization failure after lookup gate", result.Text)
	}
}

func TestToolSearchAndInstallSkillAllowsExplicitToolRequest(t *testing.T) {
	handler := &IMMessageHandler{}
	for _, query := range []string{"need a tool for api2 server status checks", "找个工具检查 api2 服务器状态"} {
		result := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": query}, nil)
		if result.Success {
			t.Fatalf("toolSearchAndInstallSkillResult(%q) success = true without app/search backend, want false", query)
		}
		if strings.Contains(result.Text, "information lookup") {
			t.Fatalf("toolSearchAndInstallSkillResult(%q) text = %q, explicit tool request should pass lookup gate", query, result.Text)
		}
		if !strings.Contains(result.Text, "app is not initialized") {
			t.Fatalf("toolSearchAndInstallSkillResult(%q) text = %q, want app initialization failure after lookup gate", query, result.Text)
		}
	}
}

func TestToolSearchAndInstallSkillAllowsExplicitSearchForSkill(t *testing.T) {
	handler := &IMMessageHandler{}
	result := handler.toolSearchAndInstallSkillResult(map[string]interface{}{"query": "Search for a PDF conversion skill."}, nil)
	if result.Success {
		t.Fatalf("toolSearchAndInstallSkillResult() success = true without app/search backend, want false")
	}
	if strings.Contains(result.Text, "information lookup") {
		t.Fatalf("toolSearchAndInstallSkillResult() text = %q, explicit skill search should pass lookup gate", result.Text)
	}
	if !strings.Contains(result.Text, "app is not initialized") {
		t.Fatalf("toolSearchAndInstallSkillResult() text = %q, want app initialization failure after lookup gate", result.Text)
	}
}

func TestSkillSearchCompatibilityTaskTextStripsSearchWrapper(t *testing.T) {
	if got := skillSearchCompatibilityTaskText("Search for a PDF conversion skill."); got != "pdf conversion" {
		t.Fatalf("skillSearchCompatibilityTaskText() = %q, want pdf conversion", got)
	}
	if got := skillSearchCompatibilityTaskText("need a tool for api2 server status checks"); got != "api2 server status checks" {
		t.Fatalf("skillSearchCompatibilityTaskText() = %q, want api2 server status checks", got)
	}
}

func TestExplicitSkillSearchCompatibilityDoesNotTreatSearchVerbAsTaskIntent(t *testing.T) {
	results := []SkillSearchResult{
		{Name: "Markdown To PDF", Description: "Converts Markdown documents to PDF."},
	}
	if got := filterSkillSearchResultsForIntent("Search for a PDF conversion skill.", results); len(got) != 0 {
		t.Fatalf("raw wrapper query should demonstrate old rejection path, got %#v", got)
	}
	taskText := skillSearchCompatibilityTaskText("Search for a PDF conversion skill.")
	if got := filterSkillSearchResultsForIntent(taskText, results); len(got) != 1 || got[0].Name != "Markdown To PDF" {
		t.Fatalf("stripped compatibility task kept %#v, want Markdown To PDF", got)
	}
}

func TestExplicitInfrastructureSkillSearchStillRejectsAPIDesignDomain(t *testing.T) {
	taskText := skillSearchCompatibilityTaskText("need a tool for api2 server status checks")
	results := []SkillSearchResult{
		{Name: "Api Design Reviewer", Description: "API Design Reviewer"},
		{Name: "Server Status Search", Description: "Searches server status and lists health checks."},
	}
	filtered := filterSkillSearchResultsForIntent(taskText, results)
	if len(filtered) != 1 || filtered[0].Name != "Server Status Search" {
		t.Fatalf("explicit infra skill search kept %#v, want only Server Status Search", filtered)
	}
}

func TestSearchAndInstallForTask_NilContextEmptyQueryDoesNotPanic(t *testing.T) {
	searcher := NewSkillSearcher(nil)
	best, err := searcher.SearchAndInstallForTask(nil, "   ", "check api2 server status")
	if err != nil {
		t.Fatalf("SearchAndInstallForTask() error = %v", err)
	}
	if best != nil {
		t.Fatalf("best = %#v, want nil", best)
	}
}

func TestContextErrNilSafe(t *testing.T) {
	if err := contextErr(nil); err != nil {
		t.Fatalf("contextErr(nil) = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := contextErr(ctx); err == nil {
		t.Fatal("contextErr(canceled) = nil, want error")
	}
}

func TestConfirmAsyncCapabilityGapSkillInstall_FailClosedForNilInput(t *testing.T) {
	var handler *IMMessageHandler
	if handler.confirmAsyncCapabilityGapSkillInstall(context.Background(), &SkillSearchResult{Name: "x"}, "desktop", "u", "q", "no_tool_stall") {
		t.Fatal("nil handler should fail closed")
	}
	handler = &IMMessageHandler{}
	if handler.confirmAsyncCapabilityGapSkillInstall(context.Background(), nil, "desktop", "u", "q", "no_tool_stall") {
		t.Fatal("nil skill result should fail closed")
	}
}

func TestConfirmAsyncCapabilityGapSkillInstall_FailClosedForMissingPlatform(t *testing.T) {
	handler := &IMMessageHandler{}
	if handler.confirmAsyncCapabilityGapSkillInstall(context.Background(), &SkillSearchResult{Name: "x"}, "", "u", "q", "no_tool_stall") {
		t.Fatal("missing platform should fail closed")
	}
}

func TestInstallSkillOnlyIsBlockedByCapabilityGapRolloutFence(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	result := handler.installSkillOnly(context.Background(), &SkillSearchResult{Name: "remote-skill"}, "need a skill", "desktop", "user", "user", nil)
	if result.Success || !result.SilentFailure {
		t.Fatalf("installSkillOnly unexpectedly allowed automatic mutation: %+v", result)
	}
	if !strings.Contains(result.Text, "automatic capability-gap Skill installation is disabled") {
		t.Fatalf("unexpected rollout-fence message: %q", result.Text)
	}
}

func TestAsyncCapabilityGapSkillSourceLabel(t *testing.T) {
	label := asyncCapabilityGapSkillSourceLabel(&SkillSearchResult{Name: "x", Status: skillSearchSourceClawHub})
	if label != "background capability repair via clawhub" {
		t.Fatalf("label = %q", label)
	}
}

func TestLogAsyncCapabilityGapInstallDecision(t *testing.T) {
	auditLog, err := NewAuditLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewAuditLog() error = %v", err)
	}
	defer auditLog.Close()

	handler := &IMMessageHandler{app: &App{auditLog: auditLog}}
	best := &SkillSearchResult{Name: "Api Design Reviewer", Status: skillSearchSourceClawHub}
	handler.logAsyncCapabilityGapInstallDecision(best, "background capability repair via clawhub", true, "api design review", "no_tool_stall")
	handler.logAsyncCapabilityGapInstallDecision(best, "background capability repair via clawhub", false, "api design review", "no_tool_stall")

	installEntries, err := auditLog.Query(security.AuditFilter{Action: security.AuditActionHubSkillInstall})
	if err != nil {
		t.Fatalf("query install audit: %v", err)
	}
	if len(installEntries) != 1 || installEntries[0].PolicyAction != security.PolicyUserOverride {
		t.Fatalf("install audit entries = %#v", installEntries)
	}
	if installEntries[0].Arguments["search_query"] != "api design review" || installEntries[0].Arguments["recover_reason"] != "no_tool_stall" {
		t.Fatalf("install audit args = %#v", installEntries[0].Arguments)
	}
	rejectEntries, err := auditLog.Query(security.AuditFilter{Action: security.AuditActionHubSkillReject})
	if err != nil {
		t.Fatalf("query reject audit: %v", err)
	}
	if len(rejectEntries) != 1 || rejectEntries[0].PolicyAction != security.PolicyDeny {
		t.Fatalf("reject audit entries = %#v", rejectEntries)
	}
}

func TestSearchAndInstallForTask_RespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	searcher := NewSkillSearcher(nil)
	best, err := searcher.SearchAndInstallForTask(ctx, "api design review", "check api2 server status")
	if err == nil {
		t.Fatal("expected canceled context error")
	}
	if best != nil {
		t.Fatalf("best = %#v, want nil", best)
	}
}

func TestAsyncCapabilityGapSearchQuery_FallsBackToUserTextWithoutDetector(t *testing.T) {
	query := asyncCapabilityGapSearchQuery(context.Background(), nil, "  check api2 server status  ", "I cannot access this server.")
	if query != "check api2 server status" {
		t.Fatalf("query = %q, want trimmed user text", query)
	}
}

func TestAsyncCapabilityGapSearchQuery_RespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	query := asyncCapabilityGapSearchQuery(ctx, nil, "check api2 server status", "I cannot access this server.")
	if query != "" {
		t.Fatalf("query = %q, want empty", query)
	}
}

func TestExtractCapabilityQuery_RespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	detector := &CapabilityGapDetector{}
	if query := detector.extractCapabilityQuery(ctx, "check api2 server status", nil); query != "" {
		t.Fatalf("query = %q, want empty", query)
	}
}

func TestLLMSelectBestSkill_RespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	detector := &CapabilityGapDetector{}
	chosen := detector.llmSelectBestSkill(ctx, "check api2 server status", []HubSkillMeta{
		{Name: "Api Design Reviewer", Description: "Generates and reviews API design documents."},
	})
	if chosen != nil {
		t.Fatalf("chosen = %#v, want nil", chosen)
	}
}

func TestCapabilityGapDetectorDetectWithContext_KeywordFallback(t *testing.T) {
	detector := &CapabilityGapDetector{}
	if !detector.DetectWithContext(context.Background(), "I cannot process this format.") {
		t.Fatal("keyword fallback should detect capability gap without LLM config")
	}
}

func TestCapabilityGapDetectorDetectWithContext_RespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	detector := &CapabilityGapDetector{}
	if detector.DetectWithContext(ctx, "I cannot process this format.") {
		t.Fatal("canceled context should suppress capability gap detection")
	}
}
