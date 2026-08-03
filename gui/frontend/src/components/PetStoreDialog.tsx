import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyEvent } from 'react';
import {
    GetPetStoreAccount,
    GetPetStoreCreatorReport,
    GetPetStoreRankings,
    InstallPetStorePack,
    IsPetStorePackInstalled,
    ListPetStorePacks,
    PurchasePetStorePack,
    WithdrawPetStorePack,
} from '../../wailsjs/go/main/App';
import { useDialog } from './CustomDialog';

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;
type StorePack = Record<string, any>;

const ACCOUNT_TABS = ['uploads', 'purchases'] as const;
type AccountTab = (typeof ACCOUNT_TABS)[number];
const REPORT_PERIODS = ['day', 'month', 'year'] as const;
type ReportPeriod = (typeof REPORT_PERIODS)[number];

const t = (lang: Lang, zh: string, en: string) => lang === 'zh-Hans' || lang === 'zh-Hant' ? zh : en;
const localeFor = (lang: Lang) => lang === 'zh-Hans' ? 'zh-CN' : lang === 'zh-Hant' ? 'zh-TW' : 'en-US';
const errText = (err: unknown) => err instanceof Error ? err.message : String(err || 'Unknown error');
const petStoreErrorText = (lang: Lang, err: unknown) => {
    const message = errText(err);
    const normalized = message.toLowerCase();
    if (normalized.includes('already own this pet pack')) {
        return t(lang, '您已拥有此宠物包，无需重复获取。', 'You already own this pet pack.');
    }
    if (normalized.includes('pet pack has been removed')) {
        return t(lang, '该宠物包已被作者移除，无法安装。', 'This pet pack has been removed by its creator and cannot be installed.');
    }
    return message;
};
// Every Pet Store binding returns this message when no HubCenter is configured
// (see errPetStoreHubCenterMissing in pet_bridge.go). Surface it as a clear
// empty-state explanation instead of a raw request error.
const isHubCenterMissingError = (err: unknown) => errText(err).toLowerCase().includes('未配置 hubcenter');
// The creator report additionally requires a signed-in HubCenter session; Go
// returns this message otherwise. Render a sign-in empty state, not an error.
const isPetStoreSignInError = (err: unknown) => errText(err).toLowerCase().includes('please sign in to hubcenter');
const storeLoadErrorText = (lang: Lang, err: unknown) => isHubCenterMissingError(err)
    ? t(lang, '未配置 HubCenter，宠物市场不可用。请先在设置中连接 HubCenter，然后重试。', 'HubCenter is not configured, so the Pet Store is unavailable. Connect to HubCenter in Settings, then retry.')
    : errText(err);
const number = (value: unknown) => {
    const numericValue = Number(value);
    return Number.isFinite(numericValue) ? numericValue.toLocaleString() : '—';
};
const date = (value: unknown, lang: Lang) => {
    if (!value) return '—';
    const parsed = new Date(String(value));
    if (Number.isNaN(parsed.getTime())) return '—';
    return parsed.toLocaleDateString(localeFor(lang));
};
const isoDate = (value: Date) => value.toISOString().slice(0, 10);
const todayISO = () => isoDate(new Date());
const reportDateLabel = (value: unknown, period: ReportPeriod, lang: Lang) => {
    const fallback = typeof value === 'string' ? value : '—';
    const parsed = new Date(`${fallback.slice(0, 10)}T00:00:00Z`);
    if (Number.isNaN(parsed.getTime())) return fallback;
    if (period === 'year') return String(parsed.getUTCFullYear());
    if (period === 'month') return new Intl.DateTimeFormat(localeFor(lang), { year: 'numeric', month: 'long', timeZone: 'UTC' }).format(parsed);
    return new Intl.DateTimeFormat(localeFor(lang), { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' }).format(parsed);
};
const isActiveAccountPack = (entry: StorePack) => String(entry.status || 'active').toLowerCase() === 'active';
// Buyers and the author can always download active/withdrawn/paused packs;
// only deleted/purged listings return 410 (HubCenter download semantics).
const isAccountPackInstallable = (entry: StorePack) => {
    const status = String(entry.status || 'active').toLowerCase();
    return status !== 'deleted' && status !== 'purged';
};
// Account IDs are implementation details and must never become a customer-facing
// identity. Accept only a real email address or phone-like value returned by
// HubCenter, with email taking precedence when both are present.
const accountContact = (user: StorePack) => {
    const email = String(user.email || '').trim();
    if (/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) return email;
    const phone = String(user.phone_number || user.phone || user.mobile || '').trim();
    return /^[+()\d][\d\s()\-]{6,}$/.test(phone) ? phone : '';
};
const shiftReportDateValue = (dateValue: string, period: ReportPeriod, offset: number) => {
    const parsed = new Date(`${dateValue}T00:00:00Z`);
    if (Number.isNaN(parsed.getTime())) return dateValue;
    if (period === 'day') parsed.setUTCDate(parsed.getUTCDate() + offset);
    else if (period === 'month') parsed.setUTCMonth(parsed.getUTCMonth() + offset);
    else parsed.setUTCFullYear(parsed.getUTCFullYear() + offset);
    return isoDate(parsed);
};

export function PetStoreDialog({ lang, onClose }: { lang: Lang; onClose: () => void }) {
    const { showAlert, showConfirm } = useDialog();
    const [query, setQuery] = useState('');
    const [submittedQuery, setSubmittedQuery] = useState('');
    const [sort, setSort] = useState('published');
    const [order, setOrder] = useState<'asc' | 'desc'>('desc');
    const [page, setPage] = useState(1);
    const [packs, setPacks] = useState<StorePack[]>([]);
    const [totalPages, setTotalPages] = useState(1);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [packsSignInRequired, setPacksSignInRequired] = useState(false);
    const [accountOpen, setAccountOpen] = useState(false);
    const [accountTab, setAccountTab] = useState<AccountTab>('uploads');
    const [account, setAccount] = useState<Record<string, any> | null>(null);
    const [accountLoading, setAccountLoading] = useState(false);
    const [report, setReport] = useState<Record<string, any> | null>(null);
    const [reportPeriod, setReportPeriod] = useState<ReportPeriod>('month');
    const [reportDate, setReportDate] = useState(() => new Date().toISOString().slice(0, 10));
    const [reportLoading, setReportLoading] = useState(true);
    const [reportError, setReportError] = useState('');
    const [reportSignInRequired, setReportSignInRequired] = useState(false);
    const [rankings, setRankings] = useState<Record<string, any> | null>(null);
    const [accountError, setAccountError] = useState('');
    const [accountSignInRequired, setAccountSignInRequired] = useState(false);
    const [busyID, setBusyID] = useState('');
    // Marketplace listings can be withdrawn and later re-issued. Ownership is
    // therefore keyed by the immutable manifest/source ID, not a listing ID.
    const [ownedSourcePackIDs, setOwnedSourcePackIDs] = useState<Set<string>>(() => new Set());
    const [installedPackIDs, setInstalledPackIDs] = useState<Set<string>>(() => new Set());
    const searchRef = useRef<HTMLInputElement>(null);
    const shellRef = useRef<HTMLElement>(null);
    const tablistRef = useRef<HTMLDivElement>(null);
    const accountOpenRef = useRef(false);
    const packsRequestRef = useRef(0);
    const accountRequestRef = useRef(0);
    const rankingsRequestRef = useRef(0);
    const reportRequestRef = useRef(0);
    const installedRequestRef = useRef(0);
    const installedProbeCacheRef = useRef<Map<string, boolean>>(new Map());
    const accountLoadedOnceRef = useRef(false);
    const packBusyKey = (pack: StorePack) => `pack:${String(pack.id)}`;
    const isPackBusy = (pack: StorePack) => busyID === packBusyKey(pack);
    const isPackInstalled = (pack: StorePack) => installedPackIDs.has(String(pack.source_pack_id || ''));
    // Probe native installation state only when the set of source pack IDs
    // changes. Account refreshes otherwise recreate the same data and used to
    // repeat one bridge call per visible pack.
    const sourcePackIDsKey = useMemo(() => Array.from(new Set([
        ...packs,
        ...(Array.isArray(account?.uploads) ? account.uploads.filter(isAccountPackInstallable) : []),
        ...(Array.isArray(account?.purchases) ? account.purchases.map((entry: StorePack) => entry.pack || entry).filter(isAccountPackInstallable) : []),
    ].map((entry) => String((entry.pack || entry).source_pack_id || '')).filter(Boolean))).sort().join('\u0000'), [account, packs]);

    useEffect(() => {
        if (page > totalPages) setPage(totalPages);
    }, [page, totalPages]);

    const loadPacks = useCallback(async () => {
        const requestID = ++packsRequestRef.current;
        setLoading(true); setError(''); setPacksSignInRequired(false);
        try {
            const result = await ListPetStorePacks(submittedQuery, sort, order, page, 20);
            if (requestID !== packsRequestRef.current) return;
            setPacks(Array.isArray(result?.packs) ? result.packs : []);
            const reportedPages = Math.floor(Number(result?.total_pages));
            setTotalPages(Number.isFinite(reportedPages) ? Math.max(1, reportedPages) : 1);
        } catch (err) {
            if (requestID !== packsRequestRef.current) return;
            if (isPetStoreSignInError(err)) setPacksSignInRequired(true);
            else setError(storeLoadErrorText(lang, err));
        } finally {
            if (requestID === packsRequestRef.current) setLoading(false);
        }
    }, [lang, order, page, sort, submittedQuery]);

    const loadAccount = useCallback(async () => {
        const requestID = ++accountRequestRef.current;
        setAccountError('');
        setAccountSignInRequired(false);
        setAccountLoading(true);
        try {
            const nextAccount = await GetPetStoreAccount();
            if (requestID !== accountRequestRef.current) return;
            setAccount(nextAccount);
            const purchases = Array.isArray(nextAccount?.purchases) ? nextAccount.purchases : [];
            setOwnedSourcePackIDs(new Set(purchases.map((entry: StorePack) => String((entry.pack || entry).source_pack_id || '')).filter(Boolean)));

        }
        catch (err) {
            if (requestID !== accountRequestRef.current) return;
            if (isPetStoreSignInError(err)) setAccountSignInRequired(true);
            else setAccountError(storeLoadErrorText(lang, err));
        } finally {
            if (requestID === accountRequestRef.current) setAccountLoading(false);
        }
    }, [lang]);
    const loadRankings = useCallback(async () => {
        const requestID = ++rankingsRequestRef.current;
        try {
            const nextRankings = await GetPetStoreRankings();
            if (requestID === rankingsRequestRef.current) setRankings(nextRankings);
        } catch {
            // Rankings are supplementary; browsing stays available.
        }
    }, []);
    const loadReport = useCallback(async () => {
        const requestID = ++reportRequestRef.current;
        setReportLoading(true); setReportError(''); setReportSignInRequired(false);
        try {
            const nextReport = await GetPetStoreCreatorReport(reportPeriod, reportDate);
            if (requestID === reportRequestRef.current) setReport(nextReport);
        }
        catch (err) {
            if (requestID !== reportRequestRef.current) return;
            if (isPetStoreSignInError(err)) setReportSignInRequired(true);
            else setReportError(storeLoadErrorText(lang, err));
        }
        finally {
            if (requestID === reportRequestRef.current) setReportLoading(false);
        }
    }, [lang, reportDate, reportPeriod]);

    useEffect(() => { void loadPacks(); }, [loadPacks]);
    useEffect(() => { void loadRankings(); }, [loadRankings]);
    useEffect(() => { void loadReport(); }, [loadReport]);
    // Browse actions depend on permanent purchase ownership, not just the
    // visible account drawer. Load it on entry so owned packs immediately
    // offer Install instead of a misleading Get action. Reopening the drawer
    // still refreshes balance and listing state after the first load.
    useEffect(() => {
        if (!accountLoadedOnceRef.current) {
            accountLoadedOnceRef.current = true;
            void loadAccount();
            return;
        }
        if (accountOpen) void loadAccount();
    }, [accountOpen, loadAccount]);
    useEffect(() => {
        accountOpenRef.current = accountOpen;
    }, [accountOpen]);
    // The same market pack can appear in browse results, uploads, purchases,
    // and later pages. Cache each native registry result for this dialog so
    // paging does not repeat bridge calls for an already-checked pack.
    useEffect(() => {
        const requestID = ++installedRequestRef.current;
        const sourcePackIDs = sourcePackIDsKey ? sourcePackIDsKey.split('\u0000') : [];
        if (sourcePackIDs.length === 0) {
            setInstalledPackIDs(new Set());
            return;
        }
        const missingSourcePackIDs = sourcePackIDs.filter((sourcePackID) => !installedProbeCacheRef.current.has(sourcePackID));
        const applyInstalledState = () => setInstalledPackIDs(new Set(
            sourcePackIDs.filter((sourcePackID) => installedProbeCacheRef.current.get(sourcePackID)),
        ));
        if (missingSourcePackIDs.length === 0) {
            applyInstalledState();
            return;
        }
        void Promise.all(missingSourcePackIDs.map(async (sourcePackID) => {
            try { return (await IsPetStorePackInstalled(sourcePackID)) ? sourcePackID : ''; }
            catch { return ''; }
        })).then((results) => {
            if (requestID !== installedRequestRef.current) return;
            missingSourcePackIDs.forEach((sourcePackID) => installedProbeCacheRef.current.set(sourcePackID, results.includes(sourcePackID)));
            applyInstalledState();
        });
    }, [sourcePackIDsKey]);
    // Focus management mirrors CustomDialog: move focus into the dialog on
    // open, cycle Tab inside the shell, restore focus to the invoker on close.
    // This handoff effect is mount-only and separate from the keydown binding:
    // the parent passes an inline onClose, so re-running on identity changes
    // would restore focus to the invoker while the dialog is still open.
    useEffect(() => {
        const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const focusFrame = window.requestAnimationFrame(() => {
            if (!accountOpenRef.current) searchRef.current?.focus();
        });
        return () => {
            window.cancelAnimationFrame(focusFrame);
            if (previousFocus?.isConnected) previousFocus.focus();
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    // CustomDialog listens in capture phase and consumes Escape while a
    // confirmation is open. This bubble-phase listener therefore only closes
    // the store when it owns the active interaction.
    useEffect(() => {
        const keydown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                if (event.defaultPrevented) return;
                event.preventDefault();
                event.stopPropagation();
                onClose();
                return;
            }
            if (event.key !== 'Tab') return;
            const focusable = shellRef.current?.querySelectorAll<HTMLElement>(
                'button:not([disabled]):not([tabindex="-1"]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href]',
            );
            if (!focusable?.length) return;
            const items = Array.from(focusable);
            const currentIndex = items.indexOf(document.activeElement as HTMLElement);
            const nextIndex = event.shiftKey
                ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
                : (currentIndex === items.length - 1 ? 0 : currentIndex + 1);
            event.preventDefault();
            items[nextIndex].focus();
        };
        window.addEventListener('keydown', keydown);
        return () => window.removeEventListener('keydown', keydown);
    }, [onClose]);

    const onTabListKeyDown = (event: ReactKeyEvent<HTMLDivElement>) => {
        const { key } = event;
        if (key !== 'ArrowLeft' && key !== 'ArrowRight' && key !== 'Home' && key !== 'End') return;
        event.preventDefault();
        const current = ACCOUNT_TABS.indexOf(accountTab);
        const next = key === 'Home' ? 0
            : key === 'End' ? ACCOUNT_TABS.length - 1
            : (current + (key === 'ArrowRight' ? 1 : ACCOUNT_TABS.length - 1)) % ACCOUNT_TABS.length;
        setAccountTab(ACCOUNT_TABS[next]);
        tablistRef.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
    };


    const purchase = async (pack: StorePack) => {
        const price = Number(pack.price || 0);
        const ok = await showConfirm(
            price > 0 ? t(lang, `确认使用 ${price} Credits 买断「${pack.name}」？购买后永久可下载。`, `Buy “${pack.name}” permanently for ${price} Credits?`) : t(lang, `确认免费获取「${pack.name}」？`, `Get “${pack.name}” for free?`),
            t(lang, '确认购买', 'Confirm purchase'),
            { confirmText: t(lang, '获取', 'Get'), cancelText: t(lang, '取消', 'Cancel') },
        );
        if (!ok) return;
        setBusyID(packBusyKey(pack));
        try {
            await PurchasePetStorePack(String(pack.id));
            // An account refresh is the authoritative source for every other
            // entitlement; keep the clicked card responsive while it is in flight.
            const sourcePackID = String(pack.source_pack_id || '');
            if (sourcePackID) setOwnedSourcePackIDs((current) => new Set(current).add(sourcePackID));
            setAccountOpen(true);
            await loadAccount();
            await loadPacks();
            await loadRankings();
        }
        catch (err) { await showAlert(petStoreErrorText(lang, err), t(lang, '购买失败', 'Purchase failed')); }
        finally { setBusyID(''); }
    };
    const install = async (pack: StorePack) => {
        setBusyID(packBusyKey(pack));
        try {
            const id = await InstallPetStorePack(String(pack.id));
            const sourcePackID = String(pack.source_pack_id || '');
            if (sourcePackID) {
                // Ignore any in-flight registry probe that started before this
                // successful install; it may otherwise replace the fresh state
                // with its older false result.
                installedRequestRef.current++;
                installedProbeCacheRef.current.set(sourcePackID, true);
                setInstalledPackIDs((current) => new Set(current).add(sourcePackID));
            }
            await loadRankings();
            await showAlert(t(lang, `已安装宠物包：${id}`, `Installed pet pack: ${id}`), t(lang, '安装完成', 'Installed'));
        }
        catch (err) { await showAlert(petStoreErrorText(lang, err), t(lang, '安装失败', 'Install failed')); }
        finally { setBusyID(''); }
    };
    const withdraw = async (pack: StorePack) => {
        const ok = await showConfirm(t(lang, `下架「${pack.name}」后不会再被新用户获取；已购买用户仍可下载。`, `Unlist “${pack.name}”? Existing buyers keep lifetime downloads.`), t(lang, '下架宠物包', 'Unlist pet pack'), { confirmText: t(lang, '下架', 'Unlist'), cancelText: t(lang, '取消', 'Cancel'), confirmVariant: 'danger' });
        if (!ok) return;
        setBusyID(packBusyKey(pack));
        try { await WithdrawPetStorePack(String(pack.id)); await loadAccount(); await loadPacks(); await loadReport(); }
        catch (err) { await showAlert(petStoreErrorText(lang, err), t(lang, '下架失败', 'Unlist failed')); }
        finally { setBusyID(''); }
    };

    const user = account?.user || {};
    const userContact = accountContact(user);
    const uploads = Array.isArray(account?.uploads) ? account.uploads.filter(isAccountPackInstallable) : [];
    const purchases = Array.isArray(account?.purchases) ? account.purchases : [];
    const accountPackStatus = (pack: StorePack) => String(pack.status || 'active').toLowerCase();
    const accountPackUnavailable = (pack: StorePack) => !isAccountPackInstallable(pack);
    const accountPackStatusLabel = (pack: StorePack) => accountPackStatus(pack) === 'paused'
        ? t(lang, '已暂停', 'Paused')
        : accountPackStatus(pack) === 'deleted' || accountPackStatus(pack) === 'purged'
            ? t(lang, '已删除', 'Removed')
            : t(lang, '已下架', 'Unlisted');
    const accountPackUnavailableTitle = (pack: StorePack) => t(
        lang,
        `该宠物包${accountPackStatusLabel(pack)}，无法下载。`,
        `This pet pack was removed and cannot be downloaded.`,
    );
    const shiftReportDate = (offset: number) => {
        const next = shiftReportDateValue(reportDate, reportPeriod, offset);
        if (next <= todayISO()) {
            setReport(null);
            setReportDate(next);
        }
    };
    const selectReportPeriod = (period: ReportPeriod) => {
        if (period === reportPeriod) return;
        setReport(null);
        setReportPeriod(period);
    };
    const paidSummary = report?.paid_summary || {};
    const previousPaidSummary = report?.previous_paid_summary || {};
    const paidPacks = Array.isArray(report?.paid_packs) ? report.paid_packs : [];
    const freeDownloadPacks = Array.isArray(report?.free_download_packs) ? report.free_download_packs : [];
    return (
        <div className="pet-store-overlay" role="dialog" aria-modal="true" aria-label={t(lang, '宠物市场', 'Pet Store')}>
            <section className="pet-store-shell" ref={shellRef}>
                <header className="pet-store-header">
                    <div><h2>{t(lang, '宠物市场', 'Pet Store')}</h2><p>{t(lang, '所有宠物包均为一次性买断，购买后永久可下载。', 'Every pack is a one-time purchase with permanent download access.')}</p></div>
                    <div className="pet-store-header-actions">
                        <button className="pet-store-account-button" type="button" onClick={() => setAccountOpen(v => !v)} aria-expanded={accountOpen} aria-label={t(lang, '切换我的市场', 'Toggle My market')}>
                            <span>{t(lang, '我的市场', 'My market')}</span>
                        </button>
                        <button className="pet-store-close" type="button" onClick={onClose} aria-label={t(lang, '关闭', 'Close')}>×</button>
                    </div>
                </header>
                <div className="pet-store-layout">
                    <main className="pet-store-main" aria-busy={loading}>
                        <section className="pet-store-rankings" aria-label={t(lang, '宠物市场排行榜', 'Pet Store rankings')}>
                            <RankingRail title={t(lang, '制作者排行', 'Top creators')} items={Array.isArray(rankings?.creators) ? rankings.creators : []} metric={(item) => `${number(item.sales_amount)} Credits`} label={(item) => String(item.creator || '—')} />
                            <RankingRail title={t(lang, '宠物下载排行', 'Most downloaded')} items={Array.isArray(rankings?.downloads) ? rankings.downloads : []} metric={(item) => `${number(item.download_count)} ${t(lang, '下载', 'downloads')}`} label={(item) => String(item.name || '—')} />
                            <RankingRail title={t(lang, '宠物销售排行', 'Top sales')} items={Array.isArray(rankings?.sales) ? rankings.sales : []} metric={(item) => `${number(item.sales_amount)} Credits`} label={(item) => String(item.name || '—')} />
                        </section>
                        <form className="pet-store-toolbar" role="search" onSubmit={(event) => { event.preventDefault(); setPage(1); setSubmittedQuery(query.trim()); }}>
                            <input ref={searchRef} value={query} onChange={e => setQuery(e.target.value)} placeholder={t(lang, '搜索宠物包', 'Search pet packs')} aria-label={t(lang, '搜索宠物包', 'Search pet packs')} />
                            <select value={sort} onChange={e => { setSort(e.target.value); setPage(1); }} aria-label={t(lang, '排序字段', 'Sort field')}>
                                <option value="published">{t(lang, '发布时间', 'Published')}</option><option value="downloads">{t(lang, '下载量', 'Downloads')}</option><option value="sales">{t(lang, '销售额', 'Sales')}</option>
                            </select>
                            <button type="button" className="pet-store-order" onClick={() => { setOrder(v => v === 'desc' ? 'asc' : 'desc'); setPage(1); }} aria-label={t(lang, '切换排序方向', 'Toggle sort direction')}>{order === 'desc' ? t(lang, '降序 ↓', 'Descending ↓') : t(lang, '升序 ↑', 'Ascending ↑')}</button>
                            <button type="submit" className="btn-primary">{t(lang, '搜索', 'Search')}</button>
                        </form>
                        {packsSignInRequired ? (
                            <div className="pet-store-empty"><strong>{t(lang, '登录后使用宠物市场', 'Sign in to use the Pet Store')}</strong><span>{t(lang, '登录 HubCenter 后即可浏览、获取和安装宠物包。', 'Sign in to HubCenter to browse, get, and install pet packs.')}</span></div>
                        ) : error ? <div className="pet-store-message" role="alert"><span>{error}</span><button type="button" onClick={() => void loadPacks()}>{t(lang, '重试', 'Retry')}</button></div> : null}
                        {loading ? (
                            <>
                                <span className="pet-store-visually-hidden" role="status">{t(lang, '正在加载宠物市场…', 'Loading Pet Store…')}</span>
                                <PackGridSkeleton />
                            </>
                        ) : (
                            <div className="pet-store-grid" aria-live="polite">
                                {packs.map((pack, index) => {
                                    const owned = ownedSourcePackIDs.has(String(pack.source_pack_id || ''));
                                    const installed = isPackInstalled(pack);
                                    return <article className="pet-store-pack" key={String(pack.id)}>
                                    <div className="pet-store-pack-art">{pack.preview_data_url ? <img src={String(pack.preview_data_url)} alt={t(lang, `${pack.name} 预览`, `${pack.name} preview`)} loading={index < 4 ? 'eager' : 'lazy'} decoding="async" /> : <span aria-hidden="true">{[...String(pack.name || '?')][0]}</span>}</div>
                                    <div className="pet-store-pack-copy"><h3 title={String(pack.name || '')}>{pack.name}</h3><p>{pack.description || t(lang, '创作者未填写简介。', 'No description from the creator.')}</p></div>
                                    <dl><div><dt>{t(lang, '下载', 'Downloads')}</dt><dd>{number(pack.download_count)}</dd></div><div><dt>{t(lang, '销售额', 'Sales')}</dt><dd>{number(pack.sales_amount)}</dd></div><div><dt>{t(lang, '发布', 'Published')}</dt><dd>{date(pack.created_at, lang)}</dd></div></dl>
                                    <footer><span className={Number(pack.price) > 0 ? 'pet-store-price' : 'pet-store-free'}>{Number(pack.price) > 0 ? `${number(pack.price)} Credits` : t(lang, '免费', 'Free')}</span><button type="button" className="btn-primary" disabled={busyID !== '' || installed} onClick={() => void (owned ? install(pack) : purchase(pack))}>{isPackBusy(pack) ? t(lang, '处理中…', 'Working…') : installed ? t(lang, '已安装', 'Installed') : owned ? t(lang, '安装', 'Install') : t(lang, '获取', 'Get')}</button></footer>
                                </article>;
                                })}
                            </div>
                        )}
                        {!loading && !error && !packsSignInRequired && packs.length === 0 ? <div className="pet-store-empty"><strong>{t(lang, '没有找到匹配的宠物包', 'No matching pet packs')}</strong><span>{submittedQuery ? t(lang, '试试更换关键词，或清空搜索查看全部。', 'Try a different keyword, or clear the search to browse everything.') : t(lang, '宠物包上架后会显示在这里。', 'Published pet packs will appear here.')}</span></div> : null}
                        <nav className="pet-store-pagination" aria-label={t(lang, '分页', 'Pagination')}><button type="button" disabled={page <= 1} onClick={() => setPage(v => v - 1)}>{t(lang, '上一页', 'Previous')}</button><span>{page} / {totalPages} · {t(lang, '每页 20 个', '20 per page')}</span><button type="button" disabled={page >= totalPages} onClick={() => setPage(v => v + 1)}>{t(lang, '下一页', 'Next')}</button></nav>
                    </main>
                    <aside className={`pet-store-account ${accountOpen ? 'is-open' : ''}`} aria-label={t(lang, '我的市场', 'My market')}>
                        <div className="pet-store-account-head"><div><strong>{userContact || t(lang, '用户中心', 'Your account')}</strong><span>{accountLoading ? t(lang, '正在更新账户…', 'Updating account…') : t(lang, '当前可用余额', 'Available balance')}</span></div><b>{account ? `${number(account.credits)} ` : '— '}<small>Credits</small></b></div>
                        <section className="pet-store-report" aria-busy={reportLoading}>
                            <div className="pet-store-report-title"><h3>{t(lang, '我的市场', 'My market')}</h3><button type="button" onClick={() => void loadReport()}>{t(lang, '刷新', 'Refresh')}</button></div>
                            <div className="pet-store-report-controls" role="group" aria-label={t(lang, '报表周期', 'Report period')}>
                                {REPORT_PERIODS.map(period => <button key={period} type="button" className={reportPeriod === period ? 'active' : ''} aria-pressed={reportPeriod === period} onClick={() => selectReportPeriod(period)}>{period === 'day' ? t(lang, '日', 'Day') : period === 'month' ? t(lang, '月', 'Month') : t(lang, '年', 'Year')}</button>)}
                            </div>
                            <div className="pet-store-report-date"><button type="button" onClick={() => shiftReportDate(-1)} aria-label={t(lang, '上一周期', 'Previous period')}>‹</button><time dateTime={report?.start_at || reportDate}>{reportDateLabel(report?.start_at || reportDate, reportPeriod, lang)}</time><button type="button" onClick={() => shiftReportDate(1)} disabled={shiftReportDateValue(reportDate, reportPeriod, 1) > todayISO()} aria-label={t(lang, '下一周期', 'Next period')}>›</button></div>
                            {reportSignInRequired ? (
                                <div className="pet-store-empty"><strong>{t(lang, '登录后查看创作者报表', 'Sign in to view your creator report')}</strong><span>{t(lang, '登录 HubCenter 后，这里会显示你的宠物包销售与下载数据。', 'Once signed in to HubCenter, sales and download stats for your pet packs appear here.')}</span></div>
                            ) : reportError ? <div className="pet-store-message" role="alert"><span>{reportError}</span><button type="button" onClick={() => void loadReport()}>{t(lang, '重试', 'Retry')}</button></div> : null}
                            {reportSignInRequired ? null : reportLoading && !report ? <><span className="pet-store-visually-hidden" role="status">{t(lang, '正在加载销售报表…', 'Loading sales report…')}</span><ReportSkeleton /></> : <>
                                <div className="pet-store-report-summary"><div><span>{t(lang, '销售额', 'Sales')}</span><strong>{number(paidSummary.sales_amount)} <small>Credits</small></strong>{Number(previousPaidSummary.sales_amount) > 0 ? <em>{`${number(Number(paidSummary.sales_amount) - Number(previousPaidSummary.sales_amount))} Credits`}</em> : null}</div><div><span>{t(lang, '销量', 'Sold')}</span><strong>{number(paidSummary.sales_count)}</strong><em>{t(lang, '收费宠物', 'Paid pets')} {number(paidSummary.paid_pack_count)}</em></div></div>
                                <ReportList title={t(lang, '收费宠物销售', 'Paid pet sales')} items={paidPacks} metric={(pack) => `${number(pack.sales_count)} · ${number(pack.sales_amount)} Credits`} empty={t(lang, '本周期暂无收费宠物成交', 'No paid pet sales this period.')} />
                                <ReportList title={t(lang, '免费宠物下载', 'Free pet downloads')} items={freeDownloadPacks} metric={(pack) => `${number(pack.download_count)} ${t(lang, '下载', 'downloads')}`} empty={t(lang, '本周期暂无免费宠物下载', 'No free pet downloads this period.')} />
                            </>}
                        </section>
                        {accountOpen ? <>
                        <div className="pet-store-tabs" role="tablist" ref={tablistRef} onKeyDown={onTabListKeyDown} aria-label={t(lang, '用户中心面板', 'Account panels')}>
                        {ACCOUNT_TABS.map(tab => <button key={tab} type="button" role="tab" id={`pet-store-tab-${tab}`} tabIndex={accountTab === tab ? 0 : -1} aria-selected={accountTab === tab} aria-controls={`pet-store-panel-${tab}`} className={accountTab === tab ? 'active' : ''} onClick={() => setAccountTab(tab)}>{tab === 'uploads' ? t(lang, '我的上传', 'Uploads') : t(lang, '已购买', 'Purchased')}</button>)}
                        </div>
                        {accountSignInRequired ? (
                            <div className="pet-store-empty"><strong>{t(lang, '登录后使用宠物市场', 'Sign in to use the Pet Store')}</strong><span>{t(lang, '登录 HubCenter 后即可管理你的宠物包与购买记录。', 'Sign in to HubCenter to manage your pet packs and purchases.')}</span></div>
                        ) : accountError ? <div className="pet-store-message" role="alert"><span>{accountError}</span><button type="button" onClick={() => void loadAccount()}>{t(lang, '重试', 'Retry')}</button></div> : null}
                        {accountTab === 'uploads' && <div className="pet-store-account-list" role="tabpanel" id="pet-store-panel-uploads" aria-labelledby="pet-store-tab-uploads">{uploads.map((pack: StorePack) => {
                            const unavailable = accountPackUnavailable(pack);
                            return <div className="pet-store-account-item" key={String(pack.id)}><div><strong>{pack.name}</strong><span>{unavailable ? accountPackStatusLabel(pack) : t(lang, '已上架', 'Listed')} · {number(pack.download_count)} {t(lang, '下载', 'downloads')}</span></div><div className="pet-store-account-actions"><button type="button" className="btn-primary" onClick={() => void install(pack)} disabled={busyID !== '' || unavailable || isPackInstalled(pack)} title={unavailable ? accountPackUnavailableTitle(pack) : undefined}>{isPackBusy(pack) ? t(lang, '处理中…', 'Working…') : isPackInstalled(pack) ? t(lang, '已安装', 'Installed') : unavailable ? accountPackStatusLabel(pack) : t(lang, '安装', 'Install')}</button>{pack.status === 'active' ? <button type="button" onClick={() => void withdraw(pack)} disabled={busyID !== ''}>{isPackBusy(pack) ? t(lang, '处理中…', 'Working…') : t(lang, '下架', 'Unlist')}</button> : null}</div></div>;
                        })}{uploads.length === 0 && <div className="pet-store-empty"><strong>{t(lang, '还没有上传的宠物包', 'No uploaded pet packs')}</strong><span>{t(lang, '在自定义宠物包上选择“分享”即可发布。', 'Use “Share” on a custom pet pack to publish it.')}</span></div>}</div>}
                        {accountTab === 'purchases' && <div className="pet-store-account-list" role="tabpanel" id="pet-store-panel-purchases" aria-labelledby="pet-store-tab-purchases">{purchases.map((entry: StorePack) => { const pack = entry.pack || entry; const unavailable = accountPackUnavailable(pack); return <div className="pet-store-account-item" key={String(pack.id)}><div><strong>{pack.name}</strong><span>{!isActiveAccountPack(pack) ? `${accountPackStatusLabel(pack)} · ` : t(lang, '永久拥有 · ', 'Lifetime owned · ')}{date(entry.purchased_at, lang)}</span></div><button type="button" className="btn-primary" disabled={busyID !== '' || unavailable || isPackInstalled(pack)} title={unavailable ? accountPackUnavailableTitle(pack) : undefined} onClick={() => void install(pack)}>{isPackBusy(pack) ? t(lang, '处理中…', 'Working…') : isPackInstalled(pack) ? t(lang, '已安装', 'Installed') : unavailable ? accountPackStatusLabel(pack) : t(lang, '安装', 'Install')}</button></div>; })}{purchases.length === 0 && <div className="pet-store-empty"><strong>{t(lang, '还没有购买的宠物包', 'No purchased pet packs')}</strong><span>{t(lang, '获取的宠物包会永久保存在这里。', 'Packs you get stay here permanently.')}</span></div>}</div>}
                        </> : null}
                    </aside>
                </div>
            </section>
        </div>
    );
}

function PackGridSkeleton() {
    return (
        <div className="pet-store-grid" aria-hidden="true">
            {Array.from({ length: 8 }, (_, index) => (
                <div className="pet-store-pack pet-store-skeleton" key={index}>
                    <div className="pet-store-skeleton-art" />
                    <div className="pet-store-skeleton-lines">
                        <span className="pet-store-skeleton-line" style={{ width: '62%' }} />
                        <span className="pet-store-skeleton-line" style={{ width: '92%' }} />
                        <span className="pet-store-skeleton-line" style={{ width: '78%' }} />
                    </div>
                    <div className="pet-store-skeleton-foot">
                        <span className="pet-store-skeleton-line" style={{ width: '34%' }} />
                        <span className="pet-store-skeleton-line" style={{ width: '22%' }} />
                    </div>
                </div>
            ))}
        </div>
    );
}

function ReportSkeleton() {
    return <div className="pet-store-report-skeleton" aria-hidden="true">
        <div className="pet-store-report-skeleton-summary"><span /><span /></div>
        <div className="pet-store-report-skeleton-list"><span /><i /><i /></div>
        <div className="pet-store-report-skeleton-list"><span /><i /><i /></div>
    </div>;
}

function RankingRail({ title, items, metric, label }: { title: string; items: StorePack[]; metric: (item: StorePack) => string; label: (item: StorePack) => string }) {
    return <section className="pet-store-ranking"><h3>{title}<small>Top 10</small></h3><ol>{items.length ? items.map((item, index) => <li key={String(item.id || item.creator || index)}><span>{index + 1}</span><strong title={label(item)}>{label(item)}</strong><em>{metric(item)}</em></li>) : <li className="pet-store-ranking-empty">—</li>}</ol></section>;
}

function ReportList({ title, items, metric, empty }: { title: string; items: StorePack[]; metric: (item: StorePack) => string; empty: string }) {
    return <section className="pet-store-report-list"><h4>{title}</h4>{items.length ? <ol>{items.slice(0, 5).map((pack) => <li key={String(pack.id)}><div>{pack.preview_data_url ? <img src={String(pack.preview_data_url)} alt="" /> : <span aria-hidden="true">{[...String(pack.name || '?')][0]}</span>}<strong title={String(pack.name || '')}>{String(pack.name || '—')}</strong></div><em>{metric(pack)}</em></li>)}</ol> : <p>{empty}</p>}</section>;
}
