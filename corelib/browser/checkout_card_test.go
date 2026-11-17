package browser

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCheckoutCardFromPageUsesPayableTotal(t *testing.T) {
	text := strings.Join([]string{
		"椰子水饮料 1250mL×2瓶",
		"京喜自营官方店",
		"默认地址：北京海淀区上地东路上地佳园8-1-202，马勇",
		"商品价 ¥11.74",
		"应付总额 ¥9.98",
		"京东快递预计明天发货",
	}, "\n")
	card := CheckoutCardFromPage(text, "https://item.jd.com/123.html")
	if card.Amount != "9.98" {
		t.Fatalf("amount %q", card.Amount)
	}
	if !strings.Contains(card.Item, "椰子水") || !strings.Contains(card.Address, "上地佳园") {
		t.Fatalf("card %+v", card)
	}
	if card.Shop != "京喜自营官方店" || card.Host != "item.jd.com" {
		t.Fatalf("card %+v", card)
	}
	if strings.Contains(card.Detail, "密码") {
		t.Fatal(card.Detail)
	}
	secret := sanitizeCheckoutCard(CheckoutCard{Amount: "9.98", Detail: "支付密码 123456"})
	if secret.Detail != "" {
		t.Fatalf("detail kept a secret: %q", secret.Detail)
	}
}

func TestParseCheckoutAmountPrefersPayableThenLast(t *testing.T) {
	payable := sanitizeCheckoutCard(CheckoutCard{Amount: "到手价 ¥11.74 应付总额 ¥9.98"})
	if payable.Amount != "9.98" {
		t.Fatalf("payable %q", payable.Amount)
	}
	last := sanitizeCheckoutCard(CheckoutCard{Amount: "¥11.74 ¥9.98"})
	if last.Amount != "9.98" {
		t.Fatalf("last %q", last.Amount)
	}
	zero := sanitizeCheckoutCard(CheckoutCard{Amount: "¥0.00"})
	if zero.Amount != "" {
		t.Fatalf("zero %q", zero.Amount)
	}
}

func TestDesktopPayAndSecretRefusals(t *testing.T) {
	pay := DesktopPayClickRefusal("立即支付")
	if pay == "" || strings.Contains(pay, "123456") {
		t.Fatalf("pay %q", pay)
	}
	if DesktopPayClickRefusal("支付方式") != "" || DesktopPayClickRefusal("搜索") != "" {
		t.Fatal("payment method and search must stay clickable")
	}
	secret := DesktopSecretTypeRefusal("支付密码")
	if secret == "" || strings.Contains(secret, "123456") {
		t.Fatalf("secret %q", secret)
	}
	if DesktopSecretTypeRefusal("搜索") != "" || DesktopSecretTypeRefusal("Adopt plan") != "" {
		t.Fatal("ordinary fields must accept typing")
	}
	if DesktopSecretTypeRefusal("OTP") == "" {
		t.Fatal("otp field must be refused")
	}
	if DesktopSecretSurfaceRefusal("密码学笔记 - Word") != "" {
		t.Fatal("a document title must not look like a password dialog")
	}
	if DesktopSecretSurfaceRefusal("请输入6位数字支付密码") == "" {
		t.Fatal("payment password window must be refused")
	}
	if DesktopSecretSurfaceRefusal("1Password") != "" {
		t.Fatal("1Password is a window title, not a password dialog")
	}
	if DesktopSecretSurfaceRefusal("Enter password") == "" {
		t.Fatal("a password dialog must be refused")
	}
}

func TestMergeCheckoutCardPinsPayableTotal(t *testing.T) {
	page := CheckoutCardFromPage(strings.Join([]string{
		"椰子水饮料 1250mL×2瓶",
		"京喜自营官方店",
		"默认地址：北京海淀区上地佳园",
		"商品价 ¥11.74",
		"应付总额 ¥9.98",
	}, "\n"), "https://item.jd.com/1")
	merged := mergeCheckoutCard(CheckoutCard{Item: "模型写的商品", Amount: "11.74"}, page)
	if merged.Amount != "9.98" || merged.Item != "模型写的商品" || !strings.Contains(merged.Address, "上地佳园") {
		t.Fatalf("%+v", merged)
	}
	if merged.Shop != "京喜自营官方店" {
		t.Fatalf("shop %+v", merged)
	}
}

func TestCheckoutNeedsPersonMessageUsesCardWhenPreviewOn(t *testing.T) {
	t.Cleanup(func() {
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
	})
	if checkoutNeedsPersonMessage() != MsgCheckoutBlocked {
		t.Fatal(checkoutNeedsPersonMessage())
	}
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(BrowserStageNotice) {})
	msg := checkoutNeedsPersonMessage()
	if !strings.Contains(msg, "confirm_order") || strings.Contains(msg, "确认支付 金额") {
		t.Fatal(msg)
	}
}

func TestCheckoutCardTimeoutClearsTheCard(t *testing.T) {
	old := userHoldWaitCap
	userHoldWaitCap = 40 * time.Millisecond
	t.Cleanup(func() {
		userHoldWaitCap = old
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveCheckoutCard(false)
		ClearCheckoutConfirmation("alice")
	})
	EnableGUIEmbed(true)
	var mu sync.Mutex
	var kinds []string
	SetBrowserStageNotifier(func(n BrowserStageNotice) {
		mu.Lock()
		kinds = append(kinds, n.Kind)
		mu.Unlock()
	})
	sess := &BrowserAgentSession{OwnerID: "alice", stopCh: make(chan struct{})}
	_, err := sess.OfferCheckoutCard(CheckoutCard{Amount: "9.98", Item: "椰子水"})
	if !errors.Is(err, errUserHoldTimeout) {
		t.Fatalf("timeout err=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(kinds) < 2 || kinds[0] != "confirm" || kinds[len(kinds)-1] != "confirm_clear" {
		t.Fatalf("kinds %v", kinds)
	}
	if err := consumeCheckoutConfirmation("alice", "应付总额 ¥9.98"); !isPolicyDenied(err) {
		t.Fatalf("timeout stored a confirmation: %v", err)
	}
}

func TestReleaseCheckoutCardSkipsANewerGeneration(t *testing.T) {
	t.Cleanup(func() {
		checkoutWaitMu.Lock()
		checkoutCardGen = 0
		checkoutWaitCurrent = nil
		checkoutWaitMu.Unlock()
		SetBrowserStageNotifier(nil)
	})
	var kinds []string
	SetBrowserStageNotifier(func(n BrowserStageNotice) { kinds = append(kinds, n.Kind) })
	checkoutWaitMu.Lock()
	checkoutCardGen = 2
	checkoutWaitMu.Unlock()
	releaseCheckoutCardNotice(&checkoutWait{gen: 1})
	if len(kinds) != 0 {
		t.Fatalf("old card cleared a newer one: %v", kinds)
	}
	releaseCheckoutCardNotice(&checkoutWait{gen: 2})
	if len(kinds) != 1 || kinds[0] != "confirm_clear" {
		t.Fatalf("kinds %v", kinds)
	}
}

func TestOfferCheckoutCardRecordsOnlyAfterApprove(t *testing.T) {
	t.Cleanup(func() {
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveCheckoutCard(false)
		ClearCheckoutConfirmation("alice")
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
	sess := &BrowserAgentSession{ID: "browser-session-1", OwnerID: "alice", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	var amount string
	go func() {
		var err error
		amount, err = sess.OfferCheckoutCard(CheckoutCard{
			Item: "椰子水饮料 1250mL×2瓶", Shop: "京喜自营官方店", Address: "上地佳园", Amount: "¥9.98",
		})
		done <- err
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("confirm card was not shown")
	}
	if got.Kind != "confirm" || got.Amount != "9.98" || got.Item == "" {
		t.Fatalf("notice %+v", got)
	}
	if _, ok := RecordCheckoutConfirmation("alice", "还没点确认"); ok {
		t.Fatal("a phrase without an amount must not record")
	}
	if err := ResolveCheckoutCard(true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil || amount != "9.98" {
			t.Fatalf("offer amount=%q err=%v", amount, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("offer did not return after confirm")
	}
	if err := consumeCheckoutConfirmation("alice", "应付总额 ¥9.98"); err != nil {
		t.Fatal(err)
	}
}

func TestOfferCheckoutCardCancelDoesNotRecord(t *testing.T) {
	t.Cleanup(func() {
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
		_ = ResolveCheckoutCard(false)
		ClearCheckoutConfirmation("alice")
	})
	EnableGUIEmbed(true)
	SetBrowserStageNotifier(func(BrowserStageNotice) {})
	sess := &BrowserAgentSession{ID: "browser-session-2", OwnerID: "alice", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := sess.OfferCheckoutCard(CheckoutCard{Item: "椰子水", Amount: "9.98"})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	if err := ResolveCheckoutCard(false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errCheckoutCancelled) {
			t.Fatalf("cancel err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("offer did not return after cancel")
	}
	if err := consumeCheckoutConfirmation("alice", "应付总额 ¥9.98"); !isPolicyDenied(err) {
		t.Fatalf("cancel stored a confirmation: %v", err)
	}
}

func TestCancelUserHoldDoesNotTellTheModelToProbe(t *testing.T) {
	t.Cleanup(func() { SetUserHoldsBrowser(false, "") })
	SetUserHoldsBrowser(true, "secret")
	sess := &BrowserAgentSession{stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- sess.blockIfUserHolds()
	}()
	time.Sleep(20 * time.Millisecond)
	CancelUserHold()
	select {
	case err := <-done:
		if !errors.Is(err, errUserCancelled) || strings.Contains(err.Error(), "probe") {
			t.Fatalf("cancel err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not release the hold")
	}
}

func TestHandPageToUserWaitsUntilDone(t *testing.T) {
	t.Cleanup(func() {
		EnableGUIEmbed(false)
		SetBrowserStageNotifier(nil)
		SetUserHoldsBrowser(false, "")
	})
	EnableGUIEmbed(true)
	var got BrowserStageNotice
	SetBrowserStageNotifier(func(n BrowserStageNotice) { got = n })
	sess := &BrowserAgentSession{ID: "browser-session-3", stopCh: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- sess.handPageToUser("secret", "https://item.jd.com/pay", "支付")
	}()
	time.Sleep(30 * time.Millisecond)
	if got.Kind != "attention" || got.Reason != "secret" || got.Host != "item.jd.com" || !got.Hold {
		t.Fatalf("notice %+v", got)
	}
	held, reason := UserHoldsBrowser()
	if !held || reason != "secret" {
		t.Fatalf("hold %v %q", held, reason)
	}
	SetUserHoldsBrowser(false, "")
	select {
	case err := <-done:
		if !errors.Is(err, errUserReleased) {
			t.Fatalf("handback err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handoff did not return")
	}
}
