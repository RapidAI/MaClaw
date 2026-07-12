import React, { useEffect, useState } from 'react';
import { useSafeBackdropDismiss } from '../hooks/useSafeBackdropDismiss';

type DoctorStatus = 'ok' | 'warn' | 'fail' | 'skip' | 'info' | string;

interface DoctorCheck {
    id: string;
    status: DoctorStatus;
    message: string;
    hint?: string;
    detail?: Record<string, unknown>;
}

interface DoctorReport {
    ok: boolean;
    summary: string;
    config_path?: string;
    base_dir?: string;
    blockers?: number;
    warnings?: number;
    checks?: DoctorCheck[];
}

interface SharedLoopRoute {
    task?: string;
    source?: string;
    model?: string;
    reason?: string;
    escalated?: boolean;
    cost_tier?: string;
    cost_route_mode?: string;
    cost_route_applied?: boolean;
    thinking_policy?: string;
    baseline_model?: string;
}

interface TurnUsage {
    model?: string;
    provider?: string;
    input_tokens?: number;
    output_tokens?: number;
    cached_tokens?: number;
    cache_write_tokens?: number;
    est_cost_rmb?: number;
    est_cost_usd?: number;
    requests?: number;
}

interface SharedAgentLoopStatus {
    mode?: string;
    percent?: number;
    workflow_pilot?: boolean;
    config_enabled?: boolean;
    config_migrated?: boolean;
    default_enabled?: boolean;
    env_override?: string;
    env_locks_mode?: boolean;
    percent_from_env?: boolean;
    workflow_from_env?: boolean;
    config_canary_percent?: number | null;
    config_workflow?: boolean;
    shared_turns?: number;
    legacy_turns?: number;
    shared_success?: number;
    shared_error?: number;
    shared_cancelled?: number;
    skip_canary?: number;
    skip_ineligible?: number;
    shadow_eligible?: number;
    skip_by_reason?: Record<string, number>;
    last_skip_reason?: string;
    last_skip_at?: string;
    last_shared_at?: string;
    last_legacy_at?: string;
    last_route?: SharedLoopRoute;
    last_usage?: TurnUsage;
    process_usage?: TurnUsage;
    prompt_light_turns?: number;
    prompt_full_turns?: number;
    prompt_light_percent?: number;
    prompt_est_tokens_saved?: number;
    last_prompt_profile?: string;
    last_prompt_at?: string;
    last_prompt_saved_tokens?: number;
    last_prompt_task?: string;
    last_prompt_reason?: string;
    prompt_by_task?: Record<string, number>;
    prompt_light_tool_denies?: number;
    prompt_last_denied_tool?: string;
    prompt_by_denied_tool?: Record<string, number>;
    prompt_light_upgrades?: number;
    prompt_last_upgrade_reason?: string;
    prompt_ab_eligible_light?: number;
    prompt_ab_sample_full?: number;
    prompt_ab_sample_percent?: number;
    prompt_upgrade_rate_percent?: number;
    prompt_deny_rate_percent?: number;
    prompt_profile_env?: string;
    prompt_profile_forced?: string;
    light_retry_enabled?: boolean;
    hub_connected?: boolean;
    hub_url?: string;
    hub_adaptive_summary?: string;
    export_dir?: string;
    process_started_at?: string;
}

/** Compact count map: "bash:2,write_file:1" (top maxKeys). */
function formatCountMapHint(
    by?: Record<string, number> | null,
    maxKeys = 3,
): string {
    const entries = Object.entries(by || {})
        .map(([k, v]) => [String(k || '').trim(), Number(v) || 0] as const)
        .filter(([k, v]) => k && v > 0)
        .sort((a, b) => (b[1] !== a[1] ? b[1] - a[1] : a[0].localeCompare(b[0])));
    if (entries.length === 0) return '';
    return entries
        .slice(0, Math.max(1, maxKeys))
        .map(([k, v]) => `${k}:${v}`)
        .join(',');
}

/** Compact light-deny breakdown: (bash:2) or (bash:2+1tools) */
function formatDeniedToolHint(
    by?: Record<string, number> | null,
    last?: string | null,
): string {
    const entries = Object.entries(by || {})
        .map(([k, v]) => [String(k || '').trim(), Number(v) || 0] as const)
        .filter(([k, v]) => k && v > 0)
        .sort((a, b) => (b[1] !== a[1] ? b[1] - a[1] : a[0].localeCompare(b[0])));
    if (entries.length === 0) {
        const l = String(last || '').trim();
        return l ? `(${l})` : '';
    }
    const [topK, topV] = entries[0];
    if (entries.length === 1) {
        return `(${topK}:${topV})`;
    }
    return `(${topK}:${topV}+${entries.length - 1}tools)`;
}

function compactUpgradeReason(reason?: string | null): string {
    let r = String(reason || '').trim();
    if (!r) return '';
    if (r.startsWith('tool_deny_retry:')) {
        r = r.slice('tool_deny_retry:'.length).trim();
    }
    return r.length > 20 ? `${r.slice(0, 20)}…` : r;
}

function formatTurnUsage(u?: TurnUsage | null): string {
    if (!u) return '';
    const inTok = Number(u.input_tokens ?? 0);
    const outTok = Number(u.output_tokens ?? 0);
    const req = Number(u.requests ?? 0);
    const cache = Number(u.cached_tokens ?? 0);
    const cost = Number(u.est_cost_rmb ?? 0);
    const costUsd = Number(u.est_cost_usd ?? 0);
    if (inTok + outTok + req + cache + cost + costUsd === 0) return '';
    const parts: string[] = [
        `in=${inTok}`,
        `out=${outTok}`,
        `total=${inTok + outTok}`,
    ];
    if (cache > 0) parts.push(`cache=${cache}`);
    if (req > 0) parts.push(`req=${req}`);
    if (cost > 0) parts.push(`~¥${cost.toFixed(4)}`);
    else if (costUsd > 0) parts.push(`~$${costUsd.toFixed(4)}`);
    if (u.model) parts.push(`model ${u.model}`);
    return parts.join(' · ');
}

type Props = {
    open: boolean;
    onClose: () => void;
    t: (key: string) => string;
};

type CanaryPreview = {
    ok?: boolean;
    user_id?: string;
    percent?: number;
    bucket?: number;
    allows?: boolean;
    summary?: string;
    error?: string;
};

type AdaptiveExportResult = {
    ok?: boolean;
    path?: string;
    written?: string;
    summary?: string;
    exported_at?: string;
    host?: string;
    hint?: string;
    error?: string;
};

type GoApp = {
    RunDoctor?: () => Promise<DoctorReport>;
    GetSharedAgentLoopStatus?: () => Promise<SharedAgentLoopStatus>;
    SetSharedAgentLoopEnabled?: (enabled: boolean) => Promise<SharedAgentLoopStatus>;
    SetSharedAgentLoopCanaryPercent?: (percent: number) => Promise<SharedAgentLoopStatus>;
    SetSharedAgentLoopWorkflow?: (enabled: boolean) => Promise<SharedAgentLoopStatus>;
    ResetAdaptivePromptStats?: () => Promise<SharedAgentLoopStatus>;
    ExportAdaptivePromptStats?: () => Promise<AdaptiveExportResult>;
    OpenAdaptivePromptExportsDir?: () => Promise<{ ok?: boolean; path?: string; error?: string }>;
    PreviewSharedLoopCanary?: (userID: string, percent: number) => Promise<CanaryPreview>;
};

function getGoApp(): GoApp | undefined {
    return (window as unknown as { go?: { main?: { App?: GoApp } } }).go?.main?.App;
}

async function callRunDoctor(): Promise<DoctorReport> {
    const app = getGoApp();
    const fn = app?.RunDoctor;
    if (typeof fn !== 'function') {
        throw new Error('RunDoctor binding unavailable — rebuild GUI after wails generate');
    }
    return fn.call(app);
}

async function callSharedStatus(): Promise<SharedAgentLoopStatus | null> {
    const app = getGoApp();
    const fn = app?.GetSharedAgentLoopStatus;
    if (typeof fn !== 'function') {
        return null;
    }
    return fn.call(app);
}

async function callSetSharedEnabled(enabled: boolean): Promise<SharedAgentLoopStatus> {
    const app = getGoApp();
    const fn = app?.SetSharedAgentLoopEnabled;
    if (typeof fn !== 'function') {
        throw new Error('SetSharedAgentLoopEnabled binding unavailable');
    }
    return fn.call(app, enabled);
}

async function callSetSharedCanaryPercent(percent: number): Promise<SharedAgentLoopStatus> {
    const app = getGoApp();
    const fn = app?.SetSharedAgentLoopCanaryPercent;
    if (typeof fn !== 'function') {
        throw new Error('SetSharedAgentLoopCanaryPercent binding unavailable — rebuild GUI after wails generate');
    }
    return fn.call(app, percent);
}

async function callSetSharedWorkflow(enabled: boolean): Promise<SharedAgentLoopStatus> {
    const app = getGoApp();
    const fn = app?.SetSharedAgentLoopWorkflow;
    if (typeof fn !== 'function') {
        throw new Error('SetSharedAgentLoopWorkflow binding unavailable — rebuild GUI after wails generate');
    }
    return fn.call(app, enabled);
}

async function callResetAdaptivePromptStats(): Promise<SharedAgentLoopStatus> {
    const app = getGoApp();
    const fn = app?.ResetAdaptivePromptStats;
    if (typeof fn !== 'function') {
        throw new Error('ResetAdaptivePromptStats binding unavailable — rebuild GUI after wails generate');
    }
    return fn.call(app);
}

async function callExportAdaptivePromptStats(): Promise<AdaptiveExportResult> {
    const app = getGoApp();
    const fn = app?.ExportAdaptivePromptStats;
    if (typeof fn !== 'function') {
        throw new Error('ExportAdaptivePromptStats binding unavailable — rebuild GUI after wails generate');
    }
    return fn.call(app);
}

async function callOpenAdaptivePromptExportsDir(): Promise<{ ok?: boolean; path?: string; error?: string }> {
    const app = getGoApp();
    const fn = app?.OpenAdaptivePromptExportsDir;
    if (typeof fn !== 'function') {
        throw new Error('OpenAdaptivePromptExportsDir binding unavailable — rebuild GUI after wails generate');
    }
    return fn.call(app);
}

async function callPreviewSharedLoopCanary(userID: string, percent: number): Promise<CanaryPreview> {
    const app = getGoApp();
    const fn = app?.PreviewSharedLoopCanary;
    if (typeof fn !== 'function') {
        throw new Error('PreviewSharedLoopCanary binding unavailable — rebuild GUI after wails generate');
    }
    return fn.call(app, userID, percent);
}

function statusColor(status: DoctorStatus): string {
    switch (status) {
        case 'ok':
            return '#27ae60';
        case 'warn':
            return '#f39c12';
        case 'fail':
            return '#e74c3c';
        case 'skip':
            return '#95a5a6';
        default:
            return '#3498db';
    }
}

function modeColor(mode: string | undefined): string {
    switch ((mode || '').toLowerCase()) {
        case 'on':
            return '#27ae60';
        case 'shadow':
            return '#f39c12';
        default:
            return '#95a5a6';
    }
}

function PathBar({ shared, legacy }: { shared: number; legacy: number }) {
    const total = shared + legacy;
    if (total <= 0) {
        return (
            <div
                style={{
                    height: 8,
                    borderRadius: 4,
                    background: 'var(--theme-border)',
                    opacity: 0.5,
                }}
            />
        );
    }
    const sharedPct = Math.round((shared / total) * 100);
    return (
        <div
            style={{
                display: 'flex',
                height: 10,
                borderRadius: 5,
                overflow: 'hidden',
                background: 'var(--theme-border)',
            }}
            title={`shared ${sharedPct}% · legacy ${100 - sharedPct}%`}
        >
            <div style={{ width: `${sharedPct}%`, background: '#3498db' }} />
            <div style={{ width: `${100 - sharedPct}%`, background: '#95a5a6' }} />
        </div>
    );
}

export function SystemDoctorDialog({ open, onClose, t }: Props) {
    const [report, setReport] = useState<DoctorReport | null>(null);
    const [loopStatus, setLoopStatus] = useState<SharedAgentLoopStatus | null>(null);
    const [error, setError] = useState<string>('');
    const [loading, setLoading] = useState(false);
    const [toggling, setToggling] = useState(false);
    const [resettingStats, setResettingStats] = useState(false);
    const [canaryUser, setCanaryUser] = useState('');
    const [canaryPreview, setCanaryPreview] = useState<CanaryPreview | null>(null);
    const [canaryLoading, setCanaryLoading] = useState(false);
    const [exportingStats, setExportingStats] = useState(false);
    const [exportPath, setExportPath] = useState('');
    const [openingExports, setOpeningExports] = useState(false);
    const [canaryPercentDraft, setCanaryPercentDraft] = useState('100');
    const [savingCanary, setSavingCanary] = useState(false);
    const [savingWorkflow, setSavingWorkflow] = useState(false);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    const load = () => {
        setLoading(true);
        setError('');
        Promise.all([callRunDoctor(), callSharedStatus()])
            .then(([r, st]) => {
                setReport(r);
                setLoopStatus(st);
                if (st) {
                    // Env-locked: show effective percent. Otherwise prefer config draft.
                    const pct = st.percent_from_env
                        ? typeof st.percent === 'number'
                            ? st.percent
                            : 100
                        : typeof st.config_canary_percent === 'number'
                          ? st.config_canary_percent
                          : typeof st.percent === 'number'
                            ? st.percent
                            : 100;
                    setCanaryPercentDraft(String(pct));
                }
            })
            .catch((err: unknown) => {
                console.error('SystemDoctor load failed:', err);
                setReport(null);
                setLoopStatus(null);
                setError(err instanceof Error ? err.message : String(err));
            })
            .finally(() => setLoading(false));
    };

    const setSharedEnabled = (enabled: boolean) => {
        setToggling(true);
        setError('');
        callSetSharedEnabled(enabled)
            .then((st) => {
                setLoopStatus(st);
                // Refresh doctor checks so agent.shared_loop reflects config.
                return callRunDoctor().then((r) => setReport(r));
            })
            .catch((err: unknown) => {
                console.error('SetSharedAgentLoopEnabled failed:', err);
                setError(err instanceof Error ? err.message : String(err));
            })
            .finally(() => setToggling(false));
    };

    const applyCanaryPercent = () => {
        const n = Math.round(Number(canaryPercentDraft));
        if (!Number.isFinite(n) || n < 0 || n > 100) {
            setError(t('sharedLoopCanaryPercentInvalid'));
            return;
        }
        setSavingCanary(true);
        setError('');
        callSetSharedCanaryPercent(n)
            .then((st) => {
                setLoopStatus(st);
                setCanaryPercentDraft(String(n));
                // Refresh membership preview so IN/OUT matches the new percent.
                const uid = canaryUser.trim();
                if (uid) {
                    return callPreviewSharedLoopCanary(uid, -1).then((p) => {
                        setCanaryPreview(p);
                        return callRunDoctor().then((r) => setReport(r));
                    });
                }
                return callRunDoctor().then((r) => setReport(r));
            })
            .catch((err: unknown) => {
                console.error('SetSharedAgentLoopCanaryPercent failed:', err);
                setError(err instanceof Error ? err.message : String(err));
            })
            .finally(() => setSavingCanary(false));
    };

    const setWorkflowPilot = (enabled: boolean) => {
        setSavingWorkflow(true);
        setError('');
        callSetSharedWorkflow(enabled)
            .then((st) => {
                setLoopStatus(st);
                return callRunDoctor().then((r) => setReport(r));
            })
            .catch((err: unknown) => {
                console.error('SetSharedAgentLoopWorkflow failed:', err);
                setError(err instanceof Error ? err.message : String(err));
            })
            .finally(() => setSavingWorkflow(false));
    };

    const resetAdaptivePromptStats = () => {
        setResettingStats(true);
        setError('');
        callResetAdaptivePromptStats()
            .then((st) => {
                setLoopStatus(st);
                return callRunDoctor().then((r) => setReport(r));
            })
            .catch((err: unknown) => {
                console.error('ResetAdaptivePromptStats failed:', err);
                setError(err instanceof Error ? err.message : String(err));
            })
            .finally(() => setResettingStats(false));
    };

    const exportAdaptivePromptStats = () => {
        setExportingStats(true);
        setError('');
        callExportAdaptivePromptStats()
            .then((res) => {
                const p = String(res?.path || res?.written || '').trim();
                setExportPath(p);
            })
            .catch((err: unknown) => {
                console.error('ExportAdaptivePromptStats failed:', err);
                setExportPath('');
                setError(err instanceof Error ? err.message : String(err));
            })
            .finally(() => setExportingStats(false));
    };

    const openExportsDir = () => {
        setOpeningExports(true);
        setError('');
        callOpenAdaptivePromptExportsDir()
            .then((res) => {
                if (res?.path) {
                    setExportPath(String(res.path));
                }
            })
            .catch((err: unknown) => {
                console.error('OpenAdaptivePromptExportsDir failed:', err);
                setError(err instanceof Error ? err.message : String(err));
            })
            .finally(() => setOpeningExports(false));
    };

    const previewCanary = () => {
        const uid = canaryUser.trim();
        if (!uid) {
            setCanaryPreview(null);
            setError(t('sharedLoopCanaryUserRequired'));
            return;
        }
        setCanaryLoading(true);
        setError('');
        // percent=-1 → env > config canary percent (same as runtime)
        callPreviewSharedLoopCanary(uid, -1)
            .then((p) => setCanaryPreview(p))
            .catch((err: unknown) => {
                console.error('PreviewSharedLoopCanary failed:', err);
                setCanaryPreview(null);
                setError(err instanceof Error ? err.message : String(err));
            })
            .finally(() => setCanaryLoading(false));
    };

    useEffect(() => {
        if (!open) return;
        load();
        setCanaryPreview(null);
        setExportPath('');
    }, [open]);

    if (!open) return null;

    const checks = report?.checks ?? [];
    const shared = Number(loopStatus?.shared_turns ?? 0);
    const legacy = Number(loopStatus?.legacy_turns ?? 0);
    const total = shared + legacy;
    const sharedPct = total > 0 ? Math.round((shared / total) * 100) : 0;
    const route = loopStatus?.last_route;

    return (
        <div className="modal-backdrop" {...backdropProps}>
            <div
                className="modal-content"
                {...dialogProps}
                style={{ width: '600px', maxHeight: '80vh', overflow: 'auto' }}
            >
                <div className="modal-header">
                    <h3 style={{ fontSize: '0.92rem', margin: 0 }}>
                        {t('systemDoctorTitle')}
                    </h3>
                    <button className="btn-close" onClick={onClose}>
                        {'\u00d7'}
                    </button>
                </div>
                <div className="modal-body" style={{ padding: '12px 16px' }}>
                    {loading && (
                        <p style={{ color: 'var(--theme-text-secondary)', fontSize: '0.8rem' }}>
                            {t('loading')}...
                        </p>
                    )}
                    {!loading && error && (
                        <p style={{ color: '#e74c3c', fontSize: '0.8rem' }}>{error}</p>
                    )}
                    {!loading && report && (
                        <>
                            <div
                                style={{
                                    marginBottom: 12,
                                    padding: '8px 10px',
                                    borderRadius: 6,
                                    background: report.ok
                                        ? 'rgba(39, 174, 96, 0.12)'
                                        : 'rgba(231, 76, 60, 0.12)',
                                    fontSize: '0.82rem',
                                }}
                            >
                                <strong>
                                    {report.ok ? t('systemDoctorReady') : t('systemDoctorNotReady')}
                                </strong>
                                <div
                                    style={{
                                        marginTop: 4,
                                        color: 'var(--theme-text-secondary)',
                                    }}
                                >
                                    {report.summary}
                                </div>
                                {(report.blockers ?? 0) + (report.warnings ?? 0) > 0 && (
                                    <div style={{ marginTop: 4, fontSize: '0.75rem' }}>
                                        {t('systemDoctorBlockers')}: {report.blockers ?? 0} ·{' '}
                                        {t('systemDoctorWarnings')}: {report.warnings ?? 0}
                                    </div>
                                )}
                            </div>

                            {/* Shared agent loop path stats */}
                            {loopStatus && (
                                <div
                                    style={{
                                        marginBottom: 14,
                                        padding: '10px 12px',
                                        borderRadius: 6,
                                        border: '1px solid var(--theme-border)',
                                        fontSize: '0.8rem',
                                    }}
                                >
                                    <div
                                        style={{
                                            display: 'flex',
                                            justifyContent: 'space-between',
                                            alignItems: 'center',
                                            marginBottom: 8,
                                            gap: 8,
                                            flexWrap: 'wrap',
                                        }}
                                    >
                                        <strong>{t('sharedLoopTitle')}</strong>
                                        <span
                                            style={{
                                                fontWeight: 700,
                                                textTransform: 'uppercase',
                                                color: modeColor(loopStatus.mode),
                                                fontSize: '0.75rem',
                                            }}
                                        >
                                            {loopStatus.mode || 'off'}
                                            {typeof loopStatus.percent === 'number' &&
                                                (loopStatus.mode === 'on' ||
                                                    loopStatus.mode === 'shadow') &&
                                                loopStatus.percent < 100 &&
                                                ` · canary ${loopStatus.percent}%`}
                                            {!!loopStatus.workflow_pilot && ' · workflow'}
                                        </span>
                                    </div>
                                    <div
                                        style={{
                                            display: 'flex',
                                            gap: 8,
                                            marginBottom: 10,
                                            flexWrap: 'wrap',
                                            alignItems: 'center',
                                        }}
                                    >
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            disabled={
                                                toggling ||
                                                !!loopStatus.env_locks_mode ||
                                                loopStatus.config_enabled === true
                                            }
                                            onClick={() => setSharedEnabled(true)}
                                            title={
                                                loopStatus.env_locks_mode
                                                    ? t('sharedLoopEnvLockedHint')
                                                    : undefined
                                            }
                                        >
                                            {t('sharedLoopEnable')}
                                        </button>
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            disabled={
                                                toggling ||
                                                !!loopStatus.env_locks_mode ||
                                                loopStatus.config_enabled === false
                                            }
                                            onClick={() => setSharedEnabled(false)}
                                            title={
                                                loopStatus.env_locks_mode
                                                    ? t('sharedLoopEnvLockedHint')
                                                    : undefined
                                            }
                                        >
                                            {t('sharedLoopDisable')}
                                        </button>
                                        {loopStatus.env_locks_mode && (
                                            <span
                                                style={{
                                                    fontSize: '0.72rem',
                                                    color: 'var(--theme-text-secondary)',
                                                }}
                                            >
                                                {t('sharedLoopEnvLocked')}:{' '}
                                                {loopStatus.env_override || 'set'}
                                            </span>
                                        )}
                                        {!loopStatus.env_locks_mode && (
                                            <span
                                                style={{
                                                    fontSize: '0.72rem',
                                                    color: 'var(--theme-text-secondary)',
                                                }}
                                            >
                                                {t('sharedLoopConfig')}:{' '}
                                                {loopStatus.config_enabled ? 'on' : 'off'}
                                            </span>
                                        )}
                                    </div>
                                    <div
                                        style={{
                                            display: 'flex',
                                            gap: 6,
                                            marginBottom: 10,
                                            flexWrap: 'wrap',
                                            alignItems: 'center',
                                        }}
                                    >
                                        <span
                                            style={{
                                                fontSize: '0.72rem',
                                                color: 'var(--theme-text-secondary)',
                                                whiteSpace: 'nowrap',
                                            }}
                                        >
                                            {t('sharedLoopCanaryPercent')}:
                                        </span>
                                        <input
                                            type="number"
                                            min={0}
                                            max={100}
                                            value={canaryPercentDraft}
                                            disabled={savingCanary || !!loopStatus.percent_from_env}
                                            onChange={(e) => setCanaryPercentDraft(e.target.value)}
                                            onKeyDown={(e) => {
                                                if (e.key === 'Enter') {
                                                    e.preventDefault();
                                                    applyCanaryPercent();
                                                }
                                            }}
                                            style={{
                                                width: 64,
                                                fontSize: '0.72rem',
                                                padding: '3px 6px',
                                                borderRadius: 4,
                                                border: '1px solid var(--theme-border)',
                                                background: 'var(--theme-surface, #fff)',
                                                color: 'var(--theme-text)',
                                            }}
                                            title={
                                                loopStatus.percent_from_env
                                                    ? t('sharedLoopCanaryPercentEnvHint')
                                                    : t('sharedLoopCanaryPercentHint')
                                            }
                                            aria-label={t('sharedLoopCanaryPercent')}
                                        />
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            disabled={
                                                savingCanary || !!loopStatus.percent_from_env
                                            }
                                            onClick={() => applyCanaryPercent()}
                                            style={{ fontSize: '0.72rem', padding: '2px 8px' }}
                                            title={
                                                loopStatus.percent_from_env
                                                    ? t('sharedLoopCanaryPercentEnvHint')
                                                    : t('sharedLoopCanaryPercentHint')
                                            }
                                        >
                                            {savingCanary
                                                ? t('sharedLoopSaving')
                                                : t('sharedLoopCanaryPercentApply')}
                                        </button>
                                        {loopStatus.percent_from_env && (
                                            <span
                                                style={{
                                                    fontSize: '0.72rem',
                                                    color: 'var(--theme-text-secondary)',
                                                }}
                                            >
                                                {t('sharedLoopCanaryPercentEnvHint')}
                                            </span>
                                        )}
                                        <label
                                            style={{
                                                display: 'inline-flex',
                                                alignItems: 'center',
                                                gap: 4,
                                                fontSize: '0.72rem',
                                                color: 'var(--theme-text-secondary)',
                                                marginLeft: 8,
                                                cursor: loopStatus.workflow_from_env
                                                    ? 'not-allowed'
                                                    : 'pointer',
                                            }}
                                            title={
                                                loopStatus.workflow_from_env
                                                    ? t('sharedLoopWorkflowEnvHint')
                                                    : t('sharedLoopWorkflowHint')
                                            }
                                        >
                                            <input
                                                type="checkbox"
                                                // Effective pilot (env wins); avoids showing config=true while env forces off.
                                                checked={!!loopStatus.workflow_pilot}
                                                disabled={
                                                    savingWorkflow || !!loopStatus.workflow_from_env
                                                }
                                                onChange={(e) =>
                                                    setWorkflowPilot(e.target.checked)
                                                }
                                            />
                                            {t('sharedLoopWorkflowPilot')}
                                        </label>
                                    </div>
                                    <div
                                        style={{
                                            display: 'flex',
                                            gap: 6,
                                            marginBottom: 10,
                                            flexWrap: 'wrap',
                                            alignItems: 'center',
                                        }}
                                    >
                                        <span
                                            style={{
                                                fontSize: '0.72rem',
                                                color: 'var(--theme-text-secondary)',
                                                whiteSpace: 'nowrap',
                                            }}
                                        >
                                            {t('sharedLoopCanaryPreview')}
                                        </span>
                                        <input
                                            type="text"
                                            value={canaryUser}
                                            onChange={(e) => setCanaryUser(e.target.value)}
                                            onKeyDown={(e) => {
                                                if (e.key === 'Enter') {
                                                    e.preventDefault();
                                                    previewCanary();
                                                }
                                            }}
                                            placeholder={t('sharedLoopCanaryUserPlaceholder')}
                                            style={{
                                                flex: '1 1 140px',
                                                minWidth: 120,
                                                maxWidth: 220,
                                                fontSize: '0.72rem',
                                                padding: '3px 8px',
                                                borderRadius: 4,
                                                border: '1px solid var(--theme-border)',
                                                background: 'var(--theme-surface, #fff)',
                                                color: 'var(--theme-text)',
                                            }}
                                            aria-label={t('sharedLoopCanaryPreview')}
                                        />
                                        <button
                                            type="button"
                                            className="btn-secondary"
                                            disabled={canaryLoading || !canaryUser.trim()}
                                            onClick={() => previewCanary()}
                                            style={{ fontSize: '0.72rem', padding: '2px 8px' }}
                                            title={t('sharedLoopCanaryPreviewHint')}
                                        >
                                            {canaryLoading
                                                ? t('sharedLoopCanaryChecking')
                                                : t('sharedLoopCanaryCheck')}
                                        </button>
                                        {canaryPreview && (
                                            <span
                                                style={{
                                                    fontSize: '0.72rem',
                                                    fontWeight: 600,
                                                    color: canaryPreview.allows
                                                        ? '#27ae60'
                                                        : '#e74c3c',
                                                }}
                                                title={canaryPreview.summary || undefined}
                                            >
                                                {canaryPreview.allows
                                                    ? t('sharedLoopCanaryIn')
                                                    : t('sharedLoopCanaryOut')}
                                                {typeof canaryPreview.bucket === 'number'
                                                    ? ` · bucket ${canaryPreview.bucket}`
                                                    : ''}
                                                {typeof canaryPreview.percent === 'number'
                                                    ? ` · ${canaryPreview.percent}%`
                                                    : ''}
                                            </span>
                                        )}
                                    </div>
                                    <PathBar shared={shared} legacy={legacy} />
                                    <div
                                        style={{
                                            display: 'grid',
                                            gridTemplateColumns: '1fr 1fr',
                                            gap: '6px 12px',
                                            marginTop: 10,
                                            fontSize: '0.76rem',
                                            color: 'var(--theme-text-secondary)',
                                        }}
                                    >
                                        <div>
                                            {t('sharedLoopShared')}:{' '}
                                            <strong style={{ color: '#3498db' }}>{shared}</strong>
                                            {total > 0 ? ` (${sharedPct}%)` : ''}
                                        </div>
                                        <div>
                                            {t('sharedLoopLegacy')}:{' '}
                                            <strong style={{ color: '#7f8c8d' }}>{legacy}</strong>
                                            {total > 0 ? ` (${100 - sharedPct}%)` : ''}
                                        </div>
                                        <div>
                                            {t('sharedLoopSuccess')}: {loopStatus.shared_success ?? 0}
                                        </div>
                                        <div>
                                            {t('sharedLoopError')}: {loopStatus.shared_error ?? 0}
                                            {(loopStatus.shared_cancelled ?? 0) > 0 &&
                                                ` · ${t('sharedLoopCancelled')}: ${loopStatus.shared_cancelled}`}
                                        </div>
                                        {(Number(loopStatus.skip_canary ?? 0) +
                                            Number(loopStatus.skip_ineligible ?? 0) +
                                            Number(loopStatus.shadow_eligible ?? 0) >
                                            0) && (
                                            <div
                                                style={{ gridColumn: '1 / -1' }}
                                                title={
                                                    formatCountMapHint(loopStatus.skip_by_reason, 4) ||
                                                    loopStatus.last_skip_reason ||
                                                    undefined
                                                }
                                            >
                                                skip: canary={loopStatus.skip_canary ?? 0}
                                                {' · '}ineligible={loopStatus.skip_ineligible ?? 0}
                                                {' · '}shadow={loopStatus.shadow_eligible ?? 0}
                                                {loopStatus.last_skip_reason
                                                    ? ` · last=${loopStatus.last_skip_reason}`
                                                    : ''}
                                            </div>
                                        )}
                                        {loopStatus.workflow_pilot && (
                                            <div style={{ gridColumn: '1 / -1' }}>
                                                {t('sharedLoopWorkflowPilot')}: on
                                            </div>
                                        )}
                                        {route && (route.model || route.task) && (
                                            <div style={{ gridColumn: '1 / -1' }}>
                                                {t('sharedLoopLastRoute')}:{' '}
                                                {[route.task, route.source, route.model]
                                                    .filter(Boolean)
                                                    .join(' / ')}
                                                {route.cost_tier &&
                                                (route.cost_route_mode === 'shadow' ||
                                                    route.cost_route_mode === 'on')
                                                    ? ` · tier=${route.cost_tier}${
                                                          route.cost_route_applied ? '' : '(shadow)'
                                                      }`
                                                    : ''}
                                                {route.thinking_policy &&
                                                (route.cost_route_mode === 'shadow' ||
                                                    route.cost_route_mode === 'on')
                                                    ? ` · think=${route.thinking_policy}${
                                                          route.cost_route_applied ? '' : '(shadow)'
                                                      }`
                                                    : ''}
                                                {route.escalated ? ` (${t('sharedLoopEscalated')})` : ''}
                                            </div>
                                        )}
                                        {formatTurnUsage(loopStatus.last_usage) && (
                                            <div style={{ gridColumn: '1 / -1' }}>
                                                {t('sharedLoopLastUsage')}:{' '}
                                                {formatTurnUsage(loopStatus.last_usage)}
                                            </div>
                                        )}
                                        {formatTurnUsage(loopStatus.process_usage) && (
                                            <div style={{ gridColumn: '1 / -1' }}>
                                                {t('sharedLoopProcessUsage')}:{' '}
                                                {formatTurnUsage(loopStatus.process_usage)}
                                            </div>
                                        )}
                                        <div
                                            style={{
                                                gridColumn: '1 / -1',
                                                display: 'flex',
                                                flexWrap: 'wrap',
                                                gap: 8,
                                                alignItems: 'center',
                                            }}
                                        >
                                            <span>
                                                {t('sharedLoopPromptAdaptive')}:{' '}
                                                {(Number(loopStatus.prompt_light_turns ?? 0) +
                                                    Number(loopStatus.prompt_full_turns ?? 0) >
                                                0) ? (
                                                    <>
                                                        <strong>
                                                            {t('sharedLoopPromptLight')}{' '}
                                                            {loopStatus.prompt_light_percent ?? 0}%
                                                        </strong>
                                                        {` (${loopStatus.prompt_light_turns ?? 0} light / ${
                                                            loopStatus.prompt_full_turns ?? 0
                                                        } full)`}
                                                        {Number(loopStatus.prompt_est_tokens_saved ?? 0) > 0
                                                            ? ` · ${t('sharedLoopPromptSaved')}: ~${
                                                                  loopStatus.prompt_est_tokens_saved
                                                              } tok`
                                                            : ''}
                                                        {loopStatus.last_prompt_profile
                                                            ? ` · last=${loopStatus.last_prompt_profile}`
                                                            : ''}
                                                        {Number(loopStatus.last_prompt_saved_tokens ?? 0) > 0
                                                            ? ` (-${loopStatus.last_prompt_saved_tokens})`
                                                            : ''}
                                                        {loopStatus.last_prompt_task
                                                            ? ` · task=${loopStatus.last_prompt_task}`
                                                            : ''}
                                                        {(() => {
                                                            const byTask = formatCountMapHint(
                                                                loopStatus.prompt_by_task,
                                                                4,
                                                            );
                                                            return byTask ? ` · by_task=${byTask}` : '';
                                                        })()}
                                                        {Number(loopStatus.prompt_light_tool_denies ?? 0) > 0
                                                            ? ` · light_deny=${loopStatus.prompt_light_tool_denies}${
                                                                  formatDeniedToolHint(
                                                                      loopStatus.prompt_by_denied_tool,
                                                                      loopStatus.prompt_last_denied_tool,
                                                                  )
                                                              }`
                                                            : ''}
                                                        {Number(loopStatus.prompt_light_upgrades ?? 0) > 0
                                                            ? ` · light_upgrade=${loopStatus.prompt_light_upgrades}${
                                                                  loopStatus.prompt_last_upgrade_reason
                                                                      ? `(${compactUpgradeReason(
                                                                            loopStatus.prompt_last_upgrade_reason,
                                                                        )})`
                                                                      : ''
                                                              }`
                                                            : ''}
                                                        {Number(loopStatus.prompt_ab_eligible_light ?? 0) > 0
                                                            ? ` · ab=${loopStatus.prompt_ab_sample_full ?? 0}/${loopStatus.prompt_ab_eligible_light}`
                                                            : ''}
                                                        {Number(loopStatus.prompt_upgrade_rate_percent ?? 0) > 0
                                                            ? ` · upgrade_rate=${loopStatus.prompt_upgrade_rate_percent}%`
                                                            : ''}
                                                        {Number(loopStatus.prompt_deny_rate_percent ?? 0) > 0
                                                            ? ` · deny_rate=${loopStatus.prompt_deny_rate_percent}%`
                                                            : ''}
                                                    </>
                                                ) : (
                                                    <span style={{ opacity: 0.85 }}>
                                                        {t('sharedLoopPromptNoData')}
                                                    </span>
                                                )}
                                                {loopStatus.prompt_profile_forced
                                                    ? ` · env MACLAW_PROMPT_PROFILE=${loopStatus.prompt_profile_forced}`
                                                    : ''}
                                                {Number(loopStatus.prompt_ab_sample_percent ?? 0) > 0
                                                    ? ` · ab_pct=${loopStatus.prompt_ab_sample_percent}`
                                                    : ''}
                                                {loopStatus.light_retry_enabled === false
                                                    ? ` · light_retry=off`
                                                    : ''}
                                            </span>
                                            <button
                                                type="button"
                                                className="btn-secondary"
                                                disabled={exportingStats}
                                                onClick={() => exportAdaptivePromptStats()}
                                                title={t('sharedLoopPromptExportHint')}
                                                style={{ fontSize: '0.72rem', padding: '2px 8px' }}
                                            >
                                                {exportingStats
                                                    ? t('sharedLoopPromptExporting')
                                                    : t('sharedLoopPromptExport')}
                                            </button>
                                            <button
                                                type="button"
                                                className="btn-secondary"
                                                disabled={openingExports}
                                                onClick={() => openExportsDir()}
                                                title={t('sharedLoopPromptOpenExportsHint')}
                                                style={{ fontSize: '0.72rem', padding: '2px 8px' }}
                                            >
                                                {openingExports
                                                    ? t('sharedLoopPromptOpeningExports')
                                                    : t('sharedLoopPromptOpenExports')}
                                            </button>
                                            <button
                                                type="button"
                                                className="btn-secondary"
                                                disabled={
                                                    resettingStats ||
                                                    (Number(loopStatus.prompt_light_turns ?? 0) +
                                                        Number(loopStatus.prompt_full_turns ?? 0) ===
                                                        0)
                                                }
                                                onClick={() => resetAdaptivePromptStats()}
                                                title={t('sharedLoopPromptResetHint')}
                                                style={{ fontSize: '0.72rem', padding: '2px 8px' }}
                                            >
                                                {t('sharedLoopPromptReset')}
                                            </button>
                                        </div>
                                        <div
                                            style={{
                                                gridColumn: '1 / -1',
                                                fontSize: '0.72rem',
                                                color: 'var(--theme-text-secondary)',
                                            }}
                                            title={t('sharedLoopHubAdaptiveHint')}
                                        >
                                            {t('sharedLoopHubAdaptive')}:{' '}
                                            {loopStatus.hub_connected ? (
                                                <strong style={{ color: '#27ae60' }}>
                                                    {t('sharedLoopHubConnected')}
                                                </strong>
                                            ) : (
                                                <span>
                                                    {loopStatus.hub_url
                                                        ? t('sharedLoopHubOffline')
                                                        : t('sharedLoopHubNotConfigured')}
                                                </span>
                                            )}
                                            {loopStatus.hub_adaptive_summary
                                                ? ` · ${loopStatus.hub_adaptive_summary}`
                                                : ` · ${t('sharedLoopHubAdaptiveEmpty')}`}
                                        </div>
                                        {exportPath && (
                                            <div
                                                style={{
                                                    gridColumn: '1 / -1',
                                                    fontSize: '0.7rem',
                                                    color: '#27ae60',
                                                    wordBreak: 'break-all',
                                                }}
                                                title={t('sharedLoopPromptExportHint')}
                                            >
                                                {t('sharedLoopPromptExported')}: {exportPath}
                                            </div>
                                        )}
                                        {loopStatus.process_started_at && (
                                            <div
                                                style={{
                                                    gridColumn: '1 / -1',
                                                    fontSize: '0.7rem',
                                                    opacity: 0.85,
                                                }}
                                            >
                                                {t('sharedLoopProcessStart')}:{' '}
                                                {loopStatus.process_started_at}
                                            </div>
                                        )}
                                    </div>
                                    {total === 0 && (
                                        <div
                                            style={{
                                                marginTop: 8,
                                                fontSize: '0.72rem',
                                                color: 'var(--theme-text-secondary)',
                                            }}
                                        >
                                            {t('sharedLoopNoTraffic')}
                                        </div>
                                    )}
                                </div>
                            )}

                            <table
                                style={{
                                    width: '100%',
                                    fontSize: '0.78rem',
                                    borderCollapse: 'collapse',
                                }}
                            >
                                <thead>
                                    <tr style={{ borderBottom: '1px solid var(--theme-border)' }}>
                                        <th style={{ textAlign: 'left', padding: '4px 6px' }}>
                                            {t('systemDoctorStatus')}
                                        </th>
                                        <th style={{ textAlign: 'left', padding: '4px 6px' }}>
                                            {t('systemDoctorCheck')}
                                        </th>
                                        <th style={{ textAlign: 'left', padding: '4px 6px' }}>
                                            {t('systemDoctorMessage')}
                                        </th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {checks.map((c) => (
                                        <tr
                                            key={c.id}
                                            style={{
                                                borderBottom: '1px solid var(--theme-border)',
                                            }}
                                        >
                                            <td
                                                style={{
                                                    padding: '5px 6px',
                                                    color: statusColor(c.status),
                                                    fontWeight: 600,
                                                    whiteSpace: 'nowrap',
                                                    textTransform: 'uppercase',
                                                }}
                                            >
                                                {c.status}
                                            </td>
                                            <td
                                                style={{
                                                    padding: '5px 6px',
                                                    fontFamily: 'var(--font-mono, monospace)',
                                                    whiteSpace: 'nowrap',
                                                }}
                                            >
                                                {c.id}
                                            </td>
                                            <td style={{ padding: '5px 6px' }}>
                                                <div>{c.message}</div>
                                                {c.hint &&
                                                    (c.status === 'fail' || c.status === 'warn') && (
                                                        <div
                                                            style={{
                                                                marginTop: 2,
                                                                color: 'var(--theme-text-secondary)',
                                                                fontSize: '0.72rem',
                                                            }}
                                                        >
                                                            → {c.hint}
                                                        </div>
                                                    )}
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                            <div style={{ marginTop: 12, display: 'flex', gap: 8 }}>
                                <button className="btn-secondary" onClick={load} type="button">
                                    {t('systemDoctorRefresh')}
                                </button>
                            </div>
                        </>
                    )}
                </div>
            </div>
        </div>
    );
}
