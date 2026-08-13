import { localizeText } from '../../i18n';
import { corelib, main } from '../../../wailsjs/go/models';

type GeneralSettingsOptionGridProps = {
    effectiveConfig: corelib.AppConfig | null;
    lang: string;
    saveConfigPatch: (patch: Record<string, any>) => void;
};

const textForLang = localizeText;

export const GeneralSettingsOptionGrid = ({ effectiveConfig, lang, saveConfigPatch }: GeneralSettingsOptionGridProps) => (
    <div className="general-settings-option-grid">
        <label className="general-settings-option">
            <input
                type="checkbox"
                aria-label={textForLang(lang, 'Skill self-evolution', '技能自进化', '技能自進化')}
                checked={effectiveConfig?.skill_evolution_enabled !== false}
                onChange={(e) => saveConfigPatch({ skill_evolution_enabled: e.target.checked })}
            />
            <span>{textForLang(lang, 'Skill self-evolution', '技能自进化', '技能自進化')}</span>
            <small>{textForLang(
                lang,
                'After skill runs, automatically attempt self-repair, optimization, and discovery. Manual Repair/Optimize still work when off. Also under Skills → Settings → Evolution.',
                '技能执行后自动尝试自修复、优化与发现。关闭后仍可手动「立即修复/立即优化」。亦可在 技能 → 设置 → 进化 中配置。',
                '技能執行後自動嘗試自修復、優化與發現。關閉後仍可手動「立即修復/立即優化」。亦可在 技能 → 設定 → 進化 中配置。',
            )}</small>
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
            <input type="checkbox" aria-label={textForLang(lang, 'Show Hub ranking badge', '显示 Hub 排名勋章', '顯示 Hub 排名勳章')} checked={(effectiveConfig as (corelib.AppConfig & { show_hub_ranking?: boolean }) | null)?.show_hub_ranking !== false} onChange={(e) => saveConfigPatch({ show_hub_ranking: e.target.checked })} />
            <span>{textForLang(lang, 'Show Hub ranking badge', '显示 Hub 排名勋章', '顯示 Hub 排名勳章')}</span>
            <small>{textForLang(lang, 'Display a medal in the sidebar when you rank top 3 this month.', '本月排名前 3 时在侧边栏显示奖牌。', '本月排名前 3 時在側邊欄顯示獎牌。')}</small>
        </label>
    </div>
);
