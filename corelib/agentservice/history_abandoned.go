package agentservice

// dropAbandonedUserTurns removes earlier user turns whose run ended cancelled
// or failed without an assistant reply. Such a turn stays in the transcript
// for the person, but the model must not see it as an open request: seen
// live, a weather request whose run a restart cancelled was answered at the
// top of the next, unrelated reply. Each run answers only its own message;
// the cancelled request is shown as cancelled and can be sent again.
func dropAbandonedUserTurns(history []Message, runs []Run, currentID string) []Message {
	if len(history) == 0 || len(runs) == 0 {
		return history
	}
	abandoned := map[string]bool{}
	answered := map[string]bool{}
	for _, r := range runs {
		id := r.UserMessageID
		if id == "" || id == currentID {
			continue
		}
		switch {
		case r.AssistantMessageID != "" || r.Status == RunStatusSucceeded:
			answered[id] = true
		case r.Status == RunStatusCancelled || r.Status == RunStatusFailed:
			abandoned[id] = true
		}
	}
	if len(abandoned) == 0 {
		return history
	}
	out := make([]Message, 0, len(history))
	for _, m := range history {
		if m.Role == "user" && m.ID != currentID && abandoned[m.ID] && !answered[m.ID] {
			continue
		}
		out = append(out, m)
	}
	return out
}
