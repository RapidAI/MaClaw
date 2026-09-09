import { useEffect, useRef, useState } from 'react';
import {
    ExportTextFile,
    KnowledgeCancelImportIndexing,
    KnowledgeImportJobStatus,
    KnowledgeScanDirectory,
    KnowledgeScanFiles,
    KnowledgeStartImportDirectory,
    KnowledgeStartImportFiles,
    SelectKnowledgeDirectory,
    SelectKnowledgeFiles,
} from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

const dialogFocusableSelector = 'button:not([disabled]):not([tabindex="-1"]), input:not([disabled]):not([tabindex="-1"]), select:not([disabled]):not([tabindex="-1"]), textarea:not([disabled]):not([tabindex="-1"]), [href]:not([tabindex="-1"])';

// Fallback only used if capabilities haven't loaded yet (e.g. dialog opened before API returns).
// The authoritative list comes from the backend via the supportedExts prop.
const fallbackExts = ['.pdf', '.ppt', '.pptx', '.doc', '.docx', '.xls', '.xlsx', '.csv', '.md', '.txt'];

export type KnowledgeImportTFunc = (en: string, zhHans: string, zhHant?: string) => string;
type TFunc = KnowledgeImportTFunc;

export const knowledgeImportSupportedFormatsHint =
    'Supported: PDF, PPT/PPTX, DOC/DOCX, XLS/XLSX, CSV, Markdown, TXT (.ppt rich content requires OfficeRead Knowledge rollout)';
export const knowledgeImportSupportedFormatsHintZhHans =
    '支持格式：PDF、PPT/PPTX、DOC/DOCX、XLS/XLSX、CSV、Markdown、TXT（.ppt 富内容需启用 OfficeRead 知识库灰度）';

export type ImportFailedItem = {
    file_path?: string;
    error?: string;
};

export type ImportResult = {
    batch_id?: string;
    status?: string;
    root_path?: string;
    total_files?: number;
    queued_files?: number;
    duplicate_files?: number;
    skipped_files?: number;
    imported_files?: number;
    failed_files?: number;
    processed_files?: number;
    current_file?: string;
    current_step?: string;
    step_progress?: number;
    total_steps?: number;
    current_step_num?: number;
    estimated_bytes?: number;
    warnings?: string[];
    ext_counts?: Record<string, number>;
    failed_items?: ImportFailedItem[];
    last_item_path?: string;
    last_item_status?: string;
    last_item_reason?: string;
};

export type ImportJob = {
    id?: string;
    status?: string;
    error?: string;
    result?: ImportResult;
};

/** True when the import job is still in flight (including background indexing). */
export function isKnowledgeImportJobActive(job: ImportJob | null | undefined): boolean {
    const st = String(job?.status || '').toLowerCase();
    return ['queued', 'running', 'pending', 'indexing'].includes(st);
}

/** True when the import job has finished (success or failure). */
export function isKnowledgeImportJobTerminal(job: ImportJob | null | undefined): boolean {
    const st = String(job?.status || '').toLowerCase();
    return st === 'completed' || st === 'failed';
}

/** Merge a progress event payload into an ImportJob (shared by dialog + global host). */
export function mergeKnowledgeImportProgress(prev: ImportJob | null, data: any): ImportJob {
    const base: ImportJob = prev || { id: data?.job_id };
    const prevStatus = String(base.status || '').toLowerCase();
    const nextStatus = String(data?.status || base.status || '').toLowerCase();
    // Never regress terminal -> indexing/running, and never overwrite failed with completed.
    let status = data?.status || base.status;
    if (
        (prevStatus === 'completed' || prevStatus === 'failed') &&
        (nextStatus === 'indexing' || nextStatus === 'running' || nextStatus === 'queued' || nextStatus === 'pending')
    ) {
        status = base.status;
    } else if (prevStatus === 'failed' && nextStatus === 'completed') {
        status = base.status;
    }

    const mergedResult = { ...(base.result || {}), ...data };
    // When a new file completes, clear stale per-file step fields if omitted (omitempty).
    if ((data?.processed_files || 0) > (base.result?.processed_files || 0)) {
        if (data.current_step === undefined) mergedResult.current_step = '';
        if (data.step_progress === undefined) mergedResult.step_progress = 0;
        if (data.current_step_num === undefined) mergedResult.current_step_num = 0;
        if (data.total_steps === undefined) mergedResult.total_steps = 0;
    }
    // Explicit post-import indexing events always win for step fields.
    if (data?.status === 'indexing' || data?.current_step === 'embedding' || data?.current_step === 'linking') {
        if (data.current_step !== undefined) mergedResult.current_step = data.current_step ?? '';
        if (data.step_progress !== undefined) mergedResult.step_progress = data.step_progress;
        if (data.current_step_num !== undefined) mergedResult.current_step_num = data.current_step_num;
        if (data.total_steps !== undefined) mergedResult.total_steps = data.total_steps;
    }
    // Terminal events clear step labels even when omitempty drops empty strings.
    if (nextStatus === 'completed' || nextStatus === 'failed') {
        if (!data?.current_step) {
            mergedResult.current_step = '';
            mergedResult.step_progress = 0;
            mergedResult.current_step_num = 0;
            mergedResult.total_steps = 0;
        }
    }
    return {
        ...base,
        id: data?.job_id || base.id,
        status,
        error: data?.error || base.error,
        result: mergedResult,
    };
}

type LogEntry = {
    path: string;
    status: 'imported' | 'skipped' | 'failed';
    reason: string;
};

type ImportDialogStep = 'choose' | 'configure' | 'progress' | 'done';

type Props = {
    open: boolean;
    onClose: () => void;
    onJobUpdate?: (job: ImportJob | null) => void;
    /** When the dialog remounts after leaving settings, hydrate from the global job. */
    restoreJob?: ImportJob | null;
    supportedExts?: string[];
    t: TFunc;
    lang: string;
};

export function KnowledgeImportDialog({ open, onClose, onJobUpdate, restoreJob, supportedExts, t, lang }: Props) {
    // Single source of truth: backend-provided list, fallback only if not yet loaded.
    const allExts = supportedExts && supportedExts.length > 0 ? supportedExts : fallbackExts;
    const [step, setStep] = useState<ImportDialogStep>('choose');
    const [importMode, setImportMode] = useState<'directory' | 'files'>('directory');
    const [selectedPath, setSelectedPath] = useState('');
    const [selectedFiles, setSelectedFiles] = useState<string[]>([]);
    const [scanResult, setScanResult] = useState<ImportResult | null>(null);
    const [scanning, setScanning] = useState(false);
    const [scanProgress, setScanProgress] = useState<{ phase: string; done: number; total: number; path: string } | null>(null);
    const [job, setJob] = useState<ImportJob | null>(null);
    const [logEntries, setLogEntries] = useState<LogEntry[]>([]);
    const [error, setError] = useState('');
    const [config, setConfig] = useState({
        saveScope: 'project',
        topicHint: '',
        labels: '',
        recursive: true,
        autoLabels: true,
        includeExts: [...allExts],
        excludeGlobs: '',
        maxFileBytes: 104857600,
        distillMode: '',
    });
    const [showAdvanced, setShowAdvanced] = useState(false);
    const dialogRef = useRef<HTMLDivElement>(null);
    const closeButtonRef = useRef<HTMLButtonElement>(null);
    const closeHandlerRef = useRef<() => void>(() => {});

    // Sync includeExts when backend capabilities arrive (supportedExts prop changes).
    const prevExtsRef = useRef(allExts);
    useEffect(() => {
        if (prevExtsRef.current === allExts) return;
        const prev = prevExtsRef.current;
        prevExtsRef.current = allExts;
        // Only auto-sync if user hasn't manually deselected anything (still matches previous full set).
        setConfig(c => {
            const wasAllSelected = prev.every(ext => c.includeExts.includes(ext)) && c.includeExts.length === prev.length;
            if (wasAllSelected) return { ...c, includeExts: [...allExts] };
            return c;
        });
    }, [allExts]);

    // Notify parent of job updates
    const onJobUpdateRef = useRef(onJobUpdate);
    onJobUpdateRef.current = onJobUpdate;
    useEffect(() => { onJobUpdateRef.current?.(job); }, [job]);

    // Hydrate after settings remount (global float Expand / leave-and-return).
    const hydratedJobIdRef = useRef('');
    useEffect(() => {
        if (!open) return;
        const restoreId = String(restoreJob?.id || '').trim();
        if (!restoreId || job?.id) return;
        if (hydratedJobIdRef.current === restoreId) return;
        hydratedJobIdRef.current = restoreId;
        let cancelled = false;
        void KnowledgeImportJobStatus(restoreId)
            .then(j => {
                if (cancelled || !j) {
                    if (!cancelled && restoreJob) {
                        setJob(restoreJob);
                        setStep(isKnowledgeImportJobTerminal(restoreJob) ? 'done' : 'progress');
                    }
                    return;
                }
                setJob(j as ImportJob);
                setStep(isKnowledgeImportJobTerminal(j as ImportJob) ? 'done' : 'progress');
                // Seed failed items into the log when available on the job result.
                const failed = (j as ImportJob)?.result?.failed_items;
                if (Array.isArray(failed) && failed.length) {
                    setLogEntries(failed
                        .filter(item => item?.file_path)
                        .map(item => ({
                            path: String(item.file_path),
                            status: 'failed' as const,
                            reason: item.error || '',
                        })));
                }
            })
            .catch(() => {
                if (!cancelled && restoreJob) {
                    setJob(restoreJob);
                    setStep(isKnowledgeImportJobTerminal(restoreJob) ? 'done' : 'progress');
                }
            });
        return () => { cancelled = true; };
    }, [open, restoreJob?.id, job?.id]);

    // EventsOn real-time progress
    useEffect(() => {
        if (!job?.id) return;
        const cleanup = EventsOn('knowledge:import-progress', (data: any) => {
            if (data.job_id !== job.id) return;
            setJob(prev => mergeKnowledgeImportProgress(prev, data));
            if (data.last_item_path && data.last_item_status) {
                setLogEntries(prev => {
                    const next = [...prev, {
                        path: data.last_item_path,
                        status: data.last_item_status,
                        reason: data.last_item_reason || '',
                    }];
                    // Cap in-memory entries to prevent unbounded growth
                    return next.length > 500 ? next.slice(-500) : next;
                });
            }
            // Finish payload may include failed_items (throttled progress can drop mid-run reasons).
            if (Array.isArray(data.failed_items) && data.failed_items.length > 0) {
                setLogEntries(prev => {
                    const existing = new Set(prev.map(e => e.path));
                    const extras: LogEntry[] = [];
                    for (const item of data.failed_items as ImportFailedItem[]) {
                        const path = (item?.file_path || '').trim();
                        if (!path || existing.has(path)) continue;
                        extras.push({ path, status: 'failed', reason: item.error || '' });
                        existing.add(path);
                    }
                    if (!extras.length) return prev;
                    const next = [...prev, ...extras];
                    return next.length > 500 ? next.slice(-500) : next;
                });
            }
            if (data.status === 'completed' || data.status === 'failed') {
                setStep('done');
            } else if (data.status === 'indexing') {
                // Files finished; stay on progress to show background index phase.
                setStep('progress');
            }
        });
        return () => { cleanup(); };
    }, [job?.id]);

    // Polling fallback (5s)
    useEffect(() => {
        const id = job?.id;
        const status = (job?.status || '').toLowerCase();
        if (!id || !['queued', 'running', 'pending', 'indexing'].includes(status)) return;
        const handle = window.setInterval(() => {
            void KnowledgeImportJobStatus(id).then(j => {
                if (j) {
                    setJob(j as ImportJob);
                    if (j.status === 'completed' || j.status === 'failed') setStep('done');
                    else if (j.status === 'indexing') setStep('progress');
                }
            }).catch(() => {});
        }, 5000);
        return () => window.clearInterval(handle);
    }, [job?.id, job?.status]);

    const handleClose = () => {
        // Include indexing so closing during background post-work minimizes (does not reset).
        const running = isKnowledgeImportJobActive(job);
        if (step === 'progress' && running) {
            // Import in progress — minimize to floating bar; backend continues.
            // Keep dialog state so Expand restores progress/logs.
            onClose();
            return;
        }
        // Reset state on close (done/choose/configure)
        setStep('choose');
        setJob(null);
        setLogEntries([]);
        setScanResult(null);
        setSelectedPath('');
        setSelectedFiles([]);
        setError('');
        setShowAdvanced(false);
        onClose();
    };
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(handleClose);
    closeHandlerRef.current = handleClose;

    useEffect(() => {
        if (!open) return;
        const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const previousOverflow = document.body.style.overflow;
        document.body.style.overflow = 'hidden';
        const focusFrame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
        const onKeyDown = (event: KeyboardEvent) => {
            const activeElement = document.activeElement;
            if (!(activeElement instanceof HTMLElement) || !dialogRef.current?.contains(activeElement)) return;
            if (event.key === 'Escape') {
                event.preventDefault();
                event.stopImmediatePropagation();
                closeHandlerRef.current();
                return;
            }
            if (event.key !== 'Tab') return;
            const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>(dialogFocusableSelector));
            if (!focusable.length) return;
            const activeIndex = focusable.indexOf(activeElement);
            const nextIndex = event.shiftKey
                ? (activeIndex <= 0 ? focusable.length - 1 : activeIndex - 1)
                : (activeIndex === focusable.length - 1 ? 0 : activeIndex + 1);
            event.preventDefault();
            event.stopImmediatePropagation();
            focusable[nextIndex].focus();
        };
        document.addEventListener('keydown', onKeyDown);
        return () => {
            window.cancelAnimationFrame(focusFrame);
            document.body.style.overflow = previousOverflow;
            document.removeEventListener('keydown', onKeyDown);
            if (previousFocus?.isConnected) previousFocus.focus();
        };
    }, [open]);

    const buildPayload = () => {
        // When all formats are selected, send empty array to let backend use its DefaultIncludeExts.
        // This ensures forward-compatibility when backend adds new formats.
        const allSelected = allExts.every(ext => config.includeExts.includes(ext));
        return {
            root_path: selectedPath,
            topic_hint: config.topicHint.trim(),
            save_scope: config.saveScope,
            distill_mode: config.distillMode,
            labels: config.labels.split(/[,;，；\n]+/).map(s => s.trim()).filter(Boolean),
            auto_labels: config.autoLabels,
            recursive: config.recursive,
            include_exts: allSelected ? [] : config.includeExts,
            exclude_globs: config.excludeGlobs.split(/[,;，；\n]+/).map(s => s.trim()).filter(Boolean),
            max_file_bytes: config.maxFileBytes,
            dry_run: false,
        };
    };

    // Scan progress events while configure-step precheck runs.
    useEffect(() => {
        if (!scanning) return;
        const cleanup = EventsOn('knowledge:scan-progress', (data: any) => {
            setScanProgress({
                phase: String(data?.phase || ''),
                done: Number(data?.done || 0),
                total: Number(data?.total || 0),
                path: String(data?.current_path || ''),
            });
        });
        return () => { cleanup(); };
    }, [scanning]);

    const handleChooseDirectory = async () => {
        try {
            const dir = await SelectKnowledgeDirectory();
            if (!dir) return;
            setSelectedPath(dir);
            setImportMode('directory');
            setStep('configure');
            setScanning(true);
            setScanProgress(null);
            setError('');
            const result = await KnowledgeScanDirectory({ ...buildPayload(), root_path: dir, dry_run: true });
            setScanResult(result);
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setScanning(false);
            setScanProgress(null);
        }
    };

    const handleChooseFiles = async () => {
        try {
            const files = await SelectKnowledgeFiles();
            if (!files || !files.length) return;
            setSelectedFiles(files);
            setImportMode('files');
            setStep('configure');
            setScanning(true);
            setScanProgress(null);
            setError('');
            const result = await KnowledgeScanFiles({ ...buildPayload(), dry_run: true }, files);
            setScanResult(result);
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setScanning(false);
            setScanProgress(null);
        }
    };

    const handleSkipIndexing = async () => {
        const id = job?.id;
        if (!id) return;
        try {
            await KnowledgeCancelImportIndexing(id);
            setJob(prev => {
                if (!prev) return prev;
                const failed = (prev.result?.failed_files || 0) > 0;
                const terminal = failed ? 'failed' : 'completed';
                return {
                    ...prev,
                    status: terminal,
                    result: { ...(prev.result || {}), current_step: '', step_progress: 0, status: terminal },
                };
            });
            setStep('done');
        } catch (err: any) {
            setError(err?.message || String(err));
        }
    };

    const handleStartImport = async () => {
        setError('');
        setLogEntries([]);
        setStep('progress');
        try {
            if (importMode === 'directory') {
                const j = await KnowledgeStartImportDirectory(buildPayload());
                setJob(j);
            } else {
                const j = await KnowledgeStartImportFiles(buildPayload(), selectedFiles);
                setJob(j);
            }
        } catch (err: any) {
            setError(err?.message || String(err));
            setJob(prev => prev ? { ...prev, status: 'failed' } : { status: 'failed' });
            setStep('done');
        }
    };

    const handleRetry = () => {
        setError('');
        setLogEntries([]);
        setJob(null);
        void handleStartImport();
    };

    if (!open) return null;

    const total = job?.result?.total_files || scanResult?.total_files || 0;
    const processed = job?.result?.processed_files || 0;
    // Progress calculation: combines file-level and step-level granularity.
    // For post-import phases (embedding/linking), files are done but step_progress tracks the sub-operation.
    const currentStep = job?.result?.current_step || '';
    const jobStatus = String(job?.status || '').toLowerCase();
    const isIndexing = jobStatus === 'indexing';
    const isPostImportPhase = isIndexing || currentStep === 'embedding' || currentStep === 'linking';
    const stepProgress = job?.result?.step_progress || 0;
    const isDone = step === 'done';
    // File processing occupies 0-85%, linking 85-92%, embedding 92-99%, done=100%.
    const percent = isDone
        ? 100
        : isPostImportPhase
            ? currentStep === 'embedding'
                ? Math.min(99, 92 + Math.round(stepProgress * 0.07))
                : Math.min(92, 85 + Math.round(stepProgress * 0.07))
            : total === 1 && stepProgress > 0
                ? Math.min(85, stepProgress)
                : total > 0 ? Math.round((processed / total) * 85) : 0;

    return (
        <div
            className="knowledge-import-overlay"
            {...backdropProps}
        >
            <div
                ref={dialogRef}
                className="knowledge-import-modal"
                role="dialog"
                aria-modal="true"
                aria-labelledby="knowledge-import-title"
                {...dialogProps}
            >
                {/* Header */}
                <div className="knowledge-import-header">
                    {step === 'configure' && (
                        <button className="knowledge-import-icon-button" aria-label={t('Back', '返回')} onClick={() => setStep('choose')}>←</button>
                    )}
                    {step === 'done' && (
                        <button className="knowledge-import-icon-button" aria-label={t('Back', '返回')} onClick={() => { setStep('choose'); setJob(null); setLogEntries([]); }}>←</button>
                    )}
                    <div className="knowledge-import-title-wrap">
                        <h3 id="knowledge-import-title" className="knowledge-import-title">
                            {step === 'choose' && t('Import to Knowledge Base', '导入知识到知识库')}
                            {step === 'configure' && t('Review & Configure', '预检与配置')}
                            {step === 'progress' && (isIndexing
                                ? t('Files imported · indexing…', '文件已导入 · 后台索引中…')
                                : t('Importing...', '正在导入...'))}
                            {step === 'done' && (job?.status === 'failed' ? t('Import Failed', '导入失败') :
                                (job?.result?.failed_files || 0) > 0 ? t('Import Completed (with errors)', '导入完成（部分失败）') :
                                t('Import Completed', '导入完成'))}
                        </h3>
                    </div>
                    <button
                        ref={closeButtonRef}
                        className="knowledge-import-icon-button"
                        aria-label={
                            step === 'progress' && ['running', 'queued', 'pending', 'indexing'].includes(String(job?.status || '').toLowerCase())
                                ? t('Minimize', '最小化')
                                : t('Close', '关闭')
                        }
                        onClick={handleClose}
                    >
                        {step === 'progress' && ['running', 'queued', 'pending', 'indexing'].includes(String(job?.status || '').toLowerCase()) ? '–' : '×'}
                    </button>
                </div>

                {/* Body */}
                <div className="knowledge-import-body">
                    {error && <div className="knowledge-import-alert knowledge-import-alert--error" role="alert">{error}</div>}

                    {step === 'choose' && (
                        <div className="knowledge-import-choose-grid">
                            <button className="knowledge-import-choice-card" onClick={handleChooseDirectory}>
                                <span className="knowledge-import-choice-icon" aria-hidden="true">DIR</span>
                                <strong>{t('Select Directory', '选择目录')}</strong>
                                <span className="knowledge-import-choice-desc">{t('Scan all documents in a folder', '扫描目录下的所有文档')}</span>
                            </button>
                            <button className="knowledge-import-choice-card" onClick={handleChooseFiles}>
                                <span className="knowledge-import-choice-icon" aria-hidden="true">DOC</span>
                                <strong>{t('Select Files', '选择文件')}</strong>
                                <span className="knowledge-import-choice-desc">{t('Pick one or more document files', '选择一个或多个文档文件')}</span>
                            </button>
                            <div className="knowledge-import-format-hint">
                                {t(knowledgeImportSupportedFormatsHint, knowledgeImportSupportedFormatsHintZhHans)}
                            </div>
                        </div>
                    )}

                    {step === 'configure' && (
                        <div className="knowledge-import-configure">
                            <div className="knowledge-import-path">
                                {importMode === 'directory' ? selectedPath : `${selectedFiles.length} ${t('files selected', '个文件已选择')}`}
                            </div>

                            {scanning ? (
                                <div className="knowledge-import-scanning" role="status">
                                    <div>{t('Scanning files...', '正在扫描文件...')}</div>
                                    {scanProgress && (
                                        <div className="knowledge-import-scan-meta" style={{ marginTop: 6 }}>
                                            {scanProgress.phase === 'hash'
                                                ? t('Hashing files…', '正在计算文件指纹…')
                                                : t('Discovering files…', '正在发现文件…')}
                                            {' '}
                                            {scanProgress.total > 0
                                                ? `${scanProgress.done}/${scanProgress.total}`
                                                : scanProgress.done > 0
                                                    ? `${scanProgress.done}`
                                                    : ''}
                                            {scanProgress.path ? ` · ${truncatePath(scanProgress.path, 48)}` : ''}
                                        </div>
                                    )}
                                </div>
                            ) : scanResult ? (
                                <div className="knowledge-import-scan-box">
                                    <div className="knowledge-import-scan-stat">
                                        <strong>{scanResult.total_files || 0}</strong> {t('files found', '个文件')}
                                        {(scanResult.estimated_bytes || 0) > 0 && (
                                            <span className="knowledge-import-scan-meta"> · {formatBytes(scanResult.estimated_bytes || 0)}</span>
                                        )}
                                        {scanResult.ext_counts && Object.keys(scanResult.ext_counts).length > 0 && (
                                            <span className="knowledge-import-scan-meta"> · {Object.entries(scanResult.ext_counts).map(([ext, n]) => `${ext}(${n})`).join(' ')}</span>
                                        )}
                                    </div>
                                    {scanResult.queued_files !== undefined && scanResult.queued_files < (scanResult.total_files || 0) && (
                                        <div className="knowledge-import-warning" role="status">
                                            {(scanResult.total_files || 0) - (scanResult.queued_files || 0)} {t('files will be skipped', '个文件将被跳过')}
                                        </div>
                                    )}
                                </div>
                            ) : null}

                            <div className="knowledge-import-config-grid">
                                <label className="knowledge-import-field">
                                    {t('Scope', '范围')}
                                    <select className="knowledge-import-input" value={config.saveScope} onChange={e => setConfig({ ...config, saveScope: e.target.value })}>
                                        <option value="project">{t('Project', '项目')}</option>
                                        <option value="personal">{t('Personal', '个人')}</option>
                                        <option value="local_only">{t('Local only', '仅本地')}</option>
                                    </select>
                                </label>
                                <label className="knowledge-import-field">
                                    {t('Topic hint', '主题提示')}
                                    <input className="knowledge-import-input" value={config.topicHint} onChange={e => setConfig({ ...config, topicHint: e.target.value })} placeholder={t('Optional', '可选')} />
                                </label>
                                <label className="knowledge-import-field">
                                    {t('Labels', '标签')}
                                    <input className="knowledge-import-input" value={config.labels} onChange={e => setConfig({ ...config, labels: e.target.value })} placeholder={t('Comma separated', '逗号分隔')} />
                                </label>
                            </div>

                            <div className="knowledge-import-checkbox-row">
                                <label className="knowledge-import-checkbox"><input type="checkbox" checked={config.recursive} onChange={e => setConfig({ ...config, recursive: e.target.checked })} /> {t('Recursive', '递归子目录')}</label>
                                <label className="knowledge-import-checkbox"><input type="checkbox" checked={config.autoLabels} onChange={e => setConfig({ ...config, autoLabels: e.target.checked })} /> {t('Auto labels', '自动标签')}</label>
                            </div>

                            <button className="knowledge-import-advanced-toggle" onClick={() => setShowAdvanced(!showAdvanced)}>
                                {showAdvanced ? '▼' : '▸'} {t('Advanced options', '高级选项')}
                            </button>
                            {showAdvanced && (
                                <div className="knowledge-import-advanced">
                                    <div className="knowledge-import-field">
                                        {t('File formats', '文件格式')}
                                        <div className="knowledge-import-ext-grid">
                                            {allExts.map(ext => (
                                                <label key={ext} className="knowledge-import-ext-checkbox">
                                                    <input
                                                        type="checkbox"
                                                        checked={config.includeExts.includes(ext)}
                                                        onChange={e => {
                                                            setConfig(prev => ({
                                                                ...prev,
                                                                includeExts: e.target.checked
                                                                    ? [...prev.includeExts, ext]
                                                                    : prev.includeExts.filter(x => x !== ext),
                                                            }));
                                                        }}
                                                    />
                                                    {ext}
                                                </label>
                                            ))}
                                        </div>
                                        <div className="knowledge-import-ext-actions">
                                            <button type="button" className="knowledge-import-link-button" onClick={() => setConfig(prev => ({ ...prev, includeExts: [...allExts] }))}>{t('Select all', '全选')}</button>
                                            <button type="button" className="knowledge-import-link-button" onClick={() => setConfig(prev => ({ ...prev, includeExts: [] }))}>{t('Clear all', '清空')}</button>
                                        </div>
                                    </div>
                                    <div className="knowledge-import-config-grid">
                                        <label className="knowledge-import-field">
                                            {t('Exclude globs', '排除规则')}
                                            <input className="knowledge-import-input" value={config.excludeGlobs} onChange={e => setConfig({ ...config, excludeGlobs: e.target.value })} placeholder="node_modules/**" />
                                        </label>
                                    </div>
                                </div>
                            )}

                            <div className="knowledge-import-actions">
                                <button className="knowledge-import-button knowledge-import-button--secondary" onClick={() => setStep('choose')}>{t('Cancel', '取消')}</button>
                                <button className="knowledge-import-button knowledge-import-button--primary" disabled={scanning || !(scanResult?.queued_files) || config.includeExts.length === 0} onClick={handleStartImport}>
                                    {t('Start Import', '开始导入')}
                                </button>
                            </div>
                            {config.includeExts.length === 0 && (
                                <div className="knowledge-import-warning knowledge-import-warning--compact">{t('Please select at least one file format.', '请至少选择一种文件格式。')}</div>
                            )}
                        </div>
                    )}

                    {(step === 'progress' || step === 'done') && (
                        <div className="knowledge-import-progress">
                            {/* Progress bar + percent inline */}
                            <div className="knowledge-import-progress-header">
                                <div className="knowledge-import-progress-track">
                                    <div
                                        className="knowledge-import-progress-fill"
                                        data-tone={step === 'done' ? (job?.status === 'failed' ? 'failed' : (job?.result?.failed_files || 0) > 0 ? 'warning' : 'success') : 'running'}
                                        style={{ width: `${percent}%` }}
                                    />
                                </div>
                                <span className="knowledge-import-percent">{percent}%</span>
                            </div>

                            {/* Current file / step (only during progress) */}
                            {step === 'progress' && job?.result?.current_file && (
                                <div className="knowledge-import-current-file">
                                    <span className="knowledge-import-current-path">{truncatePath(job.result.current_file, 60)}</span>
                                    {job.result.current_step && <span className="knowledge-import-current-step">{stepLabel(job.result.current_step, t)}</span>}
                                </div>
                            )}
                            {/* Post-import phase indicator (no current_file, just step name) */}
                            {step === 'progress' && !job?.result?.current_file && job?.result?.current_step && (
                                <div className="knowledge-import-current-file">
                                    <span className="knowledge-import-current-step">{stepLabel(job.result.current_step, t)}</span>
                                    {(job.result.step_progress || 0) > 0 && <span className="knowledge-import-current-path" style={{ marginLeft: 8 }}>{job.result.step_progress}%</span>}
                                </div>
                            )}
                            {step === 'progress' && isIndexing && (
                                <div className="knowledge-import-actions" style={{ marginTop: 8 }}>
                                    <button
                                        type="button"
                                        className="knowledge-import-button knowledge-import-button--secondary"
                                        onClick={() => { void handleSkipIndexing(); }}
                                    >
                                        {t('Skip background indexing', '跳过后台索引')}
                                    </button>
                                </div>
                            )}

                            {/* Compact stats row */}
                            <div className="knowledge-import-stats">
                                <span className="knowledge-import-stat"><span className="knowledge-import-stat-dot" data-status="imported" />{job?.result?.imported_files || 0} {t('imported', '已导入')}</span>
                                <span className="knowledge-import-stat"><span className="knowledge-import-stat-dot" data-status="skipped" />{job?.result?.skipped_files || 0} {t('skipped', '跳过')}</span>
                                <span className="knowledge-import-stat"><span className="knowledge-import-stat-dot" data-status="failed" />{job?.result?.failed_files || 0} {t('failed', '失败')}</span>
                                <span className="knowledge-import-stat-total">{processed}/{total}</span>
                            </div>

                            {/* Log */}
                            <ImportLog entries={logEntries} done={step === 'done'} hasActiveFile={!!job?.result?.current_file} t={t} />

                            {/* Done actions */}
                            {step === 'done' && (
                                <>
                                <SkippedFilesSummary entries={logEntries} t={t} />
                                <div className="knowledge-import-actions knowledge-import-actions--done">
                                    {job?.status === 'failed' && (
                                        <button className="knowledge-import-button knowledge-import-button--primary" onClick={handleRetry}>{t('Retry', '重试')}</button>
                                    )}
                                    <button className="knowledge-import-button knowledge-import-button--secondary" onClick={handleClose}>{t('Close', '关闭')}</button>
                                </div>
                                </>
                            )}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

/** Compact floating status shown when the import dialog is minimized. */
export function KnowledgeImportFloatingBar({
    job,
    t,
    onExpand,
    onDismiss,
}: {
    job: ImportJob;
    t: TFunc;
    onExpand: () => void;
    onDismiss?: () => void;
}) {
    const status = String(job.status || '').toLowerCase();
    const indexing = status === 'indexing';
    const running = ['queued', 'running', 'pending', 'indexing'].includes(status);
    const partial = status === 'completed' && (job.result?.failed_files || 0) > 0;
    const total = job.result?.total_files || 0;
    const processed = job.result?.processed_files || 0;
    const stepProgress = job.result?.step_progress || 0;
    const currentStep = job.result?.current_step || '';
    const percent = !running
        ? 100
        : indexing
            ? currentStep === 'embedding'
                ? Math.min(99, 92 + Math.round(stepProgress * 0.07))
                : Math.min(92, 85 + Math.round(stepProgress * 0.07))
            : total > 0
                ? Math.min(85, Math.round((processed / total) * 85))
                : 0;
    const tone = !running
        ? (status === 'failed' || ((job.result?.failed_files || 0) > 0 && (job.result?.imported_files || 0) === 0)
            ? 'failed'
            : partial || (job.result?.failed_files || 0) > 0
                ? 'warning'
                : 'success')
        : indexing
            ? 'warning'
            : 'running';
    const label = indexing
        ? t('Files imported · indexing…', '文件已导入 · 后台索引中…')
        : running
            ? t('Importing knowledge…', '正在导入知识…')
            : status === 'failed' || ((job.result?.failed_files || 0) > 0 && (job.result?.imported_files || 0) === 0)
                ? t('Knowledge import failed', '知识库导入失败')
                : (job.result?.failed_files || 0) > 0
                    ? t('Knowledge import finished with errors', '知识库导入完成（有失败）')
                    : t('Knowledge import completed', '知识库导入完成');
    const detail = [
        indexing ? stepLabel(currentStep || 'linking', t) : job.result?.current_file,
        `${job.result?.imported_files || 0} ${t('imported', '已导入')}`,
        (job.result?.failed_files || 0) > 0 ? `${job.result?.failed_files} ${t('failed', '失败')}` : '',
        total > 0 ? `${processed}/${total}` : '',
    ].filter(Boolean).join(' · ');

    return (
        <div className="knowledge-import-float" role="status" data-tone={tone}>
            <button type="button" className="knowledge-import-float-main" onClick={onExpand}>
                <div className="knowledge-import-float-track" aria-hidden="true">
                    <div className="knowledge-import-float-fill" data-tone={tone} style={{ width: `${percent}%` }} />
                </div>
                <div className="knowledge-import-float-text">
                    <strong>{label}</strong>
                    {detail && <span className="knowledge-import-float-detail">{detail}</span>}
                </div>
                <span className="knowledge-import-float-percent">{percent}%</span>
            </button>
            <div className="knowledge-import-float-actions">
                <button type="button" className="knowledge-import-button knowledge-import-button--secondary" onClick={onExpand}>
                    {running ? t('Expand', '展开') : t('Details', '详情')}
                </button>
                {!running && onDismiss && (
                    <button type="button" className="knowledge-import-icon-button" aria-label={t('Dismiss', '关闭')} onClick={onDismiss}>×</button>
                )}
            </div>
        </div>
    );
}

function ImportLog({ entries, done, hasActiveFile, t }: { entries: LogEntry[]; done?: boolean; hasActiveFile?: boolean; t: TFunc }) {
    const bottomRef = useRef<HTMLDivElement>(null);
    useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [entries.length]);
    const maxVisible = 150;
    const visible = entries.length > maxVisible ? entries.slice(-maxVisible) : entries;
    const hidden = entries.length - visible.length;

    if (!entries.length) {
        if (done) return null;
        if (hasActiveFile) {
            return <div className="knowledge-import-log-empty">{t('Processing...', '正在导入...')}</div>;
        }
        return <div className="knowledge-import-log-empty">{t('Waiting to start...', '等待开始...')}</div>;
    }

    return (
        <div className="knowledge-import-log">
            <div className="knowledge-import-log-title">{t('Processing log', '处理日志')}</div>
            <div className="knowledge-import-log-scroll">
                {hidden > 0 && <div className="knowledge-import-log-hidden">... {hidden} {t('more entries', '条更早记录')}</div>}
                {visible.map((entry, i) => (
                    <div key={i} className="knowledge-import-log-entry" data-status={entry.status}>
                        {entry.path}
                        {entry.reason && <span className="knowledge-import-log-reason"> ({entry.reason})</span>}
                    </div>
                ))}
                <div ref={bottomRef} />
            </div>
        </div>
    );
}

function SkippedFilesSummary({ entries, t }: { entries: LogEntry[]; t: TFunc }) {
    const skipped = entries.filter(e => e.status === 'skipped' || e.status === 'failed');
    const [exporting, setExporting] = useState(false);
    const [exportPath, setExportPath] = useState('');

    if (!skipped.length) return null;

    const buildMarkdown = () => {
        const lines: string[] = [
            `# ${t('Import Report - Skipped/Failed Files', '导入报告 - 跳过/失败的文件')}`,
            '',
            `${t('Date', '日期')}: ${new Date().toLocaleString()}`,
            '',
            `| ${t('File', '文件')} | ${t('Status', '状态')} | ${t('Reason', '原因')} |`,
            '| --- | --- | --- |',
        ];
        for (const entry of skipped) {
            const status = entry.status === 'skipped' ? t('Skipped', '跳过') : t('Failed', '失败');
            const reason = entry.reason || '-';
            lines.push(`| ${entry.path.replace(/\|/g, '\\|')} | ${status} | ${reason.replace(/\|/g, '\\|')} |`);
        }
        lines.push('');
        return lines.join('\n');
    };

    const handleExport = async () => {
        setExporting(true);
        try {
            const md = buildMarkdown();
            const filename = `import-report-${new Date().toISOString().slice(0, 10)}.md`;
            const saved = await ExportTextFile(md, filename);
            if (saved) setExportPath(saved);
        } catch { /* ignore cancel */ }
        setExporting(false);
    };

    return (
        <div className="knowledge-import-skipped">
            <div className="knowledge-import-skipped-header">
                <span>{skipped.length} {t('files skipped or failed', '个文件被跳过或失败')}</span>
                <button type="button" className="knowledge-import-button knowledge-import-button--warning" disabled={exporting} onClick={handleExport}>
                    {exporting ? '...' : t('Export Report', '导出报告')}
                </button>
            </div>
            <div className="knowledge-import-skipped-list">
                {skipped.slice(0, 20).map((entry, i) => (
                    <div key={i} className="knowledge-import-skipped-item">
                        <span className="knowledge-import-skipped-path">{entry.path}</span>
                        <span className="knowledge-import-skipped-reason">{entry.reason || (entry.status === 'skipped' ? t('skipped', '跳过') : t('failed', '失败'))}</span>
                    </div>
                ))}
                {skipped.length > 20 && (
                    <div className="knowledge-import-skipped-more">... {t('and', '及')} {skipped.length - 20} {t('more', '更多')}</div>
                )}
            </div>
            {exportPath && <div className="knowledge-import-export-success">{t('Exported to', '已导出到')}: {exportPath}</div>}
        </div>
    );
}

function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function truncatePath(path: string, max: number): string {
    if (path.length <= max) return path;
    return '...' + path.slice(-(max - 3));
}

function stepLabel(step: string, t: TFunc): string {
    switch (step) {
        case 'preparing': return t('Preparing...', '准备中...');
        case 'saving': return t('Saving source...', '保存来源...');
        case 'parsing': return t('Parsing document...', '解析文档...');
        case 'indexing': return t('Indexing content...', '索引内容...');
        case 'indexing_rows': return t('Indexing rows...', '索引行数据...');
        case 'distilling': return t('Generating cards...', '生成知识卡片...');
        case 'embedding': return t('Building vector index...', '构建向量索引...');
        case 'linking': return t('Linking topics...', '关联主题...');
        case 'post_index': return t('Background indexing...', '后台索引中...');
        case 'processing image': return t('Processing image...', '处理图片...');
        default: return step;
    }
}
