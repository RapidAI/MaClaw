import { useCallback, useEffect, useMemo, useState } from 'react';
import { GetCreditsAssetAccount, GetCreditsAssetTransactions, RedeemCreditsCard } from '../../../wailsjs/go/main/App';
import { localizeText } from '../../i18n';

type CreditTransaction = {
    id: string;
    type: string;
    amount: number;
    balance: number;
    description?: string;
    created_at?: string;
};

type Props = { lang: string };

const transactionsPageSize = 100;

const text = (lang: string, en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant || zhHans);

const formatDate = (value: string | undefined, lang: string) => {
    if (!value) return '–';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return new Intl.DateTimeFormat(lang === 'zh-Hant' ? 'zh-Hant' : lang === 'zh-Hans' ? 'zh-Hans' : 'en-US', {
        year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    }).format(date);
};

const isTopUp = (transaction: CreditTransaction) => transaction.type === 'topup';

export const AssetManagementPanel = ({ lang }: Props) => {
    const [credits, setCredits] = useState<number | null>(null);
    const [transactions, setTransactions] = useState<CreditTransaction[]>([]);
    const [transactionTotal, setTransactionTotal] = useState(0);
    const [activeKind, setActiveKind] = useState<'spending' | 'recharge'>('spending');
    const [code, setCode] = useState('');
    const [loading, setLoading] = useState(true);
    const [loadingMore, setLoadingMore] = useState(false);
    const [redeeming, setRedeeming] = useState(false);
    const [error, setError] = useState('');
    const [notice, setNotice] = useState('');

    const load = useCallback(async () => {
        setLoading(true);
        setError('');
        try {
            const [account, history] = await Promise.all([GetCreditsAssetAccount(), GetCreditsAssetTransactions(0)]);
            setCredits(Number(account?.credits || 0));
            const nextTransactions = Array.isArray(history?.transactions) ? history.transactions as CreditTransaction[] : [];
            setTransactions(nextTransactions);
            setTransactionTotal(Math.max(nextTransactions.length, Number(history?.total || 0)));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setLoading(false);
        }
    }, []);

    const loadMore = async () => {
        if (loading || loadingMore || transactions.length >= transactionTotal) return;
        setLoadingMore(true);
        setError('');
        try {
            const history = await GetCreditsAssetTransactions(transactions.length);
            const nextTransactions = Array.isArray(history?.transactions) ? history.transactions as CreditTransaction[] : [];
            setTransactions((current) => {
                const existing = new Set(current.map((transaction) => transaction.id));
                return current.concat(nextTransactions.filter((transaction) => !existing.has(transaction.id)));
            });
            setTransactionTotal((current) => Math.max(current, Number(history?.total || 0)));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setLoadingMore(false);
        }
    };

    useEffect(() => { void load(); }, [load]);

    const visibleTransactions = useMemo(
        () => transactions.filter((transaction) => activeKind === 'recharge' ? isTopUp(transaction) : transaction.type === 'purchase' || transaction.amount < 0),
        [activeKind, transactions],
    );

    const redeem = async () => {
        if (!code.trim() || redeeming) return;
        setRedeeming(true);
        setError('');
        setNotice('');
        try {
            const result = await RedeemCreditsCard(code.trim());
            setCredits(Number(result?.balance || 0));
            setCode('');
            setNotice(text(lang, 'Credits have been added to your account.', 'Credits 已充值到你的账户。', 'Credits 已儲值到你的帳戶。'));
            await load();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setRedeeming(false);
        }
    };

    return (
        <div className="settings-panel asset-management-panel">
            <header className="settings-panel-header">
                <div>
                    <h3 className="settings-panel-title">{text(lang, 'Asset Management', '资产管理', '資產管理')}</h3>
                    <p className="settings-panel-desc">{text(lang, 'Review the Credits used for Skills, pet packs, and AI experts.', '查看用于购买 Skill、宠物包和 AI 专家的 Credits。', '查看用於購買 Skill、寵物包和 AI 專家的 Credits。')}</p>
                </div>
                <button type="button" className="btn-secondary" onClick={() => void load()} disabled={loading || loadingMore}>{text(lang, 'Refresh', '刷新', '重新整理')}</button>
            </header>

            <section className="asset-balance-row" aria-busy={loading}>
                <div>
                    <span>{text(lang, 'Available Credits', '可用 Credits', '可用 Credits')}</span>
                    <strong>{credits === null ? '–' : credits.toLocaleString()}</strong>
                </div>
                <p>{text(lang, 'One balance across the Skill Market, Pet Store, and AI Expert Market.', 'Skill 市场、宠物商店与 AI 专家市场共用同一余额。', 'Skill 市場、寵物商店與 AI 專家市場共用同一餘額。')}</p>
            </section>

            <section className="asset-redeem-section">
                <div>
                    <h4>{text(lang, 'Redeem a card', '兑换卡充值', '兌換卡儲值')}</h4>
                    <p>{text(lang, 'Enter a Credits redemption code issued by your administrator.', '输入管理员发放的 Credits 兑换卡号。', '輸入管理員發放的 Credits 兌換卡號。')}</p>
                </div>
                <div className="asset-redeem-form">
                    <input value={code} onChange={(event) => setCode(event.target.value)} placeholder="CRD-…" autoComplete="off" aria-label={text(lang, 'Redemption code', '兑换卡号', '兌換卡號')} />
                    <button type="button" className="btn-primary" onClick={() => void redeem()} disabled={!code.trim() || redeeming}>{redeeming ? text(lang, 'Redeeming…', '正在兑换…', '正在兌換…') : text(lang, 'Redeem', '兑换', '兌換')}</button>
                </div>
                {notice && <p className="asset-notice" role="status">{notice}</p>}
                {error && <p className="asset-error" role="alert">{error}</p>}
            </section>

            <section className="asset-ledger-section">
                <div className="asset-ledger-head">
                    <div><h4>{text(lang, 'Credits history', 'Credits 明细', 'Credits 明細')}</h4><p>{text(lang, 'Each amount shows the resulting account balance.', '每条明细都显示交易后的账户余额。', '每筆明細都顯示交易後的帳戶餘額。')}</p></div>
                    <div className="asset-kind-tabs" role="tablist" aria-label={text(lang, 'Credits history type', 'Credits 明细类型', 'Credits 明細類型')}>
                        {(['spending', 'recharge'] as const).map((kind) => <button key={kind} type="button" role="tab" aria-selected={activeKind === kind} onClick={() => setActiveKind(kind)}>{kind === 'spending' ? text(lang, 'Spending', '消费明细', '消費明細') : text(lang, 'Recharge', '充值明细', '儲值明細')}</button>)}
                    </div>
                </div>
                <div className="asset-ledger-list" aria-busy={loading || loadingMore}>
                    {loading ? <div className="hint">{text(lang, 'Loading Credits history…', '正在加载 Credits 明细…', '正在載入 Credits 明細…')}</div> : visibleTransactions.length === 0 ? <div className="hint">{text(lang, 'No records yet.', '暂无记录。', '暫無記錄。')}</div> : visibleTransactions.map((transaction) => (
                        <article className="asset-transaction" key={transaction.id}>
                            <div><strong>{transaction.description || transaction.type}</strong><span>{formatDate(transaction.created_at, lang)}</span></div>
                            <div><b data-positive={transaction.amount > 0}>{transaction.amount > 0 ? '+' : ''}{Number(transaction.amount || 0).toLocaleString()}</b><span>{text(lang, 'Balance', '余额', '餘額')} {Number(transaction.balance || 0).toLocaleString()}</span></div>
                        </article>
                    ))}
                </div>
                {!loading && transactions.length < transactionTotal && <div className="asset-ledger-more"><span>{text(lang, `${transactions.length} of ${transactionTotal} records`, `已加载 ${transactions.length} / ${transactionTotal} 条`, `已載入 ${transactions.length} / ${transactionTotal} 筆`)}</span><button type="button" className="btn-secondary" onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? text(lang, 'Loading…', '正在加载…', '正在載入…') : text(lang, `Load ${transactionsPageSize} more`, `加载更多（${transactionsPageSize} 条）`, `載入更多（${transactionsPageSize} 筆）`)}</button></div>}
            </section>
        </div>
    );
};
