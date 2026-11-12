package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Content routing for a message with no explicit hint: does it continue one
// of the bot's recent tasks (a thread that already finished, outside Hub's
// short sticky window) or is it a new matter? A cheap model reads each
// candidate's title, goal and result and answers with a confidence; only a
// confident pick routes, everything else (unsure, no answer in time, a
// session that was not offered) stays in the main conversation.

// DesktopFollowUpCandidate is one of the bot's recent task threads.
type DesktopFollowUpCandidate struct {
	Session string `json:"session"`
	Title   string `json:"title"`
	Goal    string `json:"goal,omitempty"`
	Result  string `json:"result,omitempty"`
}

// DesktopFollowUpRoute is the verdict; Session is "" for main.
type DesktopFollowUpRoute struct {
	Session    string  `json:"session"`
	Title      string  `json:"title,omitempty"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`
}

const (
	desktopFollowUpMaxCandidates = 5
	desktopFollowUpMinConfidence = 0.75
)

// desktopFollowUpBudget keeps the decision inside the time the person
// waits for the acknowledgement anyway.
var desktopFollowUpBudget = 1500 * time.Millisecond

const desktopFollowUpSystem = `You route a user's new message for a desktop AI assistant. The assistant recently worked on the numbered tasks below. Decide whether the new message continues one of them (asks to change, extend, fix, redo, send or otherwise act on that task's work or result) or is a new, independent matter.
Choose a task only when the message clearly refers to that task's subject or result. Greetings, general questions and new jobs are "new". If two tasks fit or you are unsure, answer "new".
Return JSON only: {"task": <task number, or 0 for new>, "confidence": <0..1>, "reason": "<short>"}`

// ClassifyDesktopFollowUp picks the recent task the message continues, or
// returns an empty Session for main.
func (a *App) ClassifyDesktopFollowUp(message string, candidates []DesktopFollowUpCandidate, lang string) (*DesktopFollowUpRoute, error) {
	main := &DesktopFollowUpRoute{}
	message = strings.TrimSpace(message)
	var offered []DesktopFollowUpCandidate
	for _, c := range candidates {
		c.Session = strings.TrimSpace(c.Session)
		if c.Session == "" || strings.TrimSpace(c.Title) == "" {
			continue
		}
		offered = append(offered, c)
		if len(offered) == desktopFollowUpMaxCandidates {
			break
		}
	}
	if message == "" || len(offered) == 0 || desktopBotProtocolMessage(message) {
		return main, nil
	}
	var b strings.Builder
	for i, c := range offered {
		fmt.Fprintf(&b, "%d. title: %s\n", i+1, clipRunes(oneLine(c.Title), 120))
		if g := strings.TrimSpace(c.Goal); g != "" && g != strings.TrimSpace(c.Title) {
			fmt.Fprintf(&b, "   goal: %s\n", clipRunes(oneLine(g), 200))
		}
		if r := strings.TrimSpace(c.Result); r != "" {
			fmt.Fprintf(&b, "   result: %s\n", clipRunes(oneLine(r), 240))
		}
	}
	userPrompt := fmt.Sprintf("recent tasks:\n%s\nnew message:\n%s\n\nlanguage: %s", b.String(), clipRunes(message, 500), lang)
	verdict := a.raceDesktopFollowUp(userPrompt, len(offered))
	if verdict == nil || verdict.task < 1 || verdict.task > len(offered) || verdict.confidence < desktopFollowUpMinConfidence {
		if verdict != nil {
			main.Confidence, main.Reason = verdict.confidence, verdict.reason
		}
		return main, nil
	}
	pick := offered[verdict.task-1]
	return &DesktopFollowUpRoute{Session: pick.Session, Title: pick.Title, Confidence: verdict.confidence, Reason: verdict.reason}, nil
}

type desktopFollowUpVerdict struct {
	task       int
	confidence float64
	reason     string
}

func (a *App) raceDesktopFollowUp(userPrompt string, n int) *desktopFollowUpVerdict {
	ctx, cancel := context.WithTimeout(context.Background(), desktopFollowUpBudget)
	defer cancel()
	_, fast := desktopBotAckModels(a)
	reqs := []desktopBotUnderstandRequest{
		{systemPrompt: desktopFollowUpSystem, userPrompt: userPrompt, tag: "desktop-bot-followup", maxOut: 120},
	}
	if fast != nil {
		reqs = append(reqs, desktopBotUnderstandRequest{systemPrompt: desktopFollowUpSystem, userPrompt: userPrompt, tag: "desktop-bot-followup", override: fast, maxOut: 120})
	}
	timeoutSec := int((desktopFollowUpBudget + time.Second - 1) / time.Second)
	out := make(chan *desktopFollowUpVerdict, len(reqs))
	for _, req := range reqs {
		req.timeoutSec = timeoutSec
		go func(req desktopBotUnderstandRequest) {
			raw, err := desktopBotUnderstandLLM(ctx, a, req)
			if err != nil {
				out <- nil
				return
			}
			for _, block := range desktopBotJSONObjects(raw) {
				var v struct {
					Task       *int     `json:"task"`
					Confidence *float64 `json:"confidence"`
					Reason     string   `json:"reason"`
				}
				if json.Unmarshal([]byte(block), &v) == nil && v.Task != nil && v.Confidence != nil {
					out <- &desktopFollowUpVerdict{task: *v.Task, confidence: *v.Confidence, reason: v.Reason}
					return
				}
			}
			out <- nil
		}(req)
	}
	for range reqs {
		select {
		case v := <-out:
			if v != nil {
				return v
			}
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

func desktopBotProtocolMessage(s string) bool {
	return s == desktopBotConfirmTask || s == desktopBotReturnTask || desktopBotFilledTask.MatchString(s) || desktopBotSavedTask.MatchString(s)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
