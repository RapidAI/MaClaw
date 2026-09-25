import { isWindowDragExcludedTarget } from "../../utils/windowDrag";
import { localizeText } from "./aiAssistantI18n";

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
    pendingUnfinishedStatus: string;
}): TaskExecutionStatus {
    const raw = input.raw || "";
    const lang = input.lang;
    if (/(fail|error|blocked|verify_failed|失败|错误|阻塞)/.test(raw)) {
        return { label: localizeText(lang, "Failed", "失败", "失敗"), tone: "failed" };
    }
    if (/(^|[\\s_])(paused|pause|已暂停|暂停)(?=$|[\\s_])/.test(raw)) {
        return { label: localizeText(lang, "Paused", "已暂停", "已暫停"), tone: "pending" };
    }
    if (input.pendingReview || /(waiting[_ ]?confirm|review|approval|pending|待确认|待处理|审核|审批)/.test(raw)) {
        return { label: localizeText(lang, "Pending", "待处理", "待處理"), tone: "pending" };
    }
    if (input.cancelPending) {
        return { label: localizeText(lang, "Stopping", "正在停止", "正在停止"), tone: "pending" };
    }
    if (/(cancel|canceled|cancelled|stopped|已取消|已停止)/.test(raw)) {
        return { label: localizeText(lang, "Cancelled", "已取消", "已取消"), tone: "cancelled" };
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
