/** Helpers for pure-coding task tags shared by sidebar / search / resume. */

export type PureCodingAgentMode = "coding_dev" | "remote_coding_dev";

/** Max length for create-task command textarea / welcome coding templates (UTF-16 units). */
export const CODING_TASK_COMMAND_MAX_LEN = 2000;

export type RemoteCodingMetaFromTags = {
    host: string;
    user: string;
    port: number;
    workDir: string;
};

export function agentModeFromTaskTags(tags?: string[] | null): PureCodingAgentMode | undefined {
    if (!tags?.length) return undefined;
    if (tags.includes("remote_coding_dev")) return "remote_coding_dev";
    if (tags.includes("coding_dev")) return "coding_dev";
    return undefined;
}

export function remoteHostFromTaskTags(tags?: string[] | null): string | undefined {
    if (!tags?.length) return undefined;
    const raw = tags.find((t) => t.startsWith("remote_host:"));
    if (!raw) return undefined;
    const host = raw.slice("remote_host:".length).trim();
    return host || undefined;
}

/** Parse non-sensitive remote SSH metadata from task tags (password is never stored). */
export function remoteCodingMetaFromTaskTags(tags?: string[] | null): RemoteCodingMetaFromTags {
    const meta: RemoteCodingMetaFromTags = { host: "", user: "", port: 22, workDir: "" };
    if (!tags?.length) return meta;
    for (const raw of tags) {
        const t = String(raw || "").trim();
        if (t.startsWith("remote_host:")) {
            meta.host = t.slice("remote_host:".length).trim();
        } else if (t.startsWith("remote_user:")) {
            meta.user = t.slice("remote_user:".length).trim();
        } else if (t.startsWith("remote_port:")) {
            const p = Number.parseInt(t.slice("remote_port:".length).trim(), 10);
            if (Number.isFinite(p) && p > 0 && p < 65536) meta.port = p;
        } else if (t.startsWith("remote_workdir:")) {
            meta.workDir = t.slice("remote_workdir:".length).trim();
        }
    }
    return meta;
}

export function isPureCodingTaskTags(tags?: string[] | null): boolean {
    return agentModeFromTaskTags(tags) != null;
}

/** True when the task was created from the multi-phase coding workflow form. */
export function isCodingWorkflowSourceTags(tags?: string[] | null): boolean {
    if (!tags?.length) return false;
    return tags.some((t) => {
        const s = String(t || "").trim();
        return s === "source:coding_workflow" || s.startsWith("source:coding_workflow");
    });
}
