package guiapp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/RapidAI/CodeClaw/corelib/memory"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
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
	for _, alias := range []string{"remote_coding_dev", "remote_coding", "remote_programming", "remote_code", "remote"} {
		mode, err = mcpNormalizeTaskMode(alias, "")
		if err != nil || mode != taskRemoteCodingDevTag {
			t.Fatalf("remote alias %s = %s %v", alias, mode, err)
		}
	}
	if _, err := mcpNormalizeTaskMode("shell", ""); err == nil {
		t.Fatal("unknown mode must fail")
	}
}

func TestInboundMCPRemoteCreateTarget(t *testing.T) {
	parsed := mcpParseCreateArgs(map[string]any{
		"mode":            "remote",
		"ssh_host":        "[10.0.0.8]",
		"ssh_user":        "ubuntu",
		"ssh_password":    "s3cret-not-in-fields",
		"remote_work_dir": "/srv/app",
		"prompt":          "实现登录",
	})
	remote, err := mcpRemoteTarget(parsed)
	if err != nil || remote.Host != "10.0.0.8" || remote.User != "ubuntu" || remote.WorkDir != "/srv/app" || remote.Port != 22 || remote.Password != "s3cret-not-in-fields" {
		t.Fatalf("target %+v %v", remote, err)
	}
	key := mcpCreateClaimKey(parsed)
	if strings.Contains(key, "s3cret-not-in-fields") {
		t.Fatalf("claim kept the password: %s", key)
	}
	changed := parsed
	changed.SSHPassword = "other-secret"
	if mcpCreateClaimKey(changed) != key {
		t.Fatal("password changed the request identity")
	}
	changed.SSHHost = "10.0.0.9"
	if mcpCreateClaimKey(changed) == key {
		t.Fatal("a different host reused the request identity")
	}
	if _, err := mcpRemoteTarget(mcpCreateArgs{Mode: "remote", SSHHost: "h", SSHUser: "u", RemoteWorkDir: "/srv"}); err == nil {
		t.Fatal("missing password was accepted")
	}
	badPort := mcpParseCreateArgs(map[string]any{"ssh_port": float64(70000)})
	if badPort.ArgError == "" {
		t.Fatal("port 70000 was accepted")
	}
	if !mcpStartUsesRawText(true, taskRemoteCodingDevTag, "实现登录", false) {
		t.Fatal("remote create wrapped the prompt with the local task path")
	}
	if mcpStartUsesRawText(true, taskCodingDevTag, "实现登录", false) {
		t.Fatal("local create lost its task preamble")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath:   "p",
		Mode:          taskRemoteCodingDevTag,
		Status:        mcpStatusRunning,
		RequestID:     "mcp-remote",
		Workspace:     "/srv/app",
		RemoteHost:    "10.0.0.8",
		RemoteUser:    "ubuntu",
		RemotePort:    22,
		RemoteWorkDir: "/srv/app",
	})
	app.mcpSetRunWorkspace("p", "mcp-remote", dir)
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.workspace != "/srv/app" || view.RemoteHost != "10.0.0.8" || view.RemoteWorkDir != "/srv/app" {
		t.Fatalf("local folder replaced the remote directory: %+v", view)
	}
	raw, err := json.Marshal(view)
	if err != nil || strings.Contains(string(raw), "s3cret") || !strings.Contains(string(raw), `"ssh_host":"10.0.0.8"`) {
		t.Fatalf("published remote task %s %v", raw, err)
	}
	if _, err := app.mcpListArtifacts(context.Background(), "p"); err == nil || !strings.Contains(err.Error(), "SSH") {
		t.Fatalf("remote artifacts: %v", err)
	}
	tags := []string{taskRemoteCodingDevTag, "remote_host:10.1.1.1", "remote_user:alice", "remote_port:2200", "remote_workdir:/data/proj"}
	if mcpModeFromTags(tags) != taskRemoteCodingDevTag {
		t.Fatal("remote tag was read as chat")
	}
	host, user, workDir, port := mcpRemoteMetaFromTags(tags)
	if host != "10.1.1.1" || user != "alice" || workDir != "/data/proj" || port != 2200 {
		t.Fatalf("tags %s %s %s %d", host, user, workDir, port)
	}
	defs, err := json.Marshal(mcpToolDefinitions())
	if err != nil || !strings.Contains(string(defs), "remote_coding_dev") || !strings.Contains(string(defs), "ssh_password") {
		t.Fatalf("schema %s %v", defs, err)
	}
}

func TestInboundMCPRemoteModeAndWorkspace(t *testing.T) {
	omitted := mcpParseCreateArgs(map[string]any{
		"name":            "login",
		"ssh_host":        "10.0.0.8",
		"ssh_user":        "ubuntu",
		"ssh_password":    "s3cret-not-in-fields",
		"remote_work_dir": "/srv/app",
	})
	mode, err := mcpEffectiveCreateMode(omitted)
	if err != nil || mode != taskRemoteCodingDevTag {
		t.Fatalf("omitted mode = %s %v", mode, err)
	}
	key := mcpCreateClaimKey(omitted)
	if strings.Contains(key, "s3cret-not-in-fields") || !strings.Contains(key, "10.0.0.8") || !strings.Contains(key, "/srv/app") {
		t.Fatalf("omitted claim %s", key)
	}
	otherHost := omitted
	otherHost.SSHHost = "10.0.0.9"
	if mcpCreateClaimKey(otherHost) == key {
		t.Fatal("a different host reused the omitted-mode identity")
	}
	chat := mcpCreateArgs{Name: "login", Mode: "chat", WorkingDir: `D:\repo`}
	mixed := chat
	mixed.SSHHost = "10.0.0.8"
	mixed.SSHUser = "ubuntu"
	mixed.SSHPassword = "s3cret-not-in-fields"
	mixed.RemoteWorkDir = "/srv/app"
	if mcpCreateClaimKey(chat) == mcpCreateClaimKey(mixed) {
		t.Fatal("chat plus ssh reused a chat identity")
	}
	if _, err := mcpEffectiveCreateMode(mixed); err == nil || !strings.Contains(err.Error(), "会话任务不能带 SSH") {
		t.Fatalf("chat plus ssh: %v", err)
	}
	if _, err := mcpEffectiveCreateMode(mcpCreateArgs{Mode: "programming", SSHPort: 2200}); err == nil || !strings.Contains(err.Error(), "本机编程任务不能带 SSH") {
		t.Fatalf("local coding plus ssh: %v", err)
	}
	mode, err = mcpEffectiveCreateMode(mcpCreateArgs{WorkingDir: `D:\repo`})
	if err != nil || mode != taskCodingDevTag {
		t.Fatalf("local dir = %s %v", mode, err)
	}
	mode, err = mcpEffectiveCreateMode(mcpCreateArgs{SSHPort: 22})
	if err != nil || mode != taskRemoteCodingDevTag {
		t.Fatalf("port only = %s %v", mode, err)
	}

	sealed := mcpSealRemoteWorkspace(mcpTaskView{Mode: taskRemoteCodingDevTag, RemoteWorkDir: "/data/proj"})
	if sealed.workspace != "/data/proj" || sealed.WorkingDir != "/data/proj" {
		t.Fatalf("sealed %+v", sealed)
	}
	kept := mcpSealRemoteWorkspace(mcpTaskView{Mode: taskRemoteCodingDevTag, workspace: "/srv/app", RemoteWorkDir: "/other"})
	if kept.workspace != "/srv/app" {
		t.Fatalf("seal rewrote workspace %+v", kept)
	}
	filled := mcpFillTaskView(mcpTaskView{Mode: taskRemoteCodingDevTag}, mcpTaskView{RemoteHost: "10.1.1.1", RemoteWorkDir: "/data/proj"})
	if filled.workspace != "/data/proj" || filled.RemoteHost != "10.1.1.1" {
		t.Fatalf("fill %+v", filled)
	}

	dir := t.TempDir()
	app := &App{}
	view := mcpSealRemoteWorkspace(mcpTaskView{
		ProjectPath:   "p",
		Mode:          taskRemoteCodingDevTag,
		RemoteHost:    "10.1.1.1",
		RemoteWorkDir: "/data/proj",
	})
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath:   "p",
		Mode:          view.Mode,
		Status:        mcpStatusRunning,
		RequestID:     "mcp-seal",
		Workspace:     view.workspace,
		RemoteHost:    view.RemoteHost,
		RemoteWorkDir: view.RemoteWorkDir,
	})
	app.mcpSetRunWorkspace("p", "mcp-seal", dir)
	got, ok := app.mcpViewFromRegistry("p")
	if !ok || got.workspace != "/data/proj" {
		t.Fatalf("empty remote workspace became local: %+v", got)
	}
	fromTags := mcpViewWithTaskTags(mcpTaskView{ProjectPath: "gui"}, []string{
		taskRemoteCodingDevTag, "remote_host:10.1.1.1", "remote_user:alice", "remote_port:2200", "remote_workdir:/data/proj",
	})
	if fromTags.Mode != taskRemoteCodingDevTag || fromTags.workspace != "/data/proj" || fromTags.RemoteHost != "10.1.1.1" || fromTags.RemotePort != 2200 {
		t.Fatalf("tags %+v", fromTags)
	}
	if err := mcpRemoteArtifactError(fromTags); err == nil || !strings.Contains(err.Error(), "SSH") {
		t.Fatalf("tag refusal %v", err)
	}
	if _, err := app.mcpListArtifacts(context.Background(), dir); err != nil && strings.Contains(err.Error(), "SSH") {
		t.Fatalf("local dir refused as remote: %v", err)
	}
	raw, err := json.Marshal(mcpToolDefinitions())
	if err != nil || !strings.Contains(string(raw), "不能再带这些 SSH 字段") {
		t.Fatalf("schema omitted the ssh mix rule")
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
	if got := mcpDisplayStatus("", "done", "Finished 4 of 5 steps; 1 still failed."); got != mcpStatusFailed {
		t.Fatalf("still failed = %s", got)
	}
	if got := mcpDisplayStatus("", "done", "模型连续返回空响应，任务中断"); got != mcpStatusFailed {
		t.Fatalf("empty responses = %s", got)
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
	if got := mcpDisplayStatus(mcpStatusWaitingReview, "done", "子任务结果待审阅"); got != mcpStatusWaitingReview {
		t.Fatalf("review lost to %s", got)
	}
}

func TestInboundMCPConfirmationIsNotCompleted(t *testing.T) {
	status, text, errText, keepText, _ := mcpRunOutcome(nil, false, &IMAgentResponse{
		Text:         "请在窗口确认",
		Confirmation: &IMResponseConfirmation{Summary: "实现登录"},
	})
	if status != mcpStatusNeedsConfirmation || text != "请在窗口确认" || errText != "" || keepText {
		t.Fatalf("confirmation status=%s text=%q err=%q keep=%v", status, text, errText, keepText)
	}
	status, text, _, _, _ = mcpRunOutcome(nil, false, &IMAgentResponse{Confirmation: &IMResponseConfirmation{Summary: "实现登录"}})
	if status != mcpStatusNeedsConfirmation || text != "实现登录" {
		t.Fatalf("summary fallback status=%s text=%q", status, text)
	}
	status, text, errText, _, _ = mcpRunOutcome(nil, true, &IMAgentResponse{Text: "请确认", Confirmation: &IMResponseConfirmation{Summary: "实现登录"}})
	if status != mcpStatusCancelled || errText != "" || text != "请确认" {
		t.Fatalf("cancel must win status=%s text=%q err=%q", status, text, errText)
	}
	status, text, errText, _, _ = mcpRunOutcome(nil, false, &IMAgentResponse{Text: "写好了"})
	if status != mcpStatusCompleted || text != "写好了" || errText != "" {
		t.Fatalf("answer status=%s text=%q err=%q", status, text, errText)
	}
	status, _, errText, _, _ = mcpRunOutcome(nil, false, &IMAgentResponse{Text: "失败说明", Error: "模型失败"})
	if status != mcpStatusFailed || errText != "模型失败" {
		t.Fatalf("error status=%s err=%q", status, errText)
	}
}

func TestInboundMCPConfirmationCommandsAreOperable(t *testing.T) {
	plan := &IMAgentResponse{Text: "需要确认执行计划", Actions: codingPlanApproveActions()}
	status, text, errText, _, _ := mcpRunOutcome(nil, false, plan)
	status, text, kind, commands := mcpConfirmationCallerNotice(status, text, plan)
	if status != mcpStatusNeedsConfirmation || kind != mcpConfirmKindPlan || errText != "" || !strings.Contains(text, "send_command") {
		t.Fatalf("plan status=%s kind=%s text=%q err=%q", status, kind, text, errText)
	}
	if len(commands) < 3 || commands[0] != "/plan approve" {
		t.Fatalf("plan commands %v", commands)
	}

	card := &IMAgentResponse{Text: "请确认", Confirmation: &IMResponseConfirmation{ID: "card-1", Summary: "写文件"}}
	status, text, _, _, _ = mcpRunOutcome(nil, false, card)
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
	if _, err := app.mcpSendCommand(context.Background(), "p", "/plan approve", "", "", false); err == nil || !strings.Contains(err.Error(), "还在运行") {
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
	gated, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil || gated.Status != mcpStatusRunning || gated.ConfirmationKind != "scope" || !strings.Contains(gated.Text, "要写仓库外的文件") || !strings.Contains(gated.Text, "工作目录越界") || !strings.Contains(gated.Text, "send_command") {
		t.Fatalf("running gate %+v err=%v", gated, err)
	}
	scopeConfirmID := gated.ConfirmationID
	if !strings.HasPrefix(scopeConfirmID, "mcf_") {
		t.Fatalf("confirmation id %q", scopeConfirmID)
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
	if _, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+scopeID+" allow_task", "", "", false); err == nil || !strings.Contains(err.Error(), "没有这个决定") {
		t.Fatalf("path allow_task: %v", err)
	}
	select {
	case decision := <-scopeCh:
		t.Fatalf("rejected path decision delivered %q", decision)
	default:
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+highID+" allow_dir", "", "", false); err == nil || !strings.Contains(err.Error(), "没有这个决定") {
		t.Fatalf("high-risk allow_dir: %v", err)
	}
	select {
	case decision := <-highCh:
		t.Fatalf("rejected high-risk decision delivered %q", decision)
	default:
	}
	resolved, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+scopeID+" allow_dir", scopeConfirmID, "", false)
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
	if _, err := app.mcpSendCommand(context.Background(), "p", "/plan approve", "", "", false); err == nil || !strings.Contains(err.Error(), "还在运行") {
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
	gated, err = app.mcpGetTask(context.Background(), "p", "")
	criticalConfirmID := gated.ConfirmationID
	if !strings.HasPrefix(criticalConfirmID, "mcf_") || criticalConfirmID == scopeConfirmID {
		t.Fatalf("critical confirmation id %q previous %q", criticalConfirmID, scopeConfirmID)
	}
	if err != nil || gated.ConfirmationKind != "critical" || !containsAll(gated.Commands, "__resolve_critical_confirm__ crit_mcp confirm", "__resolve_critical_confirm__ crit_mcp reject") || containsAll(gated.Commands, "__resolve_critical_confirm__ crit_other confirm") {
		t.Fatalf("critical gate %+v err=%v", gated, err)
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__resolve_critical_confirm__ crit_other reject", "", "", false); err == nil {
		t.Fatal("foreign critical confirm was accepted")
	}
	select {
	case <-foreignCh:
		t.Fatal("foreign critical confirm was delivered")
	default:
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__resolve_critical_confirm__ crit_mcp confirm", criticalConfirmID, "", false); err != nil {
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

func TestInboundMCPScopeDecisionReachesTheToolAndClosesTheDialog(t *testing.T) {
	var event string
	app := &App{testEmit: func(name string, data ...interface{}) {
		if name == "subagent-scope-approval-resolved" {
			event = name
		}
	}}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-live"})
	handler := &IMMessageHandler{app: app}
	ch := make(chan ScopeApprovalDecision, 1)
	id := storePendingScopeApproval(handler, ScopeApprovalRequest{ProjectPath: "p", Message: "写外面"}, ch)
	t.Cleanup(func() { pendingScopeApprovals.Delete(id) })

	full := make(chan ScopeApprovalDecision, 1)
	full <- ScopeApprovalDeny
	fullID := storePendingScopeApproval(handler, ScopeApprovalRequest{ProjectPath: "p", Message: "通道已满"}, full)
	t.Cleanup(func() { pendingScopeApprovals.Delete(fullID) })
	issued, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil || !strings.HasPrefix(issued.ConfirmationID, "mcf_") {
		t.Fatalf("issued confirmation %+v err=%v", issued, err)
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+fullID+" allow_dir", issued.ConfirmationID, "", false); err == nil || !strings.Contains(err.Error(), "已过期") {
		t.Fatalf("undelivered decision: %v", err)
	}
	select {
	case decision := <-full:
		if decision != ScopeApprovalDeny {
			t.Fatalf("buffered decision changed to %q", decision)
		}
	default:
		t.Fatal("buffered decision was removed")
	}

	issued, err = app.mcpGetTask(context.Background(), "p", "")
	if err != nil || !strings.HasPrefix(issued.ConfirmationID, "mcf_") {
		t.Fatalf("next confirmation %+v err=%v", issued, err)
	}
	view, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+id+" allow_dir", issued.ConfirmationID, "", false)
	if err != nil || view.Status != mcpStatusRunning || !strings.Contains(view.Text, "已提交授权决定") {
		t.Fatalf("scope resolve %+v err=%v", view, err)
	}
	if event != "subagent-scope-approval-resolved" {
		t.Fatalf("dialog event %q", event)
	}
	select {
	case decision := <-ch:
		if decision != ScopeApprovalAllowDir {
			t.Fatalf("scope decision %q", decision)
		}
	default:
		t.Fatal("scope decision was not delivered")
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "__scope_approval__ "+id+" deny", issued.ConfirmationID, "", false); err == nil || !strings.Contains(err.Error(), "已过期") {
		t.Fatalf("second decision: %v", err)
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

func TestInboundMCPCriticalResolveClosesTheDesktopDialog(t *testing.T) {
	var event string
	var confirmID string
	app := &App{testEmit: func(name string, data ...interface{}) {
		if name != "critical-risk-confirm-resolved" || len(data) == 0 {
			return
		}
		payload, _ := data[0].(map[string]interface{})
		if payload == nil {
			return
		}
		event = name
		confirmID, _ = payload["confirm_id"].(string)
	}}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-live"})
	owner := projectSessionOwnerID("p")
	app.imHandler = &IMMessageHandler{app: app}
	ch := make(chan criticalRiskConfirmResponse, 1)
	app.imHandler.pendingCriticalConfirm.Store("crit_mcp", &pendingCriticalConfirmEntry{Ch: ch, Prompt: "安装危险技能", Owner: owner})
	t.Cleanup(func() { app.imHandler.pendingCriticalConfirm.Delete("crit_mcp") })

	issued, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil || !strings.HasPrefix(issued.ConfirmationID, "mcf_") {
		t.Fatalf("critical confirmation %+v err=%v", issued, err)
	}
	view, err := app.mcpSendCommand(context.Background(), "p", "__resolve_critical_confirm__ crit_mcp confirm", issued.ConfirmationID, "", false)
	if err != nil || view.Status != mcpStatusRunning || !strings.Contains(view.Text, "已提交安全确认") {
		t.Fatalf("critical resolve %+v err=%v", view, err)
	}
	if event != "critical-risk-confirm-resolved" || confirmID != "crit_mcp" {
		t.Fatalf("dialog event %q id %q", event, confirmID)
	}
	select {
	case resp := <-ch:
		if !resp.Confirmed {
			t.Fatal("critical confirm was rejected")
		}
	default:
		t.Fatal("critical confirm was not delivered")
	}
}

func TestInboundMCPPlanReviewBecomesApprovable(t *testing.T) {
	app := &App{}
	owner := projectSessionOwnerID("p")
	app.imHandler = &IMMessageHandler{}
	key := stickyCodingMemoryKey(owner)
	app.imHandler.stickyCodingWorkbenchMemory.Store(key, stickyCodingWorkbenchMemory{
		PendingPlanJSON:     `{"tasks":[{"index":1}]}`,
		PendingPlanMarkdown: "## 改登录",
	})
	app.imHandler.planReviewLive.Store(key, int64(1))

	live := app.mcpApplyLiveConfirmation(mcpTaskView{
		ProjectPath:      "p",
		Status:           mcpStatusNeedsConfirmation,
		ConfirmationKind: mcpConfirmKindPlan,
		Commands:         mcpPlanDecisionCommands(),
		Text:             "计划写好了",
	})
	if live.ConfirmationKind != mcpConfirmKindPlanReview || commandsContainAny(live.Commands, "/plan approve", "/plan skip", "/plan reject") {
		t.Fatalf("review still offered approval %+v", live)
	}
	if !strings.Contains(live.Text, "评审") {
		t.Fatalf("review hint missing %q", live.Text)
	}

	app.imHandler.planReviewLive.Delete(key)
	ready := app.mcpApplyLiveConfirmation(mcpTaskView{
		ProjectPath:      "p",
		Status:           mcpStatusNeedsConfirmation,
		ConfirmationKind: mcpConfirmKindPlanReview,
		Text:             live.Text,
	})
	if ready.ConfirmationKind != mcpConfirmKindPlan || !containsAll(ready.Commands, "/plan approve", "/plan skip", "/plan reject") {
		t.Fatalf("finished review %+v", ready)
	}
}

func TestInboundMCPCriticalCancelClosesTheDesktopDialog(t *testing.T) {
	var event, confirmID string
	app := &App{testEmit: func(name string, data ...interface{}) {
		if name != "critical-risk-confirm-resolved" || len(data) == 0 {
			return
		}
		payload, _ := data[0].(map[string]interface{})
		if payload == nil {
			return
		}
		event = name
		confirmID, _ = payload["confirm_id"].(string)
	}}
	handler := &IMMessageHandler{app: app}
	handler.pendingCriticalConfirm.Store("crit_cancel", &pendingCriticalConfirmEntry{
		Ch:    make(chan criticalRiskConfirmResponse, 1),
		Owner: projectSessionOwnerID("p"),
	})
	t.Cleanup(func() { handler.pendingCriticalConfirm.Delete("crit_cancel") })

	if handler.finishCancelledCriticalConfirm(nil, "crit_cancel", "danger") {
		t.Fatal("nil entry confirmed the install")
	}
	if event != "" {
		t.Fatal("nil entry closed a dialog")
	}
	entry, _ := handler.pendingCriticalConfirm.Load("crit_cancel")
	if handler.finishCancelledCriticalConfirm(entry.(*pendingCriticalConfirmEntry), "crit_cancel", "danger") {
		t.Fatal("cancel confirmed the install")
	}
	if event != "critical-risk-confirm-resolved" || confirmID != "crit_cancel" {
		t.Fatalf("dialog event %q id %q", event, confirmID)
	}
	if _, ok := handler.pendingCriticalConfirm.Load("crit_cancel"); ok {
		t.Fatal("cancelled confirm stayed pending")
	}
}

func TestInboundMCPSkillInstallResolveClosesTheDesktopDialog(t *testing.T) {
	var event, confirmID string
	app := &App{testEmit: func(name string, data ...interface{}) {
		if name != "critical-risk-confirm-resolved" || len(data) == 0 {
			return
		}
		payload, _ := data[0].(map[string]interface{})
		if payload == nil {
			return
		}
		event = name
		confirmID, _ = payload["confirm_id"].(string)
	}}
	ch := make(chan criticalRiskConfirmResponse, 1)
	app.skillInstallConfirm.Store("skill_install_mcp", &pendingCriticalConfirmEntry{Ch: ch})
	t.Cleanup(func() { app.skillInstallConfirm.Delete("skill_install_mcp") })

	if err := app.resolveSkillInstallConfirm("skill_install_mcp", true); err != nil {
		t.Fatal(err)
	}
	if event != "critical-risk-confirm-resolved" || confirmID != "skill_install_mcp" {
		t.Fatalf("dialog event %q id %q", event, confirmID)
	}
	select {
	case resp := <-ch:
		if !resp.Confirmed {
			t.Fatal("skill install was rejected")
		}
	default:
		t.Fatal("skill install was not delivered")
	}
}

func TestInboundMCPFinishKeepsConfirmationNotice(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-1", Text: "流式片段"})
	notice := "用哪个端口？\n\n需要你的回答。用 send_command 把回答原文发回这个任务。"
	app.mcpFinishRun("p", "mcp-1", mcpStatusNeedsConfirmation, notice, "", true, "ask", nil, nil)
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusNeedsConfirmation || !strings.Contains(view.Text, "流式片段") || !strings.Contains(view.Text, "用哪个端口") {
		t.Fatalf("notice dropped %+v ok=%v", view, ok)
	}

	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "q", Status: mcpStatusRunning, RequestID: "mcp-2", Text: "只有流"})
	app.mcpFinishRun("q", "mcp-2", mcpStatusCompleted, "", "", true, "", nil, nil)
	view, ok = app.mcpViewFromRegistry("q")
	if !ok || view.Text != "只有流" || view.Status != mcpStatusCompleted {
		t.Fatalf("empty notice replaced the stream %+v ok=%v", view, ok)
	}
}

func TestInboundMCPAskOptionsStayVisible(t *testing.T) {
	app := &App{}
	owner := projectSessionOwnerID("p")
	app.imHandler = &IMMessageHandler{}
	app.imHandler.pendingAskUser.Store(owner, &pendingAskUserState{
		Question:  "用 8080 还是别的端口？",
		Options:   []string{"8080", "9090"},
		InputType: "choice",
	})
	view := app.mcpApplyLiveConfirmation(mcpTaskView{ProjectPath: "p", Status: mcpStatusCompleted, Text: "先停一下"})
	if view.ConfirmationKind != "ask" || !strings.Contains(view.Text, "9090") || !containsAll(view.Commands, "8080", "9090") {
		t.Fatalf("choice %+v", view)
	}

	app.imHandler.pendingAskUser.Store(owner, &pendingAskUserState{
		Question:  "是否继续？",
		Options:   []string{"继续", "停止"},
		InputType: "confirm",
	})
	view = app.mcpApplyLiveConfirmation(mcpTaskView{ProjectPath: "p", Status: mcpStatusCompleted, Text: "先停一下"})
	if view.ConfirmationKind != "ask" || !containsAll(view.Commands, "confirm", "cancel") {
		t.Fatalf("confirm ask %+v", view)
	}
}

func TestInboundMCPFinishDoesNotOverwriteCancel(t *testing.T) {
	app := &App{}
	app.mcpReplaceRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-1"})
	app.mcpCancelRun("p")
	app.mcpFinishRun("p", "mcp-1", mcpStatusCompleted, "late", "", false, "", nil, nil)
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

func (f *fakeMCPTasks) mcpGetTask(ctx context.Context, projectPath, runID string) (mcpTaskView, error) {
	if strings.TrimSpace(projectPath) == "" {
		return mcpTaskView{}, errFakeMCPMissing
	}
	return mcpTaskView{ProjectPath: projectPath, Status: mcpStatusIdle}, nil
}

func (f *fakeMCPTasks) mcpStopTask(ctx context.Context, projectPath string) (mcpTaskView, error) {
	return mcpTaskView{ProjectPath: projectPath, Status: mcpStatusCancelled}, nil
}

func (f *fakeMCPTasks) mcpSendCommand(ctx context.Context, projectPath, command, confirmationID, clientRequestID string, wait bool) (mcpTaskView, error) {
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

func (f *fakeMCPTasks) mcpReadLog(ctx context.Context, projectPath, runID string, cursor int) (mcpLogPage, error) {
	return mcpLogPage{ProjectPath: projectPath, Events: []mcpLogEvent{}, NextCursor: cursor}, nil
}

func (f *fakeMCPTasks) mcpListArtifacts(ctx context.Context, projectPath string) (mcpArtifactList, error) {
	return mcpArtifactList{ProjectPath: projectPath, Files: []mcpArtifactFile{}}, nil
}

func (f *fakeMCPTasks) mcpReadArtifact(ctx context.Context, projectPath, relPath string, download bool) (mcpArtifactBody, error) {
	return mcpArtifactBody{ProjectPath: projectPath, Path: relPath}, nil
}

func (f *fakeMCPTasks) mcpGetRequest(ctx context.Context, clientRequestID string) (mcpRequestView, error) {
	return mcpRequestView{ClientRequestID: clientRequestID}, nil
}

func (f *fakeMCPTasks) mcpCapabilities(ctx context.Context) (mcpCapabilityView, error) {
	return mcpCapabilityView{APIVersion: mcpHostAPIVersion, Features: mcpFeatureNames(), ModelConnection: "unconfigured", BrowserMethods: mcpBrowserMethods(false)}, nil
}

func (f *fakeMCPTasks) mcpRetrySteps(ctx context.Context, projectPath string, stepIDs []string, confirmationID, clientRequestID string, wait bool) (mcpTaskView, error) {
	return mcpTaskView{ProjectPath: projectPath, Status: mcpStatusFailed}, nil
}

func (f *fakeMCPTasks) mcpBrowserTest(ctx context.Context, projectPath, action, url, selector, text, key, expect, stepID string, paths []string) (mcpBrowserResult, error) {
	return mcpBrowserResult{ProjectPath: projectPath, Action: action, StepID: stepID, OK: true}, nil
}

func (f *fakeMCPTasks) mcpPackArtifacts(ctx context.Context, projectPath string, paths []string) (mcpPackResult, error) {
	return mcpPackResult{ProjectPath: projectPath, Files: []mcpPackFile{}}, nil
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
	if !strings.Contains(listed.Body, "list_models") || !strings.Contains(listed.Body, "create_task") || !strings.Contains(listed.Body, "send_command") || !strings.Contains(listed.Body, "read_log") || !strings.Contains(listed.Body, "read_artifact") || !strings.Contains(listed.Body, "get_capabilities") {
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
	waiting := mcpPreferTaskView(
		mcpTaskView{
			Status: mcpStatusWaitingReview, Error: mcpReviewHandoffErrorText, RunID: "run-a",
			EndReason: mcpStatusWaitingReview, EndDetail: mcpReviewHandoffErrorText,
			Steps:        []mcpStepView{{ID: "T5", Status: mcpStepWaiting, Error: mcpReviewHandoffErrorText}},
			Stop:         &mcpStopView{Class: mcpStopReview, Reason: mcpReviewHandoffErrorText},
			historyCount: 2, historySeal: "交接",
		},
		mcpTaskView{Status: mcpStatusCompleted, Text: "后来的一句", historyCount: 3},
	)
	if waiting.Status != mcpStatusWaitingReview || waiting.Text != "后来的一句" || len(waiting.Steps) != 1 || waiting.Steps[0].Status != mcpStepWaiting || waiting.Stop == nil || waiting.Stop.Class != mcpStopReview {
		t.Fatalf("review upgraded %+v", waiting)
	}
	blocked := mcpPreferTaskView(
		mcpTaskView{
			Status: mcpStatusFailed, Error: "boom",
			Steps:        []mcpStepView{{ID: "T1", Status: mcpStepFailed, Error: "boom"}},
			historyCount: 2, historySeal: "失败",
		},
		mcpTaskView{Status: mcpStatusCompleted, Text: "后来的一句", historyCount: 3},
	)
	if blocked.Status != mcpStatusFailed || len(blocked.Steps) != 1 || blocked.Steps[0].ID != "T1" {
		t.Fatalf("failure upgraded %+v", blocked)
	}
	workflowRunning := mcpPreferTaskView(
		mcpTaskView{
			Status: mcpStatusFailed, Error: "boom",
			Steps:        []mcpStepView{{ID: "T1", Status: mcpStepFailed, Error: "boom"}},
			Stop:         &mcpStopView{Class: mcpStopProgram, Reason: "boom"},
			historyCount: 2, historySeal: "失败",
		},
		mcpTaskView{Status: mcpStatusRunning, Text: "工作流还显示在跑", historyCount: 3},
	)
	if workflowRunning.Status != mcpStatusFailed || workflowRunning.Text != "工作流还显示在跑" || workflowRunning.Stop == nil || workflowRunning.Stop.Class != mcpStopProgram || len(workflowRunning.Steps) != 1 {
		t.Fatalf("workflow running hid the failure %+v", workflowRunning)
	}
	nextRun := mcpPreferTaskView(
		mcpTaskView{Status: mcpStatusCompleted, historyCount: 2, historySeal: "好了"},
		mcpTaskView{Status: mcpStatusRunning, Text: "下一轮", historyCount: 3},
	)
	if nextRun.Status != mcpStatusRunning || nextRun.Text != "下一轮" || nextRun.Stop != nil {
		t.Fatalf("completed run must follow a later workflow %+v", nextRun)
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

func TestInboundMCPCreateWorkspaceSurvivesDesktopDefault(t *testing.T) {
	app := newProjectSearchTestApp(t)
	repo := t.TempDir()
	created := app.CreateTaskWithMode("MCP coding", repo, taskCodingDevTag)
	if created.ProjectPath == "" {
		t.Fatal("empty project path")
	}
	repo = normalizeProjectSessionPath(repo)
	owner := projectSessionOwnerID(created.ProjectPath)
	// A session cache is not a second execution directory. The desktop default
	// and any other cached folder both lose to the directory named on the task.
	desktop := normalizeProjectSessionPath(corelib.WorkspaceDir())
	cached := normalizeProjectSessionPath(t.TempDir())
	for _, bound := range []string{desktop, cached} {
		if bound == "" || bound == repo {
			continue
		}
		app.assistantSessionWorkingDirs.Store(owner, bound)
		if got := normalizeProjectSessionPath(app.EffectiveWorkingDirForOwner(owner)); got != repo {
			t.Fatalf("owner dir %s want %s cache %s", got, repo, bound)
		}
		h := &IMMessageHandler{app: app}
		toolDir := h.projectTabWorkDirForOwner(owner)
		if got := normalizeProjectSessionPath(mcpPromptWorkspace(created.ProjectPath, toolDir)); got != repo {
			t.Fatalf("prompt workspace %s tool %s repo %s cache %s", got, toolDir, repo, bound)
		}
	}

	tabID := "proj-mcp-workdir"
	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); msg == "" {
		t.Fatal("CreateProjectTabSession returned empty")
	}
	if got := normalizeProjectSessionPath(fmt.Sprint(app.GetTabWorkingDir(tabID)["path"])); got != repo {
		t.Fatalf("header dir %s want %s", got, repo)
	}
	next := normalizeProjectSessionPath(t.TempDir())
	if err := app.SetTabWorkingDir(tabID, next); err != nil {
		t.Fatal(err)
	}
	if got := normalizeProjectSessionPath(app.recentTaskWorkingDir(created.ProjectPath)); got != next {
		t.Fatalf("task tag %s want %s", got, next)
	}
	h := &IMMessageHandler{app: app}
	if got := normalizeProjectSessionPath(h.projectTabWorkDirForOwner(owner)); got != next {
		t.Fatalf("tool dir after choose %s want %s", got, next)
	}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: created.ProjectPath,
		Mode:        taskCodingDevTag,
		Status:      mcpStatusRunning,
		RequestID:   "mcp-keep-dir",
		Workspace:   repo,
	})
	app.mcpSetRunWorkspace(created.ProjectPath, "mcp-keep-dir", h.projectTabWorkDirForOwner(owner))
	view, ok := app.mcpViewFromRegistry(created.ProjectPath)
	if !ok || normalizeProjectSessionPath(view.workspace) != next || normalizeProjectSessionPath(view.WorkingDir) != next {
		t.Fatalf("published directory %+v want %s", view, next)
	}
	named := mcpApplyNamedWorkingDir(mcpTaskView{ProjectPath: created.ProjectPath, Mode: taskCodingDevTag}, []string{recentTaskWorkingDirTag(next)})
	if normalizeProjectSessionPath(named.workspace) != next || normalizeProjectSessionPath(named.WorkingDir) != next {
		t.Fatalf("query directory %+v want %s", named, next)
	}
	if app.memoryStore != nil {
		app.memoryStore.WaitRebuild()
		app.memoryStore.ProjectIndex().Rebuild(app.memoryStore.List(memory.CategoryTaskArtifact, ""))
	}
	if got := normalizeProjectSessionPath(app.recentTaskWorkingDir(created.ProjectPath)); got != next {
		t.Fatalf("rebuilt tag %s want %s", got, next)
	}
	body, err := os.ReadFile(filepath.Join(created.ProjectPath, "task.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Working directory: "+next) {
		t.Fatalf("task file directory %s want %s", body, next)
	}
}

func TestInboundMCPWorkingDirSessionFailureKeepsTag(t *testing.T) {
	app := newProjectSearchTestApp(t)
	repo := normalizeProjectSessionPath(t.TempDir())
	created := app.CreateTaskWithMode("MCP coding", repo, taskCodingDevTag)
	if created.ProjectPath == "" {
		t.Fatal("empty project path")
	}
	tabID := "proj-mcp-workdir-fail"
	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); msg == "" {
		t.Fatal("CreateProjectTabSession returned empty")
	}
	if err := app.ensureProjectTabSessionPersist().Close(); err != nil {
		t.Fatal(err)
	}
	next := normalizeProjectSessionPath(t.TempDir())
	if err := app.SetTabWorkingDir(tabID, next); err == nil {
		t.Fatal("closed session store must fail the directory change")
	}
	if got := normalizeProjectSessionPath(app.recentTaskWorkingDir(created.ProjectPath)); got != repo {
		t.Fatalf("tag %s want %s", got, repo)
	}
	owner := projectSessionOwnerID(created.ProjectPath)
	if got := normalizeProjectSessionPath(app.EffectiveWorkingDirForOwner(owner)); got != repo {
		t.Fatalf("owner dir %s want %s", got, repo)
	}
	body, err := os.ReadFile(filepath.Join(created.ProjectPath, "task.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Working directory: "+next) {
		t.Fatalf("failed change wrote %s", body)
	}
}

func TestInboundMCPStaleSessionCacheDoesNotReplaceTaskDir(t *testing.T) {
	app := newProjectSearchTestApp(t)
	repo := normalizeProjectSessionPath(t.TempDir())
	stale := normalizeProjectSessionPath(t.TempDir())
	created := app.CreateTaskWithMode("MCP coding", repo, taskCodingDevTag)
	if created.ProjectPath == "" {
		t.Fatal("empty project path")
	}
	tabID := "proj-mcp-stale-cache"
	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); msg == "" {
		t.Fatal("CreateProjectTabSession returned empty")
	}
	persist := app.ensureProjectTabSessionPersist()
	session, err := persist.LoadSession(tabID)
	if err != nil || session == nil {
		t.Fatalf("load session: %v", err)
	}
	session.WorkingDir = stale
	if err := persist.SaveSession(session); err != nil {
		t.Fatal(err)
	}
	owner := projectSessionOwnerID(created.ProjectPath)
	app.assistantSessionWorkingDirs.Store(owner, stale)
	app.tabWorkingDirOverrides.Store(tabID, stale)

	if got, ok := app.claimedWorkRoot(created.ProjectPath); !ok || normalizeProjectSessionPath(got) != repo {
		t.Fatalf("claim %q ok=%v want %s", got, ok, repo)
	}
	if got := normalizeProjectSessionPath(app.projectTurnExecutionDir(owner, created.ProjectPath, "")); got != repo {
		t.Fatalf("turn dir %s want %s", got, repo)
	}
	if got := normalizeProjectSessionPath(fmt.Sprint(app.GetTabWorkingDir(tabID)["path"])); got != repo {
		t.Fatalf("header dir %s want %s", got, repo)
	}
	// The header already followed the tag. The chip refresh must also publish
	// that tag as the owner bind, which horizon and in-flight stamps read.
	if got := normalizeProjectSessionPath(app.BoundWorkingDirForOwner(owner)); got != repo {
		t.Fatalf("bind after header %s want %s", got, repo)
	}
	if value, ok := app.tabWorkingDirOverrides.Load(tabID); !ok || normalizeProjectSessionPath(fmt.Sprint(value)) != repo {
		t.Fatalf("override after header %v want %s", value, repo)
	}
	refreshed, err := persist.LoadSession(tabID)
	if err != nil || refreshed == nil {
		t.Fatalf("reload session after header: %v", err)
	}
	if got := normalizeProjectSessionPath(refreshed.WorkingDir); got != repo {
		t.Fatalf("session cache after header %s want %s", got, repo)
	}
	h := &IMMessageHandler{app: app}
	if got := normalizeProjectSessionPath(h.projectTabWorkDirForOwner(owner)); got != repo {
		t.Fatalf("tool dir %s want %s", got, repo)
	}
	if got := normalizeProjectSessionPath(app.recentTaskWorkingDir(created.ProjectPath)); got != repo {
		t.Fatalf("tag %s want %s", got, repo)
	}

	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); msg == "" {
		t.Fatal("reopen returned empty")
	}
	repaired, err := persist.LoadSession(tabID)
	if err != nil || repaired == nil {
		t.Fatalf("reload session: %v", err)
	}
	if got := normalizeProjectSessionPath(repaired.WorkingDir); got != repo {
		t.Fatalf("session cache %s want %s", got, repo)
	}
	if got := normalizeProjectSessionPath(app.BoundWorkingDirForOwner(owner)); got != repo {
		t.Fatalf("bind %s want %s", got, repo)
	}

	chat := app.CreateTaskWithMode("MCP chat", repo, "")
	if chat.ProjectPath == "" {
		t.Fatal("empty chat path")
	}
	chatTab := "proj-mcp-chat-cache"
	if msg := app.CreateProjectTabSession(chatTab, chat.ProjectPath); msg == "" {
		t.Fatal("chat session returned empty")
	}
	chatSession, err := persist.LoadSession(chatTab)
	if err != nil || chatSession == nil {
		t.Fatalf("load chat session: %v", err)
	}
	chatSession.WorkingDir = stale
	if err := persist.SaveSession(chatSession); err != nil {
		t.Fatal(err)
	}
	chatOwner := projectSessionOwnerID(chat.ProjectPath)
	app.assistantSessionWorkingDirs.Store(chatOwner, stale)
	app.tabWorkingDirOverrides.Store(chatTab, stale)
	if got := normalizeProjectSessionPath(fmt.Sprint(app.GetTabWorkingDir(chatTab)["path"])); got != stale {
		t.Fatalf("chat header %s want %s", got, stale)
	}
	if got := normalizeProjectSessionPath(app.BoundWorkingDirForOwner(chatOwner)); got != stale {
		t.Fatalf("chat bind %s want %s", got, stale)
	}
	chatAfterHeader, err := persist.LoadSession(chatTab)
	if err != nil || chatAfterHeader == nil {
		t.Fatalf("reload chat session after header: %v", err)
	}
	if got := normalizeProjectSessionPath(chatAfterHeader.WorkingDir); got != stale {
		t.Fatalf("chat session after header %s want %s", got, stale)
	}
	if msg := app.CreateProjectTabSession(chatTab, chat.ProjectPath); msg == "" {
		t.Fatal("chat reopen returned empty")
	}
	kept, err := persist.LoadSession(chatTab)
	if err != nil || kept == nil {
		t.Fatalf("reload chat session: %v", err)
	}
	if got := normalizeProjectSessionPath(kept.WorkingDir); got != stale {
		t.Fatalf("chat session %s want %s", got, stale)
	}
	if got := normalizeProjectSessionPath(app.projectTurnExecutionDir(chatOwner, chat.ProjectPath, "")); got != stale {
		t.Fatalf("chat turn dir %s want %s", got, stale)
	}
}

func TestInboundMCPMirrorTabUsesTaskDir(t *testing.T) {
	app := newProjectSearchTestApp(t)
	repo := normalizeProjectSessionPath(t.TempDir())
	created := app.CreateTaskWithMode("MCP coding", repo, taskCodingDevTag)
	if created.ProjectPath == "" {
		t.Fatal("empty project path")
	}
	owner := projectSessionOwnerID(created.ProjectPath)
	desktop := normalizeProjectSessionPath(app.EffectiveDesktopWorkingDir())
	if desktop == "" || desktop == repo {
		t.Fatalf("desktop fixture %q", desktop)
	}
	app.assistantSessionWorkingDirs.Store(owner, desktop)
	// The MCP host opens this task under the mirror tab id. Registration
	// publishes the task record over a desktop bind left on the owner.
	tabID := "acp-mcp-mirror"
	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); msg == "" {
		t.Fatal("CreateProjectTabSession returned empty")
	}
	if got := normalizeProjectSessionPath(fmt.Sprint(app.GetTabWorkingDir(tabID)["path"])); got != repo {
		t.Fatalf("header %s want %s", got, repo)
	}
	if got := normalizeProjectSessionPath(app.BoundWorkingDirForOwner(owner)); got != repo {
		t.Fatalf("bind %s want %s", got, repo)
	}
	h := &IMMessageHandler{app: app}
	if got := normalizeProjectSessionPath(h.projectTabWorkDirForOwner(owner)); got != repo {
		t.Fatalf("tool dir %s want %s", got, repo)
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

func TestInboundMCPTaskDetailRoundAndEndReason(t *testing.T) {
	app := &App{}
	started := time.Date(2026, 11, 18, 1, 2, 3, 0, time.UTC)
	app.mcpRememberPin("p", "Pinned", "pin-model")
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-1",
		Workspace:   `D:\repo`,
		Round:       2,
		StartedAt:   started,
	})
	view, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Provider != "Pinned" || view.Model != "pin-model" || view.WorkingDir != `D:\repo` || view.Round != 2 || view.StartedAt != started.Format(time.RFC3339) || view.EndReason != "" || view.FinishedAt != "" {
		t.Fatalf("running detail %+v", view)
	}
	if app.mcpNextRound("p") != 3 {
		t.Fatalf("next round %d", app.mcpNextRound("p"))
	}

	app.mcpFinishRun("p", "mcp-1", mcpStatusFailed, "结果", "boom", false, "", nil, nil)
	finished, ok := app.mcpViewFromRegistry("p")
	if !ok || finished.EndReason != mcpStatusFailed || finished.EndDetail != "boom" || finished.FinishedAt == "" || finished.Status != mcpStatusFailed {
		t.Fatalf("finished %+v", finished)
	}
	published := app.mcpPublishTaskView(finished)
	if published.Provider != "Pinned" || published.Model != "pin-model" || published.EndReason != mcpStatusFailed {
		t.Fatalf("published finish %+v", published)
	}

	peekApp := &App{}
	off := false
	peekApp.configCacheValid = true
	peekApp.configCache = corelib.AppConfig{MaclawLLMCurrentProvider: "Peek", MaclawLLMModel: "peek-m", ComputerUseEnabled: &off}
	peekApp.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-2", Provider: "Recorded", Model: "rec-m"})
	kept := peekApp.mcpPublishTaskView(mcpTaskView{ProjectPath: "p", Status: mcpStatusRunning, Provider: "Recorded", Model: "rec-m"})
	if kept.Provider != "Recorded" || kept.Model != "rec-m" {
		t.Fatalf("record wins %+v", kept)
	}
	filled := peekApp.mcpPublishTaskView(mcpTaskView{ProjectPath: "other", Status: mcpStatusIdle})
	if filled.Provider != "Peek" || filled.Model != "peek-m" {
		t.Fatalf("peek fill %+v", filled)
	}
}

func TestInboundMCPLogCursorRedactsSecrets(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-1"})
	app.mcpAppendLog("p", "mcp-1", mcpLogEvent{Kind: "progress", Text: "调用 mck_abc1234567 和 sk-supersecretvalue"})
	app.mcpRecordToolEvent("p", "mcp-1", ACPToolEvent{Phase: "end", Name: "read_file", OK: false, Result: "token=mck_deadbeef01"})
	page, err := app.mcpReadLog(context.Background(), "p", "", 0)
	if err != nil || len(page.Events) != 2 || page.NextCursor != 2 {
		t.Fatalf("page %+v err=%v", page, err)
	}
	raw, _ := json.Marshal(page)
	body := string(raw)
	if strings.Contains(body, "mck_abc1234567") || strings.Contains(body, "sk-supersecretvalue") || strings.Contains(body, "mck_deadbeef01") {
		t.Fatalf("secret leaked %s", body)
	}
	if !strings.Contains(body, "mck_[redacted]") || !strings.Contains(body, "sk-[redacted]") || page.Events[1].Kind != "tool" || page.Events[1].OK == nil || *page.Events[1].OK {
		t.Fatalf("redacted page %s", body)
	}
	rest, err := app.mcpReadLog(context.Background(), "p", "", page.Events[0].Seq)
	if err != nil || len(rest.Events) != 1 || rest.Events[0].Seq != 2 || rest.NextCursor != 2 {
		t.Fatalf("cursor %+v err=%v", rest, err)
	}
	empty, err := app.mcpReadLog(context.Background(), "p", "", 2)
	if err != nil || len(empty.Events) != 0 || empty.NextCursor != 2 {
		t.Fatalf("tail %+v err=%v", empty, err)
	}
	if _, err := app.mcpReadLog(context.Background(), "missing", "", 0); err == nil {
		t.Fatal("missing task returned a log")
	}
	app.mcpAppendLog("p", "mcp-1", mcpLogEvent{Kind: "error", Text: `"api_key": "supersecretvalue" ghp_abcdefghijklmnopqrstuvwxyz` + "\n-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"})
	secretPage, err := app.mcpReadLog(context.Background(), "p", "", 2)
	if err != nil || len(secretPage.Events) != 1 {
		t.Fatalf("secret page %+v err=%v", secretPage, err)
	}
	secretBody, _ := json.Marshal(secretPage)
	if strings.Contains(string(secretBody), "supersecretvalue") || strings.Contains(string(secretBody), "ghp_abcdefghijklmnopqrstuvwxyz") || strings.Contains(string(secretBody), "BEGIN PRIVATE KEY") {
		t.Fatalf("secret leaked %s", secretBody)
	}
	app.mcpReplaceRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-2"})
	app.mcpAppendLog("p", "mcp-2", mcpLogEvent{Kind: "progress", Text: "下一轮"})
	continued, err := app.mcpReadLog(context.Background(), "p", "", secretPage.NextCursor)
	if err != nil || len(continued.Events) != 1 || continued.Events[0].Seq != secretPage.NextCursor+1 || continued.Gap || continued.Events[0].Text != "下一轮" {
		t.Fatalf("continued %+v err=%v", continued, err)
	}
	app.mcpRunsMu.Lock()
	if rec := app.mcpRuns["p"]; rec != nil && len(rec.logs) > 1 {
		rec.logs = rec.logs[1:]
	}
	app.mcpRunsMu.Unlock()
	gapped, err := app.mcpReadLog(context.Background(), "p", "", 0)
	if err != nil || !gapped.Gap || len(gapped.Events) == 0 || gapped.Events[0].Seq <= 1 {
		t.Fatalf("gap %+v err=%v", gapped, err)
	}

	var got string
	clear := globalACPToolSinks.set("mcp-tool", func(ev ACPToolEvent) { got = ev.Name })
	defer clear()
	emitACPToolEventForRequest("mcp-tool", ACPToolEvent{Name: "read_file", Phase: "start"})
	if got != "read_file" {
		t.Fatalf("mcp tool sink %q", got)
	}
	got = ""
	clearACP := globalACPToolSinks.set("acp-tool", func(ev ACPToolEvent) { got = ev.Name })
	defer clearACP()
	emitACPToolEventForRequest("acp-tool", ACPToolEvent{Name: "bash", Phase: "start"})
	if got != "bash" {
		t.Fatalf("acp tool sink %q", got)
	}
}

func TestInboundMCPConfirmationIDIsSingleUse(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath:      "p",
		Status:           mcpStatusNeedsConfirmation,
		ConfirmationKind: mcpConfirmKindPlan,
		Commands:         mcpPlanDecisionCommands(),
		RequestID:        "mcp-1",
		Round:            1,
	})
	reg, ok := app.mcpViewFromRegistry("p")
	if !ok {
		t.Fatal("missing run")
	}
	first := app.mcpReturnTask(reg)
	second := app.mcpReturnTask(reg)
	if !strings.HasPrefix(first.ConfirmationID, "mcf_") || first.ConfirmationID != second.ConfirmationID {
		t.Fatalf("stable id %q %q", first.ConfirmationID, second.ConfirmationID)
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "/plan approve", "mcf_stale", "", false); err == nil || !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("stale plan id: %v", err)
	}
	if err := app.mcpRequireListedConfirmation("p", reg, true, "/plan approve", first.ConfirmationID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.mcpSendCommand(context.Background(), "p", "/plan approve", first.ConfirmationID, "", false); err == nil || !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("spent plan id: %v", err)
	}
	reg, _ = app.mcpViewFromRegistry("p")
	renewed := app.mcpReturnTask(reg)
	if renewed.ConfirmationID == "" || renewed.ConfirmationID == first.ConfirmationID {
		t.Fatalf("renewed id %q", renewed.ConfirmationID)
	}

	scopeCh := make(chan ScopeApprovalDecision, 1)
	scopeID := storePendingScopeApproval(nil, ScopeApprovalRequest{ProjectPath: "q", Message: "写外面"}, scopeCh)
	t.Cleanup(func() { pendingScopeApprovals.Delete(scopeID) })
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "q", Status: mcpStatusRunning, RequestID: "mcp-q"})
	gated, err := app.mcpGetTask(context.Background(), "q", "")
	if err != nil || gated.ConfirmationID == "" {
		t.Fatalf("gate %+v err=%v", gated, err)
	}
	if _, err := app.mcpSendCommand(context.Background(), "q", "__scope_approval__ "+scopeID+" allow_dir", "mcf_wrong", "", false); err == nil || !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("wrong scope id: %v", err)
	}
	select {
	case decision := <-scopeCh:
		t.Fatalf("wrong id delivered %q", decision)
	default:
	}
	if _, err := app.mcpSendCommand(context.Background(), "q", "__scope_approval__ "+scopeID+" allow_dir", gated.ConfirmationID, "", false); err != nil {
		t.Fatal(err)
	}
	select {
	case decision := <-scopeCh:
		if decision != ScopeApprovalAllowDir {
			t.Fatalf("decision %q", decision)
		}
	default:
		t.Fatal("matching id was not delivered")
	}
	if _, err := app.mcpSendCommand(context.Background(), "q", "__scope_approval__ "+scopeID+" deny", gated.ConfirmationID, "", false); err == nil || !strings.Contains(err.Error(), "不匹配") && !strings.Contains(err.Error(), "已过期") {
		t.Fatalf("spent scope id: %v", err)
	}
}

func TestInboundMCPArtifactsStayInTheWorkDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg.js"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shot.bin"), []byte{0xff, 0xfe, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "id_ed25519"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusCompleted, Workspace: root, RequestID: "mcp-1"})
	list, err := app.mcpListArtifacts(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(list)
	body := string(raw)
	if strings.Contains(body, ".env") || strings.Contains(body, "pkg.js") || strings.Contains(body, "node_modules") || strings.Contains(body, "id_ed25519") {
		t.Fatalf("listed a skipped file %s", body)
	}
	var report mcpArtifactFile
	for _, file := range list.Files {
		if file.Path == "report.txt" {
			report = file
		}
	}
	sum := sha256.Sum256([]byte("hello"))
	want := hex.EncodeToString(sum[:])
	if report.Size != 5 || report.SHA256 != want {
		t.Fatalf("report %+v", report)
	}
	text, err := app.mcpReadArtifact(context.Background(), "p", "report.txt", false)
	if err != nil || text.Text != "hello" || text.SHA256 != want || text.Encoding != "utf-8" {
		t.Fatalf("read %+v err=%v", text, err)
	}
	downloaded, err := app.mcpReadArtifact(context.Background(), "p", "report.txt", true)
	if err != nil || downloaded.Content != base64.StdEncoding.EncodeToString([]byte("hello")) || downloaded.Text != "" {
		t.Fatalf("download %+v err=%v", downloaded, err)
	}
	if _, err := app.mcpReadArtifact(context.Background(), "p", ".env", false); err == nil || !strings.Contains(err.Error(), "密钥") {
		t.Fatalf("env: %v", err)
	}
	if _, err := app.mcpReadArtifact(context.Background(), "p", filepath.Join("..", "outside.txt"), false); err == nil || !strings.Contains(err.Error(), "工作目录") {
		t.Fatalf("escape: %v", err)
	}
	if _, err := app.mcpReadArtifact(context.Background(), "p", "NUL.txt", false); err == nil || !strings.Contains(err.Error(), "工作目录") {
		t.Fatalf("device: %v", err)
	}
	if _, err := app.mcpReadArtifact(context.Background(), "p", "shot.bin", false); err == nil || !strings.Contains(err.Error(), "二进制") {
		t.Fatalf("binary: %v", err)
	}
	if _, err := app.mcpReadArtifact(context.Background(), "p", "id_ed25519", false); err == nil || !strings.Contains(err.Error(), "密钥") {
		t.Fatalf("key name: %v", err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("hidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "linked")
	alias := filepath.Join(root, "alias.txt")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Logf("symlink skipped: %v", err)
	} else if err := os.Symlink(filepath.Join(outside, "secret.txt"), alias); err != nil {
		t.Logf("file symlink skipped: %v", err)
	} else {
		if _, err := app.mcpReadArtifact(context.Background(), "p", filepath.Join("linked", "secret.txt"), true); err == nil || !strings.Contains(err.Error(), "链接") && !strings.Contains(err.Error(), "工作目录") {
			t.Fatalf("symlink dir: %v", err)
		}
		if _, err := app.mcpReadArtifact(context.Background(), "p", "alias.txt", true); err == nil || !strings.Contains(err.Error(), "链接") {
			t.Fatalf("symlink file: %v", err)
		}
		again, err := app.mcpListArtifacts(context.Background(), "p")
		if err != nil {
			t.Fatal(err)
		}
		listed, _ := json.Marshal(again)
		if strings.Contains(string(listed), "secret.txt") || strings.Contains(string(listed), "alias.txt") {
			t.Fatalf("listed a link %s", listed)
		}
	}
	linkSrc := filepath.Join(root, "report.txt")
	linkDst := filepath.Join(root, "report-link.txt")
	if err := os.Link(linkSrc, linkDst); err != nil {
		t.Logf("hard link skipped: %v", err)
	} else {
		srcInfo, err := os.Lstat(linkSrc)
		if err != nil {
			t.Fatal(err)
		}
		dstInfo, err := os.Lstat(linkDst)
		if err != nil {
			t.Fatal(err)
		}
		if !mcpInfoIsSensitive(dstInfo, []os.FileInfo{srcInfo}) {
			t.Fatal("hard link did not match the source file")
		}
	}
}

func TestInboundMCPClientRequestAndCapabilities(t *testing.T) {
	app := &App{}
	req, claimed, err := app.mcpClaimRequest("c-1", mcpRequestKindCreate, "", "实现登录")
	if err != nil || !claimed || req == nil {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
	app.mcpSucceedRequest(req, "p")
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-1", Provider: "Daily", Model: "m", Round: 1, StartedAt: time.Now()})
	again, claimed, err := app.mcpClaimRequest("c-1", mcpRequestKindCreate, "", "实现登录")
	if err != nil || claimed {
		t.Fatalf("retry claimed=%v err=%v", claimed, err)
	}
	view, err := app.mcpReplayRequest(context.Background(), again)
	if err != nil || view.ProjectPath != "p" || view.Status != mcpStatusRunning || view.Provider != "Daily" || view.Round != 1 {
		t.Fatalf("replay %+v err=%v", view, err)
	}
	if _, _, err := app.mcpClaimRequest("c-1", mcpRequestKindCreate, "", "别的内容"); err == nil || !strings.Contains(err.Error(), "另一条") {
		t.Fatalf("different body: %v", err)
	}
	state, err := app.mcpGetRequest(context.Background(), "c-1")
	if err != nil || !state.Accepted || state.Pending || state.ProjectPath != "p" || state.Task == nil || state.Task.Provider != "Daily" {
		t.Fatalf("query %+v err=%v", state, err)
	}
	missing, err := app.mcpGetRequest(context.Background(), "nope")
	if err != nil || missing.Accepted {
		t.Fatalf("missing %+v err=%v", missing, err)
	}
	failed, claimed, err := app.mcpClaimRequest("c-2", mcpRequestKindCommand, "/plan approve", "")
	if err != nil || !claimed {
		t.Fatal(err)
	}
	app.mcpFailRequest(failed)
	if _, claimed, err := app.mcpClaimRequest("c-2", mcpRequestKindCommand, "/plan approve", ""); err != nil || !claimed {
		t.Fatalf("failed request stayed claimed=%v err=%v", claimed, err)
	}
	if _, _, err := app.mcpClaimRequest(strings.Repeat("a", mcpClientRequestIDRunes+1), mcpRequestKindCreate, "", "x"); err == nil || !strings.Contains(err.Error(), "过长") {
		t.Fatalf("long id: %v", err)
	}
	if _, _, err := app.mcpClaimRequest("c-path", mcpRequestKindCommand, mcpCommandClaimKey("a", "继续", "mcf_1"), ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.mcpClaimRequest("c-path", mcpRequestKindCommand, mcpCommandClaimKey("b", "继续", "mcf_1"), ""); err == nil || !strings.Contains(err.Error(), "另一条") {
		t.Fatalf("same id other path: %v", err)
	}
	if _, _, err := app.mcpClaimRequest("c-path", mcpRequestKindCommand, mcpCommandClaimKey("a", "继续", "mcf_2"), ""); err == nil || !strings.Contains(err.Error(), "另一条") {
		t.Fatalf("same id other confirmation: %v", err)
	}
	waiting, claimed, err := app.mcpClaimRequest("c-wait", mcpRequestKindCommand, "继续", "")
	if err != nil || !claimed {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.mcpReplayRequest(cancelled, waiting); err == nil || !strings.Contains(err.Error(), "get_request") {
		t.Fatalf("cancelled replay: %v", err)
	}
	pendingState, err := app.mcpGetRequest(context.Background(), "c-wait")
	if err != nil || !pendingState.Accepted || !pendingState.Pending {
		t.Fatalf("pending %+v err=%v", pendingState, err)
	}
	bound, claimed, err := app.mcpClaimRequest("c-err", mcpRequestKindCommand, "/plan approve", "")
	if err != nil || !claimed {
		t.Fatal(err)
	}
	app.mcpBindRequest(bound, "p")
	app.mcpCompleteRequest(bound, errors.New("确认已过期或已处理"))
	replayed, claimed, err := app.mcpClaimRequest("c-err", mcpRequestKindCommand, "/plan approve", "")
	if err != nil || claimed {
		t.Fatalf("failed accept claimed=%v err=%v", claimed, err)
	}
	if _, err := app.mcpReplayRequest(context.Background(), replayed); err == nil || !strings.Contains(err.Error(), "已过期") {
		t.Fatalf("replayed failure: %v", err)
	}
	failedState, err := app.mcpGetRequest(context.Background(), "c-err")
	if err != nil || !failedState.Accepted || failedState.Pending || !strings.Contains(failedState.Error, "已过期") {
		t.Fatalf("failed query %+v err=%v", failedState, err)
	}
	evict := &App{}
	for i := 0; i <= mcpClientRequestLimit; i++ {
		id := fmt.Sprintf("e-%d", i)
		item, claimed, err := evict.mcpClaimRequest(id, mcpRequestKindCreate, "", "same")
		if err != nil || !claimed {
			t.Fatalf("evict claim %s claimed=%v err=%v", id, claimed, err)
		}
		evict.mcpSucceedRequest(item, "p")
	}
	oldest, err := evict.mcpGetRequest(context.Background(), "e-0")
	if err != nil || oldest.Accepted {
		t.Fatalf("oldest stayed %+v err=%v", oldest, err)
	}
	newest, err := evict.mcpGetRequest(context.Background(), fmt.Sprintf("e-%d", mcpClientRequestLimit))
	if err != nil || !newest.Accepted || newest.Pending {
		t.Fatalf("newest %+v err=%v", newest, err)
	}

	info, err := app.mcpCapabilities(context.Background())
	if err != nil || info.APIVersion != 1 || info.ModelConnection != "unconfigured" || !info.BrowserTest || !info.Screenshot || info.Shell {
		t.Fatalf("bare capabilities %+v err=%v", info, err)
	}
	if !mcpBrowserMethodsReady(info.BrowserMethods, true) {
		t.Fatalf("browser methods %+v", info.BrowserMethods)
	}
	joined := strings.Join(info.Features, " ")
	if !strings.Contains(joined, "read_log") || !strings.Contains(joined, "get_request") || strings.Contains(joined, "shell") || !strings.Contains(joined, "retry_steps") || !strings.Contains(joined, "browser_test") || !strings.Contains(joined, "pack_artifacts") {
		t.Fatalf("features %s", joined)
	}
	off := false
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{MaclawLLMCurrentProvider: "Daily", MaclawLLMModel: "m", ComputerUseEnabled: &off}
	info, err = app.mcpCapabilities(context.Background())
	if err != nil || info.ModelConnection != "configured" || info.Provider != "Daily" || info.Model != "m" || info.BrowserTest || info.Screenshot || !mcpBrowserMethodsReady(info.BrowserMethods, false) {
		t.Fatalf("configured capabilities %+v err=%v", info, err)
	}
}

func mcpBrowserMethodsReady(methods []mcpBrowserMethod, available bool) bool {
	want := map[string]bool{"open": false, "observe": false, "click": false, "key": false, "screenshot": false, "assert": false}
	for _, method := range methods {
		if method.Tool != "browser_test" || method.Action != method.Name {
			return false
		}
		if _, ok := want[method.Name]; !ok {
			return false
		}
		if method.Available != available {
			return false
		}
		want[method.Name] = true
	}
	for _, ok := range want {
		if !ok {
			return false
		}
	}
	return true
}

func TestInboundMCPFailedStepIsNotCompleted(t *testing.T) {
	failed := &IMAgentResponse{
		Text: "Finished 4 of 5 steps; 1 still failed.",
		CodingSteps: []CodingStepOutcome{
			{ID: "T4", Title: "画面", Status: "passed"},
			{ID: "T5", Title: "浏览器验收", Status: "failed", Error: "模型连续返回空响应，任务中断", Retryable: true},
		},
	}
	status, _, errText, _, steps := mcpRunOutcome(nil, false, failed)
	if status != mcpStatusFailed || errText != "模型连续返回空响应，任务中断" || len(steps) != 2 {
		t.Fatalf("status=%s err=%q steps=%d", status, errText, len(steps))
	}
	if steps[0].Retryable || steps[0].Status != mcpStepPassed || !steps[1].Retryable || steps[1].Status != mcpStepFailed || steps[1].Error == "" {
		t.Fatalf("steps %+v", steps)
	}
	running := &IMAgentResponse{Text: "还在做", CodingSteps: []CodingStepOutcome{{ID: "T2", Status: "running"}}}
	status, _, _, _, steps = mcpRunOutcome(nil, false, running)
	if status != mcpStatusFailed || len(steps) != 1 || steps[0].Status != mcpStepUnfinished || !steps[0].Retryable {
		t.Fatalf("unfinished status=%s %+v", status, steps)
	}
	status, _, _, _, _ = mcpRunOutcome(nil, true, failed)
	if status != mcpStatusCancelled {
		t.Fatalf("cancel lost %s", status)
	}
	waiting := &IMAgentResponse{Text: "请确认", Confirmation: &IMResponseConfirmation{Summary: "实现登录"}, CodingSteps: failed.CodingSteps}
	status, _, _, _, _ = mcpRunOutcome(nil, false, waiting)
	if status != mcpStatusNeedsConfirmation {
		t.Fatalf("confirmation lost %s", status)
	}
	passed := &IMAgentResponse{Text: "写好了", CodingSteps: []CodingStepOutcome{{ID: "T1", Status: "passed"}}}
	status, _, errText, _, _ = mcpRunOutcome(nil, false, passed)
	if status != mcpStatusCompleted || errText != "" {
		t.Fatalf("passed status=%s err=%q", status, errText)
	}
	hard := &IMAgentResponse{Text: "模型连续返回空响应，任务中断", HardExit: true}
	status, _, errText, _, steps = mcpRunOutcome(nil, false, hard)
	if status != mcpStatusFailed || errText != "模型连续返回空响应，任务中断" || len(steps) != 1 || !steps[0].Retryable {
		t.Fatalf("hard status=%s err=%q steps=%+v", status, errText, steps)
	}
	prose := &IMAgentResponse{Text: "Finished 4 of 5 steps; 1 still failed."}
	status, _, errText, _, steps = mcpRunOutcome(nil, false, prose)
	if status != mcpStatusFailed || errText == "" || len(steps) != 1 || !steps[0].Retryable {
		t.Fatalf("prose status=%s err=%q steps=%+v", status, errText, steps)
	}
}

func TestInboundMCPDirectedStepSelectionKeepsPassedWork(t *testing.T) {
	tasks := []*v2.TaskItem{{Index: 4, Title: "画面"}, {Index: 5, Title: "验收"}}
	results := []v2.TaskRunResult{
		{TaskIndex: 4, Title: "画面", Status: v2.TaskPassed},
		{TaskIndex: 5, Title: "验收", Status: v2.TaskFailed, Error: "空响应"},
	}
	selected, err := codingExecSelectRetrySteps(tasks, results, []string{"T5"})
	if err != nil || len(selected) != 1 || selected[0].Index != 5 {
		t.Fatalf("selected %+v err=%v", selected, err)
	}
	if _, err := codingExecSelectRetrySteps(tasks, results, []string{"T4"}); err == nil || !strings.Contains(err.Error(), "已经通过") {
		t.Fatalf("passed: %v", err)
	}
	if _, err := codingExecSelectRetrySteps(tasks, results, []string{"T9"}); err == nil || !strings.Contains(err.Error(), "没有步骤") {
		t.Fatalf("missing: %v", err)
	}
	mem := stickyCodingWorkbenchMemory{StepStatuses: []codingWorkbenchStepStatus{
		{Index: 4, Title: "画面", Status: codingStepPassed},
		{Index: 5, Title: "验收", Status: codingStepFailed, Summary: "空响应"},
	}}
	planned, err := codingPlanSelectNamedSteps(mem, []string{"5"})
	if err != nil || len(planned) != 1 || planned[0].Index != 5 {
		t.Fatalf("plan %+v err=%v", planned, err)
	}
	if _, err := codingPlanSelectNamedSteps(mem, []string{"步骤 4"}); err == nil || !strings.Contains(err.Error(), "已经通过") {
		t.Fatalf("plan passed: %v", err)
	}
	merged := mcpMergeSteps(
		[]mcpStepView{{ID: "T4", Status: mcpStepPassed}, {ID: "T5", Status: mcpStepFailed, Error: "空响应", Retryable: true}},
		[]mcpStepView{{ID: "T5", Status: mcpStepPassed}},
	)
	if len(merged) != 2 || merged[0].ID != "T4" || merged[0].Status != mcpStepPassed || merged[1].ID != "T5" || merged[1].Status != mcpStepPassed {
		t.Fatalf("merged %+v", merged)
	}
	ids := mcpLookupStrings(map[string]any{"step_ids": []any{"T4", float64(5)}}, "step_ids")
	norm, err := mcpNormalizeStepIDs(ids)
	if err != nil || len(norm) != 2 || norm[0] != "T4" || norm[1] != "T5" {
		t.Fatalf("ids %v err=%v", norm, err)
	}
}

func TestInboundMCPRetryStepsKeepsRequestAndConfirmation(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-run"})
	_, err := app.mcpRetrySteps(context.Background(), "p", []string{"T5"}, "", "retry-1", false)
	if err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("running: %v", err)
	}
	_, err = app.mcpRetrySteps(context.Background(), "p", []string{"T5"}, "", "retry-1", false)
	if err == nil || strings.Contains(err.Error(), "另一条") || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("reusable id: %v", err)
	}

	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath:    "p",
		Status:         mcpStatusFailed,
		ConfirmationID: "mcf_keep",
		Steps: []mcpStepView{
			{ID: "T4", Status: mcpStepPassed},
			{ID: "T5", Status: mcpStepFailed, Error: "空响应", Retryable: true},
		},
	})
	_, err = app.mcpRetrySteps(context.Background(), "p", []string{"T4"}, "mcf_keep", "", false)
	if err == nil || !strings.Contains(err.Error(), "已经通过") {
		t.Fatalf("passed step: %v", err)
	}
	kept, ok := app.mcpViewFromRegistry("p")
	if !ok || kept.ConfirmationID != "mcf_keep" {
		t.Fatalf("passed burned confirmation %+v", kept)
	}
	_, err = app.mcpRetrySteps(context.Background(), "p", []string{"T5"}, "mcf_wrong", "retry-2", false)
	if err == nil || !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("mismatch: %v", err)
	}
	kept, ok = app.mcpViewFromRegistry("p")
	if !ok || kept.ConfirmationID != "mcf_keep" {
		t.Fatalf("mismatch burned confirmation %+v", kept)
	}
	_, err = app.mcpRetrySteps(context.Background(), "p", []string{"T5"}, "mcf_keep", "retry-2", false)
	if err == nil || !strings.Contains(err.Error(), "检查点") {
		t.Fatalf("checkpoint: %v", err)
	}
	kept, ok = app.mcpViewFromRegistry("p")
	if !ok || kept.ConfirmationID != "mcf_keep" {
		t.Fatalf("match burned confirmation %+v", kept)
	}
	_, err = app.mcpRetrySteps(context.Background(), "p", []string{"T5"}, "mcf_keep", "retry-2", false)
	if err == nil || strings.Contains(err.Error(), "另一条") || !strings.Contains(err.Error(), "检查点") {
		t.Fatalf("checkpoint id reusable: %v", err)
	}
}

func TestInboundMCPBrowserEvidenceFollowsTheStep(t *testing.T) {
	dir := normalizeProjectSessionPath(t.TempDir())
	body := []byte("game")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	wantHash := hex.EncodeToString(sum[:])
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: dir,
		Status:      mcpStatusFailed,
		Workspace:   dir,
		Mode:        "coding_dev",
		Steps:       []mcpStepView{{ID: "T5", Status: mcpStepFailed, Error: "验收中断", Retryable: true, Files: []mcpStepFile{{Path: "index.html"}}}},
	})
	_, err := app.mcpBrowserTest(context.Background(), dir, "observe", "", "", "", "", "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "还没有打开的任务页面") {
		t.Fatalf("closed page: %v", err)
	}
	png := []byte("png-bytes")
	prev := mcpBrowserDriveFunc
	t.Cleanup(func() { mcpBrowserDriveFunc = prev })
	mcpBrowserDriveFunc = func(req mcpBrowserDriveRequest) (mcpBrowserDriveResult, error) {
		if req.StepID != "T5" {
			t.Errorf("step %s", req.StepID)
		}
		return mcpBrowserDriveResult{OK: true, PNG: png, Detail: "seen"}, nil
	}
	shot, err := app.mcpBrowserTest(context.Background(), dir, "screenshot", "", "", "", "", "", "T5", nil)
	if err != nil || !shot.OK || shot.StepID != "T5" || len(shot.Files) != 1 || shot.Files[0].SHA256 != wantHash || shot.ScreenshotSHA256 == "" || shot.Content == "" {
		t.Fatalf("shot %+v err=%v", shot, err)
	}
	pngSum := sha256.Sum256(png)
	if shot.ScreenshotSHA256 != hex.EncodeToString(pngSum[:]) {
		t.Fatalf("png hash %s", shot.ScreenshotSHA256)
	}
	mcpBrowserDriveFunc = func(req mcpBrowserDriveRequest) (mcpBrowserDriveResult, error) {
		return mcpBrowserDriveResult{OK: false, Detail: "页面里没有得分"}, nil
	}
	checked, err := app.mcpBrowserTest(context.Background(), dir, "assert", "", "", "", "", "得分", "步骤 5", nil)
	if err != nil || checked.OK || checked.StepID != "T5" {
		t.Fatalf("assert %+v err=%v", checked, err)
	}
	view, ok := app.mcpViewFromRegistry(dir)
	if !ok || len(view.Checks) != 2 || len(view.Steps) != 1 || len(view.Steps[0].Checks) != 2 {
		t.Fatalf("checks view=%+v ok=%v", view, ok)
	}
	raw, err := json.Marshal(view)
	if err != nil || strings.Contains(string(raw), shot.Content) {
		t.Fatalf("screenshot bytes leaked into get_task")
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := app.mcpPublishTaskView(view)
	if len(fresh.Checks) != 2 || !fresh.Checks[0].Stale || !fresh.Steps[0].Checks[0].Stale {
		t.Fatalf("stale %+v", fresh.Checks)
	}
	called := false
	mcpBrowserDriveFunc = func(req mcpBrowserDriveRequest) (mcpBrowserDriveResult, error) {
		called = true
		return mcpBrowserDriveResult{}, nil
	}
	off := false
	app.configCacheValid = true
	app.configCache = corelib.AppConfig{ComputerUseEnabled: &off}
	_, err = app.mcpBrowserTest(context.Background(), dir, "observe", "", "", "", "", "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "浏览器测试没有打开") || called {
		t.Fatalf("switch off called=%v err=%v", called, err)
	}
}

func TestInboundMCPPackArtifactsReturnsManifestAndHash(t *testing.T) {
	dir := normalizeProjectSessionPath(t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: dir, Status: mcpStatusFailed, Workspace: dir, Mode: "coding_dev"})
	packed, err := app.mcpPackArtifacts(context.Background(), dir, []string{"a.txt", "b.txt", "../outside", ".env", "sub"})
	if err != nil {
		t.Fatal(err)
	}
	if len(packed.Files) != 2 || packed.ArchiveSHA256 == "" || packed.Encoding != "base64" || len(packed.Omitted) != 3 {
		t.Fatalf("pack %+v", packed)
	}
	byPath := map[string]mcpPackFile{}
	for _, file := range packed.Files {
		byPath[file.Path] = file
	}
	alpha := sha256.Sum256([]byte("alpha"))
	beta := sha256.Sum256([]byte("beta"))
	if byPath["a.txt"].SHA256 != hex.EncodeToString(alpha[:]) || byPath["b.txt"].SHA256 != hex.EncodeToString(beta[:]) {
		t.Fatalf("manifest %+v", packed.Files)
	}
	raw, err := base64.StdEncoding.DecodeString(packed.Content)
	if err != nil {
		t.Fatal(err)
	}
	archive := sha256.Sum256(raw)
	if hex.EncodeToString(archive[:]) != packed.ArchiveSHA256 || int64(len(raw)) != packed.ArchiveSize {
		t.Fatalf("archive hash %s size %d", packed.ArchiveSHA256, packed.ArchiveSize)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, file := range reader.File {
		names[file.Name] = true
	}
	if !names["a.txt"] || !names["b.txt"] || names[".env"] || names["sub"] || names["../outside"] {
		t.Fatalf("zip names %v", names)
	}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: dir, Status: mcpStatusFailed, Workspace: dir, Mode: taskRemoteCodingDevTag, RemoteHost: "example"})
	if _, err := app.mcpPackArtifacts(context.Background(), dir, []string{"a.txt"}); err == nil || !strings.Contains(err.Error(), "SSH") {
		t.Fatalf("remote pack: %v", err)
	}
}

func TestInboundMCPMergedOutcomeKeepsOpenFailures(t *testing.T) {
	app := &App{}
	shot := mcpCheckView{Action: "screenshot", StepID: "T5", OK: true, Files: []mcpStepFile{{Path: "index.html", SHA256: "abc"}}}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-1",
		Steps: []mcpStepView{
			{ID: "T4", Status: mcpStepPassed},
			{ID: "T5", Status: mcpStepFailed, Error: "空响应", Retryable: true, Checks: []mcpCheckView{shot}},
			{ID: "T6", Status: mcpStepFailed, Error: "还没做", Retryable: true},
		},
	})
	app.mcpFinishRun("p", "mcp-1", mcpStatusCompleted, "T5 好了", "", false, "", nil, []mcpStepView{{ID: "T5", Status: mcpStepPassed}})
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusFailed || view.Error != "还没做" || len(view.Steps) != 3 {
		t.Fatalf("partial %+v ok=%v", view, ok)
	}
	if view.Steps[0].ID != "T4" || view.Steps[0].Status != mcpStepPassed || view.Steps[1].ID != "T5" || view.Steps[1].Status != mcpStepPassed {
		t.Fatalf("passed %+v", view.Steps)
	}
	if len(view.Steps[1].Checks) != 1 || len(view.Steps[1].Checks[0].Files) != 1 || view.Steps[1].Checks[0].Files[0].SHA256 != "abc" {
		t.Fatalf("check %+v", view.Steps[1].Checks)
	}
	if view.Steps[2].ID != "T6" || view.Steps[2].Status != mcpStepFailed {
		t.Fatalf("open %+v", view.Steps[2])
	}

	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-2",
		Steps: []mcpStepView{
			{ID: "T4", Status: mcpStepPassed},
			{ID: "T5", Status: mcpStepFailed, Error: "旧原因", Retryable: true},
		},
	})
	app.mcpFinishRun("p", "mcp-2", mcpStatusFailed, "模型连续返回空响应，任务中断", "模型连续返回空响应，任务中断", false, "", nil, []mcpStepView{{
		Status: mcpStepFailed, Error: "模型连续返回空响应，任务中断", Retryable: true,
	}})
	view, ok = app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusFailed || len(view.Steps) != 2 || view.Steps[0].Status != mcpStepPassed || view.Steps[1].ID != "T5" || view.Steps[1].Error != "模型连续返回空响应，任务中断" {
		t.Fatalf("unscoped %+v ok=%v", view, ok)
	}

	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-3",
		Steps: []mcpStepView{
			{ID: "T4", Status: mcpStepPassed},
			{ID: "T5", Status: mcpStepPassed},
		},
	})
	app.mcpFinishRun("p", "mcp-3", mcpStatusFailed, "模型连续返回空响应，任务中断", "模型连续返回空响应，任务中断", false, "", nil, []mcpStepView{{
		Status: mcpStepFailed, Error: "模型连续返回空响应，任务中断", Retryable: true,
	}})
	view, ok = app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusFailed || len(view.Steps) != 3 || view.Steps[0].Status != mcpStepPassed || view.Steps[1].Status != mcpStepPassed || view.Steps[2].Error != "模型连续返回空响应，任务中断" {
		t.Fatalf("append %+v ok=%v", view, ok)
	}
}

func TestInboundMCPDirectedRetrySurvivesCheckpointGate(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user-mcp-retry"
	h.codingExecCheckpoint.Store(userID, codingExecCheckpoint{
		Tasks: []*v2.TaskItem{{Index: 5, Title: "验收"}},
	})
	h.storeCodingExecRetrySteps(userID, []string{"T5"})
	route := h.tryQueueCodingExecRetryCommand(userID, "重试失败")
	if route == nil || route.Response != nil || !route.WorkflowAgentLoop {
		t.Fatalf("queue %+v", route)
	}
	if ids := h.peekCodingExecRetrySteps(userID); len(ids) != 1 || ids[0] != "T5" {
		t.Fatalf("ids %v", ids)
	}
	raw, ok := h.pendingCodingExecRetryAction.Load(userID)
	action, _ := raw.(string)
	if !ok || action != codingExecRetryActionFailed {
		t.Fatalf("action %v %q", ok, action)
	}

	h.storeCodingExecRetrySteps(userID, []string{"T5"})
	h.pendingCodingExecRetryAction.Store(userID, codingExecRetryActionFailed)
	resp := h.handleWorkflowV2ExecutionPhaseWithProgress(userID, nil, nil, nil, nil)
	if resp == nil || (!strings.Contains(resp.Text, "retry") && !strings.Contains(resp.Text, "重试") && !strings.Contains(resp.Text, "重試")) {
		t.Fatalf("phase %+v", resp)
	}
	if ids := h.peekCodingExecRetrySteps(userID); len(ids) != 0 {
		t.Fatalf("ids left %v", ids)
	}
}

func TestInboundMCPPackStopsAtDownloadBudget(t *testing.T) {
	prev := mcpPackBudgetBytes
	mcpPackBudgetBytes = 8
	t.Cleanup(func() { mcpPackBudgetBytes = prev })
	dir := normalizeProjectSessionPath(t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: dir, Status: mcpStatusFailed, Workspace: dir, Mode: "coding_dev"})
	if _, err := app.mcpPackArtifacts(context.Background(), dir, []string{"a.txt", "b.txt"}); err == nil || !strings.Contains(err.Error(), "打包结果超过下载上限") {
		t.Fatalf("budget: %v", err)
	}
	packed, err := app.mcpPackArtifacts(context.Background(), dir, []string{"a.txt", "missing-dir"})
	if err != nil || len(packed.Files) != 1 || packed.Files[0].Path != "a.txt" || len(packed.Omitted) != 1 {
		t.Fatalf("room left %+v err=%v", packed, err)
	}
}

func TestInboundMCPPriorSkipBecomesRetryableAfterParentPasses(t *testing.T) {
	prev := []mcpStepView{
		{ID: "T4", Status: mcpStepPassed},
		{ID: "T5", Status: mcpStepFailed, Error: "空响应", Retryable: true},
		{ID: "T6", Status: mcpStepSkipped, Error: "skipped: prior step failed"},
	}
	held := mcpMergeSteps(prev, nil)
	if len(held) != 3 || held[2].ID != "T6" || held[2].Status != mcpStepSkipped || held[2].Retryable {
		t.Fatalf("held %+v", held)
	}
	if err := mcpRetryRejectedBySteps(held, []string{"T6"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("held retry: %v", err)
	}

	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-skip",
		Steps:       prev,
	})
	app.mcpFinishRun("p", "mcp-skip", mcpStatusCompleted, "T5 好了", "", false, "", nil, []mcpStepView{{ID: "T5", Status: mcpStepPassed}})
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusFailed || view.Error != mcpPriorSkipReadyError || len(view.Steps) != 3 {
		t.Fatalf("finish %+v ok=%v", view, ok)
	}
	got := view.Steps[2]
	if got.ID != "T6" || got.Status != mcpStepUnfinished || !got.Retryable || got.Error != mcpPriorSkipReadyError {
		t.Fatalf("reopened %+v", got)
	}
	if err := mcpRetryRejectedBySteps(view.Steps, []string{"T6"}); err != nil {
		t.Fatalf("retry: %v", err)
	}

	waiting := mcpStepsFromAgent(&IMAgentResponse{CodingSteps: []CodingStepOutcome{{
		ID: "T5", Status: mcpStepSkipped, Error: "waiting_child",
	}}})
	if len(waiting) != 1 || waiting[0].Status != mcpStepWaiting || waiting[0].Retryable || !mcpStepsBlockCompletion(waiting) {
		t.Fatalf("waiting %+v", waiting)
	}
	if merged := mcpMergeSteps(waiting, nil); len(merged) != 1 || merged[0].Status != mcpStepWaiting || merged[0].Retryable {
		t.Fatalf("waiting reopened %+v", merged)
	}
	if err := mcpRetryRejectedBySteps(waiting, []string{"T5"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("waiting retry: %v", err)
	}

	isolated := mcpStepsFromAgent(&IMAgentResponse{CodingSteps: []CodingStepOutcome{{
		ID: "T2", Status: mcpStepSkipped, Error: "skipped: remote isolation unavailable",
	}}})
	if len(isolated) != 1 || isolated[0].Status != mcpStepUnfinished || !isolated[0].Retryable || !mcpStepsBlockCompletion(isolated) {
		t.Fatalf("isolation %+v", isolated)
	}

	tasks := []*v2.TaskItem{{Index: 5, Title: "父"}, {Index: 6, Title: "子"}}
	blocked := []v2.TaskRunResult{
		{TaskIndex: 5, Status: v2.TaskFailed, Error: "空响应"},
		{TaskIndex: 6, Status: v2.TaskSkipped, Error: "skipped: prior step failed"},
	}
	if _, err := codingExecSelectRetrySteps(tasks, blocked, []string{"T6"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("exec blocked: %v", err)
	}
	ready := []v2.TaskRunResult{
		{TaskIndex: 5, Status: v2.TaskPassed},
		{TaskIndex: 6, Status: v2.TaskSkipped, Error: "skipped: prior step failed"},
	}
	selected, err := codingExecSelectRetrySteps(tasks, ready, []string{"T6"})
	if err != nil || len(selected) != 1 || selected[0].Index != 6 {
		t.Fatalf("exec ready %+v err=%v", selected, err)
	}
	if _, err := codingExecSelectRetrySteps(
		[]*v2.TaskItem{{Index: 5, Title: "交接"}},
		[]v2.TaskRunResult{{TaskIndex: 5, Status: v2.TaskSkipped, Error: "waiting_child"}},
		[]string{"T5"},
	); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("exec waiting: %v", err)
	}

	memBlocked := stickyCodingWorkbenchMemory{StepStatuses: []codingWorkbenchStepStatus{
		{Index: 5, Title: "父", Status: codingStepFailed, Summary: "空响应"},
		{Index: 6, Title: "子", Status: codingStepSkipped, Summary: "skipped: prior step failed"},
	}}
	if _, err := codingPlanSelectNamedSteps(memBlocked, []string{"T6"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("plan blocked: %v", err)
	}
	memReady := stickyCodingWorkbenchMemory{StepStatuses: []codingWorkbenchStepStatus{
		{Index: 5, Title: "父", Status: codingStepPassed},
		{Index: 6, Title: "子", Status: codingStepSkipped, Summary: "skipped: prior step failed"},
	}}
	planned, err := codingPlanSelectNamedSteps(memReady, []string{"T6"})
	if err != nil || len(planned) != 1 || planned[0].Index != 6 {
		t.Fatalf("plan ready %+v err=%v", planned, err)
	}
}

func TestInboundMCPDirectedIdsStayUntilTheRetryCommand(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user-mcp-retry-ids"
	h.storeCodingExecRetrySteps(userID, []string{"T5"})
	h.pendingCodingExecRetryAction.Store(userID, codingExecRetryActionFailed)
	h.clearWorkflowReviewCarry(userID)
	if ids := h.peekCodingExecRetrySteps(userID); len(ids) != 1 || ids[0] != "T5" {
		t.Fatalf("review cleared ids %v", ids)
	}
	if _, ok := h.pendingCodingExecRetryAction.Load(userID); ok {
		t.Fatal("review left the retry action")
	}
	if ids := h.takeDirectedRetryForCommand(userID, "你好"); len(ids) != 0 {
		t.Fatalf("stole %v", ids)
	}
	if ids := h.peekCodingExecRetrySteps(userID); len(ids) != 1 || ids[0] != "T5" {
		t.Fatalf("hello consumed %v", ids)
	}
	if ids := h.takeDirectedRetryForCommand(userID, ""); len(ids) != 0 {
		t.Fatalf("empty stole %v", ids)
	}
	ids := h.takeDirectedRetryForCommand(userID, "重试失败")
	if len(ids) != 1 || ids[0] != "T5" {
		t.Fatalf("retry %v", ids)
	}
	if left := h.peekCodingExecRetrySteps(userID); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

func TestInboundMCPReviewHandoffStaysOffRetry(t *testing.T) {
	const childSummary = "Read-only child tasks completed; start a fresh parent attempt to review their bounded results."
	empty := codingStepOutcomeFromResult(v2.TaskRunResult{
		TaskIndex: 5, Status: v2.TaskSkipped, RuntimeHandoff: true, Summary: childSummary,
	})
	if empty.Status != mcpStepWaiting || empty.Retryable || empty.Error != mcpReviewHandoffErrorText || empty.ID != "T5" {
		t.Fatalf("empty handoff %+v", empty)
	}
	failed := codingStepOutcomeFromResult(v2.TaskRunResult{
		TaskIndex: 5, Status: v2.TaskFailed, RuntimeHandoff: true,
	})
	if failed.Status != mcpStepWaiting || failed.Retryable || failed.Error != mcpReviewHandoffErrorText {
		t.Fatalf("failed handoff %+v", failed)
	}
	coded := codingStepOutcomeFromResult(v2.TaskRunResult{
		TaskIndex: 5, Status: v2.TaskSkipped, Error: "runtime_child_review_required",
	})
	if coded.Status != mcpStepWaiting || coded.Retryable || coded.Error != "runtime_child_review_required" {
		t.Fatalf("review code %+v", coded)
	}

	steps := mcpStepsFromAgent(&IMAgentResponse{CodingSteps: []CodingStepOutcome{
		{ID: "T4", Status: mcpStepPassed},
		empty,
	}})
	if !mcpStepsBlockCompletion(steps) {
		t.Fatal("handoff completed the task")
	}
	held := mcpMergeSteps(steps, nil)
	if len(held) != 2 || held[1].Status != mcpStepWaiting || held[1].Retryable || held[1].Error != mcpReviewHandoffErrorText {
		t.Fatalf("reopened %+v", held)
	}
	if err := mcpRetryRejectedBySteps(held, []string{"T5"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("retry: %v", err)
	}

	tasks := []*v2.TaskItem{{Index: 4, Title: "已完成"}, {Index: 5, Title: "实现"}}
	if _, err := codingExecSelectRetrySteps(tasks, []v2.TaskRunResult{
		{TaskIndex: 4, Status: v2.TaskPassed},
		{TaskIndex: 5, Status: v2.TaskSkipped, RuntimeHandoff: true},
	}, []string{"T5"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("exec empty: %v", err)
	}
	if _, err := codingExecSelectRetrySteps(tasks, []v2.TaskRunResult{
		{TaskIndex: 4, Status: v2.TaskPassed},
		{TaskIndex: 5, Status: v2.TaskSkipped, Error: "runtime_child_review_in_progress"},
	}, []string{"T5"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("exec code: %v", err)
	}

	mem := stickyCodingWorkbenchMemory{StepStatuses: []codingWorkbenchStepStatus{
		{Index: 4, Title: "已完成", Status: codingStepPassed},
		{Index: 5, Title: "实现", Status: codingStepSkipped, Summary: mcpReviewHandoffErrorText},
	}}
	if _, err := codingPlanSelectNamedSteps(mem, []string{"T5"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("plan: %v", err)
	}

	status, stepStatus, stepSummary, resultErr := codingWorkbenchStepOutcome(&CodingSubAgentResult{
		Status:  TaskExecWaitingChild,
		Summary: childSummary,
	})
	if status != v2.TaskSkipped || stepStatus != mcpStepWaiting || stepSummary != mcpReviewHandoffErrorText || !mcpSkipIsReviewHandoff(resultErr) {
		t.Fatalf("workbench %s %s %q %q", status, stepStatus, stepSummary, resultErr)
	}

	remote := remoteCodingStepToRunResult(&v2.TaskItem{Index: 5, Title: "实现"}, &RemoteCodingSubAgentResult{
		Status: "waiting_child", Summary: "children",
	})
	if remote.Status != v2.TaskSkipped || !remote.RuntimeHandoff || !mcpSkipIsReviewHandoff(remote.Error) {
		t.Fatalf("remote %+v", remote)
	}

	isolated, retryable := mcpClassifyRunStatus(mcpStepSkipped, "skipped: remote isolation unavailable")
	if isolated != mcpStepUnfinished || !retryable {
		t.Fatalf("isolation %s retryable=%v", isolated, retryable)
	}
}

func TestInboundMCPRemoteHandoffKeepsTheChildAndTheDependentSkip(t *testing.T) {
	result := &RemoteCodingSubAgentResult{
		Status:   "waiting_child",
		Summary:  "children",
		Children: []v2.ChildTaskView{{TaskID: "child-9", Name: "explorer"}},
	}
	stepStatus, summary, childID := remoteCodingStickyStep(result)
	if stepStatus != mcpStepWaiting || summary != mcpReviewHandoffErrorText || childID != "child-9" {
		t.Fatalf("sticky %s %q %s", stepStatus, summary, childID)
	}
	passed, passedSummary, passedChild := remoteCodingStickyStep(&RemoteCodingSubAgentResult{Status: "success", Summary: "done"})
	if passed != codingStepPassed || passedSummary != "done" || passedChild != "" {
		t.Fatalf("passed %s %q %s", passed, passedSummary, passedChild)
	}

	reported := []v2.TaskRunResult{
		remoteCodingStepToRunResult(&v2.TaskItem{Index: 5, Title: "交接"}, result),
		{TaskIndex: 6, Title: "后续", Status: v2.TaskSkipped, Error: "skipped: prior step failed"},
	}
	status, errText, steps := mcpApplyStepGate(mcpStatusCompleted, "", mcpStepsFromAgent(&IMAgentResponse{CodingSteps: codingStepsFromResults(reported)}))
	if status != mcpStatusWaitingReview || errText != mcpReviewHandoffErrorText || len(steps) != 2 {
		t.Fatalf("gate %s %q %+v", status, errText, steps)
	}
	if steps[0].Status != mcpStepWaiting || steps[0].ChildTaskID != "child-9" || len(steps[0].BlockedBy) != 1 || steps[0].BlockedBy[0] != "child-9" {
		t.Fatalf("handoff step %+v", steps[0])
	}
	if steps[1].Status != mcpStepSkipped || steps[1].Retryable || steps[1].BlockReason != "等待子任务审阅" || len(steps[1].BlockedBy) != 1 || steps[1].BlockedBy[0] != "T5" {
		t.Fatalf("dependent %+v", steps[1])
	}

	snap := codingWorkbenchStepsSnapshot([]codingWorkbenchStepStatus{
		{Index: 5, Title: "交接", Status: stepStatus, Summary: summary, ChildTaskID: childID},
		{Index: 6, Title: "后续", Status: codingStepSkipped, Summary: "skipped: prior step failed"},
	})
	published, stop := mcpExecutionFromWorkbench(snap)
	if published != mcpStatusWaitingReview || stop == nil || stop.Class != mcpStopReview || len(snap) != 2 {
		t.Fatalf("workbench %s %+v %+v", published, stop, snap)
	}
	if snap[0].Status != mcpStepWaiting || snap[0].ChildTaskID != "child-9" || len(snap[0].BlockedBy) != 1 || snap[0].BlockedBy[0] != "child-9" {
		t.Fatalf("workbench handoff %+v", snap[0])
	}
	if snap[1].Status != codingStepSkipped || snap[1].BlockReason != "等待子任务审阅" || len(snap[1].BlockedBy) != 1 || snap[1].BlockedBy[0] != "T5" {
		t.Fatalf("workbench dependent %+v", snap[1])
	}
}

func TestInboundMCPContinueKeepsTheReviewHandoff(t *testing.T) {
	held := stickyCodingWorkbenchMemory{StepStatuses: []codingWorkbenchStepStatus{
		{Index: 5, Title: "交接", Status: mcpStepWaiting, Summary: mcpReviewHandoffErrorText, ChildTaskID: "child-9"},
		{Index: 6, Title: "后续", Status: codingStepSkipped, Summary: "skipped: prior step failed"},
	}}
	tasks, ok := selectIncompleteCodingPlanTasks("继续", held)
	if !ok || len(tasks) != 0 {
		t.Fatalf("review hold tasks=%#v ok=%v", tasks, ok)
	}
	downgraded := held
	downgraded.StepStatuses = append([]codingWorkbenchStepStatus(nil), held.StepStatuses...)
	downgraded.StepStatuses[0].Status = codingStepSkipped
	tasks, ok = selectIncompleteCodingPlanTasks("继续", downgraded)
	if !ok || len(tasks) != 0 {
		t.Fatalf("downgraded handoff tasks=%#v ok=%v", tasks, ok)
	}
	failed := stickyCodingWorkbenchMemory{StepStatuses: []codingWorkbenchStepStatus{
		{Index: 4, Title: "编写", Status: codingStepFailed, Summary: "disk full"},
		{Index: 5, Title: "交接", Status: mcpStepWaiting, Summary: mcpReviewHandoffErrorText, ChildTaskID: "child-9"},
		{Index: 6, Title: "后续", Status: codingStepSkipped, Summary: "skipped: prior step failed"},
	}}
	tasks, ok = selectIncompleteCodingPlanTasks("继续", failed)
	if !ok || len(tasks) != 1 || tasks[0].Index != 4 {
		t.Fatalf("failure beside review tasks=%#v ok=%v", tasks, ok)
	}

	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.updateStickyCodingWorkbenchMemory(userID, func(stored *stickyCodingWorkbenchMemory) {
		*stored = held
	})
	resumed, _, planned := h.resolveCodingWorkbenchTasksWithDecision(
		userID, "继续", "D:/repo", held,
		codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: false},
		nil, nil,
	)
	if planned != true || len(resumed) != 0 {
		t.Fatalf("resolve planned=%v tasks=%#v", planned, resumed)
	}
	got := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
	if len(got) != 2 || got[0].Status != mcpStepWaiting || got[0].ChildTaskID != "child-9" || got[0].Summary != mcpReviewHandoffErrorText {
		t.Fatalf("checklist reopened %+v", got)
	}
	if !codingRunnerSkipNeedsStickyMirror(v2.TaskRunResult{Status: v2.TaskSkipped, Error: "skipped: prior step failed"}) {
		t.Fatal("a runner skip must still reach the checklist")
	}
	if codingRunnerSkipNeedsStickyMirror(v2.TaskRunResult{Status: v2.TaskSkipped, RuntimeHandoff: true, Error: mcpReviewHandoffErrorText}) {
		t.Fatal("a handoff must stay waiting_review on the checklist")
	}

	known := []v2.ChildTaskView{{TaskID: "child-9", Status: mcpStatusCompleted, Result: "found the leak", Review: "found the leak"}}
	status, _, errText, _, steps := mcpRunOutcomeCause(nil, nil, &IMAgentResponse{
		Text:        mcpReviewHandoffErrorText,
		CodingSteps: codingReviewHoldSteps(held.StepStatuses, known),
	})
	if status != mcpStatusWaitingReview || errText != mcpReviewHandoffErrorText || len(steps) != 2 {
		t.Fatalf("hold %s %q %+v", status, errText, steps)
	}
	if steps[0].Status != mcpStepWaiting || steps[0].ChildTaskID != "child-9" || len(steps[0].Children) != 1 || steps[0].Children[0].Result != "found the leak" {
		t.Fatalf("hold child %+v", steps[0])
	}
	if steps[1].Status != mcpStepSkipped || steps[1].Retryable || steps[1].BlockReason != "等待子任务审阅" || len(steps[1].BlockedBy) != 1 || steps[1].BlockedBy[0] != "T5" {
		t.Fatalf("hold dependent %+v", steps[1])
	}
}

func TestInboundMCPContinuePassesAnotherStepButKeepsTheReview(t *testing.T) {
	sticky := []codingWorkbenchStepStatus{
		{Index: 1, Title: "已完成", Status: codingStepPassed, Summary: "done"},
		{Index: 4, Title: "编写", Status: codingStepPassed, Summary: "wrote it"},
		{Index: 5, Title: "交接", Status: mcpStepWaiting, Summary: mcpReviewHandoffErrorText, ChildTaskID: "child-9"},
		{Index: 6, Title: "后续", Status: codingStepSkipped, Summary: "skipped: prior step failed"},
	}
	executed := []v2.TaskRunResult{{
		TaskIndex: 4,
		Title:     "编写",
		Status:    v2.TaskPassed,
		Summary:   "wrote it",
	}}
	known := []v2.ChildTaskView{{TaskID: "child-9", Status: mcpStatusCompleted, Result: "found the leak", Review: "found the leak"}}
	kept := codingResumeKeptOutcomes(executed, sticky, known)
	status, _, errText, _, steps := mcpRunOutcomeCause(nil, nil, &IMAgentResponse{
		Text:        "编写已通过",
		CodingSteps: kept,
	})
	if status != mcpStatusWaitingReview || errText != mcpReviewHandoffErrorText {
		t.Fatalf("status %s %q", status, errText)
	}
	if len(steps) != 3 {
		t.Fatalf("steps %+v", steps)
	}
	if steps[0].ID != "T4" || steps[0].Status != mcpStepPassed {
		t.Fatalf("rerun %+v", steps[0])
	}
	if steps[1].ID != "T5" || steps[1].Status != mcpStepWaiting || steps[1].ChildTaskID != "child-9" || len(steps[1].Children) != 1 || steps[1].Children[0].Result != "found the leak" {
		t.Fatalf("handoff %+v", steps[1])
	}
	if steps[2].ID != "T6" || steps[2].Status != mcpStepSkipped || steps[2].Retryable || steps[2].BlockReason != "等待子任务审阅" {
		t.Fatalf("dependent %+v", steps[2])
	}
	for _, step := range steps {
		if step.ID == "T1" {
			t.Fatalf("passed history copied onto this round: %+v", steps)
		}
	}

	failedRun := []v2.TaskRunResult{{
		TaskIndex: 4,
		Title:     "编写",
		Status:    v2.TaskFailed,
		Error:     "disk full",
	}}
	failedKept := codingResumeKeptOutcomes(failedRun, []codingWorkbenchStepStatus{
		{Index: 4, Title: "编写", Status: codingStepFailed, Summary: "disk full"},
		{Index: 5, Title: "交接", Status: mcpStepWaiting, Summary: mcpReviewHandoffErrorText, ChildTaskID: "child-9"},
	}, known)
	failedStatus, _, failedErr, _, failedSteps := mcpRunOutcomeCause(nil, nil, &IMAgentResponse{
		Text:        "编写失败",
		CodingSteps: failedKept,
	})
	if failedStatus != mcpStatusFailed || failedErr != "disk full" {
		t.Fatalf("failed status %s %q", failedStatus, failedErr)
	}
	sawHandoff := false
	for _, step := range failedSteps {
		if step.ID == "T5" && step.Status == mcpStepWaiting && step.ChildTaskID == "child-9" {
			sawHandoff = true
		}
	}
	if !sawHandoff {
		t.Fatalf("failure hid the review %+v", failedSteps)
	}
}

func TestInboundMCPKnownChildrenKeepTheReviewResult(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusWaitingReview,
		RequestID:   "mcp-old",
		RunID:       "run-old",
		Children: []mcpChildView{{
			ChildTaskID:   "child-9",
			Status:        mcpStatusCompleted,
			Result:        "found the leak",
			Review:        "found the leak",
			ReviewPending: true,
		}},
	})
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-new",
		RunID:       "run-new",
	})
	known := app.mcpKnownChildren("p")
	if len(known) != 1 || known[0].TaskID != "child-9" || known[0].Result != "found the leak" {
		t.Fatalf("empty live %+v", known)
	}

	app.mcpRunsMu.Lock()
	app.mcpRuns["p"].Children = []mcpChildView{{
		ChildTaskID: "child-new",
		Status:      "admitted",
		Name:        "explorer",
	}}
	app.mcpRunsMu.Unlock()
	known = app.mcpKnownChildren("p")
	kept := codingResumeKeptOutcomes(
		[]v2.TaskRunResult{{TaskIndex: 4, Title: "编写", Status: v2.TaskPassed, Summary: "wrote it"}},
		[]codingWorkbenchStepStatus{
			{Index: 4, Title: "编写", Status: codingStepPassed, Summary: "wrote it"},
			{Index: 5, Title: "交接", Status: mcpStepWaiting, Summary: mcpReviewHandoffErrorText, ChildTaskID: "child-9"},
		},
		known,
	)
	status, _, errText, _, steps := mcpRunOutcomeCause(nil, nil, &IMAgentResponse{
		Text:        "编写已通过",
		CodingSteps: kept,
	})
	if status != mcpStatusWaitingReview || errText != mcpReviewHandoffErrorText || len(steps) != 2 {
		t.Fatalf("kept %s %q %+v", status, errText, steps)
	}
	if steps[1].ID != "T5" || steps[1].ChildTaskID != "child-9" || len(steps[1].Children) != 1 || steps[1].Children[0].Result != "found the leak" {
		t.Fatalf("handoff result %+v", steps[1])
	}

	app.mcpRunsMu.Lock()
	app.mcpRuns["p"].Children = []mcpChildView{{
		ChildTaskID:   "child-9",
		Status:        "admitted",
		Review:        mcpReviewHandoffErrorText,
		ReviewPending: true,
	}}
	app.mcpRunsMu.Unlock()
	known = app.mcpKnownChildren("p")
	if len(known) != 1 || known[0].TaskID != "child-9" || known[0].Result != "found the leak" {
		t.Fatalf("admission hid the result %+v", known)
	}
}

func TestInboundMCPHandoffHoldsDependentSkip(t *testing.T) {
	prev := []mcpStepView{
		{ID: "T5", Status: mcpStepSkipped, Error: mcpReviewHandoffErrorText},
		{ID: "T6", Status: mcpStepSkipped, Error: "dependency not met"},
	}
	held := mcpMergeSteps(prev, nil)
	if len(held) != 2 || held[1].ID != "T6" || held[1].Status != mcpStepSkipped || held[1].Retryable || held[1].Error != "dependency not met" {
		t.Fatalf("reopened %+v", held)
	}
	if err := mcpRetryRejectedBySteps(held, []string{"T6"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("retry dependent: %v", err)
	}
	if err := mcpRetryRejectedBySteps(held, []string{"T5"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("retry handoff: %v", err)
	}
	waiting := mcpMergeSteps([]mcpStepView{
		{ID: "T5", Status: mcpStepSkipped, Error: "waiting_child"},
		{ID: "T6", Status: mcpStepSkipped, Error: "skipped: prior step failed"},
	}, nil)
	if len(waiting) != 2 || waiting[1].Status != mcpStepSkipped || waiting[1].Retryable {
		t.Fatalf("waiting child reopened %+v", waiting)
	}

	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-handoff-dep",
		Steps:       prev,
	})
	app.mcpFinishRun("p", "mcp-handoff-dep", mcpStatusCompleted, "子任务好了", "", false, "", nil, nil)
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusWaitingReview || view.Error != mcpReviewHandoffErrorText || len(view.Steps) != 2 {
		t.Fatalf("finish %+v ok=%v", view, ok)
	}
	if view.Steps[1].Status != mcpStepSkipped || view.Steps[1].Retryable || view.Steps[1].Error != "dependency not met" {
		t.Fatalf("stored %+v", view.Steps[1])
	}

	ready := mcpMergeSteps([]mcpStepView{
		{ID: "T5", Status: mcpStepPassed},
		{ID: "T6", Status: mcpStepSkipped, Error: "dependency not met"},
	}, nil)
	if len(ready) != 2 || ready[1].Status != mcpStepUnfinished || !ready[1].Retryable || ready[1].Error != mcpPriorSkipReadyError {
		t.Fatalf("ready %+v", ready)
	}

	tasks := []*v2.TaskItem{{Index: 5, Title: "交接"}, {Index: 6, Title: "后续"}}
	if _, err := codingExecSelectRetrySteps(tasks, []v2.TaskRunResult{
		{TaskIndex: 5, Status: v2.TaskSkipped, RuntimeHandoff: true},
		{TaskIndex: 6, Status: v2.TaskSkipped, Error: "dependency not met"},
	}, []string{"T6"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("exec: %v", err)
	}
	mem := stickyCodingWorkbenchMemory{StepStatuses: []codingWorkbenchStepStatus{
		{Index: 5, Title: "交接", Status: codingStepSkipped, Summary: mcpReviewHandoffErrorText},
		{Index: 6, Title: "后续", Status: codingStepSkipped, Summary: "skipped: prior step failed"},
	}}
	if _, err := codingPlanSelectNamedSteps(mem, []string{"T6"}); err == nil || !strings.Contains(err.Error(), "不能单独重试") {
		t.Fatalf("plan: %v", err)
	}
}

func TestInboundMCPFollowUpDoesNotReplaceRunning(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-old", Round: 2})
	_, err := app.mcpStartPrompt(context.Background(), mcpTaskView{ProjectPath: "p", Name: "n", Mode: "chat"}, "继续", false, false, nil)
	if err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("start: %v", err)
	}
	_, err = app.mcpConfirmRecovery(context.Background(), mcpTaskView{ProjectPath: "p", Name: "n", Mode: "chat"}, "__coding_recovery_decline__ t d", "t", "d", false)
	if err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("recovery: %v", err)
	}
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.RequestID != "mcp-old" || view.Status != mcpStatusRunning || view.Round != 2 {
		t.Fatalf("run changed: %+v ok=%v", view, ok)
	}
	if _, err := app.mcpAcquireTurn("p", false); err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("acquire running: %v", err)
	}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "q", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-wait"})
	first, err := app.mcpAcquireTurn("q", false)
	if err != nil || first == 0 {
		t.Fatal(err)
	}
	if _, err := app.mcpAcquireTurn("q", false); err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("second acquire: %v", err)
	}
	app.mcpReleaseTurn("q", first)
	second, err := app.mcpAcquireTurn("q", false)
	if err != nil || second == 0 {
		t.Fatal(err)
	}
	app.mcpReleaseTurn("q", second)
	kept, ok := app.mcpViewFromRegistry("q")
	if !ok || kept.RequestID != "mcp-wait" || kept.Status != mcpStatusNeedsConfirmation {
		t.Fatalf("waiting run changed: %+v ok=%v", kept, ok)
	}
	// A finished run still rejects a follow-up while its install claim is held,
	// which is the window wait=true used to keep open until the handler returned.
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "done", Status: mcpStatusCompleted, RequestID: "mcp-done"})
	held, err := app.mcpAcquireTurn("done", false)
	if err != nil || held == 0 {
		t.Fatal(err)
	}
	if _, err := app.mcpAcquireTurn("done", false); err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("held finished run: %v", err)
	}
	app.mcpReleaseTurn("done", held)
	next, err := app.mcpAcquireTurn("done", false)
	if err != nil || next == 0 {
		t.Fatalf("released finished run: %v", err)
	}
	app.mcpReleaseTurn("done", next)
}

func TestInboundMCPStopDropsUncommittedInstall(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-wait", Round: 1})
	token, err := app.mcpAcquireTurn("p", false)
	if err != nil || token == 0 {
		t.Fatal(err)
	}
	app.mcpCancelRun("p")
	_, _, err = app.mcpCommitRun(&mcpRunRecord{
		ProjectPath: "p",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-next",
		Round:       2,
	}, token, false)
	if err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("commit after stop: %v", err)
	}
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.RequestID != "mcp-wait" || view.Status != mcpStatusCancelled || view.Round != 1 {
		t.Fatalf("stop was overwritten: %+v ok=%v", view, ok)
	}
	// The claim does not stay taken. The next command can install.
	next, err := app.mcpAcquireTurn("p", false)
	if err != nil || next == 0 {
		t.Fatalf("acquire after stop: %v", err)
	}
	app.mcpReleaseTurn("p", next)

	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "c", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-c"})
	createToken, err := app.mcpAcquireTurn("c", true)
	if err != nil || createToken == 0 {
		t.Fatal(err)
	}
	app.mcpCancelRun("c")
	_, _, err = app.mcpCommitRun(&mcpRunRecord{
		ProjectPath: "c",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-c2",
	}, createToken, true)
	if err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("create commit after stop: %v", err)
	}
	view, ok = app.mcpViewFromRegistry("c")
	if !ok || view.RequestID != "mcp-c" || view.Status != mcpStatusCancelled {
		t.Fatalf("create overwrote stop: %+v ok=%v", view, ok)
	}

	// Nothing stored yet: stop still retires the claim the prompt already holds.
	fresh, err := app.mcpAcquireTurn("fresh", true)
	if err != nil || fresh == 0 {
		t.Fatal(err)
	}
	app.mcpCancelRun("fresh")
	_, _, err = app.mcpCommitRun(&mcpRunRecord{
		ProjectPath: "fresh",
		Status:      mcpStatusRunning,
		RequestID:   "mcp-fresh",
	}, fresh, true)
	if err == nil || !strings.Contains(err.Error(), "还在运行") {
		t.Fatalf("unstored commit after stop: %v", err)
	}
	if _, ok := app.mcpViewFromRegistry("fresh"); ok {
		t.Fatal("unstored stop left a run")
	}
}

func TestInboundMCPRunsStayOnTheirOwnSteps(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-a", RunID: "run-a", StatusVersion: 1,
		Steps: []mcpStepView{{ID: "T1", Status: mcpStepPassed, Title: "上一轮"}},
	})
	app.mcpFinishRun("p", "mcp-a", mcpStatusCompleted, "好了", "", false, "", nil, nil)
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-b", RunID: "run-b", StatusVersion: 1,
	})
	live, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil || live.RunID != "run-b" || len(live.Steps) != 0 || live.Status != mcpStatusRunning {
		t.Fatalf("live %+v err=%v", live, err)
	}
	old, err := app.mcpGetTask(context.Background(), "p", "run-a")
	if err != nil || len(old.Steps) != 1 || old.Steps[0].ID != "T1" || old.RunID != "run-a" {
		t.Fatalf("old %+v err=%v", old, err)
	}
	if live.StatusVersion == old.StatusVersion {
		t.Fatalf("versions live=%d old=%d", live.StatusVersion, old.StatusVersion)
	}
	if _, err := app.mcpGetTask(context.Background(), "p", "run-missing"); err == nil || !strings.Contains(err.Error(), "没有这一轮执行") {
		t.Fatalf("missing: %v", err)
	}
}

func TestInboundMCPReadLogFiltersByRun(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-a", RunID: "run-a"})
	app.mcpAppendLog("p", "mcp-a", mcpLogEvent{Kind: "progress", Text: "第一轮"})
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-b", RunID: "run-b"})
	app.mcpAppendLog("p", "mcp-b", mcpLogEvent{Kind: "progress", Text: "下一轮"})
	all, err := app.mcpReadLog(context.Background(), "p", "", 0)
	if err != nil || len(all.Events) != 2 || all.Events[1].Seq != all.Events[0].Seq+1 || all.Events[1].Text != "下一轮" {
		t.Fatalf("all %+v err=%v", all, err)
	}
	scoped, err := app.mcpReadLog(context.Background(), "p", "run-a", 0)
	if err != nil || len(scoped.Events) != 1 || scoped.Events[0].Text != "第一轮" || scoped.RunID != "run-a" {
		t.Fatalf("scoped %+v err=%v", scoped, err)
	}
	replaced, err := app.mcpGetTask(context.Background(), "p", "run-a")
	if err != nil || replaced.Status != mcpStatusCancelled || replaced.Stop == nil || replaced.Stop.Class != mcpStopUser || replaced.Stop.Reason == "" {
		t.Fatalf("replaced %+v err=%v", replaced, err)
	}
}

func TestInboundMCPCancelRecordsUserStop(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-1", RunID: "run-1",
		Steps: []mcpStepView{{ID: "T2", Status: mcpStepUnfinished}},
	})
	app.mcpCancelRun("p")
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.Status != mcpStatusCancelled || view.Stop == nil || view.Stop.Class != mcpStopUser || view.Stop.Reason == "" || view.Stop.At == "" || view.Stop.StepID != "T2" {
		t.Fatalf("view=%+v stop=%+v ok=%v", view, view.Stop, ok)
	}
}

func TestInboundMCPStopAndGateExplainTheBlock(t *testing.T) {
	status, errText, steps := mcpApplyStepGate(mcpStatusCompleted, "", []mcpStepView{{ID: "T1", Status: mcpStepFailed, Error: "boom", Retryable: true}})
	stop := mcpStopFor(status, errText, steps, nil, time.Unix(10, 0).UTC())
	if status != mcpStatusFailed || stop == nil || stop.Class != mcpStopProgram || stop.StepID != "T1" {
		t.Fatalf("program status=%s stop=%+v err=%s", status, stop, errText)
	}
	timeout := mcpStopFor(mcpStatusCancelled, "", nil, context.DeadlineExceeded, time.Unix(11, 0).UTC())
	if timeout == nil || timeout.Class != mcpStopTimeout || timeout.Reason != "执行超时" {
		t.Fatalf("timeout %+v", timeout)
	}
	depStatus, _, depSteps := mcpApplyStepGate(mcpStatusCompleted, "", []mcpStepView{{ID: "T3", Status: mcpStepSkipped, Error: "dependency not met"}})
	depStop := mcpStopFor(depStatus, "", depSteps, nil, time.Unix(12, 0).UTC())
	if depStatus == mcpStatusCompleted || depStop == nil || depStop.Class != mcpStopDependency || depSteps[0].BlockReason == "" {
		t.Fatalf("dep status=%s stop=%+v step=%+v", depStatus, depStop, depSteps)
	}
	waitStatus, waitErr, waitSteps := mcpApplyStepGate(mcpStatusCompleted, "", []mcpStepView{
		{ID: "T5", Status: mcpStepSkipped, Error: "waiting_child", ChildTaskID: "child-1"},
		{ID: "T6", Status: mcpStepSkipped, Error: "dependency not met"},
	})
	if waitStatus != mcpStatusWaitingReview || waitErr != mcpReviewHandoffErrorText || waitStatus == mcpStatusFailed {
		t.Fatalf("wait %s %s", waitStatus, waitErr)
	}
	if len(waitSteps) != 2 || waitSteps[0].Status != mcpStepWaiting || waitSteps[1].Status != mcpStepSkipped || waitSteps[1].BlockReason != "等待子任务审阅" || len(waitSteps[1].BlockedBy) != 1 || waitSteps[1].BlockedBy[0] != "T5" {
		t.Fatalf("steps %+v", waitSteps)
	}
}

func TestInboundMCPAdmittedChildIsNotComplete(t *testing.T) {
	if codingChildPublicStatus(codingruntime.TaskQueued) != "admitted" {
		t.Fatal("queued must stay admitted")
	}
	children := mcpChildrenFromToolText("child_task_id=child-9 parent_task_id=parent-1 name=explorer status=queued")
	if len(children) != 1 || children[0].Status != "admitted" || children[0].ChildTaskID != "child-9" || children[0].Status == mcpStatusCompleted {
		t.Fatalf("%+v", children)
	}
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-c", RunID: "run-c"})
	app.mcpNoteAdmittedChildren("p", "mcp-c", "child_task_id=child-9 parent_task_id=parent-1 name=explorer status=queued")
	view, ok := app.mcpViewFromRegistry("p")
	if !ok || view.Status == mcpStatusCompleted || view.ChildTaskID != "child-9" || len(view.Children) != 1 || view.Children[0].Status != "admitted" {
		t.Fatalf("ok=%v %+v", ok, view)
	}
}

func TestInboundMCPChildResultLandsOnTheWaitingRun(t *testing.T) {
	kept := mcpMergeChildren(
		[]mcpChildView{{
			ChildTaskID: "child-9", Name: "explorer", Status: mcpStatusCompleted,
			Progress: mcpStatusCompleted, Result: "found the leak", Review: "found the leak", ReviewPending: true,
		}},
		mcpChildrenFromSteps([]mcpStepView{{ID: "T5", Status: mcpStepWaiting, ChildTaskID: "child-9"}}),
	)
	if len(kept) != 1 || kept[0].Status != mcpStatusCompleted || kept[0].Result != "found the leak" || kept[0].Review != "found the leak" || kept[0].Name != "explorer" {
		t.Fatalf("admission row replaced the result %+v", kept)
	}
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-c", RunID: "run-c", StatusVersion: 1})
	app.mcpNoteAdmittedChildren("p", "mcp-c", "child_task_id=child-9 parent_task_id=parent-1 name=explorer status=queued")
	before, ok := app.mcpViewFromRegistry("p")
	if !ok || before.Status != mcpStatusRunning || before.ChildTaskID != "child-9" || before.Children[0].Status != "admitted" {
		t.Fatalf("admitted %+v", before)
	}
	app.mcpNoteChildOutcome(mcpChildView{
		ChildTaskID: "child-9", ParentTaskID: "parent-1", Name: "explorer",
		Status: mcpStatusCompleted, Progress: mcpStatusCompleted,
		Result: "found the leak", Review: "found the leak", ReviewPending: true,
	})
	landed, ok := app.mcpViewFromRegistry("p")
	if !ok || landed.Status != mcpStatusRunning || landed.StatusVersion <= before.StatusVersion || len(landed.Children) != 1 || landed.Children[0].Status != mcpStatusCompleted || landed.Children[0].Result != "found the leak" {
		t.Fatalf("landed %+v", landed)
	}
	app.mcpFinishRun("p", "mcp-c", mcpStatusCompleted, "子任务好了", "", false, "", nil, []mcpStepView{{
		ID: "T5", Status: mcpStepSkipped, Error: "waiting_child", ChildTaskID: "child-9",
	}})
	finished, ok := app.mcpViewFromRegistry("p")
	if !ok || finished.Status != mcpStatusWaitingReview || finished.Status == mcpStatusCompleted {
		t.Fatalf("parent %+v", finished)
	}
	published := app.mcpReturnTask(finished)
	if len(published.Children) != 1 || published.Children[0].Status != mcpStatusCompleted || published.Children[0].Result != "found the leak" || published.Children[0].Review != "found the leak" {
		t.Fatalf("published %+v", published.Children)
	}
	if len(published.Steps) != 1 || len(published.Steps[0].Children) != 1 || published.Steps[0].Children[0].Status != mcpStatusCompleted || published.Steps[0].Children[0].Result != "found the leak" || published.Steps[0].ChildTaskID != "child-9" {
		t.Fatalf("step child %+v", published.Steps)
	}
	path := normalizeProjectSessionPath("p")
	app.mcpRunsMu.Lock()
	app.mcpArchiveRunLocked(app.mcpRuns[path])
	app.mcpRuns[path] = &mcpRunRecord{ProjectPath: path, Status: mcpStatusRunning, RequestID: "mcp-d", RunID: "run-d", StatusVersion: 1}
	app.mcpRunsMu.Unlock()
	app.mcpNoteChildOutcome(mcpChildView{
		ChildTaskID: "child-9", ParentTaskID: "parent-1", Name: "explorer",
		Status: mcpStatusCompleted, Progress: mcpStatusCompleted,
		Result: "found the leak and the caller", Review: "found the leak and the caller", ReviewPending: true,
	})
	old, ok := app.mcpViewForRun("p", "run-c")
	if !ok || old.Status != mcpStatusWaitingReview || len(old.Children) != 1 || old.Children[0].Result != "found the leak and the caller" {
		t.Fatalf("archived %+v", old)
	}
	live, ok := app.mcpViewFromRegistry("p")
	if !ok || live.Status != mcpStatusRunning || live.RunID != "run-d" || len(live.Children) != 0 || live.StatusVersion != 1 {
		t.Fatalf("live %+v", live)
	}
	store := codingruntime.NewMemoryStore()
	now := time.Now().UTC()
	parent, err := store.CreateTask(codingruntime.Task{TaskID: "parent-1", ProjectRef: "repo", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	parentAttempt, err := store.StartAttempt(parent.TaskID, "parent", time.Minute, codingruntime.PolicySnapshot{ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatal(err)
	}
	policy := codingruntime.PolicySnapshot{ProjectRoot: "repo", Mode: "local", ReadOnly: true}
	service := codingruntime.ChildTaskService{Store: store, Now: func() time.Time { return now }}
	handle, err := service.AdmitReadOnlyChild(parentAttempt.AttemptID, "parent", codingruntime.ChildTaskSpec{
		Name: "explorer", RequestedWork: "inspect the leak", ProjectRef: "repo", Mode: "local",
	}, policy)
	if err != nil {
		t.Fatal(err)
	}
	runner := codingruntime.Runner{Store: store, LeaseOwner: "child", Now: func() time.Time { return now }}
	if _, _, _, err := service.RunReadOnlyChild(context.Background(), runner, handle.TaskID, policy, testReadOnlyChildExecutorFunc(func(context.Context, codingruntime.ExecutionRequest) codingruntime.ChildTaskResult {
		return codingruntime.ChildTaskResult{Status: codingruntime.TaskCompleted, Summary: "ledger summary", EvidenceDigest: "sha256:child"}
	})); err != nil {
		t.Fatal(err)
	}
	fromLedger := mcpChildViewFromRunOutcome(
		*handle,
		codingruntime.ChildRunOutcome{Attempt: &codingruntime.Attempt{Status: codingruntime.TaskCompleted, ErrorSummary: "attempt text"}},
		store,
	)
	if fromLedger.Status != mcpStatusCompleted || fromLedger.Result != "ledger summary" || fromLedger.Name != "explorer" || fromLedger.Review != "ledger summary" {
		t.Fatalf("ledger view %+v", fromLedger)
	}
}

func TestInboundMCPChildResultArrivesBeforeAdmission(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-c", RunID: "run-c", StatusVersion: 1})
	app.mcpNoteChildOutcome(mcpChildView{
		ChildTaskID: "child-9", ParentTaskID: "parent-1", Name: "explorer",
		Status: mcpStatusCompleted, Progress: mcpStatusCompleted,
		Result: "found the leak", Review: "found the leak", ReviewPending: true,
	})
	early, ok := app.mcpViewFromRegistry("p")
	if !ok || early.Status != mcpStatusRunning || len(early.Children) != 0 || early.StatusVersion != 1 {
		t.Fatalf("early %+v", early)
	}
	app.mcpNoteAdmittedChildren("p", "mcp-c", "child_task_id=child-9 parent_task_id=parent-1 name=explorer status=queued")
	landed, ok := app.mcpViewFromRegistry("p")
	if !ok || landed.Status != mcpStatusRunning || landed.StatusVersion == 1 || len(landed.Children) != 1 || landed.Children[0].Status != mcpStatusCompleted || landed.Children[0].Result != "found the leak" || landed.Children[0].Review != "found the leak" {
		t.Fatalf("landed %+v", landed)
	}
	version := landed.StatusVersion
	app.mcpNoteAdmittedChildren("p", "mcp-c", "child_task_id=child-9 parent_task_id=parent-1 name=explorer status=queued")
	again, ok := app.mcpViewFromRegistry("p")
	if !ok || again.StatusVersion != version || again.Children[0].Status != mcpStatusCompleted || again.Children[0].Result != "found the leak" {
		t.Fatalf("repeat %+v", again)
	}

	late := &App{}
	late.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-c", RunID: "run-c", StatusVersion: 1})
	late.mcpNoteChildOutcome(mcpChildView{
		ChildTaskID: "child-9", ParentTaskID: "parent-1", Name: "explorer",
		Status: mcpStatusCompleted, Progress: mcpStatusCompleted,
		Result: "found the leak", Review: "found the leak", ReviewPending: true,
	})
	late.mcpFinishRun("p", "mcp-c", mcpStatusCompleted, "子任务好了", "", false, "", nil, []mcpStepView{{
		ID: "T5", Status: mcpStepSkipped, Error: "waiting_child", ChildTaskID: "child-9",
	}})
	finished, ok := late.mcpViewFromRegistry("p")
	if !ok || finished.Status != mcpStatusWaitingReview || len(finished.Children) != 1 || finished.Children[0].Status != mcpStatusCompleted || finished.Children[0].Result != "found the leak" {
		t.Fatalf("finish %+v", finished)
	}
	published := late.mcpReturnTask(finished)
	if len(published.Children) != 1 || published.Children[0].Status != mcpStatusCompleted || published.Children[0].Review != "found the leak" {
		t.Fatalf("published %+v", published.Children)
	}
	if len(published.Steps) != 1 || len(published.Steps[0].Children) != 1 || published.Steps[0].Children[0].Result != "found the leak" || published.Steps[0].Children[0].Status != published.Children[0].Status {
		t.Fatalf("step child %+v", published.Steps)
	}

	failed := mcpChildViewFromRunOutcome(
		codingruntime.ChildTaskHandle{TaskID: "child-9", ParentTaskID: "parent-1", Name: "explorer"},
		codingruntime.ChildRunOutcome{Err: errors.New("disk full"), Attempt: &codingruntime.Attempt{Status: codingruntime.TaskFailed}},
		nil,
	)
	if failed.Status != mcpStatusFailed || failed.Result != "disk full" || failed.Review == mcpReviewHandoffErrorText || strings.TrimSpace(failed.Review) == "" {
		t.Fatalf("failed child %+v", failed)
	}
}

func TestInboundMCPSpawnNamesTheChild(t *testing.T) {
	text := formatAdmittedReadOnlyChildHandles([]codingruntime.ChildTaskHandle{{
		TaskID: "child-1", ParentTaskID: "parent-1", Name: "explorer", Status: codingruntime.TaskQueued,
	}})
	if !strings.Contains(text, "child_task_id=child-1") || !strings.Contains(text, "status=admitted") || strings.Contains(text, "completed") {
		t.Fatalf("%s", text)
	}
	if !strings.Contains(text, "spawn_coding_agent admitted read-only child task(s); parent attempt released its lease:") || !strings.Contains(text, "fresh parent attempt") {
		t.Fatalf("%s", text)
	}
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
