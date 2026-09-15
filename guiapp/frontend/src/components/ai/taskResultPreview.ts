import type { CodeFile } from "./useCodePreviewState";
import { filePreviewKindFromName } from "../preview/filePreviewKind";

/** Custom event the task-result card fires so the assistant panel can open preview. */
export const PREVIEW_TASK_RESULT_EVENT = "maclaw:preview-task-result";
/** Custom event so the assistant panel can focus the composer for a follow-up edit. */
export const CONTINUE_EDIT_TASK_RESULT_EVENT = "maclaw:continue-edit-task-result";

export type TaskResultPreviewKind = "pptx" | "pdf" | "image" | "video" | "audio" | "text";

export type TaskResultPreviewPayload = {
    path: string;
    file_name?: string;
    kind?: TaskResultPreviewKind | string;
    language?: string;
    content?: string;
    data_url?: string;
    preview_url?: string;
    truncated?: boolean;
};

export type PreviewTaskResultDetail = {
    path: string;
    messageId?: string;
};

export function isPdfFileName(name: string): boolean {
    return name.toLowerCase().endsWith(".pdf");
}

export const TASK_RESULT_PDF_PREVIEW_PATH = "/maclaw-preview/v1/file";

export function isPdfInlineDataURL(value: string): boolean {
    return value.startsWith("data:application/" + "pdf");
}

export function isTaskResultPdfPreviewURL(value: string): boolean {
    const raw = String(value || "").trim();
    if (!raw.startsWith("/") || raw.startsWith("//")) return false;
    try {
        const url = new URL(raw, "https://maclaw.local");
        if (url.pathname !== TASK_RESULT_PDF_PREVIEW_PATH) return false;
        return /^[0-9a-fA-F]{32}$/.test(url.searchParams.get("t") || "");
    } catch {
        return false;
    }
}

export function pdfInlineDataURLFromBase64(payload: string): string {
    return "data:" + "application/" + "pdf" + ";base64," + payload;
}

// "text" is never returned here: a text result still needs the backend payload,
// so an unknown extension falls through to "".
export function taskResultPreviewKindFromPath(path: string): Exclude<TaskResultPreviewKind, "text"> | "" {
    const kind = filePreviewKindFromName(path);
    if (kind === "pptx" || kind === "pdf" || kind === "image" || kind === "video" || kind === "audio") return kind;
    return "";
}

export function dispatchPreviewTaskResult(path: string, messageId?: string): void {
    const cleaned = String(path || "").trim();
    if (!cleaned || typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(PREVIEW_TASK_RESULT_EVENT, {
        detail: { path: cleaned, messageId } satisfies PreviewTaskResultDetail,
    }));
}

export function previewTaskResultPathFromEvent(event: Event): string {
    const detail = (event as CustomEvent<PreviewTaskResultDetail>).detail;
    return String(detail?.path || "").trim();
}

export function dispatchContinueEditTaskResult(path: string, messageId?: string): void {
    const cleaned = String(path || "").trim();
    if (!cleaned || typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(CONTINUE_EDIT_TASK_RESULT_EVENT, {
        detail: { path: cleaned, messageId } satisfies PreviewTaskResultDetail,
    }));
}

export function continueEditTaskResultPathFromEvent(event: Event): string {
    return previewTaskResultPathFromEvent(event);
}

export function continueEditTaskResultDraft(path: string, lang: string): string {
    const name = fileNameFromPath(path);
    if (lang === "en") return `Please continue editing the document "${name}" (${path}):\n`;
    if (lang === "zh-Hant") return `請繼續修改文件「${name}」（${path}）：\n`;
    return `请继续修改文档「${name}」（${path}）：\n`;
}

function pathMentionKey(path: string): string {
    return String(path || "").replace(/\\/g, "/");
}

export function composerMentionsTaskResult(path: string, existing: string): boolean {
    const current = String(existing || "");
    if (!current) return false;
    const needle = pathMentionKey(path);
    if (needle && pathMentionKey(current).includes(needle)) return true;
    const name = fileNameFromPath(path);
    if (!name) return false;
    return current.includes(`「${name}」`) || current.includes(`"${name}"`) || current.includes(`'${name}'`);
}

/** Merge a continue-edit prompt into the composer without wiping typed text. */
export function applyContinueEditTaskResultDraft(path: string, lang: string, existing: string): string {
    const draft = continueEditTaskResultDraft(path, lang);
    const current = String(existing || "");
    if (!current.trim()) return draft;
    if (composerMentionsTaskResult(path, current)) return current;
    return current.replace(/\s+$/u, "") + "\n\n" + draft;
}

export function localizeTaskResultExportError(message: string, lang: string): string {
    const zh = lang.startsWith("zh");
    const raw = String(message || "").replace(/^Error:\s*/i, "").trim();
    const keys = [
        "文件路径无效",
        "文件路径为空",
        "文件不存在",
        "不是文件",
        "文件过大，无法导出",
        "无法读取文件",
        "无法保存文件",
    ] as const;
    let matched = raw;
    for (const key of keys) {
        if (raw === key || raw.endsWith(key)) {
            matched = key;
            break;
        }
    }
    switch (matched) {
        case "文件路径无效":
        case "文件路径为空":
            return zh ? "文件路径无效" : "The file path is invalid";
        case "文件不存在":
            return zh ? "文件不存在" : "The file does not exist";
        case "不是文件":
            return zh ? "不是文件" : "That path is not a file";
        case "文件过大，无法导出":
            return zh ? "文件过大，无法导出" : "The file is too large to export";
        case "无法读取文件":
        case "无法保存文件":
            return zh ? "无法导出该文件" : "This file cannot be exported";
        default:
            return raw || message;
    }
}

function fileNameFromPath(path: string, explicit?: string): string {
    return String(explicit || path.split(/[/\\]/).pop() || path);
}

function taskResultCodeFile(
    path: string,
    fileName: string,
    language: string,
    content: string,
    truncated = false,
): CodeFile {
    return {
        filePath: path,
        fileName,
        absPath: path,
        content,
        language,
        opType: "read",
        updatedAt: Date.now(),
        previewTruncated: truncated || undefined,
    };
}

/** Immediate visual tab so the pane opens without waiting on the backend. */
export function codeFileForImmediateTaskResultPreview(
    path: string,
    kind: "pptx" | "pdf" | "image" | "video" | "audio",
): CodeFile {
    return taskResultCodeFile(path, fileNameFromPath(path), kind, "");
}

export function codeFileFromTaskResultPreview(preview: TaskResultPreviewPayload, fallbackPath: string): CodeFile {
    const path = String(preview?.path || fallbackPath || "").trim();
    const fileName = fileNameFromPath(path, preview?.file_name);
    const kind = String(preview?.kind || "").trim().toLowerCase();
    if (kind === "pptx" || kind === "pdf" || kind === "image" || kind === "video" || kind === "audio") {
        return taskResultCodeFile(path, fileName, kind, "");
    }
    return taskResultCodeFile(
        path,
        fileName,
        String(preview?.language || "plaintext"),
        String(preview?.content || ""),
        preview?.truncated === true,
    );
}

export function objectURLFromPdfDataURL(dataUrl: string): string {
    if (!isPdfInlineDataURL(dataUrl)) return "";
    try {
        const comma = dataUrl.indexOf(",");
        const payload = comma >= 0 ? dataUrl.slice(comma + 1) : "";
        const binary = atob(payload);
        const bytes = Uint8Array.from(binary, (ch) => ch.charCodeAt(0));
        return URL.createObjectURL(new Blob([bytes], { type: "application/" + "pdf" }));
    } catch {
        return dataUrl;
    }
}

export function localizeTaskResultPreviewError(message: string, lang: string): string {
    const zh = lang.startsWith("zh");
    switch (String(message || "").trim()) {
        case "文件路径无效":
        case "文件路径为空":
            return zh ? "文件路径无效" : "The file path is invalid";
        case "文件不存在":
            return zh ? "文件不存在" : "The file does not exist";
        case "不是文件":
            return zh ? "不是文件" : "That path is not a file";
        case "文件过大，无法预览":
            return zh ? "文件过大，无法预览" : "The file is too large to preview";
        case "不是有效的 PDF 文件":
            return zh ? "不是有效的 PDF 文件" : "This is not a valid PDF file";
        case "无法打开文件":
        case "无法读取文件":
        case "无法预览该文档":
        case "无法预览该文件":
            return zh ? "无法预览该文件" : "This file cannot be previewed";
        default:
            return message;
    }
}
