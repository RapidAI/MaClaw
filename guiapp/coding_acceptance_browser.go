package guiapp

import (
	"context"
	"errors"
	"net/url"
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
	// Checkout and navigation policy return a blocked or ask result with a nil
	// error, and the page stays where it was. A nil error is not an open page.
	result, err := b.sess.Navigate(rawURL)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// The result names the document this action left behind. A later probe can
	// still show the previous page, so a disallowed result address is already
	// a refusal. An empty result address stays that probe's job.
	if err == nil {
		if fault := acceptanceActionResultURL(b.host, b.port, result, false); fault != nil {
			return fault
		}
	}
	// unchanged means the document did not change. A click can still have
	// landed; a navigation only landed when that document is the one requested.
	if err == nil && acceptanceBrowserStatus(result) == "unchanged" {
		if fault := acceptanceUnchangedNavigate(rawURL, acceptanceObservedURL(result, b.sess.CurrentURL())); fault != nil {
			return fault
		}
	}
	return acceptanceNavigateResultError(result, err)
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
		return acceptanceWebClickMiss()
	}
	obs, err := b.sess.ProbeForModel("")
	if err != nil || obs == nil {
		return acceptanceWebClickMiss()
	}
	pageURL := strings.TrimSpace(obs.Snapshot.URL)
	if pageURL == "" {
		pageURL = strings.TrimSpace(b.sess.CurrentURL())
	}
	if pageURL == "" || acceptanceNavigationAllowed(b.host, b.port, pageURL) != nil {
		return acceptanceWebClickMiss()
	}
	el, wall, ok := acceptanceClickTarget(obs, x, y)
	if wall != "" {
		return &acceptanceClickWallError{wall: wall}
	}
	if !ok {
		return acceptanceWebClickMiss()
	}
	snap := el.SnapshotID
	if snap == "" {
		snap = obs.Snapshot.SnapshotID
	}
	if el.Ref == "" || snap == "" {
		return acceptanceWebClickMiss()
	}
	result, err := b.sess.Click(snap, el.Ref, "")
	if err == nil {
		if fault := acceptanceActionResultURL(b.host, b.port, result, true); fault != nil {
			return fault
		}
	}
	return acceptanceBrowserActionError(result, err, false)
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

// acceptanceWebDriveFault is a web failure the driver already classified.
// The runner keeps detail and reason. Error() does not name the control.
type acceptanceWebDriveFault struct {
	detail string
	reason string
}

func (e *acceptanceWebDriveFault) Error() string {
	if e == nil {
		return ""
	}
	return e.reason
}

func acceptanceWebClickMiss() error {
	return &acceptanceWebDriveFault{detail: "click_failed", reason: "隔离浏览器没有可点击的元素引用"}
}

func acceptanceWebFailure(err error, key bool) (string, string) {
	if detail, reason, ok := acceptanceClassifiedWebError(err); ok {
		return detail, reason
	}
	if key {
		return "press_failed", "隔离浏览器没有按键。没有打开 Computer Use。"
	}
	return "click_failed", "隔离浏览器没有点到可点击的元素。没有打开 Computer Use。"
}

func acceptanceWebNavigateFailure(err error) (string, string) {
	if detail, reason, ok := acceptanceClassifiedWebError(err); ok {
		return detail, reason
	}
	return "navigate_failed", "隔离浏览器没有打开页面。没有打开 Computer Use。"
}

func acceptanceClassifiedWebError(err error) (string, string, bool) {
	var fault *acceptanceWebDriveFault
	if errors.As(err, &fault) && fault != nil && fault.detail != "" {
		return fault.detail, fault.reason, true
	}
	return "", "", false
}

// acceptanceBrowserActionError treats only a completed browser action as
// success. Click and Press report captcha, QR, and checkout refusals as a
// result with a nil error. Those did not send input, so they are not a pass.
// The display text is not copied: it can name the control.
func acceptanceBrowserActionError(result *browser.BrowserActionResult, err error, key bool) error {
	if err != nil {
		return err
	}
	if result != nil {
		switch strings.TrimSpace(result.Status) {
		case "ok", "", "unchanged":
			return nil
		case "blocked", "ask":
			return &acceptanceClickWallError{wall: acceptanceActionResultWall(result), key: key}
		}
	}
	if key {
		return &acceptanceWebDriveFault{detail: "press_failed", reason: "隔离浏览器没有按键。没有打开 Computer Use。"}
	}
	return acceptanceWebClickMiss()
}

// acceptanceNavigateResultError uses the same completed-status rule as a click.
// The display text is not the user sentence. Any other driver error becomes
// the fixed navigate sentence, so a cashier URL or a typed value is not copied.
func acceptanceNavigateResultError(result *browser.BrowserActionResult, err error) error {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if _, ok := acceptanceClickWall(err); ok {
			return err
		}
		if _, _, ok := acceptanceClassifiedWebError(err); ok {
			return err
		}
		return &acceptanceWebDriveFault{detail: "navigate_failed", reason: "隔离浏览器没有打开页面。没有打开 Computer Use。"}
	}
	if result != nil {
		switch strings.TrimSpace(result.Status) {
		case "ok", "", "unchanged":
			return nil
		case "blocked", "ask":
			return &acceptanceClickWallError{wall: acceptanceActionResultWall(result)}
		}
	}
	return &acceptanceWebDriveFault{detail: "navigate_failed", reason: "隔离浏览器没有打开页面。没有打开 Computer Use。"}
}

func acceptanceBrowserStatus(result *browser.BrowserActionResult) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(result.Status)
}

func acceptanceObservedURL(result *browser.BrowserActionResult, current string) string {
	if result != nil && result.Data != nil {
		if pageURL, ok := result.Data["url"].(string); ok && strings.TrimSpace(pageURL) != "" {
			return strings.TrimSpace(pageURL)
		}
	}
	return strings.TrimSpace(current)
}

// acceptanceActionResultURL refuses a completed action whose own result names
// a page the allowlist does not admit. Status ok on another path of this
// host is a redirect and stays a success. Click status unchanged is still a
// sent click when that address is allowed. The sentence does not include it.
func acceptanceActionResultURL(host string, port int, result *browser.BrowserActionResult, acted bool) error {
	pageURL, ok := acceptanceResultURL(result)
	if !ok {
		return nil
	}
	switch acceptanceBrowserStatus(result) {
	case "ok", "", "unchanged":
	default:
		return nil
	}
	if acceptanceNavigationAllowed(host, port, pageURL) == nil {
		return nil
	}
	if acted {
		return &acceptanceWebDriveFault{
			detail: "navigation_refused",
			reason: "动作之后页面离开了监听地址。没有继续操作，也没有打开 Computer Use。",
		}
	}
	return &acceptanceWebDriveFault{
		detail: "navigation_refused",
		reason: "隔离浏览器没有打开请求的页面。没有打开 Computer Use。",
	}
}

func acceptanceResultURL(result *browser.BrowserActionResult) (string, bool) {
	if result == nil || result.Data == nil {
		return "", false
	}
	pageURL, ok := result.Data["url"].(string)
	pageURL = strings.TrimSpace(pageURL)
	if !ok || pageURL == "" {
		return "", false
	}
	return pageURL, true
}

// acceptanceUnchangedNavigate refuses a navigation whose document stayed put.
// The sentence does not include either address. Userinfo is not the same
// document: acceptanceSameDocument does not compare it.
func acceptanceUnchangedNavigate(requested, actual string) error {
	actual = strings.TrimSpace(actual)
	if strings.ContainsAny(actual, "@\\\r\n\t") || acceptanceURLHasEncodedTrick(actual) {
		return &acceptanceWebDriveFault{
			detail: "navigation_refused",
			reason: "隔离浏览器没有打开请求的页面。没有打开 Computer Use。",
		}
	}
	if acceptanceSameDocument(requested, actual) {
		return nil
	}
	return &acceptanceWebDriveFault{
		detail: "navigation_refused",
		reason: "隔离浏览器没有打开请求的页面。没有打开 Computer Use。",
	}
}

func acceptanceSameDocument(requested, actual string) bool {
	req, err1 := url.Parse(strings.TrimSpace(requested))
	got, err2 := url.Parse(strings.TrimSpace(actual))
	if err1 != nil || err2 != nil || req == nil || got == nil {
		return false
	}
	if !strings.EqualFold(req.Scheme, got.Scheme) {
		return false
	}
	if !strings.EqualFold(req.Hostname(), got.Hostname()) {
		return false
	}
	if acceptanceURLPort(req) != acceptanceURLPort(got) {
		return false
	}
	if acceptanceDocumentPath(req) != acceptanceDocumentPath(got) {
		return false
	}
	return req.RawQuery == got.RawQuery
}

func acceptanceURLPort(u *url.URL) string {
	if u == nil {
		return ""
	}
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

func acceptanceDocumentPath(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func acceptanceActionResultWall(result *browser.BrowserActionResult) string {
	if result == nil {
		return "login"
	}
	blob := strings.ToLower(result.Status + "\n" + result.Detail + "\n" + result.Display)
	switch {
	case strings.Contains(blob, "captcha") || strings.Contains(blob, "验证码"):
		return "captcha"
	case strings.Contains(blob, "checkout") || strings.Contains(blob, "支付") || strings.Contains(blob, "付款"):
		return "payment"
	default:
		return "login"
	}
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
	flags := obs.Snapshot.PageFlags
	// LoginWall is also set for a hidden password input plus a word in the
	// page body. Hidden inputs are not in the refs. The flag counts when a
	// visible entry field is on the page.
	if flags.LoginWall && !acceptanceVisibleLoginEntry(obs.Snapshot.Refs) {
		flags.LoginWall = false
	}
	if wall := acceptanceFlagWall(flags); wall != "" {
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

// acceptanceFlagWall reads the probe flags that name a control on the page:
// a visible captcha widget, a login form with an entry field, or a card
// checkout. Captcha, MFA, and LoginQR are matches against the page body.
// A sentence that mentions a code is not one of those controls.
func acceptanceFlagWall(flags browser.BrowserPageFlags) string {
	switch {
	case flags.CaptchaWidget:
		return "captcha"
	case flags.PaymentConfirm:
		return "payment"
	case flags.LoginWall:
		return "login"
	default:
		return ""
	}
}

func acceptanceElementWall(el browser.BrowserElementRef) string {
	if strings.EqualFold(strings.TrimSpace(el.InputType), "password") {
		return "password"
	}
	name := acceptanceControlCaption(el.Name)
	if wall := acceptancePageNeedsUser(acceptancePage{Title: name}); wall != "" {
		return wall
	}
	// The selector is structural. The visible name and text count only when
	// they are a short caption. A long quoted value is not a pay-button id.
	if browser.IsCheckoutControl(name, acceptanceCheckoutField(el.Selector)) || browser.IsCheckoutControl(acceptanceControlCaption(el.Text), "") {
		return "payment"
	}
	// The accessible name prefers placeholder over the HTML name. The name,
	// id, and type still sit on the other selectors and the stable key.
	// Those fields are not scanned as a paragraph.
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
	// A selector or stable key is structure plus one caption. The joined
	// string is not a paragraph: a long button sentence and an id such as
	// login-form stay ordinary. The caption is the stable key's own field.
	if wall := acceptanceAutocompleteWall(field); wall != "" {
		return wall
	}
	if acceptancePasswordAttribute(field) {
		return "password"
	}
	if browser.IsCheckoutControl("", acceptanceCheckoutField(field)) {
		return "payment"
	}
	return acceptanceStableKeyCaptionWall(field)
}

// acceptancePasswordAttribute matches the probe's name and type attributes
// and the stable key's HTML name. The quoted value is exactly password or
// otp, so passwordless and an id of otp stay ordinary controls. A placeholder
// can hide that HTML name; the attribute still counts.
func acceptancePasswordAttribute(field string) bool {
	// The quoted name belongs to the control, not to an ancestor in the selector.
	attrs := acceptanceSelectorAttrs(field)
	for _, want := range []string{"password", "otp"} {
		if acceptanceQuotedAttrEquals(attrs, "name", want) || acceptanceQuotedAttrEquals(attrs, "type", want) {
			return true
		}
	}
	parts := acceptanceStableKeyParts(field)
	if len(parts) < 6 {
		return false
	}
	htmlName := strings.TrimSpace(parts[2])
	return strings.EqualFold(htmlName, "password") || strings.EqualFold(htmlName, "otp")
}

// acceptanceVisibleLoginEntry reports a visible account field. The probe's
// LoginWall selector matches hidden inputs, so a menu that only mentions
// 登录 does not confirm the flag.
func acceptanceVisibleLoginEntry(refs []browser.BrowserElementRef) bool {
	for _, ref := range refs {
		if ref.Visible && acceptanceLoginEntryRef(ref) {
			return true
		}
	}
	return false
}

func acceptanceLoginEntryRef(ref browser.BrowserElementRef) bool {
	switch strings.ToLower(strings.TrimSpace(ref.InputType)) {
	case "password", "email", "tel":
		return true
	}
	// The tag is the control's own tag. A button whose selector mentions an
	// ancestor named email is not an entry field.
	if tag := acceptanceControlTag(ref); tag != "" && !acceptanceFormControlTag(tag) {
		return false
	}
	fields := make([]string, 0, 2+len(ref.SelectorCandidates))
	if sel := strings.TrimSpace(ref.Selector); sel != "" {
		fields = append(fields, sel)
	}
	fields = append(fields, ref.SelectorCandidates...)
	if key := strings.TrimSpace(ref.StableKey); key != "" {
		fields = append(fields, key)
	}
	for _, field := range fields {
		if acceptanceLoginEntryField(field) {
			return true
		}
	}
	return false
}

func acceptanceLoginEntryField(field string) bool {
	if parts := acceptanceStableKeyParts(field); parts != nil {
		if len(parts) < 6 || !acceptanceFormControlTag(parts[0]) {
			return false
		}
		return acceptanceLoginNameToken(parts[1]) || acceptanceLoginNameToken(parts[2])
	}
	// form[name="email"] > input names the form. The input is the last compound.
	subject := acceptanceSelectorSubject(field)
	if !acceptanceSubjectIsEntry(subject) {
		return false
	}
	for _, want := range []string{"username", "email", "tel"} {
		if acceptanceQuotedAttrEquals(subject, "autocomplete", want) {
			return true
		}
	}
	name, ok := acceptanceQuotedAttr(subject, "name")
	return ok && acceptanceLoginNameToken(name)
}

func acceptanceControlTag(ref browser.BrowserElementRef) string {
	if tag := strings.ToLower(strings.TrimSpace(ref.Tag)); tag != "" {
		return tag
	}
	parts := acceptanceStableKeyParts(ref.StableKey)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

// acceptanceSelectorAttrs is the control's own compound. A stable key has no
// CSS combinator. A selector contributes only its last compound.
func acceptanceSelectorAttrs(field string) string {
	if strings.Contains(field, "[") || strings.Contains(field, ">") {
		return acceptanceSelectorSubject(field)
	}
	return field
}

func acceptanceSubjectIsEntry(subject string) bool {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return false
	}
	tag := acceptanceSelectorTag(subject)
	if tag == "" {
		return true
	}
	return acceptanceFormControlTag(tag)
}

func acceptanceSelectorTag(subject string) string {
	subject = strings.ToLower(subject)
	i := 0
	for i < len(subject) && subject[i] >= 'a' && subject[i] <= 'z' {
		i++
	}
	if i == 0 {
		return ""
	}
	if i < len(subject) {
		switch subject[i] {
		case '#', '.', '[', ':':
		default:
			return ""
		}
	}
	return subject[:i]
}

// acceptanceSelectorSubject returns the rightmost compound. Combinators inside
// quotes, brackets, or parentheses stay part of that compound.
func acceptanceSelectorSubject(selector string) string {
	selector = strings.TrimSpace(selector)
	quote := byte(0)
	bracket, paren := 0, 0
	last := 0
	for i := 0; i < len(selector); i++ {
		c := selector[i]
		if quote != 0 {
			if c == quote && (i == 0 || selector[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case '>', '+', '~':
			if bracket == 0 && paren == 0 {
				last = i + 1
			}
		case ' ', '\t', '\n', '\r':
			if bracket != 0 || paren != 0 {
				continue
			}
			j := i
			for j < len(selector) && (selector[j] == ' ' || selector[j] == '\t' || selector[j] == '\n' || selector[j] == '\r') {
				j++
			}
			if j < len(selector) && selector[j] != '>' && selector[j] != '+' && selector[j] != '~' {
				last = j
			}
			if j > i {
				i = j - 1
			}
		}
	}
	return strings.TrimSpace(selector[last:])
}

func acceptanceFormControlTag(tag string) bool {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "input", "textarea", "select":
		return true
	default:
		return false
	}
}

// acceptanceLoginNameToken reports an account-entry id or HTML name.
// The value is one word, or several words split by a non-letter and by a
// camelCase boundary. user_email and userEmail match. headphone does not.
// tel, telephone, and cellphone are whole words, so template stays ordinary.
// A trailing digit is an index (email2) and is not part of the word.
func acceptanceLoginNameToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if acceptanceAccountToken(strings.ToLower(value)) {
		return true
	}
	for _, part := range acceptanceIdentWords(value) {
		if acceptanceAccountToken(part) {
			return true
		}
	}
	return false
}

func acceptanceAccountToken(part string) bool {
	part = acceptanceTrimIndex(part)
	switch part {
	case "tel", "email", "phone", "mobile", "username", "telephone", "cellphone":
		return true
	default:
		return false
	}
}

func acceptanceTrimIndex(part string) string {
	end := len(part)
	for end > 0 && part[end-1] >= '0' && part[end-1] <= '9' {
		end--
	}
	if end == 0 {
		return ""
	}
	return part[:end]
}

// acceptanceIdentWords splits an HTML id or name. userEmail and HTMLEmail
// become two words before the letters are compared.
func acceptanceIdentWords(value string) []string {
	var b strings.Builder
	rs := []rune(value)
	for i, r := range rs {
		if i > 0 && acceptanceIdentBoundary(rs, i) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.FieldsFunc(strings.ToLower(b.String()), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

func acceptanceIdentBoundary(rs []rune, i int) bool {
	prev, cur := rs[i-1], rs[i]
	if cur < 'A' || cur > 'Z' {
		return false
	}
	if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
		return true
	}
	// HTMLEmail: the last capital of an acronym starts the next word.
	if prev < 'A' || prev > 'Z' || i+1 >= len(rs) {
		return false
	}
	next := rs[i+1]
	return next >= 'a' && next <= 'z'
}

// acceptanceCheckoutField keeps the control's own selector structure. A stable
// key contributes tag, id, HTML name, and role. A CSS selector contributes its
// last compound, so an ancestor id such as #pay-btn is not the button inside
// it. A quoted value longer than a caption is omitted so a paragraph that
// mentions pay-btn is not a pay button.
func acceptanceCheckoutField(field string) string {
	field = strings.TrimSpace(field)
	if parts := acceptanceStableKeyParts(field); len(parts) >= 4 {
		return strings.Join(parts[:4], "|")
	}
	return acceptanceDropLongQuotes(acceptanceSelectorSubject(field))
}

func acceptanceDropLongQuotes(field string) string {
	var b strings.Builder
	b.Grow(len(field))
	for i := 0; i < len(field); {
		if field[i] != '"' && field[i] != '\'' {
			b.WriteByte(field[i])
			i++
			continue
		}
		quote := field[i]
		j := i + 1
		for j < len(field) && field[j] != quote {
			j++
		}
		if j >= len(field) {
			b.WriteString(field[i:])
			break
		}
		value := field[i+1 : j]
		if len([]rune(value)) <= acceptanceCaptionRunes {
			b.WriteString(field[i : j+1])
		} else {
			b.WriteByte(quote)
			b.WriteByte(quote)
		}
		i = j + 1
	}
	return b.String()
}

func acceptanceQuotedAttrEquals(field, attr, want string) bool {
	got, ok := acceptanceQuotedAttr(field, attr)
	return ok && got == strings.ToLower(want)
}

func acceptanceQuotedAttr(field, attr string) (string, bool) {
	lower := strings.ToLower(field)
	attr = strings.ToLower(attr)
	for i := 0; i < len(lower); i++ {
		if lower[i] != '[' {
			continue
		}
		rest := strings.TrimLeft(lower[i+1:], " \t")
		if !strings.HasPrefix(rest, attr) {
			continue
		}
		after := rest[len(attr):]
		if after == "" || (after[0] != '=' && after[0] != ' ' && after[0] != '\t') {
			continue
		}
		after = strings.TrimLeft(after, " \t")
		if !strings.HasPrefix(after, "=") {
			continue
		}
		after = strings.TrimLeft(after[1:], " \t")
		if after == "" || (after[0] != '"' && after[0] != '\'') {
			continue
		}
		quote := after[0]
		value := after[1:]
		end := strings.IndexByte(value, quote)
		if end >= 0 {
			return value[:end], true
		}
	}
	return "", false
}

// acceptanceStableKeyParts parses tag|id|name|role|caption|nth. A CSS
// selector stays with the attribute and checkout checks.
func acceptanceStableKeyParts(field string) []string {
	if strings.Contains(field, "[") || strings.Contains(field, "]") {
		return nil
	}
	if strings.Count(field, "|") < 5 {
		return nil
	}
	return strings.Split(field, "|")
}

func acceptanceStableKeyCaptionWall(field string) string {
	parts := acceptanceStableKeyParts(field)
	if len(parts) < 6 {
		return ""
	}
	end := len(parts) - 1
	if !acceptanceKeyNth(strings.TrimSpace(parts[end])) {
		end = len(parts)
	}
	if end <= 4 {
		return ""
	}
	caption := acceptanceControlCaption(strings.Join(parts[4:end], "|"))
	return acceptancePageNeedsUser(acceptancePage{Title: caption})
}

func acceptanceKeyNth(part string) bool {
	if part == "" {
		return false
	}
	for _, r := range part {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// acceptanceAutocompleteWall reads credential and card autocomplete tokens
// the probe appends as a selector. The value is the whole quoted attribute,
// so a sentence that mentions the token is not a field. "one-time offer" is
// not "one-time-code". current-password and new-password stay password fields
// when the visible caption says something else.
func acceptanceAutocompleteWall(field string) string {
	field = acceptanceSelectorAttrs(field)
	switch {
	case acceptanceQuotedAttrEquals(field, "autocomplete", "one-time-code"):
		return "captcha"
	case acceptanceQuotedAttrEquals(field, "autocomplete", "current-password"),
		acceptanceQuotedAttrEquals(field, "autocomplete", "new-password"):
		return "password"
	case acceptanceQuotedAttrEquals(field, "autocomplete", "cc-csc"),
		acceptanceQuotedAttrEquals(field, "autocomplete", "cc-number"),
		acceptanceQuotedAttrEquals(field, "autocomplete", "cc-exp"):
		return "payment"
	default:
		return ""
	}
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
		return &acceptanceWebDriveFault{detail: "press_failed", reason: "隔离浏览器没有留下可核对的页面。没有按键。"}
	}
	pageURL := strings.TrimSpace(obs.Snapshot.URL)
	if pageURL == "" {
		pageURL = strings.TrimSpace(b.sess.CurrentURL())
	}
	if pageURL == "" || acceptanceNavigationAllowed(b.host, b.port, pageURL) != nil {
		return &acceptanceWebDriveFault{detail: "navigation_refused", reason: "隔离浏览器没有留在监听地址。没有按键。"}
	}
	if wall := acceptanceObservationWall(obs); wall != "" {
		return &acceptanceClickWallError{wall: wall, key: true}
	}
	result, err := b.sess.Press(key)
	if err == nil {
		if fault := acceptanceActionResultURL(b.host, b.port, result, true); fault != nil {
			return fault
		}
	}
	return acceptanceBrowserActionError(result, err, true)
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
