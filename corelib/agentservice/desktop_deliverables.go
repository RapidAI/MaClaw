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

var desktopDeliverablesExtract = agent.ExtractDesktopDeliverables

// MACLAW_DESKTOP_DELIVERABLES=0 turns the checklist off.
func desktopDeliverablesEnabled() bool {
	return strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_DELIVERABLES")) != "0"
}

func (c *coreAgentCallbacks) desktopDeliverablesApply() bool {
	if c == nil || !desktopDeliverablesEnabled() || !c.botUsesContainer() {
		return false
	}
	if strings.TrimSpace(c.messageMetadata["bot_phase"]) != "execute" || strings.TrimSpace(c.messageMetadata["bot_wake"]) == "1" {
		return false
	}
	return strings.TrimSpace(c.userText) != ""
}

// startDesktopDeliverables reads the explicit deliverables of this turn.
func (c *coreAgentCallbacks) startDesktopDeliverables() {
	if !c.desktopDeliverablesApply() || c.deliverables != nil {
		return
	}
	st := &desktopDeliverablesState{done: make(chan struct{})}
	c.deliverables = st
	cfg, client, text := c.llmCfg, c.httpClient, c.userText
	go func() {
		defer close(st.done)
		items, err := desktopDeliverablesExtract(context.Background(), cfg, client, text)
		if err != nil {
			log.Printf("[desktop-deliverables] extract_err=%v", err)
			return
		}
		st.mu.Lock()
		st.items = items
		st.mu.Unlock()
		log.Printf("[desktop-deliverables] items=%d %q", len(items), strings.Join(items, " | "))
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
