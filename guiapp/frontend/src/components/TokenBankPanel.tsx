import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { QRCodeSVG } from 'qrcode.react';
import {
    TokenBankAddShareKey,
    TokenBankClaimGiftLink,
    TokenBankCreateGiftLink,
    TokenBankListGiftLinks,
    GetMaclawLLMProviders,
    TokenBankListShareModels,
    TokenBankListShares,
    TokenBankListWithdrawals,
    TokenBankPreviewGiftLink,
    TokenBankRevokeGiftLink,
    TokenBankRotateShareKey,
    TokenBankSetSharePaused,
    TokenBankSummary,
    TokenBankTakeOutShare,
    TokenBankWithdraw,
    TokenBankWithdrawGift,
} from '../../wailsjs/go/main/App';
import { localizeText } from '../i18n/langSelect';
import { useDialog } from './CustomDialog';
import {
    MICROCREDITS_PER_CREDIT,
    creditsToMicro,
    extractTokenBankClaimedLinks,
    extractTokenBankGiftLinks,
    extractTokenBankModelDetails,
    extractTokenBankModels,
    extractTokenBankShares,
    matchTokenBankShareCredential,
    unionShareModelNames,
    extractTokenBankWithdrawals,
    fingerprintProviderKey,
    formatCredits,
    formatCreditsGrouped,
    formatGiftInstant,
    giftInstantPassed,
    giftAmountRejection,
    giftClaimTarget,
    giftCodeFromInput,
    isShareLive,
    newWithdrawRequestID,
    normalizeTokenBankGiftLink,
    normalizeTokenBankGiftPreview,
    normalizeTokenBankSummary,
    toShareStatus,
    wholeCreditsOrMicro,
    type TokenBankGiftLink,
    type TokenBankGiftPreview,
    type TokenBankShare,
    type TokenBankShareModel,
    type TokenBankShareWindow,
    type TokenBankStatsRange,
    type TokenBankSummary as TokenBankSummaryData,
    type TokenBankWithdrawal,
} from '../utils/hubcenterTokenBank';
import { TokenBankAccessDialog } from './TokenBankAccessDialog';
import { TokenBankShareDialog } from './TokenBankShareDialog';
import { isTokenBankShareableProvider, tokenBankShareCredential, TokenBankShareableProviders } from './TokenBankShareableProviders';
import { listProviderModelsForShare, probeProviderModelForShare, type LLMProvider } from './remote/LLMConfigPanelShared';

type TokenBankPanelProps = {
    lang: string;
    showToastMessage?: (message: string) => void;
    initialClaimCode?: string;
    // Bumps on every open, including a repeat of the same code. The code alone
    // cannot tell a second click of the same link from the first.
    initialClaimSeq?: number;
    onRequestVerification?: () => void;
};

const textForLang = localizeText;

// A second quote, a line break, or the irreversibility phrase inside the name
// would keep the provider name in the sentence instead of the card.
function shareDialogSubject(value: string): string {
    return value
        .replace(/[\r\n\t]+/g, ' ')
        .replace(/[「」“”"]/g, '')
        .replace(/此操作不可撤[销銷]|此操作不可復原|this (?:action )?cannot be undone/ig, '')
        .replace(/\s+/g, ' ')
        .trim();
}

export function takeOutShareDialog(lang: string, displayName: string, shareID: string): { message: string; title: string } {
    const name = shareDialogSubject(displayName) || shareDialogSubject(shareID) || '—';
    return {
        message: textForLang(
            lang,
            `Take this share out of the bank "${name}"?\nIts models will stop being reachable. This cannot be undone.`,
            `确定从银行取出分享「${name}」？\n其模型不再可被调用。此操作不可撤销。`,
            `確定從銀行取出分享「${name}」？\n其模型不再可被呼叫。此操作不可撤銷。`,
        ),
        title: textForLang(lang, 'Take out share', '取出分享', '取出分享'),
    };
}

function badgeText(
    t: (en: string, zhHans: string, zhHant?: string) => string,
    kind: string,
): string {
    switch (kind) {
        case 'gold':
            return t('Gold', '金', '金');
        case 'silver':
            return t('Silver', '银', '銀');
        case 'bronze':
            return t('Bronze', '铜', '銅');
        case 'pillar':
            return t('Pillar', '支柱', '支柱');
        case 'steady':
            return t('Steady', '稳定', '穩定');
        case 'contributor':
            return t('Contributor', '贡献', '貢獻');
        case 'sprout':
            return t('Sprout', '新芽', '新芽');
        default:
            return kind;
    }
}

const STATUS_LABEL: Record<string, [string, string, string]> = {
    active: ['Active', '生效中', '生效中'],
    paused: ['Paused', '已暂停', '已暫停'],
    revoked: ['Taken out', '已取出', '已取出'],
};

// Three columns by nine cards. The list itself is the full set from
// TokenBankListShares; the page is only which nine are on screen.
const SHARE_PAGE_SIZE = 9;

// Four columns by twenty cards. TokenBankListWithdrawals already returns the
// caller's history; the page is only which twenty are on screen.
const WITHDRAW_PAGE_SIZE = 20;

// Kind is the ledger word. The card says where the credits came from.
const WITHDRAW_KIND_LABEL: Record<string, [string, string, string]> = {
    self: ['Your own credits', '自己的积分', '自己的積分'],
    gift: ['A claimed gift', '领取的转赠', '領取的轉贈'],
};

// Status is the grant lifecycle, not a second copy of the kind.
const WITHDRAW_STATE_LABEL: Record<string, [string, string, string]> = {
    issued: ['Pending', '待入账', '待入帳'],
    bound: ['Credited', '已入账', '已入帳'],
    reissued: ['Reissued', '已重发', '已重發'],
};

// The history lists every machine this account has pulled credits onto.
// "This machine" would mislabel a grant that landed on another hub.
const WITHDRAW_STATE_DETAIL: Record<string, [string, string, string]> = {
    issued: [
        'The bank already deducted this amount. The grant is not recorded on the machine yet.',
        '银行已扣出这笔积分，发放单尚未写入机器。',
        '銀行已扣出這筆積分，發放單尚未寫入機器。',
    ],
    bound: [
        'The grant is recorded on that machine. The assistant there can spend it.',
        '发放单已写入该机器，那里的助手可以花费。',
        '發放單已寫入該機器，那裡的助手可以花費。',
    ],
    reissued: [
        'That machine was reinstalled, and the grant was written again.',
        '该机器重装之后，发放单已重新写入。',
        '該機器重裝之後，發放單已重新寫入。',
    ],
};

// Rows with no hub id must not point at "that machine".
const WITHDRAW_STATE_DETAIL_UNNAMED: Record<string, [string, string, string]> = {
    issued: [
        'The bank already deducted this amount. The grant is not recorded yet.',
        '银行已扣出这笔积分，发放单尚未写入。',
        '銀行已扣出這筆積分，發放單尚未寫入。',
    ],
    bound: [
        'The grant is recorded. The assistant on the credited machine can spend it.',
        '发放单已写入，对应机器上的助手可以花费。',
        '發放單已寫入，對應機器上的助手可以花費。',
    ],
    reissued: [
        'The credited machine was reinstalled, and the grant was written again.',
        '入账的机器重装之后，发放单已重新写入。',
        '入帳的機器重裝之後，發放單已重新寫入。',
    ],
};

// A self debit with no grant id is an unconfirmed ack. The bank has moved the
// credits; an empty grant id does not mean this machine never wrote them.
const SELF_ISSUED_STATE_LABEL: [string, string, string] = ['Unconfirmed', '未确认', '未確認'];

const SELF_ISSUED_DETAIL: [string, string, string] = [
    'The bank already deducted this amount. That machine has not confirmed the grant id back.',
    '银行已扣出这笔积分，该机器尚未把发放单号确认回去。',
    '銀行已扣出這筆積分，該機器尚未把發放單號確認回去。',
];

const SELF_ISSUED_DETAIL_UNNAMED: [string, string, string] = [
    'The bank already deducted this amount. The grant id has not been confirmed back.',
    '银行已扣出这笔积分，发放单号尚未确认回去。',
    '銀行已扣出這筆積分，發放單號尚未確認回去。',
];

function withdrawStateLabel(kind: string, state: string, fallback: string): [string, string, string] {
    if (kind !== 'gift' && state === 'issued') return SELF_ISSUED_STATE_LABEL;
    return WITHDRAW_STATE_LABEL[state] || [fallback, fallback, fallback];
}

function withdrawStateDetail(kind: string, state: string, hasMachine: boolean): [string, string, string] | undefined {
    if (kind !== 'gift' && state === 'issued') {
        return hasMachine ? SELF_ISSUED_DETAIL : SELF_ISSUED_DETAIL_UNNAMED;
    }
    return (hasMachine ? WITHDRAW_STATE_DETAIL : WITHDRAW_STATE_DETAIL_UNNAMED)[state];
}

function withdrawPillClass(state: string): string {
    if (state === 'bound' || state === 'reissued') return 'tbk-pill--on';
    if (state === 'issued') return 'tbk-pill--wait';
    return 'tbk-pill--off';
}

const GIFT_STATUS_LABEL: Record<string, [string, string, string]> = {
    active: ['Waiting to be claimed', '待领取', '待領取'],
    // Claimed is the hold: a person is bound, the credits are still frozen.
    claimed: ['Not withdrawn', '未提取', '未提取'],
    revoked: ['Revoked', '已撤销', '已撤銷'],
    expired: ['Expired', '已过期', '已過期'],
    // Settled means the receiver withdrew the gift onto their machine.
    settled: ['Withdrawn', '已提取', '已提取'],
};

function giftRecipient(link: TokenBankGiftLink): string {
    return link.claimed_by_email.trim() || link.claimed_by_user_id.trim();
}

function giftSenderCanRevoke(status: string): boolean {
    // Claim binds a person and leaves the credits frozen. They move only when
    // that person withdraws, so the sender can still take a claimed gift back.
    return status === 'active' || status === 'claimed';
}

function giftErrorText(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

function giftErrorIsRevoked(err: unknown): boolean {
    return /gift_revoked|sender revoked this gift/i.test(giftErrorText(err));
}

function giftActionError(
    t: (en: string, zhHans: string, zhHant?: string) => string,
    err: unknown,
    kind: 'revoke' | 'withdraw' | 'claim',
): string {
    const raw = giftErrorText(err);
    if (giftErrorIsRevoked(err)) {
        return t('The sender revoked this gift.', '发送方已撤销这份转赠。', '發送方已撤銷這份轉贈。');
    }
    if (kind === 'revoke' && /"not_active"|no longer active/i.test(raw)) {
        return t('This gift can no longer be revoked.', '这笔转赠已经不能撤销。', '這筆轉贈已經不能撤銷。');
    }
    return raw;
}

function giftPillClass(status: string, claimClosed = false): string {
    // Active past the timestamp is not claimable yet, and the freeze is still
    // held. The green "waiting" pill would say the opposite.
    if (claimClosed) return 'tbk-pill--wait';
    if (status === 'active') return 'tbk-pill--on';
    if (status === 'claimed') return 'tbk-pill--wait';
    return 'tbk-pill--off';
}

const GIFT_PENDING_RETURN_LABEL: [string, string, string] = ['Pending return', '待退回', '待退回'];

// The sweeper has not released the freeze. Claim already refuses the link.
function giftClaimClosedText(t: (en: string, zhHans: string, zhHant?: string) => string): string {
    return t(
        'The expiry time has passed and the credits are not back yet. It can no longer be claimed.',
        '已过到期时间，积分尚未退回。现在已不能领取。',
        '已過到期時間，積分尚未退回。現在已不能領取。',
    );
}

// Why a preview cannot be claimed, or null when the recipient may still claim it.
// "held" means this account already claimed it and can still withdraw.
// "withdrawn" means this account already pulled it onto a machine.
// "closed" is an active link past expires_at: claim is refused, and the
// freeze stays until the sweeper sets "expired". A claimed link stays
// claimed until that status changes. Unknown states stay "unavailable"
// so the claim call can still ask the server.
function giftClaimBlock(preview: TokenBankGiftPreview | null): 'claimed' | 'held' | 'withdrawn' | 'settled' | 'revoked' | 'expired' | 'closed' | 'unavailable' | null {
    if (!preview || preview.claimable) return null;
    if (preview.withdrawable && preview.id) return 'held';
    if (preview.withdrawn) return 'withdrawn';
    const at = Date.parse(preview.expires_at || '');
    const past = Number.isFinite(at) && at <= Date.now();
    switch ((preview.status || '').toLowerCase()) {
        case 'claimed':
            // A stranger sees this. The claimer is "held" above: preview
            // keeps withdrawable set until the sweeper returns the credits.
            // The timestamp passing is not that return.
            return 'claimed';
        case 'settled':
            // The claimer already pulled the credits onto a machine.
            // "Already claimed" still sounds like they can be withdrawn.
            return 'settled';
        case 'revoked':
            return 'revoked';
        case 'expired':
            return 'expired';
        case 'active':
            if (past) return 'closed';
            return 'unavailable';
        default:
            return 'unavailable';
    }
}

function giftClaimIsError(preview: TokenBankGiftPreview | null): boolean {
    const block = giftClaimBlock(preview);
    return block !== null && block !== 'held' && block !== 'withdrawn';
}

// The debit is stored, and the hub has not confirmed the grant. A new request
// id is refused, so this row is the only way to finish that same withdrawal.
function giftWithdrawalNeedsGrant(item: TokenBankWithdrawal): boolean {
    return item.kind.toLowerCase() === 'gift' && item.link_id !== '' && item.request_id !== '' && item.grant_id === '' && item.amount_micro > 0;
}

// Keep an unfinished gift debit on the first page. A newer page of ordinary
// withdrawals must not hide the only button that can still write its grant.
function orderWithdrawalsForDisplay(items: TokenBankWithdrawal[]): { rows: TokenBankWithdrawal[]; pending: number } {
    const pending: TokenBankWithdrawal[] = [];
    const rest: TokenBankWithdrawal[] = [];
    for (const item of items) {
        if (giftWithdrawalNeedsGrant(item)) pending.push(item);
        else rest.push(item);
    }
    return {
        rows: pending.length === 0 ? items : pending.concat(rest),
        pending: pending.length,
    };
}

function giftLinkFromWithdrawal(item: TokenBankWithdrawal): TokenBankGiftLink {
    return {
        id: item.link_id,
        code: '',
        credits_micro: item.amount_micro,
        status: 'settled',
        created_at: '',
        expires_at: '',
        claimed_at: '',
        revoked_at: '',
        claimed: true,
        claimed_by_email: '',
        claimed_by_user_id: '',
        sender_masked: '',
        claim_url: '',
        deep_link: '',
    };
}

function giftLinkFromPreview(preview: TokenBankGiftPreview): TokenBankGiftLink {
    return {
        id: preview.id,
        code: preview.code,
        credits_micro: preview.credits_micro,
        status: 'claimed',
        created_at: '',
        expires_at: preview.expires_at,
        claimed_at: '',
        revoked_at: '',
        claimed: true,
        claimed_by_email: '',
        claimed_by_user_id: '',
        sender_masked: preview.sender_masked,
        claim_url: '',
        deep_link: '',
    };
}

export const TokenBankPanel = ({ lang, showToastMessage, initialClaimCode, initialClaimSeq = 0, onRequestVerification }: TokenBankPanelProps) => {
    const { showAlert, showConfirm, showPrompt } = useDialog();
    const [summary, setSummary] = useState<TokenBankSummaryData | null>(null);
    const [shares, setShares] = useState<TokenBankShare[]>([]);
    const [withdrawals, setWithdrawals] = useState<TokenBankWithdrawal[]>([]);
    const [gifts, setGifts] = useState<TokenBankGiftLink[]>([]);
    const [heldGifts, setHeldGifts] = useState<TokenBankGiftLink[]>([]);
    const [giftOpen, setGiftOpen] = useState(false);
    const [giftAmount, setGiftAmount] = useState('');
    const [createdGift, setCreatedGift] = useState<TokenBankGiftLink | null>(null);
    const [claimInput, setClaimInput] = useState('');
    const [giftPreview, setGiftPreview] = useState<TokenBankGiftPreview | null>(null);
    const [claimedGift, setClaimedGift] = useState<TokenBankGiftLink | null>(null);
    // Claim and withdraw results stay here. A toast is too easy to miss: it
    // fades in three seconds, and the confirm dialog sits above it.
    const [giftNotice, setGiftNotice] = useState<{ tone: 'info' | 'ok' | 'err'; text: string } | null>(null);
    const [modelsByShare, setModelsByShare] = useState<Record<string, TokenBankShareModel[]>>({});
    const [expanded, setExpanded] = useState<Record<string, boolean>>({});
    const [shareRange, setShareRange] = useState<TokenBankStatsRange>('all');
    const [sharePage, setSharePage] = useState(1);
    const [withdrawPage, setWithdrawPage] = useState(1);
    const [accessShare, setAccessShare] = useState<TokenBankShare | null>(null);
    const [adjustShare, setAdjustShare] = useState<{
        shareID: string;
        provider: LLMProvider;
        models: string[];
        enabled: string[];
        windows: Record<string, TokenBankShareWindow>;
    } | null>(null);
    const [loading, setLoading] = useState(false);
    const [loadedOnce, setLoadedOnce] = useState(false);
    // The provider list waits for this so it does not race the first summary load.
    const [providerReload, setProviderReload] = useState(0);
    const [error, setError] = useState('');
    const [busyKey, setBusyKey] = useState('');
    // Kept until a withdraw succeeds. A retry must send the same request id,
    // because the hub may already have debited HubCenter before the response
    // was lost. A new id on every click would take the credits twice.
    const withdrawRequestID = useRef('');
    // setState does not block a second click in the same turn. This does.
    const giftWithdrawBusy = useRef(false);
    // One id per gift for as long as this panel is open. The hub debits once
    // per id. A single shared slot forgets gift A's id when gift B is
    // withdrawn, and the retry of A then takes the amount a second time.
    const giftWithdrawIDs = useRef<Record<string, string>>({});
    // Withdraw succeeded for these ids. A later list or a late lookup can still
    // describe them as held; they are not waiting to be withdrawn again.
    const settledGiftIDs = useRef(new Set<string>());
    const claimInputRef = useRef('');
    claimInputRef.current = claimInput;
    const alertRef = useRef(showAlert);
    alertRef.current = showAlert;
    const langRef = useRef(lang);
    langRef.current = lang;
    // Bumps when a lookup or a claim starts. A deep-link preview has no busy
    // flag, so its late response must not paint over a newer lookup or claim.
    const lookupGen = useRef(0);
    const toastRef = useRef(showToastMessage);
    toastRef.current = showToastMessage;
    const expandedRef = useRef(expanded);
    expandedRef.current = expanded;
    const shareRangeRef = useRef(shareRange);
    shareRangeRef.current = shareRange;

    // The withdraw button under the form belongs to this lookup. A claimable
    // or dead result must not keep the previous gift's button.
    const rememberGiftPreview = useCallback((preview: TokenBankGiftPreview) => {
        const settled = preview.id !== '' && settledGiftIDs.current.has(preview.id);
        const shown = settled
            ? { ...preview, claimable: false, withdrawable: false, withdrawn: true }
            : preview;
        setGiftPreview(shown);
        // The row's withdraw button is derived from this preview. A claim
        // result for a different code stays until its own lookup says it is
        // not held; clearing it here drops the only withdraw control when
        // the list has not caught up yet.
        if (settled || giftClaimBlock(shown) !== 'held') {
            setClaimedGift((current) => {
                if (!current?.code || current.code.toLowerCase() !== shown.code.toLowerCase()) return current;
                return null;
            });
        }
    }, []);

    useEffect(() => {
        const code = giftCodeFromInput(initialClaimCode || '');
        // Do not remember the code. Remembering it drops the second setup
        // under StrictMode, and a second open of the same link never runs.
        if (!code) return;
        setClaimInput(code);
        claimInputRef.current = code;
        // The previous line belonged to the code that was in the box. This
        // lookup replaces that code, including a claim that is still in flight.
        setGiftNotice(null);
        const gen = ++lookupGen.current;
        let cancelled = false;
        void TokenBankPreviewGiftLink(code).then((raw) => {
            if (cancelled || lookupGen.current !== gen) return;
            const preview = normalizeTokenBankGiftPreview(raw);
            if (giftCodeFromInput(claimInputRef.current).toLowerCase() !== preview.code.toLowerCase()) return;
            rememberGiftPreview(preview);
        }).catch((err) => {
            if (cancelled || lookupGen.current !== gen) return;
            if (giftCodeFromInput(claimInputRef.current).toLowerCase() !== code.toLowerCase()) return;
            const text = err instanceof Error ? err.message : String(err);
            setGiftNotice({ tone: 'err', text });
            const uiLang = langRef.current;
            void alertRef.current(
                text,
                textForLang(uiLang, 'Preview failed', '查验失败', '查驗失敗'),
            );
        });
        return () => { cancelled = true; };
    }, [initialClaimCode, initialClaimSeq, rememberGiftPreview]);

    const t = useCallback(
        (en: string, zhHans: string, zhHant?: string) => textForLang(lang, en, zhHans, zhHant),
        [lang],
    );

    const notify = useCallback((message: string) => {
        if (message) showToastMessage?.(message);
    }, [showToastMessage]);

    const refresh = useCallback(async () => {
        setLoading(true);
        setError('');
        try {
            // Sequential, not Promise.all: a signed-out user gets the same
            // "please sign in" error from all three, and showing one message
            // beats racing three rejections into a single toast.
            const summaryRaw = await TokenBankSummary();
            setSummary(normalizeTokenBankSummary(summaryRaw));
            const sharesRaw = await TokenBankListShares(shareRange);
            setShares(extractTokenBankShares(sharesRaw));
            const withdrawalsRaw = await TokenBankListWithdrawals();
            const withdrawalRows = extractTokenBankWithdrawals(withdrawalsRaw);
            setWithdrawals(withdrawalRows);
            // The held card goes away once the link is settled, which is before
            // the hub grant is confirmed. The debit's request id is already on
            // this row; a freshly minted id is rejected and the grant is never written.
            for (const item of withdrawalRows) {
                if (giftWithdrawalNeedsGrant(item)) {
                    giftWithdrawIDs.current[item.link_id] = item.request_id;
                }
            }
            const giftsRaw = await TokenBankListGiftLinks();
            setGifts(extractTokenBankGiftLinks(giftsRaw));
            // The list keeps a claim until the sweeper changes its status, even
            // after the return time. A clock check here would hide a gift this
            // machine still owes when its clock is ahead.
            setHeldGifts(extractTokenBankClaimedLinks(giftsRaw).filter((link) => (
                link.status === 'claimed' && link.id !== '' && link.credits_micro > 0
                && !settledGiftIDs.current.has(link.id)
            )));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setLoading(false);
            setLoadedOnce(true);
            setProviderReload((n) => n + 1);
        }
    }, [shareRange]);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    const sharePageCount = Math.max(1, Math.ceil(shares.length / SHARE_PAGE_SIZE));
    // The stored page can outlive a shorter list (a take-out, a failed load).
    // Adjust it while rendering so the next paint is already on a real page.
    if (sharePage > sharePageCount) {
        setSharePage(sharePageCount);
    }
    const sharePageSafe = Math.min(sharePage, sharePageCount);
    const visibleShares = shares.slice((sharePageSafe - 1) * SHARE_PAGE_SIZE, sharePageSafe * SHARE_PAGE_SIZE);

    const orderedWithdrawals = orderWithdrawalsForDisplay(withdrawals);
    const pendingWithdrawalCount = orderedWithdrawals.pending;
    const withdrawPageCount = Math.max(1, Math.ceil(orderedWithdrawals.rows.length / WITHDRAW_PAGE_SIZE));
    if (withdrawPage > withdrawPageCount) {
        setWithdrawPage(withdrawPageCount);
    }
    const withdrawPageSafe = Math.min(withdrawPage, withdrawPageCount);
    const visibleWithdrawals = orderedWithdrawals.rows.slice(
        (withdrawPageSafe - 1) * WITHDRAW_PAGE_SIZE,
        withdrawPageSafe * WITHDRAW_PAGE_SIZE,
    );

    // An open detail stays on the window the user just picked. The ref is read
    // so toggling a card does not refetch every other open card.
    useEffect(() => {
        const open = Object.entries(expandedRef.current).filter(([, on]) => on).map(([id]) => id);
        if (!open.length) return;
        let cancelled = false;
        void (async () => {
            const next: Record<string, TokenBankShareModel[]> = {};
            for (const id of open) {
                try {
                    const raw = await TokenBankListShareModels(id, shareRange);
                    next[id] = extractTokenBankModelDetails(raw);
                } catch (err) {
                    if (!cancelled) toastRef.current?.(err instanceof Error ? err.message : String(err));
                }
            }
            if (!cancelled) setModelsByShare((prev) => ({ ...prev, ...next }));
        })();
        return () => { cancelled = true; };
    }, [shareRange]);

    const toggleModels = useCallback(async (shareID: string) => {
        const nextOpen = !expanded[shareID];
        setExpanded((prev) => ({ ...prev, [shareID]: nextOpen }));
        if (!nextOpen) return;
        const requested = shareRange;
        setBusyKey(`models:${shareID}`);
        try {
            const raw = await TokenBankListShareModels(shareID, requested);
            if (shareRangeRef.current !== requested) return;
            setModelsByShare((prev) => ({ ...prev, [shareID]: extractTokenBankModelDetails(raw) }));
        } catch (err) {
            notify(err instanceof Error ? err.message : String(err));
        } finally {
            setBusyKey('');
        }
    }, [expanded, notify, shareRange]);

    const adjustModels = useCallback(async (share: TokenBankShare) => {
        if (busyKey || toShareStatus(share.status) === 'revoked') return;
        setBusyKey(`adjust:${share.id}`);
        try {
            const data = await GetMaclawLLMProviders() as { providers?: unknown } | null;
            const providers = Array.isArray(data?.providers) ? data.providers as LLMProvider[] : [];
            const shareable = providers.filter(isTokenBankShareableProvider);
            const index = await matchTokenBankShareCredential(
                shareable.map((provider) => ({ url: provider.url, key: tokenBankShareCredential(provider) })),
                share.api_url,
                share.key_fingerprint,
            );
            if (index < 0) {
                await showAlert(
                    t(
                        'This machine has no saved provider whose key matches this share, so the models cannot be probed. Keep the same endpoint and key in LLM settings, or update the share key first.',
                        '本机没有与该分享密钥匹配的服务商，无法探测模型。请在大模型配置里保留相同的地址和密钥，或先更新分享密钥。',
                        '本機沒有與該分享密鑰匹配的服務商，無法探測模型。請在大模型配置裡保留相同的位址和密鑰，或先更新分享密鑰。',
                    ),
                    t('Adjust models', '调整模型', '調整模型'),
                );
                return;
            }
            const matched = shareable[index];
            const credential = tokenBankShareCredential(matched);
            const provider: LLMProvider = {
                ...matched,
                name: matched.name.trim(),
                url: matched.url.trim(),
                key: credential,
                protocol: String(matched.protocol || share.protocol || '').trim(),
            };
            const discovered = await listProviderModelsForShare(provider);
            const raw = await TokenBankListShareModels(share.id, 'all');
            const current = extractTokenBankModels(raw).filter((model) => model.model_name);
            const enabled = current.filter((model) => model.enabled).map((model) => model.model_name);
            const models = unionShareModelNames(discovered, enabled);
            if (models.length === 0) {
                await showAlert(
                    t('No models were found for this provider.', '未发现该服务商下的模型。', '未發現該服務商下的模型。'),
                    t('Adjust models', '调整模型', '調整模型'),
                );
                return;
            }
            const windows: Record<string, TokenBankShareWindow> = {};
            for (const model of current) {
                if (model.share_window) windows[model.model_name] = model.share_window;
            }
            setAdjustShare({ shareID: share.id, provider, models, enabled, windows });
        } catch (err) {
            notify(err instanceof Error ? err.message : String(err));
        } finally {
            setBusyKey('');
        }
    }, [busyKey, notify, showAlert, t]);

    const togglePaused = useCallback(async (share: TokenBankShare) => {
        if (busyKey) return;
        const live = isShareLive(share);
        setBusyKey(`pause:${share.id}`);
        try {
            await TokenBankSetSharePaused(share.id, live);
            notify(live
                ? t('Share paused. Its models stop receiving traffic.', '已暂停分享，其模型不再接收请求。', '已暫停分享，其模型不再接收請求。')
                : t('Share resumed.', '分享已恢复。', '分享已恢復。'));
            await refresh();
        } catch (err) {
            notify(err instanceof Error ? err.message : String(err));
        } finally {
            setBusyKey('');
        }
    }, [busyKey, notify, refresh, t]);

    const takeOut = useCallback(async (share: TokenBankShare) => {
        if (busyKey) return;
        const copy = takeOutShareDialog(lang, share.display_name, share.id);
        const ok = await showConfirm(
            copy.message,
            copy.title,
            {
                confirmText: t('Take out', '取出', '取出'),
                cancelText: t('Cancel', '取消', '取消'),
                confirmVariant: 'danger',
            },
        );
        if (!ok) return;
        setBusyKey(`takeout:${share.id}`);
        try {
            await TokenBankTakeOutShare(share.id);
            notify(t('Share taken out.', '分享已取出。', '分享已取出。'));
            await refresh();
        } catch (err) {
            notify(err instanceof Error ? err.message : String(err));
        } finally {
            setBusyKey('');
        }
    }, [busyKey, lang, notify, refresh, showConfirm, t]);

    const rotateKey = useCallback(async (share: TokenBankShare) => {
        if (busyKey) return;
        const entered = await showPrompt(
            t(
                'Paste the new provider key. This share keeps its id and the credits it has already earned.',
                '粘贴新的服务商密钥。这个分享会保留编号和已经赚到的积分。',
                '貼上新的服務商密鑰。這個分享會保留編號和已經賺到的積分。',
            ),
            t('Update key', '更新密钥', '更新密鑰'),
            { placeholder: t('New provider key', '新的服务商密钥', '新的服務商密鑰') },
        );
        const next = (entered || '').trim();
        if (!next) return;
        setBusyKey(`rotate:${share.id}`);
        try {
            const fingerprint = await fingerprintProviderKey(next, share.api_url);
            await TokenBankRotateShareKey(share.id, share.api_url, next, share.protocol, fingerprint);
            notify(t('Key updated. Earned credits stay on this share.', '密钥已更新，已赚积分仍记在这个分享上。', '密鑰已更新，已賺積分仍記在這個分享上。'));
            await refresh();
        } catch (err) {
            notify(err instanceof Error ? err.message : String(err));
        } finally {
            setBusyKey('');
        }
    }, [busyKey, notify, refresh, showPrompt, t]);

    const addKey = useCallback(async (share: TokenBankShare) => {
        if (busyKey) return;
        const entered = await showPrompt(
            t(
                'Paste another provider key. Calls rotate across the keys already on this share.',
                '粘贴另一把服务商密钥。调用会在这个分享已有的密钥之间轮换。',
                '貼上另一把服務商密鑰。呼叫會在這個分享已有的密鑰之間輪換。',
            ),
            t('Add a key', '追加密钥', '追加密鑰'),
            { placeholder: t('Additional provider key', '追加的服务商密钥', '追加的服務商密鑰') },
        );
        const next = (entered || '').trim();
        if (!next) return;
        setBusyKey(`addkey:${share.id}`);
        try {
            const fingerprint = await fingerprintProviderKey(next, share.api_url);
            await TokenBankAddShareKey(share.id, share.api_url, next, share.protocol, fingerprint);
            notify(t('Key added. Calls rotate across the keys on this share.', '密钥已追加，调用会在这些密钥之间轮换。', '密鑰已追加，呼叫會在這些密鑰之間輪換。'));
            await refresh();
        } catch (err) {
            notify(err instanceof Error ? err.message : String(err));
        } finally {
            setBusyKey('');
        }
    }, [busyKey, notify, refresh, showPrompt, t]);

    const changeAccess = useCallback((share: TokenBankShare) => {
        if (busyKey) return;
        setAccessShare(share);
    }, [busyKey]);

    const withdrawAll = useCallback(async () => {
        if (busyKey) return;
        const available = summary?.available_micro || 0;
        if (available <= 0) {
            notify(t('There are no credits available to withdraw.', '没有可提取的积分。', '沒有可提取的積分。'));
            return;
        }
        const ok = await showConfirm(
            t(
                `All ${formatCreditsGrouped(available, 2)} available credits will be moved into this Hub's grant pool. Credits cannot leave the platform.`,
                `将把全部 ${formatCreditsGrouped(available, 2)} 可用积分提取到本机的授权额度。积分不能离开平台。`,
                `將把全部 ${formatCreditsGrouped(available, 2)} 可用積分提取到本機的授權額度。積分不能離開平台。`,
            ),
            t('Withdraw to this machine?', '提取到本机？', '提取到本機？'),
            {
                confirmText: t('Withdraw', '提取', '提取'),
                cancelText: t('Cancel', '取消', '取消'),
            },
        );
        if (!ok) return;
        setBusyKey('withdraw');
        try {
            // manual=true is what makes taking the whole balance legal; the
            // server caps an automatic top-up at 1/N so one machine cannot
            // drain a user with several hubs (E6).
            if (!withdrawRequestID.current) {
                withdrawRequestID.current = newWithdrawRequestID();
            }
            await TokenBankWithdraw(withdrawRequestID.current, available, true);
            withdrawRequestID.current = '';
            notify(t('Credits withdrawn to this machine.', '积分已提取到本机。', '積分已提取到本機。'));
            await refresh();
        } catch (err) {
            notify(err instanceof Error ? err.message : String(err));
        } finally {
            setBusyKey('');
        }
    }, [busyKey, notify, refresh, showConfirm, summary, t]);

    const createGift = useCallback(async () => {
        if (busyKey) return;
        const micro = creditsToMicro(giftAmount);
        // One reason, named. The server re-checks all of this inside the freeze
        // transaction; predicting it here just means the user is told before
        // pressing the button rather than after.
        const rejection = giftAmountRejection(micro, summary);
        if (rejection === 'empty') {
            await showAlert(
                t('Enter a positive number of credits.', '请输入大于 0 的积分。', '請輸入大於 0 的積分。'),
                t('Gift credits', '转赠积分', '轉贈積分'),
            );
            return;
        }
        if (rejection === 'no_balance') {
            await showAlert(
                t('You have no credits available to gift.', '你没有可转赠的积分。', '你沒有可轉贈的積分。'),
                t('Gift credits', '转赠积分', '轉贈積分'),
            );
            return;
        }
        if (rejection === 'below_minimum') {
            await showAlert(
                t(
                    `One link must gift at least ${formatCredits((summary?.min_share_credits || 0) * MICROCREDITS_PER_CREDIT)} credits.`,
                    `单条链接至少需转赠 ${formatCredits((summary?.min_share_credits || 0) * MICROCREDITS_PER_CREDIT)} 积分。`,
                    `單條連結至少需轉贈 ${formatCredits((summary?.min_share_credits || 0) * MICROCREDITS_PER_CREDIT)} 積分。`,
                ),
                t('Gift credits', '转赠积分', '轉贈積分'),
            );
            return;
        }
        if (rejection === 'over_cap') {
            await showAlert(
                t(
                    `One link can gift at most ${formatCredits(summary?.gift_share_cap_micro || 0)} credits.`,
                    `单条链接最多可转赠 ${formatCredits(summary?.gift_share_cap_micro || 0)} 积分。`,
                    `單條連結最多可轉贈 ${formatCredits(summary?.gift_share_cap_micro || 0)} 積分。`,
                ),
                t('Gift credits', '转赠积分', '轉贈積分'),
            );
            return;
        }
        // Every non-null rejection returned above, so micro is a positive
        // number here. TypeScript cannot see that through the `rejection`
        // union, so narrow it explicitly rather than with `?? 0`, which would
        // turn a future logic slip into a silent zero-credit link.
        if (micro == null || micro <= 0) return;
        const parts = wholeCreditsOrMicro(micro);
        setBusyKey('gift-create');
        try {
            const raw = await TokenBankCreateGiftLink(parts.credits, parts.creditsMicro);
            setCreatedGift(normalizeTokenBankGiftLink(raw));
            setGiftOpen(false);
            setGiftAmount('');
            notify(t('Gift link created. The code is shown once.', '转赠链接已生成。兑换码只显示这一次。', '轉贈連結已生成。兌換碼只顯示這一次。'));
            await refresh();
        } catch (err) {
            notify(err instanceof Error ? err.message : String(err));
        } finally {
            setBusyKey('');
        }
    }, [busyKey, giftAmount, notify, refresh, showAlert, summary, t]);

    const revokeGift = useCallback(async (link: TokenBankGiftLink) => {
        if (busyKey || !giftSenderCanRevoke(link.status)) return;
        const amount = formatCreditsGrouped(link.credits_micro, 2);
        const claimed = link.status === 'claimed';
        const ok = await showConfirm(
            claimed
                ? t(
                    `${amount} credits have not been withdrawn. Revoking unfreezes them, and the recipient can no longer withdraw them.`,
                    `对方尚未提取。撤销后将解冻 ${amount} 积分，对方不能再提取。`,
                    `對方尚未提取。撤銷後將解凍 ${amount} 積分，對方不能再提取。`,
                )
                : t(
                    `${amount} credits will be unfrozen. The link will stop working.`,
                    `将解冻 ${amount} 积分。该链接将失效。`,
                    `將解凍 ${amount} 積分。該連結將失效。`,
                ),
            claimed
                ? t('Revoke this gift?', '撤销这笔转赠？', '撤銷這筆轉贈？')
                : t('Revoke this link?', '撤销这条链接？', '撤銷這條連結？'),
            {
                confirmText: t('Revoke', '撤销', '撤銷'),
                cancelText: t('Cancel', '取消', '取消'),
                confirmVariant: 'danger',
            },
        );
        if (!ok) return;
        setBusyKey(`gift-revoke:${link.id}`);
        try {
            await TokenBankRevokeGiftLink(link.id);
            if (createdGift?.id === link.id) setCreatedGift(null);
            notify(claimed
                ? t('Gift revoked. The credits are unfrozen.', '转赠已撤销，积分已解冻。', '轉贈已撤銷，積分已解凍。')
                : t('Link revoked.', '链接已撤销。', '連結已撤銷。'));
            await refresh();
        } catch (err) {
            notify(giftActionError(t, err, 'revoke'));
            // The other side may have withdrawn while this dialog was open.
            // Leave the card on the status the server has now.
            try {
                await refresh();
            } catch {
                // The revoke error is already on screen.
            }
        } finally {
            setBusyKey('');
        }
    }, [busyKey, createdGift, notify, refresh, showConfirm, t]);

    const giftBlockAlertTitle = useCallback((block: ReturnType<typeof giftClaimBlock>) => {
        if (block === 'held') return t('Withdraw this gift', '提取这份转赠', '提取這份轉贈');
        if (block === 'withdrawn') return t('Already withdrawn', '已经提取', '已經提取');
        return t('Cannot claim', '不能领取', '不能領取');
    }, [t]);

    const giftBlockMessage = useCallback((preview: TokenBankGiftPreview | null): string | null => {
        switch (giftClaimBlock(preview)) {
            case 'held':
                return t(
                    'You already claimed this gift. Withdraw it to this machine to receive the credits.',
                    '你已经领取了这份转赠。提取到本机后，积分才会入账。',
                    '你已經領取了這份轉贈。提取到本機後，積分才會入帳。',
                );
            case 'withdrawn':
                return t(
                    'You already withdrew this gift to this machine.',
                    '你已经把这份转赠提取到本机。',
                    '你已經把這份轉贈提取到本機。',
                );
            case 'settled':
                return t(
                    'Someone already withdrew this gift to their machine.',
                    '对方已经把这份转赠提取到本机。',
                    '對方已經把這份轉贈提取到本機。',
                );
            case 'claimed':
                return t('Someone has already claimed this gift.', '这份转赠已经被领取。', '這份轉贈已經被領取。');
            case 'revoked':
                return t('The sender revoked this gift.', '发送方已撤销这份转赠。', '發送方已撤銷這份轉贈。');
            case 'expired':
                return t('This gift has expired. The credits were returned.', '这份转赠已过期，积分已退回。', '這份轉贈已過期，積分已退回。');
            case 'closed':
                return giftClaimClosedText(t);
            case 'unavailable':
                return t('This gift cannot be claimed.', '这份转赠现在不能领取。', '這份轉贈現在不能領取。');
            default:
                return null;
        }
    }, [t]);

    const previewGift = useCallback(async () => {
        if (busyKey) return;
        const code = giftCodeFromInput(claimInput);
        if (!code) {
            await showAlert(
                t('Paste a gift code or link.', '请粘贴兑换码或链接。', '請貼上兌換碼或連結。'),
                t('Claim a gift', '领取转赠', '領取轉贈'),
            );
            return;
        }
        const gen = ++lookupGen.current;
        setBusyKey('gift-preview');
        setGiftNotice({ tone: 'info', text: t('Checking this gift…', '正在查验…', '正在查驗…') });
        try {
            const raw = await TokenBankPreviewGiftLink(code);
            if (lookupGen.current !== gen) return;
            const preview = normalizeTokenBankGiftPreview(raw);
            if (giftCodeFromInput(claimInputRef.current).toLowerCase() !== preview.code.toLowerCase()) {
                setGiftNotice(null);
                return;
            }
            const block = giftClaimBlock(preview);
            rememberGiftPreview(preview);
            // The preview line already names the sender, the amount, and
            // whether it can be claimed. A notice with that same sentence
            // draws a second banner under it.
            setGiftNotice(null);
            const blocked = giftBlockMessage(preview);
            // A held gift already shows its withdraw button. A dialog titled
            // with that action would not withdraw, so it is not opened here.
            if (block === 'withdrawn') {
                await showAlert(blocked || '', giftBlockAlertTitle('withdrawn'));
            } else if (blocked && block !== 'held') {
                await showAlert(blocked, giftBlockAlertTitle(block));
            }
        } catch (err) {
            if (lookupGen.current !== gen) return;
            if (giftCodeFromInput(claimInputRef.current).toLowerCase() !== code.toLowerCase()) {
                setGiftNotice(null);
                return;
            }
            setGiftPreview(null);
            const text = err instanceof Error ? err.message : String(err);
            setGiftNotice({ tone: 'err', text });
            await showAlert(text, t('Preview failed', '查验失败', '查驗失敗'));
        } finally {
            // Release this lookup even when a newer one owns the line. Gating
            // the release on the generation left the key set: the newer open
            // does not take it, and claim, preview, and withdraw then all no-op.
            setBusyKey((current) => (current === 'gift-preview' ? '' : current));
        }
    }, [busyKey, claimInput, giftBlockAlertTitle, giftBlockMessage, rememberGiftPreview, showAlert, t]);

    const claimGift = useCallback(async () => {
        if (busyKey) return;
        // A cleared box drops the lookup in onChange. The fallback covers a
        // lookup that is still on screen with an empty box.
        const code = giftCodeFromInput(claimInput) || giftPreview?.code || '';
        if (!code) {
            await showAlert(
                t('Paste a gift code or link.', '请粘贴兑换码或链接。', '請貼上兌換碼或連結。'),
                t('Claim a gift', '领取转赠', '領取轉贈'),
            );
            return;
        }
        const previewCode = (giftPreview?.code || '').toLowerCase();
        const samePreview = previewCode !== '' && code.toLowerCase() === previewCode;
        const block = samePreview ? giftClaimBlock(giftPreview) : null;
        const blocked = block ? giftBlockMessage(giftPreview) : null;
        // The withdraw button is the remaining step. Claiming again does not
        // move the credits, and a dialog titled as that button would not either.
        if (block === 'held' && giftPreview?.id) {
            setClaimedGift(giftLinkFromPreview(giftPreview));
            setGiftNotice(null);
            return;
        }
        // A known dead link has nothing to send. Say why, instead of a request
        // whose failure only used to flash a toast.
        if (blocked && block !== 'unavailable') {
            setGiftNotice(null);
            await showAlert(blocked, giftBlockAlertTitle(block));
            return;
        }
        if (!samePreview) setGiftPreview(null);
        const gen = ++lookupGen.current;
        setBusyKey('gift-claim');
        setGiftNotice({ tone: 'info', text: t('Claiming this gift…', '正在领取…', '正在領取…') });
        // True only while this claim is still the lookup under the field.
        // A newer lookup, or a different code typed while this request is in
        // flight, owns that line. The claim itself still happened.
        const ownsLine = () => lookupGen.current === gen
            && giftCodeFromInput(claimInputRef.current).toLowerCase() === code.toLowerCase();
        try {
            const raw = await TokenBankClaimGiftLink(code);
            const link = normalizeTokenBankGiftLink(raw);
            setClaimedGift(link);
            // Drop the "can be claimed" line for this code only. A preview that
            // arrived for a different code stays.
            setGiftPreview((current) => (
                current && current.code.toLowerCase() === code.toLowerCase() ? null : current
            ));
            const resumed = Boolean((raw as { resume?: boolean }).resume);
            const text = resumed
                ? t(
                    'You already claimed this gift. Withdraw it to this machine to receive the credits.',
                    '你已经领取了这份转赠。提取到本机后，积分才会入账。',
                    '你已經領取了這份轉贈。提取到本機後，積分才會入帳。',
                )
                : t(
                    'Claimed. Withdraw the gift to this machine to receive the credits.',
                    '已领取。把这份转赠提取到本机后，积分才会入账。',
                    '已領取。把這份轉贈提取到本機後，積分才會入帳。',
                );
            if (ownsLine()) {
                setGiftNotice({ tone: 'ok', text });
            } else if (lookupGen.current === gen) {
                setGiftNotice(null);
            }
            await refresh();
            await showAlert(
                text,
                resumed ? t('Already claimed', '已经领取', '已經領取') : t('Claimed', '领取成功', '領取成功'),
            );
        } catch (err) {
            const text = giftActionError(t, err, 'claim');
            if (ownsLine()) {
                setGiftNotice({ tone: 'err', text });
            } else if (lookupGen.current === gen) {
                setGiftNotice(null);
            }
            // A sender can revoke between preview and this call. Reload so a
            // held card from the earlier lookup does not keep offering it.
            if (giftErrorIsRevoked(err)) await refresh();
            await showAlert(text, t('Claim failed', '领取失败', '領取失敗'));
        } finally {
            setBusyKey((current) => (current === 'gift-claim' ? '' : current));
        }
    }, [busyKey, claimInput, giftBlockAlertTitle, giftBlockMessage, giftPreview, refresh, showAlert, t]);

    const withdrawGiftLink = useCallback(async (link: TokenBankGiftLink | null) => {
        if (giftWithdrawBusy.current || busyKey || !link?.id || link.credits_micro <= 0) return;
        // Hold the flow through the confirm. The ref closes a second click
        // before React applies busyKey, so two confirms cannot both withdraw.
        giftWithdrawBusy.current = true;
        setBusyKey('gift-withdraw');
        try {
            const ok = await showConfirm(
                t(
                    `${formatCreditsGrouped(link.credits_micro, 2)} gifted credits will move into this Hub's grant pool.`,
                    `将把获赠的 ${formatCreditsGrouped(link.credits_micro, 2)} 积分提取到本机的授权额度。`,
                    `將把獲贈的 ${formatCreditsGrouped(link.credits_micro, 2)} 積分提取到本機的授權額度。`,
                ),
                t('Withdraw this gift?', '提取这份转赠？', '提取這份轉贈？'),
                {
                    confirmText: t('Withdraw this gift', '提取这份转赠', '提取這份轉贈'),
                    cancelText: t('Cancel', '取消', '取消'),
                },
            );
            if (!ok) return;
            const requestIDs = giftWithdrawIDs.current;
            const requestID = requestIDs[link.id] || (requestIDs[link.id] = newWithdrawRequestID());
            setGiftNotice({ tone: 'info', text: t('Withdrawing this gift…', '正在提取这份转赠…', '正在提取這份轉贈…') });
            await TokenBankWithdrawGift(requestID, link.id, link.credits_micro);
            // Keep requestID so a retry of this gift replays it. The id is also
            // settled, so a stale list or a late lookup cannot offer it again.
            settledGiftIDs.current.add(link.id);
            setHeldGifts((current) => current.filter((item) => item.id !== link.id));
            setClaimedGift((current) => (current?.id === link.id ? null : current));
            // The lookup line still says this gift is waiting to be withdrawn.
            setGiftPreview((current) => (current?.id === link.id ? null : current));
            const text = t('Gift withdrawn to this machine.', '转赠积分已提取到本机。', '轉贈積分已提取到本機。');
            setGiftNotice({ tone: 'ok', text });
            await refresh();
            await showAlert(text, t('Withdrawn', '提取成功', '提取成功'));
        } catch (err) {
            const text = giftActionError(t, err, 'withdraw');
            if (giftErrorIsRevoked(err) && link?.id) {
                const dropped = link.id;
                setHeldGifts((current) => current.filter((item) => item.id !== dropped));
                setClaimedGift((current) => (current?.id === dropped ? null : current));
                setGiftPreview((current) => (current?.id === dropped ? null : current));
                await refresh();
            }
            setGiftNotice({ tone: 'err', text });
            await showAlert(text, t('Withdrawal failed', '提取失败', '提取失敗'));
        } finally {
            giftWithdrawBusy.current = false;
            setBusyKey((current) => (current === 'gift-withdraw' ? '' : current));
        }
    }, [busyKey, refresh, showAlert, showConfirm, t]);

    const copyClaimLink = useCallback(async (value: string) => {
        if (!value) return;
        try {
            await navigator.clipboard.writeText(value);
            notify(t('Link copied.', '链接已复制。', '連結已複製。'));
        } catch (err) {
            notify(err instanceof Error ? err.message : value);
        }
    }, [notify, t]);

    const summaryCards = useMemo(() => {
        const s = summary;
        if (!s) return [] as Array<{ label: string; value: string; hint?: string }>;
        return [
            { label: t('Available', '可用', '可用'), value: formatCreditsGrouped(s.available_micro, 2) },
            { label: t('Earned', '已赚', '已賺'), value: formatCreditsGrouped(s.earned_micro, 2) },
            { label: t('Received', '已收', '已收'), value: formatCreditsGrouped(s.received_micro, 2) },
            { label: t('Withdrawn', '已提取', '已提取'), value: formatCreditsGrouped(s.withdrawn_micro, 2) },
            { label: t('Gifted away', '已转赠', '已轉贈'), value: formatCreditsGrouped(s.granted_micro, 2) },
            { label: t('Frozen', '冻结中', '凍結中'), value: formatCreditsGrouped(s.frozen_micro, 2) },
        ];
    }, [summary, t]);

    const renderModelRow = (model: TokenBankShareModel) => {
        const usage = model.usage;
        const earned = usage ? usage.net_micro : model.earned_micro;
        const windowLabel = formatModelShareWindow(model.share_window, t);
        const removed = model.enabled === false;
        return (
            <li className="tbk-model" key={model.id || model.model_name}>
                <span className="tbk-model__name">{model.model_name}</span>
                <span className="tbk-meta">
                    {model.tier || 'mid'} · ×{model.tier_multiplier || 1}
                    {windowLabel ? ` · ${windowLabel}` : ''}
                </span>
                <span className={`tbk-pill ${model.available && !removed ? 'tbk-pill--on' : 'tbk-pill--off'}`}>
                    {removed
                        ? t('Removed', '已移出', '已移出')
                        : model.available ? t('Available', '可用', '可用') : t('Unavailable', '不可用', '不可用')}
                </span>
                <span className="tbk-model__earned">{formatCreditsGrouped(earned, 2)}</span>
                {usage ? (
                    <div className="tbk-meta" data-testid={`tbk-model-calc-${model.model_name}`}>
                        {usage.calls > 0 ? (
                            <>
                                <span>
                                    {t(
                                        `Consumed ${formatCreditsGrouped(usage.gross_micro, 4)} · fee ${formatCreditsGrouped(usage.fee_micro, 4)} · you earn ${formatCreditsGrouped(usage.net_micro, 4)} · buyer paid ${formatCreditsGrouped(usage.charged_micro, 4)}`,
                                        `消耗 ${formatCreditsGrouped(usage.gross_micro, 4)} · 手续费 ${formatCreditsGrouped(usage.fee_micro, 4)} · 您获得 ${formatCreditsGrouped(usage.net_micro, 4)} · 消费者实付 ${formatCreditsGrouped(usage.charged_micro, 4)}`,
                                        `消耗 ${formatCreditsGrouped(usage.gross_micro, 4)} · 手續費 ${formatCreditsGrouped(usage.fee_micro, 4)} · 您獲得 ${formatCreditsGrouped(usage.net_micro, 4)} · 消費者實付 ${formatCreditsGrouped(usage.charged_micro, 4)}`,
                                    )}
                                </span>
                                <span>
                                    {t(
                                        `${usage.calls} calls · in ${Math.trunc(usage.input_tokens).toLocaleString()} · out ${Math.trunc(usage.output_tokens).toLocaleString()}`,
                                        `${usage.calls} 次 · 输入 ${Math.trunc(usage.input_tokens).toLocaleString()} · 输出 ${Math.trunc(usage.output_tokens).toLocaleString()}`,
                                        `${usage.calls} 次 · 輸入 ${Math.trunc(usage.input_tokens).toLocaleString()} · 輸出 ${Math.trunc(usage.output_tokens).toLocaleString()}`,
                                    )}
                                </span>
                                {usage.clamped_calls > 0 ? (
                                    <span>
                                        {t(
                                            'Some calls paid less because the group price was lower than the share.',
                                            '有调用因服务组卖价更低，所得被压到不超过实付。',
                                            '有呼叫因服務組賣價更低，所得被壓到不超過實付。',
                                        )}
                                    </span>
                                ) : null}
                            </>
                        ) : (
                            <span>{t('No settled calls in this window.', '这个区间还没有结算。', '這個區間還沒有結算。')}</span>
                        )}
                    </div>
                ) : null}
            </li>
        );
    };

    const heldLookup = giftPreview && giftClaimBlock(giftPreview) === 'held' && giftPreview.id
        ? giftLinkFromPreview(giftPreview)
        : null;
    const heldOnForm = Boolean(heldLookup && !heldGifts.some((link) => link.id === heldLookup.id));
    // Preview, claim, and withdraw share one result line. A second click while
    // one of them is in flight used to return without doing anything, and the
    // finishing request then painted its sentence under the newer code.
    const giftFlowBusy = busyKey === 'gift-preview' || busyKey === 'gift-claim' || busyKey === 'gift-withdraw';

    return (
        <div className="tbk-panel">
            <div className="tbk-panel__head">
                <div>
                    <h3>{t('Token Bank', 'Token 银行', 'Token 銀行')}</h3>
                    {summary && (summary.rank > 0 || summary.rank_badge || summary.lifetime_badge) ? (
                        <p className="tbk-meta">
                            {summary.rank > 0 ? <span>#{summary.rank}</span> : null}
                            {summary.rank_badge ? (
                                <span className="tbk-pill tbk-pill--on">{badgeText(t, summary.rank_badge)}</span>
                            ) : null}
                            {summary.lifetime_badge ? (
                                <span className="tbk-pill tbk-pill--off">{badgeText(t, summary.lifetime_badge)}</span>
                            ) : null}
                        </p>
                    ) : null}
                    <p className="tbk-panel__sub">
                        {t(
                            'Credits earned by sharing your models. Pull them onto this machine and the assistant spends them. They cannot buy compute cards and there is no cash withdrawal.',
                            '共享模型赚取的积分。提取后由助手使用，不能购买算力卡，不可提现。',
                            '共享模型賺取的積分。提取後由助手使用，不能購買算力卡，不可提現。',
                        )}
                    </p>
                </div>
                <div className="tbk-panel__actions">
                    <button className="btn-secondary" type="button" onClick={() => void refresh()} disabled={loading}>
                        {t('Refresh', '刷新', '重新整理')}
                    </button>
                    <button className="btn-secondary" type="button" onClick={() => setGiftOpen((open) => !open)} disabled={loading}>
                        {t('Gift credits', '转赠积分', '轉贈積分')}
                    </button>
                    <button className="btn-primary" type="button" onClick={() => void withdrawAll()} disabled={loading || busyKey === 'withdraw' || !summary?.available_micro}>
                        {t('Withdraw to this machine', '提取到本机', '提取到本機')}
                    </button>
                </div>
            </div>

            {error ? <div className="tbk-error">{error}</div> : null}

            <section className="tbk-summary">
                {summaryCards.map((card) => (
                    <div className="tbk-summary__card" key={card.label}>
                        <span className="tbk-summary__label">{card.label}</span>
                        <strong className="tbk-summary__value">{card.value}</strong>
                    </div>
                ))}
            </section>

            {giftOpen ? (
                <form className="tbk-gift-form" onSubmit={(event) => { event.preventDefault(); void createGift(); }}>
                    <p className="tbk-gift-note">
                        {t(
                            `Available ${formatCreditsGrouped(summary?.available_micro || 0, 2)}. One link can gift at most ${formatCredits(summary?.gift_share_cap_micro || 0)}. Only the first person to claim it receives the credits.`,
                            `可用 ${formatCreditsGrouped(summary?.available_micro || 0, 2)}。单条链接最多可转赠 ${formatCredits(summary?.gift_share_cap_micro || 0)}。只有第一个领取的人能拿到。`,
                            `可用 ${formatCreditsGrouped(summary?.available_micro || 0, 2)}。單條連結最多可轉贈 ${formatCredits(summary?.gift_share_cap_micro || 0)}。只有第一個領取的人能拿到。`,
                        )}
                    </p>
                    <div className="tbk-gift-form__row">
                        <input
                            aria-label={t('Credits to gift', '要转赠的积分', '要轉贈的積分')}
                            inputMode="decimal"
                            value={giftAmount}
                            onChange={(event) => setGiftAmount(event.target.value)}
                        />
                        <button
                            className="btn-secondary"
                            type="button"
                            onClick={() => setGiftAmount(formatCredits(summary?.gift_share_cap_micro || 0, 6))}
                        >
                            {t('Gift the maximum', '全部可转赠', '全部可轉贈')}
                        </button>
                        <button className="btn-primary" type="submit" disabled={busyKey === 'gift-create'}>
                            {t('Create link', '生成链接', '生成連結')}
                        </button>
                    </div>
                </form>
            ) : null}

            {createdGift && giftClaimTarget(createdGift) ? (
                <div className="tbk-gift-ticket" data-testid="tbk-gift-ticket">
                    <strong>
                        {t(
                            `Gift ${formatCreditsGrouped(createdGift.credits_micro, 2)} credits`,
                            `转赠 ${formatCreditsGrouped(createdGift.credits_micro, 2)} 积分`,
                            `轉贈 ${formatCreditsGrouped(createdGift.credits_micro, 2)} 積分`,
                        )}
                    </strong>
                    <div className="tbk-gift-ticket__linkrow">
                        <span className="tbk-gift-ticket__url">{giftClaimTarget(createdGift)}</span>
                        <button className="btn-secondary" type="button" onClick={() => void copyClaimLink(giftClaimTarget(createdGift))}>
                            {t('Copy link', '复制链接', '複製連結')}
                        </button>
                    </div>
                    <div className="tbk-gift-ticket__qr">
                        <QRCodeSVG
                            value={giftClaimTarget(createdGift)}
                            size={128}
                            level="M"
                            bgColor="var(--theme-surface)"
                            fgColor="var(--theme-text-primary)"
                        />
                    </div>
                    <p className="tbk-gift-note">
                        {t(
                            'An unclaimed link returns the credits when it expires. The code is not shown again after you leave this screen.',
                            '未被领取的链接到期后会退回积分。离开此页后兑换码不再显示。',
                            '未被領取的連結到期後會退回積分。離開此頁後兌換碼不再顯示。',
                        )}
                    </p>
                </div>
            ) : null}

            <TokenBankShareableProviders
                lang={lang}
                reloadToken={providerReload}
                showToastMessage={showToastMessage}
                onRequestVerification={onRequestVerification}
                onShared={() => { void refresh(); }}
            />

            <section className="tbk-section tbk-section--gifts">
                <h4>{t('Gift links I sent', '我发出的转赠链接', '我發出的轉贈連結')}</h4>
                {!gifts.length ? (
                    <div className="tbk-empty">
                        {loadedOnce
                            ? t('You have not sent a gift link yet.', '你还没有发出转赠链接。', '你還沒有發出轉贈連結。')
                            : t('Loading…', '加载中…', '載入中…')}
                    </div>
                ) : (
                    <ul className="tbk-gift-grid" data-testid="tbk-gift-list">
                        {gifts.map((link) => {
                            const recipient = giftRecipient(link);
                            const held = link.status === 'claimed' || link.status === 'settled' || ((link.status === 'expired' || link.status === 'revoked') && !!recipient);
                            const returnPassed = link.status === 'claimed' && giftInstantPassed(link.expires_at);
                            // Claim itself checks the timestamp. An active link
                            // past that time can no longer be claimed, even
                            // while the sweeper has not returned the credits.
                            const claimClosed = link.status === 'active' && giftInstantPassed(link.expires_at);
                            const label = claimClosed
                                ? GIFT_PENDING_RETURN_LABEL
                                : (GIFT_STATUS_LABEL[link.status] || [link.status || '—', link.status || '—', link.status || '—']);
                            return (
                                <li className="tbk-share" key={link.id} data-testid={`tbk-gift-${link.id}`}>
                                    <div className="tbk-share__head">
                                        <span className="tbk-share__name">{formatCreditsGrouped(link.credits_micro, 2)}</span>
                                        <span className={`tbk-pill ${giftPillClass(link.status, claimClosed)}`}>
                                            {textForLang(lang, label[0], label[1], label[2])}
                                        </span>
                                    </div>
                                    {held ? (
                                        <div className="tbk-gift-claim-detail" data-testid={`tbk-gift-claim-${link.id}`}>
                                            <span className="tbk-gift-claim-detail__who">
                                                {t('Recipient', '领取人', '領取人')}
                                                {' '}
                                                {recipient || t('Unknown', '未知', '未知')}
                                            </span>
                                            {link.claimed_at ? (
                                                <span>{t('Claimed at', '领取时间', '領取時間')} {formatGiftInstant(link.claimed_at)}</span>
                                            ) : null}
                                            <span className={returnPassed ? 'tbk-gift-claim-detail__state--due' : undefined}>
                                                {link.status === 'settled'
                                                    ? t('Withdrawn to their machine.', '对方已提取到本机。', '對方已提取到本機。')
                                                    : link.status === 'expired'
                                                        ? t(
                                                            'They did not withdraw it. The credits were returned.',
                                                            '对方未提取，积分已退回。',
                                                            '對方未提取，積分已退回。',
                                                        )
                                                        : link.status === 'revoked'
                                                            ? t(
                                                                'You revoked this gift. The credits were returned.',
                                                                '你已撤销这笔转赠，积分已退回。',
                                                                '你已撤銷這筆轉贈，積分已退回。',
                                                            )
                                                            : returnPassed
                                                            ? t(
                                                                'The return time has passed and the credits are not back yet. They can still withdraw until the credits are returned.',
                                                                '已过退回时间，积分尚未退回。积分退回前对方仍可提取。',
                                                                '已過退回時間，積分尚未退回。積分退回前對方仍可提取。',
                                                            )
                                                            : t(
                                                                'Credits stay frozen until they withdraw.',
                                                                '积分仍冻结，对方提取到本机前不会转出。',
                                                                '積分仍凍結，對方提取到本機前不會轉出。',
                                                            )}
                                            </span>
                                            {link.status === 'claimed' && link.expires_at && !returnPassed ? (
                                                <span>{t('Returns at', '退回时间', '退回時間')} {formatGiftInstant(link.expires_at)}</span>
                                            ) : null}
                                        </div>
                                    ) : link.expires_at ? (
                                        <div className="tbk-meta">
                                            {claimClosed ? (
                                                <span className="tbk-gift-claim-detail__state--due">
                                                    {giftClaimClosedText(t)}
                                                </span>
                                            ) : (
                                                <span>{t('Expires', '到期', '到期')} {formatGiftInstant(link.expires_at)}</span>
                                            )}
                                        </div>
                                    ) : null}
                                    {giftSenderCanRevoke(link.status) ? (
                                        <div className="tbk-share__actions">
                                            <button className="btn-link tbk-danger" type="button" onClick={() => void revokeGift(link)} disabled={busyKey === `gift-revoke:${link.id}`}>
                                                {t('Revoke', '撤销', '撤銷')}
                                            </button>
                                        </div>
                                    ) : null}
                                </li>
                            );
                        })}
                    </ul>
                )}
            </section>

            <section className="tbk-section">
                <h4>{t('Claim a gift', '领取转赠', '領取轉贈')}</h4>
                {heldGifts.length ? (
                    <ul className="tbk-list" data-testid="tbk-held-gifts">
                        {heldGifts.map((link) => (
                            <li className="tbk-share" key={link.id}>
                                <div className="tbk-share__head">
                                    <span className="tbk-share__name">{formatCreditsGrouped(link.credits_micro, 2)}</span>
                                    <span className="tbk-pill tbk-pill--wait">{t('Not withdrawn', '未提取', '未提取')}</span>
                                </div>
                                <p className="tbk-gift-note">
                                    {link.sender_masked
                                        ? t(
                                            `${link.sender_masked} gifted this. Withdraw it to this machine to receive the credits.`,
                                            `${link.sender_masked} 转赠了这笔积分。提取到本机后才会入账。`,
                                            `${link.sender_masked} 轉贈了這筆積分。提取到本機後才會入帳。`,
                                        )
                                        : t(
                                            'You already claimed this gift. Withdraw it to this machine to receive the credits.',
                                            '你已经领取了这份转赠。提取到本机后，积分才会入账。',
                                            '你已經領取了這份轉贈。提取到本機後，積分才會入帳。',
                                        )}
                                </p>
                                {link.expires_at ? (
                                    giftInstantPassed(link.expires_at) ? (
                                        <p className="tbk-gift-note tbk-gift-claim-detail__state--due">
                                            {t(
                                                'The return time has passed. You can still withdraw until the credits are returned.',
                                                '已过退回时间。积分退回前仍可提取到本机。',
                                                '已過退回時間。積分退回前仍可提取到本機。',
                                            )}
                                        </p>
                                    ) : (
                                        <div className="tbk-meta">
                                            <span>{t('Returns at', '退回时间', '退回時間')} {formatGiftInstant(link.expires_at)}</span>
                                        </div>
                                    )
                                ) : null}
                                <div className="tbk-share__actions">
                                    <button className="btn-primary" type="button" onClick={() => void withdrawGiftLink(link)} disabled={giftFlowBusy}>
                                        {t('Withdraw this gift', '提取这份转赠', '提取這份轉贈')}
                                    </button>
                                </div>
                            </li>
                        ))}
                    </ul>
                ) : null}
                <form className="tbk-gift-claim" onSubmit={(event) => { event.preventDefault(); void previewGift(); }}>
                    <div className="tbk-gift-form__row">
                        <input
                            aria-label={t('Gift code or link', '兑换码或链接', '兌換碼或連結')}
                            value={claimInput}
                            onChange={(event) => {
                                const next = event.target.value;
                                const nextCode = giftCodeFromInput(next).toLowerCase();
                                const prevCode = giftCodeFromInput(claimInput).toLowerCase();
                                // Publish the code before the next paint. A claim
                                // response already in flight reads this ref.
                                claimInputRef.current = next;
                                setClaimInput(next);
                                // A failed lookup has no preview line, only the notice.
                                // That notice belongs to the previous code.
                                if (nextCode !== prevCode) setGiftNotice(null);
                                // Keep the lookup only while the box still holds its code.
                                // Clearing it, or typing another code, must drop the old
                                // withdraw button and must not claim the code that left.
                                if (!giftPreview || (nextCode !== '' && nextCode === giftPreview.code.toLowerCase())) return;
                                const dropped = giftPreview.code.toLowerCase();
                                setGiftPreview(null);
                                setClaimedGift((current) => (current?.code.toLowerCase() === dropped ? null : current));
                                setGiftNotice(null);
                            }}
                        />
                        <button className="btn-secondary" type="submit" disabled={giftFlowBusy}>
                            {t('Preview', '查验', '查驗')}
                        </button>
                        {heldOnForm && heldLookup ? (
                            <button className="btn-primary" type="button" onClick={() => void withdrawGiftLink(heldLookup)} disabled={giftFlowBusy}>
                                {t('Withdraw this gift', '提取这份转赠', '提取這份轉贈')}
                            </button>
                        ) : heldLookup ? null : (
                            <button className="btn-primary" type="button" onClick={() => void claimGift()} disabled={giftFlowBusy || (!claimInput && !giftPreview)}>
                                {t('Claim', '领取', '領取')}
                            </button>
                        )}
                    </div>
                    {giftPreview ? (
                        <p
                            className={giftClaimIsError(giftPreview) ? 'tbk-error' : 'tbk-gift-note'}
                            role={giftNotice ? undefined : 'status'}
                        >
                            {t(
                                `${giftPreview.sender_masked || '—'} gifted ${formatCreditsGrouped(giftPreview.credits_micro, 2)} credits. ${giftBlockMessage(giftPreview) || 'This link can be claimed.'}`,
                                `${giftPreview.sender_masked || '—'} 转赠了 ${formatCreditsGrouped(giftPreview.credits_micro, 2)} 积分。${giftBlockMessage(giftPreview) || '这条转赠可以领取。'}`,
                                `${giftPreview.sender_masked || '—'} 轉贈了 ${formatCreditsGrouped(giftPreview.credits_micro, 2)} 積分。${giftBlockMessage(giftPreview) || '這條轉贈可以領取。'}`,
                            )}
                        </p>
                    ) : null}
                    {giftNotice ? (
                        <p className={giftNotice.tone === 'err' ? 'tbk-error' : 'tbk-gift-note'} role="status">
                            {giftNotice.text}
                        </p>
                    ) : null}
                    {claimedGift && claimedGift.credits_micro > 0 && claimedGift.id !== heldLookup?.id && !heldGifts.some((link) => link.id === claimedGift.id) ? (
                        <div className="tbk-share__actions">
                            <button className="btn-primary" type="button" onClick={() => void withdrawGiftLink(claimedGift)} disabled={giftFlowBusy}>
                                {t('Withdraw this gift', '提取这份转赠', '提取這份轉贈')}
                            </button>
                        </div>
                    ) : null}
                </form>
            </section>

            <section className="tbk-section tbk-section--shares">
                <div className="tbk-section__head">
                    <h4>{t('My shares', '我的分享', '我的分享')}</h4>
                    <div className="tbk-share__actions" role="group" aria-label={t('Earnings window', '收益区间', '收益區間')}>
                        {(['today', 'month', 'all'] as const).map((range) => (
                            <button
                                key={range}
                                type="button"
                                className={shareRange === range ? 'btn-primary' : 'btn-secondary'}
                                aria-pressed={shareRange === range}
                                onClick={() => setShareRange(range)}
                            >
                                {range === 'today'
                                    ? t('Today', '今日', '今日')
                                    : range === 'month'
                                        ? t('This month', '本月', '本月')
                                        : t('All', '累计', '累計')}
                            </button>
                        ))}
                    </div>
                </div>
                {!shares.length ? (
                    <div className="tbk-empty">
                        {loadedOnce
                            ? t('You have not shared any model yet.', '你还没有共享任何模型。', '你還沒有共享任何模型。')
                            : t('Loading…', '加载中…', '載入中…')}
                    </div>
                ) : (
                    <>
                    <ul className="tbk-share-grid" data-testid="tbk-share-grid">
                        {visibleShares.map((share) => {
                            const status = toShareStatus(share.status);
                            const live = isShareLive(share);
                            const label = STATUS_LABEL[status || ''] || [share.status || '—', share.status || '—', share.status || '—'];
                            const isOpen = !!expanded[share.id];
                            return (
                                <li className="tbk-share" key={share.id}>
                                    <div className="tbk-share__head">
                                        <span className="tbk-share__name">{share.display_name || share.id}</span>
                                        {share.visibility === 'private' ? (
                                            <span className="tbk-pill tbk-pill--off">{t('Private', '私有', '私有')}</span>
                                        ) : null}
                                        <span className={`tbk-pill ${live ? 'tbk-pill--on' : 'tbk-pill--off'}`}>
                                            {textForLang(lang, label[0], label[1], label[2])}
                                        </span>
                                    </div>
                                    <div className="tbk-meta">
                                        <span className="tbk-share__fingerprint">{share.key_fingerprint || '—'}</span>
                                        {share.extra_key_count > 0 ? (
                                            <span>
                                                {t(
                                                    `${share.extra_key_count} extra keys`,
                                                    `额外密钥 ${share.extra_key_count}`,
                                                    `額外密鑰 ${share.extra_key_count}`,
                                                )}
                                            </span>
                                        ) : null}
                                        <span>
                                            {t('Earned', '已赚', '已賺')}: {formatCreditsGrouped(
                                                share.stats_ready
                                                    ? (shareRange === 'today' ? share.today_earned_micro : shareRange === 'month' ? share.month_earned_micro : share.all_earned_micro)
                                                    : share.total_earned_micro,
                                                2,
                                            )}
                                        </span>
                                        {share.stats_ready ? (
                                            <span>
                                                {t('Consumed', '消耗', '消耗')}: {formatCreditsGrouped(share.range_gross_micro, 2)}
                                                {' · '}
                                                {Math.trunc(share.range_tokens).toLocaleString()} tokens
                                            </span>
                                        ) : null}
                                    </div>
                                    {share.stats_ready ? (
                                        <div className="tbk-meta">
                                            <span>{t('Today', '今日', '今日')} +{formatCreditsGrouped(share.today_earned_micro, 2)}</span>
                                            <span>{t('This month', '本月', '本月')} +{formatCreditsGrouped(share.month_earned_micro, 2)}</span>
                                            <span>{t('All', '累计', '累計')} +{formatCreditsGrouped(share.all_earned_micro, 2)}</span>
                                        </div>
                                    ) : null}
                                    {share.last_error ? <div className="tbk-share__error">{share.last_error}</div> : null}
                                    <div className="tbk-share__actions">
                                        <button className="btn-link" type="button" onClick={() => void toggleModels(share.id)} disabled={busyKey === `models:${share.id}`}>
                                            {isOpen ? t('Hide details', '收起明细', '收起明細') : t('Details', '明细', '明細')}
                                        </button>
                                        {/* A revoked share is gone; offering a switch on it is a button that always fails. */}
                                        {status !== 'revoked' ? (
                                            <button className="btn-link" type="button" onClick={() => void togglePaused(share)} disabled={busyKey === `pause:${share.id}`}>
                                                {live ? t('Pause', '暂停', '暫停') : t('Resume', '恢复', '恢復')}
                                            </button>
                                        ) : null}
                                        {status !== 'revoked' ? (
                                            <button className="btn-link" type="button" onClick={() => void rotateKey(share)} disabled={busyKey === `rotate:${share.id}`}>
                                                {t('Update key', '更新密钥', '更新密鑰')}
                                            </button>
                                        ) : null}
                                        {status !== 'revoked' ? (
                                            <button className="btn-link" type="button" onClick={() => void addKey(share)} disabled={busyKey === `addkey:${share.id}`}>
                                                {t('Add a key', '追加密钥', '追加密鑰')}
                                            </button>
                                        ) : null}
                                        {status !== 'revoked' ? (
                                            <button className="btn-link" type="button" onClick={() => changeAccess(share)} disabled={accessShare !== null}>
                                                {t('Change access', '更改访问范围', '更改存取範圍')}
                                            </button>
                                        ) : null}
                                        {status !== 'revoked' ? (
                                            <button className="btn-link" type="button" onClick={() => void adjustModels(share)} disabled={busyKey === `adjust:${share.id}` || adjustShare !== null}>
                                                {t('Adjust models', '调整模型', '調整模型')}
                                            </button>
                                        ) : null}
                                        <button className="btn-link tbk-danger" type="button" onClick={() => void takeOut(share)} disabled={busyKey === `takeout:${share.id}`}>
                                            {t('Take out', '取出', '取出')}
                                        </button>
                                    </div>
                                    {isOpen ? (
                                        <>
                                            <p className="tbk-meta" data-testid="tbk-range-formula">
                                                {t(
                                                    'Settled calls: you earn consumed credits minus the fee, and never more than the buyer paid.',
                                                    '已结算调用：您获得 = 消耗 − 手续费，且不超过消费者实付。',
                                                    '已結算呼叫：您獲得 = 消耗 − 手續費，且不超過消費者實付。',
                                                )}
                                            </p>
                                            <ul className="tbk-list tbk-list--models">
                                                {shownShareModels(modelsByShare[share.id]).length
                                                    ? shownShareModels(modelsByShare[share.id]).map(renderModelRow)
                                                    : <li className="tbk-empty">{t('No models on this share.', '此分享下没有模型。', '此分享下沒有模型。')}</li>}
                                            </ul>
                                        </>
                                    ) : null}
                                </li>
                            );
                        })}
                    </ul>
                    {sharePageCount > 1 ? (
                        <nav className="tbk-share-pager" aria-label={t('Share pages', '分享分页', '分享分頁')} data-testid="tbk-share-pager">
                            <button
                                type="button"
                                className="btn-secondary"
                                disabled={sharePageSafe <= 1}
                                onClick={() => setSharePage(sharePageSafe - 1)}
                            >
                                {t('Previous', '上一页', '上一頁')}
                            </button>
                            <span className="tbk-share-pager__status" aria-live="polite">
                                {t(
                                    `Page ${sharePageSafe} of ${sharePageCount}`,
                                    `第 ${sharePageSafe} / ${sharePageCount} 页`,
                                    `第 ${sharePageSafe} / ${sharePageCount} 頁`,
                                )}
                            </span>
                            <button
                                type="button"
                                className="btn-secondary"
                                disabled={sharePageSafe >= sharePageCount}
                                onClick={() => setSharePage(sharePageSafe + 1)}
                            >
                                {t('Next', '下一页', '下一頁')}
                            </button>
                        </nav>
                    ) : null}
                    </>
                )}
            </section>

            <section className="tbk-section tbk-section--withdrawals">
                <div className="tbk-section__head">
                    <h4>{t('Withdrawal history', '提取记录', '提取記錄')}</h4>
                    {pendingWithdrawalCount > 0 ? (
                        <span className="tbk-pill tbk-pill--wait" data-testid="tbk-withdraw-pending">
                            {t(
                                pendingWithdrawalCount === 1
                                    ? '1 still to finish'
                                    : `${pendingWithdrawalCount} still to finish`,
                                `${pendingWithdrawalCount} 笔待完成`,
                                `${pendingWithdrawalCount} 筆待完成`,
                            )}
                        </span>
                    ) : null}
                </div>
                {!withdrawals.length ? (
                    error ? null : (
                    <div className="tbk-empty" data-testid="tbk-withdraw-empty">
                        {loadedOnce
                            ? t('No withdrawal yet.', '还没有提取记录。', '還沒有提取記錄。')
                            : t('Loading…', '加载中…', '載入中…')}
                    </div>
                    )
                ) : (
                    <>
                    <ul className="tbk-withdraw-grid" data-testid="tbk-withdraw-grid">
                        {visibleWithdrawals.map((item) => {
                            const kind = (item.kind || '').toLowerCase();
                            const state = (item.state || '').toLowerCase();
                            const kindLabel = WITHDRAW_KIND_LABEL[kind] || [item.kind || '—', item.kind || '—', item.kind || '—'];
                            const stateLabel = withdrawStateLabel(kind, state, item.state || '—');
                            const detail = withdrawStateDetail(kind, state, item.hub_id !== '');
                            return (
                                <li
                                    className="tbk-withdraw"
                                    key={item.id || item.request_id || `${item.link_id}:${item.created_at}:${item.amount_micro}`}
                                    data-testid={`tbk-withdraw-${item.id || item.request_id}`}
                                >
                                    <div className="tbk-share__head">
                                        <span className="tbk-withdraw__amount">{formatCreditsGrouped(item.amount_micro, 2)}</span>
                                        <span className={`tbk-pill ${withdrawPillClass(state)}`}>
                                            {textForLang(lang, stateLabel[0], stateLabel[1], stateLabel[2])}
                                        </span>
                                    </div>
                                    <div className="tbk-gift-claim-detail">
                                        <span className="tbk-gift-claim-detail__who">
                                            {t('Source', '来源', '來源')}
                                            {' '}
                                            {textForLang(lang, kindLabel[0], kindLabel[1], kindLabel[2])}
                                        </span>
                                        {item.created_at ? (
                                            <span>{t('Time', '时间', '時間')} {formatGiftInstant(item.created_at, true)}</span>
                                        ) : null}
                                        {item.hub_id ? (
                                            <span className="tbk-withdraw__id">{t('Machine', '机器', '機器')} {item.hub_id}</span>
                                        ) : null}
                                        {item.grant_id ? (
                                            <span className="tbk-withdraw__id">{t('Grant', '发放单', '發放單')} {item.grant_id}</span>
                                        ) : null}
                                        {kind === 'gift' && item.link_id ? (
                                            <span className="tbk-withdraw__id">{t('Gift record', '转赠记录', '轉贈記錄')} {item.link_id}</span>
                                        ) : null}
                                        {detail ? (
                                            <span>{textForLang(lang, detail[0], detail[1], detail[2])}</span>
                                        ) : null}
                                    </div>
                                    {giftWithdrawalNeedsGrant(item) ? (
                                        <div className="tbk-share__actions">
                                            <button
                                                className="btn-primary"
                                                type="button"
                                                aria-label={t(
                                                    `Finish withdrawal of ${formatCreditsGrouped(item.amount_micro, 2)}`,
                                                    `完成提取 ${formatCreditsGrouped(item.amount_micro, 2)}`,
                                                    `完成提取 ${formatCreditsGrouped(item.amount_micro, 2)}`,
                                                )}
                                                onClick={() => {
                                                    giftWithdrawIDs.current[item.link_id] = item.request_id;
                                                    void withdrawGiftLink(giftLinkFromWithdrawal(item));
                                                }}
                                                disabled={busyKey === 'gift-withdraw'}
                                            >
                                                {t('Finish this withdrawal', '完成这次提取', '完成這次提取')}
                                            </button>
                                        </div>
                                    ) : null}
                                </li>
                            );
                        })}
                    </ul>
                    {withdrawPageCount > 1 ? (
                        <nav className="tbk-share-pager" aria-label={t('Withdrawal pages', '提取分页', '提取分頁')} data-testid="tbk-withdraw-pager">
                            <button
                                type="button"
                                className="btn-secondary"
                                aria-label={t('Previous withdrawal page', '上一页提取记录', '上一頁提取記錄')}
                                disabled={withdrawPageSafe <= 1}
                                onClick={() => setWithdrawPage(withdrawPageSafe - 1)}
                            >
                                {t('Previous', '上一页', '上一頁')}
                            </button>
                            <span className="tbk-share-pager__status" aria-live="polite">
                                {t(
                                    `Withdrawals, page ${withdrawPageSafe} of ${withdrawPageCount}`,
                                    `提取记录 第 ${withdrawPageSafe} / ${withdrawPageCount} 页`,
                                    `提取記錄 第 ${withdrawPageSafe} / ${withdrawPageCount} 頁`,
                                )}
                            </span>
                            <button
                                type="button"
                                className="btn-secondary"
                                aria-label={t('Next withdrawal page', '下一页提取记录', '下一頁提取記錄')}
                                disabled={withdrawPageSafe >= withdrawPageCount}
                                onClick={() => setWithdrawPage(withdrawPageSafe + 1)}
                            >
                                {t('Next', '下一页', '下一頁')}
                            </button>
                        </nav>
                    ) : null}
                    </>
                )}
            </section>
            {accessShare ? (
                <TokenBankAccessDialog
                    lang={lang}
                    share={accessShare}
                    onClose={() => setAccessShare(null)}
                    onSaved={() => {
                        setAccessShare(null);
                        notify(t('Access updated.', '访问范围已更新。', '存取範圍已更新。'));
                        void refresh();
                    }}
                />
            ) : null}
            {adjustShare ? (
                <TokenBankShareDialog
                    lang={lang}
                    request={{
                        providerName: adjustShare.provider.name,
                        apiURL: adjustShare.provider.url,
                        apiKey: adjustShare.provider.key,
                        protocol: String(adjustShare.provider.protocol || ''),
                        models: adjustShare.models,
                    }}
                    probe={(model) => probeProviderModelForShare(adjustShare.provider, model, t)}
                    alreadySharedModels={adjustShare.enabled}
                    adjustment={{
                        shareID: adjustShare.shareID,
                        enabledModels: adjustShare.enabled,
                        windows: adjustShare.windows,
                    }}
                    onRequestVerification={onRequestVerification}
                    onClose={() => setAdjustShare(null)}
                    onShared={() => {
                        const shareID = adjustShare.shareID;
                        notify(t('Model range updated.', '模型范围已更新。', '模型範圍已更新。'));
                        void (async () => {
                            await refresh();
                            if (!expandedRef.current[shareID]) return;
                            try {
                                const raw = await TokenBankListShareModels(shareID, shareRangeRef.current);
                                setModelsByShare((prev) => ({ ...prev, [shareID]: extractTokenBankModelDetails(raw) }));
                            } catch (err) {
                                notify(err instanceof Error ? err.message : String(err));
                            }
                        })();
                    }}
                />
            ) : null}
        </div>
    );
};

function shownShareModels(models: TokenBankShareModel[] | undefined): TokenBankShareModel[] {
    return (models || []).filter((model) => model.enabled || model.history_only);
}

function formatModelShareWindow(
    window: TokenBankShareWindow | null | undefined,
    t: (en: string, zhHans: string, zhHant?: string) => string,
): string {
    if (!window) return '';
    const start = (window.start || '').trim();
    const end = (window.end || '').trim();
    const days = (window.days || []).filter((day) => day >= 0 && day <= 6);
    const clockStart = start || '00:00';
    const clockEnd = end || '24:00';
    const allDays = days.length === 0 || days.length >= 7;
    if (allDays && clockStart === '00:00' && clockEnd === '24:00') return '';
    const clock = `${clockStart}–${clockEnd}`;
    if (allDays) return t(`Every day ${clock}`, `每天 ${clock}`, `每天 ${clock}`);
    const weekday = [1, 2, 3, 4, 5];
    const has = (day: number) => days.includes(day);
    if (weekday.every(has) && days.length === weekday.length) {
        return t(`Weekdays ${clock}`, `工作日 ${clock}`, `工作日 ${clock}`);
    }
    if (has(6) && has(0) && days.length === 2) {
        return t(`Weekends ${clock}`, `周末 ${clock}`, `週末 ${clock}`);
    }
    const labels: Record<number, [string, string, string]> = {
        1: ['Mon', '一', '一'],
        2: ['Tue', '二', '二'],
        3: ['Wed', '三', '三'],
        4: ['Thu', '四', '四'],
        5: ['Fri', '五', '五'],
        6: ['Sat', '六', '六'],
        0: ['Sun', '日', '日'],
    };
    const text = [1, 2, 3, 4, 5, 6, 0]
        .filter(has)
        .map((day) => t(labels[day][0], labels[day][1], labels[day][2]))
        .join(' ');
    return `${text} ${clock}`;
}

export default TokenBankPanel;
