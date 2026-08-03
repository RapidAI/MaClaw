import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
    GetExpertMarketAccount,
    InstallExpertMarketListing,
    ListExpertMarketListings,
    PurchaseExpertMarketListing,
    UninstallExpertMarketListing,
    WithdrawExpertMarketListing,
} from '../../wailsjs/go/main/App';
import { useDialog } from './CustomDialog';

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;
type Listing = Record<string, any>;

const t = (lang: Lang, zh: string, en: string) => lang.startsWith('zh') ? zh : en;
const number = (value: unknown) => Number(value || 0).toLocaleString();
const message = (err: unknown) => err instanceof Error ? err.message : String(err || 'Unknown error');

/**
 * Consumer surface for the AI Expert Market. It deliberately mirrors the Pet
 * Store's dialog model: HubCenter remains a moderation system, while browsing,
 * purchase, install and sharing stay in the desktop app.
 */
export function ExpertMarketDialog({ lang, initialTab = 'market', onClose, onInstalled, onUninstalled, onMarketChanged }: {
    lang: Lang;
    initialTab?: 'market' | 'library';
    onClose: () => void;
    onInstalled?: () => void;
    /** Remove the local card immediately after its market installation is removed. */
    onUninstalled?: (localExpertID: string) => void;
    /** Notify the local expert cards as soon as a listing is submitted or withdrawn. */
    onMarketChanged?: () => void;
}) {
    const { showAlert, showConfirm } = useDialog();
    const [query, setQuery] = useState('');
    const [submittedQuery, setSubmittedQuery] = useState('');
    const [sort, setSort] = useState('published');
    const [listings, setListings] = useState<Listing[]>([]);
    const [account, setAccount] = useState<Record<string, any> | null>(null);
    // A successful install/uninstall is authoritative for the rest of this
    // dialog session. Keep that local result above any in-flight account or
    // catalogue response, which may have been started before the mutation.
    const [installOverrides, setInstallOverrides] = useState<Record<string, string>>({});
    // Submission mutations follow the same rule: an account response that
    // began before Unlist must not put the destructive action back on screen.
    const [submissionOverrides, setSubmissionOverrides] = useState<Record<string, string>>({});
    const [loading, setLoading] = useState(true);
    const [accountLoading, setAccountLoading] = useState(true);
    const [accountError, setAccountError] = useState('');
    const [busyID, setBusyID] = useState('');
    const [error, setError] = useState('');
    const [tab, setTab] = useState<'market' | 'library'>(initialTab);
    const dialogRef = useRef<HTMLElement | null>(null);
    const closeRef = useRef(onClose);
    const catalogueRequestRef = useRef(0);
    const accountRequestRef = useRef(0);
    const submissionOverridesRef = useRef<Record<string, string>>({});
    const mountedRef = useRef(true);
    closeRef.current = onClose;

    const purchases = useMemo(() => Array.isArray(account?.purchases) ? account.purchases as Listing[] : [], [account]);
    const uploads = useMemo(() => (Array.isArray(account?.uploads) ? account.uploads as Listing[] : []).map(entry => {
        const status = submissionOverrides[String(entry.id || '')];
        return status ? { ...entry, status } : entry;
    }), [account, submissionOverrides]);
    // The catalogue includes an entitlement flag so a user can immediately
    // install a previously acquired listing even when their account refresh is
    // still catching up after a network retry.
    const owned = useMemo(() => new Set([
        ...purchases.map(entry => String(entry.id)),
        ...listings.filter(entry => entry.owned === true).map(entry => String(entry.id)),
    ]), [purchases, listings]);
    const installed = useMemo(() => {
        const current = new Map<string, string>();
        [...purchases, ...uploads, ...listings].forEach(entry => {
            const listingID = String(entry.id || '');
            const localExpertID = String(entry.local_expert_id || '').trim();
            if (entry.installed === true && listingID && localExpertID) current.set(listingID, localExpertID);
        });
        Object.entries(installOverrides).forEach(([listingID, localExpertID]) => {
            if (localExpertID) current.set(listingID, localExpertID);
            else current.delete(listingID);
        });
        return current;
    }, [purchases, uploads, listings, installOverrides]);

    const setListingInstallState = (listingID: string, localExpertID = '') => {
        const installedNow = Boolean(localExpertID);
        const update = (entry: Listing) => String(entry.id) === listingID
            ? { ...entry, installed: installedNow, local_expert_id: localExpertID }
            : entry;
        setListings(current => current.map(update));
        setAccount(current => current ? {
            ...current,
            purchases: Array.isArray(current.purchases) ? current.purchases.map(update) : current.purchases,
            uploads: Array.isArray(current.uploads) ? current.uploads.map(update) : current.uploads,
        } : current);
        setInstallOverrides(current => ({ ...current, [listingID]: localExpertID }));
    };

    // Browsing and account data have different refresh rates. Keeping them
    // separate avoids an account round-trip for every search or sort change.
    const loadCatalogue = useCallback(async () => {
        const requestID = ++catalogueRequestRef.current;
        setLoading(true); setError('');
        try {
            const catalogue = await ListExpertMarketListings(submittedQuery, sort, 1, 30);
            if (!mountedRef.current || requestID !== catalogueRequestRef.current) return;
            setListings(Array.isArray(catalogue?.experts) ? catalogue.experts : []);
        } catch (err) {
            if (!mountedRef.current || requestID !== catalogueRequestRef.current) return;
            setError(message(err));
        } finally {
            if (mountedRef.current && requestID === catalogueRequestRef.current) setLoading(false);
        }
    }, [sort, submittedQuery]);

    const refreshAccount = useCallback(async () => {
        const requestID = ++accountRequestRef.current;
        setAccountLoading(true);
        setAccountError('');
        try {
            const nextAccount = await GetExpertMarketAccount();
            if (mountedRef.current && requestID === accountRequestRef.current) {
                const next = nextAccount || {};
                const overrides = submissionOverridesRef.current;
                for (const entry of Array.isArray(next.uploads) ? next.uploads : []) {
                    const listingID = String(entry?.id || '');
                    if (listingID && overrides[listingID] === String(entry?.status || '')) delete overrides[listingID];
                }
                setAccount(next);
                setSubmissionOverrides({ ...overrides });
            }
        } catch (err) {
            if (!mountedRef.current || requestID !== accountRequestRef.current) return;
            // Do not turn an account request failure into the misleading empty
            // “no submissions” / “no acquired experts” state. The catalogue
            // remains usable, while My library clearly explains how to retry.
            setAccountError(message(err));
        } finally {
            if (mountedRef.current && requestID === accountRequestRef.current) setAccountLoading(false);
        }
    }, []);

    useEffect(() => { void loadCatalogue(); }, [loadCatalogue]);
    useEffect(() => { void refreshAccount(); }, [refreshAccount]);
    useEffect(() => () => { mountedRef.current = false; catalogueRequestRef.current += 1; accountRequestRef.current += 1; }, []);
    useEffect(() => {
        const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const focusTimer = window.setTimeout(() => dialogRef.current?.querySelector<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled])')?.focus(), 0);
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                event.preventDefault();
                event.stopImmediatePropagation();
                closeRef.current();
                return;
            }
            if (event.key !== 'Tab') return;
            const focusable = dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href]');
            if (!focusable?.length) return;
            const items = Array.from(focusable);
            const currentIndex = items.indexOf(document.activeElement as HTMLElement);
            const nextIndex = event.shiftKey
                ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
                : (currentIndex === items.length - 1 ? 0 : currentIndex + 1);
            event.preventDefault();
            event.stopImmediatePropagation();
            items[nextIndex].focus();
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => {
            window.clearTimeout(focusTimer);
            window.removeEventListener('keydown', onKeyDown, true);
            if (previousFocus?.isConnected) previousFocus.focus();
        };
    }, []);

    const install = async (listing: Listing, purchaseInProgress = false) => {
        const id = String(listing.id || '');
        if (!id || (busyID && !purchaseInProgress)) return;
        if (!purchaseInProgress) setBusyID(id);
        try {
            const result = await InstallExpertMarketListing(id);
            const localExpertID = String(result?.expert?.id || '').trim();
            if (localExpertID) setListingInstallState(id, localExpertID);
            onInstalled?.();
            // Do not present a successful native install as failed merely
            // because the follow-up account refresh is temporarily offline.
            void refreshAccount();
            showAlert(t(lang, '专家已安装', 'Expert installed'), t(lang, '专家及其缺失依赖已安装到本机。', 'The expert and any missing dependencies were installed locally.'));
        } catch (err) {
            showAlert(t(lang, '安装失败', 'Install failed'), message(err));
        } finally { setBusyID(''); }
    };

    const purchase = async (listing: Listing) => {
        const id = String(listing.id || '');
        if (!id || busyID) return;
        const price = Number(listing.price || 0);
        const confirmed = await showConfirm(
            price > 0
                ? t(lang, `将扣除 ${number(price)} Credits，获取后可永久下载安装该版本。`, `This will deduct ${number(price)} Credits and permanently unlock this version.`)
                : t(lang, '这是免费专家，获取后可永久下载安装。', 'This expert is free and will be permanently available to install.'),
            t(lang, '获取 AI 专家', 'Get AI Expert'),
            { confirmText: t(lang, '确认获取', 'Get expert'), cancelText: t(lang, '取消', 'Cancel') },
        );
        if (!confirmed) return;
        setBusyID(id);
        try {
            await PurchaseExpertMarketListing(id);
            // Purchase settles an entitlement before the local installer runs.
            // Refresh it now so an installation failure leaves a truthful
            // "Install" retry action instead of inviting a duplicate purchase.
            void refreshAccount();
            await install(listing, true);
        } catch (err) {
            showAlert(t(lang, '获取失败', 'Get failed'), message(err));
            setBusyID('');
        }
    };

    const uninstall = async (listing: Listing) => {
        const id = String(listing.id || '');
        const localExpertID = installed.get(id) || '';
        if (!id || !localExpertID || busyID) return;
        const confirmed = await showConfirm(
            t(lang, '这会从本机移除该专家；已获得的市场授权和共享依赖会保留，可随时重新安装。', 'This removes the expert from this device. Your market entitlement and shared dependencies are kept, so you can reinstall anytime.'),
            t(lang, '卸载 AI 专家', 'Uninstall AI Expert'),
            { confirmText: t(lang, '卸载', 'Uninstall'), cancelText: t(lang, '取消', 'Cancel'), confirmVariant: 'danger' },
        );
        if (!confirmed) return;
        setBusyID(id);
        try {
            await UninstallExpertMarketListing(localExpertID);
            setListingInstallState(id);
            onUninstalled?.(localExpertID);
            void refreshAccount();
        } catch (err) {
            await showAlert(message(err), t(lang, '卸载失败', 'Uninstall failed'));
        } finally { setBusyID(''); }
    };

    const withdraw = async (listing: Listing) => {
        const id = String(listing.id || ''); if (!id || busyID) return;
        const confirmed = await showConfirm(t(lang, '下架后不再接受新获取，已购用户仍保留下载授权。', 'New users cannot get this expert after unlisting. Existing buyers retain download access.'), t(lang, '下架专家', 'Unlist expert'), { confirmText: t(lang, '下架', 'Unlist'), cancelText: t(lang, '取消', 'Cancel') });
        if (!confirmed) return;
        setBusyID(id);
        try {
            await WithdrawExpertMarketListing(id);
            // Apply the successful mutation before reconciliation. This keeps
            // My library truthful even when the next account response is slow
            // or reflects a replica that has not caught up yet.
            submissionOverridesRef.current[id] = 'unlisted';
            setSubmissionOverrides(current => ({ ...current, [id]: 'unlisted' }));
            setAccount(current => current ? {
                ...current,
                uploads: Array.isArray(current.uploads)
                    ? current.uploads.map(entry => String(entry.id || '') === id ? { ...entry, status: 'unlisted' } : entry)
                    : current.uploads,
            } : current);
            void loadCatalogue();
            void refreshAccount();
            onMarketChanged?.();
        } catch (err) { showAlert(t(lang, '下架失败', 'Unlist failed'), message(err)); } finally { setBusyID(''); }
    };

    return <div className="expert-market-overlay" role="presentation">
        <section ref={dialogRef} className="expert-market-shell" role="dialog" aria-modal="true" aria-label={t(lang, 'AI 专家市场', 'AI Expert Market')}>
            <header className="expert-market-header"><div><h2>{t(lang, 'AI 专家市场', 'AI Expert Market')}</h2><p>{t(lang, '发现、获取并安装经过审核的 AI 专家。', 'Discover, get, and install reviewed AI experts.')}</p></div><button className="expert-market-close" type="button" aria-label={t(lang, '关闭', 'Close')} onClick={onClose}>×</button></header>
            <nav className="expert-market-tabs" aria-label={t(lang, '专家市场导航', 'Expert market navigation')}>
                <button type="button" className={tab === 'market' ? 'active' : ''} onClick={() => setTab('market')}>{t(lang, '探索市场', 'Explore')}</button>
                <button type="button" className={tab === 'library' ? 'active' : ''} onClick={() => setTab('library')}>{t(lang, '我的库', 'My library')}</button>
                <span className="expert-market-balance">{t(lang, '余额', 'Balance')} <strong>{accountLoading && !account ? '—' : `${number(account?.credits)} Credits`}</strong></span>
            </nav>
            {tab === 'market' ? <main className="expert-market-body">
                <div className="expert-market-toolbar"><input value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') setSubmittedQuery(query); }} placeholder={t(lang, '搜索专家或任务', 'Search experts or tasks')} aria-label={t(lang, '搜索专家', 'Search experts')} /><select value={sort} onChange={event => setSort(event.target.value)} aria-label={t(lang, '排序', 'Sort')}><option value="published">{t(lang, '最新发布', 'Newest')}</option><option value="downloads">{t(lang, '下载最多', 'Most downloads')}</option><option value="sales">{t(lang, '销售最高', 'Top sales')}</option></select><button type="button" onClick={() => setSubmittedQuery(query)}>{t(lang, '搜索', 'Search')}</button></div>
                {error ? <div className="expert-market-message" role="alert">{error}<button type="button" onClick={() => void loadCatalogue()}>{t(lang, '重试', 'Retry')}</button></div> : null}
                {loading ? <div className="expert-market-grid" role="status" aria-label={t(lang, '正在加载专家市场', 'Loading AI Expert Market')}><div className="expert-market-card expert-market-card--skeleton" aria-hidden="true"><div className="expert-market-card-head"><i className="expert-market-skeleton expert-market-skeleton--icon" /><div><i className="expert-market-skeleton expert-market-skeleton--title" /><i className="expert-market-skeleton expert-market-skeleton--version" /></div></div><i className="expert-market-skeleton expert-market-skeleton--description" /><i className="expert-market-skeleton expert-market-skeleton--description expert-market-skeleton--short" /><footer><i className="expert-market-skeleton expert-market-skeleton--price" /><i className="expert-market-skeleton expert-market-skeleton--action" /></footer></div><span className="sr-only">{t(lang, '正在加载专家市场…', 'Loading Expert Market…')}</span></div> : <div className="expert-market-grid">{listings.map(listing => { const id = String(listing.id); const isInstalled = installed.has(id); return <article className="expert-market-card" key={id}><div className="expert-market-card-head"><div className="expert-market-card-icon" aria-hidden>{listing.icon || '🤖'}</div><div className="expert-market-card-copy"><h3 title={String(listing.name || '')}>{listing.name}</h3><span>v{listing.version}</span></div></div><p>{listing.description || t(lang, '暂无介绍', 'No description')}</p><footer><span className={Number(listing.price) > 0 ? 'expert-market-price' : 'expert-market-free'}>{Number(listing.price) > 0 ? `${number(listing.price)} Credits` : t(lang, '免费', 'Free')}</span><button className={isInstalled ? 'expert-market-uninstall' : 'btn-primary'} type="button" disabled={!!busyID} onClick={() => void (isInstalled ? uninstall(listing) : owned.has(id) ? install(listing) : purchase(listing))}>{busyID === id ? t(lang, '处理中…', 'Working…') : isInstalled ? t(lang, '卸载', 'Uninstall') : owned.has(id) ? t(lang, '安装', 'Install') : t(lang, '获取', 'Get')}</button></footer></article>; })}</div>}
                {!loading && !error && listings.length === 0 ? <div className="expert-market-empty">{t(lang, '还没有符合条件的 AI 专家。', 'No AI experts match your search.')}</div> : null}
            </main> : null}
            {tab === 'library' ? <main className="expert-market-body expert-market-library">
                {accountLoading && !account ? <div className="expert-market-empty" role="status" aria-label={t(lang, '正在加载我的专家库', 'Loading my expert library')}>{t(lang, '正在加载我的专家库…', 'Loading your expert library…')}</div> : accountError && !account ? <div className="expert-market-message" role="alert"><span>{t(lang, '无法加载我的专家库：', 'Could not load your expert library: ')}{accountError}</span><button type="button" onClick={() => void refreshAccount()}>{t(lang, '重试', 'Retry')}</button></div> : <>
                    <h3>{t(lang, '我的已购专家', 'My acquired experts')}</h3>{purchases.length ? purchases.map(listing => { const id = String(listing.id); const isInstalled = installed.has(id); return <div className="expert-market-row" key={id}><span aria-hidden>{listing.icon || '🤖'}</span><div><strong>{listing.name}</strong><small>v{listing.version} · {t(lang, '永久授权', 'Lifetime entitlement')}</small></div><button className={isInstalled ? 'expert-market-uninstall' : 'btn-primary'} disabled={!!busyID} type="button" onClick={() => void (isInstalled ? uninstall(listing) : install(listing))}>{busyID === id ? t(lang, '处理中…', 'Working…') : isInstalled ? t(lang, '卸载', 'Uninstall') : t(lang, '安装', 'Install')}</button></div>; }) : <div className="expert-market-empty">{t(lang, '还没有已购专家。', 'No acquired experts yet.')}</div>}
                    <h3>{t(lang, '我的提交', 'My submissions')}</h3>{uploads.length ? uploads.map(listing => <div className="expert-market-row" key={String(listing.id)}><span aria-hidden>{listing.icon || '🤖'}</span><div><strong>{listing.name}</strong><small>{listing.status === 'pending_review' ? t(lang, '待审核', 'Pending review') : listing.status === 'listed' ? t(lang, '已上架', 'Listed') : listing.status}</small></div>{['pending_review', 'approved', 'listed', 'rejected'].includes(String(listing.status)) ? <button type="button" disabled={!!busyID} onClick={() => void withdraw(listing)}>{t(lang, '下架', 'Unlist')}</button> : null}</div>) : <div className="expert-market-empty">{t(lang, '还没有提交的专家。', 'No expert submissions yet.')}</div>}
                </>}
            </main> : null}
        </section>
    </div>;
}
