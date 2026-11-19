/**
 * "N files changed" chip with the floating file list on hover/focus. Lives in
 * the coding plan checklist bar; while a goal command owns the session the
 * goal quick bar hosts it in its right half instead (AIAssistantPanel hands
 * the same fileChanges over and suppresses the checklist copy).
 */
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";
import { compactCodingTrailPath, type CodingAgentFileChange } from "./CodingAgentProgressStatus";
import type { CodingBannerChrome } from "./CodingWorkbenchControlPanel";

export type CodingAgentFileChangesChrome = Pick<CodingBannerChrome, "border" | "surface" | "accentStrong">;

/**
 * Max room for a floating capc list: the gap between `host`'s top edge and
 * the transcript above it. Stop at the transcript, not the column — the
 * column also holds the task header, and the list must not cover its
 * buttons. A hidden transcript (display:none during the new-task wizard)
 * reports a 0 rect; treating that as the top of the page makes the list
 * taller than the column, and the clipped rows cannot scroll.
 */
export function capcFloatingRoomPx(host: HTMLElement): number {
    const top = host.getBoundingClientRect().top;
    const column = host.closest(".aap-chat-column");
    const output = column instanceof HTMLElement
        ? column.querySelector("[data-testid='ai-output-container']")
        : null;
    const outputBox = output instanceof HTMLElement ? output.getBoundingClientRect() : null;
    const columnBox = column instanceof HTMLElement ? column.getBoundingClientRect() : null;
    const outputUsable = !!outputBox && outputBox.height > 0;
    let limit = outputUsable ? outputBox.top : (columnBox ? columnBox.top : top);
    if (outputUsable && columnBox) limit = Math.max(limit, columnBox.top);
    // A zero rect (jsdom, or the bar is not on screen yet) has no measured
    // gap. Anywhere else, use the real space so a short column does not force
    // the list past the clip.
    const measured = Math.floor(top - limit - 8);
    return measured > 0 ? measured : 120;
}

/**
 * Keeps `--capc-room` on the host while a floating capc list is open, and
 * re-fits it on column/transcript resizes. `resolveHost` and `afterFit` are
 * read through refs so callers' inline closures do not re-subscribe the
 * observers every render.
 */
export function useCapcFloatingRoom(
    open: boolean,
    resolveHost: () => HTMLElement | null,
    afterFit?: () => void,
): void {
    const resolveHostRef = useRef(resolveHost);
    const afterFitRef = useRef(afterFit);
    resolveHostRef.current = resolveHost;
    afterFitRef.current = afterFit;
    useLayoutEffect(() => {
        if (!open) return;
        const host = resolveHostRef.current();
        if (!host) return;
        const fit = () => {
            host.style.setProperty("--capc-room", `${capcFloatingRoomPx(host)}px`);
            afterFitRef.current?.();
        };
        fit();
        const column = host.closest(".aap-chat-column");
        const observed = column instanceof HTMLElement ? column : host;
        const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fit);
        observer?.observe(observed);
        // The composer grows inside the column without resizing the column
        // itself. The transcript above the bar shrinks in that case.
        if (column instanceof HTMLElement) {
            const output = column.querySelector("[data-testid='ai-output-container']");
            if (output instanceof HTMLElement) observer?.observe(output);
        }
        window.addEventListener("resize", fit);
        return () => {
            observer?.disconnect();
            window.removeEventListener("resize", fit);
        };
    }, [open]);
}

export type CodingAgentFileChangesProps = {
    lang?: string;
    theme: Theme;
    chrome: CodingAgentFileChangesChrome;
    fileChanges: CodingAgentFileChange[];
    /** Clicking a changed-file row focuses it in the code preview. */
    onOpenFile?: (path: string) => void;
};

export function codingAgentFileChangesLabel(lang: string | undefined, count: number): string {
    return localizeText(
        lang,
        count === 1 ? "1 file changed" : `${count} files changed`,
        `${count} 个文件已修改`,
        `${count} 個檔案已修改`,
    );
}

/** One changed-file row; interactive only when a preview handler exists. */
function ChecklistFileRow({
    row,
    addColor,
    delColor,
    onOpenFile,
}: {
    row: CodingAgentFileChange;
    addColor: string;
    delColor: string;
    onOpenFile?: (path: string) => void;
}) {
    // An empty path can never resolve to a preview file — keep the row inert
    // instead of looking clickable and doing nothing.
    const openable = !!onOpenFile && !!row.path;
    return (
        <li
            className="capc-file-row"
            data-testid="coding-agent-plan-file"
            title={row.path}
            role={openable ? "button" : undefined}
            tabIndex={openable ? 0 : undefined}
            data-preview-path={openable ? row.path : undefined}
            // Same as the toggle: keep the caret in the composer; the click
            // still opens.
            onMouseDown={openable ? (event) => { event.preventDefault(); } : undefined}
            onClick={openable ? () => { onOpenFile?.(row.path); } : undefined}
            onKeyDown={openable ? (event) => {
                if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    onOpenFile?.(row.path);
                    return;
                }
                // Blur runs through the wrapper onBlur and closes the list,
                // same as the toggle.
                if (event.key === "Escape") {
                    event.preventDefault();
                    event.currentTarget.blur();
                }
            } : undefined}
        >
            <span className="capc-file-path">{compactCodingTrailPath(row.path) || row.path}</span>
            <span className="capc-file-stat">
                <span style={{ color: addColor }}>+{row.added || 0}</span>
                <span style={{ color: delColor }}>-{row.removed || 0}</span>
            </span>
        </li>
    );
}

export function CodingAgentFileChanges({
    lang,
    theme,
    chrome,
    fileChanges,
    onOpenFile,
}: CodingAgentFileChangesProps) {
    const [filesHovered, setFilesHovered] = useState(false);
    const [filesFocused, setFilesFocused] = useState(false);
    const fileTimer = useRef<number | null>(null);
    const fileHoverRef = useRef<HTMLDivElement>(null);
    const hasFiles = fileChanges.length > 0;
    useEffect(() => () => {
        if (fileTimer.current != null) window.clearTimeout(fileTimer.current);
    }, []);
    useEffect(() => {
        if (hasFiles) return;
        if (fileTimer.current != null) {
            window.clearTimeout(fileTimer.current);
            fileTimer.current = null;
        }
        setFilesHovered(false);
        setFilesFocused(false);
    }, [hasFiles]);
    const showFiles = () => {
        if (!hasFiles) return;
        if (fileTimer.current != null) {
            window.clearTimeout(fileTimer.current);
            fileTimer.current = null;
        }
        setFilesHovered(true);
    };
    const hideFiles = () => {
        if (fileTimer.current != null) window.clearTimeout(fileTimer.current);
        const tick = () => {
            fileTimer.current = null;
            if (fileHoverRef.current?.matches(":hover")) {
                fileTimer.current = window.setTimeout(tick, 120);
                return;
            }
            setFilesHovered(false);
        };
        fileTimer.current = window.setTimeout(tick, 120);
    };
    const filesOpen = hasFiles && (filesHovered || filesFocused);
    // The list floats above whichever bar hosts the chip, anchored to the
    // nearest positioned ancestor (checklist section / goal bar), so that
    // ancestor is also the measuring host.
    useCapcFloatingRoom(filesOpen, () => {
        const el = fileHoverRef.current;
        if (!el) return null;
        return el.offsetParent instanceof HTMLElement ? el.offsetParent : el;
    });

    if (!hasFiles) return null;
    const fileLabel = codingAgentFileChangesLabel(lang, fileChanges.length);
    const addColor = theme.isDark ? "#5dba8a" : "#2f7a58";
    const delColor = theme.isDark ? "#f07178" : "#c43838";
    const added = fileChanges.reduce((sum, row) => sum + (row.added || 0), 0);
    const removed = fileChanges.reduce((sum, row) => sum + (row.removed || 0), 0);

    return (
        <div
            ref={fileHoverRef}
            className={"capc-files" + (filesOpen ? " capc-open" : "")}
            data-testid="coding-agent-plan-files"
            onMouseEnter={showFiles}
            onMouseLeave={hideFiles}
            // Focus tracking lives on the wrapper so tabbing from the
            // toggle into a file row does not collapse the list.
            onFocus={() => setFilesFocused(true)}
            onBlur={(event) => {
                if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
                    setFilesFocused(false);
                }
            }}
        >
            <button
                type="button"
                className="capc-files-toggle"
                data-testid="coding-agent-plan-files-toggle"
                aria-expanded={filesOpen}
                aria-controls="coding-agent-plan-files-body"
                title={localizeText(lang, "Hover to see changed files", "悬停查看修改的文件", "懸停查看修改的檔案")}
                onMouseDown={(event) => { event.preventDefault(); }}
                onKeyDown={(event) => {
                    if (event.key !== "Escape") return;
                    event.preventDefault();
                    setFilesFocused(false);
                    event.currentTarget.blur();
                }}
            >
                <span data-testid="coding-agent-plan-files-label" className="capc-files-label">{fileLabel}</span>
                <span className="capc-file-stat" aria-hidden="true">
                    <span style={{ color: addColor }}>+{added}</span>
                    <span style={{ color: delColor }}>-{removed}</span>
                </span>
                <span aria-hidden="true" className="capc-chevron">
                    <svg viewBox="0 0 16 16" width="14" height="14">
                        <path d="M4.6 6.4l3.4 3.4 3.4-3.4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                </span>
            </button>
            <div className="capc-files-popover" aria-hidden={!filesOpen}>
                <div
                    id="coding-agent-plan-files-body"
                    data-testid="coding-agent-plan-files-body"
                    className="capc-popover-card"
                    aria-hidden={!filesOpen}
                    style={{ border: `1px solid ${chrome.border}`, background: chrome.surface, color: theme.text }}
                >
                    <div className="capc-files-title" style={{ color: chrome.accentStrong }}>{fileLabel}</div>
                    <ul className="capc-file-rows">
                        {fileChanges.map((row) => (
                            <ChecklistFileRow
                                key={row.path}
                                row={row}
                                addColor={addColor}
                                delColor={delColor}
                                onOpenFile={onOpenFile}
                            />
                        ))}
                    </ul>
                </div>
            </div>
        </div>
    );
}
