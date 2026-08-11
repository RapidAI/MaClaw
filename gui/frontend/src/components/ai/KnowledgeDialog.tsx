import { useEffect, useCallback, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { CSSProperties } from "react";
import { KnowledgeSettingsPanel } from "../settings/KnowledgeSettingsPanel";
import type { Theme } from "./aiAssistantPanelTheme";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";
import { usePortalThemeAttributes } from "../../hooks/usePortalThemeAttributes";

const dialogFocusableSelector = 'button:not([disabled]):not([tabindex="-1"]), input:not([disabled]):not([tabindex="-1"]), select:not([disabled]):not([tabindex="-1"]), textarea:not([disabled]):not([tabindex="-1"]), [href]:not([tabindex="-1"])';

type WailsNoDragStyle = CSSProperties & {
    WebkitAppRegion?: "no-drag";
    "--wails-draggable"?: "no-drag";
};

interface KnowledgeDialogProps {
    open: boolean;
    onClose: () => void;
    lang: string;
    theme: Theme;
}

const overlayStyle: WailsNoDragStyle = {
    position: "fixed",
    inset: 0,
    background: "rgba(0, 0, 0, 0.5)",
    backdropFilter: "blur(3px)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    zIndex: 50000,
    animation: "fadeIn 0.2s ease-out",
    WebkitAppRegion: "no-drag",
    "--wails-draggable": "no-drag",
};

const modalStyle: WailsNoDragStyle = {
    width: "min(820px, 92vw)",
    maxHeight: "85vh",
    borderRadius: "12px",
    boxShadow: "0 20px 60px rgba(0, 0, 0, 0.3), 0 0 0 1px rgba(0, 0, 0, 0.05)",
    display: "flex",
    flexDirection: "column",
    overflow: "hidden",
    animation: "slideUp 0.25s ease-out",
    WebkitAppRegion: "no-drag",
    "--wails-draggable": "no-drag",
};

const headerStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "12px 16px",
    flexShrink: 0,
};

const bodyStyle: CSSProperties = {
    flex: 1,
    overflow: "auto",
    overscrollBehavior: "contain",
    padding: "0 4px 4px",
};

const toastStyle: CSSProperties = {
    position: "absolute",
    right: 18,
    bottom: 18,
    maxWidth: "min(420px, calc(100% - 36px))",
    padding: "10px 12px",
    borderRadius: "8px",
    background: "rgba(15, 23, 42, 0.94)",
    color: "#f8fafc",
    boxShadow: "0 16px 36px rgba(0, 0, 0, 0.28)",
    fontSize: "13px",
    lineHeight: 1.45,
    zIndex: 1,
    textAlign: "left",
};

export function KnowledgeDialog({ open, onClose, lang, theme }: KnowledgeDialogProps) {
    const [toastMessage, setToastMessage] = useState("");
    const toastTimerRef = useRef<number | null>(null);
    const closeCallbackRef = useRef(onClose);
    const dialogRef = useRef<HTMLDivElement>(null);
    const closeButtonRef = useRef<HTMLButtonElement>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);
    closeCallbackRef.current = onClose;
    // This dialog is portaled to document.body, outside #App where the
    // theme variables normally live. Mirror the theme attributes so the
    // knowledge panel keeps its selected light/dark scheme.
    const portalThemeAttributes = usePortalThemeAttributes(open);
    const handleKeyDown = useCallback((e: KeyboardEvent) => {
        const activeElement = document.activeElement;
        const dialog = dialogRef.current;
        if (!dialog || (activeElement instanceof HTMLElement && !dialog.contains(activeElement))) return;
        const nestedDialog = Array.from(dialog.querySelectorAll<HTMLElement>('[role="dialog"]'))
            .findLast((element) => element.contains(activeElement));
        // Nested dialogs own their Escape and Tab behavior. The parent must
        // not close or pull focus away while the import flow is active.
        if (nestedDialog) return;
        if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            closeCallbackRef.current();
            return;
        }
        if (e.key !== "Tab") return;
        const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(dialogFocusableSelector));
        if (!focusable.length) return;
        const activeIndex = focusable.indexOf(document.activeElement as HTMLElement);
        const nextIndex = e.shiftKey
            ? (activeIndex <= 0 ? focusable.length - 1 : activeIndex - 1)
            : (activeIndex === focusable.length - 1 ? 0 : activeIndex + 1);
        e.preventDefault();
        focusable[nextIndex].focus();
    }, []);

    const showToastMessage = useCallback((message: string, duration = 3000) => {
        setToastMessage(message);
        if (toastTimerRef.current !== null) {
            window.clearTimeout(toastTimerRef.current);
        }
        toastTimerRef.current = window.setTimeout(() => {
            setToastMessage("");
            toastTimerRef.current = null;
        }, duration);
    }, []);

    useEffect(() => {
        if (!open) return;
        const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const previousOverflow = document.body.style.overflow;
        document.body.style.overflow = "hidden";
        document.addEventListener("keydown", handleKeyDown);
        const focusFrame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
        return () => {
            window.cancelAnimationFrame(focusFrame);
            document.body.style.overflow = previousOverflow;
            document.removeEventListener("keydown", handleKeyDown);
            if (previousFocus?.isConnected) previousFocus.focus();
        };
    }, [open, handleKeyDown]);

    useEffect(() => {
        if (open) return;
        setToastMessage("");
        if (toastTimerRef.current !== null) {
            window.clearTimeout(toastTimerRef.current);
            toastTimerRef.current = null;
        }
    }, [open]);

    useEffect(() => () => {
        if (toastTimerRef.current !== null) {
            window.clearTimeout(toastTimerRef.current);
            toastTimerRef.current = null;
        }
    }, []);

    if (!open) return null;

    const title = lang === "en" ? "Knowledge Base" : "知识库";

    const dialog = (
        <div
            className="knowledge-dialog-overlay"
            style={overlayStyle}
            {...portalThemeAttributes}
            {...backdropProps}
        >
            <div
                ref={dialogRef}
                className="knowledge-dialog-modal"
                style={{ ...modalStyle, position: "relative", background: theme.bg, border: `1px solid ${theme.divider}` }}
                role="dialog"
                aria-modal="true"
                aria-label={title}
                {...dialogProps}
            >
                <div style={{ ...headerStyle, borderBottom: `1px solid ${theme.divider}` }}>
                    <h3 style={{ margin: 0, fontSize: "14px", fontWeight: 600, color: theme.text }}>
                        {title}
                    </h3>
                    <button
                        ref={closeButtonRef}
                        onClick={onClose}
                        className="knowledge-dialog-close"
                        style={{
                            background: "none",
                            border: "none",
                            fontSize: "16px",
                            cursor: "pointer",
                            color: theme.textMuted,
                            width: "28px",
                            height: "28px",
                            display: "inline-flex",
                            alignItems: "center",
                            justifyContent: "center",
                            padding: 0,
                            borderRadius: "6px",
                            lineHeight: 1,
                        }}
                        title={lang === "en" ? "Close" : "关闭"}
                        aria-label={lang === "en" ? "Close" : "关闭"}
                    >
                        ×
                    </button>
                </div>
                <div style={bodyStyle}>
                    <KnowledgeSettingsPanel lang={lang} showToastMessage={showToastMessage} />
                </div>
                {toastMessage ? <div style={toastStyle} role="status">{toastMessage}</div> : null}
            </div>
        </div>
    );

    // The assistant stays mounted while the user views another application page.
    // Render this fixed overlay at the document root so it cannot be clipped by
    // the hidden assistant host or inherit its hidden accessibility state.
    return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
