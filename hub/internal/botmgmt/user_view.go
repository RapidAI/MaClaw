package botmgmt

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// UserDesktopViewHold is how long one open Bot 观看/接管 view keeps the user's
// desktop up after the last command. The GUI refreshes it on every poll, so the
// window only has to survive a poll hiccup, not a page close. Without it the
// desktop stops the moment a run finishes and the live picture the human is
// watching goes black — exactly the "VNC 闪一下就消失" defect.
const UserDesktopViewHold = 2 * time.Minute

// noteDesktopUserView records that this user opened a Bot desktop view just
// now, so idle checks leave the desktop alone while that view is alive. The
// timestamp always lands in memory; the disk write is skipped while a previous
// write for the same view is younger than 30s, because the GUI polls a few
// times per second at most.
func (s *Service) noteDesktopUserView(tenantID, userID string) {
	if s == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	now := s.now()
	if s.desktopUserView == nil {
		s.desktopUserView = map[string]time.Time{}
	}
	if last, ok := s.desktopUserView[key]; ok && now.Sub(last) < 30*time.Second {
		s.desktopUserView[key] = now
		return
	}
	s.desktopUserView[key] = now
	s.persistDesktopState(tenantID)
}

// userDesktopViewActiveLocked reports whether a user's Bot view of this
// desktop is still inside its hold window. Callers must hold s.mu.
func (s *Service) userDesktopViewActiveLocked(key string) bool {
	last, ok := s.desktopUserView[key]
	if !ok {
		return false
	}
	age := s.now().Sub(last)
	if age < 0 || age >= UserDesktopViewHold {
		delete(s.desktopUserView, key)
		return false
	}
	return true
}

// HoldDesktopView keeps this user's desktop up while a human is watching or
// taking over from the Bot page, and returns the live noVNC page. It starts
// the desktop when nothing is running, so 人类接管 works on an idle bot.
// user_control stays true only when this bot already handed the keyboard to
// the person for a login.
func (s *Service) HoldDesktopView(ctx context.Context, tenantID, userID, botID string) (string, bool, error) {
	if s == nil || s.Desktop == nil {
		return "", false, nil
	}
	enabled, err := s.Enabled(ctx, tenantID, userID)
	if err != nil {
		return "", false, err
	}
	if !enabled {
		return "", false, ErrDisabled
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return "", false, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return "", false, ErrNotFound
	}
	if s.desktopViewURL(tenantID, userID) == "" {
		// The desktop is down. Opening it here mirrors PostDesktopViewAdminHandler.
		// The hold lands before the gate drops so a stop that a finishing command
		// is running cannot slip in between the open and the hold and kill the
		// fresh picture.
		gate := s.desktopUserGate(tenantID, userID)
		gate.Lock()
		novnc, openErr := s.Desktop.Open(ctx, tenantID, userID)
		if openErr != nil {
			gate.Unlock()
			return "", false, fmt.Errorf("%w: %s", ErrSrv, openErr.Error())
		}
		if novnc = strings.TrimSpace(novnc); novnc != "" {
			s.rememberDesktopView(tenantID, userID, novnc)
		}
		s.noteDesktopUserView(tenantID, userID)
		gate.Unlock()
	} else {
		s.noteDesktopUserView(tenantID, userID)
	}
	novnc := s.desktopViewURL(tenantID, userID)
	return novnc, novnc != "" && s.desktopKeyboardForBot(tenantID, userID, botID), nil
}
