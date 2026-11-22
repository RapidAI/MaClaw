package guiapp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestTrustedBrowserPublishedWhenLaunchableRuntimeInstalled pins the servable
// ladder's third rung: an app restart leaves no live session and no CDP
// endpoint, but the host owns the managed launcher, so a confident browser
// turn must plan instead of HostRejecting the whole request as
// no_feasible_provider (the 2026-10-10 catalog-refusal incident). The
// override stands in for the installed-runtime probe so the plan exercises
// publication, not Chrome.
func TestTrustedBrowserPublishedWhenLaunchableRuntimeInstalled(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{registry: NewToolRegistry(), app: app}
	h.trustedBrowserServableOverride = func() bool { return true }
	if !semanticTrustedBrowserPublished(h) {
		t.Fatal("host with a launchable browser runtime must publish the trusted browser adapter")
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "打开 https://app.uned.es/soid 完成注册", "desktop", "root-browser-launchable", "turn-browser-launchable",
		&intent.ClassificationResult{Primary: intent.LabelBrowser, Confidence: .92},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("browser turn on servable host must plan handled=%v err=%v defs=%d", handled, err, len(defs))
	}
	if surface.plan.Selections[0].AdapterName != semanticTrustedBrowserAdapter {
		t.Fatalf("selection adapter=%q, want %s", surface.plan.Selections[0].AdapterName, semanticTrustedBrowserAdapter)
	}
}

// TestTrustedBrowserUnpublishedWhenNoBrowserInstalled pins the one state that
// stays closed: with no runtime alive, nothing discoverable, and no browser
// installed, publication would only re-create the same refusal one turn
// later, so the adapter stays unpublished and the turn keeps its explicit
// unmet contract.
func TestTrustedBrowserUnpublishedWhenNoBrowserInstalled(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{registry: NewToolRegistry(), app: app}
	h.trustedBrowserServableOverride = func() bool { return false }
	if semanticTrustedBrowserPublished(h) {
		t.Fatal("host with no browser runtime and nothing installed must not publish")
	}
	registerBuiltinTools(h.registry, h)
	_, _, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "打开 https://app.uned.es/soid 完成注册", "desktop", "root-browser-absent", "turn-browser-absent",
		&intent.ClassificationResult{Primary: intent.LabelBrowser, Confidence: .92},
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("browser turn on unservable host must stay an explicit unmet handled=%v err=%v", handled, err)
	}
}

// TestTrustedBrowserActionEnumMatchesWhatTheHostPerforms pins the fix for the
// 2026-10-10 rejection: a model handed an unlabelled action string guessed
// "open" and got trusted_browser_action_rejected, with nothing in the schema or
// the error telling it that "navigate" was the name that would have worked.
// The enum is only documentation for the model -- the canonicalizer does not
// read it -- so this holds the two against each other: every name the schema
// advertises must be one the check accepts, and a name outside the set must be
// refused. Drift in either direction is the incident returning.
func TestTrustedBrowserActionEnumMatchesWhatTheHostPerforms(t *testing.T) {
	schema := semanticTrustedBrowserInvocationSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("browser schema has no properties: %#v", schema)
	}
	action, ok := properties["action"].(map[string]interface{})
	if !ok {
		t.Fatalf("browser schema has no action property: %#v", properties)
	}
	enum, ok := action["enum"].([]string)
	if !ok || len(enum) == 0 {
		t.Fatalf("action must publish a non-empty enum, got %#v", action["enum"])
	}
	for _, name := range enum {
		if !semanticBrowserActionAllowed(name) {
			t.Fatalf("schema advertises action %q but the execution check refuses it", name)
		}
	}
	for _, name := range semanticBrowserActions() {
		advertised := false
		for _, candidate := range enum {
			if candidate == name {
				advertised = true
				break
			}
		}
		if !advertised {
			t.Fatalf("execution check accepts action %q but the schema never shows it", name)
		}
	}
	for _, floor := range tool.BrowserControlActions() {
		if !semanticBrowserActionAllowed(floor) {
			t.Fatalf("desktop browser set dropped the shared action %q", floor)
		}
	}
	for _, handoff := range []string{"submit_secret", "request_takeover", "confirm_order"} {
		if !semanticBrowserActionAllowed(handoff) {
			t.Fatalf("desktop browser set is missing %s", handoff)
		}
	}
	// The guess that failed in production must still be refused at the
	// boundary: the enum is not the boundary, this check is.
	for _, guessed := range []string{"open", "goto", "visit", ""} {
		_, err := semanticTrustedBrowserArgsAllowed(map[string]interface{}{"action": guessed})
		if err == nil || !strings.Contains(err.Error(), "trusted_browser_action_rejected") {
			t.Fatalf("action %q = %v, want trusted_browser_action_rejected", guessed, err)
		}
		// The refusal must also name the real vocabulary. The legacy merged
		// browser surface answers the same guess with "unknown browser action
		// ... (supported: navigate, ...)"; rejecting the guess with less than
		// that is what turned one silent failure into a dead-end turn.
		for _, valid := range semanticBrowserActions() {
			if !strings.Contains(err.Error(), valid) {
				t.Fatalf("refusal for %q = %v does not mention the valid name %q", guessed, err, valid)
			}
		}
	}
	// An action that takes a url must not be accepted without one. The schema
	// says url is optional, so this is the only place the pairing is held.
	if _, err := semanticTrustedBrowserArgsAllowed(map[string]interface{}{"action": "navigate"}); err == nil ||
		!strings.Contains(err.Error(), "trusted_browser_url_required") {
		t.Fatalf("navigate without url = %v, want trusted_browser_url_required", err)
	}
}

func TestTrustedBrowserSchemaKeepsTheSecretOffTheCall(t *testing.T) {
	schema := semanticTrustedBrowserInvocationSchema()
	properties, _ := schema["properties"].(map[string]interface{})
	for _, leaked := range []string{"password", "secret", "code", "pin", "cookie", "session_id", "selector"} {
		if _, ok := properties[leaked]; ok {
			t.Fatalf("browser schema leaked %s", leaked)
		}
	}
	secret := "998877"
	_, err := semanticTrustedBrowserArgsAllowed(map[string]interface{}{
		"action":   "submit_secret",
		"method":   "otp",
		"ref":      "e3",
		"password": secret,
	})
	if err == nil || !strings.Contains(err.Error(), "trusted_browser_secret_rejected") {
		t.Fatalf("secret field = %v, want trusted_browser_secret_rejected", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("refusal echoed the secret: %v", err)
	}
	if _, err := semanticTrustedBrowserArgsAllowed(map[string]interface{}{
		"action": "submit_secret",
		"method": "password",
		"ref":    "e3",
	}); err != nil {
		t.Fatalf("submit_secret with method and ref = %v", err)
	}
}

func TestTrustedBrowserBlockedClickKeepsTheCardInstruction(t *testing.T) {
	_, err := finishTrustedBrowserAction("click", &browser.BrowserActionResult{
		Status:  "blocked",
		Display: "调用 browser(action=\"confirm_order\", item, shop, address, amount)",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "trusted_browser_action_refused") || !strings.Contains(err.Error(), "confirm_order") {
		t.Fatalf("blocked click = %v", err)
	}
}

func TestTrustedBrowserPreviewHandBackContinuesTheTurn(t *testing.T) {
	text, err := finishTrustedBrowserAction("type", nil, browser.ErrPreviewHandBack)
	if err != nil {
		t.Fatalf("hand-back err = %v", err)
	}
	for _, want := range []string{"没有发出", "浏览器会话还在", "这一轮继续", "先 probe"} {
		if !strings.Contains(text, want) {
			t.Fatalf("hand-back missing %q: %s", want, text)
		}
	}
	for _, banned := range []string{"断开", "无法", "[system rejected]", "trusted_browser_action_refused"} {
		if strings.Contains(text, banned) {
			t.Fatalf("hand-back reads as a disconnect: %s", text)
		}
	}
	qr, err := finishTrustedBrowserAction("click", &browser.BrowserActionResult{
		Status:  "blocked",
		Detail:  "login_qr",
		Display: "用户已在预览中完成扫码。先 probe，再继续。不要在对话里索要密码或验证码。",
	}, nil)
	if err != nil || !strings.Contains(qr, "浏览器会话还在") || !strings.Contains(qr, "先 probe") || strings.Contains(qr, "trusted_browser_action_refused") {
		t.Fatalf("qr hand-back = %q err=%v", qr, err)
	}
	_, err = finishTrustedBrowserAction("click", &browser.BrowserActionResult{
		Status:  "blocked",
		Detail:  "captcha_widget",
		Display: "用户取消了预览里的这一步。不要重试刚才的点击，也不要索要密码或验证码。",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "trusted_browser_action_refused") {
		t.Fatalf("cancel = %v", err)
	}
	_, err = finishTrustedBrowserAction("click", nil, fmt.Errorf("browser target is gone"))
	if err == nil || !strings.Contains(err.Error(), "trusted_browser_session_disconnected") {
		t.Fatalf("gone session = %v", err)
	}
}

func TestTrustedBrowserObservationFrequencyStopHidesTheNextClick(t *testing.T) {
	text := trustedBrowserObservationText(&browser.BrowserObservation{
		Display: "probed page 购物车 (https://cart.jd.com/cart_index), interactive elements: 2\n站点因访问频繁拒绝了这次请求。页眉和页脚不是页面内容。不要刷新这个地址，也不要隔一段时间再试。把这一步停下来告诉用户。",
		Snapshot: browser.BrowserSnapshot{
			Refs: []browser.BrowserElementRef{
				{Ref: "@e1", Role: "button", Name: "刷新看看"},
				{Ref: "@e2", Role: "link", Name: "去结算"},
			},
			PageTextExcerpt: "购物车跑丢了 去购物刷新看看 购物车(0)",
		},
	})
	if !strings.Contains(text, "把这一步停下来告诉用户") || !strings.Contains(text, "购物车(0)") {
		t.Fatalf("observation dropped the refusal: %s", text)
	}
	if strings.Contains(text, "@e1") || strings.Contains(text, "@e2") || strings.Contains(text, "probe again") || strings.Contains(text, "先 probe") {
		t.Fatalf("observation offered the next click: %s", text)
	}
}

func TestTrustedBrowserObservationNamesRefsAndHidesTheSecret(t *testing.T) {
	text := trustedBrowserObservationText(&browser.BrowserObservation{
		Display: "probed page 登录",
		Snapshot: browser.BrowserSnapshot{
			PageFlags: browser.BrowserPageFlags{LoginWall: true},
			Refs: []browser.BrowserElementRef{
				{Ref: "@e1", Role: "textbox", InputType: "tel", Name: "手机号"},
				{Ref: "@e2", Role: "textbox", InputType: "password", Name: "密码", Value: "998877"},
			},
			PageTextExcerpt: "应付 ¥9.98",
		},
	})
	for _, want := range []string{"@e1", "@e2", "input=password", "commit=secret", "submit_secret", "应付 ¥9.98"} {
		if !strings.Contains(text, want) {
			t.Fatalf("observation missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "998877") {
		t.Fatalf("observation echoed the secret: %s", text)
	}
}

func TestTrustedBrowserObservationKeepsThePayableTotalAndThePayButton(t *testing.T) {
	body := strings.Repeat("导航 ", 500) + "应付总额 ¥9.98 含运费"
	refs := make([]browser.BrowserElementRef, 100)
	for i := range refs {
		refs[i] = browser.BrowserElementRef{Ref: fmt.Sprintf("@e%d", i+1), Visible: true, InViewport: true}
	}
	refs[99] = browser.BrowserElementRef{Ref: "@e100", Role: "button", Name: "确认支付", Visible: true, InViewport: true}
	text := trustedBrowserObservationText(&browser.BrowserObservation{
		Display: "probed page 结算",
		Snapshot: browser.BrowserSnapshot{
			PageFlags:       browser.BrowserPageFlags{PaymentConfirm: true},
			PageTextExcerpt: body,
			Refs:            refs,
		},
	})
	for _, want := range []string{"应付总额 ¥9.98", "@e100", "确认支付", "commit=pay", "confirm_order"} {
		if !strings.Contains(text, want) {
			t.Fatalf("observation missing %q: %s", want, text)
		}
	}
}

func TestTrustedBrowserObservationSurvivesModelPreview(t *testing.T) {
	excerpt := "管理收货地址" + strings.Repeat("甲", 400) + "雪碧500ml" + strings.Repeat("甲", 400) + "收货地址北京市海淀区上地十街10号" + strings.Repeat("乙", 800) + "应付总额 ¥9.98 含运费" + strings.Repeat("丙", 500)
	refs := make([]browser.BrowserElementRef, 90)
	for i := range refs {
		refs[i] = browser.BrowserElementRef{
			Ref:        fmt.Sprintf("@e%d", i+1),
			Role:       "link",
			Name:       strings.Repeat("导航", 20),
			Visible:    true,
			InViewport: true,
		}
	}
	refs[len(refs)-1] = browser.BrowserElementRef{
		Ref: "@e90", Role: "button", Name: "确认支付", Visible: true, InViewport: true,
	}
	text := trustedBrowserObservationText(&browser.BrowserObservation{
		Display: "probed page 结算",
		Snapshot: browser.BrowserSnapshot{
			PageFlags:       browser.BrowserPageFlags{PaymentConfirm: true},
			PageTextExcerpt: excerpt,
			Refs:            refs,
		},
	})
	if len(text) <= 4096 {
		t.Fatalf("observation must exceed the model preview, len=%d", len(text))
	}
	projection, err := agent.ProjectToolResult("browser", "owner", text)
	if err != nil && projection.Preview == "" {
		t.Fatal(err)
	}
	for _, want := range []string{"雪碧500ml", "收货地址北京市海淀区上地十街10号", "应付总额 ¥9.98", "@e90", "commit=pay", "确认支付", "confirm_order"} {
		if !strings.Contains(projection.Preview, want) {
			t.Fatalf("model preview missing %q:\n%s", want, projection.Preview)
		}
	}
	streetAt := strings.Index(projection.Preview, "上地十街10号")
	headerAt := strings.Index(projection.Preview, "管理收货地址")
	if streetAt < 0 || (headerAt >= 0 && headerAt < streetAt) {
		t.Fatalf("header precedes the ship-to line:\n%s", projection.Preview)
	}
	payAt := strings.Index(projection.Preview, "@e90")
	pageAt := strings.Index(projection.Preview, "page_text:\n")
	hintAt := strings.Index(projection.Preview, "confirm_order")
	navAt := strings.Index(projection.Preview, "@e1 ")
	if payAt < 0 || pageAt < 0 || hintAt < 0 || navAt < 0 || payAt > pageAt || hintAt > navAt {
		t.Fatalf("pay %d page %d hint %d nav %d\n%s", payAt, pageAt, hintAt, navAt, projection.Preview)
	}
}

func TestTrustedBrowserPageExcerptPrefersThePayableTotal(t *testing.T) {
	body := "收货地址北京海淀" + strings.Repeat("甲", 200) + "合计 ¥8.00" + strings.Repeat("乙", 2000) + "应付总额 ¥9.98 含运费" + strings.Repeat("丙", 4000) + "页脚版权"
	got := trustedBrowserPageExcerpt(body)
	for _, want := range []string{"收货地址北京海淀", "合计 ¥8.00", "应付总额 ¥9.98"} {
		if !strings.Contains(got, want) {
			t.Fatalf("excerpt missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "页脚版权") {
		t.Fatalf("excerpt replaced the page with the footer: %s", got)
	}
}

func TestTrustedBrowserPageExcerptKeepsAddressBelowTheOldCap(t *testing.T) {
	body := strings.Repeat("导航", 800) + "收货地址北京海淀上地" + strings.Repeat("乙", 4000) + "应付总额 ¥9.98 含运费" + strings.Repeat("丙", 2000) + "页脚版权"
	got := trustedBrowserPageExcerpt(body)
	for _, want := range []string{"收货地址北京海淀上地", "应付总额 ¥9.98"} {
		if !strings.Contains(got, want) {
			t.Fatalf("excerpt missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "页脚版权") {
		t.Fatalf("excerpt replaced the page with the footer: %s", got)
	}
}

func TestTrustedBrowserPageExcerptKeepsTheArticlePrefix(t *testing.T) {
	body := "商品详情" + strings.Repeat("正", 6000) + "页脚版权"
	got := trustedBrowserPageExcerpt(body)
	if !strings.HasPrefix(got, "商品详情") || strings.Contains(got, "页脚版权") || !strings.Contains(got, "正") {
		t.Fatalf("excerpt = %s", got)
	}
}

func TestTrustedBrowserFrequencyStopDoesNotSayToContinue(t *testing.T) {
	text, err := finishTrustedBrowserAction("navigate", &browser.BrowserActionResult{
		Status:  "unchanged",
		Display: "站点因访问频繁拒绝了这个地址，这次没有重新打开。不要刷新同一地址，也不要隔一段时间再试。把这一步停下来告诉用户。",
	}, nil)
	if err != nil || strings.Contains(text, "再继续") || strings.Contains(text, "先 probe") || !strings.Contains(text, "没有重新打开") {
		t.Fatalf("frequency hold = %q err=%v", text, err)
	}
	opened, err := finishTrustedBrowserAction("navigate", &browser.BrowserActionResult{
		Status:  "ok",
		Display: "navigated to https://search.example/Search\n站点因访问频繁拒绝了这次请求。页眉和页脚不是页面内容。不要刷新这个地址，也不要隔一段时间再试。把这一步停下来告诉用户。",
	}, nil)
	if err != nil || strings.Contains(opened, "再继续") || strings.Contains(opened, "先 probe") || !strings.Contains(opened, "navigated to") {
		t.Fatalf("first refusal = %q err=%v", opened, err)
	}
}

func TestTrustedBrowserNavigateStillSaysToProbe(t *testing.T) {
	text, err := finishTrustedBrowserAction("navigate", &browser.BrowserActionResult{
		Status:  "ok",
		Display: "opened https://probe.example.com/item",
	}, nil)
	if err != nil || !strings.Contains(text, "opened https://probe.example.com/item") || !strings.Contains(text, "先 probe") {
		t.Fatalf("navigate result = %q err=%v", text, err)
	}
}

func TestTrustedBrowserClickTellsTheModelToProbe(t *testing.T) {
	text, err := finishTrustedBrowserAction("click", &browser.BrowserActionResult{
		Status:  "ok",
		Display: "clicked @e1",
	}, nil)
	if err != nil || !strings.Contains(text, "clicked @e1") || !strings.Contains(text, "probe") {
		t.Fatalf("click result = %q err=%v", text, err)
	}
}

// bootstrap ladder: the launcher seam bypasses the production connect-user /
// managed-persistent attempts, and a seam that yields nothing surfaces as
// unavailable. The stub records which path ran so the bootstrap contract can
// be exercised without Chrome.
func TestEnsureTrustedBrowserSessionFallsBackToManagedLauncher(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{app: app}
	launched := false
	h.trustedBrowserLauncher = func(principalID string) (*browser.BrowserAgentSession, error) {
		launched = principalID == "user-1"
		return &browser.BrowserAgentSession{}, nil
	}
	sess, err := h.ensureTrustedBrowserSession("user-1")
	if err != nil || sess == nil || !launched {
		t.Fatalf("launcher seam must satisfy bootstrap sess=%#v err=%v launched=%v", sess, err, launched)
	}
	h.trustedBrowserLauncher = func(string) (*browser.BrowserAgentSession, error) {
		return nil, nil
	}
	if _, err := h.ensureTrustedBrowserSession("user-1"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("failed bootstrap must surface as unavailable: %v", err)
	}
}
