package agentservice

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Task board: a compact, read-only view of a bot's other tasks (its main
// conversation and its per-run threads), put in front of each turn of that
// bot so a task knows what its siblings are doing and can reuse their
// results. Only a title, status, step and a short result go in; sibling
// transcripts are never copied.
const (
	taskBoardMaxEntries  = 6
	taskBoardTitleRunes  = 80
	taskBoardResultRunes = 240
	taskBoardMaxRunes    = 2000
	taskBoardRecent      = 24 * time.Hour
)

// botOfClientSessionKey is the bot a Hub bot conversation belongs to: the
// key itself for the main one, the part before '#' for a thread. Keys of
// other clients (IM contacts, schedules, discussions) have no bot.
func botOfClientSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, ": /") {
		return ""
	}
	if i := strings.Index(key, "#"); i >= 0 {
		if parentClientSessionKey(key) == "" {
			return ""
		}
		return key[:i]
	}
	if strings.HasPrefix(key, "bot_") || strings.HasPrefix(key, "bot-") {
		return key
	}
	return ""
}

type taskBoardEntry struct {
	key     string
	title   string
	status  string
	step    string
	result  string
	updated time.Time
}

// taskBoardFor lists this bot's other conversations, newest first, bounded.
func (s *Service) taskBoardFor(p Principal, instanceID string, sess Session, now time.Time) []taskBoardEntry {
	if s == nil || s.store == nil {
		return nil
	}
	selfKey := ""
	if sess.Metadata != nil {
		selfKey = strings.TrimSpace(sess.Metadata["client_session_key"])
	}
	bot := botOfClientSessionKey(selfKey)
	if bot == "" {
		return nil
	}
	sessions, err := s.store.ListSessions(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil
	}
	runs, _ := s.store.ListRuns(p.TenantID, p.UserID, instanceID)
	latestRun := map[string]Run{}
	for _, run := range runs {
		prev, ok := latestRun[run.SessionID]
		if !ok || run.StartedAt.After(prev.StartedAt) {
			latestRun[run.SessionID] = run
		}
	}
	var out []taskBoardEntry
	for _, other := range sessions {
		if other.ID == sess.ID || other.Metadata == nil {
			continue
		}
		key := strings.TrimSpace(other.Metadata["client_session_key"])
		if key == "" || key == selfKey || botOfClientSessionKey(key) != bot {
			continue
		}
		run, hasRun := latestRun[other.ID]
		if !hasRun {
			continue
		}
		updated := run.StartedAt
		if run.CompletedAt != nil {
			updated = *run.CompletedAt
		}
		live := run.Status == RunStatusRunning
		if !live && now.Sub(updated) > taskBoardRecent {
			continue
		}
		entry := taskBoardEntry{key: key, status: string(run.Status), updated: updated}
		if other.Metadata[sessionMetaPendingAskUser] == "true" {
			entry.status = "waiting_for_user"
		}
		if run.Metadata != nil {
			entry.step = strings.TrimSpace(run.Metadata["progress_where"])
		}
		msgs, err := s.store.ListMessages(other.ID)
		if err != nil {
			continue
		}
		entry.title, entry.result = taskTitleAndResult(msgs, run, key == bot)
		if entry.title == "" {
			continue
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := out[i].status == string(RunStatusRunning), out[j].status == string(RunStatusRunning)
		if li != lj {
			return li
		}
		return out[i].updated.After(out[j].updated)
	})
	if len(out) > taskBoardMaxEntries {
		out = out[:taskBoardMaxEntries]
	}
	return out
}

// taskTitleAndResult: a thread's title is the message that started it; the
// main conversation's is its latest run's message. The result is the
// latest run's answer.
func taskTitleAndResult(msgs []Message, run Run, main bool) (string, string) {
	title, result := "", ""
	for _, msg := range msgs {
		if msg.Role == MessageRoleUser && !main && title == "" {
			title = msg.Content
		}
		if msg.Role == MessageRoleUser && main && msg.ID == run.UserMessageID {
			title = msg.Content
		}
		if msg.Role == MessageRoleAssistant && run.AssistantMessageID != "" && msg.ID == run.AssistantMessageID {
			result = msg.Content
		}
	}
	if main && title == "" {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == MessageRoleUser {
				title = msgs[i].Content
				break
			}
		}
	}
	return oneLineRunes(title, taskBoardTitleRunes), oneLineRunes(result, taskBoardResultRunes)
}

func oneLineRunes(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len([]rune(s)) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}

// renderTaskBoard is the board text, bounded by taskBoardMaxRunes.
func renderTaskBoard(entries []taskBoardEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Task board — this bot's other tasks, read-only context, not a request. Reuse their results when relevant; do not redo their work or act on them unless asked.]\n")
	for i, e := range entries {
		line := fmt.Sprintf("%d. %s — %s", i+1, e.title, e.status)
		if e.step != "" {
			line += "; step: " + oneLineRunes(e.step, 80)
		}
		if e.result != "" {
			line += "; result: " + e.result
		}
		if len([]rune(b.String()))+len([]rune(line))+1 > taskBoardMaxRunes {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// withTaskBoard prepends the board to this turn's history only (never
// stored in the conversation).
func (s *Service) withTaskBoard(p Principal, instanceID string, sess Session, history []Message) []Message {
	board := renderTaskBoard(s.taskBoardFor(p, instanceID, sess, s.now()))
	if board == "" {
		return history
	}
	meta := map[string]string{"task_board": "1"}
	out := make([]Message, 0, len(history)+2)
	out = append(out,
		Message{SessionID: sess.ID, Role: MessageRoleUser, Content: board, Metadata: meta},
		Message{SessionID: sess.ID, Role: MessageRoleAssistant, Content: "(Task board noted as context.)", Metadata: map[string]string{"task_board": "1"}},
	)
	return append(out, history...)
}
