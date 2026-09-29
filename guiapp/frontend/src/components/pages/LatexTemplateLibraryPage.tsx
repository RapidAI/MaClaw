import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import './LatexTemplateLibraryPage.css';
import {
    LATEX_BLANK_TEMPLATE_ID,
    groupLatexTemplatesByCategory,
    latexShareStatusLabel,
    latexTemplateSourceLabel,
    latexTemplateText,
    parseLatexTemplateLibrary,
    type LatexTemplate,
    type LatexTemplateLibrary,
} from '../../utils/latexTemplates';
import { getWailsAppModule } from '../../utils/wailsAppModule';

export type LatexTemplateLibraryPageProps = {
    lang: string;
    /** Open the LaTeX paper expert on a template the user picked. */
    onUseTemplate: (template: LatexTemplate) => void | Promise<void>;
    /** Open the library without starting a document. */
    onClose?: () => void;
};

type LoadState = 'loading' | 'ready' | 'error';

const EMPTY_LIBRARY: LatexTemplateLibrary = { categories: [], templates: [] };

/** Four cards a row, five rows a page. */
const TEMPLATES_PER_PAGE = 20;

function pageWindow<T>(items: T[], page: number, pageSize: number): { page: number; pages: number; items: T[] } {
    const pages = Math.max(1, Math.ceil(items.length / pageSize));
    const current = Math.min(Math.max(1, page || 1), pages);
    const start = (current - 1) * pageSize;
    return { page: current, pages, items: items.slice(start, start + pageSize) };
}

function readCatalogueSummary(raw: unknown): { added: number; remaining: number } | null {
    try {
        const summary = JSON.parse(String(raw || '{}')) as { added?: number; remaining?: number };
        return { added: countField(summary.added), remaining: countField(summary.remaining) };
    } catch {
        return null;
    }
}

/** Status line for one hub pass. A quiet pass stays silent when nothing is
 * left, so opening the library does not announce "synced 0". */
function catalogueSyncStatus(raw: unknown, done: string, remainingText: string, quietWhenComplete: boolean): { text: string; added: number } {
    const summary = readCatalogueSummary(raw);
    if (!summary) {
        return { text: quietWhenComplete ? '' : done.replace('{added}', '0'), added: 0 };
    }
    const line = done.replace('{added}', String(summary.added));
    const text = summary.remaining > 0
        ? `${line} ${remainingText.replace('{count}', String(summary.remaining))}`
        : (quietWhenComplete ? '' : line);
    return { text, added: summary.added };
}

/** Fill a localized template that carries an `{error}` placeholder. */
function errorText(template: string, err: unknown): string {
    return template.replace('{error}', err instanceof Error ? err.message : String(err || ''));
}

/** Non-negative integer from a sync summary field. Anything else counts as zero. */
function countField(value: unknown): number {
    const n = Number(value);
    if (!Number.isFinite(n) || n <= 0) return 0;
    return Math.floor(n);
}

/** Parse a list payload, but refuse a string that is not JSON so a truncated
 * refresh cannot replace the library already on screen with an empty one. */
function libraryFromPayload(raw: unknown): LatexTemplateLibrary {
    if (typeof raw === 'string') {
        try {
            JSON.parse(raw);
        } catch {
            throw new Error('invalid library');
        }
    }
    return parseLatexTemplateLibrary(raw);
}

/** Longer phrase for the short source chip, so "中心" / "本地" still explains itself. */
function sourceHint(lang: string, source: string | undefined): string {
    switch (String(source || '').trim()) {
        case 'hub':
            return latexTemplateText(lang, 'From the template hub', '来自模板中心', '來自模板中心');
        case 'local':
            return latexTemplateText(lang, 'Imported on this computer', '本机导入', '本機匯入');
        default:
            return '';
    }
}

/** Read one binding off the dynamic Wails module, failing loudly when the host
 * does not have it, so a missing binding reads as one clear message instead of
 * an undefined call. */
async function requireBinding<T>(name: string): Promise<T> {
    const app = (await getWailsAppModule()) as unknown as Record<string, unknown>;
    const binding = app?.[name];
    if (typeof binding !== 'function') {
        throw new Error(`${name} unavailable`);
    }
    return binding as T;
}

export function LatexTemplateLibraryPage({ lang, onUseTemplate, onClose }: LatexTemplateLibraryPageProps) {
    const [library, setLibrary] = useState<LatexTemplateLibrary>(EMPTY_LIBRARY);
    const [state, setState] = useState<LoadState>('loading');
    const [error, setError] = useState('');
    const [notice, setNotice] = useState('');
    const [busyId, setBusyId] = useState('');
    const [syncing, setSyncing] = useState(false);
    const [refreshing, setRefreshing] = useState(false);
    const [pageByCategory, setPageByCategory] = useState<Record<string, number>>({});
    const hasLibraryRef = useRef(false);
    const loadSeq = useRef(0);
    const openedOnce = useRef(false);
    const catalogueFlight = useRef<Promise<string> | null>(null);

    const text = useMemo(() => ({
        title: latexTemplateText(lang, 'LaTeX Templates', 'LaTeX 模板', 'LaTeX 模板'),
        subtitle: latexTemplateText(
            lang,
            'Pick a template to start a paper, or import, sync, and share.',
            '选择模板开始写论文，也可以导入、同步或分享。',
            '選擇模板開始寫論文，也可以匯入、同步或分享。',
        ),
        refresh: latexTemplateText(lang, 'Refresh', '刷新', '刷新'),
        close: latexTemplateText(lang, 'Close', '关闭', '關閉'),
        importTemplate: latexTemplateText(lang, 'Import', '导入', '匯入'),
        importTitle: latexTemplateText(lang, 'Import a local template pack', '导入本地模板包', '匯入本機模板包'),
        syncHub: latexTemplateText(lang, 'Sync', '同步', '同步'),
        syncHubTitle: latexTemplateText(lang, 'Sync from the template hub', '从模板中心同步', '從模板中心同步'),
        loading: latexTemplateText(lang, 'Loading templates…', '正在加载模板…', '正在載入模板…'),
        emptyLibrary: latexTemplateText(
            lang,
            'No templates yet. Import one, or sync from the template hub.',
            '还没有模板。可以导入，或从模板中心同步。',
            '還沒有模板。可以匯入，或從模板中心同步。',
        ),
        loadFailed: latexTemplateText(lang, 'Could not load templates: {error}', '加载模板失败：{error}', '載入模板失敗：{error}'),
        actionFailed: latexTemplateText(lang, 'Action failed: {error}', '操作失败：{error}', '操作失敗：{error}'),
        use: latexTemplateText(lang, 'Use', '使用', '使用'),
        useTitle: latexTemplateText(lang, 'Start a paper from this template', '用该模板开始写论文', '用該模板開始寫論文'),
        useBlank: latexTemplateText(lang, 'Start from blank', '从空白开始', '從空白開始'),
        files: latexTemplateText(lang, '{count} files', '{count} 文件', '{count} 檔案'),
        authorTitle: latexTemplateText(lang, 'Author: {name}', '作者：{name}', '作者：{name}'),
        countTitle: latexTemplateText(lang, '{count} templates', '{count} 个模板', '{count} 個模板'),
        share: latexTemplateText(lang, 'Share', '分享', '分享'),
        moderationNote: latexTemplateText(lang, 'Note: {note}', '备注：{note}', '備註：{note}'),
        shared: latexTemplateText(
            lang,
            'Shared for review. An administrator will publish it after approval.',
            '已提交分享，等待管理员审核通过后会展示给其它用户。',
            '已提交分享，等待管理員審核通過後會展示給其它使用者。',
        ),
        remove: latexTemplateText(lang, 'Remove', '删除', '刪除'),
        removeConfirm: latexTemplateText(
            lang,
            'Remove this template from the local library?',
            '确定从本地模板库中删除该模板？',
            '確定從本機模板庫中刪除該模板？',
        ),
        syncDone: latexTemplateText(
            lang,
            'Synced {added} new templates.',
            '已同步 {added} 个新模板。',
            '已同步 {added} 個新模板。',
        ),
        syncRemaining: latexTemplateText(
            lang,
            '{count} more are waiting — sync again to continue.',
            '还有 {count} 个模板待同步，可再次点击继续。',
            '還有 {count} 個模板待同步，可再次點擊繼續。',
        ),
        prevPage: latexTemplateText(lang, 'Previous', '上一页', '上一頁'),
        nextPage: latexTemplateText(lang, 'Next', '下一页', '下一頁'),
        pageStatus: latexTemplateText(lang, '{page} / {pages}', '{page} / {pages}', '{page} / {pages}'),
        pager: latexTemplateText(lang, 'Template pages', '模板分页', '模板分頁'),
    }), [lang]);

    const syncCatalogue = useCallback(() => {
        const current = catalogueFlight.current;
        if (current) return current;
        const flight = (async () => {
            const syncBinding = await requireBinding<() => Promise<string>>('SyncLatexTemplatesFromHub');
            return syncBinding();
        })();
        catalogueFlight.current = flight;
        const clear = () => {
            if (catalogueFlight.current === flight) catalogueFlight.current = null;
        };
        void flight.then(clear, clear);
        return flight;
    }, []);

    const load = useCallback(async (options?: { syncCatalogue?: boolean }) => {
        const seq = ++loadSeq.current;
        setRefreshing(true);
        // A library already on screen stays put. Flipping back to "loading"
        // would lock the primary action for the whole moderation round-trip.
        if (!hasLibraryRef.current) setState('loading');
        setError('');
        try {
            const listBinding = await requireBinding<() => Promise<string>>('ListLatexTemplates');
            // Refresh moderation state first, so a submission approved since the
            // last visit stops showing a stale "pending review" badge. This is
            // best-effort: a machine that is offline should still show its library.
            try {
                const shareBinding = await requireBinding<() => Promise<void>>('SyncLatexTemplateShares');
                await shareBinding();
            } catch {
                // Offline or an older host without the binding.
            }
            // List the installed cache first. The opening visit then downloads
            // newly approved packs without holding the screen on that request.
            // "刷新" re-reads the cache; "同步" is the explicit download.
            const next = libraryFromPayload(await listBinding());
            if (seq !== loadSeq.current) return;
            setLibrary(next);
            hasLibraryRef.current = true;
            setState('ready');
            // Release the refresh lock before the hub download. A slow catalogue
            // must not hide templates that are already installed.
            if (seq === loadSeq.current) setRefreshing(false);
            if (!options?.syncCatalogue || seq !== loadSeq.current) return;
            let raw = '';
            try {
                raw = await syncCatalogue();
            } catch {
                return;
            }
            if (seq !== loadSeq.current) return;
            const status = catalogueSyncStatus(raw, text.syncDone, text.syncRemaining, true);
            if (status.text) setNotice(status.text);
            if (status.added <= 0) return;
            setRefreshing(true);
            const refreshed = libraryFromPayload(await listBinding());
            if (seq !== loadSeq.current) return;
            setLibrary(refreshed);
        } catch (err) {
            if (seq !== loadSeq.current) return;
            setError(errorText(text.loadFailed, err));
            setState(hasLibraryRef.current ? 'ready' : 'error');
        } finally {
            if (seq === loadSeq.current) setRefreshing(false);
        }
    }, [syncCatalogue, text.loadFailed, text.syncDone, text.syncRemaining]);

    useEffect(() => {
        const syncCatalogueOnOpen = !openedOnce.current;
        openedOnce.current = true;
        void load(syncCatalogueOnOpen ? { syncCatalogue: true } : undefined);
    }, [load]);

    const run = useCallback(async (key: string, work: () => Promise<void>) => {
        setBusyId(key);
        setError('');
        setNotice('');
        try {
            await work();
        } catch (err) {
            setError(errorText(text.actionFailed, err));
        } finally {
            setBusyId('');
        }
    }, [text.actionFailed]);

    const importTemplate = useCallback(() => {
        void run('import', async () => {
            const importBinding = await requireBinding<() => Promise<string>>('ImportLatexTemplate');
            await importBinding();
            await load();
        });
    }, [load, run]);

    const syncFromHub = useCallback(() => {
        setSyncing(true);
        void run('sync', async () => {
            // Share the opening download when it is still running, so one click
            // cannot start a second copy of the same catalogue pass.
            const raw = await syncCatalogue();
            // One pass is bounded, so say plainly when more remain rather than
            // implying the whole catalogue was fetched. Listing afterwards must
            // not start a second download.
            setNotice(catalogueSyncStatus(raw, text.syncDone, text.syncRemaining, false).text);
            await load();
        }).finally(() => setSyncing(false));
    }, [load, run, syncCatalogue, text.syncDone, text.syncRemaining]);

    const share = useCallback((template: LatexTemplate) => {
        void run(`share:${template.id}`, async () => {
            const shareBinding = await requireBinding<(id: string) => Promise<string>>('ShareLatexTemplate');
            await shareBinding(template.id);
            setNotice(text.shared);
            await load();
        });
    }, [load, run, text.shared]);

    const remove = useCallback((template: LatexTemplate) => {
        if (typeof window !== 'undefined' && !window.confirm(text.removeConfirm)) return;
        void run(`remove:${template.id}`, async () => {
            const deleteBinding = await requireBinding<(id: string) => Promise<void>>('DeleteLatexTemplate');
            await deleteBinding(template.id);
            await load();
        });
    }, [load, run, text.removeConfirm]);

    const sections = useMemo(() => groupLatexTemplatesByCategory(library, lang), [library, lang]);
    const blank = useMemo(
        () => library.templates.find((template) => template.id === LATEX_BLANK_TEMPLATE_ID) || null,
        [library.templates],
    );
    const visibleSections = useMemo(
        () => sections.map((section) => ({
            ...section,
            templates: section.templates.filter((template) => template.id !== LATEX_BLANK_TEMPLATE_ID),
        })),
        [sections],
    );
    const total = library.templates.length;
    const showLoading = state === 'loading' && !blank && visibleSections.every((section) => section.templates.length === 0);
    const showEmpty = state === 'ready' && !blank && visibleSections.every((section) => section.templates.length === 0);
    const mutationsLocked = state === 'loading' || refreshing || !!busyId || syncing;

    const showPage = useCallback((categoryId: string, page: number) => {
        setPageByCategory((prev) => {
            const next = Math.max(1, page);
            if ((prev[categoryId] || 1) === next) return prev;
            return { ...prev, [categoryId]: next };
        });
        queueMicrotask(() => {
            const escaped = typeof CSS !== 'undefined' && typeof CSS.escape === 'function'
                ? CSS.escape(categoryId)
                : categoryId.replace(/"/g, '');
            const section = document.querySelector(`[data-testid="latex-template-section-${escaped}"]`);
            if (section && typeof section.scrollIntoView === 'function') {
                section.scrollIntoView({ block: 'nearest' });
            }
        });
    }, []);

    return (
        <div className="latex-template-page elegant-scrollbar" data-testid="latex-template-page">
            <div className="latex-template-page__header">
                <div className="latex-template-page__title-row">
                    <h1 className="latex-template-page__title">{text.title}</h1>
                    <span className="latex-template-page__count" data-testid="latex-template-count" title={text.countTitle.replace('{count}', String(total))}>{String(total)}</span>
                    <div className="latex-template-page__actions">
                        <button type="button" className="latex-template-page__btn" onClick={() => { setNotice(''); void load(); }} disabled={mutationsLocked}>
                            {text.refresh}
                        </button>
                        <button type="button" className="latex-template-page__btn" data-testid="latex-template-import" title={text.importTitle} onClick={importTemplate} disabled={mutationsLocked}>
                            {text.importTemplate}
                        </button>
                        <button type="button" className="latex-template-page__btn" data-testid="latex-template-sync" title={text.syncHubTitle} onClick={syncFromHub} disabled={mutationsLocked}>
                            {text.syncHub}
                        </button>
                        {onClose ? (
                            <button type="button" className="latex-template-page__close" onClick={onClose} aria-label={text.close} title={text.close}>
                                ×
                            </button>
                        ) : null}
                    </div>
                </div>
                <p className="latex-template-page__subtitle">{text.subtitle}</p>
                {error || notice ? (
                    <div className="latex-template-page__status" role="status" aria-live="polite">
                        {error ? <span className="latex-template-page__error">{error}</span> : <span className="latex-template-page__notice">{notice}</span>}
                    </div>
                ) : null}
            </div>

            {showLoading ? (
                <div className="latex-template-page__state">{text.loading}</div>
            ) : null}
            {showEmpty ? (
                <div className="latex-template-page__state">{text.emptyLibrary}</div>
            ) : null}

            {blank ? (
                <article className="latex-template-card latex-template-card--blank" data-testid={`latex-template-card-${blank.id}`}>
                        <div className="latex-template-card__main">
                            <div className="latex-template-card__title" title={blank.name}>{blank.name}</div>
                            {blank.description ? (
                                <div className="latex-template-card__desc" title={blank.description}>{blank.description}</div>
                            ) : null}
                        </div>
                        <div className="latex-template-card__actions">
                            <button
                                type="button"
                                className="latex-template-page__btn latex-template-page__btn--primary"
                                data-testid={`latex-template-use-${blank.id}`}
                                title={text.useBlank}
                                onClick={() => void onUseTemplate(blank)}
                                disabled={!!busyId}
                            >
                                {text.useBlank}
                            </button>
                        </div>
                    </article>
            ) : null}

            {visibleSections.map((section) => {
                if (!section.templates.length) return null;
                const windowed = pageWindow(section.templates, pageByCategory[section.categoryId] || 1, TEMPLATES_PER_PAGE);
                return (
                <section
                    key={section.categoryId}
                    className="latex-template-section"
                    data-testid={`latex-template-section-${section.categoryId}`}
                >
                    <div className="latex-template-section__head">
                        <h2 className="latex-template-section__title">{section.name}</h2>
                        <span className="latex-template-section__count">{String(section.templates.length)}</span>
                        {section.description ? <p className="latex-template-section__desc">{section.description}</p> : null}
                    </div>
                    <div className="latex-template-grid">
                            {windowed.items.map((template) => {
                                const canShare = String(template.source || '').trim() !== 'hub';
                                const source = latexTemplateSourceLabel(lang, template.source);
                                const shareStatus = latexShareStatusLabel(lang, template.share_status);
                                const shareState = String(template.share_status || '').trim().toLowerCase();
                                const author = String(template.author || '').trim();
                                const version = String(template.version || '').trim();
                                const hasMeta = !!(source || shareStatus || author || version || template.file_count);
                                return (
                                    <article key={template.id} className="latex-template-card" data-testid={`latex-template-card-${template.id}`}>
                                        <div className="latex-template-card__main">
                                            <div className="latex-template-card__title" title={template.name}>{template.name}</div>
                                            {hasMeta ? (
                                                <div className="latex-template-card__meta">
                                                    {source ? <span className="latex-template-card__tag" title={sourceHint(lang, template.source)}>{source}</span> : null}
                                                    {shareStatus ? (
                                                        <span className="latex-template-card__tag latex-template-card__tag--share" data-share-status={shareState} data-testid={`latex-template-share-${template.id}`}>
                                                            {shareStatus}
                                                        </span>
                                                    ) : null}
                                                    {author ? <span title={text.authorTitle.replace('{name}', author)} aria-label={text.authorTitle.replace('{name}', author)}>{author}</span> : null}
                                                    {version ? <span>v{version.replace(/^v/i, '')}</span> : null}
                                                    {template.file_count ? <span>{text.files.replace('{count}', String(template.file_count))}</span> : null}
                                                </div>
                                            ) : null}
                                            {template.description ? (
                                                <div className="latex-template-card__desc" title={template.description}>{template.description}</div>
                                            ) : null}
                                            {template.share_note ? (
                                                <div className="latex-template-card__note" title={template.share_note}>{text.moderationNote.replace('{note}', template.share_note)}</div>
                                            ) : null}
                                        </div>
                                        <div className="latex-template-card__actions">
                                            <button
                                                type="button"
                                                className="latex-template-page__btn latex-template-page__btn--primary"
                                                data-testid={`latex-template-use-${template.id}`}
                                                title={text.useTitle}
                                                onClick={() => void onUseTemplate(template)}
                                                disabled={!!busyId}
                                            >
                                                {text.use}
                                            </button>
                                            {canShare ? (
                                                <button
                                                    type="button"
                                                    className="latex-template-page__btn"
                                                    data-testid={`latex-template-share-button-${template.id}`}
                                                    onClick={() => share(template)}
                                                    disabled={mutationsLocked}
                                                    aria-busy={busyId === `share:${template.id}`}
                                                >
                                                    {text.share}
                                                </button>
                                            ) : null}
                                            <button
                                                type="button"
                                                className="latex-template-page__btn latex-template-page__btn--danger"
                                                data-testid={`latex-template-remove-${template.id}`}
                                                onClick={() => remove(template)}
                                                disabled={mutationsLocked}
                                            >
                                                {text.remove}
                                            </button>
                                        </div>
                                    </article>
                                );
                            })}
                    </div>
                    {windowed.pages > 1 ? (
                        <nav className="latex-template-pager" data-testid={`latex-template-pager-${section.categoryId}`} aria-label={text.pager}>
                            <button
                                type="button"
                                className="latex-template-page__btn"
                                data-testid={`latex-template-page-prev-${section.categoryId}`}
                                onClick={() => showPage(section.categoryId, windowed.page - 1)}
                                disabled={windowed.page <= 1}
                            >
                                {text.prevPage}
                            </button>
                            <span className="latex-template-pager__status" aria-live="polite">{text.pageStatus.replace('{page}', String(windowed.page)).replace('{pages}', String(windowed.pages))}</span>
                            <button
                                type="button"
                                className="latex-template-page__btn"
                                data-testid={`latex-template-page-next-${section.categoryId}`}
                                onClick={() => showPage(section.categoryId, windowed.page + 1)}
                                disabled={windowed.page >= windowed.pages}
                            >
                                {text.nextPage}
                            </button>
                        </nav>
                    ) : null}
                </section>
                );
            })}
        </div>
    );
}
