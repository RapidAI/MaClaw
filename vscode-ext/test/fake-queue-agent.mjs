/**
 * Fake ACP bridge for the prompt-queue e2e (test/queue-e2e.mjs). Speaks NDJSON
 * JSON-RPC on stdin/stdout like maclaw-acp-bridge, with the knobs the queue
 * tests need:
 *   - every session/prompt arrival is logged to stderr as "PROMPT:<text>"
 *   - a second prompt while one is in flight is logged as "BUSY:<text>" and
 *     answered with the real bridge's "session busy" error — the test asserts
 *     this NEVER happens
 *   - texts starting with "FAIL" get an RPC error after the turn delay
 *   - FAKE_TURN_MS env sets the per-turn hold time (default 120ms)
 */
import * as readline from "readline";

const TURN_MS = Number(process.env.FAKE_TURN_MS ?? "120");

const rl = readline.createInterface({ input: process.stdin });
// Parent gone (or test over) → stdin EOF → don't linger as an orphan.
rl.on("close", () => process.exit(0));

const respond = (id, result) =>
  process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id, result }) + "\n");

const respondError = (id, code, message) =>
  process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id, error: { code, message } }) + "\n");

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let inFlight = false;

rl.on("line", async (line) => {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
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
        agentInfo: { name: "fake-queue-agent", version: "0.0.1" },
      });
      break;

    case "session/new":
      respond(msg.id, { sessionId: "sess-queue-1" });
      break;

    case "session/prompt": {
      const text = msg.params?.prompt?.[0]?.text ?? "";
      if (inFlight) {
        console.error(`BUSY:${text}`);
        respondError(msg.id, -32602, "session busy: another prompt is in progress");
        return;
      }
      inFlight = true;
      console.error(`PROMPT:${text}`);
      await sleep(TURN_MS);
      inFlight = false;
      if (text.startsWith("FAIL")) {
        respondError(msg.id, -32603, `boom: ${text}`);
      } else {
        respond(msg.id, { stopReason: "end_turn" });
      }
      break;
    }

    case "session/cancel":
      console.error("CANCEL");
      break;
  }
});
