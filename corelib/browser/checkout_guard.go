package browser

// Checkout hard stop. Clicking an order-submit or payment control, opening a
// cashier page, or running a page script that names one is refused unless the
// user explicitly confirmed this purchase in their own message, with the
// amount. The confirmation is bound to that amount (it must be visible on the
// page at click time), is single-use and expires. Nothing the model writes can
// create one: only the host records it from the user's message text.

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CheckoutConfirmTTL is how long a user's purchase confirmation stays valid.
const CheckoutConfirmTTL = 10 * time.Minute

// MsgCheckoutBlocked is the refusal the model sees. It must ask the user.
const MsgCheckoutBlocked = "已在下单/支付前停下：这一步会提交订单或付款，必须由用户本人确认。请把商品、数量、收货地址和应付总额（含运费）告诉用户，并请用户回复「确认支付 金额」（例如「确认支付 ¥128.50」）。不要自己点击，也不要换一种方式绕过。"

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
	l := normalizeCheckoutText(label)
	sel := strings.ToLower(selector)
	if l != "" {
		for _, safe := range checkoutLabelSafe {
			if strings.Contains(l, safe) && !strings.Contains(l, "提交") && !strings.Contains(l, "立即") {
				return false
			}
		}
		compact := strings.ReplaceAll(l, " ", "")
		for _, m := range checkoutLabelMarkers {
			if strings.Contains(l, m) || strings.Contains(compact, strings.ReplaceAll(m, " ", "")) {
				return true
			}
		}
	}
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

// pageScriptNamesCheckout reports a script that would click a checkout
// control: it names one of the labels or a cashier host.
func pageScriptNamesCheckout(script string) bool {
	s := normalizeCheckoutText(script)
	for _, m := range checkoutLabelMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	for _, h := range []string{"alipay.com", "tenpay.com", "pay.weixin", "cashier.", "/gp/buy/", "placeyourorder", "place-order", "submitorder", "buy-now", "buynow"} {
		if strings.Contains(s, h) {
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

func (s *BrowserAgentSession) ownerKey() string {
	if s == nil {
		return ""
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
	return consumeCheckoutConfirmation(s.ownerKey(), s.checkoutPageText())
}

// guardCheckoutNavigate refuses opening a cashier page without confirmation.
func (s *BrowserAgentSession) guardCheckoutNavigate(target string) error {
	if !IsCashierURL(target) {
		return nil
	}
	return consumeCheckoutConfirmation(s.ownerKey(), s.checkoutPageText())
}

// guardCheckoutScript refuses a script that names a checkout control or runs
// on a cashier page.
func (s *BrowserAgentSession) guardCheckoutScript(script string) error {
	onCashier := false
	if s != nil && s.session != nil {
		if info, err := s.session.Info(); err == nil && info != nil {
			onCashier = IsCashierURL(info.URL)
		}
	}
	if !pageScriptNamesCheckout(script) && !onCashier {
		return nil
	}
	return consumeCheckoutConfirmation(s.ownerKey(), s.checkoutPageText())
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
