import { useState, useEffect } from "react";
import type React from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import type { ChatMessage } from "./useAIAssistant";
import { AssistantInputComposer } from "./AssistantInputComposer";
import { AssistantPinnedNewsCards } from "./AssistantPinnedNewsCards";
import { getComposeActionPlaceholder, type ComposeAction, type FireSlashCommand, type PlusMenuActionId } from "./composeAction";
import type { AttachmentInfo } from "./useBufferQueue";
import type { UseVoiceInputResult } from "./useVoiceInput";
import type { AssistantPermissionMode } from "./AssistantInputComposerTypes";

// --- Data ---

interface WelcomePrompt {
    text: string;
    textEn: string;
    desc: string;
    descEn: string;
    icon: string;
    template?: string;
    templateEn?: string;
}

interface ScenarioTab {
    id: string;
    label: string;
    labelEn: string;
    prompts: WelcomePrompt[];
}

/** Multi-path professional icons (24×24) for scenario cards — not single-stroke “AI slop” glyphs. */
function WelcomePromptIcon({ name, color }: { name: string; color: string }) {
    const s = { fill: "none", stroke: color, strokeWidth: 1.55, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
    const paths: Record<string, React.ReactNode> = {
        ppt: (<><rect {...s} x="4" y="4" width="16" height="12" rx="1.5" /><path {...s} d="M8 20h8" /><path {...s} d="M12 16v4" /><path {...s} d="M8 8h4v4H8z" /><path {...s} d="M14 9h3M14 12h2" /></>),
        plan: (<><path {...s} d="M7 3h7l4 4v13a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Z" /><path {...s} d="M14 3v4h4" /><path {...s} d="M9 11h7M9 14h7M9 17h4" /></>),
        contract: (<><path {...s} d="M7 3h7l4 4v13a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Z" /><path {...s} d="M14 3v4h4" /><path {...s} d="m9 14 1.5 1.5L14 12" /><path {...s} d="M9 18h6" /></>),
        code: (<><path {...s} d="m8 8-3.5 4L8 16" /><path {...s} d="m16 8 3.5 4L16 16" /><path {...s} d="m13.2 6-2.4 12" /></>),
        bug: (<><path {...s} d="M9 9v5a3 3 0 0 0 6 0V9a3 3 0 0 0-6 0Z" /><path {...s} d="M7 10H4M7 13H4M7 16H5" /><path {...s} d="M17 10h3M17 13h3M17 16h2" /><path {...s} d="m8.5 7.5-1.5-2M15.5 7.5 17 5.5" /><path {...s} d="M12 9v8" /></>),
        docker: (<><path {...s} d="M3 13h18" /><path {...s} d="M6 13V10h2.5v3M10 13V10h2.5v3M14 13V10h2.5v3" /><path {...s} d="M10 10V7h2.5v3M6 10V7h2.5v3" /><path {...s} d="M5 16c1.2 2.8 4.5 4 8.5 3 2.2-.6 3.8-1.8 4.8-3.2" /></>),
        server: (<><rect {...s} x="4" y="4" width="16" height="6" rx="1.2" /><rect {...s} x="4" y="14" width="16" height="6" rx="1.2" /><path {...s} d="M8 7h.01M8 17h.01" /><path {...s} d="M12 7h4M12 17h4" /></>),
        install: (<><path {...s} d="M12 4v10" /><path {...s} d="m8 11 4 3 4-3" /><path {...s} d="M5 18h14" /><path {...s} d="M7 18v2h10v-2" /></>),
        deploy: (<><path {...s} d="M12 4v9" /><path {...s} d="m8 9 4-5 4 5" /><path {...s} d="M6 15h12" /><path {...s} d="m7 15 5 5 5-5" /></>),
        search: (<><circle {...s} cx="11" cy="11" r="6" /><path {...s} d="m16 16 3.5 3.5" /><path {...s} d="M8.5 11h5M11 8.5v5" /></>),
        translate: (<><path {...s} d="M4 5h7M7.5 5v2.5" /><path {...s} d="M5.5 11c1.5 2 3.5 3.5 5.5 4.5" /><path {...s} d="M11 5c-.8 2.5-2.5 4.5-4.5 6" /><path {...s} d="m14 10 3 9M15.2 15.5h4.3" /><path {...s} d="M18 7V5h-3" /></>),
        chart: (<><path {...s} d="M4 19V10M9 19V6M14 19v-7M19 19V8" /><path {...s} d="M3 19h18" /></>),
        award: (<><path {...s} d="m12 3 1.9 3.9 4.3.6-3.1 3 .7 4.3L12 12.8 8.2 14.8l.7-4.3-3.1-3 4.3-.6L12 3Z" /><path {...s} d="M9 16v4l3-1.5L15 20v-4" /></>),
        write: (<><path {...s} d="M5 19h4L19 9l-4-4L5 15v4Z" /><path {...s} d="m13.5 6.5 4 4" /><path {...s} d="M14 19h5" /></>),
        mail: (<><rect {...s} x="3.5" y="5.5" width="17" height="13" rx="1.5" /><path {...s} d="m3.5 7 8.5 6.5L20.5 7" /></>),
        meeting: (<><rect {...s} x="4" y="4" width="16" height="16" rx="1.5" /><path {...s} d="M8 9h8M8 12h8M8 15h5" /></>),
        knowledge: (<><path {...s} d="M6 4h8l4 4v12H6V4Z" /><path {...s} d="M14 4v4h4" /><path {...s} d="M9 12h7M9 15h7M9 18h4" /></>),
        qa: (<><path {...s} d="M6 5h12v8H10l-4 3.5V5Z" /><path {...s} d="M10 9.5h4" /><path {...s} d="M12 9.5v.2a2 2 0 0 1-1.2 1.8V12.2" /><circle {...s} cx="12" cy="14.2" r="0.6" fill={color} stroke="none" /></>),
        checklist: (<><path {...s} d="m5 7 1.5 1.5L9.5 5.5" /><path {...s} d="M12 7h7" /><path {...s} d="m5 12 1.5 1.5L9.5 10.5" /><path {...s} d="M12 12h7" /><path {...s} d="m5 17 1.5 1.5L9.5 15.5" /><path {...s} d="M12 17h7" /></>),
        workflow: (<><rect {...s} x="3.5" y="4" width="6" height="5" rx="1" /><rect {...s} x="14.5" y="15" width="6" height="5" rx="1" /><path {...s} d="M9.5 6.5h2.8a3 3 0 0 1 3 3V15" /><path {...s} d="M14.5 17.5H12a3 3 0 0 1-3-3V9" /></>),
        form: (<><rect {...s} x="5" y="3.5" width="14" height="17" rx="1.5" /><path {...s} d="M8 8h8M8 12h8M8 16h5" /></>),
        schedule: (<><rect {...s} x="4" y="5" width="16" height="15" rx="1.5" /><path {...s} d="M8 3.5v3M16 3.5v3M4 10h16" /><path {...s} d="M8 14h2M12 14h2M8 17h2" /></>),
        strategy: (<><path {...s} d="M4 19h16" /><path {...s} d="M6 19V12l3-3 3 3 5-6" /><path {...s} d="M15 6h3v3" /></>),
        review: (<><path {...s} d="M6 3.5h9l4 4V20a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4.5a1 1 0 0 1 1-1Z" /><path {...s} d="M15 3.5V8h4" /><path {...s} d="M8 12h8M8 15h5" /><path {...s} d="m14 17 1.2 1.2L18 15.5" /></>),
        monitor: (<><path {...s} d="M4 16h3l2-7 2.5 11 2-5h5" /><path {...s} d="M3 19h18" /></>),
        diagram: (<><rect {...s} x="3.5" y="3.5" width="6" height="4.5" rx="1" /><rect {...s} x="14.5" y="3.5" width="6" height="4.5" rx="1" /><rect {...s} x="3.5" y="16" width="6" height="4.5" rx="1" /><rect {...s} x="14.5" y="16" width="6" height="4.5" rx="1" /><path {...s} d="M9.5 5.8h5" /><path {...s} d="M6.5 8v3.5h11V8" /><path {...s} d="M6.5 16v-4.5M17.5 16v-4.5" /></>),
        target: (<><circle {...s} cx="12" cy="12" r="8" /><circle {...s} cx="12" cy="12" r="4.5" /><circle {...s} cx="12" cy="12" r="1.3" fill={color} stroke="none" /></>),
        users: (<><circle {...s} cx="9" cy="8" r="2.5" /><path {...s} d="M3.5 19c.7-3 2.7-4.5 5.5-4.5S14 16 14.5 19" /><circle {...s} cx="17" cy="9" r="2.1" /><path {...s} d="M15.5 14.5c1.8.3 3.2 1.4 3.8 3.5" /></>),
        shield: (<><path {...s} d="M12 3.5 19 6v5c0 4.5-2.9 7.8-7 9.5-4.1-1.7-7-5-7-9.5V6l7-2.5Z" /><path {...s} d="m9 12 2 2 4-4.5" /></>),
        spark: (<><path {...s} d="m12 3 1.2 4.2L17.5 8.5 13.2 9.8 12 14l-1.2-4.2L6.5 8.5l4.3-1.3L12 3Z" /><path {...s} d="m6 15 .7 2.2L9 18l-2.3.6L6 20.8l-.7-2.2L3 18l2.3-.8L6 15Z" /><path {...s} d="m17 15 .7 2.2 2.3.8-2.3.8-.7 2.2-.7-2.2-2.3-.8 2.3-.8.7-2.2Z" /></>),
    };
    return (
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true" style={{ flexShrink: 0, marginTop: 1 }}>
            {paths[name] || paths.plan}
        </svg>
    );
}

const SCENARIO_TABS: ScenarioTab[] = [
    {
        id: "business",
        label: "经营分析",
        labelEn: "Business analysis",
        prompts: [
            { text: "做一份可汇报的竞品分析", textEn: "Prepare an executive competitor brief", desc: "明确结论、证据、差异和行动建议", descEn: "Turn evidence into decisions and next actions", icon: "ppt",
              template: "帮我做一份可汇报的竞品分析\n行业/赛道：[行业]\n我方产品：[产品名称和一句话定位]\n竞品：[竞品 A、竞品 B、竞品 C]\n目标听众：[老板/销售团队/产品团队]\n请输出：1）一页结论摘要；2）功能、价格、渠道、客户口碑对比表；3）我方机会与风险；4）下一步行动建议；5）PPT 大纲。",
              templateEn: "Prepare an executive competitor brief\nIndustry: [industry]\nOur product: [name and one-line positioning]\nCompetitors: [A, B, C]\nAudience: [leadership/sales/product team]\nOutput: 1) one-page executive summary; 2) comparison table for features, pricing, channels, customer sentiment; 3) opportunities and risks; 4) recommended next actions; 5) slide outline." },
            { text: "把零散想法整理成项目方案", textEn: "Turn rough ideas into a project proposal", desc: "从目标、范围、里程碑到风险闭环", descEn: "Shape goals, scope, milestones, and risks", icon: "plan",
              template: "把下面的零散想法整理成一份项目方案\n背景：[为什么要做]\n目标：[希望达成什么]\n已有想法：[粘贴要点]\n约束：[时间/预算/人员/技术限制]\n请先追问缺失信息；信息足够后输出：项目目标、范围边界、关键里程碑、资源需求、风险清单、验收标准。",
              templateEn: "Turn the notes below into a project proposal\nBackground: [why this matters]\nGoals: [desired outcome]\nRaw notes: [paste bullets]\nConstraints: [time/budget/team/technical limits]\nFirst ask for missing information. Once enough context is available, output goals, scope boundaries, milestones, resource needs, risks, and acceptance criteria." },
            { text: "审查合同并列出谈判要点", textEn: "Review a contract and draft negotiation points", desc: "按风险等级给出可执行修改建议", descEn: "Rank risks and propose concrete edits", icon: "contract",
              template: "审查这份合同并列出谈判要点\n合同文件：[拖入文件或粘贴路径]\n我方角色：[甲方/乙方/采购方/服务商]\n重点关注：[付款、交付、违约、知识产权、保密、终止]\n请输出：高/中/低风险条款表、每条风险的原因、建议修改方向、可直接发给对方的谈判话术。",
              templateEn: "Review this contract and draft negotiation points\nContract file: [drag file or paste path]\nOur role: [buyer/vendor/client/service provider]\nFocus: [payment, delivery, liability, IP, confidentiality, termination]\nOutput: high/medium/low risk table, reason for each risk, suggested edit direction, and negotiation wording I can send to the counterparty." },
            { text: "制定一个季度经营复盘", textEn: "Create a quarterly business review", desc: "把指标、原因、风险和动作串成闭环", descEn: "Connect metrics, causes, risks, actions", icon: "strategy",
              template: "帮我制定一个季度经营复盘\n业务范围：[产品线/区域/团队]\n周期：[季度或日期范围]\n关键指标：[收入、利润、订单、转化、留存、成本等]\n已知变化：[增长/下滑/异常]\n请输出：复盘结构、核心结论、指标拆解表、原因假设、风险与机会、下季度重点动作、需要补充的数据清单。",
              templateEn: "Create a quarterly business review\nScope: [product line/region/team]\nPeriod: [quarter or date range]\nKey metrics: [revenue, profit, orders, conversion, retention, cost]\nKnown changes: [growth/decline/anomalies]\nOutput review structure, key conclusions, metric breakdown, cause hypotheses, risks and opportunities, next-quarter actions, and missing data list." },
            { text: "设计一套客户分层策略", textEn: "Design a customer segmentation strategy", desc: "按价值、需求和动作拆分客户", descEn: "Segment by value, needs, and actions", icon: "users",
              template: "帮我设计一套客户分层策略\n业务类型：[业务]\n客户数据：[字段或文件]\n目标：[提升转化/留存/客单价/服务效率]\n可执行动作：[销售跟进、运营触达、权益配置等]\n请输出：分层维度、分层规则、每层客户画像、对应运营动作、指标监控、需要补充的数据。",
              templateEn: "Design a customer segmentation strategy\nBusiness type: [business]\nCustomer data: [fields or file]\nGoal: [conversion/retention/AOV/service efficiency]\nAvailable actions: [sales follow-up, lifecycle campaigns, benefits]\nOutput segmentation dimensions, rules, personas, actions per segment, monitoring metrics, and missing data." },
            { text: "评估一个新业务机会", textEn: "Evaluate a new business opportunity", desc: "从市场、客户、成本和风险判断可行性", descEn: "Assess market, customers, cost, risk", icon: "target",
              template: "帮我评估一个新业务机会\n机会描述：[一句话说明]\n目标客户：[客户群体]\n市场线索：[已有数据或观察]\n资源约束：[团队、预算、时间、渠道]\n请输出：机会判断、目标客户假设、价值主张、商业模式、关键风险、验证实验、两周内可执行动作。",
              templateEn: "Evaluate a new business opportunity\nOpportunity: [one-line description]\nTarget customers: [customer group]\nMarket signals: [data or observations]\nConstraints: [team, budget, time, channel]\nOutput opportunity assessment, customer hypotheses, value proposition, business model, key risks, validation experiments, and actions for the next two weeks." },
            { text: "拆解一个增长目标", textEn: "Break down a growth target", desc: "把目标拆成指标树、杠杆和实验", descEn: "Turn target into metric tree and experiments", icon: "chart",
              template: "帮我拆解一个增长目标\n总目标：[例如月收入增长 20%]\n当前基线：[当前数据]\n周期：[时间范围]\n可动用资源：[渠道、预算、人员]\n请输出：指标树、关键杠杆、优先级排序、实验计划、数据看板、每周复盘节奏。",
              templateEn: "Break down a growth target\nOverall target: [e.g. 20% monthly revenue growth]\nBaseline: [current data]\nPeriod: [time range]\nResources: [channels, budget, people]\nOutput metric tree, key levers, priority ranking, experiment plan, dashboard, and weekly review cadence." },
            { text: "梳理一份商业汇报大纲", textEn: "Create a business presentation outline", desc: "把背景、结论、证据和诉求排清楚", descEn: "Structure context, conclusion, evidence, ask", icon: "ppt",
              template: "帮我梳理一份商业汇报大纲\n汇报主题：[主题]\n汇报对象：[老板/客户/投资人/跨部门]\n核心诉求：[希望对方决策或支持什么]\n已有材料：[粘贴要点或拖入文件]\n请输出：汇报主线、页级大纲、每页核心信息、需要的数据证据、可能被追问的问题。",
              templateEn: "Create a business presentation outline\nTopic: [topic]\nAudience: [leadership/customer/investor/cross-functional]\nMain ask: [decision or support needed]\nExisting material: [paste bullets or attach files]\nOutput narrative, slide-by-slide outline, key message per slide, evidence needed, and likely questions." },
        ],
    },
    {
        id: "dev",
        label: "软件开发",
        labelEn: "Development",
        prompts: [
            { text: "实现一个后台功能闭环", textEn: "Build an admin feature end to end", desc: "从现有代码风格到测试一起完成", descEn: "Follow local patterns and include verification", icon: "code",
              template: "在这个项目里实现一个后台功能闭环\n项目路径：[d:\\your\\project\\path]\n功能目标：[例如用户审核、订单退款、任务调度]\n涉及页面/接口：[已知文件或路由]\n业务规则：[列出规则]\n验收标准：[用户能完成哪些操作]\n请先阅读项目结构和现有组件风格，再给出改动并运行相关检查。",
              templateEn: "Build an admin feature end to end in this project\nProject path: [d:\\your\\project\\path]\nGoal: [e.g. user review, refund flow, task scheduling]\nPages/APIs involved: [known files or routes]\nBusiness rules: [list rules]\nAcceptance criteria: [what the user must be able to do]\nFirst inspect the project structure and existing component style, then make the change and run relevant checks." },
            { text: "定位并修复一个线上问题", textEn: "Diagnose and fix a production issue", desc: "用复现、日志、影响面和回归测试收口", descEn: "Close with repro, logs, blast radius, tests", icon: "bug",
              template: "帮我定位并修复这个线上问题\n项目路径：[d:\\your\\project\\path]\n现象：[用户看到什么]\n复现步骤：[步骤]\n期望结果：[应该发生什么]\n实际结果：[现在发生什么]\n错误日志/截图：[粘贴或拖入]\n请输出：根因判断、修复方案、改动文件、回归测试方式；如果信息不足，先列出需要我补充的最少信息。",
              templateEn: "Diagnose and fix this production issue\nProject path: [d:\\your\\project\\path]\nSymptom: [what users see]\nRepro steps: [steps]\nExpected: [what should happen]\nActual: [what happens now]\nLogs/screenshots: [paste or attach]\nOutput root cause, fix plan, changed files, and regression test method. If context is missing, ask for the minimum needed information first." },
            { text: "为项目补齐部署和环境说明", textEn: "Add deployment and environment docs", desc: "生成可落地的 Docker、配置和启动文档", descEn: "Create usable Docker, config, and run docs", icon: "docker",
              template: "为这个项目补齐部署和环境说明\n项目路径：[d:\\your\\project\\path]\n目标环境：[本地开发/测试服务器/生产]\n运行依赖：[数据库、缓存、队列、对象存储等]\n暴露端口：[端口]\n请检查现有启动方式，补齐 Dockerfile、compose 或部署文档，并说明配置项、启动步骤、健康检查和常见故障排查。",
              templateEn: "Add deployment and environment docs for this project\nProject path: [d:\\your\\project\\path]\nTarget environment: [local/test server/production]\nDependencies: [database, cache, queue, object storage]\nPorts: [ports]\nInspect the existing run flow, then add Dockerfile, compose, or deployment docs with config items, startup steps, health checks, and common troubleshooting." },
            { text: "做一次代码审查和优化", textEn: "Review and optimize code", desc: "优先找缺陷、回归风险和可测改进", descEn: "Find defects, regression risks, testable fixes", icon: "review",
              template: "帮我做一次代码审查和优化\n项目路径：[d:\\your\\project\\path]\n关注范围：[文件/模块/最近改动]\n目标：[稳定性/性能/可维护性/安全]\n约束：[不要大重构/保持 API 兼容/优先小步修改]\n请先列出按严重程度排序的问题，再修复高价值问题，最后说明改动、风险和验证方式。",
              templateEn: "Review and optimize code\nProject path: [d:\\your\\project\\path]\nScope: [files/modules/recent changes]\nGoal: [stability/performance/maintainability/security]\nConstraints: [avoid large refactor/keep API compatibility/small changes first]\nFirst list findings by severity, then fix high-value issues, and finally report changes, risks, and verification." },
            { text: "补齐单元测试和回归用例", textEn: "Add unit and regression tests", desc: "围绕风险路径补可维护测试", descEn: "Cover risky paths with maintainable tests", icon: "checklist",
              template: "帮我为这个改动补齐测试\n项目路径：[d:\\your\\project\\path]\n变更范围：[文件/功能]\n风险点：[边界条件、错误状态、并发、权限等]\n请先阅读现有测试风格，再补单元测试或回归用例，并说明覆盖了哪些风险、还剩哪些测试缺口。",
              templateEn: "Add tests for this change\nProject path: [d:\\your\\project\\path]\nChange scope: [files/feature]\nRisks: [edge cases, errors, concurrency, permissions]\nFirst inspect existing test style, then add unit or regression tests and explain covered risks and remaining gaps." },
            { text: "重构一个复杂组件", textEn: "Refactor a complex component", desc: "拆分职责，保持行为和样式不变", descEn: "Split responsibilities without behavior drift", icon: "diagram",
              template: "帮我重构一个复杂组件\n项目路径：[d:\\your\\project\\path]\n目标组件：[文件路径]\n痛点：[太长/状态混乱/重复逻辑/难测]\n约束：保持现有行为、样式和公开接口不变\n请先梳理职责，再给出小步重构方案并执行，最后运行相关测试。",
              templateEn: "Refactor a complex component\nProject path: [d:\\your\\project\\path]\nTarget component: [file path]\nPain points: [too long/state complexity/duplication/hard to test]\nConstraint: preserve behavior, styles, and public API\nFirst map responsibilities, then refactor in small steps and run relevant tests." },
            { text: "排查前端性能问题", textEn: "Investigate frontend performance issues", desc: "定位慢渲染、重复请求和卡顿来源", descEn: "Find slow renders, duplicate requests, jank", icon: "monitor",
              template: "帮我排查前端性能问题\n项目路径：[d:\\your\\project\\path]\n页面/流程：[路径或操作]\n现象：[首屏慢/交互卡/内存涨/重复请求]\n可用信息：[截图、Performance 记录、日志]\n请输出：排查步骤、可能根因、修复建议、可验证指标，并在可行时直接修复。",
              templateEn: "Investigate frontend performance issues\nProject path: [d:\\your\\project\\path]\nPage/flow: [route or action]\nSymptom: [slow first paint/jank/memory growth/duplicate requests]\nAvailable info: [screenshots, performance trace, logs]\nOutput investigation steps, likely causes, fixes, measurable verification, and implement fixes when feasible." },
            { text: "接入一个第三方 API", textEn: "Integrate a third-party API", desc: "处理鉴权、错误、重试和配置", descEn: "Handle auth, errors, retries, config", icon: "workflow",
              template: "帮我接入一个第三方 API\n项目路径：[d:\\your\\project\\path]\nAPI 文档：[链接或粘贴]\n使用场景：[业务流程]\n鉴权方式：[Key/OAuth/签名/其他]\n请输出：接入方案、配置项、错误处理、重试策略、测试用例，并按项目现有风格实现。",
              templateEn: "Integrate a third-party API\nProject path: [d:\\your\\project\\path]\nAPI docs: [link or paste]\nUse case: [business flow]\nAuth: [key/OAuth/signature/other]\nOutput integration plan, config items, error handling, retry strategy, tests, and implement using local patterns." },
        ],
    },
    {
        id: "ops",
        label: "运维排障",
        labelEn: "Ops troubleshooting",
        prompts: [
            { text: "排查服务器磁盘占满", textEn: "Investigate a full disk incident", desc: "先评估风险，再给清理命令和回滚点", descEn: "Assess risk before cleanup commands", icon: "server",
              template: "帮我排查服务器磁盘占满问题\n连接方式：[已有 SSH 配置/堡垒机/本机日志]\n系统类型：[Linux 发行版和版本]\n症状：[磁盘告警、服务异常、路径占用]\n限制：[不能停服务/只能读操作/可维护窗口]\n请先给只读诊断命令；确认后再给清理方案，并标注每一步风险、预计释放空间、可回滚方式。",
              templateEn: "Investigate a full disk incident\nAccess: [existing SSH config/bastion/local logs]\nOS: [Linux distro and version]\nSymptoms: [disk alert, service failure, occupied path]\nConstraints: [no downtime/read-only first/maintenance window]\nStart with read-only diagnostic commands. After confirmation, provide cleanup steps with risk, expected space recovered, and rollback method." },
            { text: "分析服务启动失败日志", textEn: "Analyze why a service fails to start", desc: "从日志归因到最小修复步骤", descEn: "Map logs to root cause and minimal fix", icon: "install",
              template: "分析这个服务为什么启动失败\n服务名称：[服务名]\n部署方式：[systemd/Docker/Kubernetes/手动脚本]\n最近变更：[发布、配置、证书、依赖升级]\n日志：[粘贴关键日志]\n请输出：可能根因排序、验证命令、最小修复步骤、需要避免的高风险操作。",
              templateEn: "Analyze why this service fails to start\nService: [name]\nDeployment: [systemd/Docker/Kubernetes/manual script]\nRecent changes: [release, config, cert, dependency upgrade]\nLogs: [paste key logs]\nOutput ranked root causes, validation commands, minimal fix steps, and risky operations to avoid." },
            { text: "按文档批量部署并生成检查清单", textEn: "Deploy from docs and create a checklist", desc: "把部署步骤拆成可确认的执行单", descEn: "Turn docs into an auditable rollout checklist", icon: "deploy",
              template: "根据部署文档生成批量部署执行单\n部署文档：[拖入文件或粘贴路径]\n目标环境：[测试/预生产/生产]\n发布窗口：[时间]\n回滚要求：[如何回滚]\n请先解析文档，输出部署前检查、逐台执行步骤、验证命令、失败处理、回滚步骤；涉及凭据时只提示使用安全凭据管理，不要在提示词中保存明文密码。",
              templateEn: "Create a batch deployment runbook from the deployment docs\nDocs: [drag file or paste path]\nEnvironment: [test/staging/production]\nRelease window: [time]\nRollback requirement: [how to roll back]\nParse the docs and output preflight checks, per-host execution steps, validation commands, failure handling, and rollback. For credentials, instruct use of secure credential management instead of storing plaintext secrets in the prompt." },
            { text: "设计服务监控和告警规则", textEn: "Design service monitoring and alerts", desc: "定义指标、阈值、通知和升级路径", descEn: "Define metrics, thresholds, alerts, escalation", icon: "monitor",
              template: "帮我设计服务监控和告警规则\n服务名称：[服务名]\n部署方式：[systemd/Docker/Kubernetes/云服务]\n关键依赖：[数据库、缓存、队列、第三方接口]\n业务影响：[哪些用户或流程会受影响]\n请输出：监控指标、告警阈值、分级规则、通知文案、排查入口、升级路径、误报降噪建议。",
              templateEn: "Design service monitoring and alerts\nService: [name]\nDeployment: [systemd/Docker/Kubernetes/cloud service]\nKey dependencies: [database, cache, queue, third-party API]\nBusiness impact: [affected users or workflows]\nOutput metrics, alert thresholds, severity levels, notification copy, troubleshooting entry points, escalation path, and noise-reduction suggestions." },
            { text: "排查接口超时或 5xx", textEn: "Investigate API timeout or 5xx errors", desc: "从链路、依赖、日志定位故障", descEn: "Trace service path, dependencies, logs", icon: "bug",
              template: "帮我排查接口超时或 5xx 问题\n接口路径：[路径]\n时间范围：[发生时间]\n部署环境：[测试/预生产/生产]\n相关日志：[粘贴日志或路径]\n最近变更：[发布、配置、依赖]\n请输出：排查顺序、关键查询命令、可能根因、临时止血方案、长期修复建议。",
              templateEn: "Investigate API timeout or 5xx errors\nEndpoint: [path]\nTime window: [when it happened]\nEnvironment: [test/staging/production]\nLogs: [paste logs or path]\nRecent changes: [release, config, dependency]\nOutput investigation order, key commands/queries, likely causes, mitigation, and long-term fixes." },
            { text: "制定备份和恢复演练", textEn: "Create a backup and restore drill", desc: "定义 RPO/RTO、验证步骤和责任人", descEn: "Define RPO/RTO, validation, owners", icon: "shield",
              template: "帮我制定备份和恢复演练方案\n系统/数据：[对象]\n当前备份方式：[如有]\n可接受恢复点 RPO：[时间]\n可接受恢复时间 RTO：[时间]\n限制：[不能停机/只能演练测试环境]\n请输出：备份检查清单、恢复演练步骤、验证方法、风险点、责任分工、演练记录模板。",
              templateEn: "Create a backup and restore drill\nSystem/data: [target]\nCurrent backup method: [if any]\nRPO: [time]\nRTO: [time]\nConstraints: [no downtime/test env only]\nOutput backup checklist, restore drill steps, validation method, risks, ownership, and drill record template." },
            { text: "梳理发布回滚预案", textEn: "Prepare a release rollback plan", desc: "明确发布步骤、验证点和回退条件", descEn: "Define rollout, checks, rollback triggers", icon: "deploy",
              template: "帮我梳理发布回滚预案\n发布内容：[版本/功能]\n影响范围：[服务/用户/数据]\n发布时间：[窗口]\n回滚方式：[镜像/配置/数据库/开关]\n请输出：发布前检查、发布步骤、验证点、暂停条件、回滚步骤、通知话术、责任人分工。",
              templateEn: "Prepare a release rollback plan\nRelease content: [version/feature]\nImpact scope: [services/users/data]\nRelease window: [time]\nRollback method: [image/config/database/feature flag]\nOutput preflight checks, rollout steps, validation points, stop conditions, rollback steps, notification copy, and ownership." },
            { text: "制定安全加固检查清单", textEn: "Create a security hardening checklist", desc: "覆盖账号、端口、权限、日志和备份", descEn: "Cover accounts, ports, permissions, logs, backups", icon: "shield",
              template: "帮我制定安全加固检查清单\n系统类型：[Linux/Windows/数据库/应用服务]\n部署环境：[内网/公网/云服务器]\n合规要求：[如有]\n当前已知风险：[如有]\n请输出：检查项、风险等级、验证命令、加固建议、回滚注意事项、需要人工确认的项目。",
              templateEn: "Create a security hardening checklist\nSystem type: [Linux/Windows/database/application]\nEnvironment: [intranet/public/cloud]\nCompliance requirements: [if any]\nKnown risks: [if any]\nOutput checks, risk level, validation commands, hardening suggestions, rollback notes, and items needing human confirmation." },
        ],
    },
    {
        id: "research",
        label: "科研资料",
        labelEn: "Research",
        prompts: [
            { text: "做一份带出处的资料综述", textEn: "Create a sourced research brief", desc: "区分事实、观点、推断和未验证信息", descEn: "Separate facts, opinions, inferences, unknowns", icon: "search",
              template: "围绕这个主题做一份带出处的资料综述\n主题：[研究主题]\n范围：[学科/行业/国家/时间段/对象]\n用途：[选题判断/申报准备/汇报/PPT/文章]\n已有资料：[粘贴或拖入文件，可为空]\n请输出：核心结论、关键证据表、不同观点对比、未验证信息、可继续追问的问题；每条重要结论都标注来源，缺少来源时明确写“需补证据”。",
              templateEn: "Create a sourced research brief\nTopic: [research topic]\nScope: [discipline/industry/country/time range/object]\nUse case: [topic selection/application preparation/report/slides/article]\nExisting materials: [paste or attach files, optional]\nOutput key conclusions, evidence table, competing viewpoints, unverified items, and follow-up questions. Cite sources for important conclusions; mark unsupported claims as evidence needed." },
            { text: "翻译并润色专业文档", textEn: "Translate and polish a technical document", desc: "保留术语、结构、表格和语气", descEn: "Preserve terminology, structure, tables, tone", icon: "translate",
              template: "翻译并润色这份专业文档\n文档文件：[拖入文件或粘贴路径]\n目标语言：[中文/英文]\n读者：[技术团队/客户/管理层]\n术语要求：[保留英文术语/给出中英对照]\n请先识别文档结构和关键术语，再输出翻译稿、术语表、需要人工确认的歧义点。",
              templateEn: "Translate and polish this technical document\nDocument file: [drag file or paste path]\nTarget language: [Chinese/English]\nAudience: [engineering team/customer/leadership]\nTerminology: [keep English terms/provide bilingual glossary]\nFirst identify structure and key terms, then output translation, glossary, and ambiguities that need human confirmation." },
            { text: "整理实验数据分析报告", textEn: "Create an experiment data analysis report", desc: "梳理分组、统计检验、图表和结论", descEn: "Organize groups, stats, charts, findings", icon: "chart",
              template: "把这批实验数据整理成分析报告\n数据文件：[拖入 CSV/Excel/JSON 或粘贴路径]\n实验设计：[分组、样本量、干预或变量]\n关键指标：[主要指标和次要指标]\n统计方法：[如 t 检验/方差分析/回归/不确定]\n请输出：数据质量检查、指标口径、统计分析思路、主要发现、异常点、建议图表、结论表述；不确定的统计方法请先说明可选方案和适用条件。",
              templateEn: "Create an experiment data analysis report\nData file: [drag CSV/Excel/JSON or paste path]\nExperiment design: [groups, sample size, intervention or variables]\nKey metrics: [primary and secondary metrics]\nStatistical method: [t-test/ANOVA/regression/unknown]\nOutput data quality checks, metric definitions, analysis approach, findings, anomalies, chart recommendations, and conclusion wording. If the statistical method is unclear, explain options and applicability first." },
            { text: "梳理论文选题和创新点", textEn: "Shape a paper topic and novelty claims", desc: "把问题、方法、贡献和实验对齐", descEn: "Align problem, method, contribution, experiments", icon: "write",
              template: "帮我梳理论文选题和创新点\n研究方向：[方向]\n已有想法：[粘贴要点]\n已有基础：[数据、方法、实验、论文]\n目标期刊/会议：[如有]\n请输出：候选题目、核心科学问题、创新点、方法路线、实验设计、潜在质疑、需要补充的文献和证据；不要夸大现有贡献。",
              templateEn: "Shape a paper topic and novelty claims\nResearch direction: [direction]\nCurrent ideas: [paste bullets]\nExisting foundation: [data, method, experiment, papers]\nTarget venue: [optional]\nOutput candidate titles, core research question, novelty claims, method route, experiment design, likely objections, and literature/evidence gaps. Do not overstate contributions." },
            { text: "做一份文献精读笔记", textEn: "Create a close-reading note for a paper", desc: "提炼问题、方法、证据和局限", descEn: "Extract problem, method, evidence, limits", icon: "knowledge",
              template: "帮我精读这篇论文\n论文文件或链接：[拖入 PDF/粘贴链接]\n我的研究方向：[方向]\n关注重点：[方法/实验/理论/写作]\n请输出：论文一句话贡献、问题背景、方法拆解、关键证据、局限性、可借鉴点、与我方向的关联、可追问问题。",
              templateEn: "Create a close-reading note for this paper\nPaper file or link: [attach PDF/paste link]\nMy research direction: [direction]\nFocus: [method/experiment/theory/writing]\nOutput one-line contribution, problem context, method breakdown, key evidence, limitations, useful patterns, relevance to my work, and follow-up questions." },
            { text: "整理开题报告框架", textEn: "Structure a thesis proposal", desc: "把背景、问题、路线和计划串起来", descEn: "Connect background, problem, route, plan", icon: "plan",
              template: "帮我整理开题报告框架\n研究方向：[方向]\n拟定题目：[题目]\n已有基础：[文献、数据、方法、实验]\n导师要求：[如有]\n请输出：开题报告目录、研究背景、核心问题、研究内容、技术路线、创新点、进度计划、风险与备选方案。",
              templateEn: "Structure a thesis proposal\nResearch direction: [direction]\nDraft title: [title]\nExisting foundation: [literature, data, method, experiments]\nAdvisor requirements: [if any]\nOutput proposal outline, background, core question, research content, technical route, novelty, schedule, risks and alternatives." },
            { text: "生成投稿审稿回复", textEn: "Draft a reviewer response letter", desc: "逐条回应意见，保持克制有证据", descEn: "Respond point-by-point with evidence", icon: "mail",
              template: "帮我生成投稿审稿回复\n审稿意见：[粘贴意见]\n论文修改说明：[已改内容]\n目标期刊/会议：[如有]\n语气要求：礼貌、克制、逐条回应、有证据\n请输出：总体回复、逐条回复表、需要补实验或补引用的位置、措辞风险提醒。",
              templateEn: "Draft a reviewer response letter\nReviewer comments: [paste comments]\nRevision notes: [what changed]\nTarget venue: [optional]\nTone: polite, measured, point-by-point, evidence-based\nOutput general response, response table, places needing experiments or citations, and wording risks." },
            { text: "规划科研项目路线图", textEn: "Plan a research project roadmap", desc: "拆分问题、任务、里程碑和产出", descEn: "Split questions, tasks, milestones, outputs", icon: "schedule",
              template: "帮我规划一个科研项目路线图\n项目主题：[主题]\n周期：[时间范围]\n现有基础：[人员、数据、设备、前期成果]\n预期产出：[论文/专利/系统/报告]\n请输出：研究任务分解、里程碑、关键风险、资源需求、产出节奏、每阶段验收标准。",
              templateEn: "Plan a research project roadmap\nProject topic: [topic]\nPeriod: [time range]\nExisting foundation: [people, data, equipment, prior work]\nExpected outputs: [papers/patents/system/report]\nOutput task breakdown, milestones, key risks, resource needs, output cadence, and acceptance criteria per phase." },
        ],
    },
    {
        id: "academic-application",
        label: "科研申报",
        labelEn: "Academic applications",
        prompts: [
            { text: "国家优青申报材料打磨", textEn: "Polish an NSFC Excellent Young Scientists application", desc: "突出青年潜力、独立贡献和成长轨迹", descEn: "Highlight potential, independence, growth trajectory", icon: "award",
              template: "帮我打磨国家优青申报材料\n申请学科/代码：[学科方向]\n拟定题目：[题目或关键词]\n现有申报材料：[粘贴正文或拖入文件]\n代表性成果：[论文、项目、方法、数据、平台、应用等]\n独立贡献：[本人主导或关键贡献]\n未来三到五年计划：[研究方向、关键突破、团队建设]\n请以评审专家视角审阅，不编造论文、基金、奖项、引用或数据。先判断“青年潜力-独立贡献-研究积累-未来突破”是否清晰，再输出：申报主线、个人成长轨迹凝练、代表成果排序建议、独立贡献表述、创新点改写、研究计划框架、证据缺口、评审可能质疑点和回应口径。",
              templateEn: "Polish an NSFC Excellent Young Scientists application\nDiscipline/code: [field]\nDraft title: [title or keywords]\nCurrent application materials: [paste text or attach file]\nRepresentative achievements: [papers, projects, methods, data, platforms, applications]\nIndependent contribution: [work led by the applicant or key contribution]\nThree-to-five-year plan: [research direction, key breakthroughs, team building]\nReview from an evaluator's perspective. Do not invent papers, grants, awards, citations, or data. First judge whether potential, independent contribution, research foundation, and future breakthrough form a clear story. Then output application narrative, refined growth trajectory, achievement ranking, independent-contribution wording, rewritten novelty statements, research-plan outline, evidence gaps, likely reviewer concerns, and response wording." },
            { text: "国家杰青申报书打磨", textEn: "Polish an NSFC Distinguished Young Scholars proposal", desc: "突出科学问题、原创积累和五年计划", descEn: "Focus scientific question, originality, five-year plan", icon: "award",
              template: "帮我打磨国家杰青申报书\n申请学科/代码：[学科方向]\n拟定题目：[题目或关键词]\n现有申报书：[粘贴正文或拖入文件]\n前期基础：[代表论文、项目、方法、数据、平台]\n核心科学问题：[如果已有请粘贴]\n未来五年目标：[方向、关键突破、团队建设]\n请以评审专家视角审阅，不编造论文、基金、奖项、引用或数据。先判断“科学问题-前期积累-创新突破-五年计划”是否成一条主线，再输出：申报书主线、科学问题凝练、代表成果与主线对应表、创新点表述、五年研究计划、年度里程碑、风险与替代方案、证据缺口、评审可能关注的薄弱点。",
              templateEn: "Polish an NSFC Distinguished Young Scholars proposal\nDiscipline/code: [field]\nDraft title: [title or keywords]\nCurrent proposal draft: [paste text or attach file]\nPrior foundation: [papers, projects, methods, data, platforms]\nCore scientific question: [paste if available]\nFive-year goals: [direction, key breakthroughs, team building]\nReview from an evaluator's perspective. Do not invent papers, grants, awards, citations, or data. First judge whether scientific question, prior foundation, originality, and five-year plan form one narrative. Then output proposal narrative, refined scientific question, achievement-to-narrative mapping, originality statements, five-year plan, yearly milestones, risks and alternatives, evidence gaps, and likely reviewer concerns." },
            { text: "长江学者申报书打磨", textEn: "Polish a Changjiang Scholar application", desc: "凝练学术贡献、团队建设和学科影响", descEn: "Clarify contribution, team building, discipline impact", icon: "award",
              template: "帮我打磨长江学者申报书\n申报类型：[特聘教授/讲座教授/青年学者等]\n学科方向：[一级学科/研究方向]\n现有申报书：[粘贴正文或拖入文件]\n代表性成果：[论文/项目/奖项/成果转化/平台建设]\n目标单位与岗位定位：[如有]\n请以评审专家视角审阅，不编造任何成果、头衔、数据或来源。先诊断：学术贡献是否聚焦、创新性是否说透、国内外影响是否有证据、团队与平台建设是否支撑岗位定位、未来计划是否可落地。再输出：申报书结构建议、个人简介改写、3-5 条主要贡献凝练、研究计划框架、证据缺口清单、可能被质疑的问题和回应口径。",
              templateEn: "Polish a Changjiang Scholar application\nApplication type: [Distinguished Professor/Chair Professor/Young Scholar/etc.]\nDiscipline: [field/research direction]\nCurrent application draft: [paste text or attach file]\nRepresentative achievements: [papers/projects/awards/translation/platform building]\nTarget institution and role positioning: [if any]\nReview from an evaluator's perspective. Do not invent achievements, titles, data, or sources. First diagnose focus of academic contribution, originality, evidence for impact, fit between team/platform building and role positioning, and feasibility of future plan. Then output application structure, rewritten bio, 3-5 refined contributions, research-plan outline, evidence gaps, likely reviewer concerns, and response wording." },
            { text: "基金申请书预审", textEn: "Pre-review a grant proposal", desc: "按评审要点找短板、重写摘要和创新点", descEn: "Find weaknesses, rewrite summary and novelty", icon: "award",
              template: "帮我预审这份基金申请书\n项目类型：[面上/青年/重点/地区/其他]\n申请学科/代码：[学科方向]\n现有申请书：[粘贴正文或拖入文件]\n研究基础：[代表成果和已有条件]\n请按评审标准检查：立项依据、科学问题、创新性、研究内容、技术路线、可行性、年度计划、预期成果。输出：总体评价、主要扣分点、必须补证据的位置、摘要改写、创新点改写、技术路线优化建议、评审意见模拟。",
              templateEn: "Pre-review a grant proposal\nGrant type: [General/Young/Key/Regional/other]\nDiscipline/code: [field]\nCurrent proposal: [paste text or attach file]\nResearch foundation: [representative achievements and existing conditions]\nEvaluate by reviewer criteria: rationale, scientific question, originality, research content, technical route, feasibility, yearly plan, expected outcomes. Output overall assessment, major weaknesses, evidence gaps, rewritten abstract, rewritten novelty statements, technical-route improvements, and simulated reviewer comments." },
            { text: "重点研发项目申报书打磨", textEn: "Polish a key R&D project application", desc: "梳理任务分解、考核指标和组织实施", descEn: "Clarify tasks, KPIs, organization", icon: "award",
              template: "帮我打磨重点研发项目申报书\n项目方向：[方向]\n申报指南要求：[粘贴指南]\n现有申报书：[粘贴正文或拖入文件]\n牵头单位与参与单位：[单位列表]\n请输出：指南匹配度诊断、项目总体目标、任务分解、技术路线、考核指标、组织实施方案、风险与备选方案、材料薄弱点。",
              templateEn: "Polish a key R&D project application\nProject direction: [direction]\nCall requirements: [paste call text]\nCurrent application: [paste text or attach file]\nLead and partners: [institutions]\nOutput call-fit diagnosis, overall objectives, task breakdown, technical route, KPIs, organization plan, risks and alternatives, and weak material sections." },
            { text: "人才项目个人陈述打磨", textEn: "Polish a talent-program personal statement", desc: "突出定位、贡献、潜力和单位匹配", descEn: "Clarify positioning, contribution, potential, fit", icon: "write",
              template: "帮我打磨人才项目个人陈述\n项目类型：[优青/杰青/长江/海外优青/其他]\n个人经历：[粘贴简历或简介]\n代表成果：[列表]\n目标单位/平台：[如有]\n请输出：个人定位、成长主线、核心贡献、平台匹配度、个人陈述改写版、需要补证据的位置；不要编造成果。",
              templateEn: "Polish a talent-program personal statement\nProgram type: [Excellent Young/Distinguished Young/Changjiang/Overseas Young/other]\nBackground: [paste CV or bio]\nRepresentative achievements: [list]\nTarget institution/platform: [if any]\nOutput positioning, growth narrative, key contributions, platform fit, rewritten statement, and evidence gaps. Do not invent achievements." },
            { text: "申报书摘要和立项依据改写", textEn: "Rewrite proposal abstract and rationale", desc: "压实问题价值、创新点和可行性", descEn: "Sharpen value, novelty, feasibility", icon: "review",
              template: "帮我改写申报书摘要和立项依据\n项目类型：[基金/人才/重点研发/校内项目]\n现有摘要和立项依据：[粘贴内容]\n研究基础：[代表成果和条件]\n请输出：问题诊断、摘要改写版、立项依据结构、创新点表述、可行性支撑、需要补引用或数据的位置。",
              templateEn: "Rewrite proposal abstract and rationale\nProposal type: [grant/talent/key R&D/internal]\nCurrent abstract and rationale: [paste text]\nResearch foundation: [achievements and conditions]\nOutput diagnosis, rewritten abstract, rationale structure, novelty wording, feasibility support, and places needing citations or data." },
            { text: "申报材料证据链检查", textEn: "Check evidence chain in application materials", desc: "核对主张、成果、数据和附件是否闭环", descEn: "Check claims, achievements, data, appendices", icon: "checklist",
              template: "帮我检查申报材料证据链\n申报材料：[粘贴正文或拖入文件]\n附件目录：[如有]\n重点主张：[如学术影响、原创贡献、团队基础]\n请输出：主张-证据对应表、缺证据项、表述过强项、附件补充建议、提交前检查清单。",
              templateEn: "Check evidence chain in application materials\nApplication materials: [paste text or attach file]\nAppendix list: [if any]\nKey claims: [impact, originality, team foundation]\nOutput claim-evidence mapping, missing evidence, overclaimed wording, appendix suggestions, and pre-submission checklist." },
        ],
    },
    {
        id: "writing",
        label: "写作沟通",
        labelEn: "Writing",
        prompts: [
            { text: "把要点写成正式汇报", textEn: "Turn bullets into an executive update", desc: "压缩信息、突出决策和待办", descEn: "Compress points into decisions and asks", icon: "write",
              template: "把下面这些要点写成一份正式汇报\n原始要点：[粘贴内容]\n汇报对象：[老板/客户/项目组]\n语气：[专业简洁/坚定/委婉]\n长度：[一页以内/邮件正文/会议发言稿]\n请输出：标题、核心结论、进展、风险、需要对方决策或支持的事项；避免空话。",
              templateEn: "Turn these bullets into an executive update\nRaw bullets: [paste content]\nAudience: [leadership/client/project team]\nTone: [concise/professional/firm/diplomatic]\nLength: [one page/email body/talking points]\nOutput title, key conclusion, progress, risks, and decisions or support needed. Avoid filler." },
            { text: "起草一封客户沟通邮件", textEn: "Draft a client email", desc: "把背景、立场、下一步说清楚", descEn: "Clarify context, position, and next step", icon: "mail",
              template: "帮我起草一封客户沟通邮件\n客户背景：[客户是谁/合作阶段]\n沟通目的：[通知/解释/催办/争取确认/道歉]\n关键事实：[事实要点]\n希望客户做什么：[下一步动作]\n语气要求：[礼貌但明确/稳妥/强硬]\n请输出邮件主题、正文、可选的更短版本，并标出可能引发误解的表述。",
              templateEn: "Draft a client email\nClient context: [who they are/current stage]\nPurpose: [inform/explain/follow up/request confirmation/apologize]\nKey facts: [facts]\nDesired client action: [next step]\nTone: [polite but clear/careful/firm]\nOutput subject, body, shorter variant, and phrases that may cause misunderstanding." },
            { text: "整理会议纪要和行动项", textEn: "Create meeting notes and action items", desc: "把讨论沉淀成责任人和截止时间", descEn: "Convert discussion into owners and deadlines", icon: "meeting",
              template: "把这段会议记录整理成会议纪要\n会议记录：[粘贴录音转写或要点]\n会议主题：[主题]\n参会人：[名单]\n请输出：会议结论、关键讨论点、行动项表格（事项/责任人/截止时间/依赖）、待确认问题、适合发到群里的精简版。",
              templateEn: "Create meeting notes from this transcript\nTranscript/notes: [paste content]\nTopic: [topic]\nParticipants: [names]\nOutput decisions, key discussion points, action-item table (task/owner/due date/dependencies), open questions, and a concise version suitable for group chat." },
            { text: "改写一段内容提升说服力", textEn: "Rewrite copy to be more persuasive", desc: "保留事实，优化结构、语气和重点", descEn: "Keep facts, improve structure and emphasis", icon: "write",
              template: "帮我改写这段内容，提升说服力\n原文：[粘贴内容]\n目标读者：[客户/老板/评审/同事]\n目的：[争取支持/解释风险/推动行动/澄清误解]\n语气：[专业/克制/有力度/友好]\n请输出：改写版、关键改动说明、可选的更短版本、可能引发误解的句子。",
              templateEn: "Rewrite this copy to be more persuasive\nOriginal text: [paste content]\nAudience: [customer/leadership/reviewer/colleague]\nGoal: [gain support/explain risk/drive action/clarify misunderstanding]\nTone: [professional/measured/firm/friendly]\nOutput rewritten version, key edit rationale, shorter variant, and phrases that may be misunderstood." },
            { text: "写一份项目周报", textEn: "Write a project weekly update", desc: "清楚呈现进展、风险和下周计划", descEn: "Show progress, risks, next-week plan", icon: "schedule",
              template: "帮我写一份项目周报\n项目名称：[名称]\n本周进展：[要点]\n风险/阻塞：[要点]\n下周计划：[要点]\n汇报对象：[老板/客户/团队]\n请输出：简洁周报、风险说明、需要支持的事项、适合发群里的短版。",
              templateEn: "Write a project weekly update\nProject: [name]\nThis week's progress: [bullets]\nRisks/blockers: [bullets]\nNext week plan: [bullets]\nAudience: [leadership/customer/team]\nOutput concise weekly update, risk explanation, support needed, and short group-chat version." },
            { text: "准备一次发言稿", textEn: "Prepare talking points", desc: "按场合组织开场、要点和收束", descEn: "Structure opening, points, closing", icon: "meeting",
              template: "帮我准备一次发言稿\n场合：[会议/汇报/答辩/客户沟通]\n听众：[对象]\n时长：[分钟]\n核心信息：[希望对方记住什么]\n请输出：开场、三到五个要点、过渡句、结尾、可能被问到的问题。",
              templateEn: "Prepare talking points\nOccasion: [meeting/report/defense/customer conversation]\nAudience: [audience]\nLength: [minutes]\nCore message: [what they should remember]\nOutput opening, 3-5 points, transitions, closing, and likely questions." },
            { text: "润色一份中文材料", textEn: "Polish a Chinese document", desc: "提升逻辑、语气、标题和段落衔接", descEn: "Improve logic, tone, headings, flow", icon: "write",
              template: "帮我润色这份中文材料\n材料内容：[粘贴正文或拖入文件]\n用途：[汇报/申报/公众号/通知/方案]\n目标读者：[对象]\n语气：[正式/专业/亲和/克制]\n请输出：润色版、结构调整建议、标题建议、删减建议、仍需补充的信息。",
              templateEn: "Polish this Chinese document\nContent: [paste text or attach file]\nUse case: [report/application/article/notice/proposal]\nAudience: [audience]\nTone: [formal/professional/friendly/measured]\nOutput polished version, structure suggestions, title options, cuts, and missing information." },
            { text: "生成多版本表达", textEn: "Generate multiple wording variants", desc: "同一意思按不同对象和语气输出", descEn: "Adapt same message to audiences and tones", icon: "spark",
              template: "帮我把这段意思生成多个表达版本\n原始意思：[粘贴内容]\n使用场景：[邮件/微信/汇报/PPT]\n对象：[客户/领导/同事/评审]\n请输出：正式版、简短版、委婉版、有力度版、口语版，并说明各自适用场景。",
              templateEn: "Generate multiple wording variants\nOriginal meaning: [paste content]\nScenario: [email/chat/report/slides]\nAudience: [customer/leadership/colleague/reviewer]\nOutput formal, short, diplomatic, firm, and conversational versions, with usage notes." },
        ],
    },
    {
        id: "knowledge",
        label: "知识文档",
        labelEn: "Knowledge docs",
        prompts: [
            { text: "把项目资料整理成知识库", textEn: "Organize project materials into a knowledge base", desc: "形成目录、摘要、标签和缺口", descEn: "Create structure, summaries, tags, gaps", icon: "knowledge",
              template: "把这些项目资料整理成知识库结构\n资料位置：[文件夹/文档路径/粘贴内容]\n目标读者：[新人/实施团队/客服/管理层]\n用途：[培训/检索/交接/对外说明]\n请输出：知识库目录、每篇文档摘要、推荐标签、重复或冲突内容、缺失资料清单、优先补齐顺序。",
              templateEn: "Organize these project materials into a knowledge-base structure\nMaterial location: [folder/doc path/pasted content]\nAudience: [new hire/implementation/support/leadership]\nUse case: [training/search/handoff/external explanation]\nOutput knowledge-base outline, summary for each article, tags, duplicate/conflicting content, missing documents, and priority order." },
            { text: "生成产品 FAQ 和标准回答", textEn: "Create product FAQ and standard answers", desc: "覆盖用户问题、边界和升级路径", descEn: "Cover user questions, limits, escalation", icon: "qa",
              template: "基于这些资料生成产品 FAQ 和标准回答\n产品资料：[粘贴或拖入文档]\n目标用户：[客户/销售/客服/内部团队]\n重点场景：[购买前咨询/故障排查/使用指导]\n请输出：问题分类、FAQ 表格、标准回答、不能承诺的边界、需要转人工或升级处理的条件。",
              templateEn: "Create product FAQ and standard answers from these materials\nProduct materials: [paste or attach docs]\nTarget users: [customers/sales/support/internal team]\nScenarios: [pre-sales/troubleshooting/how-to]\nOutput question categories, FAQ table, standard answers, promises to avoid, and conditions for human escalation." },
            { text: "把操作流程写成 SOP", textEn: "Turn a process into an SOP", desc: "步骤、检查点、异常处理一次补齐", descEn: "Add steps, checks, and exception handling", icon: "checklist",
              template: "把这个操作流程写成 SOP\n流程名称：[名称]\n适用对象：[谁来执行]\n原始步骤：[粘贴当前流程]\n风险点：[已知风险]\n请输出：适用范围、前置条件、逐步操作、检查点、异常处理、完成标准、版本记录；步骤要可执行，不要只写原则。",
              templateEn: "Turn this process into an SOP\nProcess name: [name]\nOperator: [who performs it]\nRaw steps: [paste current process]\nKnown risks: [risks]\nOutput scope, prerequisites, step-by-step instructions, checkpoints, exception handling, completion criteria, and version notes. Steps must be executable, not just principles." },
            { text: "整理新人上手指南", textEn: "Create an onboarding guide", desc: "把背景、环境、路径和常见坑讲清楚", descEn: "Clarify context, setup, path, pitfalls", icon: "knowledge",
              template: "帮我整理一份新人上手指南\n资料来源：[文档/仓库/流程/粘贴内容]\n新人角色：[研发/实施/客服/运营/研究助理]\n目标：[几天内能独立完成什么]\n请输出：背景介绍、必读资料、环境准备、第一周任务路径、常见问题、术语表、需要导师确认的节点。",
              templateEn: "Create an onboarding guide\nSources: [docs/repo/process/pasted content]\nNewcomer role: [engineer/implementation/support/ops/research assistant]\nGoal: [what they should handle independently and by when]\nOutput background, required reading, setup steps, first-week task path, FAQ, glossary, and mentor checkpoints." },
            { text: "整理文档目录和命名规范", textEn: "Create document taxonomy and naming rules", desc: "让资料可检索、可归档、可维护", descEn: "Make docs searchable and maintainable", icon: "form",
              template: "帮我整理文档目录和命名规范\n资料类型：[项目/产品/科研/运维/客户]\n现有目录：[粘贴目录或说明]\n使用者：[团队角色]\n请输出：目录结构、命名规则、标签规则、归档规则、示例文件名、迁移建议。",
              templateEn: "Create document taxonomy and naming rules\nMaterial type: [project/product/research/ops/customer]\nCurrent folders: [paste tree or describe]\nUsers: [team roles]\nOutput folder structure, naming rules, tags, archiving rules, example filenames, and migration suggestions." },
            { text: "提取资料中的关键信息", textEn: "Extract key information from materials", desc: "把长文档转成结构化信息表", descEn: "Turn long docs into structured tables", icon: "search",
              template: "帮我从这些资料中提取关键信息\n资料：[粘贴或拖入文件]\n提取目标：[合同条款/客户需求/实验参数/项目任务]\n字段要求：[字段列表]\n请输出：结构化表格、原文依据、缺失字段、歧义点、需要人工确认的内容。",
              templateEn: "Extract key information from these materials\nMaterials: [paste or attach files]\nExtraction target: [contract clauses/customer needs/experiment parameters/project tasks]\nFields: [field list]\nOutput structured table, source evidence, missing fields, ambiguities, and items needing human confirmation." },
            { text: "制作培训材料大纲", textEn: "Create a training material outline", desc: "按对象、目标、案例和练习组织", descEn: "Organize by audience, goals, examples, practice", icon: "ppt",
              template: "帮我制作培训材料大纲\n培训主题：[主题]\n学员对象：[角色和基础]\n培训目标：[学完会做什么]\n已有资料：[粘贴或拖入]\n请输出：课程大纲、每节目标、案例设计、练习题、讲师备注、课后检查方式。",
              templateEn: "Create a training material outline\nTopic: [topic]\nLearners: [roles and baseline]\nGoal: [what they can do after training]\nExisting materials: [paste or attach]\nOutput course outline, section goals, case design, exercises, instructor notes, and post-training checks." },
            { text: "建立知识库质检清单", textEn: "Create a knowledge-base QA checklist", desc: "检查准确性、可读性、可检索性", descEn: "Check accuracy, readability, findability", icon: "checklist",
              template: "帮我建立知识库质检清单\n知识库类型：[产品/实施/客服/研发/科研]\n使用场景：[检索/培训/对外说明]\n常见问题：[如有]\n请输出：质检维度、检查项、评分标准、问题示例、整改建议、定期复查节奏。",
              templateEn: "Create a knowledge-base QA checklist\nKnowledge-base type: [product/implementation/support/engineering/research]\nUse case: [search/training/external explanation]\nCommon issues: [if any]\nOutput QA dimensions, checks, scoring rules, issue examples, remediation suggestions, and review cadence." },
        ],
    },
    {
        id: "automation",
        label: "自动化流程",
        labelEn: "Automation",
        prompts: [
            { text: "设计一个重复任务自动化", textEn: "Design automation for a repetitive task", desc: "先拆流程，再判断可自动化边界", descEn: "Map workflow before automation boundaries", icon: "workflow",
              template: "帮我设计一个重复任务自动化方案\n当前任务：[任务描述]\n触发条件：[什么时候开始]\n输入来源：[表格/邮件/系统/API/人工]\n输出结果：[文件/通知/系统变更]\n限制：[权限、安全、人工确认]\n请输出：流程图文字版、可自动化步骤、必须人工确认的步骤、需要的工具或接口、失败重试和审计记录方案。",
              templateEn: "Design automation for a repetitive task\nCurrent task: [description]\nTrigger: [when it starts]\nInputs: [spreadsheet/email/system/API/human]\nOutputs: [file/notification/system change]\nConstraints: [permissions/security/human approval]\nOutput text workflow diagram, automatable steps, human-confirmation steps, required tools or APIs, retry handling, and audit log plan." },
            { text: "把表单收集变成工作流", textEn: "Turn form intake into a workflow", desc: "字段、校验、分派和审批都列清楚", descEn: "Define fields, validation, routing, approval", icon: "form",
              template: "把这个表单收集场景设计成工作流\n业务场景：[例如报销、需求申请、客户线索]\n提交人：[谁提交]\n字段：[已知字段]\n审批或处理人：[角色]\n规则：[校验、分派、超时、通知]\n请输出：字段清单、校验规则、流程阶段、状态流转、通知文案、异常分支。",
              templateEn: "Turn this form intake scenario into a workflow\nScenario: [expense, request intake, lead capture]\nSubmitter: [who submits]\nFields: [known fields]\nApprover/processor: [role]\nRules: [validation, routing, timeout, notifications]\nOutput field list, validation rules, stages, status transitions, notification copy, and exception branches." },
            { text: "制定定时巡检和提醒计划", textEn: "Create a scheduled check and reminder plan", desc: "把周期、阈值、通知和升级规则定好", descEn: "Set cadence, thresholds, alerts, escalation", icon: "schedule",
              template: "制定一个定时巡检和提醒计划\n巡检对象：[系统/数据/合同/任务/客户状态]\n检查频率：[每天/每周/每月/自定义]\n异常条件：[阈值或判断规则]\n通知对象：[负责人/群组]\n升级规则：[多久未处理升级]\n请输出：巡检清单、提醒文案、异常分级、升级路径、记录字段。",
              templateEn: "Create a scheduled check and reminder plan\nTarget: [system/data/contract/task/customer status]\nFrequency: [daily/weekly/monthly/custom]\nException rules: [thresholds or logic]\nNotify: [owner/group]\nEscalation: [when to escalate]\nOutput checklist, reminder copy, severity levels, escalation path, and record fields." },
            { text: "把人工流程画成流程图", textEn: "Turn a manual process into a flow diagram", desc: "识别角色、分支、异常和交接点", descEn: "Identify roles, branches, exceptions, handoffs", icon: "diagram",
              template: "把这个人工流程整理成流程图\n流程描述：[粘贴现有流程]\n参与角色：[角色列表]\n起点和终点：[开始条件/完成条件]\n异常情况：[已知异常]\n请输出：流程图文字版、角色泳道、状态节点、分支条件、异常处理、可自动化或可简化的环节。",
              templateEn: "Turn this manual process into a flow diagram\nProcess description: [paste current flow]\nRoles: [role list]\nStart and end: [start condition/completion condition]\nExceptions: [known exceptions]\nOutput text flow diagram, swimlanes, states, branch conditions, exception handling, and steps that can be automated or simplified." },
            { text: "设计审批流权限规则", textEn: "Design approval permissions", desc: "明确角色、权限、代理和越权处理", descEn: "Clarify roles, permissions, delegation, exceptions", icon: "shield",
              template: "帮我设计一套审批流权限规则\n业务场景：[场景]\n审批事项：[费用/合同/需求/发布/数据变更]\n角色：[提交人、审批人、抄送人、管理员]\n限制：[金额、部门、项目、风险等级]\n请输出：角色权限表、审批条件、代理和加签规则、越权处理、审计字段、异常兜底方案。",
              templateEn: "Design approval permissions\nScenario: [scenario]\nApproval item: [expense/contract/request/release/data change]\nRoles: [submitter/approver/cc/admin]\nLimits: [amount/department/project/risk level]\nOutput role-permission table, approval conditions, delegation and add-approver rules, override handling, audit fields, and exception fallback." },
            { text: "生成自动化执行清单", textEn: "Create an automation run checklist", desc: "把触发、执行、校验和回滚列成清单", descEn: "List triggers, execution, checks, rollback", icon: "checklist",
              template: "帮我生成一份自动化执行清单\n自动化目标：[目标]\n执行环境：[系统/账号/权限]\n输入数据：[来源和格式]\n风险点：[已知风险]\n请输出：上线前检查、执行步骤、结果校验、失败重试、人工确认点、回滚方案、运行记录字段。",
              templateEn: "Create an automation run checklist\nAutomation goal: [goal]\nRuntime environment: [systems/accounts/permissions]\nInput data: [sources and format]\nRisks: [known risks]\nOutput pre-run checks, execution steps, result validation, retry handling, human confirmation points, rollback plan, and run-log fields." },
            { text: "设计跨系统数据同步流程", textEn: "Design cross-system data sync", desc: "梳理来源、映射、冲突和补偿机制", descEn: "Map sources, fields, conflicts, compensation", icon: "workflow",
              template: "帮我设计一个跨系统数据同步流程\n源系统：[系统 A]\n目标系统：[系统 B]\n同步对象：[客户/订单/合同/任务/库存]\n同步频率：[实时/定时/手动]\n字段映射：[如有]\n请输出：同步流程、字段映射表、冲突处理、幂等规则、失败补偿、监控告警、审计记录。",
              templateEn: "Design cross-system data sync\nSource system: [system A]\nTarget system: [system B]\nObject: [customer/order/contract/task/inventory]\nFrequency: [real-time/scheduled/manual]\nField mapping: [if any]\nOutput sync flow, field mapping table, conflict handling, idempotency rules, failure compensation, monitoring alerts, and audit records." },
            { text: "优化一个现有流程", textEn: "Optimize an existing workflow", desc: "找瓶颈、重复劳动和可压缩节点", descEn: "Find bottlenecks, repetition, compressible steps", icon: "review",
              template: "帮我优化一个现有流程\n当前流程：[粘贴流程]\n参与角色：[角色]\n主要痛点：[耗时、返工、等待、出错]\n约束：[合规、系统、人员、预算]\n请输出：瓶颈分析、重复劳动清单、可删除/合并/自动化节点、改造优先级、改造后流程、风险和验证指标。",
              templateEn: "Optimize an existing workflow\nCurrent workflow: [paste flow]\nRoles: [roles]\nPain points: [delay/rework/waiting/errors]\nConstraints: [compliance/systems/people/budget]\nOutput bottleneck analysis, repetitive-work list, steps to remove/merge/automate, priority, redesigned workflow, risks, and validation metrics." },
        ],
    },
    {
        id: "data",
        label: "数据表格",
        labelEn: "Data tables",
        prompts: [
            { text: "清洗并规范一张表", textEn: "Clean and standardize a table", desc: "统一字段、格式、缺失值和异常值", descEn: "Normalize fields, formats, missing values", icon: "chart",
              template: "帮我清洗并规范这张表\n数据文件：[拖入 Excel/CSV 或粘贴路径]\n用途：[导入系统/分析/对账/报表]\n关键字段：[字段名]\n已知问题：[重复、缺失、格式混乱、异常值]\n请输出：清洗规则、字段映射表、异常数据清单、可导入的目标格式建议。",
              templateEn: "Clean and standardize this table\nData file: [drag Excel/CSV or paste path]\nUse case: [system import/analysis/reconciliation/reporting]\nKey fields: [field names]\nKnown issues: [duplicates, missing values, mixed formats, outliers]\nOutput cleaning rules, field mapping table, anomaly list, and recommended import-ready format." },
            { text: "做一份经营数据周报", textEn: "Create a weekly business report", desc: "自动提炼趋势、异常和原因假设", descEn: "Extract trends, anomalies, likely causes", icon: "ppt",
              template: "根据这些数据做一份经营数据周报\n数据文件：[拖入文件或粘贴路径]\n报告周期：[日期范围]\n核心指标：[收入/订单/活跃/转化/成本]\n对比口径：[上周/上月/去年同期/目标值]\n请输出：周报摘要、指标表、趋势变化、异常原因假设、下周建议动作。",
              templateEn: "Create a weekly business report from this data\nData file: [drag file or paste path]\nPeriod: [date range]\nMetrics: [revenue/orders/active users/conversion/cost]\nComparison: [last week/last month/YoY/target]\nOutput weekly summary, metrics table, trend changes, anomaly hypotheses, and next-week actions." },
            { text: "生成对账差异分析", textEn: "Generate reconciliation variance analysis", desc: "定位差异来源并给处理建议", descEn: "Find variance sources and next steps", icon: "search",
              template: "帮我做对账差异分析\n数据 A：[文件或路径]\n数据 B：[文件或路径]\n匹配键：[订单号/客户 ID/日期/金额]\n允许误差：[金额或时间范围]\n请输出：匹配规则、完全匹配清单、差异清单、可能原因分类、需要人工确认的项目、处理建议。",
              templateEn: "Generate reconciliation variance analysis\nData A: [file or path]\nData B: [file or path]\nMatch keys: [order ID/customer ID/date/amount]\nAllowed tolerance: [amount or time window]\nOutput match rules, exact matches, variance list, likely cause categories, items needing human confirmation, and handling recommendations." },
            { text: "设计一套看板指标", textEn: "Design dashboard metrics", desc: "定义口径、维度、刷新频率和图表", descEn: "Define metrics, dimensions, refresh, charts", icon: "chart",
              template: "帮我设计一套看板指标\n业务场景：[销售/运营/产品/财务/交付]\n使用者：[管理层/团队负责人/一线人员]\n决策问题：[看板要帮助判断什么]\n数据来源：[系统、表格、接口]\n请输出：指标体系、每个指标口径、维度拆分、刷新频率、推荐图表、异常阈值、看板布局建议。",
              templateEn: "Design dashboard metrics\nScenario: [sales/operations/product/finance/delivery]\nUsers: [leadership/team lead/frontline]\nDecision questions: [what the dashboard should help decide]\nData sources: [systems, spreadsheets, APIs]\nOutput metric system, definition for each metric, dimensions, refresh cadence, chart recommendations, alert thresholds, and dashboard layout." },
            { text: "分析用户或客户留存", textEn: "Analyze user or customer retention", desc: "按 cohort 找留存变化和流失原因", descEn: "Use cohorts to find retention and churn signals", icon: "users",
              template: "帮我分析用户或客户留存\n数据文件：[拖入文件或粘贴路径]\n对象：[用户/客户/账号/门店]\n时间字段：[注册/首单/签约/活跃日期]\n分群维度：[渠道、行业、地区、套餐、客户类型]\n请输出：cohort 留存表、关键变化、流失高风险分群、可能原因、验证方法和提升动作。",
              templateEn: "Analyze user or customer retention\nData file: [drag file or paste path]\nEntity: [user/customer/account/store]\nTime field: [signup/first order/contract/activity date]\nSegments: [channel/industry/region/plan/customer type]\nOutput cohort retention table, key changes, high-risk segments, likely causes, validation methods, and improvement actions." },
            { text: "做漏斗转化分析", textEn: "Analyze funnel conversion", desc: "定位每一步转化率和掉点原因", descEn: "Find step conversion and drop-off causes", icon: "target",
              template: "帮我做漏斗转化分析\n数据文件：[拖入文件或粘贴路径]\n漏斗步骤：[访问/注册/试用/询价/下单/支付]\n时间范围：[日期]\n分组维度：[渠道、产品、地区、人群]\n请输出：漏斗转化表、每步转化率、掉点最大的环节、分组对比、原因假设、优化建议和需要补充的数据。",
              templateEn: "Analyze funnel conversion\nData file: [drag file or paste path]\nFunnel steps: [visit/signup/trial/inquiry/order/payment]\nDate range: [dates]\nGroups: [channel/product/region/audience]\nOutput funnel table, conversion rate per step, largest drop-off points, segment comparison, cause hypotheses, recommendations, and missing data." },
            { text: "生成数据字典", textEn: "Create a data dictionary", desc: "统一字段含义、类型、口径和来源", descEn: "Standardize meanings, types, definitions, sources", icon: "form",
              template: "帮我生成一份数据字典\n数据表或文件：[拖入文件、粘贴字段或路径]\n业务场景：[系统导入/报表/建模/对账]\n已有说明：[如有]\n请输出：字段名、中文名、类型、业务含义、计算口径、来源、是否必填、示例值、质量规则和备注。",
              templateEn: "Create a data dictionary\nTable or file: [drag file, paste fields, or path]\nUse case: [system import/reporting/modeling/reconciliation]\nExisting notes: [if any]\nOutput field name, display name, type, business meaning, calculation definition, source, required status, example value, quality rules, and notes." },
            { text: "检查数据质量规则", textEn: "Check data quality rules", desc: "覆盖完整性、唯一性、一致性和异常值", descEn: "Cover completeness, uniqueness, consistency, outliers", icon: "shield",
              template: "帮我检查并设计数据质量规则\n数据对象：[表名/文件/接口]\n业务用途：[用途]\n关键字段：[字段]\n已知问题：[缺失、重复、口径不一致、异常值]\n请输出：质量维度、检查规则、SQL/伪代码示例、异常分级、处理建议、定期质检计划。",
              templateEn: "Check and design data quality rules\nData object: [table/file/API]\nBusiness use: [use case]\nKey fields: [fields]\nKnown issues: [missing values/duplicates/inconsistent definitions/outliers]\nOutput quality dimensions, validation rules, SQL or pseudocode examples, severity levels, handling suggestions, and recurring QA plan." },
        ],
    },
];

const STORAGE_KEY = "maclaw:welcome-scenario-tab";
const LEGACY_STORAGE_KEY = "maclaw:welcome-industry-tab";
const SCENARIO_TAB_IDS = new Set(SCENARIO_TABS.map(tab => tab.id));
const SCENARIO_TAB_BY_ID = new Map(SCENARIO_TABS.map(tab => [tab.id, tab]));
const isScenarioTabId = (value: string | null): value is string => !!value && SCENARIO_TAB_IDS.has(value);

/** Max width for the main content column (input, tabs, cards). */
const CONTENT_MAX_WIDTH = "720px";

// --- Component ---

/** Props subset needed by AssistantInputComposer inside the welcome view. */
export interface WelcomeComposerProps {
    browseFile: () => void;
    canSend: boolean;
    cancelPending: boolean;
    cancelSession?: unknown;
    clearSelectedFile?: () => void;
    composeAction?: ComposeAction | null;
    exitHistoryBrowsing: () => boolean;
    finishVoicePointer: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleCancel: () => void;
    handleClearInput: () => void;
    handleDragOver: (event: React.DragEvent<HTMLElement>) => void;
    handleDrop: (event: React.DragEvent<HTMLElement>) => void;
    handlePaste: (event: React.ClipboardEvent<HTMLTextAreaElement>) => void;
    handleSend: () => void;
    handleVoiceClick: () => void;
    handleVoicePointerDown: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleVoicePointerLeave: (event: React.PointerEvent<HTMLButtonElement>) => void;
    inputLocked: boolean;
    inputRef: React.Ref<HTMLTextAreaElement>;
    inputValue: string;
    isBusy: boolean;
    isSelectionCollapsedAtBoundary: (direction: "up" | "down") => boolean;
    onComposeActionChange?: (action: ComposeAction | null) => void;
    onFireSlashCommand?: (command: FireSlashCommand) => void;
    onInsertTemplate?: (template: string) => void;
    onPlusMenuAction?: (actionId: PlusMenuActionId) => void;
    pendingAttachments: AttachmentInfo[];
    permissionMode?: AssistantPermissionMode;
    onPermissionModeChange?: (mode: AssistantPermissionMode) => void;
    ready: boolean;
    recallHistory: (direction: "up" | "down") => boolean;
    rememberHistoryEdit: (value: string) => void;
    removeSelectedFile?: (index: number) => void;
    resizeInput: () => void;
    selectedFilePaths: string[];
    setPendingAttachments: React.Dispatch<React.SetStateAction<AttachmentInfo[]>>;
    showBusySpinner: boolean;
    submittedPrompts?: string[];
    updateInputValue: (value: string) => void;
    voiceInput: UseVoiceInputResult;
}

interface AssistantWelcomeViewProps {
    lang: string;
    theme: Theme;
    themeMode: "light" | "dark";
    onPromptSelect: (text: string) => void;
    pinnedNews?: ChatMessage[];
    composer: WelcomeComposerProps;
}

export function AssistantWelcomeView({ lang, theme: t, themeMode, onPromptSelect, pinnedNews, composer: cp }: AssistantWelcomeViewProps) {
    const isZh = !lang?.startsWith("en");

    const [activeTab, setActiveTab] = useState<string>(() => {
        try {
            const saved = localStorage.getItem(STORAGE_KEY) || localStorage.getItem(LEGACY_STORAGE_KEY);
            if (isScenarioTabId(saved)) return saved;
        } catch { /* ignore */ }
        return SCENARIO_TABS[0].id;
    });

    useEffect(() => {
        try {
            localStorage.setItem(STORAGE_KEY, activeTab);
            localStorage.removeItem(LEGACY_STORAGE_KEY);
        } catch { /* ignore */ }
    }, [activeTab]);

    const currentTab = SCENARIO_TAB_BY_ID.get(activeTab) || SCENARIO_TABS[0];
    const handleScenarioTabKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
        const key = event.key;
        if (!["ArrowRight", "ArrowDown", "ArrowLeft", "ArrowUp", "Home", "End"].includes(key)) return;
        event.preventDefault();

        const lastIndex = SCENARIO_TABS.length - 1;
        const nextIndex =
            key === "Home" ? 0 :
            key === "End" ? lastIndex :
            key === "ArrowRight" || key === "ArrowDown" ? (index + 1) % SCENARIO_TABS.length :
            (index - 1 + SCENARIO_TABS.length) % SCENARIO_TABS.length;
        const nextTabId = SCENARIO_TABS[nextIndex].id;
        setActiveTab(nextTabId);
        requestAnimationFrame(() => {
            const el = document.getElementById(`welcome-tab-${nextTabId}`);
            el?.focus();
            el?.scrollIntoView({ block: "nearest", inline: "nearest" });
        });
    };

    const hasNews = pinnedNews && pinnedNews.length > 0;

    return (
        <div
            role="region"
            aria-label={isZh ? "工作台任务入口" : "Workbench task entry"}
            tabIndex={0}
            style={{
                display: "flex",
                flexDirection: "column",
                height: "100%",
                boxSizing: "border-box",
                overflowY: "auto",
            }}
        >
            {/* Pinned news cards pinned to top */}
            {hasNews && (
                <div style={{ flexShrink: 0, padding: "12px 16px 0", display: "flex", justifyContent: "center" }}>
                    <div style={{ width: "100%", maxWidth: "520px" }}>
                        <AssistantPinnedNewsCards messages={pinnedNews} theme={t} />
                    </div>
                </div>
            )}

            {/* Main content centered in remaining space.
                Uses margin:auto instead of justifyContent:center to avoid
                top-clipping when content overflows a short panel. */}
            <div style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                padding: "16px 16px 16px",
                gap: "14px",
                margin: "auto 0",
                flexShrink: 0,
            }}>

            {/* Title — invite the user to pick a starter task */}
            <h2 style={{
                margin: 0,
                fontSize: "13px",
                fontWeight: 600,
                color: t.textMuted,
                textAlign: "center",
                fontFamily: "system-ui, -apple-system, sans-serif",
                letterSpacing: "0.01em",
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                gap: "6px",
            }}>
                <WelcomePromptIcon name="checklist" color={t.textMuted} />
                {isZh ? "选择一个任务开始吧！" : "Pick a task to get started!"}
            </h2>

            {/* Centered input composer — workbench field, not chat bubble */}
            <div style={{
                width: "100%",
                maxWidth: CONTENT_MAX_WIDTH,
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: t.inputBarBg,
                // Visible so history autocomplete can paint above the composer.
                overflow: "visible",
            }}>
                <AssistantInputComposer
                    browseFile={cp.browseFile}
                    canSend={cp.canSend}
                    cancelPending={cp.cancelPending}
                    cancelSession={cp.cancelSession}
                    clearSelectedFile={cp.clearSelectedFile}
                    composeAction={cp.composeAction}
                    exitHistoryBrowsing={cp.exitHistoryBrowsing}
                    finishVoicePointer={cp.finishVoicePointer}
                    handleCancel={cp.handleCancel}
                    handleClearInput={cp.handleClearInput}
                    handleDragOver={cp.handleDragOver}
                    handleDrop={cp.handleDrop}
                    handlePaste={cp.handlePaste}
                    handleSend={cp.handleSend}
                    handleVoiceClick={cp.handleVoiceClick}
                    handleVoicePointerDown={cp.handleVoicePointerDown}
                    handleVoicePointerLeave={cp.handleVoicePointerLeave}
                    inputAreaHeight={null}
                    inputLocked={cp.inputLocked}
                    inputRef={cp.inputRef}
                    inputValue={cp.inputValue}
                    inline={true}
                    isBusy={cp.isBusy}
                    isSelectionCollapsedAtBoundary={cp.isSelectionCollapsedAtBoundary}
                    lang={lang}
                    onComposeActionChange={cp.onComposeActionChange}
                    onFireSlashCommand={cp.onFireSlashCommand}
                    onInsertTemplate={cp.onInsertTemplate}
                    onPlusMenuAction={cp.onPlusMenuAction}
                    pendingAttachments={cp.pendingAttachments}
                    permissionMode={cp.permissionMode}
                    onPermissionModeChange={cp.onPermissionModeChange}
                    placeholderText={
                        getComposeActionPlaceholder(cp.composeAction, isZh)
                            || (isZh ? "输入任务或指令…" : "Enter a task or command...")
                    }
                    ready={cp.ready}
                    recallHistory={cp.recallHistory}
                    rememberHistoryEdit={cp.rememberHistoryEdit}
                    removeSelectedFile={cp.removeSelectedFile}
                    resizeInput={cp.resizeInput}
                    selectedFilePaths={cp.selectedFilePaths}
                    setPendingAttachments={cp.setPendingAttachments}
                    showBusySpinner={cp.showBusySpinner}
                    showMemoryUsage={false}
                    showVoiceInput={true}
                    submittedPrompts={cp.submittedPrompts}
                    theme={t}
                    themeMode={themeMode}
                    updateInputValue={cp.updateInputValue}
                    voiceInput={cp.voiceInput}
                />
            </div>

            {/* Scenario tabs — outer scroll container + inner centering wrapper.
                Using a wrapper div with margin:auto to center tabs when they fit,
                while allowing left-aligned overflow scroll when they don't.
                justify-content:center on overflow would clip left-side tabs. */}
            <div
                role="tablist"
                aria-label={isZh ? "场景分类" : "Scenario categories"}
                className="no-scrollbar"
                style={{
                    width: "100%",
                    maxWidth: CONTENT_MAX_WIDTH,
                    overflowX: "auto",
                    scrollbarWidth: "none",
                }}
            >
                <div style={{
                    display: "flex",
                    flexWrap: "nowrap",
                    gap: "6px",
                    width: "fit-content",
                    margin: "0 auto",
                }}>
                {SCENARIO_TABS.map((tab, index) => {
                    const isActive = tab.id === activeTab;
                        return (
                        <button
                            type="button"
                            key={tab.id}
                            id={`welcome-tab-${tab.id}`}
                            role="tab"
                            aria-selected={isActive}
                            aria-controls={`welcome-tabpanel-${tab.id}`}
                            tabIndex={isActive ? 0 : -1}
                            onClick={() => setActiveTab(tab.id)}
                            onKeyDown={event => handleScenarioTabKeyDown(event, index)}
                            style={{
                                padding: "4px 10px",
                                fontSize: "12px",
                                fontWeight: isActive ? 600 : 400,
                                lineHeight: 1.3,
                                color: isActive ? t.text : t.textMuted,
                                background: isActive ? t.fieldBg : "transparent",
                                border: `1px solid ${isActive ? t.fieldBorder : "transparent"}`,
                                borderRadius: "6px",
                                boxSizing: "border-box",
                                cursor: "pointer",
                                transition: "background 0.12s ease, border-color 0.12s ease, color 0.12s ease",
                                whiteSpace: "nowrap",
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}
                            onMouseEnter={e => {
                                if (!isActive) {
                                    e.currentTarget.style.color = t.text;
                                    e.currentTarget.style.background = t.fieldBg;
                                }
                            }}
                            onMouseLeave={e => {
                                if (!isActive) {
                                    e.currentTarget.style.color = t.textMuted;
                                    e.currentTarget.style.background = "transparent";
                                }
                            }}
                        >
                            {isZh ? tab.label : tab.labelEn}
                        </button>
                    );
                })}
                </div>
            </div>

            {/* Prompt cards */}
            <div
                role="tabpanel"
                id={`welcome-tabpanel-${currentTab.id}`}
                aria-labelledby={`welcome-tab-${currentTab.id}`}
                style={{
                    display: "grid",
                    gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
                    gap: "10px",
                    width: "100%",
                    maxWidth: CONTENT_MAX_WIDTH,
                }}
            >
                {currentTab.prompts.map(prompt => (
                    <button
                        type="button"
                        key={`${currentTab.id}-${prompt.textEn}`}
                        onClick={() => onPromptSelect(isZh ? (prompt.template || prompt.text) : (prompt.templateEn || prompt.textEn))}
                        style={{
                            display: "flex",
                            alignItems: "flex-start",
                            gap: "10px",
                            padding: "10px 12px",
                            background: t.fieldBg,
                            border: `1px solid ${t.fieldBorder}`,
                            borderRadius: "6px",
                            boxSizing: "border-box",
                            cursor: "pointer",
                            textAlign: "left",
                            transition: "border-color 0.12s ease, background 0.12s ease",
                            width: "100%",
                            minWidth: 0,
                            minHeight: "64px",
                        }}
                        onMouseEnter={e => {
                            e.currentTarget.style.borderColor = t.inputBarBorder;
                            e.currentTarget.style.background = t.inputBarBg;
                        }}
                        onMouseLeave={e => {
                            e.currentTarget.style.borderColor = t.fieldBorder;
                            e.currentTarget.style.background = t.fieldBg;
                        }}
                        onFocus={e => {
                            e.currentTarget.style.borderColor = t.sendBtnBorder || t.sendBtnBg;
                        }}
                        onBlur={e => {
                            e.currentTarget.style.borderColor = t.fieldBorder;
                        }}
                    >
                        <WelcomePromptIcon name={prompt.icon} color={t.textMuted} />
                        <div style={{ display: "flex", flexDirection: "column", gap: "3px", minWidth: 0 }}>
                            <span style={{
                                fontSize: "13px",
                                fontWeight: 500,
                                lineHeight: 1.35,
                                color: t.text,
                                overflowWrap: "anywhere",
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}>
                                {isZh ? prompt.text : prompt.textEn}
                            </span>
                            <span style={{
                                fontSize: "11px",
                                lineHeight: 1.35,
                                color: t.textMuted,
                                overflowWrap: "anywhere",
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}>
                                {isZh ? prompt.desc : prompt.descEn}
                            </span>
                        </div>
                    </button>
                ))}
            </div>
            </div>
        </div>
    );
}
