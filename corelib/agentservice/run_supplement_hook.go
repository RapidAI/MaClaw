package agentservice

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/botlog"
)

// supplementMarker delimits the rotating summary message so the hook can find
// and replace it instead of appending another copy.
//
// It exists because the loop's conversation slice is built once and only ever
// grows: nothing downstream prunes system messages, and a run may iterate up to
// hundreds of times. Appending one message per supplement would mean the first
// supplement is re-sent to the model on every remaining iteration, costing
// tokens and diluting attention for the rest of the run.
const supplementMarker = "\x1fMACLAW_SUPPLEMENT_SUMMARY\x1f"

// supplementHook is one per executed turn. It must never be shared between
// runs: its merged set describes one run's supplements only, and sharing it
// across concurrent runs would silently move one person's input into a
// different run's conversation.
type supplementHook struct {
	mu sync.Mutex
	// take drains this run's queue exactly once per item.
	take func() []*RunSupplement
	// rounds counts TransformConversation calls, which is the loop iteration
	// ordinal. It is reported with every applied supplement: without it a
	// queued item that was never consumed is indistinguishable from one the
	// loop really used.
	rounds int
	botID  string
	runID  string
	merged []string
	dirty  bool
}

// newSupplementHook builds the loop hook for one turn. botID is only used to
// name the log file; an empty one keeps the applied stage silent rather than
// writing under a guessed name.
//
// An empty runID disables delivery: without a run identity there is no way to
// know which run an item belongs to, and guessing from the session would hand
// one run's supplement to another. Failing closed here is what keeps the
// feature from becoming a silent data-leak path when wiring regresses.
func newSupplementHook(svc *Service, botID, runID string) agent.LoopHooks {
	runID = strings.TrimSpace(runID)
	if svc == nil || runID == "" {
		return agent.DefaultLoopHooks{}
	}
	return &supplementHook{
		botID: botID,
		runID: runID,
		take:  func() []*RunSupplement { return svc.takePendingRunSupplements(runID) },
	}
}

// TransformConversation folds every newly drained supplement into the single
// rotating summary message. Returning nil keeps the conversation untouched, per
// the LoopHooks contract.
func (h *supplementHook) TransformConversation(conversation []interface{}) []interface{} {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rounds++
	taken := h.take()
	if len(taken) == 0 && !h.dirty {
		return nil
	}
	for _, item := range taken {
		if content := strings.TrimSpace(item.Content); content != "" {
			h.merged = append(h.merged, content)
			h.dirty = true
		}
	}
	if len(h.merged) == 0 {
		return nil
	}
	h.dirty = false
	out := append([]interface{}(nil), conversation...)
	body := formatSupplementSummary(h.merged)
	if i := indexSupplementSummary(out); i >= 0 {
		out[i] = map[string]string{"role": "system", "content": body}
	} else {
		out = append(out, map[string]string{"role": "system", "content": body})
	}
	// The one hard proof the supplement reached the model. Counts and ids only:
	// the text itself must never be written to a log.
	for _, item := range taken {
		botlog.Write(h.botID, "srv.supplement_applied", nil,
			"run", h.runID,
			"supplement", item.ID,
			"iteration", strconv.Itoa(h.rounds),
			"total", strconv.Itoa(len(h.merged)),
		)
	}
	return out
}

// OnToolExecuted is a no-op. It is implemented rather than embedded because a
// hook type must satisfy the whole interface; behaviour stays the documented
// default from agent.DefaultLoopHooks.
func (h *supplementHook) OnToolExecuted(string, string, string, bool) {}

// OnEmptyResponse keeps the loop's default: return false, which exits. This is
// the deliberate default from agent.DefaultLoopHooks and must not change, or an
// empty model reply would start spinning instead of ending the turn.
func (h *supplementHook) OnEmptyResponse(int) bool { return false }

// indexSupplementSummary finds the rotating message, if already present.
func indexSupplementSummary(conversation []interface{}) int {
	for i, msg := range conversation {
		if content, ok := summaryText(msg); ok && strings.Contains(content, supplementMarker) {
			return i
		}
	}
	return -1
}

func summaryText(msg interface{}) (string, bool) {
	switch v := msg.(type) {
	case map[string]string:
		if v["role"] != "system" {
			return "", false
		}
		return v["content"], true
	case map[string]interface{}:
		if role, _ := v["role"].(string); role != "system" {
			return "", false
		}
		content, _ := v["content"].(string)
		return content, true
	default:
		return "", false
	}
}

// formatSupplementSummary renders every supplement as one system message.
//
// The framing states plainly that these lines came from the person running the
// task, not from a tool result or from the model itself, so they are read as
// instructions to follow rather than as content to react to.
func formatSupplementSummary(items []string) string {
	var b strings.Builder
	b.WriteString(supplementMarker)
	b.WriteString("\n[supplementary instructions from the person running this task]")
	for i, item := range items {
		fmt.Fprintf(&b, "\n%d. %s", i+1, item)
	}
	b.WriteString("\nThese are additions to the task already in progress, not a new request.")
	b.WriteString(" Apply them to the steps still ahead; completed work stays done unless one of these lines changes it.")
	return b.String()
}
