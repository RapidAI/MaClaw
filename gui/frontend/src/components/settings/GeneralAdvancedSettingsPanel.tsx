import type { Dispatch, SetStateAction } from 'react';
import { SaveConfig, SetEnvCheckInterval } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

type GeneralAdvancedSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    t: (key: string) => string;
    hasWindowsTerminal: boolean;
    envCheckInterval: number;
    setEnvCheckInterval: Dispatch<SetStateAction<number>>;
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

const saveConfigPatch = (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    patch: Record<string, any>,
) => {
    if (!config) return;
    const next = new main.AppConfig({ ...config, ...patch });
    setConfig(next);
    SaveConfig(next);
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
    <div className="settings-panel">
        <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                <input
                    type="checkbox"
                    checked={!config?.hide_startup_popup}
                    onChange={(e) => saveConfigPatch(config, setConfig, { hide_startup_popup: !e.target.checked })}
                    style={{ width: '16px', height: '16px' }}
                />
                <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-primary)' }}>{t("showWelcomePage")}</span>
            </label>
            <p style={{ fontSize: '0.75rem', color: 'var(--theme-text-muted)', marginLeft: '24px', marginTop: '4px' }}>
                {textForLang(lang, 'When enabled, a welcome popup with tutorial links will be shown at startup.', '\u5f00\u542f\u540e\uff0c\u7a0b\u5e8f\u542f\u52a8\u65f6\u5c06\u663e\u793a\u65b0\u624b\u6559\u5b66\u548c\u5feb\u901f\u5165\u95e8\u94fe\u63a5', '\u958b\u555f\u5f8c\uff0c\u7a0b\u5e8f\u555f\u52d5\u6642\u5c07\u986f\u793a\u65b0\u624b\u6559\u5b78\u548c\u5feb\u901f\u5165\u9580\u93c8\u63a5')}
            </p>
        </div>

        <div className="form-group" style={{ marginTop: '10px', borderTop: '1px solid var(--theme-border)', paddingTop: '10px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '20px', flexWrap: 'wrap' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                    <input
                        type="checkbox"
                        checked={config?.pause_env_check}
                        onChange={(e) => saveConfigPatch(config, setConfig, { pause_env_check: e.target.checked })}
                        style={{ width: '16px', height: '16px' }}
                    />
                    <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-primary)' }}>{t("pauseEnvCheck")}</span>
                </label>

                {hasWindowsTerminal && (
                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                        <input
                            type="checkbox"
                            checked={config?.use_windows_terminal}
                            onChange={(e) => saveConfigPatch(config, setConfig, { use_windows_terminal: e.target.checked })}
                            style={{ width: '16px', height: '16px' }}
                        />
                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-primary)' }}>{t("useWindowsTerminal")}</span>
                    </label>
                )}
            </div>

            {config?.pause_env_check && (
                <div style={{ marginLeft: '24px', display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <label style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                        <span>{t("envCheckIntervalPrefix")}</span>
                        <select
                            value={envCheckInterval}
                            onChange={(e) => {
                                const days = parseInt(e.target.value);
                                setEnvCheckInterval(days);
                                SetEnvCheckInterval(days);
                            }}
                            style={{ padding: '3px 6px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.8rem', width: '60px', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)' }}
                        >
                            {Array.from({ length: 29 }, (_, i) => i + 2).map(day => (
                                <option key={day} value={day}>{day}</option>
                            ))}
                        </select>
                        <span>{t("envCheckIntervalSuffix")}</span>
                    </label>
                </div>
            )}
        </div>

        <div className="form-group" style={{ marginTop: '10px', borderTop: '1px solid var(--theme-border)', paddingTop: '10px' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                <input
                    type="checkbox"
                    checked={config?.show_ai_trace_entry || false}
                    onChange={(e) => saveConfigPatch(config, setConfig, { show_ai_trace_entry: e.target.checked })}
                    style={{ width: '16px', height: '16px' }}
                />
                <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-primary)' }}>
                    {textForLang(lang, 'Show AI run details', '\u663e\u793a AI \u8fd0\u884c\u8be6\u60c5', '\u986f\u793a AI \u57f7\u884c\u8a73\u60c5')}
                </span>
            </label>
            <p style={{ fontSize: '0.75rem', color: 'var(--theme-text-muted)', marginLeft: '24px', marginTop: '4px' }}>
                {textForLang(lang, 'When enabled, AI assistant messages show a Trace / run details entry. Disabled by default.', '\u5f00\u542f\u540e\uff0cAI \u52a9\u624b\u6d88\u606f\u4e2d\u4f1a\u663e\u793a Trace / \u8fd0\u884c\u8be6\u60c5\u5165\u53e3\uff1b\u9ed8\u8ba4\u5173\u95ed\u3002', '\u958b\u555f\u5f8c\uff0cAI \u52a9\u624b\u6d88\u606f\u4e2d\u6703\u986f\u793a Trace / \u57f7\u884c\u8a73\u60c5\u5165\u53e3\uff1b\u9810\u8a2d\u95dc\u9589\u3002')}
            </p>
        </div>
    </div>
);
