import { CleanSystemCleanupNow, GetSystemCleanupStatus, LoadConfig, PatchConfigFields } from '../../../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../../../wailsjs/runtime';
import { useEffect, useMemo, useRef, useState } from 'react';
import { corelib } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';

type CleanupCategoryStatus = {
    id?: string;
    size_bytes?: number;
    size_approximate?: boolean;
    selected?: boolean;
};

type CleanupStatus = {
    auto_enabled?: boolean;
    interval_days?: number;
    max_bytes?: number;
    last_cleanup_at?: string;
    total_bytes?: number;
    total_approximate?: boolean;
    categories?: CleanupCategoryStatus[];
};

type CleanupResult = {
    freed_bytes?: number;
    removed_count?: number;
    skipped_recent?: number;
    skipped_busy?: number;
    skipped?: boolean;
    reason?: string;
    incomplete?: boolean;
    errors?: string[];
};

type CleanupProgress = {
    phase?: string;
    percent?: number;
    category_id?: string;
    freed_bytes?: number;
    index?: number;
    total?: number;
};

type SystemCleanupSectionProps = {
    config: corelib.AppConfig | null;
    setConfig: (c: corelib.AppConfig) => void;
    lang: string;
    showToastMessage: (message: string) => void;
};

const textForLang = localizeText;
const mb = 1024 * 1024;
const defaultMaxBytes = 1024 * mb;

const cleanupCatalog = (lang: string) => [
    { id: 'tool_packages', label: textForLang(lang, 'Tool packages', '工具更新包', '工具更新包') },
    { id: 'app_temp', label: textForLang(lang, 'App temp files', '应用临时文件', '應用臨時檔案') },
    { id: 'skill_temp', label: textForLang(lang, 'Workspace skill temp', '工作区技能临时', '工作區技能臨時') },
    { id: 'skill_staging', label: textForLang(lang, 'Skill install staging', '技能安装暂存', '技能安裝暫存') },
    { id: 'doc_preview', label: textForLang(lang, 'Document preview', '文档预览', '文件預覽') },
    { id: 'clipboard_paste', label: textForLang(lang, 'Clipboard paste', '粘贴暂存', '貼上暫存') },
    { id: 'preview_browser', label: textForLang(lang, 'Preview browser', '预览浏览器', '預覽瀏覽器') },
    { id: 'spilled_scripts', label: textForLang(lang, 'Spilled scripts', '溢出脚本', '溢出指令碼') },
    { id: 'coding_verify', label: textForLang(lang, 'Coding verify output', '编程校验输出', '程式校驗輸出') },
    { id: 'rebuildable_index', label: textForLang(lang, 'Rebuildable indexes', '可重建索引', '可重建索引') },
    { id: 'intent_shadow', label: textForLang(lang, 'Intent shadow files', '意图影子文件', '意圖影子檔案') },
    { id: 'demo_shots', label: textForLang(lang, 'Demo screenshots', '演示截图', '示範截圖') },
    { id: 'llm_partials', label: textForLang(lang, 'Model response leftovers', '模型响应残留', '模型回應殘留') },
    { id: 'desktop_staging', label: textForLang(lang, 'Desktop service staging', '桌面服务暂存', '桌面服務暫存') },
    { id: 'webview_cache', label: textForLang(lang, 'Interface cache', '界面资源缓存', '介面資源快取') },
    { id: 'audit_temp', label: textForLang(lang, 'Security audit temp', '安全审计暂存', '安全稽核暫存') },
    { id: 'vrepo_auth', label: textForLang(lang, 'Repo auth temp', '仓库认证暂存', '倉庫認證暫存') },
];

const defaultCleanup = {
    enabled: true,
    interval_days: 7,
    max_bytes: defaultMaxBytes,
    disabled_categories: [] as string[],
    last_cleanup_at: '',
};

const normalizeCleanup = (value: any = {}) => ({
    ...defaultCleanup,
    ...value,
    enabled: value.enabled ?? defaultCleanup.enabled,
    interval_days: value.interval_days || defaultCleanup.interval_days,
    max_bytes: value.max_bytes || defaultCleanup.max_bytes,
    disabled_categories: Array.isArray(value.disabled_categories) ? value.disabled_categories : [],
    last_cleanup_at: value.last_cleanup_at || '',
});

const formatBytes = (value: number) => {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let size = Math.max(0, value || 0);
    let unit = 0;
    while (size >= 1024 && unit < units.length - 1) {
        size /= 1024;
        unit += 1;
    }
    const digits = unit === 0 || size >= 10 ? 0 : 1;
    return `${size.toFixed(digits)} ${units[unit]}`;
};

const formatStamp = (value: string) => {
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) return value;
    return parsed.toLocaleString();
};

export const SystemCleanupSection = ({ config, setConfig, lang, showToastMessage }: SystemCleanupSectionProps) => {
    const [status, setStatus] = useState<CleanupStatus | null>(null);
    const [busy, setBusy] = useState(false);
    const [saving, setSaving] = useState(false);
    const [progress, setProgress] = useState<CleanupProgress | null>(null);
    const [maxMbDraft, setMaxMbDraft] = useState('');
    const [intervalDraft, setIntervalDraft] = useState('');
    const [disabledOverride, setDisabledOverride] = useState<string[] | null>(null);
    const pendingPatch = useRef<Record<string, any> | null>(null);
    const savingRef = useRef(false);
    const disabledOverrideRef = useRef<string[] | null>(null);
    const cleanup = useMemo(() => normalizeCleanup((config as any)?.system_cleanup), [config]);
    const catalog = useMemo(() => cleanupCatalog(lang), [lang]);
    const labels = useMemo(() => new Map(catalog.map((item) => [item.id, item.label])), [catalog]);
    const sizes = useMemo(() => {
        const out = new Map<string, CleanupCategoryStatus>();
        for (const item of status?.categories || []) {
            if (item.id) out.set(item.id, item);
        }
        return out;
    }, [status]);
    const disabled = useMemo(
        () => new Set(disabledOverride ?? cleanup.disabled_categories ?? []),
        [disabledOverride, cleanup.disabled_categories],
    );
    const lastCleanupAt = status?.last_cleanup_at || cleanup.last_cleanup_at;

    const refreshStatus = async () => {
        try {
            setStatus(await GetSystemCleanupStatus());
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        }
    };

    useEffect(() => {
        void refreshStatus();
    }, []);

    useEffect(() => {
        const handler = (payload: CleanupProgress) => {
            setProgress(payload || null);
        };
        EventsOn('system-cleanup-progress', handler);
        return () => EventsOff('system-cleanup-progress');
    }, []);

    useEffect(() => {
        setMaxMbDraft(String(Math.round(cleanup.max_bytes / mb)));
        setIntervalDraft(String(cleanup.interval_days));
    }, [cleanup.max_bytes, cleanup.interval_days]);

    const flushCleanup = async () => {
        if (!config || savingRef.current) return;
        savingRef.current = true;
        setSaving(true);
        try {
            while (pendingPatch.current) {
                const next = pendingPatch.current;
                pendingPatch.current = null;
                try {
                    const saved = await PatchConfigFields({ system_cleanup: next });
                    setConfig(new corelib.AppConfig(saved));
                } catch (err: any) {
                    showToastMessage(err?.message || String(err));
                    break;
                }
            }
        } finally {
            savingRef.current = false;
            setSaving(false);
            if (pendingPatch.current) {
                void flushCleanup();
                return;
            }
            disabledOverrideRef.current = null;
            setDisabledOverride(null);
        }
    };

    const enqueueCleanup = (patch: Record<string, any>) => {
        if (!config) return;
        pendingPatch.current = { ...(pendingPatch.current ?? {}), ...patch };
        void flushCleanup();
    };

    const commitMaxBytes = () => {
        const parsed = Number(maxMbDraft || 1024);
        const nextMb = Math.max(64, Number.isFinite(parsed) ? parsed : 1024);
        const normalized = Math.round(nextMb);
        setMaxMbDraft(String(normalized));
        if (normalized * mb !== cleanup.max_bytes) {
            enqueueCleanup({ max_bytes: normalized * mb });
        }
    };

    const commitInterval = () => {
        const parsed = Number(intervalDraft || 7);
        const nextDays = Math.min(365, Math.max(1, Number.isFinite(parsed) ? parsed : 7));
        const normalized = Math.round(nextDays);
        setIntervalDraft(String(normalized));
        if (normalized !== cleanup.interval_days) {
            enqueueCleanup({ interval_days: normalized });
        }
    };

    const toggleCategory = (id: string, checked: boolean) => {
        const next = new Set<string>(disabledOverrideRef.current ?? cleanup.disabled_categories ?? []);
        if (checked) next.delete(id);
        else next.add(id);
        const list = [...next];
        disabledOverrideRef.current = list;
        setDisabledOverride(list);
        enqueueCleanup({ disabled_categories: list });
    };

    const cleanNow = async () => {
        if (busy) return;
        setBusy(true);
        setProgress({ phase: 'clean', percent: 1 });
        try {
            const result = await CleanSystemCleanupNow() as CleanupResult;
            const savedConfig = new corelib.AppConfig(await LoadConfig());
            setConfig(savedConfig);
            await refreshStatus();
            if (result?.skipped && result.reason === 'busy') {
                showToastMessage(textForLang(lang, 'Cleanup is already running.', '清理已在进行。', '清理已在進行。'));
                return;
            }
            const freed = formatBytes(result?.freed_bytes || 0);
            if (result?.incomplete) {
                if ((result.removed_count || 0) === 0) {
                    showToastMessage(textForLang(lang, 'Cleanup did not finish. Run it again to continue.', '清理未完成，可再次执行以继续。', '清理未完成，可再次執行以繼續。'));
                    return;
                }
                showToastMessage(textForLang(
                    lang,
                    `Cleaned ${freed}. Some items remain. Run cleanup again to continue.`,
                    `已清理 ${freed}。还有未完成的项，可再次清理。`,
                    `已清理 ${freed}。還有未完成的項，可再次清理。`,
                ));
                return;
            }
            if ((result?.removed_count || 0) === 0 && (result?.skipped_busy || 0) === 0) {
                showToastMessage(textForLang(lang, 'Nothing to clean.', '没有可清理的文件。', '沒有可清理的檔案。'));
                return;
            }
            if ((result?.skipped_busy || 0) > 0) {
                showToastMessage(textForLang(
                    lang,
                    `Cleaned ${freed}. ${result?.skipped_busy} item(s) were in use and kept.`,
                    `已清理 ${freed}。有 ${result?.skipped_busy} 项正在使用，已跳过。`,
                    `已清理 ${freed}。有 ${result?.skipped_busy} 項正在使用，已跳過。`,
                ));
                return;
            }
            showToastMessage(textForLang(
                lang,
                `Cleaned, freed ${freed}.`,
                `已清理，释放 ${freed}。`,
                `已清理，釋放 ${freed}。`,
            ));
        } catch (err: any) {
            showToastMessage(err?.message || String(err));
        } finally {
            setBusy(false);
            setProgress(null);
        }
    };

    const percent = Math.max(0, Math.min(100, progress?.percent || 0));
    const activeLabel = progress?.category_id ? labels.get(progress.category_id) || progress.category_id : '';
    const selectedBytes = status?.total_bytes || 0;

    return (
        <section className="system-settings-card system-cleanup-section">
            <div className="system-cleanup-section__header">
                <div>
                    <h4>{textForLang(lang, 'System Cleanup', '系统清理', '系統清理')}</h4>
                    <p>
                        {textForLang(
                            lang,
                            'Cleans regenerable temporary files and tool packages. Conversations, knowledge, settings, sign-in data, staged app files, and a saved problem-report archive stay.',
                            '清理可重新生成的临时文件和工具更新包。对话、知识库、设置、登录数据、应用暂存文件和已保存的问题报告包会保留。',
                            '清理可重新產生的臨時檔案和工具更新包。對話、知識庫、設定、登入資料、應用暫存檔案和已儲存的問題報告包會保留。',
                        )}
                    </p>
                </div>
                <button type="button" className="btn-secondary" disabled={busy} onClick={() => void refreshStatus()}>
                    {textForLang(lang, 'Refresh', '刷新', '重新整理')}
                </button>
            </div>

            <div className="system-cleanup-section__stats">
                <strong>
                    {!status
                        ? '…'
                        : `${status.total_approximate && !selectedBytes ? '…' : `${status.total_approximate ? '≥ ' : ''}${formatBytes(selectedBytes)}`}`}
                </strong>
                <span>
                    {textForLang(lang, 'Selected temporary files', '已选临时文件', '已選臨時檔案')}
                </span>
            </div>

            <div className="system-cleanup-section__controls">
                <label>
                    <input
                        type="checkbox"
                        checked={cleanup.enabled}
                        disabled={saving || busy}
                        onChange={(e) => enqueueCleanup({ enabled: e.target.checked })}
                    />
                    <span>{textForLang(lang, 'Automatic cleanup', '自动清理', '自動清理')}</span>
                </label>
                <label>
                    <span>{textForLang(lang, 'Interval (days)', '清理间隔（天）', '清理間隔（天）')}</span>
                    <input
                        type="number"
                        min={1}
                        max={365}
                        value={intervalDraft}
                        disabled={!cleanup.enabled || saving || busy}
                        onChange={(e) => setIntervalDraft(e.target.value)}
                        onBlur={commitInterval}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
                        }}
                    />
                </label>
                <label>
                    <span>{textForLang(lang, 'Size threshold (MB)', '容量阈值 (MB)', '容量閾值 (MB)')}</span>
                    <input
                        type="number"
                        min={64}
                        step={64}
                        value={maxMbDraft}
                        disabled={!cleanup.enabled || saving || busy}
                        onChange={(e) => setMaxMbDraft(e.target.value)}
                        onBlur={commitMaxBytes}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
                        }}
                    />
                </label>
            </div>
            <p className="system-cleanup-section__hint">
                {textForLang(
                    lang,
                    'Automatic cleanup is on by default. The first run only starts the timer, unless the selected files already exceed the size threshold (default 1024 MB). Later runs follow the interval, and also run early while the size stays above the threshold. Automatic cleanup keeps files written in the last 15 minutes. Clean now ignores the interval, the threshold, and that wait.',
                    '自动清理默认开启。第一次只开始计时，除非所选文件已经超过容量阈值（默认 1024 MB）。之后按间隔清理，容量仍超过阈值时也会提前清理。自动清理会保留最近 15 分钟写入的文件。立即清理不受间隔、阈值和这段等待限制。',
                    '自動清理預設開啟。第一次只開始計時，除非所選檔案已經超過容量閾值（預設 1024 MB）。之後按間隔清理，容量仍超過閾值時也會提前清理。自動清理會保留最近 15 分鐘寫入的檔案。立即清理不受間隔、閾值和這段等待限制。',
                )}
            </p>

            <div className="system-cleanup-section__categories">
                {catalog.map((item) => {
                    const size = sizes.get(item.id);
                    const checked = !disabled.has(item.id);
                    return (
                        <label key={item.id} title={item.label}>
                            <input
                                type="checkbox"
                                checked={checked}
                                disabled={saving || busy}
                                onChange={(e) => toggleCategory(item.id, e.target.checked)}
                            />
                            <span>
                                {item.label}
                                <small>
                                    {!size || (size.size_approximate && !size.size_bytes)
                                        ? '…'
                                        : `${size.size_approximate ? '≥ ' : ''}${formatBytes(size.size_bytes || 0)}`}
                                </small>
                            </span>
                        </label>
                    );
                })}
            </div>

            {busy && (
                <div className="system-cleanup-section__progress">
                    <div
                        role="progressbar"
                        aria-valuemin={0}
                        aria-valuemax={100}
                        aria-valuenow={percent}
                    >
                        <div style={{ width: `${percent}%` }} />
                    </div>
                    <span>
                        {activeLabel
                            ? textForLang(lang, `Cleaning ${activeLabel}`, `正在清理${activeLabel}`, `正在清理${activeLabel}`)
                            : textForLang(lang, 'Cleaning…', '正在清理…', '正在清理…')}
                        {` ${percent}%`}
                    </span>
                </div>
            )}

            <div className="system-cleanup-section__actions">
                <button type="button" className="btn-primary" disabled={busy || saving} onClick={() => void cleanNow()}>
                    {busy
                        ? textForLang(lang, 'Cleaning…', '清理中…', '清理中…')
                        : textForLang(lang, 'Clean Now', '立即清理', '立即清理')}
                </button>
                <span>
                    {lastCleanupAt
                        ? textForLang(lang, `Last cleanup: ${formatStamp(lastCleanupAt)}`, `上次清理：${formatStamp(lastCleanupAt)}`, `上次清理：${formatStamp(lastCleanupAt)}`)
                        : textForLang(lang, 'No cleanup recorded yet', '尚无清理记录', '尚無清理記錄')}
                </span>
            </div>
        </section>
    );
};
