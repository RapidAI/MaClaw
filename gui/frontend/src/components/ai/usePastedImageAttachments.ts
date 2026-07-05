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

function transferredFilePath(file: File): string {
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
        throw new Error(`attached ${image ? "image" : "file"} is too large to copy (${file.size} bytes)`);
    }
}

function uniqueFilesFromSources(items?: DataTransferItemList | null, fileList?: FileList | null): File[] {
    const files: File[] = [];
    const seen = new Set<string>();
    const addFile = (file: File | null) => {
        if (!file) return;
        const key = [transferredFilePath(file), file.name, file.type, file.size, file.lastModified].join("|");
        if (seen.has(key)) return;
        seen.add(key);
        files.push(file);
    };

    if (items) {
        for (const item of Array.from(items)) {
            if (item.kind === "file") addFile(item.getAsFile());
        }
    }
    if (fileList) {
        for (const file of Array.from(fileList)) addFile(file);
    }
    return files;
}

function uniqueClipboardFiles(e: React.ClipboardEvent<HTMLTextAreaElement>): File[] {
    return uniqueFilesFromSources(e.clipboardData?.items, e.clipboardData?.files);
}

function uniqueDroppedFiles(e: React.DragEvent<HTMLElement>): File[] {
    return uniqueFilesFromSources(e.dataTransfer?.items, e.dataTransfer?.files);
}

function hasTransferredFiles(dataTransfer?: DataTransfer | null): boolean {
    return Array.from(dataTransfer?.types || []).includes("Files");
}

function attachmentBlobUrls(attachments: AttachmentInfo[]): Set<string> {
    return new Set(
        attachments
            .map(att => att.thumbnailDataUrl)
            .filter((url): url is string => typeof url === "string" && url.startsWith("blob:")),
    );
}

const DEFAULT_ATTACHMENT_SESSION_KEY = "desktop-user";
const FORGET_SESSION_STATE_EVENT = "ai-assistant:forget-session-rounds";

interface AttachmentInputOptions {
    disabled?: boolean;
}

function normalizeAttachmentSessionKey(sessionKey?: string): string {
    const trimmed = typeof sessionKey === "string" ? sessionKey.trim() : "";
    return trimmed || DEFAULT_ATTACHMENT_SESSION_KEY;
}

export function usePastedImageAttachments(sessionKey = DEFAULT_ATTACHMENT_SESSION_KEY, options?: AttachmentInputOptions) {
    const activeSessionKey = normalizeAttachmentSessionKey(sessionKey);
    const disabled = options?.disabled === true;
    const [pendingAttachments, setVisiblePendingAttachments] = useState<AttachmentInfo[]>([]);
    const pendingAttachmentsBySessionRef = useRef<Map<string, AttachmentInfo[]>>(new Map());
    const sessionResetVersionsRef = useRef<Map<string, number>>(new Map());
    const activeSessionKeyRef = useRef(activeSessionKey);
    const mountedRef = useRef(true);
    const objectUrlsRef = useRef<Set<string>>(new Set());

    const sessionResetVersion = useCallback((session: string) => {
        return sessionResetVersionsRef.current.get(normalizeAttachmentSessionKey(session)) || 0;
    }, []);

    const bumpSessionResetVersion = useCallback((session: string) => {
        const targetSession = normalizeAttachmentSessionKey(session);
        sessionResetVersionsRef.current.set(targetSession, (sessionResetVersionsRef.current.get(targetSession) || 0) + 1);
    }, []);

    const syncObjectUrls = useCallback(() => {
        if (!mountedRef.current) return;
        const activeUrls = new Set<string>();
        for (const attachments of pendingAttachmentsBySessionRef.current.values()) {
            for (const url of attachmentBlobUrls(attachments)) activeUrls.add(url);
        }
        for (const url of Array.from(objectUrlsRef.current)) {
            if (!activeUrls.has(url)) {
                URL.revokeObjectURL(url);
                objectUrlsRef.current.delete(url);
            }
        }
        for (const url of activeUrls) objectUrlsRef.current.add(url);
    }, []);

    useEffect(() => {
        activeSessionKeyRef.current = activeSessionKey;
        setVisiblePendingAttachments(pendingAttachmentsBySessionRef.current.get(activeSessionKey) || []);
    }, [activeSessionKey]);

    useEffect(() => {
        const handler = (event: Event) => {
            const rawSessionKey = String((event as CustomEvent)?.detail?.sessionKey || '').trim();
            if (!rawSessionKey) return;
            const forgottenSessionKey = normalizeAttachmentSessionKey(rawSessionKey);
            bumpSessionResetVersion(forgottenSessionKey);
            if (pendingAttachmentsBySessionRef.current.has(forgottenSessionKey)) {
                console.info("[usePastedImageAttachments] clearing pending attachments for forgotten session", { sessionKey: forgottenSessionKey });
                pendingAttachmentsBySessionRef.current.delete(forgottenSessionKey);
                syncObjectUrls();
            }
            if (activeSessionKeyRef.current === forgottenSessionKey) {
                setVisiblePendingAttachments([]);
            }
        };
        window.addEventListener(FORGET_SESSION_STATE_EVENT, handler);
        return () => window.removeEventListener(FORGET_SESSION_STATE_EVENT, handler);
    }, [bumpSessionResetVersion, syncObjectUrls]);

    const setPendingAttachments = useCallback((next: AttachmentInfo[] | ((prev: AttachmentInfo[]) => AttachmentInfo[])) => {
        if (!mountedRef.current) return;
        const session = activeSessionKeyRef.current;
        const current = pendingAttachmentsBySessionRef.current.get(session) || [];
        const resolved = typeof next === "function" ? (next as (prev: AttachmentInfo[]) => AttachmentInfo[])(current) : next;
        const safeNext = Array.isArray(resolved) ? resolved : [];
        bumpSessionResetVersion(session);
        if (safeNext.length > 0) {
            pendingAttachmentsBySessionRef.current.set(session, safeNext);
        } else {
            pendingAttachmentsBySessionRef.current.delete(session);
        }
        syncObjectUrls();
        setVisiblePendingAttachments(safeNext);
    }, [bumpSessionResetVersion, syncObjectUrls]);

    const setPendingAttachmentsForSession = useCallback((session: string, next: AttachmentInfo[] | ((prev: AttachmentInfo[]) => AttachmentInfo[]), expectedResetVersion?: number): boolean => {
        if (!mountedRef.current) return false;
        const targetSession = normalizeAttachmentSessionKey(session);
        if (expectedResetVersion !== undefined && sessionResetVersion(targetSession) !== expectedResetVersion) return false;
        const current = pendingAttachmentsBySessionRef.current.get(targetSession) || [];
        const resolved = typeof next === "function" ? (next as (prev: AttachmentInfo[]) => AttachmentInfo[])(current) : next;
        const safeNext = Array.isArray(resolved) ? resolved : [];
        if (safeNext.length > 0) {
            pendingAttachmentsBySessionRef.current.set(targetSession, safeNext);
        } else {
            pendingAttachmentsBySessionRef.current.delete(targetSession);
            bumpSessionResetVersion(targetSession);
        }
        syncObjectUrls();
        if (activeSessionKeyRef.current === targetSession) {
            setVisiblePendingAttachments(safeNext);
        }
        return true;
    }, [bumpSessionResetVersion, sessionResetVersion, syncObjectUrls]);

    useEffect(() => () => {
        mountedRef.current = false;
        const sessions = new Set([...pendingAttachmentsBySessionRef.current.keys(), activeSessionKeyRef.current]);
        for (const session of sessions) bumpSessionResetVersion(session);
        pendingAttachmentsBySessionRef.current.clear();
        for (const url of objectUrlsRef.current) URL.revokeObjectURL(url);
        objectUrlsRef.current.clear();
    }, [bumpSessionResetVersion]);

    const attachFiles = useCallback(async (files: File[], source: "pasted" | "dropped") => {
        if (disabled || files.length === 0) return;
        const targetSession = activeSessionKeyRef.current;
        const targetResetVersion = sessionResetVersion(targetSession);

        for (const file of files) {
            const fileName = file.name || `${source}-file`;
            const directPath = transferredFilePath(file);
            const image = isImageFile(file);
            try {
                if (directPath) {
                    const thumbnailDataUrl = image ? URL.createObjectURL(file) : undefined;
                    const applied = setPendingAttachmentsForSession(targetSession, prev => [...prev, {
                        filePath: directPath,
                        thumbnailDataUrl,
                        isImage: image,
                        fileName: fileNameFromPath(directPath),
                        extension: extensionFromName(directPath),
                    }], targetResetVersion);
                    if (!applied && thumbnailDataUrl) URL.revokeObjectURL(thumbnailDataUrl);
                    continue;
                }

                ensurePathlessFileSizeAllowed(file, image);
                const base64 = await readFileBase64(file);
                if (image) {
                    const ext = imageExtensionFromMimeType(file.type);
                    const filePath = await savePastedImage(base64, ext);
                    const thumbnailDataUrl = URL.createObjectURL(file);
                    const applied = setPendingAttachmentsForSession(targetSession, prev => [...prev, { filePath, thumbnailDataUrl, isImage: true, fileName: fileNameFromPath(filePath), extension: `.${ext}` }], targetResetVersion);
                    if (!applied) URL.revokeObjectURL(thumbnailDataUrl);
                    continue;
                }

                const filePath = await savePastedFile(base64, fileName, file.type || "application/octet-stream");
                setPendingAttachmentsForSession(targetSession, prev => [...prev, { filePath, isImage: false, fileName: fileNameFromPath(filePath), extension: extensionFromName(filePath) }], targetResetVersion);
            } catch (err) {
                console.error(`Failed to attach ${source} file:`, err);
            }
        }
    }, [disabled, sessionResetVersion, setPendingAttachmentsForSession]);

    const handlePaste = useCallback((e: React.ClipboardEvent<HTMLTextAreaElement>) => {
        const files = uniqueClipboardFiles(e);
        if (files.length === 0) return;
        e.preventDefault();
        if (disabled) return;
        void attachFiles(files, "pasted");
    }, [attachFiles, disabled]);

    const handleDragOver = useCallback((e: React.DragEvent<HTMLElement>) => {
        if (!hasTransferredFiles(e.dataTransfer)) return;
        e.preventDefault();
        e.stopPropagation?.();
        e.dataTransfer.dropEffect = disabled ? "none" : "copy";
    }, [disabled]);

    const handleDrop = useCallback((e: React.DragEvent<HTMLElement>) => {
        const files = uniqueDroppedFiles(e);
        if (files.length === 0 && !hasTransferredFiles(e.dataTransfer)) return;
        e.preventDefault();
        e.stopPropagation?.();
        if (disabled || files.length === 0) return;
        void attachFiles(files, "dropped");
    }, [attachFiles, disabled]);

    return { handlePaste, handleDragOver, handleDrop, pendingAttachments, setPendingAttachments };
}
