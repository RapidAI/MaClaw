import { ReportHubLLMBillingTimezone } from '../../wailsjs/go/main/App';

type RemoteHubConfig = {
    remote_hub_url?: unknown;
    remote_viewer_token?: unknown;
    remote_user_id?: unknown;
    remote_email?: unknown;
};

// Backfill legacy accounts on the first authenticated GUI launch. This stays
// outside the credits panel so a user who never opens that panel is still
// assigned the account's one-time billing timezone.
export function reportBillingTimezoneOnAuthenticatedLaunch(config: unknown, reportedForAccount: { current: string }): void {
    const cfg = (config || {}) as RemoteHubConfig;
    const hubURL = String(cfg.remote_hub_url || '').trim();
    const viewerToken = String(cfg.remote_viewer_token || '').trim();
    const accountID = String(cfg.remote_user_id || cfg.remote_email || '').trim();
    const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (!hubURL || !viewerToken || !accountID || !timezone) return;
    const key = `${hubURL}\n${accountID}\n${timezone}`;
    if (reportedForAccount.current === key) return;
    reportedForAccount.current = key;
    try {
        void Promise.resolve(ReportHubLLMBillingTimezone(timezone)).catch(() => undefined);
    } catch {
        // Wails bindings may be unavailable in a browser-only frontend test.
    }
}
