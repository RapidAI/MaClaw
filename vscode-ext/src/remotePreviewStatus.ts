/**
 * Status-bar + in-editor decoration when viewing maclaw-remote:// read-only previews.
 */
import * as vscode from "vscode";
import { REMOTE_SCHEME, uriToRemotePath } from "./remoteFs";

export class RemotePreviewStatusBar implements vscode.Disposable {
  private readonly item: vscode.StatusBarItem;
  private readonly decoration: vscode.TextEditorDecorationType;
  private readonly subs: vscode.Disposable[] = [];

  constructor() {
    this.item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 80);
    this.item.name = "MaClaw Remote Preview";
    this.item.command = "maclaw-acp.refreshActiveRemotePreview";

    // Whole-line tint + after-content on the first header line.
    this.decoration = vscode.window.createTextEditorDecorationType({
      isWholeLine: true,
      backgroundColor: new vscode.ThemeColor("editorInfo.background"),
      overviewRulerColor: new vscode.ThemeColor("editorInfo.foreground"),
      overviewRulerLane: vscode.OverviewRulerLane.Left,
      after: {
        margin: "0 0 0 1.5em",
        contentText: "🔒 远端只读预览 · 写入请用 agent 或 Remote-SSH · 点状态栏刷新",
        color: new vscode.ThemeColor("editorCodeLens.foreground"),
        fontStyle: "italic",
      },
    });

    this.subs.push(
      this.item,
      this.decoration,
      vscode.window.onDidChangeActiveTextEditor(() => this.update()),
      vscode.window.onDidChangeVisibleTextEditors(() => this.update()),
      vscode.workspace.onDidOpenTextDocument((doc) => {
        if (doc.uri.scheme === REMOTE_SCHEME) {
          // Defer until an editor is bound to the document.
          setTimeout(() => this.update(), 50);
        }
      })
    );
    this.update();
  }

  dispose(): void {
    for (const d of this.subs) {
      d.dispose();
    }
  }

  update(): void {
    this.updateStatusBar();
    this.updateDecorations();
  }

  private updateStatusBar(): void {
    const ed = vscode.window.activeTextEditor;
    if (!ed || ed.document.uri.scheme !== REMOTE_SCHEME) {
      this.item.hide();
      return;
    }
    const remotePath = uriToRemotePath(ed.document.uri) || ed.document.uri.path;
    this.item.text = "$(lock) 远端只读预览";
    this.item.tooltip = [
      "MaClaw remote preview (read-only)",
      remotePath,
      "点击刷新 · 标题栏可 Diff / 查找",
      "写入请用聊天 agent 或 Remote-SSH",
    ].join("\n");
    this.item.backgroundColor = new vscode.ThemeColor("statusBarItem.warningBackground");
    this.item.show();
  }

  private updateDecorations(): void {
    for (const ed of vscode.window.visibleTextEditors) {
      if (ed.document.uri.scheme !== REMOTE_SCHEME) {
        ed.setDecorations(this.decoration, []);
        continue;
      }
      if (ed.document.lineCount === 0) {
        ed.setDecorations(this.decoration, []);
        continue;
      }
      // Decorate the first line (// remote: … header).
      const range = ed.document.lineAt(0).range;
      ed.setDecorations(this.decoration, [range]);
    }
  }
}
