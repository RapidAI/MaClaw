/**
 * Pure helpers for AppsPage one-click AppView workspace launch feedback.
 * Kept free of React so Vitest can cover message mapping without full page mounts.
 */
import { tailText } from '../../i18n/mnTail';

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
    const name = String(appName || '').trim() || tailText(lang, 'App', '应用');
    const reason = String(result.reason || result.app_view_error || '').trim();
    if (reason === 'no_approval_instances') {
        return tailText(
            lang,
            `"${name}" has no pending approval instances; app page opened.`,
            `「${name}」暂无待办审批实例，已打开应用页。`,
        );
    }
    if (reason) {
        return tailText(
            lang,
            `"${name}" workspace did not open: ${reason}`,
            `「${name}」工作区未打开：${reason}`,
        );
    }
    return tailText(
        lang,
        `"${name}" workspace did not open (MIS may be off or DataSrv bindings missing).`,
        `「${name}」工作区未打开（可能未启用 MIS 数据服务或未配置 DataSrv 绑定）。`,
    );
}

export function formatWorkspaceLaunchError(
    appName: string,
    error: unknown,
    lang?: string,
): string {
    const name = String(appName || '').trim() || tailText(lang, 'App', '应用');
    const detail = String((error as { message?: string })?.message || error || '').trim();
    if (detail) {
        return tailText(
            lang,
            `"${name}" workspace open failed: ${detail}`,
            `「${name}」工作区打开失败：${detail}`,
        );
    }
    return tailText(lang, `"${name}" workspace open failed.`, `「${name}」工作区打开失败。`);
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
