package botmgmt

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// One handoff (the bot asked the person to log in or solve a captcha) gets
// exactly one continuation. Three paths can each start it: the chat's
// 验证完成/登录完成，继续 (action "return"), the 交还 button
// (PostBotDesktopHandBackHandler), and Hub's own resume after the page shows
// signed in (bot_desktop_resume.go). Each mints its own turn key, so the
// per-key replay cannot fold them, and two continuations of the same task
// ran side by side (「2 件同时在做」, 11-16).
//
// A handoff has an identity: desktopHandoffSeq, bumped when a bot's pin
// opens (noteDesktopHeld) or a takeover begins on its display. The first
// continuation of a sequence claims it; any later one of the same sequence
// joins the run while it is in flight and is answered without a new turn
// once it finished. A new ask opens a new sequence, so a captcha that comes
// back during the continuation is a fresh handoff, never a stale one.
//
// The callers say a post is a continuation (WithDesktopContinuation, or
// action "return"). The sentence match is only for older clients that send
// the button's text as a plain message: the whole message must be one of
// those labels, and such a post only joins a continuation still in flight.
// A typed 「登录完成，继续发帖」 is an instruction and is never swallowed.
var desktopContinueRe = regexp.MustCompile(`(?i)^(?:登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续|登录完成，继续|登入完成，繼續|验证完成，继续|驗證完成，繼續|login done, continue|verified, continue)[。.!！]?$`)

// An admission still posting has no admit state yet. Past this it either
// made one or the post itself failed and cleared its mark. A run that was
// accepted and later failed keeps the mark: the client acks the result and
// the admit record is deleted, so the failure has to live on the mark or
// the next press is told the handoff already finished.
const desktopContinuePostingGrace = 2 * time.Minute

const (
	DesktopContinueJoinedLine = "好，刚才的交还已经在接着做了，这次不再另开一件。"
	desktopContinueDoneLine   = "这次交还已经接着做完了，不再重复做一遍；还要做别的，直接告诉我。"
)

// seq is the handoff this continuation answers (0: none known, e.g. after a
// Hub restart). admitted: the post left an admit state behind; a mark like
// that whose state is gone is a finished turn, not one posting. queued: the
// post went into the desktop queue, whose replay skips this gate. failed:
// that turn ended in an error. Its admit record does not stay (the client
// acks it, or the keep window passes), and a later press may start again.
type desktopContinueMark struct {
	key      string
	seq      uint64
	at       time.Time
	admitted bool
	queued   bool
	failed   bool
}

type desktopContinuationKey struct{}

// WithDesktopContinuation marks a post as the continuation of a handoff.
func WithDesktopContinuation(ctx context.Context) context.Context {
	return context.WithValue(ctx, desktopContinuationKey{}, true)
}

// desktopContinuation reports whether a post continues a handoff, and
// whether the caller said so (explicit) rather than only the text matching.
func desktopContinuation(ctx context.Context, action, content string) (continues, explicit bool) {
	if marked, _ := ctx.Value(desktopContinuationKey{}).(bool); marked {
		return true, true
	}
	if strings.EqualFold(strings.TrimSpace(action), "return") {
		return true, true
	}
	return desktopContinueText(content), false
}

func desktopContinueText(content string) bool {
	return desktopContinueRe.MatchString(strings.TrimSpace(content))
}

func desktopContinueBotKey(tenantID, userID, botID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(botID)
}

func desktopHandoffSeqKey(tenantID, desktopUser, botID string) string {
	return desktopViewKey(tenantID, desktopUser) + "\x00" + strings.TrimSpace(botID)
}

// openDesktopHandoffLocked starts a new handoff identity for this bot's
// display. Callers hold s.mu.
func (s *Service) openDesktopHandoffLocked(tenantID, desktopUser, botID string) {
	if strings.TrimSpace(botID) == "" {
		return
	}
	if s.desktopHandoffSeq == nil {
		s.desktopHandoffSeq = map[string]uint64{}
	}
	s.desktopHandoffSeq[desktopHandoffSeqKey(tenantID, desktopUser, botID)]++
}

// openDesktopTakeover is a takeover begun on the bot's display. While this
// handoff's continuation is still running, a takeover (the person grabbing
// the screen mid-run) belongs to that continuation, not a new handoff:
// otherwise its 交还 would start a second turn beside the first.
func (s *Service) openDesktopTakeover(tenantID, desktopUser, botID string) {
	if s == nil {
		return
	}
	owner, _ := desktop.SplitDesktopUser(desktopUser)
	s.mu.Lock()
	defer s.mu.Unlock()
	if mark, ok := s.desktopContinues[desktopContinueBotKey(tenantID, owner, botID)]; ok {
		if mark.queued && s.desktopContinueParkedLocked(tenantID, owner, botID, mark.key) {
			return
		}
		if state, found := s.desktopContinueStateLocked(tenantID, owner, botID, mark.key); found {
			state.mu.Lock()
			done := state.done
			state.mu.Unlock()
			if !done {
				return
			}
		} else if !mark.admitted && !mark.queued && s.now().Sub(mark.at) < desktopContinuePostingGrace {
			return
		}
	}
	s.openDesktopHandoffLocked(tenantID, desktopUser, botID)
}

// joinDesktopContinue either names this post as the continuation of the
// bot's current handoff or returns the admission that answers it without a
// new turn. The check and the mark are one step under s.mu, so two
// continuations racing in cannot both start. The release keeps the mark
// only for a post that left a turn behind (an admit state, or a queued
// item); anything else clears it, so a retry is not joined to a turn that
// never ran.
// Only an explicit continuation (button, hand-back, Hub's resume) is told
// "already done"; a text-only match after the continuation finished posts
// as an ordinary message.
func (s *Service) joinDesktopContinue(tenantID, userID, desktopUser, botID, clientMessageID string, explicit bool) (BotAdmission, bool, func(BotAdmission, error)) {
	noop := func(BotAdmission, error) {}
	clientMessageID = strings.TrimSpace(clientMessageID)
	if s == nil || clientMessageID == "" {
		return BotAdmission{}, false, noop
	}
	botKey := desktopContinueBotKey(tenantID, userID, botID)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := s.desktopHandoffSeq[desktopHandoffSeqKey(tenantID, desktopUser, botID)]
	if mark, ok := s.desktopContinues[botKey]; ok && mark.key != clientMessageID && mark.seq == seq {
		state, found := s.desktopContinueStateLocked(tenantID, userID, botID, mark.key)
		finished := false
		switch {
		case found:
			state.mu.Lock()
			done, failed := state.done, state.err != nil
			state.mu.Unlock()
			switch {
			case !done:
				// Still going. The press that started it owns the run and its
				// bubble; handing this press the run id made the desktop app
				// wait on it too, which showed a second task for the same work.
				return desktopContinueReply(clientMessageID, DesktopContinueJoinedLine), true, noop
			case !failed:
				finished = true
			}
			// A turn that ended in an error leaves finished false, so a press
			// may start it again. "already done" would leave that handoff
			// with no way on.
		case mark.queued:
			// Its replay keeps the turn key, so once it starts the admit
			// state above answers. Until then the queue does; gone from it
			// without a run means it gave up, and a new one may start.
			// A "running" queue row is only a poll record of a turn that
			// already started. Treating it as parked made every later press
			// say "still going" after the run record itself had expired.
			if s.desktopContinueParkedLocked(tenantID, userID, botID, mark.key) {
				return desktopContinueReply(clientMessageID, DesktopContinueJoinedLine), true, noop
			}
		case mark.admitted && !mark.failed:
			finished = true
		case !mark.failed && now.Sub(mark.at) < desktopContinuePostingGrace:
			return desktopContinueReply(clientMessageID, DesktopContinueJoinedLine), true, noop
		}
		// Without a known handoff there is nothing to tell a repeat from the
		// next task, so a finished one lets the next start.
		if finished && seq > 0 && explicit {
			return desktopContinueReply(clientMessageID, desktopContinueDoneLine), true, noop
		}
	}
	if s.desktopContinues == nil {
		s.desktopContinues = map[string]desktopContinueMark{}
	}
	s.desktopContinues[botKey] = desktopContinueMark{key: clientMessageID, seq: seq, at: now}
	return BotAdmission{}, false, func(adm BotAdmission, err error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		mark, ok := s.desktopContinues[botKey]
		if !ok || mark.key != clientMessageID {
			return
		}
		_, found := s.desktopContinueStateLocked(tenantID, userID, botID, clientMessageID)
		switch {
		case err == nil && found:
			mark.admitted = true
			s.desktopContinues[botKey] = mark
		case err == nil && adm.Queued:
			mark.queued = true
			s.desktopContinues[botKey] = mark
		case err == nil && adm.Settled:
			// Answered without a run (a steer, a parked offline item): the
			// handoff got its answer.
			mark.admitted = true
			s.desktopContinues[botKey] = mark
		default:
			delete(s.desktopContinues, botKey)
		}
	}
}

// desktopContinueOwnerTurn is the turn key of the continuation that owns
// this handoff. A later press joins it and must not take the pin: only the
// turn that runs can clear the pin, or give the keyboard back if it fails.
func (s *Service) desktopContinueOwnerTurn(tenantID, userID, botID string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mark, ok := s.desktopContinues[desktopContinueBotKey(tenantID, userID, botID)]
	if !ok {
		return ""
	}
	return mark.key
}

// noteDesktopContinueRunning records that a continuation parked in the
// queue has actually started. The replay that starts it skips this gate, so
// nothing else would ever clear the queued mark: after the run record
// expired, the same handoff either looked still busy or started a second time.
func (s *Service) noteDesktopContinueRunning(tenantID, userID, botID, clientMessageID string) {
	if s == nil {
		return
	}
	clientMessageID = strings.TrimSpace(clientMessageID)
	s.mu.Lock()
	defer s.mu.Unlock()
	key := desktopContinueBotKey(tenantID, userID, botID)
	mark, ok := s.desktopContinues[key]
	if !ok || mark.key != clientMessageID {
		return
	}
	mark.admitted = true
	mark.queued = false
	s.desktopContinues[key] = mark
}

// noteDesktopContinueFailed records that the continuation's run ended in an
// error. The admit record is dropped once the client has been shown the
// result, and a mark that only says admitted would then answer the next
// press with "already done", so the same handoff could not be tried again.
func (s *Service) noteDesktopContinueFailed(tenantID, userID, botID, clientMessageID string) {
	if s == nil {
		return
	}
	clientMessageID = strings.TrimSpace(clientMessageID)
	if clientMessageID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := desktopContinueBotKey(tenantID, userID, botID)
	mark, ok := s.desktopContinues[key]
	if !ok || mark.key != clientMessageID {
		return
	}
	mark.failed = true
	mark.queued = false
	s.desktopContinues[key] = mark
}

func desktopContinueReply(clientMessageID, line string) BotAdmission {
	return BotAdmission{Accepted: true, Settled: true, Joined: true, ClientMessageID: clientMessageID, Kind: "continue", Reply: Reply{Text: line, SendToUser: line}}
}

// desktopContinueParkedLocked reports a queued turn that has not run yet.
// Callers hold s.mu.
func (s *Service) desktopContinueParkedLocked(tenantID, userID, botID, clientMessageID string) bool {
	if s.desktopStarting[desktopStartingKey(tenantID, userID, botID, clientMessageID)] != nil {
		return true
	}
	for _, item := range s.desktopQueues[desktopQueueKey(tenantID, userID, botID)] {
		if item == nil || item.ClientMessageID != clientMessageID {
			continue
		}
		switch item.State {
		case "", "queued", "held":
			return true
		}
	}
	return false
}

// desktopContinueStateLocked finds the admit state of one turn key, whether
// it is still the empty-run reserve or already keyed by its run. Callers
// hold s.mu.
func (s *Service) desktopContinueStateLocked(tenantID, userID, botID, clientMessageID string) (*desktopAdmitState, bool) {
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		match := strings.TrimSpace(state.clientMessageID) == clientMessageID
		state.mu.Unlock()
		if match {
			return state, true
		}
	}
	return nil, false
}
