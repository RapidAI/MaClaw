const OFFICIAL_MODEL = "glm-5.3-flash";

export function chatCompletionsURL(base) {
  const url = new URL(base);
  const path = url.pathname.replace(/\/$/, "");
  if (!path.endsWith("/chat/completions")) url.pathname = `${path}/chat/completions`;
  return url;
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

export function agentPrerequisite(env = process.env) {
  const loopback = String(env.MACLAW_BENCH_LOOPBACK || "") === "1";
  const required = ["MACLAW_BENCH_MODEL_BASE_URL", "MACLAW_BENCH_MODEL_NAME", "MACLAW_BENCH_MODEL_VISION", "MACLAW_BENCH_CONFIRM"];
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
  const model = String(env.MACLAW_BENCH_MODEL_NAME || "").trim();
  if (model && model !== OFFICIAL_MODEL) problems.push(`两臂模型必须同为 ${OFFICIAL_MODEL}`);
  if (String(env.MACLAW_BENCH_MODEL_VISION || "") !== "1" && !missing.includes("MACLAW_BENCH_MODEL_VISION")) {
    problems.push("MACLAW_BENCH_MODEL_VISION 必须为 1；截图臂不能退成纯文本");
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
    official_model: OFFICIAL_MODEL,
    max_calls: maxCalls,
    temperature: 0,
    protocol: "openai-chat-completions-tools",
    model_calls: 0,
    intended_model_note:
      "Bot1 官方直连的模型 id 按仓库既有调用是 glm-5.3-flash，OpenAI 兼容基址形如 https://open.bigmodel.cn/api/paas/v4。夹具不读取该密钥。",
    note: "密钥只存在于调用进程的环境或 Bot1 机器上的 127.0.0.1 代理。不要把生产配置路径交给夹具。",
  };
}
