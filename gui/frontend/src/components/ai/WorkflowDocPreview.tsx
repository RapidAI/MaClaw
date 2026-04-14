import React, { useCallback, useEffect, useRef, useState } from "react";
import type { QualityGateResult } from "./useWorkflowState";

// ── Mermaid (local npm package, no network required) ──

let mermaidMod: any = null;
let mermaidInitPromise: Promise<any> | null = null;

function getMermaid(): Promise<any> {
    if (mermaidMod) return Promise.resolve(mermaidMod);
    if (mermaidInitPromise) return mermaidInitPromise;
    mermaidInitPromise = import("mermaid").then((m) => {
        const mermaid = m.default || m;
        mermaid.initialize({ startOnLoad: false, theme: "dark", securityLevel: "loose" });
        mermaidMod = mermaid;
        return mermaid;
    });
    return mermaidInitPromise;
}

/** Renders a mermaid diagram from source code. */
function MermaidBlock({ code, theme }: { code: string; theme: DocPreviewTheme }) {
    const [svg, setSvg] = useState<string>("");
    const [error, setError] = useState<string>("");
    const idRef = useRef(`mermaid-${Math.random().toString(36).slice(2, 10)}`);

    useEffect(() => {
        let cancelled = false;
        getMermaid().then(async (m) => {
            if (cancelled) return;
            try {
                const { svg: rendered } = await m.render(idRef.current, code.trim());
                if (!cancelled) setSvg(rendered);
            } catch (e: any) {
                if (!cancelled) setError(e?.message || "Mermaid render error");
            }
        }).catch((e) => {
            if (!cancelled) setError(e?.message || "Failed to load mermaid");
        });
        return () => { cancelled = true; };
    }, [code]);

    if (error) {
        return (
            <pre style={{
                background: theme.codeBlockBg,
                border: `1px solid ${theme.codeBlockBorder}`,
                borderRadius: "6px",
                padding: "12px",
                margin: "8px 0",
                fontSize: "12px",
                color: theme.textMuted,
            }}>
                <div style={{ marginBottom: "4px", color: theme.textMuted }}>⚠️ Mermaid render failed: {error}</div>
                <code style={{ color: theme.codeText }}>{code}</code>
            </pre>
        );
    }

    if (svg) {
        return (
            <div
                style={{ margin: "8px 0", overflow: "auto" }}
                dangerouslySetInnerHTML={{ __html: svg }}
            />
        );
    }

    return (
        <div style={{ margin: "8px 0", padding: "12px", color: theme.textMuted, fontSize: "12px" }}>
            ⏳ Rendering diagram...
        </div>
    );
}

/** Theme colors passed from the parent AIAssistantPanel. */
export interface DocPreviewTheme {
    bg: string;
    text: string;
    textMuted: string;
    border: string;
    headerBg: string;
    accentColor: string;
    accentBg: string;
    codeBg: string;
    codeText: string;
    codeBlockBg: string;
    codeBlockBorder: string;
    headingColor: string;
    linkColor: string;
    quoteBorder: string;
    quoteText: string;
    quoteBg: string;
}

interface WorkflowDocPreviewProps {
    phaseDocuments: Map<string, string>;
    currentPhaseID: string;
    gateResults: Map<string, QualityGateResult>;
    onClose: () => void;
    theme: DocPreviewTheme;
    onResizeStart?: () => void;
}

const phaseLabels: Record<string, string> = {
    requirements: "需求",
    design: "设计",
    tasks: "任务",
};

// ── Lightweight Markdown renderer (no external deps) ──

function renderMarkdown(md: string, theme: DocPreviewTheme): React.ReactNode[] {
    const lines = md.split("\n");
    const nodes: React.ReactNode[] = [];
    let i = 0;
    let listItems: string[] = [];
    let inCodeBlock = false;
    let codeLines: string[] = [];
    let codeLang = "";

    const flushList = () => {
        if (listItems.length === 0) return;
        nodes.push(
            <ul key={`ul-${nodes.length}`} style={{ margin: "6px 0", paddingLeft: "20px" }}>
                {listItems.map((item, idx) => (
                    <li key={idx} style={{ marginBottom: "3px" }}>{renderInline(item, theme)}</li>
                ))}
            </ul>
        );
        listItems = [];
    };

    const flushCode = () => {
        // Mermaid diagram: render as interactive SVG instead of code block
        if (codeLang.toLowerCase() === "mermaid") {
            const mermaidCode = codeLines.join("\n");
            nodes.push(
                <MermaidBlock key={`mermaid-${nodes.length}`} code={mermaidCode} theme={theme} />
            );
            codeLines = [];
            codeLang = "";
            return;
        }
        nodes.push(
            <pre key={`code-${nodes.length}`} style={{
                background: theme.codeBlockBg,
                border: `1px solid ${theme.codeBlockBorder}`,
                borderRadius: "6px",
                padding: "12px",
                margin: "8px 0",
                overflow: "auto",
                fontSize: "13px",
                lineHeight: "1.5",
            }}>
                {codeLang && <div style={{ fontSize: "11px", color: theme.textMuted, marginBottom: "4px" }}>{codeLang}</div>}
                <code style={{ color: theme.codeText, fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace" }}>
                    {codeLines.join("\n")}
                </code>
            </pre>
        );
        codeLines = [];
        codeLang = "";
    };

    while (i < lines.length) {
        const line = lines[i];

        // Code block toggle
        if (line.trimStart().startsWith("```")) {
            if (inCodeBlock) {
                inCodeBlock = false;
                flushCode();
            } else {
                flushList();
                inCodeBlock = true;
                codeLang = line.trimStart().slice(3).trim();
            }
            i++;
            continue;
        }
        if (inCodeBlock) {
            codeLines.push(line);
            i++;
            continue;
        }

        // Headings
        const headingMatch = line.match(/^(#{1,6})\s+(.+)/);
        if (headingMatch) {
            flushList();
            const level = headingMatch[1].length;
            const sizes = ["22px", "18px", "16px", "15px", "14px", "13px"];
            nodes.push(
                <div key={`h-${i}`} style={{
                    fontSize: sizes[level - 1] || "14px",
                    fontWeight: 700,
                    color: theme.headingColor,
                    margin: level <= 2 ? "18px 0 8px" : "12px 0 6px",
                    lineHeight: 1.3,
                    borderBottom: level <= 2 ? `1px solid ${theme.border}` : undefined,
                    paddingBottom: level <= 2 ? "6px" : undefined,
                }}>
                    {renderInline(headingMatch[2], theme)}
                </div>
            );
            i++;
            continue;
        }

        // Blockquote
        if (line.startsWith("> ") || line === ">") {
            flushList();
            const quoteLines: string[] = [];
            while (i < lines.length && (lines[i].startsWith("> ") || lines[i] === ">")) {
                quoteLines.push(lines[i].replace(/^>\s?/, ""));
                i++;
            }
            nodes.push(
                <blockquote key={`bq-${nodes.length}`} style={{
                    borderLeft: `3px solid ${theme.quoteBorder}`,
                    margin: "8px 0",
                    padding: "6px 12px",
                    color: theme.quoteText,
                    background: theme.quoteBg,
                    borderRadius: "0 4px 4px 0",
                    fontSize: "13px",
                }}>
                    {quoteLines.map((ql, idx) => <div key={idx}>{renderInline(ql, theme)}</div>)}
                </blockquote>
            );
            continue;
        }

        // Unordered list
        if (/^\s*[-*+]\s+/.test(line)) {
            listItems.push(line.replace(/^\s*[-*+]\s+/, ""));
            i++;
            continue;
        }

        // Ordered list
        if (/^\s*\d+[.)]\s+/.test(line)) {
            // Flush unordered list first
            flushList();
            const olItems: string[] = [];
            while (i < lines.length && /^\s*\d+[.)]\s+/.test(lines[i])) {
                olItems.push(lines[i].replace(/^\s*\d+[.)]\s+/, ""));
                i++;
            }
            nodes.push(
                <ol key={`ol-${nodes.length}`} style={{ margin: "6px 0", paddingLeft: "20px" }}>
                    {olItems.map((item, idx) => (
                        <li key={idx} style={{ marginBottom: "3px" }}>{renderInline(item, theme)}</li>
                    ))}
                </ol>
            );
            continue;
        }

        // Horizontal rule
        if (/^---+$/.test(line.trim()) || /^\*\*\*+$/.test(line.trim())) {
            flushList();
            nodes.push(<hr key={`hr-${i}`} style={{ border: "none", borderTop: `1px solid ${theme.border}`, margin: "12px 0" }} />);
            i++;
            continue;
        }

        // Empty line
        if (line.trim() === "") {
            flushList();
            i++;
            continue;
        }

        // Paragraph
        flushList();
        nodes.push(
            <p key={`p-${i}`} style={{ margin: "6px 0", lineHeight: "1.7" }}>
                {renderInline(line, theme)}
            </p>
        );
        i++;
    }
    flushList();
    if (inCodeBlock) flushCode();
    return nodes;
}

/** Render inline markdown: bold, italic, code, links */
function renderInline(text: string, theme: DocPreviewTheme): React.ReactNode {
    const parts: React.ReactNode[] = [];
    // Regex: **bold**, *italic*, `code`, [text](url)
    const re = /(\*\*(.+?)\*\*)|(\*(.+?)\*)|(`([^`]+?)`)|(\[([^\]]+)\]\(([^)]+)\))/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let key = 0;
    while ((match = re.exec(text)) !== null) {
        if (match.index > lastIndex) {
            parts.push(text.slice(lastIndex, match.index));
        }
        if (match[1]) { // bold
            parts.push(<strong key={key++} style={{ fontWeight: 600 }}>{match[2]}</strong>);
        } else if (match[3]) { // italic
            parts.push(<em key={key++} style={{ fontStyle: "italic", color: theme.textMuted }}>{match[4]}</em>);
        } else if (match[5]) { // inline code
            parts.push(<code key={key++} style={{
                background: theme.codeBg,
                color: theme.codeText,
                padding: "1px 5px",
                borderRadius: "3px",
                fontSize: "0.9em",
                fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace",
            }}>{match[6]}</code>);
        } else if (match[7]) { // link
            parts.push(<a key={key++} href={match[9]} style={{ color: theme.linkColor, textDecoration: "underline" }} target="_blank" rel="noopener noreferrer">{match[8]}</a>);
        }
        lastIndex = match.index + match[0].length;
    }
    if (lastIndex < text.length) {
        parts.push(text.slice(lastIndex));
    }
    return parts.length === 1 ? parts[0] : <>{parts}</>;
}

/**
 * WorkflowDocPreview renders the right-side document preview panel
 * during workflow execution. Supports Markdown rendering, dark mode,
 * vertical scrollbar, and proper padding.
 */
export function WorkflowDocPreview({
    phaseDocuments,
    currentPhaseID,
    gateResults,
    onClose,
    theme,
    onResizeStart,
}: WorkflowDocPreviewProps) {
    const [viewingPhaseID, setViewingPhaseID] = useState(currentPhaseID);

    useEffect(() => {
        if (currentPhaseID) setViewingPhaseID(currentPhaseID);
    }, [currentPhaseID]);

    const activePhaseID = viewingPhaseID || currentPhaseID;
    const content = phaseDocuments.get(activePhaseID) || "";
    const gateResult = gateResults.get(activePhaseID);
    const phaseIDs = Array.from(phaseDocuments.keys());

    return (
        <div style={{
            display: "flex",
            flexDirection: "row",
            height: "100%",
            minWidth: 0,
        }}>
            {/* ── Drag handle for resizing ── */}
            <div
                onMouseDown={(e) => {
                    e.preventDefault();
                    onResizeStart?.();
                }}
                style={{
                    width: "5px",
                    cursor: "col-resize",
                    background: theme.border,
                    flexShrink: 0,
                    transition: "background 0.15s",
                }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = theme.accentColor; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = theme.border; }}
            />
            {/* ── Main preview content ── */}
            <div style={{
                display: "flex",
                flexDirection: "column",
                flex: 1,
                minWidth: 0,
                height: "100%",
                background: theme.bg,
                color: theme.text,
            }}>
                {/* Header: phase tabs + close button */}
                <div style={{
                    display: "flex",
                    alignItems: "center",
                    padding: "8px 14px",
                    borderBottom: `1px solid ${theme.border}`,
                    background: theme.headerBg,
                    gap: "4px",
                    flexWrap: "wrap",
                    flexShrink: 0,
                }}>
                    {phaseIDs.map(pid => (
                        <button
                            key={pid}
                            onClick={() => setViewingPhaseID(pid)}
                            style={{
                                padding: "4px 10px",
                                fontSize: "12px",
                                fontWeight: pid === activePhaseID ? 600 : 400,
                                border: pid === activePhaseID ? `1px solid ${theme.accentColor}` : `1px solid transparent`,
                                borderRadius: "4px",
                                background: pid === activePhaseID ? theme.accentBg : "transparent",
                                cursor: "pointer",
                                color: pid === activePhaseID ? theme.accentColor : theme.textMuted,
                            }}
                        >
                            {phaseLabels[pid] || pid}
                        </button>
                    ))}
                    <div style={{ flex: 1 }} />
                    <button
                        onClick={onClose}
                        style={{
                            background: "none",
                            border: "none",
                            cursor: "pointer",
                            fontSize: "16px",
                            padding: "2px 6px",
                            borderRadius: "4px",
                            color: theme.textMuted,
                            lineHeight: 1,
                        }}
                        title="关闭文档预览"
                    >
                        ×
                    </button>
                </div>

                {/* Quality gate banner */}
                {gateResult && (
                    <div style={{
                        padding: "6px 14px",
                        fontSize: "12px",
                        borderBottom: `1px solid ${theme.border}`,
                        background: gateResult.passed ? "rgba(16,185,129,0.1)" : "rgba(245,158,11,0.1)",
                        color: theme.text,
                        flexShrink: 0,
                    }}>
                        {gateResult.passed ? "✅" : "⚠️"} 质量门禁：
                        {gateResult.items.map((item, i) => (
                            <span key={i} style={{ marginLeft: "8px" }}>
                                {item.passed ? "✅" : "⚠️"} {item.description}
                            </span>
                        ))}
                    </div>
                )}

                {/* Document content — Markdown rendered */}
                <div style={{
                    flex: 1,
                    overflowY: "auto",
                    overflowX: "hidden",
                    padding: "16px 20px",
                    fontSize: "14px",
                    lineHeight: "1.6",
                    fontFamily: "inherit",
                    minHeight: 0,
                    boxSizing: "border-box",
                    wordBreak: "break-word",
                }}>
                    {content
                        ? renderMarkdown(content, theme)
                        : <span style={{ color: theme.textMuted }}>暂无文档内容</span>
                    }
                </div>
            </div>
        </div>
    );
}
