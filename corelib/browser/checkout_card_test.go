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

func TestDesktopPayKeyRefusal(t *testing.T) {
	reason, title, msg := DesktopPayKeyRefusal("enter", "", "", []string{"页面", "立即支付"})
	if reason != "checkout" || title != "立即支付" || msg == "" || strings.Contains(msg, "123456") {
		t.Fatalf("enter %s %s %s", reason, title, msg)
	}
	if reason, _, msg := DesktopPayKeyRefusal("ctrl+enter", "", "", []string{"提交订单"}); reason != "checkout" || msg == "" {
		t.Fatalf("ctrl+enter %s %s", reason, msg)
	}
	if reason, _, _ := DesktopPayKeyRefusal("space", "", "", []string{"立即支付"}); reason != "" {
		t.Fatalf("space without a focused pay control: %s", reason)
	}
	if reason, title, _ := DesktopPayKeyRefusal("space", "立即支付", "button", nil); reason != "checkout" || title != "立即支付" {
		t.Fatalf("space on pay %s %s", reason, title)
	}
	if reason, _, _ := DesktopPayKeyRefusal("enter", "搜索", "edit", []string{"立即支付"}); reason != "" {
		t.Fatalf("search enter %s", reason)
	}
	if reason, _, _ := DesktopPayKeyRefusal("enter", "备注", "edit", []string{"立即支付"}); reason != "" {
		t.Fatalf("note enter %s", reason)
	}
	if reason, _, _ := DesktopPayKeyRefusal("enter", "支付方式", "", []string{"立即支付"}); reason != "" {
		t.Fatalf("payment method enter %s", reason)
	}
	if reason, _, _ := DesktopPayKeyRefusal("ctrl+c", "", "", []string{"立即支付"}); reason != "" {
		t.Fatalf("copy %s", reason)
	}
	if reason, _, _ := DesktopPayKeyRefusal("center", "", "", []string{"立即支付"}); reason != "" {
		t.Fatalf("center must not look like enter: %s", reason)
	}
	reason, title, msg = DesktopPayKeyRefusal("a", "支付密码", "edit", []string{"登录"})
	if reason != "secret" || title != "支付密码" || msg == "" || strings.Contains(msg, "998877") {
		t.Fatalf("secret key %s %s %s", reason, title, msg)
	}
	if reason, _, _ := DesktopPayKeyRefusal("enter", "支付密码", "button", nil); reason != "secret" {
		t.Fatalf("a password box labeled as a button is still the caret: %s", reason)
	}
	if reason, _, msg := DesktopPayKeyRefusal("enter", "获取验证码", "button", nil); reason != "" || strings.Contains(msg, "验证码") {
		t.Fatalf("get-code button must stay usable: %s %s", reason, msg)
	}
	longLine := strings.Repeat("商品说明", 30) + "立即支付"
	if reason, _, _ := DesktopPayKeyRefusal("enter", "", "", []string{longLine, "搜索"}); reason != "" {
		t.Fatalf("a paragraph is not a pay button: %s", reason)
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

func TestCheckoutStopDoesNotDropAConfirmedCard(t *testing.T) {
	t.Cleanup(func() {
		ClearCheckoutConfirmation("alice")
		SetUserHoldsBrowser(false, "")
	})
	stop := make(chan struct{})
	close(stop)
	sess := &BrowserAgentSession{OwnerID: "alice", stopCh: stop}
	for i := 0; i < 32; i++ {
		w := &checkoutWait{
			card:     CheckoutCard{Amount: "9.98"},
			owner:    "alice",
			done:     make(chan struct{}),
			gen:      ^uint64(0),
			closed:   true,
			approved: true,
		}
		close(w.done)
		amount, err := waitCheckoutCard(sess, w)
		if err != nil || amount != "9.98" {
			t.Fatalf("confirmed card lost to stop: amount=%q err=%v", amount, err)
		}
	}
	if amount, ok := PendingCheckoutConfirmation("alice"); ok {
		t.Fatalf("reading the card stored %s", amount)
	}
}

func TestCheckoutStopLeavesAnOpenCardStopped(t *testing.T) {
	t.Cleanup(func() { ClearCheckoutConfirmation("alice") })
	stop := make(chan struct{})
	close(stop)
	sess := &BrowserAgentSession{OwnerID: "alice", stopCh: stop}
	w := &checkoutWait{
		card:  CheckoutCard{Amount: "9.98"},
		owner: "alice",
		done:  make(chan struct{}),
		gen:   ^uint64(0),
	}
	amount, err := waitCheckoutCard(sess, w)
	if amount != "" || !errors.Is(err, errUserHoldStopped) {
		t.Fatalf("open card amount=%q err=%v", amount, err)
	}
	if amount, ok := PendingCheckoutConfirmation("alice"); ok {
		t.Fatalf("stop stored %s", amount)
	}
}

func TestCheckoutApproveAfterCloseDoesNotRecord(t *testing.T) {
	t.Cleanup(func() {
		checkoutWaitMu.Lock()
		checkoutWaitCurrent = nil
		checkoutWaitMu.Unlock()
		ClearCheckoutConfirmation("alice")
		SetUserHoldsBrowser(false, "")
	})
	SetUserHoldsBrowser(false, "")
	w := &checkoutWait{
		card:   CheckoutCard{Amount: "9.98"},
		owner:  "alice",
		done:   make(chan struct{}),
		closed: true,
	}
	checkoutWaitMu.Lock()
	checkoutWaitCurrent = w
	checkoutWaitMu.Unlock()
	if err := ResolveCheckoutCard(true); err == nil {
		t.Fatal("approving a closed card succeeded")
	}
	if amount, ok := PendingCheckoutConfirmation("alice"); ok {
		t.Fatalf("closed card stored confirmation %s", amount)
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
