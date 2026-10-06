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
