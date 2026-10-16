/**
 * Pure helpers for AppsPage one-click AppView workspace launch feedback.
 * Kept free of React so Vitest can cover message mapping without full page mounts.
 */

export type WorkspaceLaunchResult = {
    app_view_opened?: boolean;
    reason?: string;
    app_view_error?: string;
    [key: string]: unknown;
};

export function isEnterpriseAppKindForWorkspace(kind: string | undefined | null): boolean {
    const k = String(kind || '').trim();
    return k === 'enterprise_normal_app' || k === 'enterprise_approval_app';
}

/**
 * Returns null when the workspace opened successfully (or result is a success).
 * Otherwise returns a localized user-facing message.
 */
export function formatWorkspaceLaunchIssue(
    appName: string,
    result: WorkspaceLaunchResult | null | undefined,
    lang?: string,
): string | null {
    if (!result || result.app_view_opened === true) {
        return null;
    }
    const zh = !lang || lang.startsWith('zh');
    const name = String(appName || '').trim() || (zh ? '应用' : 'App');
    const reason = String(result.reason || result.app_view_error || '').trim();
    if (reason === 'no_approval_instances') {
        return zh
            ? `「${name}」暂无待办审批实例，已打开应用页。`
            : `"${name}" has no pending approval instances; app page opened.`;
    }
    if (reason) {
        return zh
            ? `「${name}」工作区未打开：${reason}`
            : `"${name}" workspace did not open: ${reason}`;
    }
    return zh
        ? `「${name}」工作区未打开（可能未启用 MIS 数据服务或未配置 DataSrv 绑定）。`
        : `"${name}" workspace did not open (MIS may be off or DataSrv bindings missing).`;
}

export function formatWorkspaceLaunchError(
    appName: string,
    error: unknown,
    lang?: string,
): string {
    const zh = !lang || lang.startsWith('zh');
    const name = String(appName || '').trim() || (zh ? '应用' : 'App');
    const detail = String((error as { message?: string })?.message || error || '').trim();
    return zh
        ? `「${name}」工作区打开失败${detail ? `：${detail}` : '。'}`
        : `"${name}" workspace open failed${detail ? `: ${detail}` : '.'}`;
}

/** Clear one app id from the per-tile issue map. */
export function clearWorkspaceLaunchIssue(
    byAppId: Record<string, string>,
    appId: string,
): Record<string, string> {
    if (!byAppId[appId]) return byAppId;
    const next = { ...byAppId };
    delete next[appId];
    return next;
}

/** Set one app's launch issue message. */
export function setWorkspaceLaunchIssue(
    byAppId: Record<string, string>,
    appId: string,
    message: string,
): Record<string, string> {
    return { ...byAppId, [appId]: message };
}
