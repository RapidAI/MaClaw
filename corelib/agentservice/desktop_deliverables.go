package agentservice

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// desktopDeliverablesState holds one turn's checklist of what the person
// explicitly asked to receive. A model reads it from the request beside the
// first steps (no keyword rules); the worker sees it on the first tool
// result after it is ready, and the outcome check matches each item to a
// delivery before the turn may finish. Bot 2 (papers 11-11 20:26-20:53)
// had its requested screenshot at ~step 22 and delivered it at step 53.
type desktopDeliverablesState struct {
	done  chan struct{}
	mu    sync.Mutex
	items []string
	shown bool
}

// desktopDeliverablesWait bounds how long the outcome check waits for a
// checklist still being read.
var desktopDeliverablesWait = 20 * time.Second

// RememberDesktopOutputContract records the parsed checklist. failed means
// the model response could not be read; that is not an empty checklist. cause
// is the concrete extraction error so the receipt stays unverified with a
// specific reason instead of a bare "failed". MaClawSrv installs it.
var RememberDesktopOutputContract func(runID string, items []agent.DesktopDeliverable, failed bool, cause string)

// RecordDesktopDeliverableExtraction stores one extraction result. A parse
// or model error is recorded as failed so the receipt stays unverified; its
// message is carried through so the unverified outcome keeps a concrete cause.
func RecordDesktopDeliverableExtraction(runID string, items []agent.DesktopDeliverable, extractErr error) {
	if RememberDesktopOutputContract == nil {
		return
	}
	cause := ""
	if extractErr != nil {
		cause = strings.TrimSpace(extractErr.Error())
	}
	RememberDesktopOutputContract(runID, items, extractErr != nil, cause)
}

// MACLAW_DESKTOP_DELIVERABLES=0 turns the checklist off.
func desktopDeliverablesEnabled() bool {
	return strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_DELIVERABLES")) != "0"
}

func (c *coreAgentCallbacks) desktopDeliverablesApply() bool {
	if c == nil || !desktopDeliverablesEnabled() {
		return false
	}
	// A plan describes the work. It does not deliver the outputs. Execute
	// does, with or without a computer, including a mailbox wake.
	if strings.TrimSpace(c.messageMetadata["bot_phase"]) != "execute" {
		return false
	}
	return strings.TrimSpace(c.userText) != ""
}

// desktopDeliverableRequestText is the request the checklist is read from.
// A timer firing's order still says "every five minutes". That interval is
// the clock's job. Read as a standard, it kept one weather turn going after
// the PDF was already delivered, because one file cannot prove the cadence.
// desktopAskedText is the person's order. A resumed turn's visible text is
// the restart sentence, so the checklist was empty and the outcome check
// accepted another copy of the PDF (Bot 4, 15:24).
func desktopAskedText(c *coreAgentCallbacks, userText string) string {
	text := strings.TrimSpace(userText)
	if c == nil || c.messageMetadata == nil {
		return text
	}
	resumed := text == restartNoticeContinue || strings.TrimSpace(c.messageMetadata["restart_resume"]) == "1"
	if !resumed {
		return text
	}
	if ask := strings.TrimSpace(c.messageMetadata["task_request"]); ask != "" {
		return ask
	}
	if ask := strings.TrimSpace(c.messageMetadata["bot_task"]); ask != "" {
		return ask
	}
	return text
}

func desktopOutcomeAsk(c *coreAgentCallbacks, userText string) string {
	text := desktopAskedText(c, userText)
	if c == nil || c.messageMetadata == nil || strings.TrimSpace(c.messageMetadata["bot_origin"]) != "schedule" {
		return text
	}
	return "这是定时器已经到点的一轮。周期由定时器负责，不要把「每几分钟」或重复次数写成这一轮的标准。这一轮只交付这一次要求交给这个人的东西。\n" + text
}

func desktopDeliverableRequestText(c *coreAgentCallbacks) string {
	if c == nil {
		return ""
	}
	return desktopOutcomeAsk(c, c.userText)
}

// BeginDesktopDeliverableExtraction is set by MaClawSrv. The receipt waits
// for that extraction instead of treating a lone file as completion.
var BeginDesktopDeliverableExtraction func(runID string)

// startDesktopDeliverables reads the explicit deliverables of this turn.
func (c *coreAgentCallbacks) startDesktopDeliverables() {
	if !c.desktopDeliverablesApply() || c.deliverables != nil {
		return
	}
	st := &desktopDeliverablesState{done: make(chan struct{})}
	c.deliverables = st
	if BeginDesktopDeliverableExtraction != nil {
		BeginDesktopDeliverableExtraction(c.runID)
	}
	cfg, client, text := c.llmCfg, c.httpClient, desktopDeliverableRequestText(c)
	go func() {
		defer close(st.done)
		// One bounded correction, not a second guess on the same prompt: when
		// the first read left a malformed answer, repair re-asks grounded on
		// that output; a transport blip re-issues the request once. A still
		// failing correction stays failed — it repairs the read, never
		// invents a checklist.
		items, raw, err := agent.ExtractDesktopDeliverableContractRaw(context.Background(), cfg, client, text)
		if err != nil {
			items, err = agent.RepairDesktopDeliverableContract(context.Background(), cfg, client, text, raw)
		}
		RecordDesktopDeliverableExtraction(c.runID, items, err)
		if err != nil {
			log.Printf("[desktop-deliverables] extract_err=%v", err)
			return
		}
		lines := make([]string, 0, len(items))
		for _, item := range items {
			line := strings.TrimSpace(item.Text)
			if item.ID != "" {
				line = item.ID + " " + item.Kind + " " + line
			}
			if strings.TrimSpace(line) != "" {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
		st.mu.Lock()
		st.items = lines
		st.mu.Unlock()
		log.Printf("[desktop-deliverables] items=%d", len(items))
	}()
}

// desktopDeliverableItems waits a bounded time for the checklist.
func (c *coreAgentCallbacks) desktopDeliverableItems() []string {
	if c == nil || c.deliverables == nil {
		return nil
	}
	st := c.deliverables
	select {
	case <-st.done:
	case <-time.After(desktopDeliverablesWait):
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.items...)
}

// withDeliverablesNote puts the checklist into the worker's context once, on
// the first tool result after it is ready.
func (c *coreAgentCallbacks) withDeliverablesNote(name string, result agent.ToolExecutionResult) agent.ToolExecutionResult {
	if c == nil || c.deliverables == nil {
		return result
	}
	st := c.deliverables
	select {
	case <-st.done:
	default:
		return result
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.shown || len(st.items) == 0 {
		return result
	}
	st.shown = true
	result.Result = strings.TrimRight(result.Result, "\n") + "\n\n" + agent.DesktopDeliverablesNote(st.items)
	return result
}
