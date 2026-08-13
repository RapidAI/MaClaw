/**
 * Shared launch contract for project-backed coding tasks.
 *
 * Every creator (welcome, IM, task list, utilities, virtual repository) should
 * hand off to the assistant through this shape instead of constructing a tab
 * request ad hoc.  Credentials intentionally never belong here.
 */
export type CodingTaskAgentMode = "coding_dev" | "remote_coding_dev";

/**
 * Local-only presentation data for a newly created task. It is shown in the
 * assistant tab as a status card and is never submitted as an agent message.
 */
export interface NewTaskContext {
    kind: "new-task";
    workingDir?: string;
    remoteWorkDir?: string;
    remoteUser?: string;
    /** SSH port is safe display metadata; credentials never belong here. */
    remotePort?: number;
}

export interface CodingTaskLaunch {
    /** Correlates a one-shot tab-opening receipt with its caller. */
    launchId?: string;
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
    /** Workflow template to start after its dedicated assistant tab is ready. */
    workflowType?: string;
    imPlatform?: string;
    imTargetUID?: string;
    imIsGroup?: boolean;
    /** One-shot local context shown after a task-management creation. */
    newTaskContext?: NewTaskContext;
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
    const rawContext = input?.newTaskContext;
    const parsedRemotePort = Number(rawContext?.remotePort);
    const remotePort = Number.isInteger(parsedRemotePort) && parsedRemotePort > 0 && parsedRemotePort < 65536
        ? parsedRemotePort
        : undefined;
    const newTaskContext = rawContext?.kind === "new-task"
        ? {
            kind: "new-task" as const,
            workingDir: String(rawContext.workingDir || "").trim() || undefined,
            remoteWorkDir: String(rawContext.remoteWorkDir || "").trim() || undefined,
            remoteUser: String(rawContext.remoteUser || "").trim() || undefined,
            remotePort,
        }
        : undefined;
    return {
        launchId: String(input?.launchId || "").trim() || undefined,
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
        workflowType: String(input?.workflowType || "").trim() || undefined,
        imPlatform: String(input?.imPlatform || "").trim() || undefined,
        imTargetUID: String(input?.imTargetUID || "").trim() || undefined,
        imIsGroup: input?.imIsGroup === true,
        newTaskContext,
    };
}
