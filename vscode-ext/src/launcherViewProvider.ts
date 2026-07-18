import * as vscode from "vscode";
import { ChatViewProvider, StatusSnapshot } from "./chatViewProvider";
import { readMaclawLLMConfig, watchMaclawConfig, writeCurrentProvider } from "./maclawConfig";

/**
 * Sidebar (activity bar) control panel for MaClaw: live connection status,
 * session actions, LLM provider switching (shared with the MaClaw GUI), and
 * one-click "ask about the current selection/file" shortcuts that route into
 * the bottom-panel chat.
 */
export class LauncherViewProvider implements vscode.WebviewViewProvider, vscode.Disposable {
  public static readonly viewType = "maclaw-acp.launcher";

  private view?: vscode.WebviewView;
  private readonly disposables: vscode.Disposable[] = [];
  private configWatcher?: { close(): void };

  constructor(
    private readonly context: vscode.ExtensionContext,
    private readonly chat: ChatViewProvider
  ) {
    this.disposables.push(
      this.chat.onStatusDidChange((s) => this.postStatus(s)),
      vscode.window.onDidChangeTextEditorSelection(() => this.postSelection()),
      vscode.window.onDidChangeActiveTextEditor(() => this.postSelection())
    );
    // Reflect provider switches made inside the MaClaw GUI (its fsnotify
    // watcher picks ours up the same way).
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
    // Fresh webview needs the selection state re-posted even if unchanged.
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

  private async handleMessage(msg: { type: string; action?: string; name?: string }): Promise<void> {
    if (msg.type === "ready") {
      this.postStatus(this.chat.getStatusSnapshot());
      this.postSelection();
      this.postProviders();
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
    }
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
    // The config watcher will push the refreshed state; post immediately too
    // so the UI doesn't wait for the debounce.
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
    // Cap very large selections so a whole-file select doesn't flood the turn.
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
      "```" + sel.language + "\n" + body + (truncated ? "\n// …（截断）" : "") + "\n```";
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
  }

  private lastSelectionPost = "";

  private postSelection(): void {
    if (!this.view) {
      return;
    }
    // Cheap state only — never materialize the selection text here (fires per
    // keystroke); the text is read on demand when a shortcut is clicked.
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
    </div>

    <div class="card">
      <div class="card-title">服务商</div>
      <select id="provider-select" class="select" data-action-provider></select>
      <div class="hint" id="provider-hint">读取中…</div>
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
