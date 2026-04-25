import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { colors } from "../remote/styles";
import { OpenFileOrShowInFolder, SelectProjectDir, SetWorkflowWorkingDir } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import type { ChatMessage, CancelAIAssistantResult, ChatAction, AIAssistantInitStatus, ChatConfirmation, ChatUnfinishedSlot } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage, isImageFilePath, buildOutgoingMessageMulti } from "./useAIAssistant";
import { useWorkflowState } from "./useWorkflowState";
import { WorkflowDocPreview, type DocPreviewTheme } from "./WorkflowDocPreview";
import { useCodePreviewState } from "./useCodePreviewState";
import { CodePreviewPanel, darkCodePreviewTheme, lightCodePreviewTheme } from "./CodePreviewPanel";
import { useBufferQueue } from "./useBufferQueue";
import type { AttachmentInfo } from "./useBufferQueue";
import { BufferQueuePanel } from "./BufferQueuePanel";

interface AIAssistantPanelStateProps {
    messages: ChatMessage[];
    progressMessages?: ChatMessage[];
    sending: boolean;
    streaming: boolean;
    visualBusy?: boolean;
    ready: boolean;
    initStatus?: AIAssistantInitStatus;
    selectedFilePaths?: string[];
    submittedPrompts?: string[];
    draftInputValue?: string;
    trialReflectEnabled?: boolean;
    scrollToTopSeq?: number;
    onboardingIncomplete?: boolean;
    showTraceEntry?: boolean;
}

interface AIAssistantPanelActionProps {
    browseFile?: () => Promise<void>;
    clearSelectedFile?: () => void;
    removeSelectedFile?: (index: number) => void;
    sendMessage: (text: string) => Promise<void>;
    sendMessageInBackground?: (text: string) => Promise<void>;
    clearHistory: () => Promise<void>;
    recordSubmittedPrompt?: (text: string) => void;
    setDraftInputValue?: (text: string) => void;
    executeAction: (command: string) => Promise<void>;
    refreshNews: () => void;
    onOpenOnboarding?: () => void;
    cancelSession?: () => Promise<CancelAIAssistantResult>;
    onOpenTutorial?: () => void;
}

interface AIAssistantPanelWindowProps {
    inline?: boolean;
    maximized?: boolean;
    onToggleMaximize?: () => void;
    onHideWindow?: () => void;
}

interface AIAssistantPanelProps {
    onClose: () => void;
    lang: string; // 'zh-Hans' | 'zh-Hant' | 'en'
    chatFontSize?: number; // 12–24px, default 14
    state: AIAssistantPanelStateProps;
    actions: AIAssistantPanelActionProps;
    window?: AIAssistantPanelWindowProps;
    onThemeModeChange?: (mode: 'light' | 'dark') => void;
}

const AI_THEME_MODE_STORAGE_KEY = "ai_assistant_theme_mode";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

/** Map raw workflow_type strings to user-friendly display labels. */
const workflowTypeLabelMap: Record<string, { en: string; zh: string }> = {
    coding: { en: "Coding", zh: "编程" },
    product_design: { en: "Product Design", zh: "产品设计" },
    innovation: { en: "Innovation", zh: "创新方案" },
    business_plan: { en: "Business Plan", zh: "商业计划" },
    testing: { en: "Testing", zh: "测试" },
    literature_review: { en: "Literature Review", zh: "文献综述" },
    research_report: { en: "Research Report", zh: "研究报告" },
    experiment_design: { en: "Experiment Design", zh: "实验设计" },
    grant_proposal: { en: "Grant Proposal", zh: "基金申请" },
    paper_writing: { en: "Paper Writing", zh: "论文写作" },
    project_proposal: { en: "Project Proposal", zh: "项目提案" },
    event_planning: { en: "Event Planning", zh: "活动策划" },
    competitive_analysis: { en: "Competitive Analysis", zh: "竞品分析" },
    presentation_design: { en: "Presentation Design", zh: "演示设计" },
    bid_response: { en: "Bid Response", zh: "招投标" },
    contract_review: { en: "Contract Review", zh: "合同审查" },
    due_diligence: { en: "Due Diligence", zh: "尽职调查" },
    compliance_audit: { en: "Compliance Audit", zh: "合规审计" },
    patent_analysis: { en: "Patent Analysis", zh: "专利分析" },
    workflow: { en: "Workflow", zh: "工作流" },
};
const workflowTypeLabel = (type: string, lang?: string): string => {
    const entry = workflowTypeLabelMap[type];
    if (!entry) return type; // fallback: show raw string
    return lang === 'zh-Hans' || lang === 'zh-Hant' ? entry.zh : entry.en;
};

/* ── Theme definitions ── */

interface Theme {
    bg: string;
    titleBarBg: string;
    titleBarBorder: string;
    titleText: string;
    text: string;
    textMuted: string;
    inputBarBg: string;
    inputBarBorder: string;
    inputText: string;
    codeBg: string;
    codeText: string;
    codeBlockBg: string;
    codeBlockBorder: string;
    codeBlockLang: string;
    borderLeft: string;
    responseBorderLeft: string;
    headingColor: string;
    linkColor: string;
    pathColor: string;
    promptColor: string;
    userColor: string;
    divider: string;
    fieldBg: string;
    fieldBorder: string;
    fieldLabel: string;
    errorText: string;
    errorBg: string;
    errorBorder: string;
    emptyHint: string;
    boldColor: string;
    italicColor: string;
    bulletColor: string;
    quoteBorder: string;
    quoteText: string;
    btnColor: string;
    btnBorder: string;
    actionBtnColor: string;
    closeBtnColor: string;
    sendBtnColor: string;
    sendBtnBorder: string;
}

const overlayTheme: Theme = {
    bg: "var(--theme-surface)",
    titleBarBg: "var(--theme-surface-muted)",
    titleBarBorder: "var(--theme-border)",
    titleText: "var(--theme-text-secondary)",
    text: "var(--theme-text-primary)",
    textMuted: "var(--theme-text-muted)",
    inputBarBg: "var(--theme-surface)",
    inputBarBorder: "var(--theme-primary)",
    inputText: "var(--theme-text-primary)",
    codeBg: "var(--theme-surface-muted)",
    codeText: "var(--theme-danger)",
    codeBlockBg: "var(--theme-surface-muted)",
    codeBlockBorder: "var(--theme-border)",
    codeBlockLang: "var(--theme-text-muted)",
    borderLeft: "var(--theme-border)",
    responseBorderLeft: "var(--theme-primary-soft)",
    headingColor: "var(--theme-primary)",
    linkColor: "var(--theme-primary)",
    pathColor: "var(--theme-success)",
    promptColor: "var(--theme-primary)",
    userColor: "var(--theme-primary)",
    divider: "var(--theme-border)",
    fieldBg: "var(--theme-surface-muted)",
    fieldBorder: "var(--theme-border)",
    fieldLabel: "var(--theme-text-muted)",
    errorText: "var(--theme-danger)",
    errorBg: "var(--theme-danger-bg)",
    errorBorder: "var(--theme-danger)",
    emptyHint: "var(--theme-text-muted)",
    boldColor: "var(--theme-text-primary)",
    italicColor: "var(--theme-text-secondary)",
    bulletColor: "var(--theme-text-muted)",
    quoteBorder: "var(--theme-primary-soft)",
    quoteText: "var(--theme-text-secondary)",
    btnColor: "var(--theme-primary)",
    btnBorder: "var(--theme-primary)",
    actionBtnColor: "var(--theme-text-secondary)",
    closeBtnColor: "var(--theme-text-muted)",
    sendBtnColor: "var(--theme-text-primary)",
    sendBtnBorder: "var(--theme-primary)",
};

const lightTheme: Theme = {
    bg: "var(--theme-surface)",
    titleBarBg: "var(--theme-surface-muted)",
    titleBarBorder: "var(--theme-border)",
    titleText: "var(--theme-text-secondary)",
    text: "var(--theme-text-primary)",
    textMuted: "var(--theme-text-muted)",
    inputBarBg: "var(--theme-surface)",
    inputBarBorder: "var(--theme-border)",
    inputText: "var(--theme-text-primary)",
    codeBg: "var(--theme-surface-muted)",
    codeText: "var(--theme-danger)",
    codeBlockBg: "var(--theme-surface-muted)",
    codeBlockBorder: "var(--theme-border)",
    codeBlockLang: "var(--theme-text-muted)",
    borderLeft: "var(--theme-border)",
    responseBorderLeft: "var(--theme-primary-soft)",
    headingColor: "var(--theme-primary)",
    linkColor: "var(--theme-primary)",
    pathColor: "var(--theme-success)",
    promptColor: "var(--theme-primary)",
    userColor: "var(--theme-primary)",
    divider: "var(--theme-border)",
    fieldBg: "var(--theme-surface-muted)",
    fieldBorder: "var(--theme-border)",
    fieldLabel: "var(--theme-text-muted)",
    errorText: "var(--theme-danger)",
    errorBg: "var(--theme-danger-bg)",
    errorBorder: "var(--theme-danger)",
    emptyHint: "var(--theme-text-muted)",
    boldColor: "var(--theme-text-primary)",
    italicColor: "var(--theme-text-secondary)",
    bulletColor: "var(--theme-text-muted)",
    quoteBorder: "var(--theme-primary-soft)",
    quoteText: "var(--theme-text-secondary)",
    btnColor: "var(--theme-primary)",
    btnBorder: "var(--theme-primary)",
    actionBtnColor: "var(--theme-text-secondary)",
    closeBtnColor: "var(--theme-text-muted)",
    sendBtnColor: "var(--theme-primary)",
    sendBtnBorder: "var(--theme-primary)",
};

const darkTheme: Theme = {
    bg: "#0b1220",
    titleBarBg: "#111827",
    titleBarBorder: "#334155",
    titleText: "#f1f5f9",
    text: "#f1f5f9",
    textMuted: "#cbd5e1",
    inputBarBg: "#0f172a",
    inputBarBorder: "#334155",
    inputText: "#e5e7eb",
    codeBg: "#0f172a",
    codeText: "#f87171",
    codeBlockBg: "#111827",
    codeBlockBorder: "#334155",
    codeBlockLang: "#94a3b8",
    borderLeft: "#334155",
    responseBorderLeft: "#6366f1",
    headingColor: "#c4b5fd",
    linkColor: "#c4b5fd",
    pathColor: "#4ade80",
    promptColor: "#c4b5fd",
    userColor: "#818cf8",
    divider: "#334155",
    fieldBg: "#111827",
    fieldBorder: "rgba(148, 163, 184, 0.2)",
    fieldLabel: "#94a3b8",
    errorText: "#f87171",
    errorBg: "rgba(239, 68, 68, 0.12)",
    errorBorder: "#f87171",
    emptyHint: "#94a3b8",
    boldColor: "#f1f5f9",
    italicColor: "#cbd5e1",
    bulletColor: "#94a3b8",
    quoteBorder: "#6366f1",
    quoteText: "#cbd5e1",
    btnColor: "#c4b5fd",
    btnBorder: "#6366f1",
    actionBtnColor: "#cbd5e1",
    closeBtnColor: "#cbd5e1",
    sendBtnColor: "#c4b5fd",
    sendBtnBorder: "#6366f1",
};

/* ── FileCard Component ── */

interface FileCardProps {
    filePath: string;
    index: number;
    theme: Theme;
    lang: string;
    onRemove?: (index: number) => void;
}

const FileCard = ({ filePath, index, theme, lang, onRemove }: FileCardProps) => {
    const fileName = filePath.split(/[/\\]/).pop() || filePath;

    const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
        if (e.key === 'Delete' || e.key === 'Backspace') {
            e.preventDefault();
            onRemove?.(index);
        }
    }, [index, onRemove]);

    return (
        <div
            tabIndex={0}
            role="listitem"
            aria-label={localizeText(lang, `File: ${fileName}`, `文件：${fileName}`)}
            title={filePath}
            onKeyDown={handleKeyDown}
            style={{
                display: "inline-flex",
                alignItems: "center",
                gap: "4px",
                maxWidth: "180px",
                padding: "3px 6px 3px 8px",
                borderRadius: "12px",
                background: theme.codeBlockBg,
                border: `1px solid ${theme.codeBlockBorder}`,
                color: theme.text,
                fontSize: "12px",
                cursor: "default",
                outline: "none",
                flexShrink: 0,
            }}
        >
            <span style={{ color: theme.pathColor, fontSize: "11px", flexShrink: 0 }} aria-hidden="true">📎</span>
            <span style={{
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                fontWeight: 500,
                flex: 1,
                minWidth: 0,
            }}>
                {fileName}
            </span>
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    onRemove?.(index);
                }}
                aria-label={localizeText(lang, "Remove file", "移除文件")}
                style={{
                    border: "none",
                    borderRadius: "50%",
                    background: "transparent",
                    color: theme.textMuted,
                    cursor: "pointer",
                    padding: "0",
                    fontSize: "13px",
                    lineHeight: 1,
                    width: "16px",
                    height: "16px",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    flexShrink: 0,
                }}
                onMouseEnter={e => (e.currentTarget.style.color = theme.errorText)}
                onMouseLeave={e => (e.currentTarget.style.color = theme.textMuted)}
            >
                ×
            </button>
        </div>
    );
};

/* ── Style constants ── */

const overlayStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 10000,
    display: "flex",
    flexDirection: "column",
    background: overlayTheme.bg,
    textAlign: "left",
    boxShadow: "0 0 40px rgba(0,0,0,0.08)",
};

const maximizedInlineStyle: React.CSSProperties = {
    position: "fixed",
    inset: 0,
    zIndex: 12000,
    display: "flex",
    flexDirection: "column",
    width: "100vw",
    height: "100vh",
    minWidth: 0,
    minHeight: 0,
    boxSizing: "border-box",
    background: lightTheme.bg,
    textAlign: "left",
    boxShadow: "0 0 40px rgba(0,0,0,0.12)",
    overflow: "hidden",
};

const dotBase: React.CSSProperties = {
    width: 10,
    height: 10,
    borderRadius: "50%",
    display: "inline-block",
    cursor: "pointer",
};

const baseInputBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "1px solid",
    borderRadius: "4px",
    padding: "6px 12px",
    fontSize: "13px",
    fontFamily: "Consolas, monospace",
    cursor: "pointer",
    lineHeight: 1,
    minHeight: "34px",
    flexShrink: 0,
};

const baseWindowControlBtnStyle: React.CSSProperties = {
    width: "36px",
    height: "28px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    background: "transparent",
    border: "none",
    borderRadius: "4px",
    cursor: "pointer",
    padding: 0,
    lineHeight: 1,
    flexShrink: 0,
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    transition: "background 120ms ease, color 120ms ease",
};


const baseActionBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "none",
    fontSize: "12px",
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    cursor: "pointer",
    padding: 0,
    borderRadius: "4px",
    lineHeight: 1,
    minHeight: "28px",
    minWidth: "28px",
    width: "32px",
    height: "28px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    transition: "background 120ms ease, color 120ms ease",
};

const AI_PANEL_STATIC_STYLE_ID = "ai-panel-static-style";
const AI_PANEL_STATIC_STYLE_TEXT = `
    @keyframes blink { 50% { opacity: 0; } }
    @keyframes ai-spinner-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
    @keyframes maclaw-spin { to { transform: rotate(360deg); } }
    .pinned-news-card > div { margin-top: 0 !important; margin-bottom: 0 !important; }
    .ai-window-control:hover { background: var(--ai-window-control-hover-bg, rgba(148, 163, 184, 0.14)) !important; }
    .ai-window-control:active { filter: brightness(0.96); }
    .ai-window-control:focus-visible {
        outline: 2px solid rgba(99, 102, 241, 0.55);
        outline-offset: 1px;
    }
    .ai-titlebar-tool:hover { background: var(--ai-titlebar-tool-hover-bg, rgba(148, 163, 184, 0.12)) !important; }
    .ai-titlebar-tool:active { filter: brightness(0.96); }
    .ai-titlebar-tool:focus-visible {
        outline: 2px solid rgba(99, 102, 241, 0.4);
        outline-offset: 1px;
    }
`;

function getTitleBarToolButtonStyle(t: Theme, variant: "default" | "danger" | "active" = "default"): React.CSSProperties {
    const isDanger = variant === "danger";
    const isActive = variant === "active";
    return {
        ...baseActionBtnStyle,
        color: isDanger ? t.errorText : (isActive ? t.text : t.actionBtnColor),
        background: isDanger
            ? t.errorBg
            : (isActive ? t.divider : "transparent"),
        boxShadow: isActive ? `inset 0 0 0 1px ${t.fieldBorder}` : "none",
        ['--ai-titlebar-tool-hover-bg' as any]: isDanger
            ? t.errorBorder + "33"
            : (isActive ? t.divider : "rgba(148, 163, 184, 0.12)"),
    };
}

function getWindowControlButtonStyle(t: Theme, variant: "hide" | "fullscreen", active = false): React.CSSProperties {
    const hoverBg = variant === "hide" ? "rgba(148, 163, 184, 0.14)" : "rgba(99, 102, 241, 0.12)";
    return {
        ...baseWindowControlBtnStyle,
        color: active ? t.text : t.actionBtnColor,
        background: active ? t.divider : "transparent",
        boxShadow: active ? `inset 0 0 0 1px ${t.fieldBorder}` : "none",
        ['--ai-window-control-hover-bg' as any]: hoverBg,
    };
}

/* ── Themed inline markdown rendering ── */

/** Detect whether a string looks like a file/directory path (Windows or Unix) */
function looksLikeFilePath(s: string): boolean {
    // Windows: drive letter + backslash (e.g. C:\Users\...)
    if (/^[A-Za-z]:\\/.test(s)) return true;
    // Unix absolute or home: /Users/..., /home/..., ~/...
    if (/^(~|\/(?:Users|home|tmp|var|opt|etc|usr))[/\\]/.test(s)) return true;
    return false;
}

/** Render a clickable path link element.
 *  @param trimTrailing — strip trailing prose punctuation that the regex may over-capture
 *                         on bare (unwrapped) paths. Paths extracted from backticks/bold
 *                         are already clean and should pass false. */
function renderPathLink(filePath: string, key: number, t: Theme, trimTrailing = false): React.ReactNode {
    const display = trimTrailing
        ? filePath.replace(/[\s,;:!?。，；：！？）\]]+$/, "")
        : filePath;
    return (
        <a key={key}
           href="#"
           onClick={(event) => openFileInFolder(event, display)}
           style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }}
           title={display}
        >📂 {display}</a>
    );
}

function renderInlineMarkdown(text: string, t: Theme): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    // Path matching: two strategies per platform
    // 1. Broad match for paths with CJK/spaces — requires .ext ending as boundary anchor
    // 2. Original ASCII-only match — works without .ext
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^\s*][^*]*?\*)|(\[[^\]]+\]\([^)]+\))|([A-Za-z]:\\[^\n\r*?"<>|:]+\.\w+)|([A-Za-z]:\\[\w\\.\-]+\\?)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))\/[^\n\r*?"<>|:]+\.\w+)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))[\w/.\-]+)/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let idx = 0;
    while ((match = re.exec(text)) !== null) {
        if (match.index > lastIndex) {
            parts.push(text.slice(lastIndex, match.index));
        }
        const m = match[0];
        if (match[1]) {
            // Inline code — check if content is a file path
            const inner = m.slice(1, -1);
            if (looksLikeFilePath(inner)) {
                parts.push(renderPathLink(inner, idx++, t));
            } else {
                parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{inner}</code>);
            }
        } else if (match[2]) {
            // Bold — check if content is a file path
            const inner = m.slice(2, -2);
            if (looksLikeFilePath(inner)) {
                parts.push(renderPathLink(inner, idx++, t));
            } else {
                parts.push(<strong key={idx++} style={{ color: t.boldColor, fontWeight: 700 }}>{inner}</strong>);
            }
        } else if (match[3]) {
            parts.push(<em key={idx++} style={{ color: t.italicColor }}>{m.slice(1, -1)}</em>);
        } else if (match[4]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(e) => { e.preventDefault(); BrowserOpenURL(href); }} style={{ color: t.linkColor, textDecoration: "underline", cursor: "pointer" }}>{lm[1]}</a>);
                } else if (looksLikeFilePath(href)) {
                    // Local file path in markdown link: [label](C:\path\to\file)
                    parts.push(
                        <a key={idx++} href="#"
                           onClick={(event) => openFileInFolder(event, href)}
                           style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }}
                           title={href}
                        >📂 {lm[1]}</a>
                    );
                } else {
                    parts.push(<span key={idx++} style={{ color: t.linkColor }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        } else if (match[5] || match[6] || match[7] || match[9]) {
            // Bare path (not wrapped in backticks or bold) — may over-capture trailing punctuation
            const cleaned = m.replace(/[\s,;:!?。，；：！？）\]]+$/, "");
            if (cleaned.length !== m.length) {
                // Rewind regex lastIndex so trimmed chars are re-processed
                re.lastIndex -= (m.length - cleaned.length);
            }
            parts.push(renderPathLink(cleaned, idx++, t));
        }
        lastIndex = re.lastIndex;
    }
    if (lastIndex < text.length) {
        parts.push(text.slice(lastIndex));
    }
    return parts.length > 0 ? parts : ["\u00A0"];
}

function renderMarkdownLine(text: string, key: string | number, t: Theme): React.ReactNode {
    const trimmed = text.trimStart();

    const headingMatch = trimmed.match(/^(#{1,4})\s+(.+)$/);
    if (headingMatch) {
        const level = headingMatch[1].length;
        const sizes: Record<number, string> = { 1: "1.2em", 2: "1.1em", 3: "1.0em", 4: "0.95em" };
        return (
            <div key={key} style={{ fontSize: sizes[level] || "1em", fontWeight: 700, color: t.headingColor, margin: "0.4em 0 0.2em" }}>
                {renderInlineMarkdown(headingMatch[2], t)}
            </div>
        );
    }

    if (/^>\s/.test(trimmed)) {
        return (
            <div key={key} style={{ borderLeft: `2px solid ${t.quoteBorder}`, paddingLeft: "8px", color: t.quoteText, fontStyle: "italic", minHeight: "1.4em" }}>
                {renderInlineMarkdown(trimmed.slice(2), t)}
            </div>
        );
    }

    // Horizontal rule: ---, ***, ___ (3+ chars, nothing else on the line)
    if (/^[-*_]{3,}\s*$/.test(trimmed)) {
        return <hr key={key} style={{ border: "none", borderTop: `1px solid ${t.divider}`, margin: "8px 0" }} />;
    }

    if (/^[-*]\s/.test(trimmed)) {
        return (
            <div key={key} style={{ paddingLeft: "1em", textIndent: "-0.7em", minHeight: "1.4em", color: t.text }}>
                <span style={{ color: t.bulletColor }}>•</span>{" "}
                {renderInlineMarkdown(trimmed.slice(2), t)}
            </div>
        );
    }

    const numMatch = trimmed.match(/^(\d+)[.)]\s+(.+)$/);
    if (numMatch) {
        return (
            <div key={key} style={{ paddingLeft: "1.2em", textIndent: "-1.2em", minHeight: "1.4em", color: t.text }}>
                <span style={{ color: t.bulletColor }}>{numMatch[1]}.</span>{" "}
                {renderInlineMarkdown(numMatch[2], t)}
            </div>
        );
    }

    return (
        <div key={key} style={{ minHeight: "1.4em", color: t.text }}>
            {renderInlineMarkdown(text, t) || "\u00A0"}
        </div>
    );
}

/* ── Structured response rendering ── */

/* ── Table helpers ── */

/** Detect a pipe-delimited table row: must start with | (after trimming) */
function isTableRow(line: string): boolean {
    const trimmed = line.trim();
    // Only match lines that start with | to avoid false positives on prose with pipes
    return trimmed.startsWith("|") && trimmed.length > 1;
}

/** Detect a separator row like |---|---| or |:---:|---:| */
function isSeparatorRow(line: string): boolean {
    const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
    return /^[\s|:\-]+$/.test(trimmed) && trimmed.includes("-");
}

/** Parse cells from a pipe-delimited row */
function parseTableCells(line: string): string[] {
    let trimmed = line.trim();
    if (trimmed.startsWith("|")) trimmed = trimmed.slice(1);
    if (trimmed.endsWith("|")) trimmed = trimmed.slice(0, -1);
    return trimmed.split("|").map(c => c.trim());
}

/** Render a collected set of table lines into an HTML table element */
function renderTable(tableLines: string[], key: string, t: Theme): React.ReactNode {
    // Filter out separator rows, keep data rows
    const dataRows = tableLines.filter(l => !isSeparatorRow(l));
    if (dataRows.length === 0) return null;

    // Need at least a header + 1 body row (or header + separator) to be a real table
    // A single pipe-line is not a table — render nothing and let caller fall back
    if (tableLines.length < 2) return null;

    const headerCells = parseTableCells(dataRows[0]);
    const bodyRows = dataRows.slice(1);

    const cellStyle: React.CSSProperties = {
        border: `1px solid ${t.divider}`,
        padding: "4px 8px",
        textAlign: "left",
        fontSize: "0.9em",
        lineHeight: 1.5,
    };

    return (
        <div key={key} style={{ overflowX: "auto", margin: "4px 0" }}>
            <table style={{ borderCollapse: "collapse", width: "100%", color: t.text }}>
                <thead>
                    <tr>
                        {headerCells.map((cell, ci) => (
                            <th key={ci} style={{ ...cellStyle, fontWeight: 600, background: t.fieldBg }}>
                                {renderInlineMarkdown(cell, t)}
                            </th>
                        ))}
                    </tr>
                </thead>
                {bodyRows.length > 0 && (
                    <tbody>
                        {bodyRows.map((row, ri) => {
                            const cells = parseTableCells(row);
                            return (
                                <tr key={ri}>
                                    {headerCells.map((_, ci) => (
                                        <td key={ci} style={cellStyle}>
                                            {renderInlineMarkdown(cells[ci] || "", t)}
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

function renderContentWithCodeBlocks(content: string, t: Theme): React.ReactNode[] {
    const elements: React.ReactNode[] = [];
    const lines = content.split("\n");
    let inCodeBlock = false;
    let codeBlockLines: string[] = [];
    let codeBlockLang = "";
    let tableLines: string[] = [];
    let lineIdx = 0;

    const flushCodeBlock = () => {
        if (codeBlockLines.length > 0) {
            elements.push(
                <pre key={`code-${elements.length}`} style={{
                    background: t.codeBlockBg,
                    border: `1px solid ${t.codeBlockBorder}`,
                    borderRadius: "4px",
                    padding: "8px 10px",
                    margin: "4px 0",
                    fontSize: "0.9em",
                    overflowX: "auto",
                    color: t.codeText,
                    lineHeight: 1.5,
                }}>
                    {codeBlockLang && <div style={{ color: t.codeBlockLang, fontSize: "0.85em", marginBottom: "4px" }}>{codeBlockLang}</div>}
                    <code>{codeBlockLines.join("\n")}</code>
                </pre>
            );
        }
        codeBlockLines = [];
        codeBlockLang = "";
    };

    const flushTable = () => {
        if (tableLines.length > 0) {
            const rendered = renderTable(tableLines, `tbl-${elements.length}`, t);
            if (rendered) {
                elements.push(rendered);
            } else {
                // Not a real table (e.g. single pipe-line), render as normal lines
                for (const tl of tableLines) {
                    elements.push(renderMarkdownLine(tl, `md-fallback-${elements.length}`, t));
                }
            }
            tableLines = [];
        }
    };

    for (const line of lines) {
        if (/^```/.test(line.trimStart())) {
            flushTable();
            if (inCodeBlock) {
                flushCodeBlock();
                inCodeBlock = false;
            } else {
                inCodeBlock = true;
                codeBlockLang = line.trimStart().slice(3).trim();
            }
        } else if (inCodeBlock) {
            codeBlockLines.push(line);
        } else if (isTableRow(line)) {
            tableLines.push(line);
        } else {
            flushTable();
            elements.push(renderMarkdownLine(line, `md-${lineIdx}`, t));
        }
        lineIdx++;
    }
    if (inCodeBlock) flushCodeBlock();
    flushTable();
    return elements;
}

function renderFields(fields: Array<{ label: string; value: string }>, t: Theme): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {fields.map((f, i) => {
                const isRecovery = f.label === "Recovery";
                const recoveryTone = String(f.value || '').toLowerCase();
                const recoveryStyle: React.CSSProperties = isRecovery
                    ? {
                        display: "inline-flex",
                        alignItems: "center",
                        padding: "2px 8px",
                        borderRadius: "999px",
                        fontWeight: 700,
                        background: recoveryTone.includes('failed')
                            ? "rgba(220, 38, 38, 0.12)"
                            : recoveryTone.includes('partial')
                                ? "rgba(245, 158, 11, 0.16)"
                                : "rgba(34, 197, 94, 0.14)",
                        color: recoveryTone.includes('failed')
                            ? t.errorText
                            : recoveryTone.includes('partial')
                                ? t.errorText
                                : t.pathColor,
                    }
                    : { color: t.text };
                return (
                    <div key={`field-${i}`} data-testid="field-card" style={{
                        background: t.fieldBg,
                        border: `1px solid ${t.fieldBorder}`,
                        borderRadius: "4px",
                        padding: "4px 8px",
                        fontSize: "1em",
                    }}>
                        <span style={{ color: t.fieldLabel, marginRight: "6px" }}>{f.label}:</span>
                        <span data-testid={isRecovery ? 'recovery-badge' : undefined} style={recoveryStyle}>{f.value}</span>
                    </div>
                );
            })}
        </div>
    );
}

function renderActions(
    actions: ChatAction[],
    executeAction: (command: string) => void,
    t: Theme,
): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {actions.map((a, i) => (
                <button
                    key={`action-${i}`}
                    data-testid="action-button"
                    onClick={() => executeAction(a.command)}
                    style={{
                        ...baseInputBtnStyle,
                        color: a.style === "danger" ? t.errorText : t.btnColor,
                        borderColor: a.style === "danger" ? t.errorText : t.btnBorder,
                        fontSize: "1em",
                        padding: "4px 10px",
                        minHeight: "28px",
                    }}
                >
                    {a.label}
                </button>
            ))}
        </div>
    );
}

function renderConfirmationList(testId: string, title: string, items: string[], t: Theme): React.ReactNode {
    if (items.length === 0) return null;
    return (
        <div data-testid={testId} style={{ marginTop: "8px" }}>
            <div style={{ color: t.fieldLabel, fontSize: "0.917em", marginBottom: "4px" }}>{title}</div>
            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                {items.map((item, index) => (
                    <div key={`${testId}-${index}`} style={{ minHeight: "1.4em", color: t.text }}>
                        <span style={{ color: t.bulletColor }}>•</span>{" "}
                        {renderInlineMarkdown(item, t)}
                    </div>
                ))}
            </div>
        </div>
    );
}

function renderConfirmationCard(
    confirmation: ChatConfirmation,
    actions: ChatAction[] | undefined,
    executeAction: (command: string) => void,
    t: Theme,
    lang: string,
): React.ReactNode {
    const targetPaths = confirmation.targetPaths || [];
    const plannedActions = confirmation.plannedActions || [];
    const riskFlags = confirmation.riskFlags || [];
    const revisionHints = confirmation.revisionHints || [];
    const taskType = confirmation.taskType?.trim() || '';
    const status = confirmation.status?.trim() || '';
    return (
        <div
            data-testid="confirmation-card"
            style={{
                marginTop: "8px",
                padding: "10px 12px",
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(99,102,241,0.02))",
            }}
        >
            <div style={{ color: t.headingColor, fontWeight: 700, marginBottom: "6px" }}>
                {taskType
                    ? localizeText(lang, `Pre-execution confirmation · ${taskType}`, `执行前确认 · ${taskType}`)
                    : localizeText(lang, "Pre-execution confirmation", "执行前确认")}
            </div>
            {status && (
                <div data-testid="confirmation-status" style={{ color: t.fieldLabel, fontSize: "0.917em", marginBottom: "6px" }}>
                    {localizeText(lang, `Status: ${status}`, `状态：${status}`)}
                </div>
            )}
            <div data-testid="confirmation-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                {renderContentWithCodeBlocks(confirmation.summary, t)}
            </div>
            {renderConfirmationList("confirmation-target-paths", localizeText(lang, "Target paths", "目标目录"), targetPaths, t)}
            {renderConfirmationList("confirmation-planned-actions", localizeText(lang, "Planned actions", "计划动作"), plannedActions, t)}
            {renderConfirmationList("confirmation-risk-flags", localizeText(lang, "Risk warnings", "风险提示"), riskFlags, t)}
            {renderConfirmationList("confirmation-revision-hints", localizeText(lang, "Revision hints", "可补充/修正"), revisionHints, t)}
            {actions && actions.length > 0 && renderActions(actions, executeAction, t)}
        </div>
    );
}

function renderUnfinishedSlotCard(
    slot: ChatUnfinishedSlot,
    executeAction: (command: string) => void,
    t: Theme,
    lang: string,
): React.ReactNode {
    const actions = slot.actions || [];
    return (
        <div
            data-testid="unfinished-slot-card"
            style={{
                marginTop: "8px",
                padding: "10px 12px",
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: "linear-gradient(135deg, rgba(245,158,11,0.08), rgba(245,158,11,0.03))",
            }}
        >
            <div style={{ color: t.headingColor, fontWeight: 700, marginBottom: "6px" }}>
                {localizeText(lang, "Unfinished task", "未完成任务")}
            </div>
            {slot.status && (
                <div data-testid="unfinished-slot-status" style={{ color: t.fieldLabel, fontSize: "0.917em", marginBottom: "6px" }}>
                    {localizeText(lang, `Status: ${slot.status}`, `状态：${slot.status}`)}
                </div>
            )}
            {slot.title && (
                <div data-testid="unfinished-slot-title" style={{ color: t.text, fontWeight: 600, marginBottom: "4px" }}>
                    {slot.title}
                </div>
            )}
            {slot.summary && (
                <div data-testid="unfinished-slot-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                    {renderContentWithCodeBlocks(slot.summary, t)}
                </div>
            )}
            {slot.projectPath && (
                <div data-testid="unfinished-slot-project" style={{ marginTop: "6px", wordBreak: "break-all" }}>
                    <a
                        href="#"
                        onClick={(event) => openFileInFolder(event, slot.projectPath as string)}
                        style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }}
                        title={slot.projectPath}
                    >
                        📁 {slot.projectPath}
                    </a>
                </div>
            )}
            {actions.length > 0 && renderActions(actions, executeAction, t)}
        </div>
    );
}

function openFileInFolder(event: React.MouseEvent, filePath: string) {
    event.preventDefault();
    void OpenFileOrShowInFolder(filePath);
}

/* ── Render a single ChatMessage ── */

function renderMessage(msg: ChatMessage, executeAction: (cmd: string) => void, t: Theme, lang: string, isLastAssistant: boolean, savedFileLabel: string): React.ReactNode {
    const visibleFilePaths = msg.localFilePaths && msg.localFilePaths.length > 0
        ? msg.localFilePaths
        : (msg.localFilePath ? [msg.localFilePath] : []);
    switch (msg.role) {
        case "user":
            return (
                <div key={msg.id}>
                    <div style={{ borderTop: `1px solid ${t.divider}`, margin: "8px 0 4px 0" }} />
                    <div style={{ color: t.userColor, fontWeight: 600, padding: "3px 0 3px 1.2em", overflowWrap: "break-word", whiteSpace: "pre-wrap", textIndent: "-1.2em" }}>
                        ❯ {msg.content}
                    </div>
                </div>
            );
        case "assistant":
            return (
                <div key={msg.id} style={{
                    padding: "6px 10px",
                    borderLeft: `2px solid ${t.responseBorderLeft}`,
                    margin: "4px 0",
                    color: t.text,
                    background: t.fieldBg,
                    borderRadius: "8px",
                    boxShadow: `inset 0 0 0 1px ${t.fieldBorder}`,
                }}>
                    {/* Streaming: show blinking cursor only on the last assistant message */}
                    {isLastAssistant && !msg.content && !msg.fields && !msg.thumbnailBase64 && visibleFilePaths.length === 0 && (
                        <span style={{ opacity: 0.5, animation: "blink 1s step-end infinite" }}>▍</span>
                    )}
                    {msg.thumbnailBase64 && msg.localFilePath && (
                        <div style={{ margin: "4px 0 6px 0" }}>
                            <a href="#" onClick={(event) => openFileInFolder(event, msg.localFilePath!)}
                               style={{ display: "inline-block", cursor: "pointer" }}
                               title={msg.localFilePath}>
                                <img
                                    src={`data:image/png;base64,${msg.thumbnailBase64}`}
                                    alt="screenshot"
                                    style={{
                                        maxWidth: "180px", maxHeight: "120px",
                                        borderRadius: "4px", border: `1px solid ${t.borderLeft}`,
                                        objectFit: "contain",
                                    }}
                                />
                            </a>
                        </div>
                    )}
                    {renderContentWithCodeBlocks(msg.content, t)}
                    {msg.confirmation && renderConfirmationCard(msg.confirmation, msg.actions, executeAction, t, lang)}
                    {msg.unfinishedSlot && renderUnfinishedSlotCard(msg.unfinishedSlot, executeAction, t, lang)}
                    {visibleFilePaths.length > 0 && (
                        <div style={{ margin: "4px 0" }}>
                            {visibleFilePaths.map((fp, i) => (
                                <div key={i} style={{ padding: "2px 0" }}>
                                    <a href="#"
                                       onClick={(event) => openFileInFolder(event, fp)}
                                       style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer", wordBreak: "break-all" }}
                                       title={fp}>
                                        📄 {savedFileLabel}: 📁 {fp}
                                    </a>
                                </div>
                            ))}
                        </div>
                    )}
                    {msg.fields && msg.fields.length > 0 && renderFields(msg.fields, t)}
                    {!msg.confirmation && msg.actions && msg.actions.length > 0 && renderActions(msg.actions, executeAction, t)}
                </div>
            );
        case "progress":
            return (
                <div key={msg.id} style={{ color: t.textMuted, fontSize: "0.917em", padding: "1px 0", fontStyle: "italic" }}>
                    {msg.content}
                </div>
            );
        case "system":
            return (
                <div key={msg.id} style={{
                    padding: "8px 12px",
                    margin: "4px 0",
                    borderRadius: "8px",
                    background: t.codeBlockBg,
                    boxShadow: `inset 0 0 0 1px ${t.codeBlockBorder}`,
                    borderLeft: `3px solid ${t.promptColor}`,
                    color: t.text,
                    fontSize: "1em",
                    lineHeight: "1.6",
                }}>
                    {msg.kind === 'trace' && msg.fields && msg.fields.length > 0 && renderFields(msg.fields, t)}
                    {renderContentWithCodeBlocks(msg.content, t)}
                </div>
            );
        case "error":
            return (
                <div key={msg.id} style={{
                    color: t.errorText,
                    background: t.errorBg,
                    borderLeft: `2px solid ${t.errorBorder}`,
                    padding: "4px 8px",
                    margin: "2px 0",
                    borderRadius: "2px",
                    fontSize: "1em",
                }}>
                    {msg.content}
                </div>
            );
        default:
            return null;
    }
}

/* ── Inject static panel styles once at module level ── */
if (typeof document !== "undefined" && !document.getElementById(AI_PANEL_STATIC_STYLE_ID)) {
    const style = document.createElement("style");
    style.id = AI_PANEL_STATIC_STYLE_ID;
    style.textContent = AI_PANEL_STATIC_STYLE_TEXT;
    document.head.appendChild(style);
}

// ---------------------------------------------------------------------------
// Wails binding helper for pasting images
// ---------------------------------------------------------------------------

async function savePastedImage(base64: string, ext: string): Promise<string> {
    // @ts-ignore — Wails runtime binding
    if (typeof window !== 'undefined' && window.go?.main?.App?.SavePastedImage) {
        // @ts-ignore
        return window.go.main.App.SavePastedImage(base64, ext);
    }
    throw new Error("SavePastedImage binding not available");
}

/* ── Main component ── */

export function AIAssistantPanel({ onClose, lang, chatFontSize = 14, state, actions, window: panelWindow, onThemeModeChange }: AIAssistantPanelProps) {
    const {
        messages,
        progressMessages = [],
        sending,
        streaming,
        visualBusy,
        ready,
        initStatus,
        selectedFilePaths = [],
        submittedPrompts = [],
        draftInputValue = "",
        trialReflectEnabled = false,
        scrollToTopSeq,
        onboardingIncomplete,
        showTraceEntry = false,
    } = state;
    const {
        browseFile,
        clearSelectedFile,
        removeSelectedFile,
        sendMessage,
        clearHistory,
        recordSubmittedPrompt,
        setDraftInputValue,
        executeAction,
        refreshNews,
        onOpenOnboarding,
        cancelSession,
        onOpenTutorial,
    } = actions;
    const {
        inline,
        maximized = false,
        onToggleMaximize,
        onHideWindow,
    } = panelWindow || {};
    const [localDraftInputValue, setLocalDraftInputValue] = useState(draftInputValue);
    const [composing, setComposing] = useState(false);
    const [historyIndex, setHistoryIndex] = useState(-1);
    const [draftBeforeHistory, setDraftBeforeHistory] = useState<string | null>(null);
    const [historyEdits, setHistoryEdits] = useState<Record<number, string>>({});
    const [cancelPending, setCancelPending] = useState(false);
    const [dismissedProgressIds, setDismissedProgressIds] = useState<Set<string>>(new Set());
    const [pendingAttachments, setPendingAttachments] = useState<AttachmentInfo[]>([]);
    const [editingEntryId, setEditingEntryId] = useState<string | null>(null);
    const [themeMode, setThemeMode] = useState<'light' | 'dark'>(() => {
        if (typeof window === 'undefined') return 'light';
        try {
            return window.localStorage.getItem(AI_THEME_MODE_STORAGE_KEY) === 'dark' ? 'dark' : 'light';
        } catch {
            return 'light';
        }
    });
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const cancelRestoreSeqRef = useRef(0);
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const userScrolledUpRef = useRef(false);
    const prevMsgCountRef = useRef(0);
    const scrollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const prevReadyRef = useRef(ready);

    const { queue, addEntry, removeEntry, updateEntry, reorderEntry, extractEntry, clearQueue, restoreQueue } = useBufferQueue();

    const t = themeMode === 'dark' ? darkTheme : (inline ? lightTheme : overlayTheme);
    const showMaximizeToggle = inline && !!onToggleMaximize;

    // Workflow split-pane state
    const { state: workflowState, openDocPreview, closeDocPreview, setSplitRatio: setWorkflowSplitRatio, dismissMaximizeSuggestion, dismissDocsBar } = useWorkflowState();

    // Drag-to-resize input area height
    const [inputAreaHeight, setInputAreaHeight] = useState<number | null>(null);
    const isDraggingInputRef = useRef(false);
    const dragHandlePillRef = useRef<HTMLDivElement | null>(null);
    const dragCleanupRef = useRef<(() => void) | null>(null);

    // Cleanup drag listeners on unmount
    useEffect(() => {
        return () => { dragCleanupRef.current?.(); };
    }, []);

    // Code preview split-pane state (mutual exclusion with workflow preview)
    const { state: codePreviewState, closePanel: closeCodePreview, selectFile: selectCodeFile } = useCodePreviewState(workflowState.splitMode);

    // Determine which split-pane is active: workflow preview takes priority
    const showWorkflowPreview = workflowState.splitMode;
    const showCodePreview = !showWorkflowPreview && codePreviewState.active;
    const anySplitActive = showWorkflowPreview || showCodePreview;

    // Shared resize handler for split-pane drag handle (used by both WorkflowDocPreview and CodePreviewPanel)
    const handleSplitResizeStart = useCallback(() => {
        const container = document.querySelector('[data-testid="ai-panel-root"]')?.parentElement;
        if (!container) return;
        const onMouseMove = (e: MouseEvent) => {
            const rect = container.getBoundingClientRect();
            const newRatio = Math.max(0.2, Math.min(0.8, (e.clientX - rect.left) / rect.width));
            setWorkflowSplitRatio(newRatio);
        };
        const onMouseUp = () => {
            document.removeEventListener("mousemove", onMouseMove);
            document.removeEventListener("mouseup", onMouseUp);
            document.body.style.cursor = "";
            document.body.style.userSelect = "";
        };
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        document.addEventListener("mousemove", onMouseMove);
        document.addEventListener("mouseup", onMouseUp);
    }, [setWorkflowSplitRatio]);

    const title = localizeText(lang, "AI Assistant", "AI 助手");
    const thinkingText = localizeText(lang, "Thinking... (you can type ahead)", "正在思考...（可预输入）");
    const processingText = localizeText(lang, "Running tools... (you can type ahead)", "执行中...（可预输入）");
    const idlePlaceholderText = localizeText(lang, "Type a message...", "输入消息...");
    const savedFileLabel = localizeText(lang, "Saved file", "文件已保存");
    const isBusy = sending;
    // submitLocked: prevent sending messages while agent is working;
    // the textarea itself stays editable so users can type ahead.
    const submitLocked = isBusy || cancelPending;
    const prevSubmitLockedRef = useRef(submitLocked);
    const showThinkingState = streaming;
    const showProcessingState = isBusy && !streaming;
    const showBusySpinner = isBusy;

    const initStatusLabels: Record<AIAssistantInitStatus, { en: string; zhHans: string; zhHant: string }> = {
        connecting: { en: "Connecting to Hub...", zhHans: "正在连接 Hub...", zhHant: "正在連線 Hub..." },
        loading:    { en: "Loading components...", zhHans: "正在加载组件...", zhHant: "正在載入組件..." },
        warming:    { en: "Warming up...", zhHans: "正在预热...", zhHant: "正在預熱..." },
        ready:      { en: "Ready", zhHans: "就绪", zhHant: "就緒" },
    };
    const statusKey = initStatus ?? "connecting";
    const initLabel = localizeText(lang, initStatusLabels[statusKey].en, initStatusLabels[statusKey].zhHans, initStatusLabels[statusKey].zhHant);

    const queuePlaceholder = localizeText(lang, "Press Enter to queue...", "输入后按回车缓存...", "輸入後按 Enter 緩存...");
    const placeholderText = !ready
        ? initLabel
        : showThinkingState
            ? thinkingText
            : showProcessingState
                ? processingText
                : submitLocked
                    ? queuePlaceholder
                    : idlePlaceholderText;
    const inputValue = localDraftInputValue;
    const updateInputValue = useCallback((nextValue: string) => {
        setLocalDraftInputValue(nextValue);
        setDraftInputValue?.(nextValue);
    }, [setDraftInputValue]);
    const canSend = ready && (!!inputValue.trim() || pendingAttachments.length > 0 || selectedFilePaths.length > 0);
    const { pinnedNews, otherMessages } = useMemo(() => {
        const pinned: ChatMessage[] = [];
        const other: ChatMessage[] = [];
        for (const m of messages) {
            if (isPinnedNewsMessage(m)) {
                pinned.push(m);
            } else {
                other.push(m);
            }
        }
        return { pinnedNews: pinned.slice(0, 2), otherMessages: other };
    }, [messages]);
    const hasConversation = otherMessages.length + progressMessages.length > 0;

    const resizeInput = useCallback(() => {
        if (!inputRef.current) return;
        const el = inputRef.current;
        el.style.height = "auto";
        const contentH = el.scrollHeight;
        if (inputAreaHeight !== null) {
            // User has set a height via drag — use it as the floor, content can grow beyond
            el.style.height = Math.max(contentH, inputAreaHeight) + "px";
        } else {
            // Auto-size: grow with content up to 120px
            el.style.height = Math.min(contentH, 120) + "px";
        }
    }, [inputAreaHeight]);

    // Sync local draft from parent-owned draft state.
    useEffect(() => {
        setLocalDraftInputValue(draftInputValue);
    }, [draftInputValue]);

    useEffect(() => {
        try {
            window.localStorage.setItem(AI_THEME_MODE_STORAGE_KEY, themeMode);
        } catch {
            // ignore storage failures and keep in-memory theme mode
        }
        onThemeModeChange?.(themeMode);
    }, [themeMode, onThemeModeChange]);

    // Reset scroll flag when component mounts (panel opened/re-shown)
    useEffect(() => {
        userScrolledUpRef.current = false;
        outputEndRef.current?.scrollIntoView({ behavior: "auto" });
    }, []);

    // Debounced auto-scroll: coalesce rapid token updates into a single scroll
    useEffect(() => {
        if (userScrolledUpRef.current) {
            prevMsgCountRef.current = messages.length;
            return;
        }
        // New message added → scroll immediately
        if (messages.length !== prevMsgCountRef.current) {
            prevMsgCountRef.current = messages.length;
            outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
            return;
        }
        // Content update on existing message (streaming tokens) → debounce 80ms
        if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        scrollTimerRef.current = setTimeout(() => {
            outputEndRef.current?.scrollIntoView({ behavior: "auto" });
        }, 80);
        return () => {
            if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        };
    }, [messages]);

    useEffect(() => {
        if (!inputRef.current) return;
        resizeInput();
    }, [inputValue, resizeInput]);

    // Track user scroll position
    const handleScroll = useCallback(() => {
        const container = outputContainerRef.current;
        if (!container) return;
        const threshold = 80;
        userScrolledUpRef.current =
            container.scrollHeight - container.scrollTop - container.clientHeight > threshold;
    }, []);

    useEffect(() => {
        const becameReady = !prevReadyRef.current && ready;
        prevReadyRef.current = ready;
        if (!becameReady || userScrolledUpRef.current || !hasConversation) return;
        outputEndRef.current?.scrollIntoView({ behavior: "auto" });
    }, [ready, hasConversation]);

    // Scroll to top when pinned news are (re)loaded only if there is no
    // existing conversation yet, so reopening maclaw still shows the latest chat.
    useEffect(() => {
        if (!scrollToTopSeq || hasConversation) return;
        const container = outputContainerRef.current;
        if (container) {
            container.scrollTo({ top: 0, behavior: "smooth" });
            userScrolledUpRef.current = true; // prevent auto-scroll-to-bottom from overriding
        }
    }, [scrollToTopSeq, hasConversation]);

    // Focus input on mount
    useEffect(() => {
        const timer = setTimeout(() => inputRef.current?.focus(), 100);
        return () => clearTimeout(timer);
    }, []);

    // Escape key closes overlay mode, or exits maximized inline mode.
    useEffect(() => {
        if (!maximized && inline) return;
        const handler = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (inline && maximized) {
                onToggleMaximize?.();
                return;
            }
            if (!inline) onClose();
        };
        window.addEventListener("keydown", handler);
        return () => window.removeEventListener("keydown", handler);
    }, [onClose, inline, maximized, onToggleMaximize]);

    const handleSend = useCallback(async () => {
        const text = inputValue.trim();
        if (submitLocked) {
            // Queue mode: create BufferEntry
            if (!text && pendingAttachments.length === 0 && selectedFilePaths.length === 0) return;
            const attachments: AttachmentInfo[] = [...pendingAttachments];
            for (const fp of selectedFilePaths) {
                const fileName = fp.split(/[/\\]/).pop() || fp;
                const ext = '.' + (fileName.split('.').pop() || '').toLowerCase();
                attachments.push({
                    filePath: fp,
                    isImage: isImageFilePath(fp),
                    fileName,
                    extension: ext,
                });
            }
            addEntry(inputValue, attachments);
            recordSubmittedPrompt?.(inputValue);
            setHistoryIndex(-1);
            setDraftBeforeHistory(null);
            setHistoryEdits({});
            updateInputValue("");
            if (inputRef.current) {
                inputRef.current.style.height = "auto";
            }
            setPendingAttachments([]);
            clearSelectedFile?.();
            requestAnimationFrame(() => inputRef.current?.focus());
            return;
        }
        // Normal mode
        if (!text && selectedFilePaths.length === 0 && pendingAttachments.length === 0) return;
        // Collect all file paths: selectedFilePaths + pasted image attachments
        const allFilePaths: string[] = [...selectedFilePaths];
        for (const att of pendingAttachments) {
            if (att.filePath.trim()) {
                allFilePaths.push(att.filePath.trim());
            }
        }
        recordSubmittedPrompt?.(text);
        setHistoryIndex(-1);
        setDraftBeforeHistory(null);
        setHistoryEdits({});
        updateInputValue("");
        if (inputRef.current) {
            inputRef.current.style.height = "auto";
        }
        setPendingAttachments([]);
        clearSelectedFile?.();
        userScrolledUpRef.current = false;
        const outgoing = allFilePaths.length > 0
            ? buildOutgoingMessageMulti(text, allFilePaths)
            : text;
        await sendMessage(outgoing);
    }, [inputValue, selectedFilePaths, submitLocked, pendingAttachments, addEntry, updateInputValue, clearSelectedFile, recordSubmittedPrompt, sendMessage]);

    const applyInputValue = useCallback((nextValue: string) => {
        updateInputValue(nextValue);
        requestAnimationFrame(() => {
            resizeInput();
            if (!inputRef.current) return;
            inputRef.current.focus();
            const caret = nextValue.length;
            inputRef.current.setSelectionRange(caret, caret);
        });
    }, [resizeInput, updateInputValue]);

    const isSelectionCollapsedAtBoundary = useCallback((direction: 'up' | 'down') => {
        const input = inputRef.current;
        if (!input) return false;
        const { selectionStart, selectionEnd, value } = input;
        if (selectionStart !== selectionEnd) return false;
        if (selectionStart == null || selectionEnd == null) return false;
        if (direction === 'up') {
            return !value.slice(0, selectionStart).includes("\n");
        }
        return !value.slice(selectionEnd).includes("\n");
    }, []);

    const recallHistory = useCallback((direction: 'up' | 'down') => {
        if (submittedPrompts.length === 0) return false;

        const currentEdits = historyEdits;
        const currentHistoryIndex = historyIndex;
        const currentInputValue = inputValue;

        const rememberCurrentEntry = () => {
            if (currentHistoryIndex < 0) return;
            setHistoryEdits(prev => ({ ...prev, [currentHistoryIndex]: currentInputValue }));
        };

        if (direction === 'up') {
            if (currentHistoryIndex >= 0) {
                rememberCurrentEntry();
            } else {
                setDraftBeforeHistory(currentInputValue);
            }
            const nextIndex = currentHistoryIndex < 0 ? submittedPrompts.length - 1 : Math.max(0, currentHistoryIndex - 1);
            setHistoryIndex(nextIndex);
            applyInputValue(currentEdits[nextIndex] ?? submittedPrompts[nextIndex]);
            return true;
        }

        if (currentHistoryIndex < 0) return false;
        rememberCurrentEntry();
        if (currentHistoryIndex >= submittedPrompts.length - 1) {
            setHistoryIndex(-1);
            applyInputValue(draftBeforeHistory ?? "");
            return true;
        }
        const nextIndex = currentHistoryIndex + 1;
        setHistoryIndex(nextIndex);
        applyInputValue(currentEdits[nextIndex] ?? submittedPrompts[nextIndex]);
        return true;
    }, [submittedPrompts, historyIndex, inputValue, draftBeforeHistory, historyEdits, applyInputValue]);

    const exitHistoryBrowsing = useCallback(() => {
        if (historyIndex < 0) return false;
        setHistoryIndex(-1);
        setHistoryEdits({});
        applyInputValue(draftBeforeHistory ?? "");
        setDraftBeforeHistory(null);
        return true;
    }, [historyIndex, draftBeforeHistory, applyInputValue]);

    // ── Paste image handler (Task 7.1) ──
    const handlePaste = useCallback(async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
        const items = e.clipboardData?.items;
        if (!items) return;
        for (const item of Array.from(items)) {
            if (item.type.startsWith('image/')) {
                e.preventDefault();
                const blob = item.getAsFile();
                if (!blob) continue;
                const ext = blob.type === 'image/png' ? 'png' : 'jpg';
                try {
                    const base64 = await new Promise<string>((resolve, reject) => {
                        const reader = new FileReader();
                        reader.onload = () => {
                            const result = reader.result as string;
                            const base64Data = result.split(',')[1] || '';
                            resolve(base64Data);
                        };
                        reader.onerror = reject;
                        reader.readAsDataURL(blob);
                    });
                    const filePath = await savePastedImage(base64, ext);
                    const thumbnailDataUrl = URL.createObjectURL(blob);
                    const fileName = filePath.split(/[/\\]/).pop() || `paste.${ext}`;
                    setPendingAttachments(prev => [...prev, {
                        filePath,
                        thumbnailDataUrl,
                        isImage: true,
                        fileName,
                        extension: `.${ext}`,
                    }]);
                } catch (err) {
                    console.error("Failed to save pasted image:", err);
                }
                return;
            }
        }
        // Non-image paste: allow default behavior
    }, []);

    // ── submitLocked true→false transition: process next queued entry ──
    // Each entry gets its own independent agent loop. When the loop finishes
    // (submitLocked goes false again), the next entry is automatically processed.
    //
    // We read entry data directly from queue[0], remove it, then send.
    // sendMessage checks activeRoundRef (a ref, not state) synchronously —
    // it will accept the message because the round must be idle when
    // submitLocked just transitioned to false.
    useEffect(() => {
        if (prevSubmitLockedRef.current && !submitLocked && queue.length > 0) {
            const firstEntry = queue[0];
            const filePaths = firstEntry.attachments.map(a => a.filePath);
            const outgoing = filePaths.length > 0
                ? buildOutgoingMessageMulti(firstEntry.text, filePaths)
                : firstEntry.text;
            recordSubmittedPrompt?.(firstEntry.text);
            removeEntry(firstEntry.id);
            sendMessage(outgoing).catch(() => {
                // On failure, entry is already removed. Remaining queue
                // entries will be processed on next submitLocked transition.
            });
        }
        prevSubmitLockedRef.current = submitLocked;
    }, [submitLocked, queue.length, removeEntry, sendMessage, recordSubmittedPrompt]);

    // ── BufferQueuePanel edit handlers (Task 12.1) ──
    const handleEditEntry = useCallback((id: string) => setEditingEntryId(id), []);
    const handleCancelEdit = useCallback(() => setEditingEntryId(null), []);
    const handleSaveEdit = useCallback((id: string, text: string, attachments: AttachmentInfo[]) => {
        updateEntry(id, text, attachments);
        setEditingEntryId(null);
    }, [updateEntry]);

    // ── Fire (send) a single queued entry, interrupting the current session ──
    const handleFireEntry = useCallback(async (id: string) => {
        if (!cancelSession || cancelPending) return;

        const restoreSeq = ++cancelRestoreSeqRef.current;
        setCancelPending(true);
        try {
            await cancelSession();
            if (cancelRestoreSeqRef.current !== restoreSeq) return;

            // Extract AFTER cancelSession succeeds — if cancel fails or is
            // superseded, the entry stays in the queue instead of being lost.
            const extracted = extractEntry(id);
            if (!extracted) return;

            const outgoing = extracted.filePaths.length > 0
                ? buildOutgoingMessageMulti(extracted.text, extracted.filePaths)
                : extracted.text;
            recordSubmittedPrompt?.(extracted.text);
            await sendMessage(outgoing);
        } finally {
            setCancelPending(false);
        }
    }, [cancelSession, cancelPending, extractEntry, sendMessage, recordSubmittedPrompt]);

    const handleCancel = useCallback(async () => {
        if (!cancelSession || cancelPending) return;
        const restoreSeq = ++cancelRestoreSeqRef.current;
        const previousInputValue = inputValue;
        setCancelPending(true);
        try {
            const { canceledText } = await cancelSession();
            if (cancelRestoreSeqRef.current !== restoreSeq) return;
            if (draftInputValue === previousInputValue) {
                updateInputValue(canceledText);
            }
            setHistoryIndex(-1);
            setDraftBeforeHistory(null);
            setHistoryEdits({});
            requestAnimationFrame(() => {
                resizeInput();
                inputRef.current?.focus();
            });
        } finally {
            // Flush the first queued entry BEFORE clearing cancelPending.
            // cancelSession already reset activeRoundRef synchronously, so
            // sendMessage's idle-phase guard passes. Process one entry at a
            // time — remaining entries will be processed via the submitLocked
            // transition effect when this entry's agent loop finishes.
            if (queue.length > 0) {
                // Use extractEntry so that if the draining effect already
                // consumed this entry (race: agent completed while cancel
                // was in flight), extractEntry returns null and we skip the
                // duplicate send.
                const firstEntry = queue[0];
                const pending = extractEntry(firstEntry.id);
                if (pending) {
                    const outgoing = pending.filePaths.length > 0
                        ? buildOutgoingMessageMulti(pending.text, pending.filePaths)
                        : pending.text;
                    recordSubmittedPrompt?.(pending.text);
                    sendMessage(outgoing).catch(() => {});
                }
            }
            setCancelPending(false);
        }
    }, [cancelPending, cancelSession, inputValue, resizeInput, updateInputValue, queue, extractEntry, sendMessage, recordSubmittedPrompt]);

    const lastAssistantIdx = useMemo(() => findLastIndex(otherMessages, m => m.role === 'assistant'), [otherMessages]);
    const renderedOtherMessages = useMemo(() => {
        return otherMessages.map((msg, idx) => renderMessage(msg, executeAction, t, lang, idx === lastAssistantIdx, savedFileLabel));
    }, [otherMessages, executeAction, t, lastAssistantIdx, savedFileLabel]);

    // Clear dismissed progress IDs when progress messages reset (new request)
    useEffect(() => {
        if (progressMessages.length === 0 && dismissedProgressIds.size > 0) {
            setDismissedProgressIds(new Set());
        }
    }, [progressMessages.length]);

    const renderedProgressMessages = useMemo(() => {
        const dismissProgress = (id: string) => {
            setDismissedProgressIds(prev => {
                const next = new Set(prev);
                next.add(id);
                return next;
            });
        };
        return progressMessages
            .filter(msg => !dismissedProgressIds.has(msg.id))
            .map(msg => (
                <div key={msg.id} style={{ display: "flex", alignItems: "center", gap: "4px", color: t.textMuted, fontSize: "0.917em", padding: "1px 0", fontStyle: "italic" }}>
                    <span style={{ flex: 1 }}>{msg.content}</span>
                    <button
                        onClick={() => dismissProgress(msg.id)}
                        style={{
                            background: "none",
                            border: "none",
                            color: t.textMuted,
                            cursor: "pointer",
                            padding: "0 2px",
                            fontSize: "1em",
                            lineHeight: 1,
                            opacity: 0.6,
                            flexShrink: 0,
                        }}
                        onMouseEnter={e => { e.currentTarget.style.opacity = "1"; }}
                        onMouseLeave={e => { e.currentTarget.style.opacity = "0.6"; }}
                        title={localizeText(lang, "Dismiss", "关闭")}
                        aria-label={localizeText(lang, "Dismiss progress message", "关闭提示信息")}
                    >
                        ✕
                    </button>
                </div>
            ));
    }, [progressMessages, dismissedProgressIds, t, lang]);

    const containerStyle: React.CSSProperties = inline
        ? {
                display: "flex",
                flex: "1 1 0%",
                flexDirection: "column",
                background: t.bg,
                textAlign: "left",
                width: "100%",
                height: "100%",
                minWidth: 0,
                minHeight: 0,
                boxSizing: "border-box",
                position: "relative",
                overflow: "hidden",
            }
        : overlayStyle;

    return (
        <div style={{ display: "flex", width: "100%", height: "100%", overflow: "hidden", ...(maximized ? { position: "fixed" as const, inset: 0, zIndex: 12000, background: t.bg, boxShadow: "0 0 40px rgba(0,0,0,0.12)" } : {}) }}>
        <div data-testid="ai-panel-root" style={{...containerStyle, width: anySplitActive ? `${workflowState.splitRatio * 100}%` : "100%", flex: anySplitActive ? "none" : 1}}>
            {/* ── Drag overlay (inline mode) ── */}
            {inline && !maximized && (
                <div style={{
                    height: "30px", width: "100%",
                    position: "absolute", top: 0, left: 0, zIndex: 999,
                    '--wails-draggable': 'drag',
                } as any} />
            )}
            {/* ── Title bar ── */}
            <div
                data-testid="ai-title-bar"
                onDoubleClick={() => { if (inline) onToggleMaximize?.(); }}
                style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    minWidth: 0,
                    boxSizing: "border-box",
                    padding: "0 12px 0 10px",
                    height: "38px",
                    background: t.titleBarBg,
                    borderBottom: `1px solid ${t.titleBarBorder}`,
                    flexShrink: 0,
                    gap: "8px",
                    position: "relative",
                    zIndex: 1000,
                    ...(inline && !maximized ? { '--wails-draggable': 'drag' } as any : {}),
                }}>
                <div style={{ display: "flex", alignItems: "center", gap: "10px", minWidth: 0, flex: 1, overflow: "hidden" }}>
                    {!inline && (
                        <div style={{ display: "flex", gap: "5px", flexShrink: 0 }}>
                            <span
                                style={{ ...dotBase, background: "var(--theme-danger)" }}
                                onClick={onClose}
                                title={localizeText(lang, "Close", "关闭")}
                            />
                        </div>
                    )}
                    <span style={{
                        color: t.titleText, fontSize: "11px", fontWeight: 600, letterSpacing: "0.02em",
                        fontFamily: "'Segoe UI', 'SF Pro Text', system-ui, sans-serif",
                        overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
                        transform: "translateY(-0.5px)",
                    }}>{title}</span>
                    {trialReflectEnabled && (
                        <span style={{
                            fontSize: "10px",
                            lineHeight: 1,
                            padding: "3px 6px",
                            borderRadius: "999px",
                            background: "rgba(99, 102, 241, 0.12)",
                            color: t.headingColor,
                            border: `1px solid ${t.titleBarBorder}`,
                            flexShrink: 0,
                        }}>
                            {localizeText(lang, "Trial+Reflect", "试错反思")}
                        </span>
                    )}
                </div>
                <div style={{
                    display: "flex",
                    alignItems: "center",
                    flexShrink: 0,
                    minWidth: 0,
                    boxSizing: "border-box",
                    paddingRight: inline ? 0 : 2,
                    ...(inline ? { '--wails-draggable': 'no-drag', position: 'relative', zIndex: 1000 } as any : {}),
                }}>
                    <div data-testid="ai-titlebar-tools-group" style={{ display: "flex", gap: "6px", alignItems: "center", flexShrink: 0, minWidth: 0, paddingTop: 1 }}>
                    {onOpenTutorial && (
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: onOpenTutorial } : { onClick: onOpenTutorial })}
                        style={getTitleBarToolButtonStyle(t)}
                        title={localizeText(lang, "Tutorial", "教程")}
                    >
                        <span
                            aria-hidden="true"
                            style={{
                                fontSize: "16px",
                                lineHeight: 1,
                                transform: "translateY(-0.5px)",
                            }}
                        >
                            📚
                        </span>
                    </button>
                    )}
                    <div
                        data-testid="ai-theme-toggle-group"
                        style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: '2px',
                            padding: '2px',
                            borderRadius: '8px',
                            background: t.codeBlockBg,
                            border: `1px solid ${t.codeBlockBorder}`,
                        }}
                    >
                        <button
                            className="ai-titlebar-tool"
                            data-testid="ai-theme-toggle-light"
                            {...(inline ? {
                                onMouseDown: (e: React.MouseEvent<HTMLButtonElement>) => {
                                    e.preventDefault();
                                    e.stopPropagation();
                                    setThemeMode('light');
                                },
                            } : {
                                onClick: () => setThemeMode('light'),
                            })}
                            aria-label={localizeText(lang, 'Switch to normal mode', '切换到普通模式')}
                            aria-pressed={themeMode === 'light'}
                            style={{ ...getTitleBarToolButtonStyle(t, themeMode === 'light' ? 'active' : 'default'), width: 'auto', minWidth: '56px', padding: '0 10px', fontSize: '11px', fontWeight: 600 }}
                            title={localizeText(lang, 'Switch to normal mode', '切换到普通模式')}
                        >
                            {localizeText(lang, 'Normal', '普通')}
                        </button>
                        <button
                            className="ai-titlebar-tool"
                            data-testid="ai-theme-toggle-dark"
                            {...(inline ? {
                                onMouseDown: (e: React.MouseEvent<HTMLButtonElement>) => {
                                    e.preventDefault();
                                    e.stopPropagation();
                                    setThemeMode('dark');
                                },
                            } : {
                                onClick: () => setThemeMode('dark'),
                            })}
                            aria-label={localizeText(lang, 'Switch to dark mode', '切换到暗黑模式')}
                            aria-pressed={themeMode === 'dark'}
                            style={{ ...getTitleBarToolButtonStyle(t, themeMode === 'dark' ? 'active' : 'default'), width: 'auto', minWidth: '56px', padding: '0 10px', fontSize: '11px', fontWeight: 600 }}
                            title={localizeText(lang, 'Switch to dark mode', '切换到暗黑模式')}
                        >
                            {localizeText(lang, 'Dark', '暗黑')}
                        </button>
                    </div>
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: refreshNews } : { onClick: refreshNews })}
                        style={getTitleBarToolButtonStyle(t)}
                        title={localizeText(lang, "Refresh news", "刷新消息")}
                    >
                        <span
                            aria-hidden="true"
                            style={{
                                fontSize: "16px",
                                lineHeight: 1,
                                transform: "translateY(-0.5px)",
                            }}
                        >
                            ↻
                        </span>
                    </button>
                    <button
                        className="ai-titlebar-tool"
                        {...(inline ? { onMouseDown: clearHistory } : { onClick: clearHistory })}
                        style={getTitleBarToolButtonStyle(t, "danger")}
                        title={localizeText(lang, "Clear history", "清空历史")}
                    >
                        <span
                            aria-hidden="true"
                            style={{
                                fontSize: "16px",
                                lineHeight: 1,
                                transform: "translateY(-0.5px)",
                            }}
                        >
                            🗑
                        </span>
                    </button>
                    </div>
                    <div data-testid="ai-titlebar-window-group" style={{ display: "flex", gap: "2px", alignItems: "center", flexShrink: 0, minWidth: 0, boxSizing: "border-box", marginLeft: inline ? "16px" : "12px", paddingLeft: inline ? "14px" : "12px", paddingTop: 1, borderLeft: `1px solid ${t.titleBarBorder}` }}>
                    {inline && onHideWindow && (
                    <button
                        className="ai-window-control"
                        onMouseDown={(e) => { e.preventDefault(); e.stopPropagation(); onHideWindow(); }}
                        data-testid="ai-hide-toggle"
                        aria-label={localizeText(lang, "Minimize window", "最小化窗口")}
                        style={getWindowControlButtonStyle(t, "hide")}
                        title={localizeText(lang, "Minimize window", "最小化窗口")}
                    >
                        <span style={{ width: "10px", borderTop: "1.5px solid currentColor", transform: "translateY(4px)" }} />
                    </button>
                    )}
                    {showMaximizeToggle && (
                    <button
                        className="ai-window-control"
                        onMouseDown={(e) => { e.preventDefault(); e.stopPropagation(); onToggleMaximize?.(); }}
                        data-testid="ai-maximize-toggle"
                        aria-label={maximized ? localizeText(lang, "Restore window", "还原窗口") : localizeText(lang, "Maximize window", "最大化窗口")}
                        style={getWindowControlButtonStyle(t, "fullscreen", maximized)}
                        title={maximized ? localizeText(lang, "Restore window", "还原窗口") : localizeText(lang, "Maximize window", "最大化窗口")}
                    >
                        <span style={{
                            position: "relative",
                            width: "12px",
                            height: "12px",
                            display: "inline-block",
                        }}>
                            <span style={{
                                position: "absolute",
                                inset: maximized ? "2px 0 0 2px" : 0,
                                border: "1.5px solid currentColor",
                                borderRadius: "1px",
                                background: "transparent",
                            }} />
                            {maximized && (
                                <span style={{
                                    position: "absolute",
                                    inset: "0 2px 2px 0",
                                    border: "1.5px solid currentColor",
                                    borderRadius: "1px",
                                    background: t.titleBarBg,
                                }} />
                            )}
                        </span>
                    </button>
                    )}
                    {!inline && (
                    <button
                        className="ai-window-control"
                        onClick={onClose}
                        style={{ ...getWindowControlButtonStyle(t, "hide"), color: t.closeBtnColor, fontSize: "14px" }}
                        title={localizeText(lang, "Close", "关闭")}
                    >
                        ✕
                    </button>
                    )}
                    </div>
                </div>
            </div>

            <div data-testid="ai-panel-body" style={{
                display: "flex",
                flex: "1 1 0%",
                flexDirection: "column",
                minWidth: 0,
                minHeight: 0,
                overflow: "hidden",
                background: t.bg,
            }}>

            {/* Workflow maximize suggestion banner */}
            {workflowState.suggestMaximize && !maximized && inline && onToggleMaximize && (
                <div style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "8px 14px",
                    background: "linear-gradient(90deg, rgba(99,102,241,0.08), rgba(59,130,246,0.08))",
                    borderBottom: `1px solid ${t.titleBarBorder}`,
                    fontSize: "13px",
                    gap: "10px",
                    flexShrink: 0,
                }}>
                    <span style={{ color: t.text }}>
                        🚀 即将进入「{workflowTypeLabel(workflowState.suggestMaximizeType, lang)}」流程，全屏模式体验更佳
                    </span>
                    <div style={{ display: "flex", gap: "6px", flexShrink: 0 }}>
                        <button
                            onClick={() => { onToggleMaximize(); dismissMaximizeSuggestion(); }}
                            style={{
                                padding: "4px 12px",
                                fontSize: "12px",
                                border: "1px solid rgba(99,102,241,0.3)",
                                borderRadius: "4px",
                                background: "rgba(99,102,241,0.1)",
                                color: "rgb(99,102,241)",
                                cursor: "pointer",
                                fontWeight: 500,
                            }}
                        >
                            全屏
                        </button>
                        <button
                            onClick={dismissMaximizeSuggestion}
                            style={{
                                padding: "4px 8px",
                                fontSize: "12px",
                                border: "none",
                                borderRadius: "4px",
                                background: "transparent",
                                color: t.textMuted,
                                cursor: "pointer",
                            }}
                        >
                            稍后
                        </button>
                    </div>
                </div>
            )}

            {/* Workflow working directory banner */}
            {workflowState.workingDir && (workflowState.suggestMaximize || workflowState.active) && (
                <div style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "6px 14px",
                    background: "rgba(59,130,246,0.06)",
                    borderBottom: `1px solid ${t.titleBarBorder}`,
                    fontSize: "12px",
                    gap: "8px",
                    flexShrink: 0,
                }}>
                    <span style={{ color: t.textMuted, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        📁 工作目录：{workflowState.workingDir}
                    </span>
                    <button
                        onClick={async () => {
                            try {
                                const dir = await SelectProjectDir();
                                if (dir) {
                                    await SetWorkflowWorkingDir(dir);
                                }
                            } catch (_) { /* user cancelled */ }
                        }}
                        style={{
                            padding: "2px 8px",
                            fontSize: "11px",
                            border: `1px solid ${t.titleBarBorder}`,
                            borderRadius: "3px",
                            background: "transparent",
                            color: t.textMuted,
                            cursor: "pointer",
                            flexShrink: 0,
                        }}
                    >
                        更改
                    </button>
                </div>
            )}

            {/* Workflow phase transition notification */}
            {workflowState.transientText && (
                <div style={{
                    padding: "6px 14px",
                    background: "rgba(16,185,129,0.08)",
                    borderBottom: `1px solid ${t.titleBarBorder}`,
                    fontSize: "13px",
                    color: t.text,
                    flexShrink: 0,
                }}>
                    {workflowState.transientText}
                </div>
            )}
            {/* ── Chat area ── */}
            <div
                ref={outputContainerRef}
                data-testid="ai-output-container"
                style={{
                    flex: "1 1 0%",
                    minWidth: 0,
                    minHeight: 0,
                    boxSizing: "border-box",
                    maxHeight: "none",
                    padding: "8px 10px",
                    fontSize: `${chatFontSize}px`,
                    lineHeight: 1.5,
                    overflowY: "auto",
                    overflowX: "hidden",
                    textAlign: "left",
                    color: t.text,
                    background: t.bg,
                    fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace",
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-all",
                }}
                onScroll={handleScroll}
            >
                {onboardingIncomplete ? (
                    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "16px" }}>
                        <div style={{ color: t.textMuted, fontSize: "13px" }}>
                            {localizeText(lang, "Setup not completed", "设置未完成")}
                        </div>
                        <button
                            onClick={onOpenOnboarding}
                            style={{
                                padding: "10px 28px", fontSize: "15px", fontWeight: 600,
                                background: "linear-gradient(135deg, var(--theme-primary), var(--theme-primary-strong))",
                                color: "var(--theme-text-primary)", border: "none", borderRadius: "8px",
                                cursor: "pointer", transition: "opacity 0.2s",
                            }}
                            onMouseEnter={e => (e.currentTarget.style.opacity = "0.85")}
                            onMouseLeave={e => (e.currentTarget.style.opacity = "1")}
                        >
                            {localizeText(lang, "Complete Setup", "完成设置")}
                        </button>
                    </div>
                ) : !ready ? (
                    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "12px" }}>
                        <div style={{
                            width: "28px", height: "28px",
                            border: `3px solid ${t.inputBarBorder}`,
                            borderTop: `3px solid ${t.promptColor}`,
                            borderRadius: "50%",
                            animation: "maclaw-spin 0.8s linear infinite",
                        }} />
                        <div style={{ color: t.textMuted, fontSize: "12px" }}>
                            {initLabel}
                        </div>
                    </div>
                ) : messages.length === 0 ? (
                    <span style={{ color: t.emptyHint }}>
                        {localizeText(lang, "Ask me anything...", "有什么可以帮你的？")}
                    </span>
                ) : (
                    <>
                        {pinnedNews.length > 0 && (
                            <div style={{
                                display: 'grid',
                                gridTemplateColumns: pinnedNews.length >= 2 ? '1fr 1fr' : '1fr',
                                gap: '6px',
                                marginBottom: '6px',
                            }}>
                                {pinnedNews.map(msg => {
                                    const news = msg.news;
                                    if (!news) return null;
                                    const tooltipText = news.title + (news.body ? '\n' + news.body : '');
                                    return (
                                    <div key={msg.id} className="pinned-news-card" title={tooltipText} style={{
                                        padding: "6px 8px",
                                        borderRadius: "6px",
                                        background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(139,92,246,0.06))",
                                        borderLeft: `3px solid ${t.promptColor}`,
                                        color: t.text,
                                        fontSize: "0.917em",
                                        lineHeight: "1.4",
                                        overflow: "hidden",
                                    }}>
                                        <div style={{
                                            overflow: "hidden",
                                            textOverflow: "ellipsis",
                                            whiteSpace: "nowrap",
                                            fontWeight: 600,
                                        }}>
                                            <span>{news.icon} </span>
                                            {renderInlineMarkdown(news.title, t)}
                                        </div>
                                        {news.body && (
                                        <div style={{
                                            overflow: "hidden",
                                            display: "-webkit-box",
                                            WebkitLineClamp: 2,
                                            WebkitBoxOrient: "vertical" as any,
                                            marginTop: "2px",
                                            color: t.textMuted,
                                        }}>
                                            {renderInlineMarkdown(news.body, t)}
                                        </div>
                                        )}
                                    </div>
                                    );
                                })}
                            </div>
                        )}
                        {renderedOtherMessages}
                        {renderedProgressMessages}
                    </>
                )}
                {showThinkingState && (
                    <div style={{ color: t.textMuted, fontSize: "0.917em", padding: "4px 0", fontStyle: "italic" }}>
                        {thinkingText}
                    </div>
                )}
                {showProcessingState && (
                    <div style={{ color: t.textMuted, fontSize: "0.917em", padding: "4px 0", fontStyle: "italic" }}>
                        {processingText}
                    </div>
                )}
                <div ref={outputEndRef} />
            </div>

            {/* ── Workflow document links bar ── */}
            {workflowState.phaseDocuments.size > 0 && !workflowState.docsBarDismissed && (
                <div data-testid="ai-workflow-docs-bar" style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "6px",
                    padding: "6px 14px",
                    borderTop: `1px solid ${t.divider}`,
                    background: t.bg,
                    flexShrink: 0,
                    flexWrap: "wrap",
                }}>
                    <span style={{ fontSize: "11px", color: t.textMuted, flexShrink: 0 }}>📋</span>
                    {Array.from(workflowState.phaseDocuments.keys()).map(pid => {
                        const fileNames: Record<string, string> = {
                            requirements: "requirements.md",
                            design: "design.md",
                            tech_design: "design.md",
                            tasks: "tasks.md",
                            task_breakdown: "tasks.md",
                        };
                        const labels: Record<string, string> = {
                            requirements: "需求文档",
                            design: "设计文档",
                            tech_design: "设计文档",
                            tasks: "任务列表",
                            task_breakdown: "任务列表",
                        };
                        const isActive = workflowState.splitMode && workflowState.currentPhaseID === pid;
                        return (
                            <button
                                key={pid}
                                onClick={() => openDocPreview(pid)}
                                style={{
                                    display: "inline-flex",
                                    alignItems: "center",
                                    gap: "4px",
                                    padding: "3px 8px",
                                    fontSize: "12px",
                                    fontFamily: "'Cascadia Code', 'Fira Code', Consolas, monospace",
                                    border: `1px solid ${isActive ? t.headingColor : t.divider}`,
                                    borderRadius: "4px",
                                    background: isActive ? (t === darkTheme ? "rgba(99,102,241,0.15)" : "rgba(99,102,241,0.08)") : "transparent",
                                    color: isActive ? t.headingColor : t.linkColor,
                                    cursor: "pointer",
                                    textDecoration: "none",
                                    lineHeight: 1.3,
                                }}
                                title={labels[pid] || pid}
                            >
                                <span style={{ fontSize: "13px" }}>📄</span>
                                {fileNames[pid] || `${pid}.md`}
                            </button>
                        );
                    })}
                    <button
                        data-testid="ai-workflow-docs-bar-close"
                        onClick={dismissDocsBar}
                        style={{
                            display: "inline-flex",
                            alignItems: "center",
                            justifyContent: "center",
                            width: "20px",
                            height: "20px",
                            padding: 0,
                            marginLeft: "auto",
                            fontSize: "14px",
                            lineHeight: 1,
                            border: `1px solid ${t.divider}`,
                            borderRadius: "4px",
                            background: "transparent",
                            color: t.textMuted,
                            cursor: "pointer",
                            flexShrink: 0,
                        }}
                        title={localizeText(lang, "Close", "关闭")}
                    >
                        ×
                    </button>
                </div>
            )}

            {/* ── Buffer Queue Panel ── */}
            <BufferQueuePanel
                queue={queue}
                lang={lang}
                theme={{
                    bg: t.bg,
                    text: t.text,
                    textMuted: t.textMuted,
                    headingColor: t.headingColor,
                    inputBarBg: t.inputBarBg,
                    inputBarBorder: t.inputBarBorder,
                    codeBlockBg: t.codeBlockBg,
                    codeBlockBorder: t.codeBlockBorder,
                    divider: t.divider,
                }}
                editingEntryId={editingEntryId}
                onEdit={handleEditEntry}
                onCancelEdit={handleCancelEdit}
                onSaveEdit={handleSaveEdit}
                onDelete={removeEntry}
                onReorder={reorderEntry}
                onFireEntry={submitLocked ? handleFireEntry : undefined}
            />

            {/* ── Drag handle to resize input area ── */}
            <div
                data-testid="ai-input-resize-handle"
                role="separator"
                aria-orientation="horizontal"
                aria-label="Resize input area"
                aria-valuenow={inputAreaHeight ?? 36}
                aria-valuemin={36}
                aria-valuemax={400}
                style={{
                    height: "10px",
                    flexShrink: 0,
                    cursor: "row-resize",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    userSelect: "none",
                    touchAction: "none",
                }}
                onMouseEnter={() => {
                    if (dragHandlePillRef.current) dragHandlePillRef.current.style.opacity = "1";
                }}
                onMouseLeave={() => {
                    if (dragHandlePillRef.current && !isDraggingInputRef.current) dragHandlePillRef.current.style.opacity = "0.45";
                }}
                onMouseDown={(e) => {
                    e.preventDefault();
                    isDraggingInputRef.current = true;
                    const startY = e.clientY;
                    const startH = inputRef.current ? inputRef.current.offsetHeight : 36;
                    if (dragHandlePillRef.current) dragHandlePillRef.current.style.opacity = "1";
                    const onMouseMove = (ev: MouseEvent) => {
                        const delta = startY - ev.clientY;
                        const newH = Math.max(36, Math.min(400, startH + delta));
                        setInputAreaHeight(newH);
                    };
                    const cleanup = () => {
                        isDraggingInputRef.current = false;
                        if (dragHandlePillRef.current) dragHandlePillRef.current.style.opacity = "0.45";
                        document.removeEventListener("mousemove", onMouseMove);
                        document.removeEventListener("mouseup", onMouseUp);
                        document.body.style.cursor = "";
                        document.body.style.userSelect = "";
                        dragCleanupRef.current = null;
                    };
                    const onMouseUp = () => cleanup();
                    document.body.style.cursor = "row-resize";
                    document.body.style.userSelect = "none";
                    document.addEventListener("mousemove", onMouseMove);
                    document.addEventListener("mouseup", onMouseUp);
                    dragCleanupRef.current = cleanup;
                }}
                onDoubleClick={() => {
                    // Double-click to reset to auto-size
                    setInputAreaHeight(null);
                }}
            >
                <div ref={dragHandlePillRef} style={{
                    width: "36px",
                    height: "3px",
                    borderRadius: "2px",
                    background: t.divider,
                    opacity: 0.45,
                    transition: "opacity 0.15s",
                }} />
            </div>

            {/* ── Input bar ── */}
            <div data-testid="ai-input-bar" style={{
                display: "flex",
                flexDirection: "column",
                gap: "6px",
                minWidth: 0,
                boxSizing: "border-box",
                padding: "6px 12px",
                paddingBottom: "max(6px, env(safe-area-inset-bottom))",
                background: t.inputBarBg,
                borderTop: inline ? `1px solid ${t.inputBarBorder}` : "none",
                flexShrink: 0,
                ...(inline ? {} : { margin: "0 10px 10px 10px", borderRadius: "8px", border: `1.5px solid ${t.inputBarBorder}` }),
            }}>
                {selectedFilePaths.length > 0 && (
                    <div
                        role="list"
                        aria-label={localizeText(lang, "Selected files", "已选文件")}
                        style={{
                            display: "flex",
                            flexWrap: "wrap",
                            gap: "4px",
                            padding: "6px 8px 4px",
                            alignItems: "center",
                        }}
                    >
                        {selectedFilePaths.map((fp, idx) => (
                            <FileCard
                                key={fp}
                                filePath={fp}
                                index={idx}
                                theme={t}
                                lang={lang}
                                onRemove={removeSelectedFile}
                            />
                        ))}
                        {selectedFilePaths.length > 1 && (
                            <button
                                type="button"
                                onClick={clearSelectedFile}
                                style={{
                                    border: "none",
                                    background: "transparent",
                                    color: t.textMuted,
                                    cursor: "pointer",
                                    fontSize: "11px",
                                    padding: "2px 4px",
                                    borderRadius: "4px",
                                    lineHeight: 1,
                                    flexShrink: 0,
                                }}
                                title={localizeText(lang, "Clear all files", "清除所有文件")}
                                onMouseEnter={e => (e.currentTarget.style.color = t.errorText)}
                                onMouseLeave={e => (e.currentTarget.style.color = t.textMuted)}
                            >
                                {localizeText(lang, "clear all", "全部清除")}
                            </button>
                        )}
                    </div>
                )}
                <div style={{
                    display: "flex", alignItems: "flex-end", gap: "8px", minWidth: 0,
                }}>
                    <span style={{
                        color: t.promptColor, fontFamily: "Consolas, monospace",
                        fontSize: "13px", flexShrink: 0, userSelect: "none",
                        paddingBottom: "8px",
                    }}>❯</span>
                    <div style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column" }}>
                    <textarea
                        ref={inputRef}
                        data-testid="ai-input"
                        disabled={!ready}
                        style={{
                            flex: 1, minWidth: 0, background: "transparent",
                            border: "none", outline: "none", color: t.inputText,
                            fontFamily: "Consolas, 'Courier New', monospace",
                            fontSize: "14px", padding: "8px 0",
                            resize: "none", overflow: "auto",
                            minHeight: inputAreaHeight !== null ? `${inputAreaHeight}px` : "36px",
                            maxHeight: inputAreaHeight !== null ? "400px" : "120px",
                            lineHeight: 1.4,
                            opacity: !ready ? 0.5 : 1,
                            cursor: !ready ? "default" : "text",
                        }}
                        rows={1}
                        value={inputValue}
                        onChange={(e) => {
                            if (historyIndex >= 0) {
                                setHistoryEdits(prev => ({ ...prev, [historyIndex]: e.target.value }));
                            }
                            updateInputValue(e.target.value);
                            resizeInput();
                        }}
                        onCompositionStart={() => setComposing(true)}
                        onCompositionEnd={() => setComposing(false)}
                        onPaste={handlePaste}
                        onKeyDown={(e) => {
                            if (composing) return;
                            if (e.key === "ArrowUp" && isSelectionCollapsedAtBoundary('up')) {
                                if (recallHistory('up')) {
                                    e.preventDefault();
                                    return;
                                }
                            }
                            if (e.key === "ArrowDown" && isSelectionCollapsedAtBoundary('down')) {
                                if (recallHistory('down')) {
                                    e.preventDefault();
                                    return;
                                }
                            }
                            if (e.key === "Escape") {
                                if (exitHistoryBrowsing()) {
                                    e.preventDefault();
                                }
                                return;
                            }
                            if (e.key === "Enter" && !e.shiftKey) {
                                e.preventDefault();
                                handleSend();
                            }
                        }}
                        placeholder={placeholderText}
                        autoCapitalize="off"
                        autoCorrect="off"
                        spellCheck={false}
                    />
                    {pendingAttachments.length > 0 && (
                        <div style={{ display: "flex", gap: "4px", flexWrap: "wrap", padding: "4px 0" }}>
                            {pendingAttachments.map((att, idx) => (
                                <div key={att.filePath} style={{ position: "relative", display: "inline-block" }}>
                                    {att.thumbnailDataUrl ? (
                                        <img src={att.thumbnailDataUrl} alt={att.fileName} style={{ width: "40px", height: "40px", objectFit: "cover", borderRadius: "4px", border: `1px solid ${t.codeBlockBorder}` }} />
                                    ) : (
                                        <span style={{ fontSize: "24px" }}>🖼️</span>
                                    )}
                                    <button onClick={() => setPendingAttachments(prev => prev.filter((_, i) => i !== idx))} style={{ position: "absolute", top: "-4px", right: "-4px", background: t.bg, border: `1px solid ${t.codeBlockBorder}`, borderRadius: "50%", width: "16px", height: "16px", fontSize: "10px", cursor: "pointer", display: "flex", alignItems: "center", justifyContent: "center", color: t.textMuted, padding: 0, lineHeight: 1 }} aria-label="Remove attachment">✕</button>
                                </div>
                            ))}
                        </div>
                    )}
                    </div>
                    <button
                        type="button"
                        onClick={browseFile}
                        disabled={!ready}
                        style={{
                            ...baseInputBtnStyle,
                            color: t.pathColor,
                            borderColor: t.pathColor,
                            opacity: !ready ? 0.5 : 1,
                            marginBottom: "4px",
                        }}
                        title={localizeText(lang, "Choose file", "选择文件")}
                    >
                        📎
                    </button>
                    {isBusy && cancelSession ? (
                        <button
                            type="button"
                            onClick={handleCancel}
                            data-testid="ai-cancel-progress"
                            style={{
                                ...baseInputBtnStyle,
                                display: "inline-flex",
                                alignItems: "center",
                                justifyContent: "center",
                                width: inline ? "60px" : "64px",
                                padding: 0,
                                marginBottom: "4px",
                                background: inline ? "transparent" : "rgba(99, 102, 241, 0.08)",
                                borderColor: inline ? colors.primary : "var(--theme-primary-strong)",
                                color: inline ? colors.primary : "var(--theme-primary-strong)",
                            }}
                            title={localizeText(lang, "Cancel", "取消")}
                            aria-label={localizeText(lang, "Cancel", "取消")}
                        >
                            {showBusySpinner ? (
                                <span
                                    aria-hidden="true"
                                    style={{
                                        width: "18px",
                                        height: "18px",
                                        borderRadius: "50%",
                                        border: `2px solid ${inline ? "var(--theme-primary-soft)" : "rgba(124, 58, 237, 0.22)"}`,
                                        borderTopColor: inline ? colors.primary : "var(--theme-primary-strong)",
                                        borderRightColor: inline ? colors.primary : "var(--theme-primary-strong)",
                                        animation: "ai-spinner-spin 0.8s linear infinite",
                                    }}
                                />
                            ) : (
                                <span
                                    aria-hidden="true"
                                    style={{
                                        fontSize: "16px",
                                        lineHeight: 1,
                                        fontWeight: 700,
                                    }}
                                >
                                    ■
                                </span>
                            )}
                            <span style={{ position: "absolute", opacity: 0, pointerEvents: "none" }}>
                                {localizeText(lang, "Cancel", "取消")}
                            </span>
                        </button>
                    ) : (
                        <button
                            type="button"
                            onClick={handleSend}
                            disabled={!canSend}
                            style={{
                                ...baseInputBtnStyle,
                                ...(inline
                                    ? { color: t.sendBtnColor, borderColor: t.sendBtnBorder }
                                    : { color: t.sendBtnColor, background: t.sendBtnBorder, borderColor: t.sendBtnBorder, borderRadius: "6px" }),
                                opacity: canSend ? 1 : 0.5,
                                marginBottom: "4px",
                            }}
                            title={localizeText(lang, "Send", "发送")}
                        >
                            {isBusy ? "…" : "⏎"}
                        </button>
                    )}
                </div>
            </div>
        </div>
        </div>
        {showWorkflowPreview && (
            <div style={{ flex: 1, minWidth: 0, height: "100%" }}>
                <WorkflowDocPreview
                    phaseDocuments={workflowState.phaseDocuments}
                    currentPhaseID={workflowState.currentPhaseID}
                    gateResults={workflowState.gateResults}
                    onClose={closeDocPreview}
                    onToggleMaximize={inline ? onToggleMaximize : undefined}
                    theme={{
                        bg: t.bg,
                        text: t.text,
                        textMuted: t.textMuted,
                        border: t.divider,
                        headerBg: t.titleBarBg,
                        accentColor: t.headingColor,
                        accentBg: t === darkTheme ? "rgba(99,102,241,0.15)" : "rgba(99,102,241,0.08)",
                        codeBg: t.codeBg,
                        codeText: t.codeText,
                        codeBlockBg: t.codeBlockBg,
                        codeBlockBorder: t.codeBlockBorder,
                        headingColor: t.headingColor,
                        linkColor: t.linkColor,
                        quoteBorder: t.quoteBorder,
                        quoteText: t.quoteText,
                        quoteBg: t === darkTheme ? "rgba(99,102,241,0.08)" : "rgba(99,102,241,0.04)",
                    }}
                    onResizeStart={handleSplitResizeStart}
                />
            </div>
        )}
        {showCodePreview && (
            <div style={{ flex: 1, minWidth: 0, height: "100%" }}>
                <CodePreviewPanel
                    files={codePreviewState.files}
                    activeFilePath={codePreviewState.activeFilePath}
                    onSelectFile={selectCodeFile}
                    onClose={closeCodePreview}
                    onToggleMaximize={inline ? onToggleMaximize : undefined}
                    onResizeStart={handleSplitResizeStart}
                    theme={themeMode === 'dark' ? darkCodePreviewTheme : lightCodePreviewTheme}
                />
            </div>
        )}
        </div>
    );
}
