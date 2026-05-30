import { useEffect, useCallback, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { KnowledgeSettingsPanel } from "../settings/KnowledgeSettingsPanel";
import type { Theme } from "./aiAssistantPanelTheme";

interface KnowledgeDialogProps {
    open: boolean;
    onClose: () => void;
    lang: string;
    theme: Theme;
}

const overlayStyle: CSSProperties = {
    position: "fixed",
    inset: 0,
    background: "rgba(0, 0, 0, 0.5)",
    backdropFilter: "blur(3px)",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    zIndex: 50000,
    animation: "fadeIn 0.2s ease-out",
};

const modalStyle: CSSProperties = {
    width: "min(820px, 92vw)",
    maxHeight: "85vh",
    borderRadius: "12px",
    boxShadow: "0 20px 60px rgba(0, 0, 0, 0.3), 0 0 0 1px rgba(0, 0, 0, 0.05)",
    display: "flex",
    flexDirection: "column",
    overflow: "hidden",
    animation: "slideUp 0.25s ease-out",
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
    const handleKeyDown = useCallback((e: KeyboardEvent) => {
        if (e.key === "Escape") onClose();
    }, [onClose]);

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
        document.addEventListener("keydown", handleKeyDown);
        return () => document.removeEventListener("keydown", handleKeyDown);
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

    return (
        <div
            style={overlayStyle}
            onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
            role="dialog"
            aria-modal="true"
            aria-label={title}
        >
            <div style={{ ...modalStyle, position: "relative", background: theme.bg, border: `1px solid ${theme.divider}` }}>
                <div style={{ ...headerStyle, borderBottom: `1px solid ${theme.divider}` }}>
                    <h3 style={{ margin: 0, fontSize: "14px", fontWeight: 600, color: theme.text }}>
                        📚 {title}
                    </h3>
                    <button
                        onClick={onClose}
                        style={{
                            background: "none",
                            border: "none",
                            fontSize: "18px",
                            cursor: "pointer",
                            color: theme.textMuted,
                            padding: "4px 8px",
                            borderRadius: "6px",
                            lineHeight: 1,
                        }}
                        title={lang === "en" ? "Close" : "关闭"}
                        aria-label={lang === "en" ? "Close" : "关闭"}
                    >
                        ✕
                    </button>
                </div>
                <div style={bodyStyle}>
                    <KnowledgeSettingsPanel lang={lang} showToastMessage={showToastMessage} />
                </div>
                {toastMessage ? <div style={toastStyle} role="status">{toastMessage}</div> : null}
            </div>
        </div>
    );
}
