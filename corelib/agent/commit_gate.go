package agent

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// CommitClass is how a tool call sits against the human-approval table.
// Calls that are not in the table stay on today's authorization.
type CommitClass int

const (
	CommitUnclassified CommitClass = iota
	CommitEngineSafe
	CommitHuman
)

// CommitDecision is a person's answer for one call shape.
// Once allows the next call and is then forgotten. Task allows every later
// call of that shape. Deny refuses it without asking again.
type CommitDecision string

const (
	CommitAllowOnce CommitDecision = "once"
	CommitAllowTask CommitDecision = "task"
	CommitDeny      CommitDecision = "deny"
)

// CommitPathClass is the only path fact stored in a fingerprint.
// File bodies, mail bodies, and command text are not.
type CommitPathClass string

const (
	CommitPathNone      CommitPathClass = "none"
	CommitPathWorkspace CommitPathClass = "workspace"
	CommitPathTaskTemp  CommitPathClass = "task_temp"
	CommitPathOutside   CommitPathClass = "outside"
	CommitPathUnknown   CommitPathClass = "unknown"
)

// CommitApprovalContextPrefix marks an ask_user card that is waiting for a
// person to approve one call shape. Display strips it. The host records the
// answer against the task ledger.
const CommitApprovalContextPrefix = "commit-approval;"

// CommitGateHost is optional. A loop that does not implement it keeps today's
// tool authorization. The model cannot mark a call safe: approval lives here.
type CommitGateHost interface {
	CommitWorkspaceRoot() string
	CommitTaskTempDir() string
	CommitChecklistOpen() bool
	CommitTaskKey() string
	CommitLookup(fingerprint string) (CommitDecision, bool)
	CommitStore(fingerprint string, decision CommitDecision) error
	// CommitPrepareAsk records that this fingerprint is waiting for a person
	// and returns the nonce stamped into the card. An empty nonce blocks the
	// call because the approval could not be recorded reliably.
	CommitPrepareAsk(fingerprint string) string
}

// CommitAskResolver is optional. A host with an interactive surface implements
// it so the person answers inside the same tool call: allow runs that call,
// deny is remembered. A false result stores nothing and lets the gate fall
// through to the ask_user pause, which is what a host with no dialog (IM,
// scheduled, headless) still needs. The model cannot supply the decision.
// subject is the call on the card. It is not part of the fingerprint.
type CommitAskResolver interface {
	ResolveCommitAsk(toolName, fingerprint, subject string) (CommitDecision, bool)
}

// CommitAskPauser is optional. False means this host cannot park the loop on
// ask_user. An unresolved resolver then blocks the call instead of asking.
type CommitAskPauser interface {
	CommitAskPauses() bool
}

// CommitSubjectHost is optional. A true result replaces the default card
// subject for hosts whose executor does not use the desktop delivery.
type CommitSubjectHost interface {
	CommitCallSubject(toolName, argsJSON string) (string, bool)
}

// CommitChatBoundHost is optional. True means this call is the live
// current-chat file attach. Its arguments cannot choose another file or
// destination: the invocation is the artifact the plan already bound.
// Treating those arguments as a send would pause the turn, and the approval
// would resume the same name on the legacy tool list, which can send the
// path. The call stays unclassified.
type CommitChatBoundHost interface {
	CommitCallBoundToChat(name, argsJSON string) bool
}

// CommitFullControlHost is optional. True means this task tab is in
// 完全控制: the person already authorized it to run without another card.
// The gate reads it live, so flipping the tier mid-task takes effect on the
// next call. A stored deny and an open checklist still refuse the call.
type CommitFullControlHost interface {
	CommitFullControlGranted() bool
}

// CommitFullControlAuditor is optional. The gate calls it when the full-control
// grant waived a human-class card, so the host can record the allow the same
// way a person's answer is recorded. Keeping it separate leaves the predicate
// above read-only.
type CommitFullControlAuditor interface {
	CommitNoteFullControl(toolName, fingerprint string)
}

// CommitLedger is the in-memory approval record for one task. Hosts keep it
// on the task they already own. It is not a new state file.
type CommitLedger struct {
	mu        sync.Mutex
	decisions map[string]CommitDecision
}

func NewCommitLedger() *CommitLedger {
	return &CommitLedger{decisions: map[string]CommitDecision{}}
}

func (l *CommitLedger) Lookup(fingerprint string) (CommitDecision, bool) {
	if l == nil {
		return "", false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	decision, ok := l.decisions[fingerprint]
	return decision, ok && decision != ""
}

func (l *CommitLedger) Store(fingerprint string, decision CommitDecision) {
	if l == nil || strings.TrimSpace(fingerprint) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.decisions == nil {
		l.decisions = map[string]CommitDecision{}
	}
	if decision == "" {
		delete(l.decisions, fingerprint)
		return
	}
	l.decisions[fingerprint] = decision
}

// DeniedTools lists tool names whose shape was refused. The names are the
// first field of each fingerprint.
func (l *CommitLedger) DeniedTools() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var names []string
	for fingerprint, decision := range l.decisions {
		if decision != CommitDeny {
			continue
		}
		name := commitFingerprintTool(fingerprint)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}
	return names
}

func commitFingerprintTool(fingerprint string) string {
	name, _, _ := strings.Cut(fingerprint, "\n")
	return strings.TrimSpace(name)
}

// HumanCommitToolNames is the set a coding checklist locks. bash itself is
// not locked; git push is.
func HumanCommitToolNames() []string {
	return []string{
		"mailbox_send",
		"im_message",
		"send_file",
		"send_to_im",
		"delete_file",
		"move_file",
		"git push",
	}
}

// ApplyCommitGate runs before a loop tool is dispatched. blocked means the
// call did not run. A missing host keeps today's authorization.
func ApplyCommitGate(cb LoopCallbacks, name, argsJSON string) (ToolExecutionResult, bool) {
	host, ok := cb.(CommitGateHost)
	if !ok || host == nil || cb == nil {
		return ToolExecutionResult{}, false
	}
	class, fingerprint := classifyCommitCall(name, argsJSON, host.CommitWorkspaceRoot(), host.CommitTaskTempDir())
	if class == CommitHuman {
		if bound, ok := cb.(CommitChatBoundHost); ok && bound != nil && bound.CommitCallBoundToChat(name, argsJSON) {
			// The live grant cannot leave this chat. A stored deny or an open
			// checklist belongs to an external send of this name, not to the
			// attach the adapter will actually run.
			return ToolExecutionResult{}, false
		}
	}
	switch class {
	case CommitEngineSafe, CommitUnclassified:
		return ToolExecutionResult{}, false
	}
	toolName := strings.TrimSpace(name)
	fullControl := false
	if full, ok := cb.(CommitFullControlHost); ok && full != nil {
		fullControl = full.CommitFullControlGranted()
	}
	if host.CommitChecklistOpen() {
		// Under 完全控制 there is no person to ask, and naming an approval here
		// would send the model looking for one. The open checklist is the whole
		// reason the call waits.
		if fullControl {
			return ToolExecutionResult{
				Result:  "步骤清单还有未关闭项，执行 " + toolName + " 先不执行。",
				Outcome: ToolExecutionOutcomeError,
			}, true
		}
		return ToolExecutionResult{
			Result:  "步骤清单还有未关闭项，执行 " + toolName + " 缺少人的批准，先不执行。",
			Outcome: ToolExecutionOutcomeError,
		}, true
	}
	decision, found := host.CommitLookup(fingerprint)
	if found && decision == CommitDeny {
		return commitDeniedResult(toolName), true
	}
	if fullControl {
		// 完全控制 already authorized this call shape. Check it before a stored
		// once/task allow so a stale one-call grant is not consumed and no
		// decision is written. Leaving the tier puts the card back.
		if auditor, ok := cb.(CommitFullControlAuditor); ok && auditor != nil {
			auditor.CommitNoteFullControl(toolName, fingerprint)
		}
		return ToolExecutionResult{}, false
	}
	if found && decision == CommitAllowTask {
		return ToolExecutionResult{}, false
	}
	if found && decision == CommitAllowOnce {
		if err := host.CommitStore(fingerprint, ""); err != nil {
			return commitPersistenceResult(toolName), true
		}
		return ToolExecutionResult{}, false
	}
	subject := CommitAskSubject(name, argsJSON)
	if subjects, ok := cb.(CommitSubjectHost); ok && subjects != nil {
		if custom, replaced := subjects.CommitCallSubject(toolName, argsJSON); replaced {
			subject = custom
		}
	}
	if resolver, ok := cb.(CommitAskResolver); ok && resolver != nil {
		decision, resolved := resolver.ResolveCommitAsk(toolName, fingerprint, subject)
		if resolved {
			switch decision {
			case CommitDeny:
				if err := host.CommitStore(fingerprint, CommitDeny); err != nil {
					return commitPersistenceResult(toolName), true
				}
				return commitDeniedResult(toolName), true
			case CommitAllowTask:
				if err := host.CommitStore(fingerprint, CommitAllowTask); err != nil {
					return commitPersistenceResult(toolName), true
				}
				return ToolExecutionResult{}, false
			case CommitAllowOnce:
				return ToolExecutionResult{}, false
			}
		} else if cb.ShouldStop() || !commitAskPauses(cb) {
			return commitUnapprovedResult(toolName), true
		}
	}
	nonce := host.CommitPrepareAsk(fingerprint)
	if strings.TrimSpace(nonce) == "" {
		return commitPersistenceResult(toolName), true
	}
	return commitAskResult(toolName, host.CommitTaskKey(), fingerprint, nonce, subject), true
}

func commitAskPauses(cb LoopCallbacks) bool {
	pauser, ok := cb.(CommitAskPauser)
	if !ok || pauser == nil {
		return true
	}
	return pauser.CommitAskPauses()
}

func commitPersistenceResult(toolName string) ToolExecutionResult {
	return ToolExecutionResult{
		Result:  "执行 " + toolName + " 的审批状态无法可靠保存，先不执行。",
		Outcome: ToolExecutionOutcomeError,
	}
}

func commitDeniedResult(toolName string) ToolExecutionResult {
	return ToolExecutionResult{
		Result:  "执行 " + toolName + " 缺少人的批准，已拒绝，这一任务里不会再问。",
		Outcome: ToolExecutionOutcomeError,
	}
}

func commitUnapprovedResult(toolName string) ToolExecutionResult {
	return ToolExecutionResult{
		Result:  "执行 " + toolName + " 缺少人的批准，先不执行。",
		Outcome: ToolExecutionOutcomeError,
	}
}

// CommitAskQuestion is the sentence a person sees before a human-class call.
// The name is the tool name, not a description of the command. It states that
// nothing has run yet; the caller supplies the choices (buttons in the desktop
// dialog, numbered options on a text channel).
func CommitAskQuestion(toolName string) string {
	return "执行 " + strings.TrimSpace(toolName) + " 需要你的确认，这一步还没有执行。"
}

// CommitAskSubject is the call shown beside that sentence. It is not part of
// the fingerprint. Mail and chat bodies stay off the card. Recipient lists
// follow the shapes those tools actually accept, including a JSON array.
func CommitAskSubject(name, argsJSON string) string {
	args := commitArgs(argsJSON)
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shell", "powershell", "run_terminal", "ssh_bash":
		return oneLineCommitSubject(commitCommandSubject(args))
	case "delete_file", "remove_file":
		return oneLineCommitSubject(firstCommitString(args, "path", "file_path", "target"))
	case "move_file", "rename_file":
		return oneLineCommitSubject(commitArrowSubject(
			firstCommitString(args, "source", "from", "path", "file_path"),
			firstCommitString(args, "destination", "dest", "new_path", "target", "to"),
		))
	case "send_file", "send_to_im":
		return oneLineCommitSubject(commitSendSubject(name, args))
	case "mailbox_send":
		return oneLineCommitSubject(commitMailboxSubject(args))
	case "im_message":
		return oneLineCommitSubject(commitIMSubject(args))
	default:
		return ""
	}
}

func commitCommandSubject(args map[string]interface{}) string {
	command := commitCommandText(args)
	cwd := firstCommitString(args, "working_dir", "cwd", "workdir")
	if command == "" || cwd == "" {
		return command
	}
	return command + " · " + cwd
}

func commitArrowSubject(left, right string) string {
	switch {
	case left != "" && right != "" && left != right:
		return left + " → " + right
	case left != "":
		return left
	default:
		return right
	}
}

func commitSendSubject(name string, args map[string]interface{}) string {
	path := firstCommitString(args, "path", "file_path", "target")
	who := firstCommitString(args, "group_name", "group_id", "user_id")
	place := commitSendPlace(name, args)
	switch {
	case place != "" && who != "" && !strings.EqualFold(place, who):
		who = place + " " + who
	case who == "":
		who = place
	}
	return commitArrowSubject(path, who)
}

// commitSendPlace is the delivery the executor will actually perform.
// send_to_im forces IM. A desktop destination on send_file stays in the chat.
// An exact channel wins over a generic im destination. An unknown destination
// is not shown as a channel.
func commitSendPlace(name string, args map[string]interface{}) string {
	forwards := strings.EqualFold(strings.TrimSpace(name), "send_to_im") || SendFileForwardsToIM(args)
	if !forwards {
		if dest, ok := SendFileDestinationArg(args); ok && SendFileDestinationKind(dest) == SendFileDestDesktop {
			return dest
		}
		return ""
	}
	if channel := firstCommitString(args, "channel"); channel != "" {
		return channel
	}
	if dest, ok := SendFileDestinationArg(args); ok && SendFileDestinationKind(dest) == SendFileDestIM && dest != "im" {
		return dest
	}
	return "im"
}

func commitIMSubject(args map[string]interface{}) string {
	who := firstCommitString(args, "group_name", "group_id", "user_id", "to", "recipient", "chat_id", "conversation_id")
	if channel := firstCommitString(args, "channel"); channel != "" && who != "" && !strings.EqualFold(channel, who) {
		who = channel + " " + who
	}
	shown := commitArrowSubject(firstCommitString(args, "path", "file_path"), who)
	if shown != "" {
		return shown
	}
	action := strings.ToLower(firstCommitString(args, "action"))
	if action == "" || action == "send" || action == "send_file" {
		return ""
	}
	extra := strings.TrimSpace(strings.Join([]string{
		firstCommitString(args, "channel"),
		firstCommitString(args, "query"),
	}, " "))
	if extra == "" {
		return action
	}
	return action + " " + extra
}

func commitMailboxSubject(args map[string]interface{}) string {
	action := strings.ToLower(firstCommitString(args, "action"))
	to := firstCommitString(args, "to")
	cc := firstCommitString(args, "cc")
	bcc := firstCommitString(args, "bcc")
	mailSubject := firstCommitString(args, "subject")
	var parts []string
	switch action {
	case "reply", "forward":
		if uid := firstCommitString(args, "uid"); uid != "" {
			parts = append(parts, action+" "+uid)
		} else {
			parts = append(parts, action)
		}
	}
	if to != "" {
		parts = append(parts, to)
	}
	if cc != "" {
		parts = append(parts, "抄送 "+cc)
	}
	if bcc != "" {
		parts = append(parts, "密送 "+bcc)
	}
	head := strings.Join(parts, " ")
	if mailSubject == "" {
		return head
	}
	if head == "" {
		return mailSubject
	}
	return head + " · " + mailSubject
}

func firstCommitString(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		for name, value := range args {
			if !strings.EqualFold(strings.TrimSpace(name), key) {
				continue
			}
			if text := commitDisplayText(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func commitDisplayText(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			text, ok := item.(string)
			text = strings.TrimSpace(text)
			if ok && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v < math.MinInt64 || v > math.MaxInt64 || v != math.Trunc(v) {
			return ""
		}
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func oneLineCommitSubject(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	const maxRunes = 160
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes-1]) + "…"
}

func commitAskResult(toolName, taskKey, fingerprint, nonce, subject string) ToolExecutionResult {
	question := CommitAskQuestion(toolName)
	if subject != "" {
		question += "\n" + subject
	}
	req := &AskUserRequest{
		Question:  question,
		Options:   []string{"仅这一次", "本任务内允许", "拒绝"},
		InputType: "choice",
		Context:   CommitApprovalContext(taskKey, fingerprint, nonce),
	}
	return ToolExecutionResult{Result: AskUserResultMarker(req), Outcome: ToolExecutionOutcomeError}
}

// CommitApprovalContext packs the task, the fingerprint, and the host nonce
// into an ask_user context. The display hides the prefix. The answer path
// reads it back and records a decision only when the nonce matches the note
// CommitPrepareAsk stored.
func CommitApprovalContext(taskKey, fingerprint, nonce string) string {
	if strings.TrimSpace(fingerprint) == "" || strings.TrimSpace(nonce) == "" {
		return ""
	}
	payload, err := json.Marshal(struct {
		Task  string `json:"task"`
		FP    string `json:"fp"`
		Nonce string `json:"nonce"`
	}{Task: taskKey, FP: fingerprint, Nonce: nonce})
	if err != nil {
		return ""
	}
	return CommitApprovalContextPrefix + base64.RawURLEncoding.EncodeToString(payload) + ";"
}

// ParseCommitApprovalContext reads the ticket a commit ask stamped.
func ParseCommitApprovalContext(context string) (taskKey, fingerprint, nonce string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(context), CommitApprovalContextPrefix)
	if !found {
		return "", "", "", false
	}
	encoded, _, _ := strings.Cut(rest, ";")
	if encoded == "" {
		return "", "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", "", false
	}
	var ticket struct {
		Task  string `json:"task"`
		FP    string `json:"fp"`
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(raw, &ticket); err != nil {
		return "", "", "", false
	}
	if strings.TrimSpace(ticket.FP) == "" {
		return "", "", "", false
	}
	return ticket.Task, ticket.FP, ticket.Nonce, true
}

// ParseCommitApprovalAnswer maps a card choice onto a decision.
// Anything else is not an approval and not a denial.
func ParseCommitApprovalAnswer(answer string) (CommitDecision, bool) {
	switch strings.TrimSpace(answer) {
	case "仅这一次":
		return CommitAllowOnce, true
	case "本任务内允许":
		return CommitAllowTask, true
	case "拒绝":
		return CommitDeny, true
	default:
		return "", false
	}
}

func classifyCommitCall(name, argsJSON, workspace, taskTemp string) (CommitClass, string) {
	name = strings.TrimSpace(name)
	args := commitArgs(argsJSON)
	pathClass := commitCallPathClass(name, args, workspace, taskTemp)
	fingerprint := commitFingerprint(name, args, pathClass)
	if commitWriteInTaskTemp(name, pathClass) {
		return CommitEngineSafe, fingerprint
	}
	if commitCreateIfAbsent(name, args, pathClass, workspace) {
		return CommitEngineSafe, fingerprint
	}
	if commitCallNeedsHuman(name, args, pathClass) {
		return CommitHuman, fingerprint
	}
	return CommitUnclassified, fingerprint
}

func commitArgs(argsJSON string) map[string]interface{} {
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		return map[string]interface{}{}
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args == nil {
		return map[string]interface{}{}
	}
	return args
}

func commitFingerprint(name string, args map[string]interface{}, pathClass CommitPathClass) string {
	keys := make([]string, 0, len(args))
	seen := map[string]bool{}
	for key := range args {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if pathClass == "" {
		pathClass = CommitPathNone
	}
	return strings.ToLower(strings.TrimSpace(name)) + "\n" + strings.Join(keys, ",") + "\n" + string(pathClass)
}

func commitCallNeedsHuman(name string, args map[string]interface{}, pathClass CommitPathClass) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mailbox_send", "im_message", "send_to_im":
		return true
	case "send_file":
		// Empty arguments and a desktop destination stay in this chat. A path,
		// a recipient, or any other destination is a send for a host that can
		// actually aim the tool. A live current-chat grant cannot: ApplyCommitGate
		// leaves that call unclassified even when the model added those fields.
		return sendFileCommitNeedsHuman(args)
	case "delete_file", "remove_file", "move_file", "rename_file":
		return pathClass == CommitPathOutside || pathClass == CommitPathUnknown
	}
	if commitCommandTool(name) && commandPushesGit(commitCommandText(args)) {
		return true
	}
	return false
}

func commitWriteInTaskTemp(name string, pathClass CommitPathClass) bool {
	if pathClass != CommitPathTaskTemp {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write_file", "create_file", "edit_file", "append_file":
		return true
	default:
		return false
	}
}

func commitCreateIfAbsent(name string, args map[string]interface{}, pathClass CommitPathClass, workspace string) bool {
	if pathClass != CommitPathWorkspace {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write_file", "create_file", "append_file":
	default:
		return false
	}
	info, err := os.Stat(strings.TrimSpace(workspace))
	if err != nil || info == nil || !info.IsDir() {
		return false
	}
	target := commitPrimaryPath(name, args, workspace)
	if target == "" {
		return false
	}
	_, err = os.Stat(target)
	return os.IsNotExist(err)
}

// sendFileCommitNeedsHuman reports a send_file the model aimed somewhere.
// An empty call, or one whose destination is only this chat, stays on the
// host adapter. Unknown destinations stay human: they are not this chat.
func sendFileCommitNeedsHuman(args map[string]interface{}) bool {
	if SendFileForwardsToIM(args) {
		return true
	}
	if commitStringArg(args, "path") != "" || commitStringArg(args, "file_path") != "" || commitStringArg(args, "target") != "" {
		return true
	}
	if firstCommitString(args, "group_name", "group_id", "user_id", "channel") != "" {
		return true
	}
	if dest, ok := SendFileDestinationArg(args); ok && SendFileDestinationKind(dest) != SendFileDestDesktop {
		return true
	}
	return false
}

func commitCommandTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shell", "powershell", "run_terminal", "ssh_bash":
		return true
	default:
		return false
	}
}

func commitCommandText(args map[string]interface{}) string {
	for _, key := range []string{"command", "cmd", "script"} {
		if text := commitStringArg(args, key); text != "" {
			return text
		}
	}
	return ""
}

func commitStringArg(args map[string]interface{}, key string) string {
	for name, value := range args {
		if !strings.EqualFold(strings.TrimSpace(name), key) {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return ""
		}
		return strings.TrimSpace(text)
	}
	return ""
}

func commitCallPathClass(name string, args map[string]interface{}, workspace, taskTemp string) CommitPathClass {
	paths := commitRelevantPaths(name, args, workspace)
	if len(paths) == 0 {
		if commitPathTool(name) {
			return CommitPathUnknown
		}
		return CommitPathNone
	}
	worst := CommitPathWorkspace
	saw := false
	for _, path := range paths {
		class := classifyCommitPath(path, workspace, taskTemp)
		if !saw || commitPathWorse(class, worst) {
			worst = class
			saw = true
		}
	}
	if !saw {
		return CommitPathNone
	}
	return worst
}

func commitPathWorse(candidate, current CommitPathClass) bool {
	return commitPathRank(candidate) > commitPathRank(current)
}

func commitPathRank(class CommitPathClass) int {
	switch class {
	case CommitPathUnknown:
		return 4
	case CommitPathOutside:
		return 3
	case CommitPathWorkspace:
		return 2
	case CommitPathTaskTemp:
		return 1
	default:
		return 0
	}
}

func commitPathTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "delete_file", "remove_file", "move_file", "rename_file",
		"write_file", "create_file", "edit_file", "append_file", "send_file", "send_to_im":
		return true
	default:
		return false
	}
}

func commitRelevantPaths(name string, args map[string]interface{}, workspace string) []string {
	keys := commitPathKeys(name)
	if len(keys) == 0 {
		return nil
	}
	var paths []string
	for _, key := range keys {
		if text := commitStringArg(args, key); text != "" {
			if resolved := resolveCommitPath(workspace, text); resolved != "" {
				paths = append(paths, resolved)
			}
		}
	}
	return paths
}

func commitPathKeys(name string) []string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "delete_file", "remove_file":
		return []string{"path", "file_path", "target"}
	case "move_file", "rename_file":
		return []string{"destination", "dest", "to", "target", "new_path", "path"}
	case "write_file", "create_file", "edit_file", "append_file", "send_file", "send_to_im":
		return []string{"path", "file_path", "target"}
	case "bash", "shell", "powershell", "run_terminal", "ssh_bash":
		return []string{"working_dir", "cwd", "workdir"}
	default:
		return nil
	}
}

func commitPrimaryPath(name string, args map[string]interface{}, workspace string) string {
	paths := commitRelevantPaths(name, args, workspace)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func resolveCommitPath(workspace, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(workspace, path))
}

func classifyCommitPath(path, workspace, taskTemp string) CommitPathClass {
	path = strings.TrimSpace(path)
	if path == "" {
		return CommitPathUnknown
	}
	if taskTemp != "" && pathWithin(taskTemp, path) {
		return CommitPathTaskTemp
	}
	if workspace != "" && pathWithin(workspace, path) {
		return CommitPathWorkspace
	}
	if workspace == "" && taskTemp == "" {
		return CommitPathUnknown
	}
	return CommitPathOutside
}

func pathWithin(root, target string) bool {
	root = canonicalCommitPath(root)
	target = canonicalCommitPath(target)
	if root == "" || root == "." || target == "" {
		return false
	}
	rootKey, targetKey := root, target
	if runtime.GOOS == "windows" {
		rootKey = strings.ToLower(rootKey)
		targetKey = strings.ToLower(targetKey)
	}
	if targetKey == rootKey {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(rootKey, sep) {
		rootKey += sep
	}
	return strings.HasPrefix(targetKey, rootKey)
}

func canonicalCommitPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	var suffix []string
	parent := path
	for {
		next := filepath.Dir(parent)
		if next == parent {
			return path
		}
		suffix = append([]string{filepath.Base(parent)}, suffix...)
		parent = next
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Clean(filepath.Join(parts...))
		}
	}
}

const maxGitPushParseDepth = 16

func commandPushesGit(command string) bool {
	return commandPushesGitAt(command, 0)
}

func commandPushesGitAt(command string, depth int) bool {
	return commandPushesGitDefs(command, depth, nil)
}

type psDefs struct {
	funcs   map[string]string
	aliases map[string]string
}

func commandPushesGitDefs(command string, depth int, defs *psDefs) bool {
	if depth > maxGitPushParseDepth {
		return true
	}
	if defs == nil {
		defs = &psDefs{funcs: map[string]string{}, aliases: map[string]string{}}
	}
	segments := splitShellSegments(command)
	for i, segment := range segments {
		// A definition does not run. A later command word with that name does.
		if name, body, ok := definedFunction(segment.text); ok {
			defs.funcs[name] = body
		}
		if name, target, ok := definedAlias(segment.text); ok {
			defs.aliases[name] = target
		}
		if segmentPushesGitAt(segment.text, depth) {
			return true
		}
		if segment.piped && i > 0 && iexPipelinePushesGit(segments[i-1].text, segment.text, depth) {
			return true
		}
		if body, ok := invokedPSCommand(segment.text, defs); ok && commandPushesGitDefs(body, depth+1, defs) {
			return true
		}
	}
	return false
}

func definedFunction(segment string) (string, string, bool) {
	segment = stripShellComment(segment)
	keyword, rest, ok := cutPSCommand(segment)
	if !ok {
		return "", "", false
	}
	switch strings.ToLower(keyword) {
	case "function", "filter", "workflow":
	default:
		return "", "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", "", false
	}
	i := 0
	for i < len(rest) && rest[i] != ' ' && rest[i] != '\t' && rest[i] != '(' && rest[i] != '{' && rest[i] != '\n' && rest[i] != '\r' {
		i++
	}
	raw := rest[:i]
	if colon := strings.LastIndex(raw, ":"); colon >= 0 {
		raw = raw[colon+1:]
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "", "", false
	}
	open := strings.IndexByte(rest, '{')
	if open < 0 {
		return "", "", false
	}
	body, _, ok := braceGroup(rest, open)
	if !ok {
		return "", "", false
	}
	return raw, body, true
}

func definedAlias(segment string) (string, string, bool) {
	tokens := stripCallOperators(flattenPowerShellArrays(stripEnvAssignments(shellTokens(stripShellComment(segment)))))
	if len(tokens) == 0 {
		return "", "", false
	}
	switch commandBaseName(tokens[0]) {
	case "sal", "set-alias", "new-alias":
	default:
		return "", "", false
	}
	var name, value string
	var positional []string
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if !strings.HasPrefix(tok, "-") {
			positional = append(positional, tok)
			continue
		}
		key, attachedVal, attached := powershellFlag(tok)
		switch {
		case prefixAtLeast(key, "name", 2):
			name = takeAliasValue(attachedVal, attached, tokens, &i)
		case prefixAtLeast(key, "value", 3):
			value = takeAliasValue(attachedVal, attached, tokens, &i)
		case prefixAtLeast(key, "description", 1), prefixAtLeast(key, "option", 2), prefixAtLeast(key, "scope", 2),
			powershellCommonTakesValue(key):
			_ = takeAliasValue(attachedVal, attached, tokens, &i)
		}
	}
	if name == "" && len(positional) > 0 {
		name = positional[0]
	}
	if value == "" && len(positional) > 1 {
		value = positional[1]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	value = strings.TrimSpace(value)
	if name == "" || value == "" {
		return "", "", false
	}
	return name, value, true
}

func takeAliasValue(attachedVal string, attached bool, tokens []string, i *int) string {
	if attached {
		return attachedVal
	}
	if *i+1 < len(tokens) && !strings.HasPrefix(tokens[*i+1], "-") {
		*i++
		return tokens[*i]
	}
	return ""
}

func invokedPSCommand(segment string, defs *psDefs) (string, bool) {
	if defs == nil {
		return "", false
	}
	tokens := stripCallOperators(flattenPowerShellArrays(stripEnvAssignments(shellTokens(segment))))
	if len(tokens) == 0 {
		return "", false
	}
	word := strings.ToLower(commandBaseName(tokens[0]))
	if target, ok := defs.aliases[word]; ok {
		rest := strings.TrimSpace(strings.Join(tokens[1:], " "))
		if rest == "" {
			return target, true
		}
		return target + " " + rest, true
	}
	if body, ok := defs.funcs[word]; ok {
		return body, true
	}
	return "", false
}

// cmdForPushesGit catches cmd's FOR, whose DO body runs, and whose
// FOR /F ('command') clause runs that command. PowerShell's for ($i=...) is left
// to the brace parser.
func cmdForPushesGit(segment string, depth int) bool {
	segment = strings.TrimSpace(segment)
	if len(segment) < 4 || !strings.EqualFold(segment[:3], "for") || !isCmdWordBreak(segment[3]) {
		return false
	}
	return cmdForAt(segment, 0, depth)
}

func isCmdWordBreak(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '/' || ch == '(' || ch == '\n' || ch == ';'
}

func cmdForAt(command string, start int, depth int) bool {
	i := skipSpaceIdx(command, start+3)
	if i >= len(command) || command[i] == '(' {
		return false
	}
	forF := false
	usebackq := false
	if command[i] == '/' || command[i] == '-' {
		flag, next := readCmdWord(command, i)
		i = skipSpaceIdx(command, next)
		if strings.EqualFold(flag, "/f") || strings.EqualFold(flag, "-f") {
			forF = true
			if i < len(command) && command[i] == '"' {
				opt, next := readQuotedCmd(command, i)
				if strings.Contains(strings.ToLower(opt), "usebackq") {
					usebackq = true
				}
				i = skipSpaceIdx(command, next)
			}
		}
	}
	if i < len(command) && command[i] == '%' {
		_, i = readCmdWord(command, i)
		i = skipSpaceIdx(command, i)
	}
	inCommand := ""
	if hasCmdWord(command, i, "in") {
		i = skipSpaceIdx(command, i+2)
		if i < len(command) && command[i] == '(' {
			inner, next := readCmdParen(command, i)
			i = skipSpaceIdx(command, next)
			inCommand = cmdForInCommand(inner, forF, usebackq)
		}
	}
	doCommand := ""
	if hasCmdWord(command, i, "do") {
		doCommand = strings.TrimSpace(command[i+2:])
	}
	if inCommand != "" && commandPushesGitAt(inCommand, depth+1) {
		return true
	}
	return doCommand != "" && commandPushesGitAt(doCommand, depth+1)
}

func cmdForInCommand(inner string, forF, usebackq bool) string {
	if !forF {
		return ""
	}
	if text, ok := unwrapCmdQuoted(inner, '\''); ok {
		return text
	}
	if usebackq {
		if text, ok := unwrapCmdQuoted(inner, '`'); ok {
			return text
		}
	}
	return ""
}

func unwrapCmdQuoted(inner string, quote byte) (string, bool) {
	inner = strings.TrimSpace(inner)
	if len(inner) < 2 || inner[0] != quote || inner[len(inner)-1] != quote {
		return "", false
	}
	for i := 1; i < len(inner)-1; i++ {
		if inner[i] == quote {
			return "", false
		}
	}
	return inner[1 : len(inner)-1], true
}

func hasCmdWord(command string, i int, word string) bool {
	if i < 0 || i+len(word) > len(command) || !strings.EqualFold(command[i:i+len(word)], word) {
		return false
	}
	end := i + len(word)
	return end == len(command) || isCmdWordBreak(command[end]) || command[end] == ' ' || command[end] == '\t'
}

func skipSpaceIdx(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func readCmdWord(s string, i int) (string, int) {
	start := i
	for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '(' && s[i] != ')' && s[i] != '"' && s[i] != '\'' {
		i++
	}
	return s[start:i], i
}

func readQuotedCmd(s string, i int) (string, int) {
	if i >= len(s) {
		return "", i
	}
	quote := s[i]
	i++
	start := i
	for i < len(s) && s[i] != quote {
		i++
	}
	inner := s[start:i]
	if i < len(s) {
		i++
	}
	return inner, i
}

func readCmdParen(s string, i int) (string, int) {
	depth := 0
	var quote byte
	start := i + 1
	for ; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote = ch
			continue
		}
		if ch == '(' {
			depth++
			continue
		}
		if ch == ')' {
			depth--
			if depth == 0 {
				return s[start:i], i + 1
			}
		}
	}
	return s[start:], len(s)
}

func segmentPushesGitAt(segment string, depth int) bool {
	if depth > maxGitPushParseDepth {
		return true
	}
	segment = stripShellComment(segment)
	if cmdForPushesGit(segment, depth) {
		return true
	}
	if iexHerePushesGit(segment, depth) {
		return true
	}
	if commandSubstitutionPushesGit(segment, depth) {
		return true
	}
	return argvPushesGit(shellTokens(segment), depth)
}

// argvPushesGit keeps wrapper arguments as tokens. Joining them back into a
// command string splits a quoted script such as bash -c 'git push'.
func argvPushesGit(tokens []string, depth int) bool {
	if argvPushesGitOnce(tokens, depth) {
		return true
	}
	// PowerShell keeps backslashes. Bash still treats \t as t, so gi\t push
	// is checked again with those escapes removed.
	unescaped, changed := bashUnescapeTokens(tokens)
	if !changed {
		return false
	}
	return argvPushesGitOnce(unescaped, depth)
}

func argvPushesGitOnce(tokens []string, depth int) bool {
	if depth > maxGitPushParseDepth {
		return true
	}
	tokens = foldPowerShellArgs(stripCallOperators(flattenPowerShellArrays(stripEnvAssignments(tokens))))
	if len(tokens) == 0 || strings.HasPrefix(tokens[0], "#") {
		return false
	}
	if gitPushArgv(tokens) {
		return true
	}
	base := commandBaseName(tokens[0])
	switch base {
	case "eval":
		return len(tokens) > 1 && commandPushesGitAt(strings.Join(tokens[1:], " "), depth+1)
	case "env":
		rest, script := consumeEnvArgs(tokens[1:])
		if script != "" && commandPushesGitAt(script, depth+1) {
			return true
		}
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "xargs":
		rest := consumeXargsArgs(tokens[1:])
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "sudo":
		rest := consumeSudoArgs(tokens[1:])
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "time":
		rest := consumeFlagArgs(tokens[1:], map[string]bool{"-f": true, "-o": true, "--format": true, "--output": true})
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "nice":
		rest := consumeFlagArgs(tokens[1:], map[string]bool{"-n": true, "--adjustment": true})
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "stdbuf":
		rest := consumeStdbufArgs(tokens[1:])
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "timeout":
		rest := consumeTimeoutArgs(tokens[1:])
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "command":
		rest := consumeCommandArgs(tokens[1:])
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "exec":
		rest := consumeFlagArgs(tokens[1:], map[string]bool{"-a": true})
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "nohup":
		rest := consumeFlagArgs(tokens[1:], nil)
		return len(rest) > 0 && argvPushesGit(rest, depth+1)
	case "iex", "invoke-expression":
		// PowerShell runs the bash tool on Windows. iex is that shell's eval.
		// A literal script is classified. A variable or subexpression is not
		// visible here, so it asks.
		rest := consumeIexArgs(tokens[1:])
		if len(rest) == 0 {
			return false
		}
		script := strings.TrimSpace(strings.Join(rest, " "))
		if strings.HasPrefix(script, "$") || strings.HasPrefix(script, "(") {
			return true
		}
		return commandPushesGitAt(script, depth+1)
	case "call":
		return len(tokens) > 1 && argvPushesGit(tokens[1:], depth+1)
	case "start", "start-process", "saps":
		// PowerShell aliases start to Start-Process. cmd's start runs the same program.
		return startProcessPushesGit(tokens[1:], depth)
	case "wsl":
		return wslPushesGit(tokens[1:], depth)
	case "bash", "sh", "dash", "zsh", "powershell", "pwsh", "cmd":
		return shellDashCPushesGit(tokens, depth)
	default:
		// $git, $env:ComSpec, and %COMSPEC% expand on the Windows host.
		if indirectCommandWord(tokens[0]) {
			return indirectCommandPushesGit(tokens, depth)
		}
		return false
	}
}

func powershellCallAmp(command string, i int, buffered string) bool {
	if strings.TrimSpace(buffered) == "" {
		return true
	}
	j := i + 1
	for j < len(command) && (command[j] == ' ' || command[j] == '\t') {
		j++
	}
	return j < len(command) && command[j] == '{'
}

func stripCallOperators(tokens []string) []string {
	for len(tokens) > 0 && (tokens[0] == "&" || tokens[0] == ".") {
		tokens = tokens[1:]
	}
	return tokens
}

func indirectCommandWord(token string) bool {
	token = strings.TrimSpace(token)
	if strings.Contains(token, "$") || strings.Contains(token, "`") {
		return true
	}
	return len(token) >= 3 && token[0] == '%' && strings.Contains(token[1:], "%")
}

func indirectCommandPushesGit(tokens []string, depth int) bool {
	if gitPushArgv(append([]string{"git"}, tokens[1:]...)) {
		return true
	}
	for _, shell := range []string{"cmd", "powershell", "bash"} {
		if shellDashCPushesGit(append([]string{shell}, tokens[1:]...), depth) {
			return true
		}
	}
	return false
}

func shellDashCPushesGit(tokens []string, depth int) bool {
	if len(tokens) == 0 {
		return false
	}
	base := commandBaseName(tokens[0])
	switch base {
	case "cmd":
		// cmd /c takes the rest of the line. Carets and /v:on are cmd syntax,
		// so they are decoded only inside that script.
		return cmdScriptPushesGit(tokens, depth)
	case "powershell", "pwsh":
		return powershellScriptPushesGit(tokens, depth)
	default:
		return bashDashCPushesGit(tokens, depth)
	}
}

func cmdScriptPushesGit(tokens []string, depth int) bool {
	delayed := false
	for i := 1; i < len(tokens); i++ {
		flag := strings.ToLower(tokens[i])
		switch flag {
		case "/v:on", "-v:on":
			delayed = true
			continue
		case "/v:off", "-v:off":
			delayed = false
			continue
		}
		switch flag {
		case "/c", "/k", "-c", "-k":
		default:
			continue
		}
		if i+1 >= len(tokens) {
			return false
		}
		script := unescapeCmdCarets(strings.Join(tokens[i+1:], " "))
		script = expandCmdPercents(script)
		if delayed {
			script = markCmdDelayedVars(script)
		}
		return commandPushesGitAt(script, depth+1)
	}
	return false
}

// unescapeCmdCarets applies cmd's caret escape. ^^ is one caret, and a caret
// before a newline continues the line. PowerShell does not treat ^ this way.
func unescapeCmdCarets(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '^' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		next := s[i+1]
		if next == '\r' {
			i++
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			continue
		}
		if next == '\n' {
			i++
			continue
		}
		b.WriteByte(next)
		i++
	}
	return b.String()
}

// markCmdDelayedVars rewrites !name! as $name so an unknown cmd variable in
// the git subcommand asks, the same way git $cmd does. Delayed expansion is
// off unless cmd was started with /v:on.
func markCmdDelayedVars(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '!' {
			b.WriteByte(s[i])
			continue
		}
		j := i + 1
		for j < len(s) && isCmdVarChar(s[j]) {
			j++
		}
		if j > i+1 && j < len(s) && s[j] == '!' {
			b.WriteByte('$')
			b.WriteString(s[i+1 : j])
			i = j
			continue
		}
		b.WriteByte('!')
	}
	return b.String()
}

func isCmdVarChar(ch byte) bool {
	return ch == '_' || (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
}

// expandCmdPercents removes %NAME:~start,0%, which is empty whenever NAME is
// set, and rewrites every other %NAME% as $NAME. A bare %i in FOR is not a
// closed expansion and is left alone. PowerShell does not expand %NAME%.
func expandCmdPercents(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		inner, end, ok := cmdPercentAt(s, i)
		if !ok {
			b.WriteByte('%')
			continue
		}
		if cmdPercentIsEmpty(inner) {
			i = end
			continue
		}
		b.WriteByte('$')
		b.WriteString(cmdPercentBase(inner))
		i = end
	}
	return b.String()
}

func cmdPercentAt(s string, i int) (string, int, bool) {
	j := i + 1
	start := j
	if j >= len(s) || !isCmdVarChar(s[j]) {
		return "", 0, false
	}
	for j < len(s) && isCmdVarChar(s[j]) {
		j++
	}
	if j < len(s) && s[j] == ':' {
		if j+1 >= len(s) || s[j+1] != '~' {
			return "", 0, false
		}
		j += 2
		next := skipCmdInt(s, j)
		if next < 0 || next >= len(s) || s[next] != ',' {
			return "", 0, false
		}
		next = skipCmdInt(s, next+1)
		if next < 0 {
			return "", 0, false
		}
		j = next
	}
	if j >= len(s) || s[j] != '%' || j == start {
		return "", 0, false
	}
	return s[start:j], j, true
}

func skipCmdInt(s string, j int) int {
	if j < len(s) && s[j] == '-' {
		j++
	}
	start := j
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == start {
		return -1
	}
	return j
}

func cmdPercentIsEmpty(inner string) bool {
	_, spec, ok := strings.Cut(inner, ":~")
	if !ok {
		return false
	}
	_, length, ok := strings.Cut(spec, ",")
	return ok && length == "0"
}

func cmdPercentBase(inner string) string {
	if name, _, ok := strings.Cut(inner, ":"); ok {
		return name
	}
	return inner
}

func bashDashCPushesGit(tokens []string, depth int) bool {
	for i := 1; i < len(tokens); i++ {
		flag := tokens[i]
		script := ""
		switch {
		case flag == "-c" || flag == "/c":
			if i+1 < len(tokens) {
				script = tokens[i+1]
			}
		case strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") && strings.Contains(flag, "c") && !strings.Contains(flag, "="):
			// bash -lc and bash -ic take the command as the next token.
			if i+1 < len(tokens) {
				script = tokens[i+1]
			}
		}
		if script != "" && commandPushesGitAt(script, depth+1) {
			return true
		}
	}
	return false
}

func powershellScriptPushesGit(tokens []string, depth int) bool {
	for i := 1; i < len(tokens); i++ {
		name, value, attached := powershellFlag(tokens[i])
		switch powershellParam(name) {
		case "command":
			var script string
			if attached {
				script = value
				if i+1 < len(tokens) {
					if script != "" {
						script += " "
					}
					script += strings.Join(tokens[i+1:], " ")
				}
			} else if i+1 < len(tokens) {
				script = strings.Join(tokens[i+1:], " ")
			}
			if strings.TrimSpace(script) == "" {
				return false
			}
			return commandPushesGitAt(script, depth+1)
		case "encoded":
			encoded := value
			if !attached {
				if i+1 >= len(tokens) {
					return true
				}
				encoded = tokens[i+1]
			}
			text, ok := decodePowerShellEncodedCommand(encoded)
			if !ok {
				return true
			}
			return commandPushesGitAt(text, depth+1)
		}
	}
	return false
}

func powershellFlag(flag string) (name, value string, attached bool) {
	flag = strings.TrimLeft(strings.TrimSpace(flag), "-/")
	name, value, attached = strings.Cut(flag, ":")
	return strings.ToLower(name), value, attached
}

func powershellParam(flag string) string {
	flag = strings.TrimLeft(strings.ToLower(strings.TrimSpace(flag)), "-/")
	switch flag {
	case "c", "co", "com", "comm", "comma", "comman", "command":
		return "command"
	case "e", "en", "enc", "enco", "encod", "encode", "encoded", "encodedc", "encodedco", "encodedcom", "encodedcomm", "encodedcomma", "encodedcomman", "encodedcommand":
		return "encoded"
	default:
		return ""
	}
}

func decodePowerShellEncodedCommand(encoded string) (string, bool) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return "", false
		}
	}
	if len(raw) == 0 || len(raw)%2 != 0 {
		return "", false
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[i*2]) | uint16(raw[i*2+1])<<8
	}
	return strings.TrimPrefix(string(utf16.Decode(units)), "\uFEFF"), true
}

func consumeEnvArgs(args []string) (rest []string, splitScript string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--":
			return stripEnvAssignments(args[i+1:]), ""
		case "-i", "--ignore-environment", "-0", "--null":
			continue
		case "-u", "--unset", "-C", "--chdir":
			i++
			continue
		case "-S", "--split-string":
			script := ""
			if i+1 < len(args) {
				script = args[i+1]
				i++
			}
			return append(shellTokens(script), args[i+1:]...), script
		}
		if strings.HasPrefix(arg, "--split-string=") {
			script := strings.TrimPrefix(arg, "--split-string=")
			return append(shellTokens(script), args[i+1:]...), script
		}
		if strings.HasPrefix(arg, "--unset=") || strings.HasPrefix(arg, "--chdir=") {
			continue
		}
		if eq := strings.IndexByte(arg, '='); eq > 0 && !strings.HasPrefix(arg, "-") && !strings.ContainsAny(arg[:eq], "/\\") {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return args[i:], ""
	}
	return nil, ""
}

func consumeSudoArgs(args []string) []string {
	return consumeFlagArgs(args, map[string]bool{
		"-u": true, "-g": true, "-h": true, "-p": true, "-U": true, "-C": true, "-D": true, "-T": true, "-R": true,
		"--user": true, "--group": true, "--host": true, "--prompt": true, "--chdir": true, "--role": true,
		"--other-user": true, "--close-from": true, "--command-timeout": true,
	})
}

func consumeXargsArgs(args []string) []string {
	return consumeFlagArgs(args, map[string]bool{
		"-a": true, "-E": true, "-I": true, "-L": true, "-n": true, "-P": true, "-s": true, "-d": true,
		"--arg-file": true, "--eof": true, "--replace": true, "--max-lines": true, "--max-args": true,
		"--max-procs": true, "--max-chars": true, "--delimiter": true,
	})
}

func consumeStdbufArgs(args []string) []string {
	return consumeFlagArgs(args, map[string]bool{
		"-i": true, "-o": true, "-e": true, "--input": true, "--output": true, "--error": true,
	})
}

func consumeIexArgs(args []string) []string {
	for len(args) > 0 {
		token := args[0]
		if !isPowerShellParamToken(token) {
			return args
		}
		name, value, attached := powershellFlag(token)
		if powershellParam(name) == "command" {
			if !attached {
				return args[1:]
			}
			rest := make([]string, 0, len(args))
			if value != "" {
				rest = append(rest, value)
			}
			return append(rest, args[1:]...)
		}
		args = args[1:]
		if !attached && powershellCommonTakesValue(name) && len(args) > 0 && !isPowerShellParamToken(args[0]) {
			args = args[1:]
		}
	}
	return nil
}

func powershellFlagName(flag string) string {
	name, _, _ := powershellFlag(flag)
	return name
}

// startProcessPushesGit reads Start-Process the way PowerShell binds it.
// Named parameters may appear in any order. ArgumentList stops at the next
// Start-Process parameter, so a later -FilePath is still the program.
// A directory or other value parameter is not a git argument. When the program
// is itself a shell, that shell is classified by the same rules as a direct call.
func startProcessPushesGit(args []string, depth int) bool {
	args = consumeCmdStartFlags(args)
	file := ""
	var named []string
	namedSet := false
	var positionals []string
	for i := 0; i < len(args); i++ {
		kind, value, attached := startProcessParam(args[i])
		switch kind {
		case "file":
			if attached {
				file = value
			} else if next, ok := takeStartValue(args, &i); ok {
				file = next
			}
		case "args":
			namedSet = true
			if attached {
				named = append(named, splitPowerShellList(value)...)
				continue
			}
			for i+1 < len(args) && startProcessParamKind(args[i+1]) == "" {
				i++
				named = append(named, splitPowerShellList(args[i])...)
			}
		case "value":
			if !attached {
				_, _ = takeStartValue(args, &i)
			}
		case "switch":
		default:
			positionals = append(positionals, args[i])
		}
	}
	if file == "" && len(positionals) > 0 {
		file = positionals[0]
		positionals = positionals[1:]
	}
	argList := named
	if !namedSet {
		for _, part := range positionals {
			argList = append(argList, splitPowerShellList(part)...)
		}
	}
	if strings.TrimSpace(file) == "" {
		return false
	}
	return argvPushesGit(append([]string{file}, argList...), depth+1)
}

func startProcessParamKind(token string) string {
	kind, _, _ := startProcessParam(token)
	return kind
}

func startProcessParam(token string) (kind, value string, attached bool) {
	if !isPowerShellParamToken(token) {
		return "", "", false
	}
	name, value, attached := powershellFlag(token)
	return startProcessKind(name), value, attached
}

func isPowerShellParamToken(token string) bool {
	return token != "" && !strings.HasPrefix(token, "--") && (strings.HasPrefix(token, "-") || strings.HasPrefix(token, "/"))
}

func takeStartValue(args []string, i *int) (string, bool) {
	if *i+1 >= len(args) || startProcessParamKind(args[*i+1]) != "" {
		return "", false
	}
	*i++
	return args[*i], true
}

func startProcessKind(name string) string {
	switch {
	case name == "pspath" || prefixAtLeast(name, "filepath", 1):
		return "file"
	case name == "args" || prefixAtLeast(name, "argumentlist", 1):
		return "args"
	case prefixAtLeast(name, "workingdirectory", 2),
		prefixAtLeast(name, "credential", 2),
		name == "verb",
		prefixAtLeast(name, "windowstyle", 2),
		prefixAtLeast(name, "redirectstandarderror", 2),
		prefixAtLeast(name, "redirectstandardinput", 2),
		prefixAtLeast(name, "redirectstandardoutput", 2),
		powershellCommonTakesValue(name):
		return "value"
	case prefixAtLeast(name, "loaduserprofile", 2),
		prefixAtLeast(name, "nonewwindow", 2),
		prefixAtLeast(name, "passthru", 2),
		prefixAtLeast(name, "wait", 3),
		prefixAtLeast(name, "usenewenvironment", 1),
		prefixAtLeast(name, "verbose", 5),
		prefixAtLeast(name, "debug", 1),
		prefixAtLeast(name, "whatif", 2),
		prefixAtLeast(name, "confirm", 2):
		return "switch"
	default:
		return ""
	}
}

func powershellCommonTakesValue(name string) bool {
	switch {
	case prefixAtLeast(name, "erroraction", 3), prefixAtLeast(name, "errorvariable", 3),
		prefixAtLeast(name, "warningaction", 3), prefixAtLeast(name, "warningvariable", 3),
		prefixAtLeast(name, "informationaction", 2), prefixAtLeast(name, "informationvariable", 2),
		prefixAtLeast(name, "outvariable", 3), prefixAtLeast(name, "outbuffer", 3),
		prefixAtLeast(name, "pipelinevariable", 2),
		prefixAtLeast(name, "progressaction", 2):
		return true
	default:
		return false
	}
}

func prefixAtLeast(name, canonical string, min int) bool {
	return len(name) >= min && strings.HasPrefix(canonical, name)
}

// consumeCmdStartFlags skips the switches of cmd.exe's start. /D, /NODE, and
// /AFFINITY take the next word. A one-word quoted title is indistinguishable
// from the program name after quotes are removed, so it is left in place.
func consumeCmdStartFlags(args []string) []string {
	for len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "/d", "/node", "/affinity":
			if len(args) < 2 {
				return nil
			}
			args = args[2:]
		case "/i", "/min", "/max", "/separate", "/shared", "/low", "/normal", "/high",
			"/realtime", "/abovenormal", "/belownormal", "/wait", "/b", "/w":
			args = args[1:]
		default:
			return args
		}
	}
	return args
}

func splitPowerShellList(value string) []string {
	if !strings.Contains(value, ",") {
		return []string{value}
	}
	var parts []string
	var b strings.Builder
	var quote byte
	paren := 0
	flush := func() {
		part := strings.TrimSpace(b.String())
		b.Reset()
		if part != "" {
			parts = append(parts, part)
		}
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			b.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			b.WriteByte(ch)
		case '(':
			paren++
			b.WriteByte(ch)
		case ')':
			if paren > 0 {
				paren--
			}
			b.WriteByte(ch)
		case ',':
			if paren == 0 {
				flush()
				continue
			}
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}
	flush()
	if len(parts) == 0 {
		return []string{value}
	}
	return parts
}

// flattenPowerShellArrays turns @('push','origin') into the words PowerShell
// passes to a native program. A bare @name stays intact so the command slot
// can ask instead of guessing.
func flattenPowerShellArrays(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, flattenPowerShellWord(arg, 0)...)
	}
	return out
}

func flattenPowerShellWord(token string, depth int) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if depth > 8 || len(token) < 3 || token[0] != '@' || token[1] != '(' || token[len(token)-1] != ')' {
		return []string{token}
	}
	inner := strings.TrimSpace(token[2 : len(token)-1])
	if inner == "" {
		return nil
	}
	parts := splitPowerShellList(inner)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		// Fold before stripping quotes. 'rev-'+'parse' starts and ends with a
		// quote, and peeling those first would leave a broken expression.
		part = strings.TrimSpace(part)
		if folded, ok := foldPSArg(part); ok {
			part = folded
		} else {
			part = stripMatchingQuotes(part)
		}
		out = append(out, flattenPowerShellWord(part, depth+1)...)
	}
	return out
}

// foldPowerShellArgs evaluates the PowerShell argument expressions that a
// native program never sees: ('pu'+'sh'), ("{0}" -f 'push'), and (Get-Date).
// A value that is not an expression stays as tokenized.
func foldPowerShellArgs(tokens []string) []string {
	if len(tokens) == 0 {
		return tokens
	}
	out := make([]string, len(tokens))
	for i, token := range tokens {
		if folded, ok := foldPSArg(token); ok {
			out[i] = folded
		} else {
			out[i] = token
		}
	}
	return out
}

// foldPSArg reports whether token is a PowerShell expression. A static
// concat or {n} format becomes its text. Anything else in that shape becomes
// $expr so the existing subcommand and indirect-command rules ask.
func foldPSArg(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if body, ok := foldPSHereString(token); ok {
		return body, true
	}
	if !psArgIsExpression(token) {
		return "", false
	}
	if lit, ok := foldPSExpr(token, 0); ok {
		return lit, true
	}
	return "$expr", true
}

func psArgIsExpression(token string) bool {
	if isWrappedParen(token) {
		return true
	}
	// Bare a+b and a bare -f flag are not expressions. A quote is required
	// before + counts, and -f needs both a format string and arguments.
	// 'Xpush'.Substring(1) is a member call. Unquoted text with a dot is not.
	if end, ok := psMemberChainEnd(token, 0); ok && end == len(token)-1 {
		return true
	}
	if hasFormatExpr(token) {
		return true
	}
	return hasQuoteChar(token) && hasPlusOutsideQuotes(token)
}

func isWrappedParen(token string) bool {
	if len(token) < 2 || token[0] != '(' || token[len(token)-1] != ')' {
		return false
	}
	_, end, ok := parenthesizedCommand(token, 0)
	return ok && end == len(token)-1
}

func hasQuoteChar(s string) bool {
	return strings.ContainsAny(s, `"'`)
}

func foldPSExpr(s string, depth int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || depth > 8 {
		return "", false
	}
	if isWrappedParen(s) {
		return foldPSExpr(s[1:len(s)-1], depth+1)
	}
	if lit, ok := psQuotedLiteral(s); ok {
		return lit, true
	}
	if lit, ok := psFormatExpr(s, depth); ok {
		return lit, true
	}
	if hasPlusOutsideQuotes(s) {
		return psConcatExpr(s, depth)
	}
	if lit, ok := psMemberExpr(s, depth); ok {
		return lit, true
	}
	return "", false
}

// psMemberChainEnd reports the last byte of a quoted literal followed by
// .Member calls. Argument mode evaluates that chain; a later + does not.
func psMemberChainEnd(s string, start int) (int, bool) {
	end, ok := closePSQuote(s, start)
	if !ok {
		return 0, false
	}
	i := end + 1
	saw := false
	for i < len(s) && s[i] == '.' {
		j := i + 1
		if j >= len(s) || !psIdentStart(s[j]) {
			break
		}
		j++
		for j < len(s) && psIdentByte(s[j]) {
			j++
		}
		if j >= len(s) || s[j] != '(' {
			break
		}
		_, pend, found := parenthesizedCommand(s, j)
		if !found {
			break
		}
		i = pend + 1
		saw = true
	}
	if !saw {
		return 0, false
	}
	return i - 1, true
}

func closePSQuote(s string, i int) (int, bool) {
	if i < 0 || i >= len(s) || (s[i] != '\'' && s[i] != '"') {
		return 0, false
	}
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		if s[j] != quote {
			continue
		}
		if quote == '\'' && j+1 < len(s) && s[j+1] == '\'' {
			j++
			continue
		}
		return j, true
	}
	return 0, false
}

func psIdentStart(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func psMemberExpr(s string, depth int) (string, bool) {
	recv, rest, ok := takePSQuotedReceiver(s)
	if !ok || rest == "" || rest[0] != '.' {
		return "", false
	}
	lit := recv
	for rest != "" {
		if rest[0] != '.' {
			return "", false
		}
		name, args, called, next, ok := takePSMemberCall(rest, depth)
		if !ok {
			return "", false
		}
		if !called {
			// 'XXX'.Length is the UTF-16 length for an ASCII string.
			if !strings.EqualFold(name, "length") || !psASCII(lit) {
				return "", false
			}
			lit = strconv.Itoa(len(lit))
			rest = strings.TrimSpace(next)
			continue
		}
		applied, ok := applyPSStringMember(lit, name, args)
		if !ok {
			return "", false
		}
		lit = applied
		rest = strings.TrimSpace(next)
	}
	return lit, true
}

func takePSQuotedReceiver(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	end, ok := closePSQuote(s, 0)
	if !ok {
		return "", "", false
	}
	lit, ok := psQuotedLiteral(s[:end+1])
	if !ok {
		return "", "", false
	}
	return lit, strings.TrimSpace(s[end+1:]), true
}

func takePSMemberCall(s string, depth int) (name string, args []string, called bool, next string, ok bool) {
	if s == "" || s[0] != '.' {
		return "", nil, false, "", false
	}
	j := 1
	if j >= len(s) || !psIdentStart(s[j]) {
		return "", nil, false, "", false
	}
	start := j
	j++
	for j < len(s) && psIdentByte(s[j]) {
		j++
	}
	name = s[start:j]
	if j >= len(s) || s[j] != '(' {
		return name, nil, false, strings.TrimSpace(s[j:]), true
	}
	inner, pend, found := parenthesizedCommand(s, j)
	if !found {
		return "", nil, false, "", false
	}
	parsed, good := splitMemberArgs(inner, depth)
	if !good {
		return "", nil, false, "", false
	}
	return name, parsed, true, strings.TrimSpace(s[pend+1:]), true
}

func splitMemberArgs(inner string, depth int) ([]string, bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil, true
	}
	parts := splitPSOn(inner, ',')
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		if _, ok := parsePSInt(part); ok {
			out = append(out, part)
			continue
		}
		lit, ok := foldPSExpr(part, depth+1)
		if !ok {
			return nil, false
		}
		out = append(out, lit)
	}
	return out, true
}

func parsePSInt(s string) (int, bool) {
	if s == "" || s == "-" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if i == 0 && s[i] == '-' {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func psASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// applyPSStringMember evaluates the string methods argument mode runs before
// git sees the word. Unknown methods fail so the subcommand asks.
func applyPSStringMember(recv, name string, args []string) (string, bool) {
	switch strings.ToLower(name) {
	case "replace":
		if len(args) != 2 || args[0] == "" {
			return "", false
		}
		return strings.ReplaceAll(recv, args[0], args[1]), true
	case "substring":
		if !psASCII(recv) {
			return "", false
		}
		if len(args) == 1 {
			start, ok := parsePSInt(args[0])
			if !ok || start < 0 || start > len(recv) {
				return "", false
			}
			return recv[start:], true
		}
		if len(args) != 2 {
			return "", false
		}
		start, ok1 := parsePSInt(args[0])
		count, ok2 := parsePSInt(args[1])
		if !ok1 || !ok2 || start < 0 || count < 0 || start > len(recv) || count > len(recv)-start {
			return "", false
		}
		return recv[start : start+count], true
	case "remove":
		if !psASCII(recv) {
			return "", false
		}
		if len(args) == 1 {
			start, ok := parsePSInt(args[0])
			if !ok || start < 0 || start > len(recv) {
				return "", false
			}
			return recv[:start], true
		}
		if len(args) != 2 {
			return "", false
		}
		start, ok1 := parsePSInt(args[0])
		count, ok2 := parsePSInt(args[1])
		if !ok1 || !ok2 || start < 0 || count < 0 || start > len(recv) || count > len(recv)-start {
			return "", false
		}
		return recv[:start] + recv[start+count:], true
	case "insert":
		if len(args) != 2 || !psASCII(recv) {
			return "", false
		}
		start, ok := parsePSInt(args[0])
		if !ok || start < 0 || start > len(recv) {
			return "", false
		}
		return recv[:start] + args[1] + recv[start:], true
	case "tolower", "toupper":
		if len(args) != 0 {
			return "", false
		}
		if strings.ToLower(name) == "tolower" {
			return strings.ToLower(recv), true
		}
		return strings.ToUpper(recv), true
	case "trim":
		if len(args) != 0 {
			return "", false
		}
		return strings.TrimSpace(recv), true
	case "trimstart":
		if len(args) != 0 {
			return "", false
		}
		return strings.TrimLeftFunc(recv, unicode.IsSpace), true
	case "trimend":
		if len(args) != 0 {
			return "", false
		}
		return strings.TrimRightFunc(recv, unicode.IsSpace), true
	default:
		return "", false
	}
}

func psQuotedLiteral(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	if s[0] == '\'' {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			if s[i] != '\'' {
				b.WriteByte(s[i])
				continue
			}
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			if i == len(s)-1 {
				return b.String(), true
			}
			return "", false
		}
		return "", false
	}
	if s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	inner := s[1 : len(s)-1]
	// An expandable string is not a static argument. A nested quote means
	// this is not one literal either.
	if strings.ContainsAny(inner, "$`\"") {
		return "", false
	}
	return inner, true
}

func psFormatExpr(s string, depth int) (string, bool) {
	idx, ok := findFormatOp(s)
	if !ok {
		return "", false
	}
	left := strings.TrimSpace(s[:idx])
	right := strings.TrimSpace(s[idx+2:])
	if left == "" || right == "" {
		return "", false
	}
	format, ok := foldPSExpr(left, depth+1)
	if !ok {
		return "", false
	}
	parts := splitPowerShellList(right)
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		lit, ok := foldPSExpr(part, depth+1)
		if !ok {
			return "", false
		}
		args = append(args, lit)
	}
	return applyPSFormat(format, args)
}

func hasFormatExpr(s string) bool {
	idx, ok := findFormatOp(s)
	if !ok {
		return false
	}
	return strings.TrimSpace(s[:idx]) != "" && strings.TrimSpace(s[idx+2:]) != ""
}

func findFormatOp(s string) (int, bool) {
	depth := 0
	found := -1
	psOutsideQuotes(s, func(i int) bool {
		switch s[i] {
		case '(':
			depth++
			return true
		case ')':
			if depth > 0 {
				depth--
			}
			return true
		}
		if depth != 0 || s[i] != '-' || i+1 >= len(s) {
			return true
		}
		if s[i+1] != 'f' && s[i+1] != 'F' {
			return true
		}
		if i+2 < len(s) && psIdentByte(s[i+2]) {
			return true
		}
		if !psBoundaryBefore(s, i) {
			return true
		}
		found = i
		return false
	})
	return found, found >= 0
}

func psBoundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	switch s[i-1] {
	case ' ', '\t', '"', '\'', '(', ')':
		return true
	default:
		return false
	}
}

func psIdentByte(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func hasPlusOutsideQuotes(s string) bool {
	found := false
	depth := 0
	psOutsideQuotes(s, func(i int) bool {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '+':
			if depth == 0 {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func psConcatExpr(s string, depth int) (string, bool) {
	parts := splitPSOn(s, '+')
	if len(parts) < 2 {
		return "", false
	}
	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", false
		}
		lit, ok := foldPSExpr(part, depth+1)
		if !ok {
			return "", false
		}
		b.WriteString(lit)
	}
	return b.String(), true
}

func splitPSOn(s string, sep byte) []string {
	var parts []string
	depth := 0
	last := 0
	psOutsideQuotes(s, func(i int) bool {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if s[i] == sep && depth == 0 {
				parts = append(parts, s[last:i])
				last = i + 1
			}
		}
		return true
	})
	parts = append(parts, s[last:])
	return parts
}

// psOutsideQuotes visits bytes that are not inside quotes. A doubled ” stays
// inside a single-quoted span.
func psOutsideQuotes(s string, visit func(i int) bool) {
	quote := byte(0)
	for i := 0; i < len(s); {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i += 2
					continue
				}
				quote = 0
			}
			i++
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			i++
			continue
		}
		if !visit(i) {
			return
		}
		i++
	}
}

// applyPSFormat replaces {n}. {{ is a literal brace. Alignment and format
// specifiers are not static, so they fail closed.
func applyPSFormat(format string, args []string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] == '{' {
			if i+1 < len(format) && format[i+1] == '{' {
				b.WriteByte('{')
				i++
				continue
			}
			j := i + 1
			if j >= len(format) || format[j] < '0' || format[j] > '9' {
				return "", false
			}
			n := 0
			digits := 0
			for j < len(format) && format[j] >= '0' && format[j] <= '9' {
				digits++
				if digits > 9 {
					return "", false
				}
				n = n*10 + int(format[j]-'0')
				j++
			}
			if j >= len(format) || format[j] != '}' || n >= len(args) {
				return "", false
			}
			b.WriteString(args[n])
			i = j
			continue
		}
		if format[i] == '}' {
			if i+1 < len(format) && format[i+1] == '}' {
				b.WriteByte('}')
				i++
				continue
			}
			return "", false
		}
		b.WriteByte(format[i])
	}
	return b.String(), true
}

func stripMatchingQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func wslPushesGit(args []string, depth int) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return i+1 < len(args) && argvPushesGit(args[i+1:], depth+1)
		}
		name := arg
		inline := ""
		hasInline := false
		if strings.HasPrefix(arg, "--") {
			name, inline, hasInline = strings.Cut(arg, "=")
		}
		switch strings.ToLower(name) {
		case "-e", "--exec":
			rest := args[i+1:]
			if hasInline {
				rest = append([]string{inline}, rest...)
			}
			return len(rest) > 0 && argvPushesGit(rest, depth+1)
		case "-d", "--distribution", "-u", "--user", "--cd", "--shell-type":
			if !hasInline {
				i++
			}
			continue
		case "--system":
			continue
		case "--help", "-h", "-?", "-l", "--list", "--status", "--install", "--update", "--shutdown",
			"--terminate", "-t", "--unregister", "--export", "--import", "--import-in-place",
			"--set-default", "-s", "--set-default-version", "--set-version", "--mount", "--unmount",
			"--manage", "--debug-shell", "--version":
			return false
		}
		if strings.HasPrefix(arg, "-") {
			return true
		}
		return argvPushesGit(args[i:], depth+1)
	}
	return false
}

func consumeCommandArgs(args []string) []string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--":
			return args[i+1:]
		case "-p":
			continue
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			// -v and -V describe the command. They do not run it.
			if strings.ContainsAny(arg, "vV") {
				return nil
			}
			continue
		}
		return args[i:]
	}
	return nil
}

func consumeTimeoutArgs(args []string) []string {
	rest := consumeFlagArgs(args, map[string]bool{
		"-k": true, "--kill-after": true, "-s": true, "--signal": true,
	})
	if len(rest) > 0 && rest[0] != "" && rest[0][0] >= '0' && rest[0][0] <= '9' {
		rest = rest[1:]
	}
	return rest
}

func consumeFlagArgs(args []string, takesValue map[string]bool) []string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return args[i+1:]
		}
		if !strings.HasPrefix(arg, "-") {
			return args[i:]
		}
		if strings.Contains(arg, "=") {
			continue
		}
		if takesValue[arg] {
			i++
		}
	}
	return nil
}

func stripShellComment(segment string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(segment); i++ {
		ch := segment[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '#' && (i == 0 || segment[i-1] == ' ' || segment[i-1] == '\t') {
			return segment[:i]
		}
	}
	return segment
}

func commandSubstitutionPushesGit(command string, depth int) bool {
	return shellSyntaxPushesGit(command, depth, true)
}

// shellSyntaxPushesGit walks substitutions and, when compound is set, subshells
// and brace groups. Arithmetic $((...)) and ((...)) do not run their text as a
// command, so those bodies are scanned only for real substitutions.
func shellSyntaxPushesGit(command string, depth int, compound bool) bool {
	if depth > maxGitPushParseDepth {
		return true
	}
	var quote byte
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else if ch == '$' && quote == '"' && i+1 < len(command) && command[i+1] == '(' {
				if pushes, end, ok := dollarParenPushesGit(command, i, depth); ok && pushes {
					return true
				} else if ok {
					i = end
				}
			} else if ch == '`' && quote == '"' {
				if script, end, ok := backtickCommand(command, i); ok && commandPushesGitAt(script, depth+1) {
					return true
				} else if ok {
					i = end
				}
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if (ch == '<' || ch == '>') && i+1 < len(command) && command[i+1] == '(' {
			if script, end, ok := parenthesizedCommand(command, i+1); ok && commandPushesGitAt(script, depth+1) {
				return true
			} else if ok {
				i = end
			}
			continue
		}
		if ch == '$' && i+1 < len(command) && command[i+1] == '(' {
			if pushes, end, ok := dollarParenPushesGit(command, i, depth); ok && pushes {
				return true
			} else if ok {
				i = end
			}
			continue
		}
		if ch == '`' {
			if script, end, ok := backtickCommand(command, i); ok && commandPushesGitAt(script, depth+1) {
				return true
			} else if ok {
				i = end
			}
			continue
		}
		if !compound {
			continue
		}
		if ch == '(' {
			if i+1 < len(command) && command[i+1] == '(' {
				if script, end, ok := parenthesizedCommand(command, i); ok && shellSyntaxPushesGit(script, depth+1, false) {
					return true
				} else if ok {
					i = end
				}
				continue
			}
			if script, end, ok := parenthesizedCommand(command, i); ok && commandPushesGitAt(script, depth+1) {
				return true
			} else if ok {
				i = end
			}
			continue
		}
		if isBraceGroupOpen(command, i) {
			if script, end, ok := braceGroup(command, i); ok {
				// A bare PowerShell block is a value. An invoked block, and a bash
				// { cmd; } group, run. Nested call operators inside a value still count.
				// switch (1) { 1 {git push} } runs the clause, not the label text.
				if braceBodyRuns(command, i) {
					pushes := false
					if statementKeyword(command, i) == "switch" {
						pushes = switchClausesPushGit(script, depth+1)
					} else {
						pushes = commandPushesGitAt(script, depth+1)
					}
					if pushes {
						return true
					}
				} else if shellSyntaxPushesGit(script, depth+1, true) {
					return true
				}
				i = end
			}
		}
	}
	return false
}

func dollarParenPushesGit(command string, dollar int, depth int) (bool, int, bool) {
	open := dollar + 1
	script, end, ok := parenthesizedCommand(command, open)
	if !ok {
		return false, 0, false
	}
	if open+1 < len(command) && command[open+1] == '(' {
		return shellSyntaxPushesGit(script, depth+1, false), end, true
	}
	return commandPushesGitAt(script, depth+1), end, true
}

func backtickCommand(command string, open int) (string, int, bool) {
	for i := open + 1; i < len(command); i++ {
		if command[i] == '\\' && i+1 < len(command) {
			i++
			continue
		}
		if command[i] == '`' {
			return command[open+1 : i], i, true
		}
	}
	return "", 0, false
}

func parenthesizedCommand(command string, open int) (string, int, bool) {
	depth := 1
	var quote byte
	for i := open + 1; i < len(command); i++ {
		ch := command[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\\' && i+1 < len(command) {
			i++
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return command[open+1 : i], i, true
			}
		}
	}
	return "", 0, false
}

func isBraceGroupOpen(command string, i int) bool {
	if !braceOpens(command, i) {
		return false
	}
	_, ok := matchingBrace(command, i)
	return ok
}

// braceOpens is the structural check. matchingBrace decides whether a close exists,
// so an unclosed "{foo; git push" still splits on the semicolon.
func braceOpens(command string, i int) bool {
	if i < 0 || i >= len(command) || command[i] != '{' {
		return false
	}
	if i > 0 && command[i-1] == '$' {
		return false
	}
	if i == 0 || isShellBoundary(command[i-1]) || command[i-1] == ')' {
		return true
	}
	return powershellInvokesBlock(command, i)
}

func isBraceGroupClose(command string, i int) bool {
	if i <= 0 || i >= len(command) || command[i] != '}' {
		return false
	}
	return isShellBoundary(command[i-1])
}

func isShellSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n'
}

func isShellBoundary(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', ';', '|', '&', '(', '{':
		return true
	default:
		return false
	}
}

func braceGroup(command string, open int) (string, int, bool) {
	end, ok := matchingBrace(command, open)
	if !ok {
		return "", 0, false
	}
	return command[open+1 : end], end, true
}

func matchingBrace(command string, open int) (int, bool) {
	depth := 1
	vars := 0
	var quote byte
	for i := open + 1; i < len(command); i++ {
		ch := command[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\\' && i+1 < len(command) {
			i++
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		switch ch {
		case '{':
			if i > 0 && command[i-1] == '$' {
				vars++
			} else if braceOpens(command, i) {
				depth++
			}
		case '}':
			if vars > 0 {
				vars--
				continue
			}
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// switchClausesPushGit runs each switch clause. The label is not a command,
// and a clause written as 1 {git push} has no space after the brace.
func switchClausesPushGit(body string, depth int) bool {
	if depth > maxGitPushParseDepth {
		return true
	}
	for i := 0; i < len(body); i++ {
		if !isBraceGroupOpen(body, i) {
			continue
		}
		script, end, ok := braceGroup(body, i)
		if !ok {
			return false
		}
		if commandPushesGitAt(script, depth+1) {
			return true
		}
		i = end
	}
	return false
}

func braceBodyRuns(command string, open int) bool {
	switch statementKeyword(command, open) {
	case "function", "filter", "workflow":
		// The body runs only when the function is called.
		return false
	case "switch":
		// switch (1) { 1 {git push} } runs the matching clause, even with no space after {.
		return true
	}
	if powershellInvokesBlock(command, open) {
		return true
	}
	// Bash runs { cmd; } when the braces are separate words.
	if open+1 < len(command) && isShellSpace(command[open+1]) {
		if end, ok := matchingBrace(command, open); ok && isBraceGroupClose(command, end) {
			return true
		}
	}
	return false
}

func powershellInvokesBlock(command string, open int) bool {
	j := open - 1
	for j >= 0 && isShellSpace(command[j]) {
		j--
	}
	if j < 0 {
		return false
	}
	if command[j] == ')' {
		switch statementKeyword(command, open) {
		case "function", "filter", "workflow":
			return false
		default:
			return true
		}
	}
	if command[j] == '&' || command[j] == '.' || command[j] == '%' || command[j] == '?' {
		return j == 0 || isShellBoundary(command[j-1]) || command[j-1] == ')'
	}
	end := j + 1
	for j >= 0 && !isShellSpace(command[j]) && !isShellBoundary(command[j]) && command[j] != ')' && command[j] != '(' {
		j--
	}
	token := command[j+1 : end]
	switch strings.ToLower(token) {
	case "&", ".", "%", "?", "foreach", "foreach-object", "where", "where-object",
		"try", "catch", "finally", "else", "elseif", "do", "trap",
		"invoke-command", "icm":
		return true
	}
	if strings.HasPrefix(token, "-") || strings.HasPrefix(token, "/") {
		if powershellParam(token) == "command" {
			return true
		}
		name := strings.TrimLeft(strings.ToLower(token), "-/")
		switch name {
		case "scriptblock", "process", "begin", "end", "action", "initializationscript", "finally":
			return true
		}
	}
	return false
}

func statementKeyword(command string, open int) string {
	j := open - 1
	for j >= 0 && command[j] != ';' && command[j] != '\n' && command[j] != '|' && command[j] != '&' {
		j--
	}
	j++
	for j < open && isShellSpace(command[j]) {
		j++
	}
	end := j
	for end < open {
		ch := command[end]
		if ch == ' ' || ch == '\t' || ch == '(' || ch == ';' {
			break
		}
		end++
	}
	if end <= j {
		return ""
	}
	return strings.ToLower(command[j:end])
}

func gitPushArgv(args []string) bool {
	return gitPushArgvHops(args, 0)
}

func gitPushArgvHops(args []string, hops int) bool {
	if hops > 8 {
		return true
	}
	if len(args) == 0 {
		return false
	}
	if commandBaseName(args[0]) != "git" {
		return false
	}
	aliases := map[string]string{}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			// git -- push still runs push. A later word is the command, not an option.
			if i+1 >= len(args) {
				return false
			}
			return resolveGitAlias(args[i+1], args[i+2:], aliases, hops)
		}
		if arg == "push" {
			return true
		}
		if strings.HasPrefix(arg, "-") {
			if arg == "-c" || strings.HasPrefix(arg, "-c=") {
				val := ""
				if strings.HasPrefix(arg, "-c=") {
					val = strings.TrimPrefix(arg, "-c=")
				} else if i+1 < len(args) {
					i++
					val = args[i]
				}
				if name, body, ok := gitConfigAlias(val); ok {
					aliases[name] = body
				}
				continue
			}
			if (strings.HasPrefix(arg, "-C") || strings.HasPrefix(arg, "-c")) && len(arg) > 2 {
				continue
			}
			if commitFlagTakesValue(arg) && !strings.Contains(arg, "=") {
				i++
			}
			continue
		}
		return resolveGitAlias(arg, args[i+1:], aliases, hops)
	}
	return false
}

func resolveGitAlias(word string, rest []string, aliases map[string]string, hops int) bool {
	if hops > 8 {
		return true
	}
	if word == "push" {
		return true
	}
	// $cmd, `cmd`, or a PowerShell splat in the command position can expand to push.
	if strings.Contains(word, "$") || strings.Contains(word, "`") || strings.HasPrefix(word, "@") {
		return true
	}
	val, ok := aliases[strings.ToLower(word)]
	if !ok {
		return false
	}
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, "!") {
		return commandPushesGit(strings.TrimSpace(val[1:]))
	}
	fields := strings.Fields(val)
	if len(fields) == 0 {
		return false
	}
	followed := append(append([]string{}, fields[1:]...), rest...)
	return resolveGitAlias(fields[0], followed, aliases, hops+1)
}

func gitConfigAlias(val string) (string, string, bool) {
	val = strings.TrimSpace(val)
	const prefix = "alias."
	if len(val) <= len(prefix) || !strings.EqualFold(val[:len(prefix)], prefix) {
		return "", "", false
	}
	rest := val[len(prefix):]
	eq := strings.IndexByte(rest, '=')
	if eq <= 0 {
		return "", "", false
	}
	return strings.ToLower(rest[:eq]), rest[eq+1:], true
}

func commandBaseName(path string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	switch filepath.Ext(base) {
	case ".exe", ".cmd", ".bat", ".com":
		return strings.TrimSuffix(base, filepath.Ext(base))
	default:
		return base
	}
}

func commitFlagTakesValue(arg string) bool {
	switch arg {
	case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--config-env", "--super-prefix", "--attr-source":
		return true
	default:
		return false
	}
}

func stripEnvAssignments(args []string) []string {
	i := 0
	for i < len(args) {
		arg := args[i]
		eq := strings.IndexByte(arg, '=')
		if eq <= 0 || strings.HasPrefix(arg, "-") || strings.ContainsAny(arg[:eq], "/\\") {
			break
		}
		i++
	}
	return args[i:]
}

type shellPiece struct {
	text  string
	piped bool
}

func splitShellSegments(command string) []shellPiece {
	var segments []shellPiece
	var b strings.Builder
	quote := byte(0)
	paren, brace, tick, vars := 0, 0, 0, 0
	piped := false
	flush := func(followPipe bool) {
		segment := strings.TrimSpace(b.String())
		b.Reset()
		if segment != "" {
			segments = append(segments, shellPiece{text: segment, piped: piped})
		}
		piped = followPipe
	}
	grouped := func() bool { return paren > 0 || brace > 0 || tick > 0 }
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if quote != 0 {
			b.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\\' {
			b.WriteByte(ch)
			if i+1 < len(command) {
				i++
				b.WriteByte(command[i])
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			b.WriteByte(ch)
		case '(':
			paren++
			b.WriteByte(ch)
		case ')':
			if paren > 0 {
				paren--
			}
			b.WriteByte(ch)
		case '{':
			if isBraceGroupOpen(command, i) {
				brace++
			} else if i > 0 && command[i-1] == '$' {
				vars++
			}
			b.WriteByte(ch)
		case '}':
			if vars > 0 {
				vars--
			} else if brace > 0 {
				brace--
			}
			b.WriteByte(ch)
		case '`':
			if tick == 0 {
				tick = 1
			} else {
				tick = 0
			}
			b.WriteByte(ch)
		case '\n', ';':
			if grouped() {
				b.WriteByte(ch)
			} else {
				flush(false)
			}
		case '|':
			if grouped() {
				b.WriteByte(ch)
			} else if i+1 < len(command) && command[i+1] == '|' {
				// || is OR. It does not feed the next command to a pipeline.
				flush(false)
				i++
			} else {
				flush(true)
			}
		case '&':
			if grouped() {
				b.WriteByte(ch)
				if i+1 < len(command) && command[i+1] == '&' {
					i++
					b.WriteByte(command[i])
				}
			} else if i+1 < len(command) && command[i+1] == '&' {
				flush(false)
				i++
			} else if powershellCallAmp(command, i, b.String()) {
				// PowerShell's & calls the next command. Bash's trailing & still splits.
				b.WriteByte('&')
			} else {
				flush(false)
			}
		default:
			b.WriteByte(ch)
		}
	}
	flush(false)
	return segments
}

func iexPipelinePushesGit(prev, segment string, depth int) bool {
	segment = stripShellComment(segment)
	prev = stripShellComment(prev)
	name, rest, ok := cutPSCommand(segment)
	if !ok || !isIexCommand(commandBaseName(name)) {
		return false
	}
	// An explicit script wins. PowerShell does not also bind pipeline input.
	if len(consumeIexArgs(shellTokens(rest))) > 0 {
		return false
	}
	if body, literal, ok := staticPSCode(prev); ok {
		if !literal && psExpandable(body) {
			return true
		}
		return commandPushesGitAt(body, depth+1)
	}
	// Get-Content, a variable, or any other upstream output is the script.
	return true
}

func iexHerePushesGit(segment string, depth int) bool {
	name, rest, ok := cutPSCommand(segment)
	if !ok || !isIexCommand(commandBaseName(name)) {
		return false
	}
	body, literal, ok := unwrapPSHereString(strings.TrimSpace(rest))
	if !ok {
		return false
	}
	if !literal && psExpandable(body) {
		return true
	}
	return commandPushesGitAt(body, depth+1)
}

func isIexCommand(name string) bool {
	switch strings.ToLower(name) {
	case "iex", "invoke-expression":
		return true
	default:
		return false
	}
}

func cutPSCommand(segment string) (string, string, bool) {
	s := strings.TrimSpace(segment)
	if s == "" {
		return "", "", false
	}
	i := 0
	for i < len(s) && !isShellSpace(s[i]) {
		i++
	}
	return s[:i], strings.TrimSpace(s[i:]), true
}

func staticPSCode(segment string) (string, bool, bool) {
	s := strings.TrimSpace(segment)
	if body, literal, ok := unwrapPSHereString(s); ok {
		return body, literal, true
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' && strings.Count(s, "'") == 2 {
		return s[1 : len(s)-1], true, true
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' && strings.Count(s, "\"") == 2 {
		return s[1 : len(s)-1], false, true
	}
	return "", false, false
}

// foldPSHereString reduces a here-string argument to the text git receives.
// A double-quoted body that still contains $ or a backtick stays dynamic.
func foldPSHereString(token string) (string, bool) {
	token = strings.TrimSpace(token)
	body, literal, ok := unwrapPSHereString(token)
	if !ok || !psHereStringClosed(token) {
		return "", false
	}
	if !literal && psExpandable(body) {
		return "$expr", true
	}
	return body, true
}

func psHereStringClosed(token string) bool {
	if strings.HasPrefix(token, "@'") {
		return strings.HasSuffix(token, "'@")
	}
	if strings.HasPrefix(token, "@\"") {
		return strings.HasSuffix(token, "\"@")
	}
	return false
}

// psHereStringEnd is the last byte of a here-string opened at start.
// The closer must be at the beginning of a line, and the opener must end
// with a newline.
func psHereStringEnd(s string, start int) (int, bool) {
	var close string
	if strings.HasPrefix(s[start:], "@'") {
		close = "'@"
	} else if strings.HasPrefix(s[start:], "@\"") {
		close = "\"@"
	} else {
		return 0, false
	}
	i := start + 2
	if strings.HasPrefix(s[i:], "\r\n") {
		i += 2
	} else if i < len(s) && s[i] == '\n' {
		i++
	} else {
		return 0, false
	}
	for i < len(s) {
		j := strings.Index(s[i:], close)
		if j < 0 {
			return 0, false
		}
		at := i + j
		if at > 0 && s[at-1] == '\n' {
			return at + len(close) - 1, true
		}
		i = at + len(close)
	}
	return 0, false
}

func unwrapPSHereString(s string) (string, bool, bool) {
	literal := false
	var open, close string
	switch {
	case strings.HasPrefix(s, "@'"):
		open, close, literal = "@'", "'@", true
	case strings.HasPrefix(s, "@\""):
		open, close = "@\"", "\"@"
	default:
		return "", false, false
	}
	rest := s[len(open):]
	switch {
	case strings.HasPrefix(rest, "\r\n"):
		rest = rest[2:]
	case strings.HasPrefix(rest, "\n"):
		rest = rest[1:]
	default:
		return "", false, false
	}
	end := strings.LastIndex(rest, close)
	if end < 0 {
		return "", false, false
	}
	body := rest[:end]
	body = strings.TrimSuffix(body, "\n")
	body = strings.TrimSuffix(body, "\r")
	return body, literal, true
}

func psExpandable(body string) bool {
	return strings.Contains(body, "$") || strings.Contains(body, "`")
}

func writeANSIEscape(b *strings.Builder, segment string, i int) int {
	if i+1 >= len(segment) {
		b.WriteByte('\\')
		return i
	}
	next := segment[i+1]
	switch next {
	case 'n':
		b.WriteByte('\n')
		return i + 1
	case 't':
		b.WriteByte('\t')
		return i + 1
	case 'r':
		b.WriteByte('\r')
		return i + 1
	case '\\', '\'', '"':
		b.WriteByte(next)
		return i + 1
	case 'x':
		j := i + 2
		hex := ""
		for j < len(segment) && len(hex) < 2 && isHexByte(segment[j]) {
			hex += string(segment[j])
			j++
		}
		if hex == "" {
			b.WriteByte('x')
			return i + 1
		}
		value, _ := strconv.ParseUint(hex, 16, 8)
		b.WriteByte(byte(value))
		return j - 1
	default:
		if next >= '0' && next <= '7' {
			j := i + 1
			octal := ""
			for j < len(segment) && len(octal) < 3 && segment[j] >= '0' && segment[j] <= '7' {
				octal += string(segment[j])
				j++
			}
			value, _ := strconv.ParseUint(octal, 8, 8)
			b.WriteByte(byte(value))
			return j - 1
		}
		b.WriteByte(next)
		return i + 1
	}
}

func writePowerShellEscape(b *strings.Builder, segment string, i int) int {
	if i+1 >= len(segment) {
		return i
	}
	next := segment[i+1]
	if next == '\r' {
		if i+2 < len(segment) && segment[i+2] == '\n' {
			return i + 2
		}
		return i + 1
	}
	if next == '\n' {
		return i + 1
	}
	switch next {
	case 'n':
		b.WriteByte('\n')
	case 't':
		b.WriteByte('\t')
	case 'r':
		b.WriteByte('\r')
	case 'a':
		b.WriteByte('\a')
	default:
		b.WriteByte(next)
	}
	return i + 1
}

func bashUnescapeTokens(tokens []string) ([]string, bool) {
	changed := false
	out := make([]string, len(tokens))
	for i, token := range tokens {
		decoded, ok := bashUnescapeWord(token)
		out[i] = decoded
		if ok {
			changed = true
		}
	}
	return out, changed
}

func bashUnescapeWord(s string) (string, bool) {
	if !strings.Contains(s, "\\") {
		return s, false
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] != '\n' {
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), true
}

func isHexByte(ch byte) bool {
	return ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F'
}

func shellTokens(segment string) []string {
	var tokens []string
	var b strings.Builder
	quote := byte(0)
	flush := func() {
		if b.Len() == 0 && quote == 0 {
			return
		}
		tokens = append(tokens, b.String())
		b.Reset()
	}
	const ansiQuote = 1
	for i := 0; i < len(segment); i++ {
		ch := segment[i]
		if quote == ansiQuote {
			if ch == '\'' {
				quote = 0
				continue
			}
			if ch == '\\' {
				i = writeANSIEscape(&b, segment, i)
				continue
			}
			b.WriteByte(ch)
			continue
		}
		if quote == 0 && ch == '$' && i+1 < len(segment) && (segment[i+1] == '\'' || segment[i+1] == '"') {
			if segment[i+1] == '\'' {
				quote = ansiQuote
			} else {
				quote = '"'
			}
			i++
			continue
		}
		if ch == '\\' && quote != '\'' {
			// PowerShell leaves \ in place. Inside double quotes, only a few
			// characters are bash escapes; a Windows path keeps its backslashes.
			if quote == 0 {
				b.WriteByte('\\')
				continue
			}
			if i+1 >= len(segment) {
				b.WriteByte(ch)
				continue
			}
			next := segment[i+1]
			if quote == '"' && next != '$' && next != '`' && next != '"' && next != '\\' && next != '\n' {
				b.WriteByte(ch)
				continue
			}
			if next != '\n' {
				b.WriteByte(next)
			}
			i++
			continue
		}
		if ch == '`' && quote != '\'' {
			i = writePowerShellEscape(&b, segment, i)
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			b.WriteByte(ch)
			continue
		}
		switch ch {
		case '\'', '"':
			// &"git" is the call operator plus the command name, not one word.
			if b.String() == "&" || b.String() == "." {
				flush()
			}
			// Keep 'Xpush'.Substring(1) raw, quotes included. Unquoted
			// Xpush.Substring is still an ordinary word.
			if b.Len() == 0 {
				if end, ok := psMemberChainEnd(segment, i); ok {
					b.WriteString(segment[i : end+1])
					i = end
					continue
				}
			}
			quote = ch
		case ' ', '\t':
			flush()
		case '(':
			// A parenthesized argument is one expression, spaces included.
			// &('git') is the call operator plus that expression. $('push')
			// stays glued so the subcommand word still contains $.
			if _, end, ok := parenthesizedCommand(segment, i); ok {
				if b.String() == "&" || b.String() == "." {
					flush()
				}
				b.WriteString(segment[i : end+1])
				i = end
				continue
			}
			b.WriteByte(ch)
		default:
			if ch == '@' && i+1 < len(segment) && segment[i+1] == '(' {
				// PowerShell keeps spaces inside @(...). Bash tokenization would split them.
				if _, end, ok := parenthesizedCommand(segment, i+1); ok {
					b.WriteString(segment[i : end+1])
					i = end
					continue
				}
			}
			if ch == '@' && i+1 < len(segment) && (segment[i+1] == '\'' || segment[i+1] == '"') {
				// @'push'@ is one argument, the here-string body. A quote
				// scan would drop the quotes and leave a word that starts with @.
				if end, ok := psHereStringEnd(segment, i); ok {
					if b.String() == "&" || b.String() == "." {
						flush()
					}
					if b.Len() == 0 {
						b.WriteString(segment[i : end+1])
						i = end
						continue
					}
				}
			}
			b.WriteByte(ch)
		}
	}
	flush()
	return tokens
}

// IterationContractSource is optional. The loop appends the returned lines to
// the outbound model request only, and only when a name is missing or a tool
// is still locked.
type IterationContractSource interface {
	IterationContract() (goal string, missing []string, locked []string)
}

func withIterationContractReminder(cb LoopCallbacks, conversation []interface{}) []interface{} {
	source, ok := cb.(IterationContractSource)
	if !ok || source == nil {
		return conversation
	}
	goal, missing, locked := source.IterationContract()
	block := formatIterationContract(goal, missing, locked)
	if block == "" {
		return conversation
	}
	return appendOutboundSystemBlock(conversation, block)
}

func formatIterationContract(goal string, missing, locked []string) string {
	missing = contractNames(missing)
	locked = contractNames(locked)
	if len(missing) == 0 && len(locked) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[本轮契约]")
	if goal = oneLine(goal, 120); goal != "" {
		b.WriteString("\n目标：")
		b.WriteString(goal)
	}
	if len(missing) > 0 {
		b.WriteString("\n尚未赋值：")
		b.WriteString(strings.Join(displayContractNames(missing), "、"))
	}
	if len(locked) > 0 {
		b.WriteString("\n仍锁定：")
		b.WriteString(strings.Join(locked, "、"))
	}
	return b.String()
}

func contractNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func displayContractNames(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		switch name {
		case "document.generate.file":
			out[i] = "生成的文件"
		case "document.write.office":
			out[i] = "办公文件"
		case "shell.execute.remote_host":
			// The missing item is the remote command effect, in connect mode
			// and after the session is bound. Naming the host field made the
			// model treat a finished effect as an empty parameter.
			out[i] = "ssh 命令"
		default:
			out[i] = name
		}
	}
	return out
}

func oneLine(text string, maxRunes int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" || maxRunes <= 0 {
		return text
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxRunes])
}

func appendOutboundSystemBlock(conversation []interface{}, block string) []interface{} {
	if len(conversation) == 0 || strings.TrimSpace(block) == "" {
		return conversation
	}
	out := make([]interface{}, len(conversation))
	copy(out, conversation)
	switch msg := out[0].(type) {
	case map[string]string:
		if msg["role"] != "system" {
			return conversation
		}
		cloned := make(map[string]string, len(msg))
		for key, value := range msg {
			cloned[key] = value
		}
		cloned["content"] = strings.TrimSpace(cloned["content"]) + "\n\n" + block
		out[0] = cloned
		return out
	case map[string]interface{}:
		role, _ := msg["role"].(string)
		if role != "system" {
			return conversation
		}
		cloned := make(map[string]interface{}, len(msg))
		for key, value := range msg {
			cloned[key] = value
		}
		content, _ := cloned["content"].(string)
		cloned["content"] = strings.TrimSpace(content) + "\n\n" + block
		out[0] = cloned
		return out
	default:
		return conversation
	}
}
