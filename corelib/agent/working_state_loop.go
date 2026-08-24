package agent

import "strings"

type workingStateBatch struct {
	lastFail     *RoundSignal
	lastOK       *RoundSignal
	trustedFocus bool
}

func loopPromptProfile(cb LoopCallbacks) PromptProfile {
	if pp, ok := cb.(PromptProfileProvider); ok {
		return pp.CurrentPromptProfile()
	}
	return PromptProfileFull
}

func loopProjectedGoal(cb LoopCallbacks) string {
	if src, ok := cb.(WorkingStateGoalSource); ok {
		return strings.TrimSpace(src.ActiveWorkingStateGoal())
	}
	return ""
}

func loadInitialWorkingState(cb LoopCallbacks) *WorkingState {
	if WorkingStateDisabled() {
		return nil
	}
	if holder, ok := cb.(WorkingStateHolder); ok {
		if loaded := CloneWorkingState(holder.LoadWorkingState()); loaded != nil {
			return loaded
		}
	}
	if g := loopProjectedGoal(cb); g != "" {
		return NewWorkingState(g)
	}
	return nil
}

func finishWorkingState(cb LoopCallbacks, r *LoopResult, state *WorkingState) {
	if r.WorkingState == nil {
		r.WorkingState = state
	}
	sanitizeLoopResultVisible(r)
	holder, ok := cb.(WorkingStateHolder)
	if !ok {
		return
	}
	if r.AskUser != nil || r.RecordAudio != nil {
		holder.SaveWorkingState(CloneWorkingState(state))
		return
	}
	holder.SaveWorkingState(nil)
}

func sanitizeLoopResultVisible(r *LoopResult) {
	if r == nil {
		return
	}
	r.Text = StripWorkingStateFromVisible(r.Text)
	for i := range r.HistoryDelta {
		// Tool payloads can be file bodies that mention the heading.
		// Cutting them at the last line-start marker drops the rest of
		// the saved result. Only strip user-visible roles.
		if strings.EqualFold(strings.TrimSpace(r.HistoryDelta[i].Role), "tool") {
			continue
		}
		r.HistoryDelta[i].Content = stripWorkingStateContent(r.HistoryDelta[i].Content)
		r.HistoryDelta[i].ReasoningContent = StripWorkingStateFromVisible(r.HistoryDelta[i].ReasoningContent)
	}
}

func spliceWorkingStateAtHead(cb LoopCallbacks, conversation []interface{}, state *WorkingState, userText string, executedTools int) (*WorkingState, []interface{}) {
	if WorkingStateDisabled() {
		return nil, ApplyWorkingStateSection(conversation, nil, false)
	}
	projected := ""
	if state == nil {
		projected = loopProjectedGoal(cb)
	}
	state = EnsureWorkingState(state, userText, executedTools, projected)
	attach := ShouldAttachWorkingState(loopPromptProfile(cb), false, state)
	return state, ApplyWorkingStateSection(conversation, state, attach)
}

func (b *workingStateBatch) note(state *WorkingState, name, argsJSON string, outcome ToolExecutionOutcome) {
	if state == nil {
		return
	}
	focusLabel := ""
	if item, ok := ExtractFocus(name, argsJSON, outcome); ok {
		_ = AdmitLiveEvictOldest(state, item)
		focusLabel = item.Label
	}
	failed := outcome != ToolExecutionOutcomeOK
	// generate_pdf is host-owned and one-shot. Intake / already-used
	// denials must not open diagnose; the live turn's finish-nudge after
	// those opens made the model emit unparseable tool XML.
	if failed && strings.TrimSpace(name) == "generate_pdf" {
		return
	}
	// apply() prefers lastFail over a later success. A success in the same
	// batch must not reset LastSig/SigCount or the next identical failure
	// restarts at 1 and never reaches seek_user.
	count := state.SigCount
	if failed || b.lastFail == nil {
		count = AccountToolSignature(state, name, argsJSON, failed)
	}
	sig := RoundSignal{
		Kind:         roundKindFromOutcome(outcome),
		ToolName:     name,
		SameSigCount: count,
		Prev:         state.LastAction,
		OpenCount:    UnclosedOpenCount(state),
		FocusLabel:   focusLabel,
	}
	cp := sig
	if failed {
		b.lastFail = &cp
		return
	}
	// Settle a file success now — later failed file tools can evict it
	// from Live before apply(). Trust also closes that tool's opens.
	// Non-focus success (bash) must still close, or a later file ok
	// would skip the bash open.
	if focusLabel != "" {
		_ = ApplyControlAction(state, ActionTrust, cp)
		b.trustedFocus = true
	} else {
		CloseOpenOnTrust(state, name, "")
	}
	b.lastOK = &cp
}

func (b *workingStateBatch) apply(state *WorkingState) string {
	if state == nil {
		return ""
	}
	sig := b.lastOK
	if b.lastFail != nil {
		sig = b.lastFail
	} else if sig != nil && (strings.TrimSpace(sig.FocusLabel) != "" || b.trustedFocus) {
		// File success already trusted in note(). A later bash ok must
		// not rewrite Next off that file.
		b.lastFail = nil
		b.lastOK = nil
		b.trustedFocus = false
		return ""
	}
	if sig == nil {
		return ""
	}
	// Consume so a second apply() cannot AddOpen twice (ask_user + later
	// exit, or a future early-return that also applies).
	b.lastFail = nil
	b.lastOK = nil
	b.trustedFocus = false
	cp := *sig
	// Later successes in this batch may have closed opens or set LastAction
	// to trust after lastFail was noted. SelectAction must see live
	// workspace fields, or empiric+same-sig / open-cap still seek.
	cp.OpenCount = UnclosedOpenCount(state)
	cp.Prev = state.LastAction
	action, err := SelectAction(cp)
	if err != nil {
		return ""
	}
	_ = ApplyControlAction(state, action, cp)
	if action == ActionSeekUser && cp.SameSigCount >= 3 {
		return SameSignatureForbidMessage(cp.ToolName)
	}
	return ""
}

func applyWorkingStateEmpty(state *WorkingState, userText, lastToolName string, emptyCount, executedTools int, projectedGoal string) (*WorkingState, string) {
	if WorkingStateDisabled() {
		return state, ""
	}
	state = EnsureWorkingState(state, userText, executedTools, projectedGoal)
	if state == nil {
		return nil, ""
	}
	sig := RoundSignal{
		Kind:       RoundLLMEmpty,
		ToolName:   lastToolName,
		EmptyCount: emptyCount,
		Prev:       state.LastAction,
		OpenCount:  UnclosedOpenCount(state),
	}
	action, err := SelectAction(sig)
	if err == nil {
		_ = ApplyControlAction(state, action, sig)
	}
	return state, state.Next
}

func maybeMarkSeekUser(state *WorkingState, userText string, executedTools int, projectedGoal string) *WorkingState {
	if WorkingStateDisabled() {
		return state
	}
	state = EnsureWorkingState(state, userText, executedTools, projectedGoal)
	if state != nil {
		state.LastAction = ActionSeekUser
		state.Next = nextSeekUser()
		state.touch()
	}
	return state
}

// applyThenSeekUser records the batch (so a prior fail still opens) then marks seek.
func applyThenSeekUser(state *WorkingState, batch *workingStateBatch, userText string, executedTools int, projectedGoal string) *WorkingState {
	if batch != nil {
		_ = batch.apply(state)
	}
	return maybeMarkSeekUser(state, userText, executedTools, projectedGoal)
}
