/**
 * Virtual documents for remote coding paths (maclaw-remote://).
 * Content is fetched via Mode B ACP maclaw/read_remote_file.
 */
import * as path from "path";
import * as vscode from "vscode";
import type { AcpClient } from "./acpClient";

export const REMOTE_SCHEME = "maclaw-remote";

export interface RemoteReadResult {
  ok?: boolean;
  path?: string;
  work_dir?: string;
  content?: string;
  truncated?: boolean;
  encoding?: string;
}

export class RemoteFileProvider implements vscode.TextDocumentContentProvider {
  private readonly _onDidChange = new vscode.EventEmitter<vscode.Uri>();
  readonly onDidChange = this._onDidChange.event;

  constructor(
    private readonly getClient: () => AcpClient | undefined,
    private readonly getProjectPath: () => string | undefined
  ) {}

  dispose(): void {
    this._onDidChange.dispose();
  }

  /** Notify VS Code to re-fetch a virtual document. */
  refresh(uri: vscode.Uri): void {
    this._onDidChange.fire(uri);
  }

  /** Refresh every open maclaw-remote:// document (after agent turns, etc.). */
  refreshAllOpen(): number {
    let n = 0;
    for (const doc of vscode.workspace.textDocuments) {
      if (doc.uri.scheme === REMOTE_SCHEME) {
        this._onDidChange.fire(doc.uri);
        n++;
      }
    }
    return n;
  }

  async provideTextDocumentContent(uri: vscode.Uri): Promise<string> {
    const remotePath = uriToRemotePath(uri);
    if (!remotePath) {
      return "// invalid remote URI\n";
    }
    const client = this.getClient();
    if (!client || !client.isRunning) {
      return [
        `// MaClaw remote preview unavailable — bridge not connected`,
        `// path: ${remotePath}`,
        `// Start MaClaw GUI, attach the remote task, then reopen this file.`,
        "",
      ].join("\n");
    }
    const projectPath = (this.getProjectPath() ?? "").trim();
    if (!projectPath) {
      return [
        `// No remote task attached`,
        `// path: ${remotePath}`,
        `// Use the sidebar to attach a remote coding task first.`,
        "",
      ].join("\n");
    }
    try {
      const res = (await client.maclawCall("maclaw/read_remote_file", {
        project_path: projectPath,
        path: remotePath,
        offset: 1,
        limit: 2000,
      })) as RemoteReadResult;
      const body = typeof res.content === "string" ? res.content : "";
      const header: string[] = [
        `// remote: ${res.path || remotePath}`,
        `// work_dir: ${res.work_dir || "?"}`,
      ];
      if (res.truncated) {
        header.push("// (truncated — first 2000 lines)");
      }
      header.push("");
      return header.join("\n") + body + (body.endsWith("\n") ? "" : "\n");
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      return [
        `// Failed to read remote file`,
        `// path: ${remotePath}`,
        `// error: ${msg}`,
        `// Tip: re-attach remote coding if SSH session expired.`,
        "",
      ].join("\n");
    }
  }
}

/** Build maclaw-remote:// URI from a posix remote path. */
export function remotePathToUri(remotePath: string): vscode.Uri {
  const cleaned = remotePath.replace(/\\/g, "/").replace(/^remote:/, "");
  // Use path as URI path; encode each segment.
  const parts = cleaned.split("/").filter((p, i) => p !== "" || i === 0);
  const encoded = parts.map((p, i) => (i === 0 && p === "" ? "" : encodeURIComponent(p))).join("/");
  const pathPart = cleaned.startsWith("/") ? "/" + parts.filter(Boolean).map(encodeURIComponent).join("/") : encoded;
  return vscode.Uri.from({
    scheme: REMOTE_SCHEME,
    path: pathPart.startsWith("/") ? pathPart : "/" + pathPart,
  });
}

export function uriToRemotePath(uri: vscode.Uri): string {
  if (uri.scheme !== REMOTE_SCHEME) {
    return "";
  }
  // vscode.Uri.path is already decoded for path segments in most cases.
  let p = uri.path || "";
  if (process.platform === "win32" && /^\/[A-Za-z]:/.test(p)) {
    // not expected for remote posix paths
  }
  try {
    p = decodeURIComponent(p);
  } catch {
    /* keep raw */
  }
  return p || "";
}

export function isRemotePosixPath(p: string): boolean {
  const t = p.trim().replace(/^remote:/, "");
  return t.startsWith("/") || t.startsWith("~/");
}

export function languageIdForRemotePath(remotePath: string): string {
  const ext = path.posix.extname(remotePath).toLowerCase();
  const map: Record<string, string> = {
    ".go": "go",
    ".ts": "typescript",
    ".tsx": "typescriptreact",
    ".js": "javascript",
    ".jsx": "javascriptreact",
    ".py": "python",
    ".rs": "rust",
    ".java": "java",
    ".c": "c",
    ".h": "c",
    ".cpp": "cpp",
    ".cc": "cpp",
    ".hpp": "cpp",
    ".cs": "csharp",
    ".md": "markdown",
    ".json": "json",
    ".yml": "yaml",
    ".yaml": "yaml",
    ".toml": "toml",
    ".sh": "shellscript",
    ".bash": "shellscript",
    ".zsh": "shellscript",
    ".html": "html",
    ".css": "css",
    ".scss": "scss",
    ".sql": "sql",
    ".xml": "xml",
    ".txt": "plaintext",
  };
  return map[ext] ?? "plaintext";
}

export type RemoteLsKind = "file" | "dir" | "link" | "other";

export interface RemoteLsEntry {
  kind: RemoteLsKind;
  name: string;
  /** Absolute remote path when parentDir is known. */
  path: string;
  raw: string;
}

/**
 * Parse `ls -la` output into entries. Skips total/summary and . / ..
 */
export function parseLsListing(listing: string, parentDir: string): RemoteLsEntry[] {
  const parent = parentDir.replace(/\/+$/, "") || "/";
  const out: RemoteLsEntry[] = [];
  for (const line of listing.split(/\r?\n/)) {
    const trimmed = line.trimEnd();
    if (!trimmed || /^total\s+\d+/i.test(trimmed)) {
      continue;
    }
    // permission field starts with d/-/l/c/b/p/s
    const m = trimmed.match(/^([dlcbps\-])[rwxstST\-+]{9}/);
    if (!m) {
      continue;
    }
    const kindChar = m[1];
    // Name is last field; handle "name -> target" for symlinks.
    const parts = trimmed.split(/\s+/);
    if (parts.length < 9) {
      continue;
    }
    // After: perms links owner group size mon day time/year NAME...
    let name = parts.slice(8).join(" ");
    const arrow = name.indexOf(" -> ");
    if (arrow >= 0) {
      name = name.slice(0, arrow);
    }
    name = name.trim();
    if (!name || name === "." || name === "..") {
      continue;
    }
    let kind: RemoteLsKind = "other";
    if (kindChar === "d") {
      kind = "dir";
    } else if (kindChar === "-") {
      kind = "file";
    } else if (kindChar === "l") {
      kind = "link";
    }
    const abs =
      name.startsWith("/")
        ? name
        : parent === "/"
          ? `/${name}`
          : `${parent}/${name}`;
    out.push({ kind, name, path: abs, raw: trimmed });
  }
  return out;
}

/** Relative path of remotePath under workDir, or basename fallback. */
export function remotePathRelativeToWorkDir(remotePath: string, workDir: string): string {
  const p = remotePath.replace(/\\/g, "/").replace(/^remote:/, "");
  const w = workDir.replace(/\\/g, "/").replace(/\/+$/, "");
  if (w && (p === w || p.startsWith(w + "/"))) {
    return p.slice(w.length).replace(/^\//, "") || path.posix.basename(p);
  }
  return path.posix.basename(p);
}

/**
 * Count leading `// …` header lines + the following blank separator in
 * maclaw-remote:// virtual documents. Body line N (1-based remote source)
 * maps to document line index `header + N - 1`.
 */
export function countRemotePreviewHeaderLines(doc: {
  lineCount: number;
  lineAt(line: number): { text: string };
}): number {
  let i = 0;
  while (i < doc.lineCount) {
    const t = doc.lineAt(i).text;
    if (t.startsWith("//")) {
      i++;
      continue;
    }
    if (t.trim() === "") {
      i++;
      break;
    }
    break;
  }
  return i;
}

/** Map 1-based remote source line → 0-based document line (clamped). */
export function remoteSourceLineToDocLine(
  doc: { lineCount: number; lineAt(line: number): { text: string } },
  remoteLine: number
): number {
  const header = countRemotePreviewHeaderLines(doc);
  return Math.min(
    Math.max(0, header + remoteLine - 1),
    Math.max(0, doc.lineCount - 1)
  );
}
