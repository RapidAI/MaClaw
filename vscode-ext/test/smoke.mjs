/**
 * Protocol smoke test: AcpClient (real code, bundled from src/acpClient.ts)
 * against a fake ACP agent. Verifies handshake, session, streaming updates,
 * tool events, the reverse permission request, and cancel.
 *
 * Run: npm run build && npm run smoke
 */
import assert from "node:assert/strict";
import * as path from "path";
import { fileURLToPath } from "url";
import { AcpClient } from "./out/acpClient.cjs";

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const fakeAgent = path.join(root, "test", "fake-agent.mjs");

const client = new AcpClient();
const updates = [];
let permission = null;
client.on("update", (params) => updates.push(params.update));
client.on("permission", (perm) => {
  permission = perm;
  client.resolvePermission(perm.rpcId, "allow_once");
});

let failed = false;
try {
  const init = await client.start(process.execPath, "0.0.0-smoke", [fakeAgent]);
  assert.equal(init.protocolVersion, 1, "protocolVersion");
  assert.equal(init.agentInfo.name, "fake-agent", "agentInfo.name");

  const sessionId = await client.newSession("C:/tmp");
  assert.equal(sessionId, "sess-1", "sessionId");

  const res = await client.prompt(sessionId, "hello maclaw");
  assert.equal(res.stopReason, "end_turn", "stopReason");

  client.cancel(sessionId);
  await new Promise((r) => setTimeout(r, 100));

  const kinds = updates.map((u) => u.sessionUpdate);
  assert.ok(kinds.includes("agent_thought_chunk"), "thought chunk received");
  assert.ok(kinds.includes("agent_message_chunk"), "message chunk received");
  assert.ok(kinds.includes("tool_call"), "tool_call received");
  assert.ok(kinds.includes("tool_call_update"), "tool_call_update received");

  const echo = updates.find((u) => u.sessionUpdate === "agent_message_chunk");
  assert.match(echo.content.text, /hello maclaw/, "echo content");

  const toolCall = updates.find((u) => u.sessionUpdate === "tool_call");
  assert.equal(toolCall.toolCallId, "tc_fake_1");
  assert.equal(toolCall.locations[0].path, "C:/tmp/demo.txt");

  assert.ok(permission, "reverse permission request received");
  assert.equal(permission.rpcId, "perm-900", "string rpc id echoed verbatim");
  assert.equal(permission.params.options.length, 2);
  assert.equal(permission.params.toolCall.title, "bash: rm -rf x");
} catch (err) {
  failed = true;
  console.error("[smoke] FAIL:", err);
} finally {
  client.stop();
}

if (failed) {
  process.exit(1);
}

// Spawn failure: start() must reject and isRunning must be false (regression:
// a failed spawn previously left the client stuck "running" on a dead pipe).
const bad = new AcpClient();
try {
  await bad.start("C:/nonexistent/maclaw-acp-bridge.exe", "0.0.0-smoke");
  console.error("[smoke] FAIL: start() should have rejected for a missing binary");
  process.exit(1);
} catch {
  /* expected */
}
assert.equal(bad.isRunning, false, "isRunning false after spawn failure");
bad.stop();

console.log("[smoke] OK — handshake, session, updates, permission, cancel all verified");
