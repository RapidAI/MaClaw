import type { CodingTaskAgentMode } from "./codingTaskLaunch";

/** The only lifecycle states a coding task may expose to its UI and dispatcher. */
export type CodingTaskPhase = "non_coding" | "preparing" | "reconnect_required" | "ready";

export function resolveCodingTaskPhase(input: {
    agentMode?: CodingTaskAgentMode;
    preparing: boolean;
    remoteNeedsReconnect: boolean;
}): CodingTaskPhase {
    // Project session creation/restoration is a universal gate, including
    // ordinary project tabs that have no coding mode yet.
    if (input.preparing) return "preparing";
    if (!input.agentMode) return "non_coding";
    if (input.agentMode === "remote_coding_dev" && input.remoteNeedsReconnect) return "reconnect_required";
    return "ready";
}

/** Only a ready coding environment may execute a task-owned intent. */
export function canDispatchCodingIntent(phase: CodingTaskPhase): boolean {
    return phase === "non_coding" || phase === "ready";
}
