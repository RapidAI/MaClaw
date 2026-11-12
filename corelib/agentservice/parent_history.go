package agentservice

import (
	"strings"
	"time"
	"unicode/utf8"
)

// parentHistoryMaxMessages is how many recent parent-conversation messages
// a task thread may see. Enough to continue coherently; not the whole main
// history, and never a sibling run that started after this thread.
const parentHistoryMaxMessages = 16

// parentHistoryMaxRunes caps the copied parent text so a long main
// conversation cannot flood the thread's prompt.
const parentHistoryMaxRunes = 8000

// parentClientSessionKey is the bot's main conversation for a Hub task
// thread ("bot#t-…", "bot#xdel-…", "bot#xwake-…"). Other client keys are
// left alone, including those that happen to contain a colon or hash.
func parentClientSessionKey(key string) string {
	key = strings.TrimSpace(key)
	i := strings.Index(key, "#")
	if i <= 0 {
		return ""
	}
	rest := key[i+1:]
	if strings.HasPrefix(rest, "t-") || strings.HasPrefix(rest, "xdel-") || strings.HasPrefix(rest, "xwake-") {
		return key[:i]
	}
	return ""
}

// withParentSessionHistory prepends a bounded recent slice of the parent
// (main) conversation to a thread's history for this turn only. The parent
// messages are not written into the thread, so ListMessages of the thread
// stays just that run, and a sibling still going in main does not appear
// (only parent messages from before this thread started are included).
func (s *Service) withParentSessionHistory(p Principal, instanceID string, sess Session, history []Message) []Message {
	if s == nil || s.store == nil {
		return history
	}
	key := ""
	if sess.Metadata != nil {
		key = strings.TrimSpace(sess.Metadata["client_session_key"])
	}
	parentKey := parentClientSessionKey(key)
	if parentKey == "" {
		return history
	}
	parent, ok := s.findSessionByClientKey(p, instanceID, parentKey)
	if !ok {
		return history
	}
	parentMsgs, err := s.store.ListMessages(parent.ID)
	if err != nil || len(parentMsgs) == 0 {
		return history
	}
	cutoff := threadHistoryStart(history)
	parentMsgs = messagesBefore(parentMsgs, cutoff)
	parentMsgs = boundRecentConversation(parentMsgs, parentHistoryMaxMessages, parentHistoryMaxRunes)
	if len(parentMsgs) == 0 {
		return history
	}
	out := make([]Message, 0, len(parentMsgs)+len(history))
	for _, msg := range parentMsgs {
		msg.Metadata = cloneMap(msg.Metadata)
		if msg.Metadata == nil {
			msg.Metadata = map[string]string{}
		}
		msg.Metadata["parent_context"] = "1"
		out = append(out, msg)
	}
	return append(out, history...)
}

// threadHistoryStart is when this thread's own history began. Parent
// messages at or after this instant are treated as later main work
// (including a sibling run in main) and are not copied in.
func threadHistoryStart(history []Message) time.Time {
	var start time.Time
	for _, msg := range history {
		if msg.Metadata != nil && msg.Metadata["parent_context"] == "1" {
			continue
		}
		if msg.CreatedAt.IsZero() {
			continue
		}
		if start.IsZero() || msg.CreatedAt.Before(start) {
			start = msg.CreatedAt
		}
	}
	if start.IsZero() {
		return time.Now()
	}
	return start
}

func messagesBefore(items []Message, cutoff time.Time) []Message {
	if cutoff.IsZero() || len(items) == 0 {
		return items
	}
	out := make([]Message, 0, len(items))
	for _, msg := range items {
		if msg.CreatedAt.IsZero() || msg.CreatedAt.Before(cutoff) {
			out = append(out, msg)
		}
	}
	return out
}

// boundRecentConversation keeps the most recent parent messages, preferring
// complete user/assistant turns, under a count and rune cap.
func boundRecentConversation(items []Message, maxMessages, maxRunes int) []Message {
	if maxMessages <= 0 {
		maxMessages = parentHistoryMaxMessages
	}
	if maxRunes <= 0 {
		maxRunes = parentHistoryMaxRunes
	}
	filtered := make([]Message, 0, len(items))
	for _, msg := range items {
		if msg.Role == MessageRoleSystem {
			continue
		}
		filtered = append(filtered, msg)
	}
	if len(filtered) == 0 {
		return nil
	}
	start := 0
	if len(filtered) > maxMessages {
		start = len(filtered) - maxMessages
		// If we landed on an assistant turn, include the user message
		// that started it, when it is the immediately previous item.
		if start > 0 && filtered[start].Role == MessageRoleAssistant && filtered[start-1].Role == MessageRoleUser {
			start--
		}
	}
	sliced := filtered[start:]
	for len(sliced) > 0 {
		if conversationRunes(sliced) <= maxRunes {
			break
		}
		if len(sliced) <= 2 {
			sliced[len(sliced)-1].Content = clipRunes(sliced[len(sliced)-1].Content, maxRunes)
			if conversationRunes(sliced) <= maxRunes {
				break
			}
			sliced = sliced[1:]
			continue
		}
		// Drop the oldest turn (a leading user+assistant pair when present).
		drop := 1
		if sliced[0].Role == MessageRoleUser && sliced[1].Role == MessageRoleAssistant {
			drop = 2
		}
		sliced = sliced[drop:]
	}
	if len(sliced) == 0 {
		return nil
	}
	out := make([]Message, len(sliced))
	copy(out, sliced)
	return out
}

func conversationRunes(items []Message) int {
	n := 0
	for _, msg := range items {
		n += utf8.RuneCountInString(msg.Content)
	}
	return n
}

func clipRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}
