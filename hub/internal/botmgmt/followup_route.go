package botmgmt

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Subscription passthrough (after the Super-agent prototype's Ticket
// Subscriptions): a delegate note the main bot handed to a teammate, with a
// run id and no closing report, is that teammate's subscription to the
// person's conversation with the main bot. A follow-up the person writes to
// the main bot while it is live goes straight to the teammate's run as a
// supplement; the main bot does not re-route it with its own judgement.
//
// The decision is two-level: cheap rules first, and only what the rules
// cannot settle is left to the model (the GUI's supplement reader). The
// subscription itself is the persisted cross note, so a Hub restart keeps it.

const followUpWindow = 2 * time.Hour

// FollowUpRoute is what RouteFollowUp did with one message.
type FollowUpRoute struct {
	Routed       bool   `json:"routed"`
	Reason       string `json:"reason"` // routed | no_subscription | new_job | ambiguous | run_ended
	BotID        string `json:"bot_id,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	SupplementID string `json:"supplement_id,omitempty"`
}

var (
	followUpURLRe = regexp.MustCompile(`(?i)https?://|www\.`)
	// A message that opens another job.
	followUpNewJobRe = regexp.MustCompile(`^(另外|还有一件|还有个事|再帮我|帮我再|帮我|新任务|换个任务|顺便帮我|你去|去)|(?i)^(also|another task|new task|please also)`)
	// A message that only refines the job already underway.
	followUpRefineRe = regexp.MustCompile(`^(要|需要|加上|加个|加点|配上|配图|带上|改成|改为|换成|别|不要|不用|别用|标题|正文|图片|封面|用|再加|多加|多写|少写|字数|语气|风格|记得|注意|最好|尽量|只要|只发|发在|发到|定时|分段|图文)|图文并茂|(?i)^(make it|use |add |no |don't|do not|shorter|longer|with )`)
	followUpStopRe   = regexp.MustCompile(`^(停|停下|别做了|不做了|取消|算了)|(?i)^(stop|cancel)\b`)
)

// followUpRule is the rule level: "follow", "new", or "" (the model decides).
func followUpRule(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || followUpStopRe.MatchString(t) {
		return "new" // stopping is the main bot's STOP_WORK path
	}
	if followUpURLRe.MatchString(t) || utf8.RuneCountInString(t) > 120 || followUpNewJobRe.MatchString(t) {
		return "new"
	}
	if followUpRefineRe.MatchString(t) && utf8.RuneCountInString(t) <= 80 {
		return "follow"
	}
	return ""
}

func followUpTerminalTopic(topic string) bool {
	switch strings.TrimSpace(topic) {
	case "done", "blocked", "question", "failed":
		return true
	}
	return false
}

// liveDelegateSubscription is the newest delegate note fromBot handed out
// whose run is still open.
func liveDelegateSubscription(notes []BotCrossNote, userID, fromBot string, now time.Time) (BotCrossNote, bool) {
	var best BotCrossNote
	found := false
	for _, n := range notes {
		if n.Owner != userID || n.Kind != "delegate" || n.From != fromBot || strings.TrimSpace(n.RunID) == "" || n.Target == "" || n.Target == fromBot {
			continue
		}
		at, err := time.Parse(time.RFC3339, n.CreatedAt)
		if err != nil || now.Sub(at) > followUpWindow {
			continue
		}
		closed := false
		for _, r := range notes {
			if r.ReplyTo == n.ID && followUpTerminalTopic(r.Topic) {
				closed = true
				break
			}
		}
		if closed {
			continue
		}
		if !found || n.CreatedAt > best.CreatedAt {
			best, found = n, true
		}
	}
	return best, found
}

// followUpBrief is the bot-to-bot brief the teammate's run reads. The
// person never sees it; the teammate answers in its own voice.
func followUpBrief(task, content string) string {
	b := "The person just added this to the task you are doing for them (they wrote it to the main bot; it reaches you directly because this task is yours). Their words: 「" + strings.TrimSpace(content) + "」. "
	if t := strings.TrimSpace(task); t != "" {
		if r := []rune(t); len(r) > 200 {
			t = string(r[:200]) + "…"
		}
		b += "Your task: " + t + ". "
	}
	return b + "Fold it into the work now; do not start over and do not redo a step that is already done (never publish or pay twice). If it is clearly about something else, ignore it here and say so in your final result. When you report, say in one short first-person sentence that you did it with this change."
}

// RouteFollowUp passes a person's message to botID straight to the teammate
// run subscribed to it, when the rules say it refines that run's job.
func (s *Service) RouteFollowUp(ctx context.Context, tenantID, userID, botID, content string) (FollowUpRoute, error) {
	if s == nil {
		return FollowUpRoute{}, ErrSettingsUnavailable
	}
	botID = strings.TrimSpace(botID)
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return FollowUpRoute{}, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return FollowUpRoute{}, ErrNotFound
	}
	sub, ok := liveDelegateSubscription(rec.CrossNotes, strings.TrimSpace(userID), rec.Bots[index].ID, time.Now())
	if !ok {
		return FollowUpRoute{Reason: "no_subscription"}, nil
	}
	out := FollowUpRoute{BotID: sub.Target, RunID: sub.RunID}
	switch followUpRule(content) {
	case "new":
		out.Reason = "new_job"
		return out, nil
	case "":
		out.Reason = "ambiguous"
		return out, nil
	}
	task := sub.Order
	if task == "" {
		task = sub.Text
	}
	res, err := s.SupplementDesktopRun(ctx, tenantID, userID, sub.Target, sub.RunID, followUpBrief(task, content))
	if err != nil || !res.Accepted {
		traceBot(sub.Target, "hub_followup_route", err, "run", sub.RunID, "routed", "no")
		out.Reason = "run_ended"
		return out, nil
	}
	traceBot(sub.Target, "hub_followup_route", nil, "run", sub.RunID, "routed", "yes", "from", botID)
	out.Routed, out.Reason, out.SupplementID = true, "routed", res.SupplementID
	return out, nil
}
