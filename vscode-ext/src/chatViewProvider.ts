import * as path from "path";
import * as vscode from "vscode";
import { AcpClient, PendingPermission } from "./acpClient";
import { resolveBridgePath } from "./bridgeResolver";
import {
  countRemotePreviewHeaderLines,
  isRemotePosixPath,
  languageIdForRemotePath,
  parseLsListing,
  RemoteFileProvider,
  remotePathRelativeToWorkDir,
  remotePathToUri,
  uriToRemotePath,
  REMOTE_SCHEME,
} from "./remoteFs";
import type { RemoteSearchTreeProvider } from "./remoteSearchTree";
import type { AgentChangeTreeProvider } from "./agentChangeTree";
import type { RemoteExplorerTreeProvider } from "./remoteExplorerTree";

export interface RecentRemoteFile {
  path: string;
  at: number;
}

export interface StatusSnapshot {
  state: string;
  detail: string;
  bridge: string;
  sessionId: string;
  cwd: string;
  turnActive: boolean;
  /** Number of prompts waiting in the pre-input queue. */
  queued: number;
  /** Active agent mode: local workspace or attached remote coding task. */
  mode: "local" | "remote";
  /** Human-readable remote target (user@host:workdir) when mode=remote. */
  remoteLabel: string;
}

/**
 * Hosts the MaClaw chat webview (bottom panel) and routes messages between the
 * webview and the ACP bridge. Keeps a bounded transcript so the view can be
 * re-created (e.g. after the user drags it to the secondary sidebar) without
 * losing the conversation display.
 */
export class ChatViewProvider implements vscode.WebviewViewProvider, vscode.Disposable {
  public static readonly viewType = "maclaw-acp.chat";

  private readonly client = new AcpClient();
  private readonly output: vscode.OutputChannel;
  private view?: vscode.WebviewView;
  private sessionId?: string;
  private connecting?: Promise<boolean>;
  private turnActive = false;
  private turnGeneration = 0;
  private permSeq = 0;
  private pendingPerms = new Map<number, PendingPermission>();
  private transcript: Record<string, unknown>[] = [];
  private readonly statusListeners = new Set<(s: StatusSnapshot) => void>();
  private static readonly transcriptCap = 800;
  /**
   * Pre-input queue: prompts typed while a turn is in flight wait here and
   * fire FIFO once the current turn ends. The bridge rejects concurrent
   * prompts ("session busy"), so queueing is the only way to not lose input.
   * Items carry stable ids — the webview addresses them by id, so a shift
   * from fireNextQueued racing a user click can't hit the wrong entry.
   */
  private queue: { id: number; text: string }[] = [];
  private queueSeq = 0;
  /** Set when the last turn errored: auto-fire pauses until the user resumes. */
  private queuePaused = false;
  private static readonly queueCap = 50;
  /**
   * When set, ACP session/new uses this cwd instead of the VS Code workspace
   * folder — used to attach remote_coding_dev tasks (desktop-user:{taskPath}).
   */
  private sessionCwdOverride?: string;
  private agentMode: "local" | "remote" = "local";
  private remoteLabel = "";
  private remoteWorkDir = "";
  private remoteHost = "";
  private remoteUser = "";
  private remotePort = 22;
  private static readonly remoteAttachStateKey = "maclaw-acp.remoteAttach";
  private remoteFs?: RemoteFileProvider;
  private searchTree?: RemoteSearchTreeProvider;
  private changeTree?: AgentChangeTreeProvider;
  private explorerTree?: RemoteExplorerTreeProvider;
  private static readonly recentRemoteKey = "maclaw-acp.recentRemoteFiles";
  private static readonly recentRemoteCap = 24;

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly extensionVersion: string
  ) {
    this.output = vscode.window.createOutputChannel("MaClaw ACP");
    this.wireClientEvents();
    this.restoreRemoteAttachState();
  }

  setSearchTree(tree: RemoteSearchTreeProvider): void {
    this.searchTree = tree;
  }

  setChangeTree(tree: AgentChangeTreeProvider): void {
    this.changeTree = tree;
    tree.setMode(this.agentMode);
    tree.setWorkDir(this.remoteWorkDir);
  }

  setExplorerTree(tree: RemoteExplorerTreeProvider): void {
    this.explorerTree = tree;
  }

  /** List a remote directory for the explorer tree (attached remote only). */
  async listRemoteDirForExplorer(
    dirPath: string
  ): Promise<{ path: string; listing: string } | undefined> {
    if (this.agentMode !== "remote" || !this.sessionCwdOverride) {
      return undefined;
    }
    const connected = await this.ensureConnected();
    if (!connected) {
      return undefined;
    }
    try {
      const res = await this.client.listRemoteDir({
        projectPath: this.sessionCwdOverride,
        path: dirPath || this.remoteWorkDir || "",
      });
      return {
        path: res.path || dirPath,
        listing: res.listing ?? "",
      };
    } catch {
      return undefined;
    }
  }

  isRemoteAttached(): boolean {
    return this.agentMode === "remote" && Boolean(this.sessionCwdOverride);
  }

  /** Register virtual document provider for maclaw-remote:// previews. */
  registerRemoteFs(context: vscode.ExtensionContext): void {
    this.remoteFs = new RemoteFileProvider(
      () => (this.client.isRunning ? this.client : undefined),
      () => (this.agentMode === "remote" ? this.sessionCwdOverride : undefined)
    );
    context.subscriptions.push(
      this.remoteFs,
      vscode.workspace.registerTextDocumentContentProvider("maclaw-remote", this.remoteFs)
    );
  }

  /** Expose the ACP client for sidebar remote-task RPCs (must be connected). */
  get acpClient(): AcpClient {
    return this.client;
  }

  getRemoteWorkDir(): string {
    return this.remoteWorkDir;
  }

  getAttachedProjectPath(): string {
    return this.agentMode === "remote" ? (this.sessionCwdOverride ?? "").trim() : "";
  }

  dispose(): void {
    this.client.stop();
    this.output.dispose();
  }

  // ---- launcher (sidebar) integration ------------------------------------

  /** A live snapshot for the sidebar control panel. */
  getStatusSnapshot(): StatusSnapshot {
    return {
      state: this.lastStatus.state,
      detail: this.lastStatus.detail,
      bridge: this.client.bridge,
      sessionId: this.sessionId ?? "",
      cwd: this.sessionCwd(),
      turnActive: this.turnActive,
      queued: this.queue.length,
      mode: this.agentMode,
      remoteLabel: this.remoteLabel,
    };
  }

  /**
   * Attach chat turns to a remote coding workbench task. The local task path
   * becomes session cwd so GUI sticky remote routing (userID) matches.
   */
  async attachRemoteTask(opts: {
    projectPath: string;
    remoteLabel: string;
    workDir?: string;
    host?: string;
    user?: string;
    port?: number;
  }): Promise<void> {
    const projectPath = (opts.projectPath ?? "").trim();
    if (!projectPath) {
      throw new Error("project path is empty");
    }
    this.sessionCwdOverride = projectPath;
    this.agentMode = "remote";
    this.remoteLabel = (opts.remoteLabel ?? "").trim();
    this.remoteWorkDir = (opts.workDir ?? "").trim();
    this.remoteHost = (opts.host ?? "").trim();
    this.remoteUser = (opts.user ?? "").trim();
    this.remotePort = opts.port && opts.port > 0 ? opts.port : 22;
    this.persistRemoteAttachState();
    this.changeTree?.setMode("remote");
    this.changeTree?.setWorkDir(this.remoteWorkDir);
    this.changeTree?.clear();
    this.explorerTree?.refresh();
    await this.newSession();
    this.emitStatus();
    this.postAgentMode();
  }

  /** Clear remote attach and return to the VS Code workspace folder. */
  async detachRemoteTask(): Promise<void> {
    this.sessionCwdOverride = undefined;
    this.agentMode = "local";
    this.remoteLabel = "";
    this.remoteWorkDir = "";
    this.remoteHost = "";
    this.remoteUser = "";
    this.remotePort = 22;
    this.persistRemoteAttachState();
    this.changeTree?.setMode("local");
    this.changeTree?.setWorkDir("");
    this.changeTree?.clear();
    this.explorerTree?.refresh();
    await this.newSession();
    this.emitStatus();
    this.postAgentMode();
  }

  getAgentMode(): "local" | "remote" {
    return this.agentMode;
  }

  /** Open the local task folder (metadata / sticky shell), not the remote tree. */
  async openAttachedTaskFolder(): Promise<void> {
    const p = (this.sessionCwdOverride ?? "").trim();
    if (!p) {
      void vscode.window.showInformationMessage("MaClaw: 当前未附着远程任务。");
      return;
    }
    try {
      const uri = vscode.Uri.file(p);
      await vscode.commands.executeCommand("revealFileInOS", uri);
    } catch {
      void vscode.window.showWarningMessage(`MaClaw: 无法打开任务目录：${p}`);
    }
  }

  private restoreRemoteAttachState(): void {
    const saved = this.context.globalState.get<{
      projectPath?: string;
      remoteLabel?: string;
      workDir?: string;
      host?: string;
      user?: string;
      port?: number;
    }>(ChatViewProvider.remoteAttachStateKey);
    const projectPath = (saved?.projectPath ?? "").trim();
    if (!projectPath) {
      return;
    }
    this.sessionCwdOverride = projectPath;
    this.agentMode = "remote";
    this.remoteLabel = (saved?.remoteLabel ?? "").trim();
    this.remoteWorkDir = (saved?.workDir ?? "").trim();
    this.remoteHost = (saved?.host ?? "").trim();
    this.remoteUser = (saved?.user ?? "").trim();
    this.remotePort = saved?.port && saved.port > 0 ? saved.port : 22;
  }

  private persistRemoteAttachState(): void {
    if (this.agentMode !== "remote" || !this.sessionCwdOverride) {
      void this.context.globalState.update(ChatViewProvider.remoteAttachStateKey, undefined);
      return;
    }
    void this.context.globalState.update(ChatViewProvider.remoteAttachStateKey, {
      projectPath: this.sessionCwdOverride,
      remoteLabel: this.remoteLabel,
      workDir: this.remoteWorkDir,
      host: this.remoteHost,
      user: this.remoteUser,
      port: this.remotePort,
    });
  }

  /** Re-fetch all open remote previews from SSH. */
  refreshOpenRemotePreviews(): number {
    return this.remoteFs?.refreshAllOpen() ?? 0;
  }

  /**
   * Open remote work_dir in VS Code Remote-SSH if the extension is installed.
   * Falls back to copying an ssh command and opening docs.
   */
  async openInRemoteSSH(): Promise<void> {
    if (this.agentMode !== "remote") {
      void vscode.window.showInformationMessage("MaClaw: 请先附着远程编程任务。");
      return;
    }
    const host = this.remoteHost.trim();
    const user = this.remoteUser.trim();
    const workDir = this.remoteWorkDir.trim();
    if (!host || !user) {
      void vscode.window.showWarningMessage(
        "MaClaw: 缺少 host/user 元数据，无法构造 Remote-SSH 链接。请重新附着任务。"
      );
      return;
    }
    // vscode-remote URI: ssh-remote+user@host/path  (optional :port not in authority; use config)
    const authority = `ssh-remote+${user}@${host}`;
    const remotePath = workDir.startsWith("/") ? workDir : `/${workDir}`;
    const uri = vscode.Uri.parse(`vscode-remote://${authority}${remotePath}`);

    const remoteExt = vscode.extensions.getExtension("ms-vscode-remote.remote-ssh");
    if (!remoteExt) {
      const sshCmd =
        this.remotePort > 0 && this.remotePort !== 22
          ? `ssh -p ${this.remotePort} ${user}@${host}`
          : `ssh ${user}@${host}`;
      await vscode.env.clipboard.writeText(sshCmd);
      const pick = await vscode.window.showInformationMessage(
        `未安装 Remote - SSH。已复制：${sshCmd}\n远端目录：${workDir || "?"}`,
        "打开扩展市场"
      );
      if (pick === "打开扩展市场") {
        await vscode.commands.executeCommand(
          "workbench.extensions.search",
          "ms-vscode-remote.remote-ssh"
        );
      }
      return;
    }
    try {
      await vscode.commands.executeCommand("vscode.openFolder", uri, {
        forceNewWindow: true,
      });
    } catch (err) {
      // Some VS Code builds prefer openFolder with different args.
      try {
        await vscode.commands.executeCommand("vscode.openFolder", uri, true);
      } catch {
        const msg = err instanceof Error ? err.message : String(err);
        void vscode.window.showWarningMessage(
          `MaClaw: 无法打开 Remote-SSH 窗口 — ${msg}。可手动：Remote-SSH → ${user}@${host} → ${workDir}`
        );
      }
    }
  }

  private postAgentMode(): void {
    this.post(
      {
        type: "agentMode",
        mode: this.agentMode,
        remoteLabel: this.remoteLabel,
        workDir: this.remoteWorkDir,
        cwd: this.sessionCwd(),
      },
      false
    );
  }

  onStatusDidChange(fn: (s: StatusSnapshot) => void): vscode.Disposable {
    this.statusListeners.add(fn);
    return new vscode.Disposable(() => this.statusListeners.delete(fn));
  }

  private emitStatus(): void {
    const snap = this.getStatusSnapshot();
    for (const fn of this.statusListeners) {
      fn(snap);
    }
  }

  /** Focus the chat view and send a prompt on the user's behalf (launcher). */
  async sendPromptFromLauncher(text: string): Promise<void> {
    await vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`);
    // Busy turn: queue instead of dropping — same path as the composer.
    this.submitPrompt(text);
  }

  async reconnect(): Promise<void> {
    await this.ensureConnected();
  }

  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view;
    view.webview.options = {
      enableScripts: true,
      localResourceRoots: [vscode.Uri.joinPath(this.context.extensionUri, "dist")],
    };
    view.webview.html = this.renderHtml(view.webview);

    view.webview.onDidReceiveMessage((msg) => void this.handleWebviewMessage(msg));
    view.onDidDispose(() => {
      this.view = undefined;
      // Don't leave the agent waiting on permission prompts whose UI is gone,
      // and record the resolution so a replayed transcript doesn't show them
      // as still-clickable.
      for (const [requestId, perm] of this.pendingPerms) {
        this.client.cancelPermission(perm.rpcId);
        this.post({ type: "permissionResolved", requestId, optionId: "cancelled" });
      }
      this.pendingPerms.clear();
    });
  }

  async newSession(): Promise<void> {
    // Invalidate the finally-block of any in-flight runPrompt (see below).
    this.turnGeneration++;
    // Cancel the in-flight turn on the old session so its late updates don't
    // leak into the fresh transcript (the update filter below drops the rest).
    if (this.turnActive && this.sessionId && this.client.isRunning) {
      this.client.cancel(this.sessionId);
    }
    this.turnActive = false;
    this.sessionId = undefined;
    this.transcript = [];
    // A fresh session must not inherit prompts queued for the old one.
    this.queue = [];
    this.queuePaused = false;
    this.changeTree?.clear();
    this.postQueue();
    this.post({ type: "reset" }, false);
    if (this.client.isRunning) {
      try {
        this.sessionId = await this.client.newSession(this.sessionCwd());
      } catch (err) {
        this.postStatus("error", `new session failed: ${errMessage(err)}`);
      }
    }
    this.postTurnState();
    this.emitStatus();
  }

  cancelTurn(): void {
    if (this.sessionId && this.client.isRunning) {
      this.client.cancel(this.sessionId);
    }
  }

  // ---- webview messages -------------------------------------------------

  private async handleWebviewMessage(msg: {
    type: string;
    text?: string;
    requestId?: number;
    optionId?: string;
    path?: string;
    id?: number;
  }): Promise<void> {
    switch (msg.type) {
      case "ready":
        this.post({ type: "replay", events: this.transcript }, false);
        // Re-send the last known status (may be "connecting" while a bridge
        // handshake is still in flight — deriving from isRunning would lie).
        this.post({ type: "status", state: this.lastStatus.state, detail: this.lastStatus.detail }, false);
        this.postAgentMode();
        this.postTurnState();
        this.postQueue();
        return;
      case "prompt":
        if (typeof msg.text === "string" && msg.text.trim() !== "") {
          this.submitPrompt(msg.text);
        }
        return;
      case "queueRemove":
        this.removeQueued(msg.id);
        return;
      case "queueFire":
        await this.fireQueued(msg.id);
        return;
      case "queueClear":
        this.queue = [];
        this.queuePaused = false;
        this.postQueue();
        return;
      case "cancel":
        this.cancelTurn();
        return;
      case "newSession":
        await this.newSession();
        return;
      case "permissionResponse":
        this.handlePermissionResponse(msg.requestId, msg.optionId);
        return;
      case "openFile":
        await this.openFile(msg.path);
        return;
      case "diffRemote":
        if (this.agentMode === "remote") {
          await this.diffRemoteWithLocal(this.resolveRemotePathHint(msg.path));
        } else if (msg.path) {
          // Local mode: open the file (true workspace diff needs two URIs).
          await this.openFile(msg.path);
        }
        return;
      case "copyPath": {
        const p = this.resolveRemotePathHint(msg.path) || (msg.path ?? "").trim();
        if (p) {
          await vscode.env.clipboard.writeText(p);
          void vscode.window.showInformationMessage(`MaClaw: 已复制 ${p}`);
        }
        return;
      }
    }
  }

  /**
   * Resolve a path hint from chat cards (absolute, ~/…, or work_dir-relative)
   * for remote open/diff.
   */
  resolveRemotePathHint(raw?: string): string {
    const trimmed = (raw ?? "").trim().replace(/^remote:/, "").replace(/\\/g, "/");
    if (!trimmed) {
      return "";
    }
    if (this.agentMode !== "remote") {
      return trimmed;
    }
    if (isRemotePosixPath(trimmed)) {
      return trimmed;
    }
    const wd = (this.remoteWorkDir || "").replace(/\/+$/, "");
    if (!wd) {
      return trimmed;
    }
    if (trimmed.startsWith("./")) {
      return `${wd}/${trimmed.slice(2)}`;
    }
    return `${wd}/${trimmed.replace(/^\//, "")}`;
  }

  private handlePermissionResponse(requestId?: number, optionId?: string): void {
    if (requestId === undefined) {
      return;
    }
    const perm = this.pendingPerms.get(requestId);
    if (!perm) {
      return;
    }
    this.pendingPerms.delete(requestId);
    if (optionId) {
      this.client.resolvePermission(perm.rpcId, optionId);
      this.post({ type: "permissionResolved", requestId, optionId });
    } else {
      this.client.cancelPermission(perm.rpcId);
      this.post({ type: "permissionResolved", requestId, optionId: "cancelled" });
    }
  }

  private async openFile(target?: string): Promise<void> {
    if (!target) {
      return;
    }
    const trimmed = target.trim().replace(/^remote:/, "");
    // Remote agent paths (absolute, ~/…, or work_dir-relative): virtual preview.
    if (this.agentMode === "remote") {
      const remote = this.resolveRemotePathHint(trimmed);
      if (remote && (isRemotePosixPath(remote) || this.remoteWorkDir)) {
        await this.openRemotePreview(remote);
        return;
      }
    }
    // Only open files inside the current workspace folders or attached task dir.
    const norm = (p: string) => {
      const r = path.resolve(p);
      return process.platform === "win32" ? r.toLowerCase() : r;
    };
    const targetNorm = norm(trimmed);
    const bases: string[] = (vscode.workspace.workspaceFolders ?? []).map((f) => norm(f.uri.fsPath));
    if (this.sessionCwdOverride) {
      bases.push(norm(this.sessionCwdOverride));
    }
    const inAllowed = bases.some((base) => targetNorm === base || targetNorm.startsWith(base + path.sep));
    if (!inAllowed) {
      void vscode.window.showWarningMessage(`MaClaw: refusing to open path outside the workspace: ${trimmed}`);
      return;
    }
    try {
      const doc = await vscode.workspace.openTextDocument(vscode.Uri.file(trimmed));
      await vscode.window.showTextDocument(doc, { preview: true });
    } catch (err) {
      void vscode.window.showWarningMessage(`MaClaw: cannot open ${trimmed}: ${errMessage(err)}`);
    }
  }

  /**
   * Fetch remote file over sticky SSH and show as maclaw-remote:// document.
   * @param opts.line 1-based line in the remote file (not counting preview header).
   * @param opts.highlight optional substring to select on that line.
   */
  async openRemotePreview(
    remotePath: string,
    opts?: { line?: number; highlight?: string }
  ): Promise<void> {
    // Normalize relative / remote: paths against work_dir.
    const p = this.resolveRemotePathHint(remotePath);
    if (!p) {
      return;
    }
    if (this.agentMode !== "remote" || !this.sessionCwdOverride) {
      void vscode.window.showWarningMessage("MaClaw: 请先附着远程编程任务再预览远端文件。");
      return;
    }
    const connected = await this.ensureConnected();
    if (!connected) {
      void vscode.window.showWarningMessage("MaClaw: bridge 未连接，无法读取远端文件。");
      return;
    }
    try {
      // Proactive read so we surface SSH errors with a clear toast.
      await this.client.readRemoteFile({
        projectPath: this.sessionCwdOverride,
        path: p,
        offset: 1,
        limit: 2000,
      });
    } catch (err) {
      const msg = errMessage(err);
      if (/SSH session not connected|re-attach|password/i.test(msg)) {
        void vscode.window.showWarningMessage(
          `MaClaw: SSH 会话失效 — ${msg}。请在侧栏重新「附着远程」。`
        );
      } else {
        void vscode.window.showWarningMessage(`MaClaw: 读取远端文件失败 — ${msg}`);
      }
      // Still open virtual doc (shows error header).
    }
    const uri = remotePathToUri(p);
    this.remoteFs?.refresh(uri);
    try {
      const doc = await vscode.workspace.openTextDocument(uri);
      // Best-effort language mode from extension.
      const lang = languageIdForRemotePath(p);
      if (lang !== "plaintext" && doc.languageId === "plaintext") {
        await vscode.languages.setTextDocumentLanguage(doc, lang);
      }
      const editor = await vscode.window.showTextDocument(doc, {
        preview: true,
        preserveFocus: false,
      });
      this.recordRecentRemote(p);
      if (opts?.line && opts.line > 0) {
        this.revealRemotePreviewLine(editor, opts.line, opts.highlight);
      }
    } catch (err) {
      void vscode.window.showWarningMessage(`MaClaw: 无法打开预览：${errMessage(err)}`);
    }
  }

  /** Remember a remote path for "Open Recent Remote File". */
  private recordRecentRemote(remotePath: string): void {
    const p = remotePath.trim().replace(/^remote:/, "");
    if (!p) {
      return;
    }
    const prev = this.getRecentRemoteFiles().filter((r) => r.path !== p);
    const next: RecentRemoteFile[] = [{ path: p, at: Date.now() }, ...prev].slice(
      0,
      ChatViewProvider.recentRemoteCap
    );
    void this.context.globalState.update(ChatViewProvider.recentRemoteKey, next);
  }

  getRecentRemoteFiles(): RecentRemoteFile[] {
    const raw = this.context.globalState.get<RecentRemoteFile[]>(
      ChatViewProvider.recentRemoteKey
    );
    if (!Array.isArray(raw)) {
      return [];
    }
    return raw
      .filter((r) => r && typeof r.path === "string" && r.path.trim() !== "")
      .map((r) => ({ path: String(r.path), at: typeof r.at === "number" ? r.at : 0 }));
  }

  /** QuickPick recent remote previews (newest first). */
  async openRecentRemoteFile(): Promise<void> {
    if (this.agentMode !== "remote" || !this.sessionCwdOverride) {
      void vscode.window.showWarningMessage("MaClaw: 请先附着远程编程任务。");
      return;
    }
    const items = this.getRecentRemoteFiles();
    if (items.length === 0) {
      void vscode.window.showInformationMessage("MaClaw: 还没有最近打开的远端文件");
      return;
    }
    const pick = await vscode.window.showQuickPick(
      items.map((r) => ({
        label: path.posix.basename(r.path),
        description: r.path,
        detail: r.at ? new Date(r.at).toLocaleString() : undefined,
        remotePath: r.path,
      })),
      {
        title: "最近打开的远端预览",
        placeHolder: "选择重新打开",
        matchOnDescription: true,
      }
    );
    if (pick?.remotePath) {
      await this.openRemotePreview(pick.remotePath);
    }
  }

  /** Copy remote path of the active maclaw-remote:// tab (or prompt). */
  async copyActiveRemotePath(opts?: { relative?: boolean }): Promise<void> {
    const ed = vscode.window.activeTextEditor;
    let remote = "";
    if (ed && ed.document.uri.scheme === REMOTE_SCHEME) {
      remote = uriToRemotePath(ed.document.uri);
    }
    if (!remote) {
      void vscode.window.showInformationMessage("MaClaw: 当前不是远端预览标签");
      return;
    }
    let text = remote;
    if (opts?.relative && this.remoteWorkDir) {
      text = remotePathRelativeToWorkDir(remote, this.remoteWorkDir);
    }
    await vscode.env.clipboard.writeText(text);
    void vscode.window.showInformationMessage(`MaClaw: 已复制 ${text}`);
  }

  /**
   * Map a 1-based remote source line onto the virtual document (skips // header).
   */
  private revealRemotePreviewLine(
    editor: vscode.TextEditor,
    remoteLine: number,
    highlight?: string
  ): void {
    const doc = editor.document;
    const header = countRemotePreviewHeaderLines(doc);
    const targetLine = Math.min(
      Math.max(0, header + remoteLine - 1),
      Math.max(0, doc.lineCount - 1)
    );
    const lineText = doc.lineAt(targetLine).text;
    let startCol = 0;
    let endCol = lineText.length;
    const needle = (highlight ?? "").trim();
    if (needle) {
      const idx = lineText.indexOf(needle);
      if (idx >= 0) {
        startCol = idx;
        endCol = idx + needle.length;
      }
    }
    const start = new vscode.Position(targetLine, startCol);
    const end = new vscode.Position(targetLine, endCol);
    editor.selection = new vscode.Selection(start, end);
    editor.revealRange(new vscode.Range(start, end), vscode.TextEditorRevealType.InCenter);
  }

  /**
   * Find occurrences in the active maclaw-remote preview and jump to a match.
   * Prefer VS Code built-in Find when user just wants the widget.
   */
  async findInActiveRemotePreview(): Promise<void> {
    const ed = vscode.window.activeTextEditor;
    if (!ed || ed.document.uri.scheme !== REMOTE_SCHEME) {
      void vscode.window.showInformationMessage("MaClaw: 请先打开一个远端预览标签");
      return;
    }
    const query = await vscode.window.showInputBox({
      title: "在远端预览中查找",
      prompt: "匹配当前预览文档内容（含 header 注释）",
      placeHolder: "search text",
      ignoreFocusOut: true,
    });
    if (query === undefined || query === "") {
      // Empty cancel vs open native find widget
      if (query === "") {
        await vscode.commands.executeCommand("actions.find");
      }
      return;
    }
    const doc = ed.document;
    const q = query;
    type MatchPick = vscode.QuickPickItem & { line: number; start: number; end: number };
    const matches: MatchPick[] = [];
    const max = 200;
    for (let i = 0; i < doc.lineCount && matches.length < max; i++) {
      const text = doc.lineAt(i).text;
      let from = 0;
      for (;;) {
        const idx = text.indexOf(q, from);
        if (idx < 0) {
          break;
        }
        matches.push({
          label: `L${i + 1}`,
          description: text.trim().slice(0, 120),
          detail: `col ${idx + 1}`,
          line: i,
          start: idx,
          end: idx + q.length,
        });
        from = idx + Math.max(1, q.length);
        if (matches.length >= max) {
          break;
        }
      }
    }
    if (matches.length === 0) {
      void vscode.window.showInformationMessage(`MaClaw: 预览中无匹配 — ${q}`);
      return;
    }
    const pick = await vscode.window.showQuickPick(matches, {
      title: `预览内查找 · ${q}（${matches.length}${matches.length >= max ? "+" : ""}）`,
      placeHolder: "选择跳转",
      matchOnDescription: true,
    });
    if (!pick) {
      return;
    }
    const start = new vscode.Position(pick.line, pick.start);
    const end = new vscode.Position(pick.line, pick.end);
    ed.selection = new vscode.Selection(start, end);
    ed.revealRange(new vscode.Range(start, end), vscode.TextEditorRevealType.InCenter);
  }

  /**
   * Browse a remote directory: QuickPick with files/dirs.
   * Selecting a file opens maclaw-remote preview; selecting a dir drills in.
   */
  async openRemoteDirListing(remoteDir?: string): Promise<void> {
    if (this.agentMode !== "remote" || !this.sessionCwdOverride) {
      void vscode.window.showWarningMessage("MaClaw: 请先附着远程编程任务。");
      return;
    }
    const connected = await this.ensureConnected();
    if (!connected) {
      void vscode.window.showWarningMessage("MaClaw: bridge 未连接。");
      return;
    }
    let dir = (remoteDir ?? this.remoteWorkDir ?? "").trim() || ".";
    const workRoot = (this.remoteWorkDir || "").replace(/\/+$/, "") || "";

    for (;;) {
      let res: { ok?: boolean; path?: string; work_dir?: string; listing?: string };
      try {
        res = await this.client.listRemoteDir({
          projectPath: this.sessionCwdOverride,
          path: dir,
        });
      } catch (err) {
        void vscode.window.showWarningMessage(`MaClaw: 列目录失败 — ${errMessage(err)}`);
        return;
      }
      const abs = (res.path || dir).replace(/\/+$/, "") || "/";
      const listing = res.listing ?? "";
      const entries = parseLsListing(listing, abs);

      type PickItem = vscode.QuickPickItem & {
        entryKind?: "file" | "dir" | "up" | "raw" | "multi" | "search";
        remotePath?: string;
      };
      const items: PickItem[] = [];

      // Parent directory (stay within work_dir when possible).
      if (workRoot && abs !== workRoot && abs.startsWith(workRoot + "/")) {
        const parent = abs.replace(/\/[^/]+$/, "") || workRoot;
        items.push({
          label: "$(arrow-up) ..",
          description: parent,
          entryKind: "up",
          remotePath: parent,
        });
      }

      const fileEntries = entries.filter((e) => e.kind === "file" || e.kind === "link");
      for (const e of entries) {
        const icon =
          e.kind === "dir" ? "$(folder)" : e.kind === "link" ? "$(link)" : "$(file)";
        items.push({
          label: `${icon} ${e.name}`,
          description: e.kind,
          detail: e.path,
          entryKind: e.kind === "dir" ? "dir" : "file",
          remotePath: e.path,
        });
      }

      if (fileEntries.length > 1) {
        items.push({
          label: "$(files) 多选打开文件…",
          description: `${fileEntries.length} files`,
          entryKind: "multi",
        });
      }
      items.push({
        label: "$(search) 在此目录搜索…",
        description: abs,
        entryKind: "search",
        remotePath: abs,
      });
      items.push({
        label: "$(output) 显示原始 ls 输出",
        description: abs,
        entryKind: "raw",
      });

      if (items.length === 0) {
        void vscode.window.showInformationMessage(`MaClaw: 目录为空 — ${abs}`);
        return;
      }

      const pick = await vscode.window.showQuickPick(items, {
        title: `远端目录 · ${abs}`,
        placeHolder: "选择文件打开预览，或进入子目录（支持多选）",
        matchOnDescription: true,
        matchOnDetail: true,
      });
      if (!pick) {
        return;
      }
      if (pick.entryKind === "raw") {
        const content = [
          `# remote ls -la ${abs}`,
          `# work_dir: ${res.work_dir || this.remoteWorkDir || "?"}`,
          `# attached: ${this.remoteLabel || "?"}`,
          `# tip: 用侧栏「远端 ls」可点选打开文件`,
          "",
          listing,
          "",
        ].join("\n");
        const doc = await vscode.workspace.openTextDocument({
          content,
          language: "shellscript",
        });
        await vscode.window.showTextDocument(doc, { preview: true });
        return;
      }
      if (pick.entryKind === "search") {
        await this.searchRemoteAndOpen(pick.remotePath || abs);
        return;
      }
      if (pick.entryKind === "multi") {
        const multi = await vscode.window.showQuickPick(
          fileEntries.map((e) => ({
            label: e.name,
            description: e.kind,
            detail: e.path,
            path: e.path,
            picked: false,
          })),
          {
            title: `多选打开 · ${abs}`,
            placeHolder: "空格多选，回车打开预览（最多 10 个）",
            canPickMany: true,
            matchOnDetail: true,
          }
        );
        if (!multi || multi.length === 0) {
          continue;
        }
        const cap = multi.slice(0, 10);
        for (const m of cap) {
          await this.openRemotePreview(m.path);
        }
        if (multi.length > 10) {
          void vscode.window.showInformationMessage(
            `MaClaw: 已打开 10 个预览（共选 ${multi.length}，其余跳过）`
          );
        }
        return;
      }
      if (pick.entryKind === "up" || pick.entryKind === "dir") {
        dir = pick.remotePath || abs;
        continue;
      }
      if (pick.entryKind === "file" && pick.remotePath) {
        await this.openRemotePreview(pick.remotePath);
        return;
      }
      return;
    }
  }

  /**
   * Search under remote work_dir (or path) with rg/grep; open selected hit previews.
   */
  async searchRemoteAndOpen(scopePath?: string): Promise<void> {
    if (this.agentMode !== "remote" || !this.sessionCwdOverride) {
      void vscode.window.showWarningMessage("MaClaw: 请先附着远程编程任务。");
      return;
    }
    const connected = await this.ensureConnected();
    if (!connected) {
      void vscode.window.showWarningMessage("MaClaw: bridge 未连接。");
      return;
    }
    const query = await vscode.window.showInputBox({
      title: "远端搜索",
      prompt: "在 remote work_dir 内搜索（rg 优先，否则 grep）",
      placeHolder: "function main",
      ignoreFocusOut: true,
    });
    if (query === undefined || query.trim() === "") {
      return;
    }
    let res: Awaited<ReturnType<AcpClient["searchRemote"]>>;
    try {
      res = await this.client.searchRemote({
        projectPath: this.sessionCwdOverride,
        query: query.trim(),
        path: scopePath || this.remoteWorkDir || "",
        maxResults: 80,
      });
    } catch (err) {
      void vscode.window.showWarningMessage(`MaClaw: 搜索失败 — ${errMessage(err)}`);
      return;
    }
    const hits = (res.hits ?? []).map((h) => ({
      path: h.path,
      line: h.line,
      text: h.text,
      preview: h.preview || h.text,
    }));

    // Always populate the sidebar Search Results tree.
    this.searchTree?.setResults({
      query: query.trim(),
      scope: res.path || scopePath || this.remoteWorkDir || "?",
      workDir: res.work_dir || this.remoteWorkDir || "?",
      hits,
      truncated: Boolean(res.truncated),
      at: Date.now(),
    });
    void vscode.commands.executeCommand("maclaw-acp.searchResults.focus");

    if (hits.length === 0) {
      void vscode.window.showInformationMessage(`MaClaw: 无匹配 — ${query.trim()}（已更新结果树）`);
      return;
    }

    type HitPick = vscode.QuickPickItem & { remotePath: string; line: number };
    const items: HitPick[] = hits.map((h) => ({
      label: path.posix.basename(h.path),
      description: `${h.path}:${h.line}`,
      detail: h.preview || h.text,
      remotePath: h.path,
      line: h.line,
    }));

    const mode = await vscode.window.showQuickPick(
      [
        {
          label: "$(list-tree) 仅查看结果树",
          description: "在侧栏 Search Results 中点选",
          id: "tree" as const,
        },
        { label: "$(file) 单选打开并跳到行", id: "single" as const },
        { label: "$(files) 多选打开文件", id: "multi" as const },
      ],
      { title: `远端搜索 · ${query.trim()}（${hits.length}${res.truncated ? "+" : ""} hits）` }
    );
    if (!mode || mode.id === "tree") {
      return;
    }

    if (mode.id === "single") {
      const pick = await vscode.window.showQuickPick(items, {
        title: `远端搜索 · ${query.trim()}`,
        placeHolder: "选择一条命中，打开预览并跳转到该行",
        matchOnDescription: true,
        matchOnDetail: true,
      });
      if (!pick) {
        return;
      }
      await this.openRemotePreview(pick.remotePath, {
        line: pick.line,
        highlight: pick.detail?.slice(0, 80),
      });
      return;
    }

    const picked = await vscode.window.showQuickPick(items, {
      title: `远端搜索 · ${query.trim()}（多选）`,
      placeHolder: "空格多选，回车打开（每个文件跳到首次命中行）",
      canPickMany: true,
      matchOnDescription: true,
      matchOnDetail: true,
    });
    if (!picked || picked.length === 0) {
      return;
    }
    const firstLine = new Map<string, { line: number; highlight?: string }>();
    for (const p of picked) {
      if (!firstLine.has(p.remotePath)) {
        firstLine.set(p.remotePath, {
          line: p.line,
          highlight: p.detail?.slice(0, 80),
        });
      }
    }
    let opened = 0;
    for (const [remotePath, loc] of firstLine) {
      await this.openRemotePreview(remotePath, {
        line: loc.line,
        highlight: loc.highlight,
      });
      opened++;
      if (opened >= 10) {
        break;
      }
    }
    if (firstLine.size > 10) {
      void vscode.window.showInformationMessage(
        `MaClaw: 已打开 10 个预览（共 ${firstLine.size} 个文件）`
      );
    }
  }

  /**
   * Diff remote file (SSH) against a matching local workspace file.
   * Resolves local path as workDir-relative under any open workspace folder.
   */
  async diffRemoteWithLocal(remotePath?: string): Promise<void> {
    if (this.agentMode !== "remote" || !this.sessionCwdOverride) {
      void vscode.window.showWarningMessage("MaClaw: 请先附着远程编程任务。");
      return;
    }
    let remote = (remotePath ?? "").trim().replace(/^remote:/, "");
    if (!remote) {
      const ed = vscode.window.activeTextEditor;
      if (ed?.document.uri.scheme === REMOTE_SCHEME) {
        remote = uriToRemotePath(ed.document.uri);
      }
    }
    if (!remote) {
      const input = await vscode.window.showInputBox({
        title: "远端 vs 本地 Diff",
        prompt: "输入远端文件路径",
        placeHolder: "/home/proj/src/main.go",
        ignoreFocusOut: true,
      });
      remote = (input ?? "").trim();
    }
    if (!remote) {
      return;
    }

    const connected = await this.ensureConnected();
    if (!connected) {
      void vscode.window.showWarningMessage("MaClaw: bridge 未连接。");
      return;
    }

    let remoteContent = "";
    let absRemote = remote;
    try {
      const res = await this.client.readRemoteFile({
        projectPath: this.sessionCwdOverride,
        path: remote,
        offset: 1,
        limit: 2000,
      });
      remoteContent = res.content ?? "";
      absRemote = res.path || remote;
    } catch (err) {
      void vscode.window.showWarningMessage(`MaClaw: 读取远端失败 — ${errMessage(err)}`);
      return;
    }

    const rel = remotePathRelativeToWorkDir(absRemote, this.remoteWorkDir);
    const localUri = await this.findLocalUriForRemote(rel, path.posix.basename(absRemote));
    if (!localUri) {
      const openPrev = "仅打开远端预览";
      const pick = await vscode.window.showWarningMessage(
        `MaClaw: 工作区中未找到对应本地文件（尝试 ${rel}）。`,
        openPrev
      );
      if (pick === openPrev) {
        await this.openRemotePreview(absRemote);
      }
      return;
    }

    // Ensure remote virtual doc is warm, then diff local (left) vs remote (right).
    void remoteContent;
    const remoteUri = remotePathToUri(absRemote);
    this.remoteFs?.refresh(remoteUri);
    try {
      await vscode.workspace.openTextDocument(remoteUri);
    } catch {
      /* still try diff */
    }
    const title = `${path.basename(localUri.fsPath)} (本地) ↔ remote:${absRemote}`;
    await vscode.commands.executeCommand("vscode.diff", localUri, remoteUri, title);
  }

  /** Locate a local file matching remote relative path under workspace folders. */
  private async findLocalUriForRemote(
    relativePath: string,
    basename: string
  ): Promise<vscode.Uri | undefined> {
    const rel = relativePath.replace(/\\/g, "/").replace(/^\//, "");
    const folders = vscode.workspace.workspaceFolders ?? [];
    for (const folder of folders) {
      // Exact relative under folder root.
      if (rel) {
        const candidate = vscode.Uri.joinPath(folder.uri, ...rel.split("/"));
        try {
          await vscode.workspace.fs.stat(candidate);
          return candidate;
        } catch {
          /* try next */
        }
      }
    }
    // Glob by basename as last resort (cap results).
    if (basename) {
      try {
        const hits = await vscode.workspace.findFiles(`**/${basename}`, "**/node_modules/**", 20);
        if (hits.length === 1) {
          return hits[0];
        }
        if (hits.length > 1 && rel) {
          const normRel = rel.toLowerCase();
          const scored = hits.find((h) =>
            h.fsPath.replace(/\\/g, "/").toLowerCase().endsWith("/" + normRel)
          );
          if (scored) {
            return scored;
          }
        }
        if (hits.length > 1) {
          const pick = await vscode.window.showQuickPick(
            hits.map((h) => ({ label: vscode.workspace.asRelativePath(h), uri: h })),
            { title: "选择本地对照文件", placeHolder: basename }
          );
          return pick?.uri;
        }
      } catch {
        /* ignore */
      }
    }
    return undefined;
  }

  // ---- prompt flow -------------------------------------------------------

  /** Entry point for user text: run immediately when idle, queue when busy. */
  private submitPrompt(text: string): void {
    if (this.turnActive) {
      if (this.queue.length >= ChatViewProvider.queueCap) {
        // Hand the text back so the composer doesn't silently lose it.
        this.post({ type: "inputRestore", text }, false);
        void vscode.window.showInformationMessage(`MaClaw: 队列已满（${ChatViewProvider.queueCap} 条），请先等待或删除部分排队消息。`);
        return;
      }
      this.queue.push({ id: ++this.queueSeq, text });
      this.postQueue();
      return;
    }
    void this.runPrompt(text);
  }

  private removeQueued(id?: number): void {
    const at = this.queue.findIndex((item) => item.id === id);
    if (at < 0) {
      return; // already fired or removed — nothing to do
    }
    this.queue.splice(at, 1);
    this.postQueue();
  }

  /**
   * User picked a queued prompt to fire: when idle it runs right away. While
   * a turn is in flight it first tries real mid-turn steering (the GUI's
   * 引导发射 semantics via session/steer); when the host can't steer, the
   * prompt jumps to the head of the queue so it leads the very next turn.
   */
  private async fireQueued(id?: number): Promise<void> {
    const at = this.queue.findIndex((item) => item.id === id);
    if (at < 0) {
      return; // already fired or removed
    }
    const [item] = this.queue.splice(at, 1);
    if (this.turnActive) {
      // Generation guard for the steer round-trip: a newSession mid-await
      // clears the queue — re-adding the stale item below must not leak the
      // old session's prompt into the fresh queue (or steer it there).
      const generation = this.turnGeneration;
      if (await this.trySteer(item.text, generation)) {
        this.postQueue();
        return;
      }
      if (generation === this.turnGeneration) {
        this.queue.unshift(item);
        this.postQueue();
      }
      return;
    }
    this.postQueue();
    void this.runPrompt(item.text);
  }

  /**
   * Attempt to inject text into the running turn via session/steer. Returns
   * true when the loop accepted the injection (a receipt is echoed to the
   * chat). Any failure — no session, dead bridge, a host without the method,
   * or a session swap while we were awaiting — returns false so the caller
   * can queue instead.
   */
  private async trySteer(text: string, generation: number): Promise<boolean> {
    const sessionId = this.sessionId;
    if (!sessionId || !this.client.isRunning || generation !== this.turnGeneration) {
      return false;
    }
    try {
      const res = await this.client.steer(sessionId, text);
      if (res.accepted) {
        // The injection already landed on the old loop; only echo the receipt
        // when the session is still ours — a mid-RPC newSession must not find
        // an old-turn receipt in its fresh transcript.
        if (generation === this.turnGeneration) {
          this.post({ type: "steerAccepted", text });
        }
        return true;
      }
    } catch {
      /* host predates session/steer — queueing still works */
    }
    return false;
  }

  /** Fire the oldest queued prompt, if any. Called when a turn winds down. */
  private fireNextQueued(): void {
    const next = this.queue.shift();
    if (next === undefined) {
      return;
    }
    this.postQueue();
    void this.runPrompt(next.text);
  }

  private async runPrompt(text: string): Promise<void> {
    // Callers (submitPrompt/fireQueued/fireNextQueued) gate on turnActive, so
    // this never overlaps a live turn — the bridge rejects concurrent prompts.
    this.turnActive = true;
    // Generation guards the finally-block: if the user resets the session
    // mid-turn, our stale finally must not clear a NEWER turn's flag.
    const generation = ++this.turnGeneration;
    this.changeTree?.beginTurn(generation);
    // Echo immediately so the user's text is never lost (e.g. when the
    // bridge is missing and the turn fails below).
    this.post({ type: "userPrompt", text });
    this.postTurnState();
    // A failed turn pauses queue auto-fire: e.g. with a dead bridge we must
    // not burn through every queued prompt producing an error bubble each.
    // The user resumes explicitly via the chip's ▲ button.
    let failed = false;
    try {
      const connected = await this.ensureConnected();
      if (!connected) {
        failed = true;
        this.post({ type: "turnError", message: "MaClaw bridge is not connected" });
        return;
      }
      if (!this.sessionId) {
        this.sessionId = await this.client.newSession(this.sessionCwd());
      }
      const sessionId = this.sessionId;
      const res = await this.client.prompt(sessionId, text);
      // A "new session" during the turn makes this result stale.
      if (sessionId !== this.sessionId) {
        return;
      }
      this.post({ type: "turnEnd", stopReason: res.stopReason ?? "end_turn" });
      // After a successful remote turn, re-pull open previews (agent may have written files).
      if (this.agentMode === "remote") {
        const n = this.refreshOpenRemotePreviews();
        if (n > 0) {
          this.output.appendLine(`[remote-preview] refreshed ${n} open maclaw-remote document(s)`);
        }
        // Explorer may be stale if agent created dirs/files.
        this.explorerTree?.refresh();
        const changed = this.changeTree?.getThisTurnPaths() ?? [];
        if (changed.length > 0) {
          const label =
            changed.length === 1
              ? `Agent 改动了 1 个文件：${path.posix.basename(changed[0])}`
              : `Agent 改动了 ${changed.length} 个文件`;
          void vscode.window
            .showInformationMessage(`MaClaw: ${label}`, "查看改动", "全部打开")
            .then(async (choice) => {
              if (choice === "查看改动") {
                await vscode.commands.executeCommand("maclaw-acp.agentChanges.focus");
              } else if (choice === "全部打开") {
                const cap = changed.slice(0, 8);
                for (const p of cap) {
                  await this.openRemotePreview(p);
                }
                if (changed.length > 8) {
                  void vscode.window.showInformationMessage(
                    `MaClaw: 已打开 8 个（共 ${changed.length}）`
                  );
                }
              }
            });
        }
      }
    } catch (err) {
      failed = true;
      this.post({ type: "turnError", message: errMessage(err) });
    } finally {
      // Skip when a newer turn/session invalidated this one (generation
      // mismatch) — clearing here would clobber the newer turn's state.
      if (generation === this.turnGeneration) {
        this.turnActive = false;
        this.queuePaused = failed;
        if (!failed && this.queue.length > 0) {
          // Chain straight into the next queued turn: runPrompt posts its own
          // turnState(active), so the UI never flickers through an idle frame
          // (and doesn't yank focus back to the composer between turns).
          this.fireNextQueued();
        } else {
          this.postTurnState();
          if (failed) {
            // Refresh the queue strip so it shows the paused hint.
            this.postQueue();
          }
        }
      }
    }
  }

  private ensureConnected(): Promise<boolean> {
    if (this.client.isRunning) {
      return Promise.resolve(true);
    }
    this.connecting ??= (async () => {
      const bridge = resolveBridgePath();
      if (!bridge) {
        this.postStatus(
          "error",
          "maclaw-acp-bridge not found. Start the MaClaw app and use Utilities → Launch VS Code (or set maclaw-acp.bridgePath)."
        );
        return false;
      }
      this.postStatus("connecting", bridge);
      try {
        await this.client.start(bridge, this.extensionVersion);
        this.postStatus("connected", bridge);
        return true;
      } catch (err) {
        this.postStatus("error", `bridge handshake failed: ${errMessage(err)}`);
        return false;
      } finally {
        this.connecting = undefined;
      }
    })();
    return this.connecting;
  }

  // ---- ACP client events -------------------------------------------------

  private wireClientEvents(): void {
    this.client.on("update", (params: { sessionId?: string; update?: Record<string, unknown> }) => {
      // Drop stale updates from a session we already left (e.g. after the
      // user hit "new session" mid-turn).
      if (params?.sessionId && this.sessionId && params.sessionId !== this.sessionId) {
        return;
      }
      if (params?.update) {
        this.changeTree?.ingestUpdate(params.update);
        this.post({ type: "update", update: params.update });
      }
    });

    this.client.on("permission", (perm: PendingPermission) => {
      const requestId = ++this.permSeq;
      this.pendingPerms.set(requestId, perm);
      const payload = {
        type: "permission",
        requestId,
        toolCall: perm.params?.toolCall ?? {},
        options: perm.params?.options ?? [],
      };
      if (this.view) {
        this.post(payload);
      } else {
        void this.quickPickPermission(requestId, perm, payload);
      }
    });

    this.client.on("exit", () => {
      this.sessionId = undefined;
      // Resolve any open permission cards so the UI/transcript doesn't keep
      // showing them as clickable after the bridge is gone.
      for (const [requestId, perm] of this.pendingPerms) {
        this.client.cancelPermission(perm.rpcId);
        this.post({ type: "permissionResolved", requestId, optionId: "cancelled" });
      }
      this.pendingPerms.clear();
      this.postStatus("disconnected");
    });

    this.client.on("log", (line: string) => this.output.appendLine(line));
  }

  /** Fallback when the chat view has never been shown: use a QuickPick. */
  private async quickPickPermission(
    requestId: number,
    perm: PendingPermission,
    payload: Record<string, unknown>
  ): Promise<void> {
    this.post(payload); // still lands in the transcript for later replay
    const title =
      typeof (perm.params?.toolCall as { title?: string })?.title === "string"
        ? (perm.params.toolCall as { title: string }).title
        : "Allow tool?";
    const items = (perm.params?.options ?? []).map((o) => ({
      label: o.name,
      optionId: o.optionId,
    }));
    const pick = await vscode.window.showQuickPick(items, {
      title: `MaClaw: ${title}`,
      placeHolder: "The MaClaw agent is asking for permission",
    });
    if (pick) {
      this.handlePermissionResponse(requestId, pick.optionId);
    } else {
      this.handlePermissionResponse(requestId, undefined);
    }
  }

  // ---- helpers ------------------------------------------------------------

  private workspaceCwd(): string {
    const folder = vscode.workspace.workspaceFolders?.[0];
    return folder ? folder.uri.fsPath : process.cwd();
  }

  /** Effective ACP session cwd (remote task path override or local workspace). */
  private sessionCwd(): string {
    const override = (this.sessionCwdOverride ?? "").trim();
    if (override !== "") {
      return override;
    }
    return this.workspaceCwd();
  }

  private post(msg: Record<string, unknown>, record = true): void {
    if (record) {
      this.transcript.push(msg);
      if (this.transcript.length > ChatViewProvider.transcriptCap) {
        this.transcript.splice(0, this.transcript.length - ChatViewProvider.transcriptCap);
      }
    }
    // postMessage can reject when it races view disposal — never escalate.
    void this.view?.webview.postMessage(msg).then(undefined, () => {});
  }

  private lastStatus: { state: string; detail: string } = { state: "disconnected", detail: "" };

  private postStatus(state: string, detail?: string): void {
    this.lastStatus = { state, detail: detail ?? "" };
    this.post(
      {
        type: "status",
        state,
        detail: detail ?? "",
        mode: this.agentMode,
        remoteLabel: this.remoteLabel,
        cwd: this.sessionCwd(),
      },
      false
    );
    this.emitStatus();
  }

  private postTurnState(): void {
    this.post({ type: "turnState", active: this.turnActive }, false);
    this.emitStatus();
  }

  /** Sync the pre-input queue to the view (not part of the replay transcript). */
  private postQueue(): void {
    this.post(
      {
        type: "queue",
        items: this.queue.map((item) => ({ id: item.id, text: item.text })),
        paused: this.queuePaused && !this.turnActive,
      },
      false
    );
    // The sidebar snapshot carries the queue length — keep it in sync too.
    this.emitStatus();
  }

  private renderHtml(webview: vscode.Webview): string {
    const scriptUri = webview.asWebviewUri(
      vscode.Uri.joinPath(this.context.extensionUri, "dist", "webview.js")
    );
    const nonce = String(Date.now()) + String(Math.random()).slice(2);
    return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy"
      content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}'; img-src https: data:;">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>MaClaw</title>
</head>
<body>
  <div id="app">
    <div id="status" class="status"></div>
    <div id="messages" class="messages"></div>
    <div id="queue" class="queue" hidden></div>
    <form id="composer" class="composer">
      <textarea id="input" rows="2" placeholder="Ask MaClaw… (Enter to send, Shift+Enter for newline)"></textarea>
      <div class="composer-buttons">
        <button type="button" id="new-session" title="New session">⟳</button>
        <button type="button" id="cancel" title="Cancel turn" hidden>■</button>
        <button type="submit" id="send" title="Send">➤</button>
      </div>
    </form>
  </div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
  }
}

function errMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
