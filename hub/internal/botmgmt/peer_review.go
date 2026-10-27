package botmgmt

import (
	"os"
	"regexp"
	"strings"
)

// Peer review (S4). A delegate whose task publishes or pays gets a
// reviewer automatically: another teammate when there is one, else the
// main bot. When the worker reports completed, the reviewer is asked to
// check it (accept_result / return_result) and the assignment reads
// awaiting_review until then. The main bot may also name a reviewer on any
// delegate. MACLAW_PEER_REVIEW=off turns the automatic part off.

// peerReviewTask is generic intent wording, not any site's logic.
var peerReviewTask = regexp.MustCompile(`(?i)(发布|发表|发帖|发文|上架|publish|post\s+(it|to|on)|付款|支付|下单|购买|结账|payment|\bpay\b|checkout|purchase|transfer\s+money|转账)`)

func peerReviewAuto() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_PEER_REVIEW"))) {
	case "off", "0", "false", "no":
		return false
	}
	return assignmentsEnabled()
}

// assignmentReviewer picks the reviewer of a new delegate: the one the main
// bot named, else (for publish/payment tasks) another teammate, else the
// main bot itself. "" is no review.
var assignmentReviewer = func(rec record, userID, from, target, task, named string) string {
	if !assignmentsEnabled() {
		return ""
	}
	if named = resolveParticipant(rec, userID, strings.TrimSpace(named)); named != "" && named != target && botOwned(rec.Bots, userID, named) {
		return named
	}
	if !peerReviewAuto() || !peerReviewTask.MatchString(task) {
		return ""
	}
	for _, b := range botsOwnedBy(rec.Bots, userID) {
		if b.ID != target && b.ID != from {
			return b.ID
		}
	}
	return from
}

// owningDelegate walks a note's causation back to its delegate.
func owningDelegate(notes []BotCrossNote, n BotCrossNote) *BotCrossNote {
	id := firstNonEmptyString(n.ReplyTo, n.ParentID)
	for hop := 0; id != "" && hop <= assignmentHopLimit; hop++ {
		p := findCrossNote(notes, id)
		if p == nil {
			return nil
		}
		if p.Kind == "delegate" {
			return p
		}
		id = firstNonEmptyString(p.ReplyTo, p.ParentID)
	}
	return nil
}

// requestPeerReviewLocked asks a delegate's peer reviewer to check a
// completed result. The main bot reviews from its own mailbox already.
func (s *Service) requestPeerReviewLocked(rec *record, userID string, result BotCrossNote) {
	if !assignmentsEnabled() || !(result.State == "completed" || result.State == "" && result.Topic == "done") {
		return
	}
	d := owningDelegate(rec.CrossNotes, result)
	if d == nil || d.Reviewer == "" || d.Reviewer == d.From || d.Reviewer == result.From {
		return
	}
	task := firstNonEmptyString(d.Order, d.Text)
	if r := []rune(task); len(r) > 300 {
		task = string(r[:300])
	}
	_, _, _ = s.reportLocked(rec, userID, result.From, BotCrossNote{
		Target: d.Reviewer, Topic: "question",
		Text:      "请审核 / please review assignment=" + d.ID + "\n任务 / task: " + task + "\n结果 / result: " + result.Text + "\n核对产物后用 action=accept_result 或 action=return_result (assignment_id=" + d.ID + ", note=原因)。",
		Artifacts: result.Artifacts, Files: result.Files, ReplyTo: result.ID, Key: "review-" + result.ID,
	})
}
