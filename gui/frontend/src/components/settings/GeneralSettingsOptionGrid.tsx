import { localizeText } from '../../i18n';
import { main } from '../../../wailsjs/go/models';

type GeneralSettingsOptionGridProps = {
    effectiveConfig: main.AppConfig | null;
    lang: string;
    saveConfigPatch: (patch: Record<string, any>) => void;
};

const textForLang = localizeText;

export const GeneralSettingsOptionGrid = ({ effectiveConfig, lang, saveConfigPatch }: GeneralSettingsOptionGridProps) => (
    <div className="general-settings-option-grid">
        <label className="general-settings-option">
            <input type="checkbox" aria-label={textForLang(lang, 'Enable Workflow', '打开工作流', '開啟工作流')} checked={effectiveConfig?.workflow_enabled === true} onChange={(e) => saveConfigPatch({ workflow_enabled: e.target.checked })} />
            <span>{textForLang(lang, 'Enable Workflow', '打开工作流', '開啟工作流')}</span>
            <small>{textForLang(lang, 'Enable multi-phase guided workflows (coding, PPT design, etc.). When off, all messages go directly to the agent.', '启用多阶段引导式工作流（编码、PPT 设计等）。关闭后所有消息直接进入 Agent 处理。', '啟用多階段引導式工作流（編碼、PPT 設計等）。關閉後所有訊息直接進入 Agent 處理。')}</small>
        </label>

        <label className="general-settings-option">
            <input type="checkbox" aria-label={textForLang(lang, 'Record LLM trajectory', '记录 LLM 轨迹', '記錄 LLM 軌跡')} checked={effectiveConfig?.llm_trajectory_logging || false} onChange={(e) => saveConfigPatch({ llm_trajectory_logging: e.target.checked })} />
            <span>{textForLang(lang, 'Record LLM trajectory', '记录 LLM 轨迹', '記錄 LLM 軌跡')}</span>
            <small>{textForLang(lang, 'Save LLM interaction trajectories for analysis and training.', '保存 LLM 交互轨迹，用于分析和训练。', '保存 LLM 交互軌跡，用於分析和訓練。')}</small>
        </label>

        <label className="general-settings-option">
            <input type="checkbox" aria-label={textForLang(lang, 'Detailed logs', '日志详情', '日誌詳情')} checked={effectiveConfig?.log_detail_enabled || false} onChange={(e) => saveConfigPatch({ log_detail_enabled: e.target.checked })} />
            <span>{textForLang(lang, 'Detailed logs', '日志详情', '日誌詳情')}</span>
            <small>{textForLang(lang, 'When off, only error logs are kept', '关闭后只保留错误日志', '關閉後只保留錯誤日誌')}</small>
        </label>

        <label className="general-settings-option">
            <input type="checkbox" aria-label={textForLang(lang, 'Memory recall log', '记忆召回日志', '記憶召回日誌')} checked={effectiveConfig?.memory_recall_log_enabled || false} onChange={(e) => saveConfigPatch({ memory_recall_log_enabled: e.target.checked })} />
            <span>{textForLang(lang, 'Memory recall log', '记忆召回日志', '記憶召回日誌')}</span>
            <small>{textForLang(lang, 'Log every recall operation to a dedicated file (memory_recall.log) for debugging.', '将每次记忆召回操作记录到独立文件 memory_recall.log，用于调试。', '將每次記憶召回操作記錄到獨立檔案 memory_recall.log，用於除錯。')}</small>
        </label>

        <label className="general-settings-option">
            <input type="checkbox" aria-label={textForLang(lang, 'Auto-post Chat Gossip', '聊天八卦自动发布', '聊天八卦自動發佈')} checked={effectiveConfig?.gossip_auto_publish !== false} onChange={(e) => saveConfigPatch({ gossip_auto_publish: e.target.checked })} />
            <span>{textForLang(lang, 'Auto-post Chat Gossip', '聊天八卦自动发布', '聊天八卦自動發佈')}</span>
            <small>{textForLang(lang, 'Automatically publish selected chat highlights to the Gossip community.', '自动将筛选后的聊天亮点发布到八卦社区。', '自動將篩選後的聊天亮點發佈到八卦社群。')}</small>
        </label>

        <label className="general-settings-option">
            <input type="checkbox" aria-label={textForLang(lang, 'Show Hub ranking badge', '显示 Hub 排名勋章', '顯示 Hub 排名勳章')} checked={(effectiveConfig as (main.AppConfig & { show_hub_ranking?: boolean }) | null)?.show_hub_ranking !== false} onChange={(e) => saveConfigPatch({ show_hub_ranking: e.target.checked })} />
            <span>{textForLang(lang, 'Show Hub ranking badge', '显示 Hub 排名勋章', '顯示 Hub 排名勳章')}</span>
            <small>{textForLang(lang, 'Display a medal in the sidebar when you rank top 3 this month.', '本月排名前 3 时在侧边栏显示奖牌。', '本月排名前 3 時在側邊欄顯示獎牌。')}</small>
        </label>
    </div>
);
