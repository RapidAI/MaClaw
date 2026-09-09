import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { GetTabWorkingDir, SetTabWorkingDir, OpenProjectDirectory, SelectWorkingDir } from "../../../wailsjs/go/main/App";
import { isCloudWorkspacePath } from "./codingTaskMode";
import { IconFolder, IconFolderOpen } from "./WorkbenchIcons";

export interface SessionWorkingDirChipProps {
    /** Current active tab ID. Empty string = local tab. */
    tabId: string;
    /** Changes after the backend has initialized this tab's session. */
    sessionReadyRevision?: number;
    theme: Theme;
    lang?: string;
    /** Fired after the user successfully switches this tab's working directory. */
    onWorkingDirChange?: (path: string) => void;
    /** Fired whenever the resolved working directory is known, including first load. */
    onWorkingDirResolved?: (path: string, tabId: string) => void;
    /** Cloud workspace: reopen the in-app file browser instead of Explorer. */
    onOpenCloudFiles?: () => void;
}

interface DirState {
    path: string;
    isDefault: boolean;
}

async function readTabWorkingDir(tabId: string): Promise<DirState | null> {
    const result = await GetTabWorkingDir(tabId) as { path?: string; is_default?: boolean };
    if (typeof result?.path !== "string" || !result.path.trim()) return null;
    return { path: result.path, isDefault: !!result.is_default };
}

/** Truncates a path in the middle: D:\very\long\...\final\dir */
export function truncatePathMiddle(path: string, maxLen: number): string {
    if (path.length <= maxLen) return path;
    const sep = path.includes("\\") ? "\\" : "/";
    const parts = path.split(sep);
    if (parts.length <= 3) return path.slice(0, maxLen - 3) + "...";
    // Keep first part (drive) and last 2 parts, ellipsis in middle.
    const head = parts.slice(0, 1).join(sep);
    const tail = parts.slice(-2).join(sep);
    const result = `${head}${sep}...${sep}${tail}`;
    if (result.length > maxLen) return path.slice(0, maxLen - 3) + "...";
    return result;
}

/** Display label for a working directory: cloud paths collapse to a friendly name. */
export function workingDirDisplayLabel(path: string, lang?: string): string {
    return isCloudWorkspacePath(path)
        ? (lang === "en" ? "Cloud workspace" : "云端工作区")
        : truncatePathMiddle(path, 42);
}

/**
 * Session-level working directory chip, rendered inside the input composer
 * toolbar after the permission-mode button.
 * Click the chip to open a menu: open the directory, switch it, or copy the path.
 * Cloud workspaces show a "云端" badge and hide directory switching.
 */
export function SessionWorkingDirChip({ tabId, sessionReadyRevision = 0, theme: t, lang, onWorkingDirChange, onWorkingDirResolved, onOpenCloudFiles }: SessionWorkingDirChipProps) {
    const [dirState, setDirState] = useState<DirState | null>(null);
    const [menuOpen, setMenuOpen] = useState(false);
    const [selectingDirectory, setSelectingDirectory] = useState(false);
    const mountedRef = useRef(true);
    const rootRef = useRef<HTMLDivElement>(null);
    const chipRef = useRef<HTMLButtonElement>(null);
    const refreshGenerationRef = useRef(0);
    // State updates are asynchronous, so use a ref as the immediate guard
    // against a rapid double-click opening two native directory pickers.
    const directorySelectionInFlightRef = useRef(false);

    useEffect(() => {
        mountedRef.current = true;
        return () => { mountedRef.current = false; };
    }, []);

    // Fetch working directory whenever tab changes.
    useEffect(() => {
        let cancelled = false;
        const refresh = () => {
            const generation = ++refreshGenerationRef.current;
            return readTabWorkingDir(tabId).then((result) => {
                if (cancelled || !mountedRef.current || generation !== refreshGenerationRef.current || !result) return;
                setDirState(result);
                onWorkingDirResolved?.(result.path, tabId);
            }).catch(() => {});
        };
        setDirState(null); // Clear stale value during tab switch.
        setMenuOpen(false);
        refresh();
        return () => {
            cancelled = true;
        };
    }, [tabId, sessionReadyRevision, onWorkingDirResolved]);

    // Close the menu on outside click or Escape (Escape returns focus to the chip).
    useEffect(() => {
        if (!menuOpen) return;
        const onPointerDown = (event: MouseEvent) => {
            if (rootRef.current && !rootRef.current.contains(event.target as Node)) setMenuOpen(false);
        };
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                setMenuOpen(false);
                chipRef.current?.focus();
            }
        };
        document.addEventListener("mousedown", onPointerDown);
        document.addEventListener("keydown", onKeyDown);
        return () => {
            document.removeEventListener("mousedown", onPointerDown);
            document.removeEventListener("keydown", onKeyDown);
        };
    }, [menuOpen]);

    const handleOpenDir = useCallback(() => {
        setMenuOpen(false);
        if (isCloudWorkspacePath(dirState?.path)) {
            onOpenCloudFiles?.();
            return;
        }
        if (dirState?.path) {
            OpenProjectDirectory(dirState.path).catch(() => {});
        }
    }, [dirState?.path, onOpenCloudFiles]);

    const handleSwitchDir = useCallback(async () => {
        if (directorySelectionInFlightRef.current) return;
        directorySelectionInFlightRef.current = true;
        setSelectingDirectory(true);
        try {
            // Use the existing backend directory picker dialog.
            const selected = await SelectWorkingDir();
            if (!selected || !mountedRef.current) return;
            await SetTabWorkingDir(tabId, selected);
            // Backend may rewrite a coding identity/sandbox pick to the live
            // work root. Read back the path the tab actually bound.
            const resolved = await readTabWorkingDir(tabId).catch(() => null);
            if (!mountedRef.current) return;
            const path = resolved?.path || selected;
            setDirState({ path, isDefault: resolved?.isDefault ?? false });
            onWorkingDirChange?.(path);
            // Keep listeners (e.g. the execution header meta row) in sync with
            // the new directory right away instead of waiting for a remount.
            onWorkingDirResolved?.(path, tabId);
        } catch (e) {
            console.error("[SessionWorkingDirChip] switch dir failed:", e);
        } finally {
            directorySelectionInFlightRef.current = false;
            if (mountedRef.current) setSelectingDirectory(false);
            setMenuOpen(false);
        }
    }, [tabId, onWorkingDirChange, onWorkingDirResolved]);

    const handleCopyPath = useCallback(() => {
        setMenuOpen(false);
        const path = dirState?.path;
        if (path && typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
            navigator.clipboard.writeText(path).catch(() => {});
        }
    }, [dirState?.path]);

    if (!dirState) {
        // Show a fixed-height placeholder to prevent layout shift during tab switch.
        return (
            <div data-testid="session-context-bar" style={{
                display: "inline-flex", alignItems: "center",
                minHeight: 22, boxSizing: "border-box", flexShrink: 0,
            }}>
                <span style={{ opacity: 0.35, display: "inline-flex", color: "var(--theme-text-muted)" }}><IconFolder size={13} color="currentColor" /></span>
            </div>
        );
    }

    const isDefault = dirState.isDefault;
    const isCloud = isCloudWorkspacePath(dirState.path);
    const displayPath = workingDirDisplayLabel(dirState.path, lang);
    const badgeText = isCloud
        ? (lang === "en" ? "remote" : "云端")
        : isDefault
            ? (lang === "en" ? "default" : "默认")
            : "";

    const menuItemStyle: CSSProperties = {
        display: "flex", alignItems: "center", gap: 6, width: "100%",
        padding: "6px 12px", border: "none", background: "transparent",
        color: t.text, fontSize: 12, textAlign: "left", cursor: "pointer",
        whiteSpace: "nowrap",
    };
    const openDirLabel = isCloud
        ? (lang === "en" ? "Open cloud workspace files" : "打开云端工作区文件")
        : (lang === "en" ? "Open containing folder" : "打开所在目录");

    return (
        <div
            data-testid="session-context-bar"
            style={{
                display: "inline-flex", alignItems: "center",
                minWidth: 0, maxWidth: "100%", boxSizing: "border-box",
                flexShrink: 1, overflow: "visible",
            }}
        >
            <div ref={rootRef} style={{ position: "relative", minWidth: 0, maxWidth: "100%" }}>
                <button
                    type="button"
                    ref={chipRef}
                    data-testid="working-dir-chip"
                    className="mc-working-dir-chip"
                    aria-haspopup="menu"
                    aria-expanded={menuOpen}
                    aria-label={(lang === "en" ? "Session working directory: " : "会话工作目录：") + displayPath}
                    title={isCloud ? displayPath : dirState.path}
                    onClick={() => setMenuOpen(v => !v)}
                    style={{
                        display: "inline-flex", alignItems: "center", gap: 5,
                        maxWidth: "100%", minHeight: 22, padding: "2px 9px",
                        border: "1px solid var(--theme-border)", borderRadius: 999,
                        background: menuOpen ? "var(--theme-primary-soft)" : "var(--theme-surface-muted)",
                        color: menuOpen ? "var(--theme-primary)" : "var(--theme-text-secondary)",
                        cursor: "pointer", fontSize: 11, lineHeight: 1.4,
                        transition: "background 150ms ease, border-color 150ms ease",
                    }}
                >
                    <span style={{ display: "inline-flex", flexShrink: 0, opacity: 0.8 }}>
                        {isDefault ? <IconFolder size={12} /> : <IconFolderOpen size={12} />}
                    </span>
                    <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", minWidth: 0 }}>
                        {displayPath}
                    </span>
                    {badgeText && (
                        <span style={{ fontSize: 10, color: t.textMuted, opacity: 0.75, fontStyle: isCloud ? "normal" : "italic", fontWeight: isCloud ? 700 : undefined, flexShrink: 0 }}>
                            {badgeText}
                        </span>
                    )}
                    <span aria-hidden="true" style={{ fontSize: 9, opacity: 0.6, flexShrink: 0 }}>▾</span>
                </button>
                {menuOpen && (
                    <div
                        role="menu"
                        data-testid="working-dir-menu"
                        style={{
                            position: "absolute", bottom: "calc(100% + 4px)", left: 0, zIndex: 9999,
                            background: t.titleBarBg, border: `1px solid ${t.titleBarBorder}`,
                            borderRadius: 8, boxShadow: "0 6px 18px rgba(0,0,0,0.16)",
                            padding: "4px 0", minWidth: 170,
                        }}
                    >
                        <button
                            type="button"
                            role="menuitem"
                            className="mc-dir-chip-menu-item"
                            onClick={handleOpenDir}
                            aria-label={openDirLabel}
                            style={menuItemStyle}
                        >
                            {openDirLabel}
                        </button>
                        {!isCloud && (
                            <button
                                type="button"
                                role="menuitem"
                                className="mc-dir-chip-menu-item"
                                onClick={handleSwitchDir}
                                disabled={selectingDirectory}
                                aria-label={lang === "en" ? "Choose a different working directory" : "选择其他工作目录"}
                                style={{ ...menuItemStyle, cursor: selectingDirectory ? "progress" : "pointer", opacity: selectingDirectory ? 0.6 : 1 }}
                            >
                                {selectingDirectory ? (lang === "en" ? "Choosing…" : "选择中…") : (lang === "en" ? "Switch directory…" : "切换目录…")}
                            </button>
                        )}
                        <button
                            type="button"
                            role="menuitem"
                            className="mc-dir-chip-menu-item"
                            onClick={handleCopyPath}
                            aria-label={lang === "en" ? "Copy working directory path" : "复制工作目录路径"}
                            style={menuItemStyle}
                        >
                            {lang === "en" ? "Copy path" : "复制路径"}
                        </button>
                    </div>
                )}
            </div>
        </div>
    );
}
