package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

const (
	computerUseAuditSkipped = "skipped"
	computerUseAuditPassed  = "passed"
	computerUseAuditFailed  = "failed"
)

// computerUseTaskState is the P0 slim CU contract, keyed by the same owner as
// ExecuteToolCall / cuSession (SessionKey, else UserID).
type computerUseTaskState struct {
	Owner      string
	RequestID  string
	Goal       string
	Acceptance []string
	LastAudit  string
	FailedDone int
}

func normalizeComputerUseAcceptance(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" || isGenericComputerUseChrome(item) {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func isGenericComputerUseChrome(s string) bool {
	key := strings.ToLower(strings.TrimSpace(s))
	switch key {
	case "保存", "确定", "取消", "打开", "关闭",
		"ok", "save", "cancel", "open", "close":
		return true
	default:
		return false
	}
}

func beginComputerUseTask(owner, requestID, goal string, acceptance []string) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	st := &computerUseTaskState{
		Owner:      owner,
		RequestID:  strings.TrimSpace(requestID),
		Goal:       strings.TrimSpace(goal),
		Acceptance: normalizeComputerUseAcceptance(acceptance),
	}
	globalComputerUse.mu.Lock()
	if globalComputerUse.taskStates == nil {
		globalComputerUse.taskStates = make(map[string]*computerUseTaskState)
	}
	globalComputerUse.taskStates[owner] = st
	globalComputerUse.mu.Unlock()
}

func computerUseTaskStateFor(owner string) *computerUseTaskState {
	return snapshotComputerUseTaskState(owner)
}

func snapshotComputerUseTaskState(owner string) *computerUseTaskState {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	st := globalComputerUse.taskStates[owner]
	if st == nil {
		return nil
	}
	cp := *st
	if len(st.Acceptance) > 0 {
		cp.Acceptance = append([]string(nil), st.Acceptance...)
	}
	return &cp
}

func clearAllComputerUseTaskStatesLocked() {
	globalComputerUse.taskStates = nil
}

func maybeBeginComputerUseTask(loopCtx *LoopContext, fallbackUserID, goal string) {
	if loopCtx == nil || !loopCtx.ComputerUseFresh || loopCtx.ComputerUseBegun {
		return
	}
	owner := computerUseOwnerFromLoop(loopCtx, fallbackUserID)
	req := strings.TrimSpace(loopCtx.Runtime.RequestID)
	if req != "" {
		if existing := snapshotComputerUseTaskState(owner); existing != nil && existing.RequestID == req {
			setComputerUseOwner(owner)
			loopCtx.ComputerUseBegun = true
			return
		}
	}
	setComputerUseOwner(owner)
	beginComputerUseTask(owner, req, goal, nil)
	loopCtx.ComputerUseBegun = true
}

func recordComputerUseGate(h *IMMessageHandler, ctx *LoopContext, routingText string) (active, fresh bool) {
	if h == nil {
		return false, false
	}
	if ctx != nil && ctx.ComputerUseGateSettled {
		return ctx.ComputerUseActive, ctx.ComputerUseFresh
	}
	active, fresh = h.gateComputerUse(routingText)
	if ctx != nil {
		ctx.ComputerUseGateSettled = true
		ctx.ComputerUseActive = active
		ctx.ComputerUseFresh = fresh
	}
	return active, fresh
}

func syncComputerUseTurn(h *IMMessageHandler, ctx *LoopContext, fallbackUserID, goal string) {
	if ctx == nil {
		return
	}
	owner := computerUseOwnerFromLoop(ctx, fallbackUserID)
	ctx.ComputerUseOwner = owner
	text := ctx.ComputerUseRoutingText
	if strings.TrimSpace(text) == "" {
		text = goal
	}
	active, _ := recordComputerUseGate(h, ctx, text)
	if !active {
		return
	}
	setComputerUseOwner(owner)
	maybeBeginComputerUseTask(ctx, fallbackUserID, goal)
}

func computerUseContractPlaybookExtra(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		globalComputerUse.mu.Lock()
		owner = strings.TrimSpace(globalComputerUse.activeOwner)
		globalComputerUse.mu.Unlock()
		if owner == "" {
			owner = computerUseDefaultOwner
		}
	}
	st := snapshotComputerUseTaskState(owner)
	if st == nil || len(st.Acceptance) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Host acceptance (do not call computer_done until every line is visible in the latest computer_observe OCR/elements/text):\n")
	for _, item := range st.Acceptance {
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}
	return b.String()
}

func computerUseAuditCorpus(obs *computeruse.ObserveResult) string {
	if obs == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(obs.OCRExcerpt)
	b.WriteByte('\n')
	b.WriteString(obs.TextForModel)
	b.WriteByte('\n')
	for _, el := range obs.Elements {
		b.WriteString(el.Name)
		b.WriteByte('\n')
		b.WriteString(el.Value)
		b.WriteByte('\n')
	}
	return b.String()
}

const (
	computerUseProbeOCRBudget     = 1200
	computerUseProbeElementBudget = 1600
	computerUseProbeHugeSample    = 240
)

func clipComputerUseRunesHeadTail(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max < 8 {
		return string(runes[:max])
	}
	keep := max - 3
	head := keep * 2 / 3
	if head < 1 {
		head = 1
	}
	tail := keep - head
	if tail < 1 {
		tail = 1
		head = keep - tail
	}
	return string(runes[:head]) + "..." + string(runes[len(runes)-tail:])
}

func computerUseProbeDigest(obs *computeruse.ObserveResult) string {
	if obs == nil {
		return ""
	}
	var b strings.Builder
	if ocr := clipComputerUseRunesHeadTail(obs.OCRExcerpt, computerUseProbeOCRBudget); ocr != "" {
		b.WriteString(ocr)
		b.WriteByte('\n')
	}
	elBudget := computerUseProbeElementBudget
	sampledHuge := false
	for _, el := range obs.Elements {
		if elBudget <= 1 {
			break
		}
		for _, part := range []string{el.Name, el.Value} {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n := utf8.RuneCountInString(part) + 1
			if n > elBudget {
				if sampledHuge || elBudget <= 1 {
					continue
				}
				clipMax := computerUseProbeHugeSample
				if clipMax > elBudget {
					clipMax = elBudget
				}
				clipped := clipComputerUseRunesHeadTail(part, clipMax)
				if clipped == "" {
					continue
				}
				b.WriteString(clipped)
				b.WriteByte('\n')
				elBudget -= utf8.RuneCountInString(clipped) + 1
				sampledHuge = true
				continue
			}
			elBudget -= n
			b.WriteString(part)
			b.WriteByte('\n')
		}
	}
	b.WriteString(strings.TrimSpace(obs.TextForModel))
	return strings.TrimSpace(b.String())
}

func applyComputerUseAudit(sess *computeruse.Session, state *computerUseTaskState) (passed bool, reason string) {
	if state == nil || len(state.Acceptance) == 0 {
		return true, computerUseAuditSkipped
	}
	if sess == nil {
		return false, "observe required before computer_done"
	}
	obs := sess.LastValidObserve()
	if obs == nil {
		return false, "observe required before computer_done"
	}
	corpus := computerUseAuditCorpus(obs)
	if strings.TrimSpace(corpus) == "" {
		return false, "empty observe corpus"
	}
	for _, item := range state.Acceptance {
		if !computerUseCorpusHasAcceptance(corpus, item) {
			return false, "observe text does not satisfy acceptance: " + item
		}
	}
	return true, ""
}

func computerUseCorpusHasAcceptance(corpus, item string) bool {
	foldedCorpus := strings.ToLower(corpus)
	foldedItem := strings.ToLower(strings.TrimSpace(item))
	if foldedItem == "" {
		return false
	}
	from := 0
	for {
		idx := strings.Index(foldedCorpus[from:], foldedItem)
		if idx < 0 {
			return false
		}
		pos := from + idx
		if !computerUseNegatedMatch(foldedCorpus, pos) {
			return true
		}
		from = pos + 1
	}
}

func computerUseNegatedMatch(s string, pos int) bool {
	if pos <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:pos])
	switch r {
	case '未', '不', '没', '無', '无':
		return true
	}
	attached := s[:pos]
	// Two-character Chinese negations where the last rune is not 未/不/没/无.
	for _, neg := range []string{"没有", "未能", "无法", "不能", "不要"} {
		if strings.HasSuffix(attached, neg) {
			return true
		}
	}
	// Morphological prefixes attach to the same token (incomplete, unsaved).
	// Do not skip spaces: "main complete" / "login complete" are not in-negations.
	for _, neg := range []string{"non", "un", "in", "im", "ir"} {
		if !strings.HasSuffix(attached, neg) {
			continue
		}
		start := pos - len(neg)
		if start == 0 {
			return true
		}
		prev, _ := utf8.DecodeLastRuneInString(attached[:start])
		if prev == utf8.RuneError || !unicode.IsLetter(prev) {
			return true
		}
	}
	i := pos
	for i > 0 {
		rr, size := utf8.DecodeLastRuneInString(s[:i])
		if rr == utf8.RuneError || size <= 0 {
			break
		}
		if !unicode.IsSpace(rr) {
			break
		}
		i -= size
	}
	if i <= 0 {
		return false
	}
	word := s[:i]
	for _, neg := range []string{"never", "not", "no"} {
		if !strings.HasSuffix(word, neg) {
			continue
		}
		start := i - len(neg)
		if start == 0 {
			return true
		}
		prev, _ := utf8.DecodeLastRuneInString(word[:start])
		if prev == utf8.RuneError || !unicode.IsLetter(prev) {
			return true
		}
	}
	return false
}

func updateComputerUseTaskAudit(owner, audit string, failed bool) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = computerUseDefaultOwner
	}
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	st := globalComputerUse.taskStates[owner]
	if st == nil {
		return
	}
	st.LastAudit = audit
	if failed {
		st.FailedDone++
		return
	}
	// Passed or skipped: drop bullets so a later prompt rebuild does not keep
	// telling the model not to call computer_done.
	st.Acceptance = nil
}
