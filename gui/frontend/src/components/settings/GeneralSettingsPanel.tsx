import { LoadConfig, PatchConfigFields, SelectWorkingDir } from '../../../wailsjs/go/main/App';
import { type ChangeEvent, type Dispatch, type SetStateAction, useMemo, useRef, useState } from 'react';
import { corelib, main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { EVENT_MACLAW_CONFIG_CHANGED } from '../../constants/events';
import { miniAppEntryLabel } from '../../i18n/maclawMiniAppLabels';
import { utilitiesEntryLabel } from '../../i18n/utilitiesLabels';
import { GeneralSettingsOptionGrid } from './GeneralSettingsOptionGrid';

type GeneralSettingsPanelProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    t: (key: string) => string;
    onLanguageChange: (event: ChangeEvent<HTMLSelectElement>) => void;
};

const textForLang = localizeText;

const OFFICE_READ_FORMATS = [
    { value: 'ppt', label: 'PowerPoint 97–2003 (.ppt)' },
    { value: 'doc', label: 'Word 97–2003 (.doc)' },
    { value: 'xls', label: 'Excel 97–2003 (.xls)' },
    { value: 'docx', label: 'Word (.docx)' },
    { value: 'xlsx', label: 'Excel (.xlsx)' },
    { value: 'pptx', label: 'PowerPoint (.pptx)' },
] as const;

// The host deliberately interprets an absent or empty allowlist as the full
// supported OfficeRead scope. Keep the settings UI on the same contract:
// displaying no selected format would imply that OfficeRead has been disabled
// while every supported format is still routed to it at runtime.
const normalizedOfficeReadFormats = (formats: string[] | undefined): string[] => {
    const normalized = (formats || []).map((format) => format.trim().toLowerCase().replace(/^\./, '')).filter(Boolean);
    return normalized.length > 0 ? normalized : OFFICE_READ_FORMATS.map(({ value }) => value);
};

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

    const updateOfficeReadFormat = (format: string, enabled: boolean) => {
        const selected = new Set(normalizedOfficeReadFormats(effectiveConfig?.office_read_formats));
        // An empty persisted allowlist is defined as the full default
        // OfficeRead scope, rather than a way to disable OfficeRead. The engine
        // selector is the explicit global kill switch, so never write an
        // empty list from this control and create a misleading UI state.
        if (!enabled && selected.size === 1 && selected.has(format)) return;
        if (enabled) selected.add(format);
        else selected.delete(format);
        saveConfigPatch({ office_read_formats: OFFICE_READ_FORMATS.filter(({ value }) => selected.has(value)).map(({ value }) => value) });
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
                    <span>{utilitiesEntryLabel(lang)}</span>
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

        <section className="general-settings-card general-settings-office-read" aria-labelledby="office-read-settings-title">
            <div className="general-settings-office-read__heading">
                <div>
                    <h3 id="office-read-settings-title">{textForLang(lang, 'Office document extraction', 'Office 文档抽取', 'Office 文件擷取')}</h3>
                    <p>{textForLang(lang,
                        'Choose the rollout mode for Word, Excel, and PowerPoint extraction. Chat attachments always use plain text and never receive embedded images or Markdown.',
                        '选择 Word、Excel 与 PowerPoint 抽取的灰度模式。聊天附件始终只使用纯文本，不会接收嵌入图片或 Markdown。',
                        '選擇 Word、Excel 與 PowerPoint 擷取的灰度模式。聊天附件一律只使用純文字，不會接收內嵌圖片或 Markdown。',
                    )}</p>
                </div>
                <label className="general-settings-field general-settings-office-read__engine">
                    <span>{textForLang(lang, 'Engine', '引擎', '引擎')}</span>
                    <select
                        aria-label={textForLang(lang, 'Office extraction engine', 'Office 文档抽取引擎', 'Office 文件擷取引擎')}
                        className="form-input"
                        value={effectiveConfig?.office_read_engine || 'officeread'}
                        onChange={(e) => saveConfigPatch({ office_read_engine: e.target.value })}
                    >
                        <option value="legacy">{textForLang(lang, 'Legacy only', '仅旧引擎', '僅舊引擎')}</option>
                        <option value="dual">{textForLang(lang, 'Dual read (compare safely)', '双读（安全对比）', '雙讀（安全比對）')}</option>
                        <option value="officeread">OfficeRead</option>
                    </select>
                </label>
            </div>

            <fieldset className="general-settings-office-read__formats">
                <legend>{textForLang(lang, 'Use OfficeRead for', '使用 OfficeRead 的格式', '使用 OfficeRead 的格式')}</legend>
                <div>
                    {OFFICE_READ_FORMATS.map(({ value, label }) => {
                        const selected = normalizedOfficeReadFormats(effectiveConfig?.office_read_formats).includes(value);
                        const isOnlySelectedFormat = selected && normalizedOfficeReadFormats(effectiveConfig?.office_read_formats).length === 1;
                        return <label key={value} className="general-settings-office-read__format">
                            <input
                                type="checkbox"
                                aria-label={`${textForLang(lang, 'Use OfficeRead for', '使用 OfficeRead 的格式', '使用 OfficeRead 的格式')} ${label}`}
                                checked={selected}
                                disabled={isOnlySelectedFormat}
                                onChange={(e) => updateOfficeReadFormat(value, e.target.checked)}
                            />
                            <span>{label}</span>
                        </label>;
                    })}
                </div>
            </fieldset>
            <small className="general-settings-office-read__scope-note">
                {textForLang(lang,
                    'At least one Office format remains selected. Choose Legacy only to disable OfficeRead for every format.',
                    '至少保留一种 Office 格式。若要对所有格式关闭 OfficeRead，请选择“仅旧引擎”。',
                    '至少保留一種 Office 格式。若要對所有格式關閉 OfficeRead，請選擇「僅舊引擎」。',
                )}
            </small>

            <div className="general-settings-office-read__options">
                <label className="general-settings-office-read__option">
                    <input
                        type="checkbox"
                        aria-label={textForLang(lang, 'Fall back to legacy extraction', 'OfficeRead 失败时回退旧引擎', 'OfficeRead 失敗時回退舊引擎')}
                        checked={effectiveConfig?.office_read_fallback !== false}
                        onChange={(e) => saveConfigPatch({ office_read_fallback: e.target.checked })}
                    />
                    <span>{textForLang(lang, 'Fall back to legacy extraction', 'OfficeRead 失败时回退旧引擎', 'OfficeRead 失敗時回退舊引擎')}</span>
                    <small>{textForLang(lang, 'Keep enabled during rollout so an OfficeRead failure can use the existing reader.', '灰度期间请保持开启，OfficeRead 失败时会使用现有读取器。', '灰度期間請保持開啟，OfficeRead 失敗時會使用現有讀取器。')}</small>
                </label>
                <label className="general-settings-office-read__option">
                    <input
                        type="checkbox"
                        aria-label={textForLang(lang, 'Use structured Markdown in Knowledge', '知识库使用结构化 Markdown', '知識庫使用結構化 Markdown')}
                        checked={effectiveConfig?.office_read_emit_markdown === true}
                        onChange={(e) => saveConfigPatch({ office_read_emit_markdown: e.target.checked })}
                    />
                    <span>{textForLang(lang, 'Use structured Markdown in Knowledge', '知识库使用结构化 Markdown', '知識庫使用結構化 Markdown')}</span>
                    <small>{textForLang(lang, 'Opt-in for knowledge imports only; it never adds Markdown or images to chat context.', '仅用于知识库导入；绝不会向聊天上下文添加 Markdown 或图片。', '僅用於知識庫匯入；絕不會向聊天上下文加入 Markdown 或圖片。')}</small>
                </label>
            </div>
        </section>

        <GeneralSettingsOptionGrid effectiveConfig={effectiveConfig} lang={lang} saveConfigPatch={saveConfigPatch} />
    </div>;
};
