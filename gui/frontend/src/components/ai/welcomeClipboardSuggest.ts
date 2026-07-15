/**
 * Heuristic matching from clipboard text → welcome scenario tasks.
 * Pure functions; no DOM / clipboard API here.
 */

import { SCENARIO_TABS, type WelcomePrompt } from "./welcomeScenarioTasks";
import { welcomePromptKey } from "./welcomeTaskMemory";

export type WelcomeClipboardHit = {
    tabId: string;
    prompt: WelcomePrompt;
    key: string;
    score: number;
    /** Short Chinese reason chip. */
    reason: string;
    reasonEn: string;
};

type Rule = {
    id: string;
    reason: string;
    reasonEn: string;
    /** Weight added when the pattern matches. */
    weight: number;
    test: (text: string, lower: string) => boolean;
    /** Preferred tab ids (boost matching prompts in these tabs). */
    tabIds: string[];
    /** Optional textEn substrings to boost specific cards. */
    textEnIncludes?: string[];
};

const RULES: Rule[] = [
    {
        id: "stacktrace",
        reason: "异常日志",
        reasonEn: "Error log",
        weight: 12,
        test: (_t, lower) =>
            /traceback \(most recent call last\)|exception in thread|fatal error|panic:|java\.lang\.|at [\w.$]+\([\w.]+:\d+\)/.test(lower)
            || (
                /错误|异常|失败|error:|exception|stack trace/.test(lower)
                && /line \d+|:\d+:\d+|file "|\.go:\d+|\.ts:\d+|\.py:\d+/.test(lower)
            ),
        tabIds: ["ops", "dev"],
        // ops: fail/start/timeout; dev: Hotfix / logs / bug (incident card was replaced by greenfield)
        textEnIncludes: ["fail", "Hotfix", "log", "bug", "start", "timeout", "5xx"],
    },
    {
        id: "http_errors",
        reason: "接口故障",
        reasonEn: "API failure",
        weight: 10,
        test: (_t, lower) =>
            /\b(5\d\d|4\d\d)\b/.test(lower) && /(timeout|timed out|gateway|upstream|status|http|request|response|接口|超时)/.test(lower),
        tabIds: ["ops", "dev"],
        textEnIncludes: ["timeout", "5xx", "Hotfix", "log", "API"],
    },
    {
        id: "disk_ops",
        reason: "磁盘/运维",
        reasonEn: "Disk / ops",
        weight: 9,
        test: (_t, lower) =>
            /(no space left|disk (full|usage)|df -h|inode|空间不足|磁盘占满|清理磁盘)/.test(lower),
        tabIds: ["ops"],
        textEnIncludes: ["disk", "full", "backup", "security", "rollback"],
    },
    {
        id: "new_project",
        reason: "新建项目",
        reasonEn: "New project",
        weight: 9,
        test: (_t, lower) =>
            /(新建项目|开发新项目|初始化项目|从零(开始|搭建)|脚手架|create (a )?new (project|app|repo)|scaffold(ing)? (a )?(new )?(project|app)|greenfield|hello.?world app)/.test(lower),
        tabIds: ["dev"],
        textEnIncludes: ["new project", "Develop a new"],
    },
    {
        id: "code",
        reason: "代码片段",
        reasonEn: "Code snippet",
        weight: 8,
        test: (t, lower) =>
            /```[\s\S]{20,}/.test(t)
            || /\b(function|const|class|import |package |def |func |public class|fn )\b/.test(lower)
            || /\.(ts|tsx|js|go|py|java|rs|cpp)\b/.test(lower),
        tabIds: ["dev"],
        // First catalog match wins; "Implement" is intentional default for raw snippets.
        textEnIncludes: ["Implement", "Fix a bug", "Review", "refactor", "feature"],
    },
    {
        id: "contract",
        reason: "合同条款",
        reasonEn: "Contract",
        weight: 9,
        test: (_t, lower) =>
            /(甲方|乙方|违约责任|知识产权|保密条款|合同编号|whereas|hereinafter|liability|indemnif)/.test(lower),
        tabIds: ["business"],
        textEnIncludes: ["contract", "negotiation"],
    },
    {
        id: "table_data",
        reason: "表格数据",
        reasonEn: "Table data",
        weight: 8,
        test: (t, lower) => {
            const lines = t.split(/\r?\n/).filter((l) => l.trim()).slice(0, 12);
            const csvLike = lines.filter((l) => (l.match(/,/g) || []).length >= 2 || (l.match(/\t/g) || []).length >= 2).length;
            return csvLike >= 3
                || (/(收入|订单|转化|留存|cohort|revenue|conversion)/.test(lower) && csvLike >= 1);
        },
        tabIds: ["data", "business"],
        textEnIncludes: ["Clean", "weekly", "funnel", "retention", "reconcil", "dashboard", "dictionary"],
    },
    {
        id: "email",
        reason: "邮件沟通",
        reasonEn: "Email",
        weight: 7,
        test: (_t, lower) =>
            /^(subject|re:|fw:|转发|主题)[:：]/im.test(lower)
            || /(亲爱的|尊敬的|dear |hi team|best regards|此致|敬礼)/.test(lower),
        tabIds: ["writing"],
        textEnIncludes: ["email", "client", "persuasive", "variants"],
    },
    {
        id: "meeting",
        reason: "会议记录",
        reasonEn: "Meeting notes",
        weight: 7,
        test: (_t, lower) =>
            /(会议纪要|action items?|待办|参会|议题|主持人|下次会议)/.test(lower),
        tabIds: ["writing"],
        textEnIncludes: ["meeting", "action"],
    },
    {
        id: "research_paper",
        reason: "论文资料",
        reasonEn: "Research paper",
        weight: 8,
        test: (_t, lower) =>
            /\bdoi:\s*10\.|arxiv\.org|abstract[:：]|参考文献|参考文献|引用格式|关键词[:：]/.test(lower),
        tabIds: ["research", "academic-application"],
        textEnIncludes: ["paper", "reading", "reviewer", "sourced", "novelty", "proposal"],
    },
    {
        id: "grant",
        reason: "申报材料",
        reasonEn: "Grant materials",
        weight: 9,
        test: (_t, lower) =>
            /(国家自然科学基金|优青|杰青|长江学者|立项依据|研究内容|技术路线|预期成果|NSFC)/.test(lower),
        tabIds: ["academic-application", "research"],
        textEnIncludes: ["grant", "NSFC", "Excellent Young", "Distinguished", "Changjiang", "evidence", "abstract"],
    },
    {
        id: "sop_process",
        reason: "流程步骤",
        reasonEn: "Process steps",
        weight: 6,
        test: (_t, lower) =>
            /(第一步|第二步|step\s*1|step\s*2|操作流程|标准作业|SOP)/.test(lower),
        tabIds: ["knowledge", "automation"],
        textEnIncludes: ["SOP", "workflow", "onboarding", "automation"],
    },
];

function findPrompt(tabId: string, textEnIncludes: string[]): WelcomePrompt | null {
    const tab = SCENARIO_TABS.find((t) => t.id === tabId);
    if (!tab) return null;
    const lowered = textEnIncludes.map((s) => s.toLowerCase());
    for (const prompt of tab.prompts) {
        const hay = `${prompt.textEn} ${prompt.text} ${prompt.descEn} ${prompt.desc}`.toLowerCase();
        if (lowered.some((kw) => hay.includes(kw.toLowerCase()))) {
            return prompt;
        }
    }
    return tab.prompts[0] || null;
}

/**
 * Rank welcome tasks for clipboard content. Returns up to `limit` unique cards.
 */
export function matchWelcomeTasksFromClipboard(text: string, limit = 3): WelcomeClipboardHit[] {
    const trimmed = (text || "").trim();
    if (trimmed.length < 12) return [];
    // Cap analysis cost on huge pastes.
    const sample = trimmed.length > 8000 ? trimmed.slice(0, 8000) : trimmed;
    const lower = sample.toLowerCase();

    const hits = new Map<string, WelcomeClipboardHit>();

    for (const rule of RULES) {
        if (!rule.test(sample, lower)) continue;
        for (const tabId of rule.tabIds) {
            const prompt = findPrompt(tabId, rule.textEnIncludes || []);
            if (!prompt) continue;
            const key = welcomePromptKey(tabId, prompt.textEn);
            const prev = hits.get(key);
            const score = (prev?.score || 0) + rule.weight;
            if (!prev || score > prev.score) {
                hits.set(key, {
                    tabId,
                    prompt,
                    key,
                    score,
                    reason: rule.reason,
                    reasonEn: rule.reasonEn,
                });
            }
        }
    }

    return [...hits.values()]
        .sort((a, b) => b.score - a.score || a.key.localeCompare(b.key))
        .slice(0, Math.max(1, limit));
}

/** First template field that looks like a paste target (for clipboard prefill). */
export function pickClipboardPrefillLabel(template: string): string | null {
    if (!template) return null;
    const lines = template.split("\n");
    for (const line of lines) {
        const m = line.match(/^(.+?)\s*[：:]\s*\[([^\]]+)\]\s*$/);
        if (!m) continue;
        const label = m[1].trim();
        const hint = m[2].trim();
        if (/粘贴|日志|材料|内容|要点|意见|记录|转写|正文|paste|log|note|material|content|transcript/i.test(`${label} ${hint}`)) {
            return label;
        }
    }
    // Fall back to first labeled field.
    for (const line of lines) {
        const m = line.match(/^(.+?)\s*[：:]\s*\[([^\]]+)\]\s*$/);
        if (m) return m[1].trim();
    }
    return null;
}
