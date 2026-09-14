import type { CodeFile } from "./useCodePreviewState";

/** Custom event the task-result card fires so the assistant panel can open preview. */
export const PREVIEW_TASK_RESULT_EVENT = "maclaw:preview-task-result";

export type TaskResultPreviewKind = "pptx" | "pdf" | "text";

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

export function taskResultPreviewKindFromPath(path: string): TaskResultPreviewKind | "" {
    const base = (path.split(/[/\\]/).pop() || path).toLowerCase();
    if (base.endsWith(".pptx")) return "pptx";
    if (base.endsWith(".pdf")) return "pdf";
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

/** Immediate PPTX/PDF tab so the pane opens without waiting on the backend. */
export function codeFileForImmediateTaskResultPreview(path: string, kind: "pptx" | "pdf"): CodeFile {
    return taskResultCodeFile(path, fileNameFromPath(path), kind, "");
}

export function codeFileFromTaskResultPreview(preview: TaskResultPreviewPayload, fallbackPath: string): CodeFile {
    const path = String(preview?.path || fallbackPath || "").trim();
    const fileName = fileNameFromPath(path, preview?.file_name);
    const kind = String(preview?.kind || "").trim().toLowerCase();
    if (kind === "pptx") return taskResultCodeFile(path, fileName, "pptx", "");
    if (kind === "pdf") return taskResultCodeFile(path, fileName, "pdf", "");
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
