/**
 * MaClaw chat webview: renders the ACP conversation (message chunks, thought
 * chunks, tool chips, permission cards) and forwards user input to the
 * extension host. All HTML from agent markdown is sanitized; the CSP in the
 * host page forbids any script without our nonce.
 */
import { marked } from "marked";
import css from "./styles.css";

interface VsCodeApi {
  postMessage(msg: Record<string, unknown>): void;
}

declare function acquireVsCodeApi(): VsCodeApi;

const vscode = acquireVsCodeApi();

marked.setOptions({ gfm: true, breaks: true });

// ---------------------------------------------------------------------------
// DOM scaffolding
// ---------------------------------------------------------------------------

const styleEl = document.createElement("style");
styleEl.textContent = css;
document.head.appendChild(styleEl);

const messagesEl = document.getElementById("messages") as HTMLDivElement;
const statusEl = document.getElementById("status") as HTMLDivElement;
const formEl = document.getElementById("composer") as HTMLFormElement;
const inputEl = document.getElementById("input") as HTMLTextAreaElement;
const sendEl = document.getElementById("send") as HTMLButtonElement;
const cancelEl = document.getElementById("cancel") as HTMLButtonElement;
const newSessionEl = document.getElementById("new-session") as HTMLButtonElement;
const queueEl = document.getElementById("queue") as HTMLDivElement;

// ---------------------------------------------------------------------------
// Rendering state
// ---------------------------------------------------------------------------

/** Current streaming agent bubble; null forces a new bubble on next chunk. */
let agentBubble: { root: HTMLElement; body: HTMLElement; raw: string } | null = null;
/** Current streaming thought block. */
let thoughtBlock: { root: HTMLElement; body: HTMLElement; raw: string } | null = null;
/** Tool chips by toolCallId. */
const toolChips = new Map<string, { root: HTMLElement; status: HTMLElement; output?: HTMLElement }>();
/** Open permission cards by requestId. */
const permCards = new Map<number, HTMLElement>();

let renderTimer: number | undefined;

// ---------------------------------------------------------------------------
// Sanitizer + markdown
// ---------------------------------------------------------------------------

const FORBIDDEN_TAGS = new Set([
  "script", "style", "iframe", "object", "embed", "form", "input", "button",
  "select", "textarea", "link", "meta", "base",
]);

function sanitize(html: string): string {
  const doc = new DOMParser().parseFromString(html, "text/html");
  const walk = (el: Element) => {
    for (const child of [...el.children]) {
      if (FORBIDDEN_TAGS.has(child.tagName.toLowerCase())) {
        child.remove();
        continue;
      }
      for (const attr of [...child.attributes]) {
        const name = attr.name.toLowerCase();
        // Browsers ignore ASCII whitespace/control chars inside URL schemes
        // ("java\tscript:"), so normalize before comparing.
        const normalized = attr.value.replace(/[\s\u0000-\u001f]+/g, "").toLowerCase();
        if (
          name.startsWith("on") ||
          normalized.startsWith("javascript:") ||
          ((name === "href" || name === "src") && normalized.startsWith("data:"))
        ) {
          child.removeAttribute(attr.name);
        }
      }
      walk(child);
    }
  };
  walk(doc.body);
  return doc.body.innerHTML;
}

function renderMarkdownInto(el: HTMLElement, markdown: string): void {
  const html = marked.parse(markdown, { async: false }) as string;
  el.innerHTML = sanitize(html);
  highlightDiffs(el);
}

/** Color unified-diff code blocks line by line. */
function highlightDiffs(root: HTMLElement): void {
  for (const code of root.querySelectorAll("code.language-diff")) {
    const lines = code.innerHTML.split("\n");
    code.innerHTML = lines
      .map((line) => {
        const cls = line.startsWith("+")
          ? "diff-add"
          : line.startsWith("-")
            ? "diff-del"
            : line.startsWith("@")
              ? "diff-hunk"
              : "";
        return cls ? `<span class="${cls}">${line}</span>` : line;
      })
      .join("\n");
  }
}

// ---------------------------------------------------------------------------
// Message renderers
// ---------------------------------------------------------------------------

function scrollToBottom(): void {
  messagesEl.scrollTop = messagesEl.scrollHeight;
}

// Standard chat behavior: auto-scroll only while the user is already near the
// bottom; scrolling up to read earlier output pins the view in place.
let stickToBottom = true;
messagesEl.addEventListener("scroll", () => {
  const gap = messagesEl.scrollHeight - messagesEl.scrollTop - messagesEl.clientHeight;
  stickToBottom = gap < 40;
});

function scrollToBottomIfStuck(): void {
  if (stickToBottom) {
    scrollToBottom();
  }
}

function addUserBubble(text: string): void {
  closeStreamingBlocks();
  const el = document.createElement("div");
  el.className = "msg msg-user";
  el.textContent = text;
  messagesEl.appendChild(el);
  // The user's own message always snaps to the bottom.
  stickToBottom = true;
  scrollToBottom();
}

function closeStreamingBlocks(): void {
  agentBubble = null;
  thoughtBlock = null;
}

function appendAgentChunk(text: string): void {
  if (!agentBubble) {
    const root = document.createElement("div");
    root.className = "msg msg-agent";
    const body = document.createElement("div");
    body.className = "markdown";
    root.appendChild(body);
    messagesEl.appendChild(root);
    agentBubble = { root, body, raw: "" };
    thoughtBlock = null;
  }
  agentBubble.raw += text;
  scheduleMarkdownRender(agentBubble);
  scrollToBottomIfStuck();
}

function appendThoughtChunk(text: string): void {
  if (!thoughtBlock) {
    const root = document.createElement("details");
    root.className = "thought";
    const summary = document.createElement("summary");
    summary.textContent = "Thinking…";
    const body = document.createElement("div");
    body.className = "markdown thought-body";
    root.appendChild(summary);
    root.appendChild(body);
    messagesEl.appendChild(root);
    thoughtBlock = { root, body, raw: "" };
    agentBubble = null;
  }
  thoughtBlock.raw += text;
  scheduleMarkdownRender(thoughtBlock);
  scrollToBottomIfStuck();
}

/** Debounce markdown re-render while chunks stream in. */
const pendingRender = new Set<{ body: HTMLElement; raw: string }>();

function scheduleMarkdownRender(block: { body: HTMLElement; raw: string }): void {
  pendingRender.add(block);
  if (renderTimer !== undefined) {
    return;
  }
  renderTimer = window.setTimeout(() => {
    renderTimer = undefined;
    flushRender();
  }, 60);
}

function flushRender(): void {
  if (renderTimer !== undefined) {
    window.clearTimeout(renderTimer);
    renderTimer = undefined;
  }
  for (const block of pendingRender) {
    renderMarkdownInto(block.body, block.raw);
  }
  pendingRender.clear();
  // Markdown re-render changes the bubble's height — re-scroll after it, or
  // the view is left hanging above the real bottom during streaming.
  scrollToBottomIfStuck();
}

interface ToolUpdate {
  sessionUpdate: string;
  toolCallId?: string;
  title?: string;
  kind?: string;
  status?: string;
  rawInput?: unknown;
  rawOutput?: unknown;
  locations?: { path?: string }[];
  content?: { type?: string; text?: string }[];
}

function renderToolCall(update: ToolUpdate): void {
  closeStreamingBlocks();
  const id = update.toolCallId ?? `tc_${Date.now()}`;
  const root = document.createElement("div");
  root.className = "tool-chip";

  const head = document.createElement("div");
  head.className = "tool-head";

  const status = document.createElement("span");
  status.className = `tool-status status-${update.status ?? "pending"}`;
  status.textContent = statusIcon(update.status);

  const title = document.createElement("span");
  title.className = "tool-title";
  title.textContent = update.title ?? "tool call";
  if (update.kind) {
    title.title = `kind: ${update.kind}`;
  }

  head.appendChild(status);
  head.appendChild(title);
  root.appendChild(head);

  for (const loc of update.locations ?? []) {
    if (!loc.path) {
      continue;
    }
    const link = document.createElement("a");
    link.className = "tool-location";
    link.href = "#";
    link.textContent = loc.path;
    link.addEventListener("click", (ev) => {
      ev.preventDefault();
      vscode.postMessage({ type: "openFile", path: loc.path });
    });
    root.appendChild(link);
  }

  renderToolContent(root, update.content);

  messagesEl.appendChild(root);
  toolChips.set(id, { root, status });
  scrollToBottomIfStuck();
}

function renderToolCallUpdate(update: ToolUpdate): void {
  const id = update.toolCallId ?? "";
  const chip = toolChips.get(id);
  if (chip) {
    chip.status.className = `tool-status status-${update.status ?? "pending"}`;
    chip.status.textContent = statusIcon(update.status);
  }
  const out = typeof update.rawOutput === "string" ? update.rawOutput.trim() : "";
  if (out !== "" && chip) {
    let pre = chip.output;
    if (!pre) {
      pre = document.createElement("pre");
      pre.className = "tool-output";
      chip.root.appendChild(pre);
      chip.output = pre;
    }
    pre.textContent = out;
  }
  if (chip) {
    renderToolContent(chip.root, update.content);
  }
  scrollToBottomIfStuck();
}

function renderToolContent(root: HTMLElement, content?: { type?: string; text?: string }[]): void {
  for (const block of content ?? []) {
    if (block.type === "text" && block.text) {
      const div = document.createElement("div");
      div.className = "markdown tool-content";
      renderMarkdownInto(div, block.text);
      root.appendChild(div);
    }
  }
}

function statusIcon(status?: string): string {
  switch (status) {
    case "completed":
      return "✓";
    case "failed":
      return "✗";
    case "in_progress":
      return "◌";
    default:
      return "•";
  }
}

// ---------------------------------------------------------------------------
// Permission cards
// ---------------------------------------------------------------------------

interface PermissionMsg {
  requestId: number;
  toolCall: { title?: string; kind?: string; locations?: { path?: string }[] };
  options: { optionId: string; name: string; kind?: string }[];
}

function renderPermission(msg: PermissionMsg): void {
  closeStreamingBlocks();
  const card = document.createElement("div");
  card.className = "perm-card";

  const title = document.createElement("div");
  title.className = "perm-title";
  title.textContent = msg.toolCall?.title ?? "Tool permission requested";
  card.appendChild(title);

  for (const loc of msg.toolCall?.locations ?? []) {
    if (!loc.path) {
      continue;
    }
    const pathEl = document.createElement("div");
    pathEl.className = "perm-path";
    pathEl.textContent = loc.path;
    card.appendChild(pathEl);
  }

  const buttons = document.createElement("div");
  buttons.className = "perm-buttons";
  for (const opt of msg.options ?? []) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = `perm-btn perm-${opt.kind ?? opt.optionId}`;
    btn.textContent = opt.name;
    btn.dataset.optionId = opt.optionId;
    btn.addEventListener("click", () => {
      vscode.postMessage({ type: "permissionResponse", requestId: msg.requestId, optionId: opt.optionId });
      resolvePermissionCard(msg.requestId, opt.name);
    });
    buttons.appendChild(btn);
  }
  card.appendChild(buttons);
  messagesEl.appendChild(card);
  permCards.set(msg.requestId, card);
  scrollToBottomIfStuck();
}

function resolvePermissionCard(requestId: number, label: string): void {
  const card = permCards.get(requestId);
  if (!card) {
    return;
  }
  permCards.delete(requestId);
  for (const btn of card.querySelectorAll("button")) {
    btn.disabled = true;
  }
  const resolved = document.createElement("div");
  resolved.className = "perm-resolved";
  resolved.textContent = `→ ${label}`;
  card.appendChild(resolved);
}

// ---------------------------------------------------------------------------
// Status / turn state
// ---------------------------------------------------------------------------

function setStatus(state: string, detail: string): void {
  statusEl.className = `status status-${state}`;
  switch (state) {
    case "connected":
      statusEl.textContent = "MaClaw connected";
      statusEl.title = detail;
      break;
    case "connecting":
      statusEl.textContent = "Connecting to MaClaw…";
      statusEl.title = detail;
      break;
    case "error":
      statusEl.textContent = `⚠ ${detail}`;
      break;
    default:
      statusEl.textContent = "MaClaw disconnected — send a message to reconnect";
  }
}

const PLACEHOLDER_IDLE = "Ask MaClaw… (Enter to send, Shift+Enter for newline)";
const PLACEHOLDER_BUSY = "MaClaw is working… (Enter to queue — queued prompts fire in order)";

function setTurnActive(active: boolean): void {
  // The composer stays enabled while a turn runs: submitting queues the text
  // host-side instead of dropping it (the bridge rejects concurrent prompts).
  sendEl.classList.toggle("loading", active);
  sendEl.title = active ? "Queue message" : "Send";
  inputEl.placeholder = active ? PLACEHOLDER_BUSY : PLACEHOLDER_IDLE;
  cancelEl.hidden = !active;
  // Only re-focus when the user is already looking at the chat — never yank
  // focus out of the editor just because a turn ended.
  if (!active && document.hasFocus()) {
    inputEl.focus();
  }
}

// ---------------------------------------------------------------------------
// Pre-input queue
// ---------------------------------------------------------------------------

interface QueueItem {
  id: number;
  text: string;
}

/** Last queue state pushed by the host — used by the ↑-recall shortcut. */
let currentQueue: QueueItem[] = [];

function renderQueue(items: QueueItem[], paused: boolean): void {
  currentQueue = items;
  queueEl.innerHTML = "";
  queueEl.hidden = items.length === 0;
  if (items.length === 0) {
    return;
  }
  const label = document.createElement("span");
  label.className = paused ? "queue-label queue-paused" : "queue-label";
  label.textContent = paused
    ? `Queued (${items.length}) — paused after error, ▲ to resume`
    : `Queued (${items.length})`;
  queueEl.appendChild(label);
  for (const item of items) {
    const chip = document.createElement("span");
    chip.className = "queue-chip";

    const body = document.createElement("span");
    body.className = "queue-text";
    body.textContent = item.text;
    body.title = `${item.text}\n\nClick to edit`;
    body.addEventListener("click", () => {
      // Pull back into the composer for editing. If the item already fired
      // (raced the turn end), the host no-ops and the text is still safe here.
      vscode.postMessage({ type: "queueRemove", id: item.id });
      // Never clobber an in-progress draft — append on a new line instead.
      inputEl.value = inputEl.value.trim() === "" ? item.text : `${inputEl.value}\n${item.text}`;
      inputEl.focus();
    });
    chip.appendChild(body);

    const fire = document.createElement("button");
    fire.type = "button";
    fire.className = "queue-btn";
    fire.textContent = "▲";
    fire.title = "Fire now (while busy: jump to the front of the queue)";
    fire.addEventListener("click", () => vscode.postMessage({ type: "queueFire", id: item.id }));
    chip.appendChild(fire);

    const del = document.createElement("button");
    del.type = "button";
    del.className = "queue-btn";
    del.textContent = "✕";
    del.title = "Remove from queue";
    del.addEventListener("click", () => vscode.postMessage({ type: "queueRemove", id: item.id }));
    chip.appendChild(del);

    queueEl.appendChild(chip);
  }

  const clear = document.createElement("button");
  clear.type = "button";
  clear.className = "queue-clear";
  clear.textContent = "Clear all";
  clear.title = "Remove all queued prompts";
  clear.addEventListener("click", () => vscode.postMessage({ type: "queueClear" }));
  queueEl.appendChild(clear);
}

// ---------------------------------------------------------------------------
// Event dispatch (also used for transcript replay)
// ---------------------------------------------------------------------------

function handleEvent(msg: { type: string; [key: string]: unknown }): void {
  switch (msg.type) {
    case "userPrompt":
      addUserBubble(String(msg.text ?? ""));
      break;
    case "update": {
      const update = msg.update as ToolUpdate;
      const kind = update?.sessionUpdate;
      if (kind === "agent_message_chunk" || kind === "agent_thought_chunk") {
        // ACP sends content as a SINGLE ContentBlock object (not an array),
        // but tolerate both shapes.
        const c = update.content as
          | { type?: string; text?: string }
          | { type?: string; text?: string }[]
          | undefined;
        const text = Array.isArray(c) ? c[0]?.text ?? "" : c?.text ?? "";
        if (kind === "agent_message_chunk") {
          appendAgentChunk(text);
        } else {
          appendThoughtChunk(text);
        }
      } else if (kind === "tool_call") {
        renderToolCall(update);
      } else if (kind === "tool_call_update") {
        renderToolCallUpdate(update);
      }
      break;
    }
    case "permission":
      renderPermission(msg as unknown as PermissionMsg);
      break;
    case "permissionResolved":
      resolvePermissionCard(Number(msg.requestId), String(msg.optionId ?? ""));
      break;
    case "turnEnd":
      flushRender();
      closeStreamingBlocks();
      break;
    case "turnError": {
      flushRender();
      closeStreamingBlocks();
      const el = document.createElement("div");
      el.className = "msg msg-error";
      el.textContent = String(msg.message ?? "turn failed");
      messagesEl.appendChild(el);
      scrollToBottomIfStuck();
      break;
    }
  }
}

window.addEventListener("message", (event) => {
  const msg = event.data as { type: string; [key: string]: unknown };
  switch (msg.type) {
    case "replay": {
      messagesEl.innerHTML = "";
      toolChips.clear();
      permCards.clear();
      closeStreamingBlocks();
      for (const ev of (msg.events as { type: string }[]) ?? []) {
        handleEvent(ev);
      }
      flushRender();
      break;
    }
    case "reset":
      messagesEl.innerHTML = "";
      toolChips.clear();
      permCards.clear();
      closeStreamingBlocks();
      renderQueue([], false);
      break;
    case "queue":
      renderQueue((msg.items as QueueItem[]) ?? [], Boolean(msg.paused));
      break;
    case "inputRestore":
      // Host refused a queued prompt (queue full) — hand the text back, but
      // never clobber something the user typed in the meantime.
      if (inputEl.value.trim() === "") {
        inputEl.value = String(msg.text ?? "");
        inputEl.focus();
      }
      break;
    case "status":
      setStatus(String(msg.state ?? ""), String(msg.detail ?? ""));
      break;
    case "turnState":
      setTurnActive(Boolean(msg.active));
      break;
    default:
      handleEvent(msg);
  }
});

// ---------------------------------------------------------------------------
// Composer wiring
// ---------------------------------------------------------------------------

formEl.addEventListener("submit", (ev) => {
  ev.preventDefault();
  const text = inputEl.value.trim();
  if (text === "") {
    return;
  }
  inputEl.value = "";
  vscode.postMessage({ type: "prompt", text });
});

inputEl.addEventListener("keydown", (ev) => {
  // isComposing: Enter that confirms an IME candidate must not submit.
  if (ev.key === "Enter" && !ev.shiftKey && !ev.isComposing) {
    ev.preventDefault();
    formEl.requestSubmit();
    return;
  }
  // Empty composer + ↑: pull the newest queued prompt back for editing.
  if (ev.key === "ArrowUp" && inputEl.value.trim() === "" && currentQueue.length > 0) {
    ev.preventDefault();
    const last = currentQueue[currentQueue.length - 1];
    vscode.postMessage({ type: "queueRemove", id: last.id });
    inputEl.value = last.text;
  }
});

cancelEl.addEventListener("click", () => vscode.postMessage({ type: "cancel" }));
newSessionEl.addEventListener("click", () => vscode.postMessage({ type: "newSession" }));

vscode.postMessage({ type: "ready" });
