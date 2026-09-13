import { isWindowDragExcludedTarget } from "../../utils/windowDrag";

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
