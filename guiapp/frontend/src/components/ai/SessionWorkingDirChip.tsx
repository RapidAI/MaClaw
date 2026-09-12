import { useCallback, useEffect, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { createPortal } from "react-dom";
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

const MENU_WIDTH = 176;
const MENU_MAX_HEIGHT = 240;

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
    const [menuOpen, setMenuOpenState] = useState(false);
    const [menuPosition, setMenuPosition] = useState<{ left: number; top: number; openUp: boolean; maxHeight: number } | null>(null);
    const [selectingDirectory, setSelectingDirectory] = useState(false);
    const mountedRef = useRef(true);
    const rootRef = useRef<HTMLDivElement>(null);
    const chipRef = useRef<HTMLButtonElement>(null);
    const menuRef = useRef<HTMLDivElement>(null);
    const refreshGenerationRef = useRef(0);
    // State updates are asynchronous, so use a ref as the immediate guard
    // against a rapid double-click opening two native directory pickers.
    const directorySelectionInFlightRef = useRef(false);

    // The menu is portaled to document.body so composer toolbars with
    // overflow:hidden cannot clip it; position it from the chip's rect and
    // open toward whichever viewport side has more room.
    const updateMenuPosition = useCallback(() => {
        const chip = chipRef.current;
        if (!chip || typeof window === "undefined") return;
        const rect = chip.getBoundingClientRect();
        const viewportPadding = 8;
        const menuGap = 6;
        const spaceAbove = Math.max(0, rect.top - menuGap - viewportPadding);
        const spaceBelow = Math.max(0, window.innerHeight - rect.bottom - menuGap - viewportPadding);
        // Pick the roomier side, then cap the menu height so it can never
        // disappear beyond either viewport edge.
        const openUp = spaceAbove >= spaceBelow;
        const left = Math.min(
            Math.max(viewportPadding, rect.left),
            Math.max(viewportPadding, window.innerWidth - MENU_WIDTH - viewportPadding),
        );
        setMenuPosition({
            left,
            top: openUp ? rect.top - menuGap : rect.bottom + menuGap,
            openUp,
            maxHeight: Math.min(MENU_MAX_HEIGHT, openUp ? spaceAbove : spaceBelow),
        });
    }, []);

    const setMenuOpen = useCallback((next: boolean) => {
        if (next) updateMenuPosition();
        setMenuOpenState(next);
    }, [updateMenuPosition]);

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
            const target = event.target as Node;
            if (rootRef.current?.contains(target) || menuRef.current?.contains(target)) return;
            setMenuOpen(false);
        };
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                setMenuOpen(false);
                chipRef.current?.focus();
            }
        };
        const handleReposition = () => updateMenuPosition();
        document.addEventListener("mousedown", onPointerDown);
        document.addEventListener("keydown", onKeyDown);
        window.addEventListener("resize", handleReposition);
        window.addEventListener("scroll", handleReposition, true);
        return () => {
            document.removeEventListener("mousedown", onPointerDown);
            document.removeEventListener("keydown", onKeyDown);
            window.removeEventListener("resize", handleReposition);
            window.removeEventListener("scroll", handleReposition, true);
        };
    }, [menuOpen, setMenuOpen, updateMenuPosition]);

    // Move focus into the menu on open so arrow-key navigation works immediately.
    useEffect(() => {
        if (!menuOpen) return;
        menuRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]:not(:disabled)')?.focus();
    }, [menuOpen]);

    const handleMenuKeyDown = useCallback((event: ReactKeyboardEvent) => {
        const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') || []).filter((item) => !item.disabled);
        if (!items.length) return;
        const index = items.indexOf(event.target as HTMLButtonElement);
        if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            const step = event.key === "ArrowDown" ? 1 : items.length - 1;
            items[(Math.max(index, 0) + step) % items.length]?.focus();
        } else if (event.key === "Home") {
            event.preventDefault();
            items[0]?.focus();
        } else if (event.key === "End") {
            event.preventDefault();
            items.at(-1)?.focus();
        }
    }, []);

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
                minHeight: 24, boxSizing: "border-box", flexShrink: 0,
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
        padding: "5px 8px", border: "none", borderRadius: 4, background: "transparent",
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
            <div ref={rootRef} style={{ minWidth: 0, maxWidth: "100%" }}>
                <button
                    type="button"
                    ref={chipRef}
                    data-testid="working-dir-chip"
                    className="mc-working-dir-chip"
                    aria-haspopup="menu"
                    aria-expanded={menuOpen}
                    aria-label={(lang === "en" ? "Session working directory: " : "会话工作目录：") + displayPath}
                    title={isCloud ? displayPath : dirState.path}
                    onClick={() => setMenuOpen(!menuOpen)}
                    onKeyDown={(event) => { if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setMenuOpen(true); } }}
                    style={{
                        display: "inline-flex", alignItems: "center", gap: 5,
                        maxWidth: "100%", height: 24, padding: "0 8px",
                        border: `1px solid ${t.fieldBorder}`, borderRadius: 4,
                        background: menuOpen ? `color-mix(in srgb, ${t.btnColor} 10%, ${t.fieldBg})` : t.fieldBg,
                        color: menuOpen ? t.btnColor : t.textMuted,
                        cursor: "pointer", fontSize: 11, lineHeight: 1.4,
                        transition: "background 150ms ease, border-color 150ms ease",
                        boxSizing: "border-box",
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
                {menuOpen && menuPosition && typeof document !== "undefined" && createPortal(
                    <div
                        ref={menuRef}
                        role="menu"
                        data-testid="working-dir-menu"
                        onKeyDown={handleMenuKeyDown}
                        style={{
                            position: "fixed", left: menuPosition.left, top: menuPosition.top,
                            transform: menuPosition.openUp ? "translateY(-100%)" : undefined,
                            zIndex: 40000,
                            background: t.bg, border: `1px solid ${t.fieldBorder}`,
                            borderRadius: 6, boxShadow: "0 4px 8px rgba(15, 23, 42, 0.14)",
                            padding: 4, minWidth: MENU_WIDTH,
                            maxHeight: menuPosition.maxHeight, overflowY: "auto",
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
                    </div>,
                    document.body,
                )}
            </div>
        </div>
    );
}
