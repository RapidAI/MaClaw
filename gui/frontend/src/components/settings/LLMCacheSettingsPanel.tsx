import { type Dispatch, type ReactNode, type SetStateAction, useState } from 'react';
import { PatchConfigFields } from '../../../wailsjs/go/main/App';
import { corelib, main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { EVENT_MACLAW_CONFIG_CHANGED } from '../../constants/events';
import { ModelRoutesSettingsSection } from './ModelRoutesSettingsSection';

type LLMCacheSettingsPanelProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    showToastMessage?: (message: string, duration?: number) => void;
};

const textForLang = localizeText;

const defaults = {
    enabled: false,
    openai_enabled: true,
    anthropic_enabled: true,
    stream_synthesis_enabled: true,
    cache_dir: '~/.maclaw/llm_prompt_cache',
    ttl_seconds: 1800,
    memory_max_entries: 256,
    memory_max_bytes: 8 * 1024 * 1024,
    disk_max_bytes: 64 * 1024 * 1024,
    normalize_deterministic_params: true,
    ignore_model_field: true,
    ignore_user_field: true,
    ignore_metadata_field: true,
    singleflight_wait_timeout_ms: 15000,
};

const mb = 1024 * 1024;
const defaultCacheDir = defaults.cache_dir;

const normalizeCache = (value: any = {}) => {
    const merged = { ...defaults, ...value };
    return {
        ...merged,
        enabled: !!merged.enabled,
        openai_enabled: merged.openai_enabled ?? defaults.openai_enabled,
        anthropic_enabled: merged.anthropic_enabled ?? defaults.anthropic_enabled,
        stream_synthesis_enabled: merged.stream_synthesis_enabled ?? defaults.stream_synthesis_enabled,
        normalize_deterministic_params: merged.normalize_deterministic_params ?? defaults.normalize_deterministic_params,
        ignore_model_field: merged.ignore_model_field ?? defaults.ignore_model_field,
        ignore_user_field: merged.ignore_user_field ?? defaults.ignore_user_field,
        ignore_metadata_field: merged.ignore_metadata_field ?? defaults.ignore_metadata_field,
        ttl_seconds: merged.ttl_seconds || defaults.ttl_seconds,
        memory_max_entries: merged.memory_max_entries || defaults.memory_max_entries,
        memory_max_bytes: merged.memory_max_bytes || defaults.memory_max_bytes,
        disk_max_bytes: merged.disk_max_bytes || defaults.disk_max_bytes,
        singleflight_wait_timeout_ms: merged.singleflight_wait_timeout_ms || defaults.singleflight_wait_timeout_ms,
    };
};

const normalizeSwitchPatch = (current: any, patch: Record<string, any>) => {
    const next = { ...current, ...patch };
    if (patch.enabled === true && !next.openai_enabled && !next.anthropic_enabled && !next.stream_synthesis_enabled) {
        next.openai_enabled = defaults.openai_enabled;
        next.anthropic_enabled = defaults.anthropic_enabled;
        next.stream_synthesis_enabled = defaults.stream_synthesis_enabled;
    }
    if (!next.openai_enabled && !next.anthropic_enabled && !next.stream_synthesis_enabled) {
        next.enabled = false;
    }
    return next;
};

export const LLMCacheSettingsPanel = ({ config, setConfig, lang, showToastMessage }: LLMCacheSettingsPanelProps) => {
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState('');
    const cache = normalizeCache((config as any)?.llm_prompt_cache);
    const updateCache = (patch: Record<string, any>) => {
        const nextCache = normalizeSwitchPatch(cache, patch);
        setConfig((previous) => new corelib.AppConfig({ ...(previous || config || {}), llm_prompt_cache: nextCache }));
    };
    const numberValue = (key: string, divisor = 1) => Math.round((cache as any)[key] / divisor);
    const updateNumber = (key: string, raw: string, multiplier = 1) => {
        const parsed = Number(raw);
        if (!Number.isFinite(parsed)) return;
        updateCache({ [key]: Math.max(0, Math.round(parsed * multiplier)) });
    };
    const saveCacheConfig = async () => {
        if (!config || saving) return;
        setSaving(true);
        setSaveError('');
        try {
            const saved = await PatchConfigFields({ llm_prompt_cache: normalizeSwitchPatch(cache, {}) });
            setConfig(new corelib.AppConfig(saved));
            // Keep other surfaces (e.g. the quick-settings bar) in sync with this change.
            window.dispatchEvent(new CustomEvent(EVENT_MACLAW_CONFIG_CHANGED, { detail: saved }));
            showToastMessage?.(textForLang(lang, 'Saved successfully', '\u4fdd\u5b58\u6210\u529f', '\u5132\u5b58\u6210\u529f'));
        } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            setSaveError(message);
            showToastMessage?.(message, 5000);
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="settings-panel" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
            <label className="proxy-settings-enable">
                <span className="proxy-settings-enable__text">
                    {textForLang(lang, 'Enable local LLM cache', '\u542f\u7528\u672c\u5730 LLM \u7f13\u5b58', '\u555f\u7528\u672c\u5730 LLM \u5feb\u53d6')}
                </span>
                <span className="proxy-settings-switch">
                    <input
                        type="checkbox"
                        aria-label={textForLang(lang, 'Enable local LLM cache', '\u542f\u7528\u672c\u5730 LLM \u7f13\u5b58', '\u555f\u7528\u672c\u5730 LLM \u5feb\u53d6')}
                        checked={cache.enabled}
                        onChange={(e) => updateCache({ enabled: e.target.checked })}
                    />
                    <span aria-hidden="true" />
                </span>
            </label>

            <div style={{ display: 'grid', gap: 10 }}>
                <Check label={textForLang(lang, 'OpenAI-compatible non-stream', 'OpenAI \u517c\u5bb9\u975e\u6d41\u5f0f', 'OpenAI \u76f8\u5bb9\u975e\u4e32\u6d41')} checked={cache.openai_enabled} onChange={(v) => updateCache({ openai_enabled: v })} />
                <Check label={textForLang(lang, 'Anthropic non-stream', 'Anthropic \u975e\u6d41\u5f0f', 'Anthropic \u975e\u4e32\u6d41')} checked={cache.anthropic_enabled} onChange={(v) => updateCache({ anthropic_enabled: v })} />
                <Check label={textForLang(lang, 'Synthesize stream on cache hit', '\u6d41\u5f0f\u8bf7\u6c42\u547d\u4e2d\u540e\u5408\u6210\u8f93\u51fa', '\u4e32\u6d41\u8acb\u6c42\u547d\u4e2d\u5f8c\u5408\u6210\u8f38\u51fa')} checked={cache.stream_synthesis_enabled} onChange={(v) => updateCache({ stream_synthesis_enabled: v })} />
            </div>

            <div style={{ color: 'var(--text-secondary)', fontSize: 13, lineHeight: 1.5 }}>
                {textForLang(
                    lang,
                    'Caches deterministic OpenAI-compatible and Anthropic requests. Streaming requests use cached full responses when hit. MaClaw official Hub service is always bypassed.',
                    '\u7f13\u5b58\u786e\u5b9a\u6027\u7684 OpenAI \u517c\u5bb9\u548c Anthropic \u8bf7\u6c42\u3002\u6d41\u5f0f\u8bf7\u6c42\u547d\u4e2d\u540e\u4f7f\u7528\u5b8c\u6574\u54cd\u5e94\u5408\u6210\u8f93\u51fa\u3002MaClaw \u5b98\u65b9 Hub \u670d\u52a1\u59cb\u7ec8\u7ed5\u8fc7\u672c\u5730\u7f13\u5b58\u3002',
                    '\u5feb\u53d6\u78ba\u5b9a\u6027\u7684 OpenAI \u76f8\u5bb9\u548c Anthropic \u8acb\u6c42\u3002\u4e32\u6d41\u8acb\u6c42\u547d\u4e2d\u5f8c\u4f7f\u7528\u5b8c\u6574\u56de\u61c9\u5408\u6210\u8f38\u51fa\u3002MaClaw \u5b98\u65b9 Hub \u670d\u52d9\u59cb\u7d42\u7e5e\u904e\u672c\u5730\u5feb\u53d6\u3002'
                )}
            </div>

            <div className="proxy-settings-grid llm-cache-settings-dir-grid">
                <Field label={textForLang(lang, 'Cache directory', '\u7f13\u5b58\u76ee\u5f55', '\u5feb\u53d6\u76ee\u9304')}>
                    <div className="llm-cache-settings-dir-row">
                        <input className="form-input llm-cache-settings-dir-input" type="text" value={cache.cache_dir} title={cache.cache_dir} onChange={(e) => updateCache({ cache_dir: e.target.value })} />
                        <button className="btn-secondary llm-cache-settings-dir-reset" type="button" onClick={() => updateCache({ cache_dir: defaultCacheDir })}>
                            {textForLang(lang, 'Restore default', '\u6062\u590d\u9ed8\u8ba4', '\u6062\u5fa9\u9810\u8a2d')}
                        </button>
                    </div>
                </Field>
            </div>

            <div className="proxy-settings-grid proxy-settings-grid--server">
                <Field label={textForLang(lang, 'TTL seconds', '\u7f13\u5b58\u79d2\u6570', '\u5feb\u53d6\u79d2\u6578')}>
                    <input className="form-input" type="number" min={1} value={cache.ttl_seconds} onChange={(e) => updateNumber('ttl_seconds', e.target.value)} />
                </Field>
                <Field label={textForLang(lang, 'Memory entries', '\u5185\u5b58\u6761\u76ee', '\u8a18\u61b6\u9ad4\u689d\u76ee')}>
                    <input className="form-input" type="number" min={1} value={cache.memory_max_entries} onChange={(e) => updateNumber('memory_max_entries', e.target.value)} />
                </Field>
                <Field label={textForLang(lang, 'Singleflight wait ms', '\u5408\u5e76\u7b49\u5f85\u6beb\u79d2', '\u5408\u4f75\u7b49\u5f85\u6beb\u79d2')}>
                    <input className="form-input" type="number" min={0} value={cache.singleflight_wait_timeout_ms} onChange={(e) => updateNumber('singleflight_wait_timeout_ms', e.target.value)} />
                </Field>
            </div>

            <div className="proxy-settings-grid proxy-settings-grid--server">
                <Field label={textForLang(lang, 'Memory MB', '\u5185\u5b58 MB', '\u8a18\u61b6\u9ad4 MB')}>
                    <input className="form-input" type="number" min={1} value={numberValue('memory_max_bytes', mb)} onChange={(e) => updateNumber('memory_max_bytes', e.target.value, mb)} />
                </Field>
                <Field label={textForLang(lang, 'Disk MB', '\u78c1\u76d8 MB', '\u78c1\u789f MB')}>
                    <input className="form-input" type="number" min={1} value={numberValue('disk_max_bytes', mb)} onChange={(e) => updateNumber('disk_max_bytes', e.target.value, mb)} />
                </Field>
            </div>

            <div style={{ display: 'grid', gap: 10 }}>
                <Check label={textForLang(lang, 'Normalize default deterministic parameters', '\u5f52\u4e00\u5316\u9ed8\u8ba4\u786e\u5b9a\u6027\u53c2\u6570', '\u6b78\u4e00\u5316\u9810\u8a2d\u78ba\u5b9a\u6027\u53c3\u6578')} checked={cache.normalize_deterministic_params} onChange={(v) => updateCache({ normalize_deterministic_params: v })} />
                <Check label={textForLang(lang, 'Ignore model field in cache key', '\u7f13\u5b58\u952e\u5ffd\u7565 model \u5b57\u6bb5', '\u5feb\u53d6\u9375\u5ffd\u7565 model \u6b04\u4f4d')} checked={cache.ignore_model_field} onChange={(v) => updateCache({ ignore_model_field: v })} />
                <Check label={textForLang(lang, 'Ignore user field in cache key', '\u7f13\u5b58\u952e\u5ffd\u7565 user \u5b57\u6bb5', '\u5feb\u53d6\u9375\u5ffd\u7565 user \u6b04\u4f4d')} checked={cache.ignore_user_field} onChange={(v) => updateCache({ ignore_user_field: v })} />
                <Check label={textForLang(lang, 'Ignore metadata field in cache key', '\u7f13\u5b58\u952e\u5ffd\u7565 metadata \u5b57\u6bb5', '\u5feb\u53d6\u9375\u5ffd\u7565 metadata \u6b04\u4f4d')} checked={cache.ignore_metadata_field} onChange={(v) => updateCache({ ignore_metadata_field: v })} />
            </div>

            <div className="proxy-settings-actions">
                <button className="btn-primary" disabled={saving || !config} onClick={saveCacheConfig}>
                    {saving ? textForLang(lang, 'Saving...', '\u4fdd\u5b58\u4e2d...', '\u4fdd\u5b58\u4e2d...') : textForLang(lang, 'Save', '\u4fdd\u5b58', '\u4fdd\u5b58')}
                </button>
            </div>
            {saveError && <div style={{ color: 'var(--error-color, #c43d34)', fontSize: 13 }}>{saveError}</div>}

            <hr style={{ border: 'none', borderTop: '1px solid var(--border-color, rgba(127,127,127,0.25))', margin: '8px 0' }} />
            <ModelRoutesSettingsSection
                config={config}
                setConfig={setConfig}
                lang={lang}
                showToastMessage={showToastMessage}
            />
        </div>
    );
};

const Field = ({ label, children }: { label: string; children: ReactNode }) => (
    <div className="proxy-settings-field">
        <label className="form-label">{label}</label>
        {children}
    </div>
);

const Check = ({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) => (
    <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13 }}>
        <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
        <span>{label}</span>
    </label>
);
