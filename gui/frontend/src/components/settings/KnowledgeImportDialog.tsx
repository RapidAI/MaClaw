import { useEffect, useRef, useState } from 'react';
import {
    ExportTextFile,
    KnowledgeImportJobStatus,
    KnowledgeScanDirectory,
    KnowledgeScanFiles,
    KnowledgeStartImportDirectory,
    KnowledgeStartImportFiles,
    SelectKnowledgeDirectory,
    SelectKnowledgeFiles,
} from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';

// Fallback only used if capabilities haven't loaded yet (e.g. dialog opened before API returns).
// The authoritative list comes from the backend via the supportedExts prop.
const fallbackExts = ['.pdf', '.pptx', '.docx', '.doc', '.xlsx', '.xls', '.csv', '.md', '.txt'];

type TFunc = (en: string, zhHans: string, zhHant?: string) => string;

type ImportResult = {
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
};

type ImportJob = {
    id?: string;
    status?: string;
    error?: string;
    result?: ImportResult;
};

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
    supportedExts?: string[];
    t: TFunc;
    lang: string;
};

export function KnowledgeImportDialog({ open, onClose, onJobUpdate, supportedExts, t, lang }: Props) {
    // Single source of truth: backend-provided list, fallback only if not yet loaded.
    const allExts = supportedExts && supportedExts.length > 0 ? supportedExts : fallbackExts;
    const [step, setStep] = useState<ImportDialogStep>('choose');
    const [importMode, setImportMode] = useState<'directory' | 'files'>('directory');
    const [selectedPath, setSelectedPath] = useState('');
    const [selectedFiles, setSelectedFiles] = useState<string[]>([]);
    const [scanResult, setScanResult] = useState<ImportResult | null>(null);
    const [scanning, setScanning] = useState(false);
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

    // EventsOn real-time progress
    useEffect(() => {
        if (!job?.id) return;
        const cleanup = EventsOn('knowledge:import-progress', (data: any) => {
            if (data.job_id !== job.id) return;
            setJob(prev => {
                if (!prev) return prev;
                const merged = { ...prev.result, ...data };
                // When a file completes (processed_files increments), clear step fields
                // because omitempty won't send 0 values to overwrite stale step_progress
                if ((data.processed_files || 0) > (prev.result?.processed_files || 0)) {
                    merged.current_step = '';
                    merged.step_progress = 0;
                    merged.current_step_num = 0;
                    merged.total_steps = 0;
                }
                return { ...prev, status: data.status, result: merged };
            });
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
            if (data.status === 'completed' || data.status === 'failed') {
                setStep('done');
            }
        });
        return () => { cleanup(); };
    }, [job?.id]);

    // Polling fallback (5s)
    useEffect(() => {
        const id = job?.id;
        const status = (job?.status || '').toLowerCase();
        if (!id || !['queued', 'running', 'pending'].includes(status)) return;
        const handle = window.setInterval(() => {
            void KnowledgeImportJobStatus(id).then(j => {
                if (j) {
                    setJob(j);
                    if (j.status === 'completed' || j.status === 'failed') setStep('done');
                }
            }).catch(() => {});
        }, 5000);
        return () => window.clearInterval(handle);
    }, [job?.id, job?.status]);

    const handleClose = () => {
        if (step === 'progress' && (job?.status === 'running' || job?.status === 'queued')) {
            // Import in progress — just close the dialog, backend continues
            // Don't reset state so parent can still show status via onJobUpdate
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

    const handleChooseDirectory = async () => {
        try {
            const dir = await SelectKnowledgeDirectory();
            if (!dir) return;
            setSelectedPath(dir);
            setImportMode('directory');
            setStep('configure');
            setScanning(true);
            setError('');
            const result = await KnowledgeScanDirectory({ ...buildPayload(), root_path: dir, dry_run: true });
            setScanResult(result);
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setScanning(false);
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
            setError('');
            const result = await KnowledgeScanFiles({ ...buildPayload(), dry_run: true }, files);
            setScanResult(result);
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setScanning(false);
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
    // Single file: use step_progress for granular feedback; multi-file: use file count
    const percent = total === 1 && (job?.result?.step_progress || 0) > 0
        ? job!.result!.step_progress!
        : total > 0 ? Math.round((processed / total) * 100) : 0;

    return (
        <div className="knowledge-import-overlay" onClick={handleClose}>
            <div className="knowledge-import-modal" onClick={e => e.stopPropagation()} role="dialog" aria-modal="true">
                {/* Header */}
                <div className="knowledge-import-header">
                    {step === 'configure' && (
                        <button className="knowledge-import-icon-button" aria-label={t('Back', '返回')} onClick={() => setStep('choose')}>←</button>
                    )}
                    {step === 'done' && (
                        <button className="knowledge-import-icon-button" aria-label={t('Back', '返回')} onClick={() => { setStep('choose'); setJob(null); setLogEntries([]); }}>←</button>
                    )}
                    <div className="knowledge-import-title-wrap">
                        <h3 className="knowledge-import-title">
                            {step === 'choose' && t('Import to Knowledge Base', '导入知识到知识库')}
                            {step === 'configure' && t('Review & Configure', '预检与配置')}
                            {step === 'progress' && t('Importing...', '正在导入...')}
                            {step === 'done' && (job?.status === 'failed' ? t('Import Failed', '导入失败') :
                                (job?.result?.failed_files || 0) > 0 ? t('Import Completed (with errors)', '导入完成（部分失败）') :
                                t('Import Completed', '导入完成'))}
                        </h3>
                    </div>
                    <button className="knowledge-import-icon-button" aria-label={t('Close', '关闭')} onClick={handleClose}>×</button>
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
                                {t('Supported: PDF, PPTX, DOCX, XLSX, CSV, Markdown, TXT', '支持格式：PDF、PPTX、DOCX、XLSX、CSV、Markdown、TXT')}
                            </div>
                        </div>
                    )}

                    {step === 'configure' && (
                        <div className="knowledge-import-configure">
                            <div className="knowledge-import-path">
                                {importMode === 'directory' ? selectedPath : `${selectedFiles.length} ${t('files selected', '个文件已选择')}`}
                            </div>

                            {scanning ? (
                                <div className="knowledge-import-scanning" role="status">{t('Scanning files...', '正在扫描文件...')}</div>
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

                            {/* Compact stats row */}
                            <div className="knowledge-import-stats">
                                <span className="knowledge-import-stat"><span className="knowledge-import-stat-dot" data-status="imported" />{job?.result?.imported_files || 0} {t('imported', '已导入')}</span>
                                <span className="knowledge-import-stat"><span className="knowledge-import-stat-dot" data-status="skipped" />{job?.result?.skipped_files || 0} {t('skipped', '跳过')}</span>
                                <span className="knowledge-import-stat"><span className="knowledge-import-stat-dot" data-status="failed" />{job?.result?.failed_files || 0} {t('failed', '失败')}</span>
                                <span className="knowledge-import-stat-total">{processed}/{total}</span>
                            </div>

                            {/* Log */}
                            <ImportLog entries={logEntries} done={step === 'done'} t={t} />

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

function ImportLog({ entries, done, t }: { entries: LogEntry[]; done?: boolean; t: TFunc }) {
    const bottomRef = useRef<HTMLDivElement>(null);
    useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [entries.length]);
    const maxVisible = 150;
    const visible = entries.length > maxVisible ? entries.slice(-maxVisible) : entries;
    const hidden = entries.length - visible.length;

    if (!entries.length) {
        if (done) return null;
        return <div className="knowledge-import-log-empty">{t('Waiting for progress...', '等待处理...')}</div>;
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
        case 'distilling': return t('Generating cards...', '生成知识卡片...');
        default: return step;
    }
}
