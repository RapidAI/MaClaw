import * as path from "path";
import * as vscode from "vscode";
import { AcpClient, PendingPermission } from "./acpClient";
import { resolveBridgePath } from "./bridgeResolver";

export interface StatusSnapshot {
  state: string;
  detail: string;
  bridge: string;
  sessionId: string;
  cwd: string;
  turnActive: boolean;
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

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly extensionVersion: string
  ) {
    this.output = vscode.window.createOutputChannel("MaClaw ACP");
    this.wireClientEvents();
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
      cwd: this.workspaceCwd(),
      turnActive: this.turnActive,
    };
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
    if (this.turnActive) {
      void vscode.window.showInformationMessage("MaClaw: 当前回合进行中，请先等待完成或停止。");
      return;
    }
    await vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`);
    await this.runPrompt(text);
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
    this.post({ type: "reset" }, false);
    if (this.client.isRunning) {
      try {
        this.sessionId = await this.client.newSession(this.workspaceCwd());
      } catch (err) {
        this.postStatus("error", `new session failed: ${errMessage(err)}`);
      }
    }
    this.postTurnState();
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
  }): Promise<void> {
    switch (msg.type) {
      case "ready":
        this.post({ type: "replay", events: this.transcript }, false);
        // Re-send the last known status (may be "connecting" while a bridge
        // handshake is still in flight — deriving from isRunning would lie).
        this.post({ type: "status", state: this.lastStatus.state, detail: this.lastStatus.detail }, false);
        this.postTurnState();
        return;
      case "prompt":
        if (typeof msg.text === "string" && msg.text.trim() !== "") {
          await this.runPrompt(msg.text);
        }
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
    }
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
    // Only open files inside the current workspace folders.
    const norm = (p: string) => {
      const r = path.resolve(p);
      return process.platform === "win32" ? r.toLowerCase() : r;
    };
    const targetNorm = norm(target);
    const inWorkspace = (vscode.workspace.workspaceFolders ?? []).some((f) => {
      const base = norm(f.uri.fsPath);
      return targetNorm === base || targetNorm.startsWith(base + path.sep);
    });
    if (!inWorkspace) {
      void vscode.window.showWarningMessage(`MaClaw: refusing to open path outside the workspace: ${target}`);
      return;
    }
    try {
      const doc = await vscode.workspace.openTextDocument(vscode.Uri.file(target));
      await vscode.window.showTextDocument(doc, { preview: true });
    } catch (err) {
      void vscode.window.showWarningMessage(`MaClaw: cannot open ${target}: ${errMessage(err)}`);
    }
  }

  // ---- prompt flow -------------------------------------------------------

  private async runPrompt(text: string): Promise<void> {
    if (this.turnActive) {
      void vscode.window.showInformationMessage("MaClaw: 当前回合进行中，请先等待完成或停止。");
      return;
    }
    // Set synchronously before any await: a second prompt message arriving
    // while we connect/create a session must not start a concurrent turn.
    this.turnActive = true;
    // Generation guards the finally-block: if the user resets the session
    // mid-turn, our stale finally must not clear a NEWER turn's flag.
    const generation = ++this.turnGeneration;
    // Echo immediately so the user's text is never lost (e.g. when the
    // bridge is missing and the turn fails below).
    this.post({ type: "userPrompt", text });
    this.postTurnState();
    try {
      const connected = await this.ensureConnected();
      if (!connected) {
        this.post({ type: "turnError", message: "MaClaw bridge is not connected" });
        return;
      }
      if (!this.sessionId) {
        this.sessionId = await this.client.newSession(this.workspaceCwd());
      }
      const sessionId = this.sessionId;
      const res = await this.client.prompt(sessionId, text);
      // A "new session" during the turn makes this result stale.
      if (sessionId !== this.sessionId) {
        return;
      }
      this.post({ type: "turnEnd", stopReason: res.stopReason ?? "end_turn" });
    } catch (err) {
      this.post({ type: "turnError", message: errMessage(err) });
    } finally {
      // Skip when a newer turn/session invalidated this one (generation
      // mismatch) — clearing here would clobber the newer turn's state.
      if (generation === this.turnGeneration) {
        this.turnActive = false;
        this.postTurnState();
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
    this.post({ type: "status", state, detail: detail ?? "" }, false);
    this.emitStatus();
  }

  private postTurnState(): void {
    this.post({ type: "turnState", active: this.turnActive }, false);
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
