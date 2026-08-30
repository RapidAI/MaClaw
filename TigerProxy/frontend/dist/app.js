const api = window.go?.main?.App;
const $ = (id) => document.getElementById(id);
let toastTimer;
let loginInProgress = false;
let currentTokenPeriod = "today";

function formatCacheDecision(status) {
  const protocol = status.last_cache_protocol || "";
  const outcome = status.last_cache_outcome || "";
  const streaming = !!status.last_cache_streaming;
  if (!protocol || !outcome) return { summary: "-", reason: "" };
  const mode = streaming ? " / stream" : "";
  return {
    summary: `${protocol} / ${outcome}${mode}`,
    reason: status.last_cache_reason || "",
  };
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

function notify(message, kind = "ok") {
  const toast = $("toast");
  toast.textContent = message;
  toast.className = `toast show ${kind}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toast.className = "toast"; }, 4200);
}

function errorMessage(err) {
  return err?.message || String(err || "操作失败");
}

function setBusy(button, busy, text) {
  button.disabled = busy;
  if (busy) {
    button.dataset.originalText = button.textContent;
    button.textContent = text;
  } else if (button.dataset.originalText) {
    button.textContent = button.dataset.originalText;
    delete button.dataset.originalText;
  }
}

function codexSyncNotice(successMessage, sync) {
  if (!sync) return { message: successMessage, kind: "ok" };
  if (sync.error) {
    return {
      message: `${successMessage}；Codex 凭据未同步：${sync.error}。请修复 ~/.codex/auth.json 后重新配置 Codex。`,
      kind: "error",
    };
  }
  if (!sync.configured) return { message: successMessage, kind: "ok" };
  if (sync.updated) return { message: `${successMessage}；Codex 凭据已同步，请重启 Codex。`, kind: "ok" };
  return { message: `${successMessage}；Codex 凭据已是最新，请重启 Codex。`, kind: "ok" };
}

async function refresh() {
  try {
    const status = await api.Status();
    const s = status.settings || {};
    $("listenAddress").value = s.listen_address || "";
    $("apiKey").value = s.api_key || "";
    $("baseURL").value = s.base_url || "";
    renderModels(s.models || [], s.model_id || "");
    $("codexContextWindow").value = s.codex_context_window || "";
    $("codexAutoCompactTokenLimit").value = s.codex_auto_compact_token_limit || "";
    $("email").value = s.email || "";
    $("openaiURL").textContent = status.openai_url || "";
    $("anthropicURL").textContent = status.anthropic_url || "";
    $("healthURL").textContent = status.health_url || "";
    const autoStart = $("autoStart");
    const autoStartRow = autoStart.closest(".check-row");
    autoStart.checked = !!status.auto_start_enabled;
    autoStart.disabled = !status.auto_start_supported;
    autoStartRow.classList.toggle("disabled", !status.auto_start_supported);
    autoStartRow.title = status.auto_start_supported ? "" : "仅 Windows 平台支持开机自动启动";
    const loginChip = $("loginChip");
    loginChip.textContent = status.logged_in ? (s.email || "已登录") : "未登录";
    loginChip.className = `chip ${status.logged_in ? "ok" : "muted"}`;
    $("loginBtn").style.display = status.logged_in ? "none" : "";
    $("logoutBtn").style.display = status.logged_in ? "" : "none";
    $("cacheEntries").textContent = String(status.cache_entries || 0);
    $("cacheBytes").textContent = formatBytes(status.cache_bytes || 0);
    const hits = status.cache_hits || 0;
    const misses = status.cache_misses || 0;
    const total_cache = hits + misses;
    if ($("cacheHits")) $("cacheHits").textContent = String(hits);
    if ($("cacheMisses")) $("cacheMisses").textContent = String(misses);
    $("cacheHitRate").textContent = total_cache > 0 ? Math.round((hits / total_cache) * 100) + "%" : "-";
    // Single-line cache decision only — multi-line reason/hint causes layout flash on refresh.
    const cacheDecision = formatCacheDecision(status);
    if ($("cacheDecisionSummary")) $("cacheDecisionSummary").textContent = cacheDecision.summary;
    const extra = $("cacheDecisionExtra");
    if (extra) {
      extra.textContent = cacheDecision.reason ? ` · ${cacheDecision.reason}` : "";
    }
    const diag = $("cacheDiagnostic");
    if (diag) {
      // Longer explanations stay in title tooltip to avoid height jumps.
      let tip = cacheDecision.summary || "";
      if (cacheDecision.reason) tip += ` (${cacheDecision.reason})`;
      if (hits === 0 && misses > 0 && (status.cache_entries || 0) > 0) {
        tip += " — 精确匹配整包请求；多轮 messages 变化时难命中，相同请求重试应 HIT。";
      }
      diag.title = tip;
    }
    const badge = $("statusBadge");
    badge.textContent = status.last_error || (status.running ? (status.logged_in ? "运行中" : "等待登录") : "未运行");
    badge.className = `badge ${status.running && status.logged_in ? "ok" : "warn"}`;
    const lan = $("lanURLs");
    lan.innerHTML = "";
    (status.lan_urls || []).forEach((url) => {
      const item = document.createElement("code");
      item.textContent = `${url}/v1`;
      item.classList.add("clickable");
      item.title = "点击复制";
      item.addEventListener("click", async () => {
        try {
          await navigator.clipboard.writeText(item.textContent || "");
          item.classList.add("copied");
          setTimeout(() => item.classList.remove("copied"), 600);
          notify("已复制");
        } catch { notify("复制失败，请手动选中复制", "error"); }
      });
      lan.appendChild(item);
    });
  } catch (err) {
    $("statusBadge").textContent = String(err);
    $("statusBadge").className = "badge warn";
  }
}

function renderModels(models, selected) {
  const select = $("modelID");
  select.innerHTML = "";
  if (!models.length) {
    const option = document.createElement("option");
    option.value = selected || "";
    option.textContent = selected || "请先 SSO 登录获取模型列表";
    select.appendChild(option);
    select.disabled = true;
    return;
  }
  select.disabled = false;
  models.forEach((model) => {
    const option = document.createElement("option");
    option.value = model.id;
    option.textContent = model.name || model.id;
    select.appendChild(option);
  });
  select.value = selected && models.some((m) => m.id === selected) ? selected : models[0].id;
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
    const status = await api.SaveSettings({
      listen_address: $("listenAddress").value,
      api_key: $("apiKey").value,
      base_url: $("baseURL").value,
      model_id: $("modelID").value,
      codex_context_window: codexContextWindow,
      codex_auto_compact_token_limit: codexAutoCompactTokenLimit,
      email: $("email").value,
    });
    await refresh();
    if (!options.silent) {
      const notice = codexSyncNotice("已保存", status?.codex_credential_sync);
      notify(notice.message, notice.kind);
    }
  } finally {
    setBusy(saveBtn, false);
  }
}

$("saveBtn").addEventListener("click", () => save().catch((err) => notify(errorMessage(err), "error")));
$("loginBtn").addEventListener("click", async () => {
  if (loginInProgress) return;
  loginInProgress = true;
  const loginBtn = $("loginBtn");
  setBusy(loginBtn, true, "等待登录...");
  try {
    await save({ silent: true });
    await api.StartSSOLogin();
    notify("已打开 SSO 登录页面，请在浏览器中完成登录。完成后模型列表会自动更新。", "ok");
    await api.CompleteSSOLogin();
    await refresh();
    notify("SSO 登录成功，已刷新模型列表");
  } catch (err) {
    notify(errorMessage(err), "error");
  } finally {
    loginInProgress = false;
    setBusy(loginBtn, false);
  }
});
$("logoutBtn").addEventListener("click", async () => {
  try { await api.Logout(); await refresh(); notify("已退出登录"); } catch (err) { notify(errorMessage(err), "error"); }
});
$("genKeyBtn").addEventListener("click", async () => { const btn = $("genKeyBtn"); btn.disabled = true; try { const result = await api.GenerateAPIKey(); $("apiKey").value = result.api_key; await refresh(); const notice = codexSyncNotice("已生成并保存新的 API Key", result.codex_credential_sync); notify(notice.message, notice.kind); } catch(err) { notify(errorMessage(err), "error"); } finally { btn.disabled = false; } });
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
document.querySelectorAll("code.clickable").forEach((el) => {
  el.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(el.textContent || "");
      el.classList.add("copied");
      setTimeout(() => el.classList.remove("copied"), 600);
      notify("已复制");
    } catch { notify("复制失败，请手动选中复制", "error"); }
  });
});
$("configCodexBtn").addEventListener("click", async () => {
  const btn = $("configCodexBtn");
  setBusy(btn, true, "配置中...");
  try {
    const msg = await api.ConfigureCodex();
    if (msg && msg.includes("未检测到")) {
      notify(msg, "error");
    } else {
      notify(msg || "Codex 配置已写入 ~/.codex/");
    }
    checkCodexInstalled(); // refresh button states
  } catch (err) {
    notify(errorMessage(err), "error");
  } finally {
    setBusy(btn, false);
  }
});
$("restoreCodexBtn").addEventListener("click", async () => {
  const btn = $("restoreCodexBtn");
  if (!window.confirm("将移除第三方服务商及代理密钥，并把所有 Codex 会话恢复为 OpenAI。下次启动 Codex 需要重新登录 OpenAI。是否继续？")) return;
  setBusy(btn, true, "恢复中...");
  try {
    const msg = await api.RestoreCodex();
    notify(msg || "Codex 已恢复为 OpenAI；所有会话已更新，请重新登录 OpenAI。");
  } catch (err) {
    notify(errorMessage(err), "error");
  } finally {
    setBusy(btn, false);
  }
});
$("installCodexBtn").addEventListener("click", async () => {
  const btn = $("installCodexBtn");
  if (btn.dataset.installing) return; // double-click guard
  btn.dataset.installing = "1";
  btn.style.display = "none";
  const progressEl = $("codexProgress");
  const progressMsg = $("codexProgressMsg");
  const progressPct = $("codexProgressPct");
  const progressFill = $("codexProgressFill");
  progressEl.style.display = "";
  progressEl.className = "codex-progress";
  progressMsg.textContent = "准备安装...";
  progressPct.textContent = "";
  progressFill.style.width = "5%";
  try {
    await api.InstallCodexDesktop();
    // Progress updates come via events — poll for completion as backup
    pollCodexInstalled(60, 5000); // poll every 5s for up to 5 minutes
  } catch (err) {
    progressEl.className = "codex-progress error";
    progressMsg.textContent = errorMessage(err);
    progressPct.textContent = "失败";
    progressFill.style.width = "100%";
    notify(errorMessage(err), "error");
    btn.style.display = "";
    delete btn.dataset.installing;
    setTimeout(() => { progressEl.style.display = "none"; }, 8000);
  }
  // NOTE: do NOT delete btn.dataset.installing here — keep the guard active
  // until a terminal event ("done"/"error") resets it. This prevents double winget launches.
});

// Listen for codex install progress events from backend
if (window.runtime && window.runtime.EventsOn) {
  window.runtime.EventsOn("codex-install-progress", (data) => {
    const progressEl = $("codexProgress");
    const progressMsg = $("codexProgressMsg");
    const progressPct = $("codexProgressPct");
    const progressFill = $("codexProgressFill");
    if (!data || !progressEl) return;
    progressEl.style.display = "";
    const pct = Math.round(data.percent || 0);
    progressMsg.textContent = data.message || "";
    progressPct.textContent = pct > 0 ? pct + "%" : "";
    progressFill.style.width = Math.max(pct, 5) + "%";
    if (data.phase === "done") {
      // Deduplicate: skip if already shown as done (poll may have fired first)
      if (progressEl.classList.contains("done")) return;
      progressEl.className = "codex-progress done";
      progressPct.textContent = "OK";
      progressFill.style.width = "100%";
      notify(data.message || "Codex Desktop 安装完成！");
      $("installCodexBtn").style.display = "none";
      delete $("installCodexBtn").dataset.installing;
      setTimeout(() => { progressEl.style.display = "none"; }, 6000);
    } else if (data.phase === "error") {
      // Deduplicate: skip if already terminal
      if (progressEl.classList.contains("done") || progressEl.classList.contains("error")) return;
      progressEl.className = "codex-progress error";
      progressPct.textContent = "ERR";
      progressFill.style.width = "100%";
      $("installCodexBtn").style.display = "";
      delete $("installCodexBtn").dataset.installing;
      notify(data.message || "安装失败", "error");
      setTimeout(() => { progressEl.style.display = "none"; }, 8000);
    } else if (data.phase === "fallback") {
      progressMsg.textContent = data.message || "正在打开 Microsoft Store...";
      progressFill.style.width = "100%";
      // Keep progress visible — polling will detect install
    }
  });
}

function pollCodexInstalled(remaining, intervalMs) {
  if (remaining <= 0) return;
  setTimeout(async () => {
    const installed = await api.IsCodexInstalled().catch(() => false);
    if (installed) {
      const progressEl = $("codexProgress");
      const btn = $("installCodexBtn");
      // Only notify if progress bar hasn't already shown "done"
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
      $("configCodexBtn").removeAttribute("title");
    } else {
      pollCodexInstalled(remaining - 1, intervalMs);
    }
  }, intervalMs);
}

async function checkCodexInstalled() {
  try {
    const installed = await api.IsCodexInstalled();
    const installBtn = $("installCodexBtn");
    const configBtn = $("configCodexBtn");
    if (installed) {
      installBtn.style.display = "none";
      configBtn.disabled = false;
      configBtn.removeAttribute("title");
    } else {
      installBtn.style.display = "";
      configBtn.disabled = false;
      configBtn.setAttribute("title", "Codex 未安装，配置将预写入，安装后即可使用");
    }
  } catch (e) {
    $("installCodexBtn").style.display = "";
  }
}

refresh();
checkCodexInstalled();

$("clearCacheBtn").addEventListener("click", async () => {
  const btn = $("clearCacheBtn");
  setBusy(btn, true, "清除中...");
  try {
    await api.ClearCache();
    await refresh();
    notify("缓存已清除");
  } catch (err) {
    notify(errorMessage(err), "error");
  } finally {
    setBusy(btn, false);
  }
});

// Listen for real-time token stats updates from backend
if (window.runtime && window.runtime.EventsOn) {
  window.runtime.EventsOn("token-stats-updated", (data) => {
    if (!data) return;
    scheduleTokenStatsRefresh();
  });
  window.runtime.EventsOn("cache-stats-updated", (data) => {
    if (!data) return;
    scheduleTokenStatsRefresh();
  });
  window.runtime.EventsOn("cache-decision-updated", () => { refresh(); });
  window.runtime.EventsOn("models-refreshed", () => { refresh(); });
}

// Token stats period switcher
let tokenStatsRefreshTimer = null;

function scheduleTokenStatsRefresh() {
  // Throttle: at most once every 2 seconds.
  if (tokenStatsRefreshTimer) return;
  tokenStatsRefreshTimer = setTimeout(() => {
    tokenStatsRefreshTimer = null;
    refreshTokenStats();
  }, 2000);
}

async function refreshTokenStats() {
  if (!api || !api.TokenStats) return;
  try {
    const stats = await api.TokenStats(currentTokenPeriod);
    $("promptTokens").textContent = formatTokenCount(stats.prompt_tokens || 0);
    $("completionTokens").textContent = formatTokenCount(stats.completion_tokens || 0);
    $("totalTokens").textContent = formatTokenCount(stats.total_tokens || 0);
    // Show "before cache" values only if there are cache hits (savings exist).
    const hasSavings = (stats.total_before_cache || 0) > (stats.total_tokens || 0);
    $("promptBefore").textContent = hasSavings ? formatTokenCount(stats.prompt_before_cache || 0) : "";
    $("completionBefore").textContent = hasSavings ? formatTokenCount(stats.completion_before_cache || 0) : "";
    $("totalBefore").textContent = hasSavings ? formatTokenCount(stats.total_before_cache || 0) : "";
    const pct = stats.cache_saving_pct || 0;
    $("cacheSaving").textContent = pct > 0 ? pct.toFixed(0) + "%" : "-";
  } catch (e) { /* ignore */ }
}

document.querySelectorAll(".period-btn").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll(".period-btn").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    currentTokenPeriod = btn.dataset.period;
    refreshTokenStats();
  });
});

// Initial load uses the selected period
refreshTokenStats();
