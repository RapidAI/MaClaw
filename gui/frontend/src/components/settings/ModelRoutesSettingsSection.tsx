import { GetCodingWorkbenchCheckpointSidecarStats, GetCodingWorkbenchRouteMap, PatchConfigFields } from '../../../wailsjs/go/main/App';
import { type Dispatch, type SetStateAction, useEffect, useMemo, useState } from 'react';
import { corelib, main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';

type RouteRow = {
    task: string;
    model: string;
    url: string;
    protocol: string;
    provider: string;
    contextLength: string;
};

type ModelRoutesSettingsSectionProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    showToastMessage?: (message: string, duration?: number) => void;
};

const textForLang = localizeText;

const KNOWN_TASKS = ['reasoning', 'vision', 'fast', 'intent', 'summary', 'default'] as const;

const emptyRow = (task = ''): RouteRow => ({
    task,
    model: '',
    url: '',
    protocol: '',
    provider: '',
    contextLength: '',
});

function routesFromConfig(config: corelib.AppConfig | null): RouteRow[] {
    const raw = ((config as any)?.model_routes || {}) as Record<string, any>;
    const keys = Object.keys(raw || {});
    if (keys.length === 0) {
        return [emptyRow('reasoning'), emptyRow('vision')];
    }
    return keys
        .sort((a, b) => a.localeCompare(b))
        .map((task) => {
            const v = raw[task] || {};
            return {
                task,
                model: String(v.model || ''),
                url: String(v.url || ''),
                protocol: String(v.protocol || ''),
                provider: String(v.provider || ''),
                contextLength: Number(v.context_length) > 0 ? String(v.context_length) : '',
            };
        });
}

function rowsToRoutes(rows: RouteRow[]): Record<string, { model: string; url?: string; protocol?: string; provider?: string; context_length?: number }> {
    const out: Record<string, { model: string; url?: string; protocol?: string; provider?: string; context_length?: number }> = {};
    for (const row of rows) {
        const task = row.task.trim().toLowerCase();
        const model = row.model.trim();
        if (!task || !model) continue;
        out[task] = {
            model,
            ...(row.url.trim() ? { url: row.url.trim() } : {}),
            ...(row.protocol.trim() ? { protocol: row.protocol.trim() } : {}),
            ...(row.provider.trim() ? { provider: row.provider.trim() } : {}),
            ...(Number(row.contextLength) > 0 ? { context_length: Math.floor(Number(row.contextLength)) } : {}),
        };
    }
    return out;
}

/**
 * Editable ModelRoutes map for pure-coding route prefs (reasoning/vision/…).
 * Lives under LLM Cache settings so operators can wire ModelRouter without restart.
 */
type RouteCapPreview = {
    pref: string;
    available?: boolean;
    model?: string;
    source?: string;
    note?: string;
};

export const ModelRoutesSettingsSection = ({
    config,
    setConfig,
    lang,
    showToastMessage,
}: ModelRoutesSettingsSectionProps) => {
    const [rows, setRows] = useState<RouteRow[]>(() => routesFromConfig(config));
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState('');
    const [preview, setPreview] = useState<RouteCapPreview[]>([]);
    const [previewLoading, setPreviewLoading] = useState(false);
    const [sidecarStats, setSidecarStats] = useState<{
        total_bytes?: number;
        max_bytes?: number;
        usage_ratio?: number;
        dir_count?: number;
    } | null>(null);
    const [sidecarLoading, setSidecarLoading] = useState(false);
    const [defaultPref, setDefaultPref] = useState<string>(() => {
        const raw = String((config as any)?.coding_route_pref || 'auto').toLowerCase();
        return raw === 'primary' || raw === 'reasoning' || raw === 'vision' ? raw : 'auto';
    });
    const [prefSaving, setPrefSaving] = useState(false);
    // Resync when parent config identity changes (after load/save elsewhere).
    const configKey = useMemo(
        () => JSON.stringify({
            routes: (config as any)?.model_routes || {},
            pref: (config as any)?.coding_route_pref || 'auto',
            mirror: !!(config as any)?.coding_route_pref_mirror,
            sidecar_mb: Number((config as any)?.coding_checkpoint_sidecar_max_mb) || 0,
        }),
        [config],
    );
    const [lastKey, setLastKey] = useState(configKey);
    if (configKey !== lastKey) {
        setLastKey(configKey);
        setRows(routesFromConfig(config));
        const raw = String((config as any)?.coding_route_pref || 'auto').toLowerCase();
        setDefaultPref(raw === 'primary' || raw === 'reasoning' || raw === 'vision' ? raw : 'auto');
    }

    const refreshPreview = async () => {
        setPreviewLoading(true);
        try {
            const caps = await GetCodingWorkbenchRouteMap('');
            setPreview(Array.isArray(caps) ? caps.map((c: any) => ({
                pref: String(c.pref || ''),
                available: c.available !== false,
                model: String(c.model || ''),
                source: String(c.source || ''),
                note: String(c.note || ''),
            })) : []);
        } catch {
            setPreview([]);
        } finally {
            setPreviewLoading(false);
        }
    };

    const refreshSidecarStats = async () => {
        setSidecarLoading(true);
        try {
            const st = await GetCodingWorkbenchCheckpointSidecarStats('');
            setSidecarStats(st ? {
                total_bytes: Number(st.total_bytes) || 0,
                max_bytes: Number(st.max_bytes) || 0,
                usage_ratio: Number(st.usage_ratio) || 0,
                dir_count: Number(st.dir_count) || 0,
            } : null);
        } catch {
            setSidecarStats(null);
        } finally {
            setSidecarLoading(false);
        }
    };

    useEffect(() => {
        void refreshPreview();
        void refreshSidecarStats();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [configKey]);

    const updateRow = (index: number, patch: Partial<RouteRow>) => {
        setRows((prev) => prev.map((r, i) => (i === index ? { ...r, ...patch } : r)));
    };

    const save = async () => {
        if (!config || saving) return;
        setSaving(true);
        setSaveError('');
        try {
            const model_routes = rowsToRoutes(rows);
            const saved = await PatchConfigFields({ model_routes });
            setConfig(new corelib.AppConfig(saved));
            setRows(routesFromConfig(saved as any));
            await refreshPreview();
            showToastMessage?.(textForLang(lang, 'Model routes saved', '模型路由已保存', '模型路由已儲存'));
        } catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            setSaveError(message);
            showToastMessage?.(message, 5000);
        } finally {
            setSaving(false);
        }
    };

    return (
        <div data-testid="model-routes-settings" style={{ display: 'flex', flexDirection: 'column', gap: 12, marginTop: 8 }}>
            <div>
                <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 4 }}>
                    {textForLang(lang, 'Model routes (ModelRouter)', '模型路由 (ModelRouter)', '模型路由 (ModelRouter)')}
                </div>
                <div style={{ color: 'var(--theme-text-secondary)', fontSize: 12, lineHeight: 1.45 }}>
                    {textForLang(
                        lang,
                        'Per-task model overrides for pure-coding route prefs (auto/primary/reasoning/vision). Empty model fields are ignored. Leave URL empty to inherit the primary provider.',
                        '按任务类型覆盖模型，供编程工作台选模（auto/primary/reasoning/vision）。空 model 会被忽略；URL 留空则继承主模型提供商。',
                        '依任務類型覆寫模型，供程式工作台選模（auto/primary/reasoning/vision）。空 model 會被忽略；URL 留空則繼承主模型提供商。',
                    )}
                </div>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {rows.map((row, index) => (
                    <div
                        key={`route-${index}`}
                        data-testid={`model-route-row-${index}`}
                        style={{
                            display: 'grid',
                            gridTemplateColumns: 'minmax(90px, 0.9fr) minmax(130px, 1.3fr) minmax(120px, 1.15fr) minmax(90px, 0.8fr) auto',
                            gap: 8,
                            alignItems: 'end',
                        }}
                    >
                        <label className="proxy-settings-field" style={{ margin: 0 }}>
                            <span className="form-label">{textForLang(lang, 'Task', '任务类型', '任務類型')}</span>
                            <input
                                className="form-input"
                                list="maclaw-model-route-tasks"
                                value={row.task}
                                placeholder="reasoning"
                                onChange={(e) => updateRow(index, { task: e.target.value })}
                            />
                        </label>
                        <label className="proxy-settings-field" style={{ margin: 0 }}>
                            <span className="form-label">{textForLang(lang, 'Context (tokens)', '上下文（token）', '上下文（token）')}</span>
                            <input
                                className="form-input"
                                type="number"
                                min="0"
                                step="1000"
                                value={row.contextLength}
                                placeholder="inherit primary"
                                onChange={(e) => updateRow(index, { contextLength: e.target.value })}
                            />
                        </label>
                        <label className="proxy-settings-field" style={{ margin: 0 }}>
                            <span className="form-label">{textForLang(lang, 'Model', '模型', '模型')}</span>
                            <input
                                className="form-input"
                                value={row.model}
                                placeholder="e.g. deepseek-coder"
                                onChange={(e) => updateRow(index, { model: e.target.value })}
                            />
                        </label>
                        <label className="proxy-settings-field" style={{ margin: 0 }}>
                            <span className="form-label">{textForLang(lang, 'URL (optional)', 'URL（可选）', 'URL（可選）')}</span>
                            <input
                                className="form-input"
                                value={row.url}
                                placeholder="inherit primary"
                                onChange={(e) => updateRow(index, { url: e.target.value })}
                            />
                        </label>
                        <button
                            type="button"
                            className="btn-secondary"
                            data-testid={`model-route-remove-${index}`}
                            onClick={() => setRows((prev) => (prev.length <= 1 ? [emptyRow()] : prev.filter((_, i) => i !== index)))}
                            style={{ height: 32 }}
                        >
                            {textForLang(lang, 'Remove', '删除', '刪除')}
                        </button>
                    </div>
                ))}
            </div>
            <datalist id="maclaw-model-route-tasks">
                {KNOWN_TASKS.map((t) => (
                    <option key={t} value={t} />
                ))}
            </datalist>

            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                <button
                    type="button"
                    className="btn-secondary"
                    data-testid="model-route-add"
                    onClick={() => setRows((prev) => [...prev, emptyRow()])}
                >
                    {textForLang(lang, 'Add route', '添加路由', '新增路由')}
                </button>
                <button
                    type="button"
                    className="btn-primary"
                    data-testid="model-route-save"
                    disabled={saving || !config}
                    onClick={() => { void save(); }}
                >
                    {saving
                        ? textForLang(lang, 'Saving...', '保存中...', '儲存中...')
                        : textForLang(lang, 'Save model routes', '保存模型路由', '儲存模型路由')}
                </button>
            </div>
            {saveError ? (
                <div style={{ color: 'var(--theme-danger, #c43d34)', fontSize: 13 }}>{saveError}</div>
            ) : null}

            <div data-testid="coding-route-pref-default" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
                    <span style={{ fontWeight: 600, fontSize: 13 }}>
                        {textForLang(lang, 'Default workbench model pref', '默认编程选模', '預設程式選模')}
                    </span>
                    {(['auto', 'primary', 'reasoning', 'vision'] as const).map((pref) => (
                        <button
                            key={pref}
                            type="button"
                            data-testid={`coding-route-pref-default-${pref}`}
                            disabled={prefSaving}
                            onClick={async () => {
                                if (!config || prefSaving) return;
                                setPrefSaving(true);
                                try {
                                    const saved = await PatchConfigFields({ coding_route_pref: pref });
                                    setConfig(new corelib.AppConfig(saved));
                                    setDefaultPref(pref);
                                    showToastMessage?.(textForLang(lang, `Default pref: ${pref}`, `默认选模: ${pref}`, `預設選模: ${pref}`));
                                } catch (err) {
                                    const message = err instanceof Error ? err.message : String(err);
                                    showToastMessage?.(message, 5000);
                                } finally {
                                    setPrefSaving(false);
                                }
                            }}
                            style={{
                                height: 26,
                                padding: '0 10px',
                                borderRadius: 4,
                                border: `1px solid ${defaultPref === pref ? 'var(--theme-primary)' : 'var(--theme-border, rgba(127,127,127,0.3))'}`,
                                background: defaultPref === pref ? 'color-mix(in srgb, var(--theme-primary) 16%, transparent)' : 'transparent',
                                fontWeight: defaultPref === pref ? 600 : 400,
                                fontSize: 12,
                                cursor: prefSaving ? 'wait' : 'pointer',
                            }}
                        >
                            {pref}
                        </button>
                    ))}
                </div>
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12 }}>
                    <input
                        type="checkbox"
                        data-testid="coding-route-pref-mirror"
                        checked={!!(config as any)?.coding_route_pref_mirror}
                        disabled={prefSaving}
                        onChange={async (e) => {
                            if (!config || prefSaving) return;
                            setPrefSaving(true);
                            try {
                                const saved = await PatchConfigFields({ coding_route_pref_mirror: e.target.checked });
                                setConfig(new corelib.AppConfig(saved));
                            } catch (err) {
                                const message = err instanceof Error ? err.message : String(err);
                                showToastMessage?.(message, 5000);
                            } finally {
                                setPrefSaving(false);
                            }
                        }}
                    />
                    <span>
                        {textForLang(
                            lang,
                            'Mirror session model pref → default (new sessions inherit last choice)',
                            '将会话选模同步为默认（新会话继承上次选择）',
                            '將工作階段選模同步為預設（新工作階段繼承上次選擇）',
                        )}
                    </span>
                </label>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center', fontSize: 12 }}>
                    <span style={{ fontWeight: 600 }}>
                        {textForLang(lang, 'Checkpoint sidecar cap (MB)', '检查点侧车上限 (MB)', '檢查點側車上限 (MB)')}
                    </span>
                    <input
                        className="form-input"
                        type="number"
                        min={0}
                        max={8192}
                        data-testid="coding-checkpoint-sidecar-max-mb"
                        defaultValue={Number((config as any)?.coding_checkpoint_sidecar_max_mb) > 0
                            ? Number((config as any).coding_checkpoint_sidecar_max_mb)
                            : 256}
                        style={{ width: 96 }}
                        onBlur={async (e) => {
                            if (!config) return;
                            const raw = Number(e.target.value);
                            const mb = Number.isFinite(raw) ? Math.max(0, Math.min(8192, Math.round(raw))) : 0;
                            try {
                                const saved = await PatchConfigFields({ coding_checkpoint_sidecar_max_mb: mb });
                                setConfig(new corelib.AppConfig(saved));
                                showToastMessage?.(textForLang(
                                    lang,
                                    mb > 0 ? `Sidecar cap: ${mb} MB` : 'Sidecar cap: default 256 MB',
                                    mb > 0 ? `侧车上限: ${mb} MB` : '侧车上限: 默认 256 MB',
                                    mb > 0 ? `側車上線: ${mb} MB` : '側車上線: 預設 256 MB',
                                ));
                            } catch (err) {
                                const message = err instanceof Error ? err.message : String(err);
                                showToastMessage?.(message, 5000);
                            }
                        }}
                    />
                    <span style={{ color: 'var(--theme-text-secondary)', fontSize: 11 }}>
                        {textForLang(lang, '0 = default 256; min effective 32', '0 = 默认 256；生效最小 32', '0 = 預設 256；生效最小 32')}
                    </span>
                </div>
                <div data-testid="coding-sidecar-usage-panel" style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                        <span style={{ fontWeight: 600, fontSize: 12 }}>
                            {textForLang(lang, 'Sidecar usage', '侧车用量', '側車用量')}
                        </span>
                        <button
                            type="button"
                            className="btn-secondary"
                            data-testid="coding-sidecar-usage-refresh"
                            disabled={sidecarLoading}
                            onClick={() => { void refreshSidecarStats(); }}
                            style={{ height: 24, fontSize: 11 }}
                        >
                            {sidecarLoading
                                ? textForLang(lang, 'Refreshing…', '刷新中…', '重新整理中…')
                                : textForLang(lang, 'Refresh', '刷新', '重新整理')}
                        </button>
                        {sidecarStats ? (
                            <span style={{ fontSize: 12, color: (Number(sidecarStats.usage_ratio) >= 0.85) ? 'var(--theme-danger, #c43d34)' : 'var(--theme-text-secondary)' }}>
                                {`${((Number(sidecarStats.total_bytes) || 0) / (1024 * 1024)).toFixed(1)} / ${((Number(sidecarStats.max_bytes) || 0) / (1024 * 1024)).toFixed(0)} MB`}
                                {Number(sidecarStats.dir_count) > 0 ? ` · ${sidecarStats.dir_count} labels` : ''}
                                {Number(sidecarStats.usage_ratio) > 0 ? ` · ${Math.round(Number(sidecarStats.usage_ratio) * 100)}%` : ''}
                            </span>
                        ) : (
                            <span style={{ fontSize: 12, color: 'var(--theme-text-secondary)' }}>
                                {textForLang(lang, 'No usage data', '暂无用量', '暫無用量')}
                            </span>
                        )}
                    </div>
                    {sidecarStats && Number(sidecarStats.max_bytes) > 0 ? (
                        <div
                            data-testid="coding-sidecar-usage-bar"
                            style={{
                                height: 8,
                                borderRadius: 4,
                                background: 'var(--theme-border, rgba(127,127,127,0.2))',
                                overflow: 'hidden',
                            }}
                        >
                            <div
                                style={{
                                    height: '100%',
                                    width: `${Math.min(100, Math.max(0, (Number(sidecarStats.usage_ratio) || 0) * 100))}%`,
                                    background: (Number(sidecarStats.usage_ratio) >= 0.85)
                                        ? 'var(--theme-danger, #c43d34)'
                                        : 'var(--theme-primary)',
                                    transition: 'width 0.2s ease',
                                }}
                            />
                        </div>
                    ) : null}
                </div>
                <span style={{ color: 'var(--theme-text-secondary)', fontSize: 11 }}>
                    {textForLang(
                        lang,
                        'Default pref applies when a pure-coding session has no sticky pref yet.',
                        '默认选模仅在编程会话尚未设置选模时生效。',
                        '預設選模僅在程式工作階段尚未設定選模時生效。',
                    )}
                </span>
            </div>

            <div data-testid="model-routes-preview" style={{ marginTop: 4 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                    <div style={{ fontWeight: 600, fontSize: 13 }}>
                        {textForLang(lang, 'Workbench resolution preview', '工作台解析预览', '工作台解析預覽')}
                    </div>
                    <button
                        type="button"
                        className="btn-secondary"
                        data-testid="model-routes-preview-refresh"
                        disabled={previewLoading}
                        onClick={() => { void refreshPreview(); }}
                        style={{ height: 26, fontSize: 11 }}
                    >
                        {previewLoading
                            ? textForLang(lang, 'Refreshing…', '刷新中…', '重新整理中…')
                            : textForLang(lang, 'Refresh', '刷新', '重新整理')}
                    </button>
                </div>
                {preview.length === 0 ? (
                    <div style={{ color: 'var(--theme-text-secondary)', fontSize: 12 }}>
                        {textForLang(lang, 'No resolution data yet.', '暂无解析结果。', '暫無解析結果。')}
                    </div>
                ) : (
                    <div style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                        {preview.map((c) => (
                            <div
                                key={c.pref}
                                data-testid={`model-route-preview-${c.pref}`}
                                style={{
                                    display: 'grid',
                                    gridTemplateColumns: '88px 1fr auto',
                                    gap: 8,
                                    padding: '4px 6px',
                                    borderRadius: 4,
                                    border: '1px solid var(--theme-border, rgba(127,127,127,0.2))',
                                }}
                            >
                                <strong>{c.pref}</strong>
                                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                    {c.model || '—'}
                                    {c.note ? ` · ${c.note}` : ''}
                                </span>
                                <span style={{ color: 'var(--theme-text-secondary)', opacity: 0.9 }}>{c.source || '—'}</span>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
};

export default ModelRoutesSettingsSection;
