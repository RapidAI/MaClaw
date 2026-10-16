export type DisplayAttachmentType = "text" | "image" | "file";

const binaryDocumentMimes = new Set([
    "application/pdf",
    "application/msword",
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    "application/vnd.ms-powerpoint",
    "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    "application/vnd.ms-excel",
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
]);

/**
 * Remote attachment metadata is not a content-trust boundary. This is a
 * conservative display/router classification shared by live and history UIs:
 * a PDF/Office filename or MIME must never be promoted to an image preview.
 * Actual parser safety remains enforced by the native read_document route.
 */
export function isBinaryDocumentAttachment(filename: string | null | undefined, mimeType: string | null | undefined): boolean {
    if (/\.(pdf|doc|docx|ppt|pptx|xls|xlsx)$/i.test(String(filename || "").trim())) return true;
    const mime = String(mimeType || "").trim().toLowerCase().split(";", 1)[0];
    return binaryDocumentMimes.has(mime);
}

export function classifyDisplayAttachmentType(
    filename: string | null | undefined,
    mimeType: string | null | undefined,
    declaredType?: string | null,
): DisplayAttachmentType {
    if (isBinaryDocumentAttachment(filename, mimeType)) return "file";
    if (String(declaredType || "").trim().toLowerCase() === "image" || String(mimeType || "").trim().toLowerCase().startsWith("image/")) {
        return "image";
    }
    if (String(declaredType || "").trim().toLowerCase() === "text") return "text";
    return "file";
}
