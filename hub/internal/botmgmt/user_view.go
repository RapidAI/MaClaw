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

// desktopViewOpenTimeout is how long one watch may spend bringing the desktop
// up. The GUI poll waits the same length, so a start that finishes inside
// this budget answers that same call. The start keeps its own deadline: a
// poll that gives up must not abort the container.
const desktopViewOpenTimeout = 60 * time.Second

type desktopWatchEpochKey struct{}

// WithDesktopWatchEpoch marks one open of the Bot desktop page. Polls and the
// release for that open share the generation. An older release must not take
// down a page that has already opened again. Zero means the caller did not
// send one, and the watch behaves as it did before generations existed.
func WithDesktopWatchEpoch(ctx context.Context, epoch int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if epoch <= 0 {
		return ctx
	}
	return context.WithValue(ctx, desktopWatchEpochKey{}, epoch)
}

func desktopWatchEpoch(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	epoch, _ := ctx.Value(desktopWatchEpochKey{}).(int64)
	return epoch
}

// noteDesktopUserView records that this user opened a Bot desktop view just
// now, so idle checks leave the desktop alone while that view is alive. The
// timestamp always lands in memory; the disk write is skipped while a previous
// write for the same view is younger than 30s, because the GUI polls a few
// times per second at most. A positive epoch loses to a newer poll, and to a
// release of this generation, so a poll from a page that has already closed
// does not pin the desktop again.
func (s *Service) noteDesktopUserView(tenantID, userID string, epoch int64) bool {
	if s == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	if epoch > 0 {
		if epoch <= s.desktopWatchClosed[key] || s.desktopWatchGen[key] > epoch {
			return false
		}
		if s.desktopWatchGen == nil {
			s.desktopWatchGen = map[string]int64{}
		}
		s.desktopWatchGen[key] = epoch
	}
	now := s.now()
	if s.desktopUserView == nil {
		s.desktopUserView = map[string]time.Time{}
	}
	if last, ok := s.desktopUserView[key]; ok && now.Sub(last) < 30*time.Second {
		s.desktopUserView[key] = now
		return true
	}
	s.desktopUserView[key] = now
	s.persistDesktopState(tenantID)
	return true
}

// desktopWatchStillCurrent reports whether this poll's generation may still
// open the desktop. A release of that generation, or a newer poll, wins.
func (s *Service) desktopWatchStillCurrent(tenantID, userID string, epoch int64) bool {
	if s == nil || epoch <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := desktopViewKey(tenantID, userID)
	if epoch <= s.desktopWatchClosed[key] || s.desktopWatchGen[key] > epoch {
		return false
	}
	return true
}

// desktopWatchReplaced reports whether a poll newer than this release has
// already noted a watch. Epoch 0 has no generation to compare.
func (s *Service) desktopWatchReplaced(tenantID, userID string, epoch int64) bool {
	if s == nil || epoch <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.desktopWatchGen[desktopViewKey(tenantID, userID)] > epoch
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
	// A poll that already gave up must not pin the desktop or start it. The
	// start that is already running keeps its own deadline; this one does not
	// begin another.
	if ctx != nil && ctx.Err() != nil {
		return "", false, fmt.Errorf("%w: %s", ErrSrv, ctx.Err().Error())
	}
	// The hold is recorded before the gate. A command that finishes while this
	// poll is still opening has to see the person and leave the browser up.
	// Recording it only after Open returns lets that stop win, and a poll that
	// gives up never records it at all. A generation that has already been
	// released belongs to a page that is gone, so it must not pin a new one.
	epoch := desktopWatchEpoch(ctx)
	if !s.noteDesktopUserView(tenantID, userID, epoch) {
		return "", false, nil
	}
	if s.desktopViewURL(tenantID, userID) == "" {
		gate := s.desktopUserGate(tenantID, userID)
		gate.Lock()
		defer gate.Unlock()
		// Another poll may have published the picture while this one waited.
		// A canceled waiter must not start a second desktop behind that poll.
		// A release that won while this poll waited must not start one either.
		if s.desktopViewURL(tenantID, userID) == "" {
			if ctx != nil && ctx.Err() != nil {
				return "", false, fmt.Errorf("%w: %s", ErrSrv, ctx.Err().Error())
			}
			if !s.desktopWatchStillCurrent(tenantID, userID, epoch) {
				return "", false, nil
			}
			openCtx, cancel := desktopViewOpenContext(ctx)
			defer cancel()
			novnc, openErr := s.Desktop.Open(openCtx, tenantID, userID)
			if openErr != nil {
				return "", false, fmt.Errorf("%w: %s", ErrSrv, openErr.Error())
			}
			// The panel can close while the container is still starting.
			// Publishing that picture, or leaving the desktop up, puts a
			// browser on screen for a watch that has already been released.
			instanceID := rec.Bots[index].InstanceID
			if !s.userDesktopViewActive(tenantID, userID) && !s.desktopStopBlocked(tenantID, userID, instanceID) {
				// Open may have used the whole start budget. The stop needs
				// its own, or a slow start leaves the container up after the
				// panel has already gone.
				stopCtx, stopCancel := desktopViewOpenContext(context.Background())
				defer stopCancel()
				if stopErr := s.stopDesktop(stopCtx, tenantID, userID); stopErr != nil {
					return "", false, fmt.Errorf("%w: %s", ErrSrv, stopErr.Error())
				}
				return "", false, nil
			}
			if novnc = strings.TrimSpace(novnc); novnc != "" {
				s.rememberDesktopView(tenantID, userID, novnc)
			}
		}
	}
	novnc := s.desktopViewURL(tenantID, userID)
	return novnc, novnc != "" && s.desktopKeyboardForBot(tenantID, userID, botID), nil
}

// userDesktopViewActive reports whether this user's Bot page still holds the
// desktop. An expired hold is dropped.
func (s *Service) userDesktopViewActive(tenantID, userID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesktopHydrated(tenantID)
	return s.userDesktopViewActiveLocked(desktopViewKey(tenantID, userID))
}

// desktopStopBlocked reports whether some other use still needs the desktop,
// so closing the Bot page must not stop it.
func (s *Service) desktopStopBlocked(tenantID, userID, instanceID string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	opening := 0
	if s.desktopOpening != nil {
		opening = s.desktopOpening[key]
	}
	awaiting := s.desktopAwaiting[key]
	_, held := s.desktopHeld[key]
	admin := s.adminDesktopViewActiveLocked(key)
	s.mu.Unlock()
	return opening != 0 || awaiting || held || admin || s.otherInstanceHasDesktop(tenantID, userID, instanceID)
}

// desktopViewOpenContext detaches a desktop start from the watch poll. The
// poll context is canceled when its HTTP call times out; the start keeps its
// own deadline so that cancel does not tear the container down.
func desktopViewOpenContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), desktopViewOpenTimeout)
}

// ReleaseUserDesktopView drops the Bot page's watch hold. The desktop stops
// only when nothing is opening it, no login pin is held, no other instance
// is using it, and no admin hold is active.
func (s *Service) ReleaseUserDesktopView(ctx context.Context, tenantID, userID, botID string) error {
	if s == nil {
		return nil
	}
	enabled, err := s.Enabled(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrDisabled
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return ErrNotFound
	}
	instanceID := rec.Bots[index].InstanceID
	epoch := desktopWatchEpoch(ctx)
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	key := desktopViewKey(tenantID, userID)
	// A newer poll already replaced this page. Deleting here would drop that
	// watch, and the stop below would shut the desktop it is opening.
	if epoch > 0 && (s.desktopWatchGen[key] > epoch || s.desktopWatchClosed[key] > epoch) {
		s.mu.Unlock()
		return nil
	}
	if epoch > 0 {
		if s.desktopWatchClosed == nil {
			s.desktopWatchClosed = map[string]int64{}
		}
		s.desktopWatchClosed[key] = epoch
	}
	_, hadView := s.desktopUserView[key]
	if hadView {
		delete(s.desktopUserView, key)
		s.persistDesktopState(tenantID)
	}
	s.mu.Unlock()
	// Open holds this gate until it has published the picture or stopped.
	// A stop that skips the gate races that open and can shut the desktop
	// the next command just started.
	gate := s.desktopUserGate(tenantID, userID)
	gate.Lock()
	defer gate.Unlock()
	// The page can open again while this release waits on the start. A newer
	// generation, or any hold noted after the delete above, still needs it.
	// This release's own closed mark does not count: it just recorded itself.
	if s.desktopWatchReplaced(tenantID, userID, epoch) || s.userDesktopViewActive(tenantID, userID) {
		return nil
	}
	if s.desktopStopBlocked(tenantID, userID, instanceID) {
		return nil
	}
	stopCtx, stopCancel := desktopViewOpenContext(ctx)
	defer stopCancel()
	return s.stopDesktop(stopCtx, tenantID, userID)
}
