/**
 * Single source of truth for the coding-step state vocabulary.
 *
 * The backend emits more than one spelling per state (`running`/`in_progress`/
 * `started`, `passed`/`completed`/`success`, `failed`/`verify_failed`/`error`),
 * and those spellings used to be compared inline in five places — glyph, row
 * color, label, progress counter, status chip — so a new spelling silently
 * desynced some of them. Every decision now routes through these predicates.
 *
 * Canonical contract for the coding workbench (guiapp/coding_workbench_align.go,
 * `codingWorkbenchStepStatus.Status`):
 *     pending | running | passed | failed | skipped | verify_failed
 * Anything beyond that set is a defensive alias for another producer — notably
 * the agent-view skill runner, which normalizes to `done`/`error`/`pending`
 * (guiapp/agent_view_step_status.go) and would otherwise render as "☐ done".
 * Keep this list aligned with those two files when either side changes.
 *
 * Zero-dependency leaf module: safe to import from anywhere in components/ai
 * without creating an import cycle.
 */

/** Step reached a successful terminal state. */
export function codingStepIsDone(status: string): boolean {
    const s = (status || "").toLowerCase();
    // `passed` is the workbench canonical; `done` comes from the agent-view
    // producer; the rest are legacy spellings.
    return s === "passed" || s === "done" || s === "completed" || s === "success" || s === "succeeded";
}

/** Step ended in failure (including a failed verification pass). */
export function codingStepIsFailed(status: string): boolean {
    const s = (status || "").toLowerCase();
    return s === "failed" || s === "verify_failed" || s === "error";
}

/** Step is executing right now. */
export function codingStepIsActive(status: string): boolean {
    const s = (status || "").toLowerCase();
    return s === "running" || s === "in_progress" || s === "started";
}

/** Step will never run (both spellings of cancelled appear in the wild). */
export function codingStepIsSkipped(status: string): boolean {
    const s = (status || "").toLowerCase();
    return s === "skipped" || s === "cancelled" || s === "canceled";
}

const codingReviewBlockReason = "等待子任务审阅";

/**
 * The execution layer published a child-review wait. The runner status of a
 * dependent row stays skipped so it can reopen; the checklist must not
 * present that row as Skipped or count it done.
 */
export function codingStepAwaitsReview(step: { status?: string; block_reason?: string; summary?: string } | null | undefined): boolean {
    if (!step) return false;
    const status = String(step.status || "").toLowerCase();
    if (status === "waiting_review") return true;
    // Passed and failed rows keep that result. A review sentence left in the
    // note must not paint a failure as waiting, or count a pass as still open.
    if (status !== "skipped" && status !== "cancelled" && status !== "canceled") return false;
    if (String(step.block_reason || "").trim() === codingReviewBlockReason) return true;
    const summary = String(step.summary || "");
    return summary.includes("子任务结果待审阅")
        || summary.includes("waiting_child")
        || summary.includes("runtime_child_review_required")
        || summary.includes("runtime_child_review_in_progress");
}

/** Status the checklist paints. The stored runner status stays on the row. */
export function codingStepDisplayStatus(step: { status?: string; block_reason?: string; summary?: string } | null | undefined): string {
    if (codingStepAwaitsReview(step)) return "waiting_review";
    return String(step?.status || "");
}

/**
 * Step never started because a declared dependency did not pass. It is open
 * work, not a finished skip: it does not advance the done counter and the
 * checklist cannot retire while a blocked row remains.
 */
export function codingStepIsBlocked(status: string): boolean {
    const s = (status || "").toLowerCase();
    return s === "blocked" || s === "not_started";
}

/**
 * Step reached any terminal state. Progress counters use this: a skipped step
 * still advances "3/5", while a pending or running one does not.
 */
export function codingStepIsSettled(status: string): boolean {
    return codingStepIsDone(status) || codingStepIsFailed(status) || codingStepIsSkipped(status);
}

export type CodingWorkbenchStepRow = {
    index: number;
    title: string;
    status: string;
    summary: string;
    verify_cmd: string;
    verify_ok?: boolean;
    /** Unix seconds from the backend. 0 means the producer did not stamp the row. */
    updated_unix: number;
    blocked_by?: string[];
    block_reason?: string;
    child_task_id?: string;
};

/** Shared execution status published with the checklist. Empty means the caller still infers. */
export function codingSharedExecutionStatus(raw: unknown): string {
    if (!raw || typeof raw !== "object") return "";
    const rec = raw as Record<string, unknown>;
    const value = rec.execution_status ?? rec.executionStatus;
    return typeof value === "string" ? value.trim() : "";
}

function codingStringList(value: unknown): string[] | undefined {
    if (!Array.isArray(value)) return undefined;
    const out = value.map((item) => String(item || "").trim()).filter(Boolean);
    return out.length > 0 ? out : undefined;
}

/** Normalize a status or event step list. A non-array is an empty checklist. */
export function codingWorkbenchStepsFromPayload(raw: unknown): CodingWorkbenchStepRow[] {
    if (!Array.isArray(raw)) return [];
    return raw.map((item) => {
        const step = item && typeof item === "object" ? item as Record<string, unknown> : {};
        return {
            index: Number(step.index) || 0,
            title: String(step.title || ""),
            status: String(step.status || "pending"),
            summary: String(step.summary || ""),
            verify_cmd: String(step.verify_cmd || ""),
            verify_ok: typeof step.verify_ok === "boolean" ? step.verify_ok : undefined,
            updated_unix: Number(step.updated_unix) || 0,
            blocked_by: codingStringList(step.blocked_by ?? step.blockedBy),
            block_reason: String(step.block_reason || step.blockReason || "").trim() || undefined,
            child_task_id: String(step.child_task_id || step.childTaskId || "").trim() || undefined,
        };
    });
}

/** Newest stamp on the list. Unstamped rows contribute nothing. */
export function codingWorkbenchStepsRevision(steps: { updated_unix?: number }[] | undefined): number {
    let max = 0;
    for (const step of steps || []) {
        const stamp = Number(step?.updated_unix) || 0;
        if (stamp > max) max = stamp;
    }
    return max;
}

/**
 * A clear and an unstamped list still apply. A stamped list older than the
 * revision already on screen is a late event and must not restore the gate.
 */
export function codingWorkbenchStepsAreCurrent(
    currentRevision: number,
    incoming: { updated_unix?: number }[] | undefined,
): boolean {
    if (!incoming || incoming.length === 0) return true;
    const next = codingWorkbenchStepsRevision(incoming);
    if (next <= 0) return true;
    return next >= currentRevision;
}

/**
 * Checklist snapshot clock. `seq` is the backend emit counter and wins over
 * `unix`, because two writes in the same second share a unix stamp.
 */
export type CodingStepsClock = { seq: number; unix: number };

export function codingStepsClockFrom(seq: unknown, steps: { updated_unix?: number }[] | undefined): CodingStepsClock {
    const parsed = Number(seq);
    return {
        seq: Number.isFinite(parsed) && parsed > 0 ? parsed : 0,
        unix: codingWorkbenchStepsRevision(steps),
    };
}

/**
 * Newer sequence always wins. Without one, a stamped list older than the
 * screen is dropped, and an unstamped list cannot rewind a stamped one.
 * An empty list still applies so a finished plan can clear.
 */
export function codingStepsClockIsCurrent(current: CodingStepsClock, incoming: CodingStepsClock, incomingEmpty = false): boolean {
    if (incoming.seq > 0) {
        if (current.seq > 0) return incoming.seq >= current.seq;
        // First sequence number seen. A stamped list already on screen is
        // newer than an older unix stamp, even if that older copy has a seq.
        if (current.unix > 0 && incoming.unix > 0 && incoming.unix < current.unix) return false;
        return true;
    }
    if (current.seq > 0) return false;
    if (incomingEmpty) return true;
    if (incoming.unix <= 0) return current.unix <= 0;
    if (current.unix <= 0) return true;
    return incoming.unix >= current.unix;
}

/**
 * Same generation as the list already shown, but every incoming row is still
 * unstarted. That is the pre-confirm copy (a poll read the same counter) and
 * must not rewind a step that has already left pending.
 */
export function codingStepsSnapshotRegresses(
    started: boolean,
    current: CodingStepsClock,
    incoming: CodingStepsClock,
    incomingSteps: { status?: string }[] | undefined,
): boolean {
    if (!started) return false;
    const list = incomingSteps || [];
    if (list.length === 0 || !codingPlanAwaitingStart(list)) return false;
    if (incoming.seq > 0 && current.seq > 0 && incoming.seq === current.seq) return true;
    return incoming.seq <= 0 && incoming.unix > 0 && current.unix > 0 && incoming.unix === current.unix;
}

/**
 * Plan gate (start / skip / reject) is only for a plan that has not begun.
 *
 * An empty list still counts as awaiting: the card may only have the
 * requirement restatement. The first active or settled row means the plan
 * was confirmed and the gate would sit under a live checklist.
 */
export function codingPlanAwaitingStart<T extends { status?: string }>(steps: T[] | undefined): boolean {
    const list = steps || [];
    return list.every((step) => {
        const status = String(step?.status || "");
        if (status.toLowerCase() === "waiting_review") return false;
        return !codingStepIsActive(status) && !codingStepIsSettled(status);
    });
}

/**
 * True when every row reached a successful close (done or skipped).
 *
 * This is the checklist retirement condition: once no step is open, live, or
 * failed, the card above the composer has nothing left to report and would
 * otherwise pin a finished list over a session that is no longer working on
 * it. A failed row is deliberately not "closed" — that state stays visible so
 * the failure can be read. An empty list is not closed either; the
 * requirement-understanding card still uses it.
 */
export function codingChecklistAllDone<T extends { status?: string; block_reason?: string; summary?: string }>(steps: T[] | undefined): boolean {
    const list = steps || [];
    if (list.length === 0) return false;
    return list.every((step) => {
        if (codingStepAwaitsReview(step)) return false;
        const status = String(step?.status || "");
        return codingStepIsDone(status) || codingStepIsSkipped(status);
    });
}

/**
 * Checklist rows the plan UI should draw.
 *
 * todo_write never writes "failed". With no orchestrator plan, a failed row
 * followed by a later passed row is an older single-turn runner painting its
 * outcome onto checklist index 1. Those rows are the agent's completed todos.
 * The task badge keeps the raw steps, so that failure still fails the turn.
 * A lone failed row, or any row under a real execution plan, stays failed.
 */
export function presentCodingAgentChecklist<T extends { index?: number; status?: string; summary?: string }>(
    executionPlan: string | undefined,
    steps: T[] | undefined,
): T[] {
    const list = steps || [];
    if (list.length < 2 || String(executionPlan || "").trim()) return list;
    let maxPassed = 0;
    for (const step of list) {
        const index = Number(step?.index || 0);
        if (codingStepIsDone(String(step?.status || "")) && index > maxPassed) maxPassed = index;
    }
    if (!maxPassed) return list;
    let changed = false;
    const out = list.map((step) => {
        const index = Number(step?.index || 0);
        if (index >= maxPassed || !codingStepIsFailed(String(step?.status || ""))) return step;
        changed = true;
        return { ...step, status: "passed", summary: "" };
    });
    return changed ? out : list;
}
