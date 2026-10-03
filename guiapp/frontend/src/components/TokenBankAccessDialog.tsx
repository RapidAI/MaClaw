import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { TokenBankListShareAudiences, TokenBankSetShareVisibility } from '../../wailsjs/go/main/App';
import { localizeText } from '../i18n/langSelect';
import type { TokenBankShare } from '../utils/hubcenterTokenBank';
import { useDialog } from './CustomDialog';
import {
    AudiencePickList,
    audienceGrantRows,
    pairSelectionKey,
    readAudienceList,
    selectedAudienceMap,
    selectedPairMap,
    splitStoredAudiences,
    withGrantedAudiences,
    audienceSelectionKey,
    type AudienceChoice,
    type AudiencePair,
} from './tokenBankAudience';

type TokenBankAccessDialogProps = {
    lang: string;
    share: TokenBankShare;
    onClose: () => void;
    onSaved: () => void;
};

/**
 * Changes who may call a share that is already in the bank.
 * Public and private are radios. A private share checks hubs and tenants
 * from the signed-in account. Nothing here is typed.
 */
export function TokenBankAccessDialog({ lang, share, onClose, onSaved }: TokenBankAccessDialogProps) {
    const t = useCallback(
        (en: string, zhHans: string, zhHant?: string) => localizeText(lang, en, zhHans, zhHant ?? zhHans),
        [lang],
    );
    const { showAlert } = useDialog();
    const [visibility, setVisibility] = useState<'public' | 'private'>(share.visibility === 'private' ? 'private' : 'public');
    const [audienceLoad, setAudienceLoad] = useState(0);
    const [audienceStatus, setAudienceStatus] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
    const [audienceError, setAudienceError] = useState('');
    const [hubChoices, setHubChoices] = useState<AudienceChoice[]>([]);
    const [tenantChoices, setTenantChoices] = useState<AudienceChoice[]>([]);
    const stored = useMemo(() => splitStoredAudiences(share.audiences), [share.audiences]);
    const [selectedHubs, setSelectedHubs] = useState<Record<string, boolean>>(() => selectedAudienceMap(stored.hubs));
    const [selectedTenants, setSelectedTenants] = useState<Record<string, boolean>>(() => selectedAudienceMap(stored.tenants));
    const [selectedPairs, setSelectedPairs] = useState<Record<string, boolean>>(() => selectedPairMap(stored.pairs, stored.hubs, stored.tenants));
    const [submitting, setSubmitting] = useState(false);

    useEffect(() => {
        if (visibility !== 'private') return;
        let cancelled = false;
        setAudienceError('');
        setAudienceStatus((current) => (current === 'ready' ? 'ready' : 'loading'));
        void TokenBankListShareAudiences()
            .then((result) => {
                if (cancelled) return;
                const record = (result ?? {}) as Record<string, unknown>;
                setHubChoices(withGrantedAudiences(readAudienceList(record.hubs), stored.hubs));
                setTenantChoices(withGrantedAudiences(readAudienceList(record.tenants), stored.tenants));
                setAudienceError('');
                setAudienceStatus('ready');
            })
            .catch((error: unknown) => {
                if (cancelled) return;
                setHubChoices((current) => withGrantedAudiences(current, stored.hubs));
                setTenantChoices((current) => withGrantedAudiences(current, stored.tenants));
                setAudienceError(error instanceof Error ? error.message : String(error));
                setAudienceStatus((current) => (current === 'ready' ? 'ready' : 'error'));
            });
        return () => {
            cancelled = true;
        };
    }, [audienceLoad, stored.hubs, stored.tenants, visibility]);

    const dialogRef = useRef<HTMLDivElement>(null);
    // The opener disables while this dialog is open, and Save disables itself
    // while a request is in flight. Either one drops focus onto the page
    // behind the backdrop. A newer dialog, such as the error alert, keeps focus.
    useEffect(() => {
        const node = dialogRef.current;
        if (!node) return;
        const pullFocusBack = () => {
            const fallback = node.querySelector<HTMLElement>(
                'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled])',
            );
            (fallback ?? node).focus({ preventScroll: true });
        };
        pullFocusBack();
        const onFocusIn = (event: FocusEvent) => {
            const target = event.target;
            if (!(target instanceof Node) || node.contains(target)) return;
            if (target instanceof Element && target.closest('[role="dialog"]')) return;
            pullFocusBack();
        };
        document.addEventListener('focusin', onFocusIn, true);
        return () => document.removeEventListener('focusin', onFocusIn, true);
    }, []);

    useEffect(() => {
        const node = dialogRef.current;
        if (!node) return;
        const onKey = (event: KeyboardEvent) => {
            const active = document.activeElement;
            const focusInside = active instanceof Node && node.contains(active);
            if (event.key === 'Escape') {
                if (submitting || !focusInside) return;
                event.preventDefault();
                event.stopImmediatePropagation();
                onClose();
                return;
            }
            if (event.key !== 'Tab' || !focusInside) return;
            const focusable = [...node.querySelectorAll<HTMLElement>(
                'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href]',
            )];
            if (focusable.length === 0) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (event.shiftKey && (active === first || active === node)) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && active === last) {
                event.preventDefault();
                first.focus();
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onClose, submitting]);

    const audienceRows = () => {
        if (visibility !== 'private') return [];
        return audienceGrantRows(hubChoices, tenantChoices, stored.pairs, selectedHubs, selectedTenants, selectedPairs);
    };

    const clearPairsCoveredBy = (kind: 'hub' | 'tenant', id: string) => {
        const key = audienceSelectionKey(id);
        setSelectedPairs((prev) => {
            let changed = false;
            const next = { ...prev };
            for (const pair of stored.pairs) {
                const covered = kind === 'hub'
                    ? audienceSelectionKey(pair.hubId) === key
                    : audienceSelectionKey(pair.tenantId) === key;
                const pairKey = pairSelectionKey(pair);
                if (!covered || next[pairKey] !== true) continue;
                next[pairKey] = false;
                changed = true;
            }
            return changed ? next : prev;
        });
    };

    const audiencePending = visibility === 'private' && (audienceStatus === 'idle' || audienceStatus === 'loading');
    const saveDisabled = submitting || audiencePending || (visibility === 'private' && audienceRows().length === 0);

    const save = async () => {
        if (submitting || saveDisabled) return;
        const rows = audienceRows();
        if (rows.length > 32) {
            await showAlert(
                t(
                    'A private share can name at most 32 hubs and tenants.',
                    '私有分享最多选择 32 个 Hub 和租户。',
                    '私有分享最多選擇 32 個 Hub 和租戶。',
                ),
                t('Change who can call this share', '更改访问范围', '更改存取範圍'),
            );
            return;
        }
        setSubmitting(true);
        try {
            await TokenBankSetShareVisibility(share.id, visibility, rows);
            onSaved();
        } catch (error) {
            const message = error instanceof Error ? error.message : String(error);
            await showAlert(message, t('Could not update access', '访问范围没有更新', '存取範圍沒有更新'));
        } finally {
            setSubmitting(false);
        }
    };

    const choiceLabel = (id: string, choices: AudienceChoice[]): string => {
        const found = choices.find((item) => audienceSelectionKey(item.id) === audienceSelectionKey(id));
        if (!found) return id;
        return found.name || found.id;
    };

    const pairLabel = (pair: AudiencePair): string => {
        return `${choiceLabel(pair.hubId, hubChoices)} + ${choiceLabel(pair.tenantId, tenantChoices)}`;
    };

    const shareName = share.display_name || share.id;

    return (
        <div ref={dialogRef} className="tbk-modal-backdrop" role="dialog" aria-modal="true" aria-label={t('Change who can call this share', '更改访问范围', '更改存取範圍')} tabIndex={-1}>
            <div className="tbk-modal">
                <h3 className="tbk-modal__title">{t('Change who can call this share', '更改访问范围', '更改存取範圍')}</h3>
                <p className="tbk-modal__sub">{shareName}</p>
                <div className="tbk-modal__section">
                    <p className="tbk-modal__hint">
                        {t(
                            'Choose public or private. A checked hub allows every tenant on that hub. A checked tenant allows every hub. A row under Hub and tenant together allows only calls that match both.',
                            '选择公开或私有。勾选 Hub 允许该 Hub 上的全部租户，勾选租户允许任意 Hub 上的该租户。同时匹配的一行必须两边都符合。',
                            '選擇公開或私有。勾選 Hub 允許該 Hub 上的全部租戶，勾選租戶允許任意 Hub 上的該租戶。同時匹配的一行必須兩邊都符合。',
                        )}
                    </p>
                    <div
                        className="tbk-visibility"
                        role="radiogroup"
                        aria-label={t('Who can call this share', '谁可以调用', '誰可以呼叫')}
                    >
                        <label className="tbk-visibility__option">
                            <input
                                type="radio"
                                name="tbk-access-visibility"
                                aria-label={t('Public', '公开', '公開')}
                                checked={visibility === 'public'}
                                onChange={() => setVisibility('public')}
                                disabled={submitting}
                            />
                            <span>{t('Public', '公开', '公開')}</span>
                        </label>
                        <label className="tbk-visibility__option">
                            <input
                                type="radio"
                                name="tbk-access-visibility"
                                aria-label={t('Private', '私有', '私有')}
                                checked={visibility === 'private'}
                                onChange={() => setVisibility('private')}
                                disabled={submitting}
                            />
                            <span>{t('Private, named hubs or tenants only', '私有，只给指定的 Hub 或租户', '私有，只給指定的 Hub 或租戶')}</span>
                        </label>
                    </div>
                    {visibility === 'private' && stored.pairs.length > 0 ? (
                        <section className="tbk-audience__column" aria-label={t('Hub and tenant together', '同时匹配', '同時匹配')}>
                            <span className="tbk-audience__title">{t('Hub and tenant together', '同时匹配', '同時匹配')}</span>
                            <p className="tbk-audience__empty">
                                {t(
                                    'A call must match both ids. Uncheck a row to remove that grant. Checking the hub or the tenant below allows every call on that side and clears this row.',
                                    '调用必须同时属于这个 Hub 和这个租户。取消勾选即去掉这条授权。再勾选下面的 Hub 或租户，会放宽为该侧的全部调用，并取消这里的勾选。',
                                    '呼叫必須同時屬於這個 Hub 和這個租戶。取消勾選即去掉這條授權。再勾選下面的 Hub 或租戶，會放寬為該側的全部呼叫，並取消這裡的勾選。',
                                )}
                            </p>
                            <ul className="tbk-audience__list">
                                {stored.pairs.map((pair) => {
                                    const key = pairSelectionKey(pair);
                                    const label = pairLabel(pair);
                                    return (
                                        <li key={key}>
                                            <label className="tbk-audience__option">
                                                <input
                                                    type="checkbox"
                                                    aria-label={label}
                                                    checked={selectedPairs[key] === true}
                                                    disabled={submitting || audiencePending}
                                                    onChange={() => {
                                                        const turningOn = selectedPairs[key] !== true;
                                                        setSelectedPairs((prev) => ({ ...prev, [key]: turningOn }));
                                                        if (!turningOn) return;
                                                        const hubKey = audienceSelectionKey(pair.hubId);
                                                        const tenantKey = audienceSelectionKey(pair.tenantId);
                                                        setSelectedHubs((prev) => (prev[hubKey] ? { ...prev, [hubKey]: false } : prev));
                                                        setSelectedTenants((prev) => (prev[tenantKey] ? { ...prev, [tenantKey]: false } : prev));
                                                    }}
                                                />
                                                <span className="tbk-audience__name">{label}</span>
                                            </label>
                                        </li>
                                    );
                                })}
                            </ul>
                        </section>
                    ) : null}
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
                                        disabled={submitting || audiencePending}
                                        onToggle={(id) => {
                                            const key = audienceSelectionKey(id);
                                            const turningOn = selectedHubs[key] !== true;
                                            setSelectedHubs((prev) => ({ ...prev, [key]: turningOn }));
                                            if (turningOn) clearPairsCoveredBy('hub', id);
                                        }}
                                    />
                                    <AudiencePickList
                                        label={t('Tenants', '租户', '租戶')}
                                        emptyText={t('No tenants to choose', '没有可选择的租户', '沒有可選擇的租戶')}
                                        choices={tenantChoices}
                                        selected={selectedTenants}
                                        disabled={submitting || audiencePending}
                                        showHub
                                        onToggle={(id) => {
                                            const key = audienceSelectionKey(id);
                                            const turningOn = selectedTenants[key] !== true;
                                            setSelectedTenants((prev) => ({ ...prev, [key]: turningOn }));
                                            if (turningOn) clearPairsCoveredBy('tenant', id);
                                        }}
                                    />
                                </>
                            ) : null}
                        </div>
                    ) : null}
                </div>
                <div className="tbk-modal__footer">
                    <button className="btn-secondary" type="button" onClick={onClose} disabled={submitting}>
                        {t('Cancel', '取消', '取消')}
                    </button>
                    <button className="btn-primary" type="button" onClick={() => void save()} disabled={saveDisabled}>
                        {submitting ? t('Saving…', '保存中…', '保存中…') : t('Save', '确定', '確定')}
                    </button>
                </div>
            </div>
        </div>
    );
}
