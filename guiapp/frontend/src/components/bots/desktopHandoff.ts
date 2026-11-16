// One login or captcha handoff. The person operates the desktop once, then
// one continue gives it back. The same reason must not open a second task.

const MACHINE_ATTENTION = /^(login_wall|captcha_widget|mfa|login_qr|payment_confirm|consent_dialog|computer_takeover|login)$/i;

export function isMachineAttention(text: string | undefined | null): boolean {
    return MACHINE_ATTENTION.test(String(text || '').trim());
}

function scriptOf(lang: string): 'en' | 'hant' | 'hans' {
    const value = (lang || '').toLowerCase();
    if (value.startsWith('en')) return 'en';
    if (value.includes('hant') || value === 'zh-tw' || value === 'zh-hk') return 'hant';
    return 'hans';
}

// A reason code is not something to show. The person sees what to do instead.
export function visibleDesktopText(content: string, lang: string): string {
    const text = content || '';
    if (!isMachineAttention(text)) return text;
    const script = scriptOf(lang);
    const reason = text.trim().toLowerCase();
    if (reason === 'captcha_widget') {
        if (script === 'en') return 'Finish the check on this screen — drag the slider or complete the puzzle. Then press Verified, continue.';
        if (script === 'hant') return '請直接在這個畫面裡完成驗證（拖動滑塊或拼圖）。完成後點「驗證完成，繼續」。';
        return '请直接在这个画面里完成验证（拖动滑块或拼图）。完成后点「验证完成，继续」。';
    }
    if (reason === 'payment_confirm') {
        if (script === 'en') return 'Finish the payment in this browser. The bot continues after that.';
        if (script === 'hant') return '請在這個桌面的瀏覽器裡完成支付。完成後這個 bot 會接著操作。';
        return '请在这个桌面的浏览器里完成支付。完成后这个 bot 会接着操作。';
    }
    if (script === 'en') return 'Finish the login or verification in this browser. The login stays here for the bot.';
    if (script === 'hant') return '請在這個桌面的瀏覽器裡完成登入或驗證。登入會留在這個瀏覽器裡，完成後交還。';
    return '请在这个桌面的浏览器里完成登录或验证。登录会留在这个浏览器里，完成后交还。';
}

// A handoff the person gave back. It belongs to the continuation that return
// started, not to a reason string: the page keeps showing the old captcha for
// a moment after the return, and a later real captcha can carry the very same
// reason code.
export type ReturnedHandoff = { at: number; reason: string };

// Long enough for the continuation's bubble or run mark to show up. Past it,
// with nothing running, the return is over and reports count again.
export const RETURNED_HANDOFF_GRACE_MS = 15_000;

export function returnedHandoffLive(closed: ReturnedHandoff | undefined, now: number, continuationAlive: boolean): boolean {
    if (!closed) return false;
    return continuationAlive || now - closed.at < RETURNED_HANDOFF_GRACE_MS;
}

// What one desktop report means while a return is live.
// pass: says nothing about the handoff. hold: the returned episode, drop it.
// end: the return is over (an explicit quiet desktop, or a turn started at or
// after the return asking for the person again — the continuation itself
// counts: its bubble is created at the return, so a new ask on it is a new
// episode, not the old one); the report applies as it is.
export type HandoffReport = { reason: string; userControl: boolean; reported: boolean; turnStartedAt?: number };
export function returnedHandoffVerdict(closed: ReturnedHandoff | undefined, report: HandoffReport): 'pass' | 'hold' | 'end' {
    if (!closed) return 'pass';
    const reason = (report.reason || '').trim();
    if (!report.userControl && reason === '') return report.reported ? 'end' : 'pass';
    const started = report.turnStartedAt || 0;
    if (started >= closed.at) return 'end';
    if (started > 0 && reason !== '' && reason !== closed.reason) return 'end';
    return 'hold';
}

// A watch that left before the claim answered still says the keyboard is free.
// That report must not take the keyboard back.
export function watchMayRevokeGrant(watchStartedAt: number, claimedAt: number): boolean {
    if (!(claimedAt > 0)) return false;
    return watchStartedAt >= claimedAt;
}
