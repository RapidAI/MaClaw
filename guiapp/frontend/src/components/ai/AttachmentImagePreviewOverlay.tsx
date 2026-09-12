import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { AIAssistantAttachmentFullDataURL } from "../../../wailsjs/go/main/App";
import { isCloudWorkspacePath } from "./codingTaskMode";
import { PreviewFileActions } from "./PreviewFileActions";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";
import { usePreviewSlideLifecycle } from "./previewSlide";
import type { Theme } from "./aiAssistantPanelTheme";

/** Above the assistant panel and welcome dialogs; below alert dialogs (120000). */
const IMAGE_PREVIEW_Z_INDEX = 110000;

/** Only one preview can be open, so a constant id is unambiguous. */
const PREVIEW_STATUS_ID = "attachment-image-preview-status";

type WailsNoDragStyle = CSSProperties & {
    WebkitAppRegion?: "no-drag";
    "--wails-draggable"?: "no-drag";
};

/** Keeps the preview body a stable size when no displayable bytes are available. */
const placeholderBoxStyle: CSSProperties = {
    width: "min(70vw, 560px)",
    height: "min(50vh, 320px)",
    borderRadius: 8,
};

/** Shared chrome for the preview card's header controls. */
function overlayControlStyle(t: Theme): CSSProperties {
    return {
        width: 28,
        height: 28,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        border: "none",
        borderRadius: 8,
        background: "transparent",
        color: t.textMuted,
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

/** Full-image preview panel for a chat attachment: slides in from the right; Esc, backdrop, or close button slides it back out. */
export function ImagePreviewOverlay({ filePath, fileName, thumbnailSrc, lang, theme: t, onClose }: ImagePreviewOverlayProps) {
    const isZh = !lang.startsWith("en");
    const { src, loading, failed } = useFullResolutionSource(filePath, thumbnailSrc);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const closeRef = useRef<HTMLButtonElement | null>(null);
    // Slide lifecycle: slides in on mount; requestClose slides back out and
    // only then invokes onClose, so the parent unmounts us after the exit
    // animation instead of the moment the close was requested.
    const [open, setOpen] = useState(true);
    const { entered, closing, settled, slideMs } = usePreviewSlideLifecycle(open, onClose);
    const requestClose = useCallback(() => setOpen(false), []);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(requestClose);
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
                requestClose();
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
    }, [requestClose]);

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
        alignItems: "stretch",
        justifyContent: "flex-end",
        boxSizing: "border-box",
        background: "rgba(0, 0, 0, 0.45)",
        // Fade the dim layer in lockstep with the panel slide.
        opacity: entered && !closing ? 1 : 0,
        transition: `opacity ${slideMs}ms ease`,
        zIndex: IMAGE_PREVIEW_Z_INDEX,
        // The panel is already sliding out — let clicks fall through to the
        // chat instead of swallowing them during the exit animation.
        pointerEvents: closing ? "none" : "auto",
        WebkitAppRegion: "no-drag",
        "--wails-draggable": "no-drag",
    };

    const closeLabel = isZh ? "关闭" : "Close";
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
                aria-describedby={PREVIEW_STATUS_ID}
                data-testid="attachment-image-preview-dialog"
                ref={dialogRef}
                style={{
                    display: "flex",
                    flexDirection: "column",
                    width: "min(92vw, 720px)",
                    height: "100%",
                    background: t.bg,
                    color: t.text,
                    border: `1px solid ${t.divider}`,
                    borderRight: "none",
                    borderRadius: "12px 0 0 12px",
                    boxShadow: "-24px 0 64px rgba(0, 0, 0, 0.45)",
                    overflow: "hidden",
                    transform: settled ? "none" : (entered && !closing ? "translateX(0)" : "translateX(100%)"),
                    transition: settled ? "none" : `transform ${slideMs}ms ease`,
                }}
                {...dialogProps}
            >
                <div
                    data-testid="attachment-image-preview-header"
                    style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        gap: 12,
                        padding: "10px 8px 10px 16px",
                        borderBottom: `1px solid ${t.divider}`,
                        background: t.titleBarBg,
                        flexShrink: 0,
                    }}
                >
                    <div
                        data-testid="attachment-image-preview-title"
                        style={{
                            fontSize: 13,
                            fontWeight: 600,
                            color: t.text,
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                        }}
                    >
                        {fileName}
                    </div>
                    <div
                        data-testid="attachment-image-preview-controls"
                        style={{ display: "flex", alignItems: "center", gap: 4, flexShrink: 0 }}
                    >
                        {filePath && !isCloudWorkspacePath(filePath) && (
                            <PreviewFileActions absPath={filePath} lang={lang} color={t.textMuted} />
                        )}
                        <button
                            ref={closeRef}
                            type="button"
                            onClick={requestClose}
                            title={closeLabel}
                            aria-label={closeLabel}
                            data-testid="attachment-image-preview-close"
                            style={{ ...overlayControlStyle(t), fontSize: 16 }}
                        >
                            {"×"}
                        </button>
                    </div>
                </div>
                <div
                    data-testid="attachment-image-preview-body"
                    style={{
                        flex: 1,
                        minHeight: 0,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        padding: 16,
                        boxSizing: "border-box",
                    }}
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
                                maxWidth: "100%",
                                maxHeight: "100%",
                                objectFit: "contain",
                                borderRadius: 4,
                                background: t.codeBlockBg,
                                // Blur the thumbnail only while the real bytes
                                // are still on the way, then ease the blur off.
                                filter: loading ? "blur(8px)" : "blur(0px)",
                                transition: "filter 160ms ease",
                            }}
                        />
                    )}
                </div>
                <div
                    id={PREVIEW_STATUS_ID}
                    aria-live="polite"
                    data-testid="attachment-image-preview-status"
                    style={{
                        padding: "8px 16px",
                        borderTop: `1px solid ${t.divider}`,
                        color: t.textMuted,
                        fontSize: 12,
                        lineHeight: 1.4,
                        textAlign: "center",
                        overflowWrap: "anywhere",
                        flexShrink: 0,
                    }}
                >
                    {status}
                </div>
            </div>
        </div>
    );

    if (typeof document === "undefined") return overlay;
    return createPortal(overlay, document.body);
}
