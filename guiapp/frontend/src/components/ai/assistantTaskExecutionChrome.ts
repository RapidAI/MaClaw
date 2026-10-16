import { isWindowDragExcludedTarget } from "../../utils/windowDrag";
import { localizeText } from "./aiAssistantI18n";
import { codingStepIsDone } from "./codingStepStatus";

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
    busy: boolean;
    hasOutput: boolean;
    hasMessages: boolean;
    workflowActive: boolean;
    codingStepCount: number;
    /** Every coding step finished successfully. A live approval still keeps the badge pending. */
    codingStepsAllPassed?: boolean;
    /** A real workflow review is waiting. Unlike a ledger gate block, the user has an action. */
    awaitingUserReview?: boolean;
    pendingUnfinishedStatus: string;
}): TaskExecutionStatus {
    const raw = input.raw || "";
    const lang = input.lang;
    if (/(fail|error|blocked|verify_failed|失败|错误|阻塞)/.test(raw)) {
        return { label: localizeText(lang, "Failed", "失败", "失敗"), tone: "failed" };
    }
    // Only the leading status token counts. A later phase named "pause" must
    // not turn a finished run into a paused one.
    if (/^(paused|pause|已暂停|暂停)(?=$|[\s_])/.test(raw)) {
        return { label: localizeText(lang, "Paused", "已暂停", "已暫停"), tone: "pending" };
    }
    if (input.cancelPending) {
        return { label: localizeText(lang, "Stopping", "正在停止", "正在停止"), tone: "pending" };
    }
    if (/(cancel|canceled|cancelled|stopped|已取消|已停止)/.test(raw)) {
        return { label: localizeText(lang, "Cancelled", "已取消", "已取消"), tone: "cancelled" };
    }
    // An interrupt can be resumed. It is not a review request, and passed
    // coding steps do not make the run completed.
    if (/(^|[\s_])interrupted(?=$|[\s_])/.test(raw) || input.pendingUnfinishedStatus === "interrupted") {
        return { label: localizeText(lang, "Interrupted", "已中断", "已中斷"), tone: "pending" };
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
    if (input.pendingReview || /(waiting[_ ]?confirm|review|approval|pending|待确认|待处理|审核|审批)/.test(raw)) {
        return { label: localizeText(lang, "Pending", "待处理", "待處理"), tone: "pending" };
    }
    if (input.busy || /\b(running|execut|processing|in_progress|started|进行|执行|运行)\b/.test(raw)) {
        return { label: localizeText(lang, "In progress", "进行中", "進行中"), tone: "running" };
    }
    if (input.pendingUnfinishedStatus) {
        if (input.pendingUnfinishedStatus === "interrupted") {
            return { label: localizeText(lang, "Interrupted", "已中断", "已中斷"), tone: "pending" };
        }
        return { label: localizeText(lang, "Unfinished", "未完成", "未完成"), tone: "pending" };
    }
    if (/(complete|completed|finish|success|succeed|done|passed|已完成|完成|成功)/.test(raw) || input.hasOutput || (input.hasMessages && !input.workflowActive && input.codingStepCount === 0)) {
        return { label: localizeText(lang, "Completed", "已完成", "已完成"), tone: "completed" };
    }
    return { label: localizeText(lang, "Pending", "待处理", "待處理"), tone: "pending" };
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
    return new Intl.DateTimeFormat(lang?.startsWith("zh") ? "zh-CN" : "en-US", {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
    }).format(date);
}
