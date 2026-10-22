import type { TaskType } from "./taskDraft";

/** Mirrors guiapp.NewTaskTypeSuggestion. An empty or unconfident reading keeps the selected type. */
export interface NewTaskTypeSuggestion {
    suggested?: string;
    confident?: boolean;
    reason?: string;
    source?: string;
}

export interface TaskTypeMismatchCopy {
    title: string;
    message: string;
    confirmText: string;
    cancelText: string;
}

/**
 * The other wizard type, when the classifier is sure the command belongs there.
 * The same type, an unsure reading, and an unknown label return null so the
 * create continues with the type the user already picked.
 */
export function mismatchedTaskType(
    current: TaskType,
    suggestion: NewTaskTypeSuggestion | null | undefined,
): TaskType | null {
    if (!suggestion?.confident) return null;
    const suggested = suggestion.suggested === "coding" || suggestion.suggested === "chat"
        ? suggestion.suggested
        : null;
    if (!suggested || suggested === current) return null;
    return suggested;
}

/**
 * Confirm copy for a type change. Both buttons still create the task:
 * confirm switches the type, cancel keeps the one already selected.
 * dropsCloud is the coding rule that a cloud workspace cannot come along.
 */
export function taskTypeMismatchCopy(args: {
    current: TaskType;
    suggested: TaskType;
    dropsCloud: boolean;
    isZh: boolean;
}): TaskTypeMismatchCopy {
    const { current, suggested, dropsCloud, isZh } = args;
    if (isZh) {
        const next = suggested === "coding" ? "编程" : "会话";
        const keep = current === "coding" ? "编程" : "会话";
        const cloud = dropsCloud ? "\n编程任务不能使用云端工作区，确认后会改到本地目录。" : "";
        return {
            title: suggested === "coding" ? "改为编程任务？" : "改为会话任务？",
            message: `这条命令更像${next}。当前类型是「${keep}」。要改为「${next}」后再创建吗？${cloud}`,
            confirmText: `改为${next}`,
            cancelText: `保持${keep}`,
        };
    }
    const next = suggested === "coding" ? "Coding" : "Chat";
    const keep = current === "coding" ? "Coding" : "Chat";
    const cloud = dropsCloud
        ? "\nCoding tasks cannot use a cloud workspace. Confirming moves this task to a local directory."
        : "";
    return {
        title: suggested === "coding" ? "Switch this task to coding?" : "Switch this task to chat?",
        message: `This command fits ${next.toLowerCase()} better. The current type is “${keep}”. Switch to “${next}”, then create the task?${cloud}`,
        confirmText: `Switch to ${next.toLowerCase()}`,
        cancelText: `Keep ${keep.toLowerCase()}`,
    };
}
