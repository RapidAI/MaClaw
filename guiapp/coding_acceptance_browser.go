package guiapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func platformBrowserAcceptanceSession(listen acceptanceListen) (acceptanceBrowserSession, string) {
	if testing.Testing() {
		return nil, acceptanceReasonTestNoBrowser
	}
	policy := browser.BrowserPolicy{
		AllowedDomains:             []string{listen.Host},
		AllowCrossOriginNavigation: false,
	}
	sess, err := browser.StartAgentSessionForOwner("coding-acceptance", "", policy, false, browser.SessionModeIsolated)
	if err != nil {
		return nil, "隔离浏览器没有启动：" + err.Error() + "。没有打开 Computer Use。"
	}
	return liveAcceptanceBrowser{sess: sess, host: listen.Host, port: listen.Port}, ""
}

// acceptancePageClicker is the isolated page *browser.BrowserAgentSession
// already implements. Tests pass a fake and do not start Chrome.
type acceptancePageClicker interface {
	Navigate(rawURL string) (*browser.BrowserActionResult, error)
	ProbeForModel(query string) (*browser.BrowserObservation, error)
	CurrentURL() string
	Click(snapshotID, ref, selector string) (*browser.BrowserActionResult, error)
	Press(key string) (*browser.BrowserActionResult, error)
}

type liveAcceptanceBrowser struct {
	sess acceptancePageClicker
	host string
	port int
}

func (b liveAcceptanceBrowser) Navigate(ctx context.Context, rawURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.sess == nil {
		return errors.New("no browser")
	}
	_, err := b.sess.Navigate(rawURL)
	return err
}

func (b liveAcceptanceBrowser) Observe(ctx context.Context) (acceptancePage, error) {
	if err := ctx.Err(); err != nil {
		return acceptancePage{}, err
	}
	if b.sess == nil {
		return acceptancePage{}, errors.New("no browser")
	}
	// ProbeForModel keeps the page text and skips vision, so a password or
	// payment wall is visible without loading a screen model.
	obs, err := b.sess.ProbeForModel("")
	page := acceptancePage{URL: b.sess.CurrentURL()}
	if err != nil || obs == nil {
		if err == nil {
			err = errors.New("no page")
		}
		return page, err
	}
	if strings.TrimSpace(obs.Snapshot.URL) != "" {
		page.URL = obs.Snapshot.URL
	}
	page.Title = obs.Snapshot.Title
	page.Text = obs.Snapshot.PageTextExcerpt
	page.Wall = acceptanceObservationWall(obs)
	return page, nil
}

func (b liveAcceptanceBrowser) Click(ctx context.Context, x, y int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.sess == nil {
		return errors.New("隔离浏览器没有可点击的元素引用")
	}
	obs, err := b.sess.ProbeForModel("")
	if err != nil || obs == nil {
		return errors.New("隔离浏览器没有可点击的元素引用")
	}
	pageURL := strings.TrimSpace(obs.Snapshot.URL)
	if pageURL == "" {
		pageURL = strings.TrimSpace(b.sess.CurrentURL())
	}
	if pageURL == "" || acceptanceNavigationAllowed(b.host, b.port, pageURL) != nil {
		return errors.New("隔离浏览器没有可点击的元素引用")
	}
	el, wall, ok := acceptanceClickTarget(obs, x, y)
	if wall != "" {
		return &acceptanceClickWallError{wall: wall}
	}
	if !ok {
		return errors.New("隔离浏览器没有可点击的元素引用")
	}
	snap := el.SnapshotID
	if snap == "" {
		snap = obs.Snapshot.SnapshotID
	}
	if el.Ref == "" || snap == "" {
		return errors.New("隔离浏览器没有可点击的元素引用")
	}
	_, err = b.sess.Click(snap, el.Ref, "")
	return err
}

// acceptanceClickWallError is a click that landed on a login, captcha,
// password, or payment control. The text does not repeat the control.
type acceptanceClickWallError struct {
	wall string
	key  bool
}

func (e *acceptanceClickWallError) Error() string {
	if e.key {
		return "隔离浏览器拒绝了登录、验证码、密码或支付控件。没有按键。"
	}
	return "隔离浏览器拒绝了登录、验证码、密码或支付控件。没有点击。"
}

// acceptanceClickTarget binds a point to the smallest visible in-viewport
// element. A visible login, captcha, password, or payment control anywhere
// on the page refuses the point, including an unlabeled node inside it and
// a control that is not under the pointer.
func acceptanceClickTarget(obs *browser.BrowserObservation, x, y int) (browser.BrowserElementRef, string, bool) {
	if obs == nil {
		return browser.BrowserElementRef{}, "", false
	}
	if wall := acceptanceObservationWall(obs); wall != "" {
		return browser.BrowserElementRef{}, wall, false
	}
	px, py := float64(x), float64(y)
	found := false
	var best browser.BrowserElementRef
	var bestArea float64
	for _, ref := range obs.Snapshot.Refs {
		if !ref.Visible || !ref.InViewport {
			continue
		}
		box := ref.BoundingBox
		if box.Width <= 0 || box.Height <= 0 {
			continue
		}
		if px < box.X || py < box.Y || px >= box.X+box.Width || py >= box.Y+box.Height {
			continue
		}
		area := box.Width * box.Height
		if !found || area < bestArea {
			best = ref
			bestArea = area
			found = true
		}
	}
	return best, "", found
}

// acceptanceObservationWall is the page wall Click, Press, and Observe share.
// Flags win. Otherwise the last visible secret, login, or pay control wins.
// A hidden password field and a control named 密码学 are not a wall.
func acceptanceObservationWall(obs *browser.BrowserObservation) string {
	if obs == nil {
		return ""
	}
	if wall := acceptanceFlagWall(obs.Snapshot.PageFlags); wall != "" {
		return wall
	}
	wall := ""
	for _, ref := range obs.Snapshot.Refs {
		if !ref.Visible {
			continue
		}
		if why := acceptanceElementWall(ref); why != "" {
			wall = why
		}
	}
	return wall
}

func acceptanceFlagWall(flags browser.BrowserPageFlags) string {
	switch {
	case flags.CaptchaWidget || flags.Captcha || flags.MFA:
		return "captcha"
	case flags.PaymentConfirm:
		return "payment"
	case flags.LoginWall || flags.LoginQR:
		return "login"
	default:
		return ""
	}
}

func acceptanceElementWall(el browser.BrowserElementRef) string {
	if strings.EqualFold(strings.TrimSpace(el.InputType), "password") {
		return "password"
	}
	if wall := acceptancePageNeedsUser(acceptancePage{Title: el.Name, Text: el.Text}); wall != "" {
		return wall
	}
	if browser.IsCheckoutControl(el.Name, el.Selector) || browser.IsCheckoutControl(el.Text, el.Selector) {
		return "payment"
	}
	// The accessible name prefers placeholder over the HTML name. The name,
	// id, and type still sit on the other selectors and the stable key.
	fields := make([]string, 0, 2+len(el.SelectorCandidates))
	if sel := strings.TrimSpace(el.Selector); sel != "" {
		fields = append(fields, sel)
	}
	fields = append(fields, el.SelectorCandidates...)
	if key := strings.TrimSpace(el.StableKey); key != "" {
		fields = append(fields, key)
	}
	for _, field := range fields {
		if wall := acceptanceControlFieldWall(field); wall != "" {
			return wall
		}
	}
	return ""
}

func acceptanceControlFieldWall(field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return ""
	}
	if wall := acceptancePageNeedsUser(acceptancePage{Title: field}); wall != "" {
		return wall
	}
	if browser.IsCheckoutControl("", field) {
		return "payment"
	}
	return ""
}

func (b liveAcceptanceBrowser) Press(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.sess == nil {
		return errors.New("no browser")
	}
	obs, err := b.sess.ProbeForModel("")
	if err != nil || obs == nil {
		return errors.New("隔离浏览器没有留下可核对的页面。没有按键。")
	}
	pageURL := strings.TrimSpace(obs.Snapshot.URL)
	if pageURL == "" {
		pageURL = strings.TrimSpace(b.sess.CurrentURL())
	}
	if pageURL == "" || acceptanceNavigationAllowed(b.host, b.port, pageURL) != nil {
		return errors.New("隔离浏览器没有留在监听地址。没有按键。")
	}
	if wall := acceptanceObservationWall(obs); wall != "" {
		return &acceptanceClickWallError{wall: wall, key: true}
	}
	_, err = b.sess.Press(key)
	return err
}

func (b liveAcceptanceBrowser) HandToUser(ctx context.Context, reason, pageURL, title string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	browser.NotifyStage(browser.BrowserStageNotice{
		Kind:   reason,
		URL:    pageURL,
		Title:  title,
		Host:   b.host,
		Hold:   true,
		Reason: reason,
	})
	return nil
}
