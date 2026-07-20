import * as vscode from "vscode";
import { ChatViewProvider } from "./chatViewProvider";
import { LauncherViewProvider } from "./launcherViewProvider";
import { resolveBridgePath } from "./bridgeResolver";
import { RemoteSearchTreeProvider } from "./remoteSearchTree";
import { RemotePreviewStatusBar } from "./remotePreviewStatus";
import { RemoteSearchDecorations } from "./remoteSearchDecorations";
import { AgentChangeTreeProvider } from "./agentChangeTree";
import { RemoteExplorerTreeProvider } from "./remoteExplorerTree";

// eslint-disable-next-line @typescript-eslint/no-var-requires
const pkg = require("../package.json") as { version: string };

export function activate(context: vscode.ExtensionContext): void {
  const provider = new ChatViewProvider(context, pkg.version);
  provider.registerRemoteFs(context);

  const searchTree = new RemoteSearchTreeProvider();
  provider.setSearchTree(searchTree);
  const searchView = vscode.window.createTreeView("maclaw-acp.searchResults", {
    treeDataProvider: searchTree,
    showCollapseAll: true,
  });

  const changeTree = new AgentChangeTreeProvider();
  provider.setChangeTree(changeTree);
  const changeView = vscode.window.createTreeView("maclaw-acp.agentChanges", {
    treeDataProvider: changeTree,
    showCollapseAll: false,
  });

  const explorerTree = new RemoteExplorerTreeProvider({
    isRemoteAttached: () => provider.isRemoteAttached(),
    getRemoteWorkDir: () => provider.getRemoteWorkDir(),
    getAttachedProjectPath: () => provider.getAttachedProjectPath(),
    listRemoteDir: (dir) => provider.listRemoteDirForExplorer(dir),
  });
  provider.setExplorerTree(explorerTree);
  const explorerView = vscode.window.createTreeView("maclaw-acp.remoteExplorer", {
    treeDataProvider: explorerTree,
    showCollapseAll: true,
  });

  const previewStatus = new RemotePreviewStatusBar();
  const searchDecorations = new RemoteSearchDecorations(searchTree);
  const launcher = new LauncherViewProvider(context, provider);

  context.subscriptions.push(
    provider,
    launcher,
    searchTree,
    searchView,
    changeTree,
    changeView,
    explorerTree,
    explorerView,
    previewStatus,
    searchDecorations,
    vscode.window.registerWebviewViewProvider(ChatViewProvider.viewType, provider, {
      webviewOptions: { retainContextWhenHidden: true },
    }),
    vscode.window.registerWebviewViewProvider(LauncherViewProvider.viewType, launcher, {
      webviewOptions: { retainContextWhenHidden: true },
    }),
    vscode.commands.registerCommand("maclaw-acp.openChat", () =>
      vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`)
    ),
    vscode.commands.registerCommand("maclaw-acp.newSession", () => provider.newSession()),
    vscode.commands.registerCommand("maclaw-acp.cancelTurn", () => provider.cancelTurn()),
    vscode.commands.registerCommand("maclaw-acp.refreshRemoteTasks", () =>
      launcher.refreshRemoteTasks()
    ),
    vscode.commands.registerCommand("maclaw-acp.detachRemote", () => provider.detachRemoteTask()),
    vscode.commands.registerCommand("maclaw-acp.openRemoteTaskFolder", () =>
      provider.openAttachedTaskFolder()
    ),
    vscode.commands.registerCommand("maclaw-acp.openRemotePath", async () => {
      const input = await vscode.window.showInputBox({
        title: "打开远端路径预览",
        prompt: "输入远端绝对路径或相对 work_dir 的路径",
        placeHolder: "/home/project/main.go",
        ignoreFocusOut: true,
      });
      if (input?.trim()) {
        await provider.openRemotePreview(input.trim());
      }
    }),
    vscode.commands.registerCommand("maclaw-acp.listRemoteWorkDir", () =>
      provider.openRemoteDirListing()
    ),
    vscode.commands.registerCommand("maclaw-acp.refreshRemotePreviews", () => {
      const n = provider.refreshOpenRemotePreviews();
      void vscode.window.showInformationMessage(
        n > 0 ? `MaClaw: 已刷新 ${n} 个远端预览` : "MaClaw: 没有打开的远端预览"
      );
    }),
    vscode.commands.registerCommand("maclaw-acp.openInRemoteSSH", () =>
      provider.openInRemoteSSH()
    ),
    vscode.commands.registerCommand("maclaw-acp.refreshActiveRemotePreview", () => {
      const ed = vscode.window.activeTextEditor;
      if (!ed || ed.document.uri.scheme !== "maclaw-remote") {
        void vscode.window.showInformationMessage("MaClaw: 当前不是远端预览标签");
        return;
      }
      const n = provider.refreshOpenRemotePreviews();
      void vscode.window.showInformationMessage(
        n > 0 ? `MaClaw: 已刷新预览` : "MaClaw: 刷新失败"
      );
    }),
    vscode.commands.registerCommand("maclaw-acp.diffRemoteWithLocal", () =>
      provider.diffRemoteWithLocal()
    ),
    vscode.commands.registerCommand("maclaw-acp.searchRemote", () =>
      provider.searchRemoteAndOpen()
    ),
    vscode.commands.registerCommand("maclaw-acp.findInRemotePreview", () =>
      provider.findInActiveRemotePreview()
    ),
    vscode.commands.registerCommand(
      "maclaw-acp.openSearchHit",
      async (remotePath?: string, line?: number, highlight?: string) => {
        if (!remotePath) {
          return;
        }
        await provider.openRemotePreview(String(remotePath), {
          line: typeof line === "number" ? line : undefined,
          highlight: typeof highlight === "string" ? highlight : undefined,
        });
      }
    ),
    vscode.commands.registerCommand("maclaw-acp.clearSearchResults", () => {
      searchTree.clear();
      void vscode.window.showInformationMessage("MaClaw: 已清空搜索结果树");
    }),
    vscode.commands.registerCommand("maclaw-acp.exportSearchResults", async () => {
      const snap = searchTree.getSnapshot();
      if (!snap || snap.hits.length === 0) {
        void vscode.window.showInformationMessage("MaClaw: 没有可导出的搜索结果");
        return;
      }
      const fmt = await vscode.window.showQuickPick(
        [
          { label: "Markdown", id: "md" as const },
          { label: "JSON", id: "json" as const },
        ],
        { title: "导出远端搜索结果" }
      );
      if (!fmt) {
        return;
      }
      const content = fmt.id === "json" ? searchTree.toJSON() : searchTree.toMarkdown();
      const lang = fmt.id === "json" ? "json" : "markdown";
      const doc = await vscode.workspace.openTextDocument({ content, language: lang });
      await vscode.window.showTextDocument(doc, { preview: false });
      const save = await vscode.window.showInformationMessage(
        `MaClaw: 已打开导出内容（${snap.hits.length} hits）`,
        "另存为…"
      );
      if (save === "另存为…") {
        const defaultName =
          fmt.id === "json"
            ? `maclaw-remote-search-${Date.now()}.json`
            : `maclaw-remote-search-${Date.now()}.md`;
        const uri = await vscode.window.showSaveDialog({
          defaultUri: vscode.Uri.file(defaultName),
          filters:
            fmt.id === "json"
              ? { JSON: ["json"] }
              : { Markdown: ["md"] },
        });
        if (uri) {
          await vscode.workspace.fs.writeFile(uri, Buffer.from(content, "utf8"));
          void vscode.window.showInformationMessage(`MaClaw: 已保存 ${uri.fsPath}`);
        }
      }
    }),
    vscode.commands.registerCommand("maclaw-acp.nextSearchHit", () => {
      searchDecorations.navigateHit(1);
    }),
    vscode.commands.registerCommand("maclaw-acp.prevSearchHit", () => {
      searchDecorations.navigateHit(-1);
    }),
    vscode.commands.registerCommand("maclaw-acp.copyRemotePath", () =>
      provider.copyActiveRemotePath()
    ),
    vscode.commands.registerCommand("maclaw-acp.copyRemoteRelativePath", () =>
      provider.copyActiveRemotePath({ relative: true })
    ),
    vscode.commands.registerCommand("maclaw-acp.openRecentRemote", () =>
      provider.openRecentRemoteFile()
    ),
    vscode.commands.registerCommand("maclaw-acp.clearAgentChanges", () => {
      changeTree.clear();
      void vscode.window.showInformationMessage("MaClaw: 已清空 agent 改动列表");
    }),
    vscode.commands.registerCommand("maclaw-acp.exportAgentChanges", async () => {
      const entries = changeTree.getEntries();
      if (entries.length === 0) {
        void vscode.window.showInformationMessage("MaClaw: 没有可导出的 agent 改动");
        return;
      }
      const fmt = await vscode.window.showQuickPick(
        [
          { label: "Markdown", id: "md" as const },
          { label: "JSON", id: "json" as const },
        ],
        { title: "导出 Agent Changes" }
      );
      if (!fmt) {
        return;
      }
      const meta = {
        workDir: provider.getRemoteWorkDir(),
        remoteLabel: provider.getStatusSnapshot().remoteLabel,
      };
      const content =
        fmt.id === "json" ? changeTree.toJSON(meta) : changeTree.toMarkdown(meta);
      const lang = fmt.id === "json" ? "json" : "markdown";
      const doc = await vscode.workspace.openTextDocument({ content, language: lang });
      await vscode.window.showTextDocument(doc, { preview: false });
      const save = await vscode.window.showInformationMessage(
        `MaClaw: 已打开导出（${entries.length} files）`,
        "另存为…"
      );
      if (save === "另存为…") {
        const defaultName =
          fmt.id === "json"
            ? `maclaw-agent-changes-${Date.now()}.json`
            : `maclaw-agent-changes-${Date.now()}.md`;
        const uri = await vscode.window.showSaveDialog({
          defaultUri: vscode.Uri.file(defaultName),
          filters: fmt.id === "json" ? { JSON: ["json"] } : { Markdown: ["md"] },
        });
        if (uri) {
          await vscode.workspace.fs.writeFile(uri, Buffer.from(content, "utf8"));
          void vscode.window.showInformationMessage(`MaClaw: 已保存 ${uri.fsPath}`);
        }
      }
    }),
    vscode.commands.registerCommand("maclaw-acp.openAllAgentChanges", async () => {
      const entries = changeTree.getEntries();
      if (entries.length === 0) {
        void vscode.window.showInformationMessage("MaClaw: 没有 agent 改动文件");
        return;
      }
      const cap = entries.slice(0, 10);
      for (const e of cap) {
        await provider.openRemotePreview(e.path);
      }
      if (entries.length > 10) {
        void vscode.window.showInformationMessage(
          `MaClaw: 已打开 10 个（共 ${entries.length}）`
        );
      }
    }),
    vscode.commands.registerCommand("maclaw-acp.diffAgentChange", async (item?: unknown) => {
      const entry =
        item && typeof item === "object" && "entry" in item
          ? (item as { entry: { path: string } }).entry
          : undefined;
      const p = entry?.path;
      if (p) {
        await provider.diffRemoteWithLocal(p);
      } else {
        await provider.diffRemoteWithLocal();
      }
    }),
    vscode.commands.registerCommand("maclaw-acp.refreshRemoteExplorer", () => {
      explorerTree.refresh();
      void vscode.window.showInformationMessage("MaClaw: 已刷新远端目录树");
    }),
    vscode.commands.registerCommand("maclaw-acp.toggleRemoteDotfiles", async () => {
      const hidden = await explorerTree.toggleHideDotfiles();
      void vscode.window.showInformationMessage(
        hidden
          ? "MaClaw: 已隐藏点文件（.git / .env …）"
          : "MaClaw: 已显示点文件"
      );
    }),
    vscode.commands.registerCommand("maclaw-acp.filterRemoteExplorer", async () => {
      const current = explorerTree.getNameFilter();
      const input = await vscode.window.showInputBox({
        title: "过滤 Remote Explorer",
        prompt: "按文件名包含过滤（会话内临时，清空则取消）",
        value: current,
        placeHolder: "例如 .cpp 或 main",
        ignoreFocusOut: true,
      });
      if (input === undefined) {
        return;
      }
      explorerTree.setNameFilter(input.trim());
      void vscode.window.showInformationMessage(
        input.trim()
          ? `MaClaw: 过滤「${input.trim()}」`
          : "MaClaw: 已清除名称过滤"
      );
    }),
    vscode.commands.registerCommand(
      "maclaw-acp.refreshRemoteExplorerDir",
      (item?: unknown) => {
        const remotePath =
          item && typeof item === "object" && "remotePath" in item
            ? String((item as { remotePath: string }).remotePath)
            : undefined;
        explorerTree.refresh(remotePath);
      }
    ),
    vscode.commands.registerCommand(
      "maclaw-acp.searchInRemoteDir",
      async (item?: unknown) => {
        const remotePath =
          item && typeof item === "object" && "remotePath" in item
            ? String((item as { remotePath: string }).remotePath)
            : undefined;
        await provider.searchRemoteAndOpen(remotePath);
      }
    )
  );

  // One-time welcome: right after the launcher installs the extension, open
  // the chat so the user sees where it lives. Never pops up again afterwards.
  const welcomed = context.globalState.get<boolean>("maclaw-acp.welcomed");
  if (!welcomed) {
    void context.globalState.update("maclaw-acp.welcomed", true);
    if (resolveBridgePath()) {
      setTimeout(() => {
        void vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`);
      }, 1500);
    }
  }

  // Quietly refresh remote task list when bridge is available (no toasts).
  if (resolveBridgePath()) {
    setTimeout(() => {
      void launcher.refreshRemoteTasks({ quiet: true });
    }, 2500);
  }
}

export function deactivate(): void {
  /* provider.dispose() runs via subscriptions */
}
