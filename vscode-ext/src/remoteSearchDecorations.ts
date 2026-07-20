/**
 * Gutter + line highlights for remote search hits on open maclaw-remote:// previews.
 * Also drives "next / previous hit" navigation within the active preview.
 */
import * as vscode from "vscode";
import {
  REMOTE_SCHEME,
  remoteSourceLineToDocLine,
  uriToRemotePath,
} from "./remoteFs";
import type { RemoteSearchHit, RemoteSearchTreeProvider } from "./remoteSearchTree";

export class RemoteSearchDecorations implements vscode.Disposable {
  private readonly lineDeco: vscode.TextEditorDecorationType;
  private readonly matchDeco: vscode.TextEditorDecorationType;
  private readonly subs: vscode.Disposable[] = [];
  /** Per-editor last focused hit index among that file's hits (for next/prev). */
  private readonly focusIdx = new Map<string, number>();
  private updateTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(private readonly searchTree: RemoteSearchTreeProvider) {
    this.lineDeco = vscode.window.createTextEditorDecorationType({
      isWholeLine: true,
      backgroundColor: new vscode.ThemeColor("editor.findMatchHighlightBackground"),
      overviewRulerColor: new vscode.ThemeColor("editor.findMatchHighlightBackground"),
      overviewRulerLane: vscode.OverviewRulerLane.Center,
      borderWidth: "0 0 0 2px",
      borderStyle: "solid",
      borderColor: new vscode.ThemeColor("editorInfo.border"),
    });
    this.matchDeco = vscode.window.createTextEditorDecorationType({
      backgroundColor: new vscode.ThemeColor("editor.findMatchBackground"),
      borderRadius: "2px",
    });

    this.subs.push(
      this.lineDeco,
      this.matchDeco,
      this.searchTree.onDidChangeResults(() => this.scheduleUpdate()),
      vscode.window.onDidChangeActiveTextEditor(() => this.scheduleUpdate()),
      vscode.window.onDidChangeVisibleTextEditors(() => this.scheduleUpdate()),
      vscode.workspace.onDidOpenTextDocument((doc) => {
        if (doc.uri.scheme === REMOTE_SCHEME) {
          this.scheduleUpdate(60);
        }
      }),
      vscode.workspace.onDidChangeTextDocument((e) => {
        // Virtual docs re-fetch after refresh; re-apply once content arrives.
        if (e.document.uri.scheme === REMOTE_SCHEME) {
          this.scheduleUpdate(40);
        }
      })
    );
    this.update();
  }

  dispose(): void {
    if (this.updateTimer !== undefined) {
      clearTimeout(this.updateTimer);
      this.updateTimer = undefined;
    }
    for (const d of this.subs) {
      d.dispose();
    }
    this.focusIdx.clear();
  }

  private scheduleUpdate(delayMs = 40): void {
    if (this.updateTimer !== undefined) {
      clearTimeout(this.updateTimer);
    }
    this.updateTimer = setTimeout(() => {
      this.updateTimer = undefined;
      this.update();
    }, delayMs);
  }

  update(): void {
    const snap = this.searchTree.getSnapshot();
    const hitsByPath = new Map<string, RemoteSearchHit[]>();
    if (snap) {
      for (const h of snap.hits) {
        const list = hitsByPath.get(h.path) ?? [];
        list.push(h);
        hitsByPath.set(h.path, list);
      }
    }

    for (const ed of vscode.window.visibleTextEditors) {
      if (ed.document.uri.scheme !== REMOTE_SCHEME) {
        ed.setDecorations(this.lineDeco, []);
        ed.setDecorations(this.matchDeco, []);
        continue;
      }
      const remotePath = uriToRemotePath(ed.document.uri);
      const hits = remotePath ? hitsByPath.get(remotePath) : undefined;
      if (!hits || hits.length === 0) {
        ed.setDecorations(this.lineDeco, []);
        ed.setDecorations(this.matchDeco, []);
        continue;
      }

      const lineRanges: vscode.DecorationOptions[] = [];
      const matchRanges: vscode.DecorationOptions[] = [];
      const seenLines = new Set<number>();

      for (const h of hits) {
        if (!h.line || h.line < 1) {
          continue;
        }
        const docLine = remoteSourceLineToDocLine(ed.document, h.line);
        if (!seenLines.has(docLine)) {
          seenLines.add(docLine);
          const lineText = ed.document.lineAt(docLine).text;
          lineRanges.push({
            range: ed.document.lineAt(docLine).range,
            hoverMessage: new vscode.MarkdownString(
              `**远端搜索** · L${h.line}\n\n\`${(h.preview || h.text || "").trim().slice(0, 200)}\``
            ),
          });
          // Substring highlight when preview text appears on the line.
          const needle = (h.preview || h.text || "").trim();
          if (needle) {
            // Prefer a short distinctive fragment (first 40 chars).
            const frag = needle.slice(0, 40);
            const idx = lineText.indexOf(frag);
            if (idx >= 0) {
              matchRanges.push({
                range: new vscode.Range(docLine, idx, docLine, idx + frag.length),
              });
            } else {
              // Try first non-space token ≥ 3 chars.
              const token = needle.split(/\s+/).find((t) => t.length >= 3);
              if (token) {
                const ti = lineText.indexOf(token);
                if (ti >= 0) {
                  matchRanges.push({
                    range: new vscode.Range(docLine, ti, docLine, ti + token.length),
                  });
                }
              }
            }
          }
        }
      }

      ed.setDecorations(this.lineDeco, lineRanges);
      ed.setDecorations(this.matchDeco, matchRanges);
    }
  }

  /**
   * Jump to next / previous search hit in the active remote preview.
   * @returns true if navigation happened.
   */
  navigateHit(direction: 1 | -1): boolean {
    const ed = vscode.window.activeTextEditor;
    if (!ed || ed.document.uri.scheme !== REMOTE_SCHEME) {
      void vscode.window.showInformationMessage("MaClaw: 请先打开远端预览");
      return false;
    }
    const remotePath = uriToRemotePath(ed.document.uri);
    const snap = this.searchTree.getSnapshot();
    if (!remotePath || !snap) {
      void vscode.window.showInformationMessage("MaClaw: 当前没有搜索结果");
      return false;
    }
    const hits = snap.hits
      .filter((h) => h.path === remotePath && h.line > 0)
      .sort((a, b) => a.line - b.line);
    if (hits.length === 0) {
      void vscode.window.showInformationMessage("MaClaw: 当前文件无搜索命中");
      return false;
    }

    const key = ed.document.uri.toString();
    const curDocLine = ed.selection.active.line;
    let idx = this.focusIdx.get(key);

    if (idx === undefined) {
      // Seed from caret: next = first hit strictly after caret (wrap);
      // prev = last hit strictly before caret (wrap).
      if (direction > 0) {
        idx = 0;
        for (let i = 0; i < hits.length; i++) {
          if (remoteSourceLineToDocLine(ed.document, hits[i].line) > curDocLine) {
            idx = i;
            break;
          }
        }
      } else {
        idx = hits.length - 1;
        for (let i = hits.length - 1; i >= 0; i--) {
          if (remoteSourceLineToDocLine(ed.document, hits[i].line) < curDocLine) {
            idx = i;
            break;
          }
        }
      }
    } else {
      idx = (idx + direction + hits.length * 10) % hits.length;
    }
    this.focusIdx.set(key, idx);

    const hit = hits[idx];
    const docLine = remoteSourceLineToDocLine(ed.document, hit.line);
    const lineText = ed.document.lineAt(docLine).text;
    let startCol = 0;
    let endCol = lineText.length;
    const needle = (hit.preview || hit.text || "").trim().slice(0, 40);
    if (needle) {
      const i = lineText.indexOf(needle);
      if (i >= 0) {
        startCol = i;
        endCol = i + needle.length;
      }
    }
    const start = new vscode.Position(docLine, startCol);
    const end = new vscode.Position(docLine, endCol);
    ed.selection = new vscode.Selection(start, end);
    ed.revealRange(new vscode.Range(start, end), vscode.TextEditorRevealType.InCenter);
    void vscode.window.setStatusBarMessage(
      `MaClaw 搜索 · ${idx + 1}/${hits.length} · L${hit.line}`,
      2500
    );
    return true;
  }
}
