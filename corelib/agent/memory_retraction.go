package agent

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MemoryRetractionMarker is the line-start heading for warehouse edits that
// must override conversation history and earlier memory-recall tool results.
const MemoryRetractionMarker = "[记忆更正]"

const (
	memoryRetractionMaxItems      = 12
	memoryRetractionMaxClaimRunes = 80
	memoryRetractionMinNeedle     = 8
	memoryRetractionTTL           = 30 * time.Minute
)

// MemoryRetractionItem is one user-edited warehouse fact.
type MemoryRetractionItem struct {
	Kind        string // "deleted" or "updated"
	Content     string
	Replacement string
	AdmittedAt  time.Time
}

// MemoryRetraction is the current set of warehouse edits the live agent must honor.
type MemoryRetraction struct {
	Items []MemoryRetractionItem
}

// MemoryRetractionHolder is an optional host callback for GUI/TUI warehouse edits.
type MemoryRetractionHolder interface {
	LoadMemoryRetractions() *MemoryRetraction
}

// CloneMemoryRetraction returns a deep copy, or nil.
func CloneMemoryRetraction(r *MemoryRetraction) *MemoryRetraction {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Items != nil {
		cp.Items = append([]MemoryRetractionItem(nil), r.Items...)
	}
	return &cp
}

// AdmitMemoryRetraction appends an edit, dropping older items past the cap.
func AdmitMemoryRetraction(r *MemoryRetraction, item MemoryRetractionItem) *MemoryRetraction {
	if r == nil {
		r = &MemoryRetraction{}
	}
	item.Kind = strings.TrimSpace(item.Kind)
	item.Content = strings.TrimSpace(item.Content)
	item.Replacement = strings.TrimSpace(item.Replacement)
	if item.Content == "" {
		return r
	}
	if item.Kind == "" {
		item.Kind = "deleted"
	}
	if item.AdmittedAt.IsZero() {
		item.AdmittedAt = time.Now()
	}
	item.Content = clipRunes(item.Content, memoryRetractionMaxClaimRunes)
	item.Replacement = clipRunes(item.Replacement, memoryRetractionMaxClaimRunes)
	kept := make([]MemoryRetractionItem, 0, len(r.Items)+1)
	for _, existing := range r.Items {
		if existing.Content == item.Content && existing.Kind == item.Kind {
			continue
		}
		kept = append(kept, existing)
	}
	r.Items = append(kept, item)
	if len(r.Items) > memoryRetractionMaxItems {
		r.Items = append([]MemoryRetractionItem(nil), r.Items[len(r.Items)-memoryRetractionMaxItems:]...)
	}
	return r
}

func pruneMemoryRetractions(r *MemoryRetraction, now time.Time) *MemoryRetraction {
	if r == nil || len(r.Items) == 0 {
		return r
	}
	kept := make([]MemoryRetractionItem, 0, len(r.Items))
	for _, item := range r.Items {
		if !item.AdmittedAt.IsZero() && now.Sub(item.AdmittedAt) > memoryRetractionTTL {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		r.Items = nil
		return r
	}
	r.Items = kept
	return r
}

// CompactMemoryRetraction drops expired items in place and returns nil when none remain.
func CompactMemoryRetraction(r *MemoryRetraction) *MemoryRetraction {
	if r == nil {
		return nil
	}
	pruneMemoryRetractions(r, time.Now())
	if len(r.Items) == 0 {
		return nil
	}
	return r
}

// RenderMemoryRetraction formats a non-empty retraction tail.
func RenderMemoryRetraction(r *MemoryRetraction) string {
	r = pruneMemoryRetractions(CloneMemoryRetraction(r), time.Now())
	if r == nil || len(r.Items) == 0 {
		return ""
	}
	var b strings.Builder
	deleted, updated := 0, 0
	for _, item := range r.Items {
		if item.Kind == "updated" {
			updated++
		} else {
			deleted++
		}
	}
	b.WriteString(MemoryRetractionMarker)
	b.WriteByte('\n')
	b.WriteString("用户刚在记忆管理中更新了长期记忆。对话里标记为「已从记忆仓库删除」的内容，以及任何未再被 memory recall 返回的旧条目，都不得当作事实使用。\n")
	if deleted > 0 {
		b.WriteString("- 已删除 ")
		b.WriteString(strconv.Itoa(deleted))
		b.WriteString(" 条\n")
	}
	if updated > 0 {
		b.WriteString("- 已改写 ")
		b.WriteString(strconv.Itoa(updated))
		b.WriteString(" 条；以仓库中的新内容为准\n")
		for _, item := range r.Items {
			if item.Kind != "updated" || strings.TrimSpace(item.Replacement) == "" {
				continue
			}
			b.WriteString("- 新内容: " + item.Replacement + "\n")
		}
	}
	return b.String()
}

// MemoryRetractionNeedles returns warehouse bodies that must be stripped from
// live conversation text. Replacement text is not included.
func MemoryRetractionNeedles(r *MemoryRetraction) []string {
	r = pruneMemoryRetractions(CloneMemoryRetraction(r), time.Now())
	if r == nil || len(r.Items) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.Items))
	seen := make(map[string]struct{}, len(r.Items))
	for _, item := range r.Items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		if _, ok := seen[content]; ok {
			continue
		}
		seen[content] = struct{}{}
		out = append(out, content)
	}
	return out
}

func loadInitialMemoryRetractions(cb LoopCallbacks) *MemoryRetraction {
	if holder, ok := cb.(MemoryRetractionHolder); ok {
		return pruneMemoryRetractions(CloneMemoryRetraction(holder.LoadMemoryRetractions()), time.Now())
	}
	return nil
}

func promptTailMarkers() []string {
	return []string{WorkingStateMarker, SessionFactsMarker, MemoryRetractionMarker}
}

func stripPromptTails(content string) string {
	cut := -1
	for _, marker := range promptTailMarkers() {
		idx := lastLineStartMarker(content, marker)
		if idx >= 0 && (cut < 0 || idx < cut) {
			cut = idx
		}
	}
	if cut < 0 {
		return content
	}
	return strings.TrimRight(content[:cut], "\n")
}

func applyPromptOverlays(conversation []interface{}, state *WorkingState, attachWS bool, facts *SessionFactOverlay, retractions *MemoryRetraction) []interface{} {
	if len(conversation) == 0 {
		return conversation
	}
	role, content, kind := systemPromptContent(conversation[0])
	if role != "system" {
		return conversation
	}
	next := stripPromptTails(content)
	appendSection := func(section string) {
		section = strings.TrimRight(section, "\n")
		if section == "" {
			return
		}
		if strings.TrimSpace(next) == "" {
			next = section
			return
		}
		next = strings.TrimRight(next, "\n") + "\n\n" + section
	}
	if attachWS {
		appendSection(RenderWorkingState(state))
	}
	appendSection(RenderSessionFacts(facts))
	appendSection(RenderMemoryRetraction(retractions))
	conversation[0] = replaceSystemContent(conversation[0], kind, next)
	return conversation
}

// RedactMemoryNeedlesInText replaces warehouse facts that the user deleted or
// rewrote. Short needles are ignored so a tiny fragment cannot wipe unrelated text.
func RedactMemoryNeedlesInText(text string, needles []string) (string, bool) {
	if text == "" || len(needles) == 0 {
		return text, false
	}
	changed := false
	for _, needle := range sortedRetractionNeedles(needles) {
		if !strings.Contains(text, needle) {
			continue
		}
		text = strings.ReplaceAll(text, needle, "【已从记忆仓库删除，勿再当作事实使用】")
		changed = true
	}
	return text, changed
}

func sortedRetractionNeedles(needles []string) []string {
	out := make([]string, 0, len(needles))
	seen := make(map[string]struct{}, len(needles))
	for _, needle := range needles {
		needle = strings.TrimSpace(needle)
		if utf8.RuneCountInString(needle) < memoryRetractionMinNeedle {
			continue
		}
		if _, ok := seen[needle]; ok {
			continue
		}
		seen[needle] = struct{}{}
		out = append(out, needle)
	}
	sort.Slice(out, func(i, j int) bool {
		return utf8.RuneCountInString(out[i]) > utf8.RuneCountInString(out[j])
	})
	return out
}

// RedactMemoryNeedlesInContent walks string and text-part payloads.
func RedactMemoryNeedlesInContent(content interface{}, needles []string) (interface{}, bool) {
	switch c := content.(type) {
	case string:
		return RedactMemoryNeedlesInText(c, needles)
	case []interface{}:
		changed := false
		out := append([]interface{}(nil), c...)
		for i := range out {
			next, ok := RedactMemoryNeedlesInContent(out[i], needles)
			if ok {
				out[i] = next
				changed = true
			}
		}
		if !changed {
			return content, false
		}
		return out, true
	case map[string]interface{}:
		changed := false
		out := make(map[string]interface{}, len(c))
		for k, v := range c {
			if k == "role" || k == "name" || k == "tool_call_id" || k == "tool_calls" {
				out[k] = v
				continue
			}
			next, ok := RedactMemoryNeedlesInContent(v, needles)
			if ok {
				out[k] = next
				changed = true
				continue
			}
			out[k] = v
		}
		if !changed {
			return content, false
		}
		return out, true
	case map[string]string:
		text, hasText := c["text"]
		body, hasBody := c["content"]
		nextText, textChanged := text, false
		nextBody, bodyChanged := body, false
		if hasText {
			nextText, textChanged = RedactMemoryNeedlesInText(text, needles)
		}
		if hasBody {
			nextBody, bodyChanged = RedactMemoryNeedlesInText(body, needles)
		}
		if !textChanged && !bodyChanged {
			return content, false
		}
		out := make(map[string]string, len(c))
		for k, v := range c {
			out[k] = v
		}
		if textChanged {
			out["text"] = nextText
		}
		if bodyChanged {
			out["content"] = nextBody
		}
		return out, true
	default:
		return content, false
	}
}

// RedactConversationMessages strips deleted warehouse bodies from assistant/tool
// messages in a model conversation. System and user turns are left unchanged.
func RedactConversationMessages(conversation []interface{}, needles []string) []interface{} {
	return redactConversationInterface(conversation, needles)
}

func redactConversationInterface(conversation []interface{}, needles []string) []interface{} {
	if len(conversation) == 0 || len(needles) == 0 {
		return conversation
	}
	for i, msg := range conversation {
		role, _, _ := systemPromptContent(msg)
		if role == "system" || role == "user" {
			continue
		}
		next, ok := RedactMemoryNeedlesInContent(msg, needles)
		if ok {
			conversation[i] = next
		}
	}
	return conversation
}

// RedactConversationEntries strips deleted warehouse bodies from assistant/tool
// history. User and system messages are left intact so a user's own wording is
// not rewritten.
func RedactConversationEntries(entries []ConversationEntry, needles []string) ([]ConversationEntry, bool) {
	if len(entries) == 0 || len(needles) == 0 {
		return entries, false
	}
	changed := false
	out := append([]ConversationEntry(nil), entries...)
	for i := range out {
		role := strings.ToLower(strings.TrimSpace(out[i].Role))
		if role == "user" || role == "system" {
			continue
		}
		if next, ok := RedactMemoryNeedlesInContent(out[i].Content, needles); ok {
			out[i].Content = next
			changed = true
		}
		if next, ok := RedactMemoryNeedlesInText(out[i].ReasoningContent, needles); ok {
			out[i].ReasoningContent = next
			changed = true
		}
	}
	if !changed {
		return entries, false
	}
	return out, true
}

// StripMemoryRetractionFromVisible removes a line-start retraction block from
// user-visible text.
func StripMemoryRetractionFromVisible(text string) string {
	if text == "" || !strings.Contains(text, MemoryRetractionMarker) {
		return text
	}
	idx := lastLineStartMarker(text, MemoryRetractionMarker)
	if idx < 0 {
		return text
	}
	return strings.TrimRight(text[:idx], "\n")
}

// DropSessionFactsMatching removes overlay facts whose claim or prior overlaps
// a retracted warehouse body. The entity (IP/host) is not used as a match key.
func DropSessionFactsMatching(overlay *SessionFactOverlay, needles []string) {
	if overlay == nil || len(overlay.Facts) == 0 || len(needles) == 0 {
		return
	}
	kept := make([]SessionFact, 0, len(overlay.Facts))
	for _, fact := range overlay.Facts {
		if sessionFactMatchesNeedles(fact, needles) {
			continue
		}
		kept = append(kept, fact)
	}
	if len(kept) == 0 {
		overlay.Facts = nil
		return
	}
	overlay.Facts = kept
}

func sessionFactMatchesNeedles(fact SessionFact, needles []string) bool {
	// Match claim/prior only. Matching on the entity (an IP) would drop a
	// live overlay update such as "IP 不可达" when the user deletes the old
	// warehouse row "IP 可达".
	parts := []string{fact.Claim, fact.Prior}
	for _, part := range parts {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "" || utf8.RuneCountInString(p) < memoryRetractionMinNeedle {
			continue
		}
		for _, needle := range needles {
			n := strings.ToLower(strings.TrimSpace(needle))
			if utf8.RuneCountInString(n) < memoryRetractionMinNeedle {
				continue
			}
			// Only when the deleted warehouse body contains the overlay claim.
			// The reverse match would drop a more specific live claim that merely
			// shares a prefix with the deleted text.
			if strings.Contains(n, p) {
				return true
			}
		}
	}
	return false
}
