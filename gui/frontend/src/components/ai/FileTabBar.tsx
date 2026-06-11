/**
 * FileTabBar — file name tab bar displayed at the top of the Code Preview Panel.
 *
 * Renders one tab per file in the files map. Each tab shows:
 *   - File name only as label (extracted from full path)
 *   - Full file path as tooltip (title attribute)
 *   - Visual indicator for opType: ✏️ (modify) or ➕ (create)
 *   - Active tab highlighted with distinct theme colors
 *
 * Supports horizontal scrolling via overflow-x: auto when tabs overflow.
 * Uses inline styles based on theme props (no CSS modules).
 */
import React, { useState } from 'react';
import type { CodeFile } from './useCodePreviewState';
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
 * @returns Indicator string: "✏️" for modify, "➕" for create, "👁" for read
 */
export function getOpTypeIndicator(opType: 'create' | 'modify' | 'read'): string {
    if (opType === 'modify') return '✏️';
    if (opType === 'read') return '👁';
    return '➕';
}

// ── Component Props ──

export interface FileTabBarProps {
    files: Map<string, CodeFile>;
    activeFilePath: string;
    onSelectFile: (filePath: string) => void;
    theme: CodePreviewTheme;
}

// ── Context Menu ──

interface ContextMenuState {
    filePath: string;
    absPath: string;
    x: number;
    y: number;
}

function FileTabContextMenu({ menu, theme, onClose }: { menu: ContextMenuState; theme: CodePreviewTheme; onClose: () => void }) {
    const handleRevealInExplorer = () => {
        onClose();
        void ShowItemInFolder(menu.absPath);
    };
    const handleOpenExternal = () => {
        onClose();
        void OpenSystemUrl(menu.absPath);
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

    return (
        <div
            data-file-tab-context-menu
            style={{
                position: 'fixed',
                left: Math.min(menu.x, window.innerWidth - 180),
                top: Math.min(menu.y, window.innerHeight - 80),
                background: theme.bg,
                border: `1px solid ${theme.border}`,
                borderRadius: 6,
                boxShadow: '0 4px 16px rgba(0,0,0,0.18)',
                zIndex: 99999,
                padding: '4px 0',
                minWidth: 160,
            }}
        >
            <button type="button" style={itemStyle} onClick={handleRevealInExplorer}
                onMouseEnter={e => { e.currentTarget.style.background = theme.tabHoverBg; }}
                onMouseLeave={e => { e.currentTarget.style.background = 'none'; }}>
                📂 在资源管理器中显示
            </button>
            <button type="button" style={itemStyle} onClick={handleOpenExternal}
                onMouseEnter={e => { e.currentTarget.style.background = theme.tabHoverBg; }}
                onMouseLeave={e => { e.currentTarget.style.background = 'none'; }}>
                🔗 用其他工具打开
            </button>
        </div>
    );
}

// ── Component ──

export function FileTabBar({ files, activeFilePath, onSelectFile, theme }: FileTabBarProps) {
    const [hoveredPath, setHoveredPath] = useState<string | null>(null);
    const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);

    const handleContextMenu = (e: React.MouseEvent, filePath: string, absPath: string | undefined) => {
        if (!absPath) return; // no absolute path available, skip context menu
        e.preventDefault();
        setContextMenu({ filePath, absPath, x: e.clientX, y: e.clientY });
    };

    return (
        <>
        {contextMenu && <FileTabContextMenu menu={contextMenu} theme={theme} onClose={() => setContextMenu(null)} />}
        <div
            style={{
                display: 'flex',
                overflowX: 'auto',
                backgroundColor: theme.tabBg,
                borderBottom: `1px solid ${theme.border}`,
                minHeight: 36,
                alignItems: 'stretch',
            }}
        >
            {Array.from(files.entries()).map(([filePath, file]) => {
                const isActive = filePath === activeFilePath;
                const isHovered = filePath === hoveredPath;
                const indicator = getOpTypeIndicator(file.opType);
                const fileName = extractFileName(filePath);

                let backgroundColor = theme.tabBg;
                if (isActive) {
                    backgroundColor = theme.tabActiveBg;
                } else if (isHovered) {
                    backgroundColor = theme.tabHoverBg;
                }

                return (
                    <button
                        key={filePath}
                        title={file.absPath || filePath}
                        onClick={() => onSelectFile(filePath)}
                        onContextMenu={(e) => handleContextMenu(e, filePath, file.absPath)}
                        onMouseEnter={() => setHoveredPath(filePath)}
                        onMouseLeave={() => setHoveredPath(null)}
                        style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 4,
                            padding: '4px 12px',
                            border: 'none',
                            borderRight: `1px solid ${theme.border}`,
                            backgroundColor,
                            color: isActive ? theme.tabActiveText : theme.text,
                            cursor: 'pointer',
                            whiteSpace: 'nowrap',
                            fontSize: 13,
                            fontFamily: 'inherit',
                            lineHeight: '28px',
                        }}
                    >
                        <span style={{ fontSize: 12 }}>{indicator}</span>
                        <span>{fileName}</span>
                    </button>
                );
            })}
        </div>
        </>
    );
}
