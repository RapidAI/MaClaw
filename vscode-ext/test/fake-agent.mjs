/**
 * Fake ACP agent for smoke-testing the extension's AcpClient without VS Code
 * or the MaClaw GUI. Speaks NDJSON JSON-RPC on stdin/stdout like
 * maclaw-acp-bridge would (and exercises the reverse permission request).
 */
import * as readline from "readline";

const rl = readline.createInterface({ input: process.stdin });

const respond = (id, result) =>
  process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id, result }) + "\n");

const notify = (method, params) =>
  process.stdout.write(JSON.stringify({ jsonrpc: "2.0", method, params }) + "\n");

const request = (id, method, params) =>
  process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

rl.on("line", async (line) => {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    return;
  }

  // Response to our reverse request (string id — must be echoed verbatim).
  if (msg.id === "perm-900" && msg.result) {
    console.error(`[fake-agent] permission outcome: ${JSON.stringify(msg.result)}`);
    return;
  }

  if (!msg.method) {
    return;
  }

  switch (msg.method) {
    case "initialize":
      respond(msg.id, {
        protocolVersion: 1,
        agentCapabilities: {},
        agentInfo: { name: "fake-agent", version: "0.0.1" },
      });
      break;

    case "session/new":
      respond(msg.id, { sessionId: "sess-1" });
      break;

    case "session/prompt": {
      const { sessionId, prompt } = msg.params;
      const text = prompt?.[0]?.text ?? "";
      notify("session/update", {
        sessionId,
        update: {
          sessionUpdate: "agent_thought_chunk",
          content: { type: "text", text: "thinking about: " },
        },
      });
      await sleep(30);
      notify("session/update", {
        sessionId,
        update: {
          sessionUpdate: "agent_message_chunk",
          content: { type: "text", text: `echo: **${text}**` },
        },
      });
      notify("session/update", {
        sessionId,
        update: {
          sessionUpdate: "tool_call",
          toolCallId: "tc_fake_1",
          title: "bash: ls",
          kind: "execute",
          status: "in_progress",
          locations: [{ path: "C:/tmp/demo.txt" }],
        },
      });
      // Reverse permission request — the client must answer this. Uses a
      // string id to verify the client echoes ids verbatim (ACP allows both).
      request("perm-900", "session/request_permission", {
        sessionId,
        toolCall: { toolCallId: "tc_fake_2", title: "bash: rm -rf x", kind: "execute", status: "pending" },
        options: [
          { optionId: "allow_once", name: "Allow once", kind: "allow_once" },
          { optionId: "reject_once", name: "Reject", kind: "reject_once" },
        ],
      });
      await sleep(200);
      notify("session/update", {
        sessionId,
        update: {
          sessionUpdate: "tool_call_update",
          toolCallId: "tc_fake_1",
          status: "completed",
          rawOutput: "demo.txt",
        },
      });
      respond(msg.id, { stopReason: "end_turn" });
      break;
    }

    case "session/cancel":
      console.error("[fake-agent] cancelled");
      break;
  }
});
