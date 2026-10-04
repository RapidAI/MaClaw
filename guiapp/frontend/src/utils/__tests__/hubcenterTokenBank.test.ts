import { describe, expect, it } from 'vitest';
import {
    MICROCREDITS_PER_CREDIT,
    countProbeProgress,
    creditsToMicro,
    describeAutoWithdraw,
    displayProviderLabel,
    extractTokenBankGiftLinks,
    extractTokenBankModelDetails,
    extractTokenBankModels,
    extractTokenBankShares,
    extractTokenBankWithdrawals,
    isAutomaticTokenBankWithdrawal,
    fingerprintProviderKey,
    matchTokenBankShareCredential,
    modelsMissingFromShare,
    unionShareModelNames,
    sameTokenBankAPI,
    tokenBankDisplayURL,
    formatCredits,
    formatCreditsGrouped,
    formatGiftInstant,
    giftInstantPassed,
    giftAmountRejection,
    giftClaimTarget,
    giftCodeFromInput,
    isShareLive,
    shareCapNoteKind,
    shareCapPauseKind,
    newWithdrawRequestID,
    normalizeTokenBankShare,
    normalizeTokenBankSummary,
    selectableShareModels,
    toShareStatus,
    wholeCreditsOrMicro,
    type ShareModelEntry,
} from '../hubcenterTokenBank';

describe('hubcenterTokenBank normalization', () => {
    it('reads the snake_case client wire format', () => {
        const share = normalizeTokenBankShare({
            id: 'sh_1',
            display_name: 'My OpenAI',
            status: 'ACTIVE',
            key_fingerprint: 'sk-...abcd',
            has_key: true,
            total_earned_micro: 1_500_000,
            last_error: '',
        });
        expect(share.id).toBe('sh_1');
        expect(share.display_name).toBe('My OpenAI');
        // Status is lowercased so casing drift cannot flip isShareLive().
        expect(share.status).toBe('active');
        expect(share.has_key).toBe(true);
        expect(share.total_earned_micro).toBe(1_500_000);
        expect(share.stats_ready).toBe(false);
        expect(share.today_earned_micro).toBe(0);
    });

    it('reads the today, month, and all windows when the server sends them', () => {
        const share = normalizeTokenBankShare({
            id: 'sh_range',
            stats_ready: true,
            today_earned_micro: 200_000,
            month_earned_micro: 500_000,
            all_earned_micro: 3_000_000,
            range_gross_micro: 400_000,
            range_tokens: 12,
        });
        expect(share.stats_ready).toBe(true);
        expect(share.today_earned_micro).toBe(200_000);
        expect(share.month_earned_micro).toBe(500_000);
        expect(share.all_earned_micro).toBe(3_000_000);
        expect(share.range_gross_micro).toBe(400_000);
        expect(share.range_tokens).toBe(12);
    });

    it('falls back to PascalCase so an admin-style payload degrades instead of throwing', () => {
        const share = normalizeTokenBankShare({ ID: 'sh_2', DisplayName: 'Legacy', Status: 'paused' });
        expect(share.id).toBe('sh_2');
        expect(share.display_name).toBe('Legacy');
        expect(share.status).toBe('paused');
    });

    it('coerces nonsense numeric fields to 0 rather than NaN', () => {
        const share = normalizeTokenBankShare({ id: 'sh_3', total_earned_micro: 'not-a-number' });
        expect(share.total_earned_micro).toBe(0);
        expect(Number.isNaN(share.total_earned_micro)).toBe(false);
    });

    it('normalizes a summary with defaults for every missing field', () => {
        const summary = normalizeTokenBankSummary({ available_micro: 2_000_000 });
        expect(summary.available_micro).toBe(2_000_000);
        expect(summary.hub_count).toBe(0);
        expect(summary.frozen_micro).toBe(0);
        expect(summary.gift_share_percent).toBe(0);
        expect(summary.rank).toBe(0);
        expect(summary.rank_badge).toBe('');
        expect(summary.lifetime_badge).toBe('');
    });

    it('reads rank and lifetime badges from the summary', () => {
        const summary = normalizeTokenBankSummary({
            rank: 1,
            rank_badge: 'gold',
            lifetime_badge: 'pillar',
        });
        expect(summary.rank).toBe(1);
        expect(summary.rank_badge).toBe('gold');
        expect(summary.lifetime_badge).toBe('pillar');
    });

    it('keeps a private audience and the extra-key count', () => {
        const share = normalizeTokenBankShare({
            id: 'sh_private',
            visibility: 'private',
            extra_key_count: 2,
            audiences: [{ hub_id: 'hub-a', tenant_id: 'ten-1' }, { hub_id: '', tenant_id: '' }],
        });
        expect(share.visibility).toBe('private');
        expect(share.extra_key_count).toBe(2);
        expect(share.audiences).toEqual([{ hub_id: 'hub-a', tenant_id: 'ten-1' }]);
    });

    it('extracts arrays and tolerates a missing or malformed list', () => {
        expect(extractTokenBankShares({ shares: [{ id: 'a' }, { id: 'b' }] })).toHaveLength(2);
        expect(extractTokenBankShares({ items: [{ id: 'a' }] })).toHaveLength(1);
        expect(extractTokenBankShares({})).toEqual([]);
        expect(extractTokenBankShares(null)).toEqual([]);
        expect(extractTokenBankModels({ models: [{ model_name: 'gpt-4o' }] })[0].model_name).toBe('gpt-4o');
        const detailed = extractTokenBankModelDetails({
            models: [{ model_name: 'gpt-4o', usage: { calls: 2, net_micro: 5 } }],
            usage_models: [{ model_name: 'gpt-old', usage: { calls: 1, gross_micro: 9 } }],
        });
        expect(detailed.map((model) => model.model_name)).toEqual(['gpt-4o', 'gpt-old']);
        expect(detailed[0].usage?.calls).toBe(2);
        expect(detailed[0].usage?.net_micro).toBe(5);
        expect(extractTokenBankModels({ models: [{ model_name: 'gpt-4o' }], usage_models: [{ model_name: 'gpt-old' }] })).toHaveLength(1);
        expect(extractTokenBankWithdrawals({ withdrawals: [{ id: 'w', amount_micro: 5 }] })[0].amount_micro).toBe(5);
    });
});

describe('hubcenterTokenBank share status', () => {
    it('treats only "active" as live — the store literal, not "available"', () => {
        // This is the D27 regression: a UI that checks for "available" compiles
        // fine and then never matches, flipping every button to the wrong label.
        expect(isShareLive({ status: 'active' })).toBe(true);
        expect(isShareLive({ status: 'available' })).toBe(false);
        expect(isShareLive({ status: 'paused' })).toBe(false);
        expect(isShareLive({ status: 'revoked' })).toBe(false);
        expect(isShareLive(null)).toBe(false);
        expect(isShareLive(undefined)).toBe(false);
    });

    it('narrows recognized statuses and rejects everything else', () => {
        expect(toShareStatus('ACTIVE')).toBe('active');
        expect(toShareStatus('paused')).toBe('paused');
        expect(toShareStatus('revoked')).toBe('revoked');
        expect(toShareStatus('available')).toBeNull();
        expect(toShareStatus('')).toBeNull();
    });

    it('recognizes an automatic token-cap pause and ignores a manual one', () => {
        expect(shareCapPauseKind({
            status: 'paused',
            paused_reason: 'token cap: anomaly',
            last_error: 'token cap: anomaly',
        })).toBe('anomaly');
        expect(shareCapPauseKind({ status: 'paused', paused_reason: 'Token Cap: Daily' })).toBe('daily');
        expect(shareCapPauseKind({
            status: 'paused',
            paused_reason: 'token cap: daily',
            last_error: 'glm-5: token cap: anomaly',
        })).toBe('daily');
        expect(shareCapPauseKind({
            status: 'paused',
            paused_reason: 'token cap: daily ... token cap: anomaly',
        })).toBe('anomaly');
        expect(shareCapNoteKind('glm-5.3-flash: token cap: monthly')).toBe('monthly');
        expect(shareCapNoteKind('token cap: daily ... token cap: anomaly')).toBe('anomaly');
        expect(shareCapNoteKind('token cap: anomaly extra')).toBeNull();
        // A leftover cap sentence is not the current pause.
        expect(shareCapPauseKind({
            status: 'paused',
            paused_reason: '',
            last_error: 'token cap: anomaly',
        })).toBeNull();
        expect(shareCapPauseKind({
            status: 'paused',
            last_error: 'glm-5.3-flash: token cap: monthly',
        })).toBeNull();
        expect(shareCapPauseKind({
            status: 'paused',
            paused_reason: 'manual',
            last_error: 'upstream timeout',
        })).toBeNull();
        expect(shareCapPauseKind({ status: 'active', paused_reason: 'token cap: daily', last_error: 'token cap: anomaly' })).toBeNull();
        expect(shareCapPauseKind(null)).toBeNull();
        expect(shareCapPauseKind(undefined)).toBeNull();
    });
});

describe('hubcenterTokenBank credit formatting', () => {
    it('converts microcredits to credits with trailing zeros trimmed', () => {
        expect(formatCredits(3 * MICROCREDITS_PER_CREDIT)).toBe('3');
        expect(formatCredits(1_500_000)).toBe('1.5');
        expect(formatCredits(0)).toBe('0');
        expect(formatCredits(null)).toBe('0');
    });

    it('never rounds a tiny positive balance down to zero', () => {
        // 1 microcredit is one millionth of a credit; it must still render.
        expect(formatCredits(1)).toBe('0.000001');
    });

    it('groups thousands for headline figures', () => {
        expect(formatCreditsGrouped(1_234_500_000, 0)).toBe('1,235');
        expect(formatCreditsGrouped(0)).toBe('0');
    });

    it('parses typed whole credits into microcredits and rejects bad input', () => {
        expect(creditsToMicro('3')).toBe(3_000_000);
        expect(creditsToMicro('1.5')).toBe(1_500_000);
        expect(creditsToMicro('0')).toBe(0);
        expect(creditsToMicro('')).toBeNull();
        expect(creditsToMicro('abc')).toBeNull();
        expect(creditsToMicro('-5')).toBeNull();
    });
});

describe('hubcenterTokenBank withdraw helpers', () => {
    it('generates a distinct request id each call', () => {
        const a = newWithdrawRequestID();
        const b = newWithdrawRequestID();
        expect(a).not.toBe(b);
        expect(a.length).toBeGreaterThan(0);
    });

    it('describes the 1/N auto-withdraw headroom', () => {
        const text = describeAutoWithdraw({
            earned_micro: 0, received_micro: 0, withdrawn_micro: 0, granted_micro: 0,
            frozen_micro: 0, available_micro: 9_000_000, gift_share_cap_micro: 0,
            hub_count: 3, auto_withdraw_limit_micro: 3_000_000,
            gift_share_percent: 50, gift_link_ttl_seconds: 0,
            min_share_credits: 1, share_creates_today: 0,
            rank: 0, rank_badge: '', lifetime_badge: '',
        });
        expect(text).toBe('3 / 3');
    });

    it('describes headroom with a single hub when the count is unknown', () => {
        const text = describeAutoWithdraw({
            earned_micro: 0, received_micro: 0, withdrawn_micro: 0, granted_micro: 0,
            frozen_micro: 0, available_micro: 1_000_000, gift_share_cap_micro: 0,
            hub_count: 0, auto_withdraw_limit_micro: 1_000_000,
            gift_share_percent: 50, gift_link_ttl_seconds: 0,
            min_share_credits: 1, share_creates_today: 0,
            rank: 0, rank_badge: '', lifetime_badge: '',
        });
        expect(text).toBe('1 / 1');
    });

    it('returns an empty hint without a summary', () => {
        expect(describeAutoWithdraw(null)).toBe('');
    });

    it('marks an automatic withdrawal from the request id or the flag', () => {
        expect(isAutomaticTokenBankWithdrawal({ request_id: 'tbk-auto:hub:a@b.c:paid:0' })).toBe(true);
        expect(isAutomaticTokenBankWithdrawal({ request_id: 'req-manual', automatic: true })).toBe(true);
        expect(isAutomaticTokenBankWithdrawal({ request_id: 'req-manual' })).toBe(false);
        const [row] = extractTokenBankWithdrawals({
            withdrawals: [{ id: 'w1', request_id: 'tbk-auto:hub:a@b.c:paid:1', amount_micro: 1 }],
        });
        expect(row.automatic).toBe(true);
    });
});

describe('hubcenterTokenBank gift links', () => {
    it('formats a gift instant in local wall time and leaves junk alone', () => {
        const raw = '2026-10-03T01:02:03Z';
        const at = new Date(raw);
        const pad = (n: number) => String(n).padStart(2, '0');
        expect(formatGiftInstant(raw)).toBe(
            `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}`,
        );
        expect(formatGiftInstant(raw, true)).toBe(
            `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`,
        );
        expect(formatGiftInstant('not-a-date')).toBe('not-a-date');
        expect(formatGiftInstant('  ')).toBe('');
        expect(giftInstantPassed('2020-01-01T00:00:00Z')).toBe(true);
        expect(giftInstantPassed('2999-01-01T00:00:00Z')).toBe(false);
        expect(giftInstantPassed('')).toBe(false);
    });

    it('keeps the claimer id from the owner list', () => {
        const links = extractTokenBankGiftLinks({
            share_links: [{
                id: 'gift_2',
                status: 'claimed',
                claimed_by_email: 'pat@example.com',
                claimed_by_user_id: 'user-pat',
            }],
        });
        expect(links[0].claimed_by_email).toBe('pat@example.com');
        expect(links[0].claimed_by_user_id).toBe('user-pat');
    });

    it('reads the owner list without inventing a code', () => {
        const links = extractTokenBankGiftLinks({
            share_links: [{ id: 'gift_1', credits_micro: 2_000_000, status: 'Active', claimed_by_email: 'a***@x.com' }],
        });
        expect(links).toEqual([expect.objectContaining({
            id: 'gift_1',
            code: '',
            credits_micro: 2_000_000,
            status: 'active',
            claimed_by_email: 'a***@x.com',
            claimed_by_user_id: '',
        })]);
        expect(giftClaimTarget(links[0])).toBe('');
    });

    it('prefers the https claim URL for the QR target', () => {
        expect(giftClaimTarget({
            claim_url: 'https://hub.example/c/CODE',
            deep_link: 'maclaw://credit/CODE',
            code: 'CODE',
        })).toBe('https://hub.example/c/CODE');
        expect(giftClaimTarget({ claim_url: '', deep_link: '', code: 'CODE' })).toBe('maclaw://credit/CODE');
    });

    it('sends whole credits as credits and a fraction as credits_micro', () => {
        expect(wholeCreditsOrMicro(2_000_000)).toEqual({ credits: 2, creditsMicro: 0 });
        expect(wholeCreditsOrMicro(1_250_000)).toEqual({ credits: 0, creditsMicro: 1_250_000 });
        expect(wholeCreditsOrMicro(0)).toEqual({ credits: 0, creditsMicro: 0 });
    });

    it('extracts a code from a bare value, an https URL, and a deep link', () => {
        expect(giftCodeFromInput('SECRETCODE')).toBe('SECRETCODE');
        expect(giftCodeFromInput('https://hub.example/c/SECRETCODE')).toBe('SECRETCODE');
        expect(giftCodeFromInput('https://hub.example/c/SECRETCODE/')).toBe('SECRETCODE');
        expect(giftCodeFromInput('maclaw://credit/SECRETCODE')).toBe('SECRETCODE');
        expect(giftCodeFromInput('  ')).toBe('');
    });
});

describe('Token Bank share submission helpers', () => {
    it('counts only finished probes as done, so the bar does not jump to 100%', () => {
        const entries: ShareModelEntry[] = [
            { model: 'a', state: 'available' },
            { model: 'b', state: 'probing' },
            { model: 'c', state: 'pending' },
            { model: 'd', state: 'unavailable' },
        ];
        expect(countProbeProgress(entries)).toEqual({ done: 2, total: 4 });
    });

    it('reports an empty probe list as complete rather than NaN', () => {
        expect(countProbeProgress([])).toEqual({ done: 0, total: 0 });
    });

    it('offers only available models for selection', () => {
        const entries: ShareModelEntry[] = [
            { model: 'a', state: 'available' },
            { model: 'b', state: 'unavailable' },
            { model: 'c', state: 'pending' },
            { model: 'd', state: 'probing' },
        ];
        expect(selectableShareModels(entries).map((entry) => entry.model)).toEqual(['a']);
    });

    it('produces a stable, non-reversible fingerprint for the same key', async () => {
        const first = await fingerprintProviderKey('sk-abc', 'https://api.x/v1');
        const second = await fingerprintProviderKey('sk-abc', 'https://api.x/v1');
        expect(first).toBe(second);
        expect(first).not.toContain('sk-abc');
        expect(first.length).toBeGreaterThan(0);
    });

    it('changes the fingerprint when the key or the endpoint changes', async () => {
        const base = await fingerprintProviderKey('sk-abc', 'https://api.x/v1');
        expect(await fingerprintProviderKey('sk-abd', 'https://api.x/v1')).not.toBe(base);
        // The endpoint is bound in: the same key against a different host is a
        // different publication, matching the server's api_url binding.
        expect(await fingerprintProviderKey('sk-abc', 'https://api.y/v1')).not.toBe(base);
    });

    it('labels the confirm dialog with both the provider and its endpoint', () => {
        expect(displayProviderLabel('OpenAI', 'https://api.x/v1')).toBe('OpenAI · https://api.x/v1');
    });
});

describe('modelsMissingFromShare', () => {
    it('returns probed models that are not already shared', () => {
        expect(modelsMissingFromShare(['Llama', 'Qwen', 'llama'], ['llama'])).toEqual(['Qwen']);
    });

    it('returns nothing when nothing new is available', () => {
        expect(modelsMissingFromShare([], ['llama'])).toEqual([]);
        expect(modelsMissingFromShare(['llama'], [])).toEqual(['llama']);
    });

    it('treats the same endpoint as the same provider', () => {
        expect(sameTokenBankAPI('https://API.Example/v1/', 'https://api.example/v1')).toBe(true);
        expect(sameTokenBankAPI('https://api.example/v1', 'https://other.example/v1')).toBe(false);
    });

    it('matches a share credential across a trailing slash and ignores a different key', async () => {
        const fingerprint = await fingerprintProviderKey('sk-abc', 'https://api.example/v1');
        const index = await matchTokenBankShareCredential(
            [
                { url: 'https://other.example/v1', key: 'sk-abc' },
                { url: 'https://api.example/v1/', key: 'sk-abc' },
            ],
            'https://api.example/v1',
            fingerprint,
        );
        expect(index).toBe(1);
        expect(await matchTokenBankShareCredential(
            [{ url: 'https://api.example/v1', key: 'sk-other' }],
            'https://api.example/v1',
            fingerprint,
        )).toBe(-1);
    });

    it('keeps catalog spellings and appends shared models the catalog dropped', () => {
        expect(unionShareModelNames(['Hy3', 'hy3', 'deepseek'], ['hy3', 'retired'])).toEqual(['Hy3', 'deepseek', 'retired']);
    });

    it('keeps a stored dial window and treats a missing enabled flag as still shared', () => {
        const models = extractTokenBankModels({
            models: [
                { model_name: 'hy3', enabled: false, share_window: { days: [1, 2, 3, 4, 5], start: '22:00', end: '08:00' } },
                { model_name: 'old' },
            ],
        });
        expect(models[0].enabled).toBe(false);
        expect(models[0].share_window).toEqual({ days: [1, 2, 3, 4, 5], start: '22:00', end: '08:00' });
        expect(models[1].enabled).toBe(true);
        expect(models[1].share_window).toBeNull();
        const details = extractTokenBankModelDetails({
            models: [],
            usage_models: [{ model_name: 'gpt-old', enabled: false }],
        });
        expect(details[0].history_only).toBe(true);
        expect(details[0].enabled).toBe(false);
    });
});

describe('tokenBankDisplayURL', () => {
    it('hides a URL username and password and keeps a URL that has none', () => {
        expect(tokenBankDisplayURL(' https://user:secret@api.kimi.com/coding/v1 ')).toBe('https://api.kimi.com/coding/v1');
        expect(tokenBankDisplayURL('https://user@api.kimi.com/coding/v1/')).toBe('https://api.kimi.com/coding/v1/');
        expect(tokenBankDisplayURL(' https://b.example/coding/v1/ ')).toBe('https://b.example/coding/v1/');
        expect(tokenBankDisplayURL('https://api.example/v1?api_key=sk-hidden&x=1')).toBe('https://api.example/v1?x=1');
        expect(tokenBankDisplayURL('https://api.example/v1?x=1')).toBe('https://api.example/v1?x=1');
        expect(tokenBankDisplayURL('not a url')).toBe('not a url');
    });
});

describe('giftAmountRejection', () => {
    // The server is the authority; this helper only lets the form name the
    // reason before the user presses the button. The important property is that
    // it agrees with the server: an amount it accepts must not be one the
    // create endpoint refuses.
    const summary = {
        earned_micro: 0, received_micro: 0, withdrawn_micro: 0, granted_micro: 0,
        frozen_micro: 0, available_micro: 10_000_000, gift_share_cap_micro: 5_000_000,
        hub_count: 1, auto_withdraw_limit_micro: 10_000_000,
        gift_share_percent: 50, gift_link_ttl_seconds: 604800,
        min_share_credits: 1, share_creates_today: 0,
        rank: 0, rank_badge: '', lifetime_badge: '',
    };

    it('accepts an amount inside the cap and above the floor', () => {
        expect(giftAmountRejection(3_000_000, summary)).toBe(null);
    });

    it('accepts the exact cap and the exact floor', () => {
        expect(giftAmountRejection(5_000_000, summary)).toBe(null);
        expect(giftAmountRejection(1_000_000, summary)).toBe(null);
    });

    it('refuses an amount over the cap', () => {
        expect(giftAmountRejection(5_000_001, summary)).toBe('over_cap');
    });

    it('refuses dust below the configured floor', () => {
        // Regression: `credit_share_min_credits` was stored but never enforced.
        // 0.5 credits is well inside the 50% cap, so only the floor can refuse
        // it — which makes this assertion specific to that rule.
        expect(giftAmountRejection(500_000, summary)).toBe('below_minimum');
    });

    it('treats a zero floor as "no floor"', () => {
        const noFloor = { ...summary, min_share_credits: 0 };
        expect(giftAmountRejection(500_000, noFloor)).toBe(null);
    });

    it('reports no balance rather than over-cap when the cap is zero', () => {
        // Both are true at once (anything > 0 exceeds a zero cap), and
        // "no balance" is the useful advice: telling someone with nothing that
        // they exceeded their limit is nonsense.
        const empty = { ...summary, available_micro: 0, gift_share_cap_micro: 0 };
        expect(giftAmountRejection(1_000_000, empty)).toBe('no_balance');
    });

    it('refuses empty and non-numeric input', () => {
        expect(giftAmountRejection(null, summary)).toBe('empty');
        expect(giftAmountRejection(0, summary)).toBe('empty');
        expect(giftAmountRejection(Number.NaN, summary)).toBe('empty');
    });

    it('says no balance when there is no summary at all', () => {
        expect(giftAmountRejection(1_000_000, null)).toBe('no_balance');
    });
});
