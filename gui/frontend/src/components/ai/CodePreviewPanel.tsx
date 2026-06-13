/**
 * CodePreviewPanel - main component for the code preview panel.
 *
 * Renders a panel with:
 *   1. A header bar with a close button (X)
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
    bg: '#0f1720',
    text: '#d7dee8',
    textMuted: '#8d9aae',
    border: '#263447',
    lineNumBg: '#111b27',
    lineNumText: '#6f7d90',
    tabBg: '#111b27',
    tabActiveBg: '#162233',
    tabActiveText: '#edf3f9',
    tabHoverBg: '#1a293b',
    diffAddBg: 'rgba(122, 168, 154, 0.16)',
    diffAddText: '#b8d7cf',
    diffDeleteBg: 'rgba(196, 61, 52, 0.12)',
    diffDeleteText: '#e07a72',
    syntaxKeyword: '#9bc2ea',
    syntaxString: '#b8d7cf',
    syntaxComment: '#7d8c9e',
    syntaxNumber: '#b7d3ef',
    syntaxFunction: '#d7dee8',
    syntaxType: '#b7d3ef',
    syntaxOperator: '#c3ccd8',
};

/** Light theme for the code preview panel. */
export const lightCodePreviewTheme: CodePreviewTheme = {
    bg: '#ffffff',
    text: '#1f2937',
    textMuted: '#64748b',
    border: '#d8dee8',
    lineNumBg: '#f8fafc',
    lineNumText: '#94a3b8',
    tabBg: '#f8fafc',
    tabActiveBg: '#ffffff',
    tabActiveText: '#111827',
    tabHoverBg: '#eef2f7',
    diffAddBg: 'rgba(79, 127, 111, 0.12)',
    diffAddText: '#4f7f6f',
    diffDeleteBg: 'rgba(196, 61, 52, 0.10)',
    diffDeleteText: '#c43d34',
    syntaxKeyword: '#2f5f98',
    syntaxString: '#4f7f6f',
    syntaxComment: '#64748b',
    syntaxNumber: '#2f5f98',
    syntaxFunction: '#334155',
    syntaxType: '#2f5f98',
    syntaxOperator: '#334155',
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

// ── Markdown Preview ──

/** Detect a pipe-delimited table row */
function isMdPreviewTableRow(line: string): boolean {
    const trimmed = line.trim();
    if (trimmed.startsWith('|') && trimmed.length > 1) return true;
    if (!trimmed.includes('|')) return false;
    // At least 2 cells when split by unescaped pipes
    return parseMdPreviewTableCells(trimmed).length >= 2;
}

/** Detect a separator row like |---|---| or |:---:|---:| */
function isMdPreviewSeparatorRow(line: string): boolean {
    const trimmed = line.trim().replace(/^\|/, '').replace(/\|$/, '');
    return /^[\s|:\-]+$/.test(trimmed) && trimmed.includes('-');
}

/** Parse cells from a pipe-delimited row */
function parseMdPreviewTableCells(line: string): string[] {
    let trimmed = line.trim();
    if (trimmed.startsWith('|')) trimmed = trimmed.slice(1);
    if (trimmed.endsWith('|')) trimmed = trimmed.slice(0, -1);
    return trimmed.split('|').map(c => c.trim());
}

/** Render a markdown table as an HTML <table> */
function renderMdPreviewTable(tableLines: string[], key: number, theme: CodePreviewTheme): React.ReactNode | null {
    const dataRows = tableLines.filter(l => !isMdPreviewSeparatorRow(l));
    if (dataRows.length === 0 || tableLines.length < 2) return null;
    // Need a separator or all rows must start with |
    const hasSeparator = tableLines.some(isMdPreviewSeparatorRow);
    const allOuterPipes = tableLines.every(l => l.trim().startsWith('|'));
    if (!hasSeparator && !allOuterPipes) return null;

    const headerCells = parseMdPreviewTableCells(dataRows[0]);
    if (headerCells.length < 2) return null;
    const bodyRows = dataRows.slice(1);

    // Parse column alignments from separator row
    const separatorLine = tableLines.find(isMdPreviewSeparatorRow) || '';
    const alignments = parseMdPreviewTableCells(separatorLine).map(cell => {
        const m = cell.trim();
        const left = m.startsWith(':');
        const right = m.endsWith(':');
        if (left && right) return 'center' as const;
        if (right) return 'right' as const;
        return 'left' as const;
    });

    const cellStyle: React.CSSProperties = {
        border: `1px solid ${theme.border}`,
        padding: '6px 10px',
        fontSize: 13,
        lineHeight: 1.5,
        verticalAlign: 'top',
    };

    return (
        <div key={key} style={{ overflowX: 'auto', margin: '8px 0' }}>
            <table style={{ borderCollapse: 'collapse', width: '100%', color: theme.text }}>
                <thead>
                    <tr>
                        {headerCells.map((cell, ci) => (
                            <th key={ci} style={{ ...cellStyle, textAlign: alignments[ci] || 'left', fontWeight: 600, background: theme.lineNumBg }}>
                                {renderMdInline(cell, theme)}
                            </th>
                        ))}
                    </tr>
                </thead>
                {bodyRows.length > 0 && (
                    <tbody>
                        {bodyRows.map((row, ri) => {
                            const cells = parseMdPreviewTableCells(row);
                            return (
                                <tr key={ri} style={{ background: ri % 2 === 1 ? theme.lineNumBg : undefined }}>
                                    {headerCells.map((_, ci) => (
                                        <td key={ci} style={{ ...cellStyle, textAlign: alignments[ci] || 'left' }}>
                                            {renderMdInline(cells[ci] || '', theme)}
                                        </td>
                                    ))}
                                </tr>
                            );
                        })}
                    </tbody>
                )}
            </table>
        </div>
    );
}

function MarkdownPreview({ content, theme }: { content: string; theme: CodePreviewTheme }) {
    const lines = content.replace(/\r\n/g, '\n').replace(/\r/g, '\n').split('\n');
    const elements: React.ReactNode[] = [];
    let i = 0;
    let tableLines: string[] = [];

    const flushTable = () => {
        if (tableLines.length === 0) return;
        const rendered = renderMdPreviewTable(tableLines, elements.length, theme);
        if (rendered) {
            elements.push(rendered);
        } else {
            for (const tl of tableLines) {
                elements.push(<p key={elements.length} style={{ margin: '4px 0', lineHeight: 1.6 }}>{renderMdInline(tl, theme)}</p>);
            }
        }
        tableLines = [];
    };

    while (i < lines.length) {
        const line = lines[i];

        // Code blocks - detect ``` or ~~~ fences (with optional leading whitespace)
        const fenceMatch = line.match(/^(\s*)(```|~~~)/);
        if (fenceMatch) {
            flushTable();
            const fence = fenceMatch[2];
            const lang = line.slice(fenceMatch[0].length).trim();
            const codeLines: string[] = [];
            i++;
            while (i < lines.length) {
                if (lines[i].trimStart().startsWith(fence) && lines[i].trim() === fence) {
                    break;
                }
                codeLines.push(lines[i]);
                i++;
            }
            if (i < lines.length) i++; // skip closing fence (only if found)
            elements.push(
                <pre key={elements.length} style={{ background: theme.lineNumBg, border: `1px solid ${theme.border}`, borderRadius: 6, padding: '10px 14px', margin: '8px 0', overflow: 'auto', fontSize: 13, lineHeight: 1.5 }}>
                    <code style={{ color: theme.text, fontFamily: "'Cascadia Code', 'Consolas', monospace" }}>
                        {lang && <span style={{ color: theme.textMuted, fontSize: 11, display: 'block', marginBottom: 4 }}>{lang}</span>}
                        {codeLines.join('\n')}
                    </code>
                </pre>
            );
            continue;
        }

        // Table rows: collect consecutive pipe-delimited lines
        if (isMdPreviewTableRow(line)) {
            tableLines.push(line);
            i++;
            continue;
        }

        // Non-table line - flush any pending table
        flushTable();

        // Headings (# through ######)
        const headingMatch = line.match(/^(#{1,6})\s+(.+)/);
        if (headingMatch) {
            const level = headingMatch[1].length;
            const sizes: Record<number, number> = { 1: 22, 2: 18, 3: 15, 4: 14, 5: 13, 6: 12 };
            const margins: Record<number, string> = { 1: '16px 0 8px', 2: '14px 0 6px', 3: '12px 0 4px', 4: '10px 0 4px', 5: '8px 0 3px', 6: '8px 0 3px' };
            elements.push(
                <div key={elements.length} style={{ fontSize: sizes[level], fontWeight: level <= 2 ? 700 : 600, margin: margins[level], color: theme.tabActiveText, borderBottom: level <= 2 ? `1px solid ${theme.border}` : undefined, paddingBottom: level <= 2 ? 4 : undefined }}>
                    {renderMdInline(headingMatch[2], theme)}
                </div>
            );
            i++;
            continue;
        }

        // Multi-line blockquote - collect consecutive > lines
        if (line.startsWith('>') || line.startsWith('> ')) {
            const quoteLines: string[] = [];
            while (i < lines.length && (lines[i].startsWith('> ') || lines[i] === '>' || lines[i].startsWith('>'))) {
                quoteLines.push(lines[i].replace(/^>\s?/, ''));
                i++;
            }
            elements.push(
                <blockquote key={elements.length} style={{ borderLeft: `1px solid ${theme.border}`, paddingLeft: 12, margin: '6px 0', color: theme.textMuted, fontStyle: 'italic', lineHeight: 1.6 }}>
                    {quoteLines.map((ql, qi) => (
                        <div key={qi}>{ql.trim() === '' ? <br /> : renderMdInline(ql, theme)}</div>
                    ))}
                </blockquote>
            );
            continue;
        }

        // Task list: - [ ] or - [x] or * [ ] or * [x] - collect consecutive items
        const taskMatch = line.match(/^\s*[-*]\s+\[([ xX])\]\s+(.*)/);
        if (taskMatch) {
            const taskItems: { checked: boolean; text: string }[] = [];
            while (i < lines.length) {
                const tm = lines[i].match(/^\s*[-*]\s+\[([ xX])\]\s+(.*)/);
                if (!tm) break;
                taskItems.push({ checked: tm[1].toLowerCase() === 'x', text: tm[2] });
                i++;
            }
            elements.push(
                <div key={elements.length} style={{ margin: '4px 0', paddingLeft: 4 }}>
                    {taskItems.map((item, ti) => (
                        <div key={ti} style={{ paddingLeft: 12, margin: '2px 0', display: 'flex', alignItems: 'flex-start', gap: 6 }}>
                            <span style={{ flexShrink: 0, fontSize: 11, fontWeight: 700, color: theme.textMuted }}>{item.checked ? "DONE" : "TODO"}</span>
                            <span style={{ textDecoration: item.checked ? 'line-through' : undefined, opacity: item.checked ? 0.7 : 1 }}>{renderMdInline(item.text, theme)}</span>
                        </div>
                    ))}
                </div>
            );
            continue;
        }

        // Unordered list - collect consecutive items (supports indentation for nesting)
        if (/^\s*[-*+]\s/.test(line)) {
            const listItems: { indent: number; text: string }[] = [];
            while (i < lines.length && /^\s*[-*+]\s/.test(lines[i])) {
                const m = lines[i].match(/^(\s*)[-*+]\s+(.*)/);
                if (m) {
                    listItems.push({ indent: m[1].length, text: m[2] });
                }
                i++;
            }
            const baseIndent = Math.min(...listItems.map(it => it.indent));
            elements.push(
                <ul key={elements.length} style={{ margin: '4px 0', paddingLeft: 20, listStyleType: 'disc' }}>
                    {listItems.map((item, li) => (
                        <li key={li} style={{ marginLeft: (item.indent - baseIndent) * 10, marginBottom: 2 }}>
                            {renderMdInline(item.text, theme)}
                        </li>
                    ))}
                </ul>
            );
            continue;
        }

        // Ordered list - collect consecutive numbered items
        if (/^\s*\d+[.)]\s/.test(line)) {
            const olItems: { indent: number; num: string; text: string }[] = [];
            while (i < lines.length && /^\s*\d+[.)]\s/.test(lines[i])) {
                const m = lines[i].match(/^(\s*)(\d+)[.)]\s+(.*)/);
                if (m) {
                    olItems.push({ indent: m[1].length, num: m[2], text: m[3] });
                }
                i++;
            }
            const baseIndent = Math.min(...olItems.map(it => it.indent));
            elements.push(
                <ol key={elements.length} style={{ margin: '4px 0', paddingLeft: 20 }}>
                    {olItems.map((item, li) => (
                        <li key={li} value={parseInt(item.num, 10)} style={{ marginLeft: (item.indent - baseIndent) * 10, marginBottom: 2 }}>
                            {renderMdInline(item.text, theme)}
                        </li>
                    ))}
                </ol>
            );
            continue;
        }

        // Horizontal rule
        if (/^[-*_]{3,}\s*$/.test(line.trim()) && !line.trim().startsWith('|')) {
            elements.push(<hr key={elements.length} style={{ border: 'none', borderTop: `1px solid ${theme.border}`, margin: '12px 0' }} />);
            i++;
            continue;
        }

        // Image on its own line: ![alt](url)
        const imgMatch = line.trim().match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
        if (imgMatch) {
            elements.push(
                <div key={elements.length} style={{ margin: '8px 0', textAlign: 'center' }}>
                    <img src={imgMatch[2]} alt={imgMatch[1]} style={{ maxWidth: '100%', borderRadius: 4, border: `1px solid ${theme.border}` }} />
                    {imgMatch[1] && <div style={{ fontSize: 12, color: theme.textMuted, marginTop: 4 }}>{imgMatch[1]}</div>}
                </div>
            );
            i++;
            continue;
        }

        // Definition list: term followed by : definition
        if (i + 1 < lines.length && line.trim() !== '' && /^\s*:\s+/.test(lines[i + 1])) {
            const term = line.trim();
            const defs: string[] = [];
            i++;
            while (i < lines.length && /^\s*:\s+/.test(lines[i])) {
                defs.push(lines[i].replace(/^\s*:\s+/, ''));
                i++;
            }
            elements.push(
                <dl key={elements.length} style={{ margin: '6px 0' }}>
                    <dt style={{ fontWeight: 600 }}>{renderMdInline(term, theme)}</dt>
                    {defs.map((d, di) => (
                        <dd key={di} style={{ marginLeft: 20, margin: '2px 0 2px 20px', color: theme.text }}>{renderMdInline(d, theme)}</dd>
                    ))}
                </dl>
            );
            continue;
        }

        // Empty line
        if (line.trim() === '') {
            elements.push(<div key={elements.length} style={{ height: 8 }} />);
            i++;
            continue;
        }

        // Default: paragraph
        elements.push(<p key={elements.length} style={{ margin: '4px 0', lineHeight: 1.6 }}>{renderMdInline(line, theme)}</p>);
        i++;
    }
    flushTable();
    return (
        <div style={{ padding: '16px 20px', fontSize: 14, lineHeight: 1.6, color: theme.text, fontFamily: 'inherit', wordBreak: 'break-word' }}>
            {elements}
        </div>
    );
}

function renderMdInline(text: string, theme: CodePreviewTheme): React.ReactNode {
    const parts: React.ReactNode[] = [];
    // Order matters: longer/more specific patterns first.
    // Patterns: inline code, bold+italic (***), bold (**), strikethrough (~~),
    //           highlight (==), image (![]()), link ([]()), italic (*)
    // NOTE: italic(*) requires non-space after opening * and before closing * to avoid
    //       matching multiplication like "2 * 3 * 4".
    //       Underscore italic (_) is NOT supported to avoid false positives in identifiers.
    const re = /(`[^`]+`|\*\*\*(?!\s)(?:[^*]|\*(?!\*\*))+\*\*\*|\*\*(?!\*)(?:[^*]|\*(?!\*))+\*\*|~~[^~]+~~|==[^=]+==|!\[[^\]]*\]\([^)]+\)|\[[^\]]+\]\([^)]+\)|\*(?!\s|\*)[^*]+(?<!\s)\*)/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let key = 0;
    while ((match = re.exec(text)) !== null) {
        if (match.index > lastIndex) parts.push(text.slice(lastIndex, match.index));
        const m = match[0];
        if (m.startsWith('`')) {
            // Inline code
            parts.push(<code key={key++} style={{ background: theme.lineNumBg, padding: '1px 4px', borderRadius: 3, fontSize: '0.9em', color: theme.syntaxString }}>{m.slice(1, -1)}</code>);
        } else if (m.startsWith('***') && m.endsWith('***')) {
            // Bold + italic — recurse into inner content for nested formatting
            parts.push(<strong key={key++}><em>{renderMdInline(m.slice(3, -3), theme)}</em></strong>);
        } else if (m.startsWith('**')) {
            // Bold — recurse into inner content for nested formatting
            parts.push(<strong key={key++}>{renderMdInline(m.slice(2, -2), theme)}</strong>);
        } else if (m.startsWith('~~')) {
            // Strikethrough — recurse into inner content
            parts.push(<del key={key++} style={{ opacity: 0.7 }}>{renderMdInline(m.slice(2, -2), theme)}</del>);
        } else if (m.startsWith('==')) {
            // Highlight — recurse into inner content
            parts.push(<mark key={key++} style={{ background: theme.tabHoverBg, color: theme.tabActiveText, padding: '0 2px', borderRadius: 2 }}>{renderMdInline(m.slice(2, -2), theme)}</mark>);
        } else if (m.startsWith('![')) {
            // Inline image
            const imgM = m.match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
            if (imgM) parts.push(<img key={key++} src={imgM[2]} alt={imgM[1]} style={{ maxHeight: 200, verticalAlign: 'middle', borderRadius: 3 }} />);
            else parts.push(m);
        } else if (m.startsWith('[')) {
            // Link
            const linkMatch = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (linkMatch) parts.push(<span key={key++} style={{ color: theme.syntaxFunction, textDecoration: 'underline', cursor: 'pointer' }}>{linkMatch[1]}</span>);
            else parts.push(m);
        } else if (m.startsWith('*') && m.endsWith('*')) {
            // Italic
            parts.push(<em key={key++}>{m.slice(1, -1)}</em>);
        } else {
            parts.push(m);
        }
        lastIndex = match.index + m.length;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return <>{parts}</>;
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
            // Content changed - restore the saved position
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
                            title="Close code preview"
                        >
                            X
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
                        No code files
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
            {/* Header with close button - double-click to toggle maximize */}
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
                    title="Close code preview"
                >
                    X
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
                    activeFile.language === 'markdown' ? (
                        <MarkdownPreview content={activeFile.content} theme={theme} />
                    ) : diffLines ? (
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
                        File not found
                    </div>
                )}
            </div>
            </div>
        </div>
    );
}
