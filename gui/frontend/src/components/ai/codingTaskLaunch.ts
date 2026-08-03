/**
 * Shared launch contract for project-backed coding tasks.
 *
 * Every creator (welcome, IM, task list, utilities, virtual repository) should
 * hand off to the assistant through this shape instead of constructing a tab
 * request ad hoc.  Credentials intentionally never belong here.
 */
export type CodingTaskAgentMode = "coding_dev" | "remote_coding_dev";

export interface CodingTaskLaunch {
    projectPath: string;
    taskTitle: string;
    initialMessage?: string;
    autoSend?: boolean;
    prepareMode?: "restore-context" | "new-agent";
    agentMode?: CodingTaskAgentMode;
    remoteHost?: string;
    /** Evidence-only first SSH turn for a remote incident diagnosis. */
    remoteSafety?: "diagnosis";
    /** True only when SSH must be re-established before task intents may run. */
    remoteNeedsReconnect?: boolean;
    imPlatform?: string;
    imTargetUID?: string;
    imIsGroup?: boolean;
}

/** Coerce untrusted Wails/event payloads into the one safe launch contract. */
export function normalizeCodingTaskLaunch(input: Partial<CodingTaskLaunch> | null | undefined): CodingTaskLaunch | null {
    const projectPath = String(input?.projectPath || "").trim();
    if (!projectPath) return null;
    const rawMode = input?.agentMode;
    const agentMode: CodingTaskAgentMode | undefined = rawMode === "coding_dev" || rawMode === "remote_coding_dev"
        ? rawMode
        : undefined;
    const remoteHost = String(input?.remoteHost || "").trim() || undefined;
    return {
        projectPath,
        taskTitle: String(input?.taskTitle || "").trim() || projectPath,
        initialMessage: String(input?.initialMessage || "").trim() || undefined,
        autoSend: input?.autoSend,
        prepareMode: input?.prepareMode === "restore-context" ? "restore-context" : "new-agent",
        agentMode,
        remoteHost,
        remoteSafety: agentMode === "remote_coding_dev" && input?.remoteSafety === "diagnosis"
            ? "diagnosis"
            : undefined,
        // A local task must never inherit a stale reconnect flag.
        remoteNeedsReconnect: agentMode === "remote_coding_dev" ? input?.remoteNeedsReconnect === true : undefined,
        imPlatform: String(input?.imPlatform || "").trim() || undefined,
        imTargetUID: String(input?.imTargetUID || "").trim() || undefined,
        imIsGroup: input?.imIsGroup === true,
    };
}
