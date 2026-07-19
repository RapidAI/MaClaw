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
  /** Number of prompts waiting in the pre-input queue. */
  queued: number;
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
      queued: this.queue.length,
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
    this.postQueue();
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
    id?: number;
  }): Promise<void> {
    switch (msg.type) {
      case "ready":
        this.post({ type: "replay", events: this.transcript }, false);
        // Re-send the last known status (may be "connecting" while a bridge
        // handshake is still in flight — deriving from isRunning would lie).
        this.post({ type: "status", state: this.lastStatus.state, detail: this.lastStatus.detail }, false);
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
        this.fireQueued(msg.id);
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
   * User picked a queued prompt to fire: when idle it runs right away; while
   * a turn is in flight it jumps to the head of the queue so it steers the
   * very next turn.
   */
  private fireQueued(id?: number): void {
    const at = this.queue.findIndex((item) => item.id === id);
    if (at < 0) {
      return; // already fired or removed
    }
    const [item] = this.queue.splice(at, 1);
    if (this.turnActive) {
      this.queue.unshift(item);
      this.postQueue();
      return;
    }
    this.postQueue();
    void this.runPrompt(item.text);
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
