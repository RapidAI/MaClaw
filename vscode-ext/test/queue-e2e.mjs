/**
 * Queue e2e: drives ChatViewProvider against test/fake-queue-agent.mjs over
 * real stdio JSON-RPC and asserts the pre-input queue semantics end to end:
 *
 *   A  FIFO chaining while a turn is busy
 *   B  id-based queueRemove
 *   C  pause-on-error + explicit resume via queueFire
 *   D  fire-while-busy steers into the running turn via session/steer
 *   S  steer rejected by the agent falls back to head-of-queue
 *   E  newSession clears the queue and the stale turn never fires it
 *   F  queue cap notification
 *   G  queueClear empties a busy queue in one shot
 *   H  the bridge never sees concurrent prompts ("session busy")
 *
 * The `vscode` module is stubbed via a require hook; the provider's AcpClient
 * is injected manually (AcpClient.start's args parameter exists for exactly
 * this) so no platform wrapper binary is needed.
 *
 * Run: npm run build && node test/queue-e2e.mjs
 */
import * as path from "path";
import { fileURLToPath } from "url";
import { createRequire } from "module";

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const outDir = path.join(root, "test", "out");
const agentPath = path.join(root, "test", "fake-queue-agent.mjs");

// ---- vscode stub (must be installed before the provider bundle loads) ----

const require = createRequire(import.meta.url);
const outputLines = [];
const infoMessages = [];

const vscodeStub = {
  window: {
    createOutputChannel: () => ({ appendLine: (l) => outputLines.push(l), dispose() {} }),
    showInformationMessage: (m) => (infoMessages.push(m), Promise.resolve(undefined)),
    showWarningMessage: () => Promise.resolve(undefined),
    showErrorMessage: () => Promise.resolve(undefined),
    showQuickPick: () => Promise.resolve(undefined),
  },
  workspace: {
    workspaceFolders: [],
    getConfiguration: () => ({ get: (_key, fallback) => fallback }),
  },
  commands: { executeCommand: () => Promise.resolve() },
  Uri: {
    joinPath: (...parts) => ({ fsPath: parts.map((p) => p?.fsPath ?? String(p)).join("/") }),
    file: (p) => ({ fsPath: p }),
  },
  Disposable: class {
    constructor(fn) {
      this.fn = fn;
    }
    dispose() {
      this.fn?.();
    }
  },
};

const Module = require("module");
const origLoad = Module._load;
Module._load = function (request, ...rest) {
  if (request === "vscode") {
    return vscodeStub;
  }
  return origLoad.call(this, request, ...rest);
};

const { ChatViewProvider } = require(path.join(outDir, "chatViewProvider.cjs"));
const { AcpClient } = require(path.join(outDir, "acpClient.cjs"));

// ---- helpers ---------------------------------------------------------------

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitFor(fn, what, timeout = 8000) {
  const t0 = Date.now();
  while (Date.now() - t0 < timeout) {
    if (fn()) {
      return;
    }
    await sleep(15);
  }
  throw new Error(`timeout waiting for: ${what}`);
}

let failures = 0;
function check(name, cond) {
  if (cond) {
    console.log(`ok   - ${name}`);
  } else {
    failures++;
    console.error(`FAIL - ${name}`);
  }
}

const promptLog = () => outputLines.filter((l) => l.startsWith("PROMPT:"));
const idle = (p) => p.turnActive === false && p.queue.length === 0;

// ---- setup -----------------------------------------------------------------

const provider = new ChatViewProvider({ extensionUri: { fsPath: root } }, "0.0.0-test");

// Inject a client already bound to the fake agent, then re-wire the provider's
// event handlers onto it (the constructor wired the original, never-started
// client). ensureConnected() sees isRunning=true and never spawns a bridge.
const client = new AcpClient();
await client.start(process.execPath, "0.0.0-test", [agentPath]);
provider.client = client;
provider.wireClientEvents();

const send = (text) => provider.handleWebviewMessage({ type: "prompt", text });

// Sidebar snapshot consumers (launcher) see queue changes through this.
let lastSnap;
provider.onStatusDidChange((s) => {
  lastSnap = s;
});

// ---- A: FIFO chaining while busy --------------------------------------------

send("alpha");
send("beta");
send("gamma");
check("A1 two prompts queued behind the live turn", provider.queue.length === 2);
check("A3 status snapshot reports the queued count", lastSnap?.queued === 2);
await waitFor(() => idle(provider), "A: queue drains");
check(
  "A2 fired FIFO: alpha, beta, gamma",
  JSON.stringify(promptLog()) === JSON.stringify(["PROMPT:alpha", "PROMPT:beta", "PROMPT:gamma"])
);

// ---- B: id-based remove ------------------------------------------------------

send("one");
send("two");
send("three");
const twoId = provider.queue.find((q) => q.text === "two")?.id;
provider.handleWebviewMessage({ type: "queueRemove", id: twoId });
check("B1 removed the right item", provider.queue.length === 1 && provider.queue[0].text === "three");
await waitFor(() => idle(provider), "B: queue drains");
check(
  "B2 only one and three fired",
  JSON.stringify(promptLog().slice(-2)) === JSON.stringify(["PROMPT:one", "PROMPT:three"])
);

// ---- C: failure pauses, ▲ resumes ---------------------------------------------

send("FAIL boom");
send("after");
await waitFor(() => provider.turnActive === false, "C: failed turn settles");
await sleep(300); // prove nothing auto-fires after the error
check("C1 queue kept after error", provider.queue.length === 1 && provider.queue[0].text === "after");
check("C2 paused flag set", provider.queuePaused === true);
check("C3 no auto-fire after error", !promptLog().includes("PROMPT:after"));
provider.handleWebviewMessage({ type: "queueFire", id: provider.queue[0].id });
await waitFor(() => idle(provider), "C: resumed queue drains");
check("C4 resume fired the held prompt", promptLog().includes("PROMPT:after"));
check("C5 pause cleared after healthy turn", provider.queuePaused === false);

// ---- D: fire-while-busy steers into the running turn (session/steer) ---------

send("a");
send("b");
send("c");
const cId = provider.queue.find((q) => q.text === "c")?.id;
await provider.handleWebviewMessage({ type: "queueFire", id: cId });
check("D1 steered entry left the queue immediately", provider.queue.length === 1 && provider.queue[0].text === "b");
check(
  "D2 steer RPC delivered to the agent",
  outputLines.filter((l) => l.startsWith("STEER:")).includes("STEER:c")
);
await waitFor(() => idle(provider), "D: queue drains");
check(
  "D3 remaining prompts fired in order",
  JSON.stringify(promptLog().slice(-2)) === JSON.stringify(["PROMPT:a", "PROMPT:b"])
);
check("D4 steered text never became a prompt", !promptLog().includes("PROMPT:c"));

// ---- S: steer rejected falls back to head-of-queue ----------------------------

send("s-main");
send("NOSTEER-x");
send("s-tail");
const nsId = provider.queue.find((q) => q.text === "NOSTEER-x")?.id;
await provider.handleWebviewMessage({ type: "queueFire", id: nsId });
check("S1 rejected steer moved to the front", provider.queue[0]?.text === "NOSTEER-x");
await waitFor(() => idle(provider), "S: queue drains");
check(
  "S2 rejected steer fired as the next prompt",
  promptLog().indexOf("PROMPT:NOSTEER-x") > promptLog().indexOf("PROMPT:s-main") &&
    promptLog().indexOf("PROMPT:NOSTEER-x") < promptLog().indexOf("PROMPT:s-tail")
);

// ---- E: newSession clears queue; stale turn never fires it --------------------

send("s1");
send("s2");
send("s3");
await provider.newSession();
check("E1 newSession cleared the queue", provider.queue.length === 0);
await sleep(350); // the cancelled turn settles in the background
check(
  "E2 stale turn never fired s2/s3",
  !promptLog().includes("PROMPT:s2") && !promptLog().includes("PROMPT:s3")
);

// ---- F: queue cap ------------------------------------------------------------

send("cap-run");
for (let i = 0; i < 55; i++) {
  send(`cap-${i}`);
}
check("F1 queue capped at 50", provider.queue.length === 50);
check("F2 user notified when full", infoMessages.length > 0);
await provider.newSession(); // fast clear so the test doesn't drain 50 turns
await sleep(350);

// ---- G: queueClear empties a busy queue in one shot ----------------------------

send("g1");
send("g2");
send("g3");
check("G1 two queued before clear", provider.queue.length === 2);
provider.handleWebviewMessage({ type: "queueClear" });
check("G2 queueClear emptied the queue", provider.queue.length === 0);
await waitFor(() => idle(provider), "G: running turn settles");
check(
  "G3 cleared items never fired",
  !promptLog().includes("PROMPT:g2") && !promptLog().includes("PROMPT:g3")
);

// ---- H: the bridge never saw concurrent prompts --------------------------------

check("H1 no BUSY rejections from the bridge", !outputLines.some((l) => l.startsWith("BUSY:")));

// ---- teardown ------------------------------------------------------------------

provider.dispose();

if (failures === 0) {
  console.log("\nqueue e2e: all checks passed");
} else {
  console.error(`\nqueue e2e: ${failures} check(s) failed`);
}
process.exit(failures === 0 ? 0 : 1);
