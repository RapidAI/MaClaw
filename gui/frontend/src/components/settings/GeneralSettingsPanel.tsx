import type { ChangeEvent, Dispatch, SetStateAction } from 'react';
import { SaveConfig, SelectWorkingDir } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

type GeneralSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    t: (key: string) => string;
    onLanguageChange: (event: ChangeEvent<HTMLSelectElement>) => void;
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

const saveConfigPatch = (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    patch: Record<string, any>,
    persist = true,
) => {
    if (!config) return;
    const next = new main.AppConfig({ ...config, ...patch });
    setConfig(next);
    if (persist) SaveConfig(next);
};

export const GeneralSettingsPanel = ({ config, setConfig, lang, t, onLanguageChange }: GeneralSettingsPanelProps) => (
    <div className="settings-panel">
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '20px', marginBottom: '15px' }}>
            <div className="form-group" style={{ flex: '1', marginBottom: 0, display: 'flex', alignItems: 'center', gap: '10px' }}>
                <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', fontSize: '0.8rem' }}>{t("language")}</label>
                <select value={lang} onChange={onLanguageChange} className="form-input" style={{ width: 'auto', fontSize: '0.8rem', padding: '2px 8px', height: '28px' }}>
                    <option value="en">English</option>
                    <option value="zh-Hans">{'\u7b80\u4f53\u4e2d\u6587'}</option>
                    <option value="zh-Hant">{'\u7e41\u9ad4\u4e2d\u6587'}</option>
                </select>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px', flexShrink: 0 }}>
                <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', fontSize: '0.8rem' }}>{t("defaultLaunchModeLabel")}</label>
                <label style={{ display: 'flex', alignItems: 'center', gap: '3px', cursor: 'pointer', fontSize: '0.78rem' }}>
                    <input
                        type="radio"
                        name="launchMode"
                        checked={!config?.default_launch_mode || config.default_launch_mode === 'local'}
                        onChange={() => saveConfigPatch(config, setConfig, { default_launch_mode: 'local', remote_enabled: false })}
                    />
                    {t("localModeLabel")}
                </label>
                <label style={{ display: 'flex', alignItems: 'center', gap: '3px', cursor: 'pointer', fontSize: '0.78rem' }}>
                    <input
                        type="radio"
                        name="launchMode"
                        checked={config?.default_launch_mode === 'remote'}
                        onChange={() => saveConfigPatch(config, setConfig, { default_launch_mode: 'remote', remote_enabled: true })}
                    />
                    {t("remoteModeLabel")}
                </label>
            </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '10px', flexWrap: 'wrap' }}>
            <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', fontSize: '0.8rem' }}>
                {textForLang(lang, 'Working Directory', '\u5de5\u4f5c\u76ee\u5f55', '\u5de5\u4f5c\u76ee\u9304')}
            </label>
            <input
                type="text"
                className="form-input"
                style={{ flex: 1, minWidth: '220px', fontSize: '0.78rem', padding: '3px 8px', height: '28px' }}
                value={config?.working_directory || ''}
                placeholder="~/.maclaw/workspace"
                onChange={(e) => saveConfigPatch(config, setConfig, { working_directory: e.target.value }, false)}
                onBlur={() => { if (config) SaveConfig(config); }}
                onKeyDown={(e) => { if (e.key === 'Enter' && config) SaveConfig(config); }}
            />
            <button className="btn btn-sm" style={{ fontSize: '0.75rem', padding: '3px 10px', height: '28px', whiteSpace: 'nowrap' }} onClick={() => {
                SelectWorkingDir().then(dir => {
                    if (dir && config) {
                        const next = new main.AppConfig({ ...config, working_directory: dir });
                        setConfig(next);
                        SaveConfig(next);
                    }
                });
            }}>{textForLang(lang, 'Browse', '\u6d4f\u89c8', '\u700f\u89bd')}</button>
            {config?.working_directory && (
                <button className="btn btn-sm" style={{ fontSize: '0.75rem', padding: '3px 10px', height: '28px', whiteSpace: 'nowrap', opacity: 0.7 }} onClick={() => {
                    saveConfigPatch(config, setConfig, { working_directory: '' });
                }}>{textForLang(lang, 'Reset', '\u91cd\u7f6e', '\u91cd\u7f6e')}</button>
            )}
            <span style={{ fontSize: '0.68rem', color: 'var(--theme-text-muted)', whiteSpace: 'nowrap' }}>
                {textForLang(lang, 'Default directory for agent tasks. Leave empty for ~/.maclaw/workspace', 'Agent \u4efb\u52a1\u7684\u9ed8\u8ba4\u76ee\u5f55\uff0c\u7559\u7a7a\u5219\u4f7f\u7528 ~/.maclaw/workspace', 'Agent \u4efb\u52d9\u7684\u9810\u8a2d\u76ee\u9304\uff0c\u7559\u7a7a\u5247\u4f7f\u7528 ~/.maclaw/workspace')}
            </span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '6px' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '0.8rem' }}>
                <input
                    type="checkbox"
                    checked={config?.llm_trajectory_logging || false}
                    onChange={(e) => saveConfigPatch(config, setConfig, { llm_trajectory_logging: e.target.checked })}
                />
                <span>{textForLang(lang, 'Record LLM trajectory', '\u8bb0\u5f55 LLM \u8f68\u8ff9', '\u8a18\u9304 LLM \u8ecc\u8de1')}</span>
            </label>
            <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)' }}>
                {textForLang(lang, 'Save LLM interaction trajectories for analysis and training.', '\u4fdd\u5b58 LLM \u4ea4\u4e92\u8f68\u8ff9\uff0c\u7528\u4e8e\u5206\u6790\u548c\u8bad\u7ec3\u3002', '\u4fdd\u5b58 LLM \u4ea4\u4e92\u8ecc\u8de1\uff0c\u7528\u65bc\u5206\u6790\u548c\u8a13\u7df4\u3002')}
            </span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '6px' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '0.8rem' }}>
                <input
                    type="checkbox"
                    checked={config?.gossip_auto_publish !== false}
                    onChange={(e) => saveConfigPatch(config, setConfig, { gossip_auto_publish: e.target.checked })}
                />
                <span>{textForLang(lang, 'Auto-post Chat Gossip', '\u804a\u5929\u516b\u5366\u81ea\u52a8\u53d1\u5e03', '\u804a\u5929\u516b\u5366\u81ea\u52d5\u767c\u4f48')}</span>
            </label>
            <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)' }}>
                {textForLang(lang, 'Automatically publish selected chat highlights to the Gossip community.', '\u81ea\u52a8\u5c06\u7b5b\u9009\u540e\u7684\u804a\u5929\u4eae\u70b9\u53d1\u5e03\u5230\u516b\u5366\u793e\u533a\u3002', '\u81ea\u52d5\u5c07\u7be9\u9078\u5f8c\u7684\u804a\u5929\u4eae\u9ede\u767c\u4f48\u5230\u516b\u5366\u793e\u7fa4\u3002')}
            </span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '6px' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '0.8rem' }}>
                <input
                    type="checkbox"
                    checked={config?.log_detail_enabled || false}
                    onChange={(e) => saveConfigPatch(config, setConfig, { log_detail_enabled: e.target.checked })}
                />
                <span>{textForLang(lang, 'Detailed logs', '\u65e5\u5fd7\u8be6\u60c5', '\u65e5\u8a8c\u8a73\u60c5')}</span>
            </label>
            <span style={{ fontSize: '0.7rem', color: '#9ca3af' }}>
                {textForLang(lang, 'When off, only error logs are kept', '\u5173\u95ed\u540e\u53ea\u4fdd\u7559\u9519\u8bef\u65e5\u5fd7', '\u95dc\u9589\u5f8c\u53ea\u4fdd\u7559\u932f\u8aa4\u65e5\u8a8c')}
            </span>
        </div>
    </div>
);
