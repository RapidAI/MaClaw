export function isDigitalEmployeeAuthorizationUsable(auth: any, nowMs = Date.now()): boolean {
    if (auth?.active !== true) return false;
    if (Number(auth?.quota || 0) <= 0) return false;
    const expiresAt = String(auth?.expires_at || '').trim();
    if (!expiresAt) return false;
    const expiresAtMs = new Date(expiresAt).getTime();
    if (!Number.isFinite(expiresAtMs) || expiresAtMs <= nowMs) return false;
    return true;
}

export function shouldShowDigitalEmployeeFeatureTabs(status: any, nowMs = Date.now()): boolean {
    if (!status?.visible) return false;
    if (!isDigitalEmployeeAuthorizationUsable(status?.authorization, nowMs)) return false;
    if (Number(status?.actual_count || 0) <= 0) return false;
    return true;
}
