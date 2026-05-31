function appendTenantQuery(path: string, tenantID?: string) {
    const tenant = String(tenantID || '').trim();
    if (!tenant) return path;
    return `${path}?tenant_id=${encodeURIComponent(tenant)}`;
}

export function buildHubCreditsURL(hubURL?: string, viewerToken?: string, tenantID?: string) {
    const base = String(hubURL || '').trim().replace(/\/+$/, '');
    const token = String(viewerToken || '').trim();
    if (!base) return '';
    const creditsURL = appendTenantQuery(`${base}/get-credits`, tenantID);
    return token ? `${creditsURL}#token=${encodeURIComponent(token)}` : creditsURL;
}

export function buildHubCardStoreURL(hubURL?: string, tenantID?: string) {
    const base = String(hubURL || '').trim().replace(/\/+$/, '');
    if (!base) return '';
    return appendTenantQuery(`${base}/card_store`, tenantID);
}
