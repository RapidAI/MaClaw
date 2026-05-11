import { useEffect, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
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
import { colors, radius } from '../remote/styles';

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
        <div style={overlayStyle} onClick={handleClose}>
            <div style={modalStyle} onClick={e => e.stopPropagation()}>
                {/* Header */}
                <div style={modalHeaderStyle}>
                    {step === 'configure' && (
                        <button style={backBtnStyle} onClick={() => setStep('choose')}>←</button>
                    )}
                    {step === 'done' && (
                        <button style={backBtnStyle} onClick={() => { setStep('choose'); setJob(null); setLogEntries([]); }}>←</button>
                    )}
                    <div style={{ flex: 1 }}>
                        <h3 style={modalTitleStyle}>
                            {step === 'choose' && `📥 ${t('Import to Knowledge Base', '导入知识到知识库')}`}
                            {step === 'configure' && `📊 ${t('Review & Configure', '预检与配置')}`}
                            {step === 'progress' && `🔄 ${t('Importing...', '正在导入...')}`}
                            {step === 'done' && (job?.status === 'failed' ? `❌ ${t('Import Failed', '导入失败')}` :
                                (job?.result?.failed_files || 0) > 0 ? `⚠️ ${t('Import Completed (with errors)', '导入完成（部分失败）')}` :
                                `✅ ${t('Import Completed', '导入完成')}`)}
                        </h3>
                    </div>
                    <button style={closeBtnStyle} onClick={handleClose}>✕</button>
                </div>

                {/* Body */}
                <div style={modalBodyStyle}>
                    {error && <div style={errorStyle}>{error}</div>}

                    {step === 'choose' && (
                        <div style={chooseGridStyle}>
                            <button style={chooseCardStyle} onClick={handleChooseDirectory}>
                                <span style={chooseIconStyle}>📁</span>
                                <strong>{t('Select Directory', '选择目录')}</strong>
                                <span style={chooseDescStyle}>{t('Scan all documents in a folder', '扫描目录下的所有文档')}</span>
                            </button>
                            <button style={chooseCardStyle} onClick={handleChooseFiles}>
                                <span style={chooseIconStyle}>📄</span>
                                <strong>{t('Select Files', '选择文件')}</strong>
                                <span style={chooseDescStyle}>{t('Pick one or more document files', '选择一个或多个文档文件')}</span>
                            </button>
                            <div style={formatHintStyle}>
                                {t('Supported: PDF, PPTX, DOCX, XLSX, CSV, Markdown, TXT', '支持格式：PDF、PPTX、DOCX、XLSX、CSV、Markdown、TXT')}
                            </div>
                        </div>
                    )}

                    {step === 'configure' && (
                        <div style={configureStyle}>
                            <div style={pathDisplayStyle}>
                                {importMode === 'directory' ? `📁 ${selectedPath}` : `📄 ${selectedFiles.length} ${t('files selected', '个文件已选择')}`}
                            </div>

                            {scanning ? (
                                <div style={scanningStyle}>⏳ {t('Scanning files...', '正在扫描文件...')}</div>
                            ) : scanResult ? (
                                <div style={scanResultBoxStyle}>
                                    <div style={scanStatStyle}>
                                        <strong>{scanResult.total_files || 0}</strong> {t('files found', '个文件')}
                                        {(scanResult.estimated_bytes || 0) > 0 && (
                                            <span style={scanMetaStyle}> · {formatBytes(scanResult.estimated_bytes || 0)}</span>
                                        )}
                                        {scanResult.ext_counts && Object.keys(scanResult.ext_counts).length > 0 && (
                                            <span style={scanMetaStyle}> · {Object.entries(scanResult.ext_counts).map(([ext, n]) => `${ext}(${n})`).join(' ')}</span>
                                        )}
                                    </div>
                                    {scanResult.queued_files !== undefined && scanResult.queued_files < (scanResult.total_files || 0) && (
                                        <div style={scanWarnStyle}>
                                            ⚠️ {(scanResult.total_files || 0) - (scanResult.queued_files || 0)} {t('files will be skipped', '个文件将被跳过')}
                                        </div>
                                    )}
                                </div>
                            ) : null}

                            <div style={configGridStyle}>
                                <label style={configLabelStyle}>
                                    {t('Scope', '范围')}
                                    <select style={configInputStyle} value={config.saveScope} onChange={e => setConfig({ ...config, saveScope: e.target.value })}>
                                        <option value="project">{t('Project', '项目')}</option>
                                        <option value="personal">{t('Personal', '个人')}</option>
                                        <option value="local_only">{t('Local only', '仅本地')}</option>
                                    </select>
                                </label>
                                <label style={configLabelStyle}>
                                    {t('Topic hint', '主题提示')}
                                    <input style={configInputStyle} value={config.topicHint} onChange={e => setConfig({ ...config, topicHint: e.target.value })} placeholder={t('Optional', '可选')} />
                                </label>
                                <label style={configLabelStyle}>
                                    {t('Labels', '标签')}
                                    <input style={configInputStyle} value={config.labels} onChange={e => setConfig({ ...config, labels: e.target.value })} placeholder={t('Comma separated', '逗号分隔')} />
                                </label>
                            </div>

                            <div style={checkboxRowStyle}>
                                <label style={cbStyle}><input type="checkbox" checked={config.recursive} onChange={e => setConfig({ ...config, recursive: e.target.checked })} /> {t('Recursive', '递归子目录')}</label>
                                <label style={cbStyle}><input type="checkbox" checked={config.autoLabels} onChange={e => setConfig({ ...config, autoLabels: e.target.checked })} /> {t('Auto labels', '自动标签')}</label>
                            </div>

                            <button style={advancedToggleStyle} onClick={() => setShowAdvanced(!showAdvanced)}>
                                {showAdvanced ? '▼' : '▸'} {t('Advanced options', '高级选项')}
                            </button>
                            {showAdvanced && (
                                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                                    <div style={configLabelStyle}>
                                        {t('File formats', '文件格式')}
                                        <div style={extCheckboxGridStyle}>
                                            {allExts.map(ext => (
                                                <label key={ext} style={extCheckboxLabelStyle}>
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
                                        <div style={{ display: 'flex', gap: 8, marginTop: 2 }}>
                                            <button type="button" style={extQuickBtnStyle} onClick={() => setConfig(prev => ({ ...prev, includeExts: [...allExts] }))}>{t('Select all', '全选')}</button>
                                            <button type="button" style={extQuickBtnStyle} onClick={() => setConfig(prev => ({ ...prev, includeExts: [] }))}>{t('Clear all', '清空')}</button>
                                        </div>
                                    </div>
                                    <div style={configGridStyle}>
                                        <label style={configLabelStyle}>
                                            {t('Exclude globs', '排除规则')}
                                            <input style={configInputStyle} value={config.excludeGlobs} onChange={e => setConfig({ ...config, excludeGlobs: e.target.value })} placeholder="node_modules/**" />
                                        </label>
                                    </div>
                                </div>
                            )}

                            <div style={actionRowStyle}>
                                <button style={cancelBtnStyle} onClick={() => setStep('choose')}>{t('Cancel', '取消')}</button>
                                <button style={startBtnStyle} disabled={scanning || !(scanResult?.queued_files) || config.includeExts.length === 0} onClick={handleStartImport}>
                                    {t('Start Import', '开始导入')} →
                                </button>
                            </div>
                            {config.includeExts.length === 0 && (
                                <div style={{ fontSize: 11, color: '#d97706', marginTop: -4 }}>{t('Please select at least one file format.', '请至少选择一种文件格式。')}</div>
                            )}
                        </div>
                    )}

                    {(step === 'progress' || step === 'done') && (
                        <div style={progressStyle}>
                            {/* Progress bar + percent inline */}
                            <div style={progressHeaderStyle}>
                                <div style={progressTrackStyle}>
                                    <div style={{
                                        ...progressFillStyle,
                                        width: `${percent}%`,
                                        background: step === 'done'
                                            ? (job?.status === 'failed' ? '#ef4444' : (job?.result?.failed_files || 0) > 0 ? '#f59e0b' : '#22c55e')
                                            : '#3b82f6',
                                    }} />
                                </div>
                                <span style={percentTextStyle}>{percent}%</span>
                            </div>

                            {/* Current file / step (only during progress) */}
                            {step === 'progress' && job?.result?.current_file && (
                                <div style={currentFileRowStyle}>
                                    <span style={currentFileStyle}>{truncatePath(job.result.current_file, 60)}</span>
                                    {job.result.current_step && <span style={currentStepStyle}>{stepLabel(job.result.current_step, t)}</span>}
                                </div>
                            )}

                            {/* Compact stats row */}
                            <div style={statsRowStyle}>
                                <span style={statItemStyle}><span style={statDotImported} />{job?.result?.imported_files || 0} {t('imported', '已导入')}</span>
                                <span style={statItemStyle}><span style={statDotSkipped} />{job?.result?.skipped_files || 0} {t('skipped', '跳过')}</span>
                                <span style={statItemStyle}><span style={statDotFailed} />{job?.result?.failed_files || 0} {t('failed', '失败')}</span>
                                <span style={statItemTotalStyle}>{processed}/{total}</span>
                            </div>

                            {/* Log */}
                            <ImportLog entries={logEntries} done={step === 'done'} t={t} />

                            {/* Done actions */}
                            {step === 'done' && (
                                <>
                                <SkippedFilesSummary entries={logEntries} t={t} />
                                <div style={doneActionRowStyle}>
                                    {job?.status === 'failed' && (
                                        <button style={retryBtnStyle} onClick={handleRetry}>{t('Retry', '重试')}</button>
                                    )}
                                    <button style={closeDoneBtnStyle} onClick={handleClose}>{t('Close', '关闭')}</button>
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
        return <div style={logEmptyStyle}>{t('Waiting for progress...', '等待处理...')}</div>;
    }

    return (
        <div style={logContainerStyle}>
            <div style={logTitleStyle}>{t('Processing log', '处理日志')}</div>
            <div style={logScrollStyle}>
                {hidden > 0 && <div style={logHiddenStyle}>... {hidden} {t('more entries', '条更早记录')}</div>}
                {visible.map((entry, i) => (
                    <div key={i} style={logEntryStyle}>
                        {entry.status === 'imported' && '✅'}
                        {entry.status === 'skipped' && '⏭️'}
                        {entry.status === 'failed' && '❌'}
                        {' '}{entry.path}
                        {entry.reason && <span style={logReasonStyle}> ({entry.reason})</span>}
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
        <div style={skippedSummaryStyle}>
            <div style={skippedHeaderStyle}>
                <span>⚠️ {skipped.length} {t('files skipped or failed', '个文件被跳过或失败')}</span>
                <button type="button" style={exportBtnStyle} disabled={exporting} onClick={handleExport}>
                    {exporting ? '...' : `📄 ${t('Export Report', '导出报告')}`}
                </button>
            </div>
            <div style={skippedListStyle}>
                {skipped.slice(0, 20).map((entry, i) => (
                    <div key={i} style={skippedItemStyle}>
                        <span style={skippedPathStyle}>{entry.path}</span>
                        <span style={skippedReasonStyle}>{entry.reason || (entry.status === 'skipped' ? t('skipped', '跳过') : t('failed', '失败'))}</span>
                    </div>
                ))}
                {skipped.length > 20 && (
                    <div style={skippedMoreStyle}>... {t('and', '及')} {skipped.length - 20} {t('more', '更多')}</div>
                )}
            </div>
            {exportPath && <div style={exportSuccessStyle}>✅ {t('Exported to', '已导出到')}: {exportPath}</div>}
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

// ── Styles ──

const overlayStyle: CSSProperties = { position: 'fixed', inset: 0, background: colors.overlay, display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 9999 };
const modalStyle: CSSProperties = { width: 'min(560px, 92vw)', maxHeight: '80vh', borderRadius: radius.lg, background: colors.surface, boxShadow: '0 16px 48px rgba(0,0,0,0.2)', display: 'flex', flexDirection: 'column', overflow: 'hidden' };
const modalHeaderStyle: CSSProperties = { display: 'flex', alignItems: 'center', gap: 8, padding: '12px 16px', borderBottom: `1px solid ${colors.border}` };
const modalTitleStyle: CSSProperties = { margin: 0, fontSize: 14, fontWeight: 600, color: colors.text };
const modalBodyStyle: CSSProperties = { flex: 1, overflow: 'auto', padding: '14px 18px' };
const closeBtnStyle: CSSProperties = { background: 'none', border: 'none', fontSize: 18, cursor: 'pointer', color: colors.textMuted, padding: '4px 8px', borderRadius: radius.sm };
const backBtnStyle: CSSProperties = { ...closeBtnStyle, fontSize: 16 };

// Step 1: Choose
const chooseGridStyle: CSSProperties = { display: 'flex', flexWrap: 'wrap', justifyContent: 'center', gap: 14, padding: '20px 0' };
const chooseCardStyle: CSSProperties = { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6, width: '100%', maxWidth: 220, padding: '20px 16px', border: `1.5px solid ${colors.border}`, borderRadius: radius.sm, background: colors.surface, cursor: 'pointer', transition: 'border-color 0.15s, box-shadow 0.15s' };
const chooseIconStyle: CSSProperties = { fontSize: 28 };
const chooseDescStyle: CSSProperties = { fontSize: 11, color: colors.textMuted, textAlign: 'center' };
const formatHintStyle: CSSProperties = { fontSize: 11, color: colors.textMuted, textAlign: 'center', marginTop: 4, width: '100%' };

// Step 2: Configure
const configureStyle: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 10 };
const pathDisplayStyle: CSSProperties = { fontSize: 12, color: colors.textSecondary, padding: '6px 10px', background: colors.surfaceMuted, borderRadius: radius.sm, wordBreak: 'break-all' };
const scanningStyle: CSSProperties = { fontSize: 12, color: colors.textMuted, padding: 10, textAlign: 'center' };
const scanResultBoxStyle: CSSProperties = { padding: '8px 12px', border: `1px solid ${colors.border}`, borderRadius: radius.sm, display: 'flex', flexDirection: 'column', gap: 4 };
const scanStatStyle: CSSProperties = { fontSize: 13, fontWeight: 600, color: colors.text };
const scanMetaStyle: CSSProperties = { fontWeight: 400, color: colors.textMuted };
const scanWarnStyle: CSSProperties = { fontSize: 11, color: '#d97706' };
const configGridStyle: CSSProperties = { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 };
const configLabelStyle: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 3, fontSize: 11, color: colors.textMuted };
const configInputStyle: CSSProperties = { width: '100%', boxSizing: 'border-box', border: `1px solid ${colors.border}`, borderRadius: radius.sm, padding: '5px 8px', background: colors.surface, color: colors.text, fontSize: 12 };
const checkboxRowStyle: CSSProperties = { display: 'flex', gap: 14, flexWrap: 'wrap' };
const cbStyle: CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 12, color: colors.textMuted, cursor: 'pointer' };
const advancedToggleStyle: CSSProperties = { background: 'none', border: 'none', color: colors.textMuted, fontSize: 11, cursor: 'pointer', padding: '2px 0', textAlign: 'left' };
const extCheckboxGridStyle: CSSProperties = { display: 'flex', flexWrap: 'wrap', gap: '4px 12px', marginTop: 4 };
const extCheckboxLabelStyle: CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 12, color: colors.textSecondary, cursor: 'pointer' };
const extQuickBtnStyle: CSSProperties = { background: 'none', border: 'none', color: colors.primary, fontSize: 11, cursor: 'pointer', padding: 0, textDecoration: 'underline' };
const actionRowStyle: CSSProperties = { display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 6 };
const cancelBtnStyle: CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.sm, padding: '6px 14px', background: colors.surface, color: colors.text, cursor: 'pointer', fontSize: 12 };
const startBtnStyle: CSSProperties = { border: `1px solid ${colors.primary}`, borderRadius: radius.sm, padding: '6px 16px', background: colors.primaryLight, color: colors.primaryDark, fontWeight: 600, cursor: 'pointer', fontSize: 12 };

// Step 3: Progress (compact)
const progressStyle: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 10 };
const progressHeaderStyle: CSSProperties = { display: 'flex', alignItems: 'center', gap: 10 };
const progressTrackStyle: CSSProperties = { flex: 1, height: 6, borderRadius: 3, background: colors.surfaceMuted, overflow: 'hidden' };
const progressFillStyle: CSSProperties = { height: '100%', borderRadius: 3, transition: 'width 0.3s ease' };
const percentTextStyle: CSSProperties = { fontSize: 12, fontWeight: 600, color: colors.text, minWidth: 32, textAlign: 'right' as const };
const currentFileRowStyle: CSSProperties = { display: 'flex', alignItems: 'center', gap: 8, padding: '4px 0' };
const currentFileStyle: CSSProperties = { fontSize: 11, color: colors.textMuted, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 };
const currentStepStyle: CSSProperties = { fontSize: 11, color: colors.textSecondary, flexShrink: 0, padding: '1px 6px', background: colors.surfaceMuted, borderRadius: 3 };
const statsRowStyle: CSSProperties = { display: 'flex', alignItems: 'center', gap: 14, padding: '6px 10px', background: colors.surfaceMuted, borderRadius: radius.sm, fontSize: 12 };
const statItemStyle: CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 4, color: colors.textSecondary };
const statItemTotalStyle: CSSProperties = { marginLeft: 'auto', color: colors.textMuted, fontWeight: 500 };
const statDotImported: CSSProperties = { display: 'inline-block', width: 7, height: 7, borderRadius: '50%', background: '#22c55e' };
const statDotSkipped: CSSProperties = { display: 'inline-block', width: 7, height: 7, borderRadius: '50%', background: '#f59e0b' };
const statDotFailed: CSSProperties = { display: 'inline-block', width: 7, height: 7, borderRadius: '50%', background: '#ef4444' };
const doneActionRowStyle: CSSProperties = { display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 4 };
const retryBtnStyle: CSSProperties = { border: `1px solid ${colors.primary}`, borderRadius: radius.sm, padding: '6px 14px', background: colors.primaryLight, color: colors.primaryDark, fontWeight: 600, cursor: 'pointer', fontSize: 12 };
const closeDoneBtnStyle: CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.sm, padding: '6px 14px', background: colors.surface, color: colors.text, cursor: 'pointer', fontSize: 12 };

// Log (compact)
const logContainerStyle: CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.sm, overflow: 'hidden' };
const logTitleStyle: CSSProperties = { fontSize: 11, fontWeight: 600, color: colors.textMuted, padding: '4px 10px', borderBottom: `1px solid ${colors.border}`, background: colors.surfaceMuted, textTransform: 'uppercase' as const, letterSpacing: '0.5px' };
const logScrollStyle: CSSProperties = { maxHeight: 120, overflowY: 'auto', padding: '4px 10px', fontSize: 11, fontFamily: 'monospace', lineHeight: '1.6' };
const logEntryStyle: CSSProperties = { padding: '1px 0', color: colors.textSecondary };
const logReasonStyle: CSSProperties = { color: colors.textMuted, fontSize: 10 };
const logHiddenStyle: CSSProperties = { color: colors.textMuted, fontStyle: 'italic', padding: '1px 0', fontSize: 10 };
const logEmptyStyle: CSSProperties = { fontSize: 12, color: colors.textMuted, textAlign: 'center', padding: 14 };

// Skipped files summary
const skippedSummaryStyle: CSSProperties = { border: `1px solid #f59e0b`, borderRadius: radius.sm, overflow: 'hidden', marginTop: 4 };
const skippedHeaderStyle: CSSProperties = { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '6px 10px', background: '#fffbeb', fontSize: 12, fontWeight: 600, color: '#92400e' };
const exportBtnStyle: CSSProperties = { background: 'none', border: `1px solid #d97706`, borderRadius: radius.sm, padding: '3px 10px', fontSize: 11, color: '#92400e', cursor: 'pointer' };
const skippedListStyle: CSSProperties = { maxHeight: 140, overflowY: 'auto', padding: '4px 10px', fontSize: 11, lineHeight: '1.7' };
const skippedItemStyle: CSSProperties = { display: 'flex', justifyContent: 'space-between', gap: 8, padding: '1px 0', color: colors.textSecondary };
const skippedPathStyle: CSSProperties = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 };
const skippedReasonStyle: CSSProperties = { flexShrink: 0, color: colors.textMuted, fontSize: 10 };
const skippedMoreStyle: CSSProperties = { color: colors.textMuted, fontStyle: 'italic', padding: '2px 0' };
const exportSuccessStyle: CSSProperties = { fontSize: 11, color: '#16a34a', padding: '4px 10px', borderTop: `1px solid ${colors.border}` };

const errorStyle: CSSProperties = { border: '1px solid #fecaca', borderRadius: radius.sm, padding: 10, background: '#fef2f2', color: '#b91c1c', fontSize: 13, marginBottom: 8 };
