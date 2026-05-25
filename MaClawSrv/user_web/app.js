const $ = (id) => document.getElementById(id);
const I18N = {
  en: {
    userWorkspace: "User Workspace", assistantNav: "AI Assistant", settingsNav: "System Settings", skipToMain: "Skip to main content", appSections: "User app sections", userViews: "User views", notSignedIn: "Not signed in", logout: "Log Out", ready: "Ready", busy: "Busy",
    loginRequired: "Login required", loginHint: "Open this page from VE Platform MaClawSrv user entry.", cannotStart: "Cannot start user app", missingToken: "Missing short-lived access token.", rawSecretRejected: "Raw secrets in URLs are not accepted. Open this page again from VE Platform.", sessionExpired: "Session expired. Open this page again from VE Platform.", loadFailed: "Load failed", retry: "Retry",
    assistantTitle: "AI Assistant", assistantHint: "Instances share user-level config, tools, knowledge, memory, and security policy.", instancesTitle: "Assistant instances", instancesHint: "Runtime state and sessions stay per instance. Configuration stays shared by user.", new: "New", noInstances: "No instances", unknown: "unknown", readyState: "ready", notReady: "not ready", instanceName: "Instance name", instanceCreated: "Instance created",
    sessions: "Sessions", noSessions: "No sessions", firstMessage: "Send the first message to create a session.", selectedMissing: "Selected assistant instance was not found or is unavailable. Open it again from VE Platform or select another instance.", createFirst: "No instance yet. Create an assistant instance first.", noMessages: "No messages", typeMessage: "Type a message...", message: "Message", send: "Send", webSession: "Web session", run: "Run", waitingUser: "waiting for user", continueWaiting: "Continue waiting", cancel: "Cancel", runCancelled: "Run cancelled", sent: "Sent", runStatus: "Run status: {status}", llmManagedByHub: "LLM is not fully configured. Ask VE Platform to pass the Hub LLM endpoint and viewer token, or fill in System Settings.",
    settingsTitle: "System Settings", settingsHint: "User-scoped settings shared by all assistant instances.", sharedConfig: "Shared config", sharedConfigHint: "LLM, MCP, tools, skills, knowledge, and security policy are shared at user scope.", configResponse: "Config response", secretHint: "Masked value keeps the existing secret. Enter a new value only when rotating it.", valid: "Valid", needsAttention: "Needs attention", currentConfigOk: "Current shared config can start instances.", save: "Save", validate: "Validate", test: "Test", saved: "Saved", validated: "Validated", testPassed: "Test passed", testFailed: "Test failed", unset: "Unset", trueValue: "True", falseValue: "False",
    groupLLM: "LLM", groupLLMHint: "Primary model providers and legacy fallback fields.", groupTools: "MCP & Tools", groupToolsHint: "Remote/local tool servers and search providers shared by every instance.", groupSkills: "Skills", groupSkillsHint: "Installed skills, hubs, external directories, and source allow-list.", groupMemory: "Knowledge & Memory", groupMemoryHint: "Memory compression and knowledge context budget.", groupSecurity: "Security", groupSecurityHint: "User-level execution boundary and network policy.", groupChannels: "Channels", groupChannelsHint: "IM, bot, gateway, and voice channel settings.", groupUI: "UI & Runtime", groupUIHint: "User interface, pet, launch, proxy, and local runtime preferences.", groupAdvanced: "Advanced", groupAdvancedHint: "All remaining AppConfig fields exposed by the service schema.", numberInvalid: "{key} must be a valid {type}", jsonInvalid: "{key} must be valid JSON"
  },
  zh: {
    userWorkspace: "用户工作台", assistantNav: "AI 助手", settingsNav: "系统设置", skipToMain: "跳到主要内容", appSections: "用户应用区域", userViews: "用户视图", notSignedIn: "未登录", logout: "退出", ready: "就绪", busy: "忙碌",
    loginRequired: "需要登录", loginHint: "请从 VE Platform 的 MaClawSrv 用户入口打开本页。", cannotStart: "无法启动用户应用", missingToken: "缺少短期访问令牌。", rawSecretRejected: "URL 中不接受原始密钥。请从 VE Platform 重新打开本页。", sessionExpired: "会话已过期，请从 VE Platform 重新打开本页。", loadFailed: "加载失败", retry: "重试",
    assistantTitle: "AI 助手", assistantHint: "多个实例共享用户级配置、工具、知识、记忆和安全策略。", instancesTitle: "助手实例", instancesHint: "运行状态和会话按实例保留，配置按用户共享。", new: "新建", noInstances: "暂无实例", unknown: "未知", readyState: "就绪", notReady: "未就绪", instanceName: "实例名称", instanceCreated: "实例已创建",
    sessions: "会话", noSessions: "暂无会话", firstMessage: "发送第一条消息后会自动创建会话。", selectedMissing: "选中的助手实例不存在或不可用。请从 VE Platform 重新打开，或选择其它实例。", createFirst: "还没有实例，请先创建助手实例。", noMessages: "暂无消息", typeMessage: "输入消息...", message: "消息", send: "发送", webSession: "网页会话", run: "运行", waitingUser: "等待用户", continueWaiting: "继续等待", cancel: "取消", runCancelled: "运行已取消", sent: "已发送", runStatus: "运行状态：{status}", llmManagedByHub: "LLM 未完成配置。请让 VE Platform 传入 Hub LLM 地址和 viewer token，或在系统设置里填写可用配置。",
    settingsTitle: "系统设置", settingsHint: "这些用户级设置会被所有助手实例共享。", sharedConfig: "共享配置", sharedConfigHint: "LLM、MCP、工具、技能、知识和安全策略按用户范围共享。", configResponse: "配置响应", secretHint: "显示为掩码时会保留现有密钥；只有需要轮换时才输入新值。", valid: "有效", needsAttention: "需要处理", currentConfigOk: "当前共享配置可以启动实例。", save: "保存", validate: "校验", test: "测试", saved: "已保存", validated: "已校验", testPassed: "测试通过", testFailed: "测试失败", unset: "未设置", trueValue: "是", falseValue: "否",
    groupLLM: "LLM", groupLLMHint: "主模型服务商和旧版兜底字段。", groupTools: "MCP 与工具", groupToolsHint: "所有实例共享的远程/本地工具服务器和搜索服务商。", groupSkills: "技能", groupSkillsHint: "已安装技能、技能中心、外部目录和来源白名单。", groupMemory: "知识与记忆", groupMemoryHint: "记忆压缩和知识上下文预算。", groupSecurity: "安全", groupSecurityHint: "用户级执行边界和网络策略。", numberInvalid: "{key} 必须是有效的{type}", jsonInvalid: "{key} 必须是有效 JSON"
  }
};
const params = new URLSearchParams(location.search);
Object.assign(I18N.zh, {
  userWorkspace: "用户工作台", assistantNav: "AI 助手", settingsNav: "系统设置", skipToMain: "跳到主要内容", appSections: "用户应用区域", userViews: "用户视图", notSignedIn: "未登录", logout: "退出", ready: "就绪", busy: "忙碌",
  loginRequired: "需要登录", loginHint: "请从 VE Platform 的 MaClawSrv 用户入口打开本页。", cannotStart: "无法启动用户应用", missingToken: "缺少短期访问令牌。", rawSecretRejected: "URL 中不接受原始密钥。请从 VE Platform 重新打开本页。", sessionExpired: "会话已过期，请从 VE Platform 重新打开本页。", loadFailed: "加载失败", retry: "重试",
  assistantTitle: "AI 助手", assistantHint: "多个实例共享用户级配置、工具、知识、记忆和安全策略。", instancesTitle: "助手实例", instancesHint: "运行状态和会话按实例保留，配置按用户共享。", new: "新建", noInstances: "暂无实例", unknown: "未知", readyState: "就绪", notReady: "未就绪", instanceName: "实例名称", instanceCreated: "实例已创建",
  sessions: "会话", noSessions: "暂无会话", firstMessage: "发送第一条消息后会自动创建会话。", selectedMissing: "选中的助手实例不存在或不可用。请从 VE Platform 重新打开，或选择其它实例。", createFirst: "还没有实例，请先创建助手实例。", noMessages: "暂无消息", typeMessage: "输入消息...", message: "消息", send: "发送", webSession: "网页会话", run: "运行", waitingUser: "等待用户", continueWaiting: "继续等待", cancel: "取消", runCancelled: "运行已取消", sent: "已发送", runStatus: "运行状态：{status}", llmManagedByHub: "LLM 未完成配置。请让 VE Platform 传入 Hub LLM 地址和 viewer token，或在系统设置里填写可用配置。",
  settingsTitle: "系统设置", settingsHint: "这些用户级设置会被所有助手实例共享。", sharedConfig: "共享配置", sharedConfigHint: "LLM、MCP、工具、技能、知识和安全策略按用户范围共享。", configResponse: "配置响应", secretHint: "显示为掩码时会保留现有密钥；只有需要轮换时才输入新值。", valid: "有效", needsAttention: "需要处理", currentConfigOk: "当前共享配置可以启动实例。", save: "保存", validate: "校验", test: "测试", saved: "已保存", validated: "已校验", testPassed: "测试通过", testFailed: "测试失败", unset: "未设置", trueValue: "是", falseValue: "否",
  groupLLM: "LLM", groupLLMHint: "主模型服务商和旧版兜底字段。", groupTools: "MCP 与工具", groupToolsHint: "所有实例共享的远程/本地工具服务器和搜索服务商。", groupSkills: "技能", groupSkillsHint: "已安装技能、技能中心、外部目录和来源白名单。", groupMemory: "知识与记忆", groupMemoryHint: "记忆压缩和知识上下文预算。", groupSecurity: "安全", groupSecurityHint: "用户级执行边界和网络策略。", groupChannels: "渠道", groupChannelsHint: "IM、机器人、网关和语音渠道设置。", groupUI: "界面与运行时", groupUIHint: "用户界面、宠物、启动、代理和本地运行偏好。", groupAdvanced: "高级", groupAdvancedHint: "服务 schema 暴露的其余 AppConfig 字段。", numberInvalid: "{key} 必须是有效的{type}", jsonInvalid: "{key} 必须是有效 JSON"
});
const FIELD_I18N = {
  en: {
    maclaw_llm_url: ["LLM URL", "Legacy flat LLM endpoint URL."], maclaw_llm_key: ["LLM API Key", "Legacy flat API key or bearer token."], maclaw_llm_model: ["LLM Model", "Legacy flat default model. Use auto for VE Platform Hub LLM endpoints; service groups are platform metadata, not model names."], maclaw_llm_current_provider: ["Current Provider", "Selected provider name from maclaw_llm_providers."], maclaw_llm_providers: ["LLM Providers", "Provider list. When configured, MaClawSrv prefers the selected provider over legacy flat fields."],
    mcp_servers: ["Remote MCP Servers", "Remote MCP server registry shared by all user assistant instances."], local_mcp_servers: ["Local MCP Servers", "Local MCP stdio server registry shared by all user assistant instances."], web_search_providers: ["Web Search Providers", "Search provider configuration shared by user assistant instances."], web_search_current_provider: ["Current Web Search Provider", "Selected provider name from web_search_providers."],
    nl_skills: ["Installed Skills", "User-level skill entries available to assistant instances."], skill_hub_urls: ["Skill Hubs", "Skill discovery sources for this user."], external_skill_dirs: ["External Skill Directories", "Additional user skill directories."], skill_sources_allowed: ["Allowed Skill Sources", "Optional allow-list for skill sources. Empty allows all configured sources."],
    memory_auto_compress: ["Memory Auto Compress", "Enable automatic conversation and memory compression."], memory_max_backups: ["Memory Max Backups", "Maximum memory backup count. Zero uses service default."], knowledge_skill_token_budget: ["Knowledge Skill Token Budget", "Token budget for knowledge skill context packs. Zero uses service default."],
    security_policy_mode: ["Security Policy Mode", "User-level security policy mode for tool and agent execution."], sandbox_mode: ["Sandbox Mode", "Execution sandbox preference for this user."], network_level: ["Network Level", "Network access level for user tools and agents."], yolo_mode_allowed: ["YOLO Mode Allowed", "Allow this user to enable broad tool execution mode."]
  },
  zh: {
    maclaw_llm_url: ["LLM 服务地址", "旧版平铺 LLM 服务端点地址。由 VE Platform 托管时通常自动填入。"], maclaw_llm_key: ["LLM 访问令牌", "旧版 API Key 或 Hub viewer Bearer token。"], maclaw_llm_model: ["LLM 模型", "旧版默认模型；接入 VE Platform Hub 时使用 auto，服务组由平台元数据管理，不填在这里。"], maclaw_llm_current_provider: ["当前服务商", "从 maclaw_llm_providers 中选择的服务商名称。"], maclaw_llm_providers: ["LLM 服务商列表", "服务商列表。配置后会优先使用选中的服务商，而不是旧版平铺字段。"],
    mcp_servers: ["远程 MCP 服务", "所有助手实例共享的远程 MCP 服务注册表。"], local_mcp_servers: ["本地 MCP 服务", "所有助手实例共享的本地 stdio MCP 服务注册表。"], web_search_providers: ["联网搜索服务", "用户助手实例共享的搜索服务配置。"], web_search_current_provider: ["当前搜索服务", "从 web_search_providers 中选择的搜索服务名称。"],
    nl_skills: ["已安装技能", "可供助手实例使用的用户级技能条目。"], skill_hub_urls: ["技能中心", "此用户的技能发现来源。"], external_skill_dirs: ["外部技能目录", "额外的用户技能目录。"], skill_sources_allowed: ["允许的技能来源", "可选的技能来源白名单。留空表示允许所有已配置来源。"],
    memory_auto_compress: ["自动压缩记忆", "启用会话与记忆的自动压缩。"], memory_max_backups: ["记忆备份上限", "最大记忆备份数量。0 表示使用服务默认值。"], knowledge_skill_token_budget: ["知识技能 Token 预算", "知识技能上下文包的 Token 预算。0 表示使用服务默认值。"],
    security_policy_mode: ["安全策略模式", "用户级工具和 Agent 执行安全策略模式。"], sandbox_mode: ["沙箱模式", "此用户的执行沙箱偏好。"], network_level: ["网络访问级别", "用户工具和 Agent 的网络访问级别。"], yolo_mode_allowed: ["允许 YOLO 模式", "允许此用户启用宽松工具执行模式。"]
  }
};
const requestedLocale = (params.get("lang") || localStorage.getItem("maclaw.user.lang") || document.documentElement.lang || navigator.language || "zh-CN").toLowerCase();
Object.assign(FIELD_I18N.zh, {
  maclaw_llm_url: ["LLM 服务地址", "旧版平铺 LLM 服务端点地址。由 VE Platform 托管时通常自动填入。"], maclaw_llm_key: ["LLM 访问令牌", "旧版 API Key 或 Hub viewer Bearer token。"], maclaw_llm_model: ["LLM 模型", "旧版默认模型；接入 VE Platform Hub 时使用 auto，服务组由平台元数据管理，不填在这里。"], maclaw_llm_current_provider: ["当前服务商", "从 maclaw_llm_providers 中选择的服务商名称。"], maclaw_llm_providers: ["LLM 服务商列表", "服务商列表。配置后会优先使用选中的服务商，而不是旧版平铺字段。"],
  mcp_servers: ["远程 MCP 服务", "所有助手实例共享的远程 MCP 服务注册表。"], local_mcp_servers: ["本地 MCP 服务", "所有助手实例共享的本地 stdio MCP 服务注册表。"], web_search_providers: ["联网搜索服务", "用户助手实例共享的搜索服务配置。"], web_search_current_provider: ["当前搜索服务", "从 web_search_providers 中选择的搜索服务名称。"],
  nl_skills: ["已安装技能", "可供助手实例使用的用户级技能条目。"], skill_hub_urls: ["技能中心", "此用户的技能发现来源。"], external_skill_dirs: ["外部技能目录", "额外的用户技能目录。"], skill_sources_allowed: ["允许的技能来源", "可选的技能来源白名单。留空表示允许所有已配置来源。"],
  memory_auto_compress: ["自动压缩记忆", "启用会话与记忆的自动压缩。"], memory_max_backups: ["记忆备份上限", "最大记忆备份数量。0 表示使用服务默认值。"], knowledge_skill_token_budget: ["知识技能 Token 预算", "知识技能上下文包的 Token 预算。0 表示使用服务默认值。"],
  security_policy_mode: ["安全策略模式", "用户级工具和 Agent 执行安全策略模式。"], sandbox_mode: ["沙箱模式", "此用户的执行沙箱偏好。"], network_level: ["网络访问级别", "用户工具和 Agent 的网络访问级别。"], yolo_mode_allowed: ["允许 YOLO 模式", "允许此用户启用宽松工具执行模式。"]
});
const locale = requestedLocale.startsWith("en") ? "en" : "zh";
const t = (key, vars = {}) => Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), (I18N[locale] || I18N.zh)[key] || key);
function fieldMeta(def = {}) { const tr = FIELD_I18N[locale]?.[def.key]; return tr ? { ...def, title: tr[0], description: tr[1] } : def; }
function configTypeName(type) { if (locale !== "zh") return type; return type === "integer" ? "整数" : type === "number" ? "数字" : type; }
function configIssueLabel(issue = {}) { const key = String(issue.key || ""); const base = key.split(".")[0]; const meta = fieldMeta({ key: base, title: base }); const suffix = key.includes(".") ? ` / ${key.split(".").slice(1).join(".")}` : ""; return `${meta.title || key}${suffix}`; }
function configIssueMessage(issue = {}) { const msg = String(issue.message || ""); if (locale !== "zh") return msg; const key = issue.key || ""; if (msg.includes("managed-by-hub")) return "仍然使用 VE Platform managed-by-hub 占位符，请从 VE Platform 重新打开并传入 Hub LLM 地址和 viewer token。"; if (msg.includes("Selected provider URL is required") || msg.includes("URL is required")) return "必须填写 LLM 服务地址。"; if (msg.includes("API key is required") || msg.includes("credential is required")) return "必须填写 LLM 访问令牌。"; if (msg.includes("Selected provider model is required") || msg.includes("model is required")) return "必须填写 LLM 模型；接入 VE Platform Hub 时填写 auto。"; if (msg.includes("selected provider") && msg.includes("was not found")) return "当前服务商不在 LLM 服务商列表中。"; if (key === "maclaw_llm_current_provider") return msg.replace("maclaw_llm_current_provider is required when multiple providers are configured", "配置多个服务商时必须选择当前服务商"); return msg; }
const state = { token: "", me: null, instances: [], sessions: [], messages: [], view: "assistant", instanceId: "", sessionId: "", config: null, schema: [], settingsTab: "", busy: false, currentRun: null, runStream: null, copySnippets: [], hiddenMessages: {} };
const saved = sessionStorage.getItem("maclaw.user.token") || "";
const launchToken = params.get("launch_token") || "";
const hasLaunchToken = params.has("launch_token");
const secretURLKeys = ["token", "access_token", "api_key", "api_secret"];
const rawURLSecret = secretURLKeys.some((key) => params.has(key) || location.hash.toLowerCase().includes(`${key}=`));
state.token = hasLaunchToken || rawURLSecret ? "" : saved;
state.view = params.get("view") === "settings" ? "settings" : "assistant";
state.instanceId = params.get("instance_id") || "";
if (state.token) sessionStorage.setItem("maclaw.user.token", state.token);
if (rawURLSecret || hasLaunchToken) {
  secretURLKeys.forEach((key) => params.delete(key));
  params.delete("launch_token");
  const next = location.pathname + (params.toString() ? `?${params}` : "");
  history.replaceState(null, "", next);
}

function esc(v) { return String(v ?? "").replace(/[&<>'"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[c])); }
function pretty(v) { return JSON.stringify(v, null, 2); }
function setBusy(on) { state.busy = on; document.body.classList.toggle("is-fetching", on); $("statusBadge").textContent = on ? t("busy") : t("ready"); }
function toast(msg) { const el = $("toast"); el.textContent = msg; el.classList.remove("hidden"); clearTimeout(toast.timer); toast.timer = setTimeout(() => el.classList.add("hidden"), 3600); }
function headers(json = true) { const h = { Authorization: `Bearer ${state.token}` }; if (json) h["Content-Type"] = "application/json"; return h; }
function apiErrorMessage(data, fallback) {
  const msg = data.error || data.message || fallback;
  const text = `${msg} ${data.raw || ""}`.toLowerCase();
  return text.includes("managed-by-hub") || text.includes("viewer authentication failed") || text.includes("unauthorized") ? t("llmManagedByHub") : msg;
}
async function api(path, opt = {}) {
  if (!state.token) throw new Error("missing launch token");
  const resp = await fetch(path, { ...opt, headers: { ...headers(opt.body !== undefined), ...(opt.headers || {}) } });
  const text = await resp.text();
  let data = {};
  if (text) { try { data = JSON.parse(text); } catch { data = { raw: text }; } }
  if (!resp.ok) {
    const err = new Error(apiErrorMessage(data, `${resp.status} ${resp.statusText}`));
    err.status = resp.status;
    if (resp.status === 401) { resetRunState(); sessionStorage.removeItem("maclaw.user.token"); state.token = ""; }
    throw err;
  }
  return data;
}
function closeRunStream() { if (state.runStream) { state.runStream.abort(); state.runStream = null; } }
function resetRunState() { closeRunStream(); state.currentRun = null; }
async function exchangeLaunchToken() {
  if (state.token || !launchToken) return;
  const resp = await fetch("/api/v1/web/exchange", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ launch_token: launchToken }) });
  const text = await resp.text();
  let data = {};
  if (text) { try { data = JSON.parse(text); } catch { data = { raw: text }; } }
  if (!resp.ok) { const err = new Error(data.error || data.message || `${resp.status} ${resp.statusText}`); err.status = resp.status; throw err; }
  state.token = data.access_token || "";
  if (!state.token) throw new Error("missing exchanged access token");
  sessionStorage.setItem("maclaw.user.token", state.token);
}
function items(resp) { return Array.isArray(resp?.items) ? resp.items : Array.isArray(resp) ? resp : []; }
function activeInstance() { return state.instanceId ? (state.instances.find((x) => x.id === state.instanceId) || null) : (state.instances[0] || null); }
function selectedInstanceMissing() { return !!state.instanceId && !state.instances.some((x) => x.id === state.instanceId); }
function panelMessageKey() { return `${state.instanceId || "default"}:${state.sessionId || "new"}`; }
function messageIdentity(m) { return String(m?.id || "").trim(); }
function hiddenMessageSet(key = panelMessageKey()) { return state.hiddenMessages[key] || (state.hiddenMessages[key] = new Set()); }
function visibleMessages(messages) { const hidden = hiddenMessageSet(); return items(messages).filter((m) => !hidden.has(messageIdentity(m))); }
function setTitle(title, hint) { $("pageTitle").textContent = title; $("pageHint").textContent = hint; document.title = `${title} - MaClawSrv`; }
function initChrome() { document.documentElement.lang = locale === "zh" ? "zh-CN" : "en"; document.querySelector(".skip-link").textContent = t("skipToMain"); document.querySelector(".sidebar").setAttribute("aria-label", t("appSections")); document.querySelector(".nav").setAttribute("aria-label", t("userViews")); $("brandSubtitle").textContent = t("userWorkspace"); document.querySelector('[data-view="assistant"]').textContent = t("assistantNav"); document.querySelector('[data-view="settings"]').textContent = t("settingsNav"); $("logoutBtn").textContent = t("logout"); if (!state.me) $("identity").textContent = t("notSignedIn"); setBusy(state.busy); }
function updateNav() { document.querySelectorAll("[data-view]").forEach((b) => { const on = b.dataset.view === state.view; b.classList.toggle("active", on); b.setAttribute("aria-current", on ? "page" : "false"); }); }

async function bootstrap() {
  try {
    initChrome();
    setBusy(true);
    if (rawURLSecret) {
      sessionStorage.removeItem("maclaw.user.token");
      state.token = "";
      renderMissingToken(t("rawSecretRejected"));
      return;
    }
    await exchangeLaunchToken();
    if (!state.token) { renderMissingToken(); return; }
    const [me, inst] = await Promise.all([api("/api/v1/me"), api("/api/v1/instances")]);
    state.me = me;
    state.instances = items(inst);
    if (!state.instanceId && state.instances[0]) state.instanceId = state.instances[0].id;
    $("identity").textContent = `${me.email || me.user_id || "user"}`;
    await render();
  } catch (e) { if (e.status === 401) renderMissingToken(t("sessionExpired")); else renderError(e); }
  finally { setBusy(false); }
}
async function render() { updateNav(); if (state.view === "settings") return renderSettings(); return renderAssistant(); }
function renderMissingToken(message = t("missingToken")) { setTitle(t("loginRequired"), t("loginHint")); $("content").innerHTML = `<section class="panel stack"><h2>${t("cannotStart")}</h2><p class="error">${esc(message)}</p></section>`; }
function renderError(e) { $("content").innerHTML = `<section class="panel stack"><h2>${t("loadFailed")}</h2><p class="error">${esc(e.message)}</p><button id="retryBtn" type="button" class="primary">${t("retry")}</button></section>`; $("retryBtn").onclick = bootstrap; }
function handleAPIError(e) { if (e && e.status === 401) { renderMissingToken(t("sessionExpired")); return true; } return false; }
async function refreshInstances() {
  const out = await api("/api/v1/instances");
  state.instances = items(out);
  if (state.instanceId && !state.instances.some((x) => x.id === state.instanceId)) state.instanceId = state.instances[0]?.id || "";
  if (!state.instanceId && state.instances[0]) state.instanceId = state.instances[0].id;
  return state.instances;
}

async function renderAssistant() {
  setTitle(t("assistantTitle"), t("assistantHint"));
  const inst = activeInstance();
  if (inst) state.instanceId = inst.id;
  $("content").innerHTML = `<div class="assistant-layout"><section class="panel stack assistant-rail"><div class="split"><div><h2>${t("instancesTitle")}</h2><p class="helper">${t("instancesHint")}</p></div><button id="newInst" type="button" class="primary">${t("new")}</button></div><div id="instanceList" class="list"></div><div id="sessionList" class="list"></div></section><section class="card chat"><div id="runPanel" class="run-panel hidden"></div><div class="chat-toolbar"><span class="muted">${t("webSession")}</span><button id="clearPanel" type="button" class="secondary clear-panel-btn">${clearContentLabel()}</button></div><div class="messages-wrap"><div id="messages" class="messages"></div><button id="jumpLatest" type="button" class="jump-latest hidden">${latestLabel()}</button></div><form id="composer" class="composer"><textarea id="prompt" placeholder="${t("typeMessage")}" aria-label="${t("message")}"></textarea><button id="sendBtn" type="submit" class="primary">${t("send")}</button></form></section></div>`;
  renderInstanceList();
  $("newInst").onclick = createInstance;
  $("clearPanel").onclick = clearPanelContent;
  $("composer").onsubmit = sendMessage;
  bindComposerKeys();
  if (inst) await loadSessionsAndMessages();
  else if (selectedInstanceMissing()) {
    $("prompt").disabled = true;
    $("sendBtn").disabled = true;
    renderEmptyChat(t("selectedMissing"), true);
  } else renderEmptyChat(t("createFirst"));
}
function bindComposerKeys() {
  const prompt = $("prompt");
  if (!prompt) return;
  const sync = () => { autoResizePrompt(); updateSendButtonState(); };
  prompt.oninput = sync;
  prompt.onkeydown = (e) => {
    if (e.key !== "Enter" || e.shiftKey || e.ctrlKey || e.altKey || e.metaKey || e.isComposing) return;
    e.preventDefault();
    $("composer")?.requestSubmit();
  };
  sync();
}
function autoResizePrompt() { const el = $("prompt"); if (!el) return; el.style.height = "auto"; el.style.height = `${Math.min(el.scrollHeight, 180)}px`; }
function updateSendButtonState() { const btn = $("sendBtn"); const prompt = $("prompt"); if (btn && prompt) btn.disabled = !prompt.value.trim() || prompt.disabled; }
function renderInstanceList() {
  const box = $("instanceList");
  box.innerHTML = state.instances.map((i) => `<button type="button" class="instance ${i.id === state.instanceId ? "active" : ""}" data-instance="${esc(i.id)}"><strong>${esc(i.name || i.id)}</strong><span class="muted">${esc(i.status || t("unknown"))} · ${i.ready ? t("readyState") : t("notReady")}</span><span class="pill">${esc(i.id)}</span></button>`).join("") || `<div class="muted">${t("noInstances")}</div>`;
  box.querySelectorAll("[data-instance]").forEach((b) => b.onclick = async () => { if (state.instanceId !== b.dataset.instance) resetRunState(); state.instanceId = b.dataset.instance; state.sessionId = ""; renderInstanceList(); await loadSessionsAndMessages(); });
}
async function createInstance() {
  const name = prompt(t("instanceName"), "web-assistant");
  if (!name) return;
  try {
    setBusy(true);
    const inst = await api("/api/v1/instances", { method: "POST", body: JSON.stringify({ name, description: "VE Platform user web assistant", metadata: { channel: "ve-platform-web" } }) });
    state.instances.unshift(inst); state.instanceId = inst.id; state.sessionId = "";
    await renderAssistant(); toast(t("instanceCreated"));
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
  finally { setBusy(false); }
}
async function loadSessionsAndMessages() {
  const inst = activeInstance();
  if (!inst) return;
  try {
    const sessions = await api(`/api/v1/instances/${encodeURIComponent(inst.id)}/sessions?limit=20`);
    state.sessions = items(sessions);
    if (!state.sessionId && state.sessions[0]) state.sessionId = state.sessions[0].id;
    renderSessions();
    if (state.sessionId) {
      const msgs = await api(`/api/v1/instances/${encodeURIComponent(inst.id)}/sessions/${encodeURIComponent(state.sessionId)}/messages?limit=100`);
      state.messages = visibleMessages(msgs); renderMessages();
    } else { renderEmptyChat(t("firstMessage")); }
  } catch (e) { if (!handleAPIError(e)) renderEmptyChat(e.message, true); }
}
function renderSessions() {
  const box = $("sessionList");
  box.innerHTML = `<h3>${t("sessions")}</h3>` + (state.sessions.map((s) => `<button type="button" class="instance ${s.id === state.sessionId ? "active" : ""}" data-session="${esc(s.id)}"><strong>${esc(s.title || s.id)}</strong><span class="muted">${esc(s.status || "active")}</span></button>`).join("") || `<div class="muted">${t("noSessions")}</div>`);
  box.querySelectorAll("[data-session]").forEach((b) => b.onclick = async () => { if (state.sessionId !== b.dataset.session) resetRunState(); state.sessionId = b.dataset.session; await loadSessionsAndMessages(); });
}
function upsertSession(session) {
  if (!session?.id) return;
  const idx = state.sessions.findIndex((s) => s.id === session.id);
  if (idx >= 0) state.sessions[idx] = session; else state.sessions.unshift(session);
  renderSessions();
}
function renderEmptyChat(text, isError = false) { state.messages = []; state.copySnippets = []; updateJumpLatestButton(false); $("messages").innerHTML = `<div class="message assistant ${isError ? "error" : ""}">${esc(text)}</div>`; }
function clearPanelContent() {
  resetRunState();
  const hidden = hiddenMessageSet();
  state.messages.map(messageIdentity).filter(Boolean).forEach((id) => hidden.add(id));
  state.messages = [];
  state.copySnippets = [];
  renderRunPanel(null);
  updateJumpLatestButton(false);
  const box = $("messages");
  if (box) box.innerHTML = `<div class="message assistant">${t("noMessages")}</div>`;
  toast(contentClearedLabel());
}
function addThinkingPlaceholder(runId = "") {
  removeThinkingPlaceholders();
  state.messages.push({ id: `local-thinking-${Date.now()}`, role: "assistant", content: thinkingLabel(), created_at: new Date().toISOString(), local_thinking: true, run_id: runId });
  renderMessages(true);
}
function removeThinkingPlaceholders() {
  state.messages = state.messages.filter((m) => !m.local_thinking);
}
function removeThinkingPlaceholdersAndRender() {
  const before = state.messages.length;
  removeThinkingPlaceholders();
  if (state.messages.length !== before) renderMessages();
}
function replaceLocalMessage(localId, message) {
  if (!message?.id) return;
  const idx = state.messages.findIndex((m) => m.id === localId);
  if (idx >= 0) state.messages[idx] = message;
  else if (!state.messages.some((m) => m.id === message.id)) state.messages.push(message);
  renderMessages();
}
function messageDetails(m) {
  const meta = m.metadata || {};
  const hasTool = meta.tool_name || meta.tool_call || meta.tool_result || meta.tool_call_id || (m.role === "system" && Object.keys(meta).length);
  if (!hasTool) return "";
  return `<details class="tool-detail"><summary>${esc(meta.tool_name || meta.tool_call || m.role || "tool")}</summary><pre>${esc(pretty(meta))}</pre></details>`;
}
function splitURLTrailingPunctuation(url) {
  let body = String(url || "");
  let tail = "";
  const open = { ")": "(", "]": "[", "}": "{" };
  const count = (s, ch) => (s.match(new RegExp(`\\${ch}`, "g")) || []).length;
  while (body) {
    const ch = body.at(-1);
    if (/[.,;:!?，。；：！？]/.test(ch)) { tail = ch + tail; body = body.slice(0, -1); continue; }
    if (open[ch] && count(body, ch) > count(body, open[ch])) { tail = ch + tail; body = body.slice(0, -1); continue; }
    break;
  }
  return [body, tail];
}
function renderExternalLink(href, label) {
  const safeHref = esc(href);
  return `<a href="${safeHref}" target="_blank" rel="noopener noreferrer">${label}</a>`;
}
function restoreInlineTokens(html, tokens) {
  return tokens.reduce((out, token, idx) => out.replace(`\u0000${idx}\u0000`, token), html);
}
function renderInlineMarkdown(text) {
  const tokens = [];
  let html = esc(text).replace(/`([^`]+)`/g, (_m, code) => {
    const key = `\u0000${tokens.length}\u0000`;
    tokens.push(`<code>${code}</code>`);
    return key;
  });
  html = html.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, (_m, label, href) => {
    const key = `\u0000${tokens.length}\u0000`;
    tokens.push(renderExternalLink(href, label));
    return key;
  });
  html = html
    .replace(/(^|\s)(https?:\/\/[^\s<]+)/g, (_m, prefix, rawURL) => { const [url, tail] = splitURLTrailingPunctuation(rawURL); return `${prefix}${renderExternalLink(url, url)}${tail}`; })
    .replace(/\*\*([^*]+)\*\*/g, `<strong>$1</strong>`)
    .replace(/(^|[^*])\*([^*\s][^*]*?)\*/g, `$1<em>$2</em>`);
  return restoreInlineTokens(html, tokens);
}
function renderMarkdownParagraph(lines) {
  return `<p>${renderInlineMarkdown(lines.join("\n"))}</p>`;
}
function splitMarkdownTableRow(line) {
  const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
  return trimmed.split("|").map((cell) => cell.trim());
}
function isMarkdownTableDivider(line) {
  return /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);
}
function renderMarkdownTable(rows) {
  const header = splitMarkdownTableRow(rows[0]);
  const body = rows.slice(2).map(splitMarkdownTableRow);
  const head = `<thead><tr>${header.map((cell) => `<th>${renderInlineMarkdown(cell)}</th>`).join("")}</tr></thead>`;
  const bodyHtml = body.length ? `<tbody>${body.map((row) => `<tr>${header.map((_cell, idx) => `<td>${renderInlineMarkdown(row[idx] || "")}</td>`).join("")}</tr>`).join("")}</tbody>` : "";
  return `<div class="md-table-wrap"><table>${head}${bodyHtml}</table></div>`;
}
function renderMarkdown(text) {
  const snippets = arguments[1] || [];
  const src = String(text || "").replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const lines = src.split("\n");
  const out = [];
  let paragraph = [];
  const flushParagraph = () => { if (paragraph.length) { out.push(renderMarkdownParagraph(paragraph)); paragraph = []; } };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fence = line.match(/^```\s*([^`]*)\s*$/);
    if (fence) {
      flushParagraph();
      const code = [];
      const lang = fence[1].trim().split(/\s+/)[0] || "code";
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) code.push(lines[i++]);
      const copyIdx = snippets.push(code.join("\n")) - 1;
      out.push(`<div class="md-code-shell"><div class="md-code-head"><span>${esc(lang)}</span><button type="button" class="copy-btn" data-copy-code="${copyIdx}" aria-label="${esc(copyLabel())}">${esc(copyLabel())}</button></div><pre class="md-code"><code>${esc(code.join("\n"))}</code></pre></div>`);
      continue;
    }
    if (!line.trim()) { flushParagraph(); continue; }
    const heading = line.match(/^(#{1,3})\s+(.+)$/);
    if (heading) { flushParagraph(); out.push(`<h${heading[1].length}>${renderInlineMarkdown(heading[2])}</h${heading[1].length}>`); continue; }
    if (line.includes("|") && i + 1 < lines.length && isMarkdownTableDivider(lines[i + 1])) {
      flushParagraph();
      const rows = [line, lines[i + 1]];
      i += 2;
      while (i < lines.length && lines[i].includes("|") && lines[i].trim()) rows.push(lines[i++]);
      i--;
      out.push(renderMarkdownTable(rows));
      continue;
    }
    if (/^---+$/.test(line.trim())) { flushParagraph(); out.push(`<hr>`); continue; }
    const quote = line.match(/^>\s?(.+)$/);
    if (quote) {
      flushParagraph();
      const items = [quote[1]];
      while (i + 1 < lines.length) {
        const next = lines[i + 1].match(/^>\s?(.+)$/);
        if (!next) break;
        items.push(next[1]); i++;
      }
      out.push(`<blockquote>${renderInlineMarkdown(items.join("\n"))}</blockquote>`);
      continue;
    }
    const bullet = line.match(/^\s*[-*+]\s+(.+)$/);
    if (bullet) {
      flushParagraph();
      const items = [bullet[1]];
      while (i + 1 < lines.length) {
        const next = lines[i + 1].match(/^\s*[-*+]\s+(.+)$/);
        if (!next) break;
        items.push(next[1]); i++;
      }
      out.push(`<ul>${items.map((item) => {
        const task = item.match(/^\[( |x|X)\]\s+(.+)$/);
        return task ? `<li class="task-list-item"><input type="checkbox" disabled ${task[1].toLowerCase() === "x" ? "checked" : ""}> ${renderInlineMarkdown(task[2])}</li>` : `<li>${renderInlineMarkdown(item)}</li>`;
      }).join("")}</ul>`);
      continue;
    }
    const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
    if (ordered) {
      flushParagraph();
      const items = [ordered[1]];
      while (i + 1 < lines.length) {
        const next = lines[i + 1].match(/^\s*\d+[.)]\s+(.+)$/);
        if (!next) break;
        items.push(next[1]); i++;
      }
      out.push(`<ol>${items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join("")}</ol>`);
      continue;
    }
    paragraph.push(line);
  }
  flushParagraph();
  return out.join("");
}
function messageCreatedAt(m) { const raw = m.created_at || m.createdAt || m.timestamp || ""; if (typeof raw === "number") return Number.isFinite(raw) ? raw : 0; const ts = Date.parse(raw); return Number.isFinite(ts) ? ts : 0; }
function messageRoleClass(role) { return ["user", "assistant", "system", "tool", "error", "progress"].includes(role) ? role : "assistant"; }
function orderedMessages() { return state.messages.map((item, idx) => ({ item, idx, ts: messageCreatedAt(item) })).sort((a, b) => (a.ts && b.ts && a.ts !== b.ts) ? a.ts - b.ts : a.idx - b.idx).map((x) => x.item); }
function formatMessageTime(m) { const ts = messageCreatedAt(m); return ts ? new Date(ts).toLocaleString(locale === "en" ? "en-US" : "zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }) : ""; }
function messageMetaHTML(m) { const when = formatMessageTime(m); return when ? `<span class="message-time">${esc(when)}</span>` : ""; }
function copyLabel() { return locale === "en" ? "Copy" : "复制"; }
function copiedLabel() { return locale === "en" ? "Copied" : "已复制"; }
function latestLabel() { return locale === "en" ? "Latest" : "最新消息"; }
function clearContentLabel() { return locale === "en" ? "Clear" : "清空内容"; }
function contentClearedLabel() { return locale === "en" ? "Panel cleared" : "面板内容已清空"; }
function sendingLabel() { return locale === "en" ? "Sending..." : "发送中..."; }
function thinkingLabel() { return locale === "en" ? "Thinking" : "思考中"; }
function copyFailedLabel() { return locale === "en" ? "Copy failed" : "复制失败"; }
function fallbackCopyText(value) {
  const area = document.createElement("textarea");
  area.value = value;
  area.setAttribute("readonly", "");
  area.className = "sr-copy-area";
  document.body.appendChild(area);
  area.select();
  let copied = false;
  try { copied = document.execCommand("copy"); }
  finally { area.remove(); }
  if (!copied) throw new Error(copyFailedLabel());
}
async function copyTextToClipboard(value) {
  if (navigator.clipboard?.writeText) {
    try { await navigator.clipboard.writeText(value); return; }
    catch { /* Fall back for denied clipboard permissions or insecure contexts. */ }
  }
  fallbackCopyText(value);
}
async function copyTextImproved(text, btn) {
  const value = String(text || "");
  if (!value) return;
  try {
    await copyTextToClipboard(value);
    if (btn) {
      const prev = btn.textContent;
      btn.textContent = copiedLabel();
      setTimeout(() => { btn.textContent = prev || copyLabel(); }, 1200);
    }
    toast(copiedLabel());
  } catch (e) { toast(e?.message || copyFailedLabel()); }
}
function bindMessageCopyButtons(msgs) {
  document.querySelectorAll("[data-copy-message]").forEach((btn) => btn.onclick = () => copyTextImproved(String(msgs[Number(btn.dataset.copyMessage)]?.content || msgs[Number(btn.dataset.copyMessage)]?.text || ""), btn));
  document.querySelectorAll("[data-copy-code]").forEach((btn) => btn.onclick = () => copyTextImproved(state.copySnippets[Number(btn.dataset.copyCode)] || "", btn));
}
function shouldStickMessagesToBottom(el) { return !el || el.scrollHeight <= el.clientHeight || el.scrollHeight - el.scrollTop - el.clientHeight < 80; }
function updateJumpLatestButton(show) { const btn = $("jumpLatest"); if (btn) btn.classList.toggle("hidden", !show); }
function bindJumpLatestButton() { const btn = $("jumpLatest"); const box = $("messages"); if (!btn || !box) return; btn.onclick = () => { box.scrollTop = box.scrollHeight; updateJumpLatestButton(false); }; box.onscroll = () => { if (shouldStickMessagesToBottom(box)) updateJumpLatestButton(false); }; }
function messageCopyButtonHTML(m, idx) { return m.local_thinking || !String(m.content || m.text || "").trim() ? "" : `<button type="button" class="copy-btn" data-copy-message="${idx}" aria-label="${esc(copyLabel())}">${esc(copyLabel())}</button>`; }
function renderMessages(forceStick = false) { const box = $("messages"); const stick = forceStick || shouldStickMessagesToBottom(box); const msgs = orderedMessages(); state.copySnippets = []; box.innerHTML = msgs.map((m, idx) => `<article class="message ${messageRoleClass(m.role || "assistant")} ${m.local_pending || m.local_thinking ? "pending" : ""}"><div class="message-head"><div class="message-meta"><strong>${esc(m.role || "assistant")}</strong>${messageMetaHTML(m)}${m.local_pending ? `<span class="message-time">${sendingLabel()}</span>` : ""}</div>${messageCopyButtonHTML(m, idx)}</div><div class="md-content ${m.local_thinking ? "thinking" : ""}">${renderMarkdown(m.content || m.text || "", state.copySnippets)}</div>${messageDetails(m)}</article>`).join("") || `<div class="message assistant">${t("noMessages")}</div>`; bindMessageCopyButtons(msgs); bindJumpLatestButton(); if (stick) { box.scrollTop = box.scrollHeight; updateJumpLatestButton(false); } else { updateJumpLatestButton(true); } }
function renderRunPanel(run) {
  const panel = $("runPanel"); if (!panel) return;
  if (run === null) state.currentRun = null; else state.currentRun = run || state.currentRun;
  const r = state.currentRun;
  if (!r || !r.id) { panel.classList.add("hidden"); panel.innerHTML = ""; return; }
  const running = r.status === "running";
  const waiting = r.waiting_for_user || r.response_source === "ask_user";
  panel.classList.remove("hidden");
  panel.innerHTML = `<div><strong>${t("run")} ${esc(r.status || t("unknown"))}</strong><span class="muted">${esc(r.id)}</span>${waiting ? `<span class="pill">${t("waitingUser")}</span>` : ""}</div><div class="row"><button id="waitRun" type="button" class="secondary">${t("continueWaiting")}</button><button id="cancelRun" type="button" class="danger" ${running ? "" : "disabled"}>${t("cancel")}</button></div>`;
  $("waitRun").onclick = () => watchRun(r);
  $("cancelRun").onclick = () => cancelCurrentRun();
}
function handleRunEvent(env) {
  const snap = env?.snapshot || {};
  if (snap.run) renderRunPanel(snap.run);
  if (snap.session?.id) state.sessionId = snap.session.id;
  if (snap.assistant_message) {
    removeThinkingPlaceholders();
    const idx = state.messages.findIndex((m) => m.id === snap.assistant_message.id);
    if (idx >= 0) state.messages[idx] = snap.assistant_message; else state.messages.push(snap.assistant_message);
    renderMessages();
  }
}
function parseSSEFrame(part) {
  const lines = String(part || "").split("\n");
  const event = (lines.find((line) => line.startsWith("event:")) || "event: message").slice(6).trim() || "message";
  const data = lines.filter((line) => line.startsWith("data:")).map((line) => line.slice(5).trimStart()).join("\n");
  return { event, data };
}
function splitSSEBuffer(buffer) {
  const frames = [];
  let rest = String(buffer || "");
  while (rest) {
    const lf = rest.indexOf("\n\n");
    const crlf = rest.indexOf("\r\n\r\n");
    const useCRLF = crlf >= 0 && (lf < 0 || crlf < lf);
    const idx = useCRLF ? crlf : lf;
    if (idx < 0) break;
    frames.push(rest.slice(0, idx));
    rest = rest.slice(idx + (useCRLF ? 4 : 2));
  }
  return { frames, rest };
}
function parseSSEJSON(frame) {
  if (!frame.data) return null;
  try { return JSON.parse(frame.data); } catch { return null; }
}
function handleRunFrame(part) {
  const frame = parseSSEFrame(part);
  const payload = parseSSEJSON(frame);
  if (frame.event === "error") throw new Error(payload?.error || "stream error");
  if (payload) handleRunEvent(payload);
  if (frame.event === "done" && !payload?.snapshot?.assistant_message) removeThinkingPlaceholdersAndRender();
}
async function watchRun(run) {
  if (!run?.id) return;
  const inst = activeInstance(); if (!inst) return;
  closeRunStream();
  const controller = new AbortController(); state.runStream = controller;
  try {
    const resp = await fetch(`/api/v1/instances/${encodeURIComponent(inst.id)}/runs/${encodeURIComponent(run.id)}/events`, { headers: headers(false), signal: controller.signal });
    if (!resp.ok) { const err = new Error(`${resp.status} ${resp.statusText}`); err.status = resp.status; throw err; }
    const reader = resp.body.getReader(); const decoder = new TextDecoder(); let buf = "";
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const split = splitSSEBuffer(buf); buf = split.rest;
      split.frames.forEach(handleRunFrame);
    }
    buf += decoder.decode();
    if (buf.trim()) handleRunFrame(buf);
    await loadSessionsAndMessages();
  } catch (e) { if (e.name !== "AbortError") { removeThinkingPlaceholders(); renderMessages(); if (!handleAPIError(e)) toast(e.message); } }
  finally { if (state.runStream === controller) state.runStream = null; }
}
async function cancelCurrentRun() {
  const inst = activeInstance(); const run = state.currentRun;
  if (!inst || !run?.id) return;
  try {
    const out = await api(`/api/v1/instances/${encodeURIComponent(inst.id)}/runs/${encodeURIComponent(run.id)}/cancel`, { method: "POST", body: JSON.stringify({}) });
    renderRunPanel(out); closeRunStream(); await loadSessionsAndMessages(); toast(t("runCancelled"));
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
}
async function sendMessage(e) {
  e.preventDefault();
  const promptEl = $("prompt"); const sendBtn = $("sendBtn");
  if (!promptEl || !sendBtn || sendBtn.disabled) return;
  const inst = activeInstance(); const content = promptEl.value.trim();
  if (!inst || !content) return;
  const optimisticId = `local-user-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  closeRunStream();
  state.currentRun = null;
  sendBtn.disabled = true;
  promptEl.value = "";
  autoResizePrompt();
  state.messages.push({ id: optimisticId, role: "user", content, created_at: new Date().toISOString(), local_pending: true });
  addThinkingPlaceholder();
  try {
    setBusy(true);
    const body = { content, input_type: "text", title: t("webSession") };
    if (state.sessionId) body.session_id = state.sessionId;
    const out = await api(`/api/v1/instances/${encodeURIComponent(inst.id)}/messages`, { method: "POST", body: JSON.stringify(body) });
    state.sessionId = out.session?.id || state.sessionId;
    upsertSession(out.session);
    replaceLocalMessage(optimisticId, out.message);
    updateSendButtonState();
    renderRunPanel(out.run);
    if (out.run?.id) { addThinkingPlaceholder(out.run.id); watchRun(out.run); }
    else await loadSessionsAndMessages();
    toast(out.run?.status ? t("runStatus", { status: out.run.status }) : t("sent"));
  } catch (e2) { state.messages = state.messages.filter((m) => m.id !== optimisticId && !m.local_thinking); renderMessages(); if (!promptEl.value.trim()) promptEl.value = content; autoResizePrompt(); if (!handleAPIError(e2)) toast(e2.message); }
  finally { updateSendButtonState(); setBusy(false); }
}

async function renderSettings() {
  resetRunState();
  setTitle(t("settingsTitle"), t("settingsHint"));
  try {
    setBusy(true);
    const [schema, cfgResp] = await Promise.all([api("/api/v1/config/schema"), api("/api/v1/config")]);
    state.schema = items(schema);
    state.config = cfgResp.app_config || {};
    const validation = await api("/api/v1/config/validate", { method: "POST", body: JSON.stringify({ app_config: state.config }) });
    const valid = validation.valid ? "ok" : "error";
    $("content").innerHTML = `<section class="panel stack settings-panel"><div class="split"><div><h2>${t("sharedConfig")}</h2><p class="helper">${t("sharedConfigHint")}</p></div><span id="cfgStatus" class="badge ${valid}">${validation.valid ? t("valid") : t("needsAttention")}</span></div><div id="issues" class="stack"></div><div id="cfgTabs" class="cfg-tabs" role="tablist" aria-label="${esc(t("sharedConfig"))}"></div><form id="cfgForm" class="fields"></form><div class="row action-row"><button id="saveCfg" type="button" class="primary">${t("save")}</button><button id="validateCfg" type="button" class="secondary">${t("validate")}</button><button id="testCfg" type="button" class="secondary">${t("test")}</button></div><details class="cfg-output"><summary>${t("configResponse")}</summary><pre id="cfgOut" class="code"></pre></details></section>`;
    renderIssues(validation); renderConfigFields();
    $("saveCfg").onclick = saveConfig; $("validateCfg").onclick = validateConfig; $("testCfg").onclick = testConfig;
    setConfigOutput({ me: state.me, app_config: state.config });
  } catch (e) { if (!handleAPIError(e)) renderError(e); }
  finally { setBusy(false); }
}
function renderIssues(validation) { $("issues").innerHTML = (validation.issues || []).map((i) => `<p class="error"><strong>${esc(configIssueLabel(i))}</strong><span>${esc(configIssueMessage(i))}</span></p>`).join("") || `<p class="ok">${t("currentConfigOk")}</p>`; }
function updateConfigStatus(validation) { const el = $("cfgStatus"); if (!el) return; const valid = validation.valid ? "ok" : "error"; el.className = `badge ${valid}`; el.textContent = validation.valid ? t("valid") : t("needsAttention"); }
function setConfigOutput(value) { const el = $("cfgOut"); if (el) el.textContent = pretty(value); }
function setSettingsActionsDisabled(on) { ["saveCfg", "validateCfg", "testCfg"].forEach((id) => { const el = $(id); if (el) el.disabled = on; }); }
function configGroups(defs) {
  const byKey = Object.fromEntries(defs.map((x) => [x.key, x]));
  const allKeys = [...new Set([...defs.map((x) => x.key), ...Object.keys(state.config || {})])];
  const groups = [
    { id: "llm", title: t("groupLLM"), hint: t("groupLLMHint"), keys: ["maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_model", "maclaw_llm_current_provider", "maclaw_llm_providers"] },
    { id: "tools", title: t("groupTools"), hint: t("groupToolsHint"), keys: ["mcp_servers", "local_mcp_servers", "web_search_providers", "web_search_current_provider"] },
    { id: "skills", title: t("groupSkills"), hint: t("groupSkillsHint"), keys: ["nl_skills", "skill_hub_urls", "external_skill_dirs", "skill_sources_allowed"] },
    { id: "memory", title: t("groupMemory"), hint: t("groupMemoryHint"), keys: ["memory_auto_compress", "memory_max_backups", "knowledge_skill_token_budget"] },
    { id: "security", title: t("groupSecurity"), hint: t("groupSecurityHint"), keys: ["security_policy_mode", "sandbox_mode", "network_level", "yolo_mode_allowed"] },
  ];
  const used = new Set(groups.flatMap((g) => g.keys));
  const rest = allKeys.filter((key) => !used.has(key));
  const pick = (pred) => rest.filter(pred);
  groups.push(
    { id: "channels", title: t("groupChannels"), hint: t("groupChannelsHint"), keys: pick((key) => /^(qqbot|telegram|weixin|lansenger|thirdparty|asr_|tts_|audio_|voice_|noise_|speech_)/.test(key)) },
    { id: "ui", title: t("groupUI"), hint: t("groupUIHint"), keys: pick((key) => /^(ui_|show_|hide_|pet_|floating_|default_|remote_|power_|screen_|workstation_|check_|pause_|env_|language$|active_tool$|current_project$|projects$|extra_tool_configs$)/.test(key)) },
    { id: "advanced", title: t("groupAdvanced"), hint: t("groupAdvancedHint"), keys: [] }
  );
  const grouped = new Set(groups.flatMap((g) => g.keys));
  groups[groups.length - 1].keys = rest.filter((key) => !grouped.has(key));
  return groups.map((g) => ({ ...g, keys: g.keys.filter((key) => byKey[key] || Object.prototype.hasOwnProperty.call(state.config || {}, key)) })).filter((g) => g.keys.length);
}
function setActiveConfigTab(tab) {
  state.settingsTab = tab;
  document.querySelectorAll("[data-cfg-tab]").forEach((b) => { const on = b.dataset.cfgTab === tab; b.classList.toggle("active", on); b.setAttribute("aria-selected", on ? "true" : "false"); });
  document.querySelectorAll("[data-cfg-panel]").forEach((p) => { const off = p.dataset.cfgPanel !== tab; p.hidden = off; p.setAttribute("aria-hidden", off ? "true" : "false"); });
}
function moveConfigTab(current, delta) {
  const tabs = [...document.querySelectorAll("[data-cfg-tab]")];
  const idx = tabs.indexOf(current);
  if (idx < 0 || tabs.length === 0) return;
  const next = tabs[(idx + delta + tabs.length) % tabs.length];
  setActiveConfigTab(next.dataset.cfgTab);
  next.focus();
}
function fieldValue(key, def = {}) {
  const value = state.config[key];
  if (def.type === "array" || def.type === "object") return pretty(value || (def.type === "array" ? [] : {}));
  if (def.type === "bool") return value === true ? "true" : value === false ? "false" : "";
  return value ?? "";
}
function fieldInput(key, def) {
  const value = esc(fieldValue(key, def));
  if (def.type === "array" || def.type === "object") return `<textarea id="cfg_${key}" data-key="${esc(key)}" data-type="${esc(def.type)}">${value}</textarea>`;
  if (def.type === "bool") return `<select id="cfg_${key}" data-key="${esc(key)}" data-type="bool"><option value="" ${value === "" ? "selected" : ""}>${t("unset")}</option><option value="true" ${value === "true" ? "selected" : ""}>${t("trueValue")}</option><option value="false" ${value === "false" ? "selected" : ""}>${t("falseValue")}</option></select>`;
  if (def.type === "integer" || def.type === "number") return `<input id="cfg_${key}" data-key="${esc(key)}" data-type="${esc(def.type)}" type="number" value="${value}">`;
  return `<input id="cfg_${key}" data-key="${esc(key)}" data-type="string" type="${def.secret ? "password" : "text"}" ${def.secret ? 'autocomplete="new-password" spellcheck="false"' : ""} value="${value}">`;
}
function fieldHelper(d) {
  const text = d.description || d.example || "";
  const extra = d.secret ? t("secretHint") : "";
  return [text, extra].filter(Boolean).join(" ");
}
function parseConfigNumber(key, value, integer) {
  const raw = String(value || "").trim();
  if (raw === "") return undefined;
  const next = Number(raw);
  if (!Number.isFinite(next) || (integer && !Number.isInteger(next))) throw new Error(t("numberInvalid", { key: configIssueLabel({ key }), type: configTypeName(integer ? "integer" : "number") }));
  return next;
}
function renderConfigFields() {
  const defs = Object.fromEntries(state.schema.map((x) => [x.key, x]));
  const groups = configGroups(state.schema);
  if (!groups.some((g) => g.id === state.settingsTab)) state.settingsTab = groups[0]?.id || "";
  $("cfgTabs").innerHTML = groups.map((group) => `<button id="cfg_tab_${esc(group.id)}" type="button" role="tab" class="cfg-tab ${group.id === state.settingsTab ? "active" : ""}" data-cfg-tab="${esc(group.id)}" aria-controls="cfg_panel_${esc(group.id)}" aria-selected="${group.id === state.settingsTab ? "true" : "false"}">${esc(group.title)}</button>`).join("");
  $("cfgForm").innerHTML = groups.map((group) => `<fieldset id="cfg_panel_${esc(group.id)}" class="cfg-group" data-cfg-panel="${esc(group.id)}" role="tabpanel" aria-labelledby="cfg_tab_${esc(group.id)}" aria-hidden="${group.id === state.settingsTab ? "false" : "true"}" ${group.id === state.settingsTab ? "" : "hidden"}><legend>${esc(group.title)}</legend><p class="helper">${esc(group.hint)}</p>${group.keys.map((key) => {
    const d = fieldMeta(defs[key] || { key, title: key, type: Array.isArray(state.config[key]) ? "array" : typeof state.config[key] === "boolean" ? "bool" : typeof state.config[key] === "number" ? "number" : "string" });
    const label = `${esc(d.title || key)}${d.required ? " *" : ""}`;
    return `<div class="field"><label for="cfg_${key}">${label}</label>${fieldInput(key, d)}<span class="helper">${esc(fieldHelper(d))}</span></div>`;
  }).join("")}</fieldset>`).join("");
  document.querySelectorAll("[data-cfg-tab]").forEach((b) => {
    b.onclick = () => setActiveConfigTab(b.dataset.cfgTab);
    b.onkeydown = (e) => { if (e.key === "ArrowRight") { e.preventDefault(); moveConfigTab(b, 1); } else if (e.key === "ArrowLeft") { e.preventDefault(); moveConfigTab(b, -1); } };
  });
}
function collectConfig() {
  const next = { ...state.config };
  document.querySelectorAll("[data-key]").forEach((el) => {
    const key = el.dataset.key; const type = el.dataset.type || "string";
    if (type === "array" || type === "object") {
      try { next[key] = JSON.parse(el.value || (type === "array" ? "[]" : "{}")); } catch { throw new Error(t("jsonInvalid", { key: configIssueLabel({ key }) })); }
    } else if (type === "bool") {
      if (el.value === "") delete next[key]; else next[key] = el.value === "true";
    } else if (type === "integer") {
      const parsed = parseConfigNumber(key, el.value, true); if (parsed === undefined) delete next[key]; else next[key] = parsed;
    } else if (type === "number") {
      const parsed = parseConfigNumber(key, el.value, false); if (parsed === undefined) delete next[key]; else next[key] = parsed;
    } else { next[key] = el.value; }
  });
  return next;
}
async function saveConfig() { try { setBusy(true); setSettingsActionsDisabled(true); const next = collectConfig(); const out = await api("/api/v1/config", { method: "PUT", body: JSON.stringify({ app_config: next }) }); state.config = out.app_config || next; const validation = await api("/api/v1/config/validate", { method: "POST", body: JSON.stringify({ app_config: state.config }) }); try { await refreshInstances(); } catch (refreshErr) { if (refreshErr.status === 401) throw refreshErr; } updateConfigStatus(validation); renderIssues(validation); renderConfigFields(); setConfigOutput(out); toast(t("saved")); } catch (e) { if (!handleAPIError(e)) toast(e.message); } finally { setSettingsActionsDisabled(false); setBusy(false); } }
async function validateConfig() { try { setBusy(true); setSettingsActionsDisabled(true); const out = await api("/api/v1/config/validate", { method: "POST", body: JSON.stringify({ app_config: collectConfig() }) }); updateConfigStatus(out); renderIssues(out); setConfigOutput(out); toast(t("validated")); } catch (e) { if (!handleAPIError(e)) toast(e.message); } finally { setSettingsActionsDisabled(false); setBusy(false); } }
async function testConfig() { try { setBusy(true); setSettingsActionsDisabled(true); const out = await api("/api/v1/config/test", { method: "POST", body: JSON.stringify({ app_config: collectConfig() }) }); setConfigOutput(out); toast(out.success ? t("testPassed") : t("testFailed")); } catch (e) { if (!handleAPIError(e)) toast(e.message); } finally { setSettingsActionsDisabled(false); setBusy(false); } }

document.querySelectorAll("[data-view]").forEach((b) => b.onclick = () => { if (state.view !== b.dataset.view) resetRunState(); state.view = b.dataset.view; history.replaceState(null, "", `/app/?view=${state.view}${state.instanceId ? `&instance_id=${encodeURIComponent(state.instanceId)}` : ""}`); render(); });
$("logoutBtn").onclick = () => { resetRunState(); sessionStorage.removeItem("maclaw.user.token"); state.token = ""; renderMissingToken(); };
bootstrap();
