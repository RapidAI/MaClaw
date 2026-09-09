/**
 * Capability tiers for expert create/edit.
 *
 * Users pick a human-readable profile instead of raw tool names.
 * Tiers resolve against the live tool/skill catalog using category + risk rules
 * (not a brittle hard-coded name list), so new registry tools are classified
 * automatically via expertToolMeta / backend enrichment.
 *
 * Semantics (matches backend expert_session_policy):
 * - tools=[] and skills=[] → unrestricted
 * - non-empty list → allow-list
 */

import {
    resolveToolMeta,
    skillCategory,
    skillRisk,
    type SkillCategoryId,
    type ToolCatalogEntry,
    type ToolCategoryId,
    type ToolRisk,
} from './expertToolMeta';

export type CapabilityTierId = 'full' | 'advisor' | 'docs' | 'office' | 'custom';

export type CapabilityTierResolveResult = {
    tools: string[];
    skills: string[];
};

export type TierToolInput = string | ToolCatalogEntry;

type TierToolRule = {
    /** Highest risk allowed (safe < elevated < dangerous). */
    maxRisk: ToolRisk;
    /** Categories included in this tier. */
    categories: ToolCategoryId[];
};

type TierSkillRule = {
    maxRisk: ToolRisk;
    /** Empty → no skills selected (whitelist empty; skill gating depends on manage_skill). */
    categories: SkillCategoryId[];
};

const TIER_TOOL_RULES: Record<Exclude<CapabilityTierId, 'full' | 'custom'>, TierToolRule> = {
    // Chat-oriented: only low-risk interaction/media tools.
    advisor: {
        maxRisk: 'safe',
        categories: ['interaction', 'media'],
    },
    // Research / writing: files, web, knowledge, light office reads/writes — never system.
    docs: {
        maxRisk: 'elevated',
        categories: ['interaction', 'files', 'web', 'knowledge', 'office', 'media'],
    },
    // Office production: docs set + more media/automation helpers, still no system/privileged.
    office: {
        maxRisk: 'elevated',
        categories: ['interaction', 'files', 'web', 'knowledge', 'office', 'media', 'automation'],
    },
};

const TIER_SKILL_RULES: Record<Exclude<CapabilityTierId, 'full' | 'custom'>, TierSkillRule> = {
    advisor: { maxRisk: 'safe', categories: [] },
    docs: { maxRisk: 'elevated', categories: ['docs', 'security'] },
    office: { maxRisk: 'elevated', categories: ['docs', 'office', 'security'] },
};

/** Same-capability aliases so legacy candidate helpers still work in tests. */
const TOOL_ALIASES: Record<string, string[]> = {
    read_file: ['read_file', 'fs_read'],
    fs_read: ['read_file', 'fs_read'],
    write_file: ['write_file', 'fs_write'],
    fs_write: ['write_file', 'fs_write'],
};

function normalizeName(name: string): string {
    return String(name || '').trim().toLowerCase();
}

function riskRank(risk: ToolRisk): number {
    if (risk === 'safe') return 0;
    if (risk === 'elevated') return 1;
    return 2;
}

function riskAllowed(risk: ToolRisk, maxRisk: ToolRisk): boolean {
    return riskRank(risk) <= riskRank(maxRisk);
}

function asCatalogEntries(tools: TierToolInput[]): ToolCatalogEntry[] {
    return tools.map((t) => (typeof t === 'string' ? { name: String(t) } : t));
}

function toolMatchesCandidate(availableName: string, candidate: string): boolean {
    const a = normalizeName(availableName);
    const c = normalizeName(candidate);
    if (!a || !c) return false;
    if (a === c) return true;
    const family = TOOL_ALIASES[c] || [c];
    return family.some((x) => normalizeName(x) === a);
}

/** Pick available tools that match any candidate (preserving catalog order). */
export function pickToolsFromCandidates(candidates: string[], availableToolNames: string[]): string[] {
    if (!candidates.length) return [];
    if (!availableToolNames.length) {
        const seen = new Set<string>();
        const out: string[] = [];
        for (const c of candidates) {
            const n = String(c || '').trim();
            if (!n || seen.has(n)) continue;
            seen.add(n);
            out.push(n);
        }
        return out;
    }
    const out: string[] = [];
    const used = new Set<string>();
    for (const available of availableToolNames) {
        const name = String(available || '').trim();
        if (!name || used.has(name)) continue;
        if (candidates.some((c) => toolMatchesCandidate(name, c))) {
            used.add(name);
            out.push(name);
        }
    }
    return out;
}

/** Pick available skills whose names include any matcher substring. */
export function pickSkillsFromMatchers(matchers: string[], availableSkillNames: string[]): string[] {
    if (!matchers.length || !availableSkillNames.length) return [];
    const norms = matchers.map(normalizeName).filter(Boolean);
    const out: string[] = [];
    const used = new Set<string>();
    for (const skill of availableSkillNames) {
        const name = String(skill || '').trim();
        if (!name || used.has(name)) continue;
        const lower = normalizeName(name);
        if (norms.some((m) => lower.includes(m))) {
            used.add(name);
            out.push(name);
        }
    }
    return out;
}

function sortedCopy(list: string[]): string[] {
    return [...list].map((s) => String(s || '').trim()).filter(Boolean).sort((a, b) => a.localeCompare(b));
}

function sameSet(a: string[], b: string[]): boolean {
    const sa = sortedCopy(a);
    const sb = sortedCopy(b);
    if (sa.length !== sb.length) return false;
    return sa.every((v, i) => v === sb[i]);
}

function toolAllowedByRule(entry: ToolCatalogEntry, rule: TierToolRule): boolean {
    const meta = resolveToolMeta(entry);
    if (!riskAllowed(meta.risk, rule.maxRisk)) return false;
    return rule.categories.includes(meta.category);
}

function pickToolsByRule(rule: TierToolRule, availableTools: ToolCatalogEntry[]): string[] {
    const out: string[] = [];
    for (const entry of availableTools) {
        const name = String(entry.name || '').trim();
        if (!name) continue;
        if (toolAllowedByRule(entry, rule)) out.push(name);
    }
    return out;
}

function pickSkillsByRule(rule: TierSkillRule, availableSkillNames: string[]): string[] {
    if (!rule.categories.length || !availableSkillNames.length) return [];
    const out: string[] = [];
    for (const skill of availableSkillNames) {
        const name = String(skill || '').trim();
        if (!name) continue;
        if (!riskAllowed(skillRisk(name), rule.maxRisk)) continue;
        if (!rule.categories.includes(skillCategory(name))) continue;
        out.push(name);
    }
    return out;
}

/**
 * Resolve a non-custom tier into concrete allow-lists against the live catalog.
 * full → empty lists (unrestricted). custom → empty lists (caller keeps current).
 *
 * `availableTools` may be plain names or enriched catalog entries (preferred).
 */
export function resolveCapabilityTier(
    tierId: CapabilityTierId,
    availableTools: TierToolInput[],
    availableSkillNames: string[],
): CapabilityTierResolveResult {
    if (tierId === 'full' || tierId === 'custom') {
        return { tools: [], skills: [] };
    }
    const entries = asCatalogEntries(availableTools);
    // When the catalog has not loaded yet, fall back to empty allow-lists rather
    // than inventing names — applyTier re-runs once ListAvailableToolNames returns.
    if (!entries.length) {
        return { tools: [], skills: [] };
    }
    return {
        tools: pickToolsByRule(TIER_TOOL_RULES[tierId], entries),
        skills: pickSkillsByRule(TIER_SKILL_RULES[tierId], availableSkillNames),
    };
}

/**
 * Infer which tier best matches the current allow-lists.
 * Falls back to custom when the set is non-empty but matches no preset.
 */
export function inferCapabilityTier(
    tools: string[],
    skills: string[],
    availableTools: TierToolInput[],
    availableSkillNames: string[],
): CapabilityTierId {
    if (!tools.length && !skills.length) return 'full';
    for (const id of ['advisor', 'docs', 'office'] as const) {
        const resolved = resolveCapabilityTier(id, availableTools, availableSkillNames);
        if (sameSet(resolved.tools, tools) && sameSet(resolved.skills, skills)) {
            return id;
        }
    }
    return 'custom';
}

/** Count selected tools/skills marked dangerous (for UI warnings). */
export function countDangerousSelections(
    tools: string[],
    skills: string[],
    availableTools: TierToolInput[] = [],
): { tools: number; skills: number; total: number } {
    const byName = new Map<string, ToolCatalogEntry>();
    for (const entry of asCatalogEntries(availableTools)) {
        if (entry.name) byName.set(entry.name, entry);
    }
    let toolDanger = 0;
    for (const name of tools) {
        if (resolveToolMeta(byName.get(name) || name).risk === 'dangerous') toolDanger += 1;
    }
    let skillDanger = 0;
    for (const name of skills) {
        if (skillRisk(name) === 'dangerous') skillDanger += 1;
    }
    return { tools: toolDanger, skills: skillDanger, total: toolDanger + skillDanger };
}

export type CapabilityTierMeta = {
    id: CapabilityTierId;
    /** Display order (full first). */
    order: number;
};

export const CAPABILITY_TIER_ORDER: CapabilityTierId[] = [
    'full',
    'advisor',
    'docs',
    'office',
    'custom',
];

/** Lightweight role starters: set tier + optional seed fields for create mode. */
export type ExpertStarterTemplateId = 'blank' | 'writing' | 'research' | 'office';

export type ExpertStarterTemplate = {
    id: ExpertStarterTemplateId;
    tier: CapabilityTierId;
    icon: string;
    nameZh: string;
    nameEn: string;
    descZh: string;
    descEn: string;
    ideaZh: string;
    ideaEn: string;
};

export const EXPERT_STARTER_TEMPLATES: ExpertStarterTemplate[] = [
    {
        id: 'blank',
        tier: 'full',
        icon: '🤖',
        nameZh: '通用助手',
        nameEn: 'General assistant',
        descZh: '不限制能力，适合综合任务',
        descEn: 'Unrestricted capabilities for general tasks',
        ideaZh: '一个能帮我处理各类日常工作的通用助手',
        ideaEn: 'A general assistant for everyday work',
    },
    {
        id: 'writing',
        tier: 'docs',
        icon: '✍️',
        nameZh: '写作助手',
        nameEn: 'Writing assistant',
        descZh: '读写文档、检索资料，适合润色与翻译',
        descEn: 'Read/write docs and research — polish & translate',
        ideaZh: '帮我润色和改写文档，必要时查阅资料',
        ideaEn: 'Help polish and rewrite documents, research when needed',
    },
    {
        id: 'research',
        tier: 'docs',
        icon: '🔎',
        nameZh: '调研助手',
        nameEn: 'Research assistant',
        descZh: '网页检索与文档阅读，输出结构化结论',
        descEn: 'Web search and document reading with structured answers',
        ideaZh: '帮我调研一个主题并整理成要点',
        ideaEn: 'Research a topic and summarize key points',
    },
    {
        id: 'office',
        tier: 'office',
        icon: '📊',
        nameZh: '办公助手',
        nameEn: 'Office assistant',
        descZh: '文档/表格/PPT 等办公技能向',
        descEn: 'Docs, sheets, PPT and other office skills',
        ideaZh: '帮我处理办公文档、表格和演示文稿',
        ideaEn: 'Help with office documents, spreadsheets, and presentations',
    },
];
