import { useId, useRef, useState, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';

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

export type BotSettingsTab = 'model' | 'network' | 'computer';

export type BotSettingsTabsLabels = { model: string; network: string; computer: string };

function ModelTabIcon() {
    return (
        <svg className="bot-settings-tabs__icon" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
            <path d="M8 1.8 13.4 4.6v6.8L8 14.2 2.6 11.4V4.6z" />
            <path d="M2.6 4.6 8 7.4l5.4-2.8M8 7.4v6.8" />
        </svg>
    );
}

function NetworkTabIcon() {
    return (
        <svg className="bot-settings-tabs__icon" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
            <path d="M1.8 6.1a9 9 0 0 1 12.4 0" />
            <path d="M4.2 8.7a5.6 5.6 0 0 1 7.6 0" />
            <path d="M6.5 11.3a2.3 2.3 0 0 1 3 0" />
            <circle cx="8" cy="13.6" r="0.7" fill="currentColor" stroke="none" />
        </svg>
    );
}

function ComputerTabIcon() {
    return (
        <svg className="bot-settings-tabs__icon" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
            <rect x="1.8" y="2.4" width="12.4" height="8.4" rx="1.2" />
            <path d="M5.6 13.6h4.8M8 10.8v2.8" />
        </svg>
    );
}

/** The tab strip shared by the tabs of the Settings dialog. Each tab saves on its own. */
export function BotSettingsTabs({ active, labels, disabled, onSelect }: {
    active: BotSettingsTab;
    labels: BotSettingsTabsLabels;
    disabled?: boolean;
    onSelect: (tab: BotSettingsTab) => void;
}) {
    const tabs: { id: BotSettingsTab; icon: ReactNode }[] = [
        { id: 'model', icon: <ModelTabIcon /> },
        { id: 'network', icon: <NetworkTabIcon /> },
        { id: 'computer', icon: <ComputerTabIcon /> },
    ];

    // A tablist is one stop in the tab order; arrow keys move between its tabs.
    // Without this, Tab would stop on both tabs and Left/Right would do nothing,
    // which is what the ARIA tabs pattern promises they won't.
    const move = (from: BotSettingsTab, step: number) => {
        const index = tabs.findIndex(tab => tab.id === from);
        const next = tabs[(index + step + tabs.length) % tabs.length];
        if (next.id !== active) onSelect(next.id);
    };

    return (
        <div
            className="bot-settings-tabs"
            role="tablist"
            data-testid="bot-settings-tabs"
            onKeyDown={event => {
                if (disabled) return;
                if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return;
                event.preventDefault();
                event.stopPropagation();
                move(active, event.key === 'ArrowRight' ? 1 : -1);
            }}
        >
            {tabs.map(({ id, icon }) => (
                <button
                    key={id}
                    type="button"
                    role="tab"
                    aria-selected={active === id}
                    // Only the active tab is tabbable, so the strip is a single stop.
                    tabIndex={active === id ? 0 : -1}
                    className={`bot-settings-tabs__tab${active === id ? ' is-active' : ''}`}
                    data-testid={`bot-settings-tab-${id}`}
                    disabled={disabled}
                    onClick={() => { if (active !== id) onSelect(id); }}
                >
                    {icon}
                    {labels[id]}
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
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: () => { if (!saving) onClose(); } });

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div
                className="modal-content custom-dialog bot-llm-dialog"
                ref={dialogRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-testid="bot-network-dialog"
                tabIndex={-1}
                {...dialogProps}
            >
                <div className="bot-llm-dialog__head">
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
                </div>
                <div className="modal-body custom-dialog__body">
                    <h4 className="bot-llm-dialog__section">{labels.title}</h4>
                    <p className="bot-llm-dialog__hint">{labels.hint}</p>
                    <p className="bot-llm-dialog__status" data-testid="bot-network-current">{labels.current(labels.modes[current].name)}</p>
                    {DESKTOP_PROXY_MODES.map(value => (
                        <div
                            className={`bot-llm-dialog__option${mode === value ? ' is-selected' : ''}${locked ? ' is-disabled' : ''}`}
                            key={value}
                            onClick={() => { if (!saving && !locked) setMode(value); }}
                        >
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
                    {locked ? <p className="bot-llm-dialog__note" role="status" data-testid="bot-network-locked">{labels.locked}</p> : null}
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
