package agentservice

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// The conversation context lives on the MaClawSrv session. Hub and every GUI
// are readers. A process restart loads the same session, so a GUI opened on
// another computer sees the same history, plan, tasks, and pending asks.
const botContextMetaKey = "bot_context"

// BotContextResumeWindow is how long a cancelled, failed, or interrupted
// task stays resumable.
const BotContextResumeWindow = 30 * time.Minute

// BotContext is one bot conversation.
type BotContext struct {
	Revision        int64               `json:"revision"`
	State           string              `json:"state,omitempty"`
	Plan            BotIntakePlan       `json:"plan"`
	Messages        []BotContextMessage `json:"messages,omitempty"`
	Tasks           []BotContextTask    `json:"tasks,omitempty"`
	Asks            []BotContextAsk     `json:"asks,omitempty"`
	AttentionID     string              `json:"attention_id,omitempty"`
	AttentionReason string              `json:"attention_reason,omitempty"`
}

// BotContextMessage is one turn. ID is stable. At is the server time.
type BotContextMessage struct {
	ID    string    `json:"id"`
	At    time.Time `json:"at"`
	Role  string    `json:"role"`
	Text  string    `json:"text"`
	Kind  string    `json:"kind,omitempty"`
	RunID string    `json:"run_id,omitempty"`
}

// BotContextTask is the current or a recently stopped task.
type BotContextTask struct {
	RunID       string    `json:"run_id,omitempty"`
	Instruction string    `json:"instruction"`
	Progress    string    `json:"progress,omitempty"`
	Status      string    `json:"status"`
	At          time.Time `json:"at"`
}

// BotContextAsk is a question still waiting on a run, or one that was withdrawn.
type BotContextAsk struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id,omitempty"`
	Question  string    `json:"question"`
	Options   []string  `json:"options,omitempty"`
	At        time.Time `json:"at"`
	Withdrawn bool      `json:"withdrawn,omitempty"`
	Because   string    `json:"because,omitempty"`
	Answer    string    `json:"answer,omitempty"`
}

// BotContextSnapshot is what a GUI renders. Messages are ordered by server time.
type BotContextSnapshot struct {
	Revision        int64               `json:"revision"`
	Session         string              `json:"session,omitempty"`
	State           string              `json:"state,omitempty"`
	Plan            BotIntakePlan       `json:"plan"`
	Messages        []BotContextMessage `json:"messages"`
	Tasks           []BotContextTask    `json:"tasks,omitempty"`
	Asks            []BotContextAsk     `json:"asks,omitempty"`
	AttentionID     string              `json:"attention_id,omitempty"`
	AttentionReason string              `json:"attention_reason,omitempty"`
}

func (c BotContext) snapshot(session string) BotContextSnapshot {
	msgs := append([]BotContextMessage(nil), c.Messages...)
	// Stable order: server time, then id.
	for i := 1; i < len(msgs); i++ {
		j := i
		for j > 0 && (msgs[j].At.Before(msgs[j-1].At) || (msgs[j].At.Equal(msgs[j-1].At) && msgs[j].ID < msgs[j-1].ID)) {
			msgs[j], msgs[j-1] = msgs[j-1], msgs[j]
			j--
		}
	}
	var asks []BotContextAsk
	for _, ask := range c.Asks {
		if !ask.Withdrawn {
			asks = append(asks, ask)
		}
	}
	state := c.State
	if state == "" {
		state = "discuss"
	}
	return BotContextSnapshot{
		Revision: c.Revision, Session: session, State: state, Plan: c.Plan,
		Messages: msgs, Tasks: append([]BotContextTask(nil), c.Tasks...), Asks: asks,
		AttentionID: c.AttentionID, AttentionReason: c.AttentionReason,
	}
}

func (c BotContext) earlierUserLines() []string {
	var out []string
	for _, msg := range c.Messages {
		if msg.Role != "user" {
			continue
		}
		if line := strings.TrimSpace(msg.Text); line != "" {
			out = append(out, line)
		}
	}
	if len(out) > 8 {
		out = out[len(out)-8:]
	}
	return out
}

func (c BotContext) resumableTasks(now time.Time) []BotIntakeRun {
	var out []BotIntakeRun
	for _, task := range c.Tasks {
		if !contextTaskResumable(task.Status) {
			continue
		}
		if !task.At.IsZero() && now.Sub(task.At) > BotContextResumeWindow {
			continue
		}
		out = append(out, BotIntakeRun{
			RunID: task.RunID, Task: task.Instruction, Status: task.Status, Progress: task.Progress,
		})
	}
	return out
}

func contextTaskResumable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "cancelled", "canceled", "failed", "interrupted":
		return true
	}
	return false
}

func readBotContext(meta map[string]string) BotContext {
	var ctx BotContext
	if meta == nil {
		return ctx
	}
	raw := strings.TrimSpace(meta[botContextMetaKey])
	if raw == "" {
		return ctx
	}
	_ = json.Unmarshal([]byte(raw), &ctx)
	return ctx
}

func (s *Service) sessionForBot(ctx context.Context, p Principal, instanceID, sessionKey string) (*Session, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, ErrBotThreadsBadBot
	}
	if sess, ok := s.findSessionByClientKey(p, instanceID, sessionKey); ok {
		return &sess, nil
	}
	return s.resolveSendSession(ctx, p, instanceID, SendMessageInput{ClientSessionKey: sessionKey})
}

func (s *Service) writeBotContext(ctx context.Context, p Principal, instanceID string, sess *Session, state BotContext) error {
	state.Revision++
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	meta := cloneMap(sess.Metadata)
	if meta == nil {
		meta = map[string]string{}
	}
	meta[botContextMetaKey] = string(raw)
	updated, err := s.UpdateSession(ctx, p, instanceID, sess.ID, UpdateSessionInput{Metadata: meta})
	if err != nil {
		return err
	}
	if updated != nil {
		*sess = *updated
	} else {
		sess.Metadata = meta
	}
	return nil
}

func (s *Service) loadBotContext(ctx context.Context, p Principal, instanceID, sessionKey string) (*Session, BotContext, error) {
	sess, err := s.sessionForBot(ctx, p, instanceID, sessionKey)
	if err != nil {
		return nil, BotContext{}, err
	}
	return sess, readBotContext(sess.Metadata), nil
}

// BeginBotTurn records the person's line, withdraws asks that this new
// message replaces, and returns the earlier lines and resumable tasks the
// decision should see. The line just written is not part of Earlier.
func (s *Service) BeginBotTurn(ctx context.Context, p Principal, instanceID, sessionKey, text, clientMessageID string) ([]string, []BotIntakeRun, error) {
	sess, state, err := s.loadBotContext(ctx, p, instanceID, sessionKey)
	if err != nil {
		return nil, nil, err
	}
	earlier := state.earlierUserLines()
	tasks := state.resumableTasks(s.now())
	text = strings.TrimSpace(text)
	if text == "" {
		return earlier, tasks, nil
	}
	if clientMessageID != "" {
		for _, msg := range state.Messages {
			if msg.ID == clientMessageID || (msg.Role == "user" && msg.Text == text && msg.ID == clientMessageID) {
				return earlier, tasks, nil
			}
		}
	}
	for i := range state.Asks {
		if !state.Asks[i].Withdrawn {
			state.Asks[i].Withdrawn = true
			state.Asks[i].Because = "newer_message"
		}
	}
	id := strings.TrimSpace(clientMessageID)
	if id == "" {
		id = NewID("msg")
	}
	state.Messages = append(state.Messages, BotContextMessage{
		ID: id, At: s.now(), Role: "user", Text: text, Kind: "user",
	})
	if err := s.writeBotContext(ctx, p, instanceID, sess, state); err != nil {
		return nil, nil, err
	}
	return earlier, tasks, nil
}

// FinishBotTurn records the reply and, when a run is actually starting,
// the task. A reply that says there is nothing to do does not record a task.
func (s *Service) FinishBotTurn(ctx context.Context, p Principal, instanceID, sessionKey, ack, kind, instruction, runID string) error {
	sess, state, err := s.loadBotContext(ctx, p, instanceID, sessionKey)
	if err != nil {
		return err
	}
	if text := strings.TrimSpace(ack); text != "" {
		state.Messages = append(state.Messages, BotContextMessage{
			ID: NewID("msg"), At: s.now(), Role: "assistant", Text: text, Kind: kind, RunID: runID,
		})
	}
	if kind == "new" && strings.TrimSpace(instruction) != "" && !intakeSaysNoWork(ack) {
		state.State = "executing"
		state.Tasks = append(state.Tasks, BotContextTask{
			RunID: strings.TrimSpace(runID), Instruction: strings.TrimSpace(instruction), Status: "running", At: s.now(),
		})
	}
	return s.writeBotContext(ctx, p, instanceID, sess, state)
}

// NoteBotTask records or updates a task on the conversation.
func (s *Service) NoteBotTask(ctx context.Context, p Principal, instanceID, sessionKey string, task BotContextTask) error {
	sess, state, err := s.loadBotContext(ctx, p, instanceID, sessionKey)
	if err != nil {
		return err
	}
	task.Instruction = strings.TrimSpace(task.Instruction)
	task.Status = strings.ToLower(strings.TrimSpace(task.Status))
	if task.At.IsZero() {
		task.At = s.now()
	}
	replaced := false
	if task.RunID != "" {
		for i := range state.Tasks {
			if state.Tasks[i].RunID == task.RunID {
				if task.Instruction == "" {
					task.Instruction = state.Tasks[i].Instruction
				}
				state.Tasks[i] = task
				replaced = true
				break
			}
		}
	}
	if !replaced && task.Instruction != "" {
		state.Tasks = append(state.Tasks, task)
	}
	if contextTaskResumable(task.Status) {
		for i := range state.Asks {
			if state.Asks[i].RunID == task.RunID && task.RunID != "" && !state.Asks[i].Withdrawn {
				state.Asks[i].Withdrawn = true
				state.Asks[i].Because = "run_ended"
			}
		}
	}
	return s.writeBotContext(ctx, p, instanceID, sess, state)
}

// NoteBotAsk records a question bound to a run.
func (s *Service) NoteBotAsk(ctx context.Context, p Principal, instanceID, sessionKey string, ask BotContextAsk) error {
	sess, state, err := s.loadBotContext(ctx, p, instanceID, sessionKey)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ask.ID) == "" {
		ask.ID = NewID("ask")
	}
	if ask.At.IsZero() {
		ask.At = s.now()
	}
	state.Asks = append(state.Asks, ask)
	return s.writeBotContext(ctx, p, instanceID, sess, state)
}

// NoteBotAttention stores the handoff notice for this bot's conversation.
func (s *Service) NoteBotAttention(ctx context.Context, p Principal, instanceID, sessionKey, id, reason string) error {
	sess, state, err := s.loadBotContext(ctx, p, instanceID, sessionKey)
	if err != nil {
		return err
	}
	state.AttentionID = strings.TrimSpace(id)
	state.AttentionReason = strings.TrimSpace(reason)
	return s.writeBotContext(ctx, p, instanceID, sess, state)
}

// ReadBotContext is the snapshot a GUI renders.
func (s *Service) ReadBotContext(ctx context.Context, p Principal, instanceID, sessionKey string) (BotContextSnapshot, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return BotContextSnapshot{State: "discuss"}, nil
	}
	sess, ok := s.findSessionByClientKey(p, instanceID, sessionKey)
	if !ok {
		return BotContextSnapshot{State: "discuss", Session: sessionKey}, nil
	}
	return readBotContext(sess.Metadata).snapshot(sessionKey), nil
}
