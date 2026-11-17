const REQUIRED = [
  "MACLAW_BENCH_MODEL_BASE_URL",
  "MACLAW_BENCH_MODEL_API_KEY",
  "MACLAW_BENCH_MODEL_NAME",
  "MACLAW_BENCH_MODEL_VISION",
  "MACLAW_BENCH_CONFIRM",
];

export function agentPrerequisite(env = process.env) {
  const missing = REQUIRED.filter((name) => !String(env[name] || "").trim());
  const present = REQUIRED.filter((name) => String(env[name] || "").trim());
  const problems = [];
  const base = String(env.MACLAW_BENCH_MODEL_BASE_URL || "").trim();
  if (base) {
    let parsed;
    try {
      parsed = new URL(base);
    } catch {
      problems.push("MACLAW_BENCH_MODEL_BASE_URL 不是 URL");
    }
    if (parsed && parsed.protocol !== "https:" && !(parsed.protocol === "http:" && (parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost"))) {
      problems.push("端点必须是 https，或本机 http");
    }
    if (parsed && (parsed.username || parsed.password || /key|token|secret/i.test(parsed.href))) {
      problems.push("不要把密钥放进 URL");
    }
  }
  if (String(env.MACLAW_BENCH_MODEL_VISION || "") !== "1") {
    if (!missing.includes("MACLAW_BENCH_MODEL_VISION")) problems.push("MACLAW_BENCH_MODEL_VISION 必须为 1，表示同一视觉模型");
  }
  if (String(env.MACLAW_BENCH_CONFIRM || "") !== "1") {
    if (!missing.includes("MACLAW_BENCH_CONFIRM")) problems.push("MACLAW_BENCH_CONFIRM 必须为 1");
  }
  const requested = Number(env.MACLAW_BENCH_MAX_CALLS || 180);
  const maxCalls = Number.isFinite(requested) ? Math.min(200, Math.max(0, Math.floor(requested))) : 180;
  return {
    ok: missing.length === 0 && problems.length === 0,
    missing,
    problems,
    present_names_only: present,
    model_name: missing.includes("MACLAW_BENCH_MODEL_NAME") ? null : String(env.MACLAW_BENCH_MODEL_NAME || ""),
    max_calls: maxCalls,
    temperature: 0,
    protocol: "openai-chat-completions-tools",
    intended_model_note:
      "父流程指定同一模型为 GLM 5.3 Flash（OpenBot 免费目录 id 为 opencode/glm-5.3-flash-free）。只有 MACLAW_BENCH_* 被显式设置且 CONFIRM=1 才允许调用；不读取 OpenCode 配置、不替换别的模型。",
    model_calls: 0,
    note: "未设置时禁止调用模型，也不把脚本策略记成模型成功。不要读取生产配置、用户会话或 ~/.maclaw。",
  };
}
