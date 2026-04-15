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
    // Coding workflow
    requirements: "需求",
    tech_design: "设计",
    task_breakdown: "任务",
    implementation: "实现",
    review: "审查",
    // Product design workflow
    problem_discovery: "问题发现",
    solution_design: "方案设计",
    prd: "PRD",
    prototype: "原型设计",
    // Innovation workflow
    opportunity: "机会识别",
    ideation: "创意发散",
    validation: "可行性验证",
    roadmap: "路线图",
    action_plan: "行动计划",
    // Business plan workflow
    bp_requirement: "需求定位",
    bp_content: "内容撰写",
    bp_structure: "结构优化",
    bp_visual_design: "PPT设计",
    bp_doc_generation: "文档生成",
    // Testing workflow
    test_strategy: "测试策略",
    test_design: "用例设计",
    test_environment: "环境规划",
    test_execution: "测试执行",
    defect_report: "缺陷报告",
    // Literature review workflow
    topic_definition: "选题定义",
    literature_search: "文献检索",
    screening_classification: "筛选分类",
    content_extraction: "内容提取",
    review_writing: "综述撰写",
    // Research report workflow
    requirement_scoping: "需求定义",
    source_mapping: "信息源梳理",
    report_collection: "研报收集",
    insight_extraction: "观点提炼",
    synthesis_report: "整合报告",
    // Experiment design workflow
    hypothesis_formulation: "假设提出",
    experiment_design: "实验设计",
    variable_control: "变量控制",
    data_collection: "数据采集",
    analysis_plan: "分析计划",
    // Grant proposal workflow
    topic_justification: "选题论证",
    research_status: "研究现状",
    research_plan: "研究方案",
    expected_outcomes: "预期成果",
    budget_plan: "经费预算",
    // Paper writing workflow
    outline_design: "大纲构思",
    methodology: "方法论",
    results_presentation: "结果呈现",
    discussion_analysis: "讨论分析",
    submission_prep: "投稿准备",
    // Project proposal workflow
    background_analysis: "背景分析",
    goal_definition: "目标定义",
    // solution_design already mapped above
    resource_assessment: "资源评估",
    risk_contingency: "风险预案",
    // Event planning workflow
    requirement_confirm: "需求确认",
    scheme_planning: "方案策划",
    process_design: "流程设计",
    material_checklist: "物料清单",
    execution_manual: "执行手册",
    // Competitive analysis workflow
    target_definition: "分析目标",
    competitor_identification: "竞品识别",
    dimension_comparison: "多维对比",
    gap_analysis: "差异分析",
    strategy_recommendation: "策略建议",
    // Presentation design workflow
    audience_goal: "受众目标",
    content_outline: "内容大纲",
    style_specification: "风格规范",
    slide_scripting: "逐页脚本",
    ppt_generation: "PPT生成",
    // Bid response workflow
    tender_analysis: "招标解析",
    qualification_response: "资质响应",
    technical_proposal: "技术方案",
    commercial_proposal: "商务报价",
    bid_document_assembly: "文件组装",
    // Contract review workflow
    contract_parsing: "合同解析",
    clause_risk_analysis: "条款风险",
    compliance_check: "合规审查",
    modification_suggestions: "修改建议",
    review_summary: "审查意见",
    // Due diligence workflow
    target_profiling: "公司画像",
    business_dd: "商业尽调",
    financial_dd: "财务尽调",
    legal_dd: "法律尽调",
    dd_conclusion: "尽调结论",
    // Compliance audit workflow
    audit_scope: "审计范围",
    compliance_assessment: "合规评估",
    risk_rating: "风险评级",
    remediation_plan: "整改计划",
    audit_report: "审计报告",
    // Patent analysis workflow
    tech_disclosure: "技术解析",
    prior_art_search: "现有技术",
    infringement_assessment: "侵权评估",
    // strategy_recommendation already defined in competitive analysis (same label)
    patent_report: "分析报告",
    // Legacy aliases
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
                    width: "6px",
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
                {/* Header: phase tabs + close button — draggable for window move */}
                <div style={{
                    display: "flex",
                    alignItems: "center",
                    padding: "8px 14px",
                    borderBottom: `1px solid ${theme.border}`,
                    background: theme.headerBg,
                    gap: "4px",
                    flexWrap: "wrap",
                    flexShrink: 0,
                    '--wails-draggable': 'drag',
                } as any}>
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
                                '--wails-draggable': 'no-drag',
                            } as any}
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
                            '--wails-draggable': 'no-drag',
                        } as any}
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
                    padding: "16px 20px 16px 24px",
                    fontSize: "14px",
                    lineHeight: "1.6",
                    fontFamily: "inherit",
                    minHeight: 0,
                    boxSizing: "border-box",
                    wordBreak: "break-word",
                    textAlign: "left",
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
