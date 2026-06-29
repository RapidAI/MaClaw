/**
 * Workflow type data definitions for the Workflows panel.
 * Exports the full list of supported workflow types grouped by category.
 */

type LocalizeText = (en: string, zhHans: string, zhHant: string) => string;

export interface WorkflowShortcutItem {
    type: string;
    icon: string;
    label: string;
    description: string;
}

export interface WorkflowShortcutGroup {
    category: string;
    items: WorkflowShortcutItem[];
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
                { type: "coding", icon: "💻", label: localizeText("Coding", "编程开发", "程式開發"), description: localizeText("Full-stack coding workflow", "完整编码开发流程", "完整編碼開發流程") },
                { type: "testing", icon: "🧪", label: localizeText("Testing", "测试", "測試"), description: localizeText("Test strategy & execution", "测试策略与执行", "測試策略與執行") },
                { type: "ops_maintenance", icon: "🔧", label: localizeText("Ops & Maintenance", "运维", "運維"), description: localizeText("Operations maintenance", "运维管理", "運維管理") },
                { type: "maintenance", icon: "🔨", label: localizeText("Refactor", "重构改造", "重構改造"), description: localizeText("Refactoring & migration", "架构重构与技术迁移", "架構重構與技術遷移") },
            ],
        },
        {
            category: localizeText("Research & Academic", "学术研究", "學術研究"),
            items: [
                { type: "literature_review", icon: "📚", label: localizeText("Literature Review", "文献综述", "文獻綜述"), description: localizeText("Academic literature review", "学术文献综述", "學術文獻綜述") },
                { type: "research_report", icon: "📊", label: localizeText("Research Report", "研究报告", "研究報告"), description: localizeText("Research report writing", "研究报告撰写", "研究報告撰寫") },
                { type: "experiment_design", icon: "🔬", label: localizeText("Experiment Design", "实验设计", "實驗設計"), description: localizeText("Experimental methodology", "实验方法设计", "實驗方法設計") },
                { type: "paper_writing", icon: "✍️", label: localizeText("Paper Writing", "论文写作", "論文寫作"), description: localizeText("Academic paper writing", "学术论文撰写", "學術論文撰寫") },
                { type: "paper_reproduction", icon: "🔄", label: localizeText("Paper Reproduction", "论文复现", "論文復現"), description: localizeText("Reproduce paper results", "论文实验复现", "論文實驗復現") },
                { type: "grant_proposal", icon: "📋", label: localizeText("Grant Proposal", "基金申请", "基金申請"), description: localizeText("Grant proposal writing", "科研基金申请书", "科研基金申請書") },
            ],
        },
        {
            category: localizeText("Business & Strategy", "商业战略", "商業戰略"),
            items: [
                { type: "product_design", icon: "🎨", label: localizeText("Product Design", "产品设计", "產品設計"), description: localizeText("Product design workflow", "产品设计流程", "產品設計流程") },
                { type: "business_plan", icon: "📈", label: localizeText("Business Plan", "商业计划", "商業計畫"), description: localizeText("Business plan writing", "商业计划书", "商業計畫書") },
                { type: "competitive_analysis", icon: "🏆", label: localizeText("Competitive Analysis", "竞品分析", "競品分析"), description: localizeText("Competitive analysis", "竞争对手分析", "競爭對手分析") },
                { type: "innovation", icon: "💡", label: localizeText("Innovation", "创新方案", "創新方案"), description: localizeText("Innovation design", "创新方案设计", "創新方案設計") },
                { type: "project_proposal", icon: "📑", label: localizeText("Project Proposal", "项目提案", "專案提案"), description: localizeText("Project proposal", "项目立项提案", "專案立項提案") },
            ],
        },
        {
            category: localizeText("Legal & Compliance", "法律合规", "法律合規"),
            items: [
                { type: "contract_review", icon: "📄", label: localizeText("Contract Review", "合同审查", "合同審查"), description: localizeText("Contract risk review", "合同条款风险审查", "合同條款風險審查") },
                { type: "compliance_audit", icon: "✅", label: localizeText("Compliance Audit", "合规审计", "合規審計"), description: localizeText("Compliance audit", "企业合规审计", "企業合規審計") },
                { type: "patent_analysis", icon: "🔍", label: localizeText("Patent Analysis", "专利分析", "專利分析"), description: localizeText("Patent analysis", "专利检索与分析", "專利檢索與分析") },
                { type: "patent_application", icon: "📝", label: localizeText("Patent Application", "专利申请", "專利申請"), description: localizeText("Patent application", "专利撰写申请", "專利撰寫申請") },
                { type: "us_patent_application", icon: "🇺🇸", label: localizeText("US Patent", "美国专利", "美國專利"), description: localizeText("USPTO patent application", "美国 USPTO 专利申请", "美國 USPTO 專利申請") },
                { type: "due_diligence", icon: "🏢", label: localizeText("Due Diligence", "尽职调查", "盡職調查"), description: localizeText("Due diligence", "投资尽职调查", "投資盡職調查") },
            ],
        },
        {
            category: localizeText("Presentation & Events", "展示与活动", "展示與活動"),
            items: [
                { type: "presentation_design", icon: "📽️", label: localizeText("PPT Design", "PPT 设计", "PPT 設計"), description: localizeText("Presentation design", "演示文稿设计", "簡報設計") },
                { type: "event_planning", icon: "🎯", label: localizeText("Event Planning", "活动策划", "活動策劃"), description: localizeText("Event planning", "活动策划方案", "活動策劃方案") },
                { type: "bid_response", icon: "📦", label: localizeText("Bid Response", "招投标", "招投標"), description: localizeText("Bid response", "招投标文件生成", "招投標文件生成") },
            ],
        },
        {
            category: localizeText("Academic Funding", "学术基金", "學術基金"),
            items: [
                { type: "changjiang_scholar", icon: "🎓", label: localizeText("Changjiang Scholar", "长江学者", "長江學者"), description: localizeText("Changjiang Scholar application", "长江学者申请", "長江學者申請") },
                { type: "changjiang_scholar_review", icon: "📖", label: localizeText("Scholar Review", "长江学者评审", "長江學者評審"), description: localizeText("Changjiang Scholar application review", "长江学者申报书评审", "長江學者申報書評審") },
                { type: "nsfc_distinguished_youth", icon: "🏅", label: localizeText("NSFC Distinguished Youth", "杰青", "傑青"), description: localizeText("NSFC Distinguished Youth Fund", "国家杰出青年基金", "國家傑出青年基金") },
                { type: "nsfc_excellent_youth", icon: "🌟", label: localizeText("NSFC Excellent Youth", "优青", "優青"), description: localizeText("NSFC Excellent Youth Fund", "国家优秀青年基金", "國家優秀青年基金") },
                { type: "nsfc_youth", icon: "🌱", label: localizeText("NSFC Youth", "青年基金", "青年基金"), description: localizeText("NSFC Youth Fund", "国家自然科学基金青年项目", "國家自然科學基金青年項目") },
                { type: "nsfc_general", icon: "📘", label: localizeText("NSFC General", "面上基金", "面上基金"), description: localizeText("NSFC General Program", "国家自然科学基金面上项目", "國家自然科學基金面上項目") },
                { type: "nsfc_key", icon: "🔑", label: localizeText("NSFC Key", "重点基金", "重點基金"), description: localizeText("NSFC Key Program", "国家自然科学基金重点项目", "國家自然科學基金重點項目") },
            ],
        },
        {
            category: localizeText("Education", "教育", "教育"),
            items: [
                { type: "gaokao_application", icon: "🎒", label: localizeText("Gaokao Guidance", "高考志愿", "高考志願"), description: localizeText("College application guidance", "高考志愿填报参考", "高考志願填報參考") },
            ],
        },
    ];
}
