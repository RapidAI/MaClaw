/**
 * Tree view for remote work_dir search hits (maclaw-acp.searchResults).
 * Click a hit → open remote preview at line.
 */
import * as path from "path";
import * as vscode from "vscode";

export interface RemoteSearchHit {
  path: string;
  line: number;
  text: string;
  preview: string;
}

export interface RemoteSearchSnapshot {
  query: string;
  scope: string;
  workDir: string;
  hits: RemoteSearchHit[];
  truncated: boolean;
  at: number;
}

type TreeNode = FileNode | HitNode | EmptyNode | HeaderNode;

class HeaderNode extends vscode.TreeItem {
  constructor(label: string, desc?: string) {
    super(label, vscode.TreeItemCollapsibleState.None);
    this.description = desc;
    this.contextValue = "header";
    this.iconPath = new vscode.ThemeIcon("info");
  }
}

class EmptyNode extends vscode.TreeItem {
  constructor(message: string) {
    super(message, vscode.TreeItemCollapsibleState.None);
    this.contextValue = "empty";
  }
}

class FileNode extends vscode.TreeItem {
  readonly hits: RemoteSearchHit[];
  constructor(filePath: string, hits: RemoteSearchHit[]) {
    super(path.posix.basename(filePath), vscode.TreeItemCollapsibleState.Expanded);
    this.hits = hits;
    this.description = `${hits.length} hit${hits.length === 1 ? "" : "s"}`;
    this.tooltip = filePath;
    this.contextValue = "remoteSearchFile";
    this.iconPath = new vscode.ThemeIcon("file");
    this.resourceUri = vscode.Uri.parse(`maclaw-remote:${filePath}`);
  }
}

class HitNode extends vscode.TreeItem {
  readonly hit: RemoteSearchHit;
  constructor(hit: RemoteSearchHit) {
    super(`L${hit.line}`, vscode.TreeItemCollapsibleState.None);
    this.hit = hit;
    this.description = (hit.preview || hit.text || "").trim().slice(0, 100);
    this.tooltip = `${hit.path}:${hit.line}\n${hit.text}`;
    this.contextValue = "remoteSearchHit";
    this.iconPath = new vscode.ThemeIcon("search");
    this.command = {
      command: "maclaw-acp.openSearchHit",
      title: "Open hit",
      arguments: [hit.path, hit.line, hit.preview || hit.text],
    };
  }
}

export class RemoteSearchTreeProvider
  implements vscode.TreeDataProvider<TreeNode>, vscode.Disposable
{
  private readonly _onDidChange = new vscode.EventEmitter<TreeNode | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;
  /** Fired whenever the search snapshot is replaced or cleared (for decorations). */
  private readonly _onDidChangeResults = new vscode.EventEmitter<RemoteSearchSnapshot | undefined>();
  readonly onDidChangeResults = this._onDidChangeResults.event;
  private snapshot: RemoteSearchSnapshot | undefined;

  dispose(): void {
    this._onDidChange.dispose();
    this._onDidChangeResults.dispose();
  }

  setResults(snap: RemoteSearchSnapshot | undefined): void {
    this.snapshot = snap;
    this._onDidChange.fire();
    this._onDidChangeResults.fire(snap);
  }

  clear(): void {
    this.snapshot = undefined;
    this._onDidChange.fire();
    this._onDidChangeResults.fire(undefined);
  }

  getSnapshot(): RemoteSearchSnapshot | undefined {
    return this.snapshot;
  }

  /** Markdown export for sharing / notes. */
  toMarkdown(): string {
    const s = this.snapshot;
    if (!s) {
      return "# MaClaw remote search\n\n_(no results)_\n";
    }
    const lines: string[] = [
      `# MaClaw remote search`,
      ``,
      `- **query:** \`${s.query.replace(/`/g, "\\`")}\``,
      `- **scope:** \`${s.scope}\``,
      `- **work_dir:** \`${s.workDir}\``,
      `- **hits:** ${s.hits.length}${s.truncated ? "+" : ""}`,
      `- **at:** ${new Date(s.at).toISOString()}`,
      ``,
    ];
    let cur = "";
    for (const h of s.hits) {
      if (h.path !== cur) {
        cur = h.path;
        lines.push(`## \`${h.path}\``);
        lines.push("");
      }
      const prev = (h.preview || h.text || "").replace(/\s+/g, " ").trim();
      lines.push(`- **L${h.line}:** ${prev}`);
    }
    lines.push("");
    return lines.join("\n");
  }

  /** JSON export. */
  toJSON(): string {
    return JSON.stringify(this.snapshot ?? { hits: [] }, null, 2) + "\n";
  }

  getTreeItem(element: TreeNode): vscode.TreeItem {
    return element;
  }

  getChildren(element?: TreeNode): TreeNode[] {
    if (!this.snapshot) {
      return [
        new EmptyNode("运行「远端搜索」后在此显示结果"),
      ];
    }
    if (!element) {
      const roots: TreeNode[] = [
        new HeaderNode(
          `“${this.snapshot.query}”`,
          `${this.snapshot.hits.length}${this.snapshot.truncated ? "+" : ""} · ${this.snapshot.scope}`
        ),
      ];
      if (this.snapshot.hits.length === 0) {
        roots.push(new EmptyNode("无匹配"));
        return roots;
      }
      // Group by path, preserve first-seen order.
      const byFile = new Map<string, RemoteSearchHit[]>();
      for (const h of this.snapshot.hits) {
        const list = byFile.get(h.path) ?? [];
        list.push(h);
        byFile.set(h.path, list);
      }
      for (const [filePath, hits] of byFile) {
        roots.push(new FileNode(filePath, hits));
      }
      return roots;
    }
    if (element instanceof FileNode) {
      return element.hits.map((h) => new HitNode(h));
    }
    return [];
  }
}
