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

/** True when the snapshot tokens describe an execution still in flight (failed/paused/review/cancelled excluded). */
export function executionSnapshotRawIsRunning(raw: string): boolean {
    if (/(fail|error|blocked|verify_failed|失败|错误|阻塞)/.test(raw)) return false;
    if (/(^|[\s_])(paused|pause|已暂停|暂停)(?=$|[\s_])/.test(raw)) return false;
    if (/(waiting[_ ]?confirm|review|approval|pending|待确认|待处理|审核|审批)/.test(raw)) return false;
    if (/(cancel|canceled|cancelled|stopped|已取消|已停止)/.test(raw)) return false;
    return /\b(running|execut|processing|in_progress|started|进行|执行|运行)\b/.test(raw);
}
