// An intranet desktop offers a direct picture. Probe it once, briefly, and
// keep the Hub address when that probe does not answer.

const remembered = new Map<string, { ok: boolean; at: number }>();

export async function preferDirectDesktop(direct: string, fallback: string, probe?: typeof fetch): Promise<string> {
    const target = direct.trim();
    const hub = fallback.trim();
    if (!target || !/^https?:\/\//i.test(target)) return hub;
    const now = Date.now();
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

export function desktopPictureURL(view: { novnc_url?: string; direct_url?: string } | null | undefined, probe?: typeof fetch): Promise<string> {
    return preferDirectDesktop(String(view?.direct_url || ''), String(view?.novnc_url || ''), probe);
}
