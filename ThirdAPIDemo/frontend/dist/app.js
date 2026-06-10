const state = {
  config: null,
  cursor: "0",
  polling: false,
  pollAbort: null,
  attachments: [],
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
const attachButton = $("attachButton");
const clearAttachmentsButton = $("clearAttachmentsButton");
const attachmentList = $("attachmentList");

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
    appendMessage("assistant", "已连接 MaClaw，可以开始聊天或发送附件。");
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

attachButton.addEventListener("click", async () => {
  if (!state.config) {
    chatStatus.textContent = "请先连接网关";
    return;
  }
  attachButton.disabled = true;
  chatStatus.textContent = "选择文件中...";
  try {
    const selected = await callWails("SelectUploadFiles", state.config);
    state.attachments.push(...(Array.isArray(selected) ? selected : []));
    renderAttachmentList();
    chatStatus.textContent = state.attachments.length ? "附件已准备，可发送" : "";
  } catch (error) {
    chatStatus.textContent = `选择文件失败：${error.message}`;
  } finally {
    attachButton.disabled = false;
    messageInput.focus();
  }
});

clearAttachmentsButton.addEventListener("click", () => {
  state.attachments = [];
  renderAttachmentList();
  messageInput.focus();
});

$("disconnectButton").addEventListener("click", () => {
  stopPolling();
  state.config = null;
  state.cursor = "0";
  state.attachments = [];
  messages.innerHTML = "";
  chatStatus.textContent = "";
  renderAttachmentList();
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
  const attachments = [...state.attachments];
  if ((!text && attachments.length === 0) || !state.config) return;
  const messageType = attachments.length ? attachments[0].type || "file" : "text";
  appendMessage("user", text || `[${messageType}]`, attachments);
  messageInput.value = "";
  state.attachments = [];
  renderAttachmentList();
  sendButton.disabled = true;
  chatStatus.textContent = "发送中...";
  try {
    await callWails("Send", { ...state.config, text, messageType, attachments });
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
  if (msg.type === "tool_call" || msg.type === "tool_plan" || msg.type === "tool_cancel") {
    appendToolMessage(msg);
    return;
  }
  if (msg.progress) {
    appendMessage("progress", msg.text || "处理中...");
    return;
  }
  if (msg.error) {
    appendMessage("assistant", `错误：${msg.error}`);
    return;
  }
  const attachments = outgoingAttachments(msg);
  const text = msg.text || msg.caption || (attachments.length ? `[${msg.type || "attachment"}]` : `[${msg.type || "message"}]`);
  appendMessage("assistant", text, attachments);
}

function appendToolMessage(msg) {
  const item = document.createElement("article");
  item.className = "message assistant";
  const meta = document.createElement("div");
  meta.className = "message-meta";
  meta.textContent = "MaClaw Tool";
  const body = document.createElement("div");
  body.className = "markdown";
  const title = msg.type === "tool_plan" ? "Client tool plan" : msg.type === "tool_cancel" ? "Client tool cancel" : "Client tool call";
  body.innerHTML = renderMarkdown(`**${title}**\n\n\`\`\`json\n${JSON.stringify(msg.toolCall || msg.toolPlan || msg.toolCancel || msg, null, 2)}\n\`\`\``);
  item.append(meta, body);
  if (msg.type === "tool_call" || msg.type === "tool_plan") {
    const actions = document.createElement("div");
    actions.className = "attachment-block";
    const row = document.createElement("div");
    row.className = "attachment-row";
    const label = document.createElement("span");
    label.textContent = msg.type === "tool_plan" ? "Execute allowed plan steps" : "Execute allowed tool";
    const button = document.createElement("button");
    button.type = "button";
    button.className = "inline-button";
    button.textContent = "Execute";
    button.addEventListener("click", async () => {
      button.disabled = true;
      try {
        const result = await callWails("ExecuteToolMessage", { ...state.config, message: msg });
        chatStatus.textContent = result.message || "Tool result submitted";
      } catch (error) {
        chatStatus.textContent = `Tool execution failed: ${error.message}`;
      } finally {
        button.disabled = false;
      }
    });
    row.append(label, button);
    actions.appendChild(row);
    item.appendChild(actions);
  }
  messages.appendChild(item);
  messages.scrollTop = messages.scrollHeight;
}

function outgoingAttachments(msg) {
  const items = Array.isArray(msg.attachments) ? [...msg.attachments] : [];
  if (msg.url || msg.fileName || msg.mimeType || msg.contentType) {
    items.unshift({
      type: msg.type || "file",
      fileName: msg.fileName || "attachment",
      mimeType: msg.mimeType || msg.contentType || "",
      url: msg.url || "",
      sizeBytes: msg.sizeBytes || 0,
      durationMs: msg.durationMs || 0,
    });
  }
  return items.filter((item) => item && (item.url || item.fileName));
}

function appendMessage(role, content, attachments = []) {
  const item = document.createElement("article");
  item.className = `message ${role}`;
  const meta = document.createElement("div");
  meta.className = "message-meta";
  meta.textContent = role === "user" ? "你" : role === "progress" ? "状态" : "MaClaw";
  const body = document.createElement("div");
  body.className = "markdown";
  body.innerHTML = renderMarkdown(content);
  item.append(meta, body);
  if (attachments.length) {
    item.appendChild(renderAttachmentBlock(attachments, role === "assistant"));
  }
  messages.appendChild(item);
  messages.scrollTop = messages.scrollHeight;
}

function renderAttachmentList() {
  if (!state.attachments.length) {
    attachmentList.classList.add("hidden");
    attachmentList.innerHTML = "";
    clearAttachmentsButton.disabled = true;
    return;
  }
  attachmentList.classList.remove("hidden");
  clearAttachmentsButton.disabled = false;
  attachmentList.innerHTML = state.attachments
    .map((item, index) => `<span class="attachment-chip">${escapeHTML(item.type || "file")} · ${escapeHTML(item.fileName || "attachment")} <button type="button" data-remove-attachment="${index}" aria-label="移除附件">×</button></span>`)
    .join("");
  attachmentList.querySelectorAll("[data-remove-attachment]").forEach((button) => {
    button.addEventListener("click", () => {
      state.attachments.splice(Number(button.dataset.removeAttachment), 1);
      renderAttachmentList();
    });
  });
}

function renderAttachmentBlock(attachments, downloadable) {
  const box = document.createElement("div");
  box.className = "attachment-block";
  attachments.forEach((att) => {
    const row = document.createElement("div");
    row.className = "attachment-row";
    const label = document.createElement("span");
    label.textContent = `${att.type || "file"} · ${att.fileName || "attachment"}${att.sizeBytes ? ` · ${formatBytes(att.sizeBytes)}` : ""}`;
    row.appendChild(label);
    if (att.url) {
      const open = document.createElement("a");
      open.href = att.url;
      open.target = "_blank";
      open.rel = "noreferrer";
      open.textContent = "打开";
      row.appendChild(open);
    }
    if (downloadable && att.url) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "inline-button";
      button.textContent = "下载";
      button.addEventListener("click", async () => {
        button.disabled = true;
        try {
          const result = await callWails("Download", { ...state.config, url: att.url, fileName: att.fileName || "attachment" });
          chatStatus.textContent = `已保存：${result.path}`;
        } catch (error) {
          chatStatus.textContent = `下载失败：${error.message}`;
        } finally {
          button.disabled = false;
        }
      });
      row.appendChild(button);
    }
    box.appendChild(row);
  });
  return box;
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

function formatBytes(value) {
  const size = Number(value || 0);
  if (!Number.isFinite(size) || size <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let n = size;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
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
