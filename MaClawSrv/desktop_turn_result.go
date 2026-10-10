package main

import (
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
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

var desktopTurnResults sync.Map

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
	desktopTurnResults.Store(run.ID, desktopTurnResult{
		Ready:     true,
		Handoff:   handoff,
		Attention: attention,
		Message:   copied,
		StoredAt:  time.Now(),
	})
	cutoff := time.Now().Add(-6 * time.Hour)
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
}

func desktopRunAPIBody(dataRoot string, run *agentservice.Run) any {
	sanitized := sanitizeRunPtrForAPI(dataRoot, run)
	if run == nil {
		return sanitized
	}
	result, ok := loadDesktopTurnResult(run.ID)
	if !ok {
		return sanitized
	}
	return desktopRunAPI{
		Run:             sanitized,
		DesktopReady:    true,
		DesktopHandoff:  result.Handoff,
		AttentionReason: result.Attention,
		Message:         result.Message,
	}
}
