import * as vscode from "vscode";
import { ChatViewProvider, StatusSnapshot } from "./chatViewProvider";
import { RemoteCodingTask } from "./acpClient";
import { readMaclawLLMConfig, watchMaclawConfig, writeCurrentProvider } from "./maclawConfig";

/**
 * Sidebar (activity bar) control panel for MaClaw: live connection status,
 * session actions, LLM provider switching (shared with the MaClaw GUI),
 * remote coding task attach, and selection/file shortcuts into the chat.
 */
export class LauncherViewProvider implements vscode.WebviewViewProvider, vscode.Disposable {
  public static readonly viewType = "maclaw-acp.launcher";

  private view?: vscode.WebviewView;
  private readonly disposables: vscode.Disposable[] = [];
  private configWatcher?: { close(): void };
  private remoteTasks: RemoteCodingTask[] = [];
  private selectedRemotePath = "";
  private creatingRemoteTask = false;
  private attachingRemoteTask = false;
  private refreshingRemoteTasks?: Promise<void>;

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly chat: ChatViewProvider
  ) {
    this.disposables.push(
      this.chat.onStatusDidChange((s) => this.postStatus(s)),
      vscode.window.onDidChangeTextEditorSelection(() => this.postSelection()),
      vscode.window.onDidChangeActiveTextEditor(() => this.postSelection())
    );
    this.configWatcher = watchMaclawConfig(() => this.postProviders());
  }

  dispose(): void {
    this.configWatcher?.close();
    for (const d of this.disposables) {
      d.dispose();
    }
  }

  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view;
    this.lastSelectionPost = "";
    view.webview.options = {
      enableScripts: true,
      localResourceRoots: [vscode.Uri.joinPath(this.context.extensionUri, "dist")],
    };
    view.webview.html = this.renderHtml(view.webview);
    view.webview.onDidReceiveMessage((msg) => void this.handleMessage(msg));
    view.onDidDispose(() => {
      this.view = undefined;
    });
  }

  private async handleMessage(msg: {
    type: string;
    action?: string;
    name?: string;
    projectPath?: string;
    remoteTask?: {
      name?: string;
      host?: string;
      user?: string;
      port?: number;
      workDir?: string;
    };
  }): Promise<void> {
    if (msg.type === "ready") {
      this.postStatus(this.chat.getStatusSnapshot());
      this.postSelection();
      this.postProviders();
      this.postRemoteTasks();
      return;
    }
    if (msg.type !== "action") {
      return;
    }
    switch (msg.action) {
      case "openChat":
        await vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`);
        return;
      case "newSession":
        await this.chat.newSession();
        await vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`);
        return;
      case "cancel":
        this.chat.cancelTurn();
        return;
      case "reconnect":
        await this.chat.reconnect();
        return;
      case "openSettings":
        await vscode.commands.executeCommand("workbench.action.openSettings", "maclaw-acp");
        return;
      case "setProvider":
        this.setProvider(msg.name);
        return;
      case "explain":
      case "fix":
      case "test":
        await this.sendSelectionPrompt(msg.action);
        return;
      case "summarizeFile":
        await this.sendFilePrompt();
        return;
      case "refreshRemoteTasks":
        await this.refreshRemoteTasks();
        return;
      case "createRemoteTask":
        await this.createRemoteTask(msg.remoteTask);
        return;
      case "selectRemoteTask":
        this.selectedRemotePath = (msg.projectPath ?? "").trim();
        this.postRemoteTasks();
        return;
      case "attachRemoteTask":
        await this.attachSelectedRemote();
        return;
      case "detachRemoteTask":
        await this.detachRemote();
        return;
      case "openTaskFolder":
        await this.chat.openAttachedTaskFolder();
        return;
      case "listRemoteDir":
        await this.chat.openRemoteDirListing();
        return;
      case "refreshRemotePreviews":
        {
          const n = this.chat.refreshOpenRemotePreviews();
          void vscode.window.showInformationMessage(
            n > 0 ? `MaClaw: 已刷新 ${n} 个远端预览` : "MaClaw: 没有打开的远端预览"
          );
        }
        return;
      case "openRemoteSSH":
        await this.chat.openInRemoteSSH();
        return;
      case "diffRemoteLocal":
        await this.chat.diffRemoteWithLocal();
        return;
      case "searchRemote":
        await this.chat.searchRemoteAndOpen();
        return;
    }
  }

  // ---- remote coding attach -----------------------------------------------

  /** Public entry for commands / extension activation. */
  async refreshRemoteTasks(opts?: { quiet?: boolean }): Promise<void> {
    if (this.refreshingRemoteTasks) {
      return this.refreshingRemoteTasks;
    }
    this.refreshingRemoteTasks = this.refreshRemoteTasksImpl(opts);
    try {
      await this.refreshingRemoteTasks;
    } finally {
      this.refreshingRemoteTasks = undefined;
    }
  }

  private async refreshRemoteTasksImpl(opts?: { quiet?: boolean }): Promise<void> {
    const quiet = opts?.quiet === true;
    try {
      const connected = await this.ensureBridge();
      if (!connected) {
        if (!quiet) {
          void vscode.window.showWarningMessage(
            "MaClaw: 未连接到 GUI。请先启动 MaClaw 主程序，再刷新远程任务。"
          );
        }
        // A transient bridge outage must not erase the picker while a remote
        // task is still attached; users need that cached row to retry once the
        // GUI comes back. Clear only the non-attached stale list.
        const attached = this.chat.getStatusSnapshot().mode === "remote" ? this.chat.getStatusSnapshot().cwd : "";
        if (attached) {
          const cached = this.remoteTasks.find((t) => t.project_path === attached);
          this.remoteTasks = cached ? [cached] : [];
          this.selectedRemotePath = attached;
        } else {
          this.remoteTasks = [];
          this.selectedRemotePath = "";
        }
        this.postRemoteTasks();
        return;
      }
      const previousTasks = this.remoteTasks;
      // The GUI caps this RPC at 100. Request that full bounded set so an older
      // remote task is not hidden merely because it falls outside the first 40.
      this.remoteTasks = await this.chat.acpClient.listRemoteCodingTasks(100);
      // Prefer currently attached task if still listed.
      const attached = this.chat.getStatusSnapshot().mode === "remote" ? this.chat.getStatusSnapshot().cwd : "";
      // The GUI task index can lag briefly after creating or restoring a task.
      // Never make an already-attached remote target disappear from the picker
      // just because that one list response is stale.
      if (attached && !this.remoteTasks.some((t) => t.project_path === attached)) {
        const cached = previousTasks.find((t) => t.project_path === attached);
        if (cached) {
          this.remoteTasks = [cached, ...this.remoteTasks];
        }
      }
      if (attached && this.remoteTasks.some((t) => t.project_path === attached)) {
        this.selectedRemotePath = attached;
      }
      if (
        this.selectedRemotePath &&
        !this.remoteTasks.some((t) => t.project_path === this.selectedRemotePath)
      ) {
        this.selectedRemotePath = "";
      }
      if (!this.selectedRemotePath && this.remoteTasks.length > 0) {
        this.selectedRemotePath = this.remoteTasks[0].project_path;
      }
      this.postRemoteTasks();
      if (!quiet && this.remoteTasks.length === 0) {
        void vscode.window.showInformationMessage(
          "MaClaw: 没有远程编程任务。请先在 MaClaw 中创建 remote_coding_dev 任务。"
        );
      }
    } catch (err) {
      if (!quiet) {
        void vscode.window.showErrorMessage(
          `MaClaw: 加载远程任务失败 — ${err instanceof Error ? err.message : String(err)}`
        );
      }
    }
  }

  private async ensureBridge(): Promise<boolean> {
    await this.chat.reconnect();
    return this.chat.acpClient.isRunning;
  }

  private selectedTask(): RemoteCodingTask | undefined {
    return this.remoteTasks.find((t) => t.project_path === this.selectedRemotePath);
  }

  private remoteLabelFor(task: RemoteCodingTask): string {
    const port = task.port > 0 && task.port !== 22 ? `:${task.port}` : "";
    const host = task.host || "?";
    const user = task.user || "?";
    const wd = task.work_dir || "?";
    return `${user}@${host}${port}:${wd}`;
  }

  private async createRemoteTask(input?: {
    name?: string;
    host?: string;
    user?: string;
    port?: number;
    workDir?: string;
  }): Promise<void> {
    if (this.creatingRemoteTask) {
      return;
    }
    const name = input?.name?.trim() ?? "";
    const host = input?.host?.trim() ?? "";
    const user = input?.user?.trim() ?? "";
    const workDir = input?.workDir?.trim() ?? "";
    const port = Number(input?.port ?? 22);
    if (!name || !host || !user || !workDir) {
      void vscode.window.showWarningMessage("MaClaw: 请填写任务名、SSH 主机、用户名和远程工作目录。");
      return;
    }
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      void vscode.window.showWarningMessage("MaClaw: SSH 端口必须在 1 到 65535 之间。");
      return;
    }
    try {
      this.creatingRemoteTask = true;
      this.postRemoteTaskCreationState();
      if (!(await this.ensureBridge())) {
        void vscode.window.showWarningMessage("MaClaw: 未连接到 GUI，无法创建远程任务。");
        return;
      }
      const created = await this.chat.acpClient.createRemoteCodingTask({
        name,
        sshHost: host,
        sshUser: user,
        workDir,
        sshPort: port,
      });
      const task = created.task;
      await this.refreshRemoteTasks({ quiet: true });
      this.selectedRemotePath = task.project_path;
      const selected = this.selectedTask() ?? task;
      this.upsertRemoteTask(task);
      this.postRemoteTasks();
      await this.attachRemoteTask(selected);
      if (this.chat.getStatusSnapshot().mode !== "remote") {
        void vscode.window.showInformationMessage(
          created.reused
            ? "MaClaw: 已复用已有远程任务。可稍后在列表中选择它并点击“附着远程”。"
            : "MaClaw: 远程任务已创建。可稍后在列表中选择它并点击“附着远程”。"
        );
      }
    } catch (err) {
      void vscode.window.showErrorMessage(
        `MaClaw: 创建远程编程任务失败 — ${err instanceof Error ? err.message : String(err)}`
      );
    } finally {
      this.creatingRemoteTask = false;
      this.postRemoteTaskCreationState();
    }
  }

  private postRemoteTaskCreationState(): void {
    void this.view?.webview
      .postMessage({ type: "remoteTaskCreation", active: this.creatingRemoteTask })
      .then(undefined, () => {});
  }

  private postRemoteTaskAttachState(): void {
    void this.view?.webview
      .postMessage({ type: "remoteTaskAttach", active: this.attachingRemoteTask })
      .then(undefined, () => {});
  }

  private async attachSelectedRemote(): Promise<void> {
    const task = this.selectedTask();
    if (!task) {
      void vscode.window.showInformationMessage("MaClaw: 请先选择一个远程编程任务。");
      return;
    }
    await this.attachRemoteTask(task);
  }

  private async attachRemoteTask(task: RemoteCodingTask): Promise<void> {
    if (this.attachingRemoteTask) {
      void vscode.window.showInformationMessage("MaClaw: 正在附着远程任务，请完成当前 SSH 连接。");
      return;
    }
    try {
      this.attachingRemoteTask = true;
      this.postRemoteTaskAttachState();
      const connected = await this.ensureBridge();
      if (!connected) {
        void vscode.window.showWarningMessage("MaClaw: 未连接，无法附着远程任务。");
        return;
      }

      // Prefer re-arming sticky session (no password) when SSH is still alive.
      let res = await this.chat.acpClient.ensureCodingWorkbenchArmed(task.project_path);
      let st = res.status;
      if (st?.needs_reconnect || !st?.armed) {
        const password = await vscode.window.showInputBox({
          title: `SSH 密码 — ${this.remoteLabelFor(task)}`,
          prompt: "密码仅用于本次连接，不会写入配置或任务记录",
          password: true,
          ignoreFocusOut: true,
        });
        if (password === undefined) {
          return; // cancelled
        }
        if (password.trim() === "") {
          void vscode.window.showWarningMessage("MaClaw: 密码不能为空。");
          return;
        }
        res = await this.chat.acpClient.prepareRemoteCoding({
          projectPath: task.project_path,
          sshHost: task.host,
          sshUser: task.user,
          sshPassword: password,
          workDir: task.work_dir,
          sshPort: task.port,
        });
        st = res.status;
      }

      if (!st?.armed) {
        void vscode.window.showWarningMessage(
          `MaClaw: 远程任务未能就绪 — ${st?.message || res.status?.message || "unknown"}`
        );
        return;
      }

      await this.chat.attachRemoteTask({
        projectPath: task.project_path,
        remoteLabel: this.remoteLabelFor(task),
        workDir: task.work_dir,
        host: task.host,
        user: task.user,
        port: task.port,
      });
      // Refresh list flags (armed etc.)
      try {
        this.remoteTasks = await this.chat.acpClient.listRemoteCodingTasks(100);
        this.upsertRemoteTask(task);
        this.selectedRemotePath = task.project_path;
        this.postRemoteTasks();
      } catch {
        /* ignore */
      }
      await vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`);
      const openFolder = "打开本地任务目录";
      const choice = await vscode.window.showInformationMessage(
        `MaClaw: 已附着远程编程 — ${this.remoteLabelFor(task)}（改动在远端；聊天路径以 remote: 标记）`,
        openFolder
      );
      if (choice === openFolder) {
        await this.chat.openAttachedTaskFolder();
      }
    } catch (err) {
      void vscode.window.showErrorMessage(
        `MaClaw: 附着远程任务失败 — ${err instanceof Error ? err.message : String(err)}`
      );
    } finally {
      this.attachingRemoteTask = false;
      this.postRemoteTaskAttachState();
    }
  }

  private async detachRemote(): Promise<void> {
    try {
      await this.chat.detachRemoteTask();
      this.postRemoteTasks();
      void vscode.window.showInformationMessage("MaClaw: 已切回本地工作区编程 agent");
    } catch (err) {
      void vscode.window.showErrorMessage(
        `MaClaw: 切换失败 — ${err instanceof Error ? err.message : String(err)}`
      );
    }
  }

  /** Keep a just-created or attached row visible during eventual index refreshes. */
  private upsertRemoteTask(task: RemoteCodingTask): void {
    const index = this.remoteTasks.findIndex((candidate) => candidate.project_path === task.project_path);
    if (index < 0) {
      this.remoteTasks = [task, ...this.remoteTasks];
      return;
    }
    // The list response is the newest source for connection state (armed /
    // reconnect / message); keep caller-supplied fields only as a fallback for
    // rows that are temporarily absent from that response.
    this.remoteTasks[index] = { ...task, ...this.remoteTasks[index] };
  }

  private postRemoteTasks(): void {
    if (!this.view) {
      return;
    }
    void this.view.webview
      .postMessage({
        type: "remoteTasks",
        tasks: this.remoteTasks,
        selected: this.selectedRemotePath,
        mode: this.chat.getAgentMode(),
        remoteLabel: this.chat.getStatusSnapshot().remoteLabel,
        attaching: this.attachingRemoteTask,
      })
      .then(undefined, () => {});
  }

  // ---- provider switching (shared with the MaClaw GUI via config.json) ----

  private setProvider(name?: string): void {
    if (!name) {
      return;
    }
    try {
      writeCurrentProvider(name);
    } catch (err) {
      void vscode.window.showErrorMessage(
        `MaClaw: 切换服务商失败 — ${err instanceof Error ? err.message : String(err)}`
      );
    }
    this.postProviders();
  }

  private postProviders(): void {
    if (!this.view) {
      return;
    }
    const state = readMaclawLLMConfig();
    void this.view.webview
      .postMessage({
        type: "providers",
        ok: state !== undefined,
        providers: state?.providers ?? [],
        current: state?.current ?? "",
      })
      .then(undefined, () => {});
  }

  // ---- selection / file shortcuts ----------------------------------------

  private currentSelection(): { text: string; language: string; file: string } | undefined {
    const editor = vscode.window.activeTextEditor;
    if (!editor || editor.selection.isEmpty || editor.document.uri.scheme !== "file") {
      return undefined;
    }
    const text = editor.document.getText(editor.selection).trim();
    if (text === "") {
      return undefined;
    }
    return {
      text,
      language: editor.document.languageId,
      file: editor.document.uri.fsPath,
    };
  }

  private async sendSelectionPrompt(kind: "explain" | "fix" | "test"): Promise<void> {
    const sel = this.currentSelection();
    if (!sel) {
      void vscode.window.showInformationMessage("MaClaw: 请先在编辑器中选中一段代码。");
      return;
    }
    const maxChars = 4000;
    const truncated = sel.text.length > maxChars;
    const body = truncated ? sel.text.slice(0, maxChars) : sel.text;
    const leads: Record<typeof kind, string> = {
      explain: "解释以下代码的作用与关键逻辑",
      fix: "检查并修复以下代码中的问题，给出修改后的完整代码",
      test: "为以下代码编写单元测试",
    };
    const prompt =
      `${leads[kind]}（文件：${sel.file}${truncated ? "，选区过长已截取" : ""}）：\n\n` +
      "```" +
      sel.language +
      "\n" +
      body +
      (truncated ? "\n// …（截断）" : "") +
      "\n```";
    await this.chat.sendPromptFromLauncher(prompt);
  }

  private async sendFilePrompt(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor || editor.document.uri.scheme !== "file") {
      void vscode.window.showInformationMessage("MaClaw: 没有打开的文件。");
      return;
    }
    await this.chat.sendPromptFromLauncher(
      `请阅读并总结当前文件，指出主要结构和可改进之处：${editor.document.uri.fsPath}`
    );
  }

  // ---- live updates -------------------------------------------------------

  private postStatus(snap: StatusSnapshot): void {
    void this.view?.webview.postMessage({ type: "status", snap }).then(undefined, () => {});
    // Keep remote card in sync with mode changes from chat.
    this.postRemoteTasks();
  }

  private lastSelectionPost = "";

  private postSelection(): void {
    if (!this.view) {
      return;
    }
    const editor = vscode.window.activeTextEditor;
    const hasSelection = !!editor && !editor.selection.isEmpty && editor.document.uri.scheme === "file";
    const hasFile = !!editor && editor.document.uri.scheme === "file";
    const key = `${hasSelection}|${hasFile}`;
    if (key === this.lastSelectionPost) {
      return;
    }
    this.lastSelectionPost = key;
    void this.view.webview
      .postMessage({ type: "selection", hasSelection, hasFile })
      .then(undefined, () => {});
  }

  private renderHtml(webview: vscode.Webview): string {
    const scriptUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.context.extensionUri, "dist", "launcher.js")
    );
    const nonce = String(Date.now()) + String(Math.random()).slice(2);
    return `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy"
      content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>MaClaw</title>
</head>
<body>
  <div class="panel">
    <div class="card status-card">
      <div class="status-row"><span id="status-dot" class="dot"></span><span id="status-text">未连接</span></div>
      <div id="status-detail" class="detail"></div>
      <div id="session-cwd" class="detail"></div>
      <div id="agent-mode" class="detail mode-line"></div>
    </div>

    <div class="card">
      <div class="card-title">服务商</div>
      <select id="provider-select" class="select" data-action-provider></select>
      <div class="hint" id="provider-hint">读取中…</div>
    </div>

    <div class="card">
      <div class="card-title">远程编程</div>
      <details class="remote-create">
        <summary>新建远程任务</summary>
        <div class="remote-create-fields">
          <label class="field-label" for="remote-task-name">任务名称</label>
          <input id="remote-task-name" class="input" placeholder="例如：生产站点修复" required>
          <label class="field-label" for="remote-host">SSH 主机</label>
          <input id="remote-host" class="input" placeholder="例如：server.example.com" required>
          <div class="form-row">
            <div class="field-group">
              <label class="field-label" for="remote-user">用户名</label>
              <input id="remote-user" class="input" placeholder="用户名" required>
            </div>
            <div class="field-group port-group">
              <label class="field-label" for="remote-port">端口</label>
              <input id="remote-port" class="input" type="number" min="1" max="65535" value="22" inputmode="numeric" required>
            </div>
          </div>
          <label class="field-label" for="remote-workdir">远程工作目录</label>
          <input id="remote-workdir" class="input" placeholder="例如：/srv/app" required>
          <button class="btn primary" id="btn-create-remote" data-action="createRemoteTask">创建并附着</button>
          <div class="hint">密码只会在连接时询问，不会保存到 MaClaw 或 VS Code。</div>
        </div>
      </details>
      <select id="remote-select" class="select"></select>
      <div class="hint" id="remote-hint">连接 GUI 后刷新任务列表</div>
      <div class="btn-row">
        <button class="btn" data-action="refreshRemoteTasks">刷新</button>
        <button class="btn primary" id="btn-attach-remote" data-action="attachRemoteTask" disabled>附着远程</button>
      </div>
      <div class="btn-row">
        <button class="btn" id="btn-detach-remote" data-action="detachRemoteTask" disabled>切回本地</button>
        <button class="btn" id="btn-open-task" data-action="openTaskFolder" disabled>任务目录</button>
      </div>
      <div class="btn-row">
        <button class="btn" id="btn-list-remote" data-action="listRemoteDir" disabled>远端 ls</button>
        <button class="btn" id="btn-refresh-preview" data-action="refreshRemotePreviews" disabled>刷新预览</button>
      </div>
      <div class="btn-row">
        <button class="btn" id="btn-remote-ssh" data-action="openRemoteSSH" disabled>Remote-SSH</button>
        <button class="btn" id="btn-diff-remote" data-action="diffRemoteLocal" disabled>远端↔本地</button>
      </div>
      <button class="btn" id="btn-search-remote" data-action="searchRemote" disabled>远端搜索</button>
    </div>

    <div class="card">
      <div class="card-title">会话</div>
      <button class="btn primary" data-action="openChat">打开 MaClaw 聊天</button>
      <div class="btn-row">
        <button class="btn" data-action="newSession">新会话</button>
        <button class="btn" data-action="reconnect">重新连接</button>
      </div>
      <button class="btn warn" id="btn-cancel" data-action="cancel" disabled>停止当前回合</button>
    </div>

    <div class="card">
      <div class="card-title">选中代码</div>
      <div class="btn-row">
        <button class="btn sel" data-action="explain" disabled>解释</button>
        <button class="btn sel" data-action="fix" disabled>修复</button>
        <button class="btn sel" data-action="test" disabled>写测试</button>
      </div>
      <div class="hint" id="sel-hint">在编辑器中选中代码后可用</div>
    </div>

    <div class="card">
      <div class="card-title">当前文件</div>
      <button class="btn" id="btn-file" data-action="summarizeFile" disabled>总结当前文件</button>
    </div>

    <div class="footer">
      <a href="#" data-action="openSettings">扩展设置</a>
    </div>
  </div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
  }
}
