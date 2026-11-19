package browser

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOfferSecretCardFillsWithoutReturningTheSecret(t *testing.T) {
	const secret = "p@ss-not-for-the-model"
	orig := applyApprovedSecret
	t.Cleanup(func() {
		applyApprovedSecret = orig
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveSecretCard("", false)
	})
	var filled string
	applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, got string) error {
		filled = got
		if card.Method != "password" || card.Ref != "@e3" || card.Host != "passport.jd.com" {
			t.Errorf("card %+v", card)
		}
		return nil
	}
	EnableGUIEmbed(true)
	var got BrowserStageNotice
	ready := make(chan struct{}, 1)
	SetBrowserStageNotifier(func(n BrowserStageNotice) {
		if n.Kind == "secret" {
			got = n
		}
		select {
		case ready <- struct{}{}:
		default:
		}
	})
	sess := &BrowserAgentSession{ID: "browser-session-1", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	var msg string
	go func() {
		var err error
		msg, err = sess.OfferSecretCard(SecretCard{
			Method: "密码", Ref: "@e3", PageURL: "https://passport.jd.com/login",
		})
		done <- err
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("secret card was not shown")
	}
	if got.Kind != "secret" || got.SecretMethod != "password" || got.SecretSubmission != "fill" {
		t.Fatalf("notice %+v", got)
	}
	blob := got.Item + got.Detail + got.Amount + got.Reason + got.Host
	if strings.Contains(blob, secret) || strings.Contains(blob, "p@ss") {
		t.Fatalf("notice leaked the secret: %+v", got)
	}
	if err := ResolveSecretCard(secret, true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("offer did not return")
	}
	if filled != secret {
		t.Fatalf("filled %q", filled)
	}
	if strings.Contains(msg, secret) || !strings.Contains(msg, "probe") {
		t.Fatalf("model text %q", msg)
	}
}

func TestSecretCardRejectKeepsTheCard(t *testing.T) {
	orig := applyApprovedSecret
	t.Cleanup(func() {
		applyApprovedSecret = orig
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveSecretCard("", false)
	})
	applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, got string) error { return nil }
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(BrowserStageNotice) {})
	sess := &BrowserAgentSession{ID: "browser-session-1", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := sess.OfferSecretCard(SecretCard{Method: "otp", Digits: 6, Ref: "@e1"})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if err := ResolveSecretCard("12", true); !errors.Is(err, errSecretRejected) || strings.Contains(err.Error(), "12") {
		t.Fatalf("reject err=%v", err)
	}
	if err := ResolveSecretCard("123456", true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("offer did not finish after a good code")
	}
}

func TestSecretCardCancelDoesNotFill(t *testing.T) {
	orig := applyApprovedSecret
	t.Cleanup(func() {
		applyApprovedSecret = orig
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveSecretCard("", false)
	})
	filled := false
	applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, got string) error {
		filled = true
		return nil
	}
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(BrowserStageNotice) {})
	sess := &BrowserAgentSession{ID: "browser-session-1", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := sess.OfferSecretCard(SecretCard{Method: "authenticator", Ref: "@e2", Submission: "enter"})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if err := ResolveSecretCard("", false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errSecretCancelled) || strings.Contains(err.Error(), "probe") && strings.Contains(err.Error(), "索要") == false {
			t.Fatalf("cancel err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not finish")
	}
	if filled {
		t.Fatal("cancel filled the field")
	}
}

func TestSecretCardRequiresPreviewAndAField(t *testing.T) {
	t.Cleanup(func() {
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
	})
	sess := &BrowserAgentSession{ID: "browser-session-1", stopCh: make(chan struct{})}
	if _, err := sess.OfferSecretCard(SecretCard{Method: "password", Ref: "@e1"}); !errors.Is(err, errSecretHandoffOff) {
		t.Fatalf("off err=%v", err)
	}
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(BrowserStageNotice) {})
	if _, err := sess.OfferSecretCard(SecretCard{Method: "password"}); !errors.Is(err, errSecretTarget) {
		t.Fatalf("target err=%v", err)
	}
	if _, err := sess.OfferSecretCard(SecretCard{Method: "maybe", Ref: "@e1"}); !errors.Is(err, errSecretMethod) {
		t.Fatalf("method err=%v", err)
	}
	if _, err := sess.OfferSecretCard(SecretCard{Method: "otp", Ref: "@e1", Submission: "click"}); !errors.Is(err, errSecretSubmit) {
		t.Fatalf("submit err=%v", err)
	}
}

func TestModelPasswordTypePointsAtSecretCard(t *testing.T) {
	t.Cleanup(func() {
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
	})
	sess := &BrowserAgentSession{ID: "browser-session-1"}
	if err := sess.refuseSecretTyping(); err == nil || err.Error() != "not_password_field" {
		t.Fatalf("without preview err=%v", err)
	}
	EnableGUIEmbed(true)
	notified := false
	SetBrowserStageNotifier(func(BrowserStageNotice) { notified = true })
	err := sess.refuseSecretTyping()
	if !errors.Is(err, errUseSubmitSecret) || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("preview err=%v", err)
	}
	if notified {
		t.Fatal("refusing a model secret must not open a takeover")
	}
}

func TestSecretCardClearDoesNotDropANewerHold(t *testing.T) {
	orig := applyApprovedSecret
	t.Cleanup(func() {
		applyApprovedSecret = orig
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveSecretCard("", false)
	})
	applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, got string) error { return nil }
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(n BrowserStageNotice) {
		if n.Kind == "secret_clear" {
			SetUserHoldsBrowser(true, "confirm")
		}
	})
	sess := &BrowserAgentSession{ID: "browser-session-1", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := sess.OfferSecretCard(SecretCard{Method: "password", Ref: "@e1"})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if err := ResolveSecretCard("pw", true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("offer did not finish")
	}
	held, reason := UserHoldsBrowser()
	if !held || reason != "confirm" {
		t.Fatalf("newer hold %v %q", held, reason)
	}
}

func TestSecretFillKeepsTheHoldWhileTyping(t *testing.T) {
	orig := applyApprovedSecret
	t.Cleanup(func() {
		applyApprovedSecret = orig
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveSecretCard("", false)
	})
	started := make(chan struct{})
	release := make(chan struct{})
	applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, got string) error {
		close(started)
		<-release
		return nil
	}
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(BrowserStageNotice) {})
	sess := &BrowserAgentSession{ID: "browser-session-1", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := sess.OfferSecretCard(SecretCard{Method: "password", Ref: "@e1"})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if err := ResolveSecretCard("pw", true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("fill did not start")
	}
	if !SecretFillOwnsHold() {
		t.Fatal("closing the preview would release the hold during the fill")
	}
	held, _ := UserHoldsBrowser()
	if !held {
		t.Fatal("hold dropped while the secret was being typed")
	}
	// The person already submitted. A later cancel must not drop the hold
	// the fill still owns, and must not cancel some other hold.
	if err := ResolveSecretCard("", false); err != nil {
		t.Fatal(err)
	}
	if !SecretFillOwnsHold() {
		t.Fatal("cancel during fill released the card")
	}
	held, _ = UserHoldsBrowser()
	if !held {
		t.Fatal("cancel during fill released the hold")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fill did not finish")
	}
	if SecretFillOwnsHold() {
		t.Fatal("fill still owns the hold")
	}
	held, _ = UserHoldsBrowser()
	if held {
		t.Fatal("hold stayed after the fill")
	}
}

func TestSecretStopDoesNotDropAnApprovedFill(t *testing.T) {
	orig := applyApprovedSecret
	t.Cleanup(func() {
		applyApprovedSecret = orig
		SetUserHoldsBrowser(false, "")
		_ = ResolveSecretCard("", false)
	})
	const secret = "pw-not-for-the-model"
	filled := 0
	applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, got string) error {
		filled++
		if got != secret {
			t.Errorf("fill secret mismatch")
		}
		return nil
	}
	stop := make(chan struct{})
	close(stop)
	sess := &BrowserAgentSession{stopCh: stop}
	for i := 0; i < 32; i++ {
		w := &secretWait{
			card:     SecretCard{Method: "password", Submission: "fill"},
			done:     make(chan struct{}),
			filled:   make(chan struct{}),
			gen:      ^uint64(0),
			closed:   true,
			approved: true,
			secret:   secret,
		}
		close(w.done)
		msg, err := waitPrimarySecret(sess, w)
		if err != nil || strings.Contains(msg, secret) {
			t.Fatalf("approved fill lost to stop: msg=%q err=%v", msg, err)
		}
	}
	if filled != 32 {
		t.Fatalf("filled %d of 32 approved cards", filled)
	}

	applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, got string) error {
		t.Errorf("open card was filled")
		return nil
	}
	w := &secretWait{
		card:   SecretCard{Method: "password", Submission: "fill"},
		done:   make(chan struct{}),
		filled: make(chan struct{}),
		gen:    ^uint64(0),
	}
	_, err := waitPrimarySecret(sess, w)
	if !errors.Is(err, errUserHoldStopped) {
		t.Fatalf("open card err=%v", err)
	}
}

func TestDismissDoesNotDropAHoldStartedWhileClosing(t *testing.T) {
	t.Cleanup(func() {
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveSecretCard("", false)
	})
	entered := make(chan struct{})
	releaseDone := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(releaseDone) }) }
	t.Cleanup(finish)
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(n BrowserStageNotice) {
		if n.Kind != "secret_clear" {
			return
		}
		SetUserHoldsBrowser(true, "confirm")
		close(entered)
		<-releaseDone
	})
	sess := &BrowserAgentSession{ID: "browser-session-1", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := sess.OfferSecretCard(SecretCard{Method: "password", Ref: "@e1"})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	browserUserHold.mu.Lock()
	cancelledLease := browserUserHold.lease
	browserUserHold.mu.Unlock()
	ch := activeHoldCh()
	filling := SecretFillOwnsHold()
	if err := ResolveSecretCard("", false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("secret clear did not start the next hold")
	}
	if !filling {
		browserUserHold.releaseIfCurrent(ch, false)
	}
	finish()
	select {
	case err := <-done:
		if !errors.Is(err, errSecretCancelled) {
			t.Fatalf("cancel err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("offer did not finish")
	}
	held, reason := UserHoldsBrowser()
	if !held || reason != "confirm" {
		t.Fatalf("dismiss dropped the newer hold %v %q", held, reason)
	}
	err := waitArmedHold(cancelledLease, nil)
	if !errors.Is(err, errUserCancelled) || strings.Contains(err.Error(), "probe") {
		t.Fatalf("dismiss rewrote the cancel: %v", err)
	}
}

func TestSecretCancelLeaseStaysCancel(t *testing.T) {
	t.Cleanup(func() { SetUserHoldsBrowser(false, "") })
	SetUserHoldsBrowser(false, "")

	lease := browserUserHold.set(true, "secret_card", false)
	w := &secretWait{done: make(chan struct{}), filled: make(chan struct{}), holdCh: lease.ch}
	if !finishSecretWait(w, false, nil) {
		t.Fatal("cancel did not close the wait")
	}
	// Cleanup and dismiss both run after the cancel. Neither may turn it
	// into a hand-back, and a second finish must leave the lease alone.
	browserUserHold.releaseIfCurrent(w.holdCh, false)
	SetUserHoldsBrowser(false, "")
	if finishSecretWait(w, false, nil) {
		t.Fatal("second finish closed the wait again")
	}
	err := waitArmedHold(lease, nil)
	if !errors.Is(err, errUserCancelled) || strings.Contains(err.Error(), "probe") {
		t.Fatalf("cancel became a hand-back: %v", err)
	}

	SetUserHoldsBrowser(false, "")
	lease = browserUserHold.set(true, "secret_card", false)
	stopped := &secretWait{done: make(chan struct{}), filled: make(chan struct{}), holdCh: lease.ch}
	finishSecretWait(stopped, false, errUserHoldStopped)
	browserUserHold.releaseIfCurrent(stopped.holdCh, false)
	err = waitArmedHold(lease, nil)
	if !errors.Is(err, errUserCancelled) || strings.Contains(err.Error(), "probe") {
		t.Fatalf("session stop became a hand-back: %v", err)
	}

	SetUserHoldsBrowser(false, "")
	lease = browserUserHold.set(true, "secret_card", false)
	timedOut := &secretWait{done: make(chan struct{}), filled: make(chan struct{}), holdCh: lease.ch}
	finishSecretWait(timedOut, false, errUserHoldTimeout)
	err = waitArmedHold(lease, nil)
	if !errors.Is(err, errUserReleased) || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("timeout err=%v", err)
	}

	SetUserHoldsBrowser(false, "")
	lease = browserUserHold.set(true, "secret_card", false)
	filling := &secretWait{done: make(chan struct{}), filled: make(chan struct{}), holdCh: lease.ch, closed: true, approved: true}
	if finishSecretWait(filling, false, nil) {
		t.Fatal("finish released a card the person already submitted")
	}
	held, reason := UserHoldsBrowser()
	if !held || reason != "secret_card" {
		t.Fatalf("fill hold %v %q", held, reason)
	}
}

func TestSecretCommitDoesNotPayWithoutConfirmation(t *testing.T) {
	t.Cleanup(func() { ClearCheckoutConfirmation("desktop-user") })
	cashier := CheckoutPage{Focused: "支付密码", Tag: "password", Context: "应付总额 9.98", URL: "https://pay.jd.com/cashier"}
	if !secretCommitNeedsPay("enter", cashier, true, nil, "", "") {
		t.Fatal("enter in a payment password on the cashier must wait for confirmation")
	}
	login := CheckoutPage{Focused: "密码", Tag: "password", URL: "https://passport.jd.com/login", Context: "账号登录"}
	if secretCommitNeedsPay("enter", login, false, nil, "", "") {
		t.Fatal("enter on a sign-in password must still submit the login")
	}
	if secretCommitNeedsPay("click", CheckoutPage{}, false, &BrowserElementRef{Name: "登录"}, "", "") {
		t.Fatal("a login button is not a payment")
	}
	pay := &BrowserElementRef{Name: "立即支付"}
	if !secretCommitNeedsPay("click", CheckoutPage{}, false, pay, "", "") {
		t.Fatal("a pay button must wait for confirmation")
	}
	if !secretCommitNeedsPay("click", CheckoutPage{}, false, &BrowserElementRef{Name: "支付方式"}, ".pay-btn", "") {
		t.Fatal("a pay selector must wait for confirmation")
	}
	sess := &BrowserAgentSession{OwnerID: "desktop-user"}
	if _, ok := RecordCheckoutConfirmation("desktop-user", "确认支付 ¥9.98"); !ok {
		t.Fatal("confirmation was not recorded")
	}
	if err := sess.gateSecretCommit("click", pay, "", ""); !errors.Is(err, errSecretPayHeld) {
		t.Fatalf("pay err=%v", err)
	}
	if _, ok := PendingCheckoutConfirmation("desktop-user"); !ok {
		t.Fatal("a refused pay commit spent the confirmation")
	}
	if err := sess.gateSecretCommit("click", &BrowserElementRef{Name: "登录"}, "", ""); err != nil {
		t.Fatalf("login click err=%v", err)
	}
	if _, ok := PendingCheckoutConfirmation("desktop-user"); !ok {
		t.Fatal("a login click spent the payment confirmation")
	}
}

func TestCheckoutCardBusyWhileSecretCardIsUp(t *testing.T) {
	orig := applyApprovedSecret
	t.Cleanup(func() {
		applyApprovedSecret = orig
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveSecretCard("", false)
		_ = ResolveCheckoutCard(false)
	})
	applyApprovedSecret = func(s *BrowserAgentSession, card SecretCard, got string) error { return nil }
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(BrowserStageNotice) {})
	sess := &BrowserAgentSession{ID: "browser-session-1", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := sess.OfferSecretCard(SecretCard{Method: "otp", Digits: 6, Ref: "@e1"})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if _, err := sess.OfferCheckoutCard(CheckoutCard{Amount: "9.98", Item: "椰子水"}); !errors.Is(err, errSecretBusy) {
		t.Fatalf("checkout during secret err=%v", err)
	}
	if err := ResolveSecretCard("123456", true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("secret card did not finish")
	}
}

func TestQRPageHandsThePreviewOver(t *testing.T) {
	t.Cleanup(func() {
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
	})
	EnableGUIEmbed(true)
	var got BrowserStageNotice
	ready := make(chan struct{}, 1)
	SetBrowserStageNotifier(func(n BrowserStageNotice) {
		got = n
		select {
		case ready <- struct{}{}:
		default:
		}
	})
	sess := &BrowserAgentSession{
		ID:             "browser-session-1",
		stopCh:         make(chan struct{}),
		lastSnapshotID: "snap",
		snapshots: map[string]*BrowserSnapshot{
			"snap": {SnapshotID: "snap", URL: "https://login.example/qr", Title: "扫码登录", PageFlags: BrowserPageFlags{LoginQR: true}},
		},
	}
	done := make(chan *BrowserActionResult, 1)
	go func() {
		done <- sess.personAskIfNeeded("click")
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("qr takeover was not shown")
	}
	if got.Kind != "attention" || got.Reason != "qr" || !got.Hold {
		t.Fatalf("notice %+v", got)
	}
	SetUserHoldsBrowser(false, "")
	select {
	case res := <-done:
		if res == nil || res.Status != "blocked" || res.Detail != "login_qr" || strings.Contains(res.Display, "索要") == false {
			t.Fatalf("result %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("qr handoff did not return")
	}
}
