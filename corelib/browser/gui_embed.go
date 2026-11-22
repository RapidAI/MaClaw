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

// GUI embed mirrors the managed browser in the assistant preview. The process
// stays headed. connect_user still attaches to a browser the person already
// started; this flag only changes launches that this process owns.
var guiEmbedEnabled atomic.Bool

// EnableGUIEmbed marks that the assistant preview is mirroring the managed
// browser. The GUI calls it once at startup. Tests and the TUI leave it off.
// The browser process stays headed either way. Headless Chrome is a different
// browser to page scripts, and sites that score that environment refuse the
// session after a login that itself succeeded. The preview paints screenshots,
// so the headed window is moved off the desktop instead of being shown there.
func EnableGUIEmbed(on bool) {
	guiEmbedEnabled.Store(on)
}

// GUIEmbedEnabled reports whether the assistant preview is active.
func GUIEmbedEnabled() bool {
	return guiEmbedEnabled.Load()
}

// nativeOcclusionFeature is the Windows tracker that marks a headed window
// hidden once its rectangle leaves every monitor. The page then reports
// visibilityState hidden, and a later click calls Page.bringToFront.
const nativeOcclusionFeature = "CalculateNativeWinOcclusion"

// managedParkGap is the space kept between the parked window's right edge
// and the virtual screen. The window's own width is subtracted as well, so a
// window wider than any fixed margin still clears every monitor.
const managedParkGap = 64

// managedLaunchArgs is the Chrome/Edge command line for a managed profile.
// The process is headed. A headless process sets navigator.webdriver, has no
// window chrome, an empty plugin list, and a software GL renderer; risk
// checks treat that as automation and reject the signed request. The preview
// paints screenshots of this window. It is not a reason to switch the process
// to headless. AutomationControlled stays off so the webdriver bit remains
// the normal false. No page script is injected to lie about it.
func managedLaunchArgs(port int, userDataDir string) []string {
	// Only the loopback origin this process dials may attach. A wildcard lets
	// a page script open its own debugger socket to the same port.
	origins := fmt.Sprintf("http://127.0.0.1:%d,http://localhost:%d", port, port)
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-allow-origins=" + origins,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-blink-features=AutomationControlled",
		"--user-data-dir=" + userDataDir,
	}
	if GUIEmbedEnabled() {
		// Outer window size. Clicks use the CSS viewport from layout metrics,
		// so the OS scale factor is left alone.
		args = append(args, "--window-size=1280,800")
		if managedWindowShouldPark(userDataDir) {
			// Chrome clamps this back onto a monitor at startup. The park loop
			// moves the HWND after that. The feature flag is what keeps the
			// page visible once the HWND is off the virtual screen.
			x, y := parkedWindowPosition()
			args = append(args,
				fmt.Sprintf("--window-position=%d,%d", x, y),
				"--disable-features="+nativeOcclusionFeature,
			)
		}
	}
	return args
}

// managedWindowShouldPark is the headed Chrome this process owns while the
// preview is on. The person's daily profile is not moved.
func managedWindowShouldPark(userDataDir string) bool {
	if !GUIEmbedEnabled() || strings.TrimSpace(userDataDir) == "" {
		return false
	}
	key := managedBrowserKey(userDataDir)
	return key == managedBrowserKey(persistentProfileDir()) || key == managedBrowserKey(debugProfileDir())
}

// parkedWindowLeft is the left edge that puts a window of the given width
// fully to the left of the virtual screen. virtualX is that screen's origin.
func parkedWindowLeft(virtualX, width int) int {
	if width < 1 {
		width = 1280
	}
	return virtualX - width - managedParkGap
}

// parkedOriginForScreen is the launch position for the 1280-wide outer
// window. Chrome clamps it back onto a monitor; the park loop then uses the
// real window width.
func parkedOriginForScreen(virtualX, virtualY int) (int, int) {
	return parkedWindowLeft(virtualX, 1280), virtualY
}

// rectIntersectsVirtualScreen reports whether a window rectangle meets any
// monitor. A zero-area rectangle does not.
func rectIntersectsVirtualScreen(left, top, right, bottom, virtualX, virtualY, virtualW, virtualH int) bool {
	if right <= left || bottom <= top || virtualW <= 0 || virtualH <= 0 {
		return false
	}
	return left < virtualX+virtualW && right > virtualX && top < virtualY+virtualH && bottom > virtualY
}

// commandLineDisablesNativeOcclusion reports a Chrome command line that
// turned the Windows occlusion tracker off. The feature has to be its own
// disable-features token. A profile path that merely contains the name is
// not that switch.
func commandLineDisablesNativeOcclusion(commandLine string) bool {
	lower := strings.ToLower(commandLine)
	want := strings.ToLower(nativeOcclusionFeature)
	const key = "--disable-features="
	rest := lower
	for {
		i := strings.Index(rest, key)
		if i < 0 {
			return false
		}
		rest = rest[i+len(key):]
		val := rest
		if end := strings.IndexAny(val, " \t\"'"); end >= 0 {
			val = val[:end]
		}
		for _, part := range strings.Split(val, ",") {
			if part == want {
				return true
			}
		}
	}
}

// commandLinesDisableNativeOcclusion is true only when every process line
// has the switch. One process without it still marks an off-screen window
// hidden, so the caller relaunches instead of parking that process.
func commandLinesDisableNativeOcclusion(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		if !commandLineDisablesNativeOcclusion(line) {
			return false
		}
	}
	return true
}

// BrowserStageNotice tells the GUI to open the in-app page view.
// Kind is session, captcha, login, attention, confirm, secret, confirm_clear, or secret_clear.
// Hold means the person must operate the page before the agent continues.
// Item, shop, address, amount, and detail fill the payment confirmation card.
type BrowserStageNotice struct {
	Kind             string
	SessionID        string
	URL              string
	Title            string
	Reason           string
	Host             string
	Hold             bool
	Item             string
	Shop             string
	Address          string
	Amount           string
	Detail           string
	SecretMethod     string
	SecretDigits     int
	SecretSubmission string
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

// NotifyStage emits the existing agent-browser-stage notice. Acceptance
// calls it so a person can see the isolated page and stop it.
func NotifyStage(n BrowserStageNotice) {
	notifyBrowserStage(n)
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
	// ErrPreviewHandBack is that same hand-back. The browser process and the
	// CDP session are still connected. The action that was waiting was not
	// sent. Callers report it as a continuation: probe, then keep going in
	// this turn. A cancel, a timeout, and a stopped session are other errors.
	ErrPreviewHandBack = errUserReleased
	errUserCancelled   = errors.New("用户取消了预览里的这一步。不要重试刚才的点击，也不要索要密码或验证码。")
)

// IsPreviewHandBack reports a preview hand-back. The session is still
// connected. Cancel, timeout, and a stopped session are not hand-backs.
func IsPreviewHandBack(err error) bool {
	return errors.Is(err, ErrPreviewHandBack)
}

// userHoldWaitCap bounds how long a tool call waits while the person holds
// the preview. Tests shrink it.
var userHoldWaitCap = 15 * time.Minute

// holdLease is one hold. cancelled is set before ch is closed and is never
// written again. A later hold is a new lease, so it cannot turn this cancel
// into a hand-back, or this hand-back into a cancel.
type holdLease struct {
	ch        chan struct{}
	cancelled bool
}

// userHoldGate is process-wide: one managed browser, one person.
type userHoldGate struct {
	mu     sync.Mutex
	held   bool
	reason string
	lease  *holdLease
}

var browserUserHold = &userHoldGate{lease: closedHoldLease()}

func closedHoldLease() *holdLease {
	ch := make(chan struct{})
	close(ch)
	return &holdLease{ch: ch}
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

// DismissBrowserPreview cancels a checkout or secret card that is still
// waiting. A takeover with no card is handed back. The hold closed here is
// the one that was already up: a hold started while the cards were closing,
// and a secret the host is still typing, stay up.
func DismissBrowserPreview() {
	ch := activeHoldCh()
	filling := SecretFillOwnsHold()
	_ = ResolveCheckoutCard(false)
	_ = ResolveSecretCard("", false)
	if !filling {
		browserUserHold.releaseIfCurrent(ch, false)
	}
}

func activeHoldCh() chan struct{} {
	browserUserHold.mu.Lock()
	defer browserUserHold.mu.Unlock()
	if !browserUserHold.held || browserUserHold.lease == nil {
		return nil
	}
	return browserUserHold.lease.ch
}

// UserHoldsBrowser reports the current hold and why it was taken.
func UserHoldsBrowser() (bool, string) {
	browserUserHold.mu.Lock()
	defer browserUserHold.mu.Unlock()
	return browserUserHold.held, browserUserHold.reason
}

// set returns the lease that closes when this hold ends. A release that
// lands before the waiter selects is still visible on that lease. Starting
// a hold does not change the previous lease.
func (g *userHoldGate) set(held bool, reason string, cancel bool) *holdLease {
	g.mu.Lock()
	defer g.mu.Unlock()
	reason = strings.TrimSpace(reason)
	if held {
		if g.held {
			if reason != "" {
				g.reason = reason
			}
			return g.lease
		}
		g.held = true
		g.reason = reason
		g.lease = &holdLease{ch: make(chan struct{})}
		return g.lease
	}
	if !g.held {
		return g.lease
	}
	g.held = false
	g.reason = ""
	g.lease.cancelled = cancel
	close(g.lease.ch)
	return g.lease
}

// releaseIfCurrent closes ch only when it is still the active hold. The
// outcome is stored on that lease. A newer hold keeps its own lease.
func (g *userHoldGate) releaseIfCurrent(ch chan struct{}, cancel bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ch == nil || !g.held || g.lease == nil || g.lease.ch != ch {
		return
	}
	g.held = false
	g.reason = ""
	g.lease.cancelled = cancel
	close(g.lease.ch)
}

// holdLeaseError is the outcome both waiters read from the lease they
// captured. The close happens before that read, so a later hold cannot
// change it.
func holdLeaseError(lease *holdLease) error {
	if lease == nil {
		return nil
	}
	if lease.cancelled {
		return errUserCancelled
	}
	return errUserReleased
}

// waitUserHoldRelease blocks while the person holds the preview.
// The lease is nil when no hold ended during this wait.
func waitUserHoldRelease(ctx context.Context, stop <-chan struct{}) (*holdLease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	browserUserHold.mu.Lock()
	if !browserUserHold.held || browserUserHold.lease == nil {
		browserUserHold.mu.Unlock()
		return nil, nil
	}
	lease := browserUserHold.lease
	browserUserHold.mu.Unlock()
	timer := time.NewTimer(userHoldWaitCap)
	defer timer.Stop()
	select {
	case <-lease.ch:
		return lease, nil
	case <-stop:
		return nil, errUserHoldStopped
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, errUserHoldTimeout
	}
}

// yieldToUserHold waits out a hold and, if the person had the page, tells
// the caller not to perform the action that was queued behind them.
func (s *BrowserAgentSession) yieldToUserHold(ctx context.Context) error {
	var stop <-chan struct{}
	if s != nil {
		stop = s.stopCh
	}
	lease, err := waitUserHoldRelease(ctx, stop)
	if err != nil {
		return err
	}
	return holdLeaseError(lease)
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
	var lease *holdLease
	if hold {
		// Keep this lease. 我已完成 can close it before the select below
		// runs, and a later "is it held?" check would miss that hand-back.
		// The cancel bit stays on the lease the waiter captured.
		lease = browserUserHold.set(true, reason, false)
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
	var stop <-chan struct{}
	if s != nil {
		stop = s.stopCh
	}
	return waitArmedHold(lease, stop)
}

// waitArmedHold waits on the lease returned when this hold started.
func waitArmedHold(lease *holdLease, stop <-chan struct{}) error {
	if lease == nil || lease.ch == nil {
		return nil
	}
	timer := time.NewTimer(userHoldWaitCap)
	defer timer.Stop()
	select {
	case <-lease.ch:
	case <-stop:
		return errUserHoldStopped
	case <-timer.C:
		return errUserHoldTimeout
	}
	return holdLeaseError(lease)
}

// refuseSecretTyping never types the model's secret. In the preview the model
// must call submit_secret so the person types on the card. The returned error
// does not contain the text. Without the preview the historical code stands.
func (s *BrowserAgentSession) refuseSecretTyping() error {
	if previewHandoffEnabled() {
		return errUseSubmitSecret
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
	// Gen is the preview bar's caret generation. It advances when the press
	// focuses a different field. Text from the previous field carries the
	// old number and is not written into the new one.
	Gen int `json:"gen"`
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
