package browser

// Checkout hard stop. Clicking an order-submit or payment control, pressing
// Enter or Space that would submit one, opening a cashier page, or running a
// page script that names one is refused unless the
// user explicitly confirmed this purchase in their own message, with the
// amount. The confirmation is bound to that amount (it must be visible on the
// page at click time), is single-use and expires. Nothing the model writes can
// create one: only the host records it from the user's message text.

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// CheckoutConfirmTTL is how long a user's purchase confirmation stays valid.
const CheckoutConfirmTTL = 10 * time.Minute

// MsgCheckoutBlocked is the refusal the model sees. It must ask the user.
const MsgCheckoutBlocked = "已在下单/支付前停下：这一步会提交订单或付款，必须由用户本人确认。请把商品、数量、收货地址和应付总额（含运费）告诉用户，并请用户回复「确认支付 金额」（例如「确认支付 ¥128.50」）。不要自己点击，也不要换一种方式绕过。"

// checkoutNeedsPersonMessage is the refusal while the preview can show a card.
// The chat phrase remains the path when there is no preview.
func checkoutNeedsPersonMessage() string {
	if previewHandoffEnabled() {
		return "已在下单/支付前停下。不要自己点击。调用 browser(action=\"confirm_order\", item, shop, address, amount)，等用户在确认卡上确认后再 probe 并点击一次。不要在对话里索要支付密码。"
	}
	return MsgCheckoutBlocked
}

var checkoutLabelMarkers = []string{
	"提交订单", "确认订单并支付", "确认付款", "确认支付", "立即付款", "立即支付", "去付款", "去支付", "马上付款", "付款", "支付",
	"一键购买", "一键下单", "提交并支付",
	"place your order", "place order", "pay now", "confirm and pay", "complete purchase", "complete order",
	"submit order", "buy now", "confirm purchase", "proceed to pay", "checkout and pay",
}

// Labels that contain a marker but only open a page that still needs the
// final click, or are not purchases at all.
var checkoutLabelSafe = []string{
	"支付宝账号", "支付方式", "付款方式", "payment method", "支付设置", "待付款", "去结算", "结算",
}

// IsCheckoutControl reports an order-submit or payment control by its label
// or selector.
func IsCheckoutControl(label, selector string) bool {
	// A pay selector commits even when the visible name is 支付方式.
	if selectorHasCheckout(selector) {
		return true
	}
	return labelHasCheckoutMarker(prepareCheckoutLabel(label))
}

// selectorHasCheckout reports a CSS or id fragment that is the pay button
// even when the script never writes the visible label.
func selectorHasCheckout(selector string) bool {
	sel := strings.ToLower(selector)
	for _, m := range []string{"placeyourorder", "place-order", "placeorder", "submitorder", "submit-order", "go-pay", "gopay", "buy-now", "buynow", "pay-btn", "paybtn", "btn-pay"} {
		if strings.Contains(strings.ReplaceAll(sel, "_", "-"), m) || strings.Contains(sel, m) {
			return true
		}
	}
	return false
}

// IsCashierURL reports a payment page: Alipay, WeChat Pay, JD and Taobao
// cashiers, Amazon's final checkout, PayPal.
func IsCashierURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	switch {
	case host == "alipay.com" || strings.HasSuffix(host, ".alipay.com"):
		return strings.HasPrefix(host, "cashier") || strings.HasPrefix(host, "mclient") || strings.HasPrefix(host, "excashier") || strings.Contains(path, "cashier") || strings.Contains(path, "standard/")
	case host == "wx.tenpay.com", host == "pay.weixin.qq.com":
		return true
	case strings.HasPrefix(host, "pay.") && (strings.HasSuffix(host, ".jd.com") || strings.HasSuffix(host, ".taobao.com") || strings.HasSuffix(host, ".tmall.com")):
		return true
	case strings.HasPrefix(host, "cashier.") || strings.HasPrefix(host, "payc.") || strings.HasPrefix(host, "pcashier."):
		return strings.HasSuffix(host, ".jd.com") || strings.HasSuffix(host, ".taobao.com") || strings.HasSuffix(host, ".tmall.com")
	case strings.Contains(host, "amazon."):
		return strings.HasPrefix(path, "/gp/buy/") || strings.HasPrefix(path, "/checkout/") || strings.Contains(path, "/spc/")
	case host == "paypal.com" || strings.HasSuffix(host, ".paypal.com"):
		return strings.HasPrefix(path, "/checkoutnow") || strings.HasPrefix(path, "/webapps/hermes") || strings.HasPrefix(path, "/pay")
	}
	return false
}

// stripCheckoutSafePhrases drops labels that only open a chooser or a
// cart. Longer ones go first so 去结算 does not leave 结算 behind.
func stripCheckoutSafePhrases(s string) string {
	phrases := append([]string(nil), checkoutLabelSafe...)
	sort.Slice(phrases, func(i, j int) bool {
		return len([]rune(phrases[i])) > len([]rune(phrases[j]))
	})
	for _, phrase := range phrases {
		s = strings.ReplaceAll(s, phrase, " ")
	}
	return s
}

// stripSecretPhrases removes password and code wording before the pay-button
// check. Longer phrases go first so 支付密码 does not leave a bare 支付.
func stripSecretPhrases(label string) string {
	for _, phrase := range []string{"支付密码", "请输入密码", "密码", "password", "passcode", "验证码"} {
		label = strings.ReplaceAll(label, phrase, " ")
	}
	return label
}

// pageScriptNamesCheckout reports a script that would click a checkout
// control: it names one of the labels or a cashier host.
func pageScriptNamesCheckout(script string) bool {
	// Judge the action statements, not the whole script text. A script may
	// read innerText or compare a status string containing 提交订单/支付;
	// that is observation, not a purchase click. Only the click/submit
	// statement's own target is evidence.
	for _, action := range checkoutScriptActionSnippets(script) {
		if labelHasCheckoutMarker(prepareCheckoutLabel(action)) {
			return true
		}
		if selectorHasCheckout(normalizeCheckoutText(action)) {
			return true
		}
	}
	return false
}

// checkoutScriptActionSnippets splits a page script into the statements that
// can activate something. State reads and comparisons are deliberately left
// out; a later unrelated click must not inherit their text.
func checkoutScriptActionSnippets(script string) []string {
	parts := strings.FieldsFunc(script, func(r rune) bool { return r == '\n' || r == '\r' || r == ';' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		if part == "" {
			continue
		}
		if strings.Contains(lower, ".click") || strings.Contains(lower, "click(") || strings.Contains(lower, ".submit") || strings.Contains(lower, "requestsubmit") {
			out = append(out, part)
		}
	}
	return out
}

// prepareCheckoutLabel removes password wording and safe phrases. A pay
// marker that contains 立即 or 提交 is masked first, so 立即支付方式 keeps
// 立即支付 while 确认支付方式 does not.
func prepareCheckoutLabel(label string) string {
	s := stripSecretPhrases(normalizeCheckoutText(label))
	s, saved := maskCommitPayMarkers(s)
	s = stripCheckoutSafePhrases(s)
	return strings.TrimSpace(restoreCommitPayMarkers(s, saved))
}

func maskCommitPayMarkers(s string) (string, []string) {
	markers := make([]string, 0, 4)
	for _, m := range checkoutLabelMarkers {
		if strings.Contains(m, "立即") || strings.Contains(m, "提交") {
			markers = append(markers, m)
		}
	}
	sort.Slice(markers, func(i, j int) bool {
		return len([]rune(markers[i])) > len([]rune(markers[j]))
	})
	var saved []string
	for _, m := range markers {
		if !strings.Contains(s, m) {
			continue
		}
		saved = append(saved, m)
		s = strings.ReplaceAll(s, m, string(rune(0xE000+len(saved)-1)))
	}
	return s, saved
}

func restoreCommitPayMarkers(s string, saved []string) string {
	for i, m := range saved {
		s = strings.ReplaceAll(s, string(rune(0xE000+i)), m)
	}
	return s
}

func labelHasCheckoutMarker(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	compact := strings.ReplaceAll(s, " ", "")
	for _, m := range checkoutLabelMarkers {
		if strings.Contains(s, m) || strings.Contains(compact, strings.ReplaceAll(m, " ", "")) {
			return true
		}
	}
	return false
}

func normalizeCheckoutText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// --- confirmations ---

type checkoutConfirmation struct {
	amount  string // canonical, e.g. "128.50"
	expires time.Time
}

var (
	checkoutMu    sync.Mutex
	checkoutStore = map[string]checkoutConfirmation{}
	checkoutNow   = time.Now
)

var (
	checkoutApproveRe = regexp.MustCompile(`(?i)(确认支付|确认付款|确认下单|同意支付|confirm\s*(?:payment|purchase|order|pay))`)
	checkoutAmountRe  = regexp.MustCompile(`(?:[¥￥$€£]|rmb|cny|usd|元)?\s*(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d{1,2}))?\s*(?:元|rmb|cny|usd)?`)
)

// canonicalAmount turns "1,299" / "1299.5" / "¥1299.50" into "1299.50".
func canonicalAmount(intPart, frac string) string {
	intPart = strings.ReplaceAll(intPart, ",", "")
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	for len(frac) < 2 {
		frac += "0"
	}
	return intPart + "." + frac
}

// RecordCheckoutConfirmation stores a purchase confirmation when userText is
// the user's own explicit approval with an amount ("确认支付 ¥128.50",
// "confirm payment 59.99"). Only host code that holds the user's message may
// call it. Returns the canonical amount when one was recorded.
func RecordCheckoutConfirmation(owner, userText string) (string, bool) {
	loc := checkoutApproveRe.FindStringIndex(userText)
	if loc == nil {
		return "", false
	}
	m := checkoutAmountRe.FindStringSubmatch(userText[loc[1]:])
	if m == nil {
		m = checkoutAmountRe.FindStringSubmatch(userText[:loc[0]])
	}
	if m == nil {
		return "", false
	}
	amount := canonicalAmount(m[1], m[2])
	if amount == "0.00" {
		return "", false
	}
	checkoutMu.Lock()
	checkoutStore[strings.TrimSpace(owner)] = checkoutConfirmation{amount: amount, expires: checkoutNow().Add(CheckoutConfirmTTL)}
	checkoutMu.Unlock()
	return amount, true
}

// ClearCheckoutConfirmation drops any pending confirmation for owner.
func ClearCheckoutConfirmation(owner string) {
	checkoutMu.Lock()
	delete(checkoutStore, strings.TrimSpace(owner))
	checkoutMu.Unlock()
}

// pageAmounts returns every canonical amount in the page text.
func pageAmounts(text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range checkoutAmountRe.FindAllStringSubmatch(text, -1) {
		out[canonicalAmount(m[1], m[2])] = true
	}
	return out
}

// consumeCheckoutConfirmation spends the owner's confirmation if it is live
// and its amount is on the page. A mismatch keeps it (the user may still be
// on the cart page) but refuses this click.
func consumeCheckoutConfirmation(owner, pageText string) error {
	owner = strings.TrimSpace(owner)
	checkoutMu.Lock()
	defer checkoutMu.Unlock()
	c, ok := checkoutStore[owner]
	if !ok {
		return policyDenied("%s", MsgCheckoutBlocked)
	}
	if checkoutNow().After(c.expires) {
		delete(checkoutStore, owner)
		return policyDenied("%s", MsgCheckoutBlocked+"（之前的确认已过期）")
	}
	// A strikethrough price or a shipping fee is also on the page. The
	// confirmation has to be the payable total, or a smaller number that
	// happens to be visible would approve the whole order.
	if payable := payableAmount(pageText); payable != "" && payable != c.amount {
		return policyDenied("%s", fmt.Sprintf("已在下单/支付前停下：用户确认的金额 %s 不是页面应付总额 %s。请按应付总额重新确认。", c.amount, payable))
	}
	if !pageAmounts(pageText)[c.amount] {
		return policyDenied("%s", fmt.Sprintf("已在下单/支付前停下：用户确认的金额 %s 不在当前页面上，页面金额可能已变化。请把页面上的实际应付总额告诉用户并重新确认。", c.amount))
	}
	delete(checkoutStore, owner)
	return nil
}

// checkoutPageText reads the visible page text for amount binding.
func (s *BrowserAgentSession) checkoutPageText() string {
	if s == nil || s.session == nil {
		return ""
	}
	out, err := s.session.Eval(`(document.body && document.body.innerText || "").slice(0, 200000)`)
	if err != nil {
		return ""
	}
	return out
}

// SetCheckoutOwner names the person whose own message can confirm a purchase.
// A desktop browser session is keyed by the machine, not by that person.
func (s *BrowserAgentSession) SetCheckoutOwner(owner string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.checkoutOwner = strings.TrimSpace(owner)
	s.mu.Unlock()
}

func (s *BrowserAgentSession) ownerKey() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	owner := s.checkoutOwner
	s.mu.RUnlock()
	if owner != "" {
		return owner
	}
	return s.OwnerID
}

// guardCheckoutClick refuses an unconfirmed checkout click.
func (s *BrowserAgentSession) guardCheckoutClick(ref *BrowserElementRef, selector, text string) error {
	label := text
	if ref != nil {
		label = strings.TrimSpace(ref.Name + " " + ref.Text + " " + text)
	}
	onCashier := false
	if s != nil && s.session != nil {
		if info, err := s.session.Info(); err == nil && info != nil {
			onCashier = IsCashierURL(info.URL)
		}
	}
	if !IsCheckoutControl(label, selector) && !(onCashier && label != "") {
		return nil
	}
	return s.stopForCheckout(consumeCheckoutConfirmation(s.ownerKey(), s.checkoutPageText()))
}

// guardCheckoutNavigate refuses opening a cashier page without confirmation.
func (s *BrowserAgentSession) guardCheckoutNavigate(target string) error {
	if !IsCashierURL(target) {
		return nil
	}
	return s.stopForCheckout(consumeCheckoutConfirmation(s.ownerKey(), s.checkoutPageText()))
}

// guardCheckoutPress refuses Enter or Space that would submit the order.
// The key is not sent. A confirmation that matches the payable total is spent
// and the key then goes through.
func (s *BrowserAgentSession) guardCheckoutPress(key string) error {
	if s == nil || (!checkoutKeySubmits(key) && !pressActivatesFocusedControl(key)) {
		return nil
	}
	if !checkoutPressWouldCommit(key, s.ReadCheckoutPage()) {
		return nil
	}
	return s.stopForCheckout(consumeCheckoutConfirmation(s.ownerKey(), s.checkoutPageText()))
}

// guardCheckoutScript refuses a script that names a checkout control, runs
// on a cashier page, or presses Enter or Space on the order.
func (s *BrowserAgentSession) guardCheckoutScript(script string) error {
	onCashier := false
	if s != nil && s.session != nil {
		if info, err := s.session.Info(); err == nil && info != nil {
			onCashier = IsCashierURL(info.URL)
		}
	}
	if !pageScriptNamesCheckout(script) && !onCashier {
		if s == nil || s.session == nil || !scriptPressCommits(script, s.ReadCheckoutPage()) {
			return nil
		}
	}
	return s.stopForCheckout(consumeCheckoutConfirmation(s.ownerKey(), s.checkoutPageText()))
}

// scriptPressCommits reports a page-script press() that would submit the order.
// press("Enter") calls form.requestSubmit, so focus can be a field, not the button.
var pageScriptPressRe = regexp.MustCompile(`(?i)\bpress\s*\(\s*['"]([^'"]+)['"]`)

func scriptPressCommits(script string, page CheckoutPage) bool {
	for _, m := range pageScriptPressRe.FindAllStringSubmatch(script, -1) {
		key := strings.TrimSpace(m[1])
		if strings.TrimSpace(key) == "" && strings.Contains(m[1], " ") {
			key = "space"
		} else if strings.ContainsAny(key, " \t+") {
			continue
		}
		if checkoutScriptPressWouldCommit(key, page) {
			return true
		}
	}
	return false
}

// PendingCheckoutConfirmation reports the live confirmed amount for owner.
func PendingCheckoutConfirmation(owner string) (string, bool) {
	checkoutMu.Lock()
	defer checkoutMu.Unlock()
	c, ok := checkoutStore[strings.TrimSpace(owner)]
	if !ok || checkoutNow().After(c.expires) {
		return "", false
	}
	return c.amount, true
}
