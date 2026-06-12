import { useState, useEffect } from "react";
import type React from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import type { ChatMessage } from "./useAIAssistant";
import { AssistantInputComposer } from "./AssistantInputComposer";
import { AssistantPinnedNewsCards } from "./AssistantPinnedNewsCards";
import type { AttachmentInfo } from "./useBufferQueue";
import type { UseVoiceInputResult } from "./useVoiceInput";

// --- Data ---

interface IndustryTab {
    id: string;
    label: string;
    labelEn: string;
    prompts: { text: string; textEn: string; desc: string; descEn: string; icon: string; template?: string; templateEn?: string }[];
}

// SVG path data for prompt card icons (16x16 viewBox, stroke-only design)
const PROMPT_ICONS: Record<string, string> = {
    // Business
    ppt: "M2 2h12v12H2zM5 6h3M5 6v5M5 8.5h2.5",
    plan: "M4 1h5l4 4v10H4V1zM9 1v4h4M6 8h5M6 10h5M6 12h3",
    contract: "M4 1h5l4 4v10H4V1zM9 1v4h4M6 9l1 3 1.5-2 1.5 2 1-3",
    // Dev
    code: "M5.5 4.5L2 8l3.5 3.5M10.5 4.5L14 8l-3.5 3.5M9 2L7 14",
    bug: "M6 6v4a2 2 0 0 0 4 0V6a2 2 0 0 0-4 0zM5 6.5H3M5 8.5H3M5 10.5H3M11 6.5h2M11 8.5h2M11 10.5h2M6.5 4.5L5 3M9.5 4.5L11 3",
    docker: "M1 9h14M4 9V7h2v2M7 9V7h2v2M10 9V7h2v2M7 7V5h2v2M4 7V5h2v2M3 11c1 3 5 4 9 2.5 2-1 3-2.5 3.5-4",
    // Ops
    server: "M2 3h12v4H2zM2 9h12v4H2zM5 5h.01M5 11h.01M10 5h2M10 11h2",
    install: "M8 2v7M5.5 6.5L8 9l2.5-2.5M3 11v2h10v-2",
    deploy: "M3 10l5 5 5-5M3 10h10M8 1v5M5.5 3.5L8 1l2.5 2.5",
    // Research
    search: "M10.5 10.5L14 14M6.5 2a4.5 4.5 0 1 0 0 9 4.5 4.5 0 0 0 0-9z",
    translate: "M2 2h5M4.5 2v2M3 6c1 1.5 2.5 2.5 4 3M7.5 2C7 4 5.5 5.5 4 7M9.5 7l2 6M10 11h3M13 5V3h-2.5",
    chart: "M3 13V8M6.5 13V5M10 13V9M13.5 13V6M1.5 13h13",
};

const INDUSTRY_TABS: IndustryTab[] = [
    {
        id: "business",
        label: "商业办公",
        labelEn: "Business",
        prompts: [
            { text: "做一份竞品分析 PPT", textEn: "Create a competitive analysis PPT", desc: "自动走工作流生成专业文档", descEn: "Auto workflow generates professional docs", icon: "ppt",
              template: "做一份竞品分析 PPT\n行业：[你的行业]\n我方产品：[产品名称]\n竞品：[竞品1, 竞品2]\n重点维度：功能/价格/市场份额",
              templateEn: "Create a competitive analysis PPT\nIndustry: [your industry]\nOur product: [product name]\nCompetitors: [competitor 1, competitor 2]\nFocus: features/pricing/market share" },
            { text: "起草一份商业计划书", textEn: "Draft a business plan", desc: "需求→框架→内容逐步生成", descEn: "Requirements → outline → content step by step", icon: "plan",
              template: "起草一份商业计划书\n项目名称：[项目名]\n行业领域：[领域]\n核心产品/服务：[简述]\n目标市场：[目标客群]",
              templateEn: "Draft a business plan\nProject: [project name]\nIndustry: [field]\nCore product/service: [brief description]\nTarget market: [target audience]" },
            { text: "审查这份合同的风险条款", textEn: "Review contract risk clauses", desc: "上传文档自动分析标注风险", descEn: "Upload docs for auto risk analysis", icon: "contract",
              template: "审查这份合同的风险条款\n合同文件：[拖入文件或粘贴路径]\n关注重点：违约责任/知识产权/竞业限制",
              templateEn: "Review contract risk clauses\nContract file: [drag file or paste path]\nFocus areas: liability/IP rights/non-compete" },
        ],
    },
    {
        id: "dev",
        label: "软件开发",
        labelEn: "Development",
        prompts: [
            { text: "开发一个 React 管理后台", textEn: "Build a React admin dashboard", desc: "自动走需求→设计→编码流程", descEn: "Auto requirements → design → coding flow", icon: "code",
              template: "开发一个 React 管理后台\n项目路径：[d:\\your\\project\\path]\n功能模块：用户管理/数据看板/权限控制\n技术栈偏好：[React + Ant Design / 其他]",
              templateEn: "Build a React admin dashboard\nProject path: [d:\\your\\project\\path]\nModules: user management/dashboard/access control\nTech preference: [React + Ant Design / other]" },
            { text: "修复登录页面的白屏 bug", textEn: "Fix the login page white screen bug", desc: "直接定位问题并修复", descEn: "Directly locate and fix the issue", icon: "bug",
              template: "修复登录页面的白屏 bug\n项目路径：[d:\\your\\project\\path]\n复现步骤：[打开登录页后...]\n报错信息：[控制台错误或截图]",
              templateEn: "Fix the login page white screen bug\nProject path: [d:\\your\\project\\path]\nRepro steps: [after opening login page...]\nError: [console error or screenshot]" },
            { text: "给项目加上 Docker 部署", textEn: "Add Docker deployment to project", desc: "生成 Dockerfile + compose 配置", descEn: "Generate Dockerfile + compose config", icon: "docker",
              template: "给项目加上 Docker 部署\n项目路径：[d:\\your\\project\\path]\n项目类型：[Node.js / Python / Go / Java]\n需要的服务：[Redis/MySQL/Nginx 等]",
              templateEn: "Add Docker deployment to project\nProject path: [d:\\your\\project\\path]\nProject type: [Node.js / Python / Go / Java]\nServices needed: [Redis/MySQL/Nginx etc.]" },
        ],
    },
    {
        id: "ops",
        label: "运维",
        labelEn: "DevOps",
        prompts: [
            { text: "清理服务器垃圾数据", textEn: "Clean up server junk data", desc: "连接服务器自动清理日志/缓存/临时文件", descEn: "SSH to server, auto-clean logs/cache/tmp", icon: "server",
              template: "清理服务器垃圾数据\n服务器：[IP或域名]\n用户名：[root]\n密码：[password]\n清理目标：日志/缓存/临时文件/Docker 镜像",
              templateEn: "Clean up server junk data\nServer: [IP or domain]\nUsername: [root]\nPassword: [password]\nClean targets: logs/cache/tmp files/Docker images" },
            { text: "在服务器上安装软件", textEn: "Install software on server", desc: "SSH 登录 + 自动安装配置", descEn: "SSH login + auto install & configure", icon: "install",
              template: "在服务器上安装软件\n服务器：[IP或域名]\n用户名：[root]\n密码：[password]\n安装软件：[Docker / Nginx / Node.js / Python]",
              templateEn: "Install software on server\nServer: [IP or domain]\nUsername: [root]\nPassword: [password]\nSoftware: [Docker / Nginx / Node.js / Python]" },
            { text: "根据文档批量部署软件", textEn: "Batch deploy software from docs", desc: "读取部署文档，逐台连接执行", descEn: "Parse deploy docs, connect & execute", icon: "deploy",
              template: "根据文档批量部署软件\n部署文档：[拖入文件或粘贴路径]\n（文档中应包含：服务器列表、登录凭据、部署步骤）",
              templateEn: "Batch deploy software from docs\nDeploy document: [drag file or paste path]\n(Doc should contain: server list, credentials, deploy steps)" },
        ],
    },
    {
        id: "research",
        label: "学术研究",
        labelEn: "Research",
        prompts: [
            { text: "搜集 AI Agent 最新论文做综述", textEn: "Collect latest AI Agent papers for review", desc: "自动搜索 + 抓取 + 整理成报告", descEn: "Auto search + crawl + compile report", icon: "search",
              template: "搜集 AI Agent 最新论文做综述\n研究主题：AI Agent\n时间范围：[最近3个月]\n论文来源：HuggingFace / arXiv / Google Scholar\n输出格式：中文综述 PDF",
              templateEn: "Collect latest AI Agent papers for review\nTopic: AI Agent\nTime range: [last 3 months]\nSources: HuggingFace / arXiv / Google Scholar\nOutput: review PDF" },
            { text: "翻译这篇英文论文成中文", textEn: "Translate this English paper to Chinese", desc: "保持学术格式精准翻译", descEn: "Preserve academic formatting", icon: "translate",
              template: "翻译这篇英文论文成中文\n论文文件：[拖入 PDF 或粘贴路径]\n输出要求：保持学术格式，专业术语准确",
              templateEn: "Translate this English paper to Chinese\nPaper file: [drag PDF or paste path]\nRequirements: preserve academic formatting, accurate terminology" },
            { text: "整理实验数据生成分析报告", textEn: "Organize experiment data into report", desc: "数据处理 + 可视化 + PDF 输出", descEn: "Data processing + visualization + PDF", icon: "chart",
              template: "整理实验数据生成分析报告\n数据文件：[拖入文件或粘贴路径]\n分析维度：[对比实验/趋势分析/统计检验]\n输出格式：PDF 报告（含图表）",
              templateEn: "Organize experiment data into report\nData file: [drag file or paste path]\nAnalysis: [comparison/trend/statistical tests]\nOutput: PDF report with charts" },
        ],
    },
];

const STORAGE_KEY = "maclaw:welcome-industry-tab";

// --- Component ---

/** Props subset needed by AssistantInputComposer inside the welcome view. */
export interface WelcomeComposerProps {
    browseFile: () => void;
    canSend: boolean;
    cancelPending: boolean;
    cancelSession?: unknown;
    clearSelectedFile?: () => void;
    exitHistoryBrowsing: () => boolean;
    finishVoicePointer: (event: React.PointerEvent<HTMLButtonElement>) => void;
    handleCancel: () => void;
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
    pendingAttachments: AttachmentInfo[];
    ready: boolean;
    recallHistory: (direction: "up" | "down") => boolean;
    rememberHistoryEdit: (value: string) => void;
    removeSelectedFile?: (index: number) => void;
    resizeInput: () => void;
    selectedFilePaths: string[];
    setPendingAttachments: React.Dispatch<React.SetStateAction<AttachmentInfo[]>>;
    showBusySpinner: boolean;
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
            const saved = localStorage.getItem(STORAGE_KEY);
            if (saved && INDUSTRY_TABS.some(tab => tab.id === saved)) return saved;
        } catch { /* ignore */ }
        return INDUSTRY_TABS[0].id;
    });

    useEffect(() => {
        try { localStorage.setItem(STORAGE_KEY, activeTab); } catch { /* ignore */ }
    }, [activeTab]);

    const currentTab = INDUSTRY_TABS.find(tab => tab.id === activeTab) || INDUSTRY_TABS[0];

    const hasNews = pinnedNews && pinnedNews.length > 0;

    return (
        <div
            role="region"
            aria-label={isZh ? "AI 助手启动页" : "AI assistant welcome"}
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
                gap: "18px",
                margin: "auto 0",
                flexShrink: 0,
            }}>

            {/* Title */}
            <h2 style={{
                margin: 0,
                fontSize: "22px",
                fontWeight: 700,
                color: t.text,
                textAlign: "center",
                fontFamily: "system-ui, -apple-system, sans-serif",
                letterSpacing: "-0.3px",
            }}>
                {isZh ? "今天想做点什么？" : "What would you like to do?"}
            </h2>

            {/* Centered input composer with refined style */}
            <div style={{
                width: "100%",
                maxWidth: "520px",
                borderRadius: "14px",
                border: `1px solid ${t.inputBarBorder}`,
                boxShadow: themeMode === "dark"
                    ? "0 2px 12px rgba(0,0,0,0.32)"
                    : "0 2px 12px rgba(0,0,0,0.06)",
                background: t.inputBarBg,
                overflow: "hidden",
            }}>
                <AssistantInputComposer
                    browseFile={cp.browseFile}
                    canSend={cp.canSend}
                    cancelPending={cp.cancelPending}
                    cancelSession={cp.cancelSession}
                    clearSelectedFile={cp.clearSelectedFile}
                    exitHistoryBrowsing={cp.exitHistoryBrowsing}
                    finishVoicePointer={cp.finishVoicePointer}
                    handleCancel={cp.handleCancel}
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
                    pendingAttachments={cp.pendingAttachments}
                    placeholderText={isZh ? "描述你的需求，或直接问我任何问题..." : "Describe what you need, or ask me anything..."}
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
                    theme={t}
                    themeMode={themeMode}
                    updateInputValue={cp.updateInputValue}
                    voiceInput={cp.voiceInput}
                />
            </div>

            {/* Industry tabs */}
            <div
                role="tablist"
                aria-label={isZh ? "行业分类" : "Industry categories"}
                style={{
                    display: "flex",
                    gap: "4px",
                    flexWrap: "wrap",
                    justifyContent: "center",
                }}
            >
                {INDUSTRY_TABS.map(tab => {
                    const isActive = tab.id === activeTab;
                    return (
                        <button
                            key={tab.id}
                            role="tab"
                            aria-selected={isActive}
                            aria-controls={`welcome-tabpanel-${tab.id}`}
                            onClick={() => setActiveTab(tab.id)}
                            style={{
                                padding: "5px 12px",
                                fontSize: "12px",
                                fontWeight: isActive ? 600 : 400,
                                color: isActive ? t.sendBtnBg : t.textMuted,
                                background: isActive ? `color-mix(in srgb, ${t.sendBtnBg} 10%, transparent)` : "transparent",
                                border: `1px solid ${isActive ? t.sendBtnBg : "transparent"}`,
                                borderRadius: "16px",
                                cursor: "pointer",
                                transition: "all 0.15s ease",
                                whiteSpace: "nowrap",
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}
                            onMouseEnter={e => {
                                if (!isActive) {
                                    e.currentTarget.style.color = t.text;
                                    e.currentTarget.style.background = `color-mix(in srgb, ${t.text} 5%, transparent)`;
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

            {/* Prompt cards */}
            <div
                role="tabpanel"
                id={`welcome-tabpanel-${currentTab.id}`}
                aria-label={isZh ? currentTab.label : currentTab.labelEn}
                style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: "8px",
                    width: "100%",
                    maxWidth: "480px",
                }}
            >
                {currentTab.prompts.map((prompt, idx) => (
                    <button
                        key={`${currentTab.id}-${idx}`}
                        onClick={() => onPromptSelect(isZh ? (prompt.template || prompt.text) : (prompt.templateEn || prompt.textEn))}
                        style={{
                            display: "flex",
                            alignItems: "flex-start",
                            gap: "10px",
                            padding: "10px 14px",
                            background: t.fieldBg,
                            border: `1px solid ${t.fieldBorder}`,
                            borderRadius: "10px",
                            cursor: "pointer",
                            textAlign: "left",
                            transition: "all 0.15s ease",
                            width: "100%",
                        }}
                        onMouseEnter={e => {
                            e.currentTarget.style.borderColor = t.sendBtnBg;
                            e.currentTarget.style.transform = "translateY(-1px)";
                            e.currentTarget.style.boxShadow = `0 2px 8px color-mix(in srgb, ${t.sendBtnBg} 12%, transparent)`;
                        }}
                        onMouseLeave={e => {
                            e.currentTarget.style.borderColor = t.fieldBorder;
                            e.currentTarget.style.transform = "none";
                            e.currentTarget.style.boxShadow = "none";
                        }}
                    >
                        <svg
                            width="18"
                            height="18"
                            viewBox="0 0 16 16"
                            fill="none"
                            stroke={t.textMuted}
                            strokeWidth="1.3"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            style={{ flexShrink: 0, marginTop: "1px" }}
                            aria-hidden="true"
                        >
                            <path d={PROMPT_ICONS[prompt.icon] || ""} />
                        </svg>
                        <div style={{ display: "flex", flexDirection: "column", gap: "3px", minWidth: 0 }}>
                            <span style={{
                                fontSize: "13px",
                                fontWeight: 500,
                                color: t.text,
                                fontFamily: "system-ui, -apple-system, sans-serif",
                            }}>
                                {isZh ? prompt.text : prompt.textEn}
                            </span>
                            <span style={{
                                fontSize: "11px",
                                color: t.textMuted,
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
