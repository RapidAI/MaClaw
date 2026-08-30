import { useCallback, useEffect, useRef, useState } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { GetTabWorkingDir, SetTabWorkingDir, OpenProjectDirectory, SelectWorkingDir } from "../../../wailsjs/go/main/App";
import { isCloudWorkspacePath } from "./codingTaskMode";
import { IconEdit, IconFolder, IconFolderOpen } from "./WorkbenchIcons";

export interface ProjectDirBarProps {
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
    /** Cloud workspace: click the label to reopen the in-app file browser instead of Explorer. */
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

/**
 * Displays the current working directory for the active tab.
 * - Click path → opens directory in system file explorer (cloud: in-app file browser)
 * - Click the "Change" button → opens the directory picker (hidden for cloud)
 * - Shows "(默认)" badge when using system default directory
 */
export function ProjectDirBar({ tabId, sessionReadyRevision = 0, theme: t, lang, onWorkingDirChange, onWorkingDirResolved, onOpenCloudFiles }: ProjectDirBarProps) {
    const [dirState, setDirState] = useState<DirState | null>(null);
    const [selectingDirectory, setSelectingDirectory] = useState(false);
    const mountedRef = useRef(true);
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
        refresh();
        return () => {
            cancelled = true;
        };
    }, [tabId, sessionReadyRevision, onWorkingDirResolved]);

    const handleOpenDir = useCallback(() => {
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
        } catch (e) {
            console.error("[ProjectDirBar] switch dir failed:", e);
        } finally {
            directorySelectionInFlightRef.current = false;
            if (mountedRef.current) setSelectingDirectory(false);
        }
    }, [tabId, onWorkingDirChange]);

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
    const isCloud = isCloudWorkspacePath(dirState.path);
    const displayPath = isCloud
        ? (lang === "en" ? "Cloud workspace" : "云端工作区")
        : truncateMiddle(dirState.path, 55);
    const badgeText = isCloud
        ? (lang === "en" ? "remote" : "云端")
        : isDefault
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
                title={isCloud ? (lang === "en" ? "Cloud workspace files" : "云端工作区文件") : dirState.path}
                aria-label={isCloud ? (lang === "en" ? "Open cloud workspace files" : "打开云端工作区文件") : (lang === "en" ? "Open current working directory" : "打开当前工作目录")}
                onClick={handleOpenDir}
                style={{
                    border: "none",
                    background: "transparent",
                    padding: 0,
                    cursor: "pointer",
                    color: isCloud ? t.linkColor : (isDefault ? t.textMuted : t.linkColor),
                    textDecoration: isDefault || isCloud ? "none" : "underline",
                    textDecorationColor: isDefault || isCloud ? undefined : `${t.linkColor}44`,
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
                    fontStyle: isCloud ? "normal" : "italic",
                    fontWeight: isCloud ? 700 : undefined,
                }}>
                    {badgeText}
                </span>
            )}
            {!isCloud && <button
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
            </button>}
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
