package browser

import (
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Checkout confirmation card. The person reviews goods, address, and the
// amount in the preview, then confirms. The model cannot approve the card.
// A confirmed card records the same one-use amount the checkout guard already
// requires. It does not itself click pay.

var (
	errCheckoutConfirmed  = errors.New("用户已在预览的确认卡上确认这笔支付。这次点击没有发出。先 probe，核对页面上的应付总额仍是确认过的金额，再点击一次支付。不要自己再写确认。")
	errCheckoutCancelled  = errors.New("用户取消了这笔支付。不要再点击支付，也不要换一种方式提交。")
	errCheckoutNoAmount   = errors.New("确认卡缺少应付金额。把商品、收货地址和应付总额交给 browser(action=\"confirm_order\")，不要自己确认。")
	errCheckoutHandoffOff = errors.New("确认卡只在助手预览里打开")
	errCheckoutNone       = errors.New("没有待确认的订单")
)

// CheckoutCard is the purchase the person is asked to confirm.
type CheckoutCard struct {
	Item    string
	Shop    string
	Address string
	Amount  string // canonical, such as "9.98"
	Detail  string
	Host    string
	PageURL string
}

type checkoutWait struct {
	card     CheckoutCard
	owner    string
	done     chan struct{}
	holdCh   chan struct{}
	gen      uint64
	closed   bool
	approved bool
	err      error
}

var (
	checkoutWaitMu      sync.Mutex
	checkoutWaitCurrent *checkoutWait
	checkoutCardGen     uint64
)

// previewHandoffEnabled is true only when the GUI is showing the in-app page.
// Tests and the TUI leave it off, so checkout stays an immediate refusal.
func previewHandoffEnabled() bool {
	if !GUIEmbedEnabled() {
		return false
	}
	stageNotifyMu.Lock()
	on := stageNotifyFn != nil
	stageNotifyMu.Unlock()
	return on
}

// OfferCheckoutCard opens the confirmation card and waits for the person.
// On confirm it records the amount for ownerKey and returns that amount.
// The click that was waiting is not performed.
func (s *BrowserAgentSession) OfferCheckoutCard(card CheckoutCard) (string, error) {
	card = sanitizeCheckoutCard(card)
	if card.Amount == "" {
		return "", errCheckoutNoAmount
	}
	if !previewHandoffEnabled() {
		return "", errCheckoutHandoffOff
	}
	// One install lock so a secret card cannot pass its own check in the gap.
	previewCardMu.Lock()
	if secretCardPending() {
		previewCardMu.Unlock()
		return "", errSecretBusy
	}
	checkoutWaitMu.Lock()
	if checkoutWaitCurrent != nil {
		w := checkoutWaitCurrent
		checkoutWaitMu.Unlock()
		previewCardMu.Unlock()
		return waitCheckoutCard(s, w)
	}
	checkoutCardGen++
	w := &checkoutWait{card: card, owner: "", done: make(chan struct{}), gen: checkoutCardGen}
	if s != nil {
		w.owner = s.ownerKey()
	}
	checkoutWaitCurrent = w
	if lease := browserUserHold.set(true, "confirm", false); lease != nil {
		w.holdCh = lease.ch
	}
	checkoutWaitMu.Unlock()
	previewCardMu.Unlock()

	sessionID := ""
	if s != nil {
		sessionID = s.ID
	}
	notifyBrowserStage(BrowserStageNotice{
		Kind:      "confirm",
		SessionID: sessionID,
		URL:       card.PageURL,
		Host:      card.Host,
		Reason:    "confirm",
		Item:      card.Item,
		Shop:      card.Shop,
		Address:   card.Address,
		Amount:    card.Amount,
		Detail:    card.Detail,
	})
	return waitCheckoutCard(s, w)
}

func waitCheckoutCard(s *BrowserAgentSession, w *checkoutWait) (string, error) {
	var stop <-chan struct{}
	if s != nil {
		stop = s.stopCh
	}
	timer := time.NewTimer(userHoldWaitCap)
	defer timer.Stop()
	defer releaseCheckoutCardNotice(w)
	for {
		// The person's decision wins when it is already stored. select would
		// otherwise pick a ready session-stop or the deadline at random and
		// drop a confirmation that can still be spent.
		if amount, err, closed := checkoutClosedResult(w); closed {
			return amount, err
		}
		select {
		case <-w.done:
		case <-stop:
			finishCheckoutWait(w, false, errUserHoldStopped)
		case <-timer.C:
			finishCheckoutWait(w, false, errUserHoldTimeout)
		}
	}
}

func checkoutClosedResult(w *checkoutWait) (string, error, bool) {
	if w == nil {
		return "", errCheckoutNone, true
	}
	checkoutWaitMu.Lock()
	defer checkoutWaitMu.Unlock()
	if !w.closed {
		return "", nil, false
	}
	if w.err != nil {
		return "", w.err, true
	}
	if !w.approved {
		return "", errCheckoutCancelled, true
	}
	return w.card.Amount, nil, true
}

// finishCheckoutWait reports whether this call closed the wait. Approval
// stores the amount in that same critical section. A wait that already
// closed — cancel, stop, or timeout won — does not gain a confirmation.
func finishCheckoutWait(w *checkoutWait, approved bool, err error) bool {
	if w == nil {
		return false
	}
	checkoutWaitMu.Lock()
	defer checkoutWaitMu.Unlock()
	if w.closed {
		return false
	}
	if approved {
		if _, ok := RecordCheckoutConfirmation(w.owner, "确认支付 ¥"+w.card.Amount); !ok {
			return false
		}
	}
	w.closed = true
	w.approved = approved
	w.err = err
	close(w.done)
	if checkoutWaitCurrent == w {
		checkoutWaitCurrent = nil
	}
	// Release only while this wait is still the latest card. A cancel tells
	// queued actions not to retry the pay click.
	if checkoutWaitCurrent == nil {
		browserUserHold.releaseIfCurrent(w.holdCh, !approved && (err == nil || errors.Is(err, errUserHoldStopped)))
	}
	return true
}

// ResolveCheckoutCard records the person's decision. Approve stores the
// amount from the card the GUI showed, not from anything the model wrote.
// The store happens only when this call is the one that closes the card.
func ResolveCheckoutCard(approve bool) error {
	checkoutWaitMu.Lock()
	w := checkoutWaitCurrent
	checkoutWaitMu.Unlock()
	if w == nil {
		return errCheckoutNone
	}
	if !approve {
		finishCheckoutWait(w, false, nil)
		return nil
	}
	if finishCheckoutWait(w, true, nil) {
		return nil
	}
	checkoutWaitMu.Lock()
	closed := w.closed
	checkoutWaitMu.Unlock()
	if !closed {
		return errCheckoutNoAmount
	}
	return errCheckoutNone
}

// stopForCheckout shows a card built from the page when the preview is on.
// A page with no payable amount tells the model to call confirm_order.
// Without the preview, the original refusal stands.
func (s *BrowserAgentSession) stopForCheckout(blocked error) error {
	if blocked == nil {
		return nil
	}
	if !previewHandoffEnabled() || s == nil || s.session == nil {
		return blocked
	}
	url := ""
	if info, err := s.session.Info(); err == nil && info != nil {
		url = info.URL
	}
	card := CheckoutCardFromPage(s.checkoutPageText(), url)
	if card.Amount == "" {
		return errCheckoutNoAmount
	}
	if _, err := s.OfferCheckoutCard(card); err != nil {
		return err
	}
	return errCheckoutConfirmed
}

func sanitizeCheckoutCard(card CheckoutCard) CheckoutCard {
	card.Item = clipCheckoutField(card.Item, 160)
	card.Shop = clipCheckoutField(card.Shop, 80)
	card.Address = clipCheckoutField(card.Address, 180)
	card.Detail = clipCheckoutField(card.Detail, 180)
	if strings.Contains(card.Detail, "密码") || strings.Contains(strings.ToLower(card.Detail), "password") {
		card.Detail = ""
	}
	card.Host = clipCheckoutField(card.Host, 120)
	card.Amount = parseCheckoutAmount(card.Amount)
	if card.Host == "" {
		card.Host = stageHost(card.PageURL)
	}
	return card
}

func clipCheckoutField(s string, n int) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	r := []rune(s)
	if n > 0 && len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

func parseCheckoutAmount(raw string) string {
	raw = strings.TrimSpace(raw)
	if amount := payableAmount(raw); amount != "" {
		return amount
	}
	matches := checkoutAmountRe.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return ""
	}
	// A bare list of prices keeps the last one. Payable totals are written
	// after the crossed-out price.
	m := matches[len(matches)-1]
	amount := canonicalAmount(m[1], m[2])
	if amount == "0.00" {
		return ""
	}
	return amount
}

// releaseCheckoutCardNotice drops the card after this wait ends.
// A newer card has a higher generation, so this clear does not wipe it.
// The stage notifier must not call OfferCheckoutCard or ResolveCheckoutCard.
func releaseCheckoutCardNotice(w *checkoutWait) {
	if w == nil {
		return
	}
	checkoutWaitMu.Lock()
	defer checkoutWaitMu.Unlock()
	if checkoutCardGen != w.gen {
		return
	}
	notifyBrowserStage(BrowserStageNotice{Kind: "confirm_clear"})
}

// DesktopPayClickRefusal stops a pixel click on a pay or place-order control.
// The person confirms that purchase on the browser card, or clicks it themselves.
func DesktopPayClickRefusal(name string) string {
	if !IsCheckoutControl(name, "") {
		return ""
	}
	return "这是支付或下单按钮，不要用像素代点。网页结账改用 browser(action=\"confirm_order\", item, shop, address, amount)，等用户在确认卡上核对后再由 browser 点击一次。桌面支付框请用户自己点。不要把支付密码打进对话。"
}

// DesktopSecretTypeRefusal stops typing into a password or code field.
// The person types it on the machine. The refusal does not contain the text.
func DesktopSecretTypeRefusal(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || !secretField(n) {
		return ""
	}
	return "这是密码或验证码框。不要输入，也不要在对话里索要。网页字段改用 browser(action=\"submit_secret\", method, ref)。桌面上的框请用户自己填写，然后重新观察。"
}

// DesktopPayKeyRefusal stops a key that would submit a pay control, and stops
// every key while the agent itself is in a password or code field. combo is
// the key chord. focused is the control the agent last clicked or typed into.
// labels are the names already observed in the foreground window. Enter in a
// search box, a note, or a payment-method row is still sent. Space activates
// only the focused control, so an unknown focus does not block it. The
// refusal does not contain the keystrokes.
func DesktopPayKeyRefusal(combo, focused, kind string, labels []string) (reason, title, msg string) {
	focused = checkoutFocusLabel(focused)
	if focused != "" && desktopSecretCaret(focused, kind) {
		return "secret", focused, DesktopSecretTypeRefusal(focused)
	}
	pay := focused
	if !IsCheckoutControl(pay, "") {
		pay = desktopPayLabel(labels)
	}
	if desktopComboActivates(combo) {
		if focused != "" && IsCheckoutControl(focused, "") {
			return "checkout", focused, DesktopPayClickRefusal(focused)
		}
		return "", "", ""
	}
	if !desktopComboSubmits(combo) || pay == "" {
		return "", "", ""
	}
	if focused != "" && (checkoutSearchField(focused) || checkoutMultilineField(focused) || checkoutSafeLabel(focused)) {
		return "", "", ""
	}
	return "checkout", pay, DesktopPayClickRefusal(pay)
}

func desktopPayLabel(labels []string) string {
	best := ""
	bestLen := int(^uint(0) >> 1)
	for _, raw := range labels {
		label := checkoutFocusLabel(raw)
		if label == "" || !IsCheckoutControl(label, "") {
			continue
		}
		if n := len([]rune(label)); n < bestLen {
			best = label
			bestLen = n
		}
	}
	return best
}

func desktopComboParts(combo string) []string {
	return strings.FieldsFunc(combo, func(r rune) bool {
		return r == '+' || r == ' ' || r == '-'
	})
}

func desktopKeyIsEnter(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, "_", "")
	k = strings.ReplaceAll(k, " ", "")
	switch k {
	case "enter", "return", "kpenter":
		return true
	default:
		return false
	}
}

func desktopComboSubmits(combo string) bool {
	for _, part := range desktopComboParts(combo) {
		if desktopKeyIsEnter(part) {
			return true
		}
	}
	return false
}

func desktopKeyIsSpace(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, "_", "")
	k = strings.ReplaceAll(k, "-", "")
	return k == "space" || k == "spacebar"
}

// A modified space is an input method or a shortcut, not a button click.
func desktopComboActivates(combo string) bool {
	parts := desktopComboParts(combo)
	return len(parts) == 1 && desktopKeyIsSpace(parts[0])
}

// DesktopSubmitKey reports that the chord can activate the focused control
// or submit the page. Arrow keys and clipboard chords do not.
func DesktopSubmitKey(combo string) bool {
	return desktopComboSubmits(combo) || desktopComboActivates(combo)
}

// DesktopSecretSurfaceRefusal stops keys and untargeted typing while the
// foreground window is a password or verification dialog. A document titled
// 密码学 is not such a dialog. The refusal does not contain typed text.
func DesktopSecretSurfaceRefusal(title string) string {
	n := strings.ToLower(strings.TrimSpace(title))
	if n == "" || !secretSurface(n) {
		return ""
	}
	return "当前窗口在等密码或验证码。不要输入，也不要按键代填。请用户在屏幕上自己完成，然后重新观察。"
}

func secretField(n string) bool {
	for _, marker := range []string{"支付密码", "密码", "password", "passcode", "验证码"} {
		if strings.Contains(n, marker) {
			return true
		}
	}
	return containsASCIIToken(n, "otp")
}

// desktopSecretCaret reports that the agent's last control is a password or
// code field. A button that only mentions the code, such as 获取验证码, is not
// the caret. kind comes from the observed control; an empty kind stays closed.
func desktopSecretCaret(name, kind string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || !secretField(n) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "button", "checkbox", "radio", "link", "menuitem", "tab", "listitem", "treeitem", "icon", "menu":
		return !secretActionName(n)
	default:
		return true
	}
}

func secretActionName(n string) bool {
	for _, marker := range []string{"获取", "发送", "重发", "重新发送", "重新获取", "点击", "按钮", "resend", "get code", "send code"} {
		if strings.Contains(n, marker) {
			return true
		}
	}
	return false
}

func secretSurface(n string) bool {
	for _, marker := range []string{"支付密码", "请输入密码", "验证码", "安全验证"} {
		if strings.Contains(n, marker) {
			return true
		}
	}
	// Whole words, so a window titled 1Password is not treated as a password dialog.
	for _, token := range []string{"password", "passcode", "otp"} {
		if containsASCIIToken(n, token) {
			return true
		}
	}
	return false
}

// AlignCheckoutCard fills blank rows from the page and pins the amount to the
// payable total. The model's amount is kept only when the page has no payable
// total. A crossed-out price cannot be what the person confirms.
func (s *BrowserAgentSession) AlignCheckoutCard(card CheckoutCard) CheckoutCard {
	if s == nil {
		return card
	}
	if strings.TrimSpace(card.PageURL) == "" {
		card.PageURL = s.CurrentURL()
	}
	return mergeCheckoutCard(card, CheckoutCardFromPage(s.checkoutPageText(), card.PageURL))
}

func mergeCheckoutCard(model, scraped CheckoutCard) CheckoutCard {
	if strings.TrimSpace(model.Item) == "" {
		model.Item = scraped.Item
	}
	if strings.TrimSpace(model.Shop) == "" {
		model.Shop = scraped.Shop
	}
	if strings.TrimSpace(model.Address) == "" {
		model.Address = scraped.Address
	}
	if strings.TrimSpace(model.Detail) == "" {
		model.Detail = scraped.Detail
	}
	if scraped.Amount != "" {
		model.Amount = scraped.Amount
	}
	if strings.TrimSpace(model.PageURL) == "" {
		model.PageURL = scraped.PageURL
	}
	if strings.TrimSpace(model.Host) == "" {
		model.Host = scraped.Host
	}
	return model
}

func containsASCIIToken(text, token string) bool {
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if part == token {
			return true
		}
	}
	return false
}

var payableMarkers = []string{"应付总额", "应付金额", "实付款", "实付", "应付", "订单总额", "合计", "总计"}

// CheckoutCardFromPage pulls a payable amount and the lines around it.
// The amount is the one next to 应付 / 实付, not the first price on the page.
func CheckoutCardFromPage(text, pageURL string) CheckoutCard {
	card := CheckoutCard{Host: stageHost(pageURL), PageURL: strings.TrimSpace(pageURL)}
	card.Amount = payableAmount(text)
	card.Address = labeledLine(text, []string{"收货地址", "默认地址", "配送至", "送至"})
	card.Item = productLine(text)
	card.Shop = shopLine(text)
	card.Detail = detailLine(text)
	return sanitizeCheckoutCard(card)
}

func payableAmount(text string) string {
	best := ""
	bestRank := len(payableMarkers)
	for rank, marker := range payableMarkers {
		idx := strings.Index(text, marker)
		if idx < 0 || rank >= bestRank {
			continue
		}
		window := text[idx:]
		if rs := []rune(window); len(rs) > 80 {
			window = string(rs[:80])
		}
		m := checkoutAmountRe.FindStringSubmatch(window)
		if m == nil {
			continue
		}
		amount := canonicalAmount(m[1], m[2])
		if amount == "0.00" {
			continue
		}
		best = amount
		bestRank = rank
	}
	return best
}

func labeledLine(text string, markers []string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, marker := range markers {
			i := strings.Index(line, marker)
			if i < 0 {
				continue
			}
			rest := strings.TrimSpace(line[i+len(marker):])
			rest = strings.TrimLeft(rest, "：: \t")
			if rest == "" {
				continue
			}
			return rest
		}
	}
	return ""
}

func productLine(text string) string {
	best := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.Join(strings.Fields(line), " "))
		if line == "" || strings.Contains(line, "地址") {
			continue
		}
		if !strings.Contains(line, "×") && !strings.Contains(line, "mL") && !strings.Contains(line, "ml") {
			continue
		}
		if len([]rune(line)) > len([]rune(best)) {
			best = line
		}
	}
	return best
}

func shopLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.Join(strings.Fields(line), " "))
		if line == "" || strings.Contains(line, "地址") || strings.Contains(line, "¥") || strings.Contains(line, "￥") {
			continue
		}
		if strings.Contains(line, "店") && len([]rune(line)) <= 40 {
			return line
		}
	}
	return ""
}

func detailLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.Join(strings.Fields(line), " "))
		if strings.Contains(line, "包邮") || (strings.Contains(line, "预计") && strings.Contains(line, "发货")) {
			return line
		}
	}
	return ""
}

func stageHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
