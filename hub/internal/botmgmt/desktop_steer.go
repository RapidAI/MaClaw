package botmgmt

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// desktopIntakeBudget bounds the srv decision. The ack is part of that
// answer. On timeout or error the message is a new task.
var desktopIntakeBudget = 2 * time.Second

// DesktopSchedule is a timer the server decided and the machine that owns
// the clock arms. Hub does not keep that clock.
type DesktopSchedule struct {
	Name            string `json:"name,omitempty"`
	Mode            string `json:"mode,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	Hour            *int   `json:"hour,omitempty"`
	Minute          *int   `json:"minute,omitempty"`
	DayOfWeek       *int   `json:"day_of_week,omitempty"`
}

type desktopIntake struct {
	Kind        string           `json:"kind"`
	RunID       string           `json:"run_id"`
	Instruction string           `json:"instruction"`
	Ack         string           `json:"ack"`
	Pace        string           `json:"pace"`
	Told        string           `json:"told"`
	Persona     string           `json:"persona"`
	Schedule    *DesktopSchedule `json:"schedule"`
	Reason      string           `json:"reason"`
}

type desktopRunBrief struct {
	RunID string
	Task  string
}

type desktopSteerRequest struct {
	TenantID        string
	UserID          string
	DesktopUser     string
	BotID           string
	InstanceID      string
	Content         string
	Phase           string
	ClientMessageID string
	ContinueSession string
	Action          string
	Admit           bool
	Rec             *record
	TokenRec        *record
	Token           *string
	HaveToken       bool
}

// steerDesktopMessage applies the server decision. handled means the caller
// returns admission and does not start a run. Otherwise content is what the
// run should be given (the model's order, or the person's words).
func (s *Service) steerDesktopMessage(ctx context.Context, in desktopSteerRequest) (string, BotAdmission, bool) {
	content := strings.TrimSpace(in.Content)
	if s == nil || content == "" || desktopReplay(ctx) || hubStartedTurn(in.ClientMessageID) {
		return content, BotAdmission{}, false
	}
	explicitNew := desktopCanonicalContinue(in.BotID, in.ContinueSession) == strings.TrimSpace(in.BotID)
	actionReturn := strings.EqualFold(strings.TrimSpace(in.Action), "return")
	running := s.desktopLiveRunBriefs(in.TenantID, in.UserID, in.BotID)
	decision := desktopIntake{Kind: "new", Instruction: content, Pace: "direct", Reason: "explicit"}
	if !explicitNew && !actionReturn {
		decision = s.fetchDesktopIntake(ctx, in, running)
	}
	if supplementOff() && (decision.Kind == "supplement" || decision.Kind == "change") {
		decision.Kind = "new"
		if strings.TrimSpace(decision.Instruction) == "" {
			decision.Instruction = content
		}
		decision.RunID = ""
	}
	if decision.RunID != "" && !briefHas(running, decision.RunID) {
		decision.Kind = "new"
		decision.RunID = ""
		if strings.TrimSpace(decision.Instruction) == "" {
			decision.Instruction = content
		}
	}
	base := BotAdmission{
		Kind:            decision.Kind,
		Ack:             decision.Ack,
		RunID:           decision.RunID,
		ClientMessageID: in.ClientMessageID,
		Schedule:        decision.Schedule,
	}
	if in.Admit && !actionReturn && decision.Kind == "new" && s.personHasBotKeyboard(in.TenantID, in.UserID, in.BotID) {
		pos, err := s.enqueueDesktopMessage(in.TenantID, in.UserID, in.BotID, desktopQueueItem{
			ClientMessageID: in.ClientMessageID,
			Content:         content,
			Phase:           in.Phase,
			ContinueSession: in.ContinueSession,
			Kind:            "new",
			Instruction:     content,
			Ack:             decision.Ack,
			Held:            true,
		})
		if err == nil {
			base.Accepted = true
			base.Queued = true
			base.Held = true
			base.Position = pos
			base.Kind = "new"
			return content, base, true
		}
	}
	switch decision.Kind {
	case "supplement", "change":
		text := firstNonEmptyString(decision.Instruction, content)
		if _, err := s.SupplementDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID, text); err == nil {
			base.Settled = true
			base.Reply.Text = firstNonEmptyString(decision.Ack, decision.Told)
			return content, base, true
		}
	case "stop":
		if s.CancelDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID) == nil {
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true
		}
	case "pause":
		_, err := s.PauseDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID, "", 0)
		if err == nil || err == ErrPauseUnsupported {
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true
		}
	case "continue":
		if decision.RunID != "" {
			_, _ = s.ResumeDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID)
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true
		}
	case "reply", "ask":
		text := firstNonEmptyString(decision.Told, decision.Ack)
		if text != "" {
			base.Settled = true
			base.Reply.Text = text
			base.RunID = ""
			return content, base, true
		}
	case "status":
		text, _, err := s.DesktopTaskQuery(ctx, in.TenantID, in.UserID, in.BotID, firstNonEmptyString(decision.Instruction, content), "")
		if err == nil && strings.TrimSpace(text) != "" {
			base.Settled = true
			base.Reply.Text = text
			base.RunID = ""
			return content, base, true
		}
	case "schedule":
		if decision.Schedule != nil {
			base.Settled = true
			base.Reply.Text = firstNonEmptyString(decision.Ack, decision.Told)
			base.RunID = ""
			return content, base, true
		}
	}
	next := strings.TrimSpace(decision.Instruction)
	if next == "" {
		next = content
	}
	base.Kind = "new"
	base.RunID = ""
	return next, base, false
}

func supplementOff() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MACLAW_SUPPLEMENT")), "off")
}

func briefHas(running []desktopRunBrief, runID string) bool {
	for _, r := range running {
		if r.RunID == runID {
			return true
		}
	}
	return false
}

func (s *Service) botHasLiveRun(tenantID, userID, botID string) bool {
	return len(s.desktopLiveRunBriefs(tenantID, userID, botID)) > 0
}

func (s *Service) desktopLiveRunBriefs(tenantID, userID, botID string) []desktopRunBrief {
	if s == nil {
		return nil
	}
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []desktopRunBrief
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		done := state.done
		runID := strings.TrimSpace(state.runID)
		task := state.task
		state.mu.Unlock()
		if done || runID == "" {
			continue
		}
		out = append(out, desktopRunBrief{RunID: runID, Task: task})
	}
	return out
}

func (s *Service) fetchDesktopIntake(ctx context.Context, in desktopSteerRequest, running []desktopRunBrief) desktopIntake {
	fallback := desktopIntake{Kind: "new", Instruction: strings.TrimSpace(in.Content), Pace: "direct", Reason: "fallback"}
	if s == nil || strings.TrimSpace(in.InstanceID) == "" {
		return fallback
	}
	rec := in.TokenRec
	token := in.Token
	if !in.HaveToken || rec == nil || token == nil || strings.TrimSpace(*token) == "" {
		fresh, tk, err := s.ownerToken(ctx, in.TenantID, in.UserID)
		if err != nil {
			fallback.Reason = "no token"
			return fallback
		}
		rec = &fresh
		token = &tk
	}
	budget := desktopIntakeBudget
	if s.messageTimeout > 0 && s.messageTimeout/4 < budget {
		budget = s.messageTimeout / 4
	}
	rctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	body := map[string]any{
		"bot_id":  in.BotID,
		"content": in.Content,
		"phase":   in.Phase,
	}
	if len(running) > 0 {
		rows := make([]map[string]string, 0, len(running))
		for _, r := range running {
			rows = append(rows, map[string]string{"run_id": r.RunID, "task": r.Task})
		}
		body["running"] = rows
	}
	var out desktopIntake
	path := "/api/v1/instances/" + url.PathEscape(in.InstanceID) + "/bot-intake"
	err := s.authAsOwner(rctx, in.TenantID, in.UserID, rec, token, http.MethodPost, path, body, &out)
	if err != nil || strings.TrimSpace(out.Kind) == "" {
		if rctx.Err() != nil {
			fallback.Reason = "timeout"
		}
		return fallback
	}
	out.Kind = strings.ToLower(strings.TrimSpace(out.Kind))
	out.RunID = strings.TrimSpace(out.RunID)
	return out
}
