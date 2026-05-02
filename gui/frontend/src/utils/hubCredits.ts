export function buildHubCreditsURL(hubURL?: string, viewerToken?: string) {
    const base = String(hubURL || '').trim().replace(/\/+$/, '');
    const token = String(viewerToken || '').trim();
    if (!base || !token) return '';
    return `${base}/get-credits#token=${encodeURIComponent(token)}`;
}
