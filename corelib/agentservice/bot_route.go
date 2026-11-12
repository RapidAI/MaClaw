package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Server-side routing of a new message to one of a bot's conversations.
// Clients only display the decision and forward explicit picks; the
// decision itself is made here (content) and in Hub (explicit hints and the
// sticky windows Hub keeps for the conversation that just finished or
// asked).

// BotRouteInput is one message to route.
type BotRouteInput struct {
	BotID   string `json:"bot_id"`
	Content string `json:"content"`
	// StickySession is the conversation Hub's sticky window holds, if any.
	// It wins over content; this call then only names it.
	StickySession string `json:"sticky_session,omitempty"`
}

// BotRoute is the decision. Session "" or the bot id means main.
type BotRoute struct {
	Session    string  `json:"session"`
	Title      string  `json:"title,omitempty"`
	Via        string  `json:"via"` // sticky, content, main
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// BotRouteClassifier answers the routing prompt with the model's raw text.
type BotRouteClassifier func(ctx context.Context, system, user string) (string, error)

const (
	botRouteMinConfidence = 0.75
	botRouteMaxCandidates = 5
	botRouteCandidateAge  = 24 * time.Hour
)

// BotRouteBudget bounds the classification; on timeout the message goes to
// main.
var BotRouteBudget = 1500 * time.Millisecond

const botRouteSystem = `You route a user's new message for a desktop AI assistant. The assistant recently worked on the numbered tasks below. Decide whether the new message continues one of them (asks to change, extend, fix, redo, send or otherwise act on that task's work or result) or is a new, independent matter.
Choose a task only when the message clearly refers to that task's subject or result. Greetings, general questions and new jobs are "new". If two tasks fit or you are unsure, answer "new".
Return JSON only: {"task": <task number, or 0 for new>, "confidence": <0..1>, "reason": "<short>"}`

// RouteBotMessage decides which of the bot's conversations a message with
// no explicit hint belongs to.
func (s *Service) RouteBotMessage(ctx context.Context, p Principal, instanceID string, in BotRouteInput, classify BotRouteClassifier) (BotRoute, error) {
	bot := strings.TrimSpace(in.BotID)
	main := BotRoute{Session: bot, Via: "main"}
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return BotRoute{}, err
	}
	if bot == "" || botOfClientSessionKey(bot) != bot {
		return BotRoute{}, ErrBotThreadsBadBot
	}
	now := s.now()
	if sticky := strings.TrimSpace(in.StickySession); sticky != "" && botOfClientSessionKey(sticky) == bot {
		route := BotRoute{Session: sticky, Via: "sticky"}
		for _, t := range s.botThreads(p, instanceID, bot, BotThreadQuery{}, now) {
			if t.Session == sticky {
				route.Title = t.Title
				break
			}
		}
		return route, nil
	}
	content := strings.TrimSpace(in.Content)
	candidates := s.botThreads(p, instanceID, bot, BotThreadQuery{MaxAge: botRouteCandidateAge, Limit: botRouteMaxCandidates, ExcludeRunning: true, ThreadsOnly: true}, now)
	if content == "" || len(candidates) == 0 || classify == nil {
		return main, nil
	}
	var b strings.Builder
	for i, c := range candidates {
		fmt.Fprintf(&b, "%d. title: %s\n", i+1, oneLineRunes(c.Title, 120))
		if c.Goal != "" {
			fmt.Fprintf(&b, "   latest request: %s\n", oneLineRunes(c.Goal, 200))
		}
		if c.Result != "" {
			fmt.Fprintf(&b, "   result: %s\n", oneLineRunes(c.Result, 240))
		}
	}
	user := fmt.Sprintf("recent tasks:\n%s\nnew message:\n%s", b.String(), oneLineRunes(content, 500))
	cctx, cancel := context.WithTimeout(ctx, BotRouteBudget)
	defer cancel()
	type answer struct {
		raw string
		err error
	}
	done := make(chan answer, 1)
	go func() {
		raw, err := classify(cctx, botRouteSystem, user)
		done <- answer{raw, err}
	}()
	var got answer
	select {
	case got = <-done:
	case <-cctx.Done():
		main.Reason = "timeout"
		return main, nil
	}
	if got.err != nil {
		main.Reason = "classifier unavailable"
		return main, nil
	}
	task, confidence, reason, ok := parseBotRouteVerdict(got.raw)
	if !ok || task < 1 || task > len(candidates) || confidence < botRouteMinConfidence {
		main.Confidence, main.Reason = confidence, reason
		return main, nil
	}
	pick := candidates[task-1]
	return BotRoute{Session: pick.Session, Title: pick.Title, Via: "content", Confidence: confidence, Reason: reason}, nil
}

func parseBotRouteVerdict(raw string) (int, float64, string, bool) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return 0, 0, "", false
	}
	var v struct {
		Task       *int     `json:"task"`
		Confidence *float64 `json:"confidence"`
		Reason     string   `json:"reason"`
	}
	if json.Unmarshal([]byte(raw[start:end+1]), &v) != nil || v.Task == nil || v.Confidence == nil {
		return 0, 0, "", false
	}
	return *v.Task, *v.Confidence, oneLineRunes(v.Reason, 120), true
}
