/** Sidebar control panel: renders live status and forwards actions to the host. */
import css from "./launcher.css";

interface VsCodeApi {
  postMessage(msg: Record<string, unknown>): void;
}

declare function acquireVsCodeApi(): VsCodeApi;

const vscode = acquireVsCodeApi();

const styleEl = document.createElement("style");
styleEl.textContent = css;
document.head.appendChild(styleEl);

const statusDot = document.getElementById("status-dot") as HTMLSpanElement;
const statusText = document.getElementById("status-text") as HTMLSpanElement;
const statusDetail = document.getElementById("status-detail") as HTMLDivElement;
const sessionCwd = document.getElementById("session-cwd") as HTMLDivElement;
const cancelBtn = document.getElementById("btn-cancel") as HTMLButtonElement;
const fileBtn = document.getElementById("btn-file") as HTMLButtonElement;
const selHint = document.getElementById("sel-hint") as HTMLDivElement;
const providerSelect = document.getElementById("provider-select") as HTMLSelectElement;
const providerHint = document.getElementById("provider-hint") as HTMLDivElement;

const STATE_LABEL: Record<string, string> = {
  connected: "已连接",
  connecting: "连接中…",
  disconnected: "未连接",
  error: "连接异常",
};

for (const btn of document.querySelectorAll<HTMLElement>("[data-action]")) {
  btn.addEventListener("click", (ev) => {
    ev.preventDefault();
    const action = (btn as HTMLElement).dataset.action;
    if (action) {
      vscode.postMessage({ type: "action", action });
    }
  });
}

providerSelect.addEventListener("change", () => {
  const name = providerSelect.value;
  if (name) {
    vscode.postMessage({ type: "action", action: "setProvider", name });
  }
});

let lastProvidersRender = "";
let lastTurnActive = false;
let lastProvidersState: {
  ok: boolean;
  providers: { name: string; model: string; url: string }[];
  current: string;
} = { ok: true, providers: [], current: "" };

function rerenderProviders(): void {
  renderProviders(lastProvidersState.ok, lastProvidersState.providers, lastProvidersState.current);
}

function renderProviders(
  ok: boolean,
  providers: { name: string; model: string; url: string }[],
  current: string
): void {
  // Skip no-op re-renders: rebuilding <option>s dismisses an open dropdown.
  const key = JSON.stringify([ok, providers, current, lastTurnActive]);
  if (key === lastProvidersRender) {
    return;
  }
  lastProvidersRender = key;

  providerSelect.innerHTML = "";
  if (!ok) {
    providerSelect.disabled = true;
    providerHint.textContent = "config.json 无法读取或解析";
    return;
  }
  if (providers.length === 0) {
    providerSelect.disabled = true;
    providerHint.textContent = "未找到服务商配置 — 请先在 MaClaw 主程序中添加";
    return;
  }
  providerSelect.disabled = false;
  for (const p of providers) {
    const opt = document.createElement("option");
    opt.value = p.name;
    opt.textContent = p.model ? `${p.name} · ${p.model}` : p.name;
    providerSelect.appendChild(opt);
  }
  providerSelect.value = current;
  const cur = providers.find((p) => p.name === current);
  const suffix = lastTurnActive ? " · 切换将于下一回合生效" : "";
  providerHint.textContent = cur
    ? (cur.url || "与 MaClaw 主程序共用配置，即时生效") + suffix
    : "当前选择不在列表中 — 与 MaClaw 主程序共用配置" + suffix;
}

window.addEventListener("message", (event) => {
  const msg = event.data as {
    type: string;
    snap?: {
      state: string;
      detail: string;
      bridge: string;
      sessionId: string;
      cwd: string;
      turnActive: boolean;
    };
    hasSelection?: boolean;
    hasFile?: boolean;
    ok?: boolean;
    providers?: { name: string; model: string; url: string }[];
    current?: string;
  };

  if (msg.type === "status" && msg.snap) {
    const s = msg.snap;
    statusDot.className = `dot dot-${s.state}`;
    statusText.textContent = STATE_LABEL[s.state] ?? s.state;
    statusDetail.textContent =
      s.state === "connected"
        ? s.bridge
        : s.detail !== ""
          ? s.detail
          : s.state === "disconnected"
            ? "在聊天中发送消息即可重连"
            : "";
    sessionCwd.textContent = s.cwd ? `工作区：${s.cwd}` : "";
    cancelBtn.disabled = !s.turnActive;
    // Provider hint mentions next-turn semantics while a turn is running.
    if (s.turnActive !== lastTurnActive) {
      lastTurnActive = s.turnActive;
      lastProvidersRender = ""; // force hint refresh with the new suffix
      rerenderProviders();
    }
    return;
  }

  if (msg.type === "selection") {
    for (const btn of document.querySelectorAll<HTMLButtonElement>(".btn.sel")) {
      btn.disabled = !msg.hasSelection;
    }
    selHint.textContent = msg.hasSelection ? "已选中代码，可一键提问" : "在编辑器中选中代码后可用";
    fileBtn.disabled = !msg.hasFile;
    return;
  }

  if (msg.type === "providers") {
    lastProvidersState = {
      ok: msg.ok !== false,
      providers: msg.providers ?? [],
      current: String(msg.current ?? ""),
    };
    rerenderProviders();
  }
});

vscode.postMessage({ type: "ready" });
