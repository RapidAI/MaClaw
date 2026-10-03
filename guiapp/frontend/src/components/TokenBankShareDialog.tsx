import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { TokenBankCreateShare, TokenBankListShareAudiences, TokenBankSyncShareModels } from '../../wailsjs/go/main/App';
import { localizeText } from '../i18n/langSelect';
import { useDialog } from './CustomDialog';
import {
    AudiencePickList,
    audienceSelectionKey,
    readAudienceList,
    type AudienceChoice,
} from './tokenBankAudience';
import {
    countProbeProgress,
    fingerprintProviderKey,
    modelsMissingFromShare,
    tokenBankDisplayURL,
    type ShareModelEntry,
    type ShareModelProbeState,
    type TokenBankShareWindow,
} from '../utils/hubcenterTokenBank';

/**
 * Token Bank share dialog (design doc §7.2).
 *
 * Two steps live here rather than as two dialogs, because the probe result *is*
 * the model list the user is confirming — splitting them would mean asking the
 * user to re-confirm a list they already saw.
 *
 * The plaintext key is held in props (the caller owns it) and is handed
 * straight to `TokenBankCreateShare`, which encrypts it against the HubCenter
 * public key before anything is serialized. It is never put into component
 * state, so it cannot leak through a re-render or a devtools snapshot.
 */

export type TokenBankShareRequest = {
    providerName: string;
    apiURL: string;
    apiKey: string;
    protocol: string;
    /** Model ids discovered from the provider. The caller owns discovery. */
    models: string[];
};

type TokenBankShareDialogProps = {
    lang: string;
    request: TokenBankShareRequest;
    /** Runs one model probe; the caller owns the actual network call. */
    probe: (model: string) => Promise<{ ok: boolean; latencyMs?: number; error?: string }>;
    onClose: () => void;
    onShared?: (shareID: string) => void;
    /** Opens the existing email-code verification UI. Absent in tests that only surface the error. */
    onRequestVerification?: () => void;
    /** Models already shared for this provider. Empty on the first share, which shows no "new models" banner. */
    alreadySharedModels?: string[];
    /**
     * Set when this dialog adjusts a share that already exists.
     * The checked rows start as the models on that share, and confirm writes
     * them back instead of creating another share.
     */
    adjustment?: TokenBankShareAdjustment;
};

export type TokenBankShareAdjustment = {
    shareID: string;
    /** Models currently enabled on the share. These start checked. */
    enabledModels: string[];
    /** Existing dial windows, keyed by model name. Missing means always. */
    windows?: Record<string, TokenBankShareWindow | null | undefined>;
};

function shareBlockedUntilVerified(message: string): boolean {
    const text = message.toLowerCase();
    return text.includes('identity_not_verified') || text.includes('verify your account before sharing');
}

type ShareWindowDraft = {
    days: number[];
    start: string;
    end: string;
};

const SHARE_WEEKDAYS: Array<{ day: number; en: string; zh: string; hant: string }> = [
    { day: 1, en: 'Mon', zh: '一', hant: '一' },
    { day: 2, en: 'Tue', zh: '二', hant: '二' },
    { day: 3, en: 'Wed', zh: '三', hant: '三' },
    { day: 4, en: 'Thu', zh: '四', hant: '四' },
    { day: 5, en: 'Fri', zh: '五', hant: '五' },
    { day: 6, en: 'Sat', zh: '六', hant: '六' },
    { day: 0, en: 'Sun', zh: '日', hant: '日' },
];

function defaultShareWindow(): ShareWindowDraft {
    return { days: SHARE_WEEKDAYS.map((item) => item.day), start: '00:00', end: '24:00' };
}

function parseShareClock(raw: string): number | null {
    const match = /^(\d{1,2}):(\d{2})$/.exec(raw.trim());
    if (!match) return null;
    const hour = Number(match[1]);
    const minute = Number(match[2]);
    if (!Number.isInteger(hour) || !Number.isInteger(minute)) return null;
    if (hour < 0 || hour > 24 || minute < 0 || minute > 59) return null;
    if (hour === 24 && minute !== 0) return null;
    return hour * 60 + minute;
}

function formatShareClock(minutes: number): string {
    if (minutes === 24 * 60) return '24:00';
    return `${String(Math.floor(minutes / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`;
}

function shareWindowProblem(draft: ShareWindowDraft): 'days' | 'clock' | null {
    if (draft.days.length === 0) return 'days';
    const start = parseShareClock(draft.start);
    const end = parseShareClock(draft.end);
    if (start === null || end === null || start === end || start >= 24 * 60) return 'clock';
    return null;
}

function modelKey(name: string): string {
    return name.trim().toLowerCase();
}

function draftFromStoredWindow(raw?: TokenBankShareWindow | null): ShareWindowDraft {
    if (!raw) return defaultShareWindow();
    const start = (raw.start || '').trim();
    const end = (raw.end || '').trim();
    const days = (raw.days || []).filter((day) => Number.isInteger(day) && day >= 0 && day <= 6);
    if (!start && !end && days.length === 0) return defaultShareWindow();
    return {
        days: days.length > 0 ? days : SHARE_WEEKDAYS.map((item) => item.day),
        start: start || '00:00',
        end: end || '24:00',
    };
}

function initialSelected(models: string[], adjustment?: TokenBankShareAdjustment): Record<string, boolean> {
    const enabled = new Set((adjustment?.enabledModels ?? []).map(modelKey).filter(Boolean));
    const adjusting = Boolean(adjustment?.shareID);
    const initial: Record<string, boolean> = {};
    for (const model of models) initial[model] = adjusting ? enabled.has(modelKey(model)) : true;
    return initial;
}

function initialWindows(models: string[], adjustment?: TokenBankShareAdjustment): Record<string, ShareWindowDraft> {
    const stored = new Map<string, TokenBankShareWindow>();
    for (const [name, window] of Object.entries(adjustment?.windows ?? {})) {
        if (window) stored.set(modelKey(name), window);
    }
    const initial: Record<string, ShareWindowDraft> = {};
    for (const model of models) initial[model] = draftFromStoredWindow(stored.get(modelKey(model)));
    return initial;
}

function shareWindowPayload(draft: ShareWindowDraft): { days?: number[]; start: string; end: string } | undefined {
    const start = parseShareClock(draft.start);
    const end = parseShareClock(draft.end);
    if (start === null || end === null) return undefined;
    const startText = formatShareClock(start);
    const endText = formatShareClock(end);
    const days = SHARE_WEEKDAYS.map((item) => item.day).filter((day) => draft.days.includes(day));
    if (days.length === SHARE_WEEKDAYS.length && startText === '00:00' && endText === '24:00') return undefined;
    if (days.length === SHARE_WEEKDAYS.length) return { start: startText, end: endText };
    return { days, start: startText, end: endText };
}

export function TokenBankShareDialog({ lang, request, probe, onClose, onShared, onRequestVerification, alreadySharedModels, adjustment }: TokenBankShareDialogProps) {
    const t = useCallback(
        (en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant ?? zhHans),
        [lang],
    );
    const { showAlert, showConfirm } = useDialog();

    // Selection defaults to "everything on", matching the spec's 默认全选: the
    // common case is sharing a whole provider, and opting out is cheaper than
    // opting in for each model.
    const adjusting = Boolean(adjustment?.shareID);
    const [selected, setSelected] = useState<Record<string, boolean>>(() => initialSelected(request.models, adjustment));
    const [probeStates, setProbeStates] = useState<ShareModelEntry[]>(() =>
        request.models.map((model) => ({ model, state: 'pending' as ShareModelProbeState })),
    );
    const [windows, setWindows] = useState<Record<string, ShareWindowDraft>>(() => initialWindows(request.models, adjustment));
    const [probing, setProbing] = useState(false);
    const [submitting, setSubmitting] = useState(false);
    const [maxInput, setMaxInput] = useState('0');
    const [maxOutput, setMaxOutput] = useState('0');
    const [visibility, setVisibility] = useState<'public' | 'private'>('public');
    const [audienceLoad, setAudienceLoad] = useState(0);
    const [audienceStatus, setAudienceStatus] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
    const [audienceError, setAudienceError] = useState('');
    const [hubChoices, setHubChoices] = useState<AudienceChoice[]>([]);
    const [tenantChoices, setTenantChoices] = useState<AudienceChoice[]>([]);
    const [selectedHubs, setSelectedHubs] = useState<Record<string, boolean>>({});
    const [selectedTenants, setSelectedTenants] = useState<Record<string, boolean>>({});
    // A stable instance id so a retry after a network failure is recognized as
    // the same submission rather than creating a second share.
    const instanceID = useRef<string>('');
    if (!instanceID.current) {
        instanceID.current = `maclaw-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
    }

    // Null means "the models currently checked". While a probe is running this
    // is the queue that click started with, minus any row unchecked before its turn.
    const [probeWave, setProbeWave] = useState<string[] | null>(null);
    const selectedRef = useRef(selected);
    selectedRef.current = selected;
    const probeLock = useRef(false);
    const mountedRef = useRef(true);
    useEffect(() => {
        mountedRef.current = true;
        return () => { mountedRef.current = false; };
    }, []);

    const runProbe = useCallback(async () => {
        if (probeLock.current) return;
        const initial = probeStates.filter((entry) => selected[entry.model]).map((entry) => entry.model);
        if (initial.length === 0) return;
        probeLock.current = true;
        setProbing(true);
        setProbeWave(initial);
        // One request at a time. Only the model in flight shows 探测中, so a
        // row the user unchecks before its turn stays 待探测 and is not called.
        try {
            let wave = initial;
            for (const model of initial) {
                if (!mountedRef.current) return;
                if (!selectedRef.current[model]) {
                    wave = wave.filter((name) => name !== model);
                    setProbeWave(wave);
                    continue;
                }
                setProbeStates((prev) =>
                    prev.map((entry) =>
                        entry.model === model
                            ? { ...entry, state: 'probing' as ShareModelProbeState, latencyMs: undefined, error: undefined }
                            : entry,
                    ),
                );
                let result: { ok: boolean; latencyMs?: number; error?: string };
                try {
                    result = await probe(model);
                } catch (error) {
                    result = { ok: false, error: error instanceof Error ? error.message : String(error) };
                }
                if (!mountedRef.current) return;
                setProbeStates((prev) =>
                    prev.map((item) =>
                        item.model === model
                            ? {
                                  ...item,
                                  state: (result.ok ? 'available' : 'unavailable') as ShareModelProbeState,
                                  latencyMs: result.latencyMs,
                                  error: result.error,
                              }
                            : item,
                    ),
                );
            }
        } finally {
            probeLock.current = false;
            setProbing(false);
            setProbeWave(null);
        }
    }, [probe, probeStates, selected]);

    const progress = useMemo(() => {
        const names = new Set(probeWave ?? probeStates.filter((entry) => selected[entry.model]).map((entry) => entry.model));
        return countProbeProgress(probeStates.filter((entry) => names.has(entry.model)));
    }, [probeStates, probeWave, selected]);
    const availableCount = useMemo(
        () => probeStates.filter((entry) => entry.state === 'available').length,
        [probeStates],
    );
    // A selection counts unless the probe proved the model unusable. A model
    // that is merely `pending` must still be submittable: the dialog opens
    // before probing, and the spec's own mock shows 待探测 rows as shareable.
    // Counting only `available` would disable Confirm the moment the dialog
    // opens, which is exactly the state the user is meant to act from.
    const chosenCount = useMemo(
        () => probeStates.filter((entry) => selected[entry.model] && entry.state !== 'unavailable').length,
        [probeStates, selected],
    );
    const newModels = useMemo(() => {
        if (!alreadySharedModels || alreadySharedModels.length === 0) return [];
        const available = probeStates.filter((entry) => entry.state === 'available').map((entry) => entry.model);
        return modelsMissingFromShare(available, alreadySharedModels);
    }, [alreadySharedModels, probeStates]);

    const toggleAll = (on: boolean) => {
        const next: Record<string, boolean> = {};
        for (const entry of probeStates) next[entry.model] = on;
        setSelected(next);
    };

    const submitEntries = async (entries: ShareModelEntry[]) => {
        if (entries.length === 0) {
            await showAlert(
                t(
                    'Select at least one model to share.',
                    '请至少勾选一个模型。',
                    '請至少勾選一個模型。',
                ),
                t('Token Bank', 'Token 银行', 'Token 銀行'),
            );
            return;
        }
        const chosenHubs = hubChoices.filter((item) => selectedHubs[audienceSelectionKey(item.id)]).map((item) => item.id);
        const chosenTenants = tenantChoices.filter((item) => selectedTenants[audienceSelectionKey(item.id)]).map((item) => item.id);
        if (visibility === 'private' && audienceStatus !== 'ready') {
            await showAlert(
                audienceStatus === 'error'
                    ? audienceError || t('Could not load hubs and tenants.', '没能加载 Hub 和租户。', '沒能載入 Hub 和租戶。')
                    : t('Hubs and tenants are still loading.', 'Hub 和租户还在加载。', 'Hub 和租戶還在載入。'),
                t('Token Bank', 'Token 银行', 'Token 銀行'),
            );
            return;
        }
        if (visibility === 'private' && chosenHubs.length === 0 && chosenTenants.length === 0) {
            await showAlert(
                t(
                    'A private share needs at least one hub or tenant.',
                    '私有分享至少要选择一个 hub 或租户。',
                    '私有分享至少要選擇一個 hub 或租戶。',
                ),
                t('Token Bank', 'Token 银行', 'Token 銀行'),
            );
            return;
        }
        if (visibility === 'private' && chosenHubs.length + chosenTenants.length > 32) {
            await showAlert(
                t(
                    'A private share can name at most 32 hubs and tenants.',
                    '私有分享最多选择 32 个 Hub 和租户。',
                    '私有分享最多選擇 32 個 Hub 和租戶。',
                ),
                t('Token Bank', 'Token 银行', 'Token 銀行'),
            );
            return;
        }
        for (const entry of entries) {
            const draft = windows[entry.model] ?? defaultShareWindow();
            const problem = shareWindowProblem(draft);
            if (problem === 'days') {
                await showAlert(
                    t(
                        `${entry.model} needs at least one weekday.`,
                        `${entry.model} 至少要勾选一个星期。`,
                        `${entry.model} 至少要勾選一個星期。`,
                    ),
                    t('Token Bank', 'Token 银行', 'Token 銀行'),
                );
                return;
            }
            if (problem === 'clock') {
                await showAlert(
                    t(
                        `${entry.model} needs a Beijing time from 0:00 to 24:00. 24:00 ends that day, and the end is not included.`,
                        `${entry.model} 的时段请写成 0:00 到 24:00。24:00 表示当天结束，结束时刻不包含在内。`,
                        `${entry.model} 的時段請寫成 0:00 到 24:00。24:00 表示當天結束，結束時刻不包含在內。`,
                    ),
                    t('Token Bank', 'Token 银行', 'Token 銀行'),
                );
                return;
            }
        }
        try {
            // The server publishes only rows marked available. Pending is not
            // a failed probe: the dialog opens before probing, and marking
            // those rows unavailable stores a share that never goes live.
            // A retry of the same key replays the stored rows. A probe that
            // proved the model unusable stays available:false and is still
            // recorded so the owner can see the error.
            const models = entries.map((entry) => {
                const row: Record<string, unknown> = {
                    model: entry.model,
                    available: entry.state !== 'unavailable',
                    probe_error: entry.error ?? '',
                    used_input_tokens: 0,
                    used_output_tokens: 0,
                };
                const window = shareWindowPayload(windows[entry.model] ?? defaultShareWindow());
                if (window) row.share_window = window;
                return row;
            });
            if (adjusting && adjustment?.shareID) {
                await TokenBankSyncShareModels(adjustment.shareID, models as never);
                if (!mountedRef.current) return;
                onShared?.(adjustment.shareID);
                onClose();
                return;
            }
            const fingerprint = await fingerprintProviderKey(request.apiKey, request.apiURL);
            const payload = {
                DisplayName: request.providerName,
                APIURL: request.apiURL,
                APIKey: request.apiKey,
                Protocol: request.protocol,
                KeyFingerprint: fingerprint,
                Models: models,
                MaxInputTokens: Number.parseInt(maxInput, 10) || 0,
                MaxOutputTokens: Number.parseInt(maxOutput, 10) || 0,
                ClientInstanceID: instanceID.current,
                Visibility: visibility,
                HubIDs: chosenHubs.join(', '),
                TenantIDs: chosenTenants.join(', '),
                SeparateAudiences: visibility === 'private',
            } as never;
            const result = (await TokenBankCreateShare(payload)) as Record<string, unknown>;
            if (!mountedRef.current) return;
            const shareID = typeof result?.id === 'string' ? result.id : '';
            onShared?.(shareID);
            onClose();
        } catch (error) {
            if (!mountedRef.current) return;
            // The server's own message is shown verbatim: it distinguishes
            // "verify your account" / "token bank unavailable" from a generic
            // failure, and paraphrasing it would lose that.
            const message = error instanceof Error ? error.message : String(error);
            await showAlert(
                message,
                adjusting
                    ? t('Could not update models', '模型范围没有保存', '模型範圍沒有保存')
                    : t('Share failed', '分享失败', '分享失敗'),
            );
            if (!mountedRef.current) return;
            // The server only says the account is unverified. The existing
            // email-code wizard is the verification UI; this dialog does not
            // grow a second form.
            if (shareBlockedUntilVerified(message)) {
                const open = await showConfirm(
                    t(
                        'This account is not verified yet. Open email verification now?',
                        '当前账号尚未完成验证。现在打开邮箱验证吗？',
                        '目前帳號尚未完成驗證。現在打開郵箱驗證嗎？',
                    ),
                    t('Verify account', '验证账号', '驗證帳號'),
                );
                if (!mountedRef.current) return;
                if (open && onRequestVerification) {
                    onRequestVerification();
                    onClose();
                }
            }
        }
    };

    const submit = async () => {
        // A running probe has not produced a verdict yet. Publishing those rows
        // marks them available, and a later failure cannot correct the stored share.
        if (probing) return;
        if (chosenCount === 0) {
            await showAlert(
                t(
                    'Select at least one model to share.',
                    '请至少勾选一个模型。',
                    '請至少勾選一個模型。',
                ),
                t('Token Bank', 'Token 银行', 'Token 銀行'),
            );
            return;
        }
        setSubmitting(true);
        try {
            await submitEntries(probeStates.filter((entry) => selected[entry.model]));
        } finally {
            setSubmitting(false);
        }
    };

    // Probe locally and submit only the models that answered. The decision uses
    // this array, not React state, because setState has not flushed yet.
    const importAll = async () => {
        if (probeLock.current || probing || submitting || request.models.length === 0) return;
        probeLock.current = true;
        setProbing(true);
        setSubmitting(true);
        setProbeWave(request.models);
        const next: ShareModelEntry[] = [];
        try {
            for (let index = 0; index < request.models.length; index += 1) {
                if (!mountedRef.current) return;
                const model = request.models[index];
                setProbeStates([
                    ...next,
                    { model, state: 'probing' },
                    ...request.models.slice(index + 1).map((pending) => ({
                        model: pending,
                        state: 'pending' as ShareModelProbeState,
                    })),
                ]);
                const result = await probe(model);
                if (!mountedRef.current) return;
                next.push({
                    model,
                    state: result.ok ? 'available' : 'unavailable',
                    latencyMs: result.latencyMs,
                    error: result.error,
                });
            }
            if (!mountedRef.current) return;
            setProbeStates(next);
            const selectedNext: Record<string, boolean> = {};
            for (const entry of next) selectedNext[entry.model] = entry.state === 'available';
            setSelected(selectedNext);
            const available = next.filter((entry) => entry.state === 'available');
            if (available.length === 0) {
                await showAlert(
                    t(
                        'None of the models answered, so nothing was shared.',
                        '没有模型通过探测，未提交分享。',
                        '沒有模型通過探測，未提交分享。',
                    ),
                    t('Token Bank', 'Token 银行', 'Token 銀行'),
                );
                return;
            }
            await submitEntries(available);
        } catch (error) {
            if (!mountedRef.current) return;
            setProbeStates((prev) => prev.map((entry) => (
                entry.state === 'probing' ? { ...entry, state: 'pending' as ShareModelProbeState } : entry
            )));
            const message = error instanceof Error ? error.message : String(error);
            await showAlert(
                message,
                adjusting
                    ? t('Could not update models', '模型范围没有保存', '模型範圍沒有保存')
                    : t('Share failed', '分享失败', '分享失敗'),
            );
        } finally {
            probeLock.current = false;
            setProbing(false);
            setSubmitting(false);
            setProbeWave(null);
        }
    };

    // Private shares pick from the account's hubs and tenants. Reloading is
    // tied to audienceLoad so Retry can ask again without a text fallback.
    // A refresh keeps the list that already loaded; only the first attempt
    // replaces the picker with the loading or error state.
    useEffect(() => {
        if (visibility !== 'private') return;
        let cancelled = false;
        setAudienceError('');
        setAudienceStatus((current) => (current === 'ready' ? 'ready' : 'loading'));
        void TokenBankListShareAudiences()
            .then((result) => {
                if (cancelled) return;
                const record = (result ?? {}) as Record<string, unknown>;
                setHubChoices(readAudienceList(record.hubs));
                setTenantChoices(readAudienceList(record.tenants));
                setAudienceError('');
                setAudienceStatus('ready');
            })
            .catch((error: unknown) => {
                if (cancelled) return;
                setAudienceError(error instanceof Error ? error.message : String(error));
                setAudienceStatus((current) => (current === 'ready' ? 'ready' : 'error'));
            });
        return () => {
            cancelled = true;
        };
    }, [visibility, audienceLoad]);

    const dialogRef = useRef<HTMLDivElement>(null);
    // The deposit button disables itself while this dialog is open, so focus
    // would otherwise drop to the page behind the backdrop.
    useEffect(() => {
        const node = dialogRef.current;
        if (!node) return;
        node.focus({ preventScroll: true });
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== 'Tab') return;
            const focusable = [...node.querySelectorAll<HTMLElement>(
                'button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href]',
            )];
            if (focusable.length === 0) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            const active = document.activeElement;
            if (event.shiftKey && (active === first || active === node)) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && active === last) {
                event.preventDefault();
                first.focus();
            }
        };
        node.addEventListener('keydown', onKey);
        return () => node.removeEventListener('keydown', onKey);
    }, []);

    // Escape closes, matching every other dialog in the app.
    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key === 'Escape' && !submitting) onClose();
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onClose, submitting]);

    const stateLabel = (state: ShareModelProbeState): string => {
        switch (state) {
            case 'available':
                return t('Available', '可用', '可用');
            case 'unavailable':
                return t('Unavailable', '不可用', '不可用');
            case 'probing':
                return t('Probing…', '探测中…', '探測中…');
            default:
                return t('Not probed', '待探测', '待探測');
        }
    };

    return (
        <div ref={dialogRef} className="tbk-modal-backdrop" role="dialog" aria-modal="true" aria-label={adjusting ? t('Adjust models', '调整模型', '調整模型') : t('Share provider', '分享服务商', '分享服務商')} tabIndex={-1}>
            <div className="tbk-modal">
                <h3 className="tbk-modal__title">{adjusting ? t('Adjust models', '调整模型', '調整模型') : t('Share provider', '分享服务商', '分享服務商')}</h3>
                <p className="tbk-modal__sub">
                    {request.providerName} · {tokenBankDisplayURL(request.apiURL)}
                </p>
                {newModels.length > 0 ? (
                    <p className="tbk-new-models" role="status">
                        {t(
                            `${newModels.length} new models can be added to this share.`,
                            `发现 ${newModels.length} 个新模型，可以加入银行。`,
                            `發現 ${newModels.length} 個新模型，可以加入銀行。`,
                        )}
                    </p>
                ) : null}

                <div className="tbk-modal__section">
                    <div className="tbk-modal__row">
                        <span>{t('Models to share', '可分享模型', '可分享模型')}</span>
                        <span className="tbk-modal__picked">
                            {t(
                                `${chosenCount} selected`,
                                `已选 ${chosenCount} 个`,
                                `已選 ${chosenCount} 個`,
                            )}
                        </span>
                        <span className="tbk-modal__actions">
                            <button className="btn-link" type="button" onClick={() => toggleAll(true)} disabled={probing}>
                                {t('All', '全选', '全選')}
                            </button>
                            <button className="btn-link" type="button" onClick={() => toggleAll(false)} disabled={probing}>
                                {t('None', '全不选', '全不選')}
                            </button>
                        </span>
                    </div>
                    <p className="tbk-modal__hint">
                        {adjusting
                            ? t(
                                'Check a model to add it, and uncheck one to remove it. A removed model stops receiving calls. Earnings it already made stay on this share.',
                                '勾选模型即加入，取消勾选即移除。移除后不再接收调用，已经产生的收益仍记在这个分享上。',
                                '勾選模型即加入，取消勾選即移除。移除後不再接收呼叫，已經產生的收益仍記在這個分享上。',
                            )
                            : null}
                        {adjusting ? ' ' : null}
                        {t(
                            'Default is every day 0:00–24:00 Beijing time. Uncheck a day or change the clock to share only then.',
                            '默认每天 0:00–24:00（北京时间）。取消星期或改时间后，只在该时段接入。',
                            '預設每天 0:00–24:00（北京時間）。取消星期或改時間後，只在該時段接入。',
                        )}
                    </p>
                    <ul className="tbk-model-picker">
                        {probeStates.map((entry) => {
                            const draft = windows[entry.model] ?? defaultShareWindow();
                            return (
                            <li className="tbk-model-picker__row" key={entry.model}>
                                <label className="tbk-model-picker__label">
                                    <input
                                        type="checkbox"
                                        aria-label={entry.model}
                                        checked={!!selected[entry.model]}
                                        onChange={(event) =>
                                            setSelected((prev) => ({ ...prev, [entry.model]: event.target.checked }))
                                        }
                                        disabled={submitting}
                                    />
                                    <span className="tbk-model-picker__name">{entry.model}</span>
                                </label>
                                <span className={`tbk-pill tbk-pill--${entry.state === 'available' ? 'on' : 'off'}`}>
                                    {stateLabel(entry.state)}
                                    {entry.state === 'available' && entry.latencyMs !== undefined
                                        ? ` · ${entry.latencyMs} ms`
                                        : ''}
                                </span>
                                {selected[entry.model] ? (
                                    <div className="tbk-share-window">
                                        <span>{t('Hours', '时段', '時段')}</span>
                                        {SHARE_WEEKDAYS.map((item) => (
                                            <label className="tbk-share-window__day" key={item.day}>
                                                <input
                                                    type="checkbox"
                                                    aria-label={`${entry.model} ${t(item.en, `周${item.zh}`, `週${item.hant}`)}`}
                                                    checked={draft.days.includes(item.day)}
                                                    disabled={submitting}
                                                    onChange={(event) => {
                                                        const on = event.target.checked;
                                                        setWindows((prev) => {
                                                            const current = prev[entry.model] ?? defaultShareWindow();
                                                            const days = on
                                                                ? SHARE_WEEKDAYS.map((day) => day.day).filter((day) => day === item.day || current.days.includes(day))
                                                                : current.days.filter((day) => day !== item.day);
                                                            return { ...prev, [entry.model]: { ...current, days } };
                                                        });
                                                    }}
                                                />
                                                <span>{t(item.en, item.zh, item.hant)}</span>
                                            </label>
                                        ))}
                                        <input
                                            className="tbk-share-window__clock"
                                            aria-label={`${entry.model} ${t('start', '开始', '開始')}`}
                                            value={draft.start}
                                            disabled={submitting}
                                            inputMode="numeric"
                                            spellCheck={false}
                                            onChange={(event) => {
                                                const start = event.target.value;
                                                setWindows((prev) => ({
                                                    ...prev,
                                                    [entry.model]: { ...(prev[entry.model] ?? defaultShareWindow()), start },
                                                }));
                                            }}
                                        />
                                        <span aria-hidden="true">–</span>
                                        <input
                                            className="tbk-share-window__clock"
                                            aria-label={`${entry.model} ${t('end', '结束', '結束')}`}
                                            value={draft.end}
                                            disabled={submitting}
                                            inputMode="numeric"
                                            spellCheck={false}
                                            onChange={(event) => {
                                                const end = event.target.value;
                                                setWindows((prev) => ({
                                                    ...prev,
                                                    [entry.model]: { ...(prev[entry.model] ?? defaultShareWindow()), end },
                                                }));
                                            }}
                                        />
                                    </div>
                                ) : null}
                                {entry.error ? <span className="tbk-model-picker__error">{entry.error}</span> : null}
                            </li>
                            );
                        })}
                    </ul>
                    <div className="tbk-modal__row">
                        <button
                            className="btn-secondary"
                            type="button"
                            onClick={() => void runProbe()}
                            disabled={probing || submitting || !probeStates.some((entry) => selected[entry.model])}
                        >
                            {probing ? t('Probing…', '探测中…', '探測中…') : t('Start probe', '开始探测', '開始探測')}
                        </button>
                        <button className="btn-secondary" type="button" onClick={() => void importAll()} disabled={probing || submitting || request.models.length === 0}>
                            {t('Import every available model', '一键导入全部可用模型', '一鍵導入全部可用模型')}
                        </button>
                        <span className="tbk-modal__progress">
                            {progress.done}/{progress.total}
                        </span>
                    </div>
                </div>

                {adjusting ? null : <div className="tbk-modal__section">
                    <div className="tbk-modal__row">
                        <span>{t('Who can call this share', '谁可以调用', '誰可以呼叫')}</span>
                    </div>
                    <div
                        className="tbk-visibility"
                        role="radiogroup"
                        aria-label={t('Who can call this share', '谁可以调用', '誰可以呼叫')}
                    >
                        <label className="tbk-visibility__option">
                            <input
                                type="radio"
                                name="tbk-visibility"
                                checked={visibility === 'public'}
                                onChange={() => setVisibility('public')}
                                disabled={submitting}
                            />
                            <span>{t('Public', '公开', '公開')}</span>
                        </label>
                        <label className="tbk-visibility__option">
                            <input
                                type="radio"
                                name="tbk-visibility"
                                aria-label={t('Private', '私有', '私有')}
                                checked={visibility === 'private'}
                                onChange={() => setVisibility('private')}
                                disabled={submitting}
                            />
                            <span>{t('Private, named hubs or tenants only', '私有，只给指定的 hub 或租户', '私有，只給指定的 hub 或租戶')}</span>
                        </label>
                    </div>
                    {visibility === 'private' ? (
                        <div className="tbk-audience">
                            {audienceStatus !== 'ready' && audienceStatus !== 'error' ? (
                                <p className="tbk-audience__status">
                                    {t('Loading hubs and tenants…', '正在加载 Hub 和租户…', '正在載入 Hub 和租戶…')}
                                </p>
                            ) : null}
                            {audienceError ? (
                                <div className="tbk-audience__status" role="alert">
                                    <span>{audienceError}</span>
                                    <button
                                        className="tbk-audience__retry"
                                        type="button"
                                        onClick={() => setAudienceLoad((count) => count + 1)}
                                        disabled={submitting}
                                    >
                                        {t('Retry', '重试', '重試')}
                                    </button>
                                </div>
                            ) : null}
                            {audienceStatus === 'ready' || hubChoices.length > 0 || tenantChoices.length > 0 ? (
                                <>
                                    <AudiencePickList
                                        label={t('Hubs', 'Hub', 'Hub')}
                                        emptyText={t('No hubs to choose', '没有可选择的 Hub', '沒有可選擇的 Hub')}
                                        choices={hubChoices}
                                        selected={selectedHubs}
                                        disabled={submitting || audienceStatus !== 'ready'}
                                        onToggle={(id) => {
                                            const key = audienceSelectionKey(id);
                                            setSelectedHubs((prev) => ({ ...prev, [key]: !prev[key] }));
                                        }}
                                    />
                                    <AudiencePickList
                                        label={t('Tenants', '租户', '租戶')}
                                        emptyText={t('No tenants to choose', '没有可选择的租户', '沒有可選擇的租戶')}
                                        choices={tenantChoices}
                                        selected={selectedTenants}
                                        disabled={submitting || audienceStatus !== 'ready'}
                                        showHub
                                        onToggle={(id) => {
                                            const key = audienceSelectionKey(id);
                                            setSelectedTenants((prev) => ({ ...prev, [key]: !prev[key] }));
                                        }}
                                    />
                                </>
                            ) : null}
                        </div>
                    ) : null}
                </div>}

                {adjusting ? null : (
                <div className="tbk-modal__section">
                    <div className="tbk-modal__row">
                        <span>{t('Token caps (0 = unlimited)', '可分享 token 上限（0 = 无限）', '可分享 token 上限（0 = 無限）')}</span>
                    </div>
                    <label className="tbk-field">
                        <span>{t('Per-request input', '单次输入上限', '單次輸入上限')}</span>
                        <input value={maxInput} onChange={(event) => setMaxInput(event.target.value)} inputMode="numeric" />
                    </label>
                    <label className="tbk-field">
                        <span>{t('Per-request output', '单次输出上限', '單次輸出上限')}</span>
                        <input value={maxOutput} onChange={(event) => setMaxOutput(event.target.value)} inputMode="numeric" />
                    </label>
                </div>
                )}

                <div className="tbk-modal__footer">
                    <span className="tbk-modal__hint">
                        {t(
                            `${availableCount} available · ${chosenCount} selected`,
                            `可用 ${availableCount} 个 · 已选 ${chosenCount} 个`,
                            `可用 ${availableCount} 個 · 已選 ${chosenCount} 個`,
                        )}
                    </span>
                    <button className="btn-secondary" type="button" onClick={onClose} disabled={submitting}>
                        {t('Cancel', '取消')}
                    </button>
                    <button className="btn-primary" type="button" onClick={() => void submit()} disabled={probing || submitting || chosenCount === 0}>
                        {submitting
                            ? (adjusting ? t('Saving…', '保存中…', '保存中…') : t('Sharing…', '分享中…', '分享中…'))
                            : (adjusting ? t('Save model range', '保存模型范围', '保存模型範圍') : t('Confirm share', '确认分享', '確認分享'))}
                    </button>
                </div>
            </div>
        </div>
    );
}

