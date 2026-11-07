package agent

import (
	"log"
	"strings"
)

const (
	handoffChatKeepGroups   = 4
	handoffChatThresholdPct = 40
	handoffTailTokenMax     = 8000
)

// HandoffChatThresholdPct is the chat checkpoint line. The comparison stays
// "below when before*100 < budget*threshold", so 40 fires at or above 40%.
func HandoffChatThresholdPct() int { return handoffChatThresholdPct }

// HandoffTailTokenCap is min(8000, 12% of the message budget). The budget
// floor is 4000, matching CheckpointConversation.
func HandoffTailTokenCap(budget int) int {
	if budget < 4000 {
		budget = 4000
	}
	cap := budget * 12 / 100
	if cap > handoffTailTokenMax {
		cap = handoffTailTokenMax
	}
	if cap < 1 {
		cap = 1
	}
	return cap
}

// HandoffPinnedGroupFlags marks groups that the oldest-drop must skip.
// A user whose text starts with [系统续轮] pins that group and the following
// groups until the next user. A user whose text equals keepEquals pins that
// one group. plain[i] is false when the group is not a plain string user.
func HandoffPinnedGroupFlags(roles, texts []string, plain []bool, keepEquals []string) []bool {
	n := len(roles)
	pinned := make([]bool, n)
	for i := 0; i < n; i++ {
		if i >= len(texts) || i >= len(plain) || roles[i] != "user" || !plain[i] {
			continue
		}
		text := strings.TrimSpace(texts[i])
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, SessionRoundPrefix) {
			pinned[i] = true
			for j := i + 1; j < n; j++ {
				if roles[j] == "user" {
					break
				}
				pinned[j] = true
			}
			continue
		}
		for _, eq := range keepEquals {
			if text == strings.TrimSpace(eq) {
				pinned[i] = true
				break
			}
		}
	}
	return pinned
}

// SelectHandoffKeep keeps the newest groups, at most four, whose tokens fit
// in cap. The newest group stays even when it alone exceeds cap. Pinned
// groups stay in addition to that window, including a wave that alone
// exceeds the budget.
func SelectHandoffKeep(costs []int, pinned []bool, cap int) []bool {
	n := len(costs)
	keep := make([]bool, n)
	if n == 0 {
		return keep
	}
	if cap < 1 {
		cap = 1
	}
	running := 0
	count := 0
	for i := n - 1; i >= 0 && count < handoffChatKeepGroups; i-- {
		if count >= 1 && running+costs[i] > cap {
			break
		}
		keep[i] = true
		running += costs[i]
		count++
	}
	for i := range pinned {
		if i < n && pinned[i] {
			keep[i] = true
		}
	}
	return keep
}

// LastMemoInMessages returns the last parseable memo in msgs.
func LastMemoInMessages(msgs []interface{}) (HandoffMemo, bool) {
	var found HandoffMemo
	ok := false
	for _, msg := range msgs {
		_, content := ExtractRoleContent(msg)
		if !strings.Contains(content, handoffOpen) {
			continue
		}
		parsed, parsedOK := ParseHandoffMemo(content)
		if parsedOK {
			found = parsed
			ok = true
		}
	}
	return found, ok
}

// ResolveCoveredMemo builds one memo for messages that are leaving context.
// A summary is used only when the span does not already contain a memo and
// the summary parses into a usable memo. hadPrior means the caller must not
// summarize this span again.
func ResolveCoveredMemo(msgs []interface{}, summary string, summaryOK bool) (HandoffMemo, bool) {
	prior, hadPrior := LastMemoInMessages(msgs)
	memo := MemoFromDroppedProjects(msgs, prior, false)
	if hadPrior || !summaryOK {
		return memo, hadPrior
	}
	parsed, ok := ParseHandoffMemo(summary)
	if ok && parsed.Usable() {
		return parsed, false
	}
	return memo, false
}

// NoteCoveredMemo records one outcome. fromLLM is true only when the memo
// that replaced the span parsed from the summarizer. A deterministic fill,
// including a usable one, is a fallback. keptExtra is the additional recent
// group. The log line is lengths only.
func NoteCoveredMemo(memo HandoffMemo, fromLLM, keptExtra bool) {
	if fromLLM && memo.Usable() {
		NoteHandoffApplied()
	} else {
		NoteHandoffFallback()
	}
	if keptExtra {
		NoteHandoffKeptExtraGroup()
	}
	log.Printf("[handoff-memo] %s", memo.LengthNote())
}

// preservePriorProgress keeps a measurement that lives only in the covering
// memo. A fill built from the older slice must not drop it.
func preservePriorProgress(memo *HandoffMemo, prior HandoffMemo) {
	if memo == nil {
		return
	}
	progress := strings.TrimSpace(prior.Progress)
	if progress == "" || HandoffTextEmpty(progress) {
		return
	}
	if strings.Contains(memo.Progress, progress) {
		return
	}
	if strings.TrimSpace(memo.Progress) == "" {
		memo.Progress = progress
		return
	}
	memo.Progress = progress + "\n" + strings.TrimSpace(memo.Progress)
}

// memoForCoveredSpan returns the memo for messages that are leaving context.
// fromLLM is true only when summary itself parsed into a usable memo and the
// span does not already contain one. Otherwise the deterministic fill keeps
// the prior memo, including progress that is not in the dropped slice.
func MemoForCoveredSpan(scope, dropped []interface{}, summary string, summaryOK bool) (HandoffMemo, bool) {
	prior, hadPrior := LastMemoInMessages(scope)
	if !hadPrior && summaryOK {
		if parsed, ok := ParseHandoffMemo(summary); ok && parsed.Usable() {
			return parsed, true
		}
	}
	memo := MemoFromDroppedProjects(dropped, prior, false)
	preservePriorProgress(&memo, prior)
	return memo, false
}

// FitHandoffPreview shrinks a memo until its block is within byteLimit.
// Shelf handles, dead ends, and read paths go first. It does not insert a
// recovery marker or a middle ellipsis.
func FitHandoffPreview(memo HandoffMemo, byteLimit int) string {
	if byteLimit < len(handoffOpen)+len(handoffClose)+2 {
		byteLimit = len(handoffOpen) + len(handoffClose) + 2
	}
	fitted := memo
	for len(fitted.RenderBlock()) > byteLimit {
		switch {
		case len(fitted.ShelfHandles) > 0:
			fitted.ShelfHandles = fitted.ShelfHandles[1:]
		case len(fitted.DeadEnds) > 0:
			fitted.DeadEnds = fitted.DeadEnds[1:]
		case len(fitted.FilesRead) > 0:
			fitted.FilesRead = fitted.FilesRead[1:]
		case len(fitted.FilesModified) > 0:
			fitted.FilesModified = fitted.FilesModified[1:]
		case len(fitted.FilesCreated) > 0:
			fitted.FilesCreated = fitted.FilesCreated[1:]
		case runeLen(fitted.Progress) > 1:
			fitted.Progress = tailCut(fitted.Progress, runeLen(fitted.Progress)-1)
		case runeLen(fitted.Task) > 1:
			fitted.Task = tailCut(fitted.Task, runeLen(fitted.Task)-1)
		case runeLen(fitted.Next) > 1:
			fitted.Next = headCut(fitted.Next, runeLen(fitted.Next)-1)
		default:
			fitted.Progress = ""
			fitted.Task = ""
			fitted.Next = ""
			return fitted.RenderBlock()
		}
	}
	return fitted.RenderBlock()
}

// MessageContainsOpaque reports an image block that must stay inline.
func MessageContainsOpaque(message interface{}) bool {
	return checkpointContainsOpaqueContent([]interface{}{message})
}

// EntriesHavePendingAsk is the history-entry form of ConversationHasPendingAsk.
func EntriesHavePendingAsk(entries []ConversationEntry) bool {
	msgs := make([]interface{}, len(entries))
	for i, entry := range entries {
		msgs[i] = entry.ToMessage()
	}
	return conversationHasPendingAsk(msgs)
}

// FoldHandoffUser joins the preview at previewIndex into an adjacent
// unpinned plain-string user. The pin is judged on the text before the
// fold. A system message, a [系统续轮] user, a keepEquals user, and a
// non-string content stay as they are. The preview stays its own user when
// neither neighbor can take it.
func FoldHandoffUser(messages []interface{}, previewIndex int, keepEquals []string) []interface{} {
	if previewIndex <= 0 || previewIndex >= len(messages) {
		return messages
	}
	memoText, ok := plainStringContent(messages[previewIndex])
	if !ok || !strings.Contains(memoText, handoffOpen) {
		return messages
	}
	parsed, parsedOK := ParseHandoffMemo(memoText)
	inner := ""
	if parsedOK {
		inner = parsed.Render()
	}
	prevText, prevOK := foldableUser(messages[previewIndex-1], keepEquals)
	nextText := ""
	nextOK := false
	if previewIndex+1 < len(messages) {
		nextText, nextOK = foldableUser(messages[previewIndex+1], keepEquals)
	}
	if inner == "" {
		inner = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(memoText, handoffOpen), handoffClose))
	}
	switch {
	case prevOK && nextOK:
		base := strings.TrimSpace(HandoffOutside(prevText) + "\n\n" + HandoffOutside(nextText))
		setPlainContent(messages[previewIndex-1], ReplaceHandoffBlock(base, inner))
		return removeMessages(messages, previewIndex, previewIndex+1)
	case prevOK:
		setPlainContent(messages[previewIndex-1], ReplaceHandoffBlock(prevText, inner))
		return removeMessages(messages, previewIndex)
	case nextOK:
		setPlainContent(messages[previewIndex+1], ReplaceHandoffBlock(nextText, inner))
		return removeMessages(messages, previewIndex)
	default:
		return messages
	}
}

func foldableUser(msg interface{}, keepEquals []string) (string, bool) {
	if MsgRole(msg) != "user" {
		return "", false
	}
	text, ok := plainStringContent(msg)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, SessionRoundPrefix) {
		return "", false
	}
	for _, eq := range keepEquals {
		if trimmed == strings.TrimSpace(eq) && trimmed != "" {
			return "", false
		}
	}
	return text, true
}

func plainStringContent(msg interface{}) (string, bool) {
	switch m := msg.(type) {
	case map[string]string:
		return m["content"], true
	case map[string]interface{}:
		text, ok := m["content"].(string)
		return text, ok
	case ConversationEntry:
		text, ok := m.Content.(string)
		return text, ok
	default:
		return "", false
	}
}

func setPlainContent(msg interface{}, content string) {
	switch m := msg.(type) {
	case map[string]string:
		m["content"] = content
	case map[string]interface{}:
		m["content"] = content
	}
}

func removeMessages(messages []interface{}, indexes ...int) []interface{} {
	drop := map[int]bool{}
	for _, idx := range indexes {
		drop[idx] = true
	}
	out := make([]interface{}, 0, len(messages))
	for i, msg := range messages {
		if drop[i] {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// StripExtraHandoffMemos leaves one memo block. primary is the message that
// keeps it. Other plain-string messages lose the block and keep the rest.
func StripExtraHandoffMemos(messages []interface{}, primary int) []interface{} {
	for i, msg := range messages {
		if i == primary {
			continue
		}
		text, ok := plainStringContent(msg)
		if !ok || !strings.Contains(text, handoffOpen) {
			continue
		}
		setPlainContent(msg, HandoffOutside(text))
	}
	return messages
}

// trimHistoryWithMemo is the flag-all TrimHistory. The turn and token
// triggers stay MaxConversationTurns and MaxMemoryTokenEstimate. The note
// is one memo, pins are not dropped, and a pending ask is left untouched.
func trimHistoryWithMemo(entries []ConversationEntry) []ConversationEntry {
	if EntriesHavePendingAsk(entries) {
		return entries
	}
	if len(entries) <= MaxConversationTurns && EstimateConversationEntryTokens(entries) <= MaxMemoryTokenEstimate {
		return entries
	}
	groups := BuildEntryGroups(entries)
	if len(groups) <= 1 {
		return entries
	}
	drop := handoffHistoryDrop(entries, groups)
	var dropped, kept []ConversationEntry
	for i, group := range groups {
		part := entries[group.Start:group.End]
		if drop[i] {
			dropped = append(dropped, part...)
			continue
		}
		kept = append(kept, part...)
	}
	if len(dropped) == 0 || (len(kept) > 0 && kept[0].Role == "tool") {
		if len(kept) == 0 {
			return entries
		}
		return kept
	}
	return prependHandoffMemo(dropped, kept)
}

func handoffHistoryDrop(entries []ConversationEntry, groups []EntryGroup) []bool {
	pinned := entryGroupPins(entries, groups, nil)
	drop := make([]bool, len(groups))
	for {
		var kept []ConversationEntry
		keptGroups := 0
		for i, group := range groups {
			if drop[i] {
				continue
			}
			keptGroups++
			kept = append(kept, entries[group.Start:group.End]...)
		}
		overTurns := len(kept) > MaxConversationTurns
		overTokens := EstimateConversationEntryTokens(kept) > MaxMemoryTokenEstimate
		if !overTurns && !overTokens {
			return drop
		}
		idx := -1
		for i := range groups {
			if !drop[i] && !pinned[i] {
				idx = i
				break
			}
		}
		if idx < 0 || keptGroups <= 1 {
			return drop
		}
		drop[idx] = true
	}
}

func entryGroupPins(entries []ConversationEntry, groups []EntryGroup, keepEquals []string) []bool {
	n := len(groups)
	roles := make([]string, n)
	texts := make([]string, n)
	plain := make([]bool, n)
	pinned := make([]bool, n)
	for i, group := range groups {
		if group.Start < 0 || group.End > len(entries) || group.Start >= group.End {
			continue
		}
		entry := entries[group.Start]
		roles[i] = entry.Role
		if text, ok := entry.Content.(string); ok && entry.Role == "user" {
			texts[i] = text
			plain[i] = true
		}
		for j := group.Start; j < group.End; j++ {
			if MessageContainsOpaque(entries[j].ToMessage()) {
				pinned[i] = true
			}
		}
	}
	for i, on := range HandoffPinnedGroupFlags(roles, texts, plain, keepEquals) {
		if on {
			pinned[i] = true
		}
	}
	return pinned
}

func prependHandoffMemo(dropped, kept []ConversationEntry) []ConversationEntry {
	if _, ok := LastMemoInMessages(entriesToMessages(kept)); ok {
		return kept
	}
	msgs := entriesToMessages(dropped)
	memo, fromLLM := MemoForCoveredSpan(append(entriesToMessages(kept), msgs...), msgs, "", false)
	keptExtra := false
	if !memo.Usable() {
		extra := newestEntryGroup(dropped)
		if len(extra) > 0 && len(extra) < len(dropped) {
			dropped = dropped[:len(dropped)-len(extra)]
			kept = append(append([]ConversationEntry{}, extra...), kept...)
			keptExtra = true
			memo, fromLLM = MemoForCoveredSpan(append(entriesToMessages(kept), entriesToMessages(dropped)...), entriesToMessages(dropped), "", false)
		} else if len(extra) > 0 && len(dropped) == len(extra) {
			NoteCoveredMemo(memo, false, true)
			return append(append([]ConversationEntry{}, extra...), kept...)
		}
	}
	NoteCoveredMemo(memo, fromLLM, keptExtra)
	if !memo.Usable() {
		return kept
	}
	block := memo.RenderBlock()
	if len(kept) > 0 {
		if text, ok := kept[0].Content.(string); ok && kept[0].Role == "user" {
			if _, fold := foldableUser(kept[0].ToMessage(), nil); fold {
				kept[0].Content = ReplaceHandoffBlock(text, memo.Render())
				return kept
			}
		}
	}
	handoff := ConversationEntry{Role: "user", Content: block}
	return append([]ConversationEntry{handoff}, kept...)
}

func historyHandoffMemoEntry(dropped []ConversationEntry) ConversationEntry {
	if len(dropped) == 0 {
		return ConversationEntry{}
	}
	memo, _ := ResolveCoveredMemo(entriesToMessages(dropped), "", false)
	if !memo.Usable() {
		return ConversationEntry{}
	}
	return ConversationEntry{Role: "user", Content: memo.RenderBlock()}
}

func newestEntryGroup(entries []ConversationEntry) []ConversationEntry {
	groups := BuildEntryGroups(entries)
	if len(groups) == 0 {
		return nil
	}
	last := groups[len(groups)-1]
	if last.Start < 0 || last.End > len(entries) || last.Start >= last.End {
		return nil
	}
	return entries[last.Start:last.End]
}

func entriesToMessages(entries []ConversationEntry) []interface{} {
	msgs := make([]interface{}, len(entries))
	for i, entry := range entries {
		msgs[i] = entry.ToMessage()
	}
	return msgs
}

// trimConversationHandoff replaces the covered prefix of a loop transcript
// with one memo. msgBudget is the message budget already computed by
// TrimConversation. The live tail is four groups inside the tail cap, plus
// pins. The Chinese omission note is not used.
func trimConversationHandoff(msgs []interface{}, msgBudget int, summarize func(string) string) []interface{} {
	if len(msgs) <= 1 {
		return msgs
	}
	systemCount := 0
	if MsgRole(msgs[0]) == "system" {
		systemCount = 1
	}
	tail := msgs[systemCount:]
	groups, inlineOK := buildCheckpointGroups(tail)
	if len(groups) <= 1 {
		return msgs
	}
	costs := make([]int, len(groups))
	for i, group := range groups {
		for j := group.Start; j < group.End && j < len(tail); j++ {
			costs[i] += estimateCheckpointMessageTokens(tail[j])
		}
	}
	budget := msgBudget
	if budget < 4000 {
		budget = 4000
	}
	n := len(groups)
	roles := make([]string, n)
	texts := make([]string, n)
	plain := make([]bool, n)
	pinned := make([]bool, n)
	for i, group := range groups {
		if group.Start < 0 || group.End > len(tail) || group.Start >= group.End {
			continue
		}
		msgs := tail[group.Start:group.End]
		roles[i] = MsgRole(msgs[0])
		if text, ok := plainStringContent(msgs[0]); ok && roles[i] == "user" {
			texts[i] = text
			plain[i] = true
		}
		if checkpointContainsOpaqueContent(msgs) {
			pinned[i] = true
		}
	}
	for i, on := range HandoffPinnedGroupFlags(roles, texts, plain, nil) {
		if on {
			pinned[i] = true
		}
	}
	keep := SelectHandoffKeep(costs, pinned, HandoffTailTokenCap(budget))
	drop := make([]bool, n)
	for i, group := range groups {
		if group.Start < 0 || group.End > len(tail) || group.Start >= group.End {
			continue
		}
		part := tail[group.Start:group.End]
		if checkpointContainsOpaqueContent(part) {
			continue
		}
		broken := i < len(inlineOK) && !inlineOK[i]
		if (broken && !pinned[i]) || !keep[i] {
			drop[i] = true
		}
	}
	dropped := collectCheckpointDropped(tail, groups, drop)
	if len(dropped) == 0 {
		return msgs
	}
	if keptTailHasMemo(tail, groups, drop) {
		return assembleUnchangedTail(msgs, systemCount, tail, groups, drop)
	}
	summary := ""
	summaryOK := false
	_, hadPrior := LastMemoInMessages(tail)
	if summarize != nil && !hadPrior {
		if input := droppedMessagesForSummary(dropped); input != "" {
			summary = summarize(input)
			summaryOK = summaryAccepted(summary)
		}
	}
	memo, fromLLM := MemoForCoveredSpan(tail, dropped, summary, summaryOK)
	keptExtra := false
	if !memo.Usable() {
		for i := n - 1; i >= 0; i-- {
			if !drop[i] {
				continue
			}
			group := groups[i]
			part := tail[group.Start:group.End]
			if checkpointContainsOpaqueContent(part) || (i < len(inlineOK) && !inlineOK[i]) {
				continue
			}
			drop[i] = false
			keptExtra = true
			break
		}
		dropped = collectCheckpointDropped(tail, groups, drop)
		memo, fromLLM = MemoForCoveredSpan(tail, dropped, "", false)
	}
	NoteCoveredMemo(memo, fromLLM, keptExtra)
	content := ""
	if memo.Usable() {
		content = memo.RenderBlock()
	}
	next := make([]interface{}, 0, len(msgs))
	if systemCount == 1 {
		next = append(next, msgs[0])
	}
	inserted := false
	for i, group := range groups {
		if i < len(drop) && drop[i] {
			if !inserted && content != "" {
				next = append(next, map[string]string{"role": "user", "content": content})
				inserted = true
			}
			continue
		}
		if group.Start < 0 || group.End > len(tail) || group.Start >= group.End {
			continue
		}
		next = append(next, tail[group.Start:group.End]...)
	}
	if content != "" {
		if idx := indexOfExactContent(next, content); idx >= 0 {
			next = FoldHandoffUser(next, idx, nil)
		}
		primary := -1
		for i, msg := range next {
			text, ok := plainStringContent(msg)
			if ok && strings.Contains(text, handoffOpen) {
				primary = i
				break
			}
		}
		if primary >= 0 {
			next = StripExtraHandoffMemos(next, primary)
		}
	}
	return next
}

func keptTailHasMemo(tail []interface{}, groups []EntryGroup, drop []bool) bool {
	for i, group := range groups {
		if i < len(drop) && drop[i] {
			continue
		}
		if group.Start < 0 || group.End > len(tail) || group.Start >= group.End {
			continue
		}
		if _, ok := LastMemoInMessages(tail[group.Start:group.End]); ok {
			return true
		}
	}
	return false
}

func assembleUnchangedTail(msgs []interface{}, systemCount int, tail []interface{}, groups []EntryGroup, drop []bool) []interface{} {
	next := make([]interface{}, 0, len(msgs))
	if systemCount == 1 && len(msgs) > 0 {
		next = append(next, msgs[0])
	}
	for i, group := range groups {
		if i < len(drop) && drop[i] {
			continue
		}
		if group.Start < 0 || group.End > len(tail) || group.Start >= group.End {
			continue
		}
		next = append(next, tail[group.Start:group.End]...)
	}
	return next
}

func conversationHasToolLoop(msgs []interface{}) bool {
	for _, msg := range msgs {
		if MsgRole(msg) == "tool" || MsgHasToolCalls(msg) {
			return true
		}
	}
	return false
}

func indexOfExactContent(messages []interface{}, content string) int {
	for i, msg := range messages {
		text, ok := plainStringContent(msg)
		if ok && text == content {
			return i
		}
	}
	return -1
}
