import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';
import { buildSidebarModelOptions } from '../../utils/sidebarModelOptions';
import { panelText } from '../../i18n/mnPanel';
import { FetchProviderModels } from '../../../wailsjs/go/main/App';

// One entry the owner can pick for a bot role. Key is carried here because the
// bot needs it to call the provider; it never reaches the dialog's markup, only
// the save payload.
export type BotLLMProviderOption = {
    name: string;
    url: string;
    key: string;
    model: string;
    protocol: string;
    wireAPI: string;
    /** Model IDs discovered earlier and cached on the provider, when any. */
    models: string[];
    agentType: string;
    isHubService: boolean;
};

export type BotLLMRoleSetting = {
    unused?: boolean;
    official?: boolean;
    name?: string;
    url?: string;
    model?: string;
    protocol?: string;
    wireAPI?: string;
};

export type BotLLMRolesSetting = {
    foreground?: BotLLMRoleSetting;
    background?: BotLLMRoleSetting;
    computer_use?: BotLLMRoleSetting;
    grounder?: BotLLMRoleSetting;
};

export type BotLLMRoleSubmit = {
    unused?: boolean;
    official?: boolean;
    name?: string;
    url?: string;
    key?: string;
    model?: string;
    protocol?: string;
    wire_api?: string;
};

export type BotLLMSettingsSubmit = {
    mode: 'default' | 'custom';
    /** Mirrors foreground so an older Hub, which stores one provider, still accepts the save. */
    name?: string;
    url?: string;
    key?: string;
    model?: string;
    protocol?: string;
    wire_api?: string;
    roles?: {
        foreground: BotLLMRoleSubmit;
        background: BotLLMRoleSubmit;
        computer_use: BotLLMRoleSubmit;
        grounder: BotLLMRoleSubmit;
    };
};

export type BotLLMSettingsDialogLabels = {
    title: string;
    sharedHint: string;
    useDefault: string;
    defaultHint: string;
    useCustom: string;
    customHint: string;
    providerLabel: string;
    modelLabel: string;
    modelLoading: string;
    modelEmpty: string;
    noProviders: string;
    save: string;
    cancel: string;
    close: string;
};

type RoleKey = 'foreground' | 'background' | 'computer_use' | 'grounder';

type RoleDraft = {
    providerName: string;
    model: string;
    unused: boolean;
};

type BotLLMSettingsDialogProps = {
    labels: BotLLMSettingsDialogLabels;
    providers: BotLLMProviderOption[];
    initialMode: 'default' | 'custom';
    /** Legacy single-provider fields. Used when the server has not sent roles. */
    initialName: string;
    initialModel: string;
    initialRoles?: BotLLMRolesSetting | null;
    /** Save failure shown inside the dialog; the dialog stays open for another try. */
    notice: string;
    saving: boolean;
    onClose: () => void;
    onSubmit: (payload: BotLLMSettingsSubmit) => void;
    /** Header title when the dialog is the Model tab of the Settings dialog; labels.title then heads the body. */
    heading?: string;
    /** Tab strip rendered under the header (Settings dialog). */
    tabs?: ReactNode;
    lang?: string;
};

const ROLE_KEYS: RoleKey[] = ['foreground', 'background', 'computer_use', 'grounder'];

function willFetchModels(provider: BotLLMProviderOption | undefined): provider is BotLLMProviderOption {
    return provider != null && !provider.isHubService && provider.url.trim() !== '' && provider.key.trim() !== '';
}

function modelChoices(provider: BotLLMProviderOption | undefined, current: string, fetched: string[]): string[] {
    return buildSidebarModelOptions({
        configuredModel: current.trim() || provider?.model.trim() || '',
        cachedModels: provider?.models ?? [],
        fetchedModels: fetched,
    });
}

function seedModel(provider: BotLLMProviderOption | undefined, current: string): string {
    const options = modelChoices(provider, '', []);
    if (current && options.includes(current)) return current;
    return options[0] ?? '';
}

/**
 * One choice for every bot on this account.
 * Default hands every role back to the service. Custom expands the four role
 * rows and is the only place a provider and model are set. There is no
 * per-role "follow Hub" switch: that is what Default already means. The
 * grounding model keeps "not used", because leaving it unset is not the same
 * as following the service — the computer-use model then clicks by itself.
 */
export function BotLLMSettingsDialog({
    labels,
    providers,
    initialMode,
    initialName,
    initialModel,
    initialRoles,
    notice,
    saving,
    onClose,
    onSubmit,
    heading,
    tabs,
    lang,
}: BotLLMSettingsDialogProps) {
    const titleId = useId();
    const t = (en: string, zhHans: string, zhHant = zhHans) => panelText(lang ?? '', en, zhHans, zhHant);
    const options = useMemo(() => collectProviderOptions(providers, initialRoles, initialMode === 'custom' ? initialName : '', initialMode === 'custom' ? initialModel : ''), [providers, initialRoles, initialMode, initialName, initialModel]);
    const [mode, setMode] = useState<'default' | 'custom'>(initialMode === 'custom' && options.length > 0 ? 'custom' : initialMode === 'custom' && options.length === 0 ? 'default' : initialMode);
    const [drafts, setDrafts] = useState<Record<RoleKey, RoleDraft>>(() => initialDrafts(options, initialMode, initialName, initialModel, initialRoles));
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !saving });
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: () => { if (!saving) onClose(); } });

    const roleReady = (key: RoleKey) => {
        const draft = drafts[key];
        if (key === 'grounder' && draft.unused) return true;
        return draft.providerName.trim() !== '' && draft.model.trim() !== '';
    };
    const canSave = !saving && (mode === 'default' || (options.length > 0 && ROLE_KEYS.every(roleReady)));

    const chooseMode = (next: 'default' | 'custom') => {
        if (next === 'custom' && options.length === 0) return;
        setMode(next);
    };

    const pickProvider = (key: RoleKey, name: string) => {
        const provider = options.find(item => item.name === name);
        setDrafts(prev => ({
            ...prev,
            [key]: {
                providerName: name,
                model: seedModel(provider, prev[key].model),
                unused: false,
            },
        }));
    };

    const setModel = (key: RoleKey, model: string) => {
        setDrafts(prev => (prev[key].model === model ? prev : { ...prev, [key]: { ...prev[key], model } }));
    };

    const setGrounderUnused = (unused: boolean) => {
        setDrafts(prev => ({ ...prev, grounder: { ...prev.grounder, unused } }));
    };

    const submit = () => {
        if (!canSave) return;
        if (mode === 'default') {
            onSubmit({ mode: 'default' });
            return;
        }
        const roles = {
            foreground: rolePayload(drafts.foreground, options),
            background: rolePayload(drafts.background, options),
            computer_use: rolePayload(drafts.computer_use, options),
            grounder: drafts.grounder.unused ? { unused: true } : rolePayload(drafts.grounder, options),
        };
        const foreground = roles.foreground;
        onSubmit({
            mode: 'custom',
            name: foreground.official ? undefined : foreground.name,
            url: foreground.official ? undefined : foreground.url,
            key: foreground.official ? undefined : foreground.key,
            model: foreground.model,
            protocol: foreground.official ? undefined : foreground.protocol,
            wire_api: foreground.official ? undefined : foreground.wire_api,
            roles,
        });
    };

    const handleKeyDown = (event: ReactKeyboardEvent) => {
        if (event.key === 'Enter') {
            const native = event.nativeEvent as KeyboardEvent;
            if (native.isComposing || native.keyCode === 229) return;
            if (event.target instanceof HTMLButtonElement) return;
            if (event.target instanceof Element && event.target.closest('[data-testid="bot-llm-roles"]')) return;
            event.preventDefault();
            event.stopPropagation();
            submit();
            return;
        }
        onKeyDown(event);
    };

    const rows: Array<[RoleKey, string, string]> = [
        ['foreground', t('Foreground', '前台交互', '前台互動'), t('Planning and chat', '规划与对话', '規劃與對話')],
        ['background', t('Background', '后台执行', '後台執行'), t('Worker loops', '后台任务循环', '後台任務循環')],
        ['computer_use', t('Computer use', '电脑操作', '電腦操作'), t('Screenshots and clicks', '截图与点击', '截圖與點擊')],
        ['grounder', t('Grounding model', '定位模型', '定位模型'), t(
            'Optional. Finds the click point when Computer Use names a target in words (e.g. UI-TARS, Qwen-VL). If unset or it fails, the Computer Use model clicks by itself.',
            '可选。电脑操作用文字描述点击目标时，由它给出坐标（如 UI-TARS、Qwen-VL）。不设置或定位失败时，由电脑操作模型自己点击。',
            '可選。電腦操作用文字描述點擊目標時，由它給出座標（如 UI-TARS、Qwen-VL）。不設定或定位失敗時，由電腦操作模型自己點擊。',
        )],
    ];

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={handleKeyDown}>
            <div
                className="modal-content custom-dialog bot-llm-dialog"
                ref={dialogRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-testid="bot-llm-dialog"
                tabIndex={-1}
                {...dialogProps}
            >
                <div className="bot-llm-dialog__head">
                    <div className="modal-header custom-dialog__header">
                        <div className="custom-dialog__heading">
                            <h3 id={titleId} className="custom-dialog__title">{heading ?? labels.title}</h3>
                        </div>
                        <button type="button" className="btn-close custom-dialog__close" aria-label={labels.close} disabled={saving} onClick={onClose}>
                            <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
                                <path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                            </svg>
                        </button>
                    </div>
                    {tabs}
                </div>
                <div className="modal-body custom-dialog__body">
                    {heading ? <h4 className="bot-llm-dialog__section">{labels.title}</h4> : null}
                    <p className="bot-llm-dialog__hint">{labels.sharedHint}</p>

                    <div
                        className={`bot-llm-dialog__option${mode === 'default' ? ' is-selected' : ''}`}
                        onClick={() => { if (!saving) chooseMode('default'); }}
                    >
                        <label className="bot-llm-dialog__radio">
                            <input
                                type="radio"
                                name="bot-llm-mode"
                                value="default"
                                checked={mode === 'default'}
                                disabled={saving}
                                onChange={() => chooseMode('default')}
                                data-testid="bot-llm-mode-default"
                            />
                            <span>{labels.useDefault}</span>
                        </label>
                        <p className="bot-llm-dialog__subhint">{labels.defaultHint}</p>
                    </div>

                    <div
                        className={`bot-llm-dialog__option${mode === 'custom' ? ' is-selected' : ''}${options.length === 0 ? ' is-disabled' : ''}`}
                        onClick={() => { if (!saving) chooseMode('custom'); }}
                    >
                        <label className="bot-llm-dialog__radio">
                            <input
                                type="radio"
                                name="bot-llm-mode"
                                value="custom"
                                checked={mode === 'custom'}
                                disabled={saving || options.length === 0}
                                onChange={() => chooseMode('custom')}
                                data-testid="bot-llm-mode-custom"
                            />
                            <span>{labels.useCustom}</span>
                        </label>
                        <p className="bot-llm-dialog__subhint">{labels.customHint}</p>
                        {options.length === 0 ? <p className="bot-llm-dialog__subhint" role="status">{labels.noProviders}</p> : null}

                        {mode === 'custom' && options.length > 0 ? (
                            <div className="bot-llm-dialog__roles is-inline" data-testid="bot-llm-roles">
                                {rows.map(([key, title, hint]) => (
                                    <RoleRow
                                        key={key}
                                        roleKey={key}
                                        title={title}
                                        hint={hint}
                                        draft={drafts[key]}
                                        options={options}
                                        labels={labels}
                                        saving={saving}
                                        lang={lang}
                                        savedModel={savedModelFor(key, initialRoles, initialName, initialModel)}
                                        onProvider={name => pickProvider(key, name)}
                                        onModel={model => setModel(key, model)}
                                        onUnused={key === 'grounder' ? setGrounderUnused : undefined}
                                    />
                                ))}
                            </div>
                        ) : null}
                    </div>

                    {notice ? <p className="bot-llm-dialog__notice" role="status">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" onClick={onClose} disabled={saving}>{labels.cancel}</button>
                    <button type="button" className="btn-primary" data-testid="bot-llm-save" onClick={submit} disabled={!canSave}>{labels.save}</button>
                </div>
            </div>
        </div>
    );
}

function RoleRow({
    roleKey,
    title,
    hint,
    draft,
    options,
    labels,
    saving,
    lang,
    savedModel,
    onProvider,
    onModel,
    onUnused,
}: {
    roleKey: RoleKey;
    title: string;
    hint: string;
    draft: RoleDraft;
    options: BotLLMProviderOption[];
    labels: BotLLMSettingsDialogLabels;
    saving: boolean;
    lang?: string;
    savedModel: string;
    onProvider: (name: string) => void;
    onModel: (model: string) => void;
    onUnused?: (unused: boolean) => void;
}) {
    const t = (en: string, zhHans: string, zhHant = zhHans) => panelText(lang ?? '', en, zhHans, zhHant);
    const providerId = useId();
    const modelId = useId();
    const provider = options.find(item => item.name === draft.providerName) ?? options[0];
    const showFields = roleKey !== 'grounder' || !draft.unused;
    return (
        <div className="bot-role-assignments__row is-customized" data-testid={`bot-role-${roleKey}`}>
            <div className="bot-role-assignments__head">
                <div className="bot-role-assignments__label" title={hint}>
                    <span className="bot-role-assignments__title">{title}</span>
                    <span className="bot-role-assignments__hint">{hint}</span>
                </div>
                {onUnused ? (
                    <label className="bot-role-assignments__follow">
                        <input
                            type="checkbox"
                            aria-label={t('Grounding model not used', '不使用定位模型', '不使用定位模型')}
                            checked={draft.unused}
                            disabled={saving}
                            data-testid="bot-role-grounder-unused"
                            onChange={event => onUnused(event.target.checked)}
                        />
                        {t('Not used', '不使用', '不使用')}
                    </label>
                ) : null}
            </div>
            {showFields ? (
                <div className="bot-role-assignments__fields">
                    <div className="bot-role-assignments__field">
                        <label className="bot-role-assignments__field-label" htmlFor={providerId}>{labels.providerLabel}</label>
                        <select
                            id={providerId}
                            className="form-input bot-llm-dialog__select"
                            aria-label={`${title} ${labels.providerLabel}`}
                            value={provider?.name ?? ''}
                            disabled={saving}
                            data-testid={`bot-role-${roleKey}-provider`}
                            onClick={event => event.stopPropagation()}
                            onChange={event => onProvider(event.target.value)}
                        >
                            {options.map(item => (
                                <option key={item.name} value={item.name}>{item.name}</option>
                            ))}
                        </select>
                    </div>
                    <RoleModelSelect
                        id={modelId}
                        title={title}
                        provider={provider}
                        model={draft.model}
                        savedModel={draft.providerName === provider?.name ? savedModel : ''}
                        labels={labels}
                        saving={saving}
                        testId={`bot-role-${roleKey}-model`}
                        onChange={onModel}
                    />
                </div>
            ) : null}
        </div>
    );
}

function RoleModelSelect({
    id,
    title,
    provider,
    model,
    savedModel,
    labels,
    saving,
    testId,
    onChange,
}: {
    id: string;
    title: string;
    provider: BotLLMProviderOption | undefined;
    model: string;
    savedModel: string;
    labels: BotLLMSettingsDialogLabels;
    saving: boolean;
    testId: string;
    onChange: (model: string) => void;
}) {
    const preferred = savedModel.trim() || model;
    const [choices, setChoices] = useState<string[]>(() => modelChoices(provider, preferred, []));
    const [loading, setLoading] = useState(() => willFetchModels(provider));
    const seq = useRef(0);
    const providerName = provider?.name ?? '';
    const providerURL = provider?.url ?? '';
    const providerKey = provider?.key ?? '';

    useEffect(() => {
        const current = ++seq.current;
        const seeded = modelChoices(provider, preferred, []);
        setChoices(seeded);
        if (!seeded.includes(model) && seeded[0]) onChange(seeded[0]);
        if (!willFetchModels(provider)) {
            setLoading(false);
            return;
        }
        setLoading(true);
        FetchProviderModels(provider.url, provider.key, provider.protocol || 'openai', provider.agentType || 'openclaw')
            .then(items => {
                if (current !== seq.current) return;
                const fetched = (Array.isArray(items) ? items : [])
                    .map(item => String(item?.id ?? item?.name ?? '').trim())
                    .filter(item => item !== '');
                const next = modelChoices(provider, preferred, fetched);
                setChoices(next);
                if (!next.includes(model) && next[0]) onChange(next[0]);
            })
            .catch(() => {
                // Keep the seeded options; the service may not expose a list.
            })
            .finally(() => {
                if (current === seq.current) setLoading(false);
            });
        // The model is read when the provider changes. Following it would
        // refetch on every selection.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [providerName, providerURL, providerKey]);

    return (
        <div className="bot-role-assignments__field">
            <label className="bot-role-assignments__field-label" htmlFor={id}>{labels.modelLabel}</label>
            <select
                id={id}
                className="form-input bot-llm-dialog__select"
                aria-label={`${title} ${labels.modelLabel}`}
                value={choices.includes(model) ? model : (choices[0] ?? '')}
                disabled={saving || choices.length === 0}
                data-testid={testId}
                onClick={event => event.stopPropagation()}
                onChange={event => onChange(event.target.value)}
            >
                {choices.map(item => (
                    <option key={item} value={item}>{item}</option>
                ))}
            </select>
            {loading ? <p className="bot-llm-dialog__subhint" role="status">{labels.modelLoading}</p> : null}
            {!loading && choices.length === 0 ? <p className="bot-llm-dialog__subhint" role="status">{labels.modelEmpty}</p> : null}
        </div>
    );
}

function rolePayload(draft: RoleDraft, options: BotLLMProviderOption[]): BotLLMRoleSubmit {
    const provider = options.find(item => item.name === draft.providerName) ?? options[0];
    if (!provider) return { model: draft.model };
    if (provider.isHubService) return { official: true, model: draft.model };
    return {
        name: provider.name,
        url: provider.url,
        key: provider.key,
        model: draft.model,
        protocol: provider.protocol || '',
        wire_api: provider.wireAPI || '',
    };
}

function collectProviderOptions(providers: BotLLMProviderOption[], roles: BotLLMRolesSetting | null | undefined, legacyName: string, legacyModel: string): BotLLMProviderOption[] {
    const usable = providers.filter(item => item.name.trim() !== '' && (item.key.trim() !== '' || item.isHubService));
    const names = new Set(usable.map(item => item.name));
    const extras: BotLLMProviderOption[] = [];
    const addSaved = (role?: BotLLMRoleSetting) => {
        const name = role?.name?.trim() ?? '';
        if (!role || role.unused || role.official || name === '' || names.has(name)) return;
        names.add(name);
        extras.push({
            name,
            url: role.url?.trim() || '',
            key: '',
            model: role.model?.trim() || '',
            protocol: role.protocol?.trim() || '',
            wireAPI: role.wireAPI?.trim() || '',
            models: role.model?.trim() ? [role.model.trim()] : [],
            agentType: '',
            isHubService: false,
        });
    };
    if (roles) {
        addSaved(roles.foreground);
        addSaved(roles.background);
        addSaved(roles.computer_use);
        addSaved(roles.grounder);
    } else if (legacyName.trim() !== '' && !names.has(legacyName.trim())) {
        addSaved({ name: legacyName, model: legacyModel });
    }
    const wantsOfficial = !!roles && [roles.foreground, roles.background, roles.computer_use, roles.grounder].some(role => role?.official);
    if (wantsOfficial && !usable.some(item => item.isHubService)) {
        extras.unshift({
            name: 'Hub',
            url: '',
            key: '',
            model: '',
            protocol: '',
            wireAPI: '',
            models: [],
            agentType: '',
            isHubService: true,
        });
    }
    return [...usable, ...extras];
}

function savedModelFor(key: RoleKey, roles: BotLLMRolesSetting | null | undefined, legacyName: string, legacyModel: string): string {
    const role = roles?.[key];
    if (role?.model?.trim()) return role.model.trim();
    if (!roles && key !== 'grounder') return legacyModel.trim();
    return '';
}

function initialDrafts(options: BotLLMProviderOption[], mode: 'default' | 'custom', legacyName: string, legacyModel: string, roles: BotLLMRolesSetting | null | undefined): Record<RoleKey, RoleDraft> {
    const legacy = mode === 'custom' && !roles ? { name: legacyName.trim(), model: legacyModel.trim() } : undefined;
    return {
        foreground: draftFor(roles?.foreground ?? legacy, options, false),
        background: draftFor(roles?.background ?? legacy, options, false),
        computer_use: draftFor(roles?.computer_use ?? legacy, options, false),
        grounder: draftFor(roles?.grounder, options, true),
    };
}

function draftFor(role: BotLLMRoleSetting | undefined, options: BotLLMProviderOption[], grounder: boolean): RoleDraft {
    const first = options[0];
    const blank: RoleDraft = { providerName: first?.name ?? '', model: seedModel(first, first?.model ?? ''), unused: grounder };
    if (!role || (grounder && (role.unused || (!role.official && !role.name?.trim() && !role.model?.trim())))) {
        return blank;
    }
    if (role.official) {
        const hub = options.find(item => item.isHubService) ?? first;
        const model = role.model?.trim() || seedModel(hub, '');
        return { providerName: hub?.name ?? '', model, unused: false };
    }
    const name = role.name?.trim() ?? '';
    const match = options.find(item => item.name === name) ?? first;
    const model = role.model?.trim() || seedModel(match, '');
    if (!match) return { providerName: '', model, unused: false };
    return { providerName: match.name, model: model || seedModel(match, ''), unused: false };
}
