export type GoalBarSnapshot = {
    visible?: boolean;
    user_id?: string;
    goal_id?: string;
    objective?: string;
    status?: string;
    phase?: string;
    turns_used?: number;
    max_turns?: number;
    summary?: string;
    awaiting_user?: boolean;
    created_at?: number;
    // Unix seconds when a failed or budget-limited goal stopped. Zero while the clock runs.
    stopped_at?: number;
    // Seconds already spent terminal. Subtracted so continue does not jump the clock.
    clock_hold_seconds?: number;
};

export type GoalBarLabel =
    | "planning"
    | "executing"
    | "waiting"
    | "paused"
    | "paused-notool"
    | "paused-remote"
    | "budget"
    | "failed";

export type GoalBarView = {
    visible: boolean;
    running: boolean;
    label: GoalBarLabel;
    reason: string;
    objective: string;
    turns: string;
};

function goalText(value: unknown): string {
    return String(value ?? "").trim();
}

function goalCount(value: unknown): number {
    const parsed = typeof value === "number" ? value : Number(goalText(value));
    if (!Number.isFinite(parsed) || parsed < 0) return 0;
    return Math.trunc(parsed);
}

/** A failed or budget-stopped goal keeps the elapsed time it had when it ended. */
export function goalElapsedFrozen(input: GoalBarSnapshot | null | undefined): boolean {
    const status = goalText(input?.status).toLowerCase();
    return status === "failed" || status === "budget_limited" || status === "complete";
}

/**
 * End of the elapsed interval. A live goal, including a pause, uses now.
 * A finished goal uses stopped_at so the bar does not keep counting.
 */
export function goalElapsedEndMs(input: GoalBarSnapshot | null | undefined, nowMs: number): number {
    if (!goalElapsedFrozen(input)) return nowMs;
    const stopped = Number(input?.stopped_at);
    if (Number.isFinite(stopped) && stopped > 0) return stopped * 1000;
    const created = Number(input?.created_at);
    if (Number.isFinite(created) && created > 0) return created * 1000;
    return nowMs;
}

function goalClockHoldSeconds(input: GoalBarSnapshot | null | undefined): number {
    const hold = Number(input?.clock_hold_seconds);
    if (!Number.isFinite(hold) || hold <= 0) return 0;
    return Math.floor(hold);
}

/** Elapsed label for the composer bar. Terminal gaps in clock_hold_seconds stay out of the reading. */
export function goalElapsedText(input: GoalBarSnapshot | null | undefined, nowMs: number): string {
    const created = Number(input?.created_at);
    if (!Number.isFinite(created) || created <= 0) return "";
    return formatGoalElapsed(created + goalClockHoldSeconds(input), goalElapsedEndMs(input, nowMs));
}

/** Wall clock since the goal was created, in the Codex `3h 33m 35s` form. Pass the end instant from goalElapsedEndMs. */
export function formatGoalElapsed(createdAtUnix: number, nowMs: number): string {
    if (!Number.isFinite(createdAtUnix) || createdAtUnix <= 0 || !Number.isFinite(nowMs)) return "";
    const seconds = Math.max(0, Math.floor(nowMs / 1000 - createdAtUnix));
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const rest = seconds % 60;
    if (hours > 0) return `${hours}h ${minutes}m ${rest}s`;
    if (minutes > 0) return `${minutes}m ${rest}s`;
    return `${rest}s`;
}

function firstLine(text: string): string {
    return text.split(/\r?\n/).map(part => part.trim()).find(Boolean) || "";
}

function pausedLabel(summary: string): GoalBarLabel {
    const text = summary.toLowerCase();
    if (text.includes("no tool") || text.includes("auto-paused") || summary.includes("没有工具") || summary.includes("无工具")) {
        return "paused-notool";
    }
    if (text.includes("ssh") || summary.includes("远程")) {
        return "paused-remote";
    }
    return "paused";
}

/** What the composer goal bar shows. The transcript checklist is a separate card. */
export function goalBarView(input: GoalBarSnapshot | null | undefined): GoalBarView {
    const empty: GoalBarView = { visible: false, running: false, label: "paused", reason: "", objective: "", turns: "" };
    if (!input || input.visible === false) return empty;
    const status = goalText(input.status).toLowerCase();
    if (!status || status === "cleared" || status === "complete") return empty;
    const objective = goalText(input.objective);
    const goalID = goalText(input.goal_id);
    if (!objective && !goalID) return empty;
    const summary = goalText(input.summary);
    const awaiting = input.awaiting_user === true;
    // Waiting for a reply is still an active goal. Pause must stay available.
    // Continue is only for a goal that has already stopped.
    const running = status === "active";
    let label: GoalBarLabel = "paused";
    let reason = "";
    if (awaiting) {
        label = "waiting";
    } else if (status === "active") {
        const phase = goalText(input.phase).toLowerCase();
        label = phase === "execute" ? "executing" : "planning";
    } else if (status === "budget_limited") {
        label = "budget";
        reason = firstLine(summary);
    } else if (status === "failed") {
        label = "failed";
        reason = firstLine(summary);
    } else {
        label = pausedLabel(summary);
        reason = firstLine(summary);
    }
    const maxTurns = goalCount(input.max_turns);
    const turns = maxTurns > 0 ? `${goalCount(input.turns_used)}/${maxTurns}` : "";
    return { visible: true, running, label, reason, objective, turns };
}
