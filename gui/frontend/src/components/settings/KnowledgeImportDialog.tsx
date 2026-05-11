import { useEffect, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import {
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
    t: TFunc;
    lang: string;
};

export function KnowledgeImportDialog({ open, onClose, onJobUpdate, t, lang }: Props) {
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
        includeExts: '',
        excludeGlobs: '',
        maxFileBytes: 104857600,
        distillMode: '',
    });
    const [showAdvanced, setShowAdvanced] = useState(false);

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
                setLogEntries(prev => [...prev, {
                    path: data.last_item_path,
                    status: data.last_item_status,
                    reason: data.last_item_reason || '',
                }]);
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

    const buildPayload = () => ({
        root_path: selectedPath,
        topic_hint: config.topicHint.trim(),
        save_scope: config.saveScope,
        distill_mode: config.distillMode,
        labels: config.labels.split(/[,;，；\n]+/).map(s => s.trim()).filter(Boolean),
        auto_labels: config.autoLabels,
        recursive: config.recursive,
        include_exts: config.includeExts.split(/[,;，；\n]+/).map(s => s.trim()).filter(Boolean).map(ext => ext.startsWith('.') ? ext : `.${ext}`),
        exclude_globs: config.excludeGlobs.split(/[,;，；\n]+/).map(s => s.trim()).filter(Boolean),
        max_file_bytes: config.maxFileBytes,
        dry_run: false,
    });

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

    const handleRetry = async () => {
        setError('');
        setLogEntries([]);
        setStep('progress');
        await handleStartImport();
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
                    {step !== 'choose' && step !== 'progress' && (
                        <button style={backBtnStyle} onClick={() => setStep(step === 'done' ? 'choose' : 'choose')}>←</button>
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
                                {t('Supported: PDF, DOCX, XLSX, CSV, Markdown, TXT', '支持格式：PDF、DOCX、XLSX、CSV、Markdown、TXT')}
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
                                <div style={configGridStyle}>
                                    <label style={configLabelStyle}>
                                        {t('Include extensions', '包含扩展名')}
                                        <input style={configInputStyle} value={config.includeExts} onChange={e => setConfig({ ...config, includeExts: e.target.value })} placeholder=".pdf,.docx,.md" />
                                    </label>
                                    <label style={configLabelStyle}>
                                        {t('Exclude globs', '排除规则')}
                                        <input style={configInputStyle} value={config.excludeGlobs} onChange={e => setConfig({ ...config, excludeGlobs: e.target.value })} placeholder="node_modules/**" />
                                    </label>
                                </div>
                            )}

                            <div style={actionRowStyle}>
                                <button style={cancelBtnStyle} onClick={() => setStep('choose')}>{t('Cancel', '取消')}</button>
                                <button style={startBtnStyle} disabled={scanning || !(scanResult?.queued_files)} onClick={handleStartImport}>
                                    {t('Start Import', '开始导入')} →
                                </button>
                            </div>
                        </div>
                    )}

                    {(step === 'progress' || step === 'done') && (
                        <div style={progressStyle}>
                            {/* Progress bar */}
                            <div style={progressTrackStyle}>
                                <div style={{
                                    ...progressFillStyle,
                                    width: `${percent}%`,
                                    background: step === 'done'
                                        ? (job?.status === 'failed' ? '#ef4444' : (job?.result?.failed_files || 0) > 0 ? '#f59e0b' : '#22c55e')
                                        : '#3b82f6',
                                }} />
                            </div>
                            <div style={percentRowStyle}>
                                <span>{percent}%</span>
                                <span style={currentFileStyle}>
                                    {step === 'progress' && job?.result?.current_file ? `📄 ${truncatePath(job.result.current_file, 50)}` : ''}
                                    {step === 'progress' && job?.result?.current_step ? ` — ${stepLabel(job.result.current_step, t)}` : ''}
                                </span>
                            </div>

                            {/* Stats grid */}
                            <div style={statsGridStyle}>
                                <div style={statCardStyle}><span style={statNumStyle}>{job?.result?.imported_files || 0}</span><span style={statLabelStyle}>✅ {t('Imported', '已导入')}</span></div>
                                <div style={statCardStyle}><span style={statNumStyle}>{job?.result?.skipped_files || 0}</span><span style={statLabelStyle}>⏭️ {t('Skipped', '已跳过')}</span></div>
                                <div style={statCardStyle}><span style={statNumStyle}>{job?.result?.failed_files || 0}</span><span style={statLabelStyle}>❌ {t('Failed', '失败')}</span></div>
                                <div style={statCardStyle}><span style={statNumStyle}>{total}</span><span style={statLabelStyle}>📁 {t('Total', '总计')}</span></div>
                            </div>

                            {/* Log */}
                            <ImportLog entries={logEntries} t={t} />

                            {/* Done actions */}
                            {step === 'done' && (
                                <div style={actionRowStyle}>
                                    {job?.status === 'failed' && (
                                        <button style={startBtnStyle} onClick={handleRetry}>{t('Retry', '重试')}</button>
                                    )}
                                    <button style={cancelBtnStyle} onClick={handleClose}>{t('Close', '关闭')}</button>
                                </div>
                            )}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

function ImportLog({ entries, t }: { entries: LogEntry[]; t: TFunc }) {
    const bottomRef = useRef<HTMLDivElement>(null);
    useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [entries.length]);
    const maxVisible = 150;
    const visible = entries.length > maxVisible ? entries.slice(-maxVisible) : entries;
    const hidden = entries.length - visible.length;

    if (!entries.length) return <div style={logEmptyStyle}>{t('Waiting for progress...', '等待处理...')}</div>;

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
const modalStyle: CSSProperties = { width: 'min(640px, 92vw)', maxHeight: '85vh', borderRadius: radius.lg, background: colors.surface, boxShadow: '0 20px 60px rgba(0,0,0,0.25)', display: 'flex', flexDirection: 'column', overflow: 'hidden' };
const modalHeaderStyle: CSSProperties = { display: 'flex', alignItems: 'center', gap: 10, padding: '16px 20px', borderBottom: `1px solid ${colors.border}` };
const modalTitleStyle: CSSProperties = { margin: 0, fontSize: 16, fontWeight: 700, color: colors.text };
const modalBodyStyle: CSSProperties = { flex: 1, overflow: 'auto', padding: '20px 24px' };
const closeBtnStyle: CSSProperties = { background: 'none', border: 'none', fontSize: 18, cursor: 'pointer', color: colors.textMuted, padding: '4px 8px', borderRadius: radius.sm };
const backBtnStyle: CSSProperties = { ...closeBtnStyle, fontSize: 16 };

// Step 1: Choose
const chooseGridStyle: CSSProperties = { display: 'flex', flexWrap: 'wrap', justifyContent: 'center', gap: 20, padding: '32px 0' };
const chooseCardStyle: CSSProperties = { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8, width: '100%', maxWidth: 260, padding: '28px 20px', border: `2px solid ${colors.border}`, borderRadius: radius.lg, background: colors.surface, cursor: 'pointer', transition: 'border-color 0.15s, box-shadow 0.15s' };
const chooseIconStyle: CSSProperties = { fontSize: 36 };
const chooseDescStyle: CSSProperties = { fontSize: 12, color: colors.textMuted, textAlign: 'center' };
const formatHintStyle: CSSProperties = { fontSize: 12, color: colors.textMuted, textAlign: 'center', marginTop: 8, width: '100%' };

// Step 2: Configure
const configureStyle: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 14 };
const pathDisplayStyle: CSSProperties = { fontSize: 13, color: colors.textSecondary, padding: '8px 12px', background: colors.surfaceMuted, borderRadius: radius.sm, wordBreak: 'break-all' };
const scanningStyle: CSSProperties = { fontSize: 13, color: colors.textMuted, padding: 12, textAlign: 'center' };
const scanResultBoxStyle: CSSProperties = { padding: '12px 14px', border: `1px solid ${colors.border}`, borderRadius: radius.sm, display: 'flex', flexDirection: 'column', gap: 6 };
const scanStatStyle: CSSProperties = { fontSize: 14, fontWeight: 600, color: colors.text };
const scanMetaStyle: CSSProperties = { fontWeight: 400, color: colors.textMuted };
const scanWarnStyle: CSSProperties = { fontSize: 12, color: '#d97706' };
const configGridStyle: CSSProperties = { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 };
const configLabelStyle: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, color: colors.textMuted };
const configInputStyle: CSSProperties = { width: '100%', boxSizing: 'border-box', border: `1px solid ${colors.border}`, borderRadius: radius.sm, padding: '7px 9px', background: colors.surface, color: colors.text, fontSize: 13 };
const checkboxRowStyle: CSSProperties = { display: 'flex', gap: 16, flexWrap: 'wrap' };
const cbStyle: CSSProperties = { display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 13, color: colors.textMuted, cursor: 'pointer' };
const advancedToggleStyle: CSSProperties = { background: 'none', border: 'none', color: colors.textMuted, fontSize: 12, cursor: 'pointer', padding: '4px 0', textAlign: 'left' };
const actionRowStyle: CSSProperties = { display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 8 };
const cancelBtnStyle: CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.sm, padding: '8px 16px', background: colors.surface, color: colors.text, cursor: 'pointer', fontSize: 13 };
const startBtnStyle: CSSProperties = { border: `1px solid ${colors.primary}`, borderRadius: radius.sm, padding: '8px 20px', background: colors.primaryLight, color: colors.primaryDark, fontWeight: 700, cursor: 'pointer', fontSize: 13 };

// Step 3: Progress
const progressStyle: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 14 };
const progressTrackStyle: CSSProperties = { height: 10, borderRadius: 5, background: colors.surfaceMuted, overflow: 'hidden' };
const progressFillStyle: CSSProperties = { height: '100%', borderRadius: 5, transition: 'width 0.3s ease' };
const percentRowStyle: CSSProperties = { display: 'flex', justifyContent: 'space-between', fontSize: 13, color: colors.textMuted };
const currentFileStyle: CSSProperties = { fontSize: 12, color: colors.textMuted, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '70%' };
const statsGridStyle: CSSProperties = { display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 8 };
const statCardStyle: CSSProperties = { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2, padding: '10px 6px', border: `1px solid ${colors.border}`, borderRadius: radius.sm };
const statNumStyle: CSSProperties = { fontSize: 20, fontWeight: 700, color: colors.text };
const statLabelStyle: CSSProperties = { fontSize: 11, color: colors.textMuted };

// Log
const logContainerStyle: CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.sm, overflow: 'hidden' };
const logTitleStyle: CSSProperties = { fontSize: 12, fontWeight: 600, color: colors.textMuted, padding: '6px 10px', borderBottom: `1px solid ${colors.border}`, background: colors.surfaceMuted };
const logScrollStyle: CSSProperties = { maxHeight: 160, overflowY: 'auto', padding: '6px 10px', fontSize: 12, fontFamily: 'monospace' };
const logEntryStyle: CSSProperties = { padding: '2px 0', color: colors.textSecondary };
const logReasonStyle: CSSProperties = { color: colors.textMuted };
const logHiddenStyle: CSSProperties = { color: colors.textMuted, fontStyle: 'italic', padding: '2px 0' };
const logEmptyStyle: CSSProperties = { fontSize: 13, color: colors.textMuted, textAlign: 'center', padding: 20 };
const errorStyle: CSSProperties = { border: '1px solid #fecaca', borderRadius: radius.sm, padding: 10, background: '#fef2f2', color: '#b91c1c', fontSize: 13, marginBottom: 8 };
