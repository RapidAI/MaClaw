import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import {
    GetExpertMarketAccount,
    InstallExpertMarketListing,
    ListExpertMarketListings,
    DeletePrivateExpertMarketListing,
    MakeExpertMarketListingPrivate,
    PurchaseExpertMarketListing,
    PublishExpertMarketListing,
    UninstallExpertMarketListing,
    WithdrawExpertMarketListing,
} from '../../wailsjs/go/main/App';
import { useDialog } from './CustomDialog';
import { expertDefinitionFromInstallResult, type ExpertDefinition } from './ai/expertTypes';
import { EVENT_EXPERT_INSTALLED } from '../constants/events';
import { openSettingsTab } from '../utils/settingsNavigation';
import { tailText } from '../i18n/mnTail'

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;
type Listing = Record<string, any>;

const LIBRARY_ROWS_PER_PAGE = 3;

function libraryColumnCount() {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return 4;
    try {
        if (window.matchMedia('(max-width: 560px)').matches) return 1;
        if (window.matchMedia('(max-width: 760px)').matches) return 2;
    } catch {
        return 4;
    }
    return 4;
}

const t = (lang: Lang, zh: string, en: string) => tailText(lang, en, zh);
const contactLabel = (value: unknown) => {
    const text = String(value || '').trim();
    const body = text.toLowerCase().startsWith('phone:') ? text.slice('phone:'.length) : text;
    if (!/^\d+$/.test(body)) return text;
    if (body.startsWith('0086') && /^1[3-9]\d{9}$/.test(body.slice(4))) return body.slice(4);
    if (body.startsWith('86') && /^1[3-9]\d{9}$/.test(body.slice(2))) return body.slice(2);
    return body;
};
const number = (value: unknown) => Number(value || 0).toLocaleString();

function ExpertMarketPager({ lang, label, page, pageCount, busy = false, onPage }: {
    lang: Lang;
    label: string;
    page: number;
    pageCount: number;
    busy?: boolean;
    onPage: (page: number) => void;
}) {
    if (pageCount <= 1) return null;
    return <nav className="expert-market-pagination" aria-label={label}>
        <button type="button" disabled={busy || page <= 1} onClick={() => onPage(page - 1)}>{t(lang, '上一页', 'Previous')}</button>
        <span>{t(lang, `第 ${page} / ${pageCount} 页`, `Page ${page} of ${pageCount}`)}</span>
        <button type="button" disabled={busy || page >= pageCount} onClick={() => onPage(page + 1)}>{t(lang, '下一页', 'Next')}</button>
    </nav>;
}

function revealInMarketBody(target: HTMLElement | null) {
    const scroller = target?.closest('.expert-market-body');
    if (!target || !(scroller instanceof HTMLElement) || typeof scroller.scrollTo !== 'function') return;
    const top = target.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop;
    scroller.scrollTo({ top: Math.max(0, top - 8) });
}
const message = (err: unknown) => err instanceof Error ? err.message : String(err || 'Unknown error');
const marketErrorText = (lang: Lang, err: unknown) => {
    const raw = message(err);
    const normalized = raw.toLowerCase();
    if (normalized.includes('already bound to another user')) {
        return t(lang, '该邮箱已绑定其他市场账号。请用原 HubCenter 账号登录，或联系管理员合并账号。', 'This email is already bound to another market account. Sign in with the original HubCenter account, or ask an administrator to merge them.');
    }
    if (normalized.includes('session refresh failed') || normalized.includes('session expired or invalid')) {
        return t(lang, '登录已过期或 HubCenter 会话刷新失败，请重新连接 HubCenter 后重试。', 'The HubCenter session expired or could not be refreshed. Reconnect this device and try again.');
    }
    if (normalized.includes('未配置 hubcenter')) {
        return t(lang, '未配置 HubCenter，专家市场不可用。请先在设置中连接 HubCenter，然后重试。', 'HubCenter is not configured, so Expert Market is unavailable. Connect to HubCenter in Settings, then retry.');
    }
    return raw;
};

/**
 * Consumer surface for the AI Expert Market. It deliberately mirrors the Pet
 * Store's dialog model: HubCenter remains a moderation system, while browsing,
 * purchase, install and sharing stay in the desktop app.
 */
export function ExpertMarketDialog({ lang, initialTab = 'market', onClose, onInstalled, onUninstalled, onMarketChanged }: {
    lang: Lang;
    initialTab?: 'market' | 'library';
    onClose: () => void;
    onInstalled?: (expert: ExpertDefinition) => void;
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
    // A deleted private share must stay gone even if an already-started account
    // request completes with an older copy of the submission list.
    const [deletedSubmissionIDs, setDeletedSubmissionIDs] = useState<Set<string>>(() => new Set());
    const [loading, setLoading] = useState(true);
    const [accountLoading, setAccountLoading] = useState(true);
    const [accountError, setAccountError] = useState('');
    const [busyID, setBusyID] = useState('');
    const [confirmingID, setConfirmingID] = useState('');
    const [error, setError] = useState('');
    const [tab, setTab] = useState<'market' | 'library'>(initialTab);
    const [columns, setColumns] = useState(libraryColumnCount);
    const [cataloguePage, setCataloguePage] = useState(1);
    const [cataloguePageCount, setCataloguePageCount] = useState(1);
    const [catalogueFetch, setCatalogueFetch] = useState(0);
    const cataloguePageRef = useRef(1);
    const catalogueShownRef = useRef(1);
    catalogueShownRef.current = cataloguePage;
    const [receivedPage, setReceivedPage] = useState(1);
    const [purchasesPage, setPurchasesPage] = useState(1);
    const [uploadsPage, setUploadsPage] = useState(1);
    const pageSize = columns * LIBRARY_ROWS_PER_PAGE;
    const dialogRef = useRef<HTMLElement | null>(null);
    const marketTabRef = useRef<HTMLButtonElement | null>(null);
    const libraryTabRef = useRef<HTMLButtonElement | null>(null);
    const closeRef = useRef(onClose);
    const catalogueRequestRef = useRef(0);
    const catalogueBrowseRef = useRef('');
    const marketBodyRef = useRef<HTMLElement | null>(null);
    const receivedHeadingRef = useRef<HTMLDivElement | null>(null);
    const purchasesHeadingRef = useRef<HTMLDivElement | null>(null);
    const uploadsHeadingRef = useRef<HTMLDivElement | null>(null);
    const accountRequestRef = useRef(0);
    const submissionOverridesRef = useRef<Record<string, string>>({});
    const confirmingRef = useRef('');
    const mountedRef = useRef(true);
    const restoreFocusOnCloseRef = useRef(true);
    closeRef.current = onClose;

    const purchases = useMemo(() => Array.isArray(account?.purchases) ? account.purchases as Listing[] : [], [account]);
    const received = useMemo(() => Array.isArray(account?.received) ? account.received as Listing[] : [], [account]);
    const uploads = useMemo(() => (Array.isArray(account?.uploads) ? account.uploads as Listing[] : []).filter(entry => !deletedSubmissionIDs.has(String(entry.id || ''))).map(entry => {
        const status = submissionOverrides[String(entry.id || '')];
        return status ? { ...entry, status } : entry;
    }), [account, submissionOverrides, deletedSubmissionIDs]);

    const openAssetManagement = () => {
        // The destination settings tab owns focus after navigation.
        restoreFocusOnCloseRef.current = false;
        openSettingsTab('assetManagement');
        onClose();
    };
    const uploadPageCount = Math.max(1, Math.ceil(uploads.length / pageSize));
    const receivedPageCount = Math.max(1, Math.ceil(received.length / pageSize));
    const purchasesPageCount = Math.max(1, Math.ceil(purchases.length / pageSize));
    const currentUploadsPage = Math.min(uploadsPage, uploadPageCount);
    const currentReceivedPage = Math.min(receivedPage, receivedPageCount);
    const currentPurchasesPage = Math.min(purchasesPage, purchasesPageCount);
    const libraryPageSizeRef = useRef(pageSize);
    const libraryPageAnchorsRef = useRef({ catalogue: 0, received: 0, purchases: 0, uploads: 0 });
    const keepPageAnchor = (anchor: number, page: number) => {
        const start = (page - 1) * pageSize;
        return anchor >= start && anchor < start + pageSize ? anchor : start;
    };
    if (libraryPageSizeRef.current !== pageSize) {
        const pageFor = (anchor: number, count: number) => Math.min(Math.max(1, count), Math.max(1, Math.floor(anchor / pageSize) + 1));
        const anchors = libraryPageAnchorsRef.current;
        const next = {
            catalogue: Math.max(1, Math.floor(anchors.catalogue / pageSize) + 1),
            received: pageFor(anchors.received, receivedPageCount),
            purchases: pageFor(anchors.purchases, purchasesPageCount),
            uploads: pageFor(anchors.uploads, uploadPageCount),
        };
        libraryPageSizeRef.current = pageSize;
        libraryPageAnchorsRef.current = {
            catalogue: keepPageAnchor(anchors.catalogue, next.catalogue),
            received: keepPageAnchor(anchors.received, next.received),
            purchases: keepPageAnchor(anchors.purchases, next.purchases),
            uploads: keepPageAnchor(anchors.uploads, next.uploads),
        };
        cataloguePageRef.current = next.catalogue;
        setCatalogueFetch(count => count + 1);
        setReceivedPage(next.received);
        setPurchasesPage(next.purchases);
        setUploadsPage(next.uploads);
    } else {
        const anchors = libraryPageAnchorsRef.current;
        libraryPageAnchorsRef.current = {
            catalogue: keepPageAnchor(anchors.catalogue, cataloguePageRef.current),
            received: keepPageAnchor(anchors.received, currentReceivedPage),
            purchases: keepPageAnchor(anchors.purchases, currentPurchasesPage),
            uploads: keepPageAnchor(anchors.uploads, currentUploadsPage),
        };
    }
    // Account reconciliation can reduce a list while the user is reading a
    // later page. Persist the clamped page so a subsequent refresh cannot
    // unexpectedly return them to a formerly selected page.
    useEffect(() => {
        setUploadsPage(page => Math.min(page, uploadPageCount));
    }, [uploadPageCount]);
    useEffect(() => {
        setReceivedPage(page => Math.min(page, receivedPageCount));
    }, [receivedPageCount]);
    useEffect(() => {
        setPurchasesPage(page => Math.min(page, purchasesPageCount));
    }, [purchasesPageCount]);
    useEffect(() => {
        if (typeof window.matchMedia !== 'function') return;
        let narrow: MediaQueryList;
        let medium: MediaQueryList;
        try {
            narrow = window.matchMedia('(max-width: 560px)');
            medium = window.matchMedia('(max-width: 760px)');
        } catch {
            return;
        }
        const update = () => {
            const next = libraryColumnCount();
            setColumns(current => current === next ? current : next);
        };
        update();
        narrow.addEventListener?.('change', update);
        medium.addEventListener?.('change', update);
        return () => {
            narrow.removeEventListener?.('change', update);
            medium.removeEventListener?.('change', update);
        };
    }, []);
    const visibleUploads = useMemo(
        () => uploads.slice((currentUploadsPage - 1) * pageSize, currentUploadsPage * pageSize),
        [uploads, currentUploadsPage, pageSize],
    );
    const visibleReceived = useMemo(
        () => received.slice((currentReceivedPage - 1) * pageSize, currentReceivedPage * pageSize),
        [received, currentReceivedPage, pageSize],
    );
    const visiblePurchases = useMemo(
        () => purchases.slice((currentPurchasesPage - 1) * pageSize, currentPurchasesPage * pageSize),
        [purchases, currentPurchasesPage, pageSize],
    );
    // The catalogue includes an entitlement flag so a user can immediately
    // install a previously acquired listing even when their account refresh is
    // still catching up after a network retry.
    const owned = useMemo(() => new Set([
        ...purchases.map(entry => String(entry.id)),
        ...listings.filter(entry => entry.owned === true).map(entry => String(entry.id)),
    ]), [purchases, listings]);
    const installed = useMemo(() => {
        const current = new Map<string, string>();
        [...purchases, ...received, ...uploads, ...listings].forEach(entry => {
            const listingID = String(entry.id || '');
            const localExpertID = String(entry.local_expert_id || '').trim();
            if (entry.installed === true && listingID && localExpertID) current.set(listingID, localExpertID);
        });
        Object.entries(installOverrides).forEach(([listingID, localExpertID]) => {
            if (localExpertID) current.set(listingID, localExpertID);
            else current.delete(listingID);
        });
        return current;
    }, [purchases, received, uploads, listings, installOverrides]);

    const setListingInstallState = (listingID: string, localExpertID = '') => {
        const installedNow = Boolean(localExpertID);
        const update = (entry: Listing) => String(entry.id) === listingID
            ? { ...entry, installed: installedNow, local_expert_id: localExpertID }
            : entry;
        setListings(current => current.map(update));
        setAccount(current => current ? {
            ...current,
            purchases: Array.isArray(current.purchases) ? current.purchases.map(update) : current.purchases,
            received: Array.isArray(current.received) ? current.received.map(update) : current.received,
            uploads: Array.isArray(current.uploads) ? current.uploads.map(update) : current.uploads,
        } : current);
        setInstallOverrides(current => ({ ...current, [listingID]: localExpertID }));
    };

    // Browsing and account data have different refresh rates. Keeping them
    // separate avoids an account round-trip for every search or sort change.
    const loadCatalogue = useCallback(async () => {
        const requestID = ++catalogueRequestRef.current;
        const page = cataloguePageRef.current;
        const browse = `${submittedQuery}\0${sort}`;
        if (catalogueBrowseRef.current && catalogueBrowseRef.current !== browse) setListings([]);
        catalogueBrowseRef.current = browse;
        setLoading(true); setError('');
        let snapped = false;
        try {
            const catalogue = await ListExpertMarketListings(submittedQuery, sort, page, pageSize);
            if (!mountedRef.current || requestID !== catalogueRequestRef.current) return;
            const pages = Math.max(1, Number(catalogue?.total_pages) || 1);
            // A narrower window or a shorter result can leave the request on a
            // page that no longer exists. Keep the visible page on the cards
            // already shown until the corrected page arrives.
            if (page > pages) {
                snapped = true;
                cataloguePageRef.current = pages;
                setCatalogueFetch(count => count + 1);
                return;
            }
            setCataloguePageCount(pages);
            setCataloguePage(current => current === page ? current : page);
            setListings(Array.isArray(catalogue?.experts) ? catalogue.experts : []);
        } catch (err) {
            if (!mountedRef.current || requestID !== catalogueRequestRef.current) return;
            cataloguePageRef.current = catalogueShownRef.current;
            setError(marketErrorText(lang, err));
        } finally {
            if (mountedRef.current && requestID === catalogueRequestRef.current && !snapped) setLoading(false);
        }
    }, [catalogueFetch, lang, pageSize, sort, submittedQuery]);

    const goToCataloguePage = (page: number) => {
        cataloguePageRef.current = page;
        setCatalogueFetch(count => count + 1);
    };

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
            setAccountError(marketErrorText(lang, err));
        } finally {
            if (mountedRef.current && requestID === accountRequestRef.current) setAccountLoading(false);
        }
    }, [lang]);

    // React Strict Mode deliberately runs effects through setup → cleanup →
    // setup in development. Re-arm this guard in setup so the second pass can
    // accept its own responses instead of treating the mounted dialog as gone.
    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            catalogueRequestRef.current += 1;
            accountRequestRef.current += 1;
        };
    }, []);
    useEffect(() => { void loadCatalogue(); }, [loadCatalogue]);
    useEffect(() => { void refreshAccount(); }, [refreshAccount]);
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
            const focusable = dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]):not([tabindex="-1"]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href]:not([tabindex="-1"])');
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
            if (restoreFocusOnCloseRef.current && previousFocus?.isConnected) previousFocus.focus();
        };
    }, []);

    const install = async (listing: Listing, purchaseInProgress = false) => {
        const id = String(listing.id || '');
        if (!id || (busyID && !purchaseInProgress)) return;
        if (!purchaseInProgress) setBusyID(id);
        try {
            const result = await InstallExpertMarketListing(id);
            const installedExpert = expertDefinitionFromInstallResult(result?.expert);
            if (!installedExpert) throw new Error(t(lang, '安装未返回本地专家标识，请重试。', 'Installation did not return a local expert ID. Please try again.'));
            setListingInstallState(id, installedExpert.id);
            // The installed-expert list and the new-task picker insert this
            // card now. Waiting for the next ListExperts round trip (it also
            // pulls Hub) left the expert missing until the dialog was closed
            // and the page was reopened.
            window.dispatchEvent(new CustomEvent(EVENT_EXPERT_INSTALLED, { detail: { expert: installedExpert } }));
            onInstalled?.(installedExpert);
            // Do not present a successful native install as failed merely
            // because the follow-up account refresh is temporarily offline.
            void refreshAccount();
            showAlert(t(lang, '专家及其缺失依赖已安装到本机。', 'The expert and any missing dependencies were installed locally.'), t(lang, '专家已安装', 'Expert installed'));
        } catch (err) {
            showAlert(marketErrorText(lang, err), t(lang, '安装失败', 'Install failed'));
        } finally { setBusyID(''); }
    };

    const purchase = async (listing: Listing) => {
        const id = String(listing.id || '');
        if (!id || busyID || confirmingRef.current) return;
        const price = Number(listing.price || 0);
        confirmingRef.current = id;
        setConfirmingID(id);
        let confirmed = false;
        try {
            confirmed = await showConfirm(
                price > 0
                    ? t(lang, `将扣除 ${number(price)} Credits，获取后可永久下载安装该版本。`, `This will deduct ${number(price)} Credits and permanently unlock this version.`)
                    : t(lang, '这是免费专家，获取后可永久下载安装。', 'This expert is free and will be permanently available to install.'),
                t(lang, '获取 AI 专家', 'Get AI Expert'),
                { confirmText: t(lang, '确认获取', 'Get expert'), cancelText: t(lang, '取消', 'Cancel') },
            );
        } finally {
            confirmingRef.current = '';
            setConfirmingID('');
        }
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
            showAlert(marketErrorText(lang, err), t(lang, '获取失败', 'Get failed'));
            setBusyID('');
        }
    };

    const uninstall = async (listing: Listing) => {
        const id = String(listing.id || '');
        const localExpertID = installed.get(id) || '';
        if (!id || !localExpertID || busyID || confirmingRef.current) return;
        confirmingRef.current = id;
        setConfirmingID(id);
        let confirmed = false;
        try {
            confirmed = await showConfirm(
                t(lang, '这会从本机移除该专家；已获得的市场授权和共享依赖会保留，可随时重新安装。', 'This removes the expert from this device. Your market entitlement and shared dependencies are kept, so you can reinstall anytime.'),
                t(lang, '卸载 AI 专家', 'Uninstall AI Expert'),
                { confirmText: t(lang, '卸载', 'Uninstall'), cancelText: t(lang, '取消', 'Cancel'), confirmVariant: 'danger' },
            );
        } finally {
            confirmingRef.current = '';
            setConfirmingID('');
        }
        if (!confirmed) return;
        setBusyID(id);
        try {
            await UninstallExpertMarketListing(localExpertID);
            setListingInstallState(id);
            onUninstalled?.(localExpertID);
            void refreshAccount();
        } catch (err) {
            await showAlert(marketErrorText(lang, err), t(lang, '卸载失败', 'Uninstall failed'));
        } finally { setBusyID(''); }
    };

    const withdraw = async (listing: Listing) => {
        const id = String(listing.id || ''); if (!id || busyID || confirmingRef.current) return;
        confirmingRef.current = id;
        setConfirmingID(id);
        let confirmed = false;
        try {
            confirmed = await showConfirm(t(lang, '下架后不再接受新获取，已购用户仍保留下载授权。', 'New users cannot get this expert after unlisting. Existing buyers retain download access.'), t(lang, '下架专家', 'Unlist expert'), { confirmText: t(lang, '下架', 'Unlist'), cancelText: t(lang, '取消', 'Cancel') });
        } finally {
            confirmingRef.current = '';
            setConfirmingID('');
        }
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
        } catch (err) { showAlert(marketErrorText(lang, err), t(lang, '下架失败', 'Unlist failed')); } finally { setBusyID(''); }
    };

    const deletePrivate = async (listing: Listing) => {
        const id = String(listing.id || ''); if (!id || busyID || confirmingRef.current) return;
        confirmingRef.current = id;
        setConfirmingID(id);
        let confirmed = false;
        const sharedWithUsers = String(listing.status || '') === 'shared' || String(listing.visibility || '') === 'users';
        try {
            confirmed = await showConfirm(
                sharedWithUsers
                    ? t(lang, '删除后无法恢复。指定用户将不能再安装这个专家。', 'This cannot be undone. The selected users will no longer be able to install this expert.')
                    : t(lang, '删除后无法恢复。该私有分享会从你的市场提交中永久移除。', 'This cannot be undone. The private share will be permanently removed from your market submissions.'),
                sharedWithUsers ? t(lang, '删除指定分享', 'Delete share') : t(lang, '删除私有分享', 'Delete private share'),
                { confirmText: t(lang, '永久删除', 'Delete permanently'), cancelText: t(lang, '取消', 'Cancel'), confirmVariant: 'danger' },
            );
        } finally {
            confirmingRef.current = '';
            setConfirmingID('');
        }
        if (!confirmed) return;
        setBusyID(id);
        try {
            await DeletePrivateExpertMarketListing(id);
            setDeletedSubmissionIDs(current => new Set(current).add(id));
            delete submissionOverridesRef.current[id];
            setSubmissionOverrides(current => {
                const { [id]: _deleted, ...remaining } = current;
                return remaining;
            });
            setAccount(current => current ? {
                ...current,
                uploads: Array.isArray(current.uploads) ? current.uploads.filter(entry => String(entry.id || '') !== id) : current.uploads,
            } : current);
            void refreshAccount();
            onMarketChanged?.();
        } catch (err) { await showAlert(marketErrorText(lang, err), sharedWithUsers ? t(lang, '删除指定分享失败', 'Delete share failed') : t(lang, '删除私有分享失败', 'Delete private share failed')); } finally { setBusyID(''); }
    };

    const publish = async (listing: Listing) => {
        const id = String(listing.id || ''); if (!id || busyID || confirmingRef.current) return;
        confirmingRef.current = id;
        setConfirmingID(id);
        let confirmed = false;
        const sharedWithUsers = String(listing.status || '') === 'shared' || String(listing.visibility || '') === 'users';
        try {
            confirmed = await showConfirm(
                sharedWithUsers
                    ? t(lang, '转为公开后，指定用户会立即失去安装权限。专家需要重新审核，通过后才会出现在市场。', 'Selected users lose install access immediately. The expert must be reviewed again before it appears in the market.')
                    : t(lang, '公开后需要重新进入发布审核流程，审核通过后才会在市场展示。', 'Making this expert public submits it for review again. It will appear in the market only after approval.'),
                t(lang, '公开 AI 专家', 'Make AI Expert public'),
                { confirmText: t(lang, '提交审核', 'Submit for review'), cancelText: t(lang, '取消', 'Cancel') },
            );
        } finally {
            confirmingRef.current = '';
            setConfirmingID('');
        }
        if (!confirmed) return;
        setBusyID(id);
        try {
            await PublishExpertMarketListing(id);
            submissionOverridesRef.current[id] = 'pending_review';
            setSubmissionOverrides(current => ({ ...current, [id]: 'pending_review' }));
            setAccount(current => current ? {
                ...current,
                uploads: Array.isArray(current.uploads)
                    ? current.uploads.map(entry => String(entry.id || '') === id ? { ...entry, visibility: 'public', status: 'pending_review', share_targets: sharedWithUsers ? [] : entry.share_targets } : entry)
                    : current.uploads,
            } : current);
            void refreshAccount();
            onMarketChanged?.();
        } catch (err) { showAlert(marketErrorText(lang, err), t(lang, '提交审核失败', 'Review submission failed')); } finally { setBusyID(''); }
    };

    const makePrivate = async (listing: Listing) => {
        const id = String(listing.id || ''); if (!id || busyID || confirmingRef.current) return;
        confirmingRef.current = id;
        setConfirmingID(id);
        let confirmed = false;
        try {
            confirmed = await showConfirm(
                t(lang, '转为私有后会立即停止对其他用户分发，且无需审核；只有你可以看到该专家。', 'Making this private immediately stops distribution to other users. It will be visible only to you and needs no review.'),
                t(lang, '转为私有', 'Make private'),
                { confirmText: t(lang, '转为私有', 'Make private'), cancelText: t(lang, '取消', 'Cancel') },
            );
        } finally {
            confirmingRef.current = '';
            setConfirmingID('');
        }
        if (!confirmed) return;
        setBusyID(id);
        try {
            await MakeExpertMarketListingPrivate(id);
            submissionOverridesRef.current[id] = 'private';
            setSubmissionOverrides(current => ({ ...current, [id]: 'private' }));
            setAccount(current => current ? {
                ...current,
                uploads: Array.isArray(current.uploads)
                    ? current.uploads.map(entry => String(entry.id || '') === id ? { ...entry, visibility: 'private', status: 'private', platform_distribution: false } : entry)
                    : current.uploads,
            } : current);
            void loadCatalogue();
            void refreshAccount();
            onMarketChanged?.();
        } catch (err) { showAlert(marketErrorText(lang, err), t(lang, '转为私有失败', 'Make private failed')); } finally { setBusyID(''); }
    };

    const selectTab = (nextTab: 'market' | 'library') => {
        setTab(nextTab);
    };

    const handleTabKeyDown = (event: ReactKeyboardEvent<HTMLButtonElement>, currentTab: 'market' | 'library') => {
        const nextTab = currentTab === 'market' ? 'library' : 'market';
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight' && event.key !== 'Home' && event.key !== 'End') return;
        event.preventDefault();
        const selectedTab = event.key === 'Home' ? 'market' : event.key === 'End' ? 'library' : nextTab;
        selectTab(selectedTab);
        window.setTimeout(() => (selectedTab === 'market' ? marketTabRef : libraryTabRef).current?.focus(), 0);
    };

    const renderLibraryCard = (listing: Listing, footnote: string) => {
        const id = String(listing.id);
        const isInstalled = installed.has(id);
        return <article className="expert-market-card" key={id}>
            <div className="expert-market-card-head"><div className="expert-market-card-icon" aria-hidden>{listing.icon || '🤖'}</div><div className="expert-market-card-copy"><h3 title={String(listing.name || '')}>{listing.name}</h3><span>v{listing.version}</span></div></div>
            <p>{listing.description || t(lang, '暂无介绍', 'No description')}</p>
            <footer><span className="expert-market-free" title={footnote}>{footnote}</span><button className={isInstalled ? 'expert-market-uninstall' : 'btn-primary'} type="button" disabled={!!busyID || !!confirmingID} onClick={() => void (isInstalled ? uninstall(listing) : install(listing))}>{busyID === id ? t(lang, '处理中…', 'Working…') : isInstalled ? t(lang, '卸载', 'Uninstall') : t(lang, '安装', 'Install')}</button></footer>
        </article>;
    };

    return <div className="expert-market-overlay" role="presentation">
        <section ref={dialogRef} className="expert-market-shell" role="dialog" aria-modal="true" aria-label={t(lang, 'AI 专家市场', 'AI Expert Market')}>
            <header className="expert-market-header"><div><h2>{t(lang, 'AI 专家市场', 'AI Expert Market')}</h2><p>{t(lang, '发现、获取并安装经过审核的 AI 专家。', 'Discover, get, and install reviewed AI experts.')}</p></div><button className="expert-market-close" type="button" aria-label={t(lang, '关闭', 'Close')} onClick={onClose}>×</button></header>
            <div className="expert-market-tabs">
                <div className="expert-market-tablist" role="tablist" aria-label={t(lang, '专家市场导航', 'Expert market navigation')}>
                    <button ref={marketTabRef} id="expert-market-tab-market" role="tab" type="button" tabIndex={tab === 'market' ? 0 : -1} aria-selected={tab === 'market'} aria-controls="expert-market-panel-market" className={tab === 'market' ? 'active' : ''} onClick={() => selectTab('market')} onKeyDown={event => handleTabKeyDown(event, 'market')}>{t(lang, '探索市场', 'Explore')}</button>
                    <button ref={libraryTabRef} id="expert-market-tab-library" role="tab" type="button" tabIndex={tab === 'library' ? 0 : -1} aria-selected={tab === 'library'} aria-controls="expert-market-panel-library" className={tab === 'library' ? 'active' : ''} onClick={() => selectTab('library')} onKeyDown={event => handleTabKeyDown(event, 'library')}>{t(lang, '我的库', 'My library')}</button>
                </div>
                <div className="expert-market-account-actions">
                    <span className="expert-market-balance">{t(lang, '余额', 'Balance')} <strong>{account ? `${number(account.credits)} Credits` : '—'}</strong></span>
                    <button className="expert-market-assets-button" type="button" onClick={openAssetManagement}>{t(lang, '资产管理', 'Asset Management')}</button>
                </div>
            </div>
            {tab === 'market' ? <main ref={marketBodyRef} id="expert-market-panel-market" className="expert-market-body" role="tabpanel" aria-labelledby="expert-market-tab-market">
                <div className="expert-market-toolbar"><input value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') { cataloguePageRef.current = 1; setCataloguePage(1); setSubmittedQuery(query); } }} placeholder={t(lang, '搜索专家或任务', 'Search experts or tasks')} aria-label={t(lang, '搜索专家', 'Search experts')} /><select value={sort} onChange={event => { cataloguePageRef.current = 1; setCataloguePage(1); setSort(event.target.value); }} aria-label={t(lang, '排序', 'Sort')}><option value="published">{t(lang, '最新发布', 'Newest')}</option><option value="downloads">{t(lang, '下载最多', 'Most downloads')}</option><option value="sales">{t(lang, '销售最高', 'Top sales')}</option></select><button type="button" onClick={() => { cataloguePageRef.current = 1; setCataloguePage(1); setSubmittedQuery(query); }}>{t(lang, '搜索', 'Search')}</button></div>
                {error ? <div className="expert-market-message" role="alert">{error}<button type="button" onClick={() => void loadCatalogue()}>{t(lang, '重试', 'Retry')}</button></div> : null}
                {loading && listings.length === 0 ? <div className="expert-market-grid" role="status" aria-label={t(lang, '正在加载专家市场', 'Loading AI Expert Market')}><div className="expert-market-card expert-market-card--skeleton" aria-hidden="true"><div className="expert-market-card-head"><i className="expert-market-skeleton expert-market-skeleton--icon" /><div><i className="expert-market-skeleton expert-market-skeleton--title" /><i className="expert-market-skeleton expert-market-skeleton--version" /></div></div><i className="expert-market-skeleton expert-market-skeleton--description" /><i className="expert-market-skeleton expert-market-skeleton--description expert-market-skeleton--short" /><footer><i className="expert-market-skeleton expert-market-skeleton--price" /><i className="expert-market-skeleton expert-market-skeleton--action" /></footer></div><span className="sr-only">{t(lang, '正在加载专家市场…', 'Loading Expert Market…')}</span></div> : <div className="expert-market-grid" aria-busy={loading || undefined}>{(loading ? listings.slice(0, pageSize) : listings).map(listing => { const id = String(listing.id); const isInstalled = installed.has(id); return <article className="expert-market-card" key={id}><div className="expert-market-card-head"><div className="expert-market-card-icon" aria-hidden>{listing.icon || '🤖'}</div><div className="expert-market-card-copy"><h3 title={String(listing.name || '')}>{listing.name}</h3><span>v{listing.version}</span></div></div><p>{listing.description || t(lang, '暂无介绍', 'No description')}</p><footer><span className={Number(listing.price) > 0 ? 'expert-market-price' : 'expert-market-free'}>{Number(listing.price) > 0 ? `${number(listing.price)} Credits` : t(lang, '免费', 'Free')}</span><button className={isInstalled ? 'expert-market-uninstall' : 'btn-primary'} type="button" disabled={!!busyID || !!confirmingID || loading} onClick={() => void (isInstalled ? uninstall(listing) : owned.has(id) ? install(listing) : purchase(listing))}>{busyID === id ? t(lang, '处理中…', 'Working…') : isInstalled ? t(lang, '卸载', 'Uninstall') : owned.has(id) ? t(lang, '安装', 'Install') : t(lang, '获取', 'Get')}</button></footer></article>; })}</div>}
                {!error && cataloguePageCount > 1 ? <ExpertMarketPager lang={lang} label={t(lang, '专家市场分页', 'Expert market pages')} page={cataloguePage} pageCount={cataloguePageCount} busy={loading} onPage={page => { goToCataloguePage(page); marketBodyRef.current?.scrollTo?.({ top: 0 }); }} /> : null}
                {!loading && !error && listings.length === 0 ? <div className="expert-market-empty">{t(lang, '还没有符合条件的 AI 专家。', 'No AI experts match your search.')}</div> : null}
            </main> : null}
            {tab === 'library' ? <main id="expert-market-panel-library" className="expert-market-body expert-market-library" role="tabpanel" aria-labelledby="expert-market-tab-library">
                {accountLoading && !account ? <div className="expert-market-empty" role="status" aria-label={t(lang, '正在加载我的专家库', 'Loading my expert library')}>{t(lang, '正在加载我的专家库…', 'Loading your expert library…')}</div> : accountError && !account ? <div className="expert-market-message" role="alert"><span>{t(lang, '无法加载我的专家库：', 'Could not load your expert library: ')}{accountError}</span><button type="button" onClick={() => void refreshAccount()}>{t(lang, '重试', 'Retry')}</button></div> : <>
                    {received.length ? <>
                        <div className="expert-market-library-heading" ref={receivedHeadingRef}><h3>{t(lang, '分享给我的专家', 'Shared with me')}</h3><span>{t(lang, `共 ${received.length} 个`, `${received.length} total`)}</span></div>
                        <div className="expert-market-grid">{visibleReceived.map(listing => {
                            const from = contactLabel(listing.owner_email);
                            return renderLibraryCard(listing, from ? t(lang, `来自 ${from}`, `From ${from}`) : t(lang, '指定用户分享', 'Shared with you'));
                        })}</div>
                        <ExpertMarketPager lang={lang} label={t(lang, '分享给我的专家分页', 'Shared with me pages')} page={currentReceivedPage} pageCount={receivedPageCount} onPage={page => { setReceivedPage(page); revealInMarketBody(receivedHeadingRef.current); }} />
                    </> : null}
                    <div className="expert-market-library-heading" ref={purchasesHeadingRef}><h3>{t(lang, '我的已购专家', 'My acquired experts')}</h3>{purchases.length ? <span>{t(lang, `共 ${purchases.length} 个`, `${purchases.length} total`)}</span> : null}</div>
                    {purchases.length ? <>
                        <div className="expert-market-grid">{visiblePurchases.map(listing => renderLibraryCard(listing, t(lang, '永久授权', 'Lifetime entitlement')))}</div>
                        <ExpertMarketPager lang={lang} label={t(lang, '我的已购专家分页', 'My acquired experts pages')} page={currentPurchasesPage} pageCount={purchasesPageCount} onPage={page => { setPurchasesPage(page); revealInMarketBody(purchasesHeadingRef.current); }} />
                    </> : <div className="expert-market-empty">{t(lang, '还没有已购专家。', 'No acquired experts yet.')}</div>}
                    <section className="expert-market-submissions" aria-labelledby="expert-market-submissions-title">
                        <div className="expert-market-library-heading" ref={uploadsHeadingRef}><h3 id="expert-market-submissions-title">{t(lang, '我的提交', 'My submissions')}</h3>{uploads.length ? <span>{t(lang, `共 ${uploads.length} 个`, `${uploads.length} total`)}</span> : null}</div>
                        {uploads.length ? <>
                            <div className="expert-market-submission-grid">{visibleUploads.map(listing => {
                                const id = String(listing.id || '');
                                const isInstalled = installed.has(id);
                                const canInstall = Boolean(id);
                                const status = String(listing.status || '');
                                const shareTargetCount = Array.isArray(listing.share_targets) ? listing.share_targets.length : 0;
                                const shareTargetTitle = Array.isArray(listing.share_targets) ? listing.share_targets.map(contactLabel).filter(Boolean).join('、') : '';
                                return <article className="expert-market-submission-card" key={id}>
                                    <div className="expert-market-submission-card__head"><span aria-hidden>{listing.icon || '🤖'}</span><div><strong title={String(listing.name || '')}>{listing.name}</strong><small title={shareTargetTitle || undefined}>{status === 'private' ? t(lang, '私有 · 仅自己可见', 'Private · only you can see it') : status === 'shared' ? (shareTargetCount ? t(lang, `指定用户 · ${shareTargetCount} 人`, `Shared with ${shareTargetCount} users`) : t(lang, '指定用户', 'Shared with selected users')) : status === 'pending_review' ? t(lang, '待审核', 'Pending review') : status === 'listed' ? t(lang, '已上架', 'Listed') : status === 'unlisted' ? t(lang, '已下架', 'Unlisted') : status}</small></div></div>
                                    <div className="expert-market-submission-card__actions" role="group" aria-label={t(lang, `${listing.name || '专家'}的操作`, `Actions for ${listing.name || 'expert'}`)}>
                                        {canInstall && !isInstalled ? <button className="btn-primary expert-market-install" type="button" disabled={!!busyID || !!confirmingID} onClick={() => void install(listing)}>{busyID === id ? t(lang, '处理中…', 'Working…') : t(lang, '安装', 'Install')}</button> : null}
                                        {status === 'private' || status === 'shared' ? <><button type="button" disabled={!!busyID || !!confirmingID} onClick={() => void publish(listing)}>{t(lang, '转为公开', 'Make public')}</button><button type="button" className="expert-market-uninstall" disabled={!!busyID || !!confirmingID} onClick={() => void deletePrivate(listing)}>{status === 'shared' ? t(lang, '删除指定分享', 'Delete share') : t(lang, '删除私有分享', 'Delete private share')}</button></> : status === 'listed' ? <><button type="button" disabled={!!busyID || !!confirmingID} onClick={() => void withdraw(listing)}>{t(lang, '下架', 'Unlist')}</button><button type="button" disabled={!!busyID || !!confirmingID} onClick={() => void makePrivate(listing)}>{t(lang, '转为私有', 'Make private')}</button></> : ['pending_review', 'approved', 'rejected'].includes(status) ? <button type="button" disabled={!!busyID || !!confirmingID} onClick={() => void makePrivate(listing)}>{t(lang, '转为私有', 'Make private')}</button> : null}
                                    </div>
                                </article>;
                            })}</div>
                            <ExpertMarketPager lang={lang} label={t(lang, '我的提交分页', 'My submissions pages')} page={currentUploadsPage} pageCount={uploadPageCount} onPage={page => { setUploadsPage(page); revealInMarketBody(uploadsHeadingRef.current); }} />
                        </> : <div className="expert-market-empty">{t(lang, '还没有提交的专家。', 'No expert submissions yet.')}</div>}
                    </section>
                </>}
            </main> : null}
        </section>
    </div>;
}
