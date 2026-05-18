import React, { useEffect, useMemo, useRef, useState } from "react";
import type { PhaseInfo, QualityGateResult } from "./useWorkflowState";
import { localizeText } from "./aiAssistantI18n";

// ── Mermaid (local npm package, no network required) ──

let mermaidMod: any = null;
let mermaidInitPromise: Promise<any> | null = null;

function getMermaid(): Promise<any> {
    if (mermaidMod) return Promise.resolve(mermaidMod);
    if (mermaidInitPromise) return mermaidInitPromise;
    mermaidInitPromise = import("mermaid").then((m) => {
        const mermaid = m.default || m;
        mermaid.initialize({ startOnLoad: false, theme: "dark", securityLevel: "loose", suppressErrorRendering: true });
        mermaidMod = mermaid;
        return mermaid;
    });
    return mermaidInitPromise;
}

/**
 * Auto-fix common Mermaid syntax issues produced by LLMs.
 * Mermaid keywords are case-sensitive and must be lowercase.
 * LLMs frequently produce "Subgraph", "End", "Graph TD", etc.
 */
function sanitizeMermaidCode(raw: string): string {
    // Diagram type declarations that must be lowercase (line start)
    const diagramTypeRe = /^(\s*)(Graph|Flowchart|SequenceDiagram|StateDiagram|StateDiagram-v2|ClassDiagram|ErDiagram|Gantt|Pie|Gitgraph|Journey|Mindmap|Timeline|Quadrantchart|Sankey-beta|Xychart-beta)\b/i;
    // Keywords that appear at line start and must be lowercase
    const keywordRe = /^(\s*)(Subgraph|ClassDef|Style|Click|Note|Loop|Alt|Else|Opt|Par|Critical|Break|Rect|Activate|Deactivate|Direction)\b/i;
    // "end" on its own line
    const endRe = /^(\s*)End\s*$/;

    return raw
        .split("\n")
        .map((line) => {
            // Diagram type declaration: "Graph TB" → "graph TB"
            const dtMatch = line.match(diagramTypeRe);
            if (dtMatch) {
                const kw = dtMatch[2];
                const lower = kw.toLowerCase();
                if (kw !== lower) {
                    return line.replace(dtMatch[2], lower);
                }
                return line;
            }
            // "End" on its own line → "end"
            if (endRe.test(line)) {
                return line.replace(/End/i, "end");
            }
            // Other keywords at line start
            const kwMatch = line.match(keywordRe);
            if (kwMatch) {
                const kw = kwMatch[2];
                // classDef is camelCase, not all-lowercase
                const fixed = kw.toLowerCase() === "classdef" ? "classDef" : kw.toLowerCase();
                if (kw !== fixed) {
                    return `${kwMatch[1]}${fixed}${line.slice(kwMatch[0].length)}`;
                }
            }
            return line;
        })
        .join("\n");
}

/** Renders a mermaid diagram from source code. */
function MermaidBlock({ code, theme }: { code: string; theme: DocPreviewTheme }) {
    const [svg, setSvg] = useState<string>("");
    const [error, setError] = useState<string>("");
    // Hidden workspace div that Mermaid uses during rendering. By providing this
    // as the third argument to render(), Mermaid creates its temp <div id="d{id}">
    // inside our container instead of document.body.
    //
    // Mechanism: Mermaid's render() creates a temp element, renders the diagram SVG
    // into it, extracts the SVG string, then calls removeTempElements() to clean up.
    // On parse/draw error, it throws *before* removeTempElements(), leaving an
    // orphaned element with the "Syntax error in text" error SVG. Without a
    // container, this orphan lives in document.body — the full-page red banner.
    // With a container, the orphan is scoped to our hidden div.
    //
    // suppressErrorRendering: true (set in initialize) makes Mermaid call
    // removeTempElements() before throwing, so the orphan is cleaned up by Mermaid
    // itself. The container is defense-in-depth for any edge cases.
    const containerRef = useRef<HTMLDivElement>(null);
    const idRef = useRef(`mermaid-${Math.random().toString(36).slice(2, 10)}`);

    useEffect(() => {
        let cancelled = false;
        const sanitized = sanitizeMermaidCode(code);
        getMermaid().then(async (m) => {
            if (cancelled) return;
            try {
                const { svg: rendered } = await m.render(
                    idRef.current,
                    sanitized.trim(),
                    containerRef.current ?? undefined,
                );
                if (!cancelled) setSvg(rendered);
            } catch (e: any) {
                if (!cancelled) {
                    setError(e?.message || "Mermaid render error");
                    if (containerRef.current) {
                        containerRef.current.innerHTML = "";
                    }
                }
            }
        }).catch((e) => {
            if (!cancelled) setError(e?.message || "Failed to load mermaid");
        });
        return () => { cancelled = true; };
    }, [code]);

    let content: React.ReactNode;
    if (error) {
        content = (
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
    } else if (svg) {
        content = (
            <div
                style={{ margin: "8px 0", overflow: "auto" }}
                dangerouslySetInnerHTML={{ __html: svg }}
            />
        );
    } else {
        content = (
            <div style={{ margin: "8px 0", padding: "12px", color: theme.textMuted, fontSize: "12px" }}>
                ⏳ Rendering diagram...
            </div>
        );
    }

    return (
        <>
            <div ref={containerRef} style={{ position: "absolute", width: 0, height: 0, overflow: "hidden" }} />
            {content}
        </>
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
    latestDocumentPhaseID?: string;
    phases?: PhaseInfo[];
    workflowType?: string;
    gateResults: Map<string, QualityGateResult>;
    lang?: string;
    onClose: () => void;
    theme: DocPreviewTheme;
    onResizeStart?: () => void;
    onToggleMaximize?: () => void;
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
    // Changjiang Scholar application workflow
    cj_personal_profile: "个人资质",
    cj_academic_achievements: "学术成就",
    cj_research_plan: "研究计划",
    cj_talent_cultivation: "人才培养",
    cj_recommendation_summary: "推荐整合",
    // Changjiang Scholar review workflow
    cj_completeness_check: "完整性检测",
    cj_achievement_evaluation: "成果评估",
    cj_plan_feasibility: "计划评估",
    cj_narrative_quality: "撰写质量",
    cj_improvement_report: "修改建议",
    // Legacy aliases
    design: "设计",
    tasks: "任务",
};

function workflowPhaseLabel(lang: string | undefined, phaseID: string, phaseLabelMap: Map<string, string>): string {
    const metadataLabel = phaseLabelMap.get(phaseID);
    if (metadataLabel) return metadataLabel;
    switch (phaseID) {
        case "requirements":
            return localizeText(lang || "zh-Hans", "Requirements", "需求", "需求");
        case "tech_design":
        case "design":
            return localizeText(lang || "zh-Hans", "Design", "设计", "設計");
        case "task_breakdown":
        case "tasks":
            return localizeText(lang || "zh-Hans", "Tasks", "任务", "任務");
        default:
            return phaseLabels[phaseID] || phaseID;
    }
}

const workflowPhaseOrders: Record<string, string[]> = {
    coding: ["requirements", "design", "tasks", "implementation", "review"],
    product_design: ["problem_discovery", "solution_design", "prd", "prototype"],
    innovation: ["opportunity", "ideation", "validation", "roadmap", "action_plan"],
    business_plan: ["bp_requirement", "bp_content", "bp_structure", "bp_visual_design", "bp_doc_generation"],
    testing: ["test_strategy", "test_design", "test_environment", "test_execution", "defect_report"],
    literature_review: ["topic_definition", "literature_search", "screening_classification", "content_extraction", "review_writing"],
    research_report: ["requirement_scoping", "source_mapping", "report_collection", "insight_extraction", "synthesis_report"],
    experiment_design: ["hypothesis_formulation", "experiment_design", "variable_control", "data_collection", "analysis_plan"],
    grant_proposal: ["topic_justification", "research_status", "research_plan", "expected_outcomes", "budget_plan"],
    paper_writing: ["outline_design", "methodology", "results_presentation", "discussion_analysis", "submission_prep"],
    project_proposal: ["background_analysis", "goal_definition", "solution_design", "resource_assessment", "risk_contingency"],
    event_planning: ["requirement_confirm", "scheme_planning", "process_design", "material_checklist", "execution_manual"],
    competitive_analysis: ["target_definition", "competitor_identification", "dimension_comparison", "gap_analysis", "strategy_recommendation"],
    presentation_design: ["audience_goal", "content_outline", "style_specification", "slide_scripting", "ppt_generation"],
    bid_response: ["tender_analysis", "qualification_response", "technical_proposal", "commercial_proposal", "bid_document_assembly"],
    contract_review: ["contract_parsing", "clause_risk_analysis", "compliance_check", "modification_suggestions", "review_summary"],
    due_diligence: ["target_profiling", "business_dd", "financial_dd", "legal_dd", "dd_conclusion"],
    compliance_audit: ["audit_scope", "compliance_assessment", "risk_rating", "remediation_plan", "audit_report"],
    patent_analysis: ["tech_disclosure", "prior_art_search", "infringement_assessment", "strategy_recommendation", "patent_report"],
};

const fallbackNonDocumentPhaseIDs = new Set([
    "implementation",
    "test_execution",
    "ppt_generation",
    "bp_doc_generation",
]);

function phaseIDsFromMetadata(phases: PhaseInfo[] | undefined): string[] {
    if (!phases || phases.length === 0) return [];
    const seen = new Set<string>();
    const phaseIDs: string[] = [];
    for (const phase of [...phases].sort((a, b) => a.index - b.index)) {
        if (!phase.id || seen.has(phase.id)) continue;
        seen.add(phase.id);
        phaseIDs.push(phase.id);
    }
    return phaseIDs;
}

export function workflowProgressPhaseIDs(workflowType: string | undefined, phaseDocuments: Map<string, string>, currentPhaseID: string, phases?: PhaseInfo[]): string[] {
    const metadataPhaseIDs = phaseIDsFromMetadata(phases);
    const base = metadataPhaseIDs.length > 0
        ? metadataPhaseIDs
        : workflowType ? [...(workflowPhaseOrders[workflowType] || [])] : [];
    for (const pid of phaseDocuments.keys()) {
        if (!base.includes(pid)) base.push(pid);
    }
    if (currentPhaseID && !base.includes(currentPhaseID)) base.push(currentPhaseID);
    return base;
}

function workflowPhaseExpectsDocument(phaseID: string, phaseDocumentExpectationMap: Map<string, boolean>): boolean {
    if (phaseDocumentExpectationMap.has(phaseID)) return phaseDocumentExpectationMap.get(phaseID) !== false;
    return !fallbackNonDocumentPhaseIDs.has(phaseID);
}

export function workflowProgressPhaseCardState({
    expectsDocument = true,
    gatePassed,
    hasDoc,
    isCurrent,
    isPast,
}: {
    expectsDocument?: boolean;
    gatePassed?: boolean;
    hasDoc: boolean;
    isCurrent: boolean;
    isPast: boolean;
}): { status: string; tone: "attention" | "current" | "done" | "pending"; emphasized: boolean } {
    if (expectsDocument && !hasDoc) {
        if (isCurrent) {
            return { status: "生成中", tone: "current", emphasized: true };
        }
        if (isPast) {
            return { status: "缺文档", tone: "attention", emphasized: true };
        }
        return { status: "待开始", tone: "pending", emphasized: false };
    }
    if (typeof gatePassed === "boolean") {
        return {
            status: gatePassed ? "质检通过" : "需调整",
            tone: gatePassed ? "done" : "attention",
            emphasized: true,
        };
    }
    if (!expectsDocument) {
        if (isCurrent) {
            return { status: hasDoc ? "有产出" : "执行中", tone: "current", emphasized: true };
        }
        if (hasDoc) {
            return { status: "有产出", tone: "done", emphasized: true };
        }
        if (isPast) {
            return { status: "已执行", tone: "done", emphasized: true };
        }
        return { status: "待执行", tone: "pending", emphasized: false };
    }
    if (hasDoc) {
        if (isCurrent) {
            return { status: "待确认", tone: "current", emphasized: true };
        }
        return { status: "已完成", tone: "done", emphasized: true };
    }
    return { status: "待开始", tone: "pending", emphasized: false };
}

function workflowPhaseStatusLabel(lang: string | undefined, status: string): string {
    switch (status) {
        case "生成中":
            return localizeText(lang || "zh-Hans", "Generating", "生成中", "生成中");
        case "缺文档":
            return localizeText(lang || "zh-Hans", "Missing doc", "缺文档", "缺文檔");
        case "待开始":
            return localizeText(lang || "zh-Hans", "Pending", "待开始", "待開始");
        case "质检通过":
            return localizeText(lang || "zh-Hans", "Quality passed", "质检通过", "質檢通過");
        case "需调整":
            return localizeText(lang || "zh-Hans", "Needs changes", "需调整", "需調整");
        case "有产出":
            return localizeText(lang || "zh-Hans", "Has output", "有产出", "有產出");
        case "执行中":
            return localizeText(lang || "zh-Hans", "Running", "执行中", "執行中");
        case "已执行":
            return localizeText(lang || "zh-Hans", "Executed", "已执行", "已執行");
        case "待确认":
            return localizeText(lang || "zh-Hans", "Waiting for confirmation", "待确认", "待確認");
        case "已完成":
            return localizeText(lang || "zh-Hans", "Completed", "已完成", "已完成");
        default:
            return status;
    }
}

function WorkflowProgressBoard({
    activePhaseID,
    currentPhaseID,
    gateResults,
    onSelectPhase,
    phaseDocuments,
    phaseDocumentExpectationMap,
    phaseIDs,
    phaseLabelMap,
    theme,
    lang,
}: {
    activePhaseID: string;
    currentPhaseID: string;
    gateResults: Map<string, QualityGateResult>;
    onSelectPhase: (phaseID: string) => void;
    phaseDocuments: Map<string, string>;
    phaseDocumentExpectationMap: Map<string, boolean>;
    phaseIDs: string[];
    phaseLabelMap: Map<string, string>;
    theme: DocPreviewTheme;
    lang?: string;
}) {
    if (phaseIDs.length === 0) return null;

    const currentIndex = currentPhaseID ? phaseIDs.indexOf(currentPhaseID) : -1;
    const documentPhaseIDs = phaseIDs.filter(pid => workflowPhaseExpectsDocument(pid, phaseDocumentExpectationMap));
    const collectedCount = documentPhaseIDs.filter(pid => phaseDocuments.has(pid)).length;
    const documentSummaryText = documentPhaseIDs.length > 0
        ? localizeText(lang || "zh-Hans", `${collectedCount}/${documentPhaseIDs.length} docs`, `${collectedCount}/${documentPhaseIDs.length} 个文档`, `${collectedCount}/${documentPhaseIDs.length} 個文檔`)
        : localizeText(lang || "zh-Hans", "Execution phase", "执行阶段", "執行階段");
    const latestCollectedIndex = phaseIDs.reduce((latest, pid, index) => phaseDocuments.has(pid) ? index : latest, -1);
    const progressIndex = Math.max(currentIndex, latestCollectedIndex, 0);
    const progressPercent = phaseIDs.length > 1 ? Math.min(100, Math.max(0, (progressIndex / (phaseIDs.length - 1)) * 100)) : 0;
    const cardPaddingX = 10;
    const cardPaddingY = 8;
    const nodeSize = 24;
    const trackInset = cardPaddingX + nodeSize / 2;
    const trackTop = cardPaddingY + nodeSize / 2;
    const trackInsetTotal = trackInset * 2;
    const shouldFitTrack = phaseIDs.length <= 4;
    const shouldCapCards = phaseIDs.length <= 2;
    return (
        <div style={{
            padding: "10px 14px 11px",
            borderBottom: `1px solid ${theme.border}`,
            background: theme.headerBg,
            flexShrink: 0,
        }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "12px", marginBottom: "9px" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "8px", minWidth: 0 }}>
                    <div style={{ fontSize: "12px", fontWeight: 700, color: theme.text }}>
                        {localizeText(lang || "zh-Hans", "Workflow progress", "工作流进度", "工作流進度")}
                    </div>
                    <div style={{ width: "4px", height: "4px", borderRadius: "50%", background: theme.textMuted, opacity: 0.65 }} />
                    <div style={{ fontSize: "11px", color: theme.textMuted, whiteSpace: "nowrap" }}>
                        {localizeText(lang || "zh-Hans", `${phaseIDs.length} phases`, `${phaseIDs.length} 个阶段`, `${phaseIDs.length} 個階段`)}
                    </div>
                </div>
                <div style={{
                    fontSize: "11px",
                    color: theme.textMuted,
                    border: `1px solid ${theme.border}`,
                    borderRadius: "999px",
                    padding: "2px 8px",
                    background: theme.bg,
                    whiteSpace: "nowrap",
                }}>{documentSummaryText}</div>
            </div>
            <div style={{
                overflowX: shouldFitTrack && !shouldCapCards ? "hidden" : "auto",
                padding: shouldFitTrack && !shouldCapCards ? "2px 2px 0" : "2px 2px 6px",
                scrollbarWidth: "thin",
            }}>
                <div style={{
                    display: shouldFitTrack ? "grid" : "inline-grid",
                    gridAutoFlow: shouldFitTrack ? undefined : "column",
                    gridAutoColumns: shouldFitTrack ? undefined : "minmax(132px, 156px)",
                    gridTemplateColumns: shouldFitTrack
                        ? `repeat(${phaseIDs.length}, minmax(112px, ${shouldCapCards ? "220px" : "1fr"}))`
                        : undefined,
                    gap: "10px",
                    alignItems: "stretch",
                    justifyContent: shouldCapCards ? "start" : undefined,
                    position: "relative",
                    minWidth: shouldFitTrack && !shouldCapCards ? "100%" : "max-content",
                }}>
                {phaseIDs.length > 1 && (
                    <>
                        <div style={{
                            position: "absolute",
                            left: `${trackInset}px`,
                            right: `${trackInset}px`,
                            top: `${trackTop}px`,
                            height: "1px",
                            background: theme.border,
                            opacity: 0.7,
                            pointerEvents: "none",
                        }} />
                        <div style={{
                            position: "absolute",
                            left: `${trackInset}px`,
                            top: `${trackTop}px`,
                            width: progressPercent <= 0 ? "0px" : `calc(${progressPercent}% - ${trackInsetTotal * progressPercent / 100}px)`,
                            height: "1px",
                            background: theme.accentColor,
                            opacity: 0.75,
                            pointerEvents: "none",
                        }} />
                    </>
                )}
                {phaseIDs.map((pid, index) => {
                    const hasDoc = phaseDocuments.has(pid);
                    const expectsDocument = workflowPhaseExpectsDocument(pid, phaseDocumentExpectationMap);
                    const gate = gateResults.get(pid);
                    const isCurrent = pid === currentPhaseID;
                    const isViewing = pid === activePhaseID;
                    const isPast = currentIndex >= 0 && index < currentIndex;
                    const cardState = workflowProgressPhaseCardState({
                        expectsDocument,
                        gatePassed: gate?.passed,
                        hasDoc,
                        isCurrent,
                        isPast,
                    });
                    const accent = cardState.tone === "current"
                        ? theme.accentColor
                        : cardState.tone === "done"
                            ? "#10b981"
                            : cardState.tone === "attention"
                                ? "#f59e0b"
                                : theme.textMuted;
                    const nodeLabel = cardState.tone === "done" ? "✓" : cardState.tone === "attention" ? "!" : String(index + 1);
                    const softToneBg = cardState.tone === "current"
                        ? theme.accentBg
                        : cardState.tone === "done"
                            ? "rgba(16, 185, 129, 0.10)"
                            : cardState.tone === "attention"
                                ? "rgba(245, 158, 11, 0.11)"
                                : "transparent";
                    const phaseLabel = workflowPhaseLabel(lang, pid, phaseLabelMap);
                    const statusLabel = workflowPhaseStatusLabel(lang, cardState.status);
                    const ariaSeparator = lang === "en" ? ", " : "，";
                    return (
                        <button
                            key={pid}
                            type="button"
                            aria-label={`${phaseLabel}${ariaSeparator}${statusLabel}`}
                            aria-pressed={isViewing}
                            onClick={() => onSelectPhase(pid)}
                            style={{
                                minHeight: "68px",
                                padding: "8px 10px",
                                borderRadius: "8px",
                                border: `1px solid ${isViewing ? theme.accentColor : theme.border}`,
                                background: isViewing ? theme.accentBg : `linear-gradient(180deg, ${softToneBg}, ${theme.bg})`,
                                boxShadow: isViewing ? `0 0 0 1px ${theme.accentColor} inset` : "none",
                                color: theme.text,
                                cursor: "pointer",
                                opacity: cardState.emphasized ? 1 : 0.64,
                                textAlign: "left",
                                display: "grid",
                                gridTemplateRows: "auto 1fr",
                                gap: "7px",
                                minWidth: 0,
                                position: "relative",
                                zIndex: 1,
                                '--wails-draggable': 'no-drag',
                            } as any}
                            title={`${phaseLabel} · ${statusLabel}`}
                        >
                            <span style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "8px", minWidth: 0 }}>
                                <span style={{
                                    width: "24px",
                                    height: "24px",
                                    borderRadius: "50%",
                                    border: `1px solid ${accent}`,
                                    background: cardState.emphasized ? accent : "transparent",
                                    color: cardState.emphasized ? "#fff" : accent,
                                    display: "inline-flex",
                                    alignItems: "center",
                                    justifyContent: "center",
                                    fontSize: "11px",
                                    fontWeight: 800,
                                    lineHeight: 1,
                                    flexShrink: 0,
                                }}>{nodeLabel}</span>
                                <span style={{
                                    fontSize: "10px",
                                    color: accent,
                                    border: `1px solid ${accent}`,
                                    borderRadius: "999px",
                                    padding: "1px 6px",
                                    background: softToneBg,
                                    maxWidth: "82px",
                                    whiteSpace: "nowrap",
                                    overflow: "hidden",
                                    textOverflow: "ellipsis",
                                }}>
                                    {statusLabel}
                                </span>
                            </span>
                            <span style={{ fontSize: "12px", fontWeight: 700, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                                {phaseLabel}
                            </span>
                        </button>
                    );
                })}
                </div>
            </div>
        </div>
    );
}

function latestAvailablePhaseID(phaseIDs: string[], phaseDocuments: Map<string, string>): string {
    for (let i = phaseIDs.length - 1; i >= 0; i--) {
        const pid = phaseIDs[i];
        if (phaseDocuments.has(pid)) return pid;
    }
    return "";
}

function MissingWorkflowDocPlaceholder({
    activePhaseID,
    currentPhaseID,
    phaseDocuments,
    phaseDocumentExpectationMap,
    phaseIDs,
    phaseLabelMap,
    theme,
    lang,
}: {
    activePhaseID: string;
    currentPhaseID: string;
    phaseDocuments: Map<string, string>;
    phaseDocumentExpectationMap: Map<string, boolean>;
    phaseIDs: string[];
    phaseLabelMap: Map<string, string>;
    theme: DocPreviewTheme;
    lang?: string;
}) {
    const label = activePhaseID ? workflowPhaseLabel(lang, activePhaseID, phaseLabelMap) : localizeText(lang || "zh-Hans", "Current phase", "当前阶段", "目前階段");
    const isCurrent = activePhaseID === currentPhaseID;
    const available = phaseIDs.filter(pid => phaseDocuments.has(pid));
    const expectsDocument = workflowPhaseExpectsDocument(activePhaseID, phaseDocumentExpectationMap);
    return (
        <div style={{
            border: `1px dashed ${theme.border}`,
            borderRadius: "8px",
            padding: "18px",
            background: theme.quoteBg,
            color: theme.text,
        }}>
            <div style={{ fontSize: "15px", fontWeight: 700, marginBottom: "8px", color: theme.headingColor }}>
                {expectsDocument
                    ? localizeText(lang || "zh-Hans", `${label} document has not been generated`, `${label}文档尚未生成`, `${label}文檔尚未生成`)
                    : localizeText(lang || "zh-Hans", `${label} has no preview document`, `${label}暂无预览文档`, `${label}暫無預覽文檔`)}
            </div>
            <div style={{ fontSize: "13px", lineHeight: 1.7, color: theme.textMuted }}>
                {expectsDocument
                    ? isCurrent
                        ? localizeText(lang || "zh-Hans", "This phase is running or waiting for output. The document will appear here once generated.", "该阶段正在推进或等待产出，生成后会自动显示在这里。", "該階段正在推進或等待產出，生成後會自動顯示在這裡。")
                        : localizeText(lang || "zh-Hans", "No document content has been collected for this phase yet.", "该阶段还没有收集到文档内容。", "該階段還沒有收集到文檔內容。")
                    : isCurrent
                        ? localizeText(lang || "zh-Hans", "This phase mainly runs tools or creates external artifacts. Check the conversation and task output for progress.", "该阶段主要执行工具或生成外部产物，进展请以左侧对话和任务输出为准。", "該階段主要執行工具或生成外部產物，進展請以左側對話和任務輸出為準。")
                        : localizeText(lang || "zh-Hans", "This phase usually does not generate a Markdown preview document.", "该阶段通常不会生成 Markdown 预览文档。", "該階段通常不會生成 Markdown 預覽文檔。")}
                {available.length > 0 && (
                    <span>
                        {localizeText(lang || "zh-Hans", " Collected: ", " 当前已收集：", " 目前已收集：")}
                        {available.map(pid => workflowPhaseLabel(lang, pid, phaseLabelMap)).join(localizeText(lang || "zh-Hans", ", ", "、", "、"))}
                        {localizeText(lang || "zh-Hans", ".", "。", "。")}
                    </span>
                )}
            </div>
        </div>
    );
}

// ── Lightweight Markdown renderer (no external deps) ──

/** Detect a pipe-delimited table row */
function isDocTableRow(line: string): boolean {
    const trimmed = line.trim();
    return trimmed.startsWith("|") && trimmed.length > 1;
}

/** Detect a separator row like |---|---| */
function isDocSeparatorRow(line: string): boolean {
    const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
    return /^[\s|:\-]+$/.test(trimmed) && trimmed.includes("-");
}

/** Parse cells from a pipe-delimited row */
function parseDocTableCells(line: string): string[] {
    let trimmed = line.trim();
    if (trimmed.startsWith("|")) trimmed = trimmed.slice(1);
    if (trimmed.endsWith("|")) trimmed = trimmed.slice(0, -1);
    return trimmed.split("|").map(c => c.trim());
}

/** Render collected table lines into an HTML table */
function renderDocTable(tableLines: string[], key: string, theme: DocPreviewTheme): React.ReactNode {
    const dataRows = tableLines.filter(l => !isDocSeparatorRow(l));
    if (dataRows.length === 0) return null;
    if (tableLines.length < 2) return null;

    const headerCells = parseDocTableCells(dataRows[0]);
    const bodyRows = dataRows.slice(1);

    const cellStyle: React.CSSProperties = {
        border: `1px solid ${theme.border}`,
        padding: "6px 10px",
        textAlign: "left",
        fontSize: "13px",
        lineHeight: 1.5,
    };

    return (
        <div key={key} style={{ overflowX: "auto", margin: "8px 0" }}>
            <table style={{ borderCollapse: "collapse", width: "100%", color: theme.text }}>
                <thead>
                    <tr>
                        {headerCells.map((cell, ci) => (
                            <th key={ci} style={{ ...cellStyle, fontWeight: 600, background: theme.headerBg }}>
                                {renderInline(cell, theme)}
                            </th>
                        ))}
                    </tr>
                </thead>
                {bodyRows.length > 0 && (
                    <tbody>
                        {bodyRows.map((row, ri) => {
                            const cells = parseDocTableCells(row);
                            return (
                                <tr key={ri}>
                                    {headerCells.map((_, ci) => (
                                        <td key={ci} style={cellStyle}>
                                            {renderInline(cells[ci] || "", theme)}
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

        // Table: collect consecutive pipe-delimited rows
        if (isDocTableRow(line)) {
            flushList();
            const tblLines: string[] = [];
            while (i < lines.length && isDocTableRow(lines[i])) {
                tblLines.push(lines[i]);
                i++;
            }
            const rendered = renderDocTable(tblLines, `tbl-${nodes.length}`, theme);
            if (rendered) {
                nodes.push(rendered);
            } else {
                // Not a real table (single pipe-line), render as paragraph
                for (const tl of tblLines) {
                    nodes.push(
                        <p key={`p-tbl-${nodes.length}`} style={{ margin: "6px 0", lineHeight: "1.7" }}>
                            {renderInline(tl, theme)}
                        </p>
                    );
                }
            }
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

function isPreviewHeaderInteractiveTarget(target: EventTarget | null, currentTarget: HTMLElement): boolean {
    if (!(target instanceof HTMLElement) || target === currentTarget) return false;
    return !!target.closest('button, a, input, select, textarea, [role="button"], [data-preview-no-maximize="true"]');
}

/**
 * WorkflowDocPreview renders the right-side document preview panel
 * during workflow execution. Supports Markdown rendering, dark mode,
 * vertical scrollbar, and proper padding.
 */
export function WorkflowDocPreview({
    phaseDocuments,
    currentPhaseID,
    latestDocumentPhaseID,
    phases,
    workflowType,
    gateResults,
    lang,
    onClose,
    theme,
    onResizeStart,
    onToggleMaximize,
}: WorkflowDocPreviewProps) {
    const [viewingPhaseID, setViewingPhaseID] = useState(latestDocumentPhaseID || currentPhaseID);
    const userSelectedPhaseRef = useRef("");
    const lastLatestDocumentPhaseRef = useRef(latestDocumentPhaseID || "");
    const suppressNextHeaderDoubleClickRef = useRef(false);
    const phaseIDs = useMemo(
        () => workflowProgressPhaseIDs(workflowType, phaseDocuments, currentPhaseID, phases),
        [workflowType, phaseDocuments, currentPhaseID, phases],
    );
    const phaseLabelMap = useMemo(() => {
        const labels = new Map<string, string>();
        for (const phase of phases || []) {
            if (phase.id && phase.name) labels.set(phase.id, phase.name);
        }
        return labels;
    }, [phases]);
    const phaseDocumentExpectationMap = useMemo(() => {
        const expectations = new Map<string, boolean>();
        for (const phase of phases || []) {
            if (phase.id && typeof phase.expectsDocument === "boolean") {
                expectations.set(phase.id, phase.expectsDocument);
            }
        }
        return expectations;
    }, [phases]);
    const fallbackPhaseID = useMemo(
        () => latestAvailablePhaseID(phaseIDs, phaseDocuments),
        [phaseIDs, phaseDocuments],
    );

    useEffect(() => {
        if (phaseDocuments.size === 0 && !latestDocumentPhaseID) {
            userSelectedPhaseRef.current = "";
            lastLatestDocumentPhaseRef.current = "";
        } else if (userSelectedPhaseRef.current && !phaseIDs.includes(userSelectedPhaseRef.current)) {
            userSelectedPhaseRef.current = "";
        }
        if (
            latestDocumentPhaseID &&
            latestDocumentPhaseID !== lastLatestDocumentPhaseRef.current &&
            phaseDocuments.has(latestDocumentPhaseID)
        ) {
            userSelectedPhaseRef.current = "";
            lastLatestDocumentPhaseRef.current = latestDocumentPhaseID;
        }
        if (userSelectedPhaseRef.current) return;
        const nextPhaseID =
            latestDocumentPhaseID && phaseDocuments.has(latestDocumentPhaseID)
                ? latestDocumentPhaseID
                : currentPhaseID && phaseDocuments.has(currentPhaseID)
                    ? currentPhaseID
                    : fallbackPhaseID;
        if (nextPhaseID && nextPhaseID !== viewingPhaseID) {
            setViewingPhaseID(nextPhaseID);
        }
    }, [currentPhaseID, fallbackPhaseID, latestDocumentPhaseID, phaseDocuments, phaseIDs, viewingPhaseID]);

    const activePhaseID = viewingPhaseID || fallbackPhaseID || currentPhaseID;
    const content = phaseDocuments.get(activePhaseID) || "";
    const gateResult = gateResults.get(activePhaseID);
    const gateItems = Array.isArray(gateResult?.items) ? gateResult.items : [];
    const handleHeaderMouseDown = (e: React.MouseEvent<HTMLDivElement>) => {
        if (isPreviewHeaderInteractiveTarget(e.target, e.currentTarget)) return;
        if (e.detail !== 2) return;
        e.preventDefault();
        suppressNextHeaderDoubleClickRef.current = true;
        onToggleMaximize?.();
    };
    const handleHeaderDoubleClick = (e: React.MouseEvent<HTMLDivElement>) => {
        if (isPreviewHeaderInteractiveTarget(e.target, e.currentTarget)) return;
        if (suppressNextHeaderDoubleClickRef.current) {
            suppressNextHeaderDoubleClickRef.current = false;
            return;
        }
        onToggleMaximize?.();
    };

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
                {/* Header: title + close button — double-click to toggle maximize */}
                <div
                    onMouseDown={handleHeaderMouseDown}
                    onDoubleClick={handleHeaderDoubleClick}
                    style={{
                    display: "flex",
                    alignItems: "center",
                    padding: "8px 14px",
                    borderBottom: `1px solid ${theme.border}`,
                    background: theme.headerBg,
                    gap: "4px",
                    flexWrap: "wrap",
                    flexShrink: 0,
                    '--wails-draggable': 'no-drag',
                } as any}>
                    <div style={{ fontSize: "13px", fontWeight: 700, color: theme.text }}>
                        文档预览
                    </div>
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

                <WorkflowProgressBoard
                    activePhaseID={activePhaseID}
                    currentPhaseID={currentPhaseID}
                    gateResults={gateResults}
                    onSelectPhase={(phaseID) => {
                        userSelectedPhaseRef.current = phaseID;
                        setViewingPhaseID(phaseID);
                    }}
                    phaseDocuments={phaseDocuments}
                    phaseDocumentExpectationMap={phaseDocumentExpectationMap}
                    phaseIDs={phaseIDs}
                    phaseLabelMap={phaseLabelMap}
                    theme={theme}
                    lang={lang}
                />

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
                        {gateItems.length === 0 && (
                            <span style={{ marginLeft: "8px", color: theme.textMuted }}>暂无检查项</span>
                        )}
                        {gateItems.map((item, i) => (
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
                        : (
                            <MissingWorkflowDocPlaceholder
                                activePhaseID={activePhaseID}
                                currentPhaseID={currentPhaseID}
                                phaseDocuments={phaseDocuments}
                                phaseDocumentExpectationMap={phaseDocumentExpectationMap}
                                phaseIDs={phaseIDs}
                                phaseLabelMap={phaseLabelMap}
                                theme={theme}
                                lang={lang}
                            />
                        )
                    }
                </div>
            </div>
        </div>
    );
}
