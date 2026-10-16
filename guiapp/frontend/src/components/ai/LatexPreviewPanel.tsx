import { useEffect, useRef, useState } from 'react';
import { CompileLatexPreview, CompileLatexWorkbenchFile, GetCodingWorkbenchFilePreview, SaveCodingWorkbenchTextFile } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';
import type { CodePreviewTheme } from './FileTabBar';
import { PdfPreviewPanel } from './PdfPreviewPanel';
import { isTaskResultPdfPreviewURL } from './taskResultPreview';

type CompileResult = {
    ok?: boolean;
    phase?: string;
    path?: string;
    pdf_path?: string;
    preview_url?: string;
    error?: string;
    log?: string;
    repaired?: boolean;
    message?: string;
};

type ProgressEvent = {
    path?: string;
    phase?: string;
    message?: string;
};

export interface LatexPreviewPanelProps {
    absPath?: string;
    /** Cloud or local workbench identity. Compile and save stay on the backend. */
    projectPath?: string;
    relativePath?: string;
    initialContent?: string;
    truncated?: boolean;
    theme: CodePreviewTheme;
    lang: string;
    onSourceChange?: (content: string) => void;
    /** Identity of the current listing. A newer open discards a finished draft. */
    updatedAt?: number;
    /** Non-empty when the source could not be read. Editing stays disabled. */
    readError?: string;
}

function phaseText(phase: string, message: string, zh: boolean): string {
    switch (phase) {
        case 'installing':
            return zh ? `正在安装宏包 ${message}…` : `Installing package ${message}…`;
        case 'repairing':
            return zh ? `正在根据编译错误修复 ${message}…` : `Fixing ${message} from the compile log…`;
        case 'bibtex':
            return zh ? '正在处理参考文献…' : 'Updating the bibliography…';
        case 'error':
            return message;
        default:
            return zh ? `正在编译 ${message || 'LaTeX'}…` : `Compiling ${message || 'LaTeX'}…`;
    }
}

function normalizePreviewPath(value: string): string {
    return value.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
}

export function latexProgressMatches(eventPath: string, absPath: string, relativePath: string): boolean {
    const event = normalizePreviewPath(eventPath || '');
    if (!event) return true;
    const abs = normalizePreviewPath(absPath || '');
    if (abs && event === abs) return true;
    const relative = normalizePreviewPath(relativePath || '').replace(/^\/+/, '');
    if (relative && event === relative) return true;
    if (relative && event.endsWith('/' + relative)) {
        const prefix = event.slice(0, event.length - relative.length - 1);
        if (prefix.includes(':') || prefix.includes('cloud-workspaces')) return true;
    }
    if (!event.includes('/')) {
        const base = (relative || abs).split('/').pop() || '';
        return base !== '' && event === base;
    }
    return false;
}

type LatexBuffer = { text: string; updatedAt: number };

const latexDrafts = new Map<string, LatexBuffer>();
const latexSaved = new Map<string, string>();
const latexBlocked = new Set<string>();
const latexSaveTails = new Map<string, Promise<void>>();
const latexSaveInflight = new Map<string, number>();
const latexSaveIdleListeners = new Map<string, Set<() => void>>();

function latexDraftKey(projectPath: string, relativePath: string) {
    return `${projectPath}\0${relativePath}`;
}

function noteLatexSaved(key: string, content: string) {
    latexSaved.set(key, content);
    if (latexDrafts.get(key)?.text === content) latexDrafts.delete(key);
}

function changeLatexInflight(key: string, delta: number) {
    const next = (latexSaveInflight.get(key) ?? 0) + delta;
    if (next <= 0) {
        latexSaveInflight.delete(key);
        latexSaveIdleListeners.get(key)?.forEach((notify) => notify());
    } else {
        latexSaveInflight.set(key, next);
    }
}

function subscribeLatexSaveIdle(key: string, notify: () => void): () => void {
    let listeners = latexSaveIdleListeners.get(key);
    if (!listeners) {
        listeners = new Set();
        latexSaveIdleListeners.set(key, listeners);
    }
    listeners.add(notify);
    return () => {
        listeners.delete(notify);
        if (listeners.size === 0) latexSaveIdleListeners.delete(key);
    };
}

function enqueueLatexSave(key: string, projectPath: string, relativePath: string, content: string) {
    const prev = latexSaveTails.get(key) ?? Promise.resolve();
    changeLatexInflight(key, 1);
    const job = prev.catch(() => undefined).then(async () => {
        try {
            await SaveCodingWorkbenchTextFile(projectPath, relativePath, content);
            noteLatexSaved(key, content);
        } finally {
            changeLatexInflight(key, -1);
        }
    });
    latexSaveTails.set(key, job);
    return job;
}

function draftText(key: string, updatedAt: number): string | undefined {
    const draft = latexDrafts.get(key);
    if (!draft || latexBlocked.has(key)) return undefined;
    if (draft.updatedAt === updatedAt || (latexSaveInflight.get(key) ?? 0) > 0) return draft.text;
    return undefined;
}

export function clearLatexEditorStateForTests() {
    latexDrafts.clear();
    latexSaved.clear();
    latexBlocked.clear();
    latexSaveTails.clear();
    latexSaveInflight.clear();
    latexSaveIdleListeners.clear();
}

function listenLatexProgress(handler: (data: ProgressEvent) => void): () => void {
    const stop = EventsOn('latex-preview-progress', handler as (...data: unknown[]) => void) as unknown;
    return typeof stop === 'function' ? (stop as () => void) : () => {};
}

function buttonStyle(theme: CodePreviewTheme, disabled: boolean): React.CSSProperties {
    return {
        height: 26,
        padding: '0 10px',
        borderRadius: 4,
        border: `1px solid ${theme.border}`,
        background: disabled ? theme.tabBg : theme.tabActiveBg,
        color: disabled ? theme.textMuted : theme.tabActiveText,
        fontSize: 12,
        cursor: disabled ? 'default' : 'pointer',
    };
}

export function LatexPreviewPanel({
    absPath = '',
    projectPath = '',
    relativePath = '',
    initialContent = '',
    truncated = false,
    theme,
    lang,
    onSourceChange,
    updatedAt = 0,
    readError = '',
}: LatexPreviewPanelProps) {
    const editing = Boolean(projectPath && relativePath && !absPath);
    if (editing) {
        return (
            <LatexWorkbenchEditor
                projectPath={projectPath}
                relativePath={relativePath}
                initialContent={initialContent}
                truncated={truncated}
                theme={theme}
                lang={lang}
                onSourceChange={onSourceChange}
                updatedAt={updatedAt}
                readError={readError}
            />
        );
    }
    return <LatexCompileView absPath={absPath} theme={theme} lang={lang} />;
}

function LatexCompileView({ absPath, theme, lang }: { absPath: string; theme: CodePreviewTheme; lang: string }) {
    const zh = lang.startsWith('zh');
    const [phase, setPhase] = useState('compiling');
    const [message, setMessage] = useState('');
    const [error, setError] = useState('');
    const [log, setLog] = useState('');
    const [pdfPath, setPdfPath] = useState('');
    const [previewUrl, setPreviewUrl] = useState('');
    const [repaired, setRepaired] = useState(false);
    const [attempt, setAttempt] = useState(0);

    useEffect(() => {
        let cancelled = false;
        setPhase('compiling');
        setError('');
        setLog('');
        setPdfPath('');
        setPreviewUrl('');
        setRepaired(false);
        const onProgress = (data: ProgressEvent) => {
            if (cancelled) return;
            if (!latexProgressMatches(String(data?.path || ''), absPath, '')) return;
            if (data?.phase) setPhase(data.phase);
            if (data?.message) setMessage(data.message);
        };
        const stop = listenLatexProgress(onProgress);
        void CompileLatexPreview(absPath).then((result: CompileResult) => {
            if (cancelled) return;
            applyCompileResult(result, zh, {
                setPdfPath, setPreviewUrl, setRepaired, setPhase, setError, setLog,
            });
        }).catch((err: unknown) => {
            if (cancelled) return;
            setPhase('error');
            setError(err instanceof Error ? err.message : String(err || ''));
        });
        return () => {
            cancelled = true;
            stop();
        };
    }, [absPath, attempt, zh]);

    return (
        <LatexCompileBody
            theme={theme}
            zh={zh}
            phase={phase}
            message={message}
            error={error}
            log={log}
            pdfPath={pdfPath}
            previewUrl={previewUrl}
            repaired={repaired}
            onRetry={() => setAttempt((n) => n + 1)}
        />
    );
}

function LatexWorkbenchEditor({
    projectPath,
    relativePath,
    initialContent,
    truncated,
    theme,
    lang,
    onSourceChange,
    updatedAt,
    readError = '',
}: {
    projectPath: string;
    relativePath: string;
    initialContent: string;
    truncated: boolean;
    theme: CodePreviewTheme;
    lang: string;
    onSourceChange?: (content: string) => void;
    updatedAt: number;
    readError?: string;
}) {
    const zh = lang.startsWith('zh');
    const draftKey = latexDraftKey(projectPath, relativePath);
    const remembered = latexSaved.get(draftKey);
    const cachedDraft = draftText(draftKey, updatedAt);
    const [source, setSource] = useState(cachedDraft ?? initialContent);
    const [saved, setSaved] = useState(cachedDraft != null ? (remembered ?? initialContent) : initialContent);
    const [busy, setBusy] = useState(false);
    const [phase, setPhase] = useState('');
    const [message, setMessage] = useState('');
    const [error, setError] = useState('');
    const [log, setLog] = useState('');
    const [pdfPath, setPdfPath] = useState('');
    const [previewUrl, setPreviewUrl] = useState('');
    const [repaired, setRepaired] = useState(false);
    const [editLocked, setEditLocked] = useState(truncated || latexBlocked.has(draftKey));
    const runRef = useRef(0);
    const busyRef = useRef(false);
    const writingRef = useRef(false);
    const aliveRef = useRef(true);
    const stopListenRef = useRef<() => void>(() => {});
    const sourceRef = useRef(source);
    const savedRef = useRef(saved);
    const lockedRef = useRef(truncated || editLocked || Boolean(readError));
    const onSourceChangeRef = useRef(onSourceChange);
    const updatedAtRef = useRef(updatedAt);
    const listingRef = useRef(updatedAt);
    const heldListingRef = useRef<{ updatedAt: number; content: string; savedAtHold: string } | null>(null);
    const [saveEpoch, setSaveEpoch] = useState(0);
    sourceRef.current = source;
    savedRef.current = saved;
    lockedRef.current = truncated || editLocked || Boolean(readError);
    onSourceChangeRef.current = onSourceChange;
    updatedAtRef.current = updatedAt;

    useEffect(() => {
        const draft = latexDrafts.get(draftKey);
        if (!draft || draft.updatedAt === updatedAt || (latexSaveInflight.get(draftKey) ?? 0) > 0) return;
        latexDrafts.delete(draftKey);
    }, [draftKey, updatedAt]);

    useEffect(() => {
        if (!latexBlocked.has(draftKey)) return;
        let cancel = false;
        void GetCodingWorkbenchFilePreview(projectPath, relativePath).then((data: { content?: string; truncated?: boolean }) => {
            if (cancel || !latexBlocked.has(draftKey)) return;
            const next = String(data?.content || '');
            if (data?.truncated || !next) return;
            latexBlocked.delete(draftKey);
            latexDrafts.delete(draftKey);
            latexSaved.set(draftKey, next);
            if (!aliveRef.current) return;
            sourceRef.current = next;
            savedRef.current = next;
            setSource(next);
            setSaved(next);
            setEditLocked(false);
            onSourceChangeRef.current?.(next);
        }).catch(() => undefined);
        return () => { cancel = true; };
    }, [draftKey, projectPath, relativePath]);

    useEffect(() => {
        aliveRef.current = true;
        return () => {
            aliveRef.current = false;
            const writing = writingRef.current;
            runRef.current += 1;
            busyRef.current = false;
            stopListenRef.current();
            if (lockedRef.current || latexBlocked.has(draftKey) || writing) return;
            const text = sourceRef.current;
            if (text === savedRef.current) return;
            const listedAt = updatedAtRef.current;
            latexDrafts.set(draftKey, { text, updatedAt: listedAt });
            void enqueueLatexSave(draftKey, projectPath, relativePath, text).then(() => {
                const newer = latexDrafts.get(draftKey);
                if (newer && newer.text !== text) return;
                onSourceChangeRef.current?.(text);
            }).catch(() => undefined);
        };
    }, [draftKey, projectPath, relativePath]);

    useEffect(() => subscribeLatexSaveIdle(draftKey, () => setSaveEpoch((n) => n + 1)), [draftKey]);

    useEffect(() => {
        const held = heldListingRef.current;
        if (listingRef.current === updatedAt && (!held || held.updatedAt !== updatedAt)) return;
        const nextHeld = held && held.updatedAt === updatedAt && held.content === initialContent
            ? held
            : { updatedAt, content: initialContent, savedAtHold: savedRef.current };
        heldListingRef.current = nextHeld;
        // A listing that arrives while the buffer is dirty or a save is in flight
        // stays pending. Marking it seen here used to drop the expert's write
        // even after the editor returned to the last saved text.
        if (sourceRef.current !== savedRef.current) return;
        if ((latexSaveInflight.get(draftKey) ?? 0) > 0) return;
        const pending = draftText(draftKey, updatedAt);
        if (pending != null && pending !== initialContent && pending !== savedRef.current) return;
        listingRef.current = updatedAt;
        heldListingRef.current = null;
        if (savedRef.current !== nextHeld.savedAtHold) return;
        if (sourceRef.current === initialContent) return;
        if (latexDrafts.get(draftKey)?.text === sourceRef.current) latexDrafts.delete(draftKey);
        sourceRef.current = initialContent;
        savedRef.current = initialContent;
        setSource(initialContent);
        setSaved(initialContent);
    }, [draftKey, initialContent, updatedAt, source, saved, saveEpoch]);

    const persist = async (content: string) => {
        await enqueueLatexSave(draftKey, projectPath, relativePath, content);
        const newer = latexDrafts.get(draftKey);
        if (newer && newer.text !== content) return;
        onSourceChangeRef.current?.(content);
        if (!aliveRef.current || sourceRef.current !== content) return;
        setSaved(content);
        savedRef.current = content;
    };

    const adoptRepairedSource = async () => {
        const data = await GetCodingWorkbenchFilePreview(projectPath, relativePath) as { content?: string; truncated?: boolean };
        const next = String(data?.content || '');
        if (data?.truncated || !next) {
            latexDrafts.delete(draftKey);
            latexBlocked.add(draftKey);
            if (aliveRef.current) setEditLocked(true);
            return;
        }
        latexBlocked.delete(draftKey);
        latexSaved.set(draftKey, next);
        onSourceChangeRef.current?.(next);
        if (!aliveRef.current) {
            latexDrafts.set(draftKey, { text: next, updatedAt });
            return;
        }
        latexDrafts.delete(draftKey);
        sourceRef.current = next;
        savedRef.current = next;
        setSource(next);
        setSaved(next);
    };

    const compile = async () => {
        if (truncated || editLocked || readError || busyRef.current) return;
        const run = ++runRef.current;
        busyRef.current = true;
        writingRef.current = true;
        setBusy(true);
        setPhase('compiling');
        setError('');
        setLog('');
        setPdfPath('');
        setPreviewUrl('');
        setRepaired(false);
        const onProgress = (data: ProgressEvent) => {
            if (run !== runRef.current) return;
            if (!latexProgressMatches(String(data?.path || ''), '', relativePath)) return;
            if (data?.phase) setPhase(data.phase);
            if (data?.message) setMessage(data.message);
        };
        stopListenRef.current();
        stopListenRef.current = listenLatexProgress(onProgress);
        try {
            if (source !== saved) await persist(source);
            else if ((latexSaveInflight.get(draftKey) ?? 0) > 0) await latexSaveTails.get(draftKey);
            const result = await CompileLatexWorkbenchFile(projectPath, relativePath) as CompileResult;
            if (result?.repaired) await adoptRepairedSource();
            if (!aliveRef.current || run !== runRef.current) return;
            applyCompileResult(result, zh, {
                setPdfPath, setPreviewUrl, setRepaired, setPhase, setError, setLog,
            });
        } catch (err: unknown) {
            if (!aliveRef.current || run !== runRef.current) return;
            setPhase('error');
            setError(err instanceof Error ? err.message : String(err || ''));
        } finally {
            if (aliveRef.current && run === runRef.current) {
                busyRef.current = false;
                writingRef.current = false;
                setBusy(false);
                stopListenRef.current();
                stopListenRef.current = () => {};
            }
        }
    };

    const locked = truncated || editLocked || Boolean(readError);
    const dirty = source !== saved;
    const fill: React.CSSProperties = { display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0, background: theme.bg };
    const showingPdf = Boolean(pdfPath || previewUrl);
    return (
        <div data-testid="latex-workbench-editor" style={fill}>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center', padding: '8px 12px', borderBottom: `1px solid ${theme.border}` }}>
                <button
                    type="button"
                    data-testid="latex-workbench-save"
                    disabled={locked || busy || !dirty}
                    onClick={() => {
                        if (locked || busyRef.current || !dirty) return;
                        busyRef.current = true;
                        writingRef.current = true;
                        setBusy(true);
                        setError('');
                        void persist(source).catch((err: unknown) => {
                            if (!aliveRef.current) return;
                            setPhase('error');
                            setError(err instanceof Error ? err.message : String(err || ''));
                        }).finally(() => {
                            if (!aliveRef.current) return;
                            busyRef.current = false;
                            writingRef.current = false;
                            setBusy(false);
                        });
                    }}
                    style={buttonStyle(theme, locked || busy || !dirty)}
                >
                    {zh ? '保存' : 'Save'}
                </button>
                <button
                    type="button"
                    data-testid="latex-workbench-preview"
                    disabled={locked || busy}
                    onClick={() => { void compile(); }}
                    style={buttonStyle(theme, locked || busy)}
                >
                    {zh ? '编译预览' : 'Compile preview'}
                </button>
            </div>
            {truncated && !readError && (
                <div style={{ padding: '8px 12px', fontSize: 12, color: theme.textMuted }}>
                    {zh ? '文件过大，这里只显示了开头一部分，不能保存或编译。' : 'This file is too large to edit or compile here.'}
                </div>
            )}
            {readError && (
                <div style={{ padding: '8px 12px', fontSize: 12, color: theme.textMuted }} data-testid="latex-workbench-read-error">
                    {readError}
                </div>
            )}
            {editLocked && (
                <div style={{ padding: '8px 12px', fontSize: 12, color: theme.textMuted }}>
                    {zh ? '自动修复已写入文件，但内容太长，编辑器没有重新加载。请重新打开该文件后再编辑。' : 'The repaired file was saved, but it is too large to reload here. Reopen it before editing.'}
                </div>
            )}
            <textarea
                data-testid="latex-workbench-source"
                value={source}
                readOnly={locked || busy}
                spellCheck={false}
                onChange={(event) => {
                    if (locked || busy) return;
                    const next = event.target.value;
                    setSource(next);
                    sourceRef.current = next;
                    latexDrafts.set(draftKey, { text: next, updatedAt });
                }}
                style={{
                    flex: showingPdf ? '0 0 38%' : 1,
                    minHeight: 120,
                    margin: 0,
                    padding: 12,
                    border: 0,
                    resize: 'none',
                    outline: 'none',
                    background: theme.bg,
                    color: theme.text,
                    fontFamily: "'Cascadia Code', 'Fira Code', 'Consolas', monospace",
                    fontSize: 13,
                    lineHeight: 1.5,
                }}
            />
            <LatexCompileBody
                theme={theme}
                zh={zh}
                phase={phase}
                message={message}
                error={error}
                log={log}
                pdfPath={pdfPath}
                previewUrl={previewUrl}
                repaired={repaired}
                embedded
            />
        </div>
    );
}

function applyCompileResult(result: CompileResult, zh: boolean, slots: {
    setPdfPath: (value: string) => void;
    setPreviewUrl: (value: string) => void;
    setRepaired: (value: boolean) => void;
    setPhase: (value: string) => void;
    setError: (value: string) => void;
    setLog: (value: string) => void;
}) {
    const preview = String(result?.preview_url || '');
    if (result?.ok && (result.pdf_path || isTaskResultPdfPreviewURL(preview))) {
        slots.setPdfPath(String(result.pdf_path || ''));
        slots.setPreviewUrl(preview);
        slots.setRepaired(!!result.repaired);
        slots.setPhase('ready');
        slots.setError('');
        return;
    }
    slots.setPhase('error');
    slots.setError(String(result?.error || result?.message || (zh ? '编译失败' : 'Compile failed')));
    slots.setLog(String(result?.log || ''));
    slots.setRepaired(!!result?.repaired);
}

function LatexCompileBody({
    theme,
    zh,
    phase,
    message,
    error,
    log,
    pdfPath,
    previewUrl,
    repaired,
    onRetry,
    embedded = false,
}: {
    theme: CodePreviewTheme;
    zh: boolean;
    phase: string;
    message: string;
    error: string;
    log: string;
    pdfPath: string;
    previewUrl: string;
    repaired: boolean;
    onRetry?: () => void;
    embedded?: boolean;
}) {
    const fill: React.CSSProperties = embedded
        ? { display: 'flex', flexDirection: 'column', flex: pdfPath || previewUrl ? 1 : undefined, minHeight: 0 }
        : { display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0, background: theme.bg };
    if (!phase && embedded) return null;
    if (pdfPath || previewUrl) {
        return (
            <div style={fill} data-testid="latex-preview-ready">
                {repaired && (
                    <div style={{ padding: '8px 14px', fontSize: 12, color: theme.textMuted }}>
                        {zh ? '已根据编译错误自动修改源文件并重新编译。原文件保存在同目录 .maclaw-bak。' : 'The source was fixed from the compile log and rebuilt. The previous file is kept as .maclaw-bak.'}
                    </div>
                )}
                <PdfPreviewPanel absPath={pdfPath} previewUrl={previewUrl} theme={theme} lang={zh ? 'zh' : 'en'} />
            </div>
        );
    }
    if (!phase) return null;
    return (
        <div data-testid="latex-preview-status" role="status" style={{ ...fill, padding: embedded ? '8px 12px' : 20, boxSizing: 'border-box', color: phase === 'error' ? theme.diffDeleteText : theme.textMuted, fontSize: 13, lineHeight: 1.6 }}>
            <div>{phase === 'error' ? error : phaseText(phase, message, zh)}</div>
            {log && (
                <pre style={{ marginTop: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontSize: 12, color: theme.text }}>
                    {log}
                </pre>
            )}
            {phase === 'error' && onRetry && (
                <button type="button" onClick={onRetry} style={{ marginTop: 12 }}>
                    {zh ? '重试编译' : 'Compile again'}
                </button>
            )}
        </div>
    );
}
