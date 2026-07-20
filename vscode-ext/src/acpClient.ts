/**
 * Minimal ACP (Agent Client Protocol) stdio client.
 *
 * Speaks NDJSON JSON-RPC with maclaw-acp-bridge: one JSON object per line on
 * stdout, requests/responses correlated by numeric ids. This module is free of
 * any `vscode` imports so it can be smoke-tested in plain Node (test/smoke.mjs).
 *
 * Protocol surface used (mirrors corelib/acpagent):
 *   client → agent: initialize, session/new, session/prompt, session/cancel (notification)
 *   agent → client: session/update (notification), session/request_permission (request)
 */
import { spawn, ChildProcessWithoutNullStreams } from "child_process";
import { EventEmitter } from "events";
import * as readline from "readline";

export interface InitializeResult {
  protocolVersion: number;
  agentInfo?: { name?: string; title?: string; version?: string };
  [key: string]: unknown;
}

export interface PermissionOption {
  optionId: string;
  name: string;
  kind: string;
}

export interface PermissionRequestParams {
  sessionId: string;
  toolCall: Record<string, unknown>;
  options: PermissionOption[];
}

export interface PendingPermission {
  /** JSON-RPC id of the inbound request — needed to send the response.
   *  Echoed back verbatim (agents may legally use string ids). */
  rpcId: number | string;
  params: PermissionRequestParams;
}

/** Remote coding task row from maclaw/list_remote_coding_tasks. */
export interface RemoteCodingTask {
  name: string;
  project_path: string;
  host: string;
  user: string;
  port: number;
  work_dir: string;
  kind?: string;
  armed?: boolean;
  needs_reconnect?: boolean;
  message?: string;
  last_activity?: string;
}

export interface CodingWorkbenchStatusResult {
  status?: {
    kind?: string;
    armed?: boolean;
    needs_reconnect?: boolean;
    message?: string;
    turn_count?: number;
  };
  meta?: {
    host?: string;
    user?: string;
    port?: number;
    work_dir?: string;
  };
}

interface PendingCall {
  resolve: (value: unknown) => void;
  reject: (err: Error) => void;
}

export class BridgeExitError extends Error {}

/**
 * Events emitted:
 *   "update"     (params: { sessionId, update })      — session/update notification
 *   "permission" (perm: PendingPermission)            — session/request_permission request
 *   "exit"       (code: number | null, signal: string | null)
 *   "log"        (line: string)                       — bridge stderr lines
 */
export class AcpClient extends EventEmitter {
  private proc?: ChildProcessWithoutNullStreams;
  private rl?: readline.Interface;
  private nextId = 1;
  private pending = new Map<number, PendingCall>();
  private bridgePath = "";

  get isRunning(): boolean {
    return !!this.proc && this.proc.exitCode === null && !this.proc.killed;
  }

  get bridge(): string {
    return this.bridgePath;
  }

  /** Spawn the bridge and perform the ACP initialize handshake. */
  async start(bridgePath: string, clientVersion: string, args: string[] = []): Promise<InitializeResult> {
    this.stop();
    this.bridgePath = bridgePath;
    const proc = spawn(bridgePath, args, { stdio: ["pipe", "pipe", "pipe"] });
    this.proc = proc;

    proc.on("error", (err) => {
      // Spawn failed (missing binary, EACCES, …): without this, exitCode stays
      // null and isRunning would report true forever on a dead process.
      if (this.proc !== proc) {
        return; // stale handler from a previously replaced process
      }
      this.rl?.close();
      this.rl = undefined;
      this.proc = undefined;
      this.failAll(err);
      this.emit("exit", null, null);
    });
    proc.on("exit", (code, signal) => {
      if (this.proc !== proc) {
        return; // stale handler from a previously replaced process
      }
      this.rl?.close();
      this.rl = undefined;
      this.proc = undefined;
      this.failAll(new BridgeExitError(`bridge exited (code=${code} signal=${signal})`));
      this.emit("exit", code, signal);
    });
    // Writes to a dead pipe surface as stdin 'error'; without a listener Node
    // raises an uncaught exception in the extension host.
    proc.stdin.on("error", () => {
      /* exit handler reports the failure */
    });
    proc.stderr.on("data", (chunk) => {
      for (const line of String(chunk).split(/\r?\n/)) {
        if (line.trim() !== "") {
          this.emit("log", line);
        }
      }
    });
    this.rl = readline.createInterface({ input: proc.stdout });
    this.rl.on("line", (line) => this.handleLine(line));

    let result: InitializeResult;
    try {
      result = (await this.request("initialize", {
        protocolVersion: 1,
        clientCapabilities: {
          fs: { readTextFile: false, writeTextFile: false },
          terminal: false,
        },
        clientInfo: {
          name: "maclaw-acp-vscode",
          title: "MaClaw ACP (VS Code)",
          version: clientVersion,
        },
      })) as InitializeResult;
    } catch (err) {
      // Handshake failed but the process may still be alive (e.g. RPC error
      // response) — don't leave an uninitialized bridge running.
      this.stop();
      throw err;
    }
    return result;
  }

  async newSession(cwd: string): Promise<string> {
    const res = (await this.request("session/new", { cwd, mcpServers: [] })) as {
      sessionId: string;
    };
    return res.sessionId;
  }

  async prompt(sessionId: string, text: string): Promise<{ stopReason: string }> {
    return (await this.request("session/prompt", {
      sessionId,
      prompt: [{ type: "text", text }],
    })) as { stopReason: string };
  }

  /**
   * session/steer (MaClaw extension): inject guide-launch text into the
   * session's running agent loop — the GUI drains it between iterations
   * without cancelling the turn. accepted=false (or an RPC error on hosts
   * without this method) means: fall back to queueing for the next turn.
   */
  async steer(sessionId: string, text: string): Promise<{ accepted: boolean }> {
    return (await this.request("session/steer", { sessionId, text })) as { accepted: boolean };
  }

  /**
   * MaClaw extension RPCs (Mode B host). Used by the VS Code sidebar to attach
   * to remote_coding_dev tasks without going through the GUI task list.
   */
  async maclawCall(method: string, params: Record<string, unknown> = {}): Promise<unknown> {
    if (!method.startsWith("maclaw/")) {
      throw new Error(`maclawCall: method must start with maclaw/, got ${method}`);
    }
    return this.request(method, params);
  }

  async listRemoteCodingTasks(limit = 40): Promise<RemoteCodingTask[]> {
    const res = (await this.maclawCall("maclaw/list_remote_coding_tasks", { limit })) as {
      tasks?: RemoteCodingTask[];
    };
    return Array.isArray(res?.tasks) ? res.tasks : [];
  }

  async getCodingWorkbenchStatus(projectPath: string): Promise<CodingWorkbenchStatusResult> {
    return (await this.maclawCall("maclaw/get_coding_workbench_status", {
      project_path: projectPath,
    })) as CodingWorkbenchStatusResult;
  }

  async ensureCodingWorkbenchArmed(projectPath: string): Promise<CodingWorkbenchStatusResult> {
    return (await this.maclawCall("maclaw/ensure_coding_workbench_armed", {
      project_path: projectPath,
    })) as CodingWorkbenchStatusResult;
  }

  async prepareRemoteCoding(params: {
    projectPath: string;
    sshHost?: string;
    sshUser?: string;
    sshPassword: string;
    workDir?: string;
    sshPort?: number;
  }): Promise<CodingWorkbenchStatusResult & { ok?: boolean; message?: string }> {
    return (await this.maclawCall("maclaw/prepare_remote_coding", {
      project_path: params.projectPath,
      ssh_host: params.sshHost ?? "",
      ssh_user: params.sshUser ?? "",
      ssh_password: params.sshPassword,
      work_dir: params.workDir ?? "",
      ssh_port: params.sshPort ?? 0,
    })) as CodingWorkbenchStatusResult & { ok?: boolean; message?: string };
  }

  async readRemoteFile(params: {
    projectPath: string;
    path: string;
    offset?: number;
    limit?: number;
  }): Promise<{
    ok?: boolean;
    path?: string;
    work_dir?: string;
    content?: string;
    truncated?: boolean;
  }> {
    return (await this.maclawCall("maclaw/read_remote_file", {
      project_path: params.projectPath,
      path: params.path,
      offset: params.offset ?? 1,
      limit: params.limit ?? 2000,
    })) as {
      ok?: boolean;
      path?: string;
      work_dir?: string;
      content?: string;
      truncated?: boolean;
    };
  }

  async listRemoteDir(params: {
    projectPath: string;
    path?: string;
  }): Promise<{ ok?: boolean; path?: string; work_dir?: string; listing?: string }> {
    return (await this.maclawCall("maclaw/list_remote_dir", {
      project_path: params.projectPath,
      path: params.path ?? "",
    })) as { ok?: boolean; path?: string; work_dir?: string; listing?: string };
  }

  async searchRemote(params: {
    projectPath: string;
    query: string;
    path?: string;
    maxResults?: number;
    caseSensitive?: boolean;
  }): Promise<{
    ok?: boolean;
    query?: string;
    path?: string;
    work_dir?: string;
    hits?: { path: string; line: number; text: string; preview: string }[];
    hit_count?: number;
    truncated?: boolean;
    raw?: string;
  }> {
    return (await this.maclawCall("maclaw/search_remote", {
      project_path: params.projectPath,
      query: params.query,
      path: params.path ?? "",
      max_results: params.maxResults ?? 50,
      case_sensitive: params.caseSensitive === true,
    })) as {
      ok?: boolean;
      query?: string;
      path?: string;
      work_dir?: string;
      hits?: { path: string; line: number; text: string; preview: string }[];
      hit_count?: number;
      truncated?: boolean;
      raw?: string;
    };
  }

  /** session/cancel is a notification — no response expected. */
  cancel(sessionId: string): void {
    this.notify("session/cancel", { sessionId });
  }

  /** Answer an inbound session/request_permission request. */
  resolvePermission(rpcId: number | string, optionId: string): void {
    this.respond(rpcId, { outcome: { outcome: "selected", optionId } });
  }

  cancelPermission(rpcId: number | string): void {
    this.respond(rpcId, { outcome: { outcome: "cancelled" } });
  }

  stop(): void {
    this.rl?.close();
    this.rl = undefined;
    if (this.proc) {
      const proc = this.proc;
      this.proc = undefined;
      try {
        proc.kill();
      } catch {
        /* already gone */
      }
    }
    this.failAll(new BridgeExitError("client stopped"));
  }

  private request(method: string, params: unknown): Promise<unknown> {
    if (!this.isRunning) {
      return Promise.reject(new BridgeExitError("bridge is not running"));
    }
    const id = this.nextId++;
    const payload = JSON.stringify({ jsonrpc: "2.0", id, method, params });
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.proc!.stdin.write(payload + "\n", (err) => {
        if (err) {
          this.pending.delete(id);
          reject(err);
        }
      });
    });
  }

  private notify(method: string, params: unknown): void {
    if (!this.isRunning) {
      return;
    }
    this.proc!.stdin.write(JSON.stringify({ jsonrpc: "2.0", method, params }) + "\n");
  }

  private respond(id: number | string, result: unknown): void {
    if (!this.isRunning) {
      return;
    }
    this.proc!.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, result }) + "\n");
  }

  private respondError(id: number | string, code: number, message: string): void {
    if (!this.isRunning) {
      return;
    }
    this.proc!.stdin.write(
      JSON.stringify({ jsonrpc: "2.0", id, error: { code, message } }) + "\n"
    );
  }

  private handleLine(line: string): void {
    const trimmed = line.trim();
    if (trimmed === "") {
      return;
    }
    let msg: {
      id?: number | string;
      method?: string;
      params?: unknown;
      result?: unknown;
      error?: { code: number; message: string };
    };
    try {
      msg = JSON.parse(trimmed);
    } catch {
      this.emit("log", `[acp] dropped non-JSON line: ${trimmed.slice(0, 200)}`);
      return;
    }

    // Response to one of our requests (our outbound ids are always numeric).
    if (msg.id !== undefined && msg.method === undefined) {
      const id = typeof msg.id === "string" ? Number(msg.id) : msg.id;
      const call = this.pending.get(id);
      if (call) {
        this.pending.delete(id);
        if (msg.error) {
          call.reject(new Error(`${msg.error.message} (code ${msg.error.code})`));
        } else {
          call.resolve(msg.result);
        }
      }
      return;
    }

    // Inbound request from the agent — echo the id back verbatim.
    if (msg.method && msg.id !== undefined) {
      const id = msg.id;
      if (msg.method === "session/request_permission") {
        this.emit("permission", { rpcId: id, params: msg.params } as PendingPermission);
      } else {
        // fs/*, terminal/* and friends are never used by the MaClaw host; be
        // explicit instead of hanging the agent.
        this.respondError(id, -32601, `method not implemented by client: ${msg.method}`);
      }
      return;
    }

    // Inbound notification.
    if (msg.method === "session/update") {
      this.emit("update", msg.params);
    }
  }

  private failAll(err: Error): void {
    const calls = [...this.pending.values()];
    this.pending.clear();
    for (const call of calls) {
      call.reject(err);
    }
  }
}
