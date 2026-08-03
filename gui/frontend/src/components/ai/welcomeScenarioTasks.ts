import type { PureCodingAgentMode } from "./codingTaskMode";
import type { WelcomePromptIconName } from "./welcomeScenarioCatalogGuards";

/**
 * Welcome-page scenario cards for the AI assistant workbench.
 *
 * Templates use [placeholder] slots. The welcome param dialog turns each
 * `标签：[提示]` line into a labeled form field so users no longer fill
 * blanks inside the chat input.
 *
 * Design goals:
 * - 8 practical tasks per scenario tab (2×4 grid)
 * - 2–4 parameters per task (not essay forms)
 * - Clear deliverables the assistant can execute
 * - Coding cards keep agentMode so they open create-task after params
 */

/** Expected number of prompt cards per scenario tab. */
export const WELCOME_SCENARIO_PROMPTS_PER_TAB = 8;

export interface WelcomePrompt {
    text: string;
    textEn: string;
    desc: string;
    descEn: string;
    icon: WelcomePromptIconName;
    template?: string;
    templateEn?: string;
    /**
     * When set, after params are filled the create-task dialog opens in that
     * mode (same path as sidebar 本地编程 / 远程编程).
     */
    agentMode?: PureCodingAgentMode;
    /**
     * Extra safety posture for a remote coding card. Diagnosis starts with a
     * read-only SSH inspection turn; it never creates a remote directory or
     * changes a service just because the lightweight intent classifier failed.
     */
    remoteSafety?: "diagnosis";
}

/** The two execution contexts offered by the operations scenario. */
export type WelcomeOpsMode = "local" | "remote";

export interface ScenarioTab {
    id: string;
    label: string;
    labelEn: string;
    prompts: WelcomePrompt[];
}

export const SCENARIO_TABS: ScenarioTab[] = [
    {
        id: "business",
        label: "经营分析",
        labelEn: "Business analysis",
        prompts: [
            {
                text: "做一份可汇报的竞品分析",
                textEn: "Prepare an executive competitor brief",
                desc: "结论、对比表、机会与下一步",
                descEn: "Summary, comparison, opportunities, next steps",
                icon: "ppt",
                template:
                    "帮我做一份可汇报的竞品分析\n行业/赛道：[例如 SaaS 协作]\n我方产品：[名称 + 一句话定位]\n主要竞品：[A / B / C]\n请输出：一页结论、功能/价格/渠道对比表、我方机会与风险、下一步动作、PPT 大纲。信息不足时先追问。",
                templateEn:
                    "Prepare an executive competitor brief\nIndustry: [e.g. SaaS collab]\nOur product: [name + one-line positioning]\nMain competitors: [A / B / C]\nOutput: one-page summary, feature/price/channel table, opportunities & risks, next actions, slide outline. Ask if context is missing.",
            },
            {
                text: "把零散想法整理成项目方案",
                textEn: "Turn rough ideas into a project proposal",
                desc: "目标、范围、里程碑、风险",
                descEn: "Goals, scope, milestones, risks",
                icon: "plan",
                template:
                    "把下面的想法整理成可执行项目方案\n目标：[希望达成什么]\n已有想法：[粘贴要点]\n约束：[时间 / 预算 / 人员]\n请先补齐关键缺失信息。请输出：目标、范围边界、里程碑、资源、风险与验收标准。",
                templateEn:
                    "Turn these notes into an actionable project proposal\nGoal: [desired outcome]\nRaw notes: [paste bullets]\nConstraints: [time / budget / team]\nAsk for missing info first. Output: goals, scope, milestones, resources, risks, acceptance criteria.",
            },
            {
                text: "审查合同并列出谈判要点",
                textEn: "Review a contract and draft negotiation points",
                desc: "按风险等级给可改条款",
                descEn: "Rank risks and propose edits",
                icon: "contract",
                template:
                    "审查这份合同并列出谈判要点\n我方角色：[甲方 / 乙方]\n重点关注：[付款 / 交付 / 违约 / IP]\n合同内容：[粘贴关键条款或说明已附件]\n请输出：高/中/低风险表、原因、修改方向、可直接发给对方的话术。",
                templateEn:
                    "Review this contract and draft negotiation points\nOur role: [buyer / vendor]\nFocus: [payment / delivery / liability / IP]\nContract content: [paste key clauses or note attachment]\nOutput: risk table, reasons, edit directions, and wording I can send.",
            },
            {
                text: "制定季度经营复盘",
                textEn: "Create a quarterly business review",
                desc: "指标、原因、下季动作",
                descEn: "Metrics, causes, next-quarter actions",
                icon: "strategy",
                template:
                    "帮我做季度经营复盘\n业务范围：[产品线 / 团队 / 区域]\n周期：[如 2026 Q2]\n关键指标与变化：[收入、转化等 + 升降]\n请输出：结论摘要、指标拆解、原因假设、风险机会、下季重点动作、待补数据。",
                templateEn:
                    "Create a quarterly business review\nScope: [product / team / region]\nPeriod: [e.g. 2026 Q2]\nKey metrics & changes: [revenue, conversion + up/down]\nOutput: summary, metric breakdown, cause hypotheses, risks/opportunities, next-quarter actions, missing data.",
            },
            {
                text: "拆解一个增长目标",
                textEn: "Break down a growth target",
                desc: "指标树、杠杆与实验",
                descEn: "Metric tree, levers, experiments",
                icon: "chart",
                template:
                    "帮我拆解增长目标\n总目标：[如月收入 +20%]\n当前基线：[关键数字]\n可用资源：[渠道 / 预算 / 人力]\n请输出：指标树、关键杠杆、优先级、实验计划、看板指标、周复盘节奏。",
                templateEn:
                    "Break down a growth target\nOverall target: [e.g. +20% monthly revenue]\nBaseline: [key numbers]\nResources: [channels / budget / people]\nOutput: metric tree, levers, priority, experiment plan, dashboard metrics, weekly review cadence.",
            },
            {
                text: "梳理商业汇报大纲",
                textEn: "Create a business presentation outline",
                desc: "主线、页级结构、证据",
                descEn: "Narrative, slide outline, evidence",
                icon: "ppt",
                template:
                    "帮我梳理商业汇报大纲\n汇报主题：[主题]\n汇报对象：[老板 / 客户 / 投资人]\n核心诉求：[希望对方决定或支持什么]\n请输出：主线叙事、页级大纲、每页核心信息、需准备的数据证据、可能被追问的问题。",
                templateEn:
                    "Create a business presentation outline\nTopic: [topic]\nAudience: [leadership / customer / investor]\nMain ask: [decision or support needed]\nOutput: narrative, slide-by-slide outline, key message per slide, evidence needed, likely questions.",
            },
            {
                text: "分析客户流失与续约",
                textEn: "Analyze churn and renewal risk",
                desc: "原因分层、挽留与动作",
                descEn: "Cause layers, save plays, actions",
                icon: "users",
                template:
                    "分析客户流失与续约风险\n业务与客户类型：[SaaS/项目制 · 客群]\n流失/续约现状：[率、金额、时间窗口]\n已知信号：[投诉、用量下降、竞品…]\n请输出：原因分层、风险客户画像、挽留动作优先级、话术要点、需补数据与看板指标。",
                templateEn:
                    "Analyze churn and renewal risk\nBusiness & segment: [SaaS/project · segment]\nChurn/renewal status: [rate, revenue, window]\nKnown signals: [tickets, usage drop, competitor…]\nOutput: cause layers, risk profiles, save-play priority, talk tracks, missing data and dashboard metrics.",
            },
            {
                text: "设计定价与包装方案",
                textEn: "Design pricing and packaging",
                desc: "档位、价值锚、迁移路径",
                descEn: "Tiers, anchors, migration path",
                icon: "strategy",
                template:
                    "设计产品定价与包装方案\n产品与目标客群：[名称 · 客群]\n成本/竞品与目标：[成本结构 · 竞品价 · 收入目标]\n约束：[不能降配 / 渠道折扣等]\n请输出：档位与权益、价格锚点、升降级路径、异议应对、试运行与验证指标。",
                templateEn:
                    "Design pricing and packaging\nProduct & segment: [name · audience]\nCost/competitors & goal: [cost · competitor prices · revenue goal]\nConstraints: [no downgrade / channel discount…]\nOutput: tiers & entitlements, price anchors, up/down paths, objection handling, pilot and validation metrics.",
            },
        ],
    },
    {
        id: "dev",
        label: "软件开发",
        labelEn: "Development",
        // 2-column grid is row-major: even = local, odd = remote.
        prompts: [
            {
                text: "按需求实现功能",
                textEn: "Implement a feature",
                desc: "读代码、改实现、跑验收",
                descEn: "Read code, implement, verify",
                icon: "code",
                agentMode: "coding_dev",
                template:
                    "按需求在本地项目实现功能\n需求描述：[一句话目标 + 关键点]\n验收标准：[怎样算完成]\n约束：[兼容接口 / 不大重构 / 其他]\n请先阅读项目结构与现有风格，再实现。请输出：实现摘要、改动文件、检查/测试结果。",
                templateEn:
                    "Implement a feature in this local project\nRequirement: [goal + key points]\nAcceptance: [what done looks like]\nConstraints: [API compatible / no large refactor / other]\nInspect structure and style first, then implement. Output: summary, files touched, check/test results.",
            },
            {
                text: "在远程开发新项目",
                textEn: "Develop a new project remotely",
                desc: "初始化、实现、远端验收",
                descEn: "Scaffold, implement, verify",
                icon: "install",
                agentMode: "remote_coding_dev",
                // Workdir/SSH are collected in the param dialog; ask only for project intent here.
                // Hints stay free-form (no / · 、) so the param dialog does not invent junk chips.
                template:
                    "在远程工作目录下开发新项目\n项目目标：[一句话目标与使用场景]\n技术栈与项目名：[如 Go 与 my-cli]\n验收标准：[怎样算编译运行通过]\n请先检查远端环境与工作目录，再初始化并实现；勿覆盖已有重要文件。请输出：项目结构、关键命令、验收结果。",
                templateEn:
                    "Develop a new project under the remote workdir\nGoal: [goal and audience]\nStack & project name: [e.g. Go my-cli]\nAcceptance: [build or run criteria]\nInspect remote env and workdir first, then scaffold and implement; do not overwrite important existing files. Output: project layout, key commands, verification results.",
            },
            {
                text: "修复一个 Bug",
                textEn: "Fix a bug",
                desc: "复现、定位、防回归",
                descEn: "Repro, fix, prevent regression",
                icon: "search",
                agentMode: "coding_dev",
                template:
                    "在本地项目修复一个 Bug\n现象与期望：[实际 vs 应该]\n复现步骤：[1. 2. 3. 或写偶发]\n补充线索：[日志 / 文件位置 / 最近改动]\n请输出：根因、最小修复 diff 说明、防回归测试建议；并实际完成修复。",
                templateEn:
                    "Fix a bug in this local project\nActual vs expected: [what happens vs should]\nRepro: [steps, or intermittent]\nClues: [logs / files / recent changes]\nOutput: root cause, minimal fix notes, regression-test plan; then apply the fix.",
            },
            {
                text: "在服务器热修代码",
                textEn: "Hotfix on the server",
                desc: "小步改远端并验证",
                descEn: "Small remote edits, verify",
                icon: "server",
                agentMode: "remote_coding_dev",
                template:
                    "在远程服务器上热修代码\n要修的问题：[一句话]\n改动范围：[尽量小的文件/服务]\n如何验证：[接口 / 页面 / 日志]\n请先确认远端现状并做最小改动。请输出：改动说明、验证结果、回滚方式。",
                templateEn:
                    "Hotfix code on the remote server\nProblem: [one line]\nChange scope: [small files/service]\nHow to verify: [API / page / log]\nInspect remote state, apply a minimal change, verify. Output: change notes, verification, rollback.",
            },
            {
                text: "代码审查与改进",
                textEn: "Review and improve code",
                desc: "找问题，改高价值项",
                descEn: "Find issues, fix high-value ones",
                icon: "review",
                agentMode: "coding_dev",
                template:
                    "对本地代码做审查并改进\n关注范围：[目录 / 模块 / 最近 diff]\n关注点：[正确性 / 安全 / 性能 / 可维护性]\n优先小改动。请输出：按严重程度的问题列表、已修复项、剩余风险与验证方式。",
                templateEn:
                    "Review and improve local code\nScope: [dirs / modules / recent diff]\nFocus: [correctness / security / performance / maintainability]\nPrefer small changes. Output: issues by severity, fixes applied, residual risks, verification.",
            },
            {
                text: "发布更新并验证",
                textEn: "Release and verify",
                desc: "发布步骤、检查、可回滚",
                descEn: "Rollout, health checks, rollback",
                icon: "deploy",
                agentMode: "remote_coding_dev",
                template:
                    "在远程环境发布更新并验证\n发布内容：[版本 / 配置 / 镜像说明]\n目标服务与环境：[服务名 · 测试/预发/生产]\n限制：[可否短暂停服 / 时间窗口]\n请先核对远端现状。请输出：发布与回滚步骤、健康检查结果、异常与建议。",
                templateEn:
                    "Release an update on the remote host and verify\nRelease content: [version / config / image notes]\nService & env: [name · test/staging/prod]\nConstraints: [brief downtime OK? window]\nCheck remote state first. Output: rollout/rollback steps, health-check results, issues and next steps.",
            },
            {
                text: "补测试或小步重构",
                textEn: "Add tests or small refactor",
                desc: "提高可维护性，行为不变",
                descEn: "Improve maintainability, keep behavior",
                icon: "checklist",
                agentMode: "coding_dev",
                template:
                    "在本地项目补测试或小步重构\n目标：[补测试 / 拆模块 / 去重复]\n范围与痛点：[文件或功能 · 为何难改]\n约束：对外行为与接口不变；改动小步可回退\n请对齐现有风格后执行。请输出：改动摘要、测试结果、剩余风险。",
                templateEn:
                    "Add tests or small-step refactor in this local project\nGoal: [more tests / split modules / dedupe]\nScope & pain: [files/feature · why hard]\nConstraints: keep external behavior; small reversible steps\nMatch existing style, then execute. Output: change summary, test results, residual risks.",
            },
            {
                text: "分析远程日志与性能",
                textEn: "Analyze remote logs/perf",
                desc: "慢请求、报错、资源异常",
                descEn: "Slow APIs, errors, resource issues",
                icon: "monitor",
                agentMode: "remote_coding_dev",
                template:
                    "分析远程环境的日志与性能\n现象：[慢接口 / 错误增多 / CPU 内存异常]\n时间与服务：[大概何时 · 服务名]\n已有材料：[日志片段 / 监控说明，可空]\n请先只读排查。请输出：根因假设、验证命令、影响面、建议的下一步（改配置/代码前需说明）。",
                templateEn:
                    "Analyze logs and performance on the remote host\nSymptom: [slow APIs / error spike / CPU-memory]\nWhen & service: [approx time · service name]\nMaterials: [log snippets / monitor notes, optional]\nStart read-only. Output: hypotheses, validation commands, impact surface, recommended next steps (state impact before changes).",
            },
        ],
    },
    {
        id: "ops",
        label: "运维排障",
        labelEn: "Ops troubleshooting",
        prompts: [
            {
                text: "排查服务器磁盘占满",
                textEn: "Investigate a full disk incident",
                desc: "只读诊断 → 安全清理",
                descEn: "Read-only first, then safe cleanup",
                icon: "server",
                template:
                    "帮我排查服务器磁盘占满\n系统与访问：[发行版 · SSH/已有会话]\n症状：[告警路径 / 服务异常]\n限制：[不能停服 / 先只读 / 维护窗口]\n请先只读诊断。请输出：占用排行、清理方案、风险、预计释放空间与回滚。",
                templateEn:
                    "Investigate a full disk incident\nOS & access: [distro · SSH/existing session]\nSymptoms: [path alert / service failure]\nConstraints: [no downtime / read-only first / window]\nStart read-only. Output: top consumers, cleanup plan, risk, space recovered, rollback.",
            },
            {
                text: "分析服务启动失败",
                textEn: "Analyze why a service fails to start",
                desc: "日志归因到最小修复",
                descEn: "Logs → root cause → minimal fix",
                icon: "install",
                template:
                    "分析服务为何启动失败\n服务与部署：[服务名 · systemd/Docker/K8s]\n最近变更：[发布 / 配置 / 证书 / 依赖]\n关键日志：[粘贴片段]\n请输出：可能根因排序、验证命令、最小修复、应避免的高风险操作。",
                templateEn:
                    "Analyze why this service fails to start\nService & deploy: [name · systemd/Docker/K8s]\nRecent changes: [release / config / cert / deps]\nKey logs: [paste snippets]\nOutput: ranked causes, validation commands, minimal fix, risky ops to avoid.",
            },
            {
                text: "排查接口超时或 5xx",
                textEn: "Investigate API timeout or 5xx",
                desc: "链路、依赖、止血与根治",
                descEn: "Trace path, mitigate, fix root cause",
                icon: "bug",
                template:
                    "排查接口超时或 5xx\n接口与环境：[路径 · 测试/生产]\n时间与日志：[发生时段 · 粘贴关键日志]\n最近变更：[发布 / 配置 / 依赖]\n请输出：排查顺序、查询命令、可能根因、临时止血、长期修复。",
                templateEn:
                    "Investigate API timeout or 5xx\nEndpoint & env: [path · test/prod]\nWhen & logs: [window · paste key logs]\nRecent changes: [release / config / deps]\nOutput: investigation order, commands, likely causes, mitigation, long-term fix.",
            },
            {
                text: "设计监控和告警规则",
                textEn: "Design monitoring and alerts",
                desc: "指标、阈值、升级路径",
                descEn: "Metrics, thresholds, escalation",
                icon: "monitor",
                template:
                    "设计服务监控和告警\n服务与部署：[名称 · 部署方式]\n关键依赖与业务影响：[DB/缓存/队列 · 谁受影响]\n现有监控栈：[Prometheus/云监控/无 · 告警渠道]\n请输出：监控指标、阈值与分级、通知文案、排查入口、升级路径、降噪建议。",
                templateEn:
                    "Design service monitoring and alerts\nService & deploy: [name · how deployed]\nDeps & impact: [DB/cache/queue · who is hit]\nCurrent stack: [Prometheus/cloud/none · alert channels]\nOutput: metrics, thresholds/severity, notification copy, triage entry, escalation, noise reduction.",
            },
            {
                text: "梳理发布回滚预案",
                textEn: "Prepare a release rollback plan",
                desc: "步骤、验证点、回退条件",
                descEn: "Rollout, checks, rollback triggers",
                icon: "deploy",
                template:
                    "梳理发布回滚预案\n发布内容与影响：[版本/功能 · 服务/用户]\n回滚方式：[镜像 / 配置 / 开关 / 数据]\n请输出：发布前检查、发布步骤、验证点、暂停条件、回滚步骤、通知话术、责任分工。",
                templateEn:
                    "Prepare a release rollback plan\nRelease & impact: [version/feature · services/users]\nRollback method: [image / config / flag / data]\nOutput: preflight, rollout steps, validation, stop conditions, rollback, notification copy, ownership.",
            },
            {
                text: "制定安全加固检查清单",
                textEn: "Create a security hardening checklist",
                desc: "账号、端口、权限、日志",
                descEn: "Accounts, ports, permissions, logs",
                icon: "shield",
                template:
                    "制定安全加固检查清单\n系统类型与环境：[Linux/Windows/DB · 内网/公网]\n已知风险或合规：[等保/行业规范/无 · 如有写明]\n变更窗口与限制：[可否改防火墙 · 维护窗口]\n请输出：检查项、风险等级、验证命令、加固建议、回滚注意、需人工确认项。",
                templateEn:
                    "Create a security hardening checklist\nSystem & environment: [Linux/Windows/DB · intranet/public]\nKnown risks or compliance: [baseline/industry/none · note if any]\nChange window & limits: [firewall changes OK? · maintenance window]\nOutput: checks, risk level, validation commands, hardening tips, rollback notes, human-confirm items.",
            },
            {
                text: "排查网络连通与 DNS",
                textEn: "Troubleshoot network and DNS",
                desc: "链路、解析、证书与防火墙",
                descEn: "Path, DNS, TLS, firewall",
                icon: "diagram",
                template:
                    "排查网络连通与 DNS 问题\n现象：[超时 / 间歇失败 / 证书错误]\n路径与环境：[源→目标 · 内网/公网 · 端口]\n已做检查：[ping/curl/解析结果，可空]\n请输出：只读排查顺序、命令清单、可能根因、临时绕过、持久修复与回滚注意。",
                templateEn:
                    "Troubleshoot network connectivity and DNS\nSymptom: [timeout / intermittent / cert error]\nPath & env: [src→dst · intranet/public · port]\nAlready tried: [ping/curl/DNS notes, optional]\nOutput: read-only order, commands, likely causes, temporary bypass, lasting fix and rollback notes.",
            },
            {
                text: "做容量规划与扩缩容",
                textEn: "Plan capacity and scaling",
                desc: "水位、瓶颈、扩缩步骤",
                descEn: "Headroom, bottlenecks, scale steps",
                icon: "monitor",
                template:
                    "做容量规划与扩缩容建议\n服务与指标水位：[服务 · CPU/内存/QPS/延迟]\n增长预期与峰值：[活动/流量 · 时间窗口]\n约束：[预算 / 变更窗口 / 有状态组件]\n请输出：瓶颈判断、容量模型假设、扩缩方案与验证、告警阈值、风险与回退。",
                templateEn:
                    "Plan capacity and scaling\nService & headroom: [service · CPU/mem/QPS/latency]\nGrowth & peaks: [campaign/traffic · window]\nConstraints: [budget / change window / stateful parts]\nOutput: bottleneck read, capacity assumptions, scale plan + validation, alert thresholds, risks and rollback.",
            },
        ],
    },
    {
        id: "research",
        label: "科研资料",
        labelEn: "Research",
        prompts: [
            {
                text: "做一份带出处的资料综述",
                textEn: "Create a sourced research brief",
                desc: "区分事实、观点与未验证",
                descEn: "Separate facts, opinions, unknowns",
                icon: "search",
                template:
                    "围绕主题做带出处的资料综述\n主题与范围：[主题 · 学科/时间/对象]\n用途：[选题 / 汇报 / 文章]\n已有资料：[粘贴或说明附件，可空]\n请输出：核心结论、证据表、观点对比、未验证项、追问清单；重要结论标来源，缺来源写「需补证据」。",
                templateEn:
                    "Create a sourced research brief\nTopic & scope: [topic · field/time/object]\nUse case: [selection / report / article]\nMaterials: [paste or note attachment, optional]\nOutput: conclusions, evidence table, viewpoint contrast, unverified items, follow-ups; cite sources or mark evidence needed.",
            },
            {
                text: "翻译并润色专业文档",
                textEn: "Translate and polish a technical document",
                desc: "保留术语、结构与语气",
                descEn: "Keep terms, structure, tone",
                icon: "translate",
                template:
                    "翻译并润色专业文档\n目标语言与读者：[中/英 · 技术/客户/管理层]\n术语要求：[保留英文 / 中英对照]\n文档内容：[粘贴或说明已附件]\n请先识别结构与术语。请输出：译文、术语表、需人工确认的歧义点。",
                templateEn:
                    "Translate and polish a technical document\nLanguage & audience: [zh/en · eng/customer/leadership]\nTerminology: [keep EN / bilingual glossary]\nDocument: [paste or note attachment]\nIdentify structure and terms first. Output: translation, glossary, ambiguities needing confirmation.",
            },
            {
                text: "整理实验数据分析报告",
                textEn: "Create an experiment data analysis report",
                desc: "口径、统计思路与结论",
                descEn: "Definitions, stats approach, findings",
                icon: "chart",
                template:
                    "把实验数据整理成分析报告\n实验设计与指标：[分组/样本量 · 主次指标]\n数据说明：[文件名或已附件 · 字段要点]\n统计方法：[t 检验/ANOVA/回归/不确定]\n请输出：数据质量、指标口径、分析思路、主要发现、异常、建议图表与结论；方法不确定时先说明选项。",
                templateEn:
                    "Create an experiment data analysis report\nDesign & metrics: [groups/n · primary/secondary]\nData notes: [filename or attachment · key fields]\nStats method: [t-test/ANOVA/regression/unknown]\nOutput: quality checks, definitions, approach, findings, anomalies, chart ideas, conclusions; explain method options if unclear.",
            },
            {
                text: "梳理论文选题和创新点",
                textEn: "Shape a paper topic and novelty claims",
                desc: "问题、方法、贡献对齐",
                descEn: "Align problem, method, contribution",
                icon: "write",
                template:
                    "梳理论文选题与创新点\n研究方向与想法：[方向 · 粘贴要点]\n已有基础：[数据/方法/实验/论文]\n目标期刊/会议：[如有]\n请输出：候选题目、科学问题、创新点、方法路线、实验设计、潜在质疑、待补文献；勿夸大贡献。",
                templateEn:
                    "Shape a paper topic and novelty claims\nDirection & ideas: [field · paste bullets]\nFoundation: [data/method/experiments/papers]\nTarget venue: [optional]\nOutput: titles, research question, novelty, method route, experiments, objections, literature gaps; do not overstate.",
            },
            {
                text: "做一份文献精读笔记",
                textEn: "Create a close-reading note for a paper",
                desc: "问题、方法、证据、局限",
                descEn: "Problem, method, evidence, limits",
                icon: "knowledge",
                template:
                    "精读这篇论文并做笔记\n论文来源：[PDF/链接或已附件]\n我的方向与关注点：[研究方向 · 方法/实验/理论]\n请输出：一句话贡献、背景、方法拆解、关键证据、局限、可借鉴点、与我方向关联、追问问题。",
                templateEn:
                    "Create a close-reading note for this paper\nPaper source: [PDF/link or attachment]\nMy direction & focus: [field · method/experiment/theory]\nOutput: one-line contribution, context, method breakdown, evidence, limits, takeaways, relevance, follow-up questions.",
            },
            {
                text: "生成投稿审稿回复",
                textEn: "Draft a reviewer response letter",
                desc: "逐条回应，克制有证据",
                descEn: "Point-by-point, measured, evidenced",
                icon: "mail",
                template:
                    "生成投稿审稿回复\n审稿意见：[粘贴意见]\n已做修改：[改了什么]\n目标期刊/会议：[如有]\n语气：礼貌、克制、逐条、有证据。请输出：总体回复、逐条回复表、需补实验/引用处、措辞风险。",
                templateEn:
                    "Draft a reviewer response letter\nReviewer comments: [paste]\nRevisions made: [what changed]\nTarget venue: [optional]\nTone: polite, measured, point-by-point, evidenced. Output: general reply, response table, gaps, wording risks.",
            },
            {
                text: "设计实验与方法方案",
                textEn: "Design an experiment and method plan",
                desc: "变量、对照、可复现",
                descEn: "Variables, controls, reproducibility",
                icon: "checklist",
                template:
                    "设计实验与方法方案\n研究问题与假设：[问题 · 假设]\n可用材料与设备：[样本/试剂/仪器 · 限制]\n成功标准：[如何算验证成功]\n请输出：实验设计、变量与对照、步骤与记录表、统计思路、失败预案、伦理/安全注意；勿编造结果。",
                templateEn:
                    "Design an experiment and method plan\nQuestion & hypothesis: [question · hypothesis]\nMaterials & equipment: [samples/reagents/instruments · limits]\nSuccess criteria: [what proves it]\nOutput: design, variables/controls, steps & log sheet, stats approach, failure plan, ethics/safety. Do not invent results.",
            },
            {
                text: "整理参考文献与引用",
                textEn: "Organize references and citations",
                desc: "去重、格式、缺口",
                descEn: "Dedupe, style, gaps",
                icon: "form",
                template:
                    "整理参考文献与引用格式\n引用格式：[APA / GB/T 7714 / 期刊自定义]\n现有文献列表：[粘贴 Bib/列表]\n文章中的引用点：[粘贴需挂引用的句子，可空]\n请输出：规范化文献表、去重与疑似错误、缺页码/DOI 项、文中引用建议；不确定处标注待核。",
                templateEn:
                    "Organize references and citation style\nStyle: [APA / GB/T 7714 / journal custom]\nCurrent list: [paste Bib/list]\nIn-text claims needing cites: [paste sentences, optional]\nOutput: normalized list, dedupe/errors, missing page/DOI, in-text cite suggestions; mark uncertain items.",
            },
        ],
    },
    {
        id: "academic-application",
        label: "科研申报",
        labelEn: "Academic applications",
        prompts: [
            {
                text: "基金申请书预审",
                textEn: "Pre-review a grant proposal",
                desc: "找短板、改摘要与创新点",
                descEn: "Find gaps, rewrite summary & novelty",
                icon: "award",
                template:
                    "预审这份基金申请书\n项目类型与学科：[面上/青年/重点… · 学科代码]\n研究基础：[代表成果与条件]\n申请书正文：[粘贴或说明已附件]\n请按评审标准检查。请输出：总评、扣分点、证据缺口、摘要改写、创新点改写、技术路线建议、模拟评审意见。不编造成果数据。",
                templateEn:
                    "Pre-review this grant proposal\nType & field: [General/Young/Key… · code]\nFoundation: [achievements & conditions]\nProposal text: [paste or note attachment]\nEvaluate by reviewer criteria. Output: assessment, weaknesses, evidence gaps, rewritten abstract/novelty, route tips, simulated comments. Do not invent data.",
            },
            {
                text: "国家优青材料打磨",
                textEn: "Polish NSFC Excellent Young materials",
                desc: "潜力、独立贡献、成长轨迹",
                descEn: "Potential, independence, growth path",
                icon: "award",
                template:
                    "打磨国家优青申报材料\n学科与题目：[方向/代码 · 题目关键词]\n代表成果与独立贡献：[列表]\n材料正文：[粘贴或已附件]\n请以评审视角审阅，不编造成果。请输出：申报主线、成长轨迹、成果排序、独立贡献表述、创新点与计划框架、证据缺口与质疑回应。",
                templateEn:
                    "Polish NSFC Excellent Young Scientist materials\nField & title: [code · keywords]\nAchievements & independence: [list]\nMaterials: [paste or attachment]\nReview as evaluator; do not invent results. Output: narrative, growth path, ranking, independence wording, novelty/plan outline, evidence gaps and rebuttals.",
            },
            {
                text: "杰青/重点项目计划打磨",
                textEn: "Polish key / Distinguished Young plan",
                desc: "科学问题与五年路线",
                descEn: "Scientific question and multi-year plan",
                icon: "award",
                template:
                    "打磨杰青或重点类研究计划\n项目类型：[杰青 / 重点研发 / 其他]\n核心科学问题：[如有请写]\n前期基础与五年目标：[成果 · 突破点]\n现有草稿：[粘贴或已附件]\n请判断「问题-积累-突破-计划」是否成主线。请输出：凝练科学问题、创新点、年度里程碑、风险备选、薄弱点。不编造数据。",
                templateEn:
                    "Polish a key R&D or Distinguished Young research plan\nType: [DY / key R&D / other]\nCore question: [if any]\nFoundation & multi-year goals: [achievements · breakthroughs]\nDraft: [paste or attachment]\nJudge if question-foundation-breakthrough-plan forms one arc. Output: refined question, novelty, milestones, risks, weak spots. No invented data.",
            },
            {
                text: "人才项目个人陈述",
                textEn: "Polish a talent-program personal statement",
                desc: "定位、贡献、平台匹配",
                descEn: "Positioning, contribution, platform fit",
                icon: "write",
                template:
                    "打磨人才项目个人陈述\n项目类型：[优青/杰青/长江/海外优青/其他]\n经历与代表成果：[粘贴简历要点]\n目标单位/平台：[如有]\n请输出：个人定位、成长主线、核心贡献、平台匹配、陈述改写版、证据缺口。勿编造成果。",
                templateEn:
                    "Polish a talent-program personal statement\nProgram: [EY/DY/Changjiang/Overseas/other]\nBackground & achievements: [paste CV bullets]\nTarget institution: [optional]\nOutput: positioning, growth arc, key contributions, platform fit, rewritten statement, evidence gaps. Do not invent achievements.",
            },
            {
                text: "摘要和立项依据改写",
                textEn: "Rewrite abstract and rationale",
                desc: "压实价值、创新、可行性",
                descEn: "Sharpen value, novelty, feasibility",
                icon: "review",
                template:
                    "改写申报书摘要与立项依据\n项目类型：[基金/人才/校内…]\n现有摘要与立项依据：[粘贴内容]\n研究基础：[代表成果]\n请输出：问题诊断、摘要改写、立项依据结构、创新点表述、可行性支撑、需补引用/数据处。",
                templateEn:
                    "Rewrite proposal abstract and rationale\nProposal type: [grant/talent/internal…]\nCurrent abstract & rationale: [paste]\nResearch foundation: [achievements]\nOutput: diagnosis, rewritten abstract, rationale structure, novelty wording, feasibility support, citation/data gaps.",
            },
            {
                text: "申报材料证据链检查",
                textEn: "Check evidence chain in application materials",
                desc: "主张与附件是否闭环",
                descEn: "Claims closed with evidence",
                icon: "checklist",
                template:
                    "检查申报材料证据链\n重点主张：[影响 / 原创 / 团队基础等]\n材料与附件：[粘贴要点或目录]\n请输出：主张-证据对应表、缺证据项、表述过强处、附件补充建议、提交前检查清单。",
                templateEn:
                    "Check evidence chain in application materials\nKey claims: [impact / originality / team…]\nMaterials & appendices: [paste notes or list]\nOutput: claim-evidence map, missing evidence, overclaims, appendix suggestions, pre-submission checklist.",
            },
            {
                text: "编制研究预算与进度",
                textEn: "Build research budget and timeline",
                desc: "科目、里程碑、合规注意",
                descEn: "Line items, milestones, compliance",
                icon: "schedule",
                template:
                    "编制研究预算与进度计划\n项目类型与周期：[基金/横向 · 起止年月]\n主要任务与已有资源：[任务分解 · 设备/人员]\n预算上限或科目约束：[总额或科目比例，可空]\n请输出：年度/季度里程碑、任务-资源表、预算科目拆分、风险缓冲、合规注意；勿编造不可核实单价。",
                templateEn:
                    "Build a research budget and timeline\nType & period: [grant/contract · start-end]\nTasks & resources: [WBS · equipment/people]\nBudget cap or category rules: [total or ratios, optional]\nOutput: milestones, task-resource table, budget breakdown, contingency, compliance notes. Do not invent unverifiable unit prices.",
            },
            {
                text: "中期检查与结题框架",
                textEn: "Draft midterm / final report framework",
                desc: "完成度、成果、偏差说明",
                descEn: "Progress, outputs, variance notes",
                icon: "review",
                template:
                    "写中期检查或结题报告框架\n报告类型：[中期 / 结题]\n任务书指标与已完成：[指标 · 完成情况]\n代表性成果与问题：[论文/专利/样机 · 偏差原因]\n请输出：结构大纲、各节要点、指标完成表、偏差与整改、下一步计划；不编造成果数据。",
                templateEn:
                    "Draft a midterm or final report framework\nType: [midterm / final]\nContract metrics & status: [metrics · done so far]\nOutputs & issues: [papers/patents/prototype · variance causes]\nOutput: outline, section bullets, metric table, variance & remediation, next plan. Do not invent results.",
            },
        ],
    },
    {
        id: "writing",
        label: "写作沟通",
        labelEn: "Writing",
        prompts: [
            {
                text: "把要点写成正式汇报",
                textEn: "Turn bullets into an executive update",
                desc: "结论、风险、待决策",
                descEn: "Conclusion, risks, decisions needed",
                icon: "write",
                template:
                    "把要点写成正式汇报\n汇报对象与长度：[老板/客户 · 一页/邮件/发言稿]\n原始要点：[粘贴内容]\n请输出：标题、核心结论、进展、风险、需对方决策或支持的事项；避免空话。",
                templateEn:
                    "Turn bullets into an executive update\nAudience & length: [leadership/client · page/email/talking points]\nRaw bullets: [paste]\nOutput: title, key conclusion, progress, risks, decisions/support needed. Avoid filler.",
            },
            {
                text: "起草客户沟通邮件",
                textEn: "Draft a client email",
                desc: "目的清晰、下一步明确",
                descEn: "Clear purpose and next step",
                icon: "mail",
                template:
                    "起草客户沟通邮件\n目的与下一步：[通知/催办/确认… · 希望客户做什么]\n关键事实：[事实要点]\n语气：[礼貌明确 / 稳妥 / 强硬]\n请输出：主题、正文、更短版本，并标出易误解表述。",
                templateEn:
                    "Draft a client email\nPurpose & next step: [inform/follow-up/confirm… · desired action]\nKey facts: [facts]\nTone: [polite-clear / careful / firm]\nOutput: subject, body, shorter variant, and phrases that may confuse.",
            },
            {
                text: "整理会议纪要和行动项",
                textEn: "Create meeting notes and action items",
                desc: "结论、责任人、截止时间",
                descEn: "Decisions, owners, due dates",
                icon: "meeting",
                template:
                    "整理会议纪要与行动项\n会议主题与参会人：[主题 · 名单]\n会议记录：[粘贴转写或要点]\n请输出：结论、关键讨论、行动项表（事项/责任人/截止/依赖）、待确认问题、适合发群的精简版。",
                templateEn:
                    "Create meeting notes and action items\nTopic & participants: [topic · names]\nNotes/transcript: [paste]\nOutput: decisions, discussion points, action table (task/owner/due/deps), open questions, short chat version.",
            },
            {
                text: "改写提升说服力",
                textEn: "Rewrite copy to be more persuasive",
                desc: "保留事实，优化结构语气",
                descEn: "Keep facts, improve structure & tone",
                icon: "write",
                template:
                    "改写内容以提升说服力\n目标读者与目的：[对象 · 争取支持/解释风险/推动行动]\n原文：[粘贴内容]\n请输出：改写版、关键改动说明、更短版本、可能误解的句子。",
                templateEn:
                    "Rewrite copy to be more persuasive\nAudience & goal: [who · gain support/explain risk/drive action]\nOriginal: [paste]\nOutput: rewrite, edit rationale, shorter variant, ambiguous sentences.",
            },
            {
                text: "写一份项目周报",
                textEn: "Write a project weekly update",
                desc: "进展、风险、下周计划",
                descEn: "Progress, risks, next week",
                icon: "schedule",
                template:
                    "写项目周报\n项目与对象：[名称 · 老板/客户/团队]\n进展 / 风险 / 下周：[三条分点粘贴]\n请输出：简洁周报、风险说明、需支持事项、发群短版。",
                templateEn:
                    "Write a project weekly update\nProject & audience: [name · leadership/client/team]\nProgress / risks / next week: [paste three bullet groups]\nOutput: concise update, risk notes, support needed, short chat version.",
            },
            {
                text: "生成多版本表达",
                textEn: "Generate multiple wording variants",
                desc: "正式/简短/委婉/有力度",
                descEn: "Formal, short, diplomatic, firm",
                icon: "spark",
                template:
                    "把同一意思写成多个表达版本\n原始意思：[粘贴]\n场景与对象：[邮件/微信/汇报 · 客户/领导/同事]\n请输出：正式、简短、委婉、有力度、口语五版，并注明适用场景。",
                templateEn:
                    "Generate multiple wording variants\nOriginal meaning: [paste]\nScenario & audience: [email/chat/report · client/boss/peer]\nOutput: formal, short, diplomatic, firm, conversational versions with usage notes.",
            },
            {
                text: "起草危机沟通口径",
                textEn: "Draft crisis communication talking points",
                desc: "事实、责任边界、下一步",
                descEn: "Facts, boundaries, next steps",
                icon: "shield",
                template:
                    "起草危机/负面事件沟通口径\n事件与受众：[发生了什么 · 客户/公众/内部]\n已知事实与未知：[已确认 · 仍在查]\n目标语气：[坦诚 / 克制 / 坚定]\n请输出：一句话立场、对外口径、对内口径、禁止表述、FAQ、升级与更新节奏。",
                templateEn:
                    "Draft crisis communication talking points\nEvent & audience: [what happened · customers/public/internal]\nKnown vs unknown: [confirmed · still checking]\nTone: [candid / measured / firm]\nOutput: one-line stance, external copy, internal copy, do-not-say list, FAQ, update cadence.",
            },
            {
                text: "写岗位说明与面试提纲",
                textEn: "Write a job JD and interview outline",
                desc: "职责、要求、题库分层",
                descEn: "Scope, requirements, leveled questions",
                icon: "users",
                template:
                    "写岗位说明（JD）与面试提纲\n岗位与级别：[名称 · 初级/中级/高级]\n团队痛点与必须能力：[缺什么 · 硬技能]\n加分项与红线：[加分项 · 不可接受]\n请输出：JD 正文、成功画像、面试环节、分级题库、考察点与评分提示。",
                templateEn:
                    "Write a job description (JD) and interview outline\nRole & level: [title · junior/mid/senior]\nTeam pain & must-haves: [gaps · hard skills]\nNice-to-haves & red lines: [bonus · dealbreakers]\nOutput: JD body, success profile, interview stages, leveled questions, scoring cues.",
            },
        ],
    },
    {
        id: "knowledge",
        label: "知识文档",
        labelEn: "Knowledge docs",
        prompts: [
            {
                text: "把项目资料整理成知识库",
                textEn: "Organize materials into a knowledge base",
                desc: "目录、摘要、标签、缺口",
                descEn: "Outline, summaries, tags, gaps",
                icon: "knowledge",
                template:
                    "把项目资料整理成知识库结构\n目标读者与用途：[新人/实施/客服 · 培训/检索/交接]\n资料说明：[路径或粘贴要点]\n请输出：目录、每篇摘要、标签、冲突/重复、缺失清单与补齐优先级。",
                templateEn:
                    "Organize project materials into a knowledge base\nAudience & use: [new hire/impl/support · training/search/handoff]\nMaterials: [path or paste notes]\nOutput: outline, per-article summary, tags, conflicts/dupes, missing list and priority.",
            },
            {
                text: "生成产品 FAQ",
                textEn: "Create product FAQ and standard answers",
                desc: "标准答、边界、升级条件",
                descEn: "Answers, limits, escalation",
                icon: "qa",
                template:
                    "基于资料生成产品 FAQ\n目标用户与场景：[客户/客服 · 售前/排障/使用]\n产品资料：[粘贴或已附件]\n请输出：问题分类、FAQ 表、标准回答、不能承诺的边界、需转人工条件。",
                templateEn:
                    "Create product FAQ from materials\nUsers & scenarios: [customer/support · pre-sales/troubleshoot/how-to]\nMaterials: [paste or attachment]\nOutput: categories, FAQ table, standard answers, promise boundaries, human-escalation rules.",
            },
            {
                text: "把流程写成 SOP",
                textEn: "Turn a process into an SOP",
                desc: "步骤、检查点、异常处理",
                descEn: "Steps, checks, exception handling",
                icon: "checklist",
                template:
                    "把操作流程写成 SOP\n流程名称与执行人：[名称 · 角色]\n原始步骤与风险：[粘贴流程 · 已知风险]\n请输出：范围、前置条件、逐步操作、检查点、异常处理、完成标准；步骤要可执行。",
                templateEn:
                    "Turn this process into an SOP\nName & operator: [name · role]\nRaw steps & risks: [paste · known risks]\nOutput: scope, prerequisites, steps, checkpoints, exceptions, done criteria. Steps must be executable.",
            },
            {
                text: "整理新人上手指南",
                textEn: "Create an onboarding guide",
                desc: "环境、路径、常见坑",
                descEn: "Setup, path, common pitfalls",
                icon: "knowledge",
                template:
                    "整理新人上手指南\n新人角色与目标：[研发/实施/客服 · 几天内能独立做什么]\n资料来源：[文档/仓库/流程要点]\n请输出：背景、必读、环境准备、第一周路径、FAQ、术语表、导师确认节点。",
                templateEn:
                    "Create an onboarding guide\nRole & goal: [eng/impl/support · what to handle independently]\nSources: [docs/repo/process notes]\nOutput: background, required reading, setup, first-week path, FAQ, glossary, mentor checkpoints.",
            },
            {
                text: "提取资料中的关键信息",
                textEn: "Extract key information from materials",
                desc: "长文档 → 结构化表",
                descEn: "Long docs → structured table",
                icon: "search",
                template:
                    "从资料中提取关键信息\n提取目标：[合同条款/需求/参数/任务]\n需要的字段：[字段列表]\n资料：[粘贴或已附件]\n请输出：结构化表、原文依据、缺失字段、歧义与待确认项。",
                templateEn:
                    "Extract key information from materials\nTarget: [clauses/needs/params/tasks]\nFields: [field list]\nMaterials: [paste or attachment]\nOutput: structured table, source evidence, missing fields, ambiguities, confirmations.",
            },
            {
                text: "制作培训材料大纲",
                textEn: "Create a training material outline",
                desc: "目标、案例、练习",
                descEn: "Goals, cases, exercises",
                icon: "ppt",
                template:
                    "制作培训材料大纲\n主题与学员：[主题 · 角色与基础]\n培训目标：[学完会做什么]\n已有资料：[可空]\n请输出：课程大纲、每节目标、案例、练习、讲师备注、课后检查方式。",
                templateEn:
                    "Create a training material outline\nTopic & learners: [topic · roles/baseline]\nGoal: [what they can do after]\nExisting materials: [optional]\nOutput: outline, section goals, cases, exercises, instructor notes, post checks.",
            },
            {
                text: "写版本更新说明",
                textEn: "Write release notes",
                desc: "用户可读、风险与迁移",
                descEn: "User-facing, risks, migration",
                icon: "deploy",
                template:
                    "写版本更新说明（Release Notes）\n版本与对象：[版本号 · 终端用户/实施/内部]\n变更清单：[新功能 / 修复 / 破坏性变更]\n已知问题与迁移：[如有]\n请输出：对外更新说明、对内技术说明、升级步骤、风险与回滚、FAQ。",
                templateEn:
                    "Write release notes\nVersion & audience: [version · end users/impl/internal]\nChange list: [features / fixes / breaking]\nKnown issues & migration: [if any]\nOutput: external notes, internal technical notes, upgrade steps, risks/rollback, FAQ.",
            },
            {
                text: "写故障复盘报告",
                textEn: "Write an incident postmortem",
                desc: "时间线、根因、改进项",
                descEn: "Timeline, root cause, actions",
                icon: "review",
                template:
                    "写故障复盘报告\n事件与影响：[时间 · 用户/收入影响]\n时间线与处置：[检测→止血→恢复]\n已有结论：[如有根因假设]\n请输出：摘要、时间线、根因（5 Whys）、影响评估、做得好/不好、可落地改进项与责任人建议；不甩锅。",
                templateEn:
                    "Write an incident postmortem\nEvent & impact: [when · user/revenue impact]\nTimeline & response: [detect→mitigate→recover]\nKnown conclusions: [root-cause hypotheses if any]\nOutput: summary, timeline, root cause (5 Whys), impact, what went well/poorly, actionable follow-ups with owners. No blame.",
            },
        ],
    },
    {
        id: "automation",
        label: "自动化流程",
        labelEn: "Automation",
        prompts: [
            {
                text: "设计重复任务自动化",
                textEn: "Design automation for a repetitive task",
                desc: "可自动边界与人工确认点",
                descEn: "Automatable vs human-confirm steps",
                icon: "workflow",
                template:
                    "设计重复任务自动化方案\n当前任务与触发：[做什么 · 何时开始]\n输入/输出与限制：[来源 · 结果 · 权限/人工确认]\n请输出：流程文字版、可自动步骤、必须人工确认步骤、工具/接口、失败重试与审计。",
                templateEn:
                    "Design automation for a repetitive task\nTask & trigger: [what · when]\nI/O & constraints: [inputs · outputs · permissions/approval]\nOutput: text workflow, automatable steps, human-confirm steps, tools/APIs, retry and audit plan.",
            },
            {
                text: "把表单收集变成工作流",
                textEn: "Turn form intake into a workflow",
                desc: "字段、分派、审批",
                descEn: "Fields, routing, approval",
                icon: "form",
                template:
                    "把表单收集设计成工作流\n业务场景与提交人：[报销/需求/线索 · 谁提交]\n字段与处理人：[已知字段 · 审批角色]\n请输出：字段清单、校验规则、阶段与状态、通知文案、异常分支。",
                templateEn:
                    "Turn form intake into a workflow\nScenario & submitter: [expense/request/lead · who]\nFields & processors: [known fields · approver roles]\nOutput: field list, validation, stages/status, notification copy, exception branches.",
            },
            {
                text: "制定定时巡检计划",
                textEn: "Create a scheduled check and reminder plan",
                desc: "频率、阈值、升级",
                descEn: "Cadence, thresholds, escalation",
                icon: "schedule",
                template:
                    "制定定时巡检与提醒计划\n巡检对象与频率：[系统/数据/合同 · 每天/每周]\n异常条件与通知：[阈值 · 负责人/群]\n请输出：巡检清单、提醒文案、异常分级、升级路径、记录字段。",
                templateEn:
                    "Create a scheduled check and reminder plan\nTarget & frequency: [system/data/contract · daily/weekly]\nException & notify: [rules · owner/group]\nOutput: checklist, reminder copy, severity levels, escalation, record fields.",
            },
            {
                text: "把人工流程画成图",
                textEn: "Turn a manual process into a flow diagram",
                desc: "角色、分支、可简化点",
                descEn: "Roles, branches, simplify targets",
                icon: "diagram",
                template:
                    "把人工流程整理成流程图\n参与角色与起止：[角色 · 开始/完成条件]\n流程描述：[粘贴现有流程]\n请输出：流程图文字版、泳道、分支条件、异常处理、可自动化或可简化环节。",
                templateEn:
                    "Turn a manual process into a flow diagram\nRoles & start/end: [roles · start/done conditions]\nProcess description: [paste current flow]\nOutput: text diagram, swimlanes, branches, exceptions, automatable/simplifiable steps.",
            },
            {
                text: "设计跨系统数据同步",
                textEn: "Design cross-system data sync",
                desc: "映射、冲突、补偿",
                descEn: "Mapping, conflicts, compensation",
                icon: "workflow",
                template:
                    "设计跨系统数据同步\n源/目标与对象：[系统 A → B · 客户/订单…]\n频率与映射：[实时/定时 · 已知字段]\n请输出：同步流程、字段映射、冲突与幂等、失败补偿、监控与审计。",
                templateEn:
                    "Design cross-system data sync\nSource/target & object: [A → B · customer/order…]\nFrequency & mapping: [real-time/scheduled · known fields]\nOutput: sync flow, field map, conflict/idempotency, compensation, monitoring and audit.",
            },
            {
                text: "优化一个现有流程",
                textEn: "Optimize an existing workflow",
                desc: "找瓶颈与改造优先级",
                descEn: "Bottlenecks and change priority",
                icon: "review",
                template:
                    "优化现有流程\n主要痛点：[耗时 / 返工 / 等待 / 出错]\n当前流程：[粘贴]\n成功标准：[例如周期缩短 30% / 差错率下降]\n请输出：瓶颈分析、可删/并/自动化节点、改造优先级、改造后流程、验证指标。",
                templateEn:
                    "Optimize an existing workflow\nMain pains: [delay / rework / waiting / errors]\nCurrent flow: [paste]\nSuccess criteria: [e.g. 30% faster cycle / fewer errors]\nOutput: bottleneck analysis, remove/merge/automate targets, priority, redesigned flow, validation metrics.",
            },
            {
                text: "设计审批流与 SLA",
                textEn: "Design approval flow and SLA",
                desc: "节点、时限、超时升级",
                descEn: "Nodes, deadlines, escalation",
                icon: "schedule",
                template:
                    "设计审批流与 SLA\n业务场景与金额/风险分级：[报销/采购/权限 · 分级规则]\n参与角色：[申请人 / 主管 / 财务…]\n时限要求：[例如 24h 内]\n请输出：流程图文字版、节点与条件、SLA 与超时升级、通知文案、异常与加签、审计字段。",
                templateEn:
                    "Design approval flow and SLA\nScenario & risk tiers: [expense/purchase/access · tier rules]\nRoles: [requester / manager / finance…]\nDeadlines: [e.g. within 24h]\nOutput: text flow, nodes/conditions, SLA & escalation, notification copy, exceptions/add-approver, audit fields.",
            },
            {
                text: "设计事件驱动集成",
                textEn: "Design event-driven integration",
                desc: "事件、重试、幂等与死信",
                descEn: "Events, retry, idempotency, DLQ",
                icon: "workflow",
                template:
                    "设计事件驱动/Webhook 集成方案\n源系统与目标动作：[系统 A 事件 · 系统 B 要做什么]\n可靠性要求：[至少一次 / 顺序 / 延迟上限]\n鉴权与敏感字段：[Token/签名 · 需脱敏字段]\n请输出：事件清单与 payload、触发与过滤、重试/死信、幂等键、监控告警、失败人工入口。",
                templateEn:
                    "Design event-driven / webhook integration\nSource & target action: [system A event · what B must do]\nReliability: [at-least-once / ordering / max latency]\nAuth & sensitive fields: [token/signature · fields to redact]\nOutput: event list & payload, trigger/filter, retry/DLQ, idempotency key, monitoring, human fallback.",
            },
        ],
    },
    {
        id: "data",
        label: "数据表格",
        labelEn: "Data tables",
        prompts: [
            {
                text: "清洗并规范一张表",
                textEn: "Clean and standardize a table",
                desc: "字段、格式、异常值",
                descEn: "Fields, formats, outliers",
                icon: "form",
                template:
                    "清洗并规范这张表\n用途：[导入系统 / 分析 / 对账]\n关键字段与已知问题：[字段 · 重复/缺失/格式乱]\n数据说明：[文件名或已附件]\n请输出：清洗规则、字段映射、异常清单、目标格式建议。",
                templateEn:
                    "Clean and standardize this table\nUse case: [import / analysis / reconciliation]\nKey fields & issues: [fields · dupes/missing/messy formats]\nData notes: [filename or attachment]\nOutput: cleaning rules, field map, anomaly list, target format tips.",
            },
            {
                text: "做经营数据周报",
                textEn: "Create a weekly business report",
                desc: "趋势、异常、动作建议",
                descEn: "Trends, anomalies, actions",
                icon: "ppt",
                template:
                    "根据数据做经营周报\n周期与核心指标：[日期范围 · 收入/订单/转化…]\n对比口径：[上周 / 目标]\n数据说明：[文件或已附件]\n请输出：摘要、指标表、趋势与异常假设、下周建议动作。",
                templateEn:
                    "Create a weekly business report from data\nPeriod & metrics: [date range · revenue/orders/conversion…]\nComparison: [last week / target]\nData notes: [file or attachment]\nOutput: summary, metrics table, trends/anomaly hypotheses, next-week actions.",
            },
            {
                text: "生成对账差异分析",
                textEn: "Generate reconciliation variance analysis",
                desc: "匹配规则与差异处理",
                descEn: "Match rules and variance handling",
                icon: "search",
                template:
                    "做对账差异分析\n匹配键与容差：[订单号/客户ID… · 金额或时间容差]\n两侧数据：[A 与 B 的文件说明或已附件]\n请输出：匹配规则、完全匹配与差异清单、原因分类、待人工确认项、处理建议。",
                templateEn:
                    "Generate reconciliation variance analysis\nMatch keys & tolerance: [order/customer id… · amount/time]\nData A & B: [file notes or attachments]\nOutput: match rules, exact matches & variance list, cause categories, human-confirm items, handling tips.",
            },
            {
                text: "设计看板指标",
                textEn: "Design dashboard metrics",
                desc: "口径、维度、图表",
                descEn: "Definitions, dimensions, charts",
                icon: "chart",
                template:
                    "设计一套看板指标\n业务场景与使用者：[销售/运营/产品 · 角色]\n要回答的决策问题：[看板帮判断什么]\n数据现状：[已有表/埋点 · 缺口]\n请输出：指标体系与口径、维度、刷新频率、推荐图表、异常阈值、布局建议。",
                templateEn:
                    "Design dashboard metrics\nScenario & users: [sales/ops/product · roles]\nDecision questions: [what should it help decide]\nData reality: [existing tables/events · gaps]\nOutput: metric system & definitions, dimensions, refresh, chart ideas, alert thresholds, layout.",
            },
            {
                text: "做漏斗转化分析",
                textEn: "Analyze funnel conversion",
                desc: "分步转化与掉点",
                descEn: "Step rates and drop-offs",
                icon: "target",
                template:
                    "做漏斗转化分析\n漏斗步骤：[访问→注册→下单…]\n时间与分组：[日期 · 渠道/产品等]\n数据说明：[文件或已附件]\n请输出：转化表、最大掉点、分组对比、原因假设、优化建议、待补数据。",
                templateEn:
                    "Analyze funnel conversion\nFunnel steps: [visit→signup→order…]\nTime & groups: [dates · channel/product…]\nData notes: [file or attachment]\nOutput: conversion table, largest drop-offs, segment compare, cause hypotheses, recommendations, missing data.",
            },
            {
                text: "生成数据字典",
                textEn: "Create a data dictionary",
                desc: "含义、类型、口径、来源",
                descEn: "Meaning, type, definition, source",
                icon: "form",
                template:
                    "生成数据字典\n业务场景：[导入/报表/建模]\n表或字段说明：[粘贴字段或文件说明]\n请输出：字段名、中文名、类型、业务含义、计算口径、来源、是否必填、示例、质量规则。",
                templateEn:
                    "Create a data dictionary\nUse case: [import/reporting/modeling]\nTable or field notes: [paste fields or file notes]\nOutput: name, display name, type, meaning, calculation, source, required, example, quality rules.",
            },
            {
                text: "做留存与同期群分析",
                textEn: "Analyze retention and cohorts",
                desc: "队列、留存曲线、动作",
                descEn: "Cohorts, curves, actions",
                icon: "users",
                template:
                    "做用户留存与同期群分析\n产品与事件定义：[激活/次日留存等口径]\n时间与分组：[日期范围 · 渠道/版本]\n数据说明：[文件或已附件]\n请输出：同期群表、留存曲线解读、异常队列、原因假设、改进动作、待补数据。",
                templateEn:
                    "Analyze retention and cohorts\nProduct & event defs: [activation/D1 retention definitions]\nTime & groups: [date range · channel/version]\nData notes: [file or attachment]\nOutput: cohort table, curve reading, outlier cohorts, cause hypotheses, actions, missing data.",
            },
            {
                text: "解读 A/B 实验结果",
                textEn: "Interpret an A/B test result",
                desc: "显著性、效应量、决策",
                descEn: "Significance, effect size, decision",
                icon: "target",
                template:
                    "解读 A/B 实验结果并给决策建议\n实验目标与指标：[主指标 · 护栏指标]\n样本量与结果：[各组 n · 转化/均值 · p 值/区间可空]\n业务约束：[上线成本 · 风险]\n请输出：结果是否可信、效应是否够用、决策建议（推全/再测/放弃）、常见误区与后续实验。",
                templateEn:
                    "Interpret an A/B test and recommend a decision\nGoal & metrics: [primary · guardrails]\nSample & results: [n per arm · rates/means · p/CI optional]\nBusiness constraints: [ship cost · risk]\nOutput: credibility, whether effect is enough, decision (ship/retest/stop), common pitfalls, next experiments.",
            },
        ],
    },
];

/**
 * The ops catalog is authored once for the local-machine context. Remote ops
 * uses the same investigation intent, but always goes through the SSH task
 * creation flow and starts with an evidence-only diagnosis turn.
 */
export function getWelcomeOpsPrompts(mode: WelcomeOpsMode): WelcomePrompt[] {
    const prompts = SCENARIO_TABS.find((tab) => tab.id === "ops")?.prompts || [];
    return prompts.map((prompt) => getWelcomeOpsPrompt(prompt, mode));
}

/** Apply the active ops execution context to one prompt from any entry point. */
export function getWelcomeOpsPrompt(prompt: WelcomePrompt, mode: WelcomeOpsMode): WelcomePrompt {
    if (mode === "local") return prompt;
    return {
        ...prompt,
        agentMode: "remote_coding_dev",
        remoteSafety: "diagnosis",
    };
}
