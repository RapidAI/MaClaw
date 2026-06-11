/**
 * CodePreviewPanel — main component for the code preview panel.
 *
 * Renders a panel with:
 *   1. A header bar with a close button (×)
 *   2. FileTabBar at the top for file selection
 *   3. Code content area with line numbers, monospace font, vertical scrolling
 *   4. Syntax highlighting via tokenizeLine
 *   5. DiffView for files with original content (using computeDiff)
 *   6. Scroll position preservation on content update
 *
 * Uses inline styles based on theme props (no CSS modules).
 */
import React, { useLayoutEffect, useMemo, useRef } from 'react';
import { FileTabBar } from './FileTabBar';
import type { CodePreviewTheme } from './FileTabBar';
import type { CodeFile } from './useCodePreviewState';
import { computeDiff } from './diffCompute';
import type { DiffLine } from './diffCompute';
import { tokenizeLine } from './syntaxHighlight';
import type { HighlightToken } from './syntaxHighlight';

// ── Re-export theme type for convenience ──
export type { CodePreviewTheme } from './FileTabBar';

// ── Theme Constants ──

/** Dark theme for the code preview panel. */
export const darkCodePreviewTheme: CodePreviewTheme = {
    bg: '#1e1e1e',
    text: '#d4d4d4',
    textMuted: '#808080',
    border: '#333333',
    lineNumBg: '#1e1e1e',
    lineNumText: '#858585',
    tabBg: '#252526',
    tabActiveBg: '#1e1e1e',
    tabActiveText: '#ffffff',
    tabHoverBg: '#2a2d2e',
    diffAddBg: 'rgba(35, 134, 54, 0.2)',
    diffAddText: '#b5e8b5',
    diffDeleteBg: 'rgba(218, 54, 51, 0.2)',
    diffDeleteText: '#f0a8a8',
    syntaxKeyword: '#569cd6',
    syntaxString: '#ce9178',
    syntaxComment: '#6a9955',
    syntaxNumber: '#b5cea8',
    syntaxFunction: '#dcdcaa',
    syntaxType: '#4ec9b0',
    syntaxOperator: '#d4d4d4',
};

/** Light theme for the code preview panel. */
export const lightCodePreviewTheme: CodePreviewTheme = {
    bg: '#ffffff',
    text: '#1f1f1f',
    textMuted: '#6e7681',
    border: '#d0d7de',
    lineNumBg: '#f6f8fa',
    lineNumText: '#8b949e',
    tabBg: '#f6f8fa',
    tabActiveBg: '#ffffff',
    tabActiveText: '#1f1f1f',
    tabHoverBg: '#eaeef2',
    diffAddBg: 'rgba(46, 160, 67, 0.15)',
    diffAddText: '#1a7f37',
    diffDeleteBg: 'rgba(248, 81, 73, 0.15)',
    diffDeleteText: '#cf222e',
    syntaxKeyword: '#0550ae',
    syntaxString: '#0a3069',
    syntaxComment: '#6e7781',
    syntaxNumber: '#0550ae',
    syntaxFunction: '#8250df',
    syntaxType: '#0550ae',
    syntaxOperator: '#1f1f1f',
};

// ── Props ──

export interface CodePreviewPanelProps {
    files: Map<string, CodeFile>;
    activeFilePath: string;
    onSelectFile: (filePath: string) => void;
    onClose: () => void;
    onResizeStart?: () => void;
    theme: CodePreviewTheme;
}

// ── Syntax color mapping ──

function tokenColor(type: HighlightToken['type'], theme: CodePreviewTheme): string {
    switch (type) {
        case 'keyword': return theme.syntaxKeyword;
        case 'string': return theme.syntaxString;
        case 'comment': return theme.syntaxComment;
        case 'number': return theme.syntaxNumber;
        case 'function': return theme.syntaxFunction;
        case 'type': return theme.syntaxType;
        case 'operator': return theme.syntaxOperator;
        case 'plain':
        default:
            return theme.text;
    }
}

// ── Highlighted Line Renderer ──

function HighlightedLine({ line, language, theme }: {
    line: string;
    language: string;
    theme: CodePreviewTheme;
}) {
    const tokens = tokenizeLine(line, language);
    if (tokens.length === 0) {
        return <span>{'\u00a0'}</span>;
    }
    return (
        <>
            {tokens.map((tok, i) => (
                <span key={i} style={{ color: tokenColor(tok.type, theme) }}>
                    {tok.text}
                </span>
            ))}
        </>
    );
}

// ── Plain Code View ──

function PlainCodeView({ content, language, theme }: {
    content: string;
    language: string;
    theme: CodePreviewTheme;
}) {
    const lines = content.split('\n');

    return (
        <table style={{
            borderCollapse: 'collapse',
            width: '100%',
            fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace",
            fontSize: 13,
            lineHeight: '20px',
        }}>
            <tbody>
                {lines.map((line, idx) => (
                    <tr key={idx}>
                        <td style={{
                            width: 50,
                            minWidth: 50,
                            textAlign: 'right',
                            paddingRight: 12,
                            paddingLeft: 8,
                            color: theme.lineNumText,
                            backgroundColor: theme.lineNumBg,
                            userSelect: 'none',
                            verticalAlign: 'top',
                            borderRight: `1px solid ${theme.border}`,
                        }}>
                            {idx + 1}
                        </td>
                        <td style={{
                            paddingLeft: 12,
                            paddingRight: 8,
                            whiteSpace: 'pre',
                            color: theme.text,
                            textAlign: 'left',
                        }}>
                            <HighlightedLine line={line} language={language} theme={theme} />
                        </td>
                    </tr>
                ))}
            </tbody>
        </table>
    );
}

// ── Diff View ──

function DiffView({ diffLines, theme }: {
    diffLines: DiffLine[];
    theme: CodePreviewTheme;
}) {
    return (
        <table style={{
            borderCollapse: 'collapse',
            width: '100%',
            fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace",
            fontSize: 13,
            lineHeight: '20px',
        }}>
            <tbody>
                {diffLines.map((dl, idx) => {
                    let rowBg: string | undefined;
                    let rowColor: string = theme.text;
                    let prefix = ' ';

                    if (dl.type === 'add') {
                        rowBg = theme.diffAddBg;
                        rowColor = theme.diffAddText;
                        prefix = '+';
                    } else if (dl.type === 'delete') {
                        rowBg = theme.diffDeleteBg;
                        rowColor = theme.diffDeleteText;
                        prefix = '-';
                    }

                    return (
                        <tr key={idx} style={{ backgroundColor: rowBg }}>
                            {/* Old line number */}
                            <td style={{
                                width: 40,
                                minWidth: 40,
                                textAlign: 'right',
                                paddingRight: 6,
                                paddingLeft: 8,
                                color: theme.lineNumText,
                                backgroundColor: rowBg ?? theme.lineNumBg,
                                userSelect: 'none',
                                verticalAlign: 'top',
                            }}>
                                {dl.oldLineNum ?? ''}
                            </td>
                            {/* New line number */}
                            <td style={{
                                width: 40,
                                minWidth: 40,
                                textAlign: 'right',
                                paddingRight: 6,
                                color: theme.lineNumText,
                                backgroundColor: rowBg ?? theme.lineNumBg,
                                userSelect: 'none',
                                verticalAlign: 'top',
                                borderRight: `1px solid ${theme.border}`,
                            }}>
                                {dl.newLineNum ?? ''}
                            </td>
                            {/* Prefix indicator */}
                            <td style={{
                                width: 20,
                                minWidth: 20,
                                textAlign: 'center',
                                color: rowColor,
                                userSelect: 'none',
                                verticalAlign: 'top',
                                fontWeight: dl.type !== 'unchanged' ? 600 : 400,
                            }}>
                                {dl.type !== 'unchanged' ? prefix : ''}
                            </td>
                            {/* Content */}
                            <td style={{
                                paddingLeft: 4,
                                paddingRight: 8,
                                whiteSpace: 'pre',
                                color: rowColor,
                                textAlign: 'left',
                            }}>
                                {dl.content}
                            </td>
                        </tr>
                    );
                })}
            </tbody>
        </table>
    );
}

// ── Main Component ──

export function CodePreviewPanel({
    files,
    activeFilePath,
    onSelectFile,
    onClose,
    onResizeStart,
    theme,
}: CodePreviewPanelProps) {
    const scrollRef = useRef<HTMLDivElement>(null);
    const savedScrollTop = useRef<number>(0);
    const prevContentRef = useRef<string>('');

    const activeFile = files.get(activeFilePath);

    // Compute diff lines when active file has original content
    const diffLines = useMemo<DiffLine[] | null>(() => {
        if (!activeFile?.original) return null;
        return computeDiff(activeFile.original, activeFile.content);
    }, [activeFile?.original, activeFile?.content]);

    const currentContent = activeFile?.content ?? '';

    // Save scroll position before DOM update, restore after re-render
    useLayoutEffect(() => {
        const el = scrollRef.current;
        if (!el) return;
        if (currentContent !== prevContentRef.current) {
            // Content changed — restore the saved position
            el.scrollTop = savedScrollTop.current;
            prevContentRef.current = currentContent;
        }
        // Save current scroll position for next update
        return () => {
            if (scrollRef.current) {
                savedScrollTop.current = scrollRef.current.scrollTop;
            }
        };
    }, [currentContent]);

    // Empty state: no files
    if (files.size === 0) {
        return (
            <div style={{
                display: 'flex',
                flexDirection: 'row',
                height: '100%',
                minWidth: 0,
            }}>
                {/* Drag handle for resizing */}
                <div
                    onMouseDown={(e) => { e.preventDefault(); onResizeStart?.(); }}
                    style={{
                        width: 6,
                        cursor: 'col-resize',
                        background: theme.border,
                        flexShrink: 0,
                        transition: 'background 0.15s',
                    }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.tabActiveText; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = theme.border; }}
                />
                <div style={{
                    display: 'flex',
                    flexDirection: 'column',
                    flex: 1,
                    minWidth: 0,
                    height: '100%',
                    background: theme.bg,
                    color: theme.text,
                }}>
                    {/* Header */}
                    <div
                        data-testid="code-preview-header"
                        style={{
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'flex-end',
                            padding: '8px 14px',
                            borderBottom: `1px solid ${theme.border}`,
                            background: theme.tabBg,
                            flexShrink: 0,
                            '--wails-draggable': 'no-drag',
                        } as any}
                    >
                        <button
                            onClick={onClose}
                            style={{
                                background: 'none',
                                border: 'none',
                                cursor: 'pointer',
                                fontSize: 16,
                                padding: '2px 6px',
                                borderRadius: 4,
                                color: theme.textMuted,
                                lineHeight: 1,
                                '--wails-draggable': 'no-drag',
                            } as any}
                            title="关闭代码预览"
                        >
                            ×
                        </button>
                    </div>
                    <div style={{
                        flex: 1,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        color: theme.textMuted,
                        fontSize: 14,
                    }}>
                        暂无代码文件
                    </div>
                </div>
            </div>
        );
    }

    return (
        <div style={{
            display: 'flex',
            flexDirection: 'row',
            height: '100%',
            minWidth: 0,
        }}>
            {/* Drag handle for resizing */}
            <div
                onMouseDown={(e) => { e.preventDefault(); onResizeStart?.(); }}
                style={{
                    width: 6,
                    cursor: 'col-resize',
                    background: theme.border,
                    flexShrink: 0,
                    transition: 'background 0.15s',
                }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.tabActiveText; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = theme.border; }}
            />
            <div style={{
                display: 'flex',
                flexDirection: 'column',
                flex: 1,
                minWidth: 0,
                height: '100%',
                background: theme.bg,
                color: theme.text,
            }}>
            {/* Header with close button — double-click to toggle maximize */}
            <div
                data-testid="code-preview-header"
                style={{
                    display: 'flex',
                    alignItems: 'center',
                    padding: '4px 8px 4px 0',
                    borderBottom: `1px solid ${theme.border}`,
                    background: theme.tabBg,
                    flexShrink: 0,
                    '--wails-draggable': 'no-drag',
                } as any}
            >
                <div data-preview-no-maximize="true" style={{ flex: 1, minWidth: 0, '--wails-draggable': 'no-drag' } as any}>
                    <FileTabBar
                        files={files}
                        activeFilePath={activeFilePath}
                        onSelectFile={onSelectFile}
                        theme={theme}
                    />
                </div>
                <button
                    onClick={onClose}
                    style={{
                        background: 'none',
                        border: 'none',
                        cursor: 'pointer',
                        fontSize: 16,
                        padding: '2px 6px',
                        borderRadius: 4,
                        color: theme.textMuted,
                        lineHeight: 1,
                        flexShrink: 0,
                        marginLeft: 4,
                        '--wails-draggable': 'no-drag',
                    } as any}
                    title="关闭代码预览"
                >
                    ×
                </button>
            </div>

            {/* Code content area */}
            <div
                ref={scrollRef}
                style={{
                    flex: 1,
                    overflowY: 'auto',
                    overflowX: 'auto',
                    minHeight: 0,
                }}
            >
                {activeFile ? (
                    diffLines ? (
                        <DiffView diffLines={diffLines} theme={theme} />
                    ) : (
                        <PlainCodeView
                            content={activeFile.content}
                            language={activeFile.language}
                            theme={theme}
                        />
                    )
                ) : (
                    <div style={{
                        padding: 20,
                        color: theme.textMuted,
                        fontSize: 14,
                        textAlign: 'center',
                    }}>
                        文件未找到
                    </div>
                )}
            </div>
            </div>
        </div>
    );
}
