import { useCallback, useEffect, useRef, useState } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { GetTabWorkingDir, SetTabWorkingDir, OpenProjectDirectory, SelectWorkingDir } from "../../../wailsjs/go/main/App";
import { IconFolder, IconFolderOpen } from "./WorkbenchIcons";

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
 * - Click ▾ → opens directory picker to switch
 * - Shows "(默认)" badge when using system default directory
 */
export function ProjectDirBar({ tabId, theme: t, lang }: ProjectDirBarProps) {
    const [dirState, setDirState] = useState<DirState | null>(null);
    const mountedRef = useRef(true);

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
        try {
            // Use the existing backend directory picker dialog.
            const selected = await SelectWorkingDir();
            if (!selected || !mountedRef.current) return;
            await SetTabWorkingDir(tabId, selected);
            setDirState({ path: selected, isDefault: false });
        } catch (e) {
            console.error("[ProjectDirBar] switch dir failed:", e);
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
            <span
                title={dirState.path}
                onClick={handleOpenDir}
                style={{
                    cursor: "pointer",
                    color: isDefault ? t.textMuted : t.linkColor,
                    textDecoration: isDefault ? "none" : "underline",
                    textDecorationColor: isDefault ? undefined : `${t.linkColor}44`,
                    whiteSpace: "nowrap",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    flex: 1,
                    minWidth: 0,
                }}
            >
                {displayPath}
            </span>
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
                title={lang === "en" ? "Switch project directory" : "切换项目目录"}
                style={{
                    border: "none",
                    background: "transparent",
                    color: t.textMuted,
                    cursor: "pointer",
                    padding: "0 4px",
                    fontSize: 12,
                    opacity: 0.7,
                    flexShrink: 0,
                }}
                onMouseEnter={e => { (e.target as HTMLElement).style.opacity = "1"; }}
                onMouseLeave={e => { (e.target as HTMLElement).style.opacity = "0.7"; }}
            >
                ▾
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
