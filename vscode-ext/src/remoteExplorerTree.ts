/**
 * Lazy remote work_dir directory tree (maclaw-acp.remoteExplorer).
 * Expands via maclaw/list_remote_dir; open files as maclaw-remote:// previews.
 */
import * as path from "path";
import * as vscode from "vscode";
import {
  parseLsListing,
  remotePathToUri,
  type RemoteLsEntry,
} from "./remoteFs";

export interface RemoteExplorerHost {
  isRemoteAttached(): boolean;
  getRemoteWorkDir(): string;
  getAttachedProjectPath(): string;
  listRemoteDir(dirPath: string): Promise<{ path: string; listing: string } | undefined>;
}

type TreeNode = RootHintNode | DirNode | FileNode | ErrorNode | LoadingNode;

class RootHintNode extends vscode.TreeItem {
  constructor(message: string) {
    super(message, vscode.TreeItemCollapsibleState.None);
    this.contextValue = "hint";
  }
}

class ErrorNode extends vscode.TreeItem {
  constructor(message: string) {
    super(message, vscode.TreeItemCollapsibleState.None);
    this.contextValue = "error";
    this.iconPath = new vscode.ThemeIcon("error");
  }
}

class LoadingNode extends vscode.TreeItem {
  constructor() {
    super("加载中…", vscode.TreeItemCollapsibleState.None);
    this.contextValue = "loading";
  }
}

class DirNode extends vscode.TreeItem {
  readonly remotePath: string;
  constructor(remotePath: string, name?: string) {
    super(
      name || path.posix.basename(remotePath) || remotePath,
      vscode.TreeItemCollapsibleState.Collapsed
    );
    this.remotePath = remotePath;
    this.tooltip = remotePath;
    this.contextValue = "remoteDir";
    this.iconPath = new vscode.ThemeIcon("folder");
    this.resourceUri = remotePathToUri(remotePath);
  }
}

class FileNode extends vscode.TreeItem {
  readonly remotePath: string;
  constructor(remotePath: string, name?: string) {
    super(name || path.posix.basename(remotePath), vscode.TreeItemCollapsibleState.None);
    this.remotePath = remotePath;
    this.tooltip = remotePath;
    this.contextValue = "remoteFile";
    this.iconPath = new vscode.ThemeIcon("file");
    this.resourceUri = remotePathToUri(remotePath);
    this.command = {
      command: "maclaw-acp.openSearchHit",
      title: "Open preview",
      arguments: [remotePath],
    };
  }
}

export class RemoteExplorerTreeProvider
  implements vscode.TreeDataProvider<TreeNode>, vscode.Disposable
{
  private readonly _onDidChange = new vscode.EventEmitter<TreeNode | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;
  /** Cache of dir path → entries (or error string). */
  private readonly cache = new Map<string, RemoteLsEntry[] | string>();
  private readonly configSub: vscode.Disposable;
  /**
   * Session-only name filter (not persisted — temporary UX; hideDotfiles stays
   * in settings because users typically want that sticky).
   */
  private nameFilter = "";

  constructor(private readonly host: RemoteExplorerHost) {
    this.configSub = vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("maclaw-acp.remoteExplorer.hideDotfiles")) {
        // Filter is client-side; no need to re-list over SSH, just re-render.
        this._onDidChange.fire();
      }
    });
  }

  dispose(): void {
    this.configSub.dispose();
    this._onDidChange.dispose();
    this.cache.clear();
  }

  refresh(dirPath?: string): void {
    if (dirPath) {
      this.cache.delete(dirPath);
    } else {
      this.cache.clear();
    }
    this._onDidChange.fire();
  }

  getHideDotfiles(): boolean {
    return vscode.workspace
      .getConfiguration("maclaw-acp")
      .get<boolean>("remoteExplorer.hideDotfiles", true);
  }

  async toggleHideDotfiles(): Promise<boolean> {
    const cfg = vscode.workspace.getConfiguration("maclaw-acp");
    const next = !cfg.get<boolean>("remoteExplorer.hideDotfiles", true);
    await cfg.update("remoteExplorer.hideDotfiles", next, vscode.ConfigurationTarget.Global);
    this._onDidChange.fire();
    return next;
  }

  getNameFilter(): string {
    return this.nameFilter.trim();
  }

  setNameFilter(filter: string): void {
    this.nameFilter = filter.trim();
    this._onDidChange.fire();
  }

  private filterEntries(entries: RemoteLsEntry[]): RemoteLsEntry[] {
    const hideDot = this.getHideDotfiles();
    const filter = this.getNameFilter().toLowerCase();
    return entries.filter((e) => {
      if (hideDot && e.name.startsWith(".")) {
        return false;
      }
      if (filter && !e.name.toLowerCase().includes(filter)) {
        return false;
      }
      return true;
    });
  }

  getTreeItem(element: TreeNode): vscode.TreeItem {
    return element;
  }

  async getChildren(element?: TreeNode): Promise<TreeNode[]> {
    if (!this.host.isRemoteAttached()) {
      return [new RootHintNode("附着远程任务后浏览 work_dir")];
    }
    const root = (this.host.getRemoteWorkDir() || "").trim();
    if (!root) {
      return [new RootHintNode("任务缺少 work_dir 元数据")];
    }
    if (!this.host.getAttachedProjectPath()) {
      return [new RootHintNode("未附着 project path")];
    }

    const dirPath =
      element instanceof DirNode ? element.remotePath : root.replace(/\/+$/, "") || "/";

    // Root level: show the work_dir itself as expandable folder when first open.
    if (!element) {
      return [new DirNode(dirPath, path.posix.basename(dirPath) || dirPath)];
    }

    if (!(element instanceof DirNode)) {
      return [];
    }

    let cached = this.cache.get(dirPath);
    if (cached === undefined) {
      try {
        const res = await this.host.listRemoteDir(dirPath);
        if (!res) {
          this.cache.set(dirPath, "无法列出目录（bridge / SSH）");
        } else {
          const entries = parseLsListing(res.listing, res.path || dirPath);
          // Sort: dirs first, then files; alpha.
          entries.sort((a, b) => {
            if (a.kind === "dir" && b.kind !== "dir") {
              return -1;
            }
            if (a.kind !== "dir" && b.kind === "dir") {
              return 1;
            }
            return a.name.localeCompare(b.name);
          });
          this.cache.set(dirPath, entries);
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        this.cache.set(dirPath, msg);
      }
      cached = this.cache.get(dirPath);
    }

    if (typeof cached === "string") {
      return [new ErrorNode(cached)];
    }
    if (!cached || cached.length === 0) {
      return [new RootHintNode("(空目录)")];
    }

    const filtered = this.filterEntries(cached);
    if (filtered.length === 0) {
      const hints: string[] = [];
      if (this.getHideDotfiles()) {
        hints.push("已隐藏点文件");
      }
      if (this.getNameFilter()) {
        hints.push(`过滤: ${this.getNameFilter()}`);
      }
      return [
        new RootHintNode(
          hints.length > 0
            ? `(无可见项 · ${hints.join(" · ")})`
            : "(无可见项)"
        ),
      ];
    }

    const nodes: TreeNode[] = [];
    for (const e of filtered) {
      if (e.kind === "dir") {
        nodes.push(new DirNode(e.path, e.name));
      } else {
        // files + links + other → open as preview when possible
        nodes.push(new FileNode(e.path, e.name));
      }
    }
    return nodes;
  }
}
