const api = window.go?.main?.App;
const $ = (id) => document.getElementById(id);
let toastTimer;
let loginInProgress = false;
let currentTokenPeriod = "today";
let authModes = [];
let selectedOnboardingMode = "";
let currentAuthMode = "";
let switchingAuthMode = false;
let lastStatus = null;
let xaiWaiting = false;
let openaiWaiting = false;
let kimiWaiting = false;
let kimiHint = "";
let refreshGen = 0;

function formatCacheDecision(status) {
  const protocol = pick(status, "last_cache_protocol", "LastCacheProtocol") || "";
  const outcome = pick(status, "last_cache_outcome", "LastCacheOutcome") || "";
  const streaming = !!pick(status, "last_cache_streaming", "LastCacheStreaming");
  if (!protocol || !outcome) return { summary: "-", reason: "" };
  return { summary: `${protocol} / ${outcome}${streaming ? " / stream" : ""}`, reason: pick(status, "last_cache_reason", "LastCacheReason") || "" };
}
function formatTokenCount(n) {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(n);
}
function formatBytes(bytes) {
  if (bytes >= 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  if (bytes >= 1024) return (bytes / 1024).toFixed(1) + " KB";
  return bytes + " B";
}
function confirmDialog(message, title) {
  return new Promise((resolve) => {
    const mask = $("dialogMask");
    $("dialogTitle").textContent = title || "请确认";
    $("dialogMessage").textContent = message;
    mask.hidden = false;
    let done = false;
    const finish = (ok) => {
      if (done) return;
      done = true;
      mask.hidden = true;
      $("dialogOk").removeEventListener("click", onOk);
      $("dialogCancel").removeEventListener("click", onCancel);
      mask.removeEventListener("click", onMask);
      document.removeEventListener("keydown", onKey);
      resolve(ok);
    };
    const onOk = () => finish(true);
    const onCancel = () => finish(false);
    const onMask = (event) => { if (event.target === mask) finish(false); };
    const onKey = (event) => { if (event.key === "Escape") finish(false); };
    $("dialogOk").addEventListener("click", onOk);
    $("dialogCancel").addEventListener("click", onCancel);
    mask.addEventListener("click", onMask);
    document.addEventListener("keydown", onKey);
  });
}
function notify(message, kind = "ok") {
  const toast = $("toast");
  toast.textContent = message;
  toast.className = `toast show ${kind}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toast.className = "toast"; }, 4200);
}
function errorMessage(err) { return err?.message || String(err || "操作失败"); }
function setBusy(button, busy, text) {
  if (!button) return;
  if (busy) {
    if (!button.dataset.originalText) button.dataset.originalText = button.textContent;
    button.disabled = true;
    if (text) button.textContent = text;
    return;
  }
  button.disabled = false;
  if (button.dataset.originalText) {
    button.textContent = button.dataset.originalText;
    delete button.dataset.originalText;
  }
}
function codexSyncNotice(successMessage, sync) {
  if (!sync) return { message: successMessage, kind: "ok" };
  if (sync.error) return { message: `${successMessage}；Codex 凭据未同步：${sync.error}。请修复 ~/.codex/config.toml 后重新配置 Codex。`, kind: "error" };
  if (!sync.configured) return { message: successMessage, kind: "ok" };
  if (sync.updated) return { message: `${successMessage}；Codex 凭据已同步，请重启 Codex。`, kind: "ok" };
  return { message: `${successMessage}；Codex 凭据已是最新，请重启 Codex。`, kind: "ok" };
}
function pick(obj, ...keys) {
  if (!obj) return undefined;
  for (const key of keys) {
    if (obj[key] != null && obj[key] !== "") return obj[key];
  }
  return undefined;
}
function handleRelaunch(status, fallback) {
  if (pick(status, "relaunch_scheduled", "RelaunchScheduled")) {
    notify("正在重启 CodexProxy 以切换登录方式…");
    return true;
  }
  if (fallback) notify(fallback);
  return false;
}
// A relaunch tears the window down after a short grace period. Say so plainly,
// otherwise the app just disappears at the moment a login appears to succeed.
function showRelaunchNotice() {
  const btn = $("loginActionBtn");
  if (btn) {
    btn.disabled = true;
    btn.textContent = "登录成功，正在重启…";
  }
  if ($("logoutBtn")) $("logoutBtn").style.display = "none";
  document.querySelectorAll("#modeFields button, #modeFields input, #modeFields select")
    .forEach((el) => { el.disabled = true; });
  const toast = $("toast");
  if (toast) {
    toast.textContent = "登录成功，正在重启 CodexProxy…";
    toast.className = "toast show ok";
  }
}
function collectModels() {
  const select = $("modelID");
  if (!select) return [];
  return Array.from(select.options)
    .map((option) => ({ id: option.value, name: option.textContent || option.value }))
    .filter((model) => model.id);
}

const MODE_INPUT_IDS = ["baseURL", "upstreamKey", "anthropicCode", "xaiCode", "openaiCode", "kimiCode", "modelID"];

function snapshotModeInputs() {
  const snap = {};
  MODE_INPUT_IDS.forEach((id) => {
    const el = $(id);
    if (el) snap[id] = el.value;
  });
  return snap;
}

function restoreModeInputs(snap) {
  if (!snap) return;
  if (snap.upstreamKey && $("upstreamKey")) $("upstreamKey").value = snap.upstreamKey;
  if (snap.anthropicCode && $("anthropicCode")) $("anthropicCode").value = snap.anthropicCode;
  if (snap.xaiCode && $("xaiCode")) $("xaiCode").value = snap.xaiCode;
  if (snap.openaiCode && $("openaiCode")) $("openaiCode").value = snap.openaiCode;
  const select = $("modelID");
  if (select && snap.modelID && Array.from(select.options).some((option) => option.value === snap.modelID)) {
    select.value = snap.modelID;
  }
}

function modeMeta(id) {
  return authModes.find((m) => m.id === id) || { id, name: id, kind: "apikey", hint: "" };
}

function renderAuthModeOptions(selected) {
  const select = $("authMode");
  select.innerHTML = "";
  authModes.forEach((mode) => {
    const option = document.createElement("option");
    option.value = mode.id;
    option.textContent = mode.name;
    select.appendChild(option);
  });
  if (selected && authModes.some((m) => m.id === selected)) select.value = selected;
}

function renderOnboardingCards() {
  const grid = $("authModeGrid");
  grid.innerHTML = "";
  authModes.forEach((mode) => {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "method" + (mode.id === selectedOnboardingMode ? " selected" : "");
    btn.innerHTML = `<span class="radio" aria-hidden="true"></span><span class="copy"><b>${mode.name}</b><small>${mode.hint || ""}</small></span>`;
    btn.addEventListener("click", () => {
      selectedOnboardingMode = mode.id;
      renderOnboardingCards();
    });
    grid.appendChild(btn);
  });
}

function guideStepsFor(mode) {
  switch (mode) {
    case "custom_openai":
    case "zhipu_coding":
      return ["填写上游 <strong>URL</strong> 和 <strong>API Key</strong>", "点击 <strong>列出模型</strong> 并选择默认模型", "保存后如已切换方式将 <strong>自动重启</strong>", "再点击 <strong>配置 Codex</strong>"];
    case "openai_oauth":
    case "xai_oauth":
    case "anthropic_oauth":
      return ["Max/Pro 点 <strong>登录</strong> 走 Claude.ai", "没有订阅则填写 <strong>API Key</strong>", "选择 <strong>默认模型</strong> 并保存", "再点击 <strong>配置 Codex</strong>"];
    case "kimi_web":
      return ["点击 <strong>登录</strong>，自动打开 Kimi 授权页", "在网页里输入 <strong>验证码</strong> 并确认授权", "授权完成后自动保存登录并 <strong>重启</strong>", "选择 <strong>默认模型</strong> 并配置 Codex"];
    default:
      return ["确认 <strong>Codex Desktop</strong> 已安装", "使用当前登录方式完成认证", "选择 <strong>默认模型</strong> 并保存", "点击 <strong>配置 Codex</strong> 后启动 Desktop"];
  }
}

function renderGuide(mode) {
  const list = $("guideSteps");
  list.innerHTML = "";
  guideStepsFor(mode).forEach((html) => {
    const li = document.createElement("li");
    li.innerHTML = `<span>${html}</span>`;
    list.appendChild(li);
  });
}

function normalizeModelList(models) {
  return (models || []).map((model) => ({
    id: pick(model, "id", "ID") || "",
    name: pick(model, "name", "Name") || pick(model, "id", "ID") || "",
  })).filter((model) => model.id);
}

function renderModels(models, selected) {
  const select = $("modelID");
  if (!select) return;
  const list = normalizeModelList(models);
  select.innerHTML = "";
  if (!list.length) {
    const option = document.createElement("option");
    option.value = selected || "";
    option.textContent = selected || "请先获取模型列表";
    select.appendChild(option);
    select.disabled = !selected;
    return;
  }
  select.disabled = false;
  list.forEach((model) => {
    const option = document.createElement("option");
    option.value = model.id;
    option.textContent = model.name || model.id;
    select.appendChild(option);
  });
  select.value = selected && list.some((m) => m.id === selected) ? selected : list[0].id;
}

function modeFieldsHTML(mode, s, loggedIn) {
  const url = pick(s, "base_url", "BaseURL") || "";
  const email = pick(s, "email", "Email") || "";
  const keySet = !!(lastStatus && pick(lastStatus, "upstream_key_set", "UpstreamKeySet"));
  const keyPlaceholder = keySet ? "已保存" : "";
  const modelSelect = `<div class="field"><label>默认模型</label><select id="modelID"></select></div>`;
  const modelSelectWithList = (actionLabel) => `<div class="field"><label>默认模型</label><div class="inline"><select id="modelID"></select><button id="listModelsBtn" class="ghost" type="button">${actionLabel}</button></div></div>`;
  if (mode === "sso") {
    return `
      <div class="field"><label>上游 Base URL</label><input id="baseURL" value="${escapeAttr(url)}" disabled /></div>
      <div class="two-fields">${loggedIn ? modelSelectWithList("刷新") : modelSelect}<div class="field"><label>账号</label><input id="email" value="${escapeAttr(email)}" disabled /></div></div>`;
  }
  if (mode === "openai_oauth" || mode === "xai_oauth") {
    const waiting = !loggedIn && ((mode === "xai_oauth" && xaiWaiting) || (mode === "openai_oauth" && openaiWaiting));
    const codeId = mode === "openai_oauth" ? "openaiCode" : "xaiCode";
    const btnId = mode === "openai_oauth" ? "completeOpenAIBtn" : "completeXaiBtn";
    const waitingHint = mode === "openai_oauth"
      ? "已在本机 localhost 监听回调。若浏览器停在授权页，可把地址栏或验证码粘贴到下面。"
      : "已在本机监听回调，登录后会自动完成。若未自动跳转，可粘贴验证码。";
    const idleHint = mode === "openai_oauth"
      ? "点击右上角登录。授权完成后会自动回调 localhost。"
      : "点击右上角登录。xAI 可能显示验证码，也可等待本机 127.0.0.1 自动回调。";
    return `
      <div class="field"><label>上游 Base URL</label><input id="baseURL" value="${escapeAttr(url)}" disabled /></div>
      ${loggedIn ? `<div class="oauth-status">OAuth 已认证${email ? " · " + escapeAttr(email) : ""}</div>` : waiting ? `
        <p class="sub">${waitingHint}</p>
        <div class="field"><label>授权码</label><div class="inline wide-action"><input id="${codeId}" placeholder="粘贴 code 或回调 URL" spellcheck="false" /><button id="${btnId}" class="ghost" type="button">完成</button></div></div>` : `<p class="sub">${idleHint}</p>`}
      ${loggedIn ? modelSelectWithList("刷新") : modelSelect}`;
  }
  if (mode === "anthropic_oauth") {
    return `
      <div class="field"><label>上游 Base URL</label><input id="baseURL" value="${escapeAttr(url)}" disabled /></div>
      ${loggedIn ? `<div class="oauth-status">Claude 已登录</div>` : `<p class="sub">Claude Code 账号登录需要 Max 或 Pro。网页提示升级时，请改用 Anthropic API Key（sk-ant-...）。</p>`}
      ${loggedIn ? "" : `
        <div class="field"><label>授权码 <span class="label-hint">仅 Max/Pro 账号登录后粘贴</span></label><div class="inline wide-action"><input id="anthropicCode" placeholder="粘贴 authorization code" spellcheck="false" /><button id="completeAnthropicBtn" class="ghost" type="button">完成</button></div></div>
        <div class="field"><label>Anthropic API Key <span class="label-hint">没有 Max/Pro 时用这个</span></label><input id="upstreamKey" placeholder="${escapeAttr(keyPlaceholder)}" spellcheck="false" /></div>`}
      ${modelSelectWithList("列出")}`;
  }
  if (mode === "kimi_web") {
    const waiting = !loggedIn && kimiWaiting;
    const hint = kimiHint || "";
    return `
      <div class="field"><label>上游 Base URL</label><input id="baseURL" value="${escapeAttr(url)}" disabled /></div>
      ${loggedIn ? `<div class="oauth-status">Kimi Code 已登录</div>` : waiting ? `
        <p class="sub">已打开 Kimi 授权页，请输入验证码并确认授权，完成后会自动保存。</p>
        <div class="field"><label>验证码</label><input id="kimiCode" value="${escapeAttr(hint)}" readonly /></div>
        <button id="cancelKimiBtn" class="ghost" type="button">取消登录</button>` : `<p class="sub">点击右上角登录，浏览器会打开 Kimi 授权页，无需 API Key。</p>`}
      ${loggedIn ? modelSelectWithList("刷新") : modelSelect}`;
  }
  return `
    <div class="field"><label>上游 Base URL</label><input id="baseURL" value="${escapeAttr(url)}" spellcheck="false" /></div>
    <div class="field"><label>上游 API Key <span class="label-hint">只给上游用，和本地 Key 分开</span></label><input id="upstreamKey" placeholder="${escapeAttr(keyPlaceholder)}" spellcheck="false" /></div>
    ${modelSelectWithList("列出")}`;
}

function escapeAttr(value) {
  return String(value || "").replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch]));
}

function bindModeFieldActions() {
  const listBtn = $("listModelsBtn");
  if (listBtn) listBtn.addEventListener("click", () => listModels().catch((err) => notify(errorMessage(err), "error")));
  const kimiCancelBtn = $("cancelKimiBtn");
  if (kimiCancelBtn) kimiCancelBtn.addEventListener("click", () => cancelKimiLogin().catch((err) => notify(errorMessage(err), "error")));
  const anthBtn = $("completeAnthropicBtn");
  if (anthBtn) anthBtn.addEventListener("click", () => completeAnthropic().catch((err) => notify(errorMessage(err), "error")));
  const xaiBtn = $("completeXaiBtn");
  if (xaiBtn) xaiBtn.addEventListener("click", () => completeXaiCode().catch((err) => notify(errorMessage(err), "error")));
  const openaiBtn = $("completeOpenAIBtn");
  if (openaiBtn) openaiBtn.addEventListener("click", () => completeOpenAICode().catch((err) => notify(errorMessage(err), "error")));
}

function renderModeFields(status) {
  const s = status.settings || status.Settings || {};
  const mode = pick(status, "active_auth_mode", "ActiveAuthMode") || currentAuthMode;
  $("modeFields").innerHTML = modeFieldsHTML(mode, s, !!pick(status, "logged_in", "LoggedIn"));
  renderModels(pick(s, "models", "Models") || [], pick(s, "model_id", "ModelID") || "");
  bindModeFieldActions();
}

async function maybeRefreshSSOModels(status) {
  const s = status.settings || status.Settings || {};
  const mode = pick(status, "active_auth_mode", "ActiveAuthMode") || currentAuthMode;
  if (mode !== "sso" || !pick(status, "logged_in", "LoggedIn")) return;
  if (normalizeModelList(pick(s, "models", "Models") || []).length > 0) return;
  if (!$("modelID")) return;
  try {
    const models = await api.ListUpstreamModels($("baseURL")?.value || "", "");
    if (($("authMode")?.value || currentAuthMode) !== "sso") return;
    renderModels(normalizeModelList(models), $("modelID")?.value || pick(s, "model_id", "ModelID") || "");
  } catch {
    /* keep current dropdown */
  }
}

async function listModels() {
  const btn = $("listModelsBtn");
  const busyLabel = (btn && btn.textContent.trim().length <= 2) ? btn.textContent : "列出中...";
  setBusy(btn, true, busyLabel);
  try {
    const models = await api.ListUpstreamModels($("baseURL")?.value || "", $("upstreamKey")?.value || "");
    const list = normalizeModelList(models);
    renderModels(list, $("modelID")?.value || "");
    notify(`已列出 ${list.length} 个模型`);
  } finally {
    setBusy(btn, false);
  }
}

async function completeOpenAICode() {
  const code = $("openaiCode")?.value || "";
  if (!code.trim()) throw new Error("请粘贴 OpenAI 授权码或回调 URL");
  const btn = $("completeOpenAIBtn");
  setBusy(btn, true, "完成");
  try {
    const status = await api.CompleteOpenAIOAuthWithCode(code.trim());
    openaiWaiting = false;
    if (handleRelaunch(status)) return;
    await refresh();
    notify("OpenAI 登录成功");
  } finally {
    setBusy(btn, false);
  }
}

async function completeXaiCode() {
  const code = $("xaiCode")?.value || "";
  if (!code.trim()) throw new Error("请粘贴 xAI 验证码");
  const btn = $("completeXaiBtn");
  setBusy(btn, true, "登录中...");
  try {
    const status = await api.CompleteXAIOAuthWithCode(code.trim());
    xaiWaiting = false;
    if (handleRelaunch(status)) return;
    await refresh();
    notify("Grok 登录成功");
  } finally {
    setBusy(btn, false);
  }
}

async function cancelKimiLogin() {
  await api.CancelKimiWebLogin();
  kimiWaiting = false;
  kimiHint = "";
  await refresh();
  notify("已取消 Kimi 登录");
}

async function completeAnthropic() {
  const code = $("anthropicCode")?.value || "";
  if (!code.trim()) throw new Error("请粘贴授权码");
  const status = await api.CompleteAnthropicOAuth(code.trim());
  if (handleRelaunch(status)) return;
  await refresh();
  notify("Claude Code 登录成功");
}

function showOnboarding(show) {
  $("onboarding").hidden = !show;
  $("main").hidden = show;
}

async function refresh() {
  const gen = ++refreshGen;
  try {
    const snap = snapshotModeInputs();
    const previousMode = currentAuthMode;
    if (!authModes.length) {
      authModes = (await api.AuthModes() || []).map((mode) => ({
        id: pick(mode, "id", "ID"),
        name: pick(mode, "name", "Name"),
        hint: pick(mode, "hint", "Hint"),
        kind: pick(mode, "kind", "Kind"),
        icon: pick(mode, "icon", "Icon"),
      }));
    }
    const status = await api.Status();
    if (gen !== refreshGen) return;
    lastStatus = status;
    const s = status.settings || status.Settings || {};
    currentAuthMode = pick(status, "active_auth_mode", "ActiveAuthMode") || pick(s, "active_auth_mode", "ActiveAuthMode") || "";
    if (pick(status, "needs_onboarding", "NeedsOnboarding")) {
      showOnboarding(true);
      if (!selectedOnboardingMode) selectedOnboardingMode = authModes[0]?.id || "sso";
      renderOnboardingCards();
      return;
    }
    showOnboarding(false);
    renderAuthModeOptions(currentAuthMode);
    switchingAuthMode = true;
    $("authMode").value = currentAuthMode;
    switchingAuthMode = false;
    $("listenAddress").value = pick(s, "listen_address", "ListenAddress") || "";
    $("apiKey").value = pick(s, "api_key", "APIKey") || "";
    $("codexContextWindow").value = pick(s, "codex_context_window", "CodexContextWindow") || "";
    $("codexAutoCompactTokenLimit").value = pick(s, "codex_auto_compact_token_limit", "CodexAutoCompactTokenLimit") || "";
    renderModeFields(status);
    if (previousMode && previousMode === currentAuthMode) restoreModeInputs(snap);
    renderGuide(currentAuthMode);
    maybeRefreshSSOModels(status);
    updateRuntimeChrome(status, { autoStart: true });
  } catch (err) {
    $("statusBadge").textContent = String(err);
    $("statusBadge").className = "badge warn";
  }
}

let lastLanKey = "";

function updateRuntimeChrome(status, opts = {}) {
  if (!status) return;
  lastStatus = status;
  const s = status.settings || status.Settings || {};
  const mode = pick(status, "active_auth_mode", "ActiveAuthMode") || currentAuthMode;
  const loggedIn = !!pick(status, "logged_in", "LoggedIn");
  $("openaiURL").textContent = pick(status, "openai_url", "OpenAIURL") || "";
  $("anthropicURL").textContent = pick(status, "anthropic_url", "AnthropicURL") || "";
  $("healthURL").textContent = pick(status, "health_url", "HealthURL") || "";
  if (opts.autoStart) {
    const autoStart = $("autoStart");
    if (autoStart) {
      const autoStartRow = autoStart.closest(".check-row");
      autoStart.checked = !!pick(status, "auto_start_enabled", "AutoStartEnabled");
      autoStart.disabled = !pick(status, "auto_start_supported", "AutoStartSupported");
      if (autoStartRow) {
        autoStartRow.classList.toggle("disabled", autoStart.disabled);
        autoStartRow.title = autoStart.disabled ? "仅 Windows 平台支持开机自动启动" : "";
      }
    }
  }
  const loginChip = $("loginChip");
  if (loginChip) {
    const label = pick(status, "auth_mode_label", "AuthModeLabel") || "未选择";
    const email = pick(s, "email", "Email") || "";
    loginChip.textContent = loggedIn ? (email ? `${label} · ${email}` : `${label} · 已登录`) : (mode ? `${label} · 未登录` : "未登录");
    loginChip.className = `chip ${loggedIn ? "ok" : "muted"}`;
  }
  if ($("loginActionBtn") && !loginInProgress) {
    $("loginActionBtn").style.display = loggedIn ? "none" : "";
    $("loginActionBtn").textContent = mode === "sso" ? "SSO 登录" : "登录";
  }
  if ($("logoutBtn")) $("logoutBtn").style.display = loggedIn ? "" : "none";
  $("cacheEntries").textContent = String(pick(status, "cache_entries", "CacheEntries") || 0);
  $("cacheBytes").textContent = formatBytes(pick(status, "cache_bytes", "CacheBytes") || 0);
  const hits = pick(status, "cache_hits", "CacheHits") || 0;
  const misses = pick(status, "cache_misses", "CacheMisses") || 0;
  $("cacheHits").textContent = String(hits);
  $("cacheMisses").textContent = String(misses);
  $("cacheHitRate").textContent = hits + misses > 0 ? Math.round((hits / (hits + misses)) * 100) + "%" : "-";
  const cacheDecision = formatCacheDecision(status);
  $("cacheDecisionSummary").textContent = cacheDecision.summary;
  $("cacheDecisionExtra").textContent = cacheDecision.reason ? ` · ${cacheDecision.reason}` : "";
  const badge = $("statusBadge");
  if (badge) {
    const running = !!pick(status, "running", "Running");
    badge.textContent = pick(status, "last_error", "LastError") || (running ? (loggedIn ? "运行中" : "等待登录") : "未运行");
    badge.className = `badge ${running && loggedIn ? "ok" : "warn"}`;
  }
  const lan = $("lanURLs");
  if (lan) {
    const urls = pick(status, "lan_urls", "LANURLs") || [];
    const key = urls.join("\n");
    if (key !== lastLanKey) {
      lastLanKey = key;
      lan.innerHTML = "";
      urls.forEach((url) => {
        const item = document.createElement("code");
        item.textContent = `${url}/v1`;
        item.classList.add("clickable");
        item.title = "点击复制";
        item.addEventListener("click", copyText);
        lan.appendChild(item);
      });
    }
  }
}

async function refreshChrome() {
  try {
    const status = await api.Status();
    updateRuntimeChrome(status);
  } catch {
    /* ignore background chrome updates */
  }
}

async function refreshModelsChrome() {
  try {
    const status = await api.Status();
    updateRuntimeChrome(status);
    const s = status.settings || status.Settings || {};
    const mode = pick(status, "active_auth_mode", "ActiveAuthMode") || currentAuthMode;
    if (mode !== currentAuthMode || !$("modelID")) return;
    const selected = $("modelID").value || pick(s, "model_id", "ModelID") || "";
    renderModels(pick(s, "models", "Models") || [], selected);
  } catch {
    /* ignore */
  }
}

async function copyText(event) {
  const el = event.currentTarget;
  try {
    await navigator.clipboard.writeText(el.textContent || "");
    el.classList.add("copied");
    setTimeout(() => el.classList.remove("copied"), 600);
    notify("已复制");
  } catch {
    notify("复制失败，请手动选中复制", "error");
  }
}

async function save(options = {}) {
  const saveBtn = $("saveBtn");
  setBusy(saveBtn, true, "保存中...");
  try {
    const codexContextWindow = Number($("codexContextWindow").value);
    const codexAutoCompactTokenLimit = Number($("codexAutoCompactTokenLimit").value);
    if (!Number.isSafeInteger(codexContextWindow) || !Number.isSafeInteger(codexAutoCompactTokenLimit) || codexContextWindow <= 0 || codexAutoCompactTokenLimit <= 0) {
      throw new Error("Codex 上下文长度和压缩启动长度必须为正整数");
    }
    if (codexAutoCompactTokenLimit >= codexContextWindow) {
      throw new Error("Codex 压缩启动长度必须小于上下文长度");
    }
    const payload = {
      listen_address: $("listenAddress").value,
      api_key: $("apiKey").value,
      base_url: $("baseURL")?.value || "",
      model_id: $("modelID")?.value || "",
      models: collectModels(),
      codex_context_window: codexContextWindow,
      codex_auto_compact_token_limit: codexAutoCompactTokenLimit,
      email: $("email")?.value || "",
      active_auth_mode: options.authMode || $("authMode").value,
      upstream_api_key: $("upstreamKey")?.value || "",
    };
    const status = await api.SaveSettings(payload);
    if (options.silent) return status;
    await refresh();
    const notice = codexSyncNotice("已保存", status?.codex_credential_sync);
    notify(notice.message, notice.kind);
    return status;
  } finally {
    setBusy(saveBtn, false);
  }
}

async function runLogin() {
  if (loginInProgress) return;
  loginInProgress = true;
  const loginBtn = $("loginActionBtn");
  setBusy(loginBtn, true, "等待登录...");
  try {
    const mode = $("authMode").value || currentAuthMode;
    if (mode === "sso") {
      await save({ silent: true });
      await api.StartSSOLogin();
      notify("已打开 SSO 登录页面，请在浏览器中完成登录。");
      try {
        const status = await api.CompleteSSOLogin();
        if (handleRelaunch(status)) return;
        await refresh();
        notify("SSO 登录成功，已刷新模型列表");
      } catch (err) {
        await refresh();
        if (($("authMode")?.value || currentAuthMode) !== "sso") return;
        throw err;
      }
      return;
    }
    if (mode === "openai_oauth") {
      const start = await api.StartOpenAIOAuth();
      openaiWaiting = true;
      await refresh();
      const callback = pick(start, "callback_url", "CallbackURL") || "http://localhost:1455/auth/callback";
      notify(`已打开 OpenAI。本机回调 ${callback}。若未自动跳转，请粘贴授权码。`);
      try {
        const status = await api.CompleteOpenAIOAuth();
        openaiWaiting = false;
        if (handleRelaunch(status)) return;
        await refresh();
        notify("OpenAI 登录成功");
      } catch (err) {
        openaiWaiting = false;
        await refresh();
        if (($("authMode")?.value || currentAuthMode) !== "openai_oauth") return;
        throw err;
      }
      return;
    }
    if (mode === "xai_oauth") {
      const start = await api.StartXAIOAuth();
      xaiWaiting = true;
      await refresh();
      const callback = pick(start, "callback_url", "CallbackURL") || "http://127.0.0.1/callback";
      notify(`已打开 xAI。本机回调 ${callback}。若页面显示验证码，请粘贴到配置区。`);
      try {
        const status = await api.CompleteXAIOAuth();
        xaiWaiting = false;
        if (handleRelaunch(status)) return;
        await refresh();
        notify("Grok 登录成功");
      } catch (err) {
        xaiWaiting = false;
        await refresh();
        if (($("authMode")?.value || currentAuthMode) !== "xai_oauth") return;
        throw err;
      }
      return;
    }
    if (mode === "anthropic_oauth") {
      await api.StartAnthropicOAuth();
      notify("已打开 Claude.ai，授权后把 code 粘贴到配置区。");
      return;
    }
    if (mode === "kimi_web") {
      const device = await api.StartKimiWebLogin();
      kimiWaiting = true;
      const userCode = pick(device, "user_code", "UserCode") || "";
      const approvalURL = pick(device, "verification_uri_complete", "VerificationURIComplete")
        || pick(device, "verification_uri", "VerificationURI") || "";
      const opened = pick(device, "browser_opened", "BrowserOpened");
      kimiHint = userCode;
      await refresh();
      const prefix = opened
        ? "已打开 Kimi 授权页。"
        : `浏览器未能自动打开，请手动访问：${approvalURL}`;
      notify(`${prefix} 验证码：${userCode}`);
      try {
        const status = await api.WaitKimiWebLogin();
        kimiWaiting = false;
        kimiHint = "";
        // Show the restart notice before any re-render, because the process
        // is about to replace this window.
        showRelaunchNotice();
        if (handleRelaunch(status)) return;
        await refresh();
        notify("Kimi Code 登录成功");
      } catch (err) {
        kimiWaiting = false;
        kimiHint = "";
        await refresh();
        if (($("authMode")?.value || currentAuthMode) !== "kimi_web") return;
        throw err;
      }
      return;
    }
    notify("请填写上游 URL 和 API Key，然后保存或列出模型。");
  } catch (err) {
    notify(errorMessage(err), "error");
  } finally {
    loginInProgress = false;
    setBusy(loginBtn, false);
  }
}

$("saveBtn").addEventListener("click", () => save().catch((err) => notify(errorMessage(err), "error")));
$("loginActionBtn").addEventListener("click", () => runLogin());
$("logoutBtn").addEventListener("click", async () => {
  try { await api.Logout(); await refresh(); notify("已退出当前登录方式"); } catch (err) { notify(errorMessage(err), "error"); }
});
$("genKeyBtn").addEventListener("click", async () => {
  const btn = $("genKeyBtn"); btn.disabled = true;
  try {
    const result = await api.GenerateAPIKey();
    $("apiKey").value = result.api_key;
    await refresh();
    const notice = codexSyncNotice("已生成并保存新的本地 API Key", result.codex_credential_sync);
    notify(notice.message, notice.kind);
  } catch (err) { notify(errorMessage(err), "error"); }
  finally { btn.disabled = false; }
});
$("hideBtn").addEventListener("click", async () => { await api.WindowHide(); });
$("autoStart").addEventListener("change", async (event) => {
  const checkbox = event.currentTarget;
  checkbox.disabled = true;
  try {
    const status = await api.SetAutoStartEnabled(checkbox.checked);
    checkbox.checked = !!status.auto_start_enabled;
    notify(checkbox.checked ? "已开启开机自动启动" : "已关闭开机自动启动");
  } catch (err) {
    checkbox.checked = !checkbox.checked;
    notify(errorMessage(err), "error");
  } finally {
    await refresh();
    checkbox.disabled = checkbox.closest(".check-row").classList.contains("disabled");
  }
});
$("authMode").addEventListener("change", async () => {
  if (switchingAuthMode) return;
  const mode = $("authMode").value;
  if (mode === currentAuthMode) return;
  const label = modeMeta(mode).name;
  const ok = await confirmDialog(`切换到「${label}」后将重启 CodexProxy 使新上游生效。该方式已保存的登录会保留。是否继续？`, "切换登录方式");
  if (!ok) {
    switchingAuthMode = true;
    $("authMode").value = currentAuthMode;
    switchingAuthMode = false;
    return;
  }
  try {
    await save({ silent: true, authMode: currentAuthMode }).catch(() => {});
    const status = await api.SwitchAuthMode(mode);
    if (handleRelaunch(status)) return;
    await refresh();
    notify(`已切换到 ${label}，请完成登录。完成后将自动重启。`);
  } catch (err) {
    switchingAuthMode = true;
    $("authMode").value = currentAuthMode;
    switchingAuthMode = false;
    notify(errorMessage(err), "error");
  }
});
$("onboardingContinue").addEventListener("click", async () => {
  if (!selectedOnboardingMode) selectedOnboardingMode = authModes[0]?.id;
  const btn = $("onboardingContinue");
  setBusy(btn, true, "正在进入...");
  try {
    await api.SelectAuthMode(selectedOnboardingMode);
    await refresh();
    notify(`已选择 ${modeMeta(selectedOnboardingMode).name}`);
  } catch (err) {
    notify(errorMessage(err), "error");
  } finally {
    setBusy(btn, false);
  }
});
document.querySelectorAll("code.clickable").forEach((el) => el.addEventListener("click", copyText));
$("configCodexBtn").addEventListener("click", async () => {
  const btn = $("configCodexBtn");
  setBusy(btn, true, "配置中...");
  try {
    const msg = await api.ConfigureCodex();
    notify(msg && msg.includes("未检测到") ? msg : (msg || "Codex 配置已写入 ~/.codex/"), msg && msg.includes("未检测到") ? "error" : "ok");
    checkCodexInstalled();
  } catch (err) { notify(errorMessage(err), "error"); }
  finally { setBusy(btn, false); }
});
$("restoreCodexBtn").addEventListener("click", async () => {
  const btn = $("restoreCodexBtn");
  if (!await confirmDialog("将移除第三方服务商及代理密钥，并把所有 Codex 会话恢复为 OpenAI。是否继续？", "恢复 Codex")) return;
  setBusy(btn, true, "恢复中...");
  try {
    const msg = await api.RestoreCodex();
    notify(msg || "Codex 已恢复为 OpenAI。");
  } catch (err) { notify(errorMessage(err), "error"); }
  finally { setBusy(btn, false); }
});
$("installCodexBtn").addEventListener("click", async () => {
  const btn = $("installCodexBtn");
  if (btn.dataset.installing) return;
  btn.dataset.installing = "1";
  btn.style.display = "none";
  const progressEl = $("codexProgress");
  progressEl.style.display = "";
  progressEl.className = "codex-progress";
  $("codexProgressMsg").textContent = "准备安装...";
  $("codexProgressPct").textContent = "";
  $("codexProgressFill").style.width = "5%";
  try {
    await api.InstallCodexDesktop();
    pollCodexInstalled(60, 5000);
  } catch (err) {
    progressEl.className = "codex-progress error";
    $("codexProgressMsg").textContent = errorMessage(err);
    $("codexProgressPct").textContent = "失败";
    $("codexProgressFill").style.width = "100%";
    notify(errorMessage(err), "error");
    btn.style.display = "";
    delete btn.dataset.installing;
    setTimeout(() => { progressEl.style.display = "none"; }, 8000);
  }
});
if (window.runtime && window.runtime.EventsOn) {
  window.runtime.EventsOn("codex-install-progress", (data) => {
    const progressEl = $("codexProgress");
    if (!data || !progressEl) return;
    progressEl.style.display = "";
    const pct = Math.round(data.percent || 0);
    $("codexProgressMsg").textContent = data.message || "";
    $("codexProgressPct").textContent = pct > 0 ? pct + "%" : "";
    $("codexProgressFill").style.width = Math.max(pct, 5) + "%";
    if (data.phase === "done") {
      if (progressEl.classList.contains("done")) return;
      progressEl.className = "codex-progress done";
      $("codexProgressPct").textContent = "OK";
      $("codexProgressFill").style.width = "100%";
      notify(data.message || "Codex Desktop 安装完成！");
      $("installCodexBtn").style.display = "none";
      delete $("installCodexBtn").dataset.installing;
      setTimeout(() => { progressEl.style.display = "none"; }, 6000);
    } else if (data.phase === "error") {
      if (progressEl.classList.contains("done") || progressEl.classList.contains("error")) return;
      progressEl.className = "codex-progress error";
      $("codexProgressPct").textContent = "ERR";
      $("codexProgressFill").style.width = "100%";
      $("installCodexBtn").style.display = "";
      delete $("installCodexBtn").dataset.installing;
      notify(data.message || "安装失败", "error");
      setTimeout(() => { progressEl.style.display = "none"; }, 8000);
    }
  });
  window.runtime.EventsOn("token-stats-updated", () => scheduleTokenStatsRefresh());
  window.runtime.EventsOn("cache-stats-updated", () => scheduleTokenStatsRefresh());
  window.runtime.EventsOn("cache-decision-updated", () => { refreshChrome(); });
  window.runtime.EventsOn("models-refreshed", () => { refreshModelsChrome(); });
}
function pollCodexInstalled(remaining, intervalMs) {
  if (remaining <= 0) return;
  setTimeout(async () => {
    const installed = await api.IsCodexInstalled().catch(() => false);
    if (installed) {
      const progressEl = $("codexProgress");
      const btn = $("installCodexBtn");
      if (progressEl && !progressEl.classList.contains("done")) {
        progressEl.className = "codex-progress done";
        $("codexProgressPct").textContent = "OK";
        $("codexProgressFill").style.width = "100%";
        $("codexProgressMsg").textContent = "Codex Desktop 安装成功！";
        notify("Codex Desktop 安装完成！现在可以点击「配置 Codex」。");
        setTimeout(() => { progressEl.style.display = "none"; }, 6000);
      }
      btn.style.display = "none";
      delete btn.dataset.installing;
    } else {
      pollCodexInstalled(remaining - 1, intervalMs);
    }
  }, intervalMs);
}
async function checkCodexInstalled() {
  try {
    const installed = await api.IsCodexInstalled();
    $("installCodexBtn").style.display = installed ? "none" : "";
  } catch {
    $("installCodexBtn").style.display = "";
  }
}
$("clearCacheBtn").addEventListener("click", async () => {
  const btn = $("clearCacheBtn");
  setBusy(btn, true, "清除中...");
  try { await api.ClearCache(); await refresh(); notify("缓存已清除"); }
  catch (err) { notify(errorMessage(err), "error"); }
  finally { setBusy(btn, false); }
});
let tokenStatsRefreshTimer = null;
function scheduleTokenStatsRefresh() {
  if (tokenStatsRefreshTimer) return;
  tokenStatsRefreshTimer = setTimeout(() => { tokenStatsRefreshTimer = null; refreshTokenStats(); }, 2000);
}
async function refreshTokenStats() {
  if (!api || !api.TokenStats) return;
  try {
    const stats = await api.TokenStats(currentTokenPeriod);
    $("promptTokens").textContent = formatTokenCount(stats.prompt_tokens || 0);
    $("completionTokens").textContent = formatTokenCount(stats.completion_tokens || 0);
    $("totalTokens").textContent = formatTokenCount(stats.total_tokens || 0);
    const hasSavings = (stats.total_before_cache || 0) > (stats.total_tokens || 0);
    $("promptBefore").textContent = hasSavings ? formatTokenCount(stats.prompt_before_cache || 0) : "";
    $("completionBefore").textContent = hasSavings ? formatTokenCount(stats.completion_before_cache || 0) : "";
    $("totalBefore").textContent = hasSavings ? formatTokenCount(stats.total_before_cache || 0) : "";
    const pct = stats.cache_saving_pct || 0;
    $("cacheSaving").textContent = pct > 0 ? pct.toFixed(0) + "%" : "-";
  } catch { /* ignore */ }
}
document.querySelectorAll(".period-btn").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll(".period-btn").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    currentTokenPeriod = btn.dataset.period;
    refreshTokenStats();
  });
});
refresh();
checkCodexInstalled();
refreshTokenStats();
