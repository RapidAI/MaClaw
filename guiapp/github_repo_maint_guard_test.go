package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestGitHubRepoMaintPathJail(t *testing.T) {
	outside := t.TempDir()
	ghost := filepath.Join(outside, "repo")
	if err := os.MkdirAll(ghost, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	const marker = "MAINT-SECRET-MARKER"
	if err := os.WriteFile(secret, []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(ghost, "inside.txt")
	if err := os.WriteFile(inside, []byte("MAINT-INSIDE-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := newRepoMaintTestApp(t, "https://203.0.113.10", false)
	t.Cleanup(func() {
		closeRepoMaintDB(filepath.Join(app.GetDataDir(), githubRepoMaintDBName))
	})
	owner := "desktop-user:" + ghost
	repoMaintActiveUsers.Store(owner, ghost)
	t.Cleanup(func() { repoMaintActiveUsers.Delete(owner) })
	h := &IMMessageHandler{app: app}

	rel := filepath.Join("..", "secret.txt")
	if _, err := h.resolveFileToolPathForOwner(rel, owner); err == nil || !strings.Contains(err.Error(), "path outside the ghost directory") {
		t.Fatalf("relative read err = %v", err)
	}
	read := h.toolReadFile(map[string]interface{}{
		"path":                           rel,
		registeredToolPolicyOwnerIDField: owner,
	})
	if strings.Contains(read, marker) || !strings.Contains(read, "path outside the ghost directory") {
		t.Fatalf("read_file escaped: %s", read)
	}
	okRead := h.toolReadFile(map[string]interface{}{
		"path":                           "inside.txt",
		registeredToolPolicyOwnerIDField: owner,
	})
	if !strings.Contains(okRead, "MAINT-INSIDE-MARKER") {
		t.Fatalf("inside read = %s", okRead)
	}

	parent := filepath.Clean(filepath.Dir(ghost))
	if cwd, active := repoMaintToolDir(h, owner, ".."); !active || filepath.Clean(cwd) == parent || (cwd != "" && !repoMaintPathInside(ghost, cwd)) {
		t.Fatalf("shell cwd = %q active=%v parent=%s", cwd, active, parent)
	}
	if got := filepath.Clean(h.resolveToolWorkDirForOwner("..", owner)); got == parent {
		t.Fatalf("resolveToolWorkDir escaped to %s", got)
	}
	bash := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        "Write-Output " + marker,
		"working_dir":                    "..",
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(bash, marker) || !strings.Contains(bash, "path outside the ghost directory") {
		t.Fatalf("bash cwd escaped: %s", bash)
	}
	absBash := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        "Get-Content " + secret,
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(absBash, marker) || !strings.Contains(absBash, "path outside the ghost directory") {
		t.Fatalf("bash absolute arg escaped: %s", absBash)
	}
	if _, err := h.executeTrustedShell(owner, "Get-Content "+secret, time.Second); err == nil || !strings.Contains(err.Error(), "path outside the ghost directory") {
		t.Fatalf("trusted shell err = %v", err)
	}
	quoted := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        `Get-Content "` + secret + `"`,
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(quoted, marker) || !strings.Contains(quoted, "path outside the ghost directory") {
		t.Fatalf("quoted absolute arg escaped: %s", quoted)
	}
	if !repoMaintPathInside(ghost, "inside.txt") {
		t.Fatal("inside relative path rejected")
	}
	if cands := repoMaintCommandAbsPaths(`cmd /c echo hi`); len(cands) != 0 {
		t.Fatalf("command switch looked like a path: %#v", cands)
	}
	if cands := repoMaintCommandAbsPaths(`git clone https://github.com/owner/repo.git`); len(cands) != 0 {
		t.Fatalf("https url looked like a path: %#v", cands)
	}
	if err := repoMaintRejectShell(h, owner, ghost, `git clone https://github.com/owner/repo.git`); err != nil {
		t.Fatalf("https url rejected: %v", err)
	}
	if err := repoMaintRejectShell(h, owner, ghost, "git clone https://github.com/owner/repo.git "+secret); err == nil || !strings.Contains(err.Error(), "path outside the ghost directory") {
		t.Fatalf("url plus outside path err = %v", err)
	}
	rooted := secret[len(filepath.VolumeName(secret)):]
	if repoMaintPathInside(ghost, rooted) {
		t.Fatal("drive-root path treated as inside the ghost")
	}
	rootRead := h.toolReadFile(map[string]interface{}{
		"path":                           rooted,
		registeredToolPolicyOwnerIDField: owner,
	})
	if strings.Contains(rootRead, marker) || !strings.Contains(rootRead, "path outside the ghost directory") {
		t.Fatalf("drive-root read escaped: %s", rootRead)
	}
	rootBash := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        "Get-Content " + rooted,
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(rootBash, marker) || !strings.Contains(rootBash, "path outside the ghost directory") {
		t.Fatalf("drive-root arg escaped: %s", rootBash)
	}
	nested := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        `powershell -NoProfile -Command "Get-Content '` + secret + `'"`,
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(nested, marker) || !strings.Contains(nested, "path outside the ghost directory") {
		t.Fatalf("quoted command body escaped: %s", nested)
	}

	link := filepath.Join(ghost, "out")
	if err := os.Symlink(outside, link); err != nil {
		out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if jerr != nil {
			t.Fatalf("symlink %v; junction %v %s", err, jerr, out)
		}
	}
	linked := h.toolReadFile(map[string]interface{}{
		"path":                           filepath.Join("out", "secret.txt"),
		registeredToolPolicyOwnerIDField: owner,
	})
	if strings.Contains(linked, marker) || !strings.Contains(linked, "path outside the ghost directory") {
		t.Fatalf("symlink read escaped: %s", linked)
	}

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		projectPath:       ghost,
		permissionOwnerID: owner,
		handler:           h,
	}}
	assertCodingMaintToolStaysInside(t, cb, secret, marker)
	linkedCoding := cb.executeToolWithOutcome("read_file", mustRepoMaintToolJSON(t, map[string]string{
		"path": filepath.Join("out", "secret.txt"),
	}))
	if strings.Contains(linkedCoding.Text, marker) || !strings.Contains(linkedCoding.Text, "path outside the ghost directory") {
		t.Fatalf("coding junction read escaped: %s", linkedCoding.Text)
	}

	// Production CodingSubAgent leaves permissionOwnerID empty and uses the loop user.
	viaLoop := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		projectPath: ghost,
		loopCtx:     &LoopContext{UserID: owner},
		handler:     h,
	}}
	assertCodingMaintToolStaysInside(t, viaLoop, secret, marker)
	body, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != marker {
		t.Fatalf("secret file changed: %s", body)
	}
}

func mustRepoMaintToolJSON(t *testing.T, args map[string]string) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertCodingMaintToolStaysInside(t *testing.T, cb *codingSubAgentCallbacks, secret, marker string) {
	t.Helper()
	rooted := secret
	if vol := filepath.VolumeName(secret); vol != "" && strings.HasPrefix(secret, vol) {
		rooted = secret[len(vol):]
	}
	calls := []struct {
		name string
		args map[string]string
	}{
		{name: "bash", args: map[string]string{"command": "Get-Content " + secret}},
		{name: "bash", args: map[string]string{"command": "Get-Content " + rooted}},
		{name: "bash", args: map[string]string{"command": `powershell -NoProfile -Command "Get-Content '` + secret + `'"`}},
		{name: "read_file", args: map[string]string{"path": secret}},
		{name: "write_file", args: map[string]string{"path": secret, "content": "PWNED"}},
		{name: "edit_file", args: map[string]string{"path": secret, "old_string": marker, "new_string": "PWNED"}},
		{name: "edit_lines", args: map[string]string{"path": secret, "operation": "replace", "start_line": "1", "end_line": "1", "content": "PWNED"}},
	}
	for _, call := range calls {
		raw, err := json.Marshal(call.args)
		if err != nil {
			t.Fatal(err)
		}
		// edit_lines numbers are JSON numbers, not strings.
		if call.name == "edit_lines" {
			raw, err = json.Marshal(map[string]any{
				"path": secret, "operation": "replace", "start_line": 1, "end_line": 1, "content": "PWNED",
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		got := cb.executeToolWithOutcome(call.name, string(raw))
		if strings.Contains(got.Text, marker) || !strings.Contains(got.Text, "path outside the ghost directory") {
			t.Fatalf("coding %s escaped: %s", call.name, got.Text)
		}
	}
}

func TestGitHubRepoMaintUserPlanGate(t *testing.T) {
	app := newRepoMaintTestApp(t, "https://203.0.113.10", false)
	app.disableBackgroundEmbeddingForTest = true
	t.Cleanup(func() {
		closeRepoMaintDB(filepath.Join(app.GetDataDir(), githubRepoMaintDBName))
	})
	project := t.TempDir()
	saved, err := app.SaveGitHubRepoMaintRule(repoMaintDraft(project))
	if err != nil {
		t.Fatal(err)
	}
	project = saved.GhostDir
	userID := projectSessionOwnerID(project)
	db, err := app.repoMaintDB()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO jobs (id, rule_id, delivery_id, event_kind, object_id, ghost_dir, status, task_project_path, session_user_id, last_error, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		"job-user-plan", saved.ID, "d-user-plan", "issues.opened", "issue:21", project, "queued", "", "", "", now, now); err != nil {
		t.Fatal(err)
	}
	job := repoMaintJobByObject(t, app, "issue:21")
	if err := app.setRepoMaintJobStatus(job, "running", project, userID, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repoMaintActiveUsers.Delete(userID) })

	handler := &IMMessageHandler{app: app}
	handler.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.PlanMode = codingPlanModeApprove
	})
	resp := handler.runCodingTemplateSubAgent(userID, "1. alpha step\n2. beta step", project, nil, nil, nil)
	if resp == nil || !repoMaintResponseHasPlanGate(resp.Actions) {
		text := ""
		if resp != nil {
			text = resp.Text
		}
		t.Fatalf("plan gate response missing: %s", text)
	}
	app.finishRepoMaintTurn(userID, false, resp)
	got := repoMaintJobByObject(t, app, "issue:21")
	if got.Status != "awaiting_user" || got.SessionUserID != userID {
		t.Fatalf("user plan gate status=%s user=%s err=%s", got.Status, got.SessionUserID, got.LastError)
	}
	if dir, ok := repoMaintActiveUsers.Load(userID); !ok || strings.TrimSpace(dir.(string)) == "" {
		t.Fatalf("directory lock released: %v", dir)
	}
	if mode := handler.getStickyCodingWorkbenchMemory(userID).PlanMode; mode != codingPlanModeApprove {
		t.Fatalf("plan mode = %s", mode)
	}
}

func TestGitHubRepoMaintAutoTurnLeavesUserPlan(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	goal := "先规划一下仓库里的登录页，确定后再开工"
	steps := []*v2.TaskItem{
		{Index: 1, Title: "实现登录表单", Description: "账号和密码"},
		{Index: 2, Title: "补上失败提示", Description: "错误时不跳转"},
	}
	h.storeStickyPendingCodingPlan(userID, goal, "plan", steps)
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.PlanDialogue = true
		m.PlanDialogueBrief = goal
		m.ExecutionPlan = "T1 实现登录表单\nT2 补上失败提示"
		m.RequirementRestatement = "登录页按确认过的方案做"
		m.ApprovedPlanValidators = []string{"go test ./login"}
		m.ApprovedPlanValidatorDir = "D:/repo"
		m.SkipNextPlan = true
		m.StepStatuses = []codingWorkbenchStepStatus{{Index: 1, Title: "实现登录表单", Status: codingStepPending}}
	})
	eventText := "需要有图形界面"
	maint := &LoopContext{Platform: githubRepoMaintPlatform}
	if codingTurnAllowsUserPlan(maint) || !codingTurnAllowsUserPlan(nil) || !codingTurnAllowsUserPlan(&LoopContext{Platform: desktopPlatform}) {
		t.Fatal("automatic maintenance must be the only turn that skips the user plan")
	}
	next, stop := h.prepareCodingPlanDialogue(userID, eventText, "D:/repo", codingTurnAllowsUserPlan(maint), nil, nil)
	if stop != nil || next != eventText {
		text := ""
		if stop != nil {
			text = stop.Text
		}
		t.Fatalf("maintenance event entered plan dialogue: %s next=%q", text, next)
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || pending.UserText != goal || strings.Contains(pending.Markdown, "图形界面") {
		t.Fatalf("pending plan changed: %+v ok=%v", pending, ok)
	}
	if !h.getStickyCodingWorkbenchMemory(userID).PlanDialogue {
		t.Fatal("maintenance event cleared the user's planning conversation")
	}
	directTasks, directDecision, direct := applyCodingRepoMaintDirectTurn(maint, eventText)
	if !direct || directDecision.NeedsPlan || len(directTasks) != 1 || directTasks[0].Description != eventText {
		t.Fatalf("direct turn = %v needsPlan=%v tasks=%d", direct, directDecision.NeedsPlan, len(directTasks))
	}
	if _, _, userDirect := applyCodingRepoMaintDirectTurn(nil, eventText); userDirect {
		t.Fatal("a user turn must still plan")
	}
	kept := h.getStickyCodingWorkbenchMemory(userID)
	if kept.ExecutionPlan != "T1 实现登录表单\nT2 补上失败提示" ||
		kept.RequirementRestatement != "登录页按确认过的方案做" ||
		len(kept.ApprovedPlanValidators) != 1 || kept.ApprovedPlanValidators[0] != "go test ./login" ||
		kept.ApprovedPlanValidatorDir != "D:/repo" ||
		!kept.SkipNextPlan ||
		len(kept.StepStatuses) != 1 || kept.StepStatuses[0].Title != "实现登录表单" || kept.StepStatuses[0].Status != codingStepPending {
		t.Fatalf("direct turn wrote session memory: %+v", kept)
	}
	if _, stop := h.prepareCodingPlanDialogue(userID, eventText, "D:/repo", codingTurnAllowsUserPlan(nil), nil, nil); stop == nil {
		t.Fatal("a user turn must still answer the open plan")
	}

	approvedUser := stickyTestUserID(t)
	h.storeStickyPendingCodingPlan(approvedUser, goal, "plan", steps)
	if _, ok := h.promotePendingToApprovedCodingPlan(approvedUser); !ok {
		t.Fatal("promote failed")
	}
	before := h.getStickyCodingWorkbenchMemory(approvedUser).ApprovedPlanJSON
	if strings.TrimSpace(before) == "" {
		t.Fatal("approved plan missing")
	}
	if codingTurnAllowsUserPlan(maint) {
		h.takeStickyApprovedCodingPlan(approvedUser)
	}
	if got := h.getStickyCodingWorkbenchMemory(approvedUser).ApprovedPlanJSON; got != before {
		t.Fatal("maintenance event consumed the approved plan")
	}
	if !codingTurnAllowsUserPlan(nil) {
		t.Fatal("a user turn must be allowed to take an approved plan")
	}
	if _, ok := h.takeStickyApprovedCodingPlan(approvedUser); !ok {
		t.Fatal("a user turn should take the approved plan")
	}
	if strings.TrimSpace(h.getStickyCodingWorkbenchMemory(approvedUser).ApprovedPlanJSON) != "" {
		t.Fatal("user turn left the approved plan")
	}
}

func TestGitHubRepoMaintDirectTurnKeepsUserContract(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	project := t.TempDir()
	const (
		userRequest = "USER_REQUEST_MARK"
		userPlan    = "USER_UNDERSTANDING_MARK"
		userSummary = "USER_SUMMARY_MARK"
		eventText   = "需要有图形界面"
	)
	steps := []*v2.TaskItem{
		{Index: 1, Title: "实现登录表单", Description: "账号和密码"},
		{Index: 2, Title: "补上失败提示", Description: "错误时不跳转"},
	}
	h.storeStickyPendingCodingPlan(userID, "先规划一下仓库里的登录页", "plan", steps)
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.ProjectPath = project
		m.LastUserText = userRequest
		m.SessionPlan = ""
		m.LastSummary = userSummary
		m.RequirementRestatement = userPlan
		m.ExecutionPlan = "T1 实现登录表单\nT2 补上失败提示"
		m.ApprovedPlanValidators = []string{"go test ./login"}
		m.ApprovedPlanValidatorDir = project
		m.SkipNextPlan = true
		m.PlanDialogue = true
		m.TurnCount = 1
		m.StepStatuses = []codingWorkbenchStepStatus{{Index: 1, Title: "实现登录表单", Status: codingStepPending}}
	})

	original := runTaskWithSubAgent
	t.Cleanup(func() { runTaskWithSubAgent = original })
	var gotDesc, gotReq, gotPrev string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		if task != nil {
			gotDesc = task.Description
		}
		gotReq = reqCtx
		gotPrev = strings.Join(prevOutputs, "\n")
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "maint-done", FilesModified: []string{"login.go"}}
	}

	loop := NewLoopContext("maint-direct", 4, nil)
	loop.Platform = githubRepoMaintPlatform
	resp := h.runCodingTemplateSubAgent(userID, eventText, project, loop, nil, nil)
	if resp == nil {
		t.Fatal("missing response")
	}
	if strings.Contains(resp.Text, "批准后验证") {
		t.Fatalf("maintenance turn ran the user's validators: %s", resp.Text)
	}
	if gotDesc != eventText || strings.TrimSpace(gotReq) != "" {
		t.Fatalf("executor contract desc=%q req=%q", gotDesc, gotReq)
	}
	for _, leaked := range []string{userRequest, userPlan, userSummary, codingCurrentRequestUnderstandingPrefix, codingSessionPlanPrefix, codingPreviousUserRequestPrefix, codingPreviousTurnResultPrefix} {
		if strings.Contains(gotPrev, leaked) {
			t.Fatalf("executor prompt still carries the user's ask %q in %s", leaked, gotPrev)
		}
	}
	kept := h.getStickyCodingWorkbenchMemory(userID)
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok || pending.UserText == "" {
		t.Fatal("pending plan was cleared")
	}
	if kept.LastUserText != userRequest || kept.SessionPlan != "" || kept.RequirementRestatement != userPlan ||
		kept.ExecutionPlan != "T1 实现登录表单\nT2 补上失败提示" ||
		len(kept.ApprovedPlanValidators) != 1 || kept.ApprovedPlanValidators[0] != "go test ./login" ||
		!kept.SkipNextPlan || !kept.PlanDialogue ||
		len(kept.StepStatuses) != 1 || kept.StepStatuses[0].Title != "实现登录表单" || kept.StepStatuses[0].Status != codingStepPending {
		t.Fatalf("maintenance turn rewrote the user's session: %+v", kept)
	}
	if kept.TurnCount != 2 || kept.LastSummary != userSummary || kept.LastOutcome != "completed" || len(kept.FilesModified) != 1 || kept.FilesModified[0] != "login.go" {
		t.Fatalf("maintenance turn did not record its own work: %+v", kept)
	}
	next := appendCodingPriorDeliverable("把按钮改成蓝色", kept.prevOutputs())
	if !strings.Contains(next, userSummary) || strings.Contains(next, "maint-done") {
		t.Fatalf("next user turn treated the event as the previous delivery: %s", next)
	}
}

func TestGitHubRepoMaintRemoteRecordKeepsUserDelivery(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		m.Kind = "remote"
		m.LastUserText = "USER_REQUEST_MARK"
		m.SessionPlan = "USER_SESSION_PLAN"
		m.LastSummary = "USER_SUMMARY_MARK"
		m.TurnCount = 1
	})
	h.writeStickyRemoteCodingTurn(userID, "event body", &RemoteCodingSubAgentResult{
		Status:        "success",
		Summary:       "maint-done",
		FilesModified: []string{"login.go"},
		Iterations:    2,
	}, "completed", false)
	kept := h.getStickyCodingWorkbenchMemory(userID)
	if kept.LastUserText != "USER_REQUEST_MARK" || kept.SessionPlan != "USER_SESSION_PLAN" || kept.LastSummary != "USER_SUMMARY_MARK" {
		t.Fatalf("remote host record rewrote the user delivery: %+v", kept)
	}
	if kept.TurnCount != 2 || kept.LastOutcome != "completed" || len(kept.FilesModified) != 1 || kept.FilesModified[0] != "login.go" {
		t.Fatalf("remote host record dropped the turn: %+v", kept)
	}
	next := buildRemoteCodingPlanStepText(nil, 1, 1, false, "把按钮改成蓝色", nil, kept.TurnCount, kept.SessionPlan, kept.LastSummary, kept.FilesModified, nil)
	if strings.Contains(next, "maint-done") || !strings.Contains(next, "USER_SUMMARY_MARK") || !strings.Contains(next, codingPriorDeliverableInputRule) {
		t.Fatalf("next remote user turn used the event summary: %s", next)
	}
}

func TestGitHubRepoMaintSendFailureRestoresSkipPlan(t *testing.T) {
	prevGit := newGitHubHTTPClient
	prevHub := newRepoMaintHubHTTPClient
	prevTurn := repoMaintTestTurn
	prevSendErr := repoMaintTestSendErr
	client := repoMaintInstantHTTPClient()
	newGitHubHTTPClient = func() *http.Client { return client }
	newRepoMaintHubHTTPClient = func() *http.Client { return client }
	t.Cleanup(func() {
		newGitHubHTTPClient = prevGit
		newRepoMaintHubHTTPClient = prevHub
		repoMaintTestTurn = prevTurn
		repoMaintTestSendErr = prevSendErr
	})

	app := newRepoMaintTestApp(t, "https://203.0.113.10", true)
	app.disableBackgroundEmbeddingForTest = true
	handler := &IMMessageHandler{app: app}
	app.imHandler = handler
	t.Cleanup(func() {
		taskAsyncFlushWG.Wait()
		skillHubRefreshWG.Wait()
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
		closeRepoMaintDB(filepath.Join(app.GetDataDir(), githubRepoMaintDBName))
	})
	ghost := initRepoMaintGhost(t, "yucejade", "jjquant-v2")
	rule := saveEnabledRepoMaintRule(t, app, ghost, "https://github.com/yucejade/jjquant-v2")
	turns := 0
	repoMaintTestTurn = func(req AIAssistantSendRequest) *IMAgentResponse {
		turns++
		repoMaintTurnFacts.Store(projectSessionOwnerID(req.ProjectPath), repoMaintTurnFact{Outcome: "completed", Set: true})
		return &IMAgentResponse{}
	}

	acceptRepoMaintIssue(t, app, rule, 1, "first issue")
	app.pumpRepoMaintOnce()
	first := repoMaintJobByObject(t, app, "issue:1")
	if first.Status != "succeeded" || first.TaskProjectPath == "" || turns != 1 {
		t.Fatalf("first issue = %+v turns=%d", first, turns)
	}
	userID := projectSessionOwnerID(first.TaskProjectPath)
	t.Cleanup(func() { handler.clearStickyCodingWorkbenchMemory(userID) })
	if handler.getStickyCodingWorkbenchMemory(userID).SkipNextPlan {
		t.Fatal("a started delivery armed skip-next-plan")
	}

	handler.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.SkipNextPlan = true
	})
	repoMaintTestSendErr = fmt.Errorf("send failed")
	acceptRepoMaintIssue(t, app, rule, 2, "send fails")
	app.pumpRepoMaintOnce()
	repoMaintTestSendErr = nil
	failed := repoMaintJobByObject(t, app, "issue:2")
	if failed.Status != "queued" || failed.TaskProjectPath != "" || turns != 2 {
		t.Fatalf("reused send failure = %+v turns=%d", failed, turns)
	}
	if repoMaintEventTaskPath(t, app, rule.ID, "issues.opened") != first.TaskProjectPath {
		t.Fatal("reused send failure cleared the task mapping")
	}
	if !handler.getStickyCodingWorkbenchMemory(userID).SkipNextPlan {
		t.Fatal("failed delivery cleared a skip the user had already armed")
	}
	// The failed row is queued again. Cancel it so the next pump starts only
	// the delivery that should see an unarmed skip.
	if err := app.setRepoMaintJobStatus(failed, "cancelled", "", "", ""); err != nil {
		t.Fatal(err)
	}

	handler.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.SkipNextPlan = false
	})
	repoMaintTestSendErr = fmt.Errorf("send failed")
	acceptRepoMaintIssue(t, app, rule, 3, "leave skip unarmed")
	app.pumpRepoMaintOnce()
	repoMaintTestSendErr = nil
	if turns != 3 {
		t.Fatalf("unarmed delivery turns=%d", turns)
	}
	if handler.getStickyCodingWorkbenchMemory(userID).SkipNextPlan {
		t.Fatal("failed delivery armed skip-next-plan")
	}
	if repoMaintEventTaskPath(t, app, rule.ID, "issues.opened") != first.TaskProjectPath {
		t.Fatal("the unarmed skip replaced the task")
	}
}

func TestGitHubRepoMaintRequeueClearsActiveUser(t *testing.T) {
	app := newRepoMaintTestApp(t, "https://203.0.113.10", false)
	t.Cleanup(func() {
		closeRepoMaintDB(filepath.Join(app.GetDataDir(), githubRepoMaintDBName))
	})
	ghost := t.TempDir()
	userID := "desktop-user:requeue"
	t.Cleanup(func() { repoMaintActiveUsers.Delete(userID) })
	insertRepoMaintRuleRaw(t, app, "rule-requeue", "yucejade", "jjquant-v2", ghost)
	job := repoMaintJobRow{
		ID: "job-requeue", RuleID: "rule-requeue", EventKind: "issues.opened", ObjectID: "issue:9",
		GhostDir: ghost, Status: "queued",
	}
	insertRepoMaintJobRaw(t, app, job)
	if err := app.setRepoMaintJobStatus(job, "running", "", userID, ""); err != nil {
		t.Fatal(err)
	}
	if dir, ok := repoMaintActiveUsers.Load(userID); !ok || dir.(string) != ghost {
		t.Fatalf("running owner = %v ok=%v", dir, ok)
	}
	// Start requeues with the snapshot taken before the row's session user
	// was written. The status transition must still drop that owner.
	if err := app.setRepoMaintJobStatus(job, "queued", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := repoMaintActiveUsers.Load(userID); ok {
		t.Fatal("stale requeue left the active user")
	}
	stored := repoMaintJobByObject(t, app, "issue:9")
	if stored.Status != "queued" || stored.SessionUserID != "" {
		t.Fatalf("requeued = %s user=%s", stored.Status, stored.SessionUserID)
	}
}

func TestGitHubRepoMaintHostTurnLeavesUserQuestion(t *testing.T) {
	h := newSessionRoundHandler(t)
	h.taskContextManager = agent.NewTaskContextManager(agent.DefaultTaskContextConfig(), sessionRoundNewTaskClassifier{})
	userID := "desktop-user"
	history := []agent.ConversationEntry{
		{Role: "user", Content: "把按钮改成蓝色"},
		{Role: "assistant", Content: "半径用 10 还是 20？"},
	}
	h.memory.Save(userID, history)
	prompt := repoMaintPromptLead + "\n要做的事：\n修一下检查"

	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question:  "半径用 10 还是 20？",
		History:   history,
		Timestamp: time.Now(),
	})
	if _, _, consumed, _ := h.consumePendingAskUserAnswer(userID, prompt, history); consumed {
		t.Fatal("automatic delivery was consumed as the ask_user answer")
	}
	if _, ok := h.pendingAskUser.Load(userID); !ok {
		t.Fatal("automatic delivery deleted the question waiting for the user")
	}

	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "半径用 10 还是 20？",
		History:   history,
		Timestamp: time.Now(),
	})
	entries := append([]agent.ConversationEntry(nil), history...)
	var slot *agent.UnfinishedTaskSlot
	if _, answered := h.bindPendingUserReplyAnswer(IMUserMessage{UserID: userID, Platform: githubRepoMaintPlatform, Text: prompt}, prompt, &entries, &slot); answered {
		t.Fatal("automatic delivery was bound as the user's reply")
	}
	if _, ok := h.pendingUserReply.Load(userID); !ok {
		t.Fatal("automatic delivery deleted the pending reply")
	}

	withEvent := append(append([]agent.ConversationEntry(nil), history...), agent.ConversationEntry{Role: "user", Content: prompt})
	if _, _, consumed, _ := h.consumePendingAskUserAnswer(userID, "用 10", withEvent); !consumed {
		t.Fatal("the user's answer was dropped after the delivery")
	}

	h.memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID:   "exit-1",
		UserID:   userID,
		Status:   agent.UnfinishedTaskSlotStatusInterrupted,
		Source:   agent.UnfinishedTaskSlotSourceAppExit,
		LastTask: "把按钮改成蓝色",
	})
	exitSlot := h.memory.GetUnfinishedSlot(userID)
	decision := applyAppExitAutoResumeDecision(IMUserMessage{UserID: userID, Platform: githubRepoMaintPlatform, Text: prompt}, prompt, exitSlot, explicitTaskSlotDecision{})
	if decision.ResumeSlotID != "" {
		t.Fatal("automatic delivery resumed the app-exit slot")
	}
	kept := h.memory.GetUnfinishedSlot(userID)
	_, fresh, clear := h.applyUnifiedTaskContextDecision(
		IMUserMessage{UserID: userID, Platform: githubRepoMaintPlatform, Text: prompt},
		prompt,
		explicitTaskSlotDecision{},
		history,
		&kept,
		"",
		false,
		false,
		false,
	)
	if fresh || clear {
		t.Fatalf("automatic delivery looked like a new task fresh=%v clear=%v", fresh, clear)
	}
	if got := h.memory.Load(userID); len(got) != len(history) {
		t.Fatalf("history len=%d, automatic delivery cleared the task", len(got))
	}
}
