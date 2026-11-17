import { agentPrerequisite, providerURL, redactSecrets, resolveProtocol, sameOfficialModel } from "./agent-gate.mjs";
import { clickRef, semanticSnapshot, typeRef } from "./observe.mjs";
import { ARM_A_RULES, ARM_B_RULES, TASK_GOALS } from "./prompts.mjs";

export const AB_TASKS = [
  { id: "form_fill", path: "/form" },
  { id: "search_filter", path: "/search" },
  { id: "delayed_stale", path: "/stale?shift=1&delay=8000" },
  { id: "iframe_same", path: "/iframe?mode=same" },
  { id: "iframe_cross", path: "/iframe?mode=cross" },
  { id: "canvas_drag", path: "/canvas" },
];

const SCREENSHOT_TOOLS = [
  { name: "screenshot", description: "Return the viewport PNG. No DOM and no element refs.", parameters: { type: "object", properties: {} } },
  {
    name: "click",
    description: "Click the viewport pixel you choose.",
    parameters: { type: "object", properties: { x: { type: "number" }, y: { type: "number" } }, required: ["x", "y"] },
  },
  {
    name: "type",
    description: "Click the viewport pixel you choose, then type. Do not type secrets.",
    parameters: { type: "object", properties: { x: { type: "number" }, y: { type: "number" }, text: { type: "string" } }, required: ["x", "y", "text"] },
  },
  { name: "handoff", description: "Stop for a person. Do not type a password or code.", parameters: { type: "object", properties: { reason: { type: "string" } } } },
  { name: "done", description: "Stop. The harness scores the page, not this call.", parameters: { type: "object", properties: {} } },
];

const SEMANTIC_TOOLS = [
  { name: "snapshot", description: "Return text lines ref role \"name\". No screenshot and no coordinates.", parameters: { type: "object", properties: {} } },
  {
    name: "click",
    description: "Click one ref from the latest snapshot.",
    parameters: { type: "object", properties: { ref: { type: "string" } }, required: ["ref"] },
  },
  {
    name: "type",
    description: "Type into one ref from the latest snapshot. Do not type secrets.",
    parameters: { type: "object", properties: { ref: { type: "string" }, text: { type: "string" } }, required: ["ref", "text"] },
  },
  { name: "handoff", description: "Stop for a person. Do not type a password or code.", parameters: { type: "object", properties: { reason: { type: "string" } } } },
  { name: "done", description: "Stop. The harness scores the page, not this call.", parameters: { type: "object", properties: {} } },
];

export function equalArmBudget(taskCount, maxCalls, requestedPerArm) {
  const tasks = Math.max(1, taskCount);
  const ceiling = Math.max(1, maxCalls);
  let perArm = Math.max(1, Math.min(requestedPerArm, ceiling));
  while (tasks * 2 * perArm > ceiling && perArm > 1) perArm -= 1;
  return { per_arm_calls: perArm, fits: tasks * 2 * perArm <= ceiling };
}

function toolsFor(arm) {
  if (arm === "screenshot") return SCREENSHOT_TOOLS;
  if (arm === "semantic") return SEMANTIC_TOOLS;
  throw new Error(`arm ${arm} is not part of the two-arm run`);
}

function systemPrompt(arm, seed) {
  const rules = arm === "screenshot" ? ARM_A_RULES : ARM_B_RULES;
  return `${rules}\nseed=${seed}\n两臂使用同一个模型和同一组任务。`;
}

export function parseToolCalls(message) {
  const calls = message && Array.isArray(message.tool_calls) ? message.tool_calls : [];
  return calls.map((call) => {
    let args = {};
    let invalid = false;
    try {
      args = JSON.parse(call.function?.arguments || "{}");
    } catch {
      invalid = true;
    }
    return { id: call.id, name: call.function?.name || "", args, invalid };
  });
}

export function buildWire(env, transcript, tools) {
  const protocol = resolveProtocol(env);
  if (protocol === "anthropic") {
    const system = transcript.filter((turn) => turn.role === "system").map((turn) => turn.text).join("\n");
    const messages = [];
    for (const turn of transcript) {
      if (turn.role === "system") continue;
      if (turn.role === "assistant") {
        const content = [];
        if (turn.text) content.push({ type: "text", text: turn.text });
        for (const call of turn.toolCalls || []) content.push({ type: "tool_use", id: call.id, name: call.name, input: call.args });
        messages.push({ role: "assistant", content });
        continue;
      }
      if (turn.toolResult) {
        const content = [{ type: "tool_result", tool_use_id: turn.toolResult.id, content: turn.toolResult.text }];
        for (const image of turn.images || []) {
          content.push({ type: "image", source: { type: "base64", media_type: "image/png", data: image } });
        }
        messages.push({ role: "user", content });
        continue;
      }
      messages.push({ role: "user", content: turn.text });
    }
    return {
      protocol,
      headers: { "anthropic-version": "2023-06-01" },
      body: {
        model: env.MACLAW_BENCH_MODEL_NAME,
        max_tokens: 800,
        system,
        messages,
        tools: tools.map((tool) => ({ name: tool.name, description: tool.description, input_schema: tool.parameters })),
      },
    };
  }
  const messages = [];
  for (const turn of transcript) {
    if (turn.role === "system" || (turn.role === "user" && turn.text && !turn.toolResult)) {
      messages.push({ role: turn.role === "system" ? "system" : "user", content: turn.text });
      continue;
    }
    if (turn.role === "assistant") {
      messages.push({
        role: "assistant",
        content: turn.text || null,
        tool_calls: (turn.toolCalls || []).map((call) => ({
          id: call.id,
          type: "function",
          function: { name: call.name, arguments: JSON.stringify(call.args) },
        })),
      });
      continue;
    }
    if (turn.toolResult) {
      messages.push({ role: "tool", tool_call_id: turn.toolResult.id, content: turn.toolResult.text });
      if (turn.images?.length) {
        messages.push({
          role: "user",
          content: turn.images.map((image) => ({ type: "image_url", image_url: { url: `data:image/png;base64,${image}` } })),
        });
      }
    }
  }
  return {
    protocol,
    headers: {},
    body: {
      model: env.MACLAW_BENCH_MODEL_NAME,
      temperature: 0,
      messages,
      tools: tools.map((tool) => ({ type: "function", function: tool })),
      max_tokens: 800,
    },
  };
}

export function parseModelMessage(body, protocol) {
  if (protocol === "anthropic") {
    const blocks = Array.isArray(body?.content) ? body.content : [];
    return {
      text: blocks.filter((block) => block.type === "text").map((block) => block.text || "").join(""),
      toolCalls: blocks.filter((block) => block.type === "tool_use").map((block) => ({
        id: block.id,
        name: block.name,
        args: block.input && typeof block.input === "object" ? block.input : {},
        invalid: false,
      })),
      model: body?.model ? String(body.model) : null,
      usage: body?.usage
        ? { prompt_tokens: body.usage.input_tokens ?? null, completion_tokens: body.usage.output_tokens ?? null, total_tokens: null }
        : null,
    };
  }
  const message = body?.choices?.[0]?.message;
  return {
    text: typeof message?.content === "string" ? message.content : "",
    toolCalls: parseToolCalls(message),
    model: body?.model ? String(body.model) : null,
    usage: body?.usage
      ? {
          prompt_tokens: body.usage.prompt_tokens ?? null,
          completion_tokens: body.usage.completion_tokens ?? null,
          total_tokens: body.usage.total_tokens ?? null,
        }
      : null,
  };
}

export async function completeChat({ env, transcript, tools, trace, fetchImpl = fetch }) {
  const started = Date.now();
  const key = String(env.MACLAW_BENCH_MODEL_API_KEY || "");
  const wire = buildWire(env, transcript, tools);
  const headers = { "content-type": "application/json", ...wire.headers };
  if (key && wire.protocol === "anthropic") {
    headers["x-api-key"] = key;
    headers.authorization = `Bearer ${key}`;
  } else if (key) headers.authorization = `Bearer ${key}`;
  const response = await fetchImpl(providerURL(env.MACLAW_BENCH_MODEL_BASE_URL, wire.protocol), {
    method: "POST",
    headers,
    body: JSON.stringify(wire.body),
    signal: AbortSignal.timeout(90000),
  });
  const raw = await response.text();
  let body = null;
  try {
    body = JSON.parse(raw);
  } catch {
    body = null;
  }
  const parsed = parseModelMessage(body, wire.protocol);
  trace.push({
    kind: "model_call",
    protocol: wire.protocol,
    ms: Date.now() - started,
    http_status: response.status,
    usage: parsed.usage,
    response_model: parsed.model,
    error: response.ok ? null : redactSecrets(raw, [key]),
  });
  if (!response.ok) {
    const error = new Error(`model http ${response.status}`);
    error.redacted = redactSecrets(raw, [key]);
    error.status = response.status;
    throw error;
  }
  if (!sameOfficialModel(env.MACLAW_BENCH_MODEL_NAME, parsed.model)) {
    const error = new Error("model_mismatch");
    error.code = "model_mismatch";
    error.response_model = parsed.model;
    throw error;
  }
  return parsed;
}

async function focusedIsPassword(page) {
  if (!page.evaluate) return false;
  return page.evaluate(() => {
    const element = document.activeElement;
    return Boolean(element && String(element.getAttribute?.("type") || element.type || "").toLowerCase() === "password");
  });
}

async function applyScreenshot(page, call, trace) {
  if (call.invalid || !Number.isFinite(call.args.x) || !Number.isFinite(call.args.y)) {
    return { ok: false, error: "invalid_arguments" };
  }
  const x = call.args.x;
  const y = call.args.y;
  trace.push({ kind: "browser_primitive", arm: "screenshot", op: call.name, x, y, model_call: false });
  await page.mouse.click(x, y);
  if (call.name === "type") {
    if (await focusedIsPassword(page)) return { ok: false, error: "secret_field_refused" };
    await page.keyboard.type(String(call.args.text || ""));
  }
  return { ok: true };
}

async function applySemantic(page, call, snap, trace) {
  if (call.invalid || !call.args.ref) return { ok: false, error: "invalid_arguments" };
  const ref = String(call.args.ref);
  trace.push({ kind: "browser_primitive", arm: "semantic", op: call.name, ref, model_call: false });
  if (!snap) return { ok: false, error: "snapshot_required" };
  if (call.name === "type") {
    if (snap.store.get(ref)?.type === "password") return { ok: false, error: "secret_field_refused" };
    return typeRef(page, snap.store, ref, String(call.args.text || ""));
  }
  return clickRef(snap.store, ref);
}

export async function runProvisionedTrial({ page, arm, goal, env = process.env, fetchImpl = fetch, maxCalls = 5, contractTest = false, snapshotFn = null }) {
  const gate = agentPrerequisite(env);
  if (!gate.ok) return { executed: false, ready: false, model_calls: 0, observed_llm: false, gate };
  const tools = toolsFor(arm);
  const seed = String(env.MACLAW_BENCH_SEED || "bench-1");
  const transcript = [
    { role: "system", text: systemPrompt(arm, seed) },
    { role: "user", text: goal },
  ];
  const trace = [];
  let modelCalls = 0;
  let snap = null;
  let aborted = null;
  let simulatedHandoff = false;
  try {
    while (modelCalls < maxCalls) {
      const parsed = await completeChat({ env, transcript, tools, trace, fetchImpl });
      modelCalls += 1;
      const calls = parsed.toolCalls || [];
      transcript.push({ role: "assistant", text: parsed.text || "", toolCalls: calls });
      if (calls.length === 0) break;
      for (const call of calls) {
        const reply = (text, images = []) => transcript.push({ role: "user", toolResult: { id: call.id, text }, images });
        if (call.name === "done") {
          reply(JSON.stringify({ ok: true }));
          return finish({ page, trace, modelCalls, arm, aborted, simulatedHandoff, contractTest, stopped: "done" });
        }
        if (call.name === "handoff") {
          simulatedHandoff = true;
          if (page.evaluate) {
            await page.evaluate(() => {
              window.__simulatedHandoff = true;
            });
          }
          reply(JSON.stringify({ ok: true, simulated_handoff: true, human_verified: false }));
          return finish({ page, trace, modelCalls, arm, aborted, simulatedHandoff, contractTest, stopped: "handoff" });
        }
        if (arm === "screenshot" && call.name === "screenshot") {
          const buffer = Buffer.from(await page.screenshot({ type: "png" }));
          const b64 = buffer.toString("base64");
          trace.push({ kind: "observation", arm, channel: "screenshot", bytes: buffer.length, model_call: false });
          reply("png attached", [b64]);
          continue;
        }
        if (arm === "semantic" && call.name === "snapshot") {
          snap = snapshotFn ? await snapshotFn(page) : await semanticSnapshot(page);
          trace.push({ kind: "observation", arm, channel: "semantic_snapshot", bytes: Buffer.byteLength(snap.text), model_call: false });
          reply(snap.text);
          continue;
        }
        const result = arm === "screenshot" ? await applyScreenshot(page, call, trace) : await applySemantic(page, call, snap, trace);
        reply(JSON.stringify({ ok: Boolean(result.ok), error: result.error || null }));
      }
    }
  } catch (error) {
    const text = redactSecrets(error.redacted || error.message || "", [env.MACLAW_BENCH_MODEL_API_KEY]);
    if (error.code === "model_mismatch" || /model_mismatch/.test(text)) aborted = "model_mismatch";
    else if (arm === "screenshot" && /image|vision|multimodal/i.test(text)) aborted = "vision_rejected";
    else aborted = "model_error";
    trace.push({ kind: "abort", arm, reason: aborted, error: text, model_call: false });
  }
  const stopped = modelCalls >= maxCalls ? "call_cap" : aborted || "model_stopped";
  return finish({ page, trace, modelCalls, arm, aborted, simulatedHandoff, contractTest, stopped });
}

async function finish({ page, trace, modelCalls, arm, aborted, simulatedHandoff, contractTest, stopped }) {
  let pageOutcome = null;
  if (page.evaluate) {
    pageOutcome = await page.evaluate(() => (typeof window.__benchOutcome === "function" ? window.__benchOutcome() : null));
  }
  const humanVerified = process.env.MACLAW_BENCH_HUMAN_CONFIRMED === "1";
  return {
    executed: true,
    ready: true,
    arm,
    observed_llm: contractTest !== true,
    contract_test_not_observed_llm: contractTest === true,
    model_calls: modelCalls,
    stopped,
    aborted,
    simulated_handoff: simulatedHandoff,
    human_verified: simulatedHandoff ? humanVerified : false,
    browser_primitives: trace.filter((event) => event.kind === "browser_primitive").length,
    page_outcome: pageOutcome,
    trace,
  };
}

export async function runBoundedArms({ env = process.env, browser, origin, tasks = AB_TASKS, fetchImpl = fetch, contractTest = false, pageFactory }) {
  const gate = agentPrerequisite(env);
  if (!gate.ok) return { executed: false, ready: false, observed_llm: false, model_calls: 0, gate };
  const requested = Number(env.MACLAW_BENCH_PER_ARM_CALLS || 5);
  const budget = equalArmBudget(tasks.length, gate.max_calls, requested);
  if (!budget.fits) return { executed: false, ready: false, observed_llm: false, model_calls: 0, reason: "budget_does_not_fit_equal_arms" };
  const results = [];
  let aborted = false;
  for (const task of tasks) {
    for (const arm of ["screenshot", "semantic"]) {
      const page = pageFactory ? await pageFactory(task, arm) : await openTask(browser, origin, task);
      const trial = await runProvisionedTrial({
        page,
        arm,
        goal: TASK_GOALS[task.id] || task.id,
        env,
        fetchImpl,
        maxCalls: budget.per_arm_calls,
        contractTest,
      });
      if (!pageFactory && page.context) await page.context().close();
      results.push({ task: task.id, ...trial });
      if (trial.aborted) aborted = true;
    }
  }
  return {
    executed: true,
    ready: true,
    observed_llm: contractTest !== true,
    contract_test_not_observed_llm: contractTest === true,
    model_name: env.MACLAW_BENCH_MODEL_NAME,
    per_arm_calls: budget.per_arm_calls,
    seed: env.MACLAW_BENCH_SEED || "bench-1",
    model_calls: results.reduce((sum, trial) => sum + trial.model_calls, 0),
    aborted,
    results,
  };
}

async function openTask(browser, origin, task) {
  const context = await browser.newContext({ viewport: { width: 1100, height: 720 }, deviceScaleFactor: 1, locale: "zh-CN" });
  const page = await context.newPage();
  await page.goto(origin + task.path, { waitUntil: "domcontentloaded" });
  return page;
}
