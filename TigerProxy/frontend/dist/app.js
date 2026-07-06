const api = window.go?.main?.App;
const $ = (id) => document.getElementById(id);
let toastTimer;
let loginInProgress = false;

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
    $("loginState").textContent = status.logged_in ? `已登录 ${s.email || "CodeGen"}` : "未登录 CodeGen，请先完成 SSO。";
    const loginChip = $("loginChip");
    loginChip.textContent = status.logged_in ? (s.email || "已登录") : "未登录";
    loginChip.className = `chip ${status.logged_in ? "ok" : "muted"}`;
    const badge = $("statusBadge");
    badge.textContent = status.last_error || (status.running ? (status.logged_in ? "运行中" : "等待登录") : "未运行");
    badge.className = `badge ${status.running && status.logged_in ? "ok" : "warn"}`;
    const lan = $("lanURLs");
    lan.innerHTML = "";
    (status.lan_urls || []).forEach((url) => {
      const item = document.createElement("code");
      item.textContent = `${url}/v1`;
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
      notify("已保存并重启代理");
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
$("genKeyBtn").addEventListener("click", async () => { $("apiKey").value = await api.GenerateAPIKey(); notify("已生成新的 API Key，保存后生效"); });
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
document.querySelectorAll("button[data-copy]").forEach((btn) => {
  btn.addEventListener("click", async () => { await navigator.clipboard.writeText($(btn.dataset.copy).textContent || ""); notify("已复制"); });
});

refresh();
