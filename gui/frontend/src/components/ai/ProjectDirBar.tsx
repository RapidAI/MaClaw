import { useCallback, useEffect, useRef, useState } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { GetTabWorkingDir, SetTabWorkingDir, OpenProjectDirectory, SelectWorkingDir } from "../../../wailsjs/go/main/App";
import { IconEdit, IconFolder, IconFolderOpen } from "./WorkbenchIcons";

export interface ProjectDirBarProps {
    /** Current active tab ID. Empty string = local tab. */
    tabId: string;
    theme: Theme;
    lang?: string;
}

interface DirState {
    path: string;
    isDefault: boolean;
}

/**
 * Displays the current working directory for the active tab.
 * - Click path → opens directory in system file explorer
 * - Click the "Change" button → opens the directory picker
 * - Shows "(默认)" badge when using system default directory
 */
export function ProjectDirBar({ tabId, theme: t, lang }: ProjectDirBarProps) {
    const [dirState, setDirState] = useState<DirState | null>(null);
    const [selectingDirectory, setSelectingDirectory] = useState(false);
    const mountedRef = useRef(true);
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
        setDirState(null); // Clear stale value during tab switch.
        GetTabWorkingDir(tabId).then((result: any) => {
            if (cancelled || !mountedRef.current) return;
            if (result && typeof result.path === "string") {
                setDirState({ path: result.path, isDefault: !!result.is_default });
            }
        }).catch(() => {});
        return () => { cancelled = true; };
    }, [tabId]);

    const handleOpenDir = useCallback(() => {
        if (dirState?.path) {
            OpenProjectDirectory(dirState.path).catch(() => {});
        }
    }, [dirState?.path]);

    const handleSwitchDir = useCallback(async () => {
        if (directorySelectionInFlightRef.current) return;
        directorySelectionInFlightRef.current = true;
        setSelectingDirectory(true);
        try {
            // Use the existing backend directory picker dialog.
            const selected = await SelectWorkingDir();
            if (!selected || !mountedRef.current) return;
            await SetTabWorkingDir(tabId, selected);
            setDirState({ path: selected, isDefault: false });
        } catch (e) {
            console.error("[ProjectDirBar] switch dir failed:", e);
        } finally {
            directorySelectionInFlightRef.current = false;
            if (mountedRef.current) setSelectingDirectory(false);
        }
    }, [tabId]);

    if (!dirState) {
        // Show a fixed-height placeholder to prevent layout shift during tab switch.
        return (
            <div data-testid="project-dir-bar" style={{
                display: "flex", alignItems: "center", padding: "3px 12px",
                fontSize: 12, lineHeight: "20px", borderBottom: `1px solid ${t.titleBarBorder}`,
                background: t.titleBarBg, minHeight: 0, flexShrink: 0,
            }}>
                <span style={{ opacity: 0.35, display: "inline-flex" }}><IconFolder size={14} color="currentColor" /></span>
            </div>
        );
    }

    const isDefault = dirState.isDefault;
    const displayPath = truncateMiddle(dirState.path, 55);
    const badgeText = isDefault
        ? (lang === "en" ? "(default)" : "(默认)")
        : "";

    return (
        <div
            data-testid="project-dir-bar"
            style={{
                display: "flex",
                alignItems: "center",
                gap: 6,
                padding: "3px 12px",
                fontSize: 12,
                lineHeight: "20px",
                borderBottom: `1px solid ${t.titleBarBorder}`,
                background: t.titleBarBg,
                minHeight: 0,
                flexShrink: 0,
                overflow: "hidden",
            }}
        >
            <span style={{ opacity: 0.75, flexShrink: 0, display: "inline-flex", color: t.textMuted }}>
                {isDefault ? <IconFolder size={14} /> : <IconFolderOpen size={14} />}
            </span>
            <button
                type="button"
                title={dirState.path}
                aria-label={lang === "en" ? "Open current working directory" : "打开当前工作目录"}
                onClick={handleOpenDir}
                style={{
                    border: "none",
                    background: "transparent",
                    padding: 0,
                    cursor: "pointer",
                    color: isDefault ? t.textMuted : t.linkColor,
                    textDecoration: isDefault ? "none" : "underline",
                    textDecorationColor: isDefault ? undefined : `${t.linkColor}44`,
                    whiteSpace: "nowrap",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    flex: 1,
                    minWidth: 0,
                    textAlign: "left",
                }}
                onFocus={e => { e.currentTarget.style.outline = `2px solid ${t.linkColor}`; e.currentTarget.style.outlineOffset = "2px"; }}
                onBlur={e => { e.currentTarget.style.outline = "none"; }}
            >
                {displayPath}
            </button>
            {badgeText && (
                <span style={{
                    fontSize: 10,
                    color: t.textMuted,
                    opacity: 0.7,
                    flexShrink: 0,
                    fontStyle: "italic",
                }}>
                    {badgeText}
                </span>
            )}
            <button
                type="button"
                onClick={handleSwitchDir}
                title={lang === "en" ? "Choose a different working directory" : "选择其他工作目录"}
                aria-label={lang === "en" ? "Choose a different working directory" : "选择其他工作目录"}
                disabled={selectingDirectory}
                style={{
                    display: "inline-flex",
                    alignItems: "center",
                    gap: 4,
                    minHeight: 26,
                    padding: "3px 8px",
                    border: `1px solid ${t.titleBarBorder}`,
                    borderRadius: 6,
                    background: t.fieldBg,
                    color: t.linkColor,
                    cursor: selectingDirectory ? "progress" : "pointer",
                    fontSize: 11,
                    fontWeight: 600,
                    lineHeight: 1,
                    flexShrink: 0,
                    transition: "background 150ms ease, border-color 150ms ease",
                    opacity: selectingDirectory ? 0.65 : 1,
                }}
                onMouseEnter={e => {
                    e.currentTarget.style.background = t.titleBarBg;
                    e.currentTarget.style.borderColor = t.linkColor;
                }}
                onMouseLeave={e => {
                    e.currentTarget.style.background = t.fieldBg;
                    e.currentTarget.style.borderColor = t.titleBarBorder;
                }}
                onFocus={e => { e.currentTarget.style.outline = `2px solid ${t.linkColor}`; e.currentTarget.style.outlineOffset = "2px"; }}
                onBlur={e => { e.currentTarget.style.outline = "none"; }}
            >
                <IconEdit size={13} />
                <span>{selectingDirectory ? (lang === "en" ? "Choosing…" : "选择中…") : (lang === "en" ? "Change" : "切换目录")}</span>
            </button>
        </div>
    );
}

/** Truncates a path in the middle: D:\very\long\...\final\dir */
function truncateMiddle(path: string, maxLen: number): string {
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
