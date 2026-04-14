import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { colors } from "../remote/styles";
import { OpenFileOrShowInFolder, SelectProjectDir, SetWorkflowWorkingDir } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import type { ChatMessage, CancelAIAssistantResult, ChatAction, AIAssistantInitStatus, ChatConfirmation, ChatUnfinishedSlot } from "./useAIAssistant";
import { findLastIndex, isPinnedNewsMessage } from "./useAIAssistant";
import { useWorkflowState } from "./useWorkflowState";
import { WorkflowDocPreview, type DocPreviewTheme } from "./WorkflowDocPreview";

interface AIAssistantPanelStateProps {
    messages: ChatMessage[];
    progressMessages?: ChatMessage[];
    sending: boolean;
    streaming: boolean;
    visualBusy?: boolean;
    ready: boolean;
    initStatus?: AIAssistantInitStatus;
    selectedFilePath?: string;
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
    state: AIAssistantPanelStateProps;
    actions: AIAssistantPanelActionProps;
    window?: AIAssistantPanelWindowProps;
    onThemeModeChange?: (mode: 'light' | 'dark') => void;
}

const AI_THEME_MODE_STORAGE_KEY = "ai_assistant_theme_mode";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

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
            parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{m.slice(1, -1)}</code>);
        } else if (match[2]) {
            parts.push(<strong key={idx++} style={{ color: t.boldColor, fontWeight: 700 }}>{m.slice(2, -2)}</strong>);
        } else if (match[3]) {
            parts.push(<em key={idx++} style={{ color: t.italicColor }}>{m.slice(1, -1)}</em>);
        } else if (match[4]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(e) => { e.preventDefault(); BrowserOpenURL(href); }} style={{ color: t.linkColor, textDecoration: "underline", cursor: "pointer" }}>{lm[1]}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: t.linkColor }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        } else if (match[5] || match[6] || match[7] || match[9]) {
            // Trim trailing punctuation/whitespace that isn't part of the path
            const filePath = m.replace(/[\s,;:!?。，；：！？）\]]+$/, "");
            if (filePath.length !== m.length) {
                // Rewind regex lastIndex so trimmed chars are re-processed
                re.lastIndex -= (m.length - filePath.length);
            }
            parts.push(
                <a key={idx++}
                   href="#"
                   onClick={(event) => openFileInFolder(event, filePath)}
                   style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }}
                   title={filePath}
                >📂 {filePath}</a>
            );
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

function renderContentWithCodeBlocks(content: string, t: Theme): React.ReactNode[] {
    const elements: React.ReactNode[] = [];
    const lines = content.split("\n");
    let inCodeBlock = false;
    let codeBlockLines: string[] = [];
    let codeBlockLang = "";
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

    for (const line of lines) {
        if (/^```/.test(line.trimStart())) {
            if (inCodeBlock) {
                flushCodeBlock();
                inCodeBlock = false;
            } else {
                inCodeBlock = true;
                codeBlockLang = line.trimStart().slice(3).trim();
            }
        } else if (inCodeBlock) {
            codeBlockLines.push(line);
        } else {
            elements.push(renderMarkdownLine(line, `md-${lineIdx}`, t));
        }
        lineIdx++;
    }
    if (inCodeBlock) flushCodeBlock();
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
                        fontSize: "12px",
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
                        fontSize: "12px",
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
            <div style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "4px" }}>{title}</div>
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
                <div data-testid="confirmation-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
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
                <div data-testid="unfinished-slot-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
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
                <div key={msg.id} style={{ color: t.textMuted, fontSize: "11px", padding: "1px 0", fontStyle: "italic" }}>
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
                    fontSize: "12px",
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
                    fontSize: "12px",
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

/* ── Main component ── */

export function AIAssistantPanel({ onClose, lang, state, actions, window: panelWindow, onThemeModeChange }: AIAssistantPanelProps) {
    const {
        messages,
        progressMessages = [],
        sending,
        streaming,
        visualBusy,
        ready,
        initStatus,
        selectedFilePath = "",
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

    const t = themeMode === 'dark' ? darkTheme : (inline ? lightTheme : overlayTheme);
    const showMaximizeToggle = inline && !!onToggleMaximize;

    // Workflow split-pane state
    const { state: workflowState, openDocPreview, closeDocPreview, setSplitRatio: setWorkflowSplitRatio, dismissMaximizeSuggestion } = useWorkflowState();

    const title = localizeText(lang, "AI Assistant", "AI 助手");
    const thinkingText = localizeText(lang, "Thinking...", "正在思考...");
    const processingText = localizeText(lang, "Executing tools and finishing task...", "正在执行工具并完成任务...");
    const idlePlaceholderText = localizeText(lang, "Type a message...", "输入消息...");
    const savedFileLabel = localizeText(lang, "Saved file", "文件已保存");
    const isBusy = sending;
    const inputLocked = isBusy || cancelPending;
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

    const placeholderText = !ready
        ? initLabel
        : showThinkingState
            ? thinkingText
            : showProcessingState
                ? processingText
                : idlePlaceholderText;
    const inputValue = localDraftInputValue;
    const updateInputValue = useCallback((nextValue: string) => {
        setLocalDraftInputValue(nextValue);
        setDraftInputValue?.(nextValue);
    }, [setDraftInputValue]);
    const canSend = ready && !inputLocked && (!!inputValue.trim() || !!selectedFilePath.trim());
    const selectedFileName = selectedFilePath ? selectedFilePath.split(/[/\\]/).pop() || selectedFilePath : "";
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
        inputRef.current.style.height = "auto";
        inputRef.current.style.height = Math.min(inputRef.current.scrollHeight, 120) + "px";
    }, []);

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
        if ((!text && !selectedFilePath.trim()) || isBusy) return;
        recordSubmittedPrompt?.(text);
        setHistoryIndex(-1);
        setDraftBeforeHistory(null);
        setHistoryEdits({});
        updateInputValue("");
        if (inputRef.current) {
            inputRef.current.style.height = "auto";
        }
        userScrolledUpRef.current = false;
        await sendMessage(text);
    }, [inputValue, selectedFilePath, isBusy, recordSubmittedPrompt, sendMessage, updateInputValue]);

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
            setCancelPending(false);
        }
    }, [cancelPending, cancelSession, inputValue, resizeInput, updateInputValue]);

    const lastAssistantIdx = useMemo(() => findLastIndex(otherMessages, m => m.role === 'assistant'), [otherMessages]);
    const renderedOtherMessages = useMemo(() => {
        return otherMessages.map((msg, idx) => renderMessage(msg, executeAction, t, lang, idx === lastAssistantIdx, savedFileLabel));
    }, [otherMessages, executeAction, t, lastAssistantIdx, savedFileLabel]);

    const renderedProgressMessages = useMemo(() => {
        return progressMessages.map(msg => renderMessage(msg, executeAction, t, lang, false, savedFileLabel));
    }, [progressMessages, executeAction, t, lang, savedFileLabel]);

    const containerStyle: React.CSSProperties = inline
        ? (maximized
            ? maximizedInlineStyle
            : {
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
            })
        : overlayStyle;

    return (
        <div style={{ display: "flex", width: "100%", height: "100%", overflow: "hidden" }}>
        <div data-testid="ai-panel-root" style={{...containerStyle, width: workflowState.splitMode ? `${workflowState.splitRatio * 100}%` : "100%", flex: workflowState.splitMode ? "none" : 1}}>
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
                        🚀 即将进入「{workflowState.suggestMaximizeType}」流程，全屏模式体验更佳
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
                    fontSize: "12px",
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
                                        fontSize: "11px",
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
                    <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>
                        {thinkingText}
                    </div>
                )}
                {showProcessingState && (
                    <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>
                        {processingText}
                    </div>
                )}
                <div ref={outputEndRef} />
            </div>

            {/* ── Workflow document links bar ── */}
            {workflowState.phaseDocuments.size > 0 && (
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
                </div>
            )}

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
                {selectedFilePath && (
                    <div style={{
                        display: "flex",
                        alignItems: "center",
                        gap: "8px",
                        minWidth: 0,
                        padding: "6px 8px",
                        borderRadius: "6px",
                        background: t.codeBlockBg,
                        border: `1px solid ${t.codeBlockBorder}`,
                        color: t.text,
                        fontSize: "12px",
                    }}>
                        <span style={{ color: t.pathColor, flexShrink: 0 }}>📎</span>
                        <div style={{ minWidth: 0, flex: 1 }} title={selectedFilePath}>
                            <div style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", fontWeight: 600 }}>
                                {selectedFileName}
                            </div>
                            <div style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", color: t.textMuted, fontSize: "11px" }}>
                                {selectedFilePath}
                            </div>
                        </div>
                        <button
                            type="button"
                            onClick={clearSelectedFile}
                            disabled={inputLocked}
                            style={{
                                ...baseActionBtnStyle,
                                color: t.errorText,
                                border: `1px solid ${t.errorBorder}`,
                                background: "transparent",
                                opacity: inputLocked ? 0.5 : 1,
                            }}
                            title={localizeText(lang, "Clear selected file", "清除已选文件")}
                        >
                            ×
                        </button>
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
                    <textarea
                        ref={inputRef}
                        data-testid="ai-input"
                        disabled={!ready || inputLocked}
                        readOnly={inputLocked}
                        aria-readonly={inputLocked}
                        style={{
                            flex: 1, minWidth: 0, background: "transparent",
                            border: "none", outline: "none", color: t.inputText,
                            fontFamily: "Consolas, 'Courier New', monospace",
                            fontSize: "14px", padding: "8px 0",
                            resize: "none", overflow: "auto",
                            minHeight: "36px", maxHeight: "120px",
                            lineHeight: 1.4,
                            opacity: (!ready || inputLocked) ? 0.5 : 1,
                            cursor: inputLocked ? "default" : "text",
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
                    <button
                        type="button"
                        onClick={browseFile}
                        disabled={!ready || inputLocked}
                        style={{
                            ...baseInputBtnStyle,
                            color: t.pathColor,
                            borderColor: t.pathColor,
                            opacity: (!ready || inputLocked) ? 0.5 : 1,
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
        {workflowState.splitMode && (
            <div style={{ flex: 1, minWidth: 0, height: "100%" }}>
                <WorkflowDocPreview
                    phaseDocuments={workflowState.phaseDocuments}
                    currentPhaseID={workflowState.currentPhaseID}
                    gateResults={workflowState.gateResults}
                    onClose={closeDocPreview}
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
                    onResizeStart={() => {
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
                    }}
                />
            </div>
        )}
        </div>
    );
}
