// Plain words for why a 唤醒 click did not bring the computer up. The raw
// Hub/desktop error is kept (clipped) when no known cause matches, so the
// person never gets a bare spinner or a silent return to sleep.
export function wakeFailureReason(err: unknown): string {
    const raw = String((err as { message?: unknown } | null)?.message ?? err ?? '').trim();
    const low = raw.toLowerCase();
    if (!raw) return '电脑没有返回画面，请稍后再试';
    if (low.includes('no free desktop display')) return '没有空闲的屏幕，请稍后再试';
    if (low.includes('deadline exceeded') || low.includes('timeout') || low.includes('timed out')) return '等了一分钟电脑仍没有起来';
    if (low.includes('not open yet')) return '电脑还没有打开';
    if (low.includes('connection refused') || low.includes('unreachable') || low.includes('502') || low.includes('503')) return '电脑服务暂时连不上';
    const clipped = raw.replace(/\s+/g, ' ');
    return clipped.length > 120 ? clipped.slice(0, 120) + '…' : clipped;
}
