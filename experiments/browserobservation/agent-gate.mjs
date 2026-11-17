const OFFICIAL_TAIL = "glm-5.3-flash";
const OFFICIAL_MEMBER = "智谱编程/glm-5.3-flash";

export function officialModelTail(name) {
  const text = String(name || "").trim();
  return text.includes("/") ? text.slice(text.lastIndexOf("/") + 1) : text;
}

export function isOfficialFlashMember(name) {
  const text = String(name || "").trim();
  return text === OFFICIAL_TAIL || text === OFFICIAL_MEMBER;
}

export function sameOfficialModel(requested, response) {
  const got = String(response || "").trim();
  if (!got) return true;
  return isOfficialFlashMember(requested) && officialModelTail(requested) === officialModelTail(got) && officialModelTail(got) === OFFICIAL_TAIL;
}

export function resolveProtocol(env) {
  const explicit = String(env.MACLAW_BENCH_PROTOCOL || "").trim().toLowerCase();
  if (explicit === "anthropic" || explicit === "openai") return explicit;
  const base = String(env.MACLAW_BENCH_MODEL_BASE_URL || "");
  return base.includes("/anthropic") ? "anthropic" : "openai";
}

export function providerURL(base, protocol) {
  const url = new URL(base);
  const path = url.pathname.replace(/\/$/, "");
  const suffix = protocol === "anthropic" ? "/v1/messages" : "/chat/completions";
  if (path.endsWith("/v1")) url.pathname = `${path}${suffix.slice(3)}`;
  else if (!path.endsWith(suffix)) url.pathname = `${path}${suffix}`;
  return url;
}

export function chatCompletionsURL(base) {
  return providerURL(base, "openai");
}

export function redactSecrets(text, secrets = []) {
  let out = String(text || "");
  for (const secret of secrets) {
    const value = String(secret || "");
    if (value.length >= 6) out = out.split(value).join("[redacted]");
  }
  out = out.replace(/Bearer\s+\S+/gi, "Bearer [redacted]");
  out = out.replace(/\bsk-[A-Za-z0-9_\-]+\b/g, "[redacted]");
  return out.slice(0, 300);
}

export function agentPrerequisite(env = process.env, options = {}) {
  const loopback = String(env.MACLAW_BENCH_LOOPBACK || "") === "1";
  const required = ["MACLAW_BENCH_MODEL_BASE_URL", "MACLAW_BENCH_MODEL_NAME", "MACLAW_BENCH_CONFIRM"];
  if (!loopback) required.splice(1, 0, "MACLAW_BENCH_MODEL_API_KEY");
  const missing = required.filter((name) => !String(env[name] || "").trim());
  const problems = [];
  if (String(env.MACLAW_BENCH_CONFIG_PATH || "").trim() || String(env.MACLAW_BENCH_READ_APP_CONFIG || "") === "1") {
    problems.push("夹具不读取生产配置或 Bot1 密钥文件");
  }
  const base = String(env.MACLAW_BENCH_MODEL_BASE_URL || "").trim();
  let parsed = null;
  if (base) {
    try {
      parsed = new URL(base);
    } catch {
      problems.push("MACLAW_BENCH_MODEL_BASE_URL 不是 URL");
    }
    if (parsed && parsed.username) problems.push("不要把密钥放进 URL");
    if (parsed && parsed.protocol !== "https:") {
      const local = parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost";
      if (!(loopback && local && parsed.protocol === "http:")) problems.push("端点必须是 https；只有 MACLAW_BENCH_LOOPBACK=1 才允许本机 http");
    }
    if (parsed && /coding\/paas|opencode\.ai/i.test(parsed.href)) {
      problems.push("官方直连不要改走 coding 通道或 OpenCode");
    }
  }
  const route = String(env.MACLAW_BENCH_ROUTE || "").trim().toLowerCase();
  if (route === "admin-test" || route === "fixed-prompt" || String(env.MACLAW_BENCH_ADMIN_TEST || "") === "1") {
    problems.push("管理端 provider/test 只接受 model、node、tools、tool_choice 和固定测试提示，不能发任意消息或图片，不是夹具路由");
  }
  const model = String(env.MACLAW_BENCH_MODEL_NAME || "").trim();
  const member = options.member || "official-flash";
  if (model && member === "official-flash" && !isOfficialFlashMember(model)) problems.push(`文本模型必须是 ${OFFICIAL_MEMBER} 或 ${OFFICIAL_TAIL}`);
  if (model && member === "named" && isOfficialFlashMember(model)) problems.push("视觉助手必须是另一个模型，不能再用 glm-5.3-flash 成员");
  const protocol = resolveProtocol(env);
  if (String(env.MACLAW_BENCH_PROTOCOL || "").trim() && !["anthropic", "openai"].includes(String(env.MACLAW_BENCH_PROTOCOL).trim().toLowerCase())) {
    problems.push("MACLAW_BENCH_PROTOCOL 只能是 anthropic 或 openai");
  }
  if (String(env.MACLAW_BENCH_CONFIRM || "") !== "1" && !missing.includes("MACLAW_BENCH_CONFIRM")) {
    problems.push("MACLAW_BENCH_CONFIRM 必须为 1");
  }
  const requested = Number(env.MACLAW_BENCH_MAX_CALLS || 180);
  const maxCalls = Number.isFinite(requested) ? Math.min(200, Math.max(0, Math.floor(requested))) : 180;
  return {
    ok: missing.length === 0 && problems.length === 0,
    missing,
    problems,
    present_names_only: required.filter((name) => String(env[name] || "").trim() && name !== "MACLAW_BENCH_MODEL_API_KEY"),
    key_present: Boolean(String(env.MACLAW_BENCH_MODEL_API_KEY || "").trim()),
    loopback,
    model_name: model || null,
    official_model: OFFICIAL_TAIL,
    official_member: OFFICIAL_MEMBER,
    max_calls: maxCalls,
    temperature: 0,
    protocol: base ? protocol : null,
    vision_assumed: false,
    image_route: String(env.MACLAW_BENCH_IMAGE_ROUTE || "") === "confirmed" ? "confirmed" : "unconfirmed",
    same_model_vision_arm: String(env.MACLAW_BENCH_IMAGE_ROUTE || "") === "confirmed",
    admin_test_is_harness: false,
    model_calls: 0,
    intended_model_note:
      "标准推理路由仍由父流程提供。管理端固定测试提示不是这条路由。官方模型页写明 glm-5.3-flash 接受图像，成员标签未列 vision 不能当成否定。未见到该路由的成功图片响应之前，不发送图片。",
    note: "密钥只存在于调用进程的环境或 Bot1 机器上的 127.0.0.1 代理。不要把生产配置路径交给夹具。",
  };
}

export function helperPrerequisite(env = process.env) {
  const problems = [];
  const name = String(env.MACLAW_BENCH_HELPER_MODEL_NAME || "").trim();
  const base = String(env.MACLAW_BENCH_HELPER_MODEL_BASE_URL || "").trim();
  const loopback = String(env.MACLAW_BENCH_LOOPBACK || "") === "1";
  const key = String(env.MACLAW_BENCH_HELPER_MODEL_API_KEY || "").trim();
  if (!name) problems.push("缺少 MACLAW_BENCH_HELPER_MODEL_NAME");
  if (!base) problems.push("缺少 MACLAW_BENCH_HELPER_MODEL_BASE_URL");
  if (name && isOfficialFlashMember(name)) problems.push("视觉助手必须是另一个模型，不能再用 glm-5.3-flash 成员");
  if (!loopback && !key) problems.push("视觉助手使用自己的 MACLAW_BENCH_HELPER_MODEL_API_KEY，不借用文本模型密钥");
  let parsed = null;
  if (base) {
    try {
      parsed = new URL(base);
    } catch {
      problems.push("MACLAW_BENCH_HELPER_MODEL_BASE_URL 不是 URL");
    }
    if (parsed && parsed.username) problems.push("不要把助手密钥放进 URL");
    if (parsed && parsed.protocol !== "https:") {
      const local = parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost";
      if (!(loopback && local && parsed.protocol === "http:")) problems.push("助手端点必须是 https；只有回环才允许本机 http");
    }
  }
  const protocol = String(env.MACLAW_BENCH_HELPER_PROTOCOL || env.MACLAW_BENCH_PROTOCOL || "").trim().toLowerCase();
  if (protocol && !["anthropic", "openai"].includes(protocol)) problems.push("助手协议只能是 anthropic 或 openai");
  return {
    ok: problems.length === 0,
    problems,
    model_name: name || null,
    protocol: protocol || null,
    key_present: Boolean(key),
    confounded: true,
    not_same_model: true,
    not_primary_ab: true,
  };
}

export function helperTrialEnv(env) {
  return {
    ...env,
    MACLAW_BENCH_MODEL_BASE_URL: env.MACLAW_BENCH_HELPER_MODEL_BASE_URL,
    MACLAW_BENCH_MODEL_NAME: env.MACLAW_BENCH_HELPER_MODEL_NAME,
    MACLAW_BENCH_MODEL_API_KEY: env.MACLAW_BENCH_HELPER_MODEL_API_KEY || "",
    MACLAW_BENCH_PROTOCOL: env.MACLAW_BENCH_HELPER_PROTOCOL || env.MACLAW_BENCH_PROTOCOL || "",
    MACLAW_BENCH_ROUTE: "",
    MACLAW_BENCH_ADMIN_TEST: "",
  };
}
