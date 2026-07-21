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
const agentMode = document.getElementById("agent-mode") as HTMLDivElement;
const cancelBtn = document.getElementById("btn-cancel") as HTMLButtonElement;
const fileBtn = document.getElementById("btn-file") as HTMLButtonElement;
const selHint = document.getElementById("sel-hint") as HTMLDivElement;
const providerSelect = document.getElementById("provider-select") as HTMLSelectElement;
const providerHint = document.getElementById("provider-hint") as HTMLDivElement;
const remoteSelect = document.getElementById("remote-select") as HTMLSelectElement;
const remoteHint = document.getElementById("remote-hint") as HTMLDivElement;
const remoteTaskName = document.getElementById("remote-task-name") as HTMLInputElement;
const remoteHost = document.getElementById("remote-host") as HTMLInputElement;
const remoteUser = document.getElementById("remote-user") as HTMLInputElement;
const remotePort = document.getElementById("remote-port") as HTMLInputElement;
const remoteWorkDir = document.getElementById("remote-workdir") as HTMLInputElement;
const createRemoteBtn = document.getElementById("btn-create-remote") as HTMLButtonElement;
const attachBtn = document.getElementById("btn-attach-remote") as HTMLButtonElement;
const detachBtn = document.getElementById("btn-detach-remote") as HTMLButtonElement;
const openTaskBtn = document.getElementById("btn-open-task") as HTMLButtonElement | null;
const listRemoteBtn = document.getElementById("btn-list-remote") as HTMLButtonElement | null;
const refreshPreviewBtn = document.getElementById("btn-refresh-preview") as HTMLButtonElement | null;
const remoteSshBtn = document.getElementById("btn-remote-ssh") as HTMLButtonElement | null;
const diffRemoteBtn = document.getElementById("btn-diff-remote") as HTMLButtonElement | null;
const searchRemoteBtn = document.getElementById("btn-search-remote") as HTMLButtonElement | null;

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
      if (action === "createRemoteTask") {
        vscode.postMessage({
          type: "action",
          action,
          remoteTask: {
            name: remoteTaskName.value,
            host: remoteHost.value,
            user: remoteUser.value,
            port: Number(remotePort.value || "22"),
            workDir: remoteWorkDir.value,
          },
        });
      } else {
        vscode.postMessage({ type: "action", action });
      }
    }
  });
}

providerSelect.addEventListener("change", () => {
  const name = providerSelect.value;
  if (name) {
    vscode.postMessage({ type: "action", action: "setProvider", name });
  }
});

remoteSelect.addEventListener("change", () => {
  vscode.postMessage({
    type: "action",
    action: "selectRemoteTask",
    projectPath: remoteSelect.value,
  });
});

let lastProvidersRender = "";
let lastTurnActive = false;
let lastProvidersState: {
  ok: boolean;
  providers: { name: string; model: string; url: string }[];
  current: string;
} = { ok: true, providers: [], current: "" };

let lastRemoteRender = "";
let creatingRemoteTask = false;
let attachingRemoteTask = false;

function rerenderProviders(): void {
  renderProviders(lastProvidersState.ok, lastProvidersState.providers, lastProvidersState.current);
}

function renderProviders(
  ok: boolean,
  providers: { name: string; model: string; url: string }[],
  current: string
): void {
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

interface RemoteTaskRow {
  name: string;
  project_path: string;
  host: string;
  user: string;
  port: number;
  work_dir: string;
  armed?: boolean;
  needs_reconnect?: boolean;
  message?: string;
}

function formatRemoteTask(t: RemoteTaskRow): string {
  const port = t.port > 0 && t.port !== 22 ? `:${t.port}` : "";
  const target = `${t.user || "?"}@${t.host || "?"}${port}`;
  const name = t.name || t.project_path.split(/[/\\]/).pop() || "task";
  const flags: string[] = [];
  if (t.armed) {
    flags.push("就绪");
  }
  if (t.needs_reconnect) {
    flags.push("需重连");
  }
  const flag = flags.length ? ` · ${flags.join("/")}` : "";
  return `${name} — ${target}${flag}`;
}

function renderRemoteTasks(
  tasks: RemoteTaskRow[],
  selected: string,
  mode: string,
  remoteLabel: string
): void {
  const key = JSON.stringify([tasks, selected, mode, remoteLabel, attachingRemoteTask]);
  if (key === lastRemoteRender) {
    return;
  }
  lastRemoteRender = key;

  remoteSelect.innerHTML = "";
  if (tasks.length === 0) {
    remoteSelect.disabled = true;
    const opt = document.createElement("option");
    opt.value = "";
    opt.textContent = "（无远程任务）";
    remoteSelect.appendChild(opt);
    attachBtn.disabled = true;
    remoteHint.textContent = "在 MaClaw 创建远程编程任务后点「刷新」";
  } else {
    remoteSelect.disabled = false;
    for (const t of tasks) {
      const opt = document.createElement("option");
      opt.value = t.project_path;
      opt.textContent = formatRemoteTask(t);
      remoteSelect.appendChild(opt);
    }
    if (selected && tasks.some((t) => t.project_path === selected)) {
      remoteSelect.value = selected;
    } else {
      remoteSelect.value = tasks[0].project_path;
    }
    attachBtn.disabled = attachingRemoteTask;
    const cur = tasks.find((t) => t.project_path === remoteSelect.value);
    if (cur?.message) {
      remoteHint.textContent = cur.message;
    } else if (cur?.needs_reconnect) {
      remoteHint.textContent = "SSH 会话已失效，附着时将要求输入密码";
    } else if (cur?.armed) {
      remoteHint.textContent = "已就绪 — 可直接附着";
    } else {
      remoteHint.textContent = "附着后聊天将走远程编程 agent（文件在远端）";
    }
  }

  const isRemote = mode === "remote";
  detachBtn.disabled = !isRemote;
  if (openTaskBtn) {
    openTaskBtn.disabled = !isRemote;
  }
  if (listRemoteBtn) {
    listRemoteBtn.disabled = !isRemote;
  }
  if (refreshPreviewBtn) {
    refreshPreviewBtn.disabled = !isRemote;
  }
  if (remoteSshBtn) {
    remoteSshBtn.disabled = !isRemote;
  }
  if (diffRemoteBtn) {
    diffRemoteBtn.disabled = !isRemote;
  }
  if (searchRemoteBtn) {
    searchRemoteBtn.disabled = !isRemote;
  }
  if (isRemote) {
    agentMode.textContent = remoteLabel
      ? `模式：远程编程 · ${remoteLabel}`
      : "模式：远程编程";
    agentMode.classList.add("mode-remote");
  } else {
    agentMode.textContent = "模式：本地工作区";
    agentMode.classList.remove("mode-remote");
  }
}

function renderRemoteTaskAttach(active: boolean): void {
  attachingRemoteTask = active;
  attachBtn.disabled = active || remoteSelect.disabled;
  attachBtn.textContent = active ? "正在附着…" : "附着远程";
  remoteSelect.disabled = active || remoteSelect.options.length === 0 || remoteSelect.value === "";
}

function renderRemoteTaskCreation(active: boolean): void {
  creatingRemoteTask = active;
  createRemoteBtn.disabled = active;
  createRemoteBtn.textContent = active ? "正在创建…" : "创建并附着";
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
      queued: number;
      mode?: string;
      remoteLabel?: string;
    };
    hasSelection?: boolean;
    hasFile?: boolean;
    ok?: boolean;
    providers?: { name: string; model: string; url: string }[];
    current?: string;
    tasks?: RemoteTaskRow[];
    selected?: string;
    mode?: string;
    remoteLabel?: string;
    active?: boolean;
    attaching?: boolean;
  };

  if (msg.type === "status" && msg.snap) {
    const s = msg.snap;
    statusDot.className = `dot dot-${s.state}`;
    statusText.textContent = STATE_LABEL[s.state] ?? s.state;
    if ((s.queued ?? 0) > 0) {
      statusText.textContent += ` · 排队 ${s.queued} 条`;
    }
    statusDetail.textContent =
      s.state === "connected"
        ? s.bridge
        : s.detail !== ""
          ? s.detail
          : s.state === "disconnected"
            ? "在聊天中发送消息即可重连"
            : "";
    sessionCwd.textContent = s.cwd ? `会话目录：${s.cwd}` : "";
    cancelBtn.disabled = !s.turnActive;
    if (s.turnActive !== lastTurnActive) {
      lastTurnActive = s.turnActive;
      lastProvidersRender = "";
      rerenderProviders();
    }
    // Mode line may also arrive via remoteTasks; keep a light fallback.
    if (s.mode === "remote") {
      agentMode.textContent = s.remoteLabel
        ? `模式：远程编程 · ${s.remoteLabel}`
        : "模式：远程编程";
      agentMode.classList.add("mode-remote");
      detachBtn.disabled = false;
    } else if (s.mode === "local") {
      agentMode.textContent = "模式：本地工作区";
      agentMode.classList.remove("mode-remote");
      detachBtn.disabled = true;
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
    return;
  }

  if (msg.type === "remoteTasks") {
    attachingRemoteTask = msg.attaching === true;
    renderRemoteTasks(
      msg.tasks ?? [],
      String(msg.selected ?? ""),
      String(msg.mode ?? "local"),
      String(msg.remoteLabel ?? "")
    );
    return;
  }

  if (msg.type === "remoteTaskCreation") {
    renderRemoteTaskCreation(msg.active === true);
    return;
  }

  if (msg.type === "remoteTaskAttach") {
    renderRemoteTaskAttach(msg.active === true);
  }
});

vscode.postMessage({ type: "ready" });
