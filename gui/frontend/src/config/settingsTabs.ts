export type SettingsTabId = 'general' | 'proxy' | 'ui' | 'display' | 'pet' | 'remote' | 'searchEngine' | 'redeem' | 'skills' | 'mcp' | 'llm' | 'llmCache' | 'embedding' | 'memory' | 'knowledge' | 'misData' | 'virtualEmployee' | 'security' | 'im' | 'system';

export interface SettingsTabOption {
    id: SettingsTabId;
    label: string;
    desc: string;
}

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

export const getSettingsTabOptions = (lang: string, options: { hideVirtualEmployee?: boolean } = {}): SettingsTabOption[] => {
    const tabs: SettingsTabOption[] = [
        {
            id: 'general' as const,
            label: textForLang(lang, 'General', '通用设置', '通用設定'),
            desc: textForLang(lang, 'Language, projects, and environment', '语言、项目与环境', '語言、專案與環境'),
        },
        {
            id: 'proxy' as const,
            label: textForLang(lang, 'Proxy', '代理设置', '代理設定'),
            desc: textForLang(lang, 'Global network proxy configuration', '全局网络代理配置', '全域網路代理設定'),
        },
        {
            id: 'ui' as const,
            label: textForLang(lang, 'UI Config', 'UI 配置', 'UI 配置'),
            desc: textForLang(lang, 'UI scaling and display behavior', '界面缩放与显示行为', '介面縮放與顯示行為'),
        },
        {
            id: 'display' as const,
            label: textForLang(lang, 'Dev CLI', '编程工具', '程式工具'),
            desc: textForLang(lang, 'Tool visibility and startup behavior', '工具显示与启动页行为', '工具顯示與啟動頁行為'),
        },
        {
            id: 'pet' as const,
            label: textForLang(lang, 'Pet', '宠物', '寵物'),
            desc: textForLang(lang, 'Desktop pet appearance, actions, and interaction settings', '桌面宠物形象、动作与交互设置', '桌面寵物形象、動作與互動設定'),
        },
        {
            id: 'remote' as const,
            label: textForLang(lang, 'Remote', '远程注册', '遠端註冊'),
            desc: textForLang(lang, 'Server addresses only', '仅配置远程服务器地址', '僅設定遠端伺服器位址'),
        },
        {
            id: 'searchEngine' as const,
            label: textForLang(lang, 'Search Engine', '搜索引擎', '搜尋引擎'),
            desc: textForLang(lang, 'Configure web search providers', '配置联网搜索引擎', '設定聯網搜尋引擎'),
        },
        {
            id: 'redeem' as const,
            label: textForLang(lang, 'Service Redeem', '服务兑换', '服務兌換'),
            desc: textForLang(lang, 'View credits and redeem service codes', '查看 Credits 和兑换服务码', '查看 Credits 和兌換服務碼'),
        },
        {
            id: 'llm' as const,
            label: textForLang(lang, 'LLM Config', 'LLM 配置', 'LLM 配置'),
            desc: textForLang(lang, 'Configure LLM for MaClaw agent', '配置 MaClaw 代理使用的 LLM', '配置 MaClaw 代理使用的 LLM'),
        },
        {
            id: 'llmCache' as const,
            label: textForLang(lang, 'Cache Service', '\u7f13\u5b58\u670d\u52a1', '\u5feb\u53d6\u670d\u52d9'),
            desc: textForLang(lang, 'Local cache for OpenAI-compatible LLM requests', '\u672c\u5730\u7f13\u5b58 OpenAI \u517c\u5bb9 LLM \u8bf7\u6c42', '\u672c\u5730\u5feb\u53d6 OpenAI \u76f8\u5bb9 LLM \u8acb\u6c42'),
        },
        {
            id: 'memory' as const,
            label: textForLang(lang, 'Memory', '记忆管理', '記憶管理'),
            desc: textForLang(lang, 'View, edit and manage MaClaw long-term memory', '查看、编辑和管理 MaClaw 的长期记忆', '查看、編輯和管理 MaClaw 的長期記憶'),
        },
        {
            id: 'knowledge' as const,
            label: textForLang(lang, 'Knowledge', '知识库', '知識庫'),
            desc: textForLang(lang, 'Save URLs and batch-import document knowledge', '保存公共网页，并从目录批量录入文档知识', '保存公共網頁，並從目錄批量錄入文件知識'),
        },
        {
            id: 'misData' as const,
            label: 'MIS数据',
            desc: textForLang(lang, 'Enterprise structured data service', '企业结构化数据服务', '企業結構化資料服務'),
        },
        {
            id: 'embedding' as const,
            label: textForLang(lang, 'AI Model', 'AI 模型', 'AI 模型'),
            desc: textForLang(lang, 'Vector search and embedding model management', '向量搜索与嵌入模型管理', '向量搜尋與嵌入模型管理'),
        },
        {
            id: 'virtualEmployee' as const,
            label: textForLang(lang, 'Employees', '数字员工', '數字員工'),
            desc: textForLang(lang, 'Manage favorite digital employees', '常用数字员工管理', '常用數字員工管理'),
        },

        {
            id: 'im' as const,
            label: 'IM',
            desc: textForLang(lang, 'Configure QQ Bot, Telegram Bot, WeChat and other IM integrations', '配置 QQ 机器人、Telegram Bot、微信等即时通讯接入', '配置 QQ 機器人、Telegram Bot、微信等即時通訊接入'),
        },
        {
            id: 'security' as const,
            label: textForLang(lang, 'Security', '安全策略', '安全策略'),
            desc: textForLang(lang, 'Security policy mode and audit log', '安全策略模式与审计日志', '安全策略模式與稽核日誌'),
        },
        {
            id: 'system' as const,
            label: textForLang(lang, 'System', '系统', '系統'),
            desc: textForLang(lang, 'Heartbeat, screen dimming and other system settings', '心跳、熄屏等系统级设置', '心跳、熄屏等系統級設定'),
        },
    ];
    let filtered = tabs;
    if (options.hideVirtualEmployee) filtered = filtered.filter(tab => tab.id !== 'virtualEmployee');
    return filtered;
};
