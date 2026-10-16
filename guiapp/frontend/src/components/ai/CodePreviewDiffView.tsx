/**
 * Change-focused diff view for the code preview panel.
 *
 * Goals (as opposed to a plain `+/-` line dump):
 *   - Pair a removed line with its rewritten counterpart so "改了什么" is
 *     visible as one unit (modify row) instead of two unrelated rows.
 *   - Mark the exact rewritten words inside a modify row (word-level diff).
 *   - Collapse untouched regions into `@@ … @@` hunks so every change lands
 *     on screen, with a "只看变更" mode that drops context entirely.
 *   - Offer a side-by-side (旧 | 新) layout, which reads like the review UIs
 *     people know from Codex / GitHub / VS Code.
 *
 * Layout is CSS-grid based; colors come from the theme through CSS custom
 * properties set once on the root (see codeDiffCssVars).
 */
import React, { useEffect, useMemo, useRef, useState } from 'react';
import type { CodePreviewTheme } from './FileTabBar';
import { tokenizeLine } from './syntaxHighlight';
import type { HighlightToken } from './syntaxHighlight';
import {
    buildDiffHunks,
    type DiffHunk,
    type DiffRow,
    type WordSegment,
    type DiffLine,
} from './diffCompute';
import {
    CODE_PREVIEW_FONT_DEFAULT,
    clampCodePreviewFontSize,
    codePreviewLineHeight,
} from './codePreviewFindHelpers';

/** `split` = 旧 | 新 side by side, `inline` = single unified column. */
export type CodeDiffMode = 'split' | 'inline';

// minimapRowFromDiffRow is defined below; the minimap samples one entry per
// visual row, so folded/paired rows must be mapped, not passed through.

/**
 * Map one visual row onto the minimap row model: the minimap samples the
 * document 1:1 by visual row, while this view folds untouched regions and pairs
 * rewrites, so the raw DiffLine[] no longer lines up with the screen. A rewrite
 * shows up as an addition (its new side).
 */
export function minimapRowFromDiffRow(row: DiffRow): DiffLine {
    if (row.kind === 'delete') {
        return { type: 'delete', content: row.oldText, oldLineNum: row.oldLineNum };
    }
    if (row.kind === 'add' || row.kind === 'modify') {
        return { type: 'add', content: row.newText, newLineNum: row.newLineNum };
    }
    return { type: 'unchanged', content: row.newText, oldLineNum: row.oldLineNum, newLineNum: row.newLineNum };
}

/** Unchanged lines kept around each hunk before folding kicks in. */
const CONTEXT_LINES = 3;

const EMPTY_MATCH_LINE_INDEXES: number[] = [];
const EMPTY_INDEX_SET: ReadonlySet<number> = new Set<number>();

// ── Theme → CSS custom properties ──

/** Palette hand-off to the stylesheet: one write per render, not per row. */
export function codeDiffCssVars(theme: CodePreviewTheme): React.CSSProperties {
    return {
        '--cp-diff-text': theme.text,
        '--cp-diff-ink-muted': theme.lineNumText,
        '--cp-diff-border': theme.border,
        '--cp-diff-gutter-bg': theme.lineNumBg,
        '--cp-diff-add-bg': theme.diffAddBg,
        '--cp-diff-del-bg': theme.diffDeleteBg,
        '--cp-diff-add-ink': theme.diffAddText,
        '--cp-diff-del-ink': theme.diffDeleteText,
        '--cp-diff-bar-bg': theme.tabBg,
        '--cp-diff-hover-bg': theme.tabHoverBg,
        '--cp-diff-active-bg': theme.tabActiveBg,
        '--cp-diff-active-fg': theme.tabActiveText,
    } as React.CSSProperties;
}

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

// ── Line rendering ──

/**
 * Render one side of a diff line: syntax colors from tokenizeLine, plus a
 * highlight block over the runs that the word-level diff flagged as changed.
 */
const SegmentedLine = React.memo(function SegmentedLine({
    text,
    segments,
    language,
    theme,
    markClass,
}: {
    text: string;
    segments?: WordSegment[];
    language: string;
    theme: CodePreviewTheme;
    markClass: string;
}) {
    if (!segments || segments.length === 0) {
        if (text === '') return <span>{'\u00a0'}</span>;
        const tokens = tokenizeLine(text, language);
        if (tokens.length === 0) return <span>{'\u00a0'}</span>;
        return (
            <>
                {tokens.map((tok, i) => (
                    <span key={i} style={{ color: tokenColor(tok.type, theme) }}>{tok.text}</span>
                ))}
            </>
        );
    }
    const nodes: React.ReactNode[] = [];
    let key = 0;
    for (const seg of segments) {
        const tokens = tokenizeLine(seg.text, language);
        const spans: HighlightToken[] = tokens.length === 0 ? [{ text: seg.text, type: 'plain' }] : tokens;
        for (const tok of spans) {
            nodes.push(
                <span
                    key={key++}
                    className={seg.changed ? markClass : undefined}
                    style={{ color: tokenColor(tok.type, theme) }}
                >
                    {tok.text}
                </span>,
            );
        }
    }
    return <>{nodes}</>;
});

function DiffCell({
    text,
    segments,
    cellClass,
    language,
    theme,
    markClass,
}: {
    text: string;
    segments?: WordSegment[];
    cellClass: string;
    language: string;
    theme: CodePreviewTheme;
    markClass: string;
}) {
    return (
        <td className={`cp-diff-cell ${cellClass}`}>
            {text === ''
                ? '\u00a0'
                : <SegmentedLine text={text} segments={segments} language={language} theme={theme} markClass={markClass} />}
        </td>
    );
}

function Gutter({ value }: { value?: number }) {
    return <td className="cp-diff-gutter">{value ?? ''}</td>;
}

function Sign({ value, className }: { value: string; className?: string }) {
    return <td className={`cp-diff-sign ${className || ''}`}>{value}</td>;
}

/** Full-width separator row (hunk header / collapsed gap). */
function DiffSeparatorRow({ colSpan, children }: { colSpan: number; children: React.ReactNode }) {
    return (
        <tr className="cp-diff-sep">
            <td className="cp-diff-sep-cell" colSpan={colSpan}>{children}</td>
        </tr>
    );
}

// ── Rows ──

function SplitRow({
    row,
    language,
    theme,
    matchClass,
    dataAttrs,
}: {
    row: DiffRow;
    language: string;
    theme: CodePreviewTheme;
    matchClass: string;
    dataAttrs: Record<string, string | undefined>;
}) {
    const oldFilled = row.kind !== 'add';
    const newFilled = row.kind !== 'delete';
    // Unchanged rows stay neutral; only the half that actually changed is tinted,
    // and the missing half of an add/delete gets the muted "nothing here" fill.
    const oldClass = row.kind === 'delete' || row.kind === 'modify'
        ? 'cp-diff-cell-del cp-diff-cell-old'
        : row.kind === 'add' ? 'cp-diff-cell-empty cp-diff-cell-old' : 'cp-diff-cell-old';
    const newClass = row.kind === 'add' || row.kind === 'modify'
        ? 'cp-diff-cell-add'
        : row.kind === 'delete' ? 'cp-diff-cell-empty' : '';
    const oldSign = oldFilled && row.kind !== 'unchanged' ? '-' : '';
    const newSign = newFilled && row.kind !== 'unchanged' ? '+' : '';
    return (
        <tr className={`cp-diff-row cp-diff-row-split cp-diff-row-${row.kind} ${matchClass}`} {...dataAttrs}>
            <Gutter value={oldFilled ? row.oldLineNum : undefined} />
            <Sign value={oldSign} className={oldSign ? 'cp-diff-sign-del' : undefined} />
            <DiffCell
                text={oldFilled ? row.oldText : ''}
                segments={row.kind === 'modify' ? row.oldSegments : undefined}
                cellClass={oldClass}
                language={language}
                theme={theme}
                markClass="cp-diff-mark-del"
            />
            <Gutter value={newFilled ? row.newLineNum : undefined} />
            <Sign value={newSign} className={newSign ? 'cp-diff-sign-add' : undefined} />
            <DiffCell
                text={newFilled ? row.newText : ''}
                segments={row.kind === 'modify' ? row.newSegments : undefined}
                cellClass={newClass}
                language={language}
                theme={theme}
                markClass="cp-diff-mark-add"
            />
        </tr>
    );
}

function InlineRow({
    row,
    language,
    theme,
    matchClass,
    dataAttrs,
}: {
    row: DiffRow;
    language: string;
    theme: CodePreviewTheme;
    matchClass: string;
    dataAttrs: Record<string, string | undefined>;
}) {
    const parts: React.ReactNode[] = [];
    if (row.kind === 'modify') {
        parts.push(
            <tr key="old" className={`cp-diff-row cp-diff-row-inline cp-diff-row-modold ${matchClass}`} {...dataAttrs}>
                <Gutter value={row.oldLineNum} />
                <Gutter />
                <Sign value="-" className="cp-diff-sign-del" />
                <DiffCell text={row.oldText} segments={row.oldSegments} cellClass="cp-diff-cell-del" language={language} theme={theme} markClass="cp-diff-mark-del" />
            </tr>,
        );
        parts.push(
            <tr key="new" className={`cp-diff-row cp-diff-row-inline cp-diff-row-modnew ${matchClass}`} {...dataAttrs}>
                <Gutter />
                <Gutter value={row.newLineNum} />
                <Sign value="+" className="cp-diff-sign-add" />
                <DiffCell text={row.newText} segments={row.newSegments} cellClass="cp-diff-cell-add" language={language} theme={theme} markClass="cp-diff-mark-add" />
            </tr>,
        );
        return <>{parts}</>;
    }
    const sign = row.kind === 'add' ? '+' : row.kind === 'delete' ? '-' : '';
    const signClass = row.kind === 'add' ? 'cp-diff-sign-add' : row.kind === 'delete' ? 'cp-diff-sign-del' : undefined;
    const cellClass = row.kind === 'add' ? 'cp-diff-cell-add' : row.kind === 'delete' ? 'cp-diff-cell-del' : '';
    const text = row.kind === 'delete' ? row.oldText : row.newText;
    return (
        <tr className={`cp-diff-row cp-diff-row-inline cp-diff-row-${row.kind} ${matchClass}`} {...dataAttrs}>
            <Gutter value={row.kind === 'add' ? undefined : row.oldLineNum} />
            <Gutter value={row.kind === 'delete' ? undefined : row.newLineNum} />
            <Sign value={sign} className={signClass} />
            <DiffCell text={text} cellClass={cellClass} language={language} theme={theme} markClass={row.kind === 'delete' ? 'cp-diff-mark-del' : 'cp-diff-mark-add'} />
        </tr>
    );
}

// ── Items (rows + hunk headers + collapsed gaps) ──

type RenderItem =
    | { kind: 'row'; key: string; row: DiffRow; changeIndex: number }
    | { kind: 'hunk'; key: string; hunk: DiffHunk; hunkIndex: number; collapsed: boolean }
    | { kind: 'gap'; key: string; gapIndex: number; from: number; to: number };

function buildRenderItems(
    rows: DiffRow[],
    changeIndexByRow: number[],
    hunks: DiffHunk[],
    onlyChanges: boolean,
    foldUnchanged: boolean,
    collapsedHunks: ReadonlySet<number>,
    expandedGaps: ReadonlySet<number>,
): RenderItem[] {
    const rowItem = (index: number): RenderItem => ({
        kind: 'row',
        key: `r${index}`,
        row: rows[index],
        changeIndex: changeIndexByRow[index],
    });

    if (onlyChanges) {
        const items: RenderItem[] = [];
        for (let i = 0; i < rows.length; i++) {
            if (rows[i].kind !== 'unchanged') items.push(rowItem(i));
        }
        // Nothing changed in this file: fall back to the full render instead of
        // a blank body (the summary bar is hidden when there is no change, so
        // the user could not switch the filter back off).
        if (items.length > 0 || rows.length === 0) return items;
    }

    if (hunks.length === 0 || !foldUnchanged) {
        return rows.map((_, i) => rowItem(i));
    }

    const items: RenderItem[] = [];
    const pushGap = (gapIndex: number, from: number, to: number) => {
        if (to <= from) return;
        if (expandedGaps.has(gapIndex)) {
            for (let i = from; i < to; i++) items.push(rowItem(i));
            return;
        }
        items.push({ kind: 'gap', key: `g${gapIndex}`, gapIndex, from, to });
    };

    hunks.forEach((hunk, hunkIndex) => {
        const prevEnd = hunkIndex === 0 ? 0 : hunks[hunkIndex - 1].end;
        pushGap(hunkIndex, prevEnd, hunk.start);
        const collapsed = collapsedHunks.has(hunkIndex);
        items.push({ kind: 'hunk', key: `h${hunkIndex}`, hunk, hunkIndex, collapsed });
        if (!collapsed) {
            for (let i = hunk.start; i < hunk.end; i++) items.push(rowItem(i));
        }
    });
    const lastEnd = hunks[hunks.length - 1].end;
    pushGap(hunks.length, lastEnd, rows.length);
    return items;
}

// ── Diff view ──

export interface CodePreviewDiffViewProps {
    rows: DiffRow[];
    theme: CodePreviewTheme;
    language: string;
    lang: string;
    mode: CodeDiffMode;
    /** Drop unchanged context lines entirely. */
    onlyChanges: boolean;
    /** false renders the whole file (hunk headers stay, nothing is folded). */
    foldUnchanged: boolean;
    matchLineIndexes?: number[];
    activeMatchLine?: number;
    wordWrap?: boolean;
    fontSize?: number;
    /** Change ordinal the host wants scrolled into view (-1 = none). */
    focusChangeIndex?: number;
    /** Bumped by the host so repeated next/prev clicks re-scroll. */
    focusNonce?: number;
    /** Changing this (file path) drops fold state left over from another file. */
    resetKey?: string;
    /** Reports the rendered rows so the host can keep the minimap aligned. */
    onRenderedRows?: (rows: DiffLine[]) => void;
}

export const CodePreviewDiffView = React.memo(function CodePreviewDiffView({
    rows,
    theme,
    language,
    lang,
    mode,
    onlyChanges,
    foldUnchanged,
    matchLineIndexes = EMPTY_MATCH_LINE_INDEXES,
    activeMatchLine = -1,
    wordWrap = false,
    fontSize = CODE_PREVIEW_FONT_DEFAULT,
    focusChangeIndex = -1,
    focusNonce = 0,
    resetKey,
    onRenderedRows,
}: CodePreviewDiffViewProps) {
    const isZh = lang.startsWith('zh');
    const isZhHant = lang === 'zh-Hant';
    const rootRef = useRef<HTMLTableElement | null>(null);
    const [collapsedHunks, setCollapsedHunks] = useState<ReadonlySet<number>>(EMPTY_INDEX_SET);
    const [expandedGaps, setExpandedGaps] = useState<ReadonlySet<number>>(EMPTY_INDEX_SET);

    const size = clampCodePreviewFontSize(fontSize);
    const lineHeight = codePreviewLineHeight(size);
    const vars = useMemo(() => codeDiffCssVars(theme), [theme]);

    const changeIndexByRow = useMemo(() => {
        const out = new Array<number>(rows.length);
        let n = 0;
        for (let i = 0; i < rows.length; i++) {
            if (rows[i].kind === 'unchanged') out[i] = -1;
            else out[i] = n++;
        }
        return out;
    }, [rows]);

    const hunks = useMemo(() => buildDiffHunks(rows, CONTEXT_LINES), [rows]);
    const matchSet = useMemo(() => new Set(matchLineIndexes), [matchLineIndexes]);

    const items = useMemo(
        () => buildRenderItems(rows, changeIndexByRow, hunks, onlyChanges, foldUnchanged, collapsedHunks, expandedGaps),
        [rows, changeIndexByRow, hunks, onlyChanges, foldUnchanged, collapsedHunks, expandedGaps],
    );

    // One entry per rendered code row (separators excluded) so the minimap can
    // paint exactly what the viewport shows.
    const renderedRows = useMemo<DiffLine[]>(() => {
        const out: DiffLine[] = [];
        for (const item of items) {
            if (item.kind === 'row') out.push(minimapRowFromDiffRow(item.row));
        }
        return out;
    }, [items]);

    useEffect(() => {
        onRenderedRows?.(renderedRows);
    }, [onRenderedRows, renderedRows]);

    // Fold state is per file: drop it when the host switches tabs.
    useEffect(() => {
        setCollapsedHunks(EMPTY_INDEX_SET);
        setExpandedGaps(EMPTY_INDEX_SET);
    }, [resetKey]);

    // Jump to the change the host asked for (prev/next buttons, file switch).
    useEffect(() => {
        if (focusChangeIndex < 0) return;
        const root = rootRef.current;
        if (!root) return;
        const el = root.querySelector(`[data-change-index="${focusChangeIndex}"]`);
        if (el instanceof HTMLElement && typeof el.scrollIntoView === 'function') {
            el.scrollIntoView({ block: 'center', behavior: 'auto' });
        }
    }, [focusChangeIndex, focusNonce, resetKey]);

    const toggleHunk = (hunkIndex: number) => {
        setCollapsedHunks((prev) => {
            const next = new Set(prev);
            if (next.has(hunkIndex)) next.delete(hunkIndex);
            else next.add(hunkIndex);
            return next;
        });
    };
    const toggleGap = (gapIndex: number) => {
        setExpandedGaps((prev) => {
            const next = new Set(prev);
            if (next.has(gapIndex)) next.delete(gapIndex);
            else next.add(gapIndex);
            return next;
        });
    };

    const hunkStats = (hunk: DiffHunk) => {
        let added = 0;
        let removed = 0;
        for (let i = hunk.start; i < hunk.end; i++) {
            const kind = rows[i].kind;
            if (kind === 'add' || kind === 'modify') added++;
            if (kind === 'delete' || kind === 'modify') removed++;
        }
        return { added, removed };
    };

    const collapseLabel = isZhHant ? '摺疊' : isZh ? '折叠' : 'collapse';
    const expandLabel = isZhHant ? '展開' : isZh ? '展开' : 'expand';
    const hiddenLabel = (count: number) =>
        isZhHant
            ? `展開 ${count} 行未變更`
            : isZh
                ? `展开 ${count} 行未变更`
                : `Show ${count} unchanged lines`;

    // Table layout on purpose: auto table sizing keeps the old/new columns
    // aligned across rows and grows past the pane so the body scrolls sideways
    // (per-row CSS grids either misalign the columns or overlap the text).
    const colCount = mode === 'split' ? 6 : 4;

    return (
        <table
            ref={rootRef}
            data-testid="code-preview-diff-view"
            data-diff-mode={mode}
            data-only-changes={onlyChanges ? 'true' : 'false'}
            data-word-wrap={wordWrap ? 'true' : 'false'}
            data-font-size={String(size)}
            className={`cp-diff${wordWrap ? ' cp-diff-wrap' : ''}`}
            style={{ ...vars, fontSize: size, lineHeight: `${lineHeight}px` }}
        >
            <tbody>
            {items.map((item) => {
                if (item.kind === 'gap') {
                    const count = item.to - item.from;
                    return (
                        <DiffSeparatorRow key={item.key} colSpan={colCount}>
                            <button
                                type="button"
                                data-testid="code-preview-diff-gap"
                                className="cp-diff-gap"
                                onClick={() => toggleGap(item.gapIndex)}
                            >
                                <span className="cp-diff-gap-dots">⋯</span>
                                <span>{hiddenLabel(count)}</span>
                            </button>
                        </DiffSeparatorRow>
                    );
                }
                if (item.kind === 'hunk') {
                    const { added, removed } = hunkStats(item.hunk);
                    const range = `@@ -${item.hunk.oldStart},${item.hunk.oldCount} +${item.hunk.newStart},${item.hunk.newCount} @@`;
                    return (
                        <DiffSeparatorRow key={item.key} colSpan={colCount}>
                            <button
                                type="button"
                                data-testid="code-preview-diff-hunk"
                                data-collapsed={item.collapsed ? 'true' : 'false'}
                                className="cp-diff-hunk"
                                onClick={() => toggleHunk(item.hunkIndex)}
                                title={item.collapsed ? expandLabel : collapseLabel}
                            >
                                <span className="cp-diff-hunk-mark">{item.collapsed ? '▸' : '▾'}</span>
                                <span className="cp-diff-hunk-range">{range}</span>
                                <span className="cp-diff-hunk-stats">
                                    <span className="cp-diff-stat-add">+{added}</span>
                                    {' '}
                                    <span className="cp-diff-stat-del">-{removed}</span>
                                </span>
                                <span className="cp-diff-hunk-action">{item.collapsed ? expandLabel : collapseLabel}</span>
                            </button>
                        </DiffSeparatorRow>
                    );
                }

                const row = item.row;
                const contentLineIdx = row.newLineNum != null ? row.newLineNum - 1 : -1;
                const isMatch = contentLineIdx >= 0 && matchSet.has(contentLineIdx);
                const isActiveMatch = contentLineIdx >= 0 && contentLineIdx === activeMatchLine;
                const matchClass = isActiveMatch ? 'cp-diff-row-match-active' : isMatch ? 'cp-diff-row-match' : '';
                const dataAttrs: Record<string, string | undefined> = {
                    'data-line': row.newLineNum != null ? String(row.newLineNum) : undefined,
                    'data-change-index': item.changeIndex >= 0 ? String(item.changeIndex) : undefined,
                    'data-diff-kind': row.kind,
                    'data-find-match': isMatch ? 'true' : undefined,
                    'data-find-active': isActiveMatch ? 'true' : undefined,
                };
                return mode === 'split'
                    ? <SplitRow key={item.key} row={row} language={language} theme={theme} matchClass={matchClass} dataAttrs={dataAttrs} />
                    : <InlineRow key={item.key} row={row} language={language} theme={theme} matchClass={matchClass} dataAttrs={dataAttrs} />;
            })}
            </tbody>
        </table>
    );
});

// ── Summary / control bar ──

export interface CodeDiffSummaryBarProps {
    stats: { added: number; removed: number; modified: number };
    changeCount: number;
    cursor: number;
    mode: CodeDiffMode;
    onlyChanges: boolean;
    foldUnchanged: boolean;
    theme: CodePreviewTheme;
    lang: string;
    onModeChange: (mode: CodeDiffMode) => void;
    onToggleOnlyChanges: () => void;
    onToggleFold: () => void;
    onPrevChange: () => void;
    onNextChange: () => void;
}

/**
 * Sticky-ish control strip above the diff: how much changed, plus the toggles
 * that make the changes pop (并排/内联, 只看变更, 上一处/下一处).
 */
export function CodeDiffSummaryBar({
    stats,
    changeCount,
    cursor,
    mode,
    onlyChanges,
    foldUnchanged,
    theme,
    lang,
    onModeChange,
    onToggleOnlyChanges,
    onToggleFold,
    onPrevChange,
    onNextChange,
}: CodeDiffSummaryBarProps) {
    const isZh = lang.startsWith('zh');
    const isZhHant = lang === 'zh-Hant';
    const vars = useMemo(() => codeDiffCssVars(theme), [theme]);

    const noChanges = stats.added === 0 && stats.removed === 0;
    const splitLabel = isZhHant ? '並排' : isZh ? '并排' : 'Split';
    const inlineLabel = isZhHant ? '內聯' : isZh ? '内联' : 'Inline';
    const onlyLabel = isZhHant ? '只看變更' : isZh ? '只看变更' : 'Changes only';
    const foldLabel = foldUnchanged
        ? (isZhHant ? '展開全文' : isZh ? '展开全文' : 'Full file')
        : (isZhHant ? '摺疊未變更' : isZh ? '折叠未变更' : 'Fold context');
    const navLabel = isZhHant ? '變更' : isZh ? '变更' : 'change';
    const prevTitle = isZhHant ? '上一處變更 (Alt+↑)' : isZh ? '上一处变更 (Alt+↑)' : 'Previous change (Alt+Up)';
    const nextTitle = isZhHant ? '下一處變更 (Alt+↓)' : isZh ? '下一处变更 (Alt+↓)' : 'Next change (Alt+Down)';

    return (
        <div
            data-testid="code-preview-diff-summary"
            className="cp-diff-bar"
            style={vars}
        >
            <span className="cp-diff-stat-add" title={isZh ? '新增行 / 改写行' : 'added / rewritten lines'}>+{stats.added}</span>
            <span className="cp-diff-stat-del" title={isZh ? '删除行 / 被改写行' : 'removed / rewritten lines'}>-{stats.removed}</span>
            {stats.modified > 0 && (
                <span className="cp-diff-stat-mod" title={isZh ? '被改写的行（成对显示）' : 'rewritten lines (paired)'}>
                    ~{stats.modified}
                </span>
            )}
            {noChanges && (
                <span className="cp-diff-bar-note">{isZhHant ? '無變更' : isZh ? '无变更' : 'No changes'}</span>
            )}
            <span className="cp-diff-bar-spacer" />
            <button
                type="button"
                data-testid="code-preview-diff-mode-split"
                data-active={mode === 'split' ? 'true' : 'false'}
                className="cp-diff-btn"
                onClick={() => onModeChange('split')}
            >
                {splitLabel}
            </button>
            <button
                type="button"
                data-testid="code-preview-diff-mode-inline"
                data-active={mode === 'inline' ? 'true' : 'false'}
                className="cp-diff-btn"
                onClick={() => onModeChange('inline')}
            >
                {inlineLabel}
            </button>
            <button
                type="button"
                data-testid="code-preview-diff-only-changes"
                data-active={onlyChanges ? 'true' : 'false'}
                className="cp-diff-btn"
                onClick={onToggleOnlyChanges}
            >
                {onlyLabel}
            </button>
            <button
                type="button"
                data-testid="code-preview-diff-fold"
                data-active={foldUnchanged ? 'true' : 'false'}
                className="cp-diff-btn"
                onClick={onToggleFold}
                disabled={onlyChanges}
            >
                {foldLabel}
            </button>
            <span className="cp-diff-nav">
                <button
                    type="button"
                    data-testid="code-preview-diff-prev"
                    className="cp-diff-btn cp-diff-btn-icon"
                    onClick={onPrevChange}
                    disabled={changeCount === 0}
                    title={prevTitle}
                >
                    ↑
                </button>
                <span data-testid="code-preview-diff-cursor" className="cp-diff-cursor">
                    {changeCount === 0 ? `0 / 0` : `${Math.min(Math.max(cursor, 0), changeCount - 1) + 1} / ${changeCount}`}
                    <span className="cp-diff-cursor-unit"> {navLabel}</span>
                </span>
                <button
                    type="button"
                    data-testid="code-preview-diff-next"
                    className="cp-diff-btn cp-diff-btn-icon"
                    onClick={onNextChange}
                    disabled={changeCount === 0}
                    title={nextTitle}
                >
                    ↓
                </button>
            </span>
        </div>
    );
}
