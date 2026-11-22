package browser

import (
	"strings"
	"testing"
	"time"
)

func TestLoginWallURL(t *testing.T) {
	for _, u := range []string{"https://www.zhihu.com/signin?next=%2Fwrite", "https://www.amazon.com/ap/signin", "https://passport.example.com/x", "https://a.com/account/unhuman?type=captcha"} {
		if loginWallURL(u) == "" {
			t.Fatalf("%s should be a wall", u)
		}
	}
	for _, u := range []string{"https://zhuanlan.zhihu.com/write", "https://www.zhihu.com/"} {
		if loginWallURL(u) != "" {
			t.Fatalf("%s is not a wall", u)
		}
	}
}

func TestNavReadyNote(t *testing.T) {
	r := NavReadyReport{State: PageReadyState{URL: "https://zhuanlan.zhihu.com/write", ReadyState: "loading"}, Reloaded: true, Wall: "blank", Waited: 36 * time.Second}
	n := r.Note()
	for _, want := range []string{"final URL: https://zhuanlan.zhihu.com/write", "reloaded once", LoginWallMarker, "take over"} {
		if !strings.Contains(n, want) {
			t.Fatalf("missing %q in %q", want, n)
		}
	}
	ok := NavReadyReport{State: PageReadyState{URL: "https://x.com/", ReadyState: "complete", Interactive: 12}, Ready: true}
	if strings.Contains(ok.Note(), LoginWallMarker) {
		t.Fatal("ready page is not a wall")
	}
	login := NavReadyReport{State: PageReadyState{URL: "https://passport.jd.com/login"}, Wall: "login_form"}
	note := login.Note()
	if !strings.Contains(note, "submit_secret") || strings.Contains(note, "take over") {
		t.Fatalf("sign-in note = %q", note)
	}
	if reason, take := wallHandsPageOver("captcha"); !take || reason != "captcha" {
		t.Fatalf("captcha handoff %q %v", reason, take)
	}
	if _, take := wallHandsPageOver("login_form"); take {
		t.Fatal("a sign-in form must stay with submit_secret")
	}
	if _, take := wallHandsPageOver("rate_limit"); take {
		t.Fatal("a frequency refusal has nothing for the person to solve")
	}
	limited := NavReadyReport{State: PageReadyState{URL: "https://search.example/Search", ReadyState: "complete", Interactive: 85}, Wall: "rate_limit"}
	limitNote := limited.Note()
	if !strings.Contains(limitNote, "不要刷新") || strings.Contains(limitNote, LoginWallMarker) || strings.Contains(limitNote, "take over") {
		t.Fatalf("rate limit note = %q", limitNote)
	}
	if !strings.Contains(pageReadyExpr, "rate_limit") || !strings.Contains(pageReadyExpr, "访问频繁") || !strings.Contains(pageReadyExpr, "head.split") || !strings.Contains(pageReadyExpr, "freqOpen") {
		t.Fatal("readiness probe does not treat a frequency refusal as the page")
	}
	if strings.Contains(pageReadyExpr, "rate limit/i.test(head)") || strings.Contains(pageReadyExpr, "querySelectorAll('iframe") {
		t.Fatal("readiness walks the document, or a mention of the words is the refusal")
	}
	if !strings.Contains(frequencyNoticeFnJS, "contenteditable") || !strings.Contains(frequencyNoticeFnJS, "getBoundingClientRect") || !strings.Contains(frequencyNoticeFnJS, "opacity") || !strings.Contains(frequencyNoticeFnJS, "parentElement") || !strings.Contains(frequencyNoticeFnJS, "innerHeight") {
		t.Fatal("the notice walk counts hidden, editor, off-screen, or inline fragment text")
	}
	if strings.Contains(frequencyNoticeFnJS, "children.length <= 3") {
		t.Fatal("a container of many children is the notice")
	}
	if strings.Contains(frequencyNoticeFnJS, "defaultView") {
		t.Fatal("the notice walk reads through the page window")
	}
}

func TestFrequencyRefusalSkipsTheReload(t *testing.T) {
	search := "https://search.jd.com/Search?keyword=water"
	sess := &BrowserAgentSession{}
	note := sess.noteFrequencyRefusalLocked(BrowserSnapshot{
		URL: search,
		PageFlags: BrowserPageFlags{
			BlockingNotice: "抱歉由于访问频繁导致无法搜索，请稍后再试",
		},
	})
	if !strings.Contains(note, "不要刷新") {
		t.Fatalf("search note = %q", note)
	}
	cart := "https://cart.jd.com/cart_index"
	shell := "购物车(0) 购物车跑丢了 去购物刷新看看~~ 合计:￥0.00 去结算 (0)"
	if extra := sess.noteFrequencyRefusalLocked(BrowserSnapshot{URL: cart, PageTextExcerpt: shell}); !strings.Contains(extra, "没有加载出来") {
		t.Fatalf("empty shell after a refusal = %q", extra)
	}
	res, err := sess.Navigate(cart + "?t=1795270081351")
	if err != nil || res == nil || res.Status != "unchanged" || !strings.Contains(res.Display, "没有重新打开") {
		t.Fatalf("reload = %#v err=%v", res, err)
	}
	quiet := &BrowserAgentSession{}
	if extra := quiet.noteFrequencyRefusalLocked(BrowserSnapshot{URL: cart, PageTextExcerpt: shell}); extra != "" {
		t.Fatalf("a real empty list was called a refusal: %q", extra)
	}
	if _, err := quiet.Navigate(cart); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unrelated navigate = %v", err)
	}
	sess.mu.Lock()
	sess.frequencyPathAt[documentPathKey(cart)] = time.Now().Add(-frequencyRefusalTTL - time.Minute)
	sess.mu.Unlock()
	held, err := sess.Navigate(cart)
	if err != nil || held == nil || !strings.Contains(held.Display, "没有重新打开") {
		t.Fatalf("a live site stamp let the cart open: %#v err=%v", held, err)
	}
	sess.mu.Lock()
	sess.frequencyHostAt[documentSite(cart)] = time.Now().Add(-frequencyRefusalTTL - time.Minute)
	sess.mu.Unlock()
	if _, err := sess.Navigate(cart); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expired refusal still blocked the navigation: %v", err)
	}
}

func TestFrequencyRefusalStaysNarrow(t *testing.T) {
	long := strings.Repeat("商品说明文字", 20) + "访问频繁" + strings.Repeat("仍在介绍这件商品", 20)
	if frequencyRefusalText(long) {
		t.Fatal("a product description that mentions the words is a refusal")
	}
	buried := strings.Repeat("这是商品页的正常介绍。", 8) + "访问频繁请稍后再试。" + strings.Repeat("后面还有很多说明。", 8)
	if frequencyRefusalText(buried) {
		t.Fatal("a sentence inside the page was the notice")
	}
	page := "https://item.jd.com/100.html"
	mentioned := &BrowserAgentSession{}
	if extra := mentioned.noteFrequencyRefusalLocked(BrowserSnapshot{URL: page, PageTextExcerpt: buried}); extra != "" {
		t.Fatalf("a sentence inside the page stopped the turn: %q", extra)
	}
	if _, stamped := mentioned.frequencyPathAt[documentPathKey(page)]; stamped {
		t.Fatal("a sentence inside the page stamped the document")
	}
	shortPage := "本页提到访问频繁请稍后再试"
	if extra := (&BrowserAgentSession{}).noteFrequencyRefusalLocked(BrowserSnapshot{URL: page, PageTextExcerpt: shortPage}); extra != "" {
		t.Fatalf("the page text was treated as the notice: %q", extra)
	}
	if extra := (&BrowserAgentSession{}).noteFrequencyRefusalLocked(BrowserSnapshot{
		URL:       page,
		PageFlags: BrowserPageFlags{FrequencyNotice: "抱歉由于访问频繁导致无法搜索，请稍后再试"},
	}); !strings.Contains(extra, "不要刷新") {
		t.Fatalf("the notice field was ignored: %q", extra)
	}
	if !frequencyRefusalText("抱歉由于访问频繁导致无法搜索，请稍后再试") {
		t.Fatal("the notice itself was missed")
	}
	if frequencyRefusalText("see the rate limit") {
		t.Fatal("a rate-limit mention is not a refusal")
	}
	if !frequencyRefusalText("too many requests") || !frequencyRefusalText("rate limit exceeded, try again later") {
		t.Fatal("a short refusal line was missed")
	}
	if documentSite("https://search.jd.com/Search") != "jd.com" || documentSite("https://cart.jd.com/cart_index") != "jd.com" || documentSite("https://a.b.jd.com/x") != "jd.com" {
		t.Fatalf("jd sites = %s %s %s", documentSite("https://search.jd.com/Search"), documentSite("https://cart.jd.com/cart_index"), documentSite("https://a.b.jd.com/x"))
	}
	if documentSite("https://shop.example.co.uk/a") != documentSite("https://www.example.co.uk/b") {
		t.Fatal("a site on a multi-part suffix was split")
	}
	if documentSite("https://shop.example.co.uk/a") == documentSite("https://other.co.uk/") {
		t.Fatal("two names under co.uk were treated as one site")
	}
	cartPlain := "https://cart.jd.com/cart_index"
	cartSlash := "https://cart.jd.com/cart_index/?t=1#top"
	cartDots := "https://cart.jd.com/cart_index/../cart_index/"
	if documentPathKey(cartPlain) != documentPathKey(cartSlash) || documentPathKey(cartPlain) != documentPathKey(cartDots) {
		t.Fatalf("one document had several keys: %s %s %s", documentPathKey(cartPlain), documentPathKey(cartSlash), documentPathKey(cartDots))
	}
	if documentPathKey("https://jd.com/") != "jd.com/" || documentPathKey("https://jd.com/a") == documentPathKey("https://jd.com/b") {
		t.Fatal("document keys collapsed different pages")
	}
	slashHeld := &BrowserAgentSession{frequencyPathAt: map[string]time.Time{documentPathKey(cartSlash): time.Now()}}
	if slashHeld.frequencyNavigationBlocked(cartPlain) == "" {
		t.Fatal("a trailing slash was treated as a new document")
	}
	shell := "购物车(0) 购物车跑丢了 去购物刷新看看~~ 合计:￥0.00 去结算 (0)"
	if !emptyShellAfterRefusal(shell) || !emptyShellAfterRefusal("列表（0） 加载失败") {
		t.Fatal("the opening placeholder was not a shell")
	}
	if emptyShellAfterRefusal("合计:￥0.00 刷新看看") || emptyShellAfterRefusal("10(0) 刷新看看") || emptyShellAfterRefusal("(0.5) 刷新看看") {
		t.Fatal("a price or a longer number was a zero badge")
	}
	if emptyShellAfterRefusal(strings.Repeat("甲", 500) + "刷新看看 (0)") {
		t.Fatal("a refresh prompt past the opening was a shell")
	}
	if emptyShellAfterRefusal("购物车(0)" + strings.Repeat("甲", 80) + "加载失败") {
		t.Fatal("a header count and a later failure were one shell")
	}
	if emptyShellAfterRefusal(strings.Repeat("甲", 170) + "。加载失败 (0)") {
		t.Fatal("a later sentence was the list placeholder")
	}
	search := "https://search.jd.com/Search?keyword=water"
	sess := &BrowserAgentSession{}
	_ = sess.noteFrequencyRefusalLocked(BrowserSnapshot{
		URL:       search,
		PageFlags: BrowserPageFlags{BlockingNotice: "抱歉由于访问频繁导致无法搜索，请稍后再试"},
	})
	key := documentPathKey(search)
	old := time.Now().Add(-14 * time.Minute)
	sess.frequencyPathAt[key] = old
	sess.frequencyHostAt[documentSite(search)] = old
	if again := sess.noteFrequencyRefusalLocked(BrowserSnapshot{
		URL:       search,
		PageFlags: BrowserPageFlags{BlockingNotice: "抱歉由于访问频繁导致无法搜索，请稍后再试"},
	}); !strings.Contains(again, "不要刷新") || !sess.frequencyPathAt[key].Equal(old) {
		t.Fatalf("re-read extended the stamp: %s note=%q", sess.frequencyPathAt[key], again)
	}
	if home := sess.frequencyNavigationBlocked("https://www.jd.com/"); home == "" || !strings.Contains(home, "其他页面") {
		t.Fatalf("the site root was sent after the search refusal: %q", home)
	}
	if other := sess.frequencyNavigationBlocked("https://so.jd.com/so/search.action?keyword=water"); other == "" {
		t.Fatal("another host on the refused site was sent")
	}
	if sess.frequencyNavigationBlocked("https://example.com/") != "" {
		t.Fatal("another site was held")
	}
	expired := &BrowserAgentSession{frequencyHostAt: map[string]time.Time{documentSite(search): time.Now().Add(-frequencyRefusalTTL - time.Minute)}}
	if expired.frequencyNavigationBlocked("https://www.jd.com/") != "" {
		t.Fatal("an expired site stamp still blocked a navigation")
	}
	sess.Policy.BlockedDomains = []string{"jd.com"}
	cart := "https://cart.jd.com/cart_index"
	sess.frequencyPathAt[documentPathKey(cart)] = time.Now()
	blocked, err := sess.Navigate(cart)
	if err != nil || blocked == nil || blocked.Status != "blocked" {
		t.Fatalf("domain policy lost to the frequency hold: %#v err=%v", blocked, err)
	}
	sess.Policy.BlockedDomains = nil
	payURL := "https://pay.jd.com/cashier"
	sess.frequencyPathAt[documentPathKey(payURL)] = time.Now()
	pay, err := sess.Navigate(payURL)
	if err != nil || pay == nil || pay.Status != "blocked" || strings.Contains(pay.Display, "没有重新打开") {
		t.Fatalf("checkout lost to the frequency hold: %#v err=%v", pay, err)
	}
	held := labelRefreshResult(&BrowserActionResult{Status: "unchanged", Display: frequencyRefusalStopText(true)}, cart)
	if held == nil || held.Action != "browser_refresh" || !strings.Contains(held.Display, "没有重新打开") || strings.Contains(held.Display, "refreshed") {
		t.Fatalf("refresh renamed the hold: %#v", held)
	}
	sent := labelRefreshResult(&BrowserActionResult{Status: "ok", Display: "navigated"}, cart)
	if sent == nil || !strings.Contains(sent.Display, "refreshed "+cart) {
		t.Fatalf("refresh label = %#v", sent)
	}
	current := &BrowserAgentSession{
		snapshots:      map[string]*BrowserSnapshot{"s": {URL: cart}},
		lastSnapshotID: "s",
		frequencyPathAt: map[string]time.Time{
			documentPathKey(cart): time.Now(),
		},
	}
	if current.frequencyCurrentDocumentBlocked() == "" || !reloadControlLabel("刷新看看") || !reloadControlLabel("去购物刷新看看~~") || !reloadControlLabel("刷新") || !reloadControlLabel("重试") || reloadControlLabel("去购物") || reloadControlLabel(strings.Repeat("刷新", 20)) {
		t.Fatal("reload control on the refused document was not recognized")
	}
	if reloadControlLabel("刷新价格") || reloadControlLabel("申请重试") || reloadControlLabel("preload") || reloadControlLabel("下拉刷新") {
		t.Fatal("a word inside another action was a reload")
	}
	if current.frequencyReloadClickBlocked(&BrowserElementRef{Name: "刷新看看"}, "") == "" {
		t.Fatal("the refresh control would still be clicked")
	}
	if current.frequencyReloadClickBlocked(&BrowserElementRef{Name: "去结算"}, "") == "" || current.frequencyReloadClickBlocked(&BrowserElementRef{Name: "京东首页"}, "") == "" {
		t.Fatal("a click on the refused page was sent")
	}
	home := &BrowserAgentSession{
		snapshots:       map[string]*BrowserSnapshot{"s": {URL: "https://www.jd.com/"}},
		lastSnapshotID:  "s",
		frequencyHostAt: map[string]time.Time{documentSite("https://www.jd.com/"): time.Now()},
	}
	if home.frequencyReloadClickBlocked(&BrowserElementRef{Name: "搜索"}, "") == "" || !frequencyPressHitsSite("Enter") || !frequencyPressHitsSite("F5") || frequencyPressHitsSite("a") {
		t.Fatal("another page of the refused site still accepted a click or Enter")
	}
	elsewhere := &BrowserAgentSession{
		snapshots:       map[string]*BrowserSnapshot{"s": {URL: "https://example.com/"}},
		lastSnapshotID:  "s",
		frequencyHostAt: map[string]time.Time{"jd.com": time.Now()},
	}
	if elsewhere.frequencyReloadClickBlocked(&BrowserElementRef{Name: "去结算"}, "") != "" {
		t.Fatal("a click on another site was held")
	}
	home.session = &Session{}
	entered, err := home.Press("Enter")
	if err != nil || entered == nil || !strings.Contains(entered.Display, "没有重新打开") || !entered.batchStop || entered.batchStopReason != "frequency_refusal" {
		t.Fatalf("Enter on the refused page was sent: %#v err=%v", entered, err)
	}
	spaced, err := home.Press("Space")
	if err != nil || spaced == nil || !FrequencyRefusalStops(spaced.Display) {
		t.Fatalf("Space on the refused page was sent: %#v err=%v", spaced, err)
	}
	missed, err := home.Click("s", "@missing", "")
	if err != nil || missed == nil || !FrequencyRefusalStops(missed.Display) || strings.Contains(missed.Display, "probe") {
		t.Fatalf("a missing ref said to probe the refused page: %#v err=%v", missed, err)
	}
	named, err := home.ClickText("s", "京东首页")
	if err != nil || named == nil || !strings.Contains(named.Display, "其他页面") {
		t.Fatalf("a click by name on the refused page was sent: %#v err=%v", named, err)
	}
	payClick, err := home.Click("", "", ".pay-btn")
	if err != nil || payClick == nil || payClick.Status != "blocked" || strings.Contains(payClick.Display, "没有重新打开") {
		t.Fatalf("a pay click lost to the frequency hold: %#v err=%v", payClick, err)
	}
	payRef := &BrowserAgentSession{
		session: &Session{},
		snapshots: map[string]*BrowserSnapshot{"s": {
			URL:  "https://www.jd.com/",
			Refs: []BrowserElementRef{{Ref: "@e1", Name: "立即支付", Selector: "button.pay"}},
		}},
		lastSnapshotID:  "s",
		frequencyHostAt: map[string]time.Time{"jd.com": time.Now()},
	}
	payNamed, err := payRef.Click("s", "@e1", "")
	if err != nil || payNamed == nil || payNamed.Status != "blocked" || strings.Contains(payNamed.Display, "没有重新打开") {
		t.Fatalf("a named pay control lost to the frequency hold: %#v err=%v", payNamed, err)
	}
	elsewhere.session = &Session{}
	if _, err = elsewhere.Click("s", "@missing", ""); err == nil || !strings.Contains(err.Error(), "ref not found") {
		t.Fatalf("a missing ref on another site became a frequency hold: %v", err)
	}
	current.recentSubmitClicks = map[string]time.Time{"submit": time.Now()}
	heldSubmit, submitErr := current.heldRepeatSubmit("submit")
	if submitErr != nil || heldSubmit == nil || !strings.Contains(heldSubmit.Display, "把这一步停下来告诉用户") || strings.Contains(heldSubmit.Display, "probe the page") {
		t.Fatalf("repeat submit on a refused document said to probe: %#v err=%v", heldSubmit, submitErr)
	}
	open := &BrowserAgentSession{recentSubmitClicks: map[string]time.Time{"submit": time.Now()}}
	if _, submitErr = open.heldRepeatSubmit("submit"); submitErr == nil || !strings.Contains(submitErr.Error(), "probe the page") {
		t.Fatalf("a normal repeat submit lost its guard: %v", submitErr)
	}
	if !reloadKey("F5") || !reloadKey("Ctrl+R") || reloadKey("Enter") || reloadKey("r") {
		t.Fatal("reload key set is wrong")
	}
	if !strings.Contains(browserPageFlagsCollectJS, "frequency_notice") {
		t.Fatal("observe does not read the short refusal line")
	}
	hold := frequencyRefusalStopText(false)
	s := &BrowserAgentSession{ID: "s"}
	withHold := s.completeAction("browser_navigate", "navigated to https://cart.jd.com/cart_index", "", &BrowserObservation{frequencyHold: hold}, nil, false)
	if withHold == nil || !strings.Contains(withHold.Display, "navigated to") || !strings.Contains(withHold.Display, "把这一步停下来告诉用户") {
		t.Fatalf("the action hid the refusal: %#v", withHold)
	}
	already := s.completeAction("browser_navigate", "navigated\n"+hold, "", &BrowserObservation{frequencyHold: hold}, nil, false)
	if already == nil || strings.Count(already.Display, "把这一步停下来告诉用户") != 1 {
		t.Fatalf("the stop sentence was repeated: %q", already.Display)
	}
	notReady := s.completeAction("browser_navigate", "navigated to https://cart.jd.com/cart_index"+notReadyProbeSentence, "", &BrowserObservation{frequencyHold: hold}, nil, false)
	if notReady == nil || strings.Contains(notReady.Display, "probe once more") || !strings.Contains(notReady.Display, "把这一步停下来告诉用户") || !notReady.batchStop || notReady.batchStopReason != "frequency_refusal" {
		t.Fatalf("an unreadiness probe sat on the refusal: %#v", notReady)
	}
	snap := BrowserSnapshot{URL: "https://cart.jd.com/cart_index", Title: "cart"}
	s.lastFingerprint = snapshotFingerprint(snap)
	stuck := s.completeAction("browser_click", "clicked @e1", "", &BrowserObservation{
		Snapshot:      snap,
		frequencyHold: hold,
	}, nil, true)
	if stuck == nil || stuck.Status != "unchanged" || !strings.Contains(stuck.Display, "把这一步停下来告诉用户") ||
		strings.Contains(stuck.Display, "retrying") || strings.Contains(stuck.Display, "verify before") || strings.Contains(stuck.Display, "probe before another submit") ||
		!stuck.batchStop || stuck.batchStopReason != "frequency_refusal" {
		t.Fatalf("unchanged refusal still said to retry: %#v", stuck)
	}
	if stuck.Data != nil {
		if _, ok := stuck.Data["delta"]; ok {
			t.Fatalf("unchanged refusal kept the retry delta: %#v", stuck.Data["delta"])
		}
	}
	plain := s.completeAction("browser_click", "clicked @e1", "", &BrowserObservation{Snapshot: snap}, nil, true)
	if plain == nil || plain.Status != "unchanged" || !strings.Contains(plain.Display, "verify before") {
		t.Fatalf("a normal unchanged page lost the retry note: %#v", plain)
	}
	s.fastBatch = true
	batched := s.completeAction("browser_click", "clicked @e1", "", &BrowserObservation{
		Snapshot:      snap,
		frequencyHold: hold,
	}, nil, true)
	if batched == nil || batched.Status != "unchanged" || strings.Contains(batched.Display, "probe before another submit") || strings.Contains(batched.Display, "retrying") {
		t.Fatalf("batch refusal still said to submit again: %#v", batched)
	}
}
