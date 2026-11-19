// An intranet desktop offers a direct picture. Probe it once, briefly, and
// keep the Hub address when that probe does not answer. desktopd allows the
// GUI page to read this status; an opaque failure stays on the Hub address.

const remembered = new Map<string, { ok: boolean; at: number }>();
const blockedUntil = new Map<string, number>();
const directHub = new Map<string, string>();

// noteDirectDesktopDown keeps a picture that just failed on the Hub address.
// The page itself can still answer, so a fresh probe would walk straight back.
export function noteDirectDesktopDown(direct: string): void {
    const target = direct.trim();
    if (!target) return;
    blockedUntil.set(target, Date.now() + 120_000);
    remembered.delete(target);
}

// directPictureGaveUp is the player telling us its socket did not come back.
// The returned address is the Hub page remembered when this direct picture was chosen.
export function directPictureGaveUp(current: string): string {
    const target = current.trim();
    const hub = directHub.get(target) || '';
    if (!target || !hub || hub === target) return target;
    noteDirectDesktopDown(target);
    directHub.delete(target);
    return hub;
}

export async function preferDirectDesktop(direct: string, fallback: string, probe?: typeof fetch): Promise<string> {
    const target = direct.trim();
    const hub = fallback.trim();
    if (!target || !/^https?:\/\//i.test(target)) return hub;
    const now = Date.now();
    const until = blockedUntil.get(target);
    if (until && until > now) return hub;
    if (until) blockedUntil.delete(target);
    const hit = remembered.get(target);
    if (hit && now - hit.at < 30_000) return hit.ok ? target : hub;
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 1000);
    let ok = false;
    try {
        const resp = await (probe || fetch)(target, { method: 'GET', signal: ctrl.signal, cache: 'no-store' });
        ok = resp.ok;
    } catch {
        ok = false;
    } finally {
        clearTimeout(timer);
    }
    remembered.set(target, { ok, at: now });
    return ok ? target : hub;
}

export async function desktopPictureURL(view: { novnc_url?: string; direct_url?: string } | null | undefined, probe?: typeof fetch): Promise<string> {
    const hub = String(view?.novnc_url || '').trim();
    const direct = String(view?.direct_url || '').trim();
    const url = await preferDirectDesktop(direct, hub, probe);
    if (url && direct && url === direct && hub && url !== hub) directHub.set(url, hub);
    return url;
}
