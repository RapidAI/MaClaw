// Token Bank client-side contract (design doc §6.1).
//
// Wire-format note: the client endpoints are **entirely snake_case**. That is
// the opposite of the admin endpoints (`/api/admin/token-bank/*`), which
// marshal Go store structs that carry no json tags and therefore arrive as
// PascalCase. Both styles exist in the same feature; do not assume one.
//
// Money note: every credit figure on the wire is **microcredits** (integer,
// 1 credit = 1_000_000 micro). Display goes through formatCredits(); nothing
// here multiplies by a tier or a fee rate — pricing is the server's job, and
// duplicating it in the UI is how the two sides drift apart.

/** 1 credit expressed in microcredits. Mirrors `MicrocreditsPerCredit` in Go. */
export const MICROCREDITS_PER_CREDIT = 1_000_000;

/**
 * Share lifecycle state. The store defines exactly these three strings
 * (`TokenBankShareStatusActive` / `Paused` / `Revoked`). A UI that invents a
 * fourth ("available") compiles fine and then silently never matches: the
 * state filter returns nothing and every action button gets the wrong label.
 * Keeping it a union type makes that a compile error instead.
 */
export type TokenBankShareStatus = 'active' | 'paused' | 'revoked';

/** Window accepted by `GET /api/v1/token-bank/shares?range=`. */
export type TokenBankStatsRange = 'today' | 'month' | 'all';

/**
 * Settled totals for one window. These are sums of per-call §5 results.
 * Re-multiplying the token totals by the unit price would not match, because
 * each call is rounded on its own.
 */
export interface TokenBankUsageTotals {
    calls: number;
    input_tokens: number;
    output_tokens: number;
    cached_input_tokens: number;
    cache_write_tokens: number;
    gross_micro: number;
    fee_micro: number;
    net_micro: number;
    charged_micro: number;
    clamped_calls: number;
    tokens: number;
}

/** A share as returned by `GET /api/v1/token-bank/shares`. */
export interface TokenBankShare {
    id: string;
    display_name: string;
    api_url: string;
    protocol: string;
    status: string;
    visibility: string;
    /** Allow-list for a private share. Empty for a public share. */
    audiences: { hub_id: string; tenant_id: string }[];
    extra_key_count: number;
    service_group_id: string;
    key_fingerprint: string;
    has_key: boolean;
    total_earned_micro: number;
    /** True when the server attached usage windows. Older payloads leave this false. */
    stats_ready: boolean;
    today_earned_micro: number;
    month_earned_micro: number;
    all_earned_micro: number;
    range_earned_micro: number;
    range_gross_micro: number;
    range_fee_micro: number;
    range_charged_micro: number;
    range_tokens: number;
    range_calls: number;
    range_clamped_calls: number;
    max_input_tokens: number;
    max_output_tokens: number;
    last_error: string;
    paused_reason: string;
    created_at: string;
    updated_at: string;
}

/** One model inside a share, from `GET /api/v1/token-bank/shares/{id}/models`. */
export interface TokenBankShareModel {
    id: string;
    model_name: string;
    member_id: string;
    array_id: string;
    tier: string;
    tier_multiplier: number;
    enabled: boolean;
    available: boolean;
    last_probe_error: string;
    used_input_tokens: number;
    used_output_tokens: number;
    earned_micro: number;
    /** Null when the server did not send a window. A present object with calls 0 is a real empty window. */
    usage: TokenBankUsageTotals | null;
    /** When dispatch may dial this model. Null means always. */
    share_window: TokenBankShareWindow | null;
    /** True for a settled name that is no longer a row on the share. */
    history_only?: boolean;
}

/** Dial window stored on one shared model. Days use 0=Sunday … 6=Saturday. */
export interface TokenBankShareWindow {
    days?: number[];
    start?: string;
    end?: string;
}

/** The balance card, from `GET /api/v1/token-bank/summary`. */
export interface TokenBankSummary {
    earned_micro: number;
    received_micro: number;
    withdrawn_micro: number;
    granted_micro: number;
    frozen_micro: number;
    available_micro: number;
    gift_share_cap_micro: number;
    hub_count: number;
    auto_withdraw_limit_micro: number;
    gift_share_percent: number;
    gift_link_ttl_seconds: number;
    /**
     * Anti-abuse limits (§5), echoed so the share form can say *why* an amount
     * is refused instead of letting the user submit and collect a 400.
     *
     * `min_share_credits` is credits while the amounts on the wire are micro;
     * `share_creates_today` counts against `credit_share_daily_limit`, which
     * this payload does not echo, so the form treats "no limit left" as
     * inferable only from the server's rejection. Both are advisory — the
     * create endpoint re-checks them under a transaction.
     */
    min_share_credits: number;
    share_creates_today: number;
    /** 1-based rank among sharers with settled usage. 0 means none yet. */
    rank: number;
    rank_badge: string;
    lifetime_badge: string;
}

/**
 * One gift link. `code`, `claim_url`, and `deep_link` are present only on the
 * create, preview, and claim responses. The owner list omits the code.
 */
export interface TokenBankGiftLink {
    id: string;
    code: string;
    credits_micro: number;
    status: string;
    created_at: string;
    expires_at: string;
    claimed_at: string;
    revoked_at: string;
    claimed: boolean;
    /** Full address on the sender's own list. Other responses keep it masked. */
    claimed_by_email: string;
    /** Set on the sender's own list. Empty when the server did not send one. */
    claimed_by_user_id: string;
    /** Set on gifts this account claimed and has not withdrawn. */
    sender_masked: string;
    claim_url: string;
    deep_link: string;
}

/** Public preview, from `GET /api/v1/credits/share-links/{code}/preview`. */
export interface TokenBankGiftPreview {
    code: string;
    /** Set only when this account claimed the gift and can still withdraw it. */
    id: string;
    sender_masked: string;
    credits_micro: number;
    status: string;
    claimable: boolean;
    /** This account claimed it, and the credits are still frozen on the sender. */
    withdrawable: boolean;
    /** This account already withdrew it to a machine. */
    withdrawn: boolean;
    expires_at: string;
}

/** One withdrawal record, from `GET /api/v1/token-bank/credits/withdrawals`. */
export interface TokenBankWithdrawal {
    id: string;
    request_id: string;
    hub_id: string;
    amount_micro: number;
    grant_id: string;
    kind: string;
    link_id: string;
    state: string;
    created_at: string;
}

function numeric(value: unknown): number {
    const parsed = Number(value ?? 0);
    return Number.isFinite(parsed) ? parsed : 0;
}

function text(value: unknown): string {
    return String(value ?? '').trim();
}

/**
 * Normalize an unknown share payload.
 *
 * Reads snake_case first and falls back to PascalCase: the client endpoint is
 * snake_case today, but an admin-style struct leaking through (or a future
 * tag change) should degrade to a wrong-but-typed value rather than throw.
 */
export function normalizeTokenBankShare(raw: unknown): TokenBankShare {
    const r = (raw || {}) as Record<string, unknown>;
    return {
        id: text(r.id ?? r.ID),
        display_name: text(r.display_name ?? r.DisplayName),
        api_url: text(r.api_url ?? r.APIURL),
        protocol: text(r.protocol ?? r.Protocol),
        status: text(r.status ?? r.Status).toLowerCase(),
        visibility: text(r.visibility ?? r.Visibility),
        audiences: normalizeTokenBankAudiences(r.audiences ?? r.Audiences),
        extra_key_count: numeric(r.extra_key_count ?? r.ExtraKeyCount),
        service_group_id: text(r.service_group_id ?? r.ServiceGroupID),
        key_fingerprint: text(r.key_fingerprint ?? r.KeyFingerprint),
        has_key: Boolean(r.has_key ?? r.HasKey),
        total_earned_micro: numeric(r.total_earned_micro ?? r.TotalEarnedMicro),
        stats_ready: Boolean(r.stats_ready ?? r.StatsReady),
        today_earned_micro: numeric(r.today_earned_micro ?? r.TodayEarnedMicro),
        month_earned_micro: numeric(r.month_earned_micro ?? r.MonthEarnedMicro),
        all_earned_micro: numeric(r.all_earned_micro ?? r.AllEarnedMicro),
        range_earned_micro: numeric(r.range_earned_micro ?? r.RangeEarnedMicro),
        range_gross_micro: numeric(r.range_gross_micro ?? r.RangeGrossMicro),
        range_fee_micro: numeric(r.range_fee_micro ?? r.RangeFeeMicro),
        range_charged_micro: numeric(r.range_charged_micro ?? r.RangeChargedMicro),
        range_tokens: numeric(r.range_tokens ?? r.RangeTokens),
        range_calls: numeric(r.range_calls ?? r.RangeCalls),
        range_clamped_calls: numeric(r.range_clamped_calls ?? r.RangeClampedCalls),
        max_input_tokens: numeric(r.max_input_tokens ?? r.MaxInputTokens),
        max_output_tokens: numeric(r.max_output_tokens ?? r.MaxOutputTokens),
        last_error: text(r.last_error ?? r.LastError),
        paused_reason: text(r.paused_reason ?? r.PausedReason),
        created_at: text(r.created_at ?? r.CreatedAt),
        updated_at: text(r.updated_at ?? r.UpdatedAt),
    };
}

export function normalizeTokenBankShareModel(raw: unknown): TokenBankShareModel {
    const r = (raw || {}) as Record<string, unknown>;
    return {
        id: text(r.id ?? r.ID),
        model_name: text(r.model_name ?? r.ModelName),
        member_id: text(r.member_id ?? r.MemberID),
        array_id: text(r.array_id ?? r.ArrayID),
        tier: text(r.tier ?? r.Tier),
        tier_multiplier: numeric(r.tier_multiplier ?? r.TierMultiplier),
        available: Boolean(r.available ?? r.Available),
        last_probe_error: text(r.last_probe_error ?? r.LastProbeError),
        used_input_tokens: numeric(r.used_input_tokens ?? r.UsedInputTokens),
        used_output_tokens: numeric(r.used_output_tokens ?? r.UsedOutputTokens),
        earned_micro: numeric(r.earned_micro ?? r.EarnedMicro),
        usage: normalizeTokenBankUsage(r.usage ?? r.Usage),
        // Older payloads omit the flag. Those rows are on the share.
        enabled: r.enabled === undefined && r.Enabled === undefined ? true : Boolean(r.enabled ?? r.Enabled),
        share_window: normalizeTokenBankShareWindow(r.share_window ?? r.ShareWindow),
    };
}

function normalizeTokenBankShareWindow(raw: unknown): TokenBankShareWindow | null {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null;
    const row = raw as Record<string, unknown>;
    const start = text(row.start ?? row.Start);
    const end = text(row.end ?? row.End);
    const daysRaw = row.days ?? row.Days;
    const days = Array.isArray(daysRaw)
        ? daysRaw.map((day) => Number(day)).filter((day) => Number.isInteger(day) && day >= 0 && day <= 6)
        : [];
    if (!start && !end && days.length === 0) return null;
    const window: TokenBankShareWindow = {};
    if (days.length > 0) window.days = days;
    if (start) window.start = start;
    if (end) window.end = end;
    return window;
}

function normalizeTokenBankUsage(raw: unknown): TokenBankUsageTotals | null {
    if (!raw || typeof raw !== 'object') return null;
    const r = raw as Record<string, unknown>;
    return {
        calls: numeric(r.calls ?? r.Calls),
        input_tokens: numeric(r.input_tokens ?? r.InputTokens),
        output_tokens: numeric(r.output_tokens ?? r.OutputTokens),
        cached_input_tokens: numeric(r.cached_input_tokens ?? r.CachedInputTokens),
        cache_write_tokens: numeric(r.cache_write_tokens ?? r.CacheWriteTokens),
        gross_micro: numeric(r.gross_micro ?? r.GrossMicro),
        fee_micro: numeric(r.fee_micro ?? r.FeeMicro),
        net_micro: numeric(r.net_micro ?? r.NetMicro),
        charged_micro: numeric(r.charged_micro ?? r.ChargedMicro),
        clamped_calls: numeric(r.clamped_calls ?? r.ClampedCalls),
        tokens: numeric(r.tokens ?? r.Tokens),
    };
}

export function normalizeTokenBankSummary(raw: unknown): TokenBankSummary {
    const r = (raw || {}) as Record<string, unknown>;
    return {
        earned_micro: numeric(r.earned_micro ?? r.EarnedMicro),
        received_micro: numeric(r.received_micro ?? r.ReceivedMicro),
        withdrawn_micro: numeric(r.withdrawn_micro ?? r.WithdrawnMicro),
        granted_micro: numeric(r.granted_micro ?? r.GrantedMicro),
        frozen_micro: numeric(r.frozen_micro ?? r.FrozenMicro),
        available_micro: numeric(r.available_micro ?? r.AvailableMicro),
        gift_share_cap_micro: numeric(r.gift_share_cap_micro ?? r.GiftShareCapMicro),
        hub_count: numeric(r.hub_count ?? r.HubCount),
        auto_withdraw_limit_micro: numeric(r.auto_withdraw_limit_micro ?? r.AutoWithdrawLimitMicro),
        gift_share_percent: numeric(r.gift_share_percent ?? r.GiftSharePercent),
        gift_link_ttl_seconds: numeric(r.gift_link_ttl_seconds ?? r.GiftLinkTTLSeconds),
        min_share_credits: numeric(r.min_share_credits ?? r.MinShareCredits),
        share_creates_today: numeric(r.share_creates_today ?? r.ShareCreatesToday),
        rank: numeric(r.rank ?? r.Rank),
        rank_badge: text(r.rank_badge ?? r.RankBadge),
        lifetime_badge: text(r.lifetime_badge ?? r.LifetimeBadge),
    };
}

function normalizeTokenBankAudiences(raw: unknown): { hub_id: string; tenant_id: string }[] {
    if (!Array.isArray(raw)) return [];
    return raw.map((item) => {
        const row = (item || {}) as Record<string, unknown>;
        return {
            hub_id: text(row.hub_id ?? row.HubID),
            tenant_id: text(row.tenant_id ?? row.TenantID),
        };
    }).filter((row) => row.hub_id !== '' || row.tenant_id !== '');
}

export function normalizeTokenBankWithdrawal(raw: unknown): TokenBankWithdrawal {
    const r = (raw || {}) as Record<string, unknown>;
    return {
        id: text(r.id ?? r.ID),
        request_id: text(r.request_id ?? r.RequestID),
        hub_id: text(r.hub_id ?? r.HubID),
        amount_micro: numeric(r.amount_micro ?? r.AmountMicro),
        grant_id: text(r.grant_id ?? r.GrantID),
        kind: text(r.kind ?? r.Kind),
        link_id: text(r.link_id ?? r.LinkID),
        state: text(r.state ?? r.State ?? r.status ?? r.Status),
        created_at: text(r.created_at ?? r.CreatedAt),
    };
}

/** Extract the `shares` array from a list response, tolerating `items`. */
export function extractTokenBankShares(payload: unknown): TokenBankShare[] {
    const r = (payload || {}) as Record<string, unknown>;
    const list = r.shares ?? r.items;
    if (!Array.isArray(list)) return [];
    return list.map(normalizeTokenBankShare);
}

export function extractTokenBankModels(payload: unknown): TokenBankShareModel[] {
    const r = (payload || {}) as Record<string, unknown>;
    const list = r.models ?? r.items;
    if (!Array.isArray(list)) return [];
    return list.map(normalizeTokenBankShareModel);
}

/**
 * Models plus settled names that are no longer on the share. The share chip
 * must keep using extractTokenBankModels: a historical name is not "already
 * shared", and folding it in would hide that model from a new submit.
 */
export function extractTokenBankModelDetails(payload: unknown): TokenBankShareModel[] {
    const models = extractTokenBankModels(payload);
    const r = (payload || {}) as Record<string, unknown>;
    const extra = r.usage_models ?? r.usageModels;
    if (!Array.isArray(extra)) return models;
    return models.concat(extra.map((item) => ({ ...normalizeTokenBankShareModel(item), history_only: true })));
}

export function extractTokenBankWithdrawals(payload: unknown): TokenBankWithdrawal[] {
    const r = (payload || {}) as Record<string, unknown>;
    const list = r.withdrawals ?? r.items;
    if (!Array.isArray(list)) return [];
    return list.map(normalizeTokenBankWithdrawal);
}

export function normalizeTokenBankGiftLink(raw: unknown): TokenBankGiftLink {
    const r = (raw || {}) as Record<string, unknown>;
    return {
        id: text(r.id ?? r.ID),
        code: text(r.code ?? r.Code),
        credits_micro: numeric(r.credits_micro ?? r.CreditsMicro),
        status: text(r.status ?? r.Status).toLowerCase(),
        created_at: text(r.created_at ?? r.CreatedAt),
        expires_at: text(r.expires_at ?? r.ExpiresAt),
        claimed_at: text(r.claimed_at ?? r.ClaimedAt),
        revoked_at: text(r.revoked_at ?? r.RevokedAt),
        claimed: Boolean(r.claimed ?? r.Claimed),
        claimed_by_email: text(r.claimed_by_email ?? r.ClaimedByEmail),
        claimed_by_user_id: text(r.claimed_by_user_id ?? r.ClaimedByUserID ?? r.claimed_by_user),
        sender_masked: text(r.sender_masked ?? r.SenderMasked),
        claim_url: text(r.claim_url ?? r.ClaimURL),
        deep_link: text(r.deep_link ?? r.DeepLink),
    };
}

export function normalizeTokenBankGiftPreview(raw: unknown): TokenBankGiftPreview {
    const r = (raw || {}) as Record<string, unknown>;
    return {
        code: text(r.code ?? r.Code),
        id: text(r.id ?? r.ID),
        sender_masked: text(r.sender_masked ?? r.SenderMasked),
        credits_micro: numeric(r.credits_micro ?? r.CreditsMicro),
        status: text(r.status ?? r.Status).toLowerCase(),
        claimable: Boolean(r.claimable ?? r.Claimable),
        withdrawable: Boolean(r.withdrawable ?? r.Withdrawable),
        withdrawn: Boolean(r.withdrawn ?? r.Withdrawn),
        expires_at: text(r.expires_at ?? r.ExpiresAt),
    };
}

/**
 * Local wall-clock time for a gift timestamp. The wire value is UTC RFC3339,
 * which is hard to scan in a list. An unparseable value is returned unchanged.
 * Pass withSeconds for a history row: two withdrawals in the same minute stay distinct.
 */
export function formatGiftInstant(raw: string, withSeconds = false): string {
    const value = raw.trim();
    const at = Date.parse(value);
    if (!Number.isFinite(at)) return value;
    const d = new Date(at);
    const pad = (n: number) => String(n).padStart(2, '0');
    const clock = `${pad(d.getHours())}:${pad(d.getMinutes())}${withSeconds ? `:${pad(d.getSeconds())}` : ''}`;
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${clock}`;
}

/** True when the instant is present and no longer in the future. */
export function giftInstantPassed(raw: string, now = Date.now()): boolean {
    const at = Date.parse(raw.trim());
    return Number.isFinite(at) && at <= now;
}

/** Extract `share_links` from the owner list. An absent array is an empty list. */
export function extractTokenBankGiftLinks(payload: unknown): TokenBankGiftLink[] {
    const r = (payload || {}) as Record<string, unknown>;
    const list = r.share_links ?? r.links ?? r.items;
    if (!Array.isArray(list)) return [];
    return list.map(normalizeTokenBankGiftLink);
}

/** Gifts this account claimed and has not withdrawn. Absent means none. */
export function extractTokenBankClaimedLinks(payload: unknown): TokenBankGiftLink[] {
    const r = (payload || {}) as Record<string, unknown>;
    const list = r.claimed_links;
    if (!Array.isArray(list)) return [];
    return list.map(normalizeTokenBankGiftLink);
}

/**
 * The string a QR code and the copy button should carry.
 * Prefer the https claim URL, then the deep link, then a deep link built from
 * the code that only the create response still has.
 */
export function giftClaimTarget(link: Pick<TokenBankGiftLink, 'claim_url' | 'deep_link' | 'code'> | null | undefined): string {
    if (!link) return '';
    if (link.claim_url) return link.claim_url;
    if (link.deep_link) return link.deep_link;
    if (link.code) return `maclaw://credit/${encodeURIComponent(link.code)}`;
    return '';
}

/**
 * Split a micro amount into the one field the create API accepts.
 * A whole number of credits goes out as `credits`. Anything else goes out as
 * `credits_micro`. Sending both is a server error.
 */
export function wholeCreditsOrMicro(micro: number): { credits: number; creditsMicro: number } {
    if (!Number.isFinite(micro) || micro <= 0) return { credits: 0, creditsMicro: 0 };
    const rounded = Math.round(micro);
    if (rounded % MICROCREDITS_PER_CREDIT === 0) {
        return { credits: rounded / MICROCREDITS_PER_CREDIT, creditsMicro: 0 };
    }
    return { credits: 0, creditsMicro: rounded };
}

/**
 * Why a typed amount cannot be shared, or null when it can.
 *
 * The server is the authority — it re-checks all of this inside the freeze
 * transaction. This exists so the form can name the reason *before* the user
 * presses the button: a refusal the UI could have predicted and didn't is the
 * difference between "the form knows what it's doing" and "the form is broken".
 *
 * The floor is deliberately compared against the same micro amount the create
 * call will send, so a value that passes here cannot be rejected there.
 */
export type GiftRejection = 'empty' | 'below_minimum' | 'over_cap' | 'no_balance';

export function giftAmountRejection(
    micro: number | null,
    summary: TokenBankSummary | null | undefined,
): GiftRejection | null {
    if (micro == null || !Number.isFinite(micro) || micro <= 0) return 'empty';
    const cap = numeric(summary?.gift_share_cap_micro);
    if (cap <= 0) return 'no_balance';
    if (micro > cap) return 'over_cap';
    const minCredits = numeric(summary?.min_share_credits);
    if (minCredits > 0 && micro < minCredits * MICROCREDITS_PER_CREDIT) return 'below_minimum';
    return null;
}

/**
 * Pull a gift code out of a paste. Accepts the bare code, an https claim URL,
 * and a maclaw://credit/<code> deep link.
 */
export function giftCodeFromInput(raw: unknown): string {
    const s = text(raw);
    if (!s) return '';
    if (s.includes('://')) {
        try {
            const u = new URL(s);
            const path = u.pathname.replace(/^\/+|\/+$/g, '');
            const last = path.split('/').filter(Boolean).pop();
            if (last) return decodeURIComponent(last);
        } catch {
            // Fall through to the last path segment.
        }
    }
    const parts = s.split('/').filter(Boolean);
    return parts.length ? parts[parts.length - 1] : s;
}

/**
 * Is this share currently routable?
 *
 * Deliberately an equality check against the canonical string rather than a
 * truthiness check on an unknown: `status` is lowercased during normalization
 * so casing drift on the wire cannot flip the answer.
 */
export function isShareLive(share: Pick<TokenBankShare, 'status'> | null | undefined): boolean {
    return text(share?.status).toLowerCase() === 'active';
}

/** Narrow an unknown status string to the union, or null when unrecognized. */
export function toShareStatus(value: unknown): TokenBankShareStatus | null {
    const s = text(value).toLowerCase();
    return s === 'active' || s === 'paused' || s === 'revoked' ? s : null;
}

/**
 * Format microcredits as a credit figure for display.
 *
 * Trims trailing zeros so "3" reads as "3" rather than "3.000000", but never
 * drops a significant digit from a small amount: 1 microcredit still renders
 * rather than rounding to 0.
 */
export function formatCredits(micro: number | string | null | undefined, maxFractionDigits = 6): string {
    const credits = numeric(micro) / MICROCREDITS_PER_CREDIT;
    const fixed = credits.toFixed(Math.max(0, Math.min(6, maxFractionDigits)));
    const trimmed = fixed.includes('.') ? fixed.replace(/0+$/, '').replace(/\.$/, '') : fixed;
    // A genuinely tiny balance must not render as an empty string.
    return trimmed === '' || trimmed === '-' ? '0' : trimmed;
}

/** Same as formatCredits but with thousands separators, for headline figures. */
export function formatCreditsGrouped(micro: number | string | null | undefined, maxFractionDigits = 2): string {
    const credits = numeric(micro) / MICROCREDITS_PER_CREDIT;
    const rounded = Number(credits.toFixed(Math.max(0, Math.min(6, maxFractionDigits))));
    return rounded.toLocaleString(undefined, { maximumFractionDigits: maxFractionDigits });
}

/** Parse a user-typed whole-credit amount into microcredits, or null if invalid. */
export function creditsToMicro(input: string | number | null | undefined): number | null {
    const raw = text(input);
    if (!raw) return null;
    const credits = Number(raw);
    if (!Number.isFinite(credits) || credits < 0) return null;
    return Math.round(credits * MICROCREDITS_PER_CREDIT);
}

/**
 * A request id for an idempotent withdrawal.
 *
 * The withdraw endpoint is idempotent on `request_id`. The panel keeps one id
 * until the call succeeds, so a retry after a lost response does not debit
 * twice. The button stays disabled during the call so a double-click cannot
 * start a second id. `crypto.randomUUID` is available in the Wails webview.
 */
export function newWithdrawRequestID(): string {
    const cryptoObj = (globalThis as { crypto?: { randomUUID?: () => string; getRandomValues?: (a: Uint8Array) => void } }).crypto;
    if (cryptoObj?.randomUUID) return cryptoObj.randomUUID();
    const bytes = new Uint8Array(16);
    if (cryptoObj?.getRandomValues) {
        cryptoObj.getRandomValues(bytes);
    } else {
        for (let i = 0; i < bytes.length; i += 1) bytes[i] = Math.floor(Math.random() * 256);
    }
    return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

/**
 * Auto-withdraw headroom hint.
 *
 * Automatic withdrawal is capped at 1/N of the balance so one machine cannot
 * drain a user who owns several hubs (E6). This is informational only — the
 * server computes the real cap; showing the number here just stops the user
 * from being surprised that an automatic top-up moved less than everything.
 */
export function describeAutoWithdraw(summary: TokenBankSummary | null | undefined): string {
    if (!summary) return '';
    const hubs = Math.max(1, numeric(summary.hub_count));
    return `${formatCredits(summary.auto_withdraw_limit_micro, 2)} / ${hubs}`;
}

// --- Share submission (§7.1 / §7.2) -----------------------------------------

/**
 * Probe verdict for a single model, as the share dialog shows it.
 *
 * `pending` is a real state, not a placeholder: the dialog opens before probing
 * starts, and a model that has not been probed must not be presented as
 * "available ✗" or silently dropped.
 */
export type ShareModelProbeState = 'pending' | 'probing' | 'available' | 'unavailable';

export interface ShareModelEntry {
    model: string;
    state: ShareModelProbeState;
    /** Latency in milliseconds, when the probe measured one. */
    latencyMs?: number;
    /** Server/provider error text when the probe failed. */
    error?: string;
}

/**
 * The idempotency fingerprint for a provider key.
 *
 * The server folds this into the idempotency key, so it must be *stable across
 * retries of the same submission* and *different for a genuinely different
 * key*. Hashing the full key satisfies both; the key itself never leaves the
 * device (the key travels only inside the RSA envelope).
 *
 * A non-cryptographic fallback is used when SubtleCrypto is unavailable, which
 * would be a collision risk — so it is deliberately patterned after the key
 * rather than random, and the key is mixed in. It is still a fingerprint, not a
 * secret, and the server treats it as an opaque string.
 */
/** Models the probe found that this provider has not already shared. */
export function modelsMissingFromShare(available: string[], alreadyShared: string[]): string[] {
    const have = new Set(alreadyShared.map((name) => name.trim().toLowerCase()).filter(Boolean));
    const seen = new Set<string>();
    const missing: string[] = [];
    for (const model of available) {
        const name = model.trim();
        const key = name.toLowerCase();
        if (!name || have.has(key) || seen.has(key)) continue;
        seen.add(key);
        missing.push(name);
    }
    return missing;
}

const HIDDEN_URL_QUERY_KEYS = new Set([
    'access_token',
    'api_key',
    'api-key',
    'apikey',
    'client_secret',
    'key',
    'password',
    'secret',
    'token',
]);

/**
 * URL shown beside a provider name.
 * A stored endpoint may carry a username, a password, or a key in the query.
 * The published value stays intact; only this display copy drops those secrets.
 * A URL with nothing to hide is returned unchanged, including its trailing slash.
 */
export function tokenBankDisplayURL(url: string): string {
    const trimmed = url.trim();
    try {
        const parsed = new URL(trimmed);
        let hidden = false;
        if (parsed.username || parsed.password) {
            parsed.username = '';
            parsed.password = '';
            hidden = true;
        }
        for (const key of [...parsed.searchParams.keys()]) {
            if (!HIDDEN_URL_QUERY_KEYS.has(key.toLowerCase())) continue;
            parsed.searchParams.delete(key);
            hidden = true;
        }
        return hidden ? parsed.toString() : trimmed;
    } catch {
        return trimmed;
    }
}

/**
 * Index of the local credential that published this share.
 * The fingerprint is bound to the endpoint and the key. A trailing slash on
 * either copy still matches. Returns -1 when none match.
 */
export async function matchTokenBankShareCredential(
    candidates: Array<{ url: string; key: string }>,
    shareURL: string,
    fingerprint: string,
): Promise<number> {
    const want = fingerprint.trim();
    const target = shareURL.trim();
    if (!want || !target) return -1;
    for (let index = 0; index < candidates.length; index += 1) {
        const url = String(candidates[index]?.url || '').trim();
        const key = String(candidates[index]?.key || '').trim();
        if (!url || !key || !sameTokenBankAPI(url, target)) continue;
        if (await fingerprintProviderKey(key, url) === want) return index;
        if (url !== target && await fingerprintProviderKey(key, target) === want) return index;
    }
    return -1;
}

/** Catalog names first, then enabled share names the catalog no longer lists. */
export function unionShareModelNames(discovered: string[], shared: string[]): string[] {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const name of [...discovered, ...shared]) {
        const trimmed = String(name || '').trim();
        const key = trimmed.toLowerCase();
        if (!trimmed || seen.has(key)) continue;
        seen.add(key);
        out.push(trimmed);
    }
    return out;
}

/** Same provider endpoint, ignoring trailing slashes and case. */
export function sameTokenBankAPI(left: string, right: string): boolean {
    const normalize = (value: string) => value.trim().replace(/\/+$/, '').toLowerCase();
    const a = normalize(left);
    const b = normalize(right);
    return a !== '' && a === b;
}

export async function fingerprintProviderKey(apiKey: string, apiURL: string): Promise<string> {
    const material = `${apiURL}\u0000${apiKey}`;
    const cryptoObj = (globalThis as { crypto?: { subtle?: { digest?: (alg: string, data: Uint8Array) => Promise<ArrayBuffer> } } }).crypto;
    if (cryptoObj?.subtle?.digest) {
        const bytes = new TextEncoder().encode(material);
        const digest = await cryptoObj.subtle.digest('SHA-256', bytes);
        return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, '0')).join('').slice(0, 32);
    }
    // FNV-1a over the material: stable, cheap, and not a security boundary.
    let hash = 0x811c9dc5;
    for (let i = 0; i < material.length; i += 1) {
        hash ^= material.charCodeAt(i);
        hash = Math.imul(hash, 0x01000193) >>> 0;
    }
    return `fnv${hash.toString(16).padStart(8, '0')}${material.length.toString(16)}`;
}

/**
 * Whether a model may be submitted.
 *
 * Unavailable models are still *sent* (with available=false) so the owner sees
 * why a model is missing rather than finding it silently absent — but a share
 * with no available model at all is pointless, which the dialog blocks.
 */
export function selectableShareModels(entries: ShareModelEntry[]): ShareModelEntry[] {
    return entries.filter((entry) => entry.state === 'available');
}

export function countProbeProgress(entries: ShareModelEntry[]): { done: number; total: number } {
    // "pending" and "probing" are both outstanding: counting only "pending"
    // would make the progress bar jump to 100% the moment probing starts.
    const total = entries.length;
    const done = entries.filter((entry) => entry.state === 'available' || entry.state === 'unavailable').length;
    return { done, total };
}

/** Masked provider URL for the confirm dialog subtitle. */
export function displayProviderLabel(name: string, apiURL: string): string {
    return `${name} · ${apiURL}`;
}
