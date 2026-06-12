import { useMemo, useRef, useState, type ChangeEvent, type Dispatch, type SetStateAction } from 'react';
import { PatchConfigFields, SelectWorkingDir } from '../../../wailsjs/go/main/App';
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

const persistConfigPatch = (patch: Record<string, any>, context: string) => {
    return Promise.resolve(PatchConfigFields(patch)).catch((err) => {
        console.error(`Failed to save ${context}:`, err);
        throw err;
    });
};

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
            <label className="general-settings-field">
                <span>{t("language")}</span>
                <select value={lang} onChange={onLanguageChange} className="form-input">
                    <option value="en">English</option>
                    <option value="zh-Hans">{'\u7b80\u4f53\u4e2d\u6587'}</option>
                    <option value="zh-Hant">{'\u7e41\u9ad4\u4e2d\u6587'}</option>
                </select>
            </label>
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

        <div className="general-settings-option-grid">
            <label className="general-settings-option">
                <input
                    type="checkbox"
                    aria-label={textForLang(lang, 'Enable Workflow', '\u6253\u5f00\u5de5\u4f5c\u6d41', '\u958b\u555f\u5de5\u4f5c\u6d41')}
                    checked={effectiveConfig?.workflow_enabled === true}
                    onChange={(e) => saveConfigPatch({ workflow_enabled: e.target.checked })}
                />
                <span>{textForLang(lang, 'Enable Workflow', '\u6253\u5f00\u5de5\u4f5c\u6d41', '\u958b\u555f\u5de5\u4f5c\u6d41')}</span>
                <small>{textForLang(lang, 'Enable multi-phase guided workflows (coding, PPT design, etc.). When off, all messages go directly to the agent.', '\u542f\u7528\u591a\u9636\u6bb5\u5f15\u5bfc\u5f0f\u5de5\u4f5c\u6d41\uff08\u7f16\u7801\u3001PPT \u8bbe\u8ba1\u7b49\uff09\u3002\u5173\u95ed\u540e\u6240\u6709\u6d88\u606f\u76f4\u63a5\u8fdb\u5165 Agent \u5904\u7406\u3002', '\u555f\u7528\u591a\u968e\u6bb5\u5f15\u5c0e\u5f0f\u5de5\u4f5c\u6d41\uff08\u7de8\u78bc\u3001PPT \u8a2d\u8a08\u7b49\uff09\u3002\u95dc\u9589\u5f8c\u6240\u6709\u8a0a\u606f\u76f4\u63a5\u9032\u5165 Agent \u8655\u7406\u3002')}</small>
            </label>

            <label className="general-settings-option">
                <input
                    type="checkbox"
                    aria-label={textForLang(lang, 'Record LLM trajectory', '\u8bb0\u5f55 LLM \u8f68\u8ff9', '\u8a18\u9304 LLM \u8ecc\u8de1')}
                    checked={effectiveConfig?.llm_trajectory_logging || false}
                    onChange={(e) => saveConfigPatch({ llm_trajectory_logging: e.target.checked })}
                />
                <span>{textForLang(lang, 'Record LLM trajectory', '\u8bb0\u5f55 LLM \u8f68\u8ff9', '\u8a18\u9304 LLM \u8ecc\u8de1')}</span>
                <small>{textForLang(lang, 'Save LLM interaction trajectories for analysis and training.', '\u4fdd\u5b58 LLM \u4ea4\u4e92\u8f68\u8ff9\uff0c\u7528\u4e8e\u5206\u6790\u548c\u8bad\u7ec3\u3002', '\u4fdd\u5b58 LLM \u4ea4\u4e92\u8ecc\u8de1\uff0c\u7528\u65bc\u5206\u6790\u548c\u8a13\u7df4\u3002')}</small>
            </label>

            <label className="general-settings-option">
                <input
                    type="checkbox"
                    aria-label={textForLang(lang, 'Detailed logs', '\u65e5\u5fd7\u8be6\u60c5', '\u65e5\u8a8c\u8a73\u60c5')}
                    checked={effectiveConfig?.log_detail_enabled || false}
                    onChange={(e) => saveConfigPatch({ log_detail_enabled: e.target.checked })}
                />
                <span>{textForLang(lang, 'Detailed logs', '\u65e5\u5fd7\u8be6\u60c5', '\u65e5\u8a8c\u8a73\u60c5')}</span>
                <small>{textForLang(lang, 'When off, only error logs are kept', '\u5173\u95ed\u540e\u53ea\u4fdd\u7559\u9519\u8bef\u65e5\u5fd7', '\u95dc\u9589\u5f8c\u53ea\u4fdd\u7559\u932f\u8aa4\u65e5\u8a8c')}</small>
            </label>

            <label className="general-settings-option">
                <input
                    type="checkbox"
                    aria-label={textForLang(lang, 'Auto-post Chat Gossip', '\u804a\u5929\u516b\u5366\u81ea\u52a8\u53d1\u5e03', '\u804a\u5929\u516b\u5366\u81ea\u52d5\u767c\u4f48')}
                    checked={effectiveConfig?.gossip_auto_publish !== false}
                    onChange={(e) => saveConfigPatch({ gossip_auto_publish: e.target.checked })}
                />
                <span>{textForLang(lang, 'Auto-post Chat Gossip', '\u804a\u5929\u516b\u5366\u81ea\u52a8\u53d1\u5e03', '\u804a\u5929\u516b\u5366\u81ea\u52d5\u767c\u4f48')}</span>
                <small>{textForLang(lang, 'Automatically publish selected chat highlights to the Gossip community.', '\u81ea\u52a8\u5c06\u7b5b\u9009\u540e\u7684\u804a\u5929\u4eae\u70b9\u53d1\u5e03\u5230\u516b\u5366\u793e\u533a\u3002', '\u81ea\u52d5\u5c07\u7be9\u9078\u5f8c\u7684\u804a\u5929\u4eae\u9ede\u767c\u4f48\u5230\u516b\u5366\u793e\u7fa4\u3002')}</small>
            </label>
        </div>
    </div>;
};
