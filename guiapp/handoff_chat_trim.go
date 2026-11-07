package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func confirmationOriginalTexts(h *IMMessageHandler, userID string) []string {
	if h == nil || h.confirmationStore == nil {
		return nil
	}
	item := h.confirmationStore.get(userID)
	if item == nil || item.Status != confirmationStatusPending {
		return nil
	}
	text := strings.TrimSpace(item.OriginalText)
	if text == "" {
		return nil
	}
	return []string{text}
}

// trimHistoryWithHandoffMemo is the flag-all persistence trim. It replaces
// the covered prefix with one memo and a short tail. It does not call
// memorySink and does not emit the middle-omitted placeholder.
func trimHistoryWithHandoffMemo(entries []agent.ConversationEntry, summarizer func(string) string, maxEntries, maxTokens, precomputed int, keepEquals []string) []agent.ConversationEntry {
	if len(entries) == 0 {
		return entries
	}
	if agent.EntriesHavePendingAsk(entries) {
		return entries
	}
	limit := agent.MaxConversationTurns
	if maxEntries > 0 {
		limit = maxEntries
	}
	tok := precomputed
	if tok <= 0 && (maxTokens > 0 || len(entries) > limit) {
		tok = estimateConversationEntryTokens(entries)
	}
	entryOver := len(entries) > limit
	tokenOver := maxTokens > 0 && tok*100 >= maxTokens*agent.HandoffChatThresholdPct()
	if !entryOver && !tokenOver {
		return entries
	}
	groups := agent.BuildEntryGroups(entries)
	if len(groups) <= 1 {
		return entries
	}
	costs := make([]int, len(groups))
	for i, group := range groups {
		for j := group.Start; j < group.End && j < len(entries); j++ {
			costs[i] += entryTokenCount(entries[j])
		}
	}
	budget := maxTokens
	if budget <= 0 {
		budget = agent.MaxMemoryTokenEstimate
	}
	roles := make([]string, len(groups))
	texts := make([]string, len(groups))
	plain := make([]bool, len(groups))
	opaque := make([]bool, len(groups))
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
			if agent.MessageContainsOpaque(entries[j].ToMessage()) {
				opaque[i] = true
			}
		}
	}
	pinned := agent.HandoffPinnedGroupFlags(roles, texts, plain, keepEquals)
	for i := range pinned {
		if opaque[i] {
			pinned[i] = true
		}
	}
	keep := agent.SelectHandoffKeep(costs, pinned, agent.HandoffTailTokenCap(budget))
	drop := make([]bool, len(groups))
	for i := range groups {
		if opaque[i] || keep[i] {
			continue
		}
		drop[i] = true
	}
	droppedEntries := collectEntryGroups(entries, groups, drop)
	if len(droppedEntries) == 0 {
		return entries
	}
	kept := collectEntryGroups(entries, groups, invertFlags(drop))
	// A memo already in the kept tail is Prior for MemoForCoveredSpan. Returning
	// the tail here would drop user constraints that only exist in the prefix.
	summary := ""
	summaryOK := false
	_, hadPrior := agent.LastMemoInMessages(entriesToLoopMessages(entries))
	if summarizer != nil && !hadPrior {
		source := buildCompactionSource("", buildCompactionSummarizerInput(droppedEntries))
		if strings.TrimSpace(source) != "" {
			summary = summarizer(source)
			summaryOK = summary != "" && !compactionSummaryUnusable(summary, "")
		}
	}
	memo, fromLLM := agent.MemoForCoveredSpan(entriesToLoopMessages(entries), entriesToLoopMessages(droppedEntries), summary, summaryOK)
	keptExtra := false
	if !memo.Usable() {
		for i := len(drop) - 1; i >= 0; i-- {
			if !drop[i] || opaque[i] {
				continue
			}
			drop[i] = false
			keptExtra = true
			break
		}
		droppedEntries = collectEntryGroups(entries, groups, drop)
		kept = collectEntryGroups(entries, groups, invertFlags(drop))
		memo, fromLLM = agent.MemoForCoveredSpan(entriesToLoopMessages(entries), entriesToLoopMessages(droppedEntries), "", false)
	}
	agent.NoteCoveredMemo(memo, fromLLM, keptExtra)
	if !memo.Usable() {
		return kept
	}
	for i := range kept {
		text, ok := kept[i].Content.(string)
		if !ok || !strings.Contains(text, "<handoff-memo>") {
			continue
		}
		kept[i].Content = agent.ReplaceHandoffBlock(text, memo.Render())
		return kept
	}
	separator := agent.ConversationEntry{
		Role:    "system",
		Content: memo.RenderBlock(),
	}
	if !hadPrior {
		separator.CompactionID = newCompactionID()
		separator.CompactionSource = buildCompactionSource("", buildCompactionSummarizerInput(droppedEntries))
	}
	if len(kept) > 0 && kept[0].Role == "system" {
		if text, ok := kept[0].Content.(string); ok {
			kept[0].Content = agent.ReplaceHandoffBlock(text, memo.Render())
			if separator.CompactionID != "" && kept[0].CompactionID == "" {
				kept[0].CompactionID = separator.CompactionID
				kept[0].CompactionSource = separator.CompactionSource
			}
			return kept
		}
	}
	return append([]agent.ConversationEntry{separator}, kept...)
}

func collectEntryGroups(entries []agent.ConversationEntry, groups []agent.EntryGroup, want []bool) []agent.ConversationEntry {
	var out []agent.ConversationEntry
	for i, group := range groups {
		if i >= len(want) || !want[i] {
			continue
		}
		if group.Start < 0 || group.End > len(entries) || group.Start >= group.End {
			continue
		}
		out = append(out, entries[group.Start:group.End]...)
	}
	return out
}

func invertFlags(flags []bool) []bool {
	out := make([]bool, len(flags))
	for i, on := range flags {
		out[i] = !on
	}
	return out
}

func entriesToLoopMessages(entries []agent.ConversationEntry) []interface{} {
	msgs := make([]interface{}, len(entries))
	for i, entry := range entries {
		msgs[i] = entry.ToMessage()
	}
	return msgs
}

// handoffPageAnchorEntries is what PageIndex receives under flag all:
// memo fields, not the dropped assistant prose.
func handoffPageAnchorEntries(trimmed []agent.ConversationEntry) []memory.Entry {
	var memo agent.HandoffMemo
	found := false
	for _, entry := range trimmed {
		text, ok := entry.Content.(string)
		if !ok {
			continue
		}
		parsed, ok := agent.ParseHandoffMemo(text)
		if !ok {
			continue
		}
		memo = parsed
		found = true
	}
	if !found {
		return nil
	}
	var out []memory.Entry
	addPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		out = append(out, memory.Entry{Content: path, Category: "handoff_memo", Tags: []string{"file_path"}})
	}
	for _, path := range memo.FilesRead {
		addPath(path)
	}
	for _, path := range memo.FilesModified {
		addPath(path)
	}
	for _, path := range memo.FilesCreated {
		addPath(path)
	}
	if progress := oneAnchorLine(memo.Progress); progress != "" {
		out = append(out, memory.Entry{Content: "decided " + progress, Category: "handoff_memo", Tags: []string{"handoff_memo"}})
	}
	if next := oneAnchorLine(memo.Next); next != "" {
		out = append(out, memory.Entry{Content: next, Category: "handoff_memo", Tags: []string{"handoff_memo"}})
	}
	return out
}

func oneAnchorLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	runes := []rune(text)
	if len(runes) > 180 {
		text = string(runes[:180])
	}
	return strings.TrimSpace(text)
}
