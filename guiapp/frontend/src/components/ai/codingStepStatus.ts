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

/**
 * Step reached any terminal state. Progress counters use this: a skipped step
 * still advances "3/5", while a pending or running one does not.
 */
export function codingStepIsSettled(status: string): boolean {
    return codingStepIsDone(status) || codingStepIsFailed(status) || codingStepIsSkipped(status);
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
export function codingChecklistAllDone<T extends { status?: string }>(steps: T[] | undefined): boolean {
    const list = steps || [];
    if (list.length === 0) return false;
    return list.every((step) => {
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
