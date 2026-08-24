/**
 * FileTabBar — file name tab bar displayed at the top of the Code Preview Panel.
 *
 * Renders one tab per file in the files map. Each tab shows:
 *   - File name only as label (extracted from full path)
 *   - Full file path as tooltip (title attribute)
 *   - Compact change badge: `+12 -3`, or MOD / NEW / READ when no line delta
 *   - Active tab highlighted with distinct theme colors
 *   - Close (×) button when onCloseFile is provided
 *
 * When tabs overflow the available width, shows only as many as fit (active
 * file always kept visible) and puts the rest behind a sticky +N / ⋯ control
 * (VS Code-style open editors). Capacity = width estimate + one-pass child
 * measurement so long labels collapse without multi-frame shrink cascades.
 */
import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
    codeFileLineDeltaHasChange,
    computeCodeFileLineDelta,
    formatCodeFileLineDelta,
    getDisplayFilePaths,
    getMruCycleOrder,
    isCodeFileDirty,
    type CodeFile,
} from './useCodePreviewState';
import { ShowItemInFolder, OpenSystemUrl } from '../../../wailsjs/go/main/App';

// ── Theme Interface ──

/**
 * CodePreviewTheme — theme colors for the code preview panel.
 * Will be consolidated in Task 9; defined here as the canonical source.
 */
export interface CodePreviewTheme {
    bg: string;
    text: string;
    textMuted: string;
    border: string;
    lineNumBg: string;
    lineNumText: string;
    tabBg: string;
    tabActiveBg: string;
    tabActiveText: string;
    tabHoverBg: string;
    diffAddBg: string;
    diffAddText: string;
    diffDeleteBg: string;
    diffDeleteText: string;
    // Syntax highlighting colors
    syntaxKeyword: string;
    syntaxString: string;
    syntaxComment: string;
    syntaxNumber: string;
    syntaxFunction: string;
    syntaxType: string;
    syntaxOperator: string;
}

// ── Pure Helper Functions (exported for testing) ──

/**
 * Extract the file name from a file path.
 *
 * Handles both Unix (/) and Windows (\) separators.
 * For paths with no separator, returns the entire string.
 *
 * @param filePath - Full file path string
 * @returns The last path segment (file name)
 */
export function extractFileName(filePath: string): string {
    const lastSlash = Math.max(filePath.lastIndexOf('/'), filePath.lastIndexOf('\\'));
    if (lastSlash === -1) {
        return filePath;
    }
    return filePath.substring(lastSlash + 1);
}

/**
 * Get the visual indicator for a file's operation type.
 *
 * @param opType - 'modify', 'create', or 'read'
 * @returns Compact label for modify, create, or read.
 */
export function getOpTypeIndicator(opType: 'create' | 'modify' | 'read'): string {
    if (opType === 'modify') return 'MOD';
    if (opType === 'read') return 'READ';
    return 'NEW';
}

/** Codex-style `+12 -3` marks for a preview file. */
export function CodeFileDiffStat({
    file,
    theme,
    testId,
}: {
    file: Pick<CodeFile, 'opType' | 'content' | 'original'>;
    theme: Pick<CodePreviewTheme, 'diffAddText' | 'diffDeleteText'>;
    testId?: string;
}) {
    const delta = computeCodeFileLineDelta(file);
    if (!codeFileLineDeltaHasChange(delta)) return null;
    return (
        <span
            data-testid={testId || 'code-file-diff-stat'}
            style={{
                display: 'inline-flex',
                gap: 4,
                fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
                fontSize: 10,
                fontWeight: 600,
                fontVariantNumeric: 'tabular-nums',
                lineHeight: '16px',
                flexShrink: 0,
            }}
        >
            <span style={{ color: theme.diffAddText }}>+{delta.added}</span>
            <span style={{ color: theme.diffDeleteText }}>-{delta.removed}</span>
        </span>
    );
}

function FileOpOrDiffBadge({
    file,
    theme,
    testId,
}: {
    file: CodeFile;
    theme: CodePreviewTheme;
    testId?: string;
}) {
    if (codeFileLineDeltaHasChange(computeCodeFileLineDelta(file))) {
        return <CodeFileDiffStat file={file} theme={theme} testId={testId} />;
    }
    return (
        <span
            style={{
                minWidth: 24,
                padding: '0 4px',
                borderRadius: 3,
                border: `1px solid ${theme.border}`,
                color: theme.textMuted,
                fontSize: 10,
                fontWeight: 700,
                lineHeight: '16px',
                textAlign: 'center',
                flexShrink: 0,
            }}
        >
            {getOpTypeIndicator(file.opType)}
        </span>
    );
}

/**
 * Compute which file paths should be visible in the tab bar.
 * Rules:
 * - Active file is always visible when present.
 * - Pinned files are preferred next (when they fit).
 * - Remaining slots filled in original list order.
 * - Result preserves relative order of the input list.
 */
export function computeVisibleFilePaths(
    filePaths: string[],
    activeFilePath: string,
    maxVisible: number,
    pinnedPaths: string[] = [],
): string[] {
    if (maxVisible <= 0) return [];
    if (maxVisible >= filePaths.length) return filePaths.slice();

    const openSet = new Set(filePaths);
    const used = new Set<string>();
    const preferred: string[] = [];

    if (activeFilePath && openSet.has(activeFilePath)) {
        preferred.push(activeFilePath);
        used.add(activeFilePath);
    }

    for (const path of pinnedPaths) {
        if (preferred.length >= maxVisible) break;
        if (openSet.has(path) && !used.has(path)) {
            preferred.push(path);
            used.add(path);
        }
    }

    for (const path of filePaths) {
        if (preferred.length >= maxVisible) break;
        if (!used.has(path)) {
            preferred.push(path);
            used.add(path);
        }
    }

    // Preserve original order among the selected paths.
    return filePaths.filter((path) => used.has(path));
}

/**
 * Cycle the active file path by delta (e.g. Ctrl+Tab / Ctrl+Shift+Tab, Arrow keys).
 * Wraps around. Returns null when there are no files.
 */
export function cycleFilePath(
    filePaths: string[],
    activeFilePath: string,
    delta: number,
): string | null {
    if (filePaths.length === 0) return null;
    if (filePaths.length === 1) return filePaths[0];
    const current = filePaths.indexOf(activeFilePath);
    const start = current >= 0 ? current : 0;
    const step = delta >= 0 ? 1 : -1;
    // Support multi-step deltas while wrapping.
    const steps = Math.abs(Math.trunc(delta)) || 1;
    let next = start;
    for (let i = 0; i < steps; i++) {
        next = (next + step + filePaths.length) % filePaths.length;
    }
    return filePaths[next];
}

/**
 * Filter open editor paths by a Quick-Open style query.
 * Matches case-insensitively against file name and full path.
 * Empty query returns all paths (order preserved).
 */
export function filterOpenFilePaths(filePaths: string[], query: string): string[] {
    const q = query.trim().toLowerCase();
    if (!q) return filePaths.slice();
    return filePaths.filter((path) => {
        const name = extractFileName(path).toLowerCase();
        const full = path.toLowerCase().replace(/\\/g, '/');
        return name.includes(q) || full.includes(q);
    });
}

/**
 * Compute the destination index when dragging `fromPath` onto `overPath`.
 * Placement is before/after based on the drag side; result is the final index
 * in the list AFTER removing fromPath.
 */
export function computeDropIndex(
    filePaths: string[],
    fromPath: string,
    overPath: string,
    placeAfter: boolean,
): number {
    const fromIndex = filePaths.indexOf(fromPath);
    const overIndex = filePaths.indexOf(overPath);
    if (fromIndex < 0 || overIndex < 0 || fromPath === overPath) {
        return fromIndex;
    }
    // Index in the list with fromPath removed.
    let target = overIndex;
    if (fromIndex < overIndex) {
        target = overIndex - 1;
    }
    if (placeAfter) {
        target += 1;
    }
    return Math.max(0, Math.min(filePaths.length - 1, target));
}

/**
 * Estimated width per file tab when deciding how many fit.
 * Tabs use maxWidth 180 + badge/close chrome; 150 is a conservative
 * middle ground so we collapse into the overflow control before clipping.
 */
export const MIN_TAB_WIDTH = 150;

/**
 * Fixed width for the sticky open-editors control. Keeping this constant
 * prevents "+N" vs "⋯" label changes from reflowing the tab strip.
 */
export const OVERFLOW_CONTROL_WIDTH = 48;

/**
 * How many file tabs fit in `availableWidth` (pixels of the clipped tab strip).
 * Returns at least 1 when there is any file; never exceeds `fileCount`.
 * Invalid / zero widths return null so callers can keep prior capacity.
 */
export function computeMaxVisibleTabs(
    availableWidth: number,
    fileCount: number,
    minTabWidth: number = MIN_TAB_WIDTH,
): number | null {
    if (fileCount <= 0) return 0;
    if (!(availableWidth > 0) || !(minTabWidth > 0)) return null;
    const maxTabs = Math.max(1, Math.floor(availableWidth / minTabWidth));
    return maxTabs >= fileCount ? fileCount : maxTabs;
}

/**
 * How many leading tabs fit in `clientWidth` given each tab's measured width.
 * The first tab is always kept (even if wider than the strip — it will clip),
 * matching VS Code's "active tab stays visible" behavior.
 */
export function countFittingTabs(
    childWidths: number[],
    clientWidth: number,
    epsilon: number = 1,
): number {
    if (childWidths.length === 0) return 0;
    if (clientWidth <= 0) return 1;
    let used = 0;
    let fit = 0;
    for (const w of childWidths) {
        const width = Math.max(0, w);
        if (fit > 0 && used + width > clientWidth + epsilon) break;
        used += width;
        fit += 1;
    }
    return Math.max(1, fit);
}

/**
 * Merge a width-based capacity estimate with the currently fitted count.
 * - Width shrank / estimate lower → take estimate immediately.
 * - Estimate higher → only grow when the strip is not overflowing (room to try more)
 *   and not above `maxFitted` (last measured fit — prevents grow/shrink loops).
 * - `maxFitted == null` means "no measurement ceiling yet".
 */
export function reconcileVisibleCount(
    prev: number,
    estimate: number | null,
    fileCount: number,
    stripOverflowing: boolean,
    maxFitted: number | null = null,
): number {
    if (fileCount <= 0) return 0;
    if (estimate == null) {
        return Math.min(Math.max(1, prev), fileCount);
    }
    let next = Math.min(Math.max(0, estimate), fileCount);
    if (next <= 0) return fileCount > 0 ? 1 : 0;
    if (maxFitted != null) {
        next = Math.min(next, Math.max(1, maxFitted));
    }
    const clampedPrev = Math.min(Math.max(1, prev), fileCount);
    if (next <= clampedPrev) return next;
    // Grow only when current tabs already fit — otherwise keep fitted count.
    if (!stripOverflowing) return next;
    return clampedPrev;
}

/**
 * Target visible count for the layout second pass.
 * When overflowing, snap to measured fit; when fitting, grow toward estimate
 * without exceeding maxFitted (avoids +1/-1 oscillation with wide tabs).
 */
export function nextVisibleCountFromLayout(
    prev: number,
    fileCount: number,
    estimate: number,
    overflowing: boolean,
    measuredFit: number | null,
    maxFitted: number | null,
): { count: number; maxFitted: number | null } {
    if (fileCount <= 0) return { count: 0, maxFitted: null };
    const clampedPrev = Math.min(Math.max(1, prev), fileCount);
    const est = Math.min(Math.max(1, estimate), fileCount);

    if (overflowing && measuredFit != null) {
        const fit = Math.min(Math.max(1, measuredFit), fileCount);
        return { count: fit, maxFitted: fit };
    }

    // Fits: grow toward estimate, but never above last measured ceiling.
    let target = est;
    if (maxFitted != null) {
        target = Math.min(target, Math.max(1, maxFitted));
    }
    if (target < clampedPrev) {
        return { count: target, maxFitted };
    }
    if (target > clampedPrev && !overflowing) {
        return { count: target, maxFitted };
    }
    return { count: clampedPrev, maxFitted };
}

/** Read observed width from a ResizeObserver entry (border-box preferred). */
export function resizeObserverEntryWidth(entry: ResizeObserverEntry): number {
    const box = entry.borderBoxSize;
    // Spec: borderBoxSize is a sequence; some engines expose a single object.
    if (box) {
        const first = Array.isArray(box) ? box[0] : (box as unknown as ResizeObserverSize);
        if (first && typeof first.inlineSize === 'number' && first.inlineSize > 0) {
            return first.inlineSize;
        }
    }
    return entry.contentRect?.width ?? 0;
}

/**
 * Compact label for the sticky overflow control (fixed width).
 * Keeps "+N" short so double-digit / triple-digit counts still fit.
 */
export function formatOverflowControlLabel(overflowCount: number, hasOverflow: boolean): string {
    if (!hasOverflow) return '\u22EF'; // ⋯
    if (overflowCount > 99) return '99+';
    return `+${Math.max(0, overflowCount)}`;
}

/** Collect offsetWidths of element children (tab buttons). */
export function measureChildWidths(parent: Element): number[] {
    const widths: number[] = [];
    const children = parent.children;
    for (let i = 0; i < children.length; i++) {
        widths.push((children[i] as HTMLElement).offsetWidth);
    }
    return widths;
}

// ── Component Props ──

export interface FileTabBarProps {
    files: Map<string, CodeFile>;
    activeFilePath: string;
    /** Pinned paths — sorted left / preferred visible. */
    pinnedPaths?: string[];
    /** MRU order (most recent first) used by Ctrl+Tab. Falls back to spatial order. */
    mruOrder?: string[];
    onSelectFile: (filePath: string) => void;
    /** Close a single file tab (VS Code-style). Optional for backward compatibility. */
    onCloseFile?: (filePath: string) => void;
    /** Close all tabs except the given path. */
    onCloseOtherFiles?: (keepPath: string) => void;
    /** Close all tabs to the right of the given path. */
    onCloseFilesToTheRight?: (fromPath: string) => void;
    /** Close every open file tab. */
    onCloseAllFiles?: () => void;
    /** Drag-reorder: move fromPath to toIndex in open-file order. */
    onMoveFile?: (fromPath: string, toIndex: number) => void;
    /** Pin / unpin a file tab. */
    onTogglePinFile?: (filePath: string) => void;
    theme: CodePreviewTheme;
    lang?: string;
}

// ── Context Menu ──

interface ContextMenuState {
    filePath: string;
    absPath: string;
    x: number;
    y: number;
    /** True when Close Others would actually close at least one unpinned tab. */
    canCloseOthers: boolean;
    /** True when Close to the Right would close at least one unpinned tab. */
    canCloseToRight: boolean;
    /** True when Close All would close at least one unpinned tab. */
    canCloseAll: boolean;
    isPinned: boolean;
}

function FileTabContextMenu({
    menu,
    theme,
    onClose,
    onCloseFile,
    onCloseOtherFiles,
    onCloseFilesToTheRight,
    onCloseAllFiles,
    onTogglePinFile,
    lang,
}: {
    menu: ContextMenuState;
    theme: CodePreviewTheme;
    onClose: () => void;
    onCloseFile?: (filePath: string) => void;
    onCloseOtherFiles?: (keepPath: string) => void;
    onCloseFilesToTheRight?: (fromPath: string) => void;
    onCloseAllFiles?: () => void;
    onTogglePinFile?: (filePath: string) => void;
    lang?: string;
}) {
    // Match CodePreviewPanel default (`lang = 'en'`) — undefined is English, not Chinese.
    const isZh = (lang ?? 'en').startsWith('zh');
    const isZhHant = lang === 'zh-Hant';
    const text = (en: string, zhHans: string, zhHant: string = zhHans) => (
        isZhHant ? zhHant : isZh ? zhHans : en
    );
    const hasAbsPath = Boolean(menu.absPath);
    const hasCloseActions = Boolean(onCloseFile || onCloseOtherFiles || onCloseFilesToTheRight || onCloseAllFiles);

    const copyToClipboard = async (text: string) => {
        if (!text) return;
        try {
            if (navigator.clipboard?.writeText) {
                await navigator.clipboard.writeText(text);
                return;
            }
        } catch {
            // fall through to legacy path
        }
        try {
            const ta = document.createElement('textarea');
            ta.value = text;
            ta.style.position = 'fixed';
            ta.style.left = '-9999px';
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            document.body.removeChild(ta);
        } catch {
            // ignore clipboard failures
        }
    };

    const handleRevealInExplorer = () => {
        if (!hasAbsPath) return;
        onClose();
        void ShowItemInFolder(menu.absPath);
    };
    const handleOpenExternal = () => {
        if (!hasAbsPath) return;
        onClose();
        void OpenSystemUrl(menu.absPath);
    };
    const handleCopyPath = () => {
        onClose();
        void copyToClipboard(menu.absPath || menu.filePath);
    };
    const handleCopyRelativePath = () => {
        onClose();
        void copyToClipboard(menu.filePath);
    };
    const handleCopyFileName = () => {
        onClose();
        void copyToClipboard(extractFileName(menu.filePath));
    };
    const handleCloseTab = () => {
        onClose();
        onCloseFile?.(menu.filePath);
    };
    const handleCloseOthers = () => {
        onClose();
        onCloseOtherFiles?.(menu.filePath);
    };
    const handleCloseToRight = () => {
        onClose();
        onCloseFilesToTheRight?.(menu.filePath);
    };
    const handleCloseAll = () => {
        onClose();
        onCloseAllFiles?.();
    };
    const handleTogglePin = () => {
        onClose();
        onTogglePinFile?.(menu.filePath);
    };

    React.useEffect(() => {
        const dismiss = (e: MouseEvent) => {
            if (!(e.target as HTMLElement)?.closest?.('[data-file-tab-context-menu]')) onClose();
        };
        const dismissKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') onClose();
        };
        document.addEventListener('mousedown', dismiss);
        document.addEventListener('keydown', dismissKey);
        return () => {
            document.removeEventListener('mousedown', dismiss);
            document.removeEventListener('keydown', dismissKey);
        };
    }, [onClose]);

    const itemStyle: React.CSSProperties = {
        display: 'block',
        width: '100%',
        padding: '6px 14px',
        border: 'none',
        background: 'none',
        color: theme.text,
        fontSize: 12,
        textAlign: 'left',
        cursor: 'pointer',
        whiteSpace: 'nowrap',
    };
    const disabledItemStyle: React.CSSProperties = {
        ...itemStyle,
        color: theme.textMuted,
        cursor: 'default',
        opacity: 0.55,
    };

    const hoverHandlers = (enabled: boolean) => ({
        onMouseEnter: (e: React.MouseEvent<HTMLButtonElement>) => {
            if (enabled) e.currentTarget.style.background = theme.tabHoverBg;
        },
        onMouseLeave: (e: React.MouseEvent<HTMLButtonElement>) => {
            e.currentTarget.style.background = 'none';
        },
    });

    return (
        <div
            data-file-tab-context-menu
            data-testid="file-tab-context-menu"
            style={{
                position: 'fixed',
                left: Math.min(menu.x, window.innerWidth - 200),
                top: Math.min(menu.y, window.innerHeight - 220),
                background: theme.bg,
                border: `1px solid ${theme.border}`,
                borderRadius: 6,
                boxShadow: '0 4px 16px rgba(0,0,0,0.18)',
                zIndex: 99999,
                padding: '4px 0',
                minWidth: 180,
            }}
        >
            {hasAbsPath && (
                <>
                    <button type="button" style={itemStyle} onClick={handleRevealInExplorer} {...hoverHandlers(true)}>
                        {text('Reveal in Explorer', '在资源管理器中显示', '在檔案總管中顯示')}
                    </button>
                    <button type="button" style={itemStyle} onClick={handleOpenExternal} {...hoverHandlers(true)}>
                        {text('Open with default app', '使用默认应用打开', '使用預設應用程式開啟')}
                    </button>
                </>
            )}
            <button
                type="button"
                data-testid="file-tab-ctx-copy-path"
                style={{
                    ...itemStyle,
                    borderTop: hasAbsPath ? `1px solid ${theme.border}` : undefined,
                    marginTop: hasAbsPath ? 2 : 0,
                }}
                onClick={handleCopyPath}
                {...hoverHandlers(true)}
            >
                {text('Copy Path', '复制路径', '複製路徑')}
            </button>
            <button
                type="button"
                data-testid="file-tab-ctx-copy-relative"
                style={itemStyle}
                onClick={handleCopyRelativePath}
                {...hoverHandlers(true)}
            >
                {text('Copy Relative Path', '复制相对路径', '複製相對路徑')}
            </button>
            <button
                type="button"
                data-testid="file-tab-ctx-copy-name"
                style={itemStyle}
                onClick={handleCopyFileName}
                {...hoverHandlers(true)}
            >
                {text('Copy File Name', '复制文件名', '複製檔名')}
            </button>
            {onTogglePinFile && (
                <button
                    type="button"
                    data-testid="file-tab-ctx-pin"
                    style={{
                        ...itemStyle,
                        borderTop: `1px solid ${theme.border}`,
                        marginTop: 2,
                    }}
                    onClick={handleTogglePin}
                    {...hoverHandlers(true)}
                >
                    {menu.isPinned
                        ? text('Unpin', '取消固定')
                        : text('Pin', '固定')}
                </button>
            )}
            {hasCloseActions && (
                <>
                    {onCloseFile && (
                        <button
                            type="button"
                            data-testid="file-tab-ctx-close"
                            style={{
                                ...itemStyle,
                                borderTop: `1px solid ${theme.border}`,
                                marginTop: 2,
                            }}
                            onClick={handleCloseTab}
                            {...hoverHandlers(true)}
                        >
                            {text('Close', '关闭', '關閉')}
                        </button>
                    )}
                    {onCloseOtherFiles && (
                        <button
                            type="button"
                            data-testid="file-tab-ctx-close-others"
                            style={menu.canCloseOthers ? itemStyle : disabledItemStyle}
                            disabled={!menu.canCloseOthers}
                            onClick={menu.canCloseOthers ? handleCloseOthers : undefined}
                            {...hoverHandlers(menu.canCloseOthers)}
                        >
                            {text('Close Others', '关闭其他', '關閉其他')}
                        </button>
                    )}
                    {onCloseFilesToTheRight && (
                        <button
                            type="button"
                            data-testid="file-tab-ctx-close-right"
                            style={menu.canCloseToRight ? itemStyle : disabledItemStyle}
                            disabled={!menu.canCloseToRight}
                            onClick={menu.canCloseToRight ? handleCloseToRight : undefined}
                            {...hoverHandlers(menu.canCloseToRight)}
                        >
                            {text('Close to the Right', '关闭右侧', '關閉右側')}
                        </button>
                    )}
                    {onCloseAllFiles && (
                        <button
                            type="button"
                            data-testid="file-tab-ctx-close-all"
                            style={menu.canCloseAll ? itemStyle : disabledItemStyle}
                            disabled={!menu.canCloseAll}
                            onClick={menu.canCloseAll ? handleCloseAll : undefined}
                            {...hoverHandlers(menu.canCloseAll)}
                        >
                            {text('Close All', '全部关闭', '全部關閉')}
                        </button>
                    )}
                </>
            )}
        </div>
    );
}

// ── Single Tab ──

const FileTabButton = React.memo(function FileTabButton({
    filePath,
    file,
    isActive,
    isPinned,
    theme,
    onSelectFile,
    onCloseFile,
    onContextMenu,
    canDrag,
    dragOverSide,
    onDragStateChange,
    onDropOnTab,
}: {
    filePath: string;
    file: CodeFile;
    isActive: boolean;
    isPinned: boolean;
    theme: CodePreviewTheme;
    onSelectFile: (filePath: string) => void;
    onCloseFile?: (filePath: string) => void;
    onContextMenu: (e: React.MouseEvent, filePath: string, absPath: string | undefined) => void;
    canDrag: boolean;
    dragOverSide: 'before' | 'after' | null;
    onDragStateChange: (overPath: string | null, placeAfter: boolean) => void;
    onDropOnTab?: (fromPath: string, overPath: string, placeAfter: boolean) => void;
}) {
    const [hovered, setHovered] = useState(false);
    const fileName = extractFileName(filePath);
    const dirty = isCodeFileDirty(file);
    const deltaLabel = formatCodeFileLineDelta(computeCodeFileLineDelta(file));
    const tabTitle = deltaLabel
        ? `${file.absPath || filePath}  ${deltaLabel}`
        : (file.absPath || filePath);

    let backgroundColor = theme.tabBg;
    if (isActive) {
        backgroundColor = theme.tabActiveBg;
    } else if (hovered) {
        backgroundColor = theme.tabHoverBg;
    }

    return (
        <button
            type="button"
            role="tab"
            data-testid="file-tab"
            data-file-path={filePath}
            data-active={isActive ? 'true' : 'false'}
            data-pinned={isPinned ? 'true' : 'false'}
            data-dirty={dirty ? 'true' : 'false'}
            data-drag-over={dragOverSide || undefined}
            draggable={canDrag}
            title={tabTitle}
            aria-selected={isActive}
            onClick={() => onSelectFile(filePath)}
            onMouseDown={(e) => {
                // Middle-click closes the tab (VS Code).
                // Handle on mousedown so it works across browsers/jsdom reliably.
                if (e.button === 1 && onCloseFile) {
                    e.preventDefault();
                    e.stopPropagation();
                    onCloseFile(filePath);
                }
            }}
            onDragStart={(e) => {
                if (!canDrag) return;
                e.dataTransfer.effectAllowed = 'move';
                e.dataTransfer.setData('text/plain', filePath);
                e.dataTransfer.setData('application/x-file-tab-path', filePath);
            }}
            onDragOver={(e) => {
                if (!canDrag) return;
                e.preventDefault();
                e.dataTransfer.dropEffect = 'move';
                const rect = e.currentTarget.getBoundingClientRect();
                const placeAfter = e.clientX > rect.left + rect.width / 2;
                onDragStateChange(filePath, placeAfter);
            }}
            onDragLeave={(e) => {
                if (!canDrag) return;
                // Only clear when truly leaving this tab (not entering a child).
                if (!e.currentTarget.contains(e.relatedTarget as Node)) {
                    onDragStateChange(null, false);
                }
            }}
            onDrop={(e) => {
                if (!canDrag || !onDropOnTab) return;
                e.preventDefault();
                e.stopPropagation();
                const fromPath =
                    e.dataTransfer.getData('application/x-file-tab-path') ||
                    e.dataTransfer.getData('text/plain');
                const rect = e.currentTarget.getBoundingClientRect();
                const placeAfter = e.clientX > rect.left + rect.width / 2;
                onDragStateChange(null, false);
                if (!fromPath || fromPath === filePath) return;
                onDropOnTab(fromPath, filePath, placeAfter);
            }}
            onContextMenu={(e) => onContextMenu(e, filePath, file.absPath)}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
            style={{
                display: 'flex',
                alignItems: 'center',
                gap: 4,
                padding: '4px 8px 4px 12px',
                border: 'none',
                borderRight: `1px solid ${theme.border}`,
                borderLeft: dragOverSide === 'before' ? `2px solid ${theme.tabActiveText}` : '2px solid transparent',
                boxShadow: dragOverSide === 'after' ? `inset -2px 0 0 ${theme.tabActiveText}` : undefined,
                backgroundColor,
                color: isActive ? theme.tabActiveText : theme.text,
                cursor: canDrag ? 'grab' : 'pointer',
                whiteSpace: 'nowrap',
                fontSize: 13,
                fontFamily: 'inherit',
                lineHeight: '28px',
                flexShrink: 0,
                maxWidth: 180,
                minWidth: 0,
            }}
        >
            {isPinned && (
                <span
                    data-testid="file-tab-pin-marker"
                    aria-hidden
                    style={{
                        color: theme.textMuted,
                        fontSize: 10,
                        lineHeight: 1,
                        flexShrink: 0,
                        opacity: 0.9,
                    }}
                    title="Pinned"
                >
                    {/* Text pin mark — more reliable across fonts than emoji. */}
                    {'\u25B2'}
                </span>
            )}
            <FileOpOrDiffBadge file={file} theme={theme} testId="file-tab-diff-stat" />
            <span style={{
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                minWidth: 0,
            }}>{fileName}</span>
            {dirty && (
                <span
                    data-testid="file-tab-dirty"
                    aria-label="Changed"
                    title="Changed"
                    style={{
                        width: 7,
                        height: 7,
                        borderRadius: '50%',
                        background: isActive ? theme.tabActiveText : theme.textMuted,
                        flexShrink: 0,
                        marginLeft: 1,
                        opacity: 0.85,
                    }}
                />
            )}
            {onCloseFile && (
                <span
                    role="button"
                    data-testid="file-tab-close"
                    aria-label="Close file tab"
                    onClick={(e) => {
                        e.stopPropagation();
                        onCloseFile(filePath);
                    }}
                    style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 16,
                        height: 16,
                        marginLeft: 2,
                        borderRadius: 3,
                        fontSize: 14,
                        lineHeight: 1,
                        color: theme.textMuted,
                        flexShrink: 0,
                        // Hide × when dirty and not hovered/active — show dirty dot instead (VS Code-like).
                        opacity: isActive || hovered ? 1 : (dirty ? 0 : 0.55),
                    }}
                    title="Close"
                    onMouseEnter={(e) => { e.currentTarget.style.background = theme.border; }}
                    onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; }}
                >
                    {'\u00d7'}
                </span>
            )}
        </button>
    );
});

// ── Component ──

export function FileTabBar({
    files,
    activeFilePath,
    pinnedPaths = [],
    mruOrder = [],
    onSelectFile,
    onCloseFile,
    onCloseOtherFiles,
    onCloseFilesToTheRight,
    onCloseAllFiles,
    onMoveFile,
    onTogglePinFile,
    theme,
    lang,
}: FileTabBarProps) {
    const containerRef = useRef<HTMLDivElement>(null);
    /** Clipped strip only — capacity is measured here so the sticky +N slot is free. */
    const stripRef = useRef<HTMLDivElement>(null);
    /** Last measured fit ceiling; null = allow exploration up to width estimate. */
    const maxFittedRef = useRef<number | null>(null);
    const lastStripWidthRef = useRef(0);
    // Display order: pinned first, then remaining map open order (shared with state helpers).
    const filePaths = useMemo(
        () => getDisplayFilePaths(files, pinnedPaths),
        [files, pinnedPaths],
    );
    const pinnedSet = useMemo(() => new Set(pinnedPaths), [pinnedPaths]);
    // Stable keys so parent re-creating arrays does not thrash layout measurement.
    const pinnedKey = useMemo(() => pinnedPaths.join('\0'), [pinnedPaths]);
    // Identity of the open set (order + paths). Length-only is not enough: replacing
    // file A with B at the same count must clear maxFitted so short names can re-expand.
    const openFilesKey = useMemo(() => filePaths.join('\0'), [filePaths]);
    // Open Editors / Ctrl+Tab: shared MRU order (most recent first).
    const mruCycleOrder = useMemo(
        () => getMruCycleOrder(files, mruOrder),
        [files, mruOrder],
    );

    // Start with full count; layout effect + RO collapse immediately when narrow.
    // Using filePaths.length avoids a one-frame empty strip when only one tab.
    const [visibleCount, setVisibleCount] = useState(filePaths.length);
    const [dropdownOpen, setDropdownOpen] = useState(false);
    const [filterQuery, setFilterQuery] = useState('');
    const [highlightIndex, setHighlightIndex] = useState(0);
    const filterInputRef = useRef<HTMLInputElement>(null);
    const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
    const closeContextMenu = useCallback(() => setContextMenu(null), []);
    const [dragOver, setDragOver] = useState<{ path: string; placeAfter: boolean } | null>(null);

    // Reset measurement ceiling when the open set changes (composition or size).
    useLayoutEffect(() => {
        maxFittedRef.current = null;
        lastStripWidthRef.current = 0;
    }, [openFilesKey]);

    // Recalculate visible tab count from the clipped strip width (not the full bar).
    // The sticky open-editors control sits outside the strip, so its width is
    // already excluded by flex layout — no manual width reserve needed.
    useLayoutEffect(() => {
        const strip = stripRef.current;
        if (!strip) return;

        const applyWidth = (width: number) => {
            const estimate = computeMaxVisibleTabs(width, filePaths.length);
            if (estimate == null) return; // ignore 0 / NaN (jsdom, pre-layout)
            // Wider strip → clear ceiling so we can rediscover how many tabs fit.
            if (width > lastStripWidthRef.current + 1) {
                maxFittedRef.current = null;
            }
            lastStripWidthRef.current = width;
            const overflowing = strip.scrollWidth > strip.clientWidth + 1;
            setVisibleCount((prev) => {
                const next = reconcileVisibleCount(
                    prev,
                    estimate,
                    filePaths.length,
                    overflowing,
                    maxFittedRef.current,
                );
                return next === prev ? prev : next;
            });
        };

        if (typeof ResizeObserver === 'undefined') {
            const recalculate = () => applyWidth(strip.clientWidth || strip.getBoundingClientRect().width);
            recalculate();
            window.addEventListener('resize', recalculate);
            return () => window.removeEventListener('resize', recalculate);
        }

        const observer = new ResizeObserver((entries) => {
            for (const entry of entries) {
                applyWidth(resizeObserverEntryWidth(entry));
            }
        });
        observer.observe(strip);
        // Initial measure — some WebViews delay the first RO callback.
        applyWidth(strip.clientWidth || strip.getBoundingClientRect().width);
        return () => observer.disconnect();
    }, [openFilesKey, filePaths.length]);

    // Second pass: measure real tab widths. Shrink one-shot when overflowing;
    // when fitting, grow toward estimate without exceeding maxFitted (no loops).
    useLayoutEffect(() => {
        const strip = stripRef.current;
        if (!strip || filePaths.length === 0) return;
        const clientWidth = strip.clientWidth;
        if (clientWidth <= 0) return;

        const estimate = computeMaxVisibleTabs(clientWidth, filePaths.length);
        if (estimate == null) return;

        const overflowing = strip.scrollWidth > clientWidth + 1;
        const measuredFit = overflowing && strip.children.length > 0
            ? countFittingTabs(measureChildWidths(strip), clientWidth)
            : null;

        const result = nextVisibleCountFromLayout(
            visibleCount,
            filePaths.length,
            estimate,
            overflowing,
            measuredFit,
            maxFittedRef.current,
        );
        maxFittedRef.current = result.maxFitted;
        if (result.count !== visibleCount) {
            setVisibleCount(result.count);
        }
    }, [openFilesKey, filePaths.length, visibleCount, activeFilePath, pinnedKey]);

    const filteredEditors = useMemo(
        () => filterOpenFilePaths(mruCycleOrder, filterQuery),
        [mruCycleOrder, filterQuery],
    );

    // Keep highlight in range when filter results change.
    useEffect(() => {
        if (highlightIndex >= filteredEditors.length) {
            setHighlightIndex(Math.max(0, filteredEditors.length - 1));
        }
    }, [filteredEditors.length, highlightIndex]);

    const openEditorsPicker = useCallback(() => {
        setDropdownOpen(true);
        setFilterQuery('');
        setHighlightIndex(0);
        // Focus is handled by the dropdownOpen effect below.
    }, []);

    const closeEditorsPicker = useCallback(() => {
        setDropdownOpen(false);
        setFilterQuery('');
        setHighlightIndex(0);
    }, []);

    // Close dropdown on outside click.
    useEffect(() => {
        if (!dropdownOpen) return;
        const handler = (e: MouseEvent) => {
            if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
                closeEditorsPicker();
            }
        };
        document.addEventListener('mousedown', handler);
        return () => document.removeEventListener('mousedown', handler);
    }, [dropdownOpen, closeEditorsPicker]);

    // Auto-focus filter when picker opens.
    useEffect(() => {
        if (!dropdownOpen) return;
        const id = window.setTimeout(() => filterInputRef.current?.focus(), 0);
        return () => window.clearTimeout(id);
    }, [dropdownOpen]);

    const handleContextMenu = useCallback((e: React.MouseEvent, filePath: string, absPath: string | undefined) => {
        // Always allow context menu — at least copy-path actions are available.
        e.preventDefault();
        const index = filePaths.indexOf(filePath);
        // Mirror applyClose* pin protection: only enable actions that would close something.
        const canCloseOthers = filePaths.some((p) => p !== filePath && !pinnedSet.has(p));
        const canCloseToRight = index >= 0
            && filePaths.slice(index + 1).some((p) => !pinnedSet.has(p));
        const canCloseAll = filePaths.some((p) => !pinnedSet.has(p));
        setContextMenu({
            filePath,
            absPath: absPath || '',
            x: e.clientX,
            y: e.clientY,
            canCloseOthers,
            canCloseToRight,
            canCloseAll,
            isPinned: pinnedSet.has(filePath),
        });
    }, [filePaths, pinnedSet]);

    const handleEditorsActivate = useCallback((filePath: string) => {
        closeEditorsPicker();
        onSelectFile(filePath);
    }, [closeEditorsPicker, onSelectFile]);

    const handleEditorsClose = useCallback((filePath: string) => {
        onCloseFile?.(filePath);
    }, [onCloseFile]);

    const handleDragStateChange = useCallback((overPath: string | null, placeAfter: boolean) => {
        if (!overPath) {
            setDragOver((prev) => (prev == null ? prev : null));
            return;
        }
        // Avoid re-renders on every dragover pixel when side/path are unchanged.
        setDragOver((prev) => {
            if (prev && prev.path === overPath && prev.placeAfter === placeAfter) return prev;
            return { path: overPath, placeAfter };
        });
    }, []);

    const handleDropOnTab = useCallback((fromPath: string, overPath: string, placeAfter: boolean) => {
        if (!onMoveFile) return;
        const toIndex = computeDropIndex(filePaths, fromPath, overPath, placeAfter);
        if (toIndex < 0) return;
        onMoveFile(fromPath, toIndex);
        setDragOver(null);
    }, [filePaths, onMoveFile]);

    const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
        if (filePaths.length === 0) return;

        // Ctrl/Cmd+P or Ctrl/Cmd+Shift+E — open editors quick pick.
        if ((e.ctrlKey || e.metaKey) && !e.altKey) {
            if (e.key === 'p' || e.key === 'P' || ((e.key === 'e' || e.key === 'E') && e.shiftKey)) {
                if (filePaths.length >= 1) {
                    e.preventDefault();
                    e.stopPropagation();
                    if (dropdownOpen) closeEditorsPicker();
                    else openEditorsPicker();
                    return;
                }
            }
        }

        // Ctrl/Cmd+Tab / Ctrl/Cmd+Shift+Tab — MRU cycle (VS Code-style).
        if ((e.ctrlKey || e.metaKey) && e.key === 'Tab') {
            e.preventDefault();
            e.stopPropagation();
            const next = cycleFilePath(mruCycleOrder, activeFilePath, e.shiftKey ? -1 : 1);
            if (next) onSelectFile(next);
            return;
        }

        // Ctrl/Cmd+W — close active tab.
        if ((e.ctrlKey || e.metaKey) && (e.key === 'w' || e.key === 'W')) {
            if (!onCloseFile || !activeFilePath) return;
            e.preventDefault();
            e.stopPropagation();
            onCloseFile(activeFilePath);
            return;
        }

        // Spatial navigation along the tab bar (pinned-left display order).
        if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
            if (dropdownOpen) return; // list handles its own arrows
            e.preventDefault();
            const next = cycleFilePath(filePaths, activeFilePath, e.key === 'ArrowRight' ? 1 : -1);
            if (next) onSelectFile(next);
        }
    }, [activeFilePath, closeEditorsPicker, dropdownOpen, filePaths, mruCycleOrder, onCloseFile, onSelectFile, openEditorsPicker]);

    const handlePickerKeyDown = useCallback((e: React.KeyboardEvent) => {
        if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            closeEditorsPicker();
            containerRef.current?.focus();
            return;
        }
        const lastIdx = Math.max(0, filteredEditors.length - 1);
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            if (filteredEditors.length === 0) return;
            setHighlightIndex((i) => Math.min(lastIdx, Math.max(0, i) + 1));
            return;
        }
        if (e.key === 'ArrowUp') {
            e.preventDefault();
            if (filteredEditors.length === 0) return;
            setHighlightIndex((i) => Math.max(0, i - 1));
            return;
        }
        if (e.key === 'Enter') {
            e.preventDefault();
            if (filteredEditors.length === 0) return;
            const idx = Math.min(lastIdx, Math.max(0, highlightIndex));
            const path = filteredEditors[idx];
            if (path) handleEditorsActivate(path);
        }
    }, [closeEditorsPicker, filteredEditors, handleEditorsActivate, highlightIndex]);

    // Clamp so shrinking the file list cannot leave visibleCount > filePaths.length.
    const effectiveVisibleCount = filePaths.length === 0
        ? 0
        : Math.min(Math.max(1, visibleCount), filePaths.length);
    const hasOverflow = effectiveVisibleCount < filePaths.length;
    const visiblePaths = computeVisibleFilePaths(
        filePaths,
        activeFilePath,
        effectiveVisibleCount,
        pinnedPaths,
    );
    const overflowCount = Math.max(0, filePaths.length - visiblePaths.length);
    // Show open-editors control whenever there is more than one file, or overflow.
    const showEditorsButton = filePaths.length > 1 || hasOverflow;

    const isZh = (lang ?? 'en').startsWith('zh');
    const isZhHant = lang === 'zh-Hant';

    return (
        <>
            {contextMenu && (
                <FileTabContextMenu
                    menu={contextMenu}
                    theme={theme}
                    onClose={closeContextMenu}
                    onCloseFile={onCloseFile}
                    onCloseOtherFiles={onCloseOtherFiles}
                    onCloseFilesToTheRight={onCloseFilesToTheRight}
                    onCloseAllFiles={onCloseAllFiles}
                    onTogglePinFile={onTogglePinFile}
                    lang={lang}
                />
            )}
            <div
                ref={containerRef}
                onDragEnd={() => setDragOver((prev) => (prev == null ? prev : null))}
                data-testid="file-tab-bar"
                role="tablist"
                tabIndex={0}
                aria-label="Code preview file tabs"
                onKeyDown={handleKeyDown}
                style={{
                    display: 'flex',
                    // Outer bar stays overflow-visible so the open-editors dropdown can paint.
                    // Tabs live in a clipped strip; sticky +N control stays on the right.
                    overflowX: 'visible',
                    overflowY: 'visible',
                    backgroundColor: theme.tabBg,
                    // Header already draws the bottom edge; avoid a double hairline.
                    minHeight: 36,
                    alignItems: 'stretch',
                    position: 'relative',
                    minWidth: 0,
                    width: '100%',
                    outline: 'none',
                }}
            >
                {/* Clipped tab strip — flex child that absorbs remaining width; capacity is measured here. */}
                <div
                    ref={stripRef}
                    data-testid="file-tab-strip"
                    style={{
                        display: 'flex',
                        flex: 1,
                        minWidth: 0,
                        overflow: 'hidden',
                        alignItems: 'stretch',
                    }}
                >
                    {visiblePaths.map((filePath) => {
                        const file = files.get(filePath);
                        if (!file) return null;
                        const overSide =
                            dragOver?.path === filePath
                                ? (dragOver.placeAfter ? 'after' as const : 'before' as const)
                                : null;
                        return (
                            <FileTabButton
                                key={filePath}
                                filePath={filePath}
                                file={file}
                                isActive={filePath === activeFilePath}
                                isPinned={pinnedSet.has(filePath)}
                                theme={theme}
                                onSelectFile={onSelectFile}
                                onCloseFile={onCloseFile}
                                onContextMenu={handleContextMenu}
                                canDrag={Boolean(onMoveFile)}
                                dragOverSide={overSide}
                                onDragStateChange={handleDragStateChange}
                                onDropOnTab={onMoveFile ? handleDropOnTab : undefined}
                            />
                        );
                    })}
                </div>
                {/* Sticky right control: always visible when multi-file / overflow. */}
                {showEditorsButton && (
                    <button
                        type="button"
                        data-testid="file-tab-overflow-btn"
                        aria-haspopup="listbox"
                        aria-expanded={dropdownOpen}
                        onClick={() => {
                            if (dropdownOpen) closeEditorsPicker();
                            else openEditorsPicker();
                        }}
                        style={{
                            border: 'none',
                            borderLeft: `1px solid ${theme.border}`,
                            background: dropdownOpen ? theme.tabActiveBg : theme.tabBg,
                            color: theme.textMuted,
                            fontSize: 12,
                            padding: '4px 0',
                            cursor: 'pointer',
                            flexShrink: 0,
                            whiteSpace: 'nowrap',
                            fontFamily: 'inherit',
                            fontWeight: 700,
                            lineHeight: '28px',
                            // Fixed width so "+N" vs "⋯" never reflows the tab strip.
                            width: OVERFLOW_CONTROL_WIDTH,
                            minWidth: OVERFLOW_CONTROL_WIDTH,
                            textAlign: 'center',
                        }}
                        title={
                            lang === 'en'
                                ? hasOverflow
                                    ? `Open Editors — ${overflowCount} more (Ctrl+P)`
                                    : 'Open Editors (Ctrl+P)'
                                : isZhHant
                                    ? hasOverflow
                                        ? `已開啟編輯器 — 還有 ${overflowCount} 個 (Ctrl+P)`
                                        : '已開啟編輯器 (Ctrl+P)'
                                    : hasOverflow
                                        ? `已打开的编辑器 — 还有 ${overflowCount} 个 (Ctrl+P)`
                                        : '已打开的编辑器 (Ctrl+P)'
                        }
                        aria-label={
                            lang === 'en'
                                ? hasOverflow
                                    ? `Open editors, ${overflowCount} more`
                                    : 'Open editors'
                                : isZhHant
                                    ? hasOverflow
                                        ? `已開啟編輯器，還有 ${overflowCount} 個`
                                        : '已開啟編輯器'
                                    : hasOverflow
                                        ? `已打开的编辑器，还有 ${overflowCount} 个`
                                        : '已打开的编辑器'
                        }
                    >
                        {formatOverflowControlLabel(overflowCount, hasOverflow)}
                    </button>
                )}
                {dropdownOpen && (
                    <div
                        data-testid="file-tab-overflow-dropdown"
                        data-open-editors-picker="true"
                        style={{
                            position: 'absolute',
                            top: '100%',
                            right: 0,
                            zIndex: 9999,
                            background: theme.bg,
                            border: `1px solid ${theme.border}`,
                            borderRadius: 6,
                            boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
                            padding: '6px 0 4px',
                            minWidth: 260,
                            maxWidth: 420,
                            maxHeight: 360,
                            display: 'flex',
                            flexDirection: 'column',
                        }}
                        onKeyDown={handlePickerKeyDown}
                    >
                        <div style={{ padding: '0 8px 6px', borderBottom: `1px solid ${theme.border}` }}>
                            <input
                                ref={filterInputRef}
                                data-testid="file-tab-open-editors-filter"
                                type="text"
                                value={filterQuery}
                                onChange={(e) => {
                                    setFilterQuery(e.target.value);
                                    setHighlightIndex(0);
                                }}
                                placeholder={
                                    lang === 'en'
                                        ? 'Filter open editors…'
                                        : isZhHant
                                            ? '篩選已開啟檔案…'
                                            : '筛选已打开文件…'
                                }
                                style={{
                                    width: '100%',
                                    boxSizing: 'border-box',
                                    border: `1px solid ${theme.border}`,
                                    borderRadius: 4,
                                    padding: '5px 8px',
                                    fontSize: 12,
                                    background: theme.tabBg,
                                    color: theme.text,
                                    outline: 'none',
                                    fontFamily: 'inherit',
                                }}
                            />
                            <div style={{
                                marginTop: 4,
                                fontSize: 10,
                                color: theme.textMuted,
                                padding: '0 2px',
                            }}>
                                {lang === 'en'
                                    ? `${filteredEditors.length} of ${mruCycleOrder.length} open`
                                    : isZhHant
                                        ? `${filteredEditors.length} / ${mruCycleOrder.length} 個已開啟`
                                        : `${filteredEditors.length} / ${mruCycleOrder.length} 个已打开`}
                            </div>
                        </div>
                        <div style={{ overflowY: 'auto', maxHeight: 280, padding: '4px 0' }}>
                            {filteredEditors.length === 0 ? (
                                <div
                                    data-testid="file-tab-open-editors-empty"
                                    style={{
                                        padding: '12px 14px',
                                        fontSize: 12,
                                        color: theme.textMuted,
                                        textAlign: 'center',
                                    }}
                                >
                                    {lang === 'en' ? 'No matching files' : isZhHant ? '無相符檔案' : '无匹配文件'}
                                </div>
                            ) : (
                                filteredEditors.map((filePath, index) => {
                                    const file = files.get(filePath);
                                    if (!file) return null;
                                    const isActive = filePath === activeFilePath;
                                    const isHighlighted = index === highlightIndex;
                                    const fileName = extractFileName(filePath);
                                    const isPinned = pinnedSet.has(filePath);
                                    const dirty = isCodeFileDirty(file);
                                    return (
                                        <div
                                            key={filePath}
                                            data-testid="file-tab-overflow-item"
                                            data-file-path={filePath}
                                            data-highlighted={isHighlighted ? 'true' : 'false'}
                                            data-dirty={dirty ? 'true' : 'false'}
                                            style={{
                                                display: 'flex',
                                                alignItems: 'center',
                                                gap: 6,
                                                padding: '6px 12px',
                                                cursor: 'pointer',
                                                fontSize: 12,
                                                color: isActive ? theme.tabActiveText : theme.textMuted,
                                                fontWeight: isActive ? 600 : 400,
                                                background: isHighlighted ? theme.tabHoverBg : 'transparent',
                                            }}
                                            title={file.absPath || filePath}
                                            onClick={() => handleEditorsActivate(filePath)}
                                            onMouseEnter={() => setHighlightIndex(index)}
                                        >
                                            {isPinned && (
                                                <span style={{ fontSize: 10, flexShrink: 0, opacity: 0.9 }} aria-hidden>{'\u25B2'}</span>
                                            )}
                                            <FileOpOrDiffBadge file={file} theme={theme} testId="file-tab-overflow-diff-stat" />
                                            <span style={{
                                                flex: 1,
                                                minWidth: 0,
                                                display: 'flex',
                                                flexDirection: 'column',
                                                gap: 1,
                                            }}>
                                                <span style={{
                                                    overflow: 'hidden',
                                                    textOverflow: 'ellipsis',
                                                    whiteSpace: 'nowrap',
                                                }}>{fileName}{dirty ? ' \u2022' : ''}</span>
                                                <span style={{
                                                    overflow: 'hidden',
                                                    textOverflow: 'ellipsis',
                                                    whiteSpace: 'nowrap',
                                                    fontSize: 10,
                                                    color: theme.textMuted,
                                                    opacity: 0.85,
                                                }}>{file.absPath || filePath}</span>
                                            </span>
                                            {onCloseFile && (
                                                <span
                                                    role="button"
                                                    data-testid="file-tab-overflow-close"
                                                    onClick={(e) => {
                                                        e.stopPropagation();
                                                        handleEditorsClose(filePath);
                                                    }}
                                                    style={{
                                                        fontSize: 14,
                                                        color: theme.textMuted,
                                                        cursor: 'pointer',
                                                        flexShrink: 0,
                                                        padding: '0 2px',
                                                    }}
                                                    title={isZh ? '关闭' : 'Close'}
                                                >{'\u00d7'}</span>
                                            )}
                                        </div>
                                    );
                                })
                            )}
                        </div>
                    </div>
                )}
            </div>
        </>
    );
}
