package guiapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestCommitApprovalAnswerSticksForTheTask(t *testing.T) {
	root := t.TempDir()
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: root},
		task:     &TaskItem{Title: t.Name(), Description: "send the note"},
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })

	args := `{"to":"a@b.c","subject":"hi"}`
	got, blocked := agent.ApplyCommitGate(cb, "mailbox_send", args)
	if !blocked || !agent.IsAskUserResult(got.Result) {
		t.Fatalf("first send result=%+v blocked=%v", got, blocked)
	}
	ask, ok := ParseAskUserResult(got.Result)
	if !ok {
		t.Fatal("ask did not parse")
	}
	_, fingerprint, _, ok := agent.ParseCommitApprovalContext(ask.Context)
	if !ok || fingerprint == "" {
		t.Fatal("ask lost its ticket")
	}
	forged := *ask
	forged.Context = agent.CommitApprovalContext(cb.CommitTaskKey(), fingerprint, "forged")
	if takeCommitApproval(&forged) != nil {
		t.Fatal("forged ticket consumed the note")
	}
	if _, still := commitAskNotes.Load(cb.CommitTaskKey()); !still {
		t.Fatal("forged ticket deleted the note")
	}
	history := []agent.ConversationEntry{{Role: "assistant", Content: "hold"}}
	h := &IMMessageHandler{}
	userID := "commit-" + strings.ReplaceAll(t.Name(), "/", "-")
	h.commitPendingAskUser(userID, ask, history, nil)
	pending, _ := h.pendingAskUser.Load(userID)
	state, _ := pending.(*pendingAskUserState)
	if state == nil || state.CommitApproval == nil || state.CommitApproval.TaskKey != cb.CommitTaskKey() {
		t.Fatalf("pending=%+v", state)
	}
	if strings.Contains(state.Context, agent.CommitApprovalContextPrefix) {
		t.Fatalf("stored context kept the ticket: %q", state.Context)
	}

	hint, _, consumed, settled := h.consumePendingAskUserAnswer(userID, "3", history)
	if !consumed || settled || !strings.Contains(hint, "拒绝") {
		t.Fatalf("hint=%q consumed=%v settled=%v", hint, consumed, settled)
	}
	denied, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"subject":"other","to":"c@d.e"}`)
	if !blocked || agent.IsAskUserResult(denied.Result) || !strings.Contains(denied.Result, "已拒绝") {
		t.Fatalf("same shape after reject: %+v blocked=%v", denied, blocked)
	}

	goal, missing, locked := cb.IterationContract()
	if !strings.Contains(goal, t.Name()) || len(missing) != 0 || len(locked) != 1 || locked[0] != "mailbox_send" {
		t.Fatalf("contract goal=%q missing=%v locked=%v", goal, missing, locked)
	}
}

func TestCommitApprovalOnceIsOneCall(t *testing.T) {
	root := t.TempDir()
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: root},
		task:     &TaskItem{Title: t.Name()},
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })
	got, blocked := agent.ApplyCommitGate(cb, "im_message", `{"text":"hi"}`)
	if !blocked || !agent.IsAskUserResult(got.Result) {
		t.Fatalf("ask result=%+v blocked=%v", got, blocked)
	}
	ask, ok := ParseAskUserResult(got.Result)
	if !ok {
		t.Fatal("ask did not parse")
	}
	history := []agent.ConversationEntry{{Role: "assistant", Content: "hold"}}
	h := &IMMessageHandler{}
	userID := "once-" + strings.ReplaceAll(t.Name(), "/", "-")
	h.commitPendingAskUser(userID, ask, history, nil)
	if _, _, consumed, _ := h.consumePendingAskUserAnswer(userID, "1", history); !consumed {
		t.Fatal("numbered answer was not consumed")
	}
	allowed, blocked := agent.ApplyCommitGate(cb, "im_message", `{"text":"again"}`)
	if blocked || allowed.Result != "" {
		t.Fatalf("once allow result=%+v blocked=%v", allowed, blocked)
	}
	next, blocked := agent.ApplyCommitGate(cb, "im_message", `{"text":"third"}`)
	if !blocked || !agent.IsAskUserResult(next.Result) {
		t.Fatalf("second shape result=%+v blocked=%v", next, blocked)
	}
}

func TestCommitChecklistOpenRefusesSendAndPush(t *testing.T) {
	root := t.TempDir()
	asked := false
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			projectPath: root,
			commitAsk: func(string, string, string) (agent.CommitDecision, bool) {
				asked = true
				return agent.CommitAllowTask, true
			},
		},
		task: &TaskItem{Title: t.Name()},
		todos: codingAgentTodoState{items: []codingAgentTodoItem{{
			ID: "1", Content: "send it", Status: codingAgentTodoPending,
		}}},
	}
	if !cb.CommitChecklistOpen() {
		t.Fatal("pending checklist was closed")
	}
	for _, call := range []struct{ name, args string }{
		{"mailbox_send", `{"to":"a@b.c"}`},
		{"bash", `{"command":"git push"}`},
	} {
		got, blocked := agent.ApplyCommitGate(cb, call.name, call.args)
		if !blocked || agent.IsAskUserResult(got.Result) || !strings.Contains(got.Result, "步骤清单") || !strings.Contains(got.Result, "缺少人的批准") {
			t.Fatalf("%s result=%+v blocked=%v", call.name, got, blocked)
		}
	}
	if _, blocked := agent.ApplyCommitGate(cb, "bash", `{"command":"npm test"}`); blocked {
		t.Fatal("npm test was blocked while the checklist was open")
	}
	if asked {
		t.Fatal("open checklist asked for approval")
	}
	_, _, locked := cb.IterationContract()
	joined := strings.Join(locked, ",")
	if !strings.Contains(joined, "mailbox_send") || !strings.Contains(joined, "git push") || strings.Contains(joined, "bash") {
		t.Fatalf("locked=%v", locked)
	}

	remote := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{projectDir: root},
		task:  t.Name(),
		todos: codingAgentTodoState{items: []codingAgentTodoItem{{
			ID: "1", Content: "push", Status: codingAgentTodoPending,
		}}},
	}
	got, blocked := agent.ApplyCommitGate(remote, "bash", `{"command":"git -C repo push"}`)
	if !blocked || agent.IsAskUserResult(got.Result) || !strings.Contains(got.Result, "缺少人的批准") {
		t.Fatalf("remote checklist result=%+v blocked=%v", got, blocked)
	}
}

func TestDesktopIterationContractListsSkillGapsWithoutFlushing(t *testing.T) {
	binding := reportPathBinding()
	cb := &sharedAgentLoopCallbacks{
		userText:              "写一份报告\n请附上文件",
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	if cb.CommitChecklistOpen() || cb.CommitTaskKey() != "" || cb.CommitPrepareAsk("fp") != "" {
		t.Fatal("desktop host invented a task key")
	}
	goal, missing, locked := cb.IterationContract()
	if goal != cb.userText || len(missing) != 1 || missing[0] != "report_path" || len(locked) != 0 {
		t.Fatalf("goal=%q missing=%v locked=%v", goal, missing, locked)
	}
	if cb.turnPublishedPDF {
		t.Fatal("iteration contract flushed a PDF")
	}
	if got := cb.UnassignedDeliverables("已经做完"); len(got) != 1 || got[0] != "report_path" || cb.turnPublishedPDF {
		t.Fatalf("unassigned=%v published=%v", got, cb.turnPublishedPDF)
	}
}

type commitGateResult struct {
	res     agent.ToolExecutionResult
	blocked bool
}

func waitCommitShapePending(t *testing.T, projectPath string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var id string
		pendingScopeApprovals.Range(func(_, v any) bool {
			pending, ok := v.(*pendingScopeApproval)
			if ok && pending.Request.Kind == commitShapeApprovalKind && pending.Request.ProjectPath == projectPath {
				id = pending.ID
				if pending.Request.Message != agent.CommitAskQuestion(pending.Request.ToolName) {
					t.Errorf("dialog question = %q", pending.Request.Message)
				}
				if strings.Contains(pending.Request.Path, "secret") || strings.Contains(strings.ToLower(pending.Request.Path), "body") {
					t.Errorf("dialog subject leaked a body: %q", pending.Request.Path)
				}
				switch pending.Request.ToolName {
				case "mailbox_send":
					if pending.Request.Path != "a@b.c" {
						t.Errorf("mailbox subject = %q", pending.Request.Path)
					}
				case "im_message":
					if pending.Request.Path != "" {
						t.Errorf("im body leaked: %q", pending.Request.Path)
					}
				case "bash":
					if pending.Request.Path != "git push" {
						t.Errorf("bash subject = %q", pending.Request.Path)
					}
				}
				return false
			}
			return true
		})
		if id != "" {
			return id
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("commit approval dialog was not opened")
	return ""
}

func closeLoopCtx(loopCtx *LoopContext) {
	if loopCtx == nil || loopCtx.CancelC == nil {
		return
	}
	select {
	case <-loopCtx.CancelC:
	default:
		close(loopCtx.CancelC)
	}
}

func TestCommitShapeDialogContinuesOrDeniesTheSameCall(t *testing.T) {
	root := t.TempDir()
	audit, err := NewAuditLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	handler := &IMMessageHandler{app: &App{auditLog: audit}}
	loopCtx := &LoopContext{CancelC: make(chan struct{})}
	t.Cleanup(func() { closeLoopCtx(loopCtx) })
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: handler, loopCtx: loopCtx, projectPath: root},
		task:     &TaskItem{Title: t.Name()},
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })
	t.Cleanup(func() {
		pendingScopeApprovals.Range(func(key, value any) bool {
			pending, ok := value.(*pendingScopeApproval)
			if ok && pending.Request.ProjectPath == root {
				pendingScopeApprovals.Delete(key)
			}
			return true
		})
	})

	firstCh := make(chan commitGateResult, 1)
	go func() {
		res, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"to":"a@b.c","body":"secret"}`)
		firstCh <- commitGateResult{res, blocked}
	}()
	ResolveScopeApproval(waitCommitShapePending(t, root), "本任务内允许")
	first := <-firstCh
	if first.blocked || first.res.Result != "" {
		t.Fatalf("task allow result=%+v blocked=%v", first.res, first.blocked)
	}
	again, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"body":"other","to":"z@y.x"}`)
	if blocked || again.Result != "" {
		t.Fatalf("same shape stayed blocked: %+v blocked=%v", again, blocked)
	}

	onceCtx := &LoopContext{CancelC: make(chan struct{})}
	t.Cleanup(func() { closeLoopCtx(onceCtx) })
	once := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: handler, loopCtx: onceCtx, projectPath: root},
		task:     &TaskItem{Title: t.Name() + "-once"},
	}
	t.Cleanup(func() { forgetCommitLedger(once.CommitTaskKey()) })
	onceCh := make(chan commitGateResult, 1)
	go func() {
		res, blocked := agent.ApplyCommitGate(once, "im_message", `{"text":"hi"}`)
		onceCh <- commitGateResult{res, blocked}
	}()
	ResolveScopeApproval(waitCommitShapePending(t, root), "仅这一次")
	if allowed := <-onceCh; allowed.blocked || allowed.res.Result != "" {
		t.Fatalf("once result=%+v blocked=%v", allowed.res, allowed.blocked)
	}
	denyCh := make(chan commitGateResult, 1)
	go func() {
		res, blocked := agent.ApplyCommitGate(once, "im_message", `{"text":"again"}`)
		denyCh <- commitGateResult{res, blocked}
	}()
	ResolveScopeApproval(waitCommitShapePending(t, root), "拒绝")
	denied := <-denyCh
	if !denied.blocked || agent.IsAskUserResult(denied.res.Result) || !strings.Contains(denied.res.Result, "已拒绝") {
		t.Fatalf("deny result=%+v blocked=%v", denied.res, denied.blocked)
	}
	stuck, blocked := agent.ApplyCommitGate(once, "im_message", `{"text":"third"}`)
	if !blocked || agent.IsAskUserResult(stuck.Result) || !strings.Contains(stuck.Result, "已拒绝") {
		t.Fatalf("stored deny result=%+v blocked=%v", stuck, blocked)
	}

	remoteCtx := &LoopContext{CancelC: make(chan struct{})}
	t.Cleanup(func() { closeLoopCtx(remoteCtx) })
	remote := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{handler: handler, loopCtx: remoteCtx, projectDir: root},
		task:  t.Name() + "-remote",
	}
	t.Cleanup(func() { forgetCommitLedger(remote.CommitTaskKey()) })
	cancelCh := make(chan commitGateResult, 1)
	go func() {
		res, blocked := agent.ApplyCommitGate(remote, "bash", `{"command":"git push"}`)
		cancelCh <- commitGateResult{res, blocked}
	}()
	cancelID := waitCommitShapePending(t, root)
	closeLoopCtx(remoteCtx)
	cancelled := <-cancelCh
	if !cancelled.blocked || agent.IsAskUserResult(cancelled.res.Result) || !strings.Contains(cancelled.res.Result, "先不执行") || strings.Contains(cancelled.res.Result, "已拒绝") {
		t.Fatalf("cancel result=%+v blocked=%v", cancelled.res, cancelled.blocked)
	}
	if _, found := pendingScopeApprovals.Load(cancelID); found {
		t.Fatal("cancelled dialog stayed pending")
	}
	follow, blocked := agent.ApplyCommitGate(remote, "bash", `{"command":"git push"}`)
	if !blocked || agent.IsAskUserResult(follow.Result) || strings.Contains(follow.Result, "已拒绝") {
		t.Fatalf("cancel stuck a denial: %+v blocked=%v", follow, blocked)
	}
}

func TestVECommitGateRequiresApprovalForSendFile(t *testing.T) {
	cb := &veAgentCallbacks{ctx: context.Background(), sessionID: "session-1", requestID: "request-1"}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })
	subject, ok := cb.CommitCallSubject("send_to_im", `{"path":"report.pdf","destination":"wechat","message":"secret"}`)
	if !ok || subject != "report.pdf → 当前会话" || strings.Contains(subject, "wechat") || strings.Contains(subject, "secret") {
		t.Fatalf("VE subject = %q ok=%v", subject, ok)
	}
	result, blocked := agent.ApplyCommitGate(cb, "send_file", `{"path":"report.pdf","destination":"wechat"}`)
	if !blocked || agent.IsAskUserResult(result.Result) || !strings.Contains(result.Result, "先不执行") {
		t.Fatalf("unapproved VE send result=%+v blocked=%v", result, blocked)
	}
	cb.commitAsk = func(toolName, fingerprint, subject string) (agent.CommitDecision, bool) {
		if toolName != "send_file" || strings.Contains(fingerprint, "report.pdf") || strings.Contains(subject, "wechat") || subject != "report.pdf → 当前会话" {
			t.Errorf("resolver saw tool=%q fingerprint=%q subject=%q", toolName, fingerprint, subject)
		}
		return agent.CommitAllowOnce, true
	}
	if result, blocked := agent.ApplyCommitGate(cb, "send_file", `{"path":"report.pdf","destination":"wechat"}`); blocked {
		t.Fatalf("allowed VE send stayed blocked: %+v", result)
	}
	cb.commitAsk = func(string, string, string) (agent.CommitDecision, bool) {
		return agent.CommitDeny, true
	}
	denied, blocked := agent.ApplyCommitGate(cb, "send_file", `{"path":"report.pdf"}`)
	if !blocked || agent.IsAskUserResult(denied.Result) || !strings.Contains(denied.Result, "已拒绝") {
		t.Fatalf("denied VE send result=%+v blocked=%v", denied, blocked)
	}
}

func TestVECommitWaitResolvesOnTheConfirmDialog(t *testing.T) {
	app := &App{}
	done := make(chan struct{})
	var decision agent.CommitDecision
	var resolved bool
	go func() {
		decision, resolved = waitVECommitApproval(app, context.Background(), "session-1", "send_file", "report.pdf → 当前会话")
		close(done)
	}()
	var requestID string
	deadline := time.Now().Add(2 * time.Second)
	for requestID == "" && time.Now().Before(deadline) {
		veSensitiveApprovals.mu.Lock()
		for id := range veSensitiveApprovals.pending {
			if strings.HasPrefix(id, "ve_commit_") {
				requestID = id
				break
			}
		}
		veSensitiveApprovals.mu.Unlock()
		if requestID == "" {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if requestID == "" {
		t.Fatal("commit confirm was not registered")
	}
	if err := app.RespondDigitalEmployeeSensitiveRequest(requestID, "allow"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("commit confirm did not return")
	}
	if !resolved || decision != agent.CommitAllowOnce {
		t.Fatalf("decision=%q resolved=%v", decision, resolved)
	}
}

func TestFinishCommitTurnKeepsOnlyPausedTaskState(t *testing.T) {
	taskKey := "desktop\nuser\nproject"
	t.Cleanup(func() { forgetCommitLedger(taskKey) })
	if err := storeCommit(taskKey, "fp", agent.CommitAllowTask); err != nil {
		t.Fatal(err)
	}
	prepareCommitAsk(taskKey, "fp")
	finishCommitTurn(nil, taskKey, &agent.AskUserRequest{Question: "approve?"})
	if _, found := lookupCommit(taskKey, "fp"); !found {
		t.Fatal("paused turn dropped its decision")
	}
	if _, found := commitAskNotes.Load(taskKey); !found {
		t.Fatal("paused turn dropped its pending ask")
	}
	finishCommitTurn(nil, taskKey, nil)
	if _, found := lookupCommit(taskKey, "fp"); found {
		t.Fatal("terminal turn leaked its decision")
	}
	if _, found := commitAskNotes.Load(taskKey); found {
		t.Fatal("terminal turn leaked its pending ask")
	}
}

func TestBareAppCommitLedgerDoesNotOpenADataFile(t *testing.T) {
	app := &App{}
	if _, ok := commitApprovalDBPath(app); ok {
		t.Fatal("bare app selected a data file")
	}
	root := t.TempDir()
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{handler: &IMMessageHandler{app: app}, projectPath: root},
		task:     &TaskItem{Title: t.Name()},
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })
	fingerprint := "mailbox_send\nto\nnone"
	if err := cb.CommitStore(fingerprint, agent.CommitDeny); err != nil {
		t.Fatal(err)
	}
	decision, found := cb.CommitLookup(fingerprint)
	if !found || decision != agent.CommitDeny {
		t.Fatalf("lookup=%q found=%v", decision, found)
	}
}

func TestPersistentCommitTaskStateDoesNotLeakIntoNextTask(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	taskKey := "desktop\nuser\nsame-project"
	fingerprint := "mailbox_send\nto\nnone"
	t.Cleanup(func() { forgetCommitLedgerForApp(app, taskKey) })
	if err := storeCommitForApp(app, taskKey, fingerprint, agent.CommitAllowTask); err != nil {
		t.Fatal(err)
	}
	prepareCommitAskForApp(app, taskKey, fingerprint)
	finishCommitTurn(app, taskKey, &agent.AskUserRequest{Question: "approve?"})
	if decision, found := lookupCommitForApp(app, taskKey, fingerprint); !found || decision != agent.CommitAllowTask {
		t.Fatalf("paused task lost decision=%q found=%v", decision, found)
	}
	finishCommitTurn(app, taskKey, nil)
	if _, found := lookupCommitForApp(app, taskKey, fingerprint); found {
		t.Fatal("same task key inherited approval in the next task")
	}
}

func TestCodingCommitTaskKeysAreUniquePerAgentInstance(t *testing.T) {
	root := t.TempDir()
	task := &TaskItem{Title: "same title"}
	first := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{}, nil, root, nil)
	second := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{}, nil, root, nil)
	firstKey := (&codingSubAgentCallbacks{subagent: first, task: task}).CommitTaskKey()
	secondKey := (&codingSubAgentCallbacks{subagent: second, task: task}).CommitTaskKey()
	if firstKey == secondKey {
		t.Fatalf("same-title agents share commit task key %q", firstKey)
	}
	remoteFirst := NewRemoteCodingSubAgent(nil, corelib.MaclawLLMConfig{}, nil, "session", root, root, nil)
	remoteSecond := NewRemoteCodingSubAgent(nil, corelib.MaclawLLMConfig{}, nil, "session", root, root, nil)
	remoteFirstKey := (&remoteCodingCallbacks{agent: remoteFirst, task: task.Title}).CommitTaskKey()
	remoteSecondKey := (&remoteCodingCallbacks{agent: remoteSecond, task: task.Title}).CommitTaskKey()
	if remoteFirstKey == remoteSecondKey {
		t.Fatalf("same-title remote agents share commit task key %q", remoteFirstKey)
	}
}

func TestDesktopCommitTaskKeyResumesFromPersistentPendingAsk(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	scope := desktopCommitScope("user", "project")
	firstKey := scope + "\nrun-first"
	secondKey := scope + "\nrun-second"
	t.Cleanup(func() {
		desktopCommitTasks.Delete(scope)
		forgetCommitLedgerForApp(app, firstKey)
	})
	prepareCommitAskForApp(app, firstKey, "mailbox_send\nto\nnone")
	if got := resumeDesktopCommitTaskKey(app, scope, "run-second"); got != firstKey {
		t.Fatalf("resume key=%q, want persistent %q", got, firstKey)
	}
	finishDesktopCommitTurn(app, scope, firstKey, &agent.AskUserRequest{Question: "approve?"})
	if got := resumeDesktopCommitTaskKey(app, scope, "run-second"); got != firstKey {
		t.Fatalf("in-process resume key=%q", got)
	}
	finishDesktopCommitTurn(app, scope, firstKey, nil)
	if got := resumeDesktopCommitTaskKey(app, scope, "run-second"); got != secondKey {
		t.Fatalf("terminal task key=%q, want fresh %q", got, secondKey)
	}
}

func TestCommitApprovalResumeKeepsThePausedCall(t *testing.T) {
	resumed, profile, ok := commitApprovalResumePlan("[Context hint] " + commitApprovalAllowHint + "，本任务内都算允许。请立刻用同样的工具再调用一次。")
	if !ok || resumed.Primary != intent.LabelContinuation || profile.Reason != shortContinuationReason || profile.IsLight() {
		t.Fatalf("allow resume=%+v profile=%+v ok=%v", resumed, profile, ok)
	}
	if _, _, ok := commitApprovalResumePlan("[Context hint] " + commitApprovalDenyHint + "。不要再调用。"); !ok {
		t.Fatal("deny hint must resume the paused call")
	}
	if _, _, ok := commitApprovalResumePlan("生成新的pdf"); ok {
		t.Fatal("a document request is not a commit card")
	}
}

func TestMemoryCommitTicketCanBeReadUntilSettled(t *testing.T) {
	taskKey := "desktop\nuser\nrepeat-ticket"
	fingerprint := "mailbox_send\nto\nnone"
	t.Cleanup(func() { forgetCommitLedger(taskKey) })
	nonce := prepareCommitAsk(taskKey, fingerprint)
	ask := &AskUserRequest{Context: agent.CommitApprovalContext(taskKey, fingerprint, nonce)}
	if takeCommitApproval(ask) == nil || takeCommitApproval(ask) == nil {
		t.Fatal("reading the pending card consumed its ticket")
	}
	h := &IMMessageHandler{}
	if _, handled := h.finishCommitApprovalAskUser("仅这一次", takeCommitApproval(ask)); !handled {
		t.Fatal("valid answer was not settled")
	}
	if _, found := commitAskNotes.Load(taskKey); found {
		t.Fatal("settled memory ticket remained pending")
	}
}
