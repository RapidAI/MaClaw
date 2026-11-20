package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	mcpStatusBlockedUnknown = "blocked_unknown"
	mcpSchemaVersion        = "mcp-task-v2"
	mcpDirtyNoteText        = "既有改动、来源未证实"
	mcpStateRetention       = 7 * 24 * time.Hour

	mcpCodeTaskNotFound    = "task_not_found"
	mcpCodeTaskExpired     = "task_expired"
	mcpCodeRunNotFound     = "run_not_found"
	mcpCodeRequestConflict = "request_payload_conflict"
	mcpCodeStatusConflict  = "status_version_conflict"
	mcpCodeArtifactStale   = "artifact_stale"

	mcpRequestStateAccepted  = "accepted"
	mcpRequestStateRunning   = "running"
	mcpRequestStateFailed    = "failed"
	mcpRequestStateCompleted = "completed"
	mcpRequestStateExpired   = "expired_or_unknown"
	mcpRequestStateNotFound  = "not_found"

	mcpTaskExpiredMessage    = "这个任务的记录已过保留期"
	mcpStatusConflictMessage = "状态版本已经变化，这次没有执行"
	mcpArtifactStaleMessage  = "文件内容和期望的哈希不一致"
)

// mcpResumeConflict is another known task that already uses this work directory.
type mcpResumeConflict struct {
	Kind   string `json:"kind"`
	TaskID string `json:"task_id,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// mcpCallError keeps the Chinese sentence in Error() and adds a stable code.
type mcpCallError struct {
	Code   string
	Msg    string
	TaskID string
	RunID  string
}

func (e *mcpCallError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

func mcpErr(code, msg, taskID, runID string) error {
	return &mcpCallError{Code: code, Msg: msg, TaskID: taskID, RunID: runID}
}

func mcpToolFail(err error) map[string]any {
	if err == nil {
		return mcpToolError("")
	}
	var coded *mcpCallError
	if errors.As(err, &coded) && coded != nil {
		body := map[string]any{
			"error":   coded.Msg,
			"code":    coded.Code,
			"message": coded.Msg,
		}
		if strings.TrimSpace(coded.TaskID) != "" {
			body["task_id"] = coded.TaskID
		}
		if strings.TrimSpace(coded.RunID) != "" {
			body["run_id"] = coded.RunID
		}
		return mcpToolText(body, true)
	}
	return mcpToolError(err.Error())
}

type mcpCallOptKey struct{}

// mcpCallOpts carries optional tool arguments without changing method signatures.
type mcpCallOpts struct {
	HasExpected     bool
	ExpectedVersion int
	DryRun          bool
	ArtifactCursor  string
	HasLimit        bool
	Limit           int
	IncludeHidden   bool
	ExcludeDirs     []string
	HasExpectedSHA  bool
	ExpectedSHA     string
}

func mcpContextWithOpts(ctx context.Context, opts mcpCallOpts) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, mcpCallOptKey{}, opts)
}

func mcpCallOptsFrom(ctx context.Context) mcpCallOpts {
	if ctx == nil {
		return mcpCallOpts{}
	}
	opts, _ := ctx.Value(mcpCallOptKey{}).(mcpCallOpts)
	return opts
}

func mcpKnownTaskStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case mcpStatusRunning, mcpStatusCancelled, mcpStatusFailed, mcpStatusCompleted,
		mcpStatusNeedsConfirmation, mcpStatusWaitingReview, mcpStatusIdle, mcpStatusBlockedUnknown:
		return true
	default:
		return false
	}
}

func mcpTextAsksConfirmation(text string) bool {
	return strings.Contains(text, "需要确认执行计划")
}

// mcpCommandOutcome is the short decision a caller reads after one command.
// run_id is that command's session. A running round tells the caller to query
// it before sending anything else. A finished round leads with the reason,
// not the plan body that was appended for the desktop.
func mcpCommandOutcome(view mcpTaskView) string {
	id := strings.TrimSpace(firstNonEmpty(view.RunID, view.CurrentRunID))
	switch view.Status {
	case mcpStatusRunning:
		if view.ConfirmationKind == "scope" || view.ConfirmationKind == "critical" {
			if id == "" {
				return "这一轮还在运行，先处理 commands 里的确认，再决定下一条命令。"
			}
			return "这一轮还在运行，先处理 commands 里的确认。会话标识是 " + id + "。"
		}
		if id == "" {
			return "命令已受理，这一轮还没结束。结束后用 get_task 查询这一轮，再决定下一条命令。"
		}
		return "命令已受理，这一轮还没结束。会话标识是 " + id + "。结束后用 get_task 传入这个 run_id 查看状态，再决定下一条命令。"
	case mcpStatusNeedsConfirmation:
		lead := mcpOutcomeLead(view.Text)
		if lead == "" {
			lead = strings.TrimSpace(view.Error)
		}
		if lead == "" {
			lead = "这一轮需要确认。"
		}
		tail := "先看 status、commands 和 confirmation_id，再决定下一条命令。"
		if id != "" {
			tail = "会话标识是 " + id + "。" + tail
		}
		return lead + "\n" + tail
	case mcpStatusWaitingReview:
		lead := mcpOutcomeLead(view.Text)
		if lead == "" {
			lead = "子任务结果待审阅。"
		}
		return lead
	case mcpStatusFailed:
		lead := strings.TrimSpace(view.Error)
		if lead == "" {
			lead = mcpOutcomeLead(view.Text)
		}
		if lead == "" {
			lead = "这一轮失败了。"
		}
		return lead
	case mcpStatusCancelled:
		lead := ""
		if view.Stop != nil {
			lead = strings.TrimSpace(view.Stop.Reason)
		}
		if lead == "" {
			lead = strings.TrimSpace(view.Error)
		}
		if lead == "" {
			lead = "这一轮已停止。"
		}
		// A later command cancels a round that was waiting. The reason it
		// was waiting stays visible when that session is queried.
		if reason := mcpOutcomeLead(view.Text); reason != "" && !strings.Contains(lead, reason) {
			return reason + "\n" + lead
		}
		return lead
	case mcpStatusCompleted:
		lead := mcpOutcomeLead(view.Text)
		if lead == "" {
			lead = "这一轮已完成。"
		}
		return lead
	case mcpStatusIdle:
		if id == "" {
			return "任务已创建，还没有开始。"
		}
		return "任务已创建。会话标识是 " + id + "。"
	default:
		lead := mcpOutcomeLead(firstNonEmpty(view.Error, view.Text))
		if lead != "" {
			return lead
		}
		if status := strings.TrimSpace(view.Status); status != "" {
			return "当前状态是 " + status + "。"
		}
		return ""
	}
}

// mcpOutcomeLead keeps the decision sentence and drops the plan body and the
// send_command hint that get_task appends after it.
func mcpOutcomeLead(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	cut := len(text)
	for _, mark := range []string{
		"\n\n" + mcpPlanConfirmHint,
		"\n\n" + mcpPlanReviewHint,
		"\n\n" + mcpPlanDialogueHint,
		"\n\n## ",
	} {
		if i := strings.Index(text, mark); i >= 0 && i < cut {
			cut = i
		}
	}
	lead := strings.TrimSpace(text[:cut])
	// A "## " plan is cut above. The formatted plan starts with the goal line
	// and stands immediately before the confirm hint, so it would otherwise
	// become the decision sentence.
	if hintAt := strings.Index(text, "\n\n"+mcpPlanConfirmHint); hintAt >= 0 && hintAt == cut {
		lead = strings.TrimSpace(mcpDropPlanOverlay(lead, false))
	}
	const maxRunes = 800
	if utf8.RuneCountInString(lead) <= maxRunes {
		return lead
	}
	return string([]rune(lead)[:maxRunes]) + "…"
}

// mcpAutoResumeAllowed is false after the user stopped the run.
// An explicit send_command is still accepted; this only blocks an automatic restart.
func mcpAutoResumeAllowed(view mcpTaskView) bool {
	if view.Status == mcpStatusCancelled && view.Stop != nil && view.Stop.Class == mcpStopUser {
		return false
	}
	return true
}

func mcpRejectStaleVersion(path string, view mcpTaskView, known bool, opts mcpCallOpts) error {
	if !opts.HasExpected {
		return nil
	}
	// A task with no published run has no version. Expected 0 is a real
	// observation, not an omitted field, so it does not match "missing".
	if !known || opts.ExpectedVersion != view.StatusVersion {
		runID := ""
		if known {
			runID = firstNonEmpty(view.CurrentRunID, view.RunID)
		}
		return mcpErr(mcpCodeStatusConflict, mcpStatusConflictMessage, path, runID)
	}
	return nil
}

func (v mcpTaskView) MarshalJSON() ([]byte, error) {
	type wire mcpTaskView
	raw, err := json.Marshal(wire(v))
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	taskID := strings.TrimSpace(v.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(v.ProjectPath)
	}
	if taskID == "" {
		doc["task_id"] = nil
	} else {
		doc["task_id"] = taskID
	}
	doc["project_path"] = v.ProjectPath
	doc["current_run_id"] = mcpJSONStringOrNull(v.CurrentRunID)
	doc["run_id"] = mcpJSONStringOrNull(v.RunID)
	doc["status_version"] = v.StatusVersion
	doc["status"] = v.Status
	doc["started_at"] = mcpJSONStringOrNull(v.StartedAt)
	doc["finished_at"] = mcpJSONStringOrNull(v.FinishedAt)
	if v.Steps == nil {
		doc["steps"] = []any{}
	}
	if v.Stop == nil {
		doc["stop"] = nil
	}
	return json.Marshal(doc)
}

func mcpJSONStringOrNull(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func mcpSealTaskIdentity(view mcpTaskView) mcpTaskView {
	if strings.TrimSpace(view.TaskID) == "" {
		view.TaskID = strings.TrimSpace(view.ProjectPath)
	}
	if strings.TrimSpace(view.CurrentRunID) == "" {
		view.CurrentRunID = strings.TrimSpace(view.RunID)
	}
	if strings.TrimSpace(view.WorkingDirEffective) == "" {
		view.WorkingDirEffective = strings.TrimSpace(firstNonEmpty(view.WorkingDir, view.workspace))
	}
	if strings.TrimSpace(view.ToolRoot) == "" {
		view.ToolRoot = strings.TrimSpace(view.WorkingDirEffective)
	}
	if strings.TrimSpace(view.Provider) != "" && strings.TrimSpace(view.ProviderEffective) == "" {
		view.ProviderEffective = view.Provider
	}
	if strings.TrimSpace(view.Model) != "" && strings.TrimSpace(view.ModelEffective) == "" {
		view.ModelEffective = view.Model
	}
	return view
}

func mcpGuardCompletion(view mcpTaskView) mcpTaskView {
	raw := strings.TrimSpace(view.Status)
	if raw != "" && !mcpKnownTaskStatus(raw) {
		view.StatusRaw = raw
		view.Status = mcpStatusBlockedUnknown
		return view
	}
	if view.Status == mcpStatusCompleted && mcpTextAsksConfirmation(view.Text) {
		view.Status = mcpStatusNeedsConfirmation
		view.EndReason = mcpStatusNeedsConfirmation
	}
	if view.Status == mcpStatusCompleted && mcpStepsBlockCompletion(view.Steps) {
		view.Status = mcpStatusFailed
		if strings.TrimSpace(view.Error) == "" {
			view.Error = mcpStepFailureDetail(view.Steps, view.Text, nil)
		}
	}
	return view
}

func mcpRelabelSessionWorkspace(text, sessionPath, effective string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	effective = strings.TrimSpace(effective)
	if text == "" || sessionPath == "" || sessionPath == effective {
		return text
	}
	needle := "workspace: " + sessionPath
	if !strings.Contains(text, needle) {
		return text
	}
	return strings.ReplaceAll(text, needle, "session_path: "+sessionPath)
}

func mcpDeriveRequestState(req *mcpClientRequest, task *mcpTaskView) string {
	if req == nil {
		return mcpRequestStateNotFound
	}
	if req.failed || strings.TrimSpace(req.errText) != "" {
		return mcpRequestStateFailed
	}
	if task != nil {
		switch task.Status {
		case mcpStatusRunning:
			return mcpRequestStateRunning
		case mcpStatusFailed:
			return mcpRequestStateFailed
		case mcpStatusCompleted:
			if req.done {
				return mcpRequestStateCompleted
			}
			return mcpRequestStateRunning
		case mcpStatusNeedsConfirmation, mcpStatusWaitingReview, mcpStatusCancelled:
			return mcpRequestStateAccepted
		}
	}
	if !req.done {
		if strings.TrimSpace(req.ProjectPath) == "" {
			return mcpRequestStateAccepted
		}
		return mcpRequestStateRunning
	}
	if strings.TrimSpace(req.ProjectPath) == "" {
		return mcpRequestStateAccepted
	}
	return mcpRequestStateRunning
}

func (a *App) mcpDryRunView(path string, current mcpTaskView, known bool) (mcpTaskView, error) {
	view := current
	if !known {
		view = mcpTaskView{ProjectPath: path, Status: mcpStatusIdle}
	}
	view.ProjectPath = path
	view.DryRun = true
	dir := firstNonEmpty(view.WorkingDirEffective, view.WorkingDir, view.workspace)
	view.Conflicts = a.mcpSameWorkDirConflicts(path, dir)
	if note := mcpDirtyNote(dir); note != "" {
		view.DirtyNote = note
	}
	view.ResumeOK = mcpAutoResumeAllowed(view) && len(view.Conflicts) == 0 && view.DirtyNote == "" && view.Status != mcpStatusRunning
	return a.mcpReturnTask(view), nil
}

func (a *App) mcpSameWorkDirConflicts(self, dir string) []mcpResumeConflict {
	dir = strings.TrimSpace(dir)
	if a == nil || dir == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []mcpResumeConflict
	add := func(path, detail string) {
		path = strings.TrimSpace(path)
		if path == "" || path == self {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, mcpResumeConflict{Kind: "same_work_dir", TaskID: path, Detail: detail})
	}
	a.mcpRunsMu.Lock()
	for path, rec := range a.mcpRuns {
		if rec == nil {
			continue
		}
		other := firstNonEmpty(rec.Workspace, rec.WorkDirRequested)
		if other != "" && mcpSameDir(other, dir) {
			add(path, "同一工作目录")
		}
	}
	a.mcpRunsMu.Unlock()
	if a.memoryStore != nil {
		for _, task := range a.ListTasks(50) {
			path := normalizeProjectSessionPath(task.ProjectPath)
			other := recentTaskWorkingDirFromTags(task.Tags)
			if other != "" && mcpSameDir(other, dir) {
				add(path, "同一工作目录")
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

func mcpSameDir(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	a := normalizeProjectSessionPath(left)
	b := normalizeProjectSessionPath(right)
	if a != "" && b != "" {
		return strings.EqualFold(a, b)
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

// mcpDirtyNote reports uncommitted work without naming an author.
// A missing git directory or a failed status command is silence, not "clean".
func mcpDirtyNote(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain")
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(string(out)) == "" {
		return ""
	}
	return mcpDirtyNoteText
}

func mcpHashMatches(sum, expected string) bool {
	expected = strings.TrimSpace(strings.ToLower(expected))
	expected = strings.TrimPrefix(expected, "sha256:")
	sum = strings.TrimSpace(strings.ToLower(sum))
	if expected == "" || sum == "" {
		return false
	}
	return sum == expected
}
