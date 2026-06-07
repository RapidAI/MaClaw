const state = {
  config: null,
  cursor: "0",
  polling: false,
  pollAbort: null,
};

const $ = (id) => document.getElementById(id);

const connectView = $("connectView");
const chatView = $("chatView");
const connectForm = $("connectForm");
const chatForm = $("chatForm");
const messages = $("messages");
const messageInput = $("messageInput");
const connectButton = $("connectButton");
const sendButton = $("sendButton");
const connectError = $("connectError");
const chatStatus = $("chatStatus");

const wailsApp = () => window.go?.main?.App;

connectForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  connectError.textContent = "";
  connectButton.disabled = true;
  connectButton.textContent = "连接中...";
  try {
    const payload = readConfigFromForm();
    const res = await callWails("Connect", payload);
    state.config = res.config;
    state.cursor = res.cursor || "0";
    showChat();
    appendMessage("assistant", "已连接 MaClaw。可以开始聊天。");
    startPolling();
  } catch (error) {
    connectError.textContent = error.message;
  } finally {
    connectButton.disabled = false;
    connectButton.textContent = "连接";
  }
});

chatForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  await sendCurrentMessage();
});

messageInput.addEventListener("keydown", async (event) => {
  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    await sendCurrentMessage();
  }
});

$("disconnectButton").addEventListener("click", () => {
  stopPolling();
  state.config = null;
  state.cursor = "0";
  messages.innerHTML = "";
  chatStatus.textContent = "";
  chatView.classList.add("hidden");
  connectView.classList.remove("hidden");
});

function readConfigFromForm() {
  return {
    baseUrl: $("baseUrl").value.trim(),
    apiKey: $("apiKey").value.trim(),
    clientId: $("clientId").value.trim(),
    conversationId: $("conversationId").value.trim(),
    userId: $("userId").value.trim(),
    userName: $("userName").value.trim(),
  };
}

function showChat() {
  connectView.classList.add("hidden");
  chatView.classList.remove("hidden");
  $("connectionMeta").textContent = `${state.config.baseUrl} / ${state.config.clientId} / ${state.config.conversationId}`;
  messageInput.focus();
}

async function sendCurrentMessage() {
  const text = messageInput.value.trim();
  if (!text || !state.config) return;
  appendMessage("user", text);
  messageInput.value = "";
  sendButton.disabled = true;
  chatStatus.textContent = "发送中...";
  try {
    await callWails("Send", { ...state.config, text });
    chatStatus.textContent = "已发送，等待回复...";
    if (!state.polling) startPolling();
  } catch (error) {
    appendMessage("assistant", `发送失败：${error.message}`);
    chatStatus.textContent = "";
  } finally {
    sendButton.disabled = false;
    messageInput.focus();
  }
}

function startPolling() {
  if (state.polling || !state.config) return;
  state.polling = true;
  pollLoop();
}

function stopPolling() {
  state.polling = false;
  if (state.pollAbort) {
    state.pollAbort.abort();
    state.pollAbort = null;
  }
}

async function pollLoop() {
  while (state.polling && state.config) {
    const controller = new AbortController();
    state.pollAbort = controller;
    try {
      const data = await callWails("Poll", {
        ...state.config,
        cursor: state.cursor,
        timeout: 25,
        limit: 20,
      });
      state.cursor = data.nextCursor || state.cursor;
      const returned = Array.isArray(data.messages) ? data.messages : [];
      for (const msg of returned) {
        renderOutgoingMessage(msg);
      }
      chatStatus.textContent = state.polling ? "已连接" : "";
    } catch (error) {
      if (state.polling) {
        chatStatus.textContent = `轮询失败：${error.message}，3 秒后重试`;
        await sleep(3000);
      }
    } finally {
      state.pollAbort = null;
    }
  }
}

function renderOutgoingMessage(msg) {
  if (!msg) return;
  if (msg.progress) {
    appendMessage("progress", msg.text || "处理中...");
    return;
  }
  if (msg.error) {
    appendMessage("assistant", `错误：${msg.error}`);
    return;
  }
  const text = msg.text || msg.caption || `[${msg.type || "message"}]`;
  appendMessage("assistant", text);
}

function appendMessage(role, content) {
  const item = document.createElement("article");
  item.className = `message ${role}`;
  const meta = document.createElement("div");
  meta.className = "message-meta";
  meta.textContent = role === "user" ? "你" : role === "progress" ? "状态" : "MaClaw";
  const body = document.createElement("div");
  body.className = "markdown";
  body.innerHTML = renderMarkdown(content);
  item.append(meta, body);
  messages.appendChild(item);
  messages.scrollTop = messages.scrollHeight;
}

async function callWails(method, payload) {
  const bridge = wailsApp();
  if (!bridge || typeof bridge[method] !== "function") {
    throw new Error("Wails bridge is unavailable. Use `wails dev` or a packaged build.");
  }
  return bridge[method](payload);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function renderMarkdown(input) {
  const blocks = [];
  const source = String(input || "").replace(/\r\n/g, "\n");
  let cursor = 0;
  const codePattern = /```([a-zA-Z0-9_-]*)\n([\s\S]*?)```/g;
  let match;
  while ((match = codePattern.exec(source)) !== null) {
    blocks.push(renderMarkdownBlocks(source.slice(cursor, match.index)));
    blocks.push(`<pre><code>${escapeHTML(match[2])}</code></pre>`);
    cursor = match.index + match[0].length;
  }
  blocks.push(renderMarkdownBlocks(source.slice(cursor)));
  return blocks.join("");
}

function renderMarkdownBlocks(markdown) {
  const lines = markdown.split("\n");
  const html = [];
  let paragraph = [];
  let list = null;
  let quote = [];
  let table = [];

  const flushParagraph = () => {
    if (paragraph.length) {
      html.push(`<p>${renderInline(paragraph.join(" "))}</p>`);
      paragraph = [];
    }
  };
  const flushList = () => {
    if (list) {
      html.push(`<${list.type}>${list.items.map((item) => `<li>${renderInline(item)}</li>`).join("")}</${list.type}>`);
      list = null;
    }
  };
  const flushQuote = () => {
    if (quote.length) {
      html.push(`<blockquote>${quote.map((item) => `<p>${renderInline(item)}</p>`).join("")}</blockquote>`);
      quote = [];
    }
  };
  const flushTable = () => {
    if (table.length >= 2 && /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(table[1])) {
      const rows = table.map(splitTableRow);
      const header = rows[0].map((cell) => `<th>${renderInline(cell)}</th>`).join("");
      const body = rows.slice(2).map((row) => `<tr>${row.map((cell) => `<td>${renderInline(cell)}</td>`).join("")}</tr>`).join("");
      html.push(`<table><thead><tr>${header}</tr></thead><tbody>${body}</tbody></table>`);
    } else {
      for (const line of table) paragraph.push(line);
      flushParagraph();
    }
    table = [];
  };
  const flushAll = () => {
    flushTable();
    flushParagraph();
    flushList();
    flushQuote();
  };

  for (const rawLine of lines) {
    const line = rawLine.trimEnd();
    if (!line.trim()) {
      flushAll();
      continue;
    }
    if (line.includes("|")) {
      flushParagraph();
      flushList();
      flushQuote();
      table.push(line);
      continue;
    }
    flushTable();
    const heading = /^(#{1,3})\s+(.+)$/.exec(line);
    if (heading) {
      flushParagraph();
      flushList();
      flushQuote();
      html.push(`<h${heading[1].length}>${renderInline(heading[2])}</h${heading[1].length}>`);
      continue;
    }
    const bullet = /^\s*[-*]\s+(.+)$/.exec(line);
    const ordered = /^\s*\d+\.\s+(.+)$/.exec(line);
    if (bullet || ordered) {
      flushParagraph();
      flushQuote();
      const type = bullet ? "ul" : "ol";
      if (!list || list.type !== type) flushList();
      if (!list) list = { type, items: [] };
      list.items.push(bullet ? bullet[1] : ordered[1]);
      continue;
    }
    const quoted = /^\s*>\s?(.+)$/.exec(line);
    if (quoted) {
      flushParagraph();
      flushList();
      quote.push(quoted[1]);
      continue;
    }
    flushList();
    flushQuote();
    paragraph.push(line);
  }
  flushAll();
  return html.join("");
}

function splitTableRow(line) {
  return line.replace(/^\s*\|/, "").replace(/\|\s*$/, "").split("|").map((cell) => cell.trim());
}

function renderInline(text) {
  let safe = escapeHTML(text);
  safe = safe.replace(/`([^`]+)`/g, "<code>$1</code>");
  safe = safe.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  safe = safe.replace(/\*([^*]+)\*/g, "<em>$1</em>");
  safe = safe.replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>');
  return safe;
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}
