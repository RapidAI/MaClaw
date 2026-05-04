export type SettingsTabId = 'general' | 'proxy' | 'ui' | 'display' | 'pet' | 'remote' | 'redeem' | 'skills' | 'mcp' | 'llm' | 'embedding' | 'memory' | 'agentnet' | 'groupDiscussion' | 'security' | 'im' | 'system';

export interface SettingsTabOption {
    id: SettingsTabId;
    label: string;
    desc: string;
}

export const getSettingsTabOptions = (lang: string): SettingsTabOption[] => {
    return [
        {
            id: 'general' as const,
            label: lang === 'zh-Hans' ? '通用设置' : lang === 'zh-Hant' ? '通用設置' : 'General',
            desc: lang === 'zh-Hans' ? '语言、项目与环境' : lang === 'zh-Hant' ? '語言、項目與環境' : 'Language, projects, and environment',
        },
        {
            id: 'proxy' as const,
            label: lang === 'zh-Hans' ? '代理设置' : lang === 'zh-Hant' ? '代理設置' : 'Proxy',
            desc: lang === 'zh-Hans' ? '全局网络代理配置' : lang === 'zh-Hant' ? '全局網路代理配置' : 'Global network proxy configuration',
        },
        {
            id: 'ui' as const,
            label: lang === 'zh-Hans' ? 'UI配置' : lang === 'zh-Hant' ? 'UI配置' : 'UI Config',
            desc: lang === 'zh-Hans' ? '界面缩放与整体显示行为' : lang === 'zh-Hant' ? '介面縮放與整體顯示行為' : 'UI scaling and display behavior',
        },
        {
            id: 'display' as const,
            label: lang === 'zh-Hans' ? '编程工具' : lang === 'zh-Hant' ? '編程工具' : 'Dev CLI',
            desc: lang === 'zh-Hans' ? '工具显示与启动页行为' : lang === 'zh-Hant' ? '工具顯示與啟動頁行為' : 'Tool visibility and startup behavior',
        },
        {
            id: 'pet' as const,
            label: lang === 'zh-Hans' ? '\u5ba0\u7269' : lang === 'zh-Hant' ? '\u5bf5\u7269' : 'Pet',
            desc: lang === 'zh-Hans' ? '\u684c\u9762\u5ba0\u7269\u5f62\u8c61\u3001\u52a8\u4f5c\u4e0e\u4ea4\u4e92\u8bbe\u7f6e' : lang === 'zh-Hant' ? '\u684c\u9762\u5bf5\u7269\u5f62\u8c61\u3001\u52d5\u4f5c\u8207\u4ea4\u4e92\u8a2d\u7f6e' : 'Desktop pet appearance, actions, and interaction settings',
        },
        {
            id: 'remote' as const,
            label: lang === 'zh-Hans' ? '远程注册' : lang === 'zh-Hant' ? '遠端註冊' : 'Remote',
            desc: lang === 'zh-Hans' ? '仅配置远程服务器地址' : lang === 'zh-Hant' ? '僅配置遠端伺服器位址' : 'Server addresses only',
        },
        {
            id: 'redeem' as const,
            label: lang === 'zh-Hans' ? '\u670d\u52a1\u5151\u6362' : lang === 'zh-Hant' ? '\u670d\u52d9\u5151\u63db' : 'Service Redeem',
            desc: lang === 'zh-Hans' ? '\u67e5\u770b Credits \u548c\u5151\u6362\u670d\u52a1\u7801' : lang === 'zh-Hant' ? '\u67e5\u770b Credits \u548c\u5151\u63db\u670d\u52d9\u78bc' : 'View credits and redeem service codes',
        },
        {
            id: 'llm' as const,
            label: lang === 'zh-Hans' ? 'LLM 配置' : lang === 'zh-Hant' ? 'LLM 配置' : 'LLM Config',
            desc: lang === 'zh-Hans' ? '配置 MaClaw 代理使用的 LLM' : lang === 'zh-Hant' ? '配置 MaClaw 代理使用的 LLM' : 'Configure LLM for MaClaw agent',
        },
        {
            id: 'memory' as const,
            label: lang === 'zh-Hans' ? '记忆管理' : lang === 'zh-Hant' ? '記憶管理' : 'Memory',
            desc: lang === 'zh-Hans' ? '查看、编辑和管理 MaClaw 的长期记忆' : lang === 'zh-Hant' ? '查看、編輯和管理 MaClaw 的長期記憶' : 'View, edit and manage MaClaw long-term memory',
        },
        {
            id: 'embedding' as const,
            label: lang === 'zh-Hans' ? 'AI模型' : lang === 'zh-Hant' ? 'AI模型' : 'AI Model',
            desc: lang === 'zh-Hans' ? '向量搜索与嵌入模型管理' : lang === 'zh-Hant' ? '向量搜索與嵌入模型管理' : 'Vector search and embedding model management',
        },
        {
            id: 'agentnet' as const,
            label: lang === 'zh-Hans' ? '智网' : lang === 'zh-Hant' ? '智網' : 'AgentNet',
            desc: lang === 'zh-Hans' ? 'AgentNet P2P 去中心化 Agent 网络' : lang === 'zh-Hant' ? 'AgentNet P2P 去中心化 Agent 網路' : 'AgentNet decentralized P2P agent network',
        },
        {
            id: 'groupDiscussion' as const,
            label: lang === 'zh-Hans' ? 'A2A \u7fa4\u7ec4' : lang === 'zh-Hant' ? 'A2A \u7fa4\u7d44' : 'A2A Group',
            desc: lang === 'zh-Hans' ? '\u5f53\u524d Hub \u7684\u4e13\u5bb6\u53d1\u73b0\u3001\u9080\u8bf7\u4e0e\u7fa4\u7ec4\u8ba8\u8bba\u7b56\u7565' : lang === 'zh-Hant' ? '\u76ee\u524d Hub \u7684\u5c08\u5bb6\u767c\u73fe\u3001\u9080\u8acb\u8207\u7fa4\u7d44\u8a0e\u8ad6\u7b56\u7565' : 'Current-Hub expert discovery, invites, and discussion policy',
        },
        {
            id: 'im' as const,
            label: 'IM',
            desc: lang === 'zh-Hans' ? '配置 QQ 机器人、Telegram Bot、微信等即时通讯接入' : lang === 'zh-Hant' ? '配置 QQ 機器人、Telegram Bot、微信等即時通訊接入' : 'Configure QQ Bot, Telegram Bot, WeChat and other IM integrations',
        },
        {
            id: 'security' as const,
            label: lang === 'zh-Hans' ? '安全策略' : lang === 'zh-Hant' ? '安全策略' : 'Security',
            desc: lang === 'zh-Hans' ? '安全策略模式与审计日志' : lang === 'zh-Hant' ? '安全策略模式與審計日誌' : 'Security policy mode and audit log',
        },
        {
            id: 'system' as const,
            label: lang === 'zh-Hans' ? '系统' : lang === 'zh-Hant' ? '系統' : 'System',
            desc: lang === 'zh-Hans' ? '心跳、熄屏等系统级设置' : lang === 'zh-Hant' ? '心跳、熄屏等系統級設置' : 'Heartbeat, screen dimming and other system settings',
        },
    ];
};
