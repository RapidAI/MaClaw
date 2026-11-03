package botmgmt

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Worker channel (d79). A bot running a delegated assignment is that
// assignment's worker: it never speaks to the person. Its results, blocks
// and confirm asks go to the router that delegated it (action=report /
// the run-end result), and that router alone tells the person.

// ErrWorkerSay: a worker tried to speak to the person directly.
var ErrWorkerSay = errors.New("worker must report to its router, not speak to the person")

// WorkerSayError names the assignment the worker should report on.
type WorkerSayError struct{ AssignmentID, Router string }

func (e *WorkerSayError) Error() string {
	return "WORKER_SAY: report with action=report reply_to=" + e.AssignmentID + "; the router tells the person"
}
func (e *WorkerSayError) Unwrap() error { return ErrWorkerSay }

// openWorkerAssignment returns the delegate note bot is currently working
// (started, no result yet, still fresh), or nil.
func openWorkerAssignment(notes []BotCrossNote, userID, bot string, now time.Time) *BotCrossNote {
	for i := len(notes) - 1; i >= 0; i-- {
		n := &notes[i]
		if n.Owner != userID || n.Kind != "delegate" || n.Target != bot || n.From == bot {
			continue
		}
		if strings.TrimSpace(n.RunID) == "" && strings.TrimSpace(n.StartedAt) == "" {
			continue
		}
		if resultFor(notes, n.ID) != nil {
			continue
		}
		if at, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil && now.Sub(at) > dispatchMaxAge {
			continue
		}
		return n
	}
	return nil
}

var workerSayPhrase = regexp.MustCompile(`(?i)[^。.;；\n]*(action\s*=\s*say|desktop\s+say|直接(把[^。;；\n]*)?(告诉|说给|发给)(人|主人|用户))[^。.;；\n]*[。.;；]?`)

// workerBrief strips "tell the person directly" wording from a delegate
// brief and states the worker's channel.
func workerBrief(body, assignmentID string) string {
	body = strings.TrimSpace(workerSayPhrase.ReplaceAllString(body, ""))
	body = strings.TrimRight(strings.TrimSpace(body), "，,")
	return body + "\n\n（你是这件事的执行者：不要用 action=say，也不要直接和主人说话。做完、卡住或需要主人确认，都用 action=report reply_to=" + assignmentID + "（state=completed / blocked / waiting_human）交回派活的机器人，由它告诉主人。）"
}
