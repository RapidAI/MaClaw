package browser

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// CheckoutTarget is the control a click, an Enter, or an app step would
// activate. The decision uses its accessible name, its visible text, the
// form or page around it, and the page URL. It is not tied to one shop.
type CheckoutTarget struct {
	Name    string
	Text    string
	Context string
	URL     string
	Key     string
	Role    string
	// Submits is true for a click, a script that clicks, or a key that
	// sends Enter. Typing into a field without Enter is not a submit.
	Submits bool
}

// Commits reports that this target is an order-submit or payment commit.
func (t CheckoutTarget) Commits() bool {
	if !t.Submits {
		return false
	}
	script := t.Name + "\n" + t.Text
	if pageScriptNamesCheckout(script) && (strings.Contains(script, ".click") || strings.Contains(script, "submit") || strings.Contains(script, "Enter")) {
		return true
	}
	label := strings.TrimSpace(t.Name + " " + t.Text)
	if t.Key != "" && !checkoutKeySubmits(t.Key) {
		return false
	}
	if checkoutKeySubmits(t.Key) {
		if checkoutTextField(label) && !IsCheckoutControl(label, "") {
			return false
		}
		if IsCheckoutControl(label, "") {
			return true
		}
		if checkoutSafeLabel(label) {
			return false
		}
		if strings.TrimSpace(label) == "" && (checkoutOrderContext(t.Context) || checkoutCommitURL(t.URL)) {
			return true
		}
		return checkoutCommitVerb(label) && (checkoutOrderContext(t.Context) || checkoutCommitURL(t.URL))
	}
	if IsCheckoutControl(label, "") {
		return true
	}
	if checkoutSafeLabel(label) {
		return false
	}
	return checkoutCommitVerb(label) && (checkoutOrderContext(t.Context) || checkoutCommitURL(t.URL))
}

func checkoutKeySubmits(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, "_", "")
	k = strings.ReplaceAll(k, " ", "")
	switch k {
	case "", "return", "enter", "kpenter", "\n", "\r":
		return k != ""
	}
	return strings.Contains(k, "enter") || strings.Contains(k, "return")
}

func checkoutSafeLabel(label string) bool {
	l := normalizeCheckoutText(label)
	if l == "" {
		return false
	}
	for _, safe := range checkoutLabelSafe {
		if strings.Contains(l, safe) && !strings.Contains(l, "提交") && !strings.Contains(l, "立即") {
			return true
		}
	}
	return false
}

func checkoutTextField(label string) bool {
	n := normalizeCheckoutText(label)
	if n == "" {
		return false
	}
	for _, m := range []string{"搜索", "search", "备注", "留言", "comment", "邮箱", "email", "密码", "password", "手机", "phone", "地址", "address", "姓名"} {
		if strings.Contains(n, m) {
			return true
		}
	}
	return false
}

func checkoutCommitVerb(label string) bool {
	l := normalizeCheckoutText(label)
	if l == "" || checkoutSafeLabel(l) {
		return false
	}
	for _, m := range []string{"提交", "下单", "付款", "支付", "pay", "place order", "submit", "buy now", "confirm purchase"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

func checkoutOrderContext(text string) bool {
	n := normalizeCheckoutText(text)
	if n == "" {
		return false
	}
	if strings.Contains(n, "登录") || strings.Contains(n, "sign in") || strings.Contains(n, "log in") {
		if !strings.Contains(n, "应付") && !strings.Contains(n, "提交订单") && !strings.Contains(n, "order total") {
			return false
		}
	}
	for _, m := range []string{"应付", "实付", "提交订单", "确认订单", "place your order", "order total", "order summary", "收货地址", "shipping address", "收银台"} {
		if strings.Contains(n, m) {
			return true
		}
	}
	return false
}

// checkoutCommitURL is a payment or order-commit address by its path, not by
// a shop's host name. Known cashier hosts still count.
func checkoutCommitURL(raw string) bool {
	if IsCashierURL(raw) {
		return true
	}
	blob, ok := parseCheckoutURL(raw)
	if !ok {
		return false
	}
	for _, m := range []string{"/checkout", "/cashier", "/payment", "/pay/", "submit-order", "submitorder", "place-order", "placeorder", "confirm-order", "order/confirm", "trade/confirm"} {
		if strings.Contains(blob, m) {
			return true
		}
	}
	return false
}

func parseCheckoutURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.Contains(raw, "://") {
		return "", false
	}
	return strings.ToLower(raw), true
}

// CheckoutReady reports a live confirmation whose amount is on the page.
// It does not spend the confirmation.
func CheckoutReady(owner, pageText string) bool {
	owner = strings.TrimSpace(owner)
	checkoutMu.Lock()
	defer checkoutMu.Unlock()
	c, ok := checkoutStore[owner]
	if !ok || checkoutNow().After(c.expires) {
		return false
	}
	return pageAmounts(pageText)[c.amount]
}

// TakeCheckoutConfirmation spends a matching confirmation.
func TakeCheckoutConfirmation(owner, pageText string) error {
	return consumeCheckoutConfirmation(owner, pageText)
}

// CheckoutConfirmQuestion is the sentence the person sees before the click.
func CheckoutConfirmQuestion(control string, preselected []string) string {
	control = strings.TrimSpace(control)
	if control == "" {
		control = "提交订单或付款"
	}
	if runes := []rune(control); len(runes) > 40 {
		control = string(runes[:40])
	}
	q := "这一步会提交订单或付款（" + control + "）。"
	if len(preselected) > 0 {
		q += "购物车里有这次没有加入的商品：" + strings.Join(preselected, "、") + "。请确认是否一起下单。"
	}
	q += "请核对商品、数量、收货地址和应付总额后，回复「确认支付 金额」，例如「确认支付 ¥128.50」。"
	return q
}

// IsCartAdd reports a control that puts a product in the cart.
func IsCartAdd(name, text string) bool {
	l := normalizeCheckoutText(name + " " + text)
	for _, m := range []string{"加入购物车", "加购物车", "加入购物袋", "add to cart", "add to bag"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// CartItemFrom picks a short product name out of the page around an add click.
func CartItemFrom(context, fallback string) string {
	for _, line := range strings.Split(context, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || IsCartAdd(line, "") || IsCheckoutControl(line, "") {
			continue
		}
		runes := []rune(line)
		if len(runes) > 40 {
			line = string(runes[:40])
		}
		return line
	}
	return strings.TrimSpace(fallback)
}

var runCart = struct {
	mu   sync.Mutex
	adds map[string][]string
}{adds: map[string][]string{}}

func cartKey(item string) string {
	return normalizeCheckoutText(item)
}

// NoteRunCartAdd remembers a product this run put in the cart.
func NoteRunCartAdd(runID, item string) {
	runID, item = strings.TrimSpace(runID), strings.TrimSpace(item)
	if runID == "" || item == "" {
		return
	}
	key := cartKey(item)
	runCart.mu.Lock()
	defer runCart.mu.Unlock()
	for _, have := range runCart.adds[runID] {
		if cartKey(have) == key {
			return
		}
	}
	runCart.adds[runID] = append(runCart.adds[runID], item)
}

// ClearRunCart drops the products recorded for a finished run.
func ClearRunCart(runID string) {
	runCart.mu.Lock()
	delete(runCart.adds, strings.TrimSpace(runID))
	runCart.mu.Unlock()
}

// PreselectedCartItems are selected cart rows this run did not add.
func PreselectedCartItems(runID string, selected []string) []string {
	runCart.mu.Lock()
	added := append([]string(nil), runCart.adds[strings.TrimSpace(runID)]...)
	runCart.mu.Unlock()
	have := map[string]bool{}
	for _, item := range added {
		if key := cartKey(item); key != "" {
			have[key] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, item := range selected {
		item = strings.TrimSpace(item)
		key := cartKey(item)
		if item == "" || key == "" || have[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

// CheckoutPage is what the desktop can see without naming a shop.
type CheckoutPage struct {
	URL      string   `json:"url"`
	Context  string   `json:"context"`
	Focused  string   `json:"focused"`
	Selected []string `json:"selected"`
}

// checkoutPageExpr reads the focused control, a slice of the page, and
// checked rows. The row lookup is generic: a checked box and the text of
// its row.
const checkoutPageExpr = `(function(){
  function text(n){ return ((n && (n.innerText || n.textContent)) || '').replace(/\s+/g,' ').trim(); }
  var el = document.activeElement;
  var focused = '';
  if (el) { focused = (el.getAttribute && (el.getAttribute('aria-label') || el.getAttribute('title'))) || text(el) || (el.value || ''); }
  var body = text(document.body).slice(0, 4000);
  var selected = [];
  var nodes = document.querySelectorAll('[aria-checked="true"], input[type="checkbox"]:checked');
  for (var i = 0; i < nodes.length && selected.length < 20; i++) {
    var row = nodes[i].closest && nodes[i].closest('li, tr, [role="row"]');
    var line = text(row || nodes[i].parentElement || nodes[i]).slice(0, 80);
    if (line) selected.push(line);
  }
  return JSON.stringify({url: location.href, context: body, focused: focused.slice(0, 200), selected: selected});
})()`

// ParseCheckoutPage reads the JSON from checkoutPageExpr.
func ParseCheckoutPage(raw string) CheckoutPage {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CheckoutPage{}
	}
	var page CheckoutPage
	if json.Unmarshal([]byte(raw), &page) != nil {
		return CheckoutPage{}
	}
	return page
}

// CheckoutElementExpr is the accessible name and text at a viewport point.
func CheckoutElementExpr(x, y float64) string {
	return `(function(){
    var el = document.elementFromPoint(` + trimFloat(x) + `,` + trimFloat(y) + `);
    if (!el) return JSON.stringify({name:'', text:''});
    function text(n){ return ((n && (n.innerText || n.textContent)) || '').replace(/\s+/g,' ').trim(); }
    var name = (el.getAttribute && (el.getAttribute('aria-label') || el.getAttribute('title') || el.getAttribute('alt'))) || '';
    var node = el;
    for (var i = 0; i < 4 && node; i++) {
      if (!name) name = (node.getAttribute && node.getAttribute('aria-label')) || '';
      if (text(node).length > 0 && text(node).length < 80) { el = node; break; }
      node = node.parentElement;
    }
    return JSON.stringify({name: String(name).slice(0, 200), text: text(el).slice(0, 200)});
  })()`
}

func trimFloat(n float64) string {
	s := strings.TrimRight(strings.TrimRight(sprintfFloat(n), "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func sprintfFloat(n float64) string {
	return strings.TrimSpace(strings.ReplaceAll(jsonNumber(n), "\n", ""))
}

func jsonNumber(n float64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// ParseCheckoutElement reads the JSON from CheckoutElementExpr.
func ParseCheckoutElement(raw string) (name, text string) {
	var out struct {
		Name string `json:"name"`
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &out) != nil {
		return "", ""
	}
	return strings.TrimSpace(out.Name), strings.TrimSpace(out.Text)
}

// ReadCheckoutPage reads the open page. An unconnected session returns an
// empty page rather than an error, so a missing read is not a purchase.
func (s *BrowserAgentSession) ReadCheckoutPage() CheckoutPage {
	if s == nil || s.session == nil {
		return CheckoutPage{}
	}
	page := CheckoutPage{URL: s.CurrentURL()}
	raw, err := s.session.EvalTimeout(checkoutPageExpr, time.Second)
	if err != nil {
		return page
	}
	parsed := ParseCheckoutPage(raw)
	if parsed.URL == "" {
		parsed.URL = page.URL
	}
	return parsed
}

// ReadCheckoutElement reads the accessible name and text at a viewport point.
func (s *BrowserAgentSession) ReadCheckoutElement(x, y float64) (string, string) {
	if s == nil || s.session == nil {
		return "", ""
	}
	raw, err := s.session.EvalTimeout(CheckoutElementExpr(x, y), time.Second)
	if err != nil {
		return "", ""
	}
	return ParseCheckoutElement(raw)
}
