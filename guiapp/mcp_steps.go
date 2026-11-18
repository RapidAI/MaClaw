package guiapp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	mcpStepPassed     = "passed"
	mcpStepFailed     = "failed"
	mcpStepSkipped    = "skipped"
	mcpStepUnfinished = "unfinished"
)

// CodingStepOutcome is one coding step as the host finished it.
// MCP copies it onto the task view. It is not sent to Hub.
type CodingStepOutcome struct {
	ID        string
	Title     string
	Status    string
	Error     string
	Retryable bool
	Files     []string
}

// mcpStepFile is one workspace file and the hash at a recorded moment.
type mcpStepFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
}

// mcpStepView is one step on get_task. Status, the failure reason, and
// whether that step can be retried are fields, not only sentences in text.
type mcpStepView struct {
	ID        string         `json:"id,omitempty"`
	Title     string         `json:"title,omitempty"`
	Status    string         `json:"status"`
	Error     string         `json:"error,omitempty"`
	Retryable bool           `json:"retryable"`
	Files     []mcpStepFile  `json:"files,omitempty"`
	Checks    []mcpCheckView `json:"checks,omitempty"`
}

// mcpCheckView ties a browser screenshot or assertion to the step and the
// file bytes that existed when it was taken.
type mcpCheckView struct {
	Action           string        `json:"action"`
	StepID           string        `json:"step_id,omitempty"`
	At               string        `json:"at,omitempty"`
	OK               bool          `json:"ok"`
	Detail           string        `json:"detail,omitempty"`
	Files            []mcpStepFile `json:"files,omitempty"`
	ScreenshotSHA256 string        `json:"screenshot_sha256,omitempty"`
	Stale            bool          `json:"stale,omitempty"`
}

func codingStepsFromResults(results []v2.TaskRunResult) []CodingStepOutcome {
	if len(results) == 0 {
		return nil
	}
	out := make([]CodingStepOutcome, 0, len(results))
	for _, result := range results {
		if result.TaskIndex <= 0 && strings.TrimSpace(result.Title) == "" && result.Status == "" {
			continue
		}
		out = append(out, codingStepOutcomeFromResult(result))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func codingStepOutcomeFromResult(result v2.TaskRunResult) CodingStepOutcome {
	errText := strings.TrimSpace(result.Error)
	statusText, errText := mcpNormalizeRetryStatus(string(result.Status), errText, result.RuntimeHandoff)
	status, retryable := mcpClassifyRunStatus(statusText, errText)
	id := ""
	if result.TaskIndex > 0 {
		id = fmt.Sprintf("T%d", result.TaskIndex)
	}
	var files []string
	files = append(files, result.FilesCreated...)
	files = append(files, result.FilesModified...)
	return CodingStepOutcome{
		ID:        id,
		Title:     strings.TrimSpace(result.Title),
		Status:    status,
		Error:     strings.TrimSpace(errText),
		Retryable: retryable,
		Files:     uniqueSortedSubAgentStrings(files),
	}
}

func setCodingStepOutcomes(resp *IMAgentResponse, results []v2.TaskRunResult) {
	if resp == nil {
		return
	}
	steps := codingStepsFromResults(results)
	if len(steps) == 0 {
		return
	}
	resp.CodingSteps = steps
}

func mcpClassifyRunStatus(status, errText string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case mcpStepPassed:
		return mcpStepPassed, false
	case mcpStepFailed, codingStepVerifyFail:
		return mcpStepFailed, true
	case mcpStepSkipped:
		if mcpSkipIsRetryable(errText) {
			return mcpStepUnfinished, true
		}
		return mcpStepSkipped, false
	case codingStepPending, codingStepRunning, mcpStepUnfinished, "":
		return mcpStepUnfinished, true
	default:
		return mcpStepUnfinished, true
	}
}

func mcpSkipIsRetryable(errText string) bool {
	if isCodingExecCancelError(errText) {
		return true
	}
	// A skip that is still waiting on an earlier step, or on a child review,
	// is not itself a retry target. Every other skip is unfinished work.
	if mcpSkipWaits(errText) {
		return false
	}
	return true
}

const mcpReviewHandoffErrorText = "子任务结果待审阅"

func mcpSkipWaits(errText string) bool {
	return mcpSkipWaitsOnPrior(errText) || mcpSkipIsReviewHandoff(errText)
}

// mcpSkipIsReviewHandoff is a child-review handoff. The projected step is
// often only "skipped" with an empty error or a review code; the status
// token waiting_child is not what retry_steps sees.
func mcpSkipIsReviewHandoff(errText string) bool {
	lower := strings.ToLower(errText)
	return strings.Contains(lower, "waiting_child") ||
		strings.Contains(lower, "runtime_child_review_required") ||
		strings.Contains(lower, "runtime_child_review_in_progress") ||
		strings.Contains(errText, mcpReviewHandoffErrorText)
}

// mcpHandoffReason reports the text the retry rule should read. A handoff
// stays skipped until the review command, even when the runner stored no error.
func mcpHandoffReason(errText string, handoff bool) (string, bool) {
	if mcpSkipIsReviewHandoff(errText) {
		return strings.TrimSpace(errText), true
	}
	if !handoff {
		return errText, false
	}
	return mcpReviewHandoffErrorText, true
}

func mcpNormalizeRetryStatus(status, errText string, handoff bool) (string, string) {
	if reason, ok := mcpHandoffReason(errText, handoff); ok {
		return mcpStepSkipped, reason
	}
	return status, errText
}

func mcpSkipWaitsOnPrior(errText string) bool {
	lower := strings.ToLower(errText)
	return strings.Contains(lower, "prior step") || strings.Contains(lower, "dependency")
}

const mcpPriorSkipReadyError = "上一步已经通过，这一步还没有执行"

// mcpNamedStepRetryable is the one rule for retry_steps, the checkpoint
// selector, and the workbench selector. A skip that only waited on an
// earlier failure becomes retryable once nothing else is failed, unfinished,
// or waiting on a child review. The review handoff stays with that command.
func mcpNamedStepRetryable(status, errText string, openElsewhere bool) (string, bool) {
	classified, retryable := mcpClassifyRunStatus(status, errText)
	if classified == mcpStepSkipped && mcpSkipWaitsOnPrior(errText) && !openElsewhere {
		return mcpStepUnfinished, true
	}
	return classified, retryable
}

func mcpClassifyOpen(status, errText string) bool {
	classified, _ := mcpClassifyRunStatus(status, errText)
	if classified == mcpStepFailed || classified == mcpStepUnfinished {
		return true
	}
	// A child-review handoff has not passed. Dependency skips behind it are
	// still waiting on that review.
	return classified == mcpStepSkipped && mcpSkipIsReviewHandoff(errText)
}

func mcpStepsFromAgent(resp *IMAgentResponse) []mcpStepView {
	if resp == nil || len(resp.CodingSteps) == 0 {
		return nil
	}
	out := make([]mcpStepView, 0, len(resp.CodingSteps))
	for _, step := range resp.CodingSteps {
		out = append(out, mcpStepViewFromCoding(step))
	}
	return out
}

func mcpStepViewFromCoding(step CodingStepOutcome) mcpStepView {
	status, retryable := mcpClassifyRunStatus(step.Status, step.Error)
	if step.Retryable && status != mcpStepPassed && status != mcpStepSkipped {
		retryable = true
	}
	view := mcpStepView{
		ID:        strings.TrimSpace(step.ID),
		Title:     strings.TrimSpace(step.Title),
		Status:    status,
		Error:     strings.TrimSpace(step.Error),
		Retryable: retryable,
	}
	for _, path := range step.Files {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		view.Files = append(view.Files, mcpStepFile{Path: path})
	}
	return view
}

func mcpStepsBlockCompletion(steps []mcpStepView) bool {
	for _, step := range steps {
		status := strings.TrimSpace(step.Status)
		if status == mcpStepPassed {
			continue
		}
		// A skip blocks only while it still waits. A dependency skip whose
		// parent has already passed is reopened to unfinished before this runs.
		if status == mcpStepSkipped && !mcpSkipWaits(step.Error) {
			continue
		}
		return true
	}
	return false
}

func mcpTextBlocksCompletion(text string) bool {
	if strings.Contains(text, "模型连续返回空响应") {
		return true
	}
	if strings.Contains(text, "still failed") {
		return true
	}
	if strings.Contains(text, "did not finish") {
		return true
	}
	return false
}

func mcpOutcomeBlocksCompletion(text string, resp *IMAgentResponse) bool {
	if resp != nil && resp.HardExit {
		return true
	}
	return mcpTextBlocksCompletion(text)
}

func mcpStepsFromFinishText(text string, resp *IMAgentResponse) []mcpStepView {
	if !mcpOutcomeBlocksCompletion(text, resp) {
		return nil
	}
	errText := mcpStepFailureDetail(nil, text, resp)
	return []mcpStepView{{
		Status:    mcpStepFailed,
		Error:     errText,
		Retryable: true,
	}}
}

func mcpStepFailureDetail(steps []mcpStepView, text string, resp *IMAgentResponse) string {
	for _, step := range steps {
		if step.Status == mcpStepPassed || step.Status == mcpStepSkipped {
			continue
		}
		if errText := strings.TrimSpace(step.Error); errText != "" {
			return errText
		}
	}
	for _, step := range steps {
		if step.Status != mcpStepSkipped || !mcpSkipIsReviewHandoff(step.Error) {
			continue
		}
		if errText := strings.TrimSpace(step.Error); errText != "" {
			return errText
		}
	}
	if strings.Contains(text, "模型连续返回空响应") || (resp != nil && resp.HardExit) {
		return "模型连续返回空响应，任务中断"
	}
	if mcpTextBlocksCompletion(text) || mcpStepsBlockCompletion(steps) {
		return "有步骤失败或未完成"
	}
	return ""
}

func parseCodingStepID(raw string) (int, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", false
	}
	lower := strings.ToLower(raw)
	lower = strings.TrimPrefix(lower, "步骤")
	lower = strings.TrimPrefix(lower, "步驟")
	lower = strings.TrimSpace(lower)
	if strings.HasPrefix(lower, "t") && len(lower) > 1 {
		lower = strings.TrimSpace(lower[1:])
	}
	n, err := strconv.Atoi(lower)
	if err != nil || n <= 0 {
		return 0, "", false
	}
	return n, fmt.Sprintf("T%d", n), true
}

func mcpNormalizeStepIDs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("要指定要重试的步骤")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.ReplaceAll(item, "步骤 ", "步骤")
		item = strings.ReplaceAll(item, "步驟 ", "步驟")
		for _, part := range strings.FieldsFunc(item, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；' || r == ' ' || r == '\t' || r == '\n'
		}) {
			_, id, ok := parseCodingStepID(part)
			if !ok {
				return nil, fmt.Errorf("步骤编号无法识别：%s", strings.TrimSpace(part))
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("要指定要重试的步骤")
	}
	return out, nil
}

func mcpRetryRejectedBySteps(steps []mcpStepView, ids []string) error {
	if len(steps) == 0 {
		return nil
	}
	byID := make(map[string]mcpStepView, len(steps))
	open := false
	for _, step := range steps {
		if mcpClassifyOpen(step.Status, step.Error) {
			open = true
		}
		if id := strings.TrimSpace(step.ID); id != "" {
			byID[strings.ToUpper(id)] = step
		}
	}
	for _, id := range ids {
		step, ok := byID[strings.ToUpper(id)]
		if !ok {
			return fmt.Errorf("没有步骤 %s", id)
		}
		classified, retryable := mcpNamedStepRetryable(step.Status, step.Error, open)
		if classified == mcpStepPassed {
			return fmt.Errorf("%s 已经通过，不会重跑", id)
		}
		if !retryable {
			return fmt.Errorf("%s 不能单独重试", id)
		}
	}
	return nil
}

// mcpMergeSteps keeps an earlier step when the new result does not mention it.
// A retry that only reports the failed step must not drop steps that already passed.
// A result with no step id is the whole turn, not a partial report: it updates the
// one open step, or stays visible beside steps that already passed.
// A skip that only waited on an earlier failure is reopened once that failure is gone.
func mcpMergeSteps(prev, next []mcpStepView) []mcpStepView {
	return mcpReopenReadySkips(mcpMergeReportedSteps(prev, next))
}

func mcpMergeReportedSteps(prev, next []mcpStepView) []mcpStepView {
	if len(next) == 0 {
		return mcpCloneSteps(prev)
	}
	if !mcpStepsHaveIDs(next) {
		if !mcpStepsHaveIDs(prev) {
			return mcpCloneSteps(next)
		}
		return mcpApplyUnscopedSteps(prev, next)
	}
	if len(prev) == 0 {
		return mcpCloneSteps(next)
	}
	prevByID := make(map[string]mcpStepView, len(prev))
	for _, step := range prev {
		if id := strings.ToUpper(strings.TrimSpace(step.ID)); id != "" {
			prevByID[id] = step
		}
	}
	seen := map[string]struct{}{}
	out := make([]mcpStepView, 0, len(prev)+len(next))
	for _, step := range next {
		id := strings.ToUpper(strings.TrimSpace(step.ID))
		if id != "" {
			seen[id] = struct{}{}
			if len(step.Checks) == 0 {
				if old, ok := prevByID[id]; ok {
					step.Checks = mcpCloneChecks(old.Checks)
				}
			}
		}
		out = append(out, step)
	}
	for _, step := range prev {
		id := strings.ToUpper(strings.TrimSpace(step.ID))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, step)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ni, _, okI := parseCodingStepID(out[i].ID)
		nj, _, okJ := parseCodingStepID(out[j].ID)
		if !okI || !okJ {
			return false
		}
		return ni < nj
	})
	return mcpCloneSteps(out)
}

// mcpReopenReadySkips turns a dependency skip into unfinished work once no
// failed, unfinished, or review-handoff step remains. The handoff is left
// skipped, and so are the steps that were waiting on it.
func mcpReopenReadySkips(steps []mcpStepView) []mcpStepView {
	if len(steps) == 0 {
		return steps
	}
	for _, step := range steps {
		if mcpClassifyOpen(step.Status, step.Error) {
			return steps
		}
	}
	changed := false
	for _, step := range steps {
		if step.Status == mcpStepSkipped && mcpSkipWaitsOnPrior(step.Error) {
			changed = true
			break
		}
	}
	if !changed {
		return steps
	}
	out := mcpCloneSteps(steps)
	for i := range out {
		if out[i].Status != mcpStepSkipped || !mcpSkipWaitsOnPrior(out[i].Error) {
			continue
		}
		out[i].Status = mcpStepUnfinished
		out[i].Retryable = true
		out[i].Error = mcpPriorSkipReadyError
	}
	return out
}

func mcpStepsHaveIDs(steps []mcpStepView) bool {
	for _, step := range steps {
		if strings.TrimSpace(step.ID) != "" {
			return true
		}
	}
	return false
}

// mcpApplyUnscopedSteps folds a turn-level failure into the named checklist.
// Passed steps stay passed. One open step takes the new reason. Several open
// steps keep their own reasons, and the turn-level failure is added once.
func mcpApplyUnscopedSteps(prev, next []mcpStepView) []mcpStepView {
	failure, ok := mcpUnscopedFailure(next)
	if !ok {
		return mcpCloneSteps(prev)
	}
	out := make([]mcpStepView, 0, len(prev)+1)
	for _, step := range prev {
		if strings.TrimSpace(step.ID) == "" {
			continue
		}
		out = append(out, step)
	}
	out = mcpCloneSteps(out)
	open := -1
	openCount := 0
	for i := range out {
		if out[i].Status == mcpStepPassed || out[i].Status == mcpStepSkipped {
			continue
		}
		openCount++
		open = i
	}
	if openCount == 1 {
		out[open].Status = failure.Status
		if strings.TrimSpace(failure.Error) != "" {
			out[open].Error = failure.Error
		}
		out[open].Retryable = failure.Retryable
		return out
	}
	out = append(out, failure)
	return mcpCloneSteps(out)
}

func mcpUnscopedFailure(steps []mcpStepView) (mcpStepView, bool) {
	for _, step := range steps {
		if strings.TrimSpace(step.ID) != "" {
			continue
		}
		if step.Status == mcpStepPassed || step.Status == mcpStepSkipped {
			continue
		}
		step.Files = mcpCloneStepFiles(step.Files)
		step.Checks = mcpCloneChecks(step.Checks)
		return step, true
	}
	return mcpStepView{}, false
}

// codingExecSelectRetrySteps keeps only the named failed or unfinished tasks.
// A passed step is refused so a retry cannot start the whole plan over.
func codingExecSelectRetrySteps(tasks []*v2.TaskItem, results []v2.TaskRunResult, rawIDs []string) ([]*v2.TaskItem, error) {
	ids, err := mcpNormalizeStepIDs(rawIDs)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("还没有可续跑的编码检查点")
	}
	byIndex := make(map[int]v2.TaskRunResult, len(results))
	for _, result := range results {
		byIndex[result.TaskIndex] = result
	}
	byTask := make(map[int]*v2.TaskItem, len(tasks))
	open := false
	for _, task := range tasks {
		if task == nil || task.Index <= 0 {
			continue
		}
		byTask[task.Index] = task
		status, errText := "", ""
		handoff := false
		if result, found := byIndex[task.Index]; found {
			status = string(result.Status)
			errText = result.Error
			handoff = result.RuntimeHandoff
		}
		status, errText = mcpNormalizeRetryStatus(status, errText, handoff)
		if mcpClassifyOpen(status, errText) {
			open = true
		}
	}
	selected := make([]*v2.TaskItem, 0, len(ids))
	for _, id := range ids {
		n, _, ok := parseCodingStepID(id)
		if !ok {
			return nil, fmt.Errorf("步骤编号无法识别：%s", id)
		}
		task := byTask[n]
		if task == nil {
			return nil, fmt.Errorf("没有步骤 %s", id)
		}
		result, found := byIndex[task.Index]
		status := ""
		errText := ""
		handoff := false
		if found {
			status = string(result.Status)
			errText = result.Error
			handoff = result.RuntimeHandoff
		}
		status, errText = mcpNormalizeRetryStatus(status, errText, handoff)
		classified, retryable := mcpNamedStepRetryable(status, errText, open)
		if classified == mcpStepPassed {
			return nil, fmt.Errorf("%s 已经通过，不会重跑", id)
		}
		if !retryable {
			return nil, fmt.Errorf("%s 不能单独重试", id)
		}
		selected = append(selected, task)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("要指定要重试的步骤")
	}
	return selected, nil
}

func mcpCloneSteps(steps []mcpStepView) []mcpStepView {
	if len(steps) == 0 {
		return nil
	}
	out := make([]mcpStepView, len(steps))
	for i, step := range steps {
		out[i] = step
		out[i].Files = mcpCloneStepFiles(step.Files)
		out[i].Checks = mcpCloneChecks(step.Checks)
	}
	return out
}

func mcpCloneChecks(checks []mcpCheckView) []mcpCheckView {
	if len(checks) == 0 {
		return nil
	}
	out := make([]mcpCheckView, len(checks))
	for i, check := range checks {
		out[i] = check
		out[i].Files = mcpCloneStepFiles(check.Files)
	}
	return out
}

func mcpCloneStepFiles(files []mcpStepFile) []mcpStepFile {
	if len(files) == 0 {
		return nil
	}
	return append([]mcpStepFile(nil), files...)
}

func (a *App) mcpStampStepFiles(projectPath string, steps []mcpStepView) []mcpStepView {
	if a == nil || len(steps) == 0 {
		return steps
	}
	root, view, err := a.mcpArtifactRoot(projectPath)
	if err != nil || strings.TrimSpace(root) == "" || mcpRemoteArtifactError(view) != nil {
		return steps
	}
	for i := range steps {
		steps[i].Files = mcpHashStepFiles(root, steps[i].Files)
	}
	return steps
}

func mcpHashStepFiles(root string, files []mcpStepFile) []mcpStepFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]mcpStepFile, 0, len(files))
	seen := map[string]struct{}{}
	for _, file := range files {
		rel := mcpStepRel(root, file.Path)
		if rel == "" {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		hashed := mcpStepFile{Path: rel}
		if full, err := mcpResolveInside(root, rel); err == nil {
			if sum, hashErr := mcpHashFile(full); hashErr == nil {
				hashed.SHA256 = sum
			}
		}
		out = append(out, hashed)
	}
	return out
}

func mcpStepRel(root, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || mcpArtifactSecretName(filepath.Base(path)) {
		return ""
	}
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
		return filepath.ToSlash(rel)
	}
	rel := filepath.Clean(path)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (a *App) mcpAnnotateStepFreshness(view mcpTaskView) mcpTaskView {
	if view.Mode == taskRemoteCodingDevTag || strings.TrimSpace(view.RemoteHost) != "" {
		return view
	}
	root := mcpExistingDir(view.workspace)
	if root == "" {
		root = mcpExistingDir(view.WorkingDir)
	}
	if root == "" {
		return view
	}
	for i := range view.Steps {
		for j := range view.Steps[i].Checks {
			view.Steps[i].Checks[j].Stale = mcpCheckIsStale(root, view.Steps[i].Checks[j])
		}
	}
	for i := range view.Checks {
		view.Checks[i].Stale = mcpCheckIsStale(root, view.Checks[i])
	}
	return view
}

func mcpCheckIsStale(root string, check mcpCheckView) bool {
	if len(check.Files) == 0 {
		return false
	}
	for _, file := range check.Files {
		rel := mcpStepRel(root, file.Path)
		if rel == "" || strings.TrimSpace(file.SHA256) == "" {
			return true
		}
		full, err := mcpResolveInside(root, rel)
		if err != nil {
			return true
		}
		sum, hashErr := mcpHashFile(full)
		if hashErr != nil || !strings.EqualFold(sum, file.SHA256) {
			return true
		}
	}
	return false
}
