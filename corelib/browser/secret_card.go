package browser

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

// Secret card. The person types a password or one-time code on the preview
// card. The host fills that field. The model never receives the secret, and
// a page script cannot type it either. Captcha, a QR sign-in, and a passkey
// stay on request_takeover.

var (
	errUseSubmitSecret          = errors.New("不要把密码或验证码交给 type。调用 browser(action=\"submit_secret\", method, ref)，由用户在密钥卡上填写。看不清方式、验证码组件、扫码或通行密钥则调用 browser(action=\"request_takeover\")。不要在对话里索要。")
	errSecretCancelled          = errors.New("用户取消了密钥卡。不要重试刚才的输入，也不要在对话里索要密码或验证码。")
	errSecretMethod             = errors.New("看不清是密码、短信验证码还是验证器动态码。改调用 browser(action=\"request_takeover\")。不要在对话里索要。")
	errSecretTarget             = errors.New("submit_secret 需要 probe 给出的 ref，指向密码框或验证码框。")
	errSecretSubmit             = errors.New("submission=click 需要 submit_ref，指向登录或继续按钮。")
	errSecretSubmission         = errors.New("submission 只能是 fill、enter 或 click。")
	errSecretDigits             = errors.New("验证码位数要在 4 到 12 之间。看不清就改 request_takeover。")
	errSecretRejected           = errors.New("这一位没有通过：密码不能为空，验证码必须是数字并且位数正确。请再填一次。不要发到对话。")
	errSecretFill               = errors.New("密钥没有填进页面。先 probe，再重试 submit_secret。不要在对话里索要密码或验证码。")
	errSecretPayHeld            = errors.New("密钥已填入页面。这一步会下单或支付，所以没有点击，也没有按回车。请先调用 browser(action=\"confirm_order\", item, shop, address, amount) 让用户确认应付总额，确认后再提交。不要再索要密码或验证码。")
	errSecretFilledNotSubmitted = errors.New("密钥已填入页面，但随后的提交没有发出。先 probe，不要再索要密码或验证码。")
	errSecretHandoffOff         = errors.New("密钥卡只在助手预览里打开。不要在对话里索要密码或验证码。")
	errSecretBusy               = errors.New("预览里已经有一张卡在等用户。先完成或取消它。")
	errSecretNone               = errors.New("没有待填写的密钥卡")
)

// SecretCard is the field the person is asked to fill. It never holds the secret.
type SecretCard struct {
	Method         string // password, otp, authenticator
	Digits         int
	Submission     string // fill, enter, click
	Ref            string
	Selector       string
	SubmitRef      string
	SubmitSelector string
	SnapshotID     string
	Host           string
	PageURL        string
}

type secretWait struct {
	card       SecretCard
	done       chan struct{}
	filled     chan struct{}
	holdCh     chan struct{}
	gen        uint64
	closed     bool
	fillClosed bool
	approved   bool
	holdCancel bool
	err        error
	fillErr    error
	doneMsg    string
	secret     string
}

var (
	// previewCardMu serializes installing a secret card or a payment card.
	// The two checks used to release their own locks first, so both could pass.
	previewCardMu     sync.Mutex
	secretWaitMu      sync.Mutex
	secretWaitCurrent *secretWait
	secretCardGen     uint64
)

// applyApprovedSecret types the card secret. Tests replace it. The default
// fills the named field and does not log the secret.
var applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, secret string) error {
	return s.fillApprovedSecret(card, secret)
}

// PreviewHandoffEnabled reports whether the assistant preview can show a card
// or a takeover. Tests and the TUI leave it off.
func PreviewHandoffEnabled() bool { return previewHandoffEnabled() }

// RequestTakeover hands the preview to the person and waits until they finish
// or cancel. reason is captcha, qr, passkey, login, or attention. The result
// text never asks for a secret.
func (s *BrowserAgentSession) RequestTakeover(reason string) (string, error) {
	if s == nil || !previewHandoffEnabled() {
		return "", errSecretHandoffOff
	}
	err := s.handPageToUser(normalizeTakeoverReason(reason), "", "")
	if err == nil || errors.Is(err, errUserReleased) {
		return "用户已交还预览。浏览器会话还在，这一轮继续。先 probe，再继续。不要在对话里索要密码或验证码。", nil
	}
	return "", err
}

func normalizeTakeoverReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "captcha", "captcha_widget":
		return "captcha"
	case "passkey":
		return "passkey"
	case "qr", "login_qr":
		return "qr"
	case "login":
		return "login"
	default:
		return "attention"
	}
}

// OfferSecretCard shows the card and, once the person submits, fills the field.
// The returned text never contains the secret.
func (s *BrowserAgentSession) OfferSecretCard(card SecretCard) (string, error) {
	card, err := normalizeSecretCard(card)
	if err != nil {
		return "", err
	}
	if !previewHandoffEnabled() {
		return "", errSecretHandoffOff
	}
	previewCardMu.Lock()
	if checkoutCardPending() {
		previewCardMu.Unlock()
		return "", errSecretBusy
	}
	secretWaitMu.Lock()
	if secretWaitCurrent != nil {
		w := secretWaitCurrent
		secretWaitMu.Unlock()
		previewCardMu.Unlock()
		return waitJoinedSecret(w)
	}
	secretCardGen++
	w := &secretWait{
		card:   card,
		done:   make(chan struct{}),
		filled: make(chan struct{}),
		gen:    secretCardGen,
	}
	secretWaitCurrent = w
	if lease := browserUserHold.set(true, "secret_card", false); lease != nil {
		w.holdCh = lease.ch
	}
	secretWaitMu.Unlock()
	previewCardMu.Unlock()

	sessionID := ""
	if s != nil {
		sessionID = s.ID
	}
	notifyBrowserStage(BrowserStageNotice{
		Kind:             "secret",
		SessionID:        sessionID,
		URL:              card.PageURL,
		Host:             card.Host,
		Reason:           "secret_card",
		SecretMethod:     card.Method,
		SecretDigits:     card.Digits,
		SecretSubmission: card.Submission,
	})
	return waitPrimarySecret(s, w)
}

func waitJoinedSecret(w *secretWait) (string, error) {
	if w == nil {
		return "", errSecretNone
	}
	timer := time.NewTimer(userHoldWaitCap)
	defer timer.Stop()
	select {
	case <-w.filled:
		secretWaitMu.Lock()
		msg, err := w.doneMsg, w.fillErr
		secretWaitMu.Unlock()
		return msg, err
	case <-timer.C:
		return "", errUserHoldTimeout
	}
}

func waitPrimarySecret(s *BrowserAgentSession, w *secretWait) (msg string, err error) {
	var stop <-chan struct{}
	if s != nil {
		stop = s.stopCh
	}
	timer := time.NewTimer(userHoldWaitCap)
	defer timer.Stop()
	defer func() {
		secretWaitMu.Lock()
		if secretWaitCurrent == w {
			secretWaitCurrent = nil
		}
		if !w.fillClosed {
			if w.doneMsg == "" && w.fillErr == nil {
				if w.err != nil {
					w.fillErr = w.err
				} else {
					w.fillErr = errSecretCancelled
				}
			}
			w.fillClosed = true
			close(w.filled)
		}
		gen := w.gen
		// Release before unlocking so a new card cannot reuse this channel
		// and then lose it. The clear notice runs after, and may start a
		// newer hold of its own. holdCancel was stored before w.done closed,
		// so this cannot turn a cancel into a hand-back.
		if secretWaitCurrent == nil {
			browserUserHold.releaseIfCurrent(w.holdCh, w.holdCancel)
		}
		secretWaitMu.Unlock()
		releaseSecretCardNotice(gen)
	}()
	for {
		// A submitted card is filled even when the session stop or the
		// deadline is also ready. select would otherwise skip the fill.
		approved, waitErr, closed := secretClosedResult(w)
		if closed {
			if !approved {
				return "", waitErr
			}
			return completeApprovedSecret(s, w)
		}
		select {
		case <-w.done:
		case <-stop:
			finishSecretWait(w, false, errUserHoldStopped)
		case <-timer.C:
			finishSecretWait(w, false, errUserHoldTimeout)
		}
	}
}

func secretClosedResult(w *secretWait) (approved bool, err error, closed bool) {
	if w == nil {
		return false, errSecretNone, true
	}
	secretWaitMu.Lock()
	defer secretWaitMu.Unlock()
	if !w.closed {
		return false, nil, false
	}
	if !w.approved {
		if w.err != nil {
			return false, w.err, true
		}
		return false, errSecretCancelled, true
	}
	return true, nil, true
}

func completeApprovedSecret(s *BrowserAgentSession, w *secretWait) (string, error) {
	secretWaitMu.Lock()
	waitErr, secret := w.err, w.secret
	w.secret = ""
	secretWaitMu.Unlock()
	if waitErr != nil {
		return "", waitErr
	}
	fillErr := applyApprovedSecret(s, w.card, secret)
	msg := ""
	switch {
	case fillErr == nil:
		msg = secretDoneMessage(w.card.Submission)
	case errors.Is(fillErr, errSecretPayHeld) || errors.Is(fillErr, errSecretFilledNotSubmitted):
		// The field already has the secret. Keep that fact for the model.
	default:
		log.Printf("[browser] secret fill failed: %s", redactSecretText(fillErr.Error(), secret))
		fillErr = errSecretFill
	}
	secretWaitMu.Lock()
	w.doneMsg = msg
	w.fillErr = fillErr
	secretWaitMu.Unlock()
	return msg, fillErr
}

// finishSecretWait reports whether this call closed the wait. A card the
// person already submitted stays closed, and the caller must not release
// the hold the fill still owns.
func finishSecretWait(w *secretWait, approved bool, err error) bool {
	if w == nil {
		return false
	}
	secretWaitMu.Lock()
	defer secretWaitMu.Unlock()
	if w.closed {
		return false
	}
	w.closed = true
	w.approved = approved
	w.err = err
	if !approved {
		w.secret = ""
	}
	// A cancel or a session stop is not a hand-back. Close that lease
	// before w.done: the cleanup defer, and dismiss's plain release, both
	// run after this returns and must not rewrite the outcome.
	w.holdCancel = err == nil || errors.Is(err, errUserHoldStopped)
	browserUserHold.releaseIfCurrent(w.holdCh, w.holdCancel)
	close(w.done)
	return true
}

// ResolveSecretCard records the person's secret or a cancel. An unacceptable
// value does not close the card, and the secret is not kept. The notifier
// must not call OfferSecretCard or ResolveSecretCard.
func ResolveSecretCard(secret string, approve bool) error {
	secretWaitMu.Lock()
	w := secretWaitCurrent
	secretWaitMu.Unlock()
	if w == nil {
		return errSecretNone
	}
	if !approve {
		finishSecretWait(w, false, nil)
		return nil
	}
	secretWaitMu.Lock()
	method := ""
	digits := 0
	if secretWaitCurrent == w {
		method = w.card.Method
		digits = w.card.Digits
	}
	secretWaitMu.Unlock()
	if method != "password" {
		secret = strings.TrimSpace(secret)
	}
	if method == "" || !secretOK(method, digits, secret) {
		return errSecretRejected
	}
	secretWaitMu.Lock()
	defer secretWaitMu.Unlock()
	if secretWaitCurrent != w || w.closed {
		return errSecretNone
	}
	w.secret = secret
	w.closed = true
	w.approved = true
	close(w.done)
	return nil
}

func releaseSecretCardNotice(gen uint64) {
	secretWaitMu.Lock()
	defer secretWaitMu.Unlock()
	if secretCardGen != gen {
		return
	}
	// The stage notifier must not call OfferSecretCard or ResolveSecretCard.
	notifyBrowserStage(BrowserStageNotice{Kind: "secret_clear"})
}

// SecretFillOwnsHold reports that the host is typing an approved secret.
// Closing the preview must not release the hold during that fill.
func SecretFillOwnsHold() bool {
	secretWaitMu.Lock()
	defer secretWaitMu.Unlock()
	w := secretWaitCurrent
	return w != nil && w.approved && !w.fillClosed
}

func checkoutCardPending() bool {
	checkoutWaitMu.Lock()
	defer checkoutWaitMu.Unlock()
	return checkoutWaitCurrent != nil
}

func secretCardPending() bool {
	secretWaitMu.Lock()
	defer secretWaitMu.Unlock()
	return secretWaitCurrent != nil
}

func normalizeSecretCard(card SecretCard) (SecretCard, error) {
	method, ok := normalizeSecretMethod(card.Method)
	if !ok {
		return SecretCard{}, errSecretMethod
	}
	card.Method = method
	submission, ok := normalizeSecretSubmission(card.Submission)
	if !ok {
		return SecretCard{}, errSecretSubmission
	}
	card.Submission = submission
	card.Ref = strings.TrimSpace(card.Ref)
	card.Selector = strings.TrimSpace(card.Selector)
	card.SubmitRef = strings.TrimSpace(card.SubmitRef)
	card.SubmitSelector = strings.TrimSpace(card.SubmitSelector)
	card.SnapshotID = strings.TrimSpace(card.SnapshotID)
	if card.Ref == "" && card.Selector == "" {
		return SecretCard{}, errSecretTarget
	}
	if submission == "click" && card.SubmitRef == "" && card.SubmitSelector == "" {
		return SecretCard{}, errSecretSubmit
	}
	if method == "password" {
		card.Digits = 0
	} else if card.Digits != 0 && (card.Digits < 4 || card.Digits > 12) {
		return SecretCard{}, errSecretDigits
	}
	card.Host = clipCheckoutField(card.Host, 120)
	card.PageURL = strings.TrimSpace(card.PageURL)
	if card.Host == "" {
		card.Host = stageHost(card.PageURL)
	}
	return card, nil
}

func normalizeSecretMethod(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "password", "pwd", "passcode", "密码", "支付密码":
		return "password", true
	case "otp", "sms", "code", "验证码", "短信验证码":
		return "otp", true
	case "authenticator", "totp", "验证器", "验证器码":
		return "authenticator", true
	default:
		return "", false
	}
}

func normalizeSecretSubmission(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "fill", "on_input":
		return "fill", true
	case "enter", "return":
		return "enter", true
	case "click":
		return "click", true
	default:
		return "", false
	}
}

func secretOK(method string, digits int, secret string) bool {
	if secret == "" || strings.ContainsAny(secret, "\r\n\x00") {
		return false
	}
	if method == "password" {
		return len([]rune(secret)) <= 256
	}
	for _, r := range secret {
		if r < '0' || r > '9' {
			return false
		}
	}
	n := len([]rune(secret))
	if digits > 0 {
		return n == digits
	}
	return n >= 4 && n <= 12
}

func secretDoneMessage(submission string) string {
	if submission == "enter" || submission == "click" {
		return "用户已在密钥卡上填入并提交。先 probe，再继续。不要在对话里索要密码或验证码，也不要自己再写一遍。"
	}
	return "用户已在密钥卡上填入。先 probe，再继续。不要在对话里索要密码或验证码，也不要自己再写一遍。"
}

func redactSecretText(msg, secret string) string {
	if secret == "" {
		return msg
	}
	return strings.ReplaceAll(msg, secret, "…")
}

func (s *BrowserAgentSession) fillApprovedSecret(card SecretCard, secret string) error {
	if s == nil || s.session == nil {
		return errSecretFill
	}
	snap := card.SnapshotID
	if snap == "" {
		s.mu.RLock()
		snap = s.lastSnapshotID
		s.mu.RUnlock()
	}
	candidates, resolved, err := s.selectorCandidatesForAction(snap, card.Ref, card.Selector)
	if err != nil {
		return err
	}
	if _, _, err := s.typeWithCandidates(candidates, secret, BrowserContentFormatPlain, false, modelTypeRefForInsert(false, resolved)); err != nil {
		return err
	}
	return s.finishSecretCommit(card, snap, resolved)
}

// finishSecretCommit sends Enter or the submit click after the secret is in
// the field. A pay or place-order commit still has to match a confirmation.
// The low-level press and click do not take that gate themselves.
func (s *BrowserAgentSession) finishSecretCommit(card SecretCard, snap string, resolved *BrowserElementRef) error {
	switch card.Submission {
	case "enter":
		if err := s.gateSecretCommit("enter", resolved, "", ""); err != nil {
			return err
		}
		frameID := ""
		if resolved != nil {
			frameID = resolved.FrameID
		}
		if err := s.session.PressInFrame(frameID, "Enter"); err != nil {
			return errSecretFilledNotSubmitted
		}
		return nil
	case "click":
		subCandidates, subResolved, err := s.selectorCandidatesForAction(snap, card.SubmitRef, card.SubmitSelector)
		if err != nil {
			return errSecretFilledNotSubmitted
		}
		if err := s.gateSecretCommit("click", subResolved, card.SubmitSelector, ""); err != nil {
			return err
		}
		if _, _, err := s.clickWithCandidates(subCandidates, subResolved); err != nil {
			return errSecretFilledNotSubmitted
		}
		return nil
	default:
		return nil
	}
}

// secretCommitNeedsPay reports that this submit would place or pay an order.
func secretCommitNeedsPay(kind string, page CheckoutPage, onCashier bool, ref *BrowserElementRef, selector, text string) bool {
	switch kind {
	case "enter":
		return checkoutPressWouldCommit("Enter", page)
	case "click":
		label := text
		if ref != nil {
			label = strings.TrimSpace(ref.Name + " " + ref.Text + " " + text)
		}
		return IsCheckoutControl(label, selector) || (onCashier && strings.TrimSpace(label) != "")
	default:
		return false
	}
}

func (s *BrowserAgentSession) gateSecretCommit(kind string, ref *BrowserElementRef, selector, text string) error {
	page := s.ReadCheckoutPage()
	onCashier := false
	if s != nil && s.session != nil {
		if info, err := s.session.Info(); err == nil && info != nil {
			onCashier = IsCashierURL(info.URL)
		}
	}
	if !secretCommitNeedsPay(kind, page, onCashier, ref, selector, text) {
		return nil
	}
	if err := consumeCheckoutConfirmation(s.ownerKey(), s.checkoutPageText()); err != nil {
		return errSecretPayHeld
	}
	return nil
}
