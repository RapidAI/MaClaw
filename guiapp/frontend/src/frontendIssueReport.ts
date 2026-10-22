// Sends a short, content-free note about a frontend failure to maclaw.log
// through LogFrontendDiagnostic, so a crash or a dead click is visible in the
// logs Daniel sends. Never pass message text or user content here.
export function reportFrontendIssue(tag: string, detail: string, extra: Record<string, unknown> = {}) {
    try {
        const log = (window as any)?.go?.main?.App?.LogFrontendDiagnostic;
        if (typeof log !== 'function') return;
        const payload = { tag: 'frontend-issue', area: tag, detail: String(detail || '').slice(0, 240), ...extra };
        void Promise.resolve(log(payload)).catch(() => undefined);
    } catch {
        // reporting must never throw
    }
}

let installed = false;

// installFrontendIssueReporting forwards uncaught errors and rejections once.
export function installFrontendIssueReporting() {
    if (installed || typeof window === 'undefined') return;
    installed = true;
    let sent = 0;
    const budget = () => (sent += 1) <= 50;
    window.addEventListener('error', event => {
        if (!budget()) return;
        const err = event.error as Error | undefined;
        reportFrontendIssue('window-error', `${err?.name || 'Error'}: ${err?.message || event.message || ''}`, {
            source: String(event.filename || '').split('/').pop(), line: event.lineno, stack: String(err?.stack || '').split('\n').slice(0, 4).join(' | ').slice(0, 240),
        });
    });
    window.addEventListener('unhandledrejection', event => {
        if (!budget()) return;
        const reason = event.reason as Error | undefined;
        reportFrontendIssue('unhandled-rejection', `${reason?.name || ''}: ${reason?.message || String(event.reason || '')}`.slice(0, 240));
    });
}
