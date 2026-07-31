import { useCallback, useEffect, useRef, useState, type KeyboardEvent as ReactKeyEvent } from 'react';
import {
    GetPetStoreAccount,
    GetPetStoreRankings,
    InstallPetStorePack,
    ListPetStorePacks,
    PurchasePetStorePack,
    SubmitPetStorePack,
    WithdrawPetStorePack,
} from '../../wailsjs/go/main/App';
import { useDialog } from './CustomDialog';

type Lang = 'en' | 'zh-Hans' | 'zh-Hant' | string;
type StorePack = Record<string, any>;
type Draft = { name?: string; price?: number; zipPath?: string; sourcePackID?: string };

const ACCOUNT_TABS = ['uploads', 'purchases', 'publish'] as const;
type AccountTab = (typeof ACCOUNT_TABS)[number];

const t = (lang: Lang, zh: string, en: string) => lang === 'zh-Hans' || lang === 'zh-Hant' ? zh : en;
const errText = (err: unknown) => err instanceof Error ? err.message : String(err || 'Unknown error');
const number = (value: unknown) => {
    const numericValue = Number(value);
    return Number.isFinite(numericValue) ? numericValue.toLocaleString() : '—';
};
const date = (value: unknown, lang: Lang) => {
    if (!value) return '—';
    const parsed = new Date(String(value));
    if (Number.isNaN(parsed.getTime())) return '—';
    return parsed.toLocaleDateString(lang === 'zh-Hans' ? 'zh-CN' : lang === 'zh-Hant' ? 'zh-TW' : undefined);
};

export function PetStoreDialog({ lang, draft, onClose }: { lang: Lang; draft?: Draft; onClose: () => void }) {
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
    const [accountOpen, setAccountOpen] = useState(Boolean(draft?.zipPath));
    const [accountTab, setAccountTab] = useState<AccountTab>(draft?.zipPath ? 'publish' : 'uploads');
    const [account, setAccount] = useState<Record<string, any> | null>(null);
    const [accountLoading, setAccountLoading] = useState(false);
    const [rankings, setRankings] = useState<Record<string, any> | null>(null);
    const [accountError, setAccountError] = useState('');
    const [busyID, setBusyID] = useState('');
    const [ownedPackIDs, setOwnedPackIDs] = useState<Set<string>>(() => new Set());
    const [publishName, setPublishName] = useState(draft?.name || '');
    const [publishDescription, setPublishDescription] = useState('');
    const [publishVersion, setPublishVersion] = useState('1.0.0');
    const [publishPrice, setPublishPrice] = useState(String(draft?.price ?? 0));
    const [publishedDraft, setPublishedDraft] = useState(false);
    const searchRef = useRef<HTMLInputElement>(null);
    const publishNameRef = useRef<HTMLInputElement>(null);
    const shellRef = useRef<HTMLElement>(null);
    const tablistRef = useRef<HTMLDivElement>(null);
    const packsRequestRef = useRef(0);
    const accountRequestRef = useRef(0);
    const rankingsRequestRef = useRef(0);
    const packBusyKey = (pack: StorePack) => `pack:${String(pack.id)}`;
    const isPackBusy = (pack: StorePack) => busyID === packBusyKey(pack);

    useEffect(() => {
        setPublishName(draft?.name || '');
        setPublishPrice(String(draft?.price ?? 0));
        setPublishedDraft(false);
    }, [draft?.name, draft?.price, draft?.zipPath]);

    const loadPacks = useCallback(async () => {
        const requestID = ++packsRequestRef.current;
        setLoading(true); setError('');
        try {
            const result = await ListPetStorePacks(submittedQuery, sort, order, page, 20);
            if (requestID !== packsRequestRef.current) return;
            setPacks(Array.isArray(result?.packs) ? result.packs : []);
            setTotalPages(Math.max(1, Number(result?.total_pages || 1)));
        } catch (err) {
            if (requestID === packsRequestRef.current) setError(errText(err));
        } finally {
            if (requestID === packsRequestRef.current) setLoading(false);
        }
    }, [order, page, sort, submittedQuery]);

    const loadAccount = useCallback(async () => {
        const requestID = ++accountRequestRef.current;
        setAccountError('');
        setAccountLoading(true);
        try {
            const nextAccount = await GetPetStoreAccount();
            if (requestID !== accountRequestRef.current) return;
            setAccount(nextAccount);
            const purchases = Array.isArray(nextAccount?.purchases) ? nextAccount.purchases : [];
            setOwnedPackIDs(new Set(purchases.map((entry: StorePack) => String((entry.pack || entry).id || '')).filter(Boolean)));
        }
        catch (err) {
            if (requestID === accountRequestRef.current) setAccountError(errText(err));
        } finally {
            if (requestID === accountRequestRef.current) setAccountLoading(false);
        }
    }, []);
    const loadRankings = useCallback(async () => {
        const requestID = ++rankingsRequestRef.current;
        try {
            const nextRankings = await GetPetStoreRankings();
            if (requestID === rankingsRequestRef.current) setRankings(nextRankings);
        } catch {
            // Rankings are supplementary; browsing stays available.
        }
    }, []);

    useEffect(() => { void loadPacks(); }, [loadPacks]);
    useEffect(() => { void loadRankings(); }, [loadRankings]);
    useEffect(() => { if (accountOpen) void loadAccount(); }, [accountOpen, loadAccount]);
    // Focus management mirrors CustomDialog: move focus into the dialog on
    // open, cycle Tab inside the shell, restore focus to the invoker on close.
    // This handoff effect is mount-only and separate from the keydown binding:
    // the parent passes an inline onClose, so re-running on identity changes
    // would restore focus to the invoker while the dialog is still open.
    useEffect(() => {
        const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const focusFrame = window.requestAnimationFrame(() => {
            (draft?.zipPath ? publishNameRef.current : searchRef.current)?.focus();
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

    const hasPublishDraft = Boolean(draft?.zipPath) && !publishedDraft;

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
            setOwnedPackIDs((current) => new Set(current).add(String(pack.id)));
            setAccountOpen(true);
            await loadAccount();
            await loadPacks();
            await loadRankings();
        }
        catch (err) { await showAlert(errText(err), t(lang, '购买失败', 'Purchase failed')); }
        finally { setBusyID(''); }
    };
    const install = async (pack: StorePack) => {
        setBusyID(packBusyKey(pack));
        try { const id = await InstallPetStorePack(String(pack.id)); await loadRankings(); await showAlert(t(lang, `已安装宠物包：${id}`, `Installed pet pack: ${id}`), t(lang, '安装完成', 'Installed')); }
        catch (err) { await showAlert(errText(err), t(lang, '安装失败', 'Install failed')); }
        finally { setBusyID(''); }
    };
    const withdraw = async (pack: StorePack) => {
        const ok = await showConfirm(t(lang, `下架「${pack.name}」后不会再被新用户获取；已购买用户仍可下载。`, `Unlist “${pack.name}”? Existing buyers keep lifetime downloads.`), t(lang, '下架宠物包', 'Unlist pet pack'), { confirmText: t(lang, '下架', 'Unlist'), cancelText: t(lang, '取消', 'Cancel'), confirmVariant: 'danger' });
        if (!ok) return;
        setBusyID(packBusyKey(pack));
        try { await WithdrawPetStorePack(String(pack.id)); await loadAccount(); await loadPacks(); }
        catch (err) { await showAlert(errText(err), t(lang, '下架失败', 'Unlist failed')); }
        finally { setBusyID(''); }
    };
    const publish = async () => {
        const price = Number(publishPrice);
        if (!hasPublishDraft || !draft?.zipPath) { await showAlert(t(lang, '请从自定义宠物包的“分享”操作进入发布流程。', 'Start publishing with “Share” on a custom pet pack.')); return; }
        if (!publishName.trim() || !Number.isInteger(price) || price < 0) { await showAlert(t(lang, '请填写名称，并输入非负整数 Credits 价格。', 'Enter a name and a non-negative whole Credits price.')); return; }
        setBusyID('publish');
        try {
            await SubmitPetStorePack(draft.zipPath, publishName, publishDescription, publishVersion || '1.0.0', price, draft.sourcePackID || '');
            await showAlert(t(lang, '宠物包已发布。它采用一次性买断，不会创建订阅。', 'Pet pack published with a lifetime entitlement; no subscription was created.'), t(lang, '发布成功', 'Published'));
            // The archive is a one-use publishing draft. Clear the publish
            // fields immediately so a second click cannot create a duplicate
            // listing from the same exported pack.
            setPublishDescription('');
            setPublishedDraft(true);
            setAccountTab('uploads'); await loadAccount(); await loadPacks(); await loadRankings();
        } catch (err) { await showAlert(errText(err), t(lang, '发布失败', 'Publish failed')); }
        finally { setBusyID(''); }
    };

    const user = account?.user || {};
    const uploads = Array.isArray(account?.uploads) ? account.uploads : [];
    const purchases = Array.isArray(account?.purchases) ? account.purchases : [];
    return (
        <div className="pet-store-overlay" role="dialog" aria-modal="true" aria-label={t(lang, '宠物市场', 'Pet Store')}>
            <section className="pet-store-shell" ref={shellRef}>
                <header className="pet-store-header">
                    <div><h2>{t(lang, '宠物市场', 'Pet Store')}</h2><p>{t(lang, '所有宠物包均为一次性买断，购买后永久可下载。', 'Every pack is a one-time purchase with permanent download access.')}</p></div>
                    <div className="pet-store-header-actions">
                        <button className="pet-store-account-button" type="button" onClick={() => setAccountOpen(v => !v)} aria-expanded={accountOpen}>
                            <span>{user.email || t(lang, '用户中心', 'Account')}</span><strong>{account ? `${number(account.credits)} Credits` : '···'}</strong>
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
                        {error ? <div className="pet-store-message" role="alert"><span>{error}</span><button type="button" onClick={() => void loadPacks()}>{t(lang, '重试', 'Retry')}</button></div> : null}
                        {loading ? (
                            <>
                                <span className="pet-store-visually-hidden" role="status">{t(lang, '正在加载宠物市场…', 'Loading Pet Store…')}</span>
                                <PackGridSkeleton />
                            </>
                        ) : (
                            <div className="pet-store-grid" aria-live="polite">
                                {packs.map(pack => {
                                    const owned = ownedPackIDs.has(String(pack.id));
                                    return <article className="pet-store-pack" key={String(pack.id)}>
                                    <div className="pet-store-pack-art" aria-hidden="true">{[...String(pack.name || '?')][0]}</div>
                                    <div className="pet-store-pack-copy"><h3 title={String(pack.name || '')}>{pack.name}</h3><p>{pack.description || t(lang, '创作者未填写简介。', 'No description from the creator.')}</p></div>
                                    <dl><div><dt>{t(lang, '下载', 'Downloads')}</dt><dd>{number(pack.download_count)}</dd></div><div><dt>{t(lang, '销售额', 'Sales')}</dt><dd>{number(pack.sales_amount)}</dd></div><div><dt>{t(lang, '发布', 'Published')}</dt><dd>{date(pack.created_at, lang)}</dd></div></dl>
                                    <footer><span className={Number(pack.price) > 0 ? 'pet-store-price' : 'pet-store-free'}>{Number(pack.price) > 0 ? `${number(pack.price)} Credits` : t(lang, '免费', 'Free')}</span><button type="button" className="btn-primary" disabled={busyID !== ''} onClick={() => void (owned ? install(pack) : purchase(pack))}>{isPackBusy(pack) ? t(lang, '处理中…', 'Working…') : owned ? t(lang, '安装', 'Install') : t(lang, '获取', 'Get')}</button></footer>
                                </article>;
                                })}
                            </div>
                        )}
                        {!loading && !error && packs.length === 0 ? <div className="pet-store-empty"><strong>{t(lang, '没有找到匹配的宠物包', 'No matching pet packs')}</strong><span>{submittedQuery ? t(lang, '试试更换关键词，或清空搜索查看全部。', 'Try a different keyword, or clear the search to browse everything.') : t(lang, '宠物包上架后会显示在这里。', 'Published pet packs will appear here.')}</span></div> : null}
                        <nav className="pet-store-pagination" aria-label={t(lang, '分页', 'Pagination')}><button type="button" disabled={page <= 1} onClick={() => setPage(v => v - 1)}>{t(lang, '上一页', 'Previous')}</button><span>{page} / {totalPages} · {t(lang, '每页 20 个', '20 per page')}</span><button type="button" disabled={page >= totalPages} onClick={() => setPage(v => v + 1)}>{t(lang, '下一页', 'Next')}</button></nav>
                    </main>
                    {accountOpen ? <aside className="pet-store-account">
                        <div className="pet-store-account-head"><div><strong>{user.email || t(lang, '用户中心', 'Your account')}</strong><span>{accountLoading ? t(lang, '正在更新账户…', 'Updating account…') : t(lang, '当前可用余额', 'Available balance')}</span></div><b>{account ? `${number(account.credits)} ` : '— '}<small>Credits</small></b></div>
                        <div className="pet-store-tabs" role="tablist" ref={tablistRef} onKeyDown={onTabListKeyDown} aria-label={t(lang, '用户中心面板', 'Account panels')}>
                            {ACCOUNT_TABS.map(tab => <button key={tab} type="button" role="tab" id={`pet-store-tab-${tab}`} tabIndex={accountTab === tab ? 0 : -1} aria-selected={accountTab === tab} aria-controls={`pet-store-panel-${tab}`} className={accountTab === tab ? 'active' : ''} onClick={() => setAccountTab(tab)}>{tab === 'uploads' ? t(lang, '我的上传', 'Uploads') : tab === 'purchases' ? t(lang, '已购买', 'Purchased') : t(lang, '发布', 'Publish')}</button>)}
                        </div>
                        {accountError ? <div className="pet-store-message" role="alert"><span>{accountError}</span><button type="button" onClick={() => void loadAccount()}>{t(lang, '重试', 'Retry')}</button></div> : null}
                        {accountTab === 'uploads' && <div className="pet-store-account-list" role="tabpanel" id="pet-store-panel-uploads" aria-labelledby="pet-store-tab-uploads">{uploads.map((pack: StorePack) => <div className="pet-store-account-item" key={String(pack.id)}><div><strong>{pack.name}</strong><span>{pack.status === 'active' ? t(lang, '已上架', 'Listed') : t(lang, '已下架', 'Unlisted')} · {number(pack.download_count)} {t(lang, '下载', 'downloads')}</span></div><div className="pet-store-account-actions"><button type="button" className="btn-primary" onClick={() => void install(pack)} disabled={busyID !== ''}>{isPackBusy(pack) ? t(lang, '处理中…', 'Working…') : t(lang, '安装', 'Install')}</button>{pack.status === 'active' ? <button type="button" onClick={() => void withdraw(pack)} disabled={busyID !== ''}>{isPackBusy(pack) ? t(lang, '处理中…', 'Working…') : t(lang, '下架', 'Unlist')}</button> : null}</div></div>)}{uploads.length === 0 && <div className="pet-store-empty"><strong>{t(lang, '还没有上传的宠物包', 'No uploaded pet packs')}</strong><span>{t(lang, '在自定义宠物包上选择“分享”即可发布。', 'Use “Share” on a custom pet pack to publish it.')}</span></div>}</div>}
                        {accountTab === 'purchases' && <div className="pet-store-account-list" role="tabpanel" id="pet-store-panel-purchases" aria-labelledby="pet-store-tab-purchases">{purchases.map((entry: StorePack) => { const pack = entry.pack || entry; return <div className="pet-store-account-item" key={String(pack.id)}><div><strong>{pack.name}</strong><span>{t(lang, '永久拥有 · ', 'Lifetime owned · ')}{date(entry.purchased_at, lang)}</span></div><button type="button" className="btn-primary" disabled={busyID !== ''} onClick={() => void install(pack)}>{isPackBusy(pack) ? t(lang, '处理中…', 'Working…') : t(lang, '安装', 'Install')}</button></div>; })}{purchases.length === 0 && <div className="pet-store-empty"><strong>{t(lang, '还没有购买的宠物包', 'No purchased pet packs')}</strong><span>{t(lang, '获取的宠物包会永久保存在这里。', 'Packs you get stay here permanently.')}</span></div>}</div>}
                        {accountTab === 'publish' && <div className="pet-store-publish" role="tabpanel" id="pet-store-panel-publish" aria-labelledby="pet-store-tab-publish"><p>{hasPublishDraft ? t(lang, '已带入本地导出的宠物包。发布后可在“我的上传”中下架管理。', 'Your locally exported pet pack is ready. Manage its listing in Uploads after publishing.') : publishedDraft ? t(lang, '此导出包已发布。请在“我的上传”中管理，或从另一个自定义宠物包重新开始分享。', 'This exported pack is published. Manage it in Uploads, or share another custom pet pack.') : t(lang, '请先在自定义宠物包上右键选择“分享”。', 'Right-click a custom pet pack and choose “Share” first.')}</p><label>{t(lang, '名称', 'Name')}<input ref={publishNameRef} value={publishName} maxLength={60} onChange={e => setPublishName(e.target.value)} disabled={!hasPublishDraft || busyID !== ''} /></label><label>{t(lang, '说明', 'Description')}<textarea value={publishDescription} maxLength={1000} onChange={e => setPublishDescription(e.target.value)} disabled={!hasPublishDraft || busyID !== ''} /></label><div className="pet-store-publish-row"><label>{t(lang, '版本', 'Version')}<input value={publishVersion} maxLength={80} onChange={e => setPublishVersion(e.target.value)} disabled={!hasPublishDraft || busyID !== ''} /></label><label>{t(lang, '价格（Credits）', 'Price (Credits)')}<input inputMode="numeric" value={publishPrice} onChange={e => setPublishPrice(e.target.value)} disabled={!hasPublishDraft || busyID !== ''} /></label></div><button type="button" className="btn-primary" disabled={!hasPublishDraft || busyID !== ''} onClick={() => void publish()}>{busyID === 'publish' ? t(lang, '发布中…', 'Publishing…') : t(lang, '发布宠物包', 'Publish pet pack')}</button></div>}
                    </aside> : null}
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

function RankingRail({ title, items, metric, label }: { title: string; items: StorePack[]; metric: (item: StorePack) => string; label: (item: StorePack) => string }) {
    return <section className="pet-store-ranking"><h3>{title}<small>Top 10</small></h3><ol>{items.length ? items.map((item, index) => <li key={String(item.id || item.creator || index)}><span>{index + 1}</span><strong title={label(item)}>{label(item)}</strong><em>{metric(item)}</em></li>) : <li className="pet-store-ranking-empty">—</li>}</ol></section>;
}
