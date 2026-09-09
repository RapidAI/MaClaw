import { createPortal } from "react-dom";
import { useEffect, useRef, useState, type CSSProperties, type MouseEvent } from "react";
import { localizeText } from "./aiAssistantI18n";
import { getTitleBarToolButtonStyle, type Theme } from "./aiAssistantPanelTheme";

type WailsDragStyle = CSSProperties & { "--wails-draggable"?: "drag" | "no-drag" };

export type AssistantUpdatePayload = {
    has_update?: boolean;
    HasUpdate?: boolean;
    latest_version?: string;
    LatestVersion?: string;
    release_url?: string;
    ReleaseUrl?: string;
};

type AssistantUpdateNoticeProps = {
    inline: boolean;
    /** Title-bar keeps the legacy menu for embedded callers; the app uses the floating variant. */
    variant?: "titlebar" | "floating";
    lang: string;
    onDismissAppUpdate?: (latestVersion: string) => void;
    onOpenAppUpdate?: () => void;
    onOpenAppReleaseNotes?: () => void;
    theme: Theme;
    themeMode: "light" | "dark";
    updateAvailable?: AssistantUpdatePayload | null;
};

const stopMouse = (handler: () => void) => (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    handler();
};

const UpdateIcon = () => (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" focusable="false">
        <path d="M12 3a8.8 8.8 0 0 1 7.04 3.52l1.34-1.34A.75.75 0 0 1 21.66 5v4.25a.75.75 0 0 1-.75.75h-4.25a.75.75 0 0 1-.53-1.28l1.36-1.36A6.75 6.75 0 0 0 5.31 11a.75.75 0 0 1-1.5 0A8.25 8.25 0 0 1 12 3Z" fill="currentColor" />
        <path d="M19.44 13.25a.75.75 0 0 1 .75.75A8.25 8.25 0 0 1 5.96 19.48l-1.34 1.34A.75.75 0 0 1 3.34 20v-4.25a.75.75 0 0 1 .75-.75h4.25a.75.75 0 0 1 .53 1.28l-1.36 1.36A6.75 6.75 0 0 0 18.69 14a.75.75 0 0 1 .75-.75Z" fill="currentColor" />
    </svg>
);

const DismissIcon = () => (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" focusable="false">
        <path d="M6.47 6.47a.75.75 0 0 1 1.06 0L12 10.94l4.47-4.47a.75.75 0 1 1 1.06 1.06L13.06 12l4.47 4.47a.75.75 0 1 1-1.06 1.06L12 13.06l-4.47 4.47a.75.75 0 0 1-1.06-1.06L10.94 12 6.47 7.53a.75.75 0 0 1 0-1.06Z" fill="currentColor" />
    </svg>
);

const UpdateReadyIcon = () => (
    <svg viewBox="0 0 24 24" width="21" height="21" aria-hidden="true" focusable="false">
        <path d="M12 18V5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
        <path d="m7.5 9.5 4.5-4.5 4.5 4.5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M5 19.5h14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
);

export function AssistantUpdateNotice({ inline, lang, onDismissAppUpdate, onOpenAppReleaseNotes, onOpenAppUpdate, theme: t, themeMode, updateAvailable, variant = "titlebar" }: AssistantUpdateNoticeProps) {
    const [open, setOpen] = useState(false);
    const menuRef = useRef<HTMLDivElement | null>(null);
    const firstMenuItemRef = useRef<HTMLButtonElement | null>(null);
    const hasUpdate = !!(updateAvailable?.has_update || updateAvailable?.HasUpdate);
    const latestVersion = String(updateAvailable?.latest_version || updateAvailable?.LatestVersion || "").trim();
    const latestVersionDisplay = latestVersion.replace(/^[Vv]/, "");
    const title = latestVersion
        ? localizeText(lang, `New version ${latestVersion} available`, `\u53d1\u73b0\u65b0\u7248\u672c ${latestVersion}`, `\u767c\u73fe\u65b0\u7248\u672c ${latestVersion}`)
        : localizeText(lang, "New version available", "\u53d1\u73b0\u65b0\u7248\u672c", "\u767c\u73fe\u65b0\u7248\u672c");
    const onlineText = latestVersionDisplay
        ? localizeText(lang, `Online update to ${latestVersion}`, `\u5728\u7ebf\u66f4\u65b0\u5230 ${latestVersionDisplay} \u7248\u672c`, `\u5728\u7dda\u66f4\u65b0\u5230 ${latestVersionDisplay} \u7248\u672c`)
        : localizeText(lang, "Online update", "\u5728\u7ebf\u66f4\u65b0", "\u5728\u7dda\u66f4\u65b0");
    const skipText = localizeText(lang, "Do not remind this time", "\u6b64\u6b21\u4e0d\u63d0\u793a", "\u6b64\u6b21\u4e0d\u63d0\u793a");
    const readyText = localizeText(lang, "New version ready", "\u65b0\u7248\u672c\u5c31\u7eea", "\u65b0\u7248\u672c\u5c31\u7dd2");
    const releaseNotesText = localizeText(lang, "Release notes", "\u66f4\u65b0\u65e5\u5fd7", "\u66f4\u65b0\u65e5\u8a8c");
    const restartUpdateText = localizeText(lang, "Restart to update", "\u91cd\u542f\u5347\u7ea7", "\u91cd\u555f\u5347\u7d1a");
    const closeText = localizeText(lang, "Close update notice", "\u5173\u95ed\u66f4\u65b0\u63d0\u793a", "\u95dc\u9589\u66f4\u65b0\u63d0\u793a");

    useEffect(() => {
        if (open && hasUpdate) firstMenuItemRef.current?.focus();
    }, [hasUpdate, open]);

    useEffect(() => {
        if (!open) return;
        const closeOutside = (event: globalThis.MouseEvent) => {
            if (menuRef.current?.contains(event.target as Node)) return;
            setOpen(false);
        };
        const closeOnEscape = (event: KeyboardEvent) => {
            if (event.key === "Escape") setOpen(false);
        };
        document.addEventListener("pointerdown", closeOutside);
        document.addEventListener("keydown", closeOnEscape);
        return () => {
            document.removeEventListener("pointerdown", closeOutside);
            document.removeEventListener("keydown", closeOnEscape);
        };
    }, [open]);

    if (!hasUpdate) return null;

    if (variant === "floating") {
        const handleFloatingOpen = (event: MouseEvent<HTMLButtonElement>) => {
            event.preventDefault();
            event.stopPropagation();
            onOpenAppUpdate?.();
        };
        const handleFloatingReleaseNotes = (event: MouseEvent<HTMLButtonElement>) => {
            event.preventDefault();
            event.stopPropagation();
            if (onOpenAppReleaseNotes) onOpenAppReleaseNotes();
            else onOpenAppUpdate?.();
        };
        const handleFloatingDismiss = (event: MouseEvent<HTMLButtonElement>) => {
            event.preventDefault();
            event.stopPropagation();
            onDismissAppUpdate?.(latestVersion);
        };
        const cardStyle = {
            "--app-update-surface": t.titleBarBg,
            "--app-update-border": t.titleBarBorder,
            "--app-update-text": t.text,
            "--app-update-muted": t.textMuted,
            "--app-update-accent": themeMode === "dark" ? "#65d6a0" : "#36b98a",
            "--app-update-accent-strong": themeMode === "dark" ? "#8de8bd" : "#249a72",
            "--wails-draggable": "no-drag",
        } as CSSProperties;
        const card = <div className="app-update-float" data-testid="assistant-update-float" data-ai-theme={themeMode} role="status" aria-live="polite" style={cardStyle}>
            <div className="app-update-float__identity">
                <span className="app-update-float__icon" aria-hidden="true"><UpdateReadyIcon /></span>
                <div className="app-update-float__copy">
                    <strong>{readyText}</strong>
                    {latestVersionDisplay && <span>{latestVersionDisplay}</span>}
                </div>
            </div>
            <div className="app-update-float__actions">
                <button type="button" className="app-update-float__button app-update-float__button--outline" onClick={handleFloatingReleaseNotes}>{releaseNotesText}</button>
                <button type="button" className="app-update-float__button app-update-float__button--primary" onClick={handleFloatingOpen}>{restartUpdateText}</button>
            </div>
            <button type="button" className="app-update-float__close" aria-label={closeText} title={closeText} onClick={handleFloatingDismiss}><DismissIcon /></button>
        </div>;
        // The assistant panel is kept mounted while other pages are active and
        // also lives inside the scaled app layer. Portalling makes the notice
        // global and keeps its physical bottom-left position stable.
        return typeof document !== "undefined" && document.body ? createPortal(card, document.body) : card;
    }

    const toggleMenu = () => setOpen(value => !value);
    const toggleProps = inline
        ? {
            onMouseDown: stopMouse(toggleMenu),
            onClick: (event: MouseEvent<HTMLButtonElement>) => {
                if (event.detail > 0) return;
                toggleMenu();
            },
        }
        : { onClick: toggleMenu };
    const menuBackground = themeMode === "dark" ? t.inputBarBg : t.bg;
    const menuShadow = themeMode === "dark" ? "0 18px 45px rgba(0, 0, 0, 0.65)" : "0 16px 38px rgba(15, 23, 42, 0.18)";
    const triggerColor = t.btnColor;
    const handleOnlineUpdate = (event: MouseEvent<HTMLButtonElement>) => {
        event.preventDefault();
        event.stopPropagation();
        setOpen(false);
        onOpenAppUpdate?.();
    };
    const handleDismiss = (event: MouseEvent<HTMLButtonElement>) => {
        event.preventDefault();
        event.stopPropagation();
        setOpen(false);
        onDismissAppUpdate?.(latestVersion);
    };
    return <div ref={menuRef} style={{ position: "relative", display: "inline-flex" }}>
        <button className="ai-titlebar-tool ai-update-notice-button" {...toggleProps} aria-haspopup="menu" aria-expanded={open} aria-label={title} title={title} style={{ ...getTitleBarToolButtonStyle(t, "active"), color: triggerColor, background: `color-mix(in srgb, ${t.btnColor} 14%, ${t.fieldBg})`, boxShadow: "inset 0 0 0 1px rgba(79, 127, 111, 0.34), 0 0 0 0 rgba(79, 127, 111, 0.28)", position: "relative" }}><UpdateIcon /></button>
        {open && <div role="menu" aria-label={title} style={{ position: "absolute", top: "32px", right: 0, minWidth: "180px", padding: "6px", borderRadius: "8px", border: `1px solid ${t.titleBarBorder}`, background: menuBackground, boxShadow: menuShadow, zIndex: 30040, color: t.text, "--wails-draggable": "no-drag" } as WailsDragStyle}>
            <button ref={firstMenuItemRef} role="menuitem" className="ai-update-menu-item" onClick={handleOnlineUpdate} style={menuItemStyle(t.text)}><UpdateIcon /><span>{onlineText}</span></button>
            <button role="menuitem" className="ai-update-menu-item" onClick={handleDismiss} style={menuItemStyle(t.textMuted)}><DismissIcon /><span>{skipText}</span></button>
        </div>}
    </div>;
}

const menuItemStyle = (color: string): CSSProperties => ({
    width: "100%", display: "flex", alignItems: "center", gap: "8px", padding: "7px 8px", border: "none", borderRadius: "6px", background: "transparent", color, cursor: "pointer", fontSize: "12px", textAlign: "left", whiteSpace: "nowrap",
});
