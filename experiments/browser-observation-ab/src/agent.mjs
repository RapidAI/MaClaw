import { agentPrerequisite } from "./agent-gate.mjs";
import { clickRef, semanticSnapshot, typeRef } from "./observe.mjs";

const MAX_STEPS = 5;

function toolsFor(arm) {
  const batch =
    arm === "semantic"
      ? {
          name: "batch_ref",
          description: "在同一次工具调用里执行多步，复用当前快照的 ref。目标不在快照里时会先重新快照，这一步不是新的模型调用。",
          parameters: {
            type: "object",
            properties: {
              steps: {
                type: "array",
                items: {
                  type: "object",
                  properties: {
                    op: { type: "string", enum: ["click", "type"] },
                    ref: { type: "string" },
                    text: { type: "string" },
                  },
                  required: ["op", "ref"],
                },
              },
            },
            required: ["steps"],
          },
        }
      : {
          name: "batch_xy",
          description: "在同一次工具调用里执行多步坐标动作。不在两次动作之间强制截图。",
          parameters: {
            type: "object",
            properties: {
              steps: {
                type: "array",
                items: {
                  type: "object",
                  properties: {
                    op: { type: "string", enum: ["click", "type"] },
                    x: { type: "number" },
                    y: { type: "number" },
                    text: { type: "string" },
                  },
                  required: ["op", "x", "y"],
                },
              },
            },
            required: ["steps"],
          },
        };
  const observe =
    arm === "semantic"
      ? { name: "snapshot", description: "读取语义快照和 ref。不返回截图。", parameters: { type: "object", properties: {} } }
      : { name: "screenshot", description: "返回当前视口 PNG。不返回 DOM 或 ref。", parameters: { type: "object", properties: {} } };
  return [
    observe,
    batch,
    { name: "handoff", description: "把密码或验证码交给人工。不要填写秘密。", parameters: { type: "object", properties: { reason: { type: "string" } } } },
    { name: "done", description: "结束本试。成功与否由页面外检查决定。", parameters: { type: "object", properties: {} } },
  ];
}

async function complete(env, messages, tools, trace) {
  const started = Date.now();
  const response = await fetch(new URL("/chat/completions", env.MACLAW_BENCH_MODEL_BASE_URL), {
    method: "POST",
    headers: {
      authorization: `Bearer ${env.MACLAW_BENCH_MODEL_API_KEY}`,
      "content-type": "application/json",
    },
    body: JSON.stringify({
      model: env.MACLAW_BENCH_MODEL_NAME,
      temperature: 0,
      messages,
      tools: tools.map((tool) => ({ type: "function", function: tool })),
      max_tokens: 800,
    }),
    signal: AbortSignal.timeout(90000),
  });
  const raw = await response.text();
  let body = null;
  try {
    body = JSON.parse(raw);
  } catch {
    body = null;
  }
  const usage = body && body.usage ? body.usage : null;
  trace.push({
    kind: "model_call",
    ms: Date.now() - started,
    http_status: response.status,
    usage: usage
      ? {
          prompt_tokens: usage.prompt_tokens ?? null,
          completion_tokens: usage.completion_tokens ?? null,
          total_tokens: usage.total_tokens ?? null,
        }
      : null,
    error: response.ok ? null : raw.slice(0, 300),
  });
  if (!response.ok) throw new Error(`model http ${response.status}`);
  return body;
}

export async function runProvisionedTrial({ page, arm, goal, env = process.env }) {
  const gate = agentPrerequisite(env);
  if (!gate.ok) {
    return { executed: false, model_calls: 0, gate };
  }
  const tools = toolsFor(arm);
  const messages = [
    { role: "system", content: arm === "semantic" ? (await import("./prompts.mjs")).ARM_B_RULES : (await import("./prompts.mjs")).ARM_A_RULES },
    { role: "user", content: goal },
  ];
  const trace = [];
  let modelCalls = 0;
  let snap = null;
  const budget = gate.max_calls;
  for (let step = 0; step < MAX_STEPS; step += 1) {
    if (modelCalls >= budget) break;
    const body = await complete(env, messages, tools, trace);
    modelCalls += 1;
    const message = body.choices && body.choices[0] && body.choices[0].message;
    if (!message) break;
    messages.push(message);
    const calls = message.tool_calls || [];
    if (!calls.length) break;
    for (const call of calls) {
      const name = call.function.name;
      let args = {};
      try {
        args = JSON.parse(call.function.arguments || "{}");
      } catch {
        args = {};
      }
      let content = "";
      if (name === "done") content = JSON.stringify({ ok: true });
      else if (name === "handoff") {
        await page.evaluate(() => {
          window.__humanCompleted = true;
        });
        content = JSON.stringify({ ok: true, human_completed: true });
      } else if (name === "snapshot") {
        snap = await semanticSnapshot(page);
        content = snap.text;
      } else if (name === "screenshot") {
        const buffer = await page.screenshot({ type: "png" });
        trace.push({ kind: "observation", channel: "screenshot", bytes: buffer.length, model_call: false });
        content = JSON.stringify({ bytes: buffer.length });
        messages.push({
          role: "tool",
          tool_call_id: call.id,
          content: [
            { type: "text", text: content },
            { type: "image_url", image_url: { url: `data:image/png;base64,${buffer.toString("base64")}` } },
          ],
        });
        continue;
      } else if (name === "batch_ref" || name === "batch_xy") {
        const results = [];
        for (const item of args.steps || []) {
          if (name === "batch_ref") {
            if (!snap) snap = await semanticSnapshot(page);
            const result = item.op === "type" ? await typeRef(page, snap.store, item.ref, item.text || "") : await clickRef(snap.store, item.ref);
            if (result.error === "stale_ref") {
              snap = await semanticSnapshot(page);
              results.push({ ...result, retried_observation: true });
            } else results.push(result);
            trace.push({ kind: "browser_primitive", op: item.op, ok: Boolean(result.ok), error: result.error || null, model_call: false, chat_progress: true });
          } else {
            const secret = await page.evaluate(({ x, y }) => {
              const element = document.elementFromPoint(x, y);
              return element ? (element.getAttribute("type") || "").toLowerCase() : "";
            }, { x: item.x, y: item.y });
            if (secret === "password") {
              trace.push({ kind: "browser_primitive", op: item.op, ok: false, error: "secret_field_refused", model_call: false, chat_progress: true });
              results.push({ ok: false, error: "secret_field_refused" });
              continue;
            }
            trace.push({ kind: "browser_primitive", op: item.op, ok: true, model_call: false, chat_progress: true });
            if (item.op === "click") await page.mouse.click(item.x, item.y);
            else {
              await page.mouse.click(item.x, item.y);
              await page.keyboard.type(item.text || "");
            }
            results.push({ ok: true });
          }
        }
        content = JSON.stringify({ results });
      } else content = JSON.stringify({ ok: false, error: "unknown_tool" });
      messages.push({ role: "tool", tool_call_id: call.id, content });
      if (name === "done" || name === "handoff") {
        return finish(page, trace, modelCalls, arm);
      }
    }
  }
  return finish(page, trace, modelCalls, arm);
}

async function finish(page, trace, modelCalls, arm) {
  const outcome = await page.evaluate(() => (typeof window.__benchOutcome === "function" ? window.__benchOutcome() : null));
  return {
    executed: true,
    arm,
    model: process.env.MACLAW_BENCH_MODEL_NAME,
    model_calls: modelCalls,
    browser_primitives: trace.filter((event) => event.kind === "browser_primitive").length,
    observations: trace.filter((event) => event.kind === "observation").length,
    usage: trace.map((event) => event.usage).filter(Boolean),
    outcome,
    trace,
  };
}
