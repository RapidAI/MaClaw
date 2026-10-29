import type { TaskType } from "./taskDraft";
import { tailText } from "../../../i18n/mnTail";
import { normalizeLang } from "../../../i18n/langSelect";

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
    isZh: boolean | string;
}): TaskTypeMismatchCopy {
    const { current, suggested, dropsCloud, isZh } = args;
    const lang = typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en");
    const chinese = typeof isZh === "string"
        ? (normalizeLang(isZh) === "zh-Hans" || normalizeLang(isZh) === "zh-Hant")
        : !!isZh;
    if (chinese) {
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
        ? tailText(lang, "\nCoding tasks cannot use a cloud workspace. Confirming moves this task to a local directory.", "\n编程任务不能使用云端工作区，确认后会改到本地目录。")
        : "";
    const nextLower = next.toLowerCase();
    const keepLower = keep.toLowerCase();
    return {
        title: tailText(lang, suggested === "coding" ? "Switch this task to coding?" : "Switch this task to chat?", suggested === "coding" ? "改为编程任务？" : "改为会话任务？"),
        message: tailText(lang, `This command fits ${nextLower} better. The current type is “${keep}”. Switch to “${next}”, then create the task?`, `这条命令更像${suggested === "coding" ? "编程" : "会话"}。当前类型是「${current === "coding" ? "编程" : "会话"}」。要改为「${suggested === "coding" ? "编程" : "会话"}」后再创建吗？`) + cloud,
        confirmText: tailText(lang, `Switch to ${nextLower}`, `改为${suggested === "coding" ? "编程" : "会话"}`),
        cancelText: tailText(lang, `Keep ${keepLower}`, `保持${current === "coding" ? "编程" : "会话"}`),
    };
}
