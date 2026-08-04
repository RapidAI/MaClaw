import { localizeText } from '../i18n';

export type SettingsTabId = 'general' | 'proxy' | 'ui' | 'display' | 'pet' | 'searchEngine' | 'redeem' | 'skills' | 'mcp' | 'llm' | 'llmCache' | 'embedding' | 'memory' | 'knowledge' | 'misData' | 'virtualEmployee' | 'im' | 'security' | 'migration' | 'system' | 'assetManagement';

/**
 * Tabs that actually render a settings body panel (rail + SettingsActiveContent).
 * Note: `skills` / `mcp` remain on SettingsTabId for legacy typing but are not content tabs.
 */
export const SETTINGS_CONTENT_TAB_IDS = [
    'general',
    'proxy',
    'ui',
    'display',
    'pet',
    'searchEngine',
    'redeem',
    'llm',
    'llmCache',
    'memory',
    'knowledge',
    'misData',
    'embedding',
    'virtualEmployee',
    'im',
    'security',
    'migration',
    'system',
    'assetManagement',
] as const satisfies readonly SettingsTabId[];

const settingsContentTabIdSet: ReadonlySet<string> = new Set(SETTINGS_CONTENT_TAB_IDS);

export function isSettingsContentTabId(id: string): id is SettingsTabId {
    return settingsContentTabIdSet.has(id);
}

/** Normalize an arbitrary tab id to a renderable settings tab. */
export function resolveSettingsTabId(
    id: string | undefined | null,
    options: { hideVirtualEmployee?: boolean } = {},
): SettingsTabId {
    const raw = String(id || 'general').trim();
    if (!isSettingsContentTabId(raw)) return 'general';
    if (options.hideVirtualEmployee && raw === 'virtualEmployee') return 'general';
    return raw;
}

/** Nav grouping for the settings rail. Tabs sharing a group render under one localized header. */
export type SettingsTabGroupId = 'essentials' | 'ai' | 'services' | 'data' | 'system';

export interface SettingsTabOption {
    id: SettingsTabId;
    label: string;
    desc: string;
    icon: string;
    group?: SettingsTabGroupId;
    groupLabel?: string;
}

const settingsTabGroupMeta: Record<SettingsTabGroupId, { en: string; zhHans: string; zhHant: string }> = {
    essentials: { en: 'Essentials', zhHans: '基础设置', zhHant: '基礎設定' },
    ai: { en: 'AI & Models', zhHans: 'AI 与模型', zhHant: 'AI 與模型' },
    services: { en: 'Services & Integrations', zhHans: '服务与集成', zhHant: '服務與整合' },
    data: { en: 'Data & Memory', zhHans: '数据与记忆', zhHant: '資料與記憶' },
    system: { en: 'System', zhHans: '系统', zhHant: '系統' },
};

/** Group assignment per tab id (skills/mcp are legacy ids and stay ungrouped). */
const settingsTabGroupById: Partial<Record<SettingsTabId, SettingsTabGroupId>> = {
    general: 'essentials',
    proxy: 'essentials',
    ui: 'essentials',
    display: 'essentials',
    pet: 'essentials',
    searchEngine: 'ai',
    llm: 'ai',
    llmCache: 'ai',
    embedding: 'ai',
    memory: 'data',
    knowledge: 'data',
    misData: 'data',
    migration: 'data',
    redeem: 'services',
    virtualEmployee: 'services',
    im: 'services',
    security: 'system',
    system: 'system',
    assetManagement: 'system',
};

const attachSettingsTabGroup = (lang: string, tab: SettingsTabOption): SettingsTabOption => {
    const group = settingsTabGroupById[tab.id];
    if (!group) return tab;
    const meta = settingsTabGroupMeta[group];
    return { ...tab, group, groupLabel: localizeText(lang, meta.en, meta.zhHans, meta.zhHant) };
};

/**
 * Minimalist SVG icons for each settings tab (16×16 viewBox, stroke-based).
 * Using currentColor so they adapt to the tab's text color automatically.
 */
const settingsTabIcons: Record<SettingsTabId, string> = {
    general: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><path d="M2 4h12M2 8h8M2 12h10"/><circle cx="13" cy="4" r="1.2" fill="currentColor" stroke="none"/><circle cx="11" cy="8" r="1.2" fill="currentColor" stroke="none"/><circle cx="13" cy="12" r="1.2" fill="currentColor" stroke="none"/></svg>',
    proxy: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><circle cx="4" cy="8" r="2"/><circle cx="12" cy="8" r="2"/><path d="M6 8h4"/></svg>',
    ui: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="12" height="10" rx="1.5"/><path d="M2 6h12"/><circle cx="4" cy="4.5" r="0.7" fill="currentColor" stroke="none"/><circle cx="6" cy="4.5" r="0.7" fill="currentColor" stroke="none"/></svg>',
    display: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="12" height="9" rx="1.5"/><path d="M8 12v2M5 14h6"/><path d="M5 7l2 1.5L5 10"/><path d="M9 10h3"/></svg>',
    pet: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="5" cy="4" rx="1.4" ry="2"/><ellipse cx="11" cy="4" rx="1.4" ry="2"/><ellipse cx="3" cy="8.5" rx="1.4" ry="1.8"/><ellipse cx="13" cy="8.5" rx="1.4" ry="1.8"/><ellipse cx="8" cy="11.5" rx="2.8" ry="2.2"/></svg>',
    searchEngine: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><circle cx="7" cy="7" r="4.5"/><path d="M10.2 10.2l3.3 3.3"/></svg>',
    redeem: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="12" height="9" rx="1.5"/><path d="M2 7.5h12"/><path d="M5.5 10.5h2"/></svg>',
    skills: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="2" width="10" height="12" rx="1.5"/><path d="M6 5h4M6 7.5h4M6 10h2"/></svg>',
    mcp: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="2" width="6" height="4" rx="1"/><rect x="5" y="10" width="6" height="4" rx="1"/><path d="M8 6v4"/><path d="M4 8h8"/></svg>',
    llm: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="10" height="10" rx="2"/><circle cx="6" cy="7" r="1"/><circle cx="10" cy="7" r="1"/><path d="M6 10.5c.5.6 1.2 1 2 1s1.5-.4 2-1"/></svg>',
    llmCache: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="8" cy="4" rx="5" ry="2"/><path d="M3 4v4c0 1.1 2.2 2 5 2s5-.9 5-2V4"/><path d="M3 8v4c0 1.1 2.2 2 5 2s5-.9 5-2V8"/></svg>',
    embedding: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="3.5" r="2"/><circle cx="4" cy="12" r="2"/><circle cx="12" cy="12" r="2"/><path d="M8 5.5v2.5L4 10M8 8l4 2"/></svg>',
    memory: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="2" width="9" height="12" rx="1.5"/><path d="M5.5 5h5M5.5 7.5h5M5.5 10h3"/><path d="M3.5 5H2M3.5 8H2M3.5 11H2M12.5 5H14M12.5 8H14M12.5 11H14"/></svg>',
    knowledge: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><path d="M2 2.5h5L8 3.5l1-1h5v10h-5l-1 1-1-1H2z"/><path d="M8 3.5v10"/></svg>',
    misData: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="5" height="5" rx="1"/><rect x="9" y="2" width="5" height="5" rx="1"/><rect x="2" y="9" width="5" height="5" rx="1"/><rect x="9" y="9" width="5" height="5" rx="1"/></svg>',
    virtualEmployee: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="5" r="3"/><path d="M3 14c0-2.8 2.2-5 5-5s5 2.2 5 5"/></svg>',
    im: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h12v8H9l-2 2v-2H2z" /><path d="M5 6.5h6M5 8.5h4"/></svg>',
    security: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><path d="M8 1.5L3 3.5v4c0 3.5 2.5 5.5 5 7 2.5-1.5 5-3.5 5-7v-4z"/></svg>',
    migration: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6l-2.5 2 2.5 2"/><path d="M12 6l2.5 2-2.5 2"/><path d="M1.5 8h5M9.5 8h5"/></svg>',
    system: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><path d="M6.8 1.5h2.4l.3 1.8.9.4 1.5-1 1.7 1.7-1 1.5.4.9 1.8.3v2.4l-1.8.3-.4.9 1 1.5-1.7 1.7-1.5-1-.9.4-.3 1.8H6.8l-.3-1.8-.9-.4-1.5 1-1.7-1.7 1-1.5-.4-.9-1.8-.3V6.8l1.8-.3.4-.9-1-1.5 1.7-1.7 1.5 1 .9-.4z"/><circle cx="8" cy="8" r="2"/></svg>',
    assetManagement: '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3.5" width="12" height="9" rx="1.5"/><path d="M2 6.5h12M5 10h3"/></svg>',
};

const textForLang = localizeText;

export const getSettingsTabOptions = (lang: string, options: { hideVirtualEmployee?: boolean } = {}): SettingsTabOption[] => {
    const tabs: SettingsTabOption[] = [
        {
            id: 'general' as const,
            label: textForLang(lang, 'General', '通用设置', '通用設定'),
            desc: textForLang(lang, 'Language, projects, and environment', '语言、项目与环境', '語言、專案與環境'),
            icon: settingsTabIcons.general,
        },
        {
            id: 'proxy' as const,
            label: textForLang(lang, 'Proxy', '代理设置', '代理設定'),
            desc: textForLang(lang, 'Global network proxy configuration', '全局网络代理配置', '全域網路代理設定'),
            icon: settingsTabIcons.proxy,
        },
        {
            id: 'ui' as const,
            label: textForLang(lang, 'UI Config', 'UI 配置', 'UI 配置'),
            desc: textForLang(lang, 'UI scaling and display behavior', '界面缩放与显示行为', '介面縮放與顯示行為'),
            icon: settingsTabIcons.ui,
        },
        {
            id: 'display' as const,
            label: textForLang(lang, 'Dev CLI', '编程工具', '程式工具'),
            desc: textForLang(lang, 'Tool visibility and startup behavior', '工具显示与启动页行为', '工具顯示與啟動頁行為'),
            icon: settingsTabIcons.display,
        },
        {
            id: 'pet' as const,
            label: textForLang(lang, 'Pet', '宠物', '寵物'),
            desc: textForLang(lang, 'Desktop pet appearance, actions, and interaction settings', '桌面宠物形象、动作与交互设置', '桌面寵物形象、動作與互動設定'),
            icon: settingsTabIcons.pet,
        },
        {
            id: 'searchEngine' as const,
            label: textForLang(lang, 'Search Engine', '搜索引擎', '搜尋引擎'),
            desc: textForLang(lang, 'Configure web search providers', '配置联网搜索引擎', '設定聯網搜尋引擎'),
            icon: settingsTabIcons.searchEngine,
        },
        {
            id: 'redeem' as const,
            label: textForLang(lang, 'Service Redeem', '服务兑换', '服務兌換'),
            desc: textForLang(lang, 'View credits and redeem service codes', '查看 Credits 和兑换服务码', '查看 Credits 和兌換服務碼'),
            icon: settingsTabIcons.redeem,
        },
        {
            id: 'llm' as const,
            label: textForLang(lang, 'LLM Config', '大模型配置', '大模型配置'),
            desc: textForLang(lang, 'Configure LLM for MaClaw agent', '配置 MaClaw 代理使用的大模型', '配置 MaClaw 代理使用的大模型'),
            icon: settingsTabIcons.llm,
        },
        {
            id: 'llmCache' as const,
            label: textForLang(lang, 'Cache Service', '\u7f13\u5b58\u670d\u52a1', '\u5feb\u53d6\u670d\u52d9'),
            desc: textForLang(lang, 'Local cache for OpenAI-compatible LLM requests', '\u672c\u5730\u7f13\u5b58 OpenAI \u517c\u5bb9 LLM \u8bf7\u6c42', '\u672c\u5730\u5feb\u53d6 OpenAI \u76f8\u5bb9 LLM \u8acb\u6c42'),
            icon: settingsTabIcons.llmCache,
        },
        {
            id: 'memory' as const,
            label: textForLang(lang, 'Memory', '记忆管理', '記憶管理'),
            desc: textForLang(lang, 'View, edit and manage MaClaw long-term memory', '查看、编辑和管理 MaClaw 的长期记忆', '查看、編輯和管理 MaClaw 的長期記憶'),
            icon: settingsTabIcons.memory,
        },
        {
            id: 'knowledge' as const,
            label: textForLang(lang, 'Knowledge', '知识库', '知識庫'),
            desc: textForLang(lang, 'Save URLs and batch-import document knowledge', '保存公共网页，并从目录批量录入文档知识', '保存公共網頁，並從目錄批量錄入文件知識'),
            icon: settingsTabIcons.knowledge,
        },
        {
            id: 'misData' as const,
            label: 'MIS数据',
            desc: textForLang(lang, 'Enterprise structured data service', '企业结构化数据服务', '企業結構化資料服務'),
            icon: settingsTabIcons.misData,
        },
        {
            id: 'embedding' as const,
            label: textForLang(lang, 'AI Model', 'AI 模型', 'AI 模型'),
            desc: textForLang(lang, 'Vector search and embedding model management', '向量搜索与嵌入模型管理', '向量搜尋與嵌入模型管理'),
            icon: settingsTabIcons.embedding,
        },
        {
            id: 'virtualEmployee' as const,
            label: textForLang(lang, 'Employees', '数字员工', '數字員工'),
            desc: textForLang(lang, 'Manage favorite digital employees', '常用数字员工管理', '常用數字員工管理'),
            icon: settingsTabIcons.virtualEmployee,
        },

        {
            id: 'im' as const,
            label: 'IM',
            desc: textForLang(lang, 'Configure QQ Bot, Telegram Bot, WeChat and other IM integrations', '配置 QQ 机器人、Telegram Bot、微信等即时通讯接入', '配置 QQ 機器人、Telegram Bot、微信等即時通訊接入'),
            icon: settingsTabIcons.im,
        },
        {
            id: 'security' as const,
            label: textForLang(lang, 'Security', '安全策略', '安全策略'),
            desc: textForLang(lang, 'Security policy mode and audit log', '安全策略模式与审计日志', '安全策略模式與稽核日誌'),
            icon: settingsTabIcons.security,
        },
        {
            id: 'migration' as const,
            label: textForLang(lang, 'Move Out & In', '\u8fc1\u51fa\u4e0e\u8fc1\u5165', '\u9077\u51fa\u8207\u9077\u5165'),
            desc: textForLang(lang, 'Move memory and local knowledge through the current Hub tenant', '\u901a\u8fc7\u5f53\u524d Hub \u79df\u6237\u8fc1\u79fb\u8bb0\u5fc6\u4e0e\u672c\u5730\u77e5\u8bc6\u5e93', '\u900f\u904e\u76ee\u524d Hub \u79df\u6236\u9077\u79fb\u8a18\u61b6\u8207\u672c\u5730\u77e5\u8b58\u5eab'),
            icon: settingsTabIcons.migration,
        },
        {
            id: 'system' as const,
            label: textForLang(lang, 'System', '系统', '系統'),
            desc: textForLang(lang, 'Heartbeat, screen dimming and other system settings', '心跳、熄屏等系统级设置', '心跳、熄屏等系統級設定'),
            icon: settingsTabIcons.system,
        },
        {
            id: 'assetManagement' as const,
            label: textForLang(lang, 'Asset Management', '资产管理', '資產管理'),
            desc: textForLang(lang, 'Credits balance, spending, recharge, and redemption cards', '查看 Credits 余额、消费、充值及兑换卡', '查看 Credits 餘額、消費、儲值及兌換卡'),
            icon: settingsTabIcons.assetManagement,
        },
    ];
    let filtered = tabs;
    if (options.hideVirtualEmployee) filtered = filtered.filter(tab => tab.id !== 'virtualEmployee');
    return filtered.map((tab) => attachSettingsTabGroup(lang, tab));
};
