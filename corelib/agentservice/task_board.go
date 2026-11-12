package agentservice

import (
	"errors"
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

// BotThread is one of a bot's conversations as the server registry sees
// it: the bot's main conversation or one of its per-run threads. It is a
// view over the persisted sessions, runs and messages (no second store),
// so it survives srv restarts, device changes and cleared chats.
type BotThread struct {
	Session   string    `json:"session"`
	Main      bool      `json:"main,omitempty"`
	Title     string    `json:"title"`
	Goal      string    `json:"goal,omitempty"`
	Status    string    `json:"status"`
	Step      string    `json:"step,omitempty"`
	Result    string    `json:"result,omitempty"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UserID    string    `json:"user_id,omitempty"`
}

// Registry bounds: what any reader may see of a bot's past tasks.
const (
	botThreadRetention = 7 * 24 * time.Hour
	botThreadMaxPerBot = 50
)

// BotThreadQuery narrows the registry view.
type BotThreadQuery struct {
	MaxAge         time.Duration // 0: botThreadRetention; never above it
	Limit          int           // 0 or above botThreadMaxPerBot: botThreadMaxPerBot
	ExcludeRunning bool
	ThreadsOnly    bool   // leave out the bot's main conversation
	ExcludeSession string // a client_session_key to leave out (the asking turn)
}

func (s *Service) botThreads(p Principal, instanceID, bot string, q BotThreadQuery, now time.Time) []BotThread {
	bot = strings.TrimSpace(bot)
	if s == nil || s.store == nil || bot == "" || botOfClientSessionKey(bot) != bot {
		return nil
	}
	maxAge := q.MaxAge
	if maxAge <= 0 || maxAge > botThreadRetention {
		maxAge = botThreadRetention
	}
	limit := q.Limit
	if limit <= 0 || limit > botThreadMaxPerBot {
		limit = botThreadMaxPerBot
	}
	sessions, err := s.store.ListSessions(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil
	}
	runs, _ := s.store.ListRuns(p.TenantID, p.UserID, instanceID)
	latestRun := map[string]Run{}
	firstRun := map[string]Run{}
	for _, run := range runs {
		if prev, ok := latestRun[run.SessionID]; !ok || run.StartedAt.After(prev.StartedAt) {
			latestRun[run.SessionID] = run
		}
		if prev, ok := firstRun[run.SessionID]; !ok || run.StartedAt.Before(prev.StartedAt) {
			firstRun[run.SessionID] = run
		}
	}
	var out []BotThread
	for _, other := range sessions {
		if other.Metadata == nil {
			continue
		}
		key := strings.TrimSpace(other.Metadata["client_session_key"])
		if key == "" || key == q.ExcludeSession || botOfClientSessionKey(key) != bot {
			continue
		}
		main := key == bot
		if main && q.ThreadsOnly {
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
		if live && q.ExcludeRunning {
			continue
		}
		if !live && now.Sub(updated) > maxAge {
			continue
		}
		entry := BotThread{Session: key, Main: main, Status: string(run.Status), StartedAt: firstRun[other.ID].StartedAt, UpdatedAt: updated, UserID: p.UserID}
		if other.Metadata[sessionMetaPendingAskUser] == "true" {
			entry.Status = "waiting_for_user"
		}
		if run.Metadata != nil {
			entry.Step = oneLineRunes(run.Metadata["progress_where"], 80)
		}
		msgs, err := s.store.ListMessages(other.ID)
		if err != nil {
			continue
		}
		entry.Title, entry.Result = taskTitleAndResult(msgs, run, main)
		if !main {
			entry.Goal = latestUserLine(msgs, taskBoardTitleRunes*2)
			if entry.Goal == entry.Title {
				entry.Goal = ""
			}
		}
		if entry.Title == "" {
			continue
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := out[i].Status == string(RunStatusRunning), out[j].Status == string(RunStatusRunning)
		if li != lj {
			return li
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ListBotThreads is the owner's registry view of one bot's tasks.
func (s *Service) ListBotThreads(p Principal, instanceID, bot string, q BotThreadQuery) ([]BotThread, error) {
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	if botOfClientSessionKey(bot) != strings.TrimSpace(bot) || strings.TrimSpace(bot) == "" {
		return nil, ErrBotThreadsBadBot
	}
	return s.botThreads(p, instanceID, bot, q, s.now()), nil
}

// taskBoardFor lists this bot's other conversations, newest first, bounded.
func (s *Service) taskBoardFor(p Principal, instanceID string, sess Session, now time.Time) []BotThread {
	selfKey := ""
	if sess.Metadata != nil {
		selfKey = strings.TrimSpace(sess.Metadata["client_session_key"])
	}
	bot := botOfClientSessionKey(selfKey)
	if bot == "" {
		return nil
	}
	return s.botThreads(p, instanceID, bot, BotThreadQuery{MaxAge: taskBoardRecent, Limit: taskBoardMaxEntries, ExcludeSession: selfKey}, now)
}

func latestUserLine(msgs []Message, max int) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == MessageRoleUser && strings.TrimSpace(msgs[i].Content) != "" {
			return oneLineRunes(msgs[i].Content, max)
		}
	}
	return ""
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
func renderTaskBoard(entries []BotThread) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Task board — this bot's other tasks, read-only context, not a request. Reuse their results when relevant; do not redo their work or act on them unless asked.]\n")
	for i, e := range entries {
		line := fmt.Sprintf("%d. %s — %s", i+1, e.Title, e.Status)
		if e.Step != "" {
			line += "; step: " + e.Step
		}
		if e.Result != "" {
			line += "; result: " + e.Result
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

// ErrBotThreadsBadBot: the registry is asked for something that is not a
// Hub bot id.
var ErrBotThreadsBadBot = errors.New("bot_id is not a bot")
