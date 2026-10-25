import { useId, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

/** Desktop egress modes, as Hub stores them (desktop_proxy_mode). */
export type DesktopProxyModeValue = 'proxy' | 'direct' | 'smart';

export const DESKTOP_PROXY_MODES: DesktopProxyModeValue[] = ['proxy', 'direct', 'smart'];

export type DesktopProxyModeState = {
    mode: string;
    user_mode?: string;
    tenant_default?: string;
    locked: boolean;
    applied?: boolean;
    warning?: string;
};

export function normalizeDesktopProxyMode(raw: string | undefined | null): DesktopProxyModeValue {
    const value = String(raw ?? '').trim().toLowerCase();
    if (value === 'proxy' || value === 'always') return 'proxy';
    if (value === 'direct') return 'direct';
    return 'smart';
}

export type BotSettingsTab = 'model' | 'network';

export type BotSettingsTabsLabels = { model: string; network: string };

/** The tab strip shared by the two halves of the Settings dialog. Each tab saves on its own. */
export function BotSettingsTabs({ active, labels, disabled, onSelect }: {
    active: BotSettingsTab;
    labels: BotSettingsTabsLabels;
    disabled?: boolean;
    onSelect: (tab: BotSettingsTab) => void;
}) {
    const tabs: BotSettingsTab[] = ['model', 'network'];
    return (
        <div className="bot-settings-tabs" role="tablist" data-testid="bot-settings-tabs">
            {tabs.map(tab => (
                <button
                    key={tab}
                    type="button"
                    role="tab"
                    aria-selected={active === tab}
                    className={`bot-settings-tabs__tab${active === tab ? ' is-active' : ''}`}
                    data-testid={`bot-settings-tab-${tab}`}
                    disabled={disabled}
                    onClick={() => { if (active !== tab) onSelect(tab); }}
                >
                    {labels[tab]}
                </button>
            ))}
        </div>
    );
}

export type BotNetworkSettingsLabels = {
    heading: string;
    title: string;
    hint: string;
    current: (mode: string) => string;
    modes: Record<DesktopProxyModeValue, { name: string; hint: string }>;
    locked: string;
    save: string;
    cancel: string;
    close: string;
};

type Props = {
    labels: BotNetworkSettingsLabels;
    state: DesktopProxyModeState;
    notice: string;
    saving: boolean;
    tabs?: ReactNode;
    onClose: () => void;
    onSubmit: (mode: DesktopProxyModeValue) => void;
};

/**
 * Network tab: how this user's cloud desktop reaches the internet. The switch
 * applies to the running desktop (no restart). A tenant lock shows the forced
 * mode with the reason and disables the choice.
 */
export function BotNetworkSettingsDialog({ labels, state, notice, saving, tabs, onClose, onSubmit }: Props) {
    const titleId = useId();
    const current = normalizeDesktopProxyMode(state.mode);
    const [mode, setMode] = useState<DesktopProxyModeValue>(current);
    const locked = !!state.locked;
    const canSave = !saving && !locked && mode !== current;
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !saving });

    const onKeyDown = (event: ReactKeyboardEvent) => {
        if (event.key === 'Escape') {
            event.preventDefault();
            event.stopPropagation();
            if (!saving) onClose();
        }
    };

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div
                className="modal-content custom-dialog bot-llm-dialog"
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-testid="bot-network-dialog"
                tabIndex={-1}
                {...dialogProps}
            >
                <div className="modal-header custom-dialog__header">
                    <div className="custom-dialog__heading">
                        <h3 id={titleId} className="custom-dialog__title">{labels.heading}</h3>
                    </div>
                    <button type="button" className="btn-close custom-dialog__close" aria-label={labels.close} disabled={saving} onClick={onClose}>
                        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
                            <path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                        </svg>
                    </button>
                </div>
                {tabs}
                <div className="modal-body custom-dialog__body">
                    <h4 className="bot-llm-dialog__section">{labels.title}</h4>
                    <p className="bot-llm-dialog__hint">{labels.hint}</p>
                    <p className="bot-llm-dialog__subhint" data-testid="bot-network-current">{labels.current(labels.modes[current].name)}</p>
                    {DESKTOP_PROXY_MODES.map(value => (
                        <div className="bot-llm-dialog__option" key={value}>
                            <label className="bot-llm-dialog__radio">
                                <input
                                    type="radio"
                                    name="bot-network-mode"
                                    value={value}
                                    checked={mode === value}
                                    disabled={saving || locked}
                                    onChange={() => setMode(value)}
                                    data-testid={`bot-network-mode-${value}`}
                                />
                                <span>{labels.modes[value].name}</span>
                            </label>
                            <p className="bot-llm-dialog__subhint">{labels.modes[value].hint}</p>
                        </div>
                    ))}
                    {locked ? <p className="bot-llm-dialog__notice" role="status" data-testid="bot-network-locked">{labels.locked}</p> : null}
                    {notice ? <p className="bot-llm-dialog__notice" role="status">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" onClick={onClose} disabled={saving}>{labels.cancel}</button>
                    <button type="button" className="btn-primary" data-testid="bot-network-save" onClick={() => { if (canSave) onSubmit(mode); }} disabled={!canSave}>{labels.save}</button>
                </div>
            </div>
        </div>
    );
}
