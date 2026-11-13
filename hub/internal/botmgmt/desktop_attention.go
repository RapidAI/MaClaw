package botmgmt

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopAttention is one current handoff for one bot. ID changes when the
// reason changes, so a GUI can tell a new ask from one Hub has already dropped.
type desktopAttention struct {
	ID     string
	Reason string
}

var desktopAttentionSeq atomic.Uint64

func newDesktopAttentionID(turn string) string {
	if id := strings.TrimSpace(turn); id != "" {
		return id
	}
	return "attn_" + time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + strconv.FormatUint(desktopAttentionSeq.Add(1), 10)
}

// The attention reason is per bot already, so it is keyed by the owner even
// when an isolated bot's desktop user is passed in.
func desktopAttentionKey(tenantID, userID, botID string) string {
	owner, _ := desktop.SplitDesktopUser(userID)
	return desktopViewKey(tenantID, owner) + "\x00" + strings.TrimSpace(botID)
}

func (s *Service) noteDesktopAttention(tenantID, userID, botID, reason string) {
	s.noteDesktopAttentionFor(tenantID, userID, botID, "", reason)
}

// noteDesktopAttentionFor records the handoff. turn is the run that asked,
// when the caller has one; the same reason keeps its id.
func (s *Service) noteDesktopAttentionFor(tenantID, userID, botID, turn, reason string) {
	if s == nil {
		return
	}
	botID = strings.TrimSpace(botID)
	reason = strings.TrimSpace(reason)
	if botID == "" || reason == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopAttention == nil {
		s.desktopAttention = map[string]desktopAttention{}
	}
	key := desktopAttentionKey(tenantID, userID, botID)
	cur := s.desktopAttention[key]
	id := strings.TrimSpace(turn)
	if id == "" {
		if cur.Reason == reason && cur.ID != "" {
			id = cur.ID
		} else {
			id = newDesktopAttentionID("")
		}
	}
	s.desktopAttention[key] = desktopAttention{ID: id, Reason: reason}
}

func (s *Service) forgetDesktopAttention(tenantID, userID, botID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgetDesktopAttentionLocked(tenantID, userID, botID)
}

func (s *Service) forgetDesktopAttentionLocked(tenantID, userID, botID string) {
	if s == nil || s.desktopAttention == nil {
		return
	}
	delete(s.desktopAttention, desktopAttentionKey(tenantID, userID, botID))
}

// DesktopAttention is the screen-handoff reason for this bot, if one is current.
func (s *Service) DesktopAttention(tenantID, userID, botID string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopAttention == nil {
		return ""
	}
	return strings.TrimSpace(s.desktopAttention[desktopAttentionKey(tenantID, userID, botID)].Reason)
}

// DesktopAttentionID is the current handoff's id for this bot, empty when
// Hub is not asking the person.
func (s *Service) DesktopAttentionID(tenantID, userID, botID string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopAttention == nil {
		return ""
	}
	return strings.TrimSpace(s.desktopAttention[desktopAttentionKey(tenantID, userID, botID)].ID)
}
