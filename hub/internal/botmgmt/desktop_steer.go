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
// answer. On timeout or error the message stays a discussion.
var desktopIntakeBudget = 2 * time.Second

// DesktopSchedule is a timer MaClawSrv decided and armed on its own clock
// during bot-intake. Hub returns it so the GUI can show the ack. Hub does
// not keep a second clock, and the GUI must not start the turn.
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
	Phase       string           `json:"phase"`
	Plan        *DesktopPlan     `json:"plan"`
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
	Button          bool
	Admit           bool
	Rec             *record
	TokenRec        *record
	Token           *string
	HaveToken       bool
}

// steerDesktopMessage applies the server decision. handled means the caller
// returns admission and does not start a run. Otherwise content is what the
// run should be given (the model's order, or the person's words).
func (s *Service) steerDesktopMessage(ctx context.Context, in desktopSteerRequest) (string, BotAdmission, bool, error) {
	content := strings.TrimSpace(in.Content)
	if s == nil || content == "" || desktopReplay(ctx) || hubStartedTurn(in.ClientMessageID) {
		return content, BotAdmission{}, false, nil
	}
	explicitNew := desktopCanonicalContinue(in.BotID, in.ContinueSession) == strings.TrimSpace(in.BotID)
	actionReturn := strings.EqualFold(strings.TrimSpace(in.Action), "return")
	actionStart := strings.EqualFold(strings.TrimSpace(in.Action), "start")
	running := s.desktopLiveRunBriefs(in.TenantID, in.UserID, in.BotID)
	session := desktopCanonicalContinue(in.BotID, in.ContinueSession)
	if session == "" {
		session = strings.TrimSpace(in.BotID)
	}
	if actionStart {
		return s.startDesktopDiscussion(ctx, in, session)
	}
	// A named new conversation is not a supplement of the previous one, and
	// it is not a run. The words join that conversation's discussion.
	decision := desktopIntake{Kind: "discuss", Pace: "discuss", Reason: "new matter"}
	if actionReturn {
		decision = desktopIntake{Kind: "new", Instruction: content, Pace: "direct", Phase: "execute", Reason: "return"}
	} else if !explicitNew {
		decision = s.fetchDesktopIntake(ctx, in, running, session)
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
	if decision.Kind == "new" && strings.TrimSpace(decision.Phase) != "plan" && !in.Button && !actionReturn {
		decision.Kind = "discuss"
		decision.Pace = "discuss"
		if decision.Plan == nil && strings.TrimSpace(decision.Instruction) != "" {
			goal := strings.TrimSpace(decision.Instruction)
			decision.Plan = &DesktopPlan{Goal: goal}
		}
		decision.Instruction = ""
		decision.Phase = ""
	}
	if in.Button && !desktopIntakeFailed(decision.Reason) && (decision.Kind == "discuss" || decision.Kind == "plan_amend") {
		decision.Kind = "new"
		decision.Phase = "execute"
		if strings.TrimSpace(decision.Instruction) == "" {
			decision.Instruction = content
		}
	}
	// A caller that already named plan or execute is the existing run
	// control. A classifier failure there still carries those words.
	// A message with no phase stays a discussion: failure must not start work.
	if desktopIntakeFailed(decision.Reason) && strings.TrimSpace(in.Phase) != "" {
		decision.Kind = "new"
		decision.Phase = "execute"
		decision.Pace = "direct"
		if strings.TrimSpace(decision.Instruction) == "" {
			decision.Instruction = content
		}
	}
	if strings.TrimSpace(decision.Phase) != "plan" && decision.Kind == "new" {
		decision.Phase = "execute"
	}
	base := BotAdmission{
		Kind:            decision.Kind,
		Ack:             decision.Ack,
		Phase:           decision.Phase,
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
			Secrets:         desktopSecretBag(ctx),
		})
		if err == nil {
			base.Accepted = true
			base.Queued = true
			base.Held = true
			base.Position = pos
			base.Kind = "new"
			return content, base, true, nil
		}
	}
	switch decision.Kind {
	case "supplement", "change":
		text := firstNonEmptyString(decision.Instruction, content)
		if _, err := s.SupplementDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID, text); err == nil {
			base.Settled = true
			base.Reply.Text = firstNonEmptyString(decision.Ack, decision.Told)
			return content, base, true, nil
		}
	case "stop":
		if s.CancelDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID) == nil {
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true, nil
		}
	case "pause":
		_, err := s.PauseDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID, "", 0)
		if err == nil || err == ErrPauseUnsupported {
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true, nil
		}
	case "continue":
		if decision.RunID != "" {
			_, _ = s.ResumeDesktopRun(ctx, in.TenantID, in.UserID, in.BotID, decision.RunID)
			base.Settled = true
			base.Reply.Text = decision.Ack
			return content, base, true, nil
		}
	case "discuss", "plan_amend":
		view, err := s.recordDesktopDiscussion(ctx, in, session, content, firstNonEmptyString(decision.Ack, decision.Told), decision.Persona, decision.Plan)
		if err != nil {
			// A discussion that cannot be written still must not start a run.
			base.Settled = true
			base.Kind = decision.Kind
			base.RunID = ""
			base.Reply.Text = firstNonEmptyString(decision.Ack, decision.Told)
			return content, base, true, nil
		}
		base.Settled = true
		base.Kind = decision.Kind
		base.Ack = decision.Ack
		base.Reply.Text = firstNonEmptyString(decision.Ack, decision.Told)
		base.RunID = ""
		base.Session = session
		base.Discuss = &view
		return content, base, true, nil
	case "reply", "ask":
		text := firstNonEmptyString(decision.Told, decision.Ack)
		if text != "" {
			view, err := s.recordDesktopDiscussion(ctx, in, session, content, text, decision.Persona, nil)
			if err != nil {
				base.Settled = true
				base.Reply.Text = text
				base.RunID = ""
				return content, base, true, nil
			}
			base.Settled = true
			base.Reply.Text = text
			base.RunID = ""
			base.Session = session
			base.Discuss = &view
			return content, base, true, nil
		}
	case "status":
		text, _, err := s.DesktopTaskQuery(ctx, in.TenantID, in.UserID, in.BotID, firstNonEmptyString(decision.Instruction, content), "")
		if err == nil && strings.TrimSpace(text) != "" {
			base.Settled = true
			base.Reply.Text = text
			base.RunID = ""
			return content, base, true, nil
		}
	case "schedule":
		if decision.Schedule != nil {
			base.Settled = true
			base.Reply.Text = firstNonEmptyString(decision.Ack, decision.Told)
			base.RunID = ""
			return content, base, true, nil
		}
	}
	next := strings.TrimSpace(decision.Instruction)
	if next == "" {
		next = content
	}
	base.Kind = "new"
	base.RunID = ""
	base.Session = session
	return next, base, false, nil
}

func desktopIntakeFailed(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "timeout", "fallback", "unreadable", "classifier unavailable", "no token", "no classifier", "empty":
		return true
	default:
		return false
	}
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

func (s *Service) fetchDesktopIntake(ctx context.Context, in desktopSteerRequest, running []desktopRunBrief, session string) desktopIntake {
	fallback := desktopIntake{Kind: "discuss", Pace: "discuss", Reason: "fallback"}
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
	if stored, ok := s.cachedDiscuss(discussCacheKey(in.TenantID, in.UserID, in.BotID, session)); ok {
		if persona := strings.TrimSpace(stored.Persona); persona != "" {
			body["persona"] = persona
		}
		if len(stored.Excerpts) > 0 {
			body["earlier"] = stored.Excerpts
		}
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
	if out.Kind != "discuss" && out.Kind != "plan_amend" && strings.TrimSpace(out.Phase) != "plan" {
		out.Phase = "execute"
	}
	return out
}
