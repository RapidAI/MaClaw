package botmgmt

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// DesktopPlan is the conversation's plan. A nil list was not sent. An empty
// list clears that part of the plan.
type DesktopPlan struct {
	Goal        string    `json:"goal,omitempty"`
	Constraints *[]string `json:"constraints,omitempty"`
	Success     *[]string `json:"success,omitempty"`
	Open        *[]string `json:"open_questions,omitempty"`
}

// DesktopDiscuss is the server's discuss / planned / executing state for one
// bot conversation. The GUI shows it. It does not decide it.
type DesktopDiscuss struct {
	State    string      `json:"state"`
	Session  string      `json:"session,omitempty"`
	Plan     DesktopPlan `json:"plan"`
	Excerpts []string    `json:"excerpts,omitempty"`
	Persona  string      `json:"persona,omitempty"`
}

func discussCacheKey(tenantID, userID, botID, session string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(botID) + "\x00" + strings.TrimSpace(session)
}

func (s *Service) cachedDiscuss(key string) (DesktopDiscuss, bool) {
	if s == nil || key == "" {
		return DesktopDiscuss{}, false
	}
	value, ok := s.discussCache.Load(key)
	if !ok {
		return DesktopDiscuss{}, false
	}
	view, _ := value.(DesktopDiscuss)
	return view, true
}

func (s *Service) storeDiscuss(key string, view DesktopDiscuss) {
	if s == nil || key == "" {
		return
	}
	s.discussCache.Store(key, view)
}

func (s *Service) readDesktopDiscussion(ctx context.Context, in desktopSteerRequest, session string) (DesktopDiscuss, error) {
	var out DesktopDiscuss
	if s == nil || strings.TrimSpace(in.InstanceID) == "" || strings.TrimSpace(session) == "" {
		return out, nil
	}
	rec, token, err := s.steerToken(ctx, in)
	if err != nil {
		return out, err
	}
	path := "/api/v1/instances/" + url.PathEscape(in.InstanceID) + "/bot-discuss?client_session_key=" + url.QueryEscape(session)
	err = s.authAsOwner(ctx, in.TenantID, in.UserID, rec, token, http.MethodGet, path, nil, &out)
	if err == nil {
		s.storeDiscuss(discussCacheKey(in.TenantID, in.UserID, in.BotID, session), out)
	}
	return out, err
}

func (s *Service) recordDesktopDiscussion(ctx context.Context, in desktopSteerRequest, session, userText, assistantText, persona string, plan *DesktopPlan) (DesktopDiscuss, error) {
	var out DesktopDiscuss
	rec, token, err := s.steerToken(ctx, in)
	if err != nil {
		return out, err
	}
	body := map[string]any{
		"client_session_key": session,
		"client_message_id":  in.ClientMessageID,
		"user_text":          userText,
		"assistant_text":     assistantText,
		"persona":            persona,
	}
	if plan != nil {
		body["plan"] = plan
	}
	path := "/api/v1/instances/" + url.PathEscape(in.InstanceID) + "/bot-discuss"
	err = s.authAsOwner(ctx, in.TenantID, in.UserID, rec, token, http.MethodPost, path, body, &out)
	if err == nil {
		s.storeDiscuss(discussCacheKey(in.TenantID, in.UserID, in.BotID, session), out)
	}
	return out, err
}

func (s *Service) markDesktopDiscussionExecuting(ctx context.Context, in desktopSteerRequest, session string) error {
	rec, token, err := s.steerToken(ctx, in)
	if err != nil {
		return err
	}
	path := "/api/v1/instances/" + url.PathEscape(in.InstanceID) + "/bot-discuss/executing"
	return s.authAsOwner(ctx, in.TenantID, in.UserID, rec, token, http.MethodPost, path, map[string]any{
		"client_session_key": session,
	}, nil)
}

func (s *Service) steerToken(ctx context.Context, in desktopSteerRequest) (*record, *string, error) {
	rec := in.TokenRec
	token := in.Token
	if in.HaveToken && rec != nil && token != nil && strings.TrimSpace(*token) != "" {
		return rec, token, nil
	}
	fresh, tk, err := s.ownerToken(ctx, in.TenantID, in.UserID)
	if err != nil {
		return nil, nil, err
	}
	return &fresh, &tk, nil
}

// startDesktopDiscussion runs the stored plan in this conversation. The
// start sentence is not the instruction. A conversation with no plan does
// not start a run.
func (s *Service) startDesktopDiscussion(ctx context.Context, in desktopSteerRequest, session string) (string, BotAdmission, bool, error) {
	view, err := s.readDesktopDiscussion(ctx, in, session)
	base := BotAdmission{Kind: "discuss", ClientMessageID: in.ClientMessageID, Session: session}
	if err != nil {
		return "", base, true, err
	}
	base.Discuss = &view
	if view.State == "executing" && s.botHasLiveRun(in.TenantID, in.UserID, in.BotID) {
		base.Settled = true
		base.Ack = "这项还在做。"
		base.Reply.Text = base.Ack
		return "", base, true, nil
	}
	text := desktopExecutionText(view)
	if strings.TrimSpace(view.Plan.Goal) == "" && len(view.Excerpts) == 0 {
		base.Settled = true
		base.Ack = "还没有可以开始的计划。"
		base.Reply.Text = base.Ack
		return "", base, true, nil
	}
	base.Kind = "new"
	base.Phase = "execute"
	base.Ack = "按这个计划开始。"
	return text, base, false, nil
}

func desktopExecutionText(view DesktopDiscuss) string {
	var b strings.Builder
	b.WriteString("按这个计划执行。")
	if goal := strings.TrimSpace(view.Plan.Goal); goal != "" {
		b.WriteString("\n目标：")
		b.WriteString(goal)
	}
	writeDiscussLines(&b, "约束", view.Plan.Constraints)
	writeDiscussLines(&b, "完成标准", view.Plan.Success)
	writeDiscussLines(&b, "还没定的问题", view.Plan.Open)
	if len(view.Excerpts) > 0 {
		b.WriteString("\n讨论摘录：")
		for _, line := range view.Excerpts {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			b.WriteString("\n- ")
			b.WriteString(line)
		}
	}
	return b.String()
}

func writeDiscussLines(b *strings.Builder, title string, lines *[]string) {
	if lines == nil || len(*lines) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(title)
	b.WriteString("：")
	for _, line := range *lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b.WriteString("\n- ")
		b.WriteString(line)
	}
}
