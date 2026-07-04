import React, { useEffect, useMemo, useRef, useState } from "react";
import type { PhaseInfo, QualityGateResult } from "./useWorkflowState";
import { normalizeWorkflowPhaseID, FALLBACK_NON_DOCUMENT_PHASE_IDS } from "./workflowPhase";
import { localizeText } from "./aiAssistantI18n";
import { WORKFLOW_PHASE_META } from "./workflowPhaseMeta.generated";

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
                <div style={{ marginBottom: "4px", color: theme.textMuted }}>WARN Mermaid render failed: {error}</div>
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
                Rendering diagram...
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
    theme: DocPreviewTheme;
}

// phaseLabels mirrors the backend templates (the single source of truth) verbatim:
// every overlapping entry is character-for-character equal to the generated
// `name` in workflowPhaseMeta.generated.ts. The anti-drift contract test
// (workflowPhaseMeta.contract.test.ts, Property 4) enforces this. These labels
// are only read in degraded mode (when emitted Phase_Meta is absent); when
// metadata is present the dashboard renders from the emitted names instead.
// To change a label, edit corelib/workflow/templates.go and regenerate.
export const phaseLabels: Record<string, string> = {
    // Coding workflow
    requirements: "需求文档",
    tech_design: "技术设计",
    task_breakdown: "任务分解",
    implementation: "编码执行",
    review: "代码审查",
    // Maintenance workflow (lightweight coding)
    maint_analysis: "影响分析与方案",
    maint_execution: "执行改造",
    maint_verification: "验证确认",
    // Product design workflow
    problem_discovery: "问题发现",
    solution_design: "方案设计",
    prd: "产品需求文档",
    prototype: "原型设计",
    // Innovation workflow
    opportunity: "机会识别",
    ideation: "创意发散",
    validation: "可行性验证",
    roadmap: "路线图",
    action_plan: "行动计划",
    // Business plan workflow
    bp_requirement: "需求梳理与定位",
    bp_content: "核心内容撰写",
    bp_structure: "结构优化与数据校验",
    bp_visual_design: "PPT 脚本与视觉设计",
    bp_doc_generation: "文档生成",
    // Testing workflow
    test_strategy: "测试策略",
    test_design: "测试用例设计",
    test_environment: "环境准备",
    test_execution: "执行测试",
    defect_report: "缺陷报告",
    // Literature review workflow
    topic_definition: "选题与范围界定",
    literature_search: "文献检索与收集",
    screening_classification: "文献筛选与分类",
    content_extraction: "核心内容提取与分析",
    review_writing: "综述撰写",
    // Research report workflow
    requirement_scoping: "需求定义与范围",
    source_mapping: "信息源梳理",
    report_collection: "研报收集与摘要",
    insight_extraction: "核心观点提炼与对比",
    synthesis_report: "整合报告撰写",
    // Experiment design workflow
    hypothesis_formulation: "假设提出与研究问题",
    experiment_design: "实验设计与方法选择",
    variable_control: "变量控制与样本规划",
    data_collection: "数据采集方案",
    analysis_plan: "数据分析计划",
    // Grant proposal workflow
    topic_justification: "选题论证",
    research_status: "研究现状与文献基础",
    research_plan: "研究方案与技术路线",
    expected_outcomes: "预期成果与创新点",
    budget_plan: "经费预算与进度安排",
    // Paper writing workflow
    outline_design: "大纲构思与结构设计",
    methodology: "方法论撰写",
    results_presentation: "结果呈现",
    discussion_analysis: "讨论与分析",
    submission_prep: "投稿准备",
    // Project proposal workflow
    background_analysis: "背景分析与问题定义",
    goal_definition: "目标定义与范围界定",
    // solution_design already mapped above
    resource_assessment: "资源评估与排期",
    risk_contingency: "风险预案",
    // Event planning workflow
    requirement_confirm: "需求确认与目标设定",
    scheme_planning: "方案策划",
    process_design: "流程设计与时间线",
    material_checklist: "物料清单与供应商",
    execution_manual: "执行手册",
    // Competitive analysis workflow
    target_definition: "分析目标与维度定义",
    competitor_identification: "竞品识别与信息收集",
    dimension_comparison: "多维度对比分析",
    gap_analysis: "差异分析与洞察提炼",
    strategy_recommendation: "策略建议",
    // Presentation design workflow
    audience_goal: "受众与目标",
    content_outline: "内容大纲与逻辑线",
    style_specification: "风格与视觉规范",
    slide_scripting: "逐页脚本",
    ppt_generation: "PPT 生成",
    // Bid response workflow
    tender_analysis: "招标文件解析",
    qualification_response: "资质与业绩响应",
    technical_proposal: "技术方案编写",
    commercial_proposal: "商务报价编制",
    bid_document_assembly: "投标文件组装与检查",
    // Contract review workflow
    contract_parsing: "合同解析与概览",
    clause_risk_analysis: "条款风险分析",
    compliance_check: "合规性审查",
    modification_suggestions: "修改建议",
    review_summary: "审查意见书",
    // Due diligence workflow
    target_profiling: "目标公司画像",
    business_dd: "商业尽调",
    financial_dd: "财务尽调",
    legal_dd: "法律尽调",
    dd_conclusion: "尽调结论与建议",
    // Compliance audit workflow
    audit_scope: "审计范围与对象确认",
    compliance_assessment: "合规性评估",
    risk_rating: "风险评级与优先级",
    remediation_plan: "整改建议与行动计划",
    audit_report: "审计报告",
    // Patent analysis workflow
    tech_disclosure: "技术方案/专利文献解析",
    prior_art_search: "现有技术检索",
    infringement_assessment: "侵权风险/新颖性评估",
    // strategy_recommendation already defined in competitive analysis (same label)
    patent_report: "专利分析报告",
    // Changjiang Scholar application workflow
    cj_personal_profile: "个人资质与申报条件梳理",
    cj_academic_achievements: "学术成就与代表性成果",
    cj_research_plan: "聘期研究计划",
    cj_talent_cultivation: "人才培养与团队建设",
    cj_recommendation_summary: "推荐意见与申报书整合",
    // Changjiang Scholar review workflow
    cj_completeness_check: "基本信息完整性检测",
    cj_achievement_evaluation: "学术成果质量评估",
    cj_plan_feasibility: "研究计划可行性评估",
    cj_narrative_quality: "材料撰写质量评估",
    cj_improvement_report: "综合评估与修改建议报告",
    // NSFC Distinguished Youth Fund (杰青) application workflow
    dy_eligibility: "申请人资质与条件评估",
    dy_research_foundation: "研究工作基础与学术贡献",
    dy_research_proposal: "研究方案与创新点",
    dy_outcomes_budget: "预期成果与经费预算",
    dy_final_assembly: "申请书整合与润色",
    // NSFC Excellent Young Scientists Fund (优青) application workflow
    ey_eligibility: "申请人资质与条件评估",
    ey_research_accumulation: "研究积累与发展潜力",
    ey_research_proposal: "研究方案与关键科学问题",
    ey_outcomes_budget: "预期成果与经费预算",
    ey_final_assembly: "申请书整合与润色",
    // NSFC Youth Science Fund (青基) application workflow
    yf_rationale: "立项依据与研究内容",
    yf_foundation: "研究基础与可行性",
    yf_methodology: "研究方案与技术路线",
    yf_budget: "经费预算",
    yf_final_assembly: "申请书整合与润色",
    // NSFC General Program (面上项目) application workflow
    gp_rationale: "立项依据与研究内容",
    gp_foundation: "研究基础与工作条件",
    gp_methodology: "研究方案与技术路线",
    gp_budget: "经费预算与年度计划",
    gp_final_assembly: "申请书整合与润色",
    // NSFC Key Program (重点项目) application workflow
    kp_strategic_rationale: "战略需求与科学问题凝练",
    kp_team_foundation: "研究团队与工作基础",
    kp_research_plan: "研究方案与课题设置",
    kp_budget_management: "经费预算与管理计划",
    kp_final_assembly: "申请书整合与润色",
    // Paper reproduction workflow
    paper_analysis: "论文深度解读",
    reproduction_plan: "复现规划",
    env_and_data: "环境搭建与数据准备",
    baseline_reproduction: "基线实验复现",
    iterative_improvement: "迭代改进",
    experiment_report: "实验报告",
    // Patent application workflow
    pa_disclosure_parsing: "申请材料解析",
    pa_prior_art_search: "查新/近似检索分析",
    pa_claims_drafting: "权利要求/保护要点",
    pa_description_writing: "说明书/简要说明",
    pa_figures_organization: "附图/图片整理",
    pa_document_assembly: "申请文件组装与检查",
    // US Patent application workflow
    us_disclosure_analysis: "Disclosure Analysis / 交底书解析",
    us_prior_art_search: "Prior Art Search / 查新检索",
    us_claims_drafting: "Claims Drafting / 权利要求撰写",
    us_drawings: "Drawings / 附图生成与整理",
    us_specification_writing: "Specification Writing / 说明书撰写",
    us_application_assembly: "Application Assembly / 申请文件组装",
    // Gaokao application workflow
    gaokao_profile: "考生信息采集",
    gaokao_data_search: "录取数据检索与证据整理",
    gaokao_candidate_ranking: "候选院校专业排序",
    gaokao_final_plan: "填报参考资料与建议",
    verification: "验收确认",
    test_cases: "用例设计",
    outline: "内容大纲",
    // Legacy aliases (canonical ids -> generated names, kept consistent with the above)
    design: "技术设计",
    tasks: "任务分解",
};

// CORE_PHASE_ENGLISH_LABELS holds the English display labels for the three
// coding-workflow core phases — the only phases the dashboard translates to
// English. Every phase's Chinese label comes from the single source
// (`phaseLabels`, contract-tested against the backend templates via
// workflowPhaseMeta.generated.ts), so degraded-mode rendering can never drift
// from the backend. This map only layers the English variant on top for the
// three phases that have one.
const CORE_PHASE_ENGLISH_LABELS: Record<string, string> = {
    requirements: "Requirements",
    design: "Design",
    tasks: "Tasks",
};

function workflowPhaseLabel(lang: string | undefined, phaseID: string, phaseLabelMap: Map<string, string>): string {
    const metadataLabel = phaseLabelMap.get(phaseID);
    if (metadataLabel) return metadataLabel;
    // Degraded mode (no emitted metadata): resolve the Chinese label from the
    // single source (phaseLabels), applying canonical aliasing so legacy ids
    // (tech_design/task_breakdown) resolve too. The English variant, where one
    // exists, is layered on top — it is the only label not sourced from the
    // backend templates, so it cannot reintroduce label drift.
    const canonical = normalizeWorkflowPhaseID(phaseID) || phaseID;
    const chinese = phaseLabels[phaseID] || phaseLabels[canonical] || phaseID;
    const english = CORE_PHASE_ENGLISH_LABELS[canonical];
    if (english) return localizeText(lang || "zh-Hans", english, chinese, chinese);
    return chinese;
}

export const workflowPhaseOrders: Record<string, string[]> = {
    coding: ["requirements", "design", "tasks", "implementation", "verification"],
    testing: ["test_strategy", "test_cases", "test_environment", "test_execution", "defect_report"],
    presentation_design: ["audience_goal", "outline", "slide_scripting", "ppt_generation"],
    paper_reproduction: ["paper_analysis", "reproduction_plan", "env_and_data", "baseline_reproduction", "iterative_improvement", "experiment_report"],
    patent_application: ["pa_disclosure_parsing", "pa_prior_art_search", "pa_claims_drafting", "pa_figures_organization", "pa_description_writing", "pa_document_assembly"],
    us_patent_application: ["us_disclosure_analysis", "us_prior_art_search", "us_claims_drafting", "us_drawings", "us_specification_writing", "us_application_assembly"],
    gaokao_application: ["gaokao_profile", "gaokao_data_search", "gaokao_candidate_ranking", "gaokao_final_plan"],
};

// fallbackNonDocumentPhaseIDs is re-exported from workflowPhase.ts (the single
// frontend owner) so the degraded-mode set has exactly one definition. The
// anti-drift contract test imports this name from WorkflowDocPreview and
// thereby validates the one true set against the generated artifact.
export const fallbackNonDocumentPhaseIDs = FALLBACK_NON_DOCUMENT_PHASE_IDS;

/** One rendered phase row: id, resolved (language-neutral) label, single doc-expectation value. */
export interface ProgressPhase {
    id: string;
    label: string;
    expectsDocument: boolean;
}

/** Per-id label fallback used only in degraded mode: the hardcoded map, then the id itself. */
function fallbackPhaseLabel(phaseID: string): string {
    return phaseLabels[phaseID] || phaseID;
}

/** Per-id document-expectation fallback used only in degraded mode. */
function fallbackPhaseExpectsDocument(phaseID: string): boolean {
    return !fallbackNonDocumentPhaseIDs.has(phaseID);
}

/**
 * Resolve a single phase that appears only as an emitted document or as the current
 * phase (not in the template's metadata list). Label resolution order is
 * metadata name → fallback map → id-derived; doc-expectation is metadata → fallback.
 */
function deriveSingleProgressPhase(phaseID: string, phases: PhaseInfo[] | undefined): ProgressPhase {
    const meta = phases?.find(p => p.id === phaseID);
    const label = meta?.name && meta.name.trim() ? meta.name : fallbackPhaseLabel(phaseID);
    const expectsDocument = meta && typeof meta.expectsDocument === "boolean"
        ? meta.expectsDocument
        : fallbackPhaseExpectsDocument(phaseID);
    return { id: phaseID, label, expectsDocument };
}

/**
 * deriveProgressPhases is the single frontend reducer for the progress board.
 *
 * When emitted metadata (`phases`) is present and non-empty, order, labels, and the
 * document-expectation are derived from the metadata only — the hardcoded fallback maps
 * are never read for those fields (each field falls back per-id only when the metadata
 * itself omits it).
 *
 * When emitted metadata is absent/empty, it degrades in two tiers:
 *   1. The code-generated artifact `WORKFLOW_PHASE_META[workflowType]` — the byte-stable
 *      projection of the backend templates (the single source of truth). This covers
 *      EVERY registered backend template, carries authoritative labels and doc-flags,
 *      and is regenerated by `go generate`, so it can never drift. It is treated exactly
 *      like emitted metadata.
 *   2. Only when the generated artifact has no entry for the type (a truly unknown type)
 *      does it fall to the hand-maintained `workflowPhaseOrders`/`phaseLabels`/
 *      `fallbackNonDocumentPhaseIDs` maps as a last resort.
 *
 * Phase ids seen only in `phaseDocuments` or as `currentPhaseID` are appended to the end,
 * with labels resolved metadata → fallback map → id-derived. The result is duplicate-free.
 */
export function deriveProgressPhases(
    workflowType: string | undefined,
    phases: PhaseInfo[] | undefined,
    phaseDocuments: Map<string, string>,
    currentPhaseID: string,
): ProgressPhase[] {
    const base: ProgressPhase[] = [];
    const seen = new Set<string>();
    const append = (phase: ProgressPhase) => {
        if (!phase.id || seen.has(phase.id)) return;
        seen.add(phase.id);
        base.push(phase);
    };

    // Tier 0: emitted metadata. Tier 1: the generated artifact (backend truth, all
    // registered types). Both share the PhaseInfo shape, so they take the same path —
    // order/labels/doc-flags come from the metadata only.
    const generatedMeta = workflowType ? WORKFLOW_PHASE_META[workflowType] : undefined;
    const metadata: PhaseInfo[] | undefined =
        phases && phases.length > 0
            ? phases
            : generatedMeta && generatedMeta.length > 0
                ? generatedMeta
                : undefined;

    if (metadata) {
        for (const p of [...metadata].sort((a, b) => a.index - b.index)) {
            if (!p.id || seen.has(p.id)) continue;
            const label = p.name && p.name.trim() ? p.name : fallbackPhaseLabel(p.id);
            const expectsDocument = typeof p.expectsDocument === "boolean"
                ? p.expectsDocument
                : fallbackPhaseExpectsDocument(p.id);
            append({ id: p.id, label, expectsDocument });
        }
    } else {
        // Last resort: the hand-maintained maps, only for a type the generated artifact
        // does not cover (an unknown/legacy type).
        const order = workflowType ? workflowPhaseOrders[workflowType] || [] : [];
        for (const id of order) {
            append({ id, label: fallbackPhaseLabel(id), expectsDocument: fallbackPhaseExpectsDocument(id) });
        }
    }

    // Robustness: surface phases seen only as emitted documents or as the active phase.
    for (const pid of phaseDocuments.keys()) {
        if (!seen.has(pid)) append(deriveSingleProgressPhase(pid, phases));
    }
    if (currentPhaseID && !seen.has(currentPhaseID)) {
        append(deriveSingleProgressPhase(currentPhaseID, phases));
    }

    return base;
}

export function workflowProgressPhaseIDs(workflowType: string | undefined, phaseDocuments: Map<string, string>, currentPhaseID: string, phases?: PhaseInfo[]): string[] {
    return deriveProgressPhases(workflowType, phases, phaseDocuments, currentPhaseID).map(p => p.id);
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
    if (expectsDocument && hasDoc && isCurrent) {
        return { status: "待确认", tone: "current", emphasized: true };
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

    // Requirement 4.1: mark exactly one active node — the node whose canonical phase id
    // (aliases applied) equals the supplied current phase id (also canonicalized). Aliasing
    // is applied on BOTH sides so a non-canonical alias (e.g. tech_design/task_breakdown) or
    // a not-yet-canonicalized caller still resolves to exactly one node within the resolved
    // phase-id list. indexOf returns the first match, guaranteeing at most one active node.
    const canonicalCurrentPhaseID = currentPhaseID ? normalizeWorkflowPhaseID(currentPhaseID) : "";
    const currentIndex = canonicalCurrentPhaseID
        ? phaseIDs.findIndex(pid => normalizeWorkflowPhaseID(pid) === canonicalCurrentPhaseID)
        : -1;
    const documentPhaseIDs = phaseIDs.filter(pid => workflowPhaseExpectsDocument(pid, phaseDocumentExpectationMap));
    const collectedCount = documentPhaseIDs.filter(pid => phaseDocuments.has(pid)).length;
    const documentSummaryText = documentPhaseIDs.length > 0
        ? localizeText(lang || "zh-Hans", `${collectedCount}/${documentPhaseIDs.length} docs`, `${collectedCount}/${documentPhaseIDs.length} 个文档`, `${collectedCount}/${documentPhaseIDs.length} 個文檔`)
        : localizeText(lang || "zh-Hans", "Execution phase", "执行阶段", "執行階段");
    // Requirement 4.2: progress is a monotonic function of the active node's zero-based index
    // within the resolved phase-id list, attaining its maximum (100%) only at the final phase.
    // Driving it from the active node's index (rather than max(currentIndex, latestCollectedIndex))
    // ensures a later-collected document cannot push progress to 100% before the final phase.
    const progressIndex = currentIndex >= 0 ? currentIndex : 0;
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
                    // Exactly one active node: the one at currentIndex (alias-resolved above),
                    // so a non-canonical current phase id still highlights its canonical node.
                    const isCurrent = currentIndex >= 0 && index === currentIndex;
                    const isViewing = pid === activePhaseID;
                    const isViewingOnly = isViewing && !isCurrent;
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
                            ? "#4f7f6f"
                            : cardState.tone === "attention"
                                ? theme.accentColor
                                : theme.textMuted;
                    const nodeLabel = cardState.tone === "done" ? "OK" : cardState.tone === "attention" ? "REV" : String(index + 1);
                    const softToneBg = cardState.tone === "current"
                        ? theme.accentBg
                        : cardState.tone === "done"
                            ? "rgba(79, 127, 111, 0.08)"
                            : cardState.tone === "attention"
                                ? theme.accentBg
                                : "transparent";
                    const phaseLabel = workflowPhaseLabel(lang, pid, phaseLabelMap);
                    const statusLabel = workflowPhaseStatusLabel(lang, cardState.status);
                    const ariaSeparator = lang === "en" ? ", " : "，";
                    return (
                        <button
                            key={pid}
                            type="button"
                            aria-label={`${phaseLabel}${ariaSeparator}${statusLabel}`}
                            aria-current={isCurrent ? "step" : undefined}
                            aria-pressed={isViewing}
                            onClick={() => onSelectPhase(pid)}
                            style={{
                                minHeight: "68px",
                                padding: "8px 10px",
                                borderRadius: "7px",
                                border: `1px solid ${isCurrent ? theme.accentColor : theme.border}`,
                                background: isCurrent ? theme.accentBg : (cardState.emphasized ? softToneBg : theme.bg),
                                boxShadow: isCurrent
                                    ? `0 0 0 1px ${theme.accentColor} inset`
                                    : isViewingOnly
                                        ? `0 0 0 1px ${theme.border} inset`
                                        : "none",
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
                                    fontSize: nodeLabel.length > 1 ? "9px" : "11px",
                                    fontWeight: 700,
                                    lineHeight: 1,
                                    flexShrink: 0,
                                }}>{nodeLabel}</span>
                                <span style={{
                                    fontSize: "10px",
                                    color: accent,
                                    border: `1px solid ${accent}`,
                                    borderRadius: "999px",
                                    padding: "1px 6px",
                                    background: cardState.emphasized ? softToneBg : "transparent",
                                    maxWidth: "82px",
                                    whiteSpace: "nowrap",
                                    overflow: "hidden",
                                    textOverflow: "ellipsis",
                                }}>
                                    {statusLabel}
                                </span>
                            </span>
                            <span style={{ fontSize: "12px", fontWeight: 700, color: theme.text, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
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
    const lines = md.replace(/\r\n/g, "\n").replace(/\r/g, "\n").split("\n");
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
                    borderLeft: `1px solid ${theme.quoteBorder}`,
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

        // Block-level image: standalone ![alt](src) on its own line
        const imgMatch = line.trim().match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
        if (imgMatch) {
            flushList();
            const alt = imgMatch[1] || "";
            const src = imgMatch[2] || "";
            const isSvg = src.includes("image/svg+xml") || src.toLowerCase().endsWith(".svg");
            nodes.push(
                <div key={`img-${i}`} style={{ margin: "12px 0", textAlign: "center" }}>
                    <img src={src} alt={alt} loading="lazy" style={{
                        maxWidth: "100%",
                        maxHeight: "600px",
                        borderRadius: "6px",
                        border: `1px solid ${theme.border}`,
                        // SVGs may have transparent background with dark strokes —
                        // ensure visibility in dark mode by adding a white backdrop.
                        background: isSvg ? "#ffffff" : undefined,
                        padding: isSvg ? "8px" : undefined,
                    }} />
                    {alt && <div style={{ fontSize: "12px", color: theme.textMuted, marginTop: "4px" }}>{alt}</div>}
                </div>
            );
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
    // Regex: ![alt](src) images, **bold**, *italic*, `code`, [text](url)
    // Image pattern must come BEFORE link pattern because ![...] starts with [
    const re = /(!\[([^\]]*)\]\(([^)]+)\))|(\*\*(.+?)\*\*)|(\*(.+?)\*)|(`([^`]+?)`)|(\[([^\]]+)\]\(([^)]+)\))/g;
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let key = 0;
    while ((match = re.exec(text)) !== null) {
        if (match.index > lastIndex) {
            parts.push(text.slice(lastIndex, match.index));
        }
        if (match[1]) { // image ![alt](src)
            const alt = match[2] || "";
            const src = match[3] || "";
            const isSvg = src.includes("image/svg+xml") || src.toLowerCase().endsWith(".svg");
            parts.push(<img key={key++} src={src} alt={alt} loading="lazy" style={{
                maxWidth: "100%",
                maxHeight: "320px",
                borderRadius: "6px",
                border: `1px solid ${theme.border}`,
                display: "inline-block",
                verticalAlign: "middle",
                margin: "4px 0",
                background: isSvg ? "#ffffff" : undefined,
                padding: isSvg ? "4px" : undefined,
            }} />);
        } else if (match[4]) { // bold
            parts.push(<strong key={key++} style={{ fontWeight: 600 }}>{match[5]}</strong>);
        } else if (match[6]) { // italic
            parts.push(<em key={key++} style={{ fontStyle: "italic", color: theme.text, fontWeight: 500 }}>{match[7]}</em>);
        } else if (match[8]) { // inline code
            parts.push(<code key={key++} style={{
                background: theme.codeBg,
                color: theme.codeText,
                padding: "1px 5px",
                borderRadius: "3px",
                fontSize: "0.9em",
                fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace",
            }}>{match[9]}</code>);
        } else if (match[10]) { // link
            parts.push(<a key={key++} href={match[12]} style={{ color: theme.linkColor, textDecoration: "underline" }} target="_blank" rel="noopener noreferrer">{match[11]}</a>);
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
    latestDocumentPhaseID,
    phases,
    workflowType,
    gateResults,
    lang,
    theme,
}: WorkflowDocPreviewProps) {
    const [viewingPhaseID, setViewingPhaseID] = useState(latestDocumentPhaseID || currentPhaseID);
    const userSelectedPhaseRef = useRef("");
    const lastLatestDocumentPhaseRef = useRef(latestDocumentPhaseID || "");
    const progress = useMemo(
        () => deriveProgressPhases(workflowType, phases, phaseDocuments, currentPhaseID),
        [workflowType, phases, phaseDocuments, currentPhaseID],
    );
    const phaseIDs = useMemo(() => progress.map(p => p.id), [progress]);
    const phaseLabelMap = useMemo(() => {
        const labels = new Map<string, string>();
        for (const phase of phases || []) {
            if (phase.id && phase.name) labels.set(phase.id, phase.name);
        }
        return labels;
    }, [phases]);
    const phaseDocumentExpectationMap = useMemo(() => {
        // Single derived document-expectation value per phase, so every status
        // indicator (board card, summary, placeholder) reads the same value and the
        // generation/execution indicators can never disagree (Finding 3).
        const expectations = new Map<string, boolean>();
        for (const phase of progress) {
            expectations.set(phase.id, phase.expectsDocument);
        }
        return expectations;
    }, [progress]);
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
    const currentPhaseMeta = useMemo(
        () => (phases || []).find(phase => phase.id === normalizeWorkflowPhaseID(currentPhaseID)),
        [currentPhaseID, phases],
    );
    const awaitingReview = currentPhaseMeta?.status === "waiting_confirm";

    return (
        <div style={{
            display: "flex",
            flexDirection: "column",
            height: "100%",
            minWidth: 0,
            background: theme.bg,
            color: theme.text,
        }}>
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

                {awaitingReview && (
                    <div style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "flex-start",
                        gap: "12px",
                        padding: "10px 14px",
                        borderBottom: `1px solid ${theme.border}`,
                        background: theme.headerBg,
                    }}>
                        <div style={{ fontSize: "12px", color: theme.textMuted, lineHeight: 1.6 }}>
                            {localizeText(lang || "zh-Hans", "This phase is waiting for review. Use the action bar above the input box to continue.", "当前阶段正在等待确认。请使用输入框上方的操作栏继续。", "當前階段正在等待確認。請使用輸入框上方的操作列繼續。")}
                        </div>
                    </div>
                )}

                {/* Quality gate banner */}
                {gateResult && (
                    <div style={{
                        padding: "6px 14px",
                        fontSize: "12px",
                        borderBottom: `1px solid ${theme.border}`,
                        background: gateResult.passed ? "rgba(79,127,111,0.10)" : theme.accentBg,
                        color: theme.text,
                        flexShrink: 0,
                    }}>
                        {gateResult.passed ? "OK" : "WARN"} 质量门禁：
                        {gateItems.length === 0 && (
                            <span style={{ marginLeft: "8px", color: theme.textMuted }}>暂无检查项</span>
                        )}
                        {gateItems.map((item, i) => (
                            <span key={i} style={{ marginLeft: "8px" }}>
                                {item.passed ? "OK" : "WARN"} {item.description}
                            </span>
                        ))}
                    </div>
                )}

                {/* Document content — Markdown rendered */}
                <div className="ai-chat-scrollbar" style={{
                    flex: 1,
                    overflowY: "auto",
                    overflowX: "hidden",
                    padding: "18px 24px 18px 28px",
                    fontSize: "14px",
                    lineHeight: "1.65",
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
    );
}
