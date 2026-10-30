/**
 * Durable task-execution snapshot helpers.
 *
 * Extracted from AIAssistantPanel so the panel stays inside the 6800-line UI
 * guard cap. Both the composer busy state and the title status badge read the
 * same snapshot, so the matching/parsing rules live here rather than being
 * duplicated at each call site.
 */

export type TaskExecutionSnapshotLike = {
    project_path?: string;
    has_output?: boolean;
    tags?: string[];
    active_workflow?: {
        project_path?: string;
        status?: string;
        phase?: string;
        pending_review?: boolean;
    };
};

/** Find the durable task-snapshot row for the active project tab (same rule as the title status badge). */
export function matchTaskSnapshotForProject(tasks: TaskExecutionSnapshotLike[] | undefined, activeProjectPath: string): TaskExecutionSnapshotLike | undefined {
    const target = String(activeProjectPath || "").trim().replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
    if (!Array.isArray(tasks) || !target) return undefined;
    return tasks.find((task) => {
        const candidate = String(task?.project_path || task?.active_workflow?.project_path || "").trim().replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
        return !!candidate && candidate === target;
    });
}

/** Lower-cased activity tokens describing the active task snapshot (mirrors the title-badge pipeline). */
export function buildExecutionSnapshotRaw(snapshot: {
    activeTask?: TaskExecutionSnapshotLike;
    workflowCurrentPhaseStatus?: string;
    workflowAwaitingReview?: boolean;
    workflowActive?: boolean;
    codingStepStatuses?: Array<{ status?: string }>;
}): string {
    const workflowRaw = [
        snapshot.activeTask?.active_workflow?.status,
        snapshot.activeTask?.active_workflow?.phase,
        snapshot.activeTask?.active_workflow?.pending_review ? "pending_review" : "",
        snapshot.workflowCurrentPhaseStatus,
        snapshot.workflowAwaitingReview ? "waiting_confirm" : "",
        snapshot.workflowActive ? "active" : "",
    ].filter(Boolean).join(" ").toLowerCase();
    const stepRaw = (snapshot.codingStepStatuses || []).map((step) => String(step.status || "").toLowerCase()).join(" ");
    return `${workflowRaw} ${stepRaw}`;
}

/** The task list treats these as in progress. "execute" and "execution" share
 * the "execut" stem; a word boundary after that stem misses both. "active" is
 * the workflow status. The match is a whole token, so "interactive" does not
 * count. */
const snapshotProgressToken = /(?:^|[\s_])(running|execut\w*|active|processing|in_progress|started|进行|执行|运行)(?=$|[\s_])/;

export function snapshotRawReportsProgress(raw: string): boolean {
    return snapshotProgressToken.test(raw || "");
}

/** Leading workflow status the task list already files as a finished success. */
export function leadingSnapshotStatusCompleted(raw: string): boolean {
    const token = (raw || "").trim().toLowerCase().split(/[\s_]+/)[0] || "";
    return /^(complete|completed|finish|finished|success|succeeded|done)$/.test(token);
}

/** True when the snapshot tokens describe an execution still in flight.
 * A finished, failed, paused, interrupted, or cancelled status wins even
 * when a later phase is named execute. A progress token wins over a phase
 * merely named review. */
export function executionSnapshotRawIsRunning(raw: string): boolean {
    const text = raw || "";
    if (/(fail|error|blocked|verify_failed|失败|错误|阻塞)/.test(text)) return false;
    if (/(^|[\s_])(paused|pause|已暂停|暂停)(?=$|[\s_])/.test(text)) return false;
    if (/(cancel|canceled|cancelled|stopped|已取消|已停止)/.test(text)) return false;
    if (/(^|[\s_])interrupted(?=$|[\s_])/.test(text)) return false;
    if (leadingSnapshotStatusCompleted(text)) return false;
    // A review gate is the workflow status or the pending_review flag. A
    // later step still marked running belongs to the attempt that is
    // waiting. A phase merely named review does not.
    if (/(^|[\s_])(pending_review|waiting_confirm)(?=$|[\s_])/.test(text)) return false;
    if (/^(waiting[_ ]?review|waiting[_ ]?confirm)(?=$|[\s_])/.test(text.trim().toLowerCase())) return false;
    return snapshotRawReportsProgress(text);
}
