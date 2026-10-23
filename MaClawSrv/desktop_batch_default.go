package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Generic batch default for computer/browser work on every site:
// observe once (probe), then ONE action=batch whose plan is the rest of the
// flow, then one read-back. Zhihu is only a validation scenario; the guard
// itself is site-agnostic. Nested plan steps set desktopInsideBatchKey and
// skip this check. Stock Chromium only (CDP / page scripts / OS input).

var desktopBatchMutating = map[string]bool{
	"click": true, "type": true, "press": true, "rich_fill": true, "computer": true,
	// script is not counted: one page script is already a batched unit
	// (typically a read/extract), and refusing it pushed models into
	// bash/file detours (RUN30).
}

func desktopInsideBatch(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(desktopInsideBatchKey{}).(bool)
	return v
}

func desktopMaxSinglesBeforeBatch() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_BATCH_MAX_SINGLES"))); err == nil && v >= 0 {
		return v
	}
	return 1
}

func desktopMarkBatchStarted(scope agentruntime.Scope) {
	st := desktopRunFor(scope)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.progress == nil {
		st.progress = &desktopProgress{lastBody: -1}
	}
	st.progress.batchStarted = true
	st.progress.singleMutating = 0
}

// desktopBatchDefaultCheck refuses another single click/type/press/script/
// rich_fill once the model has already taken its free single mutation (or
// has observed the page and is past the observe phase). Fallback: after a
// batch has been attempted, singles are allowed again so a failed plan step
// can be fixed alone.
func desktopBatchDefaultCheck(ctx context.Context, scope agentruntime.Scope, action string) error {
	if desktopInsideBatch(ctx) || !desktopBatchMutating[action] {
		return nil
	}
	max := desktopMaxSinglesBeforeBatch()
	if max < 0 {
		return nil
	}
	st := desktopRunFor(scope)
	if st == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.progress == nil {
		st.progress = &desktopProgress{lastBody: -1}
	}
	p := st.progress
	if p.batchStarted {
		return nil
	}
	if p.singleMutating >= max {
		return fmt.Errorf("batch default: already used %d single browser step(s) without a plan. Observe once, then ONE action=batch with plan=[...] covering the rest of the flow (script/rich_fill/click/type/press/wait), then one read-back. Fix only a failed plan step alone.", p.singleMutating)
	}
	return nil
}

func desktopRecordBatchDefault(scope agentruntime.Scope, action string, err error) {
	st := desktopRunFor(scope)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.progress == nil {
		st.progress = &desktopProgress{lastBody: -1}
	}
	p := st.progress
	if action == "probe" && err == nil {
		p.observed = true
	}
	if action == "navigate" && err == nil {
		// singles are budgeted per page: a new page starts a new flow.
		p.singleMutating = 0
	}
	if action == "batch" {
		p.batchStarted = true
		p.singleMutating = 0
		return
	}
	if desktopBatchMutating[action] && err == nil && !p.batchStarted {
		p.singleMutating++
	}
}
