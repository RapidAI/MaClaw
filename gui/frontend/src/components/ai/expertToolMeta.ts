/**
 * Human-facing metadata for expert allow-list pickers:
 * - category grouping
 * - Chinese labels
 * - risk level (safe / elevated / dangerous)
 *
 * Prefer fields returned by ListAvailableToolNames (category/risk/label_zh/label_en).
 * Local catalogs and prefix inference are fallbacks when backend fields are absent.
 */

export type ToolRisk = 'safe' | 'elevated' | 'dangerous';

export type ToolCategoryId =
    | 'interaction'
    | 'files'
    | 'web'
    | 'office'
    | 'media'
    | 'automation'
    | 'system'
    | 'knowledge'
    | 'other';

export type SkillCategoryId = 'office' | 'docs' | 'dev' | 'security' | 'other';

export type ToolMeta = {
    /** Stable id used for grouping (primary name). */
    id: string;
    /** Alternative registry names that share this meta. */
    aliases?: string[];
    category: ToolCategoryId;
    risk: ToolRisk;
    labelZh: string;
    labelEn: string;
};

/** Optional backend-enriched fields from ListAvailableToolNames. */
export type ToolCatalogEntry = {
    name: string;
    description?: string;
    deferred?: boolean;
    category?: string;
    risk?: string;
    label_zh?: string;
    label_en?: string;
};

export type SkillMetaRule = {
    /** Case-insensitive substring match against skill name. */
    match: string;
    category: SkillCategoryId;
    risk: ToolRisk;
    labelZh?: string;
    labelEn?: string;
};

const TOOL_META: ToolMeta[] = [
    // Interaction / session
    { id: 'memory', category: 'interaction', risk: 'safe', labelZh: '记忆', labelEn: 'Memory' },
    { id: 'ask_user', category: 'interaction', risk: 'safe', labelZh: '向用户提问', labelEn: 'Ask user' },
    { id: 'discover_tool', category: 'interaction', risk: 'safe', labelZh: '按需发现工具', labelEn: 'Discover tools' },
    { id: 'recommend_tool', category: 'interaction', risk: 'safe', labelZh: '推荐工具', labelEn: 'Recommend tool' },
    { id: 'session_search', category: 'interaction', risk: 'safe', labelZh: '会话检索', labelEn: 'Session search' },
    { id: 'set_nickname', category: 'interaction', risk: 'safe', labelZh: '设置昵称', labelEn: 'Set nickname' },
    { id: 'set_max_iterations', category: 'interaction', risk: 'elevated', labelZh: '设置最大轮次', labelEn: 'Set max iterations' },
    { id: 'switch_llm_provider', category: 'interaction', risk: 'elevated', labelZh: '切换模型服务商', labelEn: 'Switch LLM provider' },
    { id: 'manage_user_model', category: 'interaction', risk: 'elevated', labelZh: '管理用户模型', labelEn: 'Manage user model' },
    { id: 'read_tool_result', category: 'interaction', risk: 'safe', labelZh: '读取工具结果', labelEn: 'Read tool result' },
    { id: 'manage_skill', category: 'interaction', risk: 'elevated', labelZh: '管理/运行技能', labelEn: 'Manage skill' },
    { id: 'search_and_install_skill', category: 'interaction', risk: 'elevated', labelZh: '搜索并安装技能', labelEn: 'Search & install skill' },
    { id: 'manage_template', category: 'interaction', risk: 'safe', labelZh: '管理模板', labelEn: 'Manage template' },
    { id: 'manage_config', category: 'interaction', risk: 'elevated', labelZh: '管理配置', labelEn: 'Manage config' },
    { id: 'manage_schedule', category: 'interaction', risk: 'elevated', labelZh: '管理定时任务', labelEn: 'Manage schedule' },
    { id: 'im_message', category: 'interaction', risk: 'elevated', labelZh: 'IM 消息', labelEn: 'IM message' },

    // Files / code
    { id: 'read_file', aliases: ['fs_read'], category: 'files', risk: 'safe', labelZh: '读取文件', labelEn: 'Read file' },
    { id: 'FileRead', category: 'files', risk: 'safe', labelZh: '按行读文件', labelEn: 'File read' },
    { id: 'write_file', aliases: ['fs_write'], category: 'files', risk: 'elevated', labelZh: '写入文件', labelEn: 'Write file' },
    { id: 'edit_file', category: 'files', risk: 'elevated', labelZh: '编辑文件', labelEn: 'Edit file' },
    { id: 'list_directory', category: 'files', risk: 'safe', labelZh: '列出目录', labelEn: 'List directory' },
    { id: 'search_files', category: 'files', risk: 'safe', labelZh: '搜索文件', labelEn: 'Search files' },
    { id: 'ripgrep', category: 'files', risk: 'safe', labelZh: '代码搜索', labelEn: 'Ripgrep' },
    { id: 'Glob', category: 'files', risk: 'safe', labelZh: '文件匹配', labelEn: 'Glob' },
    { id: 'send_file', category: 'files', risk: 'elevated', labelZh: '发送文件', labelEn: 'Send file' },
    { id: 'open', category: 'files', risk: 'elevated', labelZh: '打开文件/路径', labelEn: 'Open path' },
    { id: 'office', category: 'files', risk: 'elevated', labelZh: '办公文件处理', labelEn: 'Office files' },
    { id: 'download_file', category: 'files', risk: 'elevated', labelZh: '下载文件', labelEn: 'Download file' },

    // Office
    { id: 'read_excel', category: 'office', risk: 'safe', labelZh: '读取表格', labelEn: 'Read Excel' },
    { id: 'write_excel', category: 'office', risk: 'elevated', labelZh: '写入表格', labelEn: 'Write Excel' },
    { id: 'read_pptx', category: 'office', risk: 'safe', labelZh: '读取 PPT', labelEn: 'Read PPTX' },

    // Web
    { id: 'web_search', category: 'web', risk: 'safe', labelZh: '网页搜索', labelEn: 'Web search' },
    { id: 'web_fetch', category: 'web', risk: 'elevated', labelZh: '抓取网页', labelEn: 'Fetch web page' },

    // Media
    { id: 'screenshot', category: 'media', risk: 'elevated', labelZh: '截屏', labelEn: 'Screenshot' },
    { id: 'record_audio', category: 'media', risk: 'elevated', labelZh: '录音', labelEn: 'Record audio' },
    { id: 'tts', category: 'media', risk: 'safe', labelZh: '语音播报', labelEn: 'Text-to-speech' },
    { id: 'asr', category: 'media', risk: 'safe', labelZh: '语音识别', labelEn: 'Speech recognition' },

    // Automation
    { id: 'task', category: 'automation', risk: 'elevated', labelZh: '任务', labelEn: 'Task' },
    { id: 'goal', category: 'automation', risk: 'elevated', labelZh: '长期目标', labelEn: 'Goal' },
    { id: 'parallel_execute', category: 'automation', risk: 'elevated', labelZh: '并行执行', labelEn: 'Parallel execute' },
    { id: 'passthrough_task', category: 'automation', risk: 'elevated', labelZh: '透传任务', labelEn: 'Passthrough task' },
    { id: 'project_manage', category: 'automation', risk: 'elevated', labelZh: '项目管理', labelEn: 'Project manage' },
    { id: 'send_to_im', category: 'automation', risk: 'elevated', labelZh: '发送到 IM', labelEn: 'Send to IM' },

    // System / high risk
    { id: 'send_input', category: 'system', risk: 'dangerous', labelZh: '发送键鼠输入', labelEn: 'Send input' },
    { id: 'ssh', category: 'system', risk: 'dangerous', labelZh: 'SSH 远程', labelEn: 'SSH' },
    { id: 'bash', category: 'system', risk: 'dangerous', labelZh: 'Shell 命令', labelEn: 'Bash / shell' },
    { id: 'query_audit_log', category: 'system', risk: 'elevated', labelZh: '查询审计日志', labelEn: 'Query audit log' },
    { id: 'mis_data', category: 'system', risk: 'elevated', labelZh: '业务数据', labelEn: 'Business data' },

    // Knowledge (prefix covers the long tail)
    { id: 'knowledge_search', category: 'knowledge', risk: 'safe', labelZh: '知识库搜索', labelEn: 'Knowledge search' },
    { id: 'knowledge_save_text', category: 'knowledge', risk: 'elevated', labelZh: '保存文本到知识库', labelEn: 'Save text to knowledge' },
    { id: 'knowledge_save_url', category: 'knowledge', risk: 'elevated', labelZh: '保存 URL 到知识库', labelEn: 'Save URL to knowledge' },
    { id: 'knowledge_import_files', category: 'knowledge', risk: 'elevated', labelZh: '导入文件到知识库', labelEn: 'Import files to knowledge' },
    { id: 'knowledge_export', category: 'knowledge', risk: 'elevated', labelZh: '导出知识库', labelEn: 'Export knowledge' },
];

const SKILL_META_RULES: SkillMetaRule[] = [
    { match: 'pptx', category: 'office', risk: 'elevated', labelZh: 'PPT 生成', labelEn: 'PPT generator' },
    { match: 'sheet', category: 'office', risk: 'elevated', labelZh: '表格分析', labelEn: 'Sheet analysis' },
    { match: 'excel', category: 'office', risk: 'elevated' },
    { match: 'pdf-word', category: 'docs', risk: 'elevated', labelZh: 'PDF/Word', labelEn: 'PDF/Word' },
    { match: 'pdf_word', category: 'docs', risk: 'elevated', labelZh: 'PDF/Word', labelEn: 'PDF/Word' },
    { match: 'paper', category: 'docs', risk: 'safe', labelZh: '论文相关', labelEn: 'Paper tools' },
    { match: 'translat', category: 'docs', risk: 'safe', labelZh: '翻译', labelEn: 'Translation' },
    { match: 'contract', category: 'docs', risk: 'elevated', labelZh: '合同审阅', labelEn: 'Contract review' },
    { match: 'doc-redact', category: 'security', risk: 'elevated', labelZh: '文档脱敏', labelEn: 'Doc redact' },
    { match: 'doc_redact', category: 'security', risk: 'elevated', labelZh: '文档脱敏', labelEn: 'Doc redact' },
    { match: 'ssh', category: 'security', risk: 'dangerous', labelZh: 'SSH 技能', labelEn: 'SSH skill' },
    { match: 'craft', category: 'dev', risk: 'elevated' },
    { match: 'code', category: 'dev', risk: 'elevated' },
    { match: 'agent', category: 'dev', risk: 'elevated' },
    { match: 'empty', category: 'other', risk: 'safe' },
];

const TOOL_CATEGORY_ORDER: ToolCategoryId[] = [
    'interaction',
    'files',
    'web',
    'office',
    'knowledge',
    'media',
    'automation',
    'system',
    'other',
];

const SKILL_CATEGORY_ORDER: SkillCategoryId[] = [
    'docs',
    'office',
    'dev',
    'security',
    'other',
];

const VALID_CATEGORIES = new Set<string>(TOOL_CATEGORY_ORDER);
const VALID_RISKS = new Set<string>(['safe', 'elevated', 'dangerous']);

const toolMetaByName = (() => {
    const map = new Map<string, ToolMeta>();
    for (const meta of TOOL_META) {
        map.set(meta.id.toLowerCase(), meta);
        for (const alias of meta.aliases || []) {
            map.set(alias.toLowerCase(), meta);
        }
    }
    return map;
})();

function normalize(name: string): string {
    return String(name || '').trim().toLowerCase();
}

function asCategory(value: string | undefined): ToolCategoryId | null {
    const v = String(value || '').trim().toLowerCase();
    return VALID_CATEGORIES.has(v) ? (v as ToolCategoryId) : null;
}

function asRisk(value: string | undefined): ToolRisk | null {
    const v = String(value || '').trim().toLowerCase();
    return VALID_RISKS.has(v) ? (v as ToolRisk) : null;
}

export function lookupToolMeta(name: string): ToolMeta | null {
    const key = normalize(name);
    if (!key) return null;
    return toolMetaByName.get(key) || null;
}

/** Infer category/risk for tools not in the static catalog (mirrors backend). */
export function inferToolMeta(name: string): Pick<ToolMeta, 'category' | 'risk' | 'labelZh' | 'labelEn'> {
    const lower = normalize(name);
    const base = {
        category: 'other' as ToolCategoryId,
        risk: 'elevated' as ToolRisk,
        labelZh: name,
        labelEn: name,
    };
    if (!lower) return base;
    if (lower.startsWith('knowledge_')) {
        const safe = /search|list|explain|stats|health|suggest|capabilities|doctor/.test(lower);
        return {
            category: 'knowledge',
            risk: safe ? 'safe' : 'elevated',
            labelZh: `知识库 · ${name}`,
            labelEn: `Knowledge · ${name}`,
        };
    }
    if (lower.startsWith('browser_')) {
        return { category: 'automation', risk: 'elevated', labelZh: `浏览器 · ${name}`, labelEn: `Browser · ${name}` };
    }
    if (lower.startsWith('gui_') || lower.startsWith('computer_')) {
        return { category: 'system', risk: 'dangerous', labelZh: `桌面控制 · ${name}`, labelEn: `Desktop control · ${name}` };
    }
    if (lower.includes('ssh') || lower === 'bash' || lower.includes('shell')) {
        return { category: 'system', risk: 'dangerous', labelZh: name, labelEn: name };
    }
    if (lower.includes('web_') || lower.includes('search')) {
        return { category: 'web', risk: 'safe', labelZh: name, labelEn: name };
    }
    if (/file|read|write|edit|glob|directory/.test(lower)) {
        const elevated = /write|edit|delete|download/.test(lower);
        return { category: 'files', risk: elevated ? 'elevated' : 'safe', labelZh: name, labelEn: name };
    }
    return base;
}

export function resolveToolMeta(entry: ToolCatalogEntry | string): {
    category: ToolCategoryId;
    risk: ToolRisk;
    labelZh: string;
    labelEn: string;
} {
    const name = typeof entry === 'string' ? entry : entry.name;
    const backendCat = typeof entry === 'string' ? null : asCategory(entry.category);
    const backendRisk = typeof entry === 'string' ? null : asRisk(entry.risk);
    const backendZh = typeof entry === 'string' ? '' : String(entry.label_zh || '').trim();
    const backendEn = typeof entry === 'string' ? '' : String(entry.label_en || '').trim();

    const local = lookupToolMeta(name);
    const inferred = local
        ? { category: local.category, risk: local.risk, labelZh: local.labelZh, labelEn: local.labelEn }
        : inferToolMeta(name);

    return {
        category: backendCat || inferred.category,
        risk: backendRisk || inferred.risk,
        labelZh: backendZh || inferred.labelZh,
        labelEn: backendEn || inferred.labelEn,
    };
}

export function toolRisk(name: string, entry?: ToolCatalogEntry): ToolRisk {
    return resolveToolMeta(entry || name).risk;
}

export function toolCategory(name: string, entry?: ToolCatalogEntry): ToolCategoryId {
    return resolveToolMeta(entry || name).category;
}

export function toolDisplayLabel(
    name: string,
    isZh: boolean,
    backendDescription?: string,
    entry?: ToolCatalogEntry,
): string {
    const resolved = resolveToolMeta(entry || { name, description: backendDescription });
    const label = isZh ? resolved.labelZh : resolved.labelEn;
    if (label && label !== name) return label;
    const desc = String(backendDescription || entry?.description || '').trim().replace(/\s+/g, ' ');
    if (desc && desc.length <= 28) return desc;
    return name;
}

export function lookupSkillRule(name: string): SkillMetaRule | null {
    const lower = normalize(name);
    if (!lower) return null;
    for (const rule of SKILL_META_RULES) {
        if (lower.includes(rule.match.toLowerCase())) return rule;
    }
    return null;
}

export function skillRisk(name: string): ToolRisk {
    return lookupSkillRule(name)?.risk || 'elevated';
}

export function skillCategory(name: string): SkillCategoryId {
    return lookupSkillRule(name)?.category || 'other';
}

export function skillDisplayLabel(name: string, isZh: boolean): string {
    const rule = lookupSkillRule(name);
    if (rule?.labelZh || rule?.labelEn) {
        return isZh ? (rule.labelZh || name) : (rule.labelEn || name);
    }
    return name;
}

export type ToolGroup = {
    category: ToolCategoryId;
    items: ToolCatalogEntry[];
};

export type SkillGroup = {
    category: SkillCategoryId;
    items: string[];
};

export function groupTools(tools: ToolCatalogEntry[]): ToolGroup[] {
    const buckets = new Map<ToolCategoryId, ToolCatalogEntry[]>();
    for (const cat of TOOL_CATEGORY_ORDER) buckets.set(cat, []);
    for (const tool of tools) {
        const cat = toolCategory(tool.name, tool);
        buckets.get(cat)!.push(tool);
    }
    return TOOL_CATEGORY_ORDER
        .map((category) => ({ category, items: buckets.get(category) || [] }))
        .filter((g) => g.items.length > 0);
}

export function groupSkills(skills: string[]): SkillGroup[] {
    const buckets = new Map<SkillCategoryId, string[]>();
    for (const cat of SKILL_CATEGORY_ORDER) buckets.set(cat, []);
    for (const skill of skills) {
        const cat = skillCategory(skill);
        buckets.get(cat)!.push(skill);
    }
    return SKILL_CATEGORY_ORDER
        .map((category) => ({ category, items: buckets.get(category) || [] }))
        .filter((g) => g.items.length > 0);
}

export function toolCategoryLabel(category: ToolCategoryId, isZh: boolean): string {
    const zh: Record<ToolCategoryId, string> = {
        interaction: '对话与会话',
        files: '文件与代码',
        web: '网络',
        office: '办公文档',
        knowledge: '知识库',
        media: '音视频/截屏',
        automation: '任务与自动化',
        system: '系统与高权限',
        other: '其他',
    };
    const en: Record<ToolCategoryId, string> = {
        interaction: 'Chat & session',
        files: 'Files & code',
        web: 'Web',
        office: 'Office docs',
        knowledge: 'Knowledge',
        media: 'Media',
        automation: 'Tasks & automation',
        system: 'System & privileged',
        other: 'Other',
    };
    return isZh ? zh[category] : en[category];
}

export function skillCategoryLabel(category: SkillCategoryId, isZh: boolean): string {
    const zh: Record<SkillCategoryId, string> = {
        docs: '文档',
        office: '办公产出',
        dev: '开发/编排',
        security: '安全相关',
        other: '其他',
    };
    const en: Record<SkillCategoryId, string> = {
        docs: 'Documents',
        office: 'Office output',
        dev: 'Dev / orchestration',
        security: 'Security',
        other: 'Other',
    };
    return isZh ? zh[category] : en[category];
}

export function riskLabel(risk: ToolRisk, isZh: boolean): string {
    if (risk === 'dangerous') return isZh ? '高风险' : 'High risk';
    if (risk === 'elevated') return isZh ? '需谨慎' : 'Elevated';
    return isZh ? '低风险' : 'Low risk';
}
