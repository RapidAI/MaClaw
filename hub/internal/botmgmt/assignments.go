package botmgmt

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"
)

// Assignments: the main bot's delegate notes read as work items, with a
// structured report protocol, causation and the main bot's review.
//
//   - A report carries state (progress, completed, blocked, waiting_human,
//     failed), expects_reply (a question; the sender ends its turn) and
//     answer_to (the question it answers).
//   - Every bot message records root, parent and hop; a chain deeper than
//     assignmentHopLimit is refused, so bots cannot talk in circles.
//   - Only the main bot hands out work (PostBotCrossTalk) and only it
//     accepts or returns a result (ReviewAssignment).
//
// MACLAW_ASSIGNMENTS=off keeps the notes exactly as before.

const assignmentHopLimit = 32

var reportStates = map[string]string{
	"progress": "progress", "completed": "done", "blocked": "blocked",
	"waiting_human": "question", "failed": "blocked",
}

func assignmentsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_ASSIGNMENTS"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// ReportTopicForState maps a protocol state to the stored topic.
func ReportTopicForState(state string) (string, bool) {
	t, ok := reportStates[strings.ToLower(strings.TrimSpace(state))]
	return t, ok
}

type noteProtocol struct {
	on              bool
	state, answerTo string
	expects         bool
	root, parent    string
	hop             int
}

// assignmentProtocol validates the protocol fields of a new note and works
// out its causation from the note it follows (answer_to, else reply_to).
func assignmentProtocol(notes []BotCrossNote, in BotCrossNote) (noteProtocol, error) {
	if !assignmentsEnabled() {
		return noteProtocol{}, nil
	}
	p := noteProtocol{on: true, answerTo: strings.TrimSpace(in.AnswerTo), expects: in.ExpectsReply}
	if st := strings.ToLower(strings.TrimSpace(in.State)); st != "" {
		if _, ok := reportStates[st]; !ok {
			return p, ErrInvalidInput
		}
		p.state = st
	}
	p.parent = firstNonEmptyString(p.answerTo, strings.TrimSpace(in.ReplyTo), strings.TrimSpace(in.ParentID))
	if parent := findCrossNote(notes, p.parent); parent != nil {
		p.root = firstNonEmptyString(parent.RootID, parent.ID)
		p.hop = parent.Hop + 1
	} else {
		p.parent = ""
	}
	if p.hop > assignmentHopLimit {
		return p, ErrHopLimit
	}
	return p, nil
}

func (p noteProtocol) apply(n *BotCrossNote) {
	if !p.on {
		return
	}
	n.State, n.ExpectsReply, n.AnswerTo = p.state, p.expects, p.answerTo
	n.ParentID, n.Hop = p.parent, p.hop
	n.RootID = firstNonEmptyString(p.root, n.ID)
	if p.state != "" {
		n.Topic = reportStates[p.state]
	}
}

// Assignment is one delegate note read as a work item.
type Assignment struct {
	ID           string   `json:"id"`
	OwnerBot     string   `json:"owner_bot"`
	TargetBot    string   `json:"target_bot"`
	Task         string   `json:"task"`
	State        string   `json:"state"` // queued, running, held, waiting_human, blocked, completed, failed, accepted, returned
	RunID        string   `json:"run_id,omitempty"`
	ParentID     string   `json:"parent_id,omitempty"`
	RootID       string   `json:"root_id,omitempty"`
	CreatedAt    string   `json:"created_at"`
	LastReport   string   `json:"last_report,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
	ResultNoteID string   `json:"result_note_id,omitempty"`
	OpenQuestion string   `json:"open_question,omitempty"`
	Review       string   `json:"review,omitempty"`
	ReviewNote   string   `json:"review_note,omitempty"`
	Reviewer     string   `json:"reviewer,omitempty"`
}

// assignmentsOf builds the read model from the notes, newest first.
func assignmentsOf(notes []BotCrossNote, userID string) []Assignment {
	out := []Assignment{}
	for _, d := range notes {
		if d.Owner != userID || d.Kind != "delegate" {
			continue
		}
		a := Assignment{ID: d.ID, OwnerBot: d.From, TargetBot: d.Target, Task: firstNonEmptyString(d.Order, d.Text), RunID: d.RunID,
			ParentID: d.ParentID, RootID: firstNonEmptyString(d.RootID, d.ID), CreatedAt: d.CreatedAt, Reviewer: d.Reviewer}
		switch {
		case d.HeldAt != "":
			a.State = "held"
		case d.RunID != "" || d.StartedAt != "":
			a.State = "running"
		default:
			a.State = "queued"
		}
		for _, n := range notes {
			if n.Owner != userID || n.ReplyTo != d.ID && n.ParentID != d.ID {
				continue
			}
			if n.Kind != "report" && n.Kind != "result" {
				continue
			}
			a.LastReport = n.Text
			a.Artifacts = append(a.Artifacts, n.Artifacts...)
			st := n.State
			if st == "" {
				switch n.Topic {
				case "done":
					st = "completed"
				case "blocked":
					st = "blocked"
					if strings.HasPrefix(n.Text, "失败") {
						st = "failed"
					}
				case "question":
					st = "waiting_human"
				}
			}
			if n.ExpectsReply {
				a.OpenQuestion = n.ID
				for _, m := range notes {
					if m.AnswerTo == n.ID {
						a.OpenQuestion = ""
					}
				}
			}
			switch st {
			case "completed", "failed", "blocked", "waiting_human":
				a.State = st
				if st == "completed" || st == "failed" {
					a.ResultNoteID = n.ID
					a.OpenQuestion = ""
					a.Review, a.ReviewNote = n.Review, n.ReviewNote
				}
			}
		}
		if a.Review != "" {
			a.State = a.Review
		}
		out = append(out, a)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i] // later filed first on equal times
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// ListAssignments returns this user's assignments, newest first.
func (s *Service) ListAssignments(ctx context.Context, tenantID, userID string) ([]Assignment, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	rec, err := s.loadForUser(ctx, tenantID, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	return assignmentsOf(pruneCrossNotes(rec.CrossNotes, s.now()), strings.TrimSpace(userID)), nil
}

// ListAssignmentsForInstance is ListAssignments for a server-side bot run.
func (s *Service) ListAssignmentsForInstance(ctx context.Context, tenantID, userID, instanceID string) ([]Assignment, error) {
	if _, err := s.botForInstance(ctx, tenantID, userID, instanceID); err != nil {
		return nil, err
	}
	return s.ListAssignments(ctx, tenantID, userID)
}

func (s *Service) botForInstance(ctx context.Context, tenantID, userID, instanceID string) (string, error) {
	s.mu.Lock()
	rec, err := s.loadForUser(ctx, tenantID, strings.TrimSpace(userID))
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	for _, bot := range botsOwnedBy(rec.Bots, strings.TrimSpace(userID)) {
		if strings.TrimSpace(bot.InstanceID) == strings.TrimSpace(instanceID) && instanceID != "" {
			return bot.ID, nil
		}
	}
	return "", ErrNotFound
}

// ReviewAssignmentFromInstance is the main bot accepting or returning a
// sub-bot's result. Only the main bot may review. A return files a new
// delegate to the same bot with the note as its order, linked as a child.
func (s *Service) ReviewAssignmentFromInstance(ctx context.Context, tenantID, userID, instanceID, assignmentID, verdict, note string) (Assignment, error) {
	if s == nil {
		return Assignment{}, ErrSettingsUnavailable
	}
	if !assignmentsEnabled() {
		return Assignment{}, ErrInvalidInput
	}
	from, err := s.botForInstance(ctx, tenantID, userID, instanceID)
	if err != nil {
		return Assignment{}, err
	}
	return s.ReviewAssignment(ctx, tenantID, userID, from, assignmentID, verdict, note)
}

// ReviewAssignment applies a verdict (accepted or returned) by reviewer,
// who must be the main bot or the assignment's named peer reviewer.
func (s *Service) ReviewAssignment(ctx context.Context, tenantID, userID, reviewer, assignmentID, verdict, note string) (Assignment, error) {
	verdict = strings.ToLower(strings.TrimSpace(verdict))
	if verdict != "accepted" && verdict != "returned" {
		return Assignment{}, ErrInvalidInput
	}
	userID = strings.TrimSpace(userID)
	s.mu.Lock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		s.mu.Unlock()
		return Assignment{}, err
	}
	d := findCrossNote(rec.CrossNotes, assignmentID)
	if d == nil || d.Owner != userID || d.Kind != "delegate" {
		s.mu.Unlock()
		return Assignment{}, ErrNotFound
	}
	if reviewer != primaryBotID(rec, userID) && reviewer != d.Reviewer {
		s.mu.Unlock()
		return Assignment{}, ErrNotPrimary
	}
	var a Assignment
	for _, x := range assignmentsOf(rec.CrossNotes, userID) {
		if x.ID == d.ID {
			a = x
		}
	}
	res := findCrossNote(rec.CrossNotes, a.ResultNoteID)
	if res == nil {
		s.mu.Unlock()
		return Assignment{}, ErrInvalidInput // nothing to review yet
	}
	if res.Review != "" {
		s.mu.Unlock()
		a.State = res.Review
		return a, nil
	}
	res.Review, res.ReviewNote = verdict, strings.TrimSpace(note)
	a.Review, a.ReviewNote, a.State = verdict, res.ReviewNote, verdict
	target, task, parent := d.Target, firstNonEmptyString(d.Order, d.Text), d.ID
	from := d.From
	if err := s.save(ctx, tenantID, rec); err != nil {
		s.mu.Unlock()
		return Assignment{}, err
	}
	s.mu.Unlock()
	if verdict == "returned" {
		order := "上次的结果被退回 / returned: " + firstNonEmptyString(strings.TrimSpace(note), "please redo") + "\n原任务 / task: " + task
		if _, err := s.PostBotCrossTalk(ctx, tenantID, userID, from, BotCrossNote{Kind: "delegate", Target: target, Text: order, ParentID: parent, Key: "return-" + parent + "-" + time.Now().UTC().Format("150405")}); err != nil {
			return a, err
		}
	}
	return a, nil
}

// assignmentReviewer picks the peer reviewer of a new delegate: the one the
// main bot named, else (S4) one chosen by peer-review policy. "" is none.
var assignmentReviewer = func(rec record, userID, from, target, task, named string) string {
	named = resolveParticipant(rec, userID, strings.TrimSpace(named))
	if named != "" && named != target && botOwned(rec.Bots, userID, named) {
		return named
	}
	return ""
}
