package guiapp

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
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
	// MessageBody is the text a desktop IM send asked to deliver.
	// MessageTyped is set once a successful computer_type has covered it.
	// MessageSubmitted is set by the next Enter. Further desktop actions stop.
	MessageBody      string
	MessageTyped     bool
	MessageSubmitted bool
	typedCompact     string
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
	goal = strings.TrimSpace(goal)
	clearComputerUseExplicitFocus()
	st := &computerUseTaskState{
		Owner:       owner,
		RequestID:   strings.TrimSpace(requestID),
		Goal:        goal,
		Acceptance:  normalizeComputerUseAcceptance(acceptance),
		MessageBody: desktopMessagePayload(goal),
	}
	resetComputerUseExperienceTrace()
	globalComputerUse.mu.Lock()
	if globalComputerUse.taskStates == nil {
		globalComputerUse.taskStates = make(map[string]*computerUseTaskState)
	}
	globalComputerUse.taskStates[owner] = st
	globalComputerUse.mu.Unlock()
}

const desktopMessageStopText = "the requested message was already submitted. Call computer_done now with three lines: whether it was sent, to whom, and the exact text. Do not click, type, focus, or press Enter again."

// desktopMessagePayload is the text after 告诉它 / 告诉他 / 内容是 in a
// desktop IM send. A goal that is not that send has no payload, so typing
// and Enter in another task do not stop the desktop.
func desktopMessagePayload(goal string) string {
	if !computeruse.RequestsDesktopMessageSend(goal) {
		return ""
	}
	markers := []string{
		"告诉它", "告诉他", "告诉她",
		"告訴它", "告訴他", "告訴她",
		"跟它说", "跟他说", "跟她说",
		"内容就是", "内容是",
	}
	bestAt := -1
	best := ""
	for _, marker := range markers {
		i := strings.Index(goal, marker)
		if i < 0 || (bestAt >= 0 && i >= bestAt) {
			continue
		}
		bestAt = i
		best = goal[i+len(marker):]
	}
	if bestAt < 0 {
		return ""
	}
	if i := strings.IndexAny(best, "\r\n"); i >= 0 {
		best = best[:i]
	}
	if i := strings.Index(strings.ToLower(best), "constraints:"); i >= 0 {
		best = best[:i]
	}
	best = strings.TrimSpace(best)
	best = strings.TrimLeft(best, "，,：:、 ")
	best = strings.TrimSpace(best)
	if utf8.RuneCountInString(compactDesktopMessage(best)) < 4 {
		return ""
	}
	return best
}

func compactDesktopMessage(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func desktopMessageCovers(body, typed string) bool {
	b := compactDesktopMessage(body)
	t := compactDesktopMessage(typed)
	if utf8.RuneCountInString(b) < 4 || t == "" {
		return false
	}
	return strings.Contains(t, b)
}

func noteDesktopMessageTyped(text string) {
	owner := computerUseOwnerKey()
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	st := globalComputerUse.taskStates[owner]
	if st == nil || st.MessageBody == "" || st.MessageSubmitted {
		return
	}
	st.typedCompact += compactDesktopMessage(text)
	if desktopMessageCovers(st.MessageBody, st.typedCompact) {
		st.MessageTyped = true
	}
}

func desktopMessageSubmitChord(detail string) bool {
	parts := strings.FieldsFunc(strings.ToLower(detail), func(r rune) bool {
		return r == '+' || r == ' '
	})
	sawEnter := false
	for _, part := range parts {
		part = strings.ReplaceAll(part, "_", "")
		switch part {
		case "enter", "return", "kpenter":
			sawEnter = true
		case "shift", "shiftl", "shiftr", "alt", "altl", "altr", "meta", "super", "win", "cmd", "command":
			// Shift+Enter is a newline in chat. It must not latch the send.
			return false
		}
	}
	return sawEnter
}

// noteDesktopMessageSubmitted latches after the requested message has been
// typed and Enter was actually sent. A search-box Enter before that text
// does not latch. The return is true only on the transition.
func noteDesktopMessageSubmitted(detail string) bool {
	if !desktopMessageSubmitChord(detail) {
		return false
	}
	owner := computerUseOwnerKey()
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	st := globalComputerUse.taskStates[owner]
	if st == nil || !st.MessageTyped || st.MessageSubmitted {
		return false
	}
	st.MessageSubmitted = true
	return true
}

func cuRefuseAfterDesktopMessageSent(tool string) string {
	if tool == "computer_done" || tool == "computer_playbook" {
		return ""
	}
	owner := computerUseOwnerKey()
	globalComputerUse.mu.Lock()
	submitted := false
	if st := globalComputerUse.taskStates[owner]; st != nil {
		submitted = st.MessageSubmitted
	}
	globalComputerUse.mu.Unlock()
	if !submitted {
		return ""
	}
	return desktopMessageStopText
}

// parentDesktopSendOwnedText is the refusal for a second delivery path.
// The chat loop does not drive the desktop. The helper already owns the send.
const parentDesktopSendOwnedText = "the desktop helper is already sending this one message. Do not install a skill, write a script, or send it again. Tell the person the helper result when it appears."

// parentDesktopSendToolRefusal blocks the chat loop from delivering a desktop
// IM send that a helper already owns. The helper loop itself is not blocked.
func parentDesktopSendToolRefusal(loop *LoopContext, tool string) string {
	if loop == nil || loop.ComputerHelper || !loop.DesktopMessageHelperOwns {
		return ""
	}
	switch strings.TrimSpace(tool) {
	case "craft_tool", "search_and_install_skill", "send_to_im", "send_file", "bash", "write_file", chatTaskToolDispatch:
		return parentDesktopSendOwnedText
	default:
		return ""
	}
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
	stored := strings.TrimSpace(goal)
	if rt := strings.TrimSpace(loopCtx.ComputerUseRoutingText); rt != "" {
		stored = rt
	}
	beginComputerUseTask(owner, req, stored, nil)
	loopCtx.ComputerUseBegun = true
}

func recordComputerUseGate(h *IMMessageHandler, ctx *LoopContext, routingText string) (active, fresh bool) {
	if h == nil {
		return false, false
	}
	if ctx != nil && ctx.ComputerUseGateSettled {
		return ctx.ComputerUseActive, ctx.ComputerUseFresh
	}
	if ctx != nil && ctx.ComputerHelper {
		if !ctx.ComputerUseGateSettled {
			ctx.ComputerUseGateSettled = true
			ctx.ComputerUseActive = true
			ctx.ComputerUseFresh = true
		}
		return ctx.ComputerUseActive, ctx.ComputerUseFresh
	}
	active, fresh = h.gateComputerUse(routingText)
	// A group chat does not get a computer helper for an IM send. An explicit
	// @computer request keeps the previous opt-in. The send decision is the
	// classifier label, not a phrase list.
	deliver := h.desktopMessageIntentOpen(routingText)
	if active && computerUseMessageSendBlocked(ctx, routingText, deliver) {
		active = false
		fresh = false
	}
	if ctx != nil && loopKeepsDrivingTools(ctx) {
		ctx.ComputerUseGateSettled = true
		ctx.ComputerUseActive = active
		ctx.ComputerUseFresh = fresh
		return active, fresh
	}
	if ctx != nil {
		ctx.ComputerUseGateSettled = true
		ctx.ComputerUseActive = false
		ctx.ComputerUseFresh = fresh
		if active {
			noteMainComputerDispatch(h, ctx, routingText, deliver)
		}
	}
	return false, fresh
}

func noteMainComputerDispatch(h *IMMessageHandler, ctx *LoopContext, text string, deliver bool) {
	if ctx == nil || ctx.ComputerHelper || ctx.ComputerHelperNoted {
		return
	}
	ctx.ComputerHelperNoted = true
	screen := strings.TrimSpace(ctx.ComputerUseOwner)
	if screen == "" {
		screen = strings.TrimSpace(ctx.UserID)
	}
	if screen == "" {
		screen = strings.TrimSpace(ctx.SessionID)
	}
	card := computerHelperCard(text, deliver)
	ctx.ComputerHelperGoal = card.Goal
	ctx.ComputerHelperLimits = card.Constraints
	ctx.ComputerHelperSuccess = card.SuccessCriteria
	ctx.DesktopMessageHelperOwns = deliver
	id := computerHelperLeaseID(screen)
	started, queued := desktopbot.ClaimComputerScreen(screen, id)
	ctx.ComputerHelperDispatched = started
	ctx.ComputerHelperQueued = queued
	if !started || h == nil || h.app == nil {
		return
	}
	reg := h.ensureChatTasks()
	if reg == nil {
		desktopbot.ReleaseComputerScreen(screen, id)
		ctx.ComputerHelperDispatched = false
		ctx.ComputerHelperQueued = true
		return
	}
	session := strings.TrimSpace(ctx.UserID)
	if _, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{
		Text:       card.Text(),
		SessionKey: session,
		Computer:   true,
		Screen:     screen,
	}); err != nil {
		desktopbot.ReleaseComputerScreen(screen, id)
		ctx.ComputerHelperDispatched = false
		ctx.ComputerHelperQueued = true
	}
}

// computerUseMessageSendBlocked keeps a group conversation from starting the
// desktop helper just because the sentence sends a chat message. The helper
// would drive this machine. Explicit @computer is unchanged.
func computerUseMessageSendBlocked(ctx *LoopContext, routingText string, deliver bool) bool {
	if ctx == nil || ctx.LansengerGroupPermissions == nil || !deliver {
		return false
	}
	if computeruse.HasExplicitTrigger(computerUseGateText(routingText)) {
		return false
	}
	return true
}

// computerHelperCard is the text the helper runs. A desktop IM send is
// allowed to send exactly the message the user asked for, and it is done
// without a file. Every other job keeps the default card, which refuses
// sending and requires a path.
func computerHelperCard(text string, deliver bool) desktopbot.HelperCard {
	goal := computerHelperGoalText(text)
	card := desktopbot.CardForGoal(goal)
	if !deliver {
		return card
	}
	card.Constraints = "只发送用户要求的这一条消息，不另发内容，不付款，不新开站点。"
	// The helper report has fixed slots: line 1 finished, line 2 left, line 3
	// blocked. Putting the recipient and the text on lines 2 and 3 makes a
	// sent message look unfinished.
	card.SuccessCriteria = "第 1 行同时写是否发出、发给谁、原文。第 2 行只写无。第 3 行只写无。没有文件也算完成。"
	return card
}

// computerHelperGoalText drops the addressing trigger. The card names the
// work. It does not name click, type, or computer.
func computerHelperGoalText(text string) string {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"@computer", "computer-use", "computer_use", "computer use"} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := strings.TrimSpace(text[len(prefix):])
		rest = strings.TrimLeft(rest, " :：,，")
		if rest != "" {
			return rest
		}
	}
	return text
}

func computerHelperLeaseID(screen string) string {
	screen = strings.TrimSpace(screen)
	if screen == "" {
		screen = "default"
	}
	return "cu-" + screen
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
		if ctx.ComputerHelperDispatched || ctx.ComputerHelperQueued {
			return
		}
		// The classifier often names computer_use for "写一份简历". The gate
		// refused the surface; drop that label so the semantic planner does
		// not still require desktop control.
		releaseFalseComputerUseClassification(ctx)
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
	b.WriteString("Host acceptance (do not call computer_done until every line is visible in the latest screen refresh, from computer_observe or from the observe attached to the last action):\n")
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
