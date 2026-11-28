package botmgmt

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

// Typed desktop picture. running is the only state with a frame. asleep is
// a stopped container (or a screen that was never up). display_stopped is
// a container whose display the supervisor idle-stopped.
const (
	DesktopStateRunning        = "running"
	DesktopStateAsleep         = "asleep"
	DesktopStateDisplayStopped = "display_stopped"
	// DesktopStateUnavailable is a failed state read. It is not asleep and
	// not a stopped display.
	DesktopStateUnavailable = "unavailable"
)

type viewMemoKey struct{}

type viewMemo struct {
	once  sync.Once
	state string
	novnc string
	known bool
}

// withViewMemo makes every ask on this context share one desktopd read.
func withViewMemo(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, viewMemoKey{}, &viewMemo{})
}

// DesktopViewState is the optional typed look-up of a screen that is
// already up. It must never start, wake, or pin the desktop.
type DesktopViewState interface {
	DesktopViewState(ctx context.Context, tenantID, userID string) (state, novncURL string, err error)
}

// liveDesktopPicture drops a page whenever the screen is not running.
// A remembered URL of a stopped display is not a picture.
func liveDesktopPicture(url, state string) (string, string) {
	url = strings.TrimSpace(url)
	switch state {
	case DesktopStateAsleep, DesktopStateDisplayStopped, DesktopStateUnavailable:
		return "", state
	case DesktopStateRunning:
		if url == "" {
			return "", DesktopStateAsleep
		}
		return url, DesktopStateRunning
	default:
		// No typed state. Do not invent one from a remembered page.
		return "", ""
	}
}

// DesktopPicture is the 电脑-tab answer: a page only while the screen is
// running, and the typed state otherwise. wake is the human 唤醒 control
// (wake reason human-wake). A watch that is not a wake never starts the
// desktop. A remembered page is dropped when the screen is asleep or the
// display has stopped, so the viewer cannot keep a black frame.
func (s *Service) DesktopPicture(ctx context.Context, tenantID, userID, botID string, wake bool) (string, bool, string, error) {
	ctx = withViewMemo(ctx)
	var url string
	var control bool
	var err error
	if wake {
		url, control, err = s.WakeDesktopView(ctx, tenantID, userID, botID)
	} else {
		url, control, err = s.HoldDesktopView(ctx, tenantID, userID, botID)
	}
	if desktopbot.IsDesktopAsleep(err) {
		return "", false, DesktopStateAsleep, nil
	}
	if err != nil {
		return "", false, "", err
	}
	// A wake that returned a page started the desktop with human-wake
	// (or found it already running). Trust that page. Asking again can
	// still see the display coming up and would throw the new page away.
	if wake {
		url, state := liveDesktopPicture(url, DesktopStateRunning)
		return url, control && url != "", state, nil
	}
	desktopUser := s.desktopUserForWatch(ctx, tenantID, userID, botID)
	// Hold already dropped a rejected gate and is waiting out the recover
	// gap. Asking again here would retry on every poll.
	if strings.TrimSpace(url) == "" && s.desktopRecoverCooling(tenantID, desktopUser) {
		return "", false, DesktopStateUnavailable, nil
	}
	reported, live, known := s.askDesktopViewState(ctx, tenantID, desktopUser)
	if !known {
		if strings.TrimSpace(url) == "" {
			return "", false, DesktopStateAsleep, nil
		}
		return url, control, DesktopStateRunning, nil
	}
	if reported == DesktopStateRunning && strings.TrimSpace(live) != "" {
		// live is the raw address desktopd published — a node.invalid or
		// loopback origin the person's browser cannot dial. The browser
		// needs the gated Hub path; rememberDesktopView also retargets
		// issued tokens when a restart rotated the gate bearer.
		s.rememberDesktopView(tenantID, desktopUser, live)
		if gated := s.desktopViewURL(tenantID, desktopUser); gated != "" {
			url = gated
		}
	} else if reported != DesktopStateRunning {
		url = ""
	}
	url, state := liveDesktopPicture(url, reported)
	return url, control && url != "", state, nil
}

func (s *Service) desktopUserForWatch(ctx context.Context, tenantID, userID, botID string) string {
	if s == nil {
		return userID
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return userID
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 {
		return userID
	}
	return desktopUserForBot(userID, rec.Bots[index])
}

func (s *Service) askDesktopViewState(ctx context.Context, tenantID, desktopUser string) (string, string, bool) {
	read := func() (string, string, bool) {
		return s.readDesktopViewState(ctx, tenantID, desktopUser)
	}
	var memo *viewMemo
	if ctx != nil {
		memo, _ = ctx.Value(viewMemoKey{}).(*viewMemo)
	}
	if memo == nil {
		return read()
	}
	memo.once.Do(func() {
		memo.state, memo.novnc, memo.known = read()
	})
	return memo.state, memo.novnc, memo.known
}

func (s *Service) readDesktopViewState(ctx context.Context, tenantID, desktopUser string) (string, string, bool) {
	if s == nil || s.Desktop == nil {
		return "", "", false
	}
	st, ok := s.Desktop.(DesktopViewState)
	if !ok || st == nil {
		return "", "", false
	}
	state, novnc, err := st.DesktopViewState(ctx, tenantID, desktopUser)
	if err != nil {
		if desktopbot.IsDesktopAsleep(err) {
			return DesktopStateAsleep, "", true
		}
		// A coded upstream rejection (the tunnel's 502 after a gate restart)
		// is a definite miss, not a blink. Callers drop the cached page and
		// wait out the existing recover gap instead of asking every poll.
		if desktopViewRejected(err) {
			return DesktopStateUnavailable, "", true
		}
		return "", "", false
	}
	if strings.TrimSpace(state) == "" {
		if strings.TrimSpace(novnc) == "" {
			state = DesktopStateAsleep
		} else {
			state = DesktopStateRunning
		}
	}
	return state, novnc, true
}

// desktopViewRejected reports a definite upstream miss. A transport blink is
// not one: only a typed status (the gate tunnel's 502) or a coded asleep
// error means the cached bearer can no longer be served.
func desktopViewRejected(err error) bool {
	if err == nil {
		return false
	}
	if desktopbot.IsDesktopAsleep(err) {
		return true
	}
	var statusErr *upstream.StatusError
	return errors.As(err, &statusErr)
}

// desktopPictureRunning reports whether a remembered page is still a live
// frame. A desktop that cannot answer the question is treated as up so
// older fakes keep their current watch behavior.
func (s *Service) desktopPictureRunning(ctx context.Context, tenantID, desktopUser string) bool {
	state, novnc, known := s.askDesktopViewState(ctx, tenantID, desktopUser)
	if !known {
		return true
	}
	return state == DesktopStateRunning && strings.TrimSpace(novnc) != ""
}
