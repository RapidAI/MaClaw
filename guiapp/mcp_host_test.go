package guiapp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestInboundMCPHostDisabledByDefault(t *testing.T) {
	var cfg corelib.AppConfig
	if cfg.IsMcpHostEnabled() {
		t.Fatal("nil MCP switch must stay off")
	}
	cfg.SetMcpHostEnabled(false)
	if cfg.IsMcpHostEnabled() {
		t.Fatal("explicit false must stay off")
	}
	cfg.SetMcpHostEnabled(true)
	if !cfg.IsMcpHostEnabled() {
		t.Fatal("explicit true must turn the server on")
	}
	if cfg.PreferredMcpHostPort() != 0 {
		t.Fatal("unset port must ask the listener to try 18790")
	}
}

func TestInboundMCPAssistantHostProfile(t *testing.T) {
	if assistantHostProfile("") != assistantHostACP || assistantHostProfile("acp") != assistantHostACP {
		t.Fatal("empty profile must stay on ACP")
	}
	if assistantHostProfile(" MCP ") != assistantHostMCP {
		t.Fatal("mcp profile was not recognized")
	}
	id := mcpProgrammingRuntimeTaskID("desktop-user:C:/work", `C:\work`, "mcp-1")
	if !strings.HasPrefix(id, "mcp-coding-") {
		t.Fatalf("runtime id %q", id)
	}
	if id == acpProgrammingRuntimeTaskID("desktop-user:C:/work", `C:\work`, "mcp-1") {
		t.Fatal("MCP and ACP runtime ids must differ")
	}
	wrapped := mcpTaskUserText(`D:\repo`, "实现登录")
	if strings.Contains(wrapped, "[VS Code / ACP programming workspace]") || !strings.Contains(wrapped, "[MCP task]") {
		t.Fatalf("wrapper %q", wrapped)
	}
	if acpInnerUserRequest(wrapped) != wrapped {
		t.Fatal("ACP continue detection must leave the MCP preamble in place")
	}
}

func TestInboundMCPNormalizeTaskMode(t *testing.T) {
	mode, err := mcpNormalizeTaskMode("", "")
	if err != nil || mode != "chat" {
		t.Fatalf("empty mode = %s %v", mode, err)
	}
	mode, err = mcpNormalizeTaskMode("", `D:\repo`)
	if err != nil || mode != taskCodingDevTag {
		t.Fatalf("working dir without mode = %s %v", mode, err)
	}
	mode, err = mcpNormalizeTaskMode("chat", `D:\repo`)
	if err != nil || mode != "chat" {
		t.Fatalf("explicit chat = %s %v", mode, err)
	}
	mode, err = mcpNormalizeTaskMode("programming", "")
	if err != nil || mode != taskCodingDevTag {
		t.Fatalf("programming alias = %s %v", mode, err)
	}
	if _, err := mcpNormalizeTaskMode("remote", ""); err == nil {
		t.Fatal("unknown mode must fail")
	}
}

func TestInboundMCPDisplayStatus(t *testing.T) {
	if got := mcpDisplayStatus(mcpStatusCancelled, "completed", "done"); got != mcpStatusCancelled {
		t.Fatalf("cancelled lost to %s", got)
	}
	if got := mcpDisplayStatus("", "queued", ""); got != mcpStatusRunning {
		t.Fatalf("queued = %s", got)
	}
	if got := mcpDisplayStatus("", "paused", ""); got != mcpStatusRunning {
		t.Fatalf("paused = %s", got)
	}
	if got := mcpDisplayStatus("", "failed", "partial"); got != mcpStatusFailed {
		t.Fatalf("failed = %s", got)
	}
	if got := mcpDisplayStatus("", "done", ""); got != mcpStatusCompleted {
		t.Fatalf("done = %s", got)
	}
	if got := mcpDisplayStatus("", "", "最后的回答"); got != mcpStatusCompleted {
		t.Fatalf("assistant text = %s", got)
	}
	if got := mcpDisplayStatus("", "", ""); got != mcpStatusIdle {
		t.Fatalf("idle = %s", got)
	}
	if got := mcpDisplayStatus(mcpStatusNeedsConfirmation, "done", "请确认"); got != mcpStatusNeedsConfirmation {
		t.Fatalf("confirmation lost to %s", got)
	}
}

func TestInboundMCPConfirmationIsNotCompleted(t *testing.T) {
	status, text, errText, keepText := mcpRunOutcome(nil, false, &IMAgentResponse{
		Text:         "请在窗口确认",
		Confirmation: &IMResponseConfirmation{Summary: "实现登录"},
	})
	if status != mcpStatusNeedsConfirmation || text != "请在窗口确认" || errText != "" || keepText {
		t.Fatalf("confirmation status=%s text=%q err=%q keep=%v", status, text, errText, keepText)
	}
	status, text, _, _ = mcpRunOutcome(nil, false, &IMAgentResponse{Confirmation: &IMResponseConfirmation{Summary: "实现登录"}})
	if status != mcpStatusNeedsConfirmation || text != "实现登录" {
		t.Fatalf("summary fallback status=%s text=%q", status, text)
	}
	status, text, errText, _ = mcpRunOutcome(nil, true, &IMAgentResponse{Text: "请确认", Confirmation: &IMResponseConfirmation{Summary: "实现登录"}})
	if status != mcpStatusCancelled || errText != "" || text != "请确认" {
		t.Fatalf("cancel must win status=%s text=%q err=%q", status, text, errText)
	}
	status, text, errText, _ = mcpRunOutcome(nil, false, &IMAgentResponse{Text: "写好了"})
	if status != mcpStatusCompleted || text != "写好了" || errText != "" {
		t.Fatalf("answer status=%s text=%q err=%q", status, text, errText)
	}
	status, _, errText, _ = mcpRunOutcome(nil, false, &IMAgentResponse{Text: "失败说明", Error: "模型失败"})
	if status != mcpStatusFailed || errText != "模型失败" {
		t.Fatalf("error status=%s err=%q", status, errText)
	}
}

func TestInboundMCPConfirmationCommandsAreOperable(t *testing.T) {
	plan := &IMAgentResponse{Text: "需要确认执行计划", Actions: codingPlanApproveActions()}
	status, text, errText, _ := mcpRunOutcome(nil, false, plan)
	status, text, kind, commands := mcpConfirmationCallerNotice(status, text, plan)
	if status != mcpStatusNeedsConfirmation || kind != mcpConfirmKindPlan || errText != "" || !strings.Contains(text, "send_command") {
		t.Fatalf("plan status=%s kind=%s text=%q err=%q", status, kind, text, errText)
	}
	if len(commands) < 3 || commands[0] != "/plan approve" {
		t.Fatalf("plan commands %v", commands)
	}

	card := &IMAgentResponse{Text: "请确认", Confirmation: &IMResponseConfirmation{ID: "card-1", Summary: "写文件"}}
	status, text, _, _ = mcpRunOutcome(nil, false, card)
	status, text, kind, commands = mcpConfirmationCallerNotice(status, text, card)
	if status != mcpStatusNeedsConfirmation || kind != "confirm" || !strings.Contains(text, "send_command") {
		t.Fatalf("card status=%s kind=%s text=%q", status, kind, text)
	}
	if !containsAll(commands, "__confirm_execution__ card-1", "__cancel_execution__ card-1") {
		t.Fatalf("card commands %v", commands)
	}

	asked := &IMAgentResponse{Text: "先停一下", AskUserQuestion: "用哪个端口？"}
	status, text, kind, commands = mcpConfirmationCallerNotice(mcpStatusCompleted, "先停一下", asked)
	if status != mcpStatusNeedsConfirmation || kind != "ask" || !strings.Contains(text, "用哪个端口") || len(commands) != 0 {
		t.Fatalf("ask status=%s kind=%s text=%q commands=%v", status, kind, text, commands)
	}

	secret := &IMAgentResponse{Text: "登录", AskUserSecretName: "SITE_PASSWORD"}
	status, text, kind, commands = mcpConfirmationCallerNotice(mcpStatusCompleted, "登录", secret)
	if status != mcpStatusNeedsConfirmation || kind != "ask" || !strings.Contains(text, "SITE_PASSWORD") || len(commands) != 0 {
		t.Fatalf("secret status=%s kind=%s text=%q commands=%v", status, kind, text, commands)
	}

	desktop := &IMAgentResponse{Text: "停在桌面", DesktopUserControl: true, DesktopAttentionReason: "验证码"}
	status, text, kind, commands = mcpConfirmationCallerNotice(mcpStatusCompleted, "停在桌面", desktop)
	if status != mcpStatusNeedsConfirmation || kind != "desktop" || !strings.Contains(text, "验证码") || !strings.Contains(text, "send_command") || len(commands) != 0 {
		t.Fatalf("desktop status=%s kind=%s text=%q commands=%v", status, kind, text, commands)
	}

	traced := &IMAgentResponse{Text: "没有可见结果", Actions: []IMResponseAction{{Command: "__view_trace__ run-1"}, {Command: "__wf_review__ supplement_focus"}}}
	status, _, kind, commands = mcpConfirmationCallerNotice(mcpStatusCompleted, "没有可见结果", traced)
	if status != mcpStatusCompleted || kind != "" || len(commands) != 0 {
		t.Fatalf("trace is not a confirmation status=%s kind=%s commands=%v", status, kind, commands)
	}
	review := &IMAgentResponse{Text: "请审阅文档", Actions: []IMResponseAction{{Command: "__wf_review__ confirm"}, {Command: "__wf_review__ supplement_focus"}, {Command: "__wf_review__ abort"}}}
	status, _, kind, commands = mcpConfirmationCallerNotice(mcpStatusCompleted, "请审阅文档", review)
	if status != mcpStatusNeedsConfirmation || kind != "action" || !containsAll(commands, "__wf_review__ confirm", "__wf_review__ abort") || containsAll(commands, "__wf_review__ supplement_focus") {
		t.Fatalf("review status=%s kind=%s commands=%v", status, kind, commands)
	}

	recovery := &IMAgentResponse{CodingRuntimeRecovery: &CodingRuntimeRecoveryReview{TaskID: "task-1", ReviewDigest: "sha256:abc"}}
	_, _, kind, commands = mcpConfirmationCallerNotice(mcpStatusCompleted, "请审阅", recovery)
	if kind != "recovery" || !containsAll(commands, "__coding_recovery_confirm__ task-1 sha256:abc", "__coding_recovery_decline__ task-1 sha256:abc") {
		t.Fatalf("recovery kind=%s commands=%v", kind, commands)
	}

	status, _, kind, commands = mcpConfirmationCallerNotice(mcpStatusCancelled, "停了", plan)
	if status != mcpStatusCancelled || kind != "" || commands != nil {
		t.Fatalf("cancel must win status=%s kind=%s commands=%v", status, kind, commands)
	}

	if !mcpCommandSkipsWorkspaceWrap("/plan approve") || !mcpCommandSkipsWorkspaceWrap("__confirm_execution__ card-1") || !mcpCommandSkipsWorkspaceWrap("／plan reject") {
		t.Fatal("commands must stay raw")
	}
	if mcpCommandSkipsWorkspaceWrap("把按钮改成蓝色") {
		t.Fatal("prose is a follow-up, not a button token")
	}
	bound := mcpBindActionCommand("__resume_unfinished__ slot-9")
	if !bound.Bound || bound.ResumeSlotID != "slot-9" || !bound.UIAction || bound.Text == "__resume_unfinished__ slot-9" {
		t.Fatalf("resume binding %+v", bound)
	}
	dismiss := mcpBindActionCommand("__dismiss_recoverable_session__ sess-2")
	if dismiss.DismissRecoverableSessionID != "sess-2" || !dismiss.UIAction {
		t.Fatalf("session binding %+v", dismiss)
	}
	reviewBind := mcpBindActionCommand("__wf_review__ confirm")
	if !reviewBind.Bound || !reviewBind.UIAction || reviewBind.Text != "__wf_review__ confirm" {
		t.Fatalf("workflow review binding %+v", reviewBind)
	}

	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-live", Workspace: `D:\repo`})
	if _, err := app.mcpSendCommand(context.Background(), "p", "/plan approve", false); err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("running send: %v", err)
	}

	store := newAIConfirmationStore("")
	owner := projectSessionOwnerID("p")
	store.set(&pendingConfirmation{UserID: owner, ID: "card-9", Summary: "写文件"})
	app.imHandler = &IMMessageHandler{confirmationStore: store}
	app.imHandler.stickyCodingWorkbenchMemory.Store(stickyCodingMemoryKey(owner), stickyCodingWorkbenchMemory{
		PendingPlanJSON:     `{"tasks":[{"index":1}]}`,
		PendingPlanMarkdown: "## 改登录",
	})
	app.imHandler.pendingAskUser.Store(owner, &pendingAskUserState{Question: "用哪个端口？", Options: []string{"8080"}})
	view := app.mcpApplyLiveConfirmation(mcpTaskView{ProjectPath: "p", Status: mcpStatusCompleted, Text: "计划写好了"})
	if view.Status != mcpStatusNeedsConfirmation || view.ConfirmationKind != mcpConfirmKindPlan {
		t.Fatalf("live %+v", view)
	}
	if !containsAll(view.Commands, "/plan approve", "__confirm_execution__ card-9", "__cancel_execution__ card-9") {
		t.Fatalf("live commands %v", view.Commands)
	}
	if !strings.Contains(view.Text, "## 改登录") || !strings.Contains(view.Text, "用哪个端口") || !strings.Contains(view.Text, "8080") || !strings.Contains(view.Text, "send_command") {
		t.Fatalf("live text %q", view.Text)
	}
	running := app.mcpApplyLiveConfirmation(mcpTaskView{ProjectPath: "p", Status: mcpStatusRunning, Text: "跑着"})
	if running.Status != mcpStatusRunning || running.ConfirmationKind != "" {
		t.Fatalf("running view %+v", running)
	}

	scopeCh := make(chan ScopeApprovalDecision, 1)
	scopeID := storePendingScopeApproval(nil, ScopeApprovalRequest{
		ProjectPath: "p",
		ToolName:    "write",
		Path:        "outside.txt",
		Message:     "要写仓库外的文件",
	}, scopeCh)
	t.Cleanup(func() { pendingScopeApprovals.Delete(scopeID) })
	otherCh := make(chan ScopeApprovalDecision, 1)
	otherID := storePendingScopeApproval(nil, ScopeApprovalRequest{ProjectPath: "other", Message: "别的任务"}, otherCh)
	t.Cleanup(func() { pendingScopeApprovals.Delete(otherID) })
	workCh := make(chan ScopeApprovalDecision, 1)
	workID := storePendingScopeApproval(nil, ScopeApprovalRequest{ProjectPath: `D:\repo`, Message: "工作目录越界"}, workCh)
	t.Cleanup(func() { pendingScopeApprovals.Delete(workID) })
	nestedCh := make(chan ScopeApprovalDecision, 1)
	nestedID := storePendingScopeApproval(nil, ScopeApprovalRequest{ProjectPath: `D:\repo\child`, Message: "子目录"}, nestedCh)
	t.Cleanup(func() { pendingScopeApprovals.Delete(nestedID) })
	highCh := make(chan ScopeApprovalDecision, 1)
	highID := storePendingScopeApproval(nil, ScopeApprovalRequest{
		ProjectPath: "p",
		Kind:        localHighRiskApprovalKind,
		Message:     "高风险命令",
	}, highCh)
	t.Cleanup(func() { pendingScopeApprovals.Delete(highID) })
	commitCh := make(chan ScopeApprovalDecision, 1)
	commitID := storePendingScopeApproval(nil, ScopeApprovalRequest{
		ProjectPath: "p",
		Kind:        commitShapeApprovalKind,
		Message:     "提交形态",
	}, commitCh)
	t.Cleanup(func() { pendingScopeApprovals.Delete(commitID) })
	gated, err := app.mcpGetTask(context.Background(), "p")
	if err != nil || gated.Status != mcpStatusRunning || gated.ConfirmationKind != "scope" || !strings.Contains(gated.Text, "要写仓库外的文件") || !strings.Contains(gated.Text, "工作目录越界") || !strings.Contains(gated.Text, "send_command") {
		t.Fatalf("running gate %+v err=%v", gated, err)
	}
	if !containsAll(gated.Commands,
		"__scope_approval__ "+scopeID+" allow_dir",
		"__scope_approval__ "+scopeID+" full_access",
		"__scope_approval__ "+scopeID+" deny",
		"__scope_approval__ "+workID+" allow_dir",
		"__scope_approval__ "+workID+" deny",
		"__scope_approval__ "+highID+" allow_once",
		"__scope_approval__ "+highID+" full_access",
		"__scope_approval__ "+highID+" deny",
		"__scope_approval__ "+commitID+" allow_once",
		"__scope_approval__ "+commitID+" allow_task",
		"__scope_approval__ "+commitID+" deny",
	) || commandsContainAny(gated.Commands,
		"__scope_approval__ "+scopeID+" allow_once",
		"__scope_approval__ "+scopeID+" allow_task",
		"__scope_approval__ "+workID+" allow_once",
		"__scope_approval__ "+highID+" allow_dir",
		"__scope_approval__ "+highID+" allow_task",
		"__scope_approval__ "+commitID+" allow_dir",
		"__scope_approval__ "+commitID+" full_access",
		"__scope_approval__ "+otherID+" deny",
		"__scope_approval__ "+nestedID+" deny",
	) {
		t.Fatalf("scope commands %v", gated.Commands)
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+scopeID+" allow_task", false); err == nil || !strings.Contains(err.Error(), "没有这个决定") {
		t.Fatalf("path allow_task: %v", err)
	}
	select {
	case decision := <-scopeCh:
		t.Fatalf("rejected path decision delivered %q", decision)
	default:
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+highID+" allow_dir", false); err == nil || !strings.Contains(err.Error(), "没有这个决定") {
		t.Fatalf("high-risk allow_dir: %v", err)
	}
	select {
	case decision := <-highCh:
		t.Fatalf("rejected high-risk decision delivered %q", decision)
	default:
	}
	resolved, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+scopeID+" allow_dir", false)
	if err != nil || resolved.Status != mcpStatusRunning || !strings.Contains(resolved.Text, "已提交授权决定") {
		t.Fatalf("scope resolve %+v err=%v", resolved, err)
	}
	select {
	case decision := <-scopeCh:
		if decision != ScopeApprovalAllowDir {
			t.Fatalf("scope decision %q", decision)
		}
	default:
		t.Fatal("scope decision was not delivered")
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "/plan approve", false); err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("ordinary command while running: %v", err)
	}
	pendingScopeApprovals.Delete(workID)
	pendingScopeApprovals.Delete(nestedID)
	pendingScopeApprovals.Delete(otherID)
	pendingScopeApprovals.Delete(highID)
	pendingScopeApprovals.Delete(commitID)

	critCh := make(chan criticalRiskConfirmResponse, 1)
	app.imHandler.pendingCriticalConfirm.Store("crit_mcp", &pendingCriticalConfirmEntry{Ch: critCh, Prompt: "安装危险技能", Owner: owner})
	t.Cleanup(func() { app.imHandler.pendingCriticalConfirm.Delete("crit_mcp") })
	foreignCh := make(chan criticalRiskConfirmResponse, 1)
	app.imHandler.pendingCriticalConfirm.Store("crit_other", &pendingCriticalConfirmEntry{Ch: foreignCh, Prompt: "别人的安装", Owner: "desktop-user:other"})
	t.Cleanup(func() { app.imHandler.pendingCriticalConfirm.Delete("crit_other") })
	gated, err = app.mcpGetTask(context.Background(), "p")
	if err != nil || gated.ConfirmationKind != "critical" || !containsAll(gated.Commands, "__resolve_critical_confirm__ crit_mcp confirm", "__resolve_critical_confirm__ crit_mcp reject") || containsAll(gated.Commands, "__resolve_critical_confirm__ crit_other confirm") {
		t.Fatalf("critical gate %+v err=%v", gated, err)
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__resolve_critical_confirm__ crit_other reject", false); err == nil {
		t.Fatal("foreign critical confirm was accepted")
	}
	select {
	case <-foreignCh:
		t.Fatal("foreign critical confirm was delivered")
	default:
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__resolve_critical_confirm__ crit_mcp confirm", false); err != nil {
		t.Fatalf("critical resolve: %v", err)
	}
	select {
	case resp := <-critCh:
		if !resp.Confirmed {
			t.Fatal("critical confirm was rejected")
		}
	default:
		t.Fatal("critical confirm was not delivered")
	}
}

func containsAll(commands []string, want ...string) bool {
	have := map[string]struct{}{}
	for _, command := range commands {
		have[command] = struct{}{}
	}
	for _, command := range want {
		if _, ok := have[command]; !ok {
			return false
		}
	}
	return true
}

func commandsContainAny(commands []string, want ...string) bool {
	have := map[string]struct{}{}
	for _, command := range commands {
		have[command] = struct{}{}
	}
	for _, command := range want {
		if _, ok := have[command]; ok {
			return true
		}
	}
	return false
}

func TestInboundMCPFinishDoesNotOverwriteCancel(t *testing.T) {
	app := &App{}
	app.mcpReplaceRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-1"})
	app.mcpCancelRun("p")
	app.mcpFinishRun("p", "mcp-1", mcpStatusCompleted, "late", "", false, "", nil)
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusCancelled {
		t.Fatalf("status %+v ok=%v", view, ok)
	}
}

func TestInboundMCPConfirmationWaitClosesOnCancelAndReplace(t *testing.T) {
	app := &App{}
	store := newAIConfirmationStore("")
	app.imHandler = &IMMessageHandler{confirmationStore: store}
	owner := projectSessionOwnerID("p")
	store.set(&pendingConfirmation{UserID: owner, Summary: "请确认"})

	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-wait", Text: "请确认"})
	app.mcpCancelRun("p")
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusCancelled || view.RequestID != "mcp-wait" || view.Text != "请确认" {
		t.Fatalf("cancel view %+v ok=%v", view, ok)
	}
	if store.get(owner) == nil {
		t.Fatal("cancel alone dropped the card")
	}
	app.mcpDismissPendingConfirmation("p")
	if store.get(owner) != nil {
		t.Fatal("dismiss left the card")
	}

	store.set(&pendingConfirmation{UserID: owner, Summary: "再次确认"})
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-wait-2"})
	app.mcpReplaceRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-next"})
	view, ok = app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusRunning || view.RequestID != "mcp-next" {
		t.Fatalf("replace view %+v ok=%v", view, ok)
	}
	if store.get(owner) != nil {
		t.Fatal("replace left the old confirmation")
	}

	sameFired := false
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "same",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-same",
		cancel:      func() { sameFired = true },
	})
	app.mcpReplaceRun(&mcpRunRecord{ProjectPath: "same", Status: mcpStatusRunning, RequestID: "mcp-same"})
	if sameFired {
		t.Fatal("same request cancelled itself")
	}

	liveFired := false
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "q",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-live",
		cancel:      func() { liveFired = true },
	})
	app.mcpReplaceRun(&mcpRunRecord{ProjectPath: "q", Status: mcpStatusRunning, RequestID: "mcp-live-2"})
	view, ok = app.mcpViewFromRegistry("q")
	if !ok || view.Status != mcpStatusRunning || view.RequestID != "mcp-live-2" || !liveFired {
		t.Fatalf("live replace view %+v ok=%v fired=%v", view, ok, liveFired)
	}
}

func TestInboundMCPCatalogSkipsUntestedAndRequiresProvider(t *testing.T) {
	providers := []corelib.MaclawLLMProvider{
		{ID: "p-tested", Name: "Tested", Model: "alpha", Models: []string{"alpha", "beta"}, ConnectionTestPassed: true, Key: "secret", URL: "http://secret"},
		{ID: "p-current", Name: "Daily", Model: "daily-model", ConnectionTestPassed: false, Key: "secret-2"},
		{Name: "Hidden", Model: "hidden-model", ConnectionTestPassed: false, Key: "nope"},
		{Name: "Empty", ConnectionTestPassed: true},
	}
	catalog := mcpModelCatalogFrom(providers, "Daily")
	if catalog.Current != "Daily" || len(catalog.Providers) != 2 {
		t.Fatalf("catalog %+v", catalog)
	}
	raw, _ := json.Marshal(catalog)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "http://") {
		t.Fatalf("catalog leaked secrets: %s", raw)
	}
	name, model, err := mcpResolveProviderModel(providers, "Daily", "p-tested", "")
	if err != nil || name != "Tested" || model != "alpha" {
		t.Fatalf("default model %s %s %v", name, model, err)
	}
	name, model, err = mcpResolveProviderModel(providers, "Daily", "daily", "daily-model")
	if err != nil || name != "Daily" || model != "daily-model" {
		t.Fatalf("current provider %s %s %v", name, model, err)
	}
	if _, _, err := mcpResolveProviderModel(providers, "Daily", "", "alpha"); err == nil {
		t.Fatal("model without provider must fail")
	}
	if _, _, err := mcpResolveProviderModel(providers, "Daily", "Hidden", "hidden-model"); err == nil {
		t.Fatal("untested non-current provider must fail")
	}
	if _, _, err := mcpResolveProviderModel(providers, "Daily", "Tested", "missing"); err == nil {
		t.Fatal("unknown model must fail")
	}
}

func TestInboundMCPAPIKeyReusedUntilRotate(t *testing.T) {
	dir := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(dir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })

	first, err := loadOrCreateMCPAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateMCPAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "mck_") || len(first) < 20 {
		t.Fatalf("key reuse %q %q", first, second)
	}
	rotated, err := rotateMCPAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if rotated == first {
		t.Fatal("rotate must replace the key")
	}
	stored, err := readMCPAPIKey()
	if err != nil || stored != rotated {
		t.Fatalf("stored %q err %v", stored, err)
	}
}

func TestInboundMCPStatusHidesAccessUntilRunning(t *testing.T) {
	off := mcpStatusPayload(false, true, false, "127.0.0.1:1", 1, "mck_secret", "MaClaw.exe", "")
	if _, ok := off["apiKey"]; ok || off["enabled"] != false {
		t.Fatalf("off payload %+v", off)
	}
	failed := mcpStatusPayload(true, false, false, "", 0, "mck_secret", "MaClaw.exe", "bind failed")
	if _, ok := failed["apiKey"]; ok {
		t.Fatalf("failed payload leaked the key: %+v", failed)
	}
	if _, ok := failed["stdioConfig"]; ok {
		t.Fatalf("failed payload leaked the config: %+v", failed)
	}
	if failed["error"] != "bind failed" || failed["running"] != false {
		t.Fatalf("failed payload %+v", failed)
	}
	running := mcpStatusPayload(true, true, false, "127.0.0.1:18790", 18790, "mck_secret", `C:\MaClaw.exe`, "")
	if running["apiKey"] != "mck_secret" {
		t.Fatalf("running payload %+v", running)
	}
	stdio, _ := running["stdioConfig"].(string)
	httpCfg, _ := running["httpConfig"].(string)
	if !strings.Contains(stdio, `"args":["mcp"]`) || strings.Contains(stdio, "mck_secret") {
		t.Fatalf("stdio config %s", stdio)
	}
	if !strings.Contains(httpCfg, "Bearer mck_secret") || !strings.Contains(stdio, `C:\\MaClaw.exe`) {
		t.Fatalf("configs stdio=%s http=%s", stdio, httpCfg)
	}
}

func TestInboundMCPStatusLANConfig(t *testing.T) {
	prevLAN := mcpLANAddressFn
	t.Cleanup(func() { mcpLANAddressFn = prevLAN })
	mcpLANAddressFn = func() string { return "192.168.1.20" }

	off := mcpStatusPayload(true, false, true, "", 0, "mck_secret", "MaClaw.exe", "")
	if off["lanEnabled"] != true {
		t.Fatalf("lan flag missing while off: %+v", off)
	}
	if _, ok := off["lanURL"]; ok {
		t.Fatalf("lan url leaked while not running: %+v", off)
	}

	lan := mcpStatusPayload(true, true, true, "127.0.0.1:18790", 18790, "mck_secret", `C:\MaClaw.exe`, "")
	lanURL, _ := lan["lanURL"].(string)
	if lanURL != "http://192.168.1.20:18790/mcp" {
		t.Fatalf("lan url %q", lanURL)
	}
	lanCfg, _ := lan["lanHTTPConfig"].(string)
	if !strings.Contains(lanCfg, "http://192.168.1.20:18790/mcp") || !strings.Contains(lanCfg, "Bearer mck_secret") {
		t.Fatalf("lan config %s", lanCfg)
	}
	loopCfg, _ := lan["httpConfig"].(string)
	if !strings.Contains(loopCfg, "http://127.0.0.1:18790/mcp") {
		t.Fatalf("loopback config must stay untouched: %s", loopCfg)
	}

	mcpLANAddressFn = func() string { return "" }
	noIP := mcpStatusPayload(true, true, true, "127.0.0.1:18790", 18790, "mck_secret", "MaClaw.exe", "")
	_, hasURL := noIP["lanURL"]
	_, hasLanCfg := noIP["lanHTTPConfig"]
	if hasURL || hasLanCfg {
		t.Fatalf("no lan address must hide the lan config: %+v", noIP)
	}

	lanOff := mcpStatusPayload(true, true, false, "127.0.0.1:18790", 18790, "mck_secret", "MaClaw.exe", "")
	if _, ok := lanOff["lanURL"]; ok {
		t.Fatalf("lan off must not carry a lan url: %+v", lanOff)
	}
}

func TestListenMCPBindAddress(t *testing.T) {
	loopLn, port, err := listenMCP(0, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loopLn.Close() })
	if !isLoopbackAddr(loopLn.Addr()) || port <= 0 {
		t.Fatalf("loopback bind %s port %d", loopLn.Addr(), port)
	}

	lanLn, lanPort, err := listenMCP(0, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lanLn.Close() })
	tcpAddr, ok := lanLn.Addr().(*net.TCPAddr)
	if !ok || !tcpAddr.IP.IsUnspecified() || lanPort <= 0 {
		t.Fatalf("lan bind %s port %d", lanLn.Addr(), lanPort)
	}
	// Loopback stays reachable through the wildcard bind.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(lanPort)), time.Second)
	if err != nil {
		t.Fatalf("loopback dial via wildcard bind: %v", err)
	}
	_ = conn.Close()
}

func TestMCPHostLanConfigToggle(t *testing.T) {
	var cfg corelib.AppConfig
	if cfg.IsMcpHostLanEnabled() {
		t.Fatal("nil lan switch must stay off")
	}
	cfg.SetMcpHostLanEnabled(true)
	if !cfg.IsMcpHostLanEnabled() {
		t.Fatal("explicit true must turn lan on")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var loaded corelib.AppConfig
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if !loaded.IsMcpHostLanEnabled() {
		t.Fatalf("lan flag must round-trip: %s", raw)
	}
}

// The LAN flag rides config, not the host snapshot: a nil host (listener
// never started) must not report lanEnabled=false and flip the panel's LAN
// switch back to Off after a GenerateMCPHostAPIKey response.
func TestMCPStatusWhileEnabledKeepsLanFlagWithoutHost(t *testing.T) {
	app := &App{}
	out := app.mcpStatusWhileEnabled(nil, "mck_rotated", true)
	if out["lanEnabled"] != true || out["running"] != false {
		t.Fatalf("nil host payload %+v", out)
	}
	if _, ok := out["apiKey"]; ok {
		t.Fatalf("nil host payload must not leak the key: %+v", out)
	}
}

func TestMCPHTTPLanRemoteGate(t *testing.T) {
	api := &fakeMCPTasks{}
	lanOn := newMCPHTTPLAN(func() string { return "mck_testkey" }, api, func() bool { return true })
	lanOff := newMCPHTTPLAN(func() string { return "mck_testkey" }, api, nil)

	post := func(h http.Handler, remoteAddr, token, origin string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.RemoteAddr = remoteAddr
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// Loopback-only state refuses a non-loopback client before auth.
	if code := post(lanOff, "192.168.1.30:51000", "mck_testkey", ""); code != http.StatusForbidden {
		t.Fatalf("lan off remote %d", code)
	}
	// LAN on: a non-loopback client still needs the key.
	if code := post(lanOn, "192.168.1.30:51000", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("lan remote without key %d", code)
	}
	if code := post(lanOn, "192.168.1.30:51000", "mck_testkey", ""); code != http.StatusOK {
		t.Fatalf("lan remote with key %d", code)
	}
	// A browser origin from a LAN peer is accepted with lan on and refused
	// with lan off; a public origin stays refused in both modes (the
	// DNS-rebinding guard).
	if code := post(lanOn, "192.168.1.30:51000", "mck_testkey", "http://192.168.1.30:6274"); code != http.StatusOK {
		t.Fatalf("lan private origin %d", code)
	}
	if code := post(lanOn, "192.168.1.30:51000", "mck_testkey", "https://evil.example"); code != http.StatusForbidden {
		t.Fatalf("lan public origin %d", code)
	}
	if code := post(lanOff, "127.0.0.1:51001", "mck_testkey", "http://192.168.1.30:6274"); code != http.StatusForbidden {
		t.Fatalf("loopback remote with lan origin %d", code)
	}
}

type fakeMCPTasks struct {
	mu       sync.Mutex
	created  mcpCreateArgs
	command  string
	released chan struct{}
}

func (f *fakeMCPTasks) mcpListModels(ctx context.Context) (mcpModelCatalog, error) {
	return mcpModelCatalog{Current: "Daily", Providers: []mcpProviderInfo{{ID: "p1", Name: "Daily", DefaultModel: "m", Models: []string{"m"}}}}, nil
}

func (f *fakeMCPTasks) mcpCreateTask(ctx context.Context, args mcpCreateArgs) (mcpTaskView, error) {
	f.mu.Lock()
	f.created = args
	f.mu.Unlock()
	view := mcpTaskView{ProjectPath: `D:\work`, Name: "任务", Mode: "coding_dev", Status: mcpStatusRunning, RequestID: "mcp-req"}
	if args.Wait {
		if f.released != nil {
			select {
			case <-f.released:
			case <-ctx.Done():
				return view, nil
			}
		}
		view.Status = mcpStatusCompleted
		view.Text = "done"
	}
	return view, nil
}

func (f *fakeMCPTasks) mcpGetTask(ctx context.Context, projectPath string) (mcpTaskView, error) {
	if strings.TrimSpace(projectPath) == "" {
		return mcpTaskView{}, errFakeMCPMissing
	}
	return mcpTaskView{ProjectPath: projectPath, Status: mcpStatusIdle}, nil
}

func (f *fakeMCPTasks) mcpStopTask(ctx context.Context, projectPath string) (mcpTaskView, error) {
	return mcpTaskView{ProjectPath: projectPath, Status: mcpStatusCancelled}, nil
}

func (f *fakeMCPTasks) mcpSendCommand(ctx context.Context, projectPath, command string, wait bool) (mcpTaskView, error) {
	f.mu.Lock()
	f.command = command
	f.mu.Unlock()
	if strings.TrimSpace(command) == "" {
		return mcpTaskView{}, errFakeMCPMissing
	}
	return mcpTaskView{
		ProjectPath:      projectPath,
		Status:           mcpStatusNeedsConfirmation,
		ConfirmationKind: "plan",
		Commands:         mcpPlanDecisionCommands(),
		Text:             "待确认",
	}, nil
}

func (f *fakeMCPTasks) mcpListTasks(ctx context.Context) ([]mcpTaskView, error) {
	return []mcpTaskView{{ProjectPath: `D:\work`, Status: mcpStatusIdle}}, nil
}

var errFakeMCPMissing = errString("没有找到这个任务")

func TestInboundMCPProtocolAuthOriginAndTools(t *testing.T) {
	api := &fakeMCPTasks{released: make(chan struct{})}
	srv := httptest.NewServer(newMCPHTTP(func() string { return "mck_testkey" }, api))
	t.Cleanup(srv.Close)
	endpoint := srv.URL + "/mcp"

	if resp := mcpPost(t, endpoint, "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing auth %d", resp.StatusCode)
	}
	badOrigin := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "https://evil.example")
	if badOrigin.StatusCode != http.StatusForbidden {
		t.Fatalf("origin %d", badOrigin.StatusCode)
	}
	note := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, "")
	if note.StatusCode != http.StatusAccepted || note.Body != "" {
		t.Fatalf("notification %d %q", note.StatusCode, note.Body)
	}
	keyHeader := mcpPostWithHeader(t, endpoint, "X-Api-Key", "mck_testkey", `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if keyHeader.StatusCode != http.StatusOK || !strings.Contains(keyHeader.Body, `"result"`) {
		t.Fatalf("api key %d %s", keyHeader.StatusCode, keyHeader.Body)
	}
	initResp := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":"abc","method":"initialize","params":{"protocolVersion":"2025-03-26"}}`, "http://127.0.0.1")
	if !strings.Contains(initResp.Body, `"id":"abc"`) || !strings.Contains(initResp.Body, "2025-03-26") {
		t.Fatalf("initialize %s", initResp.Body)
	}
	unknownVersion := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`, "")
	if !strings.Contains(unknownVersion.Body, mcpProtocolVersionLatest) || strings.Contains(unknownVersion.Body, "1999-01-01") {
		t.Fatalf("fallback version %s", unknownVersion.Body)
	}
	current := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":12,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, "")
	if !strings.Contains(current.Body, "2025-11-25") {
		t.Fatalf("current version %s", current.Body)
	}
	listed := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`, "")
	if !strings.Contains(listed.Body, "list_models") || !strings.Contains(listed.Body, "create_task") || !strings.Contains(listed.Body, "send_command") {
		t.Fatalf("tools/list %s", listed.Body)
	}
	sent := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"send_command","arguments":{"project_path":"D:\\work","command":"/plan approve"}}}`, "")
	if !strings.Contains(sent.Body, "needs_confirmation") || !strings.Contains(sent.Body, "/plan approve") || strings.Contains(sent.Body, `"isError":true`) {
		t.Fatalf("send_command %s", sent.Body)
	}
	api.mu.Lock()
	sentCommand := api.command
	api.mu.Unlock()
	if sentCommand != "/plan approve" {
		t.Fatalf("recorded command %q", sentCommand)
	}
	unknown := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope"}}`, "")
	if !strings.Contains(unknown.Body, `"isError":true`) || strings.Contains(unknown.Body, `"error":{"code"`) {
		t.Fatalf("unknown tool %s", unknown.Body)
	}
	models := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list_models"}}`, "")
	if !strings.Contains(models.Body, "Daily") || strings.Contains(models.Body, `"isError":true`) {
		t.Fatalf("list_models %s", models.Body)
	}
	sse := mcpPostAccept(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":7,"method":"ping"}`, "text/event-stream")
	if !strings.Contains(sse.ContentType, "text/event-stream") || !strings.Contains(sse.Body, "event: message\ndata:") {
		t.Fatalf("sse %s %s", sse.ContentType, sse.Body)
	}
	both := mcpPostAccept(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":8,"method":"ping"}`, "application/json, text/event-stream")
	if !strings.Contains(both.ContentType, "application/json") || strings.HasPrefix(strings.TrimSpace(both.Body), "event:") {
		t.Fatalf("json preferred %s %s", both.ContentType, both.Body)
	}

	waitBody := make(chan string, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"create_task","arguments":{"name":"登录","prompt":"实现登录","working_dir":"D:\\repo","provider":"Daily","model":"m","wait":true}}}`))
		if err != nil {
			waitBody <- "ERR " + err.Error()
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer mck_testkey")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			waitBody <- "ERR " + err.Error()
			return
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			waitBody <- "ERR " + err.Error()
			return
		}
		waitBody <- string(raw)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		api.mu.Lock()
		got := api.created
		api.mu.Unlock()
		if got.Wait && got.Prompt == "实现登录" && got.WorkingDir == `D:\repo` {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	api.mu.Lock()
	created := api.created
	api.mu.Unlock()
	if !created.Wait || created.Provider != "Daily" || created.Model != "m" {
		t.Fatalf("create args %+v", created)
	}
	select {
	case body := <-waitBody:
		t.Fatalf("wait returned before the run was released: %s", body)
	case <-time.After(50 * time.Millisecond):
	}
	close(api.released)
	select {
	case body := <-waitBody:
		if !strings.Contains(body, "completed") || !strings.Contains(body, "done") || strings.Contains(body, `"isError":true`) {
			t.Fatalf("wait result %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not finish")
	}

	badMethod := mcpPost(t, endpoint, "mck_testkey", `{"jsonrpc":"2.0","id":11,"method":"nope"}`, "")
	if !strings.Contains(badMethod.Body, "-32601") {
		t.Fatalf("method %s", badMethod.Body)
	}
	parse := mcpPost(t, endpoint, "mck_testkey", `{`, "")
	if !strings.Contains(parse.Body, "-32700") {
		t.Fatalf("parse %s", parse.Body)
	}
	get := mcpHTTPStatus(t, endpoint)
	if get != http.StatusMethodNotAllowed {
		t.Fatalf("GET %d", get)
	}
}

func TestInboundMCPStdioProxySkipsNotifications(t *testing.T) {
	dir := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(dir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })

	const stdioKey = "mck_stdio_proxy_key_01"
	api := &fakeMCPTasks{}
	srv := httptest.NewServer(newMCPHTTP(func() string { return stdioKey }, api))
	t.Cleanup(srv.Close)
	if err := writeMCPAPIKey(stdioKey); err != nil {
		t.Fatal(err)
	}
	ep := mcpHostEndpoint{URL: srv.URL + "/mcp", Host: "127.0.0.1", Port: 18790, PID: os.Getpid(), Protocol: mcpEndpointProtocol}
	raw, err := json.Marshal(ep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mcpDiscoveryDir(), "endpoint.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	in := strings.NewReader("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n")
	var out strings.Builder
	if code := runMCPStdioProxy(in, &out); code != 0 {
		t.Fatalf("proxy exit %d %s", code, out.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"id":1`) {
		t.Fatalf("stdout %q", out.String())
	}

	if err := writeMCPAPIKey("mck_stdio_proxy_key_02"); err != nil {
		t.Fatal(err)
	}
	var rotated strings.Builder
	if code := runMCPStdioProxy(strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"ping\"}\n"), &rotated); code != 0 || !strings.Contains(rotated.String(), "-32000") || strings.Contains(rotated.String(), `"result"`) || strings.Contains(rotated.String(), "未运行") || !strings.Contains(rotated.String(), "API key") {
		t.Fatalf("rotated key code %d %s", code, rotated.String())
	}

	var missing strings.Builder
	corelib.SetMaclawBaseDir(t.TempDir())
	if code := runMCPStdioProxy(strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":7,\"method\":\"ping\"}\n"), &missing); code == 0 || !strings.Contains(missing.String(), "MCP服务器") {
		t.Fatalf("missing endpoint code %d %s", code, missing.String())
	}
}

func TestInboundMCPPreferFollowUpOverStaleRun(t *testing.T) {
	running := mcpPreferTaskView(
		mcpTaskView{Status: mcpStatusRunning, Text: "partial", Provider: "Daily"},
		mcpTaskView{Status: mcpStatusCompleted, Text: "后来的回答"},
	)
	if running.Status != mcpStatusRunning || running.Text != "partial" {
		t.Fatalf("running %+v", running)
	}
	failed := mcpPreferTaskView(
		mcpTaskView{Status: mcpStatusFailed, Error: "boom", Text: "解释\n\n[workspace files]\n- a.go", historyCount: 2, historySeal: "解释", Provider: "Daily", Model: "m"},
		mcpTaskView{Status: mcpStatusCompleted, Text: "解释", Name: "任务", historyCount: 2},
	)
	if failed.Status != mcpStatusFailed || failed.Error != "boom" || failed.Text != "解释\n\n[workspace files]\n- a.go" || failed.Name != "任务" {
		t.Fatalf("failed masked %+v", failed)
	}
	followed := mcpPreferTaskView(
		mcpTaskView{Status: mcpStatusCompleted, Text: "第一轮\n\n[workspace files]\n- a.go", historyCount: 2, historySeal: "第一轮", Provider: "Daily", Model: "m"},
		mcpTaskView{Status: mcpStatusCompleted, Text: "后来在界面里继续", Name: "任务", historyCount: 3},
	)
	if followed.Text != "后来在界面里继续" || followed.Provider != "Daily" || followed.Model != "m" {
		t.Fatalf("follow-up %+v", followed)
	}
}

func TestInboundMCPPinnedModelSkipsComputerUseAndBackground(t *testing.T) {
	if !mcpPinnedModelAllowed(maclawLLMRoleForeground, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("foreground chat should keep the pinned model")
	}
	if !mcpPinnedModelAllowed(maclawLLMRoleBackground, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("a chat task uses the background model role and should still keep the pin")
	}
	if mcpPinnedModelAllowed(maclawLLMRoleComputerUse, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("computer use must keep its own model")
	}
	if mcpPinnedModelAllowed(maclawLLMRoleForeground, &LoopContext{Kind: LoopKindBackground}) {
		t.Fatal("a background loop must keep the background model")
	}
}

func TestInboundMCPDiscoveryStaysOnLoopback(t *testing.T) {
	dir := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(dir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	tcp, ok := srv.Listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("addr %v", srv.Listener.Addr())
	}
	if err := writeMCPAPIKey("mck_discovery_loopback_1"); err != nil {
		t.Fatal(err)
	}
	write := func(pid int) {
		t.Helper()
		ep := mcpHostEndpoint{URL: "http://evil.example/steal", Host: "127.0.0.1", Port: tcp.Port, PID: pid, Protocol: mcpEndpointProtocol}
		raw, err := json.Marshal(ep)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mcpDiscoveryDir(), "endpoint.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(0)
	got, _, ok := readMCPHostDiscovery()
	if !ok || strings.Contains(got.URL, "evil") || !strings.Contains(got.URL, "127.0.0.1") {
		t.Fatalf("discovery %+v ok=%v", got, ok)
	}
	srv.Close()
	write(0)
	if _, _, ok := readMCPHostDiscovery(); ok {
		t.Fatal("closed port was still accepted")
	}
	if _, err := os.Stat(filepath.Join(mcpDiscoveryDir(), "endpoint.json")); !os.IsNotExist(err) {
		t.Fatalf("stale endpoint err %v", err)
	}
}

func TestInboundMCPNonGitWorkspaceStillStartsTheTurn(t *testing.T) {
	dir := t.TempDir()
	acp := programmingHostPolicy(false, dir, "acp")
	if !acp.FinalWorkspaceGateRequired {
		t.Fatal("ACP wrapper must keep the git workspace gate")
	}
	policy := programmingHostPolicy(true, dir, "mcp")
	if policy.FinalWorkspaceGateRequired {
		t.Fatal("MCP wrapper must not refuse a directory that has no git HEAD")
	}
	if programmingHostProber(policy, dir) != nil {
		t.Fatal("MCP must not run git status around a turn the gate does not use")
	}
	if programmingHostProber(acp, dir) == nil {
		t.Fatal("ACP gate still needs the git probe")
	}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Digest = digest
	called := false
	task, attempt, err := (codingruntime.Runner{
		Store:           codingruntime.NewMemoryStore(),
		LeaseOwner:      "gui:mcp:test",
		LeaseDuration:   time.Minute,
		WorkspaceProber: codingruntime.NewLocalGitWorkspaceProber(dir),
	}).Run(context.Background(), codingruntime.Task{
		TaskID:        "mcp-coding-nongit",
		WorkflowID:    "mcp",
		OwnerID:       "gui:mcp:test",
		ProjectRef:    filepath.Join(dir, "task-record"),
		Mode:          "mcp",
		RequestedWork: "实现登录",
		PolicyDigest:  digest,
	}, policy, acpProgrammingRuntimeExecutor(func(context.Context, codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
		called = true
		return codingruntime.ExecutionResult{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectObserved}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !called || task == nil || attempt == nil || attempt.Status != codingruntime.TaskCompleted {
		t.Fatalf("called=%v task=%+v attempt=%+v", called, task, attempt)
	}
}

func TestInboundMCPPromptWorkspaceUsesToolDir(t *testing.T) {
	app := newProjectSearchTestApp(t)
	repo := t.TempDir()
	created := app.CreateTaskWithMode("MCP coding", repo, taskCodingDevTag)
	if created.ProjectPath == "" || normalizeProjectSessionPath(created.ProjectPath) == normalizeProjectSessionPath(repo) {
		t.Fatalf("task path %s repo %s", created.ProjectPath, repo)
	}
	h := &IMMessageHandler{app: app}
	toolDir := h.projectTabWorkDirForOwner(projectSessionOwnerID(created.ProjectPath))
	got := mcpPromptWorkspace(created.ProjectPath, toolDir)
	if normalizeProjectSessionPath(got) != normalizeProjectSessionPath(repo) {
		t.Fatalf("workspace %s tool %s repo %s task %s", got, toolDir, repo, created.ProjectPath)
	}
	body := mcpTaskUserText(got, "实现登录")
	if !strings.Contains(body, normalizeProjectSessionPath(repo)) || strings.Contains(body, created.ProjectPath) {
		t.Fatalf("prompt names the wrong tree: %s", body)
	}
	if mcpPromptWorkspace(created.ProjectPath, "") != normalizeProjectSessionPath(created.ProjectPath) {
		t.Fatal("missing tool dir must keep the task record")
	}

	chat := app.CreateTaskWithMode("MCP chat", repo, "")
	chatDir := h.projectTabWorkDirForOwner(projectSessionOwnerID(chat.ProjectPath))
	if normalizeProjectSessionPath(mcpPromptWorkspace(chat.ProjectPath, chatDir)) != normalizeProjectSessionPath(repo) {
		t.Fatalf("chat tool dir %s task %s", chatDir, chat.ProjectPath)
	}
}

func TestInboundMCPChatDoesNotOpenCodingLedger(t *testing.T) {
	if assistantPromptUsesMutatingRuntime(assistantHostMCP, "chat", "请修改这段代码", `D:\work`) {
		t.Fatal("explicit chat must stay on the shared assistant path")
	}
	if !assistantPromptUsesMutatingRuntime(assistantHostMCP, taskCodingDevTag, "请修改这段代码", `D:\work`) {
		t.Fatal("coding task still uses the workspace gate")
	}
	if assistantPromptUsesMutatingRuntime(assistantHostACP, "", "你好", `D:\work`) {
		t.Fatal("a question stays off the ledger")
	}
	if !assistantPromptUsesMutatingRuntime("", "", "请修改代码", `D:\work`) {
		t.Fatal("ACP mutation heuristic unchanged")
	}
	if assistantPromptUsesMutatingRuntime(assistantHostMCP, "chat", "请修改这段代码", "") {
		t.Fatal("empty project path never opens the ledger")
	}
}

func TestInboundMCPAssistantReplyCountSkipsHostRows(t *testing.T) {
	n := mcpAssistantReplyCount([]ProjectConversationHistoryItem{
		{Role: "assistant", Content: "工具回显", planningEcho: true},
		{Role: "user", Content: "继续", hostContinuation: true},
		{Role: "assistant", Content: "真正的结果"},
		{Role: "assistant", Content: "   "},
	})
	if n != 1 {
		t.Fatalf("count %d", n)
	}
}

func TestInboundMCPLastAssistantSkipsHostRows(t *testing.T) {
	text := mcpLastAssistantText([]ProjectConversationHistoryItem{
		{Role: "assistant", Content: "工具回显", planningEcho: true},
		{Role: "user", Content: "继续", hostContinuation: true},
		{Role: "assistant", Content: "真正的结果"},
		{Role: "user", Content: "下一句"},
	})
	if text != "真正的结果" {
		t.Fatalf("text %q", text)
	}
}

type mcpHTTPResult struct {
	StatusCode  int
	Body        string
	ContentType string
}

func mcpPost(t *testing.T, endpoint, token, body, origin string) mcpHTTPResult {
	t.Helper()
	return mcpPostAccept(t, endpoint, token, body, "application/json, text/event-stream", origin)
}

func mcpPostWithHeader(t *testing.T, endpoint, header, token, body string) mcpHTTPResult {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(header, token)
	return mcpDo(t, req)
}

func mcpPostAccept(t *testing.T, endpoint, token, body, accept string, origin ...string) mcpHTTPResult {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if len(origin) > 0 && origin[0] != "" {
		req.Header.Set("Origin", origin[0])
	}
	return mcpDo(t, req)
}

func mcpDo(t *testing.T, req *http.Request) mcpHTTPResult {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return mcpHTTPResult{StatusCode: resp.StatusCode, Body: string(raw), ContentType: resp.Header.Get("Content-Type")}
}

func mcpHTTPStatus(t *testing.T, endpoint string) int {
	t.Helper()
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	return resp.StatusCode
}
