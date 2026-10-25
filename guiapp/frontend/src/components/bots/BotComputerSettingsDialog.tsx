import { useId, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

export type DesktopComputerAction = 'restart' | 'reset';

export type DesktopComputerState = {
    restart_allowed: boolean;
    reset_allowed: boolean;
    active_runs: number;
};

export type BotComputerSettingsLabels = {
    heading: string;
    title: string;
    shared: string;
    actions: Record<DesktopComputerAction, {
        name: string;
        button: string;
        /** Plain-language explanation, shown under the button and again in the confirm step. */
        description: string[];
        confirmTitle: string;
        confirmButton: string;
        forceButton: string;
        working: string;
        done: string;
    }>;
    resetDisabled: string;
    activeRuns: (count: number) => string;
    busy: string;
    close: string;
    cancel: string;
};

type Props = {
    labels: BotComputerSettingsLabels;
    state: DesktopComputerState;
    /** The action in flight, if any. */
    working: DesktopComputerAction | null;
    /** The action the server refused because a bot is working (offer force). */
    busy: DesktopComputerAction | null;
    notice: string;
    tabs?: ReactNode;
    onClose: () => void;
    onRun: (action: DesktopComputerAction, force: boolean) => void;
    /** Leaves the confirm step (clears a busy refusal). */
    onCancelConfirm: () => void;
};

function Description({ lines }: { lines: string[] }) {
    return (
        <ul className="bot-computer__desc">
            {lines.map(line => <li key={line}>{line}</li>)}
        </ul>
    );
}

/**
 * Computer tab: restart or reset the cloud computer shared by this person
 * and all of their bots. Both ask for confirmation, repeating the same plain
 * explanation shown under the button. Reset is danger-styled and needs an
 * explicit checkbox before it can run.
 */
export function BotComputerSettingsDialog({ labels, state, working, busy, notice, tabs, onClose, onRun, onCancelConfirm }: Props) {
    const titleId = useId();
    const [confirm, setConfirm] = useState<DesktopComputerAction | null>(null);
    const [ack, setAck] = useState(false);
    const running = working !== null;
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !running });

    const onKeyDown = (event: ReactKeyboardEvent) => {
        if (event.key === 'Escape') {
            event.preventDefault();
            event.stopPropagation();
            if (running) return;
            if (confirm || busy) { setConfirm(null); onCancelConfirm(); }
            else onClose();
        }
    };

    const ask = (action: DesktopComputerAction) => {
        setAck(false);
        setConfirm(action);
    };

    const pending = confirm ?? working ?? busy;
    const showConfirm = pending !== null;
    const action = pending ? labels.actions[pending] : null;
    const canConfirm = !running && (pending !== 'reset' || ack);

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div
                className="modal-content custom-dialog bot-llm-dialog"
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-testid="bot-computer-dialog"
                tabIndex={-1}
                {...dialogProps}
            >
                <div className="modal-header custom-dialog__header">
                    <div className="custom-dialog__heading">
                        <h3 id={titleId} className="custom-dialog__title">{labels.heading}</h3>
                    </div>
                    <button type="button" className="btn-close custom-dialog__close" aria-label={labels.close} disabled={running} onClick={onClose}>
                        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
                            <path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                        </svg>
                    </button>
                </div>
                {showConfirm ? null : tabs}
                {showConfirm && action && pending ? (
                    <>
                        <div className="modal-body custom-dialog__body" data-testid={`bot-computer-confirm-${pending}`}>
                            <h4 className={`bot-llm-dialog__section${pending === 'reset' ? ' bot-computer__danger-title' : ''}`}>{action.confirmTitle}</h4>
                            <Description lines={action.description} />
                            <p className="bot-llm-dialog__subhint bot-computer__shared">{labels.shared}</p>
                            {state.active_runs > 0 ? <p className="bot-llm-dialog__notice" role="status">{labels.activeRuns(state.active_runs)}</p> : null}
                            {busy === pending && !running ? <p className="bot-llm-dialog__notice" role="alert" data-testid="bot-computer-busy">{labels.busy}</p> : null}
                            {pending === 'reset' && !running ? (
                                <label className="bot-llm-dialog__radio bot-computer__ack">
                                    <input type="checkbox" checked={ack} onChange={e => setAck(e.target.checked)} data-testid="bot-computer-reset-ack" />
                                    <span>{action.confirmButton}</span>
                                </label>
                            ) : null}
                            {running ? <p className="bot-llm-dialog__notice" role="status" data-testid="bot-computer-working">{action.working}</p> : null}
                            {notice && !running ? <p className="bot-llm-dialog__notice" role="status">{notice}</p> : null}
                        </div>
                        <div className="modal-footer custom-dialog__footer">
                            <button type="button" className="btn-secondary" disabled={running} onClick={() => { setConfirm(null); setAck(false); onCancelConfirm(); }}>{labels.cancel}</button>
                            <button
                                type="button"
                                className={pending === 'reset' ? 'btn-secondary btn-danger' : 'btn-primary'}
                                data-testid={`bot-computer-confirm-${pending}-go`}
                                disabled={!canConfirm}
                                onClick={() => { if (canConfirm) { setConfirm(null); onRun(pending, busy === pending); } }}
                            >
                                {busy === pending ? action.forceButton : action.button}
                            </button>
                        </div>
                    </>
                ) : (
                    <>
                        <div className="modal-body custom-dialog__body">
                            <h4 className="bot-llm-dialog__section">{labels.title}</h4>
                            <p className="bot-llm-dialog__subhint bot-computer__shared">{labels.shared}</p>
                            {(['restart', 'reset'] as DesktopComputerAction[]).map(item => {
                                const copy = labels.actions[item];
                                const disabled = running || (item === 'reset' ? !state.reset_allowed : !state.restart_allowed);
                                return (
                                    <div className="bot-llm-dialog__option bot-computer__item" key={item}>
                                        <button
                                            type="button"
                                            className={item === 'reset' ? 'btn-secondary btn-danger' : 'btn-secondary'}
                                            data-testid={`bot-computer-${item}`}
                                            disabled={disabled}
                                            onClick={() => ask(item)}
                                        >
                                            {copy.button}
                                        </button>
                                        <Description lines={copy.description} />
                                        {item === 'reset' && !state.reset_allowed ? <p className="bot-llm-dialog__subhint">{labels.resetDisabled}</p> : null}
                                    </div>
                                );
                            })}
                            {notice ? <p className="bot-llm-dialog__notice" role="status" data-testid="bot-computer-notice">{notice}</p> : null}
                        </div>
                        <div className="modal-footer custom-dialog__footer">
                            <button type="button" className="btn-secondary" onClick={onClose}>{labels.close}</button>
                        </div>
                    </>
                )}
            </div>
        </div>
    );
}
