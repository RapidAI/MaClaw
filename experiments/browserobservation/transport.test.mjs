import assert from "node:assert/strict";
import http from "node:http";
import test from "node:test";
import { equalArmBudget, runProvisionedTrial } from "./agent.mjs";

const env = {
  MACLAW_BENCH_LOOPBACK: "1",
  MACLAW_BENCH_MODEL_BASE_URL: "",
  MACLAW_BENCH_MODEL_API_KEY: "sk-contract-not-a-provider-key",
  MACLAW_BENCH_MODEL_NAME: "glm-5.3-flash",
  MACLAW_BENCH_MODEL_VISION: "1",
  MACLAW_BENCH_CONFIRM: "1",
  MACLAW_BENCH_SEED: "bench-1",
};

function serve(handler) {
  const server = http.createServer(handler);
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => resolve(server));
  });
}

function png() {
  return Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x01, 0x02]);
}

test("equal budgets fit both arms inside the shared cap", () => {
  const budget = equalArmBudget(6, 180, 5);
  assert.equal(budget.per_arm_calls, 5);
  assert.equal(budget.fits, true);
  assert.equal(equalArmBudget(6, 10, 5).per_arm_calls, 1);
});

test("screenshot arm sends the png and clicks only the model's coordinates", async () => {
  const image = png();
  const clicks = [];
  const bodies = [];
  let step = 0;
  const server = await serve(async (req, res) => {
    const raw = await new Promise((resolve) => {
      const chunks = [];
      req.on("data", (chunk) => chunks.push(chunk));
      req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    });
    bodies.push(raw);
    step += 1;
    const message = step === 1
      ? { tool_calls: [{ id: "s", type: "function", function: { name: "screenshot", arguments: "{}" } }] }
      : { tool_calls: [{ id: "c", type: "function", function: { name: "click", arguments: JSON.stringify({ x: 12, y: 34 }) } }] };
    if (step > 2) message.tool_calls = [{ id: "d", type: "function", function: { name: "done", arguments: "{}" } }];
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ model: "glm-5.3-flash", choices: [{ message }] }));
  });
  const base = `http://127.0.0.1:${server.address().port}/v1`;
  const page = {
    clicks,
    async screenshot() { return image; },
    mouse: { async click(x, y) { clicks.push({ x, y }); } },
    keyboard: { async type() { throw new Error("type was not requested"); } },
    async evaluate() {
      if (clicks.length === 0) throw new Error("screenshot arm asked the DOM for a coordinate");
      return null;
    },
  };
  const trial = await runProvisionedTrial({
    page,
    arm: "screenshot",
    goal: "click",
    env: { ...env, MACLAW_BENCH_MODEL_BASE_URL: base },
    maxCalls: 4,
    contractTest: true,
  });
  await new Promise((resolve) => server.close(resolve));
  assert.equal(trial.contract_test_not_observed_llm, true);
  assert.equal(trial.observed_llm, false);
  assert.deepEqual(clicks, [{ x: 12, y: 34 }]);
  assert.match(bodies[1], new RegExp(image.toString("base64")));
  assert.equal(JSON.stringify(trial).includes(env.MACLAW_BENCH_MODEL_API_KEY), false);
});

test("vision rejection aborts the screenshot arm instead of dropping the image", async () => {
  let step = 0;
  const server = await serve(async (req, res) => {
    req.resume();
    step += 1;
    if (step === 1) {
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({
        model: "glm-5.3-flash",
        choices: [{ message: { tool_calls: [{ id: "s", type: "function", function: { name: "screenshot", arguments: "{}" } }] } }],
      }));
      return;
    }
    res.writeHead(400, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: { message: "image input is not supported" } }));
  });
  const clicks = [];
  const trial = await runProvisionedTrial({
    page: {
      async screenshot() { return png(); },
      mouse: { async click(x, y) { clicks.push({ x, y }); } },
      async evaluate() { return false; },
    },
    arm: "screenshot",
    goal: "click",
    env: { ...env, MACLAW_BENCH_MODEL_BASE_URL: `http://127.0.0.1:${server.address().port}/v1` },
    maxCalls: 4,
    contractTest: true,
  });
  await new Promise((resolve) => server.close(resolve));
  assert.equal(trial.aborted, "vision_rejected");
  assert.equal(trial.contract_test_not_observed_llm, true);
  assert.deepEqual(clicks, []);
});

test("a different response model aborts and the call cap stops a looping tool", async () => {
  const mismatch = await serve(async (req, res) => {
    req.resume();
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ model: "glm-5.3", choices: [{ message: { content: "no" } }] }));
  });
  const mismatched = await runProvisionedTrial({
    page: { async screenshot() { return png(); }, mouse: { async click() {} }, async evaluate() { return false; } },
    arm: "screenshot",
    goal: "click",
    env: { ...env, MACLAW_BENCH_MODEL_BASE_URL: `http://127.0.0.1:${mismatch.address().port}/v1` },
    maxCalls: 3,
    contractTest: true,
  });
  await new Promise((resolve) => mismatch.close(resolve));
  assert.equal(mismatched.aborted, "model_mismatch");

  let calls = 0;
  const looping = await serve(async (req, res) => {
    req.resume();
    calls += 1;
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({
      model: "glm-5.3-flash",
      choices: [{ message: { tool_calls: [{ id: `c${calls}`, type: "function", function: { name: "click", arguments: "{\"x\":1,\"y\":2}" } }] } }],
    }));
  });
  const capped = await runProvisionedTrial({
    page: {
      async screenshot() { return png(); },
      mouse: { async click() {} },
      async evaluate() { return false; },
    },
    arm: "screenshot",
    goal: "click",
    env: { ...env, MACLAW_BENCH_MODEL_BASE_URL: `http://127.0.0.1:${looping.address().port}/v1` },
    maxCalls: 2,
    contractTest: true,
  });
  await new Promise((resolve) => looping.close(resolve));
  assert.equal(capped.model_calls, 2);
  assert.equal(capped.stopped, "call_cap");
  assert.equal(capped.contract_test_not_observed_llm, true);
});

test("anthropic wire carries a png and a tool_use click", async () => {
  let pathName = "";
  let apiKey = "";
  let body = "";
  let calls = 0;
  const server = await serve(async (req, res) => {
    pathName = req.url;
    apiKey = req.headers["x-api-key"] || "";
    body = await new Promise((resolve) => {
      const chunks = [];
      req.on("data", (chunk) => chunks.push(chunk));
      req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    });
    calls += 1;
    const message = calls === 1
      ? { content: [{ type: "tool_use", id: "t0", name: "screenshot", input: {} }] }
      : calls === 2
        ? { content: [{ type: "tool_use", id: "t1", name: "click", input: { x: 3, y: 4 } }] }
        : { content: [{ type: "text", text: "stop" }] };
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ model: "glm-5.3-flash", ...message, usage: { input_tokens: 2, output_tokens: 1 } }));
  });
  const clicks = [];
  const trial = await runProvisionedTrial({
    page: {
      async screenshot() { return png(); },
      mouse: { async click(x, y) { clicks.push({ x, y }); } },
      async evaluate() { return null; },
    },
    arm: "screenshot",
    goal: "click",
    env: {
      ...env,
      MACLAW_BENCH_PROTOCOL: "anthropic",
      MACLAW_BENCH_MODEL_NAME: "智谱编程/glm-5.3-flash",
      MACLAW_BENCH_MODEL_BASE_URL: `http://127.0.0.1:${server.address().port}/anthropic`,
    },
    maxCalls: 3,
    contractTest: true,
  });
  await new Promise((resolve) => server.close(resolve));
  assert.equal(pathName.startsWith("/anthropic/v1/messages"), true);
  assert.equal(apiKey, env.MACLAW_BENCH_MODEL_API_KEY);
  assert.match(body, /input_schema/);
  assert.match(body, /"media_type":"image\/png"/);
  assert.deepEqual(clicks, [{ x: 3, y: 4 }]);
  assert.equal(trial.contract_test_not_observed_llm, true);
  assert.equal(trial.trace.some((event) => event.usage && event.usage.prompt_tokens === 2), true);
});

test("semantic arm gives the model snapshot text and clicks the ref it names", async () => {
  const seen = [];
  const clicked = [];
  let step = 0;
  const server = await serve(async (req, res) => {
    const raw = await new Promise((resolve) => {
      const chunks = [];
      req.on("data", (chunk) => chunks.push(chunk));
      req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    });
    seen.push(raw);
    step += 1;
    const message = step === 1
      ? { tool_calls: [{ id: "snap", type: "function", function: { name: "snapshot", arguments: "{}" } }] }
      : { tool_calls: [{ id: "go", type: "function", function: { name: "click", arguments: JSON.stringify({ ref: "r1" }) } }] };
    if (step > 2) message.tool_calls = [{ id: "d", type: "function", function: { name: "done", arguments: "{}" } }];
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ model: "glm-5.3-flash", choices: [{ message }] }));
  });
  const store = new Map([["r1", { type: "button", benchId: "b1", frame: { async evaluate(_fn, benchId) { clicked.push(benchId); return { ok: true }; } } }]]);
  try {
    const trial = await runProvisionedTrial({
      page: { async evaluate() { return null; } },
      arm: "semantic",
      goal: "提交",
      env: { ...env, MACLAW_BENCH_MODEL_BASE_URL: `http://127.0.0.1:${server.address().port}/v1` },
      maxCalls: 4,
      contractTest: true,
      snapshotFn: async () => ({ text: 'r1 button "提交"', store }),
    });
    assert.equal(trial.contract_test_not_observed_llm, true);
    assert.match(seen[1], /r1 button/);
    assert.deepEqual(clicked, ["b1"]);
    assert.equal(seen.join("").includes("data:image/png"), false);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});
