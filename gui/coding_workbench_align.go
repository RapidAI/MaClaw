package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// Plan modes for pure-coding multi-step planning.
//   - auto: plan then execute immediately (default; current behavior)
//   - approve: plan then pause for user approve/edit/reject
//   - off: never multi-step plan (always single task)
const (
	codingPlanModeAuto    = "auto"
	codingPlanModeApprove = "approve"
	codingPlanModeOff     = "off"
)

// Step status values for live Todo UI.
const (
	codingStepPending   = "pending"
	codingStepRunning   = "running"
	codingStepPassed    = "passed"
	codingStepFailed    = "failed"
	codingStepSkipped   = "skipped"
	codingStepVerifyFail = "verify_failed"
)

// codingWorkbenchStepStatus tracks one execution-plan step for sticky + UI.
type codingWorkbenchStepStatus struct {
	Index       int    `json:"index"`
	Title       string `json:"title,omitempty"`
	Status      string `json:"status"` // pending|running|passed|failed|skipped|verify_failed
	Summary     string `json:"summary,omitempty"`
	VerifyCmd   string `json:"verify_cmd,omitempty"`
	VerifyOK    *bool  `json:"verify_ok,omitempty"`
	UpdatedUnix int64  `json:"updated_unix,omitempty"`
}

// codingWorkbenchPendingPlan is a multi-step plan awaiting user approval.
type codingWorkbenchPendingPlan struct {
	UserText  string         `json:"user_text"`
	Markdown  string         `json:"markdown"`
	Tasks     []*v2.TaskItem `json:"tasks"`
	CreatedAt int64          `json:"created_at"`
}

// codingWorkbenchCheckpoint is a lightweight session restore point.
type codingWorkbenchCheckpoint struct {
	Label       string   `json:"label,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	SessionPlan string   `json:"session_plan,omitempty"`
	ExecPlan    string   `json:"execution_plan,omitempty"`
	Files       []string `json:"files,omitempty"`
	// FileSnapshots holds optional text content for disk restore (capped).
	FileSnapshots []codingCheckpointFileSnap `json:"file_snapshots,omitempty"`
	// ProjectPath is the workspace root used when capturing snapshots.
	ProjectPath string `json:"project_path,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}

// codingCheckpointFileSnap is one file's content at checkpoint time.
type codingCheckpointFileSnap struct {
	Path     string `json:"path"`
	Content  string `json:"content,omitempty"` // small files inlined in sticky JSON
	// Sidecar is a relative path under MaclawDataDir/coding_checkpoints for larger text files.
	Sidecar  string `json:"sidecar,omitempty"`
	Bytes    int    `json:"bytes,omitempty"`
	Missing  bool   `json:"missing,omitempty"`
	TooLarge bool   `json:"too_large,omitempty"`
	Binary   bool   `json:"binary,omitempty"`
	Error    string `json:"error,omitempty"`
}

const (
	codingCheckpointMaxSnapFiles    = 20
	codingCheckpointMaxInlineBytes  = 48 * 1024
	codingCheckpointMaxSidecarBytes = 512 * 1024
	codingCheckpointMaxTotalBytes   = 256 * 1024 // total inlined content budget
	// stickyCodingCheckpointHistoryMax is how many prior checkpoints to keep
	// (not counting the current CheckpointJSON).
	stickyCodingCheckpointHistoryMax = 8
)

// codingCheckpointListEntry is a UI/status summary for one checkpoint slot.
type codingCheckpointListEntry struct {
	Label         string `json:"label"`
	Summary       string `json:"summary,omitempty"`
	SessionPlan   string `json:"session_plan,omitempty"`
	FileCount     int    `json:"file_count,omitempty"`
	SnapshotCount int    `json:"snapshot_count,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
	Current       bool   `json:"current,omitempty"`
}

func codingCheckpointSnapshotCount(cp codingWorkbenchCheckpoint) int {
	n := 0
	for _, s := range cp.FileSnapshots {
		if s.Content != "" || strings.TrimSpace(s.Sidecar) != "" {
			n++
		}
	}
	return n
}

func codingCheckpointToListEntry(cp codingWorkbenchCheckpoint, current bool) codingCheckpointListEntry {
	return codingCheckpointListEntry{
		Label:         cp.Label,
		Summary:       truncateRunesForSubAgent(cp.Summary, 160),
		SessionPlan:   truncateRunesForSubAgent(cp.SessionPlan, 120),
		FileCount:     len(cp.Files),
		SnapshotCount: codingCheckpointSnapshotCount(cp),
		CreatedAt:     cp.CreatedAt,
		Current:       current,
	}
}

// stripCodingCheckpointForHistory drops inline file bodies so sticky JSON stays small;
// only sidecar-backed snaps are kept (inline-only snaps cannot restore after strip).
func stripCodingCheckpointForHistory(cp codingWorkbenchCheckpoint) codingWorkbenchCheckpoint {
	stripped := stripCodingCheckpointSnapBodies(cp.FileSnapshots)
	keep := make([]codingCheckpointFileSnap, 0, len(stripped))
	for _, s := range stripped {
		if strings.TrimSpace(s.Sidecar) == "" {
			continue
		}
		// Drop error noise from strip helper when sidecar is present.
		if s.Error == "stripped" {
			s.Error = ""
		}
		keep = append(keep, s)
	}
	cp.FileSnapshots = keep
	cp.Summary = truncateRunesForSubAgent(cp.Summary, 800)
	cp.SessionPlan = truncateRunesForSubAgent(cp.SessionPlan, 2000)
	cp.ExecPlan = truncateRunesForSubAgent(cp.ExecPlan, 2000)
	if len(cp.Files) > 40 {
		cp.Files = cp.Files[:40]
	}
	return cp
}

func normalizeCodingPlanMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case codingPlanModeApprove, "confirm", "gate":
		return codingPlanModeApprove
	case codingPlanModeOff, "none", "disable", "disabled":
		return codingPlanModeOff
	case codingPlanModeAuto, "", "default":
		return codingPlanModeAuto
	default:
		return codingPlanModeAuto
	}
}

func (h *IMMessageHandler) getStickyCodingPlanMode(userID string) string {
	if h == nil {
		return codingPlanModeAuto
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	return normalizeCodingPlanMode(mem.PlanMode)
}

func (h *IMMessageHandler) setStickyCodingPlanMode(userID, mode string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	mode = normalizeCodingPlanMode(mode)
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if mem.PlanMode == mode {
			return
		}
		mem.PlanMode = mode
	})
}

func (h *IMMessageHandler) storeStickyPendingCodingPlan(userID, userText, markdown string, tasks []*v2.TaskItem) {
	h.storeStickyPendingCodingPlanAt(userID, userText, markdown, tasks, 0)
}

// storeStickyPendingCodingPlanAt is like storeStickyPendingCodingPlan but preserves
// createdAt when > 0 (plan edit path).
func (h *IMMessageHandler) storeStickyPendingCodingPlanAt(userID, userText, markdown string, tasks []*v2.TaskItem, createdAt int64) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || len(tasks) == 0 {
		return
	}
	// Clone tasks so later mutation of execution path does not corrupt pending.
	cloned := cloneCodingWorkbenchTasks(tasks)
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	pending := codingWorkbenchPendingPlan{
		UserText:  strings.TrimSpace(userText),
		Markdown:  strings.TrimSpace(markdown),
		Tasks:     cloned,
		CreatedAt: createdAt,
	}
	raw, err := json.Marshal(pending)
	if err != nil {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.PendingPlanJSON = string(raw)
		mem.PendingPlanMarkdown = pending.Markdown
		mem.PendingPlanUserText = pending.UserText
		if pending.Markdown != "" {
			mem.ExecutionPlan = truncateRunesForSubAgent(pending.Markdown, 2000)
		}
		mem.StepStatuses = codingWorkbenchStepsFromTasks(cloned, codingStepPending)
	})
}

func (h *IMMessageHandler) loadStickyPendingCodingPlan(userID string) (codingWorkbenchPendingPlan, bool) {
	if h == nil {
		return codingWorkbenchPendingPlan{}, false
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	raw := strings.TrimSpace(mem.PendingPlanJSON)
	if raw == "" {
		return codingWorkbenchPendingPlan{}, false
	}
	var pending codingWorkbenchPendingPlan
	if err := json.Unmarshal([]byte(raw), &pending); err != nil {
		return codingWorkbenchPendingPlan{}, false
	}
	if len(pending.Tasks) == 0 {
		return codingWorkbenchPendingPlan{}, false
	}
	if strings.TrimSpace(pending.Markdown) == "" {
		pending.Markdown = strings.TrimSpace(mem.PendingPlanMarkdown)
	}
	if strings.TrimSpace(pending.UserText) == "" {
		pending.UserText = strings.TrimSpace(mem.PendingPlanUserText)
	}
	return pending, true
}

func (h *IMMessageHandler) clearStickyPendingCodingPlan(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.PendingPlanJSON = ""
		mem.PendingPlanMarkdown = ""
		mem.PendingPlanUserText = ""
	})
}

func cloneCodingWorkbenchTasks(tasks []*v2.TaskItem) []*v2.TaskItem {
	if len(tasks) == 0 {
		return nil
	}
	cloned := make([]*v2.TaskItem, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		cloned = append(cloned, &v2.TaskItem{
			Index:       t.Index,
			Title:       t.Title,
			Description: t.Description,
			Files:       append([]string(nil), t.Files...),
			DependsOn:   append([]int(nil), t.DependsOn...),
		})
	}
	return cloned
}

// replaceStickyPendingCodingPlanMarkdown rewrites a pending multi-step plan from
// user-edited markdown / numbered steps. Keeps original UserText. Returns the
// updated plan or an error when there is no pending plan / parse yields <2 steps.
func (h *IMMessageHandler) replaceStickyPendingCodingPlanMarkdown(userID, markdown string) (codingWorkbenchPendingPlan, error) {
	if h == nil {
		return codingWorkbenchPendingPlan{}, fmt.Errorf("handler unavailable")
	}
	userID = strings.TrimSpace(userID)
	markdown = strings.TrimSpace(markdown)
	if userID == "" {
		return codingWorkbenchPendingPlan{}, fmt.Errorf("user id required")
	}
	if markdown == "" {
		return codingWorkbenchPendingPlan{}, fmt.Errorf("plan markdown is empty")
	}
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok {
		return codingWorkbenchPendingPlan{}, fmt.Errorf("no pending plan to edit")
	}
	tasks := parseCodingWorkbenchPlan(markdown)
	if len(tasks) < codingWorkbenchPlanMinTasks {
		// Also accept reformat of existing markdown via format helper when parse is thin.
		return codingWorkbenchPendingPlan{}, fmt.Errorf("need at least %d parseable steps (got %d)", codingWorkbenchPlanMinTasks, len(tasks))
	}
	// Normalize indices and markdown for storage / UI.
	for i, t := range tasks {
		if t == nil {
			continue
		}
		if t.Index <= 0 {
			t.Index = i + 1
		}
	}
	md := formatCodingWorkbenchPlanMarkdown(pending.UserText, tasks)
	if strings.TrimSpace(md) == "" {
		md = markdown
	}
	// Preserve original CreatedAt so edit is not a new plan identity.
	createdAt := pending.CreatedAt
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	h.storeStickyPendingCodingPlanAt(userID, pending.UserText, md, tasks, createdAt)
	updated, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok {
		return codingWorkbenchPendingPlan{}, fmt.Errorf("failed to store edited plan")
	}
	return updated, nil
}

func (h *IMMessageHandler) takeStickyApprovedCodingPlan(userID string) (codingWorkbenchPendingPlan, bool) {
	if h == nil {
		return codingWorkbenchPendingPlan{}, false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return codingWorkbenchPendingPlan{}, false
	}
	var pending codingWorkbenchPendingPlan
	ok := false
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		raw := strings.TrimSpace(mem.ApprovedPlanJSON)
		if raw == "" {
			return
		}
		if err := json.Unmarshal([]byte(raw), &pending); err != nil || len(pending.Tasks) == 0 {
			mem.ApprovedPlanJSON = ""
			return
		}
		mem.ApprovedPlanJSON = ""
		ok = true
	})
	if !ok {
		return codingWorkbenchPendingPlan{}, false
	}
	return pending, true
}

func (h *IMMessageHandler) promotePendingToApprovedCodingPlan(userID string) (codingWorkbenchPendingPlan, bool) {
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	if !ok {
		return codingWorkbenchPendingPlan{}, false
	}
	raw, err := json.Marshal(pending)
	if err != nil {
		return codingWorkbenchPendingPlan{}, false
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ApprovedPlanJSON = string(raw)
		mem.PendingPlanJSON = ""
		mem.PendingPlanMarkdown = ""
		mem.PendingPlanUserText = ""
	})
	return pending, true
}

func codingWorkbenchStepsFromTasks(tasks []*v2.TaskItem, status string) []codingWorkbenchStepStatus {
	if status == "" {
		status = codingStepPending
	}
	now := time.Now().Unix()
	out := make([]codingWorkbenchStepStatus, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		out = append(out, codingWorkbenchStepStatus{
			Index:       t.Index,
			Title:       strings.TrimSpace(t.Title),
			Status:      status,
			UpdatedUnix: now,
		})
	}
	return out
}

func (h *IMMessageHandler) setStickyCodingStepStatuses(userID string, steps []codingWorkbenchStepStatus) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.StepStatuses = steps
	})
	h.emitCodingWorkbenchStepsUpdate(userID)
}

func (h *IMMessageHandler) updateStickyCodingStepStatus(userID string, index int, status, summary string) {
	if h == nil || index < 1 {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Memory-only: multi-step runs touch this many times; disk flush at turn end.
	h.updateStickyCodingWorkbenchMemoryOpts(userID, false, func(mem *stickyCodingWorkbenchMemory) {
		now := time.Now().Unix()
		found := false
		for i := range mem.StepStatuses {
			if mem.StepStatuses[i].Index == index {
				mem.StepStatuses[i].Status = status
				if s := strings.TrimSpace(summary); s != "" {
					mem.StepStatuses[i].Summary = truncateRunesForSubAgent(s, 400)
				}
				mem.StepStatuses[i].UpdatedUnix = now
				found = true
				break
			}
		}
		if !found {
			mem.StepStatuses = append(mem.StepStatuses, codingWorkbenchStepStatus{
				Index:       index,
				Status:      status,
				Summary:     truncateRunesForSubAgent(strings.TrimSpace(summary), 400),
				UpdatedUnix: now,
			})
		}
	})
	h.emitCodingWorkbenchStepsUpdate(userID)
}

func (h *IMMessageHandler) updateStickyCodingStepVerify(userID string, index int, cmd string, ok bool, summary string) {
	if h == nil || index < 1 {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Verify outcomes matter for UI; persist so crash mid-turn keeps last gate result.
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		now := time.Now().Unix()
		status := codingStepPassed
		if !ok {
			status = codingStepVerifyFail
		}
		found := false
		for i := range mem.StepStatuses {
			if mem.StepStatuses[i].Index == index {
				mem.StepStatuses[i].Status = status
				mem.StepStatuses[i].VerifyCmd = strings.TrimSpace(cmd)
				v := ok
				mem.StepStatuses[i].VerifyOK = &v
				if s := strings.TrimSpace(summary); s != "" {
					mem.StepStatuses[i].Summary = truncateRunesForSubAgent(s, 400)
				}
				mem.StepStatuses[i].UpdatedUnix = now
				found = true
				break
			}
		}
		if !found {
			v := ok
			mem.StepStatuses = append(mem.StepStatuses, codingWorkbenchStepStatus{
				Index:       index,
				Status:      status,
				VerifyCmd:   strings.TrimSpace(cmd),
				VerifyOK:    &v,
				Summary:     truncateRunesForSubAgent(strings.TrimSpace(summary), 400),
				UpdatedUnix: now,
			})
		}
	})
	h.emitCodingWorkbenchStepsUpdate(userID)
}

func (h *IMMessageHandler) clearStickyCodingStepStatuses(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.StepStatuses = nil
	})
	h.emitCodingWorkbenchStepsUpdate(userID)
}

// emitCodingWorkbenchStepsUpdate pushes live Todo checklist state to the GUI
// (Codex / Claude Code style: pending ○ → running … → passed ☑).
// Safe no-op without Wails event bus (tests / headless).
func (h *IMMessageHandler) emitCodingWorkbenchStepsUpdate(userID string) {
	if h == nil || h.app == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	projectPath := projectPathFromSessionOwnerID(userID)
	if projectPath == "" {
		projectPath = strings.TrimSpace(mem.ProjectPath)
	}
	steps := append([]codingWorkbenchStepStatus(nil), mem.StepStatuses...)
	payload := map[string]interface{}{
		"user_id":       userID,
		"project_path":  projectPath,
		"step_statuses": steps,
		"execution_plan": strings.TrimSpace(mem.ExecutionPlan),
	}
	h.app.emitEvent("coding-workbench-steps", payload)
}

// markRemainingCodingStepsSkipped marks later plan steps as skipped after a
// hard failure (sequential plan stops). Mirrors TaskRunner dependency skip UX.
//
// Also rewrites premature agent-side "passed"/"running" marks on later steps
// (e.g. todo_write mirrored into UI before orchestrator isolation, or a rushed
// subagent that claimed later work done). Terminal failed/verify_failed/skipped
// statuses are left alone.
func (h *IMMessageHandler) markRemainingCodingStepsSkipped(userID string, afterIndex int, reason string) {
	if h == nil || afterIndex < 0 {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "skipped: prior step failed"
	}
	changed := false
	h.updateStickyCodingWorkbenchMemoryOpts(userID, false, func(mem *stickyCodingWorkbenchMemory) {
		now := time.Now().Unix()
		for i := range mem.StepStatuses {
			st := &mem.StepStatuses[i]
			if st.Index <= afterIndex {
				continue
			}
			switch st.Status {
			case codingStepPending, "", codingStepRunning, codingStepPassed:
				st.Status = codingStepSkipped
				st.Summary = truncateRunesForSubAgent(reason, 400)
				st.UpdatedUnix = now
				changed = true
			}
		}
	})
	if changed {
		h.emitCodingWorkbenchStepsUpdate(userID)
	}
}

// formatCodingStepsChecklist renders a compact Claude Code-style checklist
// for streaming into the chat when a multi-step plan is active.
func formatCodingStepsChecklist(steps []codingWorkbenchStepStatus) string {
	if len(steps) == 0 {
		return ""
	}
	// Stable order by index.
	ordered := append([]codingWorkbenchStepStatus(nil), steps...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Index < ordered[j].Index
	})
	var b strings.Builder
	b.WriteString("执行步骤：\n")
	for _, st := range ordered {
		mark := "☐"
		switch st.Status {
		case codingStepPassed:
			mark = "☑"
		case codingStepRunning:
			mark = "…"
		case codingStepFailed, codingStepVerifyFail:
			mark = "✗"
		case codingStepSkipped:
			mark = "–"
		}
		title := strings.TrimSpace(st.Title)
		if title == "" {
			title = st.Status
		}
		b.WriteString(fmt.Sprintf("%s T%d %s\n", mark, st.Index, title))
	}
	return strings.TrimRight(b.String(), "\n")
}

// saveStickyCodingCheckpoint captures a restore point for the pure-coding session.
// Also best-effort captures text file content for later disk restore (capped).
// Previous current checkpoint is archived into CheckpointHistoryJSON (ring).
func (h *IMMessageHandler) saveStickyCodingCheckpoint(userID, label string) codingWorkbenchCheckpoint {
	if h == nil {
		return codingWorkbenchCheckpoint{}
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return codingWorkbenchCheckpoint{}
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = fmt.Sprintf("checkpoint-%s", time.Now().Format("150405"))
	}
	// Read sticky first (outside mutate) so we can snapshot files without holding lock on I/O.
	mem := h.getStickyCodingWorkbenchMemory(userID)
	files := uniqueSortedSubAgentStrings(append(append([]string{}, mem.FilesModified...), mem.FilesCreated...))
	if len(files) > 30 {
		files = files[:30]
	}
	projectPath := strings.TrimSpace(mem.ProjectPath)
	hooks := loadCodingWorkbenchHooks(projectPath)
	if preCP := runCodingWorkbenchHookPhase(projectPath, hooks, "pre_checkpoint"); preCP.Report != "" {
		log.Printf("[coding-hooks] pre_checkpoint: %s", truncateRunesForSubAgent(preCP.Report, 160))
	}
	// Checkpoint save still proceeds even if pre_checkpoint fails (best-effort capture).
	snaps := captureCodingCheckpointFileSnapshots(userID, label, projectPath, files)

	var cp codingWorkbenchCheckpoint
	h.updateStickyCodingWorkbenchMemory(userID, func(m *stickyCodingWorkbenchMemory) {
		// Archive previous current into history before overwrite.
		if prevRaw := strings.TrimSpace(m.CheckpointJSON); prevRaw != "" {
			var prev codingWorkbenchCheckpoint
			if err := json.Unmarshal([]byte(prevRaw), &prev); err == nil && strings.TrimSpace(prev.Label) != "" {
				// Skip archiving if same label is being overwritten (replace in place).
				if sanitizeCodingCheckpointLabel(prev.Label) != sanitizeCodingCheckpointLabel(label) {
					m.CheckpointHistoryJSON = appendStickyCodingCheckpointHistoryJSON(m.CheckpointHistoryJSON, stripCodingCheckpointForHistory(prev))
				}
			}
		}
		cp = codingWorkbenchCheckpoint{
			Label:         label,
			Summary:       truncateRunesForSubAgent(m.LastSummary, 800),
			SessionPlan:   m.SessionPlan,
			ExecPlan:      m.ExecutionPlan,
			Files:         files,
			FileSnapshots: snaps,
			ProjectPath:   projectPath,
			CreatedAt:     time.Now().Unix(),
		}
		raw, err := json.Marshal(cp)
		if err != nil {
			return
		}
		// Guard sticky disk bloat: drop snapshot bodies if JSON is huge.
		if len(raw) > codingCheckpointMaxTotalBytes*2 {
			cp.FileSnapshots = stripCodingCheckpointSnapBodies(cp.FileSnapshots)
			raw, err = json.Marshal(cp)
			if err != nil {
				return
			}
		}
		m.CheckpointJSON = string(raw)
		m.CheckpointLabel = cp.Label
		m.CheckpointAtUnix = cp.CreatedAt
		// Drop history entry with same label as new current (current wins).
		m.CheckpointHistoryJSON = removeStickyCodingCheckpointHistoryLabel(m.CheckpointHistoryJSON, label)
	})
	// Keep sidecars for current + history labels; drop the rest.
	keep := h.stickyCodingCheckpointKeepLabels(userID)
	if _, err := pruneCodingCheckpointSidecarsKeep(userID, keep); err != nil {
		// best-effort
	}
	if postCP := runCodingWorkbenchHookPhase(projectPath, hooks, "post_checkpoint"); postCP.Report != "" {
		log.Printf("[coding-hooks] post_checkpoint: %s", truncateRunesForSubAgent(postCP.Report, 160))
	}
	return cp
}

func (h *IMMessageHandler) loadStickyCodingCheckpoint(userID string) (codingWorkbenchCheckpoint, bool) {
	if h == nil {
		return codingWorkbenchCheckpoint{}, false
	}
	cur, ok, _ := stickyCodingCheckpointsFromMem(h.getStickyCodingWorkbenchMemory(userID))
	return cur, ok
}

// stickyCodingCheckpointsFromMem parses current + history from one sticky snapshot
// (avoids double getSticky/unmarshal on status polls).
func stickyCodingCheckpointsFromMem(mem stickyCodingWorkbenchMemory) (current codingWorkbenchCheckpoint, hasCurrent bool, history []codingWorkbenchCheckpoint) {
	if raw := strings.TrimSpace(mem.CheckpointJSON); raw != "" {
		var cp codingWorkbenchCheckpoint
		if err := json.Unmarshal([]byte(raw), &cp); err == nil && strings.TrimSpace(cp.Label) != "" {
			current = cp
			hasCurrent = true
		}
	}
	history = parseStickyCodingCheckpointHistoryJSON(mem.CheckpointHistoryJSON)
	return current, hasCurrent, history
}

// loadStickyCodingCheckpointHistory returns prior checkpoints (newest last).
func (h *IMMessageHandler) loadStickyCodingCheckpointHistory(userID string) []codingWorkbenchCheckpoint {
	if h == nil {
		return nil
	}
	_, _, hist := stickyCodingCheckpointsFromMem(h.getStickyCodingWorkbenchMemory(userID))
	return hist
}

func parseStickyCodingCheckpointHistoryJSON(raw string) []codingWorkbenchCheckpoint {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var list []codingWorkbenchCheckpoint
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}

func appendStickyCodingCheckpointHistoryJSON(raw string, cp codingWorkbenchCheckpoint) string {
	list := parseStickyCodingCheckpointHistoryJSON(raw)
	// Replace same label if present.
	san := sanitizeCodingCheckpointLabel(cp.Label)
	out := make([]codingWorkbenchCheckpoint, 0, len(list)+1)
	for _, x := range list {
		if sanitizeCodingCheckpointLabel(x.Label) == san {
			continue
		}
		out = append(out, x)
	}
	out = append(out, cp)
	if len(out) > stickyCodingCheckpointHistoryMax {
		out = out[len(out)-stickyCodingCheckpointHistoryMax:]
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return string(b)
}

func removeStickyCodingCheckpointHistoryLabel(raw, label string) string {
	list := parseStickyCodingCheckpointHistoryJSON(raw)
	if len(list) == 0 {
		return raw
	}
	san := sanitizeCodingCheckpointLabel(label)
	out := make([]codingWorkbenchCheckpoint, 0, len(list))
	for _, x := range list {
		if sanitizeCodingCheckpointLabel(x.Label) == san {
			continue
		}
		out = append(out, x)
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return string(b)
}

// listStickyCodingCheckpoints returns current first, then history newest-first.
func (h *IMMessageHandler) listStickyCodingCheckpoints(userID string) []codingCheckpointListEntry {
	if h == nil {
		return nil
	}
	return listStickyCodingCheckpointsFromMem(h.getStickyCodingWorkbenchMemory(userID))
}

func listStickyCodingCheckpointsFromMem(mem stickyCodingWorkbenchMemory) []codingCheckpointListEntry {
	cur, hasCur, hist := stickyCodingCheckpointsFromMem(mem)
	var out []codingCheckpointListEntry
	if hasCur {
		out = append(out, codingCheckpointToListEntry(cur, true))
	}
	for i := len(hist) - 1; i >= 0; i-- {
		out = append(out, codingCheckpointToListEntry(hist[i], false))
	}
	return out
}

// loadStickyCodingCheckpointByLabel finds current or history by label (sanitized match).
func (h *IMMessageHandler) loadStickyCodingCheckpointByLabel(userID, label string) (codingWorkbenchCheckpoint, bool) {
	if h == nil {
		return codingWorkbenchCheckpoint{}, false
	}
	label = strings.TrimSpace(label)
	cur, hasCur, hist := stickyCodingCheckpointsFromMem(h.getStickyCodingWorkbenchMemory(userID))
	if label == "" {
		return cur, hasCur
	}
	san := sanitizeCodingCheckpointLabel(label)
	if hasCur && (sanitizeCodingCheckpointLabel(cur.Label) == san || strings.EqualFold(cur.Label, label)) {
		return cur, true
	}
	for _, cp := range hist {
		if sanitizeCodingCheckpointLabel(cp.Label) == san || strings.EqualFold(cp.Label, label) {
			return cp, true
		}
	}
	return codingWorkbenchCheckpoint{}, false
}

func (h *IMMessageHandler) stickyCodingCheckpointKeepLabels(userID string) []string {
	if h == nil {
		return nil
	}
	cur, hasCur, hist := stickyCodingCheckpointsFromMem(h.getStickyCodingWorkbenchMemory(userID))
	var labels []string
	seen := map[string]struct{}{}
	add := func(lab string) {
		lab = strings.TrimSpace(lab)
		if lab == "" {
			return
		}
		san := sanitizeCodingCheckpointLabel(lab)
		if _, ok := seen[san]; ok {
			return
		}
		seen[san] = struct{}{}
		labels = append(labels, lab)
	}
	if hasCur {
		add(cur.Label)
	}
	for _, cp := range hist {
		add(cp.Label)
	}
	return labels
}

// restoreStickyCodingCheckpoint restores SessionPlan/ExecutionPlan from the last checkpoint.
// Pass restoreFiles=true to also rewrite disk files from FileSnapshots when available.
func (h *IMMessageHandler) restoreStickyCodingCheckpoint(userID string) (codingWorkbenchCheckpoint, bool) {
	return h.restoreStickyCodingCheckpointOpts(userID, false)
}

func (h *IMMessageHandler) restoreStickyCodingCheckpointOpts(userID string, restoreFiles bool) (codingWorkbenchCheckpoint, bool) {
	return h.restoreStickyCodingCheckpointByLabel(userID, "", restoreFiles)
}

// restoreStickyCodingCheckpointByLabel restores plan (and optional files) from a named checkpoint.
// Empty label restores the current checkpoint.
func (h *IMMessageHandler) restoreStickyCodingCheckpointByLabel(userID, label string, restoreFiles bool) (codingWorkbenchCheckpoint, bool) {
	cp, ok := h.loadStickyCodingCheckpointByLabel(userID, label)
	if !ok {
		return codingWorkbenchCheckpoint{}, false
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if s := strings.TrimSpace(cp.SessionPlan); s != "" {
			mem.SessionPlan = s
		}
		if s := strings.TrimSpace(cp.ExecPlan); s != "" {
			mem.ExecutionPlan = s
		}
		if s := strings.TrimSpace(cp.Summary); s != "" {
			mem.LastSummary = "Restored from checkpoint " + cp.Label + ":\n" + s
		}
	})
	if restoreFiles {
		_, _, _ = h.applyCodingCheckpointFileSnapshots(userID, cp, nil)
	}
	return cp, true
}

// applyCodingCheckpointFileSnapshots writes snapshotted file contents back to disk.
// onlyFiles nil/empty means all snaps with content. Returns restored/skipped counts.
func (h *IMMessageHandler) applyCodingCheckpointFileSnapshots(userID string, cp codingWorkbenchCheckpoint, onlyFiles []string) (restored, skipped int, err error) {
	if len(cp.FileSnapshots) == 0 {
		return 0, 0, fmt.Errorf("checkpoint has no file snapshots (save a new checkpoint after edits)")
	}
	projectPath := strings.TrimSpace(cp.ProjectPath)
	if projectPath == "" && h != nil {
		projectPath = strings.TrimSpace(h.getStickyCodingWorkbenchMemory(userID).ProjectPath)
	}
	if projectPath == "" {
		return 0, 0, fmt.Errorf("project path unknown for file restore")
	}
	filter := map[string]struct{}{}
	for _, f := range onlyFiles {
		f = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(f, "./")))
		if f != "" {
			filter[f] = struct{}{}
		}
	}
	for _, snap := range cp.FileSnapshots {
		rel := filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(snap.Path, "./")))
		if rel == "" {
			skipped++
			continue
		}
		if len(filter) > 0 {
			if _, ok := filter[rel]; !ok {
				continue
			}
		}
		if snap.Missing || snap.TooLarge || snap.Binary || snap.Error != "" {
			skipped++
			continue
		}
		body := snap.Content
		if body == "" && strings.TrimSpace(snap.Sidecar) != "" {
			scAbs := resolveCodingCheckpointSidecarPath(userID, snap.Sidecar)
			if scAbs == "" {
				skipped++
				continue
			}
			data, rerr := os.ReadFile(scAbs)
			if rerr != nil {
				skipped++
				continue
			}
			body = string(data)
		}
		if body == "" {
			skipped++
			continue
		}
		// Prevent path escape.
		abs := filepath.Clean(filepath.Join(projectPath, filepath.FromSlash(rel)))
		if !isPathInsideRoot(projectPath, abs) {
			skipped++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return restored, skipped, err
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			return restored, skipped, fmt.Errorf("write %s: %w", rel, err)
		}
		restored++
	}
	if restored == 0 && len(filter) > 0 {
		return 0, skipped, fmt.Errorf("no matching snapshot content for selected files")
	}
	if restored == 0 {
		return 0, skipped, fmt.Errorf("no restorable file snapshots")
	}
	return restored, skipped, nil
}

func captureCodingCheckpointFileSnapshots(userID, label, projectPath string, relPaths []string) []codingCheckpointFileSnap {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" || len(relPaths) == 0 {
		return nil
	}
	total := 0
	capN := len(relPaths)
	if capN > codingCheckpointMaxSnapFiles {
		capN = codingCheckpointMaxSnapFiles
	}
	out := make([]codingCheckpointFileSnap, 0, capN)
	sidecarDir := codingCheckpointSidecarDir(userID, label)
	for _, rel := range relPaths {
		if len(out) >= codingCheckpointMaxSnapFiles {
			break
		}
		rel = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(rel, "./")))
		if rel == "" || strings.Contains(rel, "..") {
			continue
		}
		abs := filepath.Clean(filepath.Join(projectPath, filepath.FromSlash(rel)))
		if !isPathInsideRoot(projectPath, abs) {
			continue
		}
		snap := codingCheckpointFileSnap{Path: rel}
		info, err := os.Stat(abs)
		if err != nil {
			snap.Missing = true
			if !os.IsNotExist(err) {
				snap.Error = "stat_failed"
			}
			out = append(out, snap)
			continue
		}
		if info.IsDir() {
			snap.Error = "is_dir"
			out = append(out, snap)
			continue
		}
		if info.Size() > int64(codingCheckpointMaxSidecarBytes) {
			snap.TooLarge = true
			snap.Bytes = int(info.Size())
			out = append(out, snap)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			snap.Error = "read_failed"
			out = append(out, snap)
			continue
		}
		if IsBinaryFile(data) {
			snap.Binary = true
			snap.Bytes = len(data)
			out = append(out, snap)
			continue
		}
		snap.Bytes = len(data)
		// Prefer inline when small and under total budget.
		if len(data) <= codingCheckpointMaxInlineBytes && total+len(data) <= codingCheckpointMaxTotalBytes {
			snap.Content = string(data)
			total += len(data)
			out = append(out, snap)
			continue
		}
		// Sidecar for larger text files (or when inline budget exhausted).
		scRel, werr := writeCodingCheckpointSidecar(sidecarDir, rel, data)
		if werr != nil {
			snap.TooLarge = true
			snap.Error = "sidecar_write"
			out = append(out, snap)
			continue
		}
		snap.Sidecar = scRel
		out = append(out, snap)
	}
	return out
}

func codingCheckpointSidecarRoot() string {
	return filepath.Join(corelib.MaclawDataDir(), "coding_checkpoints")
}

func codingCheckpointUserKey(userID string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(userID)))
	return hex.EncodeToString(sum[:8])
}

func sanitizeCodingCheckpointLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "default"
	}
	var b strings.Builder
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	safe := b.String()
	if safe == "" {
		safe = "cp"
	}
	if len(safe) > 40 {
		safe = safe[:40]
	}
	return safe
}

func codingCheckpointSidecarDir(userID, label string) string {
	return filepath.Join(codingCheckpointSidecarRoot(), codingCheckpointUserKey(userID), sanitizeCodingCheckpointLabel(label))
}

// pruneCodingCheckpointSidecars removes label directories under the user's
// checkpoint sidecar root that are not keepLabel. When keepLabel is empty,
// the entire user directory is removed. Returns number of removed entries.
func pruneCodingCheckpointSidecars(userID, keepLabel string) (int, error) {
	if strings.TrimSpace(keepLabel) == "" {
		return pruneCodingCheckpointSidecarsKeep(userID, nil)
	}
	return pruneCodingCheckpointSidecarsKeep(userID, []string{keepLabel})
}

// pruneCodingCheckpointSidecarsKeep keeps multiple labels (current + history).
// Empty keepLabels wipes the user bucket.
func pruneCodingCheckpointSidecarsKeep(userID string, keepLabels []string) (int, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return 0, nil
	}
	userDir := filepath.Join(codingCheckpointSidecarRoot(), codingCheckpointUserKey(userID))
	info, err := os.Stat(userDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		_ = os.Remove(userDir)
		return 1, nil
	}
	keep := map[string]struct{}{}
	for _, lab := range keepLabels {
		lab = strings.TrimSpace(lab)
		if lab == "" {
			continue
		}
		keep[sanitizeCodingCheckpointLabel(lab)] = struct{}{}
	}
	if len(keep) == 0 {
		if err := os.RemoveAll(userDir); err != nil {
			return 0, err
		}
		return 1, nil
	}
	entries, err := os.ReadDir(userDir)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if _, ok := keep[name]; ok {
			continue
		}
		path := filepath.Join(userDir, name)
		if err := os.RemoveAll(path); err != nil {
			continue
		}
		removed++
	}
	if remaining, _ := os.ReadDir(userDir); len(remaining) == 0 {
		_ = os.Remove(userDir)
	}
	return removed, nil
}

// pruneCodingCheckpointSidecarOrphans removes empty dirs and aged user buckets
// under the global sidecar root (best-effort GC). maxAge<=0 uses 14 days.
func pruneCodingCheckpointSidecarOrphans(maxAge time.Duration) (int, error) {
	n, _, err := pruneCodingCheckpointSidecarOrphansAndBudget("", maxAge)
	return n, err
}

// pruneCodingCheckpointSidecarOrphansAndBudget runs orphan GC then size-budget
// eviction with at most two scans of the sidecar tree (one after orphan cleanup).
// protectRel is passed to budget enforcement. Returns (orphanRemoved, budgetRemoved, err).
func pruneCodingCheckpointSidecarOrphansAndBudget(protectRel string, maxAge time.Duration) (int, int, error) {
	if maxAge <= 0 {
		maxAge = 14 * 24 * time.Hour
	}
	root := codingCheckpointSidecarRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	orphanRemoved := 0
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if !e.IsDir() {
			if info.ModTime().Before(cutoff) {
				if os.Remove(path) == nil {
					orphanRemoved++
				}
			}
			continue
		}
		sub, _ := os.ReadDir(path)
		if len(sub) == 0 {
			if os.Remove(path) == nil {
				orphanRemoved++
			}
			continue
		}
		if info.ModTime().Before(cutoff) {
			stale := true
			for _, s := range sub {
				si, _ := s.Info()
				if si != nil && si.ModTime().After(cutoff) {
					stale = false
					break
				}
			}
			if stale {
				if os.RemoveAll(path) == nil {
					orphanRemoved++
				}
			}
		}
	}
	// Single budget scan after orphan cleanup.
	budgetRemoved, berr := enforceCodingCheckpointSidecarBudget(protectRel)
	return orphanRemoved, budgetRemoved, berr
}

// pruneStickyCodingCheckpointSidecars keeps current + history checkpoint labels
// for the user, then runs a light global orphan GC and size budget
// with a single scan of the sidecar root (avoids triple Walk).
func (h *IMMessageHandler) pruneStickyCodingCheckpointSidecars(userID string) (userRemoved, orphanRemoved int) {
	var keepLabels []string
	if h != nil {
		keepLabels = h.stickyCodingCheckpointKeepLabels(userID)
	}
	if len(keepLabels) == 0 {
		n, _ := pruneCodingCheckpointSidecarsKeep(userID, nil)
		userRemoved = n
		o, b, _ := pruneCodingCheckpointSidecarOrphansAndBudget("", 0)
		orphanRemoved = o + b
		return userRemoved, orphanRemoved
	}
	n, _ := pruneCodingCheckpointSidecarsKeep(userID, keepLabels)
	userRemoved = n
	// Protect newest (current) label under budget eviction.
	protect := ""
	if userID != "" {
		protect = filepath.ToSlash(filepath.Join(codingCheckpointUserKey(userID), sanitizeCodingCheckpointLabel(keepLabels[0])))
	}
	o, b, _ := pruneCodingCheckpointSidecarOrphansAndBudget(protect, 0)
	orphanRemoved = o + b
	return userRemoved, orphanRemoved
}

// codingCheckpointSidecarDefaultMaxBytes is the default soft global cap (~256 MiB).
const codingCheckpointSidecarDefaultMaxBytes int64 = 256 * 1024 * 1024
const codingCheckpointSidecarMinMaxBytes int64 = 32 * 1024 * 1024

// codingCheckpointSidecarBudgetBytes holds the live cap (updated from AppConfig).
// 0 means use default.
var codingCheckpointSidecarBudgetBytes atomic.Int64

func codingCheckpointSidecarMaxBytes() int64 {
	if v := codingCheckpointSidecarBudgetBytes.Load(); v > 0 {
		if v < codingCheckpointSidecarMinMaxBytes {
			return codingCheckpointSidecarMinMaxBytes
		}
		return v
	}
	return codingCheckpointSidecarDefaultMaxBytes
}

// setCodingCheckpointSidecarMaxMB applies a config MB value to the live budget.
// mb<=0 resets to default.
func setCodingCheckpointSidecarMaxMB(mb int) {
	if mb <= 0 {
		codingCheckpointSidecarBudgetBytes.Store(0)
		return
	}
	if mb < 32 {
		mb = 32
	}
	if mb > 8192 {
		mb = 8192
	}
	codingCheckpointSidecarBudgetBytes.Store(int64(mb) * 1024 * 1024)
}

type codingSidecarDirStat struct {
	path    string
	rel     string // relative to sidecar root
	modTime time.Time
	size    int64
}

// enforceCodingCheckpointSidecarBudget deletes oldest label dirs until total
// size is under the live budget. protectRel is a root-relative path
// (userKey/label) that must not be deleted. maxBytesOverride>0 forces a cap
// (for tests); otherwise uses codingCheckpointSidecarMaxBytes().
func enforceCodingCheckpointSidecarBudget(protectRel string) (int, error) {
	return enforceCodingCheckpointSidecarBudgetWithCap(protectRel, 0)
}

func enforceCodingCheckpointSidecarBudgetWithCap(protectRel string, maxBytesOverride int64) (int, error) {
	root := codingCheckpointSidecarRoot()
	total, dirs, err := scanCodingCheckpointSidecarDirs(root)
	if err != nil {
		return 0, err
	}
	capBytes := maxBytesOverride
	if capBytes <= 0 {
		capBytes = codingCheckpointSidecarMaxBytes()
	}
	if total <= capBytes {
		return 0, nil
	}
	protectRel = filepath.ToSlash(strings.TrimSpace(protectRel))
	// Sort oldest first.
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].modTime.Equal(dirs[j].modTime) {
			return dirs[i].rel < dirs[j].rel
		}
		return dirs[i].modTime.Before(dirs[j].modTime)
	})
	removed := 0
	for _, d := range dirs {
		if total <= capBytes {
			break
		}
		if protectRel != "" && (d.rel == protectRel || strings.HasPrefix(d.rel, protectRel+"/")) {
			continue
		}
		if err := os.RemoveAll(d.path); err != nil {
			continue
		}
		total -= d.size
		if total < 0 {
			total = 0
		}
		removed++
	}
	return removed, nil
}

func scanCodingCheckpointSidecarDirs(root string) (total int64, dirs []codingSidecarDirStat, err error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	for _, userEnt := range entries {
		if !userEnt.IsDir() {
			continue
		}
		userPath := filepath.Join(root, userEnt.Name())
		labels, lerr := os.ReadDir(userPath)
		if lerr != nil {
			continue
		}
		for _, lab := range labels {
			if !lab.IsDir() {
				continue
			}
			labPath := filepath.Join(userPath, lab.Name())
			sz, mt := dirSizeAndMTime(labPath)
			total += sz
			rel := filepath.ToSlash(filepath.Join(userEnt.Name(), lab.Name()))
			dirs = append(dirs, codingSidecarDirStat{path: labPath, rel: rel, modTime: mt, size: sz})
		}
	}
	return total, dirs, nil
}

func dirSizeAndMTime(root string) (int64, time.Time) {
	var size int64
	var latest time.Time
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	if latest.IsZero() {
		if info, err := os.Stat(root); err == nil {
			latest = info.ModTime()
		}
	}
	return size, latest
}

// codingCheckpointSidecarStats is usage telemetry for the checkpoint sidecar store.
type codingCheckpointSidecarStats struct {
	TotalBytes   int64   `json:"total_bytes"`
	MaxBytes     int64   `json:"max_bytes"`
	UsageRatio   float64 `json:"usage_ratio,omitempty"` // 0..1+
	DirCount     int     `json:"dir_count"`
	UserBytes    int64   `json:"user_bytes,omitempty"`
	UserDirCount int     `json:"user_dir_count,omitempty"`
	UserKey      string  `json:"user_key,omitempty"`
	KeepLabel    string  `json:"keep_label,omitempty"`
}

// collectCodingCheckpointSidecarStats returns global + optional per-user usage.
func collectCodingCheckpointSidecarStats(userID, keepLabel string) codingCheckpointSidecarStats {
	st := codingCheckpointSidecarStats{
		MaxBytes:  codingCheckpointSidecarMaxBytes(),
		KeepLabel: strings.TrimSpace(keepLabel),
	}
	total, dirs, err := scanCodingCheckpointSidecarDirs(codingCheckpointSidecarRoot())
	if err != nil {
		return st
	}
	st.TotalBytes = total
	st.DirCount = len(dirs)
	if st.MaxBytes > 0 {
		st.UsageRatio = float64(st.TotalBytes) / float64(st.MaxBytes)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return st
	}
	uk := codingCheckpointUserKey(userID)
	st.UserKey = uk
	prefix := uk + "/"
	for _, d := range dirs {
		if d.rel == uk || strings.HasPrefix(d.rel, prefix) {
			st.UserBytes += d.size
			st.UserDirCount++
		}
	}
	return st
}

func formatCodingCheckpointSidecarStatsLine(st codingCheckpointSidecarStats) string {
	mb := func(b int64) float64 { return float64(b) / (1024 * 1024) }
	line := fmt.Sprintf("sidecar: %.1f / %.0f MB (%d labels)", mb(st.TotalBytes), mb(st.MaxBytes), st.DirCount)
	if st.UserDirCount > 0 || st.UserBytes > 0 {
		line += fmt.Sprintf(" · you: %.1f MB / %d", mb(st.UserBytes), st.UserDirCount)
	}
	if st.KeepLabel != "" {
		line += " · keep=" + st.KeepLabel
	}
	if st.UsageRatio >= 0.85 {
		line += " · near cap"
	}
	return line
}

func writeCodingCheckpointSidecar(dir, rel string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Hash relative path for filename stability.
	sum := sha1.Sum([]byte(filepath.ToSlash(rel)))
	name := hex.EncodeToString(sum[:12]) + ".txt"
	abs := filepath.Join(dir, name)
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", err
	}
	// Return path relative to coding_checkpoints root.
	root := codingCheckpointSidecarRoot()
	relOut, err := filepath.Rel(root, abs)
	if err != nil {
		return name, nil
	}
	return filepath.ToSlash(relOut), nil
}

func resolveCodingCheckpointSidecarPath(userID, sidecarRel string) string {
	_ = userID
	sidecarRel = filepath.ToSlash(strings.TrimSpace(sidecarRel))
	if sidecarRel == "" || strings.Contains(sidecarRel, "..") {
		return ""
	}
	root := codingCheckpointSidecarRoot()
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(sidecarRel)))
	if !isPathInsideRoot(root, abs) {
		return ""
	}
	return abs
}

func stripCodingCheckpointSnapBodies(snaps []codingCheckpointFileSnap) []codingCheckpointFileSnap {
	if len(snaps) == 0 {
		return nil
	}
	out := make([]codingCheckpointFileSnap, len(snaps))
	for i, s := range snaps {
		s.Content = ""
		// Keep Sidecar references so file restore still works after JSON strip.
		if s.Sidecar == "" && !s.TooLarge && !s.Binary && !s.Missing && s.Error == "" {
			s.Error = "stripped"
		}
		out[i] = s
	}
	return out
}

// isPathInsideRoot reports whether abs is within root (after Clean).
func isPathInsideRoot(root, abs string) bool {
	root = filepath.Clean(root)
	abs = filepath.Clean(abs)
	if root == "" || abs == "" {
		return false
	}
	sep := string(filepath.Separator)
	return abs == root || strings.HasPrefix(abs, root+sep)
}

func codingPlanApproveActions() []IMResponseAction {
	return []IMResponseAction{
		{Label: "批准并执行", Command: "/plan approve", Style: "primary"},
		{Label: "跳过规划（直接执行）", Command: "/plan skip", Style: "default"},
		{Label: "拒绝此规划", Command: "/plan reject", Style: "danger"},
		{Label: "改为自动规划", Command: "/plan mode auto", Style: "secondary"},
	}
}

func formatPendingPlanApprovalText(markdown string, stepCount int) string {
	var b strings.Builder
	b.WriteString("## 执行计划待批准\n\n")
	b.WriteString(fmt.Sprintf("已生成 **%d** 步执行计划。请确认后开始执行（当前为计划批准模式）。\n\n", stepCount))
	if strings.TrimSpace(markdown) != "" {
		b.WriteString(markdown)
		b.WriteString("\n\n")
	}
	b.WriteString("可用命令：`/plan approve` · `/plan skip` · `/plan reject` · `/plan mode auto|approve|off`\n")
	return b.String()
}
