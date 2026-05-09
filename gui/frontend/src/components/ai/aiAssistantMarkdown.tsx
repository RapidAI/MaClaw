import React from "react";
import { OpenFileOrShowInFolder, ShowItemInFolder } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import type { ChatAction, ChatConfirmation, ChatMessage, ChatUnfinishedSlot } from "./useAIAssistant";
import { renderCodingAgentProgressStatus } from "./CodingAgentProgressStatus";

export interface Theme {
    text: string;
    textMuted: string;
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
    boldColor: string;
    italicColor: string;
    bulletColor: string;
    quoteBorder: string;
    quoteText: string;
    btnColor: string;
    btnBorder: string;
    inputBarBorder: string;
}

const baseInputBtnStyle: React.CSSProperties = {
    background: "transparent",
    border: "1px solid",
    borderRadius: "8px",
    padding: 0,
    fontSize: "14px",
    fontFamily: "'Segoe UI Symbol', 'Segoe UI', sans-serif",
    cursor: "pointer",
    lineHeight: 1,
    minHeight: "34px",
    minWidth: "36px",
    width: "36px",
    height: "34px",
    flexShrink: 0,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    transition: "transform 120ms ease, box-shadow 120ms ease, background 120ms ease, border-color 120ms ease, opacity 120ms ease",
};

/* Themed inline markdown rendering */

function looksLikeFilePath(s: string): boolean {
    if (/^[A-Za-z]:\\/.test(s)) return true;
    if (/^(~|\/(?:Users|home|tmp|var|opt|etc|usr))[/\\]/.test(s)) return true;
    return false;
}

function renderPathLink(filePath: string, key: number, t: Theme, trimTrailing = false): React.ReactNode {
    const display = trimTrailing
        ? filePath.replace(/[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/, "")
        : filePath;
    return (
        <a key={key}
           href="#"
           onClick={(event) => openFileInFolder(event, display)}
           style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }}
           title={display}
        >{"\uD83D\uDCC2 "}{display}</a>
    );
}


const codeBlockPathPattern = /([A-Za-z]:\\[^\n\r*?"<>|]+\.\w+)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))\/[^\n\r*?"<>|]+\.\w+)/g;

function renderCodePathLink(filePath: string, key: string, t: Theme): React.ReactNode {
    const display = filePath.replace(/[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/, "");
    return <a key={key} href="#" onClick={(event) => openFileInFolder(event, display)} style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }} title={display}>{display}</a>;
}

function renderCodeBlockText(text: string, t: Theme): React.ReactNode[] {
    const parts: React.ReactNode[] = [];
    let lastIndex = 0;
    let idx = 0;
    codeBlockPathPattern.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = codeBlockPathPattern.exec(text)) !== null) {
        if (match.index > lastIndex) parts.push(text.slice(lastIndex, match.index));
        const raw = match[0];
        const display = raw.replace(/[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/, "");
        if (display.length !== raw.length) codeBlockPathPattern.lastIndex -= raw.length - display.length;
        parts.push(renderCodePathLink(display, "code-path-" + idx++, t));
        lastIndex = codeBlockPathPattern.lastIndex;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return parts;
}

function renderInlineMarkdownRestored(text: string, t: Theme): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^\s*][^*]*?\*)|(\[[^\]]+\]\([^)]+\))|([A-Za-z]:\\[^\n\r*?"<>|:]+\.\w+)|([A-Za-z]:\\[\w\\.\-]+\\?)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))\/[^\n\r*?"<>|:]+\.\w+)|((~|\/(?:Users|home|tmp|var|opt|etc|usr))[\w/.\-]+)/g;
    let lastIndex = 0;
    let idx = 0;
    while (true) {
        const match = re.exec(text);
        if (!match) break;
        if (match.index > lastIndex) parts.push(text.slice(lastIndex, match.index));
        const m = match[0];
        if (match[1]) {
            const inner = m.slice(1, -1);
            parts.push(looksLikeFilePath(inner) ? renderPathLink(inner, idx++, t) : <code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{inner}</code>);
        } else if (match[2]) {
            const inner = m.slice(2, -2);
            parts.push(looksLikeFilePath(inner) ? renderPathLink(inner, idx++, t) : <strong key={idx++} style={{ color: t.boldColor, fontWeight: 700 }}>{inner}</strong>);
        } else if (match[3]) {
            parts.push(<em key={idx++} style={{ color: t.italicColor }}>{m.slice(1, -1)}</em>);
        } else if (match[4]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(e) => { e.preventDefault(); BrowserOpenURL(href); }} style={{ color: t.linkColor, textDecoration: "underline", cursor: "pointer" }}>{lm[1]}</a>);
                } else if (looksLikeFilePath(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(event) => openFileInFolder(event, href)} style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }} title={href}>{"\uD83D\uDCC2 "}{lm[1]}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: t.linkColor }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        } else if (match[5] || match[6] || match[7] || match[9]) {
            const filePath = m.replace(/[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/, "");
            if (filePath.length !== m.length) re.lastIndex -= (m.length - filePath.length);
            parts.push(renderPathLink(filePath, idx++, t));
        }
        lastIndex = re.lastIndex;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return parts.length > 0 ? parts : ["\u00A0"];
}

export function renderInlineMarkdown(text: string, t: Theme): React.ReactNode[] {
    return renderInlineMarkdownRestored(text, t);
}

function renderInlineMarkdownLegacyUnused(text: string, t: Theme): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    // Path matching: two strategies per platform
    // 1. Broad match for paths with CJK/spaces - requires .ext ending as boundary anchor
    // 2. Original ASCII-only match - works without .ext
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
            const inner = m.slice(1, -1);
            if (looksLikeFilePath(inner)) {
                parts.push(renderPathLink(inner, idx++, t));
            } else {
                parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em" }}>{inner}</code>);
            }
        } else if (match[2]) {
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
                    parts.push(<a key={idx++} href="#" onClick={(event) => openFileInFolder(event, href)} style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer" }} title={href}>{"\uD83D\uDCC2 "}{lm[1]}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: t.linkColor }}>{lm[1]}</span>);
                }
            } else {
                parts.push(m);
            }
        } else if (match[5] || match[6] || match[7] || match[9]) {
            // Trim trailing punctuation/whitespace that isn't part of the path
            const filePath = m.replace(/[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/, "");
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
                >{"\u{1F4C4}"} {filePath}</a>
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

    if (/^[-*_]{3,}\s*$/.test(trimmed)) {
        return <hr key={key} style={{ border: "none", borderTop: `1px solid ${t.divider}`, margin: "8px 0" }} />;
    }

    if (/^[-*]\s/.test(trimmed)) {
        return (
            <div key={key} style={{ paddingLeft: "1em", textIndent: "-0.7em", minHeight: "1.4em" }}>
                <span style={{ color: t.bulletColor }}>{"\u2022"}</span>{" "}
                {renderInlineMarkdown(trimmed.slice(2), t)}
            </div>
        );
    }

    const numMatch = trimmed.match(/^(\d+)[.)]\s+(.+)$/);
    if (numMatch) {
        return (
            <div key={key} style={{ paddingLeft: "1.2em", textIndent: "-1.2em", minHeight: "1.4em" }}>
                <span style={{ color: t.bulletColor }}>{numMatch[1]}.</span>{" "}
                {renderInlineMarkdown(numMatch[2], t)}
            </div>
        );
    }

    return (
        <div key={key} style={{ minHeight: "1.4em" }}>
            {renderInlineMarkdown(text, t) || "\u00A0"}
        </div>
    );
}

/* Structured response rendering */

function isTableRow(line: string): boolean {
    const trimmed = line.trim();
    return trimmed.startsWith("|") && trimmed.length > 1;
}

function isSeparatorRow(line: string): boolean {
    const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
    return /^[\s|:\-]+$/.test(trimmed) && trimmed.includes("-");
}

function parseTableCells(line: string): string[] {
    let trimmed = line.trim();
    if (trimmed.startsWith("|")) trimmed = trimmed.slice(1);
    if (trimmed.endsWith("|")) trimmed = trimmed.slice(0, -1);
    return trimmed.split("|").map(c => c.trim());
}

function renderTable(tableLines: string[], key: string, t: Theme): React.ReactNode {
    const dataRows = tableLines.filter(line => !isSeparatorRow(line));
    if (tableLines.length < 2 || dataRows.length === 0) return null;
    const headerCells = parseTableCells(dataRows[0]);
    const bodyRows = dataRows.slice(1);
    const cellStyle: React.CSSProperties = { border: `1px solid ${t.divider}`, padding: "4px 8px", textAlign: "left", fontSize: "0.9em", lineHeight: 1.5 };
    return (
        <div key={key} style={{ overflowX: "auto", margin: "4px 0" }}>
            <table style={{ borderCollapse: "collapse", width: "100%", color: t.text, whiteSpace: "normal", wordBreak: "normal" }}>
                <thead><tr>{headerCells.map((cell, ci) => <th key={ci} style={{ ...cellStyle, fontWeight: 600, background: t.fieldBg }}>{renderInlineMarkdown(cell, t)}</th>)}</tr></thead>
                {bodyRows.length > 0 && <tbody>{bodyRows.map((row, ri) => { const cells = parseTableCells(row); return <tr key={ri}>{headerCells.map((_, ci) => <td key={ci} style={cellStyle}>{renderInlineMarkdown(cells[ci] || "", t)}</td>)}</tr>; })}</tbody>}
            </table>
        </div>
    );
}

export function renderContentWithCodeBlocks(content: string, t: Theme): React.ReactNode[] {
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
                    <code>{renderCodeBlockText(codeBlockLines.join("\n"), t)}</code>
                </pre>
            );
        }
        codeBlockLines = [];
        codeBlockLang = "";
    };

    const flushTable = () => {
        if (tableLines.length === 0) return;
        const rendered = renderTable(tableLines, `tbl-${elements.length}`, t);
        if (rendered) {
            elements.push(rendered);
        } else {
            for (const tableLine of tableLines) {
                elements.push(renderMarkdownLine(tableLine, `md-fallback-${elements.length}`, t));
            }
        }
        tableLines = [];
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
                            ? "#b91c1c"
                            : recoveryTone.includes('partial')
                                ? "#b45309"
                                : "#166534",
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
                        width: "auto",
                        height: "auto",
                        maxWidth: "100%",
                        minWidth: "36px",
                        minHeight: "28px",
                        lineHeight: 1.35,
                        overflowWrap: "anywhere",
                        textAlign: "center",
                        whiteSpace: "normal",
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
                        <span style={{ color: t.bulletColor }}>{"\u2022"}</span>{" "}
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
                {taskType ? `\u6267\u884c\u524d\u786e\u8ba4 - ${taskType}` : "\u6267\u884c\u524d\u786e\u8ba4"}
            </div>
            {status && (
                <div data-testid="confirmation-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
                    {"\u72b6\u6001"}: {status}
                </div>
            )}
            <div data-testid="confirmation-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                {renderContentWithCodeBlocks(confirmation.summary, t)}
            </div>
            {renderConfirmationList("confirmation-target-paths", "\u76ee\u6807\u8def\u5f84", targetPaths, t)}
            {renderConfirmationList("confirmation-planned-actions", "\u8ba1\u5212\u64cd\u4f5c", plannedActions, t)}
            {renderConfirmationList("confirmation-risk-flags", "\u98ce\u9669\u6807\u8bb0", riskFlags, t)}
            {renderConfirmationList("confirmation-revision-hints", "\u4fee\u8ba2\u63d0\u793a", revisionHints, t)}
            {actions && actions.length > 0 && renderActions(actions, executeAction, t)}
        </div>
    );
}

function formatUnfinishedSlotStatus(status: string, lang: string) {
    const normalized = status.replace(/_/g, " ");
    if (!lang.startsWith("zh")) return `Status: ${normalized}`;
    const labels: Record<string, string> = {
        pending_resume: "\u5f85\u7ee7\u7eed",
        resumed: "\u5df2\u6062\u590d",
        dismissed: "\u5df2\u5ffd\u7565",
    };
    return `\u72b6\u6001\uff1a${labels[status] || normalized}`;
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
                {"Unfinished item"}
            </div>
            {slot.status && (
                <div data-testid="unfinished-slot-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
                    {formatUnfinishedSlotStatus(slot.status, lang)}
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
                <div data-testid="unfinished-slot-project" style={{ color: t.pathColor, marginTop: "6px", wordBreak: "break-all" }}>
                    <a
                        href="#"
                        onClick={(event) => openFileInFolder(event, slot.projectPath!)}
                        style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer", wordBreak: "break-all" }}
                        title={slot.projectPath}
                    >
                        {"\u{1F4C1}"} {slot.projectPath}
                    </a>
                </div>
            )}
            {actions.length > 0 && renderActions(actions, executeAction, t)}
        </div>
    );
}

function openFileInFolder(event: React.MouseEvent, filePath: string) {
    event.preventDefault();
    void OpenFileOrShowInFolder(filePath).catch(() => ShowItemInFolder(filePath));
}

/* Render a single ChatMessage */

export function renderMessage(msg: ChatMessage, executeAction: (cmd: string) => void, t: Theme, isLastAssistant: boolean, savedFileLabel: string, lang = "en"): React.ReactNode {
    switch (msg.role) {
        case "user":
            return (
                <div key={msg.id}>
                    <div style={{ borderTop: `1px solid ${t.divider}`, margin: "8px 0 4px 0" }} />
                    <div style={{ color: t.userColor, fontWeight: 600, padding: "3px 0 3px 1.2em", overflowWrap: "break-word", whiteSpace: "pre-wrap", textIndent: "-1.2em" }}>
                        {">"} {msg.content}
                    </div>
                </div>
            );
        case "assistant": {
            const savedPaths = msg.localFilePaths && msg.localFilePaths.length > 0
                ? msg.localFilePaths
                : (msg.localFilePath ? [msg.localFilePath] : []);
            return (
                <div key={msg.id} style={{
                    padding: "4px 0 4px 8px",
                    borderLeft: `2px solid ${t.responseBorderLeft}`,
                    margin: "2px 0",
                    color: t.text,
                }}>
                    {/* Streaming: show blinking cursor only on the last assistant message */}
                    {isLastAssistant && !msg.content && !msg.fields && !msg.thumbnailBase64 && savedPaths.length === 0 && (
                        <span style={{ opacity: 0.5, animation: "blink 1s step-end infinite" }}>{"|"}</span>
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
                    {msg.confirmation && renderConfirmationCard(msg.confirmation, msg.actions, executeAction, t)}
                    {msg.unfinishedSlot && renderUnfinishedSlotCard(msg.unfinishedSlot, executeAction, t, lang)}
                    {savedPaths.length > 0 && (
                        <div style={{ margin: "4px 0" }}>
                            {savedPaths.map((fp, i) => (
                                <div key={i} style={{ padding: "2px 0" }}>
                                    <a href="#"
                                       onClick={(event) => openFileInFolder(event, fp)}
                                       style={{ color: t.pathColor, textDecoration: "underline", cursor: "pointer", wordBreak: "break-all" }}
                                       title={fp}>
                                        {"\u{1F4C4}"} {savedFileLabel}: {"\u{1F4C1}"} {fp}
                                    </a>
                                </div>
                            ))}
                        </div>
                    )}
                    {msg.fields && msg.fields.length > 0 && renderFields(msg.fields, t)}
                    {!msg.confirmation && msg.actions && msg.actions.length > 0 && renderActions(msg.actions, executeAction, t)}
                </div>
            );
        }
        case "progress":
            {
                const codingAgentProgress = renderCodingAgentProgressStatus(msg, t, lang);
                if (codingAgentProgress) return codingAgentProgress;
            }
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
                    borderRadius: "6px",
                    background: "linear-gradient(135deg, rgba(99,102,241,0.06), rgba(139,92,246,0.06))",
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
                        {">"} {msg.content}
                </div>
            );
        default:
            return null;
    }
}
