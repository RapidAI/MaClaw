import { describe, expect, it } from 'vitest';
import { RETURNED_HANDOFF_GRACE_MS, isMachineAttention, returnedHandoffLive, returnedHandoffVerdict, visibleDesktopText, watchMayRevokeGrant } from './desktopHandoff';

describe('desktop handoff', () => {
    it('turns a captcha reason code into a sentence the person can act on', () => {
        expect(isMachineAttention('captcha_widget')).toBe(true);
        expect(isMachineAttention('请完成验证')).toBe(false);
        expect(visibleDesktopText('captcha_widget', 'zh-Hans')).toContain('拖动滑块');
        expect(visibleDesktopText('captcha_widget', 'zh-Hant')).toContain('拖動滑塊');
        expect(visibleDesktopText('captcha_widget', 'en')).toContain('slider');
        expect(visibleDesktopText('登录好了', 'zh-Hans')).toBe('登录好了');
    });

    it('keeps a return alive while its continuation runs, then lets it lapse', () => {
        const closed = { at: 1_000, reason: 'captcha_widget' };
        expect(returnedHandoffLive(closed, 1_000 + RETURNED_HANDOFF_GRACE_MS - 1, false)).toBe(true);
        expect(returnedHandoffLive(closed, 1_000 + RETURNED_HANDOFF_GRACE_MS + 1, false)).toBe(false);
        expect(returnedHandoffLive(closed, 1_000_000, true)).toBe(true);
        expect(returnedHandoffLive(undefined, 0, true)).toBe(false);
    });

    it('holds the returned episode and opens a new ask from a later turn', () => {
        const closed = { at: 2_000, reason: 'captcha_widget' };
        // The watch still sees the old captcha: same episode.
        expect(returnedHandoffVerdict(closed, { reason: 'captcha_widget', userControl: true, reported: true })).toBe('hold');
        // An older job's report of the same reason: same episode.
        expect(returnedHandoffVerdict(closed, { reason: 'captcha_widget', userControl: true, reported: true, turnStartedAt: 1_500 })).toBe('hold');
        // The continuation itself hit the captcha again: a new handoff.
        expect(returnedHandoffVerdict(closed, { reason: 'captcha_widget', userControl: true, reported: true, turnStartedAt: 2_500 })).toBe('end');
        // An older job asks for something else: a new handoff.
        expect(returnedHandoffVerdict(closed, { reason: 'payment_confirm', userControl: true, reported: true, turnStartedAt: 1_500 })).toBe('end');
        // An explicit quiet desktop ends it; a payload without screen fields says nothing.
        expect(returnedHandoffVerdict(closed, { reason: '', userControl: false, reported: true })).toBe('end');
        expect(returnedHandoffVerdict(closed, { reason: '', userControl: false, reported: false })).toBe('pass');
        expect(returnedHandoffVerdict(undefined, { reason: 'captcha_widget', userControl: true, reported: true })).toBe('pass');
    });

    it('ignores a watch that started before the keyboard was claimed', () => {
        expect(watchMayRevokeGrant(1_000, 2_000)).toBe(false);
        expect(watchMayRevokeGrant(2_000, 2_000)).toBe(true);
        expect(watchMayRevokeGrant(3_000, 0)).toBe(false);
    });
});
