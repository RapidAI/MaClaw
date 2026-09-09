/**
 * Markdown preview renderer for the code preview panel.
 * Supports find/go-to-line via data-line-start / data-line-end on blocks.
 */
import React, { useMemo } from 'react';
import type { CodePreviewTheme } from './FileTabBar';

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
function renderMdPreviewTable(tableLines: string[], theme: CodePreviewTheme): React.ReactNode | null {
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
        <div style={{ overflowX: 'auto', margin: '8px 0' }}>
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

/** Wrap a markdown block with line-range attrs for find / go-to-line. */
function wrapMdFindBlock(
    key: number,
    startLine0: number,
    endLine0: number,
    matchSet: Set<number>,
    activeMatchLine: number,
    child: React.ReactNode,
): React.ReactNode {
    const start = Math.max(0, startLine0);
    const end = Math.max(start, endLine0);
    // Fast path when find is closed / no matches — skip the per-line scan.
    let isMatch = false;
    let isActive = false;
    if (matchSet.size > 0 || activeMatchLine >= 0) {
        for (let L = start; L <= end; L++) {
            if (matchSet.has(L)) isMatch = true;
            if (L === activeMatchLine) isActive = true;
            if (isMatch && isActive) break;
        }
    }
    const bg = isActive
        ? 'rgba(234, 179, 8, 0.28)'
        : isMatch
            ? 'rgba(234, 179, 8, 0.12)'
            : undefined;
    return (
        <div
            key={key}
            data-testid="code-preview-md-block"
            data-line-start={start + 1}
            data-line-end={end + 1}
            data-find-match={isMatch ? 'true' : undefined}
            data-find-active={isActive ? 'true' : undefined}
            style={{
                backgroundColor: bg,
                borderRadius: bg ? 4 : undefined,
                margin: bg ? '2px 0' : undefined,
            }}
        >
            {child}
        </div>
    );
}

const EMPTY_MD_MATCH_LINE_INDEXES: number[] = [];

/** Parsed markdown block before find-highlight wrapping. */
interface MdParsedBlock {
    start0: number;
    end0: number;
    child: React.ReactNode;
}

/**
 * Parse markdown into line-ranged blocks. Separated from find highlighting so
 * match navigation does not re-run the full markdown/inline parse.
 */
function parseMarkdownBlocks(content: string, theme: CodePreviewTheme): MdParsedBlock[] {
    const lines = content.replace(/\r\n/g, '\n').replace(/\r/g, '\n').split('\n');
    const out: MdParsedBlock[] = [];
    let i = 0;
    let tableLines: string[] = [];
    let tableStart = 0;

    const pushBlock = (start0: number, end0: number, child: React.ReactNode) => {
        out.push({ start0, end0, child });
    };

    const flushTable = () => {
        if (tableLines.length === 0) return;
        const start0 = tableStart;
        const end0 = tableStart + tableLines.length - 1;
        const rendered = renderMdPreviewTable(tableLines, theme);
        if (rendered) {
            pushBlock(start0, end0, rendered);
        } else {
            for (let ti = 0; ti < tableLines.length; ti++) {
                const tl = tableLines[ti];
                pushBlock(start0 + ti, start0 + ti, (
                    <p style={{ margin: '4px 0', lineHeight: 1.6 }}>{renderMdInline(tl, theme)}</p>
                ));
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
            const start0 = i;
            const fence = fenceMatch[2];
            const fenceLang = line.slice(fenceMatch[0].length).trim();
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
            const end0 = i - 1;
            pushBlock(start0, end0, (
                <pre style={{ background: theme.lineNumBg, border: `1px solid ${theme.border}`, borderRadius: 6, padding: '10px 14px', margin: '8px 0', overflow: 'auto', fontSize: 13, lineHeight: 1.5 }}>
                    <code style={{ color: theme.text, fontFamily: "'Cascadia Code', 'Consolas', monospace" }}>
                        {fenceLang && <span style={{ color: theme.textMuted, fontSize: 11, display: 'block', marginBottom: 4 }}>{fenceLang}</span>}
                        {codeLines.join('\n')}
                    </code>
                </pre>
            ));
            continue;
        }

        // Table rows: collect consecutive pipe-delimited lines
        if (isMdPreviewTableRow(line)) {
            if (tableLines.length === 0) tableStart = i;
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
            pushBlock(i, i, (
                <div style={{ fontSize: sizes[level], fontWeight: level <= 2 ? 700 : 600, margin: margins[level], color: theme.tabActiveText, borderBottom: level <= 2 ? `1px solid ${theme.border}` : undefined, paddingBottom: level <= 2 ? 4 : undefined }}>
                    {renderMdInline(headingMatch[2], theme)}
                </div>
            ));
            i++;
            continue;
        }

        // Multi-line blockquote - collect consecutive > lines
        if (line.startsWith('>') || line.startsWith('> ')) {
            const start0 = i;
            const quoteLines: string[] = [];
            while (i < lines.length && (lines[i].startsWith('> ') || lines[i] === '>' || lines[i].startsWith('>'))) {
                quoteLines.push(lines[i].replace(/^>\s?/, ''));
                i++;
            }
            pushBlock(start0, i - 1, (
                <blockquote style={{ borderLeft: `1px solid ${theme.border}`, paddingLeft: 12, margin: '6px 0', color: theme.textMuted, fontStyle: 'italic', lineHeight: 1.6 }}>
                    {quoteLines.map((ql, qi) => (
                        <div key={qi}>{ql.trim() === '' ? <br /> : renderMdInline(ql, theme)}</div>
                    ))}
                </blockquote>
            ));
            continue;
        }

        // Task list: - [ ] or - [x] or * [ ] or * [x] - collect consecutive items
        const taskMatch = line.match(/^\s*[-*]\s+\[([ xX])\]\s+(.*)/);
        if (taskMatch) {
            const start0 = i;
            const taskItems: { checked: boolean; text: string }[] = [];
            while (i < lines.length) {
                const tm = lines[i].match(/^\s*[-*]\s+\[([ xX])\]\s+(.*)/);
                if (!tm) break;
                taskItems.push({ checked: tm[1].toLowerCase() === 'x', text: tm[2] });
                i++;
            }
            pushBlock(start0, i - 1, (
                <div style={{ margin: '4px 0', paddingLeft: 4 }}>
                    {taskItems.map((item, ti) => (
                        <div key={ti} style={{ paddingLeft: 12, margin: '2px 0', display: 'flex', alignItems: 'flex-start', gap: 6 }}>
                            <span style={{ flexShrink: 0, fontSize: 11, fontWeight: 700, color: theme.textMuted }}>{item.checked ? "DONE" : "TODO"}</span>
                            <span style={{ textDecoration: item.checked ? 'line-through' : undefined, opacity: item.checked ? 0.7 : 1 }}>{renderMdInline(item.text, theme)}</span>
                        </div>
                    ))}
                </div>
            ));
            continue;
        }

        // Unordered list - collect consecutive items (supports indentation for nesting)
        if (/^\s*[-*+]\s/.test(line)) {
            const start0 = i;
            const listItems: { indent: number; text: string }[] = [];
            while (i < lines.length && /^\s*[-*+]\s/.test(lines[i])) {
                const m = lines[i].match(/^(\s*)[-*+]\s+(.*)/);
                if (m) {
                    listItems.push({ indent: m[1].length, text: m[2] });
                }
                i++;
            }
            if (listItems.length === 0) {
                // Defensive: outer regex matched but no items parsed — treat as paragraph.
                pushBlock(start0, start0, <p style={{ margin: '4px 0', lineHeight: 1.6 }}>{renderMdInline(line, theme)}</p>);
                continue;
            }
            const baseIndent = Math.min(...listItems.map(it => it.indent));
            pushBlock(start0, i - 1, (
                <ul style={{ margin: '4px 0', paddingLeft: 20, listStyleType: 'disc' }}>
                    {listItems.map((item, li) => (
                        <li key={li} style={{ marginLeft: (item.indent - baseIndent) * 10, marginBottom: 2 }}>
                            {renderMdInline(item.text, theme)}
                        </li>
                    ))}
                </ul>
            ));
            continue;
        }

        // Ordered list - collect consecutive numbered items
        if (/^\s*\d+[.)]\s/.test(line)) {
            const start0 = i;
            const olItems: { indent: number; num: string; text: string }[] = [];
            while (i < lines.length && /^\s*\d+[.)]\s/.test(lines[i])) {
                const m = lines[i].match(/^(\s*)(\d+)[.)]\s+(.*)/);
                if (m) {
                    olItems.push({ indent: m[1].length, num: m[2], text: m[3] });
                }
                i++;
            }
            if (olItems.length === 0) {
                pushBlock(start0, start0, <p style={{ margin: '4px 0', lineHeight: 1.6 }}>{renderMdInline(line, theme)}</p>);
                continue;
            }
            const baseIndent = Math.min(...olItems.map(it => it.indent));
            pushBlock(start0, i - 1, (
                <ol style={{ margin: '4px 0', paddingLeft: 20 }}>
                    {olItems.map((item, li) => (
                        <li key={li} value={parseInt(item.num, 10)} style={{ marginLeft: (item.indent - baseIndent) * 10, marginBottom: 2 }}>
                            {renderMdInline(item.text, theme)}
                        </li>
                    ))}
                </ol>
            ));
            continue;
        }

        // Horizontal rule
        if (/^[-*_]{3,}\s*$/.test(line.trim()) && !line.trim().startsWith('|')) {
            pushBlock(i, i, <hr style={{ border: 'none', borderTop: `1px solid ${theme.border}`, margin: '12px 0' }} />);
            i++;
            continue;
        }

        // Image on its own line: ![alt](url)
        const imgMatch = line.trim().match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
        if (imgMatch) {
            pushBlock(i, i, (
                <div style={{ margin: '8px 0', textAlign: 'center' }}>
                    <img src={imgMatch[2]} alt={imgMatch[1]} style={{ maxWidth: '100%', borderRadius: 4, border: `1px solid ${theme.border}` }} />
                    {imgMatch[1] && <div style={{ fontSize: 12, color: theme.textMuted, marginTop: 4 }}>{imgMatch[1]}</div>}
                </div>
            ));
            i++;
            continue;
        }

        // Definition list: term followed by : definition
        if (i + 1 < lines.length && line.trim() !== '' && /^\s*:\s+/.test(lines[i + 1])) {
            const start0 = i;
            const term = line.trim();
            const defs: string[] = [];
            i++;
            while (i < lines.length && /^\s*:\s+/.test(lines[i])) {
                defs.push(lines[i].replace(/^\s*:\s+/, ''));
                i++;
            }
            pushBlock(start0, i - 1, (
                <dl style={{ margin: '6px 0' }}>
                    <dt style={{ fontWeight: 600 }}>{renderMdInline(term, theme)}</dt>
                    {defs.map((d, di) => (
                        <dd key={di} style={{ marginLeft: 20, margin: '2px 0 2px 20px', color: theme.text }}>{renderMdInline(d, theme)}</dd>
                    ))}
                </dl>
            ));
            continue;
        }

        // Empty line
        if (line.trim() === '') {
            pushBlock(i, i, <div style={{ height: 8 }} />);
            i++;
            continue;
        }

        // Default: paragraph
        pushBlock(i, i, <p style={{ margin: '4px 0', lineHeight: 1.6 }}>{renderMdInline(line, theme)}</p>);
        i++;
    }
    flushTable();
    return out;
}

export const MarkdownPreview = React.memo(function MarkdownPreview({
    content,
    theme,
    matchLineIndexes = EMPTY_MD_MATCH_LINE_INDEXES,
    activeMatchLine = -1,
}: {
    content: string;
    theme: CodePreviewTheme;
    matchLineIndexes?: number[];
    activeMatchLine?: number;
}) {
    const matchSet = useMemo(() => new Set(matchLineIndexes), [matchLineIndexes]);

    // Expensive structure parse — only when content/theme change.
    const blocks = useMemo(() => parseMarkdownBlocks(content, theme), [content, theme]);

    // Cheap find-highlight wrap — re-runs on match navigation without re-parsing markdown.
    const elements = useMemo(
        () => blocks.map((b, i) => wrapMdFindBlock(i, b.start0, b.end0, matchSet, activeMatchLine, b.child)),
        [activeMatchLine, blocks, matchSet],
    );

    return (
        <div
            data-testid="code-preview-markdown-view"
            style={{ padding: '16px 20px', fontSize: 14, lineHeight: 1.6, color: theme.text, fontFamily: 'inherit', wordBreak: 'break-word' }}
        >
            {elements}
        </div>
    );
});

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
