package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GUI embed keeps the managed browser off the desktop. The MaClaw preview
// shows the page and the person takes over there. connect_user still attaches
// to a browser the person already started; this flag only changes launches
// that this process owns.
var guiEmbedEnabled atomic.Bool

// EnableGUIEmbed turns on windowless launch for managed profiles. The GUI
// calls it once at startup. Tests and the TUI leave it off, so they keep
// today's visible browser.
func EnableGUIEmbed(on bool) {
	guiEmbedEnabled.Store(on)
}

// GUIEmbedEnabled reports whether managed launches must stay off-screen.
func GUIEmbedEnabled() bool {
	return guiEmbedEnabled.Load()
}

// managedLaunchArgs is the Chrome/Edge command line for a managed profile.
// With GUI embed on, the process is headless so no window pops; the preview
// paints a live view of the same page.
func managedLaunchArgs(port int, userDataDir string) []string {
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + userDataDir,
	}
	if GUIEmbedEnabled() {
		args = append(args,
			"--headless=new",
			"--window-size=1280,800",
			"--hide-scrollbars",
			"--force-device-scale-factor=1",
			"--remote-allow-origins=*",
		)
	}
	return args
}

// BrowserStageNotice tells the GUI to open the in-app page view.
// Kind is session, captcha, login, attention, or confirm.
// Hold means the person must operate the page before the agent continues.
// Item, shop, address, amount, and detail fill the payment confirmation card.
type BrowserStageNotice struct {
	Kind      string
	SessionID string
	URL       string
	Title     string
	Reason    string
	Host      string
	Hold      bool
	Item      string
	Shop      string
	Address   string
	Amount    string
	Detail    string
}

var (
	stageNotifyMu sync.Mutex
	stageNotifyFn func(BrowserStageNotice)
)

// SetBrowserStageNotifier installs the GUI hook. Nil clears it. The hook
// runs on the browser goroutine and must return quickly.
func SetBrowserStageNotifier(fn func(BrowserStageNotice)) {
	stageNotifyMu.Lock()
	stageNotifyFn = fn
	stageNotifyMu.Unlock()
}

func notifyBrowserStage(n BrowserStageNotice) {
	stageNotifyMu.Lock()
	fn := stageNotifyFn
	stageNotifyMu.Unlock()
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[browser] stage notify recovered: %v\n", r)
		}
	}()
	fn(n)
}

var (
	errUserHoldStopped = errors.New("浏览器会话已结束，接管等待取消")
	errUserHoldTimeout = errors.New("用户仍在接管浏览器预览，这一步已停下")
	errUserReleased    = errors.New("用户刚在预览窗交还浏览器。页面可能已变化，先 probe 再重试这一步")
	errUserCancelled   = errors.New("用户取消了预览里的这一步。不要重试刚才的点击，也不要索要密码或验证码。")
)

// userHoldWaitCap bounds how long a tool call waits while the person holds
// the preview. Tests shrink it.
var userHoldWaitCap = 15 * time.Minute

// userHoldGate is process-wide: one managed browser, one person.
type userHoldGate struct {
	mu        sync.Mutex
	held      bool
	reason    string
	cancelled bool
	released  chan struct{}
}

var browserUserHold = &userHoldGate{released: closedHoldChan()}

func closedHoldChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// SetUserHoldsBrowser marks that the person is driving the preview. Mutating
// browser actions wait until this is cleared, then return so the model probes
// instead of clicking a page the person just changed.
func SetUserHoldsBrowser(held bool, reason string) {
	browserUserHold.set(held, reason, false)
}

// CancelUserHold ends a hold because the person cancelled the step.
// The waiting action is told not to retry it and not to ask for a secret.
func CancelUserHold() {
	browserUserHold.set(false, "", true)
}

// UserHoldsBrowser reports the current hold and why it was taken.
func UserHoldsBrowser() (bool, string) {
	browserUserHold.mu.Lock()
	defer browserUserHold.mu.Unlock()
	return browserUserHold.held, browserUserHold.reason
}

func (g *userHoldGate) set(held bool, reason string, cancel bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	reason = strings.TrimSpace(reason)
	if held {
		if g.held {
			if reason != "" {
				g.reason = reason
			}
			return
		}
		g.held = true
		g.reason = reason
		g.cancelled = false
		g.released = make(chan struct{})
		return
	}
	if !g.held {
		return
	}
	g.held = false
	g.reason = ""
	g.cancelled = cancel
	close(g.released)
}

// waitUserHoldRelease blocks while the person holds the preview.
// released is true only when a hold ended during this wait.
func waitUserHoldRelease(ctx context.Context, stop <-chan struct{}) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	browserUserHold.mu.Lock()
	if !browserUserHold.held {
		browserUserHold.mu.Unlock()
		return false, nil
	}
	ch := browserUserHold.released
	browserUserHold.mu.Unlock()
	timer := time.NewTimer(userHoldWaitCap)
	defer timer.Stop()
	select {
	case <-ch:
		return true, nil
	case <-stop:
		return false, errUserHoldStopped
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return false, errUserHoldTimeout
	}
}

// yieldToUserHold waits out a hold and, if the person had the page, tells
// the caller not to perform the action that was queued behind them.
func (s *BrowserAgentSession) yieldToUserHold(ctx context.Context) error {
	var stop <-chan struct{}
	if s != nil {
		stop = s.stopCh
	}
	released, err := waitUserHoldRelease(ctx, stop)
	if err != nil {
		return err
	}
	if !released {
		return nil
	}
	browserUserHold.mu.Lock()
	cancelled := browserUserHold.cancelled
	browserUserHold.mu.Unlock()
	if cancelled {
		return errUserCancelled
	}
	return errUserReleased
}

// handPageToUser opens the preview on the step the person has to finish
// (login, captcha, payment password). While the GUI is embedded, mutating
// work waits until they press 我已完成 or 取消. Secrets are not collected in chat.
func (s *BrowserAgentSession) handPageToUser(reason, url, title string) error {
	sessionID := ""
	if s != nil {
		sessionID = s.ID
		if url == "" && s.session != nil {
			if info, err := s.session.Info(); err == nil && info != nil {
				url = info.URL
				if title == "" {
					title = info.Title
				}
			}
		}
	}
	hold := previewHandoffEnabled()
	if hold {
		SetUserHoldsBrowser(true, reason)
	}
	kind := "attention"
	if reason == "captcha" || reason == "captcha_widget" {
		kind = "captcha"
	} else if reason == "login" {
		kind = "login"
	}
	notifyBrowserStage(BrowserStageNotice{
		Kind:      kind,
		SessionID: sessionID,
		URL:       url,
		Title:     title,
		Reason:    reason,
		Host:      stageHost(url),
		Hold:      hold,
	})
	if !hold {
		return nil
	}
	return s.blockIfUserHolds()
}

// refuseSecretTyping never types the secret. In the preview it hands the page
// to the person and waits. The returned error does not contain the text.
func (s *BrowserAgentSession) refuseSecretTyping() error {
	if err := s.handPageToUser("secret", "", ""); err != nil {
		return err
	}
	return errors.New("not_password_field")
}

func (s *BrowserAgentSession) blockIfUserHolds() error {
	return s.yieldToUserHold(context.Background())
}

func browserStepYieldsToUser(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "navigate", "click", "type", "press", "dialog", "select", "scroll", "hover", "set_files", "refresh", "back", "script":
		return true
	default:
		return false
	}
}

// ViewportInput is one pointer or key event from the preview, in fractions
// of the viewport (0..1) for pointer coordinates.
type ViewportInput struct {
	Kind       string  `json:"kind"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button"`
	ClickCount int     `json:"click_count"`
	DeltaX     float64 `json:"delta_x"`
	DeltaY     float64 `json:"delta_y"`
	Key        string  `json:"key"`
	Text       string  `json:"text"`
}

// ParseViewportInput decodes a preview input payload.
func ParseViewportInput(payload string) (ViewportInput, error) {
	var in ViewportInput
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &in); err != nil {
		return ViewportInput{}, fmt.Errorf("browser preview input: %w", err)
	}
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if in.Kind == "" {
		return ViewportInput{}, fmt.Errorf("browser preview input: missing kind")
	}
	return in, nil
}

// MapViewportFraction maps a 0..1 point onto the CSS viewport. Values outside
// 0..1 are clamped. A non-positive viewport is rejected.
func MapViewportFraction(x, y, width, height float64) (float64, float64, bool) {
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	if x < 0 {
		x = 0
	} else if x > 1 {
		x = 1
	}
	if y < 0 {
		y = 0
	} else if y > 1 {
		y = 1
	}
	return x * width, y * height, true
}

// ViewportJPEG captures the page the agent is driving, for the preview.
func (s *BrowserAgentSession) ViewportJPEG(quality int) (string, int, int, error) {
	if s == nil || s.session == nil {
		return "", 0, 0, fmt.Errorf("browser session not connected")
	}
	return s.session.ViewportJPEG(quality)
}

// ApplyViewportInput forwards one preview event onto the agent's page.
func (s *BrowserAgentSession) ApplyViewportInput(in ViewportInput) error {
	if err := s.requireLiveSession(); err != nil {
		return err
	}
	return s.session.ApplyViewportInput(in)
}
