import { CleanToolCacheNow, GetToolCacheStatus, LoadConfig, PatchConfigFields } from '../../../wailsjs/go/main/App';
import { useEffect, useMemo, useState } from 'react';
import { corelib, main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';

type ToolCacheStatus = {
    path?: string;
    exists?: boolean;
    size_bytes?: number;
    size_approximate?: boolean;
    auto_enabled?: boolean;
    max_bytes?: number;
    min_interval_hours?: number;
    clean_on_startup?: boolean;
    clean_on_exit?: boolean;
    last_cleanup_at?: string;
};

type ToolCacheMaintenanceSectionProps = {
    config: corelib.AppConfig | null;
    setConfig: (c: corelib.AppConfig) => void;
    lang: string;
    showToastMessage: (message: string) => void;
};

const textForLang = localizeText;
const mb = 1024 * 1024;

const defaultMaintenance = {
    enabled: true,
    max_bytes: 512 * mb,
    min_interval_hours: 24,
    clean_on_startup: true,
    clean_on_exit: true,
    last_cleanup_at: '',
};

const normalizeMaintenance = (value: any = {}) => ({
    ...defaultMaintenance,
    ...value,
    enabled: value.enabled ?? defaultMaintenance.enabled,
    max_bytes: value.max_bytes || defaultMaintenance.max_bytes,
    min_interval_hours: value.min_interval_hours || defaultMaintenance.min_interval_hours,
    clean_on_startup: value.clean_on_startup ?? defaultMaintenance.clean_on_startup,
    clean_on_exit: value.clean_on_exit ?? defaultMaintenance.clean_on_exit,
});

const formatBytes = (bytes?: number) => {
    const value = Number(bytes || 0);
    if (value >= 1024 * mb) return `${(value / (1024 * mb)).toFixed(2)} GB`;
    return `${(value / mb).toFixed(1)} MB`;
};

const formatCacheSize = (lang: string, status: ToolCacheStatus | null) => {
    if (!status) {
        return textForLang(lang, 'Checking...', '检查中...', '檢查中...');
    }
    if (status.size_approximate && Number(status.size_bytes || 0) <= 0) {
        return textForLang(lang, 'Not fully scanned', '未完整扫描', '未完整掃描');
    }
    return `${status.size_approximate ? '≥ ' : ''}${formatBytes(status.size_bytes)}`;
};

export const ToolCacheMaintenanceSection = ({ config, setConfig, lang, showToastMessage }: ToolCacheMaintenanceSectionProps) => {
    const [status, setStatus] = useState<ToolCacheStatus | null>(null);
    const [busy, setBusy] = useState(false);
    const [saving, setSaving] = useState(false);
    const [maxMbDraft, setMaxMbDraft] = useState('');
    const [intervalDraft, setIntervalDraft] = useState('');
    const maintenance = useMemo(() => normalizeMaintenance((config as any)?.tool_cache_maintenance), [config]);
    const lastCleanupAt = status?.last_cleanup_at || maintenance.last_cleanup_at;

    const refreshStatus = async () => {
        try {
            setStatus(await GetToolCacheStatus());
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        }
    };

    useEffect(() => {
        void refreshStatus();
    }, []);

    useEffect(() => {
        setMaxMbDraft(String(Math.round(maintenance.max_bytes / mb)));
        setIntervalDraft(String(maintenance.min_interval_hours));
    }, [maintenance.max_bytes, maintenance.min_interval_hours]);

    const saveMaintenance = async (patch: Record<string, any>) => {
        if (!config || saving) return;
        const next = normalizeMaintenance({
            ...maintenance,
            last_cleanup_at: status?.last_cleanup_at || maintenance.last_cleanup_at,
            ...patch,
        });
        setSaving(true);
        try {
            const saved = await PatchConfigFields({ tool_cache_maintenance: patch });
            setConfig(new corelib.AppConfig(saved));
            setStatus((previous) => previous ? {
                ...previous,
                auto_enabled: next.enabled,
                max_bytes: next.max_bytes,
                min_interval_hours: next.min_interval_hours,
                clean_on_startup: next.clean_on_startup,
                clean_on_exit: next.clean_on_exit,
            } : previous);
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        } finally {
            setSaving(false);
        }
    };

    const commitMaxBytes = () => {
        const parsed = Number(maxMbDraft || 512);
        const nextMb = Math.max(64, Number.isFinite(parsed) ? parsed : 512);
        const normalized = Math.round(nextMb);
        setMaxMbDraft(String(normalized));
        if (normalized * mb !== maintenance.max_bytes) {
            void saveMaintenance({ max_bytes: normalized * mb });
        }
    };

    const commitInterval = () => {
        const parsed = Number(intervalDraft || 24);
        const nextHours = Math.max(1, Number.isFinite(parsed) ? parsed : 24);
        const normalized = Math.round(nextHours);
        setIntervalDraft(String(normalized));
        if (normalized !== maintenance.min_interval_hours) {
            void saveMaintenance({ min_interval_hours: normalized });
        }
    };

    const cleanNow = async () => {
        if (busy) return;
        setBusy(true);
        try {
            const result = await CleanToolCacheNow();
            const savedConfig = new corelib.AppConfig(await LoadConfig());
            const savedMaintenance = normalizeMaintenance((savedConfig as any).tool_cache_maintenance);
            setConfig(savedConfig);
            setStatus((previous) => ({
                path: result?.path || previous?.path,
                exists: result?.exists ?? previous?.exists ?? false,
                size_bytes: result?.skipped ? result?.before_bytes : result?.after_bytes,
                size_approximate: false,
                auto_enabled: savedMaintenance.enabled,
                max_bytes: savedMaintenance.max_bytes,
                min_interval_hours: savedMaintenance.min_interval_hours,
                clean_on_startup: savedMaintenance.clean_on_startup,
                clean_on_exit: savedMaintenance.clean_on_exit,
                last_cleanup_at: savedMaintenance.last_cleanup_at,
            }));
            if (result?.skipped) {
                showToastMessage(textForLang(
                    lang,
                    'Tool cache is already empty.',
                    '工具缓存已为空。',
                    '工具快取已為空。'
                ));
                return;
            }
            showToastMessage(textForLang(
                lang,
                `Tool cache cleaned, freed ${formatBytes(result?.freed_bytes)}.`,
                `工具缓存已清理，释放 ${formatBytes(result?.freed_bytes)}。`,
                `工具快取已清理，釋放 ${formatBytes(result?.freed_bytes)}。`
            ));
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        } finally {
            setBusy(false);
        }
    };

    return (
        <section className="system-settings-card tool-cache-maintenance-section">
            <div className="tool-cache-maintenance-section__header">
                <div>
                    <h4>
                        {textForLang(lang, 'Tool Cache Maintenance', '工具缓存维护', '工具快取維護')}
                    </h4>
                    <p>
                        {textForLang(
                            lang,
                            'Keeps downloaded tool update packages under control without slowing normal work.',
                            '控制工具更新包缓存占用，不影响正常使用时的性能。',
                            '控制工具更新包快取佔用，不影響正常使用時的效能。'
                        )}
                    </p>
                </div>
                <button type="button" className="btn-secondary" disabled={busy} onClick={refreshStatus}>
                    {textForLang(lang, 'Refresh', '刷新', '重新整理')}
                </button>
            </div>

            <div className="tool-cache-maintenance-section__stats">
                <span>
                    {textForLang(lang, 'Current size', '当前大小', '目前大小')}: <strong>{formatCacheSize(lang, status)}</strong>
                </span>
                <span title={status?.path || ''}>{status?.path || textForLang(lang, 'Not found', '未找到', '未找到')}</span>
            </div>

            <label className="system-settings-option">
                <input
                    type="checkbox"
                    checked={maintenance.enabled}
                    disabled={saving}
                    onChange={(e) => saveMaintenance({ enabled: e.target.checked })}
                />
                <span>{textForLang(lang, 'Automatic maintenance', '自动维护', '自動維護')}</span>
                <small>
                    {textForLang(lang, 'Runs at most once per interval and only when the cache exceeds the threshold.', '每个间隔最多运行一次，且只在缓存超过阈值时执行。', '每個間隔最多執行一次，且只在快取超過門檻時執行。')}
                </small>
            </label>

            <div className="system-settings-number-grid">
                <label className="system-settings-field">
                    <span>{textForLang(lang, 'Clean above (MB)', '超过后清理 (MB)', '超過後清理 (MB)')}</span>
                    <input
                        className="form-input"
                        type="number"
                        min={64}
                        step={64}
                        value={maxMbDraft}
                        disabled={saving}
                        onChange={(e) => setMaxMbDraft(e.target.value)}
                        onBlur={commitMaxBytes}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                                e.currentTarget.blur();
                            }
                        }}
                    />
                </label>
                <label className="system-settings-field">
                    <span>{textForLang(lang, 'Minimum interval (hours)', '最小间隔 (小时)', '最小間隔 (小時)')}</span>
                    <input
                        className="form-input"
                        type="number"
                        min={1}
                        step={1}
                        value={intervalDraft}
                        disabled={saving}
                        onChange={(e) => setIntervalDraft(e.target.value)}
                        onBlur={commitInterval}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                                e.currentTarget.blur();
                            }
                        }}
                    />
                </label>
            </div>

            <div className="tool-cache-maintenance-section__checks">
                <label>
                    <input type="checkbox" checked={maintenance.clean_on_startup} disabled={saving} onChange={(e) => saveMaintenance({ clean_on_startup: e.target.checked })} />
                    <span>{textForLang(lang, 'Check after startup', '启动后检查', '啟動後檢查')}</span>
                </label>
                <label>
                    <input type="checkbox" checked={maintenance.clean_on_exit} disabled={saving} onChange={(e) => saveMaintenance({ clean_on_exit: e.target.checked })} />
                    <span>{textForLang(lang, 'Check on exit', '退出时检查', '結束時檢查')}</span>
                </label>
            </div>

            <div className="tool-cache-maintenance-section__actions">
                <button type="button" className="btn-primary" disabled={busy} onClick={cleanNow}>
                    {busy ? textForLang(lang, 'Cleaning...', '清理中...', '清理中...') : textForLang(lang, 'Clean Now', '立即清理', '立即清理')}
                </button>
                <span>
                    {lastCleanupAt
                        ? textForLang(lang, `Last cleanup: ${lastCleanupAt}`, `上次清理：${lastCleanupAt}`, `上次清理：${lastCleanupAt}`)
                        : textForLang(lang, 'No cleanup recorded yet', '尚无清理记录', '尚無清理記錄')}
                </span>
            </div>
        </section>
    );
};
