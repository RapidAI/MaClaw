import { useMemo, useRef, useState, type ChangeEvent, type Dispatch, type SetStateAction } from 'react';
import { PatchConfigFields, SelectWorkingDir } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { GeneralSettingsOptionGrid } from './GeneralSettingsOptionGrid';

type GeneralSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    t: (key: string) => string;
    onLanguageChange: (event: ChangeEvent<HTMLSelectElement>) => void;
};

const textForLang = localizeText;

const persistConfigPatch = (patch: Record<string, any>, context: string) => {
    return Promise.resolve(PatchConfigFields(patch)).catch((err) => {
        console.error(`Failed to save ${context}:`, err);
        throw err;
    });
};

// Guard anchor: GeneralSettingsOptionGrid owns llm_trajectory_logging and log_detail_enabled toggles.

export const GeneralSettingsPanel = ({ config, setConfig, lang, t, onLanguageChange }: GeneralSettingsPanelProps) => {
    const [pendingPatch, setPendingPatch] = useState<Record<string, any>>({});
    const effectiveConfig = useMemo(() => (
        config ? new main.AppConfig({ ...config, ...pendingPatch }) : config
    ), [config, pendingPatch]);
    const configRef = useRef<main.AppConfig | null>(effectiveConfig);
    configRef.current = effectiveConfig;

    const clearConfirmedPatch = (patch: Record<string, any>) => {
        setPendingPatch(prev => {
            let changed = false;
            const next = { ...prev };
            Object.entries(patch).forEach(([key, value]) => {
                if (next[key] === value) {
                    delete next[key];
                    changed = true;
                }
            });
            return changed ? next : prev;
        });
    };

    const persistAndConfirm = (patch: Record<string, any>, context: string) => {
        void persistConfigPatch(patch, context).then((saved) => {
            const confirmed = new main.AppConfig(saved);
            configRef.current = confirmed;
            setConfig(confirmed);
            clearConfirmedPatch(patch);
        }).catch(() => undefined);
    };

    const saveConfigPatch = (patch: Record<string, any>, persist = true) => {
        const current = configRef.current;
        if (!current) return null;
        const next = new main.AppConfig({ ...current, ...patch });
        configRef.current = next;
        setPendingPatch(prev => ({ ...prev, ...patch }));
        setConfig(next);
        if (persist) persistAndConfirm(patch, 'general settings');
        return next;
    };

    const persistWorkingDirectory = () => {
        const current = configRef.current;
        if (!current) return;
        persistAndConfirm({ working_directory: current.working_directory }, 'working directory');
    };

    return <div className="settings-panel general-settings-panel">
        <section className="general-settings-card general-settings-card--compact">
            <div className="general-settings-language-row">
                <label className="general-settings-field">
                    <span>{t("language")}</span>
                    <select value={lang} onChange={onLanguageChange} className="form-input">
                        <option value="en">English</option>
                        <option value="zh-Hans">{'\u7b80\u4f53\u4e2d\u6587'}</option>
                        <option value="zh-Hant">{'\u7e41\u9ad4\u4e2d\u6587'}</option>
                    </select>
                </label>
                <label className="general-settings-option general-settings-option--inline general-settings-option--plain">
                    <input type="checkbox" checked={effectiveConfig?.show_app_entry === true} onChange={(e) => saveConfigPatch({ show_app_entry: e.target.checked })} />
                    <span>{textForLang(lang, 'MaClaw app entry', 'MaClaw\u5e94\u7528\u5165\u53e3', 'MaClaw\u61c9\u7528\u5165\u53e3')}</span>
                </label>
                <label className="general-settings-option general-settings-option--inline general-settings-option--plain">
                    <input type="checkbox" checked={effectiveConfig?.show_workflow_entry !== false} onChange={(e) => saveConfigPatch({ show_workflow_entry: e.target.checked })} />
                    <span>{textForLang(lang, 'Workflow entry', '\u5de5\u4f5c\u6d41\u5165\u53e3', '\u5de5\u4f5c\u6d41\u5165\u53e3')}</span>
                </label>
                <label className="general-settings-option general-settings-option--inline general-settings-option--plain">
                    <input type="checkbox" checked={(effectiveConfig as any)?.show_utilities_entry !== false} onChange={(e) => saveConfigPatch({ show_utilities_entry: e.target.checked } as any)} />
                    <span>{textForLang(lang, 'Utilities entry', '\u5b9e\u7528\u5de5\u5177\u5165\u53e3', '\u5be6\u7528\u5de5\u5177\u5165\u53e3')}</span>
                </label>
                <label className="general-settings-option general-settings-option--inline general-settings-option--plain">
                    <input type="checkbox" checked={(effectiveConfig as any)?.survey_enabled !== false} onChange={(e) => saveConfigPatch({ survey_enabled: e.target.checked } as any)} />
                    <span>{textForLang(lang, 'Survey IM intercept', '\u95ee\u5377 IM \u62e6\u622a', '\u554f\u5377 IM \u62e6\u622a')}</span>
                </label>
            </div>
        </section>

        <section className="general-settings-card general-settings-card--stacked">
            <div className="general-settings-directory-row">
                <label className="general-settings-field general-settings-field--wide">
                    <span>{textForLang(lang, 'Working Directory', '\u5de5\u4f5c\u76ee\u5f55', '\u5de5\u4f5c\u76ee\u9304')}</span>
                    <input
                        type="text"
                        className="form-input"
                        value={effectiveConfig?.working_directory || ''}
                        placeholder="~/.maclaw/workspace"
                        onChange={(e) => saveConfigPatch({ working_directory: e.target.value }, false)}
                        onBlur={persistWorkingDirectory}
                        onKeyDown={(e) => { if (e.key === 'Enter') persistWorkingDirectory(); }}
                    />
                </label>
                <div className="general-settings-actions">
                    <button className="btn btn-sm" onClick={() => {
                        SelectWorkingDir().then(dir => {
                            if (dir) saveConfigPatch({ working_directory: dir });
                        }).catch((err) => console.error('Failed to select working directory:', err));
                    }}>{textForLang(lang, 'Browse', '\u6d4f\u89c8', '\u700f\u89bd')}</button>
                    {effectiveConfig?.working_directory && (
                        <button className="btn btn-sm" onClick={() => {
                            saveConfigPatch({ working_directory: '' });
                        }}>{textForLang(lang, 'Reset', '\u91cd\u7f6e', '\u91cd\u7f6e')}</button>
                    )}
                </div>
            </div>
            <p>{textForLang(lang, 'Default directory for agent tasks. Leave empty for ~/.maclaw/workspace', 'Agent \u4efb\u52a1\u7684\u9ed8\u8ba4\u76ee\u5f55\uff0c\u7559\u7a7a\u5219\u4f7f\u7528 ~/.maclaw/workspace', 'Agent \u4efb\u52d9\u7684\u9810\u8a2d\u76ee\u9304\uff0c\u7559\u7a7a\u5247\u4f7f\u7528 ~/.maclaw/workspace')}</p>
        </section>

        <GeneralSettingsOptionGrid effectiveConfig={effectiveConfig} lang={lang} saveConfigPatch={saveConfigPatch} />
    </div>;
};
