import assert from "node:assert/strict";
import test from "node:test";
import { agentPrerequisite, chatCompletionsURL, providerURL, redactSecrets } from "./agent-gate.mjs";

const ready = {
  MACLAW_BENCH_MODEL_BASE_URL: "https://open.bigmodel.cn/api/paas/v4",
  MACLAW_BENCH_MODEL_API_KEY: "sk-bench-example-not-real",
  MACLAW_BENCH_MODEL_NAME: "glm-5.3-flash",
  MACLAW_BENCH_MODEL_VISION: "1",
  MACLAW_BENCH_CONFIRM: "1",
};

test("official direct gate accepts the pinned model and hides the key", () => {
  const gate = agentPrerequisite(ready);
  assert.equal(gate.ok, true);
  assert.equal(gate.model_name, "glm-5.3-flash");
  assert.equal(gate.key_present, true);
  assert.equal(JSON.stringify(gate).includes("sk-bench"), false);
});

test("free and coding routes are not the official direct model", () => {
  assert.equal(agentPrerequisite({ ...ready, MACLAW_BENCH_MODEL_NAME: "opencode/glm-5.3-flash-free" }).ok, false);
  assert.equal(agentPrerequisite({ ...ready, MACLAW_BENCH_MODEL_NAME: "glm-5.3" }).ok, false);
  assert.equal(
    agentPrerequisite({ ...ready, MACLAW_BENCH_MODEL_BASE_URL: "https://open.bigmodel.cn/api/coding/paas/v4" }).ok,
    false,
  );
});

test("production config path is refused", () => {
  const gate = agentPrerequisite({ ...ready, MACLAW_BENCH_CONFIG_PATH: "/tmp/app.json" });
  assert.equal(gate.ok, false);
});

test("loopback proxy may omit the provider key", () => {
  const gate = agentPrerequisite({
    MACLAW_BENCH_LOOPBACK: "1",
    MACLAW_BENCH_MODEL_BASE_URL: "http://127.0.0.1:9/v1",
    MACLAW_BENCH_MODEL_NAME: "glm-5.3-flash",
    MACLAW_BENCH_MODEL_VISION: "1",
    MACLAW_BENCH_CONFIRM: "1",
  });
  assert.equal(gate.ok, true);
  assert.equal(gate.key_present, false);
});

test("official base URL keeps the paas path", () => {
  assert.equal(
    chatCompletionsURL("https://open.bigmodel.cn/api/paas/v4").href,
    "https://open.bigmodel.cn/api/paas/v4/chat/completions",
  );
});

test("Bot1 anthropic endpoint and member name are accepted", () => {
  const gate = agentPrerequisite({
    ...ready,
    MACLAW_BENCH_MODEL_BASE_URL: "https://open.bigmodel.cn/api/anthropic",
    MACLAW_BENCH_MODEL_NAME: "智谱编程/glm-5.3-flash",
    MACLAW_BENCH_PROTOCOL: "anthropic",
  });
  delete gate.problems;
  assert.equal(gate.ok, true);
  assert.equal(gate.protocol, "anthropic");
  assert.equal(gate.vision_assumed, false);
});

test("redaction removes bearer keys from traces", () => {
  const out = redactSecrets("HTTP 401 Bearer sk-bench-example-not-real", ["sk-bench-example-not-real"]);
  assert.equal(out.includes("sk-bench"), false);
  assert.match(out, /\[redacted\]/);
});
