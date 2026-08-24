import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { AIAssistantAttachmentFullDataURL, ShowItemInFolder } from "../../../wailsjs/go/main/App";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";
import type { Theme } from "./aiAssistantPanelTheme";

/** Above the assistant panel and welcome dialogs; below alert dialogs (120000). */
const IMAGE_PREVIEW_Z_INDEX = 110000;

/** Only one preview can be open, so a constant id is unambiguous. */
const PREVIEW_STATUS_ID = "attachment-image-preview-status";

type WailsNoDragStyle = CSSProperties & {
    WebkitAppRegion?: "no-drag";
    "--wails-draggable"?: "no-drag";
};

/** Keeps the overlay a stable size when no displayable bytes are available. */
const placeholderBoxStyle: CSSProperties = {
    width: "min(70vw, 560px)",
    height: "min(50vh, 320px)",
    borderRadius: 8,
};

/** Shared chrome for the overlay's floating controls. */
function overlayControlStyle(t: Theme): CSSProperties {
    return {
        width: 32,
        height: 32,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.text,
        lineHeight: 1,
        cursor: "pointer",
        whiteSpace: "nowrap",
    };
}

/**
 * Resolve the bytes the overlay should display. The caller's thumbnail is only
 * a placeholder: host previews are capped at 96px, and a pasted image's object
 * URL is revoked once the composer clears, so the local file is the one source
 * that still holds the full image after a message is sent.
 */
function useFullResolutionSource(filePath: string, thumbnailSrc: string) {
    const canUpgrade = !!filePath;
    const [fullSrc, setFullSrc] = useState("");
    const [failed, setFailed] = useState(false);

    useEffect(() => {
        if (!canUpgrade) return;
        let active = true;
        setFullSrc("");
        setFailed(false);
        void AIAssistantAttachmentFullDataURL(filePath)
            .then(dataUrl => {
                if (!active) return;
                const resolved = String(dataUrl || "");
                setFullSrc(resolved);
                setFailed(!resolved);
            })
            .catch(() => { if (active) setFailed(true); });
        return () => { active = false; };
    }, [filePath, canUpgrade]);

    if (!canUpgrade) return { src: thumbnailSrc, loading: false, failed: false };
    return { src: fullSrc || thumbnailSrc, loading: !fullSrc && !failed, failed };
}

export interface ImagePreviewOverlayProps {
    /** Local path used to fetch the full-resolution bytes. */
    filePath: string;
    fileName: string;
    /** Already-loaded thumbnail, shown until the full image arrives. */
    thumbnailSrc: string;
    lang: string;
    theme: Theme;
    onClose: () => void;
}

/** Full-image overlay for a chat attachment: Esc, backdrop, or close button dismisses. */
export function ImagePreviewOverlay({ filePath, fileName, thumbnailSrc, lang, theme: t, onClose }: ImagePreviewOverlayProps) {
    const isZh = !lang.startsWith("en");
    const { src, loading, failed } = useFullResolutionSource(filePath, thumbnailSrc);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const closeRef = useRef<HTMLButtonElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);
    // A sent message still points at the composer's revoked object URL, so the
    // placeholder can be undisplayable even while the real bytes are on the way.
    const [brokenSrc, setBrokenSrc] = useState("");
    const imageBroken = !!src && brokenSrc === src;

    useEffect(() => {
        const previouslyFocused = document.activeElement as HTMLElement | null;
        closeRef.current?.focus();
        return () => {
            // Restoring to <body> would strand keyboard focus; callers that
            // care refocus their own trigger instead.
            if (!previouslyFocused || previouslyFocused === document.body) return;
            if (previouslyFocused.isConnected) previouslyFocused.focus();
        };
    }, []);

    useEffect(() => {
        const onKey = (event: globalThis.KeyboardEvent) => {
            // Capture phase with stopPropagation: while the overlay is up it
            // owns these keys, so Escape cannot also reach a menu or dialog
            // listening further down.
            if (event.key === "Escape") {
                event.preventDefault();
                event.stopPropagation();
                onClose();
                return;
            }
            // Keep Tab cycling inside the overlay instead of walking into the
            // chat behind it.
            if (event.key === "Tab") {
                event.preventDefault();
                event.stopPropagation();
                const controls = Array.from(dialogRef.current?.querySelectorAll<HTMLButtonElement>("button:not([disabled])") || []);
                if (controls.length === 0) return;
                const current = controls.indexOf(document.activeElement as HTMLButtonElement);
                const next = event.shiftKey
                    ? controls[(current <= 0 ? controls.length : current) - 1]
                    : controls[(current + 1) % controls.length];
                next.focus();
            }
        };
        document.addEventListener("keydown", onKey, true);
        return () => document.removeEventListener("keydown", onKey, true);
    }, [onClose]);

    useEffect(() => {
        const html = document.documentElement;
        const prev = { body: document.body.style.overflow, html: html.style.overflow };
        document.body.style.overflow = html.style.overflow = "hidden";
        return () => {
            document.body.style.overflow = prev.body;
            html.style.overflow = prev.html;
        };
    }, []);

    const overlayStyle: WailsNoDragStyle = {
        position: "fixed",
        inset: 0,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 10,
        padding: "48px 24px 24px",
        boxSizing: "border-box",
        background: "rgba(0, 0, 0, 0.78)",
        zIndex: IMAGE_PREVIEW_Z_INDEX,
        WebkitAppRegion: "no-drag",
        "--wails-draggable": "no-drag",
    };

    const closeLabel = isZh ? "关闭" : "Close";
    const revealLabel = isZh ? "打开所在文件夹" : "Show in folder";
    const status = loading
        ? (isZh ? "正在加载原图…" : "Loading full image…")
        : failed || imageBroken
            ? (isZh ? "无法加载原图" : "This image cannot be loaded")
            : fileName;

    const overlay = (
        <div
            role="presentation"
            data-testid="attachment-image-preview-overlay"
            style={overlayStyle}
            {...backdropProps}
        >
            <div
                role="dialog"
                aria-modal="true"
                aria-label={isZh ? `图片预览：${fileName}` : `Image preview: ${fileName}`}
                // The caption lives outside the dialog so the whole dark area
                // stays clickable; describedby is what carries it to AT.
                aria-describedby={PREVIEW_STATUS_ID}
                data-testid="attachment-image-preview-dialog"
                ref={dialogRef}
                style={{ position: "relative", display: "flex", maxWidth: "100%", maxHeight: "100%" }}
                {...dialogProps}
            >
                {imageBroken ? (
                    <div
                        data-testid="attachment-image-preview-fallback"
                        style={{ ...placeholderBoxStyle, background: t.codeBlockBg, border: `1px solid ${t.codeBlockBorder}` }}
                    />
                ) : (
                    <img
                        src={src}
                        alt=""
                        draggable={false}
                        onError={() => setBrokenSrc(src)}
                        data-testid="attachment-image-preview-image"
                        style={{
                            maxWidth: "90vw",
                            // Controls + caption + padding; 82vh can clip close on a short window.
                            maxHeight: "calc(100vh - 160px)",
                            objectFit: "contain",
                            borderRadius: 8,
                            background: t.codeBlockBg,
                            boxShadow: "0 24px 64px rgba(0, 0, 0, 0.45)",
                            // While the placeholder is on screen, hold the
                            // overlay at a deliberate size instead of the
                            // 96px thumbnail's own, and blur it only while
                            // the real bytes are still on the way.
                            width: loading || failed ? "min(70vw, 560px)" : undefined,
                            filter: loading ? "blur(8px)" : undefined,
                        }}
                    />
                )}
                {/* Sit above the image, not on the window chrome: a fixed
                    top-right cluster lands on the desktop close button. */}
                <div
                    data-testid="attachment-image-preview-controls"
                    style={{ position: "absolute", right: 0, bottom: "100%", marginBottom: 16, display: "flex", alignItems: "center", gap: 8, zIndex: 1 }}
                >
                    {filePath && (
                        <button
                            type="button"
                            onClick={() => { void ShowItemInFolder(filePath).catch(() => undefined); }}
                            title={filePath}
                            aria-label={revealLabel}
                            data-testid="attachment-image-preview-open-file"
                            style={{ ...overlayControlStyle(t), width: "auto", borderRadius: 16, padding: "0 12px", fontSize: 12 }}
                        >
                            {revealLabel}
                        </button>
                    )}
                    <button
                        ref={closeRef}
                        type="button"
                        onClick={onClose}
                        title={closeLabel}
                        aria-label={closeLabel}
                        data-testid="attachment-image-preview-close"
                        style={{ ...overlayControlStyle(t), borderRadius: 16, fontSize: 18 }}
                    >
                        {"×"}
                    </button>
                </div>
            </div>
            <div
                id={PREVIEW_STATUS_ID}
                aria-live="polite"
                data-testid="attachment-image-preview-status"
                style={{
                    maxWidth: "90vw",
                    color: "#f1f5f9",
                    fontSize: 12,
                    lineHeight: 1.4,
                    textAlign: "center",
                    overflowWrap: "anywhere",
                    // Let clicks on the caption reach the backdrop, so the whole
                    // dark area outside the image dismisses the overlay.
                    pointerEvents: "none",
                }}
            >
                {status}
            </div>
        </div>
    );

    if (typeof document === "undefined") return overlay;
    return createPortal(overlay, document.body);
}

export interface AttachmentImageThumbnailProps {
    /** Thumbnail bytes already resolved by the caller. */
    src: string;
    filePath: string;
    fileName: string;
    lang: string;
    theme: Theme;
    /** Box the thumbnail fills, e.g. the chat chip or composer chip frame. */
    frameStyle: CSSProperties;
    /** Overrides for the image inside the frame; defaults to a cropped fill. */
    imageStyle?: CSSProperties;
    /** Hover text; defaults to the preview action label. */
    title?: string;
    /** Called when `src` cannot be displayed, so the caller can re-resolve it. */
    onImageError?: () => void;
}

/** Attachment thumbnail that opens a full-image preview when clicked. */
export function AttachmentImageThumbnail({ src, filePath, fileName, lang, theme: t, frameStyle, imageStyle, title, onImageError }: AttachmentImageThumbnailProps) {
    const [previewOpen, setPreviewOpen] = useState(false);
    const triggerRef = useRef<HTMLButtonElement | null>(null);
    const isZh = !lang.startsWith("en");
    const label = isZh ? `预览图片 ${fileName}` : `Preview image ${fileName}`;
    const close = useCallback(() => {
        setPreviewOpen(false);
        triggerRef.current?.focus();
    }, []);

    return (
        <>
            <button
                ref={triggerRef}
                type="button"
                onClick={() => setPreviewOpen(true)}
                title={title || label}
                aria-label={label}
                data-testid="attachment-image-thumbnail"
                style={{
                    ...frameStyle,
                    display: "inline-flex",
                    alignItems: "center",
                    justifyContent: "center",
                    padding: 0,
                    overflow: "hidden",
                    cursor: "zoom-in",
                }}
            >
                <img src={src} alt="" draggable={false} onError={onImageError} style={{ width: "100%", height: "100%", objectFit: "cover", display: "block", ...imageStyle }} />
            </button>
            {previewOpen && (
                <ImagePreviewOverlay
                    filePath={filePath}
                    fileName={fileName}
                    thumbnailSrc={src}
                    lang={lang}
                    theme={t}
                    onClose={close}
                />
            )}
        </>
    );
}
