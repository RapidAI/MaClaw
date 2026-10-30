import { intlLocale } from "../../i18n/langSelect";
import { isWindowDragExcludedTarget } from "../../utils/windowDrag";
import { localizeText } from "./aiAssistantI18n";
import { normalizeProjectSessionPath } from "./assistantSessionPath";
import { chatTaskTerminal } from "./chatTaskModel";
import { codingStepIsDone } from "./codingStepStatus";
import { leadingSnapshotStatusCompleted, snapshotRawReportsProgress } from "./taskExecutionSnapshot";

export type TaskExecutionStatusTone = "failed" | "pending" | "cancelled" | "running" | "completed";

export interface TaskExecutionStatus {
    label: string;
    tone: TaskExecutionStatusTone;
}

/** Header badge for the open task. An unresolved recovery card is not a completed run. */
export function resolveTaskExecutionStatus(input: {
    lang: string;
    raw: string;
    pendingReview: boolean;
    cancelPending: boolean;
    /** A turn is still working, including a round that paints this transcript under another session key. */
    busy: boolean;
    /**
     * Local work that is not a run: a tab restore/prepare, or a plan phase the
     * UI still paints as executing. It keeps the badge in progress only while
     * the durable snapshot carries no paused/interrupted/cancelled outcome;
     * otherwise opening an idle task would claim it is running again.
     */
    backgroundPhaseActive?: boolean;
    hasOutput: boolean;
    hasMessages: boolean;
    workflowActive: boolean;
    codingStepCount: number;
    /** Every coding step finished successfully. A live approval still keeps the badge pending. */
    codingStepsAllPassed?: boolean;
    /** A real workflow review is waiting. Unlike a ledger gate block, the user has an action. */
    awaitingUserReview?: boolean;
    pendingUnfinishedStatus: string;
    /** The latest assistant turn finished with a loop error. Idle chat with messages is not completion. */
    turnIncomplete?: boolean;
    /**
     * A background chat task is still this task's work. The foreground turn
     * has already returned, so message history alone would say completed.
     * "running" covers queued work. "needs_input" is waiting on the user.
     */
    openChatWork?: "" | "running" | "needs_input";
}): TaskExecutionStatus {
    const raw = input.raw || "";
    const lang = input.lang;
    // The task list buckets on the leading workflow status. Once that word is
    // a finished success, a later step or phase token must not reopen it.
    const settledComplete = durableExecutionStatusCompleted(raw);
    // Live foreground work is the current outcome. A failed, cancelled, or
    // interrupted token left in the snapshot (or a recovery card from an
    // earlier turn) describes the previous attempt; while the turn is still
    // working, the badge must say in progress rather than contradict the
    // reasoning trail that is still streaming.
    const liveWork = input.busy === true;
    // Failure still beats pause and an in-flight stop. A live worker wins, so
    // a failed token left in the snapshot does not hide it. A leading
    // completed status is the list's outcome; a later failed step is not.
    if (!settledComplete && !input.openChatWork && !liveWork && /(fail|error|blocked|verify_failed|失败|错误|阻塞)/.test(raw)) {
        return { label: localizeText(lang, "Failed", "失败", "失敗"), tone: "failed" };
    }
    if (input.cancelPending) {
        return { label: localizeText(lang, "Stopping", "正在停止", "正在停止"), tone: "pending" };
    }
    // The handed-off worker is the task now. A durable snapshot that still
    // says failed, cancelled, or interrupted is the previous outcome.
    if (input.openChatWork === "running") {
        return { label: localizeText(lang, "In progress", "进行中", "進行中"), tone: "running" };
    }
    if (input.openChatWork === "needs_input") {
        return { label: localizeText(lang, "Pending", "待处理", "待處理"), tone: "pending" };
    }
    // A live round outranks every durable outcome token below it. Interrupted
    // and cancelled markers can linger from the resume/recovery card that the
    // current turn continued from; reporting them here made the header say
    // 已中断 while the coding thought was still streaming.
    if (liveWork) {
        return { label: localizeText(lang, "In progress", "进行中", "進行中"), tone: "running" };
    }
    // Only the leading status token counts. A later phase named "pause" must
    // not turn a finished run into a paused one.
    if (/^(paused|pause|已暂停|暂停)(?=$|[\s_])/.test(raw)) {
        return { label: localizeText(lang, "Paused", "已暂停", "已暫停"), tone: "pending" };
    }
    if (!settledComplete && /(cancel|canceled|cancelled|stopped|已取消|已停止)/.test(raw)) {
        return { label: localizeText(lang, "Cancelled", "已取消", "已取消"), tone: "cancelled" };
    }
    // An interrupt can be resumed. It is not a review request, and passed
    // coding steps do not make the run completed. A durable completed status
    // is the later outcome: a recovery card left in the transcript belongs
    // to the attempt that already finished.
    if (!settledComplete && /(^|[\s_])interrupted(?=$|[\s_])/.test(raw)) {
        return { label: localizeText(lang, "Interrupted", "已中断", "已中斷"), tone: "pending" };
    }
    // The list files active / execute / execution as in progress before it
    // looks at a phase named review. A real approval is the pending_review
    // flag: the status word stays "active" while the user still has to
    // confirm, and that flag wins over the progress word.
    if (!settledComplete && (input.pendingReview || input.awaitingUserReview)) {
        return { label: localizeText(lang, "Pending", "待处理", "待處理"), tone: "pending" };
    }
    if (!settledComplete && (input.busy || snapshotRawReportsProgress(raw))) {
        return { label: localizeText(lang, "In progress", "进行中", "進行中"), tone: "running" };
    }
    // Finished coding steps are the task outcome. A phase name that merely
    // contains "review" must not keep the badge pending. A live approval flag
    // still does: that one is waiting on the user.
    if (input.codingStepsAllPassed && !input.pendingReview && !input.awaitingUserReview && !input.pendingUnfinishedStatus) {
        if (input.busy) {
            return { label: localizeText(lang, "In progress", "进行中", "進行中"), tone: "running" };
        }
        return { label: localizeText(lang, "Completed", "已完成", "已完成"), tone: "completed" };
    }
    // The leading status is the same word the task list buckets on. A review
    // flag or a later "pending" token belongs to an earlier phase once that
    // status is already a finished success.
    if (!settledComplete && (input.pendingReview || /(waiting[_ ]?confirm|review|approval|pending|待确认|待处理|审核|审批)/.test(raw))) {
        return { label: localizeText(lang, "Pending", "待处理", "待處理"), tone: "pending" };
    }
    if (!settledComplete && input.pendingUnfinishedStatus) {
        if (input.pendingUnfinishedStatus === "interrupted") {
            return { label: localizeText(lang, "Interrupted", "已中断", "已中斷"), tone: "pending" };
        }
        return { label: localizeText(lang, "Unfinished", "未完成", "未完成"), tone: "pending" };
    }
    // A hard exit still leaves the model's sentence in the transcript. That
    // sentence is not a checked finish, so an idle chat must not say completed.
    // A durable completed status is the later outcome and stays completed.
    if (!settledComplete && input.turnIncomplete) {
        return { label: localizeText(lang, "Unfinished", "未完成", "未完成"), tone: "pending" };
    }
    // A restored plan phase or an in-flight tab prepare is not a run. It may
    // answer "in progress" only after every explicit outcome above: those are
    // what the sidebar row shows, and the two surfaces must agree.
    if (!settledComplete && input.backgroundPhaseActive) {
        return { label: localizeText(lang, "In progress", "进行中", "進行中"), tone: "running" };
    }
    if (/(complete|completed|finish|success|succeed|done|passed|已完成|完成|成功)/.test(raw) || input.hasOutput || (input.hasMessages && !input.workflowActive && input.codingStepCount === 0)) {
        return { label: localizeText(lang, "Completed", "已完成", "已完成"), tone: "completed" };
    }
    return { label: localizeText(lang, "Pending", "待处理", "待處理"), tone: "pending" };
}

/** The leading workflow status is a finished success. Step words such as
 * "passed" are not that status: they follow the workflow token. */
function durableExecutionStatusCompleted(raw: string): boolean {
    return leadingSnapshotStatusCompleted(raw);
}

// foldChatSessionKey makes a backend session key and a tab session key comparable.
// It uses the transcript's project-path clean, so a dotted segment, padding, or
// a trailing separator cannot leave the card visible while the header says completed.
// Expert and ACP ids are not paths and stay as written.
function foldChatSessionKey(sessionKey: string | undefined): string {
    const key = String(sessionKey || "").trim();
    const prefix = "desktop-user:";
    if (!key.startsWith(prefix)) return key;
    if (key.startsWith(`${prefix}expert:`) || key.startsWith(`${prefix}acp:`)) return key;
    const path = normalizeProjectSessionPath(key.slice(prefix.length));
    return path ? `${prefix}${path}` : "desktop-user";
}

/** Same chat after path folding. C:\ and C:/ are one task, not two. */
export function chatTaskInSession(taskSessionKey: string | undefined, activeSessionKey: string): boolean {
    const active = foldChatSessionKey(activeSessionKey);
    if (!active) return false;
    return foldChatSessionKey(taskSessionKey) === active;
}

/** Open background work for this chat. */
export function openChatWorkForSession(
    tasks: Array<{ sessionKey?: string; status?: string }>,
    activeSessionKey: string,
): "" | "running" | "needs_input" {
    let waiting = false;
    for (const task of tasks) {
        if (!chatTaskInSession(task.sessionKey, activeSessionKey)) continue;
        if (chatTaskTerminal(String(task.status || ""))) continue;
        if (task.status === "needs_input") {
            waiting = true;
            continue;
        }
        return "running";
    }
    return waiting ? "needs_input" : "";
}

/** True when every coding step reached a successful terminal status. */
export function codingStepsAllPassed(steps: Array<{ status?: string }> | undefined): boolean {
    if (!steps || steps.length === 0) return false;
    // Shares the vocabulary with the plan checklist: adding a backend spelling
    // to codingStepStatus.ts keeps the badge and the checklist in sync.
    return steps.every((step) => codingStepIsDone(String(step?.status || "")));
}

/** Shared chrome helpers for the task execution surface. */
export const executionSecondaryChromeStyle = {
    position: "absolute" as const,
    width: 1,
    height: 1,
    padding: 0,
    margin: -1,
    overflow: "hidden" as const,
    clip: "rect(0, 0, 0, 0)",
    whiteSpace: "nowrap" as const,
    border: 0,
    pointerEvents: "none" as const,
};

/** Skip window restore when the double-click landed on a control in the task header. */
export function isTaskExecutionHeaderInteractiveTarget(target: EventTarget | null, currentTarget: EventTarget | null): boolean {
    if (!(currentTarget instanceof Element)) return false;
    // SVG glyphs inside buttons are Element, not HTMLElement.
    if (!(target instanceof Element) || target === currentTarget) return false;
    return isWindowDragExcludedTarget(target);
}

export function handleTaskExecutionHeaderDoubleClick(
    event: { target: EventTarget | null; currentTarget: EventTarget | null; preventDefault(): void },
    onToggleMaximize?: () => void,
): void {
    if (!onToggleMaximize) return;
    if (isTaskExecutionHeaderInteractiveTarget(event.target, event.currentTarget)) return;
    event.preventDefault();
    onToggleMaximize();
}

/** Format a task's first message timestamp for the execution header. */
export function formatTaskCreatedAt(timestamp: number | undefined, lang: string): string {
    if (!Number.isFinite(timestamp)) return "";
    const raw = Number(timestamp);
    // Restored histories may use Unix seconds while live messages use milliseconds.
    const date = new Date(raw < 1_000_000_000_000 ? raw * 1000 : raw);
    if (Number.isNaN(date.getTime())) return "";
    return new Intl.DateTimeFormat(intlLocale(lang), {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
    }).format(date);
}
