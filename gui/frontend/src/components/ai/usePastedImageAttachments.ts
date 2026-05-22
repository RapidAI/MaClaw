import { useCallback, useEffect, useRef, useState } from "react";
import type { AttachmentInfo } from "./useBufferQueue";

async function savePastedImage(base64: string, ext: string): Promise<string> {
    const w = window as any;
    if (typeof window !== "undefined" && w.go?.main?.App?.SavePastedImage) {
        return w.go.main.App.SavePastedImage(base64, ext);
    }
    throw new Error("SavePastedImage binding not available");
}

async function savePastedFile(base64: string, fileName: string, mimeType: string): Promise<string> {
    const w = window as any;
    if (typeof window !== "undefined" && w.go?.main?.App?.SavePastedFile) {
        return w.go.main.App.SavePastedFile(base64, fileName, mimeType);
    }
    throw new Error("SavePastedFile binding not available");
}

const IMAGE_EXTENSIONS = new Set([".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico", ".tif", ".tiff"]);
const MAX_PATHLESS_IMAGE_BYTES = 37 * 1024 * 1024;
const MAX_PATHLESS_FILE_BYTES = 75 * 1024 * 1024;

function fileNameFromPath(filePath: string): string {
    return filePath.split(/[/\\]/).pop() || filePath;
}

function extensionFromName(fileName: string, fallback = ""): string {
    const match = fileName.match(/\.[^./\\]+$/);
    if (match) return match[0].toLowerCase();
    return fallback ? `.${fallback.replace(/^\./, "").toLowerCase()}` : "";
}

function imageExtensionFromMimeType(mimeType: string): string {
    if (mimeType === "image/png") return "png";
    if (mimeType === "image/gif") return "gif";
    if (mimeType === "image/webp") return "webp";
    if (mimeType === "image/bmp") return "bmp";
    if (mimeType === "image/svg+xml") return "svg";
    return "jpg";
}

function isImageFile(file: File): boolean {
    if (file.type.startsWith("image/")) return true;
    return IMAGE_EXTENSIONS.has(extensionFromName(file.name));
}

function clipboardFilePath(file: File): string {
    const candidate = (file as File & { path?: unknown }).path;
    return typeof candidate === "string" ? candidate.trim() : "";
}

function readFileBase64(file: Blob): Promise<string> {
    return new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result || "").split(",")[1] || "");
        reader.onerror = reject;
        reader.readAsDataURL(file);
    });
}

function ensurePathlessFileSizeAllowed(file: File, image: boolean) {
    const limit = image ? MAX_PATHLESS_IMAGE_BYTES : MAX_PATHLESS_FILE_BYTES;
    if (file.size > limit) {
        throw new Error(`pasted ${image ? "image" : "file"} is too large to copy (${file.size} bytes)`);
    }
}

function uniqueClipboardFiles(e: React.ClipboardEvent<HTMLTextAreaElement>): File[] {
    const files: File[] = [];
    const seen = new Set<string>();
    const addFile = (file: File | null) => {
        if (!file) return;
        const key = [file.name, file.type, file.size, file.lastModified].join("|");
        if (seen.has(key)) return;
        seen.add(key);
        files.push(file);
    };

    const items = e.clipboardData?.items;
    if (items) {
        for (const item of Array.from(items)) {
            if (item.kind === "file") addFile(item.getAsFile());
        }
    }
    const clipboardFiles = e.clipboardData?.files;
    if (clipboardFiles) {
        for (const file of Array.from(clipboardFiles)) addFile(file);
    }
    return files;
}

export function usePastedImageAttachments() {
    const [pendingAttachments, setPendingAttachments] = useState<AttachmentInfo[]>([]);
    const objectUrlsRef = useRef<Set<string>>(new Set());

    useEffect(() => {
        const activeUrls = new Set(
            pendingAttachments
                .map(att => att.thumbnailDataUrl)
                .filter((url): url is string => typeof url === "string" && url.startsWith("blob:")),
        );
        for (const url of Array.from(objectUrlsRef.current)) {
            if (!activeUrls.has(url)) {
                URL.revokeObjectURL(url);
                objectUrlsRef.current.delete(url);
            }
        }
        for (const url of activeUrls) objectUrlsRef.current.add(url);
    }, [pendingAttachments]);

    useEffect(() => () => {
        for (const url of objectUrlsRef.current) URL.revokeObjectURL(url);
        objectUrlsRef.current.clear();
    }, []);

    const handlePaste = useCallback(async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
        const files = uniqueClipboardFiles(e);
        if (files.length === 0) return;
        e.preventDefault();

        for (const file of files) {
            const fileName = file.name || "pasted-file";
            const directPath = clipboardFilePath(file);
            const image = isImageFile(file);
            try {
                if (directPath) {
                    setPendingAttachments(prev => [...prev, {
                        filePath: directPath,
                        thumbnailDataUrl: image ? URL.createObjectURL(file) : undefined,
                        isImage: image,
                        fileName: fileNameFromPath(directPath),
                        extension: extensionFromName(directPath),
                    }]);
                    continue;
                }

                ensurePathlessFileSizeAllowed(file, image);
                const base64 = await readFileBase64(file);
                if (image) {
                    const ext = imageExtensionFromMimeType(file.type);
                    const filePath = await savePastedImage(base64, ext);
                    setPendingAttachments(prev => [...prev, { filePath, thumbnailDataUrl: URL.createObjectURL(file), isImage: true, fileName: fileNameFromPath(filePath), extension: `.${ext}` }]);
                    continue;
                }

                const filePath = await savePastedFile(base64, fileName, file.type || "application/octet-stream");
                setPendingAttachments(prev => [...prev, { filePath, isImage: false, fileName: fileNameFromPath(filePath), extension: extensionFromName(filePath) }]);
            } catch (err) {
                console.error("Failed to attach pasted file:", err);
            }
        }
    }, []);

    return { handlePaste, pendingAttachments, setPendingAttachments };
}
