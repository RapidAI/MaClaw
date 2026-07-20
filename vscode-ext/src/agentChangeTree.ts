/**
 * Sidebar tree of files the agent touched this session (edit/delete/move).
 * Populated from ACP tool_call / tool_call_update locations + File change cards.
 */
import * as path from "path";
import * as vscode from "vscode";
import { remotePathToUri } from "./remoteFs";

export type AgentChangeKind = "edit" | "delete" | "move" | "other";

export interface AgentChangeEntry {
  path: string;
  kind: AgentChangeKind;
  title: string;
  status: string;
  at: number;
  /** Turn generation when first seen (for "this turn" badges). */
  turn: number;
}

type TreeNode = HeaderNode | ChangeNode | EmptyNode;

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

class ChangeNode extends vscode.TreeItem {
  readonly entry: AgentChangeEntry;
  constructor(entry: AgentChangeEntry, currentTurn: number) {
    super(path.posix.basename(entry.path), vscode.TreeItemCollapsibleState.None);
    this.entry = entry;
    const thisTurn = entry.turn === currentTurn && currentTurn > 0;
    this.description = [
      entry.kind,
      thisTurn ? "本回合" : undefined,
      entry.status && entry.status !== "completed" ? entry.status : undefined,
    ]
      .filter(Boolean)
      .join(" · ");
    this.tooltip = `${entry.path}\n${entry.title}\n${new Date(entry.at).toLocaleString()}`;
    this.contextValue = "agentChange";
    this.iconPath = new vscode.ThemeIcon(
      entry.kind === "delete"
        ? "trash"
        : entry.kind === "move"
          ? "arrow-right"
          : "diff"
    );
    this.resourceUri = remotePathToUri(entry.path);
    this.command = {
      command: "maclaw-acp.openSearchHit",
      title: "Open",
      arguments: [entry.path],
    };
  }
}

export class AgentChangeTreeProvider
  implements vscode.TreeDataProvider<TreeNode>, vscode.Disposable
{
  private readonly _onDidChange = new vscode.EventEmitter<TreeNode | undefined | void>();
  readonly onDidChangeTreeData = this._onDidChange.event;
  private readonly byPath = new Map<string, AgentChangeEntry>();
  private currentTurn = 0;
  private mode: "local" | "remote" = "local";
  /** Remote work_dir for resolving relative File change paths. */
  private workDir = "";
  private static readonly maxEntries = 200;
  /** Coalesce rapid note() calls into one tree refresh. */
  private fireTimer: ReturnType<typeof setTimeout> | undefined;

  dispose(): void {
    if (this.fireTimer !== undefined) {
      clearTimeout(this.fireTimer);
      this.fireTimer = undefined;
    }
    this._onDidChange.dispose();
  }

  setMode(mode: "local" | "remote"): void {
    this.mode = mode;
    this.scheduleFire();
  }

  setWorkDir(workDir: string): void {
    this.workDir = (workDir || "").replace(/\\/g, "/").replace(/\/+$/, "");
  }

  /** Call when a user prompt turn starts. */
  beginTurn(turn: number): void {
    this.currentTurn = turn;
    this.scheduleFire();
  }

  clear(): void {
    this.byPath.clear();
    this.scheduleFire(true);
  }

  private scheduleFire(immediate = false): void {
    if (immediate) {
      if (this.fireTimer !== undefined) {
        clearTimeout(this.fireTimer);
        this.fireTimer = undefined;
      }
      this._onDidChange.fire();
      return;
    }
    if (this.fireTimer !== undefined) {
      return;
    }
    this.fireTimer = setTimeout(() => {
      this.fireTimer = undefined;
      this._onDidChange.fire();
    }, 80);
  }

  getEntries(): AgentChangeEntry[] {
    return [...this.byPath.values()].sort((a, b) => b.at - a.at);
  }

  /** Paths changed in the latest turn only. */
  getThisTurnPaths(): string[] {
    return this.getEntries()
      .filter((e) => e.turn === this.currentTurn && this.currentTurn > 0)
      .map((e) => e.path);
  }

  /** Markdown export for notes / PR body. */
  toMarkdown(opts?: { workDir?: string; remoteLabel?: string }): string {
    const entries = this.getEntries();
    const lines: string[] = [
      `# MaClaw agent changes`,
      ``,
      `- **files:** ${entries.length}`,
      `- **mode:** ${this.mode}`,
      `- **at:** ${new Date().toISOString()}`,
    ];
    if (opts?.remoteLabel) {
      lines.push(`- **remote:** \`${opts.remoteLabel}\``);
    }
    if (opts?.workDir) {
      lines.push(`- **work_dir:** \`${opts.workDir}\``);
    }
    lines.push(``);
    if (entries.length === 0) {
      lines.push(`_(no changes)_`, ``);
      return lines.join("\n");
    }
    lines.push(`| Kind | Path | When | Status |`);
    lines.push(`|------|------|------|--------|`);
    for (const e of entries) {
      const when = new Date(e.at).toISOString();
      const thisTurn =
        e.turn === this.currentTurn && this.currentTurn > 0 ? " · 本回合" : "";
      lines.push(
        `| ${e.kind}${thisTurn} | \`${e.path.replace(/`/g, "\\`")}\` | ${when} | ${e.status || "—"} |`
      );
    }
    lines.push(``);
    lines.push(`## Paths`);
    lines.push(``);
    for (const e of entries) {
      lines.push(`- \`${e.path.replace(/`/g, "\\`")}\` (${e.kind})`);
    }
    lines.push(``);
    return lines.join("\n");
  }

  /** JSON export. */
  toJSON(opts?: { workDir?: string; remoteLabel?: string }): string {
    return (
      JSON.stringify(
        {
          mode: this.mode,
          workDir: opts?.workDir ?? "",
          remoteLabel: opts?.remoteLabel ?? "",
          at: Date.now(),
          entries: this.getEntries(),
        },
        null,
        2
      ) + "\n"
    );
  }

  /**
   * Ingest an ACP session/update payload. Returns true when a new path was recorded.
   */
  ingestUpdate(update: Record<string, unknown>): boolean {
    const su = String(update.sessionUpdate ?? "");
    if (su !== "tool_call" && su !== "tool_call_update") {
      // Also harvest "### File change: `path`" cards from message content.
      return this.ingestFileChangeCards(update);
    }

    const kind = normalizeKind(String(update.kind ?? ""));
    const title = typeof update.title === "string" ? update.title : "";
    const status = typeof update.status === "string" ? update.status : "";

    // Ignore pure read/search/execute for the change list (still harvest diff cards).
    if (kind === "read" || kind === "search" || kind === "execute") {
      return this.ingestFileChangeCards(update);
    }

    const paths = collectPaths(update);
    const looksLikeWrite =
      kind === "edit" ||
      kind === "delete" ||
      kind === "move" ||
      /write_file|edit_file|apply_patch|str_replace|create_file|delete_file|remove_file|move_file|rename_file/i.test(
        title
      );

    let changed = false;
    if (looksLikeWrite && paths.length > 0) {
      const effectiveKind: AgentChangeKind =
        kind === "delete" || kind === "move" || kind === "edit"
          ? kind
          : /delete|remove/i.test(title)
            ? "delete"
            : /move|rename/i.test(title)
              ? "move"
              : "edit";
      for (const p of paths) {
        if (this.note(p, effectiveKind, title, status)) {
          changed = true;
        }
      }
    }
    if (this.ingestFileChangeCards(update)) {
      changed = true;
    }
    return changed;
  }

  private ingestFileChangeCards(update: Record<string, unknown>): boolean {
    const texts: string[] = [];
    const content = update.content;
    if (Array.isArray(content)) {
      for (const block of content) {
        if (block && typeof block === "object") {
          const t = (block as { text?: string }).text;
          if (typeof t === "string") {
            texts.push(t);
          }
        }
      }
    }
    // agent_message_chunk shape
    if (update.content && typeof update.content === "object" && !Array.isArray(update.content)) {
      const t = (update.content as { text?: string }).text;
      if (typeof t === "string") {
        texts.push(t);
      }
    }
    let changed = false;
    for (const text of texts) {
      // ### File change: `path`  or  ### File change: path
      const re = /###\s*File change:\s*`?([^\n`]+)`?/gi;
      let m: RegExpExecArray | null;
      while ((m = re.exec(text)) !== null) {
        const p = m[1].trim();
        if (p && this.note(p, "edit", "File change", "completed")) {
          changed = true;
        }
      }
    }
    return changed;
  }

  private note(
    rawPath: string,
    kind: AgentChangeKind,
    title: string,
    status: string
  ): boolean {
    const p = canonicalizePath(rawPath, this.workDir);
    if (!p) {
      return false;
    }
    // Skip obvious non-file cwd/dir-only noise when path is "."
    if (p === "." || p === "./") {
      return false;
    }
    // Dedupe: if we already have abs path and this is the same file under workDir.
    const prev = this.byPath.get(p) ?? this.findAlias(p);
    // Drop alias key if we are upgrading relative → absolute.
    if (prev && prev.path !== p) {
      this.byPath.delete(prev.path);
    }
    const entry: AgentChangeEntry = {
      path: p,
      kind: kind === "other" && prev ? prev.kind : kind,
      title: title || prev?.title || kind,
      status: status || prev?.status || "",
      at: Date.now(),
      turn: this.currentTurn || prev?.turn || 0,
    };
    // Keep first-seen turn for "本回合" while updating status/title.
    if (prev && prev.turn > 0 && (this.currentTurn === prev.turn || this.currentTurn === 0)) {
      entry.turn = prev.turn;
    }
    const meaningful =
      !prev ||
      prev.path !== entry.path ||
      prev.kind !== entry.kind ||
      prev.status !== entry.status ||
      prev.title !== entry.title;
    this.byPath.set(p, entry);
    this.trimToCap();
    if (meaningful) {
      this.scheduleFire();
    }
    return meaningful;
  }

  /** Find an existing entry that maps to the same file under workDir. */
  private findAlias(absOrRel: string): AgentChangeEntry | undefined {
    if (!this.workDir) {
      return undefined;
    }
    const wd = this.workDir;
    for (const e of this.byPath.values()) {
      if (e.path === absOrRel) {
        return e;
      }
      // relative vs absolute under work_dir
      if (absOrRel.startsWith(wd + "/") && e.path === absOrRel.slice(wd.length + 1)) {
        return e;
      }
      if (e.path.startsWith(wd + "/") && absOrRel === e.path.slice(wd.length + 1)) {
        return e;
      }
    }
    return undefined;
  }

  private trimToCap(): void {
    if (this.byPath.size <= AgentChangeTreeProvider.maxEntries) {
      return;
    }
    const sorted = [...this.byPath.values()].sort((a, b) => a.at - b.at);
    const drop = sorted.length - AgentChangeTreeProvider.maxEntries;
    for (let i = 0; i < drop; i++) {
      this.byPath.delete(sorted[i].path);
    }
  }

  getTreeItem(element: TreeNode): vscode.TreeItem {
    return element;
  }

  getChildren(element?: TreeNode): TreeNode[] {
    if (element) {
      return [];
    }
    if (this.mode !== "remote") {
      return [new EmptyNode("附着远程任务后，agent 改动的文件会出现在这里")];
    }
    const entries = this.getEntries();
    if (entries.length === 0) {
      return [new EmptyNode("本会话尚无文件改动（edit/delete）")];
    }
    const thisTurn = entries.filter((e) => e.turn === this.currentTurn && this.currentTurn > 0);
    const roots: TreeNode[] = [
      new HeaderNode(
        `${entries.length} 个文件`,
        thisTurn.length > 0 ? `本回合 ${thisTurn.length}` : undefined
      ),
    ];
    for (const e of entries) {
      roots.push(new ChangeNode(e, this.currentTurn));
    }
    return roots;
  }
}

function normalizeKind(kind: string): string {
  return kind.trim().toLowerCase();
}

function normalizePath(p: string): string {
  let s = p.trim().replace(/^remote:/, "").replace(/\\/g, "/");
  // Strip surrounding backticks / quotes
  s = s.replace(/^[`'"]+|[`'"]+$/g, "");
  return s;
}

/** Resolve relative paths against remote work_dir; keep absolute/~/ as-is. */
function canonicalizePath(raw: string, workDir: string): string {
  const n = normalizePath(raw);
  if (!n) {
    return "";
  }
  if (n.startsWith("/") || n.startsWith("~/")) {
    return n;
  }
  const wd = (workDir || "").replace(/\/+$/, "");
  if (!wd) {
    return n;
  }
  if (n.startsWith("./")) {
    return `${wd}/${n.slice(2)}`;
  }
  return `${wd}/${n.replace(/^\//, "")}`;
}

function collectPaths(update: Record<string, unknown>): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  const add = (p: unknown) => {
    if (typeof p !== "string") {
      return;
    }
    const n = normalizePath(p);
    if (!n || seen.has(n)) {
      return;
    }
    // Prefer absolute / home / relative project paths; skip empty.
    if (!(n.startsWith("/") || n.startsWith("~/") || n.includes("/"))) {
      // still allow simple basenames (agent may write "main.go")
      if (!/^[.\w-]+\.[\w]+$/.test(n) && !/^[.\w-]+$/.test(n)) {
        return;
      }
    }
    seen.add(n);
    out.push(n);
  };

  const locs = update.locations;
  if (Array.isArray(locs)) {
    for (const loc of locs) {
      if (loc && typeof loc === "object") {
        add((loc as { path?: string }).path);
      }
    }
  }

  const raw = update.rawInput;
  if (raw && typeof raw === "object") {
    const o = raw as Record<string, unknown>;
    for (const k of ["path", "file", "file_path", "filepath", "target", "to", "from"]) {
      add(o[k]);
    }
  }

  return out;
}
