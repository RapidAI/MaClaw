package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// desktopTurnResult is the reply a background desktop run finished with.
// The admitting HTTP call has already returned, so the screenshot and the
// handoff cannot ride on that response. A later read of the run returns them.
// The picture stays in memory: the stored message is text only.
type desktopTurnResult struct {
	Ready     bool
	Handoff   bool
	Attention string
	Message   *agentservice.Message
	StoredAt  time.Time
}

const desktopTurnResultTTL = 6 * time.Hour

var (
	desktopTurnResults           sync.Map
	desktopTurnResultCleanupOnce sync.Once
)

func storeDesktopTurnResult(run *agentservice.Run, msg *agentservice.Message, handoff bool, attention string) {
	if run == nil || run.ID == "" {
		return
	}
	var copied *agentservice.Message
	if msg != nil {
		clone := *msg
		if len(msg.Attachments) > 0 {
			clone.Attachments = append([]agent.MessageAttachment(nil), msg.Attachments...)
		}
		if len(msg.Metadata) > 0 {
			meta := make(map[string]string, len(msg.Metadata))
			for key, value := range msg.Metadata {
				meta[key] = value
			}
			clone.Metadata = meta
		}
		copied = &clone
	}
	clearDesktopActivity(run.ID)
	desktopTurnResults.Store(run.ID, desktopTurnResult{
		Ready:     true,
		Handoff:   handoff,
		Attention: attention,
		Message:   copied,
		StoredAt:  time.Now(),
	})
	desktopTurnResultCleanupOnce.Do(func() {
		go runDesktopTurnResultCleanup()
	})
	cleanupDesktopTurnResults(time.Now())
}

func runDesktopTurnResultCleanup() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		cleanupDesktopTurnResults(now)
	}
}

func cleanupDesktopTurnResults(now time.Time) {
	cutoff := now.Add(-desktopTurnResultTTL)
	desktopTurnResults.Range(func(key, value any) bool {
		item, _ := value.(desktopTurnResult)
		if !item.StoredAt.IsZero() && item.StoredAt.Before(cutoff) {
			desktopTurnResults.Delete(key)
		}
		return true
	})
}

func loadDesktopTurnResult(runID string) (desktopTurnResult, bool) {
	value, ok := desktopTurnResults.Load(runID)
	if !ok {
		return desktopTurnResult{}, false
	}
	item, _ := value.(desktopTurnResult)
	return item, item.Ready
}

// desktopRunAPI is a run plus the desktop reply captured after admission.
type desktopRunAPI struct {
	agentservice.Run
	DesktopReady    bool                  `json:"desktop_ready"`
	DesktopHandoff  bool                  `json:"desktop_handoff"`
	AttentionReason string                `json:"attention_reason,omitempty"`
	Message         *agentservice.Message `json:"message,omitempty"`
	// Activity is the step this run is in right now. It is empty once the
	// run has a finished reply. The words are a fixed phrase, never the
	// text the step typed.
	Activity string `json:"activity,omitempty"`
	// ScreenQueue is set while this run waits for another task of the same
	// bot to release the screen.
	ScreenQueue string `json:"screen_queue,omitempty"`
	// StatusLine is the live status for the bot's status area: run id,
	// step, latest progress, background job and its next check.
	StatusLine    string `json:"status_line,omitempty"`
	ProgressWhere string `json:"progress_where,omitempty"`
	ProgressNext  string `json:"progress_next,omitempty"`
	ProgressOpen  string `json:"progress_open,omitempty"`
	ProgressPath  string `json:"progress_path,omitempty"`
}

func desktopRunAPIBody(dataRoot string, run *agentservice.Run, stored *agentservice.Message) any {
	sanitized := sanitizeRunPtrForAPI(dataRoot, run)
	if run == nil {
		return sanitized
	}
	activity := loadDesktopActivity(run.ID)
	queue := desktopScreenQueue(run.ID)
	result, ok := loadDesktopTurnResult(run.ID)
	where, next, open, path, hasProgress := desktopProgressFields(run, result.Message)
	status := ""
	if !ok && run.Status == agentservice.RunStatusRunning {
		status = desktopRunStatusLine(run.ID)
		if queue != "" {
			if status != "" {
				status += " · " + queue
			} else {
				status = queue
			}
		}
		if note := restartNoticeOf(run); note != "" {
			if status != "" {
				status += " · " + note
			} else {
				status = note
			}
		}
	}
	if !ok && stored != nil && desktopTerminalRun(run.Status) {
		body := desktopRunAPI{Run: sanitized, Activity: activity, ScreenQueue: queue, StatusLine: status, DesktopReady: true, Message: stored}
		if hasProgress {
			body.ProgressWhere = where
			body.ProgressNext = next
			body.ProgressOpen = open
			body.ProgressPath = path
		}
		return body
	}
	if !ok && activity == "" && queue == "" && !hasProgress && status == "" {
		return sanitized
	}
	body := desktopRunAPI{Run: sanitized, Activity: activity, ScreenQueue: queue, StatusLine: status}
	if hasProgress {
		body.ProgressWhere = where
		body.ProgressNext = next
		body.ProgressOpen = open
		body.ProgressPath = path
	}
	if !ok {
		return body
	}
	body.DesktopReady = true
	body.DesktopHandoff = result.Handoff
	body.AttentionReason = result.Attention
	body.Message = result.Message
	return body
}

func restartNoticeOf(run *agentservice.Run) string {
	if run == nil || run.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(run.Metadata["restart_notice"])
}

func desktopTerminalRun(status agentservice.RunStatus) bool {
	switch status {
	case agentservice.RunStatusSucceeded, agentservice.RunStatusFailed, agentservice.RunStatusCancelled:
		return true
	default:
		return false
	}
}

// desktopStoredTerminalMessage is the assistant reply already stored for a
// finished run. The in-memory turn result does not survive a process
// restart, and Hub delivers a sentence only when the run read carries one.
func desktopStoredTerminalMessage(ctx context.Context, svc *agentservice.Service, p agentservice.Principal, run *agentservice.Run) *agentservice.Message {
	if svc == nil || run == nil || strings.TrimSpace(run.AssistantMessageID) == "" {
		return nil
	}
	if _, ok := loadDesktopTurnResult(run.ID); ok {
		return nil
	}
	if !desktopTerminalRun(run.Status) {
		return nil
	}
	msg, err := svc.GetMessage(ctx, p, run.InstanceID, run.SessionID, run.AssistantMessageID)
	if err != nil {
		return nil
	}
	return msg
}

func desktopProgressFields(run *agentservice.Run, msg *agentservice.Message) (where, next, open, path string, ok bool) {
	if msg != nil && msg.Metadata != nil {
		where = strings.TrimSpace(msg.Metadata["progress_where"])
		next = strings.TrimSpace(msg.Metadata["progress_next"])
		open = strings.TrimSpace(msg.Metadata["progress_open"])
		path = strings.TrimSpace(msg.Metadata["progress_path"])
		if where != "" || next != "" || open != "" {
			return where, next, open, path, true
		}
	}
	ids := make([]string, 0, 2)
	if run != nil {
		if id := strings.TrimSpace(run.SessionID); id != "" {
			ids = append(ids, id)
		}
		if id := strings.TrimSpace(run.ID); id != "" && id != strings.TrimSpace(run.SessionID) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		copy, found := desktopbot.RecallProgress(id)
		if !found {
			continue
		}
		return copy.Sections.WhereWeAre, copy.Sections.NextStep, copy.Sections.OpenProblems, copy.Path, true
	}
	return "", "", "", "", false
}
