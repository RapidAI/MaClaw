const api = window.go?.main?.App;
const $ = (id) => document.getElementById(id);
let toastTimer;
let loginInProgress = false;

function formatTokenCount(n) {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(n);
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

async function refresh() {
  try {
    const status = await api.Status();
    const s = status.settings || {};
    $("listenAddress").value = s.listen_address || "";
    $("apiKey").value = s.api_key || "";
    $("baseURL").value = s.base_url || "";
    renderModels(s.models || [], s.model_id || "");
    $("email").value = s.email || "";
    $("openaiURL").textContent = status.openai_url || "";
    $("anthropicURL").textContent = status.anthropic_url || "";
    $("healthURL").textContent = status.health_url || "";
    $("openaiEnv").textContent = `OPENAI_BASE_URL=${status.openai_url || ""}`;
    $("anthropicEnv").textContent = `ANTHROPIC_BASE_URL=${status.anthropic_url || ""}`;
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
    $("promptTokens").textContent = formatTokenCount(status.prompt_tokens || 0);
    $("completionTokens").textContent = formatTokenCount(status.completion_tokens || 0);
    $("totalTokens").textContent = formatTokenCount(status.total_tokens || 0);
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
    await api.SaveSettings({
      listen_address: $("listenAddress").value,
      api_key: $("apiKey").value,
      base_url: $("baseURL").value,
      model_id: $("modelID").value,
      email: $("email").value,
    });
    await refresh();
    if (!options.silent) {
      notify("已保存");
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
$("genKeyBtn").addEventListener("click", async () => { const btn = $("genKeyBtn"); btn.disabled = true; try { $("apiKey").value = await api.GenerateAPIKey(); await refresh(); notify("已生成并保存新的 API Key"); } catch(err) { notify(errorMessage(err), "error"); } finally { btn.disabled = false; } });
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
      progressPct.textContent = "✓";
      progressFill.style.width = "100%";
      notify(data.message || "Codex Desktop 安装完成！");
      $("installCodexBtn").style.display = "none";
      delete $("installCodexBtn").dataset.installing;
      setTimeout(() => { progressEl.style.display = "none"; }, 6000);
    } else if (data.phase === "error") {
      // Deduplicate: skip if already terminal
      if (progressEl.classList.contains("done") || progressEl.classList.contains("error")) return;
      progressEl.className = "codex-progress error";
      progressPct.textContent = "✗";
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
        $("codexProgressPct").textContent = "✓";
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

// Listen for backend model list refresh (e.g. after startup re-fetches from server)
if (window.runtime && window.runtime.EventsOn) {
  window.runtime.EventsOn("models-refreshed", () => { refresh(); });
}
