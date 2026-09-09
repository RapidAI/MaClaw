/**
 * Workflow type data definitions for the Workflows panel.
 * Icons are short keys rendered as professional SVGs (not emoji).
 */

import type { ReactNode } from "react";

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

export type WorkflowIconKey =
    | "code"
    | "keyboard"
    | "monitor"
    | "test"
    | "wrench"
    | "hammer"
    | "books"
    | "chart"
    | "flask"
    | "pen"
    | "refresh"
    | "clipboard"
    | "palette"
    | "trend"
    | "trophy"
    | "bulb"
    | "proposal"
    | "document"
    | "check"
    | "search"
    | "write"
    | "building"
    | "presentation"
    | "target"
    | "package"
    | "graduate"
    | "book"
    | "medal"
    | "star"
    | "seed"
    | "bookBlue"
    | "key"
    | "bag";

export interface WorkflowShortcutItem {
    type: string;
    icon: WorkflowIconKey;
    label: string;
    description: string;
}

export interface WorkflowShortcutGroup {
    category: string;
    items: WorkflowShortcutItem[];
}

/** Compact multi-path SVG for workflow tiles. */
export function WorkflowShortcutIcon({ name, size = 22 }: { name: WorkflowIconKey | string; size?: number }) {
    const s = { fill: "none", stroke: "currentColor", strokeWidth: 1.55, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
    const paths: Record<string, ReactNode> = {
        code: (<><path {...s} d="m8 8-3.5 4L8 16" /><path {...s} d="m16 8 3.5 4L16 16" /><path {...s} d="m13.2 6-2.4 12" /></>),
        keyboard: (<><rect {...s} x="3" y="7" width="18" height="11" rx="1.5" /><path {...s} d="M7 10.5h.01M10.5 10.5h.01M14 10.5h.01M17.5 10.5h.01" /><path {...s} d="M7 14h10" /></>),
        monitor: (<><rect {...s} x="3.5" y="4" width="17" height="12" rx="1.5" /><path {...s} d="M8 20h8M12 16v4" /></>),
        test: (<><path {...s} d="M9 3h6" /><path {...s} d="M10 3v6l-4.5 8.5A2 2 0 0 0 7.2 21h9.6a2 2 0 0 0 1.7-3.5L14 9V3" /><path {...s} d="M8.5 14h7" /></>),
        wrench: (<><path {...s} d="M14.7 6.3a4 4 0 0 0-5.5 5.5L4 17l3 3 5.2-5.2a4 4 0 0 0 5.5-5.5l-2.5 2.5-3-3 2.5-2.5Z" /></>),
        hammer: (<><path {...s} d="m15 5 4 4-9 9-4-4 9-9Z" /><path {...s} d="M6 18l-2 3" /><path {...s} d="M14 6l4 4" /></>),
        books: (<><path {...s} d="M4 5h5v15H4zM10 5h5v15h-5z" /><path {...s} d="M16 7h4v13h-4" /><path {...s} d="M6.5 9h2M12.5 9h2" /></>),
        chart: (<><path {...s} d="M4 19V10M9 19V6M14 19v-7M19 19V8" /><path {...s} d="M3 19h18" /></>),
        flask: (<><path {...s} d="M9 3h6" /><path {...s} d="M10 3v5.5L5.8 17A2 2 0 0 0 7.5 20h9a2 2 0 0 0 1.7-3L14 8.5V3" /><path {...s} d="M8 14h8" /></>),
        pen: (<><path {...s} d="M5 19h4L19 9l-4-4L5 15v4Z" /><path {...s} d="m13.5 6.5 4 4" /></>),
        refresh: (<><path {...s} d="M20 6v5h-5" /><path {...s} d="M4 18v-5h5" /><path {...s} d="M18 10a6 6 0 0 0-10-3L4 11" /><path {...s} d="M6 14a6 6 0 0 0 10 3l4-4" /></>),
        clipboard: (<><rect {...s} x="6" y="5" width="12" height="16" rx="1.5" /><path {...s} d="M9 5V4h6v1" /><path {...s} d="M9 10h6M9 13.5h6M9 17h4" /></>),
        palette: (<><path {...s} d="M12 4a8 8 0 1 0 0 16h1.2a2.2 2.2 0 0 0 0-4.4H12a1.6 1.6 0 0 1 0-3.2h4.5A8 8 0 0 0 12 4Z" /><circle {...s} cx="8.2" cy="10" r="0.9" fill="currentColor" stroke="none" /><circle {...s} cx="10" cy="7.5" r="0.9" fill="currentColor" stroke="none" /><circle {...s} cx="14" cy="7.5" r="0.9" fill="currentColor" stroke="none" /></>),
        trend: (<><path {...s} d="M4 18h16" /><path {...s} d="m5 14 4-4 3 3 6-7" /><path {...s} d="M15 6h3v3" /></>),
        trophy: (<><path {...s} d="M8 4h8v4a4 4 0 0 1-8 0V4Z" /><path {...s} d="M8 6H5.5A2.5 2.5 0 0 0 8 9.8" /><path {...s} d="M16 6h2.5A2.5 2.5 0 0 1 16 9.8" /><path {...s} d="M10 14h4v3h-4z" /><path {...s} d="M8 20h8" /></>),
        bulb: (<><path {...s} d="M9 18h6M10 21h4" /><path {...s} d="M8.5 15.2A5.5 5.5 0 1 1 15.5 15.2c-.8 1-1.5 1.8-1.5 3.3h-4c0-1.5-.7-2.3-1.5-3.3Z" /></>),
        proposal: (<><path {...s} d="M7 3h7l4 4v13a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Z" /><path {...s} d="M14 3v4h4" /><path {...s} d="M9 12h7M9 15h5" /></>),
        document: (<><path {...s} d="M7 3.5h7l4 4V20a1.5 1.5 0 0 1-1.5 1.5h-9.5A1.5 1.5 0 0 1 5.5 20V5A1.5 1.5 0 0 1 7 3.5Z" /><path {...s} d="M14 3.5V8h4.5" /><path {...s} d="M9 12h6M9 15.5h6M9 19h3.5" /></>),
        check: (<><circle {...s} cx="12" cy="12" r="9" /><path {...s} d="m8.2 12.2 2.4 2.4 5.2-5.4" /></>),
        search: (<><circle {...s} cx="11" cy="11" r="6.5" /><path {...s} d="m16 16 4.2 4.2" /></>),
        write: (<><path {...s} d="M5 19h4L19 9l-4-4L5 15v4Z" /><path {...s} d="m13.5 6.5 4 4" /><path {...s} d="M14 19h5" /></>),
        building: (<><path {...s} d="M4 20h16" /><path {...s} d="M6 20V6.5A1.5 1.5 0 0 1 7.5 5h9A1.5 1.5 0 0 1 18 6.5V20" /><path {...s} d="M9 9h1.5M13.5 9H15M9 12.5h1.5M13.5 12.5H15" /><path {...s} d="M10.5 20v-3h3v3" /></>),
        presentation: (<><rect {...s} x="3.5" y="4" width="17" height="11" rx="1.2" /><path {...s} d="M12 15v5M8 20h8" /><path {...s} d="M7 8h4v4H7z" /><path {...s} d="M13 9h4M13 12h3" /></>),
        target: (<><circle {...s} cx="12" cy="12" r="8" /><circle {...s} cx="12" cy="12" r="4.5" /><circle {...s} cx="12" cy="12" r="1.3" fill="currentColor" stroke="none" /></>),
        package: (<><path {...s} d="M12 3 20 7.5v9L12 21 4 16.5v-9L12 3Z" /><path {...s} d="M12 12 20 7.5" /><path {...s} d="M12 12v9" /><path {...s} d="M12 12 4 7.5" /></>),
        graduate: (<><path {...s} d="m3 10 9-5 9 5-9 5-9-5Z" /><path {...s} d="M7 12.2v4.3c0 1.3 2.2 2.5 5 2.5s5-1.2 5-2.5v-4.3" /><path {...s} d="M21 10v6" /></>),
        book: (<><path {...s} d="M5 4h6a3 3 0 0 1 3 3v13a3 3 0 0 0-3-2H5V4Z" /><path {...s} d="M19 4h-5a3 3 0 0 0-3 3" /><path {...s} d="M19 4v14h-5" /></>),
        medal: (<><circle {...s} cx="12" cy="9" r="5" /><path {...s} d="m9 13.5-1.5 7.5L12 18l4.5 3L15 13.5" /></>),
        star: (<><path {...s} d="m12 3.4 2.3 4.7 5.2.8-3.8 3.6.9 5.2L12 15.6 7.4 17.7l.9-5.2L4.5 8.9l5.2-.8L12 3.4Z" /></>),
        seed: (<><path {...s} d="M12 20V11" /><path {...s} d="M12 11c-3 0-5.5-2-6-5 3.5.3 6 2.5 6 5Z" /><path {...s} d="M12 11c3 0 5.5-2 6-5-3.5.3-6 2.5-6 5Z" /></>),
        bookBlue: (<><path {...s} d="M5 4h6a3 3 0 0 1 3 3v13a3 3 0 0 0-3-2H5V4Z" /><path {...s} d="M19 4h-5a3 3 0 0 0-3 3" /><path {...s} d="M19 4v14h-5" /><path {...s} d="M8 9h3" /></>),
        key: (<><circle {...s} cx="8" cy="14" r="3.5" /><path {...s} d="M11 12.5 20 4.5" /><path {...s} d="M16.5 8 19 5.5M18 9.5 20.5 7" /></>),
        bag: (<><path {...s} d="M6 9h12l-1 11H7L6 9Z" /><path {...s} d="M9 9V7a3 3 0 0 1 6 0v2" /></>),
    };
    return (
        <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true" style={{ display: "block" }}>
            {paths[name] || paths.document}
        </svg>
    );
}

/**
 * Returns the full list of supported workflow types with icons and labels.
 * Grouped by category for display.
 */
export function getAllWorkflowShortcuts(localizeText: LocalizeText): WorkflowShortcutGroup[] {
    return [
        {
            category: localizeText("Software & Development", "软件与开发", "軟體與開發"),
            items: [
                { type: "coding", icon: "code", label: localizeText("Coding", "编程开发", "程式開發"), description: localizeText("Full-stack coding workflow", "完整编码开发流程", "完整編碼開發流程") },
                { type: "testing", icon: "test", label: localizeText("Testing", "测试", "測試"), description: localizeText("Test strategy & execution", "测试策略与执行", "測試策略與執行") },
                { type: "ops_maintenance", icon: "wrench", label: localizeText("Ops & Maintenance", "运维", "運維"), description: localizeText("Operations maintenance", "运维管理", "運維管理") },
                { type: "maintenance", icon: "hammer", label: localizeText("Refactor", "重构改造", "重構改造"), description: localizeText("Refactoring & migration", "架构重构与技术迁移", "架構重構與技術遷移") },
            ],
        },
        {
            category: localizeText("Research & Academic", "学术研究", "學術研究"),
            items: [
                { type: "literature_review", icon: "books", label: localizeText("Literature Review", "文献综述", "文獻綜述"), description: localizeText("Academic literature review", "学术文献综述", "學術文獻綜述") },
                { type: "research_report", icon: "chart", label: localizeText("Research Report", "研究报告", "研究報告"), description: localizeText("Research report writing", "研究报告撰写", "研究報告撰寫") },
                { type: "experiment_design", icon: "flask", label: localizeText("Experiment Design", "实验设计", "實驗設計"), description: localizeText("Experimental methodology", "实验方法设计", "實驗方法設計") },
                { type: "paper_writing", icon: "pen", label: localizeText("Paper Writing", "论文写作", "論文寫作"), description: localizeText("Academic paper writing", "学术论文撰写", "學術論文撰寫") },
                { type: "paper_reproduction", icon: "refresh", label: localizeText("Paper Reproduction", "论文复现", "論文復現"), description: localizeText("Reproduce paper results", "论文实验复现", "論文實驗復現") },
                { type: "grant_proposal", icon: "clipboard", label: localizeText("Grant Proposal", "基金申请", "基金申請"), description: localizeText("Grant proposal writing", "科研基金申请书", "科研基金申請書") },
            ],
        },
        {
            category: localizeText("Business & Strategy", "商业战略", "商業戰略"),
            items: [
                { type: "product_design", icon: "palette", label: localizeText("Product Design", "产品设计", "產品設計"), description: localizeText("Product design workflow", "产品设计流程", "產品設計流程") },
                { type: "business_plan", icon: "trend", label: localizeText("Business Plan", "商业计划", "商業計畫"), description: localizeText("Business plan writing", "商业计划书", "商業計畫書") },
                { type: "competitive_analysis", icon: "trophy", label: localizeText("Competitive Analysis", "竞品分析", "競品分析"), description: localizeText("Competitive analysis", "竞争对手分析", "競爭對手分析") },
                { type: "innovation", icon: "bulb", label: localizeText("Innovation", "创新方案", "創新方案"), description: localizeText("Innovation design", "创新方案设计", "創新方案設計") },
                { type: "project_proposal", icon: "proposal", label: localizeText("Project Proposal", "项目提案", "專案提案"), description: localizeText("Project proposal", "项目立项提案", "專案立項提案") },
                { type: "bid_response", icon: "package", label: localizeText("Bid Response", "招投标", "招投標"), description: localizeText("Generate bid response documents", "根据招标文件生成投标材料", "根據招標文件生成投標材料") },
                { type: "bid_review", icon: "search", label: localizeText("Bid Review", "标书检查", "標書檢查"), description: localizeText("Review prepared bid vs tender standards", "对照招标标准检查标书并生成修改建议", "對照招標標準檢查標書並生成修改建議") },
            ],
        },
        {
            category: localizeText("Legal & Compliance", "法律合规", "法律合規"),
            items: [
                { type: "contract_review", icon: "document", label: localizeText("Contract Review", "合同审查", "合同審查"), description: localizeText("Contract risk review", "合同条款风险审查", "合同條款風險審查") },
                { type: "compliance_audit", icon: "check", label: localizeText("Compliance Audit", "合规审计", "合規審計"), description: localizeText("Compliance audit", "企业合规审计", "企業合規審計") },
                { type: "patent_analysis", icon: "search", label: localizeText("Patent Analysis", "专利分析", "專利分析"), description: localizeText("Patent analysis", "专利检索与分析", "專利檢索與分析") },
                { type: "patent_application", icon: "write", label: localizeText("Patent Application", "专利申请", "專利申請"), description: localizeText("Patent application", "专利撰写申请", "專利撰寫申請") },
                { type: "us_patent_application", icon: "building", label: localizeText("US Patent", "美国专利", "美國專利"), description: localizeText("USPTO patent application", "美国 USPTO 专利申请", "美國 USPTO 專利申請") },
                { type: "due_diligence", icon: "building", label: localizeText("Due Diligence", "尽职调查", "盡職調查"), description: localizeText("Due diligence", "投资尽职调查", "投資盡職調查") },
            ],
        },
        {
            category: localizeText("Presentation & Events", "展示与活动", "展示與活動"),
            items: [
                { type: "presentation_design", icon: "presentation", label: localizeText("PPT Design", "PPT 设计", "PPT 設計"), description: localizeText("Presentation design", "演示文稿设计", "簡報設計") },
                { type: "event_planning", icon: "target", label: localizeText("Event Planning", "活动策划", "活動策劃"), description: localizeText("Event planning", "活动策划方案", "活動策劃方案") },
            ],
        },
        {
            category: localizeText("Academic Funding", "学术基金", "學術基金"),
            items: [
                { type: "changjiang_scholar", icon: "graduate", label: localizeText("Changjiang Scholar", "长江学者", "長江學者"), description: localizeText("Changjiang Scholar application", "长江学者申请", "長江學者申請") },
                { type: "changjiang_scholar_review", icon: "book", label: localizeText("Scholar Review", "长江学者评审", "長江學者評審"), description: localizeText("Changjiang Scholar application review", "长江学者申报书评审", "長江學者申報書評審") },
                { type: "nsfc_distinguished_youth", icon: "medal", label: localizeText("NSFC Distinguished Youth", "杰青", "傑青"), description: localizeText("NSFC Distinguished Youth Fund", "国家杰出青年基金", "國家傑出青年基金") },
                { type: "nsfc_excellent_youth", icon: "star", label: localizeText("NSFC Excellent Youth", "优青", "優青"), description: localizeText("NSFC Excellent Youth Fund", "国家优秀青年基金", "國家優秀青年基金") },
                { type: "nsfc_youth", icon: "seed", label: localizeText("NSFC Youth", "青年基金", "青年基金"), description: localizeText("NSFC Youth Fund", "国家自然科学基金青年项目", "國家自然科學基金青年項目") },
                { type: "nsfc_general", icon: "bookBlue", label: localizeText("NSFC General", "面上基金", "面上基金"), description: localizeText("NSFC General Program", "国家自然科学基金面上项目", "國家自然科學基金面上項目") },
                { type: "nsfc_key", icon: "key", label: localizeText("NSFC Key", "重点基金", "重點基金"), description: localizeText("NSFC Key Program", "国家自然科学基金重点项目", "國家自然科學基金重點項目") },
            ],
        },
        {
            category: localizeText("Education", "教育", "教育"),
            items: [
                { type: "gaokao_application", icon: "bag", label: localizeText("Gaokao Guidance", "高考志愿", "高考志願"), description: localizeText("College application guidance", "高考志愿填报参考", "高考志願填報參考") },
            ],
        },
    ];
}
