package httpapi

import (
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// giftLinkCodeLength is the length minted by newGiftLinkCode. The landing page
// accepts only that shape so a crafted path cannot be reflected into HTML.
const giftLinkCodeLength = 10

// tokenBankGiftPublicRL limits anonymous reads of a share code. Preview and
// the landing page share one window: both reveal the same public facts.
var tokenBankGiftPublicRL = newGossipRateLimiter(30, time.Minute)

func validGiftLinkCode(code string) bool {
	if len(code) != giftLinkCodeLength {
		return false
	}
	for _, r := range code {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= '2' && r <= '7':
		default:
			return false
		}
	}
	return true
}

// TokenBankGiftLanding serves GET /c/{code}. The page shows the public preview
// and lets a verified account claim. Claiming still does not move credits;
// the receiver withdraws them from MaClaw afterwards.
func (h *SkillMarketHandlers) TokenBankGiftLanding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !validGiftLinkCode(code) {
		writeGiftLandingNotFound(w)
		return
	}
	// The link row exists only on the clearing node. Without this the landing
	// page — the very first thing a recipient opens — renders "not found" on
	// whichever node the load balancer picked.
	if h.tokenBankRouteToClearing(w, r) {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		http.Error(w, "token bank is not available", http.StatusServiceUnavailable)
		return
	}
	if _, err := repo.GiftLinkByCode(r.Context(), code); err != nil {
		if errors.Is(err, sqlite.ErrGiftLinkNotFound) {
			writeGiftLandingNotFound(w)
			return
		}
		http.Error(w, "token bank is not available", http.StatusServiceUnavailable)
		return
	}
	writeGiftLanding(w, r, code)
}

func writeGiftLandingNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", giftLandingCSP)
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="zh"><head><meta charset="utf-8"><title>MaClaw</title></head><body><main><h1>分享链接不存在或已失效</h1><p>This share link does not exist or is no longer valid.</p></main></body></html>`))
}

func writeGiftLanding(w http.ResponseWriter, r *http.Request, code string) {
	lang := "en"
	if strings.Contains(strings.ToLower(r.Header.Get("Accept-Language")), "zh") {
		lang = "zh"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", giftLandingCSP)
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = giftLandingTmpl.Execute(w, giftLandingView{Code: code, Lang: lang})
}

const giftLandingCSP = "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'"

type giftLandingView struct {
	Code string
	Lang string
}

var giftLandingTmpl = template.Must(template.New("gift-landing").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>MaClaw</title>
<style>
  :root { color-scheme: light; }
  body { margin: 0; font: 16px/1.5 "Segoe UI", sans-serif; background: #f4f6f8; color: #1c2430; }
  main { max-width: 28rem; margin: 8vh auto; padding: 1.5rem; background: #fff; border: 1px solid #d8dee6; border-radius: 12px; }
  h1 { font-size: 1.35rem; margin: 0 0 0.5rem; }
  p { margin: 0.4rem 0; }
  label { display: block; margin: 0.75rem 0 0.25rem; font-size: 0.9rem; }
  input { width: 100%; box-sizing: border-box; padding: 0.5rem 0.6rem; border: 1px solid #c5ced8; border-radius: 8px; }
  .row { display: flex; gap: 0.5rem; flex-wrap: wrap; margin-top: 1rem; }
  button, a.open { font: inherit; border-radius: 8px; padding: 0.5rem 0.9rem; text-decoration: none; }
  button { border: 0; background: #1d4f91; color: #fff; cursor: pointer; }
  button.secondary { background: #e8eef5; color: #1c2430; }
  a.open { display: inline-block; background: #e8eef5; color: #1c2430; }
  .note { color: #526173; font-size: 0.9rem; }
  #message { min-height: 1.4em; }
  select { margin-left: auto; }
  .top { display: flex; align-items: center; }
</style>
</head>
<body data-code="{{.Code}}" data-lang="{{.Lang}}">
<main>
  <div class="top"><strong>MaClaw</strong><select id="lang" aria-label="Language"><option value="zh">简体中文</option><option value="en">English</option></select></div>
  <h1 id="title"></h1>
  <p id="summary"></p>
  <p id="state" class="note"></p>
  <p id="message" role="status"></p>
  <div id="login">
    <label for="email" id="emailLabel"></label>
    <input id="email" type="email" autocomplete="username">
    <label for="password" id="passwordLabel"></label>
    <input id="password" type="password" autocomplete="current-password">
  </div>
  <p id="session" class="note" hidden></p>
  <div class="row">
    <button type="button" id="loginBtn"></button>
    <button type="button" id="claimBtn"></button>
    <a class="open" id="openApp" href="maclaw://credit/{{.Code}}"></a>
  </div>
  <p id="after" class="note"></p>
</main>
<script>
(function () {
  var root = document.body;
  var code = root.dataset.code || "";
  var lang = root.dataset.lang === "zh" ? "zh" : "en";
  var tokenKey = "tbk_credit_session";
  var copy = {
    zh: {
      title: "领取分享的积分",
      email: "邮箱",
      password: "密码",
      login: "登录",
      claim: "领取",
      open: "在 MaClaw 中打开",
      after: "领取只绑定到你的账号。请在 MaClaw 的 Token 银行里把这份积分提取到本机，助手才能使用。",
      loading: "正在读取链接…",
      missing: "分享链接不存在或已失效。",
      shared: "分享了",
      credits: "积分",
      claimable: "可以领取。只有第一个领取的人能拿到。",
      notClaimable: "这条链接现在不能领取。",
      loggedIn: "已登录",
      verify: "请先完成邮箱验证，再领取积分。",
      claimed: "已领取。请打开 MaClaw，把积分提取到本机。",
      held: "你已经领取了。请打开 MaClaw 的 Token 银行，把积分提取到本机。",
      withdrawn: "这份转赠已经提取到本机。",
      failed: "操作没有完成，请稍后重试。"
    },
    en: {
      title: "Claim shared credits",
      email: "Email",
      password: "Password",
      login: "Sign in",
      claim: "Claim",
      open: "Open in MaClaw",
      after: "Claiming binds the credits to your account. Withdraw them to this machine from MaClaw Token Bank before the assistant can spend them.",
      loading: "Loading this link…",
      missing: "This share link does not exist or is no longer valid.",
      shared: "shared",
      credits: "credits",
      claimable: "You can claim this. Only the first person to claim it receives the credits.",
      notClaimable: "This link cannot be claimed.",
      loggedIn: "Signed in",
      verify: "Verify your email before claiming shared credits.",
      claimed: "Claimed. Open MaClaw and withdraw the credits to this machine.",
      held: "You already claimed this. Open MaClaw Token Bank and withdraw it to this machine.",
      withdrawn: "This gift is already withdrawn to your machine.",
      failed: "That did not complete. Try again in a moment."
    }
  };
  function t(key) { return copy[lang][key]; }
  function text(id, value) { document.getElementById(id).textContent = value; }
  function showLogin(on) { document.getElementById("login").hidden = !on; document.getElementById("loginBtn").hidden = !on; }
  var session = document.getElementById("session");
  function applyLang() {
    document.documentElement.lang = lang === "zh" ? "zh" : "en";
    document.getElementById("lang").value = lang;
    text("title", t("title"));
    text("emailLabel", t("email"));
    text("passwordLabel", t("password"));
    text("loginBtn", t("login"));
    text("claimBtn", t("claim"));
    text("openApp", t("open"));
    text("after", t("after"));
  }
  document.getElementById("lang").addEventListener("change", function (event) {
    lang = event.target.value === "zh" ? "zh" : "en";
    applyLang();
    loadPreview();
  });
  function token() { try { return sessionStorage.getItem(tokenKey) || ""; } catch (e) { return ""; } }
  function setToken(value) { try { if (value) sessionStorage.setItem(tokenKey, value); else sessionStorage.removeItem(tokenKey); } catch (e) {} }
  function message(value) { text("message", value || ""); }
  async function loadPreview() {
    text("state", t("loading"));
    var headers = {};
    var current = token();
    if (current) headers.Authorization = "Bearer " + current;
    var res = await fetch("/api/v1/credits/share-links/" + encodeURIComponent(code) + "/preview", { headers: headers });
    if (!res.ok) { text("summary", ""); text("state", t("missing")); document.getElementById("claimBtn").disabled = true; return; }
    var body = await res.json();
    var credits = (Number(body.credits_micro) || 0) / 1000000;
    text("summary", (body.sender_masked || "—") + " " + t("shared") + " " + credits.toFixed(2) + " " + t("credits"));
    var state = t("notClaimable");
    if (body.withdrawable) state = t("held");
    else if (body.withdrawn) state = t("withdrawn");
    else if (body.claimable) state = t("claimable");
    text("state", state);
    document.getElementById("claimBtn").disabled = !body.claimable;
  }
  async function refreshSession() {
    var current = token();
    if (!current) { showLogin(true); session.hidden = true; return; }
    var res = await fetch("/api/v1/auth/me", { headers: { Authorization: "Bearer " + current } });
    if (!res.ok) { setToken(""); showLogin(true); session.hidden = true; return; }
    var user = await res.json();
    showLogin(false);
    session.hidden = false;
    session.textContent = t("loggedIn") + " " + (user.email || "");
    if (String(user.status || "").toLowerCase() !== "verified") message(t("verify"));
  }
  document.getElementById("loginBtn").addEventListener("click", async function () {
    message("");
    var res = await fetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: document.getElementById("email").value,
        password: document.getElementById("password").value
      })
    });
    var body = {};
    try { body = await res.json(); } catch (e) {}
    if (!res.ok || !body.session_token) { message(body.error || body.message || t("failed")); return; }
    setToken(body.session_token);
    await refreshSession();
    await loadPreview();
  });
  document.getElementById("claimBtn").addEventListener("click", async function () {
    message("");
    var current = token();
    if (!current) { message(t("login")); return; }
    var res = await fetch("/api/v1/credits/share-links/" + encodeURIComponent(code) + "/claim", {
      method: "POST",
      headers: { Authorization: "Bearer " + current }
    });
    var body = {};
    try { body = await res.json(); } catch (e) {}
    if (res.status === 403 && (body.code === "unverified_account" || body.error === "unverified_account")) { message(t("verify")); return; }
    if (!res.ok) { message(body.message || body.error || t("failed")); return; }
    message(t("claimed"));
    document.getElementById("claimBtn").disabled = true;
    loadPreview();
  });
  applyLang();
  refreshSession().finally(loadPreview);
})();
</script>
</body>
</html>
`))
