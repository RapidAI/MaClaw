package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// commitAskFunc answers one human-class call. The fingerprint is the shape
// the gate already classified. subject is only what the card shows.
// The function must not trust a model argument.
type commitAskFunc func(toolName, fingerprint, subject string) (agent.CommitDecision, bool)

// The ledger and the pending nonce are keyed by task. An app that already
// has its own data directory keeps them in that directory so a paused
// approval survives restart. A bare app keeps them in this process and
// does not create a file.
var commitLedgers sync.Map
var commitAskNotes sync.Map

type commitAskNote struct {
	TaskKey     string
	Fingerprint string
	Nonce       string
}

type pendingCommitApproval struct {
	TaskKey     string
	Fingerprint string
	Nonce       string
}

func (n *pendingCommitApproval) askNote() commitAskNote {
	if n == nil {
		return commitAskNote{}
	}
	return commitAskNote{TaskKey: n.TaskKey, Fingerprint: n.Fingerprint, Nonce: n.Nonce}
}

func commitLedger(taskKey string) *agent.CommitLedger {
	taskKey = strings.TrimSpace(taskKey)
	if taskKey == "" {
		return nil
	}
	if existing, ok := commitLedgers.Load(taskKey); ok {
		if ledger, ok := existing.(*agent.CommitLedger); ok && ledger != nil {
			return ledger
		}
	}
	ledger := agent.NewCommitLedger()
	actual, _ := commitLedgers.LoadOrStore(taskKey, ledger)
	stored, _ := actual.(*agent.CommitLedger)
	if stored == nil {
		return ledger
	}
	return stored
}

func commitAppForHandler(handler *IMMessageHandler) *App {
	if handler == nil {
		return nil
	}
	return handler.app
}

func forgetCommitLedger(taskKey string) {
	forgetCommitLedgerForApp(nil, taskKey)
}

// forgetCommitLedgerForApp drops this task's approvals from memory and,
// when the app has its own data directory, from that directory's ledger.
func forgetCommitLedgerForApp(app *App, taskKey string) {
	taskKey = strings.TrimSpace(taskKey)
	if taskKey == "" {
		return
	}
	commitLedgers.Delete(taskKey)
	commitAskNotes.Delete(taskKey)
	store, err := commitApprovalStoreForApp(app)
	if err != nil {
		log.Printf("[commit-gate] open approval store failed: %v", err)
		return
	}
	if store == nil {
		return
	}
	if err := store.forget(taskKey); err != nil {
		log.Printf("[commit-gate] forget task state failed: %v", err)
	}
}

// finishCommitTurn drops approvals when the turn ended. A paused ask keeps
// the pending note and any decision already stored, so the answer can land.
func finishCommitTurn(app *App, taskKey string, ask *agent.AskUserRequest) {
	if ask == nil {
		forgetCommitLedgerForApp(app, taskKey)
	}
}

func lookupCommit(taskKey, fingerprint string) (agent.CommitDecision, bool) {
	return lookupCommitForApp(nil, taskKey, fingerprint)
}

func lookupCommitForApp(app *App, taskKey, fingerprint string) (agent.CommitDecision, bool) {
	store, err := commitApprovalStoreForApp(app)
	if err != nil {
		log.Printf("[commit-gate] open approval store failed: %v", err)
		return "", false
	}
	if store != nil {
		decision, found, err := store.lookup(taskKey, fingerprint)
		if err != nil {
			log.Printf("[commit-gate] lookup failed: %v", err)
			return "", false
		}
		return decision, found
	}
	ledger := commitLedger(taskKey)
	if ledger == nil {
		return "", false
	}
	return ledger.Lookup(fingerprint)
}

func storeCommit(taskKey, fingerprint string, decision agent.CommitDecision) error {
	return storeCommitForApp(nil, taskKey, fingerprint, decision)
}

func storeCommitForApp(app *App, taskKey, fingerprint string, decision agent.CommitDecision) error {
	taskKey = strings.TrimSpace(taskKey)
	fingerprint = strings.TrimSpace(fingerprint)
	if taskKey == "" || fingerprint == "" {
		return fmt.Errorf("commit approval is missing a task or fingerprint")
	}
	store, err := commitApprovalStoreForApp(app)
	if err != nil {
		return err
	}
	if store != nil {
		return store.storeDecision(taskKey, fingerprint, decision)
	}
	ledger := commitLedger(taskKey)
	if ledger == nil {
		return fmt.Errorf("commit task key is empty")
	}
	ledger.Store(fingerprint, decision)
	return nil
}

func prepareCommitAsk(taskKey, fingerprint string) string {
	return prepareCommitAskForApp(nil, taskKey, fingerprint)
}

func prepareCommitAskForApp(app *App, taskKey, fingerprint string) string {
	taskKey = strings.TrimSpace(taskKey)
	fingerprint = strings.TrimSpace(fingerprint)
	if taskKey == "" || fingerprint == "" {
		return ""
	}
	nonce := newConfirmNonce()
	if nonce == "" {
		return ""
	}
	note := commitAskNote{TaskKey: taskKey, Fingerprint: fingerprint, Nonce: nonce}
	store, err := commitApprovalStoreForApp(app)
	if err != nil {
		log.Printf("[commit-gate] open approval store failed: %v", err)
		return ""
	}
	if store != nil {
		if err := store.prepare(note); err != nil {
			log.Printf("[commit-gate] prepare approval failed: %v", err)
			return ""
		}
		return nonce
	}
	commitAskNotes.Store(taskKey, note)
	return nonce
}

func takeCommitApproval(askReq *AskUserRequest) *pendingCommitApproval {
	return takeCommitApprovalForApp(nil, askReq)
}

func takeCommitApprovalForApp(app *App, askReq *AskUserRequest) *pendingCommitApproval {
	if askReq == nil {
		return nil
	}
	taskKey, fingerprint, nonce, ok := agent.ParseCommitApprovalContext(askReq.Context)
	if !ok || strings.TrimSpace(taskKey) == "" || strings.TrimSpace(nonce) == "" {
		return nil
	}
	note := commitAskNote{TaskKey: taskKey, Fingerprint: fingerprint, Nonce: nonce}
	store, err := commitApprovalStoreForApp(app)
	if err != nil {
		log.Printf("[commit-gate] open approval store failed: %v", err)
		return nil
	}
	if store != nil {
		if err := store.validate(note); err != nil {
			return nil
		}
		return &pendingCommitApproval{TaskKey: taskKey, Fingerprint: fingerprint, Nonce: nonce}
	}
	raw, found := commitAskNotes.Load(taskKey)
	stored, ok := raw.(commitAskNote)
	if !found || !ok || stored != note {
		return nil
	}
	return &pendingCommitApproval{TaskKey: taskKey, Fingerprint: fingerprint, Nonce: nonce}
}

func commitLockedNames(checklistOpen bool, taskKey string) []string {
	return commitLockedNamesForApp(nil, checklistOpen, taskKey)
}

func commitLockedNamesForApp(app *App, checklistOpen bool, taskKey string) []string {
	if checklistOpen {
		return agent.HumanCommitToolNames()
	}
	store, err := commitApprovalStoreForApp(app)
	if err != nil {
		log.Printf("[commit-gate] open approval store failed: %v", err)
		return nil
	}
	if store != nil {
		names, err := store.deniedTools(taskKey)
		if err != nil {
			log.Printf("[commit-gate] denied tools failed: %v", err)
			return nil
		}
		return names
	}
	ledger := commitLedger(taskKey)
	if ledger == nil {
		return nil
	}
	return ledger.DeniedTools()
}

func commitTaskTemp(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return filepath.Join(root, skill.MaclawSkillTmpDirName)
}

func codingContractGoal(title, description string) string {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	switch {
	case title == "":
		return description
	case description == "" || description == title:
		return title
	default:
		return title + " " + description
	}
}

const (
	commitApprovalAllowHint = "用户允许了这一形状的调用"
	commitApprovalDenyHint  = "用户拒绝了这一形状的调用"
)

// commitApprovalResumePlan keeps a card answer on the paused external call.
// "本任务内允许" is not a new document request. Re-planning it as delivery
// drops the file this task already published and the turn ends in a host
// reject before the approved tool can run again. Current-channel attach is
// not this path: that card is not a send, and the host attaches the file.
func commitApprovalResumePlan(askUserContext string) (intent.ClassificationResult, ExecutionProfile, bool) {
	text := strings.TrimSpace(askUserContext)
	if !strings.Contains(text, commitApprovalAllowHint) && !strings.Contains(text, commitApprovalDenyHint) {
		return intent.ClassificationResult{}, ExecutionProfile{}, false
	}
	return intent.ClassificationResult{
		Primary:    intent.LabelContinuation,
		Confidence: 1,
		Reason:     "commit approval resumes the paused call",
	}, fullExecutionProfile(shortContinuationReason), true
}

func (h *IMMessageHandler) finishCommitApprovalAskUser(answer string, note *pendingCommitApproval) (string, bool) {
	if h == nil || note == nil || strings.TrimSpace(note.Fingerprint) == "" || strings.TrimSpace(note.TaskKey) == "" {
		return "", false
	}
	decision, ok := agent.ParseCommitApprovalAnswer(answer)
	if !ok {
		return "", false
	}
	askNote := note.askNote()
	store, err := commitApprovalStoreForApp(h.app)
	if err != nil {
		return "[Context hint] 审批状态保存失败，这一步未执行。请稍后重试。", true
	}
	if store != nil {
		err = store.settle(askNote, decision)
	} else {
		err = storeCommit(note.TaskKey, note.Fingerprint, decision)
		if err == nil {
			commitAskNotes.Delete(note.TaskKey)
		}
	}
	if err != nil {
		return "[Context hint] 审批状态保存失败，这一步未执行。请稍后重试。", true
	}
	switch decision {
	case agent.CommitAllowOnce:
		return "[Context hint] " + commitApprovalAllowHint + "，仅这一次。请立刻用同样的工具再调用一次。不要再问用户是否继续。", true
	case agent.CommitAllowTask:
		return "[Context hint] " + commitApprovalAllowHint + "，本任务内都算允许。请立刻用同样的工具再调用一次。不要再问用户是否继续。", true
	case agent.CommitDeny:
		return "[Context hint] " + commitApprovalDenyHint + "。这一任务里不要再调用它，并向用户说明这一步被拒绝了。", true
	default:
		return "", false
	}
}

func (c *sharedAgentLoopCallbacks) CommitWorkspaceRoot() string {
	if c == nil || c.handler == nil {
		return ""
	}
	return strings.TrimSpace(c.handler.effectiveWorkingDirForUser(c.userID))
}

func (c *sharedAgentLoopCallbacks) CommitTaskTempDir() string {
	return commitTaskTemp(c.CommitWorkspaceRoot())
}

func (c *sharedAgentLoopCallbacks) CommitChecklistOpen() bool { return false }

// CommitCallBoundToChat reports that this call is the one live current-chat
// file grant. A path or destination on that call cannot leave the chat: the
// invocation attaches the artifact the plan already bound. The commit gate
// must not pause it. An approval would resume the same name on the legacy
// sender, which can send the path.
func (c *sharedAgentLoopCallbacks) CommitCallBoundToChat(name, _ string) bool {
	if c == nil || c.semanticSurface == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(name), "send_file") {
		return false
	}
	live, grant := soleLiveSemanticGrantByAdapter(c.semanticSurface, "semantic_deliver_current_file")
	return live != "" && strings.TrimSpace(grant.Token) != "" && strings.EqualFold(live, strings.TrimSpace(name))
}

var desktopCommitTasks sync.Map

func desktopCommitScope(userID, root string) string {
	userID = strings.TrimSpace(userID)
	root = strings.TrimSpace(root)
	if userID == "" && root == "" {
		return ""
	}
	return "desktop\n" + userID + "\n" + root
}

func resumeDesktopCommitTaskKey(app *App, scope, recoveryRunID string) string {
	if existing, ok := desktopCommitTasks.Load(scope); ok {
		if taskKey, ok := existing.(string); ok && strings.TrimSpace(taskKey) != "" {
			return taskKey
		}
	}
	if store, err := commitApprovalStoreForApp(app); err == nil && store != nil {
		if taskKey, err := store.latestPendingTaskKey(scope); err == nil && taskKey != "" {
			return taskKey
		}
	}
	return scope + "\n" + strings.TrimSpace(recoveryRunID)
}

func finishDesktopCommitTurn(app *App, scope, taskKey string, ask *agent.AskUserRequest) {
	if ask != nil {
		desktopCommitTasks.Store(scope, taskKey)
		return
	}
	desktopCommitTasks.Delete(scope)
	forgetCommitLedgerForApp(app, taskKey)
}

func (c *sharedAgentLoopCallbacks) CommitTaskKey() string {
	if c == nil {
		return ""
	}
	if strings.TrimSpace(c.commitTaskKey) != "" {
		return c.commitTaskKey
	}
	return desktopCommitScope(c.userID, c.CommitWorkspaceRoot())
}

func (c *sharedAgentLoopCallbacks) CommitLookup(fingerprint string) (agent.CommitDecision, bool) {
	if c == nil {
		return "", false
	}
	return lookupCommitForApp(commitAppForHandler(c.handler), c.CommitTaskKey(), fingerprint)
}

func (c *sharedAgentLoopCallbacks) CommitStore(fingerprint string, decision agent.CommitDecision) error {
	if c == nil {
		return fmt.Errorf("commit host is nil")
	}
	return storeCommitForApp(commitAppForHandler(c.handler), c.CommitTaskKey(), fingerprint, decision)
}

func (c *sharedAgentLoopCallbacks) CommitPrepareAsk(fingerprint string) string {
	if c == nil {
		return ""
	}
	return prepareCommitAskForApp(commitAppForHandler(c.handler), c.CommitTaskKey(), fingerprint)
}

func (c *sharedAgentLoopCallbacks) IterationContract() (string, []string, []string) {
	if c == nil {
		return "", nil, nil
	}
	missing := mergeDeliverableNames(c.deliverableGapNames(false, ""), hopMissingRequiredEffects(c.hopRunSnapshot()))
	return c.userText, missing, commitLockedNamesForApp(commitAppForHandler(c.handler), false, c.CommitTaskKey())
}

func (c *codingSubAgentCallbacks) CommitWorkspaceRoot() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.projectPath())
}

func (c *codingSubAgentCallbacks) CommitTaskTempDir() string {
	return commitTaskTemp(c.CommitWorkspaceRoot())
}

func (c *codingSubAgentCallbacks) CommitChecklistOpen() bool {
	if c == nil {
		return false
	}
	return strings.TrimSpace(c.OpenChecklistNudge()) != ""
}

func (c *codingSubAgentCallbacks) CommitTaskKey() string {
	if c == nil {
		return ""
	}
	title := ""
	if c.task != nil {
		title = strings.TrimSpace(c.task.Title)
	}
	root := strings.TrimSpace(c.projectPath())
	if root == "" && title == "" {
		return ""
	}
	taskID := title
	if c.subagent != nil && strings.TrimSpace(c.subagent.commitTaskID) != "" {
		taskID = c.subagent.commitTaskID
	}
	return "coding\n" + root + "\n" + taskID
}

func (c *codingSubAgentCallbacks) CommitLookup(fingerprint string) (agent.CommitDecision, bool) {
	if c == nil {
		return "", false
	}
	var app *App
	if c.subagent != nil {
		app = commitAppForHandler(c.subagent.handler)
	}
	return lookupCommitForApp(app, c.CommitTaskKey(), fingerprint)
}

func (c *codingSubAgentCallbacks) CommitStore(fingerprint string, decision agent.CommitDecision) error {
	if c == nil {
		return fmt.Errorf("commit host is nil")
	}
	var app *App
	if c.subagent != nil {
		app = commitAppForHandler(c.subagent.handler)
	}
	return storeCommitForApp(app, c.CommitTaskKey(), fingerprint, decision)
}

func (c *codingSubAgentCallbacks) CommitPrepareAsk(fingerprint string) string {
	if c == nil {
		return ""
	}
	var app *App
	if c.subagent != nil {
		app = commitAppForHandler(c.subagent.handler)
	}
	return prepareCommitAskForApp(app, c.CommitTaskKey(), fingerprint)
}

func (c *codingSubAgentCallbacks) ResolveCommitAsk(toolName, fingerprint, subject string) (agent.CommitDecision, bool) {
	if c == nil || c.subagent == nil {
		return "", false
	}
	if c.subagent.commitAsk != nil {
		return c.subagent.commitAsk(toolName, fingerprint, subject)
	}
	return waitCommitShapeApproval(c.subagent.handler, c.subagent.executionCtx, c.subagent.loopCtx, c.subagent.onProgress, toolName, c.projectPath(), subject, fingerprint)
}

// CommitFullControlGranted lets the gate skip the human-call card while the
// task tab stays on 完全控制. It reads the tier live so a mid-task switch
// applies to the next call.
func (c *codingSubAgentCallbacks) CommitFullControlGranted() bool {
	if c == nil || c.subagent == nil {
		return false
	}
	return codingTaskFullControlGranted(c.subagent.handler, c.subagent.taskPermissionOwnerID())
}

// CommitNoteFullControl records a human-class call the full-control tier let
// through, matching the audit a person's allow answer writes.
func (c *codingSubAgentCallbacks) CommitNoteFullControl(toolName, fingerprint string) {
	if c == nil || c.subagent == nil {
		return
	}
	noteCommitFullControlAudit(c.subagent.handler, toolName, fingerprint, c.projectPath())
}

func (c *codingSubAgentCallbacks) IterationContract() (string, []string, []string) {
	if c == nil {
		return "", nil, nil
	}
	title, description := "", ""
	if c.task != nil {
		title = c.task.Title
		description = c.task.Description
	}
	var app *App
	if c.subagent != nil {
		app = commitAppForHandler(c.subagent.handler)
	}
	return codingContractGoal(title, description), nil, commitLockedNamesForApp(app, c.CommitChecklistOpen(), c.CommitTaskKey())
}

func (c *remoteCodingCallbacks) CommitWorkspaceRoot() string {
	if c == nil || c.agent == nil {
		return ""
	}
	if dir := strings.TrimSpace(c.agent.projectDir); dir != "" {
		return dir
	}
	return strings.TrimSpace(c.agent.workDir)
}

func (c *remoteCodingCallbacks) CommitTaskTempDir() string {
	return commitTaskTemp(c.CommitWorkspaceRoot())
}

func (c *remoteCodingCallbacks) CommitChecklistOpen() bool {
	if c == nil {
		return false
	}
	return strings.TrimSpace(c.OpenChecklistNudge()) != ""
}

func (c *remoteCodingCallbacks) CommitTaskKey() string {
	if c == nil {
		return ""
	}
	root := strings.TrimSpace(c.CommitWorkspaceRoot())
	task := strings.TrimSpace(c.task)
	if root == "" && task == "" {
		return ""
	}
	if c.agent != nil && strings.TrimSpace(c.agent.commitTaskID) != "" {
		task = c.agent.commitTaskID
	}
	return "remote\n" + root + "\n" + task
}

func (c *remoteCodingCallbacks) CommitLookup(fingerprint string) (agent.CommitDecision, bool) {
	if c == nil {
		return "", false
	}
	var app *App
	if c.agent != nil {
		app = commitAppForHandler(c.agent.handler)
	}
	return lookupCommitForApp(app, c.CommitTaskKey(), fingerprint)
}

func (c *remoteCodingCallbacks) CommitStore(fingerprint string, decision agent.CommitDecision) error {
	if c == nil {
		return fmt.Errorf("commit host is nil")
	}
	var app *App
	if c.agent != nil {
		app = commitAppForHandler(c.agent.handler)
	}
	return storeCommitForApp(app, c.CommitTaskKey(), fingerprint, decision)
}

func (c *remoteCodingCallbacks) CommitPrepareAsk(fingerprint string) string {
	if c == nil {
		return ""
	}
	var app *App
	if c.agent != nil {
		app = commitAppForHandler(c.agent.handler)
	}
	return prepareCommitAskForApp(app, c.CommitTaskKey(), fingerprint)
}

func (c *remoteCodingCallbacks) ResolveCommitAsk(toolName, fingerprint, subject string) (agent.CommitDecision, bool) {
	if c == nil || c.agent == nil {
		return "", false
	}
	if c.agent.commitAsk != nil {
		return c.agent.commitAsk(toolName, fingerprint, subject)
	}
	return waitCommitShapeApproval(c.agent.handler, c.agent.executionCtx, c.agent.loopCtx, c.agent.onProgress, toolName, c.CommitWorkspaceRoot(), subject, fingerprint)
}

// CommitFullControlGranted mirrors the local coding host: 完全控制 on the
// task tab runs human-class calls without a card.
func (c *remoteCodingCallbacks) CommitFullControlGranted() bool {
	if c == nil || c.agent == nil {
		return false
	}
	return codingTaskFullControlGranted(c.agent.handler, c.agent.taskPermissionOwnerID())
}

// CommitNoteFullControl mirrors the local coding host's audit entry.
func (c *remoteCodingCallbacks) CommitNoteFullControl(toolName, fingerprint string) {
	if c == nil || c.agent == nil {
		return
	}
	noteCommitFullControlAudit(c.agent.handler, toolName, fingerprint, c.CommitWorkspaceRoot())
}

// noteCommitFullControlAudit records a full-control skip on the same audit
// trail a person's approval answer writes. The call subject stays off the
// entry; the tool name, shape fingerprint, and project scope explain the allow.
func noteCommitFullControlAudit(handler *IMMessageHandler, toolName, fingerprint, projectPath string) {
	recordScopeApprovalAudit(handler, "", ScopeApprovalRequest{
		ToolName:    strings.TrimSpace(toolName),
		ProjectPath: strings.TrimSpace(projectPath),
		Kind:        commitShapeApprovalKind,
		Fingerprint: strings.TrimSpace(fingerprint),
	}, ScopeApprovalFullAccess, "full_control")
}

// waitCommitShapeApproval blocks on the existing coding approval dialog.
// It does not time out and it does not grant path or command trust. No UI
// returns false so the gate keeps today's ask and stores nothing. The gate
// reaches it only when the task tab is not in 完全控制.
func waitCommitShapeApproval(handler *IMMessageHandler, execution context.Context, loopCtx *LoopContext, onProgress func(string), toolName, projectPath, subject, fingerprint string) (agent.CommitDecision, bool) {
	if handler == nil || handler.app == nil {
		return "", false
	}
	if (loopCtx != nil && loopCtx.IsCancelled()) || (execution != nil && execution.Err() != nil) {
		return "", false
	}
	var cancelC <-chan struct{}
	if loopCtx != nil {
		cancelC = loopCtx.CancelC
	}
	var execDone <-chan struct{}
	if execution != nil {
		execDone = execution.Done()
	}
	if cancelC == nil && execDone == nil {
		return "", false
	}
	toolName = strings.TrimSpace(toolName)
	question := agent.CommitAskQuestion(toolName)
	subject = strings.TrimSpace(subject)
	if onProgress != nil {
		notice := question
		if subject != "" {
			notice += "\n" + subject
		}
		onProgress(notice)
	}
	req := ScopeApprovalRequest{
		ToolName:    toolName,
		Path:        subject,
		ProjectPath: strings.TrimSpace(projectPath),
		Kind:        commitShapeApprovalKind,
		Message:     question,
		AutoAllow:   false,
		Fingerprint: strings.TrimSpace(fingerprint),
	}
	responseCh := make(chan ScopeApprovalDecision, 1)
	approvalID := storePendingScopeApproval(handler, req, responseCh)
	emitScopeApprovalEvent(handler.app, approvalID, req)
	var decision ScopeApprovalDecision
	select {
	case decision = <-responseCh:
		recordScopeApprovalAudit(handler, approvalID, req, decision, "user")
	case <-cancelC:
		pendingScopeApprovals.Delete(approvalID)
		recordScopeApprovalAudit(handler, approvalID, req, ScopeApprovalDeny, "cancelled")
		return "", false
	case <-execDone:
		pendingScopeApprovals.Delete(approvalID)
		recordScopeApprovalAudit(handler, approvalID, req, ScopeApprovalDeny, "cancelled")
		return "", false
	}
	switch decision {
	case ScopeApprovalAllowOnce:
		return agent.CommitAllowOnce, true
	case ScopeApprovalAllowTask:
		return agent.CommitAllowTask, true
	case ScopeApprovalDeny:
		return agent.CommitDeny, true
	default:
		return "", false
	}
}

func (c *remoteCodingCallbacks) IterationContract() (string, []string, []string) {
	if c == nil {
		return "", nil, nil
	}
	var app *App
	if c.agent != nil {
		app = commitAppForHandler(c.agent.handler)
	}
	return strings.TrimSpace(c.task), nil, commitLockedNamesForApp(app, c.CommitChecklistOpen(), c.CommitTaskKey())
}

func (c *veAgentCallbacks) CommitWorkspaceRoot() string {
	if c == nil {
		return ""
	}
	dirs := c.getVEAllowedDirectories()
	if len(dirs) == 0 {
		return ""
	}
	return strings.TrimSpace(dirs[0])
}

func (c *veAgentCallbacks) CommitTaskTempDir() string { return "" }

func (c *veAgentCallbacks) CommitChecklistOpen() bool { return false }

func (c *veAgentCallbacks) CommitTaskKey() string {
	if c == nil {
		return ""
	}
	return "ve\n" + strings.TrimSpace(c.sessionID) + "\n" + strings.TrimSpace(c.requestID)
}

func (c *veAgentCallbacks) CommitLookup(fingerprint string) (agent.CommitDecision, bool) {
	if c == nil {
		return "", false
	}
	return lookupCommitForApp(c.app, c.CommitTaskKey(), fingerprint)
}

func (c *veAgentCallbacks) CommitStore(fingerprint string, decision agent.CommitDecision) error {
	if c == nil {
		return fmt.Errorf("commit host is nil")
	}
	return storeCommitForApp(c.app, c.CommitTaskKey(), fingerprint, decision)
}

func (c *veAgentCallbacks) CommitPrepareAsk(fingerprint string) string {
	if c == nil {
		return ""
	}
	return prepareCommitAskForApp(c.app, c.CommitTaskKey(), fingerprint)
}

func (c *veAgentCallbacks) CommitAskPauses() bool { return false }

func (c *veAgentCallbacks) CommitCallSubject(toolName, argsJSON string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "send_file", "send_to_im":
		return veSessionFileSubject(argsJSON), true
	default:
		return "", false
	}
}

func veSessionFileSubject(argsJSON string) string {
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		path, _ = args["file_path"].(string)
		path = strings.TrimSpace(path)
	}
	if path == "" {
		return "当前会话"
	}
	return path + " → 当前会话"
}

func (c *veAgentCallbacks) ResolveCommitAsk(toolName, fingerprint, subject string) (agent.CommitDecision, bool) {
	if c == nil {
		return "", false
	}
	if c.commitAsk != nil {
		return c.commitAsk(toolName, fingerprint, subject)
	}
	if c.app == nil {
		return "", false
	}
	return waitVECommitApproval(c.app, c.ctx, c.sessionID, toolName, subject)
}
