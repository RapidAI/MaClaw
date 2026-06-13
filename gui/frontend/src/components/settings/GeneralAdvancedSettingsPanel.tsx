import type { Dispatch, SetStateAction } from 'react';
import { PatchConfigFields, SetEnvCheckInterval } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';

type GeneralAdvancedSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    t: (key: string) => string;
    hasWindowsTerminal: boolean;
    envCheckInterval: number;
    setEnvCheckInterval: Dispatch<SetStateAction<number>>;
};

const textForLang = localizeText;

const saveConfigPatch = (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    patch: Record<string, any>,
) => {
    if (!config) return;
    const next = new main.AppConfig({ ...config, ...patch });
    setConfig(next);
    PatchConfigFields(patch).then((saved) => {
        setConfig(new main.AppConfig(saved));
    }).catch((err) => {
        console.error('Failed to patch advanced settings:', err);
        setConfig(config);
    });
};

export const GeneralAdvancedSettingsPanel = ({
    config,
    setConfig,
    lang,
    t,
    hasWindowsTerminal,
    envCheckInterval,
    setEnvCheckInterval,
}: GeneralAdvancedSettingsPanelProps) => (
    <div className="settings-panel general-advanced-settings">
        <section className="general-settings-card general-settings-card--stacked">
            <div className="general-settings-option-row">
                <label className="general-settings-option general-settings-option--inline">
                    <input
                        type="checkbox"
                        checked={config?.pause_env_check}
                        onChange={(e) => saveConfigPatch(config, setConfig, { pause_env_check: e.target.checked })}
                    />
                    <span>{t("pauseEnvCheck")}</span>
                </label>

                {hasWindowsTerminal && (
                    <label className="general-settings-option general-settings-option--inline">
                        <input
                            type="checkbox"
                            checked={config?.use_windows_terminal}
                            onChange={(e) => saveConfigPatch(config, setConfig, { use_windows_terminal: e.target.checked })}
                        />
                        <span>{t("useWindowsTerminal")}</span>
                    </label>
                )}
            </div>

            {config?.pause_env_check && (
                <label className="general-settings-inline-select">
                    <span>{t("envCheckIntervalPrefix")}</span>
                    <select
                        value={envCheckInterval}
                        onChange={(e) => {
                            const days = parseInt(e.target.value);
                            setEnvCheckInterval(days);
                            SetEnvCheckInterval(days);
                        }}
                    >
                        {Array.from({ length: 29 }, (_, i) => i + 2).map(day => (
                            <option key={day} value={day}>{day}</option>
                        ))}
                    </select>
                    <span>{t("envCheckIntervalSuffix")}</span>
                </label>
            )}
        </section>

        <label className="general-settings-option general-settings-option--wide">
            <input
                type="checkbox"
                checked={config?.show_ai_trace_entry || false}
                onChange={(e) => saveConfigPatch(config, setConfig, { show_ai_trace_entry: e.target.checked })}
            />
            <span>{textForLang(lang, 'Show AI run details', '显示 AI 运行详情', '顯示 AI 執行詳情')}</span>
            <small>{textForLang(lang, 'When enabled, AI assistant messages show a Trace / run details entry. Disabled by default.', '开启后，AI 助手消息中会显示 Trace / 运行详情入口；默认关闭。', '開啟後，AI 助手消息中會顯示 Trace / 執行詳情入口；預設關閉。')}</small>
        </label>
    </div>
);
