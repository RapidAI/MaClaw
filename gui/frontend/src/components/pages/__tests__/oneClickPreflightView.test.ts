import { describe, expect, it } from 'vitest';

// Mirror pure summarizer behavior from AppsPage (kept local to avoid exporting
// large module surface). If summarizer logic drifts, update both.

type OneClickPreflightTone = 'ready' | 'warn' | 'blocked' | 'loading' | 'unknown';

function formatOneClickPreflightHint(preflight: Record<string, unknown> | null | undefined): string {
    if (!preflight) return '';
    const blocking = Array.isArray(preflight.blocking) ? preflight.blocking.map(String).filter(Boolean) : [];
    if (blocking.length > 0) return blocking.slice(0, 2).join(' · ');
    const warnings = Array.isArray(preflight.warnings) ? preflight.warnings.map(String).filter(Boolean) : [];
    if (warnings.length > 0) return warnings.slice(0, 2).join(' · ');
    return String(preflight.message || '').trim();
}

function summarizeOneClickPreflightView(
    preflight: Record<string, unknown> | null | undefined,
    text: {
        oneClickRemoteReady: string;
        oneClickRemoteWarn: string;
        oneClickRemoteBlocked: string;
        oneClickRemoteLoading: string;
        oneClickRemoteUnavailable: string;
    },
    options?: { loading?: boolean },
): { tone: OneClickPreflightTone; title: string; detail: string; readyLocal: boolean; readySkill: boolean; readyHub: boolean; lines: Array<{ id: string }> } {
    if (options?.loading) {
        return {
            tone: 'loading',
            title: text.oneClickRemoteLoading,
            detail: '',
            readyLocal: false,
            readySkill: false,
            readyHub: false,
            lines: [],
        };
    }
    if (!preflight) {
        return {
            tone: 'unknown',
            title: text.oneClickRemoteUnavailable,
            detail: '',
            readyLocal: false,
            readySkill: false,
            readyHub: false,
            lines: [],
        };
    }
    const readyLocal = preflight.ready_for_local === true;
    const readySkill = preflight.ready_for_skill_market === true;
    const readyHub = preflight.ready_for_hub_pack === true;
    const lines: Array<{ id: string }> = [];
    if (Array.isArray(preflight.checks)) {
        for (const raw of preflight.checks) {
            if (!raw || typeof raw !== 'object') continue;
            const row = raw as Record<string, unknown>;
            const id = String(row.id || '').trim();
            if (!id) continue;
            if (!['package_ready', 'dependencies', 'skill_market_email', 'hub_enrollment', 'enterprise_hub_market', 'skill_market_upload'].includes(id)) {
                continue;
            }
            lines.push({ id });
        }
    }
    let tone: OneClickPreflightTone = 'ready';
    let title = text.oneClickRemoteReady;
    if (!readyLocal) {
        tone = 'blocked';
        title = text.oneClickRemoteBlocked;
    } else if (!readySkill || !readyHub) {
        tone = 'warn';
        title = text.oneClickRemoteWarn;
    }
    return {
        tone,
        title,
        detail: formatOneClickPreflightHint(preflight) || String(preflight.message || '').trim(),
        readyLocal,
        readySkill,
        readyHub,
        lines: lines.slice(0, 6),
    };
}

const text = {
    oneClickRemoteReady: 'Remote targets ready',
    oneClickRemoteWarn: 'Remote targets may partially fail',
    oneClickRemoteBlocked: 'Publish blocked',
    oneClickRemoteLoading: 'Checking remote readiness…',
    oneClickRemoteUnavailable: 'Remote preflight unavailable',
};

describe('summarizeOneClickPreflightView', () => {
    it('marks loading and unavailable states', () => {
        expect(summarizeOneClickPreflightView(null, text, { loading: true }).tone).toBe('loading');
        expect(summarizeOneClickPreflightView(null, text).tone).toBe('unknown');
    });

    it('is ready when all remote flags are true', () => {
        const view = summarizeOneClickPreflightView({
            ready_for_local: true,
            ready_for_skill_market: true,
            ready_for_hub_pack: true,
            message: 'ready for one-click',
            checks: [
                { id: 'dependencies', ok: true, message: 'all ok' },
                { id: 'config', ok: true, message: 'ignore me' },
            ],
        }, text);
        expect(view.tone).toBe('ready');
        expect(view.readyLocal).toBe(true);
        expect(view.lines.map((l) => l.id)).toEqual(['dependencies']);
    });

    it('warns when hub pack is not ready but local is', () => {
        const view = summarizeOneClickPreflightView({
            ready_for_local: true,
            ready_for_skill_market: true,
            ready_for_hub_pack: false,
            warnings: ['dependencies: missing skill'],
            checks: [{ id: 'dependencies', ok: false, severity: 'warn', message: 'missing skill' }],
        }, text);
        expect(view.tone).toBe('warn');
        expect(view.detail).toContain('missing skill');
    });

    it('blocks when local readiness fails', () => {
        const view = summarizeOneClickPreflightView({
            ready_for_local: false,
            ready_for_skill_market: false,
            ready_for_hub_pack: false,
            blocking: ['package_ready: missing evidence'],
        }, text);
        expect(view.tone).toBe('blocked');
        expect(view.title).toBe(text.oneClickRemoteBlocked);
        expect(view.detail).toContain('package_ready');
    });

    it('treats skill-only gap as warn (may-partial affordance)', () => {
        const view = summarizeOneClickPreflightView({
            ready_for_local: true,
            ready_for_skill_market: false,
            ready_for_hub_pack: true,
            warnings: ['skill_market_email: remote_email not configured'],
        }, text);
        expect(view.tone).toBe('warn');
        expect(view.readyLocal).toBe(true);
        expect(view.readySkill).toBe(false);
        expect(view.readyHub).toBe(true);
    });
});
