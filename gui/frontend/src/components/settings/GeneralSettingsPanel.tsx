import { LoadConfig, PatchConfigFields, SelectWorkingDir } from '../../../wailsjs/go/main/App';
import { type ChangeEvent, type Dispatch, type SetStateAction, useMemo, useRef, useState } from 'react';
import { corelib, main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { EVENT_MACLAW_CONFIG_CHANGED } from '../../constants/events';
import { miniAppEntryLabel } from '../../i18n/maclawMiniAppLabels';
import { GeneralSettingsOptionGrid } from './GeneralSettingsOptionGrid';

type GeneralSettingsPanelProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    t: (key: string) => string;
    onLanguageChange: (event: ChangeEvent<HTMLSelectElement>) => void;
};

const textForLang = localizeText;

const persistConfigPatch = (patch: Record<string, any>, context: string) => {
    return Promise.resolve(PatchConfigFields(patch)).then((saved) => {
        return saved;
    }).catch((err) => {
        console.error(`Failed to save ${context}:`, err);
        throw err;
    });
};

// Guard anchor: GeneralSettingsOptionGrid owns llm_trajectory_logging and log_detail_enabled toggles.

export const GeneralSettingsPanel = ({ config, setConfig, lang, t, onLanguageChange }: GeneralSettingsPanelProps) => {
    const [pendingPatch, setPendingPatch] = useState<Record<string, any>>({});
    const effectiveConfig = useMemo(() => (
        config ? new corelib.AppConfig({ ...config, ...pendingPatch }) : config
    ), [config, pendingPatch]);
    const configRef = useRef<corelib.AppConfig | null>(effectiveConfig);
    const pendingPatchRef = useRef<Record<string, any>>(pendingPatch);
    const patchRequestVersionsRef = useRef<Record<string, number>>({});
    configRef.current = effectiveConfig;
    pendingPatchRef.current = pendingPatch;

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
            if (!changed) return prev;
            pendingPatchRef.current = next;
            return next;
        });
    };

    const clearFailedPatch = (patch: Record<string, any>, requestVersions: Record<string, number>) => {
        const failedKeys = Object.keys(patch).filter((key) => (
            patchRequestVersionsRef.current[key] === requestVersions[key]
        ));
        if (failedKeys.length === 0) return;

        const nextPending = { ...pendingPatchRef.current };
        failedKeys.forEach((key) => { delete nextPending[key]; });
        pendingPatchRef.current = nextPending;
        setPendingPatch(nextPending);

        // Reload the persisted snapshot: if the request reached the backend but
        // its response was lost, this keeps the actual saved value; otherwise it
        // cleanly rolls back the optimistic UI state.
        void Promise.resolve(LoadConfig()).then((saved) => {
            const restored = new corelib.AppConfig({ ...new corelib.AppConfig(saved), ...pendingPatchRef.current });
            configRef.current = restored;
            setConfig(restored);
            window.dispatchEvent(new CustomEvent(EVENT_MACLAW_CONFIG_CHANGED, { detail: restored }));
        }).catch((err) => {
            console.error('Failed to reload config after save failure:', err);
        });
    };

    const trackPatchRequest = (patch: Record<string, any>) => {
        const requestVersions: Record<string, number> = {};
        Object.keys(patch).forEach((key) => {
            const version = (patchRequestVersionsRef.current[key] || 0) + 1;
            patchRequestVersionsRef.current[key] = version;
            requestVersions[key] = version;
        });
        return requestVersions;
    };

    const persistAndConfirm = (patch: Record<string, any>, context: string, requestVersions: Record<string, number>) => {
        void persistConfigPatch(patch, context).then((saved) => {
            const current = configRef.current;
            if (!current) return;

            // A user can click the same switch again before its first save returns.
            // Only the newest response for each field is allowed to confirm UI state.
            const savedConfig = new corelib.AppConfig(saved);
            const acceptedPatch: Record<string, any> = {};
            const acceptedOriginalPatch: Record<string, any> = {};
            Object.entries(patch).forEach(([key, value]) => {
                if (patchRequestVersionsRef.current[key] !== requestVersions[key]) return;
                acceptedPatch[key] = (savedConfig as Record<string, any>)[key];
                acceptedOriginalPatch[key] = value;
            });
            if (Object.keys(acceptedPatch).length === 0) return;

            // Preserve newer optimistic edits to unrelated fields when an older,
            // full-config response arrives from the backend.
            const confirmed = new corelib.AppConfig({ ...current, ...acceptedPatch });
            configRef.current = confirmed;
            setConfig(confirmed);
            clearConfirmedPatch(acceptedOriginalPatch);
            // Keep other surfaces (e.g. the quick-settings bar) in sync only
            // after filtering stale responses.
            window.dispatchEvent(new CustomEvent(EVENT_MACLAW_CONFIG_CHANGED, { detail: confirmed }));
        }).catch(() => {
            clearFailedPatch(patch, requestVersions);
        });
    };

    const saveConfigPatch = (patch: Record<string, any>, persist = true) => {
        const current = configRef.current;
        if (!current) return null;
        const next = new corelib.AppConfig({ ...current, ...patch });
        configRef.current = next;
        const nextPending = { ...pendingPatchRef.current, ...patch };
        pendingPatchRef.current = nextPending;
        setPendingPatch(nextPending);
        setConfig(next);
        if (persist) persistAndConfirm(patch, 'general settings', trackPatchRequest(patch));
        return next;
    };

    const persistWorkingDirectory = () => {
        const current = configRef.current;
        if (!current) return;
        const patch = { working_directory: current.working_directory };
        persistAndConfirm(patch, 'working directory', trackPatchRequest(patch));
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
                    <input type="checkbox" checked={effectiveConfig?.show_app_entry !== false} onChange={(e) => saveConfigPatch({ show_app_entry: e.target.checked })} />
                    <span>{miniAppEntryLabel(lang)}</span>
                </label>
                <label className="general-settings-option general-settings-option--inline general-settings-option--plain">
                    <input type="checkbox" checked={effectiveConfig?.show_workflow_entry !== false} onChange={(e) => saveConfigPatch({ show_workflow_entry: e.target.checked })} />
                    <span>{textForLang(lang, 'Workflow entry', '\u5de5\u4f5c\u6d41\u5165\u53e3', '\u5de5\u4f5c\u6d41\u5165\u53e3')}</span>
                </label>
                <label className="general-settings-option general-settings-option--inline general-settings-option--plain">
                    <input type="checkbox" checked={effectiveConfig?.show_utilities_entry !== false} onChange={(e) => saveConfigPatch({ show_utilities_entry: e.target.checked })} />
                    <span>{textForLang(lang, 'Utilities entry', '\u5b9e\u7528\u5de5\u5177\u5165\u53e3', '\u5be6\u7528\u5de5\u5177\u5165\u53e3')}</span>
                </label>
                <label className="general-settings-option general-settings-option--inline general-settings-option--plain">
                    <input type="checkbox" checked={effectiveConfig?.survey_enabled !== false} onChange={(e) => saveConfigPatch({ survey_enabled: e.target.checked })} />
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
