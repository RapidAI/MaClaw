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
 * Split modules:
 *   - codePreviewFindHelpers.ts  pure find / prefs / language helpers
 *   - CodePreviewMarkdown.tsx    markdown renderer
 *   - CodePreviewPanel.tsx       this file (shell + code/diff views)
 *
 * Uses inline styles based on theme props (no CSS modules).
 */
import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { CodeFileDiffStat, FileTabBar, cycleFilePath } from './FileTabBar';
import type { CodePreviewTheme } from './FileTabBar';
import type { CodeFile } from './useCodePreviewState';
import { codeFileLineDeltaHasChange, computeCodeFileLineDelta, getMruCycleOrder, isCodeFileDirty } from './useCodePreviewState';
import { computeDiff } from './diffCompute';
import type { DiffLine } from './diffCompute';
import { tokenizeLine } from './syntaxHighlight';
import type { HighlightToken } from './syntaxHighlight';
import { MarkdownPreview } from './CodePreviewMarkdown';
import { CodePreviewWorkspace } from './CodePreviewWorkspace';
import { CloudWorkspaceEntitlement } from '../../../wailsjs/go/main/App';
import { cloudWorkspaceIdFromPath, lookupCloudWorkspaceDisplayName, rememberCloudWorkspaceDisplayNames, FOCUS_CLOUD_WORKSPACE_TREE_EVENT } from './codingTaskMode';
import { relativeLuminance, type Theme } from './aiAssistantPanelTheme';
import {
    CODE_PREVIEW_FONT_DEFAULT,
    CODE_PREVIEW_FONT_MAX,
    CODE_PREVIEW_FONT_MIN,
    clampCodePreviewFontSize,
    codePreviewLineHeight,
    compileFindMatcher,
    cycleMatchIndex,
    formatCodeLanguageLabel,
    loadCodePreviewViewPrefs,
    parseGoToLineInput,
    saveCodePreviewViewPrefs,
    type CodePreviewViewPrefs,
    type FindMatchOptions,
} from './codePreviewFindHelpers';

// Re-export public helpers / types so existing imports from CodePreviewPanel keep working.
export type { CodePreviewTheme } from './FileTabBar';
export type { CodePreviewViewPrefs, FindMatchOptions } from './codePreviewFindHelpers';
export {
    CODE_PREVIEW_FONT_DEFAULT,
    CODE_PREVIEW_FONT_MAX,
    CODE_PREVIEW_FONT_MIN,
    CODE_PREVIEW_VIEW_PREFS_KEY,
    clampCodePreviewFontSize,
    codePreviewLineHeight,
    compileFindMatcher,
    cycleMatchIndex,
    defaultCodePreviewViewPrefs,
    escapeRegExp,
    findMatchLineIndexes,
    formatCodeLanguageLabel,
    loadCodePreviewViewPrefs,
    parseGoToLineInput,
    saveCodePreviewViewPrefs,
} from './codePreviewFindHelpers';

// ── Theme Constants ──

/** Dark theme for the code preview panel. */
export const darkCodePreviewTheme: CodePreviewTheme = {
    bg: '#0f1720',
    text: '#d7dee8',
    textMuted: '#a0abbe',
    border: '#263447',
    lineNumBg: '#111b27',
    lineNumText: '#8494a8',
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
    syntaxComment: '#8d9eb2',
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
    syntaxKeyword: '#2f6fbc',
    syntaxString: '#4f7f6f',
    syntaxComment: '#64748b',
    syntaxNumber: '#2f6fbc',
    syntaxFunction: '#334155',
    syntaxType: '#2f6fbc',
    syntaxOperator: '#334155',
};

/**
 * Derive the source-preview palette from the active assistant scheme.
 *
 * The preview used to select one of the two fixed palettes above purely from
 * light/dark mode. That meant alternate assistant schemes changed the chat but
 * left the source review surface looking like it belonged to another app.
 */
export function createCodePreviewTheme(theme: Theme): CodePreviewTheme {
    const isDark = theme.isDark === true;
    const success = isDark ? '#7aa89a' : '#3f685b';
    const successBg = `color-mix(in srgb, ${success} ${isDark ? 18 : 12}%, ${theme.fieldBg})`;

    return {
        bg: theme.bg,
        text: theme.codeText || theme.text,
        textMuted: theme.textMuted,
        border: theme.divider,
        lineNumBg: theme.codeBg,
        lineNumText: theme.textMuted,
        tabBg: theme.titleBarBg,
        tabActiveBg: theme.fieldBg,
        tabActiveText: theme.headingColor,
        tabHoverBg: `color-mix(in srgb, ${theme.btnColor} ${isDark ? 14 : 8}%, ${theme.titleBarBg})`,
        diffAddBg: successBg,
        diffAddText: success,
        diffDeleteBg: theme.errorBg,
        diffDeleteText: theme.errorText,
        syntaxKeyword: theme.linkColor,
        syntaxString: success,
        syntaxComment: theme.textMuted,
        syntaxNumber: theme.pathColor,
        syntaxFunction: theme.codeText || theme.text,
        syntaxType: theme.linkColor,
        syntaxOperator: theme.text,
    };
}

/** Choose the more legible neutral ink for muted semantic fills. */
export function maximumContrastInkOnFill(fillCss: string): string {
    const fillLuminance = relativeLuminance(fillCss);
    if (fillLuminance == null) return '#ffffff';
    const contrast = (foregroundLuminance: number) => {
        const lighter = Math.max(fillLuminance, foregroundLuminance);
        const darker = Math.min(fillLuminance, foregroundLuminance);
        return (lighter + 0.05) / (darker + 0.05);
    };
    return contrast(relativeLuminance('#111111')!) >= contrast(relativeLuminance('#ffffff')!)
        ? '#111111'
        : '#ffffff';
}
// ── Props ──

export interface CodePreviewPanelProps {
    files: Map<string, CodeFile>;
    activeFilePath: string;
    pinnedPaths?: string[];
    mruOrder?: string[];
    onSelectFile: (filePath: string) => void;
    /** The coding task whose local/remote workdir powers the default explorer tab. */
    projectPath?: string;
    /** Bumped after remote SSH reconnect so the workspace tree reloads in place. */
    workspaceRefreshToken?: number;
    /** Local working-directory changes drop the previous tree immediately. */
    workspaceResetOnRefresh?: boolean;
    onOpenWorkspaceFile?: (file: CodeFile) => void;
    /** Close a single file tab (VS Code-style). */
    onCloseFile?: (filePath: string) => void;
    onCloseOtherFiles?: (keepPath: string) => void;
    onCloseFilesToTheRight?: (fromPath: string) => void;
    onCloseAllFiles?: () => void;
    onMoveFile?: (fromPath: string, toIndex: number) => void;
    onTogglePinFile?: (filePath: string) => void;
    onClose: () => void;
    onResizeStart?: () => void;
    /** Double-click header (outside interactive targets) toggles window maximize. */
    onToggleMaximize?: () => void;
    theme: CodePreviewTheme;
    lang?: string;
    /** Cloud workspace: file tree is remote content, not a local folder. */
    cloudMode?: boolean;
    /** Known Hub workspace name. Entitlement lookup overrides this when it matches the mount. */
    cloudWorkspaceName?: string;
    /** Preview pane already has a close control; hide the inner header X. */
    hideHeaderClose?: boolean;
}

/** Skip maximize when double-clicking interactive header controls / tab bar. */
function isPreviewHeaderInteractiveTarget(target: EventTarget | null, currentTarget: HTMLElement): boolean {
    if (!(target instanceof HTMLElement) || target === currentTarget) return false;
    return !!target.closest('button, a, input, select, textarea, [role="button"], [role="tab"], [data-preview-no-maximize="true"]');
}

function CloudWorkspaceNameLabel({ name, theme, compact = false }: { name: string; theme: CodePreviewTheme; compact?: boolean }) {
    const text = name.trim();
    if (!text) return null;
    return (
        <span
            data-testid="code-preview-cloud-workspace-name"
            title={text}
            style={{
                color: theme.textMuted,
                fontSize: 12,
                fontWeight: 400,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                minWidth: 0,
                flex: compact ? '0 1 auto' : 1,
                maxWidth: compact ? 160 : undefined,
                marginLeft: compact ? 6 : undefined,
            }}
        >
            {text}
        </span>
    );
}

/** True when the file should render with the markdown preview (case-insensitive). */
function isMarkdownLanguage(language: string | undefined | null): boolean {
    const key = (language || '').trim().toLowerCase();
    return key === 'markdown' || key === 'md';
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

/** Stable empty match list — avoids child re-renders when find is closed. */
const EMPTY_MATCH_LINE_INDEXES: number[] = [];

const HighlightedLine = React.memo(function HighlightedLine({ line, language, theme }: {
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
});
// ── Plain Code View ──

const PlainCodeView = React.memo(function PlainCodeView({
    content,
    language,
    theme,
    matchLineIndexes = EMPTY_MATCH_LINE_INDEXES,
    activeMatchLine = -1,
    wordWrap = false,
    fontSize = CODE_PREVIEW_FONT_DEFAULT,
}: {
    content: string;
    language: string;
    theme: CodePreviewTheme;
    matchLineIndexes?: number[];
    activeMatchLine?: number;
    wordWrap?: boolean;
    fontSize?: number;
}) {
    // Same split semantics as the panel's contentLines ('' → one empty visual line).
    const lines = useMemo(() => content.split('\n'), [content]);
    const matchSet = useMemo(() => new Set(matchLineIndexes), [matchLineIndexes]);
    const size = clampCodePreviewFontSize(fontSize);
    const lineHeight = codePreviewLineHeight(size);

    return (
        <table
            data-testid="code-preview-plain-view"
            data-word-wrap={wordWrap ? 'true' : 'false'}
            data-font-size={String(size)}
            style={{
                borderCollapse: 'collapse',
                width: '100%',
                fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace",
                fontSize: size,
                lineHeight: `${lineHeight}px`,
            }}
        >
            <tbody>
                {lines.map((line, idx) => {
                    const isMatch = matchSet.has(idx);
                    const isActiveMatch = idx === activeMatchLine;
                    const rowBg = isActiveMatch
                        ? 'rgba(234, 179, 8, 0.28)'
                        : isMatch
                            ? 'rgba(234, 179, 8, 0.12)'
                            : undefined;
                    return (
                        <tr
                            key={idx}
                            data-line={idx + 1}
                            data-find-match={isMatch ? 'true' : undefined}
                            data-find-active={isActiveMatch ? 'true' : undefined}
                            style={{ backgroundColor: rowBg }}
                        >
                            <td style={{
                                width: 50,
                                minWidth: 50,
                                textAlign: 'right',
                                paddingRight: 12,
                                paddingLeft: 8,
                                color: theme.lineNumText,
                                backgroundColor: rowBg ?? theme.lineNumBg,
                                userSelect: 'none',
                                verticalAlign: 'top',
                                borderRight: `1px solid ${theme.border}`,
                            }}>
                                {idx + 1}
                            </td>
                            <td style={{
                                paddingLeft: 12,
                                paddingRight: 8,
                                whiteSpace: wordWrap ? 'pre-wrap' : 'pre',
                                wordBreak: wordWrap ? 'break-word' : undefined,
                                overflowWrap: wordWrap ? 'anywhere' : undefined,
                                color: theme.text,
                                textAlign: 'left',
                            }}>
                                <HighlightedLine line={line} language={language} theme={theme} />
                            </td>
                        </tr>
                    );
                })}
            </tbody>
        </table>
    );
});

// ── Find bar ──

function CodePreviewFindBar({
    query,
    matchCount,
    activeIndex,
    theme,
    lang,
    caseSensitive,
    wholeWord,
    useRegex,
    regexError,
    onQueryChange,
    onToggleCase,
    onToggleWord,
    onToggleRegex,
    onNext,
    onPrev,
    onClose,
    inputRef,
}: {
    query: string;
    matchCount: number;
    activeIndex: number;
    theme: CodePreviewTheme;
    lang: string;
    caseSensitive: boolean;
    wholeWord: boolean;
    useRegex: boolean;
    regexError: string | null;
    onQueryChange: (q: string) => void;
    onToggleCase: () => void;
    onToggleWord: () => void;
    onToggleRegex: () => void;
    onNext: () => void;
    onPrev: () => void;
    onClose: () => void;
    inputRef: React.RefObject<HTMLInputElement | null>;
}) {
    const isZh = lang.startsWith('zh');
    const isZhHant = lang === 'zh-Hant';
    const counter = regexError
        ? (isZh ? '正则无效' : 'Invalid regex')
        : matchCount === 0
            ? (isZh ? '无结果' : 'No results')
            : `${Math.min(activeIndex + 1, matchCount)} / ${matchCount}`;

    const optBtn = (active: boolean): React.CSSProperties => ({
        border: `1px solid ${active ? theme.tabActiveText : theme.border}`,
        background: active ? theme.tabActiveBg : theme.bg,
        color: active ? theme.tabActiveText : theme.textMuted,
        borderRadius: 4,
        padding: '2px 6px',
        cursor: 'pointer',
        fontSize: 11,
        fontWeight: active ? 700 : 500,
        fontFamily: 'inherit',
        lineHeight: '18px',
        flexShrink: 0,
    });

    return (
        <div
            data-testid="code-preview-find-bar"
            style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 4,
                padding: '4px 10px 6px',
                borderBottom: `1px solid ${theme.border}`,
                background: theme.tabBg,
                flexShrink: 0,
            }}
        >
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                <input
                    ref={inputRef as React.RefObject<HTMLInputElement>}
                    data-testid="code-preview-find-input"
                    type="text"
                    value={query}
                    onChange={(e) => onQueryChange(e.target.value)}
                    placeholder={isZhHant ? '在檔案中尋找…' : isZh ? '在文件中查找…' : 'Find in file…'}
                    style={{
                        flex: 1,
                        minWidth: 0,
                        border: `1px solid ${regexError ? theme.diffDeleteText : theme.border}`,
                        borderRadius: 4,
                        padding: '4px 8px',
                        fontSize: 12,
                        background: theme.bg,
                        color: theme.text,
                        outline: 'none',
                        fontFamily: 'inherit',
                    }}
                />
                <span
                    data-testid="code-preview-find-count"
                    style={{
                        fontSize: 11,
                        color: regexError ? theme.diffDeleteText : theme.textMuted,
                        whiteSpace: 'nowrap',
                        minWidth: 64,
                        textAlign: 'right',
                    }}
                >
                    {counter}
                </span>
                <button
                    type="button"
                    data-testid="code-preview-find-case"
                    data-active={caseSensitive ? 'true' : 'false'}
                    onClick={onToggleCase}
                    title={isZh ? '区分大小写' : 'Match Case'}
                    style={optBtn(caseSensitive)}
                >
                    Aa
                </button>
                <button
                    type="button"
                    data-testid="code-preview-find-word"
                    data-active={wholeWord ? 'true' : 'false'}
                    onClick={onToggleWord}
                    title={isZh ? '全词匹配' : 'Match Whole Word'}
                    style={optBtn(wholeWord)}
                >
                    W
                </button>
                <button
                    type="button"
                    data-testid="code-preview-find-regex"
                    data-active={useRegex ? 'true' : 'false'}
                    onClick={onToggleRegex}
                    title={isZh ? '使用正则表达式' : 'Use Regular Expression'}
                    style={optBtn(useRegex)}
                >
                    {'.*'}
                </button>
                <button
                    type="button"
                    data-testid="code-preview-find-prev"
                    onClick={onPrev}
                    title={isZh ? '上一个 (Shift+Enter)' : 'Previous (Shift+Enter)'}
                    style={{
                        border: `1px solid ${theme.border}`,
                        background: theme.bg,
                        color: theme.text,
                        borderRadius: 4,
                        padding: '2px 8px',
                        cursor: 'pointer',
                        fontSize: 12,
                    }}
                >
                    ↑
                </button>
                <button
                    type="button"
                    data-testid="code-preview-find-next"
                    onClick={onNext}
                    title={isZh ? '下一个 (Enter)' : 'Next (Enter)'}
                    style={{
                        border: `1px solid ${theme.border}`,
                        background: theme.bg,
                        color: theme.text,
                        borderRadius: 4,
                        padding: '2px 8px',
                        cursor: 'pointer',
                        fontSize: 12,
                    }}
                >
                    ↓
                </button>
                <button
                    type="button"
                    data-testid="code-preview-find-close"
                    onClick={onClose}
                    title="Esc"
                    style={{
                        border: 'none',
                        background: 'transparent',
                        color: theme.textMuted,
                        borderRadius: 4,
                        padding: '2px 6px',
                        cursor: 'pointer',
                        fontSize: 14,
                    }}
                >
                    {'\u00d7'}
                </button>
            </div>
            {regexError && (
                <div
                    data-testid="code-preview-find-regex-error"
                    style={{ fontSize: 11, color: theme.diffDeleteText, lineHeight: 1.3 }}
                >
                    {regexError}
                </div>
            )}
        </div>
    );
}

// ── Diff View ──

const DiffView = React.memo(function DiffView({
    diffLines,
    theme,
    matchLineIndexes = EMPTY_MATCH_LINE_INDEXES,
    activeMatchLine = -1,
    wordWrap = false,
    fontSize = CODE_PREVIEW_FONT_DEFAULT,
}: {
    diffLines: DiffLine[];
    theme: CodePreviewTheme;
    /** 0-based line indexes in the *new* file content. */
    matchLineIndexes?: number[];
    activeMatchLine?: number;
    wordWrap?: boolean;
    fontSize?: number;
}) {
    const matchSet = useMemo(() => new Set(matchLineIndexes), [matchLineIndexes]);
    const size = clampCodePreviewFontSize(fontSize);
    const lineHeight = codePreviewLineHeight(size);

    return (
        <table
            data-testid="code-preview-diff-view"
            data-word-wrap={wordWrap ? 'true' : 'false'}
            data-font-size={String(size)}
            style={{
                borderCollapse: 'collapse',
                width: '100%',
                fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace",
                fontSize: size,
                lineHeight: `${lineHeight}px`,
            }}
        >
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

                    // Find highlights target new-file lines (add / unchanged).
                    const contentLineIdx = dl.newLineNum != null ? dl.newLineNum - 1 : -1;
                    const isMatch = contentLineIdx >= 0 && matchSet.has(contentLineIdx);
                    const isActiveMatch = contentLineIdx >= 0 && contentLineIdx === activeMatchLine;
                    if (isActiveMatch) {
                        rowBg = 'rgba(234, 179, 8, 0.28)';
                    } else if (isMatch) {
                        rowBg = 'rgba(234, 179, 8, 0.12)';
                    }

                    return (
                        <tr
                            key={idx}
                            data-line={dl.newLineNum ?? undefined}
                            data-diff-idx={idx}
                            data-find-match={isMatch ? 'true' : undefined}
                            data-find-active={isActiveMatch ? 'true' : undefined}
                            style={{ backgroundColor: rowBg }}
                        >
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
                                whiteSpace: wordWrap ? 'pre-wrap' : 'pre',
                                wordBreak: wordWrap ? 'break-word' : undefined,
                                overflowWrap: wordWrap ? 'anywhere' : undefined,
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
});

/** Compact view toolbar: wrap + font zoom. */
function CodePreviewViewToolbar({
    wordWrap,
    fontSize,
    theme,
    lang,
    onToggleWrap,
    onZoomIn,
    onZoomOut,
    onZoomReset,
}: {
    wordWrap: boolean;
    fontSize: number;
    theme: CodePreviewTheme;
    lang: string;
    onToggleWrap: () => void;
    onZoomIn: () => void;
    onZoomOut: () => void;
    onZoomReset: () => void;
}) {
    const isZh = lang.startsWith('zh');
    const isZhHant = lang === 'zh-Hant';
    const btnStyle: React.CSSProperties = {
        border: `1px solid ${theme.border}`,
        background: theme.bg,
        color: theme.textMuted,
        borderRadius: 4,
        padding: '1px 6px',
        cursor: 'pointer',
        fontSize: 11,
        lineHeight: '18px',
        fontFamily: 'inherit',
        flexShrink: 0,
    };
    return (
        <div
            data-testid="code-preview-view-toolbar"
            data-preview-no-maximize="true"
            style={{
                display: 'flex',
                alignItems: 'center',
                gap: 4,
                marginLeft: 4,
                flexShrink: 0,
            }}
        >
            <button
                type="button"
                data-testid="code-preview-wrap-toggle"
                data-active={wordWrap ? 'true' : 'false'}
                onClick={onToggleWrap}
                title={isZhHant ? '自動換行 (Alt+Z)' : isZh ? '自动换行 (Alt+Z)' : 'Toggle Word Wrap (Alt+Z)'}
                style={{
                    ...btnStyle,
                    background: wordWrap ? theme.tabActiveBg : theme.bg,
                    color: wordWrap ? theme.tabActiveText : theme.textMuted,
                    fontWeight: wordWrap ? 600 : 400,
                }}
            >
                {isZh ? '换行' : 'Wrap'}
            </button>
            <button
                type="button"
                data-testid="code-preview-zoom-out"
                onClick={onZoomOut}
                disabled={fontSize <= CODE_PREVIEW_FONT_MIN}
                title={isZh ? '缩小 (Ctrl+-)' : 'Zoom Out (Ctrl+-)'}
                style={{
                    ...btnStyle,
                    opacity: fontSize <= CODE_PREVIEW_FONT_MIN ? 0.45 : 1,
                    cursor: fontSize <= CODE_PREVIEW_FONT_MIN ? 'default' : 'pointer',
                }}
            >
                A-
            </button>
            <button
                type="button"
                data-testid="code-preview-zoom-reset"
                onClick={onZoomReset}
                title={isZh ? '重置字号 (Ctrl+0)' : 'Reset Zoom (Ctrl+0)'}
                style={{ ...btnStyle, minWidth: 36 }}
            >
                {fontSize}
            </button>
            <button
                type="button"
                data-testid="code-preview-zoom-in"
                onClick={onZoomIn}
                disabled={fontSize >= CODE_PREVIEW_FONT_MAX}
                title={isZh ? '放大 (Ctrl+=)' : 'Zoom In (Ctrl+=)'}
                style={{
                    ...btnStyle,
                    opacity: fontSize >= CODE_PREVIEW_FONT_MAX ? 0.45 : 1,
                    cursor: fontSize >= CODE_PREVIEW_FONT_MAX ? 'default' : 'pointer',
                }}
            >
                A+
            </button>
        </div>
    );
}

// ── Go to line bar ──

function CodePreviewGoToLineBar({
    value,
    maxLines,
    theme,
    lang,
    onChange,
    onSubmit,
    onClose,
    inputRef,
}: {
    value: string;
    maxLines: number;
    theme: CodePreviewTheme;
    lang: string;
    onChange: (v: string) => void;
    onSubmit: () => void;
    onClose: () => void;
    inputRef: React.RefObject<HTMLInputElement | null>;
}) {
    const isZh = lang.startsWith('zh');
    const isZhHant = lang === 'zh-Hant';
    return (
        <div
            data-testid="code-preview-goto-bar"
            style={{
                display: 'flex',
                alignItems: 'center',
                gap: 6,
                padding: '4px 10px',
                borderBottom: `1px solid ${theme.border}`,
                background: theme.tabBg,
                flexShrink: 0,
            }}
        >
            <span style={{ fontSize: 12, color: theme.textMuted, whiteSpace: 'nowrap' }}>
                {isZhHant ? '前往行' : isZh ? '转到行' : 'Go to Line'}
            </span>
            <input
                ref={inputRef as React.RefObject<HTMLInputElement>}
                data-testid="code-preview-goto-input"
                type="text"
                inputMode="numeric"
                value={value}
                onChange={(e) => onChange(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                        e.preventDefault();
                        onSubmit();
                    }
                }}
                placeholder={isZh ? `1–${maxLines}` : `1–${maxLines}`}
                style={{
                    width: 96,
                    border: `1px solid ${theme.border}`,
                    borderRadius: 4,
                    padding: '4px 8px',
                    fontSize: 12,
                    background: theme.bg,
                    color: theme.text,
                    outline: 'none',
                    fontFamily: 'inherit',
                }}
            />
            <span style={{ fontSize: 11, color: theme.textMuted, flex: 1 }}>
                {isZhHant ? `共 ${maxLines} 行` : isZh ? `共 ${maxLines} 行` : `${maxLines} lines`}
            </span>
            <button
                type="button"
                data-testid="code-preview-goto-go"
                onClick={onSubmit}
                style={{
                    border: `1px solid ${theme.border}`,
                    background: theme.bg,
                    color: theme.text,
                    borderRadius: 4,
                    padding: '2px 10px',
                    cursor: 'pointer',
                    fontSize: 12,
                }}
            >
                {isZh ? '跳转' : 'Go'}
            </button>
            <button
                type="button"
                data-testid="code-preview-goto-close"
                onClick={onClose}
                title="Esc"
                style={{
                    border: 'none',
                    background: 'transparent',
                    color: theme.textMuted,
                    borderRadius: 4,
                    padding: '2px 6px',
                    cursor: 'pointer',
                    fontSize: 14,
                }}
            >
                {'\u00d7'}
            </button>
        </div>
    );
}

// ── Main Component ──

export function CodePreviewPanel({
    files,
    activeFilePath,
    pinnedPaths,
    mruOrder,
    onSelectFile,
    projectPath,
    workspaceRefreshToken,
    workspaceResetOnRefresh = false,
    onOpenWorkspaceFile,
    onCloseFile,
    onCloseOtherFiles,
    onCloseFilesToTheRight,
    onCloseAllFiles,
    onMoveFile,
    onTogglePinFile,
    onClose,
    onResizeStart,
    onToggleMaximize,
    theme,
    lang = 'en',
    cloudMode = false,
    cloudWorkspaceName = '',
    hideHeaderClose = false,
}: CodePreviewPanelProps) {
    const cloudWorkspaceId = cloudMode ? cloudWorkspaceIdFromPath(projectPath) : '';
    const [resolvedCloudName, setResolvedCloudName] = useState(() => (
        cloudMode ? lookupCloudWorkspaceDisplayName(cloudWorkspaceIdFromPath(projectPath), cloudWorkspaceName) : ''
    ));
    useEffect(() => {
        if (!cloudMode) {
            setResolvedCloudName('');
            return;
        }
        const known = lookupCloudWorkspaceDisplayName(cloudWorkspaceId, cloudWorkspaceName);
        setResolvedCloudName(known);
        if (!cloudWorkspaceId || typeof CloudWorkspaceEntitlement !== 'function') return;
        let cancelled = false;
        void CloudWorkspaceEntitlement().then((ent) => {
            if (cancelled) return;
            rememberCloudWorkspaceDisplayNames(ent);
            const name = lookupCloudWorkspaceDisplayName(cloudWorkspaceId, known);
            if (name) setResolvedCloudName(name);
        }).catch(() => {});
        return () => { cancelled = true; };
    }, [cloudMode, cloudWorkspaceId, cloudWorkspaceName]);
    // Every source-preview opening starts with the project tree. Source files
    // remain open beside it, but never replace the confirmation that a local
    // or remote working directory is available.
    const [workspaceActive, setWorkspaceActive] = useState(() => Boolean(projectPath));
    const handleHeaderDoubleClick = (event: React.MouseEvent<HTMLElement>) => {
        if (isPreviewHeaderInteractiveTarget(event.target, event.currentTarget)) return;
        onToggleMaximize?.();
    };
    const scrollRef = useRef<HTMLDivElement>(null);
    const findInputRef = useRef<HTMLInputElement>(null);
    const gotoInputRef = useRef<HTMLInputElement>(null);
    const savedScrollTop = useRef<number>(0);
    const prevContentRef = useRef<string>('');
    const scrollFlashTimerRef = useRef<number | null>(null);
    const scrollFlashElRef = useRef<HTMLElement | null>(null);
    const scrollFlashPrevOutlineRef = useRef({ outline: '', offset: '' });
    const gotoRafRef = useRef<number | null>(null);
    const focusRafRef = useRef<number | null>(null);
    /** Bumps to invalidate in-flight double-rAF focus chains (cancel only kills one id). */
    const focusGenRef = useRef(0);
    const lastScrolledLineRef = useRef<number | null>(null);

    const [findOpen, setFindOpen] = useState(false);
    const [findQuery, setFindQuery] = useState('');
    const [findIndex, setFindIndex] = useState(0);
    const [findCaseSensitive, setFindCaseSensitive] = useState(false);
    const [findWholeWord, setFindWholeWord] = useState(false);
    const [findUseRegex, setFindUseRegex] = useState(false);
    const [gotoOpen, setGotoOpen] = useState(false);
    const [gotoValue, setGotoValue] = useState('');
    // Track which file the find/goto UI state belongs to so a tab switch can
    // reset during render (avoids one paint with the previous file's find index).
    const [findUiFilePath, setFindUiFilePath] = useState(activeFilePath);
    if (findUiFilePath !== activeFilePath) {
        setFindUiFilePath(activeFilePath);
        setFindIndex(0);
        setGotoOpen(false);
        setGotoValue('');
    }
    // Single localStorage read for both fields (shared across the two useState inits).
    const initialViewPrefsRef = useRef<CodePreviewViewPrefs | null>(null);
    if (initialViewPrefsRef.current === null) {
        initialViewPrefsRef.current = loadCodePreviewViewPrefs();
    }
    const [wordWrap, setWordWrap] = useState(() => initialViewPrefsRef.current!.wordWrap);
    const [fontSize, setFontSize] = useState(() => initialViewPrefsRef.current!.fontSize);
    const skipNextPrefsSaveRef = useRef(true);

    // Persist view prefs when wrap/font change; skip the mount effect write-back.
    useEffect(() => {
        if (skipNextPrefsSaveRef.current) {
            skipNextPrefsSaveRef.current = false;
            return;
        }
        saveCodePreviewViewPrefs({ wordWrap, fontSize });
    }, [wordWrap, fontSize]);

    const activeFile = files.get(activeFilePath);

    useEffect(() => {
        if (!projectPath) return;
        if (cloudMode && activeFilePath && files.has(activeFilePath)) return;
        setWorkspaceActive(true);
    }, [projectPath, cloudMode, activeFilePath, files]);
    useEffect(() => {
        if (!cloudMode) return;
        const focusTree = () => setWorkspaceActive(true);
        window.addEventListener(FOCUS_CLOUD_WORKSPACE_TREE_EVENT, focusTree);
        return () => window.removeEventListener(FOCUS_CLOUD_WORKSPACE_TREE_EVENT, focusTree);
    }, [cloudMode]);

    useEffect(() => {
        if (!activeFilePath || !files.has(activeFilePath)) setWorkspaceActive(true);
    }, [activeFilePath, files]);

    const openWorkspaceFile = useCallback((file: CodeFile) => {
        onOpenWorkspaceFile?.(file);
        if (!onOpenWorkspaceFile) onSelectFile(file.filePath);
        setWorkspaceActive(false);
    }, [onOpenWorkspaceFile, onSelectFile]);

    const handleSelectFile = useCallback((filePath: string) => {
        setWorkspaceActive(false);
        onSelectFile(filePath);
    }, [onSelectFile]);

    // Compute diff lines when active file has original content
    const diffLines = useMemo<DiffLine[] | null>(() => {
        // A truncated remote preview only contains a leading chunk. Computing a
        // diff against it would imply changes across the unseen remainder.
        if (activeFile?.original === undefined || activeFile.previewTruncated) return null;
        return computeDiff(activeFile.original, activeFile.content);
    }, [activeFile?.original, activeFile?.content, activeFile?.previewTruncated]);

    const currentContent = activeFile?.content ?? '';
    // Split once; shared by line count, find scan, and go-to-line bounds.
    // Keep editor semantics: '' is still one empty visual line (matches PlainCodeView).
    const contentLines = useMemo(() => currentContent.split('\n'), [currentContent]);
    const totalLines = activeFile ? contentLines.length : 0;

    const findOptions = useMemo<FindMatchOptions>(() => ({
        caseSensitive: findCaseSensitive,
        wholeWord: findWholeWord,
        useRegex: findUseRegex,
    }), [findCaseSensitive, findUseRegex, findWholeWord]);

    // Compile once per query/options change; reuse for error display + line scan.
    const findCompiled = useMemo(
        () => (findOpen ? compileFindMatcher(findQuery, findOptions) : null),
        [findOpen, findOptions, findQuery],
    );

    const findRegexError = findCompiled && !findCompiled.ok ? findCompiled.error : null;

    const matchLineIndexes = useMemo(() => {
        if (!findOpen || !findCompiled || !findCompiled.ok) return EMPTY_MATCH_LINE_INDEXES;
        // Skip full-file scan when the query is effectively empty.
        const hasQuery = findUseRegex ? findQuery.length > 0 : findQuery.trim().length > 0;
        if (!hasQuery) return EMPTY_MATCH_LINE_INDEXES;
        const matches: number[] = [];
        for (let i = 0; i < contentLines.length; i++) {
            if (findCompiled.test(contentLines[i])) matches.push(i);
        }
        return matches.length === 0 ? EMPTY_MATCH_LINE_INDEXES : matches;
    }, [contentLines, findCompiled, findOpen, findQuery, findUseRegex]);

    const activeMatchLine = matchLineIndexes.length > 0
        ? matchLineIndexes[Math.min(Math.max(findIndex, 0), matchLineIndexes.length - 1)]
        : -1;

    // Keep find index in range when matches change (clamp to last, not jump to first).
    useEffect(() => {
        if (matchLineIndexes.length === 0) {
            if (findIndex !== 0) setFindIndex(0);
            return;
        }
        if (findIndex >= matchLineIndexes.length) {
            setFindIndex(matchLineIndexes.length - 1);
        }
    }, [findIndex, matchLineIndexes.length]);

    const clearScrollFlash = useCallback(() => {
        if (scrollFlashTimerRef.current != null) {
            window.clearTimeout(scrollFlashTimerRef.current);
            scrollFlashTimerRef.current = null;
        }
        const prevEl = scrollFlashElRef.current;
        if (prevEl) {
            prevEl.style.outline = scrollFlashPrevOutlineRef.current.outline;
            prevEl.style.outlineOffset = scrollFlashPrevOutlineRef.current.offset;
            scrollFlashElRef.current = null;
        }
    }, []);

    const scrollToLine = useCallback((line1Based: number, opts?: { force?: boolean; flash?: boolean }) => {
        if (!scrollRef.current || line1Based < 1) return;
        // Prefer exact data-line (code/diff). Fall back to markdown blocks spanning the line.
        let row: Element | null = scrollRef.current.querySelector(`[data-line="${line1Based}"]`);
        if (!row) {
            const blocks = scrollRef.current.querySelectorAll('[data-line-start]');
            for (const el of blocks) {
                const start = Number(el.getAttribute('data-line-start'));
                const end = Number(el.getAttribute('data-line-end') || start);
                if (Number.isFinite(start) && Number.isFinite(end) && line1Based >= start && line1Based <= end) {
                    row = el;
                    break;
                }
            }
        }
        if (row instanceof HTMLElement) {
            // Skip redundant scroll when already on this line (e.g. re-render with same match).
            if (!opts?.force && lastScrolledLineRef.current === line1Based) {
                return;
            }
            lastScrolledLineRef.current = line1Based;
            // 'auto' avoids stacked smooth-scroll animations when hopping matches quickly.
            row.scrollIntoView({ block: 'center', behavior: 'auto' });
            if (opts?.flash === false) return;
            // Restore any previous flash before applying a new one.
            clearScrollFlash();
            scrollFlashPrevOutlineRef.current = {
                outline: row.style.outline,
                offset: row.style.outlineOffset,
            };
            scrollFlashElRef.current = row;
            row.style.outline = '2px solid rgba(234, 179, 8, 0.85)';
            row.style.outlineOffset = '-2px';
            scrollFlashTimerRef.current = window.setTimeout(() => {
                clearScrollFlash();
            }, 900);
        }
    }, [clearScrollFlash]);

    // Clear scroll flash / rAF on unmount.
    useEffect(() => {
        return () => {
            clearScrollFlash();
            focusGenRef.current += 1;
            if (gotoRafRef.current != null) {
                window.cancelAnimationFrame(gotoRafRef.current);
                gotoRafRef.current = null;
            }
            if (focusRafRef.current != null) {
                window.cancelAnimationFrame(focusRafRef.current);
                focusRafRef.current = null;
            }
        };
    }, [clearScrollFlash]);

    // Scroll active find match into view.
    useEffect(() => {
        if (!findOpen || activeMatchLine < 0) return;
        scrollToLine(activeMatchLine + 1, { flash: false });
    }, [activeMatchLine, findOpen, activeFilePath, scrollToLine]);

    // Side effects on tab switch (state already reset during render above).
    useEffect(() => {
        lastScrolledLineRef.current = null;
        clearScrollFlash();
        if (gotoRafRef.current != null) {
            window.cancelAnimationFrame(gotoRafRef.current);
            gotoRafRef.current = null;
        }
    }, [activeFilePath, clearScrollFlash]);

    const scheduleFocus = useCallback((
        getEl: () => HTMLElement | null | undefined,
        opts?: { select?: boolean },
    ) => {
        // Invalidate any prior double-rAF chain (cancelAnimationFrame only cancels one id).
        const gen = ++focusGenRef.current;
        if (focusRafRef.current != null) {
            window.cancelAnimationFrame(focusRafRef.current);
            focusRafRef.current = null;
        }
        // Double rAF: first after setState schedule, second after the find/goto bar paints.
        focusRafRef.current = window.requestAnimationFrame(() => {
            if (gen !== focusGenRef.current) return;
            focusRafRef.current = window.requestAnimationFrame(() => {
                if (gen !== focusGenRef.current) return;
                focusRafRef.current = null;
                const el = getEl();
                el?.focus();
                if (opts?.select && el instanceof HTMLInputElement) {
                    el.select();
                }
            });
        });
    }, []);

    const openFind = useCallback(() => {
        setGotoOpen(false);
        setFindOpen(true);
        // Select existing query so the next keystroke can replace it (VS Code-like).
        scheduleFocus(() => findInputRef.current, { select: true });
    }, [scheduleFocus]);

    const closeFind = useCallback(() => {
        setFindOpen(false);
    }, []);

    const openGoto = useCallback(() => {
        setFindOpen(false);
        setGotoOpen(true);
        setGotoValue('');
        scheduleFocus(() => gotoInputRef.current);
    }, [scheduleFocus]);

    const closeGoto = useCallback(() => {
        setGotoOpen(false);
        setGotoValue('');
    }, []);

    const submitGoto = useCallback(() => {
        const line = parseGoToLineInput(gotoValue, totalLines);
        if (line == null) return;
        closeGoto();
        // Deferred jump after the goto bar unmounts so layout is stable.
        if (gotoRafRef.current != null) {
            window.cancelAnimationFrame(gotoRafRef.current);
        }
        gotoRafRef.current = window.requestAnimationFrame(() => {
            gotoRafRef.current = null;
            scrollToLine(line, { force: true, flash: true });
        });
    }, [closeGoto, gotoValue, scrollToLine, totalLines]);

    const goNextMatch = useCallback(() => {
        if (matchLineIndexes.length === 0) return;
        setFindIndex((i) => cycleMatchIndex(matchLineIndexes.length, i, 1));
    }, [matchLineIndexes.length]);

    const goPrevMatch = useCallback(() => {
        if (matchLineIndexes.length === 0) return;
        setFindIndex((i) => cycleMatchIndex(matchLineIndexes.length, i, -1));
    }, [matchLineIndexes.length]);

    const toggleWordWrap = useCallback(() => {
        setWordWrap((v) => !v);
    }, []);

    const zoomIn = useCallback(() => {
        setFontSize((s) => clampCodePreviewFontSize(s + 1));
    }, []);

    const zoomOut = useCallback(() => {
        setFontSize((s) => clampCodePreviewFontSize(s - 1));
    }, []);

    const zoomReset = useCallback(() => {
        setFontSize(CODE_PREVIEW_FONT_DEFAULT);
    }, []);

    const handlePanelKeyDown = useCallback((e: React.KeyboardEvent) => {
        // Alt+Z — toggle word wrap (VS Code)
        if (e.altKey && !e.ctrlKey && !e.metaKey && (e.key === 'z' || e.key === 'Z')) {
            e.preventDefault();
            e.stopPropagation();
            toggleWordWrap();
            return;
        }
        // Ctrl/Cmd + = / + — zoom in; - — zoom out; 0 — reset
        if ((e.ctrlKey || e.metaKey) && !e.altKey) {
            if (e.key === '=' || e.key === '+') {
                e.preventDefault();
                e.stopPropagation();
                zoomIn();
                return;
            }
            if (e.key === '-' || e.key === '_') {
                e.preventDefault();
                e.stopPropagation();
                zoomOut();
                return;
            }
            if (e.key === '0') {
                e.preventDefault();
                e.stopPropagation();
                zoomReset();
                return;
            }
            // Ctrl/Cmd+Tab — MRU cycle even when focus is in the content/find area
            // (FileTabBar handles the same shortcut when the tab bar itself is focused).
            if (e.key === 'Tab') {
                e.preventDefault();
                e.stopPropagation();
                if (files.size > 0) {
                    const order = getMruCycleOrder(files, mruOrder ?? []);
                    const next = cycleFilePath(order, activeFilePath, e.shiftKey ? -1 : 1);
                    if (next) handleSelectFile(next);
                }
                return;
            }
            // Ctrl/Cmd+W — close active tab
            if ((e.key === 'w' || e.key === 'W') && onCloseFile && activeFilePath) {
                e.preventDefault();
                e.stopPropagation();
                onCloseFile(activeFilePath);
                return;
            }
        }
        // Ctrl/Cmd+F — open find bar
        if ((e.ctrlKey || e.metaKey) && !e.altKey && (e.key === 'f' || e.key === 'F')) {
            e.preventDefault();
            e.stopPropagation();
            openFind();
            return;
        }
        // Ctrl/Cmd+G — go to line
        if ((e.ctrlKey || e.metaKey) && !e.altKey && (e.key === 'g' || e.key === 'G')) {
            e.preventDefault();
            e.stopPropagation();
            openGoto();
            return;
        }
        if (e.key === 'Escape') {
            if (findOpen) {
                e.preventDefault();
                closeFind();
                return;
            }
            if (gotoOpen) {
                e.preventDefault();
                closeGoto();
                return;
            }
        }
        if (!findOpen) return;
        if (e.key === 'Enter' && (e.target as HTMLElement)?.closest?.('[data-testid="code-preview-find-bar"]')) {
            e.preventDefault();
            if (e.shiftKey) goPrevMatch();
            else goNextMatch();
            return;
        }
        if (e.key === 'F3') {
            e.preventDefault();
            if (e.shiftKey) goPrevMatch();
            else goNextMatch();
        }
    }, [
        activeFilePath,
        closeFind,
        closeGoto,
        files,
        findOpen,
        goNextMatch,
        goPrevMatch,
        gotoOpen,
        mruOrder,
        onCloseFile,
        handleSelectFile,
        openFind,
        openGoto,
        toggleWordWrap,
        zoomIn,
        zoomOut,
        zoomReset,
    ]);

    // Preserve scroll across same-file content updates (streaming); reset on tab switch.
    const prevActiveFilePathRef = useRef(activeFilePath);
    useLayoutEffect(() => {
        const el = scrollRef.current;
        if (!el) return;

        const fileSwitched = prevActiveFilePathRef.current !== activeFilePath;
        if (fileSwitched) {
            prevActiveFilePathRef.current = activeFilePath;
            prevContentRef.current = currentContent;
            savedScrollTop.current = 0;
            el.scrollTop = 0;
        } else if (currentContent !== prevContentRef.current) {
            // Same file content changed — restore the pre-update position.
            el.scrollTop = savedScrollTop.current;
            prevContentRef.current = currentContent;
        }

        // Always install cleanup so post-switch streaming still preserves user scroll.
        return () => {
            if (scrollRef.current) {
                savedScrollTop.current = scrollRef.current.scrollTop;
            }
        };
    }, [activeFilePath, currentContent]);

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
                        onDoubleClick={handleHeaderDoubleClick}
                        title={onToggleMaximize ? (lang.startsWith('zh') ? '双击最大化/还原' : 'Double-click to maximize/restore') : undefined}
                        style={{
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            padding: '8px 14px',
                            borderBottom: `1px solid ${theme.border}`,
                            background: theme.tabBg,
                            flexShrink: 0,
                            '--wails-draggable': 'no-drag',
                        } as any}
                    >
                        <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flex: 1, marginRight: 8 }}>
                            <span style={{ color: theme.tabActiveText, fontSize: 12, fontWeight: 600, flexShrink: 0 }}>
                                {cloudMode ? (lang.startsWith('zh') ? '云端工作区' : 'Cloud workspace') : (lang.startsWith('zh') ? '工作目录' : 'Working directory')}
                            </span>
                            {cloudMode && resolvedCloudName ? (
                                <>
                                    <span aria-hidden="true" style={{ color: theme.textMuted, flexShrink: 0 }}>·</span>
                                    <CloudWorkspaceNameLabel name={resolvedCloudName} theme={theme} />
                                </>
                            ) : null}
                        </span>
                        {!hideHeaderClose ? (
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
                        ) : null}
                    </div>
                    <CodePreviewWorkspace projectPath={projectPath} refreshToken={workspaceRefreshToken} resetOnRefresh={workspaceResetOnRefresh} cloudMode={cloudMode} lang={lang} theme={theme} onOpenFile={openWorkspaceFile} onFileDeleted={(path) => { onCloseFile?.(path); for (const filePath of Array.from(files.keys())) { if (filePath !== path && (filePath.startsWith(`${path}/`) || filePath.startsWith(`${path}\\`))) onCloseFile?.(filePath); } }} />
                </div>
            </div>
        );
    }

    return (
        <div
            data-testid="code-preview-panel"
            tabIndex={0}
            onKeyDown={handlePanelKeyDown}
            style={{
                display: 'flex',
                flexDirection: 'row',
                height: '100%',
                minWidth: 0,
                outline: 'none',
            }}
        >
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
            {/* Header with close button - double-click empty area to toggle maximize */}
            <div
                data-testid="code-preview-header"
                onDoubleClick={handleHeaderDoubleClick}
                title={onToggleMaximize ? (lang.startsWith('zh') ? '双击空白处最大化/还原' : 'Double-click empty area to maximize/restore') : undefined}
                style={{
                    display: 'flex',
                    alignItems: 'center',
                    padding: '4px 8px 4px 0',
                    borderBottom: `1px solid ${theme.border}`,
                    background: theme.tabBg,
                    flexShrink: 0,
                    // Allow FileTabBar open-editors dropdown to extend below the header.
                    overflow: 'visible',
                    '--wails-draggable': 'no-drag',
                } as any}
            >
                <div
                    data-preview-no-maximize="true"
                    style={{
                        flex: 1,
                        minWidth: 0,
                        // Keep open-editors dropdown paintable outside the strip.
                        overflow: 'visible',
                        '--wails-draggable': 'no-drag',
                    } as any}
                >
                    <button
                        type="button"
                        role="tab"
                        aria-selected={workspaceActive}
                        data-testid="code-preview-workspace-tab"
                        onClick={() => setWorkspaceActive(true)}
                        style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            flexShrink: 1,
                            minWidth: 0,
                            height: 36,
                            padding: '0 10px',
                            border: 'none',
                            borderRight: `1px solid ${theme.border}`,
                            borderBottom: workspaceActive ? `2px solid ${theme.tabActiveText}` : '2px solid transparent',
                            background: workspaceActive ? theme.tabActiveBg : theme.tabBg,
                            color: workspaceActive ? theme.tabActiveText : theme.textMuted,
                            cursor: 'pointer',
                            font: 'inherit',
                            fontSize: 12,
                            fontWeight: 600,
                        }}
                    >
                        {cloudMode ? (lang.startsWith('zh') ? '云端文件' : 'Cloud files') : (lang.startsWith('zh') ? '工作目录' : 'Working directory')}
                        {cloudMode ? <CloudWorkspaceNameLabel name={resolvedCloudName} theme={theme} compact /> : null}
                    </button>
                    <FileTabBar
                        files={files}
                        activeFilePath={activeFilePath}
                        pinnedPaths={pinnedPaths}
                        mruOrder={mruOrder}
                        onSelectFile={handleSelectFile}
                        onCloseFile={onCloseFile}
                        onCloseOtherFiles={onCloseOtherFiles}
                        onCloseFilesToTheRight={onCloseFilesToTheRight}
                        onCloseAllFiles={onCloseAllFiles}
                        onMoveFile={onMoveFile}
                        onTogglePinFile={onTogglePinFile}
                        theme={theme}
                        lang={lang}
                        cloudMode={cloudMode}
                    />
                </div>
                <CodePreviewViewToolbar
                    wordWrap={wordWrap}
                    fontSize={fontSize}
                    theme={theme}
                    lang={lang}
                    onToggleWrap={toggleWordWrap}
                    onZoomIn={zoomIn}
                    onZoomOut={zoomOut}
                    onZoomReset={zoomReset}
                />
                {!hideHeaderClose ? (
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
                ) : null}
            </div>

            {/* Active file path breadcrumb (VS Code-style status under tabs) */}
            {!workspaceActive && activeFile && (
                <div
                    data-testid="code-preview-active-path"
                    title={cloudMode ? activeFile.filePath : (activeFile.absPath || activeFile.filePath)}
                    style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 8,
                        padding: '3px 12px',
                        borderBottom: `1px solid ${theme.border}`,
                        background: theme.lineNumBg,
                        color: theme.textMuted,
                        fontSize: 11,
                        lineHeight: 1.4,
                        flexShrink: 0,
                        minWidth: 0,
                        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
                    }}
                >
                    <span style={{
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        minWidth: 0,
                        flex: 1,
                    }}>
                        {cloudMode ? activeFile.filePath : (activeFile.absPath || activeFile.filePath)}
                    </span>
                    <span
                        data-testid="code-preview-lang-badge"
                        style={{
                            flexShrink: 0,
                            padding: '0 6px',
                            borderRadius: 3,
                            border: `1px solid ${theme.border}`,
                            lineHeight: '16px',
                            fontSize: 10,
                            fontWeight: 600,
                            letterSpacing: 0.2,
                            color: theme.textMuted,
                            background: theme.tabBg,
                        }}
                    >
                        {formatCodeLanguageLabel(activeFile.language)}
                    </span>
                    {activeFile.opType === 'read' && (
                        <span
                            data-testid="code-preview-readonly-badge"
                            style={{ flexShrink: 0, opacity: 0.9 }}
                        >
                            {lang.startsWith('zh') ? '只读' : 'read-only'}
                        </span>
                    )}
                    {activeFile.opType && activeFile.opType !== 'read' ? (
                        codeFileLineDeltaHasChange(computeCodeFileLineDelta(activeFile)) ? (
                            <CodeFileDiffStat
                                file={activeFile}
                                theme={theme}
                                testId="code-preview-diff-stat"
                            />
                        ) : (
                            <span style={{ flexShrink: 0, opacity: 0.8 }}>
                                {activeFile.opType === 'create' ? 'NEW' : 'MOD'}
                            </span>
                        )
                    ) : null}
                    {totalLines > 0 && (
                        <span data-testid="code-preview-line-count" style={{ flexShrink: 0, opacity: 0.8 }}>
                            {totalLines} {lang.startsWith('zh') ? '行' : 'lines'}
                        </span>
                    )}
                    {isCodeFileDirty(activeFile) && !codeFileLineDeltaHasChange(computeCodeFileLineDelta(activeFile)) ? (
                        <span data-testid="code-preview-dirty-badge" style={{ flexShrink: 0, opacity: 0.9 }}>
                            {lang.startsWith('zh') ? '已修改' : 'changed'}
                        </span>
                    ) : null}
                </div>
            )}

            {!workspaceActive && findOpen && (
                <CodePreviewFindBar
                    query={findQuery}
                    matchCount={matchLineIndexes.length}
                    activeIndex={findIndex}
                    theme={theme}
                    lang={lang}
                    caseSensitive={findCaseSensitive}
                    wholeWord={findWholeWord}
                    useRegex={findUseRegex}
                    regexError={findRegexError}
                    onQueryChange={(q) => {
                        setFindQuery(q);
                        setFindIndex(0);
                    }}
                    onToggleCase={() => {
                        setFindCaseSensitive((v) => !v);
                        setFindIndex(0);
                    }}
                    onToggleWord={() => {
                        setFindWholeWord((v) => !v);
                        setFindIndex(0);
                    }}
                    onToggleRegex={() => {
                        setFindUseRegex((v) => !v);
                        setFindIndex(0);
                    }}
                    onNext={goNextMatch}
                    onPrev={goPrevMatch}
                    onClose={closeFind}
                    inputRef={findInputRef}
                />
            )}

            {!workspaceActive && gotoOpen && (
                <CodePreviewGoToLineBar
                    value={gotoValue}
                    maxLines={Math.max(1, totalLines)}
                    theme={theme}
                    lang={lang}
                    onChange={setGotoValue}
                    onSubmit={submitGoto}
                    onClose={closeGoto}
                    inputRef={gotoInputRef}
                />
            )}

            {/* Code content area */}
            {activeFile?.previewTruncated && (
                <div role="status" aria-live="polite" style={{ padding: '6px 12px', borderBottom: `1px solid ${theme.border}`, background: theme.lineNumBg, color: theme.textMuted, fontSize: 12, flexShrink: 0 }}>
                    {lang === 'zh-Hant'
                        ? '遠端原始碼預覽已截斷；目前僅顯示檔案開頭部分。'
                        : lang.startsWith('zh')
                            ? '远程源码预览已截断；当前仅显示文件开头部分。'
                            : 'Remote preview is truncated; only the beginning of this file is shown.'}
                </div>
            )}
            <div
                ref={scrollRef}
                className="ai-chat-scrollbar"
                style={{
                    flex: 1,
                    overflowY: 'auto',
                    overflowX: 'auto',
                    minHeight: 0,
                }}
            >
                {workspaceActive ? (
                    <CodePreviewWorkspace projectPath={projectPath} refreshToken={workspaceRefreshToken} resetOnRefresh={workspaceResetOnRefresh} cloudMode={cloudMode} lang={lang} theme={theme} onOpenFile={openWorkspaceFile} onFileDeleted={(path) => { onCloseFile?.(path); for (const filePath of Array.from(files.keys())) { if (filePath !== path && (filePath.startsWith(`${path}/`) || filePath.startsWith(`${path}\\`))) onCloseFile?.(filePath); } }} />
                ) : activeFile ? (
                    diffLines ? (
                        <DiffView
                            diffLines={diffLines}
                            theme={theme}
                            matchLineIndexes={matchLineIndexes}
                            activeMatchLine={activeMatchLine}
                            wordWrap={wordWrap}
                            fontSize={fontSize}
                        />
                    ) : isMarkdownLanguage(activeFile.language) ? (
                        <div style={{ fontSize: clampCodePreviewFontSize(fontSize) }}>
                            <MarkdownPreview
                                content={activeFile.content}
                                theme={theme}
                                matchLineIndexes={matchLineIndexes}
                                activeMatchLine={activeMatchLine}
                            />
                        </div>
                    ) : (
                        <PlainCodeView
                            content={activeFile.content}
                            language={activeFile.language}
                            theme={theme}
                            matchLineIndexes={matchLineIndexes}
                            activeMatchLine={activeMatchLine}
                            wordWrap={wordWrap}
                            fontSize={fontSize}
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
