import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    GetUserDataMigrationJob,
    StartUserDataMigrationCleanup,
    StartUserDataMigrationExport,
    StartUserDataMigrationImport,
    UserDataMigrationInstances,
    UserDataMigrationStatus,
} from '../../../wailsjs/go/main/App';
import { localizeText } from '../../i18n';

type MigrationSettingsPanelProps = {
    lang: string;
    showToastMessage: (message: string) => void;
};

type MigrationStatus = {
    configured?: boolean;
    hub_url?: string;
    tenant_id?: string;
    tenant_name?: string;
    user_id?: string;
    email?: string;
    machine_id?: string;
    machine_name?: string;
    max_compressed_bytes?: number;
    current_export?: Record<string, any> | null;
    configuration_reason?: string;
};

type MigrationInstance = {
    instance_id?: string;
    machine_id?: string;
    machine_name?: string;
    instance_name?: string;
    status?: string;
    os?: string;
    maclaw_version?: string;
    last_seen_at?: string;
    has_export?: boolean;
    export_id?: string;
    export_status?: string;
    export_claimed_by_machine_id?: string;
    export_updated_at?: string;
    export_size?: number;
};

type MigrationJob = {
    id?: string;
    kind?: string;
    status?: string;
    progress?: number;
    progress_text?: string;
    error?: string;
    result?: Record<string, any>;
};

const textForLang = localizeText;

const terminalJobStatuses = new Set(['succeeded', 'failed', 'canceled']);
const cleanupRetryStatuses = new Set(['imported', 'deleting']);
const resumableImportStatuses = new Set(['importing']);

const errorMessage = (err: unknown) => {
    if (err && typeof err === 'object' && 'message' in err) return String((err as { message?: unknown }).message || err);
    return String(err || '');
};

const localizeMigrationMessage = (lang: string, value: unknown) => {
    const raw = errorMessage(value).trim();
    if (!raw) return '';
    const lower = raw.toLowerCase();
    const pick = (en: string, zhHans: string, zhHant?: string) => textForLang(lang, en, zhHans, zhHant);

    const uploadedMatch = raw.match(/^uploaded\s+(\d+)\/(\d+)\s+chunks$/i);
    if (uploadedMatch) return pick(`Uploaded ${uploadedMatch[1]}/${uploadedMatch[2]} chunks`, `已上传 ${uploadedMatch[1]}/${uploadedMatch[2]} 个分片`, `已上傳 ${uploadedMatch[1]}/${uploadedMatch[2]} 個分片`);
    const downloadedMatch = raw.match(/^downloaded\s+(\d+)\/(\d+)\s+chunks$/i);
    if (downloadedMatch) return pick(`Downloaded ${downloadedMatch[1]}/${downloadedMatch[2]} chunks`, `已下载 ${downloadedMatch[1]}/${downloadedMatch[2]} 个分片`, `已下載 ${downloadedMatch[1]}/${downloadedMatch[2]} 個分片`);

    if (lower === 'preparing migration package') return pick('Preparing migration package', '正在准备迁移包', '正在準備遷移包');
    if (lower === 'encrypting migration package') return pick('Encrypting migration package', '正在加密迁移包', '正在加密遷移包');
    if (lower === 'uploading encrypted chunks') return pick('Uploading encrypted chunks', '正在上传加密分片', '正在上傳加密分片');
    if (lower === 'export completed') return pick('Move-out completed', '迁出已完成', '遷出已完成');
    if (lower === 'claiming migration export') return pick('Claiming migration package', '正在认领迁移包', '正在認領遷移包');
    if (lower === 'downloading encrypted chunks') return pick('Downloading encrypted chunks', '正在下载加密分片', '正在下載加密分片');
    if (lower === 'decrypting and verifying package') return pick('Decrypting and verifying package', '正在解密并校验迁移包', '正在解密並校驗遷移包');
    if (lower === 'restoring local memory and knowledge base') return pick('Restoring memory and local knowledge base', '正在恢复记忆与本地知识库', '正在還原記憶與本地知識庫');
    if (lower === 'import completed') return pick('Move-in completed', '迁入已完成', '遷入已完成');
    if (lower === 'completed') return pick('Completed', '已完成', '已完成');
    if (lower === 'checking migration cleanup state') return pick('Checking cleanup state', '正在检查清理状态', '正在檢查清理狀態');
    if (lower === 'retrying hub cleanup') return pick('Retrying Hub cleanup', '正在重试 Hub 清理', '正在重試 Hub 清理');
    if (lower === 'import cleanup completed') return pick('Hub cleanup completed', 'Hub 清理已完成', 'Hub 清理已完成');
    if (lower === 'import cleanup already completed') return pick('Hub cleanup was already completed', 'Hub 清理已完成', 'Hub 清理已完成');
    if (lower === 'local import completed; hub cleanup can be retried') {
        return pick('Move-in completed. Hub cleanup did not finish and can be retried from the package list.', '迁入已完成。Hub 清理未完成，可在迁移包列表中重试清理。', '遷入已完成。Hub 清理未完成，可在遷移包列表中重試清理。');
    }

    if (lower.includes('passwords do not match')) return pick('The two passwords do not match.', '两次输入的密码不一致。', '兩次輸入的密碼不一致。');
    if (lower.includes('migration password is incorrect') || lower.includes('password is incorrect')) return pick('The password is incorrect, or the migration package is corrupted.', '密码不正确，或迁移包已损坏。', '密碼不正確，或遷移包已損壞。');
    if (lower.includes('hash mismatch')) return pick('Migration package integrity check failed. Please move out again and retry.', '迁移包完整性校验失败，请重新迁出后再试。', '遷移包完整性校驗失敗，請重新遷出後再試。');
    if (lower.includes('size mismatch')) return pick('Migration package size check failed. Please retry the transfer.', '迁移包大小校验失败，请重试传输。', '遷移包大小校驗失敗，請重試傳輸。');
    if (lower.includes('unsupported migration package') || lower.includes('invalid encrypted migration')) return pick('This migration package is not supported or is corrupted.', '该迁移包不受支持或已损坏。', '該遷移包不受支援或已損壞。');
    if (lower.includes('compressed migration package') && lower.includes('exceeds limit')) return pick('The migration package exceeds the tenant size limit.', '迁移包超过租户设置的大小上限。', '遷移包超過租戶設定的大小上限。');
    if (lower.includes('another migration job is already running')) return pick('Another migration task is already running. Please wait for it to finish.', '已有迁移任务正在执行，请等待完成后再试。', '已有遷移任務正在執行，請等待完成後再試。');
    if (lower.includes('hub is not configured')) return pick('Hub is not configured.', 'Hub 尚未配置。', 'Hub 尚未設定。');
    if (lower.includes('hub machine is not registered')) return pick('This machine is not registered with Hub.', '当前机器尚未注册到 Hub。', '目前機器尚未註冊到 Hub。');
    if (lower.includes('hub login is required')) return pick('Please sign in to Hub before migration.', '请先登录 Hub 后再迁移。', '請先登入 Hub 後再遷移。');
    if (lower.includes('hub migration api') || lower.includes('hub cleanup retry failed')) return pick('Hub request failed. Please check the network and retry.', 'Hub 请求失败，请检查网络后重试。', 'Hub 請求失敗，請檢查網路後重試。');
    if (lower.includes('not claimed by this machine')) return pick('This migration package is not claimed by the current machine.', '该迁移包未由当前机器认领，不能在此处清理。', '該遷移包未由目前機器認領，不能在此處清理。');
    if (lower.includes('database is locked') || lower.includes('database table is locked') || lower.includes('being used by another process')) return pick('The local knowledge base is busy. Please close related operations and retry.', '本地知识库正在使用中，请关闭相关操作后重试。', '本地知識庫正在使用中，請關閉相關操作後重試。');
    if (lower.includes('knowledge snapshot') || lower.includes('missing reference') || lower.includes('failed records')) return pick('Knowledge snapshot validation failed. Please move out again and retry.', '知识库快照校验失败，请重新迁出后再试。', '知識庫快照校驗失敗，請重新遷出後再試。');
    if (lower.includes('memory entry')) return pick('Memory data validation failed. Please move out again and retry.', '记忆数据校验失败，请重新迁出后再试。', '記憶資料校驗失敗，請重新遷出後再試。');

    return raw;
};

const localizeMigrationError = (lang: string, value: unknown) => {
    const raw = errorMessage(value).trim();
    const localized = localizeMigrationMessage(lang, raw);
    const isEnglish = textForLang(lang, 'en', 'zh') === 'en';
    if (!raw || localized !== raw || isEnglish) return localized;
    return textForLang(lang, 'Migration failed. Please check the network, password, and local knowledge base state, then retry.', '迁移过程未完成，请检查网络、密码或本地知识库状态后重试。', '遷移過程未完成，請檢查網路、密碼或本地知識庫狀態後重試。');
};

const formatBytes = (value?: number) => {
    const n = Number(value || 0);
    if (!Number.isFinite(n) || n <= 0) return '-';
    if (n >= 1024 * 1024 * 1024) return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
    if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
    if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${n} B`;
};

const formatDate = (value?: string) => {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString();
};

const normalizeProgress = (value?: number) => {
    const n = Number(value || 0);
    if (!Number.isFinite(n)) return 0;
    return Math.max(0, Math.min(100, Math.round(n * 100)));
};

const statusLabel = (lang: string, status?: string) => {
    switch (String(status || '').toLowerCase()) {
        case 'ready':
            return textForLang(lang, 'Ready', '\u53ef\u8fc1\u5165', '\u53ef\u9077\u5165');
        case 'uploading':
            return textForLang(lang, 'Uploading', '\u4e0a\u4f20\u4e2d', '\u4e0a\u50b3\u4e2d');
        case 'importing':
            return textForLang(lang, 'Importing', '\u8fc1\u5165\u4e2d', '\u9077\u5165\u4e2d');
        case 'imported':
        case 'deleting':
            return textForLang(lang, 'Cleaning Up', '\u6e05\u7406\u4e2d', '\u6e05\u7406\u4e2d');
        case 'deleted':
            return textForLang(lang, 'Deleted', '\u5df2\u6e05\u7406', '\u5df2\u6e05\u7406');
        case 'failed':
            return textForLang(lang, 'Failed', '\u5931\u8d25', '\u5931\u6557');
        default:
            return status || '-';
    }
};

const jobStatusLabel = (lang: string, status?: string) => {
    switch (String(status || '').toLowerCase()) {
        case 'running':
            return textForLang(lang, 'Running', '\u6267\u884c\u4e2d', '\u57f7\u884c\u4e2d');
        case 'succeeded':
            return textForLang(lang, 'Succeeded', '\u5df2\u5b8c\u6210', '\u5df2\u5b8c\u6210');
        case 'failed':
            return textForLang(lang, 'Failed', '\u5931\u8d25', '\u5931\u6557');
        default:
            return status || '-';
    }
};

const instanceLabel = (lang: string, instance: MigrationInstance) => {
    const name = instance.machine_name || instance.instance_name || instance.machine_id || instance.instance_id || '';
    const packageStatus = instance.has_export && instance.export_status ? ` - ${statusLabel(lang, instance.export_status)}` : '';
    const updated = instance.export_updated_at ? ` / ${formatDate(instance.export_updated_at)}` : '';
    const size = instance.export_size ? ` / ${formatBytes(instance.export_size)}` : '';
    return `${name}${packageStatus}${size}${updated}`;
};

export const MigrationSettingsPanel = ({ lang, showToastMessage }: MigrationSettingsPanelProps) => {
    const [status, setStatus] = useState<MigrationStatus | null>(null);
    const [instances, setInstances] = useState<MigrationInstance[]>([]);
    const [selectedExportID, setSelectedExportID] = useState('');
    const [exportPassword, setExportPassword] = useState('');
    const [exportPasswordConfirm, setExportPasswordConfirm] = useState('');
    const [importPassword, setImportPassword] = useState('');
    const [job, setJob] = useState<MigrationJob | null>(null);
    const [loading, setLoading] = useState(false);
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState('');

    const t = useCallback((en: string, zhHans: string, zhHant?: string) => textForLang(lang, en, zhHans, zhHant), [lang]);
    const localizedError = useCallback((err: unknown) => localizeMigrationError(lang, err), [lang]);

    const loadState = useCallback(async () => {
        setLoading(true);
        setError('');
        try {
            const nextStatus = await UserDataMigrationStatus();
            const nextMigrationStatus = (nextStatus || {}) as MigrationStatus;
            setStatus(nextMigrationStatus);
            if (nextMigrationStatus.configured !== true || nextMigrationStatus.configuration_reason) {
                setInstances([]);
                return;
            }
            const nextInstances = await UserDataMigrationInstances().catch((err) => ({ error: errorMessage(err), instances: [] }));
            const rawInstances = Array.isArray((nextInstances as any)?.instances) ? (nextInstances as any).instances : [];
            setInstances(rawInstances as MigrationInstance[]);
            if ((nextInstances as any)?.error) setError(localizedError((nextInstances as any).error));
        } catch (err) {
            setError(localizedError(err));
        } finally {
            setLoading(false);
        }
    }, [localizedError]);

    useEffect(() => {
        void loadState();
    }, [loadState]);

    const currentExport = status?.current_export && typeof status.current_export === 'object' ? status.current_export : null;
    const migrationRows = useMemo(() => {
        const rows = [...instances];
        const currentExportID = String(currentExport?.export_id || '');
        if (currentExportID && !rows.some((item) => item.export_id === currentExportID)) {
            rows.push({
                instance_id: String(currentExport?.source_instance_id || currentExport?.source_machine_id || currentExportID),
                machine_id: String(currentExport?.source_machine_id || currentExport?.source_instance_id || ''),
                machine_name: String(currentExport?.source_machine_name || ''),
                has_export: true,
                export_id: currentExportID,
                export_status: String(currentExport?.status || ''),
                export_claimed_by_machine_id: String(currentExport?.claimed_by_machine_id || ''),
                export_updated_at: String(currentExport?.updated_at || ''),
                export_size: Number(currentExport?.compressed_size || 0),
            });
        }
        return rows;
    }, [instances, currentExport]);
    const packageRows = useMemo(
        () => migrationRows.filter((item) => item.has_export && item.export_id),
        [migrationRows],
    );

    const importableInstances = useMemo(
        () => packageRows.filter((item) => {
            if (!item.has_export || !item.export_id) return false;
            const exportStatus = String(item.export_status || '').toLowerCase();
            if (exportStatus === 'ready') return true;
            const claimedByCurrentMachine = !!status?.machine_id && item.export_claimed_by_machine_id === status.machine_id;
            return claimedByCurrentMachine && (cleanupRetryStatuses.has(exportStatus) || resumableImportStatuses.has(exportStatus));
        }),
        [packageRows, status?.machine_id],
    );

    useEffect(() => {
        if (importableInstances.length === 0) {
            if (selectedExportID) setSelectedExportID('');
            return;
        }
        if (!importableInstances.some((item) => item.export_id === selectedExportID)) {
            setSelectedExportID(String(importableInstances[0].export_id || ''));
        }
    }, [importableInstances, selectedExportID]);

    useEffect(() => {
        if (!job?.id || terminalJobStatuses.has(String(job.status || '').toLowerCase())) return;
        const timer = window.setInterval(async () => {
            try {
                const nextJob = await GetUserDataMigrationJob(job.id || '');
                setJob((nextJob || {}) as MigrationJob);
                if (terminalJobStatuses.has(String((nextJob as MigrationJob)?.status || '').toLowerCase())) {
                    void loadState();
                }
            } catch (err) {
                setError(localizedError(err));
            }
        }, 1200);
        return () => window.clearInterval(timer);
    }, [job?.id, job?.status, loadState, localizedError]);

    const running = String(job?.status || '').toLowerCase() === 'running';
    const busy = running || submitting || loading;
    const configured = status?.configured === true;
    const ready = configured && !status?.configuration_reason;
    const canExport = ready && !busy && exportPassword.length > 0 && exportPassword === exportPasswordConfirm;
    const selectedInstance = importableInstances.find((item) => item.export_id === selectedExportID);
    const selectedExportStatus = String(selectedInstance?.export_status || '').toLowerCase();
    const cleanupRetry = cleanupRetryStatuses.has(selectedExportStatus) && !!status?.machine_id && selectedInstance?.export_claimed_by_machine_id === status.machine_id;
    const resumeImport = resumableImportStatuses.has(selectedExportStatus) && !!status?.machine_id && selectedInstance?.export_claimed_by_machine_id === status.machine_id;
    const canImport = ready && !busy && !!selectedExportID && (cleanupRetry || importPassword.length > 0);
    const jobPercent = normalizeProgress(job?.progress);
    const jobTone = String(job?.status || '').toLowerCase() === 'failed' ? 'failed' : String(job?.status || '').toLowerCase() === 'succeeded' ? 'succeeded' : 'running';
    const jobIsImport = job?.kind === 'migration.import' || job?.kind === 'migration.import.cleanup';
    const statusLoaded = status !== null;
    const renderJobProgress = (forImport: boolean) => {
        if (!job || jobIsImport !== forImport) return null;
        const jobMessage = job.error
            ? localizeMigrationError(lang, job.error)
            : job.progress_text
                ? localizeMigrationMessage(lang, job.progress_text)
                : t('Waiting for progress...', '\u7b49\u5f85\u8fdb\u5ea6...', '\u7b49\u5f85\u9032\u5ea6...');
        return (
            <div className="migration-progress-inline">
                <div className="migration-progress-head">
                    <h4>{jobIsImport ? t('Move-in Progress', '\u8fc1\u5165\u8fdb\u5ea6', '\u9077\u5165\u9032\u5ea6') : t('Move-out Progress', '\u8fc1\u51fa\u8fdb\u5ea6', '\u9077\u51fa\u9032\u5ea6')}</h4>
                    <span>{jobStatusLabel(lang, job.status)} / {jobPercent}%</span>
                </div>
                <div
                    className="migration-progress-track"
                    role="progressbar"
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={jobPercent}
                    aria-label={jobIsImport ? t('Move-in progress', '\u8fc1\u5165\u8fdb\u5ea6', '\u9077\u5165\u9032\u5ea6') : t('Move-out progress', '\u8fc1\u51fa\u8fdb\u5ea6', '\u9077\u51fa\u9032\u5ea6')}
                >
                    <div className="migration-progress-fill" data-tone={jobTone} style={{ width: `${jobPercent}%` }} />
                </div>
                <div className="migration-progress-text">
                    {jobMessage}
                </div>
            </div>
        );
    };

    const startExport = async () => {
        if (exportPassword !== exportPasswordConfirm) {
            setError(t('Passwords do not match.', '\u4e24\u6b21\u8f93\u5165\u7684\u5bc6\u7801\u4e0d\u4e00\u81f4\u3002', '\u5169\u6b21\u8f38\u5165\u7684\u5bc6\u78bc\u4e0d\u4e00\u81f4\u3002'));
            return;
        }
        setError('');
        setSubmitting(true);
        try {
            const nextJob = await StartUserDataMigrationExport(exportPassword, exportPasswordConfirm, true);
            setJob((nextJob || {}) as MigrationJob);
            setExportPassword('');
            setExportPasswordConfirm('');
            showToastMessage(t('Migration export started.', '\u8fc1\u51fa\u5df2\u5f00\u59cb\u3002', '\u9077\u51fa\u5df2\u958b\u59cb\u3002'));
        } catch (err) {
            setError(localizedError(err));
        } finally {
            setSubmitting(false);
        }
    };

    const startImport = async () => {
        setError('');
        setSubmitting(true);
        try {
            const nextJob = cleanupRetry
                ? await StartUserDataMigrationCleanup(selectedExportID)
                : await StartUserDataMigrationImport(selectedExportID, importPassword);
            setJob((nextJob || {}) as MigrationJob);
            setImportPassword('');
            showToastMessage(cleanupRetry ? t('Migration cleanup retry started.', '\u8fc1\u5165\u6e05\u7406\u91cd\u8bd5\u5df2\u5f00\u59cb\u3002', '\u9077\u5165\u6e05\u7406\u91cd\u8a66\u5df2\u958b\u59cb\u3002') : t('Migration import started.', '\u8fc1\u5165\u5df2\u5f00\u59cb\u3002', '\u9077\u5165\u5df2\u958b\u59cb\u3002'));
        } catch (err) {
            setError(localizedError(err));
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <div className="settings-panel migration-settings-panel">
            <section className="system-settings-card migration-settings-card">
                <div className="migration-settings-head">
                    <div>
                        <h4>{t('Move Out & In', '\u8fc1\u51fa\u4e0e\u8fc1\u5165', '\u9077\u51fa\u8207\u9077\u5165')}</h4>
                        <p>{t('Move MaClaw memory and the local knowledge base from an old machine to a new one through the current Hub tenant.', '\u7528\u4e8e\u901a\u8fc7\u5f53\u524d Hub \u79df\u6237\uff0c\u5c06 MaClaw \u8bb0\u5fc6\u4e0e\u672c\u5730\u77e5\u8bc6\u5e93\u4ece\u65e7\u673a\u5668\u8fc1\u79fb\u5230\u65b0\u673a\u5668\u3002', '\u7528\u65bc\u900f\u904e\u76ee\u524d Hub \u79df\u6236\uff0c\u5c07 MaClaw \u8a18\u61b6\u8207\u672c\u5730\u77e5\u8b58\u5eab\u5f9e\u820a\u6a5f\u5668\u9077\u79fb\u5230\u65b0\u6a5f\u5668\u3002')}</p>
                    </div>
                    <button type="button" className="btn-secondary" onClick={() => void loadState()} disabled={loading || busy}>
                        {loading ? t('Refreshing...', '\u5237\u65b0\u4e2d...', '\u91cd\u65b0\u6574\u7406\u4e2d...') : t('Refresh', '\u5237\u65b0', '\u91cd\u65b0\u6574\u7406')}
                    </button>
                </div>

                <div className="migration-status-grid">
                    <div className="migration-kv">
                        <span>{t('Hub', 'Hub', 'Hub')}</span>
                        <strong>{status?.hub_url || '-'}</strong>
                    </div>
                    <div className="migration-kv">
                        <span>{t('Tenant', '\u79df\u6237', '\u79df\u6236')}</span>
                        <strong>{status?.tenant_name || status?.tenant_id || '-'}</strong>
                    </div>
                    <div className="migration-kv">
                        <span>{t('Current Machine', '\u5f53\u524d\u673a\u5668', '\u76ee\u524d\u6a5f\u5668')}</span>
                        <strong>{status?.machine_name || status?.machine_id || '-'}</strong>
                    </div>
                    <div className="migration-kv">
                        <span>{t('Package Limit', '\u8fc1\u79fb\u5305\u4e0a\u9650', '\u9077\u79fb\u5305\u4e0a\u9650')}</span>
                        <strong>{formatBytes(status?.max_compressed_bytes)}</strong>
                    </div>
                </div>

                {statusLoaded && !configured && (
                    <div className="migration-alert migration-alert--warning">
                        {status?.configuration_reason ? localizeMigrationError(lang, status.configuration_reason) : t('Hub login is required before migration.', '\u8bf7\u5148\u767b\u5f55\u5e76\u6ce8\u518c\u5f53\u524d Hub\u3002', '\u8acb\u5148\u767b\u5165\u4e26\u8a3b\u518a\u76ee\u524d Hub\u3002')}
                    </div>
                )}
                {statusLoaded && configured && status?.configuration_reason && (
                    <div className="migration-alert migration-alert--warning">{localizeMigrationError(lang, status.configuration_reason)}</div>
                )}
                {error && <div className="migration-alert migration-alert--danger">{error}</div>}
            </section>

            <section className="system-settings-card migration-settings-card">
                <h4>{t('Move Out', '\u8fc1\u51fa', '\u9077\u51fa')}</h4>
                <div className="migration-alert migration-alert--warning">
                    {t('Only one migration package is kept on Hub. A new move-out package overwrites the old one and is encrypted with the password entered here.', '\u6bcf\u4e2a\u7528\u6237\u5728 Hub \u4e0a\u53ea\u4fdd\u7559\u4e00\u4efd\u8fc1\u79fb\u5305\u3002\u65b0\u8fc1\u51fa\u4f1a\u8986\u76d6\u65e7\u6570\u636e\uff0c\u5e76\u4f7f\u7528\u672c\u6b21\u8f93\u5165\u7684\u5bc6\u7801\u52a0\u5bc6\u3002', '\u6bcf\u500b\u4f7f\u7528\u8005\u5728 Hub \u4e0a\u53ea\u4fdd\u7559\u4e00\u4efd\u9077\u79fb\u5305\u3002\u65b0\u9077\u51fa\u6703\u8986\u84cb\u820a\u8cc7\u6599\uff0c\u4e26\u4f7f\u7528\u672c\u6b21\u8f38\u5165\u7684\u5bc6\u78bc\u52a0\u5bc6\u3002')}
                </div>
                <div className="migration-form-grid">
                    <label className="system-settings-field">
                        <span>{t('Password', '\u5bc6\u7801', '\u5bc6\u78bc')}</span>
                        <input className="form-input" type="password" value={exportPassword} onChange={(e) => setExportPassword(e.target.value)} autoComplete="new-password" disabled={!ready || busy} />
                    </label>
                    <label className="system-settings-field">
                        <span>{t('Confirm Password', '\u786e\u8ba4\u5bc6\u7801', '\u78ba\u8a8d\u5bc6\u78bc')}</span>
                        <input className="form-input" type="password" value={exportPasswordConfirm} onChange={(e) => setExportPasswordConfirm(e.target.value)} autoComplete="new-password" disabled={!ready || busy} />
                    </label>
                </div>
                <div className="migration-actions">
                    <button type="button" className="btn-primary" onClick={startExport} disabled={!canExport}>
                        {t('Start Move Out', '\u5f00\u59cb\u8fc1\u51fa', '\u958b\u59cb\u9077\u51fa')}
                    </button>
                    {currentExport && (
                        <span className="migration-inline-meta">
                            {t('Current package', '\u5f53\u524d\u8fc1\u79fb\u5305', '\u76ee\u524d\u9077\u79fb\u5305')}: {statusLabel(lang, currentExport.status)} / {formatBytes(Number(currentExport.compressed_size || 0))} / {formatDate(String(currentExport.updated_at || ''))}
                        </span>
                    )}
                </div>
                {renderJobProgress(false)}
            </section>

            <section className="system-settings-card migration-settings-card">
                <h4>{t('Move In', '\u8fc1\u5165', '\u9077\u5165')}</h4>
                <div className="migration-form-grid migration-form-grid--import">
                    <label className="system-settings-field">
                        <span>{t('Source Machine', '\u6765\u6e90\u673a\u5668', '\u4f86\u6e90\u6a5f\u5668')}</span>
                        <select className="form-input" value={selectedExportID} onChange={(e) => setSelectedExportID(e.target.value)} disabled={!ready || busy || importableInstances.length === 0}>
                            {importableInstances.length === 0 && <option value="">{t('No available package', '\u6682\u65e0\u53ef\u8fc1\u5165\u6216\u5f85\u6e05\u7406\u6570\u636e', '\u66ab\u7121\u53ef\u9077\u5165\u6216\u5f85\u6e05\u7406\u8cc7\u6599')}</option>}
                            {importableInstances.map((instance) => (
                                <option key={instance.export_id} value={instance.export_id}>
                                    {instanceLabel(lang, instance)}
                                </option>
                            ))}
                        </select>
                    </label>
                    <label className="system-settings-field">
                        <span>{t('Password', '\u5bc6\u7801', '\u5bc6\u78bc')}</span>
                        <input className="form-input" type="password" value={importPassword} onChange={(e) => setImportPassword(e.target.value)} autoComplete="current-password" disabled={!ready || busy || !selectedExportID || cleanupRetry} />
                    </label>
                </div>
                <div className="migration-actions">
                    <button type="button" className="btn-primary" onClick={startImport} disabled={!canImport}>
                        {cleanupRetry ? t('Retry Cleanup', '\u91cd\u8bd5\u6e05\u7406', '\u91cd\u8a66\u6e05\u7406') : resumeImport ? t('Resume Move In', '\u7ee7\u7eed\u8fc1\u5165', '\u7e7c\u7e8c\u9077\u5165') : t('Start Move In', '\u5f00\u59cb\u8fc1\u5165', '\u958b\u59cb\u9077\u5165')}
                    </button>
                    <span className="migration-inline-meta">
                        {cleanupRetry ? t('Local data has already been restored. This only completes Hub cleanup.', '\u672c\u5730\u6570\u636e\u5df2\u6062\u590d\uff0c\u6b64\u64cd\u4f5c\u53ea\u5b8c\u6210 Hub \u6e05\u7406\u3002', '\u672c\u5730\u8cc7\u6599\u5df2\u9084\u539f\uff0c\u6b64\u64cd\u4f5c\u53ea\u5b8c\u6210 Hub \u6e05\u7406\u3002') : resumeImport ? t('This package was already claimed by this machine. Continuing will verify the password and restore again before cleanup.', '\u8be5\u8fc1\u79fb\u5305\u5df2\u7531\u5f53\u524d\u673a\u5668\u8ba4\u9886\uff0c\u7ee7\u7eed\u65f6\u4f1a\u518d\u6b21\u6821\u9a8c\u5bc6\u7801\u5e76\u6062\u590d\u540e\u6e05\u7406\u3002', '\u8a72\u9077\u79fb\u5305\u5df2\u7531\u76ee\u524d\u6a5f\u5668\u8a8d\u9818\uff0c\u7e7c\u7e8c\u6642\u6703\u518d\u6b21\u6821\u9a57\u5bc6\u78bc\u4e26\u9084\u539f\u5f8c\u6e05\u7406\u3002') : t("Move-in restores the selected package into this machine's memory and local knowledge base.", '\u8fc1\u5165\u4f1a\u5c06\u6240\u9009\u8fc1\u79fb\u5305\u6062\u590d\u5230\u5f53\u524d\u673a\u5668\u7684\u8bb0\u5fc6\u4e0e\u672c\u5730\u77e5\u8bc6\u5e93\u3002', '\u9077\u5165\u6703\u5c07\u6240\u9078\u9077\u79fb\u5305\u9084\u539f\u5230\u76ee\u524d\u6a5f\u5668\u7684\u8a18\u61b6\u8207\u672c\u5730\u77e5\u8b58\u5eab\u3002')}
                    </span>
                </div>
                {renderJobProgress(true)}

                <div className="migration-instance-table-wrap">
                    <table className="migration-instance-table">
                        <thead>
                            <tr>
                                <th>{t('Machine', '\u673a\u5668', '\u6a5f\u5668')}</th>
                                <th>{t('Package', '\u8fc1\u79fb\u5305', '\u9077\u79fb\u5305')}</th>
                                <th>{t('Size', '\u5927\u5c0f', '\u5927\u5c0f')}</th>
                                <th>{t('Updated', '\u66f4\u65b0\u65f6\u95f4', '\u66f4\u65b0\u6642\u9593')}</th>
                            </tr>
                        </thead>
                        <tbody>
                            {packageRows.length === 0 && (
                                <tr>
                                    <td colSpan={4}>{t('No migration package found.', '\u6682\u65e0\u8fc1\u79fb\u5305\u3002', '\u66ab\u7121\u9077\u79fb\u5305\u3002')}</td>
                                </tr>
                            )}
                            {packageRows.map((instance) => (
                                <tr key={instance.instance_id || instance.machine_id || instance.export_id}>
                                    <td>
                                        <strong>{instance.machine_name || instance.instance_name || instance.machine_id || '-'}</strong>
                                        <small>{instance.machine_id || instance.instance_id || ''}</small>
                                    </td>
                                    <td>{instance.has_export ? statusLabel(lang, instance.export_status) : t('None', '\u65e0', '\u7121')}</td>
                                    <td>{formatBytes(instance.export_size)}</td>
                                    <td>{formatDate(instance.export_updated_at || instance.last_seen_at)}</td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </section>
        </div>
    );
};
